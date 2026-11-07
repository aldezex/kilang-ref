package syntax

import (
	"strconv"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// This file reads patterns (spec 5.3). The `match` that takes values apart with
// them and the `let` that binds them are read by parser_stmt.go. The grammar is
//
//	pat   = alt { '|' alt }
//	alt   = '_' | lower | literal | '-' int
//	      | Upper [ '(' pat { ',' pat } [ ',' ] ')' ]
//	      | Upper '{' [ fpat { ',' fpat } [ ',' ] ] [ '..' ] '}'
//	      | '(' pat ')' | '(' pat ',' pat { ',' pat } ')'
//	      | '[' [ pat { ',' pat } ] [ '..' ( lower | '_' ) ] ']'
//	fpat  = lower [ ':' pat ]
//	match = 'match' expr '{' { pat [ 'if' expr ] '=>' expr sep } '}'
//
// A pattern is read like the expression that has the same shape, so that what
// is allowed in one is allowed in the other: the arguments of a constructor are
// read like those of a call (a comma after the last one is allowed), the fields
// of a record pattern like those of a record literal (a trailing comma, and a
// line break before a `,` or the `}`), and a tuple like a tuple (`(p,)` is a
// mistake and `(p)` is only `p` in parentheses). A list is the one that has no
// expression yet: it is `[`, patterns and an optional `..rest` that comes last,
// and a comma after its last element is a mistake.

// pattern reads a pattern, the cursor being at its first token. An or-pattern is
// the loosest of them, so `A | B | C` is one node with three alternatives (the
// alternatives are read in a loop, so a hundred thousand of them take no depth).
// It returns false when the pattern could not be read to its end: a token that
// cannot start one or that is not what the rest of the pattern needs (E0201), or
// a pattern nested past MaxPatternDepth (E0204), which has been reported once. The
// pattern is then a PatError that covers what was consumed, and the caller must
// not read on. A `Float` literal (E0706) is reported all the same, but it is a
// complete pattern, so the answer is true: the rest of the arm is read and its
// own mistakes are reported.
func (p *parser) pattern() (PatID, bool) {
	start := p.tok().Span
	if p.patDepth == 0 {
		p.patFrom, p.shaped = p.pos, false
	}
	first, ok := p.patAlt()
	if !ok || !p.at(Bar) {
		return first, ok
	}
	alts := []PatID{first}
	for p.at(Bar) {
		p.advance()
		alt, ok := p.patAlt()
		if !ok {
			return p.errorPat(start), false
		}
		alts = append(alts, alt)
	}
	return p.m.Pats.Push(Pat{Kind: PatOr, Span: p.spanFrom(start), Args: base.AppendRange(&p.m.PatPool, alts)}), true
}

// errorPat adds a PatError that covers the tokens from start to the last one
// consumed, and returns its id. If nothing was consumed since start, its span is
// empty and sits at the start of start.
func (p *parser) errorPat(start base.Span) PatID {
	return p.m.Pats.Push(Pat{Kind: PatError, Span: p.spanFrom(start)})
}

// startsPattern reports whether a token of kind k can begin a pattern. `let`
// asks it to tell a pattern from a mistake where the name of a variable should be.
func startsPattern(k TokenKind) bool {
	switch k {
	case Underscore, LowerIdent, UpperIdent, LParen, LBracket, Int, Float, Minus, KwTrue, KwFalse:
		return true
	}
	return false
}

// patAlt reads one pattern with no `|` at its top: what an alternative of an
// or-pattern is. The answer is the one of pattern. A token that cannot start a
// pattern is E0201 `pattern`, consuming nothing, so that the `)` or the `}` that
// ends the arm is still there. Strings and characters come in later chapters:
// until then their tokens cannot start a pattern.
func (p *parser) patAlt() (PatID, bool) {
	t := p.tok()
	switch t.Kind {
	case Underscore:
		p.advance()
		return p.m.Pats.Push(Pat{Kind: PatWildcard, Span: t.Span}), true
	case LowerIdent:
		p.advance()
		return p.m.Pats.Push(Pat{Kind: PatBind, Span: t.Span, Name: base.Symbol(t.Payload), NameSpan: t.Span}), true
	case KwTrue, KwFalse:
		p.advance()
		return p.m.Pats.Push(Pat{Kind: PatLit, Span: t.Span, Lit: LitBool, Bool: t.Kind == KwTrue}), true
	case Int, Float, Minus:
		return p.numberPat()
	case UpperIdent:
		return p.ctorPat()
	case LParen:
		return p.parenPat()
	case LBracket:
		return p.listPat()
	}
	p.expected("pattern")
	return p.errorPat(t.Span), false
}

// numberPat reads a pattern that is a number, the cursor being at an Int, a Float
// or a `-`: `0`, `-1`, and the Float that is no pattern (E0706). A `-` makes the
// number negative, and anything but a number after it is E0201 `number`, with a
// PatError that covers the `-` and the answer false.
//
// An Int that the lexer reported (E0105, E0107) holds 0 and not the number that
// was written, so the pattern is an error node and the lexer's error is the only
// one, as it is for an expression (see primary). The same goes for a Float that
// the lexer reported (E0109): no E0706 on top of it.
func (p *parser) numberPat() (PatID, bool) {
	from := p.tok().Span
	negative := false
	if _, ok := p.eat(Minus); ok {
		negative = true
	}
	t := p.tok()
	if t.Kind != Int && t.Kind != Float {
		p.expected("number")
		return p.errorPat(from), false
	}
	p.advance()
	span := p.spanFrom(from)
	switch {
	case p.tb.reportedNumber(t):
		return p.m.Pats.Push(Pat{Kind: PatError, Span: span}), true
	case t.Kind == Float:
		return p.floatPat(span, t, negative), true
	}
	n := p.tb.Ints[t.Payload]
	if negative {
		n = -n
	}
	return p.m.Pats.Push(Pat{Kind: PatLit, Span: span, Lit: LitInt, Int: n}), true
}

// floatPat reports E0706 for the Float literal at tok, which lies in span (with
// its `-`, if it has one), and returns the PatError that stands for the pattern.
// A float cannot be matched by its value because `==` on floats is not an
// equivalence. There is no edit: the name of the variable is not mechanical. The
// help writes the literal as the program has it, `x if x == -1.5`, with a name that
// the file does not use (see freshName). It says what to do with the guard that the
// arm has already (`&& …` joins the comparison to it) and with the alternatives
// that the float is one of (`|| x == …` compares each of them), and a `let`, which has
// no guard, says to take the value apart with a `match`.
func (p *parser) floatPat(span base.Span, tok Token, negative bool) PatID {
	text := p.tb.text(tok)
	if negative {
		text = "-" + text
	}
	if p.admit() {
		name, sh := p.freshName(), p.shape()
		guard := name + " if " + name + " == " + text
		var help string
		switch {
		case p.inLet:
			help = "a `let` has no guard: match the value with a `match` and compare in a guard: `" + guard + "`"
		case sh.guard:
			help = "bind it and add the comparison to the guard: `" + guard + " && …`"
		case sh.chain:
			help = "bind it and compare each alternative in a guard: `" + guard + " || " + name + " == …`"
		default:
			help = "bind it and compare in a guard: `" + guard + "`"
		}
		p.sink.Report(diag.NewError(diag.E(706), "a `Float` literal cannot be a pattern",
			diag.Label{Span: span, Message: "not a pattern"}).
			WithNote("`==` on floats is not an equivalence (`NaN != NaN`, `0.0 == -0.0`), so a float cannot be matched by its value").
			WithHelp(help))
	}
	return p.m.Pats.Push(Pat{Kind: PatError, Span: span})
}

// freshName returns the name for a variable in a help about the pattern being read:
// `x`, or `x1`, `x2`… when the file has a name like that anywhere. The parser does not
// know what the later passes know, which names are in scope at the pattern, which
// ones the guard and the body of the arm use and which ones the pattern binds, and a
// name that is in scope would be hidden by the variable that the help writes (a
// parameter `x` and the arm `x if x == 0.5 => x * 2.0`). A name that no token of the
// file spells hides nothing. The names are collected once, the first time a help
// needs one, so that a pattern full of floats takes no pass over the file for each
// of them.
func (p *parser) freshName() string {
	if p.names == nil {
		p.names = map[base.Symbol]bool{}
		for _, t := range p.tb.Tokens {
			if t.Kind == LowerIdent {
				p.names[base.Symbol(t.Payload)] = true
			}
		}
	}
	name := "x"
	for i := 1; ; i++ {
		// A name that was never interned is not in the file; one that was may be the
		// prelude's or another file's, which is no reason to avoid it.
		if sym, ok := p.in.Lookup([]byte(name)); !ok || !p.names[sym] {
			return name
		}
		name = "x" + strconv.Itoa(i)
	}
}

// patShape is what the help of E0706 needs to know about the whole pattern that is
// being read, which it finds out once for the pattern and not once for each float in
// it: a tuple of a hundred thousand floats would take a pass over what is left of the
// pattern for each of them.
type patShape struct {
	guard bool // the arm has a guard: the pattern ends at an `if`
	// chain says that the pattern is a chain of alternatives with no bracket in it
	// (`1 | 2.5`): the variable of the help replaces it whole, and the guard has to
	// compare each of them. An or-pattern inside a tuple or a constructor has no such
	// replacement, and gets the help of a lone float.
	chain bool
}

// shape returns the patShape of the pattern being read, the cursor being inside it.
// The pattern ends at the `=>` of the arm, the `if` of a guard, a `;`, the end of
// the line or the end of the file. A `let` has no guard and says what it says
// whatever its pattern is, so the `=` that ends its pattern is no stop here.
func (p *parser) shape() patShape {
	if p.shaped {
		return p.patInfo
	}
	toks := p.tb.Tokens
	end := p.pos
	for ; ; end++ {
		if k := toks[end].Kind; k == Eof || k == FatArrow || k == KwIf || k == Semi || k == Newline {
			break
		}
	}
	bar, bracket := false, false
	for _, t := range toks[p.patFrom:end] {
		switch t.Kind {
		case LParen, LBracket, LBrace:
			bracket = true
		case Bar:
			bar = true
		}
	}
	sh := patShape{guard: toks[end].Kind == KwIf, chain: bar && !bracket}
	p.patInfo, p.shaped = sh, true
	return sh
}

// enterPat takes one level of nesting for a pattern that has patterns inside, the
// cursor being at the token that opens them, and returns true. The caller must
// call leavePat when it is done, and only then. If the level would be past
// MaxPatternDepth it reports E0204 at the cursor, takes no level and returns
// false: the caller returns a PatError, and so does every pattern around it, since
// each passes on the false that it got. The nesting of patterns is counted on its
// own, because patterns hold no expressions and their depth is the only one that
// the walk over a pattern has to bound.
func (p *parser) enterPat() bool {
	if p.patDepth == MaxPatternDepth {
		// A `let` has no body for the second `match`: it goes after the statement.
		help := "match the inner part with a second `match` in the body of the arm"
		if p.inLet {
			help = "match the inner part with a second `match` after the `let`"
		}
		p.report(diag.NewError(diag.E(204), "pattern is nested too deeply",
			diag.Label{Span: p.tok().Span, Message: "nesting level " + strconv.Itoa(MaxPatternDepth+1) + " starts here"}).
			WithNote("the limit is " + strconv.Itoa(MaxPatternDepth)).
			WithHelp(help))
		return false
	}
	p.patDepth++
	return true
}

// leavePat gives back the level that the last enterPat that returned true took.
func (p *parser) leavePat() { p.patDepth-- }

// ctorPat reads a constructor pattern, the cursor being at its name: `None`, the
// positional `Some(x)` and the record `Point { x, y: p, .. }`. The `(` or the `{`
// must be on the line of the name (a Newline in front of it ends the pattern),
// and a name alone is a constructor with no arguments, `Pat.Args` empty.
func (p *parser) ctorPat() (PatID, bool) {
	name := p.advance()
	pat := Pat{Kind: PatCtor, Name: base.Symbol(name.Payload), NameSpan: name.Span}
	switch {
	case p.at(LParen):
		open := p.tok()
		if !p.enterPat() {
			return p.errorPat(name.Span), false
		}
		p.advance()
		args, ok := p.patArgs(name, open)
		p.leavePat()
		if !ok {
			return p.errorPat(name.Span), false
		}
		pat.Args = args
	case p.at(LBrace):
		open := p.tok()
		if !p.enterPat() {
			return p.errorPat(name.Span), false
		}
		p.advance()
		fields, rest, ok := p.fieldPats(open)
		p.leavePat()
		if !ok {
			return p.errorPat(name.Span), false
		}
		pat.Record, pat.Fields, pat.HasRest = true, fields, rest
	}
	pat.Span = p.spanFrom(name.Span)
	return p.m.Pats.Push(pat), true
}

// subPattern reads a pattern inside the delimiter open, a `(`, `[` or `{` that is
// still open. The file ending there is E0202 at the delimiter, as it is for any
// other (see expectClose), and not a pattern that is missing. The answer is the one
// of pattern.
func (p *parser) subPattern(open Token) (PatID, bool) {
	if p.at(Eof) {
		p.unclosed(open)
		return p.errorPat(open.Span), false
	}
	return p.pattern()
}

// patArgs reads the arguments of a constructor, the cursor being just after its
// `(`, which is open, and the `)`. There is at least one, and a comma after the
// last is allowed, as in a call. The arguments are collected in a slice of their
// own and added to the pool when the list is complete: a ListRange must be
// contiguous, and the patterns inside the arguments add theirs first. It returns
// false, and adds nothing to the pool, if an argument cannot be read or the `)` is
// missing (E0201, or E0202 at the end of the file). Parentheses with nothing in them
// are the first of those: E0201 with the help of what a constructor of that name
// writes, which the parser cannot say better, because only a later pass knows
// whether `name` has fields.
func (p *parser) patArgs(name, open Token) (base.ListRange, bool) {
	if p.at(RParen) {
		p.expectedWith("pattern", func(d diag.Diagnostic) diag.Diagnostic {
			n := p.tb.text(name)
			return d.WithHelp("write `" + n + "` alone if it has no fields, or a pattern for each field: `" + n + "(…)`")
		})
		return base.ListRange{}, false
	}
	var args []PatID
	for {
		arg, ok := p.subPattern(open)
		if !ok {
			return base.ListRange{}, false
		}
		args = append(args, arg)
		if _, ok := p.eat(Comma); !ok || p.at(RParen) {
			break
		}
	}
	if !p.expectClose(RParen, "`)`", open) {
		return base.ListRange{}, false
	}
	return base.AppendRange(&p.m.PatPool, args), true
}

// fieldPats reads the fields of a record pattern, the cursor being just after its
// `{`, which is open, and the `}`: fields separated by commas, and a `..` that
// ends the list and skips the fields not named. `Point { .. }` names none. A line
// break is only space: the lexer puts a Newline after a field that ends its line,
// because it cannot tell the braces of a record from those of a block, so one can
// come before a `,` or the `}` (see record), and none after the `{`, a `,` or the
// `..`, which cannot end a line. rest says that the `..` is there. It
// returns false, and adds nothing to the pool, if a field cannot be read or the
// `}` is missing (the other two results mean nothing then), and the rest of the
// pattern, up to its `}`, is skipped
// (skipBraces): the `}` that closes it would otherwise be taken for the end of the
// match around it.
func (p *parser) fieldPats(open Token) (fields base.ListRange, rest, ok bool) {
	fields, rest, ok = p.fieldPatList(open)
	if !ok {
		p.skipBraces(open)
	}
	return fields, rest, ok
}

// fieldPatList is fieldPats without the skip.
func (p *parser) fieldPatList(open Token) (fields base.ListRange, rest, ok bool) {
	var list []FieldPat
	for !p.at(RBrace) && !p.at(Eof) {
		if _, ok := p.eat(DotDot); ok {
			rest = true
			break
		}
		name, ok := p.eat(LowerIdent)
		if !ok {
			p.expected("identifier")
			return fields, rest, false
		}
		f := FieldPat{Name: base.Symbol(name.Payload), Span: name.Span}
		if _, ok := p.eat(Colon); ok {
			sub, ok := p.subPattern(open)
			if !ok {
				return fields, rest, false
			}
			f.Pat, f.HasPat, f.Span = sub, true, p.spanFrom(name.Span)
		}
		list = append(list, f)
		p.skipNewlines()
		if _, ok := p.eat(Comma); !ok {
			break
		}
	}
	if !p.expectClose(RBrace, "`}`", open) {
		return fields, rest, false
	}
	return base.AppendRange(&p.m.FieldPatPool, list), rest, true
}

// parenPat reads what starts with a `(` in a pattern, the cursor being at it: a
// pattern between parentheses, which is that pattern (a parenthesis is not a
// node, and its span leaves the parentheses out, as for an expression), or a
// tuple, two or more patterns separated by commas. `()` is not a pattern and a
// comma after the last element of a tuple is a mistake, E0201 `pattern` at the `)`.
// Each `(` is a level of nesting.
func (p *parser) parenPat() (PatID, bool) {
	open := p.tok()
	if !p.enterPat() {
		return p.errorPat(open.Span), false
	}
	p.advance()
	id, ok := p.parenPatBody(open)
	p.leavePat()
	return id, ok
}

// parenPatBody is parenPat without the level of nesting, the cursor being after
// the `(`.
func (p *parser) parenPatBody(open Token) (PatID, bool) {
	first, ok := p.subPattern(open)
	if !ok {
		return p.errorPat(open.Span), false
	}
	if !p.at(Comma) {
		if !p.expectClose(RParen, "`)`", open) {
			return p.errorPat(open.Span), false
		}
		return first, true
	}
	elems := []PatID{first}
	for {
		if _, ok := p.eat(Comma); !ok {
			break
		}
		el, ok := p.subPattern(open)
		if !ok {
			return p.errorPat(open.Span), false
		}
		elems = append(elems, el)
	}
	if !p.expectClose(RParen, "`)`", open) {
		return p.errorPat(open.Span), false
	}
	return p.m.Pats.Push(Pat{Kind: PatTuple, Span: p.spanFrom(open.Span), Args: base.AppendRange(&p.m.PatPool, elems)}), true
}

// listPat reads a list pattern, the cursor being at its `[`: the elements, which
// may be none, and then `..t` or `.._`, which binds or skips the rest of the list
// and is the last thing in it. `..` is not the same here as in a record pattern:
// there it stands alone, because the fields it skips have names, and here it needs
// a name or an underscore after it, because the rest is a list that the arm may
// want. A bare `..` is E0201 with the edit that writes the `_`. Each `[` is a level
// of nesting.
func (p *parser) listPat() (PatID, bool) {
	open := p.tok()
	if !p.enterPat() {
		return p.errorPat(open.Span), false
	}
	p.advance()
	pat := Pat{Kind: PatList}
	var elems []PatID
	ok := true
	if !p.at(RBracket) {
		for {
			if dots, isRest := p.eat(DotDot); isRest {
				pat.Rest, pat.HasRest, ok = p.restPat(open, dots)
				break
			}
			var el PatID
			if el, ok = p.subPattern(open); !ok {
				break
			}
			elems = append(elems, el)
			if _, more := p.eat(Comma); !more {
				break
			}
		}
	}
	p.leavePat()
	if !ok || !p.expectClose(RBracket, "`]`", open) {
		return p.errorPat(open.Span), false
	}
	pat.Span = p.spanFrom(open.Span)
	pat.Args = base.AppendRange(&p.m.PatPool, elems)
	return p.m.Pats.Push(pat), true
}

// restPat reads what follows the `..` of a list pattern, which is dots, the `[`
// being open: a name, which binds the rest, or `_`, which does not. Anything else,
// the `]` of `[a, ..]` included, is E0201 for the words identifier or `_`; when it is the `]`,
// the help and the edit write the `_`. The file ending there is E0202, at the `[`.
// It returns the pattern, whether there is one (the second answer, HasRest of the
// list) and whether the rest could be read.
func (p *parser) restPat(open, dots Token) (PatID, bool, bool) {
	if p.at(Eof) {
		p.unclosed(open)
		return p.errorPat(dots.Span), false, false
	}
	t := p.tok()
	switch t.Kind {
	case LowerIdent:
		p.advance()
		return p.m.Pats.Push(Pat{Kind: PatBind, Span: t.Span, Name: base.Symbol(t.Payload), NameSpan: t.Span}), true, true
	case Underscore:
		p.advance()
		return p.m.Pats.Push(Pat{Kind: PatWildcard, Span: t.Span}), true, true
	}
	p.expectedWith("identifier or `_`", func(d diag.Diagnostic) diag.Diagnostic {
		if t.Kind != RBracket {
			return d
		}
		return d.WithHelp("write `.._` to skip the rest of the list").
			WithFixIt(diag.Insert(base.Span{File: dots.Span.File, Lo: dots.Span.Hi}, "_"))
	})
	return p.errorPat(dots.Span), false, false
}
