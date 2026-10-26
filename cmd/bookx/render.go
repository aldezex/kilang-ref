package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// replacementChar is U+FFFD, the character that stands for bytes that are not
// valid UTF-8. It is written as an escape because the host that publishes the
// book refuses the character itself in a source file.
const replacementChar = "\uFFFD"

// langByExt gives the language of a code fence for the extension of the quoted
// file. Anything not listed is plain text.
var langByExt = map[string]string{
	".go": "go", ".ki": "ki", ".sh": "sh", ".yml": "yaml", ".s": "goasm", ".md": "markdown", ".mod": "text",
}

// fenceLang returns the language of the fence that quotes the file p.
func fenceLang(p string) string {
	if lang, ok := langByExt[path.Ext(p)]; ok {
		return lang
	}
	return "text"
}

// attrEscaper writes the two characters that would break a fence attribute as
// entities, which the page of the book turns back into the characters.
var attrEscaper = strings.NewReplacer(`"`, "&quot;", "`", "&#96;")

// fenceInfo builds the text after the opening backticks of a fence: the
// language and the attributes, in the order they are given, as key="value"
// pairs with the value escaped.
func fenceInfo(lang string, attrs ...[2]string) string {
	var sb strings.Builder
	sb.WriteString(lang)
	for _, kv := range attrs {
		fmt.Fprintf(&sb, ` %s="%s"`, kv[0], attrEscaper.Replace(kv[1]))
	}
	return sb.String()
}

// fenceBlock returns the lines of a fenced code block with the given info text
// around content. Invalid UTF-8 in the content becomes U+FFFD (a run of bad
// bytes becomes one), a BOM and CR characters before a newline are dropped like
// everywhere else in bookx, and the fence is longer than any run of backticks
// that starts a line of the content, so the content cannot close it early.
func fenceBlock(info string, content []byte) []string {
	content = bytes.ToValidUTF8(content, []byte(replacementChar))
	lines := splitLines(content)
	longest := 2
	for _, l := range lines {
		if n := backtickRun(strings.TrimLeft(l, " \t"), 0); n > longest {
			longest = n
		}
	}
	fence := strings.Repeat("`", longest+1)
	out := make([]string, 0, len(lines)+2)
	out = append(out, fence+info)
	out = append(out, lines...)
	return append(out, fence)
}

// cardCell makes the text of a card fit in a cell of a Markdown table.
func cardCell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

// cardsTable returns the lines of the table that {{cards}} stands for: one row
// per card, with the columns Id, Caso, Por qué, Esperado and Nivel. A `|` in a
// field is written `\|`.
func cardsTable(cards []Card) []string {
	lines := []string{"| Id | Caso | Por qué | Esperado | Nivel |", "|---|---|---|---|---|"}
	for _, c := range cards {
		lines = append(lines, fmt.Sprintf("| %s | %s | %s | %s | %s |",
			cardCell(c.ID), cardCell(c.Case), cardCell(c.Why), cardCell(c.Expect), cardCell(c.Level)))
	}
	return lines
}

