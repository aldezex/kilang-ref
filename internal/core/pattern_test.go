package core_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here are about patterns in Core: how Builder makes them, how the
// elaboration turns `match`, `let` with a pattern and `if` into Match arms with
// them, and how Print writes them. They reuse elabWant, elabPat, elabFun and the
// stack cap from elaborate_test.go, and lint_test.go's lintProg.

// patSource makes the file that holds the type declarations of the tests and the
// function f with the given parameters, result type and body.
func patSource(params, result, body string) string {
	return "type Shape {\n  Circle(Float)\n  Rect(Float, Float)\n  Unit\n}\n" +
		"type Opt { No, Yes(Int) }\n" +
		"type Pt { x: Int, y: Int, z: Int }\n" +
		"fn f(" + params + ") -> " + result + " {\n" + body + "\n}\n"
}

// patVars returns the variable of each PatBind in the pattern id and under it,
// left to right. It recurses, which is fine for the small patterns of the tests.
func patVars(p *core.Program, id core.PatID) []core.VarID {
	pat := p.Pats.Get(id)
	if pat.Kind == core.PatBind {
		return []core.VarID{pat.Var}
	}
	var out []core.VarID
	if pat.Kind == core.PatCon || pat.Kind == core.PatOr {
		for _, sub := range p.PatList(pat.Args) {
			out = append(out, patVars(p, sub)...)
		}
	}
	return out
}

