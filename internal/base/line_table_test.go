package base

import (
	"fmt"
	"testing"
)

// newLineTableManager returns a manager holding a decoy file and then a file
// with the given text, and the id of the second one. The decoy has a different
// line layout, so a method that ignores its FileID argument answers wrongly.
func newLineTableManager(text string) (*SourceManager, FileID) {
	m := &SourceManager{}
	m.Add("decoy.ki", []byte("\n\n\nxx\nyy\n"))
	return m, m.Add("main.ki", []byte(text))
}

// mustPanic runs f and fails the test unless it panics with exactly want.
func mustPanic(t *testing.T, name, want string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("%s: did not panic, want panic %q", name, want)
			return
		}
		if got := fmt.Sprint(r); got != want {
			t.Errorf("%s: panic = %q, want %q", name, got, want)
		}
	}()
	f()
}

func TestLineTable(t *testing.T) {
	t.Run("first_char", func(t *testing.T) {
		m, id := newLineTableManager("ab\ncd")
		if got, want := m.LineCol(id, 0), (LineCol{1, 1}); got != want {
			t.Errorf("LineCol(0) = %v, want %v", got, want)
		}
		// A byte in the middle of the first line, to tell "column = offset + 1"
		// from a constant.
		if got, want := m.LineCol(id, 1), (LineCol{1, 2}); got != want {
			t.Errorf("LineCol(1) = %v, want %v", got, want)
		}
	})

	t.Run("after_newline", func(t *testing.T) {
		m, id := newLineTableManager("ab\ncd")
		if got, want := m.LineCol(id, 3), (LineCol{2, 1}); got != want {
			t.Errorf("LineCol(3) = %v, want %v", got, want)
		}
		// A byte past the start of line 2, which the binary search does not find
		// in the table: the column is measured from the start of that line.
		if got, want := m.LineCol(id, 4), (LineCol{2, 2}); got != want {
			t.Errorf("LineCol(4) = %v, want %v", got, want)
		}

		// With three lines the middle one is neither the first nor the last, so
		// the table must add up the offsets of every line before it, and the
		// binary search must also work inside a line that is not the last.
		m, id = newLineTableManager("ab\ncd\nef")
		if got, want := m.LineCol(id, 4), (LineCol{2, 2}); got != want {
			t.Errorf("three lines: LineCol(4) = %v, want %v", got, want)
		}
		if got, want := m.LineCol(id, 6), (LineCol{3, 1}); got != want {
			t.Errorf("three lines: LineCol(6) = %v, want %v", got, want)
		}
		if got, want := m.LineCol(id, 7), (LineCol{3, 2}); got != want {
			t.Errorf("three lines: LineCol(7) = %v, want %v", got, want)
		}
	})

	t.Run("end_of_line", func(t *testing.T) {
		m, id := newLineTableManager("ab\ncd")
		// Offset 2 is the '\n' itself: it still belongs to line 1.
		if got, want := m.LineCol(id, 2), (LineCol{1, 3}); got != want {
			t.Errorf("LineCol(2) = %v, want %v", got, want)
		}
	})

	t.Run("end_of_file", func(t *testing.T) {
		m, id := newLineTableManager("ab\n")
		// The offset equal to the size is valid, and after a final newline it is
		// the start of an (empty) line 2.
		if got, want := m.LineCol(id, 3), (LineCol{2, 1}); got != want {
			t.Errorf("LineCol(3) = %v, want %v", got, want)
		}
	})

	t.Run("line_count", func(t *testing.T) {
		rows := []struct {
			name, text string
			want       uint32
		}{
			{"no_final_newline", "a\nb", 2},
			{"final_newline", "a\nb\n", 3},
		}
		for _, row := range rows {
			m, id := newLineTableManager(row.text)
			if got := m.LineCount(id); got != row.want {
				t.Errorf("%s: LineCount(%q) = %d, want %d", row.name, row.text, got, row.want)
			}
		}
	})

	t.Run("line_start", func(t *testing.T) {
		// Three lines: the start of line 3 is the sum of the lengths of the
		// two before it, which a table that does not accumulate gets wrong.
		m, id := newLineTableManager("ab\ncd\nef")
		if got := m.LineStart(id, 1); got != 0 {
			t.Errorf("LineStart(1) = %d, want 0", got)
		}
		if got := m.LineStart(id, 2); got != 3 {
			t.Errorf("LineStart(2) = %d, want 3", got)
		}
		if got := m.LineStart(id, 3); got != 6 {
			t.Errorf("LineStart(3) = %d, want 6", got)
		}
	})

	t.Run("empty_file", func(t *testing.T) {
		m, id := newLineTableManager("")
		if got := m.LineCount(id); got != 1 {
			t.Errorf("LineCount = %d, want 1", got)
		}
		if got, want := m.LineCol(id, 0), (LineCol{1, 1}); got != want {
			t.Errorf("LineCol(0) = %v, want %v", got, want)
		}
	})

	t.Run("out_of_range_panics", func(t *testing.T) {
		m, id := newLineTableManager("ab\ncd")
		// The last valid values first: one past them is what panics.
		if got := m.LineStart(id, 2); got != 3 {
			t.Errorf("LineStart(2) = %d, want 3", got)
		}
		if got, want := m.LineCol(id, 5), (LineCol{2, 3}); got != want {
			t.Errorf("LineCol(5) = %v, want %v", got, want)
		}
		mustPanic(t, "LineStart(3)", "base: line 3 out of range (file has 2 lines)", func() { m.LineStart(id, 3) })
		mustPanic(t, "LineStart(0)", "base: line 0 out of range (file has 2 lines)", func() { m.LineStart(id, 0) })
		mustPanic(t, "LineCol(6)", "base: offset 6 out of range (file has 5 bytes)", func() { m.LineCol(id, 6) })
	})
}
