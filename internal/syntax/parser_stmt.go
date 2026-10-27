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
// can hold another one. Each of them is a level of nesting, and MaxNesting
// bounds the levels (see recover.go).

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
// A statement that goes wrong is skipped to its end (syncStatement), which is
// one of those separators or a `let` that begins a line, so the loop goes on
// with the statement after it. Any other token after a statement that was read
// without mistakes is E0201 `newline`, and it is left where it is: it starts
// the next statement. The exception is a token with a line break in front of
// it: the line break is its separator, whether or not the line rule turned it
// into a Newline. The line rule does not turn it into one while a `(` that was
// never closed is the innermost open bracket, which hides every line break up
// to the `}` of the block, so the statements after a missing `)` are not
// mistakes (see syncStatement). A token on the same line as the statement still
// needs one.
//
// A missing `}` is E0202, at the `{`, and the block ends at the last token that
// was read. Until the end of the file the block reads what follows as its own
// statements, the items after it included: a `fn` is not a statement that the
// parser reads yet, so a statement that starts with a `fn`, a `pub` or a `#` is
// an E0201 `expression`, and it is skipped to its end (see syncStatement): the
// end of its line, a `;` or a `let` that begins a line, whichever comes first,
// outside the brackets that the skipped tokens opened. The body of a function
// opens one, so the skip goes on to the line of its `}`. So the cost is one
// E0201 for each line that begins an item, and not for each item: an attribute
// on a line of its own is one more. The E0202 is reported last, when the file
// ends, and the sink keeps only the first diag.MaxErrors errors in the order
// they are reported: once the errors reported before it reach that number it is
// dropped (with no other mistake in the file, diag.MaxErrors such lines after
// the brace are enough), and what the user gets is diag.MaxErrors errors and no
// word about the `}` (TestRecover/unclosed_brace_swallows_the_next_items). The
// block does not end at a `fn` instead, because the language allows a function
// inside a block (spec 5.3) and a later chapter reads it as a statement.
//
// Every pass of the loop consumes a token or ends the block: a
// statement that was read consumed one, and one that went wrong ends at a
// separator, which the code below consumes, at a `let` that begins a line, which
// starts the next statement, or at the `}` or the end of the file, which end the
// block.
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
			p.unclosed(open)
			break
		}

		errs := p.errs
		isLet := p.at(KwLet)
		var let StmtID
		var e ExprID
		if isLet {
			let = p.letStmt()
		} else {
			e = p.expr(0)
		}
		if p.errs != errs {
			p.syncStatement()
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
		default:
			// A token that is none of those needs a separator in front of it. A
			// line break is one, although the line rule put no Newline there:
			// NewlineBefore is the lexer's own answer to whether the source has
			// one between the last token and this one. That also covers the
			// token at which the skip of a statement that went wrong stopped
			// (see syncStatement): the only one that is none of the cases above
			// is a `let` that begins a line, and so has its line break.
			if !p.tok().NewlineBefore {
				p.expected("newline")
			}
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
// expression of its own, with its own count of operators (see condition). A
// branch is a block; the `else` may be followed by another if, which is the else
// branch of this one. A branch that is not a block is E0201 `{`. The condition,
// each branch and the else-if are each one level of nesting.
//
// If the condition is abandoned, for being too long (E0205) or too deep (E0204),
// the rest of the statement, the branches included, has been skipped, and the if
// is one error node that covers it all. So is an if whose `else if` is abandoned.
func (p *parser) ifExpr() ExprID {
	kw := p.advance()
	e := Expr{Kind: ExprIf}
	if !p.enter() {
		return p.errorExpr(kw.Span)
	}
	cond, abandoned := p.condition()
	p.leave()
	if abandoned {
		return p.errorExpr(kw.Span)
	}
	e.Cond = cond
	e.Then = p.branch()
	// Cannot happen today: the if was not abandoned when it began, and a block
	// gives the chain back as it found it, because each statement in it restores
	// the one it took (exprOrSkip). Kept so that a change to those rules ends in
	// an error node and not in an if that goes on reading an abandoned
	// expression. No test can reach it.
	if p.chain.abandoned {
		return p.errorExpr(kw.Span)
	}
	if _, ok := p.eat(KwElse); ok {
		e.HasElse = true
		if p.at(KwIf) {
			e.Else = p.elseIf()
		} else {
			e.Else = p.branch()
		}
		if p.chain.abandoned {
			return p.errorExpr(kw.Span)
		}
	}
	e.Span = p.spanFrom(kw.Span)
	return p.m.Exprs.Push(e)
}

// elseIf reads the if that follows an `else`, which is one level of nesting
// below the if that has the else.
func (p *parser) elseIf() ExprID {
	// Cannot happen today: the if that has this else took a level at this same
	// depth, and gave it back, so there is room for one more. Kept because a
	// caller must not call leave after an enter that refused, and a change to the
	// depth rules must not unbalance the two. No test can reach it.
	if !p.enter() {
		return p.errorExpr(p.tok().Span)
	}
	e := p.ifExpr()
	p.leave()
	return e
}

// branch reads the block of an if or of its else, which is one level of
// nesting. If the cursor is not at a `{` that is E0201, and the branch is an
// error node with an empty span there. A `{` on the next line is not the
// branch: the line rule has put a Newline in front of it, and it starts a
// statement of its own (TestRecover/if_brace_on_the_next_line).
func (p *parser) branch() ExprID {
	if !p.at(LBrace) {
		p.expected("`{`")
		return p.errorExpr(p.tok().Span)
	}
	// Cannot happen today, for the reason given in elseIf: the if took a level at
	// this depth before it came to its branches. No test can reach it.
	if !p.enter() {
		return p.errorExpr(p.tok().Span)
	}
	b := p.block()
	p.leave()
	return b
}
