package driver

import (
	"fmt"
	"io"
	"os"

	"github.com/aldezex/kilang/internal/kitest"
)

// testExec, when it is not nil, is what `ki test` runs each golden file with
// instead of a subprocess of ki's own executable. Only tests set it: under
// `go test`, os.Executable is the test binary, which cannot stand in for ki.
var testExec func(args []string, stdout, stderr io.Writer) int

// outWriter writes to w and remembers the first error of a Write or a Flush.
// The runner writes its report without checking for errors (it has nowhere to
// report them), so runTest asks the writer afterwards.
type outWriter struct {
	w   io.Writer
	err error
}

func (o *outWriter) Write(p []byte) (int, error) {
	n, err := o.w.Write(p)
	if err != nil && o.err == nil {
		o.err = err
	}
	return n, err
}

// Flush flushes w if it has a Flush method, so that the runner flushes the
// real output after each PASS or FAIL line, and does nothing otherwise.
func (o *outWriter) Flush() error {
	f, ok := o.w.(interface{ Flush() error })
	if !ok {
		return nil
	}
	err := f.Flush()
	if err != nil && o.err == nil {
		o.err = err
	}
	return err
}

// runTest is `ki test [--root DIR] PATH...`: it runs the golden files under
// each PATH (relative to DIR, the working directory by default) and prints a
// PASS or FAIL line for each, the differences of the failures and a summary.
// It exits with 1 if any file failed, or if the report could not be written to
// stdout (a full disk, say): a script must not take a lost report for a pass.
//
// Each file runs as a subprocess of ki's own executable, so what is tested is
// the real binary: its exit code and, once there is one, the flushing of its
// output buffer. Under `go test`, tests/golden_test.go runs the same files
// in-process instead.
func runTest(s *Session, c *command, args []string) int {
	fs := newFlagSet(c)
	root := fs.String("root", ".", "the directory the paths are relative to")
	if code, done := parseFlags(s, c, fs, args); done {
		return code
	}
	if fs.NArg() == 0 {
		return usageError(s, c, fmt.Errorf("missing PATH"))
	}
	exec := testExec
	if exec == nil {
		exe, err := os.Executable()
		if err != nil {
			_, _ = fmt.Fprintf(s.Stderr, "ki test: cannot find the ki executable: %v\n", err)
			return 1
		}
		exec = kitest.ExecSubprocess(exe)
	}
	out := &outWriter{w: s.Stdout}
	r := &kitest.Runner{Root: *root, Exec: exec, Out: out}
	_, failed := r.RunPaths(fs.Args())
	if out.err != nil {
		return writeError(s, c, out.err)
	}
	if failed > 0 {
		return 1
	}
	return 0
}
