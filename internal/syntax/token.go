package syntax

import "github.com/aldezex/kilang/internal/base"

// The token kinds (TokenKind, its constants, String and the keywords table)
// live in token_kind.go, which go generate writes from token_kinds.txt. After
// changing the list, run `go generate ./internal/syntax`.
//
//go:generate go run ./gen -o token_kind.go

// Token is one lexical unit of a source file. It is small and holds no text:
// the text is in the file, at Span, and anything the kind needs besides it is
// in Payload.
type Token struct {
	Kind TokenKind
	Span base.Span
	// Payload depends on Kind. For LowerIdent and UpperIdent it is the
	// base.Symbol of the name. For Int, Float, String, StringHead, StringMid
	// and StringTail it is an index into the Ints, Floats or Strings slice of
	// the TokenBuffer that holds the token. For Char it is the code point.
	// Every other kind leaves it 0.
	Payload uint32
	// NewlineBefore says that a line break sits between the previous token (or
	// the start of the file) and this one, comments and spaces aside. The line
	// rule (spec 5.2) reads it.
	NewlineBefore bool
}

// TokenBuffer is the result of lexing a file: its tokens, the last of them Eof,
// and the values of the literals that Token.Payload indexes. Keeping the values
// out of Token makes every token the same small size.
//
// The tokens are ordered by Span.Lo, not by Span.Hi: an Error token may contain
// the tokens that follow it. The Error of a string that never ends goes where
// the string starts and covers the rest of its line, and the tokens of the
// interpolation that was open stay after it, inside its span. So Hi can go down
// from one token to the next, and the end of a run of tokens is the largest Hi
// among them, not the Hi of the last one.
type TokenBuffer struct {
	Tokens  []Token
	Ints    []int64
	Floats  []float64
	Strings []string

	// src is the text of the file that Lex read, which is where the text of a
	// token is: src[Span.Lo:Span.Hi]. Only Lex sets it. The parser reads it to
	// quote the token in front of it when it reports an error (see text).
	src []byte
}

// text returns the text of t as the file has it. t must be a token of tb, and
// tb a buffer that Lex returned: any other has no src.
func (tb *TokenBuffer) text(t Token) string {
	return string(tb.src[t.Span.Lo:t.Span.Hi])
}
