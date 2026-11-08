package core_test

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/testsupport"
)

// lintProg is a Program built by hand for the tests of Lint: the Builder makes
// the expressions, and the methods below add the variables and the functions
// around them. Each variable is called v<id> and each function f<id>, after the
// place it takes in the Program, so a violation names exactly one of them and
// a test says which one it is about by its id (see lintFun and lintVar).
type lintProg struct {
	*core.Builder
	in *base.Interner // spells the names of the violations
}

func newLintProg() *lintProg {
	b, _ := newBuilder()
	return &lintProg{Builder: b, in: base.NewInterner()}
}

// variable adds a variable with the given slot and returns its id.
func (l *lintProg) variable(slot uint32) core.VarID {
	name := l.in.InternString("v" + strconv.Itoa(l.P.Vars.Len()))
	return l.P.Vars.Push(core.Var{Name: name, Rep: core.RepVal, Slot: slot, Span: span(0, 1)})
}

// push adds the function f, called f<id>, and returns its id.
func (l *lintProg) push(f core.Fun) core.FunID {
	f.Name = l.in.InternString("f" + strconv.Itoa(l.P.Funs.Len()))
	return l.P.Funs.Push(f)
}

// fn adds an ordinary function: it has a body and a frame of frame slots.
func (l *lintProg) fn(frame uint32, params []core.VarID, body core.ExprID) core.FunID {
	return l.push(core.Fun{Params: params, Body: body, HasBody: true, FrameSize: frame, Span: span(0, 1)})
}

// extern adds a function that the runtime provides: no body, the extern name,
// and a frame that holds its parameters.
func (l *lintProg) extern(name string, params []core.VarID) core.FunID {
	return l.push(core.Fun{Params: params, ExternName: name, FrameSize: uint32(len(params)), Span: span(0, 1)})
}

// lintFun is how a violation of the function f starts: `fun f<id>`, without the
// colon.
func lintFun(f core.FunID) string { return "fun f" + strconv.Itoa(int(f)) }

// lintVar is how a violation names the variable v: its name, v<id>, between
// backquotes.
func lintVar(v core.VarID) string { return "`v" + strconv.Itoa(int(v)) + "`" }

// lintCheck fails the test unless Lint of p returns exactly want, in that order.
func lintCheck(t *testing.T, what string, p *core.Program, in *base.Interner, want ...string) {
	t.Helper()
	if got := core.Lint(p, in); !slices.Equal(got, want) {
		t.Errorf("%s: Lint returned %q, want %q", what, got, want)
	}
}

