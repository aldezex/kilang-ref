package interp_test

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/interp"
	"github.com/aldezex/kilang/internal/testsupport"
)

// manyLets returns n lines of `let`, each a variable with a slot of its own in the
// frame of the function they are in.
func manyLets(n int) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, "  let a%d = n\n", i)
	}
	return sb.String()
}

// machineOf returns a new Machine for the program c, and the buffers that its
// stdout and its stderr go to.
func machineOf(c *testsupport.Compiled) (m *interp.Machine, out, errOut *bytes.Buffer) {
	out, errOut = new(bytes.Buffer), new(bytes.Buffer)
	return interp.New(c.Core, &c.Session.Sources, out, errOut), out, errOut
}

// call compiles src and calls the function called name with args on a new
// Machine. It returns the machine, so that a test can read its Stats, and the
// result. It fails the test if the call reports a panic, and also if the
// machine itself panics in Go (an index out of range, say): left alone, that
// panic would end the whole test binary and hide every test after it.
func call(t *testing.T, src, name string, args ...interp.Value) (*interp.Machine, interp.Value) {
	t.Helper()
	c := testsupport.Compile(t, src)
	m, _, errOut := machineOf(c)
	fun := testsupport.FunNamed(t, c, name)
	var v interp.Value
	var ok bool
	if got := panicText(func() { v, ok = m.Call(fun, args) }); got != nil {
		t.Fatalf("Call(%s) of %q panicked in Go with %v", name, src, got)
	}
	if !ok {
		t.Fatalf("Call(%s) of %q reported a panic, stderr %q", name, src, errOut.String())
	}
	return m, v
}

// panicText runs work and returns what it panicked with, or nil if it returned.
func panicText(work func()) (got any) {
	defer func() { got = recover() }()
	work()
	return nil
}

// runMainOn compiles src and runs its `main` on a new Machine. It returns the
// machine, so that a test can read its Stats, the state the machine ends in and
// what the program wrote to stdout and to stderr.
func runMainOn(t *testing.T, src string) (m *interp.Machine, h interp.Halt, stdout, stderr string) {
	t.Helper()
	m, out, errOut := machineOf(testsupport.Compile(t, src))
	h = m.RunMain()
	return m, h, out.String(), errOut.String()
}

// runMain is runMainOn for a test that does not read the machine.
func runMain(t *testing.T, src string) (h interp.Halt, stdout, stderr string) {
	t.Helper()
	_, h, stdout, stderr = runMainOn(t, src)
	return h, stdout, stderr
}

// wantHalt checks that running the `main` of src ends in the state want, with
// the message wantErr (the whole of stderr) and nothing on stdout.
func wantHalt(t *testing.T, src string, want interp.Halt, wantErr string) {
	t.Helper()
	h, stdout, stderr := runMain(t, src)
	if h != want {
		t.Errorf("RunMain() of %q = %+v, want %+v", src, h, want)
	}
	if stderr != wantErr {
		t.Errorf("stderr of %q = %q, want %q", src, stderr, wantErr)
	}
	if stdout != "" {
		t.Errorf("stdout of %q = %q, want it empty", src, stdout)
	}
}

// wantPanic checks that running the `main` of src ends in a Ki panic: Halt{101},
// the message wantErr and nothing on stdout.
func wantPanic(t *testing.T, src, wantErr string) {
	t.Helper()
	wantHalt(t, src, interp.Halt{Code: 101}, wantErr)
}

