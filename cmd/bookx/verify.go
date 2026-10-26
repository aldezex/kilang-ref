package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// This file holds the checks of `bookx verify` and the loading of the book
// that `bookx render` shares with them. A book here is two things: a working
// tree, read as an fs.FS (the prose in book/, the cards, the lists of tests
// and verified-tags.txt), and a repoReader, which gives the files of the tags.
// Nothing in this file runs git or touches the disk by itself, so the tests
// feed it a tree and a repository that live in memory.

// Paths of the book inside the working tree.
const (
	verifiedTagsPath = "book/verified-tags.txt"
	notInBookMarker  = "// bookx: not-in-book"
)

var (
	// proseNameRe is the name of a chapter's prose, book/chNN-slug.md.
	proseNameRe = regexp.MustCompile(`^ch(\d{2})-[^/]+\.md$`)
	// listNameRe is the name of a list of tests, book/tests/chNN.list.
	listNameRe = regexp.MustCompile(`^ch(\d{2})\.list$`)
	// chapterTagRe is the tag of a chapter and sessionTagRe the one of a session.
	chapterTagRe = regexp.MustCompile(`^ch(\d{2})$`)
	sessionTagRe = regexp.MustCompile(`^ch(\d{2})-s(\d+)$`)
	// containerOpenRe is the line that opens a container of the prose. Its
	// first word is its kind. The page of the book reads it the same way.
	containerOpenRe = regexp.MustCompile(`^:::(\w+)(?:\s+(.*))?$`)
)

// regionExts are the extensions of the files that can carry @book markers:
// code, shell and YAML. Prose and docs are never scanned, because they quote
// the markers as examples, and neither are the files under a testdata
// directory, which hold fixtures that contain markers on purpose.
var regionExts = map[string]bool{
	".go": true, ".sh": true, ".yml": true, ".yaml": true, ".ki": true, ".s": true,
}

// String returns the word that follows the region id in its @book marker.
func (m RegionMode) String() string {
	switch m {
	case RegionFull:
		return "full"
	case RegionHint:
		return "hint"
	case RegionSolution:
		return "solution"
	case RegionTest:
		return "test"
	default:
		panic("unreachable")
	}
}

// chapter is what the book knows about one chapter, by its two-digit number.
type chapter struct {
	NN       string // "02"
	Prose    string // path of book/chNN-slug.md in the working tree; "" when there is none
	Extra    []string
	Tests    string // path of book/tests/chNN.tests; "" when the file is not there
	List     string // path of book/tests/chNN.list; "" when the file is not there
	HasTag   bool   // the tag chNN exists
	Sessions []string
	CodeTag  string // chNN if it exists, else the last session tag, else ""
	Cards    []Card
	CardErrs []ScanError
}

// Active reports whether the chapter is part of the book: it has prose or its
// tag. A chapter that only has a file of cards is still being prepared, so only
// the cards themselves are checked.
func (c *chapter) Active() bool { return c.Prose != "" || c.HasTag }

// citation is the first place where the prose cites a tag.
type citation struct {
	Path string
	Line int
}

// tagScan is the result of reading every file of a tag that can carry markers.
type tagScan struct {
	regions []Region
	errs    []ScanError
}

// book is a working tree and a repository, with the caches that spare git from
// being asked the same thing twice.
type book struct {
	tree fs.FS
	repo repoReader

	tags     map[string]bool
	files    map[string][]string
	shows    map[string]showResult
	scans    map[string]*tagScan
	cited    map[string]citation
	chapters map[string]*chapter
}

// showResult is the cached answer of repoReader.Show.
type showResult struct {
	data []byte
	err  error
}

// newBook returns a book with empty caches. Commands that only read the files
// of one tag, such as `bookx regions`, use it without a working tree.
func newBook(tree fs.FS, repo repoReader) *book {
	return &book{
		tree:     tree,
		repo:     repo,
		tags:     map[string]bool{},
		files:    map[string][]string{},
		shows:    map[string]showResult{},
		scans:    map[string]*tagScan{},
		cited:    map[string]citation{},
		chapters: map[string]*chapter{},
	}
}

