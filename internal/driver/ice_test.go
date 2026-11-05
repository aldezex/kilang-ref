package driver

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/diag"
)

// TestMain clears KI_DEBUG_PANIC_IN before any test runs. Main reads it from the
// real environment, so a shell that had exported it would make every test that
// goes through Main (runCLI, the in-process golden runner) end in an ICE. The
// tests of the hook set it themselves, with t.Setenv.
func TestMain(m *testing.M) {
	if err := os.Unsetenv(debugPanicEnv); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// explode panics with "boom". It is a named function, so that its name shows in
// the stack of the ICE the tests make with it. The stack names it also when the
// compiler inlines it (a traceback shows inlined frames too), so it needs no
// directive.
func explode() { panic("boom") }

// writeKi writes text to a new file in a temporary directory and returns its path.
func writeKi(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.ki")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("cannot write the test file: %v", err)
	}
	return path
}

func TestGuard(t *testing.T) {
	t.Run("nil_without_panic", func(t *testing.T) {
		ran := false
		if ice := guard("lex", func() { ran = true }); ice != nil {
			t.Errorf("guard returned %+v for a function that did not panic, want nil", ice)
		}
		if !ran {
			t.Errorf("guard did not call the function")
		}
	})

	t.Run("captures_panic_value_and_stack", func(t *testing.T) {
		ice := guard("elaborate", func() { panic("boom") })
		if ice == nil {
			t.Fatal("guard returned nil for a function that panicked")
		}
		if ice.Pass != "elaborate" {
			t.Errorf("Pass = %q, want \"elaborate\"", ice.Pass)
		}
		if ice.Value != "boom" {
			t.Errorf("Value = %#v, want the string \"boom\", the value the function panicked with", ice.Value)
		}
		if want := "internal compiler error: elaborate: boom"; ice.Error() != want {
			t.Errorf("Error() = %q, want %q", ice.Error(), want)
		}
		if !strings.Contains(string(ice.Stack), "goroutine") {
			t.Errorf("Stack %q does not contain \"goroutine\"", ice.Stack)
		}

		// The stack is the one at the moment of the panic, so it names the
		// function that panicked.
		if ice := guard("parse", explode); ice == nil {
			t.Error("guard returned nil for explode, which panics")
		} else if !strings.Contains(string(ice.Stack), "driver.explode") {
			t.Errorf("the stack of a panic in explode is %q, want it to contain \"driver.explode\"", ice.Stack)
		}

		// The ICE keeps the very value that was panicked with, and the report
		// writes it the way fmt's %v does: an error by its message, a number as
		// a number.
		sentinel := errors.New("bad state")
		for _, row := range []struct {
			name string
			val  any
			want string
		}{
			{"error", sentinel, "internal compiler error: check: bad state"},
			{"number", 42, "internal compiler error: check: 42"},
		} {
			ice := guard("check", func() { panic(row.val) })
			if ice == nil {
				t.Errorf("%s: guard returned nil", row.name)
				continue
			}
			if ice.Value != row.val {
				t.Errorf("%s: Value = %#v, want %#v", row.name, ice.Value, row.val)
			}
			if got := ice.Error(); got != row.want {
				t.Errorf("%s: Error() = %q, want %q", row.name, got, row.want)
			}
		}
		// A panic that the runtime raises is an ICE like any other, and
		// keeps its own text.
		var empty []int
		if ice := guard("check", func() { _ = empty[3] }); ice == nil {
			t.Error("guard returned nil for an index out of range")
		} else if want := "internal compiler error: check: runtime error: index out of range [3] with length 0"; ice.Error() != want {
			t.Errorf("Error() = %q, want %q", ice.Error(), want)
		}
	})
}

