package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/driver"
	"github.com/aldezex/kilang/kirt/out"
)

// runMainEnv is the environment variable that turns this test binary into ki.
// Package main cannot be imported and main exits the process, so the test
// starts the test binary again, as a child process, with this variable set; the
// child runs main instead of the tests.
const runMainEnv = "KI_TEST_RUN_MAIN"

// stdoutEnv is the environment variable that makes the child's stdout a writer
// that cannot write, like a disk that is full: "buffered" for one that keeps
// what it is given until it is flushed, and "by_line" for one that sends on at
// each line break, as a terminal's does. The child gets out.Stdout replaced
// before main reads it.
const stdoutEnv = "KI_TEST_STDOUT"

// failingWriter is an io.Writer whose every Write fails and writes nothing.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		switch os.Getenv(stdoutEnv) {
		case "buffered":
			out.Stdout = out.New(failingWriter{}, 0, false)
		case "by_line":
			out.Stdout = out.New(failingWriter{}, 0, true)
		}
		main()
		// main ends the process with os.Exit and never gets here. If it did,
		// the child would exit with 0, and a test that expects another code
		// would see the difference.
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runKi runs main as a child process with args and returns its exit code and
// what it wrote to stdout and stderr. The tests of package driver call Main
// in-process; this is the only place that runs the code between the process
// and Main: the arguments, the streams and the exit code.
func runKi(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return runKiWith(t, nil, args...)
}

// runKiWith is runKi with the variables env ("NAME=value") added to the
// environment of the child.
func runKiWith(t *testing.T, env []string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("cannot find the test binary to run it as ki: %v", err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(append(os.Environ(), runMainEnv+"=1"), env...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("cannot run the test binary as ki: %v", err)
	}
	return code, stdoutBuf.String(), stderrBuf.String()
}

func TestBinary(t *testing.T) {
	t.Run("streams_and_exit_codes", func(t *testing.T) {
		const usagePrefix = "usage: ki <command> [args]\ncommands: "
		version := "ki " + driver.Version + " (" + runtime.Version() + ")\n"
		tests := []struct {
			name         string
			args         []string
			code         int
			stdout       string
			stderrPrefix string // stderr must be empty when this is empty
		}{
			{"version", []string{"--version"}, 0, version, ""},
			{"no_args", nil, 2, "", usagePrefix},
			{"unknown_command", []string{"frobnicate"}, 2, "", usagePrefix},
		}
		for _, tc := range tests {
			code, stdout, stderr := runKi(t, tc.args...)
			if code != tc.code {
				t.Errorf("%s: exit code %d, want %d", tc.name, code, tc.code)
			}
			if stdout != tc.stdout {
				t.Errorf("%s: stdout %q, want %q", tc.name, stdout, tc.stdout)
			}
			if tc.stderrPrefix == "" && stderr != "" {
				t.Errorf("%s: stderr %q, want empty", tc.name, stderr)
			}
			if !strings.HasPrefix(stderr, tc.stderrPrefix) {
				t.Errorf("%s: stderr %q does not start with %q", tc.name, stderr, tc.stderrPrefix)
			}
		}
	})

	t.Run("lost_stdout_is_reported_and_exits_1", func(t *testing.T) {
		const lost = "ki: cannot write to stdout: no space left on device\n"
		dir := t.TempDir()
		// panics prints a 7 and then panics at 3:11; prints only prints it.
		panics, prints := filepath.Join(dir, "panics.ki"), filepath.Join(dir, "prints.ki")
		for path, src := range map[string]string{
			panics: "fn main() {\n  print(7)\n  print(1 / 0)\n}\n",
			prints: "fn main() { print(7) }\n",
		} {
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatalf("cannot write the program: %v", err)
			}
		}
		panicked := "panic: division by zero at " + panics + ":3:11\n"
		tests := []struct {
			name   string
			stdout string // the value of stdoutEnv
			args   []string
			code   int
			want   string // what reaches the real stdout
			stderr string
		}{
			// The answer is only in the buffer when main returns, and the flush
			// that sends it on is the one in out.Exit.
			{"answer_lost_in_the_last_flush", "buffered", []string{"--version"}, 1, "", lost},
			// A program that panicked keeps its exit code, 101 and not 1, and
			// its stderr: the panic is what it has to say.
			{"program_exit_code_stays", "buffered", []string{"run", panics}, 101, "", panicked},
			// A writer that sends on at each line has the failure in the write of
			// the line, where print ignores it; Exit still knows.
			{"failure_in_an_earlier_line", "by_line", []string{"run", prints}, 1, "", lost},
			// Here the driver saw the failure in the write of its answer and said
			// so with its own message, and Exit does not say it a second time.
			{"reported_once_by_the_driver", "by_line", []string{"--version"}, 1, "", "ki --version: cannot write to stdout: no space left on device\n"},
			// The controls: the same programs with a stdout that works.
			{"control_prints", "", []string{"run", prints}, 0, "7\n", ""},
			{"control_panics", "", []string{"run", panics}, 101, "7\n", panicked},
		}
		for _, tc := range tests {
			code, stdout, stderr := runKiWith(t, []string{stdoutEnv + "=" + tc.stdout}, tc.args...)
			if code != tc.code || stdout != tc.want || stderr != tc.stderr {
				t.Errorf("%s: got exit code %d, stdout %q and stderr %q, want %d, %q and %q", tc.name, code, stdout, stderr, tc.code, tc.want, tc.stderr)
			}
		}
	})
}
