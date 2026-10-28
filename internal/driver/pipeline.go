package driver

import (
	"fmt"
	"io/fs"

	"github.com/aldezex/kilang"
	"github.com/aldezex/kilang/internal/base"
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
	// Resolved and Typed are nil until Analyze has run.
	Resolved *sema.ResolvedProgram
	Typed    *sema.TypedProgram
}

// ParseSource lexes and parses source, a file that is called displayPath in
// diagnostics, together with the prelude, which it reads from the standard
// library embedded in the binary. Module 0 is the prelude ("std/prelude.ki")
// and module 1 is the file. Lexical and syntax errors go to the session's
// sink. The Session takes ownership of contents, as SourceManager.Add does.
func (s *Session) ParseSource(displayPath string, contents []byte) *Frontend {
	return s.parseFiles(s.Sources.Add(displayPath, contents))
}

// LoadAndParse reads the file at path and does what ParseSource does with it.
// If the file cannot be read it writes "error: cannot read file `<path>`:
// <reason>" to stderr and returns nil and false. The exit code, 1, is for the
// caller to return.
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
		tb := syntax.Lex(&s.Sources, id, s.Interner, &s.Sink)
		f.Modules = append(f.Modules, syntax.Parse(&tb, id, s.Interner, &s.Sink))
	}
	return f
}

// Analyze runs the passes after the parser on f: so far, name resolution and
// the type checker. Both run whether or not the file has errors, because what an
// earlier pass could not understand has the error type, which the next one
// never reports again. It reports to the session's sink and does not write
// them; ReportAll does.
func (s *Session) Analyze(f *Frontend) {
	f.Resolved = sema.Resolve(f.Modules, f.User, s.Interner, &s.Sink)
	f.Typed = sema.Check(f.Modules, f.Resolved, s.Interner, &s.Sink)
}
