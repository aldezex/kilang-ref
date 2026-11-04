// bookx: not-in-book

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// mutantRepo makes a real repository with a small module. Its tests check Add and
// Max, and the only mutant they cannot see is `>` for `>=` in Max, which is
// equivalent.
func mutantRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := newRepo(t)
	writeFile(t, dir, "go.mod", "module example.com/m\n\ngo 1.21\n")
	writeFile(t, dir, ".gitignore", "/build/\n")
	writeFile(t, dir, "calc/calc.go", mutantSource)
	writeFile(t, dir, "calc/calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tt.Run(\"two_and_three\", func(t *testing.T) {\n\t\tif Add(2, 3) != 5 {\n\t\t\tt.Error(\"wrong\")\n\t\t}\n\t})\n}\n\nfunc TestMax(t *testing.T) {\n\tif Max(2, 1) != 2 {\n\t\tt.Error(\"wrong\")\n\t}\n\tif Max(1, 2) != 2 {\n\t\tt.Error(\"wrong\")\n\t}\n}\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "one")
	gitIn(t, dir, "tag", "base")
	return dir
}

const mutantSource = "package calc\n\n// Add adds.\nfunc Add(a, b int) int {\n\treturn a + b\n}\n\n// Max is the larger.\nfunc Max(a, b int) int {\n\tif a > b {\n\t\treturn a\n\t}\n\treturn b\n}\n"

