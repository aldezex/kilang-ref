package syntax

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// This file is in package syntax, like parser_expr_internal_test.go, because it
// reads a statement with the unexported parser.letStmt, as TestParseExpr does
// with expr. It builds on that file's helpers (newExprRun, wantExpr, wantText,
// plusChain…), and the helpers it defines (nth, after, e0201, parseModule…) are
// there for every internal test of the package: a test that needs one reuses it
// and does not write a copy.

// stmtSexpr prints the statement s of the run's module.
func (r *exprRun) stmtSexpr(s StmtID) string {
	pr := printer{m: r.p.m, in: r.in}
	pr.stmt(s)
	return pr.b.String()
}

// parseLetSexpr lexes src, reads a `let` statement with p.letStmt and returns
// what the printer writes for it. Like parseExprSexpr, it makes the test fail
// if the parser reports anything or does not use the whole source.
func parseLetSexpr(t *testing.T, src string) string {
	t.Helper()
	r := newExprRun(t, src)
	s := r.p.letStmt()
	if diags := r.sink.All(); len(diags) != 0 {
		t.Errorf("%s: %d diagnostics, want none: %v", show(src), len(diags), diags)
	}
	if !r.p.at(Eof) {
		t.Errorf("%s: the statement stops at token %d (%v), want it to use the whole source", show(src), r.p.pos, r.p.tok().Kind)
	}
	return r.stmtSexpr(s)
}

// moduleRun is the result of calling Parse on one source.
type moduleRun struct {
	m    *Module
	in   *base.Interner
	sink *diag.Sink
	file base.FileID
	src  string
}

// parseModuleLoose lexes and parses src as a file. The lexical errors stay in
// the sink, as the first diagnostics; the second result is how many there are.
func parseModuleLoose(t *testing.T, src string) (*moduleRun, int) {
	t.Helper()
	sm, file := newSource(t, src)
	in := base.NewInterner()
	sink := &diag.Sink{}
	tb := Lex(sm, file, in, sink)
	lexical := len(sink.All())
	m := Parse(&tb, file, in, sink)
	return &moduleRun{m: m, in: in, sink: sink, file: file, src: src}, lexical
}

// parseModule is parseModuleLoose for a source without lexical errors, so that
// every diagnostic comes from the parser.
func parseModule(t *testing.T, src string) *moduleRun {
	t.Helper()
	r, lexical := parseModuleLoose(t, src)
	if lexical != 0 {
		t.Fatalf("%s: lexing reported %d diagnostics: %v", show(src), lexical, r.sink.All())
	}
	return r
}

// text returns what PrintModule writes for the module.
func (r *moduleRun) text() string { return PrintModule(r.m, r.in) }

// messages returns the message of every diagnostic, in report order.
func (r *moduleRun) messages() []string {
	var msgs []string
	for _, d := range r.sink.All() {
		msgs = append(msgs, d.Message)
	}
	return msgs
}

// body returns the body of the first item, which must be a function with one.
func (r *moduleRun) body(t *testing.T) Expr {
	t.Helper()
	if len(r.m.Items) == 0 {
		t.Fatalf("%s: no items", show(r.src))
	}
	it := r.m.ItemNodes.Get(r.m.Items[0])
	if it.Kind != ItemFn || !it.HasBody {
		t.Fatalf("%s: the first item is %+v, want a function with a body", show(r.src), it)
	}
	return r.m.Exprs.Get(it.Body)
}

// span returns the span [lo, hi) of this run's file.
func (r *moduleRun) span(lo, hi uint32) base.Span { return base.Span{File: r.file, Lo: lo, Hi: hi} }

// wantModuleText fails the test unless src parses without a diagnostic and
// prints as want.
func wantModuleText(t *testing.T, what, src, want string) {
	t.Helper()
	r := parseModule(t, src)
	if n := len(r.sink.All()); n != 0 {
		t.Errorf("%s: %s: %d diagnostics, want none: %v", what, show(src), n, r.sink.All())
	}
	wantText(t, what+": "+show(src), r.text(), want)
}

// e0201 is the E0201 with the given whole message and its primary label at
// [lo, hi) of file. It is written out here, field by field, and not built with
// the parser's own helper, so that a change in the message or in the label is
// noticed.
func e0201(file base.FileID, lo, hi uint32, message string) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Code:     diag.E(201),
		Message:  message,
		Primary:  diag.Label{Span: base.Span{File: file, Lo: lo, Hi: hi}},
	}
}

// nth returns the byte offset of the n-th occurrence (counting from 0) of text
// in src.
func nth(t *testing.T, src, text string, n int) uint32 {
	t.Helper()
	at := 0
	for i := 0; ; i++ {
		j := strings.Index(src[at:], text)
		if j < 0 {
			t.Fatalf("%s has no occurrence number %d of %q", show(src), n, text)
		}
		if i == n {
			return uint32(at + j)
		}
		at += j + len(text)
	}
}

