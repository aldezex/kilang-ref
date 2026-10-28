package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/driver"
)

// The catalog of docs/errors holds one file for each diagnostic code, with an
// explanation, an example that fails with that code and one that is corrected.
// TestErrorDocs keeps it honest: every example runs through the compiler, and
// the files and the codes of the source match in both directions, so a code
// built with diag.E or diag.W cannot be added without its entry. The source is
// read as Go, and a call of either whose argument is not an integer literal
// fails the test, because it names no code that can be read.
//
// A file has this shape (the headings and the words of the fence are what the
// test reads):
//
//	# E0301: unknown name
//
//	An explanation, in English, of the rule.
//
//	## Erroneous example
//
//	```ki erroneous
//	fn main() { prnt(1) }
//	```
//
//	What the compiler says about it.
//
//	## Corrected
//
//	```ki corrected
//	fn main() { print(1) }
//	```
//
// A file may have several erroneous examples, each under its own heading
// "## Erroneous example", and the same for "## Corrected". After `ki` and the
// kind of the example, the fence may carry these words, in any order:
//
//   - escaped: the block is one Go string literal between double quotes, which
//     the test decodes with strconv.Unquote. It is how a file writes the bytes
//     that a text file of the repository cannot hold (an invalid UTF-8 byte, a
//     control character).
//   - run: the example is run with `ki run` and not checked with `ki check`.
//   - also=E0201,E0202: the codes of the other diagnostics that the example
//     reports besides the code of its file. They are the mistakes that come with
//     it while the language is still growing, such as a string literal, which
//     the parser cannot yet use in an expression (E0201). Each code listed must
//     really appear, so the word is removed from the file as soon as the
//     compiler stops reporting it.
//
// An erroneous example reports at least one diagnostic with the code of its file
// and no other code than the ones its also lists. A corrected example reports
// none with that code and, without the word also, nothing at all: it exits with
// 0 and writes nothing to stderr.

// docFileName is the name of a file of the catalog: the code and ".md".
var docFileName = regexp.MustCompile(`^([EW][0-9]{4})\.md$`)

// example is one fenced block of a file of the catalog.
type example struct {
	kind    string   // "erroneous" or "corrected"
	heading string   // the "## " heading above the block
	line    int      // the line of the file where the fence opens
	escaped bool     // the block is a Go string literal
	run     bool     // the example is run, not checked
	also    []string // the codes besides the code of the file that it reports
	source  string   // the block, as written in the file
}

// errorDoc is a file of the catalog, read.
type errorDoc struct {
	code     string
	intro    bool // there is text between the title and the first heading
	examples []example
	problems []string // what is wrong with the shape of the file
}

// parseDoc reads the file of the code code, whose text is text.
func parseDoc(code, text string) errorDoc {
	d := errorDoc{code: code}
	problem := func(line int, format string, args ...any) {
		d.problems = append(d.problems, fmt.Sprintf("%s.md:%d: %s", code, line, fmt.Sprintf(format, args...)))
	}
	const (
		inText = iota
		inExample
		inOtherFence // a fence that is not a Ki example: its lines are not read
	)
	state := inText
	heading := ""
	var cur example
	var body []string
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, line := range lines {
		n := i + 1
		switch state {
		case inExample:
			if line != "```" {
				body = append(body, line)
				continue
			}
			cur.source = strings.Join(body, "\n") + "\n"
			d.examples = append(d.examples, cur)
			state = inText
			continue
		case inOtherFence:
			if line == "```" {
				state = inText
			}
			continue
		}
		switch {
		case i == 0:
			if prefix := code + ": "; !strings.HasPrefix(line, "# "+prefix) || strings.TrimSpace(strings.TrimPrefix(line, "# "+prefix)) == "" {
				problem(n, "the first line is %q, want \"# %s<message>\"", line, prefix)
			}
		case strings.HasPrefix(line, "## "):
			heading = line
		case strings.HasPrefix(line, "```"):
			words := strings.Fields(strings.TrimPrefix(line, "```"))
			if len(words) == 0 || words[0] != "ki" {
				state = inOtherFence
				continue
			}
			cur, body, state = example{heading: heading, line: n}, nil, inExample
			if len(words) < 2 || words[1] != "erroneous" && words[1] != "corrected" {
				problem(n, "the fence %q must say whether the example is erroneous or corrected", line)
			} else {
				cur.kind = words[1]
			}
			for _, w := range words[min(2, len(words)):] {
				switch {
				case w == "escaped":
					cur.escaped = true
				case w == "run":
					cur.run = true
				case strings.HasPrefix(w, "also="):
					cur.also = strings.Split(strings.TrimPrefix(w, "also="), ",")
				default:
					problem(n, "the fence %q has the unknown word %q", line, w)
				}
			}
		case strings.TrimSpace(line) != "" && heading == "":
			d.intro = true
		}
	}
	if state == inExample {
		problem(cur.line, "the example that opens here is never closed")
	}
	return d
}

