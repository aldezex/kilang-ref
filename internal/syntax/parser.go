package syntax

import (
	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// Parse turns the tokens of a file into its syntax tree. tb must be the result
// of Lex: a buffer whose last token is the only Eof.
//
// The parser is recursive descent. This session reads the top-level functions:
// attributes, `pub`, the name, the parameters, the return type and a body that
// is either absent or `{}`. Expressions and statements arrive in the next
// sessions, and so do the parser's diagnostics: until then Parse reports
// nothing to sink. It does not stop at tokens it cannot read, though: the
// place where the text stops making sense becomes an error node (ItemError,
// TypeError or ExprError), and the parser goes on with the tokens after it.
// Each pass of its loops consumes at least one token, which is what makes it end.
func Parse(tb *TokenBuffer, file base.FileID, in *base.Interner, sink *diag.Sink) *Module {
	p := newParser(tb, file, in, sink)
	p.module()
	return p.m
}

// parser is the state of one call to Parse.
type parser struct {
	tb  *TokenBuffer
	pos int // index in tb.Tokens of the current token
	// end is where the last token that the parser consumed ends, not counting
	// Newlines, so that the span of a node stops at the node's last token and
	// not at the line break after it. It is 0 before the first token.
	end uint32
	m   *Module

	// in and sink are not used yet: the parser reads the Symbols the lexer
	// interned and reports nothing, for now. They are here so that the parser
	// has the shape it keeps in the next sessions.
	in   *base.Interner
	sink *diag.Sink
}

// newParser returns a parser that is about to read the first token of tb.
func newParser(tb *TokenBuffer, file base.FileID, in *base.Interner, sink *diag.Sink) *parser {
	return &parser{tb: tb, m: &Module{File: file}, in: in, sink: sink}
}

// tok returns the current token without consuming it.
func (p *parser) tok() Token { return p.tb.Tokens[p.pos] }

// at reports whether the current token is of kind k.
func (p *parser) at(k TokenKind) bool { return p.tok().Kind == k }

// advance consumes the current token and returns it. Eof is never consumed:
// at the end of the file advance returns Eof again and moves nowhere, so the
// cursor cannot leave the buffer.
func (p *parser) advance() Token {
	t := p.tok()
	if t.Kind != Eof {
		p.pos++
		if t.Kind != Newline {
			p.end = t.Span.Hi
		}
	}
	return t
}

// eat consumes the current token and returns it with true if it is of kind k;
// otherwise it consumes nothing and returns false.
func (p *parser) eat(k TokenKind) (Token, bool) {
	if !p.at(k) {
		return Token{}, false
	}
	return p.advance(), true
}

// skipNewlines consumes the Newline tokens at the cursor.
func (p *parser) skipNewlines() {
	for p.at(Newline) {
		p.advance()
	}
}

// spanFrom returns the span that starts where start does and ends where the
// last consumed token ends. If nothing has been consumed since start, the span
// is empty and sits at the start of start.
func (p *parser) spanFrom(start base.Span) base.Span {
	return base.Span{File: start.File, Lo: start.Lo, Hi: max(p.end, start.Lo)}
}

// module reads items until the end of the file. Every pass of the loop
// consumes at least one token (see item), so the loop ends.
func (p *parser) module() {
	for {
		// Items on different lines have a Newline between them (line rule).
		p.skipNewlines()
		if p.at(Eof) {
			return
		}
		p.m.Items = append(p.m.Items, p.item())
	}
}

// errorItem adds an ItemError that covers the tokens from start to the last
// one consumed, and returns its id.
func (p *parser) errorItem(start base.Span) ItemID {
	return p.m.ItemNodes.Push(Item{Kind: ItemError, Span: p.spanFrom(start)})
}

// item reads one item and always consumes at least one token. The grammar is
//
//	item = { attr } [ 'pub' ] 'fn' lower '(' [ param { ',' param } ] ')'
//	       [ '->' type ] [ block ]
//
// A function has a body exactly when a `{` follows its signature on the same
// line; the line rule puts a Newline in front of a `{` on the next one. A
// token that cannot start an item, or a signature that stops making sense, is
// an ItemError: the tokens read so far are consumed, and the item that follows
// starts at the first one that is not.
func (p *parser) item() ItemID {
	start := p.tok().Span
	if !p.at(Hash) && !p.at(KwPub) && !p.at(KwFn) {
		p.advance()
		return p.errorItem(start)
	}

	it := Item{Kind: ItemFn}
	var ok bool
	if it.Attrs, ok = p.attrs(); !ok {
		return p.errorItem(start)
	}
	_, it.Pub = p.eat(KwPub)
	if _, ok = p.eat(KwFn); !ok {
		return p.errorItem(start)
	}
	name, ok := p.eat(LowerIdent)
	if !ok {
		return p.errorItem(start)
	}
	it.Name, it.NameSpan = base.Symbol(name.Payload), name.Span
	if _, ok = p.eat(LParen); !ok {
		return p.errorItem(start)
	}
	if it.Params, ok = p.params(); !ok {
		return p.errorItem(start)
	}
	if _, ok = p.eat(Arrow); ok {
		it.Ret, it.HasRet = p.typeExpr(), true
	}
	if p.at(LBrace) {
		it.Body, it.HasBody = p.block(), true
	}
	it.Span = p.spanFrom(start)
	return p.m.ItemNodes.Push(it)
}

// attrs reads the attributes in front of an item, which may be none, and adds
// them to AttrPool. A line break after an attribute is a Newline token, as
// after any token that can end an expression, so attrs skips the Newlines
// that follow each one: the item starts on the next line. It returns false, and
// adds nothing to the pool, if an attribute is malformed.
func (p *parser) attrs() (base.ListRange, bool) {
	var list []Attr
	for p.at(Hash) {
		a, ok := p.attr()
		if !ok {
			return base.ListRange{}, false
		}
		list = append(list, a)
		p.skipNewlines()
	}
	return appendRange(&p.m.AttrPool, list), true
}

// attr reads one attribute:
//
//	attr = '#' '[' lower [ '(' string { ',' string } ')' ] ']'
//
// The lexer has no token for `#[`, so it is a Hash and an LBracket, and the
// parser does not check that nothing separates them. The arguments are string
// literals without interpolation.
func (p *parser) attr() (Attr, bool) {
	hash := p.advance()
	if _, ok := p.eat(LBracket); !ok {
		return Attr{}, false
	}
	name, ok := p.eat(LowerIdent)
	if !ok {
		return Attr{}, false
	}
	a := Attr{Name: base.Symbol(name.Payload)}
	if _, ok := p.eat(LParen); ok {
		for {
			arg, ok := p.eat(String)
			if !ok {
				return Attr{}, false
			}
			a.Args = append(a.Args, p.tb.Strings[arg.Payload])
			if _, ok := p.eat(Comma); !ok {
				break
			}
		}
		if _, ok := p.eat(RParen); !ok {
			return Attr{}, false
		}
	}
	if _, ok := p.eat(RBracket); !ok {
		return Attr{}, false
	}
	a.Span = p.spanFrom(hash.Span)
	return a, true
}

// params reads the parameters after the `(` of a function, and the `)`, and
// adds the parameters to ParamPool. It returns false, and adds nothing to the
// pool, if the list is malformed.
//
//	params = [ param { ',' param } ] ')'
//
// A comma after the last parameter is not allowed.
func (p *parser) params() (base.ListRange, bool) {
	var list []Param
	if !p.at(RParen) {
		for {
			par, ok := p.param()
			if !ok {
				return base.ListRange{}, false
			}
			list = append(list, par)
			if _, ok := p.eat(Comma); !ok {
				break
			}
		}
	}
	if _, ok := p.eat(RParen); !ok {
		return base.ListRange{}, false
	}
	return appendRange(&p.m.ParamPool, list), true
}

// param reads one parameter: param = lower [ ':' type ].
func (p *parser) param() (Param, bool) {
	name, ok := p.eat(LowerIdent)
	if !ok {
		return Param{}, false
	}
	par := Param{Name: base.Symbol(name.Payload)}
	if _, ok := p.eat(Colon); ok {
		par.Type, par.HasType = p.typeExpr(), true
	}
	par.Span = p.spanFrom(name.Span)
	return par, true
}

// typeExpr reads a type: an UpperIdent (`Int`, `Bool`; whether the name exists
// is for name resolution to say) or `()`. If the tokens at the cursor are
// neither, it returns a TypeError, which covers whatever it consumed: nothing
// at all for a token that is not a `(`, so that the `)` that ends a parameter
// list is still there for the caller.
func (p *parser) typeExpr() TypeExprID {
	start := p.tok().Span
	if t, ok := p.eat(UpperIdent); ok {
		return p.m.Types.Push(TypeExpr{Kind: TypeNamed, Span: t.Span, Name: base.Symbol(t.Payload)})
	}
	if _, ok := p.eat(LParen); ok {
		if _, ok := p.eat(RParen); ok {
			return p.m.Types.Push(TypeExpr{Kind: TypeUnit, Span: p.spanFrom(start)})
		}
	}
	return p.m.Types.Push(TypeExpr{Kind: TypeError, Span: p.spanFrom(start)})
}

// block reads a block, which the cursor is at the `{` of, and returns it as an
// ExprBlock. This session reads only `{}`: anything between the braces makes
// an ExprError that covers the `{`, and the parser goes on with the first
// token after it.
func (p *parser) block() ExprID {
	open := p.advance()
	if _, ok := p.eat(RBrace); !ok {
		return p.m.Exprs.Push(Expr{Kind: ExprError, Span: p.spanFrom(open.Span)})
	}
	return p.m.Exprs.Push(Expr{Kind: ExprBlock, Span: p.spanFrom(open.Span)})
}
