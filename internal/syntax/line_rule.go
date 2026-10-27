package syntax

import (
	"bytes"

	"github.com/aldezex/kilang/internal/base"
)

// The line rule (spec 5.2) decides which line breaks end a statement, an item
// or a match arm. The lexer reads the breaks as a flag on the next token
// (Token.NewlineBefore); insertSeparators turns the ones that matter into
// Newline tokens, so the parser never has to look at lines.
//
// A line break before a token t becomes a Newline in front of t when all three
// of these hold:
//  1. the innermost open bracket is a { or there is none: inside ( and [ a line
//     break is just a space;
//  2. the token before t can end an expression, so the line is complete;
//  3. t is not one of |>, ., &&, || and else, which continue the previous line.
//
// A line that starts with - or + is not on that list, so it starts a new
// statement whenever the line before it is complete. There is never a Newline in
// front of the Eof.

// endsExpression reports whether a token of this kind can be the last one of an
// expression. Error is not in the list on purpose: after a lexical error nothing
// is known about the line, so it stays joined to the next one instead of
// splitting a statement in two and answering one mistake with two parse errors.
func endsExpression(k TokenKind) bool {
	switch k {
	case LowerIdent, UpperIdent, Int, Float, Char, String, StringTail, KwTrue, KwFalse,
		RParen, RBracket, RBrace, Question, Underscore, KwReturn, KwBreak, KwContinue:
		return true
	}
	return false
}

// continuesLine reports whether a line that starts with a token of this kind
// belongs to the line before it.
func continuesLine(k TokenKind) bool {
	switch k {
	case Pipe, Dot, AndAnd, OrOr, KwElse:
		return true
	}
	return false
}

// bracketStack is the stack of the ( [ { that are open, outermost first. It
// also counts how many of each kind there are, so that a closing bracket with no
// partner is known to have none without looking through the stack.
//
// Each bracket remembers how many interpolations (the `${ … }` of a string)
// were open when it was pushed, so that the end of an interpolation can close
// the brackets that its expression opened.
type bracketStack struct {
	kinds                    []TokenKind
	levels                   []int // levels[i] is the value of level when kinds[i] was pushed
	parens, brackets, braces int   // how many of each kind are in kinds
	level                    int   // how many interpolations are open; see enter
}

// count returns the counter of the opening bracket of the given kind.
func (s *bracketStack) count(open TokenKind) *int {
	switch open {
	case LParen:
		return &s.parens
	case LBracket:
		return &s.brackets
	}
	return &s.braces
}

// push opens a bracket.
func (s *bracketStack) push(open TokenKind) {
	s.kinds = append(s.kinds, open)
	s.levels = append(s.levels, s.level)
	*s.count(open)++
}

// pop closes the innermost open bracket, which must exist.
func (s *bracketStack) pop() TokenKind {
	top := s.kinds[len(s.kinds)-1]
	s.kinds = s.kinds[:len(s.kinds)-1]
	s.levels = s.levels[:len(s.levels)-1]
	*s.count(top)--
	return top
}

// close handles a closing bracket whose partner is want: it closes the
// innermost open bracket of that kind and every one that was opened inside it. If
// there is none, the stack stays as it is.
//
// Looking for the partner does not cost more than closing it: the search only
// starts when the counter says there is one, and it pops every bracket it
// passes. Each bracket is pushed once, so it is popped at most once, and a
// closing bracket without a partner costs nothing.
func (s *bracketStack) close(want TokenKind) {
	if *s.count(want) == 0 {
		return
	}
	for s.pop() != want {
	}
}

// enter starts an interpolation, or the expressions of a string that never
// ends: the brackets pushed from now until the matching leave were opened
// inside it.
func (s *bracketStack) enter() {
	s.level++
}

// leave ends the innermost interpolation that is open; every call matches an
// earlier enter, and a call without one panics. It closes every bracket that was
// opened inside it and is still open, and none that was open before: those stay,
// unless the expression closed them itself. In `( "${ ) [ }"` the expression
// closes the ( and leaves the [, and leave closes the [.
func (s *bracketStack) leave() {
	if s.level == 0 {
		// Without this, level would go to -1 and the loop below would close
		// every bracket, silently.
		panic("unreachable: leave without enter")
	}
	for len(s.levels) > 0 && s.levels[len(s.levels)-1] >= s.level {
		s.pop()
	}
	s.level--
}

