package base

// EditDistance returns the Levenshtein distance between a and b: the fewest
// single-byte insertions, deletions and substitutions that turn a into b. It
// compares bytes, not characters, so a letter such as é (two bytes in UTF-8)
// counts as two. Each of the three edits costs 1, and swapping two neighbours
// is two substitutions, not one edit: EditDistance("ab", "ba") is 2.
//
// It is dynamic programming: the cell [i][j] of the table is the distance
// between the first i bytes of a and the first j bytes of b. Cell [i][j] needs
// only the cells above it, to its left and diagonally above-left, so two rows of
// the table are enough: the one being filled and the one before it.
func EditDistance(a, b string) int {
	// prev is row i-1 of the table and cur is row i. Row 0 is the cost of
	// building the first j bytes of b from nothing: j insertions.
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		// Column 0 is turning the first i bytes of a into nothing: i deletions.
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			substitution := 1
			if a[i-1] == b[j-1] {
				substitution = 0
			}
			cur[j] = min(
				prev[j]+1,              // delete a[i-1]
				cur[j-1]+1,             // insert b[j-1]
				prev[j-1]+substitution, // keep or replace the byte
			)
		}
		prev, cur = cur, prev
	}
	// The last row computed is now prev (the swap above), or row 0 when a is
	// empty.
	return prev[len(b)]
}
