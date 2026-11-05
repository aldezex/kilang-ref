package sema

import (
	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// TypeDefID names a declared type in Decls.Defs: a `type` of the program, which
// is a sum, a record or an alias.
type TypeDefID uint32

// CtorID names a constructor in Decls.Ctors: a variant of a sum.
type CtorID uint32

// TypeDef is a type that the program declares, as Declare read it. Name and Span
// are the name of the type where it is declared, and Module and Item say where
// the declaration is, so that a pass that needs more of it (a label on a field,
// the attributes) can read it from the tree.
//
// What else it holds depends on Form: a sum has Ctors, its constructors in
// source order (a constructor's Tag is its position in this list); a record has
// Fields; an alias has Alias, the type that it stands for, already expanded: the
// alias of an alias of `Float` is `Float`, with no alias left in it. For the
// other two forms Alias is the error type, which nothing reads.
type TypeDef struct {
	Name   base.Symbol
	Span   base.Span
	Module ModuleIdx
	Item   syntax.ItemID
	Form   syntax.TypeForm
	Ctors  []CtorID
	Fields []FieldDef
	Alias  TypeID
}

// FieldDef is a field of a record: its name where it is declared, its span (the
// name and the type, `x: Float`) and its type.
type FieldDef struct {
	Name base.Symbol
	Span base.Span
	Type TypeID
}

// Ctor is a constructor, a variant of the sum Owner. Tag is its position among
// the variants of its type, from 0, and Args are the types of its fields in
// order. Fields holds their names when the variant names them (`Rect(w: Float,
// h: Float)`) and is nil when it does not (`Pair(Int, Bool)`); a variant that
// names some fields and not others (`Mixed(a: Int, Bool)`) has an entry for each
// field, base.NoSymbol for the ones without a name. Span is the name of the
// constructor where it is declared: a constructor does not keep the spans of its
// fields, and the tree has them (see Decls.FieldSpan).
type Ctor struct {
	Name   base.Symbol
	Span   base.Span
	Owner  TypeDefID
	Tag    uint32
	Args   []TypeID
	Fields []base.Symbol
}

// Decls is what Declare makes of the declarations of a program: the table of
// types, which holds one type for each declared type, the declared types
// themselves and their constructors. Traits, impls and constants join it in the
// chapters that bring them.
type Decls struct {
	Types *TypeTable
	Defs  base.IdVec[TypeDefID, TypeDef]
	Ctors base.IdVec[CtorID, Ctor]

	// typeOf and ctorOf go from the definition that Resolve made of a type or of
	// a constructor to what Declare made of it. They are only looked up, never
	// iterated.
	typeOf map[DefID]TypeDefID
	ctorOf map[DefID]CtorID
}

// TypeDefOf returns the declared type that the definition def (a DefType) is,
// and whether def is one.
func (d *Decls) TypeDefOf(def DefID) (TypeDefID, bool) {
	td, ok := d.typeOf[def]
	return td, ok
}

// CtorOf returns the constructor that the definition def (a DefCtor) is, and
// whether def is one.
func (d *Decls) CtorOf(def DefID) (CtorID, bool) {
	c, ok := d.ctorOf[def]
	return c, ok
}

// Meaning is the type that the name of the declared type td stands for: the type
// of the declaration for a sum or a record, and the type that the alias expands
// to for an alias.
func (d *Decls) Meaning(td TypeDefID) TypeID {
	def := d.Defs.Get(td)
	if def.Form == syntax.TypeFormAlias {
		return def.Alias
	}
	return d.Types.Data(td, nil)
}

// CtorType is the type of the constructor c as a value. A constructor with
// fields is a function from their types to its type, `Circle: (Float) -> Shape`,
// so it can be called and also passed where a function is wanted; one without
// fields is a value of its type, `None: Shape`, which is not called.
func (d *Decls) CtorType(c CtorID) TypeID {
	ctor := d.Ctors.Get(c)
	owner := d.Types.Data(ctor.Owner, nil)
	if len(ctor.Args) == 0 {
		return owner
	}
	return d.Types.Fun(ctor.Args, owner, d.Types.EmptyRow())
}

// FieldSpan returns the span of the i-th field of the constructor c, as the
// declaration of its variant writes it: the secondary label of an argument that
// does not fit. mods are the modules that Declare received.
func (d *Decls) FieldSpan(mods []*syntax.Module, c CtorID, i int) base.Span {
	ctor := d.Ctors.Get(c)
	owner := d.Defs.Get(ctor.Owner)
	m := mods[owner.Module]
	variant := m.Variants(m.ItemNodes.Get(owner.Item).Variants)[ctor.Tag]
	return m.Fields(variant.Fields)[i].Span
}

// Declare is the pass that makes the types of the declarations: between Resolve
// and Check, it reads every `type` of the program and makes the TypeTable, which
// Check then uses (a Check that gets the Decls of another program is a bug of
// the caller, as with Resolve). mods and res are what Resolve received and
// returned, and the prelude is module 0.
//
// It works in three sweeps, each over all the declarations, which is why a field
// may name a type declared further down the file, or a type that names it back
// (`type A { Mk(b: B) }` and `type B { a: A }`): declaration does not depend on
// the order of the source.
//
//  1. Each sum and record is a declared type, with its own type in the table
//     (hash-consed, TypeData), and each variant a constructor, with its Tag.
//  2. Each alias is expanded, so that no type in the table is an alias: an alias
//     of an alias is the type of the last one, and an alias of a declared type is
//     that type. A cycle of aliases (`type A = B` and `type B = A`, or `type A =
//     A`) has no expansion: it is one E0801 for the cycle, on its first alias in
//     the source, with a secondary label on each of the others; and the aliases
//     in it, and any alias that stands for one of them, have the error type, which
//     says nothing wherever it is used. A sum or a record that names itself is not
//     a cycle: it holds its type by name, not by what the name expands to.
//  3. The types of the fields of each constructor and each record are read from
//     their type expressions.
//
// An unknown type name in an alias or in a field is E0406, as in an annotation
// (see typeReader), and has the error type. A type that the parser could not read
// after its name is declared by nobody: its name stands for the error type (see
// ResolvedProgram.LookupType). A type or a constructor that is defined twice is
// E0302 from Resolve, and is declared all the same, as it is defined: the name
// stands for the first one.
//
// Following an alias goes along its chain with a loop and a stack of its own, so a
// chain of aliases of any length adds no frames of Go.
func Declare(mods []*syntax.Module, res *ResolvedProgram, in *base.Interner, sink *diag.Sink) *Decls {
	d := &declarer{
		mods: mods, res: res, in: in, sink: sink,
		decls: &Decls{Types: NewTypeTable(), typeOf: make(map[DefID]TypeDefID), ctorOf: make(map[DefID]CtorID)},
	}
	d.reader = typeReader{mods: mods, res: res, in: in, sink: sink, decls: d.decls}
	d.collect()
	d.aliases()
	d.members()
	return d.decls
}

// declarer is the state of one call to Declare.
type declarer struct {
	mods   []*syntax.Module
	res    *ResolvedProgram
	in     *base.Interner
	sink   *diag.Sink
	decls  *Decls
	reader typeReader
}

// collect is the first sweep: a TypeDef for each type and a Ctor, with its tag
// and no argument types yet, for each constructor. The definitions are in DefID
// order and the constructors of a type come right after it, so a constructor
// belongs to the type of its own item.
func (d *declarer) collect() {
	type itemKey struct {
		mod  ModuleIdx
		item syntax.ItemID
	}
	owner := make(map[itemKey]TypeDefID)
	tbl := d.decls.Types
	for id, def := range d.res.Defs.All() {
		switch def.Kind {
		case DefType:
			it := d.mods[def.Module].ItemNodes.Get(def.Item)
			td := d.decls.Defs.Push(TypeDef{
				Name: def.Name, Span: def.Span, Module: def.Module, Item: def.Item, Form: it.Form, Alias: tbl.Error(),
			})
			tbl.nameDef(td, d.in.String(def.Name))
			d.decls.typeOf[id] = td
			owner[itemKey{def.Module, def.Item}] = td
		case DefCtor:
			td := owner[itemKey{def.Module, def.Item}]
			parent := d.decls.Defs.Ptr(td)
			c := d.decls.Ctors.Push(Ctor{Name: def.Name, Span: def.Span, Owner: td, Tag: uint32(len(parent.Ctors))})
			parent.Ctors = append(parent.Ctors, c)
			d.decls.ctorOf[id] = c
		case DefFn, DefExternFn:
		default:
			panic("unreachable")
		}
	}
}

// aliasFrame is a step of the walk over the aliases: the alias td, the aliases
// that its target mentions, and how many of them have been looked at.
type aliasFrame struct {
	td   TypeDefID
	deps []TypeDefID
	next int
}

// aliases is the second sweep: it expands every alias, depth first along what
// each one mentions, with an explicit stack of the aliases that are being
// expanded. An alias is expanded once every alias that it mentions is; meeting one
// that is still being expanded is a cycle (see cycle).
func (d *declarer) aliases() {
	const (
		unseen uint8 = iota
		open         // on the stack: being expanded
		done
	)
	n := d.decls.Defs.Len()
	state := make([]uint8, n)
	depth := make([]int, n) // the position of an open alias on the stack
	cyclic := make([]bool, n)
	frame := func(td TypeDefID) aliasFrame {
		def := d.decls.Defs.Get(td)
		return aliasFrame{td: td, deps: d.aliasRefs(def.Module, d.mods[def.Module].ItemNodes.Get(def.Item).Alias)}
	}
	var stack []aliasFrame
	for root := range TypeDefID(n) {
		if d.decls.Defs.Get(root).Form != syntax.TypeFormAlias || state[root] != unseen {
			continue
		}
		state[root], depth[root] = open, 0
		stack = append(stack[:0], frame(root))
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next < len(top.deps) {
				dep := top.deps[top.next]
				top.next++
				switch state[dep] {
				case unseen:
					state[dep], depth[dep] = open, len(stack)
					stack = append(stack, frame(dep))
				case open:
					d.cycle(stack[depth[dep]:], cyclic)
				}
				continue
			}
			// Everything that this alias mentions has its expansion (or the error
			// type, if it is in a cycle), so its own can be read.
			def := d.decls.Defs.Get(top.td)
			if !cyclic[top.td] {
				target := d.mods[def.Module].ItemNodes.Get(def.Item).Alias
				d.decls.Defs.Ptr(top.td).Alias = d.reader.typeOf(def.Module, target)
			}
			state[top.td] = done
			stack = stack[:len(stack)-1]
		}
	}
}