func TestLint(t *testing.T) {
	t.Run("accepts_elaborated_fib", func(t *testing.T) {
		c := testsupport.Compile(t, `fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}
`)
		if got := core.Lint(c.Core, c.Session.Interner); len(got) != 0 {
			t.Errorf("Lint of the elaborated fib returned %q, want nothing", got)
		}
		// The five functions of the prelude are in the Program, with no body, and
		// Lint has read them too: this is what the check above ran over.
		if n := c.Core.Funs.Len(); n != 6 {
			t.Fatalf("the Program has %d functions, want 6 (the prelude's five and fib)", n)
		}
		in := c.Session.Interner
		if f := c.Core.Funs.Get(0); in.String(f.Name) != "print" || f.HasBody {
			t.Errorf("fun 0 is %q (has a body: %v), want print with no body", in.String(f.Name), f.HasBody)
		}
		if f := c.Core.Funs.Get(5); in.String(f.Name) != "fib" || !f.HasBody {
			t.Errorf("fun 5 is %q (has a body: %v), want fib with a body", in.String(f.Name), f.HasBody)
		}
	})

	t.Run("accepts_every_construct", func(t *testing.T) {
		for _, row := range []struct{ name, source string }{
			{"negation and not", "fn f(a: Int, b: Bool) -> Bool { !b && -a < 0 }\n"},
			{"floats", "fn f(a: Float, b: Float) -> Bool { -a * 2.0 + b / 0.5 - 1e21 >= a || a == b || a != b }\n"},
			{"and, or and equality", "fn f(a: Bool, b: Bool, x: Int) -> Bool { a && b || !a || x == 1 || a != b }\n"},
			{"shadowing and statements", "fn f(a: Int) -> Int {\n  let x = 1\n  let x = x + a\n  print(x)\n  print(x)\n  x\n}\n"},
			{"a function used as a value", "fn apply(g, x) { g(x) }\nfn main() { apply(print, 1) }\n"},
			{"calls with no arguments", "fn one() -> Int { 1 }\nfn main() { print(one() + one()) }\n"},
			{"if without else", "fn f(c: Bool) { if c { print(1) } }\n"},
			{"nested blocks", "fn f(c: Bool) -> Int {\n  let a = if c { let b = 1\n b } else { 2 }\n  a\n}\n"},
		} {
			c := testsupport.Compile(t, row.source)
			if got := core.Lint(c.Core, c.Session.Interner); len(got) != 0 {
				t.Errorf("%s: Lint returned %q, want nothing", row.name, got)
			}
		}
	})

	t.Run("accepts_hand_built_shapes", func(t *testing.T) {
		for _, row := range []struct {
			name  string
			build func(l *lintProg) core.ExprID
		}{
			// A call through a variable has no arity to check: Lint does not
			// know what the variable holds.
			{"call through a variable", func(l *lintProg) core.ExprID {
				f := l.variable(0)
				call := l.Call(span(0, 1), l.VarRef(span(0, 1), f), []core.ExprID{l.LitInt(span(0, 1), 1), l.LitInt(span(0, 1), 2), l.LitInt(span(0, 1), 3)})
				return l.Let(span(0, 1), f, l.LitInt(span(0, 1), 0), call)
			}},
			{"unary primitive with one argument", func(l *lintProg) core.ExprID {
				return l.Prim(span(0, 1), core.PrimNegInt, []core.ExprID{l.LitInt(span(0, 1), 1)})
			}},
			{"function used as a value", func(l *lintProg) core.ExprID {
				return l.FunRef(span(0, 1), 0)
			}},
			// Any arms at all are valid, whatever they cover (see
			// match_coverage_is_not_checked).
			{"true and false", func(l *lintProg) core.ExprID { return lintMatch(l, core.PatTrue, core.PatFalse) }},
			{"false and true", func(l *lintProg) core.ExprID { return lintMatch(l, core.PatFalse, core.PatTrue) }},
			{"wildcard alone", func(l *lintProg) core.ExprID { return lintMatch(l, core.PatWildcard) }},
			{"true and wildcard", func(l *lintProg) core.ExprID { return lintMatch(l, core.PatTrue, core.PatWildcard) }},
			{"wildcard and false", func(l *lintProg) core.ExprID { return lintMatch(l, core.PatWildcard, core.PatFalse) }},
			{"true, false and wildcard", func(l *lintProg) core.ExprID {
				return lintMatch(l, core.PatTrue, core.PatFalse, core.PatWildcard)
			}},
			// `let v = 1; add_int(let v = 2; v, v)`: one variable bound by two
			// Lets, which Core promises not to do and Lint does not check. The
			// scope counts the binders of a variable, so the inner Let ending
			// leaves it in scope for the second argument, which the outer Let
			// still holds. A set would take it out and report it as unbound.
			{"a variable bound by two nested Lets", func(l *lintProg) core.ExprID {
				v := l.variable(0)
				inner := l.Let(span(0, 1), v, l.LitInt(span(0, 1), 2), l.VarRef(span(0, 1), v))
				body := l.Prim(span(0, 1), core.PrimAddInt, []core.ExprID{inner, l.VarRef(span(0, 1), v)})
				return l.Let(span(0, 1), v, l.LitInt(span(0, 1), 1), body)
			}},
		} {
			l := newLintProg()
			l.extern("ki_print_int", []core.VarID{l.variable(0)}) // f0, which the FunRef row names
			l.fn(1, nil, row.build(l))
			lintCheck(t, row.name, l.P, l.in)
		}
	})

	t.Run("rejects_unbound_var", func(t *testing.T) {
		for _, row := range []struct {
			name string
			// build returns the body of the function under test. u is a
			// variable that nothing binds, x one that a Let can bind, and g
			// a function of one parameter. It also says which variable the
			// one violation names.
			build func(l *lintProg, u, x core.VarID, g core.FunID) (body core.ExprID, unbound core.VarID)
		}{
			{"the body", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				return l.VarRef(span(0, 1), u), u
			}},
			{"a callee", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				return l.Call(span(0, 1), l.VarRef(span(0, 1), u), []core.ExprID{l.LitInt(span(0, 1), 1)}), u
			}},
			{"an argument", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				return l.Call(span(0, 1), l.FunRef(span(0, 1), g), []core.ExprID{l.VarRef(span(0, 1), u)}), u
			}},
			{"the first primitive argument", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				return l.Prim(span(0, 1), core.PrimAddInt, []core.ExprID{l.VarRef(span(0, 1), u), l.LitInt(span(0, 1), 1)}), u
			}},
			{"the second primitive argument", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				return l.Prim(span(0, 1), core.PrimAddInt, []core.ExprID{l.LitInt(span(0, 1), 1), l.VarRef(span(0, 1), u)}), u
			}},
			{"a scrutinee", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				arms := []core.Arm{armOf(l, core.PatTrue, l.LitInt(span(0, 1), 1)), armOf(l, core.PatFalse, l.LitInt(span(0, 1), 2))}
				return l.Match(span(0, 1), l.VarRef(span(0, 1), u), arms), u
			}},
			{"the true arm", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				arms := []core.Arm{armOf(l, core.PatTrue, l.VarRef(span(0, 1), u)), armOf(l, core.PatFalse, l.LitInt(span(0, 1), 2))}
				return l.Match(span(0, 1), l.LitBool(span(0, 1), true), arms), u
			}},
			{"the false arm", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				arms := []core.Arm{armOf(l, core.PatTrue, l.LitInt(span(0, 1), 1)), armOf(l, core.PatFalse, l.VarRef(span(0, 1), u))}
				return l.Match(span(0, 1), l.LitBool(span(0, 1), true), arms), u
			}},
			{"the init of a Let", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				return l.Let(span(0, 1), x, l.VarRef(span(0, 1), u), l.LitInt(span(0, 1), 1)), u
			}},
			{"the body of a Let", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				return l.Let(span(0, 1), x, l.LitInt(span(0, 1), 1), l.VarRef(span(0, 1), u)), u
			}},
			// `let x = x; x`: the variable of a Let is bound in its body, not
			// in the expression that gives it its value.
			{"the variable a Let defines, in its own init", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				return l.Let(span(0, 1), x, l.VarRef(span(0, 1), x), l.VarRef(span(0, 1), x)), x
			}},
			// The Let ends with its body: the second argument is outside it.
			{"a Let variable, in the next argument", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				scoped := l.Let(span(0, 1), x, l.LitInt(span(0, 1), 1), l.VarRef(span(0, 1), x))
				return l.Prim(span(0, 1), core.PrimAddInt, []core.ExprID{scoped, l.VarRef(span(0, 1), x)}), x
			}},
			// Nor does a Let in the init of another one reach its body.
			{"a Let variable, in the body of the Let that holds it in its init", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				scoped := l.Let(span(0, 1), x, l.LitInt(span(0, 1), 1), l.VarRef(span(0, 1), x))
				y := l.variable(1)
				return l.Let(span(0, 1), y, scoped, l.VarRef(span(0, 1), x)), x
			}},
			// An arm is not in the scope of the other one.
			{"a Let variable, in the next arm", func(l *lintProg, u, x core.VarID, g core.FunID) (core.ExprID, core.VarID) {
				first := l.Let(span(0, 1), x, l.LitInt(span(0, 1), 1), l.VarRef(span(0, 1), x))
				arms := []core.Arm{armOf(l, core.PatTrue, first), armOf(l, core.PatFalse, l.VarRef(span(0, 1), x))}
				return l.Match(span(0, 1), l.LitBool(span(0, 1), true), arms), x
			}},
		} {
			l := newLintProg()
			gParam := l.variable(0) // v0
			g := l.fn(1, []core.VarID{gParam}, l.VarRef(span(0, 1), gParam))
			u, x := l.variable(0), l.variable(1) // v1 and v2
			body, unbound := row.build(l, u, x, g)
			f := l.fn(2, nil, body)
			lintCheck(t, row.name, l.P, l.in, lintFun(f)+": unbound variable "+lintVar(unbound))
		}

		// A parameter is bound in the function that has it and in no other.
		l := newLintProg()
		p := l.variable(0)
		l.fn(1, []core.VarID{p}, l.VarRef(span(0, 1), p))
		l.fn(1, nil, l.VarRef(span(0, 1), p))
		lintCheck(t, "a parameter of another function", l.P, l.in, "fun f1: unbound variable `v0`")

		// The one violation says unbound, as the report of an ICE will read.
		l = newLintProg()
		l.fn(1, nil, l.VarRef(span(0, 1), l.variable(0)))
		if got := core.Lint(l.P, l.in); len(got) != 1 || !strings.Contains(got[0], "unbound") {
			t.Errorf("Lint of an unbound variable returned %q, want one violation that contains \"unbound\"", got)
		}

		// The names are the ones of the source, so that whoever reads the report
		// of an ICE finds the function and the variable in their file: `n` is
		// taken out of the parameters of `f`, and its body reads it unbound.
		c := testsupport.Compile(t, "fn f(n: Int) -> Int { n }\n")
		c.Core.Funs.Ptr(testsupport.FunNamed(t, c, "f")).Params = nil
		lintCheck(t, "a source with its parameter taken away", c.Core, c.Session.Interner, "fun f: unbound variable `n`")
	})

	t.Run("rejects_bad_arity", func(t *testing.T) {
		lit := func(l *lintProg, n int) []core.ExprID {
			args := make([]core.ExprID, n)
			for i := range args {
				args[i] = l.LitInt(span(0, 1), int64(i))
			}
			return args
		}
		for _, row := range []struct {
			name  string
			build func(l *lintProg, g2, g0 core.FunID) core.ExprID
			want  string
		}{
			{"a call with one argument too few", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Call(span(0, 1), l.FunRef(span(0, 1), g2), lit(l, 1))
			}, "call of fun `f0` with 1 argument(s), but it takes 2"},
			{"a call with one argument too many", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Call(span(0, 1), l.FunRef(span(0, 1), g2), lit(l, 3))
			}, "call of fun `f0` with 3 argument(s), but it takes 2"},
			{"a call with no arguments to a function of two", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Call(span(0, 1), l.FunRef(span(0, 1), g2), nil)
			}, "call of fun `f0` with 0 argument(s), but it takes 2"},
			{"a call with an argument to a function of none", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Call(span(0, 1), l.FunRef(span(0, 1), g0), lit(l, 1))
			}, "call of fun `f1` with 1 argument(s), but it takes 0"},
			{"a binary primitive with one argument", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Prim(span(0, 1), core.PrimAddInt, lit(l, 1))
			}, "primitive add_int with 1 argument(s), but it takes 2"},
			{"a binary primitive with three arguments", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Prim(span(0, 1), core.PrimLtInt, lit(l, 3))
			}, "primitive lt_int with 3 argument(s), but it takes 2"},
			{"a binary primitive with no arguments", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Prim(span(0, 1), core.PrimEqBool, nil)
			}, "primitive eq_bool with 0 argument(s), but it takes 2"},
			{"a unary primitive with two arguments", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Prim(span(0, 1), core.PrimNegInt, lit(l, 2))
			}, "primitive neg_int with 2 argument(s), but it takes 1"},
			{"a unary float primitive with two arguments", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Prim(span(0, 1), core.PrimNegFloat, lit(l, 2))
			}, "primitive neg_float with 2 argument(s), but it takes 1"},
			{"a binary float primitive with one argument", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Prim(span(0, 1), core.PrimAddFloat, lit(l, 1))
			}, "primitive add_float with 1 argument(s), but it takes 2"},
			{"a unary primitive with no arguments", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				return l.Prim(span(0, 1), core.PrimNotBool, nil)
			}, "primitive not_bool with 0 argument(s), but it takes 1"},
			// The call in the init of a Let: the walk reaches it.
			{"a call in the init of a Let", func(l *lintProg, g2, g0 core.FunID) core.ExprID {
				x := l.variable(0)
				return l.Let(span(0, 1), x, l.Call(span(0, 1), l.FunRef(span(0, 1), g2), lit(l, 1)), l.LitUnit(span(0, 1)))
			}, "call of fun `f0` with 1 argument(s), but it takes 2"},
		} {
			l := newLintProg()
			g2 := l.extern("ki_two", []core.VarID{l.variable(0), l.variable(1)}) // f0
			g0 := l.fn(0, nil, l.LitUnit(span(0, 1)))                            // f1
			f := l.fn(1, nil, row.build(l, g2, g0))                              // f2
			lintCheck(t, row.name, l.P, l.in, lintFun(f)+": "+row.want)
		}
	})

	t.Run("constructors", func(t *testing.T) {
		// A Con has as many arguments as its constructor has fields, whatever
		// they are, and Lint reads inside them: the constructor is named in the
		// violation as the source names it, and a wrong arity inside the argument
		// of another Con or of a call is found as well.
		lit := func(l *lintProg, n int) []core.ExprID {
			args := make([]core.ExprID, n)
			for i := range args {
				args[i] = l.LitInt(span(0, 1), int64(i))
			}
			return args
		}
		for _, row := range []struct {
			name  string
			build func(l *lintProg, none, pair core.CtorID) core.ExprID
			want  string // the violation, or empty when the Core is valid
		}{
			{"no arguments for no fields", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Con(span(0, 1), none, nil)
			}, ""},
			{"two arguments for two fields", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Con(span(0, 1), pair, lit(l, 2))
			}, ""},
			{"nested and correct", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Con(span(0, 1), pair, []core.ExprID{l.Con(span(0, 1), none, nil), l.Con(span(0, 1), none, nil)})
			}, ""},
			{"one argument too few", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Con(span(0, 1), pair, lit(l, 1))
			}, "constructor `Pair` with 1 argument(s), but it takes 2"},
			{"one argument too many", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Con(span(0, 1), pair, lit(l, 3))
			}, "constructor `Pair` with 3 argument(s), but it takes 2"},
			{"an argument for no field", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Con(span(0, 1), none, lit(l, 1))
			}, "constructor `None` with 1 argument(s), but it takes 0"},
			{"no arguments for two fields", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Con(span(0, 1), pair, nil)
			}, "constructor `Pair` with 0 argument(s), but it takes 2"},
			{"in an argument of a Con", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Con(span(0, 1), pair, []core.ExprID{l.LitInt(span(0, 1), 1), l.Con(span(0, 1), pair, lit(l, 1))})
			}, "constructor `Pair` with 1 argument(s), but it takes 2"},
			{"in the argument of a call", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Call(span(0, 1), l.FunRef(span(0, 1), 0), []core.ExprID{l.Con(span(0, 1), none, lit(l, 2))})
			}, "constructor `None` with 2 argument(s), but it takes 0"},
			{"in the init of a Let", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				x := l.variable(0)
				return l.Let(span(0, 1), x, l.Con(span(0, 1), pair, lit(l, 3)), l.LitUnit(span(0, 1)))
			}, "constructor `Pair` with 3 argument(s), but it takes 2"},
			{"an unbound variable in an argument", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Con(span(0, 1), pair, []core.ExprID{l.VarRef(span(0, 1), l.variable(0)), l.LitInt(span(0, 1), 1)})
			}, "unbound variable `v1`"},
			{"a variable of the Fun in an argument", func(l *lintProg, none, pair core.CtorID) core.ExprID {
				return l.Let(span(0, 1), l.variable(0), l.LitInt(span(0, 1), 1),
					l.Con(span(0, 1), pair, []core.ExprID{l.VarRef(span(0, 1), core.VarID(1)), l.LitInt(span(0, 1), 1)}))
			}, ""},
		} {
			l := newLintProg()
			l.extern("ki_print_int", []core.VarID{l.variable(0)}) // f0, which the call row names
			none := l.P.Ctors.Push(core.Ctor{Name: l.in.InternString("None"), Tag: 0, Arity: 0})
			pair := l.P.Ctors.Push(core.Ctor{Name: l.in.InternString("Pair"), Tag: 1, Arity: 2})
			f := l.fn(2, nil, row.build(l, none, pair))
			if row.want == "" {
				lintCheck(t, row.name, l.P, l.in)
			} else {
				lintCheck(t, row.name, l.P, l.in, lintFun(f)+": "+row.want)
			}
		}
	})

	t.Run("patterns_bind_in_their_arm", func(t *testing.T) {
		// The variables that the pattern of an arm binds are in scope in its guard
		// and its body, and nowhere else: not in the scrutinee, not in the other
		// arms, not after the Match. Each row is the body of a function that has the
		// variable x, which some patterns bind, and the one that nothing binds is
		// left out: the violations are all `unbound variable` of x, or none.
		for _, row := range []struct {
			name  string
			build func(l *lintProg, x core.VarID) core.ExprID
			want  int // how many reads of x are unbound
		}{
			{"the guard reads it", func(l *lintProg, x core.VarID) core.ExprID {
				return lintArms(l, lintScrut(l), lintArm{pat: lintBind(l, x), guard: l.VarRef(span(0, 1), x), hasGuard: true, body: l.LitInt(span(0, 1), 1)})
			}, 0},
			{"the body reads it", func(l *lintProg, x core.VarID) core.ExprID {
				return lintArms(l, lintScrut(l), lintArm{pat: lintBind(l, x), body: l.VarRef(span(0, 1), x)})
			}, 0},
			{"a variable deep in a pattern", func(l *lintProg, x core.VarID) core.ExprID {
				deep := lintCon(l, "Pair", lintWild(l), lintCon(l, "Pair", lintWild(l), lintBind(l, x)))
				return lintArms(l, lintScrut(l), lintArm{pat: deep, body: l.VarRef(span(0, 1), x)})
			}, 0},
			{"each alternative of an or-pattern binds it", func(l *lintProg, x core.VarID) core.ExprID {
				or := lintOr(l, lintCon(l, "Pair", lintBind(l, x), lintWild(l)), lintCon(l, "Pair", lintWild(l), lintBind(l, x)))
				return lintArms(l, lintScrut(l), lintArm{pat: or, guard: l.VarRef(span(0, 1), x), hasGuard: true, body: l.VarRef(span(0, 1), x)})
			}, 0},
			{"the scrutinee is outside the arms", func(l *lintProg, x core.VarID) core.ExprID {
				return l.Match(span(0, 1), l.VarRef(span(0, 1), x), []core.Arm{{Pat: lintBind(l, x), Body: l.LitInt(span(0, 1), 1)}})
			}, 1},
			{"the next arm is outside the arm before it", func(l *lintProg, x core.VarID) core.ExprID {
				return lintArms(l, lintScrut(l),
					lintArm{pat: lintBind(l, x), body: l.LitInt(span(0, 1), 1)},
					lintArm{pat: lintWild(l), body: l.VarRef(span(0, 1), x)})
			}, 1},
			{"the guard of the next arm is outside it too", func(l *lintProg, x core.VarID) core.ExprID {
				return lintArms(l, lintScrut(l),
					lintArm{pat: lintBind(l, x), body: l.LitInt(span(0, 1), 1)},
					lintArm{pat: lintWild(l), guard: l.VarRef(span(0, 1), x), hasGuard: true, body: l.LitInt(span(0, 1), 2)})
			}, 1},
			{"the arm before it is outside the arm that binds", func(l *lintProg, x core.VarID) core.ExprID {
				return lintArms(l, lintScrut(l),
					lintArm{pat: lintWild(l), body: l.VarRef(span(0, 1), x)},
					lintArm{pat: lintBind(l, x), body: l.LitInt(span(0, 1), 1)})
			}, 1},
			{"the variable leaves with the arm", func(l *lintProg, x core.VarID) core.ExprID {
				m := lintArms(l, lintScrut(l), lintArm{pat: lintBind(l, x), body: l.VarRef(span(0, 1), x)})
				return l.Prim(span(0, 1), core.PrimAddInt, []core.ExprID{m, l.VarRef(span(0, 1), x)})
			}, 1},
			// Two alternatives bind x twice: it leaves the scope when both are done,
			// not when one is.
			{"a variable that two alternatives bind leaves with the arm", func(l *lintProg, x core.VarID) core.ExprID {
				or := lintOr(l, lintBind(l, x), lintBind(l, x))
				m := lintArms(l, lintScrut(l), lintArm{pat: or, body: l.VarRef(span(0, 1), x)})
				return l.Prim(span(0, 1), core.PrimAddInt, []core.ExprID{m, l.VarRef(span(0, 1), x)})
			}, 1},
			// The Let still binds x when the arm that bound it again is over.
			{"a Let around the Match keeps its variable", func(l *lintProg, x core.VarID) core.ExprID {
				m := lintArms(l, lintScrut(l), lintArm{pat: lintBind(l, x), body: l.LitInt(span(0, 1), 1)})
				body := l.Prim(span(0, 1), core.PrimAddInt, []core.ExprID{m, l.VarRef(span(0, 1), x)})
				return l.Let(span(0, 1), x, l.LitInt(span(0, 1), 0), body)
			}, 0},
		} {
			l := newLintProg()
			x := l.variable(0)
			f := l.fn(1, nil, row.build(l, x))
			var want []string
			for range row.want {
				want = append(want, lintFun(f)+": unbound variable "+lintVar(x))
			}
			lintCheck(t, row.name, l.P, l.in, want...)
		}
	})

	t.Run("rejects_bad_patterns", func(t *testing.T) {
		// A PatBind whose variable has no slot in the frame, and a PatCon with
		// another number of patterns than its constructor has fields, wherever they
		// are in the pattern. A tuple has no name, so it is told by its arity; a
		// PatBind that an or-pattern repeats in each alternative is a violation for
		// each one.
		for _, row := range []struct {
			name  string
			build func(l *lintProg, fit, outside core.VarID) core.PatID
			want  []string // the violations; $v is the variable outside the frame
		}{
			{"a variable in the last slot of the frame", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintBind(l, fit)
			}, nil},
			{"a variable one past the frame", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintBind(l, outside)
			}, []string{"variable $v has slot 1, outside a frame of 1 slot(s)"}},
			{"a variable deep in a pattern", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "Pair", lintWild(l), lintCon(l, "Pair", lintWild(l), lintBind(l, outside)))
			}, []string{"variable $v has slot 1, outside a frame of 1 slot(s)"}},
			{"the second alternative of an or-pattern", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintOr(l, lintBind(l, fit), lintBind(l, outside))
			}, []string{"variable $v has slot 1, outside a frame of 1 slot(s)"}},
			{"both alternatives of an or-pattern", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintOr(l, lintBind(l, outside), lintBind(l, outside))
			}, []string{"variable $v has slot 1, outside a frame of 1 slot(s)", "variable $v has slot 1, outside a frame of 1 slot(s)"}},
			{"two patterns for two fields", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "Pair", lintWild(l), lintWild(l))
			}, nil},
			{"no patterns for no fields", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "None")
			}, nil},
			{"one pattern too few", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "Pair", lintWild(l))
			}, []string{"pattern of constructor `Pair` with 1 argument(s), but it takes 2"}},
			{"one pattern too many", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "Pair", lintWild(l), lintWild(l), lintWild(l))
			}, []string{"pattern of constructor `Pair` with 3 argument(s), but it takes 2"}},
			{"a pattern for no field", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "None", lintWild(l))
			}, []string{"pattern of constructor `None` with 1 argument(s), but it takes 0"}},
			{"no patterns for two fields", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "Pair")
			}, []string{"pattern of constructor `Pair` with 0 argument(s), but it takes 2"}},
			{"a tuple with the arity of its constructor", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "", lintWild(l), lintWild(l))
			}, nil},
			{"a tuple with one pattern too many", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "", lintWild(l), lintWild(l), lintWild(l))
			}, []string{"pattern of tuple constructor with 3 argument(s), but it takes 2"}},
			{"inside an or-pattern", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintOr(l, lintCon(l, "None"), lintCon(l, "Pair", lintWild(l)))
			}, []string{"pattern of constructor `Pair` with 1 argument(s), but it takes 2"}},
			{"an outer pattern before the one inside it", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "Pair", lintCon(l, "Pair", lintWild(l)), lintCon(l, "None", lintWild(l)))
			}, []string{"pattern of constructor `Pair` with 1 argument(s), but it takes 2", "pattern of constructor `None` with 1 argument(s), but it takes 0"}},
			{"an outer pattern with the wrong arity and its inner ones", func(l *lintProg, fit, outside core.VarID) core.PatID {
				return lintCon(l, "Pair", lintBind(l, outside))
			}, []string{"pattern of constructor `Pair` with 1 argument(s), but it takes 2", "variable $v has slot 1, outside a frame of 1 slot(s)"}},
		} {
			l := newLintProg()
			fit, outside := l.variable(0), l.variable(1)
			f := l.fn(1, nil, lintArms(l, lintScrut(l), lintArm{pat: row.build(l, fit, outside), body: l.LitInt(span(0, 1), 1)}))
			var want []string
			for _, w := range row.want {
				want = append(want, lintFun(f)+": "+strings.ReplaceAll(w, "$v", lintVar(outside)))
			}
			lintCheck(t, row.name, l.P, l.in, want...)
		}

		// The violations come in the order of the source: the scrutinee, then each
		// arm's pattern, its guard and its body, and then the next arm.
		l := newLintProg()
		bad, a, b, c := l.variable(1), l.variable(0), l.variable(0), l.variable(0)
		lintCon(l, "Pair", lintWild(l), lintWild(l)) // make sure the constructors exist
		body := lintArms(l, l.VarRef(span(0, 1), a),
			lintArm{pat: lintBind(l, bad), guard: l.VarRef(span(0, 1), b), hasGuard: true, body: l.VarRef(span(0, 1), c)},
			lintArm{pat: lintCon(l, "Pair", lintWild(l)), body: l.LitInt(span(0, 1), 1)})
		f := l.fn(1, nil, body)
		lintCheck(t, "the order of the source", l.P, l.in,
			lintFun(f)+": unbound variable "+lintVar(a),
			lintFun(f)+": variable "+lintVar(bad)+" has slot 1, outside a frame of 1 slot(s)",
			lintFun(f)+": unbound variable "+lintVar(b),
			lintFun(f)+": unbound variable "+lintVar(c),
			lintFun(f)+": pattern of constructor `Pair` with 1 argument(s), but it takes 2")

		// A kind of pattern that Lint has no case for, which only a Program made by
		// hand can hold, is a bug of whoever made it: each switch ends in
		// panic("unreachable").
		l = newLintProg()
		pat := l.Pat(core.Pat{Kind: core.PatKind(77), Span: span(0, 1)})
		l.fn(1, nil, lintArms(l, lintScrut(l), lintArm{pat: pat, body: l.LitInt(span(0, 1), 1)}))
		if got := elabRaised(func() { core.Lint(l.P, l.in) }); got != "unreachable" {
			t.Errorf("Lint of a pattern of kind 77 panicked with %#v, want the string \"unreachable\"", got)
		}
	})

	t.Run("rejects_slot_out_of_frame", func(t *testing.T) {
		const slotsOf = ", outside a frame of "
		for _, row := range []struct {
			name  string
			slot  uint32 // the slot of the variable under test
			frame uint32 // the FrameSize of its function
			param bool   // whether the variable is a parameter or the variable of a Let
			bad   bool
		}{
			{"a Let variable in the last slot", 1, 2, false, false},
			{"a Let variable one past the frame", 2, 2, false, true},
			{"a Let variable far past the frame", 100, 2, false, true},
			{"a Let variable in a frame of no slots", 0, 0, false, true},
			{"a parameter in the last slot", 0, 1, true, false},
			{"a parameter one past the frame", 1, 1, true, true},
			{"a parameter far past the frame", 100, 1, true, true},
			{"a Let variable in the last slot of the largest frame", math.MaxUint32 - 1, math.MaxUint32, false, false},
			{"a Let variable one past the largest frame", math.MaxUint32, math.MaxUint32, false, true},
			// The comparison is of unsigned numbers: as signed ones, 2^31 would
			// be negative.
			{"a Let variable at 2^31 in a small frame", 1 << 31, 1, false, true},
			{"a Let variable in a frame of 2^31 slots", 1, 1 << 31, false, false},
		} {
			l := newLintProg()
			v := l.variable(row.slot)
			var f core.FunID
			if row.param {
				f = l.fn(row.frame, []core.VarID{v}, l.LitUnit(span(0, 1)))
			} else {
				f = l.fn(row.frame, nil, l.Let(span(0, 1), v, l.LitInt(span(0, 1), 1), l.LitUnit(span(0, 1))))
			}
			if !row.bad {
				lintCheck(t, row.name, l.P, l.in)
				continue
			}
			lintCheck(t, row.name, l.P, l.in, lintFun(f)+": variable "+lintVar(v)+" has slot "+strconv.FormatUint(uint64(row.slot), 10)+
				slotsOf+strconv.FormatUint(uint64(row.frame), 10)+" slot(s)")
		}

		// The parameters of a function that the runtime provides are in its
		// frame too.
		l := newLintProg()
		p := l.variable(1)
		l.push(core.Fun{Params: []core.VarID{p}, ExternName: "ki_x", FrameSize: 1, Span: span(0, 1)})
		lintCheck(t, "an extern parameter one past the frame", l.P, l.in, "fun f0: variable `v0` has slot 1, outside a frame of 1 slot(s)")
	})

	t.Run("match_coverage_is_not_checked", func(t *testing.T) {
		// Whether the arms of a Match cover every value is for the pass `patterns`
		// of the checker, so Core that has a Match with only the arm `true`, with
		// no arms at all or with a repeated arm is valid, and Lint says nothing of
		// it. (It used to ask for both literals or a wildcard.) The machine panics
		// if such a Match gets a value that no arm fits.
		for _, row := range []struct {
			name string
			pats []core.PatKind
		}{
			{"only true", []core.PatKind{core.PatTrue}},
			{"only false", []core.PatKind{core.PatFalse}},
			{"no arms", nil},
			{"true twice", []core.PatKind{core.PatTrue, core.PatTrue}},
			{"false twice", []core.PatKind{core.PatFalse, core.PatFalse}},
		} {
			l := newLintProg()
			l.fn(1, nil, lintMatch(l, row.pats...))
			lintCheck(t, row.name, l.P, l.in)
		}

		// A Match inside the arm of another one is no different.
		l := newLintProg()
		inner := lintMatch(l, core.PatTrue)
		outer := l.Match(span(0, 1), l.LitBool(span(0, 1), true), []core.Arm{armOf(l, core.PatTrue, inner)})
		l.fn(1, nil, outer)
		lintCheck(t, "a match inside a match", l.P, l.in)
	})

	t.Run("rejects_bad_function_shape", func(t *testing.T) {
		for _, row := range []struct {
			name string
			fun  func(l *lintProg) core.Fun
			want []string // the violations of the function under test, which is fun f1
		}{
			{"an extern with a body", func(l *lintProg) core.Fun {
				return core.Fun{HasBody: true, ExternName: "ki_print_int", Body: l.LitUnit(span(0, 1)), Span: span(0, 1)}
			}, []string{"fun f1: extern `ki_print_int` has a body"}},
			{"a function with no body and no extern name", func(l *lintProg) core.Fun {
				return core.Fun{Span: span(0, 1)}
			}, []string{"fun f1: has no body and no extern name"}},
			{"an ordinary function", func(l *lintProg) core.Fun {
				return core.Fun{HasBody: true, Body: l.LitUnit(span(0, 1)), Span: span(0, 1)}
			}, nil},
			{"an extern", func(l *lintProg) core.Fun {
				return core.Fun{ExternName: "ki_print_int", Span: span(0, 1)}
			}, nil},
			// The Body of a function without one is the zero id, which is some
			// other function's expression: Lint must not walk it.
			{"the body of an extern is not walked", func(l *lintProg) core.Fun {
				return core.Fun{ExternName: "ki_x", Body: l.VarRef(span(0, 1), l.variable(0)), Span: span(0, 1)}
			}, nil},
			{"the body of a function with neither is not walked", func(l *lintProg) core.Fun {
				return core.Fun{Body: l.VarRef(span(0, 1), l.variable(0)), Span: span(0, 1)}
			}, []string{"fun f1: has no body and no extern name"}},
		} {
			l := newLintProg()
			l.extern("ki_print_int", []core.VarID{l.variable(0)}) // f0 is fine
			l.push(row.fun(l))
			lintCheck(t, row.name, l.P, l.in, row.want...)
		}
	})

	t.Run("reports_every_violation_in_order", func(t *testing.T) {
		l := newLintProg()
		// fun f0 breaks every rule once. The violations come function by
		// function, the shape of the function first, then its parameters, then
		// its body in pre-order: a node's own violation before those of its
		// children, and a Let's slot before the violations of its init; those of an
		// arm's pattern come after the scrutinee's and before the arm's body.
		p := l.variable(3)  // v0: slot 3 in a frame of 1
		x := l.variable(5)  // v1: slot 5 in a frame of 1
		u1 := l.variable(0) // v2: never bound
		u2 := l.variable(0) // v3: never bound
		w := l.variable(7)  // v4: slot 7 in a frame of 1, bound by the pattern of the arm
		init := l.Prim(span(0, 1), core.PrimNegInt, []core.ExprID{l.LitInt(span(0, 1), 1), l.LitInt(span(0, 1), 2)})
		pat := l.Pat(core.Pat{Kind: core.PatBind, Span: span(0, 1), Var: w})
		match := l.Match(span(0, 1), l.VarRef(span(0, 1), u1), []core.Arm{{Pat: pat, Body: l.VarRef(span(0, 1), u2)}})
		body := l.Let(span(0, 1), x, init, match)
		l.push(core.Fun{Params: []core.VarID{p}, Body: body, HasBody: true, ExternName: "ki_x", FrameSize: 1, Span: span(0, 1)})
		// fun f1 has one violation, which comes after all those of fun f0.
		l.fn(1, nil, l.VarRef(span(0, 1), u1))
		lintCheck(t, "a program with many violations", l.P, l.in,
			"fun f0: extern `ki_x` has a body",
			"fun f0: variable `v0` has slot 3, outside a frame of 1 slot(s)",
			"fun f0: variable `v1` has slot 5, outside a frame of 1 slot(s)",
			"fun f0: primitive neg_int with 2 argument(s), but it takes 1",
			"fun f0: unbound variable `v2`",
			"fun f0: variable `v4` has slot 7, outside a frame of 1 slot(s)",
			"fun f0: unbound variable `v3`",
			"fun f1: unbound variable `v2`",
		)
	})

	t.Run("long_block_no_recursion", func(t *testing.T) {
		// A block of N statements is a chain of N Let that no limit of the
		// parser bounds, so the walks over Core must not recurse along it. Go's
		// stack is capped at 1 MiB for the whole test (see withStackCap): with
		// its default limit, 1 GB, a recursion with a frame for each of the
		// 100 000 statements would fit and the test would not see it.
		const lets = 100_000
		var src, want strings.Builder
		src.WriteString("fn main() {\n")
		want.WriteString("fn main() =\n")
		for i := 0; i < lets; i++ {
			src.WriteString("  let x" + strconv.Itoa(i) + " = 1\n")
			want.WriteString("  let x" + strconv.Itoa(i) + " = 1;\n")
		}
		src.WriteString("}\n")
		want.WriteString("  ()\n")

		// The same size of block with statements that are only expressions, which
		// the elaboration binds to a `_` each: Print gives them all different
		// names, `_`, `_1` … `_99998`, and the last one is the tail. (The test
		// checks the names, not how long it takes to find them.)
		var calls, wantCalls strings.Builder
		calls.WriteString("fn main() {\n")
		wantCalls.WriteString("fn main() =\n")
		for i := 0; i < lets; i++ {
			calls.WriteString("  print(1)\n")
			switch i {
			case 0:
				wantCalls.WriteString("  let _ = print(1);\n")
			case lets - 1:
				wantCalls.WriteString("  print(1)\n")
			default:
				wantCalls.WriteString("  let _" + strconv.Itoa(i) + " = print(1);\n")
			}
		}
		calls.WriteString("}\n")

		// lintAndPrint compiles source and checks that Lint accepts it and that
		// Print writes exactly want.
		lintAndPrint := func(what, source, wantText string) *testsupport.Compiled {
			c := testsupport.Compile(t, source)
			if got := core.Lint(c.Core, c.Session.Interner); len(got) != 0 {
				t.Errorf("%s: Lint returned %d violations, the first is %q, want none", what, len(got), got[0])
			}
			if got := core.Print(c.Core, c.Session.Interner); got != wantText {
				t.Errorf("%s: Print wrote %d bytes, want %d; the text differs at byte %d", what, len(got), len(wantText), lintFirstDiff(got, wantText))
			}
			return c
		}

		withStackCap(func() {
			c := lintAndPrint("a block of lets", src.String(), want.String())
			lintAndPrint("a block of expression statements", calls.String(), wantCalls.String())

			p := c.Core
			kinds := map[core.ExprKind]int{} // a map, so that a new kind of expression is counted and does not run out of an array
			n := 0
			for id := range p.Subexprs(p.Funs.Get(p.Main).Body) {
				kinds[p.Exprs.Get(id).Kind]++
				n++
			}
			if n != 2*lets+1 || kinds[core.Let] != lets || kinds[core.LitInt] != lets || kinds[core.LitUnit] != 1 {
				t.Errorf("Subexprs visited %d expressions (%d Let, %d LitInt, %d LitUnit), want %d (%d, %d and 1)",
					n, kinds[core.Let], kinds[core.LitInt], kinds[core.LitUnit], 2*lets+1, lets, lets)
			}
		})
	})

	t.Run("deep_constructors_no_recursion", func(t *testing.T) {
		// Lint and the walk of Subexprs cross a Con with their own stack, so 100 000
		// constructors, each the argument of the next, add no frames of Go. The
		// printer's half of this is in TestPrint.
		const depth = 100_000
		p, in, e := lintDeepCon(depth, 1)
		withStackCap(func() {
			if got := core.Lint(p, in); len(got) != 0 {
				t.Errorf("Lint of %d nested constructors returned %d violations, the first is %q, want none", depth, len(got), got[0])
			}
			n := 0
			for range p.Subexprs(e) {
				n++
			}
			// The constructors, and the literal at the bottom.
			if n != depth+1 {
				t.Errorf("Subexprs of %d nested constructors visited %d expressions, want %d", depth, n, depth+1)
			}
		})
		// The same depth with a wrong arity at the very bottom is found there.
		p, in, _ = lintDeepCon(depth, 2)
		withStackCap(func() {
			want := []string{"fun deep: constructor `Wrap` with 2 argument(s), but it takes 1"}
			if got := core.Lint(p, in); !slices.Equal(got, want) {
				t.Errorf("Lint of %d nested constructors with two arguments at the bottom returned %q, want %q", depth, got, want)
			}
		})
	})

	t.Run("deep_nesting_no_recursion", func(t *testing.T) {
		// The same for an expression that is deep and not long: 100 000
		// primitive calls, each the argument of the next. The parser bounds the
		// depth of what it reads (E0204 and E0205), so no source makes this, but
		// a Program built by hand can, and the walks do not rely on the bound.
		const depth = 100_000
		in := base.NewInterner()
		b, p := newBuilder()
		e := b.LitInt(span(0, 1), 1)
		for i := 0; i < depth; i++ {
			e = b.Prim(span(0, 1), core.PrimNegInt, []core.ExprID{e})
		}
		p.Funs.Push(core.Fun{Name: in.InternString("deep"), Body: e, HasBody: true, UserModule: true})
		wantText := "fn deep() =\n  " + strings.Repeat("neg_int(", depth) + "1" + strings.Repeat(")", depth) + "\n"

		withStackCap(func() {
			if got := core.Lint(p, in); len(got) != 0 {
				t.Errorf("Lint of %d nested primitives returned %d violations, the first is %q, want none", depth, len(got), got[0])
			}
			if got := core.Print(p, in); got != wantText {
				t.Errorf("Print of %d nested primitives wrote %d bytes, want %d; the text differs at byte %d", depth, len(got), len(wantText), lintFirstDiff(got, wantText))
			}
			n := 0
			for range p.Subexprs(e) {
				n++
			}
			if n != depth+1 {
				t.Errorf("Subexprs of %d nested primitives visited %d expressions, want %d", depth, n, depth+1)
			}
		})
	})
}

