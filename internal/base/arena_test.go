package base

import "testing"

func TestArena(t *testing.T) {
	t.Run("alloc_len_equals_cap", func(t *testing.T) {
		a := NewArena(0)
		b := a.Alloc(5)
		if len(b) != 5 || cap(b) != 5 {
			t.Errorf("Alloc(5): len = %d, cap = %d, want 5 and 5", len(b), cap(b))
		}
		// A second request shares the slab; its capacity must not reach
		// into whatever comes after it, or append would overwrite a neighbour.
		c := a.Alloc(3)
		if len(c) != 3 || cap(c) != 3 {
			t.Errorf("Alloc(3): len = %d, cap = %d, want 3 and 3", len(c), cap(c))
		}
		// An empty request needs no memory, so it must not open a slab
		// (a fresh arena would otherwise pay 64 KiB for it). The docs of Alloc
		// and Copy promise nil, so the test checks for nil: an empty slice that
		// is not nil also has len 0 and cap 0.
		fresh := NewArena(0)
		if z := fresh.Alloc(0); z != nil {
			t.Errorf("Alloc(0) = %#v, want nil", z)
		}
		if z := fresh.Copy(nil); z != nil {
			t.Errorf("Copy(nil) = %#v, want nil", z)
		}
		if got := fresh.Slabs(); got != 0 {
			t.Errorf("Slabs after Alloc(0) and Copy(nil) on a new arena = %d, want 0", got)
		}
	})

	t.Run("many_small_share_slabs", func(t *testing.T) {
		a := NewArena(0)
		for i := 0; i < 1000; i++ {
			a.Alloc(16)
		}
		if got := a.Slabs(); got != 1 {
			t.Errorf("Slabs after 1000 x 16 bytes = %d, want 1", got)
		}
		// Sharing a slab is only correct if the requests do not overlap, and
		// the slab count cannot see that: a cursor that forgot everything
		// before the last request (used = n instead of used += n) would
		// still report one slab. Fill three consecutive requests with 1, 2
		// and 3 and check that each kept its value.
		first, second, third := a.Alloc(4), a.Alloc(4), a.Alloc(4)
		requests := [][]byte{first, second, third}
		for i, r := range requests {
			for j := range r {
				r[j] = byte(i + 1)
			}
		}
		overlap := false
		for i, r := range requests {
			for _, b := range r {
				if b != byte(i+1) {
					overlap = true
				}
			}
		}
		if overlap {
			t.Errorf("three consecutive requests overlap: %v %v %v, want [1 1 1 1] [2 2 2 2] [3 3 3 3]", first, second, third)
		}
	})

	t.Run("small_that_does_not_fit_opens_new_slab", func(t *testing.T) {
		a := NewArena(32)
		first := a.Alloc(20)
		for i := range first {
			first[i] = 0x11
		}
		second := a.Alloc(20)
		for i := range second {
			second[i] = 0x22
		}
		if got := a.Slabs(); got != 2 {
			t.Fatalf("Slabs after 20 + 20 bytes in 32-byte slabs = %d, want 2", got)
		}
		for i, b := range first {
			if b != 0x11 {
				t.Fatalf("first[%d] = %#x, want 0x11: the second request overwrote it", i, b)
			}
		}
		// The new slab starts at 0, so 12 bytes are left in it and a request
		// of exactly 12 fits.
		a.Alloc(12)
		if got := a.Slabs(); got != 2 {
			t.Errorf("Slabs after an exact fit of the 12 bytes left = %d, want 2", got)
		}
		// A request of exactly the slab size is small: it opens a slab that
		// becomes the current one and it is filled by it. A mutant that
		// treats it as large leaves the old slab current, and the one-byte
		// request below fits there.
		b := NewArena(32)
		b.Alloc(16)
		b.Alloc(32)
		if got := b.Slabs(); got != 2 {
			t.Fatalf("Slabs after 16 + 32 bytes in 32-byte slabs = %d, want 2", got)
		}
		b.Alloc(1)
		if got := b.Slabs(); got != 3 {
			t.Errorf("Slabs after a request of exactly the slab size and then 1 byte = %d, want 3: the full-size request must become the current slab", got)
		}
		// One byte more than the slab size is large and gets a slab of its own.
		c := NewArena(32)
		if got := c.Alloc(33); len(got) != 33 || cap(got) != 33 || c.Slabs() != 1 {
			t.Errorf("Alloc(33) in 32-byte slabs: len = %d, cap = %d, Slabs = %d, want 33, 33 and 1", len(got), cap(got), c.Slabs())
		}
	})

	t.Run("large_gets_own_slab", func(t *testing.T) {
		a := NewArena(0)
		if got := a.Slabs(); got != 0 {
			t.Errorf("Slabs before the first Alloc = %d, want 0", got)
		}
		first := a.Alloc(16)
		for i := range first {
			first[i] = 0x11
		}
		big := a.Alloc(100 << 10)
		if len(big) != 100<<10 {
			t.Fatalf("len(big) = %d, want %d", len(big), 100<<10)
		}
		for i := range big {
			big[i] = 0xAB
		}
		if got := a.Slabs(); got != 2 {
			t.Errorf("Slabs after the large request = %d, want 2", got)
		}
		small := a.Alloc(16)
		if got := a.Slabs(); got != 2 {
			t.Errorf("Slabs after a small request following the large one = %d, want 2: the large slab must not become the current one", got)
		}
		// Even with the slab count right, a small request carved from the
		// start of the large slab would alias its first bytes.
		for i := range small {
			small[i] = 0xCD
		}
		for i, b := range big {
			if b != 0xAB {
				t.Fatalf("big[%d] = %#x after writing the small request, want 0xAB: the small request shares memory with the large one", i, b)
			}
		}
		// The large request must also leave the cursor of the current slab
		// alone. If it reset the cursor, the small request would be carved on
		// top of the first one, and writing it would overwrite the 0x11 bytes.
		for i, b := range first {
			if b != 0x11 {
				t.Fatalf("first[%d] = %#x after the large request and a small one, want 0x11: the large request must leave the cursor of the current slab alone", i, b)
			}
		}
	})

	t.Run("copy_is_independent", func(t *testing.T) {
		a := NewArena(0)
		b := []byte("abc")
		c := a.Copy(b)
		b[0] = 'X'
		if string(c) != "abc" {
			t.Errorf("Copy shares memory with its input: got %q, want \"abc\"", c)
		}
		// The copy must come out of the arena, not from a make of its own: the
		// arena has handed out exactly the 3 bytes, in the one slab it opened.
		if used, slabs := a.used, a.Slabs(); used != 3 || slabs != 1 {
			t.Errorf("after Copy of 3 bytes: arena used = %d bytes in %d slabs, want 3 bytes in 1", used, slabs)
		}
	})

	t.Run("zero_slab_size_means_64k", func(t *testing.T) {
		for _, size := range []int{0, -1} {
			if got := NewArena(size).slabSize; got != 64<<10 {
				t.Errorf("NewArena(%d).slabSize = %d, want %d", size, got, 64<<10)
			}
		}
	})
}
