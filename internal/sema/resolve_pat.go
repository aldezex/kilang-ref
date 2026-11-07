package sema

import (
	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// This file is how Resolve reads patterns: the variables that they bind, one Local
// each, and what the name of a constructor stands for. They are in a file of their
// own, and not in resolve.go, because they are one idea, and the two diagnostics
// that go with it, E0703 and E0704, are about a pattern and not about a name.
//
// A pattern binds in the scope that is current when it is bound: the scope of its
// arm, or the block of its `let`. The walk over a pattern recurses once per level
// of nesting, which the parser bounds (MaxPatternDepth); a tuple, a list, the
// arguments of a constructor and the alternatives of an or-pattern are loops, so
// a pattern of a hundred thousand elements is as deep as one of two. A tree built
// by hand is not bounded.

// patBinding is where a pattern binds a name, and the variable it binds there.
type patBinding struct {
	span  base.Span
	local LocalID
}

// patNames is the names that one part of a pattern binds, which the part around it
// may not bind again. The pattern as a whole is one, and each alternative of an
// or-pattern is another, whose outer is the part that holds the or-pattern: the
// alternatives do not see each other's names (they bind the same ones), and each
// sees what was bound before the or-pattern.
type patNames struct {
	bound map[base.Symbol]patBinding
	// order lists the names of bound in the order they were bound, so that nothing
	// depends on the order of a Go map.
	order []base.Symbol
	outer *patNames
	// union is, for an alternative of an or-pattern, the names that the alternatives
	// before it have bound: where it finds the variable of a name that they
	// bound, to bind the same one. It is nil for a part that is not an alternative.
	union map[base.Symbol]patBinding
}

// find returns where the name is bound in n or in the parts around it.
func (n *patNames) find(name base.Symbol) (patBinding, bool) {
	for part := n; part != nil; part = part.outer {
		if b, ok := part.bound[name]; ok {
			return b, true
		}
	}
	return patBinding{}, false
}

// shared returns the variable that an alternative before this one bound for name,
// looking at the or-patterns that hold n, and whether there is one.
func (n *patNames) shared(name base.Symbol) (LocalID, bool) {
	for part := n; part != nil; part = part.outer {
		if b, ok := part.union[name]; ok {
			return b.local, true
		}
	}
	return 0, false
}

// add records that n binds name as b.
func (n *patNames) add(name base.Symbol, b patBinding) {
	if n.bound == nil {
		n.bound = make(map[base.Symbol]patBinding)
	}
	n.bound[name] = b
	n.order = append(n.order, name)
}

// arm resolves one arm of a match: a scope for it, in which its pattern binds its
// variables and then the guard and the body are walked, so that all of them see
// the variables and no other arm does.
func (r *resolver) arm(arm syntax.MatchArm) {
	outer := r.scope
	r.scope = &scope{parent: outer}
	r.bindPattern(arm.Pat)
	if arm.HasGuard {
		r.expr(arm.Guard)
	}
	r.expr(arm.Body)
	r.scope = outer
}

// bindPattern binds the variables of the pattern id in the current scope.
func (r *resolver) bindPattern(id syntax.PatID) {
	r.pat(id, &patNames{})
}

// pat walks the pattern id, which binds in n: the variables of a PatBind and of the
// pun of a record pattern, in the order they are written, and what the name of a
// constructor stands for. The kinds that hold patterns walk them, an
// or-pattern by its own rule (see orPattern).
func (r *resolver) pat(id syntax.PatID, n *patNames) {
	p := r.m.Pats.Get(id)
	switch p.Kind {
	case syntax.PatWildcard, syntax.PatLit, syntax.PatError:
		// Nothing is bound, and a PatError has been reported by the parser.
	case syntax.PatBind:
		r.res.PatLocal[id] = r.bindName(p.Name, p.NameSpan, n)
	case syntax.PatCtor:
		if p.Record {
			for i, f := range r.m.FieldPats(p.Fields) {
				if f.HasPat {
					r.pat(f.Pat, n)
				} else {
					r.res.FieldPatLocal[int(p.Fields.First)+i] = r.bindName(f.Name, f.Span, n)
				}
			}
			return
		}
		r.res.PatRes[id] = r.ctorRes(p)
		for _, arg := range r.m.PatList(p.Args) {
			r.pat(arg, n)
		}
	case syntax.PatTuple:
		for _, el := range r.m.PatList(p.Args) {
			r.pat(el, n)
		}
	case syntax.PatList:
		for _, el := range r.m.PatList(p.Args) {
			r.pat(el, n)
		}
		if p.HasRest {
			r.pat(p.Rest, n)
		}
	case syntax.PatOr:
		r.orPattern(p, n)
	default:
		panic("unreachable")
	}
}

// ctorRes says what the name of the constructor pattern p stands for: the
// constructor, or a ResError if the module knows the name but there is nothing to
// resolve it to (the parser could not read the type), or, for a name that stands
// for nothing, E0306 as for the same name in an expression.
func (r *resolver) ctorRes(p syntax.Pat) Res {
	if res, ok := r.lookupDef(r.mod, p.Name); ok {
		return res
	}
	r.unknownCtor(p.NameSpan, r.in.String(p.Name))
	return Res{Kind: ResError}
}

// bindName binds the name that a pattern binds at span, in n and in the current
// scope, and returns its variable. A name that n, or the part around it, has
// bound already is E0704 at span, with the first binding as the secondary label,
// and the variable is a new one that no name stands for, so the body keeps seeing
// the first. A name that an alternative before this one bound is not a repeat
// (every alternative binds it), and has the same variable.
func (r *resolver) bindName(name base.Symbol, span base.Span, n *patNames) LocalID {
	if first, again := n.find(name); again {
		r.sink.Report(twiceBoundError(r.in.String(name), span, first.span))
		return r.rp.Locals.Push(Local{Name: name, Span: span, Owner: r.owner})
	}
	local, shared := n.shared(name)
	if !shared {
		local = r.bind(name, span)
	}
	n.add(name, patBinding{span: span, local: local})
	return local
}

// twiceBoundError is the E0704 for the name that a pattern binds again at again,
// after binding it at first. There is no help: which of the two parts the name
// was meant for is not for the compiler to say.
func twiceBoundError(name string, again, first base.Span) diag.Diagnostic {
	return diag.NewError(diag.E(704), "pattern binds `"+name+"` twice", diag.Label{Span: again, Message: "bound again here"}).
		WithSecondary(diag.Label{Span: first, Message: "first bound here"}).
		WithNote("a pattern binds each name once; to compare two parts, bind them apart and use a guard")
}

// orPattern walks the or-pattern p, which binds in n. Every alternative binds the
// same names, so that the body can use them whichever matched: each name is one
// variable, bound by the first alternative that has it, and the others reuse it.
// A name that some alternative does not bind is one E0703, on the first alternative
// that lacks it, with the first that binds it as the secondary label; the name
// stays bound for the body, which then adds no unknown name to the one mistake.
// The names of the whole or-pattern are then n's, for what comes after it.
//
// Which alternative lacks a name first is found as the alternatives are walked, in
// one pass: pending holds the names that every alternative so far binds, so each
// alternative looks only at those, never at all the names of the union, and a name
// that an alternative lacks leaves pending for good. The cost is the size of the
// pattern, and not its number of alternatives times its number of names (an
// or-pattern of 100 000 alternatives that each bind a name of their own would take
// a minute).
func (r *resolver) orPattern(p syntax.Pat, n *patNames) {
	alts := r.m.PatList(p.Args)
	union := make(map[base.Symbol]patBinding)
	var names []base.Symbol // the names of the union, in the order they were first bound
	binds := make(map[base.Symbol]int)
	lacks := make(map[base.Symbol]int)
	var pending []base.Symbol
	for i, alt := range alts {
		part := &patNames{outer: n, union: union}
		r.pat(alt, part)
		kept := pending[:0]
		for _, name := range pending {
			if _, ok := part.bound[name]; ok {
				kept = append(kept, name)
			} else {
				lacks[name] = i
			}
		}
		pending = kept
		for _, name := range part.order {
			if _, ok := union[name]; ok {
				continue
			}
			union[name] = part.bound[name]
			names = append(names, name)
			binds[name] = i
			if i == 0 {
				pending = append(pending, name)
			} else {
				// The first alternative came before the one that binds it first.
				lacks[name] = 0
			}
		}
	}
	for _, name := range names {
		if lacking, ok := lacks[name]; ok {
			r.sink.Report(orBindsError(r.in.String(name), r.m.Pats.Get(alts[lacking]).Span, r.m.Pats.Get(alts[binds[name]]).Span))
		}
		n.add(name, union[name])
	}
}

// orBindsError is the E0703 for the name that the alternative at lacks does not
// bind and the one at binds does.
func orBindsError(name string, lacks, binds base.Span) diag.Diagnostic {
	return diag.NewError(diag.E(703), "or-pattern binds `"+name+"` in one alternative only",
		diag.Label{Span: lacks, Message: "`" + name + "` is not bound here"}).
		WithSecondary(diag.Label{Span: binds, Message: "`" + name + "` is bound here"}).
		WithNote("every alternative of `|` binds the same names, so the body can use them")
}
