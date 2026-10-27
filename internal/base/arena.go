package base

// defaultSlabSize is the size of a slab when NewArena is given 0 or a negative
// size.
const defaultSlabSize = 64 << 10

// Arena hands out byte slices carved from big slabs. It uses no unsafe: it
// exists for locality (consecutive requests sit next to each other in memory)
// and to pay one Go allocation per slab instead of one per request.
//
// Nothing is ever freed one piece at a time. The Arena keeps every slab until
// the Arena itself is unreachable, and a slice keeps its whole slab alive: the
// garbage collector frees memory a slab at a time, never a request at a time.
// An Arena is not safe for concurrent use.
type Arena struct {
	slabs    [][]byte // every slab ever created, including the ones for large requests
	cur      []byte   // the slab small requests are served from; nil before the first one
	used     int      // bytes of cur already handed out
	slabSize int      // size of every slab created for small requests
}

// NewArena returns an Arena whose slabs hold slabSize bytes; 0 (or any
// negative number, which makes no sense as a size) means 64 KiB. It creates no
// slab until the first Alloc.
func NewArena(slabSize int) *Arena {
	if slabSize <= 0 {
		slabSize = defaultSlabSize
	}
	return &Arena{slabSize: slabSize}
}

// Alloc returns a slice with len n and cap n. The capacity is clipped to
// n so that an append by the caller reallocates instead of writing into the
// next request's bytes. Alloc(0) returns nil and creates no slab: an empty
// request needs no memory.
//
// A request larger than the slab size gets a slab of its own, which never
// becomes the current one: the current slab keeps serving small requests, and
// its free space is not thrown away. A small request that does not fit in what
// is left of the current slab starts a new slab, and the rest of the old one is
// left unused.
func (a *Arena) Alloc(n int) []byte {
	if n == 0 {
		return nil
	}
	if n > a.slabSize {
		big := make([]byte, n)
		a.slabs = append(a.slabs, big)
		return big[:n:n]
	}
	if a.cur == nil || len(a.cur)-a.used < n {
		a.cur = make([]byte, a.slabSize)
		a.used = 0
		a.slabs = append(a.slabs, a.cur)
	}
	b := a.cur[a.used : a.used+n : a.used+n]
	a.used += n
	return b
}

// Copy returns a copy of b stored in the arena. The result does not share
// memory with b. Copying an empty b returns nil and uses no arena memory.
func (a *Arena) Copy(b []byte) []byte {
	c := a.Alloc(len(b))
	copy(c, b)
	return c
}

// Slabs returns the number of slabs the arena has created, large ones included.
func (a *Arena) Slabs() int {
	return len(a.slabs)
}
