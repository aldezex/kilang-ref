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
		got, isPad := cascadePadded(src, 1, 2, false)
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
		got, isPad := cascadePadded(src, 1, 1, true)
		lines := strings.Split(got, "\n")
		if lines[0] != "one" || lines[1] != "  let bookxLet0 = 0" || lines[2] != "two" {
			t.Errorf("text:\n%s", got)
		}
		if isPad(1) || !isPad(2) || isPad(3) || isPad(4) {
			t.Errorf("isPad of the statement is wrong, in:\n%s", got)
		}
	})
	t.Run("a_program_with_no_final_newline", func(t *testing.T) {
		got, _ := cascadePadded("one", 1, 1, false)
		if !strings.HasPrefix(got, "one\n\nfn bookxPad0(") {
			t.Errorf("text:\n%s", got)
		}
	})
}

func TestParseKiDiagnostics(t *testing.T) {
	t.Run("errors_of_the_json_lines", func(t *testing.T) {
		out := `{"severity":"error","code":"E0301","message":"unknown name","path":"x","line":3,"col":9,"labels":[{"primary":true,"line":3,"col":9,"message":"m"},{"primary":false,"line":2,"col":7,"message":"n"}]}
not json
{"severity":"warning","code":"W1","message":"w","line":4,"col":1}
{"severity":"error","code":"E0201","message":"other","line":5,"col":1}
`
		got := parseKiDiagnostics([]byte(out))
		if len(got) != 2 || got[0].Code != "E0301" || got[0].Line != 3 || got[0].Col != 9 || got[1].Code != "E0201" || got[1].Line != 5 {
			t.Errorf("diagnostics %+v", got)
		}
	})
}

// fakeKi writes a program that stands for `ki check --diagnostics=json FILE`:
//
//	a line with OOPS    gives one error at that line
//	a line with STRAY   gives one error at that line and another at line 1
//	a line with FAR     gives one error at line 1 and none at that line
//	after a line with OPEN, each line with bookxPad or bookxLet gives one error
//	ICE exits with 70, and HANG never ends
const fakeKi = `#!/bin/sh
f="$3"
grep -q ICE "$f" && exit 70
grep -q HANG "$f" && exec sleep 60
awk '
function diag(n) { printf "{\"severity\":\"error\",\"code\":\"E1\",\"message\":\"m\",\"line\":%d,\"col\":1}\n", n; bad = 1 }
/OPEN/ { open = 1 }
/OOPS/ { diag(NR) }
/STRAY/ { diag(NR); diag(1) }
/FAR/ { diag(1) }
open && /bookx(Pad|Let)/ { diag(NR) }
END { exit bad }
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
		want := "cascade p.ki: 6 mutants, 2 still valid, 4 with errors (1: 4), 0 grow, 0 at correct text, 0 crash\n"
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
			"p.ki:1:1: grows: rep `a` -> `OPEN`: 0 diagnostics, then 8, then 16, with 0, 4 and 8 correct additions after it: E1 m\n",
			"cascade p.ki: 3 mutants, 3 still valid, 0 with errors (none), 2 grow, 2 at correct text, 0 crash\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("want %q in:\n%s", want, out)
			}
		}
		if code != exitFail {
			t.Errorf("code %d", code)
		}
	})

	t.Run("an_error_elsewhere_besides_the_one_at_the_edit", func(t *testing.T) {
		code, out, _ := cascadeRun(t, "a\nb\nc\nd\n", "STRAY")
		if code != exitFail {
			t.Errorf("code %d", code)
		}
		if !strings.Contains(out, "p.ki:4:1: correct-text: rep `d` -> `STRAY`: an error at 1:1, and the edit is on line 4\n") {
			t.Errorf("stdout:\n%s", out)
		}
		// On line 2 the other error is next to the edit: nothing to report.
		if strings.Contains(out, "p.ki:2:1: correct-text") {
			t.Errorf("an error next to the edit is flagged:\n%s", out)
		}
	})

	t.Run("the_only_error_may_be_far_from_the_edit", func(t *testing.T) {
		// Renaming a function gives its error where it is used: it is the mistake.
		code, out, _ := cascadeRun(t, "a\nb\nc\nd\n", "FAR")
		if code != exitOK || strings.Contains(out, "correct-text:") || !strings.Contains(out, "(1: 8)") {
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
