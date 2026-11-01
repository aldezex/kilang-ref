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
		// {{cards}} reads the file in the tag of the chapter, not in the tree.
		f.setTag("ch02", "book/tests/ch02.tests", string(lines(
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
		f.setTag("ch02", "book/tests/ch02.tests", "")
		checkRender(t, f, "02", "# Título\n\n| Id | Caso | Por qué | Esperado | Nivel |\n|---|---|---|---|---|\n\nFin.\n")
	})

	// The cards of a chapter are those of its own moment: the file
	// book/tests/chNN.tests in the tag chNN, or in the last session tag while
	// the chapter has no tag yet. The tests below reword or break the working
	// tree after the tag, as an errata of a later chapter does, and look at
	// what the reader is shown.

	// tagTable is the table that {{cards}} makes of the cards that the base
	// fixture has in its tag ch02.
	const tagTable = "| Id | Caso | Por qué | Esperado | Nivel |\n|---|---|---|---|---|\n" +
		"| T02.01 | una línea que empieza por `\\|>` continúa la anterior | si el lexer insertara un Newline, el error saldría una línea más abajo | ningún Newline entre `xs` y `\\|>` | core |\n" +
		"| T02.02 | el lexer nunca entra en pánico | el lexer lee entrada ajena | ninguna entrada hace caer el lexer | core |\n" +
		"| T02.03 | internar dos veces el mismo texto | el resto del compilador compara números, no textos | la misma clave | core |\n"

	// oneCard is a file with the card T02.01, whose expect is the text given.
	oneCard := func(expect string) string {
		return string(lines(
			"[T02.01] lexer.first                                       level: core",
			"kind:    unit (go test)      file: internal/syntax/lexer_test.go",
			"case:    caso",
			"expect:  "+expect,
			"why:     motivo",
			"ref:     TestLexer/pipe_continues_line",
		))
	}

	t.Run("cards_are_those_of_the_chapter_tag", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTree(ch2, "{{cards}}\n")
		// A card reworded after the tag, a card added after it and a card
		// dropped after it: none of them reaches the table.
		f.edit("book/tests/ch02.tests", "expect:  ningún Newline entre `xs` y `|>`", "expect:  ningún Newline, y una etiqueta que ch02 no da")
		f.edit("book/tests/ch02.tests", "[T02.03] interner.example                                  level: core", "[T02.03] interner.example                                  level: stretch")
		f.setTree("book/tests/ch02.tests", f.text("book/tests/ch02.tests")+"\n"+string(lines(
			"[T02.04] lexer.later                                       level: core",
			"kind:    prop                file: internal/syntax/lexer_test.go",
			"case:    otro",
			"expect:  otro",
			"why:     otro",
			"ref:     FuzzLater",
		)))
		checkRender(t, f, "02", tagTable)

		// The same tree, read through a tag that has the reworded card, shows
		// it: the table follows the tag and not a copy made at some other time.
		f.setTag("ch02", "book/tests/ch02.tests", oneCard("lo que dice ch02"))
		got, problems := f.render("02")
		if len(problems) != 0 || !strings.Contains(got, "| T02.01 | caso | motivo | lo que dice ch02 | core |") || strings.Contains(got, "T02.02") {
			t.Errorf("render = %q with problems %q, want the single card of the tag", got, problems)
		}
	})

	t.Run("an_unfinished_chapter_reads_its_last_session_tag", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTree(ch2, "{{cards}}\n")
		// ch02 has no chapter tag. Its sessions are s1, s2 and s10, and only
		// the last one by number (s10; a sort of the names would say s2) has
		// the cards the reader is shown.
		delete(f.tags, "ch02")
		f.setTag("ch02-s1", "book/tests/ch02.tests", oneCard("de s1"))
		f.setTag("ch02-s2", "book/tests/ch02.tests", oneCard("de s2"))
		f.setTag("ch02-s10", "book/tests/ch02.tests", oneCard("de s10"))
		got, problems := f.render("02")
		want := "| Id | Caso | Por qué | Esperado | Nivel |\n|---|---|---|---|---|\n| T02.01 | caso | motivo | de s10 | core |\n"
		if got != want || len(problems) != 0 {
			t.Errorf("render = %q with problems %q, want %q", got, problems, want)
		}

		// The chapter tag, once it exists, takes over from the sessions.
		f.setTag("ch02", "book/tests/ch02.tests", oneCard("de ch02"))
		if got, _ := f.render("02"); !strings.Contains(got, "| de ch02 |") {
			t.Errorf("with the chapter tag, render = %q, want the card of ch02", got)
		}
	})

	t.Run("reftest_reads_the_card_of_the_tag", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTree(ch2, "{{reftest T02.01}}\n\n{{reftest T02.03}}\n")
		// After the tag the tree renames the test of T02.01 and drops T02.03:
		// the reader of ch02 is still shown both tests, found where the cards
		// of the tag say they are.
		f.edit("book/tests/ch02.tests", "ref:     TestLexer/pipe_continues_line", "ref:     TestLexer/renamed_later")
		cut := strings.Index(f.text("book/tests/ch02.tests"), "[T02.03]")
		f.setTree("book/tests/ch02.tests", f.text("book/tests/ch02.tests")[:cut])
		got, problems := f.render("02")
		if len(problems) != 0 {
			t.Errorf("problems = %q, want none", problems)
		}
		for _, hint := range []string{`hint="T02.01 lexer.pipe_continues_line"`, `hint="T02.03 interner.example"`} {
			if !strings.Contains(got, hint) {
				t.Errorf("render has no %s:\n%s", hint, got)
			}
		}

		// A card that the tree has and the tag does not is not there for the
		// reader, and the message says where it looked.
		f.setTree(ch2, "{{reftest T02.04}}\n")
		f.setTree("book/tests/ch02.tests", f.text("book/tests/ch02.tests")+string(lines(
			"[T02.04] lexer.later                                       level: core",
			"kind:    prop                file: internal/syntax/lexer_test.go",
			"case:    otro", "expect:  otro", "why:     otro", "ref:     FuzzLex",
		)))
		_, problems = f.render("02")
		if want := []string{ch2 + ":1: there is no card T02.04 in the cards of chapter 02 at tag `ch02`"}; !slices.Equal(problems, want) {
			t.Errorf("problems = %q, want %q", problems, want)
		}
	})

	t.Run("cards_missing_from_the_tag_are_a_problem", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTree(ch2, "{{cards}}\n\n{{reftest T02.01}}\n")
		delete(f.tags["ch02"], "book/tests/ch02.tests")
		_, problems := f.render("02")
		want := []string{
			ch2 + ":1: `book/tests/ch02.tests` is not in tag `ch02`",
			ch2 + ":3: `book/tests/ch02.tests` is not in tag `ch02`",
		}
		if !slices.Equal(problems, want) {
			t.Errorf("problems = %q, want %q", problems, want)
		}

		// A chapter that is still written is read at its last session tag, and
		// the message names that one.
		delete(f.tags, "ch02")
		_, problems = f.render("02")
		if len(problems) != 2 || !strings.HasSuffix(problems[0], ": `book/tests/ch02.tests` is not in tag `ch02-s1`") {
			t.Errorf("problems = %q, want the file missing from tag ch02-s1", problems)
		}
	})

	t.Run("cards_of_the_tag_that_do_not_parse_are_reported_once", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTree(ch2, "{{cards}}\n\n{{reftest T02.02}}\n\n{{reftest T02.03}}\n")
		f.editTag("ch02", "book/tests/ch02.tests", "ref:     TestLexer/pipe_continues_line\n", "ref:     TestLexer/pipe_continues_line\nextra:   no\n")
		got, problems := f.render("02")
		// The error is in the file of the tag, which the path says, and it is
		// one error, not one for each directive that reads the cards. The card
		// with the problem is not in the table and the others are.
		tagged, err := f.Show("ch02", "book/tests/ch02.tests")
		if err != nil {
			t.Fatal(err)
		}
		line := 1 + strings.Count(string(tagged[:strings.Index(string(tagged), "extra:")]), "\n")
		wantProblems := []string{want("ch02:book/tests/ch02.tests:%d: card T02.01: unknown field `extra:`", line)}
		if !slices.Equal(problems, wantProblems) {
			t.Errorf("problems = %q, want %q", problems, wantProblems)
		}
		if strings.Contains(got, "| T02.01 |") || !strings.Contains(got, "| T02.02 |") || !strings.Contains(got, `hint="T02.02 lexer.fuzz"`) {
			t.Errorf("render = %q, want T02.02 and not T02.01", got)
		}

		// Either directive alone reports it too.
		for _, directive := range []string{"{{cards}}", "{{reftest T02.02}}"} {
			f.setTree(ch2, directive+"\n")
			if _, problems := f.render("02"); !slices.Equal(problems, wantProblems) {
				t.Errorf("%s alone: problems = %q, want %q", directive, problems, wantProblems)
			}
		}
	})

	t.Run("a_chapter_with_no_tag_shows_the_cards_of_the_tree", func(t *testing.T) {
		f := cleanFixture(t)
		f.setTree(ch2, "{{cards}}\n\n{{reftest T02.01}}\n")
		// With no chapter tag and no session tag there is no moment to read
		// the cards at, so the cards of the tree are shown, and the reftest
		// has no code to read.
		delete(f.tags, "ch02")
		delete(f.tags, "ch02-s1")
		f.setTree("book/tests/ch02.tests", oneCard("de la copia de trabajo"))
		got, problems := f.render("02")
		if !strings.Contains(got, "| T02.01 | caso | motivo | de la copia de trabajo | core |") {
			t.Errorf("render = %q, want the card of the tree", got)
		}
		if want := []string{ch2 + ":3: chapter 02 has no tag `ch02` and no session tag `ch02-sK` to read the test from"}; !slices.Equal(problems, want) {
			t.Errorf("problems = %q, want %q", problems, want)
		}
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
