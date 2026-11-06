package interp

import (
	"math"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
)

func TestShowData(t *testing.T) {
	t.Run("tuples_and_records", func(t *testing.T) {
		// A tuple has no name: it is its fields between parentheses. A record is its
		// name and its fields between braces, each with its name, in the order the
		// record declares them. A variant that names its fields is not a record: it
		// is written as any other, with the fields' values alone.
		in := base.NewInterner()
		p := &core.Program{}
		sym := in.InternString
		tuple := func(arity uint32) core.CtorID { return p.Ctors.Push(core.Ctor{Arity: arity, Tuple: true}) }
		point := p.Ctors.Push(core.Ctor{Name: sym("Point"), Arity: 2, Record: true, Fields: []base.Symbol{sym("x"), sym("y")}})
		line := p.Ctors.Push(core.Ctor{Name: sym("Line"), Arity: 2, Record: true, Fields: []base.Symbol{sym("from"), sym("to")}})
		empty := p.Ctors.Push(core.Ctor{Name: sym("Empty"), Record: true})
		rect := p.Ctors.Push(core.Ctor{Name: sym("Rect"), Arity: 2, Fields: []base.Symbol{sym("w"), sym("h")}})
		two, three := tuple(2), tuple(3)
		pt := func(x, y float64) Value { return ConV{Ctor: point, Fields: []Value{FloatV{V: x}, FloatV{V: y}}} }
		rows := []struct {
			name string
			v    Value
			want string
		}{
			{"pair", ConV{Ctor: two, Fields: []Value{IntV{V: 1}, BoolV{V: true}}}, "(1, true)"},
			{"three", ConV{Ctor: three, Fields: []Value{IntV{V: 1}, UnitV{}, FloatV{V: 0.1}}}, "(1, (), 0.1)"},
			{"nested", ConV{Ctor: two, Fields: []Value{ConV{Ctor: two, Fields: []Value{IntV{V: 1}, IntV{V: 2}}}, ConV{Ctor: three, Fields: []Value{IntV{V: 3}, IntV{V: 4}, IntV{V: 5}}}}}, "((1, 2), (3, 4, 5))"},
			{"record", pt(1, 2), "Point { x: 1.0, y: 2.0 }"},
			{"record with special floats", pt(math.Copysign(0, -1), math.NaN()), "Point { x: -0.0, y: NaN }"},
			{"record in a record", ConV{Ctor: line, Fields: []Value{pt(0, 0), pt(1, 1)}}, "Line { from: Point { x: 0.0, y: 0.0 }, to: Point { x: 1.0, y: 1.0 } }"},
			{"record with no fields", ConV{Ctor: empty}, "Empty"},
			{"record in a tuple", ConV{Ctor: two, Fields: []Value{pt(1, 2), ConV{Ctor: two, Fields: []Value{IntV{V: 1}, pt(3, 4)}}}}, "(Point { x: 1.0, y: 2.0 }, (1, Point { x: 3.0, y: 4.0 }))"},
			{"a tuple in a record", ConV{Ctor: line, Fields: []Value{ConV{Ctor: two, Fields: []Value{IntV{V: 1}, IntV{V: 2}}}, UnitV{}}}, "Line { from: (1, 2), to: () }"},
			{"variant with named fields", ConV{Ctor: rect, Fields: []Value{FloatV{V: 2}, FloatV{V: 3}}}, "Rect(2.0, 3.0)"},
		}
		for _, row := range rows {
			if got := Show(row.v, p, in); got != row.want {
				t.Errorf("%s: Show = %q, want %q", row.name, got, row.want)
			}
		}

		// Nested 100 000 deep, with the stack of Go capped at 1 MiB: Show has a stack
		// of its own for the braces and the names as for the parentheses.
		const depth = 100_000
		var tup, rec Value = IntV{V: 1}, IntV{V: 1}
		for range depth {
			tup = ConV{Ctor: two, Fields: []Value{tup, UnitV{}}}
			rec = ConV{Ctor: line, Fields: []Value{rec, UnitV{}}}
		}
		wantTuple := strings.Repeat("(", depth) + "1" + strings.Repeat(", ())", depth)
		wantRecord := strings.Repeat("Line { from: ", depth) + "1" + strings.Repeat(", to: () }", depth)
		old := debug.SetMaxStack(1 << 20)
		defer debug.SetMaxStack(old)
		if got := Show(tup, p, in); got != wantTuple {
			t.Errorf("Show of %d nested tuples wrote %d bytes, want %d", depth, len(got), len(wantTuple))
		}
		if got := Show(rec, p, in); got != wantRecord {
			t.Errorf("Show of %d nested records wrote %d bytes, want %d", depth, len(got), len(wantRecord))
		}
	})
}
