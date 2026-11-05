package sema_test

import (
	"slices"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/sema"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here are about the names that `type` declarations bring: a type is
// a definition in a name space of its own, a constructor one in the space of the
// values, and what a type annotation stands for. wantDiags, spanIn, unknownType,
// wantCodes, defsOf and resolveSources are the helpers of check_test.go,
// scope_test.go and resolve_test.go, in the same package.

// redefined is the E0302 for the name that is defined again at at, with the
// first definition at first.
func redefined(name string, at, first base.Span) diag.Diagnostic {
	return diag.NewError(diag.E(302), "`"+name+"` is defined more than once", diag.Label{Span: at, Message: "redefined here"}).
		WithSecondary(diag.Label{Span: first, Message: "first defined here"}).
		WithHelp("rename one of them, or delete the second")
}

// checkSources resolves and checks a prelude and a user file that the test
// writes, as resolveSources and Check do together, and returns what the passes
// reported. It is for the rules that the real prelude, which has no types,
// cannot show.
func checkSources(t *testing.T, prelude, user string) []diag.Diagnostic {
	t.Helper()
	rp, mods, in, sink := resolveSources(t, prelude, user)
	sema.Check(mods, rp, sema.Declare(mods, rp, in, sink), in, sink)
	return sink.All()
}

func TestTypeNames(t *testing.T) {
	t.Run("types_and_constructors_are_definitions", func(t *testing.T) {
		// A type is a definition and so is each of its variants, one after the
		// other in source order; a record and an alias have no constructor. A
		// constructor names the item of its type, and is `pub` as the type is.
		const src = "type Shape {\n  Circle(r: Float)\n  Rect(w: Float, h: Float)\n  Unit\n}\n" +
			"pub type Point { x: Float }\ntype Meters = Float\npub type Flag { On, Off }\nfn main() {}"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		m, in := p.Frontend.Modules[p.Frontend.User], p.Session.Interner
		type def struct {
			name string
			kind sema.DefKind
			item int
			pub  bool
			at   string // the name, whose first occurrence in the source is where it is defined
		}
		want := []def{
			{"Shape", sema.DefType, 0, false, "Shape"},
			{"Circle", sema.DefCtor, 0, false, "Circle"},
			{"Rect", sema.DefCtor, 0, false, "Rect"},
			{"Unit", sema.DefCtor, 0, false, "Unit"},
			{"Point", sema.DefType, 1, true, "Point"},
			{"Meters", sema.DefType, 2, false, "Meters"},
			{"Flag", sema.DefType, 3, true, "Flag"},
			{"On", sema.DefCtor, 3, true, "On"},
			{"Off", sema.DefCtor, 3, true, "Off"},
			{"main", sema.DefFn, 4, false, "main"},
		}
		got := defsOf(p.Frontend.Resolved, p.Frontend.User)
		if len(got) != len(want) {
			t.Fatalf("the file has %d definitions, want %d: %+v", len(got), len(want), got)
		}
		for i, w := range want {
			lo := indexFrom(src, w.at, 0)
			wantDef := sema.Def{
				Name: in.InternString(w.name), Kind: w.kind, Module: p.Frontend.User, Item: m.Items[w.item], Pub: w.pub,
				Span: base.Span{File: m.File, Lo: uint32(lo), Hi: uint32(lo + len(w.at))},
			}
			if got[i] != wantDef {
				t.Errorf("definition %d is %+v, want %+v", i, got[i], wantDef)
			}
		}
		// A type has the type that its name stands for, a constructor the type of
		// a value (a function when it has fields) and `main` its own.
		typed := p.Frontend.Typed
		wantTypes := map[string]string{
			"Shape": "Shape", "Circle": "(Float) -> Shape", "Rect": "(Float, Float) -> Shape", "Unit": "Shape",
			"Point": "Point", "Meters": "Float", "Flag": "Flag", "On": "Flag", "Off": "Flag", "main": "() -> ()",
		}
		for id, d := range p.Frontend.Resolved.Defs.All() {
			if d.Module != p.Frontend.User {
				continue
			}
			if got, want := typed.Types.Show(typed.DefTypes.Get(id)), wantTypes[in.String(d.Name)]; got != want {
				t.Errorf("%s has the type %s, want %s", in.String(d.Name), got, want)
			}
		}
	})

	t.Run("a_type_and_a_constructor_may_share_a_name", func(t *testing.T) {
		// Types are looked up in their own space and constructors with the
		// values, so `Circle` is a type and a constructor at once and neither is
		// defined twice. In an expression it is the constructor; in a type
		// annotation, the type.
		const src = "type Shape { Circle(r: Float) }\ntype Circle { x: Float }\nfn f(c: Circle) {}\nfn main() { let c = Circle }"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		res := userRes(t, p, "Circle")
		if res.Kind != sema.ResDef || p.Frontend.Resolved.Defs.Get(res.Def).Kind != sema.DefCtor {
			t.Errorf("Circle in an expression resolved to %+v, want the constructor", res)
		}
		// Only the constructor is in the space of the values: the second
		// definition that is called Circle is the type, and is not a repeat.
		rp, in := p.Frontend.Resolved, p.Session.Interner
		got, ok := rp.LookupType(p.Frontend.User, in.InternString("Circle"))
		if !ok || got.Kind != sema.ResDef || rp.Defs.Get(got.Def).Kind != sema.DefType {
			t.Errorf("Circle as a type is %+v (found %v), want the type", got, ok)
		}
		if _, ok := rp.LookupType(p.Frontend.User, in.InternString("Shape")); !ok {
			t.Error("Shape is not found as a type")
		}
	})

	t.Run("a_type_is_not_a_value_and_a_constructor_is_not_a_type", func(t *testing.T) {
		// `Shape` in an expression is an unknown constructor (E0306): types are not
		// in the space of values, and a name that starts in uppercase can only be
		// a constructor. `A` in an annotation is an unknown type (E0406):
		// constructors are not in the space of types. Neither has a suggestion.
		const value = "type Shape { A }\nfn main() { Shape }"
		p := testsupport.Analyze(t, value)
		wantDiags(t, "a type as a value", p, diag.NewError(diag.E(306), "unknown constructor `Shape`",
			diag.Label{Span: spanIn(t, p, value, "{ Shape }", "Shape"), Message: "not a constructor in this scope"}))
		const ctor = "type Shape { A }\nfn f(a: A) {}\nfn main() {}"
		p = testsupport.Analyze(t, ctor)
		wantDiags(t, "a constructor as a type", p, unknownType(spanIn(t, p, ctor, ": A", "A"), "A", ""))

		// A type is not a candidate for an unknown constructor either, even when it
		// is the very first definition of the program, which has the id 0 that the
		// space of the values answers for a name it does not have: the prelude
		// here declares `Maybe` first, and `Mayb` is not asked "did you mean
		// `Maybe`?".
		_, _, _, sink := resolveSources(t, "pub type Maybe { Just }\n", "fn main() { Mayb }\n")
		all := sink.All()
		if len(all) != 1 || all[0].Code != diag.E(306) || all[0].Help != "" || len(all[0].Fixits) != 0 {
			t.Errorf("a type is suggested for a constructor: %+v", all)
		}
		// Nor for an unknown name that starts in lowercase, which is one letter
		// from the type (`maybe`, E0301): the list of those leaves out the types.
		_, _, _, sink = resolveSources(t, "pub type Maybe { Just }\n", "fn main() { maybe }\n")
		all = sink.All()
		if len(all) != 1 || all[0].Code != diag.E(301) || all[0].Help != "" || len(all[0].Fixits) != 0 {
			t.Errorf("a type is suggested for a value: %+v", all)
		}
	})

	t.Run("duplicate_type_E0302", func(t *testing.T) {
		// A type defined twice is the same mistake as a function defined twice,
		// whatever form each has: the second is reported, the first is where to
		// look, and a use of the name stands for the first.
		rows := []struct{ name, src, second string }{
			{"two sums", "type Shape { A }\ntype Shape { B }\nfn main() {}", "type Shape { B }"},
			{"a sum and a record", "type Shape { A }\ntype Shape { x: Int }\nfn main() {}", "type Shape { x: Int }"},
			{"a record and an alias", "type Shape { x: Int }\ntype Shape = Int\nfn main() {}", "type Shape = Int"},
		}
		for _, row := range rows {
			p := testsupport.Analyze(t, row.src)
			file := p.Frontend.Modules[p.Frontend.User].File
			first := base.Span{File: file, Lo: 5, Hi: 10}
			lo := uint32(indexFrom(row.src, row.second, 0) + 5)
			wantDiags(t, row.name, p, redefined("Shape", base.Span{File: file, Lo: lo, Hi: lo + 5}, first))
		}
		// Both are definitions, and the second one's constructors still exist:
		// `B` is not an unknown name, and a use of `Shape` is not an unknown type.
		p := testsupport.Analyze(t, "type Shape { A }\ntype Shape { B }\nfn f(s: Shape) { let b = B }\nfn main() {}")
		wantCodes(t, p, "E0302")
		if got := len(defsOf(p.Frontend.Resolved, p.Frontend.User)); got != 6 {
			t.Errorf("the file has %d definitions, want 6 (two types, two constructors, f and main)", got)
		}
		// A use of the name stands for the first: the types hold the definition at
		// the start of the file, not the repeat.
		rp, in := p.Frontend.Resolved, p.Session.Interner
		got, ok := rp.LookupType(p.Frontend.User, in.InternString("Shape"))
		if !ok || got.Kind != sema.ResDef || rp.Defs.Get(got.Def).Span.Lo != 5 {
			t.Errorf("Shape as a type is %+v (found %v), want the first definition, whose name is at byte 5", got, ok)
		}
	})

	t.Run("duplicate_constructor_E0302", func(t *testing.T) {
		// A constructor defined twice, in one type or in two, is E0302 at the
		// second, with the first as the secondary label.
		rows := []struct{ name, src, context string }{
			{"in one type", "type S { A, B, A }\nfn main() {}", "B, A }"},
			{"in two types", "type S { A }\ntype T { A }\nfn main() {}", "T { A }"},
			{"a unit and one with fields", "type S { A(Int) }\ntype T { A }\nfn main() {}", "T { A }"},
		}
		for _, row := range rows {
			p := testsupport.Analyze(t, row.src)
			file := p.Frontend.Modules[p.Frontend.User].File
			first := indexFrom(row.src, "A", 0)
			second := indexFrom(row.src, row.context, 0) + len(row.context) - len(" }") - 1
			wantDiags(t, row.name, p, redefined("A",
				base.Span{File: file, Lo: uint32(second), Hi: uint32(second + 1)},
				base.Span{File: file, Lo: uint32(first), Hi: uint32(first + 1)}))
		}
	})

	t.Run("unknown_type_E0406", func(t *testing.T) {
		// The example of the plan: a type that is spelled almost like a declared
		// one gets the question and the edit, and applying the edit leaves a
		// program with no diagnostics.
		const src = "type Shape { Circle(r: Float) }\nfn f(s: Shap) {}\nfn main() {}"
		p := testsupport.Analyze(t, src)
		wantDiags(t, "a declared type", p, unknownType(spanIn(t, p, src, ": Shap", "Shap"), "Shap", "Shape"))
		if all := p.Session.Sink.All(); len(all) == 1 {
			fixed := testsupport.ApplyFixits(src, all[0])
			if want := "type Shape { Circle(r: Float) }\nfn f(s: Shape) {}\nfn main() {}"; fixed != want {
				t.Errorf("applying the edit gives %q, want %q", fixed, want)
			}
			wantDiags(t, "after the edit", testsupport.Analyze(t, fixed))
		}

		// The candidates are the three builtins and the types that are declared:
		// a name near a builtin still suggests the builtin, with a type around;
		// one that is near neither has no help; and of two equally near the one
		// that comes first in the alphabet wins, wherever it was declared.
		rows := []struct{ name, best string }{
			{"Flot", "Float"}, {"Bol", "Bool"}, {"Shap", "Shape"}, {"Shapes", "Shape"}, {"Blob", ""}, {"Inty", "Int"},
			// `print` is one letter away and is the first definition of the
			// program, but it is a function: no type is named like it.
			{"Print", ""},
		}
		for _, row := range rows {
			s := "type Shape { A }\ntype Intt = Int\nfn f(x: " + row.name + ") {}"
			p := testsupport.Analyze(t, s)
			wantDiags(t, row.name, p, unknownType(spanIn(t, p, s, ": "+row.name, row.name), row.name, row.best))
		}

		// A type of the prelude is seen from the user's file when it is `pub`,
		// both as a type and as a candidate; a private one is not seen at all,
		// not even its own name, and is not suggested.
		const prelude = "pub type Maybe { Just(Int), Nothing }\ntype Hidden { H }\n"
		const user = "fn f(a: Maybe, b: Mayb, c: Hidden, d: Hiden) {}\nfn main() {}\n"
		got := checkSources(t, prelude, user)
		at := func(context, word string) base.Span {
			lo := indexFrom(user, context, 0) + len(context) - len(word)
			return base.Span{File: got[0].Primary.Span.File, Lo: uint32(lo), Hi: uint32(lo + len(word))}
		}
		want := []diag.Diagnostic{
			unknownType(at("b: Mayb", "Mayb"), "Mayb", "Maybe"),
			unknownType(at("c: Hidden", "Hidden"), "Hidden", ""),
			unknownType(at("d: Hiden", "Hiden"), "Hiden", ""),
		}
		if len(got) != len(want) {
			t.Fatalf("%d diagnostics, want %d: %+v", len(got), len(want), got)
		}
		for i := range want {
			if got[i].Message != want[i].Message || got[i].Primary.Message != want[i].Primary.Message ||
				got[i].Help != want[i].Help || !slices.Equal(got[i].Fixits, want[i].Fixits) || len(got[i].Notes) != 0 {
				t.Errorf("diagnostic %d is %+v, want %+v", i, got[i], want[i])
			}
		}

		// The other way round, the types of the user's file are not candidates for
		// a name in the prelude: it cannot use them.
		got = checkSources(t, "fn f(x: Mayb) {}\n", "pub type Maybe { Just }\nfn main() {}\n")
		if len(got) != 1 || got[0].Code != diag.E(406) || got[0].Help != "" || len(got[0].Fixits) != 0 {
			t.Errorf("a type of the user's file is suggested to the prelude: %+v", got)
		}
	})

	t.Run("a_declared_type_shadows_a_builtin_and_the_prelude", func(t *testing.T) {
		// The module's own type wins over what the prelude declares and over a
		// builtin, as its own function wins over the prelude's: `Bool` in a
		// signature is the type the file declares, and a type of the prelude that
		// the file declares again is not a repeat.
		rp, _, in, sink := resolveSources(t, "pub type Maybe { Just }\n", "type Maybe { Nothing }\ntype Bool { Yes, No }\nfn main() {}\n")
		if sink.HasErrors() {
			t.Fatalf("errors: %+v", sink.All())
		}
		for _, name := range []string{"Maybe", "Bool"} {
			res, ok := rp.LookupType(1, in.InternString(name))
			if !ok || res.Kind != sema.ResDef || rp.Defs.Get(res.Def).Module != 1 {
				t.Errorf("%s in the user's file is %+v (found %v), want the user's own type", name, res, ok)
			}
		}
		// From the prelude's side its own is the only one, and a builtin is not a
		// declared type: LookupType does not know the builtins.
		res, ok := rp.LookupType(0, in.InternString("Maybe"))
		if !ok || rp.Defs.Get(res.Def).Module != 0 {
			t.Errorf("Maybe in the prelude is %+v (found %v), want the prelude's type", res, ok)
		}
		if _, ok := rp.LookupType(0, in.InternString("Bool")); ok {
			t.Error("Bool is a declared type of the prelude, which declares none")
		}
		// Without a type of its own the file sees the `pub` types of the prelude
		// and not the private ones.
		rp, _, in, _ = resolveSources(t, "fn first() {}\npub type Maybe { Just }\ntype Hidden { H }\n", "fn main() {}\n")
		res, ok = rp.LookupType(1, in.InternString("Maybe"))
		if !ok || res.Kind != sema.ResDef || rp.Defs.Get(res.Def).Module != 0 || in.String(rp.Defs.Get(res.Def).Name) != "Maybe" {
			t.Errorf("Maybe in the user's file is %+v (found %v), want the prelude's pub type", res, ok)
		}
		if res, ok := rp.LookupType(1, in.InternString("Hidden")); ok {
			t.Errorf("Hidden in the user's file is %+v, want it not found: the prelude's type is private", res)
		}
		// The shadowing has a visible effect: with the type declared, a `Bool`
		// parameter is no longer the builtin. Both are written `Bool`, so the
		// test looks at the kind of the node.
		for _, row := range []struct {
			name, src string
			want      sema.TypeKind
		}{
			{"declared", "type Bool { Yes }\nfn f(b: Bool) -> Bool { b }\nfn main() {}", sema.TypeData},
			{"builtin", "fn f(b: Bool) -> Bool { b }\nfn main() {}", sema.TypeBuiltin},
		} {
			p := testsupport.Analyze(t, row.src)
			wantCodes(t, p)
			id, _ := fnNamed(t, p, "f")
			typed := p.Frontend.Typed
			fn := typed.Types.Node(typed.DefTypes.Get(id))
			if got := typed.Types.Node(fn.Params[0]).Kind; got != row.want || typed.Types.Node(fn.Ret).Kind != row.want {
				t.Errorf("%s: the parameter has kind %d, want %d", row.name, got, row.want)
			}
		}
	})

	t.Run("a_declared_type_is_what_its_name_stands_for", func(t *testing.T) {
		// A name in an annotation is the type that Declare made: a sum or a record
		// is its own type, an alias is what it expands to, so `Meters` and `Float`
		// are the same type, and `Shape` and `Float` are not.
		const src = "type Shape { A }\ntype Meters = Float\n" +
			"fn f(s: Shape) -> Int { 1 }\nfn g(m: Meters) -> Float { m }\nfn main() {\n  let x: Int = f(A)\n  let y: Meters = 1.5\n}"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		for name, want := range map[string]string{"f": "(Shape) -> Int", "g": "(Float) -> Float"} {
			if got := testsupport.DefType(t, p, name); got != want {
				t.Errorf("%s has type %s, want %s", name, got, want)
			}
		}
		const bad = "type Shape { A }\nfn f(s: Shape) -> Float { s }\nfn main() {}"
		p = testsupport.Analyze(t, bad)
		wantDiags(t, "a Shape where a Float is returned", p,
			secondary(mismatch(spanIn(t, p, bad, "{ s }", "s"), "expected `Float`, found `Shape`"),
				spanIn(t, p, bad, "-> Float", "Float"), "expected because of the return type"))

		// A type that has the name of a builtin one is another type. The two read
		// alike in the message that compares them, so a note says they are not the
		// same; a message of two types that differ has no note.
		const shadow = "type Int { Mk }\nfn f(x: Int) -> Int { x }\nfn main() { print(f(1)) }"
		const same = "these are two different types with the same name: a `type` of the program hides the type that had that name"
		p = testsupport.Analyze(t, shadow)
		wantDiags(t, "a declared Int and the builtin", p,
			secondary(mismatch(spanIn(t, p, shadow, "f(1", "1"), "expected `Int`, found `Int`"),
				spanIn(t, p, shadow, "x: Int", "x: Int"), "the parameter is declared here").WithNote(same),
			mismatch(spanIn(t, p, shadow, "print(f(1))", "f(1)"), "expected `Int`, found `Int`").WithNote(same))

		// A type that is declared twice is E0302, the name stands for the first and
		// the constructors of the second have the second's type: the E0401 that
		// follows is a true one, and its note blames the repeat and not a builtin
		// type, which nothing hides here.
		const twice = "type Shape { Circle(r: Float) }\ntype Shape { Square(side: Float) }\nfn main() {\n  let s = Square(1.0)\n  let t: Shape = s\n}"
		p = testsupport.Analyze(t, twice)
		all := p.Session.Sink.All()
		if len(all) != 2 || all[0].Code != diag.E(302) || all[1].Code != diag.E(401) || all[1].Primary.Message != "expected `Shape`, found `Shape`" ||
			!slices.Equal(all[1].Notes, []string{"these are two different types with the same name: the program declares a type with that name more than once"}) {
			t.Errorf("a type declared twice: diagnostics %+v, want the E0302 and an E0401 whose note blames the repeat", all)
		}
	})

	t.Run("a_constructor_is_a_value_of_its_type", func(t *testing.T) {
		// A constructor with fields is a function from them to its type, and one
		// without is a value of its type, so each fits where its type is expected
		// and nowhere else.
		const src = "type Shape { Circle(r: Float), None }\n" +
			"fn main() {\n  let a: Shape = Circle(1.0)\n  let b: Shape = None\n  let c = Circle\n  let d: Int = a\n}"
		p := testsupport.Analyze(t, src)
		wantDiags(t, "a shape where an Int is wanted", p,
			secondary(mismatch(spanIn(t, p, src, "= a\n", "a"), "expected `Int`, found `Shape`"),
				spanIn(t, p, src, ": Int", "Int"), "expected because of this annotation"))
		res := useAt(t, p, "Circle", 0)
		if res.Kind != sema.ResDef || p.Frontend.Resolved.Defs.Get(res.Def).Kind != sema.DefCtor {
			t.Errorf("Circle resolved to %+v, want its constructor", res)
		}
		if got := exprType(t, p, spanIn(t, p, src, "= Circle\n", "Circle")); got != "(Float) -> Shape" {
			t.Errorf("Circle alone has the type %s, want (Float) -> Shape", got)
		}
	})

	t.Run("a_broken_type_is_one_error", func(t *testing.T) {
		// The parser reports a `type` that it cannot read, and name resolution
		// says nothing more about its name, the constructors it read before the
		// mistake, or what has the type: one error, not an unknown type and an
		// unknown name for each use.
		rows := []struct{ name, src string }{
			{"a use of the type and of a constructor", "type Shape { Circle( }\nfn f(s: Shape) {}\nfn g() { let c = Circle }\nfn main() {}"},
			{"the variant read before the mistake", "type Shape { Circle(r: Float), Rect( }\nfn g() {\n  let c = Circle\n  let r = Rect\n}\nfn main() {}"},
			{"the variants after the mistake", "type Shape {\n  Circle(r: Float)\n  Rect(w: Float h: Float)\n  Tri(a: Float)\n}\nfn area(s: Shape) {}\n" +
				"fn main() {\n  Circle(1.0)\n  Rect(1.0, 2.0)\n  Tri(1.0)\n}"},
			{"an alias that cannot be read", "type A = 1\nfn f(a: A) {}\nfn main() {}"},
			{"a type with no body", "type A\nfn f(a: A) {}\nfn main() {}"},
			{"a real type of the same name wins", "type Shape { Circle( }\ntype Shape { A }\nfn f(s: Shape) { let a = A }\nfn main() {}"},
			// The header is what is wrong, with the brace on the line below the name:
			// the variants are read, and a use of one is not an unknown name.
			{"the brace below the name", "type Shape\n{\n  Circle(r: Float)\n  Dot\n}\nfn area(s: Shape) -> Int { 1 }\n" +
				"fn main() {\n  Circle(1.0)\n  print(area(Dot))\n}"},
		}
		for _, row := range rows {
			wantCodes(t, testsupport.Analyze(t, row.src), "E0201")
		}
		// A type of the same name that was read stands for the name, and not the
		// error that the broken one leaves, whichever of the two is written first.
		for _, row := range []struct{ name, src string }{
			{"the broken type first", "type Shape { Circle( }\ntype Shape { A }\nfn main() {}"},
			{"the real type first", "type Shape { A }\ntype Shape { Circle( }\nfn main() {}"},
		} {
			p := testsupport.Analyze(t, row.src)
			rp, in := p.Frontend.Resolved, p.Session.Interner
			res, ok := rp.LookupType(p.Frontend.User, in.InternString("Shape"))
			if want := uint32(indexFrom(row.src, "type Shape { A }", 0) + len("type ")); !ok || res.Kind != sema.ResDef || rp.Defs.Get(res.Def).Span.Lo != want {
				t.Errorf("%s: Shape as a type is %+v (found %v), want the real type, whose name is at byte %d", row.name, res, ok, want)
			}
		}
	})

	t.Run("attributes_on_a_type", func(t *testing.T) {
		// No attribute applies to a type yet. An unknown one is E0305 as on a
		// function; `#[extern]` is E0304, and removing it is the whole fix, since
		// a type has no body to write.
		const unknown = "#[derive(\"Eq\")] type T { A }\nfn main() {}"
		p := testsupport.Analyze(t, unknown)
		attr := spanIn(t, p, unknown, "#[derive(\"Eq\")]", "#[derive(\"Eq\")]")
		wantDiags(t, "unknown", p, diag.NewError(diag.E(305), "unknown attribute `derive`",
			diag.Label{Span: attr, Message: "not an attribute Ki knows"}).
			WithNote("a program cannot use attributes yet").WithHelp("remove it").WithFixIt(diag.Delete(attr)))

		const extern = "#[extern(\"x\")] type T { A }\nfn main() {}"
		p = testsupport.Analyze(t, extern)
		attr = spanIn(t, p, extern, "#[extern(\"x\")]", "#[extern(\"x\")]")
		wantDiags(t, "extern", p, diag.NewError(diag.E(304), "`#[extern]` is only allowed in the standard library",
			diag.Label{Span: attr, Message: "not allowed outside the standard library"}).
			WithNote("extern functions are declared by the standard library and implemented by the runtime; a program calls them through the prelude").
			WithHelp("remove the attribute").WithFixIt(diag.Delete(attr)))
		if all := p.Session.Sink.All(); len(all) == 1 {
			if fixed := testsupport.ApplyFixits(extern, all[0]); fixed != " type T { A }\nfn main() {}" {
				t.Errorf("removing the attribute gives %q", fixed)
			}
		}
		// The type is a type all the same.
		if got := len(defsOf(p.Frontend.Resolved, p.Frontend.User)); got != 3 {
			t.Errorf("the file has %d definitions, want 3 (T, A and main)", got)
		}
	})
}

// indexFrom is strings.Index of sub in s[from:], from the start of s: the byte
// offset in s of the first occurrence at or after from, which must exist.
func indexFrom(s, sub string, from int) int {
	for i := from; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	panic("indexFrom: " + sub + " is not in " + s)
}
