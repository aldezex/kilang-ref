// Package fmtnum is the one place that writes a Float as text (spec 5.10). The
// interpreter, the VM and the native code all call it, so the same number prints
// the same way in every backend. It is a leaf of kirt: it imports the standard
// library only, so the oracle may import it too (kirt/imports_test.go).
package fmtnum

import (
	"math"
	"strconv"
	"strings"
)

// Float returns the text of f, by the rule of Ki:
//
//   - The digits are the shortest ones that read back as f. They come from
//     strconv.FormatFloat(f, 'e', -1, 64), the only call that decides digits
//     (strconv.Itoa below only writes the exponent), and the rule is written
//     on top of them because 'g' would choose another form (`1e+06`) and write
//     `+Inf`.
//   - The fixed form, `123.456` or `0.000001`, is used when 1e-6 <= |f| < 1e21,
//     and the exponential one, `1e21` or `1.5e-7`, outside that range: the
//     thresholds and the shape of JavaScript's Number.prototype.toString, but
//     with no `+` and no padding zeros in the exponent.
//   - `.0` goes after a fixed form that has no point, so a Float never reads as
//     an Int: `1.0`, `6.0`, `1000000.0`.
//   - NaN, infinities and zero are written as `NaN`, `inf`, `-inf`, `0.0` and
//     `-0.0`. The sign of a NaN is not written.
func Float(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case f == 0 && math.Signbit(f):
		return "-0.0"
	case f == 0:
		return "0.0"
	}
	digits, exp, negative := shortest(f)

	var b strings.Builder
	if negative {
		b.WriteByte('-')
	}
	if exp < -6 || exp > 20 {
		// d[.ddd]e[-]x
		b.WriteByte(digits[0])
		if len(digits) > 1 {
			b.WriteByte('.')
			b.WriteString(digits[1:])
		}
		b.WriteByte('e')
		b.WriteString(strconv.Itoa(exp))
		return b.String()
	}
	if exp < 0 {
		// 0.000ddd: -exp-1 zeros between the point and the digits.
		b.WriteString("0.")
		b.WriteString(strings.Repeat("0", -exp-1))
		b.WriteString(digits)
		return b.String()
	}
	// The integer part has exp+1 digits, padded with zeros if the shortest
	// digits are fewer; what is left of the digits is the fraction.
	whole := exp + 1
	if len(digits) <= whole {
		b.WriteString(digits)
		b.WriteString(strings.Repeat("0", whole-len(digits)))
		b.WriteString(".0")
		return b.String()
	}
	b.WriteString(digits[:whole])
	b.WriteByte('.')
	b.WriteString(digits[whole:])
	return b.String()
}

// shortest returns the shortest decimal digits of the finite, non-zero f, with
// no point, the power of ten of the first digit (f is 0.ddd... times 10^(exp+1),
// that is d.dd... times 10^exp), and whether f is negative.
func shortest(f float64) (digits string, exp int, negative bool) {
	// The text is `[-]d[.ddd]e[+-]xx`, with at least two digits of exponent.
	text := strconv.FormatFloat(f, 'e', -1, 64)
	if text[0] == '-' {
		negative, text = true, text[1:]
	}
	mantissa, exponent, _ := strings.Cut(text, "e")
	digits = strings.Replace(mantissa, ".", "", 1)
	// The exponent is read by hand: a second call into strconv would make two
	// the places where the format depends on it.
	sign := 1
	if exponent[0] == '-' {
		sign = -1
	}
	for _, c := range exponent[1:] {
		exp = exp*10 + int(c-'0')
	}
	return digits, sign * exp, negative
}
