package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// This file is `bookx lint`: the rules of sections 4 and 5 of
// docs/superpowers/process/prose.md that a machine can check. Two kinds of rule:
// an error is a limit that can be counted, and a warning comes from a list of
// words, where only a reader can tell a real case from a coincidence, so it
// never fails. The errors on the shape of a chapter (shapeRules: its sections
// and its theory half) fail every chapter; the limits of section 5 fail from
// chapter 11 on, and the chapters before it are only measured against them.

// The limits of a chapter (prose.md, section 5).
const (
	budgetWords   = 2500
	maxFolds      = 5
	maxFoldWords  = 150
	maxTitleWords = 8
	maxTableWords = 200
)

// chapterSections are the level-2 sections of every chapter, in the order of
// book/README.md: the theory half, then the practice half from practiceStart on.
var chapterSections = []string{"Objetivo", "Teoría", "Preguntas de repaso", "Resumen", "Go en el trabajo", "Tests primero", "Implementación", "Ejecútalo", "Ejercicios"}

// practiceStart is the section that opens the practice half of a chapter.
const practiceStart = "Tests primero"

// shapeRules are the rules on the shape of a chapter. Their errors fail every
// chapter, not only those from firstStrictChapter on.
var shapeRules = map[string]bool{"sections": true, "theory-half": true}

// theoryFences are the languages of the blocks that the theory half never
// holds, and theoryDirectives the directives: Go code, commands and their output.
var (
	theoryFences     = map[string]bool{"go": true, "console": true}
	theoryDirectives = map[string]bool{"file": true, "diff": true, "listing": true, "reftest": true, "cards": true}
)

// Severities of a finding of the lint.
const (
	lintError   = "error"
	lintWarning = "warning"
)

// lintIssue is one finding of the lint in the prose of a chapter.
type lintIssue struct {
	Line     int
	Severity string
	Rule     string
	Detail   string
}

// String formats the finding without its file, as `<line>: <severity> <rule>: <detail>`.
func (i lintIssue) String() string {
	return fmt.Sprintf("%d: %s %s: %s", i.Line, i.Severity, i.Rule, i.Detail)
}

// lintCounts are the numbers of a chapter, the measure that the lint gives even
// when no rule fails.
type lintCounts struct {
	Flow           int // flow words, as `bookx words` counts them
	Folds          int
	FoldWords      int
	TableWords     int // words in open tables
	Whys           int // cards with a `why`
	WhysOverTarget int // over whyTargetWords
	WhysOverLimit  int // over whyMaxWords
}

// lintResult is what the lint says of a chapter.
type lintResult struct {
	Issues []lintIssue // sorted by line
	Counts lintCounts
}

var (
	// genderRe is the words of prose.md that give the reader a gender, plus the
	// reader as a noun. The noun «lista» is everywhere in a book about compilers
	// and says nothing about anybody, so only the masculine `listo` is here.
	genderRe = regexp.MustCompile(`(?i)\b(el lector|la lectora|los lectores|listos?|preparad[oa]s?|cansad[oa]s?|atascad[oa]s?|perdid[oa]s?|bienvenid[oa]s?|nuev[oa]s? en go|segur[oa]s? de|convencid[oa]s?|acostumbrad[oa]s?)\b`)
	// jargonRe is the words of the plan and of its reviews, which mean nothing to
	// the reader. «Hito M1» is the book's own word, so it is not here.
	jargonRe = regexp.MustCompile(`\b(Tarea \d+|Task \d+|Plan \d+|el plan|del plan|spec \d+|el spec|del spec|ledger|controlador|panel|lentes?|rulings?)\b`)
	// testNameRe is the name of a test function.
	testNameRe = regexp.MustCompile(`\b(?:Test|Fuzz|Benchmark|Example)[A-Z]\w*`)
	// cardIDInTextRe is a card id in the prose.
	cardIDInTextRe = regexp.MustCompile(`\bT\d{2}\.\d{2,3}\b`)
	// sessionStepRe starts a numbered step.
	sessionStepRe = regexp.MustCompile(`^\s*\d+[.)]\s`)
	// commandSpanRe is a code span that holds a command the reader runs: its test
	// names are arguments, and the rule keeps them.
	commandSpanRe = regexp.MustCompile("`(?:go|ci/|build/bin/|ki|dlv|git)[ /a-z][^`]*`")
)