// loadBook reads the tags of the repository and discovers the chapters: their
// prose, their cards and lists in the working tree, and their tags. It reads
// and parses the cards of every chapter. The returned problems are the ones
// found while doing so (a card file that does not parse, two prose files for
// one chapter).
func loadBook(tree fs.FS, repo repoReader) (*book, []ScanError, error) {
	b := newBook(tree, repo)
	var problems []ScanError
	fail := func(p string, line int, format string, args ...any) {
		problems = append(problems, ScanError{Path: p, Line: line, Message: fmt.Sprintf(format, args...)})
	}

	tags, err := repo.Tags()
	if err != nil {
		return nil, nil, err
	}
	sessionNums := map[string]map[string]int{} // chapter -> session tag -> K
	for _, t := range tags {
		b.tags[t] = true
		if m := chapterTagRe.FindStringSubmatch(t); m != nil {
			b.chapter(m[1]).HasTag = true
		} else if m := sessionTagRe.FindStringSubmatch(t); m != nil {
			if sessionNums[m[1]] == nil {
				sessionNums[m[1]] = map[string]int{}
			}
			k, _ := strconv.Atoi(m[2]) // the pattern allows only digits
			sessionNums[m[1]][t] = k
			b.chapter(m[1]).Sessions = append(b.chapter(m[1]).Sessions, t)
		}
	}

	proseFiles, _ := fs.Glob(tree, "book/ch[0-9][0-9]-*.md")
	for _, p := range proseFiles {
		m := proseNameRe.FindStringSubmatch(path.Base(p))
		if m == nil {
			continue
		}
		c := b.chapter(m[1])
		if c.Prose == "" {
			c.Prose = p
		} else {
			c.Extra = append(c.Extra, p)
		}
	}
	listFiles, _ := fs.Glob(tree, "book/tests/*.list")
	for _, p := range listFiles {
		if m := listNameRe.FindStringSubmatch(path.Base(p)); m != nil {
			b.chapter(m[1]).List = p
		}
	}
	cardFiles, _ := fs.Glob(tree, "book/tests/*.tests")
	for _, p := range cardFiles {
		text, err := fs.ReadFile(tree, p)
		if err != nil {
			fail(p, 1, "cannot read the file: %v", err)
			continue
		}
		cards, errs := parseCards(p, text)
		problems = append(problems, errs...)
		if m := cardFileRe.FindStringSubmatch(path.Base(p)); m != nil {
			c := b.chapter(m[1])
			c.Tests, c.Cards = p, cards
		}
	}

	for nn, c := range b.chapters {
		for _, extra := range c.Extra {
			fail(extra, 1, "chapter %s has more than one prose file; the other is `%s`", nn, c.Prose)
		}
		c.CodeTag = codeTag(nn, c.HasTag, sessionNums[nn])
	}
	return b, problems, nil
}

// chapter returns the chapter with the number nn, creating it on first use.
func (b *book) chapter(nn string) *chapter {
	c := b.chapters[nn]
	if c == nil {
		c = &chapter{NN: nn}
		b.chapters[nn] = c
	}
	return c
}

// sortedChapters returns the chapters in the order of their numbers.
func (b *book) sortedChapters() []*chapter {
	list := make([]*chapter, 0, len(b.chapters))
	for _, c := range b.chapters {
		list = append(list, c)
	}
	slices.SortFunc(list, func(x, y *chapter) int { return strings.Compare(x.NN, y.NN) })
	return list
}

// codeTag chooses the tag that holds the code of chapter nn: its chapter tag if
// it exists and otherwise its last session tag, the one with the highest K (so
// ch02-s10 comes after ch02-s9). It returns "" when the chapter has neither.
func codeTag(nn string, hasTag bool, sessions map[string]int) string {
	if hasTag {
		return "ch" + nn
	}
	best, bestK := "", -1
	for t, k := range sessions {
		if k > bestK {
			best, bestK = t, k
		}
	}
	return best
}

// show returns the content of path in tag, asking git once.
func (b *book) show(tag, p string) ([]byte, error) {
	key := tag + ":" + p
	r, ok := b.shows[key]
	if !ok {
		r.data, r.err = b.repo.Show(tag, p)
		b.shows[key] = r
	}
	return r.data, r.err
}

