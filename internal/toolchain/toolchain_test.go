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

// readRepoLines returns the lines of the file at rel, a path from the root of
// the repository such as "go.mod". go test runs each package's tests in that
// package's directory, so the root is two levels up.
func readRepoLines(t *testing.T, rel string) []string {
	t.Helper()
	data, err := os.ReadFile("../../" + rel)
	if err != nil {
		t.Fatalf("cannot read %s: %v", rel, err)
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
		lines := readRepoLines(t, "go.mod")
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
		for i, line := range readRepoLines(t, "go.mod") {
			if strings.HasPrefix(line, "require") {
				t.Errorf("go.mod line %d is %q: Ki uses only the standard library", i+1, line)
			}
		}
	})
}

// toolchainVersion returns the version on the toolchain line of go.mod, such
// as "go1.27.3". It is the first word after "toolchain ", as for
// ci/ci-local.sh: a comment after it ("// …") is not part of it.
func toolchainVersion(t *testing.T) string {
	t.Helper()
	for _, line := range readRepoLines(t, "go.mod") {
		if rest, ok := strings.CutPrefix(line, "toolchain "); ok {
			if words := strings.Fields(rest); len(words) > 0 {
				return words[0]
			}
		}
	}
	t.Fatalf("go.mod has no toolchain line")
	return ""
}

func TestRuntimeVersionMatchesToolchainLine(t *testing.T) {
	want := toolchainVersion(t)
	if got := runtime.Version(); got != want {
		t.Errorf("runtime.Version() is %q, but go.mod asks for toolchain %q", got, want)
	}
}
