package core

import (
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
)

// Print writes the functions of the user's file as text, one after the other
// in the order of the Program (the order of the source), and returns it; the
// prelude's functions are left out. It is what `ki emit core` shows.
//
// A function is `fn NAME(PARAMS) =` and its body in the next line, indented two
// spaces; a function with no body is only `fn NAME(PARAMS)`. In the body:
//
//   - a Let is `let x = INIT;` and its body in the next line, so a chain of
//     them reads from top to bottom;
//   - a Match is `match S {`, one line for each arm two spaces further in
//     (`true => …`, `false => …`, `_ => …`) and a `}`;
//   - a call or a primitive is `callee(a, b)` or `add_int(a, b)`, all in one
//     line unless an operand takes several.
//
// Whatever starts in the middle of a line (an argument, the callee, the init of
// a Let, the scrutinee or the body of an arm) goes on in that line, and its
// other lines are indented two spaces more than the line it started in, except
// the `}` of a Match, which lines up with that line. What comes after it (`)`,
// `, ` or `;`) follows its last line. A Let that starts a line of a chain, as
// the body of a function or of another Let does, keeps the indentation of the
// chain instead.
//
// Variables are called by their own names, in the order the text reaches them,
// the parameters first. A name that the function has used already gets a
// number: `x`, `x_1`, `x_2`… and, for `_` (which every expression statement
// binds), `_`, `_1`, `_2`… A number that would make the name of another
// variable is skipped: after `x_1`, `x` and `x`, the third is `x_2`. So two
// variables of a function never print alike; a function and a variable that
// have the same name do, since the text does not tell a FunRef from a VarRef.
// The text is for reading and not for parsing back: a Let that is the callee of
// a call or the scrutinee of a match has no delimiter, so it reads like a Let
// whose body is the call or the match.
//
// Print walks with a stack of its own and never calls itself: a Let chain or
// nested arguments of any depth add no frames of Go. Like Lint, it assumes that
// the body of each function is a tree (see Subexprs) and does not check it.
func Print(p *Program, in *base.Interner) string {
	pr := &printer{p: p, in: in}
	for _, f := range p.Funs.All() {
		if f.UserModule {
			pr.fun(f)
		}
	}
	return pr.out.String()
}

// printTaskKind says what a printTask asks the printer to do.
type printTaskKind uint8

const (
	taskExpr    printTaskKind = iota // write an expression
	taskText                         // write text as it is
	taskNewline                      // start a new line, indented
)

// printTask is an entry of the stack of the printer.
type printTask struct {
	kind printTaskKind
	// For taskExpr: the expression, the indentation of the line where it starts
	// and whether it is in a chain (the body of a function, or of a Let that is
	// in one), where the body of a Let goes on at the same indentation; outside
	// a chain it goes two spaces further in.
	expr  ExprID
	ind   int
	chain bool
	// For taskText, the text; for taskNewline, ind is the indentation.
	text string
}

// printer is the state of one call to Print.
type printer struct {
	p   *Program
	in  *base.Interner
	out strings.Builder

	// The names of the function being printed: what each of its variables is
	// called, every name in use, and for a name that repeats the number that the
	// next one will try first.
	names map[VarID]string
	used  map[string]bool
	next  map[string]int

	stack []printTask
	seq   []printTask // scratch for the tasks of one node
}

// fun writes one function.
func (pr *printer) fun(f Fun) {
	pr.names, pr.used, pr.next = map[VarID]string{}, map[string]bool{}, map[string]int{}
	pr.out.WriteString("fn " + pr.in.String(f.Name) + "(")
	for i, v := range f.Params {
		if i > 0 {
			pr.out.WriteString(", ")
		}
		pr.out.WriteString(pr.name(v))
	}
	pr.out.WriteString(")")
	if f.HasBody {
		pr.out.WriteString(" =")
		pr.newline(1)
		pr.run(printTask{kind: taskExpr, expr: f.Body, ind: 1, chain: true})
	}
	pr.out.WriteString("\n")
}

// newline writes a line break and the indentation of ind levels of two spaces.
func (pr *printer) newline(ind int) {
	pr.out.WriteString("\n")
	for i := 0; i < ind; i++ {
		pr.out.WriteString("  ")
	}
}

