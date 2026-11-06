package core

import (
	"fmt"
	"iter"

	"github.com/aldezex/kilang/internal/base"
)

// appendChildren appends to buf the expressions directly under e, in the order
// a walk visits them, and returns the result: the callee of a Call and then its
// arguments, the arguments of a PrimCall or a Con, the init of a Let and then its
// body, and the scrutinee of a Match and then the body of each arm in order.
func (p *Program) appendChildren(buf []ExprID, e *Expr) []ExprID {
	switch e.Kind {
	case LitInt, LitFloat, LitBool, LitUnit, VarRef, FunRef:
		return buf
	case Call:
		buf = append(buf, e.Callee)
		return append(buf, p.Args(e.Args)...)
	case PrimCall, Con:
		return append(buf, p.Args(e.Args)...)
	case Proj:
		return append(buf, e.Target)
	case Let:
		return append(buf, e.Init, e.Body)
	case Match:
		buf = append(buf, e.Scrutinee)
		for _, arm := range p.ArmsOf(e.Arms) {
			buf = append(buf, arm.Body)
		}
		return buf
	default:
		panic("unreachable")
	}
}

// Subexprs returns an iterator over root and every expression under it, in
// pre-order: each expression comes before its children (see appendChildren for
// their order). It keeps the expressions still to visit in a stack of its own
// instead of calling itself, so a Let chain as long as a block of any number
// of statements adds no frames of Go. It only reads the Program. A loop that
// stops early stops the walk.
//
// The expressions under root must form a tree: none is under itself, and none
// is the child of two parents. Subexprs does not check it and keeps no record
// of what it has visited, so on a Program that is not a tree it may visit an
// expression once for each parent, or never finish.
func (p *Program) Subexprs(root ExprID) iter.Seq[ExprID] {
	return func(yield func(ExprID) bool) {
		stack := []ExprID{root}
		var kids []ExprID
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if !yield(id) {
				return
			}
			e := p.Exprs.Get(id)
			kids = p.appendChildren(kids[:0], &e)
			// The first child goes on last, so that it is the next one out.
			for i := len(kids) - 1; i >= 0; i-- {
				stack = append(stack, kids[i])
			}
		}
	}
}

// Lint checks the invariants of Core that the elaboration promises and the
// passes after it rely on, except that each Var is bound in one place (see
// Var), which it does not check in Part I, and returns one string for each
// violation. An empty result means valid. The violations come in the order of
// the functions of p and, in each, the shape of the function first, then its
// parameters, then its body in pre-order (a node before its children). Each
// string reads "fun NAME: WHAT", with the name of the function as in the
// source, and the functions and variables that WHAT mentions are named the same
// way, with a variable or a called function between backquotes: the message of
// the report of an ICE of the pass lint is this text, and a person reading it
// has the file and no ids. in only spells the names; Lint adds nothing to it. A
// name is not an identity (a variable that shadows another has the same name,
// and so does a function of the prelude and one of the file), but the function
// and the name are where to look. It checks that:
//
//   - every VarRef is in scope: its variable is a parameter of the function or
//     the variable of a Let whose body holds the VarRef (the init of a Let is
//     outside the scope of its own variable);
//   - a call of a FunRef has as many arguments as that function has
//     parameters, and a PrimCall as many as Prim.Arity says (a call through a
//     variable is not checked: Lint does not know what the variable holds);
//   - a Con has as many arguments as its constructor has fields (Ctor.Arity); a
//     Proj has nothing of its own to check, because Core does not know how many
//     fields the value of its target has;
//   - a Match has the arms true and false, or a wildcard arm;
//   - a function either has a body and no extern name, or an extern name and no
//     body, and it walks the body of a function only if it has one;
//   - the slot of every parameter and every Let variable is less than the
//     FrameSize of its function.
//
// Lint assumes that the expressions, functions and constructors it follows
// exist, and the variable of every Let, parameter and VarRef (it needs the
// variable to spell its name): an id past the end of its table is an index out of
// range (see base.IdVec.Get), not a violation. A Let chain or a nesting of any
// depth is fine, because the walk keeps its own stack (see Subexprs).
//
// It also assumes that the body of each function is a tree, as Subexprs says:
// no expression is under itself, and none is the child of two parents. Lint
// does not check it, so on a Program that is not a tree it may report a
// violation once for each parent of the expression that has it, or never
// return.
func Lint(p *Program, in *base.Interner) []string {
	l := &linter{p: p, in: in, scope: make(map[VarID]int)}
	for id, f := range p.Funs.All() {
		l.fun(id, f)
	}
	return l.out
}

// linter is the state of one call to Lint.
type linter struct {
	p   *Program
	in  *base.Interner
	out []string
	// cur is the function being checked, for the text of its violations.
	cur FunID
	// scope counts, for each variable, how many of the parameters and Lets
	// that bind it hold the expression being visited: a variable is in scope
	// when its count is not zero. In a Program that keeps its promise of unique
	// binders the count is 0 or 1.
	scope map[VarID]int
}

// lintAction says what a lintStep asks the walk to do.
type lintAction uint8

const (
	lintVisit  lintAction = iota // check the expression and go on to its children
	lintBind                     // the init of a Let is done: its variable comes into scope
	lintUnbind                   // the body of a Let is done: its variable leaves the scope
)

