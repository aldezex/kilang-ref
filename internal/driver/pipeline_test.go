package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/diag"
)

func TestPipeline(t *testing.T) {
	t.Run("parse_source_puts_prelude_first", func(t *testing.T) {
		const src = "fn main() {}\n"
		var errOut strings.Builder
		s := NewSession(strings.NewReader(""), &strings.Builder{}, &errOut)
		f := s.ParseSource("a.ki", []byte(src))
		if len(f.Modules) != 2 || f.User != 1 {
			t.Fatalf("%d modules and User %d, want 2 and 1", len(f.Modules), f.User)
		}
		if f.Resolved != nil {
			t.Errorf("Resolved is %+v before Analyze, want nil", f.Resolved)
		}
		for i, want := range []struct{ path, text string }{
			{"std/prelude.ki", "ki_print_int"},
			{"a.ki", "fn main() {}"},
		} {
			id := f.Modules[i].File
			if got := s.Sources.DisplayPath(id); got != want.path {
				t.Errorf("module %d is the file %q, want %q", i, got, want.path)
			}
			if got := string(s.Sources.Contents(id)); !strings.Contains(got, want.text) {
				t.Errorf("module %d has the text %q, want it to contain %q", i, got, want.text)
			}
		}
		// Each module is the tree of its own file: the prelude has its one
		// function, with its attribute, and the file has `main`.
		if prelude, user := f.Modules[0], f.Modules[1]; len(prelude.Items) != 1 || len(prelude.AttrPool) != 1 ||
			len(user.Items) != 1 || len(user.AttrPool) != 0 || s.Interner.String(user.ItemNodes.Get(user.Items[0]).Name) != "main" {
			t.Errorf("prelude has %d items and %d attributes, file has %d items and %d attributes; want 1 and 1, 1 and 0, the item being main",
				len(prelude.Items), len(prelude.AttrPool), len(user.Items), len(user.AttrPool))
		}
		// Neither the prelude nor the file reports anything.
		if diags := s.Sink.All(); len(diags) != 0 || errOut.Len() != 0 {
			t.Errorf("diagnostics %+v and stderr %q, want none", diags, errOut.String())
		}
	})

	t.Run("unreadable_file", func(t *testing.T) {
		const path = "/nonexistent/x.ki"
		const prefix = "error: cannot read file `" + path + "`: "
		var errOut strings.Builder
		s := NewSession(strings.NewReader(""), &strings.Builder{}, &errOut)
		f, ok := s.LoadAndParse(path)
		if f != nil || ok {
			t.Errorf("LoadAndParse returned %v and %v, want nil and false", f, ok)
		}
		if got := errOut.String(); !strings.HasPrefix(got, prefix) || strings.Count(got, "\n") != 1 || !strings.HasSuffix(got, "\n") {
			t.Errorf("stderr %q, want one line that starts with %q", got, prefix)
		}
		if diags := s.Sink.All(); len(diags) != 0 {
			t.Errorf("diagnostics %+v, want none", diags)
		}
		// Every command that reads a file says the same and exits with 1,
		// whichever way it reads it: the very line LoadAndParse wrote.
		line := errOut.String()
		for _, args := range [][]string{{"emit", "tokens", path}, {"emit", "ast", path}} {
			code, out, err := runCLI(t, args...)
			if code != 1 || out != "" || err != line {
				t.Errorf("ki %s: got %d %q %q, want 1, nothing on stdout and %q on stderr",
					strings.Join(args, " "), code, out, err, line)
			}
		}
	})

	t.Run("analyze_does_not_write", func(t *testing.T) {
		// Analyze reports to the sink and ReportAll writes: a command decides
		// when the diagnostics are written (emit writes its dump first).
		var errOut strings.Builder
		s := NewSession(strings.NewReader(""), &strings.Builder{}, &errOut)
		f := s.ParseSource("a.ki", []byte("fn f() {}\nfn f() {}\n"))
		s.Analyze(f)
		if f.Resolved == nil {
			t.Error("Resolved is nil after Analyze")
		}
		if got := s.Sink.Count(diag.E(302)); got != 1 || len(s.Sink.All()) != 1 {
			t.Errorf("sink %+v, want exactly one E0302", s.Sink.All())
		}
		if errOut.Len() != 0 {
			t.Errorf("Analyze wrote %q to stderr, want nothing", errOut.String())
		}
	})

	t.Run("emit_ast_shows_syntax_only", func(t *testing.T) {
		// A function defined twice is a mistake of names and not of syntax:
		// the dump has both and ki emit ast neither reports it nor fails. ki
		// check does (tests/errors/ch07/duplicate_definition.ki).
		path := filepath.Join(t.TempDir(), "a.ki")
		if err := os.WriteFile(path, []byte("fn f() {}\nfn f() {}\n"), 0o644); err != nil {
			t.Fatalf("cannot write the file: %v", err)
		}
		code, out, err := runCLI(t, "emit", "ast", path)
		if want := "(fn f (params) (ret _) (block))\n(fn f (params) (ret _) (block))\n"; code != 0 || out != want || err != "" {
			t.Errorf("got %d %q %q, want 0 %q \"\"", code, out, err, want)
		}
	})
}
