package syntax_test

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// This file is in package syntax_test, like parser_test.go, and so uses the
// helpers of the external tests. The internal tests of the package
// (parser_expr_internal_test.go, parser_stmt_internal_test.go) have helpers of
// their own that this package cannot call. recShow, nthIndex and recE0201 are
// the external copies of show, nth and e0201: they are written out again
// because the two test packages share no helper, and sharing one would take an
// export_test.go for a few lines. Inside this file there is one helper per
// concept: recRun.at is built on nthIndex and does not search by itself.

// recRun is the result of lexing and parsing one source for TestRecover.
type recRun struct {
	m    *syntax.Module
	in   *base.Interner
	sink *diag.Sink
	id   base.FileID
	src  string
}

// recParse lexes and parses src. Every diagnostic is in the sink, the lexer's
// first.
func recParse(t *testing.T, src string) *recRun {
	t.Helper()
	m, id, in, sink := parseFile(t, src)
	return &recRun{m: m, in: in, sink: sink, id: id, src: src}
}

// text is what PrintModule writes for the module.
func (r *recRun) text() string { return syntax.PrintModule(r.m, r.in) }

// span returns the span [lo, hi) of this run's file.
func (r *recRun) span(lo, hi int) base.Span {
	return base.Span{File: r.id, Lo: uint32(lo), Hi: uint32(hi)}
}

// at returns the span of the n-th occurrence (from 0) of text in the source.
func (r *recRun) at(t *testing.T, text string, n int) base.Span {
	t.Helper()
	lo := nthIndex(t, r.src, text, n)
	return r.span(lo, lo+len(text))
}

// wantDiags fails the test unless the diagnostics of the sink are exactly want,
// in this order, field by field.
func (r *recRun) wantDiags(t *testing.T, what string, want ...diag.Diagnostic) {
	t.Helper()
	got := r.sink.All()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: %s: diagnostics\n%+v\nwant\n%+v", what, recShow(r.src), got, want)
	}
}

// wantText fails the test unless PrintModule gives exactly want. A long text is
// not printed in full: the failure says where the two first differ.
func (r *recRun) wantText(t *testing.T, what, want string) {
	t.Helper()
	got := r.text()
	if got == want {
		return
	}
	if len(got) <= 300 && len(want) <= 300 {
		t.Errorf("%s: %s printed\n%s\nwant\n%s", what, recShow(r.src), got, want)
		return
	}
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	t.Errorf("%s: %s printed %d bytes, want %d; they first differ at byte %d", what, recShow(r.src), len(got), len(want), i)
}

// recShow quotes src for a failure message, cut if it is long: some of the
// sources of this file are hundreds of kilobytes.
func recShow(src string) string {
	if len(src) <= 60 {
		return strconv.Quote(src)
	}
	return strconv.Quote(src[:30]) + "... (" + strconv.Itoa(len(src)) + " bytes)"
}

// The diagnostics of this file, written out in full and not built from the
// parser's own constants, so that a change of text, code or limit is noticed.

func recE0201(sp base.Span, message string) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error, Code: diag.E(201), Message: message,
		Primary: diag.Label{Span: sp, Message: primaryOf(message)},
	}
}

// primaryOf is the text of the primary label of an E0201 whose message is
// "expected <what>, found <token>": "expected <what> here". It is the external
// copy of expectedHere, in parser_stmt_internal_test.go: the two test packages
// cannot share a helper.
func primaryOf(message string) string {
	what, _, ok := strings.Cut(strings.TrimPrefix(message, "expected "), ", found ")
	if !ok {
		panic("not the message of an E0201: " + message)
	}
	return "expected " + what + " here"
}

// recE0201Let is the E0201 for the name that a `let` is missing: it has the
// help that no other identifier gets.
func recE0201Let(sp base.Span, message string) diag.Diagnostic {
	d := recE0201(sp, message)
	d.Help = "write the name of the variable: `let x = \u2026`"
	return d
}

// recE0201Opened is the E0201 for a closer that is not there when the token
// found does not say that the closer is what is missing: the closer is found at
// sp, and the delimiter open (`(` or `[`) at openSp is named by the secondary
// label. There is no help and no fix-it. It is the external copy of
// e0201Opened, in parser_stmt_internal_test.go: the two test packages cannot
// share a helper.
func recE0201Opened(sp base.Span, message string, openSp base.Span, open string) diag.Diagnostic {
	d := recE0201(sp, message)
	d.Secondary = []diag.Label{{Span: openSp, Message: "unclosed `" + open + "` opened here"}}
	return d
}

// recE0201Close is recE0201Opened for a token that says that the closer is
// missing: the closer goes at the offset insertAt, right after the last token
// that was read. It is the external copy of e0201Close, in
// parser_stmt_internal_test.go.
func recE0201Close(sp base.Span, message string, openSp base.Span, open string, insertAt uint32) diag.Diagnostic {
	closer := map[string]string{"(": ")", "[": "]"}[open]
	d := recE0201Opened(sp, message, openSp, open)
	d.Help = "close it with `" + closer + "`"
	d.Fixits = []diag.FixIt{{Span: base.Span{File: sp.File, Lo: insertAt, Hi: insertAt}, Replacement: closer}}
	return d
}

// e0202 is the E0202 for the delimiter open (`{`, `(` or `[`) at sp, with the
// help and the fix-it that writes its closer at the end of this run's file.
func (r *recRun) e0202(sp base.Span, open string) diag.Diagnostic {
	return r.e0202At(sp, open, uint32(len(r.src)))
}

// e0202At is e0202 with the closer written at byte offset at.
func (r *recRun) e0202At(sp base.Span, open string, at uint32) diag.Diagnostic {
	closer := map[string]string{"{": "}", "(": ")", "[": "]"}[open]
	return diag.Diagnostic{
		Severity: diag.Error, Code: diag.E(202), Message: "unclosed delimiter",
		Primary: diag.Label{Span: sp, Message: "this `" + open + "` is never closed"},
		Help:    "close it with `" + closer + "`",
		Fixits:  []diag.FixIt{{Span: base.Span{File: sp.File, Lo: at, Hi: at}, Replacement: closer}},
	}
}

// recE0203 is the E0203 of a chain of comparisons whose operators are at first
// and second, with the help and the fix-it that the chain gets (see
// chainedDiag in parser_expr_internal_test.go, which builds the same thing).
func recE0203(first, second base.Span, help, insert string) diag.Diagnostic {
	d := diag.Diagnostic{
		Severity: diag.Error, Code: diag.E(203), Message: "comparison operators cannot be chained",
		Primary:   diag.Label{Span: second, Message: "second comparison here"},
		Secondary: []diag.Label{{Span: first, Message: "first comparison here"}},
		Help:      help,
	}
	if insert != "" {
		d.Fixits = []diag.FixIt{{Span: base.Span{File: second.File, Lo: second.Lo, Hi: second.Lo}, Replacement: insert}}
	}
	return d
}

func recE0204(sp base.Span) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error, Code: diag.E(204), Message: "expression is nested too deeply",
		Primary: diag.Label{Span: sp, Message: "nesting level 257 starts here"}, Notes: []string{"the limit is 256"},
		Help: "split the expression with `let` bindings",
	}
}

// recE0205 is the E0205 for the operator or call parenthesis at sp.
func recE0205(sp base.Span) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error, Code: diag.E(205), Message: "expression chain is too long",
		Primary: diag.Label{Span: sp, Message: "operator or call number 1025 of this chain"},
		Notes:   []string{"the limit is 1024 operators and calls in one expression"},
		Help:    "split the expression with `let` bindings",
	}
}

// wrapLet puts an expression where the nesting tests read it: the initializer of
// a let in the body of main. The parser reads the body at depth 0.
func wrapLet(body string) string { return "fn main() { let x = " + body + " }" }

// wrapLetText is what PrintModule writes for wrapLet(body) when body prints as p.
func wrapLetText(p string) string { return "(fn main (params) (ret _) (block (let x _ " + p + ")))\n" }

