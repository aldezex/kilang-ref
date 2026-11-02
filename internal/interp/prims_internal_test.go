package interp

import (
	"math"
	"testing"

	"github.com/aldezex/kilang/internal/core"
)

// The names of the extremes keep the rows short. They are the int64 limits.
const (
	maxI = math.MaxInt64
	minI = math.MinInt64
)

// arithRow is one row of a table over the checked operations of two operands.
// overflow says that the exact result does not fit in an int64; the value of
// such a row means nothing, so a row that overflows leaves want at 0 and the
// test does not look at it.
type arithRow struct {
	name     string
	a, b     int64
	want     int64
	overflow bool
}

// checkArith runs every row through op and reports the rows it gets wrong.
func checkArith(t *testing.T, opName string, op func(a, b int64) (int64, bool), rows []arithRow) {
	t.Helper()
	for _, row := range rows {
		got, overflow := op(row.a, row.b)
		if overflow != row.overflow {
			t.Errorf("%s: %s(%d, %d) overflow = %v, want %v", row.name, opName, row.a, row.b, overflow, row.overflow)
			continue
		}
		if !overflow && got != row.want {
			t.Errorf("%s: %s(%d, %d) = %d, want %d", row.name, opName, row.a, row.b, got, row.want)
		}
	}
}

