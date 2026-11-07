// bookx: not-in-book

package main

import (
	"strings"
	"testing"
)

func TestFlowWords(t *testing.T) {
	// table runs rows of {name, markdown, want}. The rows are not subtests: a
	// failing row is named in the message and the others still run.
	type row struct {
		name string
		md   string
		want int
	}
	table := func(t *testing.T, rows []row) {
		t.Helper()
		for _, r := range rows {
			if got := flowWords([]byte(r.md)); got != r.want {
				t.Errorf("%s: flowWords(%q) = %d, want %d", r.name, r.md, got, r.want)
			}
		}
	}

	t.Run("counts_words_of_running_text", func(t *testing.T) {
		table(t, []row{
			{"empty file", "", 0},
			{"only blank lines", "\n\n  \n", 0},
			{"one paragraph", "Uno dos tres.\n", 3},
			{"no final newline", "Uno dos tres.", 3},
			{"words on several lines", "Uno dos\ntres cuatro\n\ncinco\n", 5},
			{"tabs and runs of spaces separate words", "uno\t\tdos   tres\n", 3},
			{"CRLF line ends", "uno dos\r\ntres\r\n", 3},
			{"a heading is text", "## Objetivo del capítulo\n", 3},
			{"a block quote is text", "> uno dos\n", 2},
			{"inline code is part of the text", "Usa `fmt.Println` y `x` aquí.\n", 5},
			{"a directive inside inline code is a word", "Usa `{{cards}}` aquí.\n", 3},
		})
	})

	t.Run("a_word_has_a_letter_or_a_digit", func(t *testing.T) {
		table(t, []row{
			{"dashes and arrows are not words", "uno — dos → tres - cuatro\n", 4},
			{"bullet markers are not words", "- uno dos\n* tres\n+ cuatro\n", 4},
			{"numbered markers are not words", "1. uno dos\n2) tres\n10. cuatro\n", 4},
			{"a number is a word", "- 42 cosas\n", 2},
			{"a number that is not a marker is a word", "Hay 3. cosas\n", 3},
			{"an operator inside inline code is not a word", "`a + b`\n", 2},
			{"accented and non-Latin letters are letters", "ñandú ключ 語\n", 3},
		})
	})

	t.Run("front_matter_is_not_text", func(t *testing.T) {
		table(t, []row{
			{"a closed block is skipped", "---\nchapter: 1\ntitle: Dos tres cuatro\n---\nUno.\n", 1},
			{"the block after a byte order mark", "\xef\xbb\xbf---\nchapter: 1\n---\nUno.\n", 1},
			{"a block that is never closed is text", "---\nuno dos\n", 2},
			{"a rule in the body is not front matter", "Uno.\n\n---\n\nDos tres.\n", 3},
			{"the block must start on the first line", "\n---\nuno dos\n---\ntres\n", 3},
		})
	})

	t.Run("code_fences_are_not_text", func(t *testing.T) {
		table(t, []row{
			{"the body, the info string and the fences", "Antes uno.\n```go title=\"a b\"\nx := 1\n```\nDespués dos.\n", 4},
			{"a longer fence is not closed by a shorter one", "````text\n```\nno cuenta\n```\n````\nfin del texto\n", 3},
			{"a tilde fence", "~~~\nx y z\n~~~\nuno\n", 1},
			{"a fence that is never closed swallows the rest", "uno\n```\ndos tres\n", 1},
			{"the closing fence may be longer", "```\nx y\n`````\nuno\n", 1},
			{"a fence closes only with its own character", "~~~\nx\n```\ny z\n~~~\nuno\n", 1},
			{"a math block", "Uno.\n```math\nΓ ⊢ e : τ\n───────────\nΓ ⊢ f e : σ\n```\nDos.\n", 2},
			{"a pseudocode block", "Uno.\n```pseudo\nfunción f(x)\n  si x entonces devuelve 1\nfin\n```\nDos.\n", 2},
		})
	})

	t.Run("tables_and_directives_are_not_text", func(t *testing.T) {
		table(t, []row{
			{"a table", "| a | b |\n|---|---|\n| uno dos | tres |\nTexto aparte.\n", 2},
			{"a table row with leading spaces", "  | uno dos |\ntres\n", 1},
			{"a directive alone on its line", "{{cards}}\n  {{listing ch01-s1 driver.main}}\n{{diff a..b x.go}}\nuno\n", 1},
			{"a line that only starts with braces is text", "{{ uno dos tres\n", 3},
			{"a directive inside a fence is not seen as one", "```\n{{cards}}\n```\nuno\n", 1},
		})
	})

	t.Run("containers_that_are_text_and_containers_that_are_not", func(t *testing.T) {
		for _, c := range []struct {
			kind string
			want int
		}{
			{"exercise", 4}, {"review", 4}, {"from Rust", 4}, {"note", 4}, {"bug", 4},
			{"solution", 0}, {"answer", 0}, {"more Detalle largo", 0},
		} {
			md := ":::" + c.kind + "\nuno dos\ntres cuatro\n:::\n"
			if got := flowWords([]byte(md)); got != c.want {
				t.Errorf(":::%s: flowWords(%q) = %d, want %d", c.kind, md, got, c.want)
			}
		}
		table(t, []row{
			{"the text after an excluded container counts again", ":::solution\ntres cuatro cinco\n:::\nseis\n", 1},
			{"the text before it counts", "uno\n:::answer\ndos tres\n:::\ncuatro\n", 2},
			{"the marker lines are not words", ":::from Rust\nuno\n:::\n:::more Detalle largo\ndos\n:::\n", 1},
			{"an exercise and its solution", ":::exercise\nuno dos\n:::\n\n:::solution\ntres cuatro cinco\n:::\n", 2},
			{"a container that is never closed swallows the rest", ":::more x\nuno dos\n", 0},
			{"a fence inside a counted container is still code", ":::exercise\nuno\n```\nx y\n```\ndos\n:::\n", 2},
			{"a ::: line inside a fence does not close an excluded container", ":::solution\n```text\n:::\n```\nsigue excluido\n:::\nfin\n", 1},
			{"a ::: line inside a fence does not open a container", "```text\n:::solution\n```\nuno dos\n", 2},
			{"a table inside a counted container is still a table", ":::note\n| a | b |\n|---|---|\nuno\n:::\n", 1},
		})
	})

	t.Run("a_whole_chapter", func(t *testing.T) {
		// One chapter with every kind of line, counted by hand.
		md := strings.Join([]string{
			"---",                    // front matter: 0
			"chapter: 1",             //
			"title: El taller",       //
			"---",                    //
			"",                       //
			"## Objetivo",            // 1
			"",                       //
			"Dejas el taller listo.", // 4
			"",
			"{{cards}}", // 0
			"",
			"| Id | Caso |", // 0
			"|---|---|",
			"| T01.01 | uno dos |",
			"",
			"```go", // 0
			"func main() {}",
			"```",
			"",
			":::from Rust",             // 0
			"Aquí no hay `ownership`.", // 4
			":::",
			"",
			":::exercise",       // 0
			"Cambia el código.", // 3
			":::",
			":::solution", // 0
			"Devuelve uno, siempre.",
			":::",
			":::more Todos los códigos", // 0
			"Una lista larga de valores.",
			":::",
			"- Fin del capítulo.", // 3
		}, "\n")
		if got, want := flowWords([]byte(md)), 1+4+4+3+3; got != want {
			t.Errorf("flowWords = %d, want %d", got, want)
		}
	})
}