// aliasRefs returns the aliases that the type expression id of module mod
// mentions, in the order they are written, each time it mentions one. It goes
// down the structure of the type with a list of the parts still to look at, so
// the forms that nest (tuples, functions, applications) will add their parts to
// that list.
func (d *declarer) aliasRefs(mod ModuleIdx, id syntax.TypeExprID) []TypeDefID {
	var refs []TypeDefID
	todo := []syntax.TypeExprID{id}
	for len(todo) > 0 {
		te := d.mods[mod].Types.Get(todo[len(todo)-1])
		todo = todo[:len(todo)-1]
		switch te.Kind {
		case syntax.TypeUnit, syntax.TypeError:
			// Nothing inside.
		case syntax.TypeNamed:
			res, ok := d.res.LookupType(mod, te.Name)
			if !ok || res.Kind != ResDef {
				continue
			}
			if td := d.decls.typeOf[res.Def]; d.decls.Defs.Get(td).Form == syntax.TypeFormAlias {
				refs = append(refs, td)
			}
		default:
			panic("unreachable")
		}
	}
	return refs
}

// cycle reports the cycle of aliases that is the end of the stack: members are
// the aliases from the one that was met again to the one that met it, each
// standing for the next and the last for the first. They are marked in cyclic, so
// that none is expanded. The report is one E0801, on the member that comes first
// in the source (the lowest TypeDefID), with a secondary label on each of the
// others in the order the cycle goes through them; a cycle of one alias has no
// secondary label. If the sink has dropped errors the report is not built: a
// cycle of thousands of aliases would write thousands of labels for nobody.
// The labels are one per member, however long the cycle (the row of E0801 has no
// cap), so they are built in one slice that is the right size from the start:
// adding them one by one with WithSecondary copies the whole list each time, and
// the cost of one error grows with the square of the cycle.
func (d *declarer) cycle(members []aliasFrame, cyclic []bool) {
	first := 0
	for i, m := range members {
		cyclic[m.td] = true
		if m.td < members[first].td {
			first = i
		}
	}
	if d.sink.Truncated() {
		return
	}
	primary := d.decls.Defs.Get(members[first].td)
	diagnostic := diag.NewError(diag.E(801), "alias `"+d.in.String(primary.Name)+"` refers to itself",
		diag.Label{Span: primary.Span, Message: "this alias is part of a cycle"})
	if len(members) > 1 {
		through := make([]diag.Label, 0, len(members)-1)
		for k := 1; k < len(members); k++ {
			other := d.decls.Defs.Get(members[(first+k)%len(members)].td)
			through = append(through, diag.Label{Span: other.Span, Message: "through `" + d.in.String(other.Name) + "`, defined here"})
		}
		diagnostic.Secondary = through
	}
	d.sink.Report(diagnostic.WithNote("an alias stands for another type, so it cannot stand for itself"))
}

