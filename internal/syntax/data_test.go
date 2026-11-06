package syntax_test

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here are about what chapter 11, session 4 adds to the parser: record
// literals, field accesses, tuples and their indexes, and the tuple type. They
// reuse parseFile, printModule and wantModule from parser_test.go.

// dataExprs returns the expressions of kind k of m, in the order they were made.
func dataExprs(m *syntax.Module, k syntax.ExprKind) []syntax.Expr {
	var out []syntax.Expr
	for _, e := range m.Exprs.All() {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

// dataDiags parses src and returns the module, the id of the file and every
// diagnostic that lexing and parsing reported.
func dataDiags(t *testing.T, src string) (*syntax.Module, base.FileID, []diag.Diagnostic) {
	t.Helper()
	m, id, _, sink := parseFile(t, src)
	return m, id, sink.All()
}

// dataDiagsOf is the diagnostics that parsing src reports.
func dataDiagsOf(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()
	_, _, d := dataDiags(t, src)
	return d
}

// wantOneE0201 fails the test unless parsing src reports exactly one
// diagnostic, an E0201 with this message and its primary label on found, the
// first occurrence of found in src.
func wantOneE0201(t *testing.T, src, found, message string) {
	t.Helper()
	wantOneE0201In(t, src, found, found, message)
}

// wantOneE0201In is wantOneE0201 for a found that is not the first occurrence of
// its text: it is the first occurrence of found inside the first one of context.
func wantOneE0201In(t *testing.T, src, context, found, message string) {
	t.Helper()
	_, id, diags := dataDiags(t, src)
	lo := uint32(strings.Index(src, context) + strings.Index(context, found))
	want := base.Span{File: id, Lo: lo, Hi: lo + uint32(len(found))}
	if len(diags) != 1 || diags[0].Code != diag.E(201) || diags[0].Message != message || diags[0].Primary.Span != want {
		t.Errorf("%q: diagnostics %+v, want exactly one E0201 %q at %v", src, diags, message, want)
	}
}

func TestDataParse(t *testing.T) {
	t.Run("record_literals", func(t *testing.T) {
		// Fields in the order they are written; a field that is only a name is
		// punning, and prints like `x: x`; `..base` ends the list; a comma after the
		// last field, line breaks around the fields and an empty literal are
		// accepted.
		rows := []struct{ name, src, want string }{
			{"two fields", "Point { x: 1.0, y: 2.0 }", "(record Point (x 1.0) (y 2.0))"},
			{"punning", "Point { x, y }", "(record Point (x x) (y y))"},
			{"one pun and one value", "Point { x, y: 2.0 }", "(record Point (x x) (y 2.0))"},
			{"update", "Point { x: 3.0, ..p }", "(record Point (x 3.0) (.. p))"},
			{"update with no field", "Point { ..p }", "(record Point (.. p))"},
			{"base is an expression", "Point { ..f(a).p }", "(record Point (.. (field (call f a) p)))"},
			{"trailing comma", "Point { x: 1.0, y: 2.0, }", "(record Point (x 1.0) (y 2.0))"},
			{"trailing comma before the base", "Point { x: 1.0, ..p }", "(record Point (x 1.0) (.. p))"},
			{"no fields", "Point { }", "(record Point)"},
			{"over lines", "Point {\n  x: 1.0,\n  y: 2.0\n}", "(record Point (x 1.0) (y 2.0))"},
			{"over lines with the base last", "Point {\n  x: 1.0,\n  ..p\n}", "(record Point (x 1.0) (.. p))"},
			{"a record in a field", "Line { a: Point { x: 1.0 }, b: p }", "(record Line (a (record Point (x 1.0))) (b p))"},
			{"a value with operators", "Point { x: 1.0 + a, y: -b }", "(record Point (x (+ 1.0 a)) (y (neg b)))"},
			// A name in small letters is read as the literal it is, where the `{` is
			// followed by fields; what is not that is a name and a block (below).
			{"a name in small letters", "point { x: 1.0 }", "(record point (x 1.0))"},
			{"a name in small letters and a pun", "point { x, y }", "(record point (x x) (y y))"},
			{"a name in small letters and a base", "point { ..p }", "(record point (.. p))"},
		}
		for _, row := range rows {
			wantModule(t, row.name, "fn f() { "+row.src+" }", "(fn f (params) (ret _) (block "+row.want+"))\n")
		}
	})

	t.Run("field_access_and_tuple_index", func(t *testing.T) {
		// `.` binds tighter than any operator, as a call does, and they chain in
		// both orders. The lexer reads `n.0.1` as `n`, `.`, the Float `0.1`, and
		// `n.1.1.0` as `n`, `.`, the Float `1.1`, `.`, `0`: the parser splits the
		// Float into two indexes.
		rows := []struct{ name, src, want string }{
			{"field", "p.x", "(field p x)"},
			{"field of a field", "p.x.y", "(field (field p x) y)"},
			{"field of a call", "g(a).x", "(field (call g a) x)"},
			{"call of a field", "p.f(1)", "(call (field p f) 1)"},
			{"index", "t.0", "(index t 0)"},
			{"call of an index", "t.0(1)", "(call (index t 0) 1)"},
			{"index of a tuple", "(a, b).1", "(index (tuple a b) 1)"},
			{"two indexes in one Float", "n.0.1", "(index (index n 0) 1)"},
			{"three indexes, Float in the middle", "n.1.1.0", "(index (index (index n 1) 1) 0)"},
			{"four indexes, two Floats", "n.1.2.3.4", "(index (index (index (index n 1) 2) 3) 4)"},
			{"index then field", "t.0.x", "(field (index t 0) x)"},
			{"field then index", "t.x.0", "(index (field t x) 0)"},
			{"index, index, field", "t.0.1.x", "(field (index (index t 0) 1) x)"},
			{"binds tighter than a prefix operator", "-t.0", "(neg (index t 0))"},
			{"binds tighter than a binary operator", "a + t.0 * 2", "(+ a (* (index t 0) 2))"},
			{"space around the dot", "t . 0", "(index t 0)"},
			{"an index may be large", "t.4294967296", "(index t 4294967296)"},
		}
		for _, row := range rows {
			wantModule(t, row.name, "fn f() { "+row.src+" }", "(fn f (params) (ret _) (block "+row.want+"))\n")
		}
	})

	t.Run("tuples_and_the_tuple_type", func(t *testing.T) {
		// `()` stays the unit and `(e)` a parenthesis; two or more elements are a
		// tuple, of any size, nested or not. A tuple type is the same with types.
		rows := []struct{ name, src, want string }{
			{"unit", "()", "()"},
			{"parenthesis", "(1)", "1"},
			{"double parenthesis", "((1))", "1"},
			{"pair", "(1, true)", "(tuple 1 true)"},
			{"a parenthesized tuple", "((1, 2))", "(tuple 1 2)"},
			{"unit inside", "(1, ())", "(tuple 1 ())"},
			{"nested", "((1, 2), (3, (4, 5)))", "(tuple (tuple 1 2) (tuple 3 (tuple 4 5)))"},
			{"eight elements", "(1, 2, 3, 4, 5, 6, 7, 8)", "(tuple 1 2 3 4 5 6 7 8)"},
			{"elements with operators", "(a + 1, f(b), -c)", "(tuple (+ a 1) (call f b) (neg c))"},
			{"a parenthesis does not make a tuple", "(a, b).0 + (c)", "(+ (index (tuple a b) 0) c)"},
		}
		for _, row := range rows {
			wantModule(t, row.name, "fn f() { "+row.src+" }", "(fn f (params) (ret _) (block "+row.want+"))\n")
		}
		wantModule(t, "types", "fn f(x: (Int, (Bool, ())), y: Point) -> (Int, Int) {}",
			"(fn f (params (x (tuple Int (tuple Bool ()))) (y Point)) (ret (tuple Int Int)) (block))\n")
		wantModule(t, "type alias", "type P = (Int, Float, Bool)", "(type P (alias (tuple Int Float Bool)))\n")
		wantModule(t, "field of a record", "type R { a: (Int, Int) }", "(type R (record (a (tuple Int Int))))\n")
		// The span of a tuple type is from its `(` to its `)`.
		const src = "fn f() -> (Int, Bool) {}"
		m, _, _, _ := parseFile(t, src)
		ret := m.Types.Get(m.ItemNodes.Get(m.Items[0]).Ret)
		if lo := strings.Index(src, "("+"Int"); ret.Kind != syntax.TypeTuple || ret.Span.Lo != uint32(lo) || ret.Span.Hi != uint32(strings.Index(src, ") {")+1) {
			t.Errorf("the tuple type is %+v, want a TypeTuple that spans `(Int, Bool)`", ret)
		}
		if elems := m.TypeList(ret.Elems); len(elems) != 2 || m.Types.Get(elems[0]).Kind != syntax.TypeNamed || m.Types.Get(elems[1]).Kind != syntax.TypeNamed ||
			m.Types.Get(elems[0]).Span.Lo != uint32(strings.Index(src, "Int")) || m.Types.Get(elems[1]).Span.Lo != uint32(strings.Index(src, "Bool")) {
			t.Errorf("the elements of the tuple type are %v, want two named types, `Int` and `Bool`", elems)
		}
	})

	t.Run("no_record_literal_in_an_if_head", func(t *testing.T) {
		// Rust's rule: in the condition of an `if`, outside any delimiter, `Name {`
		// is a name and the block of the `if`. Inside a parenthesis, the arguments
		// of a call, a tuple, a block and the braces of a record, a literal may
		// start again; so it may in the branches. A `{` on the next line never
		// starts one.
		rows := []struct{ name, src, want string }{
			{"compared with a name", "if x == Origin { 1 } else { 2 }", "(if (== x Origin) (block 1) (block 2))"},
			{"a name alone", "if Origin { 1 } else { 2 }", "(if Origin (block 1) (block 2))"},
			{"in an else if", "if a { 1 } else if x == Origin { 2 } else { 3 }",
				"(if a (block 1) (if (== x Origin) (block 2) (block 3)))"},
			{"inside parentheses", "if (Origin { x: 1 }) == o { 1 } else { 2 }", "(if (== (record Origin (x 1)) o) (block 1) (block 2))"},
			{"inside call arguments", "if g(Origin { x: 1 }) { 1 } else { 2 }", "(if (call g (record Origin (x 1))) (block 1) (block 2))"},
			{"inside a tuple", "if (a, Origin { x: 1 }).1 == o { 1 } else { 2 }",
				"(if (== (index (tuple a (record Origin (x 1))) 1) o) (block 1) (block 2))"},
			{"inside a block", "if { Origin { x: 1 } } { 1 } else { 2 }", "(if (block (record Origin (x 1))) (block 1) (block 2))"},
			{"in the branches", "if a { Origin { x: 1 } } else { Origin { x: 2 } }",
				"(if a (block (record Origin (x 1))) (block (record Origin (x 2))))"},
			{"the flag is back after the head", "if a { 1 } else { 2 }\n  Origin { x: 1 }",
				"(expr (if a (block 1) (block 2))) (record Origin (x 1))"},
			{"the flag is back after a call in the head", "if g(a) { Origin { x: 1 } } else { 2 }",
				"(if (call g a) (block (record Origin (x 1))) (block 2))"},
			{"after a group", "if (x) == Origin { 1 } else { 2 }", "(if (== x Origin) (block 1) (block 2))"},
			{"after a call", "if g(x) == Origin { 1 } else { 2 }", "(if (== (call g x) Origin) (block 1) (block 2))"},
			{"after a tuple", "if (a, b).0 == Origin { 1 } else { 2 }", "(if (== (index (tuple a b) 0) Origin) (block 1) (block 2))"},
			{"after a block", "if { a } == Origin { 1 } else { 2 }", "(if (== (block a) Origin) (block 1) (block 2))"},
			{"after a record", "if (Origin { x: 1 }).x == Origin { 1 } else { 2 }",
				"(if (== (field (record Origin (x 1)) x) Origin) (block 1) (block 2))"},
			{"a lowercase name never starts one", "if x == origin { 1 } else { 2 }", "(if (== x origin) (block 1) (block 2))"},
			{"a `{` on the next line", "Origin\n  { 1 }", "(expr Origin) (block 1)"},
		}
		for _, row := range rows {
			wantModule(t, row.name, "fn f() { "+row.src+" }", "(fn f (params) (ret _) (block "+row.want+"))\n")
		}

		// What only a literal can start is a literal that needs parentheses: one
		// E0201 on it, with the edit that writes them, and the literal is in the
		// tree as it is written, so the checker does not read a block (`x: 1` is not
		// a statement) and a constructor that is not there. A `{ x }` is a block, and
		// a literal that is wrong by itself says only that.
		const head = "type Point { x: Float, y: Float }\nfn f(p: Point) { if "
		const tail = " > 0.5 { 1 } else { 2 } }"
		for _, row := range []struct{ name, lit, want string }{
			{"a field", "Point { x: 1.0, y: 2.0 }.x", "(field (record Point (x 1.0) (y 2.0)) x)"},
			{"a pun and a field", "Point { x, y: 2.0 }.x", "(field (record Point (x x) (y 2.0)) x)"},
			{"a base", "Point { ..p }.x", "(field (record Point (.. p)) x)"},
		} {
			src := head + row.lit + tail
			m, id, in, sink := parseFile(t, src)
			lo := len(head)
			lit := base.Span{File: id, Lo: uint32(lo), Hi: uint32(lo + strings.Index(row.lit, "}") + 1)}
			got := sink.All()
			if len(got) != 1 || got[0].Code != diag.E(201) || got[0].Primary.Span != lit ||
				got[0].Message != "a record literal in the head of an `if` needs parentheses" ||
				got[0].Primary.Message != "this literal needs parentheses" ||
				got[0].Help != "write the literal between parentheses: after a name, a `{` starts the block of the `if`" {
				t.Fatalf("%s: diagnostics %+v, want one E0201 on %v that asks for parentheses", row.name, got, lit)
			}
			if want := []diag.FixIt{diag.Insert(lit, "("), diag.Insert(base.Span{File: id, Lo: lit.Hi, Hi: lit.Hi}, ")")}; !slices.Equal(got[0].Fixits, want) {
				t.Errorf("%s: fix-its %+v, want %+v", row.name, got[0].Fixits, want)
			}
			if tree := syntax.PrintModule(m, in); !strings.Contains(tree, "(if (> "+row.want+" 0.5) (block 1) (block 2))") {
				t.Errorf("%s: printed\n%s\nwant the literal in the tree", row.name, tree)
			}
			if again := dataDiagsOf(t, testsupport.ApplyFixits(src, got[0])); len(again) != 0 {
				t.Errorf("%s: the edit leaves %+v", row.name, again)
			}
		}
		wantModule(t, "a block that starts with a name", "fn f() { if x == Origin { y } else { 2 } }",
			"(fn f (params) (ret _) (block (if (== x Origin) (block y) (block 2))))\n")
		// A name in small letters followed by fields is a literal where a name in
		// capitals would be one, but not in the head of an `if`: there it is the
		// condition and the `{` the block, as for a name in capitals.
		if m, _, _, _ := parseFile(t, "fn f(b: Bool) { if b { x: 1 } }"); len(dataExprs(m, syntax.ExprRecord)) != 0 {
			t.Errorf("a lowercase name in the head of an `if` and a `{` with fields is a record literal, want the condition and the block")
		}
		// A literal that is wrong by itself says that and nothing about parentheses,
		// even when the mistake is far enough before its end that a second report
		// would not be suppressed.
		wantOneE0201(t, "fn f() { if P { x: 1 y: 2, a: 1, b: 2, c: 3, d: 4, e: 5 } { 1 } else { 2 } }", "y", "expected `,` or `}`, found `y`")
	})

	t.Run("the_rule_of_the_if_head_does_not_outlive_it", func(t *testing.T) {
		// What the head of an `if` turns off is back on after it, also when a mistake
		// was reported before: the literals after the `if`, in the same function and
		// after the call in the head of another, are read, and only the one mistake is
		// there.
		const src = "fn f() { 1 + }\nfn g() {\n  if a { 1 } else { 2 }\n  Origin { x: 1 }\n  if h(b) { 1 } else { 2 }\n  Origin { x: 2 }\n" +
			"  (Origin { x: 1 }, Origin { x: 2 })\n  P { a: Q { b: 1 }, c: Q { b: 2 }, ..Q { b: 3 } }\n}"
		m, _, in, sink := parseFile(t, src)
		got := syntax.PrintModule(m, in)
		if n := len(sink.All()); n != 1 || !strings.Contains(got, "(expr (if a (block 1) (block 2))) (expr (record Origin (x 1))) (expr (if (call h b) (block 1) (block 2))) "+
			"(expr (record Origin (x 2))) (expr (tuple (record Origin (x 1)) (record Origin (x 2)))) "+
			"(record P (a (record Q (b 1))) (c (record Q (b 2))) (.. (record Q (b 3))))") {
			t.Errorf("%d diagnostics and the second function printed as\n%s\nwant one diagnostic and both literals read", n, got)
		}
	})

	t.Run("an_access_continues_the_third_operand_of_a_chain", func(t *testing.T) {
		// The help of a chained comparison writes the three operands it rewrites only
		// when each is one token; the third is not when an access follows it
		// (`z.w`), as it is not when a call or an operator does, so the help is the
		// generic one, and the edit that is offered stays.
		for _, row := range []struct{ src, help string }{
			{"fn f() { x < y <= z }", "use `&&`: `x < y && y <= z`"},
			{"fn f() { x < y <= z.w }", "use `&&`: `a < b && b < c`"},
			{"fn f() { x < y <= z.0 }", "use `&&`: `a < b && b < c`"},
		} {
			_, _, diags := dataDiags(t, row.src)
			if len(diags) != 1 || diags[0].Code != diag.E(203) || diags[0].Help != row.help {
				t.Errorf("%q: diagnostics %+v, want one E0203 with the help %q", row.src, diags, row.help)
			}
		}
	})

	t.Run("record_and_tuple_nodes", func(t *testing.T) {
		// The fields of the nodes that the printer does not show: spans, the name
		// of a field, whether a field is a pun, the base.
		const src = "fn f() { Point { x, y: 2.0, ..p }.x }"
		m, id, _, sink := parseFile(t, src)
		if n := len(sink.All()); n != 0 {
			t.Fatalf("%d diagnostics: %v", n, sink.All())
		}
		// sp is the span of the n bytes that start at the first occurrence of text.
		sp := func(text string, n int) base.Span {
			lo := strings.Index(src, text)
			return base.Span{File: id, Lo: uint32(lo), Hi: uint32(lo + n)}
		}
		const lit = "Point { x, y: 2.0, ..p }"
		braces := sp("{ x", len(lit)-len("Point "))
		rec := dataExprs(m, syntax.ExprRecord)[0]
		if rec.Span != sp("Point {", len(lit)) || rec.NameSpan != sp("Point", 5) || rec.OpSpan != braces {
			t.Errorf("record spans: Span %v, NameSpan %v, OpSpan %v; want %v, %v and %v (the braces)",
				rec.Span, rec.NameSpan, rec.OpSpan, sp("Point {", len(lit)), sp("Point", 5), braces)
		}
		fields := m.FieldInits(rec.Fields)
		if len(fields) != 2 || !fields[0].Pun || fields[1].Pun || fields[0].Span != sp("x,", 1) || fields[1].Span != sp("y:", 1) {
			t.Fatalf("fields %+v, want the pun x and the value y", fields)
		}
		if v := m.Exprs.Get(fields[0].Value); v.Kind != syntax.ExprName || v.Span != fields[0].Span || v.Name != fields[0].Name {
			t.Errorf("the value of a pun is %+v, want a name with the span of the field", v)
		}
		if !rec.HasBase || m.Exprs.Get(rec.Base).Span != sp("p }", 1) {
			t.Errorf("base %+v (HasBase %v), want the expression `p`", m.Exprs.Get(rec.Base), rec.HasBase)
		}
		acc := dataExprs(m, syntax.ExprField)[0]
		nameLo := uint32(strings.LastIndex(src, "x }"))
		want, wantName := sp("Point {", len(lit)+len(".x")), base.Span{File: id, Lo: nameLo, Hi: nameLo + 1}
		if acc.Span != want || acc.NameSpan != wantName || m.Exprs.Get(acc.Target).Kind != syntax.ExprRecord {
			t.Errorf("field access: Span %v and NameSpan %v, want %v and %v, on the record", acc.Span, acc.NameSpan, want, wantName)
		}
		const tsrc = "fn f() { (1, true) }"
		tm, tid, _, _ := parseFile(t, tsrc)
		tup := dataExprs(tm, syntax.ExprTuple)[0]
		if want := (base.Span{File: tid, Lo: 9, Hi: 18}); tup.Span != want || tup.Args.Count != 2 {
			t.Errorf("tuple: Span %v with %d elements, want %v (from `(` to `)`) with 2", tup.Span, tup.Args.Count, want)
		}
	})

	t.Run("tuple_index_nodes_and_saturation", func(t *testing.T) {
		// One node per index: its Span runs from the start of the whole access to
		// its own digits, OpSpan is the digits, Name is the digits as written and
		// Index their value. An index that does not fit in 32 bits is kept at the
		// largest one that does, so that it is out of range of every tuple, and Name
		// still has what the program wrote.
		const src = "fn f() { n.0.1 }"
		m, id, _, _ := parseFile(t, src)
		idx := dataExprs(m, syntax.ExprTupleIndex)
		if len(idx) != 2 {
			t.Fatalf("%d tuple index nodes for n.0.1, want 2", len(idx))
		}
		first, second := idx[0], idx[1]
		sp := func(lo, hi uint32) base.Span { return base.Span{File: id, Lo: lo, Hi: hi} }
		if first.Index != 0 || first.Span != sp(9, 12) || first.OpSpan != sp(11, 12) ||
			second.Index != 1 || second.Span != sp(9, 14) || second.OpSpan != sp(13, 14) {
			t.Errorf("first %+v and second %+v, want indexes 0 and 1, spans 9..12 and 9..14, digits at 11..12 and 13..14", first, second)
		}
		if m.Exprs.Get(second.Target).Kind != syntax.ExprTupleIndex || m.Exprs.Get(first.Target).Kind != syntax.ExprName {
			t.Errorf("the second index is not on the first, or the first is not on `n`")
		}
		for _, row := range []struct {
			digits string
			index  uint32
		}{
			{"4294967294", math.MaxUint32 - 1},
			{"4294967295", math.MaxUint32},
			{"4294967296", math.MaxUint32},
			{"99999999999", math.MaxUint32},
		} {
			m, _, in, _ := parseFile(t, "fn f() { t."+row.digits+" }")
			e := dataExprs(m, syntax.ExprTupleIndex)[0]
			if e.Index != row.index || in.String(e.Name) != row.digits {
				t.Errorf("t.%s: Index %d and Name %q, want %d and the digits as written", row.digits, e.Index, in.String(e.Name), row.index)
			}
		}
		// The first of two indexes in one token may be the large one.
		m, _, in, _ := parseFile(t, "fn f() { t.99999999999.1 }")
		if idx := dataExprs(m, syntax.ExprTupleIndex); len(idx) != 2 || idx[0].Index != math.MaxUint32 || idx[1].Index != 1 || in.String(idx[1].Name) != "1" {
			t.Errorf("t.99999999999.1 gives %+v, want a saturated index and then 1", idx)
		}
		// The largest `Int` is an index like any other, one that no tuple has. A
		// number that is not an `Int` was reported by the lexer, E0105, and is no
		// index at all: an error node, and no second error about the same digits.
		m, _, in, sink := parseFile(t, "fn f() { t.9223372036854775807 }")
		if e := dataExprs(m, syntax.ExprTupleIndex)[0]; len(sink.All()) != 0 || e.Index != math.MaxUint32 || in.String(e.Name) != "9223372036854775807" {
			t.Errorf("t.9223372036854775807 gives %+v and %+v, want a saturated index and no diagnostic", e, sink.All())
		}
		m, _, in, sink = parseFile(t, "fn f() { t.9223372036854775808.1 }")
		if idx := dataExprs(m, syntax.ExprTupleIndex); len(sink.All()) != 0 || len(idx) != 2 || idx[0].Index != math.MaxUint32 || idx[1].Index != 1 {
			t.Errorf("t.9223372036854775808.1 gives %+v and %+v, want two indexes (the first saturated) and no diagnostic: it is a Float token", idx, sink.All())
		}
		// Nor is any other number that the lexer reported: an `Int` that is malformed
		// (E0107), a `Float` that is malformed or out of range (E0107, E0109). Each is
		// one diagnostic, the lexer's, and an error node: neither an index nor a second
		// error about digits that are already wrong.
		for _, row := range []struct {
			src  string
			code diag.Code
		}{
			{"fn f() { t.9223372036854775808 }", diag.E(105)},
			{"fn f() { t.1_ }", diag.E(107)},
			{"fn f() { t.1__ }", diag.E(107)},
			{"fn f() { t.0_0_ }", diag.E(107)},
			{"fn f() { t.0x_ }", diag.E(107)},
			{"fn f() { t.0x1_ }", diag.E(107)},
			{"fn f() { t.0b }", diag.E(107)},
			{"fn f() { t.1_.5 }", diag.E(107)},
			{"fn f() { t.1.5_ }", diag.E(107)},
			{"fn f() { t.1e5_ }", diag.E(107)},
			{"fn f() { t.1e999 }", diag.E(109)},
		} {
			m, _, diags := dataDiags(t, row.src)
			if len(diags) != 1 || diags[0].Code != row.code || len(dataExprs(m, syntax.ExprTupleIndex)) != 0 || len(dataExprs(m, syntax.ExprError)) != 1 {
				t.Errorf("%q gives %+v and %d index nodes, want the one diagnostic %v of the lexer and an error node", row.src, diags, len(dataExprs(m, syntax.ExprTupleIndex)), row.code)
			}
		}
	})

	t.Run("not_an_index_or_a_name_after_a_dot_E0201", func(t *testing.T) {
		// After a `.`: a lowercase name or the digits of an index, written one way.
		// The lexer accepts more as numbers (`01`, `1_0`, `0x1`, `1e5`, `1.5e3`), and
		// none of them is an index; a name that starts in uppercase is not a field.
		const what = "expected field name or tuple index, found "
		for _, row := range []struct{ src, found string }{
			{"fn f() { t.1e5 }", "1e5"},
			{"fn f() { t.01 }", "01"},
			{"fn f() { t.0x1 }", "0x1"},
			{"fn f() { t.1_0 }", "1_0"},
			{"fn f() { t.1.5e3 }", "1.5e3"},
			{"fn f() { t.0.01 }", "0.01"},
			{"fn f() { t.A }", "A"},
			{"fn f() { t.true }", "true"},
			{"fn f() { t.(1) }", ".("},
		} {
			found := strings.TrimPrefix(row.found, ".")
			wantOneE0201In(t, row.src, row.found, found, what+"`"+found+"`")
		}
		// A number with leading zeros is the one that has a repair: the index written
		// without them, with the help that says so and the edit. The others, `1_0`,
		// `0x1` and `1e5`, say what they are and have no help.
		for _, row := range []struct{ src, fixed string }{
			{"fn f() { t.01 }", "fn f() { t.1 }"},
			{"fn f() { t.00 }", "fn f() { t.0 }"},
			{"fn f() { t.0.01 }", "fn f() { t.0.1 }"},
			{"fn f() { t.007.1 }", "fn f() { t.7.1 }"},
		} {
			_, _, d := dataDiags(t, row.src)
			fixed := strings.TrimSuffix(strings.TrimPrefix(row.fixed, "fn f() { t."), " }")
			if len(d) != 1 || d[0].Help != "write the index without leading zeros: `"+fixed+"`" || len(d[0].Fixits) != 1 {
				t.Errorf("%q: diagnostics %+v, want one E0201 with the help for `%s` and one edit", row.src, d, fixed)
			} else if got := testsupport.ApplyFixits(row.src, d[0]); got != row.fixed {
				t.Errorf("%q: the edit gives %q, want %q", row.src, got, row.fixed)
			} else if again := dataDiagsOf(t, got); len(again) != 0 {
				t.Errorf("%q: the edit leaves %+v", row.src, again)
			}
		}
		for _, src := range []string{"fn f() { t.1e5 }", "fn f() { t.0x1 }", "fn f() { t.1_0 }", "fn f() { t.1.5e3 }", "fn f() { t.A }"} {
			if d := dataDiagsOf(t, src); len(d) != 1 || d[0].Help != "" || len(d[0].Fixits) != 0 {
				t.Errorf("%q: diagnostics %+v, want one E0201 with no help and no edit", src, d)
			}
		}
		wantOneE0201(t, "fn f() { t. }", "}", what+"`}`")
		wantOneE0201(t, "fn f() { t.0. }", "}", what+"`}`")
		_, _, in := dataDiags(t, "fn f() { t.1e5 }")
		if in[0].Primary.Message != "expected field name or tuple index here" {
			t.Errorf("the primary label is %q", in[0].Primary.Message)
		}
	})

	t.Run("tuple_and_record_mistakes_E0201", func(t *testing.T) {
		// A tuple has two or more elements, and so does its type: one element and a
		// comma is a missing expression, and one type in parentheses a missing `,`,
		// with a help. A comma after the last element is a missing element, in a
		// tuple of two or more as in one, and in the type as in the expression (a call
		// takes a trailing comma, a tuple does not). Inside the braces of a record, a
		// mistake is one E0201 at the token found, and the rest of the literal is
		// skipped up to its own `}`.
		wantOneE0201In(t, "fn f() { (1,) }", ",)", ")", "expected expression, found `)`")
		wantOneE0201In(t, "fn f() { (1, 2,) }", "2,)", ")", "expected expression, found `)`")
		wantOneE0201In(t, "fn f() -> (Int, Bool,) {}", "Bool,)", ")", "expected type, found `)`")
		// The tuple with the comma is an error node and not a tuple of two with an
		// error inside, which would be of the wrong arity wherever it is used; the
		// statement after it is read.
		for _, src := range []string{"fn f() {\n  (1, 2,)\n  let b = 3\n}", "fn f() {\n  (1,)\n  let b = 3\n}"} {
			m, _, in, _ := parseFile(t, src)
			if got := syntax.PrintModule(m, in); !strings.Contains(got, "(block (expr (error)) (let b _ 3))") {
				t.Errorf("%q printed\n%s\nwant an error node and then the statement `let b = 3`", src, got)
			}
		}
		// An element that cannot be read in the middle is the same: the tuple is an
		// error node, not a tuple of three with one of them an error.
		wantOneE0201In(t, "fn f() { (1, , 2) }", " ,", ",", "expected expression, found `,`")
		if m, _, in, _ := parseFile(t, "fn f() {\n  (1, , 2)\n  let b = 3\n}"); !strings.Contains(syntax.PrintModule(m, in), "(block (expr (error)) (let b _ 3))") {
			t.Errorf("a tuple with an element missing in the middle printed\n%s\nwant an error node and then the statement `let b = 3`", syntax.PrintModule(m, in))
		}
		wantOneE0201(t, "fn f() { (1, 2 3) }", "3", "expected `)`, found `3`")
		wantOneE0201In(t, "fn f() -> (Int) {}", "(Int)", ")", "expected `,`, found `)`")
		_, _, d := dataDiags(t, "fn f() -> (Int) {}")
		if d[0].Help != "a tuple type has two or more types, as in `(Int, Bool)`" {
			t.Errorf("help %q for a type with one element", d[0].Help)
		}
		wantOneE0201In(t, "fn f() -> (Int, Bool Int) {}", "Bool Int", "Int", "expected `)`, found `Int`")
		wantOneE0201(t, "fn f() -> (Int, 1) {}", "1", "expected type, found `1`")
		wantOneE0201(t, "fn f() { Point { x 1 } }", "1", "expected `}`, found `1`")
		wantOneE0201(t, "fn f() { Point { ..p, x: 1 } }", ",", "expected `}`, found `,`")
		wantOneE0201(t, "fn f() { Point { 1 } }", "1", "expected identifier, found `1`")
		wantOneE0201(t, "fn f() { Point { X: 1 } }", "X", "expected identifier, found `X`")
		wantOneE0201(t, "fn f() { Point { x: 1, Y: 2 } }", "Y", "expected identifier, found `Y`")
		// Only a name with a capital has the help and the edit: any other token
		// that is not a field name has neither.
		for _, src := range []string{"fn f() { Point { 1 } }", "fn f() { Point { = } }", "fn f() { Point { x: 1, ,} }"} {
			if d := dataDiagsOf(t, src); len(d) != 1 || d[0].Help != "" || len(d[0].Fixits) != 0 {
				t.Errorf("%q: diagnostics %+v, want one E0201 with no help and no edit", src, d)
			}
		}
		wantOneE0201(t, "fn f() { Point { x: } }", "}", "expected expression, found `}`")
		wantOneE0201(t, "fn f() { Point { x: 1 y: 2 } }", "y", "expected `,` or `}`, found `y`")
		wantOneE0201(t, "fn f() { Point { x: 1; y: 2 } }", ";", "expected `,` or `}`, found `;`")
		wantOneE0201(t, "fn f() { Point { x: 1 ..p } }", "..", "expected `,` or `}`, found `..`")
		// A name that starts in lowercase starts a literal only where the `{` is
		// followed by fields: `{ 1 }` is a block, which needs a line break before it
		// (and in the head of an `if` neither is a literal).
		wantOneE0201In(t, "fn f() { a { 1 } }", "a { 1", "{", "expected newline, found `{`")
		// A `,` that is missing between two fields, a `;` where it goes or the next
		// field on the line below, has the help and the edit that write it. The
		// literal is read to its `}` and is an error node, so the statement after it
		// is there and nothing else is reported (the edit leaves a literal that is
		// read as it was meant). A field that starts in uppercase has the help of the
		// rule and the edit that writes it in small letters.
		for _, row := range []struct{ name, src, fixed string }{
			{"a semicolon", "fn f() { Point { x: 1.0; y: 2.0 } }", "fn f() { Point { x: 1.0, y: 2.0 } }"},
			{"a semicolon and a line break", "fn f() { Point { x: 1.0;\n y: 2.0 } }", "fn f() { Point { x: 1.0,\n y: 2.0 } }"},
			{"the field on the next line", "fn f() {\n  Point {\n    x: 1.0\n    y: 2.0\n  }\n}", "fn f() {\n  Point {\n    x: 1.0,\n    y: 2.0\n  }\n}"},
			{"the same line", "fn f() { Point { x: 1.0 y: 2.0 } }", "fn f() { Point { x: 1.0, y: 2.0 } }"},
			{"a base on the next line", "fn f() {\n  Point {\n    x: 1.0\n    ..p\n  }\n}", "fn f() {\n  Point {\n    x: 1.0,\n    ..p\n  }\n}"},
			{"three fields", "fn f() { Point { x: 1.0; y: 2.0; z: 3.0 } }", ""},
			// A field that is only a name, a pun, is the next field as well. It is told
			// by what follows it and by where it is: on the line of the previous field,
			// or on a line of its own straight under it.
			{"a pun on the next line", "fn f() {\n  Point {\n    x\n    y\n  }\n}", "fn f() {\n  Point {\n    x,\n    y\n  }\n}"},
			{"a pun after a field with a value", "fn f() {\n  Point {\n    x: 1.0\n    y\n  }\n}", "fn f() {\n  Point {\n    x: 1.0,\n    y\n  }\n}"},
			{"two puns on one line", "fn f() { Point { x y } }", "fn f() { Point { x, y } }"},
			{"a pun with its comma", "fn f() { Point { x: 1.0 y, z: 2.0 } }", "fn f() { Point { x: 1.0, y, z: 2.0 } }"},
			{"a pun under a field on the line of the brace", "fn f() {\n  Point { x: 1.0\n          y }\n}", "fn f() {\n  Point { x: 1.0,\n          y }\n}"},
		} {
			_, _, d := dataDiags(t, row.src)
			if row.fixed == "" {
				// Each separator is reported on its own.
				if len(d) != 2 || d[0].Code != diag.E(201) || d[1].Code != diag.E(201) {
					t.Errorf("%s: diagnostics %+v, want two E0201, one for each `;`", row.name, d)
				}
				continue
			}
			if len(d) != 1 || d[0].Code != diag.E(201) || d[0].Help != "separate the fields with `,`" || len(d[0].Fixits) != 1 {
				t.Errorf("%s: diagnostics %+v, want one E0201 with the help and one edit", row.name, d)
				continue
			}
			if fixed := testsupport.ApplyFixits(row.src, d[0]); fixed != row.fixed {
				t.Errorf("%s: the edit gives %q, want %q", row.name, fixed, row.fixed)
			} else if again := dataDiagsOf(t, fixed); len(again) != 0 {
				t.Errorf("%s: the edit leaves %+v", row.name, again)
			}
		}
		// A name that is not the start of a field leaves the `}` as the mistake: an
		// operator after it (`y + 1` is an expression), and a name on a line of its own
		// that is not under the previous field, which is as likely a statement after a
		// literal whose `}` is missing, and then the `{` is the one that is reported.
		wantOneE0201(t, "fn f() { Point { x: 1 y + 1 } }", "y", "expected `}`, found `y`")
		// A pun that is followed by another pun, or by a base, is a field all the same.
		// The second `,` that is missing is not reported over the first: its token is
		// the one after the token of the first.
		wantOneE0201(t, "fn f() { Point { x y z } }", "y", "expected `,` or `}`, found `y`")
		wantOneE0201(t, "fn f() { Point { x: 1.0 y ..p } }", "y", "expected `,` or `}`, found `y`")
		wantOneE0201(t, "fn f() {\n  Point { x: 1.0\n  y }\n}", "y", "expected `}`, found `y`")
		_, _, d = dataDiags(t, "fn f() -> Point {\n  let p = Point { x: 1.0\n  p\n}\n")
		if len(d) != 1 || d[0].Message != "expected `}`, found `p`" || len(d[0].Secondary) != 1 || d[0].Secondary[0].Message != "unclosed `{` opened here" {
			t.Errorf("a literal whose `}` is missing, then a name: diagnostics %+v, want one E0201 with the `{` of the literal", d)
		}
		if m, _, in, _ := parseFile(t, "fn f() {\n  let p = Point {\n    x: 1.0\n    y: 2.0\n  }\n  let b = 3\n}"); !strings.Contains(syntax.PrintModule(m, in), "(let p _ (error))") ||
			!strings.Contains(syntax.PrintModule(m, in), "(let b _ 3)") {
			t.Errorf("a literal with a missing comma printed\n%s\nwant an error node and then the statement `let b = 3`", syntax.PrintModule(m, in))
		}
		for _, row := range []struct{ src, fixed string }{
			{"fn f() { Point { X: 1.0 } }", "fn f() { Point { x: 1.0 } }"},
			{"fn f() { Point { x: 1.0, Yy: 2.0 } }", "fn f() { Point { x: 1.0, yy: 2.0 } }"},
		} {
			_, _, d := dataDiags(t, row.src)
			if len(d) != 1 || d[0].Help != "a field name starts with a lowercase letter" || len(d[0].Fixits) != 1 {
				t.Errorf("%q: diagnostics %+v, want one E0201 with the help and one edit", row.src, d)
			} else if fixed := testsupport.ApplyFixits(row.src, d[0]); fixed != row.fixed {
				t.Errorf("%q: the edit gives %q, want %q", row.src, fixed, row.fixed)
			}
		}

		// What follows the broken literal is read: one diagnostic for the literal,
		// including its nested braces, and then the next statement and the next item
		// are still there.
		for _, src := range []string{
			"fn f() {\n  let a = Point { x 1, y: 2 }\n  let b = 3\n}\nfn g() {}",
			"fn f() {\n  let a = Point { x 1, y: Line { z: { 2 } } }\n  let b = 3\n}\nfn g() {}",
		} {
			m, _, in, sink := parseFile(t, src)
			if diags := sink.All(); len(diags) != 1 || diags[0].Code != diag.E(201) {
				t.Errorf("%q: diagnostics %+v, want one E0201", src, diags)
			}
			if got := syntax.PrintModule(m, in); !strings.Contains(got, "(let b _ 3)") || !strings.HasSuffix(got, "(fn g (params) (ret _) (block))\n") {
				t.Errorf("%q printed\n%s\nwant the statement `let b = 3` and the function g after the broken literal", src, got)
			}
		}
	})

	t.Run("many_elements_and_fields_are_not_many_levels", func(t *testing.T) {
		// The elements of a tuple and the fields of a literal are one level each
		// while they are read, and the level is given back: 300 of them side by side
		// are read, as 300 arguments of a call are.
		elems := strings.TrimSuffix(strings.Repeat("1, ", 300), ", ")
		var fields []string
		for i := range 300 {
			fields = append(fields, "f"+strings.Repeat("a", i)+": 1")
		}
		for _, row := range []struct{ name, src string }{
			{"a tuple of 300 elements", "fn f() { (" + elems + ") }"},
			{"a tuple type of 300 elements", "fn f() -> (" + strings.TrimSuffix(strings.Repeat("Int, ", 300), ", ") + ") {}"},
			{"a literal of 300 fields", "fn f() { P { " + strings.Join(fields, ", ") + " } }"},
			{"a literal of 300 fields and a base", "fn f() { P { " + strings.Join(fields, ", ") + ", ..p } }"},
			{"300 updates in a row", "fn f() {" + strings.Repeat("\n  P { x: 1, ..p }", 300) + "\n}"},
		} {
			if _, _, diags := dataDiags(t, row.src); len(diags) != 0 {
				t.Errorf("%s: diagnostics %+v, want none", row.name, diags)
			}
		}
	})

	t.Run("a_literal_that_could_not_be_read_is_an_error_node", func(t *testing.T) {
		// Whatever stops a literal, the rest of it is skipped up to its own `}` and
		// the literal is an ExprError that covers it all, from the name to that `}`:
		// a field that is not a name, a value or a base that is too deep, a literal
		// that starts at the deepest level, so that its first field or its base is the
		// one over the limit, and braces inside the skipped part. A missing `,` is not
		// skipped but read through (see missingComma), and the literal is the same
		// error node. When the expression is abandoned the statement is one more error
		// node, with the same span if the literal is all of it.
		deep := strings.Repeat("(", syntax.MaxNesting+1) + "1" + strings.Repeat(")", syntax.MaxNesting+1)
		open, shut := strings.Repeat("(", syntax.MaxNesting), strings.Repeat(")", syntax.MaxNesting)
		for _, row := range []struct {
			before, lit, after string
			nodes              int // the ExprErrors that cover the literal and nothing else
		}{
			{"", "Point { 1 }", "", 1},
			{"", "Point { x: 1 y: 2 }", "", 1},
			{"", "Point { ..p, x: 1 }", "", 1},
			{"", "Point { x 1, y: Line { z: { 2 } } }", "", 1},
			{"", "Point { x: " + deep + " }", "", 2},
			{"", "Point { ..(" + deep + ") }", "", 2},
			{open, "Point { x: 1 }", shut, 1},
			{open, "Point { ..p }", shut, 1},
		} {
			src := "fn f() { " + row.before + row.lit + row.after + " }"
			m, id, _, _ := parseFile(t, src)
			lo := uint32(len("fn f() { " + row.before))
			want := base.Span{File: id, Lo: lo, Hi: lo + uint32(len(row.lit))}
			covering := 0
			for _, e := range dataExprs(m, syntax.ExprError) {
				if e.Span == want {
					covering++
				}
			}
			if covering != row.nodes {
				t.Errorf("%.60q: %d ExprErrors cover the whole literal %v, want %d", src, covering, want, row.nodes)
			}
		}
	})

	t.Run("the_end_of_the_file_inside_a_type_or_a_literal_is_E0202", func(t *testing.T) {
		// The end of the file with the `(` of a tuple type, or the `{` of a literal,
		// still open is the unclosed delimiter, as it is for any other, on that
		// delimiter, and not a type or a field that is missing.
		for _, row := range []struct{ src, open string }{
			{"fn f() -> (", "("}, {"fn f() -> (Int,", "("}, {"fn f() -> (Int", "("}, {"fn f(x: (Int, (Bool,", "("},
			{"fn f() { Point {", "{"}, {"fn f() { Point { x: 1.0,", "{"}, {"fn f() { Point { x: 1.0", "{"}, {"fn f() { Point { ..p", "{"},
			{"fn f() { Point { x: 1.0, y: Line { z: 2", "{"},
			// A field that is alone on its line, the file ending right after its
			// comma: the edit that removes it looks at what follows the comma, and
			// there is nothing there.
			{"fn f() {\n  Point {\n    x: 1.0,", "{"},
		} {
			_, id, diags := dataDiags(t, row.src)
			open := strings.LastIndex(row.src, row.open)
			if row.open == "{" {
				open = strings.Index(row.src, "Point {") + len("Point ")
			}
			want := base.Span{File: id, Lo: uint32(open), Hi: uint32(open + 1)}
			if row.src == "fn f() { Point { x: 1.0, y: Line { z: 2" {
				want.Lo, want.Hi = uint32(strings.LastIndex(row.src, "{")), uint32(strings.LastIndex(row.src, "{")+1)
			}
			if len(diags) != 1 || diags[0].Code != diag.E(202) || diags[0].Primary.Span != want {
				t.Errorf("%q: diagnostics %+v, want one E0202 on %v", row.src, diags, want)
			}
		}
	})

	t.Run("the_end_of_the_file_inside_a_literal_that_went_wrong_is_E0202", func(t *testing.T) {
		// A literal that is broken and then not closed is two mistakes: the one in the
		// braces, and the `{` that the skip to its `}` never finds, at that `{`, also
		// when the skipped part has braces of its own that it does close or not, and
		// whether the first field is the one that breaks (`Point { 1, …`) or the
		// literal breaks after it.
		for _, src := range []string{"fn f() { Point { x 1, y: 2, z: 3", "fn f() { Point { x 1, y: Line { z: 2 }, w: 3", "fn f() { Point { x 1, y: Line { z: 2",
			"fn f() { Point { 1, y: 2", "fn f() { Point { , y: Line { z: 2"} {
			_, id, diags := dataDiags(t, src)
			open := uint32(strings.Index(src, "Point {") + len("Point "))
			var codes []string
			var at base.Span
			for _, d := range diags {
				codes = append(codes, d.Code.String())
				if d.Code == diag.E(202) {
					at = d.Primary.Span
				}
			}
			slices.Sort(codes)
			if want := []string{"E0201", "E0202"}; !slices.Equal(codes, want) || at != (base.Span{File: id, Lo: open, Hi: open + 1}) {
				t.Errorf("%q: diagnostics %+v, want an E0201 and an E0202 on the `{` of the literal (byte %d)", src, diags, open)
			}
		}
	})

	t.Run("a_tuple_type_that_could_not_be_read_is_an_error_node", func(t *testing.T) {
		// What stops a tuple type leaves a TypeError, which the later passes read
		// as a type already reported, and its span covers what was read: from the
		// `(` up to the token before the one that stopped it.
		for _, row := range []struct{ src, covers string }{
			{"fn f() -> (Int, 1) {}", "(Int,"},
			{"fn f() -> (Int, Bool Int) {}", "(Int, Bool"},
			{"fn f() -> (Int) {}", "(Int"},
			{"fn f() -> (Int,", "(Int,"},
			{"fn f() -> (", "("},
			{"fn f() -> ((Int, Bool, 1), Int) {}", "((Int, Bool,"},
		} {
			m, id, _, _ := parseFile(t, row.src)
			ret := m.Types.Get(m.ItemNodes.Get(m.Items[0]).Ret)
			lo := uint32(strings.Index(row.src, "-> ") + 3)
			want := base.Span{File: id, Lo: lo, Hi: lo + uint32(len(row.covers))}
			if ret.Kind != syntax.TypeError || ret.Span != want {
				t.Errorf("%q: the return type is %+v, want a TypeError that covers %q (%v)", row.src, ret, row.covers, want)
			}
		}
		// The 257th `(` is not read: the types that were started become TypeErrors,
		// and the outermost covers the 256 `(` that were read. (The parameter whose
		// type this is is not kept, so the nodes are looked for in the table.)
		src := "fn f(x: " + strings.Repeat("(", syntax.MaxNesting+1) + "Int" + strings.Repeat(", Int)", syntax.MaxNesting+1) + ") {}"
		m, id, _, _ := parseFile(t, src)
		lo := uint32(len("fn f(x: "))
		var outermost, first syntax.TypeExpr // the 257th `(` is the first TypeError made
		errors := 0
		for _, ty := range m.Types.All() {
			if ty.Kind == syntax.TypeError {
				if errors == 0 {
					first = ty
				}
				errors++
				if ty.Span.Hi-ty.Span.Lo > outermost.Span.Hi-outermost.Span.Lo {
					outermost = ty
				}
			}
		}
		want, wantFirst := base.Span{File: id, Lo: lo, Hi: lo + syntax.MaxNesting}, base.Span{File: id, Lo: lo + syntax.MaxNesting, Hi: lo + syntax.MaxNesting}
		if errors != syntax.MaxNesting+1 || outermost.Span != want || first.Span != wantFirst {
			t.Errorf("a type of 257 levels has %d TypeErrors, the outermost %v and the first %v, want 257, %v and the empty span at the 257th `(` %v",
				errors, outermost.Span, first.Span, want, wantFirst)
		}
	})

	t.Run("removal_of_a_field_of_a_literal", func(t *testing.T) {
		// FieldInit.Removal is what the edit of E0411 deletes: a field with a comma
		// after it takes the comma and the blanks that follow it, and the blanks
		// before it too if it is alone on its line (what is left of the line is then
		// empty, not a line of blanks); the last field, with no comma after it, takes
		// the comma before it; the only field is itself. It is one line when no line
		// break lies inside it.
		rows := []struct {
			name, src string
			removal   []string // the text of Removal for each field, in order
			oneLine   []bool
		}{
			{"two fields", "Point { x: 1.0, y: 2.0 }", []string{"x: 1.0, ", ", y: 2.0"}, []bool{true, true}},
			{"three fields", "P { a: 1, b: 2, c: 3 }", []string{"a: 1, ", "b: 2, ", ", c: 3"}, []bool{true, true, true}},
			{"one field", "P { a: 1 }", []string{"a: 1"}, []bool{true}},
			{"trailing comma", "P { a: 1, b: 2, }", []string{"a: 1, ", "b: 2, "}, []bool{true, true}},
			{"tabs after the comma", "P { a: 1,\t\tb: 2 }", []string{"a: 1,\t\t", ",\t\tb: 2"}, []bool{true, true}},
			{"a pun", "P { a, b }", []string{"a, ", ", b"}, []bool{true, true}},
			{"over lines", "P {\n  a: 1,\n  b: 2\n}", []string{"  a: 1,", ",\n  b: 2"}, []bool{true, false}},
			{"the last one on its own line, with a comma", "P { a: 1,\n  b: 2,\n}", []string{"a: 1,", "  b: 2,"}, []bool{true, true}},
			{"a field that shares its line", "P {\n  a: 1, b: 2\n}", []string{"a: 1, ", ", b: 2"}, []bool{true, true}},
			{"tabs and CRLF", "P {\r\n\ta: 1,\r\n\tb: 2\r\n}", []string{"\ta: 1,", ",\r\n\tb: 2"}, []bool{true, false}},
			{"a value that spans lines", "P { a: (1 +\n 2), b: 3 }", []string{"a: (1 +\n 2), ", ", b: 3"}, []bool{false, true}},
		}
		for _, row := range rows {
			src := "fn f() { " + row.src + " }"
			m, id, _, sink := parseFile(t, src)
			if n := len(sink.All()); n != 0 {
				t.Errorf("%s: %d diagnostics: %v", row.name, n, sink.All())
				continue
			}
			fields := m.FieldInits(dataExprs(m, syntax.ExprRecord)[0].Fields)
			if len(fields) != len(row.removal) {
				t.Errorf("%s: %d fields, want %d", row.name, len(fields), len(row.removal))
				continue
			}
			for i, f := range fields {
				if f.Removal.File != id {
					t.Errorf("%s: field %d removes %v, in file %d, want file %d", row.name, i, f.Removal, f.Removal.File, id)
				}
				if got := src[f.Removal.Lo:f.Removal.Hi]; got != row.removal[i] || f.RemovalOneLine != row.oneLine[i] {
					t.Errorf("%s: field %d removes %q (one line %v), want %q (%v)", row.name, i, got, f.RemovalOneLine, row.removal[i], row.oneLine[i])
				}
			}
		}
	})

	t.Run("nesting_of_tuple_types_E0204", func(t *testing.T) {
		// Each `(` of a tuple type is a level of nesting, as in an expression: 256
		// are read, the 257th is one E0204, whatever follows it, and the type is
		// not read, so the compiler never goes deeper than the limit (Review Focus
		// 2: a type nested 10 000 times is one error, and the next item is read).
		tuple := func(depth int) string {
			return strings.Repeat("(", depth) + "Int" + strings.Repeat(", Int)", depth)
		}
		if _, _, diags := dataDiags(t, "fn f(x: "+tuple(syntax.MaxNesting)+") {}"); len(diags) != 0 {
			t.Errorf("%d levels of tuple type: diagnostics %+v, want none", syntax.MaxNesting, diags)
		}
		for _, depth := range []int{syntax.MaxNesting + 1, 10000} {
			src := "fn f(x: " + tuple(depth) + ") {}\nfn g() {}"
			m, id, in, sink := parseFile(t, src)
			lo := uint32(len("fn f(x: ") + syntax.MaxNesting)
			diags := sink.All()
			if len(diags) != 1 || diags[0].Code != diag.E(204) || diags[0].Primary.Span != (base.Span{File: id, Lo: lo, Hi: lo + 1}) {
				t.Errorf("%d levels: diagnostics %+v, want exactly one E0204 on the 257th `(`", depth, diags)
			} else if want := "split the type with `type` aliases"; diags[0].Help != want {
				// A type is not split with `let` bindings, which is the help of an expression.
				t.Errorf("%d levels: the help is %q, want %q", depth, diags[0].Help, want)
			} else if want := "type is nested too deeply"; diags[0].Message != want {
				// The title names what is too deep: a type is not an expression.
				t.Errorf("%d levels: the title is %q, want %q", depth, diags[0].Message, want)
			}
			if got := syntax.PrintModule(m, in); !strings.HasSuffix(got, "(fn g (params) (ret _) (block))\n") {
				t.Errorf("%d levels: the module ends with %q, want the function g after the broken one", depth, got[max(0, len(got)-60):])
			}
		}
		// A tuple type that is done gives its level back: 300 of them in a row, none
		// inside another, are not 300 levels. And a type that was abandoned is an
		// error node, so that no later pass reads a name that is not there: the one
		// E0204 is all the compiler says about it.
		var params []string
		for range 300 {
			params = append(params, "(Int, Bool)")
		}
		sequence := "fn f(a: " + strings.Join(params, ") {}\nfn g(a: ") + ") {}"
		if _, _, diags := dataDiags(t, sequence); len(diags) != 0 {
			t.Errorf("300 tuple types, none inside another: diagnostics %+v, want none", diags)
		}
		oneParam := "fn f(a: " + strings.Join(params, ", a: ") + ") {}"
		if _, _, diags := dataDiags(t, oneParam); len(diags) != 0 {
			t.Errorf("300 parameters with a tuple type: diagnostics %+v, want none", diags)
		}
		if p := testsupport.Analyze(t, "fn f(x: "+tuple(syntax.MaxNesting+1)+") {}\nfn main() {}"); len(testsupport.ErrorCodes(p)) != 1 {
			t.Errorf("a type of 257 levels: error codes %v, want only E0204", testsupport.ErrorCodes(p))
		}
		// The state of the abandoned type does not leak into what follows it: the
		// mistakes of the next statements of the same function, those inside an
		// expression and those outside one (a `let` with no name), are reported.
		src := "fn f() {\n  let x: " + tuple(syntax.MaxNesting+1) + " = 1\n  let = 2\n  let y = 1 +\n}"
		_, id, diags := dataDiags(t, src)
		eq, last := uint32(strings.Index(src, "let = 2")+len("let ")), uint32(strings.LastIndex(src, "}"))
		if len(diags) != 3 || diags[0].Code != diag.E(204) || diags[1].Code != diag.E(201) || diags[2].Code != diag.E(201) ||
			diags[1].Message != "expected identifier, found `=`" || diags[2].Message != "expected expression, found `}`" ||
			diags[1].Primary.Span != (base.Span{File: id, Lo: eq, Hi: eq + 1}) || diags[2].Primary.Span != (base.Span{File: id, Lo: last, Hi: last + 1}) {
			t.Errorf("a deep type then `let = 2` and `let y = 1 +`: diagnostics %+v, want E0204, then `expected identifier, found `=`` at the `=` "+
				"and `expected expression, found `}`` at the `}`", diags)
		}
	})

	t.Run("accesses_count_in_the_chain_E0205", func(t *testing.T) {
		// A `.` is a link of the chain like an operator or a call, so a statement
		// with 1024 accesses is read and with 1025 is one E0205, and `t.0.1` is two
		// links because it is two nodes.
		rows := []struct {
			name, chain string
			errors      bool
		}{
			{"1024 dots", strings.Repeat(".0", syntax.MaxChain), false},
			{"1025 dots", strings.Repeat(".0", syntax.MaxChain+1), true},
			{"1024 links in 512 floats", strings.Repeat(".0.1", syntax.MaxChain/2), false},
			{"1026 links in 513 floats", strings.Repeat(".0.1", syntax.MaxChain/2+1), true},
			{"a float over the limit", strings.Repeat(".0", syntax.MaxChain-1) + ".0.1", true},
			{"fields", strings.Repeat(".x", syntax.MaxChain+1), true},
		}
		for _, row := range rows {
			_, _, diags := dataDiags(t, "fn f() { t"+row.chain+" }")
			oneE0205 := len(diags) == 1 && diags[0].Code == diag.E(205)
			if row.errors && !oneE0205 {
				t.Errorf("%s: diagnostics %+v, want exactly one E0205", row.name, diags)
			}
			if !row.errors && len(diags) != 0 {
				t.Errorf("%s: diagnostics %+v, want none", row.name, diags)
			}
		}
		// The link that passes the limit is where the error is: the `.` of the 1025th
		// access, and, when it is the second index of a `0.1`, the token that has it.
		// (The prefix is made of fields: a run of `.0` is split into tokens by the lexer in
		// a way that does not put the 1025th link where a count of the dots would.)
		src := "fn f() { t" + strings.Repeat(".x", syntax.MaxChain) + ".x }"
		_, id, diags := dataDiags(t, src)
		if lo := uint32(strings.LastIndex(src, ".x")); len(diags) != 1 || diags[0].Primary.Span != (base.Span{File: id, Lo: lo, Hi: lo + 1}) {
			t.Errorf("the 1025th dot: diagnostics %+v, want one on that dot (byte %d)", diags, lo)
		}
		src = "fn f() { t" + strings.Repeat(".x", syntax.MaxChain-1) + ".0.1 }"
		_, id, diags = dataDiags(t, src)
		if lo := uint32(strings.LastIndex(src, "0.1")); len(diags) != 1 || diags[0].Primary.Span != (base.Span{File: id, Lo: lo, Hi: lo + 3}) {
			t.Errorf("the second index of a `0.1` over the limit: diagnostics %+v, want one on the token `0.1` (byte %d)", diags, lo)
		}
		// The count is of the statement: one more operator in front of 1024 accesses
		// is over the limit, and the accesses of another statement do not add up.
		_, _, diags = dataDiags(t, "fn f() { 1 + t"+strings.Repeat(".0", syntax.MaxChain)+" }")
		if len(diags) != 1 || diags[0].Code != diag.E(205) {
			t.Errorf("1 + 1024 accesses: diagnostics %+v, want one E0205", diags)
		}
		_, _, diags = dataDiags(t, "fn f() {\n  t"+strings.Repeat(".0", syntax.MaxChain)+"\n  u"+strings.Repeat(".0", syntax.MaxChain)+"\n}")
		if len(diags) != 0 {
			t.Errorf("two statements of 1024 accesses: diagnostics %+v, want none", diags)
		}
	})
}
