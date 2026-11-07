package syntax

import (
	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// This file reads blocks, `let`, `if` and `match`. The grammar (spec 5.3) is
//
//	block = '{' { stmt sep } [ expr ] '}'        sep = Newline | ';'
//	stmt  = 'let' pat [ ':' type ] '=' expr | expr
//	if    = 'if' expr block [ 'else' ( block | if ) ]
//	match = 'match' expr '{' { pat [ 'if' expr ] '=>' expr sep } '}'
//
// where a pat is read by parser_pat.go, and a `let` whose pattern is a plain name
// is the statement of Part I, with the name in Stmt.Name. Blocks, ifs and matches
// are expressions (primary in parser_expr.go calls block, ifExpr and matchExpr),
// so they can be operands, arguments and initializers, and a block can hold
// another one. Each of them is a level of nesting, and MaxNesting bounds the
// levels (see recover.go).

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
	// A block is inside a delimiter: a record literal may start in it, even if
	// the block is in the head of an `if`.
	outerNoRecord, outerParens := p.noRecord, p.parens
	p.noRecord, p.parens = false, 0
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
			first := p.tok()
			e = p.expr(0)
			if p.errs == errs && p.at(LBrace) {
				e = p.forgottenMatch(first, e)
			}
		}
		if p.errs != errs {
			p.skipRest()
		}

		// The statement is the tail when nothing but line breaks, or the end of the
		// block, comes after it: not after a `;`, and never a `let`.
		semi := p.at(Semi)
		p.separator()
		isTail := !isLet && !semi && (p.at(RBrace) || p.at(Eof))

		switch {
		case isTail:
			tail, hasTail = e, true
		case isLet:
			stmts = append(stmts, let)
		default:
			stmts = append(stmts, p.m.Stmts.Push(Stmt{Kind: StmtExpr, Span: p.m.Exprs.Get(e).Span, Expr: e}))
		}
	}
	p.noRecord, p.parens = outerNoRecord, outerParens
	// The statements are collected in a slice of their own and added to the
	// pool when the list is complete: the blocks inside them add theirs first,
	// and a ListRange must be contiguous.
	return p.m.Exprs.Push(Expr{
		Kind: ExprBlock, Span: p.spanFrom(open.Span),
		Stmts: base.AppendRange(&p.m.StmtPool, stmts), Tail: tail, HasTail: hasTail,
	})
}

// separator consumes what ends a statement of a block or an arm of a match: a `;`,
// or the line breaks (the Newline tokens), and nothing at a `}` or at the end of
// the file, which end the block or the match themselves (the top of their loops
// report a missing `}`). A token that is none of those needs a separator in front
// of it. A line break is one, although the line rule put no Newline there:
// NewlineBefore is the lexer's own answer to whether the source has one between
// the last token and this one. That also covers the token at which the skip of a
// statement that went wrong stopped (see syncStatement): the only one that is
// none of the cases above is a `let` that begins a line, and so has its line break.
// Any other token is E0201 `newline`, and is left where it is: it starts what
// comes next.
func (p *parser) separator() {
	switch {
	case p.at(Semi):
		p.advance()
	case p.at(Newline):
		p.skipNewlines()
	case p.at(RBrace), p.at(Eof):
	default:
		if !p.tok().NewlineBefore {
			p.expected("newline")
		}
	}
}

