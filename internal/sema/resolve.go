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

// Local is a local variable: a parameter or a `let`. Resolve makes none yet;
// the next session binds them.
type Local struct {
	Name base.Symbol
	// Span is the span of the name where it is bound.
	Span base.Span
	// Owner is the function whose body binds it.
	Owner DefID
}

// ResKind says what a Res refers to.
type ResKind uint8

// The kinds of resolution.
const (
	// ResNone is the zero Res: nothing was resolved. Every expression that is
	// not a name has it. So has a name that is not a definition: until locals
	// exist (next session) the resolver does not look at parameters or `let`s,
	// and it does not yet report a name that is nowhere.
	ResNone ResKind = iota
	// ResDef is a name that stands for a top-level definition, Res.Def.
	ResDef
	// ResLocal is a name that stands for a local variable, Res.Local.
	ResLocal
	// ResError is a name that stands for nothing and has been reported.
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
	// means something only for a StmtLet, and nothing sets it until locals
	// exist: every row is 0 for now.
	LetLocal []LocalID
	// ParamLocals is indexed by ItemID: the variables that the parameters of a
	// function bind, in order. Nothing sets it until locals exist: every row is
	// nil for now.
	ParamLocals [][]LocalID
}

// ResolvedProgram is the result of Resolve.
type ResolvedProgram struct {
	// Defs lists the top-level definitions of every module: those of module 0
	// first and, within a module, in source order. A function defined twice
	// has a Def for each definition.
	Defs   base.IdVec[DefID, Def]
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
// ones further down the file. The second walks the body of each function and
// resolves its names. In the body of a module's function a name stands first
// for a definition of that module and then for a `pub` definition of the
// prelude. Each module has a name space of its own, so a module can define a
// name that the prelude has already defined: its own definition wins.
//
// It reports to sink: E0302 for a function defined twice in a module, E0304
// for `#[extern]` anywhere but the standard library (any module but user) and
// E0305 for any other attribute, in any module. A tree that has syntax errors
// is resolved like any other: its error nodes stand for nothing and are
// skipped. Resolve does not change the modules.
//
// The second pass recurses once per level of the tree. Parse bounds that depth
// (MaxNesting levels, and the operators and calls of one statement, MaxChain,
// as PrintModule's comment says), so a tree the parser made is safe; a tree
// built by hand is not.
func Resolve(mods []*syntax.Module, user ModuleIdx, in *base.Interner, sink *diag.Sink) *ResolvedProgram {
	r := &resolver{mods: mods, user: user, in: in, sink: sink, rp: &ResolvedProgram{}}
	r.rp.Modules = make([]ModuleRes, len(mods))
	r.tops = make([]map[base.Symbol]DefID, len(mods))
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

	// The module whose function bodies the second pass is walking, its tree
	// and its table.
	mod ModuleIdx
	m   *syntax.Module
	res *ModuleRes
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
				// The parser could not read it and has reported that.
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
	r.sink.Report(diag.NewError(diag.E(302), "`"+r.in.String(it.Name)+"` is defined more than once",
		diag.Label{Span: it.NameSpan, Message: "redefined here"}).
		WithSecondary(diag.Label{Span: r.rp.Defs.Get(first).Span, Message: "first defined here"}))
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
			r.sink.Report(diag.NewError(diag.E(305), "unknown attribute `"+name+"`",
				diag.Label{Span: a.Span, Message: "the only attribute in Part I is `#[extern]`"}))
		case mod == r.user:
			r.sink.Report(diag.NewError(diag.E(304), "`#[extern]` is only allowed in the standard library",
				diag.Label{Span: a.Span, Message: "remove the attribute"}))
		case kind != DefExternFn:
			kind = DefExternFn
			if len(a.Args) > 0 {
				externName = a.Args[0]
			}
		}
	}
	return kind, externName
}

// lookup returns the top-level definition that name stands for in module mod:
// the module's own, and failing that a `pub` one of the prelude. (The prelude
// itself has no other module to fall back on.)
func (r *resolver) lookup(mod ModuleIdx, name base.Symbol) (DefID, bool) {
	if def, ok := r.tops[mod][name]; ok {
		return def, true
	}
	if mod != preludeModule {
		if def, ok := r.tops[preludeModule][name]; ok && r.rp.Defs.Get(def).Pub {
			return def, true
		}
	}
	return 0, false
}

// bodies is the second pass: it walks the body of every function with one.
func (r *resolver) bodies() {
	for i, m := range r.mods {
		r.mod, r.m, r.res = ModuleIdx(i), m, &r.rp.Modules[i]
		for _, id := range m.Items {
			if it := m.ItemNodes.Get(id); it.Kind == syntax.ItemFn && it.HasBody {
				r.expr(it.Body)
			}
		}
	}
}

// expr resolves the names in the expression id and in everything inside it.
func (r *resolver) expr(id syntax.ExprID) {
	e := r.m.Exprs.Get(id)
	switch e.Kind {
	case syntax.ExprInt, syntax.ExprBool, syntax.ExprUnit, syntax.ExprError:
		// No names inside.
	case syntax.ExprName:
		if def, ok := r.lookup(r.mod, e.Name); ok {
			r.res.ExprRes[id] = Res{Kind: ResDef, Def: def}
		}
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
		for _, s := range r.m.StmtList(e.Stmts) {
			r.stmt(s)
		}
		if e.HasTail {
			r.expr(e.Tail)
		}
	default:
		panic("unreachable")
	}
}

// stmt resolves the names in the statement id.
func (r *resolver) stmt(id syntax.StmtID) {
	s := r.m.Stmts.Get(id)
	switch s.Kind {
	case syntax.StmtLet, syntax.StmtExpr:
		r.expr(s.Expr)
	case syntax.StmtError:
		// Only its span is set: there is no expression to walk.
	default:
		panic("unreachable")
	}
}
