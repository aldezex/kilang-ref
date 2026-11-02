package interp

import (
	"fmt"
	"math"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
)

// Checked 64-bit arithmetic. In Go, an int64 operation that does not fit wraps
// around: the result is defined, and nothing tells you it happened. Ki makes
// every overflow a panic (spec 5.10), so each function below does the
// operation and then checks, with explicit arithmetic, whether the exact result
// fits. The second result of add, sub, mul and neg is true when it does not; the
// first result is then the wrapped value, which means nothing and is not to be
// used.

// addInt returns a + b and whether the sum overflowed. A sum overflows exactly
// when both operands have a sign that the result does not have: two positives
// that give a negative, or two negatives that give a non-negative. `x^r` has
// its sign bit set when x and r differ in sign, so the sum overflows iff
// (a^r)&(b^r) < 0.
func addInt(a, b int64) (int64, bool) {
	r := a + b
	return r, (a^r)&(b^r) < 0
}

// subInt returns a - b and whether the difference overflowed. a - b is
// a + (-b), so the overflow is the one of addInt with b's sign flipped: a and b
// differ in sign and the result's sign is not a's. Hence (a^b)&(a^r) < 0.
func subInt(a, b int64) (int64, bool) {
	r := a - b
	return r, (a^b)&(a^r) < 0
}

// mulInt returns a * b and whether the product overflowed. A product that wraps
// does not give a back when it is divided by b, so it overflowed iff r/b != a.
// That check has a blind spot, MinInt64 / -1, which in Go wraps to MinInt64
// instead of failing: the product MinInt64 * -1, which is exactly 2^63, would
// pass it, so it is checked first. The other order, -1 * MinInt64, the division
// does catch (r/b is 1, not -1); it is checked first as well, so that the two
// read the same. A zero operand cannot overflow, and it is answered before the
// division so that b is never 0 there.
func mulInt(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, false
	}
	if (a == -1 && b == math.MinInt64) || (b == -1 && a == math.MinInt64) {
		return math.MinInt64, true
	}
	r := a * b
	return r, r/b != a
}

// negInt returns -a and whether it overflowed. Only MinInt64 does: its negation,
// 2^63, is one more than MaxInt64.
func negInt(a int64) (int64, bool) {
	return -a, a == math.MinInt64
}

// divErr says why a division has no result.
type divErr uint8

// The outcomes of divInt and remInt.
const (
	divOK       divErr = iota // the division has a result
	divByZero                 // the divisor is 0
	divOverflow               // MinInt64 / -1, whose result is 2^63
)

// divInt returns a / b truncated toward zero. b == 0 is divByZero, and so the
// result is 0 and means nothing; MinInt64 / -1 is divOverflow. The zero check
// comes first because Go panics on an integer division by zero, and a Ki panic
// is never a Go panic.
func divInt(a, b int64) (int64, divErr) {
	if b == 0 {
		return 0, divByZero
	}
	if a == math.MinInt64 && b == -1 {
		return math.MinInt64, divOverflow
	}
	return a / b, divOK
}

// remInt returns the remainder of a / b, which has the sign of the dividend
// (Go's % does the same). b == 0 is divByZero. MinInt64 % -1 is 0, with no
// overflow: the quotient overflows, but the remainder is well defined. Go does
// not panic on it, so it needs no check of its own.
func remInt(a, b int64) (int64, divErr) {
	if b == 0 {
		return 0, divByZero
	}
	return a % b, divOK
}

// The messages of the panics that a primitive can cause.
const (
	msgOverflow  = "integer overflow"
	msgDivByZero = "division by zero"
)

// panicAt reports that the program panics at sp: it writes
// "panic: <msg> at <path>:<line>:<col>\n" to err, where the position is the
// start of sp, and returns Halt{Code: 101}. It is one Write, and its error is
// not checked: there is nowhere better than err to tell that err failed, and the
// exit code says that the program panicked either way. It does not stop the
// machine: the caller records the Halt.
func (m *Machine) panicAt(sp base.Span, msg string) Halt {
	pos := m.sm.LineCol(sp.File, sp.Lo)
	fmt.Fprintf(m.err, "panic: %s at %s:%d:%d\n", msg, m.sm.DisplayPath(sp.File), pos.Line, pos.Col)
	return Halt{Code: 101}
}

// applyPrim computes the primitive p on the operands a and b. A primitive of one
// argument (neg_int, not_bool) reads a only. The second result is "" when the
// primitive has a value, and otherwise the message of the panic, in which case
// the first is nil.
//
// Core has no types, but the type checker has accepted the program, so the
// operands have the forms that p needs. A Value of another form is a bug of an
// earlier pass, and it shows as the interface conversion panic of Go.
func applyPrim(p core.Prim, a, b Value) (Value, string) {
	switch p {
	case core.PrimAddInt:
		return intResult(addInt(intOf(a), intOf(b)))
	case core.PrimSubInt:
		return intResult(subInt(intOf(a), intOf(b)))
	case core.PrimMulInt:
		return intResult(mulInt(intOf(a), intOf(b)))
	case core.PrimNegInt:
		return intResult(negInt(intOf(a)))
	case core.PrimDivInt:
		return divResult(divInt(intOf(a), intOf(b)))
	case core.PrimRemInt:
		return divResult(remInt(intOf(a), intOf(b)))
	case core.PrimEqInt:
		return BoolV{V: intOf(a) == intOf(b)}, ""
	case core.PrimNeInt:
		return BoolV{V: intOf(a) != intOf(b)}, ""
	case core.PrimLtInt:
		return BoolV{V: intOf(a) < intOf(b)}, ""
	case core.PrimLeInt:
		return BoolV{V: intOf(a) <= intOf(b)}, ""
	case core.PrimGtInt:
		return BoolV{V: intOf(a) > intOf(b)}, ""
	case core.PrimGeInt:
		return BoolV{V: intOf(a) >= intOf(b)}, ""
	case core.PrimEqBool:
		return BoolV{V: boolOf(a) == boolOf(b)}, ""
	case core.PrimNeBool:
		return BoolV{V: boolOf(a) != boolOf(b)}, ""
	case core.PrimNotBool:
		return BoolV{V: !boolOf(a)}, ""
	default:
		panic("unreachable")
	}
}

// intOf returns the int64 inside v, which must be an IntV.
func intOf(v Value) int64 { return v.(IntV).V }

// boolOf returns the bool inside v, which must be a BoolV.
func boolOf(v Value) bool { return v.(BoolV).V }

// intResult turns the result of addInt, subInt, mulInt or negInt into what
// applyPrim returns.
func intResult(r int64, overflow bool) (Value, string) {
	if overflow {
		return nil, msgOverflow
	}
	return IntV{V: r}, ""
}

// divResult turns the result of divInt or remInt into what applyPrim returns.
func divResult(r int64, e divErr) (Value, string) {
	switch e {
	case divOK:
		return IntV{V: r}, ""
	case divByZero:
		return nil, msgDivByZero
	case divOverflow:
		return nil, msgOverflow
	default:
		panic("unreachable")
	}
}
