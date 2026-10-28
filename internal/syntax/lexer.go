package syntax

import (
	"bytes"
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
// The tokens include the Newlines that the line rule inserts (see
// insertSeparators in line_rule.go), so a later pass never looks at lines.
//
// A file that CheckSource refuses (invalid UTF-8, a control byte) is not lexed
// at all: the buffer holds only Eof, and sink holds that one error.
//
// Identifiers are interned in in, and their tokens carry the Symbol.
//
// A string or character literal that is not closed on its line is reported
// and lexing goes on at the line break; see unterminated in strings.go.
func Lex(sm *base.SourceManager, id base.FileID, in *base.Interner, sink *diag.Sink) TokenBuffer {
	lx := &lexer{src: sm.Contents(id), id: id, in: in, sink: sink}
	lx.tb.src = lx.src
	if !CheckSource(sm, id, sink) {
		lx.pos = len(lx.src)
		lx.emit(Eof, len(lx.src), 0)
		return lx.tb
	}
	for {
		lx.skipTrivia()
		if lx.interp.broken {
			// A line break inside an interpolation ends the string.
			lx.unterminated(lx.interp.breakAt)
		}
		if lx.pos >= len(lx.src) {
			if len(lx.interp.modes) > 0 {
				// The file ended inside an interpolation.
				lx.unterminated(lx.pos)
			}
			lx.emit(Eof, lx.pos, 0)
			lx.insertSeparators()
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

	// interp is the state that string literals and their interpolations need.
	interp interpState

	// inChar is set while char reads the contents of a character literal, so
	// that an escape that goes wrong knows which kind of literal it is in.
	inChar bool
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
// whether it went past a line break, also one inside a block comment. A line
// break inside an interpolation is also noted for Lex, which ends the string.
func (lx *lexer) skipTrivia() {
	for lx.pos < len(lx.src) {
		switch c := lx.src[lx.pos]; {
		case c == '\n':
			lx.noteBreak()
			lx.pos++
		case c == ' ' || c == '\t' || c == '\r':
			lx.pos++
		case c == '/' && lx.at(1) == '/':
			// Stop before the line break, so that the case above sees it. A
			// lone CR is no line break, so it is part of the comment.
			for lx.pos < len(lx.src) && lx.src[lx.pos] != '\n' {
				lx.pos++
			}
			// Nothing came after the comment, not even a line break.
			lx.tb.endsInComment = lx.pos == len(lx.src)
		case c == '/' && lx.at(1) == '*':
			lx.skipBlockComment()
		default:
			return
		}
	}
}

// skipBlockComment skips a /* */ comment, which may contain other comments:
// each /* inside opens one more level and each */ closes one. If the file ends
// before the outermost one closes, it reports E0103 on the outermost /* and
// notes that the file ends inside a comment.
func (lx *lexer) skipBlockComment() {
	start := lx.pos
	depth := 1
	lx.pos += 2
	unit := lx.pos // where the last unit read starts: a `/*`, a `*/` or one byte
	for lx.pos < len(lx.src) {
		unit = lx.pos
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
				lx.noteBreak()
			}
			lx.pos++
		}
	}
	// The comment runs to the end of the file, so that is where the closing
	// `*/` goes, one for each level that is still open: a fix-it with fewer
	// would leave the file with the same mistake. If the file ends in a `/`
	// that is a unit of its own (the text of the comment, as in a URL, and not
	// the end of a `*/`), the first `*` of the edit would join it into a `/*`
	// and open one level more, so the edit starts with a space.
	closing := strings.Repeat("*/", depth)
	if unit == len(lx.src)-1 && lx.src[unit] == '/' {
		closing = " " + closing
	}
	lx.tb.endsInComment = true
	end := base.Span{File: lx.id, Lo: uint32(len(lx.src))}
	lx.sink.Report(diag.NewError(diag.E(103), "unterminated block comment",
		diag.Label{Span: base.Span{File: lx.id, Lo: uint32(start), Hi: uint32(start) + 2}, Message: "comment starts here"}).
		WithHelp("close the comment with `*/`").
		WithFixIt(diag.Insert(end, closing)))
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
	switch c {
	case '\'':
		lx.char()
		return
	case '"':
		lx.str()
		return
	case '}':
		if lx.endInterpolation() {
			return
		}
	}
	if kind, n, ok := punctuation(c, lx.at(1)); ok {
		start := lx.pos
		lx.pos += n
		lx.trackBraces(kind)
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
	span := base.Span{File: lx.id, Lo: uint32(start), Hi: uint32(lx.pos)}
	d := diag.NewError(diag.E(104), "unexpected character `"+visible(string(r))+"`",
		diag.Label{Span: span, Message: "not valid in Ki source"})
	// A letter of another script is the likely mistake (an accent in a name),
	// and the user may not see why Ki refuses it; for a symbol the label says
	// enough.
	if unicode.IsLetter(r) {
		d = d.WithNote("identifiers and operators are ASCII in Ki")
	}
	lx.sink.Report(d.WithHelp("remove it").WithFixIt(diag.Delete(span)))
	lx.emit(Error, start, 0)
}

// DumpTokens renders tb as text for `ki emit tokens`: one line per token,
// `<line>:<col> <Kind>` and, for the kinds that carry a value, the value in
// parentheses: LowerIdent(fib), Int(20), Float(2.5), Char(U+0061) and
// String("a\tb"), where the strings are quoted as Go does. The position is
// where the token starts. Every line ends with a line feed.
//
// Apart from a binary search per line, the time is linear in the size of the
// file. Asking sm.LineCol for every token would not be: LineCol counts the code
// points from the start of the line each time, so a file that is one very long
// line would cost the square of its length. The tokens come in the order of
// their positions, so DumpTokens keeps the position of the previous one: when
// no line feed lies between the two, the column grows by the number of code
// points between them. Only the first token that starts on each line asks
// sm.LineCol, which costs a binary search over the line starts (a log factor
// in the number of lines) and the code points before that token on its line;
// those add up to at most the size of the file.
func DumpTokens(tb *TokenBuffer, sm *base.SourceManager, in *base.Interner) string {
	var b strings.Builder
	var (
		file base.FileID  // the file of the previous token
		src  []byte       // its contents
		prev uint32       // where the previous token starts
		lc   base.LineCol // and its position
	)
	for i, tok := range tb.Tokens {
		lo := tok.Span.Lo
		if i > 0 && tok.Span.File == file && lo >= prev && bytes.IndexByte(src[prev:lo], '\n') < 0 {
			lc.Col += uint32(utf8.RuneCount(src[prev:lo]))
		} else {
			file = tok.Span.File
			src = sm.Contents(file)
			lc = sm.LineCol(file, lo)
		}
		prev = lo
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
