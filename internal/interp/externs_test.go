package interp_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/core/coretest"
	"github.com/aldezex/kilang/internal/interp"
	"github.com/aldezex/kilang/internal/testsupport"
)

// heldOut is a stdout that keeps what it is given until it is flushed, as a
// kirt/out Writer does, and then sends it to log. The test makes stderr write to
// the same log, so that the log shows the order in which the two reached it.
type heldOut struct {
	log  *strings.Builder
	held strings.Builder
}

func (h *heldOut) Write(p []byte) (int, error) { return h.held.Write(p) }

func (h *heldOut) Flush() error {
	h.log.WriteString(h.held.String())
	h.held.Reset()
	return nil
}

func TestExterns(t *testing.T) {
	t.Run("print_int_writes_decimal_and_newline", func(t *testing.T) {
		rows := []struct {
			name, src, want string
		}{
			{"negative", "fn main() { print(-5) }", "-5\n"},
			{"zero", "fn main() { print(0) }", "0\n"},
			{"positive", "fn main() { print(42) }", "42\n"},
			{"max_int", "fn main() { print(9223372036854775807) }", "9223372036854775807\n"},
			{"min_int", "fn main() { print(-9223372036854775807 - 1) }", "-9223372036854775808\n"},
			// The argument is evaluated before the call.
			{"argument_is_an_expression", "fn main() { print(1 + 2 * 3) }", "7\n"},
			// Each call is one line, in the order of the calls.
			{"two_lines_in_order", "fn main() {\n  print(1)\n  print(-20)\n}", "1\n-20\n"},
			// print is a function like another: it can be called from a function
			// of the program, and the call can be the last thing that function
			// does or not.
			{"through_a_function", "fn say(x: Int) { print(x) }\nfn main() {\n  say(3)\n  print(4)\n  say(5)\n}", "3\n4\n5\n"},
			{"result_is_bound", "fn main() {\n  let u = print(1)\n  print(2)\n}", "1\n2\n"},
		}
		for _, row := range rows {
			h, stdout, stderr := runMain(t, row.src)
			if h != (interp.Halt{Code: 0}) || stdout != row.want || stderr != "" {
				t.Errorf("%s: RunMain() = %+v, stdout %q, stderr %q, want Halt{0}, %q and nothing on stderr", row.name, h, stdout, stderr, row.want)
			}
		}
	})

	t.Run("print_returns_unit_and_adds_no_environment", func(t *testing.T) {
		// Calling print runs no body: it makes no environment, so a program that
		// only prints has the one environment of its own `main`, however many
		// lines it prints and whether or not the call is the last thing it does.
		const src = "fn main() {\n  print(1)\n  print(2)\n  print(3)\n}"
		m, h, _, _ := runMainOn(t, src)
		if h != (interp.Halt{Code: 0}) {
			t.Fatalf("RunMain() = %+v, want Halt{0}", h)
		}
		if got := m.Stats().MaxEnvs; got != 1 {
			t.Errorf("Stats().MaxEnvs = %d, want 1", got)
		}
		// The value of the call is the unit value, and not nil.
		_, got := call(t, "fn f() { print(1) }", "f")
		if got != (interp.UnitV{}) {
			t.Errorf("Call(f) of a function whose last call is print = %#v, want UnitV{}", got)
		}
	})

	t.Run("print_inside_a_tail_loop", func(t *testing.T) {
		// A call of print between the tail calls of a loop must not disturb them:
		// the loop still runs in the environment of its first call and one more.
		const src = "fn count(n: Int) {\n  if n == 0 { () } else {\n    print(n)\n    count(n - 1)\n  }\n}\nfn main() { count(1000) }"
		m, h, stdout, stderr := runMainOn(t, src)
		var want strings.Builder
		for n := 1000; n >= 1; n-- {
			fmt.Fprintf(&want, "%d\n", n)
		}
		if h != (interp.Halt{Code: 0}) || stdout != want.String() || stderr != "" {
			t.Errorf("RunMain() = %+v, %d bytes on stdout, stderr %q, want Halt{0}, the numbers 1000 down to 1 and nothing on stderr", h, len(stdout), stderr)
		}
		if got := m.Stats().MaxEnvs; got != 2 {
			t.Errorf("Stats().MaxEnvs = %d, want 2 (main and count)", got)
		}
		// The retFrame of count and, above it, the two frames of whatever is being
		// evaluated: a letFrame and an argFrame for print(n), or a matchFrame and a
		// primFrame for n == 0. A frame that print left behind would add one more
		// with every turn of the loop, and the count would be about 1000, not 3.
		if got := m.Stats().MaxDepth; got != 3 {
			t.Errorf("Stats().MaxDepth = %d, want 3 (the retFrame of count and two frames above it)", got)
		}
	})

	t.Run("unknown_extern_is_unreachable", func(t *testing.T) {
		// The control: with a name that the table has, the same program is valid
		// Core and runs, so that a name that is not in the table is the only
		// thing that the panics below can be about.
		if problems := core.Lint(coretest.ExternProgram("ki_print_int"), base.NewInterner()); len(problems) != 0 {
			t.Errorf("core.Lint of the control program = %q, want no violations", problems)
		}
		var out, errOut bytes.Buffer
		if h := interp.New(coretest.ExternProgram("ki_print_int"), new(base.SourceManager), &out, &errOut).RunMain(); h != (interp.Halt{Code: 0}) || out.String() != "9\n" || errOut.String() != "" {
			t.Fatalf("the control program with the extern ki_print_int: RunMain() = %+v, stdout %q, stderr %q, want Halt{0}, \"9\\n\" and nothing on stderr", h, out.String(), errOut.String())
		}
		// A name that is not in the table, even one that is spelled almost like
		// one that is, is a Go panic that says which.
		for _, name := range []string{"nope", "", "KI_PRINT_INT", "ki_print_int ", "ki_print"} {
			out.Reset()
			errOut.Reset()
			got := panicText(func() {
				interp.New(coretest.ExternProgram(name), new(base.SourceManager), &out, &errOut).RunMain()
			})
			if want := "unknown extern " + name; got != want {
				t.Errorf("extern %q: RunMain panicked with %#v, want the string %q", name, got, want)
			}
			if out.Len() != 0 || errOut.Len() != 0 {
				t.Errorf("extern %q: the program wrote %q to stdout and %q to stderr, want nothing on either", name, out.String(), errOut.String())
			}
		}
	})

	t.Run("a_panic_empties_stdout_before_its_message", func(t *testing.T) {
		// The second source overflows the stack of 1 MiB that KI_STACK_MB=1 gives
		// it, at the call of f in its own body.
		t.Setenv("KI_STACK_MB", "1")
		rows := []struct {
			name, src, want string
			code            int
		}{
			{"panic", "fn main() {\n  print(1)\n  print(2 / 0)\n}", "1\npanic: division by zero at test.ki:3:11\n", 101},
			{"stack_overflow", "fn f(n: Int) -> Int { 1 + f(n + 1) }\nfn main() {\n  print(1)\n  print(f(0))\n}", "1\npanic: stack overflow at test.ki:1:27\n", 103},
			// The control: a program that does not end in a panic leaves what it
			// printed in the writer, which only the end of the process flushes.
			{"no_panic_flushes_nothing", "fn main() {\n  print(1)\n}", "", 0},
		}
		for _, row := range rows {
			var log strings.Builder
			c := testsupport.Compile(t, row.src)
			h := interp.New(c.Core, &c.Session.Sources, &heldOut{log: &log}, &log).RunMain()
			if h != (interp.Halt{Code: row.code}) || log.String() != row.want {
				t.Errorf("%s: RunMain() = %+v, the log holds %q, want Halt{%d} and %q", row.name, h, log.String(), row.code, row.want)
			}
		}
	})
}
