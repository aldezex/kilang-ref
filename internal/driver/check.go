package driver

// runCheck is `ki check [--diagnostics=human|short|json] FILE`: it reads FILE,
// reports what is wrong with it and exits with 1 if anything is, with 0 if
// nothing is. A clean file prints nothing.
//
// It lexes the file and the prelude, and Lex starts by checking the bytes of the
// source (invalid UTF-8, control characters), so those errors come out once.
// Then it parses the tokens, whatever the lexer reported: the parser recovers
// from a syntax error and goes on, so one run reports one error for each broken
// statement or item and not only the first. A few inputs cost more, as the doc
// comment of syntax.Parse says: a `{` that is never closed (each line after it
// that begins an item is one more error) and others by one or two errors, such
// as a block that is not the branch of an `if` (the `else` after it is an error
// of its own). Last it analyzes the trees (see Analyze): so far it resolves
// names (see sema.Resolve) and checks types (see sema.Check). Resolving
// collects the top-level definitions, binds the parameters and the `let`s,
// resolves every name in the function bodies, and reports what is wrong: a name
// that stands for nothing (with a suggestion and its edit if one is spelled
// almost like it), a function defined twice and an attribute that is not
// allowed. Checking then finds the type of every expression and reports the
// ones that do not fit where they are used. That happens even if the file has
// syntax errors: the error nodes of the trees define nothing and have the error
// type, except that a function whose signature could not be read after its name
// still has the name, so the calls to it are not reported as unknown names.
//
// With --diagnostics=json each diagnostic is one line of JSON on stderr, but
// two things are still written there as plain text: the "error: cannot read
// file" line when FILE cannot be loaded, and the note that ReportAll adds after
// 100 errors. A tool that reads the JSON must skip the lines that do not start
// with '{', as the runner of `ki test` does.
func runCheck(s *Session, c *command, args []string) int {
	path, code, done := fileArgs(s, c, args)
	if done {
		return code
	}
	f, ok := s.LoadAndParse(path)
	if !ok {
		return 1
	}
	s.Analyze(f)
	s.ReportAll()
	if s.Sink.HasErrors() {
		return 1
	}
	return 0
}
