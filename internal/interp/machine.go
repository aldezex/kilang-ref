// Package interp is the oracle: the interpreter that runs Core and that every
// other backend is compared with (spec 9.1). It is a CEK machine, named after
// its three parts: the Control (what it is about to do), the Environment (the
// variables of the function that is running) and the Kontinuation (what to do
// with the value when it is ready). All three are data in the Machine, and
// running it is a loop over them (Machine.run), not a recursion over the tree.
//
// So far it evaluates literals, variables, `let` and the primitives, which is
// where a program can panic: an overflow or a division by zero ends it with
// Halt{Code: 101}. The rest of Core (calls and `match`) arrives in the next
// session of this chapter.
package interp

import (
	"fmt"
	"io"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
)

// frame is one frame of the continuation: what the machine must do with the
// next value it computes. It is a sealed interface like Value, and applyFrame
// has the type switch over its kinds. So far there are two, letFrame and
// primFrame; the others come with the expressions that need them.
type frame interface{ frame() }

// letFrame waits for the value of the Init of a Let. When it arrives, the value
// is written in the slot of the variable v and the machine goes on with body.
type letFrame struct {
	v    core.VarID
	body core.ExprID
}

func (letFrame) frame() {}

// primFrame waits for the value of an argument of the PrimCall expression call.
// The arguments are evaluated from left to right. While the first is being
// evaluated first is nil; when its value arrives, a binary primitive pushes a
// new primFrame that holds it in first, and evaluates the second argument. When
// the last value arrives, the primitive runs.
type primFrame struct {
	call  core.ExprID
	first Value
}

func (primFrame) frame() {}

// Halt is the state in which the machine ends. Code is the exit code of the
// program: 0 when `main` returns, and 101 when the program panics. A Ki panic is
// never a Go panic: it is a Halt.
type Halt struct{ Code int }

// Stats counts what the machine has done, over all the calls made on it.
type Stats struct {
	// Steps is the number of transitions: each evaluation of an expression and
	// each application of a frame is one.
	Steps int
	// MaxDepth is the most frames the stack has held at once.
	MaxDepth int
	// MaxEnvs is the largest len(envs) seen: the most environments that were
	// alive at once, one for each function being run.
	MaxEnvs int
}

// The limit of the stack is KI_STACK_MB mebibytes (spec 8.10), counted in frames
// and not in bytes: a frame is assumed to take 64 bytes, so a mebibyte holds
// 1<<14 of them. The machine does not check the limit yet, and does not read
// KI_STACK_MB: New uses the default.
const (
	defaultStackMB = 256
	framesPerMB    = 1 << 14
)

// Machine is a CEK machine over one Program. Make it with New. A Machine is not
// safe for use by several goroutines.
type Machine struct {
	prog *core.Program
	// sm is where the spans of Core are turned into positions, for the messages
	// of the program's panics.
	sm *base.SourceManager
	// out and err are the stdout and the stderr of the program.
	out, err io.Writer

	// envs are the environments of the functions being run, the one of the
	// function that runs now being the last. Each has FrameSize slots. The code
	// indexes envs and never keeps a pointer to one of its elements (&envs[i]):
	// an append to envs may copy the slice headers it holds to a bigger array
	// and leave that pointer on the old one. The slots do not move with them,
	// because each header points to its own array.
	envs [][]Value
	// stack is the continuation: the frames, the one to apply next being the last.
	stack []frame
	stats Stats
	// maxFrames is the most frames the stack may hold, once the machine checks it.
	maxFrames int
	// halt is nil while the program runs, and the state it ended in once it has
	// panicked. run stops as soon as it is set.
	halt *Halt

	// The control is either an expression to evaluate (returning is false and
	// expr is the expression) or a value that is ready (returning is true and
	// val is the value). It lives here, and not in the variables of the loop,
	// because applyFrame has to set it.
	expr      core.ExprID
	val       Value
	returning bool
}

// New returns a Machine for p. sm resolves the spans of p, and out and err are
// the stdout and the stderr of the program. The machine writes the messages of
// the program's panics to err, using sm for their positions; it does not write
// to out yet, because nothing that it runs so far prints.
func New(p *core.Program, sm *base.SourceManager, out, err io.Writer) *Machine {
	return &Machine{prog: p, sm: sm, out: out, err: err, maxFrames: defaultStackMB * framesPerMB}
}

// Stats returns the counts of what the machine has done since New.
func (m *Machine) Stats() Stats { return m.stats }

// RunMain runs the function `main` of the program and returns the state the
// machine ends in: Halt{Code: 0} when `main` returns, and the Halt of the panic
// when the program panics (its message is then already on err). RunMain itself
// panics if the program has no `main`, so whoever runs a program has to check
// that first.
func (m *Machine) RunMain() Halt {
	if !m.prog.HasMain {
		panic("interp: RunMain on a program without a main function")
	}
	if _, ok := m.Call(m.prog.Main, nil); !ok {
		return *m.halt
	}
	return Halt{Code: 0}
}

