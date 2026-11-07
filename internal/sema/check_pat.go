package sema

import (
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// This file is how Check reads a `match` and the patterns in it, and the `let`
// that takes a value apart with one. They are in a file of their own, and not in
// check.go, because they are one idea, and two of the diagnostics that go with
// it, E0705 and the pattern form of E0410, have a builder each (the pattern form
// of E0409 is the one of a record literal, which takes a flag for a pun).
//
// A pattern is checked against the type of the value it matches, which the place
// hands down: the scrutinee for the pattern of an arm, the type of the field for a
// pattern inside a constructor, an element of the tuple for one inside a tuple.
// The pattern binds its variables to the types of the parts they match, and not to
// a type of their own: a part that has no type yet (a parameter that nothing has
// typed) gives its variable that part's type variable, which a later use may fix
// (`fn g(x) -> Int { match x { y => y + 1 } }` makes both `Int`). A parameter that
// nothing has typed is a variable that the first pattern binds
// (`fn f(x) { match x { Some(_) => … } }`).
//
// Each pattern says what is wrong with it once. When a constructor does not fit
// the type, its sub-patterns are not compared with the fields, and what an earlier
// error left with no type (the error type) says nothing: a constructor that does
// not exist, a scrutinee that is an unknown name. The sub-patterns are walked in
// every case, with the error type where there is no type to give them, because
// that is what binds their variables.
//
// checkPat recurses once per level of nesting of the pattern, which the parser
// bounds (MaxPatternDepth); a tuple, a list, the arguments of a constructor and the
// alternatives of an or-pattern are loops. A tree built by hand is not bounded.

// inferMatch is the type of a `match` where nothing is expected of it: the type of
// the first arm, and every other arm is checked against that one. If an arm does
// not fit, that is the one mistake and the match has the error type, so the place
// that uses it adds nothing, as for the branches of an `if`; so does a match with
// no arms, which covers no value and whose type nothing says (the pass that finds
// what a match does not cover, E0701, says so). The scrutinee is inferred first and
// each arm's pattern and guard are checked before its body, in the order they are
// written.
func (c *checker) inferMatch(e syntax.Expr) TypeID {
	scrutinee := c.infer(e.Scrutinee)
	at := c.m.Exprs.Get(e.Scrutinee).Span
	ty, because, fits := c.errT, base.Span{}, true
	for i, arm := range c.m.Arms(e.Arms) {
		c.armHead(arm, scrutinee, at)
		if i == 0 {
			ty, because = c.infer(arm.Body), c.valueSpan(arm.Body)
			continue
		}
		if _, ok := c.check(arm.Body, ty, Reason{Kind: ReasonMatchArms, Because: because}); !ok {
			fits = false
		}
	}
	if !fits {
		return c.errT
	}
	return ty
}

// checkMatch checks the `match` e against expected, which r explains. It hands the
// expectation to every arm, as a block hands it to its tail: each arm that does not
// fit is its own error, with the reason of the place (the return type, a
// parameter), and an Int literal where a Float is expected has its edit. The match
// has the expected type, or the error type if an arm does not fit, for what uses it
// to say nothing more. The arms are not compared with one another: each is
// compared with what the place wants.
func (c *checker) checkMatch(e syntax.Expr, expected TypeID, r Reason) (TypeID, bool) {
	scrutinee := c.infer(e.Scrutinee)
	at := c.m.Exprs.Get(e.Scrutinee).Span
	fits := true
	for _, arm := range c.m.Arms(e.Arms) {
		c.armHead(arm, scrutinee, at)
		// The body of an arm has nowhere to write a `let` unless it is a block, which
		// clears this on its way in (checkBlock); the help of a value that is unused
		// needs to know (see Reason.InArm).
		armReason := r
		armReason.InArm = true
		if _, ok := c.check(arm.Body, expected, armReason); !ok {
			fits = false
		}
	}
	if !fits {
		return c.errT, false
	}
	return expected, true
}

// armHead checks the pattern of the arm against the type of the scrutinee, which is
// written at at, and then its guard, if it has one, against Bool: the guard has the
// note of an `if` condition worded for a guard. An arm that the parser had trouble
// with is marked while its pattern is checked (see checker.brokenArm).
func (c *checker) armHead(arm syntax.MatchArm, scrutinee TypeID, at base.Span) {
	c.brokenArm = arm.Broken
	c.checkPat(arm.Pat, scrutinee, Reason{Kind: ReasonScrutinee, Because: at, Whole: scrutinee})
	c.brokenArm = false
	if arm.HasGuard {
		c.check(arm.Guard, c.boolT, Reason{Kind: ReasonIfCondition, Detail: guardDetail})
	}
}

// letPattern checks a `let` with a pattern as an arm is checked, the initializer
// being the scrutinee: the pattern is checked against the type of the initializer,
// or against the annotation if the `let` has one, which the initializer is then
// checked against first and which explains a pattern that does not fit. The
// statement binds the variables of the pattern, and has none of its own.
func (c *checker) letPattern(s syntax.Stmt) {
	if s.HasType {
		ty := c.typeOf(c.mod, s.Type)
		r := Reason{Kind: ReasonLetAnnotation, Because: c.m.Types.Get(s.Type).Span}
		c.check(s.Expr, ty, r)
		c.checkPat(s.Pat, ty, r)
		return
	}
	ty := c.infer(s.Expr)
	c.checkPat(s.Pat, ty, Reason{Kind: ReasonScrutinee, Because: c.m.Exprs.Get(s.Expr).Span, Whole: ty})
}

// checkPat checks the pattern id against expected, the type of the value that it
// matches, which r explains, and writes the type of the pattern and of the
// variables in it.
func (c *checker) checkPat(id syntax.PatID, expected TypeID, r Reason) {
	p := c.m.Pats.Get(id)
	c.patTypes[id] = expected
	switch p.Kind {
	case syntax.PatWildcard:
		// Matches anything.
	case syntax.PatError:
		// The parser has reported it and it matches nothing the checker can say.
		c.patTypes[id] = c.errT
	case syntax.PatBind:
		c.bindVariable(c.rm.PatLocal[id], p.NameSpan, expected)
	case syntax.PatLit:
		c.u.Unify(expected, c.literalType(p), p.Span, r)
	case syntax.PatCtor:
		if p.Record {
			c.recordPat(id, p, expected, r)
		} else {
			c.ctorPat(id, p, expected, r)
		}
	case syntax.PatTuple:
		c.tuplePat(p, expected, r)
	case syntax.PatList:
		c.listPat(p, expected, r)
	case syntax.PatOr:
		// Every alternative matches the same value, and is checked against its type.
		for _, alt := range c.m.PatList(p.Args) {
			c.checkPat(alt, expected, r)
		}
	default:
		panic("unreachable")
	}
}

// checkAll checks the patterns ids against the same type.
func (c *checker) checkAll(ids []syntax.PatID, expected TypeID, r Reason) {
	for _, id := range ids {
		c.checkPat(id, expected, r)
	}
}

// bindVariable gives the variable local, which a pattern binds at at, the type ty
// of the part it matches. A variable that has a type already is one that another
// alternative of an or-pattern bound (they bind the same names to one variable):
// the two must be the same type, or E0401 at this one. The reason has no Because,
// so no secondary label: no other place explains the first.
func (c *checker) bindVariable(local LocalID, at base.Span, ty TypeID) {
	slot := c.tp.LocalTypes.Ptr(local)
	if *slot == c.errT {
		*slot = ty
		return
	}
	c.u.Unify(*slot, ty, at, Reason{Kind: ReasonScrutinee})
}

// literalType is the type of the literal that the pattern p is.
func (c *checker) literalType(p syntax.Pat) TypeID {
	switch p.Lit {
	case syntax.LitInt:
		return c.intT
	case syntax.LitBool:
		return c.boolT
	default:
		panic("unreachable")
	}
}

// patCtor returns the constructor that the name of the constructor pattern id
// stands for, and whether it stands for one.
func (c *checker) patCtor(id syntax.PatID) (CtorID, bool) {
	res := c.rm.PatRes[id]
	if res.Kind != ResDef {
		return 0, false
	}
	return c.decls.CtorOf(res.Def)
}

// ctorPat checks the positional constructor pattern p, `Some(x)` or `None`,
// against expected. Its type is the type of the sum that declares the constructor;
// then the sub-patterns are the fields', each with ReasonPatternArg, which has the
// field as the secondary label, and one that does not number the fields is E0705.
// A name that stands for nothing was reported by Resolve: the pattern has the error
// type and its sub-patterns are only walked.
func (c *checker) ctorPat(id syntax.PatID, p syntax.Pat, expected TypeID, r Reason) {
	args := c.m.PatList(p.Args)
	ctor, ok := c.patCtor(id)
	if !ok {
		c.patTypes[id] = c.errT
		c.checkAll(args, c.errT, r)
		return
	}
	if !c.u.Unify(expected, c.decls.OwnerType(ctor), p.Span, r) {
		c.checkAll(args, c.errT, r)
		return
	}
	fields := c.decls.Ctors.Get(ctor).Args
	if len(args) != len(fields) {
		// A `(` that was left out (`Rect w, h) => …`) makes the pattern `Rect`, which
		// is the parse error's own doing: the edit that writes the fields would leave
		// the line as broken as it was. The sub-patterns are walked all the same.
		if !c.brokenArm {
			c.patArityError(p, ctor, len(args))
		}
		c.checkAll(args, c.errT, r)
		return
	}
	for i, arg := range args {
		c.checkPat(arg, fields[i], Reason{Kind: ReasonPatternArg, Because: c.decls.FieldSpan(c.mods, ctor, i)})
	}
}

// patArityError reports E0705 for the constructor pattern p, which writes named
// sub-patterns where the constructor has another number of fields. The primary
// label is on the pattern, the secondary on the constructor's declaration (when it
// is in this file), and the help writes the pattern with a wildcard for each field,
// with the edit that replaces the pattern by it when the pattern is on one line. A
// constructor with no fields has a message of its own.
func (c *checker) patArityError(p syntax.Pat, id CtorID, named int) {
	if c.sink.Truncated() {
		return
	}
	ctor := c.decls.Ctors.Get(id)
	name := c.in.String(ctor.Name)
	fields := len(ctor.Args)
	title, write := "`"+name+"` has no fields", name
	if fields > 0 {
		title = "`" + name + "` has " + count(fields, "field") + " but the pattern names " + strconv.Itoa(named)
		write += "(" + strings.Repeat("_, ", fields-1) + "_)"
	}
	d := diag.NewError(diag.E(705), title, diag.Label{Span: p.Span, Message: count(named, "sub-pattern")})
	if ctor.Span.File == p.Span.File {
		d = d.WithSecondary(diag.Label{Span: ctor.Span, Message: "`" + name + "` is declared here"})
	}
	d = d.WithHelp("write `" + write + "`")
	if p.OneLine {
		d = d.WithFixIt(diag.Replace(p.Span, write))
	}
	c.sink.Report(d)
}

// count writes n things: `1 field`, `2 fields`, `0 sub-patterns`.
func count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}

