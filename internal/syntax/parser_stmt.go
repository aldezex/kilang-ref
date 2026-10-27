package syntax

import "github.com/aldezex/kilang/internal/base"

// This file reads blocks, `let` and `if`. The grammar (spec 5.3) is
//
//	block = '{' { stmt sep } [ expr ] '}'        sep = Newline | ';'
//	stmt  = 'let' lower [ ':' type ] '=' expr | expr
//	if    = 'if' expr block [ 'else' ( block | if ) ]
//
// Blocks and ifs are expressions (primary in parser_expr.go calls block and
// ifExpr), so they can be operands, arguments and initializers, and a block
// can hold another one. Nothing bounds that nesting yet: each level takes a
// level of Go's call stack, and the limit that bounds them (E0204) comes in a
// later session.

// block reads a block, the cursor being at its `{`, and returns it as an
// ExprBlock. Each statement is read by expr(0), so it has the count of
// operators of its own (see expr).
//
// What follows a statement decides what it is:
//
//   - a `;` makes it a statement, and so does a Newline unless the next token is
//     the `}`;
//   - a `}` right after an expression, or after a Newline, makes it the tail of
//     the block, the expression that gives the block its value, and so does the
//     end of the file (the missing `}` is reported once, at the top of the loop);
//   - a `let` is always a statement, and needs no separator in front of the `}`.
//
// Any other token after a statement is E0201 `newline`, and it is left where it
// is: it starts the next statement. The exception is a token that cannot start
// a statement either, so that nothing was read for it (its E0201 is
// "expected expression"): it is dropped, which is what makes every pass of the
// loop consume a token or end it.
//
// A missing `}` is E0201, and the block ends at the last token that was read.
func (p *parser) block() ExprID {
	open := p.advance()
	var stmts []StmtID
	var tail ExprID
	hasTail := false
	for {
		if _, ok := p.eat(RBrace); ok {
			break
		}
		if p.at(Eof) {
			p.expected("`}`")
			break
		}

		before := p.pos
		isLet := p.at(KwLet)
		var let StmtID
		var e ExprID
		if isLet {
			let = p.letStmt()
		} else {
			e = p.expr(0)
		}

		isTail := false
		switch {
		case p.at(Semi):
			p.advance()
		case p.at(Newline):
			p.skipNewlines()
			isTail = !isLet && p.at(RBrace)
		case p.at(RBrace):
			isTail = !isLet
		case p.at(Eof):
			// The top of the loop reports the missing `}`.
			isTail = !isLet
		case p.pos == before:
			p.advance()
		default:
			p.expected("newline")
		}

		switch {
		case isTail:
			tail, hasTail = e, true
		case isLet:
			stmts = append(stmts, let)
		default:
			stmts = append(stmts, p.m.Stmts.Push(Stmt{Kind: StmtExpr, Span: p.m.Exprs.Get(e).Span, Expr: e}))
		}
	}
	// The statements are collected in a slice of their own and added to the
	// pool when the list is complete: the blocks inside them add theirs first,
	// and a ListRange must be contiguous.
	return p.m.Exprs.Push(Expr{
		Kind: ExprBlock, Span: p.spanFrom(open.Span),
		Stmts: appendRange(&p.m.StmtPool, stmts), Tail: tail, HasTail: hasTail,
	})
}

// letStmt reads a `let` statement, the cursor being at the `let`:
//
//	'let' lower [ ':' type ] '=' expr
//
// Its span runs from the `let` to the end of the initializer, and leaves out
// the separator that follows. If the name is missing (E0201 `identifier`) the
// statement is a StmtError that covers the `let`. If the `=` is missing (E0201
// `=`) it is still a StmtLet that binds the name, with an error node for the
// initializer, an empty one at the token found instead of the `=`: the
// statements after it can use the name without more errors about it. No
// initializer is read then, because the tokens that follow may well be the end
// of the block.
func (p *parser) letStmt() StmtID {
	kw := p.advance()
	name, ok := p.expect(LowerIdent, "identifier")
	if !ok {
		return p.m.Stmts.Push(Stmt{Kind: StmtError, Span: kw.Span})
	}
	s := Stmt{Kind: StmtLet, Name: base.Symbol(name.Payload), NameSpan: name.Span}
	if _, ok := p.eat(Colon); ok {
		s.Type, s.HasType = p.typeExpr(), true
	}
	if _, ok := p.expect(Eq, "`=`"); ok {
		s.Expr = p.expr(0)
	} else {
		s.Expr = p.errorExpr(p.tok().Span)
	}
	s.Span = p.spanFrom(kw.Span)
	return p.m.Stmts.Push(s)
}

// ifExpr reads an if, the cursor being at the `if`. The condition is an
// expression of its own, with its own count of operators (see expr). A branch is
// a block; the `else` may be followed by another if, which is the else branch
// of this one. A branch that is not a block is E0201 `{`.
//
// If the condition is abandoned for being too long (E0205), the rest of the
// statement, the branches included, has been skipped, and the if is one error
// node that covers it all.
func (p *parser) ifExpr() ExprID {
	kw := p.advance()
	e := Expr{Kind: ExprIf}
	cond, abandoned := p.exprOrSkip(0)
	if abandoned {
		return p.errorExpr(kw.Span)
	}
	e.Cond = cond
	e.Then = p.branch()
	if _, ok := p.eat(KwElse); ok {
		e.HasElse = true
		if p.at(KwIf) {
			e.Else = p.ifExpr()
		} else {
			e.Else = p.branch()
		}
	}
	e.Span = p.spanFrom(kw.Span)
	return p.m.Exprs.Push(e)
}

// branch reads the block of an if or of its else. If the cursor is not at a
// `{` that is E0201, and the branch is an error node with an empty span there.
func (p *parser) branch() ExprID {
	if p.at(LBrace) {
		return p.block()
	}
	p.expected("`{`")
	return p.errorExpr(p.tok().Span)
}