// members is the third sweep: the types of the fields of each constructor and of
// each record, read from the type expressions that declare them.
func (d *declarer) members() {
	for id, td := range d.decls.Defs.All() {
		m := d.mods[td.Module]
		it := m.ItemNodes.Get(td.Item)
		switch td.Form {
		case syntax.TypeFormSum:
			for i, v := range m.Variants(it.Variants) {
				ctor := d.decls.Ctors.Ptr(td.Ctors[i])
				fields := m.Fields(v.Fields)
				named := false
				for _, f := range fields {
					named = named || f.HasName
				}
				for _, f := range fields {
					ctor.Args = append(ctor.Args, d.reader.typeOf(td.Module, f.Type))
					if named {
						ctor.Fields = append(ctor.Fields, f.Name)
					}
				}
			}
		case syntax.TypeFormRecord:
			rec := d.decls.Defs.Ptr(id)
			for _, f := range m.Fields(it.Fields) {
				rec.Fields = append(rec.Fields, FieldDef{Name: f.Name, Span: f.Span, Type: d.reader.typeOf(td.Module, f.Type)})
			}
		case syntax.TypeFormAlias:
			// Expanded by the second sweep.
		default:
			panic("unreachable")
		}
	}
}

// typeReader turns a type as the program writes it into a type of the table. Both
// passes that read type expressions use it, Declare for the fields and aliases of
// the declarations and Check for the annotations of functions and `let`s, so
// that a name means the same in all of them and an unknown one is reported once,
// where it is written.
type typeReader struct {
	mods  []*syntax.Module
	res   *ResolvedProgram
	in    *base.Interner
	sink  *diag.Sink
	decls *Decls
}

