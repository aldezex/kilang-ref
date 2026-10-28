package syntax

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// This file is in package syntax and not in syntax_test because its tests call
// the unexported parser.expr, as TestParserCursor does, which is what the
// _internal_test.go in its name says. So it cannot use lexFile or the other
// helpers of the external tests, and it builds its own.

// exprRun is a parser that is about to read the tokens of one source, together
// with what a test needs to look at what the parser did.
type exprRun struct {
	p    *parser
	in   *base.Interner
	sink *diag.Sink
	file base.FileID
	src  string
}

// show returns src quoted for a failure message; a long source is cut, because
// some of the sources here are hundreds of kilobytes.
func show(src string) string {
	if len(src) <= 40 {
		return strconv.Quote(src)
	}
	return strconv.Quote(src[:20]) + "... (" + strconv.Itoa(len(src)) + " bytes)"
}

// newSource returns a SourceManager and the id of src, its second file, after
// an empty one. The id is 1 and not 0: a parser that builds a span and forgets
// the file leaves the zero value, which would be the right id if the file were
// the first. The id is checked, so that deleting the padding fails every test
// that goes through newSource instead of letting those that compare a File with
// it pass for the wrong reason.
func newSource(t *testing.T, src string) (*base.SourceManager, base.FileID) {
	t.Helper()
	sm := &base.SourceManager{}
	sm.Add("pad.ki", nil)
	id := sm.Add("test.ki", []byte(src))
	if id != 1 {
		t.Fatalf("test.ki is file %d, want 1: pad.ki must come first, or a span that lost its File would pass for the right one", id)
	}
	return sm, id
}

// newExprRun lexes src and returns a parser at its first token. src must be
// free of lexical errors, so that every diagnostic a test sees comes from the
// parser.
func newExprRun(t *testing.T, src string) *exprRun {
	t.Helper()
	sm, file := newSource(t, src)
	in := base.NewInterner()
	sink := &diag.Sink{}
	tb := Lex(sm, file, in, sink)
	if n := len(sink.All()); n != 0 {
		t.Fatalf("%s: lexing reported %d diagnostics: %v", show(src), n, sink.All())
	}
	return &exprRun{p: newParser(&tb, file, in, sink), in: in, sink: sink, file: file, src: src}
}

// sexpr prints the expression e of the parser's module.
func (r *exprRun) sexpr(e ExprID) string {
	pr := printer{m: r.p.m, in: r.in}
	pr.expr(e)
	return pr.b.String()
}

// node returns the expression e of the parser's module.
func (r *exprRun) node(e ExprID) Expr { return r.p.m.Exprs.Get(e) }

// span returns the span [lo, hi) of this run's file.
func (r *exprRun) span(lo, hi uint32) base.Span { return base.Span{File: r.file, Lo: lo, Hi: hi} }

// wantAt fails the test unless the parser's cursor is at a token of the given
// kind that starts at byte lo.
func (r *exprRun) wantAt(t *testing.T, what string, kind TokenKind, lo uint32) {
	t.Helper()
	if tok := r.p.tok(); tok.Kind != kind || tok.Span.Lo != lo {
		t.Errorf("%s: %s: the cursor is at %v (byte %d), want %v (byte %d)", what, show(r.src), tok.Kind, tok.Span.Lo, kind, lo)
	}
}

// parseExprSexpr lexes src, builds a parser, reads one expression with
// p.expr(0) and returns what the printer writes for it. The source must be one
// expression that the parser reads to its last token, with no diagnostics: a
// parser that stops early or that reports something is a failure of the test
// that calls this.
func parseExprSexpr(t *testing.T, src string) string {
	t.Helper()
	r := newExprRun(t, src)
	e := r.p.expr(0)
	if diags := r.sink.All(); len(diags) != 0 {
		t.Errorf("%s: %d diagnostics, want none: %v", show(src), len(diags), diags)
	}
	if !r.p.at(Eof) {
		t.Errorf("%s: the expression stops at token %d (%v), want it to use the whole source", show(src), r.p.pos, r.p.tok().Kind)
	}
	return r.sexpr(e)
}

// wantText fails the test unless got is exactly want. For a text of more than
// 200 bytes it says where the two first differ instead of printing both.
func wantText(t *testing.T, what, got, want string) {
	t.Helper()
	if got == want {
		return
	}
	if len(got) <= 200 && len(want) <= 200 {
		t.Errorf("%s: got\n%s\nwant\n%s", what, got, want)
		return
	}
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	t.Errorf("%s: got %d bytes, want %d; they first differ at byte %d", what, len(got), len(want), i)
}

// wantExpr fails the test unless src is one expression that prints as want.
func wantExpr(t *testing.T, what, src, want string) {
	t.Helper()
	wantText(t, what+": "+show(src), parseExprSexpr(t, src), want)
}

// wantOnlyDiag fails the test unless the sink holds exactly one diagnostic and
// it is want, field by field.
func wantOnlyDiag(t *testing.T, what string, sink *diag.Sink, want diag.Diagnostic) {
	t.Helper()
	got := sink.All()
	if len(got) != 1 {
		t.Errorf("%s: %d diagnostics, want exactly 1: %+v", what, len(got), got)
		return
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("%s: diagnostic\n%+v\nwant\n%+v", what, got[0], want)
	}
}

// The two diagnostics of this file, written out here in full and not built
// from the parser's constants, so that a change of text or of limit is noticed.

// genericChainHelp is the help of an E0203 whose operands are not all one token
// each.
const genericChainHelp = "use `&&`: `a < b && b < c`"

// chainedDiag is the E0203 of a chain of comparisons whose first operator is at
// [flo, fhi) and the second at [lo, hi). help is its help line; insert is what
// the one fix-it writes just before the second operator ("" if the chain gets
// no fix-it).
func chainedDiag(file base.FileID, flo, fhi, lo, hi uint32, help, insert string) diag.Diagnostic {
	d := diag.Diagnostic{
		Severity:  diag.Error,
		Code:      diag.E(203),
		Message:   "comparison operators cannot be chained",
		Primary:   diag.Label{Span: base.Span{File: file, Lo: lo, Hi: hi}, Message: "second comparison here"},
		Secondary: []diag.Label{{Span: base.Span{File: file, Lo: flo, Hi: fhi}, Message: "first comparison here"}},
		Help:      help,
	}
	if insert != "" {
		d.Fixits = []diag.FixIt{{Span: base.Span{File: file, Lo: lo, Hi: lo}, Replacement: insert}}
	}
	return d
}

