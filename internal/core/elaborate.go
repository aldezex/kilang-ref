package core

import (
	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/sema"
	"github.com/aldezex/kilang/internal/syntax"
)

// Elaborate turns the typed syntax trees of a program into Core S0 (spec 7.1,
// stage 7). mods, res and typed are what Resolve and Check received and
// returned, with the prelude as module 0 and the program's own file after it,
// and in is the interner of the names in them. The Program it returns has one
// Fun for each definition, with the id of its definition (FunID(d) is the Fun
// of DefID d), the prelude's first. Main is set from res.
//
// The program must have no errors. Elaborate does not look for them: a call
// with the wrong number of arguments is translated as written. It panics on the
// nodes that stand for an error (an expression or a statement the parser could
// not read, a name that stands for nothing, and an operator on a type it does
// not work on), because it has nothing to translate them to.
//
// What it makes of each construct:
//
//   - A parameter or a `let` is a Var, with the next slot of its function's
//     frame: the slots are 0, 1, 2… in the order the program names the
//     variables, and FrameSize is how many there are. The variables of the
//     blocks inside a function are in its frame too, and nothing reuses a slot.
//   - A block is a chain of Let, one for each statement, and its value is the
//     tail, or `()` if it has none. A statement that is only an expression, `e`,
//     is `let _ = e` with a Var named `_` of its own. A `let` cannot bind that
//     name: the lexer reads `_` as a token of its own and not as an identifier
//     (TestLexer/keywords_and_identifiers), and the parser rejects `let _`
//     (TestParseStmt/found_text_is_the_source_text).
//   - `if` is a Match on the condition with the arms `true` and `false`, in that
//     order; without `else`, the arm `false` is `()`. `a && b` is
//     `match a { true => b; false => false }` and `a || b` is
//     `match a { true => true; false => b }`, so b is read only when a does not
//     settle the result.
//   - An operator is a PrimCall. Its primitive depends on the type of the left
//     operand (the operand of `-`), once resolved: Float gives the Float
//     primitive, Bool (only for `==` and `!=`) the Bool one and Int the Int one,
//     and so does a type variable that nothing has bound (a parameter that
//     nothing fixes, in a function that no one calls with a value). A value of
//     unknown type does not exist at run time, so choosing Int there changes
//     what no program does; chapter 13, where the operators become methods,
//     drops the rule. `%` is Int only.
//   - A name that stands for a definition is a FunRef and one that stands for a
//     variable is a VarRef, wherever it is: the callee of a Call is elaborated
//     like any other expression.
//
// Every node has the span of the syntax it comes from, except these: a PrimCall
// has the span of its operator (OpSpan: that is where the machine reports an
// overflow); a Let has the span of its statement; the Var of `_` is bound at the
// expression of its statement; the nodes that the elaboration adds (the `()` of
// a missing `else` or of a block with no tail, the `true` or `false` of `&&` and
// `||`) have the span of the `if`, the block or the operator expression they
// complete; and a Fun has the span of its name.
//
// The statements of a block are walked with a loop, from the first to elaborate
// the initializers and from the last to the first to build the Let, so a block of
// any number of statements adds no Go frames (TestElaborate/block_is_a_let_chain
// elaborates one of 100 000 with the stack of Go capped at 1 MiB). The
// expressions inside them are elaborated with a recursive call for each level of
// the tree, as Resolve and Check walk it: Parse bounds that depth (MaxNesting
// levels, and the operators and calls of one statement, MaxChain), so a tree the
// parser made is safe; a tree built by hand is not.
//
// It only reads mods, res and typed, apart from this: TypeTable.Resolve
// compresses the paths of the type variables it follows, which changes how the
// table is stored. It does add `_` to in.
func Elaborate(mods []*syntax.Module, res *sema.ResolvedProgram, typed *sema.TypedProgram, in *base.Interner) *Program {
	e := &elaborator{
		mods: mods, res: res, typed: typed,
		underscore: in.InternString("_"),
		p:          &Program{},
		varOf:      make([]VarID, res.Locals.Len()),
	}
	e.b = Builder{P: e.p}
	// The Defs are in DefID order, so each Push gives the id of its Def.
	for _, def := range res.Defs.All() {
		e.p.Funs.Push(e.fun(def))
	}
	e.p.Main, e.p.HasMain = FunID(res.Main), res.HasMain
	return e.p
}

// elaborator is the state of one call to Elaborate.
type elaborator struct {
	mods  []*syntax.Module
	res   *sema.ResolvedProgram
	typed *sema.TypedProgram
	// underscore is the name of the variable that an expression statement binds.
	underscore base.Symbol

	p *Program
	b Builder
	// varOf is indexed by LocalID: the Var that each variable of the program
	// became, set when the variable is bound.
	varOf []VarID

	// The function being elaborated: the tree and the tables of its module, and
	// the number of slots it has used so far.
	m     *syntax.Module
	rm    *sema.ModuleRes
	tm    *sema.TypedModule
	slots uint32
}

