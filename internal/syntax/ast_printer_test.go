package syntax_test

import (
	"math"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/syntax"
)

// tree builds a Module by hand. The parser reads only function signatures so
// far, so the printer's forms for expressions and statements cannot be reached
// through Parse yet; these tests reach them through the builder instead.
type tree struct {
	m  syntax.Module
	in *base.Interner
}

func newTree() *tree { return &tree{in: base.NewInterner()} }

func (b *tree) sym(s string) base.Symbol { return b.in.InternString(s) }

func (b *tree) expr(e syntax.Expr) syntax.ExprID { return b.m.Exprs.Push(e) }

func (b *tree) intLit(n int64) syntax.ExprID {
	return b.expr(syntax.Expr{Kind: syntax.ExprInt, Int: n})
}

func (b *tree) boolLit(v bool) syntax.ExprID {
	return b.expr(syntax.Expr{Kind: syntax.ExprBool, Bool: v})
}

func (b *tree) unit() syntax.ExprID { return b.expr(syntax.Expr{Kind: syntax.ExprUnit}) }

func (b *tree) name(s string) syntax.ExprID {
	return b.expr(syntax.Expr{Kind: syntax.ExprName, Name: b.sym(s)})
}

func (b *tree) call(callee syntax.ExprID, args ...syntax.ExprID) syntax.ExprID {
	first := uint32(len(b.m.ExprPool))
	b.m.ExprPool = append(b.m.ExprPool, args...)
	return b.expr(syntax.Expr{Kind: syntax.ExprCall, Callee: callee,
		Args: base.ListRange{First: first, Count: uint32(len(args))}})
}

func (b *tree) binary(op syntax.BinOp, lhs, rhs syntax.ExprID) syntax.ExprID {
	return b.expr(syntax.Expr{Kind: syntax.ExprBinary, Op: op, Lhs: lhs, Rhs: rhs})
}

func (b *tree) unary(op syntax.UnOp, operand syntax.ExprID) syntax.ExprID {
	return b.expr(syntax.Expr{Kind: syntax.ExprUnary, UnOp: op, Operand: operand})
}

// ifExpr builds an if. hasElse false leaves a valid expression in Else anyway,
// so that a printer that ignores the flag prints something extra.
func (b *tree) ifExpr(cond, then, els syntax.ExprID, hasElse bool) syntax.ExprID {
	return b.expr(syntax.Expr{Kind: syntax.ExprIf, Cond: cond, Then: then, Else: els, HasElse: hasElse})
}

// block builds a block. hasTail false leaves a valid expression in Tail anyway.
func (b *tree) block(stmts []syntax.StmtID, tail syntax.ExprID, hasTail bool) syntax.ExprID {
	first := uint32(len(b.m.StmtPool))
	b.m.StmtPool = append(b.m.StmtPool, stmts...)
	return b.expr(syntax.Expr{Kind: syntax.ExprBlock, Tail: tail, HasTail: hasTail,
		Stmts: base.ListRange{First: first, Count: uint32(len(stmts))}})
}

func (b *tree) named(s string) syntax.TypeExprID {
	return b.m.Types.Push(syntax.TypeExpr{Kind: syntax.TypeNamed, Name: b.sym(s)})
}

func (b *tree) letStmt(name string, ty syntax.TypeExprID, hasType bool, init syntax.ExprID) syntax.StmtID {
	return b.m.Stmts.Push(syntax.Stmt{Kind: syntax.StmtLet, Name: b.sym(name), Type: ty, HasType: hasType, Expr: init})
}

func (b *tree) exprStmt(e syntax.ExprID) syntax.StmtID {
	return b.m.Stmts.Push(syntax.Stmt{Kind: syntax.StmtExpr, Expr: e})
}

// printBody prints a module whose only item is `fn f()` with the expression
// that build returns as its body, and returns what PrintModule writes.
func printBody(build func(b *tree) syntax.ExprID) string {
	b := newTree()
	body := build(b)
	id := b.m.ItemNodes.Push(syntax.Item{Kind: syntax.ItemFn, Name: b.sym("f"), Body: body, HasBody: true})
	b.m.Items = []syntax.ItemID{id}
	return syntax.PrintModule(&b.m, b.in)
}

// wantBody fails the test unless the body that build returns prints as want.
func wantBody(t *testing.T, what string, build func(b *tree) syntax.ExprID, want string) {
	t.Helper()
	if got, full := printBody(build), "(fn f (params) (ret _) "+want+")\n"; got != full {
		t.Errorf("%s: printed\n%s\nwant\n%s", what, got, full)
	}
}

