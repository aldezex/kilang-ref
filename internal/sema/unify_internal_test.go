package sema

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/testsupport/within"
)

// The tests here build their types by hand with a TypeTable and read the
// table's own record of a variable (to see whether a failed Unify left it
// unbound), which is not exported. So, like types_internal_test.go, this file
// is part of package sema and does not use testsupport; it imports only
// testsupport/within, which imports nothing of the compiler.

// Two spans for every test: where the unifier is told the found type is, and
// the place that explains why the expected type is expected. They differ, so
// a diagnostic that swaps them is not equal to the one the test wants.
var (
	unifyFoundAt = base.Span{File: 1, Lo: 40, Hi: 45}
	unifyBecause = base.Span{File: 1, Lo: 7, Hi: 12}
)

// unifyEnv is what a subtest of TestUnify starts from: a table with the three
// builtin types at hand. Each Unify call of a test gets a sink of its own, so
// a row reads exactly what its call reported.
type unifyEnv struct {
	tbl                *TypeTable
	intT, boolT, unitT TypeID
}

func newUnifyEnv() *unifyEnv {
	tbl := NewTypeTable()
	return &unifyEnv{
		tbl:   tbl,
		intT:  tbl.Builtin(BuiltinInt),
		boolT: tbl.Builtin(BuiltinBool),
		unitT: tbl.Builtin(BuiltinUnit),
	}
}

// fn returns the function type with these parameters and result and the empty
// effect row. The result comes first, as it does in a signature read from the
// right: fn(Int, Bool) is `(Bool) -> Int`.
func (e *unifyEnv) fn(ret TypeID, params ...TypeID) TypeID {
	return e.tbl.Fun(params, ret, e.tbl.EmptyRow())
}

// fresh returns a new unbound variable.
func (e *unifyEnv) fresh() TypeID { return e.tbl.FreshVar(Origin{}) }

// isUnbound reports whether the type id is a variable that nothing has bound.
func (e *unifyEnv) isUnbound(id TypeID) bool {
	n := e.tbl.Node(id)
	return n.Kind == TypeVariable && !e.tbl.vars.Get(n.Var).IsBound
}

// unify calls Unify with a new sink and returns what it answered and every
// diagnostic it reported.
func (e *unifyEnv) unify(expected, found TypeID, r Reason) (bool, []diag.Diagnostic) {
	sink := &diag.Sink{}
	ok := (&Unifier{Types: e.tbl, Sink: sink}).Unify(expected, found, unifyFoundAt, r)
	return ok, slices.Clone(sink.All())
}

// check fails the test unless Unify(expected, found, ...) answers wantOK and
// reports exactly the diagnostics in want, in that order, field by field: the
// message, the labels and their spans, the notes, the help and the fix-its.
func (e *unifyEnv) check(t *testing.T, row string, expected, found TypeID, r Reason, wantOK bool, want ...diag.Diagnostic) {
	t.Helper()
	ok, got := e.unify(expected, found, r)
	if ok != wantOK {
		t.Errorf("%s: Unify returned %v, want %v", row, ok, wantOK)
	}
	if len(got) != len(want) {
		t.Errorf("%s: reported %d diagnostics, want %d: %+v", row, len(got), len(want), got)
		return
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("%s: diagnostic %d is\n\t%+v\nwant\n\t%+v", row, i, got[i], want[i])
		}
	}
}

// badAnnotated fails the test unless Unify(expected, found, ...) with the reason
// of an annotated `let` answers false and reports exactly one E0401 whose
// primary label says label, and whose secondary label is the reason's.
func (e *unifyEnv) badAnnotated(t *testing.T, row string, expected, found TypeID, label string) {
	t.Helper()
	e.check(t, row, expected, found, annotated, false,
		withBecause(wantMismatch(label), annotatedLabel))
}

// wantMismatch is the E0401 that a failed Unify reports: its primary label is
// on unifyFoundAt and says label.
func wantMismatch(label string) diag.Diagnostic {
	return diag.NewError(diag.E(401), "mismatched types", diag.Label{Span: unifyFoundAt, Message: label})
}

// withBecause adds the secondary label of the reason, on unifyBecause.
func withBecause(d diag.Diagnostic, text string) diag.Diagnostic {
	return d.WithSecondary(diag.Label{Span: unifyBecause, Message: text})
}

// wantInfinite is the E0405 that a failed occurs check reports.
func wantInfinite(note string) diag.Diagnostic {
	return diag.NewError(diag.E(405), "cannot construct an infinite type",
		diag.Label{Span: unifyFoundAt, Message: "this type would contain itself"}).WithNote(note)
}

// annotated is the reason of a `let` with an annotation, on unifyBecause, and
// annotatedLabel the text of its secondary label.
var annotated = Reason{Kind: ReasonLetAnnotation, Because: unifyBecause}

const annotatedLabel = "expected because of this annotation"