// letStmt reads a `let` statement, the cursor being at the `let`:
//
//	'let' pat [ ':' type ] '=' expr
//
// A pattern that is a plain lowercase name is the `let` of Part I: Name and
// NameSpan are the name. Any other pattern, `let (a, b) = e` or `let _ = e`, is read
// by pattern (parser_pat.go) and kept in Pat, with HasPat true and no name. A
// pattern that cannot be read (it has been reported) makes a StmtError that covers
// what was consumed, and block skips the rest of the statement: the names that it
// would have bound are not known, so none is.
//
// Its span runs from the `let` to the end of the initializer, and leaves out
// the separator that follows. If the pattern is missing (E0201 `identifier`, for a
// token that cannot start a pattern) the statement is a StmtError that covers the
// `let`. If the `=` is missing (E0201 `=`) it is still a StmtLet that binds the
// name, with an error node for the initializer, an empty one at the token found
// instead of the `=`: the statements after it can use the name without more errors
// about it. No initializer is read then, because the tokens that follow may well
// be the end of the block.
func (p *parser) letStmt() StmtID {
	kw := p.advance()
	var s Stmt
	switch t := p.tok(); {
	case t.Kind == LowerIdent:
		p.advance()
		s = Stmt{Kind: StmtLet, Name: base.Symbol(t.Payload), NameSpan: t.Span}
	case startsPattern(t.Kind):
		p.inLet = true
		pat, ok := p.pattern()
		p.inLet = false
		if !ok {
			return p.m.Stmts.Push(Stmt{Kind: StmtError, Span: p.spanFrom(kw.Span)})
		}
		s = Stmt{Kind: StmtLet, Pat: pat, HasPat: true}
	default:
		// The help is for the `let` alone: any other place that needs an
		// identifier has nothing concrete to suggest.
		p.expectedWith("identifier", func(d diag.Diagnostic) diag.Diagnostic {
			return d.WithHelp("write the name of the variable: `let x = …`")
		})
		return p.m.Stmts.Push(Stmt{Kind: StmtError, Span: kw.Span})
	}
	if _, ok := p.eat(Colon); ok {
		s.Type, s.HasType = p.typeExpr(), true
	}
	if _, ok := p.expect(Eq, "`=`"); ok {
		first, errs := p.tok(), p.errs
		s.Expr = p.expr(0)
		if p.errs == errs && p.at(LBrace) {
			// `let a = s {` over arms: the keyword that the initializer lost.
			s.Expr = p.forgottenMatch(first, s.Expr)
		}
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
	cond, abandoned := p.condition(ifHead)
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

// matchExpr reads a match, the cursor being at the `match`. A match is one level of
// nesting, the one that its scrutinee, the guard and the body of each arm are read
// in, as the condition and the branches of an if are read in the level that the if
// takes (see ifExpr).
//
// The scrutinee is the head of the match (see condition), so a record literal in it
// needs parentheses; the arms are inside braces, where a record literal may start
// again. If the scrutinee is abandoned (E0205, E0204) the statement was skipped to
// its end, the arms included, and the match is one error node that covers what was
// read. A `{` that does not follow the scrutinee is E0201 `{`, and then the arms
// are read all the same when they are where they should be, the line after the
// scrutinee (see armsFollow): the one mistake is the `{`, and the arms are not
// statements, so none of their variables is a false "unknown name". When they are
// not there the match is one error node, as for the branch of an if.
//
// An arm that goes wrong is skipped to its end, and the arms after it are read: a
// broken arm stays in the list, with an error pattern or an error body (see
// MatchArm), so each one is a diagnostic and the others are not lost. A missing `}`
// is E0202, at the `{`, if the file ends first, and the match ends at the last token
// that was read, as a block does. If a `let`, the start of an item or a line that only
// a statement can begin (see beginsStatement) comes first, it ends the match with the
// E0201 that any closer has (see matchArms). A `{` after an expression statement with
// arms after it is a match whose keyword was left out (see forgottenMatch), and it is
// read as one.
func (p *parser) matchExpr() ExprID {
	kw := p.advance()
	if !p.enter() {
		return p.errorExpr(kw.Span)
	}
	e := p.matchBody(kw)
	p.leave()
	return e
}

// matchBody is matchExpr without the level of nesting, the cursor being after the
// `match`, which is kw.
func (p *parser) matchBody(kw Token) ExprID {
	scrutinee, abandoned := p.condition(matchHead)
	if abandoned {
		return p.errorExpr(kw.Span)
	}
	open, ok := p.expect(LBrace, "`{`")
	var opener *Token
	if ok {
		opener = &open
	} else if brace, found := p.misplacedBrace(); found {
		opener = &brace
	} else if !p.armsFollow() {
		return p.errorExpr(kw.Span)
	}
	return p.matchOf(kw.Span, scrutinee, opener)
}

// matchOf reads the arms of a match that starts at start, whose scrutinee has been
// read and whose `{` is opener (nil if it is missing), and returns the match.
func (p *parser) matchOf(start base.Span, scrutinee ExprID, opener *Token) ExprID {
	outerNoRecord := p.noRecord
	p.noRecord = false
	arms := p.matchArms(opener)
	p.noRecord = outerNoRecord
	// The arms are collected in a slice of their own and added to the pool when the
	// list is complete: the matches inside them add theirs first, and a ListRange
	// must be contiguous.
	return p.m.Exprs.Push(Expr{
		Kind: ExprMatch, Span: p.spanFrom(start), Scrutinee: scrutinee, Arms: base.AppendRange(&p.m.ArmPool, arms),
	})
}

// forgottenMatch is called by block when the expression of a statement, which began
// at the token first, is read with no mistake and is followed by a `{` (a `{` on the
// next line is behind a Newline), and returns the statement's expression. letStmt
// calls it the same way for the initializer of a `let`, which is where a `match`
// is most often written (`let a = s {`).
// Nothing valid has a `{` there: a block, an `if` and a record literal were read
// with the expression, so it is a mistake already, and if what follows the `{` is
// arms (see armsFollowBrace) the mistake is a `match` whose keyword was left out:
// `s {` over `Circle(r) => …`. It is reported once, E0201 at the first token
// of the statement, with the edit that writes the keyword in front of it when the
// statement is on one line, and the arms are read as the arms of a match whose
// scrutinee is the expression: their variables are not statements for the passes
// after to call unknown names, and the mistake costs one diagnostic however many arms
// there are (TestParsePat/e0201_sites_of_patterns). A statement that
// ends with a `{` that does not start arms is left to block, which reports it as it
// always did.
func (p *parser) forgottenMatch(first Token, scrutinee ExprID) ExprID {
	if !p.armsFollowBrace() {
		return scrutinee
	}
	open, start := p.tok(), first.Span
	d := diag.NewError(diag.E(201), "expected `match`, found `"+p.tb.text(first)+"`",
		diag.Label{Span: start, Message: "expected `match` here"}).
		WithHelp("a `{` followed by arms belongs to a `match`: write the keyword before the value they take apart")
	if p.sameLine(base.Span{Lo: start.Lo, Hi: open.Span.Lo}) {
		d = d.WithFixIt(diag.Insert(start, "match "))
	}
	p.report(d)
	if !p.enter() {
		return scrutinee
	}
	p.advance()
	e := p.matchOf(start, scrutinee, &open)
	p.leave()
	return e
}

// armsFollowBrace says whether the `{` at the cursor is followed by arms: after the
// line breaks, a token that can start a pattern and, before that line ends, a `=>`
// outside every bracket the line opens. A block or a record literal does not start
// that way, because an expression is not followed by `=>`.
func (p *parser) armsFollowBrace() bool {
	toks := p.tb.Tokens
	i := p.pos + 1
	for toks[i].Kind == Newline {
		i++
	}
	if !startsPattern(toks[i].Kind) {
		return false
	}
	depth := 0
	for ; ; i++ {
		switch toks[i].Kind {
		case Eof, Semi, Newline:
			return false
		case LParen, LBracket, LBrace:
			depth++
		case RParen, RBracket, RBrace:
			if depth == 0 {
				return false
			}
			depth--
		case FatArrow:
			if depth == 0 {
				return true
			}
		}
	}
}

// misplacedBrace is called after the E0201 for a `{` that is not where it should be,
// right after the scrutinee of a match, with the cursor at what was found instead
// (`match s x {`, `match s ) {`). If the `{` is further along the same line, it is
// the one that opens the arms: misplacedBrace consumes what is in front of it and
// the `{` itself, and returns it with true, so that the one mistake is the junk and
// the arms are read as they are. Otherwise it consumes nothing and returns false.
// The search stops at the end of the line, which the lexer's NewlineBefore says
// even where the line rule put no Newline (inside a parenthesis): the Newline token
// is passed, and the token after it is the one that stops the search. It stops at a
// `}` that closes the block around the match too, and at a `=>`, which makes what
// was found the first arm and the `{` after it the block of its body.
func (p *parser) misplacedBrace() (Token, bool) {
	toks := p.tb.Tokens
	for i := p.pos; ; i++ {
		t := toks[i]
		switch {
		case t.Kind == Eof, t.NewlineBefore, t.Kind == RBrace, t.Kind == FatArrow:
			return Token{}, false
		case t.Kind == LBrace:
			for p.pos < i {
				p.advance()
			}
			return p.advance(), true
		}
	}
}

// armsFollow is called after the E0201 for the `{` that is missing after the
// scrutinee of a match, with the cursor where the `{` should be. It says whether
// the arms are there all the same, and then leaves the cursor at the first one: at
// the token found if it can start a pattern, or else at the first token of the next
// line that can, after what is left of the line of the scrutinee (a `)` that
// someone wrote for the `{`) and the line breaks. If they are not there it consumes
// nothing, so a `{` on the next line, which the line rule has parted from the
// scrutinee, is still the block that a statement of its own starts (the same as the
// branch of an if, TestRecover/if_brace_on_the_next_line).
func (p *parser) armsFollow() bool {
	toks := p.tb.Tokens
	i := p.pos
	// The rest of the line ends at a `;` or a `}` (which end the statement or the
	// block, and are not the line's junk) and at the first token of the next line:
	// the lexer's NewlineBefore says which it is, even where the line rule put no
	// Newline, and where it did the Newline token is stepped over, as it carries no
	// flag of its own and the token after it has it.
	if !startsPattern(toks[i].Kind) {
		for k := toks[i].Kind; k != Eof && k != Semi && k != RBrace && !toks[i].NewlineBefore; k = toks[i].Kind {
			i++
		}
		if !startsPattern(toks[i].Kind) {
			return false
		}
	}
	for p.pos < i {
		p.advance()
	}
	return true
}

// matchArms reads the arms of a match, the cursor being just after its `{`, which
// is open, and the `}`. When the `{` is missing (armsFollow found the arms all the
// same) open is nil, and nothing is reported at the end: the missing `{` was the
// mistake, and said so. Each arm is read by matchArm; what follows it decides what
// happens, as it does for a statement (see block and separator): an arm that went
// wrong is skipped to its end (syncStatement), and the separator after it is
// consumed.
//
// No arm starts with a `let`, with the keyword of an item or of a statement, or with
// a name that is called or used as an operand (see beginsStatement), so a line that
// begins with one and has no `=>` is not a mistake in an arm but what the block holds
// after a match whose `}` was left out, or put somewhere else. It ends the match: E0201
// for the `}` that is missing, as any closer has it (closeWith), with the edit that
// writes it after the last arm, and what follows is read by the block as statements, so
// the one mistake costs one diagnostic however many statements follow it
// (TestParsePat/a_broken_arm_is_one_diagnostic_and_the_others_are_read). A line that
// could be an arm that went wrong stays an arm and costs one diagnostic: one that
// starts like a pattern, and one with a `=>` outside every bracket whatever it starts
// with (`rect(w, h) => …`, `n > 0 => …`), which only an arm has; as does an item after a
// block that is not closed (see block).
//
// Every pass of the loop consumes a token or ends the match: an arm that read
// something consumed one, and one that read nothing and stopped at once leaves the
// cursor at a token that begins a line and a statement, which ends the match before
// the arm is read.
//
// An arm that went wrong is marked (MatchArm.Broken), and so is one whose body has a
// mistake inside, which may have swallowed the arms after it.
func (p *parser) matchArms(open *Token) []MatchArm {
	var arms []MatchArm
	for {
		if _, ok := p.eat(RBrace); ok {
			return arms
		}
		if p.at(Eof) {
			if open != nil {
				p.unclosed(*open)
			}
			return arms
		}
		if p.tok().NewlineBefore && p.beginsStatement() {
			if open != nil {
				p.closeWith(RBrace, "`}`", *open, true)
			}
			p.endedAt = p.pos + 1
			return arms
		}
		before, errs := p.pos, p.errs
		arm := p.matchArm()
		if p.errs != errs {
			arm.Broken = true
			p.skipRest()
		} else {
			arm.Removal, arm.RemovalOneLine = p.armRemoval(arm)
		}
		p.separator()
		if p.pos == before {
			p.advance()
			p.syncStatement()
			p.separator()
		}
		arms = append(arms, arm)
	}
}

// beginsStatement says whether the token at the cursor, the first of a line among
// the arms of a match, can only begin a statement of the block around the match and
// no arm: a `let`, an item, an `if` or a `match`, a block, and a name that is followed
// on its line by what continues an operand, a `(`, a `.` or a binary operator
// (`print(3)`, `p.x`, `a + b`; see continuesOperand), and has no `=>` on its line
// (see hasArrow). A name that is followed by `=>`, by the `if` of a guard, by a `|`
// or by nothing is a variable pattern, and every other token stays an arm that went
// wrong, which is one diagnostic: the answer is a guess, and a wrong yes would make
// a mistake in an arm read what follows as statements, with the arms that it cut off
// judged as if they were all of the match. The line that says no for sure is the one
// that has the arrow: a mistyped constructor (`rect(w, h) => …`), a guard written
// without its `if` (`n > 0 => …`) and a pattern that starts with a `{` are arms. The
// statements that have an arrow of their own, a `let` whose initializer is a lambda
// (`let f = x => x`, which the language has from chapter 15), have an `=` in front of
// it, and neither a pattern nor a guard has one. The keywords of statements that the
// parser does not read yet (`for`, `while`, `return`) are not in the list: they are a
// mistake wherever they are.
func (p *parser) beginsStatement() bool {
	t := p.tok()
	var starts bool
	switch t.Kind {
	case KwLet, KwIf, KwMatch, LBrace:
		starts = true
	case LowerIdent:
		// A line break after the name is a Newline token, which continues nothing.
		starts = continuesOperand(p.kindAt(1), 0)
	default:
		starts = isItemStart(t.Kind)
	}
	return starts && !p.hasArrow()
}

// hasArrow says whether the line that starts at the cursor has a `=>` that is not
// inside a bracket opened on that line and has no `=` in front of it: the mark of an
// arm. The line ends at a Newline, at a `;` and at a token that the lexer says begins
// another line even where the line rule put no Newline (inside a `(` that was never
// closed), and at a closer that nothing on the line opened. An `=` ends it too: it is
// the one of a `let`, and the arrow after it is its initializer's. Brackets that the
// line opens are stepped over whole, so a match written on one line
// (`match y { _ => 1 }`) has no arrow.
func (p *parser) hasArrow() bool {
	toks := p.tb.Tokens
	depth := 0
	for i := p.pos; ; i++ {
		t := toks[i]
		if i > p.pos && t.NewlineBefore && depth == 0 {
			return false
		}
		switch t.Kind {
		case Eof, Semi, Newline, Eq:
			return false
		case LParen, LBracket, LBrace:
			depth++
		case RParen, RBracket, RBrace:
			if depth == 0 {
				return false
			}
			depth--
		case FatArrow:
			if depth == 0 {
				return true
			}
		}
	}
}

// armRemoval returns the span that an edit deleting the arm takes out (see
// MatchArm.Removal), the cursor being at what follows the arm, and whether it
// stays on one line. A `;` that follows the arm goes with it, with the blanks after
// it, so that deleting an arm of `a => 1; b => 2; c => 3` leaves `a => 1; c => 3`
// and not two separators in a row.
func (p *parser) armRemoval(arm MatchArm) (base.Span, bool) {
	end := arm.Span.Hi
	if p.at(Semi) {
		end = p.afterBlanks(p.tok().Span.Hi)
	}
	removal := p.ownLine(base.Span{File: arm.Span.File, Lo: arm.Span.Lo, Hi: end})
	return removal, p.sameLine(removal)
}

// matchArm reads one arm, `pat [if guard] => body`. The guard and the body are
// expressions with the count of operators of their own (see expr); a guard that is
// abandoned has skipped the rest of the arm with it (see armGuard). The guard has to
// be on the line of the pattern, because an `if` does not continue the line before
// it, and the body may start on the line after the `=>`, which cannot end an
// expression. A mistake in the pattern, a missing `=>` or an expression that is not
// there leaves what was not read as an error node, and the arm is still an arm.
func (p *parser) matchArm() MatchArm {
	start := p.tok().Span
	pat, ok := p.pattern()
	arm := MatchArm{Pat: pat}
	if ok {
		arm.Guard, arm.HasGuard, ok = p.armGuard()
	}
	if ok {
		_, ok = p.expect(FatArrow, "`=>`")
	}
	if !ok {
		arm.Body = p.errorExpr(p.tok().Span)
	} else {
		arm.Body = p.expr(0)
	}
	arm.Span = p.spanFrom(start)
	return arm
}

// armGuard reads the guard of an arm if the cursor is at its `if`: the expression
// after it, up to the `=>`. It returns false if the guard is too deeply nested or
// too long (E0204, E0205, reported), where the arm cannot go on: an expression that
// is abandoned has skipped the rest of the arm with it, the `=>` and the body, as
// the condition of an if skips its branches (see condition).
func (p *parser) armGuard() (guard ExprID, has, ok bool) {
	if _, isGuard := p.eat(KwIf); !isGuard {
		return guard, false, true
	}
	guard, abandoned := p.exprOrSkip(0)
	return guard, true, !abandoned
}