// tagFiles returns the files of tag, asking git once.
func (b *book) tagFiles(tag string) ([]string, error) {
	if files, ok := b.files[tag]; ok {
		return files, nil
	}
	files, err := b.repo.Files(tag)
	if err != nil {
		return nil, err
	}
	b.files[tag] = files
	return files, nil
}

// hasFile reports whether tag has a file at path p.
func (b *book) hasFile(tag, p string) (bool, error) {
	files, err := b.tagFiles(tag)
	if err != nil {
		return false, err
	}
	return slices.Contains(files, p), nil
}

// scanTag reads every file of tag that can carry markers and returns its
// regions and the problems with its markers. A problem is reported with the
// path "<tag>:<file>", so the reader sees which tag it comes from. Region ids
// have to be unique in the whole tag, not only in a file.
func (b *book) scanTag(tag string) *tagScan {
	if s, ok := b.scans[tag]; ok {
		return s
	}
	s := &tagScan{}
	b.scans[tag] = s
	files, err := b.tagFiles(tag)
	if err != nil {
		s.errs = append(s.errs, ScanError{Path: tag, Line: 1, Message: fmt.Sprintf("cannot list the files: %v", err)})
		return s
	}
	first := map[string]string{} // region id -> "file:line" of its marker
	for _, f := range files {
		if !regionExts[path.Ext(f)] || slices.Contains(strings.Split(f, "/"), "testdata") {
			continue
		}
		text, err := b.show(tag, f)
		if err != nil {
			s.errs = append(s.errs, ScanError{Path: tag + ":" + f, Line: 1, Message: fmt.Sprintf("cannot read the file: %v", err)})
			continue
		}
		regions, errs := scanRegions(f, text)
		for _, e := range errs {
			e.Path = tag + ":" + e.Path
			s.errs = append(s.errs, e)
		}
		for _, r := range regions {
			here := fmt.Sprintf("%s:%d", f, r.FirstLine-1)
			if prev, dup := first[r.ID]; dup {
				s.errs = append(s.errs, ScanError{
					Path: tag + ":" + f, Line: r.FirstLine - 1,
					Message: fmt.Sprintf("duplicate region id `%s`, first defined at %s", r.ID, prev),
				})
			} else {
				first[r.ID] = here
			}
			s.regions = append(s.regions, r)
		}
	}
	sortScanErrors(s.errs)
	return s
}

// verify applies the seven rules of `bookx verify` to the book. It returns the
// problems sorted by file and line; an empty result means the book is sound.
// The error is for what stops the check altogether, such as a repository that
// cannot be read.
func verify(tree fs.FS, repo repoReader) ([]ScanError, error) {
	b, problems, err := loadBook(tree, repo)
	if err != nil {
		return nil, err
	}

	for _, c := range b.sortedChapters() {
		if !c.Active() {
			continue
		}
		if c.Prose != "" {
			_, ps := b.renderChapter(c)
			problems = append(problems, ps...)
			problems = append(problems, b.checkProse(c)...)
		}
		problems = append(problems, b.checkChapterFiles(c)...)
		problems = append(problems, b.checkCardsAndTests(c)...)
	}
	problems = append(problems, b.checkCitedTags()...)
	sortScanErrors(problems)
	return problems, nil
}

// checkProse applies rules 3 and 7 to the prose of a chapter: it has the
// {{cards}} directive and its containers do not nest.
func (b *book) checkProse(c *chapter) []ScanError {
	text, err := fs.ReadFile(b.tree, c.Prose)
	if err != nil {
		return []ScanError{{Path: c.Prose, Line: 1, Message: fmt.Sprintf("cannot read the file: %v", err)}}
	}
	var out []ScanError
	ds, _ := findDirectives(text) // its problems are reported by renderChapter
	if !slices.ContainsFunc(ds, func(d Directive) bool { return d.Kind == DirCards }) {
		out = append(out, ScanError{Path: c.Prose, Line: 1, Message: "the chapter has no {{cards}} directive"})
	}
	return append(out, checkContainers(c.Prose, splitLines(text))...)
}

