// Package interp is the oracle: the interpreter that runs Core and that every
// other backend is compared with (spec 9.1). It is a CEK machine, named after
// its three parts: the Control (what it is about to do), the Environment (the
// variables of the function that is running) and the Kontinuation (what to do
// with the value when it is ready). All three are data in the Machine, and
// running it is a loop over them (Machine.run), not a recursion over the tree.
//
// It evaluates all of Core as Part I has it: literals, variables, `let`, the
// primitives, `match`, and calls of the program's functions. A program can end
// in two ways other than returning from `main`: a panic (an overflow or a
// division by zero), with Halt{Code: 101}, and a stack that has grown past its
// limit, with Halt{Code: 103}. A call that is the last thing its function does
// reuses that function's frame and environment instead of adding another pair,
// so a loop written as such calls does not make the stack grow. The functions
// that the runtime provides, such as `print`, have no body in Core: the machine
// runs them from the table in externs.go.
package interp

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
)

// frame is one frame of the continuation: what the machine must do with the
// next value it computes. It is a sealed interface like Value, and applyFrame
// has the type switch over its kinds: letFrame, primFrame, argFrame, matchFrame
// and retFrame.
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

// argFrame waits for the value of the callee or of an argument of the Call
// expression call. vals holds the values that have arrived, the callee's first
// and then the arguments' from left to right, and has room for all of them.
// When the callee's value has arrived and each argument's, the call is made.
type argFrame struct {
	call core.ExprID
	vals []Value
}

func (argFrame) frame() {}

// matchFrame waits for the value of the scrutinee of the Match expression
// match. When it arrives, the arm that fits becomes the control.
type matchFrame struct{ match core.ExprID }

func (matchFrame) frame() {}

// retFrame is where a function returns to. A call that is not in tail position
// pushes one, and it stays on the stack until the function returns; a call in
// tail position reuses the one that is there (see enter). env is the index in
// envs of the environment of the function that made the call: applying the frame
// cuts envs back to it, so that the environment of the function that returns is
// no longer in envs, and hands the value on to the frame below.
type retFrame struct{ env int }

func (retFrame) frame() {}

// The exit codes of a program that does not return normally (spec 8.10).
const (
	codePanic         = 101
	codeStackOverflow = 103
)

// Halt is the state in which the machine ends. Code is the exit code of the
// program: 0 when `main` returns, 101 when the program panics and 103 when its
// stack overflows. A Ki panic is never a Go panic: it is a Halt.
type Halt struct{ Code int }

// Stats counts what the machine has done, over all the calls made on it.
type Stats struct {
	// Steps is the number of transitions: each evaluation of an expression and
	// each application of a frame is one.
	Steps int
	// MaxDepth is the most frames the stack has held at once.
	MaxDepth int
	// MaxEnvs is the largest len(envs) seen: the most environments that were
	// alive at once, one for each function being run (a tail call replaces the
	// environment of the function that makes it, so it adds none).
	MaxEnvs int
}

// The limit of the stack is KI_STACK_MB mebibytes (spec 8.10), counted twice and
// in nominal sizes, not in the bytes that Go ends up using: that many mebibytes
// of frames, at 64 bytes each, and as many of values, at 16 bytes for each slot
// (a Value is an interface). A mebibyte holds 1<<14 frames or 1<<16 slots. Frames
// alone would not bound those values: each call that is alive holds an
// environment of FrameSize slots, and each call being prepared holds an argFrame
// with room for its callee and its arguments, so the bytes that a level of a
// recursion takes grow with the variables and the arguments of the function. New
// reads the variable (see framesFor and slotsFor) and enter checks both limits.
const (
	stackEnv       = "KI_STACK_MB"
	defaultStackMB = 256
	framesPerMB    = 1 << 14
	slotsPerMB     = 1 << 16
)

// framesFor returns the limit of the frames of the stack for the value of
// KI_STACK_MB (see limitFor).
func framesFor(setting string) int { return limitFor(setting, framesPerMB) }

// slotsFor returns the limit of the slots of the environments for the value of
// KI_STACK_MB (see limitFor).
func slotsFor(setting string) int { return limitFor(setting, slotsPerMB) }

