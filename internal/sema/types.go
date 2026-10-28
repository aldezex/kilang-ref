package sema

import (
	"encoding/binary"
	"slices"
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
)

// TypeID names a type in a TypeTable. The table builds each type once, so two
// types of the same shape (`Int`, or `(Int) -> Int`) have the same id, and the
// compiler tells whether two types are the same by comparing two numbers, once
// the variables in them are resolved: a function whose parameter is a variable
// bound to `Int` is another node than the one whose parameter is `Int` (see
// Fun), and Unify is what sees through that.
type TypeID uint32

// RowID names an effect row in a TypeTable: the effects that a function may
// perform. In Part I every function has the empty row, which EmptyRow returns;
// rows with labels arrive in chapter 20.
type RowID uint32

// TypeVarID names an inference variable in a TypeTable: a type that the
// compiler does not know yet and will find out by unification. A variable has
// two ids. This one is its place in the table of variables, where the table
// records what it is bound to. Its TypeID is the type that stands for it
// inside other types, so that `(a) -> a` can hold it.
type TypeVarID uint32

// Builtin is one of the types that the language has built in.
type Builtin uint8

// The builtin types.
const (
	// BuiltinInt is `Int`, the signed 64-bit integer.
	BuiltinInt Builtin = iota
	// BuiltinBool is `Bool`.
	BuiltinBool
	// BuiltinUnit is `()`, the type that has a single value.
	BuiltinUnit
)

// numBuiltins is the number of builtin types: the size of the table that maps
// each Builtin to its TypeID. BuiltinUnit is the last constant, so a builtin
// added after it has to move this line too.
const numBuiltins = int(BuiltinUnit) + 1

// TypeKind says what a Type is, and so which of its fields mean something.
type TypeKind uint8

// The kinds of type.
const (
	// TypeBuiltin is `Int`, `Bool` or `()`: Type.Builtin says which.
	TypeBuiltin TypeKind = iota
	// TypeFun is a function type: Type.Params, Type.Ret and Type.Effects.
	TypeFun
	// TypeVariable is an inference variable: Type.Var is the variable. The
	// kind is not called TypeVar because that is the name of the table's
	// record of a variable, below.
	TypeVariable
	// TypeErr is the type of something that has no type because of an error
	// that was already reported. It exists so that the checker can go on
	// without reporting the same mistake again at every place that uses the
	// thing.
	TypeErr
)

// Type is a node of the type table. Only the fields of its Kind mean
// something, as TypeKind says. Params is shared with the table, so the caller
// must not change it.
type Type struct {
	Kind    TypeKind
	Builtin Builtin
	Params  []TypeID
	Ret     TypeID
	Effects RowID
	Var     TypeVarID
}

// Row is an effect row: the effects of a function. In Part I the only row is
// the empty closed one (no labels and no tail), so nothing reads these fields
// yet.
type Row struct {
	Labels  []TypeID
	Tail    TypeVarID
	HasTail bool
}

// VarKind says what a variable stands for. Every variable carries its kind
// (spec 7.2) so that a row variable can never be unified with a type.
type VarKind uint8

// The kinds of variable.
const (
	// VarKindType is a variable for a type.
	VarKindType VarKind = iota
	// VarKindRow is a variable for an effect row. None exists until chapter 20.
	VarKindRow
)

// Origin says where a variable was born and why, for the notes that explain a
// type error: Note is a short phrase such as "parameter `x`", "let `y`" or
// "return type of `f`".
type Origin struct {
	Span base.Span
	Note string
}

// TypeVar is the table's record of one variable: the node of a union-find
// forest, in the form spec 7.2 gives it. Bound and IsBound are the link to the
// variable's parent, here the type it was bound to. A variable that is not
// bound is the end of its chain.
//
// Rank and Level are stored and nothing reads them yet: the interface of the
// table is the final one from this chapter, and the let-polymorphism of
// chapter 13 is what uses them.
type TypeVar struct {
	Bound   TypeID
	IsBound bool
	Rank    uint8
	Level   uint32
	Kind    VarKind
	Origin  Origin
}

// funKey is what makes two function types the same type: the same parameters
// in the same order, the same result and the same effects. A Go slice cannot be
// a map key because slices cannot be compared, so the parameters go in as a
// string that holds their ids as bytes, four bytes per id: comparable, and
// with no two lists of ids written the same.
type funKey struct {
	ret     TypeID
	effects RowID
	params  string
}

// encodeIDs writes ids as four bytes each, so that two different lists never
// give the same string (which they would if the numbers were written in
// decimal and put next to each other: [1, 12] and [11, 2] would both be
// "112").
func encodeIDs(ids []TypeID) string {
	var b strings.Builder
	b.Grow(4 * len(ids))
	var word [4]byte
	for _, id := range ids {
		binary.LittleEndian.PutUint32(word[:], uint32(id))
		b.Write(word[:])
	}
	return b.String()
}