// fun makes the Fun of def: its parameters first, so that they have the first
// slots, and then its body, if it has one.
func (e *elaborator) fun(def sema.Def) Fun {
	e.m = e.mods[def.Module]
	e.rm = &e.res.Modules[def.Module]
	e.tm = &e.typed.Modules[def.Module]
	e.slots = 0
	it := e.m.ItemNodes.Get(def.Item)
	f := Fun{
		Name: def.Name, HasBody: it.HasBody, ExternName: def.ExternName,
		Span: def.Span, UserModule: def.Module != sema.PreludeModule,
	}
	for _, l := range e.rm.ParamLocals[def.Item] {
		f.Params = append(f.Params, e.bind(l))
	}
	if it.HasBody {
		f.Body = e.expr(it.Body)
	}
	f.FrameSize = e.slots
	return f
}

// newVar makes a Var with the next slot of the function being elaborated.
func (e *elaborator) newVar(name base.Symbol, sp base.Span) VarID {
	v := e.p.Vars.Push(Var{Name: name, Rep: RepVal, Slot: e.slots, Span: sp})
	e.slots++
	return v
}

// bind makes the Var for the variable l, with its name and the span where it is
// bound, and records it for the names that stand for l.
func (e *elaborator) bind(l sema.LocalID) VarID {
	loc := e.res.Locals.Get(l)
	v := e.newVar(loc.Name, loc.Span)
	e.varOf[l] = v
	return v
}

// expr elaborates the expression id of the current module.
func (e *elaborator) expr(id syntax.ExprID) ExprID {
	x := e.m.Exprs.Get(id)
	switch x.Kind {
	case syntax.ExprInt:
		return e.b.LitInt(x.Span, x.Int)
	case syntax.ExprFloat:
		return e.b.LitFloat(x.Span, x.Float)
	case syntax.ExprBool:
		return e.b.LitBool(x.Span, x.Bool)
	case syntax.ExprUnit:
		return e.b.LitUnit(x.Span)
	case syntax.ExprName:
		switch r := e.rm.ExprRes[id]; r.Kind {
		case sema.ResDef:
			return e.b.FunRef(x.Span, FunID(r.Def))
		case sema.ResLocal:
			return e.b.VarRef(x.Span, e.varOf[r.Local])
		case sema.ResNone, sema.ResError:
			panic("core: cannot elaborate a name that stands for nothing")
		default:
			panic("unreachable")
		}
	case syntax.ExprCall:
		callee := e.expr(x.Callee)
		var args []ExprID
		for _, a := range e.m.ExprList(x.Args) {
			args = append(args, e.expr(a))
		}
		return e.b.Call(x.Span, callee, args)
	case syntax.ExprBinary:
		return e.binary(x)
	case syntax.ExprUnary:
		operand := e.expr(x.Operand)
		switch x.UnOp {
		case syntax.Neg:
			prim := PrimNegInt
			if e.operandType(x.Operand, "-") == opFloat {
				prim = PrimNegFloat
			}
			return e.b.Prim(x.OpSpan, prim, []ExprID{operand})
		case syntax.Not:
			return e.b.Prim(x.OpSpan, PrimNotBool, []ExprID{operand})
		default:
			panic("unreachable")
		}
	case syntax.ExprIf:
		cond := e.expr(x.Cond)
		then := e.expr(x.Then)
		var otherwise ExprID
		if x.HasElse {
			otherwise = e.expr(x.Else)
		} else {
			otherwise = e.b.LitUnit(x.Span)
		}
		return e.b.Match(x.Span, cond, []Arm{{Pat: PatTrue, Body: then}, {Pat: PatFalse, Body: otherwise}})
	case syntax.ExprBlock:
		return e.block(x)
	case syntax.ExprError:
		panic("core: cannot elaborate an expression that could not be read")
	default:
		panic("unreachable")
	}
}

// binary elaborates a binary operator: `&&` and `||` to a Match, the others to
// a PrimCall of the operator's span.
func (e *elaborator) binary(x syntax.Expr) ExprID {
	switch x.Op {
	case syntax.BinAnd:
		return e.shortCircuit(x, false)
	case syntax.BinOr:
		return e.shortCircuit(x, true)
	}
	prim := e.binaryPrim(x)
	lhs, rhs := e.expr(x.Lhs), e.expr(x.Rhs)
	return e.b.Prim(x.OpSpan, prim, []ExprID{lhs, rhs})
}

// shortCircuit elaborates `a && b` (settledBy false) or `a || b` (settledBy
// true) to a Match on a. When a is settledBy the result is that literal, which
// has the span of the whole expression; otherwise it is b, which the Match
// reads only then.
func (e *elaborator) shortCircuit(x syntax.Expr, settledBy bool) ExprID {
	lhs, rhs := e.expr(x.Lhs), e.expr(x.Rhs)
	lit := e.b.LitBool(x.Span, settledBy)
	// `&&` goes on to b when a is true and is settled when it is false; `||`
	// the other way round.
	onTrue, onFalse := rhs, lit
	if settledBy {
		onTrue, onFalse = lit, rhs
	}
	return e.b.Match(x.Span, lhs, []Arm{{Pat: PatTrue, Body: onTrue}, {Pat: PatFalse, Body: onFalse}})
}

