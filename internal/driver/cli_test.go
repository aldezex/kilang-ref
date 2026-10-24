package driver

import (
	"os"
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
		want := "usage: ki <command> [args]\ncommands: --version, version\n"
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
}
