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
// the caller, and ReportAll panics on the first diagnostic it has to write.
func (s *Session) ReportAll() {
	s.Sink.SortByPosition()
	var sb strings.Builder
	for i, d := range s.Sink.All() {
		if i > 0 && s.Format == FormatHuman {
			sb.WriteString("\n")
		}
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