// name returns what the variable v is called in the function being printed,
// choosing it the first time it is asked for (see Print for the rule).
func (pr *printer) name(v VarID) string {
	if s, ok := pr.names[v]; ok {
		return s
	}
	stem := pr.in.String(pr.p.Vars.Get(v).Name)
	sep := "_"
	if stem == "_" {
		sep = ""
	}
	s := stem
	if pr.used[s] {
		k := max(pr.next[stem], 1)
		for {
			s = stem + sep + strconv.Itoa(k)
			k++
			if !pr.used[s] {
				break
			}
		}
		pr.next[stem] = k
	}
	pr.used[s] = true
	pr.names[v] = s
	return s
}

// run does the tasks that start from first, and the tasks they make, until none
// is left.
func (pr *printer) run(first printTask) {
	pr.stack = append(pr.stack[:0], first)
	for len(pr.stack) > 0 {
		t := pr.stack[len(pr.stack)-1]
		pr.stack = pr.stack[:len(pr.stack)-1]
		switch t.kind {
		case taskText:
			pr.out.WriteString(t.text)
		case taskNewline:
			pr.newline(t.ind)
		case taskExpr:
			pr.node(t)
		default:
			panic("unreachable")
		}
	}
}

// push puts the tasks of pr.seq on the stack so that the first comes out first.
func (pr *printer) push() {
	for i := len(pr.seq) - 1; i >= 0; i-- {
		pr.stack = append(pr.stack, pr.seq[i])
	}
	pr.seq = pr.seq[:0]
}

// addText, addExpr and addNewline add a task to pr.seq.
func (pr *printer) addText(s string) {
	pr.seq = append(pr.seq, printTask{kind: taskText, text: s})
}

func (pr *printer) addExpr(id ExprID, ind int, chain bool) {
	pr.seq = append(pr.seq, printTask{kind: taskExpr, expr: id, ind: ind, chain: chain})
}

func (pr *printer) addNewline(ind int) {
	pr.seq = append(pr.seq, printTask{kind: taskNewline, ind: ind})
}

// node writes the start of the expression of t that needs no later task, and
// pushes the tasks for the rest.
func (pr *printer) node(t printTask) {
	e := pr.p.Exprs.Get(t.expr)
	switch e.Kind {
	case LitInt:
		pr.out.WriteString(strconv.FormatInt(e.Int, 10))
	case LitBool:
		pr.out.WriteString(strconv.FormatBool(e.Bool))
	case LitUnit:
		pr.out.WriteString("()")
	case VarRef:
		pr.out.WriteString(pr.name(e.Var))
	case FunRef:
		pr.out.WriteString(pr.in.String(pr.p.Funs.Get(e.Fun).Name))
	case Call:
		pr.addExpr(e.Callee, t.ind, false)
		pr.args(e.Args, t.ind)
	case PrimCall:
		pr.out.WriteString(e.Prim.String())
		pr.args(e.Args, t.ind)
	case Let:
		pr.out.WriteString("let " + pr.name(e.Var) + " = ")
		bodyInd := t.ind
		if !t.chain {
			bodyInd++
		}
		pr.addExpr(e.Init, t.ind, false)
		pr.addText(";")
		pr.addNewline(bodyInd)
		pr.addExpr(e.Body, bodyInd, true)
	case Match:
		pr.out.WriteString("match ")
		pr.addExpr(e.Scrutinee, t.ind, false)
		pr.addText(" {")
		for _, arm := range pr.p.ArmsOf(e.Arms) {
			pr.addNewline(t.ind + 1)
			switch arm.Pat {
			case PatTrue:
				pr.addText("true => ")
			case PatFalse:
				pr.addText("false => ")
			case PatWildcard:
				pr.addText("_ => ")
			default:
				panic("unreachable")
			}
			pr.addExpr(arm.Body, t.ind+1, false)
		}
		pr.addNewline(t.ind)
		pr.addText("}")
	default:
		panic("unreachable")
	}
	pr.push()
}

// args adds to pr.seq the parenthesized list of arguments that follows a callee
// or a primitive's name.
func (pr *printer) args(r base.ListRange, ind int) {
	pr.addText("(")
	for i, a := range pr.p.Args(r) {
		if i > 0 {
			pr.addText(", ")
		}
		pr.addExpr(a, ind, false)
	}
	pr.addText(")")
}
