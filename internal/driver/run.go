package driver

import (
	"flag"
	"fmt"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/interp"
)

// backends are the values that --backend takes. The oracle, the interpreter, is
// the only backend so far; the others come with the chapters that build them.
var backends = []string{"interp"}

// runRun is `ki run [--backend=interp] [--diagnostics=human|short|json] FILE`: it
// compiles FILE and runs its `main`.
//
// A program that has errors in the front end gets them written to stderr and
// exit code 1, and nothing runs. The warnings that the front end found (W0701) are
// written with the errors and not otherwise: a program that runs has its stderr to
// itself, and the first line of it is the one of its panic. A program with no
// errors but no `main` gets
// E0303, with the same exit code: `ki check` accepts such a file, because a
// library is a fine thing to check, but there is nothing to start. Otherwise
// the program runs on the machine of package interp, which reads KI_STACK_MB for
// the limit of its stack, and the exit code of ki is the one the program ends
// with: 0 when `main` returns, 101 for a Ki panic and 103 for a stack that
// overflowed, the two with their message already on stderr.
//
// What the program prints goes to the session's stdout and its panics to its
// stderr, both through the writers that Main was given.
func runRun(s *Session, c *command, args []string) int {
	var backend string
	path, code, done := fileArgsWith(s, c, args, func(fs *flag.FlagSet) func() error {
		fs.StringVar(&backend, "backend", "interp", "what runs the program: interp")
		return func() error {
			for _, b := range backends {
				if backend == b {
					return nil
				}
			}
			return fmt.Errorf("unknown backend %q", backend)
		}
	})
	if done {
		return code
	}
	f, ok := s.LoadAndParse(path)
	if !ok {
		return 1
	}
	s.Analyze(f)
	if !s.Sink.HasErrors() && !f.Resolved.HasMain {
		// The whole file is the place: there is no line of it that is to blame
		// and no edit that fixes it (where `main` goes, and what it does, is not
		// for the compiler to say), so the diagnostic is on the start of the
		// file, an empty span, and it has help and no fix-it.
		at := base.Span{File: f.Modules[f.User].File}
		s.Sink.Report(diag.NewError(diag.E(303), "no `main` function",
			diag.Label{Span: at, Message: "this file has no `fn main()`"}).
			WithNote("`ki run` starts the program at `fn main()`; `ki check` does not need one").
			WithHelp("add `fn main() { … }`"))
	}
	if s.Sink.HasErrors() {
		s.report()
		return 1
	}
	p := s.Lower(f)
	s.lint(p)
	return s.execute(p)
}

// execute runs the `main` of p, a program that Lint has accepted and that has a
// `main`, on the interpreter, and returns the exit code that the program ends
// with. The machine is a pass like the others (the pass "run"), so a Go panic
// inside it, such as the one for an extern that the table does not have, is an
// internal compiler error: Main reports it and exits with 70, and it is not a
// crash. A panic of the Ki program is not a Go panic: it is the Halt that the
// machine returns, and its code is the one execute returns.
func (s *Session) execute(p *core.Program) int {
	var h interp.Halt
	s.runPass("run", func() {
		h = interp.New(p, &s.Sources, s.Stdout, s.Stderr).RunMain()
	})
	return h.Code
}