// Call runs the function f with args, one for each of its parameters, and
// returns its value. The second result is false if the program panicked, and
// then the message is already on err and the value is nil.
//
// Each call starts from an empty machine, so a Call never sees what an earlier
// one left, not even the frames of a program that panicked; only Stats go on
// counting. Call panics, with a message that says why, if f takes another number
// of parameters than len(args) or has no body (a function that the runtime
// provides): those are mistakes of the caller, and not something a Ki program
// can do.
func (m *Machine) Call(f core.FunID, args []Value) (Value, bool) {
	fun := m.prog.Funs.Get(f)
	if len(args) != len(fun.Params) {
		panic(fmt.Sprintf("interp: function %d takes %d arguments, but Call got %d", f, len(fun.Params), len(args)))
	}
	if !fun.HasBody {
		panic(fmt.Sprintf("interp: Call of function %d, which has no body", f))
	}
	env := make([]Value, fun.FrameSize)
	for i, p := range fun.Params {
		env[m.prog.Vars.Get(p).Slot] = args[i]
	}
	m.envs = append(m.envs[:0], env)
	m.stack = m.stack[:0]
	m.halt = nil
	m.stats.MaxEnvs = max(m.stats.MaxEnvs, len(m.envs))
	m.eval(fun.Body)
	return m.run()
}

// run steps the machine until the control is a value and no frame is left, and
// returns that value and true. If the program panics first, it stops there and
// returns nil and false. It is a loop and not a recursion: how deep the program
// is shows in the length of m.stack, not in the stack of Go.
func (m *Machine) run() (Value, bool) {
	for m.halt == nil {
		switch n := len(m.stack); {
		case !m.returning:
			m.stats.Steps++
			m.step(m.expr)
		case n > 0:
			m.stats.Steps++
			f := m.stack[n-1]
			m.stack = m.stack[:n-1]
			m.applyFrame(f, m.val)
		default:
			// The value is ready and no frame waits for it: the run is over.
			return m.val, true
		}
	}
	return nil, false
}

// eval makes the expression e the control: the next step evaluates it.
func (m *Machine) eval(e core.ExprID) {
	m.expr, m.val, m.returning = e, nil, false
}

// ret makes the value v the control: the next step returns it to the top frame.
func (m *Machine) ret(v Value) {
	m.expr, m.val, m.returning = 0, v, true
}

// push adds f to the continuation.
func (m *Machine) push(f frame) {
	m.stack = append(m.stack, f)
	m.stats.MaxDepth = max(m.stats.MaxDepth, len(m.stack))
}

// env returns the environment of the function that is running. The slice shares
// its elements with the one in envs, so writing to it writes there.
func (m *Machine) env() []Value { return m.envs[len(m.envs)-1] }

// step evaluates the expression id: a literal or a variable returns its value,
// a Let evaluates its Init under a letFrame that remembers what to do next, and
// a PrimCall starts on its first argument under a primFrame.
func (m *Machine) step(id core.ExprID) {
	x := m.prog.Exprs.Get(id)
	switch x.Kind {
	case core.LitInt:
		m.ret(IntV{V: x.Int})
	case core.LitBool:
		m.ret(BoolV{V: x.Bool})
	case core.LitUnit:
		m.ret(UnitV{})
	case core.VarRef:
		// A Core that Lint accepted binds a variable before it reads it, so the
		// slot has been written.
		m.ret(m.env()[m.prog.Vars.Get(x.Var).Slot])
	case core.Let:
		m.push(letFrame{v: x.Var, body: x.Body})
		m.eval(x.Init)
	case core.PrimCall:
		// Every primitive has one or two arguments (Prim.Arity), and Lint checks
		// that a PrimCall has that many.
		m.push(primFrame{call: id})
		m.eval(m.prog.Args(x.Args)[0])
	default:
		// FunRef, Call and Match are reachable from a program, but the machine
		// does not run them yet.
		panic(fmt.Sprintf("interp: cannot evaluate an expression of kind %d yet", x.Kind))
	}
}

// applyFrame returns the value v to the frame f, which the machine has just
// taken off the stack. For a letFrame, v is written in the slot of the
// variable, and the body becomes the control: nothing is pushed, so a chain of
// Lets does not make the stack grow. For a primFrame, v is the value of an
// argument: either the second argument is evaluated next, or the primitive runs
// and its value is returned, or, if it fails, the program panics and the
// machine halts with the panic.
func (m *Machine) applyFrame(f frame, v Value) {
	switch f := f.(type) {
	case letFrame:
		m.env()[m.prog.Vars.Get(f.v).Slot] = v
		m.eval(f.body)
	case primFrame:
		x := m.prog.Exprs.Get(f.call)
		if f.first == nil && x.Prim.Arity() == 2 {
			m.push(primFrame{call: f.call, first: v})
			m.eval(m.prog.Args(x.Args)[1])
			return
		}
		// All the arguments have a value. A binary primitive has the first in
		// f.first and the second in v. A unary one has no f.first: its only
		// operand is v, and it reads a alone.
		a, b := f.first, v
		if a == nil {
			a, b = v, nil
		}
		r, msg := applyPrim(x.Prim, a, b)
		if msg != "" {
			h := m.panicAt(x.Span, msg)
			m.halt = &h
			return
		}
		m.ret(r)
	default:
		panic("unreachable")
	}
}
