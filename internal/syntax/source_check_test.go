package syntax_test

import (
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// checkSource runs CheckSource on a file made of src and returns the answer
// and the diagnostics it reported.
func checkSource(t *testing.T, src string) (ok bool, diags []diag.Diagnostic, id base.FileID) {
	t.Helper()
	var sm base.SourceManager
	id = sm.Add("test.ki", []byte(src))
	var sink diag.Sink
	ok = syntax.CheckSource(&sm, id, &sink)
	return ok, sink.All(), id
}

// wantOne fails the test unless diags holds exactly one error with the given
// code, message, primary span [lo, hi) and primary label. what names the case
// in the failure messages.
func wantOne(t *testing.T, what string, diags []diag.Diagnostic, id base.FileID, code diag.Code, msg string, lo, hi uint32, label string) {
	t.Helper()
	if len(diags) != 1 {
		t.Errorf("%s: got %d diagnostics, want exactly 1: %+v", what, len(diags), diags)
		return
	}
	d := diags[0]
	if d.Severity != diag.Error || d.Code != code || d.Message != msg {
		t.Errorf("%s: got %v %v %q, want error %v %q", what, d.Severity, d.Code, d.Message, code, msg)
	}
	if want := (base.Span{File: id, Lo: lo, Hi: hi}); d.Primary.Span != want {
		t.Errorf("%s: primary span %+v, want %+v", what, d.Primary.Span, want)
	}
	if d.Primary.Message != label {
		t.Errorf("%s: primary label %q, want %q", what, d.Primary.Message, label)
	}
}

func TestCheckSource(t *testing.T) {
	t.Run("clean_file", func(t *testing.T) {
		ok, diags, _ := checkSource(t, "fn main() {}\n// año, café, 😀\n")
		if !ok || len(diags) != 0 {
			t.Fatalf("got ok=%v and %d diagnostics, want true and none: %+v", ok, len(diags), diags)
		}
	})
	t.Run("invalid_utf8_E0101", func(t *testing.T) {
		ok, diags, id := checkSource(t, "let x = \xff")
		if ok {
			t.Error("CheckSource returned true for a file with an invalid byte")
		}
		wantOne(t, "invalid byte", diags, id, diag.E(101), "invalid UTF-8 byte 0xFF", 8, 9, "not valid UTF-8")
	})
	t.Run("control_byte_E0102", func(t *testing.T) {
		// The edges of the range (NUL, the most common control byte in a
		// binary renamed to .ki, and 0x1F), a byte in the middle (0x01), and
		// the vertical tab and the form feed, which are controls although
		// they look like white space.
		tests := []struct {
			name, src, msg, label string
		}{
			{"NUL", "a\x00", "control character U+0000 in source", "`\\u{0}`"},
			{"0x01", "a\x01", "control character U+0001 in source", "`\\u{1}`"},
			{"vertical tab", "a\x0b", "control character U+000B in source", "`\\u{b}`"},
			{"form feed", "a\x0c", "control character U+000C in source", "`\\u{c}`"},
			{"0x1F", "a\x1f", "control character U+001F in source", "`\\u{1f}`"},
		}
		for _, tc := range tests {
			ok, diags, id := checkSource(t, tc.src)
			if ok {
				t.Errorf("%s: CheckSource returned true for a file with a control byte", tc.name)
			}
			wantOne(t, tc.name, diags, id, diag.E(102), tc.msg, 1, 2, "not allowed in Ki source; write it as an escape such as "+tc.label)
		}
	})
	t.Run("tab_cr_lf_are_fine", func(t *testing.T) {
		ok, diags, _ := checkSource(t, "a\tb\r\nc")
		if !ok || len(diags) != 0 {
			t.Fatalf("got ok=%v and %d diagnostics, want true and none: %+v", ok, len(diags), diags)
		}
	})
	t.Run("delete_byte_E0102", func(t *testing.T) {
		ok, diags, id := checkSource(t, "a\x7f")
		if ok {
			t.Error("CheckSource returned true for a file with a DEL byte")
		}
		wantOne(t, "DEL", diags, id, diag.E(102), "control character U+007F in source", 1, 2,
			"not allowed in Ki source; write it as an escape such as `\\u{7f}`")
	})
	t.Run("reports_first_problem_only", func(t *testing.T) {
		// Whichever comes first wins, and the other is not reported at all.
		ok, diags, id := checkSource(t, "ab\x01cd\xffef\x02")
		if ok {
			t.Error("CheckSource returned true")
		}
		wantOne(t, "control first", diags, id, diag.E(102), "control character U+0001 in source", 2, 3,
			"not allowed in Ki source; write it as an escape such as `\\u{1}`")

		ok, diags, id = checkSource(t, "ab\xffcd\x01ef\xfe")
		if ok {
			t.Error("CheckSource returned true")
		}
		wantOne(t, "invalid first", diags, id, diag.E(101), "invalid UTF-8 byte 0xFF", 2, 3, "not valid UTF-8")
	})
}
