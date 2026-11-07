package syntax_test

import (
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/syntax"
)

// These build the nodes of chapter 12, session 1 by hand, as the printer's other
// tests do, and fix how `ki emit ast` writes patterns, arms and matches.

func (b *tree) pat(p syntax.Pat) syntax.PatID { return b.m.Pats.Push(p) }

func (b *tree) bindPat(name string) syntax.PatID {
	return b.pat(syntax.Pat{Kind: syntax.PatBind, Name: b.sym(name)})
}

func (b *tree) intPat(n int64) syntax.PatID {
	return b.pat(syntax.Pat{Kind: syntax.PatLit, Lit: syntax.LitInt, Int: n})
}

func (b *tree) patRange(ids ...syntax.PatID) base.ListRange {
	return base.AppendRange(&b.m.PatPool, ids)
}

// ctorPat builds `name(args…)`, or just `name` for no arguments.
func (b *tree) ctorPat(name string, args ...syntax.PatID) syntax.PatID {
	return b.pat(syntax.Pat{Kind: syntax.PatCtor, Name: b.sym(name), Args: b.patRange(args...)})
}

// matchOf builds `match scrutinee { arms… }`.
func (b *tree) matchOf(scrutinee syntax.ExprID, arms ...syntax.MatchArm) syntax.ExprID {
	return b.expr(syntax.Expr{Kind: syntax.ExprMatch, Scrutinee: scrutinee, Arms: base.AppendRange(&b.m.ArmPool, arms)})
}

