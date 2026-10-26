// bookx: not-in-book

package main

import (
	"reflect"
	"slices"
	"testing"
)

// cardsPath is the file name every card test parses under: the NN of the ids
// has to match the one in it.
const cardsPath = "book/tests/ch05.tests"

// cardLines returns the lines of a complete, valid card with the given id,
// followed by the blank line that separates cards.
func cardLines(id string) []string {
	return []string{
		"[" + id + "] cli.some_case                       level: core",
		"kind:    unit (go test)      file: internal/driver/cli_test.go",
		"case:    el caso",
		"input:   []",
		"expect:  lo esperado",
		"why:     la razón",
		"ref:     TestCLI/some_case",
		"",
	}
}

func TestParseCards(t *testing.T) {
	t.Run("full_card", func(t *testing.T) {
		// The example of the plan's «Convenciones», with the id T05.01
		// because a file has to start at 01.
		src := lines(
			"",
			"[T05.01] lexer.pipe_continues_line                         level: core",
			"kind:    unit (go test)      file: internal/syntax/line_rule_test.go",
			"case:    una línea que empieza por `|>` continúa la expresión de la línea anterior",
			"input:   \"let total = xs\\n  |> sum\\n\"",
			"expect:  ningún Newline entre `xs` y `|>`; después, Newline y Eof",
			"why:     si el lexer insertara aquí un Newline, la sentencia terminaría en `xs`",
			"         y el error saldría una línea más abajo, lejos de su causa",
			"ref:     TestLineRule/pipe_continues_line",
		)
		got, errs := parseCards(cardsPath, src)
		checkErrs(t, errs)
		want := []Card{{
			ID:     "T05.01",
			Name:   "lexer.pipe_continues_line",
			Level:  "core",
			Kind:   "unit",
			File:   "internal/syntax/line_rule_test.go",
			Case:   "una línea que empieza por `|>` continúa la expresión de la línea anterior",
			Input:  "\"let total = xs\\n  |> sum\\n\"",
			Expect: "ningún Newline entre `xs` y `|>`; después, Newline y Eof",
			Why:    "si el lexer insertara aquí un Newline, la sentencia terminaría en `xs` y el error saldría una línea más abajo, lejos de su causa",
			Ref:    "TestLineRule/pipe_continues_line",
			Line:   2,
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("cards = %+v, want %+v", got, want)
		}
	})

	t.Run("multiline_why", func(t *testing.T) {
		src := lines(
			"[T05.01] a.b level: core",
			"kind:    unit file: a_test.go",
			"case:    c",
			"expect:  e1",
			"         e2",
			"why:     w1",
			"         w2",
			"\tw3",
			"ref:     TestA",
		)
		got, errs := parseCards(cardsPath, src)
		checkErrs(t, errs)
		if len(got) != 1 {
			t.Fatalf("got %d cards, want 1", len(got))
		}
		// One space between the pieces; the indentation is not kept, and a
		// continuation line only extends the field right above it.
		if got[0].Why != "w1 w2 w3" {
			t.Errorf("Why = %q, want %q", got[0].Why, "w1 w2 w3")
		}
		if got[0].Expect != "e1 e2" {
			t.Errorf("Expect = %q, want %q", got[0].Expect, "e1 e2")
		}
		if got[0].Ref != "TestA" || got[0].Case != "c" {
			t.Errorf("Ref = %q, Case = %q; want TestA and c", got[0].Ref, got[0].Case)
		}
	})

	t.Run("missing_field", func(t *testing.T) {
		src := lines(
			"[T05.01] a.b level: core",
			"kind:    unit file: a_test.go",
			"case:    c",
			"expect:  e",
			"ref:     TestA",
		)
		got, errs := parseCards(cardsPath, src)
		checkErrs(t, errs, "book/tests/ch05.tests:1: card T05.01: missing field `why:`")
		if len(got) != 0 {
			t.Errorf("got %d cards, want 0: a card with a problem is left out", len(got))
		}
	})

	t.Run("missing_every_required_field", func(t *testing.T) {
		got, errs := parseCards(cardsPath, lines("[T05.01] a.b"))
		checkErrs(t, errs,
			"book/tests/ch05.tests:1: card T05.01: missing field `kind:`",
			"book/tests/ch05.tests:1: card T05.01: missing field `case:`",
			"book/tests/ch05.tests:1: card T05.01: missing field `expect:`",
			"book/tests/ch05.tests:1: card T05.01: missing field `why:`",
			"book/tests/ch05.tests:1: card T05.01: missing field `ref:`",
		)
		if len(got) != 0 {
			t.Errorf("got %d cards, want 0", len(got))
		}
	})

	t.Run("bad_id", func(t *testing.T) {
		src := lines(
			"[T5.7] a.b level: core",
			"kind:    unit file: a_test.go",
			"case:    c",
			"expect:  e",
			"why:     w",
			"ref:     TestA",
		)
		got, errs := parseCards(cardsPath, src)
		checkErrs(t, errs, "book/tests/ch05.tests:1: invalid card id `T5.7`: write it as TNN.MM, as in `T05.07`")
		if len(got) != 0 {
			t.Errorf("got %d cards, want 0", len(got))
		}
	})

	t.Run("gap_in_ids", func(t *testing.T) {
		src := lines(slices.Concat(cardLines("T05.01"), cardLines("T05.03"))...)
		got, errs := parseCards(cardsPath, src)
		checkErrs(t, errs, "book/tests/ch05.tests:9: card ids must be consecutive: expected T05.02, found T05.03")
		// Only the position is wrong, so both cards are still returned.
		if len(got) != 2 || got[0].ID != "T05.01" || got[1].ID != "T05.03" {
			t.Errorf("cards = %+v, want T05.01 and T05.03", got)
		}
	})

	t.Run("ids_must_start_at_01_and_not_repeat", func(t *testing.T) {
		src := lines(slices.Concat(cardLines("T05.02"), cardLines("T05.02"), cardLines("T05.03"))...)
		_, errs := parseCards(cardsPath, src)
		checkErrs(t, errs,
			"book/tests/ch05.tests:1: card ids must be consecutive: expected T05.01, found T05.02",
			"book/tests/ch05.tests:9: card ids must be consecutive: expected T05.03, found T05.02",
		)
	})

	t.Run("id_number_must_match_file", func(t *testing.T) {
		got, errs := parseCards(cardsPath, lines(cardLines("T06.01")...))
		checkErrs(t, errs, "book/tests/ch05.tests:1: card T06.01: its number must be 05, like the file `ch05.tests`")
		if len(got) != 1 {
			t.Errorf("got %d cards, want 1", len(got))
		}
		// A wrong id does not break the count of the cards after it.
		src := lines(slices.Concat(cardLines("T05.01"), cardLines("T06.02"), cardLines("T05.03"))...)
		_, errs = parseCards(cardsPath, src)
		checkErrs(t, errs, "book/tests/ch05.tests:9: card T06.02: its number must be 05, like the file `ch05.tests`")

		_, errs = parseCards("book/tests/notes.txt", lines(cardLines("T05.01")...))
		checkErrs(t, errs, "book/tests/notes.txt:1: card files are named chNN.tests, not `notes.txt`")
	})

	t.Run("orphan_header_keeps_the_count", func(t *testing.T) {
		// A header with no `]` is one mistake, not also a gap before the next card.
		src := lines(slices.Concat([]string{"[T05.01 a.b", ""}, cardLines("T05.02"), cardLines("T05.03"))...)
		got, errs := parseCards(cardsPath, src)
		checkErrs(t, errs, "book/tests/ch05.tests:1: malformed card header: write it as `[TNN.MM] name level: core`")
		if len(got) != 2 || got[0].ID != "T05.02" || got[1].ID != "T05.03" {
			t.Errorf("cards = %+v, want T05.02 and T05.03", got)
		}
	})

	t.Run("unknown_kind", func(t *testing.T) {
		src := lines(
			"[T05.01] a.b level: core",
			"kind:    ctest        file: a_test.go",
			"case:    c",
			"expect:  e",
			"why:     w",
			"ref:     TestA",
		)
		got, errs := parseCards(cardsPath, src)
		checkErrs(t, errs, "book/tests/ch05.tests:2: card T05.01: unknown kind `ctest`: want unit, prop, example, bench, golden, diag or run")
		if len(got) != 0 {
			t.Errorf("got %d cards, want 0", len(got))
		}
	})

	t.Run("prop_example_bench", func(t *testing.T) {
		src := lines(
			"[T05.01] a.prop level: core",
			"kind:    prop (fuzz)         file: a_test.go",
			"case:    c1",
			"expect:  e1",
			"why:     w1",
			"ref:     FuzzLex",
			"",
			"[T05.02] a.example level: stretch",
			"kind:    example file: b_test.go",
			"case:    c2",
			"expect:  e2",
			"why:     w2",
			"ref:     ExampleInterner",
			"",
			"[T05.03] a.bench",
			"kind:    bench (go test -bench) file: c_test.go",
			"case:    c3",
			"expect:  e3",
			"why:     w3",
			"ref:     BenchmarkLex",
		)
		got, errs := parseCards(cardsPath, src)
		checkErrs(t, errs)
		type row struct{ id, level, kind, file, ref, input string }
		want := []row{
			{"T05.01", "core", "prop", "a_test.go", "FuzzLex", ""},
			{"T05.02", "stretch", "example", "b_test.go", "ExampleInterner", ""},
			{"T05.03", "core", "bench", "c_test.go", "BenchmarkLex", ""},
		}
		if len(got) != len(want) {
			t.Fatalf("got %d cards, want %d", len(got), len(want))
		}
		for i, w := range want {
			g := row{got[i].ID, got[i].Level, got[i].Kind, got[i].File, got[i].Ref, got[i].Input}
			if g != w {
				t.Errorf("card %d = %+v, want %+v", i, g, w)
			}
		}
	})

	t.Run("golden_diag_run", func(t *testing.T) {
		src := lines(
			"[T05.01] g.one level: core",
			"kind:    golden              file: tests/golden/ch05/a.ki",
			"case:    c1",
			"expect:  e1",
			"why:     w1",
			"ref:     tests/golden/ch05/a.ki",
			"",
			"[T05.02] g.two level: core",
			"kind:    diag (snapshot)     file: tests/errors/ch05/b.ki",
			"case:    c2",
			"expect:  e2",
			"why:     w2",
			"ref:     tests/errors/ch05/b.ki",
			"",
			"[T05.03] g.three level: core",
			"kind:    run                 file: tests/golden/ch05/c.ki",
			"case:    c3",
			"expect:  e3",
			"why:     w3",
			"ref:     tests/golden/ch05/c.ki",
		)
		got, errs := parseCards(cardsPath, src)
		checkErrs(t, errs)
		var kinds, files []string
		for _, c := range got {
			kinds = append(kinds, c.Kind)
			files = append(files, c.File)
		}
		if want := []string{"golden", "diag", "run"}; !slices.Equal(kinds, want) {
			t.Errorf("kinds = %q, want %q", kinds, want)
		}
		wantFiles := []string{"tests/golden/ch05/a.ki", "tests/errors/ch05/b.ki", "tests/golden/ch05/c.ki"}
		if !slices.Equal(files, wantFiles) {
			t.Errorf("files = %q, want %q", files, wantFiles)
		}
	})

	t.Run("level", func(t *testing.T) {
		card := func(header string) []byte {
			return lines(
				header,
				"kind:    unit file: a_test.go",
				"case:    c",
				"expect:  e",
				"why:     w",
				"ref:     TestA",
			)
		}
		rows := []struct{ header, level, err string }{
			{"[T05.01] a.b", "core", ""},
			{"[T05.01] a.b level: core", "core", ""},
			{"[T05.01] a.b level: stretch", "stretch", ""},
			{"[T05.01] a.b level: optional", "", "book/tests/ch05.tests:1: card T05.01: unknown level `optional`: want core or stretch"},
			{"[T05.01] a.b level:", "", "book/tests/ch05.tests:1: card T05.01: unknown level ``: want core or stretch"},
			{"[T05.01] a.b stretch", "", "book/tests/ch05.tests:1: card T05.01: after the name, the header can only say `level: core` or `level: stretch`"},
			{"[T05.01]", "", "book/tests/ch05.tests:1: card T05.01: the header needs a name after the id"},
		}
		for _, r := range rows {
			got, errs := parseCards(cardsPath, card(r.header))
			if r.err == "" {
				if len(errs) != 0 || len(got) != 1 || got[0].Level != r.level {
					t.Errorf("%s: cards = %+v, errors = %q; want level %q and no errors", r.header, got, errStrings(errs), r.level)
				}
				continue
			}
			if g := errStrings(errs); !slices.Equal(g, []string{r.err}) || len(got) != 0 {
				t.Errorf("%s: cards = %+v, errors = %q; want no cards and %q", r.header, got, g, r.err)
			}
		}
	})

	t.Run("malformed_lines", func(t *testing.T) {
		const p = "book/tests/ch05.tests:"
		rows := []struct {
			name string
			src  []byte
			want []string
		}{
			{
				"text before the first card",
				lines("hola", "", cardLines("T05.01")[0], "kind: unit file: a_test.go", "case: c", "expect: e", "why: w", "ref: TestA"),
				[]string{p + "1: line outside a card"},
			},
			{
				"unknown field",
				lines(slices.Insert(cardLines("T05.01"), 6, "color:   azul")...),
				[]string{p + "7: card T05.01: unknown field `color:`"},
			},
			{
				"unknown field with continuation",
				lines(slices.Insert(cardLines("T05.01"), 6, "color:   azul", "         verde")...),
				[]string{p + "7: card T05.01: unknown field `color:`"},
			},
			{
				"duplicate field",
				lines(slices.Insert(cardLines("T05.01"), 6, "why:     otra")...),
				[]string{p + "7: card T05.01: field `why:` appears twice"},
			},
			{
				"level as a field",
				lines(slices.Insert(cardLines("T05.01"), 6, "level:   core")...),
				[]string{p + "7: card T05.01: unknown field `level:`"},
			},
			{
				"line that is not a field",
				lines(slices.Insert(cardLines("T05.01"), 6, "no es un campo")...),
				[]string{p + "7: card T05.01: unexpected line"},
			},
			{
				"continuation right after the header",
				lines(slices.Insert(cardLines("T05.01"), 1, "         suelta")...),
				[]string{p + "2: card T05.01: indented line with no field above it"},
			},
			{
				"field after a blank line",
				lines(slices.Concat(cardLines("T05.01"), []string{"why:     otra"})...),
				[]string{p + "9: line outside a card"},
			},
			{
				"header without bracket",
				lines("[T05.01 a.b"),
				[]string{p + "1: malformed card header: write it as `[TNN.MM] name level: core`"},
			},
			{
				"kind without file",
				lines("[T05.01] a.b", "kind: unit", "case: c", "expect: e", "why: w", "ref: TestA"),
				[]string{p + "2: card T05.01: the kind line needs `file: PATH` after the kind"},
			},
			{
				"kind with unclosed parenthesis",
				lines("[T05.01] a.b", "kind: unit (go test file: a_test.go", "case: c", "expect: e", "why: w", "ref: TestA"),
				[]string{p + "2: card T05.01: the kind line needs `file: PATH` after the kind"},
			},
			{
				"file with spaces",
				lines("[T05.01] a.b", "kind: unit file: a b.go", "case: c", "expect: e", "why: w", "ref: TestA"),
				[]string{p + "2: card T05.01: the file must be a single path without spaces"},
			},
		}
		for _, r := range rows {
			_, errs := parseCards(cardsPath, r.src)
			if g := errStrings(errs); !slices.Equal(g, r.want) {
				t.Errorf("%s: errors = %q, want %q", r.name, g, r.want)
			}
		}
	})

	t.Run("errors_sorted_by_line", func(t *testing.T) {
		// The unknown field is found while reading line 4, the missing `why:`
		// only when the card ends; the second belongs to the earlier line.
		src := lines(
			"[T05.01] a.b level: core",
			"kind:    unit file: a_test.go",
			"case:    c",
			"color:   azul",
			"expect:  e",
			"ref:     TestA",
		)
		_, errs := parseCards(cardsPath, src)
		checkErrs(t, errs,
			"book/tests/ch05.tests:1: card T05.01: missing field `why:`",
			"book/tests/ch05.tests:4: card T05.01: unknown field `color:`",
		)
	})

	t.Run("empty_file", func(t *testing.T) {
		got, errs := parseCards(cardsPath, nil)
		checkErrs(t, errs)
		if len(got) != 0 {
			t.Errorf("got %d cards, want 0", len(got))
		}
	})

	t.Run("crlf_and_no_final_newline", func(t *testing.T) {
		// The `case` has a lone "\r" inside it, which is text and not a line end.
		src := []byte("[T05.01] a.b level: core\r\nkind:    unit file: a_test.go\r\ncase:    c\rd\r\n" +
			"expect:  e\r\nwhy:     w1\r\n         w2\r\nref:     TestA")
		got, errs := parseCards(cardsPath, src)
		checkErrs(t, errs)
		if len(got) != 1 || got[0].Case != "c\rd" || got[0].Why != "w1 w2" || got[0].Ref != "TestA" {
			t.Errorf("cards = %+v, want one card with Case \"c\\rd\", Why \"w1 w2\" and Ref TestA", got)
		}
	})
}