// recordPat checks the record pattern p, `Point { x, y: q, .. }`, against
// expected. The name is a type, looked up as the name of a record literal is
// (recordTypeNamed), and each field that the pattern names is checked against the
// field of the record, with ReasonPatternArg: a pun binds a variable of that type.
// A field that the record does not have is E0409, one named twice E0411 (two puns
// of one name are the one E0704 that Resolve has reported), and fields that the
// pattern leaves out, when it does not end with `..`, are E0410. As in a literal, an
// unknown field is most likely the one that was meant, so it hides E0410.
func (c *checker) recordPat(id syntax.PatID, p syntax.Pat, expected TypeID, r Reason) {
	fields := c.m.FieldPats(p.Fields)
	ty, ok := c.recordTypeNamed(p.Name, p.NameSpan, true)
	if !ok {
		c.patTypes[id] = c.errT
		c.fieldPats(p, c.errT, r)
		return
	}
	if !c.u.Unify(expected, ty, p.Span, r) {
		c.fieldPats(p, c.errT, r)
		return
	}
	td, def, _ := c.recordOf(ty)
	given := make([]bool, len(def.Fields))
	first := make(map[base.Symbol]syntax.FieldPat)
	// The fields of the record that the pattern names, which an unknown field is
	// not offered as a spelling of.
	named := make(map[base.Symbol]bool, len(fields))
	for _, f := range fields {
		if _, ok := c.decls.FieldIndex(td, f.Name); ok {
			named[f.Name] = true
		}
	}
	unknown := false
	for i, f := range fields {
		at := c.fieldNameSpan(f)
		idx, ok := c.decls.FieldIndex(td, f.Name)
		if !ok {
			unknown = true
			c.unknownField(def, f.Name, at, named, !f.HasPat, false)
			c.fieldPat(p, i, c.errT, r)
			continue
		}
		if before, again := first[f.Name]; !again {
			first[f.Name] = f
		} else if f.HasPat || before.HasPat {
			c.duplicateFieldError(f.Name, f.Span, before.Span, base.Span{}, false)
		}
		given[idx] = true
		c.fieldPat(p, i, def.Fields[idx].Type, Reason{Kind: ReasonPatternArg, Because: def.Fields[idx].Span})
	}
	if !p.HasRest && !unknown {
		c.missingInPattern(p, def, given)
	}
}

