package syntax

import (
	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// Parse turns the tokens of a file into its syntax tree. tb must be the result
// of Lex: a buffer whose last token is the only Eof.
//
// The parser is recursive descent. Parse reads the top-level functions:
// attributes, `pub`, the name, the parameters, the return type and a body, which
// is a block, or is absent in a function marked `#[extern]`. The expressions are
// read by parser_expr.go, the blocks, `let` and `if` by parser_stmt.go, and
// recover.go says what the parser does when the tokens stop making sense.
//
// Where they do, Parse reports E0201 to sink, at the token it found instead of
// the one it needed (see expected), and goes on: the place becomes an error node
// (ItemError, StmtError, TypeError or ExprError) and the parser skips the rest
// of the statement or of the item, so a mistake in a statement or in an item is
// one diagnostic. That includes a `(` that is never closed before the `}` of
// its block: the statements after it are told apart by their line breaks,
// although the line rule put no Newline between them (see block). That is what
// it promises, and it has exceptions. One input costs more the more follows it:
// a `{` that is never closed leaves everything after it inside its block, where
// each line that begins an item (with `#`, `pub` or `fn`, so an attribute on a
// line of its own costs one more than the item it belongs to) is a statement
// that cannot be read and so a diagnostic of its own, and the E0202 that names
// the cause is reported last, at the end of the file, so the sink, which keeps
// the first diag.MaxErrors errors, has dropped it once the errors reported
// before it reach that number: one E0201 for each such line after the brace, so
// diag.MaxErrors of them are enough in a file with no other mistake (see
// block). A few others cost one or two diagnostics more, and none besides the
// unclosed `{` is known to grow with what follows the mistake. For example, a
// block that is not the branch of an `if` is a statement of its own, so the
// `else` after it is a mistake of its own: a `{` on the line after the
// condition of an `if` is such a block, because the line rule has ended the
// `if` there, and the missing branch costs a diagnostic besides (see branch,
// TestRecover/if_brace_on_the_next_line). A mistake fewer than three tokens
// after the last one reported is not reported (recover.go). Each pass of its
// loops consumes at least one token or ends the loop, which is what makes them
// end.
func Parse(tb *TokenBuffer, file base.FileID, in *base.Interner, sink *diag.Sink) *Module {
	p := newParser(tb, file, in, sink)
	p.module()
	return p.m
}

// parser is the state of one call to Parse.
type parser struct {
	tb  *TokenBuffer
	pos int // index in tb.Tokens of the current token
	// index is how many of the tokens before the current one are not Newlines.
	// Mistakes are measured in these (see tokIndex).
	index int
	// last is the last token that the parser consumed, not counting Newlines,
	// so that the span of a node stops at the node's last token and not at the
	// line break after it. It is the zero Token before the first token, which
	// ends at 0.
	last Token
	m    *Module

	// chain is the state of the statement-level expression that is being read;
	// see expr in parser_expr.go.
	chain chainState
	// depth is how many levels of nesting enclose the place that is being read:
	// see MaxNesting. Unlike chain it belongs to no expression, because the
	// statements of a nested block are inside the levels around the block.
	depth int

	// errs counts the mistakes found so far, those that were reported and those
	// that were not (see detect). reported says that a mistake has been reported,
	// and lastReported is the tokIndex it was at; see admit.
	errs         int
	reported     bool
	lastReported int

	// in is only read, to tell the attribute `extern` from the others (see
	// hasExtern): the parser takes the names of everything else as the Symbols
	// the lexer interned. sink receives the diagnostics: E0201 to E0205.
	in   *base.Interner
	sink *diag.Sink
}

// ExternAttr is the name of the attribute that marks a function whose code is
// not written in Ki, as in `#[extern("ki_print_int")]`. Such a function is
// declared without a body. The parser accepts a function without a body only if
// it has this attribute, and name resolution allows the attribute only in the
// standard library.
const ExternAttr = "extern"

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
			p.last = t
			p.index++
		}
	}
	return t
}

// prev returns the last token that advance consumed, not counting Newlines:
// the token after which a closer that is missing is written. Before the first
// token it is the zero Token.
func (p *parser) prev() Token { return p.last }

// eat consumes the current token and returns it with true if it is of kind k;
// otherwise it consumes nothing and returns false.
func (p *parser) eat(k TokenKind) (Token, bool) {
	if !p.at(k) {
		return Token{}, false
	}
	return p.advance(), true
}

// found describes the token at the cursor for the second half of an E0201
// message: `newline` for a Newline, `end of file` for the Eof, and for any other
// token its text in the file between backquotes: `}`, `x`, `let`, `0xFF`. A
// literal is written as the file has it, not as its value.
//
// The text is not shortened: a string literal of fifty megabytes where an
// identifier was expected gives a message of fifty megabytes. That is deliberate.
// The message quotes the token as the file spells it, and a cut would need a
// rule that the reader of the message would have to know. If a renderer ever
// has to show such a message, it is the renderer that should cut it.
func (p *parser) found() string {
	switch t := p.tok(); t.Kind {
	case Newline:
		return "newline"
	case Eof:
		return "end of file"
	default:
		return "`" + p.tb.text(t) + "`"
	}
}

