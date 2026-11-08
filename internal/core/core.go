// Package core defines Core, the intermediate representation that sits between
// the typed syntax tree and everything that runs a program (spec 6.1). Core has
// no types: nothing in it records the type of a variable or of an expression.
// Its expressions are strict.
//
// This file holds the data: a Program owns every function, expression, variable
// and constructor of a build. The Builder in builder.go is how the elaboration,
// and the tests, make them. Part I needs only the part of Core that the language
// of Part I elaborates to: literals, variables, calls, primitive operations,
// `let` and a `match` on Bool; Part II adds the constructors, which build the
// values of the types that a program declares, and the patterns, which take
// them apart.
//
// Like the syntax tree, a Program stores its nodes in IdVecs and refers to
// them by id, never by pointer. The children of a node that form a list are a
// base.ListRange over one of the Program's pools (read them with Args, ArmsOf
// and PatList).
package core

import (
	"strconv"

	"github.com/aldezex/kilang/internal/base"
)

// ExprID names an expression in Program.Exprs.
type ExprID uint32

// VarID names a variable in Program.Vars.
type VarID uint32

// FunID names a function in Program.Funs.
type FunID uint32

// CtorID names a constructor in Program.Ctors.
type CtorID uint32

// Rep says how the value of a variable is represented at run time.
type Rep uint8

// The representations. Part I has the one, the generic one; chapter 56 adds
// RepI64 and RepF64, so that code that works on a single type can keep a
// number unboxed.
const (
	RepVal Rep = iota
)

// Var is a variable: a parameter of a function, the name a `let` binds or the
// name a pattern binds. Core is meant to bind each Var in one place only (spec
// 6.1); neither Builder nor Program checks it, and Lint does not check it in Part
// I (it is exercise 1 of chapter 9). The one exception is an or-pattern: each of
// its alternatives binds the same Vars, so that the guard and the body of the arm
// read them whichever alternative fitted.
type Var struct {
	Name base.Symbol
	Rep  Rep
	// Slot is the index of the variable in the frame of its function, the
	// block of memory that holds the parameters and the `let`s of one call.
	Slot uint32
	// Span is where the program binds the variable.
	Span base.Span
}

// Prim is a primitive operation: a function that the machine itself knows how
// to run, as opposed to one that the program defines.
type Prim uint8

// The primitives, with the order of their names (see Prim.String).
const (
	PrimAddInt Prim = iota
	PrimSubInt
	PrimMulInt
	PrimDivInt
	PrimRemInt
	PrimNegInt
	PrimEqInt
	PrimNeInt
	PrimLtInt
	PrimLeInt
	PrimGtInt
	PrimGeInt
	PrimEqBool
	PrimNeBool
	PrimNotBool
)

// The primitives of Part II come after PrimNotBool, in the order the sessions
// add them, so that no primitive of Part I changes its value. Each line of this
// block is written `X + 1 + iota`: an implicit repetition of a line without
// iota would give every name the same value.
const (
	PrimAddFloat Prim = PrimNotBool + 1 + iota
	PrimSubFloat
	PrimMulFloat
	PrimDivFloat
	PrimNegFloat
	PrimEqFloat
	PrimNeFloat
	PrimLtFloat
	PrimLeFloat
	PrimGtFloat
	PrimGeFloat
)

// primNames is the name of each primitive, indexed by Prim.
var primNames = [...]string{
	PrimAddInt: "add_int", PrimSubInt: "sub_int", PrimMulInt: "mul_int",
	PrimDivInt: "div_int", PrimRemInt: "rem_int", PrimNegInt: "neg_int",
	PrimEqInt: "eq_int", PrimNeInt: "ne_int", PrimLtInt: "lt_int",
	PrimLeInt: "le_int", PrimGtInt: "gt_int", PrimGeInt: "ge_int",
	PrimEqBool: "eq_bool", PrimNeBool: "ne_bool", PrimNotBool: "not_bool",
	PrimAddFloat: "add_float", PrimSubFloat: "sub_float", PrimMulFloat: "mul_float",
	PrimDivFloat: "div_float", PrimNegFloat: "neg_float", PrimEqFloat: "eq_float",
	PrimNeFloat: "ne_float", PrimLtFloat: "lt_float", PrimLeFloat: "le_float",
	PrimGtFloat: "gt_float", PrimGeFloat: "ge_float",
}

