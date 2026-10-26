// bookx: not-in-book

package main

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// render resolves the directives of the prose of chapter nn of the fixture and
// returns the Markdown and the problems it found.
func (f *fixture) render(nn string) (string, []string) {
	f.t.Helper()
	b, _, err := loadBook(f.tree, f)
	if err != nil {
		f.t.Fatalf("loadBook: %v", err)
	}
	c := b.chapters[nn]
	if c == nil || c.Prose == "" {
		f.t.Fatalf("the fixture has no prose for chapter %s", nn)
	}
	md, ps := b.renderChapter(c)
	return md, errStrings(ps)
}

// checkRender fails unless the prose renders, with no problem, to exactly want.
func checkRender(t *testing.T, f *fixture, nn, want string) {
	t.Helper()
	got, problems := f.render(nn)
	if len(problems) != 0 {
		t.Errorf("render has problems: %q", problems)
	}
	if got != want {
		t.Errorf("render =\n%s\nwant\n%s", got, want)
	}
}

func TestRender(t *testing.T) {
	const ch2 = "book/ch02-lexer.md"

	t.Run("cards_table", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTree(ch2, "# Título\n\n{{cards}}\n\nFin.\n")
		f.setTree("book/tests/ch02.tests", string(lines(
			"[T02.01] lexer.first                                       level: core",
			"kind:    unit (go test)      file: internal/syntax/lexer_test.go",
			"case:    usa `a | b` en una tabla",
			"expect:  uno",
			"why:     porque sí",
			"ref:     TestLexer/pipe_continues_line",
			"",
			"[T02.02] lexer.second                                      level: stretch",
			"kind:    prop                file: internal/syntax/lexer_test.go",
			"case:    dos",
			"expect:  esperado dos",
			"why:     por qué dos",
			"ref:     FuzzLex",
		)))
		// The columns are Id, Caso, Por qué, Esperado, Nivel: the reason comes
		// before the expectation, unlike the order of the fields in a card.
		checkRender(t, f, "02", strings.Join([]string{
			"# Título",
			"",
			"| Id | Caso | Por qué | Esperado | Nivel |",
			"|---|---|---|---|---|",
			"| T02.01 | usa `a \\| b` en una tabla | porque sí | uno | core |",
			"| T02.02 | dos | por qué dos | esperado dos | stretch |",
			"",
			"Fin.",
			"",
		}, "\n"))

		// A chapter with no cards still gets the header of the table.
		f.setTree("book/tests/ch02.tests", "")
		checkRender(t, f, "02", "# Título\n\n| Id | Caso | Por qué | Esperado | Nivel |\n|---|---|---|---|---|\n\nFin.\n")
	})

	t.Run("code_fence_attrs", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTree(ch2, strings.Join([]string{
			"{{file ch02-s1 internal/base/source.go}}",
			"",
			"{{listing ch01-s1 driver.main}}",
			"",
			"{{reftest T02.01}}",
			"",
			"  {{file ch02-s1 go.mod}}  ", // a directive may be indented
			"",
		}, "\n"))
		checkRender(t, f, "02", strings.Join([]string{
			"```go title=\"internal/base/source.go\" tag=\"ch02-s1\" mode=\"file\"",
			"package base",
			"",
			"// Source is a file that was read.",
			"type Source struct{ Name string }",
			"```",
			"",
			"```go title=\"internal/driver/cli.go\" tag=\"ch01-s1\" mode=\"listing\"",
			"// Main runs the command line and returns the exit code.",
			"func Main() int {",
			"\treturn 0",
			"}",
			"```",
			"",
			// The test starts at the `t` of t.Run, with the indentation of the file below it.
			"```go title=\"internal/syntax/lexer_test.go\" tag=\"ch02\" mode=\"test\" hint=\"T02.01 lexer.pipe_continues_line\"",
			"t.Run(\"pipe_continues_line\", func(t *testing.T) {",
			"\t\tif continues(\"|> x\") {",
			"\t\t\tt.Fatal(\"wrong\")",
			"\t\t}",
			"\t})",
			"```",
			"",
			"```text title=\"go.mod\" tag=\"ch02-s1\" mode=\"file\"",
			"module github.com/aldezex/kilang",
			"",
			"go 1.27",
			"",
			"toolchain go1.27.3",
			"```",
			"",
		}, "\n"))

		// A hint region adds the hint attribute after the others.
		f.setTree(ch2, "{{listing ch02-s1 lexer.line_rule}}\n")
		checkRender(t, f, "02", strings.Join([]string{
			"```go title=\"internal/syntax/lexer.go\" tag=\"ch02-s1\" mode=\"listing\" hint=\"a line that starts with |> continues the previous one\"",
			"func continues(line string) bool {",
			"\t// ...",
			"\treturn false",
			"}",
			"```",
			"",
		}, "\n"))
	})

	t.Run("diff_fence", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTag("ch01-s1", "t.txt", "a\n")
		f.setTag("ch01-s2", "t.txt", "b\n")
		f.setTree(ch2, "{{diff ch01-s1..ch01-s2 t.txt}}\n")
		// The fixture's Diff lists the old lines with `-` and the new ones with `+`.
		checkRender(t, f, "02", strings.Join([]string{
			"```diff title=\"t.txt\" tag=\"ch01-s1..ch01-s2\" mode=\"diff\"",
			"--- ch01-s1:t.txt",
			"+++ ch01-s2:t.txt",
			"-a",
			"+b",
			"```",
			"",
		}, "\n"))
	})

	t.Run("empty_diff_is_a_problem", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTag("ch01-s1", "t.txt", "a\n")
		f.setTag("ch01-s2", "t.txt", "a\n")
		f.setTree(ch2, "{{diff ch01-s1..ch01-s2 t.txt}}\n")
		_, problems := f.render("02")
		want := []string{ch2 + ":1: `t.txt` is the same in tag `ch01-s1` and in tag `ch01-s2`: the diff would be empty"}
		if !slices.Equal(problems, want) {
			t.Errorf("problems = %q, want %q", problems, want)
		}
		// A file that exists in only one of the tags is a real diff.
		f.setTag("ch01-s2", "u.txt", "b\n")
		f.setTree(ch2, "{{diff ch01-s1..ch01-s2 u.txt}}\n")
		if _, problems := f.render("02"); len(problems) != 0 {
			t.Errorf("a file added between the tags: problems = %q, want none", problems)
		}
	})

	t.Run("attr_escapes", func(t *testing.T) {
		f := cleanFixture(t)
		hint := "say \"hi\" to `x`"
		// The marker is built from pieces: a line of this file that began with
		// it would be read as a marker of the book.
		f.setTag("ch02-s1", "x.go", string(lines("package x", "// "+"@book a.b hint "+strconv.Quote(hint), "code", "// "+"@end")))
		f.setTree(ch2, "{{listing ch02-s1 a.b}}\n")
		checkRender(t, f, "02", strings.Join([]string{
			"```go title=\"x.go\" tag=\"ch02-s1\" mode=\"listing\" hint=\"say &quot;hi&quot; to &#96;x&#96;\"",
			"code",
			"```",
			"",
		}, "\n"))

		// Only the quote and the backtick are escaped.
		if got, want := fenceInfo("go", [2]string{"hint", "a & b < c ' d"}), `go hint="a & b < c ' d"`; got != want {
			t.Errorf("fenceInfo = %q, want %q", got, want)
		}
	})

	t.Run("invalid_utf8_replaced", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTag("ch02-s1", "bad.txt", "ab\xffcd\nx\xff\xfey\n")
		f.setTree(ch2, "{{file ch02-s1 bad.txt}}\n")
		// One bad byte is one U+FFFD, and a run of bad bytes is one too.
		checkRender(t, f, "02", strings.Join([]string{
			"```text title=\"bad.txt\" tag=\"ch02-s1\" mode=\"file\"",
			"ab\uFFFDcd",
			"x\uFFFDy",
			"```",
			"",
		}, "\n"))
		if got, _ := f.render("02"); strings.Contains(got, "\xff") {
			t.Error("the output still has an invalid byte")
		}
	})

	t.Run("fence_longer_than_content_and_languages", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTag("ch02-s1", "docs/note.md", "Mira:\n```go\nx := 1\n```\n  `````\nfin\n")
		f.setTree(ch2, "{{file ch02-s1 docs/note.md}}\n")
		checkRender(t, f, "02", strings.Join([]string{
			"``````markdown title=\"docs/note.md\" tag=\"ch02-s1\" mode=\"file\"",
			"Mira:",
			"```go",
			"x := 1",
			"```",
			"  `````",
			"fin",
			"``````",
			"",
		}, "\n"))

		for _, c := range []struct{ path, lang string }{
			{"a.go", "go"}, {"a.ki", "ki"}, {"a.sh", "sh"}, {"a.yml", "yaml"}, {"a.s", "goasm"},
			{"a.md", "markdown"}, {"go.mod", "text"}, {"a.yaml", "text"}, {"Makefile", "text"}, {"a.txt", "text"},
		} {
			if got := fenceLang(c.path); got != c.lang {
				t.Errorf("fenceLang(%q) = %q, want %q", c.path, got, c.lang)
			}
		}
	})

	t.Run("problems_do_not_hide_the_rest", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTree(ch2, "{{file ch02-s1 nope.go}}\n\n{{cards}}\n")
		got, problems := f.render("02")
		if want := []string{ch2 + ":1: `nope.go` is not in tag `ch02-s1`"}; !slices.Equal(problems, want) {
			t.Errorf("problems = %q, want %q", problems, want)
		}
		if !strings.Contains(got, "| Id | Caso |") {
			t.Errorf("the other directive should still render, got\n%s", got)
		}
	})
}