func TestPatternCore(t *testing.T) {
	t.Run("builder_creates_pats_and_guarded_arms", func(t *testing.T) {
		b, p := newBuilder()
		// The patterns of a constructor are in PatPool, and the ListRange of the
		// second one starts after the first's: a range that always started at 0
		// would give it the first one's patterns.
		w := b.Pat(core.Pat{Kind: core.PatWildcard, Span: span(0, 1)})
		n := b.Pat(core.Pat{Kind: core.PatInt, Span: span(2, 3), Int: -7})
		v := b.Pat(core.Pat{Kind: core.PatBind, Span: span(4, 5), Var: 3})
		first := b.Pat(core.Pat{Kind: core.PatCon, Span: span(0, 9), Ctor: 2, Args: base.AppendRange(&p.PatPool, []core.PatID{w, n})})
		second := b.Pat(core.Pat{Kind: core.PatOr, Span: span(0, 20), Args: base.AppendRange(&p.PatPool, []core.PatID{v, first, n})})
		for i, id := range []core.PatID{w, n, v, first, second} {
			if int(id) != i {
				t.Errorf("the pattern number %d has the id %d, want %d", i, id, i)
			}
		}
		if p.Pats.Len() != 5 {
			t.Errorf("Pats.Len() = %d, want 5", p.Pats.Len())
		}
		if got, want := p.Pats.Get(n), (core.Pat{Kind: core.PatInt, Span: span(2, 3), Int: -7}); got != want {
			t.Errorf("the Int pattern is %+v, want %+v", got, want)
		}
		if got, want := p.Pats.Get(v), (core.Pat{Kind: core.PatBind, Span: span(4, 5), Var: 3}); got != want {
			t.Errorf("the variable pattern is %+v, want %+v", got, want)
		}
		if got, want := p.Pats.Get(second).Args, (base.ListRange{First: 2, Count: 3}); got != want {
			t.Errorf("the alternatives of the or-pattern are %+v, want %+v", got, want)
		}
		if got := p.PatList(p.Pats.Get(first).Args); len(got) != 2 || got[0] != w || got[1] != n || cap(got) != len(got) {
			t.Errorf("PatList of the constructor is %v with cap %d, want [%d %d] with the capacity cut", got, cap(got), w, n)
		}
		if got := p.PatList(p.Pats.Get(second).Args); len(got) != 3 || got[0] != v || got[1] != first || got[2] != n {
			t.Errorf("PatList of the or-pattern is %v, want [%d %d %d]", got, v, first, n)
		}
		if len(p.ExprPool) != 0 || len(p.ArmPool) != 0 || p.Exprs.Len() != 0 {
			t.Errorf("patterns left %d expressions, %d ids in ExprPool and %d arms, want none", p.Exprs.Len(), len(p.ExprPool), len(p.ArmPool))
		}

		// An arm keeps its guard, and an arm with no guard has none.
		g := b.LitBool(span(30, 31), true)
		body := b.LitInt(span(32, 33), 1)
		b.Match(span(30, 33), g, []core.Arm{{Pat: first, Guard: g, HasGuard: true, Body: body}, {Pat: w, Body: body}})
		want := []core.Arm{{Pat: first, Guard: g, HasGuard: true, Body: body}, {Pat: w, Body: body}}
		if len(p.ArmPool) != 2 || p.ArmPool[0] != want[0] || p.ArmPool[1] != want[1] {
			t.Errorf("ArmPool is %+v, want %+v", p.ArmPool, want)
		}
	})

	t.Run("if_and_logic_arms_are_pat_nodes", func(t *testing.T) {
		// `if`, `&&` and `||` still elaborate to a Match whose arms are `true` and
		// `false`, in that order; the arms now hold the ids of a PatTrue and a
		// PatFalse of their own, with the span of the whole expression, and no guard.
		for _, row := range []struct{ name, src, at string }{
			{"if", "fn f(c: Bool) -> Int { if c { 1 } else { 2 } }", "if c { 1 } else { 2 }"},
			{"if without else", "fn f(c: Bool) { if c { print(1) } }", "if c { print(1) }"},
			{"and", "fn f(a: Bool, b: Bool) -> Bool { a && b }", "a && b"},
			{"or", "fn f(a: Bool, b: Bool) -> Bool { a || b }", "a || b"},
		} {
			c := testsupport.Compile(t, row.src)
			body := c.Core.Exprs.Get(elabFun(t, c, "f").Body)
			if body.Kind != core.Match {
				t.Errorf("%s: the body is a %v, want a Match", row.name, body.Kind)
				continue
			}
			arms := c.Core.ArmsOf(body.Arms)
			if len(arms) != 2 || arms[0].Pat == arms[1].Pat || arms[0].HasGuard || arms[1].HasGuard {
				t.Errorf("%s: arms %+v, want two with patterns of their own and no guard", row.name, arms)
				continue
			}
			for i, kind := range []core.PatKind{core.PatTrue, core.PatFalse} {
				pat := c.Core.Pats.Get(arms[i].Pat)
				if pat.Kind != kind || elabText(c, pat.Span) != row.at || pat.Args.Count != 0 {
					t.Errorf("%s: arm %d has the pattern %+v (span %q), want kind %d with the span %q", row.name, i, pat, elabText(c, pat.Span), kind, row.at)
				}
			}
		}
	})

	t.Run("patterns_elaborate_to_pats", func(t *testing.T) {
		// The shape of each function (elabShape): the patterns as elabPat writes
		// them. A constructor is its name and the patterns of its fields, a tuple
		// has no name, a record is its constructor with a pattern for each field in
		// the order the record declares them, `_` for the ones that the source leaves
		// out or does not name, and a variable is name@slot.
		for _, row := range []struct{ name, params, result, body, want string }{
			{"the arms of a sum", "s: Shape", "Float",
				"  match s {\n    Circle(r) => r * r\n    Rect(w, h) => w * h\n    Unit => 0.0\n  }",
				"match s@0 { Circle(r@1) => mul_float(r@1, r@1); Rect(w@2, h@3) => mul_float(w@2, h@3); Unit => 0.0 }"},
			{"literals, nested and negative", "o: Opt", "Int",
				"  match o {\n    Yes(0) => 1\n    Yes(-1) => 2\n    Yes(k) => k\n    No => 0\n  }",
				"match o@0 { Yes(0) => 1; Yes(-1) => 2; Yes(k@1) => k@1; No => 0 }"},
			{"Bool literals and a wildcard", "b: Bool, n: Int", "Int",
				"  match b {\n    true => 1\n    false if n > 0 => 2\n    _ => 3\n  }",
				"match b@0 { true => 1; false if gt_int(n@1, 0) => 2; _ => 3 }"},
			{"a tuple", "t: (Int, Bool)", "Int",
				"  match t {\n    (0, true) => 1\n    (n, _) => n\n  }",
				"match t@0 { (0, true) => 1; (n@1, _) => n@1 }"},
			{"records: punning, the order of the record, and `..`", "p: Pt", "Int",
				"  match p {\n    Pt { z, x: a, .. } => a + z\n    Pt { x, y, z } => x + y + z\n  }",
				"match p@0 { Pt(a@2, _, z@1) => add_int(a@2, z@1); Pt(x@3, y@4, z@5) => add_int(add_int(x@3, y@4), z@5) }"},
			{"an or-pattern inside a constructor", "o: Opt", "Int",
				"  match o {\n    Yes(1 | 2) => 1\n    Yes(_) => 2\n    No => 3\n  }",
				"match o@0 { Yes(1 | 2) => 1; Yes(_) => 2; No => 3 }"},
			{"an or-pattern with a guard that binds in both alternatives", "p: (Opt, Opt), n: Int", "Int",
				"  match p {\n    (Yes(x), _) | (_, Yes(x)) if x > n => x\n    _ => 0\n  }",
				"match p@0 { (Yes(x@2), _) | (_, Yes(x@2)) if gt_int(x@2, n@1) => x@2; _ => 0 }"},
			{"an or-pattern whose alternatives name the variables in another order", "p: (Opt, Opt)", "Int",
				"  match p {\n    (Yes(a), Yes(b)) | (Yes(b), Yes(a)) => a - b\n    _ => 0\n  }",
				"match p@0 { (Yes(a@1), Yes(b@2)) | (Yes(b@2), Yes(a@1)) => sub_int(a@1, b@2); _ => 0 }"},
			{"slots follow the order of the source", "p: (Int, Int), q: Int", "Int",
				"  match ({\n    let a = q\n    p\n  }) {\n    (x, y) if {\n      let g = x\n      g > q\n    } => {\n      let b = y\n      b\n    }\n    (z, _) => z\n  }",
				"match let a@2 = q@1; p@0 { (x@3, y@4) if let g@5 = x@3; gt_int(g@5, q@1) => let b@6 = y@4; b@6; (z@7, _) => z@7 }"},
		} {
			c := testsupport.Compile(t, patSource(row.params, row.result, row.body))
			elabWant(t, c, "f", row.want)
		}

		// One slot for a variable that two alternatives bind, and one Var: the frame
		// has the two parameters and x.
		c := testsupport.Compile(t, patSource("p: (Opt, Opt), n: Int", "Int", "  match p {\n    (Yes(x), _) | (_, Yes(x)) if x > n => x\n    _ => 0\n  }"))
		f := elabFun(t, c, "f")
		if f.FrameSize != 3 {
			t.Errorf("FrameSize of a function with an or-pattern that binds x twice is %d, want 3 (p, n and one x)", f.FrameSize)
		}
		vars := patVars(c.Core, c.Core.ArmsOf(c.Core.Exprs.Get(f.Body).Arms)[0].Pat)
		if len(vars) != 2 || vars[0] != vars[1] {
			t.Errorf("the variables that the two alternatives bind are %v, want two equal ids", vars)
		}
	})

	t.Run("pattern_spans_cover_their_syntax", func(t *testing.T) {
		// Each pattern node has the span of the pattern it comes from, a pun the span
		// of its name, and the `_` that stands for a field that a record pattern leaves
		// out the span of the whole pattern. Each row is the text under the span of
		// every node of one arm's pattern, in pre-order; and each Match has the span of
		// the `match` it comes from, or of the `let` that made it.
		c := testsupport.Compile(t, patSource("p: (Opt, Opt), o: Opt, r: Pt, s: Shape, b: Bool", "Int", `  let (u, _) = p
  let q = match s {
    Rect(w, _) | Circle(w) if true => 1
    _ => 2
  }
  let t = match o {
    Yes(-1 | 2) => 1
    No => 2
    _ => 3
  }
  let v = match b {
    true => 1
    false => 2
  }
  match r {
    Pt { x, y: 0, .. } => x
    _ => 0
  }`))
		var walk func(id core.PatID, out *[]string)
		walk = func(id core.PatID, out *[]string) {
			pat := c.Core.Pats.Get(id)
			*out = append(*out, elabText(c, pat.Span))
			if pat.Kind == core.PatCon || pat.Kind == core.PatOr {
				for _, sub := range c.Core.PatList(pat.Args) {
					walk(sub, out)
				}
			}
		}
		// The Matches of the function, outside in: the `let (u, _)`, then the one of
		// `s` (in the initializer of a `let`), the one of `o` and the one of `r`.
		var spans [][]string
		var matches []string
		for id := range c.Core.Subexprs(elabFun(t, c, "f").Body) {
			if c.Core.Exprs.Get(id).Kind != core.Match {
				continue
			}
			matches = append(matches, elabText(c, c.Core.Exprs.Get(id).Span))
			for _, arm := range c.Core.ArmsOf(c.Core.Exprs.Get(id).Arms) {
				var row []string
				walk(arm.Pat, &row)
				spans = append(spans, row)
			}
		}
		want := [][]string{
			{"(u, _)", "u", "_"},
			{"Rect(w, _) | Circle(w)", "Rect(w, _)", "w", "_", "Circle(w)", "w"},
			{"_"},
			{"Yes(-1 | 2)", "-1 | 2", "-1", "2"},
			{"No"},
			{"_"},
			{"true"},
			{"false"},
			{"Pt { x, y: 0, .. }", "x", "0", "Pt { x, y: 0, .. }"},
			{"_"},
		}
		wantMatches := []string{
			"let (u, _) = p",
			"match s {\n    Rect(w, _) | Circle(w) if true => 1\n    _ => 2\n  }",
			"match o {\n    Yes(-1 | 2) => 1\n    No => 2\n    _ => 3\n  }",
			"match b {\n    true => 1\n    false => 2\n  }",
			"match r {\n    Pt { x, y: 0, .. } => x\n    _ => 0\n  }",
		}
		if !slices.Equal(matches, wantMatches) {
			t.Errorf("the Matches of the function have the spans %q, want %q", matches, wantMatches)
		}
		if len(spans) != len(want) {
			t.Fatalf("the function has %d arms, want %d", len(spans), len(want))
		}
		for i := range want {
			if strings.Join(spans[i], "|") != strings.Join(want[i], "|") {
				t.Errorf("arm %d: the nodes of the pattern have the spans %q, want %q", i, spans[i], want[i])
			}
		}
	})

	t.Run("let_with_a_pattern_is_a_match_of_one_arm", func(t *testing.T) {
		// `let (a, b) = t` followed by the rest of the block is a Match on `t` with
		// one arm, whose body is the rest: the checker made the pattern irrefutable,
		// so no other arm is needed. The Match has the span of the statement and the
		// names of the pattern take their slots before the initializer's.
		for _, row := range []struct{ name, params, body, want string }{
			{"a tuple", "t: (Int, Int)", "  let (a, b) = t\n  let s = a + b\n  s",
				"match t@0 { (a@1, b@2) => let s@3 = add_int(a@1, b@2); s@3 }"},
			{"a wildcard", "t: Int", "  let _ = t\n  t",
				"match t@0 { _ => t@0 }"},
			{"a pattern between plain lets", "t: (Int, Int)", "  let (a, _) = t\n  let c = a\n  let (d, e) = (c, a)\n  d + e",
				"match t@0 { (a@1, _) => let c@2 = a@1; match (c@2, a@1) { (d@3, e@4) => add_int(d@3, e@4) } }"},
			{"a record", "t: Pt", "  let Pt { x, .. } = t\n  x",
				"match t@0 { Pt(x@1, _, _) => x@1 }"},
			{"a block in the initializer", "t: (Int, Int)", "  let (a, b) = {\n    let u = 1\n    t\n  }\n  a + b",
				"match let u@3 = 1; t@0 { (a@1, b@2) => add_int(a@1, b@2) }"},
		} {
			c := testsupport.Compile(t, patSource(row.params, "Int", row.body))
			elabWant(t, c, "f", row.want)
		}
		c := testsupport.Compile(t, patSource("t: (Int, Int)", "Int", "  let (a, b) = t\n  a + b"))
		m := c.Core.Exprs.Get(elabFun(t, c, "f").Body)
		if got, want := elabText(c, m.Span), "let (a, b) = t"; got != want {
			t.Errorf("the Match of a `let` has the span of %q, want %q", got, want)
		}
		if arms := c.Core.ArmsOf(m.Arms); len(arms) != 1 || arms[0].HasGuard {
			t.Errorf("the Match of a `let` has the arms %+v, want exactly one, with no guard", arms)
		}
	})

	t.Run("patterns_that_have_no_core_panic", func(t *testing.T) {
		// Elaborate needs a program without errors, and what it cannot translate it
		// does not guess: it panics with the text that the report of the ICE shows.
		// These are the patterns that the front end rejects and that leave a node
		// behind: one that could not be read, a list (no type has one yet, E0401), the
		// name of something that is not a constructor, and a record pattern on a value
		// that is not a record.
		for _, row := range []struct{ name, src, want string }{
			{"unreadable", "fn main() { match 1 { + => 1 } }\n", "core: cannot elaborate a pattern that could not be read"},
			{"list", "fn main() { match 1 { [a] => 1 } }\n", "core: cannot elaborate a list pattern"},
			{"not a constructor", "type Pt { x: Int }\nfn main() { match 1 { Pt(a) => 1 } }\n", "core: cannot elaborate a constructor pattern whose name is not a constructor"},
			{"record on an Int", "type Pt { x: Int }\nfn main() { match 1 { Pt { x } => 1 } }\n", "core: cannot elaborate a record pattern on a value that is not a record: `Int`"},
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

	t.Run("print_of_patterns", func(t *testing.T) {
		// Patterns are written as the program writes them, as far as Core keeps them:
		// a record is a positional constructor, as the Con of a record literal is,
		// with `_` for the fields that the source does not name; an or-pattern that is
		// an alternative of another is in parentheses; and the guard goes between the
		// pattern and the arrow. The variables are named as the text reaches them: the
		// pattern, then the guard, then the body.
		c := testsupport.Compile(t, patSource("p: (Opt, Opt), s: Shape, r: Pt, n: Int", "Int", `  let (a, _) = p
  let k = match s {
    Circle(radius) if n > 0 => 1
    Rect(_, _) | Unit => 2
    _ => 3
  }
  match r {
    Pt { x: 0, .. } => k
    Pt { z, y, .. } if y == z => z
    _ => match a {
      Yes(0 | -1) => 7
      Yes(1) | (No | Yes(2)) => 8
      Yes(m) => m
    }
  }`))
		want := `fn f(p, s, r, n) =
  match p {
    (a, _) => let k = match s {
      Circle(radius) if gt_int(n, 0) => 1
      Rect(_, _) | Unit => 2
      _ => 3
    };
      match r {
        Pt(0, _, _) => k
        Pt(_, y, z) if eq_int(y, z) => z
        _ => match a {
          Yes(0 | -1) => 7
          Yes(1) | (No | Yes(2)) => 8
          Yes(m) => m
        }
      }
  }
`
		if got := core.Print(c.Core, c.Session.Interner); !strings.HasSuffix(got, want) {
			t.Errorf("Print of patterns:\n%s\nwant it to end with:\n%s", got, want)
		}
		// A guard that takes several lines goes on two spaces further in than the arm,
		// as any expression that starts in the middle of a line does.
		c = testsupport.Compile(t, patSource("n: Int", "Int", "  match n {\n    k if {\n      let g = k\n      g > 0\n    } => k\n    _ => 0\n  }"))
		want = "fn f(n) =\n  match n {\n    k if let g = k;\n      gt_int(g, 0) => k\n    _ => 0\n  }\n"
		if got := core.Print(c.Core, c.Session.Interner); !strings.HasSuffix(got, want) {
			t.Errorf("Print of a guard of two lines:\n%s\nwant it to end with:\n%s", got, want)
		}
		// Two variables of one function with the same name are told apart where the
		// pattern binds them, as everywhere: the `x` of the second arm is `x_1` in its
		// pattern and in its body.
		c = testsupport.Compile(t, patSource("a: Opt, b: Opt", "Int", "  let r = match a {\n    Yes(x) => x\n    No => 0\n  }\n  match b {\n    Yes(x) => x + r\n    No => r\n  }"))
		want = "fn f(a, b) =\n  let r = match a {\n    Yes(x) => x\n    No => 0\n  };\n  match b {\n    Yes(x_1) => add_int(x_1, r)\n    No => r\n  }\n"
		if got := core.Print(c.Core, c.Session.Interner); !strings.HasSuffix(got, want) {
			t.Errorf("Print of two variables called x:\n%s\nwant it to end with:\n%s", got, want)
		}
		// The same variable in two alternatives has one name.
		c = testsupport.Compile(t, patSource("p: (Opt, Opt)", "Int", "  match p {\n    (Yes(x), _) | (_, Yes(x)) if x > 0 => x\n    _ => 0\n  }"))
		want = "fn f(p) =\n  match p {\n    (Yes(x), _) | (_, Yes(x)) if gt_int(x, 0) => x\n    _ => 0\n  }\n"
		if got := core.Print(c.Core, c.Session.Interner); !strings.HasSuffix(got, want) {
			t.Errorf("Print of an or-pattern that binds x twice:\n%s\nwant it to end with:\n%s", got, want)
		}
	})

	t.Run("big_matches_and_patterns_without_recursion", func(t *testing.T) {
		// A `match` of a hundred thousand arms, a tuple pattern of a hundred thousand
		// elements and an or-pattern of a hundred thousand alternatives are elaborated,
		// linted and printed with the stack of Go capped at 1 MiB: a recursion over the
		// arms, or one that goes down a level for each sibling, would not fit. Each is as
		// deep as a small one, so a recursion that goes down only into the parts of a
		// pattern is not tried here (deep_patterns_no_recursion does it, for Lint and the
		// printer).
		const n = 100_000
		var arms, wide, alts []string
		for i := range n {
			arms = append(arms, "    "+strconv.Itoa(i)+" => "+strconv.Itoa(i))
			wide = append(wide, "_")
			alts = append(alts, strconv.Itoa(i))
		}
		arms = append(arms, "    _ => 0")
		ones := strings.TrimSuffix(strings.Repeat("1, ", n), ", ")
		for _, row := range []struct {
			name, src string
			arms      int
			text      []string // pieces that the printed function must contain
		}{
			{"arms", "fn f(x: Int) -> Int {\n  match x {\n" + strings.Join(arms, "\n") + "\n  }\n}\n", n + 1,
				[]string{"\n    0 => 0\n", "\n    99999 => 99999\n    _ => 0\n  }\n"}},
			{"a tuple pattern", "fn f() -> Int {\n  match (" + ones + ") {\n    (" + strings.Join(wide, ", ") + ") => 1\n  }\n}\n", 1,
				[]string{"    (_, _, _", "_, _, _) => 1\n"}},
			{"an or-pattern", "fn f(x: Int) -> Int {\n  match x {\n    " + strings.Join(alts, " | ") + " => 1\n    _ => 0\n  }\n}\n", 2,
				[]string{"    0 | 1 | 2 | 3", "99998 | 99999 => 1\n    _ => 0\n"}},
		} {
			c := elabCompileCapped(t, row.src)
			f := elabFun(t, c, "f")
			if got := len(c.Core.ArmsOf(c.Core.Exprs.Get(f.Body).Arms)); got != row.arms {
				t.Errorf("%s: the Match has %d arms, want %d", row.name, got, row.arms)
			}
			var problems []string
			var printed string
			withStackCap(func() {
				problems = core.Lint(c.Core, c.Session.Interner)
				printed = core.Print(c.Core, c.Session.Interner)
			})
			if len(problems) != 0 {
				t.Errorf("%s: Lint returned %d violations, the first %q", row.name, len(problems), problems[0])
			}
			for _, piece := range row.text {
				if !strings.Contains(printed, piece) {
					t.Errorf("%s: the printed function does not contain %q", row.name, piece)
				}
			}
		}
	})

	t.Run("deep_patterns_no_recursion", func(t *testing.T) {
		// A pattern nested a hundred thousand levels deep, which a source cannot
		// write (E0204) but a Program made by hand can, is linted and printed with a
		// stack of its own: a PatCon in the field of a PatCon, and an or-pattern in the
		// first alternative of an or-pattern.
		const depth = 100_000
		in := base.NewInterner()
		b, p := newBuilder()
		wrap := p.Ctors.Push(core.Ctor{Name: in.InternString("W"), Arity: 1})
		x := p.Vars.Push(core.Var{Name: in.InternString("x"), Rep: core.RepVal, Slot: 0, Span: span(0, 1)})
		leaf := b.Pat(core.Pat{Kind: core.PatBind, Span: span(0, 1), Var: x})
		con, or := leaf, leaf
		for range depth {
			con = b.Pat(core.Pat{Kind: core.PatCon, Span: span(0, 1), Ctor: wrap, Args: base.AppendRange(&p.PatPool, []core.PatID{con})})
		}
		wild := b.Pat(core.Pat{Kind: core.PatWildcard, Span: span(0, 1)})
		for range depth {
			or = b.Pat(core.Pat{Kind: core.PatOr, Span: span(0, 1), Args: base.AppendRange(&p.PatPool, []core.PatID{or, wild})})
		}
		scrut := b.LitUnit(span(0, 1))
		for name, pat := range map[string]core.PatID{"cons": con, "ors": or} {
			body := b.Match(span(0, 1), scrut, []core.Arm{{Pat: pat, Body: b.VarRef(span(0, 1), x)}})
			p.Funs.Push(core.Fun{Name: in.InternString(name), Body: body, HasBody: true, FrameSize: 1, UserModule: true})
		}
		var problems []string
		var printed string
		withStackCap(func() {
			problems = core.Lint(p, in)
			printed = core.Print(p, in)
		})
		if len(problems) != 0 {
			t.Errorf("Lint of deep patterns returned %d violations, the first %q", len(problems), problems[0])
		}
		// The constructors nest as written, and each or-pattern that is the
		// alternative of another is in parentheses, so the chain has depth-1 pairs.
		cons := strings.Repeat("W(", depth) + "x" + strings.Repeat(")", depth)
		ors := strings.Repeat("(", depth-1) + "x" + strings.Repeat(" | _)", depth-1) + " | _"
		for name, pat := range map[string]string{"cons": cons, "ors": ors} {
			if !strings.Contains(printed, "    "+pat+" => x\n") {
				t.Errorf("the printed function %s does not have the pattern of %d levels on one line", name, depth)
			}
		}
	})
}
