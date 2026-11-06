package sema

import (
	"slices"
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// TypedModule is what Check learns about the expressions of one module, in a
// table beside its syntax tree, as ModuleRes is for names.
type TypedModule struct {
	// ExprTypes is indexed by ExprID: the type of each expression. A type here
	// can be a variable that was bound after the expression was checked, so
	// ask TypeTable.Resolve (or Show) for what it stands for. The row of an
	// expression that Check never reached, such as one that the parser dropped
	// when it recovered from a syntax error, is the error type.
	ExprTypes []TypeID
}

// TypedProgram is the result of Check.
type TypedProgram struct {
	// Types holds every type that the tables below name: it is the table of
	// Decls, which Declare made and Check adds to.
	Types *TypeTable
	// Decls is the declarations that Check was given.
	Decls *Decls
	// Modules has one entry for each module, in the order of the modules.
	Modules []TypedModule
	// DefTypes is the type of each definition, indexed by DefID: a function
	// type.
	DefTypes base.IdVec[DefID, TypeID]
	// LocalTypes is the type of each variable, indexed by LocalID.
	LocalTypes base.IdVec[LocalID, TypeID]
}

// Check finds the type of every expression, variable and definition of a
// program, and reports to sink what does not fit. mods and res are what Resolve
// received and returned, and the prelude is module 0. Whether the program has
// errors already does not matter: whatever an earlier pass could not make
// sense of has the error type, which never gives an error of its own (see
// Unifier.Unify), so one mistake is reported once.
//
// The checker is bidirectional: infer works out the type of an expression from
// what is inside it, and check is given the type that the place expects. The
// two meet at a call to Unify, which reports the E0401 with the reason for the
// expectation. A block that is checked hands the expectation to its tail, so
// the error lands on the expression that is wrong and not on the whole block.
// An `if` with no `else` is `()`, and when that is what the place expects its
// branch is checked against `()`; an `if` with an `else` infers its `then`
// branch, checks the `else` branch against it and unifies the result with the
// expectation, on the whole `if` (see checkIf).
//
// Signatures come first. Every function gets its type from its annotations:
// a parameter without one gets a new variable, and a function without `->`
// returns `()`. A type name is a type that the program declares (the module's
// own, or a `pub` one of the prelude: see ResolvedProgram.LookupType), or `Int`,
// `Float`, `Bool`, or `()`; any other is E0406 (an unknown type), reported once
// where it is written, and has the error type. A declared type is the type that
// Declare made of it (a sum or a record, or what an alias expands to), and a
// constructor is a value of the type of Decls.CtorType: a function from its
// fields to its type, or its type itself when it has no fields, so `Circle(1.0)`
// is a call like any other and `Circle` can be passed as a function. A name that
// is in no scope has the error type.
// Then `main`, if the user's file has one, is held to its type, `() -> ()`
// (E0404). Only then are the bodies checked, each against the return type of
// its function (the tail of a body is the reason ReasonBlockTail). All the
// functions of the program are one group whose variables are not generalized
// (chapter 13 does that), so what a call or a body finds out about a parameter
// is what every other use of the function sees.
//
// The operators are fixed by the type of their operands, and the left operand
// says which: `+ - *` and `/` take two Int and give an Int, or two Float and
// give a Float; `< <= > >=` take two Int or two Float and give a Bool; `%` takes
// two Int and gives an Int, and has no Float; `&& ||` take and give Bool; `-` is
// Int to Int or Float to Float and `!` Bool to Bool. `==` and `!=` compare two
// values of one type, and that type has to be Int, Bool or Float. When the left
// operand has no type yet, which of the types it is waits until every body has
// been checked (see numeric and equality). If an operand has the error type, so
// does the result. A call is checked against the
// type of its callee: E0403 if it is not a function, E0402 if the number of
// arguments is not the number of parameters (unless an argument is missing,
// which the parser has reported: see call). A callee whose type is a variable
// that nothing has bound yet (a parameter without an annotation) is made a
// function of the types of the arguments, unless an argument is missing (see
// call). The error type of an unknown type name gives no complaint of its own
// wherever it is used, but it is still part of the types around it, so a
// message about another mistake can write it as `{error}` (a function with a
// parameter of an unknown type, used where an `Int` is expected, is a mismatch
// with `({error}) -> ()`).
//
// infer and check call each other once per level of the tree, like Resolve:
// Parse bounds that depth (MaxNesting levels, and the operators and calls of
// one statement, MaxChain), so a tree the parser made is safe; a tree built by
// hand is not.
func Check(mods []*syntax.Module, res *ResolvedProgram, decls *Decls, in *base.Interner, sink *diag.Sink) *TypedProgram {
	tbl := decls.Types
	c := &checker{
		mods: mods, res: res, in: in, sink: sink, decls: decls,
		reader: typeReader{mods: mods, res: res, in: in, sink: sink, decls: decls},
		tp:     &TypedProgram{Types: tbl, Decls: decls, Modules: make([]TypedModule, len(mods))},
		u:      Unifier{Types: tbl, Sink: sink},
		intT:   tbl.Builtin(BuiltinInt),
		boolT:  tbl.Builtin(BuiltinBool),
		unitT:  tbl.Builtin(BuiltinUnit),
		floatT: tbl.Builtin(BuiltinFloat),
		errT:   tbl.Error(),
	}
	c.signatures()
	c.mainSignature()
	c.bodies()
	c.judgePending()
	return c.tp
}

// checker is the state of one call to Check.
type checker struct {
	mods []*syntax.Module
	res  *ResolvedProgram
	in   *base.Interner
	sink *diag.Sink
	tp   *TypedProgram
	u    Unifier
	// decls is what Declare made, and reader reads the types that annotations
	// write with it.
	decls  *Decls
	reader typeReader

	// The builtin types and the error type, which every rule uses.
	intT, boolT, unitT, floatT, errT TypeID

	// The module whose bodies are being checked, its tree, its names and its
	// table of types (a slice of tp.Modules, so a write here is a write there).
	mod       ModuleIdx
	m         *syntax.Module
	rm        *ModuleRes
	exprTypes []TypeID

	// pending are the operators whose left operand had no type yet when the body
	// was checked, to be judged once all the bodies are (see numeric).
	pending []pendingOperand
	// rejected holds the variables whose type an operator has refused already: a
	// variable that an operator refuses is one mistake, however many operators
	// have it as an operand (see rejectOperand). The variable is the one the
	// operand is as it was written: two parameters that a comparison made equal
	// are two variables, and each is reported (see firstRejection).
	rejected map[TypeID]bool
}

// operandSet says which types an operator works on.
type operandSet uint8

const (
	// onNumbers is Int and Float: `+ - * /`, the four orderings and `-`.
	onNumbers operandSet = iota
	// onEquatable is Int, Bool and Float: `==` and `!=`.
	onEquatable
	// onIntegers is Int alone: `%`, which has no Float.
	onIntegers
)

// pendingOperand is a left operand of an operator that was a variable nothing
// had bound yet: operand is its type, at where it is written, op the operator
// and set the types the operator works on. reason is only used by `%`, which
// explains its mistake with the operator as the reason (see remainder).
type pendingOperand struct {
	operand TypeID
	at      base.Span
	op      string
	set     operandSet
	reason  Reason
}

// ifKeywordLen is the length of the `if` keyword in bytes.
const ifKeywordLen = 2

// signatures is the first pass: it makes the type of every definition and of
// every parameter, and a row of the error type for everything else, so that
// the second pass has a valid slot to write for any id of the program.
func (c *checker) signatures() {
	for i, m := range c.mods {
		types := make([]TypeID, m.Exprs.Len())
		for j := range types {
			types[j] = c.errT
		}
		c.tp.Modules[i] = TypedModule{ExprTypes: types}
	}
	for range c.res.Locals.All() {
		c.tp.LocalTypes.Push(c.errT)
	}
	// The definitions are in DefID order, so each Push gives the id of its Def.
	for id, def := range c.res.Defs.All() {
		c.tp.DefTypes.Push(c.signature(id, def))
	}
}

// signature returns the function type of def, and gives its parameters their
// types. A parameter with an annotation has that type; one without gets a new
// variable. A function without `->` returns `()`. A constructor has the type
// that Decls.CtorType says. The definition of a type has the type that its name
// stands for (see Decls.Meaning): no expression is the name of a type, so nothing
// reads it as the type of a value.
func (c *checker) signature(id DefID, def Def) TypeID {
	switch def.Kind {
	case DefType:
		td, _ := c.decls.TypeDefOf(id)
		return c.decls.Meaning(td)
	case DefCtor:
		ctor, _ := c.decls.CtorOf(id)
		return c.decls.CtorType(ctor)
	}
	m := c.mods[def.Module]
	it := m.ItemNodes.Get(def.Item)
	locals := c.res.Modules[def.Module].ParamLocals[def.Item]
	var params []TypeID
	for i, p := range m.Params(it.Params) {
		var ty TypeID
		if p.HasType {
			ty = c.typeOf(def.Module, p.Type)
		} else {
			ty = c.tp.Types.FreshVar(Origin{
				Span: c.res.Locals.Get(locals[i]).Span,
				Note: "parameter `" + c.in.String(p.Name) + "`",
			})
		}
		*c.tp.LocalTypes.Ptr(locals[i]) = ty
		params = append(params, ty)
	}
	ret := c.unitT
	if it.HasRet {
		ret = c.typeOf(def.Module, it.Ret)
	}
	return c.tp.Types.Fun(params, ret, c.tp.Types.EmptyRow())
}

// typeOf returns the type that the type expression id of module mod writes (see
// typeReader.typeOf).
func (c *checker) typeOf(mod ModuleIdx, id syntax.TypeExprID) TypeID {
	return c.reader.typeOf(mod, id)
}

// mainSignature reports E0404 if the `main` of the user's file does not have the
// type `() -> ()`. A file without a `main` is not for this pass to report (that
// is E0303, which comes with `ki run`), and one that defines it twice is held to
// the first, as Resolve is. A return type that is not a type Ki has gives no
// E0404 of its own: it is E0406, and the error type, which never gives one.
//
// The primary label is on the name and says what is wrong with the signature:
// how many parameters there are, what is returned, or both. The help names the
// same pieces, and each comes with the fix-it that removes it, the parameters
// as `()` and the return clause with the blanks just before its `->` (a
// comment between the `)` and them stays). A signature that runs over several
// lines has the help and no fix-its, since an edit is of one line, and so does
// a signature with two things wrong when only one could be edited, so that the
// help never promises more than the fix-its do. Only the pieces that the help
// names count: an empty parameter list or a `-> ()` that runs over lines is not
// what is wrong, and does not take the edit of the other piece away.
func (c *checker) mainSignature() {
	if !c.res.HasMain {
		return
	}
	def := c.res.Defs.Get(c.res.Main)
	it := c.mods[def.Module].ItemNodes.Get(def.Item)
	fn := c.tp.Types.Node(c.tp.DefTypes.Get(c.res.Main))
	var declared, remove []string
	var fixits []diag.FixIt
	oneLine := true
	if len(fn.Params) > 0 {
		declared = append(declared, "with "+strconv.Itoa(len(fn.Params))+" parameter(s)")
		remove = append(remove, "the parameters")
		fixits = append(fixits, diag.Replace(it.ParamList, "()"))
		oneLine = oneLine && it.ParamListOneLine
	}
	// The return type of a signature is never a variable, so it needs no
	// resolving.
	if fn.Ret != c.unitT && !c.isError(fn.Ret) {
		ret := c.tp.Types.Show(fn.Ret)
		declared = append(declared, "returning `"+ret+"`")
		remove = append(remove, "`-> "+ret+"`")
		fixits = append(fixits, diag.Delete(it.RetClause))
		oneLine = oneLine && it.RetClauseOneLine
	}
	if len(declared) == 0 {
		return
	}
	d := diag.NewError(diag.E(404), "`main` must be `fn main()`",
		diag.Label{Span: def.Span, Message: "declared here " + strings.Join(declared, " and ")}).
		WithNote("it takes no parameters and returns `()`").
		WithHelp("remove " + strings.Join(remove, " and "))
	if oneLine {
		for _, f := range fixits {
			d = d.WithFixIt(f)
		}
	}
	c.sink.Report(d)
}

// bodies is the second pass: it checks the body of every function that has one
// against the function's return type. The reason is the tail of the body, and
// its secondary label is on the return type, or on the name of the function
// when it has none. A value at that tail where `()` is wanted can be returned
// by giving the function a return type, which Reason.CanReturn says, for every
// function but the `main` of the program: E0404 holds its type to `() -> ()`.
func (c *checker) bodies() {
	for id, def := range c.res.Defs.All() {
		m := c.mods[def.Module]
		it := m.ItemNodes.Get(def.Item)
		if !it.HasBody {
			continue
		}
		c.mod, c.m, c.rm, c.exprTypes = def.Module, m, &c.res.Modules[def.Module], c.tp.Modules[def.Module].ExprTypes
		because := def.Span
		if it.HasRet {
			because = m.Types.Get(it.Ret).Span
		}
		isMain := c.res.HasMain && id == c.res.Main
		c.check(it.Body, c.tp.Types.Node(c.tp.DefTypes.Get(id)).Ret,
			Reason{Kind: ReasonBlockTail, Because: because, CanReturn: !isMain})
	}
}

// check checks the expression id against the type expected, which the place r
// explains. It returns the type of the expression and whether it fit (false
// means that an error was reported for it, here or inside it).
//
// A block hands the expectation to its tail, so the error lands on the
// expression that is wrong. An `if` with no `else` checks its branch against
// `()` when `()` is what the place expects; an `if` with an `else` infers its
// `then` branch, checks the `else` branch against it and unifies the result
// with the expectation on the whole `if` (see checkIf). Any other expression is
// inferred and then unified, with the error on the whole expression.
func (c *checker) check(id syntax.ExprID, expected TypeID, r Reason) (TypeID, bool) {
	e := c.m.Exprs.Get(id)
	var ty TypeID
	var fits bool
	switch e.Kind {
	case syntax.ExprBlock:
		ty, fits = c.checkBlock(e, expected, r)
	case syntax.ExprIf:
		ty, fits = c.checkIf(e, expected, r)
	default:
		return c.checkAt(id, expected, r)
	}
	c.exprTypes[id] = ty
	return ty, fits
}

// checkAt infers the expression id and unifies its type with expected, with
// the error on the whole expression. It returns what check returns.
func (c *checker) checkAt(id syntax.ExprID, expected TypeID, r Reason) (TypeID, bool) {
	found := c.infer(id)
	e := c.m.Exprs.Get(id)
	// An Int literal where a Float is expected is the one mistake whose edit is
	// certain: the same number, written as a Float, when the Float is that number
	// (see exactAsFloat). A number that does not fit in an Int is not a literal here:
	// the parser made an error node of it.
	if e.Kind == syntax.ExprInt && c.tp.Types.Resolve(expected) == c.floatT && exactAsFloat(e.Int) {
		r.IntAsFloat = strconv.FormatInt(e.Int, 10) + ".0"
	}
	return found, c.u.Unify(expected, found, e.Span, r)
}

// exactAsFloat reports whether the Int n is also a Float: a float64 holds every
// integer up to 2^53 and, above that, only some of them, so for the others the
// number written as a Float is a different number. The comparison goes through
// `n` again to see that nothing was lost, and `f < 1<<63` keeps the conversion
// back inside the range of an Int, where Go defines it.
func exactAsFloat(n int64) bool {
	f := float64(n)
	return f < 1<<63 && int64(f) == n
}

// infer works out the type of the expression id from what is inside it, and
// records it.
func (c *checker) infer(id syntax.ExprID) TypeID {
	e := c.m.Exprs.Get(id)
	var ty TypeID
	switch e.Kind {
	case syntax.ExprInt:
		ty = c.intT
	case syntax.ExprFloat:
		ty = c.floatT
	case syntax.ExprBool:
		ty = c.boolT
	case syntax.ExprUnit:
		ty = c.unitT
	case syntax.ExprName:
		ty = c.name(id)
	case syntax.ExprCall:
		ty = c.call(e)
	case syntax.ExprBinary:
		ty = c.binary(e)
	case syntax.ExprUnary:
		ty = c.unary(e)
	case syntax.ExprField:
		ty = c.field(e)
	case syntax.ExprTupleIndex:
		ty = c.tupleIndex(e)
	case syntax.ExprRecord:
		ty = c.record(e)
	case syntax.ExprTuple:
		ty = c.tuple(e)
	case syntax.ExprIf:
		ty = c.inferIf(e)
	case syntax.ExprBlock:
		c.stmts(e)
		ty = c.unitT
		if e.HasTail {
			ty = c.infer(e.Tail)
		}
	case syntax.ExprError:
		ty = c.errT
	default:
		panic("unreachable")
	}
	c.exprTypes[id] = ty
	return ty
}

// name returns the type of the name written in the expression id: that of the
// variable or the definition it stands for, or the error type if it stands for
// nothing (Resolve reported that).
func (c *checker) name(id syntax.ExprID) TypeID {
	switch res := c.rm.ExprRes[id]; res.Kind {
	case ResDef:
		return c.tp.DefTypes.Get(res.Def)
	case ResLocal:
		return c.tp.LocalTypes.Get(res.Local)
	case ResNone, ResError:
		return c.errT
	default:
		panic("unreachable")
	}
}

// stmts checks the statements of the block e, in order.
func (c *checker) stmts(e syntax.Expr) {
	for _, id := range c.m.StmtList(e.Stmts) {
		s := c.m.Stmts.Get(id)
		switch s.Kind {
		case syntax.StmtLet:
			// With an annotation the initializer is checked against it, and
			// the variable has the annotated type; without one the variable
			// has the type of the initializer.
			var ty TypeID
			if s.HasType {
				ty = c.typeOf(c.mod, s.Type)
				c.check(s.Expr, ty, Reason{Kind: ReasonLetAnnotation, Because: c.m.Types.Get(s.Type).Span})
			} else {
				ty = c.infer(s.Expr)
			}
			*c.tp.LocalTypes.Ptr(c.rm.LetLocal[id]) = ty
		case syntax.StmtExpr:
			c.infer(s.Expr)
		case syntax.StmtError:
			// There is no expression to check.
		default:
			panic("unreachable")
		}
	}
}

// checkBlock checks the block e against expected. Its statements come first,
// and then its tail takes the expectation; a block without a tail is `()`, and
// the error, if that is not what is expected, is on the block.
func (c *checker) checkBlock(e syntax.Expr, expected TypeID, r Reason) (TypeID, bool) {
	c.stmts(e)
	if e.HasTail {
		return c.check(e.Tail, expected, r)
	}
	return c.unitT, c.u.Unify(expected, c.unitT, e.Span, r)
}

// condition checks the condition of an `if`, which has to be a Bool.
func (c *checker) condition(id syntax.ExprID) {
	c.check(id, c.boolT, Reason{Kind: ReasonIfCondition})
}

// unreadThen reports whether e is an `if` with no `else` whose `then` branch is
// an error node: the parser could not read it and has said so. Without the
// `else` such an `if` would be `()`, but nothing in the program says so, and
// where the place expects another type that `()` would be a second E0401 that
// goes away as soon as the branch is written. So the `if` has the error type,
// as an operation with an operand of that type does (see binary). The branch is
// not visited: it has the error type from the start (see signatures). The rule
// is about the branch only: inferIf and checkIf call it after c.condition, so a
// condition that is not a `Bool` is still reported.
func (c *checker) unreadThen(e syntax.Expr) bool {
	return !e.HasElse && c.m.Exprs.Get(e.Then).Kind == syntax.ExprError
}

// inferIf is the type of an `if` where nothing is expected of it. Without an
// `else` it is `()`, and then the `then` branch has to be `()` too: the missing
// `else` stands for the other branch, so the secondary label is on the `if`
// keyword. With an `else`, the type of the `then` branch is the type of the
// `if` (see elseBranch). If the parser could not read the `then` branch of an
// `if` with no `else`, the `if` has the error type (see unreadThen); its
// condition is checked all the same.
func (c *checker) inferIf(e syntax.Expr) TypeID {
	c.condition(e.Cond)
	if c.unreadThen(e) {
		return c.errT
	}
	if !e.HasElse {
		keyword := base.Span{File: e.Span.File, Lo: e.Span.Lo, Hi: e.Span.Lo + ifKeywordLen}
		c.check(e.Then, c.unitT, Reason{Kind: ReasonIfBranches, Because: keyword})
		return c.unitT
	}
	return c.elseBranch(e, c.infer(e.Then))
}

// checkIf checks the `if` e against expected, which r explains.
//
// Without an `else` the `if` is `()`. That is unified with the expected type
// first, on the whole `if`, with the reason of the place. If that fails it is
// the one error, and the `then` branch is only inferred: it would be a second
// error for the same mistake. If it does not fail, the `then` branch is checked
// against `()` with the same reason.
//
// With an `else`, the `then` branch is inferred and the `else` branch is
// checked against it (see elseBranch), and the result is unified with the
// expected type on the whole `if`. That is the rule that makes the `then`
// branch the one that decides, and a mistake between the branches one error. It
// has one exception: when a `Float` is expected, each branch is checked against
// it. There the mistake is an `Int` literal and the edit is certain, and the
// rule would put the error on the `2.5` of `if b { 1 } else { 2.5 }` (it says
// the other branch is an `Int`, and the function returns a `Float`) and none on
// the `1`, which is the one that is wrong. A `Float` expected has no other
// reading of which branch is right, so each wrong branch has its own error, as
// each wrong argument of a call does. The `if` has the error type if one of
// them does not fit, so the place that uses it adds nothing. The branches have
// the reason of the place.
//
// An `if` with no `else` whose `then` branch the parser could not read (see
// unreadThen) is neither: it has the error type, which fits every place, and
// the checker adds nothing for the branch. Its condition is checked first, as
// in every other `if`.
func (c *checker) checkIf(e syntax.Expr, expected TypeID, r Reason) (TypeID, bool) {
	c.condition(e.Cond)
	if c.unreadThen(e) {
		return c.errT, true
	}
	if !e.HasElse {
		if !c.u.Unify(expected, c.unitT, e.Span, r) {
			c.infer(e.Then)
			return c.unitT, false
		}
		// What the branch ends in is not what the function returns, so even at
		// the tail of a body the help does not offer to return it.
		then := r
		then.CanReturn = false
		_, fits := c.check(e.Then, c.unitT, then)
		return c.unitT, fits
	}
	if c.tp.Types.Resolve(expected) == c.floatT {
		_, thenFits := c.check(e.Then, expected, r)
		_, elseFits := c.check(e.Else, expected, r)
		if !thenFits || !elseFits {
			return c.errT, false
		}
		return c.floatT, true
	}
	ty := c.elseBranch(e, c.infer(e.Then))
	return ty, c.u.Unify(expected, ty, e.Span, r)
}

// elseBranch checks the `else` branch of e against thenTy, the type of its
// `then` branch, and returns the type of the `if`: thenTy, or the error type if
// the branches do not agree, so that a mistake between the branches is not
// reported a second time where the `if` is used. The secondary label of that
// error is on the tail of the `then` branch.
func (c *checker) elseBranch(e syntax.Expr, thenTy TypeID) TypeID {
	then := c.m.Exprs.Get(e.Then)
	because := then.Span
	if then.Kind == syntax.ExprBlock && then.HasTail {
		because = c.m.Exprs.Get(then.Tail).Span
	}
	if _, fits := c.check(e.Else, thenTy, Reason{Kind: ReasonIfBranches, Because: because}); !fits {
		return c.errT
	}
	return thenTy
}

// isError reports whether ty is the error type. A variable is never bound to
// it (Unify binds nothing to the error type), so ty needs no resolving.
func (c *checker) isError(ty TypeID) bool {
	return ty == c.errT
}

// binary is the type of a binary operation. `&&` and `||` check each operand
// against Bool, with the error on the operand; the other operators have a
// function of their own.
func (c *checker) binary(e syntax.Expr) TypeID {
	switch e.Op {
	case syntax.BinEq, syntax.BinNe:
		return c.equality(e)
	case syntax.BinRem:
		return c.remainder(e)
	case syntax.BinAdd, syntax.BinSub, syntax.BinMul, syntax.BinDiv, syntax.BinLt, syntax.BinLe, syntax.BinGt, syntax.BinGe:
		return c.numeric(e)
	case syntax.BinAnd, syntax.BinOr:
		r := Reason{Kind: ReasonOperand, Because: e.OpSpan, Detail: e.Op.String(), Index: 2}
		lhs, _ := c.checkAt(e.Lhs, c.boolT, r)
		rhs, _ := c.checkAt(e.Rhs, c.boolT, r)
		if c.isError(lhs) || c.isError(rhs) {
			return c.errT
		}
		return c.boolT
	default:
		panic("unreachable")
	}
}

// numeric is the type of `+ - * /` (the type of the operands) and of `< <= > >=`
// (Bool). Both operands are one type, Int or Float, and the left operand
// decides which, as in equality:
//
//   - Int or Float: the right operand is checked against it, with the operator as
//     the reason, so `1 + 2.0` is an E0401 on the `2.0` that says the operator
//     expects `Int` operands. A right operand that does not fit is the one
//     mistake, and the operation has the error type, as in remainder: if it kept
//     the type of the left operand, `-> Float { i + x }` with an `Int` for `i`
//     would say a second time, where the result is used, that an `Int` is not a
//     `Float`.
//   - Any other type known by now (Bool, a function, `()`): that is the one
//     mistake, an E0401 on the left operand that no reason explains (see
//     rejectedOperandError), and the right operand is only inferred. The
//     operation has the error type, so the place where it is used says nothing
//     more.
//   - The error type: the right operand is inferred, and the operation has the
//     error type.
//   - A variable that nothing has bound yet (a parameter without an annotation):
//     the right operand is checked against it, which binds it if the right
//     operand has a type of its own, and so does the use of the result, since an
//     arithmetic operation has the type of its operands. If it is still a
//     variable after that, it is judged when all the bodies are checked
//     (judgePending), and not now: a call elsewhere is what binds a parameter,
//     so what is known here depends on the order of the functions, and the same
//     program must not be accepted in one order and refused in the other. That is
//     what lets `fn sq(x) -> Float { x * x }` be a function of Floats wherever
//     it is written.
func (c *checker) numeric(e syntax.Expr) TypeID {
	op := e.Op.String()
	ordering := e.Op == syntax.BinLt || e.Op == syntax.BinLe || e.Op == syntax.BinGt || e.Op == syntax.BinGe
	lhs := c.infer(e.Lhs)
	lhsAt := c.m.Exprs.Get(e.Lhs).Span
	kind := c.operandKindOf(lhs, onNumbers)
	if kind == operandRejected {
		c.rejectOperand(lhsAt, lhs, op, onNumbers)
		c.infer(e.Rhs)
		return c.errT
	}
	rhs, fits := c.checkAt(e.Rhs, lhs, Reason{Kind: ReasonOperand, Because: e.OpSpan, Detail: op, Index: 2})
	if !fits || c.isError(lhs) || c.isError(rhs) {
		return c.errT
	}
	if kind == operandFree {
		// The right operand may have told what the left one is.
		switch c.operandKindOf(lhs, onNumbers) {
		case operandRejected:
			c.rejectOperand(lhsAt, lhs, op, onNumbers)
			return c.errT
		case operandFree:
			c.pending = append(c.pending, pendingOperand{operand: lhs, at: lhsAt, op: op, set: onNumbers})
			if !ordering {
				// Still no type: the result is the type of the operands, whatever
				// it turns out to be.
				return lhs
			}
		}
	}
	if ordering {
		return c.boolT
	}
	return c.tp.Types.Resolve(lhs)
}

// remainder is the type of `a % b`: Int. There is no `%` on Float, so the types
// that the left operand may have are Int alone (onIntegers), and a Float there
// is one E0401, on that operand, with the operator as the reason. Otherwise it
// is judged as the left operand of numeric is:
//
//   - Int: the right operand is checked against Int.
//   - Any other type known by now: that is the one mistake. The right operand is
//     not compared with Int, and the operation has the error type: a mistake in
//     the left operand is not three mistakes (the two operands and the place
//     where the result is used). A wrong right operand has the error type as its
//     result too, for the same reason.
//   - A variable that nothing has bound yet: the right operand is checked against
//     it, which binds it if the right operand has a type of its own, and if it
//     is still a variable after that it is judged when all the bodies are checked
//     (judgePending), and not now, for the reason that numeric gives. The
//     operation is an Int either way.
func (c *checker) remainder(e syntax.Expr) TypeID {
	r := Reason{Kind: ReasonOperand, Because: e.OpSpan, Detail: "%", Index: 2}
	lhs := c.infer(e.Lhs)
	lhsAt := c.m.Exprs.Get(e.Lhs).Span
	kind := c.operandKindOf(lhs, onIntegers)
	if kind == operandRejected {
		c.rejectRemainder(lhsAt, lhs, r)
		c.infer(e.Rhs)
		return c.errT
	}
	against := c.intT
	if kind == operandFree {
		against = lhs
	}
	rhs, fits := c.checkAt(e.Rhs, against, r)
	if !fits || c.isError(lhs) || c.isError(rhs) {
		return c.errT
	}
	if kind == operandFree {
		// The right operand may have told what the left one is.
		switch c.operandKindOf(lhs, onIntegers) {
		case operandRejected:
			c.rejectRemainder(lhsAt, lhs, r)
			return c.errT
		case operandFree:
			c.pending = append(c.pending, pendingOperand{operand: lhs, at: lhsAt, op: "%", set: onIntegers, reason: r})
		}
	}
	return c.intT
}

// unary is the type of `-x` and `!x`. `!` takes a Bool. `-` works on Int and on
// Float and gives the type of its operand, which is judged like the left operand
// of numeric: a type that is neither is an E0401 on the operand that no reason
// explains, and an operand that is a free variable waits for the bodies.
func (c *checker) unary(e syntax.Expr) TypeID {
	if e.UnOp == syntax.Not {
		found, _ := c.checkAt(e.Operand, c.boolT, Reason{Kind: ReasonOperand, Because: e.OpSpan, Detail: "!", Index: 1})
		if c.isError(found) {
			return c.errT
		}
		return c.boolT
	}
	found := c.infer(e.Operand)
	at := c.m.Exprs.Get(e.Operand).Span
	switch c.operandKindOf(found, onNumbers) {
	case operandRejected:
		c.rejectOperand(at, found, "-", onNumbers)
		return c.errT
	case operandFree:
		c.pending = append(c.pending, pendingOperand{operand: found, at: at, op: "-", set: onNumbers})
		return found
	}
	if c.isError(found) {
		return c.errT
	}
	return c.tp.Types.Resolve(found)
}

// equality is the type of `a == b` and `a != b`: Bool. The two operands are one
// type, and it has to be Int, Bool or Float. The left operand is inferred first,
// and what it is decides what happens:
//
//   - Int, Bool or Float (or the error type, which says nothing): the right
//     operand is checked against it, with the operator as the reason, so
//     `1 == true` is an E0401 on the `true` that says the operator expects `Int`
//     operands.
//   - Any other type known by now (a function, `()`): that is the one mistake,
//     an E0401 on the left operand that no reason explains, so it has a note
//     and no secondary label. The right operand is only inferred, because `==`
//     does not expect it to be a function and a label that said so would
//     contradict the note.
//   - A variable that nothing has bound yet (a parameter without an
//     annotation): the right operand is checked against it, which binds it if
//     the right operand has a type of its own. If it is still a variable after
//     that, the left operand is judged when all the bodies are checked
//     (judgePending), and not now: a call elsewhere is what binds a
//     parameter, so what is known here depends on the order of the functions,
//     and the same program must not be accepted in one order and refused in the
//     other.
func (c *checker) equality(e syntax.Expr) TypeID {
	op := e.Op.String()
	lhs := c.infer(e.Lhs)
	lhsAt := c.m.Exprs.Get(e.Lhs).Span
	if c.operandKindOf(lhs, onEquatable) == operandRejected {
		c.rejectOperand(lhsAt, lhs, op, onEquatable)
		c.infer(e.Rhs)
		return c.boolT
	}
	rhs, _ := c.checkAt(e.Rhs, lhs, Reason{Kind: ReasonOperand, Because: e.OpSpan, Detail: op, Index: 2})
	if c.isError(lhs) || c.isError(rhs) {
		return c.errT
	}
	// The right operand may have told what the left one is.
	switch c.operandKindOf(lhs, onEquatable) {
	case operandRejected:
		c.rejectOperand(lhsAt, lhs, op, onEquatable)
	case operandFree:
		c.pending = append(c.pending, pendingOperand{operand: lhs, at: lhsAt, op: op, set: onEquatable})
	}
	return c.boolT
}

// operandKind is what is known about the type of a left operand.
type operandKind uint8

const (
	// operandOK is a type that the operator works on, or the error type.
	operandOK operandKind = iota
	// operandFree is a variable that nothing has bound.
	operandFree
	// operandRejected is any other type: for an operator on numbers, Bool, a
	// function or `()`; for `==`, a function or `()`; for `%`, also Float.
	operandRejected
)

// operandKindOf says what ty, resolved, is for an operator that works on the
// types of set.
func (c *checker) operandKindOf(ty TypeID, set operandSet) operandKind {
	switch t := c.tp.Types.Resolve(ty); {
	case t == c.intT || t == c.errT || t == c.floatT && set != onIntegers:
		return operandOK
	case t == c.boolT && set == onEquatable:
		return operandOK
	case c.tp.Types.Node(t).Kind == TypeVariable:
		return operandFree
	default:
		return operandRejected
	}
}

// rejectOperand reports the operand of the operator op that is written at and has
// the type ty, a type the operator does not work on (rejectedOperandError). If ty
// is an inference variable, the report is made once (see firstRejection).
func (c *checker) rejectOperand(at base.Span, ty TypeID, op string, set operandSet) {
	if c.firstRejection(ty) {
		c.rejectedOperandError(at, ty, op, set)
	}
}

// rejectRemainder reports the left operand of `%` that is written at and has the
// type ty, which is not Int. Unlike the other operators, `%` explains the
// mistake with a reason, the operator that expects an Int (r): it is the E0401
// that Unify writes for an Int that is not there. Like rejectOperand it
// reports once for a variable (see firstRejection).
func (c *checker) rejectRemainder(at base.Span, ty TypeID, r Reason) {
	if c.firstRejection(ty) {
		c.u.Unify(c.intT, ty, at, r)
	}
}

// firstRejection says whether the refusal of an operand of type ty is to be
// reported now. It is, unless ty is an inference variable that an operator has
// refused already: a parameter that nothing typed and that an operator refuses
// is one mistake, in the type of the parameter, however many operators have it
// as an operand, and whether the call that made it so was checked before the
// operator or after it. The parameter is told by its variable: the operands of
// `a + b + c` are one (the sum is the type of `a`), but two parameters that
// something else made equal (`x == y`) are two variables, and the call that
// makes both a Bool is a mistake in each. A type that is written (`true`, a
// parameter annotated `Bool`) is reported at each use.
func (c *checker) firstRejection(ty TypeID) bool {
	if c.tp.Types.Node(ty).Kind != TypeVariable {
		return true
	}
	if c.rejected[ty] {
		return false
	}
	if c.rejected == nil {
		c.rejected = make(map[TypeID]bool)
	}
	c.rejected[ty] = true
	return true
}

// rejectedOperandError reports E0401 for the operand of the operator op that is
// written at and has the type ty, a type the operator does not work on. The
// label says what the operator takes and the note says it again for the
// operator, with no secondary label: no other place in the program explains the
// rule. set is onNumbers or onEquatable: `%` has a reason to give instead (see
// rejectRemainder).
func (c *checker) rejectedOperandError(at base.Span, ty TypeID, op string, set operandSet) {
	// Once the sink has dropped an error it drops this one too, so the text of
	// the type is not written (see Unifier.mismatch).
	if c.sink.Truncated() {
		return
	}
	expected, works := "`Int` or `Float`", "`Int` and `Float`"
	if set == onEquatable {
		expected, works = "`Int`, `Bool` or `Float`", "`Int`, `Bool` and `Float`"
	}
	c.sink.Report(diag.NewError(diag.E(401), "mismatched types",
		diag.Label{Span: at, Message: "expected " + expected + ", found `" + c.tp.Types.Show(ty) + "`"}).
		WithNote("`" + op + "` works on " + works + " here"))
}

// judgePending judges the left operands of the operators that had no type when
// their body was checked. By now every body has had its say about a parameter,
// so a type that the operator does not work on is reported as it would have been
// if the call had come first. A variable that is still free is left alone:
// nothing says it is a function or `()`. A program can therefore be accepted
// with a free variable as the type of an operand (`fn same(x, y) -> Bool { x ==
// y }` and no call), and the table keeps it. The pass that has to choose between
// the Int, Float and Bool primitives takes Int for such an operand. Nothing can
// tell the difference: a value whose type nothing knows never exists, because
// main has no parameters, so every value starts as a literal, a function or the
// result of a function of the prelude.
//
// An operand that several operators share (`x * x + x` has the same variable in
// both) is reported once, at the first of them: see rejectOperand.
func (c *checker) judgePending() {
	for _, q := range c.pending {
		if c.operandKindOf(q.operand, q.set) != operandRejected {
			continue
		}
		if q.set == onIntegers {
			c.rejectRemainder(q.at, q.operand, q.reason)
		} else {
			c.rejectOperand(q.at, q.operand, q.op, q.set)
		}
	}
}

// call is the type of a call: the result type of the function it calls. The
// callee is inferred, and what it turned out to be says what to do.
func (c *checker) call(e syntax.Expr) TypeID {
	callee := c.infer(e.Callee)
	ct := c.tp.Types.Resolve(callee)
	node := c.tp.Types.Node(ct)
	args := c.m.ExprList(e.Args)
	switch node.Kind {
	case TypeFun:
		if len(node.Params) != len(args) {
			// Which argument is extra or missing is not for the compiler to
			// say, so the arguments are not compared with the parameters. They
			// still have a type of their own, which may be wrong.
			//
			// The count itself is judged only when no argument is missing. The
			// parser leaves a node of error where it could not read one, and
			// that node is not necessarily an argument: a stray comma
			// (`g(1,,2)`) makes the list one longer than the author meant, and
			// the E0402 would go away when the comma is mended. The parser's
			// error is the one report of that slip, and the E0402 comes back
			// once no argument is missing, even if the list was too long
			// without the node as well (`g(1, 2, 3,,)`). Nothing else excuses
			// the count: an argument that is complete and wrong (an unknown
			// name has the error type) or that breaks off (`2 +`) is an
			// argument all the same.
			if !slices.ContainsFunc(args, c.missingArg) {
				c.arityError(e, node)
			}
			c.inferAll(args)
			return node.Ret
		}
		for i, arg := range args {
			c.check(arg, node.Params[i], c.argReason(e.Callee, i))
		}
		return node.Ret
	case TypeVariable:
		// A parameter without an annotation that is called: it is a function
		// of these arguments, and its result is not known yet.
		types := make([]TypeID, len(args))
		for i, arg := range args {
			types[i] = c.infer(arg)
		}
		ret := c.tp.Types.FreshVar(Origin{Span: e.Span, Note: "result of this call"})
		if slices.ContainsFunc(args, c.missingArg) {
			// The list as read is not the list the author meant (`x(1,,2)` has
			// three entries), and a function of that list would become what
			// every other use of the callee is checked against: `x(1, 2)` would
			// be an E0402 for a function of three parameters that nobody wrote,
			// and `f(h)` with a function h of two an E0401 whose type has the
			// node of error in it. The parser has reported the slip, so the
			// callee is left as it is. What is hidden comes back once the comma
			// is mended: a callee that already has a type that is not a
			// function (`x(x + 1,,2)`) is an E0401 then, as for any call.
			return ret
		}
		// If the callee is still an unbound variable, Unify binds it to this
		// function type, and the only error it can report is the occurs check
		// (`fn f(x) { x(x) }`). But ct was resolved before the arguments were
		// inferred, and they may already have given the callee a type
		// (`fn f(x) { x(x + 1) }` makes x an Int), and then Unify reports the
		// E0401 for that mismatch. The reason is ReasonArgument with no place
		// (the zero Span), so that error has no secondary label. Unify does
		// read the reason: one that mismatch refuses, such as an operand reason
		// with Index 0, would turn this program into a panic.
		c.u.Unify(ct, c.tp.Types.Fun(types, ret, c.tp.Types.EmptyRow()), c.m.Exprs.Get(e.Callee).Span,
			Reason{Kind: ReasonArgument})
		return ret
	case TypeErr:
		c.inferAll(args)
		return c.errT
	case TypeBuiltin, TypeData, TypeTuple:
		// A constructor with no fields is a value of its type, not a function.
		c.notCallableError(e, ct)
		c.inferAll(args)
		return c.errT
	default:
		panic("unreachable")
	}
}

// missingArg reports whether the argument id is one that the parser reported
// and left as a node of error, or an operation that starts with one. The node
// is a gap (the extra comma of `g(1,,2)`), and also what the parser leaves for
// other slips, such as a chained comparison or a group with no `)`. A stray
// operator is read as an operation of such nodes: `g(1, +, 2)` has an argument
// that is a sum of two missing operands, and `g(1, -, 2)` one that is a
// negation of a missing operand. Only the start counts, the left operand of an
// operation of two and the operand of a prefix operation, however deep. An
// argument that begins well and breaks off (`2 +`, whose right operand is
// missing) is an argument the author wrote.
func (c *checker) missingArg(id syntax.ExprID) bool {
	for {
		e := c.m.Exprs.Get(id)
		switch e.Kind {
		case syntax.ExprError:
			return true
		case syntax.ExprBinary:
			// Down the starts of the operations. This is a loop and not a
			// recursion because the length of a chain is the program's (see
			// MaxChain).
			id = e.Lhs
		case syntax.ExprUnary:
			id = e.Operand
		default:
			return false
		}
	}
}

// inferAll infers the expressions ids, for the mistakes inside them.
func (c *checker) inferAll(ids []syntax.ExprID) {
	for _, id := range ids {
		c.infer(id)
	}
}

// calleeDef returns the definition that the callee expression is the name of,
// and its id.
func (c *checker) calleeDef(callee syntax.ExprID) (DefID, Def, bool) {
	if res := c.rm.ExprRes[callee]; c.m.Exprs.Get(callee).Kind == syntax.ExprName && res.Kind == ResDef {
		return res.Def, c.res.Defs.Get(res.Def), true
	}
	return 0, Def{}, false
}

// paramSpan returns the span of the i-th parameter of the function that the
// callee expression is the name of, or, for a constructor, of its i-th field as
// the declaration of its variant writes it: the secondary label of an argument
// that does not fit. A constructor has no Params to index, because the item that
// its Def names is the type's. If the callee is not the name of a definition (it
// is a variable that holds a function), there is nothing to point at, and the
// result is the zero Span, which Unify takes for no place at all.
func (c *checker) paramSpan(callee syntax.ExprID, i int) base.Span {
	id, def, ok := c.calleeDef(callee)
	if !ok {
		return base.Span{}
	}
	if def.Kind == DefCtor {
		ctor, _ := c.decls.CtorOf(id)
		return c.decls.FieldSpan(c.mods, ctor, i)
	}
	m := c.mods[def.Module]
	return m.Params(m.ItemNodes.Get(def.Item).Params)[i].Span
}

// argReason is the reason for the i-th argument of a call whose callee is the
// expression callee: the argument of a function, or of a constructor, which has
// a reason of its own because it is a field that explains the type, and not a
// parameter.
func (c *checker) argReason(callee syntax.ExprID, i int) Reason {
	kind := ReasonArgument
	if _, def, ok := c.calleeDef(callee); ok && def.Kind == DefCtor {
		kind = ReasonCtorArg
	}
	return Reason{Kind: kind, Because: c.paramSpan(callee, i)}
}

// maxNoteParams is how many parameter types the note of an E0402 writes. A
// function can have thousands of parameters and every call that supplies the
// wrong number of arguments writes the note again, so it stops after this many
// and ends in `…`. Each type is cut on its own as well (see TypeTable.Show), so
// a note is at most about maxNoteParams × MaxTypeText bytes long.
const maxNoteParams = 16

// arityError reports E0402 for the call e of a function of type fn with another
// number of arguments than parameters. The primary label is on the argument
// list. If the callee is a name, a note gives the parameter types (the first
// maxNoteParams of them), and, if it is declared in the file of the call, a
// secondary label shows where. (A function of another file, such as one of the
// prelude, cannot be drawn under this one: RenderHuman refuses it.)
func (c *checker) arityError(e syntax.Expr, fn Type) {
	// Once the sink has dropped an error it drops this one too, and writing the
	// note is not cheap even with the cut: Show pushes every parameter of each
	// type before it gets to the cut, so the 16 types of the note cost the
	// parameters of all 16 (see Unifier.mismatch).
	if c.sink.Truncated() {
		return
	}
	d := diag.NewError(diag.E(402),
		"this function takes "+strconv.Itoa(len(fn.Params))+" argument(s) but "+strconv.Itoa(int(e.Args.Count))+" were supplied",
		diag.Label{Span: e.OpSpan, Message: "supplied here"})
	if callee := c.m.Exprs.Get(e.Callee); callee.Kind == syntax.ExprName {
		name := c.in.String(callee.Name)
		if _, def, ok := c.calleeDef(e.Callee); ok && def.Span.File == e.OpSpan.File {
			d = d.WithSecondary(diag.Label{Span: def.Span, Message: "`" + name + "` is declared here"})
		}
		params := fn.Params[:min(len(fn.Params), maxNoteParams)]
		texts := c.tp.Types.showAll(params...)
		if len(params) < len(fn.Params) {
			texts = append(texts, cutMark)
		}
		d = d.WithNote("`" + name + "` takes `(" + strings.Join(texts, ", ") + ")`")
	}
	c.sink.Report(d)
}

// notCallableError reports E0403 for the call e whose callee has the type ty,
// which is not a function. If the callee is a variable, a secondary label shows
// where it is bound. (A function is always a function, so a callee that is a
// function never gets here; one that is a constructor without fields does, and
// has no secondary label: its definition is not a variable.)
func (c *checker) notCallableError(e syntax.Expr, ty TypeID) {
	// Once the sink has dropped an error it drops this one too, and the type of the
	// callee is written before anything else: it is a tuple of any size now, and
	// Show pushes all of its elements before it gets to the cut (see arityError).
	if c.sink.Truncated() {
		return
	}
	callee := c.m.Exprs.Get(e.Callee)
	d := diag.NewError(diag.E(403), "cannot call a value of type `"+c.tp.Types.Show(ty)+"`",
		diag.Label{Span: callee.Span, Message: "not a function"})
	if res := c.rm.ExprRes[e.Callee]; callee.Kind == syntax.ExprName && res.Kind == ResLocal {
		local := c.res.Locals.Get(res.Local)
		d = d.WithSecondary(diag.Label{Span: local.Span, Message: "`" + c.in.String(local.Name) + "` is bound here"})
	}
	c.sink.Report(d)
}