// shapeProblems returns what is wrong with the shape of the file d.
func shapeProblems(d errorDoc) []string {
	problems := slices.Clone(d.problems)
	if !d.intro {
		problems = append(problems, d.code+".md: no text between the title and the first heading")
	}
	counts := map[string]int{}
	for _, ex := range d.examples {
		if ex.kind == "" {
			continue // already reported by parseDoc
		}
		counts[ex.kind]++
		want := map[string]string{"erroneous": "## Erroneous example", "corrected": "## Corrected"}[ex.kind]
		if !strings.HasPrefix(ex.heading, want) {
			problems = append(problems, fmt.Sprintf("%s.md:%d: the %s example is under the heading %q, want one that starts with %q",
				d.code, ex.line, ex.kind, ex.heading, want))
		}
	}
	for _, kind := range []string{"erroneous", "corrected"} {
		if counts[kind] == 0 {
			problems = append(problems, fmt.Sprintf("%s.md: no %s example", d.code, kind))
		}
	}
	return problems
}

// exampleArgs is the command line that runs the example in the file path.
func exampleArgs(ex example, path string) []string {
	command := "check"
	if ex.run {
		command = "run"
	}
	return []string{command, "--diagnostics=json", path}
}

// outcome is what the compiler did with an example.
type outcome struct {
	exit   int
	codes  []string // the code of each diagnostic, in the order of the output
	stderr string
}

// quiet reports whether the compiler had nothing to say about the example: it
// exited with 0 and wrote nothing to stderr. Both halves are needed. A warning
// exits with 0 and writes to stderr, and a failure that is not a diagnostic
// can exit with another code and write nothing.
func (o outcome) quiet() bool { return o.exit == 0 && o.stderr == "" }

// runExample writes the example to dir/example.ki and runs it in process, the
// way golden_test.go runs the golden files. It returns the outcome, or the
// reason the example cannot be run.
func runExample(dir string, ex example) (outcome, error) {
	source := ex.source
	if ex.escaped {
		var err error
		if source, err = strconv.Unquote(strings.TrimSpace(source)); err != nil {
			return outcome{}, fmt.Errorf("the block is not a Go string literal: %v", err)
		}
	}
	path := filepath.Join(dir, "example.ki")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		return outcome{}, err
	}
	var stdout, stderr bytes.Buffer
	exit := driver.Main(exampleArgs(ex, path), strings.NewReader(""), &stdout, &stderr)
	return outcomeOf(exit, stderr.String())
}

// outcomeOf reads what ki wrote to stderr, one JSON diagnostic per line, and
// returns it with the exit code. It is an error if a line is not a diagnostic:
// the compiler spoke in plain text, as it does for a command it does not know
// or a file it cannot read, and there is no code to compare.
func outcomeOf(exit int, stderr string) (outcome, error) {
	out := outcome{exit: exit, stderr: stderr}
	for _, line := range strings.Split(strings.TrimSuffix(out.stderr, "\n"), "\n") {
		if line == "" {
			continue
		}
		var d struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			return out, fmt.Errorf("stderr has a line that is not a diagnostic: %q", line)
		}
		out.codes = append(out.codes, d.Code)
	}
	return out, nil
}