// checkContainers applies rule 7: the `:::` containers of the prose are never
// nested, and each one is closed. Lines inside a fenced code block do not
// count. A `:::kind` line that appears while another container is open is a
// problem and does not open anything, so its own closing line is not needed.
func checkContainers(p string, lines []string) []ScanError {
	var out []ScanError
	var open fence
	var cur string // kind of the open container; "" when none
	curLine := 0
	for i, line := range lines {
		n := i + 1
		if open.char != 0 {
			if open.closedBy(line) {
				open = fence{}
			}
			continue
		}
		if f, ok := openingFence(line); ok {
			open = f
			continue
		}
		if m := containerOpenRe.FindStringSubmatch(line); m != nil {
			if cur != "" {
				out = append(out, ScanError{Path: p, Line: n, Message: "nested container :::" + m[1]})
			} else {
				cur, curLine = m[1], n
			}
		} else if strings.TrimSpace(line) == ":::" {
			cur = ""
		}
	}
	if cur != "" {
		out = append(out, ScanError{Path: p, Line: curLine, Message: "unclosed container"})
	}
	return out
}

// checkChapterFiles applies the file part of rule 6: a chapter that is tagged
// has its prose and its cards in the tag, and a chapter with prose or a tag
// has its list of tests in the working tree.
func (b *book) checkChapterFiles(c *chapter) []ScanError {
	var out []ScanError
	if c.HasTag {
		tag := "ch" + c.NN
		files, err := b.tagFiles(tag)
		if err != nil {
			return []ScanError{{Path: tag, Line: 1, Message: fmt.Sprintf("cannot list the files: %v", err)}}
		}
		if !slices.ContainsFunc(files, func(f string) bool {
			return path.Dir(f) == "book" && proseNameRe.MatchString(path.Base(f)) && strings.HasPrefix(path.Base(f), "ch"+c.NN+"-")
		}) {
			out = append(out, ScanError{Path: "book/ch" + c.NN + "-*.md", Line: 1, Message: fmt.Sprintf("tag `%s` has no prose for the chapter", tag)})
		}
		if !slices.Contains(files, "book/tests/ch"+c.NN+".tests") {
			out = append(out, ScanError{Path: "book/tests/ch" + c.NN + ".tests", Line: 1, Message: fmt.Sprintf("tag `%s` has no cards for the chapter", tag)})
		}
	}
	if c.List == "" {
		hint := "ci/check-tags.sh --list-as ch" + c.NN + " <last session tag>"
		out = append(out, ScanError{Path: "book/tests/ch" + c.NN + ".list", Line: 1, Message: "the working tree has no list of tests for the chapter; write it with " + hint})
	}
	if c.Tests == "" {
		out = append(out, ScanError{Path: "book/tests/ch" + c.NN + ".tests", Line: 1, Message: "the working tree has no cards for the chapter"})
	}
	return out
}

// checkCitedTags applies the rest of rule 6 and rule 2 to the tags that the
// prose cites: each one is in verified-tags.txt with the SHA of its commit, and
// its markers are balanced and unique.
func (b *book) checkCitedTags() []ScanError {
	var out []ScanError
	var verified []VerifiedTag
	text, err := fs.ReadFile(b.tree, verifiedTagsPath)
	if err == nil {
		verified, err = parseVerifiedTags(text)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		line := 1
		var le *lineError
		if errors.As(err, &le) {
			line = le.Line
			err = errors.New(le.Message)
		}
		out = append(out, ScanError{Path: verifiedTagsPath, Line: line, Message: err.Error()})
		verified = nil
	}

	cited := make([]string, 0, len(b.cited))
	for t := range b.cited {
		cited = append(cited, t)
	}
	slices.Sort(cited)
	for _, tag := range cited {
		at := b.cited[tag]
		i := slices.IndexFunc(verified, func(v VerifiedTag) bool { return v.Tag == tag })
		if i < 0 {
			out = append(out, ScanError{Path: at.Path, Line: at.Line, Message: fmt.Sprintf("tag `%s` is not in %s: run ci/check-tags.sh '%s'", tag, verifiedTagsPath, tag)})
		} else if sha, err := b.repo.Rev(tag); err != nil {
			out = append(out, ScanError{Path: at.Path, Line: at.Line, Message: fmt.Sprintf("cannot resolve tag `%s`: %v", tag, err)})
		} else if sha != verified[i].SHA {
			out = append(out, ScanError{Path: at.Path, Line: at.Line, Message: fmt.Sprintf("tag `%s` points to %s, but %s verified %s: run ci/check-tags.sh '%s'", tag, sha, verifiedTagsPath, verified[i].SHA, tag)})
		}
		out = append(out, b.scanTag(tag).errs...)
	}
	return out
}

