// bookx: not-in-book

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTokenizeKi(t *testing.T) {
	t.Run("rows", func(t *testing.T) {
		type row struct {
			name, src string
			want      []string
		}
		for _, r := range []row{
			{"empty", "", nil},
			{"words and numbers", "fn f1 _x 1_000", []string{"fn", "f1", "_x", "1_000"}},
			{"long operators", "a -> b |> c == d != e && f || g <= h >= i", []string{"a", "->", "b", "|>", "c", "==", "d", "!=", "e", "&&", "f", "||", "g", "<=", "h", ">=", "i"}},
			{"one mark at a time", "(a,b){}=+-", []string{"(", "a", ",", "b", ")", "{", "}", "=", "+", "-"}},
			{"a string is one token", `print("a b \" c") x`, []string{"print", "(", `"a b \" c"`, ")", "x"}},
			{"a comment is no token", "a // b c\nd /* e\nf */ g", []string{"a", "d", "g"}},
			{"a string that does not end stops at its line", "\"abc\nd", []string{`"`, "abc", "d"}},
		} {
			var got []string
			for _, tok := range tokenizeKi(r.src) {
				got = append(got, tok.Text)
			}
			if !slices.Equal(got, r.want) {
				t.Errorf("%s: %q, want %q", r.name, got, r.want)
			}
		}
	})
	t.Run("positions", func(t *testing.T) {
		toks := tokenizeKi("// c\n  ab cd\ne")
		type pos struct {
			text              string
			offset, line, col int
		}
		want := []pos{{"ab", 7, 2, 3}, {"cd", 10, 2, 6}, {"e", 13, 3, 1}}
		if len(toks) != len(want) {
			t.Fatalf("tokens %+v", toks)
		}
		for i, w := range want {
			got := toks[i]
			if got.Text != w.text || got.Offset != w.offset || got.Line != w.line || got.Col != w.col {
				t.Errorf("token %d: %+v, want %+v", i, got, w)
			}
		}
	})
}

func TestCascadeEdits(t *testing.T) {
	t.Run("delete_replace_and_insert_each_token_once", func(t *testing.T) {
		edits := cascadeEdits("a b\n", []string{"x", "b"})
		var got []string
		for _, e := range edits {
			got = append(got, e.Kind+"|"+e.Token+"|"+e.Text+"|"+e.Source)
		}
		want := []string{
			"del|a||  b\n",
			"rep|a|x|x b\n",
			"ins|a|x|x a b\n",
			"rep|a|b|b b\n",
			"ins|a|b|b a b\n",
			"del|b||a  \n",
			"rep|b|x|a x\n",
			"ins|b|x|a x b\n",
			"ins|b|b|a b b\n",
		}
		if !slices.Equal(got, want) {
			t.Errorf("edits:\n got %q\nwant %q", got, want)
		}
	})
	t.Run("the_same_program_is_tried_once", func(t *testing.T) {
		// Deleting either `a` gives a different text, but inserting `a` before
		// either one gives `a a a`: only the first remains.
		var sources []string
		for _, e := range cascadeEdits("a a", []string{"a"}) {
			sources = append(sources, e.Source)
		}
		want := []string{"  a", "a a a", "a  "}
		if !slices.Equal(sources, want) {
			t.Errorf("sources %q, want %q", sources, want)
		}
	})
	t.Run("a_mutant_has_the_line_of_its_token", func(t *testing.T) {
		edits := cascadeEdits("a\n  b", []string{})
		if len(edits) != 2 || edits[1].Line != 2 || edits[1].Col != 3 {
			t.Errorf("edits %+v", edits)
		}
	})
}

