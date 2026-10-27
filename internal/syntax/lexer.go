package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// Lex turns the file id of sm into tokens. It reports lexical errors (E01xx)
// to sink and always returns a buffer that ends with Eof, so later passes
// never need to check for the end of the tokens.
//
// A file that CheckSource refuses (invalid UTF-8, a control byte) is not lexed
// at all: the buffer holds only Eof, and sink holds that one error.
//
// Identifiers are interned in in, and their tokens carry the Symbol.
func Lex(sm *base.SourceManager, id base.FileID, in *base.Interner, sink *diag.Sink) TokenBuffer {
	lx := &lexer{src: sm.Contents(id), id: id, in: in, sink: sink}
	if !CheckSource(sm, id, sink) {
		lx.pos = len(lx.src)
		lx.emit(Eof, len(lx.src), 0)
		return lx.tb
	}
	for {
		lx.skipTrivia()
		if lx.pos >= len(lx.src) {
			lx.emit(Eof, lx.pos, 0)
			return lx.tb
		}
		lx.next()
	}
}

// lexer is the state of one call to Lex.
type lexer struct {
	src  []byte
	id   base.FileID
	in   *base.Interner
	sink *diag.Sink
	tb   TokenBuffer
	pos  int // offset of the next byte to read

	// newline is set when a line break goes by while skipping spaces and
	// comments, and cleared by emit once the next token records it.
	newline bool
}

// emit appends a token of the given kind that starts at start and ends at the
// current position, and clears the pending line break.
func (lx *lexer) emit(kind TokenKind, start int, payload uint32) {
	lx.tb.Tokens = append(lx.tb.Tokens, Token{
		Kind:          kind,
		Span:          lx.span(start),
		Payload:       payload,
		NewlineBefore: lx.newline,
	})
	lx.newline = false
}

// at returns the byte i places after the current position, or 0 past the end
// of the file. A real 0 byte cannot be mistaken for the end: CheckSource has
// already refused any file that contains one.
func (lx *lexer) at(i int) byte {
	if lx.pos+i < len(lx.src) {
		return lx.src[lx.pos+i]
	}
	return 0
}

// skipTrivia skips spaces, line breaks and comments, and notes in lx.newline
// whether it went past a line break, also one inside a block comment.
func (lx *lexer) skipTrivia() {
	for lx.pos < len(lx.src) {
		switch c := lx.src[lx.pos]; {
		case c == '\n':
			lx.newline = true
			lx.pos++
		case c == ' ' || c == '\t' || c == '\r':
			lx.pos++
		case c == '/' && lx.at(1) == '/':
			// Stop before the line break, so that the case above sees it.
			for lx.pos < len(lx.src) && lx.src[lx.pos] != '\n' {
				lx.pos++
			}
		case c == '/' && lx.at(1) == '*':
			lx.skipBlockComment()
		default:
			return
		}
	}
}

// skipBlockComment skips a /* */ comment, which may contain other comments:
// each /* inside opens one more level and each */ closes one. If the file ends
// before the outermost one closes, it reports E0103 on the outermost /*.
func (lx *lexer) skipBlockComment() {
	start := lx.pos
	depth := 1
	lx.pos += 2
	for lx.pos < len(lx.src) {
		switch {
		case lx.src[lx.pos] == '/' && lx.at(1) == '*':
			depth++
			lx.pos += 2
		case lx.src[lx.pos] == '*' && lx.at(1) == '/':
			depth--
			lx.pos += 2
			if depth == 0 {
				return
			}
		default:
			if lx.src[lx.pos] == '\n' {
				lx.newline = true
			}
			lx.pos++
		}
	}
	lx.sink.Report(diag.NewError(diag.E(103), "unterminated block comment",
		diag.Label{Span: base.Span{File: lx.id, Lo: uint32(start), Hi: uint32(start) + 2}, Message: "comment starts here"}))
}

// next lexes the token that starts at the current position, which is not
// trivia and not the end of the file.
func (lx *lexer) next() {
	c := lx.src[lx.pos]
	if isDecDigit(c) {
		lx.number()
		return
	}
	if isIdentStart(c) {
		lx.ident()
		return
	}
	if kind, n, ok := punctuation(c, lx.at(1)); ok {
		start := lx.pos
		lx.pos += n
		lx.emit(kind, start, 0)
		return
	}
	lx.unexpected()
}

