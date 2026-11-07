package sema

import "github.com/aldezex/kilang/internal/base"

// ReasonKind says why the checker expects the type it expects. When unification
// fails, the reason becomes the secondary label of the error, which points at
// the place that explains the expectation.
type ReasonKind uint8

// The kinds of reason.
const (
	// ReasonAnnotation is an expected type that the program wrote as an
	// annotation. Because is the annotation. Nothing makes one in Part I, where
	// the only such annotation is that of a `let` (ReasonLetAnnotation), and no
	// chapter has fixed which will: the list of kinds is the final one.
	ReasonAnnotation ReasonKind = iota
	// ReasonLetAnnotation is the annotation of a `let`: `let x: Bool = 1`.
	// Because is the annotation. Its label is the same as ReasonAnnotation's.
	ReasonLetAnnotation
	// ReasonReturnType is a value returned from a function that is not the
	// tail of a block, which is what a `return` would be. Because is the return
	// type, or the function's name if it has none. It has the same label as
	// ReasonBlockTail but not its help. Nothing makes one until `return` arrives,
	// in chapter 23.
	ReasonReturnType
	// ReasonBlockTail is the tail of a function body, checked against the
	// return type: `fn f() { 1 }` expects `()` there. Because is the return
	// type, or the function's name if it has none. It is the only kind that
	// makes Unify add a help line (see Unify), and CanReturn is what that help
	// reads.
	ReasonBlockTail
	// ReasonArgument is an argument of a call, checked against the type of the
	// parameter. Because is the parameter.
	ReasonArgument
	// ReasonIfBranches is the `else` branch of an `if`, checked against the
	// type of the `then` branch. Because is the tail of the `then` branch (the
	// block, if it has no tail). It is also the `then` branch of an `if` with no
	// `else` that nothing expects a type of, checked against `()`, which is the
	// type of the branch that is missing: Because is then the `if` keyword.
	ReasonIfBranches
	// ReasonIfCondition is the condition of an `if`, which must be Bool. It has
	// no secondary label, so Because is not read.
	ReasonIfCondition
	// ReasonOperand is an operand of an operator. Because is the operator,
	// Detail its text and Index how many operands it has (see Reason).
	ReasonOperand
)

// The reasons of Part II come after ReasonOperand, in the order the sessions add
// them.
const (
	// ReasonCtorArg is an argument of a constructor, checked against the type of
	// the field. Because is the field, as written in the declaration of the
	// variant. It is ReasonArgument for a constructor, which has no parameter to
	// point at but a field.
	ReasonCtorArg ReasonKind = ReasonOperand + 1
	// ReasonField is the value of a field in a record literal, checked against
	// the type of the field. Because is the field, as the declaration of the
	// record writes it. It is what ReasonCtorArg is for a variant. The base of an
	// update (`Point { x: 3.0, ..p }`) has this kind too, with no Because: nothing
	// in the program explains why the base is a `Point`, and Unify takes the zero
	// Span for no place at all.
	ReasonField ReasonKind = ReasonCtorArg + 1
)

// The reasons of chapter 12 come after ReasonField.
const (
	// ReasonScrutinee is a pattern, checked against the type of the value that a
	// `match` or a `let` takes apart. Because is that value (the scrutinee, or the
	// initializer of the `let`) and Whole its type, which the label names; a
	// pattern inside a tuple has the type of one element as its expected type, so
	// Whole is what tells the reader the type of the value as a whole. With no
	// Because there is no label, and the zero Whole (the type with id 0) that comes
	// with it is written with the other two types and not used: Unify reads it for
	// every ReasonScrutinee, and the label is what drops it (the variable that two
	// alternatives of an or-pattern bind with different types is the one case).
	// A `let` with an annotation uses ReasonLetAnnotation instead: the annotation
	// is what explains the expectation there.
	ReasonScrutinee ReasonKind = ReasonField + 1 + iota
	// ReasonMatchArms is the body of an arm of a `match` that nothing expects a
	// type of, checked against the type of the first arm. Because is the value of
	// the first arm (its tail, if it is a block). When something does expect a
	// type, each arm is checked against it with the reason of that place, as a
	// block hands it to its tail.
	ReasonMatchArms
	// ReasonPatternArg is a pattern inside a constructor or a record pattern,
	// checked against the type of the field. Because is the field, as its
	// declaration writes it. It is ReasonCtorArg for patterns, and the tuples and
	// or-patterns inside the pattern keep it: the field explains their parts too.
	ReasonPatternArg
)

// Reason is why a type is expected, given to Unify so that the error it may
// report can explain itself.
type Reason struct {
	// Kind says what sort of place the expectation comes from.
	Kind ReasonKind
	// Because is the place in the source that explains the expectation: the
	// secondary label of the error goes there. ReasonIfCondition does not read
	// it. If it is the zero Span (there is no such place) or lies in another
	// file than the found type (as the parameter of a prelude function does),
	// the error has no secondary label, because one in another file could not be
	// drawn under the primary one.
	Because base.Span
	// Index is the number of operands of the operator, 1 or 2. Only
	// ReasonOperand reads it, and then only when a mismatch is reported: any
	// other value makes Unify panic at that point, because it is a bug of the
	// checker and not of the program.
	Index int
	// Detail is the text of the operator (`&&`, `-`) for ReasonOperand. For
	// ReasonIfCondition it is guardDetail when the condition is the guard of an arm
	// of a `match`, which has a note of its own. No other kind reads it.
	Detail string
	// Whole is the type of the whole value that a pattern is matched against,
	// for ReasonScrutinee, which names it. No other kind reads it.
	Whole TypeID
	// CanReturn says whether giving the function a return type would mend a
	// mistake at a tail: the help that Unify adds to it offers that only when
	// it is true. Only ReasonBlockTail reads it. The checker sets it for the
	// tail of a body and clears it where returning the value would not mend
	// anything: for the `main` of the program, whose type E0404 holds to
	// `() -> ()`, and for the `then` branch of an `if` with no `else`, because
	// what that branch ends in is not what the function returns.
	CanReturn bool
	// InArm says that the value is the body of an arm of a `match`, or something
	// inside it that is not in a block, where no `let` can be written: the help
	// that Unify adds to a value that is unused then says to write the arm as a
	// block (`{ let name = … }`) and not to bind it with a `let`. A block clears
	// it, since a `let` goes there. Only ReasonBlockTail reads it.
	InArm bool
	// IntAsFloat is the Float literal that the Int literal being checked is the
	// same number as (`1` and `1.0`), when the place expects a Float: the mismatch
	// that Unify reports then has the edit that writes it. It is empty for what is
	// not a literal, and Unify reads it only when the expected type is Float.
	IntAsFloat string
}

// guardDetail is the Detail of a ReasonIfCondition whose condition is the guard of
// an arm: the same rule as an `if` condition (it must be a Bool), with the note
// worded for a guard.
const guardDetail = "guard"
