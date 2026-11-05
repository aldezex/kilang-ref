package interp

import (
	"fmt"
	"io"
	"math"
	"os"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
)

// This is the half of the canary C10 that runs in the interpreter (spec 5.10,
// "Floats idénticos en todos los backends"; the other half is TestC10 in
// kirt/canary). Go may fuse `x*y + z` into one instruction that rounds once, and
// arm64 does it by default, so the same Float program could give two answers on
// two machines. The operands here are the ones where the fused and the unfused
// answer differ in every bit that matters:
//
//	a = 1 + 2^-52    d = 1 + 2^-51    a*a = 1 + 2^-51 + 2^-104
//
// The product a*a rounds to d, and d - d is 0, but a fused a*a - d is 2^-104.
//
// The test lives in package interp, and not in kirt/canary, because kirt cannot
// import the interpreter (kirt/imports_test.go) and applyPrim is not exported.
// It is a file of its own, apart from prims_internal_test.go, because the canary
// runs it by name in two builds of this package, GOAMD64=v1 and GOAMD64=v3, and
// compares what they print: with KI_C10_BITS set, each row writes the line
// `c10 <name> <result>` to stdout. The test also checks every row against the
// result written here, so it fails in a build that fuses even on its own.

const (
	c10Env = "KI_C10_BITS"

	bitsA = 0x3ff0000000000001 // 1 + 2^-52
	bitsD = 0x3ff0000000000002 // 1 + 2^-51
)

// c10Row is one Float result: the name of the row, how to compute it and the
// text of its result, which is `0x` and the 16 digits of math.Float64bits for a
// Float, "nan" for any NaN (the bits of the NaN that 0/0 makes are not the same
// on every machine), and "true" or "false" for a Bool.
type c10Row struct {
	name string
	run  func(t *testing.T) Value
	want string
}

// c10Text writes a result the way a c10Row's want does.
func c10Text(v Value) string {
	switch v := v.(type) {
	case FloatV:
		if math.IsNaN(v.V) {
			return "nan"
		}
		return fmt.Sprintf("%#016x", math.Float64bits(v.V))
	case BoolV:
		return fmt.Sprint(v.V)
	default:
		return fmt.Sprintf("%#v", v)
	}
}