// String returns the name of the primitive: "add_int", "sub_int" and so on up
// to "ge_float". A value that is not a primitive prints as "Prim(n)".
func (p Prim) String() string {
	if int(p) < len(primNames) {
		return primNames[p]
	}
	return "Prim(" + strconv.Itoa(int(p)) + ")"
}

// Arity returns the number of arguments the primitive takes: 1 for PrimNegInt,
// PrimNotBool and PrimNegFloat, 2 for every other primitive.
func (p Prim) Arity() int {
	if p == PrimNegInt || p == PrimNotBool || p == PrimNegFloat {
		return 1
	}
	return 2
}

// ExprKind says what an Expr is, and so which of its fields mean something.
type ExprKind uint8

// The kinds of expression.
const (
	LitInt   ExprKind = iota // Int
	LitBool                  // Bool
	LitUnit                  // no field
	VarRef                   // Var, the variable that is read
	FunRef                   // Fun, the function that is named
	Call                     // Callee, Args
	PrimCall                 // Prim, Args
	Let                      // Var, the variable that is bound; Init; Body
	Match                    // Scrutinee, Arms

	// The kinds of Part II come after Match, in the order the sessions add them.

	LitFloat // Float
	Con      // Ctor, Args: `K(ē)`, the value of the constructor Ctor built from the values of Args
	Proj     // Target, Index: `e.i`, the field number Index of the value of Target
)

// PatID names a pattern in Program.Pats.
type PatID uint32

// PatKind says what a Pat is, and so which of its fields mean something.
type PatKind uint8

// The kinds of pattern. The first three have no field of their own: the value
// true, the value false, or any value. The kinds of Part II come after
// PatWildcard, so that the first three keep their values; each line is written
// `X + 1 + iota` for the same reason as in the block of the primitives. The
// patterns on strings, characters and unsigned integers join them in the chapters
// that add those types.
const (
	PatTrue PatKind = iota
	PatFalse
	PatWildcard
)

const (
	PatInt  PatKind = PatWildcard + 1 + iota // Int: an Int literal, `0` or `-1`
	PatBind                                  // Var: any value, which the pattern binds to Var
	PatCon                                   // Ctor, Args: a value that Ctor built, whose fields fit the patterns in Args
	PatOr                                    // Args: the first of the alternatives that fits
)

// Pat is a pattern. Its Kind says which of the other fields mean something (see
// the constants); the rest are zero. A pattern is a tree like an expression, and
// like one it is stored in Program.Pats and named by its id.
//
// A PatCon fits a value that the constructor Ctor built when each of its fields
// fits the pattern in the same place of Args, so Args has as many patterns as
// the constructor has fields (Lint checks it). A tuple is a PatCon of the tuple
// constructor of its arity, and a record a PatCon of the constructor of the
// record, with a PatWildcard for each field that the source leaves out. A PatOr
// fits a value that one of its alternatives fits, tried in order (the parser only
// makes one of two or more). Every alternative of a PatOr binds the same Vars: the
// elaboration gives a name one Var wherever the alternatives bind it, so the guard
// and the body read it without asking which alternative fitted; neither Lint nor
// the machine checks that they do.
//
// Args holds the sub-patterns of a PatCon and the alternatives of a PatOr: a
// range over Program.PatPool, read with Program.PatList.
type Pat struct {
	Kind PatKind
	Span base.Span

	Int  int64
	Var  VarID
	Ctor CtorID
	Args base.ListRange
}

// Arm is one arm of a Match: the pattern, an optional guard and the expression
// that is the value of the Match when the arm is the one that wins. The guard
// means something only when HasGuard is true. The arm of an `if` or of `&&` and
// `||` has PatTrue or PatFalse nodes of its own for its pattern.
type Arm struct {
	Pat      PatID
	Guard    ExprID
	HasGuard bool
	Body     ExprID
}

