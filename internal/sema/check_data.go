package sema

import (
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// This file is how Check reads the expressions that build and take apart records
// and tuples: a record literal, `Point { x: 1.0, ..p }`, a field, `p.x`, a tuple,
// `(1, true)`, and an element of one, `t.0`. They are in a file of their own, and
// not in check.go, because they are one idea, and the five diagnostics that go
// with it, E0409 to E0413, have a long builder each.
//
// The receiver of an access has to have its type by the time the access is read
// (spec 5.4, rule 1): the checker looks at the type of the target as it is, after
// resolving, and a variable that nothing has bound is not a record or a tuple yet.
// That is an error for now (see field and tupleIndex), and the methods of chapter
// 14 give the case its own diagnostic. An expression that has the error type is
// not an error again, and has the error type.
//
// Reading goes through the functions in the order of the file, so what an
// unannotated parameter is when its function is read may depend on a call in
// another function that came first: unlike the operators, which wait for every
// body (see judgePending), an access is judged at once, as the rule says, and the
// same two functions in the other order can be judged differently.

// maxMissingListed is how many missing fields the message of an E0410 names, and
// how many secondary labels it has. A record can have thousands of fields, and a
// literal that gives none would write them all.
const maxMissingListed = 8

// tuple is the type of a tuple expression: the tuple of the types of its
// elements, which are inferred from left to right.
func (c *checker) tuple(e syntax.Expr) TypeID {
	args := c.m.ExprList(e.Args)
	types := make([]TypeID, len(args))
	for i, a := range args {
		types[i] = c.infer(a)
	}
	return c.tp.Types.Tuple(types)
}

// tupleIndex is the type of `t.0`: the type of that element of the tuple that t
// has. The index is out of range, or t is not a tuple, or has no type yet: E0413
// (see noElementError), and the expression has the error type. A target that has
// the error type is not reported again.
func (c *checker) tupleIndex(e syntax.Expr) TypeID {
	t := c.tp.Types.Resolve(c.infer(e.Target))
	if t == c.errT {
		return c.errT
	}
	node := c.tp.Types.Node(t)
	if node.Kind == TypeTuple && uint64(e.Index) < uint64(len(node.Params)) {
		return node.Params[e.Index]
	}
	c.noElementError(e, t)
	return c.errT
}

// noElementError reports E0413 for the tuple index e, whose target has the type
// t, which has no such element: a tuple that is shorter, or a type that is not a
// tuple at all. The primary label is on the index, and what the message and the
// label say about the index is the index as it is written (Expr.Name), which is
// not the number that Expr.Index holds if it is too large for 32 bits.
func (c *checker) noElementError(e syntax.Expr, t TypeID) {
	if c.sink.Truncated() {
		return
	}
	label := "not a tuple"
	node := c.tp.Types.Node(t)
	if node.Kind == TypeTuple {
		label = onlyElements(len(node.Params))
	}
	d := diag.NewError(diag.E(413), "`"+c.tp.Types.Show(t)+"` has no element `"+c.in.String(e.Name)+"`",
		diag.Label{Span: e.OpSpan, Message: label})
	if node.Kind != TypeTuple {
		d = d.WithNote("only a tuple has elements `.0`, `.1`…")
	}
	c.sink.Report(d)
}

// onlyElements is the label of an E0413 on a tuple of n elements: it says which
// ones exist, by name when there are three or fewer and as a range otherwise.
func onlyElements(n int) string {
	switch n {
	case 2:
		return "only `.0` and `.1` exist"
	case 3:
		return "only `.0`, `.1` and `.2` exist"
	}
	return "only `.0` to `." + strconv.Itoa(n-1) + "` exist"
}

// recordOf returns the declaration of the record that the type ty is, which has
// to be resolved, and whether it is one: a type that is a declared type whose form
// is a record. An alias of a record is the record, as every alias is expanded.
func (c *checker) recordOf(ty TypeID) (TypeDefID, TypeDef, bool) {
	n := c.tp.Types.Node(ty)
	if n.Kind != TypeData {
		return 0, TypeDef{}, false
	}
	def := c.decls.Defs.Get(n.Def)
	return n.Def, def, def.Form == syntax.TypeFormRecord
}

// field is the type of `p.x`: the type of the field x of the record that p is.
// If p is not a record, which includes a variable that nothing has bound, that is
// E0412 (see notRecordError); if it is a record with no such field, E0409 (see
// unknownField). Either way the expression has the error type, which says
// nothing wherever it is used, so a mistake in the field is one error. A target
// that has the error type is not reported again.
func (c *checker) field(e syntax.Expr) TypeID {
	t := c.tp.Types.Resolve(c.infer(e.Target))
	if t == c.errT {
		return c.errT
	}
	td, def, ok := c.recordOf(t)
	if !ok {
		c.notRecordError(e.NameSpan, t, "", c.declaredAs(t), false)
		return c.errT
	}
	i, ok := c.decls.FieldIndex(td, e.Name)
	if !ok {
		c.unknownField(def, e.Name, e.NameSpan, nil, false, false)
		return c.errT
	}
	return def.Fields[i].Type
}

// declaration is where a type is declared, for the secondary label of an E0412:
// the name that the type is written with and the span of that name in its
// `type`, and whether the type is a sum, which has a note of its own.
type declaration struct {
	name string
	span base.Span
	sum  bool
	ok   bool // the type is declared by the program: false for a builtin, a tuple…
}

// declaredAs returns where the type ty, which is resolved, is declared, if it is
// a type that the program declares.
func (c *checker) declaredAs(ty TypeID) declaration {
	n := c.tp.Types.Node(ty)
	if n.Kind != TypeData {
		return declaration{}
	}
	def := c.decls.Defs.Get(n.Def)
	return declaration{name: c.in.String(def.Name), span: def.Span, sum: def.Form == syntax.TypeFormSum, ok: true}
}

// notRecordError reports E0412 for what is not a record but is used as one: the
// message writes the type ty as Show does, or name if it is not empty (the name
// that a literal was written with), and the primary label is at, the name after
// the dot of an access, the name of a literal or the name of a record pattern
// (pattern is true for the last: it takes a value apart itself, and it matches
// fields and does not read them, so the label and the note are worded for that).
// Where the type is declared is a secondary label when it is in the file of the
// primary label; a sum has a note that says how it is taken apart. A type is
// written only after the sink is known to have room: writing a large one is the
// cost of a message that is dropped.
func (c *checker) notRecordError(at base.Span, ty TypeID, name string, decl declaration, pattern bool) {
	if c.sink.Truncated() {
		return
	}
	shown := name
	if shown == "" {
		shown = c.tp.Types.Show(ty)
	}
	label, note := "has no fields to access", "only a record has fields; a sum type is taken apart with `match`"
	if pattern {
		label, note = "has no fields to match", "only a record has fields; the values of a sum type are matched by the names of its variants"
	}
	d := diag.NewError(diag.E(412), "`"+shown+"` is not a record", diag.Label{Span: at, Message: label})
	if decl.ok && decl.span.File == at.File {
		d = d.WithSecondary(diag.Label{Span: decl.span, Message: "`" + decl.name + "` is defined here"})
	}
	if decl.sum {
		d = d.WithNote(note)
	}
	c.sink.Report(d)
}

// unknownField reports E0409 for the field written at at, whose name is not one
// of those of the record def: in an access, or in a literal. The label of the
// declaration of the type is a secondary one when it is in the same file, and the
// help asks whether a field of the record, spelled almost like it, was meant, with
// the edit that writes it. The fields in given, which a literal gives already, are
// not candidates: the edit would write one of them twice. In a pun, `Point { xx }`,
// the name is the field and also the variable that gives its value. If that
// variable exists, the edit that writes the field only, `Point { x }`, would read
// another variable: it writes the field and keeps the variable, `x: xx`. If it
// does not (varUnknown: the resolver has reported E0301 on the same name) there is
// no variable to keep, and the edit that mends both errors is the resolver's to
// offer, because only it knows the variables (see punEdit): this error keeps its
// question and has no edit, so that no two edits compete for the same name.
func (c *checker) unknownField(def TypeDef, name base.Symbol, at base.Span, given map[base.Symbol]bool, pun, varUnknown bool) {
	if c.sink.Truncated() {
		return
	}
	typ, field := c.in.String(def.Name), c.in.String(name)
	d := diag.NewError(diag.E(409), "no field `"+field+"` on `"+typ+"`", diag.Label{Span: at, Message: "unknown field"})
	if def.Span.File == at.File {
		d = d.WithSecondary(diag.Label{Span: def.Span, Message: "`" + typ + "` is defined here"})
	}
	names := make([]string, 0, len(def.Fields))
	for _, f := range def.Fields {
		if !given[f.Name] {
			names = append(names, c.in.String(f.Name))
		}
	}
	if best, ok := suggest(field, names); ok {
		d = d.WithHelp("did you mean `" + best + "`?")
		if !varUnknown {
			text := best
			if pun {
				text = best + ": " + field
			}
			d = d.WithFixIt(diag.Replace(at, text))
		}
	}
	c.sink.Report(d)
}

// record is the type of a record literal, `Point { x: 1.0, ..p }`. The name has to
// be a type that is a record, or an alias of one, as the type of an annotation is
// looked up (see recordType), and the literal has that type: each field that it
// gives is checked against the type of the field, with ReasonField, and the base,
// if there is one, against the record. The mistakes inside the literal do not
// change its type: it is still a `Point`, and the place that uses it is not
// told again (an unknown field is E0409, a field given twice E0411, and fields
// that are not given, when there is no base, E0410). If the name is not a record
// the literal has the error type; its values are still inferred, for the mistakes
// in them.
//
// A literal that has a field the record does not have, which is most often a
// field misspelled, does not give E0410 for the one it was meant to be: that is
// the same mistake.
func (c *checker) record(e syntax.Expr) TypeID {
	fields := c.m.FieldInits(e.Fields)
	ty, ok := c.recordTypeNamed(e.Name, e.NameSpan, false)
	if !ok {
		for _, f := range fields {
			c.infer(f.Value)
		}
		if e.HasBase {
			c.infer(e.Base)
		}
		return c.errT
	}
	td, def, _ := c.recordOf(ty)
	given := make([]bool, len(def.Fields))
	firstAt := make(map[base.Symbol]base.Span)
	// The fields of the record that the literal names, which an unknown field is not
	// offered as a spelling of.
	named := make(map[base.Symbol]bool, len(fields))
	for _, f := range fields {
		if _, ok := c.decls.FieldIndex(td, f.Name); ok {
			named[f.Name] = true
		}
	}
	unknown := false
	for _, f := range fields {
		i, ok := c.decls.FieldIndex(td, f.Name)
		if !ok {
			unknown = true
			c.unknownField(def, f.Name, f.Span, named, f.Pun, f.Pun && c.rm.ExprRes[f.Value].Kind == ResError)
			c.infer(f.Value)
			continue
		}
		if first, dup := firstAt[f.Name]; dup {
			c.duplicateFieldError(f.Name, f.Span, first, f.Removal, f.RemovalOneLine)
		} else {
			firstAt[f.Name] = f.Span
		}
		given[i] = true
		c.check(f.Value, def.Fields[i].Type, Reason{Kind: ReasonField, Because: def.Fields[i].Span})
	}
	if e.HasBase {
		c.check(e.Base, ty, Reason{Kind: ReasonField})
	} else if !unknown {
		c.missingFieldsError(e, def, given)
	}
	return ty
}

// recordTypeNamed returns the type that the name of a record literal, or of a
// record pattern, stands for, and whether it is a record; sym is the name and at
// where it is written; pattern is true for a pattern, whose E0412 is worded for
// one. A name that is not is reported here, and the literal or the pattern has the
// error type: an unknown name is E0406, as in an annotation, a builtin type
// (`Int { x: 1 }`) or a declared type that is a sum, or an alias of something that
// is not a record, E0412. A type that the parser could not read after its name has
// been reported, and is not reported again, nor is an alias that has the error type
// (a cycle, an unknown name, a broken type).
func (c *checker) recordTypeNamed(sym base.Symbol, at base.Span, pattern bool) (TypeID, bool) {
	name := c.in.String(sym)
	res, found := c.res.LookupType(c.mod, sym)
	if !found {
		if _, builtin := builtinTypes[name]; builtin {
			c.notRecordError(at, c.errT, name, declaration{}, pattern)
		} else {
			c.reader.unknownTypeError(c.mod, at, name)
		}
		return c.errT, false
	}
	if res.Kind != ResDef {
		return c.errT, false
	}
	named := c.decls.typeOf[res.Def]
	ty := c.decls.Meaning(named)
	if ty == c.errT {
		// An alias in a cycle, or of a type that is not there or could not be read,
		// has the error type, and has been reported where that went wrong: it is not
		// a record that is missing its fields.
		return c.errT, false
	}
	if _, _, ok := c.recordOf(ty); !ok {
		// An alias that expands to a type that is not a record is declared as the
		// alias, and the note is for what it stands for.
		decl := c.declaredAs(ty)
		def := c.decls.Defs.Get(named)
		decl.name, decl.span, decl.ok = name, def.Span, true
		c.notRecordError(at, c.errT, name, decl, pattern)
		return c.errT, false
	}
	return ty, true
}

// duplicateFieldError reports E0411 for the field name of a record literal or a
// record pattern, which is given again at at after being given at first. The
// fix-it removes the field, with its comma, from removal if oneLine says that stays
// in one line; a pattern does not know where its comma is, and offers none.
func (c *checker) duplicateFieldError(name base.Symbol, at, first, removal base.Span, oneLine bool) {
	d := diag.NewError(diag.E(411), "field `"+c.in.String(name)+"` is given twice",
		diag.Label{Span: at, Message: "given again here"}).
		WithSecondary(diag.Label{Span: first, Message: "first given here"}).
		WithHelp("remove one of them")
	if oneLine {
		d = d.WithFixIt(diag.Delete(removal))
	}
	c.sink.Report(d)
}

// missingFieldsError reports E0410 for the record literal e, which gives no value
// for some of the fields of def (given says which it gives, in the order of the
// declaration) and has no base: one error that lists the missing ones, with the
// primary label on the closing brace and a secondary one on the declaration of
// each field, in the same file. Nothing is reported if no field is missing.
func (c *checker) missingFieldsError(e syntax.Expr, def TypeDef, given []bool) {
	closing := base.Span{File: e.OpSpan.File, Lo: e.OpSpan.Hi - 1, Hi: e.OpSpan.Hi}
	d, ok := c.missingFields(def, given, closing, "")
	if !ok {
		return
	}
	c.sink.Report(d.WithNote("a record literal names every field, or ends with `..base`"))
}

// missingFields is the E0410 for a record literal or a record pattern of the type
// def whose closing brace is at closing and that does not give the fields that
// given does not mark, without the note, which is the caller's: the literal and the
// pattern each have their rule. what is empty for the literal and "pattern " for
// the pattern, which the message names. It answers false, with nothing to report,
// if no field is missing or the sink is full: the list of fields is not written for
// a message that would be dropped.
func (c *checker) missingFields(def TypeDef, given []bool, closing base.Span, what string) (diag.Diagnostic, bool) {
	var missing []int
	for i, ok := range given {
		if !ok {
			missing = append(missing, i)
		}
	}
	if len(missing) == 0 || c.sink.Truncated() {
		return diag.Diagnostic{}, false
	}
	shownCount := min(len(missing), maxMissingListed)
	names := make([]string, shownCount)
	for k, i := range missing[:shownCount] {
		names[k] = "`" + c.in.String(def.Fields[i].Name) + "`"
	}
	list := joinNames(names)
	if len(missing) > shownCount {
		list = strings.Join(names, ", ") + " and " + strconv.Itoa(len(missing)-shownCount) + " more"
	}
	in := " in " + what + "`" + c.in.String(def.Name) + " { … }`"
	title, label := "missing field "+list+in, list+" is not given"
	if len(missing) > 1 {
		title, label = "missing fields "+list+in, list+" are not given"
	}
	d := diag.NewError(diag.E(410), title, diag.Label{Span: closing, Message: label})
	for _, i := range missing[:shownCount] {
		if f := def.Fields[i]; f.Span.File == closing.File {
			d = d.WithSecondary(diag.Label{Span: f.Span, Message: "declared here"})
		}
	}
	return d, true
}

// joinNames writes names as a list of words: `a`, `a` and `b`, `a`, `b` and `c`.
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
