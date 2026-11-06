package sema_test

import (
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here are about how Check reads records and tuples: the types of a
// literal, an access and a tuple, and the five diagnostics E0409 to E0413 that go
// with them. They reuse spanIn, wantDiags, wantCodes, mismatch, secondary,
// unknownType, notCallable, aliasCycle, exprType and checkSources from the other
// test files of the package.

// noSpan is the span that a diagnostic builder takes for "no such label".
var noSpan base.Span

// unknownFieldDiag is the E0409 for the field written at at, on the type typ,
// with a secondary label on decl (the name of the type in its declaration) unless
// decl is noSpan, and the question and the edit if best is not empty.
func unknownFieldDiag(at base.Span, typ, field, best string, decl base.Span) diag.Diagnostic {
	d := diag.NewError(diag.E(409), "no field `"+field+"` on `"+typ+"`", diag.Label{Span: at, Message: "unknown field"})
	if decl != noSpan {
		d = d.WithSecondary(diag.Label{Span: decl, Message: "`" + typ + "` is defined here"})
	}
	if best != "" {
		d = d.WithHelp("did you mean `" + best + "`?").WithFixIt(diag.Replace(at, best))
	}
	return d
}

// missingFieldsDiag is the E0410 for a literal of the type typ whose closing brace
// is at closing: title and label are written by the caller, because they are the
// wording the test pins; declared are the declarations of the fields that the
// secondary labels point at.
func missingFieldsDiag(closing base.Span, title, label string, declared ...base.Span) diag.Diagnostic {
	d := diag.NewError(diag.E(410), title, diag.Label{Span: closing, Message: label})
	for _, at := range declared {
		d = d.WithSecondary(diag.Label{Span: at, Message: "declared here"})
	}
	return d.WithNote("a record literal names every field, or ends with `..base`")
}

// twiceDiag is the E0411 for the field `name` given again at again, the first
// time being at first; edit is the span that the fix-it deletes, or noSpan.
func twiceDiag(name string, again, first, edit base.Span) diag.Diagnostic {
	d := diag.NewError(diag.E(411), "field `"+name+"` is given twice", diag.Label{Span: again, Message: "given again here"}).
		WithSecondary(diag.Label{Span: first, Message: "first given here"}).WithHelp("remove one of them")
	if edit != noSpan {
		d = d.WithFixIt(diag.Delete(edit))
	}
	return d
}

// notRecordDiag is the E0412 for a type shown as shown, at at, with a secondary
// label on decl if it is not noSpan and the note about sums if sum.
func notRecordDiag(at base.Span, shown string, decl base.Span, name string, sum bool) diag.Diagnostic {
	d := diag.NewError(diag.E(412), "`"+shown+"` is not a record", diag.Label{Span: at, Message: "has no fields to access"})
	if decl != noSpan {
		d = d.WithSecondary(diag.Label{Span: decl, Message: "`" + name + "` is defined here"})
	}
	if sum {
		d = d.WithNote("only a record has fields; a sum type is taken apart with `match`")
	}
	return d
}

// noElementDiag is the E0413 for the index written at at (as digits) on a type
// shown as shown. A tuple says which elements exist, in label; a type that is not
// one says `not a tuple` and has the note.
func noElementDiag(at base.Span, shown, digits, label string) diag.Diagnostic {
	d := diag.NewError(diag.E(413), "`"+shown+"` has no element `"+digits+"`", diag.Label{Span: at, Message: label})
	if label == "not a tuple" {
		d = d.WithNote("only a tuple has elements `.0`, `.1`…")
	}
	return d
}

// onlyCodes fails the test unless the error codes of p are exactly want, in the
// order the diagnostics come.
func onlyCodes(t *testing.T, what string, p *testsupport.Program, want ...string) {
	t.Helper()
	got := testsupport.ErrorCodes(p)
	if !slices.Equal(got, want) {
		t.Errorf("%s: error codes %v, want %v; diagnostics %+v", what, got, want, p.Session.Sink.All())
	}
}

func TestDataCheck(t *testing.T) {
	t.Run("types_of_records_and_tuples", func(t *testing.T) {
		// A literal has the type of its record, whatever the order of its fields,
		// with a pun, with a base or through an alias; an access has the type of
		// the field or of the element; a tuple has the tuple of the types of its
		// elements, which nest.
		const src = "type Point { x: Float, y: Float }\ntype P = Point\nfn main() {\n  let p = Point { x: 1.0, y: 2.0 }\n" +
			"  let a = p.x\n  let b = Point { y: 3.0, x: 4.0 }\n  let c = Point { x: 5.0, ..p }\n  let d = Point { ..p }\n" +
			"  let e = P { x: 1.0, y: 2.0 }\n  let x = 1.0\n  let y = 2.0\n  let f = Point { x, y }\n  let g = (1, true)\n" +
			"  let h = g.1\n  let n = ((1, 2), (3, (true, 5.0)))\n  let i = n.0\n  let j = n.1\n  let k = n.1.1\n  let l = n.1.1.0\n}"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		for _, row := range []struct{ context, word, want string }{
			{"= Point { x: 1.0, y: 2.0 }", "Point { x: 1.0, y: 2.0 }", "Point"},
			{"= p.x", "p.x", "Float"},
			{"= Point { y: 3.0, x: 4.0 }", "Point { y: 3.0, x: 4.0 }", "Point"},
			{"= Point { x: 5.0, ..p }", "Point { x: 5.0, ..p }", "Point"},
			{"= Point { ..p }", "Point { ..p }", "Point"},
			{"= P { x: 1.0, y: 2.0 }", "P { x: 1.0, y: 2.0 }", "Point"},
			{"= Point { x, y }", "Point { x, y }", "Point"},
			{"= (1, true)", "(1, true)", "(Int, Bool)"},
			{"= g.1", "g.1", "Bool"},
			{"= n.0\n", "n.0", "(Int, Int)"},
			{"= n.1\n", "n.1", "(Int, (Bool, Float))"},
			{"= n.1.1\n", "n.1.1", "(Bool, Float)"},
			{"= n.1.1.0", "n.1.1.0", "Bool"},
		} {
			if got := exprType(t, p, spanIn(t, p, src, row.context, row.word)); got != row.want {
				t.Errorf("%s has the type %s, want %s", row.word, got, row.want)
			}
		}
		// A field is looked up by its name in its own record, so two records may
		// use the same names for fields of different types.
		const two = "type A { v: Int, w: Bool }\ntype B { w: Float, v: () }\nfn main() {\n" +
			"  let a = A { v: 1, w: true }\n  let b = B { v: (), w: 1.0 }\n  let c = a.w\n  let d = b.w\n  let e = b.v\n}"
		q := testsupport.Analyze(t, two)
		wantCodes(t, q)
		for context, want := range map[string]string{"= a.w": "Bool", "= b.w": "Float", "= b.v": "()"} {
			word := context[len("= "):]
			if got := exprType(t, q, spanIn(t, q, two, context, word)); got != want {
				t.Errorf("%s has the type %s, want %s", word, got, want)
			}
		}
		// A record that declares a name twice is E0302 at the second, as a type or
		// a constructor is, with the first as the secondary label. The first stays
		// for the name, the second is not a field (a literal that gives `v` once is
		// not missing anything), its type is read all the same, and the fields after
		// the repeat are still fields (a literal gives `w`, and `c.w` reads it).
		const dup = "type C { v: Int, v: Bool, w: Int }\nfn get(c: C) { let g = c.v\n let h = c.w }\nfn main() { let c = C { v: 1, w: 2 } }"
		r := testsupport.Analyze(t, dup)
		wantDiags(t, "a field declared twice", r, redefined("v", spanIn(t, r, dup, ", v: Bool", "v"), spanIn(t, r, dup, "{ v: Int", "v")))
		if got := exprType(t, r, spanIn(t, r, dup, "= c.v", "c.v")); got != "Int" {
			t.Errorf("c.v has the type %s, want Int, the type of the first v", got)
		}
		if got := exprType(t, r, spanIn(t, r, dup, "= c.w", "c.w")); got != "Int" {
			t.Errorf("c.w has the type %s, want Int, the field after the repeated one", got)
		}
		const dupBad = "type C { v: Int, v: Nope }\nfn main() {}"
		r = testsupport.Analyze(t, dupBad)
		wantCodes(t, r, "E0302", "E0406")
	})

	t.Run("values_are_checked_against_the_fields_E0401", func(t *testing.T) {
		// Each value is checked against the type of its field, with the label of the
		// field's declaration; the base against the record, with no declaration to
		// point at; the elements of a tuple are not compared with anything until the
		// tuple is.
		const src = "type Point { x: Float, y: Float }\ntype Shape { Circle(r: Float) }\nfn main() {\n" +
			"  let a = Point { x: 1, y: 2.0 }\n  let x = 1\n  let b = Point { x, y: 2.0 }\n  let s = Circle(1.0)\n" +
			"  let c = Point { x: 1.0, ..s }\n  let d: (Int, Bool) = (1, 2)\n  let e: (Int, Bool) = (1, true, 2)\n}"
		p := testsupport.Analyze(t, src)
		field := func(label string) base.Span { return spanIn(t, p, src, "{ x: Float, y: Float }", label) }
		wantDiags(t, "mismatches", p,
			floatEdit(secondary(mismatch(spanIn(t, p, src, "x: 1,", "1"), "expected `Float`, found `Int`"), field("x: Float"), "the field is declared here"),
				spanIn(t, p, src, "x: 1,", "1"), "1.0"),
			secondary(mismatch(spanIn(t, p, src, "{ x, y", "x"), "expected `Float`, found `Int`"), field("x: Float"), "the field is declared here"),
			mismatch(spanIn(t, p, src, "..s }", "s"), "expected `Point`, found `Shape`"),
			secondary(mismatch(spanIn(t, p, src, "(1, 2)", "(1, 2)"), "expected `(Int, Bool)`, found `(Int, Int)`"),
				spanIn(t, p, src, "d: (Int, Bool)", "(Int, Bool)"), "expected because of this annotation"),
			secondary(mismatch(spanIn(t, p, src, "(1, true, 2)", "(1, true, 2)"), "expected `(Int, Bool)`, found `(Int, Bool, Int)`"),
				spanIn(t, p, src, "e: (Int, Bool)", "(Int, Bool)"), "expected because of this annotation"))
	})

	t.Run("unknown_field_E0409", func(t *testing.T) {
		// In an access and in a literal, with the label of the type's declaration
		// and, when a field is spelled almost like the name, the question and the
		// edit; none when no field is close. A literal that has an unknown field is
		// not also missing the field it was meant to give.
		const src = "type Point { x: Float, y: Float }\nfn main() {\n  let p = Point { x: 1.0, y: 2.0 }\n  let a = p.z\n" +
			"  let b = p.whatever\n  let c = Point { z: 1.0, y: 2.0 }\n  let d = Point { x: 1.0, y: 2.0, wxyz: 3.0 }\n}"
		p := testsupport.Analyze(t, src)
		decl := spanIn(t, p, src, "type Point {", "Point")
		wantDiags(t, "unknown fields", p,
			unknownFieldDiag(spanIn(t, p, src, "= p.z", "z"), "Point", "z", "x", decl),
			unknownFieldDiag(spanIn(t, p, src, "= p.whatever", "whatever"), "Point", "whatever", "", decl),
			unknownFieldDiag(spanIn(t, p, src, "{ z: 1.0", "z"), "Point", "z", "x", decl),
			unknownFieldDiag(spanIn(t, p, src, "wxyz: 3.0", "wxyz"), "Point", "wxyz", "", decl))

		// A field that is not there does not stop the literal being read: its own value
		// is checked, and so are the fields after it.
		const rest = "type Point { x: Float, y: Float }\nfn main() { let e = Point { z: 1.0 + true, x: false, y: 2.0 } }"
		r := testsupport.Analyze(t, rest)
		wantDiags(t, "fields after an unknown one", r,
			unknownFieldDiag(spanIn(t, r, rest, "{ z:", "z"), "Point", "z", "", spanIn(t, r, rest, "type Point {", "Point")),
			secondary(mismatch(spanIn(t, r, rest, "+ true", "true"), "expected `Float`, found `Bool`"), spanIn(t, r, rest, "1.0 + true", "+"), "`+` expects `Float` operands"),
			secondary(mismatch(spanIn(t, r, rest, "x: false", "false"), "expected `Float`, found `Bool`"), spanIn(t, r, rest, "{ x: Float", "x: Float"), "the field is declared here"))

		// Applying the edit of each leaves a program that checks.
		for _, one := range []struct{ src, fixed string }{
			{"type Point { x: Float, y: Float }\nfn f(p: Point) -> Float { p.z }", "type Point { x: Float, y: Float }\nfn f(p: Point) -> Float { p.x }"},
			{"type Point { x: Float, y: Float }\nfn f() -> Point { Point { x: 1.0, w: 2.0 } }", "type Point { x: Float, y: Float }\nfn f() -> Point { Point { x: 1.0, y: 2.0 } }"},
		} {
			q := testsupport.Analyze(t, one.src)
			all := q.Session.Sink.All()
			if len(all) != 1 {
				t.Errorf("%q: %d diagnostics, want 1: %+v", one.src, len(all), all)
				continue
			}
			if fixed := testsupport.ApplyFixits(one.src, all[0]); fixed != one.fixed {
				t.Errorf("applying the edit gives %q, want %q", fixed, one.fixed)
			} else {
				wantCodes(t, testsupport.Analyze(t, fixed))
			}
		}

		// A pun is a field and a variable in one name: the edit of an unknown field
		// keeps the variable (`x: xx`), and the edit of an unknown variable keeps
		// the field (`x: best`), so that the program that results is the one that was
		// meant, and does not give the other error back.
		for _, one := range []struct{ name, src, code, fixed string }{
			{"an unknown field", "type Point { x: Float, y: Float }\nfn make(xx: Float, y: Float) -> Point { Point { xx, y } }",
				"E0409", "type Point { x: Float, y: Float }\nfn make(xx: Float, y: Float) -> Point { Point { x: xx, y } }"},
			{"an unknown variable", "type Box { size: Float }\nfn make(sizes: Float) -> Box { Box { size } }",
				"E0301", "type Box { size: Float }\nfn make(sizes: Float) -> Box { Box { size: sizes } }"},
			// A value that is not a pun is an ordinary expression: the edit is the name that
			// the author meant, and the field is not repeated before it.
			{"an unknown variable in a field value", "type Point { x: Float, y: Float }\nfn f(size: Float) -> Point { Point { x: sizee, y: 2.0 } }",
				"E0301", "type Point { x: Float, y: Float }\nfn f(size: Float) -> Point { Point { x: size, y: 2.0 } }"},
			// What comes after a pun is not one: the base has the edit of a name.
			{"a base after a pun", "type Point { x: Float, y: Float }\nfn f(x: Float, p: Point) -> Point { Point { x, ..pq } }",
				"E0301", "type Point { x: Float, y: Float }\nfn f(x: Float, p: Point) -> Point { Point { x, ..p } }"},
		} {
			q := testsupport.Analyze(t, one.src)
			all := q.Session.Sink.All()
			if len(all) != 1 || all[0].Code.String() != one.code {
				t.Errorf("%s: %d diagnostics, want one %s: %+v", one.name, len(all), one.code, all)
				continue
			}
			if fixed := testsupport.ApplyFixits(one.src, all[0]); fixed != one.fixed {
				t.Errorf("%s: applying the edit gives %q, want %q", one.name, fixed, one.fixed)
			} else {
				wantCodes(t, testsupport.Analyze(t, fixed))
			}
		}

		// A pun whose name is neither a variable nor a field is wrong twice, so it has
		// two errors, E0301 and E0409, and only one of them has an edit: the one that
		// mends both, which writes the name that is a variable and a field (`x`),
		// and is the resolver's, because only it knows the variables. When there is
		// none, neither has an edit; and where the resolver cannot tell what the
		// record is, the variable is mended and the field is left to its own error.
		const points = "type Point { x: Float, y: Float }\n"
		for _, one := range []struct{ name, src, fixed, help string }{
			{"a name that is a variable and a field", points + "fn make(x: Float, y: Float) -> Point { Point { xx, y } }",
				points + "fn make(x: Float, y: Float) -> Point { Point { x, y } }", "did you mean `x`?"},
			{"through an alias", points + "type Q = Point\nfn make(x: Float, y: Float) -> Q { Q { xx, y } }",
				points + "type Q = Point\nfn make(x: Float, y: Float) -> Q { Q { x, y } }", "did you mean `x`?"},
			{"a variable that is not a field", "type Box { size: Float }\nfn make(sizes: Float) -> Box { Box { sizee } }", "", "did you mean `size`?"},
			{"a field that the literal gives", points + "fn f(x: Float) -> Point { Point { x: 1.0, xx } }", "", ""},
			// A pun reads a variable: a function with the name of a field is not what it
			// meant (the edit would give a function to the field), unless a variable has
			// that name too, which is the one the name stands for.
			{"a function that is a field", "type Item { count: Int }\nfn count() -> Int { 1 }\nfn make() -> Item { Item { cont } }", "", "did you mean `count`?"},
			{"a variable with the name of a function", "type Item { count: Int }\nfn count() -> Int { 1 }\nfn make(count: Int) -> Item { Item { cont } }",
				"type Item { count: Int }\nfn count() -> Int { 1 }\nfn make(count: Int) -> Item { Item { count } }", "did you mean `count`?"},
		} {
			q := testsupport.Analyze(t, one.src)
			var unknownName, unknownField *diag.Diagnostic
			for _, d := range q.Session.Sink.All() {
				switch d.Code.String() {
				case "E0301":
					unknownName = &d
				case "E0409":
					unknownField = &d
				}
			}
			if got := testsupport.ErrorCodes(q); len(got) != 2 || unknownName == nil || unknownField == nil {
				t.Errorf("%s: error codes %v, want E0301 and E0409", one.name, got)
				continue
			}
			if len(unknownField.Fixits) != 0 || unknownField.Help != one.help {
				t.Errorf("%s: E0409 has the help %q and %d edits, want %q and none", one.name, unknownField.Help, len(unknownField.Fixits), one.help)
			}
			if one.fixed == "" {
				if len(unknownName.Fixits) != 0 || unknownName.Help != "" {
					t.Errorf("%s: E0301 has the help %q and %d edits, want neither", one.name, unknownName.Help, len(unknownName.Fixits))
				}
			} else if fixed := testsupport.ApplyFixits(one.src, *unknownName); fixed != one.fixed || unknownName.Help != one.help {
				t.Errorf("%s: E0301 has the help %q and its edit gives %q, want %q and %q", one.name, unknownName.Help, fixed, one.help, one.fixed)
			} else {
				wantCodes(t, testsupport.Analyze(t, fixed))
			}
		}
		// Where the resolver cannot tell what the record is, the variable is mended
		// and the field is the checker's: a type that does not exist, and an alias
		// that goes round in a circle (which must end, not loop).
		for _, one := range []struct{ name, src, fixed string }{
			{"a type that does not exist", "fn f(x: Float) { Nope { xx } }", "fn f(x: Float) { Nope { xx: x } }"},
			{"aliases in a cycle", "type A = B\ntype B = A\nfn f(x: Float) { A { xx } }", "type A = B\ntype B = A\nfn f(x: Float) { A { xx: x } }"},
		} {
			q := testsupport.Analyze(t, one.src)
			var found bool
			for _, d := range q.Session.Sink.All() {
				if d.Code.String() == "E0301" {
					found = true
					if fixed := testsupport.ApplyFixits(one.src, d); fixed != one.fixed {
						t.Errorf("%s: the edit of E0301 gives %q, want %q", one.name, fixed, one.fixed)
					}
				}
			}
			if !found {
				t.Errorf("%s: no E0301 in %v", one.name, testsupport.ErrorCodes(q))
			}
		}

		// A field that is not a pun keeps its edit whether or not the name that is its
		// value exists: that name is not the name of the field.
		{
			const src = points + "fn f() -> Point { Point { x: 1.0, w: q } }"
			var found bool
			for _, d := range testsupport.Analyze(t, src).Session.Sink.All() {
				if d.Code.String() == "E0409" {
					found = true
					if fixed := testsupport.ApplyFixits(src, d); fixed != points+"fn f() -> Point { Point { x: 1.0, y: q } }" {
						t.Errorf("a field whose value is an unknown name: the edit gives %q, want the field `y` written over `w`", fixed)
					}
				}
			}
			if !found {
				t.Errorf("a field whose value is an unknown name: no E0409")
			}
		}

		// The label of the declaration is there only when it is in the same file: a
		// type of the prelude is declared in another one.
		diags := checkSources(t, "pub type Point { x: Float, y: Float }\n", "fn f(p: Point) -> Float { p.z }\nfn main() {}\n")
		if len(diags) != 1 || diags[0].Code != diag.E(409) || len(diags[0].Secondary) != 0 || diags[0].Help != "did you mean `x`?" {
			t.Errorf("a type declared in the prelude: %+v, want one E0409 with the help and no secondary label", diags)
		}
	})

	t.Run("missing_fields_E0410", func(t *testing.T) {
		// One error for a literal, on its closing brace, that lists the fields it does
		// not give, with a label on the declaration of each: the first eight when
		// there are more. `..base` gives the others, and a literal with an unknown
		// field is not also missing one.
		const point = "type Point { x: Float, y: Float }\nfn main() { let p = Point "
		const eight = "a: Int, b: Int, c: Int, d: Int, e: Int, f: Int, g: Int, h: Int"
		rows := []struct {
			name, src, before string // the brace is the first `}` after before
			title, label      string
			declared          []string // the text of each declaration that is labelled
		}{
			{"one", point + "{ x: 1.0 } }", "x: 1.0 ", "missing field `y` in `Point { … }`", "`y` is not given", []string{"y: Float"}},
			{"two of three", "type Cube { x: Int, y: Int, z: Int }\nfn main() { let c = Cube { y: 1 } }", "y: 1 ",
				"missing fields `x` and `z` in `Cube { … }`", "`x` and `z` are not given", []string{"x: Int", "z: Int"}},
			{"none given", point + "{ } }", "let p = Point { ", "missing fields `x` and `y` in `Point { … }`", "`x` and `y` are not given", []string{"x: Float", "y: Float"}},
			{"three", "type T { a: Int, b: Int, c: Int }\nfn main() { let t = T { } }", "let t = T { ",
				"missing fields `a`, `b` and `c` in `T { … }`", "`a`, `b` and `c` are not given", []string{"a: Int", "b: Int", "c: Int"}},
			{"eight", "type Wide { " + eight + " }\nfn main() { let w = Wide { } }", "let w = Wide { ",
				"missing fields `a`, `b`, `c`, `d`, `e`, `f`, `g` and `h` in `Wide { … }`",
				"`a`, `b`, `c`, `d`, `e`, `f`, `g` and `h` are not given", strings.Split(eight, ", ")},
			{"nine", "type Wider { " + eight + ", i: Int }\nfn main() { let w = Wider { } }", "let w = Wider { ",
				"missing fields `a`, `b`, `c`, `d`, `e`, `f`, `g`, `h` and 1 more in `Wider { … }`",
				"`a`, `b`, `c`, `d`, `e`, `f`, `g`, `h` and 1 more are not given", strings.Split(eight, ", ")},
			{"brace on its own line", point + "{\n    y: 2.0,\n  } }", "y: 2.0,\n  ", "missing field `x` in `Point { … }`", "`x` is not given", []string{"x: Float"}},
		}
		for _, row := range rows {
			p := testsupport.Analyze(t, row.src)
			var declared []base.Span
			for _, text := range row.declared {
				declared = append(declared, spanAfter(t, p, row.src, "", text))
			}
			closing := spanAfter(t, p, row.src, row.before, "}")
			wantDiags(t, row.name, p, missingFieldsDiag(closing, row.title, row.label, declared...))
		}

		// The labels of the declarations are there only when they are in the same file.
		diags := checkSources(t, "pub type P { a: Int, b: Int }\n", "fn main() { let p = P { a: 1 } }\n")
		if len(diags) != 1 || diags[0].Code != diag.E(410) || len(diags[0].Secondary) != 0 || len(diags[0].Notes) != 1 {
			t.Errorf("a type declared in the prelude: %+v, want one E0410 with the note and no secondary label", diags)
		}

		// No E0410 when `..base` gives the rest, nor when the literal has a field the
		// record does not (which is the one it was meant to give, misspelled); one for
		// a field given twice and another that is missing.
		wantCodes(t, testsupport.Analyze(t, point+"{ x: 1.0, y: 2.0 }\n  let q = Point { x: 1.0, ..p } }"))
		wantCodes(t, testsupport.Analyze(t, point+"{ x: 1.0, y: 2.0 }\n  let q = Point { x: 1.0, z: 2.0 } }"), "E0409")
		wantCodes(t, testsupport.Analyze(t, point+"{ x: 1.0, x: 2.0 } }"), "E0411", "E0410")
	})

	t.Run("field_given_twice_E0411", func(t *testing.T) {
		// The primary label is on the field written again and a secondary one on the
		// first; the edit deletes the second with its comma, if that stays on one
		// line, and with its indentation if it is alone on its line, which leaves an
		// empty line and not one of blanks (an edit cannot take the line break).
		// Both values are still checked.
		const pre = "type Point { x: Float, y: Float }\nfn f() -> Point { "
		rows := []struct {
			name, src  string
			afterAgain string // what comes just before the field that is written again
			editAfter  string // what comes before the text that the edit deletes
			edit       string // what the edit deletes, "" for none
			fixed      string // the program that the edit leaves, "" if it is not applied here
		}{
			{"in the middle", pre + "Point { x: 1.0, x: 2.0, y: 3.0 } }", "x: 1.0, ", "x: 1.0, ", "x: 2.0, ", pre + "Point { x: 1.0, y: 3.0 } }"},
			{"last", pre + "Point { y: 3.0, x: 1.0, x: 2.0 } }", "x: 1.0, ", "x: 1.0", ", x: 2.0", pre + "Point { y: 3.0, x: 1.0 } }"},
			{"over lines, the comma on the line before", pre + "Point {\n  y: 3.0,\n  x: 1.0,\n  x: 2.0\n} }", "x: 1.0,\n  ", "", "", ""},
			{"over lines, its own line", pre + "Point {\n  x: 1.0,\n  x: 2.0,\n  y: 3.0\n} }", "x: 1.0,\n  ", "x: 1.0,\n", "  x: 2.0,",
				pre + "Point {\n  x: 1.0,\n\n  y: 3.0\n} }"},
		}
		for _, row := range rows {
			p := testsupport.Analyze(t, row.src)
			again, first := spanAfter(t, p, row.src, row.afterAgain, "x"), spanAfter(t, p, row.src, pre, "x")
			edit := noSpan
			if row.edit != "" {
				edit = spanAfter(t, p, row.src, row.editAfter, row.edit)
			}
			wantDiags(t, row.name, p, twiceDiag("x", again, first, edit))
			if row.fixed != "" {
				if fixed := testsupport.ApplyFixits(row.src, p.Session.Sink.All()[0]); fixed != row.fixed {
					t.Errorf("%s: applying the edit gives %q, want %q", row.name, fixed, row.fixed)
				} else {
					wantCodes(t, testsupport.Analyze(t, fixed))
				}
			}
		}
		// Given three times: two errors, each against the first. Both values of a
		// repeated field are checked against its type.
		const three = pre + "Point { x: 1.0, x: 2.0, x: 3.0, y: 4.0 } }"
		p := testsupport.Analyze(t, three)
		wantDiags(t, "three times", p,
			twiceDiag("x", spanAfter(t, p, three, "x: 1.0, ", "x"), spanAfter(t, p, three, pre, "x"), spanAfter(t, p, three, "x: 1.0, ", "x: 2.0, ")),
			twiceDiag("x", spanAfter(t, p, three, "x: 2.0, ", "x"), spanAfter(t, p, three, pre, "x"), spanAfter(t, p, three, "x: 2.0, ", "x: 3.0, ")))
		const typed = pre + "Point { x: 1.0, x: true, y: 3.0 } }"
		q := testsupport.Analyze(t, typed)
		wantDiags(t, "a repeated value is checked", q,
			twiceDiag("x", spanAfter(t, q, typed, "x: 1.0, ", "x"), spanAfter(t, q, typed, pre, "x"), spanAfter(t, q, typed, "x: 1.0, ", "x: true, ")),
			secondary(mismatch(spanAfter(t, q, typed, "x: 1.0, x: ", "true"), "expected `Float`, found `Bool`"),
				spanAfter(t, q, typed, "", "x: Float"), "the field is declared here"))
	})

	t.Run("not_a_record_E0412", func(t *testing.T) {
		// A field read from, or a literal named after, something that is not a record:
		// a sum (with the note about `match`), a builtin, a tuple, the unit, an alias
		// of a sum or of a builtin (labelled at the alias, as that is what the program
		// wrote), and a value whose type is not known yet, which is written as Show
		// writes it.
		const src = "type Shape { Circle(r: Float), Square(s: Float) }\ntype Meters = Float\ntype Alias = Shape\nfn g(q) { q.x }\n" +
			"fn main() {\n  let s = Circle(1.0)\n  let a = s.r\n  let n = 1\n  let b = n.x\n  let c = (1, 2).x\n  let d = ().x\n" +
			"  let e = Shape { r: 1.0 }\n  let f = Int { x: 1 }\n  let h = Meters { x: 1.0 }\n  let i = Alias { r: 1.0 }\n" +
			"  let j: Alias = s\n  let k = j.r\n}"
		p := testsupport.Analyze(t, src)
		shape, meters, alias := spanAfter(t, p, src, "", "Shape"), spanAfter(t, p, src, "type ", "Meters"), spanAfter(t, p, src, "type Meters = Float\ntype ", "Alias")
		wantDiags(t, "not records", p,
			notRecordDiag(spanAfter(t, p, src, "q.", "x"), "a", noSpan, "", false),
			notRecordDiag(spanAfter(t, p, src, "= s.", "r"), "Shape", shape, "Shape", true),
			notRecordDiag(spanAfter(t, p, src, "= n.", "x"), "Int", noSpan, "", false),
			notRecordDiag(spanAfter(t, p, src, "= (1, 2).", "x"), "(Int, Int)", noSpan, "", false),
			notRecordDiag(spanAfter(t, p, src, "= ().", "x"), "()", noSpan, "", false),
			notRecordDiag(spanAfter(t, p, src, "let e = ", "Shape"), "Shape", shape, "Shape", true),
			notRecordDiag(spanAfter(t, p, src, "let f = ", "Int"), "Int", noSpan, "", false),
			notRecordDiag(spanAfter(t, p, src, "let h = ", "Meters"), "Meters", meters, "Meters", false),
			notRecordDiag(spanAfter(t, p, src, "let i = ", "Alias"), "Alias", alias, "Alias", true),
			notRecordDiag(spanAfter(t, p, src, "= j.", "r"), "Shape", shape, "Shape", true))

		// The label of the declaration is there only when it is in the same file.
		diags := checkSources(t, "pub type Flag { On, Off }\n", "fn f(x: Flag) -> Int { x.v }\nfn main() {}\n")
		if len(diags) != 1 || diags[0].Code != diag.E(412) || len(diags[0].Secondary) != 0 || len(diags[0].Notes) != 1 {
			t.Errorf("a type declared in the prelude: %+v, want one E0412 with the note and no secondary label", diags)
		}
	})

	t.Run("no_element_E0413", func(t *testing.T) {
		// An index past the end of a tuple, or on a type that is not one. The label
		// says which elements exist; the message and the label write the index as the
		// program did, also when it does not fit in 32 bits (and when it is exactly
		// the largest that does).
		const src = "type Point { x: Float, y: Float }\nfn g(q) { q.0 }\nfn side(n: Int) -> Int { n }\nfn main() {\n  let p = Point { x: 1.0, y: 2.0 }\n" +
			"  let t2 = (1, true)\n  let t3 = (1, true, 2.0)\n  let t8 = (1, 2, 3, 4, 5, 6, 7, 8)\n  let a = t2.2\n  let b = t3.3\n" +
			"  let c = t8.8\n  let d = t8.7\n  let e = t2.99999999999\n  let f = t2.4294967295\n  let h = t2.4294967296\n" +
			"  let i = 5.0.0\n  let j = p.0\n  let k = ().0\n  let m = side.0\n}"
		p := testsupport.Analyze(t, src)
		at := func(prefix, digits string) base.Span { return spanAfter(t, p, src, prefix, digits) }
		wantDiags(t, "no elements", p,
			noElementDiag(at("q.", "0"), "a", "0", "not a tuple"),
			noElementDiag(at("let a = t2.", "2"), "(Int, Bool)", "2", "only `.0` and `.1` exist"),
			noElementDiag(at("let b = t3.", "3"), "(Int, Bool, Float)", "3", "only `.0`, `.1` and `.2` exist"),
			noElementDiag(at("let c = t8.", "8"), "(Int, Int, Int, Int, Int, Int, Int, Int)", "8", "only `.0` to `.7` exist"),
			noElementDiag(at("let e = t2.", "99999999999"), "(Int, Bool)", "99999999999", "only `.0` and `.1` exist"),
			noElementDiag(at("let f = t2.", "4294967295"), "(Int, Bool)", "4294967295", "only `.0` and `.1` exist"),
			noElementDiag(at("let h = t2.", "4294967296"), "(Int, Bool)", "4294967296", "only `.0` and `.1` exist"),
			noElementDiag(at("let i = 5.0.", "0"), "Float", "0", "not a tuple"),
			noElementDiag(at("let j = p.", "0"), "Point", "0", "not a tuple"),
			noElementDiag(at("let k = ().", "0"), "()", "0", "not a tuple"),
			noElementDiag(at("let m = side.", "0"), "(Int) -> Int", "0", "not a tuple"))
		// The largest `Int` is an index that no tuple has, E0413; one more is not an
		// `Int`, and is the E0105 of the lexer and nothing else.
		const largest = "fn main() {\n  let t = (1, 2)\n  let a = t.9223372036854775807\n}"
		onlyCodes(t, "the largest Int", testsupport.Analyze(t, largest), "E0413")
		const beyond = "fn main() {\n  let t = (1, 2)\n  let a = t.9223372036854775808\n}"
		onlyCodes(t, "beyond the largest Int", testsupport.Analyze(t, beyond), "E0105")
	})

	t.Run("a_receiver_is_known_from_what_came_before_it", func(t *testing.T) {
		// Spec 5.4, rule 1: the type of the receiver has to be known when the access is
		// read, from left to right. A parameter that nothing has typed is not (E0412
		// with the type as `a`); one that an earlier line has made a tuple or a record
		// is, through the variable it was bound to.
		const src = "type Point { x: Float, y: Float }\nfn tup(t) -> Int {\n  let u = (1, true)\n  let w = if true { t } else { u }\n  t.0\n}\n" +
			"fn rec(q) -> Float {\n  let p = Point { x: 1.0, y: 2.0 }\n  let w = if true { q } else { p }\n  q.x\n}\n" +
			"fn late(q) -> Float {\n  let n = q.x\n  let p = Point { x: 1.0, y: 2.0 }\n  let w = if true { q } else { p }\n  n\n}\nfn main() {}"
		p := testsupport.Analyze(t, src)
		wantDiags(t, "receivers", p, notRecordDiag(spanAfter(t, p, src, "let n = q.", "x"), "a", noSpan, "", false))

		// The reading goes through the functions in the order of the file, too: a
		// call that comes first gives the parameter its type, and the access in the
		// function that comes after is fine; the same two items the other way round
		// are E0412. The operators do not depend on that, they wait for every body
		// (see judgePending), and an access cannot: the rule says the receiver is
		// known at that point, and the methods of chapter 14 settle the case with
		// their own diagnostic.
		const get = "type Point { x: Float, y: Float }\nfn get(p) -> Float { p.x }\n"
		const use = "fn main() { printFloat(get(Point { x: 1.0, y: 2.0 })) }"
		p = testsupport.Analyze(t, get+use)
		wantDiags(t, "the function first", p, notRecordDiag(spanAfter(t, p, get+use, "{ p.", "x"), "a", noSpan, "", false))
		wantDiags(t, "the call first", testsupport.Analyze(t, use+"\n"+get))
	})

	t.Run("types_in_messages_are_written_after_the_cap", func(t *testing.T) {
		// A message that writes the type of a function of 1500 parameters costs 200 KB
		// to write, so once the sink is full the compiler must not write it: the same
		// 100 errors cost at most a quarter after 100 cheap ones have filled the sink
		// (the same measure as TestCheck/diagnostics_not_built_after_the_cap). The
		// help of an unknown field is the same kind of cost: it looks for the
		// closest of the 3000 fields of a record. A call of a tuple of 20 000
		// elements is the same, since the type of the callee is what E0403 writes.
		params := make([]string, 1500)
		fields := make([]string, 3000)
		for i := range params {
			params[i] = "q" + strconv.Itoa(i)
		}
		for i := range fields {
			fields[i] = "field" + strconv.Itoa(i) + ": Int"
		}
		big := "fn big(" + strings.Join(params, ", ") + ") {}\n"
		wide := "type Wide { " + strings.Join(fields, ", ") + " }\nfn use(w: Wide) {\n"
		repeat := func(line string) string { return strings.Repeat(line, diag.MaxErrors) }
		tuple := "(" + strings.Repeat("1, ", 19999) + "1)"
		cheap := "fn cheap() {\n" + repeat("  let c: Int = true\n") + "}\n"
		for _, row := range []struct {
			name    string
			code    uint16
			program string
		}{
			{"not_a_record", 412, big + "fn main() {\n" + repeat("  let n = big.x\n") + "}"},
			{"no_element", 413, big + "fn main() {\n" + repeat("  let n = big.0\n") + "}"},
			{"unknown_field", 409, wide + repeat("  let n = w.fieldx\n") + "}\nfn main() {}"},
			{"not_callable", 403, "fn main() {\n  let t = " + tuple + "\n" + repeat("  t(1)\n") + "}"},
		} {
			cost := func(src string) (uint64, *diag.Sink) {
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				p := testsupport.Analyze(t, src)
				runtime.ReadMemStats(&after)
				return after.TotalAlloc - before.TotalAlloc, &p.Session.Sink
			}
			room, sink := cost(row.program)
			if sink.Truncated() || sink.Count(diag.E(row.code)) != diag.MaxErrors {
				t.Fatalf("%s: %d errors, %d of them E0%d, truncated %v, want exactly %d and room left", row.name, sink.ErrorCount(), sink.Count(diag.E(row.code)), row.code, sink.Truncated(), diag.MaxErrors)
			}
			full, sink := cost(cheap + row.program)
			if !sink.Truncated() || sink.ErrorCount() != diag.MaxErrors {
				t.Fatalf("%s: the sink has %d errors, truncated %v, want %d and truncated", row.name, sink.ErrorCount(), sink.Truncated(), diag.MaxErrors)
			}
			if room < 10<<20 {
				t.Fatalf("%s: %d errors that write big allocated %d bytes, want at least 10 MB: the comparison proves nothing", row.name, diag.MaxErrors, room)
			}
			if full*4 > room {
				t.Errorf("%s: %d errors dropped by a full sink allocated %d bytes, and with room in the sink %d, want at most a quarter", row.name, diag.MaxErrors, full, room)
			}
		}
	})

	t.Run("literal_names", func(t *testing.T) {
		// The name of a literal is looked up among the types, as an annotation is: a
		// name that is not there is E0406 (with the help if one is close), and its
		// values and its base are still checked; an alias of a record is the record; an
		// alias of anything else, and a builtin type, is E0412. A literal whose name is
		// not a record has the error type, which fits whatever it is annotated with.
		const src = "type Point { x: Float, y: Float }\ntype P = Point\ntype Shape { Circle(r: Float) }\ntype Meters = Float\nfn main() {\n" +
			"  let a = Unknown { x: Circle(true) }\n  let b = Pint { x: 1.0, y: 2.0 }\n  let c = P { x: 1.0, y: 2.0 }\n" +
			"  let d: Int = Qwerty { x: 1 }\n  let e: Bool = Qwerty { x: 1 }\n  let f: Int = Int { x: 1 }\n  let g: Bool = Int { x: 1 }\n" +
			"  let h: Int = Meters { x: 1.0 }\n  let i: Bool = Meters { x: 1.0 }\n  let j = Qwerty { x: 1, ..c.qq }\n}"
		p := testsupport.Analyze(t, src)
		meters := spanAfter(t, p, src, "type ", "Meters")
		wantDiags(t, "names", p,
			unknownType(spanAfter(t, p, src, "let a = ", "Unknown"), "Unknown", ""),
			secondary(mismatch(spanAfter(t, p, src, "Circle(", "true"), "expected `Float`, found `Bool`"),
				spanAfter(t, p, src, "", "r: Float"), "the field is declared here"),
			unknownType(spanAfter(t, p, src, "let b = ", "Pint"), "Pint", "Point"),
			unknownType(spanAfter(t, p, src, "let d: Int = ", "Qwerty"), "Qwerty", ""),
			unknownType(spanAfter(t, p, src, "let e: Bool = ", "Qwerty"), "Qwerty", ""),
			notRecordDiag(spanAfter(t, p, src, "let f: Int = ", "Int"), "Int", noSpan, "", false),
			notRecordDiag(spanAfter(t, p, src, "let g: Bool = ", "Int"), "Int", noSpan, "", false),
			notRecordDiag(spanAfter(t, p, src, "let h: Int = ", "Meters"), "Meters", meters, "Meters", false),
			notRecordDiag(spanAfter(t, p, src, "let i: Bool = ", "Meters"), "Meters", meters, "Meters", false),
			unknownType(spanAfter(t, p, src, "let j = ", "Qwerty"), "Qwerty", ""),
			unknownFieldDiag(spanAfter(t, p, src, "..c.", "qq"), "Point", "qq", "", spanAfter(t, p, src, "", "Point")))
		// A type that the parser could not read after its name is reported there,
		// and a literal of it says nothing more: it has the error type, and its
		// values are read all the same (the name inside the second is unknown).
		const broken = "type Foo { x: , }\nfn main() {\n  let a = Foo { x: 1 }\n  let b: Int = Foo { x: nosuch }\n}"
		wantCodes(t, testsupport.Analyze(t, broken), "E0201", "E0301")
		// An alias that has the error type, in a cycle, of a name that is not a type, or
		// of a type that could not be read, has been reported where it went wrong,
		// and a literal named by it says nothing more.
		const errAlias = "type A = B\ntype B = A\ntype Bad = Strin\ntype Brk = Pt\ntype Pt { x: Float,\nfn main() {\n" +
			"  let a = A { f: 1 }\n  let b = Bad { f: 1 }\n  let c = Brk { x: 1.0 }\n  let d: Int = A { f: 1 }\n}"
		wantCodes(t, testsupport.Analyze(t, errAlias), "E0201", "E0801", "E0406")
		const fixable = "type Point { x: Float }\nfn main() { let b = Pint { x: 1.0 } }"
		q := testsupport.Analyze(t, fixable)
		if all := q.Session.Sink.All(); len(all) != 1 {
			t.Errorf("%d diagnostics for %q, want 1", len(all), fixable)
		} else if fixed := testsupport.ApplyFixits(fixable, all[0]); fixed != "type Point { x: Float }\nfn main() { let b = Point { x: 1.0 } }" {
			t.Errorf("applying the edit gives %q", fixed)
		} else {
			wantCodes(t, testsupport.Analyze(t, fixed))
		}
		// A name in small letters, where the `{` is followed by fields, is a literal
		// of a type that nothing has: the one E0406, with the type it looks like and
		// the edit that writes it, and not an unknown name, a `{` that is not on a
		// new line and an unknown name for each field.
		const small = "type Point { x: Float, y: Float }\nfn main() {\n  let p = point { x: 1.0, y: 2.0 }\n  print(p.x)\n}"
		r := testsupport.Analyze(t, small)
		if all := r.Session.Sink.All(); len(all) != 1 || all[0].Code != diag.E(406) || all[0].Help != "did you mean `Point`?" {
			t.Errorf("%q: diagnostics %+v, want one E0406 with the help for `Point`", small, all)
		} else if fixed := testsupport.ApplyFixits(small, all[0]); fixed != strings.Replace(small, "point {", "Point {", 1) {
			t.Errorf("applying the edit gives %q", fixed)
		}
	})

	t.Run("an_unknown_field_is_one_error", func(t *testing.T) {
		// Review Focus 3: a field that the record does not have has the error type,
		// which fits every place, so each use is the one E0409 and nothing on top of
		// it: as an operand, an argument, a receiver, a condition, a field value, an
		// element and an annotated value. A literal with an unknown field still has
		// the type of its record.
		const src = "type Point { x: Float, y: Float }\nfn main() {\n  let p = Point { x: 1.0, y: 2.0 }\n  let a = 1.0 + p.z\n" +
			"  printFloat(p.z)\n  let b = p.z.w\n  let c = -p.z\n  let d: Int = p.z\n  let e = if p.z { 1 } else { 2 }\n" +
			"  let f = Point { x: p.z, y: 2.0 }\n  let g = (p.z, 1).0\n  let h = p.z == 1.0\n" +
			"  let i = printFloat(Point { z: 1.0, y: 2.0 }.x)\n  let j = p.z.0\n  let k: Bool = p.z.0\n  let l: Int = p.z.0\n" +
			"  let m: Bool = p.z.w\n  let n: Int = p.z.w\n}"
		p := testsupport.Analyze(t, src)
		onlyCodes(t, "unknown fields", p, slices.Repeat([]string{"E0409"}, 15)...)
		if got := exprType(t, p, spanAfter(t, p, src, "let i = printFloat(", "Point { z: 1.0, y: 2.0 }")); got != "Point" {
			t.Errorf("a literal with an unknown field has the type %s, want Point", got)
		}
	})

	t.Run("a_trailing_comma_in_a_tuple_is_one_error", func(t *testing.T) {
		// A comma before the `)` is a missing element, which the parser reports. The
		// tuple is an error node and has the error type, so the place that expects a
		// tuple of another arity (an annotation of two, a parameter of two, one of
		// three, a type that is no tuple) adds nothing to the E0201; and the type
		// `(Int, Bool,)` is the error type as well.
		// An element that cannot be read in the middle is the same mistake: the tuple
		// is not one of three with an error inside.
		const src = "fn takes(a: (Int, Int)) {}\nfn main() {\n  let t: (Int, Bool) = (1, true,)\n  let v: Int = (1,)\n" +
			"  takes((1, 2,))\n  let u: (Int, Int, Int) = (1, 2,)\n  let w: (Int, Bool,) = (1, true)\n  let m: (Int, Int) = (1, , 2)\n}"
		onlyCodes(t, "trailing commas", testsupport.Analyze(t, src), slices.Repeat([]string{"E0201"}, 6)...)
	})

	t.Run("a_literal_in_an_if_head_is_one_error", func(t *testing.T) {
		// The parser reports the literal that needs parentheses and keeps it in the
		// tree, so what follows reads it as what it is: no unknown constructor
		// (`Point` is declared), no unknown name (`x` is a field) and no block
		// (`E0412` for the `()` it would have), and a mistake inside the literal
		// is still one of its own.
		const src = "type Point { x: Float, y: Float }\nfn main() {\n  let p = Point { x: 1.0, y: 2.0 }\n" +
			"  if Point { x: 1.0, y: 2.0 }.x > p.x { print(1) }\n}"
		onlyCodes(t, "a literal in the head", testsupport.Analyze(t, src), "E0201")
		const wrong = "type Point { x: Float, y: Float }\nfn main() {\n  if Point { x: 1.0, z: 2.0 }.x > 0.5 { print(1) }\n}"
		onlyCodes(t, "a literal in the head with a wrong field", testsupport.Analyze(t, wrong), "E0201", "E0409")
	})

	t.Run("tuple_types_and_aliases", func(t *testing.T) {
		// A tuple type is the tuple of its elements, so `(a, Int)` and `(Bool, Int)`
		// are told apart by their elements. An alias that goes through a tuple back to
		// itself is E0801 once, however many times the tuple mentions it, one that
		// leads into the cycle is not an error of its own and has the error type, a
		// name that is not a type in one is E0406 once, and a tuple is not a function.
		const src = "type A = (A, Int)\ntype A2 = (Int, A2)\ntype B = (C, Int)\ntype C = B\ntype D = (A, Bool)\ntype Pair = (Int, Bool)\n" +
			"type Tree = (Tree, Tree, Tree)\ntype P = (Q, Q)\ntype Q = (P, P, Int)\n" +
			"fn first(x: Pair) -> Int { x.0 }\nfn main() {\n  let a: (Int, Bol) = (1, true)\n  let b = first((1, true))\n" +
			"  let c: D = (1, 2)\n  let t = (1, true)\n  let d = t(1)\n}"
		p := testsupport.Analyze(t, src)
		wantDiags(t, "tuple types", p,
			aliasCycle("A", spanAfter(t, p, src, "type ", "A")),
			aliasCycle("A2", spanIn(t, p, src, "type A2 =", "A2")),
			aliasCycle("B", spanAfter(t, p, src, "type ", "B"), aliasAt{"C", spanAfter(t, p, src, "type B = (C, Int)\ntype ", "C")}),
			aliasCycle("Tree", spanIn(t, p, src, "type Tree", "Tree")),
			aliasCycle("P", spanIn(t, p, src, "type P =", "P"), aliasAt{"Q", spanIn(t, p, src, "type Q", "Q")}),
			unknownType(spanAfter(t, p, src, "(Int, ", "Bol"), "Bol", "Bool"),
			secondary(notCallable(spanAfter(t, p, src, "let d = ", "t"), "(Int, Bool)"),
				spanIn(t, p, src, " t = (1, true)", "t"), "`t` is bound here"))
	})
}

// spanAfter returns the span, in the user's file of p, of the first occurrence of
// text that comes after the first occurrence of after (the start of src if after
// is empty).
func spanAfter(t *testing.T, p *testsupport.Program, src, after, text string) base.Span {
	t.Helper()
	i := strings.Index(src, after)
	if i < 0 {
		t.Fatalf("%q is not in %q", after, src)
	}
	j := strings.Index(src[i+len(after):], text)
	if j < 0 {
		t.Fatalf("%q does not come after %q in %q", text, after, src)
	}
	lo := i + len(after) + j
	return base.Span{File: userFile(p), Lo: uint32(lo), Hi: uint32(lo + len(text))}
}
