package driver

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// usagePrefix is how every usage error starts. The list after "commands: "
// grows as ki gains commands, so the other usage tests check only this prefix
// and the number of lines; usage_lists_commands alone pins the whole list.
const usagePrefix = "usage: ki <command> [args]\ncommands: "

// runCLI runs Main in-process with args and returns its exit code and what it
// wrote to stdout and stderr. stdin is empty.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut strings.Builder
	code = Main(args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

// checkUsageError fails the test unless the result is a usage error: exit code
// 2, nothing on stdout, and on stderr exactly two lines, each ending in a
// newline, that start with usagePrefix.
func checkUsageError(t *testing.T, code int, stdout, stderr string) {
	t.Helper()
	if code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, usagePrefix) {
		t.Errorf("stderr %q does not start with %q", stderr, usagePrefix)
	}
	if strings.Count(stderr, "\n") != 2 || !strings.HasSuffix(stderr, "\n") {
		t.Errorf("stderr %q is not exactly two lines ending in a newline", stderr)
	}
}

// processStderr runs f with os.Stderr, the stderr of the test process itself,
// pointed at a temporary file, and returns what f wrote there. runCLI captures
// only the writers it hands to Main; this catches output that bypasses them,
// such as a flag.FlagSet with no output of its own, which writes to os.Stderr.
func processStderr(t *testing.T, f func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("cannot create a file to stand in for stderr: %v", err)
	}
	defer file.Close()
	saved := os.Stderr
	os.Stderr = file
	defer func() { os.Stderr = saved }()
	f()
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("cannot read back what went to stderr: %v", err)
	}
	return string(data)
}

// failingWriter is an io.Writer whose every Write fails with err and writes
// nothing, like a full disk.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// flushFailingWriter is an io.Writer whose writes work and whose Flush fails
// with err, like a buffered output whose last bytes cannot be written.
type flushFailingWriter struct{ err error }

func (w flushFailingWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w flushFailingWriter) Flush() error                { return w.err }

// runGoldensInProcess makes `ki test` run each golden file by calling Main in
// this process, because under `go test` the executable is the test binary and
// not ki (see testExec). The change lasts until the end of the test.
func runGoldensInProcess(t *testing.T) {
	t.Helper()
	testExec = func(args []string, stdout, stderr io.Writer) int {
		return Main(args, strings.NewReader(""), stdout, stderr)
	}
	t.Cleanup(func() { testExec = nil })
}

// goldenDir returns a directory with a golden file that passes (pass.ki) and
// one that fails (fail.ki).
func goldenDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"pass.ki": "// test: T00.01\n// run: check\nfn main() {}\n",
		"fail.ki": "// test: T00.02\n// run: check\n// expect-stdout: nope\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatalf("cannot write %s: %v", name, err)
		}
	}
	return dir
}