// tooLongDiag is the E0205 that the operator or call parenthesis at [lo, hi)
// gets when it is the 1025th of its expression.
func tooLongDiag(file base.FileID, lo, hi uint32) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Code:     diag.E(205),
		Message:  "expression chain is too long",
		Primary:  diag.Label{Span: base.Span{File: file, Lo: lo, Hi: hi}, Message: "operator or call number 1025 of this chain"},
		Notes:    []string{"the limit is 1024 operators and calls in one expression"},
		Help:     "split the expression with `let` bindings",
	}
}

// applyFixes returns src with the fix-its of d applied, each at its offsets in
// the original text, from the right: of two edits at one offset the later one in
// d.Fixits is written first, as FixIt in package diag says. (The tests of
// package syntax_test have their own applyFixits: the two packages cannot share
// a helper.)
func applyFixes(src string, d diag.Diagnostic) string {
	fixits := slices.Clone(d.Fixits)
	slices.SortStableFunc(fixits, func(a, b diag.FixIt) int { return int(b.Span.Lo) - int(a.Span.Lo) })
	for _, f := range fixits {
		src = src[:f.Span.Lo] + f.Replacement + src[f.Span.Hi:]
	}
	return src
}

// plusChain returns "1" followed by n times " + 1": n binary operators in a row.
func plusChain(n int) string { return "1" + strings.Repeat(" + 1", n) }

// wrapPlus returns what the printer writes for s followed by n times " + 1":
// (+ (+ s 1) 1) with n pairs of parentheses.
func wrapPlus(s string, n int) string {
	for range n {
		s = "(+ " + s + " 1)"
	}
	return s
}

// lastIndex returns the byte offset of the last occurrence of text in src.
func lastIndex(t *testing.T, src, text string) uint32 {
	t.Helper()
	i := strings.LastIndex(src, text)
	if i < 0 {
		t.Fatalf("%q is not in %s", text, show(src))
	}
	return uint32(i)
}

