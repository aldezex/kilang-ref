// bookx: not-in-book

package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// lines joins ls with "\n" and ends the text with a final newline, which is
// how a file on disk ends. The test inputs are built from interpreted strings
// on purpose: a marker written at the start of a source line of this file
// would itself be read as a marker by bookx.
func lines(ls ...string) []byte {
	return []byte(strings.Join(ls, "\n") + "\n")
}

// errStrings renders errs with ScanError.String, so a test compares the exact
// text the author will read.
func errStrings(errs []ScanError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.String()
	}
	return out
}

// checkErrs fails unless errs is exactly want: same number, same texts, same
// order. Called with no want, it checks that there are no errors at all.
func checkErrs(t *testing.T, errs []ScanError, want ...string) {
	t.Helper()
	if got := errStrings(errs); !slices.Equal(got, want) {
		t.Errorf("errors = %q, want %q", got, want)
	}
}

func TestScanRegions(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		src := lines(
			"package x",
			"// @book a.b full",
			"x := 1",
			"y := 2",
			"// @end",
			"z := 3",
		)
		got, errs := scanRegions("f.go", src)
		checkErrs(t, errs)
		want := []Region{{
			ID: "a.b", Mode: RegionFull, Path: "f.go", FirstLine: 3,
			Lines: []string{"x := 1", "y := 2"},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("regions = %+v, want %+v", got, want)
		}
	})

	t.Run("hint_text", func(t *testing.T) {
		src := lines(
			"// @book a.b hint \"write the loop\"",
			"for {",
			"}",
			"// @end",
		)
		got, errs := scanRegions("f.go", src)
		checkErrs(t, errs)
		want := []Region{{
			ID: "a.b", Mode: RegionHint, Hint: "write the loop", Path: "f.go", FirstLine: 2,
			Lines: []string{"for {", "}"},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("regions = %+v, want %+v", got, want)
		}
	})

	t.Run("hash_comments", func(t *testing.T) {
		src := lines(
			"#!/usr/bin/env bash",
			"# @book ci.local full",
			"set -e",
			"# @elide",
			"go test ./...",
			"# @end",
		)
		got, errs := scanRegions("ci/ci-local.sh", src)
		checkErrs(t, errs)
		want := []Region{{
			ID: "ci.local", Mode: RegionFull, Path: "ci/ci-local.sh", FirstLine: 3,
			Lines: []string{"set -e", "# ...", "go test ./..."},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("regions = %+v, want %+v", got, want)
		}
	})

	t.Run("elide_line", func(t *testing.T) {
		src := lines(
			"// @book a.b full",
			"first",
			"// @elide",
			"\t// @elide helper functions",
			"last",
			"// @end",
		)
		got, errs := scanRegions("f.go", src)
		checkErrs(t, errs)
		if len(got) != 1 {
			t.Fatalf("got %d regions, want 1", len(got))
		}
		// The marker's own indentation stays, so the dots line up with the
		// code around them; the text after @elide is the author's note and
		// is not shown.
		want := []string{"first", "// ...", "\t// ...", "last"}
		if !slices.Equal(got[0].Lines, want) {
			t.Errorf("lines = %q, want %q", got[0].Lines, want)
		}
	})

	t.Run("unbalanced_end", func(t *testing.T) {
		got, errs := scanRegions("f.go", lines("x", "// @end", "y"))
		checkErrs(t, errs, "f.go:2: @end without a matching @book")
		if len(got) != 0 {
			t.Errorf("got %d regions, want 0", len(got))
		}

		// A stray @end after a good region is one error, and the good
		// region is still returned.
		src := lines("// @book a.b full", "x", "// @end", "// @end")
		got, errs = scanRegions("f.go", src)
		checkErrs(t, errs, "f.go:4: @end without a matching @book")
		if len(got) != 1 || got[0].ID != "a.b" {
			t.Errorf("regions = %+v, want only a.b", got)
		}
	})

	t.Run("unclosed", func(t *testing.T) {
		src := lines("package x", "// @book a.b full", "x := 1", "y := 2")
		got, errs := scanRegions("f.go", src)
		checkErrs(t, errs, "f.go:2: region `a.b` is never closed: it needs an @end line")
		if len(got) != 0 {
			t.Errorf("got %d regions, want 0", len(got))
		}
	})

	t.Run("duplicate_id", func(t *testing.T) {
		src := lines(
			"// @book a.b full",
			"first",
			"// @end",
			"// @book a.b full",
			"second",
			"// @end",
		)
		got, errs := scanRegions("f.go", src)
		checkErrs(t, errs, "f.go:4: duplicate region id `a.b`, first defined at line 1")
		if len(got) != 1 || !slices.Equal(got[0].Lines, []string{"first"}) {
			t.Errorf("regions = %+v, want only the first a.b", got)
		}
	})

	t.Run("nested_book", func(t *testing.T) {
		// A @book inside an open region reports the open one as unclosed, and
		// the new region goes on normally: one mistake, one error.
		src := lines(
			"// @book a.b full",
			"outer",
			"// @book c.d full",
			"inner",
			"// @end",
		)
		got, errs := scanRegions("f.go", src)
		checkErrs(t, errs, "f.go:1: region `a.b` is never closed: it needs an @end line")
		if len(got) != 1 || got[0].ID != "c.d" || !slices.Equal(got[0].Lines, []string{"inner"}) {
			t.Errorf("regions = %+v, want only c.d with the line \"inner\"", got)
		}
	})

	t.Run("all_modes", func(t *testing.T) {
		src := lines(
			"// @book m.full full", "a", "// @end",
			"// @book m.hint hint \"h\"", "b", "// @end",
			"// @book m.solution solution", "c", "// @end",
			"// @book m.test test", "d", "// @end",
		)
		got, errs := scanRegions("f.go", src)
		checkErrs(t, errs)
		type row struct {
			id        string
			mode      RegionMode
			firstLine int
		}
		want := []row{
			{"m.full", RegionFull, 2},
			{"m.hint", RegionHint, 5},
			{"m.solution", RegionSolution, 8},
			{"m.test", RegionTest, 11},
		}
		if len(got) != len(want) {
			t.Fatalf("got %d regions, want %d", len(got), len(want))
		}
		for i, w := range want {
			if got[i].ID != w.id || got[i].Mode != w.mode || got[i].FirstLine != w.firstLine {
				t.Errorf("region %d = {%s %d line %d}, want {%s %d line %d}",
					i, got[i].ID, got[i].Mode, got[i].FirstLine, w.id, w.mode, w.firstLine)
			}
		}
		if len(got) == 4 && got[1].Hint != "h" {
			t.Errorf("hint = %q, want %q", got[1].Hint, "h")
		}
	})

	t.Run("bad_book_lines", func(t *testing.T) {
		rows := []struct{ marker, want string }{
			{"// @book", "f.go:1: @book needs a region id and a mode"},
			{"// @book a.b", "f.go:1: @book `a.b` needs a mode: full, hint, solution or test"},
			{"// @book ab full", "f.go:1: invalid region id `ab`: write lowercase words joined by dots, as in `lexer.line_rule`"},
			{"// @book A.b full", "f.go:1: invalid region id `A.b`: write lowercase words joined by dots, as in `lexer.line_rule`"},
			{"// @book a.b loud", "f.go:1: unknown region mode `loud`: want full, hint, solution or test"},
			{"// @book a.b hint", "f.go:1: mode `hint` needs a quoted text, as in `hint \"write the loop\"`"},
			{"// @book a.b hint write", "f.go:1: mode `hint` needs a quoted text, as in `hint \"write the loop\"`"},
			{"// @book a.b hint \"open", "f.go:1: the hint text is not a valid quoted string"},
			{"// @book a.b hint \"a\" b", "f.go:1: the hint text is not a valid quoted string"},
			{"// @book a.b full \"x\"", "f.go:1: mode `full` takes no text"},
		}
		for _, r := range rows {
			got, errs := scanRegions("f.go", lines(r.marker, "x", "// @end"))
			if g := errStrings(errs); !slices.Equal(g, []string{r.want}) {
				t.Errorf("%s: errors = %q, want %q", r.marker, g, []string{r.want})
			}
			if len(got) != 0 {
				t.Errorf("%s: got %d regions, want 0", r.marker, len(got))
			}
		}
	})

	t.Run("elide_outside_region", func(t *testing.T) {
		got, errs := scanRegions("f.go", lines("x", "// @elide", "y"))
		checkErrs(t, errs, "f.go:2: @elide outside a region")
		if len(got) != 0 {
			t.Errorf("got %d regions, want 0", len(got))
		}
	})

	t.Run("not_a_marker", func(t *testing.T) {
		// None of these is a marker, so inside a region they are all code.
		code := []string{
			"x := 1 // @book a.b full",
			"// @booking a.b full",
			"// see @book a.b full",
			"/// @book a.b full",
			"/ @book a.b full",
			"\"// @book a.b full\"",
			"// @endless",
			"// @elidex",
		}
		src := lines(slices.Concat([]string{"// @book c.d full"}, code, []string{"// @end"})...)
		got, errs := scanRegions("f.go", src)
		checkErrs(t, errs)
		if len(got) != 1 || !slices.Equal(got[0].Lines, code) {
			t.Errorf("regions = %+v, want one region holding all the lines as code", got)
		}

		// Outside a region they open nothing.
		got, errs = scanRegions("f.go", lines(code...))
		checkErrs(t, errs)
		if len(got) != 0 {
			t.Errorf("got %d regions, want 0: %+v", len(got), got)
		}
	})

	t.Run("crlf_and_bom", func(t *testing.T) {
		src := []byte("\xef\xbb\xbf// @book a.b full\r\nx := 1\r\n// @end\r\n")
		got, errs := scanRegions("f.go", src)
		checkErrs(t, errs)
		want := []Region{{
			ID: "a.b", Mode: RegionFull, Path: "f.go", FirstLine: 2,
			Lines: []string{"x := 1"},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("regions = %+v, want %+v", got, want)
		}

		// A "\r" that no "\n" follows is text, in the middle of a line and at
		// the very end of the file; only "\r\n" is a line end.
		rows := []struct {
			name string
			text string
			want []string
		}{
			{"lone cr inside a line", "a\rb\r\nc\n", []string{"a\rb", "c"}},
			{"lone cr at the end of the file", "a\rb\nc\r", []string{"a\rb", "c\r"}},
			{"crlf at the end of the file", "a\r\nc\r\n", []string{"a", "c"}},
			{"crlf and no final newline", "a\r\nc", []string{"a", "c"}},
			{"only a cr", "\r", []string{"\r"}},
			{"only a crlf", "\r\n", []string{""}},
		}
		for _, r := range rows {
			if g := splitLines([]byte(r.text)); !slices.Equal(g, r.want) {
				t.Errorf("%s: splitLines(%q) = %q, want %q", r.name, r.text, g, r.want)
			}
		}
		got, errs = scanRegions("f.go", []byte("// @book a.b full\nx\ry\r\n// @end\n"))
		checkErrs(t, errs)
		if len(got) != 1 || !slices.Equal(got[0].Lines, []string{"x\ry"}) {
			t.Errorf("regions = %+v, want one region with the line \"x\\ry\"", got)
		}
	})

	t.Run("indented_markers_and_no_final_newline", func(t *testing.T) {
		src := []byte("\t// @book a.b full\n\t\tx := 1\n\n\t// @end")
		got, errs := scanRegions("f.go", src)
		checkErrs(t, errs)
		if len(got) != 1 || !slices.Equal(got[0].Lines, []string{"\t\tx := 1", ""}) {
			t.Errorf("regions = %+v, want one region with lines [\"\\t\\tx := 1\" \"\"]", got)
		}
	})

	t.Run("scan_error_string", func(t *testing.T) {
		e := ScanError{Path: "book/ch01.md", Line: 12, Message: "boom"}
		if got, want := e.String(), "book/ch01.md:12: boom"; got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	})
}
