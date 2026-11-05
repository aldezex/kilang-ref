package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
)

// This file is `bookx cascade`: it breaks a correct program one token at a time,
// runs `ki check` on each result, and reports when one mistake costs more than it
// should. The report of the speed sweep asks for it because the worst diagnostics
// found so far were cascades, and they took a reading of the whole chapter to see.
//
// Each edit is tried three ways, because "many errors" alone says little:
//
//   - grows: with correct functions and statements added after the edit, the
//     errors go on growing. One mistake then costs as many errors as the rest of
//     the file has lines, up to the cap of the sink, which hides all the others.
//   - correct-text: an error at a line that was added and that depends on nothing
//     else, so it points at text that is not wrong. The added line next to the edit
//     does not count: a missing token is noticed at the next one, wherever it is.
//   - far: the edit got its error, and another one is far from it. It may be correct
//     text or the mistake's own consequence: delete a `fn`, and the error is where it
//     was called too. Only a person can tell, so it is listed and it fails nothing. A
//     lone error far from the edit is not even listed: rename a function, and the
//     error is where it is called.
//   - crash: ki does not answer with 0 or 1, or the time or the memory ran out.
//
// The exit code says whether something grows, points at correct text or crashes, so
// a program that exits 1 has something new to look at. Not the accepted limit: a
// missing `}` costs one E0201 for each later line that starts an item (see
// braceLimit), which is counted apart until the parser ends a block at an item.
//
// bookx imports only the standard library, so its tokens are its own: this reads
// source well enough to choose what to break, and never decides what is valid.

// kiToken is a token of a Ki program.
type kiToken struct {
	Text      string
	Offset    int
	Line, Col int // both 1-based, and Col counts bytes
}

// kiTokenRe takes one token, a run of blanks or a comment from the start of a text.
var kiTokenRe = regexp.MustCompile(`(?s)\A(?:\s+|//[^\n]*|/\*.*?\*/|"(?:[^"\\\n]|\\.)*"|[A-Za-z_]\w*|\d[\d_]*|->|\|>|==|!=|&&|\|\||<=|>=|.)`)

// tokenizeKi returns the tokens of a program, without blanks or comments.
func tokenizeKi(src string) []kiToken {
	var out []kiToken
	line, lineStart := 1, 0
	for at := 0; at < len(src); {
		text := kiTokenRe.FindString(src[at:])
		tok := kiToken{Text: text, Offset: at, Line: line, Col: at - lineStart + 1}
		if strings.TrimSpace(text) != "" && !strings.HasPrefix(text, "//") && !strings.HasPrefix(text, "/*") {
			out = append(out, tok)
		}
		for i, c := range text {
			if c == '\n' {
				line++
				lineStart = at + i + 1
			}
		}
		at += len(text)
	}
	return out
}

// cascadeEdit is one program with one edit.
type cascadeEdit struct {
	Kind      string // del, rep or ins
	Token     string // the token it edits
	Text      string // what it replaces it with, or inserts before it
	Line, Col int    // of the token
	Source    string
}

// String is how a report names the edit.
func (e cascadeEdit) String() string {
	if e.Kind == "del" {
		return fmt.Sprintf("del `%s`", e.Token)
	}
	return fmt.Sprintf("%s `%s` -> `%s`", e.Kind, e.Token, e.Text)
}

// cascadeEdits returns the programs that one edit makes out of src: each token
// deleted, replaced by each word of the vocabulary, and each word inserted before
// it. A text that comes up twice is kept the first time, and src itself never.
func cascadeEdits(src string, vocabulary []string) []cascadeEdit {
	var out []cascadeEdit
	seen := map[string]bool{src: true}
	add := func(t kiToken, kind, text, source string) {
		if !seen[source] {
			seen[source] = true
			out = append(out, cascadeEdit{Kind: kind, Token: t.Text, Text: text, Line: t.Line, Col: t.Col, Source: source})
		}
	}
	for _, t := range tokenizeKi(src) {
		before, after := src[:t.Offset], src[t.Offset+len(t.Text):]
		// A deleted token leaves spaces, so what follows keeps its column.
		add(t, "del", "", before+strings.Repeat(" ", len(t.Text))+after)
		for _, v := range vocabulary {
			add(t, "rep", v, before+v+after)
			add(t, "ins", v, before+v+" "+t.Text+after)
		}
	}
	return out
}