// renderChapter resolves the directives of the prose of chapter c and returns
// the Markdown with each directive replaced by what it stands for, together
// with the problems it found: a directive that is badly written, is not alone
// on its line, or whose target is not where it says. The result is only
// meaningful when there are no problems. It also records, in b.cited, the
// first place where each tag is cited, which verify checks afterwards.
func (b *book) renderChapter(c *chapter) (string, []ScanError) {
	text, err := fs.ReadFile(b.tree, c.Prose)
	if err != nil {
		return "", []ScanError{{Path: c.Prose, Line: 1, Message: fmt.Sprintf("cannot read the file: %v", err)}}
	}
	var problems []ScanError
	ds, errs := findDirectives(text)
	for _, e := range errs {
		e.Path = c.Prose
		problems = append(problems, e)
	}
	byLine := map[int][]Directive{}
	for _, d := range ds {
		byLine[d.Line] = append(byLine[d.Line], d)
	}

	var out []string
	var open fence
	for i, line := range splitLines(text) {
		n := i + 1
		if open.char != 0 {
			if open.closedBy(line) {
				open = fence{}
			}
			out = append(out, line)
			continue
		}
		if f, ok := openingFence(line); ok {
			open = f
			out = append(out, line)
			continue
		}
		on := byLine[n]
		if len(on) == 0 {
			out = append(out, line)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if len(on) != 1 || !strings.HasPrefix(trimmed, "{{") || strings.Index(trimmed, "}}")+2 != len(trimmed) {
			problems = append(problems, ScanError{Path: c.Prose, Line: n, Message: "a directive has to be alone on its line"})
			out = append(out, line)
			continue
		}
		block, msg := b.resolve(c, on[0])
		if msg != "" {
			problems = append(problems, ScanError{Path: c.Prose, Line: n, Message: msg})
			out = append(out, line)
			continue
		}
		out = append(out, block...)
	}
	sortScanErrors(problems)
	return strings.Join(out, "\n") + "\n", problems
}

// cite records that the prose of chapter c, at line, cites tag.
func (b *book) cite(c *chapter, tag string, line int) {
	if _, ok := b.cited[tag]; !ok {
		b.cited[tag] = citation{Path: c.Prose, Line: line}
	}
}

// checkCitedTag checks that a tag cited by a directive exists and is a session
// tag, and records the citation. It returns the message of the problem, or "".
func (b *book) checkCitedTag(c *chapter, tag string, line int) string {
	if chapterTagRe.MatchString(tag) {
		return fmt.Sprintf("the prose cites the chapter tag `%s`, which does not exist while the chapter is written: cite a session tag such as `%s-s1`", tag, tag)
	}
	if !b.tags[tag] {
		return fmt.Sprintf("unknown tag `%s`", tag)
	}
	b.cite(c, tag, line)
	return ""
}

// resolve returns the lines that a directive stands for, or the message of the
// problem when its target cannot be found.
func (b *book) resolve(c *chapter, d Directive) ([]string, string) {
	switch d.Kind {
	case DirCards:
		return cardsTable(c.Cards), ""

	case DirFile:
		if msg := b.checkCitedTag(c, d.Tag, d.Line); msg != "" {
			return nil, msg
		}
		src, msg := b.readInTag(d.Tag, d.Target)
		if msg != "" {
			return nil, msg
		}
		info := fenceInfo(fenceLang(d.Target), [2]string{"title", d.Target}, [2]string{"tag", d.Tag}, [2]string{"mode", "file"})
		return fenceBlock(info, src), ""

	case DirListing:
		if msg := b.checkCitedTag(c, d.Tag, d.Line); msg != "" {
			return nil, msg
		}
		var found []Region
		for _, r := range b.scanTag(d.Tag).regions {
			if r.ID == d.Target {
				found = append(found, r)
			}
		}
		switch len(found) {
		case 0:
			return nil, fmt.Sprintf("region `%s` is not in tag `%s`", d.Target, d.Tag)
		case 1:
		default:
			return nil, fmt.Sprintf("region `%s` is defined %d times in tag `%s`", d.Target, len(found), d.Tag)
		}
		r := found[0]
		attrs := [][2]string{{"title", r.Path}, {"tag", d.Tag}, {"mode", "listing"}}
		if r.Mode == RegionHint {
			attrs = append(attrs, [2]string{"hint", r.Hint})
		}
		return fenceBlock(fenceInfo(fenceLang(r.Path), attrs...), []byte(strings.Join(r.Lines, "\n"))), ""

	case DirDiff:
		for _, tag := range []string{d.Tag, d.TagTo} {
			if msg := b.checkCitedTag(c, tag, d.Line); msg != "" {
				return nil, msg
			}
		}
		inFrom, err := b.hasFile(d.Tag, d.Target)
		if err != nil {
			return nil, fmt.Sprintf("cannot list the files of tag `%s`: %v", d.Tag, err)
		}
		inTo, err := b.hasFile(d.TagTo, d.Target)
		if err != nil {
			return nil, fmt.Sprintf("cannot list the files of tag `%s`: %v", d.TagTo, err)
		}
		if !inFrom && !inTo {
			return nil, fmt.Sprintf("`%s` is in neither tag `%s` nor tag `%s`", d.Target, d.Tag, d.TagTo)
		}
		diff, err := b.repo.Diff(d.Tag, d.TagTo, d.Target)
		if err != nil {
			return nil, fmt.Sprintf("cannot diff `%s` between `%s` and `%s`: %v", d.Target, d.Tag, d.TagTo, err)
		}
		if len(diff) == 0 {
			return nil, fmt.Sprintf("`%s` is the same in tag `%s` and in tag `%s`: the diff would be empty", d.Target, d.Tag, d.TagTo)
		}
		info := fenceInfo("diff", [2]string{"title", d.Target}, [2]string{"tag", d.Tag + ".." + d.TagTo}, [2]string{"mode", "diff"})
		return fenceBlock(info, diff), ""

	case DirRefTest:
		i := slices.IndexFunc(c.Cards, func(card Card) bool { return card.ID == d.Target })
		if i < 0 {
			return nil, fmt.Sprintf("there is no card %s in the cards of chapter %s", d.Target, c.NN)
		}
		card := c.Cards[i]
		if card.Kind != "unit" && card.Kind != "prop" && card.Kind != "example" && card.Kind != "bench" {
			return nil, fmt.Sprintf("card %s is a `%s` card: {{reftest}} shows the Go test of a unit, prop, example or bench card", card.ID, card.Kind)
		}
		if c.CodeTag == "" {
			return nil, fmt.Sprintf("chapter %s has no tag `ch%s` and no session tag `ch%s-sK` to read the test from", c.NN, c.NN, c.NN)
		}
		src, msg := b.readInTag(c.CodeTag, card.File)
		if msg != "" {
			return nil, msg
		}
		test, ok := extractGoTest(src, card.Ref)
		if !ok {
			return nil, fmt.Sprintf("test `%s` of card %s is not in `%s` (tag `%s`)", card.Ref, card.ID, card.File, c.CodeTag)
		}
		info := fenceInfo("go", [2]string{"title", card.File}, [2]string{"tag", "ch" + c.NN}, [2]string{"mode", "test"}, [2]string{"hint", card.ID + " " + card.Name})
		return fenceBlock(info, []byte(test)), ""

	default:
		panic("unreachable")
	}
}

// readInTag returns the content of file p in tag, or the message of the
// problem: the file is not there, or git could not read it.
func (b *book) readInTag(tag, p string) ([]byte, string) {
	ok, err := b.hasFile(tag, p)
	if err != nil {
		return nil, fmt.Sprintf("cannot list the files of tag `%s`: %v", tag, err)
	}
	if !ok {
		return nil, fmt.Sprintf("`%s` is not in tag `%s`", p, tag)
	}
	src, err := b.show(tag, p)
	if err != nil {
		return nil, fmt.Sprintf("cannot read `%s` in tag `%s`: %v", p, tag, err)
	}
	return src, ""
}

// renderBook writes the Markdown of every chapter that has prose into dir, as
// chNN.md, and nothing else. When any directive of any chapter does not
// resolve, it writes nothing and returns the problems. Problems with the cards
// of a chapter count too, because {{cards}} and {{reftest}} would show an
// incomplete table.
func renderBook(tree fs.FS, repo repoReader, dir string) ([]ScanError, error) {
	b, loadProblems, err := loadBook(tree, repo)
	if err != nil {
		return nil, err
	}
	var problems []ScanError
	var order []string // the chapters, in the order of their numbers
	rendered := map[string]string{}
	for _, c := range b.sortedChapters() {
		if c.Prose == "" {
			continue
		}
		for _, p := range loadProblems {
			if p.Path == c.Tests {
				problems = append(problems, p)
			}
		}
		md, ps := b.renderChapter(c)
		problems = append(problems, ps...)
		order, rendered[c.NN] = append(order, c.NN), md
	}
	if len(problems) > 0 {
		sortScanErrors(problems)
		return problems, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for _, nn := range order {
		if err := os.WriteFile(filepath.Join(dir, "ch"+nn+".md"), []byte(rendered[nn]), 0o644); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
