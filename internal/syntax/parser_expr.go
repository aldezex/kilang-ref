package syntax

import (
	"strconv"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// MaxChain is the most binary operators and calls that one statement-level
// expression may chain (E0205). `1 + 1 + 1 + …` is a tree as deep as it has
// operators, and every pass after the parser walks it. The limit bounds the
// operators and calls of one statement, those inside its parentheses and
// arguments included, so that a statement cannot be a tree of a hundred
// thousand levels made of them. It does not bound the whole tree: the
// statements of a block inside the statement count apart, and what nests is
// bounded by MaxNesting. The depth of a tree is therefore bounded by the two
// together and not by either one alone (see PrintModule).
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
	// count is how many binary operators and calls the expression has consumed
	// so far, inside parentheses and arguments as well.
	count int
	// abandoned says that the parser gave up on the expression: its chain passed
	// MaxChain (E0205), its nesting passed MaxNesting (E0204), or an expression
	// inside it that was a condition was abandoned (see condition). The mistake
	// has been reported. From then on countChain and enter refuse, so the loops
	// of pratt stop, expected and unclosed report nothing, and exprOrSkip
	// replaces the whole expression with an error node.
	//
	// The token that was refused stays unconsumed, and the functions that were
	// reading the expression come back one after the other with a partial result
	// that exprOrSkip drops. Most of them need not look at the flag: a group wants
	// a `)` and the arguments of a call a `,` or a `)`, and that token is neither.
	// A function that reports a diagnostic of its own has to look, so as not to
	// report a mistake in an expression that is being abandoned: countChain (which
	// would count the token again), expected and unclosed. pratt after a right
	// operand, callArgs and ifExpr after its else look as well, to stop reading at
	// once and not to read on with a result that is going to be dropped. Those
	// are the readers that run. enter, and ifExpr after its then branch, look too,
	// but no test reaches them: the functions that call enter have stopped before,
	// and a block gives the chain of its if back as it found it, since each of its
	// statements restores the one it took (exprOrSkip). They stay so that a change
	// to those rules ends in an error node and not in more reading.
	abandoned bool
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
// If the expression has more than MaxChain operators and calls, or nests deeper
// than MaxNesting, expr reports E0205 or E0204 once and consumes the rest of the
// statement (syncStatement). The expression it returns is then one error node
// that covers all of it, so no pass after the parser gets a tree as deep as the
// chain or the nesting.
func (p *parser) expr(minBP int) ExprID {
	e, _ := p.exprOrSkip(minBP)
	return e
}

// exprOrSkip is expr, and it also says whether the expression was abandoned, in
// which case the cursor is at the end of the statement and the expression is an
// error node. A caller that needs more tokens after the expression (the `{`
// after the condition of an if) must not ask for them then: they were skipped
// with the rest of the statement.
func (p *parser) exprOrSkip(minBP int) (e ExprID, abandoned bool) {
	outer := p.chain
	p.chain = chainState{}
	start := p.tok().Span
	e = p.pratt(minBP)
	abandoned = p.chain.abandoned
	if abandoned {
		p.syncStatement()
		e = p.errorExpr(start)
	}
	p.chain = outer
	return e, abandoned
}

// condition reads the condition of an if. It is a statement-level expression
// with a count of its own, like the others, but not a statement: the if it is
// in is part of an expression that goes on after it. If the condition is
// abandoned, the statement was skipped to its end, with the rest of that if and
// of whatever the if is in. So the expression around the if is abandoned too:
// without that, an if inside the condition of another would look for its `{`
// and the call that holds it for its `)` in the tokens after the end of the
// statement, and report mistakes that are not in the program.
func (p *parser) condition() (e ExprID, abandoned bool) {
	e, abandoned = p.exprOrSkip(0)
	if abandoned {
		p.chain.abandoned = true
	}
	return e, abandoned
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
// whatever it has read, as soon as the count passes MaxChain or the expression is
// abandoned for any other reason (see chainState.abandoned). Every pass of the
// loop consumes a token (an operator or a `(`), so the loop ends.
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
			args, ok := p.callArgs(t)
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
		if p.chain.abandoned {
			// The token the right operand stopped at is the one that was refused.
			// It could be a comparison, and the check below would take it for a
			// chained one.
			break
		}
		lhs = p.m.Exprs.Push(Expr{Kind: ExprBinary, Span: p.spanFrom(start), Op: op, OpSpan: t.Span, Lhs: lhs, Rhs: rhs})
		if bp == bpCompare && isComparison(p.tok().Kind) {
			lhs = p.chainedComparison(start, t.Span)
		}
	}
	return lhs
}

