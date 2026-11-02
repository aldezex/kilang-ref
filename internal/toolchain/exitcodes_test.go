package toolchain_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// helperRun runs the test binary again, with only TestHelperProcess selected and
// the environment variable that makes it do what, and returns what it wrote and
// the exit code it ended with. A process that could not be started, or that was
// killed by a signal, fails the test: it has no exit code to look at.
func helperRun(t *testing.T, what string) (code int, stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"="+what)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		code = exit.ExitCode()
	default:
		t.Fatalf("the helper process %q did not end with an exit code: %v\n--- stdout:\n%s\n--- stderr:\n%s", what, err, out.String(), errOut.String())
	}
	return code, out.String(), errOut.String()
}

func TestExitCodes(t *testing.T) {
	t.Run("go_panic_exits_2", func(t *testing.T) {
		code, stdout, stderr := helperRun(t, "panic")
		if code != 2 {
			t.Errorf("a Go panic that nothing recovers ended with exit code %d, want 2\n--- stdout:\n%s\n--- stderr:\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "panic: boom") {
			t.Errorf("stderr of a Go panic is %q, want it to contain \"panic: boom\"", stderr)
		}
	})

	t.Run("os_exit_skips_deferred_calls", func(t *testing.T) {
		code, stdout, stderr := helperRun(t, "exit101")
		if code != 101 {
			t.Errorf("os.Exit(101) ended with exit code %d, want 101\n--- stdout:\n%s\n--- stderr:\n%s", code, stdout, stderr)
		}
		if strings.Contains(stdout, "deferred") {
			t.Errorf("stdout after os.Exit(101) is %q, want it without \"deferred\": os.Exit does not run deferred calls", stdout)
		}
	})
}
