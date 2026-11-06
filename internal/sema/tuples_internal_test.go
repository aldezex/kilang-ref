package sema

import (
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/testsupport/within"
)

// The tests here build tuple types by hand, as the other internal tests of the
// package do, and are about the table (Tuple, Show, Occurs) and Unify.
// They reuse unifyEnv, namedType, distinctIDs, panics and bind.

func TestTuples(t *testing.T) {
	t.Run("hash_cons", func(t *testing.T) {
		// Tuple builds each tuple once, as Fun and Data do: the same elements in the
		// same order give the same id, and any difference gives another. The ids go
		// in as four bytes each, so [1, 12] and [11, 2] are two tuples, and the table
		// keeps its own copy of the elements. A tuple has two or more of them.
		tbl := NewTypeTable()
		intT, boolT := tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinBool)
		x, y := tbl.FreshVar(Origin{}), tbl.FreshVar(Origin{})
		elems := []TypeID{intT, boolT}
		pair := tbl.Tuple(elems)
		elems[0] = boolT // the caller reuses its slice
		all := []namedType{
			{"(Int, Bool)", pair}, {"(Bool, Int)", tbl.Tuple([]TypeID{boolT, intT})}, {"(Int, Int)", tbl.Tuple([]TypeID{intT, intT})},
			{"(Int, Bool, Bool)", tbl.Tuple([]TypeID{intT, boolT, boolT})}, {"(x, y)", tbl.Tuple([]TypeID{x, y})},
			{"(y, x)", tbl.Tuple([]TypeID{y, x})}, {"((Int, Bool), Int)", tbl.Tuple([]TypeID{pair, intT})},
			{"(Int, (Int, Bool))", tbl.Tuple([]TypeID{intT, pair})},
			{"the function () -> (Int, Bool)", tbl.Fun(nil, pair, tbl.EmptyRow())},
			{"the declared type 0 applied to (Int, Bool)", tbl.Data(0, []TypeID{pair})},
		}
		distinctIDs(t, all)
		if got := tbl.Tuple([]TypeID{intT, boolT}); got != pair {
			t.Errorf("Tuple([Int, Bool]) a second time = %d, want %d: the table copies the elements it is given", got, pair)
		}
		if n := tbl.Node(pair); n.Kind != TypeTuple || !slices.Equal(n.Params, []TypeID{intT, boolT}) || n.Args != nil {
			t.Errorf("the node of (Int, Bool) is %+v, want TypeTuple with the elements Int and Bool", n)
		}
		// Two elements the ids of which would read the same side by side in decimal.
		var ids []TypeID
		for range 13 {
			ids = append(ids, tbl.FreshVar(Origin{}))
		}
		if tbl.Tuple([]TypeID{ids[1], ids[12]}) == tbl.Tuple([]TypeID{ids[11], ids[2]}) {
			t.Error("the tuples of the ids [1, 12] and [11, 2] are the same type")
		}
		for _, n := range []int{0, 1} {
			if !panics(func() { tbl.Tuple(make([]TypeID, n)) }) {
				t.Errorf("Tuple of %d elements did not panic: a tuple has two or more", n)
			}
		}
		if panics(func() { tbl.Tuple([]TypeID{intT, intT}) }) {
			t.Error("Tuple of two elements panicked")
		}
	})

	t.Run("show", func(t *testing.T) {
		// A tuple is its elements between parentheses, separated by `, `, whatever
		// they are; the unbound variables are named a, b… across the whole type.
		tbl := NewTypeTable()
		i, b, u, bad := tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinBool), tbl.Builtin(BuiltinUnit), tbl.Error()
		x, y := tbl.FreshVar(Origin{}), tbl.FreshVar(Origin{})
		tbl.nameDef(0, "Box")
		pair := tbl.Tuple([]TypeID{i, b})
		for _, row := range []struct {
			name string
			id   TypeID
			want string
		}{
			{"pair", pair, "(Int, Bool)"},
			{"three", tbl.Tuple([]TypeID{i, b, u}), "(Int, Bool, ())"},
			{"nested", tbl.Tuple([]TypeID{pair, tbl.Tuple([]TypeID{u, pair})}), "((Int, Bool), ((), (Int, Bool)))"},
			{"a function in it", tbl.Tuple([]TypeID{tbl.Fun([]TypeID{i}, b, tbl.EmptyRow()), i}), "((Int) -> Bool, Int)"},
			{"a function of tuples", tbl.Fun([]TypeID{pair}, tbl.Tuple([]TypeID{b, b}), tbl.EmptyRow()), "((Int, Bool)) -> (Bool, Bool)"},
			{"a declared type holding one", tbl.Data(0, []TypeID{pair}), "Box<(Int, Bool)>"},
			{"variables", tbl.Tuple([]TypeID{x, y, x}), "(a, b, a)"},
			{"the error type", tbl.Tuple([]TypeID{bad, i}), "({error}, Int)"},
		} {
			if got := tbl.Show(row.id); got != row.want {
				t.Errorf("%s: Show = %q, want %q", row.name, got, row.want)
			}
		}
		// A tuple that is too long to write is cut, with the mark, like any type, and
		// a very deep one ends too: Show goes down with a stack of its own.
		wide := make([]TypeID, 200)
		for k := range wide {
			wide[k] = i
		}
		deep := pair
		for range 100000 {
			deep = tbl.Tuple([]TypeID{deep, i})
		}
		var wideText, deepText string
		within.Run(t, "Show of a tuple of 200 elements and of one nested 100 000 times", func() {
			wideText, deepText = tbl.Show(tbl.Tuple(wide)), tbl.Show(deep)
		})
		for name, text := range map[string]string{"200 elements": wideText, "100 000 levels": deepText} {
			if !strings.HasSuffix(text, cutMark) || len(text) > MaxTypeText+len(cutMark) {
				t.Errorf("Show of a tuple of %s is %d bytes ending %q, want at most %d ending with the cut mark", name, len(text), text[max(0, len(text)-8):], MaxTypeText+len(cutMark))
			}
		}
	})

	t.Run("occurs_through_tuples", func(t *testing.T) {
		// A variable inside a tuple, at any level, is found; one that is not there is
		// not. A type of 40 levels in which each level holds the one below twice is
		// walked once per type and not once per path, or the walk would be 2^40.
		tbl := NewTypeTable()
		i := tbl.Builtin(BuiltinInt)
		x, y := tbl.FreshVar(Origin{}), tbl.FreshVar(Origin{})
		vx, vy := tbl.Node(x).Var, tbl.Node(y).Var
		in := tbl.Tuple([]TypeID{i, tbl.Tuple([]TypeID{i, x})})
		if !tbl.Occurs(vx, in) || tbl.Occurs(vy, in) {
			t.Errorf("Occurs(x, (Int, (Int, x))) = %v and Occurs(y, ...) = %v, want true and false", tbl.Occurs(vx, in), tbl.Occurs(vy, in))
		}
		twice := func(bottom TypeID) TypeID {
			ty := bottom
			for range 40 {
				ty = tbl.Tuple([]TypeID{ty, ty})
			}
			return ty
		}
		var withX, withoutX bool
		within.Run(t, "Occurs on the tuples of 40 levels", func() {
			withX, withoutX = tbl.Occurs(vx, twice(x)), tbl.Occurs(vx, twice(y))
		})
		if !withX || withoutX {
			t.Errorf("Occurs(x) in 40 levels over x = %v and over y = %v, want true and false", withX, withoutX)
		}
	})

	t.Run("unify", func(t *testing.T) {
		// Two tuples are unified element by element, in order, so a variable among
		// them is bound by the element at its place and the first element that
		// does not match is the mistake, with what was bound before it left bound.
		// Tuples of other lengths, and a tuple and anything else, do not match; the
		// label writes both types whole.
		e := newUnifyEnv()
		i, b, u := e.intT, e.boolT, e.unitT
		tup := func(elems ...TypeID) TypeID { return e.tbl.Tuple(elems) }

		e.check(t, "(Int, Bool) ~ (Int, Bool)", tup(i, b), tup(i, b), annotated, true)
		e.badAnnotated(t, "(Int, Bool) ~ (Int, Int)", tup(i, b), tup(i, i), "expected `(Int, Bool)`, found `(Int, Int)`")
		e.badAnnotated(t, "(Int, Bool) ~ (Bool, Bool)", tup(i, b), tup(b, b), "expected `(Int, Bool)`, found `(Bool, Bool)`")
		e.badAnnotated(t, "(Int, Bool) ~ (Bool, Int)", tup(i, b), tup(b, i), "expected `(Int, Bool)`, found `(Bool, Int)`")
		// The shorter is a prefix of the longer in both rows: a comparison that stops
		// at the shorter would take them for equal.
		e.badAnnotated(t, "(Int, Bool) ~ (Int, Bool, ())", tup(i, b), tup(i, b, u), "expected `(Int, Bool)`, found `(Int, Bool, ())`")
		e.badAnnotated(t, "(Int, Bool, ()) ~ (Int, Bool)", tup(i, b, u), tup(i, b), "expected `(Int, Bool, ())`, found `(Int, Bool)`")
		e.badAnnotated(t, "(Int, Bool) ~ Int", tup(i, b), i, "expected `(Int, Bool)`, found `Int`")
		e.badAnnotated(t, "Int ~ (Int, Bool)", i, tup(i, b), "expected `Int`, found `(Int, Bool)`")
		e.badAnnotated(t, "(Int, Bool) ~ ()", tup(i, b), u, "expected `(Int, Bool)`, found `()`")
		e.badAnnotated(t, "(Int, Bool) ~ a function", tup(i, b), e.fn(i), "expected `(Int, Bool)`, found `() -> Int`")
		e.badAnnotated(t, "a function ~ (Int, Bool)", e.fn(i), tup(i, b), "expected `() -> Int`, found `(Int, Bool)`")
		// A function of as many parameters as the tuple has elements is no tuple: the
		// parts are not compared, whatever they are.
		e.badAnnotated(t, "(Int, Bool) ~ a function of two parameters", tup(i, b), e.fn(u, i, b), "expected `(Int, Bool)`, found `(Int, Bool) -> ()`")
		e.badAnnotated(t, "a function of two parameters ~ (Int, Bool)", e.fn(u, i, b), tup(i, b), "expected `(Int, Bool) -> ()`, found `(Int, Bool)`")

		// A variable among the elements is bound by the element at its place, at any
		// level.
		x, y := e.fresh(), e.fresh()
		e.check(t, "(a, Int) ~ (Bool, Int)", tup(x, i), tup(b, i), annotated, true)
		if got := e.tbl.Resolve(x); got != b {
			t.Errorf("(a, Int) ~ (Bool, Int): a is %d, want Bool (%d)", got, b)
		}
		x = e.fresh()
		e.check(t, "(Int, (a, b)) ~ (Int, (Bool, ()))", tup(i, tup(x, y)), tup(i, tup(b, u)), annotated, true)
		if e.tbl.Resolve(x) != b || e.tbl.Resolve(y) != u {
			t.Errorf("(Int, (a, b)) ~ (Int, (Bool, ())): a is %d and b is %d, want Bool (%d) and () (%d)", e.tbl.Resolve(x), e.tbl.Resolve(y), b, u)
		}
		x = e.fresh()
		e.badAnnotated(t, "(a, Int) ~ (Bool, Bool)", tup(x, i), tup(b, b), "expected `(Bool, Int)`, found `(Bool, Bool)`")
		if e.tbl.Resolve(x) != b {
			t.Errorf("(a, Int) ~ (Bool, Bool): a is %d afterwards, want Bool (%d), bound before the mismatch", e.tbl.Resolve(x), b)
		}
		// The same tuple twice on one side, against two different ones on the other,
		// is two pairs and not one: the second is compared although it shares its
		// first (or its second) tuple with the first, and is where the mismatch is.
		// The two orders are two rows, as the pair is told by both of its tuples.
		x, y = e.fresh(), e.fresh()
		same := tup(x, y)
		e.badAnnotated(t, "((a, b), (a, b)) ~ ((Int, Int), (Int, Bool))", tup(same, same), tup(tup(i, i), tup(i, b)),
			"expected `((Int, Int), (Int, Int))`, found `((Int, Int), (Int, Bool))`")
		x, y = e.fresh(), e.fresh()
		same = tup(x, y)
		e.badAnnotated(t, "((Int, Int), (Int, Bool)) ~ ((a, b), (a, b))", tup(tup(i, i), tup(i, b)), tup(same, same),
			"expected `((Int, Int), (Int, Bool))`, found `((Int, Int), (Int, Int))`")
		// A variable is bound to a tuple as to any other type, and one that would
		// hold itself is E0405.
		x = e.fresh()
		e.check(t, "a ~ (Int, Bool)", x, tup(i, b), annotated, true)
		if got := e.tbl.Show(x); got != "(Int, Bool)" {
			t.Errorf("a ~ (Int, Bool): a shows as %q, want (Int, Bool)", got)
		}
		x = e.fresh()
		e.check(t, "a ~ (Int, a)", x, tup(i, x), annotated, false,
			wantInfinite("`a` would have to be `(Int, a)`, which has no finite size"))
		if !e.isUnbound(x) {
			t.Error("a ~ (Int, a): a was bound")
		}
		// The error type inside is silent and binds nothing.
		e.check(t, "(Int, {error}) ~ (Int, Bool)", tup(i, e.tbl.Error()), tup(i, b), annotated, true)
		e.check(t, "{error} ~ (Int, Bool)", e.tbl.Error(), tup(i, b), annotated, true)

		// Two tuples of 40 levels that hold the one below twice are taken apart once
		// per pair, not once per path (2^40), whether they differ at the bottom or not.
		twice := func(bottom TypeID) TypeID {
			ty := bottom
			for range 40 {
				ty = e.tbl.Tuple([]TypeID{ty, ty})
			}
			return ty
		}
		endA, endB := e.fresh(), e.fresh()
		onA, onB := twice(endA), twice(endB)
		bool40, int40 := twice(b), twice(i)
		var okVars, okBad bool
		var gotVars, gotBad []diag.Diagnostic
		within.Run(t, "Unify of two tuples of 40 levels", func() {
			okVars, gotVars = e.unify(onA, onB, annotated)
			okBad, gotBad = e.unify(int40, bool40, annotated)
		})
		if !okVars || len(gotVars) != 0 || e.tbl.Resolve(endA) != e.tbl.Resolve(endB) {
			t.Errorf("two tuples of 40 levels that end in two variables: Unify returned %v with %d diagnostics, want true with none and the variables made one", okVars, len(gotVars))
		}
		if okBad || len(gotBad) != 1 || gotBad[0].Code != diag.E(401) {
			t.Errorf("two tuples of 40 levels that differ at the bottom: Unify returned %v with %+v, want false and one E0401", okBad, gotBad)
		}
	})
}
