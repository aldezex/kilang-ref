package toolchain_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// modFile is the go.mod of every throwaway module these tests create.
const modFile = "module example.com/m\n\ngo 1.27\n"

// goRun is one run of the go command: its arguments, what it wrote and how it
// ended.
type goRun struct {
	args           []string
	stdout, stderr string
	err            error // nil when go exited with 0
}

// String shows the whole run, so a failing test prints everything go said.
func (r goRun) String() string {
	return fmt.Sprintf("go %s: err=%v\n--- stdout:\n%s\n--- stderr:\n%s",
		strings.Join(r.args, " "), r.err, r.stdout, r.stderr)
}

// runGo runs go with args in dir. It always sets four variables that keep
// the machine's own settings out of the result: GOTOOLCHAIN=local, so go never
// downloads another toolchain; GOWORK=off, so no go.work joins the throwaway
// module to other modules; an empty GOFLAGS, so no inherited flags reach go;
// and GOENV=off, so go ignores its configuration file, where "go env -w"
// stores settings such as GOFLAGS (an empty GOFLAGS alone would fall back to
// that file). Then it adds env (entries such as "CGO_ENABLED=0"). os/exec
// keeps the last value of a repeated variable, so these override what this
// process inherited. It does not judge the result: each test decides what
// counts as the expected behaviour.
func runGo(dir string, env []string, args ...string) goRun {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOENV=off"), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return goRun{args: args, stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// writeFile writes content to dir/name, creating the directories it needs.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("cannot create the directory of %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
}

// buildAndRun builds the main package in dir with a plain go build, runs the
// binary and returns what it printed on stdout.
func buildAndRun(t *testing.T, dir string) string {
	t.Helper()
	// On Windows the file has to end in .exe to be run, and go build -o writes
	// exactly the name it is given.
	prog := "prog"
	if runtime.GOOS == "windows" {
		prog += ".exe"
	}
	if r := runGo(dir, nil, "build", "-o", prog, "."); r.err != nil {
		t.Fatalf("the program does not build:\n%v", r)
	}
	cmd := exec.Command(filepath.Join(dir, prog))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("the program failed: %v\n--- stdout:\n%s\n--- stderr:\n%s", err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func TestToolchainFacts(t *testing.T) {
	t.Run("go_mod_init_writes_full_version", func(t *testing.T) {
		dir := t.TempDir()
		r := runGo(dir, nil, "mod", "init", "example.com/m")
		if r.err != nil {
			t.Fatalf("go mod init failed:\n%v", r)
		}
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			t.Fatalf("cannot read the go.mod that go mod init wrote: %v", err)
		}
		var goLines []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "go ") {
				goLines = append(goLines, line)
			}
		}
		if len(goLines) != 1 || goLines[0] != "go 1.27.3" {
			t.Fatalf("go lines %q, want exactly [\"go 1.27.3\"]; go.mod:\n%s\n%v", goLines, data, r)
		}
	})
	t.Run("ignore_directive_hides_build", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", modFile+"\nignore ./build\n")
		writeFile(t, dir, "m.go", "package m\n")
		writeFile(t, dir, "build/x/main.go", "package main\n\nfunc main() { this does not compile }\n")
		r := runGo(dir, nil, "list", "./...")
		if r.err != nil || r.stdout != "example.com/m\n" {
			t.Fatalf("want go list to succeed and print only \"example.com/m\\n\":\n%v", r)
		}
	})
	t.Run("plain_build_has_devel_version", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", modFile)
		writeFile(t, dir, "main.go", `package main

import (
	"fmt"
	"runtime/debug"
)

func main() {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("no build info")
		return
	}
	fmt.Println(bi.Main.Version)
}
`)
		if got, want := buildAndRun(t, dir), "(devel)\n"; got != want {
			t.Fatalf("Main.Version printed %q, want %q", got, want)
		}
	})
	t.Run("race_requires_cgo", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", modFile)
		writeFile(t, dir, "m_test.go", "package m\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n")
		r := runGo(dir, []string{"CGO_ENABLED=0"}, "test", "-race")
		var exit *exec.ExitError
		if !errors.As(r.err, &exit) || exit.ExitCode() != 2 ||
			!strings.Contains(r.stdout+r.stderr, "-race requires cgo") {
			t.Fatalf("want exit code 2 and \"-race requires cgo\" in the output:\n%v", r)
		}
	})
	t.Run("generic_method_on_concrete_type", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", modFile)
		writeFile(t, dir, "main.go", `package main

import (
	"fmt"
	"strconv"
)

type Set struct{ items []int }

// Map has a type parameter of its own, U: a generic method, new in Go 1.27.
func (s *Set) Map[U any](f func(int) U) []U {
	out := make([]U, 0, len(s.items))
	for _, x := range s.items {
		out = append(out, f(x))
	}
	return out
}

func main() {
	s := &Set{items: []int{1, 2, 3}}
	fmt.Println(s.Map(func(x int) string { return strconv.Itoa(x * 10) }))
	fmt.Println(s.Map[float64](func(x int) float64 { return float64(x) / 2 }))
}
`)
		if got, want := buildAndRun(t, dir), "[10 20 30]\n[0.5 1 1.5]\n"; got != want {
			t.Fatalf("the program printed %q, want %q", got, want)
		}
	})
	t.Run("testflag_lists_shuffle_fuzz_artifacts", func(t *testing.T) {
		r := runGo(t.TempDir(), nil, "help", "testflag")
		if r.err != nil {
			t.Fatalf("go help testflag failed:\n%v", r)
		}
		lines := strings.Split(r.stdout, "\n")
		// Each flag is defined on a line of its own that starts with a tab and
		// the flag, such as "\t-fuzz regexp". Looking for that line, not for
		// the text "-fuzz" anywhere, keeps -fuzztime from passing for -fuzz.
		for _, flag := range []string{"-shuffle", "-fuzz", "-artifacts"} {
			found := false
			for _, line := range lines {
				if f := strings.Fields(line); strings.HasPrefix(line, "\t-") && f[0] == flag {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: go help testflag has no line defining it:\n%v", flag, r)
			}
		}
	})
}