// checkICE fails the test unless the result of a command is the report of an
// ICE in pass: exit code 70, nothing on stdout, and on stderr the message
// "internal compiler error: <pass>: debug panic requested" and then the stack.
func checkICE(t *testing.T, what string, code int, stdout, stderr, pass string) {
	t.Helper()
	if code != 70 {
		t.Errorf("%s: exit code %d, want 70", what, code)
	}
	if stdout != "" {
		t.Errorf("%s: stdout %q, want empty", what, stdout)
	}
	prefix := "internal compiler error: " + pass + ": debug panic requested\n"
	if !strings.HasPrefix(stderr, prefix) {
		t.Errorf("%s: stderr %q does not start with %q", what, stderr, prefix)
		return
	}
	if rest := stderr[len(prefix):]; !strings.HasPrefix(rest, "goroutine ") {
		t.Errorf("%s: the stack after the message is %q, want it to start with \"goroutine \"", what, rest)
	}
}

// raised runs f and returns what it panicked with, or nil if it returned.
func raised(f func()) (r any) {
	defer func() { r = recover() }()
	f()
	return nil
}

// newSession returns a Session whose hook, DebugPanicIn, is debugPanicIn, with
// no stdin and nothing written to stdout or stderr.
func newSession(debugPanicIn string) *Session {
	s := NewSession(strings.NewReader(""), io.Discard, io.Discard)
	s.DebugPanicIn = debugPanicIn
	return s
}