func TestParseExpr(t *testing.T) {
	t.Run("precedence_mul_over_add", func(t *testing.T) {
		wantExpr(t, "example", "1 + 2 * 3", "(+ 1 (* 2 3))")
	})
	t.Run("left_assoc_sub", func(t *testing.T) {
		wantExpr(t, "example", "1 - 2 - 3", "(- (- 1 2) 3)")
	})
	t.Run("unary_binds_tighter", func(t *testing.T) {
		wantExpr(t, "example", "-a * b", "(* (neg a) b)")
	})
	t.Run("not_and_or", func(t *testing.T) {
		wantExpr(t, "example", "!a && b || c", "(|| (&& (not a) b) c)")
	})
	t.Run("chained_comparison_E0203", func(t *testing.T) {
		const src = "a < b < c"
		r := newExprRun(t, src)
		e := r.p.expr(0)
		// The diagnostic sits on the second operator, `<` at byte 6, and has
		// the help that suggests `&&`. The whole chain is one error node that covers
		// all three operands, and the parser has read all of them.
		wantOnlyDiag(t, src, r.sink, chainedDiag(r.file, 2, 3, 6, 7, genericChainHelp, "&& b "))
		wantText(t, src, r.sexpr(e), "(error)")
		if got, want := r.node(e).Span, r.span(0, 9); got != want {
			t.Errorf("%s: the error node spans %v, want %v", src, got, want)
		}
		r.wantAt(t, src, Eof, 9)
	})
	t.Run("call_args", func(t *testing.T) {
		wantExpr(t, "example", "f(1, g(2))", "(call f 1 (call g 2))")
	})
	t.Run("trailing_comma_in_call", func(t *testing.T) {
		wantExpr(t, "example", "f(1, 2,)", "(call f 1 2)")
	})
	t.Run("parens_group", func(t *testing.T) {
		wantExpr(t, "example", "(1 + 2) * 3", "(* (+ 1 2) 3)")
	})
	t.Run("unit_literal", func(t *testing.T) {
		wantExpr(t, "example", "()", "()")
	})
	t.Run("upper_name", func(t *testing.T) {
		wantExpr(t, "example", "Foo", "Foo")
	})

	t.Run("chain_at_limit_ok", func(t *testing.T) {
		// 1024 operators in a row are allowed, and every one of them is in the
		// tree: the printed text has 1024 levels.
		wantExpr(t, "1024 operators", plusChain(1024), wrapPlus("1", 1024))

		// The 1025th is the one that is reported, at its own `+`. The whole
		// expression becomes one error node, and the parser is at the end.
		src := plusChain(1025)
		r := newExprRun(t, src)
		e := r.p.expr(0)
		lo := lastIndex(t, src, "+")
		wantOnlyDiag(t, "1025 operators", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "1025 operators", r.sexpr(e), "(error)")
		r.wantAt(t, "1025 operators", Eof, uint32(len(src)))
	})
	t.Run("chain_counts_inside_args", func(t *testing.T) {
		// The call is the first of the chain, so 1023 operators in its
		// arguments make 1024; 1024 make 1025. A counter per Pratt loop would
		// see 1 in the outer loop and 1024 in the inner one, and report
		// nothing.
		wantExpr(t, "1 call and 1023 operators", "f("+plusChain(1023)+")", "(call f "+wrapPlus("1", 1023)+")")

		src := "f(" + plusChain(1024) + ")"
		r := newExprRun(t, src)
		e := r.p.expr(0)
		lo := lastIndex(t, src, "+")
		wantOnlyDiag(t, "1 call and 1024 operators", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "1 call and 1024 operators", r.sexpr(e), "(error)")
		r.wantAt(t, "1 call and 1024 operators", Eof, uint32(len(src)))
	})
	t.Run("long_chain_reports_E0205", func(t *testing.T) {
		// A hundred thousand operators are far past the limit: one diagnostic,
		// at the 1025th operator, and a tree that is one node.
		src := plusChain(100000)
		r := newExprRun(t, src)
		e := r.p.expr(0)
		lo := lastIndex(t, plusChain(1025), "+")
		wantOnlyDiag(t, "100000 operators", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "100000 operators", r.sexpr(e), "(error)")
		if got, want := r.node(e).Span, r.span(0, uint32(len(src))); got != want {
			t.Errorf("the error node spans %v, want the whole source %v", got, want)
		}
		r.wantAt(t, "100000 operators", Eof, uint32(len(src)))
	})

	t.Run("binary_operators", func(t *testing.T) {
		// Every binary operator gives its own node: a table that confused two
		// of them would print the wrong symbol.
		for _, op := range []string{"+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=", "&&", "||"} {
			wantExpr(t, op, "a "+op+" b", "("+op+" a b)")
		}
	})
	t.Run("precedence_levels", func(t *testing.T) {
		rows := []struct{ src, want string }{
			// || binds looser than &&, and both are left associative.
			{"a || b && c", "(|| a (&& b c))"},
			{"a && b || c && d", "(|| (&& a b) (&& c d))"},
			{"a || b || c", "(|| (|| a b) c)"},
			{"a && b && c", "(&& (&& a b) c)"},
			// Comparisons bind tighter than && and ||, looser than + and -.
			{"a == b && c != d", "(&& (== a b) (!= c d))"},
			{"a || b < c", "(|| a (< b c))"},
			{"1 + 2 < 3 * 4", "(< (+ 1 2) (* 3 4))"},
			// + and - are one level, left associative, in both orders.
			{"a - b + c", "(+ (- a b) c)"},
			{"a + b - c", "(- (+ a b) c)"},
			{"a + b * c - d", "(- (+ a (* b c)) d)"},
			{"1 + 2 + 3 * 4 * 5 + 6", "(+ (+ (+ 1 2) (* (* 3 4) 5)) 6)"},
			// * / % are one level, left associative, in every order.
			{"8 / 4 / 2", "(/ (/ 8 4) 2)"},
			{"a * b / c", "(/ (* a b) c)"},
			{"a / b * c", "(* (/ a b) c)"},
			{"a % b * c", "(* (% a b) c)"},
			{"a * b % c", "(% (* a b) c)"},
			{"a / b % c", "(% (/ a b) c)"},
			{"a % b / c", "(/ (% a b) c)"},
			// A parenthesis overrides all of it.
			{"(a || b) && c", "(&& (|| a b) c)"},
			{"(a + b) * (c - d)", "(* (+ a b) (- c d))"},
			{"a - (b - c)", "(- a (- b c))"},
		}
		for _, row := range rows {
			wantExpr(t, row.src, row.src, row.want)
		}
	})
	t.Run("unary_operators", func(t *testing.T) {
		rows := []struct{ src, want string }{
			{"-a", "(neg a)"},
			{"!a", "(not a)"},
			{"- -a", "(neg (neg a))"},
			{"!!a", "(not (not a))"},
			{"-!a", "(neg (not a))"},
			{"!-a", "(not (neg a))"},
			// A prefix operator binds tighter than every binary one...
			{"-a + b", "(+ (neg a) b)"},
			{"a - -b", "(- a (neg b))"},
			{"a * -b", "(* a (neg b))"},
			{"-a * -b", "(* (neg a) (neg b))"},
			{"!a == b", "(== (not a) b)"},
			{"!a || !b", "(|| (not a) (not b))"},
			// ...and a call binds tighter than a prefix operator.
			{"-f(x)", "(neg (call f x))"},
			{"!f(x)(y)", "(not (call (call f x) y))"},
			{"-(a + b)", "(neg (+ a b))"},
		}
		for _, row := range rows {
			wantExpr(t, row.src, row.src, row.want)
		}
	})
	t.Run("call_forms", func(t *testing.T) {
		rows := []struct{ src, want string }{
			{"f()", "(call f)"},
			{"f(x)", "(call f x)"},
			{"f (x)", "(call f x)"},
			{"f(1,)", "(call f 1)"},
			{"f(a + b, c * d)", "(call f (+ a b) (* c d))"},
			{"f(a, b, c)", "(call f a b c)"},
			// The call applies to what is on its left, whatever that is.
			{"f(1)(2)", "(call (call f 1) 2)"},
			{"f()()()", "(call (call (call f)))"},
			{"(f)(1)", "(call f 1)"},
			{"1(2)", "(call 1 2)"},
			{"f(g(h(1)))", "(call f (call g (call h 1)))"},
			// A call binds tighter than any binary operator.
			{"f(1) + g(2)", "(+ (call f 1) (call g 2))"},
			{"a + f(b) * c", "(+ a (* (call f b) c))"},
			{"a < f(b)", "(< a (call f b))"},
			// The arguments of a call are one list, and the arguments of the
			// calls inside them are other lists, which must not mix with it.
			{"f(g(1, 2), h(3), 4)", "(call f (call g 1 2) (call h 3) 4)"},
			{"f(1, g(2, 3, 4), h())", "(call f 1 (call g 2 3 4) (call h))"},
			{"f(g(1))(h(2), 3)", "(call (call f (call g 1)) (call h 2) 3)"},
		}
		for _, row := range rows {
			wantExpr(t, row.src, row.src, row.want)
		}
	})
	t.Run("primary_forms", func(t *testing.T) {
		rows := []struct{ src, want string }{
			{"0", "0"},
			{"42", "42"},
			{"9223372036854775807", "9223372036854775807"},
			// The value of the literal, not its place in the token buffer.
			{"0xFF + 1_000", "(+ 255 1000)"},
			{"7 + 8 + 9", "(+ (+ 7 8) 9)"},
			{"true", "true"},
			{"false", "false"},
			{"true && false", "(&& true false)"},
			{"x", "x"},
			{"fooBar", "fooBar"},
			{"Foo", "Foo"},
			{"foo + Bar", "(+ foo Bar)"},
			{"()", "()"},
			{"( )", "()"},
			{"(())", "()"},
			{"f(())", "(call f ())"},
			{"(1)", "1"},
			{"((1))", "1"},
			{"( x )", "x"},
		}
		for _, row := range rows {
			wantExpr(t, row.src, row.src, row.want)
		}
	})

	t.Run("chained_comparison_forms", func(t *testing.T) {
		// first and second are the spans of the first and of the second
		// comparison operator; help and insert are the help line and the text
		// of the fix-it (see chainedDiag).
		rows := []struct {
			src           string
			first, second [2]uint32
			help, insert  string
			want          string
		}{
			// Every pair of comparison operators is a chain: they are one level.
			{"a == b != c", [2]uint32{2, 4}, [2]uint32{7, 9}, "use `&&`: `a == b && b != c`", "&& b ", "(error)"},
			{"a < b == c", [2]uint32{2, 3}, [2]uint32{6, 8}, "use `&&`: `a < b && b == c`", "&& b ", "(error)"},
			{"a <= b > c", [2]uint32{2, 4}, [2]uint32{7, 8}, "use `&&`: `a <= b && b > c`", "&& b ", "(error)"},
			{"a >= b <= c", [2]uint32{2, 4}, [2]uint32{7, 9}, "use `&&`: `a >= b && b <= c`", "&& b ", "(error)"},
			{"a != b < c", [2]uint32{2, 4}, [2]uint32{7, 8}, "use `&&`: `a != b && b < c`", "&& b ", "(error)"},
			// The operand after the second operator is read in full. It is not
			// one token here, so the help is the generic one, but the middle
			// operand is one token and the fix-it stays.
			{"a < b < c + 1", [2]uint32{2, 3}, [2]uint32{6, 7}, genericChainHelp, "&& b ", "(error)"},
			{"x < y <= z + 1", [2]uint32{2, 3}, [2]uint32{6, 8}, genericChainHelp, "&& y ", "(error)"},
			{"x < y <= z && w", [2]uint32{2, 3}, [2]uint32{6, 8}, "use `&&`: `x < y && y <= z`", "&& y ", "(&& (error) w)"},
			{"x < y <= z || w", [2]uint32{2, 3}, [2]uint32{6, 8}, "use `&&`: `x < y && y <= z`", "&& y ", "(|| (error) w)"},
			{"x < y <= f(z)", [2]uint32{2, 3}, [2]uint32{6, 8}, genericChainHelp, "&& y ", "(error)"},
			{"x < y <= -z", [2]uint32{2, 3}, [2]uint32{6, 8}, genericChainHelp, "&& y ", "(error)"},
			// The loop that reads the operators goes on after the chain.
			{"a < b < c && d", [2]uint32{2, 3}, [2]uint32{6, 7}, "use `&&`: `a < b && b < c`", "&& b ", "(&& (error) d)"},
			// Three in a row are one mistake, reported at the second operator,
			// and the rest of the chain is read. The help and the fix-it are
			// about the first three operands.
			{"a < b < c < d", [2]uint32{2, 3}, [2]uint32{6, 7}, "use `&&`: `a < b && b < c`", "&& b ", "(error)"},
			{"a < b < c == d != e", [2]uint32{2, 3}, [2]uint32{6, 7}, "use `&&`: `a < b && b < c`", "&& b ", "(error)"},
			// The three rows above use the names a, b and c, whose help is the
			// text of the generic one: only other names tell a help made of the
			// real operands from the generic help when a third comparison
			// follows the third operand.
			{"x < y <= z < w", [2]uint32{2, 3}, [2]uint32{6, 8}, "use `&&`: `x < y && y <= z`", "&& y ", "(error)"},
			// A name that starts with a capital is an operand like any other:
			// the parser reads it as a value (nothing defines one yet).
			{"a < Foo < c", [2]uint32{2, 3}, [2]uint32{8, 9}, "use `&&`: `a < Foo && Foo < c`", "&& Foo ", "(error)"},
			// The operators around the chain go on being read, and the first
			// operand is the one that starts the chain, not the whole `x && a`.
			{"x && a < b < c && y", [2]uint32{7, 8}, [2]uint32{11, 12}, "use `&&`: `a < b && b < c`", "&& b ", "(&& (&& x (error)) y)"},
			// Operands that are not one token: no help of its own and, when the
			// middle one is not one token, no fix-it.
			{"a + b < c * d < e", [2]uint32{6, 7}, [2]uint32{14, 15}, genericChainHelp, "", "(error)"},
			{"f(a < b < c, d)", [2]uint32{4, 5}, [2]uint32{8, 9}, "use `&&`: `a < b && b < c`", "&& b ", "(call f (error) d)"},
			{"-1 < x < 2", [2]uint32{3, 4}, [2]uint32{7, 8}, genericChainHelp, "&& x ", "(error)"},
		}
		for _, row := range rows {
			r := newExprRun(t, row.src)
			e := r.p.expr(0)
			wantOnlyDiag(t, row.src, r.sink,
				chainedDiag(r.file, row.first[0], row.first[1], row.second[0], row.second[1], row.help, row.insert))
			wantText(t, row.src, r.sexpr(e), row.want)
			r.wantAt(t, row.src, Eof, uint32(len(row.src)))
		}
	})
	t.Run("e0203_labels_help_and_fixit", func(t *testing.T) {
		// The primary label is on the second operator and the secondary on the
		// first; the help quotes the real operands and operators when each
		// operand is one token (so a different chain gets a different help); the
		// one fix-it writes `&& ` and the middle operand just before the second
		// operator. With the fix-it applied, the chain is gone.
		rows := []struct {
			src           string
			first, second [2]uint32 // the two operators
			help, insert  string
		}{
			{"a < b < c", [2]uint32{2, 3}, [2]uint32{6, 7}, "use `&&`: `a < b && b < c`", "&& b "},
			{"x < y <= z", [2]uint32{2, 3}, [2]uint32{6, 8}, "use `&&`: `x < y && y <= z`", "&& y "},
			{"alpha >= beta == gamma", [2]uint32{6, 8}, [2]uint32{14, 16}, "use `&&`: `alpha >= beta && beta == gamma`", "&& beta "},
			{"1 != n < 20", [2]uint32{2, 4}, [2]uint32{7, 8}, "use `&&`: `1 != n && n < 20`", "&& n "},
			{"true == flag != false", [2]uint32{5, 7}, [2]uint32{13, 15}, "use `&&`: `true == flag && flag != false`", "&& flag "},
		}
		for _, row := range rows {
			r := newExprRun(t, row.src)
			r.p.expr(0)
			diags := r.sink.All()
			if len(diags) != 1 {
				t.Errorf("%s: %d diagnostics, want exactly 1: %+v", row.src, len(diags), diags)
				continue
			}
			d := diags[0]
			want := chainedDiag(r.file, row.first[0], row.first[1], row.second[0], row.second[1], row.help, row.insert)
			if !reflect.DeepEqual(d, want) {
				t.Errorf("%s: diagnostic\n%+v\nwant\n%+v", row.src, d, want)
				continue
			}
			fixed := applyFixes(row.src, d)
			r2 := newExprRun(t, fixed)
			r2.p.expr(0)
			if n := len(r2.sink.All()); n != 0 {
				t.Errorf("%s: after the fix-it, %q reports %d diagnostics: %+v", row.src, fixed, n, r2.sink.All())
			}
		}
	})
	t.Run("e0203_compound_middle_operand", func(t *testing.T) {
		// When the middle operand is more than one token there is nothing
		// mechanical to write before the second operator: the generic help and
		// no fix-it. The first and the last operand being one token each does
		// not change that.
		for _, src := range []string{"a < f(x) < c", "a < -b < c", "a < (b) < c", "a < b + 1 < c", "a < { b } < c", "a < if b { c } else { d } < e"} {
			r := newExprRun(t, src)
			r.p.expr(0)
			diags := r.sink.All()
			if len(diags) < 1 || diags[0].Code != diag.E(203) {
				t.Errorf("%s: diagnostics %+v, want an E0203 first", src, diags)
				continue
			}
			if d := diags[0]; d.Help != genericChainHelp || len(d.Fixits) != 0 {
				t.Errorf("%s: help %q and fixits %+v, want the generic help and no fix-it", src, d.Help, d.Fixits)
			}
		}
	})
	t.Run("e0205_label_and_help", func(t *testing.T) {
		// The label counts the operator or call that went over the limit, and
		// the help is the same whether it was an operator or a call.
		for _, row := range []struct{ name, src string }{
			{"operators", plusChain(1025)},
			{"calls", "f" + strings.Repeat("()", 1025)},
		} {
			r := newExprRun(t, row.src)
			r.p.expr(0)
			diags := r.sink.All()
			if len(diags) != 1 {
				t.Errorf("%s: %d diagnostics, want exactly 1: %+v", row.name, len(diags), diags)
				continue
			}
			d := diags[0]
			if want := "operator or call number 1025 of this chain"; d.Primary.Message != want {
				t.Errorf("%s: primary label %q, want %q", row.name, d.Primary.Message, want)
			}
			if want := "split the expression with `let` bindings"; d.Help != want || len(d.Fixits) != 0 {
				t.Errorf("%s: help %q and fixits %+v, want %q and none", row.name, d.Help, d.Fixits, want)
			}
		}
	})
	t.Run("comparison_not_chained_ok", func(t *testing.T) {
		// None of these chains: a parenthesis, an operator of another level,
		// an argument or an operand starts a new expression.
		rows := []struct{ src, want string }{
			{"(a < b) < c", "(< (< a b) c)"},
			{"a < (b < c)", "(< a (< b c))"},
			{"(a < b) == (c < d)", "(== (< a b) (< c d))"},
			{"a < b && b < c", "(&& (< a b) (< b c))"},
			{"a < b || c < d || e < f", "(|| (|| (< a b) (< c d)) (< e f))"},
			{"a + b < c + d", "(< (+ a b) (+ c d))"},
			{"a == b || c != d", "(|| (== a b) (!= c d))"},
			{"f(a < b, b < c)", "(call f (< a b) (< b c))"},
			{"!a < b", "(< (not a) b)"},
		}
		for _, row := range rows {
			wantExpr(t, row.src, row.src, row.want)
		}
	})

	t.Run("spans", func(t *testing.T) {
		check := func(what string, got, want base.Span) {
			t.Helper()
			if got != want {
				t.Errorf("%s: span %v, want %v", what, got, want)
			}
		}
		// A literal or a name is the span of its token, with no blank before it.
		r := newExprRun(t, "  7")
		check("int", r.node(r.p.expr(0)).Span, r.span(2, 3))
		r = newExprRun(t, "foo")
		check("name", r.node(r.p.expr(0)).Span, r.span(0, 3))
		r = newExprRun(t, "true")
		check("bool", r.node(r.p.expr(0)).Span, r.span(0, 4))
		r = newExprRun(t, " ( )")
		check("unit", r.node(r.p.expr(0)).Span, r.span(1, 4))

		// A binary expression runs from the start of its left operand to the end
		// of its right one, and its OpSpan is the operator alone.
		r = newExprRun(t, "a + b * c")
		e := r.node(r.p.expr(0))
		check("a + b * c", e.Span, r.span(0, 9))
		check("a + b * c: operator", e.OpSpan, r.span(2, 3))
		check("a + b * c: left", r.node(e.Lhs).Span, r.span(0, 1))
		check("a + b * c: right", r.node(e.Rhs).Span, r.span(4, 9))
		check("a + b * c: right operator", r.node(e.Rhs).OpSpan, r.span(6, 7))

		// In a left associative chain every node starts at the first token.
		r = newExprRun(t, "x - y - z")
		e = r.node(r.p.expr(0))
		check("x - y - z", e.Span, r.span(0, 9))
		check("x - y - z: operator", e.OpSpan, r.span(6, 7))
		check("x - y - z: left", r.node(e.Lhs).Span, r.span(0, 5))
		check("x - y - z: left operator", r.node(e.Lhs).OpSpan, r.span(2, 3))

		// A prefix operator starts the span, and the span ends with its operand.
		r = newExprRun(t, " ! f(x)")
		e = r.node(r.p.expr(0))
		check("! f(x)", e.Span, r.span(1, 7))
		check("! f(x): operator", e.OpSpan, r.span(1, 2))
		check("! f(x): operand", r.node(e.Operand).Span, r.span(3, 7))

		// A call runs from the start of its callee to its `)`.
		r = newExprRun(t, "f(1, 2)")
		e = r.node(r.p.expr(0))
		check("f(1, 2)", e.Span, r.span(0, 7))
		check("f(1, 2): callee", r.node(e.Callee).Span, r.span(0, 1))
		args := r.p.m.ExprList(e.Args)
		if len(args) != 2 {
			t.Fatalf("f(1, 2): %d arguments, want 2", len(args))
		}
		check("f(1, 2): first argument", r.node(args[0]).Span, r.span(2, 3))
		check("f(1, 2): second argument", r.node(args[1]).Span, r.span(5, 6))
		r = newExprRun(t, "f(1)(2)")
		e = r.node(r.p.expr(0))
		check("f(1)(2)", e.Span, r.span(0, 7))
		check("f(1)(2): callee", r.node(e.Callee).Span, r.span(0, 4))

		// A parenthesis is not a node: the group is its inner expression, whose
		// span leaves the parentheses out, but an expression that starts with
		// one starts at it.
		r = newExprRun(t, "(1 + 2) * 3")
		e = r.node(r.p.expr(0))
		check("(1 + 2) * 3", e.Span, r.span(0, 11))
		check("(1 + 2) * 3: operator", e.OpSpan, r.span(8, 9))
		check("(1 + 2) * 3: group", r.node(e.Lhs).Span, r.span(1, 6))
		r = newExprRun(t, "(f)(1)")
		e = r.node(r.p.expr(0))
		check("(f)(1)", e.Span, r.span(0, 6))
		check("(f)(1): callee", r.node(e.Callee).Span, r.span(1, 2))

		// An error node is an ExprError that covers what the parser consumed
		// for it and no more. A missing operand covers nothing: its span is
		// empty and sits at the start of the token found instead, which is
		// right after the operator in `1 +` and past a blank in `1 + )`, just
		// outside the binary that holds it (see Expr).
		isError := func(what string, e Expr) {
			t.Helper()
			if e.Kind != ExprError {
				t.Errorf("%s: kind %d, want ExprError (%d)", what, e.Kind, ExprError)
			}
		}
		r = newExprRun(t, "1 +")
		e = r.node(r.p.expr(0))
		check("1 +", e.Span, r.span(0, 3))
		missing := r.node(e.Rhs)
		isError("1 +: missing operand", missing)
		check("1 +: missing operand", missing.Span, r.span(3, 3))
		r = newExprRun(t, "1 + )")
		e = r.node(r.p.expr(0))
		check("1 + )", e.Span, r.span(0, 3))
		missing = r.node(e.Rhs)
		isError("1 + ): missing operand", missing)
		check("1 + ): missing operand", missing.Span, r.span(4, 4))
		r = newExprRun(t, "(1 + 2")
		e = r.node(r.p.expr(0))
		isError("(1 + 2", e)
		check("(1 + 2", e.Span, r.span(0, 6))
		r = newExprRun(t, "f(1, 2")
		e = r.node(r.p.expr(0))
		isError("f(1, 2", e)
		check("f(1, 2", e.Span, r.span(0, 6))
	})

	t.Run("call_arg_list_span", func(t *testing.T) {
		// The OpSpan of a call is its argument list, from the `(` to the `)`
		// and not the callee's end plus one: the type checker draws E0402 on it.
		rows := []struct {
			src    string
			lo, hi uint32
		}{
			{"f(1, 2)", 1, 7},
			{"f()", 1, 3},
			{"f( )", 1, 4},
			{"f(1,)", 1, 5},
			{"f(\n1)", 1, 5},
			// A blank between the callee and the `(` is not part of the list,
			// and neither is the `)` that closes a callee in parentheses.
			{"f (1)", 2, 5},
			{"f  ()", 3, 5},
			{"(f)(1)", 3, 6},
			{"( f ) (1)", 6, 9},
		}
		for _, row := range rows {
			r := newExprRun(t, row.src)
			e := r.node(r.p.expr(0))
			if e.Kind != ExprCall {
				t.Errorf("%s: kind %d, want ExprCall (%d)", show(row.src), e.Kind, ExprCall)
				continue
			}
			if want := r.span(row.lo, row.hi); e.OpSpan != want {
				t.Errorf("%s: argument list span %v, want %v", show(row.src), e.OpSpan, want)
			}
		}
		// Each call of a chain has its own list, and the one of an argument
		// that is a call is inside the list of the outer call.
		r := newExprRun(t, "f(1)(2)")
		outer := r.node(r.p.expr(0))
		if want := r.span(4, 7); outer.OpSpan != want {
			t.Errorf("f(1)(2): outer list %v, want %v", outer.OpSpan, want)
		}
		if want := r.span(1, 4); r.node(outer.Callee).OpSpan != want {
			t.Errorf("f(1)(2): inner list %v, want %v", r.node(outer.Callee).OpSpan, want)
		}
		r = newExprRun(t, "f(g(1), 2)")
		outer = r.node(r.p.expr(0))
		if want := r.span(1, 10); outer.OpSpan != want {
			t.Errorf("f(g(1), 2): outer list %v, want %v", outer.OpSpan, want)
		}
		if want := r.span(3, 6); r.node(r.p.m.ExprList(outer.Args)[0]).OpSpan != want {
			t.Errorf("f(g(1), 2): list of g(1) %v, want %v", r.node(r.p.m.ExprList(outer.Args)[0]).OpSpan, want)
		}
	})

	t.Run("malformed_input_still_parses", func(t *testing.T) {
		// Each of these stops making sense inside an expression. The parser
		// must come back, print what it understood with an (error) where it
		// did not, and leave the cursor at the first token it did not use:
		// a `)` or a `}` that it has no use for must stay for the caller. The
		// diagnostics it reports for them are not looked at here (see
		// TestParseStmt/e0201_sites).
		rows := []struct {
			src  string
			want string
			rest TokenKind // the kind of the token the cursor is at afterwards
		}{
			{"", "(error)", Eof},
			{")", "(error)", RParen},
			{"}", "(error)", RBrace},
			{"let", "(error)", KwLet},
			{"1 +", "(+ 1 (error))", Eof},
			{"1 + )", "(+ 1 (error))", RParen},
			{"-", "(neg (error))", Eof},
			{"!", "(not (error))", Eof},
			{"1 2", "1", Int},
			{"(", "(error)", Eof},
			{"(1", "(error)", Eof},
			{"(1 + 2", "(error)", Eof},
			{"(1 2)", "(error)", Int},
			{"f(", "(error)", Eof},
			{"f(1", "(error)", Eof},
			{"f(1 2)", "(error)", Int},
			{"f(1,", "(error)", Eof},
			{"f(,)", "(call f (error))", Eof},
			{"f(1,,2)", "(call f 1 (error) 2)", Eof},
		}
		for _, row := range rows {
			r := newExprRun(t, row.src)
			e := r.p.expr(0)
			wantText(t, row.src, r.sexpr(e), row.want)
			if got := r.p.tok().Kind; got != row.rest {
				t.Errorf("%s: the cursor ends at %v, want %v", show(row.src), got, row.rest)
			}
		}
	})

	t.Run("chain_counts_calls", func(t *testing.T) {
		// Calls count as operators do: 1024 in a row are allowed, the 1025th
		// is reported at its `(`.
		wantExpr(t, "1024 calls", "f"+strings.Repeat("()", 1024), strings.Repeat("(call ", 1024)+"f"+strings.Repeat(")", 1024))

		src := "f" + strings.Repeat("()", 1025)
		r := newExprRun(t, src)
		e := r.p.expr(0)
		lo := lastIndex(t, src, "(")
		wantOnlyDiag(t, "1025 calls", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "1025 calls", r.sexpr(e), "(error)")
		r.wantAt(t, "1025 calls", Eof, uint32(len(src)))
	})
	t.Run("chain_counts_inside_parens", func(t *testing.T) {
		// Operators inside parentheses and outside them are one count. 1000
		// inside and 24 after make 1024; 25 after make 1025.
		wantExpr(t, "1000 + 24 operators", "("+plusChain(1000)+")"+strings.Repeat(" + 1", 24), wrapPlus(wrapPlus("1", 1000), 24))

		src := "(" + plusChain(1000) + ")" + strings.Repeat(" + 1", 25)
		r := newExprRun(t, src)
		e := r.p.expr(0)
		lo := lastIndex(t, src, "+")
		wantOnlyDiag(t, "1000 + 25 operators", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "1000 + 25 operators", r.sexpr(e), "(error)")
		r.wantAt(t, "1000 + 25 operators", Eof, uint32(len(src)))
	})
	t.Run("chain_ignores_prefix_operators", func(t *testing.T) {
		// Only binary operators and calls count. 513 binary minus signs, each
		// followed by a prefix one, are 513 in the chain and not 1026.
		want := "1"
		for range 513 {
			want = "(- " + want + " (neg 1))"
		}
		wantExpr(t, "513 binary and 513 prefix", "1"+strings.Repeat(" - -1", 513), want)

		// What hangs from a prefix operator is part of the chain: 1000 calls in
		// the operand of `-` and 24 additions after it make 1024; 25 make 1025.
		calls := strings.Repeat("(call ", 1000) + "f" + strings.Repeat(")", 1000)
		operand := "-f" + strings.Repeat("()", 1000)
		wantExpr(t, "1000 calls under a prefix and 24 operators", operand+strings.Repeat(" + 1", 24), wrapPlus("(neg "+calls+")", 24))

		src := operand + strings.Repeat(" + 1", 25)
		r := newExprRun(t, src)
		e := r.p.expr(0)
		lo := lastIndex(t, src, "+")
		wantOnlyDiag(t, "1000 calls under a prefix and 25 operators", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "1000 calls under a prefix and 25 operators", r.sexpr(e), "(error)")
		r.wantAt(t, "1000 calls under a prefix and 25 operators", Eof, uint32(len(src)))
	})
	t.Run("chain_skips_to_statement_end", func(t *testing.T) {
		// After E0205 the parser skips the rest of the statement, and stops at
		// the token that ends it without consuming it: the next Newline or `;`
		// outside any braces, a `}` that closes nothing opened in the skipped
		// text, or the end of the file.
		over := plusChain(1025)
		rows := []struct {
			name   string
			src    string
			before int // how many tokens to consume before reading the expression
			rest   TokenKind
			lo     uint32 // where the token the cursor stops at starts
		}{
			{"semicolon", over + "; x", 0, Semi, uint32(len(over))},
			{"newline", over + "\nx", 0, Newline, uint32(len(over))},
			{"the rest of the line", over + " + 1 + 1;", 0, Semi, uint32(len(over) + len(" + 1 + 1"))},
			{"closing brace", over + " + 1 }", 0, RBrace, uint32(len(over) + len(" + 1 "))},
			{"end of file", over + " + 1 + 1", 0, Eof, uint32(len(over) + len(" + 1 + 1"))},
			// The overflow is inside a call: the `)` and what follows it are
			// part of the statement.
			{"inside a call", "f(" + plusChain(1024) + " + 2) + 3; x", 0, Semi, uint32(len("f(" + plusChain(1024) + " + 2) + 3"))},
			// A line break inside parentheses is not a Newline token, so the one
			// after the `)` ends the statement.
			{"inside parentheses", "((" + plusChain(1025) + "\n)) + 3\nx", 0, Newline, uint32(len("((" + plusChain(1025) + "\n)) + 3"))},
			{"unclosed call", "f(" + plusChain(1024) + " + 2;", 0, Semi, uint32(len("f(" + plusChain(1024) + " + 2"))},
			// The token that passes the limit is the `(` of a call, and a `;` is
			// inside it. The `(` is left for the skip, which opens it, so the `;`
			// between it and its `)` does not end the statement. A parser that
			// consumed the `(` before counting it would leave it out of the skip,
			// and the skip would stop at that `;`.
			{"call with a semicolon inside", plusChain(1024) + "(a; b) + 1; x", 0, Semi, uint32(len(plusChain(1024) + "(a; b) + 1"))},
			// Braces opened in the skipped text close in it: the `;` and the
			// line breaks between them do not end the statement.
			{"braces", over + " + { ; \n 1 \n } ; x", 0, Semi, uint32(len(over + " + { ; \n 1 \n } "))},
			{"nested braces", over + " + { { ; } ; } ; x", 0, Semi, uint32(len(over + " + { { ; } ; } "))},
			// A statement inside a block: the `{` that was consumed before the
			// expression began does not belong to the statement, so the `;` that
			// ends it is at depth 0 and stops the skip.
			{"statement in a block", "{ " + over + "; y }", 1, Semi, uint32(len("{ " + over))},
		}
		for _, row := range rows {
			r := newExprRun(t, row.src)
			for range row.before {
				r.p.advance()
			}
			startLo := r.p.tok().Span.Lo
			e := r.p.expr(0)
			if n := r.sink.Count(diag.E(205)); n != 1 || len(r.sink.All()) != 1 {
				t.Errorf("%s: %d diagnostics (%d of them E0205), want exactly 1: %v", row.name, len(r.sink.All()), n, r.sink.All())
			}
			wantText(t, row.name, r.sexpr(e), "(error)")
			// The error node covers the statement up to the end of the last token
			// that was skipped, not up to the blanks in front of the next one.
			end := uint32(len(strings.TrimRight(row.src[:row.lo], " \n")))
			if got, want := r.node(e).Span, r.span(startLo, end); got != want {
				t.Errorf("%s: the error node spans %v, want %v", row.name, got, want)
			}
			r.wantAt(t, row.name, row.rest, row.lo)
		}
	})
	t.Run("chain_overflow_at_a_comparison", func(t *testing.T) {
		// The operator that passes the limit can itself be a comparison, inside
		// a parenthesis that is the right operand of another comparison. The
		// comparisons around it must not take it for a chained one, and must not
		// count it again: E0205 is the only thing reported about that token.
		src := "x < (" + plusChain(1023) + " < 1)"
		r := newExprRun(t, src)
		e := r.p.expr(0)
		lo := lastIndex(t, src, "<")
		wantOnlyDiag(t, "operand of a comparison", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "operand of a comparison", r.sexpr(e), "(error)")
		r.wantAt(t, "operand of a comparison", Eof, uint32(len(src)))

		// The same inside the operand of a comparison that has been chained: the
		// loop that reads the rest of the chain meets the token that overflowed.
		src = "a < b < (" + plusChain(1022) + " < 1)"
		r = newExprRun(t, src)
		e = r.p.expr(0)
		lo = lastIndex(t, src, "<")
		want := []diag.Diagnostic{chainedDiag(r.file, 2, 3, 6, 7, genericChainHelp, "&& b "), tooLongDiag(r.file, lo, lo+1)}
		if got := r.sink.All(); !reflect.DeepEqual(got, want) {
			t.Errorf("operand of a chained comparison: diagnostics\n%+v\nwant\n%+v", got, want)
		}
		wantText(t, "operand of a chained comparison", r.sexpr(e), "(error)")
		r.wantAt(t, "operand of a chained comparison", Eof, uint32(len(src)))
	})
	t.Run("chain_counter_restarts", func(t *testing.T) {
		// The count belongs to one statement-level expression. Two in a row,
		// each at the limit, are fine...
		src := plusChain(1024) + "; " + plusChain(1024)
		r := newExprRun(t, src)
		first := r.sexpr(r.p.expr(0))
		r.wantAt(t, "first of two", Semi, uint32(len(plusChain(1024))))
		r.p.advance()
		second := r.sexpr(r.p.expr(0))
		wantText(t, "first of two", first, wrapPlus("1", 1024))
		wantText(t, "second of two", second, wrapPlus("1", 1024))
		if n := len(r.sink.All()); n != 0 {
			t.Errorf("two chains of 1024: %d diagnostics, want none: %v", n, r.sink.All())
		}
		r.wantAt(t, "second of two", Eof, uint32(len(src)))

		// ...and one that overflows does not leave the parser in the middle of
		// an abandoned expression: the next one is read as usual, and nothing
		// more is reported.
		src = plusChain(1025) + "; 1 + 2"
		r = newExprRun(t, src)
		first = r.sexpr(r.p.expr(0))
		r.wantAt(t, "overflow then expression", Semi, uint32(len(plusChain(1025))))
		r.p.advance()
		second = r.sexpr(r.p.expr(0))
		wantText(t, "overflowing one", first, "(error)")
		wantText(t, "the one after it", second, "(+ 1 2)")
		lo := lastIndex(t, plusChain(1025), "+")
		wantOnlyDiag(t, "overflow then expression", r.sink, tooLongDiag(r.file, lo, lo+1))
		r.wantAt(t, "overflow then expression", Eof, uint32(len(src)))
	})

	t.Run("chained_comparison_counts_toward_chain", func(t *testing.T) {
		// The operators that the parser reads to get past a chained comparison
		// are operators of the expression like any other: 1021 additions, two
		// comparisons and the two additions in the operand of the second are
		// 1025. E0203 comes first, at the second comparison; E0205 at the last
		// addition, which is the 1025th and is four tokens after the second
		// comparison, so that the rule that suppresses a mistake close to the
		// last one reported leaves it alone.
		src := plusChain(1021) + " < 1 < 1 + 1 + 1"
		r := newExprRun(t, src)
		e := r.p.expr(0)
		base := uint32(len(plusChain(1021)))
		want := []diag.Diagnostic{
			chainedDiag(r.file, base+1, base+2, base+5, base+6, genericChainHelp, "&& 1 "),
			tooLongDiag(r.file, base+13, base+14),
		}
		if got := r.sink.All(); !reflect.DeepEqual(got, want) {
			t.Errorf("diagnostics\n%+v\nwant\n%+v", got, want)
		}
		wantText(t, "1021 additions, 2 comparisons and 2 more additions", r.sexpr(e), "(error)")
		r.wantAt(t, "1021 additions, 2 comparisons and 2 more additions", Eof, uint32(len(src)))
	})
	t.Run("nested_expr_keeps_outer_count", func(t *testing.T) {
		// A statement inside a block inside an expression has its own count of
		// operators, and the expression around the block goes on with the count
		// it had. The three sources are expressions with a block in the middle.
		//
		// The expression around the block is at the limit when the block starts
		// (1023 operators, and the `+` in front of the block is the 1024th), and
		// the tail of the block has 1024 operators of its own. If the two shared
		// a count, the block would overflow.
		wantExpr(t, "both at the limit", plusChain(1023)+" + { "+plusChain(1024)+" }",
			"(+ "+wrapPlus("1", 1023)+" (block "+wrapPlus("1", 1024)+"))")

		// The block ends and the expression around it goes on with the count it
		// had, not with the block's: 1023 and the `+` before the block are 1024,
		// and the `+` after it is the 1025th. The block's own `+` counts for the
		// block only; if it counted for the expression around, the overflow would
		// be there, and if the count of the block replaced the other, there
		// would be none.
		src := plusChain(1023) + " + { 1 + 1 } + 1"
		r := newExprRun(t, src)
		e := r.p.expr(0)
		lo := lastIndex(t, src, "+")
		wantOnlyDiag(t, "outer count restored", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "outer count restored", r.sexpr(e), "(error)")
		r.wantAt(t, "outer count restored", Eof, uint32(len(src)))

		// A statement of the block that overflows is abandoned alone: the block
		// and the expression around it are read as usual afterwards, and are not
		// marked as too long.
		const prefix = "1 + 1 + 1 + 1 + 1 + { "
		src = prefix + plusChain(1025) + " } + 1"
		r = newExprRun(t, src)
		e = r.p.expr(0)
		lo = uint32(len(prefix) + strings.LastIndex(plusChain(1025), "+"))
		wantOnlyDiag(t, "inner overflows", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "inner overflows", r.sexpr(e), "(+ (+ "+wrapPlus("1", 4)+" (block (error))) 1)")
		r.wantAt(t, "inner overflows", Eof, uint32(len(src)))
	})
	t.Run("skip_stops_at_the_end_of_the_inner_statement", func(t *testing.T) {
		// A statement inside a block is abandoned up to its own end, not to the
		// end of the statement that holds the block: the `}` of the block ends it
		// (and is left for the block), and so does a `;`. The skip counts the
		// brackets of the tokens it passes, not those that enclose the statement.
		over := plusChain(1025)
		overAt := uint32(strings.LastIndex(over, "+")) // the 1025th operator
		rows := []struct {
			name, src string
			want      string
			inner     uint32 // where the text of the statement starts, to place the E0205
		}{
			// The `}` ends the inner statement, and the call goes on to its `, 2)`.
			{"call argument that is a block", "f({ " + over + " }, 2) + 3", "(+ (call f (block (error)) 2) 3)", uint32(len("f({ "))},
			// The `;` ends it, and the block goes on to `y`.
			{"statement of a block", "{ x; " + over + "; y }", "(block (expr x) (expr (error)) y)", uint32(len("{ x; "))},
			// The same two, one block deeper.
			{"nested block", "{ { " + over + " } ; y }", "(block (expr (block (error))) y)", uint32(len("{ { "))},
		}
		for _, row := range rows {
			r := newExprRun(t, row.src)
			e := r.p.expr(0)
			lo := row.inner + overAt
			wantOnlyDiag(t, row.name, r.sink, tooLongDiag(r.file, lo, lo+1))
			wantText(t, row.name, r.sexpr(e), row.want)
			r.wantAt(t, row.name, Eof, uint32(len(row.src)))
		}
	})
}
