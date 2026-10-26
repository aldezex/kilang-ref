package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// This file is `bookx web`: it writes the files that the page of the book
// reads, so that the controller can publish them to the artifact. The page
// itself is cmd/bookx/web/index.html, embedded in the binary (see main.go).

// specPath is the spec, in the working tree of the repository, whose section
// 12.5 holds the index of the book.
const specPath = "docs/superpowers/specs/2026-10-24-ki-design-go.md"

// fffdMarker takes the place of every U+FFFD in the Markdown of a chapter. The
// host that serves the artifact refuses U+FFFD in text, and the page turns the
// marker back into the character when it shows the chapter.
const fffdMarker = "%%U+FFFD%%"

// Manifest is the content of book/manifest.json: the chapters that are
// published, in the order of their numbers.
type Manifest struct {
	Chapters []ManifestChapter `json:"chapters"`
}

// ManifestChapter describes one published chapter. Title, Hours, Part and
// Milestone come from the front matter of its prose and Tags are the tags of
// the repository that belong to the chapter: its sessions in the order of
// their numbers, then the chapter tag.
type ManifestChapter struct {
	Chapter   int      `json:"chapter"`
	Title     string   `json:"title"`
	File      string   `json:"file"`
	Hours     int      `json:"hours"`
	Part      string   `json:"part"`
	Milestone string   `json:"milestone"`
	Tags      []string `json:"tags"`
}

// frontMatter is the metadata block at the start of the prose of a chapter.
type frontMatter struct {
	Chapter   int
	Title     string
	Part      string
	Hours     int
	Milestone string
}

// parseFrontMatter reads the block between the two `---` lines at the start of
// the Markdown of chapter nn. It needs the five keys of book/README.md, and
// the chapter number has to be the one in the file name. The problems carry
// path p and the line they are on.
func parseFrontMatter(p, nn, markdown string) (frontMatter, []ScanError) {
	var fm frontMatter
	fail := func(line int, format string, args ...any) []ScanError {
		return []ScanError{{Path: p, Line: line, Message: fmt.Sprintf(format, args...)}}
	}
	lines := splitLines([]byte(markdown))
	if len(lines) == 0 || lines[0] != "---" {
		return fm, fail(1, "the chapter has no front matter: the file has to start with a --- line")
	}
	end := slices.Index(lines[1:], "---") + 1
	if end == 0 {
		return fm, fail(1, "the front matter is never closed with a --- line")
	}
	values := map[string]string{}
	at := map[string]int{} // line of each key
	for i, line := range lines[1:end] {
		if key, val, ok := strings.Cut(line, ":"); ok {
			key = strings.TrimSpace(key)
			if _, dup := values[key]; !dup {
				values[key], at[key] = strings.TrimSpace(val), i+2
			}
		}
	}
	var problems []ScanError
	for _, key := range []string{"chapter", "title", "part", "hours", "milestone"} {
		if values[key] == "" {
			return fm, fail(1, "the front matter has no `%s`", key)
		}
	}
	number := func(key string) (n int, ok bool) {
		n, err := strconv.Atoi(values[key])
		if err != nil {
			problems = append(problems, fail(at[key], "the front matter says %s: %q, which is not a number", key, values[key])...)
			return 0, false
		}
		return n, true
	}
	var ok bool
	if fm.Chapter, ok = number("chapter"); ok {
		if want, _ := strconv.Atoi(nn); fm.Chapter != want {
			problems = append(problems, fail(at["chapter"], "the front matter says chapter %d, but the file is the prose of chapter %d", fm.Chapter, want)...)
		}
	}
	fm.Hours, _ = number("hours")
	fm.Title, fm.Part, fm.Milestone = values["title"], values["part"], values["milestone"]
	return fm, problems
}