// exampleProblems runs every example of the kind kind in d (a temporary
// directory per file) and returns what does not hold.
func exampleProblems(dir string, d errorDoc, kind string) []string {
	var problems []string
	for _, ex := range d.examples {
		if ex.kind != kind {
			continue
		}
		where := fmt.Sprintf("%s.md:%d", d.code, ex.line)
		out, err := runExample(dir, ex)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", where, err))
			continue
		}
		mine := slices.Contains(out.codes, d.code)
		var others []string // the other codes that it reports, without repeats
		for _, c := range out.codes {
			if c != d.code && !slices.Contains(others, c) {
				others = append(others, c)
			}
		}
		for _, c := range others {
			if !slices.Contains(ex.also, c) {
				problems = append(problems, fmt.Sprintf("%s: reports %s, which is not %s and is not in its also list %v", where, c, d.code, ex.also))
			}
		}
		for _, c := range ex.also {
			if !slices.Contains(others, c) {
				problems = append(problems, fmt.Sprintf("%s: its also list has %s, but the example does not report it", where, c))
			}
		}
		switch kind {
		case "erroneous":
			if !mine {
				problems = append(problems, fmt.Sprintf("%s: reports %v, want at least one %s", where, out.codes, d.code))
			}
		case "corrected":
			if mine {
				problems = append(problems, fmt.Sprintf("%s: still reports %s", where, d.code))
			}
			if len(ex.also) == 0 && !out.quiet() {
				problems = append(problems, fmt.Sprintf("%s: exits with %d and writes %q to stderr, want 0 and nothing", where, out.exit, out.stderr))
			}
			if len(ex.also) > 0 && out.exit == 0 {
				problems = append(problems, fmt.Sprintf("%s: exits with 0, but its also list is %v", where, ex.also))
			}
		}
	}
	return problems
}

// catalogProblems compares the codes that the source builds (a set) with the
// files of the catalog, in both directions.
func catalogProblems(inSource map[string]bool, docs map[string]errorDoc) []string {
	var problems []string
	for code := range inSource {
		if _, ok := docs[code]; !ok {
			problems = append(problems, fmt.Sprintf("the source builds %s, and docs/errors/%s.md does not exist", code, code))
		}
	}
	for code := range docs {
		if !inSource[code] {
			problems = append(problems, fmt.Sprintf("docs/errors/%s.md exists, and no code in the source builds %s", code, code))
		}
	}
	slices.Sort(problems)
	return problems
}

// codesIn returns the diagnostic codes that the Go file name, whose text is
// src, builds: the argument of each call diag.E(301) or diag.W(702), as the
// code E0301 or W0702, in the order of the calls. It reads the syntax tree and
// not the text, so a code that a comment or a string literal only names is not
// built and does not count. Every call written diag.E or diag.W must have one
// integer literal as its argument, and the literal must fit a code (16 bits),
// or the file is an error that names the call: a call like diag.E(n) builds a
// code that cannot be read here, and skipping it would let that code be born
// without its entry. A call of another package, another function of diag and a
// bare E(7) are not read.
func codesIn(name string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var codes []string
	var bad error
	ast.Inspect(file, func(node ast.Node) bool {
		if bad != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := fn.X.(*ast.Ident)
		if !ok || pkg.Name != "diag" || (fn.Sel.Name != "E" && fn.Sel.Name != "W") {
			return true
		}
		var lit *ast.BasicLit
		if len(call.Args) == 1 {
			lit, _ = call.Args[0].(*ast.BasicLit)
		}
		if lit == nil || lit.Kind != token.INT {
			bad = fmt.Errorf("%s: diag.%s needs one integer literal as its argument, so that the test can tell which code it builds", fset.Position(call.Pos()), fn.Sel.Name)
			return false
		}
		n, err := strconv.ParseUint(lit.Value, 0, 16)
		if err != nil {
			bad = fmt.Errorf("%s: %v", name, err)
			return false
		}
		codes = append(codes, fmt.Sprintf("%s%04d", fn.Sel.Name, n))
		return true
	})
	if bad != nil {
		return nil, bad
	}
	return codes, nil
}