// after returns the span of text where it comes right after prefix in src.
func after(t *testing.T, file base.FileID, src, prefix, text string) base.Span {
	t.Helper()
	i := strings.Index(src, prefix+text)
	if i < 0 {
		t.Fatalf("%q is not in %s", prefix+text, show(src))
	}
	lo := uint32(i + len(prefix))
	return base.Span{File: file, Lo: lo, Hi: lo + uint32(len(text))}
}

func TestParseStmt(t *testing.T) {
	t.Run("block_tail_expression", func(t *testing.T) {
		wantExpr(t, "example", "{ let x = 1\n x }", "(block (let x _ 1) x)")
	})
	t.Run("semicolon_makes_statement", func(t *testing.T) {
		wantExpr(t, "example", "{ f(); }", "(block (expr (call f)))")
	})
	t.Run("empty_block", func(t *testing.T) {
		wantExpr(t, "example", "{}", "(block)")
	})
	t.Run("let_with_type", func(t *testing.T) {
		wantText(t, "example", parseLetSexpr(t, "let x: Int = 1"), "(let x Int 1)")
	})
	t.Run("let_without_init_E0201", func(t *testing.T) {
		const src = "fn f() { let x }"
		r := parseModule(t, src)
		// The diagnostic sits on the `}` where the `=` should be, and it is the
		// only one: the `}` still closes the block.
		lo := nth(t, src, "}", 0)
		wantOnlyDiag(t, src, r.sink, e0201(r.file, lo, lo+1, "expected `=`, found `}`"))
	})
	t.Run("if_else_chain", func(t *testing.T) {
		wantExpr(t, "example", "if a { 1 } else if b { 2 } else { 3 }", "(if a (block 1) (if b (block 2) (block 3)))")
	})
	t.Run("if_without_else", func(t *testing.T) {
		wantExpr(t, "example", "if a { f() }", "(if a (block (call f)))")
	})
	t.Run("newline_before_closing_brace", func(t *testing.T) {
		wantExpr(t, "example", "{\n f()\n}", "(block (call f))")
	})
	t.Run("found_token_descriptions", func(t *testing.T) {
		// Only the message of the E0201 is compared. After the first mistake
		// the parser goes on at the next token (recovery is for a later
		// session), so the follow-up mistakes that this causes (`let x = 1` at
		// the top level is four tokens that are none of them an item) are not
		// pinned here: only the first diagnostic is.
		rows := []struct{ src, want string }{
			{"fn f() { let x }", "expected `=`, found `}`"},
			{"fn f() { let x\n}", "expected `=`, found newline"},
			{"fn f() { let x", "expected `=`, found end of file"},
			{"fn f() { let 1 }", "expected identifier, found `1`"},
			{"let x = 1", "expected item, found `let`"},
		}
		for _, row := range rows {
			r := parseModule(t, row.src)
			diags := r.sink.All()
			if len(diags) == 0 {
				t.Errorf("%s: no diagnostics, want an E0201 %q", show(row.src), row.want)
				continue
			}
			if d := diags[0]; d.Code != diag.E(201) || d.Message != row.want {
				t.Errorf("%s: first diagnostic is %s %q, want E0201 %q", show(row.src), d.Code, d.Message, row.want)
			}
		}
	})

	t.Run("block_statement_forms", func(t *testing.T) {
		// What ends a statement and what makes an expression the tail of its
		// block: a `;` makes a statement of it, a line break makes one of it
		// unless the next token is the `}`, and a `}` right after it makes the
		// tail. A `let` is always a statement, with or without a separator.
		rows := []struct{ src, want string }{
			{"{ 1 }", "(block 1)"},
			{"{ x; }", "(block (expr x))"},
			{"{ x;\n }", "(block (expr x))"},
			{"{ f(); g() }", "(block (expr (call f)) (call g))"},
			{"{ f()\n g() }", "(block (expr (call f)) (call g))"},
			{"{ f();\n g() }", "(block (expr (call f)) (call g))"},
			{"{ f(); g(); }", "(block (expr (call f)) (expr (call g)))"},
			{"{ f()\n g()\n }", "(block (expr (call f)) (call g))"},
			{"{ let x = 1 }", "(block (let x _ 1))"},
			{"{ let x = 1; }", "(block (let x _ 1))"},
			{"{ let x = 1\n }", "(block (let x _ 1))"},
			{"{ let x = 1; x }", "(block (let x _ 1) x)"},
			{"{ let x = 1\n let y = 2\n x + y }", "(block (let x _ 1) (let y _ 2) (+ x y))"},
			// A line that starts with `-` is a new statement, not a subtraction.
			{"{ 1 + 2\n -3 }", "(block (expr (+ 1 2)) (neg 3))"},
			{"{ { 1 } }", "(block (block 1))"},
			{"{ { 1 }\n 2 }", "(block (expr (block 1)) 2)"},
			{"{ { 1 }; 2 }", "(block (expr (block 1)) 2)"},
			{"{ if a { 1 }\n 2 }", "(block (expr (if a (block 1))) 2)"},
			{"{ if a { 1 } else { 2 } }", "(block (if a (block 1) (block 2)))"},
			{"{ if a { 1 };\n }", "(block (expr (if a (block 1))))"},
		}
		for _, row := range rows {
			wantExpr(t, row.src, row.src, row.want)
		}
	})
	t.Run("nested_blocks_keep_their_lists", func(t *testing.T) {
		// The statements of a block are one list of the pool, and the ones of
		// the blocks inside it are other lists, which must not mix with it.
		rows := []struct{ src, want string }{
			{"{ let a = { let b = 1; b }; { c; d }; e }",
				"(block (let a _ (block (let b _ 1) b)) (expr (block (expr c) d)) e)"},
			{"{ { a; b }; { c; d } }", "(block (expr (block (expr a) b)) (block (expr c) d))"},
			{"{ f({ a; b }, { c }); g }", "(block (expr (call f (block (expr a) b) (block c))) g)"},
			{"{ { { x; } } ; y; z }", "(block (expr (block (block (expr x)))) (expr y) z)"},
		}
		for _, row := range rows {
			wantExpr(t, row.src, row.src, row.want)
		}
	})
	t.Run("let_forms", func(t *testing.T) {
		rows := []struct{ src, want string }{
			{"let x = 1", "(let x _ 1)"},
			{"let x: Bool = true", "(let x Bool true)"},
			{"let x: () = ()", "(let x () ())"},
			{"let fooBar = x", "(let fooBar _ x)"},
			{"let x: Int = -1", "(let x Int (neg 1))"},
			{"let x = f(1) + 2 * 3", "(let x _ (+ (call f 1) (* 2 3)))"},
			{"let x = { 1 }", "(let x _ (block 1))"},
			{"let x = if a { 1 } else { 2 }", "(let x _ (if a (block 1) (block 2)))"},
		}
		for _, row := range rows {
			wantText(t, row.src, parseLetSexpr(t, row.src), row.want)
		}
	})
	t.Run("let_missing_equals_keeps_the_name", func(t *testing.T) {
		// The name is read before the `=` is missed, so the `let` is still a
		// `let` that binds it: the statements after it can use `x` without
		// getting an error about a name nobody defined. Its initializer is an
		// error node with an empty span where the `=` should have been.
		rows := []struct{ src, want string }{
			{"fn f() { let x }", "(fn f (params) (ret _) (block (let x _ (error))))\n"},
			{"fn f() { let x: Int }", "(fn f (params) (ret _) (block (let x Int (error))))\n"},
		}
		for _, row := range rows {
			r := parseModule(t, row.src)
			wantText(t, show(row.src), r.text(), row.want)
		}
		const src = "fn f() { let x }"
		r := parseModule(t, src)
		body := r.body(t)
		s := r.m.Stmts.Get(r.m.StmtList(body.Stmts)[0])
		if want := r.span(nth(t, src, "let", 0), nth(t, src, "x", 0)+1); s.Span != want {
			t.Errorf("the let spans %v, want %v (`let x`)", s.Span, want)
		}
		brace := nth(t, src, "}", 0)
		if want := r.span(brace, brace); r.m.Exprs.Get(s.Expr).Span != want {
			t.Errorf("the initializer spans %v, want the empty span %v at the `}`", r.m.Exprs.Get(s.Expr).Span, want)
		}
	})
	t.Run("if_forms", func(t *testing.T) {
		rows := []struct{ src, want string }{
			{"if a { 1 } else { 2 }", "(if a (block 1) (block 2))"},
			{"if a {}", "(if a (block))"},
			{"if a {} else {}", "(if a (block) (block))"},
			{"if a < b { 1 }", "(if (< a b) (block 1))"},
			{"if f(x) && !y { 1 }", "(if (&& (call f x) (not y)) (block 1))"},
			{"if (a) { 1 }", "(if a (block 1))"},
			{"if a { 1 } else if b { 2 }", "(if a (block 1) (if b (block 2)))"},
			{"if a { 1 } else if b { 2 } else if c { 3 } else { 4 }",
				"(if a (block 1) (if b (block 2) (if c (block 3) (block 4))))"},
			{"if a { if b { 1 } else { 2 } }", "(if a (block (if b (block 1) (block 2))))"},
			{"if a { 1 } else { if b { 2 } }", "(if a (block 1) (block (if b (block 2))))"},
			// An if is an expression, so it can be an operand.
			{"1 + if a { 2 } else { 3 }", "(+ 1 (if a (block 2) (block 3)))"},
			// An `else` on the next line belongs to the if: the line rule does
			// not end the statement in front of it.
			{"if a { 1 }\nelse { 2 }", "(if a (block 1) (block 2))"},
		}
		for _, row := range rows {
			wantExpr(t, row.src, row.src, row.want)
		}
	})
	t.Run("if_condition_chain_limit", func(t *testing.T) {
		// The condition of an if is an expression of its own, with its own
		// count of operators. A chain of 1024 in it is allowed; one of 1025 is
		// E0205, and the rest of the statement, the branches included, is
		// skipped with it, so no E0201 comes out of the `{` that "is missing".
		wantModuleText(t, "1024 operators", "fn f() { if "+plusChain(1024)+" { x } else { y } }",
			"(fn f (params) (ret _) (block (if "+wrapPlus("1", 1024)+" (block x) (block y))))\n")

		src := "fn f() { if " + plusChain(1025) + " { x } else { y } }"
		r := parseModule(t, src)
		lo := nth(t, src, "+", 1024)
		wantOnlyDiag(t, "1025 operators", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "1025 operators", r.text(), "(fn f (params) (ret _) (block (error)))\n")
		// The tail is the error node of the whole statement: from the `if` to the
		// end of the last token that was skipped, which is the `}` of the else
		// branch and not the `}` of the function.
		if body := r.body(t); !body.HasTail {
			t.Errorf("the block has no tail, want the error node of the statement")
		} else if want := r.span(nth(t, src, "if", 0), uint32(strings.LastIndex(src, "}"))-1); r.m.Exprs.Get(body.Tail).Span != want {
			t.Errorf("the error node spans %v, want %v", r.m.Exprs.Get(body.Tail).Span, want)
		}

		// The same in an else-if, and with the if as an operand.
		src = "fn f() { if a { 1 } else if " + plusChain(1025) + " { x } }"
		r = parseModule(t, src)
		lo = nth(t, src, "+", 1024)
		wantOnlyDiag(t, "else if", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "else if", r.text(), "(fn f (params) (ret _) (block (if a (block 1) (error))))\n")
		src = "fn f() { 1 + if " + plusChain(1025) + " { x } }"
		r = parseModule(t, src)
		lo = nth(t, src, "+", 1025) // the first `+` is the one in front of the if
		wantOnlyDiag(t, "operand", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "operand", r.text(), "(fn f (params) (ret _) (block (+ 1 (error))))\n")

		// The count of the condition is its own, not the one of the expression
		// that holds the if, in both directions. The 601 operators in front of
		// the condition and the 600 inside it make 1201 together, and none of
		// them is a mistake; and the `+ 1` after the if is the 602nd operator of
		// the outer expression, and not the 1202nd. The statements of a block
		// that is an operand count apart in the same way.
		wantModuleText(t, "own count in the condition",
			"fn f() { "+plusChain(600)+" + if "+plusChain(600)+" { x } else { y } + 1 }",
			"(fn f (params) (ret _) (block (+ (+ "+wrapPlus("1", 600)+" (if "+wrapPlus("1", 600)+" (block x) (block y))) 1)))\n")
		wantModuleText(t, "own count in a block operand",
			"fn f() { "+plusChain(600)+" + { "+plusChain(600)+" } + 1 }",
			"(fn f (params) (ret _) (block (+ (+ "+wrapPlus("1", 600)+" (block "+wrapPlus("1", 600)+")) 1)))\n")
	})
	t.Run("chain_limit_per_statement", func(t *testing.T) {
		// Every statement of a block is an expression with its own count: two
		// of 1024 operators in one block are fine, though they add up to 2048.
		src := "fn f() { " + plusChain(1024) + "; " + plusChain(1024) + " }"
		r := parseModule(t, src)
		if n := len(r.sink.All()); n != 0 {
			t.Errorf("two statements of 1024 operators: %d diagnostics, want none: %v", n, r.sink.All())
		}
		if body := r.body(t); body.Stmts.Count != 1 || !body.HasTail {
			t.Errorf("two statements of 1024 operators: block %+v, want one statement and a tail", body)
		}
		// One of 1025 is abandoned up to its `;`, and the statements around it
		// are read as usual.
		src = "fn f() { x; " + plusChain(1025) + "; y }"
		r = parseModule(t, src)
		lo := nth(t, src, "+", 1024)
		wantOnlyDiag(t, "one of 1025", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "one of 1025", r.text(), "(fn f (params) (ret _) (block (expr x) (expr (error)) y))\n")
		// The initializer of a `let` is one too: two of 1024 are fine, and one
		// of 1025 is abandoned up to the line break.
		src = "fn f() { let a = " + plusChain(1024) + "\n let b = " + plusChain(1024) + "\n a }"
		r = parseModule(t, src)
		if n := len(r.sink.All()); n != 0 {
			t.Errorf("two lets of 1024 operators: %d diagnostics, want none: %v", n, r.sink.All())
		}
		src = "fn f() { let a = " + plusChain(1025) + "\n a }"
		r = parseModule(t, src)
		lo = nth(t, src, "+", 1024)
		wantOnlyDiag(t, "let", r.sink, tooLongDiag(r.file, lo, lo+1))
		wantText(t, "let", r.text(), "(fn f (params) (ret _) (block (let a _ (error)) a))\n")
	})
	t.Run("item_bodies", func(t *testing.T) {
		// Parse reads the body of a function as a block.
		rows := []struct{ src, want string }{
			{"fn f() { 1 }", "(fn f (params) (ret _) (block 1))\n"},
			{"fn f(n: Int) -> Int { let m = n + 1\n m * 2 }", "(fn f (params (n Int)) (ret Int) (block (let m _ (+ n 1)) (* m 2)))\n"},
			{"fn a() { 1 }\nfn b() { 2 }", "(fn a (params) (ret _) (block 1))\n(fn b (params) (ret _) (block 2))\n"},
			{"fn a() {\n  f()\n}\nfn b() {}", "(fn a (params) (ret _) (block (call f)))\n(fn b (params) (ret _) (block))\n"},
			// The types of the module are numbered in the order they are read, so
			// the one of the `let` is not the first.
			{"fn f(a: Bool) -> Bool { let b: Int = 1\n a }", "(fn f (params (a Bool)) (ret Bool) (block (let b Int 1) a))\n"},
			{"pub fn a(x: Int)\nfn b() { x }", "(pub fn a (params (x Int)) (ret _) none)\n(fn b (params) (ret _) (block x))\n"},
			{"fn a() {\n  if c {\n    1\n  } else {\n    2\n  }\n}\n",
				"(fn a (params) (ret _) (block (if c (block 1) (block 2))))\n"},
		}
		for _, row := range rows {
			wantModuleText(t, row.src, row.src, row.want)
		}
	})
	t.Run("spans", func(t *testing.T) {
		const src = "fn f() {\n  let x: Int = 1 + 2;\n  g();\n  if c { 3 } else if d { 4 }\n}\n"
		r := parseModule(t, src)
		if n := len(r.sink.All()); n != 0 {
			t.Fatalf("%d diagnostics, want none: %v", n, r.sink.All())
		}
		check := func(what string, got, want base.Span) {
			t.Helper()
			if got != want {
				t.Errorf("%s: span %v, want %v", what, got, want)
			}
		}
		// A block runs from its `{` to its `}`.
		body := r.body(t)
		check("block", body.Span, r.span(nth(t, src, "{", 0), uint32(strings.LastIndex(src, "}"))+1))
		stmts := r.m.StmtList(body.Stmts)
		if len(stmts) != 2 || !body.HasTail {
			t.Fatalf("block has %d statements and tail %v, want 2 and true", len(stmts), body.HasTail)
		}

		// A `let` runs from the `let` to the end of its initializer, which
		// leaves the `;` out; NameSpan is the name alone.
		let := r.m.Stmts.Get(stmts[0])
		check("let", let.Span, after(t, r.file, src, "  ", "let x: Int = 1 + 2"))
		check("let name", let.NameSpan, after(t, r.file, src, "let ", "x"))
		check("let type", r.m.Types.Get(let.Type).Span, after(t, r.file, src, "x: ", "Int"))
		check("let initializer", r.m.Exprs.Get(let.Expr).Span, after(t, r.file, src, "= ", "1 + 2"))

		// An expression statement is its expression, without the `;`.
		es := r.m.Stmts.Get(stmts[1])
		check("expression statement", es.Span, after(t, r.file, src, "  ", "g()"))
		check("expression statement's expression", r.m.Exprs.Get(es.Expr).Span, after(t, r.file, src, "  ", "g()"))

		// An if runs from the `if` to the end of its last branch; an else-if
		// is an if of its own.
		tail := r.m.Exprs.Get(body.Tail)
		check("if", tail.Span, after(t, r.file, src, "  ", "if c { 3 } else if d { 4 }"))
		check("if condition", r.m.Exprs.Get(tail.Cond).Span, after(t, r.file, src, "if ", "c"))
		check("then", r.m.Exprs.Get(tail.Then).Span, after(t, r.file, src, "c ", "{ 3 }"))
		elseIf := r.m.Exprs.Get(tail.Else)
		check("else if", elseIf.Span, after(t, r.file, src, "else ", "if d { 4 }"))
		check("else if then", r.m.Exprs.Get(elseIf.Then).Span, after(t, r.file, src, "d ", "{ 4 }"))

		// A block that is never closed ends at the last token it read.
		const open = "fn f() { 1"
		r = parseModule(t, open)
		check("unclosed block", r.body(t).Span, r.span(nth(t, open, "{", 0), uint32(len(open))))
		// A let with no name is an error statement that covers the `let`.
		const noName = "fn f() { let 1 }"
		r = parseModule(t, noName)
		es = r.m.Stmts.Get(r.m.StmtList(r.body(t).Stmts)[0])
		if es.Kind != StmtError {
			t.Errorf("let with no name: statement %+v, want a StmtError", es)
		}
		check("let with no name", es.Span, after(t, r.file, noName, "{ ", "let"))

		// The block that an if or an else is missing is an empty error node at
		// the start of the token found instead of the `{`. That token can lie
		// past the end of the if that holds the node, which stops at the last
		// token it consumed: here, after the spaces in front of the line break
		// and after the spaces in front of the `}`.
		const noThen = "fn f() { if x   \n{ 1 } }"
		r = parseModule(t, noThen)
		es = r.m.Stmts.Get(r.m.StmtList(r.body(t).Stmts)[0])
		ifE := r.m.Exprs.Get(es.Expr)
		if ifE.Kind != ExprIf {
			t.Fatalf("if with no block: expression %+v, want an if", ifE)
		}
		check("if with no block", ifE.Span, after(t, r.file, noThen, "{ ", "if x"))
		lf := nth(t, noThen, "\n", 0)
		check("missing block of the if", r.m.Exprs.Get(ifE.Then).Span, r.span(lf, lf))
		if k := r.m.Exprs.Get(ifE.Then).Kind; k != ExprError {
			t.Errorf("missing block of the if: kind %d, want ExprError (%d)", k, ExprError)
		}
		const noElse = "fn f() { if x { 1 } else   }"
		r = parseModule(t, noElse)
		ifE = r.m.Exprs.Get(r.body(t).Tail)
		if ifE.Kind != ExprIf || !ifE.HasElse {
			t.Fatalf("if with no else block: expression %+v, want an if with an else", ifE)
		}
		check("if with no else block", ifE.Span, after(t, r.file, noElse, "{ ", "if x { 1 } else"))
		rb := nth(t, noElse, "}", 1)
		check("missing block of the else", r.m.Exprs.Get(ifE.Else).Span, r.span(rb, rb))
		if k := r.m.Exprs.Get(ifE.Else).Kind; k != ExprError {
			t.Errorf("missing block of the else: kind %d, want ExprError (%d)", k, ExprError)
		}
	})

	t.Run("e0201_sites", func(t *testing.T) {
		// One row for every place where the parser needs a token it does not
		// find. The first diagnostic must be the E0201 with this whole message
		// and its primary label on the token that was found: the `n`-th
		// occurrence (from 0) of at in the source, the line feed for "\n" or
		// the end of the file for "<eof>". What follows the first diagnostic
		// is not compared.
		rows := []struct {
			src, at string
			n       int
			want    string
		}{
			// expression
			{"fn f() { 1 + }", "}", 0, "expected expression, found `}`"},
			{"fn f() { ; }", ";", 0, "expected expression, found `;`"},
			{"fn f() { g(,) }", ",", 0, "expected expression, found `,`"},
			{"fn f() { - }", "}", 0, "expected expression, found `}`"},
			{"fn f() { let x = }", "}", 0, "expected expression, found `}`"},
			{"fn f() { if }", "}", 0, "expected expression, found `}`"},
			{"fn f() { 1 + else }", "else", 0, "expected expression, found `else`"},
			// identifier
			{"fn 1", "1", 0, "expected identifier, found `1`"},
			{"fn f(1) {}", "1", 0, "expected identifier, found `1`"},
			{"fn f(a, ) {}", ")", 0, "expected identifier, found `)`"},
			{"#[1] fn f() {}", "1", 0, "expected identifier, found `1`"},
			{"fn f() { let 1 }", "1", 0, "expected identifier, found `1`"},
			// type
			{"fn f(x: ) {}", ")", 0, "expected type, found `)`"},
			{"fn f() -> {}", "{", 0, "expected type, found `{`"},
			{"fn f(x: 1) {}", "1", 0, "expected type, found `1`"},
			{"fn f() { let x: = 1 }", "=", 0, "expected type, found `=`"},
			// `=`
			{"fn f() { let x 5 }", "5", 0, "expected `=`, found `5`"},
			{"fn f() { let x: Int }", "}", 0, "expected `=`, found `}`"},
			// `)`
			{"fn f(x y) {}", "y", 0, "expected `)`, found `y`"},
			{"fn f(x", "<eof>", 0, "expected `)`, found end of file"},
			{"fn f() { (1 2) }", "2", 0, "expected `)`, found `2`"},
			{"fn f() { g(1 2) }", "2", 0, "expected `)`, found `2`"},
			{"fn f() -> (Int) {}", "Int", 0, "expected `)`, found `Int`"},
			{"#[a(\"x\" \"y\")] fn f() {}", "\"y\"", 0, "expected `)`, found `\"y\"`"},
			// `{`
			{"fn f() { if a }", "}", 0, "expected `{`, found `}`"},
			{"fn f() { if a\n{ 1 } }", "\n", 0, "expected `{`, found newline"},
			{"fn f() { if a { 1 } else 2 }", "2", 0, "expected `{`, found `2`"},
			// `}`
			{"fn f() {", "<eof>", 0, "expected `}`, found end of file"},
			{"fn f() { 1", "<eof>", 0, "expected `}`, found end of file"},
			{"fn f() { if a { 1 }", "<eof>", 0, "expected `}`, found end of file"},
			// newline
			{"fn f() { f() x }", "x", 0, "expected newline, found `x`"},
			{"fn f() { let a = 1 b }", "b", 0, "expected newline, found `b`"},
			{"fn f() { 1 2 }", "2", 0, "expected newline, found `2`"},
			// item
			{"1", "1", 0, "expected item, found `1`"},
			{"#[a] 1", "1", 0, "expected item, found `1`"},
			{"pub 1", "1", 0, "expected item, found `1`"},
			{"fn f() {} )", ")", 1, "expected item, found `)`"},
			{"}", "}", 0, "expected item, found `}`"},
			{"let x = 1", "let", 0, "expected item, found `let`"},
			// the punctuation of the signature and of the attributes
			{"# fn f() {}", "fn", 0, "expected `[`, found `fn`"},
			{"#[a fn f() {}", "fn", 0, "expected `]`, found `fn`"},
			{"fn f x) {}", "x", 0, "expected `(`, found `x`"},
			{"#[a(1)] fn f() {}", "1", 0, "expected string, found `1`"},
			{"#[a(\"x\",)] fn f() {}", ")", 0, "expected string, found `)`"},
		}
		for _, row := range rows {
			r := parseModule(t, row.src)
			diags := r.sink.All()
			if len(diags) == 0 {
				t.Errorf("%s: no diagnostics, want %q", show(row.src), row.want)
				continue
			}
			var lo, hi uint32
			switch row.at {
			case "<eof>":
				lo, hi = uint32(len(row.src)), uint32(len(row.src))
			default:
				lo = nth(t, row.src, row.at, row.n)
				hi = lo + uint32(len(row.at))
			}
			if want := e0201(r.file, lo, hi, row.want); !reflect.DeepEqual(diags[0], want) {
				t.Errorf("%s: first diagnostic\n%+v\nwant\n%+v", show(row.src), diags[0], want)
			}
		}
	})
	t.Run("found_text_is_the_source_text", func(t *testing.T) {
		// The token that was found is quoted as the file spells it: not as its
		// value (`255` for `0xFF`), not as its kind, and not shortened.
		rows := []struct{ found, want string }{
			{"0xFF", "`0xFF`"},
			{"1_000", "`1_000`"},
			{"1.5", "`1.5`"},
			{"'a'", "`'a'`"},
			{`"a\n"`, "`\"a\\n\"`"},
			{`"a${x}b"`, "`\"a${`"},
			{"true", "`true`"},
			{"X", "`X`"},
			{"_", "`_`"},
			{"==", "`==`"},
			{"->", "`->`"},
			{"|>", "`|>`"},
			{"..", "`..`"},
		}
		for _, row := range rows {
			src := "fn f() { let " + row.found + " }"
			r := parseModule(t, src)
			diags := r.sink.All()
			want := "expected identifier, found " + row.want
			if len(diags) == 0 || diags[0].Message != want {
				t.Errorf("%s: diagnostics %v, want the first to say %q", show(src), r.messages(), want)
			}
		}
		// A character the lexer rejects is an Error token. The lexer reports
		// it (E0104), and the parser finds it where an expression should be.
		const src = "fn f() { $ }"
		r, lexical := parseModuleLoose(t, src)
		if lexical != 1 || r.sink.All()[0].Code != diag.E(104) {
			t.Fatalf("%s: %d lexical diagnostics, first %v, want one E0104", show(src), lexical, r.sink.All())
		}
		lo := nth(t, src, "$", 0)
		if got, want := r.sink.All()[1], e0201(r.file, lo, lo+1, "expected expression, found `$`"); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: parser diagnostic\n%+v\nwant\n%+v", show(src), got, want)
		}
	})
	t.Run("recovery_in_blocks", func(t *testing.T) {
		// What the parser makes of a block that goes wrong, with every
		// diagnostic it reports. The recovery of this session is the simplest:
		// the mistake becomes an error node and the parser goes on with the next
		// token. A token that cannot start a statement and that nothing was
		// read for is dropped, so that the loop moves on. A later session
		// recovers to the end of the statement and suppresses the follow-ups,
		// and then these rows change.
		rows := []struct {
			src  string
			want string
			msgs []string
		}{
			{"fn f() { f() x }", "(fn f (params) (ret _) (block (expr (call f)) x))\n",
				[]string{"expected newline, found `x`"}},
			{"fn f() { ) }", "(fn f (params) (ret _) (block (expr (error))))\n",
				[]string{"expected expression, found `)`"}},
			{"fn f() { ; }", "(fn f (params) (ret _) (block (expr (error))))\n",
				[]string{"expected expression, found `;`"}},
			{"fn f() { 1 + }", "(fn f (params) (ret _) (block (+ 1 (error))))\n",
				[]string{"expected expression, found `}`"}},
			{"fn f() { (1 }", "(fn f (params) (ret _) (block (error)))\n",
				[]string{"expected `)`, found `}`"}},
			{"fn f() {", "(fn f (params) (ret _) (block))\n",
				[]string{"expected `}`, found end of file"}},
			{"fn f() { 1", "(fn f (params) (ret _) (block 1))\n",
				[]string{"expected `}`, found end of file"}},
			{"fn f() { let x = 1", "(fn f (params) (ret _) (block (let x _ 1)))\n",
				[]string{"expected `}`, found end of file"}},
			{"fn f() { if a { 1 } else }", "(fn f (params) (ret _) (block (if a (block 1) (error))))\n",
				[]string{"expected `{`, found `}`"}},
			{"fn f() { 1 }}", "(fn f (params) (ret _) (block 1))\n(error)\n",
				[]string{"expected item, found `}`"}},
		}
		for _, row := range rows {
			r := parseModule(t, row.src)
			wantText(t, show(row.src), r.text(), row.want)
			if got := r.messages(); !slices.Equal(got, row.msgs) {
				t.Errorf("%s: diagnostics %q, want %q", show(row.src), got, row.msgs)
			}
		}
	})
	t.Run("malformed_blocks_terminate", func(t *testing.T) {
		// Sources that go wrong inside a block, cut short at every point of an
		// `if` and of a `let`, or with a token in the wrong place. For each, the
		// parser comes back after reading every token, reports at least one
		// E0201 (a source that is not a program must not parse in silence),
		// gives every item a span inside the file, and gives each item its own
		// piece of the file, in order. Each loop of the parser has to consume
		// a token or end, or this test never ends.
		srcs := []string{
			"fn f() {", "fn f() { 1", "fn f() { 1 +", "fn f() { let", "fn f() { let x", "fn f() { let x =",
			"fn f() { let x:", "fn f() { let x: Int", "fn f() { let x: Int =", "fn f() { if", "fn f() { if a", "fn f() { if a {",
			"fn f() { if a { 1 }", "fn f() { if a { 1 } else", "fn f() { if a { 1 } else {", "fn f() { if a { 1 } else if",
			"fn f() { ) }", "fn f() { } }", "fn f() { ; ; }", "fn f() { { { }", "fn f() { f( }", "fn f() { ((( }",
			"fn f() { 1 2 3 }", "fn f() { else }", "fn f() { let let }", "fn f() { let x = let }", "fn f() { 1 ) 2 }",
			"fn f() { , }", "fn f() { -> }", "fn f() { fn g() {} }", "fn f() { pub }", "fn f() { # }", "fn f() { 1 } }",
			"fn f() { {", "fn f() { if { } }", "fn f() { if a { let } }", "fn f() { if a b }", "fn f() { let x = if }",
			"fn f() { let x = { }", "fn f() { let x = {", "fn f() { = }", "fn f() { 1 = 2 }", "fn f() { f(1 2) }",
			"fn f() { ) ) ) }", "fn f() { ; } ; }", "fn f() { let ) }", "fn f() { if ) { } }", "fn f() { 1 +\n}",
		}
		for _, src := range srcs {
			r := newExprRun(t, src)
			r.p.module()
			if !r.p.at(Eof) {
				t.Errorf("%s: the parser stops at token %d (%v), want it to read them all", show(src), r.p.pos, r.p.tok().Kind)
			}
			if r.sink.Count(diag.E(201)) == 0 {
				t.Errorf("%s: no E0201 reported, want at least one", show(src))
			}
			prevHi := uint32(0)
			for _, id := range r.p.m.Items {
				sp := r.p.m.ItemNodes.Get(id).Span
				if sp.File != r.file || sp.Lo < prevHi || sp.Hi < sp.Lo || int(sp.Hi) > len(src) {
					t.Errorf("%s: item span %v after one that ends at %d, want an increasing span inside [0,%d)", show(src), sp, prevHi, len(src))
				}
				prevHi = sp.Hi
			}
		}
	})
}
