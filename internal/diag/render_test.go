package diag

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
)

// update makes the golden tests rewrite their files instead of comparing with
// them: go test ./internal/diag -update. Read the diff before committing it.
var update = flag.Bool("update", false, "rewrite golden files")

// wantGoldens is the number of files the human renderer tests keep in
// testdata. no_trailing_spaces fails if one is missing, so deleting a golden
// cannot make that test quietly check less.
const wantGoldens = 22

// newSource returns a SourceManager that holds src as the file "test.ki".
func newSource(src string) (*base.SourceManager, base.FileID) {
	sm := &base.SourceManager{}
	return sm, sm.Add("test.ki", []byte(src))
}

// spanOf returns the span of the first occurrence of sub in src. It fails the
// test if sub is not there, so a typo cannot silently point at offset -1.
func spanOf(t *testing.T, src, sub string) base.Span {
	t.Helper()
	i := strings.Index(src, sub)
	if i < 0 {
		t.Fatalf("%q does not occur in %q", sub, src)
	}
	return base.Span{File: 0, Lo: uint32(i), Hi: uint32(i + len(sub))}
}

// spanWithin returns the span of the first sub inside the first occurrence of
// ctx in src, for a text that occurs more than once in the file.
func spanWithin(t *testing.T, src, ctx, sub string) base.Span {
	t.Helper()
	c := spanOf(t, src, ctx)
	sp := spanOf(t, ctx, sub)
	return base.Span{File: 0, Lo: c.Lo + sp.Lo, Hi: c.Lo + sp.Hi}
}

// checkGolden compares got with testdata/<name>.golden, or rewrites that file
// when the test runs with -update.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("creating testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (run go test ./internal/diag -update to create it): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s differs\n--- got\n%s--- want\n%s", path, got, want)
	}
}

// shownLine renders an unknown-name diagnostic over the "zz" at the end of src
// (a one-line file) and returns the source line as it is printed, without the
// "1 | " prefix.
func shownLine(t *testing.T, src string) string {
	t.Helper()
	sm, _ := newSource(src)
	out := strings.Split(RenderHuman(unknownName("zz", spanOf(t, src, "zz")), sm), "\n")
	const prefix = "1 | "
	if len(out) < 4 || !strings.HasPrefix(out[3], prefix) {
		t.Fatalf("source line missing from the rendering of %q: %q", src, out)
	}
	return strings.TrimPrefix(out[3], prefix)
}

// wantPanic fails the test unless f panics with exactly the value want.
func wantPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		if got := recover(); got != want {
			t.Errorf("panicked with %v, want %q", got, want)
		}
	}()
	f()
}

// lines joins the lines of an expected rendering; every line ends with "\n".
func lines(ls ...string) string {
	return strings.Join(ls, "\n") + "\n"
}

// unknownName is the diagnostic that most of the tests render: E0301 with one
// primary label over sp.
func unknownName(name string, sp base.Span) Diagnostic {
	return NewError(E(301), "unknown name `"+name+"`", Label{Span: sp, Message: "not found in this scope"})
}

// renderLabels renders the diagnostic of unknownName("zz", primary) over src
// with one more label, "also here", over each span of secondary.
func renderLabels(src string, primary base.Span, secondary ...base.Span) string {
	sm, _ := newSource(src)
	d := unknownName("zz", primary)
	for _, sp := range secondary {
		d = d.WithSecondary(Label{Span: sp, Message: "also here"})
	}
	return RenderHuman(d, sm)
}