// punctuation returns the kind and the length in bytes of the punctuation
// token that starts with the bytes c and next. It picks the longest token that
// fits (maximal munch): "=>" is one FatArrow, not an Eq and a Gt. next is 0 at
// the end of the file. ok is false when c cannot start a punctuation token.
func punctuation(c, next byte) (kind TokenKind, n int, ok bool) {
	// two returns the two-byte token when next is second, else the one-byte one.
	two := func(second byte, long, short TokenKind) (TokenKind, int, bool) {
		if next == second {
			return long, 2, true
		}
		return short, 1, true
	}
	switch c {
	case '(':
		return LParen, 1, true
	case ')':
		return RParen, 1, true
	case '[':
		return LBracket, 1, true
	case ']':
		return RBracket, 1, true
	case '{':
		return LBrace, 1, true
	case '}':
		return RBrace, 1, true
	case ',':
		return Comma, 1, true
	case ';':
		return Semi, 1, true
	case ':':
		return Colon, 1, true
	case '.':
		return two('.', DotDot, Dot)
	case '=':
		switch next {
		case '>':
			return FatArrow, 2, true
		case '=':
			return EqEq, 2, true
		}
		return Eq, 1, true
	case '-':
		return two('>', Arrow, Minus)
	case '|':
		switch next {
		case '>':
			return Pipe, 2, true
		case '|':
			return OrOr, 2, true
		}
		return Bar, 1, true
	case '!':
		return two('=', NotEq, Bang)
	case '<':
		return two('=', Le, Lt)
	case '>':
		return two('=', Ge, Gt)
	case '+':
		return Plus, 1, true
	case '*':
		return Star, 1, true
	case '/':
		return Slash, 1, true
	case '%':
		return Percent, 1, true
	case '&':
		// A lone & is not a token of Ki.
		if next == '&' {
			return AndAnd, 2, true
		}
	case '?':
		return Question, 1, true
	case '#':
		return Hash, 1, true
	}
	return 0, 0, false
}

// isIdentStart reports whether c can start an identifier.
func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

// isIdentPart reports whether c can continue an identifier.
func isIdentPart(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

// ident lexes an identifier, a keyword or the lone underscore. The first
// character decides the class: an uppercase letter makes an UpperIdent, any
// other (including `_`) a LowerIdent, so `_x`, `_X` and `_1` are LowerIdent.
func (lx *lexer) ident() {
	start := lx.pos
	for lx.pos < len(lx.src) && isIdentPart(lx.src[lx.pos]) {
		lx.pos++
	}
	text := lx.src[start:lx.pos]
	if string(text) == "_" {
		lx.emit(Underscore, start, 0)
		return
	}
	if kw, ok := keywords[string(text)]; ok {
		lx.emit(kw, start, 0)
		return
	}
	switch {
	case text[0] >= 'A' && text[0] <= 'Z':
		lx.emit(UpperIdent, start, uint32(lx.in.Intern(text)))
	default:
		lx.emit(LowerIdent, start, uint32(lx.in.Intern(text)))
	}
}

// visible returns text, which comes from the source file, in a form that a
// message can show. It replaces every character that unicode.IsPrint rejects
// (controls, bidirectional and zero-width marks, spaces other than ' ') with
// <U+XXXX>, so that a message does not hide one of them or change the order of
// the text around it. A character that IsPrint accepts stays as it is, and a
// few of those still show as nothing, such as U+3164 HANGUL FILLER.
func visible(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsPrint(r) {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, "<U+%04X>", r)
		}
	}
	return b.String()
}

// unexpected reports E0104 for the character at the current position and
// emits an Error token that covers all of its bytes, so lexing resumes after
// the whole character and not in the middle of it. The message shows the
// character as visible does.
func (lx *lexer) unexpected() {
	start := lx.pos
	r, size := utf8.DecodeRune(lx.src[lx.pos:])
	lx.pos += size
	lx.sink.Report(diag.NewError(diag.E(104), "unexpected character `"+visible(string(r))+"`",
		diag.Label{Span: base.Span{File: lx.id, Lo: uint32(start), Hi: uint32(lx.pos)}, Message: "not valid in Ki source"}))
	lx.emit(Error, start, 0)
}

// DumpTokens renders tb as text for `ki emit tokens`: one line per token,
// `<line>:<col> <Kind>` and, for the kinds that carry a value, the value in
// parentheses: LowerIdent(fib), Int(20), Float(2.5), Char(U+0061) and
// String("a\tb"), where the strings are quoted as Go does. The position is
// where the token starts. Every line ends with a line feed.
func DumpTokens(tb *TokenBuffer, sm *base.SourceManager, in *base.Interner) string {
	var b strings.Builder
	for _, tok := range tb.Tokens {
		lc := sm.LineCol(tok.Span.File, tok.Span.Lo)
		fmt.Fprintf(&b, "%d:%d %s", lc.Line, lc.Col, tok.Kind)
		switch tok.Kind {
		case LowerIdent, UpperIdent:
			fmt.Fprintf(&b, "(%s)", in.String(base.Symbol(tok.Payload)))
		case Int:
			fmt.Fprintf(&b, "(%d)", tb.Ints[tok.Payload])
		case Float:
			fmt.Fprintf(&b, "(%s)", strconv.FormatFloat(tb.Floats[tok.Payload], 'g', -1, 64))
		case Char:
			fmt.Fprintf(&b, "(U+%04X)", tok.Payload)
		case String, StringHead, StringMid, StringTail:
			fmt.Fprintf(&b, "(%s)", strconv.Quote(tb.Strings[tok.Payload]))
		}
		b.WriteByte('\n')
	}
	return b.String()
}
