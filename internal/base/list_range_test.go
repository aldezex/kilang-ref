package base

import (
	"slices"
	"testing"
)

func TestListRange(t *testing.T) {
	// A pool the way a pass builds it: grown by append, so it has room to
	// spare after its last element (len 5, cap 8). Slice has to look at the
	// length and not at that room.
	pool := make([]string, 0, 8)
	pool = append(pool, "a", "b", "c", "d", "e")

	// Slice returns the elements that a range selects, and nothing else can
	// be reached through the result: its capacity is its length, so an append
	// by the caller reallocates and leaves the pool alone, the first element
	// of the next list and the room after the last one included.
	for _, row := range []struct {
		name string
		r    ListRange
		want []string
	}{
		{"first list", ListRange{First: 0, Count: 2}, []string{"a", "b"}},
		{"list in the middle", ListRange{First: 2, Count: 1}, []string{"c"}},
		{"last list", ListRange{First: 3, Count: 2}, []string{"d", "e"}},
		{"whole pool", ListRange{First: 0, Count: 5}, []string{"a", "b", "c", "d", "e"}},
		{"empty list at the start", ListRange{First: 0, Count: 0}, nil},
		{"empty list at the end", ListRange{First: 5, Count: 0}, nil},
	} {
		whole := slices.Clone(pool[:cap(pool)])
		got := Slice(pool, row.r)
		if !slices.Equal(got, row.want) {
			t.Errorf("%s: Slice(pool, %+v) = %q, want %q", row.name, row.r, got, row.want)
		}
		if cap(got) != len(got) {
			t.Errorf("%s: cap(Slice(pool, %+v)) = %d, want %d, its length", row.name, row.r, cap(got), len(got))
		}
		_ = append(got, "X")
		if after := pool[:cap(pool)]; !slices.Equal(after, whole) {
			t.Errorf("%s: an append to Slice(pool, %+v) changed the pool to %q, want %q", row.name, row.r, after, whole)
		}
	}

	// A range that reaches past the last element is a bug of the caller, and
	// panics with the runtime's own error, even when the room after the last
	// element would hold it: it must not read what the pool does not have yet.
	for _, row := range []struct {
		name string
		r    ListRange
	}{
		{"one past the end", ListRange{First: 0, Count: 6}},
		{"starts at the end and takes one", ListRange{First: 5, Count: 1}},
		{"starts past the end", ListRange{First: 6, Count: 0}},
	} {
		mustPanic(t, row.name, "runtime error: slice bounds out of range [::6] with capacity 5", func() { Slice(pool, row.r) })
	}

	// AppendRange appends the items and returns the range that they now
	// occupy: it starts where the pool ended, and it is as long as the items.
	// Each list goes after the one before, an empty one included.
	var appended, all []string
	for _, row := range []struct {
		name  string
		items []string
		want  ListRange
	}{
		{"first list into an empty pool", []string{"a", "b"}, ListRange{First: 0, Count: 2}},
		{"empty list", nil, ListRange{First: 2, Count: 0}},
		{"list after an empty one", []string{"c"}, ListRange{First: 2, Count: 1}},
		{"longer list", []string{"d", "e", "f"}, ListRange{First: 3, Count: 3}},
	} {
		got := AppendRange(&appended, row.items)
		all = append(all, row.items...)
		if got != row.want {
			t.Errorf("%s: AppendRange returned %+v, want %+v", row.name, got, row.want)
			continue
		}
		if !slices.Equal(appended, all) {
			t.Errorf("%s: the pool is %q after AppendRange, want %q", row.name, appended, all)
			continue
		}
		// The range selects the very list that was appended.
		if list := Slice(appended, got); !slices.Equal(list, row.items) {
			t.Errorf("%s: Slice(pool, %+v) = %q, want %q", row.name, got, list, row.items)
		}
	}
}