// runMutateIn runs the command with no cap and the default build cache: a test
// has no systemd to count on, and a new cache takes half a minute to fill.
func runMutateIn(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	noCap(t)
	cache, err := exec.Command("go", "env", "GOCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	mutateSharedCache = strings.TrimSpace(string(cache))
	t.Cleanup(func() { mutateSharedCache = "" })
	return runBookx(t, append([]string{"mutate"}, args...)...)
}

// mutantsOf returns the mutants of a package with this one source file, each one
// as `<op>|<old>|<new>`, and how many were left out as equivalent.
func mutantsOf(t *testing.T, src string) ([]string, int) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/m\n\ngo 1.21\n")
	writeFile(t, dir, "p/p.go", src)
	ms, filtered, err := packageMutants(dir, filepath.Join(dir, "p"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range ms {
		out = append(out, m.Op+"|"+m.Old+"|"+m.New)
	}
	return out, filtered
}

func TestPackageMutants(t *testing.T) {
	t.Run("operators", func(t *testing.T) {
		type row struct {
			name string
			src  string
			want []string
		}
		for _, r := range []row{
			{"if, comparison, numbers", "func f(a int) int {\n\tif a < 1 {\n\t}\n\treturn 0\n}\n", []string{
				"if-negate|a < 1|!(a < 1)", "binop|<|<=", "int+1|1|2", "int+1|0|1",
			}},
			{"and drops to true", "func f(a, b bool) bool {\n\treturn a && b\n}\n", []string{
				"drop-lhs|a|true", "binop|&&|||", "drop-rhs|b|true",
			}},
			{"or drops to false", "func f(a, b bool) bool {\n\treturn a || b\n}\n", []string{
				"drop-lhs|a|false", "binop||||&&", "drop-rhs|b|false",
			}},
			{"every comparison", "func f(a, b int) {\n\t_ = a >= b\n\t_ = a > b\n\t_ = a <= b\n\t_ = a == b\n\t_ = a != b\n}\n", []string{
				"del-assign|_ = a >= b|", "binop|>=|>", "del-assign|_ = a > b|", "binop|>|>=", "del-assign|_ = a <= b|", "binop|<=|<",
				"del-assign|_ = a == b|", "binop|==|!=", "del-assign|_ = a != b|", "binop|!=|==",
			}},
			{"arithmetic", "func f(a, b int) int {\n\treturn a + b - a*b/a%b\n}\n", []string{
				"binop|+|-", "binop|-|+", "binop|*|/", "binop|/|*", "binop|%|*",
			}},
			{"a string join stays", "func f(a, b string) string {\n\treturn a + b\n}\n", nil},
			{"for, break, continue", "func f(n int) int {\n\tfor i := 0; i < n; i++ {\n\t\tif i == 3 {\n\t\t\tbreak\n\t\t}\n\t\tcontinue\n\t}\n\treturn n\n}\n", []string{
				"int+1|0|1", "for-false|i < n|false", "binop|<|<=", "incdec|++|--", "if-negate|i == 3|!(i == 3)", "binop|==|!=", "int+1|3|4", "branch|break|continue", "branch|continue|break",
			}},
			{"a labelled break stays", "func f() {\nL:\n\tfor {\n\t\tbreak L\n\t}\n}\n", nil},
			{"assignments", "func f(a int) int {\n\ta += b()\n\ta -= b()\n\ta = b()\n\ta--\n\treturn 9\n}\nfunc b() int { return 7 }\n", []string{
				"del-assign|a += b()|", "assign-op|+=|-=", "del-assign|a -= b()|", "assign-op|-=|+=", "del-assign|a = b()|", "del-incdec|a--|", "incdec|--|++", "int+1|9|10", "int+1|7|8",
			}},
			{"calls and defers", "func f(a int) int {\n\tg(a)\n\tdefer g(a)\n\treturn g(a)\n}\nfunc g(n int) int { return n }\n", []string{
				"call-unwrap|g(a)|a", "del-call|g(a)|", "del-defer|defer g(a)|", "call-unwrap|g(a)|a", "call-unwrap|g(a)|a",
			}},
			{"a panic stays", "func f() {\n\tpanic(\"unreachable\")\n}\n", nil},
			{"another result type is no unwrap", "func f(a int) string {\n\treturn g(a)\n}\nfunc g(n int) string { return \"\" }\n", nil},
			{"case values", "func f(a int) {\n\tswitch a {\n\tcase 1, 2:\n\t}\n}\n", []string{
				"int+1|1|2", "case-drop|1, |", "case-drop|, 2|", "int+1|2|3",
			}},
			{"a single case value stays", "func f(a string) {\n\tswitch a {\n\tcase \"x\":\n\t}\n}\n", nil},
			{"booleans", "func f() (bool, bool) {\n\treturn true, false\n}\n", []string{"bool|true|false", "bool|false|true"}},
			{"struct fields", "type S struct{ X, Y int }\nfunc f(v int) S {\n\treturn S{X: v, Y: v}\n}\n", []string{
				"field-drop|X: v, |", "field-drop|, Y: v|",
			}},
			{"a one-field literal stays", "type S struct{ X int }\nfunc f(v int) S {\n\treturn S{X: v}\n}\n", nil},
			{"at most two other fields", "type S struct{ X, Y, Z, W int; N string }\nfunc f(s S) int {\n\treturn s.X\n}\n", []string{"swap|X|Y", "swap|X|Z"}},
			{"at most two other locals", "func f(a, b, c, d int) int {\n\treturn d\n}\n", []string{"swap|d|a", "swap|d|b"}},
			{"a local of another type or declared later stays", "func f(a int, s string) int {\n\tx := a\n\t_ = s\n\ty := x\n\treturn y + 0*x\n}\n", []string{
				"del-assign|_ = s|", "swap|x|a", "binop|+|-", "int+1|0|1", "binop|*|/",
			}},
			{"constants, variables and types stay", "const k = 1\nvar v = 2 + 3\ntype T [4]int\n", nil},
		} {
			got, _ := mutantsOf(t, "package p\n\n"+r.src)
			if !slices.Equal(got, r.want) {
				t.Errorf("%s:\n got %q\nwant %q", r.name, got, r.want)
			}
		}
	})

	t.Run("equivalent_shapes_are_left_out_and_counted", func(t *testing.T) {
		type row struct {
			name     string
			src      string
			want     []string
			filtered int
		}
		for _, r := range []row{
			{"a zero field", "type S struct{ X, Y int; N string; B bool; P *int }\nfunc f(v int) S {\n\treturn S{X: 0, Y: v, N: \"\", B: false, P: nil}\n}\n", []string{"int+1|0|1", "field-drop|Y: v, |", "bool|false|true"}, 4},
			{"a constant that is the zero", "type K int\nconst (\n\tZero K = iota\n\tOne\n)\ntype S struct{ A, B K }\nfunc f(k K) S {\n\treturn S{A: Zero, B: k}\n}\n", []string{"field-drop|, B: k|"}, 1},
			{"a constant that is not", "type K int\nconst (\n\tZero K = iota\n\tOne\n)\ntype S struct{ A, B K }\nfunc f(k K) S {\n\treturn S{A: One, B: k}\n}\n", []string{"field-drop|A: One, |", "field-drop|, B: k|"}, 0},
			{"a capacity", "func f() []int {\n\treturn make([]int, 2, 8)\n}\n", []string{"int+1|2|3"}, 1},
			{"a map size", "func f() map[int]int {\n\treturn make(map[int]int, 8)\n}\n", nil, 1},
			{"the length of make is no hint", "func f() []int {\n\treturn make([]int, 8)\n}\n", []string{"int+1|8|9"}, 0},
			{"the number of return 0, false", "func f() (int, bool) {\n\treturn 0, false\n}\n", []string{"bool|false|true"}, 1},
			{"a capacity that is no literal", "func f(n int) []int {\n\treturn make([]int, 2, n)\n}\n", []string{"int+1|2|3"}, 0},
			{"a result that is no literal", "func f(n int) (int, bool) {\n\treturn n, false\n}\n", []string{"bool|false|true"}, 0},
			{"the number of return 0, true counts", "func f() (int, bool) {\n\treturn 0, true\n}\n", []string{"int+1|0|1", "bool|true|false"}, 0},
		} {
			got, filtered := mutantsOf(t, "package p\n\n"+r.src)
			if !slices.Equal(got, r.want) || filtered != r.filtered {
				t.Errorf("%s:\n got %q, %d filtered\nwant %q, %d filtered", r.name, got, filtered, r.want, r.filtered)
			}
		}
	})

	t.Run("files_that_are_not_the_package", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", "module example.com/m\n\ngo 1.21\n")
		writeFile(t, dir, "p/p.go", "package p\n\nfunc F() int { return 1 }\n")
		writeFile(t, dir, "p/p_test.go", "package p\n\nfunc G() int { return 2 }\n")
		writeFile(t, dir, "p/other.go", "//go:build ignore\n\npackage main\n\nfunc H() int { return 3 }\n")
		writeFile(t, dir, "p/gen.go", "// Code generated by x. DO NOT EDIT.\n\npackage p\n\nfunc I() int { return 4 }\n")
		ms, _, err := packageMutants(dir, filepath.Join(dir, "p"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) != 1 || ms[0].File != "p/p.go" || ms[0].Old != "1" {
			t.Errorf("mutants %+v, want only the 1 of p/p.go", ms)
		}
	})

	t.Run("an_id_does_not_depend_on_the_filter", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", "module example.com/m\n\ngo 1.21\n")
		writeFile(t, dir, "p/p.go", "package p\n\nfunc F() int { return 1 }\n\nfunc G() int { return 2 }\n")
		ms, _, err := packageMutants(dir, filepath.Join(dir, "p"), func(file string, line int) bool { return file == "p/p.go" && line == 5 })
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) != 1 || ms[0].ID != "M0002" || ms[0].Line != 5 || ms[0].Old != "2" {
			t.Errorf("mutants %+v, want M0002 on line 5", ms)
		}
	})

	t.Run("a_report_shows_an_edit_on_one_short_line", func(t *testing.T) {
		long := strings.Repeat("é", 61)
		m := mutant{ID: "M0001", File: "a.go", Line: 3, Op: "del-call", Old: "f(a,\n\t\tb)", New: long}
		want := "M0001 a.go:3 del-call: `f(a, b)` -> `" + strings.Repeat("é", 60) + "…`"
		if got := m.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	})

	t.Run("apply_replaces_the_bytes_of_the_edit", func(t *testing.T) {
		m := mutant{lo: 2, hi: 3, New: "XY"}
		src := []byte("abcd")
		if got := string(m.apply(src)); got != "abXYd" {
			t.Errorf("apply = %q", got)
		}
		if string(src) != "abcd" {
			t.Errorf("apply changed its input: %q", src)
		}
	})
}

