package sema

import (
	"slices"
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// This file is the pass "patterns": what the program says about the values it takes
// apart. It asks two questions of every `match` and one of every `let` that has a
// pattern, and answers them with the engine of pattern_matrix.go.
//
//   - Does the `match` cover every value? If not, E0701 names values that it does
//     not, as patterns (the witnesses).
//   - Does every arm match some value that the arms above it do not? If not, the
//     arm is W0701, the first warning of the language.
//   - Does the pattern of a `let` match every value of its initializer? If not,
//     E0702, with the same witnesses: a `let` has no other arm to turn to.
//
// The rule behind all of them (spec 5.7) is that a guard does not count: an arm
// with an `if` may fail whatever its pattern says, so it covers nothing for the arms
// below it and for the match as a whole, although its own pattern is still checked
// against the arms above. A literal `Int` is never all of its type, so a match on
// integers needs a `_` or a variable. `Bool` is its two literals, and a tuple or a
// record is a type of one constructor.
//
// It runs after Check, and reads what Check wrote. A match that Check had trouble
// with says nothing here: each of its mistakes has been reported, and a second
// message from this pass would say the same thing twice (a constructor that does not
// exist is E0306 and not also "pattern `Circl(_)` not covered"). So the pass looks at
// a `match` only if its scrutinee has a type, and every pattern of its arms fits
// that type by shape: the constructor is one of the type's and has the right number
// of fields, a tuple has the right size, a literal has the type of the column, a
// record names the type and its fields. Whatever does not fit is something Check has
// already reported, and the match is left alone. So is a match that the parser had
// trouble with, if one of its arms is marked (syntax.MatchArm.Broken): the arms that
// were read are not necessarily the program's (a `)` that is missing lets an arm
// swallow the ones after it, and a name where an arm broke off reads as a variable
// that covers them all), and what this pass would say about them, an arm that is not
// covered or one that is never reached, would be said about arms that are in the file,
// with an edit that deletes them.
//
// The expressions are visited by id, one pass over the table of each module, not by
// walking the tree: a `match` inside an arm, inside a function, inside anything, is
// found by being in the table, and the walk has no depth.

// patternBudget is how many steps the engine may take for one `match` or `let`
// before the pass gives up with E0707. A step is a node of the search, a row built
// for it or a cell pushed (see engine.spend), so ten million of them is a second
// or less of work, and far past what any match that a person writes needs: the
// ones that reach it are encodings of a problem that is NP-complete, finding a
// value that no arm matches (E0707).
var patternBudget = 10_000_000

// maxWitnesses is how many witnesses E0701 and E0702 name. The engine looks for one
// more than that, and "and more" is said only if it finds it.
const maxWitnesses = 3

// CheckPatterns is the pass "patterns": it reports every `match` that does not cover
// all the values of its scrutinee (E0701), every `let` whose pattern does not match
// all the values it is given (E0702), and every one of these that is too complex to
// decide (E0707), and the arms that no value reaches (W0701) as far as what is left
// of the budget reaches: the search for them is a warning, so when it runs out it
// stops and says nothing more (see checkMatch). mods, res and typed are what Resolve
// and Check made of the program, and in is its interner, for the names that the
// witnesses write. It reports nothing about a `match` or `let` that has an error
// already, as the file says.
func CheckPatterns(mods []*syntax.Module, res *ResolvedProgram, typed *TypedProgram, in *base.Interner, sink *diag.Sink) {
	p := &patternPass{mods: mods, res: res, typed: typed, decls: typed.Decls, in: in, sink: sink, errT: typed.Types.Error()}
	for mi, m := range mods {
		for _, e := range m.Exprs.All() {
			if e.Kind == syntax.ExprMatch {
				p.checkMatch(mi, e)
			}
		}
		for _, s := range m.Stmts.All() {
			if s.Kind == syntax.StmtLet && s.HasPat {
				p.checkLet(mi, s)
			}
		}
	}
}

// patternPass is the state of one call to CheckPatterns. It is the typeShapes of
// the engine: the types are those of the table, and a constructor is what Declare
// made of a variant.
type patternPass struct {
	mods  []*syntax.Module
	res   *ResolvedProgram
	typed *TypedProgram
	decls *Decls
	in    *base.Interner
	sink  *diag.Sink
	errT  TypeID
	// recordFields caches the types of the fields of a record, which a TypeDef holds
	// with their spans and names and the engine wants as a slice of types.
	recordFields map[TypeDefID][]TypeID
}

// typeNode is the table's entry for the type ty, with its variable resolved at the top.
func (p *patternPass) typeNode(ty TypeID) Type {
	return p.typed.Types.Node(p.typed.Types.Resolve(ty))
}

// signature implements typeShapes.
func (p *patternPass) signature(ty TypeID) signature {
	n := p.typeNode(ty)
	switch n.Kind {
	case TypeBuiltin:
		switch n.Builtin {
		case BuiltinInt:
			return signature{kind: sigInt}
		case BuiltinBool:
			return signature{kind: sigFinite, n: 2}
		}
	case TypeTuple:
		return signature{kind: sigFinite, n: 1}
	case TypeData:
		def := p.decls.Defs.Get(n.Def)
		switch def.Form {
		case syntax.TypeFormSum:
			return signature{kind: sigFinite, n: len(def.Ctors)}
		case syntax.TypeFormRecord:
			return signature{kind: sigFinite, n: 1}
		}
	}
	return signature{}
}

// fields implements typeShapes.
func (p *patternPass) fields(ty TypeID, key int64) []TypeID {
	n := p.typeNode(ty)
	switch n.Kind {
	case TypeTuple:
		return n.Params
	case TypeData:
		def := p.decls.Defs.Get(n.Def)
		if def.Form == syntax.TypeFormSum {
			return p.decls.Ctors.Get(def.Ctors[key]).Args
		}
		if tys, ok := p.recordFields[n.Def]; ok {
			return tys
		}
		tys := make([]TypeID, len(def.Fields))
		for i, f := range def.Fields {
			tys[i] = f.Type
		}
		if p.recordFields == nil {
			p.recordFields = make(map[TypeDefID][]TypeID)
		}
		p.recordFields[n.Def] = tys
		return tys
	}
	return nil // Bool: `false` and `true` have no fields
}

// describe implements typeShapes.
func (p *patternPass) describe(ty TypeID, key int64) ctorShape {
	n := p.typeNode(ty)
	switch n.Kind {
	case TypeBuiltin:
		return ctorShape{name: strconv.FormatBool(key == 1)}
	case TypeTuple:
		return ctorShape{arity: len(n.Params)}
	}
	def := p.decls.Defs.Get(n.Def)
	if def.Form == syntax.TypeFormSum {
		c := p.decls.Ctors.Get(def.Ctors[key])
		return ctorShape{name: p.in.String(c.Name), arity: len(c.Args)}
	}
	names := make([]string, len(def.Fields))
	for i, f := range def.Fields {
		names[i] = p.in.String(f.Name)
	}
	return ctorShape{name: p.in.String(def.Name), form: formRecord, fields: names, arity: len(def.Fields)}
}

// lower turns the pattern id, which is checked against a value of type ty in module
// mi, into what the engine reads, and reports false if the pattern does not fit the
// type by shape (see the top of the file): Check has said what is wrong with it. It
// recurses once per level of nesting of the pattern, which the parser bounds
// (MaxPatternDepth); a tuple, the arguments of a constructor, the fields of a
// record and the alternatives of an or-pattern are loops.
func (p *patternPass) lower(mi int, id syntax.PatID, ty TypeID) (*dpat, bool) {
	m := p.mods[mi]
	pat := m.Pats.Get(id)
	ty = p.typed.Types.Resolve(ty)
	n := p.typed.Types.Node(ty)
	switch pat.Kind {
	case syntax.PatWildcard, syntax.PatBind:
		return wildPat, true
	case syntax.PatLit:
		switch {
		case pat.Lit == syntax.LitInt && n.Kind == TypeBuiltin && n.Builtin == BuiltinInt:
			return ctorPat(pat.Int, nil), true
		case pat.Lit == syntax.LitBool && n.Kind == TypeBuiltin && n.Builtin == BuiltinBool:
			key := int64(0)
			if pat.Bool {
				key = 1
			}
			return ctorPat(key, nil), true
		}
		return nil, false
	case syntax.PatTuple:
		elems := m.PatList(pat.Args)
		if n.Kind != TypeTuple || len(n.Params) != len(elems) {
			return nil, false
		}
		args, ok := p.lowerAll(mi, elems, n.Params)
		if !ok {
			return nil, false
		}
		return ctorPat(0, args), true
	case syntax.PatCtor:
		if pat.Record {
			return p.lowerRecord(mi, id, pat, n)
		}
		return p.lowerCtor(mi, id, pat, n)
	case syntax.PatOr:
		alts := make([]*dpat, 0, pat.Args.Count)
		for _, alt := range m.PatList(pat.Args) {
			d, ok := p.lower(mi, alt, ty)
			if !ok {
				return nil, false
			}
			alts = append(alts, d)
		}
		return orPat(alts), true
	}
	// PatError, and the list patterns, which have no type until the prelude has one
	// (Check reports every one as E0401).
	return nil, false
}

// lowerAll lowers each of the patterns ids against the type of the same place in tys.
func (p *patternPass) lowerAll(mi int, ids []syntax.PatID, tys []TypeID) ([]*dpat, bool) {
	args := make([]*dpat, len(ids))
	for i, id := range ids {
		d, ok := p.lower(mi, id, tys[i])
		if !ok {
			return nil, false
		}
		args[i] = d
	}
	return args, true
}

// lowerCtor lowers the positional constructor pattern pat, `Some(x)` or `None`, whose
// value has the type node n.
func (p *patternPass) lowerCtor(mi int, id syntax.PatID, pat syntax.Pat, n Type) (*dpat, bool) {
	res := p.res.Modules[mi].PatRes[id]
	if res.Kind != ResDef || n.Kind != TypeData {
		return nil, false
	}
	ctor, ok := p.decls.CtorOf(res.Def)
	if !ok {
		return nil, false
	}
	c := p.decls.Ctors.Get(ctor)
	subs := p.mods[mi].PatList(pat.Args)
	if c.Owner != n.Def || len(subs) != len(c.Args) {
		return nil, false
	}
	args, ok := p.lowerAll(mi, subs, c.Args)
	if !ok {
		return nil, false
	}
	return ctorPat(int64(c.Tag), args), true
}

// lowerRecord lowers the record pattern pat, `Point { x, y: q, .. }`, whose value
// has the type node n. The fields go in the order of the record, and a field that the
// pattern leaves out, which `..` allows, is a wildcard. A pattern that names the
// record by another type, names a field twice or not at all, or leaves out a field
// without `..` is one that Check has reported.
func (p *patternPass) lowerRecord(mi int, id syntax.PatID, pat syntax.Pat, n Type) (*dpat, bool) {
	if n.Kind != TypeData || p.decls.Defs.Get(n.Def).Form != syntax.TypeFormRecord {
		return nil, false
	}
	res, found := p.res.LookupType(ModuleIdx(mi), pat.Name)
	if !found || res.Kind != ResDef {
		return nil, false
	}
	named, ok := p.decls.TypeDefOf(res.Def)
	if !ok || p.decls.Meaning(named) != p.typed.Types.Data(n.Def, nil) {
		return nil, false
	}
	def := p.decls.Defs.Get(n.Def)
	args := make([]*dpat, len(def.Fields))
	given := 0
	for _, f := range p.mods[mi].FieldPats(pat.Fields) {
		i, ok := p.decls.FieldIndex(n.Def, f.Name)
		if !ok || args[i] != nil {
			return nil, false
		}
		args[i] = wildPat
		given++
		if f.HasPat {
			d, ok := p.lower(mi, f.Pat, def.Fields[i].Type)
			if !ok {
				return nil, false
			}
			args[i] = d
		}
	}
	if given != len(def.Fields) && !pat.HasRest {
		return nil, false
	}
	for i := range args {
		if args[i] == nil {
			args[i] = wildPat
		}
	}
	return ctorPat(0, args), true
}

// checkMatch asks the questions of one `match` e of module mi.
func (p *patternPass) checkMatch(mi int, e syntax.Expr) {
	if p.sink.Truncated() {
		return
	}
	scrutinee := p.typed.Types.Resolve(p.typed.Modules[mi].ExprTypes[e.Scrutinee])
	if scrutinee == p.errT {
		return
	}
	arms := p.mods[mi].Arms(e.Arms)
	if slices.ContainsFunc(arms, func(arm syntax.MatchArm) bool { return arm.Broken }) {
		return
	}
	pats := make([]*dpat, len(arms))
	for i, arm := range arms {
		d, ok := p.lower(mi, arm.Pat, scrutinee)
		if !ok {
			return
		}
		pats[i] = d
	}
	at := p.mods[mi].Exprs.Get(e.Scrutinee).Span
	eng := &engine{shapes: p, budget: patternBudget}
	// Coverage first: it is the question that has to be answered, and a match that
	// ends with an arm of `_` or of a variable is covered with no search at all.
	all := newArmMatrix(eng, scrutinee)
	for i, arm := range arms {
		if !arm.HasGuard && !all.add(pats[i]) {
			p.tooComplex(at, "match")
			return
		}
	}
	witnesses, ok := all.missing(maxWitnesses + 1)
	if !ok {
		p.tooComplex(at, "match")
		return
	}
	if len(witnesses) > 0 {
		p.nonExhaustive(at, witnesses, eng.intInFirst(maxWitnesses))
	}
	// Then the arms that no value reaches, with what is left of the budget. It is a
	// warning, so a match whose arms take longer to compare than the budget is not
	// an error: the search stops, and the arms after the one it stopped at are not
	// looked at.
	above := newArmMatrix(eng, scrutinee)
	for i, arm := range arms {
		useful, ok := above.useful(pats[i])
		if !ok {
			return
		}
		if !useful {
			p.unreachable(arm, p.mods[mi].Pats.Get(arm.Pat).Span)
			// It matches nothing that the arms above do not, so adding it would
			// change no answer: leaving it out keeps a match that repeats an arm a
			// hundred thousand times from growing the matrix with each copy.
			continue
		}
		if !arm.HasGuard && !above.add(pats[i]) {
			return
		}
	}
}

// checkLet asks the one question of the `let` s of module mi that has a pattern: does
// it match every value of its initializer?
func (p *patternPass) checkLet(mi int, s syntax.Stmt) {
	if p.sink.Truncated() {
		return
	}
	ty := p.typed.Types.Resolve(p.typed.Modules[mi].PatTypes[s.Pat])
	if ty == p.errT {
		return
	}
	d, ok := p.lower(mi, s.Pat, ty)
	if !ok {
		return
	}
	at := p.mods[mi].Pats.Get(s.Pat).Span
	eng := &engine{shapes: p, budget: patternBudget}
	matrix := newArmMatrix(eng, ty)
	if !matrix.add(d) {
		p.tooComplex(at, "let")
		return
	}
	witnesses, ok := matrix.missing(maxWitnesses + 1)
	if !ok {
		p.tooComplex(at, "let")
		return
	}
	if len(witnesses) > 0 {
		p.refutable(at, witnesses)
	}
}

// witnessList writes witnesses as a list of words for a message: `a`, `a` and `b`,
// `a`, `b` and `c`, and, if there were more than the message shows,
// `a`, `b`, `c` and more. The engine finds one more than maxWitnesses for this: it
// does not count them, because the number of values a match leaves out is not
// canonical (it depends on where the algorithm splits).
func witnessList(witnesses []string) string {
	shown := witnesses[:min(len(witnesses), maxWitnesses)]
	names := make([]string, len(shown))
	for i, w := range shown {
		names[i] = "`" + w + "`"
	}
	if len(witnesses) > maxWitnesses {
		return strings.Join(names, ", ") + " and more"
	}
	return joinNames(names)
}

// nonExhaustive reports E0701 for the `match` whose scrutinee is at at and that does
// not cover the values that witnesses are patterns of. An `Int` literal in a witness
// is a number that no arm reaching that place names, and the next one is the one
// after it: no list of arms ends, so the help says that a `_` does. literal is true
// when one of the witnesses that the message shows has such a number: the one that
// "and more" stands for is not shown, and the reader could not see why a `_` is
// needed.
func (p *patternPass) nonExhaustive(at base.Span, witnesses []string, literal bool) {
	list := witnessList(witnesses)
	label, help := "pattern "+list+" not covered", "add an arm for "+list
	if len(witnesses) > 1 {
		label, help = "patterns "+list+" not covered", "add arms for "+list
	}
	if literal {
		help += ", or end the `match` with a `_` arm"
	}
	p.sink.Report(diag.NewError(diag.E(701), "non-exhaustive match", diag.Label{Span: at, Message: label}).
		WithNote("a `match` must cover every value; guards do not count").
		WithHelp(help))
}

// refutable reports E0702 for the `let` whose pattern is at at and that does not
// match the values that witnesses are patterns of.
func (p *patternPass) refutable(at base.Span, witnesses []string) {
	label := witnessList(witnesses) + " is not covered"
	if len(witnesses) > 1 {
		label = witnessList(witnesses) + " are not covered"
	}
	p.sink.Report(diag.NewError(diag.E(702), "refutable pattern in `let`", diag.Label{Span: at, Message: label}).
		WithNote("a `let` binds every value, so its pattern must match all of them").
		WithHelp("use `match` to handle the other cases"))
}

// unreachable reports W0701 for the arm that matches no value that the arms above
// it do not, whose pattern is at at. The help is to remove the arm, with the edit
// that does it when the arm stays on one line.
func (p *patternPass) unreachable(arm syntax.MatchArm, at base.Span) {
	d := diag.Diagnostic{
		Severity: diag.Warning, Code: diag.W(701), Message: "unreachable pattern",
		Primary: diag.Label{Span: at, Message: "covered by the arms above"},
		Help:    "remove this arm",
	}
	if arm.RemovalOneLine {
		d = d.WithFixIt(diag.Delete(arm.Removal))
	}
	p.sink.Report(d)
}

// tooComplex reports E0707 for the `match` (what is "match") or `let` whose
// scrutinee or pattern is at at, which the engine gave up on. The help is the way
// out that the construct has: a `match` can be split into nested ones or end with
// a `_` arm, and a `let`, which has no arms, can write `_` where every value
// matches or be taken apart with `match`es of a few values each.
func (p *patternPass) tooComplex(at base.Span, what string) {
	where := "checking it needs"
	note := "finding a value that no arm matches is NP-complete: a `match` on many booleans can take forever"
	help := "split it into nested `match`es, or end it with a `_` arm"
	if what == "let" {
		where = "checking its pattern needs"
		note = "finding a value that the pattern does not match is NP-complete: a pattern over many booleans can take forever"
		help = "write `_` where every value matches, or take the value apart with `match`es of a few values each"
	}
	p.sink.Report(diag.NewError(diag.E(707), "this `"+what+"` is too complex to check",
		diag.Label{Span: at, Message: where + " more than " + spaced(patternBudget) + " steps"}).
		WithNote(note).
		WithHelp(help))
}

// spaced writes n with a space between each three digits, as the book writes
// them: 10 000 000.
func spaced(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + " " + s[i:]
	}
	return s
}