// listEntry is a line of book/tests/chNN.list.
type listEntry struct {
	Pkg, Test string
	Line      int
}

// readList reads the list of tests of a chapter from the working tree. A file
// that is not there is an empty list. A line that is not `<package> <test>` is
// a problem.
func (b *book) readList(listPath string) ([]listEntry, []ScanError) {
	if listPath == "" {
		return nil, nil
	}
	text, err := fs.ReadFile(b.tree, listPath)
	if err != nil {
		return nil, []ScanError{{Path: listPath, Line: 1, Message: fmt.Sprintf("cannot read the file: %v", err)}}
	}
	var entries []listEntry
	var errs []ScanError
	for i, line := range splitLines(text) {
		f := strings.Fields(line)
		if len(f) != 2 {
			errs = append(errs, ScanError{Path: listPath, Line: i + 1, Message: "want `<package> <test>`"})
			continue
		}
		entries = append(entries, listEntry{Pkg: f[0], Test: f[1], Line: i + 1})
	}
	return entries, errs
}

// cardRefForTest is the ref that the card of a listed test carries: the name of
// the test, except for a case of the runner of golden files, TestKi/<dir>/<chNN>/<name>,
// whose card points to the file tests/<dir>/<chNN>/<name>.ki.
func cardRefForTest(test string) string {
	if rest, ok := strings.CutPrefix(test, "TestKi/"); ok {
		return "tests/" + rest + ".ki"
	}
	return test
}

// checkCardsAndTests applies rules 4 and 5 to a chapter: every card has its
// test in the code, every card that is not a benchmark has its test in the
// list, and every test that is new in the chapter has a card.
func (b *book) checkCardsAndTests(c *chapter) []ScanError {
	var out []ScanError
	if c.Tests == "" || c.List == "" {
		return nil // checkChapterFiles has reported what is missing
	}
	if c.CodeTag == "" {
		return []ScanError{{Path: c.Tests, Line: 1, Message: fmt.Sprintf("chapter %s has no tag `ch%s` and no session tag `ch%s-sK` to read its code from", c.NN, c.NN, c.NN)}}
	}
	for _, card := range c.Cards {
		if msg := b.checkCardRef(c.CodeTag, card); msg != "" {
			out = append(out, ScanError{Path: c.Tests, Line: card.Line, Message: fmt.Sprintf("card %s: %s", card.ID, msg)})
		}
	}

	list, errs := b.readList(c.List)
	out = append(out, errs...)
	var previous []listEntry
	if n, err := strconv.Atoi(c.NN); err == nil && n > 1 {
		prev := b.chapters[fmt.Sprintf("%02d", n-1)]
		if prev != nil {
			previous, _ = b.readList(prev.List) // its own check reports a bad line
		}
	}
	inPrevious := map[string]bool{}
	for _, e := range previous {
		inPrevious[e.Pkg+" "+e.Test] = true
	}
	refs := map[string]bool{}
	for _, card := range c.Cards {
		refs[card.Ref] = true
	}
	listed := map[string]bool{}
	for _, e := range list {
		listed[cardRefForTest(e.Test)] = true
	}

	for _, card := range c.Cards {
		if card.Kind != "bench" && !listed[card.Ref] {
			out = append(out, ScanError{Path: c.Tests, Line: card.Line, Message: fmt.Sprintf("card %s: its test `%s` is not in %s", card.ID, card.Ref, c.List)})
		}
	}
	for _, e := range list {
		if inPrevious[e.Pkg+" "+e.Test] || refs[cardRefForTest(e.Test)] {
			continue
		}
		if b.notInBook(c.CodeTag, e) {
			continue
		}
		out = append(out, ScanError{Path: c.List, Line: e.Line, Message: fmt.Sprintf("test `%s` of %s is new in the chapter and has no card with ref `%s`", e.Test, e.Pkg, cardRefForTest(e.Test))})
	}
	return out
}

