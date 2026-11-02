package base

import "iter"

// IdVec is a slice that only its own id type can index, like rustc's
// IndexVec. The compiler will keep its tables (expressions, types,
// definitions…) in IdVecs, as SourceManager already does with its files, so
// passing a TypeID where an ExprID is expected is a compile error instead of a
// silent bug. The zero value is an empty IdVec, ready to use.
//
// I is a defined type whose underlying type is uint32 (FileID, ExprID…). The
// ~uint32 constraint lets IdVec convert between I and an index, but two
// different id types still cannot be mixed: Go's type rules reject that.
//
// An IdVec never checks that it holds fewer than 2^32 elements: the compiler
// stays far below that, and Push would hand out a wrapped id past it.
type IdVec[I ~uint32, T any] struct {
	items []T
}

// Push appends x and returns the id of the new element, which is the id that
// Next returned just before.
func (v *IdVec[I, T]) Push(x T) I {
	id := I(len(v.items))
	v.items = append(v.items, x)
	return id
}

// Get returns a copy of the element with the given id. It panics, with the
// runtime's index-out-of-range error, if no such element exists.
func (v *IdVec[I, T]) Get(i I) T {
	return v.items[i]
}

// Ptr returns a pointer to the element with the given id, so the caller can
// change it in place. The pointer is valid only until the next Push, because
// Push may move the elements to a bigger array and the pointer would keep
// pointing at the old one. Ptr panics like Get for an id that does not exist.
func (v *IdVec[I, T]) Ptr(i I) *T {
	return &v.items[i]
}

// Len returns the number of elements.
func (v *IdVec[I, T]) Len() int {
	return len(v.items)
}

// Next returns the id that the next call to Push will return.
func (v *IdVec[I, T]) Next() I {
	return I(len(v.items))
}

// All returns an iterator over the ids and elements in id order, for use with
// range: for id, x := range v.All(). The elements are copies, as in Get.
func (v *IdVec[I, T]) All() iter.Seq2[I, T] {
	return func(yield func(I, T) bool) {
		for i, x := range v.items {
			if !yield(I(i), x) {
				return
			}
		}
	}
}

// ListRange is a slice of a shared pool: the elements pool[First : First+Count].
// The syntax tree and Core store the children of a node as a ListRange into one
// pool per node kind, instead of one small slice per node, so that nodes hold
// no pointers and a whole tree is a few big arrays. Slice reads a range and
// AppendRange makes one.
type ListRange struct {
	First, Count uint32
}

// Slice returns the elements of pool that r selects. The result has its
// capacity clipped to its length, so an append by the caller reallocates
// instead of overwriting the first element of the next list in the pool.
// It panics, with the runtime's slice bounds error, if r reaches past the end
// of the pool.
func Slice[T any](pool []T, r ListRange) []T {
	// A slice expression checks its upper bound against the capacity, not the
	// length, and a pool that append has grown has spare room after its last
	// element. Clipping the pool first makes the check use the length.
	pool = pool[:len(pool):len(pool)]
	end := r.First + r.Count
	return pool[r.First:end:end]
}

// AppendRange appends items to *pool and returns the range they now occupy.
// A pass collects the elements of a list in a slice of its own and appends them
// in one go: a list that contains other lists (the arguments of a call are
// expressions, which have arguments…) would otherwise end up with its elements
// interleaved with theirs, and a ListRange has to be contiguous.
func AppendRange[T any](pool *[]T, items []T) ListRange {
	first := uint32(len(*pool))
	*pool = append(*pool, items...)
	return ListRange{First: first, Count: uint32(len(items))}
}