// cardNamesRe matches, in a line of the prose, the name of a card or of the
// subtest it points to. Names without an underscore are ordinary words, and they
// are left out. It is nil when no card has such a name. The longest name goes
// first, so `lexer.pipe_line` is found as itself and not as `pipe_line`.
func cardNamesRe(cards []Card) *regexp.Regexp {
	var names []string
	for _, c := range cards {
		for _, n := range []string{c.Name, c.Ref[strings.LastIndex(c.Ref, "/")+1:]} {
			if strings.Contains(n, "_") && !strings.Contains(n, ".ki") && !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	slices.SortFunc(names, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	for i, n := range names {
		names[i] = regexp.QuoteMeta(n)
	}
	return regexp.MustCompile(`\b(?:` + strings.Join(names, "|") + `)\b`)
}

// lintChapter applies the rules to the Markdown of a chapter and its cards.
func lintChapter(markdown string, cards []Card) lintResult {
	var r lintResult
	add := func(line int, severity, rule, format string, args ...any) {
		r.Issues = append(r.Issues, lintIssue{Line: line, Severity: severity, Rule: rule, Detail: fmt.Sprintf(format, args...)})
	}
	names := cardNamesRe(cards)

	r.Counts.Flow = flowWords([]byte(markdown))
	if r.Counts.Flow > budgetWords {
		add(1, lintError, "budget", "%d flow words; the budget is %d", r.Counts.Flow, budgetWords)
	}

	firstTable, sixth := 0, 0
	foldLine, foldTitle, foldWords := 0, "", 0
	closeFold := func() {
		if foldLine != 0 && foldWords > maxFoldWords {
			add(foldLine, lintError, "fold-words", "`%s` has %d words; at most %d", foldTitle, foldWords, maxFoldWords)
		}
		r.Counts.FoldWords += foldWords
		foldLine, foldTitle, foldWords = 0, "", 0
	}
	for _, l := range proseLines([]byte(markdown)) {
		switch l.Kind {
		case lineContainerOpen:
			if l.Container == "more" {
				foldLine, foldTitle = l.Number, l.Title
				r.Counts.Folds++
				if r.Counts.Folds == maxFolds+1 {
					sixth = l.Number
				}
				lintFoldTitle(l, add)
				lintWordLists(l.Number, l.Title, add)
			}
		case lineContainerClose:
			closeFold()
		case lineText:
			if l.Container == "more" {
				foldWords += wordsIn(strings.TrimSpace(l.Text))
			}
			if isDirectiveLine(l.Text) {
				continue
			}
			if isTableLine(l.Text) && !excludedKinds[l.Container] {
				r.Counts.TableWords += wordsIn(strings.TrimSpace(l.Text))
				if firstTable == 0 {
					firstTable = l.Number
				}
			}
			lintWordLists(l.Number, l.Text, add)
			lintNames(l, names, add)
		}
	}
	closeFold()
	if sixth != 0 {
		add(sixth, lintError, "fold-count", "%d folds; at most %d", r.Counts.Folds, maxFolds)
	}
	if r.Counts.TableWords > maxTableWords {
		add(firstTable, lintError, "open-tables", "%d words in open tables; at most %d", r.Counts.TableWords, maxTableWords)
	}

	for _, c := range cards {
		n := wordsIn(c.Why)
		r.Counts.Whys++
		if n > whyTargetWords {
			r.Counts.WhysOverTarget++
		}
		if n > whyMaxWords {
			r.Counts.WhysOverLimit++
		}
	}
	lintShape(markdown, add)
	slices.SortStableFunc(r.Issues, func(a, b lintIssue) int { return a.Line - b.Line })
	return r
}

// lintShape checks the shape of a chapter (prose.md, section 4): its level-2
// sections are those of book/README.md, each once and in their order, and the
// theory half, everything before `## Tests primero`, holds no Go block, no
// console block and no directive of code. Only the headings of the running text
// are sections: one inside a container or a fence is not. A heading out of
// order is named with the first section of README that it should precede. A
// missing section is reported on the line of the next section it should
// precede, or after the last one there is. Without `## Tests primero` the
// theory half has no end, and only the missing section is reported.
func lintShape(markdown string, add lintAdd) {
	lines := proseLines([]byte(markdown))
	order := map[string]int{}
	for i, s := range chapterSections {
		order[s] = i
	}
	at := map[string]int{} // line of the first heading of each section
	highest := -1          // the furthest section in order seen so far
	practice := 0          // line of `## Tests primero`; 0 until it is found
	for _, l := range lines {
		title, ok := strings.CutPrefix(l.Text, "## ")
		if l.Kind != lineText || l.Container != "" || !ok {
			continue
		}
		title = strings.TrimSpace(title)
		i, known := order[title]
		switch {
		case !known:
			add(l.Number, lintError, "sections", "`## %s` is not a section of book/README.md", title)
			continue
		case at[title] != 0:
			add(l.Number, lintError, "sections", "`## %s` is repeated: the first is on line %d", title, at[title])
			continue
		}
		at[title] = l.Number
		if title == practiceStart {
			practice = l.Number
		}
		if i < highest {
			next := ""
			for _, s := range chapterSections[i+1:] {
				if at[s] != 0 {
					next = s
					break
				}
			}
			add(l.Number, lintError, "sections", "`## %s` is out of order: book/README.md puts it before `## %s`", title, next)
			continue
		}
		highest = i
	}
	last := 1
	if len(lines) > 0 {
		last = lines[len(lines)-1].Number
	}
	for i, s := range chapterSections {
		if at[s] != 0 {
			continue
		}
		before, after := "", ""
		for _, n := range chapterSections[i+1:] {
			if at[n] != 0 {
				before = n
				break
			}
		}
		for _, p := range slices.Backward(chapterSections[:i]) {
			if at[p] != 0 {
				after = p
				break
			}
		}
		switch {
		case before != "":
			add(at[before], lintError, "sections", "`## %s` is missing: it goes before `## %s`", s, before)
		case after != "":
			add(last, lintError, "sections", "`## %s` is missing: it goes after `## %s`", s, after)
		default:
			add(1, lintError, "sections", "`## %s` is missing", s)
		}
	}

	if practice == 0 {
		return
	}
	const where = "before `## " + practiceStart + "`: Go code, commands and code directives go in the practice half"
	for _, l := range lines {
		if l.Number >= practice {
			break
		}
		switch {
		case l.Kind == lineFenceOpen:
			if f := strings.Fields(l.FenceInfo); len(f) > 0 && theoryFences[f[0]] {
				add(l.Number, lintError, "theory-half", "a ```%s block %s", f[0], where)
			}
		case l.Kind == lineText && isDirectiveLine(l.Text):
			text := strings.TrimSpace(l.Text)
			if f := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(text, "{{"), "}}")); len(f) > 0 && theoryDirectives[f[0]] {
				add(l.Number, lintError, "theory-half", "`%s` %s", text, where)
			}
		}
	}
}

// lintAdd is how a rule reports a finding.
type lintAdd func(line int, severity, rule, format string, args ...any)

// lintFoldTitle checks that the title of a fold names one topic in a few words.
// A comma or «y más» is a list. A plain «y» may be a list or one topic («ida y
// vuelta»), so it is only a warning. Code spans can hold anything.
func lintFoldTitle(l proseLine, add lintAdd) {
	words := withoutCodeSpans(l.Title)
	switch {
	case strings.Contains(words, ","):
		add(l.Number, lintError, "fold-title", "`%s` lists topics (a comma)", l.Title)
	case strings.Contains(" "+words+" ", " y más "):
		add(l.Number, lintError, "fold-title", "`%s` lists topics (y más)", l.Title)
	case strings.Contains(" "+words+" ", " y "):
		add(l.Number, lintWarning, "fold-title", "`%s` may join two topics (y)", l.Title)
	}
	if n := wordsIn(l.Title); n > maxTitleWords {
		add(l.Number, lintError, "fold-title", "`%s` has %d words; a title names one topic in at most %d", l.Title, n, maxTitleWords)
	}
}

// lintWordLists looks for the words that no text of the book should use. Code
// spans are not text.
func lintWordLists(line int, text string, add lintAdd) {
	words := withoutCodeSpans(text)
	for _, m := range genderRe.FindAllString(words, -1) {
		add(line, lintWarning, "gender", "`%s`", m)
	}
	for _, m := range jargonRe.FindAllString(words, -1) {
		add(line, lintWarning, "jargon", "`%s`", m)
	}
}

// lintNames looks for test names and card ids in running text. The solutions and
// the answers are consulted and not read in the flow, so the rule leaves them. A
// card id is fine on a numbered step of a session: that is where the reader is
// told which cards to write.
func lintNames(l proseLine, names *regexp.Regexp, add lintAdd) {
	if l.Container == "solution" || l.Container == "answer" {
		return
	}
	text := commandSpanRe.ReplaceAllString(l.Text, " ")
	for _, m := range testNameRe.FindAllString(text, -1) {
		if !strings.HasSuffix(m, "Xxx") {
			add(l.Number, lintWarning, "test-name", "`%s`", m)
		}
	}
	if names != nil {
		for _, m := range names.FindAllString(text, -1) {
			add(l.Number, lintWarning, "test-name", "`%s`", m)
		}
	}
	if l.Section == "Implementación" && l.Session != "" && l.Container == "" && sessionStepRe.MatchString(l.Text) {
		return
	}
	for _, m := range cardIDInTextRe.FindAllString(text, -1) {
		add(l.Number, lintWarning, "card-id", "`%s`", m)
	}
}

// runLint is `bookx lint <file>...`: it prints each finding as
// `<file>:<line>: <severity> <rule>: <detail>` and one line of counts per file.
// The cards of a chapter are read from tests/chNN.tests next to its prose. The
// exit code is 1 when a chapter has an error of shapeRules, a chapter from
// firstStrictChapter on has any error, or a file cannot be read.
func runLint(args []string, stdout, stderr io.Writer) int {
	_, files, ok := parseArgs("lint", args, -1, false, stderr)
	if !ok {
		return exitUsage
	}
	code := exitOK
	for _, f := range files {
		text, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(stderr, "bookx lint: %v\n", err)
			code = exitFail
			continue
		}
		nn := ""
		if m := proseNameRe.FindStringSubmatch(filepath.Base(f)); m != nil {
			nn = m[1]
		}
		cardsPath := filepath.Join(filepath.Dir(f), "tests", "ch"+nn+".tests")
		var cards []Card
		if data, err := os.ReadFile(cardsPath); err == nil {
			var errs []ScanError
			cards, errs = parseCards(cardsPath, data)
			for _, e := range errs {
				fmt.Fprintln(stderr, e)
				code = exitFail
			}
		}
		r := lintChapter(string(text), cards)
		errorCount, shapeErrors, warnings := 0, 0, 0
		for _, i := range r.Issues {
			fmt.Fprintf(stdout, "%s:%s\n", f, i)
			if i.Severity == lintError {
				errorCount++
				if shapeRules[i.Rule] {
					shapeErrors++
				}
			} else {
				warnings++
			}
		}
		strict := strictChapter(nn)
		if strict {
			for _, c := range cards {
				if n := wordsIn(c.Why); n > whyTargetWords {
					fmt.Fprintf(stdout, "%s:%d: %s why-target: card %s: its `why` has %d words; the target is %d\n", cardsPath, c.Line, lintWarning, c.ID, n, whyTargetWords)
					warnings++
				}
			}
		}
		c := r.Counts
		tail := ""
		if !strict {
			tail = fmt.Sprintf(" (before chapter %d only sections and theory-half fail)", firstStrictChapter)
		}
		fmt.Fprintf(stdout, "lint %s: flow %d/%d, folds %d/%d (%d words), open tables %d/%d, whys %d (over %d: %d, over %d: %d), %d errors, %d warnings%s\n",
			f, c.Flow, budgetWords, c.Folds, maxFolds, c.FoldWords, c.TableWords, maxTableWords, c.Whys, whyTargetWords, c.WhysOverTarget, whyMaxWords, c.WhysOverLimit, errorCount, warnings, tail)
		if (strict && errorCount > 0) || shapeErrors > 0 {
			code = exitFail
		}
	}
	return code
}
