package base

import (
	"bytes"
	"fmt"
	"testing"
	"unique"
)

func TestInterner(t *testing.T) {
	t.Run("same_text_same_symbol", func(t *testing.T) {
		in := NewInterner()
		a := in.Intern([]byte("alpha"))
		b := in.InternString("alpha")
		if a != b {
			t.Errorf("Intern and InternString of %q gave %d and %d, want the same symbol", "alpha", a, b)
		}
		if got := in.Len(); got != 1 {
			t.Errorf("Len after interning the same text twice = %d, want 1", got)
		}
	})

	t.Run("different_text_different_symbol", func(t *testing.T) {
		in := NewInterner()
		// "" and "a" and "ab" differ only in length; "ab" and "ba" only in order.
		texts := []string{"", "a", "ab", "ba", "b"}
		seen := map[Symbol]string{}
		for _, s := range texts {
			sym := in.InternString(s)
			if prev, dup := seen[sym]; dup {
				t.Errorf("%q and %q share the symbol %d", prev, s, sym)
			}
			seen[sym] = s
		}
		if got := in.Len(); got != len(texts) {
			t.Errorf("Len = %d, want %d", got, len(texts))
		}
	})

	t.Run("first_symbol_is_one", func(t *testing.T) {
		in := NewInterner()
		if got := in.Len(); got != 0 {
			t.Errorf("Len of a new Interner = %d, want 0", got)
		}
		first := in.InternString("x")
		second := in.InternString("y")
		if first != 1 || second != 2 {
			t.Errorf("first two symbols = %d and %d, want 1 and 2", first, second)
		}
	})

	t.Run("bytes_round_trip", func(t *testing.T) {
		in := NewInterner()
		texts := []string{"", "a", "fib", "año", "\x00\xff\xfe", "with space\n"}
		syms := make([]Symbol, len(texts))
		for i, s := range texts {
			syms[i] = in.InternString(s)
		}
		for i, s := range texts {
			if got := string(in.Bytes(syms[i])); got != s {
				t.Errorf("Bytes(%d) = %q, want %q", syms[i], got, s)
			}
			// Even the empty text has a view that is not nil.
			if in.Bytes(syms[i]) == nil {
				t.Errorf("Bytes(%d) of %q is nil, want a non-nil view", syms[i], s)
			}
			if got := in.String(syms[i]); got != s {
				t.Errorf("String(%d) = %q, want %q", syms[i], got, s)
			}
		}
		// The interner keeps its own copy: changing the caller's slice later
		// must not change the stored text.
		buf := []byte("abc")
		sym := in.Intern(buf)
		buf[0] = 'X'
		if got := in.String(sym); got != "abc" {
			t.Errorf("after changing the input, String = %q, want %q", got, "abc")
		}
	})

	t.Run("lookup_does_not_insert", func(t *testing.T) {
		in := NewInterner()
		if sym, ok := in.Lookup([]byte("ghost")); ok || sym != NoSymbol {
			t.Errorf("Lookup of an unknown text = (%d, %v), want (0, false)", sym, ok)
		}
		if got := in.Len(); got != 0 {
			t.Errorf("Len after a failed Lookup = %d, want 0", got)
		}
		// If Lookup had inserted, "ghost" would already be symbol 1.
		if got := in.InternString("other"); got != 1 {
			t.Errorf("first Intern after a failed Lookup = %d, want 1", got)
		}
		want := in.InternString("ghost")
		if sym, ok := in.Lookup([]byte("ghost")); !ok || sym != want {
			t.Errorf("Lookup of a known text = (%d, %v), want (%d, true)", sym, ok, want)
		}
	})

	t.Run("bytes_view_cannot_grow_into_neighbor", func(t *testing.T) {
		in := NewInterner()
		s1 := in.Intern([]byte("ab"))
		s2 := in.Intern([]byte("cd"))
		// Intern keeps its texts in the arena: two 2-byte texts take 4 bytes of
		// its first slab. Without this, an Intern that copied them with make
		// would pass every check below, because make also gives cap == len.
		if used, slabs := in.arena.used, in.arena.Slabs(); used != 4 || slabs != 1 {
			t.Errorf("after interning two 2-byte texts: arena used = %d bytes in %d slabs, want 4 bytes in 1", used, slabs)
		}
		if v := in.Bytes(s1); len(v) != 2 || cap(v) != 2 {
			t.Errorf("Bytes(s1): len = %d, cap = %d, want 2 and 2", len(v), cap(v))
		} else if &v[0] != &in.Bytes(s1)[0] {
			// Bytes exists so that nobody copies: two calls must give views
			// of the same stored bytes, not of two fresh copies.
			t.Errorf("two calls to Bytes(s1) returned views of different arrays, want the one stored text")
		}
		_ = append(in.Bytes(s1), 'X')
		if got := in.String(s2); got != "cd" {
			t.Errorf("String(s2) after appending to Bytes(s1) = %q, want %q", got, "cd")
		}
	})

	t.Run("colliding_hashes_are_distinguished", func(t *testing.T) {
		in := NewInterner()
		in.hash = func([]byte) uint64 { return 42 }
		texts := []string{"one", "two", "three"}
		syms := make([]Symbol, len(texts))
		for i, s := range texts {
			syms[i] = in.InternString(s)
		}
		if syms[0] == syms[1] || syms[0] == syms[2] || syms[1] == syms[2] {
			t.Fatalf("three colliding texts got symbols %v, want three different ones", syms)
		}
		for i, s := range texts {
			// Interning again must find the symbol at any depth of the chain.
			if got := in.InternString(s); got != syms[i] {
				t.Errorf("second Intern of %q = %d, want %d", s, got, syms[i])
			}
			if got, ok := in.Lookup([]byte(s)); !ok || got != syms[i] {
				t.Errorf("Lookup of %q = (%d, %v), want (%d, true)", s, got, ok, syms[i])
			}
			if got := in.String(syms[i]); got != s {
				t.Errorf("String(%d) = %q, want %q", syms[i], got, s)
			}
		}
		if _, ok := in.Lookup([]byte("four")); ok {
			t.Errorf("Lookup of an unknown text with the same hash found a symbol")
		}
		if got := in.Len(); got != 3 {
			t.Errorf("Len = %d, want 3", got)
		}

		// Texts that overlap must also stay apart under one hash. They are at
		// most 3 bytes long, and they catch an equality that stops short of the
		// whole text when it compares only the first (or only the last) 1 or 2
		// bytes, only what HasPrefix or HasSuffix compare, only half of the
		// text, or only the length and the first and last bytes. "" goes first,
		// so an equality that "" satisfies (every text starts and ends with it)
		// would merge the next text into it at once. "ab", "ac" and "aa" have
		// the same length and the same first byte; "aa" and "ba" the same
		// length and the same last byte; "abc" extends "ab"; and "axc", "xbc"
		// and "abx" are 3 bytes long like "abc" and differ from it in one byte
		// each (the middle, the first and the last one), so an equality that
		// ignores one byte of a 3-byte text, whichever it is, merges one of
		// them into "abc".
		p := NewInterner()
		p.hash = func([]byte) uint64 { return 42 }
		overlapping := []string{"", "ab", "abc", "ac", "aa", "ba", "axc", "xbc", "abx"}
		got := make([]Symbol, len(overlapping))
		for i, s := range overlapping {
			got[i] = p.InternString(s)
			if want := Symbol(i + 1); got[i] != want {
				t.Errorf("symbol of %q = %d, want the new symbol %d", s, got[i], want)
			}
		}
		if n := p.Len(); n != len(overlapping) {
			t.Errorf("Len after %d overlapping texts = %d, want %d", len(overlapping), n, len(overlapping))
		}
		// "a" starts "ab", "c" ends "abc" and "abcd" extends "abc", but none of
		// them was interned.
		for _, s := range []string{"a", "c", "abcd"} {
			if sym, ok := p.Lookup([]byte(s)); ok {
				t.Errorf("Lookup of %q before interning it = (%d, true), want not found", s, sym)
			}
		}
		for i, s := range overlapping {
			if sym, ok := p.Lookup([]byte(s)); !ok || sym != got[i] {
				t.Errorf("Lookup of %q = (%d, %v), want (%d, true)", s, sym, ok, got[i])
			}
		}

		// The empty text has a branch of its own in Intern, so ask for it
		// again, as a string and as nil: both must give the symbol "" already
		// has. An Intern that skipped the lookup for an empty text would make a
		// new symbol for every "" in a program.
		if again, fromNil := p.InternString(""), p.Intern(nil); again != got[0] || fromNil != got[0] {
			t.Errorf("Intern of \"\" again = %d and of nil = %d, want %d", again, fromNil, got[0])
		}

		// Eight-byte texts that differ in one letter each. The overlapping set
		// has no text longer than 3 bytes, so an equality that, between texts
		// of the same length, compares only the first 3 to 7 bytes, or only the
		// last 3 to 7, would pass it. Here "abcdefgh" goes first, and then
		// eight copies of it follow, each with the letter at one position
		// upper-cased: all are different texts, so each must get a new symbol.
		// An equality that compares only the first (or only the last) 3 to 7
		// bytes, that ignores the byte at any one position, or that ignores the
		// case of the letters, as bytes.EqualFold does, would merge one of them
		// into "abcdefgh".
		q := NewInterner()
		q.hash = func([]byte) uint64 { return 42 }
		text := []byte("abcdefgh")
		q.Intern(text)
		for i := range text {
			v := bytes.Clone(text)
			v[i] -= 'a' - 'A' // upper-case the letter at position i
			if sym, want := q.Intern(v), Symbol(i+2); sym != want {
				t.Errorf("symbol of %q = %d, want the new symbol %d", v, sym, want)
			}
		}
	})

	t.Run("default_hash_spreads_texts", func(t *testing.T) {
		const n = 300
		// Two families of texts, each like real identifiers. In the first the
		// texts share a long prefix and differ at the end, so a hash that read
		// only the first bytes of a text would put them all together. In the
		// second they share a long tail and differ at the start, so a hash that
		// read only the last bytes (or the last half) would.
		for _, family := range []struct{ format, what string }{
			{"identifier_%d", "texts"},
			{"%d_identifier", "texts with a common tail"},
		} {
			in := NewInterner()
			for i := 0; i < n; i++ {
				text := fmt.Sprintf(family.format, i)
				if got, want := in.InternString(text), Symbol(i+1); got != want {
					t.Fatalf("symbol of %s = %d, want %d", text, got, want)
				}
			}
			// With a real hash almost every text has a bucket of its own (a
			// collision among 300 64-bit hashes has odds near 1 in 10^15). A
			// hash that returned a constant would leave one bucket and one long
			// chain.
			if got := len(in.index); got < n*9/10 {
				t.Errorf("%d %s use only %d hash buckets, want nearly one each", n, family.what, got)
			}
			if got := in.Len(); got != n {
				t.Errorf("Len = %d, want %d", got, n)
			}
		}
	})

	t.Run("intern_existing_does_not_allocate", func(t *testing.T) {
		in := NewInterner()
		// The text is longer than 32 bytes on purpose. A hash written as
		// maphash.String(seed, string(b)) converts b to a string, and Go keeps
		// that string on the stack when it is 32 bytes or fewer and the
		// conversion does not escape (Go 1.27): a shorter text would hide the
		// allocation.
		b := []byte("already_known_and_longer_than_32_bytes")
		want := in.Intern(b)
		// AllocsPerRun counts Go allocations only, and bytes carved out of a
		// slab the arena already owns are not one: an Intern that copied the
		// text into the arena before looking it up, and threw the copy away on
		// a hit, would pass that count. So the arena is watched as well.
		usedBefore, slabsBefore := in.arena.used, in.arena.Slabs()
		allocs := testing.AllocsPerRun(100, func() {
			if in.Intern(b) != want {
				t.Errorf("Intern returned a different symbol for a known text")
			}
		})
		if allocs != 0 {
			t.Errorf("Intern of a known text allocated %v times per run, want 0", allocs)
		}
		if used, slabs := in.arena.used, in.arena.Slabs(); used != usedBefore || slabs != slabsBefore {
			t.Errorf("Intern of a known text used the arena: used %d -> %d bytes, slabs %d -> %d, want no change",
				usedBefore, used, slabsBefore, slabs)
		}
	})
}

