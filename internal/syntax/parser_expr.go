package syntax

import (
	"bytes"
	"math"
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// MaxChain is the most binary operators, calls and accesses (`.x`, `.0`) that one
// statement-level expression may chain (E0205). `1 + 1 + 1 + …` is a tree as deep
// as it has operators, and so is `t.0.0.0.0…`, and every pass after the parser
// walks them. The limit bounds the operators, calls and accesses of one
// statement, those inside its parentheses, arguments and tuples included, so that
// a statement cannot be a tree of a hundred thousand levels made of them. An
// access like `t.0.1`, which the lexer reads as a `.` and a Float, counts for
// two, because it is two nodes. It does not bound the whole tree: the
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
	// count is how many binary operators, calls and accesses (the `.` is an
	// operator too) the expression has consumed so far, inside parentheses and
	// arguments as well.
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
	// The `{` after the condition is the block of the if, so `Name {` in the
	// condition, outside any delimiter, is a name and not a record literal; unless
	// what follows the `{` is how no block starts, and then the literal is read and
	// asked to have parentheses (see literalInHead).
	outer := p.noRecord
	p.noRecord = true
	e, abandoned = p.exprOrSkip(0)
	p.noRecord = outer
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
// after an operand always belongs to the operand; so is a `.` and what follows it
// (see afterDot).
//
// The operators, calls and accesses it consumes are counted in p.chain, and it
// stops, with whatever it has read, as soon as the count passes MaxChain or the
// expression is abandoned for any other reason (see chainState.abandoned). Every
// pass of the loop consumes a token (an operator, a `(` or a `.`), so the loop
// ends.
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
			// The arguments are inside parentheses: a record literal may start
			// in them, even in the condition of an if.
			outerNoRecord := p.noRecord
			p.noRecord = false
			args, ok := p.callArgs(t)
			p.noRecord = outerNoRecord
			if !ok {
				lhs = p.errorExpr(start)
				continue
			}
			lhs = p.m.Exprs.Push(Expr{Kind: ExprCall, Span: p.spanFrom(start), OpSpan: p.spanFrom(t.Span), Callee: lhs, Args: args})
			continue
		}
		if t.Kind == Dot {
			if !p.countChain(t.Span) {
				break
			}
			p.advance()
			lhs = p.afterDot(start, lhs)
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
// integer, a float, a boolean or a name, the expressions that primary reads from
// a single token.
func isOperandToken(k TokenKind) bool {
	switch k {
	case Int, Float, KwTrue, KwFalse, LowerIdent, UpperIdent:
		return true
	}
	return false
}

// continuesOperand reports whether a token of kind k, right after an operand,
// is part of the expression that pratt reads with minimum binding power minBP:
// a `(` (a call), a `.` (an access) or a binary operator at least that strong.
// It is the test that the loop of pratt applies to the token after each operand.
func continuesOperand(k TokenKind, minBP int) bool {
	_, bp, ok := binaryOp(k)
	return k == LParen || k == Dot || ok && bp >= minBP
}

// countChain adds the operator or call at span at to the count of the
// expression being read, and returns true. The `.` of a field access or a tuple
// index is an operator, so it is counted as one, and E0205 names it so. If that makes the count pass
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

// primary reads an expression that has no operator at its top: an integer,
// float or boolean literal, a name, a record literal, `()`, a parenthesized
// expression or a tuple, a block or an if. The other literals (strings and characters) come in later chapters, so
// for now their tokens are ones that cannot start an expression. A token
// that cannot start an expression is E0201, and gives an ExprError with an
// empty span at that token, which consumes nothing, so that a `)` or a `}` that
// ends the expression is still there for the caller. Whoever calls it must
// therefore consume a token before calling it again.
func (p *parser) primary() ExprID {
	t := p.tok()
	switch t.Kind {
	case Int:
		p.advance()
		// An Int that the lexer reported (E0105 for a number that does not fit,
		// E0107 for one that is malformed) holds the value 0, and that 0 is not the
		// number written: the literal is an error node, so the lexer's error is the
		// only one and nothing downstream compares a value that is not there. A zero
		// that is written, `0`, `0x0` or `0_0`, was not reported and is the Int.
		if p.tb.reportedNumber(t) {
			return p.errorExpr(t.Span)
		}
		return p.m.Exprs.Push(Expr{Kind: ExprInt, Span: t.Span, Int: p.tb.Ints[t.Payload]})
	case Float:
		p.advance()
		return p.m.Exprs.Push(Expr{Kind: ExprFloat, Span: t.Span, Float: p.tb.Floats[t.Payload]})
	case KwTrue, KwFalse:
		p.advance()
		return p.m.Exprs.Push(Expr{Kind: ExprBool, Span: t.Span, Bool: t.Kind == KwTrue})
	case LowerIdent, UpperIdent:
		p.advance()
		// An uppercase name and a `{` on the same line start a record literal
		// (a `{` on the next line is not on it: the line rule has put a Newline
		// in front), except in the head of an if, where the `{` is the block
		// (see condition). What can only be a literal there, because no block
		// starts like it, is read as one and reported (see literalInHead).
		if t.Kind == UpperIdent && p.at(LBrace) {
			if !p.noRecord {
				return p.record(t)
			}
			if p.startsFields() {
				return p.literalInHead(t)
			}
		}
		// A lowercase name and a `{` that starts fields is a record literal whose
		// name is spelled wrong (`point { x: 1.0 }`): no block starts like that, so
		// it is read as the literal it is, and the name that no type has is the
		// one error, E0406, with the type that it looks like (see resolve). Read
		// as a name and a block it would be an unknown name, a `{` that is not on
		// a new line and an unknown name for the field. Not in the head of an
		// `if`, where the literal is already a mistake.
		if t.Kind == LowerIdent && p.at(LBrace) && !p.noRecord && p.startsFields() {
			return p.record(t)
		}
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

// startsFields says whether what follows the `{` at the cursor is how the fields
// of a record literal start, and not how the statements of a block do: a field
// name and a `:` or a `,` (`{ x: 1`, `{ x, y`), or the `..` of a base. No block
// starts with any of them, an expression is not followed by a colon or a comma
// and `..` is not an operator, so there is no `if Name { x }` it can be mistaken
// for; `{ x }` and `{ }` are left to the block.
func (p *parser) startsFields() bool {
	return p.kindAt(1) == DotDot || p.kindAt(1) == LowerIdent && (p.kindAt(2) == Colon || p.kindAt(2) == Comma)
}

// kindAt is the kind of the token n places after the cursor: Eof past the end.
func (p *parser) kindAt(n int) TokenKind {
	return p.tb.Tokens[min(p.pos+n, len(p.tb.Tokens)-1)].Kind
}

// literalInHead reads the record literal that name starts, the cursor being at its
// `{`, in the head of an if, where a literal needs parentheses (the `{` of the
// block is the one the head ends with). The literal is read as it is written, so
// that what is in it is checked, and the mistake is one E0201 on it, with the edit
// that wraps it, instead of the errors that reading the block would give: the name
// before the `{` is no constructor (E0306), `x` is an unknown name (E0301), `:` is
// not a newline (E0201) and the `()` that the block would have is not a record
// (E0412), three of them false. A literal that is broken by itself has said so,
// and says no more.
func (p *parser) literalInHead(name Token) ExprID {
	id := p.record(name)
	lit := p.m.Exprs.Get(id)
	if lit.Kind == ExprError {
		return id
	}
	n := lit.Span
	p.report(diag.NewError(diag.E(201), "a record literal in the head of an `if` needs parentheses",
		diag.Label{Span: n, Message: "this literal needs parentheses"}).
		WithHelp("write the literal between parentheses: after a name, a `{` starts the block of the `if`").
		WithFixIt(diag.Insert(n, "(")).
		WithFixIt(diag.Insert(base.Span{File: n.File, Lo: n.Hi, Hi: n.Hi}, ")")))
	return id
}

// group reads what starts with a `(`: `()`, an expression between parentheses,
// or a tuple, `(1, true)`. A parenthesis is not a node, so the group is its inner
// expression, with the span of that expression, which leaves the parentheses
// out; a tuple is a node, with the span from its `(` to its `)`. It is one level
// of nesting, and a record literal may start inside it, even in the condition of
// an if. If the `)` is missing, that is E0201 (E0202 at the end of the file) and
// the group is an error node that covers what was read.
func (p *parser) group() ExprID {
	if !p.enter() {
		return p.errorExpr(p.tok().Span)
	}
	outerNoRecord := p.noRecord
	p.noRecord = false
	e := p.groupBody()
	p.noRecord = outerNoRecord
	p.leave()
	return e
}

// groupBody is group without the level of nesting, the cursor being at the `(`.
// What follows the first expression says what the group is: a `)` ends a
// parenthesized expression, and a `,` makes it a tuple of two or more elements,
// each one level of nesting like the argument of a call. There is no tuple of
// one element (`(1,)`: the missing second element is E0201 `expression`), and a
// comma after the last element is that mistake too.
func (p *parser) groupBody() ExprID {
	open := p.advance()
	if _, ok := p.eat(RParen); ok {
		return p.m.Exprs.Push(Expr{Kind: ExprUnit, Span: p.spanFrom(open.Span)})
	}
	inner := p.pratt(0)
	if p.at(Comma) {
		return p.tuple(open, inner)
	}
	if !p.expectClose(RParen, "`)`", open) {
		return p.errorExpr(open.Span)
	}
	return inner
}

// tuple reads the rest of a tuple, the cursor being at the first `,`, after the
// first element first of the tuple that open starts, and the `)`. The elements
// are collected in a slice of their own and added to the pool when the list is
// complete, as callArgs does, and it returns an error node, and adds nothing to
// the pool, if the expression is abandoned, the `)` is missing or an element
// could not be read. A tuple with an element that is an error node (the one that
// a comma before the `)` leaves, `(1, 2,)`) has not the arity that was written,
// so it would be a second error wherever its type is compared; the error node
// has the error type, which says nothing, as it does in a tuple type
// (see parenType). The rest of the elements and the `)` are read all the same,
// so that the mistake is reported once and the tokens after it are not.
func (p *parser) tuple(open Token, first ExprID) ExprID {
	elems := []ExprID{first}
	unreadable := p.m.Exprs.Get(first).Kind == ExprError
	for {
		if _, ok := p.eat(Comma); !ok {
			break
		}
		if !p.enter() {
			return p.errorExpr(open.Span)
		}
		e := p.pratt(0)
		p.leave()
		if p.chain.abandoned {
			return p.errorExpr(open.Span)
		}
		unreadable = unreadable || p.m.Exprs.Get(e).Kind == ExprError
		elems = append(elems, e)
	}
	if !p.expectClose(RParen, "`)`", open) || unreadable {
		return p.errorExpr(open.Span)
	}
	return p.m.Exprs.Push(Expr{Kind: ExprTuple, Span: p.spanFrom(open.Span), Args: base.AppendRange(&p.m.ExprPool, elems)})
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
	return base.AppendRange(&p.m.ExprPool, args), true
}

// afterDot reads what follows a `.` that the loop of pratt has consumed: the
// name of a field, `p.x`, or a tuple index, `t.0`, and returns the access that
// it makes with target, the operand before the `.`. start is where the operand
// starts. A name is a lowercase one; the name of a type, or a keyword, is not a
// field.
//
// The lexer reads the dots and the numbers of `n.0.1` as a `.` and the Float
// `0.1`, and of `n.1.1.0` as `.`, the Float `1.1`, `.` and the Int `0`. So a
// Float token after a `.` is two indexes when it is two runs of digits with a
// dot between them (see indexDigits). Anything else after the `.` that is a
// number, `t.1e5`, `t.1_0`, `t.0x1`, `t.01`, is not an index, and neither is
// any token that is not a number or a name: it is E0201 `field name or tuple
// index`, and an error node that consumes nothing, as primary does.
func (p *parser) afterDot(start base.Span, target ExprID) ExprID {
	t := p.tok()
	switch t.Kind {
	case LowerIdent:
		p.advance()
		return p.m.Exprs.Push(Expr{
			Kind: ExprField, Span: p.spanFrom(start), Target: target, Name: base.Symbol(t.Payload), NameSpan: t.Span,
		})
	case Int, Float:
		// A number that the lexer reported (E0105 for an Int that does not fit in 64
		// bits, E0107 for one that is malformed, E0109 for a Float out of range) is
		// the lexer's mistake, and the access on it an error node: with no index to
		// be out of range and no claim that these digits are not an index, the same
		// digits are not the subject of a second error.
		if p.tb.reportedNumber(t) {
			p.advance()
			return p.errorExpr(start)
		}
		if parts, ok := indexDigits(p.tb.text(t)); ok {
			p.advance()
			return p.tupleIndexes(start, target, t.Span, parts)
		}
		// Digits with a leading zero are a number that says what it is, and the one
		// way to write the index is without the zeros: the help says so, with the
		// edit.
		if fixed, ok := withoutLeadingZeros(p.tb.text(t)); ok {
			p.expectedWith("field name or tuple index", func(d diag.Diagnostic) diag.Diagnostic {
				return d.WithHelp("write the index without leading zeros: `" + fixed + "`").WithFixIt(diag.Replace(t.Span, fixed))
			})
			return p.errorExpr(start)
		}
	}
	p.expected("field name or tuple index")
	return p.errorExpr(start)
}

// withoutLeadingZeros returns the number that text, the text of a number token
// read after a `.`, writes without the zeros that come before its digits (`01`
// is `1`, `00` is `0`, `0.01` is `0.1`), and ok true only if there were some and
// every other character is a digit or the one dot: `1_0`, `0x1` and `1e5` are
// not an index written with zeros, and have no repair.
func withoutLeadingZeros(text string) (fixed string, ok bool) {
	runs := strings.Split(text, ".")
	if len(runs) > 2 {
		return "", false
	}
	for i, run := range runs {
		if run == "" || strings.Trim(run, "0123456789") != "" {
			return "", false
		}
		if trimmed := strings.TrimLeft(run, "0"); trimmed == "" {
			runs[i] = "0"
		} else {
			runs[i] = trimmed
		}
	}
	fixed = strings.Join(runs, ".")
	return fixed, fixed != text
}

// indexDigits returns the indexes that the text of a number token, read after a
// `.`, writes: one for a run of digits (`0`), two for two runs of digits with a
// dot between them (`0.1`), and ok false for anything else. A run is the digits
// of an index as a person writes it: no underscore, no prefix and no exponent
// (the text is the token as it is in the file, where the lexer has accepted
// `1_0`, `0x1` and `1e5`), and no leading zero, so that the text of an index is
// the number written the one way.
func indexDigits(text string) (parts []string, ok bool) {
	parts = strings.Split(text, ".")
	if len(parts) > 2 {
		return nil, false
	}
	for _, d := range parts {
		if d == "" || d[0] == '0' && len(d) > 1 || strings.Trim(d, "0123456789") != "" {
			return nil, false
		}
	}
	return parts, true
}

// tupleIndexes adds the accesses that the digit runs parts, the text of the
// number token at tok, make after target: one node for each, the second with the
// first as its target. Each has the span from start to its own digits, and its
// digits as OpSpan. Each counts in the chain, as a `.` does (the first was
// counted by pratt): if the second makes it pass MaxChain, the first is what
// there is.
func (p *parser) tupleIndexes(start base.Span, target ExprID, tok base.Span, parts []string) ExprID {
	lo := tok.Lo
	for i, digits := range parts {
		if i > 0 && !p.countChain(tok) {
			break
		}
		hi := lo + uint32(len(digits))
		// An index that does not fit in 32 bits is out of range of every tuple, so
		// it stays at the largest number that fits, and what was written is in
		// Name.
		index, err := strconv.ParseUint(digits, 10, 32)
		if err != nil {
			index = math.MaxUint32
		}
		target = p.m.Exprs.Push(Expr{
			Kind: ExprTupleIndex, Span: base.Span{File: start.File, Lo: start.Lo, Hi: hi}, Target: target,
			Index: uint32(index), Name: p.in.InternString(digits), OpSpan: base.Span{File: tok.File, Lo: lo, Hi: hi},
		})
		lo = hi + 1 // the dot between two runs
	}
	return target
}

// record reads a record literal, the cursor being at its `{` and name the type's
// name before it:
//
//	record = name '{' [ finit { ',' finit } [ ',' ] ] [ '..' expr ] '}'
//	finit  = lower ':' expr | lower
//
// The name is uppercase. A lowercase one is read all the same, where the `{` is
// followed by fields (see primary), and it is name resolution that says no type
// has it.
//
// A field that is only a name is punning, `{ x, y }`: it gives the field x the
// value of the variable x, which is an ExprName with the span of the name. A `..`
// and the expression after it, the base, end the list: `Point { x: 3.0, ..p }`
// (and `Point { ..p }`, which gives no field). A line break is only space: the
// lexer puts a Newline after a value that ends its line, because it cannot tell
// the braces of a record from those of a block, so one can come before a `,` or
// the `}` (there is none after the `{` or after a `,`, which cannot end a line).
// Every field value is one level of nesting, like the argument of a call, and a
// record literal may start in it, even in the condition of an if.
//
// A mistake inside the braces is E0201 at the token found, and the rest of the
// literal, up to its `}`, is skipped (skipRecord): if it were not, the `}` of
// the literal would be taken for the end of the block around it. The literal is
// then an error node, which like every error node of the parser holds nothing: it
// covers what was read, the field values read before the mistake are not in the
// tree, and no pass after the parser sees the names in them. The file ending
// before the `}` is E0202, at the `{`.
//
// A `,` that is missing between two fields (a `;` where it goes, or the next field
// on the line below) is one E0201 too, but nothing is skipped: the fields after
// it are read as if the `,` were there, so that what is wrong with them is
// reported, and the literal is an error node when its `}` is reached, as for any
// other mistake in it (see missingComma).
func (p *parser) record(name Token) ExprID {
	open := p.advance()
	outerNoRecord := p.noRecord
	p.noRecord = false
	defer func() { p.noRecord = outerNoRecord }()

	e := Expr{Kind: ExprRecord, Name: base.Symbol(name.Payload), NameSpan: name.Span}
	var fields []FieldInit
	// prevComma is the comma after the last field that was read, if there is one.
	var prevComma base.Span
	// broken says that a `,` was missing between two fields.
	broken := false
	for !p.at(RBrace) && !p.at(Eof) {
		if _, ok := p.eat(DotDot); ok {
			// A base that is too deep, or that is abandoned, leaves the expression
			// abandoned, and then the E0202 of skipRecord is not reported (see
			// unclosed): the end of the file is reported at the `{` of the block
			// around the literal. So only a field that cannot be read, or a missing
			// `}`, makes skipRecord say which `{` is open.
			if !p.enter() {
				return p.skipRecord(name, open)
			}
			e.Base, e.HasBase = p.pratt(0), true
			p.leave()
			if p.chain.abandoned {
				return p.skipRecord(name, open)
			}
			p.skipNewlines()
			break
		}
		f, ok := p.fieldInit()
		if !ok {
			return p.skipRecord(name, open)
		}
		end := p.last.Span.Hi
		p.skipNewlines()
		comma, ok := p.eat(Comma)
		if !ok {
			// The last field: with the comma before it, if it has one, so that
			// what is left is a list that ends as it did.
			f.Removal = base.Span{File: f.Span.File, Lo: f.Span.Lo, Hi: end}
			if prevComma != (base.Span{}) {
				f.Removal.Lo = prevComma.Lo
			}
			f.RemovalOneLine = p.sameLine(f.Removal)
			fields = append(fields, f)
			if !p.missingComma(f.Span) {
				break
			}
			// Another field comes after this one, with no `,` between them: it has
			// been reported, and the next is read as if the `,` were there. The
			// literal is a mistake all the same, and an error node at the end.
			broken = true
			prevComma = base.Span{}
			continue
		}
		// A field that has a comma after it takes it, and the blanks after it; one
		// that is alone on its line takes the blanks before it too.
		f.Removal = p.ownLine(base.Span{File: f.Span.File, Lo: f.Span.Lo, Hi: p.afterBlanks(comma.Span.Hi)})
		f.RemovalOneLine = p.sameLine(f.Removal)
		fields = append(fields, f)
		prevComma = comma.Span
	}
	if !p.expectClose(RBrace, "`}`", open) {
		return p.skipRecord(name, open)
	}
	if broken {
		return p.errorExpr(name.Span)
	}
	e.Span = p.spanFrom(name.Span)
	e.OpSpan = p.spanFrom(open.Span)
	e.Fields = base.AppendRange(&p.m.FieldInitPool, fields)
	return p.m.Exprs.Push(e)
}

// missingComma is called where a field of a record literal ended with no `,`, the
// cursor after any line breaks, and prev is the span of that field's name. If what
// is there is a `;` or the start of another field (see atField, or the `..` of a
// base), the field list goes on and what is missing is the `,` between the two: it
// reports that, one E0201 on the token found with the help and the edit that
// writes the `,` (after the last token read, or in place of the `;`), and returns
// true, having consumed the `;`. A `;` is what a type body takes as a separator and
// a line break is what it takes too, which is where the slip comes from. Any other
// token ends the list: whatever it is, the `}` is the one that is missing (see
// expectClose), or the field is the last.
func (p *parser) missingComma(prev base.Span) bool {
	semi := p.at(Semi)
	if !semi && !p.at(DotDot) && !p.atField(prev) {
		return false
	}
	found := p.tok()
	end := p.prev().Span.Hi
	p.expectedWith("`,` or `}`", func(d diag.Diagnostic) diag.Diagnostic {
		d = d.WithHelp("separate the fields with `,`")
		if semi {
			return d.WithFixIt(diag.Replace(found.Span, ","))
		}
		return d.WithFixIt(diag.Insert(base.Span{File: p.m.File, Lo: end, Hi: end}, ","))
	})
	if semi {
		// No line break comes after a `;` to be skipped: it cannot end an expression
		// (see the line rule).
		p.advance()
	}
	return true
}

// atField says whether the cursor is at the start of a field that comes after
// another, whose name is at prev, with no `,` between them. A name followed by a
// `:` is one (`y: 2.0`): nothing else starts like that. A name that is the whole
// field, a pun (`y`), which a type body written without the types looks like, is
// told by what follows it (the `,` or the `}` that ends it, a line break, the `..`
// of a base or the name of another pun), and by where it is: on the line of the
// previous field, or on a line of its own straight under the name of the
// previous one. The same name on a line of its own anywhere else is not taken
// for a field, because it is as likely a statement after a literal whose `}` is
// missing (`let p = P { x: 1` and then `p`), and that is the mistake which
// expectClose reports, at the `{`. Any other token after the name is not how a
// field goes on (`y + 1` is an expression).
func (p *parser) atField(prev base.Span) bool {
	if !p.at(LowerIdent) {
		return false
	}
	switch p.kindAt(1) {
	case Colon:
		return true
	case Comma, RBrace, Newline, DotDot, LowerIdent:
		return !p.tok().NewlineBefore || p.sameColumn(prev.Lo, p.tok().Span.Lo)
	}
	return false
}

// sameColumn reports whether the bytes at a and b are the same number of bytes
// after the start of their lines.
func (p *parser) sameColumn(a, b uint32) bool {
	return a-p.lineStart(a) == b-p.lineStart(b)
}

// lineStart returns the offset where the line that holds byte at begins.
func (p *parser) lineStart(at uint32) uint32 {
	return uint32(bytes.LastIndexByte(p.tb.src[:at], '\n') + 1)
}

// fieldInit reads one field of a record literal, `x: 1.0` or `x`, without its
// Removal. It returns false if the field could not be read: there is no name
// (E0201 `identifier`), or the expression is abandoned.
func (p *parser) fieldInit() (FieldInit, bool) {
	name, ok := p.eat(LowerIdent)
	if !ok {
		// A name that starts in uppercase is what the author writes after the
		// body of a type in which variants do: the help says the rule and the edit
		// writes the same word with a small letter.
		t := p.tok()
		p.expectedWith("identifier", func(d diag.Diagnostic) diag.Diagnostic {
			if t.Kind != UpperIdent {
				return d
			}
			text := p.tb.text(t)
			return d.WithHelp("a field name starts with a lowercase letter").
				WithFixIt(diag.Replace(t.Span, string(text[0]-'A'+'a')+text[1:]))
		})
		return FieldInit{}, false
	}
	f := FieldInit{Name: base.Symbol(name.Payload), Span: name.Span}
	if _, ok := p.eat(Colon); !ok {
		// Punning: the value is the variable that has the name of the field.
		f.Pun = true
		f.Value = p.m.Exprs.Push(Expr{Kind: ExprName, Span: name.Span, Name: f.Name})
		return f, true
	}
	if !p.enter() {
		return FieldInit{}, false
	}
	f.Value = p.pratt(0)
	p.leave()
	return f, !p.chain.abandoned
}

// skipRecord consumes the rest of the record literal that name and open, its `{`,
// begin, up to and including the `}` that closes it, and returns the error node
// that the literal is. The braces that the skipped tokens open are matched with
// their own `}`. If the file ends first, that is E0202 at the `{`.
func (p *parser) skipRecord(name, open Token) ExprID {
	var nested bracketStack
	for {
		switch k := p.tok().Kind; {
		case k == Eof:
			p.unclosed(open)
			return p.errorExpr(name.Span)
		case k == RBrace && nested.braces == 0:
			p.advance()
			return p.errorExpr(name.Span)
		default:
			trackBracket(&nested, k)
			p.advance()
		}
	}
}

// afterBlanks returns where the run of spaces and tabs that starts at byte lo
// ends: lo itself if no blank comes there.
func (p *parser) afterBlanks(lo uint32) uint32 {
	src := p.tb.src
	for int(lo) < len(src) && (src[lo] == ' ' || src[lo] == '\t') {
		lo++
	}
	return lo
}

// ownLine widens removal, the text that ends a field with its comma, to the start
// of its line if the field is alone on it: only blanks come before the field, and
// nothing but the line break comes after removal. What is left of the line when
// the edit deletes it is then an empty line and not one of blanks. An edit stays
// inside one line, so the line break itself is not part of it.
func (p *parser) ownLine(removal base.Span) base.Span {
	src := p.tb.src
	lo := int(removal.Lo)
	for lo > 0 && (src[lo-1] == ' ' || src[lo-1] == '\t') {
		lo--
	}
	if lo > 0 && src[lo-1] != '\n' {
		return removal
	}
	if hi := int(removal.Hi); hi < len(src) && src[hi] != '\n' && src[hi] != '\r' {
		return removal
	}
	removal.Lo = uint32(lo)
	return removal
}

// sameLine reports whether no line break lies inside sp, so an edit over it
// stays on one line.
func (p *parser) sameLine(sp base.Span) bool {
	return bytes.IndexByte(p.tb.src[sp.Lo:sp.Hi], '\n') < 0
}
