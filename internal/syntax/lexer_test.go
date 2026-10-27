package syntax_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// lexAll lexes a file made of src and returns the tokens and the diagnostics.
func lexAll(t *testing.T, src string) (syntax.TokenBuffer, *diag.Sink) {
	t.Helper()
	tb, sink, _, _ := lexFile(t, src)
	return tb, sink
}

// lexFile is lexAll that also returns the id of the file and the interner that
// holds the symbols of the identifiers. The file is the second one of its
// SourceManager, after an empty one, so its id is 1 and not 0: a pass that
// builds a span and forgets the file leaves the zero value, which would be the
// right id if the file were the first. The id is checked, so that deleting the
// padding fails every test that goes through lexFile instead of letting those
// that compare a File with id pass for the wrong reason.
func lexFile(t *testing.T, src string) (syntax.TokenBuffer, *diag.Sink, base.FileID, *base.Interner) {
	t.Helper()
	var sm base.SourceManager
	sm.Add("pad.ki", nil)
	id := sm.Add("test.ki", []byte(src))
	if id != 1 {
		t.Fatalf("test.ki is file %d, want 1: pad.ki must come first, or a span that lost its File would pass for the right one", id)
	}
	in := base.NewInterner()
	sink := &diag.Sink{}
	return syntax.Lex(&sm, id, in, sink), sink, id, in
}

// lexKinds returns the kind of every token of src, the final Eof included.
func lexKinds(t *testing.T, src string) []syntax.TokenKind {
	t.Helper()
	tb, _ := lexAll(t, src)
	kinds := make([]syntax.TokenKind, len(tb.Tokens))
	for i, tok := range tb.Tokens {
		kinds[i] = tok.Kind
	}
	return kinds
}

// dumpTokens lexes src and returns what DumpTokens writes for its tokens.
func dumpTokens(t *testing.T, src string) string {
	t.Helper()
	var sm base.SourceManager
	id := sm.Add("test.ki", []byte(src))
	in := base.NewInterner()
	tb := syntax.Lex(&sm, id, in, &diag.Sink{})
	return syntax.DumpTokens(&tb, &sm, in)
}

// wantKinds fails the test unless the kinds of the tokens of src are exactly
// want. what names the case in the failure message.
func wantKinds(t *testing.T, what, src string, want ...syntax.TokenKind) {
	t.Helper()
	if got := lexKinds(t, src); !slices.Equal(got, want) {
		t.Errorf("%s: lexing %q gave %v, want %v", what, src, got, want)
	}
}