// chapterTags returns the tags of chapter c that exist in the repository: its
// sessions ordered by their number (ch02-s10 after ch02-s9), then the chapter
// tag if it exists. The slice is never nil.
func chapterTags(c *chapter) []string {
	tags := slices.Clone(c.Sessions)
	num := func(tag string) int {
		n, _ := strconv.Atoi(sessionTagRe.FindStringSubmatch(tag)[2]) // every session tag matches
		return n
	}
	slices.SortFunc(tags, func(a, b string) int { return num(a) - num(b) })
	if c.HasTag {
		tags = append(tags, "ch"+c.NN)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags
}

// problemChapterRe finds the chapter that a problem belongs to from its path:
// the prose book/chNN-slug.md, the cards or list book/tests/chNN.*, or a file
// of a tag, which verify writes as "chNN-sK:path".
var problemChapterRe = regexp.MustCompile(`^(?:book/ch|book/tests/ch|ch)(\d{2})(?:[-.:]|$)`)

// problemChapter returns the number of the chapter that problem p belongs to,
// or "" when it belongs to none, such as a problem in verified-tags.txt.
func problemChapter(p ScanError) string {
	if m := problemChapterRe.FindStringSubmatch(p.Path); m != nil {
		return m[1]
	}
	return ""
}

// buildWeb writes the page of the book into dir: index.html, book/outline.json,
// book/manifest.json and a book/chNN.md for each chapter that is published,
// which is each one with prose and with its chapter tag in verified-tags.txt,
// or each one with prose when all is set. It writes nothing and returns the
// problems when verify finds any in the chapters it would publish (problems of
// the other chapters are none of its business), or when their front matter is
// wrong. It only adds and replaces files: whatever dir already holds, such as
// the chapter of an earlier run that is no longer published, stays where it
// is, so the caller gives it a directory of its own. The error is for what
// stops the work altogether.
func buildWeb(tree fs.FS, repo repoReader, dir string, all bool) ([]ScanError, error) {
	spec, err := fs.ReadFile(tree, specPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read the index of the book: %w", err)
	}
	outline, err := parseSpecIndex(spec)
	if err != nil {
		return nil, fmt.Errorf("cannot read the index of the book: %s: %w", specPath, err)
	}

	verified := map[string]bool{}
	text, err := fs.ReadFile(tree, verifiedTagsPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		tags, err := parseVerifiedTags(text)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", verifiedTagsPath, err)
		}
		for _, t := range tags {
			verified[t.Tag] = true
		}
	}

	b, _, err := loadBook(tree, repo)
	if err != nil {
		return nil, err
	}
	var chapters []*chapter
	selected := map[string]bool{}
	for _, c := range b.sortedChapters() {
		if c.Prose != "" && (all || verified["ch"+c.NN]) {
			chapters = append(chapters, c)
			selected[c.NN] = true
		}
	}

	var problems []ScanError
	found, err := verify(tree, repo)
	if err != nil {
		return nil, err
	}
	for _, p := range found {
		if nn := problemChapter(p); nn == "" || selected[nn] {
			problems = append(problems, p)
		}
	}

	manifest := Manifest{Chapters: []ManifestChapter{}}
	rendered := map[string]string{}
	for _, c := range chapters {
		md, _ := b.renderChapter(c) // verify has reported its problems
		fm, ps := parseFrontMatter(c.Prose, c.NN, md)
		problems = append(problems, ps...)
		rendered[c.NN] = md
		outline.Exercises[strconv.Itoa(fm.Chapter)] = countExercises([]byte(md))
		manifest.Chapters = append(manifest.Chapters, ManifestChapter{
			Chapter: fm.Chapter, Title: fm.Title, File: "ch" + c.NN + ".md", Hours: fm.Hours,
			Part: fm.Part, Milestone: fm.Milestone, Tags: chapterTags(c),
		})
	}
	if len(problems) > 0 {
		sortScanErrors(problems)
		return problems, nil
	}

	bookDir := filepath.Join(dir, "book")
	if err := os.MkdirAll(bookDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), indexHTML, 0o644); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(bookDir, "outline.json"), outline); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(bookDir, "manifest.json"), manifest); err != nil {
		return nil, err
	}
	for _, c := range chapters {
		md := strings.ReplaceAll(rendered[c.NN], replacementChar, fffdMarker)
		if err := os.WriteFile(filepath.Join(bookDir, "ch"+c.NN+".md"), []byte(md), 0o644); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// writeJSON writes v to the file p as one line of JSON. The encoder does not
// escape <, > and & (the page reads the text, not HTML). A U+FFFD that was in
// the data is written as the six-character JSON escape (a backslash, u and
// fffd), which means the same in JSON, so no file of the output holds the
// character itself.
func writeJSON(p string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return os.WriteFile(p, bytes.ReplaceAll(buf.Bytes(), []byte(replacementChar), []byte("\\"+"ufffd")), 0o644)
}

// runWeb is `bookx web <out-dir> [--repo DIR] [--all]`.
func runWeb(args []string, stdout, stderr io.Writer) int {
	var all bool
	repo, pos, ok := parseArgs("web", args, 1, true, stderr, func(flags *flag.FlagSet) {
		flags.BoolVar(&all, "all", false, "publish every chapter that has prose, not only the verified ones")
	})
	if !ok {
		return exitUsage
	}
	problems, err := buildWeb(os.DirFS(repo), Git{Repo: repo}, pos[0], all)
	if err != nil {
		fmt.Fprintf(stderr, "bookx web: %v\n", err)
		return exitFail
	}
	for _, p := range problems {
		fmt.Fprintln(stdout, p)
	}
	if len(problems) > 0 {
		return exitFail
	}
	return exitOK
}