// limitFor returns the number of mebibytes that setting says times perMB when it
// is a positive integer, and the default number of mebibytes times perMB when it
// is anything else (empty, not a number, zero or negative). A number too large
// for the product is cut to the largest that fits, because the product would
// otherwise wrap around to a negative limit.
func limitFor(setting string, perMB int) int {
	// The error says why Atoi failed, and the number says enough: it is 0 for a
	// text that is not an integer, and the largest int of its sign for one that
	// does not fit in an int.
	mb, _ := strconv.Atoi(setting)
	if mb <= 0 {
		mb = defaultStackMB
	}
	return min(mb, math.MaxInt/perMB) * perMB
}

// Machine is a CEK machine over one Program. Make it with New. A Machine is not
// safe for use by several goroutines.
type Machine struct {
	prog *core.Program
	// sm is where the spans of Core are turned into positions, for the messages
	// of the program's panics and stack overflows.
	sm *base.SourceManager
	// out and err are the stdout and the stderr of the program.
	out, err io.Writer

	// envs are the environments of the functions being run, the one of the
	// function that runs now being the last (a tail call replaces it). Each has
	// FrameSize slots. The code indexes envs and never keeps a pointer to one of
	// its elements (&envs[i]): an append to envs may copy the slice headers it
	// holds to a bigger array and leave that pointer on the old one. The slots
	// do not move with them, because each header points to its own array.
	envs [][]Value
	// stack is the continuation: the frames, the one to apply next being the last.
	stack []frame
	stats Stats
	// maxFrames is the limit of the stack: a call that is about to push its
	// retFrame overflows if the stack already holds maxFrames frames or more (see
	// enter). Stats.MaxDepth can still end above maxFrames, for two reasons. The
	// check runs only when a call is entered, so at the check the stack can be
	// over maxFrames by what the expressions around the call pushed since the
	// previous check. And the check comes after the arguments of the call were
	// evaluated, when the frames they used (its argFrame and those of the
	// expressions inside the arguments) are gone, but MaxDepth had counted them.
	// Both come from how the expressions nest, which MaxNesting (256, E0204)
	// and, for a chain of operators, MaxChain (1024, E0205) limit together.
	// TestMachine/stack_overflow_exits_103 works out the figure for its program.
	maxFrames int
	// slots is how many slots the environments in envs and the argFrames on the
	// stack have, added up, and maxSlots is its limit: a call whose environment
	// would take slots past maxSlots overflows (see enter).
	slots, maxSlots int
	// halt is nil while the program runs, and the state it ended in once it has
	// panicked or overflowed its stack. run stops as soon as it is set.
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
// the stdout and the stderr of the program. What the program prints (`print`,
// see externs) goes to out. The machine writes the messages of the program's
// panics and of its stack overflow to err, using sm for their positions. The
// limits of the stack are the ones that the KI_STACK_MB variable says when New
// reads it (see framesFor and slotsFor).
func New(p *core.Program, sm *base.SourceManager, out, err io.Writer) *Machine {
	setting := os.Getenv(stackEnv)
	return &Machine{prog: p, sm: sm, out: out, err: err, maxFrames: framesFor(setting), maxSlots: slotsFor(setting)}
}

// Stats returns the counts of what the machine has done since New. In Part I only
// the tests and an exercise of chapter 10 read them, and no chapter has fixed a
// command that shows them.
func (m *Machine) Stats() Stats { return m.stats }

// RunMain runs the function `main` of the program and returns the state the
// machine ends in: Halt{Code: 0} when `main` returns, and the Halt of the panic
// or of the stack overflow when the program ends in one (its message is then
// already on err). RunMain itself panics if the program has no `main`, so
// whoever runs a program has to check that first.
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
// returns its value. The second result is false if the program panicked or
// overflowed its stack, and then the message is already on err and the value is
// nil.
//
// Each call starts from an empty machine, so a Call never sees what an earlier
// one left, not even the frames of a program that panicked; only Stats go on
// counting. Call panics, with a message that says why, if f takes another number
// of parameters than len(args) or has no body (a function that the runtime
// provides, which only a call from Ki runs: see enter): those are mistakes of
// the caller, and not something a Ki program can do.
func (m *Machine) Call(f core.FunID, args []Value) (Value, bool) {
	fun := m.prog.Funs.Get(f)
	if len(args) != len(fun.Params) {
		panic(fmt.Sprintf("interp: function %d takes %d arguments, but Call got %d", f, len(fun.Params), len(args)))
	}
	if !fun.HasBody {
		panic(fmt.Sprintf("interp: Call of function %d, which has no body", f))
	}
	entry := m.newEnv(fun, args)
	m.envs = append(m.envs[:0], entry)
	m.slots = len(entry)
	m.stack = m.stack[:0]
	m.halt = nil
	m.stats.MaxEnvs = max(m.stats.MaxEnvs, len(m.envs))
	m.eval(fun.Body)
	return m.run()
}

// run steps the machine until the control is a value and no frame is left, and
// returns that value and true. If the program panics or overflows its stack
// first, it stops there and returns nil and false. It is a loop and not a
// recursion: how deep the program is shows in the length of m.stack, not in the
// stack of Go.
func (m *Machine) run() (Value, bool) {
	for m.halt == nil {
		switch n := len(m.stack); {
		case !m.returning:
			m.stats.Steps++
			m.step(m.expr)
		case n > 0:
			m.stats.Steps++
			f := m.stack[n-1]
			// Cutting the slice does not clear the frame it drops: the backing
			// array still points at it, and at the values it holds, until a later
			// push overwrites it. The same goes for the environments that
			// applyFrame cuts off envs. Nothing here needs it otherwise, because
			// `ki run` makes one machine for one program and then ends; a machine
			// reused after a deep run would have to clear them.
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

// stop records h, the state the program ends in: run returns as soon as it is
// set. It is the one place that does.
func (m *Machine) stop(h Halt) { m.halt = &h }

// push adds f to the continuation.
func (m *Machine) push(f frame) {
	m.stack = append(m.stack, f)
	m.stats.MaxDepth = max(m.stats.MaxDepth, len(m.stack))
}

// env returns the environment of the function that is running. The slice shares
// its elements with the one in envs, so writing to it writes there.
func (m *Machine) env() []Value { return m.envs[len(m.envs)-1] }

// step evaluates the expression id: a literal, a variable or a function name
// returns its value; a Let evaluates its Init under a letFrame that remembers
// what to do next; a PrimCall starts on its first argument under a primFrame; a
// Call starts on its callee under an argFrame; and a Match starts on its
// scrutinee under a matchFrame.
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
	case core.FunRef:
		m.ret(FunV{Fun: x.Fun})
	case core.Let:
		m.push(letFrame{v: x.Var, body: x.Body})
		m.eval(x.Init)
	case core.PrimCall:
		// Every primitive has one or two arguments (Prim.Arity), and Lint checks
		// that a PrimCall has that many.
		m.push(primFrame{call: id})
		m.eval(m.prog.Args(x.Args)[0])
	case core.Call:
		// The callee is a value like any other (Core allows any expression
		// there), so it is evaluated first, and then each argument. The frame
		// has room for all the values, and those count as slots until enter
		// takes them out.
		n := 1 + len(m.prog.Args(x.Args))
		m.slots += n
		m.push(argFrame{call: id, vals: make([]Value, 0, n)})
		m.eval(x.Callee)
	case core.Match:
		m.push(matchFrame{match: id})
		m.eval(x.Scrutinee)
	default:
		panic("unreachable")
	}
}

// applyFrame returns the value v to the frame f, which the machine has just
// taken off the stack. For a letFrame, v is written in the slot of the
// variable, and the body becomes the control: nothing is pushed, so a chain of
// Lets does not make the stack grow. For a primFrame, v is the value of an
// argument: either the second argument is evaluated next, or the primitive runs
// and its value is returned, or, if it fails, the program panics and the
// machine halts with the panic. For an argFrame, v is the value of the callee
// or of an argument: either the next argument is evaluated, or the call is made
// (see enter). For a matchFrame, v is the scrutinee, and the arm that fits
// becomes the control, again with nothing pushed, so a call in an arm is still
// in tail position. For a retFrame, the function that made the call is
// returning v.
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
			m.stop(m.panicAt(x.Span, msg))
			return
		}
		m.ret(r)
	case argFrame:
		x := m.prog.Exprs.Get(f.call)
		vals := append(f.vals, v)
		// The callee is vals[0], so the next argument is the one with index
		// len(vals)-1, if there is one.
		if args := m.prog.Args(x.Args); len(vals) <= len(args) {
			m.push(argFrame{call: f.call, vals: vals})
			m.eval(args[len(vals)-1])
			return
		}
		m.enter(x, vals)
	case matchFrame:
		x := m.prog.Exprs.Get(f.match)
		// The type checker has made the scrutinee a Bool, like the operands of a
		// primitive (see applyPrim).
		m.eval(selectArm(m.prog.ArmsOf(x.Arms), v.(BoolV).V))
	case retFrame:
		// The function that returns has the last environment, and it is the only
		// one above that of its caller: each frame of this kind has one of its own.
		m.slots -= len(m.envs[len(m.envs)-1])
		m.envs = m.envs[:f.env+1]
		m.ret(v)
	default:
		panic("unreachable")
	}
}

