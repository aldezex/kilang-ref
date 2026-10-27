package kitest_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/kitest"
)

// writeFiles creates in dir the files of the map (name -> contents).
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("cannot write %s: %v", name, err)
		}
	}
}

// call is one run of the fake Exec.
type call struct {
	args []string
	cwd  string
}

// fakeExec returns an Exec that records each call and answers with what
// answer returns for its arguments.
func fakeExec(calls *[]call, answer func(args []string) (exit int, stdout, stderr string)) func([]string, io.Writer, io.Writer) int {
	return func(args []string, stdout, stderr io.Writer) int {
		cwd, _ := os.Getwd()
		*calls = append(*calls, call{args: args, cwd: cwd})
		exit, out, errOut := answer(args)
		_, _ = io.WriteString(stdout, out)
		_, _ = io.WriteString(stderr, errOut)
		return exit
	}
}

// flushRecorder is an io.Writer with a Flush method. It keeps what was written
// so far at each Flush, to check what the runner had written by then.
type flushRecorder struct {
	buf       strings.Builder
	snapshots []string
}

func (w *flushRecorder) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *flushRecorder) Flush() error {
	w.snapshots = append(w.snapshots, w.buf.String())
	return nil
}

// sameDir reports whether a and b are the same directory, whatever symlinks
// (such as /tmp) lead to it.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ia, err := os.Stat(a)
	if err != nil {
		t.Fatalf("cannot stat %s: %v", a, err)
	}
	ib, err := os.Stat(b)
	if err != nil {
		t.Fatalf("cannot stat %s: %v", b, err)
	}
	return os.SameFile(ia, ib)
}

