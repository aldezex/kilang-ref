package syntax

import (
	"reflect"
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

// chainedDiag is the E0203 that a second comparison operator at [lo, hi) gets.
func chainedDiag(file base.FileID, lo, hi uint32) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Code:     diag.E(203),
		Message:  "comparison operators cannot be chained",
		Primary:  diag.Label{Span: base.Span{File: file, Lo: lo, Hi: hi}},
		Help:     "use `&&`: `a < b && b < c`",
	}
}

// tooLongDiag is the E0205 that the operator or call parenthesis at [lo, hi)
// gets when it is the 1025th of its expression.
func tooLongDiag(file base.FileID, lo, hi uint32) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Code:     diag.E(205),
		Message:  "expression chain is too long",
		Primary:  diag.Label{Span: base.Span{File: file, Lo: lo, Hi: hi}},
		Notes:    []string{"the limit is 1024 operators and calls in one expression"},
	}
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
		wantOnlyDiag(t, src, r.sink, chainedDiag(r.file, 6, 7))
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
		rows := []struct {
			src    string
			lo, hi uint32 // the second comparison operator
			want   string
		}{
			// Every pair of comparison operators is a chain: they are one level.
			{"a == b != c", 7, 9, "(error)"},
			{"a < b == c", 6, 8, "(error)"},
			{"a <= b > c", 7, 8, "(error)"},
			{"a >= b <= c", 7, 9, "(error)"},
			{"a != b < c", 7, 8, "(error)"},
			// The operand after the second operator is read in full.
			{"a < b < c + 1", 6, 7, "(error)"},
			// The loop that reads the operators goes on after the chain.
			{"a < b < c && d", 6, 7, "(&& (error) d)"},
			// Three in a row are one mistake, reported at the second operator,
			// and the rest of the chain is read.
			{"a < b < c < d", 6, 7, "(error)"},
			{"a < b < c == d != e", 6, 7, "(error)"},
			// The operators around the chain go on being read.
			{"x && a < b < c && y", 11, 12, "(&& (&& x (error)) y)"},
			{"a + b < c * d < e", 14, 15, "(error)"},
			{"f(a < b < c, d)", 8, 9, "(call f (error) d)"},
			{"-1 < x < 2", 7, 8, "(error)"},
		}
		for _, row := range rows {
			r := newExprRun(t, row.src)
			e := r.p.expr(0)
			wantOnlyDiag(t, row.src, r.sink, chainedDiag(r.file, row.lo, row.hi))
			wantText(t, row.src, r.sexpr(e), row.want)
			r.wantAt(t, row.src, Eof, uint32(len(row.src)))
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

	t.Run("malformed_input_still_parses", func(t *testing.T) {
		// Each of these stops making sense inside an expression. The parser
		// reports nothing about it yet (the next session adds E0201), but it
		// must come back, print what it understood with an (error) where it
		// did not, and leave the cursor at the first token it did not use:
		// a `)` or a `}` that it has no use for must stay for the caller. The
		// sessions that recover from errors may change what is printed here.
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
		want := []diag.Diagnostic{chainedDiag(r.file, 6, 7), tooLongDiag(r.file, lo, lo+1)}
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
		// are operators of the expression like any other: 1022 additions and
		// three comparisons are 1025. E0203 comes first, at the second
		// comparison; E0205 at the third, which is the 1025th.
		src := plusChain(1022) + " < 1 < 1 < 1"
		r := newExprRun(t, src)
		e := r.p.expr(0)
		first := uint32(len(plusChain(1022)) + 1) // the first `<`; each next one is 4 bytes on
		want := []diag.Diagnostic{
			chainedDiag(r.file, first+4, first+5),
			tooLongDiag(r.file, first+8, first+9),
		}
		if got := r.sink.All(); !reflect.DeepEqual(got, want) {
			t.Errorf("diagnostics\n%+v\nwant\n%+v", got, want)
		}
		wantText(t, "1022 additions and 3 comparisons", r.sexpr(e), "(error)")
		r.wantAt(t, "1022 additions and 3 comparisons", Eof, uint32(len(src)))
	})
	t.Run("nested_expr_keeps_outer_count", func(t *testing.T) {
		// A statement inside a block inside an expression has its own count,
		// and the expression around it goes on with the count it had. The
		// parser cannot reach this through the grammar yet, so the test starts
		// from a parser that is in the middle of an expression.
		//
		// An outer expression at the limit does not make the inner one report...
		r := newExprRun(t, "1 + 1")
		outer := chainState{from: 0, count: MaxChain}
		r.p.chain = outer
		wantText(t, "inner at the limit of the outer", r.sexpr(r.p.expr(0)), "(+ 1 1)")
		if n := len(r.sink.All()); n != 0 {
			t.Errorf("inner expression: %d diagnostics, want none: %v", n, r.sink.All())
		}
		if r.p.chain != outer {
			t.Errorf("chain state afterwards is %+v, want %+v", r.p.chain, outer)
		}

		// ...nor does an outer one that has already been marked as too long: the
		// inner one starts from a clean state, and reports nothing.
		r = newExprRun(t, "1 + 1")
		outer = chainState{from: 0, count: MaxChain + 1, tooLong: true}
		r.p.chain = outer
		wantText(t, "inner of an outer that is too long", r.sexpr(r.p.expr(0)), "(+ 1 1)")
		if n := len(r.sink.All()); n != 0 {
			t.Errorf("inner expression: %d diagnostics, want none: %v", n, r.sink.All())
		}
		if r.p.chain != outer {
			t.Errorf("chain state afterwards is %+v, want %+v", r.p.chain, outer)
		}

		// ...and an inner one that overflows leaves the outer one as it was.
		r = newExprRun(t, plusChain(1025))
		outer = chainState{from: 0, count: 5}
		r.p.chain = outer
		wantText(t, "inner that overflows", r.sexpr(r.p.expr(0)), "(error)")
		if got := r.sink.Count(diag.E(205)); got != 1 {
			t.Errorf("inner expression: %d E0205, want 1", got)
		}
		if r.p.chain != outer {
			t.Errorf("chain state afterwards is %+v, want %+v", r.p.chain, outer)
		}
	})
	t.Run("skip_statement_counts_consumed_braces", func(t *testing.T) {
		// skipStatement starts in the middle of a statement. The braces that the
		// statement opened before that point count: the `;` inside them does
		// not end it, the `;` after the `}` that closes them does. The parser
		// cannot consume a brace in an expression yet, so the test moves the
		// cursor by hand.
		const src = "{ x ; y } ; z"
		r := newExprRun(t, src)
		r.p.advance() // {
		r.p.advance() // x
		r.p.skipStatement()
		r.wantAt(t, "inside a brace", Semi, uint32(strings.LastIndex(src, ";")))

		// A brace that was opened before the statement began counts for nothing:
		// it is the block that holds the statement. Here the statement is `x`,
		// and its `;` is at depth 0 even though a `{` came before it.
		const inBlock = "{ x ; y }"
		r = newExprRun(t, inBlock)
		r.p.advance() // {
		r.p.chain.from = r.p.pos
		r.p.advance() // x
		r.p.skipStatement()
		r.wantAt(t, "statement in a block", Semi, uint32(strings.Index(inBlock, ";")))

		// A brace that was opened and closed before that point counts for nothing.
		const closed = "{ x } ; y"
		r = newExprRun(t, closed)
		for range 3 { // { x }
			r.p.advance()
		}
		r.p.skipStatement()
		r.wantAt(t, "after a closed brace", Semi, uint32(strings.Index(closed, ";")))
	})
}