// fieldNameSpan is the span of the name of the field f of a record pattern: its
// Span, which also covers the pattern that follows the name, cut to the name.
func (c *checker) fieldNameSpan(f syntax.FieldPat) base.Span {
	return base.Span{File: f.Span.File, Lo: f.Span.Lo, Hi: f.Span.Lo + uint32(len(c.in.Bytes(f.Name)))}
}

// fieldPat checks the i-th field of the record pattern p against ty: its pattern,
// or, for a pun, the variable that binds the name.
func (c *checker) fieldPat(p syntax.Pat, i int, ty TypeID, r Reason) {
	f := c.m.FieldPats(p.Fields)[i]
	if f.HasPat {
		c.checkPat(f.Pat, ty, r)
		return
	}
	c.bindVariable(c.rm.FieldPatLocal[int(p.Fields.First)+i], f.Span, ty)
}

// fieldPats checks every field of the record pattern p against ty, for the
// variables to have a type.
func (c *checker) fieldPats(p syntax.Pat, ty TypeID, r Reason) {
	for i := range c.m.FieldPats(p.Fields) {
		c.fieldPat(p, i, ty, r)
	}
}

// missingInPattern reports E0410 for the record pattern p, which names the fields
// of def that given marks and does not end with `..`. It is the error of a record
// literal worded for a pattern (see missingFields): the primary label is on the
// closing brace and the help is to skip the rest with `..`, with the edit that
// writes it when the pattern is on one line. The edit goes where it reads best:
// after the last field, with the comma that separates it, or, if a comma follows
// the last field already, or no field is named, just before the brace, with no
// comma of its own and a space after it, since the blank that stood before the
// brace is now before the `..` (`{ x, }` and `{ }` become `{ x, .. }` and `{ .. }`).
func (c *checker) missingInPattern(p syntax.Pat, def TypeDef, given []bool) {
	closing := base.Span{File: p.Span.File, Lo: p.Span.Hi - 1, Hi: p.Span.Hi}
	d, ok := c.missingFields(def, given, closing, "pattern ")
	if !ok {
		return
	}
	d = d.WithNote("a record pattern names every field, or ends with `..`").WithHelp("add `..` to skip the rest")
	if p.OneLine {
		fields := c.m.FieldPats(p.Fields)
		if len(fields) == 0 || p.TrailingComma {
			d = d.WithFixIt(diag.Insert(closing, ".. "))
		} else {
			last := fields[len(fields)-1].Span
			d = d.WithFixIt(diag.Insert(base.Span{File: last.File, Lo: last.Hi}, ", .."))
		}
	}
	c.sink.Report(d)
}