func TestCascadePads(t *testing.T) {
	src := "one\ntwo\nthree\n"
	t.Run("functions_at_the_end", func(t *testing.T) {
		got, isPad := cascadePadded(src, 1, 2, padFunctions)
		if !strings.HasPrefix(got, src+"\nfn bookxPad0(") || !strings.Contains(got, "fn bookxPad1(") || strings.Contains(got, "fn bookxPad2(") {
			t.Errorf("text:\n%s", got)
		}
		for line, want := range map[int]bool{1: false, 3: false, 4: false, 5: true, 9: true, 10: false, 11: true, 15: true, 16: false} {
			if isPad(line) != want {
				t.Errorf("isPad(%d) = %v, want %v, in:\n%s", line, !want, want, got)
			}
		}
	})
	t.Run("statements_after_the_edit", func(t *testing.T) {
		got, isPad := cascadePadded(src, 1, 1, padStatements)
		lines := strings.Split(got, "\n")
		if lines[0] != "one" || lines[1] != "  print(0) // bookxCall0" || lines[2] != "  let bookxLet0 = 0" || lines[3] != "two" {
			t.Errorf("text:\n%s", got)
		}
		if isPad(1) || !isPad(2) || !isPad(3) || isPad(4) || isPad(5) {
			t.Errorf("isPad of the statements is wrong, in:\n%s", got)
		}
	})
	t.Run("calls_come_before_the_lets", func(t *testing.T) {
		// Each call and each `let` is a statement that ends the arms of a `match` that
		// lost its `}`; the order is fixed, the calls first and then the `let`s, so that
		// every edit gets the same text after it.
		got, _ := cascadePadded(src, 1, 3, padStatements)
		lines := strings.Split(got, "\n")
		want := []string{"one", "  print(0) // bookxCall0", "  print(1) // bookxCall1", "  print(2) // bookxCall2", "  let bookxLet0 = 0", "  let bookxLet1 = 1", "  let bookxLet2 = 2", "two"}
		if !slices.Equal(lines[:len(want)], want) {
			t.Errorf("text:\n%s", got)
		}
	})
	t.Run("copies_of_the_arm_after_the_edit", func(t *testing.T) {
		const arms = "m {\n  A => 1\n  B => 2\n}\n"
		got, isPad := cascadePadded(arms, 1, 2, padArms)
		lines := strings.Split(got, "\n")
		want := []string{"m {", "  A => 1", "  A => 1", "  A => 1", "  B => 2", "}"}
		if !slices.Equal(lines[:len(want)], want) {
			t.Errorf("text:\n%s", got)
		}
		for line, pad := range map[int]bool{1: false, 2: false, 3: true, 4: true, 5: false, 6: false} {
			if isPad(line) != pad {
				t.Errorf("isPad(%d) = %v, want %v, in:\n%s", line, !pad, pad, got)
			}
		}
		// The line after the edit is the one that is copied, if it is an arm: after
		// line 2 it is `B => 2`, after line 3 it is the `}`, and after the last line
		// there is none, so only the first of these three adds copies.
		for line, want := range map[int][2]int{2: {1, 3}, 3: {1, 1}, 4: {1, 1}} {
			text, _ := cascadePadded(arms, line, 2, padArms)
			if a, b := strings.Count(text, "  A => 1"), strings.Count(text, "  B => 2"); a != want[0] || b != want[1] {
				t.Errorf("an edit at line %d: %d copies of A and %d of B, want %d and %d:\n%s", line, a, b, want[0], want[1], text)
			}
		}
		for _, line := range []int{3, 4} {
			if text, _ := cascadePadded(arms, line, 2, padArms); !strings.HasPrefix(text, arms+"\nfn bookxPad0(") {
				t.Errorf("an edit at line %d, where no arm follows, changed the program:\n%s", line, text)
			}
		}
	})
	t.Run("a_program_with_no_final_newline", func(t *testing.T) {
		got, _ := cascadePadded("one", 1, 1, padFunctions)
		if !strings.HasPrefix(got, "one\n\nfn bookxPad0(") {
			t.Errorf("text:\n%s", got)
		}
	})
}

