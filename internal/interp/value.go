package interp

import (
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/kirt/fmtnum"
)

// Value is what an expression evaluates to. It is a sealed interface (spec 9.1):
// there is one struct for each form of value, and the unexported method value
// keeps any other package from defining a new form, because only this package
// can write it. (A type elsewhere can still embed Value and so satisfy it, but
// the machine never makes one.) A type switch with a case for each of the six
// forms below covers every form the machine makes, and it ends in
// `default: panic("unreachable")` for the day someone adds a seventh form and
// forgets one of the switches, and for a Value built by embedding.
type Value interface{ value() }

// IntV is an Int: a signed 64-bit integer.
type IntV struct{ V int64 }

// FloatV is a Float: an IEEE 754 binary64 number.
type FloatV struct{ V float64 }

// BoolV is a Bool.
type BoolV struct{ V bool }

// UnitV is the value of `()`, the only one of its type.
type UnitV struct{}

// FunV is a function used as a value: it names a function of the Program, by
// the id of its Fun, and carries nothing else.
type FunV struct{ Fun core.FunID }

// ConV is a value that a constructor built: which constructor it was, and the
// values of its fields in order. It is never changed once the machine has built
// it: the machine hands the same Fields to every place that holds the value, so
// a Value that is a ConV must not be written through (an update of a record
// builds a new one). A ConV is not comparable with `==`, because it holds a
// slice.
type ConV struct {
	Ctor   core.CtorID
	Fields []Value
}

func (IntV) value()   {}
func (FloatV) value() {}
func (BoolV) value()  {}
func (UnitV) value()  {}
func (FunV) value()   {}
func (ConV) value()   {}

// Show returns how a value is written: an Int in decimal ("42", "-7"), a Float
// as kirt/fmtnum writes it ("1.0", "0.1", "NaN"), a Bool as "true" or "false",
// the unit value as "()", any function as "<fn>", and a value that a constructor
// built as the constructor's name followed by its fields in parentheses, each
// written the same way ("Circle(1.0)", "Some(Circle(1.0))"), or the name alone
// when it has no fields ("None"). A tuple has no name, so it is its fields in
// parentheses ("(1, true)"), and a record is its name and its fields between
// braces, each with its name ("Point { x: 1.0, y: 2.0 }"): a variant that names
// its fields is not, and is written as any other. It does not name a function, because a Value
// does not know the name of its Fun. The name of a constructor is not in the value
// either, only its id, so Show needs the Program that has the constructor and the
// interner that spells its name; p and in may be nil for a value that holds no
// constructor.
//
// A value can nest as deep as the program that made it (a list of 100 000
// elements is 100 000 constructors, each in the field of the one before), so Show
// keeps a stack of what is left to write and never calls itself.
func Show(v Value, p *core.Program, in *base.Interner) string {
	// piece is a part of the text that is left to write: the value v, or, when
	// text is not empty, that text as it is.
	type piece struct {
		v    Value
		text string
	}
	var out strings.Builder
	todo := []piece{{v: v}}
	for len(todo) > 0 {
		next := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if next.text != "" {
			out.WriteString(next.text)
			continue
		}
		switch v := next.v.(type) {
		case IntV:
			out.WriteString(strconv.FormatInt(v.V, 10))
		case FloatV:
			out.WriteString(fmtnum.Float(v.V))
		case BoolV:
			out.WriteString(strconv.FormatBool(v.V))
		case UnitV:
			out.WriteString("()")
		case FunV:
			out.WriteString("<fn>")
		case ConV:
			ctor := p.Ctors.Get(v.Ctor)
			if !ctor.Tuple {
				out.WriteString(in.String(ctor.Name))
			}
			if len(v.Fields) > 0 {
				// The opening now; the rest goes on the stack, last piece first, so
				// that it comes off in the order "f0, f1)" or "x: f0, y: f1 }".
				open, close := "(", ")"
				if ctor.Record {
					open, close = " { ", " }"
				}
				out.WriteString(open)
				todo = append(todo, piece{text: close})
				for i := len(v.Fields) - 1; i >= 0; i-- {
					todo = append(todo, piece{v: v.Fields[i]})
					if ctor.Record {
						todo = append(todo, piece{text: in.String(ctor.Fields[i]) + ": "})
					}
					if i > 0 {
						todo = append(todo, piece{text: ", "})
					}
				}
			}
		default:
			panic("unreachable")
		}
	}
	return out.String()
}
