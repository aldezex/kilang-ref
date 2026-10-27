package diag

import (
	"fmt"
	"slices"
	"testing"

	"github.com/aldezex/kilang/internal/base"
)

// at returns a diagnostic whose primary span starts at offset lo of file.
func at(code Code, file base.FileID, lo uint32, msg string) Diagnostic {
	return NewError(code, msg, Label{Span: base.Span{File: file, Lo: lo, Hi: lo + 1}})
}

// warningAt is like at but the diagnostic is a warning.
func warningAt(code Code, lo uint32, msg string) Diagnostic {
	d := at(code, 0, lo, msg)
	d.Severity = Warning
	return d
}

// messages returns the Message of each diagnostic, in order.
func messages(ds []Diagnostic) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Message
	}
	return out
}

func TestSink(t *testing.T) {
	t.Run("counts_errors_only", func(t *testing.T) {
		var s Sink
		if s.HasErrors() || s.ErrorCount() != 0 {
			t.Errorf("empty sink: HasErrors = %v, ErrorCount = %d, want false and 0", s.HasErrors(), s.ErrorCount())
		}
		s.Report(warningAt(W(702), 0, "w"))
		if s.HasErrors() || s.ErrorCount() != 0 {
			t.Errorf("only a warning: HasErrors = %v, ErrorCount = %d, want false and 0", s.HasErrors(), s.ErrorCount())
		}
		s.Report(at(E(101), 0, 3, "e"))
		if !s.HasErrors() || s.ErrorCount() != 1 {
			t.Errorf("a warning and an error: HasErrors = %v, ErrorCount = %d, want true and 1", s.HasErrors(), s.ErrorCount())
		}
		if got := len(s.All()); got != 2 {
			t.Errorf("len(All()) = %d, want 2", got)
		}
	})

	t.Run("count_by_code", func(t *testing.T) {
		var s Sink
		s.Report(at(E(101), 0, 0, "a"))
		s.Report(at(E(102), 0, 1, "b"))
		s.Report(at(E(101), 0, 2, "c"))
		s.Report(warningAt(W(702), 3, "d"))
		rows := []struct {
			name string
			code Code
			want int
		}{
			{"two errors", E(101), 2},
			{"one error", E(102), 1},
			{"one warning", W(702), 1},
			{"same number as the warning, other letter", E(702), 0},
			{"never reported", E(999), 0},
		}
		for _, row := range rows {
			if got := s.Count(row.code); got != row.want {
				t.Errorf("%s: Count(%v) = %d, want %d", row.name, row.code, got, row.want)
			}
		}
	})

	t.Run("sort_by_position_is_stable", func(t *testing.T) {
		var s Sink
		s.Report(at(E(101), 0, 9, "late"))
		s.Report(at(E(102), 0, 5, "same-1"))
		s.Report(at(E(103), 0, 2, "early"))
		s.Report(at(E(104), 0, 5, "same-2"))
		s.Report(at(E(105), 0, 5, "same-3"))
		s.SortByPosition()
		want := []string{"early", "same-1", "same-2", "same-3", "late"}
		if got := messages(s.All()); !slices.Equal(got, want) {
			t.Errorf("order after SortByPosition = %q, want %q", got, want)
		}

		// With many elements, an unstable sort (the standard library switches to
		// one for more than a dozen) would shuffle diagnostics at one offset.
		var many Sink
		var wantMany []string
		for off := uint32(0); off < 3; off++ {
			for i := 0; i < 30; i++ {
				wantMany = append(wantMany, fmt.Sprintf("o%d-%02d", off, i))
			}
		}
		for i := 0; i < 30; i++ {
			for off := uint32(2); off != ^uint32(0); off-- {
				many.Report(at(E(101), 0, off, fmt.Sprintf("o%d-%02d", off, i)))
			}
		}
		many.SortByPosition()
		if got := messages(many.All()); !slices.Equal(got, wantMany) {
			t.Errorf("order of 90 diagnostics at 3 offsets differs from position then report order:\n got %q\nwant %q", got, wantMany)
		}

		// The file is the first key: a later file never goes in front of an
		// earlier one, whatever the offsets are.
		var f Sink
		f.Report(at(E(101), 1, 0, "file1-lo0"))
		f.Report(at(E(102), 0, 7, "file0-lo7"))
		f.SortByPosition()
		wantFiles := []string{"file0-lo7", "file1-lo0"}
		if got := messages(f.All()); !slices.Equal(got, wantFiles) {
			t.Errorf("order of two files = %q, want %q", got, wantFiles)
		}
	})

	t.Run("stops_after_max_errors", func(t *testing.T) {
		var s Sink
		for i := 1; i <= MaxErrors+1; i++ {
			s.Report(at(E(101), 0, uint32(i), fmt.Sprintf("e%d", i)))
		}
		if got := s.ErrorCount(); got != MaxErrors {
			t.Errorf("ErrorCount() = %d, want %d", got, MaxErrors)
		}
		if !s.Truncated() {
			t.Errorf("Truncated() = false, want true after %d errors", MaxErrors+1)
		}
		all := s.All()
		if len(all) != MaxErrors {
			t.Fatalf("len(All()) = %d, want %d", len(all), MaxErrors)
		}
		// The 101st error is the one that is dropped, not an earlier one.
		if all[0].Message != "e1" || all[MaxErrors-1].Message != "e100" {
			t.Errorf("kept %q ... %q, want e1 ... e100", all[0].Message, all[MaxErrors-1].Message)
		}
	})

	t.Run("exactly_max_errors_not_truncated", func(t *testing.T) {
		var s Sink
		for i := 1; i <= MaxErrors; i++ {
			s.Report(at(E(101), 0, uint32(i), fmt.Sprintf("e%d", i)))
		}
		if got := s.ErrorCount(); got != MaxErrors {
			t.Errorf("ErrorCount() = %d, want %d", got, MaxErrors)
		}
		if s.Truncated() {
			t.Errorf("Truncated() = true after exactly %d errors, want false", MaxErrors)
		}
		// Nothing has been dropped yet, so a warning is still kept.
		s.Report(warningAt(W(702), 500, "w"))
		if got := len(s.All()); got != MaxErrors+1 {
			t.Errorf("len(All()) after a warning = %d, want %d", got, MaxErrors+1)
		}
		if s.Truncated() {
			t.Errorf("Truncated() = true after a warning, want false")
		}
	})

	t.Run("drops_warnings_after_truncation", func(t *testing.T) {
		var s Sink
		for i := 1; i <= MaxErrors+1; i++ {
			s.Report(at(E(101), 0, uint32(i), "e"))
		}
		s.Report(warningAt(W(702), 500, "w"))
		if got := len(s.All()); got != MaxErrors {
			t.Errorf("len(All()) = %d, want %d", got, MaxErrors)
		}
		if got := s.Count(W(702)); got != 0 {
			t.Errorf("Count(W0702) = %d, want 0", got)
		}
		if got := s.ErrorCount(); got != MaxErrors {
			t.Errorf("ErrorCount() = %d, want %d", got, MaxErrors)
		}
	})
}
