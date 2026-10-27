package syntax

import (
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
)

// PrintModule writes the items of m as S-expressions, one line per item, each
// ended by a line feed: the text of `ki emit ast`. A module with no items
// prints as the empty string. The forms are
//
//	([pub ]fn NAME [(attrs (NAME "arg"...)...)] (params (NAME T|_)...) (ret T|_) BODY|none)
//	(block S... [tail])      (let NAME T|_ E)      (expr E)
//	(if C T [E])             (OP L R)              (neg E)    (not E)
//	(call F ARGS...)         42    true    ()      NAME       (error)
//
// where T is a type (`Int`, `()`, `(error)`) and `_` stands for a type that the
// program did not write. The arguments of an attribute are quoted as
// strconv.Quote does. An attribute list that is empty is left out.
//
// Parse produces every one of these forms; the printer's own tests build their
// trees by hand, so that they do not depend on the parser.
//
// PrintModule does not change m. Printing recurses once per level of the tree
// (a loop walks each list of children), so the depth of Go's call stack follows
// the depth of the tree. Nothing bounds the depth of the trees that Parse builds
// yet: the limit on the nesting of expressions comes in a later session.
func PrintModule(m *Module, in *base.Interner) string {
	pr := printer{m: m, in: in}
	for _, id := range m.Items {
		pr.item(m.ItemNodes.Get(id))
		pr.b.WriteByte('\n')
	}
	return pr.b.String()
}

// printer is the state of one call to PrintModule.
type printer struct {
	m  *Module
	in *base.Interner
	b  strings.Builder
}

// str appends s.
func (pr *printer) str(s string) { pr.b.WriteString(s) }

// sym appends the text of a name.
func (pr *printer) sym(s base.Symbol) { pr.b.Write(pr.in.Bytes(s)) }

func (pr *printer) item(it Item) {
	switch it.Kind {
	case ItemFn:
		pr.str("(")
		if it.Pub {
			pr.str("pub ")
		}
		pr.str("fn ")
		pr.sym(it.Name)
		if it.Attrs.Count > 0 {
			pr.str(" (attrs")
			for _, a := range pr.m.Attrs(it.Attrs) {
				pr.str(" (")
				pr.sym(a.Name)
				for _, arg := range a.Args {
					pr.str(" " + strconv.Quote(arg))
				}
				pr.str(")")
			}
			pr.str(")")
		}
		pr.str(" (params")
		for _, par := range pr.m.Params(it.Params) {
			pr.str(" (")
			pr.sym(par.Name)
			pr.str(" ")
			pr.optType(par.Type, par.HasType)
			pr.str(")")
		}
		pr.str(") (ret ")
		pr.optType(it.Ret, it.HasRet)
		pr.str(") ")
		if it.HasBody {
			pr.expr(it.Body)
		} else {
			pr.str("none")
		}
		pr.str(")")
	case ItemError:
		pr.str("(error)")
	default:
		panic("unreachable")
	}
}

// optType appends the type, or `_` if the program wrote none.
func (pr *printer) optType(id TypeExprID, has bool) {
	if !has {
		pr.str("_")
		return
	}
	t := pr.m.Types.Get(id)
	switch t.Kind {
	case TypeNamed:
		pr.sym(t.Name)
	case TypeUnit:
		pr.str("()")
	case TypeError:
		pr.str("(error)")
	default:
		panic("unreachable")
	}
}

func (pr *printer) stmt(id StmtID) {
	s := pr.m.Stmts.Get(id)
	switch s.Kind {
	case StmtLet:
		pr.str("(let ")
		pr.sym(s.Name)
		pr.str(" ")
		pr.optType(s.Type, s.HasType)
		pr.str(" ")
		pr.expr(s.Expr)
		pr.str(")")
	case StmtExpr:
		pr.str("(expr ")
		pr.expr(s.Expr)
		pr.str(")")
	case StmtError:
		pr.str("(error)")
	default:
		panic("unreachable")
	}
}

func (pr *printer) expr(id ExprID) {
	e := pr.m.Exprs.Get(id)
	switch e.Kind {
	case ExprInt:
		pr.str(strconv.FormatInt(e.Int, 10))
	case ExprBool:
		pr.str(strconv.FormatBool(e.Bool))
	case ExprUnit:
		pr.str("()")
	case ExprName:
		pr.sym(e.Name)
	case ExprCall:
		pr.str("(call ")
		pr.expr(e.Callee)
		for _, arg := range pr.m.ExprList(e.Args) {
			pr.str(" ")
			pr.expr(arg)
		}
		pr.str(")")
	case ExprBinary:
		pr.str("(" + e.Op.String() + " ")
		pr.expr(e.Lhs)
		pr.str(" ")
		pr.expr(e.Rhs)
		pr.str(")")
	case ExprUnary:
		pr.str("(" + e.UnOp.String() + " ")
		pr.expr(e.Operand)
		pr.str(")")
	case ExprIf:
		pr.str("(if ")
		pr.expr(e.Cond)
		pr.str(" ")
		pr.expr(e.Then)
		if e.HasElse {
			pr.str(" ")
			pr.expr(e.Else)
		}
		pr.str(")")
	case ExprBlock:
		pr.str("(block")
		for _, s := range pr.m.StmtList(e.Stmts) {
			pr.str(" ")
			pr.stmt(s)
		}
		if e.HasTail {
			pr.str(" ")
			pr.expr(e.Tail)
		}
		pr.str(")")
	case ExprError:
		pr.str("(error)")
	default:
		panic("unreachable")
	}
}
