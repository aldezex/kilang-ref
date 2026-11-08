package core_test

import (
	"math"
	"slices"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
)

// span returns a span in file 1 (file 0 is the zero value, which a builder that
// forgot the span would leave behind) with the given offsets.
func span(lo, hi uint32) base.Span {
	return base.Span{File: 1, Lo: lo, Hi: hi}
}

// newBuilder returns a Builder over an empty Program, the way a pass starts.
func newBuilder() (*core.Builder, *core.Program) {
	p := &core.Program{}
	return &core.Builder{P: p}, p
}

func TestBuilder(t *testing.T) {
	t.Run("creates_let", func(t *testing.T) {
		b, p := newBuilder()
		// Two variables, so that x has id 1: an id of 0 is also what a field
		// that was never set holds.
		p.Vars.Push(core.Var{})
		x := p.Vars.Push(core.Var{})
		// The Init is a literal and the Body a variable, so a Let whose two
		// children are swapped differs in Kind as well as in id.
		init := b.LitInt(span(8, 9), 42)
		body := b.VarRef(span(11, 12), x)
		let := b.Let(span(4, 12), x, init, body)

		if got := p.Exprs.Len(); got != 3 {
			t.Fatalf("Exprs.Len() = %d, want 3 (Init, Body and the Let)", got)
		}
		if init != 0 || body != 1 || let != 2 {
			t.Errorf("ids are %d, %d and %d, want 0, 1 and 2, in the order they were built", init, body, let)
		}
		want := core.Expr{Kind: core.Let, Span: span(4, 12), Var: x, Init: init, Body: body}
		if got := p.Exprs.Get(let); got != want {
			t.Errorf("the Let is %+v, want %+v", got, want)
		}
		if got := p.Exprs.Get(init); got.Kind != core.LitInt || got.Int != 42 {
			t.Errorf("the Init is %+v, want the literal 42", got)
		}
		if got := p.Exprs.Get(body); got.Kind != core.VarRef || got.Var != x {
			t.Errorf("the Body is %+v, want a reference to variable %d", got, x)
		}

		// A Let in the Body of another one. Here no child of the outer Let has
		// id 0, so a Let that never stored its Init or its Body is caught too.
		outerInit := b.LitInt(span(20, 21), 1)
		innerInit := b.LitUnit(span(26, 28))
		innerBody := b.VarRef(span(29, 30), x)
		inner := b.Let(span(22, 30), x, innerInit, innerBody)
		outer := b.Let(span(0, 30), x, outerInit, inner)
		if got, want := p.Exprs.Get(outer), (core.Expr{Kind: core.Let, Span: span(0, 30), Var: x, Init: outerInit, Body: inner}); got != want {
			t.Errorf("the outer Let is %+v, want %+v", got, want)
		}
		if got, want := p.Exprs.Get(inner), (core.Expr{Kind: core.Let, Span: span(22, 30), Var: x, Init: innerInit, Body: innerBody}); got != want {
			t.Errorf("the inner Let is %+v, want %+v", got, want)
		}
	})

	t.Run("creates_literals_and_refs", func(t *testing.T) {
		b, p := newBuilder()
		rows := []struct {
			name string
			id   core.ExprID
			want core.Expr
		}{
			{"int", b.LitInt(span(1, 2), math.MinInt64), core.Expr{Kind: core.LitInt, Span: span(1, 2), Int: math.MinInt64}},
			{"float", b.LitFloat(span(23, 26), 1.5), core.Expr{Kind: core.LitFloat, Span: span(23, 26), Float: 1.5}},
			{"true", b.LitBool(span(3, 7), true), core.Expr{Kind: core.LitBool, Span: span(3, 7), Bool: true}},
			{"false", b.LitBool(span(8, 13), false), core.Expr{Kind: core.LitBool, Span: span(8, 13)}},
			{"unit", b.LitUnit(span(14, 16)), core.Expr{Kind: core.LitUnit, Span: span(14, 16)}},
			{"var", b.VarRef(span(17, 18), 3), core.Expr{Kind: core.VarRef, Span: span(17, 18), Var: 3}},
			{"fun", b.FunRef(span(19, 22), 5), core.Expr{Kind: core.FunRef, Span: span(19, 22), Fun: 5}},
		}
		if p.Exprs.Len() != len(rows) {
			t.Fatalf("Exprs.Len() = %d, want %d", p.Exprs.Len(), len(rows))
		}
		for i, row := range rows {
			if row.id != core.ExprID(i) {
				t.Errorf("%s: id %d, want %d", row.name, row.id, i)
			}
			if got := p.Exprs.Get(row.id); got != row.want {
				t.Errorf("%s: %+v, want %+v", row.name, got, row.want)
			}
		}
		if len(p.ExprPool) != 0 || len(p.ArmPool) != 0 {
			t.Errorf("leaves left %d ids in ExprPool and %d arms in ArmPool, want none", len(p.ExprPool), len(p.ArmPool))
		}
	})

	t.Run("creates_calls_and_prims", func(t *testing.T) {
		b, p := newBuilder()
		one := b.LitInt(span(0, 1), 1)
		two := b.LitInt(span(2, 3), 2)
		three := b.LitInt(span(4, 5), 3)
		f := b.FunRef(span(6, 7), 9)

		call := b.Call(span(6, 12), f, []core.ExprID{one, two})
		noArgs := b.Call(span(13, 16), f, nil)
		add := b.Prim(span(17, 22), core.PrimAddInt, []core.ExprID{one, three})
		neg := b.Prim(span(23, 26), core.PrimNegInt, []core.ExprID{two})

		for _, row := range []struct {
			name string
			id   core.ExprID
			want core.Expr
		}{
			{"call", call, core.Expr{Kind: core.Call, Span: span(6, 12), Callee: f, Args: base.ListRange{First: 0, Count: 2}}},
			{"call without arguments", noArgs, core.Expr{Kind: core.Call, Span: span(13, 16), Callee: f, Args: base.ListRange{First: 2, Count: 0}}},
			{"binary prim", add, core.Expr{Kind: core.PrimCall, Span: span(17, 22), Prim: core.PrimAddInt, Args: base.ListRange{First: 2, Count: 2}}},
			{"unary prim", neg, core.Expr{Kind: core.PrimCall, Span: span(23, 26), Prim: core.PrimNegInt, Args: base.ListRange{First: 4, Count: 1}}},
		} {
			if got := p.Exprs.Get(row.id); got != row.want {
				t.Errorf("%s: %+v, want %+v", row.name, got, row.want)
			}
		}
		// The four lists sit one after the other in the pool, each in order.
		if want := []core.ExprID{one, two, one, three, two}; !slices.Equal(p.ExprPool, want) {
			t.Errorf("ExprPool is %v, want %v", p.ExprPool, want)
		}
		for _, row := range []struct {
			name string
			r    base.ListRange
			want []core.ExprID
		}{
			{"call", p.Exprs.Get(call).Args, []core.ExprID{one, two}},
			{"call without arguments", p.Exprs.Get(noArgs).Args, nil},
			{"binary prim", p.Exprs.Get(add).Args, []core.ExprID{one, three}},
			{"unary prim", p.Exprs.Get(neg).Args, []core.ExprID{two}},
		} {
			got := p.Args(row.r)
			if !slices.Equal(got, row.want) {
				t.Errorf("Args of the %s is %v, want %v", row.name, got, row.want)
			}
			if cap(got) != len(got) {
				t.Errorf("Args of the %s has len %d and cap %d, want them equal", row.name, len(got), cap(got))
			}
		}
		// Because the capacity ends where the list does, an append to one list
		// cannot overwrite the first element of the next.
		_ = append(p.Args(p.Exprs.Get(call).Args), 99)
		if want := []core.ExprID{one, two, one, three, two}; !slices.Equal(p.ExprPool, want) {
			t.Errorf("append to the Args of the call changed ExprPool to %v, want %v", p.ExprPool, want)
		}
		// The builder copies the arguments: the caller can reuse its slice.
		args := []core.ExprID{three, three}
		again := b.Call(span(27, 30), f, args)
		args[0], args[1] = one, one
		if got := p.Args(p.Exprs.Get(again).Args); !slices.Equal(got, []core.ExprID{three, three}) {
			t.Errorf("Args is %v after the caller changed its slice, want [%d %d]", got, three, three)
		}
	})

	t.Run("creates_con", func(t *testing.T) {
		// A Con holds its constructor and its arguments, which the builder copies
		// into the pool as it does for a call; one with no arguments has an empty
		// list, and the Builder does not check the arity (Lint does).
		b, p := newBuilder()
		one := b.LitInt(span(0, 1), 1)
		two := b.LitInt(span(2, 3), 2)
		args := []core.ExprID{one, two}
		pair := b.Con(span(0, 8), 7, args)
		args[0], args[1] = two, two // the caller reuses its slice
		none := b.Con(span(9, 13), 3, nil)
		wrong := b.Con(span(14, 20), 7, []core.ExprID{one})
		for _, row := range []struct {
			name string
			id   core.ExprID
			want core.Expr
		}{
			{"two arguments", pair, core.Expr{Kind: core.Con, Span: span(0, 8), Ctor: 7, Args: base.ListRange{First: 0, Count: 2}}},
			{"no arguments", none, core.Expr{Kind: core.Con, Span: span(9, 13), Ctor: 3, Args: base.ListRange{First: 2, Count: 0}}},
			{"an arity that no constructor has", wrong, core.Expr{Kind: core.Con, Span: span(14, 20), Ctor: 7, Args: base.ListRange{First: 2, Count: 1}}},
		} {
			if got := p.Exprs.Get(row.id); got != row.want {
				t.Errorf("%s: %+v, want %+v", row.name, got, row.want)
			}
		}
		if got := p.Args(p.Exprs.Get(pair).Args); !slices.Equal(got, []core.ExprID{one, two}) || cap(got) != len(got) {
			t.Errorf("Args of the pair is %v (cap %d) after the caller changed its slice, want [%d %d] with the capacity clipped", got, cap(got), one, two)
		}
		if want := []core.ExprID{one, two, one}; !slices.Equal(p.ExprPool, want) {
			t.Errorf("ExprPool is %v, want %v", p.ExprPool, want)
		}
	})

	t.Run("creates_match", func(t *testing.T) {
		b, p := newBuilder()
		// The condition is not the first expression, so its id is not the 0 that
		// a Match which never stored its Scrutinee would hold.
		one := b.LitInt(span(5, 6), 1)
		cond := b.LitBool(span(0, 4), true)
		two := b.LitInt(span(7, 8), 2)
		three := b.LitInt(span(9, 10), 3)
		// The patterns are nodes of their own and the arms hold their ids. A filler
		// comes first, so that none of those ids is the 0 of a field never written.
		b.Pat(core.Pat{Kind: core.PatWildcard, Span: span(0, 0)})
		patTrue := b.Pat(core.Pat{Kind: core.PatTrue, Span: span(0, 4)})
		patFalse := b.Pat(core.Pat{Kind: core.PatFalse, Span: span(0, 5)})
		patWild := b.Pat(core.Pat{Kind: core.PatWildcard, Span: span(0, 1)})

		ifElse := b.Match(span(0, 20), cond, []core.Arm{{Pat: patTrue, Body: one}, {Pat: patFalse, Body: two}})
		wild := b.Match(span(21, 30), cond, []core.Arm{{Pat: patWild, Body: three}})

		for _, row := range []struct {
			name string
			id   core.ExprID
			want core.Expr
		}{
			{"true and false", ifElse, core.Expr{Kind: core.Match, Span: span(0, 20), Scrutinee: cond, Arms: base.ListRange{First: 0, Count: 2}}},
			{"wildcard", wild, core.Expr{Kind: core.Match, Span: span(21, 30), Scrutinee: cond, Arms: base.ListRange{First: 2, Count: 1}}},
		} {
			if got := p.Exprs.Get(row.id); got != row.want {
				t.Errorf("%s: %+v, want %+v", row.name, got, row.want)
			}
		}
		wantPool := []core.Arm{{Pat: patTrue, Body: one}, {Pat: patFalse, Body: two}, {Pat: patWild, Body: three}}
		if !slices.Equal(p.ArmPool, wantPool) {
			t.Errorf("ArmPool is %v, want %v", p.ArmPool, wantPool)
		}
		for _, row := range []struct {
			name string
			r    base.ListRange
			want []core.Arm
		}{
			{"true and false", p.Exprs.Get(ifElse).Arms, wantPool[:2]},
			{"wildcard", p.Exprs.Get(wild).Arms, wantPool[2:]},
		} {
			got := p.ArmsOf(row.r)
			if !slices.Equal(got, row.want) {
				t.Errorf("ArmsOf the %s match is %v, want %v", row.name, got, row.want)
			}
			if cap(got) != len(got) {
				t.Errorf("ArmsOf the %s match has len %d and cap %d, want them equal", row.name, len(got), cap(got))
			}
		}
		// The arms live in ArmPool and only there.
		if len(p.ExprPool) != 0 {
			t.Errorf("Match left %v in ExprPool, want nothing", p.ExprPool)
		}
	})
}