// opType is the type that an operator works on, as far as the choice of its
// primitive goes.
type opType uint8

const (
	opInt opType = iota
	opFloat
	opBool
)

// operandType returns the opType of the expression operand of the operator op,
// from the type that the checker found for it: Float is opFloat, Bool is opBool,
// and Int is opInt, and so is a type variable that nothing has bound (see
// Elaborate). Any other type has no primitive, and neither has Bool for an
// operator that is not `==` or `!=`: the checker reports both, and a program
// with errors is not elaborated, so it panics with the text that the report of
// the ICE shows.
func (e *elaborator) operandType(operand syntax.ExprID, op string) opType {
	types := e.typed.Types
	ty := types.Resolve(e.tm.ExprTypes[operand])
	switch {
	case ty == types.Builtin(sema.BuiltinFloat):
		return opFloat
	case ty == types.Builtin(sema.BuiltinBool) && (op == "==" || op == "!="):
		return opBool
	case ty == types.Builtin(sema.BuiltinInt), types.Node(ty).Kind == sema.TypeVariable:
		return opInt
	default:
		panic("core: cannot elaborate `" + op + "` on a value of type `" + types.Show(ty) + "`")
	}
}

// binaryPrims is the primitive of each binary operator that is not `&&` or
// `||`, for each opType: the Int one, the Float one and, for `==` and `!=`, the
// Bool one. A zero entry (PrimAddInt is 0, so the zero Prim is a real one)
// would be a mistake of the table: operandType never returns opBool for the
// operators that have none, and `%` has no Float.
var binaryPrims = map[syntax.BinOp][3]Prim{
	syntax.BinAdd: {opInt: PrimAddInt, opFloat: PrimAddFloat},
	syntax.BinSub: {opInt: PrimSubInt, opFloat: PrimSubFloat},
	syntax.BinMul: {opInt: PrimMulInt, opFloat: PrimMulFloat},
	syntax.BinDiv: {opInt: PrimDivInt, opFloat: PrimDivFloat},
	syntax.BinRem: {opInt: PrimRemInt},
	syntax.BinLt:  {opInt: PrimLtInt, opFloat: PrimLtFloat},
	syntax.BinLe:  {opInt: PrimLeInt, opFloat: PrimLeFloat},
	syntax.BinGt:  {opInt: PrimGtInt, opFloat: PrimGtFloat},
	syntax.BinGe:  {opInt: PrimGeInt, opFloat: PrimGeFloat},
	syntax.BinEq:  {opInt: PrimEqInt, opFloat: PrimEqFloat, opBool: PrimEqBool},
	syntax.BinNe:  {opInt: PrimNeInt, opFloat: PrimNeFloat, opBool: PrimNeBool},
}

// binaryPrim returns the primitive of an operator that is not `&&` or `||`.
func (e *elaborator) binaryPrim(x syntax.Expr) Prim {
	ty := opInt
	if x.Op != syntax.BinRem {
		ty = e.operandType(x.Lhs, x.Op.String())
	}
	prims, ok := binaryPrims[x.Op]
	if !ok {
		panic("unreachable")
	}
	return prims[ty]
}

// block elaborates a block to a chain of Let. The first loop goes through the
// statements in order, so that the variables take their slots in the order the
// program names them (a `let` takes its slot before the blocks inside its
// initializer do), and keeps what each Let needs. The second builds the chain
// from the last statement to the first, so that the body of the Let of
// statement k is the Let of statement k+1 and the last one holds the tail.
func (e *elaborator) block(x syntax.Expr) ExprID {
	type binding struct {
		sp   base.Span
		v    VarID
		init ExprID
	}
	stmts := e.m.StmtList(x.Stmts)
	binds := make([]binding, 0, len(stmts))
	for _, id := range stmts {
		s := e.m.Stmts.Get(id)
		switch s.Kind {
		case syntax.StmtLet:
			v := e.bind(e.rm.LetLocal[id])
			binds = append(binds, binding{s.Span, v, e.expr(s.Expr)})
		case syntax.StmtExpr:
			v := e.newVar(e.underscore, s.Span)
			binds = append(binds, binding{s.Span, v, e.expr(s.Expr)})
		case syntax.StmtError:
			panic("core: cannot elaborate a statement that could not be read")
		default:
			panic("unreachable")
		}
	}
	var body ExprID
	if x.HasTail {
		body = e.expr(x.Tail)
	} else {
		body = e.b.LitUnit(x.Span)
	}
	for i := len(binds) - 1; i >= 0; i-- {
		body = e.b.Let(binds[i].sp, binds[i].v, binds[i].init, body)
	}
	return body
}
