package driver

import (
	"errors"
	"fmt"

	"github.com/aldezex/kilang/internal/syntax"
)

// runEmit is `ki emit TARGET [--diagnostics=human|short|json] FILE`: it shows
// what one stage of the compiler makes of FILE. The target comes first, and its
// own flags and FILE after it. The targets are tokens and ast. A request for
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
	case "ast":
		return emitAST(s, c, args[1:])
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
	return finishEmit(s, c, syntax.DumpTokens(&tb, &s.Sources, s.Interner))
}

// emitAST is `ki emit ast`: it lexes and parses FILE and prints the syntax tree
// on stdout, one line per item (see syntax.PrintModule), and then the
// diagnostics on stderr. As with emit tokens, the exit code is 1 if the file has
// errors, but only after both are written, so the dump of a file with syntax
// errors shows what the parser made of it, with an (error) where it could not
// read.
func emitAST(s *Session, c *command, args []string) int {
	id, code, done := fileArgs(s, c, args)
	if done {
		return code
	}
	tb := syntax.Lex(&s.Sources, id, s.Interner, &s.Sink)
	m := syntax.Parse(&tb, id, s.Interner, &s.Sink)
	return finishEmit(s, c, syntax.PrintModule(m, s.Interner))
}

// finishEmit ends every target of `ki emit` the same way, so that the order
// cannot drift between them: the dump out on stdout, then the diagnostics on
// stderr, and the exit code last. The exit code is 1 if the file has errors, but
// only after both are written; a dump that cannot be written is 1 as well (see
// printOut), and the diagnostics are still shown.
func finishEmit(s *Session, c *command, out string) int {
	code := printOut(s, c, out)
	s.ReportAll()
	if code == 0 && s.Sink.HasErrors() {
		return 1
	}
	return code
}
