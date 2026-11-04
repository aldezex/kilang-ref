// bookx: not-in-book

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDoctestParse(t *testing.T) {
	t.Run("blocks_with_their_place", func(t *testing.T) {
		md := "---\nchapter: 11\n---\n## Objetivo\n\n```console\n$ a\nuno\n$ b\n```\n\n```go\n$ not a command\n```\n\n" +
			"## Implementación\n\n### Sesión 2: algo (≈ 2 h)\n\n1. Paso:\n\n   ```console\n   $ c\n   dos\n     tres\n   ```\n\n" +
			":::solution\n```console\n$ d\n```\n:::\n\n## Ejecútalo\n\n```console\n$ e\n```\n"
		blocks, errs := parseConsoleBlocks([]byte(md))
		if len(errs) != 0 {
			t.Fatalf("problems: %v", errs)
		}
		type row struct {
			line      int
			session   int
			container string
			cmds      []string
			expected  [][]string
		}
		want := []row{
			{6, 0, "", []string{"a", "b"}, [][]string{{"uno"}, nil}},
			{22, 2, "", []string{"c"}, [][]string{{"dos", "  tres"}}},
			{29, 2, "solution", []string{"d"}, [][]string{nil}},
			{36, 0, "", []string{"e"}, [][]string{nil}},
		}
		if len(blocks) != len(want) {
			t.Fatalf("%d blocks, want %d: %+v", len(blocks), len(want), blocks)
		}
		for i, w := range want {
			b := blocks[i]
			if b.Line != w.line || b.Session != w.session || b.Container != w.container {
				t.Errorf("block %d: line %d session %d container %q, want %d %d %q", i, b.Line, b.Session, b.Container, w.line, w.session, w.container)
			}
			var cmds []string
			for j, c := range b.Commands {
				cmds = append(cmds, c.Cmd)
				if !slices.Equal(c.Expected, w.expected[j]) {
					t.Errorf("block %d command %d: expected %q, want %q", i, j, c.Expected, w.expected[j])
				}
			}
			if !slices.Equal(cmds, w.cmds) {
				t.Errorf("block %d: commands %q, want %q", i, cmds, w.cmds)
			}
		}
	})

	t.Run("a_line_with_less_indent_than_its_fence_loses_only_spaces", func(t *testing.T) {
		blocks, _ := parseConsoleBlocks([]byte("   ```console\n   $ a\n b\ncd\n     e\n   ```\n"))
		want := []string{"b", "cd", "  e"}
		if len(blocks) != 1 || len(blocks[0].Commands) != 1 || !slices.Equal(blocks[0].Commands[0].Expected, want) {
			t.Errorf("blocks %+v, want output %q", blocks, want)
		}
	})

	t.Run("a_command_can_go_on_in_the_next_line", func(t *testing.T) {
		blocks, _ := parseConsoleBlocks([]byte("```console\n$ a \\\n  b\nuno\n```\n"))
		if len(blocks) != 1 || len(blocks[0].Commands) != 1 || blocks[0].Commands[0].Cmd != "a \\\n  b" || !slices.Equal(blocks[0].Commands[0].Expected, []string{"uno"}) {
			t.Errorf("blocks %+v", blocks)
		}
	})

	t.Run("annotations", func(t *testing.T) {
		type row struct {
			name, info string
			mode       doctestMode
			reason     string
			problem    string
		}
		for _, r := range []row{
			{"none", "console", doctestRun, "", ""},
			{"skip", `console doctest="skip: needs a terminal"`, doctestSkip, "needs a terminal", ""},
			{"varies", `console doctest="varies: benchmark numbers"`, doctestVaries, "benchmark numbers", ""},
			{"red", `console doctest="red"`, doctestRed, "", ""},
			{"other attributes stay", `console title="x" doctest="red"`, doctestRed, "", ""},
			{"skip without a reason", `console doctest="skip"`, doctestRun, "", "1: doctest annotation `skip` needs a reason: `skip: why`"},
			{"varies without a reason", `console doctest="varies: "`, doctestRun, "", "1: doctest annotation `varies: ` needs a reason: `varies: why`"},
			{"an unknown one", `console doctest="maybe"`, doctestRun, "", "1: unknown doctest annotation `maybe`; want `skip: why`, `varies: why` or `red`"},
		} {
			blocks, errs := parseConsoleBlocks([]byte("```" + r.info + "\n$ a\n```\n"))
			if len(blocks) != 1 || blocks[0].Mode != r.mode || blocks[0].Reason != r.reason {
				t.Errorf("%s: blocks %+v", r.name, blocks)
			}
			var got string
			if len(errs) > 0 {
				got = errs[0].String()
			}
			if want := r.problem; want != "" {
				want = ":" + want
				if got != want {
					t.Errorf("%s: problem %q, want %q", r.name, got, want)
				}
			} else if got != "" {
				t.Errorf("%s: problem %q, want none", r.name, got)
			}
		}
	})
}

