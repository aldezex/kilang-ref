package interp

import (
	"math"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
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
			{FloatV{V: 1.5}, "1.5"},
			{FloatV{V: 2}, "2.0"},
			{FloatV{V: 0.1}, "0.1"},
			{FloatV{V: 1e21}, "1e21"},
			{FloatV{V: math.NaN()}, "NaN"},
			{FloatV{V: math.Inf(-1)}, "-inf"},
			{FloatV{V: math.Copysign(0, -1)}, "-0.0"},
			{BoolV{V: true}, "true"},
			{BoolV{V: false}, "false"},
			{UnitV{}, "()"},
			{FunV{Fun: 0}, "<fn>"},
			{FunV{Fun: 3}, "<fn>"},
		}
		for _, row := range rows {
			if got := Show(row.v, nil, nil); got != row.want {
				t.Errorf("Show(%#v) = %q, want %q", row.v, got, row.want)
			}
		}
	})

	t.Run("show_constructors", func(t *testing.T) {
		// A value that a constructor built is written as the constructor's name,
		// from the Program and the interner that Show is given, and its fields in
		// parentheses in the order they have, each written as a value is, however
		// deep: a constructor with no fields is the name alone, and one whose
		// field is a constructor with none writes it as such.
		in := base.NewInterner()
		p := &core.Program{}
		ctor := func(name string, arity uint32) core.CtorID {
			return p.Ctors.Push(core.Ctor{Name: in.InternString(name), Arity: arity})
		}
		none, circle, pair, wrap := ctor("None", 0), ctor("Circle", 1), ctor("Pair", 2), ctor("Wrap", 1)
		rows := []struct {
			name string
			v    Value
			want string
		}{
			{"no fields", ConV{Ctor: none}, "None"},
			{"no fields, an empty list", ConV{Ctor: none, Fields: []Value{}}, "None"},
			{"a float", ConV{Ctor: circle, Fields: []Value{FloatV{V: 1}}}, "Circle(1.0)"},
			{"two fields of two forms", ConV{Ctor: pair, Fields: []Value{IntV{V: -7}, BoolV{V: true}}}, "Pair(-7, true)"},
			{"a unit and a function", ConV{Ctor: pair, Fields: []Value{UnitV{}, FunV{Fun: 4}}}, "Pair((), <fn>)"},
			{"nested in the second", ConV{Ctor: pair, Fields: []Value{IntV{V: 1}, ConV{Ctor: pair, Fields: []Value{IntV{V: 2}, ConV{Ctor: none}}}}}, "Pair(1, Pair(2, None))"},
			{"nested in the first", ConV{Ctor: pair, Fields: []Value{ConV{Ctor: circle, Fields: []Value{FloatV{V: 0.1}}}, ConV{Ctor: none}}}, "Pair(Circle(0.1), None)"},
			{"nested in both", ConV{Ctor: pair, Fields: []Value{ConV{Ctor: wrap, Fields: []Value{IntV{V: 1}}}, ConV{Ctor: wrap, Fields: []Value{IntV{V: 2}}}}}, "Pair(Wrap(1), Wrap(2))"},
			{"negative zero and not a number", ConV{Ctor: pair, Fields: []Value{FloatV{V: math.Copysign(0, -1)}, FloatV{V: math.NaN()}}}, "Pair(-0.0, NaN)"},
		}
		for _, row := range rows {
			if got := Show(row.v, p, in); got != row.want {
				t.Errorf("%s: Show = %q, want %q", row.name, got, row.want)
			}
		}
		// The same constructor, written twice by two different Shows, reads the
		// same name: the value holds no text.
		if a, b := Show(ConV{Ctor: wrap, Fields: []Value{IntV{V: 1}}}, p, in), Show(ConV{Ctor: wrap, Fields: []Value{IntV{V: 1}}}, p, in); a != b {
			t.Errorf("Show of the same value twice is %q and %q", a, b)
		}

		// A value as deep as a list of 100 000 elements is written with the stack
		// of Go capped at 1 MiB: Show has a stack of its own.
		const depth = 100_000
		var v Value = ConV{Ctor: none}
		for i := 0; i < depth; i++ {
			v = ConV{Ctor: wrap, Fields: []Value{v}}
		}
		want := strings.Repeat("Wrap(", depth) + "None" + strings.Repeat(")", depth)
		old := debug.SetMaxStack(1 << 20)
		defer debug.SetMaxStack(old)
		if got := Show(v, p, in); got != want {
			t.Errorf("Show of %d nested constructors wrote %d bytes, want %d", depth, len(got), len(want))
		}
	})

	t.Run("unknown_value_is_unreachable", func(t *testing.T) {
		var got any
		func() {
			defer func() { got = recover() }()
			Show(fakeV{}, nil, nil)
		}()
		if got != "unreachable" {
			t.Errorf("Show(fakeV{}, nil, nil) panicked with %#v, want the string \"unreachable\"", got)
		}
	})
}