// checkCardRef applies rule 4 to one card, reading the code in tag. It returns
// the message of the problem, or "" when the card's test is where it says.
func (b *book) checkCardRef(tag string, card Card) string {
	src, err := b.show(tag, card.File)
	if err != nil {
		ok, ferr := b.hasFile(tag, card.File)
		if ferr == nil && !ok {
			return fmt.Sprintf("file `%s` does not exist in tag `%s`", card.File, tag)
		}
		return fmt.Sprintf("cannot read `%s` in tag `%s`: %v", card.File, tag, err)
	}
	switch card.Kind {
	case "unit", "prop", "example", "bench":
		prefix := map[string]string{"unit": "Test", "prop": "Fuzz", "example": "Example", "bench": "Benchmark"}[card.Kind]
		name, sub, hasSub := strings.Cut(card.Ref, "/")
		if !strings.HasPrefix(name, prefix) {
			return fmt.Sprintf("ref `%s` of a `%s` card must start with `%s`", card.Ref, card.Kind, prefix)
		}
		if hasSub && card.Kind != "unit" {
			return fmt.Sprintf("ref `%s` of a `%s` card cannot name a subtest", card.Ref, card.Kind)
		}
		if !bytes.Contains(src, []byte("func "+name+"(")) {
			return fmt.Sprintf("`func %s(` is not in `%s` (tag `%s`)", name, card.File, tag)
		}
		if hasSub {
			for _, part := range strings.Split(sub, "/") {
				if !bytes.Contains(src, []byte(`"`+part+`"`)) {
					return fmt.Sprintf("the subtest name \"%s\" is not in `%s` (tag `%s`)", part, card.File, tag)
				}
			}
		}
	case "golden", "diag", "run":
		for _, line := range splitLines(src) {
			if !strings.HasPrefix(line, "//") {
				break // the header ends at the first line that is not a comment
			}
			if rest, ok := strings.CutPrefix(line, "// test: "); ok {
				if strings.TrimSpace(rest) != card.ID {
					return fmt.Sprintf("`%s` says `// test: %s`, not `%s`", card.File, strings.TrimSpace(rest), card.ID)
				}
				return ""
			}
		}
		return fmt.Sprintf("`%s` has no `// test: %s` header line", card.File, card.ID)
	default:
		panic("unreachable")
	}
	return ""
}

// moduleOf returns the module path declared by the go.mod of tag.
func (b *book) moduleOf(tag string) (string, error) {
	src, err := b.show(tag, "go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range splitLines(src) {
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", errors.New("go.mod has no module line")
}

// notInBook reports whether a listed test is declared in a file of the code
// tag that carries the not-in-book marker, so it needs no card. The file is
// found by looking for `func <Name>(` among the _test.go files of the test's
// package. A test whose file cannot be found is not excused.
func (b *book) notInBook(tag string, e listEntry) bool {
	module, err := b.moduleOf(tag)
	if err != nil {
		return false
	}
	dir := "."
	if e.Pkg != module {
		rest, ok := strings.CutPrefix(e.Pkg, module+"/")
		if !ok {
			return false
		}
		dir = rest
	}
	files, err := b.tagFiles(tag)
	if err != nil {
		return false
	}
	top, _, _ := strings.Cut(e.Test, "/")
	decl := []byte("func " + top + "(")
	for _, f := range files {
		if path.Dir(f) != dir || !strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := b.show(tag, f)
		if err != nil || !bytes.Contains(src, decl) {
			continue
		}
		return bytes.Contains(src, []byte(notInBookMarker))
	}
	return false
}
