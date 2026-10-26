// bookx: not-in-book

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// runBookx runs the command line in process and returns its exit code, stdout
// and stderr.
func runBookx(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// repoFromFixture makes a real git repository out of the fixture: one commit
// and one annotated tag for each tag of the fixture, in the order of the book,
// and then the working tree of the fixture with a verified-tags.txt that holds
// the real SHAs of the commits. It returns the path of the repository.
func repoFromFixture(t *testing.T, f *fixture) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := newRepo(t)
	clear := func() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() != ".git" {
				if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	order := []string{"ch01-s1", "ch01-s2", "ch01", "ch02-s1", "ch02"}
	var verified strings.Builder
	for _, tag := range order {
		clear()
		for p, file := range f.tags[tag] {
			writeFile(t, dir, p, string(file.Data))
		}
		gitIn(t, dir, "add", "-A")
		gitIn(t, dir, "commit", "--allow-empty", "-m", tag)
		gitIn(t, dir, "tag", "-a", tag, "-m", tag)
		verified.WriteString(tag + " " + gitIn(t, dir, "rev-parse", tag+"^{commit}") + " 2026-10-26 go1.27.3\n")
	}
	clear()
	for p, file := range f.tree {
		writeFile(t, dir, p, string(file.Data))
	}
	writeFile(t, dir, "book/verified-tags.txt", verified.String())
	return dir
}

func TestRun(t *testing.T) {
	t.Run("usage_errors", func(t *testing.T) {
		const top = "usage: bookx <command> [args]\ncommands: verify, render, regions, testlist\n"
		rows := []struct {
			name   string
			args   []string
			stderr string
		}{
			{"no command", nil, top},
			{"unknown command", []string{"frobnicate"}, top},
			{"verify with an argument", []string{"verify", "extra"}, "bookx verify: want 0 argument(s), found 1\nusage: bookx verify [--repo DIR]\n"},
			{"verify with an unknown flag", []string{"verify", "--bogus"}, "bookx verify: flag provided but not defined: -bogus\nusage: bookx verify [--repo DIR]\n"},
			{"verify with --repo and no value", []string{"verify", "--repo"}, "bookx verify: flag needs an argument: -repo\nusage: bookx verify [--repo DIR]\n"},
			{"render without a directory", []string{"render"}, "bookx render: want 1 argument(s), found 0\nusage: bookx render <out-dir> [--repo DIR]\n"},
			{"render with two directories", []string{"render", "a", "b"}, "bookx render: want 1 argument(s), found 2\nusage: bookx render <out-dir> [--repo DIR]\n"},
			{"regions without a tag", []string{"regions"}, "bookx regions: want 1 argument(s), found 0\nusage: bookx regions <tag> [--repo DIR]\n"},
			{"testlist without a file", []string{"testlist"}, "bookx testlist: want 1 argument(s), found 0\nusage: bookx testlist <go-test-json-file>\n"},
			{"testlist has no --repo", []string{"testlist", "--repo", "d", "f"}, "bookx testlist: flag provided but not defined: -repo\nusage: bookx testlist <go-test-json-file>\n"},
		}
		for _, r := range rows {
			code, stdout, stderr := runBookx(t, r.args...)
			if code != 2 || stdout != "" || stderr != r.stderr {
				t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit 2, no stdout, stderr %q", r.name, code, stdout, stderr, r.stderr)
			}
		}
	})

	t.Run("verify_on_a_git_repository", func(t *testing.T) {
		f := cleanFixture(t)
		dir := repoFromFixture(t, f)

		code, stdout, stderr := runBookx(t, "verify", "--repo", dir)
		if code != 0 || stdout != "bookx verify: OK\n" || stderr != "" {
			t.Fatalf("verify: exit %d, stdout %q, stderr %q; want exit 0 and \"bookx verify: OK\\n\"", code, stdout, stderr)
		}

		// The SHA of a tag is the one of its commit, not of the tag object: with
		// the second one in verified-tags.txt the tag would not match.
		tagObject := gitIn(t, dir, "rev-parse", "ch02-s1")
		commit := gitIn(t, dir, "rev-parse", "ch02-s1^{commit}")
		path := filepath.Join(dir, "book", "verified-tags.txt")
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(text), commit, tagObject, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		line := f.lineOf("book/ch02-lexer.md", "{{file ch02-s1")
		wantOut := "book/ch02-lexer.md:" + strconv.Itoa(line) + ": tag `ch02-s1` points to " + commit + ", but book/verified-tags.txt verified " + tagObject + ": run ci/check-tags.sh 'ch02-s1'\n"
		code, stdout, stderr = runBookx(t, "verify", "--repo", dir)
		if code != 1 || stdout != wantOut || stderr != "" {
			t.Errorf("verify with the SHA of the tag object: exit %d, stdout %q, stderr %q; want exit 1 and %q", code, stdout, stderr, wantOut)
		}

		// A directory that is not a repository is an error of the tool, not a problem of the book.
		code, stdout, stderr = runBookx(t, "verify", "--repo", t.TempDir())
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "bookx verify: git tag: ") {
			t.Errorf("verify outside a repository: exit %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})

	t.Run("render_writes_only_the_chapters", func(t *testing.T) {
		dir := repoFromFixture(t, cleanFixture(t))
		out := filepath.Join(t.TempDir(), "out")

		// The flag may come after the directory, or before it.
		for _, args := range [][]string{{"render", out, "--repo", dir}, {"render", "--repo", dir, out}} {
			if err := os.RemoveAll(out); err != nil {
				t.Fatal(err)
			}
			code, stdout, stderr := runBookx(t, args...)
			if code != 0 || stdout != "" || stderr != "" {
				t.Fatalf("%v: exit %d, stdout %q, stderr %q; want a quiet exit 0", args, code, stdout, stderr)
			}
			entries, err := os.ReadDir(out)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			if want := []string{"ch01.md", "ch02.md"}; !slices.Equal(names, want) {
				t.Fatalf("%v wrote %q, want exactly %q", args, names, want)
			}
		}

		ch1, err := os.ReadFile(filepath.Join(out, "ch01.md"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"chapter: 1\ntitle: El taller\n", // the front matter stays
			"```text title=\"go.mod\" tag=\"ch01-s1\" mode=\"file\"\nmodule github.com/aldezex/kilang\n",
			// The diff is the real output of git.
			"```diff title=\"internal/driver/cli.go\" tag=\"ch01-s1..ch01-s2\" mode=\"diff\"\ndiff --git a/internal/driver/cli.go b/internal/driver/cli.go\n",
			"-\treturn 0\n+\treturn 1\n }\n",
			"| T01.01 | `Main` con el único argumento `--version` | es lo primero que ejecutas tras compilar `ki` | código de salida 0 | core |\n",
			":::exercise\nCambia el código de salida.\n:::\n", // containers are left as they are
		} {
			if !strings.Contains(string(ch1), want) {
				t.Errorf("ch01.md does not contain %q", want)
			}
		}
		if strings.Contains(string(ch1), "{{") {
			t.Error("ch01.md still has a directive")
		}

		// With a directive that does not resolve nothing is written.
		writeFile(t, dir, "book/ch01-taller.md", "{{file ch01-s1 nope.go}}\n")
		bad := filepath.Join(t.TempDir(), "bad")
		code, stdout, stderr := runBookx(t, "render", bad, "--repo", dir)
		if want := "book/ch01-taller.md:1: `nope.go` is not in tag `ch01-s1`\n"; code != 1 || stdout != want || stderr != "" {
			t.Errorf("render with a bad directive: exit %d, stdout %q, stderr %q; want exit 1 and %q", code, stdout, stderr, want)
		}
		if _, err := os.Stat(bad); err == nil {
			t.Error("render created the output directory although a directive failed")
		}
	})

	t.Run("regions_lists_the_markers_of_a_tag", func(t *testing.T) {
		dir := repoFromFixture(t, cleanFixture(t))
		code, stdout, stderr := runBookx(t, "regions", "ch02-s1", "--repo", dir)
		want := "driver.main full internal/driver/cli.go:4\ndriver.usage hint internal/driver/cli.go:12\nlexer.line_rule hint internal/syntax/lexer.go:4\n"
		if code != 0 || stdout != want || stderr != "" {
			t.Errorf("regions: exit %d, stdout %q, stderr %q; want exit 0 and %q", code, stdout, stderr, want)
		}

		code, stdout, stderr = runBookx(t, "regions", "ch09-s9", "--repo", dir)
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "bookx regions: git ls-tree: ") {
			t.Errorf("regions of a tag that does not exist: exit %d, stdout %q, stderr %q", code, stdout, stderr)
		}

		// A broken marker is reported on stderr, the good regions are still listed.
		writeFile(t, dir, "extra.go", "package x\n\n// "+"@book extra.open full\n")
		gitIn(t, dir, "add", "extra.go")
		gitIn(t, dir, "commit", "-m", "extra")
		gitIn(t, dir, "tag", "-a", "extra", "-m", "extra")
		code, stdout, stderr = runBookx(t, "regions", "extra", "--repo", dir)
		if wantErr := "extra:extra.go:3: region `extra.open` is never closed: it needs an @end line\n"; code != 1 || stderr != wantErr || !strings.Contains(stdout, "driver.main full") {
			t.Errorf("regions with a broken marker: exit %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})

	t.Run("testlist_prints_the_list", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "go-test.json")
		input := goTestEvents("pass", "p", "TestB", "TestA", "TestA/x") + goTestEvents("fail", "p", "TestC")
		if err := os.WriteFile(file, []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runBookx(t, "testlist", file)
		if want := "p TestA/x\np TestB\n"; code != 0 || stdout != want || stderr != "" {
			t.Errorf("testlist: exit %d, stdout %q, stderr %q; want exit 0 and %q", code, stdout, stderr, want)
		}

		code, stdout, stderr = runBookx(t, "testlist", filepath.Join(t.TempDir(), "missing.json"))
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "bookx testlist: ") {
			t.Errorf("testlist of a missing file: exit %d, stdout %q, stderr %q", code, stdout, stderr)
		}

		if err := os.WriteFile(file, []byte("not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr = runBookx(t, "testlist", file)
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "bookx testlist: go test -json output: ") {
			t.Errorf("testlist of a file that is not JSON: exit %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})
}