func TestDoctestMatch(t *testing.T) {
	type row struct {
		name          string
		expected, got []string
		varies        bool
		want          bool
	}
	lines := func(s ...string) []string { return s }
	rows := []row{
		{"same", lines("uno", "dos"), lines("uno", "dos"), false, true},
		{"another line", lines("uno", "dos"), lines("uno", "tres"), false, false},
		{"one line more", lines("uno"), lines("uno", "dos"), false, false},
		{"one line less", lines("uno", "dos"), lines("uno"), false, false},
		{"no output at all", nil, nil, false, true},
		{"output where none is shown", nil, lines("uno"), false, false},
		{"blank lines at the end", lines("uno", ""), lines("uno", "", ""), false, true},
		{"spaces at the end of a line", lines("uno"), lines("uno  "), false, true},
		{"spaces at the start count", lines("uno"), lines("  uno"), false, false},
		{"a go test time", lines("ok  \tpkg\t0.003s"), lines("ok  \tpkg\t1.250s"), false, true},
		{"cached is a time", lines("ok  \tpkg\t0.003s"), lines("ok  \tpkg\t(cached)"), false, true},
		{"a time in a test line", lines("--- FAIL: T (0.00s)"), lines("--- FAIL: T (3.21s)"), false, true},
		{"milliseconds", lines("took 12ms"), lines("took 3.5µs"), false, true},
		{"a time is not any number", lines("a 1.5"), lines("a 2.5"), false, false},
		{"an ellipsis line takes any lines", lines("a", "…", "z"), lines("a", "b", "c", "z"), false, true},
		{"an ellipsis line takes no line", lines("a", "…", "z"), lines("a", "z"), false, true},
		{"what follows it still counts", lines("a", "…", "z"), lines("a", "b"), false, false},
		{"three dots too", lines("a", "...", "z"), lines("a", "b", "z"), false, true},
		{"an ellipsis in a line takes any text", lines("no files in …/core"), lines("no files in /x/y/core"), false, true},
		{"the rest of that line counts", lines("no files in …/core"), lines("no files in /x/y/base"), false, false},
		{"text with regexp marks", lines("a.b(c)*"), lines("a.b(c)*"), false, true},
		{"those marks are not patterns", lines("a.b"), lines("axb"), false, false},
		{"varies: numbers", lines("BenchmarkX  100  12 ns/op"), lines("BenchmarkX  2000000  7 ns/op"), true, true},
		{"varies: words still count", lines("BenchmarkX  100"), lines("BenchmarkY  100"), true, false},
		{"without it numbers count", lines("BenchmarkX  100"), lines("BenchmarkX  200"), false, false},
	}
	t.Run("rows", func(t *testing.T) {
		for _, r := range rows {
			if got := doctestMatch(r.expected, r.got, r.varies); got != r.want {
				t.Errorf("%s: doctestMatch = %v, want %v", r.name, got, r.want)
			}
		}
	})
}

// doctestRepo makes a repository with two sessions of chapter 11. The session
// tags hold what the fences of the tests below expect.
func doctestRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := newRepo(t)
	commit := func(tag string, files map[string]string) {
		t.Helper()
		for p, c := range files {
			writeFile(t, dir, p, c)
		}
		gitIn(t, dir, "add", "-A")
		gitIn(t, dir, "commit", "-q", "-m", tag)
		gitIn(t, dir, "tag", tag)
	}
	commit("ch10-s1", map[string]string{"code.txt": "ch10\n", "x_test.go": "old test\n", "only10.txt": "ten\n"})
	commit("ch11-s1", map[string]string{"code.txt": "s1\n", "t_test.go": "s1 test\n"})
	commit("ch11-s2", map[string]string{"code.txt": "s2\n", "t_test.go": "s2 test\n"})
	return dir
}

// doctestChapterRun writes the chapter in the repository and runs the command on it. It
// replaces the memory cap with nothing, because a test cannot count on systemd.
func doctestChapterRun(t *testing.T, repo, name, md string) (int, string, string) {
	t.Helper()
	noCap(t)
	p := filepath.Join(repo, "book", name)
	writeFile(t, repo, "book/"+name, md)
	code, stdout, stderr := runBookx(t, "doctest", "--repo", repo, p)
	return code, strings.ReplaceAll(stdout, p, name), stderr
}

