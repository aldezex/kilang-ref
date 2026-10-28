package base

import "testing"

func TestEditDistance(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		// The four first rows are the classic ones. The rest are there because
		// a careless distance can pass those four and still be wrong: counting
		// the characters that differ in place, plus the difference in length,
		// also gives 3, 3, 0 and 2 for them.
		rows := []struct {
			name string
			a, b string
			want int
		}{
			{"kitten_sitting", "kitten", "sitting", 3},
			{"empty_to_abc", "", "abc", 3},
			{"equal", "igual", "igual", 0},
			{"swap_costs_two", "ab", "ba", 2},
			{"both_empty", "", "", 0},
			{"substitution_costs_one", "a", "b", 1},
			{"last_byte_differs", "abc", "abd", 1},
			{"first_byte_differs", "xbc", "abc", 1},
			{"insertion_in_the_middle", "abc", "abxc", 1},
			// Deleting the first byte shifts every other one: only an
			// alignment, not a comparison in place, finds the 1.
			{"shifted", "abc", "bc", 1},
			{"flaw_lawn", "flaw", "lawn", 2},
			{"sunday_saturday", "sunday", "saturday", 3},
			{"intention_execution", "intention", "execution", 5},
			// It counts bytes: é is the two bytes C3 A9, so turning it into
			// e is one substitution and one deletion.
			{"counts_bytes_not_runes", "é", "e", 2},
		}
		for _, row := range rows {
			// The distance is symmetric, and the shorter string can be either
			// of the two: a row sized after the wrong one runs off its end.
			if got := EditDistance(row.a, row.b); got != row.want {
				t.Errorf("%s: EditDistance(%q, %q) = %d, want %d", row.name, row.a, row.b, got, row.want)
			}
			if got := EditDistance(row.b, row.a); got != row.want {
				t.Errorf("%s: EditDistance(%q, %q) = %d, want %d", row.name, row.b, row.a, got, row.want)
			}
		}
	})
}
