package syntax

import (
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/kirt/fmtnum"
)

// PrintModule writes the items of m as S-expressions, one line per item, each
// ended by a line feed: the text of `ki emit ast`. A module with no items
// prints as the empty string. The forms are
//
//	([pub ]fn NAME [(attrs (NAME "arg"...)...)] (params (NAME T|_)...) (ret T|_) BODY|none)
//	([pub ]type NAME [(attrs ...)] (sum (NAME (FNAME T)...)...))
//	([pub ]type NAME [(attrs ...)] (record (FNAME T)...))
//	([pub ]type NAME [(attrs ...)] (alias T))
//	(block S... [tail])      (let NAME T|_ E)      (expr E)
//	(let PAT T|_ E)          a `let` whose pattern is not a plain name
//	(if C T [E])             (OP L R)              (neg E)    (not E)
//	(call F ARGS...)         42    true    ()      NAME       (error)
//	1.5                      a Float, written as kirt/fmtnum writes it: 2.0, 1e21
//	(field E NAME)           (index E N)           (tuple E E...)
//	(record NAME (FNAME E)... [(.. E)])
//	(match E ARM...)       ARM is (arm PAT BODY) or (arm PAT (guard G) BODY)
//
// where T is a type (`Int`, `()`, `(error)`, `(tuple T T...)`) and `_` stands for
// a type that the program did not write. The arguments of an attribute are quoted as
// strconv.Quote does. An attribute list that is empty is left out.
//
// A type lists its variants, each with its fields, or its fields, or the type
// it stands for. A field is `(FNAME T)`, and FNAME is `_` for a positional one:
// `type Option { None, Some(Int) }` is `(type Option (sum (None) (Some (_ Int))))`
// and `type Point { x: Float }` is `(type Point (record (x Float)))`.
//
// `p.x` is `(field p x)` and `t.0` is `(index t 0)`, with the index as it is
// written, so that one too large for a tuple shows as it is. A record literal
// lists its fields in the order they are written, `Point { x: 1.0, ..p }` being
// `(record Point (x 1.0) (.. p))`, and a field that is only a name, `{ x }`, is
// `(x x)`: the printer does not keep the difference between the two.
// `(1, true)` is `(tuple 1 true)`.
//
// A match lists its arms in the order they are written, and an arm is its pattern,
// its guard if it has one, and its body: `match x { 0 => 1\n n if n > 0 => n }` is
// `(match x (arm 0 1) (arm n (guard (> n 0)) n))`. A PAT is written as the
// program writes it (spec 5.3), with a single space after each comma and around
// each `|`: `_`, `x`, `0`, `-1`, `true`, `None`, `Some(x)`, `Point { x, y: p, .. }`,
// `(a, b)`, `[a, b, ..t]`, `A | B | C`; a field that is only a name is written as
// the name, and the pattern that could not be read as `(error)`. Where a pattern
// is a PAT of a `let`, it follows the name's place: `let (a, b) = e` is
// `(let (a, b) _ E)`.
//
// Parse produces every one of these forms; the printer's own tests build their
// trees by hand, so that they do not depend on the parser.
//
// PrintModule does not change m. Printing recurses once per level of the tree
// (a loop walks each list of children), so the depth of Go's call stack follows
// the depth of the tree. Parse bounds the nesting of what it reads (MaxNesting
// levels) and the operators and calls of one statement (MaxChain), so the depth
// of a tree it builds is bounded, though by the two together and not by either
// one alone: a statement at every level of nesting may have a chain of its own.
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
		pr.attrs(it.Attrs)
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
	case ItemType:
		pr.str("(")
		if it.Pub {
			pr.str("pub ")
		}
		pr.str("type ")
		pr.sym(it.Name)
		pr.attrs(it.Attrs)
		switch it.Form {
		case TypeFormSum:
			pr.str(" (sum")
			for _, v := range pr.m.Variants(it.Variants) {
				pr.str(" (")
				pr.sym(v.Name)
				pr.fields(v.Fields)
				pr.str(")")
			}
			pr.str(")")
		case TypeFormRecord:
			pr.str(" (record")
			pr.fields(it.Fields)
			pr.str(")")
		case TypeFormAlias:
			pr.str(" (alias ")
			pr.optType(it.Alias, true)
			pr.str(")")
		default:
			panic("unreachable")
		}
		pr.str(")")
	case ItemError:
		pr.str("(error)")
	default:
		panic("unreachable")
	}
}

// attrs appends the attribute list r, ` (attrs (NAME "arg"...)...)`, or nothing
// if it is empty.
func (pr *printer) attrs(r base.ListRange) {
	if r.Count == 0 {
		return
	}
	pr.str(" (attrs")
	for _, a := range pr.m.Attrs(r) {
		pr.str(" (")
		pr.sym(a.Name)
		for _, arg := range a.Args {
			pr.str(" " + strconv.Quote(arg))
		}
		pr.str(")")
	}
	pr.str(")")
}