// TypeTable holds the types of a program. Types, variables and rows live in
// tables indexed by their ids, and a type refers to others by id, never by
// pointer.
type TypeTable struct {
	types base.IdVec[TypeID, Type]
	vars  base.IdVec[TypeVarID, TypeVar]
	rows  base.IdVec[RowID, Row]
	// funs finds the function type with a given key, so that Fun builds each
	// one only once (hash-consing).
	funs map[funKey]TypeID

	builtins [numBuiltins]TypeID
	errType  TypeID
	emptyRow RowID
}

// NewTypeTable returns a table that holds the builtin types, the error type
// and the empty row.
func NewTypeTable() *TypeTable {
	t := &TypeTable{funs: make(map[funKey]TypeID)}
	for b := BuiltinInt; int(b) < numBuiltins; b++ {
		t.builtins[b] = t.types.Push(Type{Kind: TypeBuiltin, Builtin: b})
	}
	t.errType = t.types.Push(Type{Kind: TypeErr})
	t.emptyRow = t.rows.Push(Row{})
	return t
}

// Builtin returns the type b.
func (t *TypeTable) Builtin(b Builtin) TypeID {
	return t.builtins[b]
}

// Error returns the error type.
func (t *TypeTable) Error() TypeID {
	return t.errType
}

// EmptyRow returns the effect row with no effects.
func (t *TypeTable) EmptyRow() RowID {
	return t.emptyRow
}

// Fun returns the function type with these parameters, result and effects.
// The table builds a given function type once, so equal arguments give the
// same id. The table keeps its own copy of params: the caller may change its
// slice afterwards.
//
// The ids are compared as they come. A parameter that is a variable bound to
// `Int` is a different id from `Int`, so `(a) -> Int` and `(Int) -> Int` are
// two nodes until the caller resolves the parameter first.
func (t *TypeTable) Fun(params []TypeID, ret TypeID, effects RowID) TypeID {
	key := funKey{ret: ret, effects: effects, params: encodeIDs(params)}
	if id, ok := t.funs[key]; ok {
		return id
	}
	var own []TypeID // stays nil for a function with no parameters
	if len(params) > 0 {
		own = slices.Clone(params)
	}
	id := t.types.Push(Type{Kind: TypeFun, Params: own, Ret: ret, Effects: effects})
	t.funs[key] = id
	return id
}

// FreshVar returns a new variable, as a type: one that nothing has bound yet,
// of kind VarKindType, with rank 0 and level 0. Every call makes a different
// variable. origin says where it was born and why.
func (t *TypeTable) FreshVar(origin Origin) TypeID {
	v := t.vars.Push(TypeVar{Kind: VarKindType, Origin: origin})
	return t.types.Push(Type{Kind: TypeVariable, Var: v})
}

// Node returns the node of the type id, as it is stored: a variable that has
// been bound is still a TypeVariable here, and Resolve is what follows its
// binding.
func (t *TypeTable) Node(id TypeID) Type {
	return t.types.Get(id)
}

// Resolve follows the bindings of id: if id is a variable that has been bound,
// it returns the type that the chain of bindings ends at, and otherwise id
// itself. What it returns is never a bound variable. It looks at the head of
// the type only: the parameters of a function type are not resolved.
//
// On the way it rebinds every variable of the chain straight to the type it
// ended at (path compression), so that the next Resolve of any of them takes
// one step instead of the whole chain.
func (t *TypeTable) Resolve(id TypeID) TypeID {
	// First pass: find the end of the chain.
	end := id
	for {
		n := t.types.Get(end)
		if n.Kind != TypeVariable {
			break
		}
		v := t.vars.Get(n.Var)
		if !v.IsBound {
			break
		}
		end = v.Bound
	}
	// Second pass: every variable that was on the chain now points at the end.
	for id != end {
		v := t.vars.Ptr(t.types.Get(id).Var)
		next := v.Bound
		v.Bound = end
		id = next
	}
	return end
}

// Bind makes the variable v stand for the type to.
//
// Two things are the caller's job, and Bind does not check them: that v is not
// bound yet, and that to does not contain v (the occurs check, Occurs). A
// variable that stood for a type containing itself would be an infinite type,
// which Resolve and Show cannot handle.
func (t *TypeTable) Bind(v TypeVarID, to TypeID) {
	p := t.vars.Ptr(v)
	p.Bound = to
	p.IsBound = true
}

