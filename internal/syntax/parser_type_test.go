package syntax_test

import (
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// This file is in package syntax_test and uses the helpers of recover_test.go
// (recParse, recE0201, recE0201Close, e0202…): a `type` declaration is read by
// the same parser, and what it does after a mistake is judged the same way.

// typeHelp is the help that the E0201 of a missing separator carries when a
// type body is being read: the three separators of the grammar (spec 5.3,
// `vsep`). what is "variants" or "fields".
func typeHelp(what string) string {
	return "separate the " + what + " with a line break, `;` or `,`"
}

// typeSep is the E0201 `newline` of two variants or fields with nothing between
// them, at sp.
func typeSep(sp base.Span, found, what string) diag.Diagnostic {
	d := recE0201(sp, "expected newline, found `"+found+"`")
	d.Help = typeHelp(what)
	return d
}

// eof is the empty span at the end of the file, where the Eof token is.
func (r *recRun) eof() base.Span { return r.span(len(r.src), len(r.src)) }

func TestParseType(t *testing.T) {
	t.Run("three_forms_three_separators", func(t *testing.T) {
		// A sum and a record take their items separated by a line break, a `;` or
		// a `,`; an alias has none. The same type in each separator prints the
		// same tree.
		const sum = "(type Shape (sum (Circle (radius Float)) (Rect (w Float) (h Float))))\n"
		const rec = "(type Point (record (x Float) (y Float)))\n"
		rows := []struct{ name, src, want string }{
			{"sum with line breaks", "type Shape {\n  Circle(radius: Float)\n  Rect(w: Float, h: Float)\n}", sum},
			{"sum with semicolons", "type Shape { Circle(radius: Float); Rect(w: Float, h: Float) }", sum},
			{"sum with commas", "type Shape { Circle(radius: Float), Rect(w: Float, h: Float) }", sum},
			{"sum with commas over lines", "type Shape {\n  Circle(radius: Float),\n  Rect(w: Float, h: Float)\n}", sum},
			{"sum with a semicolon and a line break", "type Shape {\n  Circle(radius: Float);\n  Rect(w: Float, h: Float)\n}", sum},
			{"record with line breaks", "type Point {\n  x: Float\n  y: Float\n}", rec},
			{"record with semicolons", "type Point { x: Float; y: Float }", rec},
			{"record with commas", "type Point { x: Float, y: Float }", rec},
			{"alias", "type Meters = Float", "(type Meters (alias Float))\n"},
			{"alias of unit", "type Nothing = ()", "(type Nothing (alias ()))\n"},
			{"alias with the type on the next line", "type Meters =\n  Float", "(type Meters (alias Float))\n"},
		}
		for _, row := range rows {
			wantModule(t, row.name, row.src, row.want)
		}
	})

	t.Run("a_record_may_have_one_field", func(t *testing.T) {
		// The form is the first token of the body, so a record of one field is
		// not a variant and a sum of one variant is not a record.
		wantModule(t, "record of one field", "type Wrapper { value: Int }", "(type Wrapper (record (value Int)))\n")
		wantModule(t, "sum of one variant", "type Wrapper { Wrapper(Int) }", "(type Wrapper (sum (Wrapper (_ Int))))\n")
		wantModule(t, "sum of one variant with no fields", "type Only { Only }", "(type Only (sum (Only)))\n")
	})

	t.Run("variants_with_and_without_fields", func(t *testing.T) {
		// A variant has no parentheses (`None`), or a list of fields that each
		// have a name (`radius: Float`) or do not (`Some(Int)`), and the two kinds
		// may be mixed in one variant: the grammar says so, and what to make of it
		// is for a later pass.
		rows := []struct{ name, src, want string }{
			{"no parentheses", "type O { None, Some(Int) }", "(type O (sum (None) (Some (_ Int))))\n"},
			{"positional fields", "type P { Pair(Int, Bool) }", "(type P (sum (Pair (_ Int) (_ Bool))))\n"},
			{"named fields", "type S { Rect(w: Float, h: Float) }", "(type S (sum (Rect (w Float) (h Float))))\n"},
			{"named and positional", "type S { Mix(a: Int, Bool) }", "(type S (sum (Mix (a Int) (_ Bool))))\n"},
			{"unit type as a field", "type S { Wrap(()) }", "(type S (sum (Wrap (_ ()))))\n"},
			{"a declared type as a field", "type S { Holds(Shape) }", "(type S (sum (Holds (_ Shape))))\n"},
			{"three variants of three kinds", "type S { A\n B(Int)\n C(x: Int, y: Int) }",
				"(type S (sum (A) (B (_ Int)) (C (x Int) (y Int))))\n"},
			{"field lists over lines", "type S { A(\n  x: Int,\n  y: Int\n)\n B }", "(type S (sum (A (x Int) (y Int)) (B)))\n"},
		}
		for _, row := range rows {
			wantModule(t, row.name, row.src, row.want)
		}
	})

	t.Run("a_separator_may_end_the_body", func(t *testing.T) {
		// A line break before the `}` is the usual last separator, and a `,` or a
		// `;` there is accepted too (Rust writes it). Only one.
		rows := []struct{ name, src, want string }{
			{"sum with a comma", "type S { A, B, }", "(type S (sum (A) (B)))\n"},
			{"sum with a semicolon", "type S { A; B; }", "(type S (sum (A) (B)))\n"},
			{"record with a comma", "type P { x: Int, y: Int, }", "(type P (record (x Int) (y Int)))\n"},
			{"record with a comma and a line break", "type P {\n  x: Int,\n  y: Int,\n}", "(type P (record (x Int) (y Int)))\n"},
		}
		for _, row := range rows {
			wantModule(t, row.name, row.src, row.want)
		}
		r := recParse(t, "type S { A,, B }")
		r.wantDiags(t, "two commas", recE0201(r.at(t, ",", 1), "expected identifier, found `,`"))
		r.wantText(t, "two commas", "(error)\n")
		r = recParse(t, "type P { x: Int,, y: Int }")
		r.wantDiags(t, "two commas in a record", recE0201(r.at(t, ",", 1), "expected identifier, found `,`"))
		r.wantText(t, "two commas in a record", "(error)\n")
	})

	t.Run("pub_and_attributes", func(t *testing.T) {
		rows := []struct{ name, src, want string }{
			{"pub", "pub type T = Int", "(pub type T (alias Int))\n"},
			{"attribute", "#[a(\"x\")] type T { A }", "(type T (attrs (a \"x\")) (sum (A)))\n"},
			{"attribute on its own line, then pub", "#[a]\npub type T { x: Int }", "(pub type T (attrs (a)) (record (x Int)))\n"},
			{"after a function", "fn f() {}\ntype T = Int\nfn g() {}",
				"(fn f (params) (ret _) (block))\n(type T (alias Int))\n(fn g (params) (ret _) (block))\n"},
		}
		for _, row := range rows {
			wantModule(t, row.name, row.src, row.want)
		}
	})

	t.Run("tree_shape", func(t *testing.T) {
		// The tree keeps what the dump hides: spans, which field has a name, and
		// where each list lies in its pool (the lists of two items do not mix).
		const src = "#[a]\npub type Shape {\n  Circle(radius: Float)\n  Rect(Float)\n  Unit\n}\ntype P { x: Int }\ntype M = ()"
		r := recParse(t, src)
		r.wantDiags(t, "tree")
		m := r.m
		if len(m.Items) != 3 {
			t.Fatalf("%d items, want 3", len(m.Items))
		}
		shape, point, alias := m.ItemNodes.Get(m.Items[0]), m.ItemNodes.Get(m.Items[1]), m.ItemNodes.Get(m.Items[2])
		for i, it := range []syntax.Item{shape, point, alias} {
			if it.Kind != syntax.ItemType {
				t.Errorf("item %d is kind %d, want ItemType", i, it.Kind)
			}
		}

		if !shape.Pub || shape.Form != syntax.TypeFormSum || r.in.String(shape.Name) != "Shape" ||
			shape.NameSpan != r.at(t, "Shape", 0) || shape.Attrs.Count != 1 {
			t.Errorf("Shape is %+v", shape)
		}
		whole := r.span(0, strings.Index(src, "}")+1)
		if shape.Span != whole {
			t.Errorf("Shape spans %v, want %v: from the `#` to the `}`", shape.Span, whole)
		}
		if shape.Fields.Count != 0 {
			t.Errorf("a sum has no fields of its own, got %v", shape.Fields)
		}
		variants := m.Variants(shape.Variants)
		if len(variants) != 3 {
			t.Fatalf("Shape has %d variants, want 3", len(variants))
		}
		wantNames := []string{"Circle", "Rect", "Unit"}
		wantSpans := []string{"Circle(radius: Float)", "Rect(Float)", "Unit"}
		for i, v := range variants {
			if r.in.String(v.Name) != wantNames[i] || v.NameSpan != r.at(t, wantNames[i], 0) || v.Span != r.at(t, wantSpans[i], 0) {
				t.Errorf("variant %d is %+v, want %s spanning %q", i, v, wantNames[i], wantSpans[i])
			}
		}
		if got := variants[2].Fields.Count; got != 0 {
			t.Errorf("Unit has %d fields, want 0", got)
		}
		radius := m.Fields(variants[0].Fields)
		if len(radius) != 1 || !radius[0].HasName || r.in.String(radius[0].Name) != "radius" ||
			radius[0].NameSpan != r.at(t, "radius", 0) || radius[0].Span != r.at(t, "radius: Float", 0) {
			t.Errorf("the field of Circle is %+v", radius)
		} else if ty := m.Types.Get(radius[0].Type); ty.Kind != syntax.TypeNamed || ty.Span != r.at(t, "Float", 0) {
			t.Errorf("the type of radius is %+v", ty)
		}
		positional := m.Fields(variants[1].Fields)
		if len(positional) != 1 || positional[0].HasName || positional[0].Name != base.NoSymbol ||
			positional[0].NameSpan != (base.Span{}) || positional[0].Span != r.at(t, "Float", 1) {
			t.Errorf("the field of Rect is %+v, want one with no name spanning its type", positional)
		}

		if point.Form != syntax.TypeFormRecord || point.Variants.Count != 0 || point.Alias != 0 || point.Pub || point.Attrs.Count != 0 {
			t.Errorf("P is %+v", point)
		}
		fields := m.Fields(point.Fields)
		if len(fields) != 1 || !fields[0].HasName || fields[0].Span != r.at(t, "x: Int", 0) || fields[0].NameSpan != r.at(t, "x", 0) {
			t.Errorf("the fields of P are %+v", fields)
		}
		// The two lists of fields (Circle's, Rect's, then P's) are one after the
		// other in the pool, and each is its own.
		if point.Fields.First != 2 || len(m.FieldPool) != 3 {
			t.Errorf("P's fields are %v in a pool of %d, want the third of three", point.Fields, len(m.FieldPool))
		}

		if alias.Form != syntax.TypeFormAlias || alias.Variants.Count != 0 || alias.Fields.Count != 0 ||
			m.Types.Get(alias.Alias).Kind != syntax.TypeUnit || m.Types.Get(alias.Alias).Span != r.at(t, "()", 0) {
			t.Errorf("M is %+v", alias)
		}
		if alias.Span != r.at(t, "type M = ()", 0) || alias.NameSpan != r.at(t, "M", 0) {
			t.Errorf("M spans %v with its name at %v", alias.Span, alias.NameSpan)
		}
	})

	t.Run("form_is_the_first_token_of_the_body", func(t *testing.T) {
		// A capital letter is a variant, a small one a record field, and nothing
		// else starts a body. The empty body is the mistake the plan names:
		// `expected identifier`, at the `}`.
		rows := []struct{ name, src, found string }{
			{"empty", "type A {}", "}"},
			{"empty over lines", "type A {\n}", "}"},
			{"a number", "type A { 1 }", "1"},
			{"a separator", "type A { , }", ","},
			{"an alias sign", "type A { = }", "="},
			{"a keyword", "type A { let }", "let"},
		}
		for _, row := range rows {
			r := recParse(t, row.src)
			r.wantDiags(t, row.name, recE0201(r.at(t, row.found, 0), "expected identifier, found `"+row.found+"`"))
			r.wantText(t, row.name, "(error)\n")
			if it := r.m.ItemNodes.Get(r.m.Items[0]); it.Kind != syntax.ItemError || it.ErrorOf != syntax.ItemType ||
				r.in.String(it.Name) != "A" || it.NameSpan != r.at(t, "A", 0) {
				t.Errorf("%s: the item is %+v, want an error that keeps the name A of a type", row.name, it)
			}
		}
		// A small letter that is not followed by `:` is a record field that has
		// no type, and not a variant spelled wrong.
		// The help says what a field and a variant look like, since the name may be
		// either, and offers no edit.
		withHelp := func(d diag.Diagnostic) diag.Diagnostic {
			d.Help = "a field is written `name: Type`, and a variant starts with an uppercase letter"
			return d
		}
		r := recParse(t, "type A { circle }")
		r.wantDiags(t, "a variant in small letters", withHelp(recE0201(r.at(t, "}", 0), "expected `:`, found `}`")))
		r.wantText(t, "a variant in small letters", "(error)\n")
		r = recParse(t, "type A { circle(r: Float) }")
		r.wantDiags(t, "a variant in small letters with fields", withHelp(recE0201(r.at(t, "(", 0), "expected `:`, found `(`")))
		r.wantText(t, "a variant in small letters with fields", "(error)\n")
		r = recParse(t, "type Color { red, green, blue }")
		r.wantDiags(t, "three variants in small letters", withHelp(recE0201(r.at(t, ",", 0), "expected `:`, found `,`")))
		r = recParse(t, "type P { x Int }")
		r.wantDiags(t, "a field with no colon", withHelp(recE0201(r.at(t, "Int", 0), "expected `:`, found `Int`")))
	})

	t.Run("mistakes_in_a_type_are_one_error_each", func(t *testing.T) {
		// Each row is one mistake and has one diagnostic, exactly; the item is an
		// error that keeps the name of the type when the name was read.
		rows := []struct {
			name, src string
			want      func(r *recRun) diag.Diagnostic
			named     bool
		}{
			{"no name at the end of the file", "type", func(r *recRun) diag.Diagnostic {
				return recE0201(r.eof(), "expected identifier, found end of file")
			}, false},
			// A type starts in uppercase: the help says it, and the edit writes the same
			// word with a capital, except for a name that starts with `_`.
			{"a name in small letters", "type shape { Circle(r: Float) }", func(r *recRun) diag.Diagnostic {
				at := r.at(t, "shape", 0)
				return recE0201(at, "expected identifier, found `shape`").
					WithHelp("a type name starts with an uppercase letter").WithFixIt(diag.Replace(at, "Shape"))
			}, false},
			{"a name that starts with an underscore", "type _a {}", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, "_a", 0), "expected identifier, found `_a`").WithHelp("a type name starts with an uppercase letter")
			}, false},
			{"no body at the end of the file", "type A", func(r *recRun) diag.Diagnostic {
				return recE0201(r.eof(), "expected `{` or `=`, found end of file")
			}, true},
			{"a number instead of a body", "type A 1", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, "1", 0), "expected `{` or `=`, found `1`")
			}, true},
			{"type parameters are not read yet", "type A<T> { B(T) }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, "<", 0), "expected `{` or `=`, found `<`")
			}, true},
			{"the brace on the next line", "type A\n{ B }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, "\n", 0), "expected `{` or `=`, found newline")
			}, true},
			{"alias with nothing after the sign", "type A =", func(r *recRun) diag.Diagnostic {
				return recE0201(r.eof(), "expected type, found end of file")
			}, true},
			{"alias of a name in small letters", "type A = a", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, "a", 0), "expected type, found `a`")
			}, true},
			{"a field with no type", "type A { B(Int, ) }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, ")", 0), "expected type, found `)`")
			}, true},
			{"empty parentheses", "type A { B() }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, ")", 0), "expected type, found `)`")
			}, true},
			{"a named field with no type", "type A { B(x: ) }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, ")", 0), "expected type, found `)`")
			}, true},
			{"a name with no colon", "type A { B(x) }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, ")", 0), "expected `:`, found `)`")
			}, true},
			{"a record field with no type", "type A { x: }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, "}", 0), "expected type, found `}`")
			}, true},
			{"a parenthesis never closed", "type A { B(Int }", func(r *recRun) diag.Diagnostic {
				return recE0201Close(r.at(t, "}", 0), "expected `)`, found `}`", r.at(t, "(", 0), "(", uint32(strings.Index(r.src, " }")))
			}, true},
			{"two variants on a line", "type A { B C }", func(r *recRun) diag.Diagnostic {
				return typeSep(r.at(t, "C", 0), "C", "variants")
			}, true},
			{"two fields on a line", "type A { x: Int y: Int }", func(r *recRun) diag.Diagnostic {
				y := strings.Index(r.src, "y:")
				return typeSep(r.span(y, y+1), "y", "fields")
			}, true},
			// A field written with a capital reads as a variant, and the `:` is what
			// does not fit: the help is the rule that tells a field from a variant.
			{"a field with a capital in a sum", "type A { X: Int }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, ":", 0), "expected newline, found `:`").
					WithHelp("a field is written `name: Type`, and a variant starts with an uppercase letter")
			}, true},
			{"a field in a sum", "type A { B, x: Int }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, "x", 0), "expected identifier, found `x`")
			}, true},
			{"a variant in a record", "type A { x: Int, B }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, "B", 0), "expected identifier, found `B`")
			}, true},
			{"a variant name in small letters after the first", "type A { B\n c }", func(r *recRun) diag.Diagnostic {
				return recE0201(r.at(t, "c", 0), "expected identifier, found `c`")
			}, true},
			{"nothing after the brace", "type A {", func(r *recRun) diag.Diagnostic {
				return r.e0202(r.at(t, "{", 0), "{")
			}, true},
			{"the body never closed, in a sum", "type A { B", func(r *recRun) diag.Diagnostic {
				return r.e0202(r.at(t, "{", 0), "{")
			}, true},
			{"the body never closed, in a record", "type A { x: Int", func(r *recRun) diag.Diagnostic {
				return r.e0202(r.at(t, "{", 0), "{")
			}, true},
			{"the body never closed, after a separator", "type A { B,", func(r *recRun) diag.Diagnostic {
				return r.e0202(r.at(t, "{", 0), "{")
			}, true},
			{"the field list never closed", "type A { B(Int", func(r *recRun) diag.Diagnostic {
				return r.e0202(r.at(t, "(", 0), "(")
			}, true},
		}
		for _, row := range rows {
			r := recParse(t, row.src)
			r.wantDiags(t, row.name, row.want(r))
			r.wantText(t, row.name, "(error)\n")
			it := r.m.ItemNodes.Get(r.m.Items[0])
			if it.Kind != syntax.ItemError {
				t.Errorf("%s: the item is kind %d, want ItemError", row.name, it.Kind)
				continue
			}
			if row.named != (it.Name != base.NoSymbol) {
				t.Errorf("%s: the error keeps the name %v, want a name: %v", row.name, it.Name, row.named)
			}
			if row.named && it.ErrorOf != syntax.ItemType {
				t.Errorf("%s: the error is of kind %d, want a type", row.name, it.ErrorOf)
			}
		}
	})

	t.Run("a_body_missing_its_brace_is_the_brace", func(t *testing.T) {
		// Another item starts where an item of the body should: the `}` is what is
		// missing, and the error says so as it does for a literal, with the `{` that
		// is still open and the edit that writes the `}` right after the last token
		// read, and not that an identifier was expected at the next item.
		for _, row := range []struct{ name, src, found, last string }{
			{"a sum", "type A {\n  B\n  C(Int)\n\nfn f() {}", "fn", "C(Int)"},
			{"a record", "type A {\n  x: Int\n  y: Int\nfn f() {}", "fn", "y: Int"},
			{"a body with nothing in it", "type A {\nfn f() {}", "fn", "{"},
			{"followed by a type", "type A { B\ntype C = Int", "type", "B"},
			{"an attribute starts the next item", "type A { B\n#[extern(\"x\")] fn f() {}", "#", "B"},
		} {
			r := recParse(t, row.src)
			found := strings.Index(r.src, "\n"+row.found) + 1
			at := strings.Index(r.src, row.last) + len(row.last)
			want := recE0201Opened(r.span(found, found+len(row.found)), "expected `}`, found `"+row.found+"`", r.at(t, "{", 0), "{")
			want.Help = "close it with `}`"
			want.Fixits = []diag.FixIt{{Span: base.Span{File: want.Primary.Span.File, Lo: uint32(at), Hi: uint32(at)}, Replacement: "}"}}
			r.wantDiags(t, row.name, want)
		}
	})

	t.Run("a_broken_type_keeps_what_was_read", func(t *testing.T) {
		// The name of the type, and the names of the variants read before the
		// mistake, outlive it: name resolution needs them to say nothing more
		// about a use of them. The variant the mistake is in is one of them.
		r := recParse(t, "type Shape { Circle(r: Float), Rect( }")
		it := r.m.ItemNodes.Get(r.m.Items[0])
		var names []string
		for _, v := range r.m.Variants(it.Variants) {
			names = append(names, r.in.String(v.Name))
		}
		if strings.Join(names, " ") != "Circle Rect" {
			t.Errorf("the error item keeps the variants %q, want `Circle Rect`", names)
		}
		if it.Fields.Count != 0 || it.Alias != 0 {
			t.Errorf("an error item has fields %v and alias %v, want none", it.Fields, it.Alias)
		}
		// It covers what was consumed, which is the whole body down to the `}`: the
		// parser skips what is left of the broken variant to read the ones after
		// it. The variants have their spans: the one that was read whole covers its
		// fields, and the one that broke is its name alone.
		if it.Span != r.at(t, "type Shape { Circle(r: Float), Rect( }", 0) || it.NameSpan != r.at(t, "Shape", 0) {
			t.Errorf("the error item spans %v with its name at %v", it.Span, it.NameSpan)
		}
		variants := r.m.Variants(it.Variants)
		// The variants of the error item are in the pool once, and the fields of
		// a variant that could not be read are not in it at all.
		if len(r.m.VariantPool) != 2 || len(r.m.FieldPool) != 1 {
			t.Errorf("the pools hold %d variants and %d fields, want 2 and 1 (Circle's)", len(r.m.VariantPool), len(r.m.FieldPool))
		}
		if len(variants) == 2 {
			circle, rect := variants[0], variants[1]
			if circle.Span != r.at(t, "Circle(r: Float)", 0) || circle.NameSpan != r.at(t, "Circle", 0) || circle.Fields.Count != 1 {
				t.Errorf("Circle is %+v, want it whole", circle)
			}
			if rect.Span != r.at(t, "Rect", 0) || rect.NameSpan != rect.Span || rect.Fields.Count != 0 {
				t.Errorf("Rect is %+v, want its name alone", rect)
			}
		}
		// A mistake inside a variant ends that variant and nothing else: the ones
		// after it are read, each with the name it was written with, so one mistake
		// is one diagnostic and a use of a later variant is not an unknown name as
		// well. What ends the variant is a line break, a `;` or a `,` outside its
		// parentheses, and the `}` of the body.
		for _, row := range []struct{ name, src, want string }{
			{"two on a line, and another after", "type S {\n  A B\n  C\n}", "A B C"},
			{"after a line break", "type Shape {\n  Circle(r: Float)\n  Rect(w: Float h: Float)\n  Tri(a: Float)\n}", "Circle Rect Tri"},
			{"after a comma", "type S { A(x: Int y: Int), B }", "A B"},
			{"after a semicolon", "type S { A(x: Int y: Int); B }", "A B"},
			{"fields after the mistake", "type S {\n  A(x: Int y: Int, z: Int)\n  B\n}", "A B"},
			{"a field with no type and fields after it", "type S { A(x: , a: Int, b: Int), B }", "A B"},
			{"a name that is not a variant", "type S {\n  A\n  1 2\n  B\n}", "A B"},
			// The `}` that closes a brace the skipped text opened is not the end of the
			// body: the variant after it is still inside, and the `}` at the end of
			// the type is the one that closes it.
			{"a brace closed inside what is skipped", "type S {\n  A(x: Int, { 1 })\n  B\n}", "A B"},
			{"the last one, at the end of the file", "type S { A, B(x: Int y: Int) }", "A B"},
			// The header is the mistake and the body is still read: the `{` below the
			// name (a brace style that is not Ki's), after blank lines, or after a word
			// that does not belong there.
			{"the brace on the line below the name", "type S\n{\n  A\n  B(x: Int)\n}", "A B"},
			{"blank lines before the brace", "type S\n\n\n{ A, B }", "A B"},
			{"a word before the brace", "type S x {\n  A\n  B\n}", "A B"},
		} {
			r := recParse(t, row.src)
			if n := len(r.sink.All()); n != 1 {
				t.Errorf("%s: %d diagnostics, want exactly 1: %v", row.name, n, r.sink.All())
			}
			r.wantText(t, row.name, "(error)\n")
			var names []string
			for _, v := range r.m.Variants(r.m.ItemNodes.Get(r.m.Items[0]).Variants) {
				names = append(names, r.in.String(v.Name))
			}
			if got := strings.Join(names, " "); got != row.want {
				t.Errorf("%s: the error item keeps the variants %q, want %q", row.name, got, row.want)
			}
		}
		// A record that cannot be read leaves nothing in the pools either.
		r = recParse(t, "type P { x: Int, y: }")
		if len(r.m.FieldPool) != 0 || len(r.m.VariantPool) != 0 {
			t.Errorf("a broken record left %d fields and %d variants in the pools, want none", len(r.m.FieldPool), len(r.m.VariantPool))
		}
		// An alias that cannot be read keeps the name too, and covers up to the sign.
		r = recParse(t, "type A = 1")
		if it := r.m.ItemNodes.Get(r.m.Items[0]); it.Span != r.at(t, "type A =", 0) || it.NameSpan != r.at(t, "A", 0) || it.ErrorOf != syntax.ItemType {
			t.Errorf("the broken alias is %+v, want an error of a type spanning `type A =` named A", it)
		}
		// A function whose signature broke keeps its name and no variants.
		r = recParse(t, "fn f(x y) {}")
		if it := r.m.ItemNodes.Get(r.m.Items[0]); it.ErrorOf != syntax.ItemFn || it.Variants.Count != 0 {
			t.Errorf("a broken function is %+v", it)
		}
	})

	t.Run("the_item_after_a_broken_type_is_read", func(t *testing.T) {
		// The mistake is one diagnostic and the next item comes out as if nothing
		// had happened: the skip stops at the next `fn` or `type` outside the
		// brackets that the skipped text opened.
		rows := []struct{ name, src, want string }{
			{"function", "type Shape { Circle( }\nfn f() {}", "(error)\n(fn f (params) (ret _) (block))\n"},
			{"type", "type Shape { Circle(\ntype P = Int", "(error)\n(type P (alias Int))\n"},
			{"after a mistake in the body", "type S { A\n 1 2 3 }\nfn f() {}", "(error)\n(fn f (params) (ret _) (block))\n"},
			// What the skip passes over may open brackets, and an item start inside
			// them is not the next item, in the body of a sum and after an alias.
			{"brackets opened in the body", "type S { A\n 1 ( type B = Int ) }\nfn f() {}", "(error)\n(fn f (params) (ret _) (block))\n"},
			{"braces opened in the body", "type S { A\n 1 { fn h() {} } }\nfn f() {}", "(error)\n(fn f (params) (ret _) (block))\n"},
			{"brackets opened after an alias", "type A = 1 ( type B = Int )\nfn f() {}", "(error)\n(fn f (params) (ret _) (block))\n"},
			{"the next item is on the following line", "type A = 1\nfn f() {}\ntype B { x: Int }",
				"(error)\n(fn f (params) (ret _) (block))\n(type B (record (x Int)))\n"},
			{"pub item", "type A\npub fn f() {}", "(error)\n(pub fn f (params) (ret _) (block))\n"},
			// With no body of its own, a type does not take the next item's.
			{"no body and a function", "type A\nfn f() {}", "(error)\n(fn f (params) (ret _) (block))\n"},
			{"a word and no body", "type A x\nfn f() {}", "(error)\n(fn f (params) (ret _) (block))\n"},
			{"an item on the same line", "type A fn f() {}", "(error)\n(fn f (params) (ret _) (block))\n"},
			{"a comment line before the brace", "type A\n// the body\n{ B }\nfn f() {}", "(error)\n(fn f (params) (ret _) (block))\n"},
			{"the body is read and the item after it", "type A\n{ B }\nfn f() {}", "(error)\n(fn f (params) (ret _) (block))\n"},
		}
		for _, row := range rows {
			r := recParse(t, row.src)
			r.wantText(t, row.name, row.want)
			if n := len(r.sink.All()); n != 1 {
				t.Errorf("%s: %d diagnostics, want exactly 1: %v", row.name, n, r.sink.All())
			}
		}
	})

	t.Run("a_type_is_read_as_a_type_after_garbage", func(t *testing.T) {
		// `type` stops the skip of a broken item as `fn` does, and is read as an
		// item: it used to be an error of its own (TestRecover).
		r := recParse(t, "1 2 3 4 type A { B } fn g() {}")
		r.wantDiags(t, "garbage then type", recE0201(r.at(t, "1", 0), "expected item, found `1`"))
		r.wantText(t, "garbage then type", "(error)\n(type A (sum (B)))\n(fn g (params) (ret _) (block))\n")
	})

	t.Run("big_declarations_are_read_without_recursion", func(t *testing.T) {
		// Review Focus 2: a type with 10 000 variants, and a record of 10 000
		// fields. Every list of the parser is a loop, so the size of the type
		// costs no stack: the whole test runs with the stack of Go capped at 1 MiB,
		// past which Go does not panic but ends the test binary (the tests of this
		// package do not run in parallel).
		defer debug.SetMaxStack(debug.SetMaxStack(1 << 20))
		const n = 10000
		var sum, record strings.Builder
		sum.WriteString("type Big {\n")
		record.WriteString("type Wide {\n")
		for i := range n {
			sum.WriteString("  V" + strconv.Itoa(i) + "(Int, x: Bool)\n")
			record.WriteString("  f" + strconv.Itoa(i) + ": Int\n")
		}
		sum.WriteString("}\n")
		record.WriteString("}\n")

		r := recParse(t, sum.String()+record.String())
		r.wantDiags(t, "big")
		big, wide := r.m.ItemNodes.Get(r.m.Items[0]), r.m.ItemNodes.Get(r.m.Items[1])
		variants := r.m.Variants(big.Variants)
		if len(variants) != n || r.in.String(variants[0].Name) != "V0" || r.in.String(variants[n-1].Name) != "V9999" {
			t.Fatalf("Big has %d variants, want %d from V0 to V9999", len(variants), n)
		}
		if got := len(r.m.Fields(variants[n-1].Fields)); got != 2 {
			t.Errorf("the last variant has %d fields, want 2", got)
		}
		if got := len(r.m.FieldPool); got != 2*n+n {
			t.Errorf("the field pool has %d entries, want %d", got, 3*n)
		}
		fields := r.m.Fields(wide.Fields)
		if len(fields) != n || r.in.String(fields[n-1].Name) != "f9999" {
			t.Errorf("Wide has %d fields, want %d ending in f9999", len(fields), n)
		}
		if got := strings.Count(r.text(), "\n"); got != 2 {
			t.Errorf("the dump has %d lines, want 2 (one for each type)", got)
		}
	})
}
