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
// and runs the passes that exist so far on it. It never fails: the errors in
// source are diagnostics in p.Session.Sink (see ErrorCodes).
func Analyze(t *testing.T, source string) *Program {
	t.Helper()
	// The session writes into the buffers of the Program, so the Program has to
	// exist, and stay where it is, before the session does.
	p := &Program{}
	p.Session = driver.NewSession(strings.NewReader(""), &p.Out, &p.Err)
	p.Frontend = p.Session.ParseSource("test.ki", []byte(source))
	p.Session.Analyze(p.Frontend)
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