// typeOf returns the type that the type expression id of module mod writes.
//
// A name is looked up among the types that the program declares first, and then
// among the builtins, so a declared type wins over `Bool` (see
// ResolvedProgram.LookupType); a declared type is what its name stands for (see
// Decls.Meaning). One that the parser could not read after its name has the error
// type, silently: the parser has said what is wrong. A name that is neither is
// E0406, reported here, and has the error type.
func (r *typeReader) typeOf(mod ModuleIdx, id syntax.TypeExprID) TypeID {
	tbl := r.decls.Types
	te := r.mods[mod].Types.Get(id)
	switch te.Kind {
	case syntax.TypeUnit:
		return tbl.Builtin(BuiltinUnit)
	case syntax.TypeNamed:
		if res, ok := r.res.LookupType(mod, te.Name); ok {
			if res.Kind != ResDef {
				return tbl.Error()
			}
			return r.decls.Meaning(r.decls.typeOf[res.Def])
		}
		switch r.in.String(te.Name) {
		case "Int":
			return tbl.Builtin(BuiltinInt)
		case "Float":
			return tbl.Builtin(BuiltinFloat)
		case "Bool":
			return tbl.Builtin(BuiltinBool)
		}
		// Not a type that is in scope: reported here, once, where it is written,
		// and the error type from then on, which says nothing more wherever it is
		// used.
		r.unknownTypeError(mod, te.Span, r.in.String(te.Name))
		return tbl.Error()
	case syntax.TypeError:
		return tbl.Error()
	default:
		panic("unreachable")
	}
}

// unknownTypeError reports E0406 for the type name that is written at, in module
// mod. The help and its edit are there when a type in scope, a builtin or one
// that the program declares, is spelled almost like it (`Bol` for `Bool`,
// `Shap` for `Shape`), as for an unknown name. There is no note: what is in
// scope depends on the program.
func (r *typeReader) unknownTypeError(mod ModuleIdx, at base.Span, name string) {
	d := diag.NewError(diag.E(406), "unknown type `"+name+"`", diag.Label{Span: at, Message: "not a type in this scope"})
	// Like the help of an unknown name, it costs a pass over the candidates,
	// which nobody reads once the sink is full.
	if !r.sink.Truncated() {
		candidates := append([]string{"Int", "Float", "Bool"}, r.res.visibleTypes(mod, r.in)...)
		if best, ok := suggest(name, candidates); ok {
			d = d.WithHelp("did you mean `" + best + "`?").WithFixIt(diag.Replace(at, best))
		}
	}
	r.sink.Report(d)
}
