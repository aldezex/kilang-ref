package base

import (
	"bytes"
	"hash/maphash"
)

// Symbol identifies one text interned by an Interner. Two texts interned by the
// same Interner are equal exactly when their symbols are equal, so comparing
// names costs one integer comparison. Symbols of different Interners are
// unrelated: never compare them.
type Symbol uint32

// NoSymbol is the zero Symbol. No text is ever interned as NoSymbol: the first
// interned text is Symbol 1, so a zeroed field means "no symbol yet".
const NoSymbol Symbol = 0

// Interner stores texts in an Arena and finds them with hash/maphash. Intern
// never allocates for bytes it already knows (InternString may allocate for the
// []byte conversion).
//
// Several texts can share a hash. index keeps the first symbol of each hash and
// next[sym] chains the other symbols with the same hash, so equality is never
// decided by the hash alone: bytes.Equal against the stored copy settles it.
// The stored copy lives in the arena, except for the empty text, which is
// stored as an empty slice that uses no arena memory.
//
// Symbols are numbered in the order the texts are first interned, so nothing
// observable depends on the random maphash seed. An Interner never checks that
// it holds fewer than 2^32 texts: the compiler stays far below that, and Intern
// would hand out a wrapped Symbol past it. An Interner is not safe for
// concurrent use.
type Interner struct {
	arena *Arena
	seed  maphash.Seed
	index map[uint64]Symbol // hash -> first symbol with that hash
	next  []Symbol          // next[sym] -> next symbol with the same hash, or NoSymbol; slot 0 is unused
	texts [][]byte          // texts[sym] -> the stored bytes, cap == len; slot 0 is unused
	// hash is a field so that a test can force collisions. NewInterner sets it
	// to maphash.Bytes with the interner's own seed.
	hash func([]byte) uint64
}

// NewInterner returns an empty Interner.
func NewInterner() *Interner {
	in := &Interner{
		arena: NewArena(0),
		seed:  maphash.MakeSeed(),
		index: make(map[uint64]Symbol),
		// Slot 0 belongs to NoSymbol, so the first real symbol is 1 and a
		// symbol can index next and texts directly.
		next:  []Symbol{NoSymbol},
		texts: [][]byte{nil},
	}
	// The closure reads in.seed, so the field is the one place the seed lives.
	in.hash = func(b []byte) uint64 { return maphash.Bytes(in.seed, b) }
	return in
}

// find returns the symbol of b if it is already interned, together with the
// hash of b and the last symbol of b's chain (NoSymbol when no text has that
// hash). Intern uses the last symbol to append to the chain. It is meaningful
// only when sym == NoSymbol: on a hit, find stops at the match and last is just
// the symbol visited before it.
func (in *Interner) find(b []byte) (sym Symbol, h uint64, last Symbol) {
	h = in.hash(b)
	for s := in.index[h]; s != NoSymbol; s = in.next[s] {
		if bytes.Equal(in.texts[s], b) {
			return s, h, last
		}
		last = s
	}
	return NoSymbol, h, last
}

// Intern returns the symbol of b, adding a copy of b to the arena the first
// time it is seen (the empty text is stored as an empty slice that uses no
// arena memory). The caller keeps ownership of b: changing it afterwards does
// not change the stored text. Intern allocates nothing when b is already known.
func (in *Interner) Intern(b []byte) Symbol {
	sym, h, last := in.find(b)
	if sym != NoSymbol {
		return sym
	}
	sym = Symbol(len(in.texts))
	var stored []byte
	if len(b) == 0 {
		// Copy would return nil for the empty text. Store a non-nil empty slice
		// instead, so that Bytes never returns nil for a symbol that Intern
		// handed out, "" included. It needs no arena memory.
		stored = []byte{}
	} else {
		stored = in.arena.Copy(b)
	}
	in.texts = append(in.texts, stored)
	in.next = append(in.next, NoSymbol)
	if last == NoSymbol {
		in.index[h] = sym
	} else {
		in.next[last] = sym
	}
	return sym
}

// InternString is Intern for a string. Converting s to []byte may allocate.
func (in *Interner) InternString(s string) Symbol {
	return in.Intern([]byte(s))
}

// Lookup returns the symbol of b and true if b was interned before. It never
// inserts: for an unknown text it returns NoSymbol and false.
func (in *Interner) Lookup(b []byte) (Symbol, bool) {
	sym, _, _ := in.find(b)
	return sym, sym != NoSymbol
}

// Bytes returns the text of s as a view of the stored copy (for the empty
// text, an empty slice outside the arena), without copying. The view has
// cap == len, so appending to it reallocates instead of overwriting the next
// text; the caller must still not modify its elements. The view is never nil
// for a symbol Intern returned, "" included. s must be a symbol this Interner
// returned; for any other value the result is undefined.
func (in *Interner) Bytes(s Symbol) []byte {
	return in.texts[s]
}

// String returns the text of s as a new string (it copies). Use it for
// messages and dumps; use Bytes in hot paths.
func (in *Interner) String(s Symbol) string {
	return string(in.Bytes(s))
}

// Len returns the number of distinct texts interned so far.
func (in *Interner) Len() int {
	return len(in.texts) - 1
}
