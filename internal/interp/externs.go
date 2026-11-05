package interp

import (
	"io"
	"math"
	"strconv"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/kirt/fmtnum"
)

// externFn is the code of one function of the runtime: it gets the machine, for
// its output and for the messages of its panics, the span of the call that runs
// it (the position that a panic of the function is reported at), and the
// arguments of the call. It returns the value of the call, or, when the program
// panics in it, a Halt that stops the machine: the message is already on err and
// the value is nil.
type externFn func(m *Machine, at base.Span, args []Value) (Value, *Halt)

// externs holds the functions that the runtime provides, by the name that the
// attribute `#[extern("…")]` gives them in the standard library. A Fun with no
// body and an ExternName is run by looking the name up here.
//
// Only the standard library declares externs (E0304 keeps a program from
// declaring its own), so a name that is not in the table is a mistake of the
// compiler or of the library and never of the program: see callExtern.
var externs = map[string]externFn{
	// print(x: Int) writes x in decimal and a line feed to the stdout of the
	// program. It is a single write, so a writer that flushes at each line break
	// flushes once. A failed write is not reported here: print has no way to say
	// so (its type is (Int) -> ()). A kirt/out Writer keeps its first error, and
	// out.Exit reports it and ends with 1 if the program would otherwise have
	// ended with 0.
	"ki_print_int": func(m *Machine, _ base.Span, args []Value) (Value, *Halt) {
		_, _ = io.WriteString(m.out, strconv.FormatInt(intOf(args[0]), 10)+"\n")
		return UnitV{}, nil
	},
	// printFloat(x: Float) is print for a Float, with the text of kirt/fmtnum
	// (spec 5.10). It goes when print is generic.
	"ki_print_float": func(m *Machine, _ base.Span, args []Value) (Value, *Halt) {
		_, _ = io.WriteString(m.out, fmtnum.Float(floatOf(args[0]))+"\n")
		return UnitV{}, nil
	},
	// toFloat(i: Int) -> Float is the nearest Float to i (ties to even): an Int
	// of more than 53 significant bits may not be exact.
	"ki_to_float": func(_ *Machine, _ base.Span, args []Value) (Value, *Halt) {
		return FloatV{V: float64(intOf(args[0]))}, nil
	},
	// truncate(f: Float) -> Int drops the fraction of f, toward zero. A NaN, an
	// infinity and a value that does not fit in an Int are a panic, at the span
	// of the call.
	"ki_truncate": func(m *Machine, at base.Span, args []Value) (Value, *Halt) {
		n, ok := truncateFloat(floatOf(args[0]))
		if !ok {
			h := m.panicAt(at, msgFloatRange)
			return nil, &h
		}
		return IntV{V: n}, nil
	},
	// sqrt(f: Float) -> Float is the square root, which IEEE 754 rounds exactly;
	// the root of a negative number is NaN.
	"ki_sqrt": func(_ *Machine, _ base.Span, args []Value) (Value, *Halt) {
		return FloatV{V: math.Sqrt(floatOf(args[0]))}, nil
	},
}

// truncateFloat returns f with its fraction dropped, as an Int, and false if
// there is no such Int: f is a NaN, an infinity or out of the range of an int64.
// The range is [-2^63, 2^63), and both limits are exact Floats. The upper one is
// not float64(math.MaxInt64): that rounds up to 2^63, so a test `f > MaxInt64`
// would take 2^63 in, and converting it to an int64 is not defined in Go.
func truncateFloat(f float64) (int64, bool) {
	if math.IsNaN(f) || f >= 1<<63 || f < -(1<<63) {
		return 0, false
	}
	return int64(f), true
}

// callExtern runs the function of the runtime that fun, which has no body,
// stands for, with the arguments args, and returns its value, or the Halt of the
// panic it caused (see externFn). at is the span of the call. It panics with
// "unknown extern <name>" if the table has no such name. That is a Go panic and
// not a Ki panic (Halt{Code: 101}) on purpose: no program can cause it, so it is
// a bug of the compiler, which the driver reports as an internal compiler error.
func (m *Machine) callExtern(name string, at base.Span, args []Value) (Value, *Halt) {
	f, ok := externs[name]
	if !ok {
		panic("unknown extern " + name)
	}
	return f(m, at, args)
}