// noCap makes the capped runs of the test run bare.
func noCap(t *testing.T) {
	t.Helper()
	old := capCommand
	capCommand = func(unit string, argv []string) ([]string, error) { return argv, nil }
	t.Cleanup(func() { capCommand = old })
}

func TestDoctestRun(t *testing.T) {
	session := func(k, body string) string {
		return "## Implementación\n\n### Sesión " + k + ": algo (≈ 2 h)\n\n" + body + "\n"
	}
	fence := func(body string) string { return "```console\n" + body + "\n```\n" }

	t.Run("a_session_block_runs_at_its_session_tag", func(t *testing.T) {
		code, out, errOut := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", session("1", fence("$ cat code.txt\ns1")))
		want := "doctest ch11-x.md: 1 commands: 1 ok, 0 differ, 0 inputs, 0 skipped, 0 shape only, 0 blocks without a command\n"
		if code != exitOK || out != want || errOut != "" {
			t.Errorf("code %d, stdout %q, stderr %q; want 0, %q, empty", code, out, errOut, want)
		}
	})

	t.Run("any_other_block_runs_at_the_chapter_code", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", "## Objetivo\n\n"+fence("$ cat code.txt\ns2"))
		if code != exitOK || !strings.Contains(out, "1 ok") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_session_with_no_tag_runs_at_the_chapter_code", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", session("9", fence("$ cat code.txt\ns2")))
		if code != exitOK || !strings.Contains(out, "1 ok") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("the_chapter_tag_is_the_chapter_code", func(t *testing.T) {
		repo := doctestRepo(t)
		writeFile(t, repo, "code.txt", "chapter\n")
		gitIn(t, repo, "add", "-A")
		gitIn(t, repo, "commit", "-q", "-m", "prose")
		gitIn(t, repo, "tag", "ch11")
		code, out, _ := doctestChapterRun(t, repo, "ch11-x.md", "## Objetivo\n\n"+fence("$ cat code.txt\nchapter"))
		if code != exitOK || !strings.Contains(out, "1 ok") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_difference_fails_from_chapter_11_on", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", session("1", fence("$ cat code.txt\nwrong")))
		want := "ch11-x.md:6: differ at ch11-s1: $ cat code.txt\n  - wrong\n  + s1\n" +
			"doctest ch11-x.md: 1 commands: 0 ok, 1 differ, 0 inputs, 0 skipped, 0 shape only, 0 blocks without a command\n"
		if code != exitFail || out != want {
			t.Errorf("code %d, stdout %q; want 1, %q", code, out, want)
		}
	})

	t.Run("an_earlier_chapter_is_only_reported", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch10-x.md", fence("$ cat code.txt\nwrong"))
		want := "ch10-x.md:2: differ at ch10-s1: $ cat code.txt\n  - wrong\n  + ch10\n" +
			"doctest ch10-x.md: 1 commands: 0 ok, 1 differ, 0 inputs, 0 skipped, 0 shape only, 0 blocks without a command (report only: before chapter 11)\n"
		if code != exitOK || out != want {
			t.Errorf("code %d, stdout %q; want 0, %q", code, out, want)
		}
	})

	t.Run("it_runs_in_a_clean_copy_of_the_tag", func(t *testing.T) {
		repo := doctestRepo(t)
		writeFile(t, repo, "code.txt", "dirty working tree\n")
		code, out, _ := doctestChapterRun(t, repo, "ch11-x.md", session("1", fence("$ cat code.txt\ns1\n$ test -f book/ch11-x.md; echo $?\n1")))
		if code != exitOK || !strings.Contains(out, "2 ok") {
			t.Errorf("code %d, stdout %q", code, out)
		}
		if got, _ := os.ReadFile(filepath.Join(repo, "code.txt")); string(got) != "dirty working tree\n" {
			t.Errorf("the working tree changed: %q", got)
		}
	})

	t.Run("the_copy_has_no_bookx_because_the_reader_has_none", func(t *testing.T) {
		repo := doctestRepo(t)
		writeFile(t, repo, "cmd/bookx/main.go", "package main\n")
		writeFile(t, repo, "cmd/other/main.go", "package main\n")
		gitIn(t, repo, "add", "-A")
		gitIn(t, repo, "commit", "-q", "-m", "tools")
		gitIn(t, repo, "tag", "ch11-s3")
		code, out, _ := doctestChapterRun(t, repo, "ch11-x.md", fence("$ test -e cmd/bookx; echo $?\n1\n$ test -e cmd/other; echo $?\n0"))
		if code != exitOK || !strings.Contains(out, "2 ok") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("commands_of_a_block_share_a_shell", func(t *testing.T) {
		md := fence("$ mkdir -p sub && cd sub\n$ pwd | sed 's|.*/||'\nsub\n$ false\n$ echo $?\n1\n$ printf 'no newline'\nno newline\n$ echo after\nafter")
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", md)
		if code != exitOK || !strings.Contains(out, "6 ok") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("stderr_is_part_of_the_output", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", fence("$ sh -c 'echo a; echo b >&2; echo c'\na\nb\nc"))
		if code != exitOK || !strings.Contains(out, "1 ok") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_cat_of_a_file_the_tag_lacks_is_an_input", func(t *testing.T) {
		md := "## Objetivo\n\n" + fence("$ cat new.ki\nprimera\nsegunda\n$ wc -l < new.ki\n2") +
			"\n## Teoría\n\n" + fence("$ wc -l < new.ki\n2\n$ cat new.ki\nsola\n$ wc -l < new.ki\n1")
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", md)
		if code != exitOK || !strings.Contains(out, "3 ok, 0 differ, 2 inputs") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("an_input_reaches_the_copies_of_later_tags", func(t *testing.T) {
		md := session("1", fence("$ cat new.ki\nuna")) + "### Sesión 2: otra (≈ 1 h)\n\n" + fence("$ cat new.ki\nuna\n$ wc -l < new.ki\n1")
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", md)
		if code != exitOK || !strings.Contains(out, "1 ok, 0 differ, 2 inputs") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_cat_outside_the_tree_is_a_command", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", fence("$ cat ../escape.txt\nsecreto"))
		if code != exitFail || !strings.Contains(out, "0 inputs") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("the_cap_never_sees_the_text_of_a_block", func(t *testing.T) {
		repo := doctestRepo(t)
		writeFile(t, repo, "book/ch11-x.md", fence("$ cat new.ki\nprint(\"${n}\")\n$ cat new.ki\nprint(\"${n}\")\n$ echo ${HOME:+set} '${n}'\nset ${n}"))
		// systemd-run expands `${x}` in its arguments, so none can hold one.
		old := capCommand
		capCommand = func(_ string, argv []string) ([]string, error) {
			for _, a := range argv {
				if strings.Contains(a, "${") {
					t.Errorf("the cap is given %q", a)
				}
			}
			return argv, nil
		}
		t.Cleanup(func() { capCommand = old })
		code, out, _ := runBookx(t, "doctest", "--repo", repo, filepath.Join(repo, "book", "ch11-x.md"))
		if code != exitOK || !strings.Contains(out, "1 ok, 0 differ, 2 inputs") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("skip_runs_nothing_and_says_why", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", "```console doctest=\"skip: needs a terminal\"\n$ false\nno se ejecuta\n```\n")
		want := "ch11-x.md:1: skip: needs a terminal\n" +
			"doctest ch11-x.md: 1 commands: 0 ok, 0 differ, 0 inputs, 1 skipped, 0 shape only, 0 blocks without a command\n"
		if code != exitOK || out != want {
			t.Errorf("code %d, stdout %q; want 0, %q", code, out, want)
		}
	})

	t.Run("varies_compares_the_shape", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", "```console doctest=\"varies: a count\"\n$ wc -l < code.txt\n77\n$ echo word\nother\n```\n")
		if code != exitFail || !strings.Contains(out, "1 ok, 1 differ, 0 inputs, 0 skipped, 2 shape only") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("red_runs_the_previous_tag_with_this_session_tests", func(t *testing.T) {
		red := "```console doctest=\"red\"\n$ cat code.txt\ns1\n$ cat t_test.go\ns2 test\n```\n"
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", session("2", red))
		if code != exitOK || !strings.Contains(out, "2 ok") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("red_of_a_first_session_starts_from_the_chapter_before", func(t *testing.T) {
		red := "```console doctest=\"red\"\n$ cat code.txt\nch10\n$ cat t_test.go\ns1 test\n$ cat x_test.go\nold test\n```\n"
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", session("1", red))
		if code != exitOK || !strings.Contains(out, "3 ok") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("red_needs_a_session_and_a_tag_before", func(t *testing.T) {
		red := "```console doctest=\"red\"\n$ echo uno\nuno\n```\n"
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", red)
		if code != exitFail || !strings.Contains(out, "ch11-x.md:1: red: this block is in no session") {
			t.Errorf("no session: code %d, stdout %q", code, out)
		}
		code, out, _ = doctestChapterRun(t, doctestRepo(t), "ch10-x.md", session("1", red))
		if code != exitOK || !strings.Contains(out, "ch10-x.md:5: red: no tag comes before ch10-s1") {
			t.Errorf("no tag before: code %d, stdout %q", code, out)
		}
	})

	t.Run("a_bad_annotation_fails_from_chapter_11_on", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", "```console doctest=\"skip\"\n$ true\n```\n")
		if code != exitFail || !strings.Contains(out, "ch11-x.md:1: doctest annotation `skip` needs a reason") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_block_without_a_command_is_counted", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", fence("salida suelta"))
		if code != exitOK || !strings.Contains(out, "1 blocks without a command") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_chapter_with_no_tag_cannot_be_run", func(t *testing.T) {
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch12-x.md", fence("$ true"))
		if code != exitFail || !strings.Contains(out, "ch12-x.md:1: chapter 12 has no tag `ch12` and no session tag") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_tag_of_another_go_is_skipped", func(t *testing.T) {
		repo := doctestRepo(t)
		writeFile(t, repo, "go.mod", "module x\n\ntoolchain go1.2.3\n")
		gitIn(t, repo, "add", "-A")
		gitIn(t, repo, "commit", "-q", "-m", "go.mod")
		gitIn(t, repo, "tag", "ch11-s3")
		code, out, _ := doctestChapterRun(t, repo, "ch11-x.md", fence("$ false\nnever"))
		want := "ch11-x.md:1: skip: ch11-s3 needs go1.2.3\n"
		if code != exitOK || !strings.HasPrefix(out, want) || !strings.Contains(out, "1 skipped") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("a_command_that_hangs_is_stopped", func(t *testing.T) {
		old := doctestTimeoutSeconds
		doctestTimeoutSeconds = "1"
		t.Cleanup(func() { doctestTimeoutSeconds = old })
		code, out, _ := doctestChapterRun(t, doctestRepo(t), "ch11-x.md", fence("$ echo before\nbefore\n$ sleep 60\n$ echo after\nafter"))
		if code != exitFail || !strings.Contains(out, "1 ok, 2 differ") || !strings.Contains(out, "stopped: exit 124") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("it_leaves_nothing_in_build", func(t *testing.T) {
		repo := doctestRepo(t)
		doctestChapterRun(t, repo, "ch11-x.md", fence("$ true"))
		if entries, err := os.ReadDir(filepath.Join(repo, "build", "doctest")); err != nil || len(entries) != 0 {
			t.Errorf("build/doctest holds %v (err %v)", entries, err)
		}
	})

	t.Run("a_name_that_is_not_a_chapter", func(t *testing.T) {
		noCap(t)
		repo := doctestRepo(t)
		code, _, errOut := runBookx(t, "doctest", "--repo", repo, "book/notes.md")
		if code != exitFail || errOut != "bookx doctest: book/notes.md: not a chapter (chNN-slug.md)\n" {
			t.Errorf("code %d, stderr %q", code, errOut)
		}
	})

	t.Run("no_cap_no_run", func(t *testing.T) {
		repo := doctestRepo(t)
		writeFile(t, repo, "book/ch11-x.md", fence("$ touch ran.txt"))
		old := capCommand
		capCommand = func(string, []string) ([]string, error) { return nil, errors.New("no cap here") }
		t.Cleanup(func() { capCommand = old })
		code, out, errOut := runBookx(t, "doctest", "--repo", repo, filepath.Join(repo, "book", "ch11-x.md"))
		if code != exitFail || out != "" || errOut != "bookx doctest: no cap here\n" {
			t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
		}
	})
}

func TestCapCommand(t *testing.T) {
	t.Run("the_cap_of_the_process_rules", func(t *testing.T) {
		got, err := systemdCap("ki-test-1", []string{"go", "test"})
		if err != nil {
			t.Skipf("no systemd-run here: %v", err)
		}
		want := []string{"systemd-run", "--user", "--scope", "-q", "--unit=ki-test-1", "-p", "MemoryMax=4G", "-p", "MemorySwapMax=0", "-p", "OOMPolicy=continue", "go", "test"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("without_systemd_it_refuses", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := systemdCap("ki-test-1", []string{"go", "test"})
		if err == nil || !strings.Contains(err.Error(), "the memory cap needs systemd-run") {
			t.Errorf("error %v", err)
		}
	})
}

func TestCappedRun(t *testing.T) {
	run := func(t *testing.T, script string) ([]byte, int) {
		t.Helper()
		noCap(t)
		out, exit, err := cappedRun{Unit: "u", Dir: t.TempDir(), Env: cleanEnv(), Seconds: "10", Argv: []string{"sh", "-c", script}}.run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return out, exit
	}
	t.Run("merges_the_streams_and_gives_the_exit_code", func(t *testing.T) {
		out, exit := run(t, "echo a; echo b >&2; echo c; exit 3")
		if string(out) != "a\nb\nc\n" || exit != 3 {
			t.Errorf("output %q, exit %d", out, exit)
		}
	})
	t.Run("a_kill_is_reported_as_a_shell_does", func(t *testing.T) {
		if _, exit := run(t, "kill -9 $$"); exit != exitKilled {
			t.Errorf("exit %d, want %d", exit, exitKilled)
		}
	})
	t.Run("output_without_end_stays_at_the_limit", func(t *testing.T) {
		out, exit := run(t, fmt.Sprintf("head -c %d /dev/zero", maxCapturedOutput+1000))
		if len(out) != maxCapturedOutput || exit != 0 {
			t.Errorf("%d bytes, exit %d; want %d bytes, exit 0", len(out), exit, maxCapturedOutput)
		}
	})
	t.Run("the_cap_comes_first_and_is_given_the_unit", func(t *testing.T) {
		var unit string
		var argv []string
		old := capCommand
		capCommand = func(u string, a []string) ([]string, error) {
			unit, argv = u, a
			return a, nil
		}
		t.Cleanup(func() { capCommand = old })
		if _, _, err := (cappedRun{Unit: "ki-x-1", Dir: t.TempDir(), Seconds: "7", Argv: []string{"true"}}).run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if want := []string{"timeout", "-k", "1", "7", "true"}; unit != "ki-x-1" || !slices.Equal(argv, want) {
			t.Errorf("unit %q, argv %q; want %q", unit, argv, want)
		}
	})
	t.Run("clean_env_takes_out_what_changes_ki", func(t *testing.T) {
		t.Setenv("KI_STACK_MB", "1")
		t.Setenv("KI_DEBUG_PANIC_IN", "lint")
		t.Setenv("GOFLAGS", "-race")
		env := cleanEnv()
		for _, kv := range env {
			if strings.HasPrefix(kv, "KI_") || kv == "GOFLAGS=-race" {
				t.Errorf("%s stays", kv)
			}
		}
		for _, want := range []string{"GOTOOLCHAIN=local", "GOFLAGS=", "GOENV=off", "GOWORK=off", "LC_ALL=C.UTF-8"} {
			if !slices.Contains(env, want) {
				t.Errorf("%s is missing", want)
			}
		}
	})
}

func TestScratch(t *testing.T) {
	t.Run("removes_its_directory", func(t *testing.T) {
		repo := t.TempDir()
		dir, done, err := newScratch(repo, "demo")
		if err != nil {
			t.Fatal(err)
		}
		if got := filepath.Dir(dir); got != filepath.Join(repo, "build", "demo") {
			t.Errorf("dir %s", dir)
		}
		writeFile(t, dir, "sub/file", "x")
		done()
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", dir, err)
		}
	})
	t.Run("sweeps_what_a_dead_run_left", func(t *testing.T) {
		repo := t.TempDir()
		base := filepath.Join(repo, "build", "demo")
		// A shell that has exited gives a pid that nothing holds any more.
		cmd := exec.Command("true")
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		dead := filepath.Join(base, "run-dead")
		writeFile(t, dead, "pid", fmt.Sprint(cmd.Process.Pid))
		live := filepath.Join(base, "run-live")
		writeFile(t, live, "pid", fmt.Sprint(os.Getpid()))
		other := filepath.Join(base, "keep.log")
		writeFile(t, base, "keep.log", "x")
		_, done, err := newScratch(repo, "demo")
		if err != nil {
			t.Fatal(err)
		}
		defer done()
		if _, err := os.Stat(dead); !os.IsNotExist(err) {
			t.Errorf("the dead run is still there: %v", err)
		}
		if _, err := os.Stat(live); err != nil {
			t.Errorf("the live run is gone: %v", err)
		}
		if _, err := os.Stat(other); err != nil {
			t.Errorf("another file is gone: %v", err)
		}
	})
}