// lintDeepCon is the Con of depth nested constructors with one field each, the
// last holding literals: `Wrap(Wrap(… 1))`. The parser bounds how deep a source
// goes, and a Program built by hand does not. The innermost Con is given
// `innermost` literals instead of one, which is a wrong arity if it is not one.
func lintDeepCon(depth, innermost int) (*core.Program, *base.Interner, core.ExprID) {
	in := base.NewInterner()
	b, p := newBuilder()
	wrap := p.Ctors.Push(core.Ctor{Name: in.InternString("Wrap"), Arity: 1})
	bottom := make([]core.ExprID, innermost)
	for i := range bottom {
		bottom[i] = b.LitInt(span(0, 1), 1)
	}
	e := b.Con(span(0, 1), wrap, bottom)
	for i := 1; i < depth; i++ {
		e = b.Con(span(0, 1), wrap, []core.ExprID{e})
	}
	p.Funs.Push(core.Fun{Name: in.InternString("deep"), Body: e, HasBody: true, UserModule: true})
	return p, in, e
}

// lintMatch makes a Match on `true` with one arm for each pattern in pats, each
// with a literal for body.
func lintMatch(l *lintProg, pats ...core.PatKind) core.ExprID {
	arms := make([]core.Arm, len(pats))
	for i, pat := range pats {
		arms[i] = armOf(l, pat, l.LitInt(span(0, 1), int64(i)))
	}
	return l.Match(span(0, 1), l.LitBool(span(0, 1), true), arms)
}