func TestClassifyRun(t *testing.T) {
	type row struct {
		name    string
		exit    int
		output  string
		verdict string
		by      string
	}
	t.Run("rows", func(t *testing.T) {
		for _, r := range []row{
			{"green", 0, "ok  \tpkg\t0.1s\n", verdictSurvived, ""},
			{"a test fails", 1, "--- FAIL: TestA (0.00s)\nFAIL\nFAIL\tpkg\t0.1s\nFAIL\n", verdictKilled, "TestA"},
			{"a subtest fails", 1, "--- FAIL: TestA (0.00s)\n    --- FAIL: TestA/b (0.00s)\n        --- FAIL: TestA/b/c (0.00s)\nFAIL\n", verdictKilled, "TestA/b/c"},
			{"the first failure is named", 1, "--- FAIL: TestA (0.00s)\n    --- FAIL: TestA/b (0.00s)\n--- FAIL: TestC (0.00s)\nFAIL\n", verdictKilled, "TestA/b"},
			{"a test with a name that only starts alike", 1, "--- FAIL: TestA (0.00s)\n--- FAIL: TestAB (0.00s)\n", verdictKilled, "TestA"},
			{"a panic outside a test", 2, "panic: boom\n\ngoroutine 1 [running]:\nFAIL\tpkg\t0.1s\n", verdictKilled, "panic: boom"},
			{"only the package", 1, "FAIL\tpkg\t0.1s\n", verdictKilled, "pkg"},
			{"nothing to go on", 3, "", verdictKilled, "exit code"},
			{"no compile", 1, "# pkg\nx.go:1:1: undefined: y\nFAIL\tpkg [build failed]\nFAIL\n", verdictNoCompile, ""},
			{"no setup", 1, "FAIL\tpkg [setup failed]\n", verdictNoCompile, ""},
			{"a test that prints those words", 1, "--- FAIL: TestA (0.00s)\n    x_test.go:1: FAIL pkg [build failed] was in the text\n", verdictKilled, "TestA"},
			{"exit 137 is the memory cap", 137, "", verdictOOM, "the memory cap"},
			{"go test saw the kill", 1, "signal: killed\nFAIL\tpkg\t3.1s\n", verdictOOM, "the memory cap"},
			{"exit 124 is the time limit", 124, "", verdictTimeout, "the time limit"},
			{"the limit of go test", 1, "panic: test timed out after 1s\n", verdictTimeout, "the time limit"},
		} {
			verdict, by := classifyRun(r.exit, []byte(r.output))
			if verdict != r.verdict || by != r.by {
				t.Errorf("%s: %q, %q; want %q, %q", r.name, verdict, by, r.verdict, r.by)
			}
		}
	})
}

