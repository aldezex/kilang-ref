package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

// repoReader is what verify, render and regions need from a repository: the
// files of a tag and nothing else. Git implements it with the git command; the
// tests of verify and render implement it in memory, so they run without a
// repository.
type repoReader interface {
	// Show returns the content of path in tag.
	Show(tag, path string) ([]byte, error)
	// Files returns every file path of tag, in the order git lists them.
	Files(tag string) ([]string, error)
	// Rev returns the full SHA of the commit that tag points to.
	Rev(tag string) (string, error)
	// Diff returns the changes of path from the tag from to the tag to.
	Diff(from, to, path string) ([]byte, error)
	// Tags returns the names of all the tags, sorted.
	Tags() ([]string, error)
}

// Git reads a repository with the git command. Repo is the directory of the
// repository: every command runs as `git -C Repo ...`. Arguments are passed to
// git one by one, never through a shell, and a tag or a path that has anything
// other than letters, digits and `. _ - /` is refused before git runs, so no
// text can turn into an option or a second command.
type Git struct{ Repo string }

var _ repoReader = Git{}

// checkName returns an error if s is not safe to hand to git as a tag or a
// path. A leading `-` is refused as well, because git would read it as an
// option.
func checkName(what, s string) error {
	if !pathTokenRe.MatchString(s) || strings.HasPrefix(s, "-") {
		return fmt.Errorf("invalid %s %q: only letters, digits, `.`, `_`, `-` and `/` are allowed, and it cannot start with `-`", what, s)
	}
	return nil
}

// run runs `git -C Repo args...` and returns what it wrote to stdout. When git
// fails, the error carries its message from stderr. It turns off the pager and
// the colour, and keeps quotepath off so that names with accents come back as
// they are.
func (g Git) run(args ...string) ([]byte, error) {
	full := append([]string{"-C", g.Repo, "--no-pager", "-c", "color.ui=never", "-c", "core.quotepath=off"}, args...)
	cmd := exec.Command("git", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.Bytes(), nil
}

// Show returns the content of path in tag: `git show <tag>:<path>`, as the
// spec says. --no-textconv keeps a diff driver of the user's configuration or
// of .gitattributes from rewriting the file on its way out.
func (g Git) Show(tag, path string) ([]byte, error) {
	if err := errors.Join(checkName("tag", tag), checkName("path", path)); err != nil {
		return nil, err
	}
	return g.run("show", "--no-textconv", tag+":"+path)
}

// Files returns the path of every file in tag, one per entry:
// `git ls-tree -r --name-only <tag>`. It asks git for NUL-separated output
// (-z) so that a name with a newline or a non-ASCII character comes back whole.
func (g Git) Files(tag string) ([]string, error) {
	if err := checkName("tag", tag); err != nil {
		return nil, err
	}
	out, err := g.run("ls-tree", "-r", "--name-only", "-z", tag)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// Rev returns the SHA of the commit that tag points to: `git rev-parse
// <tag>^{commit}`. For an annotated tag that is the commit and not the tag
// object, which is the value ci/check-tags.sh writes in verified-tags.txt.
func (g Git) Rev(tag string) (string, error) {
	if err := checkName("tag", tag); err != nil {
		return "", err
	}
	out, err := g.run("rev-parse", tag+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Diff returns the changes of path between two tags: `git diff <from> <to> --
// <path>`. The output is empty when the file is the same in both. The options
// pin everything that the user's configuration (diff.algorithm, diff.noprefix,
// diff.mnemonicPrefix, diff.context, diff.renames and the like) could change,
// so the same book renders the same diff on every machine.
func (g Git) Diff(from, to, path string) ([]byte, error) {
	if err := errors.Join(checkName("tag", from), checkName("tag", to), checkName("path", path)); err != nil {
		return nil, err
	}
	return g.run(slices.Concat(diffOptions, []string{from, to, "--", path})...)
}

// diffOptions is `git diff` with every option that has a configuration
// variable set to the value the book relies on.
var diffOptions = []string{
	"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames",
	"--diff-algorithm=myers", "--indent-heuristic", "--inter-hunk-context=0",
	"--src-prefix=a/", "--dst-prefix=b/", "-U3",
}

// Tags returns the names of all the tags of the repository, sorted by name:
// `git tag --list`.
func (g Git) Tags() ([]string, error) {
	out, err := g.run("tag", "--list")
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, name := range strings.Split(string(out), "\n") {
		if name = strings.TrimSpace(name); name != "" {
			tags = append(tags, name)
		}
	}
	slices.Sort(tags)
	return tags, nil
}

// VerifiedTag is a line of book/verified-tags.txt: a tag whose commit passed
// ci/ci-local.sh, the day it was checked and the Go version that checked it.
type VerifiedTag struct {
	Tag, SHA, Date, GoVersion string
}

// lineError is a problem on one line of a file that has no ScanError of its
// own, because the parser does not know the name of the file.
type lineError struct {
	Line    int
	Message string
}

func (e *lineError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Message) }

// goVersionRe is the shape of what `go version` prints as its third word.
var goVersionRe = regexp.MustCompile(`^go[0-9]+\.[0-9]+[0-9A-Za-z.]*$`)

// sha40Re is a full SHA-1 in lowercase hexadecimal.
var sha40Re = regexp.MustCompile(`^[0-9a-f]{40}$`)

// parseVerifiedTags reads book/verified-tags.txt: one line per tag,
// `<tag> <sha40> <YYYY-MM-DD> <goversion>`, separated by single spaces. Blank
// lines are skipped. The first malformed line, or a tag that appears twice,
// is an error (a *lineError); the lines come back in the order of the file.
func parseVerifiedTags(text []byte) ([]VerifiedTag, error) {
	var tags []VerifiedTag
	seen := map[string]bool{}
	for i, line := range splitLines(text) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		bad := func(format string, args ...any) error {
			return &lineError{Line: i + 1, Message: fmt.Sprintf(format, args...)}
		}
		f := strings.Split(line, " ")
		if len(f) != 4 {
			return nil, bad("want `<tag> <sha40> <YYYY-MM-DD> <goversion>`, found %d fields", len(f))
		}
		v := VerifiedTag{Tag: f[0], SHA: f[1], Date: f[2], GoVersion: f[3]}
		if err := checkName("tag", v.Tag); err != nil {
			return nil, bad("%v", err)
		}
		if !sha40Re.MatchString(v.SHA) {
			return nil, bad("`%s` is not a 40-character lowercase hexadecimal SHA", v.SHA)
		}
		if _, err := time.Parse("2006-01-02", v.Date); err != nil {
			return nil, bad("`%s` is not a date written YYYY-MM-DD", v.Date)
		}
		if !goVersionRe.MatchString(v.GoVersion) {
			return nil, bad("`%s` is not a Go version such as go1.27.3", v.GoVersion)
		}
		if seen[v.Tag] {
			return nil, bad("tag `%s` appears twice", v.Tag)
		}
		seen[v.Tag] = true
		tags = append(tags, v)
	}
	return tags, nil
}