// selectArm returns the body of the arm of a Match that fits b: the first arm
// whose pattern is that value, or, when there is none, the first wildcard arm.
// A Core that Lint accepted has an arm for each value or a wildcard, so one
// always fits; if none does, selectArm panics instead of going on with no arm.
func selectArm(arms []core.Arm, b bool) core.ExprID {
	var wildcard core.ExprID
	haveWildcard := false
	for _, arm := range arms {
		switch arm.Pat {
		case core.PatTrue:
			if b {
				return arm.Body
			}
		case core.PatFalse:
			if !b {
				return arm.Body
			}
		case core.PatWildcard:
			if !haveWildcard {
				wildcard, haveWildcard = arm.Body, true
			}
		default:
			panic("unreachable")
		}
	}
	if !haveWildcard {
		panic("interp: no arm of the match fits the value")
	}
	return wildcard
}

// newEnv returns the environment of a call of fun with the arguments args: a
// slice of fun.FrameSize slots with each argument in the slot of its parameter.
// Lint checks that a call of a function that it names has as many arguments as
// the function has parameters, and the type checker checks the others.
func (m *Machine) newEnv(fun core.Fun, args []Value) []Value {
	env := make([]Value, fun.FrameSize)
	for i, p := range fun.Params {
		env[m.prog.Vars.Get(p).Slot] = args[i]
	}
	return env
}

