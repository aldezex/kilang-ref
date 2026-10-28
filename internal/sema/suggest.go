package sema

import (
	"maps"
	"slices"

	"github.com/aldezex/kilang/internal/base"
)

// suggest returns the name in candidates that is nearest to name, for the help
// line `did you mean …?` of an unknown name. The nearest is the one with the
// smallest base.EditDistance, and a name is only near enough if that distance is
// at most max(1, len(name)/3): one edit for a short name, and one more for each
// three bytes of a longer one, so that `fibb` suggests `fib` but a long name
// does not suggest every short one. If several are equally near the one that
// comes first in the alphabet wins, wherever it is in the list. It returns false
// if no candidate is near enough.
//
// A candidate whose length differs from the name's by more than the limit is
// out of reach and is skipped without measuring it: each edit changes the length
// by one at most, so the distance is at least the difference.
func suggest(name string, candidates []string) (string, bool) {
	limit := max(1, len(name)/3)
	best, bestDistance, found := "", 0, false
	for _, c := range candidates {
		if len(c) > len(name)+limit || len(name) > len(c)+limit {
			continue
		}
		d := base.EditDistance(name, c)
		if d > limit {
			continue
		}
		if !found || d < bestDistance || (d == bestDistance && c < best) {
			best, bestDistance, found = c, d, true
		}
	}
	return best, found
}

// candidates returns the names that the unknown name being resolved could have
// meant: the ones name looks for before it gives up, that is, the local
// variables visible at this place, the definitions of the module being walked
// and the `pub` definitions of the prelude. They come sorted and each one once,
// so the list does not depend on the order they were gathered in.
func (r *resolver) candidates() []string {
	// A set, because a variable can have the name of a function and then the
	// name is one candidate, not two. Its keys come out in a Go map's order,
	// which changes from run to run, so slices.Sorted puts them in order.
	set := make(map[string]bool)
	for sym := range r.scope.visible() {
		set[r.in.String(sym)] = true
	}
	// The definitions come from the list of all of them, in id order, and not
	// from a walk over a map of tops. A repeated definition is skipped: the name
	// stands for the first one, and lookupDef looks at that one's `pub`.
	for id, def := range r.rp.Defs.All() {
		if r.tops[def.Module][def.Name] != id {
			continue
		}
		if def.Module == r.mod || (def.Module == preludeModule && def.Pub) {
			set[r.in.String(def.Name)] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}