// lintArm is an arm for lintArms: a pattern, an optional guard and a body.
type lintArm struct {
	pat      core.PatID
	guard    core.ExprID
	hasGuard bool
	body     core.ExprID
}

// lintScrut is a scrutinee that has nothing to report: the literal `true`.
func lintScrut(l *lintProg) core.ExprID { return l.LitBool(span(0, 1), true) }

// lintArms makes a Match on scrut with the arms arms.
func lintArms(l *lintProg, scrut core.ExprID, arms ...lintArm) core.ExprID {
	out := make([]core.Arm, len(arms))
	for i, a := range arms {
		out[i] = core.Arm{Pat: a.pat, Guard: a.guard, HasGuard: a.hasGuard, Body: a.body}
	}
	return l.Match(span(0, 1), scrut, out)
}

// lintWild makes the pattern `_`, and lintBind the pattern that binds v.
func lintWild(l *lintProg) core.PatID {
	return l.Pat(core.Pat{Kind: core.PatWildcard, Span: span(0, 1)})
}

func lintBind(l *lintProg, v core.VarID) core.PatID {
	return l.Pat(core.Pat{Kind: core.PatBind, Span: span(0, 1), Var: v})
}

// lintCon makes a pattern of the constructor called name, with the patterns subs
// for its fields. The constructors are made when they are first asked for, with
// the arity of their name: `None` has no fields, `Pair` two, and the empty name
// is the tuple constructor of two.
func lintCon(l *lintProg, name string, subs ...core.PatID) core.PatID {
	c := core.Ctor{Name: l.in.InternString(name), Arity: 2}
	switch name {
	case "None":
		c.Arity = 0
	case "":
		c.Name, c.Tuple = base.NoSymbol, true
	}
	id := core.CtorID(0)
	found := false
	for i, have := range l.P.Ctors.All() {
		if have.Name == c.Name && have.Tuple == c.Tuple {
			id, found = i, true
		}
	}
	if !found {
		id = l.P.Ctors.Push(c)
	}
	return l.Pat(core.Pat{Kind: core.PatCon, Span: span(0, 1), Ctor: id, Args: base.AppendRange(&l.P.PatPool, subs)})
}