func TestRunner(t *testing.T) {
	t.Run("pass_and_fail_lines", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"b.ki":      "// test: T00.02\n// run: check\n// expect-stdout: 1\n",
			"a.ki":      "// test: T00.01\n// run: check\n// args: --diagnostics=short\n// expect-stdout: 1\n",
			"notes.txt": "not a Ki file, so it never runs",
		})
		var calls []call
		exec := fakeExec(&calls, func(args []string) (int, string, string) {
			if args[len(args)-1] == "a.ki" {
				return 0, "1\n", ""
			}
			return 0, "2\n", ""
		})
		var out strings.Builder
		r := &kitest.Runner{Root: dir, Exec: exec, Out: &out}
		passed, failed := r.RunPaths([]string{"."})
		if passed != 1 || failed != 1 {
			t.Errorf("got %d passed and %d failed, want 1 and 1", passed, failed)
		}
		want := "PASS a.ki\nFAIL b.ki\n- 1\n+ 2\nki test: 1 passed, 1 failed\n"
		if out.String() != want {
			t.Errorf("output %q, want %q", out.String(), want)
		}
		if len(calls) != 2 ||
			strings.Join(calls[0].args, " ") != "check --diagnostics=short a.ki" ||
			strings.Join(calls[1].args, " ") != "check b.ki" {
			t.Errorf("the runner executed %+v, want `check --diagnostics=short a.ki` and then `check b.ki`", calls)
		}
	})
	t.Run("exit_code_diff_first", func(t *testing.T) {
		const head = "// test: T00.03\n// run: check\n"
		tests := []struct {
			name, header string // header: the lines after `test` and `run`
			exit         int
			stdout       string
			stderr       string
			want         string
		}{
			{"exit code first, then stdout", "// expect-stdout: ok\n", 1, "bad\n", "",
				"FAIL c.ki\n- exit 0\n+ exit 1\n- ok\n+ bad\n"},
			{"expected exit code from the header", "// expect-exit: 101\n", 0, "", "",
				"FAIL c.ki\n- exit 101\n+ exit 0\n"},
			{"only the first differing line", "// expect-stdout:\n// | a\n// | b\n// | c\n", 0, "a\nB\nC\n", "",
				"FAIL c.ki\n- b\n+ B\n"},
			{"stdout ends first", "// expect-stdout:\n// | a\n// | b\n", 0, "a\n", "",
				"FAIL c.ki\n- b\n+ <eof>\n"},
			{"stdout goes on", "// expect-stdout: a\n", 0, "a\nb\n", "",
				"FAIL c.ki\n- <eof>\n+ b\n"},
			{"output not expected at all", "", 0, "extra\n", "",
				"FAIL c.ki\n- <eof>\n+ extra\n"},
			{"last line without newline", "// expect-stdout: ok\n", 0, "ok", "",
				"FAIL c.ki\n- ok\n+ ok [no newline at end]\n"},
			{"stderr after stdout", "// expect-stdout: o\n// expect-stderr: e\n", 0, "O\n", "E\n",
				"FAIL c.ki\n- o\n+ O\n- e\n+ E\n"},
			{"stderr alone", "// expect-stderr: e\n", 0, "", "",
				"FAIL c.ki\n- e\n+ <eof>\n"},
			{"everything equal", "// expect-stdout: o\n// expect-stderr: e\n// expect-exit: 3\n", 3, "o\n", "e\n",
				"PASS c.ki\n"},
		}
		for _, tc := range tests {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{"c.ki": head + tc.header})
			var calls []call
			r := &kitest.Runner{Root: dir, Out: io.Discard, Exec: fakeExec(&calls, func([]string) (int, string, string) {
				return tc.exit, tc.stdout, tc.stderr
			})}
			ok, report := r.RunFile("c.ki")
			if report != tc.want || ok != (tc.want == "PASS c.ki\n") {
				t.Errorf("%s: got ok=%v and %q, want %q", tc.name, ok, report, tc.want)
			}
		}
	})
	t.Run("header_error_is_fail", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"d.ki": "// test: T00.04\nfn main() {}\n"})
		var calls []call
		var out strings.Builder
		r := &kitest.Runner{Root: dir, Out: &out, Exec: fakeExec(&calls, func([]string) (int, string, string) { return 0, "", "" })}
		passed, failed := r.RunPaths([]string{"d.ki"})
		want := "FAIL d.ki\n- malformed header: missing key 'run'\nki test: 0 passed, 1 failed\n"
		if out.String() != want || passed != 0 || failed != 1 {
			t.Errorf("got %d passed, %d failed and %q, want 0, 1 and %q", passed, failed, out.String(), want)
		}
		if len(calls) != 0 {
			t.Errorf("the runner executed %d commands for a file with a malformed header", len(calls))
		}
	})
	t.Run("annotations_compare_positions", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"e.ki": "// test: T00.05\n// run: check\nlet x = 1  // ERROR(E0101) @9\n",
			"f.ki": "// test: T00.06\n// run: check\nlet x = 1  // ERROR(E0101) @9  // ERROR(E0101) @9\n",
			"w.ki": "// test: T00.13\n// run: check\nlet x = 1  // ERROR(W0702) @9\n",
			"m.ki": "// test: T00.14\n// run: check\nlet x = 1  // ERROR(W0702) @9\nlet y = 2  // ERROR(E0101) @9\n",
		})
		diagJSON := func(code string, line, col int) string {
			return `{"severity":"error","code":"` + code + `","message":"m","path":"e.ki","line":` +
				strconv.Itoa(line) + `,"col":` + strconv.Itoa(col) + `,"labels":[],"notes":[],"help":null}` + "\n"
		}
		// run answers the first pass (the exit code and stdout of an erroneous
		// program, with stderr text that must be ignored) and the second pass
		// (json lines on stderr) of a file.
		run := func(exit int, jsonLines string) (*kitest.Runner, *[]call) {
			calls := new([]call)
			exec := fakeExec(calls, func(args []string) (int, string, string) {
				for _, a := range args {
					if a == "--diagnostics=json" {
						return exit, "", jsonLines
					}
				}
				return exit, "", "free text, never compared\n"
			})
			return &kitest.Runner{Root: dir, Out: io.Discard, Exec: exec}, calls
		}

		r, calls := run(1, diagJSON("E0101", 3, 9))
		ok, report := r.RunFile("e.ki")
		if want := "PASS e.ki\n"; !ok || report != want {
			t.Errorf("same position: got ok=%v and %q, want true and %q", ok, report, want)
		}
		if len(*calls) != 2 ||
			strings.Join((*calls)[0].args, " ") != "check e.ki" ||
			strings.Join((*calls)[1].args, " ") != "check --diagnostics=json e.ki" {
			t.Errorf("the runner executed %+v, want `check e.ki` and then `check --diagnostics=json e.ki`", *calls)
		}

		r, _ = run(1, diagJSON("E0101", 4, 1))
		ok, report = r.RunFile("e.ki")
		if want := "FAIL e.ki\nmissing E0101 at 3:9\nunexpected E0101 at 4:1\n"; ok || report != want {
			t.Errorf("other position: got ok=%v and %q, want false and %q", ok, report, want)
		}

		// Two annotations with the same code and position are two
		// diagnostics, not one: the comparison is of multisets.
		r, _ = run(1, diagJSON("E0101", 3, 9))
		ok, report = r.RunFile("f.ki")
		if want := "FAIL f.ki\nmissing E0101 at 3:9\n"; ok || report != want {
			t.Errorf("one diagnostic for two annotations: got ok=%v and %q, want false and %q", ok, report, want)
		}

		// Without an `expect-exit` the exit code is 1 when some annotation
		// is an error (its code starts with E) and 0 when all are warnings.
		exitRows := []struct {
			name, file string
			exit       int
			json, want string
		}{
			{"warning only, exit 0", "w.ki", 0, diagJSON("W0702", 3, 9), "PASS w.ki\n"},
			{"warning only, exit 1", "w.ki", 1, diagJSON("W0702", 3, 9), "FAIL w.ki\n- exit 0\n+ exit 1\n"},
			{"error, exit 0", "e.ki", 0, diagJSON("E0101", 3, 9), "FAIL e.ki\n- exit 1\n+ exit 0\n"},
			// One error among warnings is enough, wherever it is.
			{"warning then error, exit 1", "m.ki", 1, diagJSON("W0702", 3, 9) + diagJSON("E0101", 4, 9), "PASS m.ki\n"},
			{"warning then error, exit 0", "m.ki", 0, diagJSON("W0702", 3, 9) + diagJSON("E0101", 4, 9), "FAIL m.ki\n- exit 1\n+ exit 0\n"},
		}
		for _, row := range exitRows {
			r, _ := run(row.exit, row.json)
			if ok, report := r.RunFile(row.file); report != row.want || ok != (row.want[0] == 'P') {
				t.Errorf("%s: got ok=%v and %q, want %q", row.name, ok, report, row.want)
			}
		}

		// Only the lines that start with '{' are diagnostics: the note that
		// ki prints after 100 errors is skipped, and a line that starts with
		// '{' but is not a diagnostic is reported, before the differences of
		// the annotations.
		const note = "note: too many errors; the first 100 are shown\n"
		lineRows := []struct{ name, json, want string }{
			{"note before the diagnostic", note + diagJSON("E0101", 3, 9), "PASS e.ki\n"},
			{"blank lines", "\n" + diagJSON("E0101", 3, 9) + "\n", "PASS e.ki\n"},
			{"not JSON", "{not json}\n", "FAIL e.ki\n+ unreadable diagnostic: {not json}\nmissing E0101 at 3:9\n"},
			{"not JSON next to the diagnostic", "{not json}\n" + diagJSON("E0101", 3, 9), "FAIL e.ki\n+ unreadable diagnostic: {not json}\n"},
		}
		for _, row := range lineRows {
			r, _ := run(1, row.json)
			if ok, report := r.RunFile("e.ki"); report != row.want || ok != (row.want[0] == 'P') {
				t.Errorf("%s: got ok=%v and %q, want %q", row.name, ok, report, row.want)
			}
		}
	})
	t.Run("restores_cwd", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"g.ki": "// test: T00.07\n// run: check\n"})
		before, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd: %v", err)
		}
		var calls []call
		r := &kitest.Runner{Root: dir, Out: io.Discard, Exec: fakeExec(&calls, func([]string) (int, string, string) { return 0, "", "" })}
		r.RunPaths([]string{"g.ki"})
		after, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd: %v", err)
		}
		if after != before {
			t.Errorf("the working directory is %q after RunPaths, was %q", after, before)
		}
		if len(calls) != 1 || !sameDir(t, calls[0].cwd, dir) {
			t.Errorf("the command ran in %+v, want the root %q", calls, dir)
		}
	})
	t.Run("flushes_after_each_line", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"a.ki": "// test: T00.08\n// run: check\n",
			"b.ki": "// test: T00.09\n// run: check\n// expect-stdout: x\n",
		})
		var calls []call
		out := &flushRecorder{}
		r := &kitest.Runner{Root: dir, Out: out, Exec: fakeExec(&calls, func([]string) (int, string, string) { return 0, "", "" })}
		r.RunPaths([]string{"."})
		// The third flush comes after the summary, so a buffered Out has the
		// result line too.
		want := []string{"PASS a.ki\n", "PASS a.ki\nFAIL b.ki\n", "PASS a.ki\nFAIL b.ki\n- x\n+ <eof>\nki test: 1 passed, 1 failed\n"}
		if len(out.snapshots) != len(want) {
			t.Fatalf("Flush was called %d times, want %d: %q", len(out.snapshots), len(want), out.snapshots)
		}
		for i := range want {
			if out.snapshots[i] != want[i] {
				t.Errorf("at flush %d the runner had written %q, want %q", i+1, out.snapshots[i], want[i])
			}
		}

		// The summary is flushed when there is no file to run too: here the
		// root does not exist.
		out = &flushRecorder{}
		r = &kitest.Runner{Root: filepath.Join(dir, "missing"), Out: out, Exec: r.Exec}
		r.RunPaths([]string{"."})
		if len(out.snapshots) != 1 || !strings.HasSuffix(out.snapshots[0], "\nki test: 0 passed, 1 failed\n") {
			t.Errorf("without a root: Flush snapshots %q, want one that ends with the summary", out.snapshots)
		}
	})
	t.Run("path_without_golden_files_fails", func(t *testing.T) {
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{
			"only.txt": "not a Ki file",
			"a.ki":     "// test: T00.10\n// run: check\n",
		})
		if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
			t.Fatalf("cannot create a directory: %v", err)
		}
		var calls []call
		var out strings.Builder
		r := &kitest.Runner{Root: dir, Out: &out, Exec: fakeExec(&calls, func([]string) (int, string, string) { return 0, "", "" })}
		// The empty directory is a failure, and the path after it still runs.
		passed, failed := r.RunPaths([]string{"empty", "a.ki"})
		want := "FAIL empty\n- no .ki file found below this directory\nPASS a.ki\nki test: 1 passed, 1 failed\n"
		if out.String() != want || passed != 1 || failed != 1 {
			t.Errorf("empty directory: got %d passed, %d failed and %q, want 1, 1 and %q", passed, failed, out.String(), want)
		}

		// Files that are not .ki do not count either.
		out.Reset()
		if err := os.Remove(filepath.Join(dir, "a.ki")); err != nil {
			t.Fatalf("cannot remove a file: %v", err)
		}
		passed, failed = r.RunPaths([]string{"."})
		want = "FAIL .\n- no .ki file found below this directory\nki test: 0 passed, 1 failed\n"
		if out.String() != want || passed != 0 || failed != 1 {
			t.Errorf("directory of other files: got %d passed, %d failed and %q, want 0, 1 and %q", passed, failed, out.String(), want)
		}
	})
	t.Run("unreadable_directory_fails", func(t *testing.T) {
		dir := t.TempDir()
		locked := filepath.Join(dir, "locked")
		if err := os.Mkdir(locked, 0o755); err != nil {
			t.Fatalf("cannot create a directory: %v", err)
		}
		writeFiles(t, dir, map[string]string{"a.ki": "// test: T00.11\n// run: check\n"})
		writeFiles(t, locked, map[string]string{"b.ki": "// test: T00.12\n// run: check\n"})
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatalf("cannot lock the directory: %v", err)
		}
		// Put the permissions back, so that the temporary directory can be removed.
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		if _, err := os.ReadDir(locked); err == nil {
			t.Skip("this system lets the test read a directory without permissions (Windows, or root)")
		}
		var calls []call
		var out strings.Builder
		r := &kitest.Runner{Root: dir, Out: &out, Exec: fakeExec(&calls, func([]string) (int, string, string) { return 0, "", "" })}
		passed, failed := r.RunPaths([]string{"."})
		want := "PASS a.ki\nFAIL locked\n- cannot read directory: permission denied\nki test: 1 passed, 1 failed\n"
		if out.String() != want || passed != 1 || failed != 1 {
			t.Errorf("got %d passed, %d failed and %q, want 1, 1 and %q", passed, failed, out.String(), want)
		}
	})
	t.Run("files_run_in_path_order", func(t *testing.T) {
		// WalkDir visits "a" before "a-b" (the shorter name first), but as
		// paths "a-b/x.ki" sorts before "a/y.ki", because '-' comes before '/'.
		dir := t.TempDir()
		for _, sub := range []string{"a", "a-b"} {
			if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
				t.Fatalf("cannot create a directory: %v", err)
			}
		}
		writeFiles(t, filepath.Join(dir, "a"), map[string]string{"y.ki": "// test: T00.15\n// run: check\n"})
		writeFiles(t, filepath.Join(dir, "a-b"), map[string]string{"x.ki": "// test: T00.16\n// run: check\n"})
		var calls []call
		var out strings.Builder
		r := &kitest.Runner{Root: dir, Out: &out, Exec: fakeExec(&calls, func([]string) (int, string, string) { return 0, "", "" })}
		passed, failed := r.RunPaths([]string{"."})
		want := "PASS a-b/x.ki\nPASS a/y.ki\nki test: 2 passed, 0 failed\n"
		if out.String() != want || passed != 2 || failed != 0 {
			t.Errorf("got %d passed, %d failed and %q, want 2, 0 and %q", passed, failed, out.String(), want)
		}
	})
	t.Run("subprocess_exec_reports_start_failure", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code := kitest.ExecSubprocess("/nonexistent/ki")([]string{"check", "x.ki"}, &out, &errOut)
		// The reason comes from the operating system, so its text is not pinned.
		const prefix = "ki test: cannot run /nonexistent/ki: "
		if code != -1 || out.Len() != 0 || !strings.HasPrefix(errOut.String(), prefix) ||
			len(errOut.String()) == len(prefix)+1 || strings.Count(errOut.String(), "\n") != 1 || !strings.HasSuffix(errOut.String(), "\n") {
			t.Errorf("got %d, stdout %q and stderr %q, want -1, nothing and one line that starts with %q and gives a reason", code, out.String(), errOut.String(), prefix)
		}

		// A program that dies of a signal has no exit code either, and that is
		// not a failure to start it: nothing is added to stderr.
		if runtime.GOOS == "windows" {
			t.Skip("needs /bin/sh and signals")
		}
		out.Reset()
		errOut.Reset()
		code = kitest.ExecSubprocess("/bin/sh")([]string{"-c", "kill -9 $$"}, &out, &errOut)
		if code != -1 || out.Len() != 0 || errOut.Len() != 0 {
			t.Errorf("a killed program: got %d, stdout %q and stderr %q, want -1 and nothing", code, out.String(), errOut.String())
		}
	})
	t.Run("subprocess_exec_reports_exit_code", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("needs /bin/sh")
		}
		var out, errOut bytes.Buffer
		code := kitest.ExecSubprocess("/bin/sh")([]string{"-c", "echo out; echo err >&2; exit 7"}, &out, &errOut)
		if code != 7 || out.String() != "out\n" || errOut.String() != "err\n" {
			t.Errorf("got %d %q %q, want 7 \"out\\n\" \"err\\n\"", code, out.String(), errOut.String())
		}
	})
}