// allPrims lists every primitive with its name and arity, in the order of the
// constants. The tests of Prim read it.
var allPrims = []struct {
	p     core.Prim
	name  string
	arity int
}{
	{core.PrimAddInt, "add_int", 2},
	{core.PrimSubInt, "sub_int", 2},
	{core.PrimMulInt, "mul_int", 2},
	{core.PrimDivInt, "div_int", 2},
	{core.PrimRemInt, "rem_int", 2},
	{core.PrimNegInt, "neg_int", 1},
	{core.PrimEqInt, "eq_int", 2},
	{core.PrimNeInt, "ne_int", 2},
	{core.PrimLtInt, "lt_int", 2},
	{core.PrimLeInt, "le_int", 2},
	{core.PrimGtInt, "gt_int", 2},
	{core.PrimGeInt, "ge_int", 2},
	{core.PrimEqBool, "eq_bool", 2},
	{core.PrimNeBool, "ne_bool", 2},
	{core.PrimNotBool, "not_bool", 1},
	{core.PrimAddFloat, "add_float", 2},
	{core.PrimSubFloat, "sub_float", 2},
	{core.PrimMulFloat, "mul_float", 2},
	{core.PrimDivFloat, "div_float", 2},
	{core.PrimNegFloat, "neg_float", 1},
	{core.PrimEqFloat, "eq_float", 2},
	{core.PrimNeFloat, "ne_float", 2},
	{core.PrimLtFloat, "lt_float", 2},
	{core.PrimLeFloat, "le_float", 2},
	{core.PrimGtFloat, "gt_float", 2},
	{core.PrimGeFloat, "ge_float", 2},
}

func TestPrim(t *testing.T) {
	t.Run("names", func(t *testing.T) {
		for _, row := range allPrims {
			if got := row.p.String(); got != row.name {
				t.Errorf("Prim(%d).String() = %q, want %q", uint8(row.p), got, row.name)
			}
		}
		// The 26 primitives are the values 0 to 25, and each value of Part II
		// follows the one before it: the next one is no primitive, and a value
		// past the table prints without a panic.
		for i, row := range allPrims {
			if int(row.p) != i {
				t.Errorf("%s is the value %d, want %d: the primitives are numbered in the order of allPrims", row.name, row.p, i)
			}
		}
		for _, row := range []struct {
			p    core.Prim
			want string
		}{{core.PrimGeFloat + 1, "Prim(26)"}, {255, "Prim(255)"}} {
			if got := row.p.String(); got != row.want {
				t.Errorf("Prim(%d).String() = %q, want %q", uint8(row.p), got, row.want)
			}
		}
	})

	t.Run("arity", func(t *testing.T) {
		for _, row := range allPrims {
			if got := row.p.Arity(); got != row.arity {
				t.Errorf("%s.Arity() = %d, want %d", row.name, got, row.arity)
			}
		}
	})
}