func TestParseKiDiagnostics(t *testing.T) {
	t.Run("errors_of_the_json_lines", func(t *testing.T) {
		out := `{"severity":"error","code":"E0301","message":"unknown name","path":"x","line":3,"col":9,"labels":[{"primary":true,"line":3,"col":9,"message":"m"},{"primary":false,"line":2,"col":7,"message":"n"}],"help":"h"}
not json
{"severity":"warning","code":"W1","message":"w","line":4,"col":1}
{"severity":"error","code":"E0201","message":"other","line":5,"col":1}
`
		got := parseKiDiagnostics([]byte(out))
		if len(got) != 2 || got[0].Code != "E0301" || got[0].Line != 3 || got[0].Col != 9 || got[1].Code != "E0201" || got[1].Line != 5 || got[0].Help != "h" || got[1].Help != "" {
			t.Errorf("diagnostics %+v", got)
		}
	})
}

// fakeKi writes a program that stands for `ki check --diagnostics=json FILE`:
//
//	a line with OOPS    gives one error at that line
//	a line with STRAY   gives one error at that line and another at line 1
//	a line with FAR     gives one error at line 1 and none at that line
//	after a line with OPEN, each line with bookxPad, bookxCall or bookxLet gives one error
//	after a line with CALLS, each call that was added (a line with bookxCall) gives
//	                    one error at that line, and a `let` that was added none
//	a line with SECOND  gives one error at the second added call, and none anywhere
//	                    else
//	a line with ARMLINE gives one error at each line with a =>
//	a line with ARMS    gives, for each line with a => in the program, one error at
//	                    line 1
//	a line with NEXT    gives one error at the next line
//	a line with BRACE   gives E0202 for a `{` at that line, and each later line that
//	                    starts with fn, pub or # an E0201 that says what it found
//	a line with HEADER  does the same but with `expected identifier` on the first
//	                    such line, as a `let` left before a `fn` does
//	a line with PAREN   does the same as BRACE for a `(`
//	a line with NOCLOSE does the same as BRACE, without the E0202
//	a line with WIDE    does the same as BRACE, and the first statement of each pad
//	                    gets an error too
//	a line with ELSE    does the same as BRACE, and line 1 gets another error
//	a line with ONEPAD  gives one error on the first added function, however many
//	a line with GROWS   gives, for each added line, one error at line 1
//	a line with FARCOL  gives one error at 2:7, then one at that line
//	a line with PASTEND gives one error at that line and one past the last line
//	a line with ATEND   gives one error at that line and one on the last line
//	a line with NEARGROW gives one error at the next line and, for each added line,
//	                    one error at line 1
//	a line with FNPADS  gives one error on each added function, and none next to
//	                    the edit
//	the text `ONCE a`   gives one error on the first added function, however many
//	ICE exits with 70, and HANG never ends
const fakeKi = `#!/bin/sh
f="$3"
grep -q ICE "$f" && exit 70
grep -q HANG "$f" && exec sleep 60
awk '
function diag(n) { printf "{\"severity\":\"error\",\"code\":\"E1\",\"message\":\"m\",\"line\":%d,\"col\":1}\n", n; bad = 1 }
function diagat(n, c) { printf "{\"severity\":\"error\",\"code\":\"E1\",\"message\":\"m\",\"line\":%d,\"col\":%d}\n", n, c; bad = 1 }
/FARCOL/ { diagat(2, 7); diag(NR) }
/PASTEND/ { diag(NR); pastend = 1 }
/ATEND/ { diag(NR); atend = 1 }
/NEARGROW/ { nextline = NR + 1; grows = 1 }
/FNPADS/ { fnpads = 1 }
/CALLS/ { calls = 1 }
calls && /bookxCall/ { diag(NR) }
/SECOND/ { second = 1 }
second && /bookxCall1/ { diag(NR) }
/ARMLINE/ { armline = 1 }
armline && /=>/ { diag(NR) }
/ARMS/ { arms = 1 }
arms && /=>/ { diag(1) }
fnpads && /fn bookxPad/ { diag(NR) }
/ONCE a/ { onepad = 1 }
function item(n, what, word) { printf "{\"severity\":\"error\",\"code\":\"E0201\",\"message\":\"expected %s, found \140%s\140\",\"line\":%d,\"col\":1}\n", what, word, n; bad = 1 }
function closer(n, c) { printf "{\"severity\":\"error\",\"code\":\"E0202\",\"message\":\"unclosed delimiter\",\"line\":%d,\"col\":1,\"help\":\"close it with \140%s\140\"}\n", n, c; bad = 1 }
/NEXT/ { nextline = NR + 1 }
NR == nextline { diag(NR) }
/BRACE|HEADER|ELSE|WIDE/ { closer(NR, "}"); swallow = 1 }
/HEADER/ { first = 1 }
/ELSE/ { diag(1) }
/WIDE/ { wide = 1 }
/PAREN/ { closer(NR, ")"); swallow = 1 }
/NOCLOSE/ { swallow = 1 }
swallow && /^fn / { item(NR, first ? "identifier" : "expression", "fn"); first = 0 }
swallow && /^pub / { item(NR, "expression", "pub") }
swallow && /^#/ { item(NR, "expression", "#") }
wide && /let b = / { diag(NR) }
/ONEPAD/ { onepad = 1 }
onepad && /fn bookxPad0\(/ { diag(NR) }
/GROWS/ { grows = 1 }
grows && /bookx(Pad|Call|Let)/ { diag(1) }
/OPEN/ { open = 1 }
/OOPS/ { diag(NR) }
/STRAY/ { diag(NR); diag(1) }
/FAR/ { diag(1) }
open && /bookx(Pad|Call|Let)/ { diag(NR) }
END { if (pastend) diag(NR + 1); if (atend) diag(NR); exit bad }
' "$f" >&2
`