// cascadePadded returns the program with k correct functions at its end and, when
// withStatements, k correct statements after line editLine. isPad tells whether a
// line of the result is one of those. The functions and the statements use nothing
// outside themselves, so no edit elsewhere can make them wrong.
func cascadePadded(src string, editLine, k int, withStatements bool) (text string, isPad func(int) bool) {
	lines := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	pad := map[int]bool{}
	var out []string
	for i, l := range lines {
		out = append(out, l)
		if withStatements && i+1 == editLine {
			for j := 0; j < k; j++ {
				out = append(out, fmt.Sprintf("  let bookxLet%d = %d", j, j))
				pad[len(out)] = true
			}
		}
	}
	for j := 0; j < k; j++ {
		out = append(out, "")
		for _, l := range []string{
			fmt.Sprintf("fn bookxPad%d(a: Int) -> Int {", j),
			fmt.Sprintf("  let b = a + %d", j),
			"  let c = b * 2",
			"  c - b",
			"}",
		} {
			out = append(out, l)
			pad[len(out)] = true
		}
	}
	return strings.Join(out, "\n") + "\n", func(line int) bool { return pad[line] }
}

// kiDiagnostic is what the report needs of a line of `--diagnostics=json`.
type kiDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Help     string `json:"help"` // ki writes null when it has none
}

// parseKiDiagnostics returns the errors in the output of ki. A line that is not
// JSON is not a diagnostic.
func parseKiDiagnostics(output []byte) []kiDiagnostic {
	var out []kiDiagnostic
	for _, line := range strings.Split(string(output), "\n") {
		var d kiDiagnostic
		if json.Unmarshal([]byte(line), &d) == nil && d.Severity == "error" {
			out = append(out, d)
		}
	}
	return out
}

// braceLimit returns what tells apart the errors of the accepted limit, in the
// diagnostics of one run on text: internal/syntax/parser_stmt.go documents that a
// block with no `}` reads what follows as statements, so each later line that starts
// an item is an E0201 that found its first token, and the E0202 comes last. So an
// error of the limit needs that E0202, and it is an E0201 about the token that starts
// its line. Past diag.MaxErrors the E0202 is lost and nothing is excused: the program
// would need a hundred items, and then a failure is the right answer.
func braceLimit(ds []kiDiagnostic, text string) func(kiDiagnostic) bool {
	open := slices.ContainsFunc(ds, func(d kiDiagnostic) bool {
		return d.Code == "E0202" && strings.Contains(d.Help, "`}`")
	})
	lines := strings.Split(text, "\n")
	return func(d kiDiagnostic) bool {
		if !open || d.Code != "E0201" || d.Line < 1 || d.Line > len(lines) {
			return false
		}
		line := strings.TrimLeft(lines[d.Line-1], " \t")
		return slices.ContainsFunc([]string{"fn", "pub", "#"}, func(word string) bool {
			return strings.HasPrefix(line, word) && strings.HasSuffix(d.Message, "found `"+word+"`")
		})
	}
}

// Variables so a test can shorten them. The vocabulary is what breaks a program
// most: the delimiters, the marks that start and continue an expression, what
// starts a statement, and a value and a name.
var (
	cascadeVocabulary     = []string{"(", ")", "{", "}", ",", "=", "let", "1", "x"}
	cascadeTimeoutSeconds = "10"
)

// The pads of the two runs that look for growth.
const (
	cascadePadsFirst  = 4
	cascadePadsSecond = 8
)

// cascadeFinding is one thing to report.
type cascadeFinding struct {
	Kind   string
	Line   int
	Col    int
	Detail string
}

// cascadeReport is what a hunt found in one program.
type cascadeReport struct {
	Mutants, Valid, WithErrors int
	KnownLimit                 int // mutants that hit the accepted limit of braceLimit
	Histogram                  map[int]int
	Findings                   []cascadeFinding
}

// count returns how many findings are of a kind.
func (r *cascadeReport) count(kind string) int {
	n := 0
	for _, f := range r.Findings {
		if f.Kind == kind {
			n++
		}
	}
	return n
}

// histogramText shows how many errors the mutants got: `1: 500, 2: 30`.
func (r *cascadeReport) histogramText() string {
	if len(r.Histogram) == 0 {
		return "none"
	}
	keys := make([]int, 0, len(r.Histogram))
	for k := range r.Histogram {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d: %d", k, r.Histogram[k]))
	}
	return strings.Join(parts, ", ")
}

// cascadeHunt is one run of the command.
type cascadeHunt struct {
	ctx     context.Context
	ki      string
	scratch string
	runs    int
	okLines map[int]bool // by line of the program: statements can be added after it
}

