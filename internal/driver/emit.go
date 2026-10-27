package driver

import (
	"errors"
	"fmt"

	"github.com/aldezex/kilang/internal/syntax"
)

// runEmit is `ki emit TARGET [--diagnostics=human|short|json] FILE`: it shows
// what one stage of the compiler makes of FILE. The target comes first, and its
// own flags and FILE after it. Today the only target is tokens. A request for
// help (-h, -help or --help) in the place of the target gets the same answer as
// after it, as in `ki emit tokens -h`: the usage line and exit code 0. Any other
// flag there is not a target.
func runEmit(s *Session, c *command, args []string) int {
	if len(args) == 0 {
		return usageError(s, c, errors.New("missing target"))
	}
	switch args[0] {
	case "-h", "-help", "--help":
		return printUsage(s, c)
	case "tokens":
		return emitTokens(s, c, args[1:])
	}
	return usageError(s, c, fmt.Errorf("unknown target `%s`", args[0]))
}

// emitTokens is `ki emit tokens`: it lexes FILE and prints the tokens on stdout,
// one per line (see syntax.DumpTokens), and then the diagnostics on stderr. It
// exits with 1 if the file has errors, but only after both are written, so the
// dump of a file with errors shows what the lexer made of it.
func emitTokens(s *Session, c *command, args []string) int {
	id, code, done := fileArgs(s, c, args)
	if done {
		return code
	}
	tb := syntax.Lex(&s.Sources, id, s.Interner, &s.Sink)
	code = printOut(s, c, syntax.DumpTokens(&tb, &s.Sources, s.Interner))
	s.ReportAll()
	if code == 0 && s.Sink.HasErrors() {
		return 1
	}
	return code
}
