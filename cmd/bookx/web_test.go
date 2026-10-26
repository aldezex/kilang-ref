// bookx: not-in-book

package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// webFixture is the base book of the verify tests plus the copy of the index
// of the spec, which `web` reads from the working tree.
func webFixture(t *testing.T) *fixture {
	t.Helper()
	f := cleanFixture(t)
	spec, err := os.ReadFile(filepath.Join("testdata", "spec-index.md"))
	if err != nil {
		t.Fatal(err)
	}
	f.setTree(specPath, string(spec))
	return f
}

// outputTree reads every file under dir into a map from its slash-separated
// relative path to its content.
func outputTree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("reading the output tree: %v", err)
	}
	return files
}

// treeNames returns the sorted paths of an output tree.
func treeNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	slices.Sort(names)
	return names
}

// buildOK runs buildWeb on the fixture and fails the test if it reports a
// problem or an error. It returns the output directory.
func (f *fixture) buildOK(all bool) string {
	f.t.Helper()
	out := filepath.Join(f.t.TempDir(), "out")
	problems, err := buildWeb(f.tree, f, out, all)
	if err != nil || len(problems) != 0 {
		f.t.Fatalf("buildWeb: problems %q, error %v; want neither", errStrings(problems), err)
	}
	return out
}

// manifestOf reads book/manifest.json of an output tree.
func manifestOf(t *testing.T, files map[string][]byte) Manifest {
	t.Helper()
	var m Manifest
	if err := json.Unmarshal(files["book/manifest.json"], &m); err != nil {
		t.Fatalf("book/manifest.json: %v", err)
	}
	return m
}