// chainedComparison deals with a comparison operator that comes right after a
// comparison (`a < b < c`), the cursor being at the second operator. start is
// where the chain begins (the first token of its first operand) and first is
// the span of the first operator. The comparisons do not associate, so it
// reports E0203 at the second operator, once for the whole chain, with the
// first operator as its secondary label, and reads the rest of the chain, so
// that the parser goes on after it. It returns one error node for the whole
// chain, which starts at start: a tree of comparisons would make a type error
// out of a mistake that the parser has already reported.
func (p *parser) chainedComparison(start, first base.Span) ExprID {
	p.report(p.chainedDiag(start, first))
	for isComparison(p.tok().Kind) {
		if !p.countChain(p.tok().Span) {
			break
		}
		p.advance()
		p.pratt(bpCompare + 1)
	}
	return p.errorExpr(start)
}

// chainedDiag builds the E0203 for the chain that chainedComparison deals with.
//
// The fix-it writes `&& ` and the middle operand just before the second
// operator, which turns `a < b < c` into `a < b && b < c`. That is mechanical
// only when the middle operand is a single token, so it is the only case that
// gets one. The help shows the three operands and the two operators as they
// are written when each operand is a single token, and the general form
// otherwise. The operands are looked up in the tokens around the cursor,
// without reading the rest of the chain, so that the diagnostic is reported in
// its place among the others.
func (p *parser) chainedDiag(start, first base.Span) diag.Diagnostic {
	toks, at := p.tb.Tokens, p.pos
	second := toks[at]
	d := diag.NewError(diag.E(203), "comparison operators cannot be chained",
		diag.Label{Span: second.Span, Message: "second comparison here"}).
		WithSecondary(diag.Label{Span: first, Message: "first comparison here"})
	help := "use `&&`: `a < b && b < c`"

	// The middle operand is one token when the first operator is the token
	// before it.
	if at >= 2 && toks[at-2].Span == first && isOperandToken(toks[at-1].Kind) {
		mid := p.tb.text(toks[at-1])
		d = d.WithFixIt(diag.Insert(second.Span, "&& "+mid+" "))

		// The first operand is one token when it is the token before the first
		// operator and the one that begins the chain. The third is one token
		// when nothing after it would continue it (see continuesOperand).
		firstOK := at >= 3 && isOperandToken(toks[at-3].Kind) && toks[at-3].Span.Lo == start.Lo
		thirdOK := at+2 < len(toks) && isOperandToken(toks[at+1].Kind) && !continuesOperand(toks[at+2].Kind, bpCompare+1)
		if firstOK && thirdOK {
			help = "use `&&`: `" + p.tb.text(toks[at-3]) + " " + p.tb.text(toks[at-2]) + " " + mid +
				" && " + mid + " " + p.tb.text(second) + " " + p.tb.text(toks[at+1]) + "`"
		}
	}
	return d.WithHelp(help)
}

// isOperandToken reports whether a token of kind k is by itself an operand: an
// integer, a boolean or a name, the expressions that primary reads from a
// single token.
func isOperandToken(k TokenKind) bool {
	switch k {
	case Int, KwTrue, KwFalse, LowerIdent, UpperIdent:
		return true
	}
	return false
}

// continuesOperand reports whether a token of kind k, right after an operand,
// is part of the expression that pratt reads with minimum binding power minBP:
// a `(` (a call) or a binary operator at least that strong. It is the test
// that the loop of pratt applies to the token after each operand.
func continuesOperand(k TokenKind, minBP int) bool {
	_, bp, ok := binaryOp(k)
	return k == LParen || ok && bp >= minBP
}

