package interp

import (
	"strconv"

	"github.com/aldezex/kilang/internal/core"
)

// Value is what an expression evaluates to. It is a sealed interface (spec 9.1):
// there is one struct for each form of value, and the unexported method value
// keeps any other package from defining a new form, because only this package
// can write it. (A type elsewhere can still embed Value and so satisfy it, but
// the machine never makes one.) A type switch with a case for each of the four
// forms below covers every form the machine makes, and it ends in
// `default: panic("unreachable")` for the day someone adds a fifth form and
// forgets one of the switches, and for a Value built by embedding.
type Value interface{ value() }

// IntV is an Int: a signed 64-bit integer.
type IntV struct{ V int64 }

// BoolV is a Bool.
type BoolV struct{ V bool }

// UnitV is the value of `()`, the only one of its type.
type UnitV struct{}

// FunV is a function used as a value: it names a function of the Program, by
// the id of its Fun, and carries nothing else.
type FunV struct{ Fun core.FunID }

func (IntV) value()  {}
func (BoolV) value() {}
func (UnitV) value() {}
func (FunV) value()  {}

// Show returns how a value is written: an Int in decimal ("42", "-7"), a Bool
// as "true" or "false", the unit value as "()" and any function as "<fn>". It
// does not name the function, because a Value does not know the name of its Fun.
func Show(v Value) string {
	switch v := v.(type) {
	case IntV:
		return strconv.FormatInt(v.V, 10)
	case BoolV:
		return strconv.FormatBool(v.V)
	case UnitV:
		return "()"
	case FunV:
		return "<fn>"
	default:
		panic("unreachable")
	}
}
