package interp

import (
	"math"
	"testing"
)

// fakeV is a Value that this package does not define. It can only be written
// here, inside the package, because value is unexported: that is the point of
// the sealed interface, and the only way to hand Show a form it has no case for.
type fakeV struct{}

func (fakeV) value() {}

func TestValue(t *testing.T) {
	t.Run("show", func(t *testing.T) {
		// Each form of Value, and for Int the values that a careless conversion
		// gets wrong: a negative number, and the extremes of an int64.
		rows := []struct {
			v    Value
			want string
		}{
			{IntV{V: 42}, "42"},
			{IntV{V: 0}, "0"},
			{IntV{V: -7}, "-7"},
			{IntV{V: math.MaxInt64}, "9223372036854775807"},
			{IntV{V: math.MinInt64}, "-9223372036854775808"},
			{BoolV{V: true}, "true"},
			{BoolV{V: false}, "false"},
			{UnitV{}, "()"},
			{FunV{Fun: 0}, "<fn>"},
			{FunV{Fun: 3}, "<fn>"},
		}
		for _, row := range rows {
			if got := Show(row.v); got != row.want {
				t.Errorf("Show(%#v) = %q, want %q", row.v, got, row.want)
			}
		}
	})

	t.Run("unknown_value_is_unreachable", func(t *testing.T) {
		var got any
		func() {
			defer func() { got = recover() }()
			Show(fakeV{})
		}()
		if got != "unreachable" {
			t.Errorf("Show(fakeV{}) panicked with %#v, want the string \"unreachable\"", got)
		}
	})
}
