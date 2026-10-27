package driver

import (
	"github.com/aldezex/kilang/internal/syntax"
)

// runCheck is `ki check [--diagnostics=human|short|json] FILE`: it reads FILE,
// reports what is wrong with it and exits with 1 if anything is, with 0 if
// nothing is. A clean file prints nothing.
//
// It lexes the file, and Lex starts by checking the bytes of the source (invalid
// UTF-8, control characters), so those errors come out once. Then it parses the
// tokens, whatever the lexer reported: the parser recovers from a syntax error
// and goes on, so one run reports one error for each broken statement or item
// and not only the first. A few inputs cost more, as the doc comment of
// syntax.Parse says: a `{` that is never closed (each line after it that begins
// an item is one more error) and others by one or two errors, such as a block
// that is not the branch of an `if` (the `else` after it is an error of its
// own). The tree itself is of no use to check yet.
//
// With --diagnostics=json each diagnostic is one line of JSON on stderr, but
// two things are still written there as plain text: the "error: cannot read
// file" line when FILE cannot be loaded, and the note that ReportAll adds after
// 100 errors. A tool that reads the JSON must skip the lines that do not start
// with '{', as the runner of `ki test` does.
func runCheck(s *Session, c *command, args []string) int {
	id, code, done := fileArgs(s, c, args)
	if done {
		return code
	}
	tb := syntax.Lex(&s.Sources, id, s.Interner, &s.Sink)
	syntax.Parse(&tb, id, s.Interner, &s.Sink)
	s.ReportAll()
	if s.Sink.HasErrors() {
		return 1
	}
	return 0
}