func TestC10Bits(t *testing.T) {
	a, d := FloatV{V: math.Float64frombits(bitsA)}, FloatV{V: math.Float64frombits(bitsD)}
	zero, negZero := FloatV{V: 0}, FloatV{V: math.Copysign(0, -1)}
	one, minusOne, nan := FloatV{V: 1}, FloatV{V: -1}, FloatV{V: math.NaN()}

	// prim applies one primitive to its operands.
	prim := func(p core.Prim, x, y Value) func(*testing.T) Value {
		return func(t *testing.T) Value {
			t.Helper()
			v, msg := applyPrim(p, x, y)
			if msg != "" {
				t.Fatalf("%s panicked with %q, and the primitives of Float never do", p, msg)
			}
			return v
		}
	}
	// machine runs the Core that build makes, a function of no parameters, on a
	// Machine: the operations meet in Values, as in a program.
	machine := func(build func(b *core.Builder) core.ExprID) func(*testing.T) Value {
		return func(t *testing.T) Value {
			t.Helper()
			p := &core.Program{}
			b := &core.Builder{P: p}
			body := build(b)
			f := p.Funs.Push(core.Fun{Body: body, HasBody: true})
			v, ok := New(p, new(base.SourceManager), io.Discard, io.Discard).Call(f, nil)
			if !ok {
				t.Fatalf("the machine halted on a program of Float operations")
			}
			return v
		}
	}
	lit := func(b *core.Builder, v FloatV) core.ExprID { return b.LitFloat(base.Span{}, v.V) }
	op := func(b *core.Builder, p core.Prim, args ...core.ExprID) core.ExprID {
		return b.Prim(base.Span{}, p, args)
	}

	rows := []c10Row{
		// Every arithmetic primitive, with the operands in both orders where the
		// order matters.
		{"add_a_d", prim(core.PrimAddFloat, a, d), "0x4000000000000002"},
		{"sub_a_d", prim(core.PrimSubFloat, a, d), "0xbcb0000000000000"},
		{"sub_d_a", prim(core.PrimSubFloat, d, a), "0x3cb0000000000000"},
		{"mul_a_d", prim(core.PrimMulFloat, a, d), "0x3ff0000000000003"},
		{"mul_a_a", prim(core.PrimMulFloat, a, a), "0x3ff0000000000002"},
		{"div_a_d", prim(core.PrimDivFloat, a, d), "0x3feffffffffffffe"},
		{"div_d_a", prim(core.PrimDivFloat, d, a), "0x3ff0000000000001"},
		{"neg_a", prim(core.PrimNegFloat, a, nil), "0xbff0000000000001"},
		{"neg_zero", prim(core.PrimNegFloat, zero, nil), "0x8000000000000000"},
		{"neg_negative_zero", prim(core.PrimNegFloat, negZero, nil), "0x0000000000000000"},
		// IEEE: no panic for a division by zero, and an overflow is an infinity.
		{"div_one_zero", prim(core.PrimDivFloat, one, zero), "0x7ff0000000000000"},
		{"div_minus_one_zero", prim(core.PrimDivFloat, minusOne, zero), "0xfff0000000000000"},
		{"div_one_negative_zero", prim(core.PrimDivFloat, one, negZero), "0xfff0000000000000"},
		{"div_zero_zero", prim(core.PrimDivFloat, zero, zero), "nan"},
		{"mul_overflow", prim(core.PrimMulFloat, FloatV{V: 1e308}, FloatV{V: 10}), "0x7ff0000000000000"},
		// The comparisons, which are IEEE's: a NaN is unordered and equal to nothing.
		{"eq_a_a", prim(core.PrimEqFloat, a, a), "true"},
		{"eq_a_d", prim(core.PrimEqFloat, a, d), "false"},
		{"eq_zero_negative_zero", prim(core.PrimEqFloat, zero, negZero), "true"},
		{"eq_nan_nan", prim(core.PrimEqFloat, nan, nan), "false"},
		{"ne_a_a", prim(core.PrimNeFloat, a, a), "false"},
		{"ne_a_d", prim(core.PrimNeFloat, a, d), "true"},
		{"ne_nan_nan", prim(core.PrimNeFloat, nan, nan), "true"},
		{"lt_a_d", prim(core.PrimLtFloat, a, d), "true"},
		{"lt_d_a", prim(core.PrimLtFloat, d, a), "false"},
		{"lt_a_a", prim(core.PrimLtFloat, a, a), "false"},
		{"lt_nan_one", prim(core.PrimLtFloat, nan, one), "false"},
		{"le_a_d", prim(core.PrimLeFloat, a, d), "true"},
		{"le_d_a", prim(core.PrimLeFloat, d, a), "false"},
		{"le_a_a", prim(core.PrimLeFloat, a, a), "true"},
		{"le_nan_nan", prim(core.PrimLeFloat, nan, nan), "false"},
		{"gt_a_d", prim(core.PrimGtFloat, a, d), "false"},
		{"gt_d_a", prim(core.PrimGtFloat, d, a), "true"},
		{"gt_a_a", prim(core.PrimGtFloat, a, a), "false"},
		{"gt_one_nan", prim(core.PrimGtFloat, one, nan), "false"},
		{"ge_a_d", prim(core.PrimGeFloat, a, d), "false"},
		{"ge_d_a", prim(core.PrimGeFloat, d, a), "true"},
		{"ge_a_a", prim(core.PrimGeFloat, a, a), "true"},
		{"ge_nan_nan", prim(core.PrimGeFloat, nan, nan), "false"},

		// The shapes that a compiler fuses, run on a Machine, so each operation
		// is a primitive of its own: a*a + c, a*a - d, d - a*a and -(a*a) + d. All
		// four are 0 when each operation rounds, and ±2^-104 (0x3970000000000000)
		// when the product is not rounded before the sum.
		{"a_times_a_plus_minus_d", machine(func(b *core.Builder) core.ExprID {
			return op(b, core.PrimAddFloat, op(b, core.PrimMulFloat, lit(b, a), lit(b, a)), lit(b, FloatV{V: -d.V}))
		}), "0x0000000000000000"},
		{"a_times_a_minus_d", machine(func(b *core.Builder) core.ExprID {
			return op(b, core.PrimSubFloat, op(b, core.PrimMulFloat, lit(b, a), lit(b, a)), lit(b, d))
		}), "0x0000000000000000"},
		{"d_minus_a_times_a", machine(func(b *core.Builder) core.ExprID {
			return op(b, core.PrimSubFloat, lit(b, d), op(b, core.PrimMulFloat, lit(b, a), lit(b, a)))
		}), "0x0000000000000000"},
		{"negated_product_plus_d", machine(func(b *core.Builder) core.ExprID {
			return op(b, core.PrimAddFloat, op(b, core.PrimNegFloat, op(b, core.PrimMulFloat, lit(b, a), lit(b, a))), lit(b, d))
		}), "0x0000000000000000"},
	}

	echo := os.Getenv(c10Env) != ""
	for _, row := range rows {
		got := c10Text(row.run(t))
		if echo {
			fmt.Printf("c10 %s %s\n", row.name, got)
		}
		if got != row.want {
			t.Errorf("%s: the result is %s, want %s", row.name, got, row.want)
		}
	}
}
