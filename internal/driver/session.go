package driver

import "io"

// Session holds what every command shares. It grows with the book (sources,
// interner, diagnostics…).
//
// Commands read and write through the Session instead of os.Stdin, os.Stdout
// and os.Stderr, so a test can run ki in-process and capture its output.
type Session struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// NewSession returns a Session that reads from stdin and writes to stdout and
// stderr. It is the constructor that Main and the tests use. Every field that
// needs a start value other than zero is set here as the book adds it, so
// build a Session with NewSession and not with a &Session{...} literal.
func NewSession(stdin io.Reader, stdout, stderr io.Writer) *Session {
	return &Session{Stdin: stdin, Stdout: stdout, Stderr: stderr}
}
