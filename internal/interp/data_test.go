package interp_test

import (
	"testing"

	"github.com/aldezex/kilang/internal/interp"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here run records and tuples in the oracle: what the Con and the Proj
// that the elaboration makes do, and what Show writes for the values.

// shownCall compiles src, calls the function called name, which takes nothing,
// and returns the value it gives, written by Show.
func shownCall(t *testing.T, src, name string) string {
	t.Helper()
	c := testsupport.Compile(t, src)
	m, _, errOut := machineOf(c)
	v, ok := m.Call(testsupport.FunNamed(t, c, name), nil)
	if !ok {
		t.Fatalf("Call(%s) of %q reported a panic, stderr %q", name, src, errOut.String())
	}
	return interp.Show(v, c.Core, c.Session.Interner)
}

func TestDataRun(t *testing.T) {
	t.Run("records_and_tuples_have_their_values", func(t *testing.T) {
		// What each form builds, written as Show writes it: a literal in either order
		// of its fields, a pun, an update that takes none, one and all of its fields
		// from the base (and the base is the one that was read), a tuple of two and of
		// eight, and the elements that a read picks out, at every level.
		const src = `type Point { x: Float, y: Float }
type R { a: Int, b: Int, c: Int }
fn base() -> Point { Point { x: 1.0, y: 2.0 } }
fn lit() -> Point { Point { x: 1.0, y: 2.0 } }
fn swapped() -> Point { Point { y: 2.0, x: 1.0 } }
fn pun() -> Point {
  let x = 5.0
  let y = 6.0
  Point { x, y }
}
fn none() -> Point { Point { ..base() } }
fn one() -> Point { Point { x: 3.0, ..base() } }
fn other() -> Point { Point { y: 3.0, ..base() } }
fn all() -> Point { Point { x: 3.0, y: 4.0, ..base() } }
fn chained() -> Point { Point { x: 9.0, ..Point { y: 8.0, ..base() } } }
fn picks() -> R { R { b: 20, ..R { a: 1, b: 2, c: 3 } } }
fn pair() -> (Int, Bool) { (1, true) }
fn eight() -> (Int, Int, Int, Int, Int, Int, Int, Int) { (1, 2, 3, 4, 5, 6, 7, 8) }
fn nested() -> ((Int, Int), (Int, (Bool, Float))) { ((1, 2), (3, (true, 4.5))) }
fn readEight() -> Int { (1, 2, 3, 4, 5, 6, 7, 8).7 }
fn readFirst() -> Int { (10, 20).0 }
fn readSecond() -> Int { (10, 20).1 }
fn readDeep() -> Bool { ((1, 2), (3, (true, 4.5))).1.1.0 }
fn readInner() -> Int { ((1, 2), (3, 4)).0.1 }
fn readField() -> Float { (1, Point { x: 7.0, y: 8.0 }).1.y }
fn recordOfTuples() -> (Point, Point) { (Point { x: 1.0, y: 2.0 }, Point { x: 3.0, ..base() }) }
`
		for _, row := range []struct{ fn, want string }{
			{"lit", "Point { x: 1.0, y: 2.0 }"},
			{"swapped", "Point { x: 1.0, y: 2.0 }"},
			{"pun", "Point { x: 5.0, y: 6.0 }"},
			{"none", "Point { x: 1.0, y: 2.0 }"},
			{"one", "Point { x: 3.0, y: 2.0 }"},
			{"other", "Point { x: 1.0, y: 3.0 }"},
			{"all", "Point { x: 3.0, y: 4.0 }"},
			{"chained", "Point { x: 9.0, y: 8.0 }"},
			{"picks", "R { a: 1, b: 20, c: 3 }"},
			{"pair", "(1, true)"},
			{"eight", "(1, 2, 3, 4, 5, 6, 7, 8)"},
			{"nested", "((1, 2), (3, (true, 4.5)))"},
			{"readEight", "8"},
			{"readFirst", "10"},
			{"readSecond", "20"},
			{"readDeep", "true"},
			{"readInner", "2"},
			{"readField", "8.0"},
			{"recordOfTuples", "(Point { x: 1.0, y: 2.0 }, Point { x: 3.0, y: 2.0 })"},
		} {
			if got := shownCall(t, src, row.fn); got != row.want {
				t.Errorf("%s() = %s, want %s", row.fn, got, row.want)
			}
		}
	})

	t.Run("an_update_keeps_the_order_and_reads_the_base_once", func(t *testing.T) {
		// The values of a literal are evaluated in the order they are written, each
		// once, and the base after them, once, however many fields it gives: `mk`
		// writes 100 each time it runs. The oracle does not update in place: the base
		// is still what it was after an update that is built from it.
		const src = `type R { a: Int, b: Int, c: Int }
fn p(n: Int) -> Int {
  print(n)
  n
}
fn mk() -> R {
  print(100)
  R { a: 7, b: 8, c: 9 }
}
fn main() {
  let first = R { c: p(1), a: p(2), ..mk() }
  let second = R { ..mk() }
  let third = R { c: p(3), a: p(4), b: p(5) }
  let fourth = R { a: p(6), b: p(7), c: p(8) }
  let o = mk()
  let changed = R { a: 0, ..o }
  print(o.a)
  print(changed.a)
  print(changed.b)
}
`
		h, stdout, stderr := runMain(t, src)
		want := "1\n2\n100\n100\n3\n4\n5\n6\n7\n8\n100\n7\n0\n8\n"
		if h != (interp.Halt{Code: 0}) || stdout != want || stderr != "" {
			t.Errorf("RunMain() = %+v with stdout %q and stderr %q, want Halt{Code: 0}, stdout %q and nothing on stderr", h, stdout, stderr, want)
		}
	})

	t.Run("tuple_elements_run_left_to_right", func(t *testing.T) {
		// The elements of a tuple, like the arguments of a constructor, are evaluated
		// in order and each once, those inside an element before the next one, and a
		// read evaluates its target once.
		const src = `fn p(n: Int) -> Int {
  print(n)
  n
}
fn pair(a: Int, b: Int) -> (Int, Int) { (a, b) }
fn main() {
  let a = (p(1), (p(2), p(3)), p(4))
  let b = pair(p(5), p(6)).1
  let c = (p(7), p(8)).0
}
`
		h, stdout, stderr := runMain(t, src)
		if want := "1\n2\n3\n4\n5\n6\n7\n8\n"; h != (interp.Halt{Code: 0}) || stdout != want || stderr != "" {
			t.Errorf("RunMain() = %+v with stdout %q and stderr %q, want Halt{Code: 0}, stdout %q and nothing on stderr", h, stdout, stderr, want)
		}
	})

	t.Run("a_read_is_a_step_and_a_frame", func(t *testing.T) {
		// A Proj is the node, its target and one frame that picks the field:
		// `(1, 2).1` is the Proj, the Con, the `1`, its frame, the `2`, its frame and
		// the frame of the Proj, seven steps. A read of a read adds one node and one
		// frame each.
		for _, row := range []struct {
			src   string
			steps int
		}{
			{"fn f() -> Int { (1, 2).1 }", 7},
			{"fn f() -> Int { ((1, 2), 3).0.1 }", 13},
		} {
			m, _ := call(t, row.src, "f")
			if got := m.Stats().Steps; got != row.steps {
				t.Errorf("%q: %d steps, want %d", row.src, got, row.steps)
			}
		}
	})
}
