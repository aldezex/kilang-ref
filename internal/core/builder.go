package core

import "github.com/aldezex/kilang/internal/base"

// Builder adds expressions to a Program. Each method makes one node, stores it
// in P and returns its id, so a tree is built bottom-up: a node's children
// exist, and have their ids, before the node does. The zero Program is ready
// to use: `&core.Builder{P: &core.Program{}}`.
//
// The Builder does not check what it is given: a VarRef to a variable that
// nothing binds, or a call with the wrong number of arguments, is built as
// asked.
type Builder struct {
	P *Program
}

// add stores e as the next expression and returns its id.
func (b *Builder) add(e Expr) ExprID {
	return b.P.Exprs.Push(e)
}

// LitInt makes the integer literal v.
func (b *Builder) LitInt(sp base.Span, v int64) ExprID {
	return b.add(Expr{Kind: LitInt, Span: sp, Int: v})
}

// LitFloat makes the floating-point literal v.
func (b *Builder) LitFloat(sp base.Span, v float64) ExprID {
	return b.add(Expr{Kind: LitFloat, Span: sp, Float: v})
}

// LitBool makes the literal true or false.
func (b *Builder) LitBool(sp base.Span, v bool) ExprID {
	return b.add(Expr{Kind: LitBool, Span: sp, Bool: v})
}

// LitUnit makes the literal `()`.
func (b *Builder) LitUnit(sp base.Span) ExprID {
	return b.add(Expr{Kind: LitUnit, Span: sp})
}

// VarRef makes a read of the variable v.
func (b *Builder) VarRef(sp base.Span, v VarID) ExprID {
	return b.add(Expr{Kind: VarRef, Span: sp, Var: v})
}

// FunRef makes a reference to the function f.
func (b *Builder) FunRef(sp base.Span, f FunID) ExprID {
	return b.add(Expr{Kind: FunRef, Span: sp, Fun: f})
}

// Call makes a call of callee with args. The Builder copies args into the
// Program's pool, so the caller may reuse the slice.
func (b *Builder) Call(sp base.Span, callee ExprID, args []ExprID) ExprID {
	return b.add(Expr{Kind: Call, Span: sp, Callee: callee, Args: base.AppendRange(&b.P.ExprPool, args)})
}

// Con makes the value of the constructor c built from args, which the Builder
// copies like Call does. It is not checked against the arity of c: Lint does.
func (b *Builder) Con(sp base.Span, c CtorID, args []ExprID) ExprID {
	return b.add(Expr{Kind: Con, Span: sp, Ctor: c, Args: base.AppendRange(&b.P.ExprPool, args)})
}

// Proj makes the read of the field number index of the value of target.
func (b *Builder) Proj(sp base.Span, target ExprID, index uint32) ExprID {
	return b.add(Expr{Kind: Proj, Span: sp, Target: target, Index: index})
}

// Prim makes a call of the primitive p with args, which the Builder copies like
// Call does.
func (b *Builder) Prim(sp base.Span, p Prim, args []ExprID) ExprID {
	return b.add(Expr{Kind: PrimCall, Span: sp, Prim: p, Args: base.AppendRange(&b.P.ExprPool, args)})
}

// Let makes `let v = init; body`: the value of init is bound to v in body.
func (b *Builder) Let(sp base.Span, v VarID, init, body ExprID) ExprID {
	return b.add(Expr{Kind: Let, Span: sp, Var: v, Init: init, Body: body})
}

// Match makes a match on scrutinee with arms, which the Builder copies into the
// Program's arm pool.
func (b *Builder) Match(sp base.Span, scrutinee ExprID, arms []Arm) ExprID {
	return b.add(Expr{Kind: Match, Span: sp, Scrutinee: scrutinee, Arms: base.AppendRange(&b.P.ArmPool, arms)})
}
