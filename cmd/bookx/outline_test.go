// bookx: not-in-book

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specIndex parses testdata/spec-index.md, the tables of section 12.5 of the
// spec, and fails the test if they do not parse.
func specIndex(t *testing.T) Outline {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "spec-index.md"))
	if err != nil {
		t.Fatalf("reading the copy of the index: %v", err)
	}
	out, err := parseSpecIndex(data)
	if err != nil {
		t.Fatalf("parseSpecIndex: %v", err)
	}
	return out
}

// chapterOf returns chapter n of the outline, or fails the test.
func chapterOf(t *testing.T, o Outline, n int) OutlineChapter {
	t.Helper()
	for _, p := range o.Parts {
		for _, c := range p.Chapters {
			if c.Chapter == n {
				return c
			}
		}
	}
	t.Fatalf("the outline has no chapter %d", n)
	return OutlineChapter{}
}

func TestParseSpecIndex(t *testing.T) {
	t.Run("sixty_eight_chapters", func(t *testing.T) {
		o := specIndex(t)
		if len(o.Parts) != 11 {
			t.Errorf("%d parts, want 11", len(o.Parts))
		}
		next := 1
		for _, p := range o.Parts {
			for _, c := range p.Chapters {
				// Numbers 1..68 in order: nothing is lost, repeated or shuffled,
				// and 54a and 54b are one chapter, not two.
				if c.Chapter != next {
					t.Errorf("part %s: found chapter %d where chapter %d should be", p.Part, c.Chapter, next)
				}
				next = c.Chapter + 1
			}
		}
		if next != 69 {
			t.Errorf("the chapters end at %d, want 68", next-1)
		}
		// The parts keep the order and the titles of the headings.
		first, last := o.Parts[0], o.Parts[len(o.Parts)-1]
		if first.Part != "I" || first.Title != "Fundamentos y primer programa" {
			t.Errorf("first part = %q %q, want I and \"Fundamentos y primer programa\"", first.Part, first.Title)
		}
		if last.Part != "XI" || last.Title != "Otras plataformas" {
			t.Errorf("last part = %q %q, want XI and \"Otras plataformas\"", last.Part, last.Title)
		}
	})

	t.Run("chapter_hours_sum_794", func(t *testing.T) {
		o := specIndex(t)
		total := 0
		for _, p := range o.Parts {
			sum := 0
			for _, c := range p.Chapters {
				sum += c.Hours
			}
			if sum != p.Hours {
				t.Errorf("part %s: the heading says %d h, its chapters add up to %d h", p.Part, p.Hours, sum)
			}
			total += sum
		}
		if total != 794 {
			t.Errorf("the chapters add up to %d h, want 794", total)
		}
		// 54a and 54b (14 h each) are one chapter of 28 h with the merged title.
		c54 := chapterOf(t, o, 54)
		if c54.Hours != 28 {
			t.Errorf("chapter 54 has %d h, want 28", c54.Hours)
		}
		if want := "Fibras nativas: `kiResume`, el portador A y la prueba a tres bandas"; c54.Title != want {
			t.Errorf("chapter 54 title = %q, want %q", c54.Title, want)
		}
	})

	t.Run("escaped_pipe_rows", func(t *testing.T) {
		o := specIndex(t)
		rows := []struct {
			n     int
			title string
			hours int
		}{
			{15, "Closures, lambdas, pipes y métodos", 10},
			{51, "Marcos, llamadas, tail calls y polls", 16},
		}
		for _, r := range rows {
			c := chapterOf(t, o, r.n)
			if c.Title != r.title || c.Hours != r.hours {
				t.Errorf("chapter %d = %q, %d h; want %q, %d h", r.n, c.Title, c.Hours, r.title, r.hours)
			}
		}

		// The cell is cut at a pipe that is not escaped, and `\|` is a pipe in the text.
		got := splitRow("| 1 | a \\|> b | `x \\| y` |")
		if want := []string{"1", "a |> b", "`x | y`"}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("splitRow = %q, want %q", got, want)
		}
	})

	t.Run("strips_pencil_mark", func(t *testing.T) {
		o := specIndex(t)
		if title := chapterOf(t, o, 1).Title; !strings.HasPrefix(title, "El taller") {
			t.Errorf("chapter 1 title = %q, want it to start with \"El taller\"", title)
		}
		for _, p := range o.Parts {
			for _, c := range p.Chapters {
				if strings.Contains(c.Title, pencilMark) || c.Title != strings.TrimSpace(c.Title) {
					t.Errorf("chapter %d title = %q: it keeps the mark or a space", c.Chapter, c.Title)
				}
			}
		}
	})

	t.Run("milestones_from_heading", func(t *testing.T) {
		o := specIndex(t)
		want := []string{"M1", "M2, M3", "M4", "M5", "M6", "M7", "M8", "M9, M10", "M11", "M12", "M13, M14"}
		if len(o.Parts) != len(want) {
			t.Fatalf("%d parts, want %d", len(o.Parts), len(want))
		}
		for i, p := range o.Parts {
			if p.Milestones != want[i] {
				t.Errorf("part %s milestones = %q, want %q", p.Part, p.Milestones, want[i])
			}
		}
	})

	t.Run("rejects_malformed_input", func(t *testing.T) {
		const head = "#### Parte I. Uno (≈ 5 h → M1)\n\n| # | Capítulo | Hito | Go nuevo | Compiladores | h |\n|---|---|---|---|---|---|\n"
		rows := []struct {
			name string
			spec string
			want string // the error has to contain it
		}{
			{"no parts", "# Nada\n\n| 1 | a | b | c | d | 1 |\n", "the spec has no heading"},
			{"bad heading", "#### Parte I. Uno (5 horas)\n", "line 1: cannot read the heading of the part"},
			{"five cells", head + "| 1 | a | b | c | 1 |\n", "line 5: a row of the index has 5 cells, want 6"},
			{"seven cells from an unescaped pipe", head + "| 1 | a | b | c | d | e | 1 |\n", "line 5: a row of the index has 7 cells, want 6"},
			{"chapter is not a number", head + "| uno | a | b | c | d | 1 |\n", "line 5: \"uno\" is not a chapter number"},
			{"hours are not a number", head + "| 1 | a | b | c | d | muchas |\n", "line 5: the hours of chapter 1 are \"muchas\""},
			{"chapter twice", head + "| 1 | a | b | c | d | 1 |\n| 2 | a | b | c | d | 1 |\n| 1 | a | b | c | d | 1 |\n", "line 7: chapter 1 appears twice"},
			{"part hours too big", "#### Parte I. Uno (≈ 99999999999999999999 h → M1)\n", "line 1: the hours of the part are \"99999999999999999999\""},
			{"chapter number too big", head + "| 99999999999999999999 | a | b | c | d | 1 |\n", "line 5: \"99999999999999999999\" is not a chapter number that fits"},
			{"two rows with no merged title", head + "| 7a | a | b | c | d | 1 |\n| 7b | a | b | c | d | 1 |\n", "chapter 7 is written in 2 rows"},
		}
		for _, r := range rows {
			_, err := parseSpecIndex([]byte(r.spec))
			if err == nil || !strings.Contains(err.Error(), r.want) {
				t.Errorf("%s: error = %v, want one that contains %q", r.name, err, r.want)
			}
		}
	})
}

func TestCountExercises(t *testing.T) {
	t.Run("ignores_fences", func(t *testing.T) {
		text := strings.Join([]string{
			"# Capítulo",
			"",
			":::exercise",
			"Uno.",
			":::",
			"",
			"```text",
			":::exercise",
			"dentro de una valla",
			":::",
			"```",
			"",
			"````markdown",
			"```",
			":::exercise", // still inside the four-backtick fence
			"````",
			"",
			"~~~",
			":::exercise",
			"~~~",
			"",
			":::solution",
			"No es un ejercicio.",
			":::",
			"",
			":::exercises",
			"Tampoco: otro tipo.",
			":::",
			"",
			":::exercise",
			"Dos.",
			":::",
			"",
		}, "\n")
		if got := countExercises([]byte(text)); got != 2 {
			t.Errorf("countExercises = %d, want 2", got)
		}
		if got := countExercises(nil); got != 0 {
			t.Errorf("countExercises of nothing = %d, want 0", got)
		}
	})
}
