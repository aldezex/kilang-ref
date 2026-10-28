// Package sema is the meaning of a Ki program: what each name stands for
// (resolve.go) and, in the chapters that follow, what type each expression has.
package sema

import (
	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// DefID names a top-level definition in ResolvedProgram.Defs.
type DefID uint32

// LocalID names a local variable (a parameter or a `let`) in
// ResolvedProgram.Locals.
type LocalID uint32

// ModuleIdx is the position of a module in the slice that Resolve receives.
// Module 0 is the prelude.
type ModuleIdx uint32

// preludeModule is the module whose `pub` definitions every other module sees.
const preludeModule ModuleIdx = 0

// DefKind says what a Def is.
type DefKind uint8

// The kinds of definition.
const (
	// DefFn is a function written in Ki.
	DefFn DefKind = iota
	// DefExternFn is a function whose code is not written in Ki: the
	// standard library declares it with `#[extern("name")]`, and the
	// interpreter supplies the code under that name.
	DefExternFn
)

// Def is a top-level definition: today, a function.
type Def struct {
	Name base.Symbol
	Kind DefKind
	// Module is the module that defines it. Item is the function's item in
	// that module's tree.
	Module ModuleIdx
	Item   syntax.ItemID
	// Span is the span of the name where it is defined (`fib` in `fn fib`).
	Span base.Span
	// Pub says whether it was written with `pub`: the prelude's `pub`
	// definitions are the ones that other modules can see.
	Pub bool
	// ExternName is the name that DefExternFn functions go by in the
	// interpreter: the first argument of the attribute (`ki_print_int`). It is
	// empty for DefFn, and for an `#[extern]` without arguments: nothing checks
	// the arguments in Part I, because only the standard library, which we
	// write, may use the attribute.
	ExternName string
}

// Local is a local variable: a parameter or a `let`. Every parameter and every
// `let` in the program is one Local, even when another one has the same name:
// `let x = 1` followed by `let x = 2` is two.
type Local struct {
	Name base.Symbol
	// Span is the span of the name where it is bound: `x` in `let x = 1` and
	// in the parameter `x: Int`, not the whole parameter.
	Span base.Span
	// Owner is the function that binds it, as a parameter or in its body.
	Owner DefID
}

// ResKind says what a Res refers to.
type ResKind uint8

// The kinds of resolution.
const (
	// ResNone is the zero Res: nothing was resolved. Every expression that is
	// not a name has it. A name that an item reaches has another kind once
	// Resolve has run; one that no item reaches keeps it. When the parser
	// recovers from a syntax error inside a body it drops the sub-expression
	// that it replaces with an error node: the names in it stay in the arena, no
	// item reaches them and Resolve never sees them.
	ResNone ResKind = iota
	// ResDef is a name that stands for a top-level definition, Res.Def.
	ResDef
	// ResLocal is a name that stands for a local variable, Res.Local.
	ResLocal
	// ResError is a name that stands for nothing: Resolve has reported it
	// (E0301), or it is the name of a function whose signature the parser could
	// not read, which the parser has reported.
	ResError
)

// Res is what a name in an expression stands for. Def means something only
// for ResDef and Local only for ResLocal.
type Res struct {
	Kind  ResKind
	Def   DefID
	Local LocalID
}

// ModuleRes holds what Resolve learns about the nodes of one module, in tables
// beside its syntax tree (spec 7.2): the tree itself is never changed.
//
// Resolve allocates every table with a row for each node, so any id of the
// tree is a valid index.
type ModuleRes struct {
	// ExprRes is indexed by ExprID: what each name stands for. The rows of
	// expressions that are not names stay zero.
	ExprRes []Res
	// LetLocal is indexed by StmtID: the variable that a `let` binds. It
	// means something only for a StmtLet; the other rows stay 0.
	LetLocal []LocalID
	// ParamLocals is indexed by ItemID: the variables that the parameters of a
	// function bind, in order, whether or not the function has a body. The row
	// of a function with no parameters, and that of an item that is not a
	// function, is empty.
	ParamLocals [][]LocalID
}

// ResolvedProgram is the result of Resolve.
type ResolvedProgram struct {
	// Defs lists the top-level definitions of every module: those of module 0
	// first and, within a module, in source order. A function defined twice
	// has a Def for each definition.
	Defs base.IdVec[DefID, Def]
	// Locals lists the local variables of every module: the ones that
	// ModuleRes.ParamLocals and ModuleRes.LetLocal name, and that a name
	// refers to with ResLocal.
	Locals base.IdVec[LocalID, Local]
	// Modules has one entry for each module, in the order of the modules.
	Modules []ModuleRes
	// Main is the `main` function of the user's module. It means something
	// only if HasMain is true; a program with no `main` is not a mistake of
	// name resolution (`ki run` reports it).
	Main    DefID
	HasMain bool
}

// Resolve finds what the names of a program stand for. mods are its modules,
// with the prelude as module 0, and user is the index of the user's own file.
// user must be a valid index.
//
// It makes two passes. The first collects the top-level definitions of every
// module, before any body is read, so that a function can call itself and the
// ones further down the file. The second binds the parameters of each function
// and walks its body, resolving its names. A name stands first for a local
// variable (see scope), the innermost one if there are several; failing that
// for a definition of its module and then for a `pub` definition of the
// prelude. Each module has a name space of its own, so a module can define a
// name that the prelude has already defined: its own definition wins.
//
// A `let` binds its name from the statement after it to the end of its block.
// Its own initializer does not see it, so `let x = x` reads the `x` outside.
// A name can be bound again, in the same block or in one inside it: the later
// binding shadows the earlier one, whose Local still exists. That holds for the
// parameters too: `fn f(x: Int, x: Int)` binds two variables and the body sees
// the second. Nothing is reported, because Ki defines no diagnostic for a
// repeated parameter (TestScopes/shadowing_picks_innermost pins this).
//
// It reports to sink: E0301 for a name that stands for nothing, with the help
// line "did you mean `fib`?" and the fix-it that writes `fib` if a variable or
// definition it could have stood for is spelled almost like it, and with a
// label on the `let` and a note if the name is the one that `let` binds;
// E0302 for a function defined twice in a module, E0304 for `#[extern]` in the
// user's module and E0305 for any other attribute, in any module. The last two
// have the fix-it that removes the attribute when it is on one line, except
// E0304 on a function with no body (the edit alone would leave a function
// without a body), and E0305 in the standard library has the one that fixes the
// name when it is close to `extern`; in the user's module `extern` is not an
// attribute to suggest (it is E0304 there), so E0305 only offers to remove the
// attribute. A tree that has syntax errors is resolved like any other: its
// error nodes are skipped and define nothing. The exception is a function whose
// signature the parser could not read after its name: that name stands for
// ResError wherever the module uses it, and Resolve reports nothing for it (the
// parser has reported the signature), so a call to it does not add an E0301 to
// that error. It is not a definition (it has no Def), a definition of the same
// name in the module wins over it and a variable hides it. Resolve does not
// change the modules.
//
// The second pass recurses once per level of the tree. Parse bounds that depth
// (MaxNesting levels, and the operators and calls of one statement, MaxChain,
// as PrintModule's comment says), so a tree the parser made is safe; a tree
// built by hand is not.
func Resolve(mods []*syntax.Module, user ModuleIdx, in *base.Interner, sink *diag.Sink) *ResolvedProgram {
	r := &resolver{mods: mods, user: user, in: in, sink: sink, rp: &ResolvedProgram{}}
	r.rp.Modules = make([]ModuleRes, len(mods))
	r.tops = make([]map[base.Symbol]DefID, len(mods))
	r.broken = make(map[moduleName]bool)
	for i, m := range mods {
		r.rp.Modules[i] = ModuleRes{
			ExprRes:     make([]Res, m.Exprs.Len()),
			LetLocal:    make([]LocalID, m.Stmts.Len()),
			ParamLocals: make([][]LocalID, m.ItemNodes.Len()),
		}
		r.tops[i] = make(map[base.Symbol]DefID)
	}
	r.collect()
	r.bodies()
	return r.rp
}

// resolver is the state of one call to Resolve.
type resolver struct {
	mods []*syntax.Module
	user ModuleIdx
	in   *base.Interner
	sink *diag.Sink
	rp   *ResolvedProgram

	// tops[m] maps each name defined at the top level of module m to its first
	// definition. The maps are only ever looked up, never iterated, so nothing
	// depends on the order of a map.
	tops []map[base.Symbol]DefID

	// broken holds the functions whose signature the parser could not read
	// (error items that kept their name). They are not definitions, but they
	// exist: see lookupDef. None is the zero value.
	broken map[moduleName]bool

	// The module whose function bodies the second pass is walking, its tree
	// and its table.
	mod ModuleIdx
	m   *syntax.Module
	res *ModuleRes

	// The function whose body is being walked and the scope of the place in it
	// that is being walked.
	owner DefID
	scope *scope

	// initializing holds the `let`s whose initializer is being walked, the
	// innermost last: the name each one binds and where. A name that is unknown
	// there and is the name of one of them is `let x = x`, which gets an
	// explanation of its own (see name).
	initializing []letName
}

// moduleName is a name in the name space of one module.
type moduleName struct {
	mod  ModuleIdx
	name base.Symbol
}

// letName is the name that a `let` binds, with the span of the name in the
// `let`.
type letName struct {
	name base.Symbol
	span base.Span
}

// collect is the first pass: it makes a Def for every function of every module.
func (r *resolver) collect() {
	for i, m := range r.mods {
		mod := ModuleIdx(i)
		for _, id := range m.Items {
			it := m.ItemNodes.Get(id)
			switch it.Kind {
			case syntax.ItemFn:
				r.define(mod, id, it)
			case syntax.ItemError:
				// The parser could not read it and has reported that. If it
				// had read the name of a function before the signature went
				// wrong, the function exists, only without a signature that
				// anything can use: remember the name, so that a call to it
				// is not an unknown name (see lookupDef).
				if it.Name != base.NoSymbol {
					r.broken[moduleName{mod, it.Name}] = true
				}
			default:
				panic("unreachable")
			}
		}
	}
	if main, ok := r.in.Lookup([]byte("main")); ok {
		r.rp.Main, r.rp.HasMain = r.tops[r.user][main]
	}
}

// define adds the function it, the item id of module mod, to the program. A
// name that the module already defines is E0302, and the Def is added all the
// same, so the item has one and the name keeps standing for the first.
func (r *resolver) define(mod ModuleIdx, id syntax.ItemID, it syntax.Item) {
	kind, externName := r.attributes(mod, r.mods[mod], it)
	def := r.rp.Defs.Push(Def{
		Name: it.Name, Kind: kind, Module: mod, Item: id, Span: it.NameSpan, Pub: it.Pub, ExternName: externName,
	})
	first, dup := r.tops[mod][it.Name]
	if !dup {
		r.tops[mod][it.Name] = def
		return
	}
	// No fix-it: which of the two stays is not for the compiler to say.
	r.sink.Report(diag.NewError(diag.E(302), "`"+r.in.String(it.Name)+"` is defined more than once",
		diag.Label{Span: it.NameSpan, Message: "redefined here"}).
		WithSecondary(diag.Label{Span: r.rp.Defs.Get(first).Span, Message: "first defined here"}).
		WithHelp("rename one of them, or delete the second"))
}

// attributes checks the attributes of the function it of module m and says what
// kind of function it is. In Part I the only attribute is `extern`, which marks
// a function of the standard library whose code the interpreter supplies. The
// user's module may not use it (E0304: the function is a plain DefFn then), and
// no module may use any other (E0305). If a function has several `extern`
// attributes the first gives the name.
func (r *resolver) attributes(mod ModuleIdx, m *syntax.Module, it syntax.Item) (kind DefKind, externName string) {
	for _, a := range m.Attrs(it.Attrs) {
		name := string(r.in.Bytes(a.Name))
		switch {
		case name != syntax.ExternAttr:
			r.unknownAttribute(mod, a, name)
		case mod == r.user:
			d := diag.NewError(diag.E(304), "`#[extern]` is only allowed in the standard library",
				diag.Label{Span: a.Span, Message: "not allowed outside the standard library"}).
				WithNote("extern functions are declared by the standard library and implemented by the runtime; a program calls them through the prelude")
			if it.HasBody {
				d = removable(d, a, "remove the attribute")
			} else {
				// An extern function has no body, and removing the attribute
				// leaves one that the parser refuses (E0201), so the help names
				// both steps and there is no edit that does either alone.
				d = d.WithHelp("remove the attribute, and write a body for the function")
			}
			r.sink.Report(d)
		case kind != DefExternFn:
			kind = DefExternFn
			if len(a.Args) > 0 {
				externName = a.Args[0]
			}
		}
	}
	return kind, externName
}

// unknownAttribute reports E0305 for the attribute a, called name, of a function
// of module mod. In the standard library, if the name is close to `extern` the
// help suggests it and the fix-it writes it in place of the name; otherwise the
// help is to remove the attribute. In the user's module there is nothing to
// suggest, because `extern` is not allowed there (E0304) and a program has no
// other attribute: the help is to remove it, and the note does not list
// `extern` as one that Ki knows.
func (r *resolver) unknownAttribute(mod ModuleIdx, a syntax.Attr, name string) {
	d := diag.NewError(diag.E(305), "unknown attribute `"+name+"`",
		diag.Label{Span: a.Span, Message: "not an attribute Ki knows"})
	if mod == r.user {
		r.sink.Report(removable(d.WithNote("a program cannot use attributes yet"), a, "remove it"))
		return
	}
	d = d.WithNote("the attributes Ki knows: `" + syntax.ExternAttr + "`")
	if best, ok := suggest(name, []string{syntax.ExternAttr}); ok {
		d = d.WithHelp("did you mean `" + best + "`?").WithFixIt(diag.Replace(a.NameSpan, best))
	} else {
		d = removable(d, a, "remove it")
	}
	r.sink.Report(d)
}

// removable adds to d the help line help and, if the attribute a is on one line,
// the fix-it that deletes it. An attribute can run over several lines (a line
// break inside its brackets or parentheses is only a space), and a fix-it is
// an edit of one line, so for that one the help is all there is.
func removable(d diag.Diagnostic, a syntax.Attr, help string) diag.Diagnostic {
	d = d.WithHelp(help)
	if a.OneLine {
		d = d.WithFixIt(diag.Delete(a.Span))
	}
	return d
}

// lookupDef returns what name stands for among the top-level functions of module
// mod, and whether it stands for anything there: the module's own definition;
// failing that a function of the module whose signature the parser could not
// read, which is ResError, because there is nothing to resolve to and the parser
// has reported the mistake; and failing that a `pub` definition of the prelude.
// (The prelude itself has no other module to fall back on.) A function that
// could not be read comes before the prelude's, as the module's own definition
// does. It knows nothing of local variables: see name.
func (r *resolver) lookupDef(mod ModuleIdx, name base.Symbol) (Res, bool) {
	if def, ok := r.tops[mod][name]; ok {
		return Res{Kind: ResDef, Def: def}, true
	}
	if r.broken[moduleName{mod, name}] {
		return Res{Kind: ResError}, true
	}
	if mod != preludeModule {
		if def, ok := r.tops[preludeModule][name]; ok && r.rp.Defs.Get(def).Pub {
			return Res{Kind: ResDef, Def: def}, true
		}
	}
	return Res{}, false
}

// bodies is the second pass: it goes through the definitions, which are in the
// order of the modules and, within one, of the source, binds the parameters of
// each function and walks the body of those that have one.
func (r *resolver) bodies() {
	for id, def := range r.rp.Defs.All() {
		r.mod, r.m, r.res = def.Module, r.mods[def.Module], &r.rp.Modules[def.Module]
		r.function(id, def)
	}
}

// function binds the parameters of the function that def defines, id being its
// DefID, and resolves the names in its body if it has one. The parameters are a
// scope of their own and the body, a block, is a scope inside it: a `let` in the
// body that has the name of a parameter shadows it, and so does a later
// parameter of the same name.
//
// A function with no body, such as an `#[extern]` one, binds its parameters all
// the same: they are variables that nothing can read.
func (r *resolver) function(id DefID, def Def) {
	it := r.m.ItemNodes.Get(def.Item)
	r.owner, r.scope = id, &scope{}
	var params []LocalID
	for _, p := range r.m.Params(it.Params) {
		// Param.Span covers `x: Int`. The name is its first len(name) bytes.
		name := base.Span{File: p.Span.File, Lo: p.Span.Lo, Hi: p.Span.Lo + uint32(len(r.in.Bytes(p.Name)))}
		params = append(params, r.bind(p.Name, name))
	}
	r.res.ParamLocals[def.Item] = params
	if it.HasBody {
		r.expr(it.Body)
	}
}

// bind makes a new variable of the function being walked, called name and bound
// at span, and makes name stand for it in the scope being walked from now on.
func (r *resolver) bind(name base.Symbol, span base.Span) LocalID {
	id := r.rp.Locals.Push(Local{Name: name, Span: span, Owner: r.owner})
	r.scope.bind(name, id)
	return id
}

// name says what the name written in e stands for: a local variable if a scope
// has one by that name, and failing that a definition. If there is none it
// reports E0301, with a help line and a fix-it if a name that it could have
// meant exists (see suggest) and the sink has not yet dropped an error, and
// says ResError. If the name is the one that a `let` whose initializer is being
// walked binds, the diagnostic points at that `let` and gives the rule.
func (r *resolver) name(e syntax.Expr) Res {
	if local, ok := r.scope.lookup(e.Name); ok {
		return Res{Kind: ResLocal, Local: local}
	}
	if res, ok := r.lookupDef(r.mod, e.Name); ok {
		return res
	}
	name := r.in.String(e.Name)
	d := diag.NewError(diag.E(301), "unknown name `"+name+"`",
		diag.Label{Span: e.Span, Message: "not found in this scope"})
	// A `let` is visible only after its own statement, so the name that a `let`
	// binds is unknown in its initializer. That is a mistake the user may not
	// see, so the diagnostic points at the `let` and gives the rule. The
	// innermost `let` that binds the name is the one it points at.
	for i := len(r.initializing) - 1; i >= 0; i-- {
		if l := r.initializing[i]; l.name == e.Name {
			d = d.WithSecondary(diag.Label{Span: l.span, Message: "`" + name + "` is bound by this `let`, which cannot see itself"}).
				WithNote("a `let` is visible only after its own statement")
			break
		}
	}
	// The help line costs a pass over every candidate. Once the sink is full it
	// drops what it is given, so a file with thousands of unknown names would
	// pay for thousands of suggestions that nobody reads.
	if !r.sink.Truncated() {
		if best, ok := suggest(name, r.candidates()); ok {
			d = d.WithHelp("did you mean `" + best + "`?").WithFixIt(diag.Replace(e.Span, best))
		}
	}
	r.sink.Report(d)
	return Res{Kind: ResError}
}

// expr resolves the names in the expression id and in everything inside it.
func (r *resolver) expr(id syntax.ExprID) {
	e := r.m.Exprs.Get(id)
	switch e.Kind {
	case syntax.ExprInt, syntax.ExprBool, syntax.ExprUnit, syntax.ExprError:
		// No names inside.
	case syntax.ExprName:
		r.res.ExprRes[id] = r.name(e)
	case syntax.ExprCall:
		r.expr(e.Callee)
		for _, arg := range r.m.ExprList(e.Args) {
			r.expr(arg)
		}
	case syntax.ExprBinary:
		r.expr(e.Lhs)
		r.expr(e.Rhs)
	case syntax.ExprUnary:
		r.expr(e.Operand)
	case syntax.ExprIf:
		r.expr(e.Cond)
		r.expr(e.Then)
		if e.HasElse {
			r.expr(e.Else)
		}
	case syntax.ExprBlock:
		// A block is a scope inside the one around it. Its variables are the
		// ones its `let`s bind, and they end with it, tail included.
		outer := r.scope
		r.scope = &scope{parent: outer}
		for _, s := range r.m.StmtList(e.Stmts) {
			r.stmt(s)
		}
		if e.HasTail {
			r.expr(e.Tail)
		}
		r.scope = outer
	default:
		panic("unreachable")
	}
}

// stmt resolves the names in the statement id.
func (r *resolver) stmt(id syntax.StmtID) {
	s := r.m.Stmts.Get(id)
	switch s.Kind {
	case syntax.StmtLet:
		// The initializer first: the variable is not bound, so not visible in
		// it, until the initializer has been resolved.
		r.initializing = append(r.initializing, letName{name: s.Name, span: s.NameSpan})
		r.expr(s.Expr)
		r.initializing = r.initializing[:len(r.initializing)-1]
		r.res.LetLocal[id] = r.bind(s.Name, s.NameSpan)
	case syntax.StmtExpr:
		r.expr(s.Expr)
	case syntax.StmtError:
		// Only its span is set: there is no expression to walk.
	default:
		panic("unreachable")
	}
}
