package syntax

import (
	"strings"
	"unicode/utf8"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// Character and string literals (spec 5.2, Literales, which gives only the
// examples; the rules below are this lexer's own):
//
//	Char   = "'" ( c | escape ) "'"
//	String = '"' { c | escape | "${" expr "}" } '"'
//	escape = '\' ( n | t | r | '\' | "'" | '"' | 0 | '$' | u '{' h{1,6} '}' )
//
// where c is any character but the delimiter, a backslash or a line break, and
// the number in `\u{…}` is a valid Unicode scalar value.
//
// A literal cannot hold a line break, so a literal that is not closed on its
// line ends where the line ends and lexing goes on at the line break.
//
// A string with no `${` is one String token. With them, it is split around each
// interpolation: StringHead (from the opening quote up to and including the
// first `${`), the tokens of the expression, then a StringMid for every `}` that
// is followed by another `${` and, to finish, a StringTail (from the last `}` up
// to and including the closing quote). The payload of the four is an index into
// TokenBuffer.Strings, which holds the text with its escapes already resolved.

// escapesNote is the note of E0106. The `…` is U+2026.
const escapesNote = `valid escapes: \n \t \r \\ \' \" \0 \$ \u{…}`

// maxHelpText is the longest text between the quotes of a character literal,
// in bytes, that the help of E0108 quotes as the string to write. The help is
// a hint and the edit carries the whole text (the human format shows only a
// window of it), so a longer text is left out of the help and the help line
// does not grow with the literal.
const maxHelpText = 80

// modeKind says what the lexer is reading.
type modeKind uint8

const (
	// modeString is the text of a string literal.
	modeString modeKind = iota
	// modeCode is the expression of an interpolation, between `${` and its `}`.
	modeCode
)

// mode is an entry of the stack of modes. Strings and code nest, because the
// expression of an interpolation can hold another string, so the stack runs
// string, code, string, code… from the outermost string in; an empty stack is
// the code of the file itself. The fields after kind describe a modeString
// entry and are zero in a modeCode one.
type mode struct {
	kind modeKind
	// quote is the offset of the opening quote.
	quote int
	// first is where the first token of the string goes in the TokenBuffer.
	first int
	// newline says whether a line break came before the opening quote.
	newline bool
	// brackets and parts are the heights of interpState.brackets and
	// interpState.parts when the string opened, so closing it can drop what it
	// added.
	brackets, parts int
}

// bracket is an entry of the stack of open braces.
type bracket uint8

const (
	// normalBrace is a `{` that has not met its `}` yet.
	normalBrace bracket = iota
	// InterpSentinel is pushed on the stack when a `${` opens an interpolation.
	// The `}` that finds it on top closes the interpolation, not a brace. Braces
	// opened inside the expression sit above it, so they close first.
	InterpSentinel
)

// interpState is the part of the lexer that strings and interpolation need.
type interpState struct {
	modes []mode
	// brackets holds the open `{` of the file and the sentinel of each open
	// interpolation, in order. Parentheses and square brackets stay out of it:
	// only a `}` has to decide whether it closes a brace or an interpolation.
	brackets []bracket
	// parts holds the index in the TokenBuffer of every StringHead and
	// StringMid of the strings that are still open. If one of them turns out
	// to be unterminated, these are the tokens that its Error replaces.
	parts []int
	// broken says that a line break went by in the code of an interpolation,
	// and breakAt is the offset of the first one.
	broken  bool
	breakAt int
}

// inInterpolation reports whether the lexer is reading the expression of an
// interpolation.
func (lx *lexer) inInterpolation() bool {
	n := len(lx.interp.modes)
	return n > 0 && lx.interp.modes[n-1].kind == modeCode
}

// noteBreak records that the lexer is going past the line break at lx.pos
// while skipping trivia. If that happens inside an interpolation, the first
// such break is kept for the main loop, which reports E0110.
func (lx *lexer) noteBreak() {
	lx.newline = true
	if !lx.interp.broken && lx.inInterpolation() {
		lx.interp.broken = true
		lx.interp.breakAt = lx.pos
	}
}

// lineEndAt reports whether a line ends at offset i: at a line feed, at a
// carriage return that a line feed follows (a CRLF is one line break), or at
// the end of the file. A lone carriage return does not end a line.
func (lx *lexer) lineEndAt(i int) bool {
	if i >= len(lx.src) {
		return true
	}
	c := lx.src[i]
	return c == '\n' || c == '\r' && i+1 < len(lx.src) && lx.src[i+1] == '\n'
}

// atLineEnd is lineEndAt for the current position.
func (lx *lexer) atLineEnd() bool { return lx.lineEndAt(lx.pos) }

// escape resolves the escape sequence whose backslash is at lx.pos: it appends
// the character it stands for to text, moves past the sequence and returns the
// text. A sequence that is not an escape gives E0106 and contributes U+FFFD, and
// the lexer goes on right after the characters that the error covers.
//
// A backslash at the end of a line is not an escape: escape only moves past it.
// The caller sees the line end next and reports the literal as unterminated,
// which is the one mistake there is.
func (lx *lexer) escape(text []byte) []byte {
	start := lx.pos
	if lx.lineEndAt(start + 1) {
		lx.pos++
		return text
	}
	c := lx.src[start+1]
	if v, ok := simpleEscape(c); ok {
		lx.pos += 2
		return append(text, v)
	}
	if c == 'u' {
		return lx.unicodeEscape(text)
	}
	// Any other character, however many bytes it takes, is the whole of the
	// bad escape.
	_, size := utf8.DecodeRune(lx.src[start+1:])
	lx.pos += 1 + size
	return lx.badEscape(text, start)
}

// simpleEscape returns the character that the escape `\c` stands for, for the
// escapes with a one-character spelling.
func simpleEscape(c byte) (v byte, ok bool) {
	switch c {
	case 'n':
		return '\n', true
	case 't':
		return '\t', true
	case 'r':
		return '\r', true
	case '0':
		return 0, true
	case '\\', '\'', '"', '$':
		return c, true
	}
	return 0, false
}

// unicodeEscape resolves the `\u{H…}` whose backslash is at lx.pos. When it is
// not a valid one it consumes the `\u`, then, if a `{` follows, every
// hexadecimal digit after it (also past the sixth) and a `}` if that is what
// comes next; a missing `}` is left where it is.
func (lx *lexer) unicodeEscape(text []byte) []byte {
	start := lx.pos
	lx.pos += 2
	if lx.pos < len(lx.src) && lx.src[lx.pos] == '{' {
		lx.pos++
		first := lx.pos
		for lx.pos < len(lx.src) && isHexDigit(lx.src[lx.pos]) {
			lx.pos++
		}
		digits := lx.src[first:lx.pos]
		if lx.pos < len(lx.src) && lx.src[lx.pos] == '}' {
			lx.pos++
			if len(digits) >= 1 && len(digits) <= 6 {
				// Six hexadecimal digits are 24 bits, so v cannot overflow.
				var v rune
				for _, c := range digits {
					v = v<<4 | rune(hexValue(c))
				}
				if utf8.ValidRune(v) {
					return utf8.AppendRune(text, v)
				}
			}
		}
	}
	return lx.badEscape(text, start)
}

// hexValue returns the value of the hexadecimal digit c, which isHexDigit has
// accepted.
func hexValue(c byte) byte {
	switch {
	case c <= '9':
		return c - '0'
	case c >= 'a':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// badEscape reports E0106 for the text that goes from start to the current
// position and appends U+FFFD to text. The message shows that text as visible
// does, since the character after the backslash can be a tab or a carriage
// return.
//
// The help is that a backslash is written by doubling it. In a string the
// fix-it does that for the whole escape, `\\q` for `\q`, and the text that
// followed the backslash becomes plain text; this is also what it does for a
// `\u{…}` that is not valid. In a character it offers no fix-it, because
// `'\\q'` holds two characters: the help is still true, the edit is not one.
func (lx *lexer) badEscape(text []byte, start int) []byte {
	span := lx.span(start)
	d := diag.NewError(diag.E(106), "invalid escape sequence `"+visible(string(lx.src[start:lx.pos]))+"`",
		diag.Label{Span: span, Message: "unknown escape"}).
		WithNote(escapesNote).
		WithHelp("to write a backslash, double it: `\\\\`")
	if !lx.inChar {
		d = d.WithFixIt(diag.Replace(span, `\`+string(lx.src[start:lx.pos])))
	}
	lx.sink.Report(d)
	return utf8.AppendRune(text, utf8.RuneError)
}

// danglingBackslash reports whether the backslashes that end the text before
// offset end leave one that nothing follows: the lexer reads them in pairs,
// and an odd one out at the end of a line is not an escape. A quote written
// after it would be the escape's operand, not a closing quote.
func (lx *lexer) danglingBackslash(end int) bool {
	n := 0
	for i := end - 1; i >= 0 && lx.src[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// char lexes the character literal whose opening quote is at lx.pos. Whatever
// is wrong with it (nothing in it, more than one character, no closing quote on
// its line) is E0108 and the token is still a Char, with the payload U+FFFD.
// A literal with no closing quote takes the rest of its line.
func (lx *lexer) char() {
	start := lx.pos
	lx.pos++
	var text []byte
	lx.inChar = true
	for !lx.atLineEnd() && lx.src[lx.pos] != '\'' {
		if lx.src[lx.pos] == '\\' {
			text = lx.escape(text)
		} else {
			text = append(text, lx.src[lx.pos])
			lx.pos++
		}
	}
	lx.inChar = false
	closed := !lx.atLineEnd()
	if closed {
		lx.pos++
	} else {
		// The literal takes the rest of its line, and whatever is written at
		// its end is part of it (see TokenBuffer.unterminatedLiteral).
		lx.tb.unterminatedLiteral = lx.span(start)
	}
	// text holds valid UTF-8, so it is one character exactly when DecodeRune
	// consumes all of it.
	r, size := utf8.DecodeRune(text)
	if closed && size > 0 && size == len(text) {
		lx.emit(Char, start, uint32(r))
		return
	}
	d := diag.NewError(diag.E(108), "invalid character literal",
		diag.Label{Span: lx.span(start), Message: "a character literal holds exactly one character"})
	switch {
	case !closed:
		// The literal runs to the end of the line, which is where the quote
		// goes. A quote there closes a character only if exactly one was
		// written after the opening quote (size is from the decoding above):
		// `'a b` would become a literal of three characters and a lone `'` one
		// of none, both E0108 again. So the edit is offered for one character;
		// with none the help is the one for an empty literal, and with several
		// there is nothing concrete to propose. A quote after a backslash that
		// nothing follows would be escaped, so there the help stays and the
		// edit does not.
		switch {
		case lx.danglingBackslash(lx.pos):
			d = d.WithHelp("close it with `'`")
		case size > 0 && size == len(text):
			d = d.WithHelp("close it with `'`").WithFixIt(diag.Insert(lx.span(lx.pos), "'"))
		case len(text) == 0:
			d = d.WithHelp("write the character between the quotes")
		}
	case len(text) == 0:
		d = d.WithHelp("write the character between the quotes")
	default:
		// Two or more characters: it is text. The string has the characters
		// the file has between the quotes, escapes as they are written, unless
		// one of them would not stay text in a string: a quote ends the string
		// early and a `${` opens an interpolation, so `'a${b}x'` would become
		// a string that reads the variable b instead of the characters. Then
		// the help says what to escape, and there is no fix-it: the edit would
		// not fix the error. Only a quote or a `${` that is not escaped already
		// counts (see hasLoose): in `'a\"b'` the string has the escape it needs.
		inside := string(lx.src[start+1 : lx.pos-1])
		var escape []string
		if hasLoose(inside, `"`) {
			escape = append(escape, "each `\"` in it as `\\\"`")
		}
		if hasLoose(inside, "${") {
			escape = append(escape, "each `${` in it as `\\${`")
		}
		if len(escape) > 0 {
			d = d.WithHelp("for text, write a string, and write " + strings.Join(escape, " and "))
		} else {
			// The help quotes the text as visible shows it, so that a mark that
			// hides itself shows as its code point, and only if it is short;
			// the edit carries the text as the file has it.
			help := "for text, write a string"
			if len(inside) <= maxHelpText {
				help += ": `\"" + visible(inside) + "\"`"
			}
			d = d.WithHelp(help).WithFixIt(diag.Replace(lx.span(start), `"`+inside+`"`))
		}
	}
	lx.sink.Report(d)
	lx.emit(Char, start, uint32(utf8.RuneError))
}

// hasLoose reports whether sub occurs in s, the text between the quotes of a
// literal as the file has it, where no backslash escapes its first byte. A
// backslash and the byte after it are one escape and the scan goes on after
// both, so in `a\"b` the quote is escaped and in `a\\"b` it is not: the
// backslash before it is itself escaped.
func hasLoose(s, sub string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if strings.HasPrefix(s[i:], sub) {
			return true
		}
	}
	return false
}

// str lexes the string literal whose opening quote is at lx.pos, up to the
// closing quote or, if there is an interpolation, up to its `${`.
func (lx *lexer) str() {
	start := lx.pos
	lx.pos++
	st := &lx.interp
	st.modes = append(st.modes, mode{
		kind:     modeString,
		quote:    start,
		first:    len(lx.tb.Tokens),
		newline:  lx.newline,
		brackets: len(st.brackets),
		parts:    len(st.parts),
	})
	lx.stringText(start)
}

// stringText reads the text of the string on top of the stack of modes,
// starting at lx.pos, where the segment that begins at segStart (the opening
// quote or the `}` of an interpolation) continues. It stops at the closing
// quote, which ends the string, at a `${`, which opens an interpolation and
// leaves the lexer in its code, or at the end of the line, which makes the
// string unterminated.
func (lx *lexer) stringText(segStart int) {
	st := &lx.interp
	m := st.modes[len(st.modes)-1]
	first := segStart == m.quote // no interpolation came before this segment
	var text []byte
	for {
		if lx.atLineEnd() {
			lx.unterminated(lx.pos)
			return
		}
		switch c := lx.src[lx.pos]; {
		case c == '"':
			lx.pos++
			kind := StringTail
			if first {
				kind = String
			}
			lx.emitText(kind, segStart, text)
			st.modes = st.modes[:len(st.modes)-1]
			st.parts = st.parts[:m.parts]
			return
		case c == '$' && lx.at(1) == '{':
			lx.pos += 2
			kind := StringMid
			if first {
				kind = StringHead
			}
			lx.emitText(kind, segStart, text)
			st.parts = append(st.parts, len(lx.tb.Tokens)-1)
			st.brackets = append(st.brackets, InterpSentinel)
			st.modes = append(st.modes, mode{kind: modeCode})
			return
		case c == '\\':
			text = lx.escape(text)
		default:
			text = append(text, c)
			lx.pos++
		}
	}
}

// emitText emits the token of a piece of a string, from start to the current
// position, with the text it holds.
func (lx *lexer) emitText(kind TokenKind, start int, text []byte) {
	lx.tb.Strings = append(lx.tb.Strings, string(text))
	lx.emit(kind, start, uint32(len(lx.tb.Strings)-1))
}

// endInterpolation handles the `}` at lx.pos. If the sentinel of an
// interpolation is on top of the stack of braces, the `}` closes it, and
// endInterpolation goes back to reading the text of the string and reports
// true. Otherwise it reports false and does nothing.
func (lx *lexer) endInterpolation() bool {
	st := &lx.interp
	n := len(st.brackets)
	if n == 0 || st.brackets[n-1] != InterpSentinel {
		return false
	}
	start := lx.pos
	lx.pos++
	st.brackets = st.brackets[:n-1]
	st.modes = st.modes[:len(st.modes)-1]
	lx.stringText(start)
	return true
}

// trackBraces keeps the stack of braces up to date for the punctuation token
// of the given kind. A `}` with nothing open pops nothing: it is the parser's
// to report.
func (lx *lexer) trackBraces(kind TokenKind) {
	st := &lx.interp
	switch kind {
	case LBrace:
		st.brackets = append(st.brackets, normalBrace)
	case RBrace:
		if n := len(st.brackets); n > 0 {
			st.brackets = st.brackets[:n-1]
		}
	}
}

// unterminated reports E0110 for the outermost string that is open and
// replaces what it produced by one Error token. at is the offset where its
// line breaks (or the length of the file).
//
// The Error token goes where the string's first token would have been, so
// tokens stay ordered by Span.Lo, and spans from the opening quote to the end of
// the contents of the line. The text parts of the open strings (their
// StringHead and StringMid) are taken out, since they have no end to match; the
// tokens of the expressions, and the strings that are complete, stay after it.
// They lie inside the span of the Error, so a token after it can end before it
// does: see TokenBuffer, whose order is the one of Span.Lo, not of Span.Hi.
// A string inside the expression of another is never reported on its own: the
// line break ends all of them, and the outermost quote is where the literal
// starts. Lexing goes on from lx.pos, in the code of the file.
//
// TokenBuffer.Strings keeps the text of the parts that were taken out. The span
// of the Error token is also recorded as the last literal that never ends (see
// TokenBuffer.unterminatedLiteral).
func (lx *lexer) unterminated(at int) {
	st := &lx.interp
	outer := st.modes[0]

	end := at
	if at > 0 && at < len(lx.src) && lx.src[at] == '\n' && lx.src[at-1] == '\r' {
		end = at - 1 // the carriage return belongs to the line break
	}
	d := diag.NewError(diag.E(110), "unterminated string literal",
		diag.Label{Span: base.Span{File: lx.id, Lo: uint32(outer.quote), Hi: uint32(outer.quote) + 1}, Message: "string starts here"}).
		WithHelp("close the string with `\"`")
	// The closing quote goes where the line breaks. It closes the string only
	// if the line breaks in the text of the string itself: in the code of an
	// interpolation that is open it would start a string there, and after a
	// backslash that nothing follows it would be escaped. Then there is the
	// help and no fix-it. When there is one, the help says where it puts the
	// quote: the lexer cannot tell where the string was meant to end, and the
	// rest of the line may be code that belongs outside it (`print("abc) }`).
	if len(st.modes) == 1 && !lx.danglingBackslash(end) {
		d = d.WithHelp("close the string with `\"` at the end of the line").
			WithFixIt(diag.Insert(lx.span(end), `"`))
	}
	lx.sink.Report(d)

	toks := lx.tb.Tokens
	kept := make([]Token, 0, len(toks)-outer.first)
	drop := 0 // how many of st.parts the loop has gone past
	for i := outer.first; i < len(toks); i++ {
		if drop < len(st.parts) && st.parts[drop] == i {
			drop++
			continue
		}
		kept = append(kept, toks[i])
	}
	if len(toks) == outer.first {
		// Nothing was emitted since the quote, so the Error token takes the
		// line break that was waiting for the string.
		lx.newline = false
	}
	// The span of the Error token is the string as far as it goes: if no line
	// break came before the end of the file, that is the end of the file.
	// Whatever is written inside it or at its end is part of the string. It
	// replaces what a literal inside its interpolation recorded, since that one
	// lies in this span.
	span := base.Span{File: lx.id, Lo: uint32(outer.quote), Hi: uint32(end)}
	lx.tb.unterminatedLiteral = span
	lx.tb.Tokens = append(toks[:outer.first], Token{
		Kind:          Error,
		Span:          span,
		NewlineBefore: outer.newline,
	})
	lx.tb.Tokens = append(lx.tb.Tokens, kept...)

	st.brackets = st.brackets[:outer.brackets]
	st.modes = st.modes[:0]
	st.parts = st.parts[:0]
	st.broken = false
}
