// bookx: not-in-book

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// gitIn runs git in dir and returns its trimmed output. The identity and the
// signing options are passed on the command line, and the user's own git
// configuration is ignored, so the test does not depend on the machine.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-C", dir,
		"-c", "user.name=Test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo creates an empty repository in a temporary directory, on a branch
// called main, and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-b", "main")
	return dir
}

// writeFile writes content at path p inside dir, creating its directories.
func writeFile(t *testing.T, dir, p, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGit(t *testing.T) {
	// The environment is set for the whole test, so Git itself, which does not
	// pass options of its own, runs with no user configuration.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "one\n")
	writeFile(t, dir, "dir/b.txt", "bee\n")
	writeFile(t, dir, "é.txt", "accent\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-m", "first")
	first := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "tag", "-a", "v1", "-m", "version one")

	writeFile(t, dir, "a.txt", "two\n")
	writeFile(t, dir, "c.txt", "see\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-m", "second")
	second := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "tag", "-a", "v2", "-m", "version two")
	gitIn(t, dir, "tag", "a0") // a lightweight tag that sorts before the others

	g := Git{Repo: dir}

	t.Run("show", func(t *testing.T) {
		for _, c := range []struct{ tag, path, want string }{
			{"v1", "a.txt", "one\n"},
			{"v2", "a.txt", "two\n"},
			{"v2", "dir/b.txt", "bee\n"},
		} {
			got, err := g.Show(c.tag, c.path)
			if err != nil || string(got) != c.want {
				t.Errorf("Show(%q, %q) = %q, %v; want %q", c.tag, c.path, got, err, c.want)
			}
		}
		if got, err := g.Show("v1", "c.txt"); err == nil {
			t.Errorf("Show(v1, c.txt) = %q, nil; want an error: the file is not in v1", got)
		}
		if got, err := g.Show("v9", "a.txt"); err == nil {
			t.Errorf("Show(v9, a.txt) = %q, nil; want an error: there is no tag v9", got)
		}
	})

	t.Run("files", func(t *testing.T) {
		got, err := g.Files("v2")
		want := []string{"a.txt", "c.txt", "dir/b.txt", "é.txt"}
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Files(v2) = %q, %v; want %q", got, err, want)
		}
		got, err = g.Files("v1")
		want = []string{"a.txt", "dir/b.txt", "é.txt"}
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Files(v1) = %q, %v; want %q", got, err, want)
		}
		if got, err := g.Files("v9"); err == nil {
			t.Errorf("Files(v9) = %q, nil; want an error", got)
		}
	})

	t.Run("rev_is_the_commit_not_the_tag_object", func(t *testing.T) {
		tagObject := gitIn(t, dir, "rev-parse", "v1")
		if tagObject == first {
			t.Fatal("v1 should be an annotated tag, whose object differs from its commit")
		}
		for _, c := range []struct{ tag, want string }{{"v1", first}, {"v2", second}} {
			got, err := g.Rev(c.tag)
			if err != nil || got != c.want {
				t.Errorf("Rev(%q) = %q, %v; want the commit %q", c.tag, got, err, c.want)
			}
		}
		// A lightweight tag points straight at a commit.
		if got, err := g.Rev("a0"); err != nil || got != second {
			t.Errorf("Rev(a0) = %q, %v; want %q", got, err, second)
		}
		if got, err := g.Rev("v9"); err == nil {
			t.Errorf("Rev(v9) = %q, nil; want an error", got)
		}
	})

	t.Run("diff", func(t *testing.T) {
		got, err := g.Diff("v1", "v2", "a.txt")
		if err != nil {
			t.Fatalf("Diff(v1, v2, a.txt): %v", err)
		}
		text := string(got)
		for _, want := range []string{"diff --git a/a.txt b/a.txt\n", "@@ -1 +1 @@\n-one\n+two\n"} {
			if !strings.Contains(text, want) {
				t.Errorf("Diff(v1, v2, a.txt) = %q, want it to contain %q", text, want)
			}
		}
		if strings.Contains(text, "c.txt") {
			t.Errorf("Diff(v1, v2, a.txt) = %q, but it must be limited to a.txt", text)
		}
		same, err := g.Diff("v1", "v2", "dir/b.txt")
		if err != nil || len(same) != 0 {
			t.Errorf("Diff(v1, v2, dir/b.txt) = %q, %v; want empty output for an unchanged file", same, err)
		}
	})

	t.Run("diff_ignores_the_user_configuration", func(t *testing.T) {
		// Each pair of files is one that a setting changes: the context and the
		// prefixes show in every diff; the algorithm, the indent heuristic and
		// the inter-hunk context only in the files made to need them.
		numbered := func(changed ...int) string {
			var sb strings.Builder
			for i := 1; i <= 20; i++ {
				sb.WriteString("line " + strconv.Itoa(i))
				if slices.Contains(changed, i) {
					sb.WriteString(" changed")
				}
				sb.WriteString("\n")
			}
			return sb.String()
		}
		pairs := []struct{ name, old, new string }{
			{"long.txt", numbered(), numbered(10)},
			{"two_hunks.txt", numbered(), numbered(4, 12)},
			{"algorithm.txt", "}\n}\n\n\n}\n\n}\n}\nx\n", "}\n}\n\n\n}\n}\n}\n\nx\n"},
			{"indent.txt", "  b\nx\n}\nint f() {\ny\n  a\nx\n{\ny\n{\nx\n}\n}\nx\n", "int f() {\n  b\nx\n}\nint f() {\ny\ny\n  a\nx\n{\ny\n{\nx\n}\n}\nx\n"},
		}
		cdir := newRepo(t)
		for _, p := range pairs {
			writeFile(t, cdir, p.name, p.old)
		}
		gitIn(t, cdir, "add", "-A")
		gitIn(t, cdir, "commit", "-m", "one")
		gitIn(t, cdir, "tag", "c1")
		for _, p := range pairs {
			writeFile(t, cdir, p.name, p.new)
		}
		gitIn(t, cdir, "add", "-A")
		gitIn(t, cdir, "commit", "-m", "two")
		gitIn(t, cdir, "tag", "c2")

		g := Git{Repo: cdir}
		plain := map[string][]byte{}
		for _, p := range pairs {
			out, err := g.Diff("c1", "c2", p.name)
			if err != nil {
				t.Fatalf("Diff(c1, c2, %s): %v", p.name, err)
			}
			plain[p.name] = out
		}
		for _, want := range []string{"diff --git a/long.txt b/long.txt\n", "@@ -7,7 +7,7 @@", " line 9\n-line 10\n+line 10 changed\n line 11\n"} {
			if !strings.Contains(string(plain["long.txt"]), want) {
				t.Fatalf("Diff(c1, c2, long.txt) = %q, want it to contain %q", plain["long.txt"], want)
			}
		}

		// The same repository under a configuration that changes every default.
		cfg := filepath.Join(t.TempDir(), "gitconfig")
		hostile := "[diff]\n\talgorithm = histogram\n\tnoprefix = true\n\tmnemonicPrefix = true\n\tcontext = 1\n\tinterHunkContext = 5\n\trenames = copies\n\tindentHeuristic = false\n\tcolor = always\n"
		if err := os.WriteFile(cfg, []byte(hostile), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CONFIG_GLOBAL", cfg)
		for _, p := range pairs {
			got, err := g.Diff("c1", "c2", p.name)
			if err != nil || !bytes.Equal(got, plain[p.name]) {
				t.Errorf("%s: Diff under a user configuration = %q, %v; want the same as without it, %q", p.name, got, err, plain[p.name])
			}
		}
	})

	t.Run("tags", func(t *testing.T) {
		got, err := g.Tags()
		want := []string{"a0", "v1", "v2"}
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Tags() = %q, %v; want %q", got, err, want)
		}
		empty, err := Git{Repo: newRepo(t)}.Tags()
		if err != nil || len(empty) != 0 {
			t.Errorf("Tags() in a repository with no tags = %q, %v; want none", empty, err)
		}
	})

	t.Run("refuses_names_that_are_not_plain", func(t *testing.T) {
		marker := filepath.Join(dir, "pwned")
		bad := []string{"x;rm", "x y", "$(touch " + marker + ")", "a`b", "--output=" + marker, "-v1", "v1\nv2", ""}
		for _, name := range bad {
			calls := map[string]func() error{
				"Show tag":  func() error { _, err := g.Show(name, "a.txt"); return err },
				"Show path": func() error { _, err := g.Show("v1", name); return err },
				"Files":     func() error { _, err := g.Files(name); return err },
				"Rev":       func() error { _, err := g.Rev(name); return err },
				"Diff from": func() error { _, err := g.Diff(name, "v2", "a.txt"); return err },
				"Diff to":   func() error { _, err := g.Diff("v1", name, "a.txt"); return err },
				"Diff path": func() error { _, err := g.Diff("v1", "v2", name); return err },
			}
			// The map is only walked to run every call; the order does not matter.
			for call, f := range calls {
				// An error from our own check says "invalid"; one from git would not.
				if err := f(); err == nil || !strings.Contains(err.Error(), "invalid") {
					t.Errorf("%s with %q: error = %v; want one from the name check (it must say \"invalid\")", call, name, err)
				}
			}
		}
		if _, err := os.Stat(marker); err == nil {
			t.Errorf("%s exists: a name was run as a command", marker)
		}
		// The usual characters of tags and paths are accepted.
		for _, ok := range []string{"ch01-s1", "internal/syntax/lexer_test.go", "v1.2_3"} {
			if err := checkName("name", ok); err != nil {
				t.Errorf("checkName(%q) = %v; want nil", ok, err)
			}
		}
	})
}
