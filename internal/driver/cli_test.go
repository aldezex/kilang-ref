package driver

import (
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
		want := "usage: ki <command> [args]\ncommands: --version\n"
		if code != 2 || out != "" || err != want {
			t.Fatalf("got %d %q %q, want 2 \"\" %q", code, out, err, want)
		}
	})
}