// codesUnder returns the codes that the Go files under root (the tests aside)
// build with diag.E and diag.W, as a set. The files that are not Go and the
// ones whose name ends in _test.go are not read; a Go file that cannot be read
// as Go is an error that names it.
func codesUnder(root string) (map[string]bool, error) {
	codes := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := codesIn(path, text)
		if err != nil {
			return err
		}
		for _, code := range found {
			codes[code] = true
		}
		return nil
	})
	return codes, err
}

// sourceCodes returns the codes that the Go files under internal/ (the tests
// aside) build with diag.E and diag.W.
func sourceCodes(t *testing.T) map[string]bool {
	t.Helper()
	codes, err := codesUnder(filepath.Join(repoRoot, "internal"))
	if err != nil {
		t.Fatalf("cannot read the source under internal/: %v", err)
	}
	if len(codes) == 0 {
		t.Fatal("found no diagnostic code in the source under internal/: the test would pass without checking anything")
	}
	return codes
}

// readCatalog reads the files of docs/errors. A file whose name is not a code
// is a problem, and so is a catalog with no file at all.
func readCatalog(t *testing.T) (docs map[string]errorDoc, problems []string) {
	t.Helper()
	dir := filepath.Join(repoRoot, "docs", "errors")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read the catalog: %v", err)
	}
	docs = map[string]errorDoc{}
	for _, entry := range entries {
		m := docFileName.FindStringSubmatch(entry.Name())
		if m == nil {
			problems = append(problems, fmt.Sprintf("docs/errors/%s is not the file of a code", entry.Name()))
			continue
		}
		text, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("cannot read the catalog: %v", err)
		}
		docs[m[1]] = parseDoc(m[1], string(text))
	}
	if len(docs) == 0 {
		t.Fatal("docs/errors has no file: the test would pass without checking anything")
	}
	return docs, problems
}

