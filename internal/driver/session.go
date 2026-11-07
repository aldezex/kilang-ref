package driver

import (
	"fmt"
	"io"
	"strings"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// DiagFormat is the way ki writes diagnostics, chosen with --diagnostics.
type DiagFormat uint8

const (
	// FormatHuman shows each diagnostic with the lines of source it points at.
	FormatHuman DiagFormat = iota
	// FormatShort writes each diagnostic on one line, "path:line:col: error[E0401]: msg".
	FormatShort
	// FormatJSON writes each diagnostic as one line of JSON, for tools.
	FormatJSON
)

// parseDiagFormat turns the value of --diagnostics into a DiagFormat.
func parseDiagFormat(v string) (DiagFormat, error) {
	switch v {
	case "human":
		return FormatHuman, nil
	case "short":
		return FormatShort, nil
	case "json":
		return FormatJSON, nil
	}
	return 0, fmt.Errorf("invalid --diagnostics value %q (want human, short or json)", v)
}

// Session holds what every command shares: the standard streams, the source
// files read so far and the diagnostics the passes have reported about them.
// It grows with the book.
//
// Commands read and write through the Session instead of os.Stdin, os.Stdout
// and os.Stderr, so a test can run ki in-process and capture its output.
type Session struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Sources        base.SourceManager
	Sink           diag.Sink
	Interner       *base.Interner // the identifiers of the files lexed so far; NewSession makes it
	Format         DiagFormat     // how ReportAll writes the diagnostics; one of the constants above
	// DebugPanicIn is the name of the pass that panics on purpose when it
	// starts ("lex", "parse", "resolve", "declare", "check", "patterns"…), to try the ICE boundary (see
	// runPass); empty means none. Main sets it from KI_DEBUG_PANIC_IN, and
	// NewSession leaves it empty: a Session made with NewSession does not
	// depend on the environment, but one that Main makes does. The tests of
	// this package that go through Main clear the variable first, and so do
	// those of package tests (the golden files and the error catalog also run
	// the compiler through Main): see the TestMain of each. ci/ci-local.sh
	// unsets it for the golden files that it runs through the real binary.
	DebugPanicIn string
}

// NewSession returns a Session that reads from stdin and writes to stdout and
// stderr, with the human format for diagnostics. It is the constructor that
// Main and the tests use. Every field that needs a start value other than zero
// is set here as the book adds it, so build a Session with NewSession and not
// with a &Session{...} literal.
func NewSession(stdin io.Reader, stdout, stderr io.Writer) *Session {
	return &Session{Stdin: stdin, Stdout: stdout, Stderr: stderr, Interner: base.NewInterner(), Format: FormatHuman}
}

// truncationNote is the last line of the diagnostics when the sink dropped
// errors past diag.MaxErrors.
const truncationNote = "note: too many errors; the first 100 are shown\n"

// ReportAll writes every diagnostic of the sink to stderr in the session's
// format, ordered by position. In the human format a blank line separates one
// diagnostic from the next. If the sink dropped errors, the last line says so
// (in the human format after a blank line, so it is not taken for part of the
// last diagnostic). With FormatJSON the note is not JSON: it is a plain line
// among the lines of JSON, and a reader of them skips the lines that do not
// start with '{'.
//
// Stderr is the last place left to report a failure, so a failed write is
// ignored. A Format that is not one of the DiagFormat constants is a bug of
// the caller, and ReportAll panics on the first diagnostic it has to write. The
// commands call it through report.
func (s *Session) ReportAll() { s.writeDiagnostics(false) }

// writeDiagnostics is ReportAll, or, with errorsOnly, ReportAll for the
// diagnostics that are errors: a warning is for the commands whose business is to
// say what is wrong with a program (see reportErrors).
func (s *Session) writeDiagnostics(errorsOnly bool) {
	s.Sink.SortByPosition()
	var sb strings.Builder
	written := 0
	for _, d := range s.Sink.All() {
		if errorsOnly && d.Severity != diag.Error {
			continue
		}
		if written > 0 && s.Format == FormatHuman {
			sb.WriteString("\n")
		}
		written++
		sb.WriteString(s.render(d))
	}
	if s.Sink.Truncated() {
		if s.Format == FormatHuman {
			sb.WriteString("\n")
		}
		sb.WriteString(truncationNote)
	}
	_, _ = io.WriteString(s.Stderr, sb.String())
}

// report runs ReportAll as the pass "render" (see runPass), which is how every
// command writes its diagnostics. A diagnostic that the renderer cannot draw (a
// label in another file, say) is a bug of the pass that built it, so it ends in
// an ICE: exit code 70 and the buffers of kirt/out flushed, not Go's own report.
func (s *Session) report() {
	s.runPass("render", s.ReportAll)
}

// reportErrors is report for a command that shows something other than
// diagnostics, the dump of `ki emit`: the warnings are left out, as `ki run` leaves
// them out by writing diagnostics only when the program has errors. A warning is
// about a program that is accepted, and `ki check` is the command that says what it
// thinks of one. Without it, the dump that a tool reads would be followed by a text
// that is not an error and that it did not ask for. The pass "render" runs all the
// same, so that the order of a dump and its diagnostics is the same for every
// program.
func (s *Session) reportErrors() {
	s.runPass("render", func() { s.writeDiagnostics(true) })
}

// render writes one diagnostic in the session's format.
func (s *Session) render(d diag.Diagnostic) string {
	switch s.Format {
	case FormatHuman:
		return diag.RenderHuman(d, &s.Sources)
	case FormatShort:
		return diag.RenderShort(d, &s.Sources)
	case FormatJSON:
		return diag.RenderJSON(d, &s.Sources)
	}
	panic(fmt.Sprintf("driver: unknown diagnostic format %d", s.Format))
}
