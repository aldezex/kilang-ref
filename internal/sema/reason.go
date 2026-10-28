package sema

import "github.com/aldezex/kilang/internal/base"

// ReasonKind says why the checker expects the type it expects. When unification
// fails, the reason becomes the secondary label of the error, which points at
// the place that explains the expectation.
type ReasonKind uint8

// The kinds of reason.
const (
	// ReasonAnnotation is an expected type that the program wrote as an
	// annotation. Because is the annotation.
	ReasonAnnotation ReasonKind = iota
	// ReasonLetAnnotation is the annotation of a `let`: `let x: Bool = 1`.
	// Because is the annotation. Its label is the same as ReasonAnnotation's.
	ReasonLetAnnotation
	// ReasonReturnType is a value returned from a function that is not the
	// tail of a block, which is what a `return` would be. Because is the return
	// type, or the function's name if it has none. It has the same label as
	// ReasonBlockTail but not its help.
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
	// Detail is the text of the operator (`&&`, `-`). Only ReasonOperand reads
	// it.
	Detail string
	// CanReturn says whether giving the function a return type would mend a
	// mistake at a tail: the help that Unify adds to it offers that only when
	// it is true. Only ReasonBlockTail reads it. The checker sets it for the
	// tail of a body and clears it where returning the value would not mend
	// anything: for the `main` of the program, whose type E0404 holds to
	// `() -> ()`, and for the `then` branch of an `if` with no `else`, because
	// what that branch ends in is not what the function returns.
	CanReturn bool
}
