package syntax_test

import (
	"math"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/syntax"
)

// These build the nodes of chapter 11, session 4 by hand, as the printer's other
// tests do, and fix how `ki emit ast` writes them.

func (b *tree) field(target syntax.ExprID, name string) syntax.ExprID {
	return b.expr(syntax.Expr{Kind: syntax.ExprField, Target: target, Name: b.sym(name)})
}

// tupleIndex builds `target.digits`: Index is what a program that wrote more than
// a uint32 holds, and the printer must show the digits.
func (b *tree) tupleIndex(target syntax.ExprID, digits string, index uint32) syntax.ExprID {
	return b.expr(syntax.Expr{Kind: syntax.ExprTupleIndex, Target: target, Name: b.sym(digits), Index: index})
}

func (b *tree) tuple(elems ...syntax.ExprID) syntax.ExprID {
	return b.expr(syntax.Expr{Kind: syntax.ExprTuple, Args: base.AppendRange(&b.m.ExprPool, elems)})
}

// record builds `name { fields… }` with the fields given as name, value pairs and
// `..base` if hasBase. A base that hasBase says is not there stays in the node, as
// ifExpr does with its else, so that a printer that ignores the flag prints it.
func (b *tree) record(name string, baseExpr syntax.ExprID, hasBase bool, fields ...any) syntax.ExprID {
	var inits []syntax.FieldInit
	for i := 0; i < len(fields); i += 2 {
		inits = append(inits, syntax.FieldInit{Name: b.sym(fields[i].(string)), Value: fields[i+1].(syntax.ExprID)})
	}
	return b.expr(syntax.Expr{Kind: syntax.ExprRecord, Name: b.sym(name), Fields: base.AppendRange(&b.m.FieldInitPool, inits),
		Base: baseExpr, HasBase: hasBase})
}

func TestPrinterData(t *testing.T) {
	t.Run("expressions", func(t *testing.T) {
		rows := []struct {
			name  string
			build func(b *tree) syntax.ExprID
			want  string
		}{
			{"field", func(b *tree) syntax.ExprID { return b.field(b.name("p"), "x") }, "(field p x)"},
			{"field of a field", func(b *tree) syntax.ExprID { return b.field(b.field(b.name("p"), "a"), "b") }, "(field (field p a) b)"},
			{"index", func(b *tree) syntax.ExprID { return b.tupleIndex(b.name("t"), "0", 0) }, "(index t 0)"},
			{"an index is shown as written", func(b *tree) syntax.ExprID {
				return b.tupleIndex(b.name("t"), "99999999999", math.MaxUint32)
			}, "(index t 99999999999)"},
			{"tuple", func(b *tree) syntax.ExprID { return b.tuple(b.intLit(1), b.boolLit(true)) }, "(tuple 1 true)"},
			{"nested tuple", func(b *tree) syntax.ExprID {
				return b.tuple(b.tuple(b.intLit(1), b.intLit(2)), b.unit(), b.name("x"))
			}, "(tuple (tuple 1 2) () x)"},
			{"record", func(b *tree) syntax.ExprID {
				return b.record("Point", 0, false, "x", b.floatLit(1), "y", b.name("y"))
			}, "(record Point (x 1.0) (y y))"},
			{"record with no field", func(b *tree) syntax.ExprID { return b.record("Unit", 0, false) }, "(record Unit)"},
			{"record with a base", func(b *tree) syntax.ExprID {
				return b.record("Point", b.name("p"), true, "x", b.floatLit(3))
			}, "(record Point (x 3.0) (.. p))"},
			{"a base that is not there is not printed", func(b *tree) syntax.ExprID {
				return b.record("Point", b.name("p"), false, "x", b.floatLit(3))
			}, "(record Point (x 3.0))"},
			{"record in a call", func(b *tree) syntax.ExprID {
				return b.call(b.name("f"), b.record("P", 0, false, "a", b.intLit(1)), b.field(b.name("q"), "a"))
			}, "(call f (record P (a 1)) (field q a))"},
		}
		for _, row := range rows {
			wantBody(t, row.name, row.build, row.want)
		}
	})

	t.Run("tuple_types", func(t *testing.T) {
		b := newTree()
		elems := func(ids ...syntax.TypeExprID) base.ListRange { return base.AppendRange(&b.m.TypePool, ids) }
		unit := b.m.Types.Push(syntax.TypeExpr{Kind: syntax.TypeUnit})
		inner := b.m.Types.Push(syntax.TypeExpr{Kind: syntax.TypeTuple, Elems: elems(b.named("Bool"), unit)})
		pair := b.m.Types.Push(syntax.TypeExpr{Kind: syntax.TypeTuple, Elems: elems(b.named("Int"), inner)})
		body := b.block([]syntax.StmtID{b.letStmt("x", pair, true, b.name("y"))}, 0, false)
		id := b.m.ItemNodes.Push(syntax.Item{Kind: syntax.ItemFn, Name: b.sym("f"), Body: body, HasBody: true})
		b.m.Items = []syntax.ItemID{id}
		const want = "(fn f (params) (ret _) (block (let x (tuple Int (tuple Bool ())) y)))\n"
		if got := syntax.PrintModule(&b.m, b.in); got != want {
			t.Errorf("printed\n%s\nwant\n%s", got, want)
		}
	})
}