// Occurs reports whether the variable v appears inside the type in, looking
// through the variables that have been bound. It is the occurs check of
// unification: binding v to a type that contains v would make an infinite
// type. v is meant to be unbound.
//
// It keeps its own stack of the types still to look at instead of calling
// itself, because how deep a type goes is up to the program that is being
// compiled. And it looks into each function type once, however many places
// hold it: the table builds equal types once and a bound variable stands for
// the same type wherever it appears, so a type can hold the same type twice at
// every level, and a walk that went down every path would take 2^n steps for
// n levels while the table holds only n nodes. A program of a few lines
// reaches that (`p1(p0, p0)`, then `p2(p1, p1)`, and so on).
//
// That makes one call linear in the number of function types below in, and
// not in the number of paths to them. It does not make the work over a whole
// program linear: the unifier asks once for each binding, and in a chain like
// the one above each new binding sits over all the earlier ones, so each call
// walks a longer DAG than the one before and the total grows with the square
// of the length of the chain. A chain of a few thousand functions already takes
// seconds. The type levels of chapter 13 remove that.
func (t *TypeTable) Occurs(v TypeVarID, in TypeID) bool {
	todo := []TypeID{in}
	// seen holds the function types already looked into. Only they have
	// anything inside, so only they can be reached by many paths; the other
	// kinds are answered on the spot.
	var seen map[TypeID]struct{}
	for len(todo) > 0 {
		id := t.Resolve(todo[len(todo)-1])
		todo = todo[:len(todo)-1]
		n := t.types.Get(id)
		switch n.Kind {
		case TypeBuiltin, TypeErr:
			// Nothing inside.
		case TypeVariable:
			// Resolve never returns a bound variable, so this one is unbound.
			if n.Var == v {
				return true
			}
		case TypeFun:
			if _, done := seen[id]; done {
				continue
			}
			if seen == nil {
				seen = make(map[TypeID]struct{})
			}
			seen[id] = struct{}{}
			todo = append(todo, n.Params...)
			todo = append(todo, n.Ret)
		default:
			panic("unreachable")
		}
	}
	return false
}

// Show writes the type id the way a diagnostic shows it: `Int`, `Bool`, `()`,
// `(Int, Bool) -> Int`, `() -> ()`; the error type is `{error}`. A variable
// that has been bound shows what it is bound to. One that has not shows as a
// name made up on the spot: `a` for the first unbound variable that the text
// mentions, then `b`… up to `z`, and then `a1`, `b1`… The same variable has
// the same name each time it appears in one call, and the next call starts
// again at `a`.
//
// The text is cut: it is at most MaxTypeText bytes long, plus the `…` that
// ends it when the type needs more. The cut falls after the last piece of text
// that fits (a name, a comma, a bracket), so a cut type is the start of the
// full text followed by `…`, and a type whose text is MaxTypeText bytes long
// or less is written whole. It is cut because the table builds equal types
// once: a type can hold the same type twice at every level, so a few lines of a
// program make a type of n levels whose full text has 2^n pieces (`p1(p0, p0)`,
// then `p2(p1, p1)`, and so on), and a diagnostic that wrote all of it would
// not end.
//
// Show does not write effect rows: in Part I every function has the empty
// one. Like Occurs it keeps its own stack of what is left to write, because
// how deep a type goes is up to the program that is being compiled.
func (t *TypeTable) Show(id TypeID) string {
	// piece is a part of the text that is left to write: the type id, or, when
	// text is not empty, that text as it is.
	type piece struct {
		id   TypeID
		text string
	}
	var out strings.Builder
	names := make(map[TypeVarID]int) // unbound variable -> its number, in order of appearance
	todo := []piece{{id: id}}
	for len(todo) > 0 {
		p := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		// next is what this piece writes now. A variable is numbered only once
		// its name is written, so that a name the cut leaves out is not used up.
		next := p.text
		var newVar TypeVarID
		isNew := false
		if next == "" {
			n := t.types.Get(t.Resolve(p.id))
			switch n.Kind {
			case TypeBuiltin:
				next = builtinName(n.Builtin)
			case TypeErr:
				next = "{error}"
			case TypeVariable:
				num, ok := names[n.Var]
				if !ok {
					num, newVar, isNew = len(names), n.Var, true
				}
				next = varName(num)
			case TypeFun:
				// "(" now; the rest goes on the stack, last piece first, so that it
				// comes off in the order "p0, p1, p2) -> ret".
				next = "("
				todo = append(todo, piece{id: n.Ret}, piece{text: " -> "}, piece{text: ")"})
				for i := len(n.Params) - 1; i >= 0; i-- {
					todo = append(todo, piece{id: n.Params[i]})
					if i > 0 {
						todo = append(todo, piece{text: ", "})
					}
				}
			default:
				panic("unreachable")
			}
		}
		if out.Len()+len(next) > MaxTypeText {
			out.WriteString(cutMark)
			break
		}
		out.WriteString(next)
		if isNew {
			names[newVar] = len(names)
		}
	}
	return out.String()
}

// MaxTypeText is the most bytes of a type that Show writes before it cuts the
// text and ends it with `…` (see Show).
const MaxTypeText = 256

// cutMark is what ends a text that Show cut.
const cutMark = "…"

// builtinName is how Show writes the builtin type b.
func builtinName(b Builtin) string {
	switch b {
	case BuiltinInt:
		return "Int"
	case BuiltinBool:
		return "Bool"
	case BuiltinUnit:
		return "()"
	default:
		panic("unreachable")
	}
}

// varName is the name that Show gives to the unbound variable that is number i
// (from 0) in order of appearance: a to z, then a1 to z1, then a2…
func varName(i int) string {
	name := string(rune('a' + i%26))
	if i >= 26 {
		name += strconv.Itoa(i / 26)
	}
	return name
}