func TestICE(t *testing.T) {
	t.Run("debug_hook_exits_70", func(t *testing.T) {
		path := writeKi(t, "fn main() {}\n")

		t.Setenv("KI_DEBUG_PANIC_IN", "check")
		code, stdout, stderr := runCLI(t, "check", path)
		checkICE(t, "ki check with the hook in check", code, stdout, stderr, "check")

		// The hook fires in the pass it names, in each command that runs the
		// pass (check in ki check was the run above): the lexer and the parser
		// also in emit, and every pass up to Core in emit core.
		for _, row := range []struct {
			pass string
			args []string
		}{
			{"lex", []string{"check", path}},
			{"parse", []string{"check", path}},
			{"resolve", []string{"check", path}},
			{"declare", []string{"check", path}},
			{"lex", []string{"emit", "tokens", path}},
			{"lex", []string{"emit", "ast", path}},
			{"parse", []string{"emit", "ast", path}},
			{"lex", []string{"emit", "core", path}},
			{"parse", []string{"emit", "core", path}},
			{"resolve", []string{"emit", "core", path}},
			{"declare", []string{"emit", "core", path}},
			{"check", []string{"emit", "core", path}},
			{"elaborate", []string{"emit", "core", path}},
			{"lint", []string{"emit", "core", path}},
		} {
			t.Setenv("KI_DEBUG_PANIC_IN", row.pass)
			code, stdout, stderr := runCLI(t, row.args...)
			checkICE(t, "ki "+strings.Join(row.args[:len(row.args)-1], " ")+" with the hook in "+row.pass, code, stdout, stderr, row.pass)
		}

		// Writing the diagnostics is a pass too, called render, in each command
		// that writes them. The dumps of ki emit come out before it, so they are
		// on stdout as they are without the hook; and ki run writes diagnostics
		// only for a file that has errors.
		t.Setenv("KI_DEBUG_PANIC_IN", "render")
		code, stdout, stderr = runCLI(t, "check", path)
		checkICE(t, "ki check with the hook in render", code, stdout, stderr, "render")
		code, stdout, stderr = runCLI(t, "run", writeKi(t, "fn main() { y }\n"))
		checkICE(t, "ki run of a file with an error, with the hook in render", code, stdout, stderr, "render")
		code, stdout, stderr = runCLI(t, "emit", "core", writeKi(t, "fn main() { y }\n"))
		checkICE(t, "ki emit core of a file with an error, with the hook in render", code, stdout, stderr, "render")
		for _, target := range []string{"tokens", "ast", "core"} {
			t.Setenv("KI_DEBUG_PANIC_IN", "")
			_, wantOut, _ := runCLI(t, "emit", target, path)
			t.Setenv("KI_DEBUG_PANIC_IN", "render")
			code, stdout, stderr := runCLI(t, "emit", target, path)
			checkICE(t, "ki emit "+target+" with the hook in render", code, "", stderr, "render")
			if stdout != wantOut || stdout == "" {
				t.Errorf("ki emit %s with the hook in render: stdout %q, want the dump %q", target, stdout, wantOut)
			}
		}

		// What the passes before the one that panicked had found is not
		// written: the report is all there is on stderr, and its last line says
		// how many diagnostics it leaves out. The file has a syntax error, which
		// the parser reports to the sink before the resolver panics. Without the
		// hook that error is written, so the file does have something to drop.
		bad := writeKi(t, "fn main( {}\n")
		t.Setenv("KI_DEBUG_PANIC_IN", "")
		if code, stdout, stderr := runCLI(t, "check", bad); code != 1 || stdout != "" || !strings.HasPrefix(stderr, "error[E0201]: ") {
			t.Fatalf("ki check of a file with a syntax error: exit code %d, stdout %q, stderr %q, want 1, nothing and an error[E0201]", code, stdout, stderr)
		}
		t.Setenv("KI_DEBUG_PANIC_IN", "resolve")
		code, stdout, stderr = runCLI(t, "check", bad)
		checkICE(t, "ki check of a file with a syntax error, with the hook in resolve", code, stdout, stderr, "resolve")
		if strings.Contains(stderr, "error[") {
			t.Errorf("ki check of a file with a syntax error, with the hook in resolve: stderr %q has a diagnostic, want only the report of the ICE", stderr)
		}
		for _, row := range []struct{ name, source, want string }{
			{"one", "fn main( {}\n", "note: 1 diagnostic was reported before this error and is not shown\n"},
			{"two", "fn f(x {}\nfn g(y {}\n", "note: 2 diagnostics were reported before this error and are not shown\n"},
			// The sink keeps the first 100 and only knows that there were more.
			{"past_the_limit", strings.Repeat("fn f( {}\n", 150), "note: more than 100 diagnostics were reported before this error and are not shown\n"},
		} {
			code, stdout, stderr := runCLI(t, "check", writeKi(t, row.source))
			checkICE(t, "the pending diagnostics, "+row.name, code, stdout, stderr, "resolve")
			if !strings.HasSuffix(stderr, row.want) {
				t.Errorf("%s: stderr %q does not end in %q", row.name, stderr, row.want)
			}
			if strings.Count(stderr, "reported before this error") != 1 {
				t.Errorf("%s: stderr %q does not say it once", row.name, stderr)
			}
		}
		// With nothing pending, the report says nothing about it.
		code, _, stderr = runCLI(t, "check", path)
		checkICE(t, "ki check of a valid file with the hook in resolve", code, "", stderr, "resolve")
		if strings.Contains(stderr, "reported before this error") {
			t.Errorf("ki check of a valid file: stderr %q speaks of diagnostics, and there are none", stderr)
		}
	})

	t.Run("debug_hook_names_one_pass", func(t *testing.T) {
		path := writeKi(t, "fn main() {}\n")
		t.Setenv("KI_DEBUG_PANIC_IN", "")
		_, wantVersion, _ := runCLI(t, "version")

		// Something that is not the exact name of a pass does nothing.
		for _, name := range []string{"", "bogus", "Check", "chec", "checks", "check,lex"} {
			t.Setenv("KI_DEBUG_PANIC_IN", name)
			if code, stdout, stderr := runCLI(t, "check", path); code != 0 || stdout != "" || stderr != "" {
				t.Errorf("ki check with KI_DEBUG_PANIC_IN=%q: exit code %d, stdout %q, stderr %q, want 0 and nothing written", name, code, stdout, stderr)
			}
		}
		// Nor does a pass that the command does not run: version runs none, and
		// emit tokens stops after the lexer.
		t.Setenv("KI_DEBUG_PANIC_IN", "check")
		if code, stdout, stderr := runCLI(t, "version"); code != 0 || stdout != wantVersion || stderr != "" {
			t.Errorf("ki version with the hook in check: exit code %d, stdout %q, stderr %q, want 0, %q and nothing on stderr", code, stdout, stderr, wantVersion)
		}
		if code, stdout, stderr := runCLI(t, "emit", "tokens", path); code != 0 || stdout == "" || stderr != "" {
			t.Errorf("ki emit tokens with the hook in check: exit code %d, stdout %q, stderr %q, want 0, the tokens and nothing on stderr", code, stdout, stderr)
		}
		// The passes after the checker run only where Core is made: neither
		// ki check nor ki emit ast gets as far as elaborating.
		for _, pass := range []string{"elaborate", "lint"} {
			t.Setenv("KI_DEBUG_PANIC_IN", pass)
			if code, stdout, stderr := runCLI(t, "check", path); code != 0 || stdout != "" || stderr != "" {
				t.Errorf("ki check with the hook in %s: exit code %d, stdout %q, stderr %q, want 0 and nothing written", pass, code, stdout, stderr)
			}
			if code, stdout, stderr := runCLI(t, "emit", "ast", path); code != 0 || stdout == "" || stderr != "" {
				t.Errorf("ki emit ast with the hook in %s: exit code %d, stdout %q, stderr %q, want 0, the tree and nothing on stderr", pass, code, stdout, stderr)
			}
		}
	})

	t.Run("run_pass_unwinds_with_the_ice", func(t *testing.T) {
		ran := false
		if r := raised(func() { newSession("").runPass("parse", func() { ran = true }) }); r != nil || !ran {
			t.Errorf("a pass that returns: runPass panicked with %v (ran %v), want it to return after running the pass", r, ran)
		}

		// A pass that panics ends the command: runPass panics with the *ICE.
		r := raised(func() { newSession("").runPass("parse", func() { panic("boom") }) })
		if ice, ok := r.(*ICE); !ok || ice.Pass != "parse" || ice.Value != "boom" {
			t.Errorf("a pass that panics: runPass panicked with %#v, want the *ICE of pass \"parse\" and value \"boom\"", r)
		}

		// The hook panics when the pass starts, so the pass itself never runs.
		ran = false
		r = raised(func() { newSession("parse").runPass("parse", func() { ran = true }) })
		if ice, ok := r.(*ICE); !ok || ice.Pass != "parse" || ice.Value != "debug panic requested" {
			t.Errorf("the hook: runPass panicked with %#v, want the *ICE of pass \"parse\" and value \"debug panic requested\"", r)
		}
		if ran {
			t.Errorf("the hook is set to panic on entry, but the pass ran")
		}

		// A label in another file makes RenderHuman panic. The commands write
		// diagnostics as the pass render, so that bug of a pass is an ICE, with
		// exit code 70 and the message of the renderer, and not Go's own report.
		old := commands
		commands = append(slices.Clone(old), command{
			Name: "badlabel", Usage: "ki badlabel",
			Run: func(s *Session, _ []string) int {
				a := s.Sources.Add("a.ki", []byte("x\n"))
				b := s.Sources.Add("b.ki", []byte("y\n"))
				s.Sink.Report(diag.NewError(diag.E(101), "an error", diag.Label{Span: base.Span{File: a, Lo: 0, Hi: 1}}).
					WithSecondary(diag.Label{Span: base.Span{File: b, Lo: 0, Hi: 1}, Message: "here"}))
				s.report()
				return 1
			},
		})
		t.Cleanup(func() { commands = old })
		var out, errOut strings.Builder
		code := Main([]string{"badlabel"}, strings.NewReader(""), &out, &errOut)
		if code != 70 || out.Len() != 0 {
			t.Errorf("a label in another file: exit code %d, stdout %q, want 70 and nothing", code, out.String())
		}
		if want := "internal compiler error: render: diag: secondary label in file 1, primary in file 0\ngoroutine "; !strings.HasPrefix(errOut.String(), want) {
			t.Errorf("a label in another file: stderr %q does not start with %q", errOut.String(), want)
		}
		if want := "note: 1 diagnostic was reported before this error and is not shown\n"; !strings.HasSuffix(errOut.String(), want) {
			t.Errorf("a label in another file: stderr %q does not end in %q", errOut.String(), want)
		}
	})

	t.Run("lint_violation_is_ice", func(t *testing.T) {
		// program returns a Program with unbound functions, each of which reads
		// a variable that nothing binds: the functions are f0, f1… and their
		// variables x0, x1…, spelled by in. With 0 it returns a valid Program,
		// with one function that is `()`.
		program := func(in *base.Interner, unbound int) *core.Program {
			b := &core.Builder{P: &core.Program{}}
			for i := 0; i < unbound; i++ {
				v := b.P.Vars.Push(core.Var{Name: in.InternString("x" + strconv.Itoa(i))})
				b.P.Funs.Push(core.Fun{Name: in.InternString("f" + strconv.Itoa(i)), Body: b.VarRef(base.Span{}, v), HasBody: true, FrameSize: 1})
			}
			if unbound == 0 {
				b.P.Funs.Push(core.Fun{Name: in.InternString("f0"), Body: b.LitUnit(base.Span{}), HasBody: true})
			}
			return b.P
		}

		// The path of a real violation: Lint finds it, lintOrPanic panics with
		// it and guard turns the panic into the ICE of the pass lint, whose
		// stack goes through lintOrPanic.
		in := base.NewInterner()
		ice := guard("lint", func() { lintOrPanic(program(in, 1), in) })
		if ice == nil {
			t.Fatal("guard returned nil for a Program with an unbound variable")
		}
		if want := "internal compiler error: lint: fun f0: unbound variable `x0`"; ice.Error() != want {
			t.Errorf("Error() = %q, want %q", ice.Error(), want)
		}
		if !strings.HasPrefix(ice.Error(), "internal compiler error: lint: fun ") || !strings.Contains(ice.Error(), "unbound") {
			t.Errorf("Error() = %q, want it to start with \"internal compiler error: lint: fun \" and contain \"unbound\"", ice.Error())
		}
		if !strings.Contains(string(ice.Stack), "lintOrPanic") {
			t.Errorf("the stack does not go through lintOrPanic: %q", ice.Stack)
		}

		// Every violation is in the report, one for each line.
		ice = guard("lint", func() { lintOrPanic(program(in, 2), in) })
		if want := "internal compiler error: lint: fun f0: unbound variable `x0`\nfun f1: unbound variable `x1`"; ice == nil || ice.Error() != want {
			t.Errorf("a Program with two violations: guard returned %v, want the ICE %q", ice, want)
		}

		// A valid Program is not a report.
		if ice := guard("lint", func() { lintOrPanic(program(in, 0), in) }); ice != nil {
			t.Errorf("lintOrPanic of a valid Program made the ICE %q, want none", ice.Error())
		}

		// The Session's pass wraps it the same way, and so unwinds the command:
		// the panic is the *ICE.
		s := newSession("")
		r := raised(func() { s.lint(program(s.Interner, 1)) })
		if ice, ok := r.(*ICE); !ok || ice.Pass != "lint" || ice.Value != "fun f0: unbound variable `x0`" {
			t.Errorf("Session.lint of a Program with an unbound variable panicked with %#v, want the *ICE of pass \"lint\" with the violation as value", r)
		}
		if r := raised(func() { s.lint(program(s.Interner, 0)) }); r != nil {
			t.Errorf("Session.lint of a valid Program panicked with %#v, want it to return", r)
		}
	})

	t.Run("other_panics_are_not_reports", func(t *testing.T) {
		// A command that panics outside a pass: not an ICE, so Main raises it
		// again as it was and writes nothing.
		old := commands
		commands = append(slices.Clone(old), command{
			Name: "explode", Usage: "ki explode",
			Run: func(*Session, []string) int { panic("not a pass") },
		})
		t.Cleanup(func() { commands = old })

		var out, errOut strings.Builder
		var r any
		func() {
			defer func() { r = recover() }()
			Main([]string{"explode"}, strings.NewReader(""), &out, &errOut)
		}()
		if r != "not a pass" {
			t.Errorf("Main panicked with %#v, want the value \"not a pass\"", r)
		}
		if out.Len() != 0 || errOut.Len() != 0 {
			t.Errorf("Main wrote stdout %q and stderr %q, want nothing", out.String(), errOut.String())
		}
	})
}