func TestPrims(t *testing.T) {
	t.Run("add_boundaries", func(t *testing.T) {
		checkArith(t, "addInt", addInt, []arithRow{
			{"max_plus_1", maxI, 1, 0, true},
			{"1_plus_max", 1, maxI, 0, true},
			{"min_plus_minus_1", minI, -1, 0, true},
			{"minus_1_plus_min", -1, minI, 0, true},
			{"max_plus_max", maxI, maxI, 0, true},
			{"min_plus_min", minI, minI, 0, true},
			{"max_plus_0", maxI, 0, maxI, false},
			{"min_plus_0", minI, 0, minI, false},
			{"0_plus_min", 0, minI, minI, false},
			{"min_plus_1_plus_minus_1", minI + 1, -1, minI, false},
			{"max_plus_minus_1", maxI, -1, maxI - 1, false},
			// The two extremes cancel out: the signs differ, so nothing overflows.
			{"min_plus_max", minI, maxI, -1, false},
			{"max_plus_min", maxI, minI, -1, false},
			// The sign of the result differs from the sign of an operand, and
			// that alone is not an overflow.
			{"minus_1_plus_1", -1, 1, 0, false},
			{"1_plus_minus_3", 1, -3, -2, false},
			{"0_plus_0", 0, 0, 0, false},
		})
	})

	t.Run("sub_boundaries", func(t *testing.T) {
		checkArith(t, "subInt", subInt, []arithRow{
			{"min_minus_1", minI, 1, 0, true},
			{"0_minus_min", 0, minI, 0, true},
			{"max_minus_minus_1", maxI, -1, 0, true},
			{"max_minus_min", maxI, minI, 0, true},
			{"min_minus_max", minI, maxI, 0, true},
			{"min_minus_0", minI, 0, minI, false},
			{"max_minus_0", maxI, 0, maxI, false},
			// -1 - MaxInt64 is exactly MinInt64: the lowest value that fits.
			{"minus_1_minus_max", -1, maxI, minI, false},
			{"0_minus_max", 0, maxI, minI + 1, false},
			{"min_minus_minus_1", minI, -1, minI + 1, false},
			{"minus_1_minus_min", -1, minI, maxI, false},
			{"min_minus_min", minI, minI, 0, false},
			{"max_minus_max", maxI, maxI, 0, false},
			// The operands are not interchangeable.
			{"3_minus_5", 3, 5, -2, false},
			{"5_minus_3", 5, 3, 2, false},
		})
	})

	t.Run("mul_boundaries", func(t *testing.T) {
		checkArith(t, "mulInt", mulInt, []arithRow{
			// 3037000499 is the largest n with n*n <= MaxInt64.
			{"3037000499_squared", 3037000499, 3037000499, 9223372030926249001, false},
			{"3037000500_squared", 3037000500, 3037000500, 0, true},
			{"minus_3037000499_by_3037000499", -3037000499, 3037000499, -9223372030926249001, false},
			{"minus_3037000500_by_3037000500", -3037000500, 3037000500, 0, true},
			// MinInt64 / -1 does not fail in Go, it wraps, so the division
			// check cannot see the first of these two products.
			{"min_times_minus_1", minI, -1, 0, true},
			{"minus_1_times_min", -1, minI, 0, true},
			{"min_times_1", minI, 1, minI, false},
			{"1_times_min", 1, minI, minI, false},
			{"0_times_min", 0, minI, 0, false},
			{"min_times_0", minI, 0, 0, false},
			{"0_times_0", 0, 0, 0, false},
			// -2 * 2^62 is exactly MinInt64, and 2 * 2^62 is one past MaxInt64.
			{"minus_2_times_2_to_62", -2, 1 << 62, minI, false},
			{"2_to_62_times_minus_2", 1 << 62, -2, minI, false},
			{"2_times_2_to_62", 2, 1 << 62, 0, true},
			{"max_times_minus_1", maxI, -1, minI + 1, false},
			{"minus_1_times_max", -1, maxI, minI + 1, false},
			{"max_times_2", maxI, 2, 0, true},
			{"min_times_2", minI, 2, 0, true},
			{"min_times_min", minI, minI, 0, true},
			{"max_times_max", maxI, maxI, 0, true},
			{"7_times_minus_6", 7, -6, -42, false},
		})
	})

	t.Run("neg_boundary", func(t *testing.T) {
		rows := []struct {
			a, want  int64
			overflow bool
		}{
			{minI, 0, true},
			{maxI, minI + 1, false},
			{minI + 1, maxI, false},
			{0, 0, false},
			{5, -5, false},
			{-5, 5, false},
		}
		for _, row := range rows {
			got, overflow := negInt(row.a)
			if overflow != row.overflow {
				t.Errorf("negInt(%d) overflow = %v, want %v", row.a, overflow, row.overflow)
			} else if !overflow && got != row.want {
				t.Errorf("negInt(%d) = %d, want %d", row.a, got, row.want)
			}
		}
	})

	t.Run("div_rem", func(t *testing.T) {
		// Each row gives a/b and a%b. When the error is not divOK, the value
		// means nothing and is not compared.
		rows := []struct {
			name string
			a, b int64
			div  int64
			divE divErr
			rem  int64
			remE divErr
		}{
			{"minus_7_by_2", -7, 2, -3, divOK, -1, divOK},
			{"7_by_2", 7, 2, 3, divOK, 1, divOK},
			{"7_by_minus_2", 7, -2, -3, divOK, 1, divOK},
			{"minus_7_by_minus_2", -7, -2, 3, divOK, -1, divOK},
			{"minus_7_by_3", -7, 3, -2, divOK, -1, divOK},
			{"7_by_minus_3", 7, -3, -2, divOK, 1, divOK},
			{"0_by_minus_1", 0, -1, 0, divOK, 0, divOK},
			{"min_by_1", minI, 1, minI, divOK, 0, divOK},
			{"min_by_2", minI, 2, -(1 << 62), divOK, 0, divOK},
			{"min_by_min", minI, minI, 1, divOK, 0, divOK},
			{"max_by_minus_1", maxI, -1, minI + 1, divOK, 0, divOK},
			{"min_plus_1_by_minus_1", minI + 1, -1, maxI, divOK, 0, divOK},
			// MinInt64 / -1 is 2^63, which does not fit; MinInt64 % -1 is 0.
			{"min_by_minus_1", minI, -1, 0, divOverflow, 0, divOK},
			{"1_by_0", 1, 0, 0, divByZero, 0, divByZero},
			{"0_by_0", 0, 0, 0, divByZero, 0, divByZero},
			// A zero divisor is divByZero whatever the dividend, even an extreme one.
			{"min_by_0", minI, 0, 0, divByZero, 0, divByZero},
			{"max_by_0", maxI, 0, 0, divByZero, 0, divByZero},
		}
		for _, row := range rows {
			gotDiv, gotDivE := divInt(row.a, row.b)
			if gotDivE != row.divE {
				t.Errorf("%s: divInt(%d, %d) error = %d, want %d", row.name, row.a, row.b, gotDivE, row.divE)
			} else if gotDivE == divOK && gotDiv != row.div {
				t.Errorf("%s: divInt(%d, %d) = %d, want %d", row.name, row.a, row.b, gotDiv, row.div)
			}
			gotRem, gotRemE := remInt(row.a, row.b)
			if gotRemE != row.remE {
				t.Errorf("%s: remInt(%d, %d) error = %d, want %d", row.name, row.a, row.b, gotRemE, row.remE)
			} else if gotRemE == divOK && gotRem != row.rem {
				t.Errorf("%s: remInt(%d, %d) = %d, want %d", row.name, row.a, row.b, gotRem, row.rem)
			}
		}
	})

	t.Run("unknown_cases_are_unreachable", func(t *testing.T) {
		// Prim and divErr are enumerations, and the switch over each ends in
		// panic("unreachable"). These are the values that no switch has a case
		// for: the first one past the last, and the largest the type holds.
		rows := []struct {
			name string
			work func()
		}{
			{"primitive_after_the_last", func() { applyPrim(core.PrimNotBool+1, BoolV{V: true}, BoolV{V: true}) }},
			{"largest_primitive", func() { applyPrim(core.Prim(math.MaxUint8), UnitV{}, UnitV{}) }},
			{"division_error_after_the_last", func() { divResult(0, divOverflow+1) }},
		}
		for _, row := range rows {
			var got any
			func() {
				defer func() { got = recover() }()
				row.work()
			}()
			if got != "unreachable" {
				t.Errorf("%s: panicked with %#v, want the string \"unreachable\"", row.name, got)
			}
		}
	})
}