// lintOr makes the or-pattern of alts.
func lintOr(l *lintProg, alts ...core.PatID) core.PatID {
	return l.Pat(core.Pat{Kind: core.PatOr, Span: span(0, 1), Args: base.AppendRange(&l.P.PatPool, alts)})
}

// patMaker is what makes patterns: a Builder, or a lintProg, which holds one.
type patMaker interface {
	Pat(core.Pat) core.PatID
}

// armOf makes an arm with a pattern of the given kind, that has no fields, and
// the body body.
func armOf(b patMaker, kind core.PatKind, body core.ExprID) core.Arm {
	return core.Arm{Pat: b.Pat(core.Pat{Kind: kind, Span: span(0, 1)}), Body: body}
}

// lintFirstDiff returns the index of the first byte where a and b differ, or
// the length of the shorter one if one is a prefix of the other.
func lintFirstDiff(a, b string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func TestSubexprs(t *testing.T) {
	t.Run("a_con_visits_its_arguments", func(t *testing.T) {
		// A Con is a node with its arguments as children, in order, like a call
		// without a callee: `Pair(1, Pair(2, 3))` is the Con, 1, the inner Con, 2
		// and 3.
		b, p := newBuilder()
		one := b.LitInt(span(0, 1), 1)
		two := b.LitInt(span(2, 3), 2)
		three := b.LitInt(span(4, 5), 3)
		inner := b.Con(span(2, 5), 1, []core.ExprID{two, three})
		outer := b.Con(span(0, 5), 1, []core.ExprID{one, inner})
		empty := b.Con(span(6, 7), 0, nil)
		if got, want := slices.Collect(p.Subexprs(outer)), []core.ExprID{outer, one, inner, two, three}; !slices.Equal(got, want) {
			t.Errorf("Subexprs of the Con visited %v, want %v", got, want)
		}
		if got := slices.Collect(p.Subexprs(empty)); !slices.Equal(got, []core.ExprID{empty}) {
			t.Errorf("Subexprs of a Con with no arguments visited %v, want just the Con", got)
		}
	})

	t.Run("preorder_visits_every_node", func(t *testing.T) {
		// let x = add_int(1, 2); x
		b, p := newBuilder()
		x := p.Vars.Push(core.Var{})
		one := b.LitInt(span(0, 1), 1)
		two := b.LitInt(span(2, 3), 2)
		sum := b.Prim(span(0, 3), core.PrimAddInt, []core.ExprID{one, two})
		ref := b.VarRef(span(5, 6), x)
		let := b.Let(span(0, 6), x, sum, ref)
		// The ids are in the order the Builder made the nodes, which is not the
		// order of a pre-order walk: the node comes before its children.
		if got, want := slices.Collect(p.Subexprs(let)), []core.ExprID{let, sum, one, two, ref}; !slices.Equal(got, want) {
			t.Errorf("Subexprs of the Let visited %v, want %v (the Let, the primitive, 1, 2 and the variable)", got, want)
		}

		// A Call visits the callee and then the arguments, and a Match the
		// scrutinee and then the arms in order, each arm's body after the one
		// before it. The walk starts at the root it is given, not at the
		// first expression.
		cond := b.LitBool(span(10, 11), true)
		thenArm := b.LitInt(span(12, 13), 7)
		elseArm := b.LitInt(span(14, 15), 8)
		match := b.Match(span(10, 15), cond, []core.Arm{armOf(b, core.PatTrue, thenArm), armOf(b, core.PatFalse, elseArm)})
		f := b.FunRef(span(20, 21), 0)
		last := b.VarRef(span(22, 23), x)
		call := b.Call(span(20, 23), f, []core.ExprID{match, last})
		if got, want := slices.Collect(p.Subexprs(call)), []core.ExprID{call, f, match, cond, thenArm, elseArm, last}; !slices.Equal(got, want) {
			t.Errorf("Subexprs of the Call visited %v, want %v (the Call, its callee, the Match, its scrutinee and its two arms, the last argument)", got, want)
		}
		if got, want := slices.Collect(p.Subexprs(match)), []core.ExprID{match, cond, thenArm, elseArm}; !slices.Equal(got, want) {
			t.Errorf("Subexprs of the Match visited %v, want %v", got, want)
		}
		// A leaf is its own walk.
		if got, want := slices.Collect(p.Subexprs(last)), []core.ExprID{last}; !slices.Equal(got, want) {
			t.Errorf("Subexprs of a leaf visited %v, want just the leaf %v", got, want)
		}
		// The walk reads the Program and writes nothing: the same root gives the
		// same ids the second time.
		if got, want := slices.Collect(p.Subexprs(let)), []core.ExprID{let, sum, one, two, ref}; !slices.Equal(got, want) {
			t.Errorf("a second walk of the Let visited %v, want %v", got, want)
		}
	})

	t.Run("a_match_visits_its_guards", func(t *testing.T) {
		// A Match visits the scrutinee and then, for each arm in order, its guard if
		// it has one and then its body. The patterns are not expressions, so they are
		// not visited.
		b, p := newBuilder()
		scrut := b.LitBool(span(0, 1), true)
		g1, b1 := b.LitBool(span(2, 3), true), b.LitInt(span(4, 5), 1)
		b2 := b.LitInt(span(6, 7), 2)
		g3, b3 := b.LitBool(span(8, 9), false), b.LitInt(span(10, 11), 3)
		wild := b.Pat(core.Pat{Kind: core.PatWildcard, Span: span(0, 1)})
		match := b.Match(span(0, 11), scrut, []core.Arm{
			{Pat: wild, Guard: g1, HasGuard: true, Body: b1},
			{Pat: wild, Body: b2},
			{Pat: wild, Guard: g3, HasGuard: true, Body: b3},
		})
		if got, want := slices.Collect(p.Subexprs(match)), []core.ExprID{match, scrut, g1, b1, b2, g3, b3}; !slices.Equal(got, want) {
			t.Errorf("Subexprs of a match with guards visited %v, want %v", got, want)
		}
	})

	t.Run("stops_when_the_loop_breaks", func(t *testing.T) {
		b, p := newBuilder()
		one := b.LitInt(span(0, 1), 1)
		two := b.LitInt(span(2, 3), 2)
		three := b.LitInt(span(4, 5), 3)
		sum := b.Prim(span(0, 5), core.PrimAddInt, []core.ExprID{one, two})
		root := b.Prim(span(0, 5), core.PrimAddInt, []core.ExprID{sum, three})
		// A loop that breaks after two ids gets those two. If the iterator
		// kept calling yield after the break, Go would panic.
		var got []core.ExprID
		for id := range p.Subexprs(root) {
			got = append(got, id)
			if len(got) == 2 {
				break
			}
		}
		if want := []core.ExprID{root, sum}; !slices.Equal(got, want) {
			t.Errorf("a loop that breaks after two ids got %v, want %v", got, want)
		}
	})
}
