package sema

import (
	"iter"
	"slices"
	"testing"

	"github.com/aldezex/kilang/internal/base"
)

// firstN returns the first n names of seq and then stops asking for more.
func firstN(seq iter.Seq[base.Symbol], n int) []base.Symbol {
	var got []base.Symbol
	for name := range seq {
		got = append(got, name)
		if len(got) == n {
			break
		}
	}
	return got
}

func TestScope(t *testing.T) {
	t.Run("visible_lists_innermost_first_once", func(t *testing.T) {
		// Four scopes, from the outside in. The names are Symbols written as
		// plain numbers, and every scope is written out of order, so that no
		// rotation of it is sorted: a map that was read directly would show it.
		// Symbol 5 is bound in the three scopes and symbol 7 in two; the
		// innermost scope has no names at all.
		outer := &scope{names: map[base.Symbol]LocalID{7: 0, 3: 1, 9: 2, 5: 3}}
		middle := &scope{parent: outer, names: map[base.Symbol]LocalID{6: 4, 2: 5, 5: 6, 8: 7}}
		inner := &scope{parent: middle, names: map[base.Symbol]LocalID{4: 8, 1: 9, 7: 10, 5: 11}}
		empty := &scope{parent: inner}

		// Each name once, at the place where it is closest: 5 and 7 come with
		// the innermost scope that binds them, not with the outer ones.
		want := []base.Symbol{1, 4, 5, 7, 2, 6, 8, 3, 9}
		// A Go map is read in a different order each time, so one pass could
		// come out right by chance: read it again and again.
		for i := range 50 {
			if got := slices.Collect(empty.visible()); !slices.Equal(got, want) {
				t.Fatalf("pass %d: visible names %v, want %v", i, got, want)
			}
		}
		if got := slices.Collect(inner.visible()); !slices.Equal(got, want) {
			t.Errorf("visible names of a scope that binds names: %v, want %v", got, want)
		}
		// From the middle scope the inner names are not visible, and 7 is the
		// outer one.
		if got, wantMiddle := slices.Collect(middle.visible()), []base.Symbol{2, 5, 6, 8, 3, 7, 9}; !slices.Equal(got, wantMiddle) {
			t.Errorf("visible names of the middle scope: %v, want %v", got, wantMiddle)
		}

		// The caller can stop early: in the middle of a scope, at the end of
		// one and after taking everything.
		for _, n := range []int{1, 2, 4, 5, len(want), len(want) + 3} {
			if got, wantN := firstN(empty.visible(), n), want[:min(n, len(want))]; !slices.Equal(got, wantN) {
				t.Errorf("first %d visible names: %v, want %v", n, got, wantN)
			}
		}
	})
}