// sortedCodes returns the codes of docs in order, so that the failures come in
// the same order every time.
func sortedCodes(docs map[string]errorDoc) []string {
	codes := make([]string, 0, len(docs))
	for code := range docs {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	return codes
}

func TestErrorDocs(t *testing.T) {
	t.Run("catalog_matches_the_codes_in_the_source", func(t *testing.T) {
		docs, problems := readCatalog(t)
		problems = append(problems, catalogProblems(sourceCodes(t), docs)...)
		for _, p := range problems {
			t.Error(p)
		}
	})

	t.Run("documents_have_the_fixed_shape", func(t *testing.T) {
		docs, _ := readCatalog(t)
		for _, code := range sortedCodes(docs) {
			for _, p := range shapeProblems(docs[code]) {
				t.Error(p)
			}
		}
	})

	t.Run("erroneous_examples_report_their_code", func(t *testing.T) {
		docs, _ := readCatalog(t)
		for _, code := range sortedCodes(docs) {
			for _, p := range exampleProblems(t.TempDir(), docs[code], "erroneous") {
				t.Error(p)
			}
		}
	})

	t.Run("corrected_examples_check_clean", func(t *testing.T) {
		docs, _ := readCatalog(t)
		for _, code := range sortedCodes(docs) {
			for _, p := range exampleProblems(t.TempDir(), docs[code], "corrected") {
				t.Error(p)
			}
		}
	})

	t.Run("checks_catch_bad_catalogs", func(t *testing.T) {
		// The checks above pass on a good catalog; this shows that each of them
		// fails on a bad one. Every row is a file (or a pair of sets of codes)
		// written here, and the problems that the check must report, as pieces
		// of their text.
		const head = "# E0301: unknown name\n\nThe rule.\n\n"
		const erroneous = "## Erroneous example\n\n```ki erroneous\nfn main() { prnt(1) }\n```\n\n"
		const corrected = "## Corrected\n\n```ki corrected\nfn main() { print(1) }\n```\n"

		shape := []struct{ name, text, want string }{
			{"a good file", head + erroneous + corrected, ""},
			{"no code in the title", "# unknown name\n\nThe rule.\n\n" + erroneous + corrected, "the first line is"},
			{"no message in the title", "# E0301: \n\nThe rule.\n\n" + erroneous + corrected, "the first line is"},
			{"another code in the title", "# E0302: x\n\nThe rule.\n\n" + erroneous + corrected, "the first line is"},
			{"no explanation", "# E0301: unknown name\n\n" + erroneous + corrected, "no text between"},
			{"no erroneous example", head + corrected, "no erroneous example"},
			{"no corrected example", head + erroneous, "no corrected example"},
			{"erroneous example under the wrong heading", head + strings.Replace(erroneous, "## Erroneous example", "## Corrected", 1) + corrected, "the erroneous example is under the heading"},
			{"corrected example under the wrong heading", head + erroneous + strings.Replace(corrected, "## Corrected", "## Erroneous example", 1), "the corrected example is under the heading"},
			{"unknown word in the fence", head + strings.Replace(erroneous, "ki erroneous", "ki erroneous skipped", 1) + corrected, `the unknown word "skipped"`},
			{"no kind in the fence", head + strings.Replace(erroneous, "ki erroneous", "ki", 1) + corrected, "must say whether"},
			{"fence never closed", head + erroneous + "## Corrected\n\n```ki corrected\nfn main() {}\n", "is never closed"},
			{"two kinds of example in order", head + erroneous + corrected + erroneous + corrected, ""},
			// The lines of a fence that is not a Ki example are not read, also the
			// ones that look like a heading or like the start of an example.
			{"a fence that is not Ki", head + strings.Replace(erroneous, "```ki erroneous", "```text\n## Corrected\n```ki corrected\n```\n\n```ki erroneous", 1) + corrected, ""},
		}
		for _, row := range shape {
			problems := shapeProblems(parseDoc("E0301", row.text))
			if row.want == "" && len(problems) != 0 {
				t.Errorf("%s: shape problems %q, want none", row.name, problems)
			}
			if row.want != "" && !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, row.want) }) {
				t.Errorf("%s: shape problems %q, want one with %q", row.name, problems, row.want)
			}
		}

		// The examples run through the compiler. The file's code is E0301. Each
		// row puts its block in place of the example of its kind in a good file.
		build := func(kind, fence, source string) string {
			erroneousBlock := "```ki erroneous\nfn main() { prnt(1) }\n```"
			correctedBlock := "```ki corrected\nfn main() { print(1) }\n```"
			if block := "```" + fence + "\n" + source + "```"; kind == "erroneous" {
				erroneousBlock = block
			} else {
				correctedBlock = block
			}
			return head + "## Erroneous example\n\n" + erroneousBlock + "\n\n## Corrected\n\n" + correctedBlock + "\n"
		}
		examples := []struct{ name, kind, fence, source, want string }{
			{"an erroneous example that reports its code", "erroneous", "ki erroneous", "fn main() { prnt(1) }\n", ""},
			{"an erroneous example that reports nothing", "erroneous", "ki erroneous", "fn main() { print(1) }\n", "want at least one E0301"},
			{"an erroneous example with another code", "erroneous", "ki erroneous", "fn main() { 1 + }\n", "reports E0201, which is not E0301"},
			{"an erroneous example with its code and another", "erroneous", "ki erroneous", "fn main() { prnt(1 + ) }\n", "reports E0201, which is not E0301"},
			{"an erroneous example with the other code allowed", "erroneous", "ki erroneous also=E0201", "fn main() { prnt(1 + ) }\n", ""},
			{"an also that the example does not need", "erroneous", "ki erroneous also=E0201", "fn main() { prnt(1) }\n", "its also list has E0201"},
			{"an erroneous example that is escaped", "erroneous", "ki erroneous escaped", "\"fn main() { prnt(1) }\\n\"\n", ""},
			{"an escaped example that is not a Go string", "erroneous", "ki erroneous escaped", "fn main() { prnt(1) }\n", "not a Go string literal"},
			{"a corrected example that is clean", "corrected", "ki corrected", "fn main() { print(1) }\n", ""},
			{"a corrected example that still reports its code", "corrected", "ki corrected", "fn main() { prnt(1) }\n", "still reports E0301"},
			{"a corrected example with another error", "corrected", "ki corrected", "fn main() { 1 + }\n", "reports E0201, which is not E0301"},
			{"a corrected example with the other code allowed", "corrected", "ki corrected also=E0201", "fn main() { 1 + }\n", ""},
			{"a corrected also that is not needed", "corrected", "ki corrected also=E0201", "fn main() { print(1) }\n", "its also list has E0201"},
			{"a corrected example that exits with 1 and writes to stderr", "corrected", "ki corrected", "fn main() { 1 + }\n", "exits with 1 and writes"},
		}
		for _, row := range examples {
			problems := exampleProblems(t.TempDir(), parseDoc("E0301", build(row.kind, row.fence, row.source)), row.kind)
			if row.want == "" && len(problems) != 0 {
				t.Errorf("%s: problems %q, want none", row.name, problems)
			}
			if row.want != "" && !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, row.want) }) {
				t.Errorf("%s: problems %q, want one with %q", row.name, problems, row.want)
			}
		}

		// A corrected example with no also list must be quiet, and quiet is
		// both halves of it: the exit code 0 and nothing on stderr. A warning
		// passes the first and not the second; the other way round, an exit
		// code with nothing written passes the second and not the first. No
		// example of the catalog separates the two halves today, so these rows
		// do.
		for _, row := range []struct {
			name string
			out  outcome
			want bool
		}{
			{"exit 0 and nothing on stderr", outcome{exit: 0}, true},
			{"exit 0 and a warning on stderr", outcome{exit: 0, stderr: `{"code":"W0301"}` + "\n"}, false},
			{"exit 1 and nothing on stderr", outcome{exit: 1}, false},
			{"exit 1 and an error on stderr", outcome{exit: 1, stderr: `{"code":"E0301"}` + "\n"}, false},
		} {
			if got := row.out.quiet(); got != row.want {
				t.Errorf("%s: quiet is %v for %+v, want %v", row.name, got, row.out, row.want)
			}
		}

		// The command that runs an example: ki check, or ki run for the word run.
		if got, want := exampleArgs(example{}, "x.ki"), []string{"check", "--diagnostics=json", "x.ki"}; !slices.Equal(got, want) {
			t.Errorf("command of an example: %q, want %q", got, want)
		}
		if got, want := exampleArgs(example{run: true}, "x.ki"), []string{"run", "--diagnostics=json", "x.ki"}; !slices.Equal(got, want) {
			t.Errorf("command of an example with run: %q, want %q", got, want)
		}

		// The word run in the fence reaches the example, and only that example.
		runDoc := parseDoc("E0301", build("erroneous", "ki erroneous run", "fn main() { prnt(1) }\n"))
		if len(runDoc.examples) != 2 || !runDoc.examples[0].run || runDoc.examples[1].run {
			t.Errorf("a fence with run: examples %+v, want the erroneous one with run and the corrected one without", runDoc.examples)
		}

		// What ki writes to stderr is one JSON diagnostic per line. Plain text,
		// as for a command that ki does not know or a file it cannot read, has
		// no code in it: it is an error and not an example with no diagnostics.
		if out, err := outcomeOf(2, "usage: ki <command> [args]\n"); err == nil || !strings.Contains(err.Error(), "that is not a diagnostic") {
			t.Errorf("stderr with plain text: outcome %+v and error %v, want an error that says so", out, err)
		}
		if out, err := outcomeOf(1, `{"code":"E0301"}`+"\n"+`{"code":"E0201"}`+"\n"); err != nil || out.exit != 1 || !slices.Equal(out.codes, []string{"E0301", "E0201"}) {
			t.Errorf("stderr with two diagnostics: outcome %+v and error %v, want exit 1 and the codes E0301 and E0201 in order", out, err)
		}

		// The codes of the source are the arguments of the calls diag.E and
		// diag.W, found in the syntax tree and not in the text, with both
		// letters (no warning exists yet, but the first one is born with its
		// catalog entry like an error is). A code that a comment of any kind or
		// a string only names is not built: a code removed from the compiler
		// would keep its file alive. A call inside another call counts, and so
		// does a literal in hexadecimal; the function of another package, a
		// name that is not E or W and a bare E do not.
		goSource := strings.Join([]string{
			"package p",
			"",
			"// diag.E(901) in a doc comment.",
			"func f() {",
			"\t/* diag.E(902) in a block comment */",
			"\treport(diag.NewError(diag.E(301), \"m\")) // diag.E(903) after a call",
			"\t_ = \"diag.E(904) in a string\"",
			"\t_ = `diag.E(905) in a raw string`",
			"\t_ = diag.W(702)",
			"\t_ = diag.E(0x12D)",
			"\t_ = diag.X(5)",
			"\t_ = other.E(6)",
			"\t_ = E(7)",
			"}",
		}, "\n") + "\n"
		if got, err := codesIn("f.go", []byte(goSource)); err != nil || !slices.Equal(got, []string{"E0301", "W0702", "E0301"}) {
			t.Errorf("codes of a source file: %q and error %v, want E0301, W0702 and E0301 (0x12D) in order and no error", got, err)
		}
		// A number that is not a code (they are 16 bits) is an error, not a code
		// of another number.
		if got, err := codesIn("f.go", []byte("package p\n\nvar a = diag.E(70000)\n")); err == nil {
			t.Errorf("codes of a source file with diag.E(70000): %q and no error, want an error", got)
		}
		// A call whose argument is not an integer literal builds a code that
		// this reading cannot know, so it is an error that names the call (the
		// file, the line and the column, and E or W), and not a call that is
		// skipped: a code born that way would have no file in the catalog and
		// nothing would notice. It does not matter what the argument is.
		for _, row := range []struct{ name, call string }{
			{"a variable", "diag.E(n)"},
			{"a constant", "diag.W(codeUnknown)"},
			{"a sum", "diag.E(300 + 1)"},
			{"a conversion", "diag.E(uint16(301))"},
			{"a string", `diag.E("301")`},
			{"no argument", "diag.E()"},
			{"two arguments", "diag.W(1, 2)"},
		} {
			got, err := codesIn("f.go", []byte("package p\n\nvar a = "+row.call+"\n"))
			if want := "f.go:3:9: " + row.call[:6] + " "; err == nil || !strings.HasPrefix(err.Error(), want) || got != nil {
				t.Errorf("%s: codes of a source file with %s: %q and error %v, want no codes and an error that starts %q", row.name, row.call, got, err, want)
			}
		}

		// A tree of directories is read file by file: the Go files of its
		// subdirectories count, and the tests and the files that are not Go do
		// not.
		tree := t.TempDir()
		write := func(name, text string) {
			t.Helper()
			path := filepath.Join(tree, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("cannot make the directory of %s: %v", name, err)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatalf("cannot write %s: %v", name, err)
			}
		}
		write("a.go", "package p\n\nvar a = diag.E(301) // diag.E(901)\n")
		write("sub/b.go", "package p\n\nvar b = diag.W(702)\n")
		write("sub/b_test.go", "package p\n\nvar c = diag.E(902)\n")
		write("notes.txt", "diag.E(903)\n")
		if got, err := codesUnder(tree); err != nil || !maps.Equal(got, map[string]bool{"E0301": true, "W0702": true}) {
			t.Errorf("codes under a tree: %v and error %v, want E0301 and W0702 and no error", got, err)
		}
		// A file named .go that does not parse is an error that names it, not a
		// file with no codes (or only with the ones that come before the mistake).
		write("bad.go", "package p\n\nvar d = diag.E(904)\nfunc (\n")
		if got, err := codesUnder(tree); err == nil || !strings.Contains(err.Error(), "bad.go") {
			t.Errorf("codes under a tree with a Go file that does not parse: %v and error %v, want an error that names bad.go", got, err)
		}

		// The files and the codes of the source are compared both ways.
		doc := func(code string) errorDoc { return errorDoc{code: code} }
		got := catalogProblems(map[string]bool{"E0101": true, "E0102": true}, map[string]errorDoc{"E0101": doc("E0101"), "E0999": doc("E0999")})
		want := []string{
			"docs/errors/E0999.md exists, and no code in the source builds E0999",
			"the source builds E0102, and docs/errors/E0102.md does not exist",
		}
		if !slices.Equal(got, want) {
			t.Errorf("catalog problems %q, want %q", got, want)
		}
		if got := catalogProblems(map[string]bool{"E0101": true}, map[string]errorDoc{"E0101": doc("E0101")}); len(got) != 0 {
			t.Errorf("a catalog that matches: problems %q, want none", got)
		}
	})
}
