package testsupport

import (
	"slices"

	"github.com/aldezex/kilang/internal/diag"
)

// ApplyFixits returns src with the fix-its of d applied, each at its offsets in
// the original text. They are applied from the last to the first, so that no
// edit moves another. It is for the tests that check that a fix-it does what
// its help says.
func ApplyFixits(src string, d diag.Diagnostic) string {
	fixits := slices.Clone(d.Fixits)
	slices.SortStableFunc(fixits, func(a, b diag.FixIt) int { return int(b.Span.Lo) - int(a.Span.Lo) })
	for _, f := range fixits {
		src = src[:f.Span.Lo] + f.Replacement + src[f.Span.Hi:]
	}
	return src
}
