package kitest

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Runner runs golden files and compares what they print with what their
// headers expect.
type Runner struct {
	// Root is the directory the golden files are relative to. The runner works
	// inside it while it runs, and every path it shows is relative to it. The
	// empty string means the working directory, ".".
	Root string

	// Exec runs ki with args (the words after "ki") and writes what ki prints
	// to stdout and stderr; it returns the exit code. tests/golden_test.go
	// passes a function that calls driver.Main in-process, and `ki test` one
	// that starts the real binary (see ExecSubprocess).
	Exec func(args []string, stdout, stderr io.Writer) int

	// Out receives the progress: a PASS or FAIL line for each file and, at the
	// end, the summary line. After each PASS or FAIL line, and once more after
	// the summary, the runner calls Out.Flush if Out has that method: a file
	// that hangs or crashes ki cannot hide the lines before it, and a buffered
	// Out does not lose the result because its owner forgot to flush it.
	Out io.Writer

	// Update makes the runner rewrite the expect-stdout and the expect-stderr of
	// each file with what the run printed, and then judge the file as it is
	// afterwards. It never changes the expect-exit or the annotations, so a file
	// can still fail on those: a key inserted above an annotation moves its line
	// down, and an absolute `@LINE:COL` keeps the line it had, while a relative
	// `@COL` goes with its line. See updateFile for the form of the keys.
	//
	// A run that did not exit (Exec gave a negative code: the program could not
	// start, or died of a signal) is not recorded, because what it printed is
	// not the output of the program; the file stays as it is and fails on the
	// exit code.
	Update bool
}

// root returns the directory the runner works in.
func (r *Runner) root() string {
	if r.Root == "" {
		return "."
	}
	return r.Root
}