// The help that a value left at the tail of a body that returns `()` gets: the
// first where returning the value would mend the mistake (Reason.CanReturn),
// the second where only binding it would. check_test.go, which is in another
// package, writes the same two texts out for its own rows: each test pins the
// words, so a change to them fails both.
const (
	unusedHelp     = "this value is unused: bind it with `let name = …`, or return it from the function"
	unusedBindHelp = "this value is unused: bind it with `let name = …`"
)

// panics reports whether f panics.
func panics(f func()) (did bool) {
	defer func() { did = recover() != nil }()
	f()
	return false
}

func TestUnify(t *testing.T) {
	t.Run("same_type_ok", func(t *testing.T) {
		e := newUnifyEnv()
		r := annotated
		e.check(t, "Int and Int", e.intT, e.intT, r, true)
		e.check(t, "Bool and Bool", e.boolT, e.boolT, r, true)
		e.check(t, "() and ()", e.unitT, e.unitT, r, true)
		e.check(t, "the same function", e.fn(e.boolT, e.intT), e.fn(e.boolT, e.intT), r, true)

		// A variable and itself: Occurs(a, a) is true, so a unifier that went on
		// to the occurs check, or that bound a to a, would report an infinite
		// type or build a binding that Resolve never leaves.
		a := e.fresh()
		e.check(t, "a variable and itself", a, a, r, true)
		if !e.isUnbound(a) {
			t.Errorf("a variable and itself: the variable is bound to %s", e.tbl.Show(e.tbl.Resolve(a)))
		}
		b := e.fresh()
		bind(t, e.tbl, b, a)
		e.check(t, "a variable and the variable it is bound to", b, a, r, true)
		e.check(t, "the same two, the other way", a, b, r, true)
		if !e.isUnbound(a) {
			t.Errorf("a variable and the variable it is bound to: a is bound to %s", e.tbl.Show(e.tbl.Resolve(a)))
		}

		// A variable that stands for Int is Int. Comparing the ids without
		// resolving them first would call these two different.
		c := e.fresh()
		bind(t, e.tbl, c, e.intT)
		e.check(t, "a bound variable and its type", c, e.intT, r, true)
		e.check(t, "a type and the variable bound to it", e.intT, c, r, true)

		// Two functions of the same shape that are two nodes of the table, because
		// the table compares the parameter ids as they come and one parameter is
		// a variable bound to Int.
		viaVar, direct := e.fn(e.intT, c), e.fn(e.intT, e.intT)
		if viaVar == direct {
			t.Fatalf("the test needs two nodes, and (c) -> Int and (Int) -> Int are both %d", viaVar)
		}
		e.check(t, "the same shape in two nodes", viaVar, direct, r, true)
		e.check(t, "the same shape in two nodes, the other way", direct, viaVar, r, true)
	})

	t.Run("var_binds_then_matches", func(t *testing.T) {
		e := newUnifyEnv()
		a := e.fresh()
		e.check(t, "a ~ Int", a, e.intT, annotated, true)
		if got := e.tbl.Resolve(a); got != e.intT {
			t.Errorf("a ~ Int: a resolves to %s, want Int", e.tbl.Show(got))
		}
		e.check(t, "a ~ Bool after a ~ Int", a, e.boolT, annotated, false,
			withBecause(wantMismatch("expected `Int`, found `Bool`"), annotatedLabel))
		if got := e.tbl.Resolve(a); got != e.intT {
			t.Errorf("a ~ Bool after a ~ Int: a resolves to %s, want it still Int", e.tbl.Show(got))
		}

		// The variable on the found side.
		b := e.fresh()
		e.check(t, "Bool ~ b", e.boolT, b, annotated, true)
		if got := e.tbl.Resolve(b); got != e.boolT {
			t.Errorf("Bool ~ b: b resolves to %s, want Bool", e.tbl.Show(got))
		}
		e.check(t, "Int ~ b after Bool ~ b", e.intT, b, annotated, false,
			withBecause(wantMismatch("expected `Int`, found `Bool`"), annotatedLabel))

		// Two variables become one: what binds either of them binds both.
		c, d := e.fresh(), e.fresh()
		e.check(t, "c ~ d", c, d, annotated, true)
		if e.tbl.Resolve(c) != e.tbl.Resolve(d) || !e.isUnbound(e.tbl.Resolve(c)) {
			t.Errorf("c ~ d: c resolves to %s and d to %s, want one unbound variable", e.tbl.Show(e.tbl.Resolve(c)), e.tbl.Show(e.tbl.Resolve(d)))
		}
		e.check(t, "d ~ Int after c ~ d", d, e.intT, annotated, true)
		if got := e.tbl.Resolve(c); got != e.intT {
			t.Errorf("d ~ Int after c ~ d: c resolves to %s, want Int", e.tbl.Show(got))
		}
		e.check(t, "c ~ Bool after d ~ Int", c, e.boolT, annotated, false,
			withBecause(wantMismatch("expected `Int`, found `Bool`"), annotatedLabel))

		// A variable can stand for a function type, and then it is that type.
		f := e.fresh()
		e.check(t, "f ~ (Int) -> Bool", f, e.fn(e.boolT, e.intT), annotated, true)
		if got := e.tbl.Show(f); got != "(Int) -> Bool" {
			t.Errorf("f ~ (Int) -> Bool: f shows as %q", got)
		}
		e.check(t, "f ~ Int after that", f, e.intT, annotated, false,
			withBecause(wantMismatch("expected `(Int) -> Bool`, found `Int`"), annotatedLabel))
	})

	t.Run("function_types_componentwise", func(t *testing.T) {
		e := newUnifyEnv()
		i, b := e.intT, e.boolT
		// The label always shows the two types that Unify was given, and not the
		// parts of them that differ.
		e.badAnnotated(t, "result differs", e.fn(b, i), e.fn(i, i), "expected `(Int) -> Bool`, found `(Int) -> Int`")
		e.badAnnotated(t, "parameter differs", e.fn(i, i), e.fn(i, b), "expected `(Int) -> Int`, found `(Bool) -> Int`")
		e.badAnnotated(t, "first of three parameters differs", e.fn(i, b, i, i), e.fn(i, i, i, i), "expected `(Bool, Int, Int) -> Int`, found `(Int, Int, Int) -> Int`")
		e.badAnnotated(t, "middle of three parameters differs", e.fn(i, i, b, i), e.fn(i, i, i, i), "expected `(Int, Bool, Int) -> Int`, found `(Int, Int, Int) -> Int`")
		e.badAnnotated(t, "last of three parameters differs", e.fn(i, i, i, b), e.fn(i, i, i, i), "expected `(Int, Int, Bool) -> Int`, found `(Int, Int, Int) -> Int`")
		e.badAnnotated(t, "parameter order differs", e.fn(i, i, b), e.fn(i, b, i), "expected `(Int, Bool) -> Int`, found `(Bool, Int) -> Int`")
		e.badAnnotated(t, "inside a parameter", e.fn(i, e.fn(i, i)), e.fn(i, e.fn(b, i)), "expected `((Int) -> Int) -> Int`, found `((Int) -> Bool) -> Int`")
		e.badAnnotated(t, "inside a result", e.fn(e.fn(i, i), i), e.fn(e.fn(b, i), i), "expected `(Int) -> (Int) -> Int`, found `(Int) -> (Int) -> Bool`")
		e.badAnnotated(t, "a function and Int", e.fn(i, i), i, "expected `(Int) -> Int`, found `Int`")
		e.badAnnotated(t, "Int and a function", i, e.fn(i, i), "expected `Int`, found `(Int) -> Int`")
		e.badAnnotated(t, "a function with no parameters and Int", e.fn(i), i, "expected `() -> Int`, found `Int`")
		e.badAnnotated(t, "Int and a function with no parameters", i, e.fn(i), "expected `Int`, found `() -> Int`")
		e.badAnnotated(t, "Int and Bool", i, b, "expected `Int`, found `Bool`")
		e.badAnnotated(t, "() and Int", e.unitT, i, "expected `()`, found `Int`")
		// Two parts differ, and the mistake is reported once, not once a part.
		e.badAnnotated(t, "two parameters and the result differ", e.fn(i, i, i), e.fn(b, b, b), "expected `(Int, Int) -> Int`, found `(Bool, Bool) -> Bool`")

		// Every part is unified, and what the variables stand for shows.
		a, c := e.fresh(), e.fresh()
		e.check(t, "variables take the parts of the other type", e.fn(a, a, c), e.fn(b, b, i), annotated, true)
		if got := e.tbl.Show(e.fn(a, a, c)); got != "(Bool, Int) -> Bool" {
			t.Errorf("variables take the parts of the other type: (a, c) -> a shows as %q, want %q", got, "(Bool, Int) -> Bool")
		}
		x, y := e.fresh(), e.fresh()
		e.check(t, "variables on the found side", e.fn(i, b), e.fn(y, x), annotated, true)
		if got := e.tbl.Show(e.fn(y, x)); got != "(Bool) -> Int" {
			t.Errorf("variables on the found side: (x) -> y shows as %q, want %q", got, "(Bool) -> Int")
		}

		// The same variable twice has to get the same type both times: it takes
		// Int from the first parameter, and the second is where the two types
		// disagree. The bindings made before the mismatch stay, so the label
		// shows p as Int.
		p := e.fresh()
		e.badAnnotated(t, "a variable that gets two types", e.fn(i, p, p), e.fn(i, i, b), "expected `(Int, Int) -> Int`, found `(Int, Bool) -> Int`")
		if got := e.tbl.Resolve(p); got != i {
			t.Errorf("a variable that gets two types: p resolves to %s, want Int", e.tbl.Show(got))
		}

		// The parameters come before the result: q takes Int from the parameter
		// and then the results differ, so the label shows q as Int. A unifier that
		// started with the result would stop there and show it as a variable.
		q := e.fresh()
		e.badAnnotated(t, "a parameter that binds before the result fails", e.fn(i, q), e.fn(b, i), "expected `(Int) -> Int`, found `(Int) -> Bool`")

		// The memo of Unify is keyed by the pair of types, not by one side of it:
		// one function type can be met by two different ones. pf is a variable
		// bound to (Int) -> Int, so both of the first two parameters on one side
		// are the one node X. The other side has (cv) -> Int, with cv bound to Int,
		// which is another node, and then (Int) -> Bool. X against the first is
		// fine, and X against the second is the mistake. A memo that looked only
		// at the expected node would take the second pair for the first and skip
		// it; the mirror image does the same for the found node.
		pf, cv := e.fresh(), e.fresh()
		bind(t, e.tbl, pf, e.fn(i, i))
		bind(t, e.tbl, cv, i)
		e.badAnnotated(t, "one expected function against two found ones",
			e.fn(i, pf, pf), e.fn(i, e.fn(i, cv), e.fn(b, i)),
			"expected `((Int) -> Int, (Int) -> Int) -> Int`, found `((Int) -> Int, (Int) -> Bool) -> Int`")
		e.badAnnotated(t, "one found function against two expected ones",
			e.fn(i, e.fn(i, cv), e.fn(b, i)), e.fn(i, pf, pf),
			"expected `((Int) -> Int, (Int) -> Bool) -> Int`, found `((Int) -> Int, (Int) -> Int) -> Int`")

		// Types as deep as the program may make them are walked to the bottom.
		// (3000 frames of Go recursion would fit, so this does not show that
		// Unify has none; it keeps its own stack, as its doc says.) The chains are
		// `(...((T) -> Int) -> Int...) -> Int`, 3000 functions deep. The first is
		// built on Int and the second on a variable bound to Int, so no function
		// of one is a function of the other.
		const depth = 3000
		chain := func(bottom TypeID) TypeID {
			id := bottom
			for range depth {
				id = e.fn(i, id)
			}
			return id
		}
		viaVar := e.fresh()
		bind(t, e.tbl, viaVar, i)
		onInt, onVar, onBool := chain(i), chain(viaVar), chain(b)
		if onInt == onVar {
			t.Fatal("the test needs two chains with no function in common")
		}
		e.check(t, "deep chains of the same shape", onInt, onVar, annotated, true)
		// Both chains start with 3000 brackets, so Show cuts both at 256 of them
		// and the two texts of the label are the same: a cut text is bounded, not
		// always useful. The row fixes that, and that the label is what Show
		// writes.
		shownOnInt, shownOnBool := e.tbl.Show(onInt), e.tbl.Show(onBool)
		if cut := strings.Repeat("(", MaxTypeText) + "…"; shownOnInt != cut || shownOnBool != cut {
			t.Fatalf("Show of the chains of %d levels is %.20q… and %.20q…, want the same %d brackets and …", depth, shownOnInt, shownOnBool, MaxTypeText)
		}
		e.badAnnotated(t, "a deep chain that differs at the bottom", onInt, onBool,
			"expected `"+shownOnInt+"`, found `"+shownOnBool+"`")

		// Types that hold the same type twice at every level. 40 levels are 40
		// function nodes on each side and 2^40 paths from the top to the bottom
		// of each, and the two sides share no node, so the ids tell nothing. A
		// Unify that took every path apart would not finish, and within fails
		// the test if it takes ten seconds; one that takes each pair apart once
		// does about a hundred steps. This does not count the steps, so a
		// comparison that took each pair apart twice would pass. The first two
		// rows are matches. A mismatch is the third, further down: its label has
		// to write both types, and does it cut (see TypeTable.Show).
		const levels = 40
		twice := func(bottom TypeID) TypeID {
			id := bottom
			for range levels {
				id = e.fn(i, id, id)
			}
			return id
		}
		endA, endB := e.fresh(), e.fresh()
		onA, onB := twice(endA), twice(endB)
		endC := e.fresh()
		bind(t, e.tbl, endC, i)
		twiceInt, twiceVar := twice(i), twice(endC)
		if onA == onB || twiceInt == twiceVar {
			t.Fatal("the test needs pairs of chains that share no function")
		}
		var okVars, okInt bool
		var gotVars, gotInt []diag.Diagnostic
		within.Run(t, "Unify of two types of 40 levels", func() {
			okVars, gotVars = e.unify(onA, onB, annotated)
			okInt, gotInt = e.unify(twiceInt, twiceVar, annotated)
		})
		if !okVars || len(gotVars) != 0 {
			t.Errorf("two chains of 40 levels that end in two variables: Unify returned %v with %d diagnostics, want true with none", okVars, len(gotVars))
		}
		if e.tbl.Resolve(endA) != e.tbl.Resolve(endB) {
			t.Error("two chains of 40 levels that end in two variables: the variables at the bottom were not made one")
		}
		if !okInt || len(gotInt) != 0 {
			t.Errorf("two chains of 40 levels, on Int and on a variable bound to Int: Unify returned %v with %d diagnostics, want true with none", okInt, len(gotInt))
		}

		// A mismatch between two such types ends too, and its label writes each
		// type cut: whole, each would be 2^40 pieces of text. These two end in Int
		// and in Bool, so the one pair that differs is at the bottom of 40 levels.
		twiceBool := twice(b)
		var okBad bool
		var gotBad []diag.Diagnostic
		within.Run(t, "Unify of two types of 40 levels that differ at the bottom", func() {
			okBad, gotBad = e.unify(twiceInt, twiceBool, annotated)
		})
		shownInt, shownBool := e.tbl.Show(twiceInt), e.tbl.Show(twiceBool)
		if !strings.HasSuffix(shownInt, "…") || !strings.HasSuffix(shownBool, "…") {
			t.Fatalf("the test needs two types that Show cuts, and it writes %q and %q", shownInt, shownBool)
		}
		wantBad := withBecause(wantMismatch("expected `"+shownInt+"`, found `"+shownBool+"`"), annotatedLabel)
		if okBad || len(gotBad) != 1 || !reflect.DeepEqual(gotBad[0], wantBad) {
			t.Errorf("two chains of 40 levels that differ at the bottom: Unify returned %v and reported %+v, want false and %+v", okBad, gotBad, wantBad)
		}
	})

	t.Run("arity_mismatch_is_E0401", func(t *testing.T) {
		e := newUnifyEnv()
		i := e.intT
		// The shorter list of parameters is a prefix of the longer one in the
		// first two rows, which a comparison that stops at the shorter one
		// would take for equal.
		e.badAnnotated(t, "one parameter, then two", e.fn(i, i), e.fn(i, i, i), "expected `(Int) -> Int`, found `(Int, Int) -> Int`")
		e.badAnnotated(t, "two parameters, then one", e.fn(i, i, i), e.fn(i, i), "expected `(Int, Int) -> Int`, found `(Int) -> Int`")
		e.badAnnotated(t, "none, then one", e.fn(i), e.fn(i, i), "expected `() -> Int`, found `(Int) -> Int`")
		e.badAnnotated(t, "one, then none", e.fn(i, i), e.fn(i), "expected `(Int) -> Int`, found `() -> Int`")
		e.badAnnotated(t, "inside a parameter", e.fn(i, e.fn(i, i)), e.fn(i, e.fn(i, i, i)), "expected `((Int) -> Int) -> Int`, found `((Int, Int) -> Int) -> Int`")

		// The arity is checked before any part is unified: with variables in the
		// parts, none of them ends up bound. And two different variables get two
		// names in one label, because the label names the variables of both types
		// together.
		x, y := e.fresh(), e.fresh()
		e.badAnnotated(t, "variables", e.fn(i, x), e.fn(i, y, y), "expected `(a) -> Int`, found `(b, b) -> Int`")
		if !e.isUnbound(x) || !e.isUnbound(y) || e.tbl.Resolve(x) == e.tbl.Resolve(y) {
			t.Errorf("variables: x is %s and y is %s afterwards, want two different unbound variables", e.tbl.Show(x), e.tbl.Show(y))
		}

		// A name that the cut leaves out is not used up. The first type has 51
		// parameters of Int and then a variable: its text is 256 bytes long up to
		// the variable, which does not fit, so the cut ends it there and the
		// variable is never written. The variable of the other type is then the
		// first that the label names, `a`, and not `b`.
		ints := make([]TypeID, 51)
		for k := range ints {
			ints[k] = i
		}
		z, w := e.fresh(), e.fresh()
		e.badAnnotated(t, "a variable that the cut leaves out", e.fn(i, append(ints, z)...), e.fn(i, w),
			"expected `("+strings.Repeat("Int, ", 51)+"…`, found `(a) -> Int`")
	})

	t.Run("operand_reason_names_expected_type", func(t *testing.T) {
		e := newUnifyEnv()
		operand := func(op string, arity int) Reason {
			return Reason{Kind: ReasonOperand, Because: unifyBecause, Detail: op, Index: arity}
		}
		// The text names the operator that Detail holds and the type that was
		// expected, and says "operands" for the binary form (Index 2) and "a ...
		// operand" for the unary one (Index 1), with "an" before a type that
		// starts with a vowel.
		e.check(t, "&& wants Bool operands", e.boolT, e.intT, operand("&&", 2), false,
			withBecause(wantMismatch("expected `Bool`, found `Int`"), "`&&` expects `Bool` operands"))
		e.check(t, "+ wants Int operands", e.intT, e.boolT, operand("+", 2), false,
			withBecause(wantMismatch("expected `Int`, found `Bool`"), "`+` expects `Int` operands"))
		e.check(t, "unary - wants an Int operand", e.intT, e.boolT, operand("-", 1), false,
			withBecause(wantMismatch("expected `Int`, found `Bool`"), "`-` expects an `Int` operand"))
		e.check(t, "unary ! wants a Bool operand", e.boolT, e.intT, operand("!", 1), false,
			withBecause(wantMismatch("expected `Bool`, found `Int`"), "`!` expects a `Bool` operand"))
		// The article follows the type, not the operator or the arity: the other
		// types that start with something that is not a vowel, in the unary form.
		e.check(t, "unary operator that wants a unit operand", e.unitT, e.intT, operand("!", 1), false,
			withBecause(wantMismatch("expected `()`, found `Int`"), "`!` expects a `()` operand"))
		e.check(t, "unary operator that wants a function operand", e.fn(e.intT, e.intT), e.boolT, operand("-", 1), false,
			withBecause(wantMismatch("expected `(Int) -> Int`, found `Bool`"), "`-` expects a `(Int) -> Int` operand"))

		// The reason says nothing about what is unified when it succeeds, and a
		// reason with an arity that is neither 1 nor 2 is the checker's bug.
		e.check(t, "a matching operand needs no arity", e.intT, e.intT, operand("+", 0), true)
		for _, arity := range []int{0, 3, -1} {
			if !panics(func() { e.unify(e.intT, e.boolT, operand("+", arity)) }) {
				t.Errorf("an operand reason with arity %d: Unify did not panic", arity)
			}
		}
		// A kind of reason that is not one of the eight is the checker's bug
		// too: ReasonKind is a number, so a value outside the enum can arrive.
		if !panics(func() { e.unify(e.intT, e.boolT, Reason{Kind: ReasonKind(200)}) }) {
			t.Errorf("a reason of kind 200: Unify did not panic")
		}
	})

	t.Run("occurs_check_E0405", func(t *testing.T) {
		e := newUnifyEnv()
		i := e.intT
		const note = "`a` would have to be `(a) -> Int`, which has no finite size"
		// An infinite type has no secondary label and no help, whatever the
		// reason was: the cause is in the type itself.
		reasons := []Reason{
			annotated,
			{Kind: ReasonArgument, Because: unifyBecause},
			{Kind: ReasonBlockTail, Because: unifyBecause},
		}
		for _, r := range reasons {
			a := e.fresh()
			e.check(t, "a ~ (a) -> Int", a, e.fn(i, a), r, false, wantInfinite(note))
			// Fatalf, and no Show of a: if Unify had bound a to a type that
			// contains it, Show would never finish writing it.
			if !e.isUnbound(a) {
				t.Fatalf("a ~ (a) -> Int: a is bound afterwards, want it unbound")
			}
		}

		a := e.fresh()
		e.check(t, "(a) -> Int ~ a, the variable on the found side", e.fn(i, a), a, annotated, false, wantInfinite(note))
		if !e.isUnbound(a) {
			t.Fatalf("(a) -> Int ~ a: a is bound afterwards, want it unbound")
		}

		// The variable is found inside a part of the types.
		e.check(t, "inside a parameter", e.fn(i, a), e.fn(i, e.fn(i, a)), annotated, false, wantInfinite(note))
		e.check(t, "inside a nested parameter", a, e.fn(i, i, e.fn(i, a)),
			annotated, false, wantInfinite("`a` would have to be `(Int, (a) -> Int) -> Int`, which has no finite size"))

		// Through a variable that is bound: b stands for (a) -> Int.
		b := e.fresh()
		bind(t, e.tbl, b, e.fn(i, a))
		e.check(t, "a ~ b with b bound to (a) -> Int", a, b, annotated, false, wantInfinite(note))

		// The note names the variable `a` and writes the type with the same
		// names, so a variable of the type that is not the one in the note is `b`.
		x, y := e.fresh(), e.fresh()
		e.check(t, "two variables", y, e.fn(i, x, y), annotated, false,
			wantInfinite("`a` would have to be `(b, a) -> Int`, which has no finite size"))
		if !e.isUnbound(x) || !e.isUnbound(y) {
			t.Fatalf("two variables: x or y is bound afterwards, want both unbound")
		}

		// A variable and a different variable is not an infinite type.
		c, d := e.fresh(), e.fresh()
		e.check(t, "c ~ (d) -> Int", c, e.fn(i, d), annotated, true)
		if got := e.tbl.Show(c); got != "(a) -> Int" {
			t.Errorf("c ~ (d) -> Int: c shows as %q, want %q", got, "(a) -> Int")
		}
	})

	t.Run("error_type_unifies_silently", func(t *testing.T) {
		e := newUnifyEnv()
		i, b, bad := e.intT, e.boolT, e.tbl.Error()
		r := annotated
		e.check(t, "error and Int", bad, i, r, true)
		e.check(t, "Int and error", i, bad, r, true)
		e.check(t, "error and error", bad, bad, r, true)
		e.check(t, "error and a function", bad, e.fn(i, i), r, true)
		e.check(t, "a function and error", e.fn(i, i), bad, r, true)
		e.check(t, "error at the tail of a body", e.unitT, bad, Reason{Kind: ReasonBlockTail, Because: unifyBecause}, true)

		// A variable that meets the error type is not bound to it.
		v, w := e.fresh(), e.fresh()
		e.check(t, "a variable and error", v, bad, r, true)
		e.check(t, "error and a variable", bad, w, r, true)
		if !e.isUnbound(v) || !e.isUnbound(w) {
			t.Errorf("a variable and error: v is %s and w is %s afterwards, want both unbound", e.tbl.Show(v), e.tbl.Show(w))
		}

		// Inside a function type, the part that is the error type matches and
		// the rest is compared as usual.
		e.check(t, "error as a parameter", e.fn(i, bad), e.fn(i, b), r, true)
		e.check(t, "error as the result", e.fn(bad, i), e.fn(b, i), r, true)
		e.check(t, "error in a part, a mistake in another", e.fn(i, bad, i), e.fn(i, b, b), r, false,
			withBecause(wantMismatch("expected `({error}, Int) -> Int`, found `(Bool, Bool) -> Int`"), annotatedLabel))
	})

	t.Run("mismatch_has_reason_label", func(t *testing.T) {
		e := newUnifyEnv()
		i, b := e.intT, e.boolT
		// The secondary label sits on Because and its text depends on the kind
		// of reason. IfCondition and Operand have their own subtests.
		rows := []struct {
			name string
			kind ReasonKind
			want string
		}{
			{"annotation", ReasonAnnotation, "expected because of this annotation"},
			{"let annotation", ReasonLetAnnotation, "expected because of this annotation"},
			{"return type", ReasonReturnType, "expected because of the return type"},
			{"block tail", ReasonBlockTail, "expected because of the return type"},
			{"argument", ReasonArgument, "the parameter is declared here"},
			{"if branches", ReasonIfBranches, "the other branch has type `Int`"},
		}
		for _, row := range rows {
			e.check(t, row.name, i, b, Reason{Kind: row.kind, Because: unifyBecause}, false,
				withBecause(wantMismatch("expected `Int`, found `Bool`"), row.want))
		}

		// A reason without a place (the zero Span), or with one in another file
		// than the found type, has no secondary label, because RenderHuman could
		// not draw one in another file under the primary label. The rest of the
		// diagnostic is as it was, for every kind of reason.
		elsewhere := base.Span{File: unifyFoundAt.File + 1, Lo: 7, Hi: 12}
		for _, where := range []struct {
			name string
			span base.Span
		}{{"no place", base.Span{}}, {"another file", elsewhere}} {
			for _, row := range rows {
				e.check(t, where.name+", "+row.name, i, b, Reason{Kind: row.kind, Because: where.span}, false,
					wantMismatch("expected `Int`, found `Bool`"))
			}
			e.check(t, where.name+", one operand", i, b, Reason{Kind: ReasonOperand, Because: where.span, Detail: "-", Index: 1}, false,
				wantMismatch("expected `Int`, found `Bool`"))
			e.check(t, where.name+", two operands", i, b, Reason{Kind: ReasonOperand, Because: where.span, Detail: "+", Index: 2}, false,
				wantMismatch("expected `Int`, found `Bool`"))
			// The help of a body that returns `()` does not depend on the place.
			e.check(t, where.name+", help at the tail", e.unitT, i, Reason{Kind: ReasonBlockTail, Because: where.span, CanReturn: true}, false,
				wantMismatch("expected `()`, found `Int`").WithHelp(unusedHelp))
		}
		// The zero Span is told from a real span of the file that has the id 0,
		// the prelude's, by being the zero Span and not by its file: a found
		// type in that file, with no place, has no label either, and one with a
		// place has it.
		inPrelude := func(because base.Span) []diag.Diagnostic {
			sink := &diag.Sink{}
			(&Unifier{Types: e.tbl, Sink: sink}).Unify(i, b, base.Span{File: 0, Lo: 40, Hi: 45}, Reason{Kind: ReasonArgument, Because: because})
			return sink.All()
		}
		primary := diag.Label{Span: base.Span{File: 0, Lo: 40, Hi: 45}, Message: "expected `Int`, found `Bool`"}
		if got, want := inPrelude(base.Span{}), []diag.Diagnostic{diag.NewError(diag.E(401), "mismatched types", primary)}; !reflect.DeepEqual(got, want) {
			t.Errorf("found type in file 0, no place: %+v, want %+v", got, want)
		}
		placed := base.Span{File: 0, Lo: 7, Hi: 12}
		want := diag.NewError(diag.E(401), "mismatched types", primary).WithSecondary(diag.Label{Span: placed, Message: "the parameter is declared here"})
		if got := inPrelude(placed); !reflect.DeepEqual(got, []diag.Diagnostic{want}) {
			t.Errorf("found type in file 0, place in file 0: %+v, want %+v", got, want)
		}

		// The branch label shows the type that the first branch has, resolved:
		// a variable bound to Int is Int, and a function type is written whole.
		branches := Reason{Kind: ReasonIfBranches, Because: unifyBecause}
		c := e.fresh()
		bind(t, e.tbl, c, i)
		e.check(t, "branches with a bound variable", c, b, branches, false,
			withBecause(wantMismatch("expected `Int`, found `Bool`"), "the other branch has type `Int`"))
		e.check(t, "branches with a function", e.fn(i, i), b, branches, false,
			withBecause(wantMismatch("expected `(Int) -> Int`, found `Bool`"), "the other branch has type `(Int) -> Int`"))
		// And with the variables named as in the primary label.
		x, y := e.fresh(), e.fresh()
		e.check(t, "branches with variables", e.fn(i, y, x), e.fn(b, x), branches, false,
			withBecause(wantMismatch("expected `(a, b) -> Int`, found `(b) -> Bool`"), "the other branch has type `(a, b) -> Int`"))
	})

	t.Run("if_condition_has_note", func(t *testing.T) {
		e := newUnifyEnv()
		// The condition has a note about the rule and no secondary label, though
		// the reason carries a span.
		cond := Reason{Kind: ReasonIfCondition, Because: unifyBecause}
		note := "an `if` condition must be `Bool`"
		e.check(t, "an Int condition", e.boolT, e.intT, cond, false,
			wantMismatch("expected `Bool`, found `Int`").WithNote(note))
		e.check(t, "a function condition", e.boolT, e.fn(e.intT, e.intT), cond, false,
			wantMismatch("expected `Bool`, found `(Int) -> Int`").WithNote(note))
		e.check(t, "a Bool condition", e.boolT, e.boolT, cond, true)
	})

	t.Run("unit_at_block_tail_has_help", func(t *testing.T) {
		e := newUnifyEnv()
		i, b, u := e.intT, e.boolT, e.unitT
		// A tail where returning the value would mend the mistake, and one where
		// it would not.
		tail := Reason{Kind: ReasonBlockTail, Because: unifyBecause, CanReturn: true}
		bindOnly := Reason{Kind: ReasonBlockTail, Because: unifyBecause}
		const returnLabel = "expected because of the return type"
		helped := func(found, help string) diag.Diagnostic {
			return withBecause(wantMismatch("expected `()`, found `"+found+"`"), returnLabel).WithHelp(help)
		}
		e.check(t, "Int at the tail", u, i, tail, false, helped("Int", unusedHelp))
		e.check(t, "Bool at the tail", u, b, tail, false, helped("Bool", unusedHelp))
		e.check(t, "a function at the tail", u, e.fn(i, i), tail, false, helped("(Int) -> Int", unusedHelp))
		// The help is chosen by what the types are, not by the ids they came as.
		unitVar, intVar := e.fresh(), e.fresh()
		bind(t, e.tbl, unitVar, u)
		bind(t, e.tbl, intVar, i)
		e.check(t, "expected is a variable bound to ()", unitVar, i, tail, false, helped("Int", unusedHelp))
		e.check(t, "found is a variable bound to Int", u, intVar, tail, false, helped("Int", unusedHelp))
		e.check(t, "a variable at the tail binds to ()", u, e.fresh(), tail, true)

		// Where the value cannot be returned (the end of a branch with no
		// `else`, say) the help offers only the binding.
		e.check(t, "Int at a tail that cannot be returned", u, i, bindOnly, false, helped("Int", unusedBindHelp))
		e.check(t, "a function at a tail that cannot be returned", u, e.fn(i, i), bindOnly, false, helped("(Int) -> Int", unusedBindHelp))
		e.check(t, "expected is a variable bound to (), at a tail that cannot be returned", unitVar, i, bindOnly, false, helped("Int", unusedBindHelp))

		// Without the help: another kind of reason with the same two types, and
		// the block-tail reason with other types. A mistake reported without help
		// is checked as a whole: one E0401 with no fix-it. The other kinds do not
		// read CanReturn, so they get no help with it set either.
		noHelp := func(row string, expected, found TypeID, r Reason) {
			t.Helper()
			ok, got := e.unify(expected, found, r)
			if ok || len(got) != 1 || got[0].Code != diag.E(401) || got[0].Help != "" || len(got[0].Fixits) != 0 {
				t.Errorf("%s: Unify returned %v and reported %+v, want false and one E0401 with no help and no fix-it", row, ok, got)
			}
		}
		for _, r := range []Reason{
			{Kind: ReasonLetAnnotation, Because: unifyBecause, CanReturn: true},
			{Kind: ReasonAnnotation, Because: unifyBecause, CanReturn: true},
			{Kind: ReasonReturnType, Because: unifyBecause, CanReturn: true},
			{Kind: ReasonArgument, Because: unifyBecause, CanReturn: true},
			{Kind: ReasonIfBranches, Because: unifyBecause, CanReturn: true},
			{Kind: ReasonIfCondition, Because: unifyBecause, CanReturn: true},
			{Kind: ReasonOperand, Because: unifyBecause, Detail: "+", Index: 2, CanReturn: true},
		} {
			noHelp("() and Int with the reason kind "+strconv.Itoa(int(r.Kind)), u, i, r)
		}
		noHelp("Int expected, Bool at the tail", i, b, tail)
		noHelp("Int expected, () at the tail", i, u, tail)
		noHelp("a function expected, Int at the tail", e.fn(u), i, tail)
		noHelp("Int expected, Bool at a tail that cannot be returned", i, b, bindOnly)
	})
}