func TestPrinterPat(t *testing.T) {
	t.Run("patterns", func(t *testing.T) {
		rows := []struct {
			name  string
			build func(b *tree) syntax.PatID
			want  string
		}{
			{"wildcard", func(b *tree) syntax.PatID { return b.pat(syntax.Pat{Kind: syntax.PatWildcard}) }, "_"},
			{"variable", func(b *tree) syntax.PatID { return b.bindPat("x") }, "x"},
			{"zero is an Int", func(b *tree) syntax.PatID {
				// A Bool payload left set must not show: the literal says which payload counts.
				return b.pat(syntax.Pat{Kind: syntax.PatLit, Lit: syntax.LitInt, Int: 0, Bool: true})
			}, "0"},
			{"negative", func(b *tree) syntax.PatID { return b.intPat(-5) }, "-5"},
			{"several digits", func(b *tree) syntax.PatID { return b.intPat(1234567890) }, "1234567890"},
			{"false is a Bool", func(b *tree) syntax.PatID {
				return b.pat(syntax.Pat{Kind: syntax.PatLit, Lit: syntax.LitBool, Bool: false, Int: 7})
			}, "false"},
			{"true", func(b *tree) syntax.PatID {
				return b.pat(syntax.Pat{Kind: syntax.PatLit, Lit: syntax.LitBool, Bool: true})
			}, "true"},
			{"constructor without arguments", func(b *tree) syntax.PatID { return b.ctorPat("None") }, "None"},
			{"constructor", func(b *tree) syntax.PatID { return b.ctorPat("Some", b.bindPat("x")) }, "Some(x)"},
			{"constructor with two arguments", func(b *tree) syntax.PatID {
				return b.ctorPat("Rect", b.bindPat("w"), b.intPat(1))
			}, "Rect(w, 1)"},
			{"record with no field", func(b *tree) syntax.PatID {
				return b.pat(syntax.Pat{Kind: syntax.PatCtor, Name: b.sym("P"), Record: true})
			}, "P { }"},
			{"record with a pun and a pattern", func(b *tree) syntax.PatID {
				fields := base.AppendRange(&b.m.FieldPatPool, []syntax.FieldPat{
					{Name: b.sym("x")},
					{Name: b.sym("y"), Pat: b.intPat(0), HasPat: true},
				})
				return b.pat(syntax.Pat{Kind: syntax.PatCtor, Name: b.sym("P"), Record: true, Fields: fields})
			}, "P { x, y: 0 }"},
			{"a pun does not show the pattern that it does not have", func(b *tree) syntax.PatID {
				// HasPat false leaves a valid pattern in the field, as ifExpr does with its else.
				fields := base.AppendRange(&b.m.FieldPatPool, []syntax.FieldPat{{Name: b.sym("x"), Pat: b.intPat(9)}})
				return b.pat(syntax.Pat{Kind: syntax.PatCtor, Name: b.sym("P"), Record: true, Fields: fields})
			}, "P { x }"},
			{"record with a rest", func(b *tree) syntax.PatID {
				fields := base.AppendRange(&b.m.FieldPatPool, []syntax.FieldPat{{Name: b.sym("x")}})
				return b.pat(syntax.Pat{Kind: syntax.PatCtor, Name: b.sym("P"), Record: true, Fields: fields, HasRest: true})
			}, "P { x, .. }"},
			{"record that is only a rest", func(b *tree) syntax.PatID {
				return b.pat(syntax.Pat{Kind: syntax.PatCtor, Name: b.sym("P"), Record: true, HasRest: true})
			}, "P { .. }"},
			{"tuple", func(b *tree) syntax.PatID {
				return b.pat(syntax.Pat{Kind: syntax.PatTuple, Args: b.patRange(b.bindPat("a"), b.ctorPat("None"))})
			}, "(a, None)"},
			{"empty list", func(b *tree) syntax.PatID { return b.pat(syntax.Pat{Kind: syntax.PatList}) }, "[]"},
			{"list", func(b *tree) syntax.PatID {
				return b.pat(syntax.Pat{Kind: syntax.PatList, Args: b.patRange(b.bindPat("a"), b.bindPat("b"))})
			}, "[a, b]"},
			{"list with a rest", func(b *tree) syntax.PatID {
				rest := b.bindPat("t")
				return b.pat(syntax.Pat{Kind: syntax.PatList, Args: b.patRange(b.bindPat("a")), Rest: rest, HasRest: true})
			}, "[a, ..t]"},
			{"list that is only a rest", func(b *tree) syntax.PatID {
				rest := b.pat(syntax.Pat{Kind: syntax.PatWildcard})
				return b.pat(syntax.Pat{Kind: syntax.PatList, Rest: rest, HasRest: true})
			}, "[.._]"},
			{"a rest that is not there does not show", func(b *tree) syntax.PatID {
				return b.pat(syntax.Pat{Kind: syntax.PatList, Args: b.patRange(b.bindPat("a")), Rest: b.bindPat("t")})
			}, "[a]"},
			{"or-pattern", func(b *tree) syntax.PatID {
				return b.pat(syntax.Pat{Kind: syntax.PatOr, Args: b.patRange(b.intPat(1), b.intPat(2), b.intPat(3))})
			}, "1 | 2 | 3"},
			{"nested", func(b *tree) syntax.PatID {
				inner := b.pat(syntax.Pat{Kind: syntax.PatTuple, Args: b.patRange(b.bindPat("a"), b.intPat(0))})
				return b.ctorPat("Some", inner, b.pat(syntax.Pat{Kind: syntax.PatError}))
			}, "Some((a, 0), (error))"},
			{"error", func(b *tree) syntax.PatID { return b.pat(syntax.Pat{Kind: syntax.PatError}) }, "(error)"},
		}
		for _, row := range rows {
			b := newTree()
			arm := syntax.MatchArm{Pat: row.build(b), Body: b.intLit(1)}
			body := b.matchOf(b.name("x"), arm)
			id := b.m.ItemNodes.Push(syntax.Item{Kind: syntax.ItemFn, Name: b.sym("f"), Body: body, HasBody: true})
			b.m.Items = []syntax.ItemID{id}
			want := "(fn f (params) (ret _) (match x (arm " + row.want + " 1)))\n"
			if got := syntax.PrintModule(&b.m, b.in); got != want {
				t.Errorf("%s: printed\n%s\nwant\n%s", row.name, got, want)
			}
		}
	})

	t.Run("matches_and_lets", func(t *testing.T) {
		rows := []struct {
			name  string
			build func(b *tree) syntax.ExprID
			want  string
		}{
			{"no arms", func(b *tree) syntax.ExprID { return b.matchOf(b.name("x")) }, "(match x)"},
			{"two arms", func(b *tree) syntax.ExprID {
				return b.matchOf(b.name("x"),
					syntax.MatchArm{Pat: b.intPat(0), Body: b.intLit(1)},
					syntax.MatchArm{Pat: b.bindPat("n"), Body: b.name("n")})
			}, "(match x (arm 0 1) (arm n n))"},
			{"guard", func(b *tree) syntax.ExprID {
				g := b.binary(syntax.BinGt, b.name("n"), b.intLit(0))
				return b.matchOf(b.name("x"), syntax.MatchArm{Pat: b.bindPat("n"), Guard: g, HasGuard: true, Body: b.name("n")})
			}, "(match x (arm n (guard (> n 0)) n))"},
			{"a guard that is not there does not show", func(b *tree) syntax.ExprID {
				return b.matchOf(b.name("x"), syntax.MatchArm{Pat: b.bindPat("n"), Guard: b.boolLit(true), Body: b.name("n")})
			}, "(match x (arm n n))"},
			{"match in an arm", func(b *tree) syntax.ExprID {
				inner := b.matchOf(b.name("y"), syntax.MatchArm{Pat: b.ctorPat("A"), Body: b.intLit(1)})
				return b.matchOf(b.name("x"), syntax.MatchArm{Pat: b.ctorPat("B"), Body: inner})
			}, "(match x (arm B (match y (arm A 1))))"},
			{"let with a pattern", func(b *tree) syntax.ExprID {
				// The name that a let with a pattern does not have stays in the node, so
				// that a printer that prefers it shows `p` and not the pattern.
				pat := b.pat(syntax.Pat{Kind: syntax.PatTuple, Args: b.patRange(b.bindPat("a"), b.bindPat("b"))})
				let := b.m.Stmts.Push(syntax.Stmt{Kind: syntax.StmtLet, Name: b.sym("p"), HasPat: true, Pat: pat, Expr: b.name("t")})
				return b.block([]syntax.StmtID{let}, 0, false)
			}, "(block (let (a, b) _ t))"},
			{"let with a pattern and a type", func(b *tree) syntax.ExprID {
				pat := b.pat(syntax.Pat{Kind: syntax.PatWildcard})
				let := b.m.Stmts.Push(syntax.Stmt{Kind: syntax.StmtLet, HasPat: true, Pat: pat, Type: b.named("Int"), HasType: true, Expr: b.intLit(1)})
				return b.block([]syntax.StmtID{let}, 0, false)
			}, "(block (let _ Int 1))"},
			{"let with a name does not show the pattern that it does not have", func(b *tree) syntax.ExprID {
				let := b.m.Stmts.Push(syntax.Stmt{Kind: syntax.StmtLet, Name: b.sym("x"), Pat: b.intPat(5), Expr: b.intLit(1)})
				return b.block([]syntax.StmtID{let}, 0, false)
			}, "(block (let x _ 1))"},
		}
		for _, row := range rows {
			wantBody(t, row.name, row.build, row.want)
		}
	})
}