// countChain adds the operator or call at span at to the count of the
// expression being read, and returns true. If that makes the count pass
// MaxChain, it reports E0205 at at, abandons the expression and returns false:
// the caller must stop without consuming the token. Once the expression is
// abandoned, for this reason or another, it returns false without reporting
// anything, so E0205 is reported once per expression however many functions
// meet the token.
func (p *parser) countChain(at base.Span) bool {
	if p.chain.abandoned {
		return false
	}
	p.chain.count++
	if p.chain.count <= MaxChain {
		return true
	}
	p.chain.abandoned = true
	p.report(diag.NewError(diag.E(205), "expression chain is too long",
		diag.Label{Span: at, Message: "operator or call number " + strconv.Itoa(MaxChain+1) + " of this chain"}).
		WithNote("the limit is " + strconv.Itoa(MaxChain) + " operators and calls in one expression").
		WithHelp("split the expression with `let` bindings"))
	return false
}

// errorExpr adds an ExprError that covers the tokens from start to the last one
// consumed, and returns its id. If nothing was consumed since start, its span
// is empty and sits at the start of start.
func (p *parser) errorExpr(start base.Span) ExprID {
	return p.m.Exprs.Push(Expr{Kind: ExprError, Span: p.spanFrom(start)})
}

// prefix reads an operand: a primary expression or a prefix operator (`-` or
// `!`) and the operand it applies to, which is one level of nesting. The operand
// binds tighter than every binary operator but looser than a call, so `-a * b`
// is `(-a) * b` and `-f(x)` is `-(f(x))`.
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
	if !p.enter() {
		return p.errorExpr(t.Span)
	}
	operand := p.pratt(bpPrefix)
	p.leave()
	return p.m.Exprs.Push(Expr{Kind: ExprUnary, Span: p.spanFrom(t.Span), UnOp: op, OpSpan: t.Span, Operand: operand})
}

// primary reads an expression that has no operator at its top: an integer or
// boolean literal, a name, `()`, a parenthesized expression, a block or an if.
// The other literals (strings, characters and floats) come in later chapters,
// so for now their tokens are ones that cannot start an expression. A token
// that cannot start an expression is E0201, and gives an ExprError with an
// empty span at that token, which consumes nothing, so that a `)` or a `}` that
// ends the expression is still there for the caller. Whoever calls it must
// therefore consume a token before calling it again.
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
	case LBrace:
		if !p.enter() {
			return p.errorExpr(t.Span)
		}
		b := p.block()
		p.leave()
		return b
	case KwIf:
		return p.ifExpr()
	}
	p.expected("expression")
	return p.errorExpr(t.Span)
}

// group reads what starts with a `(`: `()`, or an expression between
// parentheses. A parenthesis is not a node, so the group is its inner
// expression, with the span of that expression, which leaves the parentheses
// out. It is one level of nesting. If the `)` is missing, that is E0201 (E0202
// at the end of the file) and the group is an error node that covers what was
// read.
func (p *parser) group() ExprID {
	if !p.enter() {
		return p.errorExpr(p.tok().Span)
	}
	e := p.groupBody()
	p.leave()
	return e
}

// groupBody is group without the level of nesting, the cursor being at the `(`.
func (p *parser) groupBody() ExprID {
	open := p.advance()
	if _, ok := p.eat(RParen); ok {
		return p.m.Exprs.Push(Expr{Kind: ExprUnit, Span: p.spanFrom(open.Span)})
	}
	inner := p.pratt(0)
	if !p.expectClose(RParen, "`)`", open) {
		return p.errorExpr(open.Span)
	}
	return inner
}

// callArgs reads the arguments of a call, the cursor being just after its `(`,
// which is open, and the `)` that ends them. A comma after the last argument is
// allowed. Each argument is one level of nesting. It returns false, and adds
// nothing to ExprPool, if the arguments are cut short or do not end with a `)`,
// which is E0201 (E0202 at the end of the file), or if the expression is
// abandoned. Every pass of the loop either consumes a comma or ends the loop,
// so it ends.
//
// The arguments are collected in a slice of their own and added to the pool
// when the list is complete: the arguments that are calls add theirs first, and
// a ListRange must be contiguous.
func (p *parser) callArgs(open Token) (base.ListRange, bool) {
	var args []ExprID
	for !p.at(RParen) {
		if !p.enter() {
			return base.ListRange{}, false
		}
		arg := p.pratt(0)
		p.leave()
		if p.chain.abandoned {
			return base.ListRange{}, false
		}
		args = append(args, arg)
		if _, ok := p.eat(Comma); !ok {
			break
		}
	}
	if !p.expectClose(RParen, "`)`", open) {
		return base.ListRange{}, false
	}
	return appendRange(&p.m.ExprPool, args), true
}
