package syntax_test

import (
	"slices"
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
			name, src, msg string
		}{
			{"NUL", "a\x00", "control character U+0000 in source"},
			{"0x01", "a\x01", "control character U+0001 in source"},
			{"vertical tab", "a\x0b", "control character U+000B in source"},
			{"form feed", "a\x0c", "control character U+000C in source"},
			{"0x1F", "a\x1f", "control character U+001F in source"},
		}
		for _, tc := range tests {
			ok, diags, id := checkSource(t, tc.src)
			if ok {
				t.Errorf("%s: CheckSource returned true for a file with a control byte", tc.name)
			}
			wantOne(t, tc.name, diags, id, diag.E(102), tc.msg, 1, 2, "not allowed in Ki source")
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
		wantOne(t, "DEL", diags, id, diag.E(102), "control character U+007F in source", 1, 2, "not allowed in Ki source")
	})
	t.Run("reports_first_problem_only", func(t *testing.T) {
		// Whichever comes first wins, and the other is not reported at all.
		ok, diags, id := checkSource(t, "ab\x01cd\xffef\x02")
		if ok {
			t.Error("CheckSource returned true")
		}
		wantOne(t, "control first", diags, id, diag.E(102), "control character U+0001 in source", 2, 3, "not allowed in Ki source")

		ok, diags, id = checkSource(t, "ab\xffcd\x01ef\xfe")
		if ok {
			t.Error("CheckSource returned true")
		}
		wantOne(t, "invalid first", diags, id, diag.E(101), "invalid UTF-8 byte 0xFF", 2, 3, "not valid UTF-8")
	})
	t.Run("e0101_note_and_help", func(t *testing.T) {
		// The note says what a Ki file is; the help is the two things the user
		// can do. There is no fix-it: deleting one byte of a multi-byte
		// sequence patches half a character, and re-encoding the file is not
		// an edit of its text.
		_, diags, _ := checkSource(t, "let x = \xff")
		if len(diags) != 1 {
			t.Fatalf("got %d diagnostics, want exactly 1: %+v", len(diags), diags)
		}
		d := diags[0]
		if want := []string{"Ki source files are UTF-8"}; !slices.Equal(d.Notes, want) {
			t.Errorf("notes %q, want %q", d.Notes, want)
		}
		if want := "save the file as UTF-8, or delete the byte"; d.Help != want {
			t.Errorf("help %q, want %q", d.Help, want)
		}
		if len(d.Fixits) != 0 || len(d.Secondary) != 0 {
			t.Errorf("fixits %+v and secondary labels %+v, want none", d.Fixits, d.Secondary)
		}
	})
	t.Run("e0102_help_and_fixit", func(t *testing.T) {
		// The help names the escape with the value of the byte in hexadecimal,
		// and the one fix-it deletes exactly that byte, in the right file.
		rows := []struct {
			name, src, help string
			at              uint32
		}{
			{"0x01", "a\x01b", "write it as the escape `\\u{1}` inside a string, or delete it", 1},
			{"0x1F", "a\x1f", "write it as the escape `\\u{1f}` inside a string, or delete it", 1},
			{"DEL", "ab\x7fc", "write it as the escape `\\u{7f}` inside a string, or delete it", 2},
		}
		for _, row := range rows {
			ok, diags, id := checkSource(t, row.src)
			if ok || len(diags) != 1 {
				t.Errorf("%s: ok=%v and %d diagnostics, want false and exactly 1", row.name, ok, len(diags))
				continue
			}
			d := diags[0]
			if want := []string{"Ki allows only `\\t`, `\\n` and `\\r` as raw control characters; the others are invisible in an editor"}; !slices.Equal(d.Notes, want) {
				t.Errorf("%s: notes %q, want %q", row.name, d.Notes, want)
			}
			if d.Help != row.help {
				t.Errorf("%s: help %q, want %q", row.name, d.Help, row.help)
			}
			if want := []diag.FixIt{diag.Delete(base.Span{File: id, Lo: row.at, Hi: row.at + 1})}; !slices.Equal(d.Fixits, want) {
				t.Errorf("%s: fixits %+v, want %+v", row.name, d.Fixits, want)
			}
		}
	})
}
