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
}
