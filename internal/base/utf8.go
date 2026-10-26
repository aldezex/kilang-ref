package base

import "unicode/utf8"

// FirstInvalidUTF8 returns the byte offset of the first invalid sequence in b
// (an overlong form, a surrogate, a value above U+10FFFF, or a sequence cut
// short) and found == true. When the whole slice is valid UTF-8 it returns
// (0, false). U+FFFD itself (EF BF BD) is valid.
func FirstInvalidUTF8(b []byte) (offset int, found bool) {
	for i := 0; i < len(b); {
		if b[i] < utf8.RuneSelf {
			i++
			continue
		}
		// DecodeRune returns (RuneError, 1) for every kind of invalid input.
		// A real U+FFFD decodes to (RuneError, 3), so the size tells them apart.
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			return i, true
		}
		i += size
	}
	return 0, false
}