// check runs ki on a text and returns its errors and its exit code.
func (h *cascadeHunt) check(text string) ([]kiDiagnostic, int, error) {
	p := filepath.Join(h.scratch, "p.ki")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		return nil, 0, err
	}
	h.runs++
	out, exit, err := cappedRun{
		Unit:    fmt.Sprintf("ki-cascade-%d-%d", os.Getpid(), h.runs),
		Dir:     h.scratch,
		Env:     cleanEnv(),
		Seconds: cascadeTimeoutSeconds,
		Argv:    []string{h.ki, "check", "--diagnostics=json", p},
	}.run(h.ctx)
	if err != nil {
		return nil, 0, err
	}
	return parseKiDiagnostics(out), exit, nil
}

// statementsFit reports whether statements can go after a line of the program:
// the program stays valid with them. It asks once per line.
func (h *cascadeHunt) statementsFit(src string, line int) (bool, error) {
	if ok, seen := h.okLines[line]; seen {
		return ok, nil
	}
	text, _ := cascadePadded(src, line, 1, true)
	ds, exit, err := h.check(text)
	if err != nil {
		return false, err
	}
	h.okLines[line] = len(ds) == 0 && exit == 0
	return h.okLines[line], nil
}

// program hunts in one program. An error here is for what stops the hunt.
func (h *cascadeHunt) program(src string, limit int) (*cascadeReport, error) {
	h.okLines = map[int]bool{}
	ds, exit, err := h.check(src)
	if err != nil {
		return nil, err
	}
	if len(ds) > 0 || exit != 0 {
		return nil, fmt.Errorf("the program has %d errors before any edit", len(ds))
	}
	edits := cascadeEdits(src, cascadeVocabulary)
	if limit > 0 && len(edits) > limit {
		step := float64(len(edits)) / float64(limit)
		sample := make([]cascadeEdit, 0, limit)
		for i := 0; i < limit; i++ {
			sample = append(sample, edits[int(float64(i)*step)])
		}
		edits = sample
	}
	// The last line with text. ki puts the end of the file on the line after a final
	// newline, so counting that one too would let an error at the end pass as far.
	lastLine := strings.Count(strings.TrimRight(src, "\n"), "\n") + 1
	r := &cascadeReport{Mutants: len(edits), Histogram: map[int]int{}}
	for _, e := range edits {
		if err := h.edit(src, e, lastLine, r); err != nil {
			return nil, err
		}
		if err := h.ctx.Err(); err != nil {
			return nil, fmt.Errorf("interrupted")
		}
	}
	return r, nil
}

// edit tries one edit and adds what it finds to r.
func (h *cascadeHunt) edit(src string, e cascadeEdit, lastLine int, r *cascadeReport) error {
	add := func(kind, format string, args ...any) {
		r.Findings = append(r.Findings, cascadeFinding{Kind: kind, Line: e.Line, Col: e.Col, Detail: fmt.Sprintf(format, args...)})
	}
	base, exit, err := h.check(e.Source)
	if err != nil {
		return err
	}
	if exit != 0 && exit != 1 {
		add("crash", "%s: exit %d", e, exit)
		return nil
	}
	if len(base) == 0 {
		r.Valid++
	} else {
		r.WithErrors++
		r.Histogram[len(base)]++
		limit := braceLimit(base, e.Source)
		if slices.ContainsFunc(base, func(d kiDiagnostic) bool { return nearLine(d.Line, e.Line) }) {
			// The mistake has its error. Is there another, far from it? Past the last
			// line is the end of the file, where a delimiter left open belongs.
			if i := slices.IndexFunc(base, func(d kiDiagnostic) bool { return !nearLine(d.Line, e.Line) && d.Line <= lastLine && !limit(d) }); i >= 0 {
				add("far", "%s: an error at %d:%d, and the edit is on line %d", e, base[i].Line, base[i].Col, e.Line)
			}
		}
	}

	fit, err := h.statementsFit(src, e.Line)
	if err != nil {
		return err
	}
	text, isPad := cascadePadded(e.Source, e.Line, cascadePadsFirst, fit)
	first, exit, err := h.check(text)
	if err != nil {
		return err
	}
	if exit != 0 && exit != 1 {
		add("crash", "%s with correct text added: exit %d", e, exit)
		return nil
	}
	// A mutant on the limit has it here too: the added functions are items after it.
	limit := braceLimit(first, text)
	if slices.ContainsFunc(first, limit) {
		r.KnownLimit++
	}
	wrongOnAdded := func(d kiDiagnostic, limit func(kiDiagnostic) bool) bool {
		return isPad(d.Line) && !nearLine(d.Line, e.Line) && !limit(d)
	}
	if i := slices.IndexFunc(first, func(d kiDiagnostic) bool { return wrongOnAdded(d, limit) }); i >= 0 {
		add("correct-text", "%s: an error on an added correct line: %s %s", e, first[i].Code, first[i].Message)
	}
	if len(first) <= len(base) {
		return nil
	}
	text, isPad = cascadePadded(e.Source, e.Line, cascadePadsSecond, fit)
	second, exit, err := h.check(text)
	if err != nil {
		return err
	}
	if exit != 0 && exit != 1 {
		add("crash", "%s with correct text added: exit %d", e, exit)
		return nil
	}
	if len(second) > len(first) {
		limit := braceLimit(second, text)
		i := slices.IndexFunc(second, func(d kiDiagnostic) bool { return wrongOnAdded(d, limit) })
		if i < 0 && slices.ContainsFunc(second, func(d kiDiagnostic) bool { return isPad(d.Line) && limit(d) }) {
			return nil // what grew is the limit, one error per added item
		}
		cause := "none on the added lines"
		if i >= 0 {
			cause = second[i].Code + " " + second[i].Message
		}
		add("grows", "%s: %d diagnostics, then %d, then %d, with 0, %d and %d correct additions after it: %s", e, len(base), len(first), len(second), cascadePadsFirst, cascadePadsSecond, cause)
	}
	return nil
}

