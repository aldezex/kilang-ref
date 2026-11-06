package core_test

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here are about how records, field reads and tuples become Core: a
// Con of a constructor and a Proj by index. They reuse elabWant, elabFun and
// elabShape from elaborate_test.go, and lintProg, lintDeepCon and lintCheck from
// lint_test.go.

// projChain makes `t.0.0…0`, depth reads of field 0 from the parameter t, as the
// body of the function `deep`. The parser bounds how many a source writes
// (E0205), and a Program built by hand has no bound.
func projChain(depth int) (*core.Program, *base.Interner, core.ExprID) {
	in := base.NewInterner()
	b, p := newBuilder()
	t := p.Vars.Push(core.Var{Name: in.InternString("t"), Rep: core.RepVal, Slot: 0, Span: span(0, 1)})
	e := b.VarRef(span(0, 1), t)
	for range depth {
		e = b.Proj(span(0, 1), e, 0)
	}
	p.Funs.Push(core.Fun{Name: in.InternString("deep"), Params: []core.VarID{t}, Body: e, HasBody: true, FrameSize: 1, UserModule: true})
	return p, in, e
}

func TestDataCore(t *testing.T) {
	t.Run("record_literal_and_field_are_a_con_and_a_proj", func(t *testing.T) {
		// A literal whose fields come in the order they are declared is a Con of the
		// record's constructor, with the values as they are written; a pun is the
		// variable; `p.y` is a Proj of the number of the field. No Let is added.
		c := testsupport.Compile(t, "type Point { x: Float, y: Float }\nfn lit() -> Point { Point { x: 1.0, y: 2.0 } }\n"+
			"fn pun(x: Float, y: Float) -> Point { Point { x, y } }\nfn get(p: Point) -> Float { p.y }\n"+
			"fn first(p: Point) -> Float { p.x }\nfn nested(p: Point) -> Point { Point { x: p.y, y: p.x } }\n"+
			"fn viaVar(q) -> Float {\n  let p = Point { x: 1.0, y: 2.0 }\n  let w = if true { q } else { p }\n  q.y\n}\n")
		elabWant(t, c, "lit", "Point(1.0, 2.0)")
		elabWant(t, c, "pun", "Point(x@0, y@1)")
		elabWant(t, c, "get", "p@0.1")
		elabWant(t, c, "first", "p@0.0")
		elabWant(t, c, "nested", "Point(p@0.1, p@0.0)")
		// The target may have a variable for its type, bound by an earlier line.
		if got := elabShape(c, elabFun(t, c, "viaVar").Body); !strings.HasSuffix(got, "; q@0.1") {
			t.Errorf("a field of a parameter that an earlier line made a Point elaborates to %s, want a read of field 1 of q at the end", got)
		}
	})

	t.Run("update_with_none_one_and_all_the_fields", func(t *testing.T) {
		// An update is a new Con. Each value is bound to a variable named as its
		// field, in the order written, then the base to `base`, and the Con takes the
		// variable of each field it was given and a Proj of the base for each it was
		// not. The base is read once, however many fields come from it.
		c := testsupport.Compile(t, "type Point { x: Float, y: Float }\nfn none(o: Point) -> Point { Point { ..o } }\n"+
			"fn one(o: Point) -> Point { Point { x: 3.0, ..o } }\nfn other(o: Point) -> Point { Point { y: 3.0, ..o } }\n"+
			"fn all(o: Point) -> Point { Point { x: 3.0, y: 4.0, ..o } }\nfn swapped(o: Point) -> Point { Point { y: 4.0, x: 3.0, ..o } }\n")
		elabWant(t, c, "none", "let base@1 = o@0; Point(base@1.0, base@1.1)")
		elabWant(t, c, "one", "let x@1 = 3.0; let base@2 = o@0; Point(x@1, base@2.1)")
		elabWant(t, c, "other", "let y@1 = 3.0; let base@2 = o@0; Point(base@2.0, y@1)")
		elabWant(t, c, "all", "let x@1 = 3.0; let y@2 = 4.0; let base@3 = o@0; Point(x@1, y@2)")
		elabWant(t, c, "swapped", "let y@1 = 4.0; let x@2 = 3.0; let base@3 = o@0; Point(x@2, y@1)")
		for _, name := range []string{"none", "one", "other", "all", "swapped"} {
			if n := strings.Count(elabShape(c, elabFun(t, c, name).Body), "= o@0"); n != 1 {
				t.Errorf("%s: the base is read %d times, want once", name, n)
			}
		}
	})

	t.Run("values_keep_the_order_they_are_written_in", func(t *testing.T) {
		// A literal that writes the fields in another order than they are declared
		// in binds each value first, in the order written, so the Con does not
		// evaluate them in the order of the declaration; one in order needs no
		// binding. The base comes after the values, as an argument after the ones
		// before it.
		c := testsupport.Compile(t, "type R { a: Int, b: Int, c: Int }\nfn side(n: Int) -> Int { n }\n"+
			"fn inOrder() -> R { R { a: side(1), b: side(2), c: side(3) } }\n"+
			"fn outOfOrder() -> R { R { c: side(1), a: side(2), b: side(3) } }\n"+
			"fn lastInPlace() -> R { R { b: side(1), a: side(2), c: side(3) } }\n"+
			"fn partly(o: R) -> R { R { c: side(1), a: side(2), ..o } }\n"+
			"fn onlyBase(o: R, p: R) -> R { R { b: side(1), ..side2(o, p) } }\nfn side2(x: R, y: R) -> R { y }\n")
		elabWant(t, c, "inOrder", "R(@side(1), @side(2), @side(3))")
		elabWant(t, c, "outOfOrder", "let c@0 = @side(1); let a@1 = @side(2); let b@2 = @side(3); R(a@1, b@2, c@0)")
		elabWant(t, c, "lastInPlace", "let b@0 = @side(1); let a@1 = @side(2); let c@2 = @side(3); R(a@1, b@0, c@2)")
		elabWant(t, c, "partly", "let c@1 = @side(1); let a@2 = @side(2); let base@3 = o@0; R(a@2, base@3.1, c@1)")
		elabWant(t, c, "onlyBase", "let b@2 = @side(1); let base@3 = @side2(o@0, p@1); R(base@3.0, b@2, base@3.2)")
	})

	t.Run("tuples_are_cons_of_one_constructor_for_each_arity", func(t *testing.T) {
		// A tuple is a Con of the tuple constructor of its arity, made when the
		// elaboration first meets that arity and used by every tuple of it: it has no
		// name and no tag, it is marked Tuple, and it comes after the constructors of
		// the declarations and the records.
		c := testsupport.Compile(t, "type Shape { Circle(r: Float), None }\ntype Point { x: Float }\n"+
			"fn three() -> (Int, Bool, Float) { (1, true, 2.0) }\nfn two() -> (Int, Bool) { (1, true) }\nfn again() -> (Int, Bool) { (2, false) }\n"+
			"fn nested() -> ((Int, Int), (Int, Int, Int)) { ((1, 2), (3, 4, 5)) }\n"+
			"fn eight() -> (Int, Int, Int, Int, Int, Int, Int, Int) { (1, 2, 3, 4, 5, 6, 7, 8) }\n")
		elabWant(t, c, "three", "(1, true, 2.0)")
		elabWant(t, c, "two", "(1, true)")
		elabWant(t, c, "again", "(2, false)")
		elabWant(t, c, "nested", "((1, 2), (3, 4, 5))")
		elabWant(t, c, "eight", "(1, 2, 3, 4, 5, 6, 7, 8)")
		var arities []uint32
		for i, ctor := range c.Core.Ctors.All() {
			if i < 3 { // Circle, None, and the record Point
				if ctor.Tuple {
					t.Errorf("constructor %d is a tuple, want the declared ones first", i)
				}
				continue
			}
			if !ctor.Tuple || ctor.Name != base.NoSymbol || ctor.Tag != 0 || ctor.Record || ctor.Fields != nil {
				t.Errorf("constructor %d is %+v, want a tuple with no name, no tag and no fields", i, ctor)
			}
			arities = append(arities, ctor.Arity)
		}
		// In the order of the first use: three, two (again shares it), then eight; the
		// pair and the triple of `nested` are those two arities already.
		if want := []uint32{3, 2, 8}; !slices.Equal(arities, want) {
			t.Errorf("the tuple constructors have the arities %v, want %v: one for each arity, in the order they were met", arities, want)
		}
		rec := c.Core.Ctors.Get(2)
		if !rec.Record || rec.Tuple || c.Session.Interner.String(rec.Name) != "Point" || rec.Arity != 1 {
			t.Errorf("constructor 2 is %+v, want the record Point, which no variant declares", rec)
		}
	})

	t.Run("tuple_index_is_a_proj", func(t *testing.T) {
		// `t.0` is a Proj of 0; `n.0.1` is two Projs, the second of the first; a
		// field of an element is a Proj of a Proj.
		c := testsupport.Compile(t, "type Point { x: Float, y: Float }\nfn first(t: (Int, Bool)) -> Int { t.0 }\n"+
			"fn second(t: (Int, Bool)) -> Bool { t.1 }\nfn deep(n: ((Int, Int), Bool)) -> Int { n.0.1 }\n"+
			"fn field(t: (Int, Point)) -> Float { t.1.y }\nfn built() -> Int { (1, 2).1 }\n")
		elabWant(t, c, "first", "t@0.0")
		elabWant(t, c, "second", "t@0.1")
		elabWant(t, c, "deep", "n@0.0.1")
		elabWant(t, c, "field", "t@0.1.1")
		elabWant(t, c, "built", "(1, 2).1")
	})

	t.Run("nodes_have_the_span_of_their_syntax", func(t *testing.T) {
		// A tuple and a literal are at the whole of what is written, a read at the
		// whole access; the Lets of an update are at the field and its value and at the
		// base, the variable of a given field at the field's name, and the variable of
		// the base, and the Proj that takes a field from it, where the literal is.
		c := testsupport.Compile(t, "type Point { x: Float, y: Float }\nfn a() -> (Int, Bool) { (1, true) }\nfn b(t: (Int, Int)) -> Int { t.0 }\n"+
			"fn c(p: Point) -> Float { p.y }\nfn d() -> Point { Point { x: 1.0, y: 2.0 } }\nfn e(o: Point) -> Point { Point { y: 4.0, ..o } }\n"+
			"fn f() -> Int { { let a = (1, 2)\n a }.0 }\n")
		kinds := map[core.ExprKind]string{core.LitInt: "int", core.LitFloat: "float", core.LitBool: "bool", core.VarRef: "var", core.Let: "let", core.Con: "con", core.Proj: "proj"}
		for _, row := range []struct {
			fn   string
			want []string
		}{
			{"a", []string{"con@(1, true)", "int@1", "bool@true"}},
			{"b", []string{"proj@t.0", "var@t"}},
			{"c", []string{"proj@p.y", "var@p"}},
			{"d", []string{"con@Point { x: 1.0, y: 2.0 }", "float@1.0", "float@2.0"}},
			{"e", []string{"let@y: 4.0", "float@4.0", "let@o", "var@o", "con@Point { y: 4.0, ..o }", "proj@Point { y: 4.0, ..o }", "var@o", "var@y"}},
			{"f", []string{"proj@{ let a = (1, 2)\n a }.0", "let@let a = (1, 2)", "con@(1, 2)", "int@1", "int@2", "var@a"}},
		} {
			var got []string
			for sub := range c.Core.Subexprs(elabFun(t, c, row.fn).Body) {
				x := c.Core.Exprs.Get(sub)
				got = append(got, kinds[x.Kind]+"@"+elabText(c, x.Span))
			}
			if !slices.Equal(got, row.want) {
				t.Errorf("fn %s: the nodes are %q, want %q", row.fn, got, row.want)
			}
		}
		// The variables that an update makes are bound where the program wrote what
		// they hold: a given field at its name, the base at the base.
		var bound []string
		for sub := range c.Core.Subexprs(elabFun(t, c, "e").Body) {
			if x := c.Core.Exprs.Get(sub); x.Kind == core.Let {
				v := c.Core.Vars.Get(x.Var)
				bound = append(bound, c.Session.Interner.String(v.Name)+"@"+elabText(c, v.Span))
			}
		}
		if want := []string{"y@y", "base@o"}; !slices.Equal(bound, want) {
			t.Errorf("the variables of the update are bound at %q, want %q", bound, want)
		}
	})

	t.Run("print_of_reads", func(t *testing.T) {
		// An index is written in decimal, however large; a read whose target is a Let
		// writes the Let in parentheses, with its body one level further in, as for a
		// call, and the read after the `)`. Without them the read would be one of the
		// body, and two trees would print alike: the read of a Let, and a Let whose
		// last expression is a read.
		const big = "(Int, Int, Int, Int, Int, Int, Int, Int, Int, Int, Int, Int)"
		got := printSource(t, "fn at(t: "+big+") -> Int { t.10 }\nfn f() -> Int { { let a = (1, 2)\n a }.0 }\n")
		const want = "fn at(t) =\n  t.10\nfn f() =\n  (let a = (1, 2);\n    a).0\n"
		if got != want {
			t.Errorf("Print wrote\n%s\nwant\n%s", got, want)
		}
		const trees = "fn g() -> Int { let a = { let u = (1, 2)\n u }.0\n a }\nfn h() -> Int { let a = { let u = (1, 2)\n u.0 }\n a }\n"
		const wantTrees = "fn g() =\n  let a = (let u = (1, 2);\n    u).0;\n  a\nfn h() =\n  let a = let u = (1, 2);\n    u.0;\n  a\n"
		if got := printSource(t, trees); got != wantTrees {
			t.Errorf("Print wrote\n%s\nwant\n%s", got, wantTrees)
		}
	})

	t.Run("builder_creates_proj", func(t *testing.T) {
		b, p := newBuilder()
		target := b.LitInt(span(0, 1), 7)
		got := b.Proj(span(2, 5), target, 3)
		e := p.Exprs.Get(got)
		if e.Kind != core.Proj || e.Target != target || e.Index != 3 || e.Span != span(2, 5) {
			t.Errorf("Proj made %+v, want Kind Proj, Target %d, Index 3 and the span 2..5", e, target)
		}
		if max := b.Proj(span(0, 1), target, math.MaxUint32); p.Exprs.Get(max).Index != math.MaxUint32 {
			t.Errorf("Proj with the largest index kept %d", p.Exprs.Get(max).Index)
		}
	})

	t.Run("lint_print_and_subexprs_cross_a_proj", func(t *testing.T) {
		// A Proj has the target as its one child: Lint finds a violation inside it,
		// Subexprs visits it, and Print writes `target.index`. A Con of a tuple that
		// has the wrong number of arguments is named a tuple constructor, because it
		// has no name; a record is named after its type.
		l := newLintProg()
		v := l.variable(0)
		body := l.Proj(span(0, 1), l.VarRef(span(0, 1), v), 0)
		f := l.fn(1, nil, body)
		lintCheck(t, "a Proj of a variable that nothing binds", l.P, l.in, lintFun(f)+": unbound variable "+lintVar(v))

		b, p := newBuilder()
		in := base.NewInterner()
		pair := p.Ctors.Push(core.Ctor{Arity: 2, Tuple: true})
		point := p.Ctors.Push(core.Ctor{Name: in.InternString("Point"), Arity: 2, Record: true, Fields: []base.Symbol{in.InternString("x"), in.InternString("y")}})
		one := b.LitInt(span(0, 1), 1)
		p.Funs.Push(core.Fun{Name: in.InternString("short"), Body: b.Con(span(0, 1), pair, []core.ExprID{one}), HasBody: true, UserModule: true})
		p.Funs.Push(core.Fun{Name: in.InternString("long"), Body: b.Con(span(0, 1), point, []core.ExprID{one, one, one}), HasBody: true, UserModule: true})
		lintCheck(t, "wrong arities", p, in,
			"fun short: tuple constructor with 1 argument(s), but it takes 2",
			"fun long: constructor `Point` with 3 argument(s), but it takes 2")

		c := testsupport.Compile(t, "type Point { x: Float, y: Float }\nfn f(p: Point, t: (Int, (Bool, Point))) -> Float { t.1.1.y + p.x }\n"+
			"fn g(p: Point) -> (Int, Point) { (1, Point { x: p.y, y: 2.0 }) }\n")
		want := "fn Point() =\n  ()\nfn f(p, t) =\n  add_float(t.1.1.1, p.0)\nfn g(p) =\n  (1, Point(p.1, 2.0))\n"
		if got := core.Print(c.Core, c.Session.Interner); got != want {
			t.Errorf("Print wrote\n%s\nwant\n%s", got, want)
		}
		subs := 0
		for range c.Core.Subexprs(elabFun(t, c, "f").Body) {
			subs++
		}
		// add_float, its two operands' chains t.1.1.1 (4 nodes: three Projs and t) and p.0 (2 nodes).
		if subs != 1+4+2 {
			t.Errorf("Subexprs of t.1.1.y + p.x visited %d expressions, want 7", subs)
		}
	})

	t.Run("deep_reads_no_recursion", func(t *testing.T) {
		// Lint, Subexprs and Print cross 100 000 reads of a field, each the target of
		// the next, with the stack of Go capped: each has a stack of its own.
		const depth = 100_000
		p, in, e := projChain(depth)
		want := "fn deep(t) =\n  t" + strings.Repeat(".0", depth) + "\n"
		withStackCap(func() {
			if got := core.Lint(p, in); len(got) != 0 {
				t.Errorf("Lint of %d nested reads returned %d violations, the first is %q, want none", depth, len(got), got[0])
			}
			n := 0
			for range p.Subexprs(e) {
				n++
			}
			if n != depth+1 { // the reads, and the variable at the bottom
				t.Errorf("Subexprs of %d nested reads visited %d expressions, want %d", depth, n, depth+1)
			}
			if got := core.Print(p, in); got != want {
				t.Errorf("Print of %d nested reads wrote %d bytes, want %d; the text differs at byte %d", depth, len(got), len(want), lintFirstDiff(got, want))
			}
		})
	})
}
