// bookx: not-in-book

package main

import (
	"slices"
	"strings"
	"testing"
)

// cardsWithWhy is a file of one card whose `why` has n words. The words are
// split over two lines, because a continuation line belongs to the field.
func cardsWithWhy(nn string, n int) string {
	words := strings.Fields(strings.Repeat("palabra ", n))
	first, rest := words[:n/2], words[n/2:]
	return "[T" + nn + ".01] demo.case level: core\n" +
		"kind:    unit (go test)      file: internal/demo/demo_test.go\n" +
		"case:    un caso\n" +
		"expect:  un resultado\n" +
		"why:     " + strings.Join(first, " ") + "\n" +
		"         " + strings.Join(rest, " ") + "\n" +
		"ref:     TestDemo\n"
}

func TestWhyCap(t *testing.T) {
	type row struct {
		name  string
		nn    string
		words int
		want  []string
	}
	const over = "book/tests/ch11.tests:1: card T11.01: its `why` has 36 words; the limit is 35 and the target 25"
	rows := []row{
		{"at the target", "11", 25, nil},
		{"over the target, under the limit", "11", 26, nil},
		{"at the limit", "11", 35, nil},
		{"one past the limit", "11", 36, []string{over}},
		{"a later chapter too", "12", 36, []string{strings.NewReplacer("ch11", "ch12", "T11", "T12").Replace(over)}},
		{"the last exempt chapter", "10", 36, nil},
		{"an earlier chapter is exempt however long", "03", 500, nil},
	}
	t.Run("from_chapter_11_on", func(t *testing.T) {
		for _, r := range rows {
			f := cleanFixture(t)
			f.setTree("book/tests/ch"+r.nn+".tests", cardsWithWhy(r.nn, r.words))
			if got := f.problems(); !slices.Equal(got, r.want) {
				t.Errorf("%s: verify says %q, want %q", r.name, got, r.want)
			}
		}
	})
	t.Run("words_are_those_of_bookx_words", func(t *testing.T) {
		// A dash or an arrow is not a word, so 35 words and a dash still pass.
		f := cleanFixture(t)
		f.setTree("book/tests/ch11.tests", strings.Replace(cardsWithWhy("11", 35), "palabra", "palabra — →", 1))
		if got := f.problems(); len(got) != 0 {
			t.Errorf("verify says %q, want no problem", got)
		}
	})
}
