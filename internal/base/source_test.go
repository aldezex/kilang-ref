package base

import "testing"

func TestSourceManager(t *testing.T) {
	t.Run("add_returns_distinct_ids", func(t *testing.T) {
		var m SourceManager
		a := m.Add("a.ki", []byte("let a = 1"))
		b := m.Add("b.ki", []byte("let b = 2"))
		if a == b {
			t.Errorf("Add returned the same id %d for two files", a)
		}
		// Each id must also lead to its own file, not to the first or the last one.
		if got := m.DisplayPath(a); got != "a.ki" {
			t.Errorf("DisplayPath(a) = %q, want %q", got, "a.ki")
		}
		if got := m.DisplayPath(b); got != "b.ki" {
			t.Errorf("DisplayPath(b) = %q, want %q", got, "b.ki")
		}
	})

	t.Run("contents_round_trip", func(t *testing.T) {
		var m SourceManager
		// A decoy file first, with another length: Contents and Size must use
		// the file named by the id, not the first one.
		m.Add("decoy.ki", []byte("0123456789abcdef"))
		id := m.Add("main.ki", []byte("fn main() {}"))
		if got := string(m.Contents(id)); got != "fn main() {}" {
			t.Errorf("Contents = %q, want %q", got, "fn main() {}")
		}
		if got := m.Size(id); got != 12 {
			t.Errorf("Size = %d, want 12", got)
		}
	})

	t.Run("display_path_round_trip", func(t *testing.T) {
		var m SourceManager
		id := m.Add("src/deep/main.ki", []byte("fn main() {}"))
		if got := m.DisplayPath(id); got != "src/deep/main.ki" {
			t.Errorf("DisplayPath = %q, want %q", got, "src/deep/main.ki")
		}
	})

	t.Run("span_slices_contents", func(t *testing.T) {
		var m SourceManager
		// A decoy file first: Text must use the file named by the span.
		m.Add("decoy.ki", []byte("0123456789abcdef"))
		id := m.Add("main.ki", []byte("fn main() {}"))
		if got := m.Text(Span{File: id, Lo: 3, Hi: 7}); got != "main" {
			t.Errorf("Text = %q, want %q", got, "main")
		}
	})

	t.Run("contents_append_does_not_alias", func(t *testing.T) {
		var m SourceManager
		src := make([]byte, 5, 100)
		copy(src, "hello")
		id := m.Add("a.ki", src)
		// If Add kept the capacity of src (100), this append would find spare
		// room and write 'X' into the array that holds the file.
		_ = append(m.Contents(id), 'X')
		if src[:6][5] != 0 {
			t.Errorf("append to the result of Contents wrote %q into the file's array", src[:6][5])
		}
	})

	t.Run("text_of_empty_span_is_empty", func(t *testing.T) {
		var m SourceManager
		id := m.Add("main.ki", []byte("fn main() {}"))
		if got := m.Text(Span{File: id, Lo: 3, Hi: 3}); got != "" {
			t.Errorf("Text(3,3) = %q, want empty", got)
		}
		// The end of the file (offset 12 of 12 bytes) is a valid position.
		if got := m.Text(Span{File: id, Lo: 12, Hi: 12}); got != "" {
			t.Errorf("Text(12,12) = %q, want empty", got)
		}
	})

	t.Run("text_past_end_panics", func(t *testing.T) {
		var m SourceManager
		// The file has 5 bytes but the array behind it has room for 100, as a
		// buffer read from disk often does. A span past byte 5 must still panic.
		src := make([]byte, 5, 100)
		copy(src, "hello")
		id := m.Add("a.ki", src)
		// The last valid span first: one byte more is what panics.
		if got := m.Text(Span{File: id, Lo: 0, Hi: 5}); got != "hello" {
			t.Errorf("Text(0,5) = %q, want %q", got, "hello")
		}
		rows := []struct {
			name string
			sp   Span
		}{
			{"one_past_end", Span{File: id, Lo: 0, Hi: 6}},
			{"far_past_end", Span{File: id, Lo: 0, Hi: 50}},
		}
		for _, row := range rows {
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("%s: Text(%+v) did not panic", row.name, row.sp)
					}
				}()
				m.Text(row.sp)
			}()
		}
	})
}
