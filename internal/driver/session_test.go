package driver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// newTestSession returns a Session whose stderr is captured in the returned
// builder, with one file called x.ki that holds src.
func newTestSession(src string) (*Session, base.FileID, *strings.Builder) {
	var errOut strings.Builder
	s := NewSession(strings.NewReader(""), &strings.Builder{}, &errOut)
	id := s.Sources.Add("x.ki", []byte(src))
	return s, id, &errOut
}

func TestReportAll(t *testing.T) {
	t.Run("sorted_by_position", func(t *testing.T) {
		// "let a = 1\n" is 10 bytes: offset 2 is the `t` of line 1 and
		// offset 10 is the `l` of line 2.
		const src = "let a = 1\nlet b = 2\n"
		report := func(s *Session, id base.FileID) {
			s.Sink.Report(diag.NewError(diag.E(301), "second in the file", diag.Label{Span: base.Span{File: id, Lo: 10, Hi: 13}, Message: "here"}))
			s.Sink.Report(diag.NewError(diag.E(302), "first in the file", diag.Label{Span: base.Span{File: id, Lo: 2, Hi: 3}, Message: "here"}))
		}

		s, id, errOut := newTestSession(src)
		report(s, id)
		s.ReportAll()
		want := "error[E0302]: first in the file\n" +
			" --> x.ki:1:3\n" +
			"  |\n" +
			"1 | let a = 1\n" +
			"  |   ^ here\n" +
			"\n" +
			"error[E0301]: second in the file\n" +
			" --> x.ki:2:1\n" +
			"  |\n" +
			"2 | let b = 2\n" +
			"  | ^^^ here\n"
		if errOut.String() != want {
			t.Errorf("human format:\ngot  %q\nwant %q", errOut.String(), want)
		}

		s, id, errOut = newTestSession(src)
		s.Format = FormatShort
		report(s, id)
		s.ReportAll()
		want = "x.ki:1:3: error[E0302]: first in the file\nx.ki:2:1: error[E0301]: second in the file\n"
		if errOut.String() != want {
			t.Errorf("short format:\ngot  %q\nwant %q", errOut.String(), want)
		}
	})
	t.Run("unknown_format_panics", func(t *testing.T) {
		s, id, _ := newTestSession("x\n")
		s.Format = DiagFormat(9)
		s.Sink.Report(diag.NewError(diag.E(101), "an error", diag.Label{Span: base.Span{File: id, Lo: 0, Hi: 1}}))
		defer func() {
			want := "driver: unknown diagnostic format 9"
			if got := recover(); got != want {
				t.Errorf("ReportAll panicked with %v, want %q", got, want)
			}
		}()
		s.ReportAll()
	})
	t.Run("truncation_note", func(t *testing.T) {
		const note = "note: too many errors; the first 100 are shown\n"
		fill := func(n int) (*Session, *strings.Builder) {
			s, id, errOut := newTestSession("x\n")
			for i := range n {
				s.Sink.Report(diag.NewError(diag.E(101), fmt.Sprintf("error %d", i), diag.Label{Span: base.Span{File: id, Lo: 0, Hi: 1}}))
			}
			return s, errOut
		}

		s, errOut := fill(101)
		s.ReportAll()
		if got := strings.Count(errOut.String(), "error[E0101]"); got != 100 {
			t.Errorf("101 errors: %d diagnostics shown, want 100", got)
		}
		// In the human format the note is not part of the last diagnostic, so
		// a blank line comes before it; nothing comes after it.
		if !strings.HasSuffix(errOut.String(), "\n\n"+note) || strings.Count(errOut.String(), "note: too many") != 1 {
			t.Errorf("101 errors: stderr should end with a blank line and the note, got the tail %q", tail(errOut.String(), 120))
		}

		s, errOut = fill(100)
		s.Format = FormatShort
		s.ReportAll()
		if got := strings.Count(errOut.String(), "error[E0101]"); got != 100 || strings.Contains(errOut.String(), "note:") {
			t.Errorf("100 errors: %d diagnostics shown and note=%v, want 100 and no note", got, strings.Contains(errOut.String(), "note:"))
		}

		s, errOut = fill(101)
		s.Format = FormatShort
		s.ReportAll()
		if !strings.HasSuffix(errOut.String(), "\n"+note) || strings.HasSuffix(errOut.String(), "\n\n"+note) {
			t.Errorf("101 errors, short format: the note should follow the last line directly, got the tail %q", tail(errOut.String(), 120))
		}
	})
}

// tail returns the last n bytes of s, to keep failure messages short.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