// inBlock reports whether the innermost open bracket is a { or there is none,
// which is where a line break can end a statement.
func (s *bracketStack) inBlock() bool {
	return len(s.kinds) == 0 || s.kinds[len(s.kinds)-1] == LBrace
}

// insertSeparators puts a Newline token in front of every token that starts a
// line that ends a statement, following the line rule above. It makes one pass
// over lx.tb.Tokens and replaces them with a copy that has the Newlines.
//
// The brackets are tracked here, with a stack of their own, and not by the
// lexer's stack of braces, which holds only { and the marks of interpolations.
// The `${` and `}` of an interpolation are not tokens, so they have no entry,
// but they mark where the brackets of its expression end: the StringHead starts
// the interpolations of its string and the StringTail ends them, closing every
// bracket that their expressions left open (see bracketStack.leave). A string
// that never ends has an Error token in place of its head and no tail; the line
// break that ended it ends its interpolations too.
//
// A closing bracket closes the brackets that are open inside its partner as
// well (see bracketStack.close), and is ignored if it has no partner. So a ( that
// is never closed stops hiding the line breaks at the } or ] that encloses it,
// and a closing bracket that was never opened does not close a bracket of
// another kind. The parser reports both mistakes.
//
// The Newline's span is the first line terminator after the previous token: the
// LF, or the CR and LF of a CRLF. The token t keeps its NewlineBefore flag.
func (lx *lexer) insertSeparators() {
	toks := lx.tb.Tokens
	out := make([]Token, 0, len(toks))
	var open bracketStack // the ( [ { that are still open
	// unterminated is set from the Error token of a string that never ends
	// until the line break that ended it.
	unterminated := false
	for i, t := range toks {
		if unterminated && t.NewlineBefore {
			// This token is the first one after the string. The string has no
			// StringTail to end its interpolation, so the line break does: the
			// brackets that the interpolation left open (`"${ f(a`) end here,
			// before the line break is judged.
			open.leave()
			unterminated = false
		}
		if i > 0 && t.NewlineBefore && t.Kind != Eof && open.inBlock() &&
			endsExpression(toks[i-1].Kind) && !continuesLine(t.Kind) {
			out = append(out, lx.newlineAfter(toks[i-1]))
		}
		out = append(out, t)
		switch t.Kind {
		case LParen, LBracket, LBrace:
			open.push(t.Kind)
		case RParen:
			open.close(LParen)
		case RBracket:
			open.close(LBracket)
		case RBrace:
			open.close(LBrace)
		case StringHead:
			open.enter()
		case StringTail:
			// The tail ends the expression of the last interpolation. Brackets
			// it left open (`"${ f(a }"`) end with it, instead of hiding the line
			// breaks of the rest of the file. A StringMid does nothing: the
			// string goes on, and its tail closes the brackets of all its
			// interpolations at once. No decision depends on them before that,
			// because a string that ends has no line break inside.
			open.leave()
		case Error:
			// The only Error that starts with a quote is a string that never
			// ends (E0110): the other one, E0104, starts at the character it
			// rejects, and a quote starts a string. The lexer dropped the
			// string's StringHead, so the interpolation is entered here.
			if lx.src[t.Span.Lo] == '"' {
				open.enter()
				unterminated = true
			}
		}
	}
	lx.tb.Tokens = out
}

// newlineAfter returns the Newline token for the line break that follows prev:
// its span is the first line terminator after prev.
func (lx *lexer) newlineAfter(prev Token) Token {
	i := bytes.IndexByte(lx.src[prev.Span.Hi:], '\n')
	if i < 0 {
		// A token is only flagged NewlineBefore when a line feed went by after
		// the token before it.
		panic("unreachable: no line feed after the token before a NewlineBefore")
	}
	end := int(prev.Span.Hi) + i + 1
	start := end - 1
	if start > int(prev.Span.Hi) && lx.src[start-1] == '\r' {
		start-- // a CRLF is one line break
	}
	return Token{Kind: Newline, Span: base.Span{File: lx.id, Lo: uint32(start), Hi: uint32(end)}}
}
