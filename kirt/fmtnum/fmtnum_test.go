package fmtnum_test

import (
	"math"
	"math/rand/v2"
	"regexp"
	"strconv"
	"testing"

	"github.com/aldezex/kilang/kirt/fmtnum"
)

// bits is a float64 written as the bits that it is made of, so that a row of a
// table names a value that no decimal literal could (the largest subnormal) or
// that Go would fold at compile time (0.1 + 0.2 is 0.3 for the compiler, and not
// for the machine).
func bits(b uint64) float64 { return math.Float64frombits(b) }

// floatRow is one value and the text that Float must give for it. The texts are
// written by hand, from the rule of spec 5.10 and not from strconv, so a test
// does not grade the implementation with the function it is built on.
type floatRow struct {
	name string
	f    float64
	want string
}

// checkRows reports each row that Float gets wrong.
func checkRows(t *testing.T, rows []floatRow) {
	t.Helper()
	for _, row := range rows {
		if got := fmtnum.Float(row.f); got != row.want {
			t.Errorf("%s: Float(%v) = %q, want %q", row.name, row.f, got, row.want)
		}
	}
}

func TestFloat(t *testing.T) {
	t.Run("fixed_form_adds_dot_zero_only_to_integers", func(t *testing.T) {
		checkRows(t, []floatRow{
			{"one", 1, "1.0"},
			{"six", 6, "6.0"},
			{"a_tenth", 0.1, "0.1"},
			{"a_million", 1000000, "1000000.0"},
			{"a_thousandth", 0.001, "0.001"},
			{"negative", -2.5, "-2.5"},
			{"hundred", 100, "100.0"},
			{"digits_then_zeros", 120, "120.0"},
			{"point_inside_the_digits", 123.456, "123.456"},
			{"sum_that_rounds", bits(0x3fd3333333333334), "0.30000000000000004"},
			{"big_integer_value", 1e20, "100000000000000000000.0"},
			{"negative_integer_value", -7, "-7.0"},
			{"fraction_below_one_keeps_its_zero", 0.5, "0.5"},
		})
	})

	t.Run("exponential_form_has_no_plus_and_no_padding", func(t *testing.T) {
		checkRows(t, []floatRow{
			{"one_e21", 1e21, "1e21"},
			{"small_with_a_fraction", 1.5e-7, "1.5e-7"},
			{"negative_and_large", -2.5e300, "-2.5e300"},
			{"one_e_minus_7", 1e-7, "1e-7"},
			{"largest_finite", math.MaxFloat64, "1.7976931348623157e308"},
			{"smallest_subnormal", bits(1), "5e-324"},
			{"largest_subnormal", bits(0x000fffffffffffff), "2.225073858507201e-308"},
			{"smallest_normal", bits(0x0010000000000000), "2.2250738585072014e-308"},
			{"one_e100", 1e100, "1e100"},
		})
	})

	t.Run("thresholds_of_the_fixed_form", func(t *testing.T) {
		checkRows(t, []floatRow{
			// 1e-6 is the smallest value of the fixed form: the one below it, and
			// the one above it, are on each side of the rule.
			{"1e-6_is_fixed", 1e-6, "0.000001"},
			{"just_below_1e-6", math.Nextafter(1e-6, 0), "9.999999999999997e-7"},
			{"just_above_1e-6", math.Nextafter(1e-6, 1), "0.0000010000000000000002"},
			{"negative_1e-6", -1e-6, "-0.000001"},
			// 1e21 is the smallest value of the exponential form.
			{"just_below_1e21", math.Nextafter(1e21, 0), "999999999999999900000.0"},
			{"1e21_is_exponential", 1e21, "1e21"},
			{"just_above_1e21", math.Nextafter(1e21, math.Inf(1)), "1.0000000000000001e21"},
			{"negative_1e21", -1e21, "-1e21"},
		})
	})

	t.Run("special_values_have_explicit_names", func(t *testing.T) {
		checkRows(t, []floatRow{
			{"nan", math.NaN(), "NaN"},
			// The sign of a NaN is not part of the text.
			{"negative_nan", math.Copysign(math.NaN(), -1), "NaN"},
			{"nan_with_a_payload", bits(0x7ff8000000000001), "NaN"},
			{"positive_infinity", math.Inf(1), "inf"},
			{"negative_infinity", math.Inf(-1), "-inf"},
			{"zero", 0, "0.0"},
			{"negative_zero", math.Copysign(0, -1), "-0.0"},
		})
	})

	t.Run("many_values_round_trip_and_have_the_shape_of_their_form", func(t *testing.T) {
		// A form is told by its shape: the fixed one is digits, a point and
		// digits, and the exponential one a mantissa, an `e` and an exponent
		// with no `+` and no leading zero.
		fixed := regexp.MustCompile(`^-?[0-9]+\.[0-9]+$`)
		exponential := regexp.MustCompile(`^-?[0-9](\.[0-9]+)?e-?[1-9][0-9]*$`)
		rng := rand.New(rand.NewPCG(1, 2))
		for i := range 4000 {
			var f float64
			switch i % 2 {
			case 0:
				f = math.Float64frombits(rng.Uint64())
			default:
				// A value of a few digits at a power of ten, which is where the
				// two forms meet (a random bit pattern is almost never there).
				f, _ = strconv.ParseFloat(strconv.Itoa(rng.IntN(100000)+1)+"e"+strconv.Itoa(rng.IntN(60)-30), 64)
			}
			if math.IsNaN(f) || math.IsInf(f, 0) {
				continue
			}
			got := fmtnum.Float(f)
			back, err := strconv.ParseFloat(got, 64)
			if err != nil || math.Float64bits(back) != math.Float64bits(f) {
				t.Errorf("Float(%v) = %q, which reads back as %v (error %v)", f, got, back, err)
			}
			abs := math.Abs(f)
			if f == 0 || abs >= 1e-6 && abs < 1e21 {
				if !fixed.MatchString(got) {
					t.Errorf("Float(%v) = %q, want the fixed form", f, got)
				}
			} else if !exponential.MatchString(got) {
				t.Errorf("Float(%v) = %q, want the exponential form", f, got)
			}
		}
	})
}