// enterRoot makes Root the working directory and returns its absolute path
// and a function that goes back to the previous directory.
func (r *Runner) enterRoot() (absRoot string, restore func(), err error) {
	absRoot, err = filepath.Abs(r.root())
	if err != nil {
		return "", nil, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	if err := os.Chdir(absRoot); err != nil {
		return "", nil, err
	}
	return absRoot, func() {
		// If this fails the process is left in Root, and there is nobody to tell.
		_ = os.Chdir(wd)
	}, nil
}

// RunFile runs one golden file and compares the result with its header. path
// is relative to Root, or absolute and inside it. The report starts with the
// line `PASS <path>` or `FAIL <path>`, with the path relative to Root. A
// failing report goes on with the differences, one line each, in this order
// and only the ones that exist:
//
//   - the exit code: `- exit 0` (expected) and `+ exit 1` (real);
//   - the first line of stdout that differs: `- <expected>` and `+ <real>`,
//     where `<eof>` stands for an output that ended before the other;
//   - the same for stderr, unless the file has annotations;
//   - the annotations, in order of position and without a prefix:
//     `missing E0101 at 3:9` for one the real run did not report,
//     `unexpected E0101 at 4:1` for one that the file lacks.
//
// A malformed header is a failure whose only difference is
// `- malformed header: …`.
//
// With Update the file is rewritten first (see updateFile) and, if that changed
// it, run again: the lines it added or removed move the code, and an output that
// names a line must be the one of the file as it now is. The report is about
// that second run. A file that cannot be written fails with
// `- cannot write file: …`. A run that did not exit is not recorded (see
// Runner.Update).
func (r *Runner) RunFile(path string) (ok bool, report string) {
	absRoot, restore, err := r.enterRoot()
	if err != nil {
		return false, failReport(path, fmt.Sprintf("- cannot enter the root `%s`: %v", r.root(), err))
	}
	defer restore()
	return r.runHere(absRoot, path)
}

// runHere runs one file with Root as the working directory.
func (r *Runner) runHere(absRoot, path string) (ok bool, report string) {
	rel := displayPath(absRoot, path)
	data, err := os.ReadFile(path)
	if err != nil {
		return false, failReport(rel, fmt.Sprintf("- cannot read file: %v", reason(err)))
	}
	spec, err := ParseHeader(data)
	if err != nil {
		return false, failReport(rel, "- "+err.Error())
	}

	command := append(slices.Clone(spec.Run), spec.Args...)
	var stdout, stderr bytes.Buffer
	run := func() int {
		stdout.Reset()
		stderr.Reset()
		return r.Exec(append(slices.Clone(command), rel), &stdout, &stderr)
	}
	exit := run()
	if r.Update && exit >= 0 {
		updated, changed, err := updateFile(path, data, spec, stdout.String(), stderr.String())
		if err != nil {
			return false, failReport(rel, "- "+err.Error())
		}
		if changed {
			spec = updated
			exit = run()
		}
	}

	wantExit := 0
	switch {
	case spec.ExpectExit != nil:
		wantExit = *spec.ExpectExit
	case slices.ContainsFunc(spec.Errors, func(a Annotation) bool { return a.Code[0] == 'E' }):
		wantExit = 1
	}
	var diffs []string
	if exit != wantExit {
		diffs = append(diffs, fmt.Sprintf("- exit %d", wantExit), fmt.Sprintf("+ exit %d", exit))
	}
	diffs = append(diffs, firstDifference(deref(spec.ExpectStdout), stdout.String())...)
	if len(spec.Errors) == 0 {
		diffs = append(diffs, firstDifference(deref(spec.ExpectStderr), stderr.String())...)
	} else {
		// The text of the errors is not compared: their codes and positions
		// are. Those are read from a second run that prints them as JSON.
		var jsonOut, jsonErr bytes.Buffer
		r.Exec(append(slices.Clone(command), "--diagnostics=json", rel), &jsonOut, &jsonErr)
		diffs = append(diffs, annotationDifferences(spec.Errors, jsonErr.String())...)
	}

	if len(diffs) == 0 {
		return true, "PASS " + rel + "\n"
	}
	return false, failReport(rel, diffs...)
}

// failReport builds the report of a failure: the line `FAIL <rel>` and each
// difference on a line of its own, exactly as the caller wrote it (with its
// "- " or "+ " when it has one: the annotation lines have none).
func failReport(rel string, diffs ...string) string {
	var sb strings.Builder
	sb.WriteString("FAIL " + rel + "\n")
	for _, d := range diffs {
		sb.WriteString(d + "\n")
	}
	return sb.String()
}

// reason returns the error without the *fs.PathError around it, so that a
// message does not repeat the path, which the report already shows relative to
// the root, and does not show the absolute one.
func reason(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// displayPath returns path relative to the root, with forward slashes. The
// working directory is the root while the runner works, so a relative path is
// already relative to it.
func displayPath(absRoot, path string) string {
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(absRoot, path); err == nil {
			path = rel
		}
	}
	return filepath.ToSlash(filepath.Clean(path))
}

// deref returns the text a pointer to an expected output holds: empty if the
// header gave none.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// firstDifference compares two outputs line by line and describes the first
// line where they differ: "- <expected>" and "+ <real>". When one output ends
// before the other, its side is "<eof>". It returns nothing if they are equal.
func firstDifference(want, got string) []string {
	w, g := splitLines(want), splitLines(got)
	for i := 0; i < max(len(w), len(g)); i++ {
		switch {
		case i >= len(w):
			return []string{"- <eof>", "+ " + showLine(g[i])}
		case i >= len(g):
			return []string{"- " + showLine(w[i]), "+ <eof>"}
		case w[i] != g[i]:
			return []string{"- " + showLine(w[i]), "+ " + showLine(g[i])}
		}
	}
	return nil
}

// splitLines cuts text into lines that keep their "\n", so that a last line
// without one differs from the same line with it.
func splitLines(text string) []string {
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// showLine returns a line without its "\n". A line that had none, the last of
// an output, says so, because otherwise it would look equal to the same line
// with its "\n".
func showLine(line string) string {
	if text, ok := strings.CutSuffix(line, "\n"); ok {
		return text
	}
	return line + " [no newline at end]"
}

// annotationDifferences compares the annotations of a file with the
// diagnostics in the JSON lines that ki printed on stderr. Both are treated as
// multisets of (code, line, column): an annotation written twice needs two
// diagnostics. A line of stderr that does not start with '{' is not a
// diagnostic (the note that ReportAll adds after 100 errors, for one) and is
// skipped. The differences are the bare lines `missing E0101 at 3:9` and
// `unexpected E0101 at 4:1`, by position; a line that starts with '{' but is
// not a diagnostic is the one exception, `+ unreadable diagnostic: …`.
func annotationDifferences(want []Annotation, stderr string) []string {
	var got []Annotation
	var diffs []string
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var d struct {
			Code      string `json:"code"`
			Line, Col uint32
		}
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			diffs = append(diffs, "+ unreadable diagnostic: "+line)
			continue
		}
		got = append(got, Annotation{Code: d.Code, Line: d.Line, Col: d.Col})
	}

	byPosition := func(a, b Annotation) int {
		return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Col, b.Col), cmp.Compare(a.Code, b.Code))
	}
	want = slices.SortedFunc(slices.Values(want), byPosition)
	slices.SortFunc(got, byPosition)
	// Both lists are sorted by the same key, so walking them together pairs
	// equal elements and leaves the rest, already in position order.
	i, j := 0, 0
	for i < len(want) || j < len(got) {
		switch {
		case j >= len(got) || (i < len(want) && byPosition(want[i], got[j]) < 0):
			diffs = append(diffs, fmt.Sprintf("missing %s at %d:%d", want[i].Code, want[i].Line, want[i].Col))
			i++
		case i >= len(want) || byPosition(want[i], got[j]) > 0:
			diffs = append(diffs, fmt.Sprintf("unexpected %s at %d:%d", got[j].Code, got[j].Line, got[j].Col))
			j++
		default:
			i++
			j++
		}
	}
	return diffs
}

