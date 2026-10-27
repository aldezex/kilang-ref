package syntax

import (
	"strconv"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// MaxChain is the most binary operators and calls that one statement-level
// expression may chain (E0205). `1 + 1 + 1 + …` is a tree as deep as it has
// operators, and every pass after the parser walks it. The limit bounds the
// operators and calls of one statement, so that a statement cannot be a tree of
// a hundred thousand levels made of them. It does not bound the nesting of
// parentheses, arguments and prefix operators: a later session adds a limit for
// those (see expr).
const MaxChain = 1024

// The binding powers of the operators (spec 5.3), loosest first: the higher the
// power, the tighter the operator holds its operands. 0 (`|>`) and 4 (`..`) are
// the powers of two operators that later sessions add, so they are left free.
// A call is postfix and binds tighter than all of these, so it has no power of
// its own: see pratt.
const (
	bpOr      = 1 // ||
	bpAnd     = 2 // &&
	bpCompare = 3 // == != < <= > >=, which do not chain
	bpAdd     = 5 // + -
	bpMul     = 6 // * / %
	bpPrefix  = 7 // prefix - and !
)

// binaryOp returns the operator that a token of kind k is and its binding
// power, or ok false if k is not a binary operator.
func binaryOp(k TokenKind) (op BinOp, bp int, ok bool) {
	switch k {
	case OrOr:
		return BinOr, bpOr, true
	case AndAnd:
		return BinAnd, bpAnd, true
	case EqEq:
		return BinEq, bpCompare, true
	case NotEq:
		return BinNe, bpCompare, true
	case Lt:
		return BinLt, bpCompare, true
	case Le:
		return BinLe, bpCompare, true
	case Gt:
		return BinGt, bpCompare, true
	case Ge:
		return BinGe, bpCompare, true
	case Plus:
		return BinAdd, bpAdd, true
	case Minus:
		return BinSub, bpAdd, true
	case Star:
		return BinMul, bpMul, true
	case Slash:
		return BinDiv, bpMul, true
	case Percent:
		return BinRem, bpMul, true
	}
	return 0, 0, false
}

// isComparison reports whether a token of kind k is one of the six comparison
// operators.
func isComparison(k TokenKind) bool {
	_, bp, ok := binaryOp(k)
	return ok && bp == bpCompare
}

// chainState is what the parser keeps about the statement-level expression it
// is reading (see expr).
type chainState struct {
	// from is the index, in the token buffer, of the first token of the
	// expression.
	from int
	// count is how many binary operators and calls the expression has consumed
	// so far, inside parentheses and arguments as well.
	count int
	// tooLong says that count passed MaxChain and E0205 was reported. From then
	// on countChain refuses every operator and call, so the loops of pratt stop,
	// and expr replaces the whole expression with an error node.
	//
	// The operator or call that passed the limit stays unconsumed, and the
	// functions that were reading the expression come back one after the other
	// with a partial result that expr drops. Most of them need not look at the
	// flag: a group wants a `)` and the arguments of a call a `,` or a `)`, and
	// that token is neither. The ones that would do something visible with it
	// look: pratt after a right operand (the token may be a comparison, which
	// would be taken for a chained one) and countChain (it would count the token
	// again). A function that reports a diagnostic of its own has to look too, so
	// as not to report a mistake in an expression that is being abandoned.
	tooLong bool
}

// expr reads an expression of a statement: the initializer of a `let`, an
// expression statement, the tail of a block. Only operators that bind at least
// as tight as minBP belong to it; a caller outside this file passes 0, which
// takes all of them.
//
// Each call of expr counts the operators and calls of its own expression
// apart from the one it may be in the middle of, so a statement inside a block
// inside an expression has its own count. The functions of this file that read
// a part of the expression (the operand of an operator, a parenthesis, an
// argument) are not entry points: they go through pratt, and add to the count
// of the expression they are in.
//
// If the expression has more than MaxChain operators and calls, expr reports
// E0205 once and consumes the rest of the statement (skipStatement). The
// expression it returns is then one error node that covers all of it, so no
// pass after the parser gets a tree as deep as the chain.
//
// What expr does not limit yet is nesting: parentheses, arguments and prefix
// operators each take a level of Go's call stack, and the limit that bounds
// them (E0204) comes in a later session.
func (p *parser) expr(minBP int) ExprID {
	outer := p.chain
	p.chain = chainState{from: p.pos}
	start := p.tok().Span
	e := p.pratt(minBP)
	if p.chain.tooLong {
		p.skipStatement()
		e = p.errorExpr(start)
	}
	p.chain = outer
	return e
}

// pratt reads an operand followed by the operators that belong to it, with the
// technique of Pratt parsing: it reads the first operand, and then, as long as
// the next token is an operator whose binding power is at least minBP, it reads
// the right operand with a minBP one higher than the operator's own, which
// leaves an operator of the same power to the loop here, and so makes the
// binary operators associate to the left.
//
// A call is a postfix operator with a power above all the others, so a `(`
// after an operand always belongs to the operand.
//
// The operators and calls it consumes are counted in p.chain, and it stops, with
// whatever it has read, as soon as the count passes MaxChain (see
// chainState.tooLong). Every pass of the loop consumes a token (an operator or a
// `(`), so the loop ends.
func (p *parser) pratt(minBP int) ExprID {
	start := p.tok().Span
	lhs := p.prefix()
	for {
		t := p.tok()
		if t.Kind == LParen {
			if !p.countChain(t.Span) {
				break
			}
			p.advance()
			args, ok := p.callArgs()
			if !ok {
				lhs = p.errorExpr(start)
				continue
			}
			lhs = p.m.Exprs.Push(Expr{Kind: ExprCall, Span: p.spanFrom(start), Callee: lhs, Args: args})
			continue
		}
		op, bp, ok := binaryOp(t.Kind)
		if !ok || bp < minBP {
			break
		}
		if !p.countChain(t.Span) {
			break
		}
		p.advance()
		rhs := p.pratt(bp + 1)
		if p.chain.tooLong {
			// The token the right operand stopped at is the one that passed the
			// limit. It could be a comparison, and the check below would take
			// it for a chained one.
			break
		}
		lhs = p.m.Exprs.Push(Expr{Kind: ExprBinary, Span: p.spanFrom(start), Op: op, OpSpan: t.Span, Lhs: lhs, Rhs: rhs})
		if bp == bpCompare && isComparison(p.tok().Kind) {
			lhs = p.chainedComparison(start)
		}
	}
	return lhs
}

// chainedComparison deals with a comparison operator that comes right after a
// comparison (`a < b < c`), the cursor being at the second operator. The
// comparisons do not associate, so it reports E0203 at that operator, once for
// the whole chain, and reads the rest of the chain, so that the parser goes on
// after it. It returns one error node for the whole chain, which starts at
// start: a tree of comparisons would make a type error out of a mistake that the
// parser has already reported.
func (p *parser) chainedComparison(start base.Span) ExprID {
	p.sink.Report(diag.NewError(diag.E(203), "comparison operators cannot be chained",
		diag.Label{Span: p.tok().Span}).
		WithHelp("use `&&`: `a < b && b < c`"))
	for isComparison(p.tok().Kind) {
		if !p.countChain(p.tok().Span) {
			break
		}
		p.advance()
		p.pratt(bpCompare + 1)
	}
	return p.errorExpr(start)
}

// countChain adds the operator or call at span at to the count of the
// expression being read, and returns true. If that makes the count pass
// MaxChain, it reports E0205 at at, marks the expression as too long and
// returns false: the caller must stop without consuming the token. Once the
// expression is marked, it returns false without reporting anything, so E0205
// is reported once per expression however many functions meet the token.
func (p *parser) countChain(at base.Span) bool {
	if p.chain.tooLong {
		return false
	}
	p.chain.count++
	if p.chain.count <= MaxChain {
		return true
	}
	p.chain.tooLong = true
	p.sink.Report(diag.NewError(diag.E(205), "expression chain is too long", diag.Label{Span: at}).
		WithNote("the limit is " + strconv.Itoa(MaxChain) + " operators and calls in one expression"))
	return false
}

// skipStatement consumes the rest of a statement-level expression that has been
// abandoned, and leaves the cursor at the token that ends the statement, which
// it does not consume: a Newline or a `;` that is not inside braces, a `}`
// that closes no brace opened in the text of the statement, or the Eof.
//
// Only braces matter. The lexer puts no Newline token inside parentheses or
// brackets (line rule), so a line break there is skipped like a blank, and a
// `}` closes the parentheses that are still open, so it ends the statement
// whatever is open inside it. The braces opened since the first token of the
// expression are counted from the tokens already consumed, so that a skip that
// starts in the middle of a block inside the statement knows what it is in. A
// later session writes the general recovery; this is the part of it that E0205
// needs.
func (p *parser) skipStatement() {
	braces := 0
	note := func(k TokenKind) {
		switch k {
		case LBrace:
			braces++
		case RBrace:
			braces--
		}
	}
	for _, t := range p.tb.Tokens[p.chain.from:p.pos] {
		note(t.Kind)
	}
	for {
		switch p.tok().Kind {
		case Eof:
			return
		case Newline, Semi, RBrace:
			if braces == 0 {
				return
			}
		}
		note(p.advance().Kind)
	}
}

// errorExpr adds an ExprError that covers the tokens from start to the last one
// consumed, and returns its id. If nothing was consumed since start, its span
// is empty and sits at the start of start.
func (p *parser) errorExpr(start base.Span) ExprID {
	return p.m.Exprs.Push(Expr{Kind: ExprError, Span: p.spanFrom(start)})
}

// prefix reads an operand: a primary expression or a prefix operator (`-` or
// `!`) and the operand it applies to. The operand binds tighter than every
// binary operator but looser than a call, so `-a * b` is `(-a) * b` and `-f(x)`
// is `-(f(x))`.
func (p *parser) prefix() ExprID {
	t := p.tok()
	if t.Kind != Minus && t.Kind != Bang {
		return p.primary()
	}
	p.advance()
	op := Neg
	if t.Kind == Bang {
		op = Not
	}
	operand := p.pratt(bpPrefix)
	return p.m.Exprs.Push(Expr{Kind: ExprUnary, Span: p.spanFrom(t.Span), UnOp: op, OpSpan: t.Span, Operand: operand})
}

// primary reads an expression that has no operator at its top: an integer or
// boolean literal, a name, `()` or a parenthesized expression. The other
// literals (strings, characters and floats) come in later chapters, so for now
// their tokens are ones that cannot start an expression. A token that cannot
// start an expression gives an ExprError with an empty span at that token, which
// consumes nothing, so that a `)` or a `}` that ends the expression is still
// there for the caller. Whoever calls it must therefore consume a token before
// calling it again.
func (p *parser) primary() ExprID {
	t := p.tok()
	switch t.Kind {
	case Int:
		p.advance()
		return p.m.Exprs.Push(Expr{Kind: ExprInt, Span: t.Span, Int: p.tb.Ints[t.Payload]})
	case KwTrue, KwFalse:
		p.advance()
		return p.m.Exprs.Push(Expr{Kind: ExprBool, Span: t.Span, Bool: t.Kind == KwTrue})
	case LowerIdent, UpperIdent:
		p.advance()
		return p.m.Exprs.Push(Expr{Kind: ExprName, Span: t.Span, Name: base.Symbol(t.Payload)})
	case LParen:
		return p.group()
	}
	return p.errorExpr(t.Span)
}

// group reads what starts with a `(`: `()`, or an expression between
// parentheses. A parenthesis is not a node, so the group is its inner
// expression, with the span of that expression, which leaves the parentheses
// out. If the `)` is missing the group is an error node that covers what was
// read.
func (p *parser) group() ExprID {
	open := p.advance()
	if _, ok := p.eat(RParen); ok {
		return p.m.Exprs.Push(Expr{Kind: ExprUnit, Span: p.spanFrom(open.Span)})
	}
	inner := p.pratt(0)
	if _, ok := p.eat(RParen); !ok {
		return p.errorExpr(open.Span)
	}
	return inner
}

// callArgs reads the arguments of a call, the cursor being just after its `(`,
// and the `)` that ends them. A comma after the last argument is allowed. It
// returns false, and adds nothing to ExprPool, if the arguments are cut short
// or do not end with a `)`. Every pass of the loop either consumes a comma or
// ends the loop, so it ends.
//
// The arguments are collected in a slice of their own and added to the pool
// when the list is complete: the arguments that are calls add theirs first, and
// a ListRange must be contiguous.
func (p *parser) callArgs() (base.ListRange, bool) {
	var args []ExprID
	for !p.at(RParen) {
		args = append(args, p.pratt(0))
		if _, ok := p.eat(Comma); !ok {
			break
		}
	}
	if _, ok := p.eat(RParen); !ok {
		return base.ListRange{}, false
	}
	return appendRange(&p.m.ExprPool, args), true
}