// enter makes the call of the Call expression call, once its argFrame has
// collected the values vals (the callee first, then the arguments). It makes
// the body of the callee the control, in a new environment. A callee with no
// body is a function of the runtime: it runs at once (see callExtern) and its
// value goes to the top frame. It pushes no frame and no environment, so it
// adds nothing to envs.
//
// A call in tail position, one with no frame waiting for its value but the
// retFrame of the function that makes it, reuses that retFrame and puts the new
// environment in place of the caller's: the function that made the call has
// nothing left to do, so neither it nor its environment needs to stay. That is
// the machine's tail-call rule, and it is why a loop written as a tail call
// does not grow the stack or envs. The function that Call starts has no
// retFrame, so its own tail call pushes one, and the calls after that reuse it.
// The stack is not checked in this case, because the call pushes nothing. Its
// environment can be larger than the one it replaces, and the next call that
// pushes a frame sees that in slots, so a loop of tail calls cannot grow past
// the limit by more than the one environment that it keeps replacing.
//
// Any other call pushes a retFrame that remembers the environment of the
// caller, and appends the new one. If the stack already holds maxFrames frames
// or more, or if the new environment would take slots past maxSlots, it
// overflows instead: the machine writes the message at the span of the call to
// err and halts with Halt{Code: 103}.
//
// The check is only here, and step charges an argFrame when it pushes it, so
// slots can pass maxSlots between two checks: by the one environment that a tail
// call can make bigger, and by the argFrames opened since the last check that
// have not been entered yet. Those are nested in one another in the source, so
// MaxNesting (256, E0204) limits how many there are. The excess does not add up
// from level to level: the next check sees it and overflows.
func (m *Machine) enter(call core.Expr, vals []Value) {
	// The argFrame is gone: its values become the new environment, or the
	// arguments of a function of the runtime, and step charged all of them.
	m.slots -= cap(vals)
	// A callee that is not a function would be a bug of the type checker.
	callee := vals[0].(FunV)
	fun := m.prog.Funs.Get(callee.Fun)
	if !fun.HasBody {
		m.ret(m.callExtern(fun.ExternName, vals[1:]))
		return
	}
	env := m.newEnv(fun, vals[1:])

	if n := len(m.stack); n > 0 && isRetFrame(m.stack[n-1]) {
		top := len(m.envs) - 1
		m.slots += len(env) - len(m.envs[top])
		m.envs[top] = env
	} else {
		if len(m.stack) >= m.maxFrames || m.slots+len(env) > m.maxSlots {
			m.stop(m.haltAt(call.Span, "stack overflow", codeStackOverflow))
			return
		}
		m.push(retFrame{env: len(m.envs) - 1})
		m.envs = append(m.envs, env)
		m.slots += len(env)
	}
	m.stats.MaxEnvs = max(m.stats.MaxEnvs, len(m.envs))
	m.eval(fun.Body)
}

// isRetFrame reports whether f is a retFrame.
func isRetFrame(f frame) bool {
	_, ok := f.(retFrame)
	return ok
}
