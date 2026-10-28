// Package testsupport runs a piece of Ki source through the compiler for the
// tests of the passes. It imports internal/driver, so only external test
// packages (sema_test…) and tests/golden_test.go may import it: a test inside
// a package that driver imports would be an import cycle.
package testsupport

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/driver"
	"github.com/aldezex/kilang/internal/sema"
)

// Program is a piece of source after the compiler has been through it.
type Program struct {
	// Session holds the sources, the interner and the sink of diagnostics.
	Session *driver.Session
	// Frontend holds the trees, with the prelude as module 0 and the source as
	// module 1, and what the passes made of them.
	Frontend *driver.Frontend
	// Out and Err are the stdout and the stderr of the Session.
	Out, Err bytes.Buffer
}

// Analyze parses source as a file called test.ki, together with the prelude,
// and runs the passes that exist so far on it: name resolution and the type
// checker. It never fails: the errors in source are diagnostics in
// p.Session.Sink (see ErrorCodes).
func Analyze(t *testing.T, source string) *Program {
	t.Helper()
	p := parse(source)
	p.Session.Analyze(p.Frontend)
	return p
}

// Resolve parses source like Analyze and runs name resolution only: p.Frontend
// has Resolved and no Typed, and the errors are those of the lexer, the parser
// and Resolve. It is for the tests of what a name stands for, whose sources
// need not type check (`-print` is not a program, but the `print` in it still
// stands for the prelude's function).
func Resolve(t *testing.T, source string) *Program {
	t.Helper()
	p := parse(source)
	f := p.Frontend
	f.Resolved = sema.Resolve(f.Modules, f.User, p.Session.Interner, &p.Session.Sink)
	return p
}

// parse makes the Program of source and parses it, together with the prelude.
func parse(source string) *Program {
	// The session writes into the buffers of the Program, so the Program has to
	// exist, and stay where it is, before the session does.
	p := &Program{}
	p.Session = driver.NewSession(strings.NewReader(""), &p.Out, &p.Err)
	p.Frontend = p.Session.ParseSource("test.ki", []byte(source))
	return p
}

// ErrorCodes returns the code of each error that the passes reported, in the
// order they reported them (not the order of the source): ["E0301"], or nil if
// there are none. Warnings are not listed.
func ErrorCodes(p *Program) []string {
	var codes []string
	for _, d := range p.Session.Sink.All() {
		if d.Severity == diag.Error {
			codes = append(codes, d.Code.String())
		}
	}
	return codes
}

// DefType returns the type of the function called name, written the way
// TypeTable.Show writes it: `(Int) -> Int`. It looks first among the functions
// of the user's file, the first one if there are several, and then among those
// of the prelude, which is the order in which a name in the file finds them. It
// fails the test if neither defines the name, or if the type checker has not
// run.
func DefType(t *testing.T, p *Program, name string) string {
	t.Helper()
	rp, typed := p.Frontend.Resolved, p.Frontend.Typed
	if rp == nil || typed == nil {
		t.Fatalf("the program has not been type checked (Resolved %v, Typed %v)", rp != nil, typed != nil)
	}
	for _, mod := range []sema.ModuleIdx{p.Frontend.User, 0} {
		for id, d := range rp.Defs.All() {
			if d.Module == mod && p.Session.Interner.String(d.Name) == name {
				return typed.Types.Show(typed.DefTypes.Get(id))
			}
		}
	}
	t.Fatalf("neither the file nor the prelude defines a function called %q", name)
	panic("unreachable")
}
