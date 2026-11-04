// bookx: not-in-book

package main

import (
	"slices"
	"strings"
	"testing"
)

// lintLines lints a chapter 11 with the given cards and returns its issues as
// `<line>: <severity> <rule>: <detail>`.
func lintLines(t *testing.T, md string, cards ...Card) []string {
	t.Helper()
	return lintIssues(lintChapter(md, cards))
}

func lintIssues(r lintResult) []string {
	var out []string
	for _, i := range r.Issues {
		out = append(out, i.String())
	}
	return out
}

// words returns n words of running text.
func words(n int) string {
	return strings.TrimSpace(strings.Repeat("palabra ", n))
}

// tableRow returns a row of a table with n words.
func tableRow(n int) string {
	return "| " + words(n) + " |\n"
}

func TestLintChapter(t *testing.T) {
	type row struct {
		name string
		md   string
		want []string
	}
	table := func(t *testing.T, rows []row) {
		t.Helper()
		for _, r := range rows {
			got := lintLines(t, r.md)
			if !slices.Equal(got, r.want) {
				t.Errorf("%s:\n got %q\nwant %q", r.name, got, r.want)
			}
		}
	}

	t.Run("budget", func(t *testing.T) {
		table(t, []row{
			{"at the budget", words(2500) + "\n", nil},
			{"one past the budget", words(2501) + "\n", []string{"1: error budget: 2501 flow words; the budget is 2500"}},
			{"a fold does not count", words(2500) + "\n\n:::more Un tema\n" + words(5) + "\n:::\n", nil},
		})
	})

	t.Run("fold_count", func(t *testing.T) {
		fold := ":::more Un tema\nuno\n:::\n"
		table(t, []row{
			{"five folds", strings.Repeat(fold, 5), nil},
			{"six folds", strings.Repeat(fold, 6), []string{"16: error fold-count: 6 folds; at most 5"}},
		})
	})

	t.Run("fold_words", func(t *testing.T) {
		table(t, []row{
			{"at the limit", ":::more Un tema\n" + words(150) + "\n:::\n", nil},
			{"one past the limit", "\n:::more Un tema\n" + words(151) + "\n:::\n", []string{"2: error fold-words: `Un tema` has 151 words; at most 150"}},
			{"code in a fold does not count", ":::more Un tema\n```go\n" + words(200) + "\n```\n:::\n", nil},
			{"a table in a fold does", ":::more Un tema\n" + tableRow(151) + ":::\n", []string{"1: error fold-words: `Un tema` has 151 words; at most 150"}},
			{"another container is not a fold", ":::solution\n" + words(151) + "\n:::\n", nil},
		})
	})

	t.Run("fold_title", func(t *testing.T) {
		table(t, []row{
			{"one topic", ":::more Qué hace `GOTOOLCHAIN`\nuno\n:::\n", nil},
			{"a comma list", ":::more Mapas, slices\nuno\n:::\n", []string{"1: error fold-title: `Mapas, slices` lists topics (a comma)"}},
			{"a comma in code is not a list", ":::more Cómo usar `f(a, b)`\nuno\n:::\n", nil},
			{"y más", ":::more Mapas y más\nuno\n:::\n", []string{"1: error fold-title: `Mapas y más` lists topics (y más)"}},
			{"eight words", ":::more uno dos tres cuatro cinco seis siete ocho\nuno\n:::\n", nil},
			{"nine words", ":::more uno dos tres cuatro cinco seis siete ocho nueve\nuno\n:::\n", []string{"1: error fold-title: `uno dos tres cuatro cinco seis siete ocho nueve` has 9 words; a title names one topic in at most 8"}},
			{"two topics joined by y", ":::more Mapas y slices\nuno\n:::\n", []string{"1: warning fold-title: `Mapas y slices` may join two topics (y)"}},
			{"y in code does not count", ":::more El comando `x y z`\nuno\n:::\n", nil},
		})
	})

	t.Run("open_tables", func(t *testing.T) {
		table(t, []row{
			{"at the limit", tableRow(200), nil},
			{"one past the limit", tableRow(100) + tableRow(101), []string{"1: error open-tables: 201 words in open tables; at most 200"}},
			{"a table in a fold is closed", ":::more Un tema\n" + tableRow(100) + ":::\n" + tableRow(200), nil},
			{"a table in a solution is closed", ":::solution\n" + tableRow(201) + ":::\n", nil},
			{"a table in an exercise is open", ":::exercise\n" + tableRow(201) + ":::\n", []string{"2: error open-tables: 201 words in open tables; at most 200"}},
			{"a table in a fence is code", "```text\n" + tableRow(201) + "```\n", nil},
			{"the separator row has no words", "|---|---|\n" + tableRow(200), nil},
		})
	})

	t.Run("gender", func(t *testing.T) {
		table(t, []row{
			{"el lector", "Lo ve el lector.\n", []string{"1: warning gender: `el lector`"}},
			{"listo", "\nCuando estés listo sigue.\n", []string{"2: warning gender: `listo`"}},
			{"a capital letter", "Bienvenido al libro.\n", []string{"1: warning gender: `Bienvenido`"}},
			{"the noun lista is fine", "Una lista y otra lista.\n", nil},
			{"in code it is not about the reader", "Usa `listo` aquí.\n", nil},
			{"in a fence neither", "```text\nlisto\n```\n", nil},
			{"a word inside another is not it", "alistados\n", nil},
			{"the title of a fold is read", ":::more Si el lector duda\nuno\n:::\n", []string{"1: warning gender: `el lector`"}},
		})
	})

	t.Run("jargon", func(t *testing.T) {
		table(t, []row{
			{"tarea", "Según la Tarea 12.\n", []string{"1: warning jargon: `Tarea 12`"}},
			{"el plan", "Lo dice el plan.\n", []string{"1: warning jargon: `el plan`"}},
			{"spec", "Mira spec 7.2.\n", []string{"1: warning jargon: `spec 7`"}},
			{"lens", "Una lente lo vio.\n", []string{"1: warning jargon: `lente`"}},
			{"a milestone is the book's word", "Es el hito M1.\n", nil},
			{"in code it is not jargon", "El campo `spec 7`.\n", nil},
		})
	})

	t.Run("test_names", func(t *testing.T) {
		table(t, []row{
			{"a test in text", "Lo prueba `TestLexer`.\n", []string{"1: warning test-name: `TestLexer`"}},
			{"a subtest", "Lo prueba `TestLexer/pipe_line`.\n", []string{"1: warning test-name: `TestLexer`"}},
			{"a fuzz target", "Y FuzzParse también.\n", []string{"1: warning test-name: `FuzzParse`"}},
			{"a command the reader runs stays", "Ejecuta `go test -run TestLexer ./x`.\n", nil},
			{"a directive is not text", "{{reftest TestLexer}}\n", nil},
			{"a solution is not running text", ":::solution\nLo prueba TestLexer.\n:::\n", nil},
			{"the shape itself is taught", "Un `TestXxx` y un `FuzzXxx`.\n", nil},
			{"a word that only starts alike", "Testing y Tested.\n", nil},
		})
		card := Card{ID: "T11.01", Name: "lexer.pipe_line", Ref: "TestLexer/pipe_line"}
		got := lintLines(t, "Lo prueba `pipe_line`.\nY `lexer.pipe_line`.\nY `pipe`.\n", card)
		want := []string{"1: warning test-name: `pipe_line`", "2: warning test-name: `lexer.pipe_line`"}
		if !slices.Equal(got, want) {
			t.Errorf("names of the cards: got %q, want %q", got, want)
		}
	})

	t.Run("card_ids", func(t *testing.T) {
		session := "## Implementación\n\n### Sesión 1: algo (≈ 2 h)\n\n"
		table(t, []row{
			{"in a session step", session + "1. **Escribe T11.01–T11.03.**\n", nil},
			{"a three-digit id in a step", session + "2) Escribe T11.100.\n", nil},
			{"in a session, not in a step", session + "Mira T11.01.\n", []string{"5: warning card-id: `T11.01`"}},
			{"in a step outside a session", "## Teoría\n\n1. Mira T11.01.\n", []string{"3: warning card-id: `T11.01`"}},
			{"a later section ends the session", session + "## Ejecútalo\n\n1. Mira T11.01.\n", []string{"7: warning card-id: `T11.01`"}},
			{"in a box inside a session", session + ":::note\n1. Mira T11.01.\n:::\n", []string{"6: warning card-id: `T11.01`"}},
			{"a directive is not text", "{{reftest T11.01}}\n", nil},
			{"inside a solution is not running text", ":::solution\nMira T11.01.\n:::\n", nil},
		})
	})

	t.Run("front_matter_is_not_text", func(t *testing.T) {
		table(t, []row{
			{"front matter", "---\ntitle: Según el plan\n---\n\nHola.\n", nil},
		})
	})

	t.Run("reports_flow_folds_tables_and_whys", func(t *testing.T) {
		md := "---\nchapter: 11\n---\nUno dos.\n\n:::more Un tema\ntres cuatro\n:::\n\n:::more Otro\ncinco\n:::\n| seis siete |\n"
		cards := []Card{{ID: "T11.01", Why: words(25)}, {ID: "T11.02", Why: words(26)}, {ID: "T11.03", Why: words(35)}, {ID: "T11.04", Why: words(36)}}
		r := lintChapter(md, cards)
		want := lintCounts{Flow: 2, Folds: 2, FoldWords: 3, TableWords: 2, Whys: 4, WhysOverTarget: 3, WhysOverLimit: 1}
		if r.Counts != want {
			t.Errorf("counts %+v, want %+v", r.Counts, want)
		}
	})
}