// Expr is an expression. Its Kind says which of the other fields mean
// something (see the constants); the rest are zero.
type Expr struct {
	Kind ExprKind
	Span base.Span

	Int   int64
	Float float64
	Bool  bool
	Var   VarID
	Fun   FunID
	Prim  Prim
	// Ctor is the constructor that a Con builds.
	Ctor CtorID
	// A Proj reads the field number Index (from 0) of the value of Target, which
	// is a value that a constructor built: a record, a tuple, or any value of a
	// declared type with that many fields. Core has no types, so nothing here says
	// that Target has such a field: Lint cannot check it, and the machine takes
	// the checker's word for it.
	Target ExprID
	Index  uint32

	// Callee is what a Call calls. Args holds the arguments of a Call, of a
	// PrimCall and of a Con: a range over Program.ExprPool, read with
	// Program.Args.
	Callee ExprID
	Args   base.ListRange

	// A Let binds Var to the value of Init in Body.
	Init, Body ExprID

	// A Match compares the value of Scrutinee with the pattern of each of its
	// Arms, from the first to the last: the first arm whose pattern fits the value
	// and whose guard, if it has one, is true wins, and its Body is the value of
	// the Match. An arm with an or-pattern and a guard is one row for each of its
	// alternatives, in order, so a false guard goes on with the next alternative
	// before it goes on with the next arm. Arms is a range over Program.ArmPool,
	// read with Program.ArmsOf.
	Scrutinee ExprID
	Arms      base.ListRange
}

// Fun is a function.
type Fun struct {
	Name base.Symbol
	// Params are the variables that the arguments are bound to, in order. It
	// is a slice and not a ListRange because its elements are not expressions
	// (spec 7.2); whoever builds a Fun does not change it afterwards.
	Params []VarID
	// Body is the expression to evaluate. It means something only when
	// HasBody is true: a function that the runtime provides has no body.
	Body    ExprID
	HasBody bool
	// ExternName is the name of the runtime function that stands for this one
	// when it has no body, such as "ki_print_int"; it is empty otherwise.
	ExternName string
	// FrameSize is the number of slots of the frame of one call.
	FrameSize uint32
	Span      base.Span
	// UserModule is true for a function of the user's file and false for one of
	// the prelude.
	UserModule bool
}

// Ctor is a constructor: how a value of a declared type is built. Arity is how
// many values it is built from, and a Con that gives another number is what Lint
// refuses. Tag tells the constructors of one type apart: its position among them,
// from 0. Tuple marks a constructor that builds a tuple, which has no name of its
// own (Name is base.NoSymbol) and exists once for each arity, added by the
// elaboration as the tuples that the program writes need it. Record marks the
// only constructor of a record, which has the name of the record and always has
// Fields, so that a value of it is written with the names of its fields, `Point {
// x: 1.0, y: 2.0 }`, and the value of a variant that names its fields is not
// (`Rect(2.0, 3.0)`). Fields holds the names of the fields when the declaration
// gives them, and is nil otherwise (a field with no name has base.NoSymbol in it
// when a constructor names only some).
type Ctor struct {
	Name   base.Symbol
	Tag    uint32
	Arity  uint32
	Tuple  bool
	Record bool
	Fields []base.Symbol
}

// Program is the Core of a whole build: the prelude and the user's file.
type Program struct {
	Funs base.IdVec[FunID, Fun]
	// Ctors lists the constructors of every declared type, in the order of the
	// declarations: CtorID(c) is the constructor that sema.CtorID(c) is.
	Ctors base.IdVec[CtorID, Ctor]
	// Exprs holds every expression. ExprPool and ArmPool are the pools that the
	// ListRanges of the expressions point into.
	Exprs    base.IdVec[ExprID, Expr]
	Vars     base.IdVec[VarID, Var]
	ExprPool []ExprID
	ArmPool  []Arm
	// Pats holds every pattern, and PatPool is the pool that the Args of the
	// patterns point into.
	Pats    base.IdVec[PatID, Pat]
	PatPool []PatID
	// Main is the function `main`. It means something only when HasMain is true.
	Main    FunID
	HasMain bool
}

// Args returns the expressions that r selects in ExprPool. The caller must not
// change the elements. See base.Slice for what the result is and when it panics.
func (p *Program) Args(r base.ListRange) []ExprID { return base.Slice(p.ExprPool, r) }

// ArmsOf returns the arms that r selects in ArmPool, like Args does for
// expressions.
func (p *Program) ArmsOf(r base.ListRange) []Arm { return base.Slice(p.ArmPool, r) }

// PatList returns the patterns that r selects in PatPool, like Args does for
// expressions.
func (p *Program) PatList(r base.ListRange) []PatID { return base.Slice(p.PatPool, r) }