// RunPaths runs every golden file under each path: a path that is a directory
// stands for the .ki files below it, sorted by path, and any other path for
// itself. Paths are relative to Root, which is the working directory until the
// method returns. It writes the report of each file to Out (flushing after
// each PASS or FAIL line) and then `ki test: <P> passed, <F> failed`, which it
// flushes too.
//
// A directory counts as failed, and its report is `FAIL <path>` and one
// difference, when it holds no .ki file at all (a script with an empty
// variable would otherwise take "nothing ran" for "everything passed") and,
// for each subdirectory that cannot be read, when it cannot be read. The
// files that were found still run, and their reports come first.
//
// Writing to Out is best effort: a failing Out does not stop the run, and
// RunPaths does not report it. A caller that must know wraps Out in a writer
// that remembers its first error (see runTest).
func (r *Runner) RunPaths(paths []string) (passed, failed int) {
	out := r.Out
	if out == nil {
		out = io.Discard
	}
	flush := func() {
		if f, ok := out.(interface{ Flush() error }); ok {
			_ = f.Flush()
		}
	}
	defer func() {
		_, _ = fmt.Fprintf(out, "ki test: %d passed, %d failed\n", passed, failed)
		flush()
	}()

	absRoot, restore, err := r.enterRoot()
	if err != nil {
		_, _ = io.WriteString(out, failReport(r.root(), fmt.Sprintf("- cannot enter the root: %v", err)))
		return 0, 1
	}
	defer restore()

	// write sends a report to Out. The first line goes out and is flushed
	// before the differences, so the flush always comes right after a PASS or
	// FAIL line.
	write := func(report string) {
		head, rest, _ := strings.Cut(report, "\n")
		_, _ = io.WriteString(out, head+"\n")
		flush()
		_, _ = io.WriteString(out, rest)
	}
	for _, p := range paths {
		files, problems := kiFiles(absRoot, p)
		for _, file := range files {
			ok, report := r.runHere(absRoot, file)
			if ok {
				passed++
			} else {
				failed++
			}
			write(report)
		}
		for _, report := range problems {
			failed++
			write(report)
		}
	}
	return passed, failed
}

// kiFiles returns the files a path stands for: the .ki files below it, sorted
// by path, if it is a directory, and the path itself otherwise (a path that
// does not exist too, so that running it reports the error). It also returns
// the reports of the failures found on the way: a directory that holds no .ki
// file, and every subdirectory that cannot be read. Each is a complete report,
// `FAIL <path>` and its difference.
func kiFiles(absRoot, path string) (files, problems []string) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return []string{path}, nil
	}
	// The function always returns nil, so WalkDir goes on after an error and
	// the error it returns would be nil: the errors it hands to the function
	// are the only ones.
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			problems = append(problems, unreadableDirectory(absRoot, p, err))
		case !d.IsDir() && filepath.Ext(p) == ".ki":
			files = append(files, p)
		}
		return nil
	})
	if len(files) == 0 && len(problems) == 0 {
		problems = append(problems, failReport(displayPath(absRoot, path), "- no .ki file found below this directory"))
	}
	// WalkDir visits the entries of each directory in lexical order, which is
	// not the order of the full paths ("a-b/x.ki" comes after "a/y.ki" there
	// but before it when sorted as strings), so sort the paths themselves.
	slices.Sort(files)
	return files, problems
}

// unreadableDirectory returns the report of a directory that filepath.WalkDir
// could not read: `FAIL <path>` and the reason, without the path that the
// error repeats. p is the path WalkDir gave, below absRoot.
func unreadableDirectory(absRoot, p string, err error) string {
	return failReport(displayPath(absRoot, p), fmt.Sprintf("- cannot read directory: %v", reason(err)))
}

// ExecSubprocess returns an Exec that runs the program at exe with the
// arguments and connects its stdout and stderr to the writers. The exit code is
// the real one of the process, so `ki test` tests what a shell would see. If
// the program cannot be started at all, or dies of a signal, it returns -1; in
// the first case the reason goes to stderr.
func ExecSubprocess(exe string) func(args []string, stdout, stderr io.Writer) int {
	return func(args []string, stdout, stderr io.Writer) int {
		cmd := exec.Command(exe, args...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		err := cmd.Run()
		if cmd.ProcessState == nil {
			_, _ = fmt.Fprintf(stderr, "ki test: cannot run %s: %v\n", exe, err)
			return -1
		}
		return cmd.ProcessState.ExitCode()
	}
}
