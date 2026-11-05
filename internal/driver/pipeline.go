package driver

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/aldezex/kilang"
	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/sema"
	"github.com/aldezex/kilang/internal/syntax"
)

// The prelude is module 0 of every program and the user's file is module 1.
// sema.Resolve relies on the first (it is the module whose pub definitions the
// others see); the second is only the choice of ParseSource.
const (
	// preludeDisplayPath is the path that diagnostics show for the prelude.
	preludeDisplayPath = "std/prelude.ki"
	// preludeEmbedPath is where the prelude lives in kilang.Stdlib.
	preludeEmbedPath = "stdlib/prelude.ki"
	// userModule is the index of the user's file among the modules.
	userModule sema.ModuleIdx = 1
)

// Frontend is what the passes before Core make of a program: its syntax trees
// and what they learned about them. The prelude is Modules[0] and the user's
// file is Modules[User].
type Frontend struct {
	Modules []*syntax.Module
	User    sema.ModuleIdx
	// Resolved, Decls and Typed are nil until Analyze has run.
	Resolved *sema.ResolvedProgram
	Decls    *sema.Decls
	Typed    *sema.TypedProgram
}

// ParseSource lexes and parses source, a file that is called displayPath in
// diagnostics, together with the prelude, which it reads from the standard
// library embedded in the binary. Module 0 is the prelude ("std/prelude.ki")
// and module 1 is the file. Lexical and syntax errors go to the session's
// sink. The Session takes ownership of contents, as SourceManager.Add does.
//
// If the lexer or the parser panics, ParseSource does not return: it panics
// with the *ICE (see runPass), which Main turns into the report.
func (s *Session) ParseSource(displayPath string, contents []byte) *Frontend {
	return s.parseFiles(s.Sources.Add(displayPath, contents))
}

// LoadAndParse reads the file at path and does what ParseSource does with it.
// If the file cannot be read it writes "error: cannot read file `<path>`:
// <reason>" to stderr and returns nil and false. The exit code, 1, is for the
// caller to return. Like ParseSource, it panics with the *ICE if the lexer or
// the parser panics.
func (s *Session) LoadAndParse(path string) (*Frontend, bool) {
	id, ok := s.load(path)
	if !ok {
		return nil, false
	}
	return s.parseFiles(id), true
}

// load reads the file at path into the session's sources. If it cannot, it
// writes the error to stderr, as plain text whatever the diagnostic format, and
// returns false.
func (s *Session) load(path string) (base.FileID, bool) {
	id, err := s.Sources.Load(path, path)
	if err != nil {
		// stderr is the last place left to report this; if it fails too there
		// is nowhere else to say so.
		_, _ = fmt.Fprintf(s.Stderr, "error: %v\n", err)
		return 0, false
	}
	return id, true
}

// parseFiles lexes and parses the prelude and the user's file, already in the
// session's sources as user, with the session's one interner: the same name is
// the same Symbol in both, which is what lets a name of the file find the
// prelude's definition.
func (s *Session) parseFiles(user base.FileID) *Frontend {
	prelude, err := fs.ReadFile(kilang.Stdlib, preludeEmbedPath)
	if err != nil {
		// go build embedded the file, so this is a bug of ki and not of the
		// program being compiled.
		panic(fmt.Sprintf("driver: cannot read the embedded prelude: %v", err))
	}
	f := &Frontend{User: userModule}
	for _, id := range []base.FileID{s.Sources.Add(preludeDisplayPath, prelude), user} {
		tb := s.lex(id)
		var m *syntax.Module
		s.runPass("parse", func() { m = syntax.Parse(&tb, id, s.Interner, &s.Sink) })
		f.Modules = append(f.Modules, m)
	}
	return f
}

// lex runs the lexer on the file id as the pass "lex" (see runPass). Both
// parseFiles and `ki emit tokens` lex through it.
func (s *Session) lex(id base.FileID) (tb syntax.TokenBuffer) {
	s.runPass("lex", func() { tb = syntax.Lex(&s.Sources, id, s.Interner, &s.Sink) })
	return tb
}

// Analyze runs the passes after the parser on f: so far, name resolution (the
// pass "resolve"), the declarations (the pass "declare", which makes the table of
// types from the `type` items) and the type checker (the pass "check"), in that
// order. All three run whether or not the file has errors, because what an
// earlier pass could not understand has the error type, which the next one never
// reports again. It reports to the session's sink and does not write them;
// ReportAll does. If any pass panics, Analyze does not return: it panics with
// the *ICE (see runPass). Check is called only from here: it needs the Decls
// that the pass before it made.
func (s *Session) Analyze(f *Frontend) {
	s.runPass("resolve", func() { f.Resolved = sema.Resolve(f.Modules, f.User, s.Interner, &s.Sink) })
	s.runPass("declare", func() { f.Decls = sema.Declare(f.Modules, f.Resolved, s.Interner, &s.Sink) })
	s.runPass("check", func() { f.Typed = sema.Check(f.Modules, f.Resolved, f.Decls, s.Interner, &s.Sink) })
}

// Lower elaborates f, which Analyze has been through, to Core (the pass
// "elaborate"). f must have no errors, as Elaborate needs: the caller checks the
// sink before it lowers. If the pass panics, Lower does not return: it panics
// with the *ICE (see runPass).
func (s *Session) Lower(f *Frontend) *core.Program {
	var p *core.Program
	s.runPass("elaborate", func() { p = core.Elaborate(f.Modules, f.Resolved, f.Typed, s.Interner) })
	return p
}

// lintOrPanic checks p with core.Lint, which spells the names of the violations
// with in, and panics if it finds any violation, with the violations, one to a
// line, as the value. A Core that breaks its own invariants is a bug of the
// compiler and never a fault of the program, so the panic is the right answer:
// it reaches the boundary of the pass lint as an ICE (see runPass), which Main
// writes as the report and exit code 70.
func lintOrPanic(p *core.Program, in *base.Interner) {
	if violations := core.Lint(p, in); len(violations) > 0 {
		panic(strings.Join(violations, "\n"))
	}
}

// lint checks the Core that Lower made, as the pass "lint" (see runPass). Lower
// does not call it, so this is a rule for the commands and not something the
// code enforces: every command that lowers a program must call lint right after
// Lower and before it runs or shows the Core. `ki emit core` and `ki run` are the
// two so far, and the row with the hook in lint of TestICE/debug_hook_exits_70
// (for the first) and of TestRun/debug_hook_exits_70_in_each_pass (for the
// second) is what shows that each does; the next command that lowers needs its
// own row. If the Core is not valid, lint does not return: it panics with the
// *ICE.
func (s *Session) lint(p *core.Program) {
	s.runPass("lint", func() { lintOrPanic(p, s.Interner) })
}
