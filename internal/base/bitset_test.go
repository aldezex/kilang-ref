package base

import (
	"math/rand/v2"
	"testing"
)

func TestBitSet(t *testing.T) {
	t.Run("members_and_count", func(t *testing.T) {
		// A set of 130 integers takes three words. The members chosen are on each
		// side of the boundaries between words (63 and 64, 127 and 128) and at both
		// ends; Has is true for exactly them, Count is how many they are, adding one
		// again changes nothing, and Len is the size the set was made with, whatever
		// the members.
		const n = 130
		set := NewBitSet(n)
		members := []int{0, 1, 63, 64, 65, 127, 128, 129}
		if set.Len() != n || set.Count() != 0 {
			t.Fatalf("a new set has Len %d and Count %d, want %d and 0", set.Len(), set.Count(), n)
		}
		for i, m := range members {
			set.Set(m)
			set.Set(m)
			if got := set.Count(); got != i+1 {
				t.Errorf("after adding %d (twice) Count is %d, want %d", m, got, i+1)
			}
		}
		for i := range n {
			want := false
			for _, m := range members {
				want = want || m == i
			}
			if got := set.Has(i); got != want {
				t.Errorf("Has(%d) = %v, want %v", i, got, want)
			}
		}
		if set.Len() != n {
			t.Errorf("Len is %d after adding members, want %d", set.Len(), n)
		}
		copied := set
		copied.Set(2)
		if !set.Has(2) {
			t.Error("a copy of a set does not share its words: the doc comment says it does")
		}
	})

	t.Run("sizes", func(t *testing.T) {
		// The number of words is the size rounded up: a set of 64 has one, a set
		// of 65 two, and the last integer of each can be set. The zero value, and a set
		// of none, hold nothing.
		for _, n := range []struct{ size, words int }{{0, 0}, {1, 1}, {63, 1}, {64, 1}, {65, 2}, {128, 2}, {129, 3}} {
			set := NewBitSet(n.size)
			if len(set.words) != n.words {
				t.Errorf("size %d: %d words, want %d", n.size, len(set.words), n.words)
			}
			if n.size == 0 {
				continue
			}
			set.Set(n.size - 1)
			if !set.Has(n.size-1) || set.Count() != 1 {
				t.Errorf("size %d: the last integer is not a member alone", n.size)
			}
		}
		for name, set := range map[string]BitSet{"zero value": {}, "size 0": NewBitSet(0)} {
			if set.Len() != 0 || set.Count() != 0 {
				t.Errorf("%s: Len %d and Count %d, want 0 and 0", name, set.Len(), set.Count())
			}
			if _, ok := set.NextClear(0); ok {
				t.Errorf("%s: NextClear(0) found an integer", name)
			}
		}
	})

	t.Run("out_of_range_panics", func(t *testing.T) {
		// An integer that the set cannot hold is a bug of the caller, for Set and for
		// Has (an answer of false would hide it): both ends, and a size that is
		// negative.
		const want = "base: BitSet index out of range"
		set := NewBitSet(70)
		for _, bad := range []int{-1, 70, 1000} {
			mustPanic(t, "Set", want, func() { set.Set(bad) })
			mustPanic(t, "Has", want, func() { set.Has(bad) })
		}
		// The two ends that are in range are added, and nothing else is.
		set.Set(69)
		set.Set(0)
		if !set.Has(0) || !set.Has(69) || set.Has(1) || set.Has(68) || set.Count() != 2 {
			t.Errorf("after Set(0) and Set(69) Has(0) is %v, Has(69) %v, Has(1) %v, Has(68) %v and Count %d, want true, true, false, false and 2",
				set.Has(0), set.Has(69), set.Has(1), set.Has(68), set.Count())
		}
		mustPanic(t, "Set on the zero value", want, func() { var z BitSet; z.Set(0) })
		mustPanic(t, "Has on the zero value", want, func() { var z BitSet; z.Has(0) })
		mustPanic(t, "NewBitSet(-1)", "base: NewBitSet of a negative size", func() { NewBitSet(-1) })
	})

	t.Run("next_clear", func(t *testing.T) {
		// NextClear is the smallest integer at or after from that is below Len and is
		// not a member. Each size has random sets, with a fixed seed, from empty to
		// full, and every from from before 0 to past the end is compared with a loop
		// over the integers. The sizes are on each side of the boundaries of words,
		// where the clear bits of the last word that are past Len must not be taken
		// for integers of the set.
		r := rand.New(rand.NewPCG(3, 4))
		for _, n := range []int{1, 2, 63, 64, 65, 70, 128, 130} {
			for round := range 40 {
				set := NewBitSet(n)
				// A density that goes from nearly empty to full, and one round in
				// which every integer is a member.
				for i := range n {
					if round == 39 || r.IntN(40) < round {
						set.Set(i)
					}
				}
				for from := -2; from <= n+2; from++ {
					want, found := 0, false
					for i := max(from, 0); i < n; i++ {
						if !set.Has(i) {
							want, found = i, true
							break
						}
					}
					if got, ok := set.NextClear(from); got != want || ok != found {
						t.Fatalf("size %d, round %d, NextClear(%d) = %d, %v, want %d, %v", n, round, from, got, ok, want, found)
					}
				}
			}
		}
	})
}