func TestLexer(t *testing.T) {
	t.Run("empty_file_is_eof", func(t *testing.T) {
		wantKinds(t, "empty", "", syntax.Eof)
	})
	t.Run("punctuation_maximal_munch", func(t *testing.T) {
		wantKinds(t, "maximal munch", "|>=>..->", syntax.Pipe, syntax.FatArrow, syntax.DotDot, syntax.Arrow, syntax.Eof)
		// Every punctuation token once, one-character ones first and
		// two-character ones after, so a swapped or missing case shows up.
		const oneChar = "( ) [ ] { } , ; : . = < > + - * / % ! | ? #"
		wantKinds(t, "one character", oneChar,
			syntax.LParen, syntax.RParen, syntax.LBracket, syntax.RBracket, syntax.LBrace, syntax.RBrace,
			syntax.Comma, syntax.Semi, syntax.Colon, syntax.Dot, syntax.Eq, syntax.Lt, syntax.Gt,
			syntax.Plus, syntax.Minus, syntax.Star, syntax.Slash, syntax.Percent, syntax.Bang, syntax.Bar,
			syntax.Question, syntax.Hash, syntax.Eof)
		// wantFixedWidth fails the test unless every token of src is width bytes
		// long. src holds the tokens separated by one space each, so token i
		// starts at byte i*(width+1), and the Eof is empty and sits at the end.
		wantFixedWidth := func(what, src string, width int) {
			t.Helper()
			tb, _ := lexAll(t, src)
			var got, want [][2]uint32
			for _, tok := range tb.Tokens {
				got = append(got, [2]uint32{tok.Span.Lo, tok.Span.Hi})
			}
			for lo := 0; lo < len(src); lo += width + 1 {
				want = append(want, [2]uint32{uint32(lo), uint32(lo + width)})
			}
			want = append(want, [2]uint32{uint32(len(src)), uint32(len(src))})
			if !slices.Equal(got, want) {
				t.Errorf("%s: spans %v, want %v", what, got, want)
			}
		}

		// Each of them is one byte long: token i starts at byte 2i, the spaces
		// being the odd bytes, and the Eof is empty and sits at the end. A sign
		// that took two bytes would swallow the space after it, and the last
		// one, with nothing after it, would leave the Eof past the end of the file.
		wantFixedWidth("one character", oneChar, 1)

		// The two-character ones, the same way: token i starts at byte 3i. The
		// kinds cannot tell a sign that took three bytes, because the space it
		// swallows separates nothing and the next token is found all the same;
		// the spans can.
		const twoChar = ".. => -> |> == != <= >= && ||"
		wantKinds(t, "two characters", twoChar,
			syntax.DotDot, syntax.FatArrow, syntax.Arrow, syntax.Pipe, syntax.EqEq, syntax.NotEq,
			syntax.Le, syntax.Ge, syntax.AndAnd, syntax.OrOr, syntax.Eof)
		wantFixedWidth("two characters", twoChar, 2)
	})
	t.Run("keywords_and_identifiers", func(t *testing.T) {
		tb, sink, _, in := lexFile(t, "fn fib Shape _x _X _1 _")
		want := []syntax.TokenKind{syntax.KwFn, syntax.LowerIdent, syntax.UpperIdent, syntax.LowerIdent,
			syntax.LowerIdent, syntax.LowerIdent, syntax.Underscore, syntax.Eof}
		var got []syntax.TokenKind
		for _, tok := range tb.Tokens {
			got = append(got, tok.Kind)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("kinds %v, want %v", got, want)
		}
		if n := len(sink.All()); n != 0 {
			t.Errorf("got %d diagnostics, want none: %+v", n, sink.All())
		}
		// Token 1 is fib, 2 is Shape and 3 to 5 are _x, _X and _1.
		for _, row := range []struct {
			index int
			name  string
		}{{1, "fib"}, {2, "Shape"}, {3, "_x"}, {4, "_X"}, {5, "_1"}} {
			if text := in.String(base.Symbol(tb.Tokens[row.index].Payload)); text != row.name {
				t.Errorf("token %d: payload interns %q, want %q", row.index, text, row.name)
			}
		}

		// Every keyword, and words that only start like one or contain one.
		wantKinds(t, "all keywords",
			"fn let var type trait impl effect for in while break continue return if else match handle with uses spawn lazy select import pub true false",
			syntax.KwFn, syntax.KwLet, syntax.KwVar, syntax.KwType, syntax.KwTrait, syntax.KwImpl, syntax.KwEffect,
			syntax.KwFor, syntax.KwIn, syntax.KwWhile, syntax.KwBreak, syntax.KwContinue, syntax.KwReturn,
			syntax.KwIf, syntax.KwElse, syntax.KwMatch, syntax.KwHandle, syntax.KwWith, syntax.KwUses,
			syntax.KwSpawn, syntax.KwLazy, syntax.KwSelect, syntax.KwImport, syntax.KwPub, syntax.KwTrue,
			syntax.KwFalse, syntax.Eof)
		wantKinds(t, "near keywords", "fnx f For IF if2 _fn", syntax.LowerIdent, syntax.LowerIdent,
			syntax.UpperIdent, syntax.UpperIdent, syntax.LowerIdent, syntax.LowerIdent, syntax.Eof)

		// A tab and a carriage return separate tokens as a space does. If either
		// were not whitespace, it would be an unexpected character.
		wantKinds(t, "tab and carriage return", "a\tb\rc", syntax.LowerIdent, syntax.LowerIdent, syntax.LowerIdent, syntax.Eof)

		// The first and last digit and the last letters of both cases belong to
		// a name. A range that stopped one character short would split AZ into a
		// name and an unexpected character, and Z9 and z9 into a name and a
		// number, and one that started a character late would do the same to a0.
		wantKinds(t, "end of the ranges", "az AZ a0 Z9 z9", syntax.LowerIdent, syntax.UpperIdent, syntax.LowerIdent, syntax.UpperIdent, syntax.LowerIdent, syntax.Eof)

		// The character next to each range, outside it, does not belong to a
		// name: ` comes just before a, @ just before A, / just before 0 and :
		// just after 9. A range that reached one character further would take
		// it into the name, and a`b, a@b, a/b or a:b would be one LowerIdent.
		// The ` and the @ are no token of Ki, so each is an Error between the
		// two names.
		wantKinds(t, "just outside the ranges", "a`b a@b a/b a:b",
			syntax.LowerIdent, syntax.Error, syntax.LowerIdent,
			syntax.LowerIdent, syntax.Error, syntax.LowerIdent,
			syntax.LowerIdent, syntax.Slash, syntax.LowerIdent,
			syntax.LowerIdent, syntax.Colon, syntax.LowerIdent, syntax.Eof)
	})
	t.Run("contextual_words_are_identifiers", func(t *testing.T) {
		wantKinds(t, "contextual words", "from as only", syntax.LowerIdent, syntax.LowerIdent, syntax.LowerIdent, syntax.Eof)
		wantKinds(t, "the other three", "recv send default", syntax.LowerIdent, syntax.LowerIdent, syntax.LowerIdent, syntax.Eof)
	})
	t.Run("line_comment_skipped", func(t *testing.T) {
		tb, sink := lexAll(t, "a // b\nc")
		if len(tb.Tokens) != 4 || tb.Tokens[0].Kind != syntax.LowerIdent || tb.Tokens[1].Kind != syntax.Newline ||
			tb.Tokens[2].Kind != syntax.LowerIdent || tb.Tokens[3].Kind != syntax.Eof {
			t.Fatalf("got %+v, want LowerIdent Newline LowerIdent Eof", tb.Tokens)
		}
		for i, want := range []bool{false, false, true, false} {
			if got := tb.Tokens[i].NewlineBefore; got != want {
				t.Errorf("token %d: NewlineBefore = %v, want %v", i, got, want)
			}
		}
		if n := len(sink.All()); n != 0 {
			t.Errorf("got %d diagnostics, want none: %+v", n, sink.All())
		}
		// A comment that ends the file without a line break.
		wantKinds(t, "comment at end of file", "a // b", syntax.LowerIdent, syntax.Eof)
	})
	t.Run("nested_block_comment", func(t *testing.T) {
		tb, sink := lexAll(t, "/* a /* b */ c */ x")
		if len(tb.Tokens) != 2 || tb.Tokens[0].Kind != syntax.LowerIdent || tb.Tokens[1].Kind != syntax.Eof {
			t.Fatalf("got %+v, want LowerIdent Eof", tb.Tokens)
		}
		if sp := tb.Tokens[0].Span; sp.Lo != 18 || sp.Hi != 19 {
			t.Errorf("x has span [%d,%d), want [18,19)", sp.Lo, sp.Hi)
		}
		if n := len(sink.All()); n != 0 {
			t.Errorf("got %d diagnostics, want none: %+v", n, sink.All())
		}

		// A comment with nothing inside closes with its own `*/`: the opener
		// takes two bytes, so the star of `/*` is not the star of `*/`. Inside
		// another comment the same holds for the nested opener.
		for _, src := range []string{"/**/ x", "/* /**/ */ x"} {
			wantKinds(t, "empty comment", src, syntax.LowerIdent, syntax.Eof)
			if _, sink := lexAll(t, src); len(sink.All()) != 0 {
				t.Errorf("%q: got %d diagnostics, want none: %+v", src, len(sink.All()), sink.All())
			}
		}

		// A line break inside a comment is still a line break: the token after
		// the comment records it. (b starts at offset 12; the tokens around it
		// depend on the line rule, so the test looks for it by its span.)
		tb, sink = lexAll(t, "a /* x\ny */ b")
		if n := len(sink.All()); n != 0 {
			t.Errorf("a /* x\\ny */ b: got %d diagnostics, want none: %+v", n, sink.All())
		}
		found := false
		for _, tok := range tb.Tokens {
			if tok.Span.Lo == 12 {
				found = true
				if tok.Kind != syntax.LowerIdent || !tok.NewlineBefore {
					t.Errorf("a /* x\\ny */ b: the token at 12 is %v with NewlineBefore = %v, want LowerIdent with true", tok.Kind, tok.NewlineBefore)
				}
			}
		}
		if !found {
			t.Errorf("a /* x\\ny */ b: no token starts at 12: %+v", tb.Tokens)
		}
	})
	t.Run("unterminated_block_comment_E0103", func(t *testing.T) {
		// The label sits on the outermost `/*`, wherever it is, and `/*/`
		// opens a comment instead of closing it with its own star, also inside
		// another comment: there it opens a second level, and the `*/` after it
		// closes only that one, so the file still ends inside the first.
		for _, row := range []struct {
			src       string
			lo, hi    uint32
			wantIdent bool // the file starts with the identifier x
		}{
			{"/* a /* b */", 0, 2, false},
			{"x /* a /* b */", 2, 4, true},
			{"/*/", 0, 2, false},
			{"/* /*/ */ x", 0, 2, false},
		} {
			tb, sink, id, _ := lexFile(t, row.src)
			wantOne(t, row.src, sink.All(), id, diag.E(103), "unterminated block comment", row.lo, row.hi, "comment starts here")
			if row.wantIdent {
				if len(tb.Tokens) != 2 || tb.Tokens[0].Kind != syntax.LowerIdent || tb.Tokens[1].Kind != syntax.Eof {
					t.Errorf("%q: tokens %+v, want LowerIdent Eof", row.src, tb.Tokens)
				}
			} else if len(tb.Tokens) != 1 || tb.Tokens[0].Kind != syntax.Eof {
				t.Errorf("%q: tokens %+v, want only Eof", row.src, tb.Tokens)
			}
		}
	})
	t.Run("unexpected_char_E0104", func(t *testing.T) {
		tb, sink, id, _ := lexFile(t, "$")
		wantOne(t, "$", sink.All(), id, diag.E(104), "unexpected character `$`", 0, 1, "not valid in Ki source")
		if len(tb.Tokens) != 2 || tb.Tokens[0].Kind != syntax.Error || tb.Tokens[1].Kind != syntax.Eof {
			t.Errorf("tokens %+v, want Error Eof", tb.Tokens)
		}

		// Lexing goes on after the error, and a lone `&` is not `&&`.
		tb, sink, id, _ = lexFile(t, "a $ b")
		wantOne(t, "a $ b", sink.All(), id, diag.E(104), "unexpected character `$`", 2, 3, "not valid in Ki source")
		// The span check below reads token 1. Fatalf, not Errorf, so that a Lex
		// that returns too few tokens ends the test here with this message
		// instead of with an index out of range.
		if len(tb.Tokens) != 4 {
			t.Fatalf("a $ b: got %d tokens, want 4 (LowerIdent Error LowerIdent Eof): %+v", len(tb.Tokens), tb.Tokens)
		}
		wantKinds(t, "after the error", "a $ b", syntax.LowerIdent, syntax.Error, syntax.LowerIdent, syntax.Eof)
		if tb.Tokens[1].Span.Lo != 2 || tb.Tokens[1].Span.Hi != 3 {
			t.Errorf("Error token has span [%d,%d), want [2,3)", tb.Tokens[1].Span.Lo, tb.Tokens[1].Span.Hi)
		}
		_, sink, id, _ = lexFile(t, "&")
		wantOne(t, "&", sink.All(), id, diag.E(104), "unexpected character `&`", 0, 1, "not valid in Ki source")
	})
	t.Run("unexpected_multibyte_char_E0104", func(t *testing.T) {
		// The message shows a character that is not printable as <U+XXXX>: a
		// bidirectional override, a C1 control, a line separator and a zero-width
		// space would otherwise be invisible or would reorder the message, and a
		// no-break space would look like the ordinary space next to it.
		for _, row := range []struct {
			src   string
			size  uint32
			shown string // the character as the message writes it
		}{
			{"é", 2, "é"},
			{"€", 3, "€"},
			{"😀", 4, "😀"},
			{"\u202e", 3, "<U+202E>"},
			{"\u0085", 2, "<U+0085>"},
			{"\u2028", 3, "<U+2028>"},
			{"\u200b", 3, "<U+200B>"},
			{"\u00a0", 2, "<U+00A0>"},
		} {
			tb, sink, id, _ := lexFile(t, row.src)
			wantOne(t, row.src, sink.All(), id, diag.E(104), "unexpected character `"+row.shown+"`", 0, row.size, "not valid in Ki source")
			if len(tb.Tokens) != 2 || tb.Tokens[0].Kind != syntax.Error || tb.Tokens[1].Kind != syntax.Eof {
				t.Errorf("%q: tokens %+v, want Error Eof", row.src, tb.Tokens)
				continue
			}
			if sp := tb.Tokens[0].Span; sp.Lo != 0 || sp.Hi != row.size {
				t.Errorf("%q: Error token has span [%d,%d), want [0,%d)", row.src, sp.Lo, sp.Hi, row.size)
			}
		}
	})
	t.Run("invalid_utf8_stops_with_E0101", func(t *testing.T) {
		// Whatever comes before the bad byte is not lexed either: the buffer
		// holds only Eof, and the first problem is the only diagnostic.
		for _, row := range []struct {
			src  string
			code diag.Code
		}{
			{"\xff", diag.E(101)},
			{"fn fib\xff x", diag.E(101)},
			{"fn fib\x01 x", diag.E(102)},
		} {
			tb, sink := lexAll(t, row.src)
			if len(tb.Tokens) != 1 || tb.Tokens[0].Kind != syntax.Eof {
				t.Errorf("%q: tokens %+v, want only Eof", row.src, tb.Tokens)
			}
			if got := sink.All(); len(got) != 1 || got[0].Code != row.code {
				t.Errorf("%q: diagnostics %+v, want exactly one %v", row.src, got, row.code)
			}
		}
	})
	t.Run("spans_are_within_file", func(t *testing.T) {
		const src = "fn fib(n) { }"
		tb, sink, id, _ := lexFile(t, src)
		if n := len(sink.All()); n != 0 {
			t.Fatalf("got %d diagnostics, want none: %+v", n, sink.All())
		}
		for i, tok := range tb.Tokens {
			if tok.Span.Lo > tok.Span.Hi || tok.Span.Hi > uint32(len(src)) {
				t.Errorf("token %d (%v): span [%d,%d) is not within [0,%d]", i, tok.Kind, tok.Span.Lo, tok.Span.Hi, len(src))
			}
			if tok.Span.File != id {
				t.Errorf("token %d (%v): span in file %d, want %d", i, tok.Kind, tok.Span.File, id)
			}
		}
		// The exact spans, and the Eof, which is empty and sits at the end.
		want := [][2]uint32{{0, 2}, {3, 6}, {6, 7}, {7, 8}, {8, 9}, {10, 11}, {12, 13}, {13, 13}}
		if len(tb.Tokens) != len(want) {
			t.Fatalf("got %d tokens, want %d: %+v", len(tb.Tokens), len(want), tb.Tokens)
		}
		for i, w := range want {
			if sp := tb.Tokens[i].Span; sp.Lo != w[0] || sp.Hi != w[1] {
				t.Errorf("token %d (%v): span [%d,%d), want [%d,%d)", i, tb.Tokens[i].Kind, sp.Lo, sp.Hi, w[0], w[1])
			}
		}

		// `ki emit tokens` prints where each token starts as line:column.
		const wantDump = "1:1 KwFn\n1:4 LowerIdent(fib)\n1:7 LParen\n1:8 LowerIdent(n)\n1:9 RParen\n1:11 LBrace\n1:13 RBrace\n1:14 Eof\n"
		if got := dumpTokens(t, src); got != wantDump {
			t.Errorf("DumpTokens of %q = %q, want %q", src, got, wantDump)
		}
	})
	t.Run("dump_tokens", func(t *testing.T) {
		// One line per token: where it starts as line:column, its kind and, for
		// the kinds that carry a value, the value in parentheses.
		for _, row := range []struct{ src, want string }{
			// The end of the file after a line break is on the next line.
			{"fn fib\n", "1:1 KwFn\n1:4 LowerIdent(fib)\n2:1 Eof\n"},
			// A Newline starts at the line feed, or at the carriage return of a CRLF.
			{"a\nb", "1:1 LowerIdent(a)\n1:2 Newline\n2:1 LowerIdent(b)\n2:2 Eof\n"},
			{"a\r\nb", "1:1 LowerIdent(a)\n1:2 Newline\n2:1 LowerIdent(b)\n2:2 Eof\n"},
			// The values: an Int, a Float, a Char as a code point and a String
			// quoted as Go does, with its escape resolved.
			{`20 1_000.5 'a' "a\tb"`, "1:1 Int(20)\n1:4 Float(1000.5)\n1:12 Char(U+0061)\n1:16 String(\"a\\tb\")\n1:22 Eof\n"},
			{`"a${x}b${y}c"`, "1:1 StringHead(\"a\")\n1:5 LowerIdent(x)\n1:6 StringMid(\"b\")\n1:10 LowerIdent(y)\n1:11 StringTail(\"c\")\n1:14 Eof\n"},
			{"$", "1:1 Error\n1:2 Eof\n"},
			// A Char is U+ and its code point in upper-case hexadecimal with at
			// least four digits: a letter stays a capital, and a code point past
			// U+FFFF takes a fifth digit.
			{`'é' '\u{1F600}'`, "1:1 Char(U+00E9)\n1:5 Char(U+1F600)\n1:16 Eof\n"},
			// A Float is written as strconv does with 'g' and no fixed precision:
			// the shortest text that reads back as the same number, however many
			// digits that takes (3.141592653589793 has sixteen), with an exponent
			// when it is very large or very small.
			{"1e21 1e-7 3.141592653589793", "1:1 Float(1e+21)\n1:6 Float(1e-07)\n1:11 Float(3.141592653589793)\n1:28 Eof\n"},
			// A token several lines down, after a blank line and a comment, and
			// tokens after a multibyte one, a tab and a lone carriage return on
			// the same line: the String "ñ" is 3 code points but 4 bytes, so b
			// is in column 7 (the tab takes one column, not four), and c is in
			// column 9 (the carriage return takes one column and starts no line).
			{"a\n\n// c\n  \"ñ\"\tb\rc", "1:1 LowerIdent(a)\n1:2 Newline\n4:3 String(\"ñ\")\n4:7 LowerIdent(b)\n4:9 LowerIdent(c)\n4:10 Eof\n"},
		} {
			if got := dumpTokens(t, row.src); got != row.want {
				t.Errorf("DumpTokens of %q = %q, want %q", row.src, got, row.want)
			}
		}

		// The long row below, with a DumpTokens that is wrong and slow, would
		// run until the time limit of go test and hide the messages of the rows
		// above behind a panic. So stop here if one of them failed.
		if t.Failed() {
			return
		}

		// A file that is one very long line. A DumpTokens that asked
		// SourceManager.LineCol for the position of every token would count
		// the code points from the start of the line each time, and its cost
		// would grow with the square of the line: this file would not finish
		// within the default time limit of go test, which is the only clock
		// here (the test measures no time, so a slow machine does not fail it).
		// The tokens are the strings "ñ" followed by a space: 5 bytes but 4
		// code points, so a column counted in bytes is wrong from the second
		// token on.
		const n = 2_000_000
		rest := dumpTokens(t, strings.Repeat(`"ñ" `, n))
		for i := 0; i <= n; i++ {
			want := fmt.Sprintf("1:%d String(\"ñ\")", 1+4*i)
			if i == n {
				want = fmt.Sprintf("1:%d Eof", 1+4*n)
			}
			line, after, found := strings.Cut(rest, "\n")
			if !found || line != want {
				// The first line that differs is enough: the dump has two
				// million of them.
				t.Fatalf("DumpTokens of %d strings on one line: line %d is %q, want %q", n, i+1, line, want)
			}
			rest = after
		}
		if rest != "" {
			t.Errorf("DumpTokens of %d strings on one line goes on after the Eof: %d more bytes", n, len(rest))
		}
	})
}
