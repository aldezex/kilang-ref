// Package syntax turns the text of a source file into tokens (Lex) and the
// tokens into a syntax tree (Parse). Before lexing anything, CheckSource
// refuses a file whose bytes are not valid Ki source at all.
package syntax

import (
	"fmt"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// CheckSource looks for the two kinds of byte that no Ki file may contain:
// invalid UTF-8 (E0101) and an ASCII control character other than tab, line
// feed and carriage return (E0102: a byte below 0x20, or 0x7F). It reports
// the first problem of the file, whichever of the two comes first, to sink and
// returns false; it returns true, reporting nothing, when the file is clean.
//
// It stops at the first problem on purpose. A file that is not text (a binary
// renamed to .ki, say) has thousands of bad bytes, and a hundred identical
// errors would tell the user nothing more than the first.
func CheckSource(sm *base.SourceManager, id base.FileID, sink *diag.Sink) bool {
	src := sm.Contents(id)

	// A control byte is ASCII, so it never sits inside a multi-byte sequence
	// and a plain byte loop finds it. Only the part before the first invalid
	// sequence needs scanning: if the invalid byte comes first, it wins.
	end := len(src)
	bad, invalid := base.FirstInvalidUTF8(src)
	if invalid {
		end = bad
	}
	for i, b := range src[:end] {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' || b == 0x7F {
			span := base.Span{File: id, Lo: uint32(i), Hi: uint32(i) + 1}
			sink.Report(diag.NewError(diag.E(102),
				fmt.Sprintf("control character U+%04X in source", b),
				diag.Label{Span: span, Message: fmt.Sprintf("not allowed in Ki source; write it as an escape such as `\\u{%x}`", b)}))
			return false
		}
	}
	if invalid {
		span := base.Span{File: id, Lo: uint32(bad), Hi: uint32(bad) + 1}
		sink.Report(diag.NewError(diag.E(101),
			fmt.Sprintf("invalid UTF-8 byte 0x%02X", src[bad]),
			diag.Label{Span: span, Message: "not valid UTF-8"}))
		return false
	}
	return true
}