// nearLine reports whether a diagnostic's line is the edit's, or next to it: a
// token that is missing is often noticed at the next one.
func nearLine(line, edit int) bool { return line >= edit-1 && line <= edit+1 }

// runCascade is `bookx cascade [--ki BIN] [--limit N] [--top N] <file.ki>...`: it
// prints up to --top findings of each kind per program and a line of counts. The
// exit code is 1 when something grows, points at correct text or crashes, or when it
// could not do its work; `far` and the accepted limit only get their counts.
func runCascade(args []string, stdout, stderr io.Writer) int {
	ki, limit, top := "build/bin/ki", 0, 20
	_, files, ok := parseArgs("cascade", args, -1, false, stderr, func(f *flag.FlagSet) {
		f.StringVar(&ki, "ki", ki, "the ki binary")
		f.IntVar(&limit, "limit", 0, "at most this many mutants per program, evenly spaced")
		f.IntVar(&top, "top", top, "at most this many findings of each kind per program")
	})
	if !ok {
		return exitUsage
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "bookx cascade: %v\n", err)
		return exitFail
	}
	if _, err := capCommand("ki-cascade-check", []string{"true"}); err != nil {
		return fail(err)
	}
	abs, err := filepath.Abs(ki)
	if err != nil {
		return fail(err)
	}
	scratch, done, err := newScratch(".", "cascade")
	if err != nil {
		return fail(err)
	}
	defer done()
	if scratch, err = filepath.Abs(scratch); err != nil {
		return fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	h := &cascadeHunt{ctx: ctx, ki: abs, scratch: scratch}

	code := exitOK
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(stderr, "bookx cascade: %v\n", err)
			code = exitFail
			continue
		}
		r, err := h.program(string(data), limit)
		if err != nil {
			fmt.Fprintf(stderr, "bookx cascade: %s: %v\n", f, err)
			code = exitFail
			continue
		}
		for _, kind := range []string{"grows", "correct-text", "far", "crash"} {
			shown := 0
			for _, finding := range r.Findings {
				if finding.Kind != kind {
					continue
				}
				if shown < top {
					fmt.Fprintf(stdout, "%s:%d:%d: %s: %s\n", f, finding.Line, finding.Col, kind, finding.Detail)
				}
				shown++
			}
			if shown > top {
				fmt.Fprintf(stdout, "%s: and %s more of %s\n", f, fmt.Sprint(shown-top), kind)
			}
		}
		fmt.Fprintf(stdout, "cascade %s: %d mutants, %d still valid, %d with errors (%s), %d grow, %d at correct text, %d far from the edit, %d on the known limit, %d crash\n",
			f, r.Mutants, r.Valid, r.WithErrors, r.histogramText(), r.count("grows"), r.count("correct-text"), r.count("far"), r.KnownLimit, r.count("crash"))
		if r.count("grows")+r.count("correct-text")+r.count("crash") > 0 {
			code = exitFail
		}
	}
	return code
}
