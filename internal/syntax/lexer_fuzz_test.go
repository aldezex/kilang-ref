package syntax_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// FuzzLex feeds the lexer arbitrary bytes and checks what every later pass
// relies on: Lex does not panic, the tokens end with Eof, every span lies within
// the file, and the tokens come in the order of the text (by where they start).
// It also checks DumpTokens, which finds the column of a token from the one of
// the token before it: its line for each token has to start with the position
// that SourceManager.LineCol gives.
//
// `go test` runs only the seeds. To search for more, run for a while with
// `go test ./internal/syntax -run=^$ -fuzz=FuzzLex -fuzztime=10s`; an input that
// breaks a property is saved under testdata/fuzz/FuzzLex and becomes a seed.
func FuzzLex(f *testing.F) {
	f.Add("fn main() {}")
	f.Add(`"a${x}b"`)
	f.Add("/* /* */")
	f.Add(`"\xff"`)
	// The byte 0xFF itself, which no valid file holds: Lex refuses the file.
	f.Add("\xff")
	// Line breaks, which are what the line rule works on: a CRLF between two
	// statements, inside parentheses, and inside a block.
	f.Add("a\r\nb")
	f.Add("f(a\n)")
	f.Add("{\na\n}")
	// Strings that never end and have tokens in their interpolation. The Error
	// token takes the place where the string starts and the tokens of the
	// expression stay after it, which makes this the code that moves tokens
	// around (lexer.unterminated). Then the line rule closes the brackets that
	// the expression left open, with and without a line break in between.
	f.Add("\"${0")
	f.Add("\"a${x}b${y\nz")
	f.Add("( \"${ ) [ x\nb")
	f.Fuzz(func(t *testing.T, src string) {
		var sm base.SourceManager
		id := sm.Add("fuzz.ki", []byte(src))
		in := base.NewInterner()
		tb := syntax.Lex(&sm, id, in, &diag.Sink{})

		if n := len(tb.Tokens); n == 0 || tb.Tokens[n-1].Kind != syntax.Eof {
			t.Fatalf("%q: the last token is not Eof: %+v", src, tb.Tokens)
		}
		prev := uint32(0) // where the previous token starts
		for i, tok := range tb.Tokens {
			if tok.Span.Lo > tok.Span.Hi || int(tok.Span.Hi) > len(src) {
				t.Fatalf("%q: token %d (%v) has span [%d,%d), not within [0,%d]", src, i, tok.Kind, tok.Span.Lo, tok.Span.Hi, len(src))
			}
			if tok.Span.Lo < prev {
				t.Fatalf("%q: token %d (%v) starts at %d, before the previous token, which starts at %d", src, i, tok.Kind, tok.Span.Lo, prev)
			}
			prev = tok.Span.Lo
		}

		// LineCol counts the code points from the start of the line for every
		// token, so on one very long line this check would cost the square of
		// its length, which is what DumpTokens avoids. Inputs over this size
		// skip it.
		const maxChecked = 1 << 14
		if len(src) > maxChecked {
			return
		}
		lines := strings.Split(strings.TrimSuffix(syntax.DumpTokens(&tb, &sm, in), "\n"), "\n")
		if len(lines) != len(tb.Tokens) {
			t.Fatalf("%q: DumpTokens wrote %d lines for %d tokens", src, len(lines), len(tb.Tokens))
		}
		for i, tok := range tb.Tokens {
			lc := sm.LineCol(id, tok.Span.Lo)
			// The line is the position and the kind, and the value in
			// parentheses after them for the kinds that have one.
			want := fmt.Sprintf("%d:%d %v", lc.Line, lc.Col, tok.Kind)
			if got := lines[i]; got != want && !strings.HasPrefix(got, want+"(") {
				t.Fatalf("%q: DumpTokens line %d is %q, want %q", src, i+1, got, want)
			}
		}
	})
}
