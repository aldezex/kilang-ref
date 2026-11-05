package interp_test

import (
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/interp"
	"github.com/aldezex/kilang/internal/testsupport"
)

// printed compiles and runs a program that is only the lines in body, each a
// statement of `main`, and returns what it wrote to stdout. It fails the test
// unless the program ends normally with nothing on stderr.
func printed(t *testing.T, body string) []string {
	t.Helper()
	h, stdout, stderr := runMain(t, "fn main() {\n"+body+"}\n")
	if h != (interp.Halt{Code: 0}) || stderr != "" {
		t.Fatalf("RunMain() = %+v, stderr %q, want Halt{0} and nothing on stderr", h, stderr)
	}
	return strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
}

// wantLines fails the test unless got is exactly want, line by line.
func wantLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %d lines %q, want %d: %q", what, len(got), got, len(want), want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: line %d is %q, want %q", what, i+1, got[i], want[i])
		}
	}
}

func TestFloatOps(t *testing.T) {
	t.Run("arithmetic_follows_ieee_and_never_panics", func(t *testing.T) {
		rows := []struct{ expr, want string }{
			{"0.1 + 0.2", "0.30000000000000004"},
			{"0.3 - 0.1", "0.19999999999999998"},
			{"0.1 * 3.0", "0.30000000000000004"},
			{"7.0 / 2.0", "3.5"},
			{"2.0 * 3.5", "7.0"},
			{"1.0 - 1.0", "0.0"},
			{"-(1.5)", "-1.5"},
			{"-0.0", "-0.0"},
			{"1.0 / 0.0", "inf"},
			{"-1.0 / 0.0", "-inf"},
			{"0.0 / 0.0", "NaN"},
			{"1e308 * 10.0", "inf"},
			{"1e21 + 1.0", "1e21"},
			{"1e-7 / 10.0", "1e-8"},
		}
		var body strings.Builder
		var want []string
		for _, row := range rows {
			body.WriteString("  printFloat(" + row.expr + ")\n")
			want = append(want, row.want)
		}
		wantLines(t, "printFloat of each expression", printed(t, body.String()), want)
	})

	t.Run("comparisons_follow_ieee", func(t *testing.T) {
		// Each row is a condition and whether it holds. The comparison of a NaN is
		// false for everything but `!=`, and the two zeros are equal.
		rows := []struct {
			cond string
			want bool
		}{
			{"1.0 < 2.0", true}, {"2.0 < 2.0", false}, {"2.0 < 1.0", false},
			{"1.0 <= 2.0", true}, {"2.0 <= 2.0", true}, {"2.0 <= 1.0", false},
			{"2.0 > 1.0", true}, {"2.0 > 2.0", false}, {"1.0 > 2.0", false},
			{"2.0 >= 1.0", true}, {"2.0 >= 2.0", true}, {"1.0 >= 2.0", false},
			{"1.5 == 1.5", true}, {"1.5 == 2.5", false}, {"1.5 != 2.5", true}, {"1.5 != 1.5", false},
			{"0.0 == -0.0", true}, {"0.0 < -0.0", false},
			{"1e308 * 10.0 > 1e308", true}, {"-1e308 * 10.0 < -1e308", true},
			{"nan == nan", false}, {"nan != nan", true}, {"nan < 1.0", false}, {"nan <= nan", false},
			{"1.0 > nan", false}, {"1.0 >= nan", false}, {"nan != 1.0", true},
		}
		var body strings.Builder
		body.WriteString("  let zero = 0.0\n  let nan = zero / zero\n")
		var want []string
		for _, row := range rows {
			body.WriteString("  print(if " + row.cond + " { 1 } else { 0 })\n")
			if row.want {
				want = append(want, "1")
			} else {
				want = append(want, "0")
			}
		}
		wantLines(t, "each condition as 1 or 0", printed(t, body.String()), want)
	})

	t.Run("truncate_drops_the_fraction_toward_zero", func(t *testing.T) {
		rows := []struct{ arg, want string }{
			{"2.9", "2"}, {"-2.9", "-2"}, {"0.9", "0"}, {"-0.9", "0"}, {"0.0", "0"}, {"-0.0", "0"},
			{"1e18", "1000000000000000000"},
			// The largest Float below 2^63, and the smallest Int, which is exactly -2^63.
			{"9223372036854774784.0", "9223372036854774784"},
			{"-9223372036854775808.0", "-9223372036854775808"},
		}
		var body strings.Builder
		var want []string
		for _, row := range rows {
			body.WriteString("  print(truncate(" + row.arg + "))\n")
			want = append(want, row.want)
		}
		wantLines(t, "print of each truncate", printed(t, body.String()), want)
	})

	t.Run("truncate_panics_out_of_range", func(t *testing.T) {
		// Floats that are no Int. 9223372036854775807.0 is 2^63 once it is a
		// Float, one past the largest Int: a test against float64(MaxInt64) would
		// let it through.
		rows := []struct{ name, arg string }{
			{"nan", "0.0 / 0.0"},
			{"infinity", "1.0 / 0.0"},
			{"negative_infinity", "-1.0 / 0.0"},
			{"two_to_the_63", "9223372036854775808.0"},
			{"max_int_as_float", "9223372036854775807.0"},
			{"below_min_int", "-9223372036854777856.0"},
			{"large", "1e300"},
		}
		for _, row := range rows {
			// The call starts at column 9 of the second line: `truncate` after `  print(`.
			src := "fn main() {\n  print(truncate(" + row.arg + "))\n  print(2)\n}\n"
			wantHalt(t, src, interp.Halt{Code: 101}, "panic: Float out of range at test.ki:2:9\n")
		}
	})

	t.Run("truncate_panic_is_at_the_call_and_flushes_stdout_first", func(t *testing.T) {
		// The call is in a function of the program, on the second line, and what
		// main printed before comes out before the message.
		const src = "fn cut(x: Float) -> Int {\n  1 + truncate(x)\n}\nfn main() {\n  print(1)\n  print(cut(1.0 / 0.0))\n}\n"
		var log strings.Builder
		c := testsupport.Compile(t, src)
		h := interp.New(c.Core, &c.Session.Sources, &heldOut{log: &log}, &log).RunMain()
		const want = "1\npanic: Float out of range at test.ki:2:7\n"
		if h != (interp.Halt{Code: 101}) || log.String() != want {
			t.Errorf("RunMain() = %+v, the log holds %q, want Halt{101} and %q", h, log.String(), want)
		}
	})

	t.Run("to_float_and_sqrt", func(t *testing.T) {
		rows := []struct{ expr, want string }{
			{"toFloat(0)", "0.0"},
			{"toFloat(-7)", "-7.0"},
			// 2^53 + 1 is not a Float: it rounds to the even neighbour.
			{"toFloat(9007199254740993)", "9007199254740992.0"},
			// 2^53 + 3 is half way between two Floats, and the even one is above.
			{"toFloat(9007199254740995)", "9007199254740996.0"},
			{"toFloat(9223372036854775807)", "9223372036854776000.0"},
			{"toFloat(-9223372036854775807 - 1)", "-9223372036854776000.0"},
			{"sqrt(4.0)", "2.0"},
			{"sqrt(2.0)", "1.4142135623730951"},
			{"sqrt(0.0)", "0.0"},
			{"sqrt(-0.0)", "-0.0"},
			{"sqrt(-1.0)", "NaN"},
			{"sqrt(1.0 / 0.0)", "inf"},
		}
		var body strings.Builder
		var want []string
		for _, row := range rows {
			body.WriteString("  printFloat(" + row.expr + ")\n")
			want = append(want, row.want)
		}
		wantLines(t, "printFloat of each call", printed(t, body.String()), want)
	})

	t.Run("float_functions_are_values", func(t *testing.T) {
		// toFloat, truncate and sqrt are externs and not operators, so each can be
		// bound and passed like `print` (see TestExterns).
		const body = "  let f = toFloat\n  printFloat(f(3))\n  let g = truncate\n  print(g(2.5))\n  printFloat(apply(sqrt, 16.0))\n"
		const src = "fn apply(h, x: Float) -> Float { h(x) }\nfn main() {\n" + body + "}\n"
		h, stdout, stderr := runMain(t, src)
		if h != (interp.Halt{Code: 0}) || stdout != "3.0\n2\n4.0\n" || stderr != "" {
			t.Errorf("RunMain() = %+v, stdout %q, stderr %q, want Halt{0}, \"3.0\\n2\\n4.0\\n\" and nothing on stderr", h, stdout, stderr)
		}
	})

	t.Run("floats_flow_through_calls_and_tail_calls", func(t *testing.T) {
		// Ten additions of 0.1, one after the other and in a tail call, are not
		// 1.0: each sum rounds before the next is added.
		const src = "fn sum(n: Int, acc: Float) -> Float {\n  if n == 0 { acc } else { sum(n - 1, acc + 0.1) }\n}\nfn main() { printFloat(sum(10, 0.0)) }\n"
		m, h, stdout, stderr := runMainOn(t, src)
		if h != (interp.Halt{Code: 0}) || stdout != "0.9999999999999999\n" || stderr != "" {
			t.Errorf("RunMain() = %+v, stdout %q, stderr %q, want Halt{0}, \"0.9999999999999999\\n\" and nothing on stderr", h, stdout, stderr)
		}
		if got := m.Stats().MaxEnvs; got != 2 {
			t.Errorf("Stats().MaxEnvs = %d, want 2 (main and sum: the tail call reuses its environment)", got)
		}
	})

	t.Run("call_passes_and_returns_a_float", func(t *testing.T) {
		// Call hands a Float to a function and gets one back.
		_, got := call(t, "fn half(x: Float) -> Float { x / 2.0 }", "half", interp.FloatV{V: 5})
		if got != (interp.FloatV{V: 2.5}) {
			t.Errorf("Call(half, 5.0) = %#v, want FloatV{2.5}", got)
		}
	})
}
