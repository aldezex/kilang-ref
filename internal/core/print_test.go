package core_test

import (
	"math"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/testsupport"
)

// printSource compiles source and returns what Print writes for it.
func printSource(t *testing.T, source string) string {
	t.Helper()
	c := testsupport.Compile(t, source)
	return core.Print(c.Core, c.Session.Interner)
}

func TestPrint(t *testing.T) {
	t.Run("match_as_argument", func(t *testing.T) {
		got := printSource(t, "fn main() { print(if true { 1 } else { 2 }) }\n")
		want := `fn main() =
  print(match true {
    true => 1
    false => 2
  })
`
		if got != want {
			t.Errorf("Print of a call with a match as its argument:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("functions_and_repeated_names", func(t *testing.T) {
		// The functions come in the order of the source, with no blank line
		// between them, and without the prelude's `print`. A name that a
		// function repeats gets _1, _2… (and a repeated `_` gets 1, 2…), and
		// the count starts again in each function.
		got := printSource(t, `fn f(a: Int) -> Int {
  let x = 1
  let x = x + a
  print(x)
  print(x)
  x
}
fn g() -> Int {
  let x_1 = 1
  let x = 2
  let x = 3
  x_1 + x
}
fn h() {
  let x = 1
  print(x)
}
fn k(a: Int) -> Int {
  let a = a + 1
  a
}
fn u() {
  print(1)
  print(2)
  print(3)
  print(4)
}
`)
		want := `fn f(a) =
  let x = 1;
  let x_1 = add_int(x, a);
  let _ = print(x_1);
  let _1 = print(x_1);
  x_1
fn g() =
  let x_1 = 1;
  let x = 2;
  let x_2 = 3;
  add_int(x_1, x_2)
fn h() =
  let x = 1;
  print(x)
fn k(a) =
  let a_1 = add_int(a, 1);
  a_1
fn u() =
  let _ = print(1);
  let _1 = print(2);
  let _2 = print(3);
  print(4)
`
		if got != want {
			t.Errorf("Print of five functions:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("nested_layouts", func(t *testing.T) {
		// Whatever starts in the middle of a line goes on in the same line and
		// its other lines are indented two spaces more than that line, except
		// the closing `}` of a match, which lines up with it. That holds for a
		// match or a block in the init of a `let`, in an argument, in an arm and
		// in the callee of a call or the scrutinee of a match. (These last two
		// are not delimited, so the text of `m` reads like a Let whose body is the
		// call: the dump is for reading, and says only that much.)
		got := printSource(t, `fn h(c: Bool) -> Int {
  let a = if c { 1 } else { 2 }
  let v = {
    let w = a
    w + 1
  }
  print({
    let t = v
    t
  })
  print(if c {
    let t = a
    t
  } else { 0 })
  if c {
    let b = a + 1
    b
  } else { a }
}
fn k(c: Bool) -> Int {
  (if c { k } else { k })(c)
}
fn m(c: Bool) -> Int {
  ({
    let f = m
    f
  })(c)
}
fn n(c: Bool) -> Int {
  if {
    let a = c
    a
  } { 1 } else { 2 }
}
`)
		want := `fn h(c) =
  let a = match c {
    true => 1
    false => 2
  };
  let v = let w = a;
    add_int(w, 1);
  let _ = print(let t = v;
    t);
  let _1 = print(match c {
    true => let t_1 = a;
      t_1
    false => 0
  });
  match c {
    true => let b = add_int(a, 1);
      b
    false => a
  }
fn k(c) =
  match c {
    true => k
    false => k
  }(c)
fn m(c) =
  let f = m;
    f(c)
fn n(c) =
  match let a = c;
    a {
    true => 1
    false => 2
  }
`
		if got != want {
			t.Errorf("Print of matches and blocks inside expressions:\n%s\nwant:\n%s", got, want)
		}

		// A match in the arm of another one, with a wildcard arm, which no
		// source makes.
		in := base.NewInterner()
		b, p := newBuilder()
		deep := b.Match(span(0, 1), b.LitBool(span(0, 1), true), []core.Arm{
			{Pat: core.PatTrue, Body: b.Match(span(0, 1), b.LitBool(span(0, 1), false), []core.Arm{
				{Pat: core.PatTrue, Body: b.LitInt(span(0, 1), 1)},
				{Pat: core.PatWildcard, Body: b.LitInt(span(0, 1), 2)},
			})},
			{Pat: core.PatFalse, Body: b.LitInt(span(0, 1), 3)},
		})
		p.Funs.Push(core.Fun{Name: in.InternString("nest"), Body: deep, HasBody: true, UserModule: true})
		wantNest := `fn nest() =
  match true {
    true => match false {
      true => 1
      _ => 2
    }
    false => 3
  }
`
		if got := core.Print(p, in); got != wantNest {
			t.Errorf("Print of a match in a match:\n%s\nwant:\n%s", got, wantNest)
		}
	})

	t.Run("literals_calls_and_bodiless_functions", func(t *testing.T) {
		in := base.NewInterner()
		b, p := newBuilder()
		sym := in.InternString
		a := p.Vars.Push(core.Var{Name: sym("a")})
		bb := p.Vars.Push(core.Var{Name: sym("b")})
		z := p.Vars.Push(core.Var{Name: sym("z")})

		// fun #0 is the prelude's: it is not printed, whatever it has.
		p.Funs.Push(core.Fun{Name: sym("prelude"), Body: b.LitUnit(span(0, 1)), HasBody: true, FrameSize: 0})
		// fun #1: the callee of a call is an expression like any other, so a
		// call can be the callee of another one.
		show := core.FunID(1)
		inner := b.Call(span(0, 1), b.FunRef(span(0, 1), show), []core.ExprID{b.VarRef(span(0, 1), a)})
		args := []core.ExprID{
			b.Prim(span(0, 1), core.PrimNegInt, []core.ExprID{b.LitInt(span(0, 1), math.MinInt64)}),
			b.LitBool(span(0, 1), true), b.LitBool(span(0, 1), false), b.LitUnit(span(0, 1)),
			b.LitInt(span(0, 1), math.MaxInt64), b.LitInt(span(0, 1), 0),
		}
		p.Funs.Push(core.Fun{Name: sym("show"), Params: []core.VarID{a, bb}, Body: b.Call(span(0, 1), inner, args), HasBody: true, FrameSize: 2, UserModule: true})
		// fun #2 has no body: a function that the runtime provides, in the user's
		// file, which the elaboration never makes but a Program can have.
		p.Funs.Push(core.Fun{Name: sym("bare"), Params: []core.VarID{z}, ExternName: "ki_bare", FrameSize: 1, UserModule: true})

		want := `fn show(a, b) =
  show(a)(neg_int(-9223372036854775808), true, false, (), 9223372036854775807, 0)
fn bare(z)
`
		if got := core.Print(p, in); got != want {
			t.Errorf("Print of a call of a call, every literal and a function with no body:\n%s\nwant:\n%s", got, want)
		}

		// A Program with no function of the user's file has nothing to print.
		empty := &core.Program{}
		if got := core.Print(empty, in); got != "" {
			t.Errorf("Print of a Program with no functions = %q, want \"\"", got)
		}
		onlyPrelude := &core.Program{}
		onlyPrelude.Funs.Push(core.Fun{Name: sym("prelude"), ExternName: "ki_x"})
		if got := core.Print(onlyPrelude, in); got != "" {
			t.Errorf("Print of a Program with only the prelude's functions = %q, want \"\"", got)
		}
	})

	t.Run("float_literals_print_as_kirt_fmtnum_writes_them", func(t *testing.T) {
		// A Float is never written like an Int, so that `2.0` and `2` differ in
		// the dump, and the text is the one that `printFloat` writes.
		got := printSource(t, "fn f(x: Float) -> Float { -x * 2.0 + 1e21 }\nfn main() { printFloat(f(0.1)) }\n")
		const want = `fn f(x) =
  add_float(mul_float(neg_float(x), 2.0), 1e21)
fn main() =
  printFloat(f(0.1))
`
		if got != want {
			t.Errorf("Print of a function of Floats:\n%s\nwant:\n%s", got, want)
		}

		// A Program can hold a literal that no source writes: the special values
		// and the negative zero have their own names.
		in := base.NewInterner()
		b, p := newBuilder()
		args := []core.ExprID{
			b.LitFloat(span(0, 1), 1.5), b.LitFloat(span(0, 1), 2), b.LitFloat(span(0, 1), 1e21),
			b.LitFloat(span(0, 1), math.NaN()), b.LitFloat(span(0, 1), math.Inf(-1)), b.LitFloat(span(0, 1), math.Copysign(0, -1)),
		}
		p.Funs.Push(core.Fun{Name: in.InternString("show"), Body: b.Call(span(0, 1), b.FunRef(span(0, 1), 0), args), HasBody: true, UserModule: true})
		if got, want := core.Print(p, in), "fn show() =\n  show(1.5, 2.0, 1e21, NaN, -inf, -0.0)\n"; got != want {
			t.Errorf("Print of a call with Float literals:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("repeated_names_allocation_bound", func(t *testing.T) {
		// Each `print(1)` but the last (the tail) binds a `_`, and Print gives
		// the n-1 of them different names. A Print that looked for a free name
		// from the first candidate each time would try about n*n/2 names and
		// make a string for each; one that remembers where it stopped makes a
		// few allocations per statement. The test bounds the allocations and
		// not the time: the count does not depend on the machine, and the bound
		// is far from what either kind of Print makes. A Print that wrote
		// nothing would pass the bound, so the test also counts the lines of
		// the text: the header and one for each `print(1)` (the names are
		// checked by the tests above and by TestLint).
		const n = 10_000
		var src strings.Builder
		src.WriteString("fn main() {\n")
		for i := 0; i < n; i++ {
			src.WriteString("  print(1)\n")
		}
		src.WriteString("}\n")
		c := testsupport.Compile(t, src.String())

		var got string
		allocs := testing.AllocsPerRun(1, func() { got = core.Print(c.Core, c.Session.Interner) })
		if bound := float64(100 * n); allocs >= bound {
			t.Errorf("Print of %d lines `print(1)` made %.0f allocations, want fewer than %.0f (100 for each line)", n, allocs, bound)
		}
		if lines := strings.Count(got, "\n"); lines != n+1 {
			t.Errorf("Print of %d lines `print(1)` wrote %d lines of text, want %d (the header and one for each `print(1)`)", n, lines, n+1)
		}
	})
}
