package tests

import (
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/driver"
	"github.com/aldezex/kilang/internal/kitest"
)

// repoRoot is the root of the repository seen from this directory: go test
// runs the tests of a package with the package's directory as the working
// directory.
const repoRoot = ".."

// goldenDirs are the directories of golden files, relative to the root. Each
// holds one directory per chapter (ch03…), and each of those holds .ki files.
var goldenDirs = []string{"golden", "errors"}

// TestKi runs every golden file of the repository in-process, one subtest per
// file named TestKi/<dir>/<chNN>/<name>. `ki test` runs the same files, but as
// a subprocess of the real binary: both go through kitest.Runner.Exec.
func TestKi(t *testing.T) {
	var files []string // paths relative to the root, with forward slashes
	for _, dir := range goldenDirs {
		matches, err := filepath.Glob(filepath.Join(repoRoot, "tests", dir, "ch*", "*.ki"))
		if err != nil {
			t.Fatalf("cannot list the files of tests/%s: %v", dir, err)
		}
		for _, m := range matches {
			rel, err := filepath.Rel(repoRoot, m)
			if err != nil {
				t.Fatalf("cannot make %s relative to the root: %v", m, err)
			}
			files = append(files, filepath.ToSlash(rel))
		}
	}
	slices.Sort(files)
	if len(files) == 0 {
		t.Fatal("found no golden file under tests/golden or tests/errors: the test would pass without checking anything")
	}

	runner := &kitest.Runner{
		Root: repoRoot,
		Out:  io.Discard,
		Exec: func(args []string, stdout, stderr io.Writer) int {
			// Main builds a new Session on every call, so no case sees the
			// state of another.
			return driver.Main(args, strings.NewReader(""), stdout, stderr)
		},
	}
	for _, file := range files {
		// tests/errors/ch03/x.ki → errors/ch03/x
		name := strings.TrimSuffix(strings.TrimPrefix(file, "tests/"), ".ki")
		t.Run(name, func(t *testing.T) {
			if ok, report := runner.RunFile(file); !ok {
				t.Errorf("\n%s", report)
			}
		})
	}
}
