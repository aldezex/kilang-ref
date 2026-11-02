package driver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/core/coretest"
	"github.com/aldezex/kilang/internal/diag"
)

// runUsage is the usage line of `ki run`.
const runUsage = "usage: ki run [--backend=interp] [--diagnostics=human|short|json] FILE\n"

// Two lines of the diagnostic E0303, the same in every format that has them.
const (
	noMainNote = "`ki run` starts the program at `fn main()`; `ki check` does not need one"
	noMainHelp = "add `fn main() { … }`"
)

func TestRun(t *testing.T) {
	t.Run("empty_file_reports_E0303", func(t *testing.T) {
		path := writeKi(t, "")
		code, stdout, stderr := runCLI(t, "run", path)
		// The whole diagnostic, so that the line of source is seen to be
		// exactly "1 |": the file has no text, and nothing may follow the bar.
		want := "error[E0303]: no `main` function\n" +
			" --> " + path + ":1:1\n" +
			"  |\n" +
			"1 |\n" +
			"  | ^ this file has no `fn main()`\n" +
			"  = note: " + noMainNote + "\n" +
			"  = help: " + noMainHelp + "\n"
		if code != 1 || stdout != "" || stderr != want {
			t.Errorf("ki run of an empty file: got %d, stdout %q, stderr %q, want 1, nothing on stdout and stderr %q", code, stdout, stderr, want)
		}
	})

	t.Run("missing_main_in_each_format", func(t *testing.T) {
		path := writeKi(t, "fn helper() -> Int { 1 }\n")
		rows := []struct {
			name, format, want string
		}{
			{"human", "human", "error[E0303]: no `main` function\n" +
				" --> " + path + ":1:1\n" +
				"  |\n" +
				"1 | fn helper() -> Int { 1 }\n" +
				"  | ^ this file has no `fn main()`\n" +
				"  = note: " + noMainNote + "\n" +
				"  = help: " + noMainHelp + "\n"},
			{"short", "short", path + ":1:1: error[E0303]: no `main` function\n"},
			// No fix-it: where `main` goes, and what it does, is not for the
			// compiler to say.
			{"json", "json", `{"severity":"error","code":"E0303","message":"no ` + "`main`" + ` function","path":"` + path + `","line":1,"col":1,` +
				`"labels":[{"primary":true,"line":1,"col":1,"message":"this file has no ` + "`fn main()`" + `"}],` +
				`"notes":["` + noMainNote + `"],"help":"` + noMainHelp + `","fixits":[]}` + "\n"},
		}
		for _, row := range rows {
			code, stdout, stderr := runCLI(t, "run", "--diagnostics="+row.format, path)
			if code != 1 || stdout != "" || stderr != row.want {
				t.Errorf("%s: got %d, stdout %q, stderr %q, want 1, nothing on stdout and stderr %q", row.name, code, stdout, stderr, row.want)
			}
		}
	})

	t.Run("missing_main_is_at_the_start_of_the_user_file", func(t *testing.T) {
		// What the three formats show of the span, a path, a line and a column,
		// would be the same for an empty span and for one that holds a character.
		// So the diagnostic is read from the session itself: one error, whose
		// primary span is the empty one at the start of the user's file, and not
		// at the prelude's, which is also in the session. The file is checked
		// here as well as in the text of the formats (missing_main_in_each_format).
		path := writeKi(t, "fn helper() -> Int { 1 }\n")
		var stdout, stderr strings.Builder
		s := NewSession(strings.NewReader(""), &stdout, &stderr)
		var run *command
		for i := range commands {
			if commands[i].Name == "run" {
				run = &commands[i]
			}
		}
		if run == nil {
			t.Fatal("the table of commands has no run")
		}
		code := run.Run(s, []string{path})
		diags := s.Sink.All()
		if code != 1 || len(diags) != 1 {
			t.Fatalf("ki run of a file with no main: got exit code %d and %d diagnostics, want 1 and exactly 1", code, len(diags))
		}
		d := diags[0]
		if d.Code != diag.E(303) {
			t.Errorf("the diagnostic has the code %v, want E0303", d.Code)
		}
		span := d.Primary.Span
		if span.Lo != 0 || span.Hi != 0 {
			t.Errorf("the primary span is [%d, %d), want the empty span [0, 0)", span.Lo, span.Hi)
		}
		if got := s.Sources.DisplayPath(span.File); got != path {
			t.Errorf("the primary span is in the file %q, want the user's file %q", got, path)
		}
	})

	t.Run("front_end_errors_stop_before_main_is_checked", func(t *testing.T) {
		// Errors in the file come first and are all that is written: the file may
		// have a `main`, which the machine must not start, or none, which is not
		// reported on top of the errors.
		rows := []struct {
			name, src, want string
		}{
			{"error_and_main", "fn main() { print(true) }\n", ":1:19: error[E0401]: mismatched types\n"},
			{"error_and_no_main", "fn f() -> Int { true }\n", ":1:17: error[E0401]: mismatched types\n"},
		}
		for _, row := range rows {
			path := writeKi(t, row.src)
			code, stdout, stderr := runCLI(t, "run", "--diagnostics=short", path)
			if want := path + row.want; code != 1 || stdout != "" || stderr != want {
				t.Errorf("%s: got %d, stdout %q, stderr %q, want 1, nothing on stdout and stderr %q", row.name, code, stdout, stderr, want)
			}
		}
	})

	t.Run("unreadable_file_exits_1", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "run", "/nonexistent/x.ki")
		const prefix = "error: cannot read file `/nonexistent/x.ki`: "
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, prefix) || strings.Count(stderr, "\n") != 1 {
			t.Errorf("got %d, stdout %q, stderr %q, want 1, nothing on stdout and one line on stderr that starts with %q", code, stdout, stderr, prefix)
		}
	})

	t.Run("exit_code_is_the_one_the_program_ends_with", func(t *testing.T) {
		rows := []struct {
			name, src string
			code      int
			stdout    string
			stderrFmt string // stderr, with the path of the file where %s goes
		}{
			{"returns", "fn main() { print(7) }\n", 0, "7\n", ""},
			// A panic keeps what the program printed before it, and writes its
			// message to stderr: the two streams do not mix.
			{"overflow_panics", "fn main() {\n  print(1)\n  print(9223372036854775807 + 1)\n}\n", 101, "1\n", "panic: integer overflow at %s:3:29\n"},
			{"division_by_zero_panics", "fn main() {\n  print(1)\n  print(1 / 0)\n}\n", 101, "1\n", "panic: division by zero at %s:3:11\n"},
		}
		for _, row := range rows {
			path := writeKi(t, row.src)
			wantErr := row.stderrFmt
			if strings.Contains(wantErr, "%s") {
				wantErr = fmt.Sprintf(wantErr, path)
			}
			code, stdout, stderr := runCLI(t, "run", path)
			if code != row.code || stdout != row.stdout || stderr != wantErr {
				t.Errorf("%s: got %d, stdout %q, stderr %q, want %d, %q and %q", row.name, code, stdout, stderr, row.code, row.stdout, wantErr)
			}
		}
	})

	t.Run("unknown_backend_is_usage_error", func(t *testing.T) {
		path := writeKi(t, "fn main() { print(7) }\n")
		const unknownVM = "ki run: unknown backend \"vm\"\n" + runUsage
		rows := []struct {
			name   string
			args   []string
			code   int
			stdout string
			stderr string
		}{
			{"vm", []string{"run", "--backend=vm", path}, 2, "", unknownVM},
			// The value is checked whether or not FILE is there.
			{"vm_without_a_file", []string{"run", "--backend=vm"}, 2, "", unknownVM},
			{"empty", []string{"run", "--backend=", path}, 2, "", "ki run: unknown backend \"\"\n" + runUsage},
			{"capitalized", []string{"run", "--backend=Interp", path}, 2, "", "ki run: unknown backend \"Interp\"\n" + runUsage},
			// --diagnostics is checked first.
			{"vm_and_bad_diagnostics", []string{"run", "--backend=vm", "--diagnostics=xml", path}, 2, "",
				"ki run: invalid --diagnostics value \"xml\" (want human, short or json)\n" + runUsage},
			// The one backend there is, named, runs the program.
			{"interp", []string{"run", "--backend=interp", path}, 0, "7\n", ""},
		}
		for _, row := range rows {
			code, stdout, stderr := runCLI(t, row.args...)
			if code != row.code || stdout != row.stdout || stderr != row.stderr {
				t.Errorf("%s: got %d, stdout %q, stderr %q, want %d, %q and %q", row.name, code, stdout, stderr, row.code, row.stdout, row.stderr)
			}
		}
	})

	t.Run("usage_errors", func(t *testing.T) {
		rows := []struct {
			name   string
			args   []string
			code   int
			stdout string
			stderr string
		}{
			{"no_file", []string{"run"}, 2, "", "ki run: missing FILE\n" + runUsage},
			{"no_file_after_a_flag", []string{"run", "--backend=interp"}, 2, "", "ki run: missing FILE\n" + runUsage},
			{"two_files", []string{"run", "a.ki", "b.ki"}, 2, "", "ki run: unexpected argument \"b.ki\"\n" + runUsage},
			{"unknown_flag", []string{"run", "--speed=fast", "a.ki"}, 2, "", "ki run: flag provided but not defined: -speed\n" + runUsage},
			{"help", []string{"run", "-h"}, 0, runUsage, ""},
		}
		for _, row := range rows {
			code, stdout, stderr := runCLI(t, row.args...)
			if code != row.code || stdout != row.stdout || stderr != row.stderr {
				t.Errorf("%s: got %d, stdout %q, stderr %q, want %d, %q and %q", row.name, code, stdout, stderr, row.code, row.stdout, row.stderr)
			}
		}
	})

	t.Run("stack_overflow_exits_103", func(t *testing.T) {
		t.Setenv("KI_STACK_MB", "1")
		path := writeKi(t, "fn f(n: Int) -> Int { 1 + f(n + 1) }\nfn main() { let x = f(0) }\n")
		code, stdout, stderr := runCLI(t, "run", path)
		// The overflow is at the call of f in its own body, `f(n + 1)`, which
		// starts at column 27 of line 1.
		if want := "panic: stack overflow at " + path + ":1:27\n"; code != 103 || stdout != "" || stderr != want {
			t.Errorf("got %d, stdout %q, stderr %q, want 103, nothing on stdout and stderr %q", code, stdout, stderr, want)
		}
	})

	t.Run("debug_hook_exits_70_in_each_pass", func(t *testing.T) {
		path := writeKi(t, "fn main() { print(7) }\n")
		// The control: without the hook the program runs, so that each row below
		// ends in an ICE because of the hook and not for another reason.
		t.Setenv("KI_DEBUG_PANIC_IN", "")
		if code, stdout, stderr := runCLI(t, "run", path); code != 0 || stdout != "7\n" || stderr != "" {
			t.Fatalf("ki run without the hook: got %d, stdout %q, stderr %q, want 0, \"7\\n\" and nothing on stderr", code, stdout, stderr)
		}
		// Every pass that `ki run` runs, from the lexer to the machine itself.
		for _, pass := range []string{"lex", "parse", "resolve", "check", "elaborate", "lint", "run"} {
			t.Setenv("KI_DEBUG_PANIC_IN", pass)
			code, stdout, stderr := runCLI(t, "run", path)
			checkICE(t, "ki run with the hook in "+pass, code, stdout, stderr, pass)
		}
	})

	t.Run("go_panic_in_the_machine_is_an_ice", func(t *testing.T) {
		// A program that Lint accepts (TestExterns checks it) but whose `main`
		// calls an extern that the interpreter does not have: only a bug of the
		// compiler can make one.
		// The machine panics, and execute turns that into the ICE of the pass
		// "run", as it must for Main to report it with exit code 70.
		r := raised(func() { newSession("").execute(coretest.ExternProgram("nope")) })
		if ice, ok := r.(*ICE); !ok || ice.Pass != "run" || ice.Value != "unknown extern nope" {
			t.Errorf("execute of a program with an unknown extern panicked with %#v, want the *ICE of pass \"run\" and value \"unknown extern nope\"", r)
		}
		// The control: with the extern that exists, the same program ends with 0
		// and prints.
		var stdout strings.Builder
		s := newSession("")
		s.Stdout = &stdout
		if code := s.execute(coretest.ExternProgram("ki_print_int")); code != 0 || stdout.String() != "9\n" {
			t.Errorf("execute of the control program: got %d and stdout %q, want 0 and \"9\\n\"", code, stdout.String())
		}
	})
}
