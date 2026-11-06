package syntax

import (
	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// This file reads `type` declarations. The grammar (spec 5.3) is
//
//	type    = 'type' Upper ( '=' ty | '{' variant { vsep variant } '}' | '{' field { vsep field } '}' )
//	variant = Upper [ '(' [ lower ':' ] ty { ',' [ lower ':' ] ty } ')' ]
//	field   = lower ':' ty
//	vsep    = Newline | ';' | ','
//
// without type parameters, which come with the generics: a `<` after the name is
// E0201. A type is a sum (variants), a record (fields) or an alias (`= ty`), and
// the parser tells them apart by the first token that it sees after the name,
// with no look-ahead: an `=` is an alias; after a `{`, an uppercase name starts a
// variant (a sum) and a lowercase one a field (a record), and any other token,
// the `}` of an empty body included, is E0201 `identifier`. A lowercase name that
// no `:` follows is a record field that has no type, and is reported as one
// (`type Shape { circle(r: Float) }` is E0201 `:` at the `(`), however much it
// looks like a variant spelled wrong.
//
// Every list is read with a loop, so the size of a type (spec Review Focus 2:
// 10 000 variants) costs no depth of Go's stack. The types inside a declaration,
// in an alias or a field, nest only as tuple types do (see parenType), and each
// level of those counts against MaxNesting.

// typeItem reads a `type` declaration, the cursor being at the `type` keyword,
// into it, an ItemType whose attributes and `pub` item has read already. start is
// the start of the item: the first attribute, `pub` or `type`.
//
// A name that is missing is E0201 `identifier` and an ItemError with no name. A
// name that is read and then something that is wrong (no `{` or `=`, a body or
// an alias that cannot be read) is an ItemError that keeps the name and, for a
// sum, the variants whose name was read (see Item.ErrorOf). The first mistake
// ends the declaration, and module skips the rest of it (see syncItem), except
// in a sum: a mistake inside a variant ends that variant only, and the variants
// after it are read (see variant and bodyList), so that a use of one of them is
// not an unknown name as well.
func (p *parser) typeItem(start base.Span, it Item) ItemID {
	p.advance() // `type`
	name, ok := p.eat(UpperIdent)
	if !ok {
		p.typeNameExpected()
		return p.errorItem(start)
	}
	it.Name, it.NameSpan = base.Symbol(name.Payload), name.Span
	switch {
	case p.at(Eq):
		p.advance()
		alias := p.typeExpr()
		if p.m.Types.Get(alias).Kind == TypeError {
			return p.namedErrorTypeItem(start, name, nil)
		}
		it.Form, it.Alias = TypeFormAlias, alias
	case p.at(LBrace):
		variants, ok := p.typeBody(&it)
		if !ok {
			return p.namedErrorTypeItem(start, name, variants)
		}
	default:
		p.expected("`{` or `=`")
		// The body may still be there: the `{` on the line below the name, or
		// after a word that does not belong. Its variants are read all the same, so
		// that a use of one of them is not an unknown name as well (the one mistake
		// is the header).
		if p.reachBody() {
			variants, _ := p.typeBody(&it)
			return p.namedErrorTypeItem(start, name, variants)
		}
		return p.namedErrorTypeItem(start, name, nil)
	}
	it.Span = p.spanFrom(start)
	return p.m.ItemNodes.Push(it)
}

// typeNameExpected reports the E0201 for a type name that is not there. A name in
// small letters is the likely slip, since a type starts in uppercase and a
// function or a variable does not (the first letter is the rule, as in a
// variant), so the error says the rule, with the edit that writes the same word
// with a capital when its first character is a letter that has one: `_name` has
// none, and the author has to choose.
func (p *parser) typeNameExpected() {
	t := p.tok()
	p.expectedWith("identifier", func(d diag.Diagnostic) diag.Diagnostic {
		if t.Kind != LowerIdent {
			return d
		}
		d = d.WithHelp("a type name starts with an uppercase letter")
		if text := p.tb.text(t); text[0] >= 'a' && text[0] <= 'z' {
			d = d.WithFixIt(diag.Replace(t.Span, string(text[0]-'a'+'A')+text[1:]))
		}
		return d
	})
}

// reachBody moves the cursor from the broken header of a type to the `{` of its
// body, if there is one, and says whether it did: the words that come before the
// `{` on the line of the header are skipped, and so are the line breaks that
// come before it, but no word is skipped past the start of another item, so a
// type with no body does not swallow the function below it. The line breaks it
// passes are ones that the loop of items skips anyway.
func (p *parser) reachBody() bool {
	for !p.at(LBrace) && !p.at(Newline) && !p.at(Eof) && !isItemStart(p.tok().Kind) {
		p.advance()
	}
	p.skipNewlines()
	return p.at(LBrace)
}

// namedErrorTypeItem is namedErrorItem for a type whose name was read before the
// rest of it stopped making sense. variants are those of a sum that were read,
// the one that the mistake is in last when its name was read; they go to the
// pool as the variants of the error item, so that name resolution knows that the
// constructors exist (see Item.ErrorOf).
func (p *parser) namedErrorTypeItem(start base.Span, name Token, variants []Variant) ItemID {
	return p.m.ItemNodes.Push(Item{
		Kind: ItemError, ErrorOf: ItemType, Span: p.spanFrom(start), Name: base.Symbol(name.Payload), NameSpan: name.Span,
		Variants: base.AppendRange(&p.m.VariantPool, variants),
	})
}

// typeBody reads the `{ … }` of a sum or a record into it, the cursor being at
// the `{`, and sets its Form and its list. It returns false, with the variants
// that were read, if the body could not be read: the one with the mistake
// included if its name was read, without fields, and the ones after it (see
// variant). Then it has added nothing to the pools for the item, which is an
// error item that takes the variants (see namedErrorTypeItem). A `{` that the
// end of the file finds open is E0202 at the `{`.
func (p *parser) typeBody(it *Item) (variants []Variant, ok bool) {
	open := p.advance()
	switch {
	case p.at(Eof):
		p.unclosed(open)
		return nil, false
	case p.at(UpperIdent):
		it.Form = TypeFormSum
		ok = p.bodyList(open, "variants", true, func() bool {
			v, ok := p.variant()
			if v.Name != base.NoSymbol {
				variants = append(variants, v)
			}
			return ok
		})
		if ok {
			it.Variants = base.AppendRange(&p.m.VariantPool, variants)
		}
	case p.at(LowerIdent):
		it.Form = TypeFormRecord
		var fields []Field
		ok = p.bodyList(open, "fields", false, func() bool {
			f, ok := p.recordField()
			fields = append(fields, f)
			return ok
		})
		if ok {
			it.Fields = base.AppendRange(&p.m.FieldPool, fields)
		}
	case isItemStart(p.tok().Kind):
		// Another item starts where the first of the body should: the `}` is missing.
		p.expectClose(RBrace, "`}`", open)
		return nil, false
	default:
		p.expected("identifier")
		return nil, false
	}
	return variants, ok
}

// bodyList reads the items of a type body, the cursor being at the first one:
// read reads one and returns false if it could not. Two items are separated by a
// line break, a `;` or a `,`, and the last may be followed by one separator too
// (a trailing `,` is accepted, as Rust accepts it; the line break before the `}`
// is the usual one). It returns true after consuming the `}`, unless an item
// could not be read.
//
// Two items with nothing between them are E0201 `newline`, with the help that
// names the three separators; what names the items in the plural, `variants` or
// `fields`. The end of the file inside the body is E0202 at open, the `{`. A
// token that starts another item where an item of the body should be is the `}`
// that is missing, and is reported as any other `}` that is missing (see
// expectClose): E0201 `}` with the `{` it would close and the edit that writes it.
//
// The first mistake ends the body when resume is false. When it is true the list
// goes on after a mistake, and it returns false after the `}`: the mistake is
// still the body's. An item that could not be read has been skipped by read, and
// the cursor stands at what ends it (a separator, the `}`, or the end of the
// file); if read left it anywhere else, at the start of the next item, the body
// is over and the `}` is missing. Two items with nothing between them are read
// as if the separator were there.
func (p *parser) bodyList(open Token, what string, resume bool, read func() bool) bool {
	failed := false
	for {
		if p.at(Eof) {
			p.unclosed(open)
			return false
		}
		if isItemStart(p.tok().Kind) {
			p.expectClose(RBrace, "`}`", open)
			return false
		}
		if !read() {
			if !resume || !p.atBodyBreak() {
				return false
			}
			failed = true
		}
		switch {
		case p.at(Comma), p.at(Semi):
			p.advance()
		case p.at(Newline):
			p.skipNewlines()
		case p.at(RBrace), p.at(Eof):
			// The `}` ends the body just below; the end of the file is reported
			// at the top of the loop.
		default:
			// A `:` after a variant is a field written with a capital: the help is
			// the one for a variant written in small letters (see recordField), the
			// rule that both slips break.
			help := "separate the " + what + " with a line break, `;` or `,`"
			if what == "variants" && p.at(Colon) {
				help = fieldOrVariantHelp
			}
			p.expectedWith("newline", func(d diag.Diagnostic) diag.Diagnostic {
				return d.WithHelp(help)
			})
			if !resume {
				return false
			}
			// The separator that is missing is taken as there: the next item is
			// read from where the cursor is, and the body is still a mistake.
			failed = true
		}
		if _, ok := p.eat(RBrace); ok {
			return !failed
		}
	}
}

// atBodyBreak reports whether the cursor is at a token that a body list can go
// on from: a separator, the `}` that ends the body, or the end of the file.
func (p *parser) atBodyBreak() bool {
	switch p.tok().Kind {
	case Newline, Semi, Comma, RBrace, Eof:
		return true
	}
	return false
}

// variant reads one variant: `None`, `Some(A)` or `Circle(radius: Float)`. It
// returns false if it could not; the Variant then has its name if that was read,
// and no fields. It has then consumed what remained of the variant (see
// syncVariant), and the cursor is where the list of variants can go on.
func (p *parser) variant() (Variant, bool) {
	name, ok := p.expect(UpperIdent, "identifier")
	if !ok {
		p.syncVariant(false)
		return Variant{}, false
	}
	v := Variant{Name: base.Symbol(name.Payload), Span: name.Span, NameSpan: name.Span}
	if open, ok := p.eat(LParen); ok {
		var fields []Field
		for {
			f, ok := p.variantField()
			if !ok {
				p.syncVariant(true)
				return v, false
			}
			fields = append(fields, f)
			if _, ok := p.eat(Comma); !ok {
				break
			}
		}
		if !p.expectClose(RParen, "`)`", open) {
			p.syncVariant(true)
			return v, false
		}
		v.Fields = base.AppendRange(&p.m.FieldPool, fields)
	}
	v.Span = p.spanFrom(name.Span)
	return v, true
}

// syncVariant consumes the tokens that remain of a variant that went wrong and
// leaves the cursor at what ends it, which it does not consume:
//
//   - a Newline, or a `;` or a `,`, outside the brackets that the skipped tokens
//     opened. If the variant stopped inside its list of fields, inParen, the
//     list is still open and the `,` between two fields and a `;` are not
//     separators of the body until the `)` that closes it is skipped.
//   - a `}` that closes no `{` opened by the skipped tokens: the end of the body,
//     which also ends a `(` that was left open.
//   - a token that starts an item, outside those brackets, as syncItem stops at:
//     the `}` is missing and the item is the next one.
//   - the end of the file.
//
// The brackets that were open before the cursor are not counted, except the `(`
// of the field list that inParen says is open.
func (p *parser) syncVariant(inParen bool) {
	var open bracketStack
	for {
		k := p.tok().Kind
		if k == Eof || k == RBrace && open.braces == 0 {
			return
		}
		if len(open.kinds) == 0 {
			switch {
			case isItemStart(k), k == Newline, !inParen && (k == Comma || k == Semi):
				return
			case k == RParen && inParen:
				inParen = false
				p.advance()
				continue
			}
		}
		trackBracket(&open, k)
		p.advance()
	}
}

// variantField reads a field of a variant: `lower ':' type` or `type`. A type
// never starts with a lowercase token, so the first token says which.
func (p *parser) variantField() (Field, bool) {
	start := p.tok().Span
	var f Field
	if name, ok := p.eat(LowerIdent); ok {
		if _, ok := p.expect(Colon, "`:`"); !ok {
			return Field{}, false
		}
		f.Name, f.NameSpan, f.HasName = base.Symbol(name.Payload), name.Span, true
	}
	f.Type = p.typeExpr()
	f.Span = p.spanFrom(start)
	return f, p.m.Types.Get(f.Type).Kind != TypeError
}

// recordField reads a field of a record: `lower ':' type`. A name with no `:`
// after it is what a variant looks like when it is spelled in small letters, so
// the help says both rules: it has no edit, because whether the name was meant
// to be a field or a variant is for the author to say.
func (p *parser) recordField() (Field, bool) {
	name, ok := p.expect(LowerIdent, "identifier")
	if !ok {
		return Field{}, false
	}
	if _, ok := p.eat(Colon); !ok {
		p.expectedWith("`:`", func(d diag.Diagnostic) diag.Diagnostic {
			return d.WithHelp(fieldOrVariantHelp)
		})
		return Field{}, false
	}
	f := Field{Name: base.Symbol(name.Payload), NameSpan: name.Span, HasName: true}
	f.Type = p.typeExpr()
	f.Span = p.spanFrom(name.Span)
	return f, p.m.Types.Get(f.Type).Kind != TypeError
}

// fieldOrVariantHelp is the help for the two ways of confusing a field with a
// variant: a name in small letters with no `:` after it, and a name with a
// capital that has one.
const fieldOrVariantHelp = "a field is written `name: Type`, and a variant starts with an uppercase letter"
