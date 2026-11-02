package core_test

import (
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/driver"
	"github.com/aldezex/kilang/internal/sema"
	"github.com/aldezex/kilang/internal/syntax"
	"github.com/aldezex/kilang/internal/testsupport"
)

// elabFun returns the function called name: the first one of that name in the
// Program, which is the user's if the prelude does not define it too.
func elabFun(t *testing.T, c *testsupport.Compiled, name string) core.Fun {
	t.Helper()
	for _, f := range c.Core.Funs.All() {
		if c.Session.Interner.String(f.Name) == name {
			return f
		}
	}
	t.Fatalf("the Program has no function called %q", name)
	panic("unreachable")
}

// elabText returns the source text that sp covers.
func elabText(c *testsupport.Compiled, sp base.Span) string {
	return c.Session.Sources.Text(sp)
}

// elabVar writes a variable as name@slot: its name and its place in the frame.
func elabVar(c *testsupport.Compiled, v core.VarID) string {
	vr := c.Core.Vars.Get(v)
	return c.Session.Interner.String(vr.Name) + "@" + strconv.FormatUint(uint64(vr.Slot), 10)
}

// elabShape writes the expression id on one line, with the fields that decide
// what it does and no spans: a literal as its value, a variable as name@slot, a
// function as @name, a call as callee(args), a primitive as name(args), a Let as
// `let v = init; body` and a Match as `match s { pattern => body; … }`. A test
// compares the whole line, so a node of the wrong kind, a swapped pair of
// children or a slot out of place shows as a different text. It recurses over
// the tree, which is fine for the small ones the tests build.
func elabShape(c *testsupport.Compiled, id core.ExprID) string {
	p := c.Core
	e := p.Exprs.Get(id)
	args := func(r base.ListRange) string {
		var parts []string
		for _, a := range p.Args(r) {
			parts = append(parts, elabShape(c, a))
		}
		return strings.Join(parts, ", ")
	}
	switch e.Kind {
	case core.LitInt:
		return strconv.FormatInt(e.Int, 10)
	case core.LitBool:
		return strconv.FormatBool(e.Bool)
	case core.LitUnit:
		return "()"
	case core.VarRef:
		return elabVar(c, e.Var)
	case core.FunRef:
		return "@" + c.Session.Interner.String(p.Funs.Get(e.Fun).Name)
	case core.Call:
		return elabShape(c, e.Callee) + "(" + args(e.Args) + ")"
	case core.PrimCall:
		return e.Prim.String() + "(" + args(e.Args) + ")"
	case core.Let:
		return "let " + elabVar(c, e.Var) + " = " + elabShape(c, e.Init) + "; " + elabShape(c, e.Body)
	case core.Match:
		var arms []string
		for _, a := range p.ArmsOf(e.Arms) {
			pat := "PatKind(" + strconv.Itoa(int(a.Pat)) + ")"
			switch a.Pat {
			case core.PatTrue:
				pat = "true"
			case core.PatFalse:
				pat = "false"
			case core.PatWildcard:
				pat = "_"
			}
			arms = append(arms, pat+" => "+elabShape(c, a.Body))
		}
		return "match " + elabShape(c, e.Scrutinee) + " { " + strings.Join(arms, "; ") + " }"
	}
	return fmt.Sprintf("ExprKind(%d)", e.Kind)
}

// elabWant fails the test unless the function called name has a body whose
// shape (see elabShape) is want.
func elabWant(t *testing.T, c *testsupport.Compiled, name, want string) {
	t.Helper()
	f := elabFun(t, c, name)
	if !f.HasBody {
		t.Errorf("fn %s has no body, want %s", name, want)
		return
	}
	if got := elabShape(c, f.Body); got != want {
		t.Errorf("fn %s: the body is\n\t%s\nwant\n\t%s", name, got, want)
	}
}

// elabStackCap is the stack, in bytes, that elabCompileCapped lets Go use.
const elabStackCap = 1 << 20

// elabCompileCapped is testsupport.Compile with the stack of Go capped at
// elabStackCap, and the cap lifted when it returns. The cap is what makes a
// recursion over the statements of a block show: the default limit is 1 GB, and
// a frame for each of 100 000 statements would fit in it without anyone
// noticing. Past the cap Go does not panic: it ends the whole test binary with
// `fatal error: stack overflow`. The cap is of the process, so the tests of this
// package must not run in parallel (none does).
func elabCompileCapped(t *testing.T, source string) *testsupport.Compiled {
	t.Helper()
	old := debug.SetMaxStack(elabStackCap)
	defer debug.SetMaxStack(old)
	return testsupport.Compile(t, source)
}

