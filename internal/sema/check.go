package sema

import (
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
	// Types holds every type that the tables below name.
	Types *TypeTable
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
// expectation. A block or an `if` that is checked passes the expectation on to
// its tail or its branches, so the error lands on the expression that is wrong
// and not on the whole block.
//
// Signatures come first. Every function gets its type from its annotations:
// a parameter without one gets a new variable, and a function without `->`
// returns `()`. A type name that is neither `Int` nor `Bool` is E0406 (an
// unknown type), reported once where it is written, and has the error type.
// Then `main`, if the user's file has one, is held to its type, `() -> ()`
// (E0404). Only then are the bodies checked, each against the return type of
// its function (the tail of a body is the reason ReasonBlockTail). All the
// functions of the program are one group whose variables are not generalized
// (chapter 13 does that), so what a call or a body finds out about a parameter
// is what every other use of the function sees.
//
// The operators are fixed: `+ - * / %` take two Int and give an Int, `< <= > >=`
// take two Int and give a Bool, `&& ||` take and give Bool, `-` is Int to Int
// and `!` Bool to Bool. `==` and `!=` compare two values of one type, and that
// type has to be Int or Bool (see equality for when that is judged). If an
// operand has the error type, so does the result. A call is checked against the
// type of its callee: E0403 if it is not a function, E0402 if the number of
// arguments is not the number of parameters. A callee whose type is a variable
// that nothing has bound yet (a parameter without an annotation) is made a
// function of the types of the arguments. The error type of an unknown type
// name gives no complaint of its own wherever it is used, but it is still part
// of the types around it, so a message about another mistake can write it as
// `{error}` (a function with a parameter of an unknown type, used where an
// `Int` is expected, is a mismatch with `({error}) -> ()`).
//
// infer and check call each other once per level of the tree, like Resolve:
// Parse bounds that depth (MaxNesting levels, and the operators and calls of
// one statement, MaxChain), so a tree the parser made is safe; a tree built by
// hand is not.
func Check(mods []*syntax.Module, res *ResolvedProgram, in *base.Interner, sink *diag.Sink) *TypedProgram {
	tbl := NewTypeTable()
	c := &checker{
		mods: mods, res: res, in: in, sink: sink,
		tp:    &TypedProgram{Types: tbl, Modules: make([]TypedModule, len(mods))},
		u:     Unifier{Types: tbl, Sink: sink},
		intT:  tbl.Builtin(BuiltinInt),
		boolT: tbl.Builtin(BuiltinBool),
		unitT: tbl.Builtin(BuiltinUnit),
		errT:  tbl.Error(),
	}
	c.signatures()
	c.mainSignature()
	c.bodies()
	c.judgeEqualities()
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

	// The builtin types and the error type, which every rule uses.
	intT, boolT, unitT, errT TypeID

	// The module whose bodies are being checked, its tree, its names and its
	// table of types (a slice of tp.Modules, so a write here is a write there).
	m         *syntax.Module
	rm        *ModuleRes
	exprTypes []TypeID

	// equalities are the `==` and `!=` whose left operand had no type yet when
	// the body was checked, to be judged once all the bodies are (see equality).
	equalities []pendingEquality
}

// pendingEquality is a left operand of `==` or `!=` that was a variable
// nothing had bound yet: operand is its type, at where it is written and op
// the operator.
type pendingEquality struct {
	operand TypeID
	at      base.Span
	op      string
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
	for _, def := range c.res.Defs.All() {
		c.tp.DefTypes.Push(c.signature(def))
	}
}

// signature returns the function type of def, and gives its parameters their
// types. A parameter with an annotation has that type; one without gets a new
// variable. A function without `->` returns `()`.
func (c *checker) signature(def Def) TypeID {
	m := c.mods[def.Module]
	it := m.ItemNodes.Get(def.Item)
	locals := c.res.Modules[def.Module].ParamLocals[def.Item]
	var params []TypeID
	for i, p := range m.Params(it.Params) {
		var ty TypeID
		if p.HasType {
			ty = c.typeOf(m, p.Type)
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
		ret = c.typeOf(m, it.Ret)
	}
	return c.tp.Types.Fun(params, ret, c.tp.Types.EmptyRow())
}

// typeOf returns the type that the type expression id of module m writes.
func (c *checker) typeOf(m *syntax.Module, id syntax.TypeExprID) TypeID {
	te := m.Types.Get(id)
	switch te.Kind {
	case syntax.TypeUnit:
		return c.unitT
	case syntax.TypeNamed:
		switch c.in.String(te.Name) {
		case "Int":
			return c.intT
		case "Bool":
			return c.boolT
		}
		// Not a type Ki has: reported here, once, where it is written, and the
		// error type from then on, which says nothing more wherever it is used.
		c.unknownTypeError(te.Span, c.in.String(te.Name))
		return c.errT
	case syntax.TypeError:
		return c.errT
	default:
		panic("unreachable")
	}
}

// unknownTypeError reports E0406 for the type name that is written at. The
// help and its edit are there when a type Ki has is spelled almost like it
// (`Bol` for `Bool`), as for an unknown name.
func (c *checker) unknownTypeError(at base.Span, name string) {
	d := diag.NewError(diag.E(406), "unknown type `"+name+"`", diag.Label{Span: at, Message: "not a type Ki knows"}).
		WithNote("the types Ki knows: `Int`, `Bool` and `()`")
	if best, ok := suggest(name, []string{"Int", "Bool"}); ok {
		d = d.WithHelp("did you mean `" + best + "`?").WithFixIt(diag.Replace(at, best))
	}
	c.sink.Report(d)
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
		c.m, c.rm, c.exprTypes = m, &c.res.Modules[def.Module], c.tp.Modules[def.Module].ExprTypes
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
// A block hands the expectation to its tail and an `if` to its branches, so the
// error lands on the expression that is wrong. Any other expression is
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
	return found, c.u.Unify(expected, found, c.m.Exprs.Get(id).Span, r)
}

// infer works out the type of the expression id from what is inside it, and
// records it.
func (c *checker) infer(id syntax.ExprID) TypeID {
	e := c.m.Exprs.Get(id)
	var ty TypeID
	switch e.Kind {
	case syntax.ExprInt:
		ty = c.intT
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
				ty = c.typeOf(c.m, s.Type)
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

// inferIf is the type of an `if` where nothing is expected of it. Without an
// `else` it is `()`, and then the `then` branch has to be `()` too: the missing
// `else` stands for the other branch, so the secondary label is on the `if`
// keyword. With an `else`, the type of the `then` branch is the type of the
// `if` (see elseBranch).
func (c *checker) inferIf(e syntax.Expr) TypeID {
	c.condition(e.Cond)
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
// expected type on the whole `if`.
func (c *checker) checkIf(e syntax.Expr, expected TypeID, r Reason) (TypeID, bool) {
	c.condition(e.Cond)
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

// binary is the type of a binary operation. Each operand is checked against the
// type the operator wants, with the error on the operand.
func (c *checker) binary(e syntax.Expr) TypeID {
	if e.Op == syntax.BinEq || e.Op == syntax.BinNe {
		return c.equality(e)
	}
	var operand, result TypeID
	switch e.Op {
	case syntax.BinAdd, syntax.BinSub, syntax.BinMul, syntax.BinDiv, syntax.BinRem:
		operand, result = c.intT, c.intT
	case syntax.BinLt, syntax.BinLe, syntax.BinGt, syntax.BinGe:
		operand, result = c.intT, c.boolT
	case syntax.BinAnd, syntax.BinOr:
		operand, result = c.boolT, c.boolT
	default:
		panic("unreachable")
	}
	r := Reason{Kind: ReasonOperand, Because: e.OpSpan, Detail: e.Op.String(), Index: 2}
	lhs, _ := c.checkAt(e.Lhs, operand, r)
	rhs, _ := c.checkAt(e.Rhs, operand, r)
	if c.isError(lhs) || c.isError(rhs) {
		return c.errT
	}
	return result
}

// unary is the type of `-x` (Int to Int) and `!x` (Bool to Bool).
func (c *checker) unary(e syntax.Expr) TypeID {
	operand, text := c.intT, "-"
	if e.UnOp == syntax.Not {
		operand, text = c.boolT, "!"
	}
	found, _ := c.checkAt(e.Operand, operand, Reason{Kind: ReasonOperand, Because: e.OpSpan, Detail: text, Index: 1})
	if c.isError(found) {
		return c.errT
	}
	return operand
}

// equality is the type of `a == b` and `a != b`: Bool. The two operands are one
// type, and it has to be Int or Bool. The left operand is inferred first, and
// what it is decides what happens:
//
//   - Int or Bool (or the error type, which says nothing): the right operand is
//     checked against it, with the operator as the reason, so `1 == true` is
//     an E0401 on the `true` that says the operator expects `Int` operands.
//   - Any other type known by now (a function, `()`): that is the one mistake,
//     an E0401 on the left operand that no reason explains, so it has a note
//     and no secondary label. The right operand is only inferred, because `==`
//     does not expect it to be a function and a label that said so would
//     contradict the note.
//   - A variable that nothing has bound yet (a parameter without an
//     annotation): the right operand is checked against it, which binds it if
//     the right operand has a type of its own. If it is still a variable after
//     that, the left operand is judged when all the bodies are checked
//     (judgeEqualities), and not now: a call elsewhere is what binds a
//     parameter, so what is known here depends on the order of the functions,
//     and the same program must not be accepted in one order and refused in the
//     other.
func (c *checker) equality(e syntax.Expr) TypeID {
	op := e.Op.String()
	lhs := c.infer(e.Lhs)
	lhsAt := c.m.Exprs.Get(e.Lhs).Span
	if c.eqKindOf(lhs) == eqRejected {
		c.rejectedEqualityError(lhsAt, lhs, op)
		c.infer(e.Rhs)
		return c.boolT
	}
	rhs, _ := c.checkAt(e.Rhs, lhs, Reason{Kind: ReasonOperand, Because: e.OpSpan, Detail: op, Index: 2})
	if c.isError(lhs) || c.isError(rhs) {
		return c.errT
	}
	// The right operand may have told what the left one is.
	switch c.eqKindOf(lhs) {
	case eqRejected:
		c.rejectedEqualityError(lhsAt, lhs, op)
	case eqUnknown:
		c.equalities = append(c.equalities, pendingEquality{operand: lhs, at: lhsAt, op: op})
	}
	return c.boolT
}

// eqKind is what is known about the type of a left operand of `==` or `!=`.
type eqKind uint8

const (
	// eqOK is Int, Bool or the error type.
	eqOK eqKind = iota
	// eqUnknown is a variable that nothing has bound.
	eqUnknown
	// eqRejected is any other type: a function or `()`.
	eqRejected
)

// eqKindOf says what ty, resolved, is for an equality.
func (c *checker) eqKindOf(ty TypeID) eqKind {
	switch t := c.tp.Types.Resolve(ty); {
	case t == c.intT || t == c.boolT || t == c.errT:
		return eqOK
	case c.tp.Types.Node(t).Kind == TypeVariable:
		return eqUnknown
	default:
		return eqRejected
	}
}

// rejectedEqualityError reports E0401 for the left operand of `==` or `!=`,
// which is written at and has the type ty, a type the operator does not work on.
func (c *checker) rejectedEqualityError(at base.Span, ty TypeID, op string) {
	c.sink.Report(diag.NewError(diag.E(401), "mismatched types",
		diag.Label{Span: at, Message: "expected `Int` or `Bool`, found `" + c.tp.Types.Show(ty) + "`"}).
		WithNote("`" + op + "` works on `Int` and `Bool` here"))
}

// judgeEqualities judges the left operands of the equalities that had no type
// when their body was checked. By now every body has had its say about a
// parameter, so a type that is not Int or Bool is reported as it would have
// been if the call had come first. A variable that is still free is left alone:
// nothing says it is a function or `()`. A program can therefore be accepted
// with a free variable as the type of an operand of `==` (`fn same(x, y) ->
// Bool { x == y }` and no call), and the table keeps it: a pass that needs a
// type for every operand has to decide what that means.
func (c *checker) judgeEqualities() {
	for _, q := range c.equalities {
		if c.eqKindOf(q.operand) == eqRejected {
			c.rejectedEqualityError(q.at, q.operand, q.op)
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
			c.arityError(e, node)
			c.inferAll(args)
			return node.Ret
		}
		for i, arg := range args {
			c.check(arg, node.Params[i], Reason{Kind: ReasonArgument, Because: c.paramSpan(e.Callee, i)})
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
		// A variable and a function type cannot mismatch, so the reason is
		// never read: this reports only the occurs check (`fn f(x) { x(x) }`).
		c.u.Unify(ct, c.tp.Types.Fun(types, ret, c.tp.Types.EmptyRow()), c.m.Exprs.Get(e.Callee).Span,
			Reason{Kind: ReasonArgument})
		return ret
	case TypeErr:
		c.inferAll(args)
		return c.errT
	case TypeBuiltin:
		c.notCallableError(e, ct)
		c.inferAll(args)
		return c.errT
	default:
		panic("unreachable")
	}
}

// inferAll infers the expressions ids, for the mistakes inside them.
func (c *checker) inferAll(ids []syntax.ExprID) {
	for _, id := range ids {
		c.infer(id)
	}
}

// calleeDef returns the definition that the callee expression is the name of.
func (c *checker) calleeDef(callee syntax.ExprID) (Def, bool) {
	if res := c.rm.ExprRes[callee]; c.m.Exprs.Get(callee).Kind == syntax.ExprName && res.Kind == ResDef {
		return c.res.Defs.Get(res.Def), true
	}
	return Def{}, false
}

// paramSpan returns the span of the i-th parameter of the function that the
// callee expression is the name of: the secondary label of an argument that
// does not fit. If the callee is not the name of a function (it is a variable
// that holds one), there is no parameter to point at, and the result is the
// zero Span, which Unify takes for no place at all.
func (c *checker) paramSpan(callee syntax.ExprID, i int) base.Span {
	def, ok := c.calleeDef(callee)
	if !ok {
		return base.Span{}
	}
	m := c.mods[def.Module]
	return m.Params(m.ItemNodes.Get(def.Item).Params)[i].Span
}

// arityError reports E0402 for the call e of a function of type fn with another
// number of arguments than parameters. The primary label is on the argument
// list. If the callee is a name, a note gives the parameter types, and, if it
// is a function of the user's file, a secondary label shows where it is
// declared. (One in the prelude is another file, which cannot be drawn under
// this one.)
func (c *checker) arityError(e syntax.Expr, fn Type) {
	d := diag.NewError(diag.E(402),
		"this function takes "+strconv.Itoa(len(fn.Params))+" argument(s) but "+strconv.Itoa(int(e.Args.Count))+" were supplied",
		diag.Label{Span: e.OpSpan, Message: "supplied here"})
	if callee := c.m.Exprs.Get(e.Callee); callee.Kind == syntax.ExprName {
		name := c.in.String(callee.Name)
		if def, ok := c.calleeDef(e.Callee); ok && def.Module != preludeModule {
			d = d.WithSecondary(diag.Label{Span: def.Span, Message: "`" + name + "` is declared here"})
		}
		d = d.WithNote("`" + name + "` takes `(" + strings.Join(c.tp.Types.showAll(fn.Params...), ", ") + ")`")
	}
	c.sink.Report(d)
}

// notCallableError reports E0403 for the call e whose callee has the type ty,
// which is not a function. If the callee is a variable, a secondary label shows
// where it is bound. (A definition is always a function, so a callee that is a
// definition never gets here.)
func (c *checker) notCallableError(e syntax.Expr, ty TypeID) {
	callee := c.m.Exprs.Get(e.Callee)
	d := diag.NewError(diag.E(403), "cannot call a value of type `"+c.tp.Types.Show(ty)+"`",
		diag.Label{Span: callee.Span, Message: "not a function"})
	if res := c.rm.ExprRes[e.Callee]; callee.Kind == syntax.ExprName && res.Kind == ResLocal {
		local := c.res.Locals.Get(res.Local)
		d = d.WithSecondary(diag.Label{Span: local.Span, Message: "`" + c.in.String(local.Name) + "` is bound here"})
	}
	c.sink.Report(d)
}
