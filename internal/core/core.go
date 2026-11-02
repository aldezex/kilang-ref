// Package core defines Core, the intermediate representation that sits between
// the typed syntax tree and everything that runs a program (spec 6.1). Core has
// no types: nothing in it records the type of a variable or of an expression.
// Its expressions are strict.
//
// This file holds the data: a Program owns every function, expression and
// variable of a build. The Builder in builder.go is how the elaboration, and
// the tests, make them. Part I needs only the part of Core that the language
// of Part I elaborates to: literals, variables, calls, primitive operations,
// `let` and a `match` on Bool.
//
// Like the syntax tree, a Program stores its nodes in IdVecs and refers to
// them by id, never by pointer. The children of a node that form a list are a
// base.ListRange over one of the Program's two pools (read them with Args and
// ArmsOf).
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

// Rep says how the value of a variable is represented at run time.
type Rep uint8

// The representations. Part I has the one, the generic one; chapter 56 adds
// RepI64 and RepF64, so that code that works on a single type can keep a
// number unboxed.
const (
	RepVal Rep = iota
)

// Var is a variable: a parameter of a function or the name a `let` binds. Core
// is meant to bind each Var in one place only (spec 6.1); neither Builder nor
// Program checks it, and Lint does not check it in Part I (it is exercise 1 of
// chapter 9).
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

// primNames is the name of each primitive, indexed by Prim.
var primNames = [...]string{
	PrimAddInt: "add_int", PrimSubInt: "sub_int", PrimMulInt: "mul_int",
	PrimDivInt: "div_int", PrimRemInt: "rem_int", PrimNegInt: "neg_int",
	PrimEqInt: "eq_int", PrimNeInt: "ne_int", PrimLtInt: "lt_int",
	PrimLeInt: "le_int", PrimGtInt: "gt_int", PrimGeInt: "ge_int",
	PrimEqBool: "eq_bool", PrimNeBool: "ne_bool", PrimNotBool: "not_bool",
}

// String returns the name of the primitive: "add_int", "sub_int" and so on up
// to "not_bool". A value that is not a primitive prints as "Prim(n)".
func (p Prim) String() string {
	if int(p) < len(primNames) {
		return primNames[p]
	}
	return "Prim(" + strconv.Itoa(int(p)) + ")"
}

// Arity returns the number of arguments the primitive takes: 1 for PrimNegInt
// and PrimNotBool, 2 for every other primitive.
func (p Prim) Arity() int {
	if p == PrimNegInt || p == PrimNotBool {
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
)

// PatKind is the kind of pattern of one arm of a Match.
type PatKind uint8

// The patterns an arm can have: the value true, the value false, or any value.
const (
	PatTrue PatKind = iota
	PatFalse
	PatWildcard
)

// Arm is one arm of a Match: the pattern and the expression that is the value of
// the Match when the pattern fits.
type Arm struct {
	Pat  PatKind
	Body ExprID
}

// Expr is an expression. Its Kind says which of the other fields mean
// something (see the constants); the rest are zero.
type Expr struct {
	Kind ExprKind
	Span base.Span

	Int  int64
	Bool bool
	Var  VarID
	Fun  FunID
	Prim Prim

	// Callee is what a Call calls. Args holds the arguments of a Call and of a
	// PrimCall: a range over Program.ExprPool, read with Program.Args.
	Callee ExprID
	Args   base.ListRange

	// A Let binds Var to the value of Init in Body.
	Init, Body ExprID

	// A Match compares the value of Scrutinee with the pattern of each of its
	// Arms: the first arm whose pattern is the value wins, wherever it is, and
	// if there is none, the first wildcard arm does. Arms is a range over
	// Program.ArmPool, read with Program.ArmsOf.
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

// Program is the Core of a whole build: the prelude and the user's file.
type Program struct {
	Funs base.IdVec[FunID, Fun]
	// Exprs holds every expression. ExprPool and ArmPool are the pools that the
	// ListRanges of the expressions point into.
	Exprs    base.IdVec[ExprID, Expr]
	Vars     base.IdVec[VarID, Var]
	ExprPool []ExprID
	ArmPool  []Arm
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
