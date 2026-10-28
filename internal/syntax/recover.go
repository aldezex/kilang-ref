package syntax

import (
	"strconv"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// This file is how the parser goes on after a mistake, and how it bounds the
// depth of what it reads (spec 7.3):
//
//   - Error nodes. A place the parser cannot read becomes an ItemError, a
//     StmtError, a TypeError or an ExprError, so the tree is complete and every
//     pass after the parser can walk it. The parser reports the mistake once,
//     and the passes after it report nothing about an error node.
//   - Synchronization. After a mistake inside a statement the parser skips the
//     rest of the statement (syncStatement); after a mistake in an item, the
//     rest of the item (syncItem). Both skip by brackets, so a `;` or a line
//     break inside a `(`, `[` or `{` that the skipped tokens opened does not end
//     the statement.
//   - Suppression. A mistake within suppressDistance tokens of the last one that
//     was reported is not reported: it is most likely the same mistake seen
//     again from a different place (admit).
//   - Unclosed delimiters. A `{`, `(` or `[` whose closing partner is missing
//     where the file ends is E0202, at the delimiter and not at the end of the
//     file (expectClose, block). Where the closer is missing anywhere else, the
//     mistake is an E0201 (expected), and a file that ends within
//     suppressDistance tokens of another mistake reports only that one (admit).
//   - Nesting. Parentheses, call arguments, prefix operators, the parts of an if
//     and blocks nest, and each of them takes a level of Go's call stack.
//     MaxNesting bounds the levels (E0204).

// MaxNesting is the deepest that parentheses, call arguments, prefix operators,
// the condition and the branches of an if, and blocks may nest inside one
// another (E0204). The parser calls itself once for each level, and so does
// every pass after it that walks the tree, so the limit bounds the Go stack that
// a file can make the parser use: a file of a million `(` would end the process
// with a fatal error.
//
// The count is 0 in a statement of the body of a function, and each of those
// constructs adds 1 while it is being read. It is a count of the levels around
// the place the parser is at, whatever statements lie between them: a statement
// inside a block that is inside a call has the levels of the block and of the
// argument. (The limit on a chain of operators, MaxChain, is the opposite: each
// statement counts its own.) Reading the 257th level is the mistake.
const MaxNesting = 256

// suppressDistance is how far apart two reported mistakes must be: the parser
// does not report one that is fewer than this many tokens after the last one it
// reported. Distance is counted in tokens that are not Newlines (see tokIndex).
const suppressDistance = 3

// tokIndex returns the index, among the tokens that are not Newlines, of the
// token at the cursor. The first token is 0. A Newline is not counted, and a
// mistake found at one is at the token the line break follows, which is where
// the line ends: it has that token's index.
func (p *parser) tokIndex() int {
	if p.at(Newline) {
		return p.index - 1
	}
	return p.index
}

// detect records that the parser found a mistake in what it is reading,
// whether or not the mistake is reported. The loops of the parser compare the
// count before and after a statement to know that it went wrong.
func (p *parser) detect() { p.errs++ }

// admit records a mistake found at the cursor and says whether it is to be
// reported. It is not if it is fewer than suppressDistance tokens after the last
// one that was. A mistake that is not reported does not become the point the
// next one is measured from.
func (p *parser) admit() bool {
	p.detect()
	i := p.tokIndex()
	if p.reported && i-p.lastReported < suppressDistance {
		return false
	}
	p.reported, p.lastReported = true, i
	return true
}

// report sends d to the sink, a mistake found at the cursor, unless admit
// suppresses it. A diagnostic whose text is costly to build should call admit
// first (see expected).
func (p *parser) report(d diag.Diagnostic) {
	if p.admit() {
		p.sink.Report(d)
	}
}

// closerOf returns the text of the closing partner of open, a `{`, `(` or `[`.
func closerOf(open TokenKind) string {
	switch open {
	case LBrace:
		return "}"
	case LParen:
		return ")"
	case LBracket:
		return "]"
	}
	panic("unreachable")
}

// unclosed reports E0202 for the delimiter open, a `{`, `(` or `[` whose
// closing partner the parser did not find before the end of the file. The label
// is on the delimiter, because that is where the mistake is, and not on the end
// of the file. The help names the closer, and the fix-it writes it where the
// parser noticed, at the end of the file: the cursor is at the Eof token, which
// is the last in the buffer. That place is not always one where a closer can
// be written:
//
//   - A comment that nothing closes (see TokenBuffer.endsInComment) would take
//     the closer as part of it, so the closer goes right after the last token
//     that was read instead, in front of the comment, as expectClose does.
//   - A string or a character that never ends (see
//     TokenBuffer.unterminatedLiteral) takes it too, if the place is inside it
//     or at its end. The end of the file is when the literal runs to it. The
//     end of the last token read, when a comment ends the file, is when that
//     token lies in the literal: it is the literal's own token or the code of
//     its interpolation. Where the closer belongs then depends on how the
//     literal was meant to end, so there is the help and no fix-it.
//
// Nothing is reported while the expression being read is abandoned.
func (p *parser) unclosed(open Token) {
	if p.chain.abandoned {
		return
	}
	closer := closerOf(open.Kind)
	at := p.tb.Tokens[len(p.tb.Tokens)-1].Span // the Eof
	if p.tb.endsInComment {
		at = base.Span{File: p.m.File, Lo: p.prev().Span.Hi}
	}
	d := diag.NewError(diag.E(202), "unclosed delimiter",
		diag.Label{Span: open.Span, Message: "this `" + p.tb.text(open) + "` is never closed"}).
		WithHelp("close it with `" + closer + "`")
	if !p.tb.inUnterminatedLiteral(at.Lo) {
		d = d.WithFixIt(diag.Insert(at, closer))
	}
	p.report(d)
}

// closerIsMissing reports whether a token of kind k, found where the closer of
// a delimiter was expected, says that the closer is what is missing: k is one
// of the tokens that come after the construct that the delimiter belongs to, so
// the delimiter was left open. They are the `}` or `]` of the construct around
// it, a `;`, a `let` (nothing but a statement starts with one) and a token that
// starts an item. Any other token is more likely a missing `,` or operator
// (`print(1 2)`, `fn f(x y)`) or a different mistake (`-> (Int)`), and writing
// the closer after the last token would not fix the line.
func closerIsMissing(k TokenKind) bool {
	switch k {
	case RBrace, RBracket, Semi, KwLet:
		return true
	}
	return isItemStart(k)
}

// expectClose consumes the closing delimiter of kind k that matches open, the
// token that opened it, and returns true. If it is not there, it reports and
// consumes nothing, and returns false: E0202 at the delimiter if the file ends
// there, and otherwise E0201 for what (see expected). So the `}` that ends a
// block ends a `(` that is still open in it, with an E0201.
//
// The E0201 points at the delimiter that is still open. When the token found
// shows that the closer is what is missing (see closerIsMissing) it also says
// how to close it and offers to write the closer.
func (p *parser) expectClose(k TokenKind, what string, open Token) bool {
	if _, ok := p.eat(k); ok {
		return true
	}
	if p.at(Eof) {
		p.unclosed(open)
	} else {
		p.expectedWith(what, func(d diag.Diagnostic) diag.Diagnostic {
			// The delimiter that the closer would close explains the mistake,
			// whatever was found.
			d = d.WithSecondary(diag.Label{Span: open.Span, Message: "unclosed `" + p.tb.text(open) + "` opened here"})
			if !closerIsMissing(p.tok().Kind) {
				return d
			}
			// The closer goes right after the last token that was read, where
			// the expression stopped, and not at the token that was found.
			closer := closerOf(open.Kind)
			at := base.Span{File: p.m.File, Lo: p.prev().Span.Hi}
			return d.WithHelp("close it with `" + closer + "`").WithFixIt(diag.Insert(at, closer))
		})
	}
	return false
}

// enter takes one level of nesting for the construct that the parser is about
// to read at the cursor, and returns true. The caller must call leave when it
// is done with the construct, and only then. If the level would be past
// MaxNesting it reports E0204 at the cursor, abandons the expression being read
// (see chainState.abandoned), takes no level and returns false: the caller must
// read nothing and return an error node. It also returns false, and reports
// nothing, if the expression is already abandoned.
func (p *parser) enter() bool {
	if p.chain.abandoned {
		return false
	}
	if p.depth == MaxNesting {
		p.chain.abandoned = true
		p.report(diag.NewError(diag.E(204), "expression is nested too deeply",
			diag.Label{Span: p.tok().Span, Message: "nesting level " + strconv.Itoa(MaxNesting+1) + " starts here"}).
			WithNote("the limit is " + strconv.Itoa(MaxNesting)).
			WithHelp("split the expression with `let` bindings"))
		return false
	}
	p.depth++
	return true
}

// leave gives back the level that the last enter that returned true took.
func (p *parser) leave() { p.depth-- }

// trackBracket updates open, the brackets that the tokens skipped by a sync have
// opened and not closed, for a token of kind k. A closing bracket closes its
// partner and whatever was opened inside it, and does nothing if it has none:
// the same rule as the line rule's (see bracketStack.close), so the skip and
// the lexer agree on where a line break is only a space. They do not in a string
// interpolation that leaves a bracket open: the lexer closes it at the
// StringTail (`"${ g(a }"`, see bracketStack.leave), and trackBracket has no case
// for the tokens of a string, so the skip never does. From there it runs on to
// the `}` of the block, and the mistakes in the statements it passes are not
// reported. Strings are not read yet (each one is an E0201 here), so no test
// pins this, and the chapter that reads them has to settle it: the skip may
// start inside an interpolation, where the StringTail has no StringHead of its
// own, and a string may never end.
func trackBracket(open *bracketStack, k TokenKind) {
	switch k {
	case LParen, LBracket, LBrace:
		open.push(k)
	case RParen:
		open.close(LParen)
	case RBracket:
		open.close(LBracket)
	case RBrace:
		open.close(LBrace)
	}
}

// syncStatement consumes the tokens that remain of a statement that went wrong
// and leaves the cursor at the token that ends it, which it does not consume:
// the next Newline or `;` that is not inside brackets opened by the skipped
// tokens, a `}` that closes no `{` opened by them, a `let` that begins a line
// outside such brackets, or the end of the file. The `}` of the block that
// holds the statement is therefore never skipped, and it closes a `(` or `[`
// that the statement left open.
//
// A `let` that begins a line ends the statement because nothing but a statement
// can start with one, and because the lexer puts no Newline in front of it when
// the line before is incomplete, which is when the parser is here: after
// `let x =` the line break is not a Newline token, and without this stop the
// `let` of the next line would be skipped with the rest of the broken one. A
// `let` in the middle of a line does not end it: it belongs to the broken line
// and is skipped with it. In `if let y = x { y } else { 0 }` and in
// `foo(let x = 1)` the `let` is the mistake itself, and a stop there would read
// it as a statement of its own and report the correct text after it (the `{`,
// the `else`, the `)`) as more mistakes. The price is that a `let` after garbage
// on the same line is not in the tree: `let x = let y = 1` has no `y`.
//
// The same happens with a `(` or `[` that the statement left open, which is the
// innermost open bracket for the line rule too: it puts no Newline in front of
// anything up to the `}` of the block. So after a missing `)` the skip runs on
// to a `let` that begins a line, a `;` or that `}`, and the statements after
// the `let` have no Newline between them, only the line breaks of the source.
// block takes those as their separators (Token.NewlineBefore), and the missing
// `)` is one diagnostic and not one for each statement after it; a statement
// that follows on the same line still needs its separator
// (TestRecover/unclosed_paren_is_one_diagnostic).
//
// The brackets that were open before the cursor are not counted. The tokens
// already consumed are the statement's own business: a mistake inside a call
// leaves the cursor inside its parentheses, and the `)` that closes them is one
// of the closing brackets with no partner here, which the skip goes past.
func (p *parser) syncStatement() {
	var open bracketStack
	for {
		t := p.tok()
		k := t.Kind
		switch {
		case k == Eof:
			return
		case (k == Newline || k == Semi || (k == KwLet && t.NewlineBefore)) && len(open.kinds) == 0:
			return
		case k == RBrace && open.braces == 0:
			return
		}
		trackBracket(&open, k)
		p.advance()
	}
}

// isItemStart reports whether a token of kind k can start an item: `fn`, `pub`
// and `#` start the only kind of item there is so far, and `type`, `trait`,
// `impl`, `effect` and `import` those of later parts of the book.
func isItemStart(k TokenKind) bool {
	switch k {
	case KwFn, KwPub, Hash, KwType, KwTrait, KwImpl, KwEffect, KwImport:
		return true
	}
	return false
}

// syncItem consumes the tokens that remain of an item that went wrong and
// leaves the cursor at the next token that can start an item and is not inside
// brackets opened by the skipped tokens, or at the end of the file. A `}` or
// `)` that nothing opened is skipped like any other token. The caller has
// consumed at least one token of the item, so the loop that reads items moves on
// even when this skips none.
func (p *parser) syncItem() {
	var open bracketStack
	for {
		k := p.tok().Kind
		if k == Eof || (len(open.kinds) == 0 && isItemStart(k)) {
			return
		}
		trackBracket(&open, k)
		p.advance()
	}
}