func TestMachine(t *testing.T) {
	t.Run("literal", func(t *testing.T) {
		rows := []struct {
			name, src string
			want      interp.Value
		}{
			{"int", "fn f() -> Int { 42 }", interp.IntV{V: 42}},
			{"zero", "fn f() -> Int { 0 }", interp.IntV{V: 0}},
			{"max_int", "fn f() -> Int { 9223372036854775807 }", interp.IntV{V: math.MaxInt64}},
			{"true", "fn f() -> Bool { true }", interp.BoolV{V: true}},
			{"false", "fn f() -> Bool { false }", interp.BoolV{V: false}},
		}
		for _, row := range rows {
			m, got := call(t, row.src, "f")
			if got != row.want {
				t.Errorf("%s: Call(f) = %#v, want %#v", row.name, got, row.want)
			}
			// A literal is one step, and it needs no frame: only the environment
			// of the call.
			if s, want := m.Stats(), (interp.Stats{Steps: 1, MaxDepth: 0, MaxEnvs: 1}); s != want {
				t.Errorf("%s: Stats() = %+v, want %+v", row.name, s, want)
			}
		}
	})

	t.Run("unit_literal", func(t *testing.T) {
		m, got := call(t, "fn f() {}", "f")
		if got != (interp.UnitV{}) {
			t.Errorf("Call(f) = %#v, want UnitV{}", got)
		}
		if s, want := m.Stats(), (interp.Stats{Steps: 1, MaxDepth: 0, MaxEnvs: 1}); s != want {
			t.Errorf("Stats() = %+v, want %+v", s, want)
		}
	})

	t.Run("let_binding", func(t *testing.T) {
		rows := []struct {
			name, src string
			want      interp.Value
			depth     int // the most frames the stack holds at once
		}{
			{"read_back", "fn f() -> Int { let x = 1\n let y = x\n y }", interp.IntV{V: 1}, 1},
			// Two variables with different values, read in both orders: a Let that
			// wrote every variable in the same slot would answer 2 to the first.
			{"first_of_two", "fn f() -> Int { let a = 1\n let b = 2\n a }", interp.IntV{V: 1}, 1},
			{"second_of_two", "fn f() -> Int { let a = 1\n let b = 2\n b }", interp.IntV{V: 2}, 1},
			{"bool_and_unit", "fn f() -> Bool { let u = ()\n let b = true\n b }", interp.BoolV{V: true}, 1},
			{"unit_after_bool", "fn f() { let b = true\n let u = ()\n u }", interp.UnitV{}, 1},
			{"shadowing_reads_the_latest", "fn f() -> Int { let x = 1\n let x = 2\n x }", interp.IntV{V: 2}, 1},
			{"shadowed_value_survives", "fn f() -> Int { let x = 1\n let y = x\n let x = 2\n y }", interp.IntV{V: 1}, 1},
			// A Let inside the Init of another: two letFrames are waiting at once,
			// and the value of the inner Init must go to the inner one, which is the
			// last pushed. The `let c` is there on purpose. A machine that took its
			// frames from the other end would hand the 1 of b to the frame of a, read
			// a, and hand that 1 to the frame of b: the 2 would reach no frame and
			// the program would answer 2 all the same. With a `let c` still to run,
			// the 2 goes to the frame of c and the program answers the 1 of a.
			{"let_inside_init", "fn f() -> Int { let a = { let b = 1\n 2 }\n let c = 3\n a }", interp.IntV{V: 2}, 2},
		}
		for _, row := range rows {
			m, got := call(t, row.src, "f")
			if got != row.want {
				t.Errorf("%s: Call(f) = %#v, want %#v", row.name, got, row.want)
			}
			// The stack holds one letFrame for each Let whose Init is being
			// evaluated, and a Let that is in the body of another adds none; and
			// there is one environment.
			if s := m.Stats(); s.MaxDepth != row.depth || s.MaxEnvs != 1 {
				t.Errorf("%s: Stats() = %+v, want MaxDepth %d and MaxEnvs 1", row.name, s, row.depth)
			}
		}
	})

	t.Run("let_chain_uses_constant_stack", func(t *testing.T) {
		// let x0 = 7; let x1 = x0; … let x999 = x998; x999
		const n = 1000
		var src strings.Builder
		src.WriteString("fn f() -> Int {\n let x0 = 7\n")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&src, " let x%d = x%d\n", i, i-1)
		}
		fmt.Fprintf(&src, " x%d\n}", n-1)

		m, got := call(t, src.String(), "f")
		if got != (interp.IntV{V: 7}) {
			t.Errorf("Call(f) = %#v, want IntV{7}", got)
		}
		// Each Let takes three steps (evaluate the Let, evaluate its Init, apply
		// its frame) and the tail one more. The stack never holds more than one
		// frame: applying a letFrame pushes nothing.
		if s, want := m.Stats(), (interp.Stats{Steps: 3*n + 1, MaxDepth: 1, MaxEnvs: 1}); s != want {
			t.Errorf("Stats() = %+v, want %+v", s, want)
		}
	})

	t.Run("call_binds_arguments", func(t *testing.T) {
		const src = `
fn first(a: Int, b: Int, c: Int) -> Int { a }
fn second(a: Int, b: Int, c: Int) -> Int { b }
fn third(a: Int, b: Int, c: Int) -> Int { c }
fn keepsParam(a: Int, b: Int) -> Int { let w = b
 a }
fn readsLet(a: Int, b: Int) -> Int { let w = b
 w }
fn idBool(x: Bool) -> Bool { x }
fn idUnit(x: ()) -> () { x }
fn idInt(x: Int) -> Int { x }
`
		i := func(v int64) interp.Value { return interp.IntV{V: v} }
		rows := []struct {
			name, fun string
			args      []interp.Value
			want      interp.Value
		}{
			{"first", "first", []interp.Value{i(1), i(2), i(3)}, i(1)},
			{"second", "second", []interp.Value{i(1), i(2), i(3)}, i(2)},
			{"third", "third", []interp.Value{i(1), i(2), i(3)}, i(3)},
			// The Let gets a slot of its own: it must not overwrite a parameter.
			{"let_keeps_parameters", "keepsParam", []interp.Value{i(10), i(20)}, i(10)},
			{"let_reads_parameter", "readsLet", []interp.Value{i(10), i(20)}, i(20)},
			// The machine has no types: whatever form of Value it is given comes back.
			{"bool", "idBool", []interp.Value{interp.BoolV{V: true}}, interp.BoolV{V: true}},
			{"unit", "idUnit", []interp.Value{interp.UnitV{}}, interp.UnitV{}},
			{"function", "idInt", []interp.Value{interp.FunV{Fun: 7}}, interp.FunV{Fun: 7}},
			{"min_int", "idInt", []interp.Value{i(math.MinInt64)}, i(math.MinInt64)},
		}
		for _, row := range rows {
			_, got := call(t, src, row.fun, row.args...)
			if got != row.want {
				t.Errorf("%s: Call(%s, %v) = %#v, want %#v", row.name, row.fun, row.args, got, row.want)
			}
		}
	})

	t.Run("slots_come_from_the_variables", func(t *testing.T) {
		// A Program made by hand, in which the slot of a variable is not the
		// position of its argument: the parameters a and b are in slots 2 and 1,
		// and the variable of the Let is in slot 0. The elaboration numbers the
		// slots 0, 1, 2… in the order of the source, so only a Program like this
		// tells a machine that reads Var.Slot from one that takes the position of
		// the argument. The ids of a and w (0 and 2) differ from their slots (2 and
		// 0), so a machine that writes by one and reads by the other fails here;
		// one that used the id everywhere would not. Ids belong to the whole
		// program, so that machine would index outside its environment as soon as
		// an earlier function has variables, and the tests that compile real
		// programs catch it.
		p := &core.Program{}
		b := core.Builder{P: p}
		sp := base.Span{File: 1}
		a := p.Vars.Push(core.Var{Slot: 2})
		bv := p.Vars.Push(core.Var{Slot: 1})
		w := p.Vars.Push(core.Var{Slot: 0})
		// fn retA(a, b) = a
		retA := p.Funs.Push(core.Fun{Params: []core.VarID{a, bv}, Body: b.VarRef(sp, a), HasBody: true, FrameSize: 3})
		// fn retLet(a, b) = let w = b; w
		retLet := p.Funs.Push(core.Fun{
			Params:  []core.VarID{a, bv},
			Body:    b.Let(sp, w, b.VarRef(sp, bv), b.VarRef(sp, w)),
			HasBody: true, FrameSize: 3,
		})
		args := []interp.Value{interp.IntV{V: 10}, interp.IntV{V: 20}}
		for _, row := range []struct {
			name string
			fun  core.FunID
			want interp.Value
		}{
			{"parameter", retA, interp.IntV{V: 10}},
			{"let_variable", retLet, interp.IntV{V: 20}},
		} {
			var out, errOut bytes.Buffer
			m := interp.New(p, new(base.SourceManager), &out, &errOut)
			got, ok := m.Call(row.fun, args)
			if !ok || got != row.want {
				t.Errorf("%s: Call = (%#v, %v), want (%#v, true)", row.name, got, ok, row.want)
			}
		}
	})

	t.Run("stats_accumulate_over_calls", func(t *testing.T) {
		c := testsupport.Compile(t, "fn f(x: Int) -> Int { let y = x\n y }")
		m, _, _ := machineOf(c)
		f := testsupport.FunNamed(t, c, "f")

		// Four steps: the Let, the read of x, the frame, the read of y.
		got, ok := m.Call(f, []interp.Value{interp.IntV{V: 5}})
		if !ok || got != (interp.IntV{V: 5}) {
			t.Errorf("first Call(f, 5) = (%#v, %v), want (IntV{5}, true)", got, ok)
		}
		if s, want := m.Stats(), (interp.Stats{Steps: 4, MaxDepth: 1, MaxEnvs: 1}); s != want {
			t.Errorf("Stats() after the first call = %+v, want %+v", s, want)
		}
		// The second call starts with its own environment (a leftover of the
		// first would make MaxEnvs 2), and the counts go on.
		got, ok = m.Call(f, []interp.Value{interp.IntV{V: 6}})
		if !ok || got != (interp.IntV{V: 6}) {
			t.Errorf("second Call(f, 6) = (%#v, %v), want (IntV{6}, true)", got, ok)
		}
		if s, want := m.Stats(), (interp.Stats{Steps: 8, MaxDepth: 1, MaxEnvs: 1}); s != want {
			t.Errorf("Stats() after the second call = %+v, want %+v", s, want)
		}
	})

	t.Run("run_main", func(t *testing.T) {
		// helper comes before main, so it has the smaller id: a RunMain that ran
		// the first function of the file would take seven steps and not four.
		c := testsupport.Compile(t, "fn helper() -> Int { let a = 1\n let b = 2\n a }\nfn main() { let x = 1 }")
		m, out, errOut := machineOf(c)
		if h := m.RunMain(); h != (interp.Halt{Code: 0}) {
			t.Errorf("RunMain() = %+v, want Halt{Code: 0}", h)
		}
		// The Let, its Init, its frame and the `()` that ends the block.
		if s := m.Stats(); s.Steps != 4 {
			t.Errorf("Stats().Steps = %d, want 4 (the steps of main)", s.Steps)
		}
		if out.Len() != 0 || errOut.Len() != 0 {
			t.Errorf("RunMain wrote stdout %q and stderr %q, want both empty", out.String(), errOut.String())
		}
	})

	t.Run("misuse_panics", func(t *testing.T) {
		c := testsupport.Compile(t, "fn f(a: Int, b: Int) -> Int { a }")
		f := testsupport.FunNamed(t, c, "f")
		// The prelude's `print` is the function that has no body.
		var noBody core.FunID
		for id, fn := range c.Core.Funs.All() {
			if !fn.HasBody {
				noBody = id
			}
		}
		newMachine := func() *interp.Machine {
			m, _, _ := machineOf(c)
			return m
		}
		one := []interp.Value{interp.IntV{V: 1}}
		three := []interp.Value{interp.IntV{V: 1}, interp.IntV{V: 2}, interp.IntV{V: 3}}
		rows := []struct {
			name string
			work func()
			want string
		}{
			{"too_few_arguments", func() { newMachine().Call(f, one) },
				fmt.Sprintf("interp: function %d takes 2 arguments, but Call got 1", f)},
			{"too_many_arguments", func() { newMachine().Call(f, three) },
				fmt.Sprintf("interp: function %d takes 2 arguments, but Call got 3", f)},
			{"no_arguments", func() { newMachine().Call(f, nil) },
				fmt.Sprintf("interp: function %d takes 2 arguments, but Call got 0", f)},
			{"function_without_body", func() { newMachine().Call(noBody, one) },
				fmt.Sprintf("interp: Call of function %d, which has no body", noBody)},
			{"main_missing", func() { newMachine().RunMain() },
				"interp: RunMain on a program without a main function"},
		}
		for _, row := range rows {
			if got := panicText(row.work); got != row.want {
				t.Errorf("%s: panicked with %#v, want the string %q", row.name, got, row.want)
			}
		}
	})

	t.Run("add_and_compare", func(t *testing.T) {
		rows := []struct {
			name, src string
			want      bool
		}{
			{"sum_equals", "fn f() -> Bool { 1 + 2 == 3 }", true},
			{"sum_differs", "fn f() -> Bool { 1 + 2 == 4 }", false},
		}
		for _, row := range rows {
			_, got := call(t, row.src, "f")
			if got != (interp.BoolV{V: row.want}) {
				t.Errorf("%s: Call(f) = %#v, want BoolV{%v}", row.name, got, row.want)
			}
		}
	})

	t.Run("arithmetic", func(t *testing.T) {
		rows := []struct {
			name, expr string
			want       int64
		}{
			{"add", "40 + 2", 42},
			{"sub", "10 - 3", 7},
			{"sub_not_commutative", "3 - 10", -7},
			{"mul", "6 * 7", 42},
			{"neg", "-(5)", -5},
			{"neg_of_negative", "-(-5)", 5},
			{"neg_max", "-9223372036854775807", math.MinInt64 + 1},
			// The result is exactly MinInt64: it fits, so it does not panic.
			{"mul_to_min", "-2 * 4611686018427387904", math.MinInt64},
			{"min_by_one", "(-9223372036854775807 - 1) * 1", math.MinInt64},
			{"sum_of_products", "2 * 3 + 4 * 5", 26},
			{"difference_of_products", "2 * 3 - 4 * 5", -14},
			{"products_of_sums", "(1 + 2) * (3 + 4)", 21},
			{"right_operand_is_a_sum", "10 - (2 + 3)", 5},
		}
		for _, row := range rows {
			_, got := call(t, "fn f() -> Int { "+row.expr+" }", "f")
			if got != (interp.IntV{V: row.want}) {
				t.Errorf("%s: %s = %#v, want IntV{%d}", row.name, row.expr, got, row.want)
			}
		}
	})

	t.Run("primitive_steps", func(t *testing.T) {
		// A primitive takes a step for itself, one for each argument and one for
		// each frame it applies: a binary one applies two (the first argument is
		// ready, then the second), a unary one applies one. The stack holds one
		// primFrame at a time, and two while the arguments of the outer primitive
		// are themselves primitives.
		rows := []struct {
			name, expr string
			want       interp.Stats
		}{
			{"binary", "1 + 2", interp.Stats{Steps: 5, MaxDepth: 1, MaxEnvs: 1}},
			{"unary", "-5", interp.Stats{Steps: 3, MaxDepth: 1, MaxEnvs: 1}},
			{"nested", "(1 + 2) * (3 + 4)", interp.Stats{Steps: 13, MaxDepth: 2, MaxEnvs: 1}},
		}
		for _, row := range rows {
			m, _ := call(t, "fn f() -> Int { "+row.expr+" }", "f")
			if got := m.Stats(); got != row.want {
				t.Errorf("%s: Stats() = %+v, want %+v", row.name, got, row.want)
			}
		}
	})

	t.Run("comparisons", func(t *testing.T) {
		const minSrc = "(-9223372036854775807 - 1)"
		const maxSrc = "9223372036854775807"
		ops := []string{"<", "<=", ">", ">=", "==", "!="}
		// For each pair of operands, the answer of each operator in the order of
		// ops, T for true and F for false. The pairs have the first operand less
		// than, equal to and greater than the second, and the negative ones and
		// the extremes tell a signed comparison from an unsigned one.
		rows := []struct{ lhs, rhs, want string }{
			{"1", "2", "TTFFFT"},
			{"2", "2", "FTFTTF"},
			{"3", "2", "FFTTFT"},
			{"-1", "0", "TTFFFT"},
			{"0", "-1", "FFTTFT"},
			{minSrc, maxSrc, "TTFFFT"},
			{maxSrc, minSrc, "FFTTFT"},
			{minSrc, minSrc, "FTFTTF"},
			{maxSrc, maxSrc, "FTFTTF"},
		}
		var src strings.Builder
		for i, row := range rows {
			for j, op := range ops {
				fmt.Fprintf(&src, "fn c%d_%d() -> Bool { %s %s %s }\n", i, j, row.lhs, op, row.rhs)
			}
		}
		c := testsupport.Compile(t, src.String())
		m, _, _ := machineOf(c)
		for i, row := range rows {
			for j, op := range ops {
				f := testsupport.FunNamed(t, c, fmt.Sprintf("c%d_%d", i, j))
				got, ok := m.Call(f, nil)
				want := interp.BoolV{V: row.want[j] == 'T'}
				if !ok || got != want {
					t.Errorf("%s %s %s = (%#v, %v), want (%#v, true)", row.lhs, op, row.rhs, got, ok, want)
				}
			}
		}
	})

	t.Run("bool_primitives", func(t *testing.T) {
		rows := []struct {
			expr string
			want bool
		}{
			{"!true", false},
			{"!false", true},
			{"true == true", true},
			{"true == false", false},
			{"false == true", false},
			{"false == false", true},
			{"true != true", false},
			{"true != false", true},
			{"false != true", true},
			{"false != false", false},
		}
		for _, row := range rows {
			_, got := call(t, "fn f() -> Bool { "+row.expr+" }", "f")
			if got != (interp.BoolV{V: row.want}) {
				t.Errorf("%s = %#v, want BoolV{%v}", row.expr, got, row.want)
			}
		}
	})

	t.Run("division_truncates", func(t *testing.T) {
		rows := []struct {
			expr string
			want int64
		}{
			{"-7 / 2", -3},
			{"7 / -2", -3},
			{"-7 / -2", 3},
			{"7 / 2", 3},
			{"6 / 2", 3},
			{"0 / 5", 0},
		}
		for _, row := range rows {
			_, got := call(t, "fn f() -> Int { "+row.expr+" }", "f")
			if got != (interp.IntV{V: row.want}) {
				t.Errorf("%s = %#v, want IntV{%d}", row.expr, got, row.want)
			}
		}
	})

	t.Run("remainder_sign_of_dividend", func(t *testing.T) {
		rows := []struct {
			expr string
			want int64
		}{
			{"-7 % 3", -1},
			{"7 % -3", 1},
			{"-7 % -3", -1},
			{"7 % 3", 1},
			{"6 % 3", 0},
		}
		for _, row := range rows {
			_, got := call(t, "fn f() -> Int { "+row.expr+" }", "f")
			if got != (interp.IntV{V: row.want}) {
				t.Errorf("%s = %#v, want IntV{%d}", row.expr, got, row.want)
			}
		}
	})

	t.Run("overflow_panics", func(t *testing.T) {
		wantPanic(t, "fn main() { let x = 9223372036854775807 + 1 }",
			"panic: integer overflow at test.ki:1:41\n")
	})

	t.Run("sub_overflow_panics", func(t *testing.T) {
		wantPanic(t, "fn main() { let x = -9223372036854775807 - 2 }",
			"panic: integer overflow at test.ki:1:42\n")
	})

	t.Run("mul_overflow_panics", func(t *testing.T) {
		wantPanic(t, "fn main() { let x = 3037000500 * 3037000500 }",
			"panic: integer overflow at test.ki:1:32\n")
	})

	t.Run("division_by_zero_panics", func(t *testing.T) {
		wantPanic(t, "fn main() { let z = 0\n let x = 1 / z }",
			"panic: division by zero at test.ki:2:12\n")
	})

	t.Run("remainder_by_zero_panics", func(t *testing.T) {
		wantPanic(t, "fn main() { let z = 0\n let x = 1 % z }",
			"panic: division by zero at test.ki:2:12\n")
	})

	t.Run("min_div_minus_one_panics", func(t *testing.T) {
		wantPanic(t, "fn main() { let x = (-9223372036854775807 - 1) / -1 }",
			"panic: integer overflow at test.ki:1:48\n")
	})

	t.Run("neg_min_panics", func(t *testing.T) {
		wantPanic(t, "fn main() { let x = -(-9223372036854775807 - 1) }",
			"panic: integer overflow at test.ki:1:21\n")
	})

	t.Run("min_rem_minus_one_is_zero", func(t *testing.T) {
		_, got := call(t, "fn f() -> Int { (-9223372036854775807 - 1) % -1 }", "f")
		if got != (interp.IntV{V: 0}) {
			t.Errorf("Call(f) = %#v, want IntV{0}", got)
		}
	})

	t.Run("arguments_run_left_to_right", func(t *testing.T) {
		// Both arguments of the outer `+` panic. The one that panics first is
		// the report: the left one, the division by zero.
		wantPanic(t, "fn main() { let x = 1 / 0 + (9223372036854775807 + 1) }",
			"panic: division by zero at test.ki:1:23\n")
	})

	t.Run("call_after_a_panic_starts_clean", func(t *testing.T) {
		// bad panics inside the Init of a Let, so a letFrame is still on the
		// stack when it halts. good must not see that frame: the value of good
		// would go to it, be written in the slot of x and make the machine go on
		// with the body of bad's Let, `x + 100`, and answer 105. good has a second
		// local, w, so that its environment has a slot for x to be written in;
		// with one slot the stale frame would write outside the environment and
		// the failure would be a Go panic that stops the whole test binary, and
		// not the wrong answer. Nor must good see the halt itself.
		c := testsupport.Compile(t, "fn bad(z: Int) -> Int { let x = 1 / z\n x + 100 }\nfn good() -> Int { let y = 5\n let w = 6\n y }")
		m, out, errOut := machineOf(c)
		bad, good := testsupport.FunNamed(t, c, "bad"), testsupport.FunNamed(t, c, "good")

		const msg = "panic: division by zero at test.ki:1:35\n"
		if v, ok := m.Call(bad, []interp.Value{interp.IntV{V: 0}}); v != nil || ok {
			t.Errorf("Call(bad, 0) = (%#v, %v), want (nil, false)", v, ok)
		}
		if errOut.String() != msg {
			t.Errorf("stderr after bad = %q, want %q", errOut.String(), msg)
		}
		if v, ok := m.Call(good, nil); v != (interp.IntV{V: 5}) || !ok {
			t.Errorf("Call(good) after a panic = (%#v, %v), want (IntV{5}, true)", v, ok)
		}
		if v, ok := m.Call(bad, []interp.Value{interp.IntV{V: 2}}); v != (interp.IntV{V: 100}) || !ok {
			t.Errorf("Call(bad, 2) after a panic = (%#v, %v), want (IntV{100}, true)", v, ok)
		}
		if errOut.String() != msg || out.Len() != 0 {
			t.Errorf("after the calls, stderr = %q and stdout = %q, want %q and nothing", errOut.String(), out.String(), msg)
		}
	})

	t.Run("call_with_args", func(t *testing.T) {
		// A call from Ki evaluates the callee and then each argument, left to
		// right, and binds them to the parameters in order. pick gives each
		// parameter a different weight, so a swap or a repeat shows in the number.
		const src = `
fn second(a: Int, b: Int) -> Int { b }
fn zero() -> Int { 7 }
fn one(x: Int) -> Int { x }
fn pick(a: Int, b: Int, c: Int) -> Int { a * 100 + b * 10 + c }
fn callZero() -> Int { zero() }
fn callOne() -> Int { one(5) }
fn callSecond() -> Int { second(1, 2) }
fn callPick() -> Int { pick(1, 2, 3) }
fn callExpressions() -> Int { second(1 + 1, 2 * 3) }
fn callNested() -> Int { pick(one(4), second(0, 5), zero()) }
fn callInsideCall() -> Int { second(0, second(1, 2)) }
fn callBool() -> Bool { idBool(true) }
fn idBool(x: Bool) -> Bool { x }
`
		// The function of the brief, called from outside.
		_, got := call(t, src, "second", interp.IntV{V: 1}, interp.IntV{V: 2})
		if got != (interp.IntV{V: 2}) {
			t.Errorf("Call(second, 1, 2) = %#v, want IntV{2}", got)
		}

		rows := []struct {
			fun  string
			want interp.Value
		}{
			{"callZero", interp.IntV{V: 7}},
			{"callOne", interp.IntV{V: 5}},
			{"callSecond", interp.IntV{V: 2}},
			{"callPick", interp.IntV{V: 123}},
			{"callExpressions", interp.IntV{V: 6}},
			{"callNested", interp.IntV{V: 457}},
			{"callInsideCall", interp.IntV{V: 2}},
			{"callBool", interp.BoolV{V: true}},
		}
		for _, row := range rows {
			if _, got := call(t, src, row.fun); got != row.want {
				t.Errorf("Call(%s) = %#v, want %#v", row.fun, got, row.want)
			}
		}

		// The steps of callSecond: the Call, the callee, the frame of the callee,
		// `1`, its frame, `2`, its frame (which makes the call), the read of b and
		// the retFrame: nine. The stack holds one frame at a time (the argFrame,
		// then the retFrame) and there are two environments, the entry's and the
		// callee's.
		m, _ := call(t, src, "callSecond")
		if s, want := m.Stats(), (interp.Stats{Steps: 9, MaxDepth: 1, MaxEnvs: 2}); s != want {
			t.Errorf("Stats() of callSecond = %+v, want %+v", s, want)
		}
	})

	t.Run("call_arguments_run_left_to_right", func(t *testing.T) {
		// Both arguments panic. The report is the left one, the division by zero.
		wantPanic(t, "fn f(a: Int, b: Int) -> Int { a }\nfn main() { let x = f(1 / 0, 9223372036854775807 + 1) }",
			"panic: division by zero at test.ki:2:25\n")
	})

	t.Run("call_evaluates_the_callee", func(t *testing.T) {
		// The callee of a Call is an expression like any other, and a function can
		// be the value of a variable. dec comes before inc, so a machine that read
		// the function from the callee's own node and not from its value would
		// find no function there (a VarRef has none) and call the first function
		// of the program.
		const src = `
fn dec(x: Int) -> Int { x - 1 }
fn inc(x: Int) -> Int { x + 1 }
fn viaVariable() -> Int { let g = inc
 g(41) }
fn viaTwoVariables() -> Int { let g = dec
 let h = g
 h(41) }
fn bothFunctions() -> Int { let g = dec
 let h = inc
 g(10) * 100 + h(10) }
`
		rows := []struct {
			fun  string
			want int64
		}{
			{"viaVariable", 42},
			{"viaTwoVariables", 40},
			{"bothFunctions", 911},
		}
		for _, row := range rows {
			if _, got := call(t, src, row.fun); got != (interp.IntV{V: row.want}) {
				t.Errorf("Call(%s) = %#v, want IntV{%d}", row.fun, got, row.want)
			}
		}
	})

	t.Run("match_on_bool", func(t *testing.T) {
		rows := []struct {
			name, src string
			want      interp.Value
		}{
			{"then_branch", "fn f() -> Int { if true { 1 } else { 2 } }", interp.IntV{V: 1}},
			{"else_branch", "fn f() -> Int { if false { 1 } else { 2 } }", interp.IntV{V: 2}},
			{"computed_condition_true", "fn f() -> Int { if 1 < 2 { 10 } else { 20 } }", interp.IntV{V: 10}},
			{"computed_condition_false", "fn f() -> Int { if 2 < 1 { 10 } else { 20 } }", interp.IntV{V: 20}},
			// The scrutinee is a call, so the matchFrame waits while a function runs.
			{"condition_is_a_call", "fn yes() -> Bool { true }\nfn f() -> Int { if yes() { 1 } else { 2 } }", interp.IntV{V: 1}},
			{"nested_in_then", "fn f() -> Int { if true { if false { 1 } else { 2 } } else { 3 } }", interp.IntV{V: 2}},
			{"nested_in_else", "fn f() -> Int { if false { 1 } else { if true { 3 } else { 4 } } }", interp.IntV{V: 3}},
			{"without_else_taken", "fn f() { if true { () } }", interp.UnitV{}},
			{"without_else_skipped", "fn f() { if false { () } }", interp.UnitV{}},
			{"bool_result", "fn f() -> Bool { if 3 > 2 { false } else { true } }", interp.BoolV{V: false}},
		}
		for _, row := range rows {
			_, got := call(t, row.src, "f")
			if got != row.want {
				t.Errorf("%s: Call(f) = %#v, want %#v", row.name, got, row.want)
			}
		}

		// Only the arm that fits is evaluated: the Match, its scrutinee, the
		// matchFrame and the arm, four steps, whichever arm it is. The matchFrame
		// is gone when the arm starts, so the arm adds no frame.
		for _, src := range []string{
			"fn f() -> Int { if true { 1 } else { 2 } }",
			"fn f() -> Int { if false { 1 } else { 2 } }",
		} {
			m, _ := call(t, src, "f")
			if s, want := m.Stats(), (interp.Stats{Steps: 4, MaxDepth: 1, MaxEnvs: 1}); s != want {
				t.Errorf("Stats() of %q = %+v, want %+v", src, s, want)
			}
		}
	})

	t.Run("match_arms", func(t *testing.T) {
		// Programs made by hand, because the elaboration only makes a Match with
		// the arms true and false, in that order. Each row is one function whose
		// body is `match scrutinee { arms }`, and every arm answers a different
		// number. A literal arm wins over a wildcard that comes first, and the
		// wildcard is for the value that no other arm fits.
		const (
			t1, f2, w9 = 1, 2, 9
		)
		type arm struct {
			pat  core.PatKind
			body int64
		}
		rows := []struct {
			name string
			scr  bool
			arms []arm
			want int64
		}{
			{"true_arm", true, []arm{{core.PatTrue, t1}, {core.PatFalse, f2}}, t1},
			{"false_arm", false, []arm{{core.PatTrue, t1}, {core.PatFalse, f2}}, f2},
			{"reversed_true", true, []arm{{core.PatFalse, f2}, {core.PatTrue, t1}}, t1},
			{"reversed_false", false, []arm{{core.PatFalse, f2}, {core.PatTrue, t1}}, f2},
			{"wildcard_after_true_gets_false", false, []arm{{core.PatTrue, t1}, {core.PatWildcard, w9}}, w9},
			{"wildcard_after_true_leaves_true", true, []arm{{core.PatTrue, t1}, {core.PatWildcard, w9}}, t1},
			{"wildcard_after_false_gets_true", true, []arm{{core.PatFalse, f2}, {core.PatWildcard, w9}}, w9},
			{"wildcard_after_false_leaves_false", false, []arm{{core.PatFalse, f2}, {core.PatWildcard, w9}}, f2},
			{"wildcard_alone_for_true", true, []arm{{core.PatWildcard, w9}}, w9},
			{"wildcard_alone_for_false", false, []arm{{core.PatWildcard, w9}}, w9},
			{"literal_wins_over_earlier_wildcard", true, []arm{{core.PatWildcard, w9}, {core.PatTrue, t1}}, t1},
			{"first_of_two_wildcards", false, []arm{{core.PatWildcard, w9}, {core.PatWildcard, 8}}, w9},
			{"first_of_two_equal_arms", true, []arm{{core.PatTrue, t1}, {core.PatTrue, 7}}, t1},
		}
		p := &core.Program{}
		b := core.Builder{P: p}
		sp := base.Span{File: 1}
		funs := make([]core.FunID, len(rows))
		for i, row := range rows {
			arms := make([]core.Arm, len(row.arms))
			for j, a := range row.arms {
				arms[j] = core.Arm{Pat: a.pat, Body: b.LitInt(sp, a.body)}
			}
			body := b.Match(sp, b.LitBool(sp, row.scr), arms)
			funs[i] = p.Funs.Push(core.Fun{Body: body, HasBody: true})
		}
		for i, row := range rows {
			var out, errOut bytes.Buffer
			m := interp.New(p, new(base.SourceManager), &out, &errOut)
			got, ok := m.Call(funs[i], nil)
			if !ok || got != (interp.IntV{V: row.want}) {
				t.Errorf("%s: Call = (%#v, %v), want (IntV{%d}, true)", row.name, got, ok, row.want)
			}
		}
	})

	t.Run("match_without_a_fitting_arm_panics", func(t *testing.T) {
		// Lint rejects a Match that has neither both literals nor a wildcard, so
		// no program that passed Lint gets here; the machine says so instead of
		// going on with no arm. Made by hand: only the arm true, and the value false.
		p := &core.Program{}
		b := core.Builder{P: p}
		sp := base.Span{File: 1}
		arms := []core.Arm{{Pat: core.PatTrue, Body: b.LitInt(sp, 1)}}
		f := p.Funs.Push(core.Fun{Body: b.Match(sp, b.LitBool(sp, false), arms), HasBody: true})
		got := panicText(func() {
			interp.New(p, new(base.SourceManager), new(bytes.Buffer), new(bytes.Buffer)).Call(f, nil)
		})
		if want := "interp: no arm of the match fits the value"; got != want {
			t.Errorf("Call panicked with %#v, want the string %q", got, want)
		}
	})

	t.Run("unknown_kinds_are_unreachable", func(t *testing.T) {
		// An ExprKind and a PatKind that the machine has no case for, which only
		// a Program made by hand can hold: each switch ends in panic("unreachable").
		p := &core.Program{}
		b := core.Builder{P: p}
		sp := base.Span{File: 1}
		badExpr := p.Funs.Push(core.Fun{Body: p.Exprs.Push(core.Expr{Kind: core.ExprKind(200), Span: sp}), HasBody: true})
		badPat := p.Funs.Push(core.Fun{HasBody: true, Body: b.Match(sp, b.LitBool(sp, true),
			[]core.Arm{{Pat: core.PatKind(77), Body: b.LitInt(sp, 1)}})})
		rows := []struct {
			name string
			fun  core.FunID
		}{
			{"expression_kind", badExpr},
			{"pattern_kind", badPat},
		}
		for _, row := range rows {
			got := panicText(func() {
				interp.New(p, new(base.SourceManager), new(bytes.Buffer), new(bytes.Buffer)).Call(row.fun, nil)
			})
			if got != "unreachable" {
				t.Errorf("%s: Call panicked with %#v, want the string \"unreachable\"", row.name, got)
			}
		}
	})

	t.Run("and_or_short_circuit", func(t *testing.T) {
		// The two rows with a right operand that would panic come first: a
		// machine that evaluated it ends the test at once, with the message of the
		// panic (call fails the test when the call reports one). The rows with boom
		// come after them because boom never returns and never overflows (its call
		// is a tail call), so a machine that evaluated it would not stop, and the
		// test would end by timeout.
		const boom = "fn boom() -> Bool { boom() }\n"
		rows := []struct {
			name, src string
			want      bool
		}{
			{"and_false_skips_a_panic", "fn f() -> Bool { false && 1 / 0 == 0 }", false},
			{"or_true_skips_a_panic", "fn f() -> Bool { true || 1 / 0 == 0 }", true},
			{"and_false_skips_the_right", boom + "fn f() -> Bool { false && boom() }", false},
			{"or_true_skips_the_right", boom + "fn f() -> Bool { true || boom() }", true},
			// When the left operand does not settle the answer, the right one is it.
			{"and_true_gives_the_right_false", "fn f() -> Bool { true && false }", false},
			{"and_true_gives_the_right_true", "fn f() -> Bool { true && true }", true},
			{"or_false_gives_the_right_false", "fn f() -> Bool { false || false }", false},
			{"or_false_gives_the_right_true", "fn f() -> Bool { false || true }", true},
			{"and_false_false", "fn f() -> Bool { false && false }", false},
			{"or_true_true", "fn f() -> Bool { true || true }", true},
		}
		for _, row := range rows {
			_, got := call(t, row.src, "f")
			if got != (interp.BoolV{V: row.want}) {
				t.Errorf("%s: Call(f) = %#v, want BoolV{%v}", row.name, got, row.want)
			}
		}
	})

	t.Run("tail_call_constant_depth", func(t *testing.T) {
		const src = "fn count(n: Int) -> Int { if n == 0 { 0 } else { count(n - 1) } }"
		m, got := call(t, src, "count", interp.IntV{V: 1_000_000})
		if got != (interp.IntV{V: 0}) {
			t.Errorf("Call(count, 1000000) = %#v, want IntV{0}", got)
		}
		// The first call of count comes from the entry, which has no retFrame to
		// reuse, so it pushes one; every call after it is in tail position and
		// reuses that one. The deepest the stack gets is three frames: the
		// retFrame, and above it the two that the evaluation of one iteration
		// needs (the matchFrame and a primFrame of `n == 0`, or the argFrame and
		// a primFrame of `n - 1`).
		if s := m.Stats(); s.MaxDepth != 3 {
			t.Errorf("Stats().MaxDepth = %d, want 3", s.MaxDepth)
		}
	})

	t.Run("tail_loop_does_not_grow_envs", func(t *testing.T) {
		const src = "fn count(n: Int) -> Int { if n == 0 { 0 } else { count(n - 1) } }"
		m, got := call(t, src, "count", interp.IntV{V: 1_000_000})
		if got != (interp.IntV{V: 0}) {
			t.Errorf("Call(count, 1000000) = %#v, want IntV{0}", got)
		}
		// The environment of the entry and the one of count, which each tail call
		// replaces with its own: two for a million iterations.
		if s := m.Stats(); s.MaxEnvs != 2 {
			t.Errorf("Stats().MaxEnvs = %d, want 2", s.MaxEnvs)
		}

		// The same from `main`, which is what `ki run` does: the environments
		// are main's and count's.
		m, h, _, stderr := runMainOn(t, src+"\nfn main() { let x = count(1000000) }")
		if h != (interp.Halt{Code: 0}) || stderr != "" {
			t.Errorf("RunMain() = %+v with stderr %q, want Halt{0} and nothing", h, stderr)
		}
		if s := m.Stats(); s.MaxEnvs != 2 {
			t.Errorf("Stats().MaxEnvs from main = %d, want 2", s.MaxEnvs)
		}
	})

	t.Run("tail_call_between_functions", func(t *testing.T) {
		// Two functions that tail-call each other and have environments of
		// different sizes (a has four slots, b two), each reading its own
		// parameters after the other's tail call has replaced the environment.
		const src = `
fn a(n: Int, acc: Int) -> Int {
  let t = acc + 1
  let u = t
  if n == 0 { u } else { b(n - 1, u) }
}
fn b(n: Int, acc: Int) -> Int {
  if n == 0 { acc } else { a(n - 1, acc + 10) }
}
`
		// a(n, x) adds 1 and goes on with b(n-1, x+1); b(n, x) goes on with
		// a(n-1, x+10). By hand, a(4, 0): 1, then b(3, 1), a(2, 11), 12, b(1, 12),
		// a(0, 22), 23. A million is even, so a runs at every even n, and each pair
		// of calls adds 11: 500 000 pairs reach a(0, 5500000), which answers 5500001.
		_, got := call(t, src, "a", interp.IntV{V: 4}, interp.IntV{V: 0})
		if got != (interp.IntV{V: 23}) {
			t.Errorf("Call(a, 4, 0) = %#v, want IntV{23}", got)
		}
		m, got := call(t, src, "a", interp.IntV{V: 1_000_000}, interp.IntV{V: 0})
		if got != (interp.IntV{V: 5_500_001}) {
			t.Errorf("Call(a, 1000000, 0) = %#v, want IntV{5500001}", got)
		}
		if s := m.Stats(); s.MaxDepth != 3 || s.MaxEnvs != 2 {
			t.Errorf("Stats() = %+v, want MaxDepth 3 and MaxEnvs 2", s)
		}
	})

	t.Run("return_frees_environment", func(t *testing.T) {
		// g has a variable on purpose. If the environment of the last g stayed in
		// envs, main would write its x there: with no slot in it, that write would
		// be an index out of range, a Go panic that ends the whole test binary
		// before the count below can fail. With a slot, the count fails alone.
		m, h, stdout, stderr := runMainOn(t, "fn g() -> Int { let a = 1\n a }\nfn f() -> Int { g() + g() + g() }\nfn main() { let x = f() }")
		if h != (interp.Halt{Code: 0}) || stdout != "" || stderr != "" {
			t.Errorf("RunMain() = %+v, stdout %q, stderr %q, want Halt{0} and nothing written", h, stdout, stderr)
		}
		// main, f, and one g at a time: each g is gone from envs before the next
		// one starts. Without that, the three g would pile up: five in all.
		if s := m.Stats(); s.MaxEnvs != 3 {
			t.Errorf("Stats().MaxEnvs = %d, want 3", s.MaxEnvs)
		}
	})

	t.Run("deep_non_tail_recursion", func(t *testing.T) {
		// The default limit: an empty setting is not a number, so it is the default
		// whatever the environment this test runs in says.
		t.Setenv("KI_STACK_MB", "")
		const src = "fn sumTo(n: Int) -> Int { if n == 0 { 0 } else { n + sumTo(n - 1) } }"
		m, got := call(t, src, "sumTo", interp.IntV{V: 200_000})
		if got != (interp.IntV{V: 20000100000}) {
			t.Errorf("Call(sumTo, 200000) = %#v, want IntV{20000100000}", got)
		}
		// The entry's environment and one for each of the 200 000 calls of sumTo
		// that are alive at the deepest point.
		if s := m.Stats(); s.MaxEnvs != 200_001 {
			t.Errorf("Stats().MaxEnvs = %d, want 200001", s.MaxEnvs)
		}
	})

	t.Run("stack_overflow_exits_103", func(t *testing.T) {
		t.Setenv("KI_STACK_MB", "1")
		m, h, stdout, stderr := runMainOn(t, "fn f(n: Int) -> Int { 1 + f(n + 1) }\nfn main() { let x = f(0) }")
		if h != (interp.Halt{Code: 103}) {
			t.Errorf("RunMain() = %+v, want Halt{Code: 103}", h)
		}
		if want := "panic: stack overflow at test.ki:1:27\n"; stderr != want {
			t.Errorf("stderr = %q, want %q", stderr, want)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want it empty", stdout)
		}
		// The limit decides whether a call is allowed, and it is checked when the
		// call is about to push its retFrame: here with 16385 frames on the stack
		// (main's letFrame, then a primFrame and a retFrame for each level of f,
		// 1 + 2k, and 16385 is the first of those that is at least 1<<14). The
		// arguments of that call had already pushed two frames more, its argFrame
		// and the primFrame of `n + 1`, so the stack got to 16387 and not to 1<<14.
		if s := m.Stats(); s.MaxDepth != 1<<14+3 {
			t.Errorf("Stats().MaxDepth = %d, want %d", s.MaxDepth, 1<<14+3)
		}

		// The environments have a limit of their own: a MiB holds 1<<16 slots of 16
		// bytes, and a call that would take them past that overflows, long before
		// the frames do (a level of f keeps two frames, so the 1<<14 frames of a MiB
		// would allow 8192 levels). The entry's environment is the first, and a call
		// is refused if its own environment does not fit, so MaxEnvs is the largest
		// e with e*size <= the limit. With 1024 slots the last environment fills the
		// 65536 slots exactly and is allowed, which a check with >= would refuse; and
		// with 1001 slots a check that left out the environment of the callee would
		// allow a 66th.
		for _, row := range []struct {
			lets    int
			mb      string
			wantEnv int
		}{
			{1000, "1", 65},  // 1001 slots: 65*1001 = 65065, 66*1001 = 66066
			{1023, "1", 64},  // 1024 slots: 64*1024 = 65536
			{1023, "2", 128}, // 1024 slots: 128*1024 = 131072, the limit of 2 MiB
		} {
			t.Setenv("KI_STACK_MB", row.mb)
			src := "fn f(n: Int) -> Int {\n" + manyLets(row.lets) + "  1 + f(n + 1)\n}"
			c := testsupport.Compile(t, src)
			m, _, errOut := machineOf(c)
			v, ok := m.Call(testsupport.FunNamed(t, c, "f"), []interp.Value{interp.IntV{V: 0}})
			if v != nil || ok {
				t.Errorf("%d lets, %s MiB: Call(f, 0) = (%#v, %v), want (nil, false)", row.lets, row.mb, v, ok)
			}
			if want := fmt.Sprintf("panic: stack overflow at test.ki:%d:7\n", row.lets+2); errOut.String() != want {
				t.Errorf("%d lets, %s MiB: stderr = %q, want %q", row.lets, row.mb, errOut.String(), want)
			}
			if s := m.Stats(); s.MaxEnvs != row.wantEnv {
				t.Errorf("%d lets, %s MiB: Stats().MaxEnvs = %d, want %d", row.lets, row.mb, s.MaxEnvs, row.wantEnv)
			}
		}

		// What returned no longer counts. Each call of leaf takes 1024 slots and
		// gives them back, so 100 of them, which would be 102400 slots if they
		// added up, fit in the 65536 of a MiB.
		t.Setenv("KI_STACK_MB", "1")
		returns := "fn leaf(n: Int) -> Int {\n" + manyLets(1023) + "  n\n}\n" +
			"fn f(n: Int) -> Int { if n == 0 { 0 } else { 1 + leaf(n) - n + f(n - 1) } }"
		if _, got := call(t, returns, "f", interp.IntV{V: 100}); got != (interp.IntV{V: 100}) {
			t.Errorf("Call(f, 100) of the calls that return = %#v, want IntV{100}", got)
		}

		// Each Call starts from an empty machine, the count of slots included: after
		// an overflow, a call that fits is allowed on the same machine.
		c := testsupport.Compile(t, "fn f(n: Int) -> Int {\n"+manyLets(1023)+"  if n == 0 { 0 } else { 1 + f(n - 1) }\n}")
		again, _, _ := machineOf(c)
		f := testsupport.FunNamed(t, c, "f")
		if v, ok := again.Call(f, []interp.Value{interp.IntV{V: 1000}}); v != nil || ok {
			t.Errorf("Call(f, 1000) = (%#v, %v), want (nil, false)", v, ok)
		}
		if v, ok := again.Call(f, []interp.Value{interp.IntV{V: 3}}); v != (interp.IntV{V: 3}) || !ok {
			t.Errorf("Call(f, 3) after the overflow = (%#v, %v), want (IntV{3}, true)", v, ok)
		}
	})

	t.Run("stack_limit_counts_frames", func(t *testing.T) {
		// Each level of f keeps three frames on the stack while the next one runs:
		// the two primFrames that wait for the call, and the retFrame of the call.
		// The first call comes from the entry with two frames already waiting, so
		// the call number j finds 2 + 3j frames; it overflows at the first j for
		// which that is at least the limit. The environments alive then are the
		// entry's and the j calls that were allowed, 1 + j.
		const src = "fn f(n: Int) -> Int { 1 + (1 + f(n + 1)) }"
		rows := []struct {
			mb      string
			wantEnv int // 1 + j
		}{
			// 16384 frames: 2 + 3j >= 16384 first holds for j = 5461 (16385).
			{"1", 5462},
			// 32768 frames: 2 + 3j >= 32768 holds for j = 10922, with 32768 exactly,
			// so a limit that was checked with `>` would let one more call through.
			{"2", 10923},
		}
		for _, row := range rows {
			t.Setenv("KI_STACK_MB", row.mb)
			c := testsupport.Compile(t, src)
			m, _, errOut := machineOf(c)
			v, ok := m.Call(testsupport.FunNamed(t, c, "f"), []interp.Value{interp.IntV{V: 0}})
			if v != nil || ok {
				t.Errorf("KI_STACK_MB=%s: Call(f, 0) = (%#v, %v), want (nil, false)", row.mb, v, ok)
			}
			if want := "panic: stack overflow at test.ki:1:32\n"; errOut.String() != want {
				t.Errorf("KI_STACK_MB=%s: stderr = %q, want %q", row.mb, errOut.String(), want)
			}
			if s := m.Stats(); s.MaxEnvs != row.wantEnv {
				t.Errorf("KI_STACK_MB=%s: Stats().MaxEnvs = %d, want %d", row.mb, s.MaxEnvs, row.wantEnv)
			}
		}
	})

	t.Run("tail_call_at_the_limit", func(t *testing.T) {
		// A tail call pushes no frame, so it is allowed whatever the size of the
		// stack. Each level of f keeps two frames (a primFrame and a retFrame), and
		// the call of base at the last level is in tail position: with f(8192) the
		// stack holds 16384 frames, exactly the limit, when base is called. The
		// call of f(0) before it found 16383 and was allowed. With f(8193) the
		// call of f(0) finds 16385 and overflows.
		t.Setenv("KI_STACK_MB", "1")
		const src = "fn base() -> Int { 7 }\nfn f(n: Int) -> Int { if n == 0 { base() } else { 1 + f(n - 1) } }"
		c := testsupport.Compile(t, src)
		f := testsupport.FunNamed(t, c, "f")

		m, _, errOut := machineOf(c)
		if v, ok := m.Call(f, []interp.Value{interp.IntV{V: 8192}}); v != (interp.IntV{V: 8199}) || !ok {
			t.Errorf("Call(f, 8192) = (%#v, %v), want (IntV{8199}, true); stderr %q", v, ok, errOut.String())
		}

		m, _, errOut = machineOf(c)
		v, ok := m.Call(f, []interp.Value{interp.IntV{V: 8193}})
		if v != nil || ok {
			t.Errorf("Call(f, 8193) = (%#v, %v), want (nil, false)", v, ok)
		}
		if want := "panic: stack overflow at test.ki:2:55\n"; errOut.String() != want {
			t.Errorf("stderr of Call(f, 8193) = %q, want %q", errOut.String(), want)
		}

		// A tail call replaces the environment, so the count of slots changes by the
		// difference. spin loops over its own 1024 slots: if they added up, the call
		// of one at the end would find 102400 of the 65536 of a MiB.
		const one = "fn one() -> Int { 1 }\n"
		spin := one + "fn spin(n: Int) -> Int {\n" + manyLets(1023) + "  if n == 0 { 1 + one() } else { spin(n - 1) }\n}"
		if _, got := call(t, spin, "spin", interp.IntV{V: 100}); got != (interp.IntV{V: 2}) {
			t.Errorf("Call(spin, 100) = %#v, want IntV{2}", got)
		}
		// And small, of one slot, grows to the 1024 of big by a tail call, which
		// the next call that pushes a frame has to see: it finds 1024 slots for
		// each level, and at the 64th, with 65536, one more does not fit. If only
		// the slot of small counted, the 8192 levels of the frames would come first.
		grow := "fn small(n: Int) -> Int { big(n) }\nfn big(n: Int) -> Int {\n" + manyLets(1023) + "  1 + small(n + 1)\n}"
		c = testsupport.Compile(t, grow)
		m, _, errOut = machineOf(c)
		if v, ok := m.Call(testsupport.FunNamed(t, c, "big"), []interp.Value{interp.IntV{V: 0}}); v != nil || ok {
			t.Errorf("Call(big, 0) = (%#v, %v), want (nil, false)", v, ok)
		}
		if s := m.Stats(); s.MaxEnvs != 64 {
			t.Errorf("Stats().MaxEnvs of big and small = %d, want 64", s.MaxEnvs)
		}
	})

	t.Run("fib_20", func(t *testing.T) {
		const src = "fn fib(n: Int) -> Int { if n < 2 { n } else { fib(n - 1) + fib(n - 2) } }"
		rows := []struct{ n, want int64 }{
			{0, 0}, {1, 1}, {2, 1}, {3, 2}, {10, 55}, {20, 6765},
		}
		for _, row := range rows {
			_, got := call(t, src, "fib", interp.IntV{V: row.n})
			if got != (interp.IntV{V: row.want}) {
				t.Errorf("Call(fib, %d) = %#v, want IntV{%d}", row.n, got, row.want)
			}
		}
	})
}