// benchWords returns n distinct identifier-like texts as byte slices.
func benchWords(n int) [][]byte {
	words := make([][]byte, n)
	for i := range words {
		words[i] = []byte(fmt.Sprintf("identifier_%d", i))
	}
	return words
}

// BenchmarkIntern measures Intern on texts that are already known, which is
// what a lexer does most of the time. Run it with -benchmem.
func BenchmarkIntern(b *testing.B) {
	words := benchWords(1000)
	in := NewInterner()
	for _, w := range words {
		in.Intern(w)
	}
	i := 0
	for b.Loop() {
		in.Intern(words[i])
		i++
		if i == len(words) {
			i = 0
		}
	}
}

// BenchmarkUniqueMake is the same work with the standard library's unique
// package. unique.Make takes a string, but the compiler reports that
// string(w) does not escape (go test -c -gcflags=-m), so the conversion does
// not allocate: both benchmarks give 0 B/op and 0 allocs/op on this machine,
// and only the time per call differs.
func BenchmarkUniqueMake(b *testing.B) {
	words := benchWords(1000)
	for _, w := range words {
		unique.Make(string(w))
	}
	i := 0
	for b.Loop() {
		unique.Make(string(words[i]))
		i++
		if i == len(words) {
			i = 0
		}
	}
}