func TestCLI(t *testing.T) {
	t.Run("version_prints_version_and_go", func(t *testing.T) {
		code, out, err := runCLI(t, "--version")
		want := "ki " + Version + " (" + runtime.Version() + ")\n"
		if code != 0 || out != want || err != "" {
			t.Fatalf("got %d %q %q, want 0 %q \"\"", code, out, err, want)
		}
	})
	t.Run("no_args_is_usage_error", func(t *testing.T) {
		code, out, err := runCLI(t)
		checkUsageError(t, code, out, err)
	})
	t.Run("unknown_command_is_usage_error", func(t *testing.T) {
		code, out, err := runCLI(t, "frobnicate")
		checkUsageError(t, code, out, err)
	})
	t.Run("usage_lists_commands", func(t *testing.T) {
		code, out, err := runCLI(t)
		want := "usage: ki <command> [args]\ncommands: --version, version, check, test, emit\n"
		if code != 2 || out != "" || err != want {
			t.Fatalf("got %d %q %q, want 2 \"\" %q", code, out, err, want)
		}
	})
	t.Run("version_command", func(t *testing.T) {
		code, out, err := runCLI(t, "version")
		_, flagOut, _ := runCLI(t, "--version")
		want := "ki " + Version + " (" + runtime.Version() + ")\n"
		if code != 0 || out != want || err != "" {
			t.Fatalf("got %d %q %q, want 0 %q \"\"", code, out, err, want)
		}
		if out != flagOut {
			t.Fatalf("version printed %q, but --version printed %q", out, flagOut)
		}
	})
	t.Run("version_rejects_unknown_flag", func(t *testing.T) {
		var code int
		var out, err string
		leaked := processStderr(t, func() { code, out, err = runCLI(t, "version", "-x") })
		want := "ki version: flag provided but not defined: -x\nusage: ki version\n"
		if code != 2 || out != "" || err != want {
			t.Fatalf("got %d %q %q, want 2 \"\" %q", code, out, err, want)
		}
		if leaked != "" {
			t.Fatalf("Main wrote %q to the process's own stderr, want nothing there", leaked)
		}
	})
	t.Run("version_flag_rejects_extra_args", func(t *testing.T) {
		// Both spellings share runVersion, and each message must name the
		// spelling that was typed.
		tests := []struct{ name, cmd, want string }{
			{"flag", "--version", "ki --version: unexpected argument \"extra\"\nusage: ki --version\n"},
			{"command", "version", "ki version: unexpected argument \"extra\"\nusage: ki version\n"},
		}
		for _, tc := range tests {
			code, out, err := runCLI(t, tc.cmd, "extra")
			if code != 2 || out != "" || err != tc.want {
				t.Errorf("%s: got %d %q %q, want 2 \"\" %q", tc.name, code, out, err, tc.want)
			}
		}
	})
	t.Run("version_help_prints_usage", func(t *testing.T) {
		// Asking for help is not a mistake in the command line: the answer is
		// the usage line on stdout and exit code 0, and it names the command
		// as it was typed.
		tests := []struct {
			name string
			args []string
			want string
		}{
			{"flag", []string{"--version", "-h"}, "usage: ki --version\n"},
			{"command", []string{"version", "--help"}, "usage: ki version\n"},
		}
		for _, tc := range tests {
			code, out, err := runCLI(t, tc.args...)
			if code != 0 || out != tc.want || err != "" {
				t.Errorf("%s: got %d %q %q, want 0 %q \"\"", tc.name, code, out, err, tc.want)
			}
		}
	})
	t.Run("version_reports_write_error", func(t *testing.T) {
		tests := []struct{ name, cmd, want string }{
			{"flag", "--version", "ki --version: cannot write to stdout: disk full\n"},
			{"command", "version", "ki version: cannot write to stdout: disk full\n"},
		}
		for _, tc := range tests {
			var errOut strings.Builder
			stdout := failingWriter{errors.New("disk full")}
			code := Main([]string{tc.cmd}, strings.NewReader(""), stdout, &errOut)
			if code != 1 || errOut.String() != tc.want {
				t.Errorf("%s: got %d %q, want 1 %q", tc.name, code, errOut.String(), tc.want)
			}
		}
	})
	t.Run("check_missing_file", func(t *testing.T) {
		code, out, err := runCLI(t, "check", "/nonexistent/x.ki")
		const prefix = "error: cannot read file `/nonexistent/x.ki`: "
		if code != 1 || out != "" || !strings.HasPrefix(err, prefix) || strings.Count(err, "\n") != 1 {
			t.Fatalf("got %d %q %q, want 1 \"\" and one line that starts with %q", code, out, err, prefix)
		}
	})
	t.Run("check_bad_diagnostics_flag", func(t *testing.T) {
		code, out, err := runCLI(t, "check", "--diagnostics=xml", "x.ki")
		want := "ki check: invalid --diagnostics value \"xml\" (want human, short or json)\n" +
			"usage: ki check [--diagnostics=human|short|json] FILE\n"
		if code != 2 || out != "" || err != want {
			t.Fatalf("got %d %q %q, want 2 \"\" %q", code, out, err, want)
		}
	})
	t.Run("check_usage_errors", func(t *testing.T) {
		const usage = "usage: ki check [--diagnostics=human|short|json] FILE\n"
		tests := []struct {
			name string
			args []string
			want string
		}{
			{"no file", []string{"check"}, "ki check: missing FILE\n" + usage},
			{"no file after a flag", []string{"check", "--diagnostics=short"}, "ki check: missing FILE\n" + usage},
			{"two files", []string{"check", "a.ki", "b.ki"}, "ki check: unexpected argument \"b.ki\"\n" + usage},
			{"three files", []string{"check", "a.ki", "b.ki", "c.ki"}, "ki check: unexpected argument \"b.ki\"\n" + usage},
		}
		for _, tc := range tests {
			code, out, err := runCLI(t, tc.args...)
			if code != 2 || out != "" || err != tc.want {
				t.Errorf("%s: got %d %q %q, want 2 \"\" %q", tc.name, code, out, err, tc.want)
			}
		}
	})
	t.Run("check_short_format", func(t *testing.T) {
		// A control byte is the one error that a file with no other problem
		// can have. The path is shown as it was typed.
		path := filepath.Join(t.TempDir(), "c.ki")
		if err := os.WriteFile(path, []byte("let x = 1\x01\n"), 0o644); err != nil {
			t.Fatalf("cannot write the file: %v", err)
		}
		code, out, err := runCLI(t, "check", "--diagnostics=short", path)
		want := path + ":1:10: error[E0102]: control character U+0001 in source\n"
		if code != 1 || out != "" || err != want {
			t.Errorf("got %d %q %q, want 1 \"\" %q", code, out, err, want)
		}
	})
	t.Run("check_reports_E0101_once", func(t *testing.T) {
		// Lex runs CheckSource itself, so a command that called both would
		// report the same byte twice. The code is counted with its `error[`
		// prefix because the temporary directory is named after this test, and
		// its path, which the report shows as typed, also contains "E0101".
		path := filepath.Join(t.TempDir(), "x.ki")
		if err := os.WriteFile(path, []byte("let x = \xff"), 0o644); err != nil {
			t.Fatalf("cannot write the file: %v", err)
		}
		code, out, err := runCLI(t, "check", path)
		if code != 1 || out != "" || strings.Count(err, "error[E0101]") != 1 {
			t.Errorf("check: got %d %q %q, want 1, nothing on stdout and error[E0101] exactly once on stderr", code, out, err)
		}
		// emit tokens lexes the file once as well. The file is refused, so the
		// dump holds only the Eof, after the last byte (line 1, column 10).
		code, out, err = runCLI(t, "emit", "tokens", path)
		if code != 1 || out != "1:10 Eof\n" || strings.Count(err, "error[E0101]") != 1 {
			t.Errorf("emit tokens: got %d %q %q, want 1, \"1:10 Eof\\n\" and error[E0101] exactly once on stderr", code, out, err)
		}
	})
	t.Run("emit_unknown_target", func(t *testing.T) {
		const usage = "usage: ki emit tokens|ast [--diagnostics=human|short|json] FILE\n"
		tests := []struct {
			name string
			args []string
			code int
			out  string // what stdout gets
			err  string // what stderr gets
		}{
			{"unknown target", []string{"emit", "bogus", "x.ki"}, 2, "", "ki emit: unknown target `bogus`\n" + usage},
			// The flags come after the target, so one before it is not a target.
			{"flag before the target", []string{"emit", "--diagnostics=short", "tokens", "x.ki"}, 2, "", "ki emit: unknown target `--diagnostics=short`\n" + usage},
			{"no target", []string{"emit"}, 2, "", "ki emit: missing target\n" + usage},
			// Asking for help is not a mistake, not even where the target goes:
			// the answer is the usage line on stdout and exit code 0, the same as
			// `ki emit tokens -h` gives.
			{"-h", []string{"emit", "-h"}, 0, usage, ""},
			{"-help", []string{"emit", "-help"}, 0, usage, ""},
			{"--help", []string{"emit", "--help"}, 0, usage, ""},
			// And where the flags go, after the target. Both commands read their
			// words with the same function, which has to hand the answer to
			// help back as it is: the usage line and exit code 0.
			{"tokens -h", []string{"emit", "tokens", "-h"}, 0, usage, ""},
			{"check -h", []string{"check", "-h"}, 0, "usage: ki check [--diagnostics=human|short|json] FILE\n", ""},
		}
		for _, tc := range tests {
			code, out, err := runCLI(t, tc.args...)
			if code != tc.code || out != tc.out || err != tc.err {
				t.Errorf("%s: got %d %q %q, want %d %q %q", tc.name, code, out, err, tc.code, tc.out, tc.err)
			}
		}
	})
	t.Run("emit_ast", func(t *testing.T) {
		write := func(src string) string {
			t.Helper()
			path := filepath.Join(t.TempDir(), "a.ki")
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatalf("cannot write the file: %v", err)
			}
			return path
		}
		// A correct file: the tree on stdout, one line per item, and nothing on
		// stderr.
		path := write("pub fn id(x: Int) -> Int { x }\nfn main() {}\n")
		code, out, err := runCLI(t, "emit", "ast", path)
		want := "(pub fn id (params (x Int)) (ret Int) (block x))\n(fn main (params) (ret _) (block))\n"
		if code != 0 || out != want || err != "" {
			t.Errorf("correct file: got %d %q %q, want 0 %q \"\"", code, out, err, want)
		}
		// A file with a syntax error: the tree of what the parser understood,
		// with an (error) where it did not, on stdout, and then the diagnostic on
		// stderr, and exit code 1 only after both are written.
		path = write("fn f() -> Int { 1 + }\n")
		code, out, err = runCLI(t, "emit", "ast", "--diagnostics=short", path)
		want = "(fn f (params) (ret Int) (block (+ 1 (error))))\n"
		wantErr := path + ":1:21: error[E0201]: expected expression, found `}`\n"
		if code != 1 || out != want || err != wantErr {
			t.Errorf("syntax error: got %d %q %q, want 1 %q %q", code, out, err, want, wantErr)
		}
		// A lexical error and a syntax error in different functions: Parse runs
		// after a lexer that reported, so both functions are in the tree, with an
		// (error) where each could not be read, and both diagnostics come out, in
		// the order of the file. A command that skipped the parser when the lexer
		// had reported would print an empty tree and only the lexer's diagnostic.
		path = write("fn a() { let x = $ }\nfn b() -> Int { 1 + }\n")
		code, out, err = runCLI(t, "emit", "ast", "--diagnostics=short", path)
		want = "(fn a (params) (ret _) (block (let x _ (error))))\n(fn b (params) (ret Int) (block (+ 1 (error))))\n"
		wantErr = path + ":1:18: error[E0104]: unexpected character `$`\n" + path + ":2:21: error[E0201]: expected expression, found `}`\n"
		if code != 1 || out != want || err != wantErr {
			t.Errorf("lexical and syntax errors: got %d %q %q, want 1 %q %q", code, out, err, want, wantErr)
		}
		// A file with an invalid UTF-8 byte (E0101): Lex checks the bytes and
		// refuses it, and a command that checked the source as well would report
		// it twice. The tree of a refused file is empty, so stdout gets nothing,
		// and the exit code is 1. The code is counted with its `error[` prefix
		// for the reason given in check_reports_E0101_once.
		path = write("let x = \xff")
		code, out, err = runCLI(t, "emit", "ast", path)
		if code != 1 || out != "" || strings.Count(err, "error[E0101]") != 1 {
			t.Errorf("refused file: got %d %q %q, want 1, nothing on stdout and error[E0101] exactly once on stderr", code, out, err)
		}
		// The usage errors are those of emit tokens, with the line of both.
		code, out, err = runCLI(t, "emit", "ast")
		wantErr = "ki emit: missing FILE\nusage: ki emit tokens|ast [--diagnostics=human|short|json] FILE\n"
		if code != 2 || out != "" || err != wantErr {
			t.Errorf("no file: got %d %q %q, want 2 \"\" %q", code, out, err, wantErr)
		}
		code, out, err = runCLI(t, "emit", "ast", "-h")
		if wantOut := "usage: ki emit tokens|ast [--diagnostics=human|short|json] FILE\n"; code != 0 || out != wantOut || err != "" {
			t.Errorf("-h: got %d %q %q, want 0 %q \"\"", code, out, err, wantOut)
		}
	})
	t.Run("emit_dump_before_diagnostics", func(t *testing.T) {
		// Both targets end through finishEmit: the dump on stdout, then the
		// diagnostics on stderr. runCLI keeps the two streams in separate
		// builders, so it cannot show the order. Each file is run twice: once
		// with separate writers, to see what each stream gets, and once with
		// one writer for both, as on a terminal or with `2>&1`, where the dump
		// has to come first.
		tests := []struct{ name, target, src string }{
			{"emit tokens", "tokens", "let x = 1\x01\n"},
			{"emit ast", "ast", "fn f() -> Int { 1 + }\n"},
		}
		for _, tc := range tests {
			path := filepath.Join(t.TempDir(), "a.ki")
			if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
				t.Fatalf("cannot write the file: %v", err)
			}
			code, out, errOut := runCLI(t, "emit", tc.target, path)
			if code != 1 || out == "" || errOut == "" {
				t.Errorf("%s, separate writers: got %d %q %q, want 1 and something on each stream", tc.name, code, out, errOut)
				continue
			}
			var both strings.Builder
			code = Main([]string{"emit", tc.target, path}, strings.NewReader(""), &both, &both)
			if want := out + errOut; code != 1 || both.String() != want {
				t.Errorf("%s, one writer for both: got %d %q, want 1 %q (the dump, then the diagnostics)", tc.name, code, both.String(), want)
			}
		}
	})
	t.Run("test_exit_code_and_summary", func(t *testing.T) {
		runGoldensInProcess(t)
		const failReport = "FAIL fail.ki\n- nope\n+ <eof>\n"
		tests := []struct {
			name  string
			flags []string
			paths []string
			code  int
			want  string
		}{
			{"one file that passes", nil, []string{"pass.ki"}, 0, "PASS pass.ki\nki test: 1 passed, 0 failed\n"},
			{"one file that fails", nil, []string{"fail.ki"}, 1, failReport + "ki test: 0 passed, 1 failed\n"},
			{"the failing file is the last PATH", nil, []string{"pass.ki", "fail.ki"}, 1, "PASS pass.ki\n" + failReport + "ki test: 1 passed, 1 failed\n"},
			{"the failing file is the first PATH", nil, []string{"fail.ki", "pass.ki"}, 1, failReport + "PASS pass.ki\nki test: 1 passed, 1 failed\n"},
			// --update rewrites the stale expect-stdout of fail.ki, and then it passes.
			{"--update fixes the stale file", []string{"--update"}, []string{"fail.ki", "pass.ki"}, 0, "PASS fail.ki\nPASS pass.ki\nki test: 2 passed, 0 failed\n"},
		}
		for _, tc := range tests {
			// Each case has files of its own: --update changes them.
			dir := goldenDir(t)
			// --root is the only way to find the files: the working directory
			// of this test is the package directory.
			args := append([]string{"test", "--root", dir}, tc.flags...)
			code, out, err := runCLI(t, append(args, tc.paths...)...)
			if code != tc.code || out != tc.want || err != "" {
				t.Errorf("%s: got %d %q %q, want %d %q \"\"", tc.name, code, out, err, tc.code, tc.want)
			}
		}
	})
	t.Run("test_reports_write_error", func(t *testing.T) {
		runGoldensInProcess(t)
		dir := goldenDir(t)
		// The files pass, so only the lost report can make the exit code 1.
		var errOut strings.Builder
		stdout := failingWriter{errors.New("disk full")}
		code := Main([]string{"test", "--root", dir, "pass.ki"}, strings.NewReader(""), stdout, &errOut)
		if want := "ki test: cannot write to stdout: disk full\n"; code != 1 || errOut.String() != want {
			t.Errorf("got %d %q, want 1 %q", code, errOut.String(), want)
		}
	})
	t.Run("test_reports_flush_error", func(t *testing.T) {
		runGoldensInProcess(t)
		dir := goldenDir(t)
		// The runner flushes the report after each line and ignores the error;
		// ki test remembers it and exits with 1, as for a failed Write.
		var errOut strings.Builder
		stdout := flushFailingWriter{errors.New("cannot flush")}
		code := Main([]string{"test", "--root", dir, "pass.ki"}, strings.NewReader(""), stdout, &errOut)
		if want := "ki test: cannot write to stdout: cannot flush\n"; code != 1 || errOut.String() != want {
			t.Errorf("got %d %q, want 1 %q", code, errOut.String(), want)
		}
	})
	t.Run("test_without_path_is_usage_error", func(t *testing.T) {
		code, out, err := runCLI(t, "test")
		want := "ki test: missing PATH\nusage: ki test [--root DIR] [--update] PATH...\n"
		if code != 2 || out != "" || err != want {
			t.Fatalf("got %d %q %q, want 2 \"\" %q", code, out, err, want)
		}
	})
}