func TestChangedLines(t *testing.T) {
	t.Run("lines_of_the_new_version", func(t *testing.T) {
		diff := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +2,2 @@ func\n-a\n+b\n+c\n@@ -9,0 +12 @@\n+d\n@@ -20,2 +23,0 @@\n-e\n-f\n+++ b/y.go\n@@ -1 +1 @@\n"
		got := changedLines([]byte(diff))
		if !got["x.go"][2] || !got["x.go"][3] || !got["x.go"][12] || !got["y.go"][1] {
			t.Errorf("lines %v", got)
		}
		if got["x.go"][1] || got["x.go"][4] || got["x.go"][23] || len(got["x.go"]) != 3 {
			t.Errorf("extra lines %v", got)
		}
	})
}

func TestMutateRun(t *testing.T) {
	const verdicts = "mutate ./calc: 5 mutants of 5 (0 left out as equivalent)\n" +
		"killed    M0001 calc/calc.go:5 binop: `+` -> `-` (by TestAdd/two_and_three)\n" +
		"killed    M0002 calc/calc.go:10 if-negate: `a > b` -> `!(a > b)` (by TestMax)\n" +
		"survived  M0003 calc/calc.go:10 binop: `>` -> `>=`\n" +
		"killed    M0004 calc/calc.go:11 swap: `a` -> `b` (by TestMax)\n" +
		"killed    M0005 calc/calc.go:13 swap: `b` -> `a` (by TestMax)\n"

	t.Run("each_verdict_names_its_mutant_and_what_killed_it", func(t *testing.T) {
		dir := mutantRepo(t)
		code, out, errOut := runMutateIn(t, "--repo", dir, "--pkg", "./calc")
		if code != exitOK || errOut != "" || !strings.HasPrefix(out, verdicts) {
			t.Fatalf("code %d, stderr %q, stdout:\n%s\nwant it to start with:\n%s", code, errOut, out, verdicts)
		}
		tail := strings.TrimPrefix(out, verdicts)
		if !strings.HasPrefix(tail, "mutate ./calc: 4 killed (0 by other packages), 0 by the memory cap, 0 by the time limit, 1 survived, 0 did not compile; ") {
			t.Errorf("summary %q", tail)
		}
	})

	t.Run("the_tree_is_never_touched_and_nothing_stays", func(t *testing.T) {
		dir := mutantRepo(t)
		before := gitIn(t, dir, "status", "--porcelain")
		code, _, _ := runMutateIn(t, "--repo", dir, "--pkg", "./calc")
		if code != exitOK {
			t.Fatalf("code %d", code)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "calc", "calc.go")); string(got) != mutantSource {
			t.Errorf("the source changed:\n%s", got)
		}
		if after := gitIn(t, dir, "status", "--porcelain"); after != before {
			t.Errorf("git status went from %q to %q", before, after)
		}
		entries, err := os.ReadDir(filepath.Join(dir, "build", "mutate"))
		if err != nil || len(entries) != 1 || entries[0].Name() != "last.log" {
			t.Errorf("build/mutate holds %v (err %v), want only last.log", entries, err)
		}
	})

	t.Run("the_log_says_what_was_about_to_run", func(t *testing.T) {
		dir := mutantRepo(t)
		runMutateIn(t, "--repo", dir, "--pkg", "./calc", "--only", "M0003")
		got, err := os.ReadFile(filepath.Join(dir, "build", "mutate", "last.log"))
		want := "start M0003 calc/calc.go:10 binop: `>` -> `>=`\nsurvived  M0003 calc/calc.go:10 binop: `>` -> `>=`\n"
		if err != nil || string(got) != want {
			t.Errorf("log %q (err %v), want %q", got, err, want)
		}
	})

	t.Run("it_sees_the_working_tree_as_it_is", func(t *testing.T) {
		dir := mutantRepo(t)
		// A new test, not committed, kills the mutant that the old ones let through.
		writeFile(t, dir, "calc/new_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestAddZero(t *testing.T) {\n\tif Add(0, 7) != 7 {\n\t\tt.Error(\"wrong\")\n\t}\n}\n")
		writeFile(t, dir, "calc/calc.go", strings.Replace(mutantSource, "// Add adds.", "// Add adds two numbers.", 1))
		code, out, _ := runMutateIn(t, "--repo", dir, "--pkg", "./calc", "--list")
		if code != exitOK || !strings.Contains(out, "M0001 calc/calc.go:5 binop") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("since_keeps_the_changed_lines", func(t *testing.T) {
		dir := mutantRepo(t)
		writeFile(t, dir, "calc/calc.go", strings.Replace(mutantSource, "return a + b", "return b + a", 1))
		writeFile(t, dir, "calc/fresh.go", "package calc\n\nfunc Fresh() int { return 1 }\n")
		code, out, _ := runMutateIn(t, "--repo", dir, "--pkg", "./calc", "--since", "base", "--list")
		want := "mutate ./calc: 2 mutants of 2 (0 left out as equivalent)\n" +
			"M0001 calc/calc.go:5 binop: `+` -> `-`\n" +
			"M0006 calc/fresh.go:3 int+1: `1` -> `2`\n"
		if code != exitOK || out != want {
			t.Errorf("code %d, stdout %q, want %q", code, out, want)
		}
	})

	t.Run("limit_takes_an_even_sample", func(t *testing.T) {
		code, out, _ := runMutateIn(t, "--repo", mutantRepo(t), "--pkg", "./calc", "--limit", "2", "--list")
		want := "mutate ./calc: 2 mutants of 5 (0 left out as equivalent)\n" +
			"M0001 calc/calc.go:5 binop: `+` -> `-`\n" +
			"M0003 calc/calc.go:10 binop: `>` -> `>=`\n"
		if code != exitOK || out != want {
			t.Errorf("code %d, stdout %q, want %q", code, out, want)
		}
	})

	t.Run("another_package_can_kill_a_survivor", func(t *testing.T) {
		dir := mutantRepo(t)
		writeFile(t, dir, "calc/calc_test.go", "package calc\n")
		writeFile(t, dir, "user/user_test.go", "package user\n\nimport (\n\t\"testing\"\n\n\t\"example.com/m/calc\"\n)\n\nfunc TestUsesAdd(t *testing.T) {\n\tif calc.Add(2, 3) != 5 {\n\t\tt.Error(\"wrong\")\n\t}\n}\n")
		code, out, _ := runMutateIn(t, "--repo", dir, "--pkg", "./calc", "--only", "M0001")
		want := "killed    M0001 calc/calc.go:5 binop: `+` -> `-` (by TestUsesAdd)\n"
		if code != exitOK || !strings.Contains(out, want) || !strings.Contains(out, "1 killed (1 by other packages)") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_mutant_that_does_not_compile_is_told_apart", func(t *testing.T) {
		// Deleting the only use of a local leaves it declared and not used.
		dir := mutantRepo(t)
		writeFile(t, dir, "calc/calc.go", mutantSource+"\n// Sum is a sum.\nfunc Sum(a int) int {\n\tc := a\n\t_ = c\n\treturn 1\n}\n")
		code, out, _ := runMutateIn(t, "--repo", dir, "--pkg", "./calc", "--since", "base", "--limit", "1")
		if code != exitOK || !strings.Contains(out, "nocompile M0006 calc/calc.go:19 del-assign: `_ = c` -> ``") || !strings.Contains(out, "0 survived, 1 did not compile") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("red_tests_stop_it", func(t *testing.T) {
		dir := mutantRepo(t)
		writeFile(t, dir, "calc/red_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestRed(t *testing.T) { t.Error(\"red\") }\n")
		code, out, errOut := runMutateIn(t, "--repo", dir, "--pkg", "./calc")
		if code != exitFail || !strings.Contains(errOut, "the tests fail with no mutant") || strings.Contains(out, "killed") {
			t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
		}
	})

	t.Run("red_tests_show_the_end_of_a_long_output", func(t *testing.T) {
		dir := mutantRepo(t)
		writeFile(t, dir, "calc/red_test.go", "package calc\n\nimport (\n\t\"strings\"\n\t\"testing\"\n)\n\nfunc TestRed(t *testing.T) { t.Error(strings.Repeat(\"x\", 3000)) }\n")
		code, _, errOut := runMutateIn(t, "--repo", dir, "--pkg", "./calc")
		if code != exitFail || !strings.Contains(errOut, "FAIL\texample.com/m/calc") || len(errOut) > 2200 {
			t.Errorf("code %d, %d bytes of stderr: %q", code, len(errOut), errOut)
		}
	})

	t.Run("a_test_in_the_copy_finds_no_repository", func(t *testing.T) {
		// Its temporary files are inside the repository, and git must not see it.
		dir := mutantRepo(t)
		writeFile(t, dir, "calc/git_test.go", "package calc\n\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc TestNoRepo(t *testing.T) {\n\tcmd := exec.Command(\"git\", \"rev-parse\", \"--git-dir\")\n\tcmd.Dir = t.TempDir()\n\tif out, err := cmd.CombinedOutput(); err == nil {\n\t\tt.Errorf(\"found %s\", out)\n\t}\n}\n")
		code, out, errOut := runMutateIn(t, "--repo", dir, "--pkg", "./calc", "--only", "M0003")
		if code != exitOK || errOut != "" || !strings.Contains(out, "survived  M0003") {
			t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
		}
	})

	t.Run("no_cap_no_run", func(t *testing.T) {
		dir := mutantRepo(t)
		old := capCommand
		capCommand = func(string, []string) ([]string, error) { return nil, os.ErrNotExist }
		t.Cleanup(func() { capCommand = old })
		code, out, errOut := runBookx(t, "mutate", "--repo", dir, "--pkg", "./calc")
		if code != exitFail || out != "" || !strings.HasPrefix(errOut, "bookx mutate: ") {
			t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
		}
		if _, err := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(err) {
			t.Errorf("build exists: %v", err)
		}
	})

	t.Run("usage", func(t *testing.T) {
		code, out, errOut := runBookx(t, "mutate")
		if code != exitUsage || out != "" || !strings.HasPrefix(errOut, "bookx mutate: want --pkg or --apply\nusage: bookx mutate --pkg PKG") {
			t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
		}
	})
}

func TestMutateApply(t *testing.T) {
	patch := "--- a/calc/calc.go\n+++ b/calc/calc.go\n@@ -5 +5 @@\n-\treturn a + b\n+\treturn a * b\n"

	t.Run("a_patch_a_test_kills", func(t *testing.T) {
		dir := mutantRepo(t)
		p := filepath.Join(t.TempDir(), "m.patch")
		if err := os.WriteFile(p, []byte(patch), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out, errOut := runMutateIn(t, "--repo", dir, "--apply", p)
		want := "killed    " + p + " (1 file(s)) (by TestAdd/two_and_three)\n"
		if code != exitOK || out != want || errOut != "" {
			t.Errorf("code %d, stdout %q, stderr %q; want 0, %q", code, out, errOut, want)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "calc", "calc.go")); string(got) != mutantSource {
			t.Errorf("the source changed:\n%s", got)
		}
	})

	t.Run("old_and_new_that_survive_fail", func(t *testing.T) {
		code, out, errOut := runMutateIn(t, "--repo", mutantRepo(t), "--apply", "calc/calc.go", "--old", "a > b", "--new", "a >= b")
		want := "survived  calc/calc.go (1 file(s))\n"
		if code != exitFail || out != want || errOut != "" {
			t.Errorf("code %d, stdout %q, stderr %q; want 1, %q", code, out, errOut, want)
		}
	})

	t.Run("a_panic_is_killed_by_its_test", func(t *testing.T) {
		code, out, _ := runMutateIn(t, "--repo", mutantRepo(t), "--apply", "calc/calc.go", "--old", "return a + b", "--new", `panic("boom")`, "--pkg", "./calc")
		if code != exitOK || !strings.Contains(out, "killed    calc/calc.go (1 file(s)) (by TestAdd/two_and_three)") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_hang_is_stopped_and_named", func(t *testing.T) {
		code, out, _ := runMutateIn(t, "--repo", mutantRepo(t), "--apply", "calc/calc.go", "--old", "return a + b", "--new", "for {\n\t}", "--pkg", "./calc", "--timeout", "1")
		if code != exitOK || !strings.Contains(out, "timeout   calc/calc.go (1 file(s)) (by the time limit)") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_mutant_that_does_not_compile_fails_and_shows_why", func(t *testing.T) {
		code, out, _ := runMutateIn(t, "--repo", mutantRepo(t), "--apply", "calc/calc.go", "--old", "return a + b", "--new", "return nope", "--pkg", "./calc")
		if code != exitFail || !strings.Contains(out, "nocompile calc/calc.go") || !strings.Contains(out, "undefined: nope") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("old_has_to_occur_once", func(t *testing.T) {
		for _, old := range []string{"not there", "return"} {
			code, _, errOut := runMutateIn(t, "--repo", mutantRepo(t), "--apply", "calc/calc.go", "--old", old, "--new", "x")
			if code != exitFail || !strings.Contains(errOut, "--old has to occur exactly once in calc/calc.go") {
				t.Errorf("%s: code %d, stderr %q", old, code, errOut)
			}
		}
	})

	t.Run("a_patch_that_does_not_apply", func(t *testing.T) {
		dir := mutantRepo(t)
		p := filepath.Join(t.TempDir(), "m.patch")
		if err := os.WriteFile(p, []byte(strings.Replace(patch, "a + b", "x + y", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		code, _, errOut := runMutateIn(t, "--repo", dir, "--apply", p)
		if code != exitFail || !strings.Contains(errOut, "git apply") {
			t.Errorf("code %d, stderr %q", code, errOut)
		}
	})

	t.Run("a_patch_inside_the_repository_is_not_skipped", func(t *testing.T) {
		// The copy lives inside the repository, where a `git apply` that finds the
		// repository would skip every path and say nothing.
		dir := mutantRepo(t)
		writeFile(t, dir, "build/m.patch", patch)
		code, out, _ := runMutateIn(t, "--repo", dir, "--apply", filepath.Join(dir, "build", "m.patch"))
		if code != exitOK || !strings.Contains(out, "(1 file(s))") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})
}
