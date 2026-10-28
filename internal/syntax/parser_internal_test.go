package syntax

import (
	"testing"

	"github.com/aldezex/kilang/internal/base"
)

func TestParserCursor(t *testing.T) {
	t.Run("advance_stops_at_eof", func(t *testing.T) {
		// The parser never asks for a token past the Eof that ends every buffer
		// Lex returns, but a loop that forgot to test for it must not leave the
		// buffer: advance keeps answering Eof, the cursor stays on it and the
		// end of the last consumed token does not move.
		tb := &TokenBuffer{Tokens: []Token{
			{Kind: LowerIdent, Span: base.Span{Lo: 0, Hi: 1}},
			{Kind: Eof, Span: base.Span{Lo: 1, Hi: 1}},
		}}
		p := newParser(tb, 0, nil, nil)
		if got := p.advance(); got.Kind != LowerIdent || p.pos != 1 || p.prev().Span.Hi != 1 {
			t.Fatalf("first advance: token %v, pos %d, end %d, want LowerIdent, 1, 1", got.Kind, p.pos, p.prev().Span.Hi)
		}
		for i := range 3 {
			if got := p.advance(); got.Kind != Eof || p.pos != 1 || p.prev().Span.Hi != 1 || !p.at(Eof) {
				t.Errorf("advance %d at the end: token %v, pos %d, end %d, want Eof, 1, 1", i+2, got.Kind, p.pos, p.prev().Span.Hi)
			}
		}
		if _, ok := p.eat(LowerIdent); ok || p.pos != 1 {
			t.Errorf("eat of a kind that is not at the cursor: ok %v, pos %d, want false, 1", ok, p.pos)
		}
	})

	t.Run("prev_skips_newlines", func(t *testing.T) {
		// prev is the last token that advance consumed that is not a Newline:
		// the place where a closer that is missing goes, which is after the
		// last token of the line and not at the start of the next one. Before
		// the first token there is none, and the end of the file is never
		// consumed.
		tb := &TokenBuffer{Tokens: []Token{
			{Kind: LowerIdent, Span: base.Span{Lo: 0, Hi: 1}},
			{Kind: Newline, Span: base.Span{Lo: 1, Hi: 2}},
			{Kind: Int, Span: base.Span{Lo: 2, Hi: 3}},
			{Kind: Eof, Span: base.Span{Lo: 3, Hi: 3}},
		}}
		p := newParser(tb, 0, nil, nil)
		if got := p.prev(); got != (Token{}) {
			t.Errorf("before the first token: prev is %+v, want the zero Token", got)
		}
		for i, want := range []TokenKind{LowerIdent, LowerIdent, Int, Int} {
			p.advance()
			if got := p.prev(); got.Kind != want {
				t.Errorf("after %d advances: prev is a %v, want a %v", i+1, got.Kind, want)
			}
		}
		if got := p.prev(); got.Span != (base.Span{Lo: 2, Hi: 3}) {
			t.Errorf("at the end: prev has span %+v, want [2,3)", got.Span)
		}
	})
}
