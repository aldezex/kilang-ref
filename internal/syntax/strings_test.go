package syntax_test

import (
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// escapesNote is the note that every E0106 carries. The `…` is U+2026.
const escapesNote = `valid escapes: \n \t \r \\ \' \" \0 \$ \u{…}`

// shape describes every token of tb with a short string: the kind and, for
// the kinds that carry a value, the value in parentheses. Strings are quoted,
// so an empty text shows as `StringHead("")`.
func shape(tb syntax.TokenBuffer, in *base.Interner) []string {
	out := make([]string, len(tb.Tokens))
	for i, tok := range tb.Tokens {
		s := tok.Kind.String()
		switch tok.Kind {
		case syntax.LowerIdent, syntax.UpperIdent:
			s += "(" + in.String(base.Symbol(tok.Payload)) + ")"
		case syntax.Int:
			s += fmt.Sprintf("(%d)", tb.Ints[tok.Payload])
		case syntax.Char:
			s += fmt.Sprintf("(U+%04X)", tok.Payload)
		case syntax.String, syntax.StringHead, syntax.StringMid, syntax.StringTail:
			s += "(" + strconv.Quote(tb.Strings[tok.Payload]) + ")"
		}
		out[i] = s
	}
	return out
}

// wantShape fails the test unless the tokens of tb have exactly the shape want.
func wantShape(t *testing.T, what string, tb syntax.TokenBuffer, in *base.Interner, want ...string) {
	t.Helper()
	if got := shape(tb, in); !slices.Equal(got, want) {
		t.Errorf("%s: tokens %q, want %q", what, got, want)
	}
}

// wantSpans fails the test unless the spans of the tokens of tb are exactly
// want, in order, as [lo, hi) pairs.
func wantSpans(t *testing.T, what string, tb syntax.TokenBuffer, want ...[2]uint32) {
	t.Helper()
	got := make([][2]uint32, len(tb.Tokens))
	for i, tok := range tb.Tokens {
		got[i] = [2]uint32{tok.Span.Lo, tok.Span.Hi}
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s: spans %v, want %v", what, got, want)
	}
}

// wantOrdered fails the test unless every span of tb lies within a file of n
// bytes and the starts never go back: the order of the tokens is the order of
// the text.
func wantOrdered(t *testing.T, what string, tb syntax.TokenBuffer, n int) {
	t.Helper()
	prev := uint32(0)
	for i, tok := range tb.Tokens {
		if tok.Span.Lo > tok.Span.Hi || tok.Span.Hi > uint32(n) || tok.Span.Lo < prev {
			t.Errorf("%s: token %d (%v) has span [%d,%d) after a token that starts at %d, in a file of %d bytes",
				what, i, tok.Kind, tok.Span.Lo, tok.Span.Hi, prev, n)
		}
		prev = tok.Span.Lo
	}
}

// wantNewlines fails the test unless the NewlineBefore flags of the tokens of
// tb are exactly want.
func wantNewlines(t *testing.T, what string, tb syntax.TokenBuffer, want ...bool) {
	t.Helper()
	got := make([]bool, len(tb.Tokens))
	for i, tok := range tb.Tokens {
		got[i] = tok.NewlineBefore
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s: NewlineBefore %v, want %v", what, got, want)
	}
}

// wantBadEscape fails the test unless diags holds exactly one E0106 whose
// message names the escape (the text the lexer consumed), whose primary span is
// [lo, hi) and which carries the note about the valid escapes.
func wantBadEscape(t *testing.T, what string, diags []diag.Diagnostic, id base.FileID, escape string, lo, hi uint32) {
	t.Helper()
	wantOne(t, what, diags, id, diag.E(106), "invalid escape sequence `"+escape+"`", lo, hi, "")
	if len(diags) == 1 && !slices.Equal(diags[0].Notes, []string{escapesNote}) {
		t.Errorf("%s: notes %q, want exactly %q", what, diags[0].Notes, escapesNote)
	}
}

// wantUnterminated fails the test unless diags holds exactly one E0110 whose
// label sits on the one-byte opening quote at offset quote.
func wantUnterminated(t *testing.T, what string, diags []diag.Diagnostic, id base.FileID, quote uint32) {
	t.Helper()
	wantOne(t, what, diags, id, diag.E(110), "unterminated string literal", quote, quote+1, "string starts here")
}

// wantBadChar fails the test unless diags holds exactly one E0108 whose
// primary span is [lo, hi).
func wantBadChar(t *testing.T, what string, diags []diag.Diagnostic, id base.FileID, lo, hi uint32) {
	t.Helper()
	wantOne(t, what, diags, id, diag.E(108), "invalid character literal", lo, hi,
		"a character literal holds exactly one character")
}

// charValues returns the payload of every Char token of tb, in order.
func charValues(tb syntax.TokenBuffer) []uint32 {
	var vals []uint32
	for _, tok := range tb.Tokens {
		if tok.Kind == syntax.Char {
			vals = append(vals, tok.Payload)
		}
	}
	return vals
}

// wantText fails the test unless token i of tb is a string token (String,
// StringHead, StringMid or StringTail) whose resolved text is want.
func wantText(t *testing.T, what string, tb syntax.TokenBuffer, i int, want string) {
	t.Helper()
	switch tb.Tokens[i].Kind {
	case syntax.String, syntax.StringHead, syntax.StringMid, syntax.StringTail:
		if p := tb.Tokens[i].Payload; int(p) >= len(tb.Strings) {
			t.Errorf("%s: payload %d is not an index of Strings, which has %d entries", what, p, len(tb.Strings))
		} else if tb.Strings[p] != want {
			t.Errorf("%s: text %q, want %q", what, tb.Strings[p], want)
		}
	default:
		t.Errorf("%s: token %d is %v, not a string token", what, i, tb.Tokens[i].Kind)
	}
}

func TestStrings(t *testing.T) {
	t.Run("char_literals", func(t *testing.T) {
		// A plain character, a simple escape and a Unicode escape, and the exact
		// span of each.
		src := `'a' '\n' '\u{1F600}'`
		tb, sink := lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.Char, syntax.Char, syntax.Char, syntax.Eof)
		if got, want := charValues(tb), []uint32{97, 10, 0x1F600}; !slices.Equal(got, want) {
			t.Errorf("%s: payloads %v, want %v", src, got, want)
		}
		wantSpans(t, src, tb, [2]uint32{0, 3}, [2]uint32{4, 8}, [2]uint32{9, 20}, [2]uint32{20, 20})

		// One character each: every escape, hexadecimal digits in both cases,
		// the edges of `\u{…}` (1 and 6 digits, the largest scalar, the two
		// sides of the surrogates) and characters that take more than one byte.
		rows := []struct {
			src  string
			want uint32
		}{
			{`'\n'`, '\n'}, {`'\t'`, '\t'}, {`'\r'`, '\r'}, {`'\\'`, '\\'}, {`'\''`, '\''},
			{`'\"'`, '"'}, {`'\0'`, 0}, {`'\$'`, '$'},
			{`'\u{41}'`, 0x41}, {`'\u{e9}'`, 0xE9}, {`'\u{aBcD}'`, 0xABCD}, {`'\u{0}'`, 0},
			{`'\u{000041}'`, 0x41}, {`'\u{10FFFF}'`, 0x10FFFF},
			{`'\u{D7FF}'`, 0xD7FF}, {`'\u{E000}'`, 0xE000},
			{`'"'`, '"'}, {`'$'`, '$'}, {`'é'`, 0xE9}, {`'€'`, 0x20AC}, {`'😀'`, 0x1F600},
		}
		for _, row := range rows {
			tb, sink := lexAll(t, row.src)
			wantNoDiags(t, row.src, sink)
			wantKindList(t, row.src, tb, syntax.Char, syntax.Eof)
			if got := charValues(tb); !slices.Equal(got, []uint32{row.want}) {
				t.Errorf("%s: payloads %v, want [%d]", row.src, got, row.want)
			}
			wantSpans(t, row.src, tb, [2]uint32{0, uint32(len(row.src))}, [2]uint32{uint32(len(row.src)), uint32(len(row.src))})
		}
	})
	t.Run("invalid_escape_E0106", func(t *testing.T) {
		// A character whose escape is wrong is still one Char, U+FFFD, and the
		// escape is the only mistake.
		src := `'\q'`
		tb, sink, id, _ := lexFile(t, src)
		wantBadEscape(t, src, sink.All(), id, `\q`, 1, 3)
		wantKindList(t, src, tb, syntax.Char, syntax.Eof)
		if got := charValues(tb); !slices.Equal(got, []uint32{0xFFFD}) {
			t.Errorf("%s: payloads %v, want [0xFFFD]", src, got)
		}
		wantSpans(t, src, tb, [2]uint32{0, 4}, [2]uint32{4, 4})

		// In a string the bad escape adds U+FFFD to the text and lexing goes on
		// right after the two characters that `\` and the next one make.
		rows := []struct {
			src, escape string
			lo, hi      uint32
			text        string
		}{
			{`"\q"`, `\q`, 1, 3, "\uFFFD"},
			{`"\qabc"`, `\q`, 1, 3, "\uFFFDabc"},
			{`"a\wb"`, `\w`, 2, 4, "a\uFFFDb"},
			{`"\x41"`, `\x`, 1, 3, "\uFFFD41"},
			// Two characters, not two bytes: `é` takes two bytes.
			{`"\é"`, `\é`, 1, 4, "\uFFFD"},
			// A carriage return that no line feed follows is not a line break,
			// so it is the character after the backslash. The message shows it
			// and a tab as <U+XXXX>, not as the raw control characters.
			{"\"a\\\rb\"", `\<U+000D>`, 2, 4, "a\uFFFDb"},
			{"\"\\\t\"", `\<U+0009>`, 1, 3, "\uFFFD"},
		}
		for _, row := range rows {
			tb, sink, id, _ := lexFile(t, row.src)
			wantBadEscape(t, row.src, sink.All(), id, row.escape, row.lo, row.hi)
			wantKindList(t, row.src, tb, syntax.String, syntax.Eof)
			wantText(t, row.src, tb, 0, row.text)
		}

		// The same carriage return as the last byte of the file: the lexer must
		// not look past the end to find out whether a line feed follows. The
		// string is then unterminated, a second mistake.
		src = "\"\\\r"
		tb, sink, id, _ = lexFile(t, src)
		if got := sink.All(); len(got) != 2 {
			t.Errorf("%q: got %d diagnostics, want 2: %+v", src, len(got), got)
		} else {
			wantBadEscape(t, src, got[:1], id, `\<U+000D>`, 1, 3)
			wantUnterminated(t, src, got[1:], id, 0)
		}
		wantKindList(t, src, tb, syntax.Error, syntax.Eof)
	})
	t.Run("bad_unicode_escape_forms_E0106", func(t *testing.T) {
		// Each form is one E0106 over exactly the text the lexer consumed, the
		// string stays closed, and the text holds U+FFFD for the escape and,
		// after it, whatever the lexer did not consume.
		rows := []struct {
			src, escape string
			lo, hi      uint32
			text        string
		}{
			{`"\u0041"`, `\u`, 1, 3, "\uFFFD0041"},
			{`"\u{}"`, `\u{}`, 1, 5, "\uFFFD"},
			{`"\u{1234567}"`, `\u{1234567}`, 1, 12, "\uFFFD"},
			{`"\u{D800}"`, `\u{D800}`, 1, 9, "\uFFFD"},
			{`"\u{12"`, `\u{12`, 1, 6, "\uFFFD"},
			// The edges of the rules: 7 digits even when they are zeros, the
			// first scalar past the last one, the last surrogate, and digits that
			// go on past the sixth with no `}`.
			{`"\u{0000041}"`, `\u{0000041}`, 1, 12, "\uFFFD"},
			{`"\u{110000}"`, `\u{110000}`, 1, 11, "\uFFFD"},
			{`"\u{DFFF}"`, `\u{DFFF}`, 1, 9, "\uFFFD"},
			{`"\u{1234567"`, `\u{1234567`, 1, 11, "\uFFFD"},
			{`"\u{12x"`, `\u{12`, 1, 6, "\uFFFDx"},
		}
		for _, row := range rows {
			tb, sink, id, _ := lexFile(t, row.src)
			wantBadEscape(t, row.src, sink.All(), id, row.escape, row.lo, row.hi)
			wantKindList(t, row.src, tb, syntax.String, syntax.Eof)
			wantText(t, row.src, tb, 0, row.text)
			wantSpans(t, row.src, tb, [2]uint32{0, uint32(len(row.src))}, [2]uint32{uint32(len(row.src)), uint32(len(row.src))})
		}

		// A `\u` cut short by the end of the file is a bad escape and then an
		// unterminated literal: the lexer must not read past the end.
		cut := []struct {
			src, escape string
			hi          uint32 // end of the escape; the literal ends at len(src)
			closing     diag.Code
			kinds       []syntax.TokenKind
		}{
			{`"\u`, `\u`, 3, diag.E(110), []syntax.TokenKind{syntax.Error, syntax.Eof}},
			{`"\u{`, `\u{`, 4, diag.E(110), []syntax.TokenKind{syntax.Error, syntax.Eof}},
			{`"\u{12`, `\u{12`, 6, diag.E(110), []syntax.TokenKind{syntax.Error, syntax.Eof}},
			{`'\u{12`, `\u{12`, 6, diag.E(108), []syntax.TokenKind{syntax.Char, syntax.Eof}},
		}
		for _, row := range cut {
			tb, sink, id, _ := lexFile(t, row.src)
			got := sink.All()
			if len(got) != 2 {
				t.Errorf("%s: got %d diagnostics, want 2: %+v", row.src, len(got), got)
				continue
			}
			wantBadEscape(t, row.src, got[:1], id, row.escape, 1, row.hi)
			if got[1].Code != row.closing {
				t.Errorf("%s: second diagnostic %v, want %v", row.src, got[1].Code, row.closing)
			}
			wantKindList(t, row.src, tb, row.kinds...)
		}
	})
	t.Run("empty_char_E0108", func(t *testing.T) {
		src := `''`
		tb, sink, id, _ := lexFile(t, src)
		wantBadChar(t, src, sink.All(), id, 0, 2)
		wantKindList(t, src, tb, syntax.Char, syntax.Eof)
		if got := charValues(tb); !slices.Equal(got, []uint32{0xFFFD}) {
			t.Errorf("%s: payloads %v, want [0xFFFD]", src, got)
		}
	})
	t.Run("two_chars_E0108", func(t *testing.T) {
		rows := []struct {
			src string
			hi  uint32
		}{
			{`'ab'`, 4},
			{`'aé'`, 5}, // two characters, five bytes with the quotes
			{`'\n\n'`, 6},
			{`'😀😀'`, 10},
		}
		for _, row := range rows {
			tb, sink, id, _ := lexFile(t, row.src)
			wantBadChar(t, row.src, sink.All(), id, 0, row.hi)
			wantKindList(t, row.src, tb, syntax.Char, syntax.Eof)
			if got := charValues(tb); !slices.Equal(got, []uint32{0xFFFD}) {
				t.Errorf("%s: payloads %v, want [0xFFFD]", row.src, got)
			}
		}
		// The literal ends at its closing quote, so lexing goes on right there.
		src := `'ab' x`
		tb, sink, _, in := lexFile(t, src)
		if n := len(sink.All()); n != 1 {
			t.Errorf("%s: got %d diagnostics, want 1", src, n)
		}
		wantShape(t, src, tb, in, "Char(U+FFFD)", "LowerIdent(x)", "Eof")
	})
	t.Run("unclosed_char_E0108", func(t *testing.T) {
		// A character that is not closed on its line goes to the end of the
		// line without the line break, which is where lexing goes on. It stays
		// a Char with U+FFFD.
		rows := []struct {
			src  string
			hi   uint32
			rest []string // the tokens after the Char, Eof included
		}{
			{"'a", 2, []string{"Eof"}},
			{"'", 1, []string{"Eof"}},
			{"'a b", 4, []string{"Eof"}},
			{"'a\nx", 2, []string{"LowerIdent(x)", "Eof"}},
			{"'a\r\nx", 2, []string{"LowerIdent(x)", "Eof"}},
			// A backslash at the end of the line is not an escape: the character
			// is unclosed and that is the only mistake.
			{"'\\", 2, []string{"Eof"}},
			{"'\\\nx", 2, []string{"LowerIdent(x)", "Eof"}},
			{"'\\\r\nx", 2, []string{"LowerIdent(x)", "Eof"}},
			// A carriage return that ends the file is not a line break (no line
			// feed follows), so it belongs to the contents. With a line feed
			// right after it, even when the two are the last bytes of the file,
			// the pair is a line break and not contents.
			{"'a\r", 3, []string{"Eof"}},
			{"'a\r\n", 2, []string{"Eof"}},
			{"'\\\r\n", 2, []string{"Eof"}},
		}
		for _, row := range rows {
			tb, sink, id, in := lexFile(t, row.src)
			wantBadChar(t, row.src, sink.All(), id, 0, row.hi)
			wantShape(t, row.src, tb, in, append([]string{"Char(U+FFFD)"}, row.rest...)...)
			if sp := tb.Tokens[0].Span; sp.Lo != 0 || sp.Hi != row.hi {
				t.Errorf("%q: Char has span [%d,%d), want [0,%d)", row.src, sp.Lo, sp.Hi, row.hi)
			}
		}
		// The line break after the character is still a line break.
		tb, _ := lexAll(t, "'a\nx")
		wantNewlines(t, "'a\\nx", tb, false, true, false)
	})
	t.Run("plain_string", func(t *testing.T) {
		src := `"a\tb"`
		tb, sink := lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.String, syntax.Eof)
		wantText(t, src, tb, 0, "a\tb")
		wantSpans(t, src, tb, [2]uint32{0, 6}, [2]uint32{6, 6})

		// The text of each string, with its escapes resolved: every escape in
		// order, the three forms of `\u{…}`, characters that take more than one
		// byte, the empty string and a lone `\r`, which is not a line break.
		rows := []struct {
			src, text string
		}{
			{`"\n\t\r\\\'\"\0\$"`, "\n\t\r\\'\"\x00$"},
			{`"\u{41}\u{e9}\u{1F600}"`, "A\u00e9\U0001F600"},
			{`"é😀"`, "é😀"},
			{`""`, ""},
			{"\"a\rb\"", "a\rb"},
			{`"'"`, "'"},
		}
		for _, row := range rows {
			tb, sink := lexAll(t, row.src)
			wantNoDiags(t, row.src, sink)
			wantKindList(t, row.src, tb, syntax.String, syntax.Eof)
			wantText(t, row.src, tb, 0, row.text)
			wantSpans(t, row.src, tb, [2]uint32{0, uint32(len(row.src))}, [2]uint32{uint32(len(row.src)), uint32(len(row.src))})
		}

		// Every string gets its own entry in Strings.
		src = `"a" "b" "a"`
		tb, sink = lexAll(t, src)
		wantNoDiags(t, src, sink)
		if want := []string{"a", "b", "a"}; !slices.Equal(tb.Strings, want) {
			t.Errorf("%s: Strings %q, want %q", src, tb.Strings, want)
		}
		for i, tok := range tb.Tokens[:min(3, len(tb.Tokens))] {
			if tok.Payload != uint32(i) {
				t.Errorf("%s: token %d has payload %d, want %d", src, i, tok.Payload, i)
			}
		}
	})
	t.Run("dollar_escape", func(t *testing.T) {
		src := `"\${x}"`
		tb, sink := lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.String, syntax.Eof)
		wantText(t, src, tb, 0, "${x}")

		src = `"\$"`
		tb, sink = lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.String, syntax.Eof)
		wantText(t, src, tb, 0, "$")
	})
	t.Run("lone_dollar_is_literal", func(t *testing.T) {
		src := `"a$b"`
		tb, sink := lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.String, syntax.Eof)
		wantText(t, src, tb, 0, "a$b")

		// A `$` that is not followed by `{`: before the quote, alone, doubled,
		// and before a space and a brace. Only `$` right before `{` opens an
		// interpolation, and the first of two dollars is still a dollar.
		rows := []struct{ src, text string }{
			{`"$"`, "$"},
			{`"a$"`, "a$"},
			{`"$$"`, "$$"},
			{`"$ {x}"`, "$ {x}"},
		}
		for _, row := range rows {
			tb, sink := lexAll(t, row.src)
			wantNoDiags(t, row.src, sink)
			wantKindList(t, row.src, tb, syntax.String, syntax.Eof)
			wantText(t, row.src, tb, 0, row.text)
		}
		src = `"$${x}"`
		tb, sink, _, in := lexFile(t, src)
		wantNoDiags(t, src, sink)
		wantShape(t, src, tb, in, `StringHead("$")`, "LowerIdent(x)", `StringTail("")`, "Eof")
	})
	t.Run("unterminated_string_E0110", func(t *testing.T) {
		// The simplest case: the label is on the opening quote, the token is an
		// Error from there to the end of the line, and lexing goes on at the
		// line break, which the next token records.
		src := "\"abc\nx"
		tb, sink, id, in := lexFile(t, src)
		wantUnterminated(t, src, sink.All(), id, 0)
		wantShape(t, src, tb, in, "Error", "LowerIdent(x)", "Eof")
		wantSpans(t, src, tb, [2]uint32{0, 4}, [2]uint32{5, 6}, [2]uint32{6, 6})
		wantNewlines(t, src, tb, false, true, false)

		// The end of the line is the end of the contents: a CRLF is one line
		// break, the end of the file counts too, and a lone `\r` is part of the
		// text.
		rows := []struct {
			src  string
			hi   uint32
			rest []string
		}{
			{"\"abc", 4, []string{"Eof"}},
			{"\"", 1, []string{"Eof"}},
			{"\"abc\n", 4, []string{"Eof"}},
			{"\"abc\r\nx", 4, []string{"LowerIdent(x)", "Eof"}},
			{"\"a\rb\nx", 4, []string{"LowerIdent(x)", "Eof"}},
			// A carriage return that ends the file is part of the contents too.
			{"\"abc\r", 5, []string{"Eof"}},
		}
		for _, row := range rows {
			tb, sink, id, in := lexFile(t, row.src)
			wantUnterminated(t, row.src, sink.All(), id, 0)
			wantShape(t, row.src, tb, in, append([]string{"Error"}, row.rest...)...)
			if sp := tb.Tokens[0].Span; sp.Lo != 0 || sp.Hi != row.hi {
				t.Errorf("%q: Error has span [%d,%d), want [0,%d)", row.src, sp.Lo, sp.Hi, row.hi)
			}
		}

		// Two lines, two unterminated strings, one error each.
		src = "\"abc\n\"def\nx"
		tb, sink, id, in = lexFile(t, src)
		if got := sink.All(); len(got) != 2 {
			t.Errorf("%q: got %d diagnostics, want 2: %+v", src, len(got), got)
		} else {
			wantUnterminated(t, src, got[:1], id, 0)
			wantUnterminated(t, src, got[1:], id, 5)
		}
		wantShape(t, src, tb, in, "Error", "Error", "LowerIdent(x)", "Eof")

		// The Error token keeps the line break before the string, and the
		// quote that opens the string, not the line, is where the label goes.
		src = "a\n  \"abc\nx"
		tb, sink, id, in = lexFile(t, src)
		wantUnterminated(t, src, sink.All(), id, 4)
		wantShape(t, src, tb, in, "LowerIdent(a)", "Error", "LowerIdent(x)", "Eof")
		wantNewlines(t, src, tb, false, true, true, false)

		// The Error token used up that line break: nothing else follows it.
		src = "a\n\"abc"
		tb, sink, id, in = lexFile(t, src)
		wantUnterminated(t, src, sink.All(), id, 2)
		wantShape(t, src, tb, in, "LowerIdent(a)", "Error", "Eof")
		wantNewlines(t, src, tb, false, true, false)
	})
	t.Run("backslash_before_crlf_E0110_only", func(t *testing.T) {
		// A backslash at the end of a line is not an escape, so the only
		// mistake is the unterminated string; its token includes the
		// backslash and not the line break.
		rows := []struct {
			src  string
			rest []string
		}{
			{"\"abc\\\r\nx", []string{"LowerIdent(x)", "Eof"}},
			{"\"abc\\\nx", []string{"LowerIdent(x)", "Eof"}},
			{"\"abc\\", []string{"Eof"}},
			// The CRLF is the last two bytes of the file.
			{"\"abc\\\r\n", []string{"Eof"}},
		}
		for _, row := range rows {
			tb, sink, id, in := lexFile(t, row.src)
			wantUnterminated(t, row.src, sink.All(), id, 0)
			wantShape(t, row.src, tb, in, append([]string{"Error"}, row.rest...)...)
			if sp := tb.Tokens[0].Span; sp.Lo != 0 || sp.Hi != 5 {
				t.Errorf("%q: Error has span [%d,%d), want [0,5)", row.src, sp.Lo, sp.Hi)
			}
		}
	})
	t.Run("interpolation_tokens", func(t *testing.T) {
		src := `"a${x}b${y + 1}c"`
		tb, sink, _, in := lexFile(t, src)
		wantNoDiags(t, src, sink)
		wantShape(t, src, tb, in, `StringHead("a")`, "LowerIdent(x)", `StringMid("b")`, "LowerIdent(y)", "Plus",
			"Int(1)", `StringTail("c")`, "Eof")
		// The head runs up to and including `${`; a middle part from the `}` up to
		// and including the next `${`; the tail from the `}` up to and including
		// the closing quote.
		wantSpans(t, src, tb, [2]uint32{0, 4}, [2]uint32{4, 5}, [2]uint32{5, 9}, [2]uint32{9, 10}, [2]uint32{11, 12},
			[2]uint32{13, 14}, [2]uint32{14, 17}, [2]uint32{17, 17})

		rows := []struct {
			src  string
			want []string
		}{
			{`"${x}"`, []string{`StringHead("")`, "LowerIdent(x)", `StringTail("")`, "Eof"}},
			{`"${a}${b}"`, []string{`StringHead("")`, "LowerIdent(a)", `StringMid("")`, "LowerIdent(b)", `StringTail("")`, "Eof"}},
			{`"é${x}😀"`, []string{`StringHead("é")`, "LowerIdent(x)", `StringTail("😀")`, "Eof"}},
			{`"a\n${x}\t\${"`, []string{`StringHead("a\n")`, "LowerIdent(x)", `StringTail("\t${")`, "Eof"}},
			{`"a${x}" "b"`, []string{`StringHead("a")`, "LowerIdent(x)", `StringTail("")`, `String("b")`, "Eof"}},
			{`"a${x}" }`, []string{`StringHead("a")`, "LowerIdent(x)", `StringTail("")`, "RBrace", "Eof"}},
		}
		for _, row := range rows {
			tb, sink, _, in := lexFile(t, row.src)
			wantNoDiags(t, row.src, sink)
			wantShape(t, row.src, tb, in, row.want...)
		}
	})
	t.Run("interpolation_with_braces", func(t *testing.T) {
		src := `"${ g({ y }) }"`
		tb, sink, _, in := lexFile(t, src)
		wantNoDiags(t, src, sink)
		wantShape(t, src, tb, in, `StringHead("")`, "LowerIdent(g)", "LParen", "LBrace", "LowerIdent(y)", "RBrace", "RParen",
			`StringTail("")`, "Eof")

		// Braces nest to any depth, an empty pair is a pair, a brace outside the
		// string does not close it, and a `}` with nothing open is just a token.
		rows := []struct {
			src  string
			want []string
		}{
			{`"${ {} }"`, []string{`StringHead("")`, "LBrace", "RBrace", `StringTail("")`, "Eof"}},
			{`"${ {{ }} }"`, []string{`StringHead("")`, "LBrace", "LBrace", "RBrace", "RBrace", `StringTail("")`, "Eof"}},
			{`"${ { } }x"`, []string{`StringHead("")`, "LBrace", "RBrace", `StringTail("x")`, "Eof"}},
			{`{ "${x}" }`, []string{"LBrace", `StringHead("")`, "LowerIdent(x)", `StringTail("")`, "RBrace", "Eof"}},
			{`}`, []string{"RBrace", "Eof"}},
			{`} "${x}" }`, []string{"RBrace", `StringHead("")`, "LowerIdent(x)", `StringTail("")`, "RBrace", "Eof"}},
			// Only the braces are counted: a ( or a [ that is open does not stop
			// the `}` from ending the interpolation, and is not an error of the
			// lexer, which the parser reports.
			{`"${ ( }"`, []string{`StringHead("")`, "LParen", `StringTail("")`, "Eof"}},
			{`"${ [ }"`, []string{`StringHead("")`, "LBracket", `StringTail("")`, "Eof"}},
			{`"${ f(x }"`, []string{`StringHead("")`, "LowerIdent(f)", "LParen", "LowerIdent(x)", `StringTail("")`, "Eof"}},
		}
		for _, row := range rows {
			tb, sink, _, in := lexFile(t, row.src)
			wantNoDiags(t, row.src, sink)
			wantShape(t, row.src, tb, in, row.want...)
		}
	})
	t.Run("string_in_interpolation", func(t *testing.T) {
		src := `"${ "in" }"`
		tb, sink, _, in := lexFile(t, src)
		wantNoDiags(t, src, sink)
		wantShape(t, src, tb, in, `StringHead("")`, `String("in")`, `StringTail("")`, "Eof")

		// An interpolated string inside an interpolation: each `}` closes the
		// interpolation of the string that is innermost at that point.
		src = `"${ "a${x}b" }c"`
		tb, sink, _, in = lexFile(t, src)
		wantNoDiags(t, src, sink)
		wantShape(t, src, tb, in, `StringHead("")`, `StringHead("a")`, "LowerIdent(x)", `StringTail("b")`,
			`StringTail("c")`, "Eof")

		src = `"${ "${ "${x}" }" }"`
		tb, sink, _, in = lexFile(t, src)
		wantNoDiags(t, src, sink)
		wantShape(t, src, tb, in, `StringHead("")`, `StringHead("")`, `StringHead("")`, "LowerIdent(x)", `StringTail("")`,
			`StringTail("")`, `StringTail("")`, "Eof")
	})
	t.Run("newline_inside_interpolation_E0110", func(t *testing.T) {
		// A line break right after the `${`: one E0110 on the opening quote; the
		// string and its StringHead become one Error token to the end of the
		// line, and the line break is the first thing lexing sees after it.
		src := "\"${\nx"
		tb, sink, id, in := lexFile(t, src)
		wantUnterminated(t, src, sink.All(), id, 0)
		wantShape(t, src, tb, in, "Error", "LowerIdent(x)", "Eof")
		wantSpans(t, src, tb, [2]uint32{0, 3}, [2]uint32{4, 5}, [2]uint32{5, 5})
		wantNewlines(t, src, tb, false, true, false)

		// The Error token takes the place of the string's first token, so the
		// tokens of the interpolation that were already lexed stay after it, and
		// the text parts of the strings that were open disappear. Every row has
		// exactly one E0110, on the quote at offset 0.
		rows := []struct {
			src  string
			hi   uint32 // end of the Error token
			want []string
		}{
			{"\"${ y\nx", 5, []string{"Error", "LowerIdent(y)", "LowerIdent(x)", "Eof"}},
			{"\"${\r\nx", 3, []string{"Error", "LowerIdent(x)", "Eof"}},
			// Several breaks in a row: the Error ends at the first.
			{"\"${\n\n  \nx", 3, []string{"Error", "LowerIdent(x)", "Eof"}},
			// The end of the file ends the line too.
			{"\"${x", 4, []string{"Error", "LowerIdent(x)", "Eof"}},
			{"\"${", 3, []string{"Error", "Eof"}},
			// A line break that is the last thing in the file is a line break: the
			// Error ends before it, as it does before any other.
			{"\"${x\n", 4, []string{"Error", "LowerIdent(x)", "Eof"}},
			{"\"${\r\n", 3, []string{"Error", "Eof"}},
			// Strings inside strings: one error, on the outermost quote.
			{"\"${ \"a${ \nx", 9, []string{"Error", "LowerIdent(x)", "Eof"}},
			{"\"${ \"abc\nx", 8, []string{"Error", "LowerIdent(x)", "Eof"}},
			// The break comes after the interpolation, in the text of the string.
			{"\"a${x}b\ny", 7, []string{"Error", "LowerIdent(x)", "LowerIdent(y)", "Eof"}},
			{"\"a${x}b${y}c\nz", 12, []string{"Error", "LowerIdent(x)", "LowerIdent(y)", "LowerIdent(z)", "Eof"}},
			// A break inside a comment inside the interpolation is a break too;
			// the Error ends at the first one and lexing goes on after the comment.
			{"\"${ /* a\nb */ x", 8, []string{"Error", "LowerIdent(x)", "Eof"}},
			// A string that is complete inside the expression stays as it is.
			{"\"${ \"a${x}b\" \ny", 13, []string{"Error", `StringHead("a")`, "LowerIdent(x)", `StringTail("b")`,
				"LowerIdent(y)", "Eof"}},
			{"\"${ \"a\" \ny", 8, []string{"Error", `String("a")`, "LowerIdent(y)", "Eof"}},
			{"\"${ x // c\ny", 10, []string{"Error", "LowerIdent(x)", "LowerIdent(y)", "Eof"}},
		}
		for _, row := range rows {
			tb, sink, id, in := lexFile(t, row.src)
			wantUnterminated(t, row.src, sink.All(), id, 0)
			wantShape(t, row.src, tb, in, row.want...)
			wantOrdered(t, row.src, tb, len(row.src))
			if sp := tb.Tokens[0].Span; sp.Lo != 0 || sp.Hi != row.hi {
				t.Errorf("%q: Error has span [%d,%d), want [0,%d)", row.src, sp.Lo, sp.Hi, row.hi)
			}
		}

		// A comment and a character that are open inside the interpolation have
		// their own mistake, reported first; the string, which then ends with the
		// line or the file, is a second one.
		for _, row := range []struct {
			src            string
			code           diag.Code // of the first diagnostic
			message, label string    // of the first diagnostic
			lo, hi         uint32    // span of the first diagnostic
			tokens         []string
		}{
			{"\"${ x /* c", diag.E(103), "unterminated block comment", "comment starts here", 6, 8,
				[]string{"Error", "LowerIdent(x)", "Eof"}},
			{"\"${ 'a", diag.E(108), "invalid character literal", "a character literal holds exactly one character", 4, 6,
				[]string{"Error", "Char(U+FFFD)", "Eof"}},
		} {
			tb, sink, id, in := lexFile(t, row.src)
			if got := sink.All(); len(got) != 2 {
				t.Errorf("%q: got %d diagnostics, want 2: %+v", row.src, len(got), got)
			} else {
				wantOne(t, row.src, got[:1], id, row.code, row.message, row.lo, row.hi, row.label)
				wantUnterminated(t, row.src, got[1:], id, 0)
			}
			wantShape(t, row.src, tb, in, row.tokens...)
			wantOrdered(t, row.src, tb, len(row.src))
		}

		// The Error token keeps the line break that came before the string.
		src = "a\n\"${\nx"
		tb, sink, id, in = lexFile(t, src)
		wantUnterminated(t, src, sink.All(), id, 2)
		wantShape(t, src, tb, in, "LowerIdent(a)", "Error", "LowerIdent(x)", "Eof")
		wantNewlines(t, src, tb, false, true, true, false)

		// Nothing of the string is left open afterwards: a `}` on the next line
		// is a plain brace and not the end of the interpolation. The obvious
		// way to write this case, `"${\n}"`, would be two unterminated strings:
		// the quote after that `}` opens another one.
		src = "\"${\n}x"
		tb, sink, id, in = lexFile(t, src)
		wantUnterminated(t, src, sink.All(), id, 0)
		wantShape(t, src, tb, in, "Error", "RBrace", "LowerIdent(x)", "Eof")

		src = "\"${\n}\""
		tb, sink, id, in = lexFile(t, src)
		if got := sink.All(); len(got) != 2 {
			t.Errorf("%q: got %d diagnostics, want 2: %+v", src, len(got), got)
		} else {
			wantUnterminated(t, src, got[:1], id, 0)
			wantUnterminated(t, src, got[1:], id, 5)
		}
		wantShape(t, src, tb, in, "Error", "RBrace", "Error", "Eof")
		wantSpans(t, src, tb, [2]uint32{0, 3}, [2]uint32{4, 5}, [2]uint32{5, 6}, [2]uint32{6, 6})
	})
}
