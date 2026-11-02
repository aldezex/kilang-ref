package driver

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

func TestICE(t *testing.T) {
	t.Run("debug_hook_exits_70", func(t *testing.T) {
		path := writeKi(t, "fn main() {}\n")

		t.Setenv("KI_DEBUG_PANIC_IN", "check")
		code, stdout, stderr := runCLI(t, "check", path)
		checkICE(t, "ki check with the hook in check", code, stdout, stderr, "check")

		// The hook fires in the pass it names, in each command that runs the
		// pass (check in ki check was the run above): the lexer and the parser
		// also in emit.
		for _, row := range []struct {
			pass string
			args []string
		}{
			{"lex", []string{"check", path}},
			{"parse", []string{"check", path}},
			{"resolve", []string{"check", path}},
			{"lex", []string{"emit", "tokens", path}},
			{"lex", []string{"emit", "ast", path}},
			{"parse", []string{"emit", "ast", path}},
		} {
			t.Setenv("KI_DEBUG_PANIC_IN", row.pass)
			code, stdout, stderr := runCLI(t, row.args...)
			checkICE(t, "ki "+strings.Join(row.args[:len(row.args)-1], " ")+" with the hook in "+row.pass, code, stdout, stderr, row.pass)
		}

		// What the passes before the one that panicked had found is not
		// written: the report is all there is on stderr. The file has a syntax
		// error, which the parser reports to the sink before the resolver
		// panics. Without the hook that error is written, so the file does
		// have something to drop.
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
	})

	t.Run("run_pass_unwinds_with_the_ice", func(t *testing.T) {
		// raised runs f and returns what it panicked with, or nil.
		raised := func(f func()) (r any) {
			defer func() { r = recover() }()
			f()
			return nil
		}
		newSession := func(debugPanicIn string) *Session {
			s := NewSession(strings.NewReader(""), io.Discard, io.Discard)
			s.DebugPanicIn = debugPanicIn
			return s
		}

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