// expected reports E0201, "expected <what>, found <token>", with its primary
// label ("expected <what> here") on the token at the cursor, and consumes
// nothing. what is one of the words that the message uses: `expression`,
// `identifier`, `type`, `item`, `newline` and `string`, or a piece of
// punctuation between backquotes, such as the `(` after the name of a function
// or the `[` of an attribute. TestParseStmt/e0201_sites has a row for each
// word. The word `{` is asked for by the branches of an if and by a function
// that has no body and no `#[extern]`.
//
// It reports nothing in three cases. The token found may be an Error token: the
// lexer has reported that character or string already, and a second diagnostic
// on the same bytes would say the same mistake twice. The mistake is still
// counted (see detect), because the statement did go wrong. While the
// statement-level expression being read is being abandoned (see
// chainState.abandoned) what the parser meets on its way out of it is not a
// mistake of the program, so it is not counted either. And admit (recover.go)
// may suppress it: a mistake fewer than suppressDistance tokens after the last
// one reported is counted, and the sink hears nothing of it. The E0202 of an
// unclosed delimiter goes through the same rule (see report).
func (p *parser) expected(what string) { p.expectedWith(what, nil) }

// expectedWith is expected for a caller that adds to the diagnostic (a help
// line, a secondary label, a fix-it): it calls decorate, if there is one, on the
// E0201 before it is reported, and only if it is reported.
func (p *parser) expectedWith(what string, decorate func(diag.Diagnostic) diag.Diagnostic) {
	if p.chain.abandoned {
		return
	}
	if p.at(Error) {
		p.detect()
		return
	}
	if p.admit() {
		t := p.tok()
		d := diag.NewError(diag.E(201), "expected "+what+", found "+p.found(),
			diag.Label{Span: t.Span, Message: "expected " + what + " here"})
		if decorate != nil {
			d = decorate(d)
		}
		p.sink.Report(d)
	}
}

// expect consumes the token at the cursor and returns it with true if it is of
// kind k. Otherwise it reports E0201 for what (see expected), consumes nothing
// and returns false.
func (p *parser) expect(k TokenKind, what string) (Token, bool) {
	if t, ok := p.eat(k); ok {
		return t, true
	}
	p.expected(what)
	return Token{}, false
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
	return base.Span{File: start.File, Lo: start.Lo, Hi: max(p.last.Span.Hi, start.Lo)}
}

// module reads items until the end of the file. Every pass of the loop consumes
// at least one token (see item), so the loop ends. After an item that went
// wrong it skips to the next one (see syncItem).
func (p *parser) module() {
	for {
		// Items on different lines have a Newline between them (line rule).
		p.skipNewlines()
		if p.at(Eof) {
			return
		}
		id := p.item()
		p.m.Items = append(p.m.Items, id)
		if p.m.ItemNodes.Get(id).Kind == ItemError {
			p.syncItem()
		}
	}
}

// errorItem adds an ItemError that covers the tokens from start to the last
// one consumed, and returns its id.
func (p *parser) errorItem(start base.Span) ItemID {
	return p.m.ItemNodes.Push(Item{Kind: ItemError, Span: p.spanFrom(start)})
}

// namedErrorItem is errorItem for a function whose name, the token name, was
// read before its signature stopped making sense. The item keeps the name, so
// that name resolution knows that the function exists and a call to it is not
// reported as the call of a name that nothing defines.
func (p *parser) namedErrorItem(start base.Span, name Token) ItemID {
	return p.m.ItemNodes.Push(Item{
		Kind: ItemError, Span: p.spanFrom(start), Name: base.Symbol(name.Payload), NameSpan: name.Span,
	})
}

