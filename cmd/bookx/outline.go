package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// This file reads the index of the book out of the spec: the eleven parts and
// the sixty-eight chapters of section 12.5, with their hours and milestones.
// The page of the book draws its table of contents from the result, so a
// chapter is listed there before its prose exists.

// Outline is the content of book/outline.json: the parts of the book with
// their chapters, and the number of exercises of each chapter that has been
// rendered (keyed by the chapter number written in decimal, as JSON requires
// string keys).
type Outline struct {
	Parts     []OutlinePart  `json:"parts"`
	Exercises map[string]int `json:"exercises"`
}

// OutlinePart is one part of the book. Hours is the figure of the heading of
// the part, and Milestones is the text after the arrow, such as "M2, M3".
type OutlinePart struct {
	Part       string           `json:"part"`
	Title      string           `json:"title"`
	Hours      int              `json:"hours"`
	Milestones string           `json:"milestones"`
	Chapters   []OutlineChapter `json:"chapters"`
}

// OutlineChapter is one chapter of the index. The two rows of a chapter that
// the spec splits in two (54a and 54b) are a single OutlineChapter.
type OutlineChapter struct {
	Chapter int    `json:"chapter"`
	Title   string `json:"title"`
	Hours   int    `json:"hours"`
}

// partHeadingRe is the heading of a part in section 12.5:
// "#### Parte II. El núcleo funcional en el oráculo (≈ 102 h → M2, M3)".
var partHeadingRe = regexp.MustCompile(`^#### Parte ([IVXLC]+)\. (.+) \(≈ (\d+) h → (.+)\)$`)

// chapterRowRe is the first cell of a row of the index: the chapter number,
// with a letter when the spec splits the chapter in two rows (54a, 54b).
var chapterRowRe = regexp.MustCompile(`^(\d+)([a-z]?)$`)

// mergedTitles are the titles of the chapters that the spec writes as two rows.
// One title cannot be derived from two, so each such chapter names its own.
var mergedTitles = map[int]string{
	54: "Fibras nativas: `kiResume`, el portador A y la prueba a tres bandas",
}

// pencilMark is the mark ✎ that the spec puts before the title of a chapter
// whose prose is still to be written by hand. It is not part of the title.
const pencilMark = "✎"

// splitRow splits a line of a Markdown table into its cells. A cell ends at
// the next `|` that is not escaped: `\|` stands for a literal pipe inside the
// cell and is returned as `|`. Any other backslash stays as it is. The pipes
// at the two ends of the line, if any, do not make empty cells.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	cells = append(cells, cur.String())
	if len(cells) > 0 && strings.TrimSpace(cells[0]) == "" {
		cells = cells[1:]
	}
	if n := len(cells); n > 0 && strings.TrimSpace(cells[n-1]) == "" {
		cells = cells[:n-1]
	}
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}

// isSeparatorRow reports whether the cells are the `|---|:---:|` row that
// follows the header of a table.
func isSeparatorRow(cells []string) bool {
	for _, c := range cells {
		if c == "" || strings.Trim(c, "-:") != "" {
			return false
		}
	}
	return len(cells) > 0
}

// parseSpecIndex reads the index of the book from the text of the spec. The
// index is made of the headings "#### Parte X. Título (≈ H h → M…)" and, below
// each, a table with the columns # | Capítulo | Hito | Go nuevo | Compiladores
// | h. Any other line is ignored, so the whole spec can be passed. A table row
// that does not have those six cells, or a number or hours that cannot be read,
// is an error that names the line. The rows of one chapter (54a and 54b) are
// merged: their hours are added and the title comes from mergedTitles.
func parseSpecIndex(spec []byte) (Outline, error) {
	out := Outline{Parts: []OutlinePart{}, Exercises: map[string]int{}}
	var part *OutlinePart
	rows := map[int]int{} // chapter number -> rows read so far
	for i, line := range splitLines(spec) {
		n := i + 1
		if strings.HasPrefix(line, "#") {
			part = nil // a table outside a part is not read
			if !strings.HasPrefix(line, "#### Parte ") {
				continue
			}
			m := partHeadingRe.FindStringSubmatch(line)
			if m == nil {
				return Outline{}, fmt.Errorf("line %d: cannot read the heading of the part: %q", n, line)
			}
			hours, err := strconv.Atoi(m[3])
			if err != nil {
				return Outline{}, fmt.Errorf("line %d: the hours of the part are %q, not a number that fits: %q", n, m[3], line)
			}
			out.Parts = append(out.Parts, OutlinePart{Part: m[1], Title: m[2], Hours: hours, Milestones: m[4], Chapters: []OutlineChapter{}})
			part = &out.Parts[len(out.Parts)-1]
			continue
		}
		if part == nil || !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := splitRow(line)
		if isSeparatorRow(cells) || (len(cells) > 0 && cells[0] == "#") {
			continue
		}
		if len(cells) != 6 {
			return Outline{}, fmt.Errorf("line %d: a row of the index has %d cells, want 6: %q", n, len(cells), line)
		}
		m := chapterRowRe.FindStringSubmatch(cells[0])
		if m == nil {
			return Outline{}, fmt.Errorf("line %d: %q is not a chapter number", n, cells[0])
		}
		num, err := strconv.Atoi(m[1])
		if err != nil {
			return Outline{}, fmt.Errorf("line %d: %q is not a chapter number that fits: %q", n, cells[0], line)
		}
		hours, err := strconv.Atoi(cells[5])
		if err != nil {
			return Outline{}, fmt.Errorf("line %d: the hours of chapter %s are %q, not a number", n, cells[0], cells[5])
		}
		title := strings.TrimSpace(strings.ReplaceAll(cells[1], pencilMark, ""))

		rows[num]++
		if rows[num] > 1 {
			// A second row of the same chapter has to follow the first one.
			if len(part.Chapters) == 0 || part.Chapters[len(part.Chapters)-1].Chapter != num {
				return Outline{}, fmt.Errorf("line %d: chapter %d appears twice, and not in rows next to each other", n, num)
			}
			part.Chapters[len(part.Chapters)-1].Hours += hours
			continue
		}
		part.Chapters = append(part.Chapters, OutlineChapter{Chapter: num, Title: title, Hours: hours})
	}
	if len(out.Parts) == 0 {
		return Outline{}, fmt.Errorf("the spec has no heading like \"#### Parte I. Título (≈ 78 h → M1)\"")
	}
	for pi := range out.Parts {
		for ci := range out.Parts[pi].Chapters {
			c := &out.Parts[pi].Chapters[ci]
			if rows[c.Chapter] < 2 {
				continue
			}
			title, ok := mergedTitles[c.Chapter]
			if !ok {
				return Outline{}, fmt.Errorf("chapter %d is written in %d rows and bookx has no merged title for it", c.Chapter, rows[c.Chapter])
			}
			c.Title = title
		}
	}
	return out, nil
}

// countExercises counts the `:::exercise` containers of a Markdown text. The
// lines inside a fenced code block do not count, because a listing may quote
// the text of the container.
func countExercises(markdown []byte) int {
	count := 0
	var open fence
	for _, line := range splitLines(markdown) {
		if open.char != 0 {
			if open.closedBy(line) {
				open = fence{}
			}
			continue
		}
		if f, ok := openingFence(line); ok {
			open = f
			continue
		}
		if m := containerOpenRe.FindStringSubmatch(line); m != nil && m[1] == "exercise" {
			count++
		}
	}
	return count
}