func TestRecover(t *testing.T) {
	t.Run("missing_expression_E0201", func(t *testing.T) {
		r := recParse(t, "fn f() -> Int { 1 + }")
		r.wantDiags(t, "example", recE0201(r.at(t, "}", 0), "expected expression, found `}`"))
	})
	t.Run("unclosed_brace_E0202", func(t *testing.T) {
		r := recParse(t, "fn f() {")
		// The label sits on the `{` that is never closed, not on the end of the
		// file where the parser noticed.
		r.wantDiags(t, "example", r.e0202(r.at(t, "{", 0), "{"))
	})
	t.Run("unclosed_paren_closed_by_brace", func(t *testing.T) {
		r := recParse(t, "fn f() { (1 }")
		r.wantDiags(t, "example", recE0201Close(r.at(t, "}", 0), "expected `)`, found `}`",
			r.at(t, "(", 1), "(", r.at(t, "1", 0).Hi))
		// The `}` closes the parenthesis and then the block, so the function
		// ends where it should: nothing more is reported for it.
		r.wantText(t, "example", "(fn f (params) (ret _) (block (error)))\n")
	})
	t.Run("suppression_at_distance_two", func(t *testing.T) {
		// Counting the tokens that are not Newlines from `fn` = 0: the first
		// error is at the `=` of the first let (index 6), the second at the `=` of
		// the second (index 8). 8 - 6 = 2 < 3: the second is not reported.
		r := recParse(t, "fn f() { let =\nlet = }")
		r.wantDiags(t, "example", recE0201Let(r.at(t, "=", 0), "expected identifier, found `=`"))
		r.wantText(t, "example", "(fn f (params) (ret _) (block (error) (error)))\n")
	})
	t.Run("no_suppression_at_distance_three", func(t *testing.T) {
		// The first error is at the `=` (index 6), the second at the `}` (index
		// 9): 9 - 6 = 3, so it is reported.
		r := recParse(t, "fn f() { let =\nlet y }")
		r.wantDiags(t, "example",
			recE0201Let(r.at(t, "=", 0), "expected identifier, found `=`"),
			recE0201(r.at(t, "}", 0), "expected `=`, found `}`"))
		r.wantText(t, "example", "(fn f (params) (ret _) (block (error) (let y _ (error))))\n")
	})
	t.Run("suppression_counts_non_newline_tokens", func(t *testing.T) {
		// Four more line breaks change no index. (The lexer joins them into one
		// Newline; the point is that a Newline is not a token for this count.)
		r := recParse(t, "fn f() { let =\n\n\n\n\nlet = }")
		r.wantDiags(t, "example", recE0201Let(r.at(t, "=", 0), "expected identifier, found `=`"))
		r.wantText(t, "example", "(fn f (params) (ret _) (block (error) (error)))\n")

		// The line break after an `=` is not a token at all (the line is not
		// finished), so the source above has none for the count to include. This
		// one has: the first error is the missing `=` of `let x`, found at the
		// Newline, which is at the `x` (index 6); the Newline then sits between
		// the two errors, and the second error is the `=` of the next let (index
		// 8). 8 - 6 = 2, so it is not reported. A count that included the Newline
		// would make it 3 and report it.
		r = recParse(t, "fn f() { let x\n\n\nlet = }")
		r.wantDiags(t, "a Newline between", recE0201(r.at(t, "\n", 0), "expected `=`, found newline"))
		r.wantText(t, "a Newline between", "(fn f (params) (ret _) (block (let x _ (error)) (error)))\n")
	})
	t.Run("nesting_at_limit_ok", func(t *testing.T) {
		if syntax.MaxNesting != 256 {
			t.Fatalf("MaxNesting is %d, want 256", syntax.MaxNesting)
		}
		// 256 parentheses: the 1 inside is at depth 256 and there is no error.
		src := wrapLet(strings.Repeat("(", 256) + "1" + strings.Repeat(")", 256))
		r := recParse(t, src)
		r.wantDiags(t, "256 parentheses")
		r.wantText(t, "256 parentheses", wrapLetText("1"))
		// 257: exactly one E0204, on the parenthesis that would be the 257th.
		src = wrapLet(strings.Repeat("(", 257) + "1" + strings.Repeat(")", 257))
		r = recParse(t, src)
		r.wantDiags(t, "257 parentheses", recE0204(r.at(t, "(", 1+256)))
		r.wantText(t, "257 parentheses", wrapLetText("(error)"))
	})
	t.Run("deep_nesting_reports_E0204", func(t *testing.T) {
		src := wrapLet(strings.Repeat("(", 10000) + "1" + strings.Repeat(")", 10000))
		r := recParse(t, src)
		// The label is on the 257th parenthesis of the initializer (the first
		// "(" of the file is the one of `main()`), and the parser skipped the
		// other 9743 without recursing into them.
		r.wantDiags(t, "10000 parentheses", recE0204(r.at(t, "(", 1+256)))
		r.wantText(t, "10000 parentheses", wrapLetText("(error)"))

		// The same with a `;` inside the parentheses. The 257th is refused and
		// left for the skip, which opens it, so the `;` between it and its `)`
		// does not end the statement and nothing else is reported. A parser that
		// consumed the refused `(` would leave it out of the skip, which would
		// stop at that `;`.
		src = wrapLet(strings.Repeat("(", 257) + "a; b" + strings.Repeat(")", 257))
		r = recParse(t, src)
		r.wantDiags(t, "257 parentheses and a semicolon", recE0204(r.at(t, "(", 1+256)))
		r.wantText(t, "257 parentheses and a semicolon", wrapLetText("(error)"))
	})
	t.Run("recovers_to_next_item", func(t *testing.T) {
		r := recParse(t, "fn a() { ) }\nfn b() {}")
		if got := r.text(); !strings.Contains(got, "(fn b (params) (ret _) (block))") {
			t.Errorf("example: printed\n%s\nwant it to contain %q", got, "(fn b (params) (ret _) (block))")
		}
		r.wantDiags(t, "example", recE0201(r.at(t, ")", 1), "expected expression, found `)`"))
	})
	t.Run("error_nodes_print_as_error", func(t *testing.T) {
		r := recParse(t, "fn f() { 1 + }")
		if got := r.text(); !strings.HasSuffix(got, "(+ 1 (error))))\n") {
			t.Errorf("example: printed\n%s\nwant the tail to print as (+ 1 (error))", got)
		}
		r.wantText(t, "example", "(fn f (params) (ret _) (block (+ 1 (error))))\n")
	})

	t.Run("suppression_does_not_move_the_index", func(t *testing.T) {
		// Errors at the `=` of three lets: indices 6, 8 and 10. The second is
		// suppressed (8 - 6 = 2) and does not become the point the third is
		// measured from: 10 - 6 = 4, so the third is reported. A parser that
		// moved the index on a suppressed error would measure 10 - 8 = 2.
		r := recParse(t, "fn f() { let =\nlet =\nlet = }")
		r.wantDiags(t, "three lets",
			recE0201Let(r.at(t, "=", 0), "expected identifier, found `=`"),
			recE0201Let(r.at(t, "=", 2), "expected identifier, found `=`"))
		r.wantText(t, "three lets", "(fn f (params) (ret _) (block (error) (error) (error)))\n")
	})
	t.Run("suppression_index_of_a_line_break", func(t *testing.T) {
		// An error found at a Newline is at the token the line break follows:
		// the `y` (index 8), not the `}` after it (index 9). The first error is
		// at index 6, so this one is 2 away and not reported. A parser that put
		// it at index 9 would report it (9 - 6 = 3).
		r := recParse(t, "fn f() { let =\nlet y\n}")
		r.wantDiags(t, "newline", recE0201Let(r.at(t, "=", 0), "expected identifier, found `=`"))
		r.wantText(t, "newline", "(fn f (params) (ret _) (block (error) (let y _ (error))))\n")
		// One token further, the same kind of mistake is reported: in `let z w`
		// it is at the `w` (index 9), and 9 - 6 = 3.
		r = recParse(t, "fn f() { let =\nlet z w\n}")
		r.wantDiags(t, "one token further",
			recE0201Let(r.at(t, "=", 0), "expected identifier, found `=`"),
			recE0201(r.at(t, "w", 0), "expected `=`, found `w`"))
	})
	t.Run("every_parser_error_is_suppressed", func(t *testing.T) {
		// The rule is for the whole parser, not for E0201: an E0205 two tokens
		// after an E0203 is not reported either. The expression is still
		// abandoned: a suppressed error changes what the user sees and nothing
		// else. 1022 additions, then `< 1 < 1 < 1`: the second `<` is the
		// chained comparison (E0203), the third is the 1025th operator (E0205)
		// and lies two tokens after the second.
		src := "fn f() { 1" + strings.Repeat(" + 1", 1022) + " < 1 < 1 < 1 }"
		r := recParse(t, src)
		r.wantDiags(t, "E0203 then E0205",
			recE0203(r.at(t, "<", 0), r.at(t, "<", 1), "use `&&`: `a < b && b < c`", "&& 1 "))
		r.wantText(t, "E0203 then E0205", "(fn f (params) (ret _) (block (error)))\n")

		// The same for an E0204 after an E0201. The body of the function is at
		// depth 0 and the 255 blocks take it to 255. `a b` is the mistake (a
		// missing separator, at the `b`), and in `b { { } }` the first `{` takes
		// the depth to 256, the limit, so the second `{`, two tokens after the
		// `b`, is the E0204.
		src = "fn f() { " + strings.Repeat("{ ", 255) + "a b { { } } " + strings.Repeat("} ", 255) + "}"
		r = recParse(t, src)
		r.wantDiags(t, "E0201 then E0204", recE0201(r.at(t, "b", 0), "expected newline, found `b`"))
	})

	t.Run("statement_sync_stops_at_the_end_of_the_statement", func(t *testing.T) {
		// After a mistake inside a statement the parser skips to where the
		// statement ends and reads on from there (see syncStatement): the next
		// Newline or `;` that is not inside brackets opened by the skipped
		// tokens, a `}` that closes no `{` opened by them (even if a `(` or a
		// `[` of theirs is still open), or a `let` that begins a line outside
		// those brackets. Every source has one mistake: the `)` where an
		// expression should be, after `1 +`. In four rows a `let` is skipped like
		// any other token: inside brackets, or in the middle of a line.
		rows := []struct{ name, src, want string }{
			{"newline", "fn f() { 1 + )\n g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			{"semicolon", "fn f() { 1 + ) ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			{"closing brace", "fn f() { 1 + ) }\nfn g() {}", "(fn f (params) (ret _) (block (+ 1 (error))))\n(fn g (params) (ret _) (block))\n"},
			{"the rest of the line is skipped", "fn f() { 1 + ) 2 3 x y\n g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			// A `;`, a line break or a `let` inside brackets is not the end.
			{"semicolon in parentheses", "fn f() { 1 + ) (a; b) ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			{"semicolon in brackets", "fn f() { 1 + ) [a; b] ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			{"semicolon in braces", "fn f() { 1 + ) { a; b } ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			{"line break in braces", "fn f() { 1 + ) { a\n b } ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			{"braces in braces", "fn f() { 1 + ) { { a; } ; b } ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			{"let in braces", "fn f() { 1 + ) { let y = 2; y } ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			{"let in parentheses", "fn f() { 1 + ) (let y = 2) ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			{"let that begins a line, in braces", "fn f() { 1 + ) { a\n let y = 2\n y } ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			// A `let` ends the skip only when it begins a line. The lexer puts no
			// Newline in front of it when the line before is incomplete, as here,
			// after the `+`; a `let` in the middle of a line is part of the line.
			{"let that begins a line", "fn f() { 1 + ) 2 +\n let y = 3\n y }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) (let y _ 3) y))\n"},
			{"let in the middle of a line", "fn f() { 1 + ) 2 let y = 3\n y }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) y))\n"},
			// A closing bracket with no partner in the skipped text goes by
			// without ending anything or lowering any count.
			{"closers with no partner", "fn f() { 1 + ) ) ] ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			{"closers with no partner, then a bracket", "fn f() { 1 + ) ] (a; b) ; g }", "(fn f (params) (ret _) (block (expr (+ 1 (error))) g))\n"},
			// A closing bracket closes a partner of its own kind only.
			{"a bracket does not close a parenthesis", "fn f() { 1 + ) ( a ] ; g }", "(fn f (params) (ret _) (block (+ 1 (error))))\n"},
			// The `}` of the block closes a parenthesis that the skipped text left
			// open, and is not consumed.
			{"brace closes an open parenthesis", "fn f() { 1 + ) ( a }\nfn g() {}", "(fn f (params) (ret _) (block (+ 1 (error))))\n(fn g (params) (ret _) (block))\n"},
			{"brace closes an open bracket", "fn f() { 1 + ) [ a }\nfn g() {}", "(fn f (params) (ret _) (block (+ 1 (error))))\n(fn g (params) (ret _) (block))\n"},
		}
		for _, row := range rows {
			r := recParse(t, row.src)
			r.wantText(t, row.name, row.want)
			r.wantDiags(t, row.name, recE0201(r.at(t, ")", 1), "expected expression, found `)`"))
		}

		// The `let` itself is the mistake: it is where an expression should be,
		// and the skip starts on it. It is in the middle of its line, so it is
		// skipped with the rest of the line, and what comes after it is not
		// reported. `if let` is what a reader used to Rust or Swift writes; a
		// stop at the `let` would read `let y = x` as a statement, and then
		// report the `{` after it and the `else` after that. The last row is the
		// price: the `let` of `let y = 1` is not in the tree.
		for _, row := range []struct {
			name, src string
			n         int // which `let` of the source is the mistake
			want      string
		}{
			{"if let", "fn f(x: Int) -> Int {\n  if let y = x { y } else { 0 }\n}", 0,
				"(fn f (params (x Int)) (ret Int) (block (if (error) (error))))\n"},
			{"let in a call", "fn f() {\n  foo(let x = 1)\n}", 0,
				"(fn f (params) (ret _) (block (error)))\n"},
			{"let after a let", "fn f() { let x = let y = 1 }", 1,
				"(fn f (params) (ret _) (block (let x _ (error))))\n"},
		} {
			r := recParse(t, row.src)
			r.wantDiags(t, row.name, recE0201(r.at(t, "let", row.n), "expected expression, found `let`"))
			r.wantText(t, row.name, row.want)
		}
	})
	t.Run("sync_leaves_the_next_statement_alone", func(t *testing.T) {
		// Everything after the end of a broken statement is read as usual: its
		// own mistakes are reported when they are far enough from the first.
		r := recParse(t, "fn f() {\n  let a = )\n  let b = 1\n  let c = )\n  b\n}")
		r.wantDiags(t, "two mistakes",
			recE0201(r.at(t, ")", 1), "expected expression, found `)`"),
			recE0201(r.at(t, ")", 2), "expected expression, found `)`"))
		r.wantText(t, "two mistakes", "(fn f (params) (ret _) (block (let a _ (error)) (let b _ 1) (let c _ (error)) b))\n")
	})
	t.Run("expected_newline_keeps_the_token", func(t *testing.T) {
		// A token after a complete statement with no separator is not part of a
		// broken statement: it starts the next one, as the grammar says, and is
		// not skipped.
		r := recParse(t, "fn f() { f() x\n y }")
		r.wantDiags(t, "x", recE0201(r.at(t, "x", 0), "expected newline, found `x`"))
		r.wantText(t, "x", "(fn f (params) (ret _) (block (expr (call f)) (expr x) y))\n")
	})

	t.Run("item_sync_stops_at_the_next_item", func(t *testing.T) {
		// A token that cannot start an item, or a signature that goes wrong,
		// is skipped up to the next token that can start an item at depth 0.
		rows := []struct{ name, src, want string }{
			{"fn", "1 2 3 4 fn g() {}", "(error)\n(fn g (params) (ret _) (block))\n"},
			{"pub", "1 2 3 4 pub fn g() {}", "(error)\n(pub fn g (params) (ret _) (block))\n"},
			{"attribute", "1 2 3 4 #[a] fn g() {}", "(error)\n(fn g (attrs (a)) (params) (ret _) (block))\n"},
			{"signature", "fn f(x y) {}\nfn g() {}", "(error)\n(fn g (params) (ret _) (block))\n"},
			// Brackets opened in the skipped text hide what is inside them.
			{"in braces", "1 { fn g() {} } 2 3 fn h() {}", "(error)\n(fn h (params) (ret _) (block))\n"},
			{"in parentheses", "1 ( fn g() {} ) 2 3 fn h() {}", "(error)\n(fn h (params) (ret _) (block))\n"},
			{"in brackets", "1 [ fn g() {} ] 2 3 fn h() {}", "(error)\n(fn h (params) (ret _) (block))\n"},
			// A closing bracket with no partner is skipped like any token.
			{"closers with no partner", "} ) ] fn g() {}", "(error)\n(fn g (params) (ret _) (block))\n"},
		}
		for _, row := range rows {
			r := recParse(t, row.src)
			r.wantText(t, row.name, row.want)
			if n := len(r.sink.All()); n != 1 {
				t.Errorf("%s: %s: %d diagnostics, want exactly 1: %v", row.name, recShow(row.src), n, r.sink.All())
			}
		}
		// The words that start an item in a later part stop the skip too. The
		// parser cannot read such an item yet, so each one is an error of its
		// own: the one at the word, four tokens after the first.
		for _, word := range []string{"type", "trait", "impl", "effect", "import"} {
			src := "1 2 3 4 " + word + " 5 6"
			r := recParse(t, src)
			r.wantDiags(t, word,
				recE0201(r.at(t, "1", 0), "expected item, found `1`"),
				recE0201(r.at(t, word, 0), "expected item, found `"+word+"`"))
			r.wantText(t, word, "(error)\n(error)\n")
		}
		// And they do not inside brackets.
		for _, word := range []string{"type", "trait", "impl", "effect", "import"} {
			src := "1 { " + word + " } 2 3 4 fn g() {}"
			r := recParse(t, src)
			r.wantText(t, word+" in braces", "(error)\n(fn g (params) (ret _) (block))\n")
			if n := len(r.sink.All()); n != 1 {
				t.Errorf("%s in braces: %d diagnostics, want exactly 1: %v", word, n, r.sink.All())
			}
		}
	})

	t.Run("unclosed_delimiters_E0202", func(t *testing.T) {
		// One row for every place that holds a delimiter open. At the end of the
		// file the parser reports E0202 on the delimiter and not E0201 on the end
		// of the file. When several are open it reports the innermost only: the
		// ones outside are noticed at the same token, which is less than 3 away
		// from the error that was just reported.
		rows := []struct {
			src   string
			open  string
			token string // the delimiter's text, and which occurrence of it
			n     int
		}{
			{"fn f() {", "{", "{", 0},
			{"fn f() { 1", "{", "{", 0},
			{"fn f() { let x = 1", "{", "{", 0},
			{"fn f() { if a { 1 }", "{", "{", 0},
			{"fn f() { if a {", "{", "{", 1},
			{"fn f() { if a { 1 } else {", "{", "{", 2},
			{"fn f() { { 1", "{", "{", 1},
			{"fn f() { (1", "(", "(", 1},
			{"fn f() { g(1", "(", "(", 1},
			{"fn f() { g(h(1", "(", "(", 2},
			{"fn f(x", "(", "(", 0},
			{"fn f() -> (", "(", "(", 1},
			{"fn f(x: (", "(", "(", 1},
			{"#[a", "[", "[", 0},
			{`#[a("x"`, "(", "(", 0},
		}
		for _, row := range rows {
			r := recParse(t, row.src)
			r.wantDiags(t, row.src, r.e0202(r.at(t, row.token, row.n), row.open))
		}
		// An expression abandoned for being too long is skipped up to the end of
		// the statement, which here is the end of the file. The parenthesis that
		// holds the if is then open at the end of the file, but the group or the
		// call is abandoned too and is not reported: the mistake is the E0205,
		// and the one other thing wrong is the `{` of the function.
		for _, open := range []string{"(", "g("} {
			src := "fn f() { " + open + "if 1" + strings.Repeat(" + 1", 1025) + " { x } else { y }"
			r := recParse(t, src)
			lo := nthIndex(t, src, "+", 1024)
			r.wantDiags(t, open+" abandoned, then the end of the file",
				recE0205(r.span(lo, lo+1)),
				r.e0202(r.at(t, "{", 0), "{"))
			r.wantText(t, open+" abandoned, then the end of the file", "(fn f (params) (ret _) (block (error)))\n")
		}

		// An unclosed delimiter that something else closes is a plain E0201.
		for _, src := range []string{"fn f() { (1 }", "fn f() { g(1 }", "fn f(x }", "#[a }"} {
			r := recParse(t, src)
			if n := r.sink.Count(diag.E(202)); n != 0 {
				t.Errorf("%s: %d E0202, want none: %v", recShow(src), n, r.sink.All())
			}
		}
	})

	t.Run("error_token_is_reported_once", func(t *testing.T) {
		// A character the lexer rejects is an Error token, and the lexer has
		// reported it (E0104). The parser finds it where it wanted something
		// else, recovers, and says nothing more about it: one mistake, one
		// diagnostic.
		rows := []struct{ name, src, want string }{
			{"as an expression", "fn f() { $ }", "(fn f (params) (ret _) (block (error)))\n"},
			{"as an operand", "fn f() { 1 + $ }", "(fn f (params) (ret _) (block (+ 1 (error))))\n"},
			{"as a name", "fn f() { let $ = 1 }", "(fn f (params) (ret _) (block (error)))\n"},
			{"as the closing parenthesis", "fn f() { g(1 $) }", "(fn f (params) (ret _) (block (error)))\n"},
			{"as an item", "$ fn g() {}", "(error)\n(fn g (params) (ret _) (block))\n"},
			{"in a signature", "fn f($) {}\nfn g() {}", "(error)\n(fn g (params) (ret _) (block))\n"},
		}
		for _, row := range rows {
			r := recParse(t, row.src)
			diags := r.sink.All()
			if len(diags) != 1 || diags[0].Code != diag.E(104) {
				t.Errorf("%s: %s: diagnostics %v, want exactly the lexer's E0104", row.name, recShow(row.src), diags)
			}
			r.wantText(t, row.name, row.want)
		}
		// A mistake of the parser after an Error token is still reported: the
		// silence is for the token and nothing else.
		r := recParse(t, "fn f() { $; 1 + }")
		if got := r.sink.All(); len(got) != 2 || got[0].Code != diag.E(104) || got[1].Code != diag.E(201) {
			t.Errorf("%s: diagnostics %v, want the lexer's E0104 and the parser's E0201", recShow(r.src), got)
		}
		r.wantText(t, "after an Error token", "(fn f (params) (ret _) (block (expr (error)) (+ 1 (error))))\n")
	})

	t.Run("nesting_limit_of_each_construct", func(t *testing.T) {
		// Each construct that nests an expression counts one level: a
		// parenthesis, a call argument, the operand of a prefix operator, the
		// condition of an if, the branches of an if (the then branch, the else
		// branch and the else-if) and a block. With d levels around a 1, d = 256
		// is accepted and reads as it was written; d = 257 is exactly one E0204,
		// with its label on the token that would be at depth 257, and the
		// statement becomes an error node; the same for 10000, which the parser
		// does not follow down.
		type kind struct {
			name  string
			build func(d int) string
			// print is what PrintModule writes for the initializer built with d
			// levels when nothing goes wrong; failPrint, when a level is refused.
			print     func(d int) string
			failPrint string
			// The label sits on the first token that is read at depth 257. With
			// 257 levels failTok is that token, the failN-th occurrence (from 0)
			// of it in the initializer; with more levels the text up to there is
			// the same, and the token is failBig (the same as failTok if empty)
			// because the innermost 1 is further on.
			failTok string
			failN   int
			failBig string
		}
		rep := strings.Repeat
		// Where the refused level is inside the block of a statement, that
		// statement is the one that is abandoned, and the blocks around it keep
		// what they had read; where it is not, the whole initializer is.
		kinds := []kind{
			{name: "parentheses",
				build:     func(d int) string { return rep("(", d) + "1" + rep(")", d) },
				print:     func(int) string { return "1" },
				failPrint: "(error)",
				failTok:   "(", failN: 256},
			// The `()` is a parenthesis like the others: it takes a level while
			// it is read, though there is nothing inside it.
			{name: "parentheses, the innermost a unit",
				build:     func(d int) string { return rep("(", d-1) + "()" + rep(")", d-1) },
				print:     func(int) string { return "()" },
				failPrint: "(error)",
				failTok:   "(", failN: 256},
			{name: "call arguments",
				build:     func(d int) string { return rep("f(", d) + "1" + rep(")", d) },
				print:     func(d int) string { return rep("(call f ", d) + "1" + rep(")", d) },
				failPrint: "(error)",
				failTok:   "1", failN: 0, failBig: "f"},
			{name: "prefix operators",
				build: func(d int) string {
					var b strings.Builder
					for i := range d {
						b.WriteString([]string{"- ", "! "}[i%2])
					}
					return b.String() + "1"
				},
				print: func(d int) string {
					var open, closing strings.Builder
					for i := range d {
						open.WriteString([]string{"(neg ", "(not "}[i%2])
						closing.WriteString(")")
					}
					return open.String() + "1" + closing.String()
				},
				failPrint: "(error)",
				failTok:   "1", failN: 0, failBig: "!"},
			{name: "blocks with statements",
				build:     func(d int) string { return rep("{ x; ", d) + "1" + rep(" }", d) },
				print:     func(d int) string { return rep("(block (expr x) ", d) + "1" + rep(")", d) },
				failPrint: rep("(block (expr x) ", 256) + "(error)" + rep(")", 256),
				failTok:   "{", failN: 256},
			{name: "if conditions",
				build:     func(d int) string { return rep("if ", d) + "1" + rep(" { 1 } else { 2 }", d) },
				print:     func(d int) string { return rep("(if ", d) + "1" + rep(" (block 1) (block 2))", d) },
				failPrint: "(error)",
				failTok:   "1", failN: 0, failBig: "if"},
			{name: "then branches",
				build:     func(d int) string { return rep("if t { ", d) + "1" + rep(" }", d) },
				print:     func(d int) string { return rep("(if t (block ", d) + "1" + rep("))", d) },
				failPrint: rep("(if t (block ", 256) + "(error)" + rep("))", 256),
				failTok:   "t", failN: 256},
			{name: "else branches",
				build:     func(d int) string { return rep("if t { 1 } else { ", d) + "1" + rep(" }", d) },
				print:     func(d int) string { return rep("(if t (block 1) (block ", d) + "1" + rep("))", d) },
				failPrint: rep("(if t (block 1) (block ", 256) + "(error)" + rep("))", 256),
				failTok:   "t", failN: 256},
			// d ifs, each the else of the one before: the last one is at depth
			// d - 1, and its condition and its then branch one level deeper.
			{name: "else-if chain (d ifs)",
				build:     func(d int) string { return "if t { 1 }" + rep(" else if t { 1 }", d-1) },
				print:     func(d int) string { return rep("(if t (block 1) ", d-1) + "(if t (block 1))" + rep(")", d-1) },
				failPrint: "(error)",
				failTok:   "t", failN: 256},
		}
		for _, k := range kinds {
			ok := recParse(t, wrapLet(k.build(256)))
			ok.wantDiags(t, k.name+", 256 levels")
			ok.wantText(t, k.name+", 256 levels", wrapLetText(k.print(256)))
			// Where the refused token starts: the same offset for every d, since
			// the text in front of it is the same.
			offset := len("fn main() { let x = ") + nthIndex(t, k.build(257), k.failTok, k.failN)
			for _, d := range []int{257, 10000} {
				tok := k.failTok
				if d > 257 && k.failBig != "" {
					tok = k.failBig
				}
				r := recParse(t, wrapLet(k.build(d)))
				what := k.name + ", " + strconv.Itoa(d) + " levels"
				r.wantDiags(t, what, recE0204(r.span(offset, offset+len(tok))))
				r.wantText(t, what, wrapLetText(k.failPrint))
			}
		}
	})
	t.Run("nesting_levels_of_different_constructs_add_up", func(t *testing.T) {
		// A parenthesis, a call argument, a block and a prefix operator, in turn.
		// They share one count: 256 levels of the four together are accepted,
		// 257 are one E0204.
		build := func(d int) string {
			open := []string{"(", "f(", "{ ", "- "}
			closing := []string{")", ")", " }", ""}
			var head, tail strings.Builder
			for i := range d {
				head.WriteString(open[i%4])
			}
			for i := d - 1; i >= 0; i-- {
				tail.WriteString(closing[i%4])
			}
			return head.String() + "1" + tail.String()
		}
		r := recParse(t, wrapLet(build(256)))
		r.wantDiags(t, "256 levels")
		if strings.Contains(r.text(), "(error)") {
			t.Errorf("256 levels printed %s, want no error node", r.text())
		}
		r = recParse(t, wrapLet(build(257)))
		if got := r.sink.All(); len(got) != 1 || got[0].Code != diag.E(204) {
			t.Errorf("257 levels: diagnostics %v, want exactly one E0204", got)
		}
		// The refused level is in a block, whose statement is the one abandoned.
		if !strings.Contains(r.text(), "(error)") {
			t.Errorf("257 levels printed %s, want an error node", r.text())
		}
	})
	t.Run("nesting_depth_is_released", func(t *testing.T) {
		// 300 constructs of a kind one after the other are 300 times one level,
		// not 300 levels: leaving a construct gives its level back.
		rep := strings.Repeat
		rows := []struct{ name, src string }{
			{"parentheses", "fn main() { let x = " + rep("(1) + ", 299) + "(1) }"},
			{"call arguments", "fn main() { f(" + rep("1, ", 299) + "1) }"},
			{"prefix operators", "fn main() { let x = " + rep("- 1 + ", 299) + "- 1 }"},
			{"blocks", "fn main() {\n" + rep("{ 1 }\n", 300) + "}"},
			{"ifs", "fn main() {\n" + rep("if t { 1 } else { 2 }\n", 300) + "}"},
			{"else-ifs", "fn main() {\n" + rep("if t { 1 } else if t { 2 } else { 3 }\n", 300) + "}"},
			// Nested ones inside a long run: the levels of the inside are
			// given back as well.
			{"nested, in a run", "fn main() {\n" + rep("{ { { 1 } } }\n", 300) + "}"},
		}
		for _, row := range rows {
			r := recParse(t, row.src)
			r.wantDiags(t, row.name)
			if strings.Contains(r.text(), "(error)") {
				t.Errorf("%s: printed an error node", row.name)
			}
		}

		// Giving back a level that was never taken is as wrong as not giving it
		// back: the count would go below zero and the limit would be 257 or more
		// for whatever comes next. One of each construct, then 256 parentheses
		// (accepted) and 257 (the E0204), each in a statement of its own.
		const each = "fn main() {\n () \n (1) \n f(1) \n - 1 \n { 1 } \n if t { 1 } else if t { 2 } else { 3 } \n"
		fits := strings.Repeat("(", 256) + "1" + strings.Repeat(")", 256)
		over := strings.Repeat("(", 257) + "1" + strings.Repeat(")", 257)
		r := recParse(t, each+" "+fits+"\n}")
		r.wantDiags(t, "256 after one of each")
		if strings.Contains(r.text(), "(error)") {
			t.Errorf("256 after one of each: printed an error node")
		}
		src := each + " " + over + "\n}"
		r = recParse(t, src)
		r.wantDiags(t, "257 after one of each", recE0204(r.span(nthIndex(t, src, "(", strings.Count(each, "(")+256), nthIndex(t, src, "(", strings.Count(each, "(")+256)+1)))
		if got := r.text(); !strings.Contains(got, "(error)") {
			t.Errorf("257 after one of each: printed %s, want an error node", got)
		}
	})
	t.Run("nesting_overflow_does_not_leak_depth", func(t *testing.T) {
		// A statement that is too deep is abandoned up to the end of its line,
		// and the next statement starts again from depth 0: 256 parentheses in
		// the line below are accepted, and so is the name after them. The depth
		// is not left too high (`let b` would overflow) and not too low either:
		// a third line that overflows again is reported again, which it would
		// not be if the first overflow had taken a level back that it never
		// took. The two overflowing lines are far enough apart for the
		// suppression of nearby mistakes not to hide the second.
		over := strings.Repeat("(", 257) + "1" + strings.Repeat(")", 257)
		fits := strings.Repeat("(", 256) + "1" + strings.Repeat(")", 256)
		src := "fn f() {\n let a = " + over + " + 2 + 3\n let b = " + fits + "\n let c = " + over + " + 2 + 3\n g\n}"
		r := recParse(t, src)
		// The `(` that overflows is number 257 of its line. Before the first
		// line there is the `(` of `fn f()`; before the third one, the 257 of
		// the first line and the 256 of the second.
		r.wantDiags(t, "overflow, fit, overflow",
			recE0204(r.at(t, "(", 1+256)),
			recE0204(r.at(t, "(", 1+257+256+256)))
		r.wantText(t, "overflow, fit, overflow", "(fn f (params) (ret _) (block (let a _ (error)) (let b _ 1) (let c _ (error)) g))\n")
	})
	t.Run("nesting_counts_through_nested_blocks", func(t *testing.T) {
		// The statements of a nested block start at the depth of the block: a
		// block is one level, and the parentheses in its statements come on top
		// of it, so 256 of them overflow. The statement is abandoned up to its
		// own line break, and the block goes on with the next one.
		inner := strings.Repeat("(", 256) + "1" + strings.Repeat(")", 256)
		src := "fn f() {\n let a = { " + inner + "\n g }\n h\n}"
		r := recParse(t, src)
		r.wantDiags(t, "256 in a block", recE0204(r.at(t, "(", 1+255)))
		r.wantText(t, "256 in a block", "(fn f (params) (ret _) (block (let a _ (block (expr (error)) g)) h))\n")
		// 255 fit.
		inner = strings.Repeat("(", 255) + "1" + strings.Repeat(")", 255)
		r = recParse(t, "fn f() {\n let a = { "+inner+"\n g }\n h\n}")
		r.wantDiags(t, "255 in a block")
		r.wantText(t, "255 in a block", "(fn f (params) (ret _) (block (let a _ (block (expr 1) g)) h))\n")
	})
	t.Run("unclosed_brace_swallows_the_next_items", func(t *testing.T) {
		// A `{` that is never closed leaves the items after it inside its
		// block, and the block reads them as its statements. A `fn` is not a
		// statement the parser can read yet, so each line that begins an item
		// is an E0201 of its own and the line is skipped; the E0202 on the `{`
		// comes last, from the end of the file. The block does not end at a
		// `fn`, because the language allows a function inside a block, so one
		// missing `}` costs a diagnostic for each line that begins an item
		// after it, and those items are not in the tree. The cost has a
		// ceiling, at the end of this test.
		const head = "fn a() {\n  1\n\n"
		r := recParse(t, head+"fn b() { 2 }\nfn c() { 3 }\n")
		r.wantDiags(t, "two items after the brace",
			recE0201(r.at(t, "fn", 1), "expected expression, found `fn`"),
			recE0201(r.at(t, "fn", 2), "expected expression, found `fn`"),
			r.e0202(r.at(t, "{", 0), "{"))
		r.wantText(t, "two items after the brace", "(fn a (params) (ret _) (block (expr 1) (expr (error)) (error)))\n")

		src := head
		for _, name := range []string{"b", "c", "d", "e", "f"} {
			src += "fn " + name + "() { 2 }\n"
		}
		r = recParse(t, src)
		var want []diag.Diagnostic
		for i := 1; i <= 5; i++ {
			want = append(want, recE0201(r.at(t, "fn", i), "expected expression, found `fn`"))
		}
		want = append(want, r.e0202(r.at(t, "{", 0), "{"))
		r.wantDiags(t, "five items after the brace", want...)
		if n := len(r.m.Items); n != 1 {
			t.Errorf("five items after the brace: %d items in the module, want 1 (the others are inside the block of a)", n)
		}

		// The cost is for each line that begins an item, and not for each
		// item: an attribute on a line of its own is one more. Two items give
		// three E0201, on the `#`, on the `pub` of the next line and on the
		// `fn` of c, and each line is skipped to its end, `#[extern("k")]`
		// included. A block that ended at the `#` would report none of the
		// three, and one that ended at the `pub`, only the first.
		const attr = "an attribute on a line of its own"
		r = recParse(t, head+"#[extern(\"k\")]\npub fn b(x: Int)\n\nfn c() { 2 }\n")
		r.wantDiags(t, attr,
			recE0201(r.at(t, "#", 0), "expected expression, found `#`"),
			recE0201(r.at(t, "pub", 0), "expected expression, found `pub`"),
			recE0201(r.at(t, "fn", 2), "expected expression, found `fn`"),
			r.e0202(r.at(t, "{", 0), "{"))
		r.wantText(t, attr, "(fn a (params) (ret _) (block (expr 1) (expr (error)) (expr (error)) (error)))\n")
		if n := len(r.m.Items); n != 1 {
			t.Errorf("%s: %d items in the module, want 1", attr, n)
		}

		// The ceiling. The E0202 is reported last, when the file ends, and the
		// sink keeps the first 100 errors in the order they are reported. With
		// 99 items after the brace the E0202 is the 100th error and is kept;
		// with 100 it is the 101st and is dropped, and the user is left with
		// a hundred E0201 and nothing that says a `}` is missing.
		for _, row := range []struct {
			items         int
			e0202, capped bool
		}{{99, true, false}, {100, false, true}} {
			what := strconv.Itoa(row.items) + " items after the brace"
			r = recParse(t, head+strings.Repeat("fn g() { 2 }\n", row.items))
			want = nil
			for i := 1; i <= row.items; i++ {
				want = append(want, recE0201(r.at(t, "fn", i), "expected expression, found `fn`"))
			}
			if row.e0202 {
				want = append(want, r.e0202(r.at(t, "{", 0), "{"))
			}
			r.wantDiags(t, what, want...)
			r.wantText(t, what, "(fn a (params) (ret _) (block (expr 1)"+strings.Repeat(" (expr (error))", row.items-1)+" (error)))\n")
			if got := r.sink.Truncated(); got != row.capped {
				t.Errorf("%s: sink truncated is %v, want %v", what, got, row.capped)
			}
			if n := len(r.m.Items); n != 1 {
				t.Errorf("%s: %d items in the module, want 1", what, n)
			}
		}
	})
	t.Run("if_brace_on_the_next_line", func(t *testing.T) {
		// The line rule ends the `if` at the line break after its condition, so
		// the `{` on the next line is not its block: the `if` is missing one
		// (the first mistake), and that `{` starts a statement of its own, a
		// block that is followed by an `else` that nothing expects (the
		// second). Both are reported: the `else` is four tokens after the first
		// mistake, far enough not to be suppressed as a repeat of it.
		const src = "fn f() {\n  if x\n  { 1 } else { 2 }\n}"
		r := recParse(t, src)
		r.wantDiags(t, "brace on the next line",
			recE0201(r.at(t, "\n", 1), "expected `{`, found newline"),
			recE0201(r.at(t, "else", 0), "expected newline, found `else`"))
		r.wantText(t, "brace on the next line", "(fn f (params) (ret _) (block (expr (if x (error))) (expr (block 1)) (error)))\n")
	})
	t.Run("unclosed_paren_is_one_diagnostic", func(t *testing.T) {
		// A `(` that is never closed is the innermost open bracket for the line
		// rule up to the `}` of the block, so the lexer puts no Newline in front
		// of the statements after it. The parser takes the line break of the
		// source as their separator, and the missing `)` is the one E0201 at the
		// token that is neither a `,` nor a `)`: the statements after it, as many
		// as there are, are read as usual. The last two rows are the other side: a
		// statement that follows on the same line still needs its separator.
		rows := []struct {
			name, src string
			tok       string // the token the diagnostics are on, and which occurrence of it
			n         int
			message   string
			open      int    // which `(` of the source is the one that is not closed
			last      string // the last token before the one found: the closer goes after it
			lastN     int
			bare      bool   // the token found does not say that the closer is missing: no help, no fix-it
			want      string // PrintModule
		}{
			{"call, then lets and a call", "fn f() {\n  let a = g(1\n  let b = 2\n  let c = 3\n  h(a, b, c)\n  c\n}",
				"let", 1, "expected `)`, found `let`", 1, "1", 0, false,
				"(fn f (params) (ret _) (block (let a _ (error)) (let b _ 2) (let c _ 3) (expr (call h a b c)) c))\n"},
			{"group, then lets", "fn f() {\n  let a = (1 + 2\n  let b = 3\n  let c = 4\n  b\n}",
				"let", 1, "expected `)`, found `let`", 1, "2", 0, false,
				"(fn f (params) (ret _) (block (let a _ (error)) (let b _ 3) (let c _ 4) b))\n"},
			{"calls are skipped with the statement", "fn f() {\n  let a = g(1\n  h(2)\n  k(3)\n  let z = 1\n  z\n}",
				"h", 0, "expected `)`, found `h`", 1, "1", 0, true,
				"(fn f (params) (ret _) (block (let a _ (error)) (let z _ 1) z))\n"},
			{"after a function", "fn fib(n: Int) -> Int {\n  n\n}\nfn main() {\n  let a = fib(10\n  let b: Int = a + 2 * 3\n  let c = b * 2\n  f(a, b, c)\n  c\n}\n",
				"let", 1, "expected `)`, found `let`", 2, "10", 0, false,
				"(fn fib (params (n Int)) (ret Int) (block n))\n" +
					"(fn main (params) (ret _) (block (let a _ (error)) (let b Int (+ a (* 2 3))) (let c _ (* b 2)) (expr (call f a b c)) c))\n"},
		}
		for _, row := range rows {
			r := recParse(t, row.src)
			want := recE0201Close(r.at(t, row.tok, row.n), row.message,
				r.at(t, "(", row.open), "(", r.at(t, row.last, row.lastN).Hi)
			if row.bare {
				// A name is found: it could be a missing `,` as well, so the
				// diagnostic names the opener and offers no edit.
				want = recE0201Opened(r.at(t, row.tok, row.n), row.message, r.at(t, "(", row.open), "(")
			}
			r.wantDiags(t, row.name, want)
			r.wantText(t, row.name, row.want)
		}

		// A separator is due on the same line, also in the lines after a missing
		// `)`: `let b = 2 let c = 3` is two statements with nothing between them.
		r := recParse(t, "fn f() { let a = 1 let b = 2 }")
		r.wantDiags(t, "two lets on a line", recE0201(r.at(t, "let", 1), "expected newline, found `let`"))
		r.wantText(t, "two lets on a line", "(fn f (params) (ret _) (block (let a _ 1) (let b _ 2)))\n")
		r = recParse(t, "fn f() {\n  let a = g(1\n  let b = 2 let c = 3\n  c\n}")
		r.wantDiags(t, "a missing paren, then two lets on a line",
			recE0201Close(r.at(t, "let", 1), "expected `)`, found `let`", r.at(t, "(", 1), "(", r.at(t, "1", 0).Hi),
			recE0201(r.at(t, "let", 2), "expected newline, found `let`"))
		r.wantText(t, "a missing paren, then two lets on a line",
			"(fn f (params) (ret _) (block (let a _ (error)) (let b _ 2) (let c _ 3) c))\n")
	})
	t.Run("e0202_fixit_closes_at_eof", func(t *testing.T) {
		// The help names the closer of the delimiter that is open, and the
		// one fix-it writes it at the very end of the file, also when that is
		// a line break (the fix-it is then on the empty last line). Not when
		// the file ends inside something that would swallow it:
		//   - a comment that nothing closes, a `//` with no line break after
		//     it or a `/*` with no `*/`: the closer goes right after the last
		//     token that was read, in front of the comment;
		//   - a string or a character that never ends: if the place where the
		//     closer would go (the end of the file, or the end of the last
		//     token read when a comment ends the file) is inside it or at its
		//     end, there is the help and no fix-it.
		rows := []struct {
			src, open, closer string
			n                 int         // which occurrence of open is the unclosed one
			after             string      // the closer goes after the first occurrence of this; "" is the end of the file
			lexer             []diag.Code // the diagnostics that come before the E0202: the lexer's, and an E0201 of the parser where a row has one
			noEdit            bool        // the E0202 has the help and no fix-it
		}{
			{"fn f() {", "{", "}", 0, "", nil, false},
			{"fn f() {\n", "{", "}", 0, "", nil, false},
			{"fn f() { (1", "(", ")", 1, "", nil, false},
			{"#[a", "[", "]", 0, "", nil, false},
			{`#[a("x"`, "(", ")", 0, "", nil, false},
			{"fn f() { g(1\n", "(", ")", 1, "", nil, false},
			// The last line is a comment with nothing after it: the closer
			// goes in front of the comment, after the last token, for each
			// kind of delimiter. A lone CR does not end a comment, so it is
			// still part of it.
			{"fn f() {\n  // todo", "{", "}", 0, "{", nil, false},
			{"fn f() { // x", "{", "}", 0, "{", nil, false},
			{"fn main() {\n  print(1)\n  // todo", "{", "}", 0, "print(1)", nil, false},
			{"fn f() { (1 // x", "(", ")", 1, "1", nil, false},
			{"#[a\n// x", "[", "]", 0, "a", nil, false},
			{"fn f() { // x\r", "{", "}", 0, "{", nil, false},
			// The same for a `/*` that is never closed, which also reports
			// E0103: the last row is the commonest way to get one, a comment
			// that swallows the `}` that was meant to close the function.
			{"fn f() {\n  /* x", "{", "}", 0, "{", []diag.Code{diag.E(103)}, false},
			{"fn f() { (1 /* x", "(", ")", 1, "1", []diag.Code{diag.E(103)}, false},
			{"fn main() {\n  print(1)\n  /* print(2)\n}\n", "{", "}", 0, "print(1)", []diag.Code{diag.E(103)}, false},
			// A comment that is not the last thing in the file, or that a line
			// break ends, does not move the closer from the end of the file,
			// where it is not part of any comment.
			{"fn f() {\n  // todo\n", "{", "}", 0, "", nil, false},
			{"fn f() { // x\r\n", "{", "}", 0, "", nil, false},
			{"fn f() { // x\n  1", "{", "}", 0, "", nil, false},
			{"fn f() { /* x */", "{", "}", 0, "", nil, false},
			// A string that never ends takes the end of the file, also when
			// the code of an interpolation is still open in it: there is the
			// help and no fix-it, and the comment inside the interpolation of
			// the last row does not change that.
			{"fn f() {\n  \"abc", "{", "}", 0, "", []diag.Code{diag.E(110)}, true},
			{"fn f() {\n  1 + \"abc", "{", "}", 0, "", []diag.Code{diag.E(110)}, true},
			{"fn f() {\n  \"a ${ x", "{", "}", 0, "", []diag.Code{diag.E(110)}, true},
			{"fn f() {\n  \"a ${ x /* y", "{", "}", 0, "", []diag.Code{diag.E(103), diag.E(110)}, true},
			// A string that a line break ends leaves the end of the file free,
			// and so does an Error token that is not a string: the `@` is not
			// part of anything that a `}` could join.
			{"fn f() {\n  \"abc\n", "{", "}", 0, "", []diag.Code{diag.E(110)}, false},
			{"fn f() {\n  \"a ${ x\n", "{", "}", 0, "", []diag.Code{diag.E(110)}, false},
			{"fn f() { @", "{", "}", 0, "", []diag.Code{diag.E(104)}, false},
			// A string that a line break ends, in a file that ends in a comment.
			// The closer goes after the last token read, and when that token
			// belongs to the string the closer would be part of it. In the
			// first row it is the string's own Error token; in the second the
			// comment is inside the interpolation, so the line break that ends
			// the string is inside the comment and the Error token reaches into
			// it; in the third the token is the code of the interpolation,
			// which lies in the span of the Error token. There is the help and
			// no fix-it.
			{"fn f() {\n  \"abc\n/* x", "{", "}", 0, "", []diag.Code{diag.E(110), diag.E(103)}, true},
			{"fn f() {\n  \"a ${/* x } b\"\n", "{", "}", 0, "", []diag.Code{diag.E(103), diag.E(110)}, true},
			{"fn f() {\n  \"a ${ x /* y\n z", "{", "}", 0, "", []diag.Code{diag.E(103), diag.E(110)}, true},
			// Two strings that a line break ends: it is the last that counts.
			{"fn f() {\n  \"abc\n  \"def\n/* x", "{", "}", 0, "", []diag.Code{diag.E(110), diag.E(110), diag.E(103)}, true},
			// A character literal that never ends takes the rest of its line
			// as a string does, also when no comment is in the way. The
			// parser cannot read it, so its E0201 for the string in front of it
			// comes before the E0202.
			{"fn f() { \"x\" y 'a", "{", "}", 0, "", []diag.Code{diag.E(108), diag.E(201)}, true},
			// Not these: a token after the string, a string that is complete
			// (here an attribute argument, since the parser cannot read a
			// string in an expression yet), and an Error token that is not a
			// string, each followed by a comment that ends the file. The
			// closer goes after that token.
			{"fn f() {\n  \"abc\n  1\n/* x", "{", "}", 0, "1", []diag.Code{diag.E(110), diag.E(103)}, false},
			{"fn f() {\n  \"a ${ x\n  1 // x", "{", "}", 0, "1", []diag.Code{diag.E(110)}, false},
			{"#[a(\"x\" // c", "(", ")", 0, "\"x\"", nil, false},
			{"fn f() { @ // x", "{", "}", 0, "@", []diag.Code{diag.E(104)}, false},
		}
		for _, row := range rows {
			r := recParse(t, row.src)
			at := len(row.src)
			if row.after != "" {
				at = nthIndex(t, row.src, row.after, 0) + len(row.after)
			}
			// The mistake in the file is the first diagnostic after the ones
			// the row lists; the rest are the other delimiters that are open,
			// reported or suppressed.
			diags := r.sink.All()
			first := len(row.lexer)
			if len(diags) <= first || diags[first].Code != diag.E(202) {
				t.Errorf("%s: diagnostics %v, want an E0202 after %d from the lexer", recShow(row.src), diags, first)
				continue
			}
			for i, code := range row.lexer {
				if diags[i].Code != code {
					t.Errorf("%s: diagnostic %d is %v, want %v", recShow(row.src), i, diags[i].Code, code)
				}
			}
			e := diags[first]
			want := r.e0202At(r.at(t, row.open, row.n), row.open, uint32(at))
			if row.noEdit {
				want.Fixits = nil
			}
			if !reflect.DeepEqual(e, want) {
				t.Errorf("%s: the E0202\n%+v\nwant\n%+v", recShow(row.src), e, want)
			}
			if !row.noEdit {
				if got := e.Fixits; len(got) != 1 || got[0].Span.Lo != uint32(at) || got[0].Span.Hi != uint32(at) || got[0].Replacement != row.closer {
					t.Errorf("%s: fixits %+v, want one that writes %q at %d", recShow(row.src), got, row.closer, at)
				}
				// Applied, the fix-it closes that delimiter: the file has no E0202
				// on it any more (the other delimiters that are open stay open).
				opener := r.at(t, row.open, row.n)
				for _, d := range recParse(t, applyFixits(row.src, e)).sink.All() {
					if d.Code == diag.E(202) && d.Primary.Span.Lo == opener.Lo {
						t.Errorf("%s: after the fix-it, the delimiter at %d is still reported as never closed", recShow(row.src), opener.Lo)
					}
				}
			}
			// The edits of the lexer's diagnostics and this one are at
			// different places when the file ends in a comment, so each of
			// them applies whatever the order. (The E0103 writes its `*/` at
			// the end of the file.)
			if row.lexer != nil && row.lexer[0] == diag.E(103) && !row.noEdit {
				if lexFix := diags[0].Fixits; len(lexFix) != 1 || lexFix[0].Span.Lo == e.Fixits[0].Span.Lo {
					t.Errorf("%s: the fix-its of E0103 %+v and E0202 %+v are at the same place", recShow(row.src), lexFix, e.Fixits)
				}
			}
		}

		// Whatever the file ends in, an E0202 that offers an edit is one whose
		// edit works. Applied alone, it writes the closer as code (the patched
		// file has a token of that kind right where the edit went, and not a
		// closer that a string, a character or a comment took in) and the
		// delimiter is no longer reported. The rows above say where the edit
		// goes for the endings that were tried; this runs every combination of
		// a few beginnings, strings and endings, so that a way to end a file
		// that no row names cannot put the edit where it closes nothing. It
		// counts the E0202 with an edit and those without, because a loop that
		// found none of one kind would check nothing about it.
		closerKind := map[string]syntax.TokenKind{"}": syntax.RBrace, ")": syntax.RParen, "]": syntax.RBracket}
		heads := []string{"fn f() {\n  ", "fn f() { (1 + ", `fn f() { "x" y `}
		strs := []string{`"abc`, `"a\`, `"a ${ x`, `"a ${ x } b`, `"a ${ x /* y`, `"a ${/* x } b"`, `"a ${ "b`, `"a ${ ("`, `'a`, `'ab`}
		ends := []string{"", "\n", " // z", "\n// z", " /* z", "\n/* z", "\n  1", "\n  1 // z", "\n  1\n/* z", "\r\n/* z"}
		withEdit, helpOnly := 0, 0
		for _, head := range heads {
			for _, str := range strs {
				for _, end := range ends {
					src := head + str + end
					for _, d := range recParse(t, src).sink.All() {
						if d.Code != diag.E(202) {
							continue
						}
						if len(d.Fixits) == 0 {
							helpOnly++
							continue
						}
						withEdit++
						edit := d.Fixits[0]
						patched := applyFixits(src, d)
						tb, _, _, _ := lexFile(t, patched)
						if !slices.ContainsFunc(tb.Tokens, func(tok syntax.Token) bool {
							return tok.Kind == closerKind[edit.Replacement] && tok.Span.Lo == edit.Span.Lo
						}) {
							t.Errorf("%s: the closer %q that the E0202 writes at %d is not code in %s", recShow(src), edit.Replacement, edit.Span.Lo, recShow(patched))
						}
						for _, after := range recParse(t, patched).sink.All() {
							if after.Code == diag.E(202) && after.Primary.Span.Lo == d.Primary.Span.Lo {
								t.Errorf("%s: the E0202 at %d is still reported after its fix-it %+v", recShow(src), d.Primary.Span.Lo, d.Fixits)
							}
						}
					}
				}
			}
		}
		if withEdit == 0 || helpOnly == 0 {
			t.Errorf("%d E0202 with an edit and %d with the help only, want some of each", withEdit, helpOnly)
		}

		// In a file that ends with a line break the fix-it is on the empty
		// last line, line 2 here, and the diagnostic shows it as such: the `-`
		// line has nothing after it (not even a space) and the `+` line has
		// the closer.
		const src = "fn f() {\n"
		var sm base.SourceManager
		id := sm.Add("test.ki", []byte(src))
		in, sink := base.NewInterner(), &diag.Sink{}
		tb := syntax.Lex(&sm, id, in, sink)
		syntax.Parse(&tb, id, in, sink)
		diags := sink.All()
		if len(diags) != 1 {
			t.Fatalf("%s: %d diagnostics, want exactly 1: %v", recShow(src), len(diags), diags)
		}
		want := strings.Join([]string{
			"error[E0202]: unclosed delimiter",
			" --> test.ki:1:8",
			"  |",
			"1 | fn f() {",
			"  |        ^ this `{` is never closed",
			"  = help: close it with `}`",
			"  |",
			"2 -",
			"2 + }",
			"",
		}, "\n")
		if got := diag.RenderHuman(diags[0], &sm); got != want {
			t.Errorf("%s: RenderHuman =\n%s\nwant\n%s", recShow(src), got, want)
		}
	})
	t.Run("e0204_label_and_help", func(t *testing.T) {
		src := "fn f() { " + strings.Repeat("(", 257) + "1" + strings.Repeat(")", 257) + " }"
		r := recParse(t, src)
		diags := r.sink.All()
		if len(diags) != 1 {
			t.Fatalf("%d diagnostics, want exactly 1: %v", len(diags), diags)
		}
		d := diags[0]
		if want := "nesting level 257 starts here"; d.Primary.Message != want {
			t.Errorf("primary label %q, want %q", d.Primary.Message, want)
		}
		if want := "split the expression with `let` bindings"; d.Help != want || len(d.Fixits) != 0 {
			t.Errorf("help %q and fixits %+v, want %q and none", d.Help, d.Fixits, want)
		}
		if want := []string{"the limit is 256"}; !slices.Equal(d.Notes, want) {
			t.Errorf("notes %q, want %q", d.Notes, want)
		}
	})
}

// nthIndex returns the byte offset of the n-th occurrence (from 0) of text in s.
func nthIndex(t *testing.T, s, text string, n int) int {
	t.Helper()
	at := 0
	for i := 0; ; i++ {
		j := strings.Index(s[at:], text)
		if j < 0 {
			t.Fatalf("%s has no occurrence number %d of %q", recShow(s), n, text)
		}
		if i == n {
			return at + j
		}
		at += j + len(text)
	}
}