func TestWeb(t *testing.T) {
	t.Run("writes_page_and_json", func(t *testing.T) {
		f := webFixture(t)
		f.copyTag("ch01-s2", "ch01-s3")
		files := outputTree(t, f.buildOK(false))

		wantNames := []string{"book/ch01.md", "book/ch02.md", "book/manifest.json", "book/outline.json", "index.html"}
		if got := treeNames(files); !slices.Equal(got, wantNames) {
			t.Fatalf("the output has %q, want exactly %q", got, wantNames)
		}
		if !bytes.Equal(files["index.html"], indexHTML) || len(indexHTML) == 0 {
			t.Error("index.html is not the page embedded in bookx")
		}

		m := manifestOf(t, files)
		want := []ManifestChapter{
			{Chapter: 1, Title: "El taller", File: "ch01.md", Hours: 8, Part: "I", Milestone: "ki --version",
				Tags: []string{"ch01-s1", "ch01-s2", "ch01-s3", "ch01"}},
			{Chapter: 2, Title: "El lexer", File: "ch02.md", Hours: 8, Part: "I", Milestone: "ki emit tokens",
				Tags: []string{"ch02-s1", "ch02"}},
		}
		if len(m.Chapters) != len(want) {
			t.Fatalf("the manifest has %d chapters, want %d", len(m.Chapters), len(want))
		}
		for i, w := range want {
			g := m.Chapters[i]
			if g.Chapter != w.Chapter || g.Title != w.Title || g.File != w.File || g.Hours != w.Hours ||
				g.Part != w.Part || g.Milestone != w.Milestone || !slices.Equal(g.Tags, w.Tags) {
				t.Errorf("manifest chapter %d = %+v, want %+v", i, g, w)
			}
		}
		// The keys are the ones the page reads.
		for _, key := range []string{`"chapter":1`, `"title":"El taller"`, `"file":"ch01.md"`, `"hours":8`, `"part":"I"`, `"milestone":"ki --version"`, `"tags":["ch01-s1","ch01-s2","ch01-s3","ch01"]`} {
			if !bytes.Contains(files["book/manifest.json"], []byte(key)) {
				t.Errorf("manifest.json does not contain %s", key)
			}
		}

		var o Outline
		if err := json.Unmarshal(files["book/outline.json"], &o); err != nil {
			t.Fatalf("book/outline.json: %v", err)
		}
		wantOutline := specIndex(t)
		if len(o.Parts) != 11 || o.Parts[1].Milestones != "M2, M3" || o.Parts[0].Chapters[0].Title != wantOutline.Parts[0].Chapters[0].Title {
			t.Errorf("outline.json does not hold the index of the spec: %d parts", len(o.Parts))
		}
		// The prose of chapter 1 has one :::exercise and the one of chapter 2 none.
		if !bytes.Contains(files["book/outline.json"], []byte(`"exercises":{"1":1,"2":0}`)) {
			t.Errorf("outline.json does not end with the exercises {\"1\":1,\"2\":0}: %s", files["book/outline.json"])
		}

		// The Markdown is the rendered chapter, front matter included.
		ch1 := string(files["book/ch01.md"])
		if !strings.HasPrefix(ch1, "---\nchapter: 1\ntitle: El taller\n") || strings.Contains(ch1, "{{") ||
			!strings.Contains(ch1, "```text title=\"go.mod\" tag=\"ch01-s1\" mode=\"file\"\n") {
			t.Errorf("ch01.md is not the rendered chapter:\n%s", ch1)
		}
	})

	t.Run("tags_in_session_order", func(t *testing.T) {
		f := webFixture(t)
		for _, tag := range []string{"ch02-s2", "ch02-s10"} {
			f.copyTag("ch02-s1", tag)
		}
		m := manifestOf(t, outputTree(t, f.buildOK(false)))
		// s10 comes after s2, and the chapter tag after all the sessions.
		if want := []string{"ch02-s1", "ch02-s2", "ch02-s10", "ch02"}; !slices.Equal(m.Chapters[1].Tags, want) {
			t.Errorf("tags of chapter 2 = %q, want %q", m.Chapters[1].Tags, want)
		}
	})

	t.Run("chapter_without_chapter_tag", func(t *testing.T) {
		// With --all a chapter that has prose and sessions but no chNN tag yet is
		// published, and its tags are the sessions only: ch02 does not exist.
		f := webFixture(t)
		delete(f.tags, "ch02")
		var kept []string
		for _, line := range strings.Split(f.text(verifiedTagsPath), "\n") {
			if !strings.HasPrefix(line, "ch02 ") {
				kept = append(kept, line)
			}
		}
		f.setTree(verifiedTagsPath, strings.Join(kept, "\n"))
		m := manifestOf(t, outputTree(t, f.buildOK(true)))
		if len(m.Chapters) != 2 {
			t.Fatalf("the manifest has %d chapters, want 2", len(m.Chapters))
		}
		if want := []string{"ch02-s1"}; !slices.Equal(m.Chapters[1].Tags, want) {
			t.Errorf("tags of chapter 2 = %q, want %q", m.Chapters[1].Tags, want)
		}
		if want := []string{"ch01-s1", "ch01-s2", "ch01"}; !slices.Equal(m.Chapters[0].Tags, want) {
			t.Errorf("tags of chapter 1 = %q, want %q", m.Chapters[0].Tags, want)
		}
	})

	t.Run("fffd_marker", func(t *testing.T) {
		f := webFixture(t)
		// U+FFFD in three ways: a typed one in the prose, one in the title (which
		// goes to the manifest too) and a run of bytes that are not UTF-8 in a
		// quoted file, which render turns into one U+FFFD.
		f.edit("book/ch01-taller.md", "Dejas el taller listo.", "Dejas el taller listo "+replacementChar+".")
		f.edit("book/ch01-taller.md", "title: El taller\n", "title: El taller "+replacementChar+"\n")
		f.setTag("ch01-s1", "go.mod", "module github.com/aldezex/kilang\n\xff\xfe\n")
		files := outputTree(t, f.buildOK(false))

		for p, data := range files {
			if bytes.Contains(data, []byte("\xef\xbf\xbd")) {
				t.Errorf("%s still has a U+FFFD", p)
			}
		}
		if !bytes.Contains(files["index.html"], []byte("%%U+FFFD%%")) {
			t.Errorf("index.html does not know the marker %s", "%%U+FFFD%%")
		}
		ch1 := string(files["book/ch01.md"])
		for _, want := range []string{"Dejas el taller listo %%U+FFFD%%.\n", "title: El taller %%U+FFFD%%\n", "module github.com/aldezex/kilang\n%%U+FFFD%%\n```"} {
			if !strings.Contains(ch1, want) {
				t.Errorf("ch01.md does not contain %q", want)
			}
		}
		if n := strings.Count(ch1, "%%U+FFFD%%"); n != 3 {
			t.Errorf("ch01.md has %d markers, want 3", n)
		}
		// The JSON keeps the character as an escape, so it reads back unchanged.
		if got := manifestOf(t, files).Chapters[0].Title; got != "El taller "+replacementChar {
			t.Errorf("manifest title = %q, want %q", got, "El taller "+replacementChar)
		}
	})

	t.Run("only_verified_chapters", func(t *testing.T) {
		f := webFixture(t)
		var kept []string
		for _, line := range strings.Split(f.text(verifiedTagsPath), "\n") {
			if !strings.HasPrefix(line, "ch02 ") {
				kept = append(kept, line)
			}
		}
		f.setTree(verifiedTagsPath, strings.Join(kept, "\n"))

		files := outputTree(t, f.buildOK(false))
		if got, want := treeNames(files), []string{"book/ch01.md", "book/manifest.json", "book/outline.json", "index.html"}; !slices.Equal(got, want) {
			t.Errorf("without --all the output has %q, want %q", got, want)
		}
		if m := manifestOf(t, files); len(m.Chapters) != 1 || m.Chapters[0].Chapter != 1 {
			t.Errorf("without --all the manifest has %+v, want only chapter 1", m.Chapters)
		}
		if !bytes.Contains(files["book/outline.json"], []byte(`"exercises":{"1":1}`)) {
			t.Errorf("without --all the exercises are not only those of chapter 1: %s", files["book/outline.json"])
		}

		files = outputTree(t, f.buildOK(true))
		if got, want := treeNames(files), []string{"book/ch01.md", "book/ch02.md", "book/manifest.json", "book/outline.json", "index.html"}; !slices.Equal(got, want) {
			t.Errorf("with --all the output has %q, want %q", got, want)
		}
		if m := manifestOf(t, files); len(m.Chapters) != 2 {
			t.Errorf("with --all the manifest has %d chapters, want 2", len(m.Chapters))
		}

		// With no verified-tags.txt at all nothing is publishable, but the page and the index are.
		delete(f.tree, verifiedTagsPath)
		out := filepath.Join(t.TempDir(), "out")
		if problems, err := buildWeb(f.tree, f, out, false); err != nil || len(problems) != 0 {
			t.Fatalf("buildWeb without verified-tags.txt: problems %q, error %v", errStrings(problems), err)
		}
		if got, want := treeNames(outputTree(t, out)), []string{"book/manifest.json", "book/outline.json", "index.html"}; !slices.Equal(got, want) {
			t.Errorf("with no verified tags the output has %q, want %q", got, want)
		}
	})

	t.Run("verify_problems_stop_the_build", func(t *testing.T) {
		f := webFixture(t)
		f.edit("book/ch01-taller.md", "{{file ch01-s1 go.mod}}", "{{file ch01-s1 nope.go}}")
		line := f.lineOf("book/ch01-taller.md", "nope.go")
		out := filepath.Join(t.TempDir(), "out")
		problems, err := buildWeb(f.tree, f, out, false)
		wantProblems := []string{want("book/ch01-taller.md:%d: `nope.go` is not in tag `ch01-s1`", line)}
		if err != nil || !slices.Equal(errStrings(problems), wantProblems) {
			t.Errorf("problems %q, error %v; want %q", errStrings(problems), err, wantProblems)
		}
		if _, err := os.Stat(out); err == nil {
			t.Error("the output directory exists although the build had problems")
		}

		// A problem in a chapter that is not published does not stop the others...
		f.setTree(verifiedTagsPath, strings.Replace(f.text(verifiedTagsPath), "ch01 ", "ch01x ", 1))
		f.edit("book/ch02-lexer.md", "{{cards}}", "{{cards}}\n\n{{file ch02-s1 nope.go}}")
		problems, err = buildWeb(f.tree, f, out, false)
		if err != nil || len(problems) != 1 || !strings.HasPrefix(problems[0].Path, "book/ch02-") {
			t.Errorf("with ch01 broken but unverified and ch02 broken: problems %q, error %v; want only the one of ch02", errStrings(problems), err)
		}
		// ...unless --all asks for it.
		problems, err = buildWeb(f.tree, f, out, true)
		if err != nil || len(problems) != 2 {
			t.Errorf("with --all: problems %q, error %v; want the two broken directives", errStrings(problems), err)
		}
	})

	t.Run("problems_without_a_chapter_always_count", func(t *testing.T) {
		// A problem found in a tag whose name is not a chapter's (here the tag
		// "extra", cited by the prose of chapter 2) belongs to no chapter, so it
		// stops the build even though chapter 2 is not published.
		f := webFixture(t)
		f.setTree(verifiedTagsPath, strings.Replace(f.text(verifiedTagsPath), "ch02 ", "ch02x ", 1))
		f.setTag("extra", "a.go", string(lines("package a", "// "+"@book a.open full", "x")))
		f.edit("book/ch02-lexer.md", "{{cards}}", "{{cards}}\n\n{{file extra a.go}}")
		out := filepath.Join(t.TempDir(), "out")
		problems, err := buildWeb(f.tree, f, out, false)
		want := "extra:a.go:2: region `a.open` is never closed: it needs an @end line"
		if err != nil || !slices.Contains(errStrings(problems), want) {
			t.Errorf("problems %q, error %v; want %q among them", errStrings(problems), err, want)
		}
		if _, err := os.Stat(out); err == nil {
			t.Error("the output directory exists although the build had problems")
		}
	})

	t.Run("rejects_bad_front_matter", func(t *testing.T) {
		const prose = "book/ch01-taller.md"
		rows := []struct {
			name string
			edit func(f *fixture)
			want string
		}{
			{"no front matter", func(f *fixture) { f.setTree(prose, "## Objetivo\n\n{{cards}}\n") },
				prose + ":1: the chapter has no front matter: the file has to start with a --- line"},
			{"never closed", func(f *fixture) { f.setTree(prose, "---\nchapter: 1\n\n{{cards}}\n") },
				prose + ":1: the front matter is never closed with a --- line"},
			{"missing key", func(f *fixture) { f.edit(prose, "part: I\n", "") },
				prose + ":1: the front matter has no `part`"},
			{"hours are not a number", func(f *fixture) { f.edit(prose, "hours: 8\n", "hours: ocho\n") },
				prose + ":5: the front matter says hours: \"ocho\", which is not a number"},
			{"another chapter", func(f *fixture) { f.edit(prose, "chapter: 1\n", "chapter: 2\n") },
				prose + ":2: the front matter says chapter 2, but the file is the prose of chapter 1"},
		}
		for _, r := range rows {
			f := webFixture(t)
			r.edit(f)
			out := filepath.Join(t.TempDir(), "out")
			problems, err := buildWeb(f.tree, f, out, false)
			if err != nil || !slices.Contains(errStrings(problems), r.want) {
				t.Errorf("%s: problems %q, error %v; want %q among them", r.name, errStrings(problems), err, r.want)
			}
			if _, err := os.Stat(out); err == nil {
				t.Errorf("%s: the output directory exists", r.name)
			}
		}
	})

	t.Run("command_line", func(t *testing.T) {
		const usage = "usage: bookx web <out-dir> [--repo DIR] [--all]\n"
		rows := []struct {
			name   string
			args   []string
			stderr string
		}{
			{"without a directory", []string{"web"}, "bookx web: want 1 argument(s), found 0\n" + usage},
			{"with two directories", []string{"web", "a", "b"}, "bookx web: want 1 argument(s), found 2\n" + usage},
			{"with an unknown flag", []string{"web", "a", "--bogus"}, "bookx web: flag provided but not defined: -bogus\n" + usage},
		}
		for _, r := range rows {
			code, stdout, stderr := runBookx(t, r.args...)
			if code != 2 || stdout != "" || stderr != r.stderr {
				t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit 2, no stdout, stderr %q", r.name, code, stdout, stderr, r.stderr)
			}
		}

		// On a real repository, with the flags on either side of the directory.
		f := webFixture(t)
		dir := repoFromFixture(t, f)
		out := filepath.Join(t.TempDir(), "out")
		for _, args := range [][]string{{"web", out, "--repo", dir}, {"web", "--repo", dir, out}} {
			if err := os.RemoveAll(out); err != nil {
				t.Fatal(err)
			}
			code, stdout, stderr := runBookx(t, args...)
			if code != 0 || stdout != "" || stderr != "" {
				t.Fatalf("%v: exit %d, stdout %q, stderr %q; want a quiet exit 0", args, code, stdout, stderr)
			}
			if got, want := treeNames(outputTree(t, out)), []string{"book/ch01.md", "book/ch02.md", "book/manifest.json", "book/outline.json", "index.html"}; !slices.Equal(got, want) {
				t.Fatalf("%v wrote %q, want %q", args, got, want)
			}
		}

		// A chapter whose directives do not resolve makes the command exit with 1 and write nothing.
		writeFile(t, dir, "book/ch01-taller.md", "---\nchapter: 1\ntitle: El taller\npart: I\nhours: 8\nmilestone: m\n---\n\n{{file ch01-s1 nope.go}}\n")
		bad := filepath.Join(t.TempDir(), "bad")
		code, stdout, stderr := runBookx(t, "web", bad, "--repo", dir)
		if w := "book/ch01-taller.md:9: `nope.go` is not in tag `ch01-s1`\n"; code != 1 || !strings.Contains(stdout, w) || stderr != "" {
			t.Errorf("web with a bad directive: exit %d, stdout %q, stderr %q; want exit 1 and %q among the output", code, stdout, stderr, w)
		}
		if _, err := os.Stat(bad); err == nil {
			t.Error("web created the output directory although a directive failed")
		}

		// A repository without the spec is an error of the tool, not a problem of the book.
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(specPath))); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr = runBookx(t, "web", bad, "--repo", dir)
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "bookx web: cannot read the index of the book: ") {
			t.Errorf("web without the spec: exit %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})
}
