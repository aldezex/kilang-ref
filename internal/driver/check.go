package driver

import (
	"fmt"

	"github.com/aldezex/kilang/internal/syntax"
)

// runCheck is `ki check [--diagnostics=human|short|json] FILE`: it reads FILE,
// reports what is wrong with it and exits with 1 if anything is, with 0 if
// nothing is. A clean file prints nothing.
//
// With --diagnostics=json each diagnostic is one line of JSON on stderr, but
// two things are still written there as plain text: the "error: cannot read
// file" line when FILE cannot be loaded, and the note that ReportAll adds after
// 100 errors. A tool that reads the JSON must skip the lines that do not start
// with '{', as the runner of `ki test` does.
func runCheck(s *Session, c *command, args []string) int {
	fs := newFlagSet(c)
	diagnostics := fs.String("diagnostics", "human", "how to write diagnostics: human, short or json")
	if code, done := parseFlags(s, c, fs, args); done {
		return code
	}
	format, err := parseDiagFormat(*diagnostics)
	if err != nil {
		return usageError(s, c, err)
	}
	switch fs.NArg() {
	case 0:
		return usageError(s, c, fmt.Errorf("missing FILE"))
	case 1:
	default:
		return usageError(s, c, fmt.Errorf("unexpected argument %q", fs.Arg(1)))
	}
	s.Format = format

	path := fs.Arg(0)
	id, err := s.Sources.Load(path, path)
	if err != nil {
		// stderr is the last place left to report this; if it fails too there
		// is nowhere else to say so.
		_, _ = fmt.Fprintf(s.Stderr, "error: %v\n", err)
		return 1
	}
	syntax.CheckSource(&s.Sources, id, &s.Sink)
	s.ReportAll()
	if s.Sink.HasErrors() {
		return 1
	}
	return 0
}