// elabRaised runs f and returns what it panicked with, or nil if it returned.
func elabRaised(f func()) (r any) {
	defer func() { r = recover() }()
	f()
	return nil
}

func TestElaborate(t *testing.T) {
	t.Run("if_becomes_match", func(t *testing.T) {
		c := testsupport.Compile(t, `fn pick(c: Bool, a: Int, b: Int) -> Int { if c { a } else { b } }
fn once(c: Bool) { if c { print(1) } }
fn chain(p: Bool, q: Bool) -> Int { if p { 1 } else if q { 2 } else { 3 } }
`)
		elabWant(t, c, "pick", "match c@0 { true => a@1; false => b@2 }")
		elabWant(t, c, "once", "match c@0 { true => @print(1); false => () }")
		elabWant(t, c, "chain", "match p@0 { true => 1; false => match q@1 { true => 2; false => 3 } }")

		// A Match has the span of the `if` it comes from, and so has the `()`
		// that stands for a missing `else`.
		p := c.Core
		pick := p.Exprs.Get(elabFun(t, c, "pick").Body)
		if got := elabText(c, pick.Span); got != "if c { a } else { b }" {
			t.Errorf("pick: the Match covers %q, want the whole `if`", got)
		}
		once := p.Exprs.Get(elabFun(t, c, "once").Body)
		if got := elabText(c, once.Span); got != "if c { print(1) }" {
			t.Errorf("once: the Match covers %q, want the whole `if`", got)
		}
		if arms := p.ArmsOf(once.Arms); len(arms) == 2 {
			if got := elabText(c, p.Exprs.Get(arms[1].Body).Span); got != "if c { print(1) }" {
				t.Errorf("once: the `()` of the missing else covers %q, want the whole `if`", got)
			}
		}
		chain := p.Exprs.Get(elabFun(t, c, "chain").Body)
		if arms := p.ArmsOf(chain.Arms); len(arms) == 2 {
			if got := elabText(c, p.Exprs.Get(arms[1].Body).Span); got != "if q { 2 } else { 3 }" {
				t.Errorf("chain: the Match of the else-if covers %q, want that `if`", got)
			}
		}
	})

	t.Run("and_short_circuits", func(t *testing.T) {
		c := testsupport.Compile(t, `fn both(a: Bool, b: Bool) -> Bool { a && b }
fn three(a: Bool, b: Bool, c: Bool) -> Bool { a && b && c }
`)
		elabWant(t, c, "both", "match a@0 { true => b@1; false => false }")
		elabWant(t, c, "three", "match match a@0 { true => b@1; false => false } { true => c@2; false => false }")

		// The Match, and the `false` that the operator adds, have the span of
		// the whole expression.
		p := c.Core
		both := p.Exprs.Get(elabFun(t, c, "both").Body)
		if got := elabText(c, both.Span); got != "a && b" {
			t.Errorf("both: the Match covers %q, want \"a && b\"", got)
		}
		if arms := p.ArmsOf(both.Arms); len(arms) == 2 {
			if got := elabText(c, p.Exprs.Get(arms[1].Body).Span); got != "a && b" {
				t.Errorf("both: the added `false` covers %q, want \"a && b\"", got)
			}
		}
		three := p.Exprs.Get(elabFun(t, c, "three").Body)
		if got := elabText(c, three.Span); got != "a && b && c" {
			t.Errorf("three: the outer Match covers %q, want \"a && b && c\"", got)
		}
		if got := elabText(c, p.Exprs.Get(three.Scrutinee).Span); got != "a && b" {
			t.Errorf("three: the inner Match covers %q, want \"a && b\"", got)
		}
	})

	t.Run("or_short_circuits", func(t *testing.T) {
		c := testsupport.Compile(t, `fn either(a: Bool, b: Bool) -> Bool { a || b }
fn three(a: Bool, b: Bool, c: Bool) -> Bool { a || b || c }
fn mixed(a: Bool, b: Bool, c: Bool) -> Bool { a && b || c }
`)
		elabWant(t, c, "either", "match a@0 { true => true; false => b@1 }")
		elabWant(t, c, "three", "match match a@0 { true => true; false => b@1 } { true => true; false => c@2 }")
		elabWant(t, c, "mixed", "match match a@0 { true => b@1; false => false } { true => true; false => c@2 }")

		p := c.Core
		either := p.Exprs.Get(elabFun(t, c, "either").Body)
		if got := elabText(c, either.Span); got != "a || b" {
			t.Errorf("either: the Match covers %q, want \"a || b\"", got)
		}
		if arms := p.ArmsOf(either.Arms); len(arms) == 2 {
			if got := elabText(c, p.Exprs.Get(arms[0].Body).Span); got != "a || b" {
				t.Errorf("either: the added `true` covers %q, want \"a || b\"", got)
			}
		}
	})

	t.Run("eq_on_bool_uses_eq_bool", func(t *testing.T) {
		// The primitive depends on the type of the left operand, whatever
		// the operand is: a variable, a literal or a larger expression.
		for _, row := range []struct{ expr, want string }{
			{"a == b", "eq_bool(a@0, b@1)"},
			{"a != b", "ne_bool(a@0, b@1)"},
			{"x == y", "eq_int(x@2, y@3)"},
			{"x != y", "ne_int(x@2, y@3)"},
			{"true == a", "eq_bool(true, a@0)"},
			{"1 == x", "eq_int(1, x@2)"},
			{"(x < y) == (y < x)", "eq_bool(lt_int(x@2, y@3), lt_int(y@3, x@2))"},
			{"(x + y) != y", "ne_int(add_int(x@2, y@3), y@3)"},
		} {
			c := testsupport.Compile(t, "fn f(a: Bool, b: Bool, x: Int, y: Int) -> Bool { "+row.expr+" }\n")
			f := elabFun(t, c, "f")
			if got := elabShape(c, f.Body); got != row.want {
				t.Errorf("%s: the body is %s, want %s", row.expr, got, row.want)
			}
		}
	})

	t.Run("eq_on_unbound_variable_uses_eq_int", func(t *testing.T) {
		const same = `fn same(x, y) -> Bool { x == y }
fn differ(x, y) -> Bool { x != y }
`
		// leftOperand returns the type, in the table of the type checker, of
		// the left operand of the one `op` that the user's file has.
		leftOperand := func(c *testsupport.Compiled, op syntax.BinOp) (*sema.TypeTable, sema.TypeID) {
			t.Helper()
			f := c.Frontend
			for _, e := range f.Modules[f.User].Exprs.All() {
				if e.Kind == syntax.ExprBinary && e.Op == op {
					return f.Typed.Types, f.Typed.Modules[f.User].ExprTypes[e.Lhs]
				}
			}
			t.Fatalf("the file has no `%s`", op)
			panic("unreachable")
		}

		// Nothing fixes the parameters, so the type of the operand is a
		// variable that nothing has bound, and the comparison is on Int.
		c := testsupport.Compile(t, same)
		elabWant(t, c, "same", "eq_int(x@0, y@1)")
		elabWant(t, c, "differ", "ne_int(x@0, y@1)")
		for _, op := range []syntax.BinOp{syntax.BinEq, syntax.BinNe} {
			tbl, ty := leftOperand(c, op)
			if k := tbl.Node(tbl.Resolve(ty)).Kind; k != sema.TypeVariable {
				t.Errorf("`%s`: the type of the operand is of kind %d, want TypeVariable (the variable that nothing binds)", op, k)
			}
		}

		// A call that fixes the parameters of `same` to Bool binds the same
		// variable: the operand is a variable bound to Bool, which has to be
		// resolved to be seen as Bool. `differ`, which nothing calls, stays on
		// Int.
		c = testsupport.Compile(t, same+"fn main() { let r = same(true, false) }\n")
		elabWant(t, c, "same", "eq_bool(x@0, y@1)")
		elabWant(t, c, "differ", "ne_int(x@0, y@1)")
		tbl, ty := leftOperand(c, syntax.BinEq)
		if k := tbl.Node(ty).Kind; k != sema.TypeVariable {
			t.Errorf("`==` in same: the type of the operand is of kind %d before it is resolved, want TypeVariable", k)
		}
		if got := tbl.Resolve(ty); got != tbl.Builtin(sema.BuiltinBool) {
			t.Errorf("`==` in same: the operand resolves to %s, want Bool", tbl.Show(got))
		}
	})

	t.Run("slots_are_dense", func(t *testing.T) {
		c := testsupport.Compile(t, `fn f(a: Int, b: Int) -> Int { let c = a + b
 let d = c * 2
 d }
fn g(x: Int) -> Int { x }
fn h(c: Bool) -> Int {
  let a = { let b = 1
    b }
  if c { let d = 2
    d } else { a }
}
fn sum() -> Int { { let a = 1
  a } + { let b = 2
  b } }
fn both(c: Bool) -> Bool { { let p = c
  p } && { let q = c
  q } }
fn cond(c: Bool) -> Int { if { let t = c
  t } { let u = 1
  u } else { let v = 2
  v } }
fn call(g) { { let h = g
  h }({ let a = 1
  a }) }
fn stmt(c: Bool) {
  if c { let d = 1
    print(d) }
  print(2)
}
`)
		f := elabFun(t, c, "f")
		if f.FrameSize != 4 {
			t.Errorf("f: FrameSize is %d, want 4", f.FrameSize)
		}
		var params []string
		for _, v := range f.Params {
			params = append(params, elabVar(c, v))
		}
		if got := strings.Join(params, " "); got != "a@0 b@1" {
			t.Errorf("f: the parameters are %q, want \"a@0 b@1\"", got)
		}
		elabWant(t, c, "f", "let c@2 = add_int(a@0, b@1); let d@3 = mul_int(c@2, 2); d@3")

		// Every function counts from 0 again.
		if g := elabFun(t, c, "g"); g.FrameSize != 1 || len(g.Params) != 1 || c.Core.Vars.Get(g.Params[0]).Slot != 0 {
			t.Errorf("g: FrameSize %d and parameters %v, want a frame of 1 and its parameter in slot 0", g.FrameSize, g.Params)
		}
		elabWant(t, c, "g", "x@0")

		// The variables of the blocks inside a function are in its frame, in
		// the order the program names them: a before the b of its initializer,
		// and both before the d of the branch.
		if h := elabFun(t, c, "h"); h.FrameSize != 4 {
			t.Errorf("h: FrameSize is %d, want 4", h.FrameSize)
		}
		elabWant(t, c, "h", "let a@1 = let b@2 = 1; b@2; match c@0 { true => let d@3 = 2; d@3; false => a@1 }")

		// The same order inside one expression: the operands of an operator,
		// the condition and the branches of an `if`, the callee and the
		// arguments of a call, and an expression statement, whose `_` comes
		// before the variables of the block it holds.
		elabWant(t, c, "sum", "add_int(let a@0 = 1; a@0, let b@1 = 2; b@1)")
		elabWant(t, c, "both", "match let p@1 = c@0; p@1 { true => let q@2 = c@0; q@2; false => false }")
		elabWant(t, c, "cond", "match let t@1 = c@0; t@1 { true => let u@2 = 1; u@2; false => let v@3 = 2; v@3 }")
		elabWant(t, c, "call", "let h@1 = g@0; h@1(let a@2 = 1; a@2)")
		elabWant(t, c, "stmt", "let _@1 = match c@0 { true => let d@2 = 1; @print(d@2); false => () }; @print(2)")
	})

	t.Run("prim_span_is_operator", func(t *testing.T) {
		c := testsupport.Compile(t, `fn f(a: Int, b: Int) -> Int {
  let s = a  +   b
  let le = a <= b
  let t = !le
  let n = -s
  (s - 1) * n
}
`)
		prims := map[core.Prim]core.Expr{}
		calls := 0
		for _, e := range c.Core.Exprs.All() {
			if e.Kind == core.PrimCall {
				prims[e.Prim] = e
				calls++
			}
		}
		for _, want := range []struct {
			prim      core.Prim
			text      string
			line, col uint32
		}{
			{core.PrimAddInt, "+", 2, 14},
			{core.PrimLeInt, "<=", 3, 14},
			{core.PrimNotBool, "!", 4, 11},
			{core.PrimNegInt, "-", 5, 11},
			{core.PrimSubInt, "-", 6, 6},
			{core.PrimMulInt, "*", 6, 11},
		} {
			e, ok := prims[want.prim]
			if !ok {
				t.Errorf("%s: no PrimCall of that primitive", want.prim)
				continue
			}
			lc := c.Session.Sources.LineCol(e.Span.File, e.Span.Lo)
			if got := elabText(c, e.Span); got != want.text || lc.Line != want.line || lc.Col != want.col {
				t.Errorf("%s: the span is %q at %d:%d, want %q at %d:%d", want.prim, got, lc.Line, lc.Col, want.text, want.line, want.col)
			}
		}
		if calls != 6 || len(prims) != 6 {
			t.Errorf("the file has %d PrimCall of %d different primitives, want 6 of 6", calls, len(prims))
		}
	})

	t.Run("expr_stmt_becomes_let_underscore", func(t *testing.T) {
		c := testsupport.Compile(t, `fn main() {
  let x = 1 + 2
  print(x)
  print(x)
  print(x)
}
fn semi() { print(1); }
`)
		mainFn := elabFun(t, c, "main")
		elabWant(t, c, "main", "let x@0 = add_int(1, 2); let _@1 = @print(x@0); let _@2 = @print(x@0); @print(x@0)")
		if mainFn.FrameSize != 3 {
			t.Errorf("main: FrameSize is %d, want 3 (the `_` of each statement has a slot)", mainFn.FrameSize)
		}

		// The Let of an expression statement covers the expression, and its
		// variable is another one each time.
		p := c.Core
		var lets []core.Expr
		for id := mainFn.Body; p.Exprs.Get(id).Kind == core.Let; id = p.Exprs.Get(id).Body {
			lets = append(lets, p.Exprs.Get(id))
		}
		if len(lets) != 3 {
			t.Fatalf("main: %d nested Let, want 3", len(lets))
		}
		if got := elabText(c, lets[0].Span); got != "let x = 1 + 2" {
			t.Errorf("main: the first Let covers %q, want \"let x = 1 + 2\"", got)
		}
		if got := elabText(c, p.Vars.Get(lets[0].Var).Span); got != "x" {
			t.Errorf("main: the variable x was bound at %q, want \"x\"", got)
		}
		underscores := lets[1:]
		if underscores[0].Var == underscores[1].Var {
			t.Errorf("main: both expression statements bind the variable %d, want two variables", underscores[0].Var)
		}
		if a, b := p.Vars.Get(underscores[0].Var).Span, p.Vars.Get(underscores[1].Var).Span; a == b {
			t.Errorf("main: both `_` were bound at the span %+v, want the span of each statement", a)
		}
		for i, l := range underscores {
			v := p.Vars.Get(l.Var)
			if name := c.Session.Interner.String(v.Name); name != "_" {
				t.Errorf("main: the variable of expression statement %d is called %q, want \"_\"", i+1, name)
			}
			if got := elabText(c, l.Span); got != "print(x)" {
				t.Errorf("main: the Let of expression statement %d covers %q, want \"print(x)\"", i+1, got)
			}
			if got := elabText(c, v.Span); got != "print(x)" {
				t.Errorf("main: the variable of expression statement %d was bound at %q, want \"print(x)\"", i+1, got)
			}
		}

		// A statement that ends with `;` is still a statement, and the block
		// has no tail: its value is `()`.
		elabWant(t, c, "semi", "let _@0 = @print(1); ()")
		if semi := elabFun(t, c, "semi"); semi.FrameSize != 1 {
			t.Errorf("semi: FrameSize is %d, want 1", semi.FrameSize)
		}
	})

	t.Run("extern_has_no_body", func(t *testing.T) {
		c := testsupport.Compile(t, "fn main() {}\n")
		printFn := elabFun(t, c, "print")
		if printFn.HasBody || printFn.ExternName != "ki_print_int" || printFn.UserModule {
			t.Errorf("print: HasBody %v, ExternName %q, UserModule %v, want false, \"ki_print_int\", false",
				printFn.HasBody, printFn.ExternName, printFn.UserModule)
		}
		if len(printFn.Params) != 1 || printFn.FrameSize != 1 {
			t.Errorf("print: %d parameters and FrameSize %d, want 1 and 1 (the machine binds the argument to it)", len(printFn.Params), printFn.FrameSize)
		} else if got := elabVar(c, printFn.Params[0]); got != "x@0" {
			t.Errorf("print: the parameter is %s, want x@0", got)
		}
		mainFn := elabFun(t, c, "main")
		if !mainFn.HasBody || mainFn.ExternName != "" || !mainFn.UserModule {
			t.Errorf("main: HasBody %v, ExternName %q, UserModule %v, want true, \"\", true", mainFn.HasBody, mainFn.ExternName, mainFn.UserModule)
		}
	})

	t.Run("operators_map_to_prims", func(t *testing.T) {
		// Each operator has its primitive, and the operands keep their order.
		// (== and != are in eq_on_bool_uses_eq_bool.)
		for _, row := range []struct{ expr, ret, want string }{
			{"x + y", "Int", "add_int(x@0, y@1)"},
			{"x - y", "Int", "sub_int(x@0, y@1)"},
			{"x * y", "Int", "mul_int(x@0, y@1)"},
			{"x / y", "Int", "div_int(x@0, y@1)"},
			{"x % y", "Int", "rem_int(x@0, y@1)"},
			{"-x", "Int", "neg_int(x@0)"},
			{"x < y", "Bool", "lt_int(x@0, y@1)"},
			{"x <= y", "Bool", "le_int(x@0, y@1)"},
			{"x > y", "Bool", "gt_int(x@0, y@1)"},
			{"x >= y", "Bool", "ge_int(x@0, y@1)"},
			{"!b", "Bool", "not_bool(b@2)"},
			{"x + y * x", "Int", "add_int(x@0, mul_int(y@1, x@0))"},
			{"-x - -y", "Int", "sub_int(neg_int(x@0), neg_int(y@1))"},
		} {
			c := testsupport.Compile(t, "fn f(x: Int, y: Int, b: Bool) -> "+row.ret+" { "+row.expr+" }\n")
			if got := elabShape(c, elabFun(t, c, "f").Body); got != row.want {
				t.Errorf("%s: the body is %s, want %s", row.expr, got, row.want)
			}
		}
	})

	t.Run("block_is_a_let_chain", func(t *testing.T) {
		// The last function has 100 000 statements, and the source is compiled
		// with a stack of 1 MiB (see elabCompileCapped): a `block` that
		// recursed over the statements would overflow it.
		var long strings.Builder
		long.WriteString("fn long() -> Int {\n")
		const lets = 100_000
		for i := 0; i < lets; i++ {
			fmt.Fprintf(&long, "  let v%d = %d\n", i, i)
		}
		long.WriteString("  v0\n}\n")

		c := elabCompileCapped(t, `fn empty() {}
fn unit() { () }
fn only() -> Int { 7 }
fn noTail() { let a = 1 }
fn order() -> Int {
  let a = 1
  let b = 2
  let c = 3
  a + b + c
}
fn shadow() -> Int {
  let x = 1
  let x = x + 1
  x
}
`+long.String())
		elabWant(t, c, "empty", "()")
		elabWant(t, c, "unit", "()")
		elabWant(t, c, "only", "7")
		elabWant(t, c, "noTail", "let a@0 = 1; ()")
		elabWant(t, c, "order", "let a@0 = 1; let b@1 = 2; let c@2 = 3; add_int(add_int(a@0, b@1), c@2)")

		// An empty block is the `()` that covers its braces.
		p := c.Core
		if got := elabText(c, p.Exprs.Get(elabFun(t, c, "empty").Body).Span); got != "{}" {
			t.Errorf("empty: the `()` covers %q, want \"{}\"", got)
		}
		// Each `let` is its own variable, also when the name is the same: the
		// initializer of the second reads the first, and the tail the second.
		elabWant(t, c, "shadow", "let x@0 = 1; let x@1 = add_int(x@0, 1); x@1")
		if shadow := elabFun(t, c, "shadow"); shadow.FrameSize != 2 {
			t.Errorf("shadow: FrameSize is %d, want 2", shadow.FrameSize)
		}

		// The body of the Let of statement k is the Let of statement k+1, and
		// the last one holds the tail, however many statements there are. The
		// chain is walked with a loop: elabShape recurses and would overflow
		// the stack here.
		f := elabFun(t, c, "long")
		if f.FrameSize != lets {
			t.Errorf("long: FrameSize is %d, want %d", f.FrameSize, lets)
		}
		id := f.Body
		for i := 0; i < lets; i++ {
			e := p.Exprs.Get(id)
			if e.Kind != core.Let {
				t.Fatalf("long: statement %d is a %v, want a Let", i, e.Kind)
			}
			if v := p.Vars.Get(e.Var); v.Slot != uint32(i) || c.Session.Interner.String(v.Name) != "v"+strconv.Itoa(i) {
				t.Fatalf("long: the Let of statement %d binds %s, want v%d@%d", i, elabVar(c, e.Var), i, i)
			}
			if init := p.Exprs.Get(e.Init); init.Kind != core.LitInt || init.Int != int64(i) {
				t.Fatalf("long: the initializer of statement %d is %s, want %d", i, elabShape(c, e.Init), i)
			}
			id = e.Body
		}
		if got := elabShape(c, id); got != "v0@0" {
			t.Errorf("long: the tail of the chain is %s, want v0@0", got)
		}
	})

	t.Run("calls_use_funref_or_varref", func(t *testing.T) {
		c := testsupport.Compile(t, `fn apply(g, x) { g(x) }
fn add1(n: Int) -> Int { n + 1 }
fn first() { later() }
fn later() {}
fn main() {
  apply(print, 1)
  let r = add1(2)
  print(r)
}
`)
		// A call to a function of the program names it with a FunRef, even
		// when the function is defined further down, and a call to a variable
		// reads the variable. A function used as a value is a FunRef too.
		elabWant(t, c, "apply", "g@0(x@1)")
		elabWant(t, c, "first", "@later()")
		elabWant(t, c, "main", "let _@0 = @apply(@print, 1); let r@1 = @add1(2); @print(r@1)")

		// The FunRef holds the id of the function it names.
		p := c.Core
		first := p.Exprs.Get(elabFun(t, c, "first").Body)
		callee := p.Exprs.Get(first.Callee)
		if callee.Kind != core.FunRef || c.Session.Interner.String(p.Funs.Get(callee.Fun).Name) != "later" {
			t.Errorf("first: the callee is %+v, want a FunRef to later", callee)
		}
		if got := elabText(c, first.Span); got != "later()" {
			t.Errorf("first: the Call covers %q, want \"later()\"", got)
		}
	})

	t.Run("node_spans_cover_their_syntax", func(t *testing.T) {
		c := testsupport.Compile(t, `fn f(g, x: Int) { g(x, 12, true, ()) }
fn h(k) { k(print) }
`)
		// A literal, a name and a call cover what the program wrote for them.
		p := c.Core
		call := p.Exprs.Get(elabFun(t, c, "f").Body)
		if got := elabText(c, call.Span); call.Kind != core.Call || got != "g(x, 12, true, ())" {
			t.Errorf("f: the body is a %v covering %q, want the Call \"g(x, 12, true, ())\"", call.Kind, got)
		}
		callee := p.Exprs.Get(call.Callee)
		if got := elabText(c, callee.Span); callee.Kind != core.VarRef || got != "g" {
			t.Errorf("f: the callee is a %v covering %q, want the VarRef \"g\"", callee.Kind, got)
		}
		args := p.Args(call.Args)
		if len(args) != 4 {
			t.Fatalf("f: the call has %d arguments, want 4", len(args))
		}
		for i, want := range []struct {
			kind core.ExprKind
			text string
		}{{core.VarRef, "x"}, {core.LitInt, "12"}, {core.LitBool, "true"}, {core.LitUnit, "()"}} {
			arg := p.Exprs.Get(args[i])
			if got := elabText(c, arg.Span); arg.Kind != want.kind || got != want.text {
				t.Errorf("f: argument %d is a %v covering %q, want the %v %q", i, arg.Kind, got, want.kind, want.text)
			}
		}
		fn := p.Exprs.Get(p.Args(p.Exprs.Get(elabFun(t, c, "h").Body).Args)[0])
		if got := elabText(c, fn.Span); fn.Kind != core.FunRef || got != "print" {
			t.Errorf("h: the argument is a %v covering %q, want the FunRef \"print\"", fn.Kind, got)
		}
	})

	t.Run("defs_become_funs", func(t *testing.T) {
		c := testsupport.Compile(t, `fn first() -> Int { second() }
fn second() -> Int { 2 }
fn main() {}
fn last() {}
`)
		// A Fun for each definition, with the id of its Def: the prelude's
		// first, then the file's in source order.
		rp, p := c.Frontend.Resolved, c.Core
		if p.Funs.Len() != rp.Defs.Len() || p.Funs.Len() != 5 {
			t.Fatalf("%d functions for %d definitions, want 5 of each", p.Funs.Len(), rp.Defs.Len())
		}
		for id, def := range rp.Defs.All() {
			f := p.Funs.Get(core.FunID(id))
			name := c.Session.Interner.String(def.Name)
			if f.Name != def.Name {
				t.Errorf("function %d is called %q, want the name of its definition, %q", id, c.Session.Interner.String(f.Name), name)
			}
			if got := elabText(c, f.Span); f.Span != def.Span || got != name {
				t.Errorf("%s: Span covers %q, want the name where it is defined", name, got)
			}
			if want := def.Module != 0; f.UserModule != want {
				t.Errorf("%s: UserModule is %v, want %v", name, f.UserModule, want)
			}
		}
		if f := p.Funs.Get(0); c.Session.Interner.String(f.Name) != "print" || f.UserModule {
			t.Errorf("function 0 is %q, want the prelude's print", c.Session.Interner.String(f.Name))
		}
		// Main is the function main, which is not the last one.
		if !p.HasMain || c.Session.Interner.String(p.Funs.Get(p.Main).Name) != "main" {
			t.Errorf("HasMain %v and Main %d, want the function main", p.HasMain, p.Main)
		}
		if len(elabFun(t, c, "first").Params) != 0 {
			t.Errorf("first has %d parameters, want 0", len(elabFun(t, c, "first").Params))
		}
		elabWant(t, c, "first", "@second()")

		// A file without main.
		c = testsupport.Compile(t, "fn f() {}\n")
		if c.Core.HasMain {
			t.Errorf("HasMain is true for a file without main (Main %d)", c.Core.Main)
		}
	})

	t.Run("error_nodes_panic", func(t *testing.T) {
		// Elaborate needs a program without errors. What it cannot translate it
		// does not guess: it panics, with the text that the report of the ICE
		// shows. (It does not look for the errors that leave a node it can
		// translate.)
		for _, row := range []struct{ name, src, want string }{
			{"expression", "fn main() { 1 + }\n", "core: cannot elaborate an expression that could not be read"},
			{"statement", "fn main() { let = 1 }\n", "core: cannot elaborate a statement that could not be read"},
			{"name", "fn main() { y }\n", "core: cannot elaborate a name that stands for nothing"},
			{"equality", "fn main() { () == () }\n", "core: cannot elaborate `==` on a value of type `()`"},
			{"inequality", "fn main() { let f = main\n f != f }\n", "core: cannot elaborate `!=` on a value of type `() -> ()`"},
		} {
			p := testsupport.Analyze(t, row.src)
			if len(testsupport.ErrorCodes(p)) == 0 {
				t.Errorf("%s: the front end found no errors in %q", row.name, row.src)
				continue
			}
			f := p.Frontend
			r := elabRaised(func() { core.Elaborate(f.Modules, f.Resolved, f.Typed, p.Session.Interner) })
			if r != row.want {
				t.Errorf("%s: Elaborate panicked with %#v, want %q", row.name, r, row.want)
			}
		}
	})

	t.Run("lower_runs_under_the_elaborate_pass", func(t *testing.T) {
		p := testsupport.Analyze(t, "fn main() {}\n")
		s, f := p.Session, p.Frontend
		prog := s.Lower(f)
		if prog == nil || !prog.HasMain || prog.Funs.Len() != 2 {
			t.Fatalf("Lower returned %+v, want the Program of print and main", prog)
		}

		// The debug hook of the pass: the panic comes before the elaboration.
		s.DebugPanicIn = "elaborate"
		r := elabRaised(func() { s.Lower(f) })
		if ice, ok := r.(*driver.ICE); !ok || ice.Pass != "elaborate" || ice.Value != "debug panic requested" {
			t.Errorf("Lower with the hook in elaborate panicked with %#v, want the *driver.ICE of pass \"elaborate\"", r)
		}

		// And a panic of Elaborate itself is an ICE of that pass, not a bare
		// panic that nothing reports.
		bad := testsupport.Analyze(t, "fn main() { y }\n")
		r = elabRaised(func() { bad.Session.Lower(bad.Frontend) })
		const want = "internal compiler error: elaborate: core: cannot elaborate a name that stands for nothing"
		if ice, ok := r.(*driver.ICE); !ok || ice.Error() != want {
			t.Errorf("Lower of a program with errors panicked with %#v, want the *driver.ICE %q", r, want)
		}
	})
}