// tuplePat checks the tuple pattern p against expected. A tuple of the same size
// gives each element its pattern's type. A type that nothing has bound yet is a
// tuple of new variables, which the patterns then give their types. Any other type
// is E0401 on the pattern, that says what the pattern is in words.
func (c *checker) tuplePat(p syntax.Pat, expected TypeID, r Reason) {
	elems := c.m.PatList(p.Args)
	t := c.tp.Types.Resolve(expected)
	node := c.tp.Types.Node(t)
	switch {
	case t == c.errT:
		c.checkAll(elems, c.errT, r)
	case node.Kind == TypeTuple && len(node.Params) == len(elems):
		for i, el := range elems {
			c.checkPat(el, node.Params[i], r)
		}
	case node.Kind == TypeVariable:
		parts := make([]TypeID, len(elems))
		for i, el := range elems {
			parts[i] = c.tp.Types.FreshVar(Origin{Span: c.m.Pats.Get(el).Span, Note: "element of a tuple pattern"})
		}
		// The variables are new, so this binds and cannot fail (an occurs check needs
		// the variable to be in the type it is bound to).
		c.u.Unify(t, c.tp.Types.Tuple(parts), p.Span, r)
		for i, el := range elems {
			c.checkPat(el, parts[i], r)
		}
	default:
		c.u.Reject(expected, "a tuple pattern of "+count(len(elems), "element"), p.Span, r)
		c.checkAll(elems, c.errT, r)
	}
}

// listPat checks the list pattern p, `[a, ..t]`, against expected. The type of a
// list is `List<A>` of the prelude, which has no such type yet (it comes with the
// generics and the standard library), so for now no type fits: it is E0401 at the
// pattern, `a list pattern`, never an internal error, and never silence unless the
// expected type is the error type already, for which Reject says nothing (an earlier
// mistake has been reported). The elements and the rest are walked with the error
// type, which binds their variables and says nothing about them.
func (c *checker) listPat(p syntax.Pat, expected TypeID, r Reason) {
	c.u.Reject(expected, "a list pattern", p.Span, r)
	c.checkAll(c.m.PatList(p.Args), c.errT, r)
	if p.HasRest {
		c.checkPat(p.Rest, c.errT, r)
	}
}