// lintStep is an entry of the stack of the walk: an expression to visit, or a
// change of scope to make at that point of the walk.
type lintStep struct {
	action lintAction
	expr   ExprID // for lintVisit
	v      VarID  // for lintBind and lintUnbind
}

// report adds a violation in the function being checked. The name of the
// function is spelled here, when there is a violation, and not when the
// function starts: most functions have none.
func (l *linter) report(format string, args ...any) {
	l.out = append(l.out, "fun "+l.in.String(l.p.Funs.Get(l.cur).Name)+": "+fmt.Sprintf(format, args...))
}

// fun checks one function.
func (l *linter) fun(id FunID, f Fun) {
	l.cur = id
	switch {
	case f.HasBody && f.ExternName != "":
		l.report("extern `%s` has a body", f.ExternName)
	case !f.HasBody && f.ExternName == "":
		l.report("has no body and no extern name")
	}
	for _, v := range f.Params {
		l.slot(v, f.FrameSize)
	}
	if !f.HasBody {
		return
	}
	for _, v := range f.Params {
		l.scope[v]++
	}
	l.walk(f.Body, f.FrameSize)
	for _, v := range f.Params {
		l.unbind(v)
	}
}

// slot reports the variable v if its slot is not in a frame of frame slots.
func (l *linter) slot(v VarID, frame uint32) {
	if s := l.p.Vars.Get(v).Slot; s >= frame {
		l.report("variable `%s` has slot %d, outside a frame of %d slot(s)", l.in.String(l.p.Vars.Get(v).Name), s, frame)
	}
}

// unbind takes one binding of v out of the scope.
func (l *linter) unbind(v VarID) {
	if l.scope[v]--; l.scope[v] == 0 {
		delete(l.scope, v)
	}
}

// walk checks the expression root and everything under it, with a stack of
// steps in place of recursion. A Let does not push its children as they are:
// the order of what it pushes is what makes the scope right, as the comments
// below say.
func (l *linter) walk(root ExprID, frame uint32) {
	stack := []lintStep{{action: lintVisit, expr: root}}
	var kids []ExprID
	for len(stack) > 0 {
		st := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch st.action {
		case lintBind:
			l.scope[st.v]++
		case lintUnbind:
			l.unbind(st.v)
		case lintVisit:
			e := l.p.Exprs.Get(st.expr)
			l.check(&e, frame)
			if e.Kind == Let {
				// The stack gives them back in the opposite order: the init,
				// then the variable comes into scope, then the body, then the
				// variable leaves.
				stack = append(stack,
					lintStep{action: lintUnbind, v: e.Var},
					lintStep{action: lintVisit, expr: e.Body},
					lintStep{action: lintBind, v: e.Var},
					lintStep{action: lintVisit, expr: e.Init})
				continue
			}
			kids = l.p.appendChildren(kids[:0], &e)
			for i := len(kids) - 1; i >= 0; i-- {
				stack = append(stack, lintStep{action: lintVisit, expr: kids[i]})
			}
		default:
			panic("unreachable")
		}
	}
}

// check reports what is wrong with the expression e itself, not with its
// children.
func (l *linter) check(e *Expr, frame uint32) {
	switch e.Kind {
	case LitInt, LitFloat, LitBool, LitUnit, FunRef:
	case VarRef:
		if l.scope[e.Var] == 0 {
			l.report("unbound variable `%s`", l.in.String(l.p.Vars.Get(e.Var).Name))
		}
	case Call:
		if callee := l.p.Exprs.Get(e.Callee); callee.Kind == FunRef {
			want, got := len(l.p.Funs.Get(callee.Fun).Params), len(l.p.Args(e.Args))
			if got != want {
				l.report("call of fun `%s` with %d argument(s), but it takes %d", l.in.String(l.p.Funs.Get(callee.Fun).Name), got, want)
			}
		}
	case PrimCall:
		want, got := e.Prim.Arity(), len(l.p.Args(e.Args))
		if got != want {
			l.report("primitive %s with %d argument(s), but it takes %d", e.Prim, got, want)
		}
	case Con:
		ctor := l.p.Ctors.Get(e.Ctor)
		if got := len(l.p.Args(e.Args)); got != int(ctor.Arity) {
			// A tuple has no name: it is told by its arity.
			name := "constructor `" + l.in.String(ctor.Name) + "`"
			if ctor.Tuple {
				name = "tuple constructor"
			}
			l.report("%s with %d argument(s), but it takes %d", name, got, ctor.Arity)
		}
	case Proj:
		// Core has no types, so nothing says how many fields the value of the
		// target has: there is nothing to check of the node itself.
	case Let:
		l.slot(e.Var, frame)
	case Match:
		var hasTrue, hasFalse, hasWildcard bool
		for _, arm := range l.p.ArmsOf(e.Arms) {
			switch arm.Pat {
			case PatTrue:
				hasTrue = true
			case PatFalse:
				hasFalse = true
			case PatWildcard:
				hasWildcard = true
			default:
				panic("unreachable")
			}
		}
		if !(hasTrue && hasFalse) && !hasWildcard {
			l.report("match must have an arm for true and one for false, or a wildcard arm")
		}
	default:
		panic("unreachable")
	}
}