// fields appends the fields in r, each as ` (NAME T)`, with `_` for the name of
// a positional field.
func (pr *printer) fields(r base.ListRange) {
	for _, f := range pr.m.Fields(r) {
		pr.str(" (")
		if f.HasName {
			pr.sym(f.Name)
		} else {
			pr.str("_")
		}
		pr.str(" ")
		pr.optType(f.Type, true)
		pr.str(")")
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
	case TypeTuple:
		pr.str("(tuple")
		for _, e := range pr.m.TypeList(t.Elems) {
			pr.str(" ")
			pr.optType(e, true)
		}
		pr.str(")")
	default:
		panic("unreachable")
	}
}

func (pr *printer) stmt(id StmtID) {
	s := pr.m.Stmts.Get(id)
	switch s.Kind {
	case StmtLet:
		pr.str("(let ")
		if s.HasPat {
			pr.pat(s.Pat)
		} else {
			pr.sym(s.Name)
		}
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
	case ExprFloat:
		pr.str(fmtnum.Float(e.Float))
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
	case ExprField:
		pr.str("(field ")
		pr.expr(e.Target)
		pr.str(" ")
		pr.sym(e.Name)
		pr.str(")")
	case ExprTupleIndex:
		pr.str("(index ")
		pr.expr(e.Target)
		pr.str(" ")
		pr.sym(e.Name)
		pr.str(")")
	case ExprRecord:
		pr.str("(record ")
		pr.sym(e.Name)
		for _, f := range pr.m.FieldInits(e.Fields) {
			pr.str(" (")
			pr.sym(f.Name)
			pr.str(" ")
			pr.expr(f.Value)
			pr.str(")")
		}
		if e.HasBase {
			pr.str(" (.. ")
			pr.expr(e.Base)
			pr.str(")")
		}
		pr.str(")")
	case ExprTuple:
		pr.str("(tuple")
		for _, el := range pr.m.ExprList(e.Args) {
			pr.str(" ")
			pr.expr(el)
		}
		pr.str(")")
	case ExprMatch:
		pr.str("(match ")
		pr.expr(e.Scrutinee)
		for _, arm := range pr.m.Arms(e.Arms) {
			pr.str(" (arm ")
			pr.pat(arm.Pat)
			if arm.HasGuard {
				pr.str(" (guard ")
				pr.expr(arm.Guard)
				pr.str(")")
			}
			pr.str(" ")
			pr.expr(arm.Body)
			pr.str(")")
		}
		pr.str(")")
	case ExprError:
		pr.str("(error)")
	default:
		panic("unreachable")
	}
}

// pat appends the pattern id as the program writes it. It recurses once for each
// level of the pattern, which Parse bounds (MaxPatternDepth), and walks each list
// of sub-patterns with a loop.
func (pr *printer) pat(id PatID) {
	p := pr.m.Pats.Get(id)
	switch p.Kind {
	case PatWildcard:
		pr.str("_")
	case PatBind:
		pr.sym(p.Name)
	case PatLit:
		switch p.Lit {
		case LitInt:
			pr.str(strconv.FormatInt(p.Int, 10))
		case LitBool:
			pr.str(strconv.FormatBool(p.Bool))
		default:
			panic("unreachable")
		}
	case PatCtor:
		pr.sym(p.Name)
		switch {
		case p.Record:
			pr.str(" {")
			sep := " "
			for _, f := range pr.m.FieldPats(p.Fields) {
				pr.str(sep)
				sep = ", "
				pr.sym(f.Name)
				if f.HasPat {
					pr.str(": ")
					pr.pat(f.Pat)
				}
			}
			if p.HasRest {
				pr.str(sep + "..")
			}
			pr.str(" }")
		case p.Args.Count > 0:
			pr.patList("(", p.Args, ")")
		}
	case PatTuple:
		pr.patList("(", p.Args, ")")
	case PatList:
		pr.str("[")
		for i, el := range pr.m.PatList(p.Args) {
			if i > 0 {
				pr.str(", ")
			}
			pr.pat(el)
		}
		if p.HasRest {
			if p.Args.Count > 0 {
				pr.str(", ")
			}
			pr.str("..")
			pr.pat(p.Rest)
		}
		pr.str("]")
	case PatOr:
		for i, alt := range pr.m.PatList(p.Args) {
			if i > 0 {
				pr.str(" | ")
			}
			// An alternative that is an or-pattern was written between parentheses
			// (`(A | B) | C`, `A | (B | C)`), and it is a node of its own. Without
			// them the dump would read as one node of three alternatives, which is
			// another tree, as it does for Core (see the printer there).
			if pr.m.Pats.Get(alt).Kind == PatOr {
				pr.str("(")
				pr.pat(alt)
				pr.str(")")
			} else {
				pr.pat(alt)
			}
		}
	case PatError:
		pr.str("(error)")
	default:
		panic("unreachable")
	}
}

// patList appends the patterns in r between open and closer, separated by `, `.
func (pr *printer) patList(open string, r base.ListRange, closer string) {
	pr.str(open)
	for i, el := range pr.m.PatList(r) {
		if i > 0 {
			pr.str(", ")
		}
		pr.pat(el)
	}
	pr.str(closer)
}
