package sema

import (
	"iter"
	"maps"
	"slices"

	"github.com/aldezex/kilang/internal/base"
)

// A scope is the set of local variables that a piece of a function can see.
// The parameters of a function are one scope, and each block `{ … }` inside
// the function is another scope that sits inside the one around it: the
// scopes of a function form a chain from the innermost block out to the
// parameters, linked by parent. A name that is bound in a scope is visible in
// that scope and in every scope inside it, until an inner scope binds the same
// name again: then the inner variable shadows the outer one.
//
// A scope only holds names. The Local that each name stands for is in
// ResolvedProgram.Locals, and Resolve fills the scopes in as it walks a body.
// The top-level definitions are not in any scope: Resolve looks for them only
// when the scopes have nothing by that name.
type scope struct {
	// names maps each name bound in this scope, and not in the scopes around
	// it, to its variable. A scope with no variables may leave it nil.
	names  map[base.Symbol]LocalID
	parent *scope
}

// bind makes name stand for id in s from now on. If s already binds name, the
// new variable replaces the old one in s: a `let x = 1` followed by another
// `let x = 2` in the same block leaves the second visible to the statements
// after it.
func (s *scope) bind(name base.Symbol, id LocalID) {
	if s.names == nil {
		s.names = make(map[base.Symbol]LocalID)
	}
	s.names[name] = id
}

// lookup returns the variable that name stands for in s: the one bound in the
// innermost scope that binds it, looking at s first and then at the scopes
// around it. It returns false if no scope binds name.
func (s *scope) lookup(name base.Symbol) (LocalID, bool) {
	for sc := s; sc != nil; sc = sc.parent {
		if id, ok := sc.names[name]; ok {
			return id, true
		}
	}
	return 0, false
}

// visible returns every name that s can see, once each, for the suggestions of
// the next session: the names of s first, then those of its parent that s does
// not bind, and so on outwards. The names of one scope come in increasing
// Symbol order, which is the order they were first interned, not alphabetical:
// the order depends on the program and never on the order of a Go map.
func (s *scope) visible() iter.Seq[base.Symbol] {
	return func(yield func(base.Symbol) bool) {
		seen := make(map[base.Symbol]bool)
		for sc := s; sc != nil; sc = sc.parent {
			for _, name := range slices.Sorted(maps.Keys(sc.names)) {
				if seen[name] {
					continue
				}
				seen[name] = true
				if !yield(name) {
					return
				}
			}
		}
	}
}