// cascadeRun runs the command on a program with the fake ki and the given words
// to insert.
func cascadeRun(t *testing.T, program string, vocabulary ...string) (int, string, string) {
	t.Helper()
	noCap(t)
	old := cascadeVocabulary
	cascadeVocabulary = vocabulary
	t.Cleanup(func() { cascadeVocabulary = old })
	dir := t.TempDir()
	writeFile(t, dir, "ki", fakeKi)
	if err := os.Chmod(filepath.Join(dir, "ki"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "p.ki", program)
	t.Chdir(dir)
	return runBookx(t, "cascade", "--ki", "./ki", "p.ki")
}

func TestCascadeRun(t *testing.T) {
	t.Run("nothing_wrong", func(t *testing.T) {
		code, out, errOut := cascadeRun(t, "a\nb\n", "OOPS")
		want := "cascade p.ki: 6 mutants, 2 still valid, 4 with errors (1: 4), 0 grow, 0 at correct text, 0 far from the edit, 0 per arm, 0 on the known limit, 0 crash\n"
		if code != exitOK || out != want || errOut != "" {
			t.Errorf("code %d, stdout %q, stderr %q; want 0, %q", code, out, errOut, want)
		}
	})

	t.Run("a_program_with_errors_cannot_be_the_start", func(t *testing.T) {
		code, _, errOut := cascadeRun(t, "OOPS\n", "x")
		if code != exitFail || errOut != "bookx cascade: p.ki: the program has 1 errors before any edit\n" {
			t.Errorf("code %d, stderr %q", code, errOut)
		}
	})

	t.Run("diagnostics_that_grow_with_correct_text", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "a\n", "OPEN")
		for _, want := range []string{
			"p.ki:1:1: grows: rep `a` -> `OPEN`: 0 diagnostics, then 12, then 24, with 0, 4 and 8 correct additions after it: E1 m\n",
			"cascade p.ki: 3 mutants, 3 still valid, 0 with errors (none), 2 grow, 2 at correct text, 0 far from the edit, 0 per arm, 0 on the known limit, 0 crash\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("want %q in:\n%s", want, out)
			}
		}
		if code != exitFail {
			t.Errorf("code %d", code)
		}
	})

	t.Run("a_call_among_the_added_statements_is_seen", func(t *testing.T) {
		// The added statements have calls as well as `let`s, because a mistake may cost
		// an error for each statement of one kind and none for the other: the errors
		// that grow with the calls are found (and, being on lines that the tool added,
		// are text that is correct as well).
		code, out, _ := cascadeRun(t, "a\n", "CALLS")
		for _, want := range []string{
			"p.ki:1:1: grows: rep `a` -> `CALLS`: 0 diagnostics, then 4, then 8, with 0, 4 and 8 correct additions after it: E1 m\n",
			"p.ki:1:1: correct-text: rep `a` -> `CALLS`: an error on an added correct line: E1 m\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("want %q in:\n%s", want, out)
			}
		}
		if code != exitFail {
			t.Errorf("code %d", code)
		}
	})

	t.Run("the_second_call_is_next_to_the_edit_too", func(t *testing.T) {
		// A `(` that an edit leaves open takes the first call with it and is noticed at
		// the second, two lines after the edit: that is the mistake's own error, and not
		// text that is correct.
		code, out, _ := cascadeRun(t, "a\n", "SECOND")
		if !strings.Contains(out, " 0 grow, 0 at correct text, ") || code != exitOK {
			t.Errorf("code %d, output:\n%s", code, out)
		}
	})

	t.Run("copies_of_an_arm_among_the_added_lines_are_seen", func(t *testing.T) {
		// A match that lost its keyword costs errors for each arm that it has, which no
		// statement after the edit shows: a copy of the arm that follows the edit does.
		// The program has two arms, so the edit starts with two errors and each copy
		// adds one. It is listed and counted, and it fails nothing: a scrutinee that is
		// wrong costs an error for each arm too.
		code, out, _ := cascadeRun(t, "a\n  A => 1\n  B => 2\n", "ARMS")
		want := "p.ki:1:1: per-arm: rep `a` -> `ARMS`: 2 diagnostics, then 6, then 10, with 0, 4 and 8 correct additions after it: none on the added lines\n"
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
		if strings.Contains(out, "grows:") || strings.Contains(out, "correct-text:") || code != exitOK {
			t.Errorf("the growth per arm is no failure: code %d, output:\n%s", code, out)
		}
		// The errors of the copies are on the copies, which are arms of the program: the
		// run for the arms does not call them correct text, as it does for the lines of
		// the other runs, and says what the error is.
		code, out, _ = cascadeRun(t, "a\n  A => 1\n  B => 2\n", "ARMLINE")
		want = "p.ki:1:1: per-arm: rep `a` -> `ARMLINE`: 2 diagnostics, then 6, then 10, with 0, 4 and 8 correct additions after it: E1 m\n"
		if !strings.Contains(out, want) || strings.Contains(out, "correct-text:") || code != exitOK {
			t.Errorf("errors on the copies: code %d, output:\n%s", code, out)
		}
		// A program with no arm has no copies, so it adds no run.
		code, out, _ = cascadeRun(t, "a\nb\n", "ARMS")
		if strings.Contains(out, "per-arm") || code != exitOK {
			t.Errorf("a program with no arms: code %d, output:\n%s", code, out)
		}
	})

	t.Run("an_error_elsewhere_besides_the_one_at_the_edit_is_listed_and_does_not_fail", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "a\nb\nc\nd\n", "STRAY")
		if code != exitOK {
			t.Errorf("code %d: a line far from the edit is often the mistake's own consequence", code)
		}
		if !strings.Contains(out, "p.ki:4:1: far: rep `d` -> `STRAY`: an error at 1:1, and the edit is on line 4\n") {
			t.Errorf("stdout:\n%s", out)
		}
		// On line 2 the other error is next to the edit: nothing to report.
		if strings.Contains(out, "p.ki:2:1: far") {
			t.Errorf("an error next to the edit is flagged:\n%s", out)
		}
		if !strings.Contains(out, " 0 at correct text, 4 far from the edit, ") {
			t.Errorf("counts:\n%s", out)
		}
	})

	t.Run("an_added_line_next_to_the_edit_is_where_the_mistake_is_noticed", func(t *testing.T) {
		// A missing `)` is noticed at the next token, and that one may be on an added line.
		code, out, _ := cascadeRun(t, "a\nb\n", "NEXT")
		if code != exitOK || strings.Contains(out, "correct-text:") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	// The accepted limit: a missing `}` costs an E0201 for each later line that starts
	// an item, as internal/syntax/parser_stmt.go documents. A program with an item
	// after the edit, so the program itself shows it too.
	const items = "a\nfn g\n"

	t.Run("the_missing_brace_is_counted_apart_and_does_not_fail", func(t *testing.T) {
		// The item is two lines below the first edit, so it is far from it as well.
		code, out, _ := cascadeRun(t, "a\nb\nc\nfn g\n", "BRACE", "HEADER")
		if code != exitOK {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
		for _, kind := range []string{": grows:", ": correct-text:", ": far:"} {
			if strings.Contains(out, kind) {
				t.Errorf("%s in:\n%s", kind, out)
			}
		}
		if !strings.Contains(out, " 0 grow, 0 at correct text, 0 far from the edit, 0 per arm, 20 on the known limit, 0 crash\n") {
			t.Errorf("counts:\n%s", out)
		}
	})

	for _, row := range []struct{ name, word string }{
		{"without_an_e0202_it_is_a_cascade_like_any_other", "NOCLOSE"},
		{"an_unclosed_paren_is_not_that_limit", "PAREN"},
	} {
		t.Run(row.name, func(t *testing.T) {
			code, out, _ := cascadeRun(t, items, row.word)
			if code != exitFail || !strings.Contains(out, ": grows: rep `a` -> `"+row.word+"`") || !strings.Contains(out, ": correct-text: rep `a` -> `"+row.word+"`") {
				t.Errorf("code %d, stdout:\n%s", code, out)
			}
		})
	}

	t.Run("the_mistake_noticed_on_the_next_line_does_not_hide_the_limit", func(t *testing.T) {
		// `{ x (` hides the line breaks: the `(` is noticed at the next `let`, an added
		// one, and each item below still costs its E0201.
		code, out, _ := cascadeRun(t, items, "NEXTBRACE")
		if code != exitOK || strings.Contains(out, ": grows:") || strings.Contains(out, ": correct-text:") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("an_error_on_an_added_line_fails_by_itself", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "a\n", "ONEPAD")
		if code != exitFail || strings.Contains(out, ": grows:") || !strings.Contains(out, ": correct-text: rep `a` -> `ONEPAD`") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("growing_fails_by_itself", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "a\n", "GROWS")
		if code != exitFail || strings.Contains(out, ": correct-text:") || !strings.Contains(out, ": grows: rep `a` -> `GROWS`") || !strings.Contains(out, ": none on the added lines\n") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("an_error_on_any_other_added_line_is_no_limit", func(t *testing.T) {
		code, out, _ := cascadeRun(t, items, "WIDE")
		if code != exitFail || !strings.Contains(out, ": grows: rep `a` -> `WIDE`") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("the_limit_excuses_only_its_own_lines", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "b\nc\nd\n"+"e\nfn g\n", "ELSE")
		if !strings.Contains(out, "p.ki:4:1: far: rep `e` -> `ELSE`: an error at 1:1, and the edit is on line 4\n") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
		if !strings.Contains(out, "on the known limit") || strings.Contains(out, ": grows:") {
			t.Errorf("stdout:\n%s", out)
		}
	})

	t.Run("a_line_that_starts_no_item_is_not_excused", func(t *testing.T) {
		if braceLimit([]kiDiagnostic{{Code: "E0202", Help: "close it with `}`"}}, "x\nfn f\nlet y\n")(kiDiagnostic{Code: "E0201", Message: "expected expression, found `fn`", Line: 3}) {
			t.Errorf("found `fn` on a line that starts with let")
		}
		if braceLimit([]kiDiagnostic{{Code: "E0202", Help: "close it with `}`"}}, "x\nfn f\n")(kiDiagnostic{Code: "E1", Message: "expected expression, found `fn`", Line: 2}) {
			t.Errorf("a code that is not E0201")
		}
		if braceLimit([]kiDiagnostic{{Code: "E0202", Help: "close it with `}`"}}, "x\nfn f\n")(kiDiagnostic{Code: "E0201", Message: "expected expression, found `fn`", Line: 9}) {
			t.Errorf("a line past the end of the text")
		}
		if !braceLimit([]kiDiagnostic{{Code: "E0202", Help: "close it with `}`"}}, "x\n  fn f\n")(kiDiagnostic{Code: "E0201", Message: "expected expression, found `fn`", Line: 2}) {
			t.Errorf("an indented fn starts an item")
		}
		for _, line := range []string{"pub fn f", "#[extern(x)]"} {
			word := line[:strings.IndexAny(line, " [")]
			if !braceLimit([]kiDiagnostic{{Code: "E0202", Help: "close it with `}`"}}, line+"\n")(kiDiagnostic{Code: "E0201", Message: "expected expression, found `" + word + "`", Line: 1}) {
				t.Errorf("%s starts an item", line)
			}
		}
		if braceLimit([]kiDiagnostic{{Code: "E0202", Help: "close it with `}`"}}, "fn f\n")(kiDiagnostic{Code: "E0201", Message: "expected expression, found `1`", Line: 1}) {
			t.Errorf("an E0201 about another token, on a line that starts with fn")
		}
		if braceLimit([]kiDiagnostic{{Code: "E0202", Help: "close it with `}`"}}, "fn f\n")(kiDiagnostic{Code: "E0201", Message: "expected expression, found `fn`", Line: 0}) {
			t.Errorf("line 0 is no line")
		}
		if braceLimit([]kiDiagnostic{{Code: "E0202", Help: "close it with `)`"}}, "fn f\n")(kiDiagnostic{Code: "E0201", Message: "expected expression, found `fn`", Line: 1}) {
			t.Errorf("an E0202 for a `)`")
		}
		if braceLimit([]kiDiagnostic{{Code: "E0201", Help: "close it with `}`"}}, "fn f\n")(kiDiagnostic{Code: "E0201", Message: "expected expression, found `fn`", Line: 1}) {
			t.Errorf("an E0201 whose help names `}` is no E0202")
		}
	})

	t.Run("a_far_error_may_come_first_and_is_named_by_line_and_column", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "a\nb\nc\nd\n", "FARCOL")
		if code != exitOK || !strings.Contains(out, "p.ki:4:1: far: rep `d` -> `FARCOL`: an error at 2:7, and the edit is on line 4\n") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("past_the_last_line_is_not_far_and_the_last_line_is", func(t *testing.T) {
		_, past, _ := cascadeRun(t, "a\nb\nc\nd\n", "PASTEND")
		if strings.Contains(past, ": far:") {
			t.Errorf("an error past the end, where a delimiter left open belongs, is listed:\n%s", past)
		}
		_, last, _ := cascadeRun(t, "a\nb\nc\nd\n", "ATEND")
		if !strings.Contains(last, "p.ki:1:1: far: rep `a` -> `ATEND`: an error at 4:1, and the edit is on line 1\n") {
			t.Errorf("an error on the last line is not listed:\n%s", last)
		}
	})

	t.Run("an_error_next_to_the_edit_does_not_excuse_what_grows", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "a\n", "NEARGROW")
		if code != exitFail || !strings.Contains(out, ": grows: rep `a` -> `NEARGROW`") || !strings.Contains(out, ": none on the added lines\n") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("what_grows_names_its_first_error_on_an_added_line", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "a\n", "FNPADS")
		if code != exitFail || !strings.Contains(out, ": grows: rep `a` -> `FNPADS`") || strings.Contains(out, "none on the added lines") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("one_finding_alone_fails", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "a\n", "ONCE")
		if code != exitFail || !strings.Contains(out, " 0 grow, 1 at correct text, ") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("the_only_error_may_be_far_from_the_edit", func(t *testing.T) {
		// Renaming a function gives its error where it is used: it is the mistake.
		code, out, _ := cascadeRun(t, "a\nb\nc\nd\n", "FAR")
		if code != exitOK || strings.Contains(out, "correct-text:") || strings.Contains(out, ": far:") || !strings.Contains(out, "(1: 8)") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("a_crash_is_reported", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "a\n", "ICE")
		if code != exitFail || !strings.Contains(out, "p.ki:1:1: crash: rep `a` -> `ICE`: exit 70\n") || !strings.Contains(out, "2 crash") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("a_hang_is_stopped_and_reported", func(t *testing.T) {
		old := cascadeTimeoutSeconds
		cascadeTimeoutSeconds = "1"
		t.Cleanup(func() { cascadeTimeoutSeconds = old })
		code, out, _ := cascadeRun(t, "a\n", "HANG")
		if code != exitFail || !strings.Contains(out, "p.ki:1:1: crash: ins `a` -> `HANG`: exit 124\n") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("limit_takes_an_even_sample", func(t *testing.T) {
		noCap(t)
		old := cascadeVocabulary
		cascadeVocabulary = []string{"OOPS"}
		t.Cleanup(func() { cascadeVocabulary = old })
		dir := t.TempDir()
		writeFile(t, dir, "ki", fakeKi)
		if err := os.Chmod(filepath.Join(dir, "ki"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "p.ki", "a\nb\n")
		t.Chdir(dir)
		code, out, _ := runBookx(t, "cascade", "--ki", "./ki", "--limit", "2", "p.ki")
		if code != exitOK || !strings.HasPrefix(out, "cascade p.ki: 2 mutants, ") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("the_list_of_findings_is_cut_and_says_so", func(t *testing.T) {
		noCap(t)
		old := cascadeVocabulary
		cascadeVocabulary = []string{"OPEN"}
		t.Cleanup(func() { cascadeVocabulary = old })
		dir := t.TempDir()
		writeFile(t, dir, "ki", fakeKi)
		if err := os.Chmod(filepath.Join(dir, "ki"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "p.ki", "a\nb\nc\n")
		t.Chdir(dir)
		code, out, _ := runBookx(t, "cascade", "--ki", "./ki", "--top", "1", "p.ki")
		if code != exitFail || strings.Count(out, ": grows: ") != 1 || !strings.Contains(out, "p.ki: and 5 more of grows\n") {
			t.Errorf("code %d, stdout:\n%s", code, out)
		}
	})

	t.Run("no_cap_no_run", func(t *testing.T) {
		old := capCommand
		capCommand = func(string, []string) ([]string, error) { return nil, os.ErrNotExist }
		t.Cleanup(func() { capCommand = old })
		code, out, errOut := runBookx(t, "cascade", "--ki", "./ki", "p.ki")
		if code != exitFail || out != "" || !strings.HasPrefix(errOut, "bookx cascade: ") {
			t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
		}
	})

	t.Run("usage", func(t *testing.T) {
		code, _, errOut := runBookx(t, "cascade")
		if code != exitUsage || !strings.HasPrefix(errOut, "bookx cascade: want at least 1 argument(s), found 0\nusage: bookx cascade ") {
			t.Errorf("code %d, stderr %q", code, errOut)
		}
	})
}
