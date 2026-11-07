package base

import "math/bits"

// BitSet is a set of the integers 0 to Len()-1, one bit for each, packed 64 to a
// word of a []uint64. It is what a pass uses when the members are small numbers it
// can count in advance: the exhaustiveness check keeps in one the constructors of a
// type that the arms of a `match` have named, so that it asks "are they all
// here?" with one popcount per word and not with a loop over the arms.
//
// The zero value is a set of no integers, and NewBitSet makes one of a given size.
// A BitSet does not grow: a number outside 0..Len()-1 is a bug of the caller, and
// Set and Has panic on it. A copy of a BitSet shares its words with the original.
type BitSet struct {
	words []uint64
	n     int
}

// NewBitSet returns an empty set of the integers 0 to n-1. It panics if n is
// negative.
func NewBitSet(n int) BitSet {
	if n < 0 {
		panic("base: NewBitSet of a negative size")
	}
	return BitSet{words: make([]uint64, (n+63)/64), n: n}
}

// Len returns how many integers the set can hold: the n of NewBitSet. It is not
// the number of members, which Count returns.
func (s *BitSet) Len() int { return s.n }

// Set adds i to the set. It panics if i is outside 0..Len()-1.
func (s *BitSet) Set(i int) {
	s.check(i)
	s.words[i/64] |= 1 << (uint(i) % 64)
}

// Has reports whether i is in the set. It panics if i is outside 0..Len()-1,
// because a membership test on a number that cannot be a member is a bug of the
// caller, and an answer of false would hide it.
func (s *BitSet) Has(i int) bool {
	s.check(i)
	return s.words[i/64]&(1<<(uint(i)%64)) != 0
}

// Count returns the number of members: the bits that are set.
func (s *BitSet) Count() int {
	n := 0
	for _, w := range s.words {
		n += bits.OnesCount64(w)
	}
	return n
}

// NextClear returns the smallest integer that is at least from, is below Len() and
// is not in the set, and whether there is one. A negative from starts at 0. It
// reads a word of 64 integers at a time, so walking the integers that are missing
// from a set that is nearly full takes a step for each of them and not for each
// integer of the set.
func (s *BitSet) NextClear(from int) (int, bool) {
	from = max(from, 0)
	for w := from / 64; w < len(s.words); w++ {
		free := ^s.words[w]
		if w == from/64 {
			free &^= 1<<(uint(from)%64) - 1 // the integers below from do not count
		}
		if free == 0 {
			continue
		}
		// The last word may have bits past Len() that were never set: they are
		// clear, but they are not integers of the set.
		if i := w*64 + bits.TrailingZeros64(free); i < s.n {
			return i, true
		}
		return 0, false
	}
	return 0, false
}

// check panics if i is not an integer that the set can hold.
func (s *BitSet) check(i int) {
	if i < 0 || i >= s.n {
		panic("base: BitSet index out of range")
	}
}
