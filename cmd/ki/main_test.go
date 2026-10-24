package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/driver"
)

// runMainEnv is the environment variable that turns this test binary into ki.
// Package main cannot be imported and main exits the process, so the test
// starts the test binary again, as a child process, with this variable set; the
// child runs main instead of the tests.
const runMainEnv = "KI_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
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
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("cannot find the test binary to run it as ki: %v", err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), runMainEnv+"=1")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
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
	return code, out.String(), errOut.String()
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
}
