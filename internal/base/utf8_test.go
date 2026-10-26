package base

import "testing"

func TestFirstInvalidUTF8(t *testing.T) {
	// check runs FirstInvalidUTF8 on in and compares both results.
	check := func(t *testing.T, in string, wantOffset int, wantFound bool) {
		t.Helper()
		offset, found := FirstInvalidUTF8([]byte(in))
		if offset != wantOffset || found != wantFound {
			t.Errorf("FirstInvalidUTF8(%q) = (%d, %v), want (%d, %v)", in, offset, found, wantOffset, wantFound)
		}
	}

	t.Run("ascii_valid", func(t *testing.T) {
		check(t, "fn main() {}", 0, false)
	})

	t.Run("multibyte_valid", func(t *testing.T) {
		check(t, "é€😀", 0, false)
	})

	t.Run("replacement_char_is_valid", func(t *testing.T) {
		// U+FFFD itself is valid. A decoder returns it for bad input too, so
		// checking only r == utf8.RuneError would wrongly flag this one.
		check(t, "\xEF\xBF\xBD", 0, false)
	})

	t.Run("invalid_ff", func(t *testing.T) {
		check(t, "\xff", 0, true)
	})

	t.Run("lone_continuation_byte", func(t *testing.T) {
		// 0x80 is the first byte value that is not ASCII, and a continuation
		// byte cannot start a sequence.
		check(t, "a\x80", 1, true)
	})

	t.Run("truncated_sequence", func(t *testing.T) {
		check(t, "a\xE2\x82", 1, true)
	})

	t.Run("offset_is_in_bytes", func(t *testing.T) {
		// 'é' takes two bytes, so the fault is at byte 2 but after one code point.
		check(t, "é\xff", 2, true)
	})

	t.Run("overlong_rejected", func(t *testing.T) {
		check(t, "\xC0\xAF", 0, true)
	})

	t.Run("surrogate_rejected", func(t *testing.T) {
		check(t, "\xED\xA0\x80", 0, true)
	})

	t.Run("above_max_rejected", func(t *testing.T) {
		check(t, "\xF4\x90\x80\x80", 0, true)
	})
}