func TestPrinter(t *testing.T) {
	t.Run("expressions", func(t *testing.T) {
		rows := []struct {
			name  string
			build func(b *tree) syntax.ExprID
			want  string
		}{
			{"int", func(b *tree) syntax.ExprID { return b.intLit(42) }, "42"},
			{"zero", func(b *tree) syntax.ExprID { return b.intLit(0) }, "0"},
			{"negative int", func(b *tree) syntax.ExprID { return b.intLit(-7) }, "-7"},
			{"largest int", func(b *tree) syntax.ExprID { return b.intLit(math.MaxInt64) }, "9223372036854775807"},
			{"smallest int", func(b *tree) syntax.ExprID { return b.intLit(math.MinInt64) }, "-9223372036854775808"},
			{"true", func(b *tree) syntax.ExprID { return b.boolLit(true) }, "true"},
			{"false", func(b *tree) syntax.ExprID { return b.boolLit(false) }, "false"},
			{"unit", func(b *tree) syntax.ExprID { return b.unit() }, "()"},
			{"lower name", func(b *tree) syntax.ExprID { return b.name("fib") }, "fib"},
			{"upper name", func(b *tree) syntax.ExprID { return b.name("Foo") }, "Foo"},
			{"call without arguments", func(b *tree) syntax.ExprID { return b.call(b.name("f")) }, "(call f)"},
			{"call with arguments", func(b *tree) syntax.ExprID {
				return b.call(b.name("f"), b.intLit(1), b.call(b.name("g"), b.intLit(2)), b.name("x"))
			}, "(call f 1 (call g 2) x)"},
			{"call of a call", func(b *tree) syntax.ExprID {
				return b.call(b.call(b.name("f"), b.intLit(1)), b.intLit(2))
			}, "(call (call f 1) 2)"},
			{"operands in order", func(b *tree) syntax.ExprID {
				return b.binary(syntax.BinSub, b.name("a"), b.binary(syntax.BinMul, b.name("b"), b.name("c")))
			}, "(- a (* b c))"},
			{"neg", func(b *tree) syntax.ExprID { return b.unary(syntax.Neg, b.intLit(1)) }, "(neg 1)"},
			{"not", func(b *tree) syntax.ExprID { return b.unary(syntax.Not, b.name("a")) }, "(not a)"},
			{"if with else", func(b *tree) syntax.ExprID {
				return b.ifExpr(b.name("a"), b.intLit(1), b.intLit(2), true)
			}, "(if a 1 2)"},
			{"if without else", func(b *tree) syntax.ExprID {
				return b.ifExpr(b.name("a"), b.intLit(1), b.intLit(2), false)
			}, "(if a 1)"},
			{"else if", func(b *tree) syntax.ExprID {
				inner := b.ifExpr(b.name("b"), b.intLit(2), b.intLit(3), true)
				return b.ifExpr(b.name("a"), b.intLit(1), inner, true)
			}, "(if a 1 (if b 2 3))"},
		}
		// Every binary operator, with the text written here and not taken from
		// BinOp.String: the printer must put each operator's own symbol first.
		ops := []struct {
			op   syntax.BinOp
			text string
		}{
			{syntax.BinAdd, "+"}, {syntax.BinSub, "-"}, {syntax.BinMul, "*"}, {syntax.BinDiv, "/"},
			{syntax.BinRem, "%"}, {syntax.BinEq, "=="}, {syntax.BinNe, "!="}, {syntax.BinLt, "<"},
			{syntax.BinLe, "<="}, {syntax.BinGt, ">"}, {syntax.BinGe, ">="}, {syntax.BinAnd, "&&"},
			{syntax.BinOr, "||"},
		}
		for _, row := range rows {
			wantBody(t, row.name, row.build, row.want)
		}
		for _, row := range ops {
			wantBody(t, "operator "+row.text, func(b *tree) syntax.ExprID {
				return b.binary(row.op, b.name("a"), b.name("b"))
			}, "("+row.text+" a b)")
		}
	})
	t.Run("statements_and_blocks", func(t *testing.T) {
		rows := []struct {
			name  string
			build func(b *tree) syntax.ExprID
			want  string
		}{
			{"empty block", func(b *tree) syntax.ExprID { return b.block(nil, 0, false) }, "(block)"},
			{"tail only", func(b *tree) syntax.ExprID { return b.block(nil, b.name("x"), true) }, "(block x)"},
			// With HasTail false the Tail field is garbage, here a real
			// expression, and must not be printed.
			{"statements only", func(b *tree) syntax.ExprID {
				return b.block([]syntax.StmtID{b.exprStmt(b.call(b.name("f")))}, b.name("junk"), false)
			}, "(block (expr (call f)))"},
			{"statements and tail", func(b *tree) syntax.ExprID {
				stmts := []syntax.StmtID{
					b.letStmt("x", 0, false, b.intLit(1)),
					b.letStmt("y", b.named("Int"), true, b.intLit(2)),
					b.exprStmt(b.call(b.name("f"))),
				}
				return b.block(stmts, b.name("x"), true)
			}, "(block (let x _ 1) (let y Int 2) (expr (call f)) x)"},
			// A let without a type has Type 0, which is the id of a real type
			// when one was added first: HasType decides, not the id.
			{"let without a type", func(b *tree) syntax.ExprID {
				b.named("Int")
				return b.block([]syntax.StmtID{b.letStmt("x", 0, false, b.intLit(1))}, 0, false)
			}, "(block (let x _ 1))"},
			{"let with the unit type", func(b *tree) syntax.ExprID {
				unit := b.m.Types.Push(syntax.TypeExpr{Kind: syntax.TypeUnit})
				return b.block([]syntax.StmtID{b.letStmt("u", unit, true, b.unit())}, 0, false)
			}, "(block (let u () ()))"},
			{"nested blocks", func(b *tree) syntax.ExprID {
				inner := b.block([]syntax.StmtID{b.exprStmt(b.intLit(1))}, b.intLit(2), true)
				outer := b.block([]syntax.StmtID{b.exprStmt(inner)}, 0, false)
				return b.ifExpr(b.boolLit(true), outer, 0, false)
			}, "(if true (block (expr (block (expr 1) 2))))"},
		}
		for _, row := range rows {
			wantBody(t, row.name, row.build, row.want)
		}
	})
	t.Run("error_nodes", func(t *testing.T) {
		wantBody(t, "expression", func(b *tree) syntax.ExprID {
			return b.expr(syntax.Expr{Kind: syntax.ExprError})
		}, "(error)")
		wantBody(t, "operand", func(b *tree) syntax.ExprID {
			return b.binary(syntax.BinAdd, b.intLit(1), b.expr(syntax.Expr{Kind: syntax.ExprError}))
		}, "(+ 1 (error))")
		wantBody(t, "statement", func(b *tree) syntax.ExprID {
			bad := b.m.Stmts.Push(syntax.Stmt{Kind: syntax.StmtError})
			return b.block([]syntax.StmtID{bad, b.exprStmt(b.intLit(1))}, 0, false)
		}, "(block (error) (expr 1))")
		wantBody(t, "type", func(b *tree) syntax.ExprID {
			bad := b.m.Types.Push(syntax.TypeExpr{Kind: syntax.TypeError})
			return b.block([]syntax.StmtID{b.letStmt("x", bad, true, b.intLit(1))}, 0, false)
		}, "(block (let x (error) 1))")

		// An error item prints as (error) on a line of its own, between items
		// that print normally; and a parameter whose type is an error and a
		// return type that is one print theirs in place.
		b := newTree()
		bad := b.m.Types.Push(syntax.TypeExpr{Kind: syntax.TypeError})
		b.m.ParamPool = []syntax.Param{{Name: b.sym("x"), Type: bad, HasType: true}}
		first := b.m.ItemNodes.Push(syntax.Item{Kind: syntax.ItemFn, Name: b.sym("a"),
			Params: base.ListRange{First: 0, Count: 1}, Ret: bad, HasRet: true})
		broken := b.m.ItemNodes.Push(syntax.Item{Kind: syntax.ItemError})
		last := b.m.ItemNodes.Push(syntax.Item{Kind: syntax.ItemFn, Name: b.sym("b")})
		b.m.Items = []syntax.ItemID{first, broken, last}
		want := "(fn a (params (x (error))) (ret (error)) none)\n(error)\n(fn b (params) (ret _) none)\n"
		if got := syntax.PrintModule(&b.m, b.in); got != want {
			t.Errorf("error item: printed\n%s\nwant\n%s", got, want)
		}
	})
}
