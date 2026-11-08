package interp_test

import (
	"bytes"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/interp"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here are about how the machine runs a `match` with patterns: the arm
// that wins, the guards, the or-patterns and the cost of all that on the stack.
// They compile their programs, except where the Program has to be one that the
// checker would reject, and they reuse call, machineOf and panicText from
// machine_test.go.

// matchDecls are the declarations that the sources of these tests share.
const matchDecls = `type Shape {
  Circle(Float)
  Rect(Float, Float)
  Unit
}
type Opt { No, Yes(Int) }
type Nest { Leaf, Node(Nest, Int) }
type Pt { x: Int, y: Int, z: Int }
`

// matchRows compiles matchDecls and src once and calls each function that the
// rows name, with no arguments, on a machine of its own. A row that does not give
// the want it names fails with the name of the function.
func matchRows(t *testing.T, src string, rows []matchRow) {
	t.Helper()
	c := testsupport.Compile(t, matchDecls+src)
	for _, row := range rows {
		m, _, errOut := machineOf(c)
		var v interp.Value
		var ok bool
		if got := panicText(func() { v, ok = m.Call(testsupport.FunNamed(t, c, row.fn), nil) }); got != nil {
			t.Errorf("%s: Call panicked in Go with %v", row.fn, got)
			continue
		}
		if !ok || v != row.want {
			t.Errorf("%s: Call = (%#v, %v), want (%#v, true); stderr %q", row.fn, v, ok, row.want, errOut.String())
		}
	}
}

// matchRow is the function fn, which takes no arguments, and the value it returns.
type matchRow struct {
	fn   string
	want interp.Value
}

func TestMatchRun(t *testing.T) {
	t.Run("arms_take_values_apart", func(t *testing.T) {
		// One function for each case: constructors with and without fields, nested
		// ones, Int and Bool literals (a negative one too), tuples, records written
		// out of order with `..`, and a `let` with a pattern. The values are the ones
		// that the pattern's variables are bound to, so a variable bound to the wrong
		// field, or in the wrong slot, gives another number.
		matchRows(t, `
fn area(s: Shape) -> Float {
  match s {
    Circle(r) => r * r
    Rect(w, h) => w * h
    Unit => 0.0
  }
}
fn circle() -> Float { area(Circle(2.0)) }
fn rect() -> Float { area(Rect(2.0, 3.0)) }
fn unit() -> Float { area(Unit) }

fn depth(n: Nest) -> Int {
  match n {
    Node(Node(Leaf, a), b) => a * 10 + b
    Node(Leaf, a) => a
    Node(_, _) => 99
    Leaf => 0
  }
}
fn nested() -> Int { depth(Node(Node(Leaf, 4), 2)) }
fn shallow() -> Int { depth(Node(Leaf, 7)) }
fn deeper() -> Int { depth(Node(Node(Node(Leaf, 1), 1), 1)) }
fn leaf() -> Int { depth(Leaf) }

fn lit(n: Int) -> Int {
  match n {
    0 => 10
    -1 => 20
    k => k
  }
}
fn zero() -> Int { lit(0) }
fn minus_one() -> Int { lit(-1) }
fn other() -> Int { lit(5) }

fn flag(b: Bool) -> Int {
  match b {
    true => 1
    false => 2
  }
}
fn yes() -> Int { flag(true) }
fn no() -> Int { flag(false) }

fn pair(n: Int, b: Bool) -> Int {
  match (n, b) {
    (0, true) => 1
    (0, _) => 2
    (k, _) => k
  }
}
fn tuple_first() -> Int { pair(0, true) }
fn tuple_second() -> Int { pair(0, false) }
fn tuple_third() -> Int { pair(8, true) }

fn rec(p: Pt) -> Int {
  match p {
    Pt { z, x: 0, .. } => z
    Pt { x, y, z } => x * 100 + y * 10 + z
  }
}
fn record_first() -> Int { rec(Pt { x: 0, y: 5, z: 9 }) }
fn record_second() -> Int { rec(Pt { x: 1, y: 2, z: 3 }) }

fn let_tuple() -> Int {
  let (a, b) = (1, 2)
  a * 10 + b
}
fn let_record() -> Int {
  let Pt { y, .. } = Pt { x: 1, y: 2, z: 3 }
  y
}
fn let_nested() -> Int {
  let (a, (b, c)) = (1, (2, 3))
  a * 100 + b * 10 + c
}
`, []matchRow{
			{"circle", interp.FloatV{V: 4}}, {"rect", interp.FloatV{V: 6}}, {"unit", interp.FloatV{V: 0}},
			{"nested", interp.IntV{V: 42}}, {"shallow", interp.IntV{V: 7}}, {"deeper", interp.IntV{V: 99}}, {"leaf", interp.IntV{V: 0}},
			{"zero", interp.IntV{V: 10}}, {"minus_one", interp.IntV{V: 20}}, {"other", interp.IntV{V: 5}},
			{"yes", interp.IntV{V: 1}}, {"no", interp.IntV{V: 2}},
			{"tuple_first", interp.IntV{V: 1}}, {"tuple_second", interp.IntV{V: 2}}, {"tuple_third", interp.IntV{V: 8}},
			{"record_first", interp.IntV{V: 9}}, {"record_second", interp.IntV{V: 123}},
			{"let_tuple", interp.IntV{V: 12}}, {"let_record", interp.IntV{V: 2}}, {"let_nested", interp.IntV{V: 123}},
		})
	})

	t.Run("guards_fall_through_to_the_next_arm", func(t *testing.T) {
		// An arm whose pattern fits but whose guard is false does not win: the match
		// goes on with the arm after it. The guard sees the variables of the pattern.
		matchRows(t, `
fn rank(n: Int) -> Int {
  match n {
    a if a > 10 => 1
    b if b > 5 => 2
    c => 3
  }
}
fn big() -> Int { rank(11) }
fn middle() -> Int { rank(7) }
fn small() -> Int { rank(3) }
fn edge() -> Int { rank(10) * 10 + rank(5) }

fn unwrap(o: Opt) -> Int {
  match o {
    Yes(x) if x > 0 => x
    Yes(_) => 0 - 1
    No => 0 - 2
  }
}
fn positive() -> Int { unwrap(Yes(4)) }
fn not_positive() -> Int { unwrap(Yes(0)) }
fn none() -> Int { unwrap(No) }
`, []matchRow{
			{"big", interp.IntV{V: 1}}, {"middle", interp.IntV{V: 2}}, {"small", interp.IntV{V: 3}}, {"edge", interp.IntV{V: 23}},
			{"positive", interp.IntV{V: 4}}, {"not_positive", interp.IntV{V: -1}}, {"none", interp.IntV{V: -2}},
		})

		// The guards run in order and only until one is true: each prints its number,
		// and the body of the arm that wins prints after them. A match that ran a
		// guard it did not need to, or the guards of all the arms, writes more lines.
		// The last two rows have a guard that is always false and or-patterns in a tuple:
		// each alternative is tried once, so x is 0 and then 1; and with an or-pattern in
		// each of two fields the rows are the combinations in the order they are
		// written, the second field changing first, so a*10+b is 12, 15, 42 and 45.
		for _, row := range []struct{ name, scrutinee, arms, want string }{
			{"two false and a fallback", "5", "_ if show(1, false) => 1\n    _ if show(2, false) => 2\n    _ => {\n      print(30)\n      3\n    }", "1\n2\n30\n"},
			{"the first is true", "5", "_ if show(1, true) => {\n      print(10)\n      1\n    }\n    _ if show(2, true) => 2\n    _ => 3", "1\n10\n"},
			{"the second is true", "5", "_ if show(1, false) => 1\n    _ if show(2, true) => {\n      print(20)\n      2\n    }\n    _ => 3", "1\n2\n20\n"},
			{"each alternative of an or-pattern once", "((Yes(0), Yes(1)), 5)", "(((Yes(x), _) | (_, Yes(x))), 5) if show(x, false) => 1\n    _ => {\n      print(30)\n      2\n    }", "0\n1\n30\n"},
			{"two or-patterns in one arm", "((Yes(1), Yes(4)), (Yes(2), Yes(5)))", "(((Yes(a), _) | (_, Yes(a))), ((Yes(b), _) | (_, Yes(b)))) if show(a * 10 + b, false) => 1\n    _ => {\n      print(30)\n      2\n    }", "12\n15\n42\n45\n30\n"},
		} {
			src := matchDecls + "fn show(n: Int, b: Bool) -> Bool {\n  print(n)\n  b\n}\nfn main() {\n  let r = match " + row.scrutinee + " {\n    " + row.arms + "\n  }\n}\n"
			if h, stdout, stderr := runMain(t, src); h != (interp.Halt{Code: 0}) || stdout != row.want || stderr != "" {
				t.Errorf("%s: RunMain() = %+v, stdout %q, stderr %q, want Halt{Code: 0}, stdout %q and nothing on stderr", row.name, h, stdout, stderr, row.want)
			}
		}
	})

	t.Run("or_patterns_try_each_alternative", func(t *testing.T) {
		// An or-pattern fits where one of its alternatives does, the first one that
		// does, and a guard that is false makes the match go on with the next
		// alternative of the same arm before it goes to the next arm. With an or-pattern
		// in each of three fields the rows are the combinations in the order they are
		// written, the last field changing first: the guard of `three` is true for
		// `c = 3` (after `c = 0`), and for `b = 2` only after it (a field after the
		// second that were matched in the other order would answer 20).
		matchRows(t, `
fn pick(p: (Opt, Opt)) -> Int {
  match p {
    (Yes(x), _) | (_, Yes(x)) if x > 0 => x
    _ => 100
  }
}
fn both_positive() -> Int { pick((Yes(2), Yes(3))) }
fn second_alternative() -> Int { pick((Yes(0), Yes(1))) }
fn only_the_second_fits() -> Int { pick((No, Yes(4))) }
fn no_alternative_passes() -> Int { pick((Yes(0), Yes(0))) }
fn the_first_only() -> Int { pick((Yes(0), No)) }
fn neither() -> Int { pick((No, No)) }

fn literals(n: Int) -> Int {
  match n {
    1 | 2 | 3 => 1
    4 | -5 => 2
    _ => 3
  }
}
fn alt_first() -> Int { literals(1) }
fn alt_middle() -> Int { literals(2) }
fn alt_last() -> Int { literals(3) }
fn alt_negative() -> Int { literals(-5) }
fn alt_none() -> Int { literals(9) }

fn inside(p: (Opt, Int)) -> Int {
  match p {
    (Yes(1 | 2), 3 | 4) => 1
    (Yes(_), _) => 2
    _ => 3
  }
}
fn inside_both() -> Int { inside((Yes(2), 4)) }
fn inside_first() -> Int { inside((Yes(5), 4)) }
fn inside_second() -> Int { inside((Yes(2), 5)) }
fn inside_none() -> Int { inside((No, 3)) }

fn after(p: ((Opt, Opt), Int)) -> Int {
  match p {
    (((Yes(x), _) | (_, Yes(x))), 5) if x > 0 => x
    _ => 100
  }
}
fn after_second() -> Int { after(((Yes(0), Yes(1)), 5)) }
fn after_rest_fails() -> Int { after(((Yes(0), Yes(1)), 6)) }
fn after_first() -> Int { after(((Yes(2), Yes(1)), 5)) }

fn order(p: (Opt, Opt)) -> Int {
  match p {
    (Yes(a), Yes(b)) | (Yes(b), Yes(a)) if a > b => a * 10 + b
    _ => 0
  }
}
fn order_first() -> Int { order((Yes(2), Yes(1))) }
fn order_swapped() -> Int { order((Yes(1), Yes(2))) }
fn order_equal() -> Int { order((Yes(1), Yes(1))) }

fn two(p: ((Opt, Opt), (Opt, Opt))) -> Int {
  match p {
    ((Yes(a), _) | (_, Yes(a)), (Yes(b), _) | (_, Yes(b))) if a + b > 5 => a * 10 + b
    _ => 0
  }
}
fn two_or_patterns() -> Int { two(((Yes(1), Yes(4)), (Yes(2), Yes(5)))) }

fn three(p: ((Opt, Opt), (Opt, Opt), (Opt, Opt))) -> Int {
  match p {
    ((Yes(a), _) | (_, Yes(a)), (Yes(b), _) | (_, Yes(b)), (Yes(c), _) | (_, Yes(c))) if b + c >= 2 => a * 100 + b * 10 + c
    _ => 0 - 1
  }
}
fn three_or_patterns() -> Int { three(((Yes(0), Yes(1)), (Yes(0), Yes(2)), (Yes(0), Yes(3)))) }
`, []matchRow{
			{"both_positive", interp.IntV{V: 2}}, {"second_alternative", interp.IntV{V: 1}}, {"only_the_second_fits", interp.IntV{V: 4}},
			{"no_alternative_passes", interp.IntV{V: 100}}, {"the_first_only", interp.IntV{V: 100}}, {"neither", interp.IntV{V: 100}},
			{"alt_first", interp.IntV{V: 1}}, {"alt_middle", interp.IntV{V: 1}}, {"alt_last", interp.IntV{V: 1}}, {"alt_negative", interp.IntV{V: 2}}, {"alt_none", interp.IntV{V: 3}},
			{"inside_both", interp.IntV{V: 1}}, {"inside_first", interp.IntV{V: 2}}, {"inside_second", interp.IntV{V: 2}}, {"inside_none", interp.IntV{V: 3}},
			{"after_second", interp.IntV{V: 1}}, {"after_rest_fails", interp.IntV{V: 100}}, {"after_first", interp.IntV{V: 2}},
			{"order_first", interp.IntV{V: 21}}, {"order_swapped", interp.IntV{V: 21}}, {"order_equal", interp.IntV{V: 0}},
			{"two_or_patterns", interp.IntV{V: 15}}, {"three_or_patterns", interp.IntV{V: 3}},
		})
	})

	t.Run("a_match_does_not_grow_the_stack", func(t *testing.T) {
		// The body of the arm that wins runs with nothing of the match left on the
		// stack, guard or not, so a call there is still in tail position: a loop of a
		// hundred thousand iterations goes through the same frames and environments
		// as one of ten. `count` recurses from an arm that has a guard, and `down` from
		// one that has none, which is the path where no frame is pushed at all. The steps
		// of the longer loop are at least a thousand times those of the shorter one (the
		// counts are about 9 500 times more, which no test pins).
		const src = "fn count(n: Int, acc: Int) -> Int {\n  match n {\n    0 => acc\n    k if k > 0 => count(k - 1, acc + k)\n    _ => acc\n  }\n}\n" +
			"fn down(n: Int, acc: Int) -> Int {\n  match n {\n    0 => acc\n    _ => down(n - 1, acc + 1)\n  }\n}\n" +
			"fn few() -> Int { count(10, 0) }\nfn many() -> Int { count(100000, 0) }\n" +
			"fn few_down() -> Int { down(10, 0) }\nfn many_down() -> Int { down(100000, 0) }\n"
		for _, loop := range []struct {
			name, few, many   string
			wantFew, wantMany int64
		}{
			{"a guarded arm", "few", "many", 55, 5000050000},
			{"an arm with no guard", "few_down", "many_down", 10, 100000},
		} {
			var stats [2]interp.Stats
			for i, row := range []struct {
				fn   string
				want int64
			}{{loop.few, loop.wantFew}, {loop.many, loop.wantMany}} {
				m, v := call(t, src, row.fn)
				if v != (interp.IntV{V: row.want}) {
					t.Errorf("%s: %s() = %#v, want IntV{%d}", loop.name, row.fn, v, row.want)
				}
				stats[i] = m.Stats()
			}
			if stats[0].MaxDepth != stats[1].MaxDepth || stats[0].MaxEnvs != stats[1].MaxEnvs || stats[1].Steps < 1000*stats[0].Steps {
				t.Errorf("%s: the loop of 10 had %+v and the loop of 100000 had %+v, want the same MaxDepth and MaxEnvs and a thousandfold more steps", loop.name, stats[0], stats[1])
			}
		}
	})

	t.Run("a_guard_is_one_frame_and_a_few_steps", func(t *testing.T) {
		// `match 7 { x if x > 5 => x; _ => 0 }`, counted by hand: the Match, its
		// scrutinee, the matchFrame (the arm fits and the guard starts under a
		// guardFrame), the guard `x > 5` (the PrimCall, `x`, a primFrame, `5`, a
		// primFrame), the guardFrame (it is true, and the body is the control) and the
		// body `x`: ten steps, with the guardFrame and the primFrame at once on the
		// stack. With the guard false, the second arm's body is the tenth step in the
		// place of `x`.
		for _, row := range []struct {
			scrutinee string
			want      int64
		}{{"7", 7}, {"3", 0}} {
			src := "fn f() -> Int { match " + row.scrutinee + " { x if x > 5 => x\n _ => 0 } }"
			m, v := call(t, src, "f")
			if v != (interp.IntV{V: row.want}) {
				t.Errorf("match %s: f() = %#v, want IntV{%d}", row.scrutinee, v, row.want)
			}
			if s, want := m.Stats(), (interp.Stats{Steps: 10, MaxDepth: 2, MaxEnvs: 1}); s != want {
				t.Errorf("match %s: Stats() = %+v, want %+v", row.scrutinee, s, want)
			}
		}
	})

	t.Run("a_match_that_falls_through_after_a_guard_panics", func(t *testing.T) {
		// Made by hand, because the checker does not accept a match that leaves a
		// value out: one arm, `_ if false => 1`. The pattern fits, the guard is false,
		// no choice and no arm is left, and the machine panics in Go with the same text
		// as for a pattern that does not fit.
		p := &core.Program{}
		b := core.Builder{P: p}
		sp := base.Span{File: 1}
		arm := core.Arm{Pat: b.Pat(core.Pat{Kind: core.PatWildcard, Span: sp}), Guard: b.LitBool(sp, false), HasGuard: true, Body: b.LitInt(sp, 1)}
		f := p.Funs.Push(core.Fun{Body: b.Match(sp, b.LitInt(sp, 0), []core.Arm{arm}), HasBody: true})
		got := panicText(func() {
			interp.New(p, new(base.SourceManager), new(bytes.Buffer), new(bytes.Buffer)).Call(f, nil)
		})
		if got != "match fell through" {
			t.Errorf("Call panicked with %#v, want the string \"match fell through\"", got)
		}
	})

	t.Run("or_patterns_of_one_alternative_and_none", func(t *testing.T) {
		// The parser never makes an or-pattern of fewer than two alternatives, but a
		// Program made by hand can, and the machine reads it as it is written: with
		// one alternative it fits what that one fits, and a false guard goes on to
		// the next arm (there is no second alternative to try); with none it fits
		// nothing. Each row is `match 1 { <pattern> [if false] => 10; _ => 20 }`.
		for _, row := range []struct {
			name       string
			alts       []int64
			falseGuard bool
			want       int64
		}{
			{"one alternative that fits", []int64{1}, false, 10},
			{"one alternative that does not", []int64{2}, false, 20},
			{"one alternative that fits, with a false guard", []int64{1}, true, 20},
			{"no alternatives", nil, false, 20},
		} {
			p := &core.Program{}
			b := core.Builder{P: p}
			sp := base.Span{File: 1}
			var alts []core.PatID
			for _, n := range row.alts {
				alts = append(alts, b.Pat(core.Pat{Kind: core.PatInt, Span: sp, Int: n}))
			}
			or := b.Pat(core.Pat{Kind: core.PatOr, Span: sp, Args: base.AppendRange(&p.PatPool, alts)})
			first := core.Arm{Pat: or, Body: b.LitInt(sp, 10)}
			if row.falseGuard {
				first.Guard, first.HasGuard = b.LitBool(sp, false), true
			}
			second := core.Arm{Pat: b.Pat(core.Pat{Kind: core.PatWildcard, Span: sp}), Body: b.LitInt(sp, 20)}
			f := p.Funs.Push(core.Fun{HasBody: true, Body: b.Match(sp, b.LitInt(sp, 1), []core.Arm{first, second})})
			var got interp.Value
			var ok bool
			if r := panicText(func() {
				got, ok = interp.New(p, new(base.SourceManager), new(bytes.Buffer), new(bytes.Buffer)).Call(f, nil)
			}); r != nil {
				t.Errorf("%s: Call panicked with %#v", row.name, r)
			} else if !ok || got != (interp.IntV{V: row.want}) {
				t.Errorf("%s: Call = (%#v, %v), want (IntV{%d}, true)", row.name, got, ok, row.want)
			}
		}
	})

	t.Run("big_patterns_without_recursion", func(t *testing.T) {
		// A tuple pattern of a hundred thousand elements, an or-pattern of a hundred
		// thousand alternatives (the last one is the one that fits) and a constructor
		// nested a hundred thousand levels deep in the pattern and in the value, which
		// no source can write but a Program made by hand can, are matched with the stack
		// of Go capped at 1 MiB: a recursion over the pattern or the value would not
		// fit. The matcher keeps its own stack.
		const n = 100_000
		old := debug.SetMaxStack(1 << 20)
		defer debug.SetMaxStack(old)

		ones := strings.TrimSuffix(strings.Repeat("1, ", n), ", ")
		wild := strings.TrimSuffix(strings.Repeat("_, ", n), ", ")
		var alts []string
		for i := range n {
			alts = append(alts, strconv.Itoa(i))
		}
		src := "fn tuple() -> Int {\n  match (" + ones + ") {\n    (" + wild + ") => 1\n  }\n}\n" +
			"fn last(x: Int) -> Int {\n  match x {\n    " + strings.Join(alts, " | ") + " => 1\n    _ => 2\n  }\n}\n" +
			"fn fits() -> Int { last(99999) }\nfn misses() -> Int { last(100000) }\n"
		for _, row := range []struct {
			fn   string
			want int64
		}{{"tuple", 1}, {"fits", 1}, {"misses", 2}} {
			if _, v := call(t, src, row.fn); v != (interp.IntV{V: row.want}) {
				t.Errorf("%s() = %#v, want IntV{%d}", row.fn, v, row.want)
			}
		}

		p := &core.Program{}
		b := core.Builder{P: p}
		sp := base.Span{File: 1}
		wrap := p.Ctors.Push(core.Ctor{Arity: 1})
		x := p.Vars.Push(core.Var{Slot: 0})
		value := b.LitInt(sp, 42)
		pat := b.Pat(core.Pat{Kind: core.PatBind, Span: sp, Var: x})
		for range n {
			value = b.Con(sp, wrap, []core.ExprID{value})
			pat = b.Pat(core.Pat{Kind: core.PatCon, Span: sp, Ctor: wrap, Args: base.AppendRange(&p.PatPool, []core.PatID{pat})})
		}
		f := p.Funs.Push(core.Fun{HasBody: true, FrameSize: 1,
			Body: b.Match(sp, value, []core.Arm{{Pat: pat, Body: b.VarRef(sp, x)}})})
		var out, errOut bytes.Buffer
		got, ok := interp.New(p, new(base.SourceManager), &out, &errOut).Call(f, nil)
		if !ok || got != (interp.IntV{V: 42}) {
			t.Errorf("a pattern %d levels deep: Call = (%#v, %v), want (IntV{42}, true); stderr %q", n, got, ok, errOut.String())
		}
	})
}