// item reads one item and always consumes at least one token. The grammar is
//
//	item = { attr } [ 'pub' ] 'fn' lower '(' [ param { ',' param } ] ')'
//	       [ '->' type ] [ block ]
//
// A function has a body exactly when a `{` follows its signature on the same
// line; the line rule puts a Newline in front of a `{` on the next one. Only a
// function with an `#[extern]` attribute may have none (see hasExtern). Any
// other is E0201 `{` at the token that follows the signature, and it stays an
// ItemFn without a body, with its name and its signature: the mistake is the
// missing body, so the rest of the file can still call the function. A token
// that cannot start an item, or a signature that stops making sense, is an
// ItemError and E0201: the tokens read so far are consumed, and module skips
// the rest of the item. If it goes wrong after the name, the ItemError keeps the
// name (see namedErrorItem).
func (p *parser) item() ItemID {
	start := p.tok().Span
	if !p.at(Hash) && !p.at(KwPub) && !p.at(KwFn) {
		p.expected("item")
		p.advance()
		return p.errorItem(start)
	}

	it := Item{Kind: ItemFn}
	var ok bool
	if it.Attrs, ok = p.attrs(); !ok {
		return p.errorItem(start)
	}
	_, it.Pub = p.eat(KwPub)
	// After attributes or `pub` an item is what is needed, and `fn` is the only
	// kind there is so far.
	if _, ok = p.expect(KwFn, "item"); !ok {
		return p.errorItem(start)
	}
	name, ok := p.expect(LowerIdent, "identifier")
	if !ok {
		return p.errorItem(start)
	}
	it.Name, it.NameSpan = base.Symbol(name.Payload), name.Span
	open, ok := p.expect(LParen, "`(`")
	if !ok {
		return p.namedErrorItem(start, name)
	}
	if it.Params, ok = p.params(open); !ok {
		return p.namedErrorItem(start, name)
	}
	if _, ok = p.eat(Arrow); ok {
		it.Ret, it.HasRet = p.typeExpr(), true
	}
	if p.at(LBrace) {
		it.Body, it.HasBody = p.block(), true
	} else if !p.hasExtern(it.Attrs) {
		p.expected("`{`")
	}
	it.Span = p.spanFrom(start)
	return p.m.ItemNodes.Push(it)
}

// hasExtern reports whether one of the attributes in r, a range over AttrPool,
// is called extern. Only the name counts: whether the arguments are the ones an
// extern function needs is for name resolution to say.
func (p *parser) hasExtern(r base.ListRange) bool {
	for _, a := range p.m.Attrs(r) {
		if string(p.in.Bytes(a.Name)) == ExternAttr {
			return true
		}
	}
	return false
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
// literals without interpolation. A malformed attribute is E0201 at the token
// that is not what the grammar needs: the `[`, the name, a string, a `)` or the
// `]`; or E0202, if the file ends where a `)` or a `]` was needed.
func (p *parser) attr() (Attr, bool) {
	first := p.pos
	hash := p.advance()
	bracket, ok := p.expect(LBracket, "`[`")
	if !ok {
		return Attr{}, false
	}
	name, ok := p.expect(LowerIdent, "identifier")
	if !ok {
		return Attr{}, false
	}
	a := Attr{Name: base.Symbol(name.Payload), NameSpan: name.Span}
	if paren, ok := p.eat(LParen); ok {
		for {
			arg, ok := p.expect(String, "string")
			if !ok {
				return Attr{}, false
			}
			a.Args = append(a.Args, p.tb.Strings[arg.Payload])
			if _, ok := p.eat(Comma); !ok {
				break
			}
		}
		if !p.expectClose(RParen, "`)`", paren) {
			return Attr{}, false
		}
	}
	if !p.expectClose(RBracket, "`]`", bracket) {
		return Attr{}, false
	}
	a.Span = p.spanFrom(hash.Span)
	// The tokens after the `#` that have a line break in front of them are the
	// ones that put the attribute on more than one line.
	a.OneLine = true
	for _, t := range p.tb.Tokens[first+1 : p.pos] {
		if t.NewlineBefore {
			a.OneLine = false
		}
	}
	return a, true
}

// params reads the parameters after the `(` of a function, which is open, and
// the `)`, and adds the parameters to ParamPool. It returns false, and adds
// nothing to the pool, if the list is malformed.
//
//	params = [ param { ',' param } ] ')'
//
// A comma after the last parameter is not allowed.
func (p *parser) params(open Token) (base.ListRange, bool) {
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
	if !p.expectClose(RParen, "`)`", open) {
		return base.ListRange{}, false
	}
	return appendRange(&p.m.ParamPool, list), true
}

// param reads one parameter: param = lower [ ':' type ].
func (p *parser) param() (Param, bool) {
	name, ok := p.expect(LowerIdent, "identifier")
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
// neither, it reports E0201 (`type` for a token that is not a `(`, `)` for a
// `(` that is not closed) and returns a TypeError, which covers whatever it
// consumed: nothing at all for a token that is not a `(`, so that the `)` that
// ends a parameter list is still there for the caller.
func (p *parser) typeExpr() TypeExprID {
	start := p.tok().Span
	if t, ok := p.eat(UpperIdent); ok {
		return p.m.Types.Push(TypeExpr{Kind: TypeNamed, Span: t.Span, Name: base.Symbol(t.Payload)})
	}
	if open, ok := p.eat(LParen); ok {
		if p.expectClose(RParen, "`)`", open) {
			return p.m.Types.Push(TypeExpr{Kind: TypeUnit, Span: p.spanFrom(start)})
		}
	} else {
		p.expected("type")
	}
	return p.m.Types.Push(TypeExpr{Kind: TypeError, Span: p.spanFrom(start)})
}