func TestRenderHuman(t *testing.T) {
	t.Run("single_label", func(t *testing.T) {
		sm, _ := newSource("let x = 1\nlet y = z\n")
		d := unknownName("z", base.Span{Lo: 18, Hi: 19})
		want := lines(
			"error[E0301]: unknown name `z`",
			" --> test.ki:2:9",
			"  |",
			"2 | let y = z",
			"  |         ^ not found in this scope",
		)
		if got := RenderHuman(d, sm); got != want {
			t.Errorf("RenderHuman =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("tab_and_accents_align_caret", func(t *testing.T) {
		// The tab is 4 visual columns and the accent is one column but two
		// bytes, so zz sits at byte 6, code point column 6 and visual column 9.
		sm, _ := newSource("\té = zz\n")
		d := unknownName("zz", base.Span{Lo: 6, Hi: 8})
		want := lines(
			"error[E0301]: unknown name `zz`",
			" --> test.ki:1:6",
			"  |",
			"1 |     é = zz",
			"  |         ^^ not found in this scope",
		)
		if got := RenderHuman(d, sm); got != want {
			t.Errorf("RenderHuman =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("secondary_same_line", func(t *testing.T) {
		const src = "let y = 1 + true\n"
		sm, _ := newSource(src)
		d := NewError(E(401), "mismatched types",
			Label{Span: spanOf(t, src, "true"), Message: "expected `Int`, found `Bool`"})
		// Given right to left on purpose: the renderer must sort them by column.
		d = d.WithSecondary(Label{Span: spanOf(t, src, "+"), Message: "this operator needs `Int`"})
		d = d.WithSecondary(Label{Span: spanOf(t, src, "1"), Message: "this is an `Int`"})
		checkGolden(t, "secondary_same_line", RenderHuman(d, sm))

		// Labels at the same offset keep the order in which the pass added
		// them. Thirty-six labels over three offsets, added in turns: a sort
		// that is not stable only scrambles ties in a long, unsorted list.
		sm, _ = newSource(src)
		d = NewError(E(401), "mismatched types", Label{Span: spanOf(t, src, "true"), Message: "primary"})
		head := []string{
			"error[E0401]: mismatched types",
			" --> test.ki:1:13",
			"  |",
			"1 | let y = 1 + true",
			"  |             ^^^^ primary",
		}
		var byColumn [3][]string // the expected lines under "l", "1" and "+"
		for i := range 36 {
			msg := fmt.Sprintf("added %02d", i)
			sub := []string{"l", "1", "+"}[i%3]
			d = d.WithSecondary(Label{Span: spanOf(t, src, sub), Message: msg})
			byColumn[i%3] = append(byColumn[i%3], "  | "+strings.Repeat(" ", strings.Index(src, sub))+"- "+msg)
		}
		wantLines := append(head, slices.Concat(byColumn[0], byColumn[1], byColumn[2])...)
		if got, want := RenderHuman(d, sm), lines(wantLines...); got != want {
			t.Errorf("36 labels at 3 offsets: RenderHuman =\n%s\nwant\n%s", got, want)
		}

		// A label in another file would show text of the wrong file.
		sm, _ = newSource(src)
		other := sm.Add("other.ki", []byte("fn main() {}\n"))
		d = NewError(E(401), "mismatched types", Label{Span: spanOf(t, src, "true")})
		d = d.WithSecondary(Label{Span: base.Span{File: other, Lo: 0, Hi: 2}})
		func() {
			defer func() {
				want := "diag: secondary label in file 1, primary in file 0"
				if got := recover(); got != want {
					t.Errorf("RenderHuman panicked with %v, want %q", got, want)
				}
			}()
			RenderHuman(d, sm)
		}()
	})

	t.Run("two_lines_with_gap", func(t *testing.T) {
		const src = "fn f() {\n  let a = 1\n  let b = 2\n  let c = 3\n  let a = 4\n}\n"
		sm, _ := newSource(src)
		first := spanWithin(t, src, "let a = 1", "a")
		second := spanWithin(t, src, "let a = 4", "a")
		d := NewError(E(302), "`a` is defined more than once", Label{Span: second, Message: "redefined here"})
		d = d.WithSecondary(Label{Span: first, Message: "first defined here"})
		checkGolden(t, "two_lines_with_gap", RenderHuman(d, sm))

		// Skipping exactly one line is a gap too, and the margin is as wide as
		// the largest line number shown (11), not the smallest (9).
		const near = "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nlet a = 1\nl10\nlet a = 2\n"
		sm, _ = newSource(near)
		d = NewError(E(302), "`a` is defined more than once",
			Label{Span: spanWithin(t, near, "let a = 2", "a"), Message: "redefined here"})
		d = d.WithSecondary(Label{Span: spanWithin(t, near, "let a = 1", "a"), Message: "first defined here"})
		checkGolden(t, "gap_of_one_line_with_wider_margin", RenderHuman(d, sm))
	})

	t.Run("notes_and_help", func(t *testing.T) {
		// The label is on line 10, so the line numbers are two digits wide and
		// the note and help prefixes have to follow that width.
		const src = "\n\n\n\n\n\n\n\n\nlet y = fibb(3)\n"
		sm, _ := newSource(src)
		d := Diagnostic{
			Severity: Warning,
			Code:     W(702),
			Message:  "unknown name `fibb`",
			Primary:  Label{Span: spanOf(t, src, "fibb"), Message: "not found in this scope"},
		}
		d = d.WithNote("names are case sensitive").WithNote("`fib` is defined in this file").WithHelp("did you mean `fib`?")
		checkGolden(t, "notes_and_help", RenderHuman(d, sm))
	})

	t.Run("caret_on_last_line_without_newline", func(t *testing.T) {
		const src = "fn main() {\n  zz"
		sm, _ := newSource(src)
		checkGolden(t, "caret_on_last_line_without_newline", RenderHuman(unknownName("zz", spanOf(t, src, "zz")), sm))
	})

	t.Run("invalid_byte_shown_as_replacement", func(t *testing.T) {
		// Two invalid bytes in a row: each one is its own U+FFFD.
		const src = "let \xff\xfe = zz\n"
		sm, _ := newSource(src)
		checkGolden(t, "invalid_byte_shown_as_replacement", RenderHuman(unknownName("zz", spanOf(t, src, "zz")), sm))
	})

	t.Run("control_byte_shown_as_picture", func(t *testing.T) {
		const src = "a\x01b\x7fc = zz\n"
		sm, _ := newSource(src)
		checkGolden(t, "control_byte_shown_as_picture", RenderHuman(unknownName("zz", spanOf(t, src, "zz")), sm))

		// The edges of the C0 range: 0x00 and 0x1F are pictures, 0x20 and 0x7E
		// are ordinary characters, and 0x7F has its own picture.
		rows := []struct{ b, want string }{
			{"\x00", "\u2400"},
			{"\x1f", "\u241f"},
			{" ", " "},
			{"~", "~"},
			{"\x7f", "\u2421"},
		}
		for _, row := range rows {
			got := shownLine(t, "a"+row.b+"b = zz")
			if want := "a" + row.want + "b = zz"; got != want {
				t.Errorf("%q: line shown as %q, want %q", row.b, got, want)
			}
		}
	})

	t.Run("lone_cr_shown_as_picture", func(t *testing.T) {
		const src = "a\rb = zz"
		sm, _ := newSource(src)
		checkGolden(t, "lone_cr_shown_as_picture", RenderHuman(unknownName("zz", spanOf(t, src, "zz")), sm))

		// The \r of a CRLF line end is part of the line break, not of the
		// line: it is not shown, and a span that touches the break is cut at
		// the end of the text, where the caret goes. The spans are the "\n"
		// (bytes 3 to 4) and the whole break (2 to 4) of the first line, and
		// the "cd" of the second.
		const crlf = "ab\r\ncd\r\n"
		for _, tc := range []struct {
			name  string
			span  base.Span
			pos   string
			line  string
			marks string
		}{
			{"the newline", base.Span{Lo: 3, Hi: 4}, "1:4", "1 | ab", "  |   ^"},
			{"the whole break", base.Span{Lo: 2, Hi: 4}, "1:3", "1 | ab", "  |   ^"},
			{"the next line", base.Span{Lo: 4, Hi: 6}, "2:1", "2 | cd", "  | ^^"},
		} {
			sm, _ := newSource(crlf)
			want := lines(
				"error[E0301]: unknown name `zz`",
				" --> test.ki:"+tc.pos,
				"  |",
				tc.line,
				tc.marks+" not found in this scope",
			)
			if got := RenderHuman(unknownName("zz", tc.span), sm); got != want {
				t.Errorf("%s: RenderHuman =\n%s\nwant\n%s", tc.name, got, want)
			}
		}
	})

	t.Run("c1_and_bidi_controls_shown_as_codes", func(t *testing.T) {
		// U+0085 takes two bytes and U+202E three; each is shown as 8 columns.
		const src = "\u0085x = zz\n\u202Ex = zz\n"
		sm, _ := newSource(src)
		first := spanWithin(t, src, "\u0085x = zz", "zz")
		second := spanWithin(t, src, "\u202Ex = zz", "zz")
		d := unknownName("zz", first).WithSecondary(Label{Span: second, Message: "and here"})
		checkGolden(t, "c1_and_bidi_controls_shown_as_codes", RenderHuman(d, sm))

		// The edges of every range that is shown as a code, and the character
		// just outside each of them, which is printed as it is.
		rows := []struct {
			r    rune
			want string
		}{
			{0x7F, "\u2421"}, {0x80, "<U+0080>"}, {0x9F, "<U+009F>"}, {0xA0, "\u00a0"},
			{0x061B, "\u061b"}, {0x061C, "<U+061C>"}, {0x061D, "\u061d"},
			{0x200D, "\u200d"}, {0x200E, "<U+200E>"}, {0x200F, "<U+200F>"}, {0x2010, "\u2010"},
			{0x2029, "\u2029"}, {0x202A, "<U+202A>"}, {0x202E, "<U+202E>"}, {0x202F, "\u202f"},
			{0x2065, "\u2065"}, {0x2066, "<U+2066>"}, {0x2069, "<U+2069>"}, {0x206A, "\u206a"},
		}
		for _, row := range rows {
			got := shownLine(t, "a"+string(row.r)+"b = zz")
			if want := "a" + row.want + "b = zz"; got != want {
				t.Errorf("U+%04X: line shown as %q, want %q", row.r, got, want)
			}
		}
	})

	t.Run("empty_source_line_has_no_trailing_space", func(t *testing.T) {
		sm, _ := newSource("\n")
		d := NewError(E(201), "expected expression, found newline", Label{Span: base.Span{Lo: 0, Hi: 0}, Message: "expected an expression here"})
		checkGolden(t, "empty_source_line_has_no_trailing_space", RenderHuman(d, sm))

		// A line of only spaces and tabs is shown as empty too: the cut of
		// trailing spaces leaves nothing, and "1 |" gets no space after it.
		sm, _ = newSource("   \t \n")
		d = NewError(E(201), "expected expression, found a blank line", Label{Span: base.Span{Lo: 5, Hi: 5}, Message: "expected an expression here"})
		checkGolden(t, "blank_source_line_has_no_trailing_space", RenderHuman(d, sm))

		// The marks line has the same rule when a label has no message: it
		// ends at the last mark, whether the label is primary or secondary.
		const src = "let y = 1 + true\n"
		sm, _ = newSource(src)
		d = NewError(E(401), "mismatched types", Label{Span: spanOf(t, src, "true")})
		d = d.WithSecondary(Label{Span: spanOf(t, src, "+")})
		checkGolden(t, "label_without_message_has_no_trailing_space", RenderHuman(d, sm))
	})

	t.Run("span_over_tab_marks_visual_width", func(t *testing.T) {
		// Line 1: a tab at column 1 is 4 columns wide. Line 2: a tab after two
		// letters only reaches the next multiple of 4, so it is 2 columns wide,
		// and the tab at the end of the line is not printed at all.
		const src = "\tx\nab\tc\t\n"
		sm, _ := newSource(src)
		d := NewError(E(104), "unexpected tab", Label{Span: base.Span{Lo: 0, Hi: 1}, Message: "here"})
		d = d.WithSecondary(Label{Span: spanWithin(t, src, "ab\tc", "\t"), Message: "and here"})
		checkGolden(t, "span_over_tab_marks_visual_width", RenderHuman(d, sm))
	})

	t.Run("multiline_span_marks_first_line", func(t *testing.T) {
		// The span runs from the "{" to the end of the file; only its first
		// line is shown, and it is marked from the "{" to the end of that line
		// (eleven columns, not one).
		const src = "fn f() { let a = 1\n  2\n}\n"
		sm, _ := newSource(src)
		sp := base.Span{Lo: uint32(strings.Index(src, "{")), Hi: uint32(len(src))}
		d := NewError(E(202), "unclosed delimiter", Label{Span: sp, Message: "this `{` is never closed"})
		checkGolden(t, "multiline_span_marks_first_line", RenderHuman(d, sm))

		// A line is cut like a span: one of 100 000 bytes is shown as a window
		// of at most maxShownBytes bytes around its labels, with "..." where
		// text was left out, and the marks stay under the right characters.
		// The size of the output does not depend on the size of the line.
		// longLine is the diagnostic of a one-line file whose label starts at
		// column col.
		longLine := func(col int, shown string, marks string) string {
			return lines(
				"error[E0301]: unknown name `zz`",
				" --> test.ki:1:"+fmt.Sprint(col),
				"  |",
				"1 | "+shown,
				"  | "+marks,
			)
		}
		rep := strings.Repeat
		rows := []struct{ name, got, want string }{
			{"label in the middle",
				renderLabels(rep("a", 50000)+"zz"+rep("b", 50000)+"\n", base.Span{Lo: 50000, Hi: 50002}),
				longLine(50001, "..."+rep("a", 40)+"zz"+rep("b", 40)+"...",
					rep(" ", 3+40)+"^^ not found in this scope")},
			{"label at the start",
				renderLabels("zz"+rep("b", 100000)+"\n", base.Span{Lo: 0, Hi: 2}),
				longLine(1, "zz"+rep("b", 40)+"...", "^^ not found in this scope")},
			{"label at the end",
				renderLabels(rep("a", 100000)+"zz", base.Span{Lo: 100000, Hi: 100002}),
				longLine(100001, "..."+rep("a", 40)+"zz",
					rep(" ", 3+40)+"^^ not found in this scope")},
			{"label wider than the window",
				renderLabels(rep("a", 100000)+"\n", base.Span{Lo: 0, Hi: 100000}),
				longLine(1, rep("a", maxShownBytes)+"...", rep("^", maxShownBytes)+" not found in this scope")},
			// A line of exactly maxShownBytes bytes is shown whole.
			{"line of maxShownBytes bytes",
				renderLabels(rep("a", maxShownBytes-2)+"zz\n", base.Span{Lo: maxShownBytes - 2, Hi: maxShownBytes}),
				longLine(maxShownBytes-1, rep("a", maxShownBytes-2)+"zz",
					rep(" ", maxShownBytes-2)+"^^ not found in this scope")},
			// One byte more is a window; here it ends at the label, so only
			// its start is cut.
			{"line of maxShownBytes+1 bytes",
				renderLabels(rep("a", maxShownBytes-1)+"zz\n", base.Span{Lo: maxShownBytes - 1, Hi: maxShownBytes + 1}),
				longLine(maxShownBytes, "..."+rep("a", 40)+"zz",
					rep(" ", 3+40)+"^^ not found in this scope")},
			// The window starts and ends on a character: with a 2-byte "é"
			// the margin of 40 bytes before the label falls inside one.
			{"window does not cut a character",
				renderLabels(rep("é", 50000)+"azza"+rep("é", 50000)+"\n", base.Span{Lo: 100001, Hi: 100003}),
				longLine(50002, "..."+rep("é", 19)+"azza"+rep("é", 20)+"...",
					rep(" ", 3+19+1)+"^^ not found in this scope")},
			// The cut falls inside a character, so the window ends with it
			// and is a few bytes over maxShownBytes: 201 here, because the
			// 200th byte after the start is the second of a 2-byte "é".
			{"window ends on the character it cuts",
				renderLabels("a"+rep("é", 50000)+"\n", base.Span{Lo: 1, Hi: 100001}),
				longLine(2, "a"+rep("é", 100)+"...", rep(" ", 1)+rep("^", 100)+" not found in this scope")},
			// Spaces after the last label are invisible and are not shown,
			// but only when the line ends there: here the window is cut on the
			// left and the line ends in spaces.
			{"long line ending in spaces",
				renderLabels(rep("a", 1000)+"zz"+rep(" ", 30)+"\n", base.Span{Lo: 1000, Hi: 1002}),
				longLine(1001, "..."+rep("a", 40)+"zz",
					rep(" ", 3+40)+"^^ not found in this scope")},
			// Stray continuation bytes (0x80 to 0xBF with no lead byte) are
			// not the rest of a character: moving the ends to a character
			// start must stop after three bytes, or a run of them would
			// widen the window to the whole line (first row) or empty it
			// (second row).
			{"label before a run of stray bytes",
				renderLabels("a"+rep("\x80", 1000)+"\n", base.Span{Lo: 0, Hi: 1}),
				longLine(1, "a"+rep("\uFFFD", 40+3)+"...", "^ not found in this scope")},
			{"label inside a run of stray bytes",
				renderLabels(rep("\x80", 1000)+"\n", base.Span{Lo: 500, Hi: 502}),
				longLine(501, "..."+rep("\uFFFD", 82)+"...",
					rep(" ", 3+37)+"^^ not found in this scope")},
		}
		for _, row := range rows {
			if row.got != row.want {
				t.Errorf("%s: RenderHuman =\n%.600s\nwant\n%.600s", row.name, row.got, row.want)
			}
			if len(row.got) > 2000 {
				t.Errorf("%s: the diagnostic of a line of 100 000 bytes is %d bytes long", row.name, len(row.got))
			}
		}

		// A label that is outside the window is marked at the edge of it. The
		// window is built around the primary label (the second label is too
		// far to take part), so it ends contextBytes bytes after it, and the
		// second label is marked under the "..." there.
		got := renderLabels(rep("a", 10)+"zz"+rep("a", 99988)+"yy\n", base.Span{Lo: 10, Hi: 12}, base.Span{Lo: 100000, Hi: 100002})
		want := lines(
			"error[E0301]: unknown name `zz`",
			" --> test.ki:1:11",
			"  |",
			"1 | "+rep("a", 10)+"zz"+rep("a", contextBytes)+"...",
			"  | "+rep(" ", 10)+"^^ not found in this scope",
			"  | "+rep(" ", 10+2+contextBytes)+"- also here",
		)
		if got != want {
			t.Errorf("label outside the window: RenderHuman =\n%.600s\nwant\n%.600s", got, want)
		}
	})

	t.Run("window_is_built_around_the_primary_label", func(t *testing.T) {
		// The window of a long line is built around the primary label, and
		// only around the other labels that fit with it in roomForLabels
		// bytes. In the rows below the primary label is the "zz" and the
		// second one the "yy" (or the "xx"); the second label is on the left of
		// the primary one, as the secondary labels that name an earlier place
		// are (the opening parenthesis, the first definition).
		rep := strings.Repeat
		const lead, tail = 100, 100
		// oneLine is the diagnostic of a row: the position of the primary
		// label, the line of the source as it is shown and the lines of marks.
		oneLine := func(col int, shown string, marks ...string) string {
			ls := []string{
				"error[E0301]: unknown name `zz`",
				" --> test.ki:1:" + fmt.Sprint(col),
				"  |",
				"1 | " + shown,
			}
			return lines(append(ls, marks...)...)
		}
		const primaryMark = "^^ not found in this scope"
		// gap is the number of bytes between the "yy" and the "zz" for which the
		// two labels together cover exactly the room that is left in a window
		// of maxShownBytes bytes after contextBytes of text on each side: spelt
		// out here and not as roomForLabels, so the row also checks that
		// constant.
		const gap = maxShownBytes - 2*contextBytes - 4
		rows := []struct{ name, got, want string }{
			// The first label of the line used to set the start of the window,
			// so with the second 300 bytes before the primary the window showed
			// the second and the primary mark was drawn at the right edge, under
			// the "...". Now the window is around the primary and the second
			// label is marked at the left edge, under the last "." of the
			// leading "...".
			{"second label 300 bytes before",
				renderLabels("yy"+rep("a", 298)+"zz"+rep("b", tail)+"\n", base.Span{Lo: 300, Hi: 302}, base.Span{Lo: 0, Hi: 2}),
				oneLine(301, "..."+rep("a", contextBytes)+"zz"+rep("b", contextBytes)+"...",
					"  | "+rep(" ", 3+contextBytes)+primaryMark,
					"  | "+rep(" ", 2)+"- also here")},
			// The labels cover roomForLabels bytes together: the window is
			// built around both, a whole maxShownBytes bytes, and each mark is
			// under its own text.
			{"labels covering roomForLabels bytes",
				renderLabels(rep("a", lead)+"yy"+rep("a", gap)+"zz"+rep("b", tail)+"\n",
					base.Span{Lo: lead + 2 + gap, Hi: lead + 4 + gap}, base.Span{Lo: lead, Hi: lead + 2}),
				oneLine(lead+3+gap, "..."+rep("a", contextBytes)+"yy"+rep("a", gap)+"zz"+rep("b", contextBytes)+"...",
					"  | "+rep(" ", 3+contextBytes+2+gap)+primaryMark,
					"  | "+rep(" ", 3+contextBytes)+"-- also here")},
			// One byte more between them: the second label no longer takes
			// part, and the window is the one around the primary alone.
			{"labels covering one byte more",
				renderLabels(rep("a", lead)+"yy"+rep("a", gap+1)+"zz"+rep("b", tail)+"\n",
					base.Span{Lo: lead + 3 + gap, Hi: lead + 5 + gap}, base.Span{Lo: lead, Hi: lead + 2}),
				oneLine(lead+4+gap, "..."+rep("a", contextBytes)+"zz"+rep("b", contextBytes)+"...",
					"  | "+rep(" ", 3+contextBytes)+primaryMark,
					"  | "+rep(" ", 2)+"- also here")},
			// A primary label as wide as the room is shown whole, in a window of
			// a whole maxShownBytes bytes, with a second label far to its left.
			{"primary label as wide as roomForLabels",
				renderLabels(rep("a", 300)+rep("z", 120)+rep("b", tail)+"\n", base.Span{Lo: 300, Hi: 300 + 120}, base.Span{Lo: 0, Hi: 2}),
				oneLine(301, "..."+rep("a", contextBytes)+rep("z", 120)+rep("b", contextBytes)+"...",
					"  | "+rep(" ", 3+contextBytes)+rep("^", 120)+" not found in this scope",
					"  | "+rep(" ", 2)+"- also here")},
			// A second label too wide to take part in the window that ends where
			// the window starts is wholly outside it, and is marked at the left
			// edge; one more byte, and its last byte is the first one shown, and
			// it is marked there, under that character.
			{"wide label ending where the window starts",
				renderLabels(rep("a", 300)+"zz"+rep("b", tail)+"\n", base.Span{Lo: 300, Hi: 302}, base.Span{Lo: 170, Hi: 300 - contextBytes}),
				oneLine(301, "..."+rep("a", contextBytes)+"zz"+rep("b", contextBytes)+"...",
					"  | "+rep(" ", 3+contextBytes)+primaryMark,
					"  | "+rep(" ", 2)+"- also here")},
			{"wide label ending one byte into the window",
				renderLabels(rep("a", 300)+"zz"+rep("b", tail)+"\n", base.Span{Lo: 300, Hi: 302}, base.Span{Lo: 171, Hi: 301 - contextBytes}),
				oneLine(301, "..."+rep("a", contextBytes)+"zz"+rep("b", contextBytes)+"...",
					"  | "+rep(" ", 3+contextBytes)+primaryMark,
					"  | "+rep(" ", 3)+"- also here")},
		}
		for _, row := range rows {
			if row.got != row.want {
				t.Errorf("%s: RenderHuman =\n%.700s\nwant\n%.700s", row.name, row.got, row.want)
			}
		}

		// A long line that has no primary label: the window is built around its
		// first label, the "yy", and the "xx", which with it would cover more
		// than roomForLabels bytes, is marked at the right edge. The primary
		// label is on the line before.
		src := "zz\n" + rep("a", 1000) + "yy" + rep("a", 148) + "xx" + rep("a", 1000) + "\n"
		yy := base.Span{Lo: 3 + 1000, Hi: 3 + 1002}
		xx := base.Span{Lo: 3 + 1150, Hi: 3 + 1152}
		got := renderLabels(src, base.Span{Lo: 0, Hi: 2}, xx, yy)
		want := lines(
			"error[E0301]: unknown name `zz`",
			" --> test.ki:1:1",
			"  |",
			"1 | zz",
			"  | "+primaryMark,
			"2 | ..."+rep("a", contextBytes)+"yy"+rep("a", contextBytes)+"...",
			"  | "+rep(" ", 3+contextBytes)+"-- also here",
			"  | "+rep(" ", 3+contextBytes+2+contextBytes)+"- also here",
		)
		if got != want {
			t.Errorf("line without a primary label: RenderHuman =\n%.700s\nwant\n%.700s", got, want)
		}
	})

	t.Run("fixit_replacement_diff", func(t *testing.T) {
		const src = "let y = fibb\n"
		sm, _ := newSource(src)
		sp := spanOf(t, src, "fibb")
		d := unknownName("fibb", sp).WithHelp("did you mean `fib`?").WithFixIt(Replace(sp, "fib"))
		checkGolden(t, "fixit_replacement_diff", RenderHuman(d, sm))
	})

	t.Run("fixit_insertion_diff", func(t *testing.T) {
		// The closer goes right after the `1`, not at the `}` that was found.
		const src = "fn c() -> Int { (1 }\n"
		sm, _ := newSource(src)
		one := spanOf(t, src, "1")
		d := NewError(E(201), "expected `)`, found `}`", Label{Span: spanOf(t, src, "}"), Message: "expected `)` here"}).
			WithSecondary(Label{Span: spanOf(t, src, "("), Message: "unclosed `(` opened here"}).
			WithHelp("close it with `)`").
			WithFixIt(Insert(base.Span{Lo: one.Hi}, ")"))
		checkGolden(t, "fixit_insertion_diff", RenderHuman(d, sm))
	})

	t.Run("fixit_deletion_diff", func(t *testing.T) {
		const src = "#[inline]\nfn f() {}\n"
		sm, _ := newSource(src)
		attr := spanOf(t, src, "#[inline]")
		d := NewError(E(305), "unknown attribute `inline`", Label{Span: attr, Message: "not an attribute Ki knows"}).
			WithHelp("remove it").WithFixIt(Delete(attr))
		got := RenderHuman(d, sm)
		checkGolden(t, "fixit_deletion_diff", got)
		// The patched line is empty, and an empty line has no trailing space:
		// "1 +" and nothing after it.
		if want := "\n1 - #[inline]\n1 +\n"; !strings.HasSuffix(got, want) {
			t.Errorf("RenderHuman ends %q, want it to end %q", got[max(0, len(got)-len(want)-10):], want)
		}
	})

	t.Run("two_fixits_one_line_right_to_left", func(t *testing.T) {
		// The left edit makes the line three bytes shorter and the right one
		// makes it longer than its span. Each lands on its own text only if the
		// offsets are those of the original line, whatever the order of the
		// list: applied one after the other in the order of the list, with the
		// offsets of the original, one of the two orders would shift the second
		// edit.
		const src = "x = fibb + gammaa\n"
		sm, _ := newSource(src)
		left, right := spanOf(t, src, "fibb"), spanOf(t, src, "gammaa")
		named := unknownName("fibb", left).WithHelp("rename both")
		leftFirst := named.WithFixIt(Replace(left, "f")).WithFixIt(Replace(right, "a_longer_name"))
		rightFirst := named.WithFixIt(Replace(right, "a_longer_name")).WithFixIt(Replace(left, "f"))
		got := RenderHuman(leftFirst, sm)
		checkGolden(t, "two_fixits_one_line_right_to_left", got)
		if want := "\n1 - x = fibb + gammaa\n1 + x = f + a_longer_name\n"; !strings.HasSuffix(got, want) {
			t.Errorf("left edit first: RenderHuman ends %q, want it to end %q", got[max(0, len(got)-len(want)-10):], want)
		}
		if other := RenderHuman(rightFirst, sm); other != got {
			t.Errorf("right edit first: RenderHuman =\n%s\nwant the same as with the left edit first:\n%s", other, got)
		}

		// Two insertions at one position: the later one in the list is written
		// first, as applying them one after the other from the right gives (see
		// FixIt). The same when one of them replaces the text that starts
		// there: an insertion that comes after the replacement in the list is
		// written in front of it.
		const ins = "f(x)\n"
		sm, _ = newSource(ins)
		at := spanOf(t, ins, "x")
		named = unknownName("x", at).WithHelp("write both")
		both := named.WithFixIt(Insert(at, "a, ")).WithFixIt(Insert(at, "b, "))
		if got, want := RenderHuman(both, sm), "\n1 - f(x)\n1 + f(b, a, x)\n"; !strings.HasSuffix(got, want) {
			t.Errorf("two insertions: RenderHuman ends %q, want it to end %q", got, want)
		}
		before := named.WithFixIt(Replace(at, "y")).WithFixIt(Insert(at, "a, "))
		if got, want := RenderHuman(before, sm), "\n1 - f(x)\n1 + f(a, y)\n"; !strings.HasSuffix(got, want) {
			t.Errorf("a replacement and an insertion at its start: RenderHuman ends %q, want it to end %q", got, want)
		}
	})

	t.Run("fixit_on_a_later_line", func(t *testing.T) {
		// The primary label is on line 2 and the fix-it on line 5: the code
		// part shows line 2 only, the diff shows line 5 only, and the margin is
		// one digit wide.
		const src = "l1\nlet y = fibb\nl3\nl4\nlet z = fibb\n"
		sm, _ := newSource(src)
		first := spanOf(t, src, "fibb")
		second := spanWithin(t, src, "let z = fibb", "fibb")
		d := unknownName("fibb", first).WithHelp("did you mean `fib`?").WithFixIt(Replace(second, "fib"))
		checkGolden(t, "fixit_on_a_later_line", RenderHuman(d, sm))
	})

	t.Run("fixit_widens_line_number_width", func(t *testing.T) {
		// The width of the margin comes from the largest line number shown
		// anywhere, in the code or in the diff, and it is the same in the
		// whole diagnostic.
		//
		// First direction: the primary label is on line 9 and the fix-it on
		// line 10, so the margin is two digits wide in the `-->` line and the
		// code line too, not only in the diff.
		src := strings.Repeat("l\n", 8) + "let y = fibb\nlet z = fibb\n"
		sm, _ := newSource(src)
		first := spanOf(t, src, "fibb")
		second := spanWithin(t, src, "let z = fibb", "fibb")
		d := unknownName("fibb", first).WithHelp("did you mean `fib`?").WithFixIt(Replace(second, "fib"))
		checkGolden(t, "fixit_widens_line_number_width", RenderHuman(d, sm))

		// The other direction: the fix-it is on line 9 and the primary label
		// on line 10, as when a missing closer is written at the end of the
		// previous line. The width cannot come from the fix-it lines alone:
		// the margin would be one digit wide and the `10` of the code line
		// would not fit in it.
		src = strings.Repeat("l\n", 8) + "let z = fibb\nlet y = fibb\n"
		sm, _ = newSource(src)
		fixed := spanOf(t, src, "fibb")
		primary := spanWithin(t, src, "let y = fibb", "fibb")
		d = unknownName("fibb", primary).WithHelp("did you mean `fib`?").WithFixIt(Replace(fixed, "fib"))
		checkGolden(t, "label_widens_line_number_width", RenderHuman(d, sm))
	})

	t.Run("fixits_on_two_lines_in_line_order", func(t *testing.T) {
		// The fix-its come in the order 4, 2, 4: the diff has the lines in
		// increasing order and one pair for each line, with both edits of line
		// 4 in its pair.
		const src = "a\nfibb\nc\nfibb fibb\n"
		sm, _ := newSource(src)
		line2 := spanOf(t, src, "fibb")
		line4 := spanWithin(t, src, "fibb fibb", "fibb")
		line4b := base.Span{Lo: line4.Lo + 5, Hi: line4.Hi + 5}
		d := unknownName("fibb", line2).WithHelp("rename them").
			WithFixIt(Replace(line4, "x")).WithFixIt(Replace(line2, "y")).WithFixIt(Replace(line4b, "z"))
		want := lines(
			"error[E0301]: unknown name `fibb`",
			" --> test.ki:2:1",
			"  |",
			"2 | fibb",
			"  | ^^^^ not found in this scope",
			"  = help: rename them",
			"  |",
			"2 - fibb",
			"2 + y",
			"4 - fibb fibb",
			"4 + x z",
		)
		if got := RenderHuman(d, sm); got != want {
			t.Errorf("RenderHuman =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("fixit_diff_uses_display_rules", func(t *testing.T) {
		// The same function that draws the code line draws the diff lines: a
		// tab goes to the next multiple of 4 columns (4 columns at the start,
		// 2 after "ab"), a control byte is its picture and an invalid byte is
		// U+FFFD.
		const src = "\tab\tx\x01\xff = fibb\n"
		sm, _ := newSource(src)
		sp := spanOf(t, src, "fibb")
		d := unknownName("fibb", sp).WithHelp("did you mean `fib`?").WithFixIt(Replace(sp, "fib"))
		got := RenderHuman(d, sm)
		checkGolden(t, "fixit_diff_uses_display_rules", got)
		if want := "\n1 -     ab  x\u2401\uFFFD = fibb\n1 +     ab  x\u2401\uFFFD = fib\n"; !strings.HasSuffix(got, want) {
			t.Errorf("RenderHuman ends %q, want it to end %q", got[max(0, len(got)-len(want)-10):], want)
		}
	})

	t.Run("fixit_diff_windows_long_lines", func(t *testing.T) {
		// A line of 200 000 bytes is cut in the diff like in the code part: a
		// window around the edit, with "..." at each end, in the old line and
		// in the new one. The size of the output does not depend on the line.
		//
		// The long line is the second of the file, as it is in a real file
		// (a minified one has a header before it): the offsets of an edit in
		// the file and in its line differ by the length of the lines before it,
		// and a window computed from the first would be off by that much.
		rep := strings.Repeat
		const header = "fn fib() {}\n"
		src := header + rep("a", 100000) + "fibb" + rep("b", 100000) + "\n"
		sm, _ := newSource(src)
		sp := spanOf(t, src, "fibb")
		d := unknownName("fibb", sp).WithHelp("did you mean `fib`?").WithFixIt(Replace(sp, "fib"))
		got := RenderHuman(d, sm)
		for _, want := range []string{
			"\n2 - ..." + rep("a", 40) + "fibb" + rep("b", 40) + "...\n",
			"\n2 + ..." + rep("a", 40) + "fib" + rep("b", 40) + "...\n",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("RenderHuman has no line %q", strings.TrimSpace(want))
			}
		}
		if len(got) > 2000 {
			t.Errorf("the diagnostic of a line of 200 000 bytes is %d bytes long", len(got))
		}

		// Two fix-its on the line, a hundred bytes apart, share one window that
		// goes from before the first to after the last, in the old line and in
		// the new one.
		src = header + rep("a", 1000) + "fibb" + rep("b", 96) + "fibb" + rep("c", 1000) + "\n"
		sm, _ = newSource(src)
		at := uint32(len(header)) + 1000
		first := base.Span{Lo: at, Hi: at + 4}
		second := base.Span{Lo: at + 100, Hi: at + 104}
		d = unknownName("fibb", first).WithHelp("did you mean `fib`?").
			WithFixIt(Replace(first, "fib")).WithFixIt(Replace(second, "fib"))
		got = RenderHuman(d, sm)
		for _, want := range []string{
			"\n2 - ..." + rep("a", 40) + "fibb" + rep("b", 96) + "fibb" + rep("c", 40) + "...\n",
			"\n2 + ..." + rep("a", 40) + "fib" + rep("b", 96) + "fib" + rep("c", 40) + "...\n",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("two fix-its: RenderHuman has no line %q", strings.TrimSpace(want))
			}
		}

		// The window starts contextBytes (40) bytes before the first edit. With
		// the edit at byte 40 it starts at byte 0 and nothing is hidden, so
		// there is no mark at the start; one byte later it starts at byte 1,
		// and that single hidden byte needs the mark as much as a thousand.
		for _, row := range []struct {
			before int // bytes of the line in front of the edit
			cut    bool
		}{{40, false}, {41, true}, {42, true}} {
			src = header + rep("a", row.before) + "fibb" + rep("b", 300) + "\n"
			sm, _ = newSource(src)
			sp = spanOf(t, src, "fibb")
			d = unknownName("fibb", sp).WithHelp("did you mean `fib`?").WithFixIt(Replace(sp, "fib"))
			got = RenderHuman(d, sm)
			lead := ""
			if row.cut {
				lead = "..."
			}
			for _, want := range []string{
				"\n2 - " + lead + rep("a", 40) + "fibb" + rep("b", 40) + "...\n",
				"\n2 + " + lead + rep("a", 40) + "fib" + rep("b", 40) + "...\n",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("edit at byte %d: RenderHuman has no line %q", row.before, strings.TrimSpace(want))
				}
			}
		}

		// The old line and the new one are cut together: if either is over the
		// limit both are, so that an edit that takes a line of exactly
		// maxShownBytes bytes to one more does not show the old line whole beside
		// a new line cut (text would seem to vanish in the patch), and the same
		// for one byte less. The line is `fn main() { NAME() } // xxx…`, the edit
		// replaces NAME at byte 12, and the window ends 40 bytes after it.
		const lead = "fn main() { "
		for _, row := range []struct {
			name     string
			from, to string // NAME before and after the edit
			size     int    // bytes of the line before the edit
			cut      bool   // both lines are windowed
		}{
			{"200 bytes, grows to 201", "fi", "fib", 200, true},
			{"201 bytes, shrinks to 200", "fib", "fi", 201, true},
			{"200 bytes, same size", "fi", "fx", 200, false},
			{"200 bytes, shrinks to 199", "fib", "fi", 200, false},
			{"201 bytes, same size", "fib", "fix", 201, true},
		} {
			tail := "() } // "
			tail += rep("x", row.size-len(lead+row.from+tail))
			src = header + lead + row.from + tail + "\n"
			sm, _ = newSource(src)
			sp = base.Span{Lo: uint32(len(header + lead)), Hi: uint32(len(header+lead) + len(row.from))}
			d = unknownName(row.from, sp).WithHelp("did you mean `" + row.to + "`?").WithFixIt(Replace(sp, row.to))
			got = RenderHuman(d, sm)
			shown, shownTo := lead+row.from+tail, lead+row.to+tail
			if row.cut {
				shown, shownTo = lead+row.from+tail[:40]+"...", lead+row.to+tail[:40]+"..."
			}
			for _, want := range []string{"\n2 - " + shown + "\n", "\n2 + " + shownTo + "\n"} {
				if !strings.Contains(got, want) {
					t.Errorf("%s: RenderHuman has no line %q", row.name, strings.TrimSpace(want))
				}
			}
		}
	})

	t.Run("fixit_at_the_end_of_a_crlf_line", func(t *testing.T) {
		// A fix-it may end where the text of its line ends, before the CR of a
		// CRLF: an insertion there (the closing quote of an unterminated
		// string is one) and a deletion of the last character.
		const src = "ab\r\ncd\r\n"
		sm, _ := newSource(src)
		d := unknownName("ab", base.Span{Lo: 0, Hi: 2}).WithHelp("fix both").
			WithFixIt(Insert(base.Span{Lo: 2}, "!")).WithFixIt(Delete(base.Span{Lo: 1, Hi: 2}))
		if got, want := RenderHuman(d, sm), "\n1 - ab\n1 + a!\n"; !strings.HasSuffix(got, want) {
			t.Errorf("RenderHuman ends %q, want it to end %q", got, want)
		}
	})

	t.Run("fixit_without_help_panics", func(t *testing.T) {
		const src = "let y = fibb\n"
		sm, _ := newSource(src)
		sp := spanOf(t, src, "fibb")
		d := unknownName("fibb", sp).WithFixIt(Replace(sp, "fib"))
		wantPanic(t, "diag: fix-its without a help line", func() { RenderHuman(d, sm) })
	})

	t.Run("fixit_across_lines_panics", func(t *testing.T) {
		const src = "let y = fibb\nlet z = 1\n"
		sm, _ := newSource(src)
		d := unknownName("fibb", spanOf(t, src, "fibb")).WithHelp("h")
		const msg = "diag: fix-it spans a line break"
		// One byte over the end of the text: the "\n" is in the span.
		wantPanic(t, msg, func() { RenderHuman(d.WithFixIt(Delete(base.Span{Lo: 8, Hi: 13})), sm) })
		// Across two lines.
		wantPanic(t, msg, func() { RenderHuman(d.WithFixIt(Delete(base.Span{Lo: 8, Hi: 17})), sm) })
		// The "\r" of a CRLF is part of the line break and not of the line.
		sm, _ = newSource("ab\r\ncd\r\n")
		d = unknownName("ab", base.Span{Lo: 0, Hi: 2}).WithHelp("h")
		wantPanic(t, msg, func() { RenderHuman(d.WithFixIt(Delete(base.Span{Lo: 1, Hi: 3})), sm) })
		// Starting inside the break, between the "\r" and the "\n".
		wantPanic(t, msg, func() { RenderHuman(d.WithFixIt(Insert(base.Span{Lo: 3}, "x")), sm) })
	})

	t.Run("fixit_in_another_file_panics", func(t *testing.T) {
		const src = "let y = fibb\n"
		sm, _ := newSource(src)
		other := sm.Add("other.ki", []byte("fn main() {}\n"))
		d := unknownName("fibb", spanOf(t, src, "fibb")).WithHelp("h").
			WithFixIt(Delete(base.Span{File: other, Lo: 0, Hi: 2}))
		wantPanic(t, "diag: fix-it in file 1, primary in file 0", func() { RenderHuman(d, sm) })
	})

	t.Run("overlapping_fixits_panic", func(t *testing.T) {
		const src = "let y = fibb\n"
		sm, _ := newSource(src)
		d := unknownName("fibb", spanOf(t, src, "fibb")).WithHelp("h")
		wantPanic(t, "diag: overlapping fix-its", func() {
			RenderHuman(d.WithFixIt(Delete(base.Span{Lo: 8, Hi: 11})).WithFixIt(Delete(base.Span{Lo: 10, Hi: 12})), sm)
		})
		// Edits that touch without overlapping are fine: the first ends where
		// the second starts.
		got := RenderHuman(d.WithFixIt(Delete(base.Span{Lo: 8, Hi: 10})).WithFixIt(Delete(base.Span{Lo: 10, Hi: 12})), sm)
		if want := "\n1 - let y = fibb\n1 + let y =\n"; !strings.HasSuffix(got, want) {
			t.Errorf("touching edits: RenderHuman ends %q, want it to end %q", got, want)
		}
	})

	t.Run("no_trailing_spaces", func(t *testing.T) {
		files, err := filepath.Glob(filepath.Join("testdata", "*.golden"))
		if err != nil {
			t.Fatalf("Glob: %v", err)
		}
		if len(files) != wantGoldens {
			t.Fatalf("testdata has %d golden files, want %d: %q", len(files), wantGoldens, files)
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("reading %s: %v", f, err)
			}
			if !strings.HasSuffix(string(data), "\n") {
				t.Errorf("%s does not end with a newline", f)
			}
			for i, line := range strings.Split(string(data), "\n") {
				if strings.HasSuffix(line, " ") {
					t.Errorf("%s:%d ends with a space: %q", f, i+1, line)
				}
			}
		}
	})
}

func TestRenderShort(t *testing.T) {
	t.Run("format", func(t *testing.T) {
		sm, _ := newSource("let x = 1\nlet y = z\n")
		d := unknownName("z", base.Span{Lo: 18, Hi: 19})
		d = d.WithNote("a note").WithHelp("some help").WithSecondary(Label{Span: base.Span{Lo: 0, Hi: 1}, Message: "other"})
		want := "test.ki:2:9: error[E0301]: unknown name `z`\n"
		if got := RenderShort(d, sm); got != want {
			t.Errorf("RenderShort = %q, want %q", got, want)
		}
	})
}

func TestRenderJSON(t *testing.T) {
	t.Run("line", func(t *testing.T) {
		sm, _ := newSource("let x = 1\nlet y = z\n")
		d := unknownName("z", base.Span{Lo: 18, Hi: 19})
		want := `{"severity":"error","code":"E0301","message":"unknown name ` + "`z`" + `","path":"test.ki","line":2,"col":9,` +
			`"labels":[{"primary":true,"line":2,"col":9,"message":"not found in this scope"}],"notes":[],"help":null,"fixits":[]}` + "\n"
		if got := RenderJSON(d, sm); got != want {
			t.Errorf("RenderJSON =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("escapes", func(t *testing.T) {
		sm, _ := newSource("x\n")
		d := NewError(E(104), "say \"hi\" \\ \t", Label{Span: base.Span{Lo: 0, Hi: 1}})
		want := `{"severity":"error","code":"E0104","message":"say \"hi\" \\ \t","path":"test.ki","line":1,"col":1,` +
			`"labels":[{"primary":true,"line":1,"col":1,"message":""}],"notes":[],"help":null,"fixits":[]}` + "\n"
		if got := RenderJSON(d, sm); got != want {
			t.Errorf("RenderJSON =\n%s\nwant\n%s", got, want)
		}

		// <, > and & stay as they are: the types and operators of Ki would
		// otherwise come out as \u003c and \u0026 in every message and help.
		d = NewError(E(203), "expected `Vec<Int>` & more", Label{Span: base.Span{Lo: 0, Hi: 1}}).WithHelp("write `a < b && b > c`")
		want = `{"severity":"error","code":"E0203","message":"expected ` + "`Vec<Int>`" + ` & more","path":"test.ki","line":1,"col":1,` +
			`"labels":[{"primary":true,"line":1,"col":1,"message":""}],"notes":[],"help":"write ` + "`a < b && b > c`" + `","fixits":[]}` + "\n"
		if got := RenderJSON(d, sm); got != want {
			t.Errorf("RenderJSON =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("notes_help_and_secondary_labels", func(t *testing.T) {
		const src = "let y = 1 + true\n"
		sm, _ := newSource(src)
		d := NewError(E(401), "mismatched types", Label{Span: spanOf(t, src, "true"), Message: "expected `Int`, found `Bool`"})
		// Two secondaries, added right to left: the output keeps the primary
		// first and then the order in which the pass added them.
		d = d.WithSecondary(Label{Span: spanOf(t, src, "+"), Message: "this operator needs `Int`"})
		d = d.WithSecondary(Label{Span: spanOf(t, src, "1"), Message: "this is an `Int`"})
		d = d.WithNote("first").WithNote("second").WithHelp("use a number")
		want := `{"severity":"error","code":"E0401","message":"mismatched types","path":"test.ki","line":1,"col":13,` +
			`"labels":[{"primary":true,"line":1,"col":13,"message":"expected ` + "`Int`, found `Bool`" + `"},` +
			`{"primary":false,"line":1,"col":11,"message":"this operator needs ` + "`Int`" + `"},` +
			`{"primary":false,"line":1,"col":9,"message":"this is an ` + "`Int`" + `"}],` +
			`"notes":["first","second"],"help":"use a number","fixits":[]}` + "\n"
		if got := RenderJSON(d, sm); got != want {
			t.Errorf("RenderJSON =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("fixits", func(t *testing.T) {
		const src = "let y = fibb\n"
		sm, _ := newSource(src)
		sp := spanOf(t, src, "fibb")
		d := unknownName("fibb", sp).WithHelp("did you mean `fib`?").WithFixIt(Replace(sp, "fib"))
		want := `{"severity":"error","code":"E0301","message":"unknown name ` + "`fibb`" + `","path":"test.ki","line":1,"col":9,` +
			`"labels":[{"primary":true,"line":1,"col":9,"message":"not found in this scope"}],"notes":[],` +
			`"help":"did you mean ` + "`fib`" + `?","fixits":[{"line":1,"col":9,"end_line":1,"end_col":13,"replacement":"fib"}]}` + "\n"
		if got := RenderJSON(d, sm); got != want {
			t.Errorf("RenderJSON =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("fixits_keep_order_and_count_code_points", func(t *testing.T) {
		// Positions are 1-based and count code points, as the labels do: the
		// "é" is two bytes and one column. The fix-its come out in the order
		// of the list, not of the file; an insertion has the same start and
		// end, the end is exclusive, and the replacement is escaped like any
		// string ("\"" and "<" included).
		// The file is not the first of its SourceManager: the fix-its are read
		// in their own file and not in file 0.
		const src = "é = fibb\nxx\n"
		sm := &base.SourceManager{}
		sm.Add("pad.ki", []byte("p\nq\nr\n\n\n\n"))
		id := sm.Add("test.ki", []byte(src))
		inFile := func(sp base.Span) base.Span { sp.File = id; return sp }
		fibb := inFile(spanOf(t, src, "fibb"))
		xx := inFile(spanOf(t, src, "xx"))
		d := unknownName("fibb", fibb).WithHelp("h").
			WithFixIt(Insert(xx, "\"<")).WithFixIt(Replace(fibb, "fib")).WithFixIt(Delete(xx))
		want := `{"severity":"error","code":"E0301","message":"unknown name ` + "`fibb`" + `","path":"test.ki","line":1,"col":5,` +
			`"labels":[{"primary":true,"line":1,"col":5,"message":"not found in this scope"}],"notes":[],"help":"h",` +
			`"fixits":[{"line":2,"col":1,"end_line":2,"end_col":1,"replacement":"\"<"},` +
			`{"line":1,"col":5,"end_line":1,"end_col":9,"replacement":"fib"},` +
			`{"line":2,"col":1,"end_line":2,"end_col":3,"replacement":""}]}` + "\n"
		if got := RenderJSON(d, sm); got != want {
			t.Errorf("RenderJSON =\n%s\nwant\n%s", got, want)
		}
	})
}
