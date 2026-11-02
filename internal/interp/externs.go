package interp

import (
	"io"
	"strconv"
)

// externs holds the functions that the runtime provides, by the name that the
// attribute `#[extern("…")]` gives them in the standard library. A Fun with no
// body and an ExternName is run by looking the name up here: the function gets
// the machine, for its output, and the arguments of the call, and returns the
// value of the call.
//
// Only the standard library declares externs (E0304 keeps a program from
// declaring its own), so a name that is not in the table is a mistake of the
// compiler or of the library and never of the program: see callExtern.
var externs = map[string]func(m *Machine, args []Value) Value{
	// print(x: Int) writes x in decimal and a line feed to the stdout of the
	// program. It is a single write, so a writer that flushes at each line break
	// flushes once. A failed write is not reported here: print has no way to say
	// so (its type is (Int) -> ()). A kirt/out Writer keeps its first error, and
	// out.Exit reports it and ends with 1 if the program would otherwise have
	// ended with 0.
	"ki_print_int": func(m *Machine, args []Value) Value {
		_, _ = io.WriteString(m.out, strconv.FormatInt(intOf(args[0]), 10)+"\n")
		return UnitV{}
	},
}

// callExtern runs the function of the runtime that fun, which has no body,
// stands for, with the arguments args, and returns its value. It panics with
// "unknown extern <name>" if the table has no such name. That is a Go panic and
// not a Ki panic (Halt{Code: 101}) on purpose: no program can cause it, so it is
// a bug of the compiler, which the driver reports as an internal compiler error.
func (m *Machine) callExtern(name string, args []Value) Value {
	f, ok := externs[name]
	if !ok {
		panic("unknown extern " + name)
	}
	return f(m, args)
}
