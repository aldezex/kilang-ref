package driver

import (
	"fmt"
	"runtime/debug"
)

// ExitICE is the exit code of ki after an internal compiler error.
const ExitICE = 70

// debugPanicEnv is the environment variable that Main reads into
// Session.DebugPanicIn: it names the pass that panics on purpose, to try the
// ICE boundary.
const debugPanicEnv = "KI_DEBUG_PANIC_IN"

// ICE is what a Go panic inside a pass becomes: a report instead of a crash. It
// means ki has a bug, as opposed to a diagnostic, which means the program it
// was given has one. A Go fatal error, such as a stack overflow, is not a panic
// and still kills the process: recover cannot see it.
type ICE struct {
	Pass  string // the pass that panicked: "lex", "parse", "resolve"…
	Value any    // what the pass panicked with
	Stack []byte // the stack of the goroutine at the moment of the panic
}

// Error returns "internal compiler error: <pass>: <value>", with the value
// written as %v does.
func (e *ICE) Error() string {
	return fmt.Sprintf("internal compiler error: %s: %v", e.Pass, e.Value)
}

// guard runs f, the code of the pass called pass, and returns nil if f returns
// normally. If f panics, guard recovers and returns the ICE that describes the
// panic. The stack is taken inside the deferred function, which runs before
// the panic has unwound anything, so it still shows the function that
// panicked.
func guard(pass string, f func()) (ice *ICE) {
	defer func() {
		if r := recover(); r != nil {
			ice = &ICE{Pass: pass, Value: r, Stack: debug.Stack()}
		}
	}()
	f()
	return nil
}

// runPass runs f as the pass called name, under guard. If the pass panics, no
// other pass can run on what it left half made, so runPass does not return:
// it panics with the *ICE, which unwinds the command up to Main (see
// catchICE).
//
// It is also the debug hook: when DebugPanicIn is name, the pass panics on
// entry, before f runs.
func (s *Session) runPass(name string, f func()) {
	ice := guard(name, func() {
		if s.DebugPanicIn == name {
			panic("debug panic requested")
		}
		f()
	})
	if ice != nil {
		panic(ice)
	}
}

// catchICE is the deferred call of Main. If the command is unwinding because a
// pass raised an ICE, it writes the report to stderr (the message, a newline
// and the stack) and sets *code to ExitICE. It does not touch any other
// panic: those are not passes, so they are not reports, and it raises them
// again.
func (s *Session) catchICE(code *int) {
	r := recover()
	if r == nil {
		return
	}
	ice, ok := r.(*ICE)
	if !ok {
		panic(r)
	}
	// stderr is the last place left to report this; if it fails too there is
	// nowhere else to say so.
	_, _ = fmt.Fprintf(s.Stderr, "%s\n%s", ice.Error(), ice.Stack)
	*code = ExitICE
}
