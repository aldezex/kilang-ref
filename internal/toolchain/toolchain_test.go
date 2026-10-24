// Package toolchain_test pins to tests the facts about Go that the book cites
// "in Go 1.27". It holds no code that is not a test: if a new version of Go
// changes one of these facts, a test here fails, and the book's text has to be
// checked again before the upgrade goes in.
package toolchain_test

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// readGoMod returns the lines of the repository's go.mod. go test runs each
// package's tests in that package's directory, so the file is two levels up.
func readGoMod(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("cannot read go.mod: %v", err)
	}
	return strings.Split(string(data), "\n")
}

// hasLine reports whether one of lines is exactly want. It compares whole
// lines on purpose: a substring check would accept "go 1.27.3" for "go 1.27".
func hasLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func TestGoMod(t *testing.T) {
	t.Run("lines", func(t *testing.T) {
		lines := readGoMod(t)
		tests := []struct{ name, want string }{
			{"go_line", "go 1.27"},
			{"toolchain_line", "toolchain go1.27.3"},
			{"ignore_build", "ignore ./build"},
		}
		for _, tc := range tests {
			if !hasLine(lines, tc.want) {
				t.Errorf("%s: go.mod has no line %q", tc.name, tc.want)
			}
		}
	})
	t.Run("no_dependencies", func(t *testing.T) {
		for i, line := range readGoMod(t) {
			if strings.HasPrefix(line, "require") {
				t.Errorf("go.mod line %d is %q: Ki uses only the standard library", i+1, line)
			}
		}
	})
}

func TestRuntimeVersionMatchesToolchainLine(t *testing.T) {
	var want string
	for _, line := range readGoMod(t) {
		// The version is the first word after "toolchain ", as for
		// ci/ci-local.sh: a comment after it ("// …") is not part of it.
		if rest, ok := strings.CutPrefix(line, "toolchain "); ok {
			if words := strings.Fields(rest); len(words) > 0 {
				want = words[0]
			}
		}
	}
	if want == "" {
		t.Fatalf("go.mod has no toolchain line")
	}
	if got := runtime.Version(); got != want {
		t.Errorf("runtime.Version() is %q, but go.mod asks for toolchain %q", got, want)
	}
}
