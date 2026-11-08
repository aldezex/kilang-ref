package core

import (
	"slices"
	"strconv"

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
//     is `let _ = e` with a Var named `_` of its own. A `let` of a plain name
//     cannot bind that name: the lexer reads `_` as a token of its own and not as
//     an identifier (TestLexer/keywords_and_identifiers). The `let _ = e` that
//     the program writes is a pattern `let`, whose wildcard binds nothing: it
//     is a Match of one arm (TestPatternCore/let_with_a_pattern_is_a_match_of_one_arm).
//   - `if` is a Match on the condition with the arms `true` and `false`, in that
//     order, whose patterns are a PatTrue and a PatFalse of their own, with the
//     span of the `if`; without `else`, the arm `false` is `()`. `a && b` is
//     `match a { true => b; false => false }` and `a || b` is
//     `match a { true => true; false => b }`, so b is read only when a does not
//     settle the result.
//   - A `match` is a Match with the arms in the order they are written, and an
//     arm keeps its guard. A pattern is a Pat: `_` is a PatWildcard, a variable a
//     PatBind, an Int literal a PatInt, `true` and `false` a PatTrue and a
//     PatFalse, a constructor a PatCon of its Ctor (with a PatWildcard for each
//     field that a record pattern leaves out, `..` or not given, and the fields
//     in the order the record declares them), a tuple a PatCon of the tuple
//     constructor of its arity, and `p | q` a PatOr. The variables of a pattern
//     take the next slots as the program names them, before the arm's guard and
//     body; an or-pattern binds a name once, in the first alternative that has
//     it, and the others reuse that Var. A `let` with a pattern, `let (a, b) = e`,
//     is a Match of a single arm whose body is the rest of the block: the
//     checker has made sure the pattern fits every value (E0702), so no other
//     arm is needed.
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
//   - A type and a constructor are definitions too, so each is a Fun, for
//     FunID(d) to be the Fun of DefID d. The Fun of a type has no code to run: its
//     body is `()`, a filler. The Fun of a constructor is its wrapper, the
//     function that `Circle` is when it is written without a call: it takes one
//     parameter for each field, named as the field is (`a0`, `a1`… for a field
//     with no name), and its body is the Con of the constructor on those
//     parameters.
//   - A call of a constructor is a Con of its arguments, which the machine
//     evaluates from left to right. A constructor with no fields is a Con with no
//     arguments wherever it is written, because it is a value and not a function;
//     one with fields that is written without a call is a FunRef to its wrapper,
//     as a function's name is. Program.Ctors has one Ctor for each constructor of
//     the declarations, in their order, so CtorID(c) is sema.CtorID(c): its Arity
//     is the number of fields. After those come the constructors that no variant
//     declares: one for each record, in the order of the declarations (Record, with
//     the name of the record and the names of the fields), and one for each arity of
//     tuple that the program writes, in the order the elaboration meets them
//     (Tuple, with no name).
//   - A record literal is a Con of the constructor of its record, with the values in
//     the order the record declares the fields, `p.x` is a Proj of the number of
//     the field x, and `t.0` is a Proj of 0. A tuple is a Con of the tuple
//     constructor of its arity. An update, `Point { x: 3.0, ..p }`, is a new Con:
//     the base is read once, and a field that the literal does not give is a Proj
//     of the base (the machine does not update a value in place). The values of the
//     literal are evaluated in the order they are written, and the base after them,
//     as the arguments of a call are, which a Con alone would not do for a literal
//     that writes the fields in another order than they are declared in: when the
//     literal has a base, or writes them out of order, each value is bound to a
//     variable named as its field first, in the order they are written, the base is
//     bound to `base` after them, and the Con and its Projs read the variables.
//
// Every node has the span of the syntax it comes from, except these: a PrimCall
// has the span of its operator (OpSpan: that is where the machine reports an
// overflow); a Let has the span of its statement; the Var of `_` is bound at the
// expression of its statement; the nodes that the elaboration adds (the `()` of
// a missing `else` or of a block with no tail, the `true` or `false` of `&&` and
// `||`) have the span of the `if`, the block or the operator expression they
// complete, and so do the PatTrue and PatFalse of the arms of an `if`; a Match
// that a `let` with a pattern makes has the span of the statement; the PatBind of
// a pun in a record pattern has the span of the name, and the PatWildcard of a
// field that a record pattern leaves out the span of the whole pattern; a Fun has
// the span of its name; and everything in the Fun of a constructor (its
// parameters, their reads and the Con) has the span of the constructor's name, the
// only place it has.
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
// table is stored. It does add `_` and `base` to in, and `a0`, `a1`… when a
// constructor has a field with no name.
func Elaborate(mods []*syntax.Module, res *sema.ResolvedProgram, typed *sema.TypedProgram, in *base.Interner) *Program {
	e := &elaborator{
		mods: mods, res: res, typed: typed, in: in,
		underscore: in.InternString("_"),
		baseName:   in.InternString("base"),
		p:          &Program{},
		records:    make(map[sema.TypeDefID]CtorID),
		tuples:     make(map[int]CtorID),
		varOf:      make([]VarID, res.Locals.Len()),
		made:       make([]bool, res.Locals.Len()),
	}
	e.b = Builder{P: e.p}
	// The constructors first, so that every Con the functions build has its Ctor.
	for _, c := range typed.Decls.Ctors.All() {
		e.p.Ctors.Push(Ctor{Name: c.Name, Tag: c.Tag, Arity: uint32(len(c.Args)), Fields: slices.Clone(c.Fields)})
	}
	// Then the one of each record, which has no variant: they follow the declared
	// ones, so that the numbers of those do not change.
	for id, td := range typed.Decls.Defs.All() {
		if td.Form != syntax.TypeFormRecord {
			continue
		}
		names := make([]base.Symbol, len(td.Fields))
		for i, f := range td.Fields {
			names[i] = f.Name
		}
		e.records[id] = e.p.Ctors.Push(Ctor{Name: td.Name, Arity: uint32(len(td.Fields)), Record: true, Fields: names})
	}
	// The Defs are in DefID order, so each Push gives the id of its Def.
	for id, def := range res.Defs.All() {
		e.p.Funs.Push(e.fun(id, def))
	}
	e.p.Main, e.p.HasMain = FunID(res.Main), res.HasMain
	return e.p
}

// elaborator is the state of one call to Elaborate.
type elaborator struct {
	mods  []*syntax.Module
	res   *sema.ResolvedProgram
	typed *sema.TypedProgram
	in    *base.Interner
	// underscore is the name of the variable that an expression statement binds,
	// and baseName that of the variable that an update binds its base to.
	underscore, baseName base.Symbol
	// records is the constructor of each record, and tuples the one of each arity
	// of tuple that has been written so far, which tupleCtor adds when it is first
	// needed.
	records map[sema.TypeDefID]CtorID
	tuples  map[int]CtorID

	p *Program
	b Builder
	// varOf is indexed by LocalID: the Var that each variable of the program
	// became, set when the variable is bound. A LocalID can be bound more than
	// once only by the alternatives of an or-pattern, which share it, and made[l]
	// says whether varOf[l] is set (VarID 0 is a real Var, so the zero value of
	// varOf cannot tell).
	varOf []VarID
	made  []bool

	// The function being elaborated: the tree and the tables of its module, and
	// the number of slots it has used so far.
	m     *syntax.Module
	rm    *sema.ModuleRes
	tm    *sema.TypedModule
	slots uint32
}

// fun makes the Fun of def, the definition id: its parameters first, so that they
// have the first slots, and then its body, if it has one.
func (e *elaborator) fun(id sema.DefID, def sema.Def) Fun {
	e.m = e.mods[def.Module]
	e.rm = &e.res.Modules[def.Module]
	e.tm = &e.typed.Modules[def.Module]
	e.slots = 0
	switch def.Kind {
	case sema.DefType:
		return Fun{
			Name: def.Name, HasBody: true, Body: e.b.LitUnit(def.Span),
			Span: def.Span, UserModule: def.Module != sema.PreludeModule,
		}
	case sema.DefCtor:
		return e.wrapper(id, def)
	}
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

// wrapper makes the Fun of the constructor that the definition id is: the
// function that the constructor is when it is not called, with a parameter for
// each field and the Con of the constructor on them as its body. A parameter is
// bound at the name of the constructor, which is the only place the Fun has.
func (e *elaborator) wrapper(id sema.DefID, def sema.Def) Fun {
	c, _ := e.typed.Decls.CtorOf(id)
	ctor := e.typed.Decls.Ctors.Get(c)
	f := Fun{Name: def.Name, HasBody: true, Span: def.Span, UserModule: def.Module != sema.PreludeModule}
	args := make([]ExprID, len(ctor.Args))
	for i := range ctor.Args {
		name := e.in.InternString("a" + strconv.Itoa(i))
		if ctor.Fields != nil && ctor.Fields[i] != base.NoSymbol {
			name = ctor.Fields[i]
		}
		v := e.newVar(name, def.Span)
		f.Params = append(f.Params, v)
		args[i] = e.b.VarRef(def.Span, v)
	}
	f.Body = e.b.Con(def.Span, CtorID(c), args)
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
	e.made[l] = true
	return v
}

// bindPattern is bind for a variable that a pattern binds: the alternatives of an
// or-pattern bind the same variable, and only the first makes its Var, which the
// others reuse.
func (e *elaborator) bindPattern(l sema.LocalID) VarID {
	if e.made[l] {
		return e.varOf[l]
	}
	return e.bind(l)
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
			// A constructor with no fields is a value and not a function.
			if c, ok := e.ctorOf(id); ok && e.p.Ctors.Get(c).Arity == 0 {
				return e.b.Con(x.Span, c, nil)
			}
			return e.b.FunRef(x.Span, FunID(r.Def))
		case sema.ResLocal:
			return e.b.VarRef(x.Span, e.varOf[r.Local])
		case sema.ResNone, sema.ResError:
			panic("core: cannot elaborate a name that stands for nothing")
		default:
			panic("unreachable")
		}
	case syntax.ExprCall:
		c, isCtor := e.ctorOf(x.Callee)
		var callee ExprID
		if !isCtor {
			callee = e.expr(x.Callee)
		}
		var args []ExprID
		for _, a := range e.m.ExprList(x.Args) {
			args = append(args, e.expr(a))
		}
		if isCtor {
			return e.b.Con(x.Span, c, args)
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
		return e.b.Match(x.Span, cond, e.boolArms(x.Span, then, otherwise))
	case syntax.ExprBlock:
		return e.block(x)
	case syntax.ExprTuple:
		args := make([]ExprID, 0, x.Args.Count)
		for _, a := range e.m.ExprList(x.Args) {
			args = append(args, e.expr(a))
		}
		return e.b.Con(x.Span, e.tupleCtor(len(args)), args)
	case syntax.ExprTupleIndex:
		return e.b.Proj(x.Span, e.expr(x.Target), x.Index)
	case syntax.ExprField:
		td := e.recordOf(x.Target)
		i, ok := e.typed.Decls.FieldIndex(td, x.Name)
		if !ok {
			panic("core: cannot elaborate a field that the record does not have")
		}
		return e.b.Proj(x.Span, e.expr(x.Target), uint32(i))
	case syntax.ExprRecord:
		return e.record(id, x)
	case syntax.ExprMatch:
		return e.match(x)
	case syntax.ExprError:
		panic("core: cannot elaborate an expression that could not be read")
	default:
		panic("unreachable")
	}
}

// match elaborates the `match` x to a Match with its arms in the order they are
// written. The scrutinee comes first, then, for each arm, its pattern (which
// binds the variables), its guard and its body, so the slots follow the order of
// the source. The arms are a loop, so a `match` of any number of arms adds no Go
// frames; what recurses is the pattern of an arm, once per level of nesting, which
// the parser bounds (MaxPatternDepth).
func (e *elaborator) match(x syntax.Expr) ExprID {
	scrutinee := e.expr(x.Scrutinee)
	written := e.m.Arms(x.Arms)
	arms := make([]Arm, 0, len(written))
	for _, a := range written {
		arm := Arm{Pat: e.pat(a.Pat)}
		if a.HasGuard {
			arm.Guard, arm.HasGuard = e.expr(a.Guard), true
		}
		arm.Body = e.expr(a.Body)
		arms = append(arms, arm)
	}
	return e.b.Match(x.Span, scrutinee, arms)
}

// pat elaborates the pattern id of the current module to a Pat (see the list in
// Elaborate), binding the variables it names as it meets them, left to right. The
// patterns that hold patterns walk them with a loop and call pat for each, so what
// the depth of the Go stack follows is the nesting of the pattern, and not its
// width: a tuple pattern of a hundred thousand elements is as deep as one of two.
// A tree built by hand is not bounded.
func (e *elaborator) pat(id syntax.PatID) PatID {
	p := e.m.Pats.Get(id)
	switch p.Kind {
	case syntax.PatWildcard:
		return e.b.Pat(Pat{Kind: PatWildcard, Span: p.Span})
	case syntax.PatBind:
		return e.b.Pat(Pat{Kind: PatBind, Span: p.Span, Var: e.bindPattern(e.rm.PatLocal[id])})
	case syntax.PatLit:
		switch p.Lit {
		case syntax.LitInt:
			return e.b.Pat(Pat{Kind: PatInt, Span: p.Span, Int: p.Int})
		case syntax.LitBool:
			kind := PatFalse
			if p.Bool {
				kind = PatTrue
			}
			return e.b.Pat(Pat{Kind: kind, Span: p.Span})
		default:
			panic("core: cannot elaborate a pattern that is a literal of a type that has no Core yet")
		}
	case syntax.PatTuple:
		args := e.pats(e.m.PatList(p.Args))
		return e.b.Pat(Pat{Kind: PatCon, Span: p.Span, Ctor: e.tupleCtor(len(args)), Args: base.AppendRange(&e.p.PatPool, args)})
	case syntax.PatCtor:
		if p.Record {
			return e.recordPat(id, p)
		}
		c, ok := e.typed.Decls.CtorOf(e.rm.PatRes[id].Def)
		if !ok {
			panic("core: cannot elaborate a constructor pattern whose name is not a constructor")
		}
		args := e.pats(e.m.PatList(p.Args))
		return e.b.Pat(Pat{Kind: PatCon, Span: p.Span, Ctor: CtorID(c), Args: base.AppendRange(&e.p.PatPool, args)})
	case syntax.PatOr:
		args := e.pats(e.m.PatList(p.Args))
		return e.b.Pat(Pat{Kind: PatOr, Span: p.Span, Args: base.AppendRange(&e.p.PatPool, args)})
	case syntax.PatList:
		panic("core: cannot elaborate a list pattern")
	case syntax.PatError:
		panic("core: cannot elaborate a pattern that could not be read")
	default:
		panic("unreachable")
	}
}

// pats elaborates the patterns ids, in order.
func (e *elaborator) pats(ids []syntax.PatID) []PatID {
	out := make([]PatID, len(ids))
	for i, id := range ids {
		out[i] = e.pat(id)
	}
	return out
}

// recordPat elaborates the record pattern p, whose id is id, to a PatCon of the
// constructor of its record with a pattern for each field in the order the record
// declares them: the fields are met in the order they are written, so that their
// variables take the slots in that order, and a field that the pattern does not
// name is a PatWildcard, which is what `..` allows. A pun, `Point { x }`, is a
// PatBind of the variable that Resolve made for it. The checker has made sure that
// each field is named at most once and that every field is named unless `..` says
// the rest do not matter.
func (e *elaborator) recordPat(id syntax.PatID, p syntax.Pat) PatID {
	td := e.recordType(e.tm.PatTypes[id], "a record pattern on a value")
	ctor := e.records[td]
	args := make([]PatID, e.p.Ctors.Get(ctor).Arity)
	// PatID 0 is a real pattern, so a slice says which fields the pattern names.
	named := make([]bool, len(args))
	for k, f := range e.m.FieldPats(p.Fields) {
		i, ok := e.typed.Decls.FieldIndex(td, f.Name)
		if !ok {
			panic("core: cannot elaborate a field that the record does not have")
		}
		named[i] = true
		if f.HasPat {
			args[i] = e.pat(f.Pat)
		} else {
			l := e.rm.FieldPatLocal[int(p.Fields.First)+k]
			args[i] = e.b.Pat(Pat{Kind: PatBind, Span: f.Span, Var: e.bindPattern(l)})
		}
	}
	for i := range args {
		if !named[i] {
			args[i] = e.b.Pat(Pat{Kind: PatWildcard, Span: p.Span})
		}
	}
	return e.b.Pat(Pat{Kind: PatCon, Span: p.Span, Ctor: ctor, Args: base.AppendRange(&e.p.PatPool, args)})
}

// tupleCtor returns the constructor of the tuples of n elements, making it the
// first time: it has no name and no tag, it is Tuple, and it comes after the
// constructors that are there already.
func (e *elaborator) tupleCtor(n int) CtorID {
	if c, ok := e.tuples[n]; ok {
		return c
	}
	c := e.p.Ctors.Push(Ctor{Arity: uint32(n), Tuple: true})
	e.tuples[n] = c
	return c
}

// recordOf returns the record that the type of the expression id of the current
// module is. The checker has made sure that it is one.
func (e *elaborator) recordOf(id syntax.ExprID) sema.TypeDefID {
	return e.recordType(e.tm.ExprTypes[id], "the field of a value")
}

// recordType returns the record that the type ty is, and panics, saying that it
// cannot elaborate what, if it is not one.
func (e *elaborator) recordType(ty sema.TypeID, what string) sema.TypeDefID {
	types := e.typed.Types
	n := types.Node(types.Resolve(ty))
	if n.Kind != sema.TypeData || e.typed.Decls.Defs.Get(n.Def).Form != syntax.TypeFormRecord {
		panic("core: cannot elaborate " + what + " that is not a record: `" + types.Show(ty) + "`")
	}
	return n.Def
}

// record elaborates the record literal x, whose id is id. The literal is a Con of
// the constructor of its record when its values can go straight to it: it has no
// base, and it writes the fields in the order they are declared in, so the Con
// evaluates them in the order they are written. Otherwise each value is bound to a
// variable, named as its field, in the order they are written, then the base to
// `base`, and the Con takes the variable of each field it was given and a Proj of
// the base for each it was not.
//
// A literal that was accepted gives each field of the record at most once, which
// is what lets it say, for each declared field, which written one it is.
func (e *elaborator) record(id syntax.ExprID, x syntax.Expr) ExprID {
	td := e.recordOf(id)
	ctor := e.records[td]
	fields := e.m.FieldInits(x.Fields)
	// at[i] is the position, among the fields as they are written, of the one that
	// gives the declared field i, or -1 if the literal does not give it.
	at := make([]int, e.p.Ctors.Get(ctor).Arity)
	for i := range at {
		at[i] = -1
	}
	inOrder := true
	for k, f := range fields {
		i, ok := e.typed.Decls.FieldIndex(td, f.Name)
		if !ok {
			panic("core: cannot elaborate a field that the record does not have")
		}
		at[i] = k
		inOrder = inOrder && i == k
	}
	if !x.HasBase && inOrder {
		args := make([]ExprID, len(fields))
		for k, f := range fields {
			args[k] = e.expr(f.Value)
		}
		return e.b.Con(x.Span, ctor, args)
	}

	type binding struct {
		sp   base.Span
		v    VarID
		init ExprID
	}
	binds := make([]binding, 0, len(fields)+1)
	vars := make([]VarID, len(fields))
	for k, f := range fields {
		v := e.newVar(f.Name, f.Span)
		vars[k] = v
		sp := base.Span{File: f.Span.File, Lo: f.Span.Lo, Hi: e.m.Exprs.Get(f.Value).Span.Hi}
		binds = append(binds, binding{sp, v, e.expr(f.Value)})
	}
	var baseVar VarID
	if x.HasBase {
		baseVar = e.newVar(e.baseName, e.m.Exprs.Get(x.Base).Span)
		binds = append(binds, binding{e.m.Exprs.Get(x.Base).Span, baseVar, e.expr(x.Base)})
	}
	args := make([]ExprID, len(at))
	for i, k := range at {
		if k >= 0 {
			args[i] = e.b.VarRef(fields[k].Span, vars[k])
		} else {
			args[i] = e.b.Proj(x.Span, e.b.VarRef(e.m.Exprs.Get(x.Base).Span, baseVar), uint32(i))
		}
	}
	body := e.b.Con(x.Span, ctor, args)
	for i := len(binds) - 1; i >= 0; i-- {
		body = e.b.Let(binds[i].sp, binds[i].v, binds[i].init, body)
	}
	return body
}

// ctorOf returns the constructor that the expression id is the name of, and
// whether it is one.
func (e *elaborator) ctorOf(id syntax.ExprID) (CtorID, bool) {
	if e.m.Exprs.Get(id).Kind != syntax.ExprName {
		return 0, false
	}
	r := e.rm.ExprRes[id]
	if r.Kind != sema.ResDef {
		return 0, false
	}
	c, ok := e.typed.Decls.CtorOf(r.Def)
	return CtorID(c), ok
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
	return e.b.Match(x.Span, lhs, e.boolArms(x.Span, onTrue, onFalse))
}

// boolArms makes the two arms of an `if` or of `&&` and `||`: `true => onTrue`
// and then `false => onFalse`, each with a pattern node of its own that has the
// span sp.
func (e *elaborator) boolArms(sp base.Span, onTrue, onFalse ExprID) []Arm {
	return []Arm{
		{Pat: e.b.Pat(Pat{Kind: PatTrue, Span: sp}), Body: onTrue},
		{Pat: e.b.Pat(Pat{Kind: PatFalse, Span: sp}), Body: onFalse},
	}
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
	// A statement whose pattern is not a plain name has isPat set and the pattern
	// in pat, and v means nothing.
	type binding struct {
		sp    base.Span
		v     VarID
		init  ExprID
		pat   PatID
		isPat bool
	}
	stmts := e.m.StmtList(x.Stmts)
	binds := make([]binding, 0, len(stmts))
	for _, id := range stmts {
		s := e.m.Stmts.Get(id)
		switch s.Kind {
		case syntax.StmtLet:
			if s.HasPat {
				// The names of the pattern come before the initializer's blocks, as
				// the name of a plain `let` does.
				pat := e.pat(s.Pat)
				binds = append(binds, binding{sp: s.Span, init: e.expr(s.Expr), pat: pat, isPat: true})
				continue
			}
			v := e.bind(e.rm.LetLocal[id])
			binds = append(binds, binding{sp: s.Span, v: v, init: e.expr(s.Expr)})
		case syntax.StmtExpr:
			v := e.newVar(e.underscore, s.Span)
			binds = append(binds, binding{sp: s.Span, v: v, init: e.expr(s.Expr)})
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
		if b := binds[i]; b.isPat {
			body = e.b.Match(b.sp, b.init, []Arm{{Pat: b.pat, Body: body}})
		} else {
			body = e.b.Let(b.sp, b.v, b.init, body)
		}
	}
	return body
}