func TestRunLint(t *testing.T) {
	clean := "Uno dos.\n"
	broken := words(2501) + "\n"
	lints := func(t *testing.T, files map[string]string, args ...string) (int, string, string) {
		t.Helper()
		dir := t.TempDir()
		for name, text := range files {
			writeFile(t, dir, name, text)
		}
		var full []string
		for _, a := range args {
			full = append(full, dir+"/"+a)
		}
		code, stdout, stderr := runBookx(t, append([]string{"lint"}, full...)...)
		return code, strings.ReplaceAll(stdout, dir+"/", ""), stderr
	}

	t.Run("a_clean_chapter", func(t *testing.T) {
		code, out, errOut := lints(t, map[string]string{"ch11-x.md": clean}, "ch11-x.md")
		want := "lint ch11-x.md: flow 2/2500, folds 0/5 (0 words), open tables 0/200, whys 0 (over 25: 0, over 35: 0), 0 errors, 0 warnings\n"
		if code != exitOK || out != want || errOut != "" {
			t.Errorf("code %d, stdout %q, stderr %q; want 0, %q, empty", code, out, errOut, want)
		}
	})

	t.Run("an_error_fails_from_chapter_11_on", func(t *testing.T) {
		code, out, _ := lints(t, map[string]string{"ch11-x.md": broken}, "ch11-x.md")
		want := "ch11-x.md:1: error budget: 2501 flow words; the budget is 2500\n" +
			"lint ch11-x.md: flow 2501/2500, folds 0/5 (0 words), open tables 0/200, whys 0 (over 25: 0, over 35: 0), 1 errors, 0 warnings\n"
		if code != exitFail || out != want {
			t.Errorf("code %d, stdout %q; want 1, %q", code, out, want)
		}
	})

	t.Run("an_earlier_chapter_is_only_reported", func(t *testing.T) {
		code, out, _ := lints(t, map[string]string{"ch10-x.md": broken}, "ch10-x.md")
		want := "ch10-x.md:1: error budget: 2501 flow words; the budget is 2500\n" +
			"lint ch10-x.md: flow 2501/2500, folds 0/5 (0 words), open tables 0/200, whys 0 (over 25: 0, over 35: 0), 1 errors, 0 warnings (report only: before chapter 11)\n"
		if code != exitOK || out != want {
			t.Errorf("code %d, stdout %q; want 0, %q", code, out, want)
		}
	})

	t.Run("a_warning_does_not_fail", func(t *testing.T) {
		code, out, _ := lints(t, map[string]string{"ch11-x.md": "Uno, listo.\n"}, "ch11-x.md")
		if code != exitOK || !strings.HasPrefix(out, "ch11-x.md:1: warning gender: `listo`\n") {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})

	t.Run("reads_the_cards_of_the_chapter", func(t *testing.T) {
		code, out, _ := lints(t, map[string]string{
			"ch11-x.md":        "Uno `demo_case`.\n",
			"tests/ch11.tests": strings.Replace(cardsWithWhy("11", 26), "TestDemo", "TestDemo/demo_case", 1),
		}, "ch11-x.md")
		want := "ch11-x.md:1: warning test-name: `demo_case`\n" +
			"tests/ch11.tests:1: warning why-target: card T11.01: its `why` has 26 words; the target is 25\n" +
			"lint ch11-x.md: flow 2/2500, folds 0/5 (0 words), open tables 0/200, whys 1 (over 25: 1, over 35: 0), 0 errors, 2 warnings\n"
		if code != exitOK || out != want {
			t.Errorf("code %d, stdout %q; want 0, %q", code, out, want)
		}
	})

	t.Run("no_why_warning_before_chapter_11", func(t *testing.T) {
		_, out, _ := lints(t, map[string]string{
			"ch10-x.md":        "Uno dos.\n",
			"tests/ch10.tests": cardsWithWhy("10", 26),
		}, "ch10-x.md")
		want := "lint ch10-x.md: flow 2/2500, folds 0/5 (0 words), open tables 0/200, whys 1 (over 25: 1, over 35: 0), 0 errors, 0 warnings (report only: before chapter 11)\n"
		if out != want {
			t.Errorf("stdout %q, want %q", out, want)
		}
	})

	t.Run("broken_cards_fail_the_chapter", func(t *testing.T) {
		code, _, errOut := lints(t, map[string]string{
			"ch11-x.md":        "Uno dos.\n",
			"tests/ch11.tests": "not a card\n",
		}, "ch11-x.md")
		if code != exitFail || !strings.Contains(errOut, "tests/ch11.tests:1: line outside a card") {
			t.Errorf("code %d, stderr %q", code, errOut)
		}
	})

	t.Run("an_unreadable_file", func(t *testing.T) {
		code, out, errOut := lints(t, map[string]string{"ch11-x.md": clean}, "nope.md", "ch11-x.md")
		if code != exitFail || !strings.Contains(errOut, "nope.md") || !strings.Contains(out, "lint ch11-x.md:") {
			t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
		}
	})
}
