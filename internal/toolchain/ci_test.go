package toolchain_test

import (
	"slices"
	"strings"
	"testing"
)

// ciLine is one line of ci.yml that says something: how many spaces it is
// indented, and its text without them.
type ciLine struct {
	indent int
	text   string
}

// readCIWorkflow returns the lines of the repository's GitHub Actions
// workflow, without blank lines and without comments. YAML nests by
// indentation, so a line only means something under the right parent, and the
// indentation is what tells the parent. The tests read the file as plain text:
// Go's standard library has no YAML parser, and the facts they check are
// visible in the text itself.
func readCIWorkflow(t *testing.T) []ciLine {
	t.Helper()
	var lines []ciLine
	for _, line := range readRepoLines(t, ".github/workflows/ci.yml") {
		text := strings.TrimLeft(line, " ")
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		lines = append(lines, ciLine{indent: len(line) - len(text), text: text})
	}
	return lines
}

// under returns the text of every line nested below the line that path names,
// at any depth and in file order. Each element of path is the exact text of a
// line, and the next one is looked for among the direct children of the
// previous one: under(lines, "jobs:", "check:", "env:") is the "env:" of the
// job, never the "env:" of one of its steps. Like the other helpers, it stops
// the test when a line of the path is missing, saying which.
func under(t *testing.T, lines []ciLine, path ...string) []string {
	t.Helper()
	for _, header := range path {
		start := -1
		for i := range lines {
			// The first line of the region sets the indentation of its children.
			if lines[i].indent == lines[0].indent && lines[i].text == header {
				start = i
				break
			}
		}
		if start < 0 {
			t.Fatalf("ci.yml has no line %q at the place %q leads to", header, path)
		}
		end := start + 1
		for end < len(lines) && lines[end].indent > lines[start].indent {
			end++
		}
		lines = lines[start+1 : end]
	}
	texts := make([]string, len(lines))
	for i, line := range lines {
		texts[i] = line.text
	}
	return texts
}

func TestCIWorkflow(t *testing.T) {
	t.Run("runs_ci_local", func(t *testing.T) {
		// The step has exactly this one line: a commented-out
		// "# run: ci/ci-local.sh", "run: ci/ci-local.sh -x", or a step that
		// also says "if: false" or "continue-on-error: true", does not count.
		got := under(t, readCIWorkflow(t), "jobs:", "check:", "steps:", "- name: Run ci-local")
		if want := []string{"run: ci/ci-local.sh"}; !slices.Equal(got, want) {
			t.Errorf("the step \"Run ci-local\" has the lines %q, want %q: the CI must run the same checks as this machine, every time", got, want)
		}
	})
	t.Run("pins_toolchain", func(t *testing.T) {
		// Whole lines, each under its own parent: a commented-out
		// "# GOTOOLCHAIN: local", "GOTOOLCHAIN: local+auto", which allows
		// downloads, or the job's "local" and the "Set up Go" step's "auto"
		// swapped, do not count.
		lines := readCIWorkflow(t)
		job := []string{"jobs:", "check:"}
		setupGo := append(slices.Clone(job), "steps:", "- name: Set up Go")
		tests := []struct {
			name string
			path []string
			want string
		}{
			{"job_env", append(slices.Clone(job), "env:"), "GOTOOLCHAIN: local"},
			{"setup_go_env", append(slices.Clone(setupGo), "env:"), "GOTOOLCHAIN: auto"},
			{"setup_go_with", append(slices.Clone(setupGo), "with:"), "go-version-file: go.mod"},
		}
		for _, tc := range tests {
			if got := under(t, lines, tc.path...); !slices.Contains(got, tc.want) {
				t.Errorf("%s: under %q there is no line %q, only %q", tc.name, tc.path, tc.want, got)
			}
		}
	})
	t.Run("go_version_matches_toolchain", func(t *testing.T) {
		// The step compares the installed Go with a literal written in the
		// workflow. The literal must be the version of go.mod's toolchain
		// line: if only one of the two changes at a Go upgrade, the checks
		// pass here and the CI stops at its first step.
		want := []string{`run: test "$(go version | awk '{print $3}')" = ` + toolchainVersion(t)}
		got := under(t, readCIWorkflow(t), "jobs:", "check:", "steps:", "- name: Check the Go version")
		if !slices.Equal(got, want) {
			t.Errorf("the step \"Check the Go version\" has the lines %q, want %q", got, want)
		}
	})
	t.Run("triggers", func(t *testing.T) {
		// The three triggers the header of ci.yml promises, and no others.
		want := []string{
			"push:",
			"branches: [main]",
			"tags: ['ch*', 'm*']",
			"pull_request: {}",
		}
		if got := under(t, readCIWorkflow(t), "on:"); !slices.Equal(got, want) {
			t.Errorf("the lines under \"on:\" are %q, want %q", got, want)
		}
	})
}
