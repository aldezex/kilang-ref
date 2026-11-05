package sema_test

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/sema"
	"github.com/aldezex/kilang/internal/syntax"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here are about Declare, the pass that makes the types of the
// `type` declarations, and about what Check does with them: constructors as
// functions, and the errors that the declarations and the constructors give. They
// reuse spanIn, wantDiags, wantCodes, unknownType, secondary, mismatch,
// arityError, notCallable, exprType and fnNamed from the other test files of the
// package.

// shown writes the type id of the declarations of p the way a message shows it.
func shown(p *testsupport.Program, id sema.TypeID) string {
	return p.Frontend.Decls.Types.Show(id)
}

// shownAll writes each of ids with shown.
func shownAll(p *testsupport.Program, ids []sema.TypeID) []string {
	var out []string
	for _, id := range ids {
		out = append(out, shown(p, id))
	}
	return out
}

// ctorNamed returns the constructor called name of the user's file, which must
// have exactly one.
func ctorNamed(t *testing.T, p *testsupport.Program, name string) sema.Ctor {
	t.Helper()
	d := p.Frontend.Decls
	var found []sema.Ctor
	for _, c := range d.Ctors.All() {
		if p.Session.Interner.String(c.Name) == name {
			found = append(found, c)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the file has %d constructors called %s, want 1", len(found), name)
	}
	return found[0]
}

// typeNamed returns the declared type called name and its id, which must be
// exactly one.
func typeNamed(t *testing.T, p *testsupport.Program, name string) (sema.TypeDefID, sema.TypeDef) {
	t.Helper()
	d := p.Frontend.Decls
	for id, td := range d.Defs.All() {
		if p.Session.Interner.String(td.Name) == name {
			return id, td
		}
	}
	t.Fatalf("the file declares no type called %s", name)
	panic("unreachable")
}

// aliasCycle is the E0801 that the alias called name starts, with a secondary
// label for each of through, the other aliases of its cycle in the order the
// cycle goes through them. at finds the span of an alias name.
func aliasCycle(name string, at base.Span, through ...aliasAt) diag.Diagnostic {
	d := diag.NewError(diag.E(801), "alias `"+name+"` refers to itself", diag.Label{Span: at, Message: "this alias is part of a cycle"})
	for _, o := range through {
		d = d.WithSecondary(diag.Label{Span: o.span, Message: "through `" + o.name + "`, defined here"})
	}
	return d.WithNote("an alias stands for another type, so it cannot stand for itself")
}

// aliasAt is the name of an alias and where it is declared.
type aliasAt struct {
	name string
	span base.Span
}

// unknownCtor is the E0306 for the name written at, with the help of a
// suggestion if best is not empty.
func unknownCtor(at base.Span, name, best string) diag.Diagnostic {
	d := diag.NewError(diag.E(306), "unknown constructor `"+name+"`", diag.Label{Span: at, Message: "not a constructor in this scope"})
	if best != "" {
		d = d.WithHelp("did you mean `" + best + "`?").WithFixIt(diag.Replace(at, best))
	}
	return d
}

func TestDeclare(t *testing.T) {
	t.Run("the_three_forms", func(t *testing.T) {
		// A sum with every shape of variant, a record and an alias, as Declare
		// reads them: the types, the constructors with their tags and the types of
		// their fields, the field names (none for a positional variant, an entry
		// each for a named or a mixed one), the fields of the record and what the
		// alias expands to.
		const src = "type Shape {\n  Circle(r: Float)\n  Rect(w: Float, h: Float)\n  Pair(Int, Bool)\n" +
			"  Mixed(a: Int, Bool)\n  None\n}\ntype Point { x: Float, y: Int }\ntype Meters = Float\nfn main() {}"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		d, in := p.Frontend.Decls, p.Session.Interner
		if d.Defs.Len() != 3 || d.Ctors.Len() != 5 {
			t.Fatalf("%d types and %d constructors, want 3 and 5", d.Defs.Len(), d.Ctors.Len())
		}
		m := p.Frontend.Modules[p.Frontend.User]
		at := func(word string) base.Span { return spanIn(t, p, src, word, word) }

		shape, shapeDef := typeNamed(t, p, "Shape")
		wantShape := sema.TypeDef{
			Name: in.InternString("Shape"), Span: spanIn(t, p, src, "type Shape {", "Shape"), Module: p.Frontend.User, Item: m.Items[0],
			Form: syntax.TypeFormSum, Ctors: []sema.CtorID{0, 1, 2, 3, 4}, Alias: d.Types.Error(),
		}
		if !slices.Equal(shapeDef.Ctors, wantShape.Ctors) || shapeDef.Name != wantShape.Name || shapeDef.Span != wantShape.Span ||
			shapeDef.Module != wantShape.Module || shapeDef.Item != wantShape.Item || shapeDef.Form != wantShape.Form ||
			shapeDef.Alias != wantShape.Alias || shapeDef.Fields != nil {
			t.Errorf("Shape is %+v, want %+v", shapeDef, wantShape)
		}
		if got := shown(p, d.Types.Data(shape, nil)); got != "Shape" {
			t.Errorf("the type of Shape is shown as %s, want Shape", got)
		}

		type ctorRow struct {
			name   string
			args   string
			fields []string
			ctor   string
		}
		for tag, row := range []ctorRow{
			{"Circle", "Float", []string{"r"}, "(Float) -> Shape"},
			{"Rect", "Float, Float", []string{"w", "h"}, "(Float, Float) -> Shape"},
			{"Pair", "Int, Bool", nil, "(Int, Bool) -> Shape"},
			{"Mixed", "Int, Bool", []string{"a", ""}, "(Int, Bool) -> Shape"},
			{"None", "", nil, "Shape"},
		} {
			c := ctorNamed(t, p, row.name)
			var fields []string
			for _, f := range c.Fields {
				name := ""
				if f != base.NoSymbol {
					name = in.String(f)
				}
				fields = append(fields, name)
			}
			args := strings.Join(shownAll(p, c.Args), ", ")
			if c.Owner != shape || c.Tag != uint32(tag) || args != row.args || !slices.Equal(fields, row.fields) ||
				(row.fields == nil) != (c.Fields == nil) || c.Span != at(row.name) {
				t.Errorf("%s is %+v (args %q, fields %q), want owner %d, tag %d, args %q, fields %q at %v",
					row.name, c, args, fields, shape, tag, row.args, row.fields, at(row.name))
			}
			if got := shown(p, d.CtorType(sema.CtorID(tag))); got != row.ctor {
				t.Errorf("%s has the type %s, want %s", row.name, got, row.ctor)
			}
		}

		_, point := typeNamed(t, p, "Point")
		wantFields := []sema.FieldDef{
			{Name: in.InternString("x"), Span: at("x: Float"), Type: d.Types.Builtin(sema.BuiltinFloat)},
			{Name: in.InternString("y"), Span: at("y: Int"), Type: d.Types.Builtin(sema.BuiltinInt)},
		}
		if point.Form != syntax.TypeFormRecord || !slices.Equal(point.Fields, wantFields) || point.Ctors != nil || point.Alias != d.Types.Error() {
			t.Errorf("Point is %+v, want a record with the fields %+v, no constructors and the error type as its alias", point, wantFields)
		}
		meters, alias := typeNamed(t, p, "Meters")
		if alias.Form != syntax.TypeFormAlias || alias.Alias != d.Types.Builtin(sema.BuiltinFloat) || d.Meaning(meters) != alias.Alias {
			t.Errorf("Meters is %+v, want an alias whose expansion, and meaning, is Float", alias)
		}
		if got := d.Meaning(shape); got != d.Types.Data(shape, nil) || d.Types.Node(got).Kind != sema.TypeData {
			t.Errorf("the meaning of Shape is %d, want its own type %d", got, d.Types.Data(shape, nil))
		}

		// Each definition finds its declaration, and a function finds none.
		for id, def := range p.Frontend.Resolved.Defs.All() {
			if def.Module != p.Frontend.User {
				continue
			}
			td, isType := d.TypeDefOf(id)
			c, isCtor := d.CtorOf(id)
			if isType != (def.Kind == sema.DefType) || isCtor != (def.Kind == sema.DefCtor) {
				t.Errorf("%s (%d): TypeDefOf %v and CtorOf %v for the kind %d", in.String(def.Name), id, isType, isCtor, def.Kind)
			}
			if isType && d.Defs.Get(td).Name != def.Name || isCtor && d.Ctors.Get(c).Name != def.Name {
				t.Errorf("%s (%d) found the declaration of another name", in.String(def.Name), id)
			}
		}
	})

	t.Run("declaration_does_not_depend_on_the_order", func(t *testing.T) {
		// A field names a type declared further down, and two types name each
		// other: neither is an unknown type and neither is an error, whichever
		// comes first. A field of an alias that is declared later is what the
		// alias expands to.
		const src = "type Holder { Hold(n: Later, d: Dist) }\ntype Later { back: Holder, next: Later }\ntype Dist = Float\nfn main() {}"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		hold := ctorNamed(t, p, "Hold")
		if got := shownAll(p, hold.Args); !slices.Equal(got, []string{"Later", "Float"}) {
			t.Errorf("the fields of Hold have the types %q, want Later and Float", got)
		}
		_, later := typeNamed(t, p, "Later")
		if len(later.Fields) != 2 || shown(p, later.Fields[0].Type) != "Holder" || shown(p, later.Fields[1].Type) != "Later" {
			t.Errorf("the fields of Later are %+v, want Holder and Later", later.Fields)
		}
		// The same, with the order of the declarations reversed, gives the same.
		const reversed = "type Dist = Float\ntype Later { back: Holder, next: Later }\ntype Holder { Hold(n: Later, d: Dist) }\nfn main() {}"
		q := testsupport.Analyze(t, reversed)
		wantCodes(t, q)
		if got := shownAll(q, ctorNamed(t, q, "Hold").Args); !slices.Equal(got, []string{"Later", "Float"}) {
			t.Errorf("reversed: the fields of Hold have the types %q, want Later and Float", got)
		}
	})

	t.Run("an_alias_expands_to_its_end", func(t *testing.T) {
		// An alias of an alias is the last one's type, whichever order they are
		// written in; an alias of a declared type is that type; `()` is a type to
		// alias too; and a field that names an alias has the expansion.
		// D leads to the alias that B leads to, which the walk has finished by
		// then: two aliases for one are no cycle.
		const src = "type A = B\ntype B = C\ntype C = Int\ntype S = Shape\ntype U = ()\ntype Shape { Mk(a: A) }\ntype D = C\nfn f(x: S, u: U) -> A { 1 }\nfn main() {}"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		d := p.Frontend.Decls
		for name, want := range map[string]string{"A": "Int", "B": "Int", "C": "Int", "D": "Int", "S": "Shape", "U": "()"} {
			id, td := typeNamed(t, p, name)
			if got := shown(p, td.Alias); got != want || d.Meaning(id) != td.Alias {
				t.Errorf("%s expands to %s, want %s", name, got, want)
			}
		}
		if got := testsupport.DefType(t, p, "f"); got != "(Shape, ()) -> Int" {
			t.Errorf("f has the type %s, want (Shape, ()) -> Int", got)
		}
		if got := shownAll(p, ctorNamed(t, p, "Mk").Args); !slices.Equal(got, []string{"Int"}) {
			t.Errorf("the field of Mk has the type %q, want Int", got)
		}
	})

	t.Run("a_long_alias_chain_is_followed_without_recursion", func(t *testing.T) {
		// The length of a chain of aliases is the program's: 10 000 of them,
		// written from the first to the last so that the first is the one that
		// starts the walk over all of them, are followed with the stack of Go
		// capped at 1 MiB, and each one is the type at the end.
		const n = 10_000
		var src strings.Builder
		for i := range n {
			fmt.Fprintf(&src, "type A%d = A%d\n", i, i+1)
		}
		fmt.Fprintf(&src, "type A%d = Bool\nfn f(x: A0) -> A%d { x }\nfn main() {}\n", n, n)
		old := debug.SetMaxStack(1 << 20)
		defer debug.SetMaxStack(old)
		p := testsupport.Analyze(t, src.String())
		wantCodes(t, p)
		if got := testsupport.DefType(t, p, "f"); got != "(Bool) -> Bool" {
			t.Errorf("f has the type %s, want (Bool) -> Bool", got)
		}
		d := p.Frontend.Decls
		if first, last := d.Defs.Get(0).Alias, d.Defs.Get(n).Alias; first != d.Types.Builtin(sema.BuiltinBool) || last != first {
			t.Errorf("the first alias expands to %d and the last to %d, want Bool (%d) for both", first, last, d.Types.Builtin(sema.BuiltinBool))
		}
	})

	t.Run("alias_cycle_E0801", func(t *testing.T) {
		// One E0801 for each cycle: on the alias that comes first in the source,
		// with a secondary label on each of the others in the order the cycle
		// goes, and nothing for the aliases that merely lead into it. Every alias
		// that is in or leads to a cycle has the error type, so a use of one is
		// silent.
		// The first column of a line is the offset of the line; the name of an
		// alias is the first word after `type `.
		rows := []struct {
			name, src string
			want      func(p *testsupport.Program, src string) []diag.Diagnostic
		}{
			{"two", "type A = B\ntype B = A\nfn main() {}", func(p *testsupport.Program, src string) []diag.Diagnostic {
				return []diag.Diagnostic{aliasCycle("A", spanIn(t, p, src, "type A", "A"), aliasAt{"B", spanIn(t, p, src, "type B", "B")})}
			}},
			{"one", "type A = A\nfn main() {}", func(p *testsupport.Program, src string) []diag.Diagnostic {
				return []diag.Diagnostic{aliasCycle("A", spanIn(t, p, src, "type A", "A"))}
			}},
			{"three", "type A = B\ntype B = C\ntype C = A\nfn main() {}", func(p *testsupport.Program, src string) []diag.Diagnostic {
				return []diag.Diagnostic{aliasCycle("A", spanIn(t, p, src, "type A", "A"),
					aliasAt{"B", spanIn(t, p, src, "type B", "B")}, aliasAt{"C", spanIn(t, p, src, "type C", "C")})}
			}},
			// The walk starts at R and meets the cycle at N, but M comes first in
			// the source: it is the primary, and the secondary is N. R is not in the
			// cycle and has nothing said about it.
			{"entered_in_the_middle", "type R = N\ntype M = N\ntype N = M\nfn f(r: R, m: M, n: N) {}\nfn main() {}",
				func(p *testsupport.Program, src string) []diag.Diagnostic {
					return []diag.Diagnostic{aliasCycle("M", spanIn(t, p, src, "type M", "M"), aliasAt{"N", spanIn(t, p, src, "type N", "N")})}
				}},
			{"two_cycles", "type A = A\ntype B = C\ntype C = B\nfn main() {}", func(p *testsupport.Program, src string) []diag.Diagnostic {
				return []diag.Diagnostic{
					aliasCycle("A", spanIn(t, p, src, "type A", "A")),
					aliasCycle("B", spanIn(t, p, src, "type B", "B"), aliasAt{"C", spanIn(t, p, src, "type C", "C")}),
				}
			}},
		}
		for _, row := range rows {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p, row.want(p, row.src)...)
			d := p.Frontend.Decls
			for id, td := range d.Defs.All() {
				if td.Alias != d.Types.Error() || d.Meaning(id) != d.Types.Error() {
					t.Errorf("%s: the alias %s has the type %s, want the error type", row.name, p.Session.Interner.String(td.Name), shown(p, td.Alias))
				}
			}
		}

		// A cycle is silent where it is used: neither an unknown type nor a
		// mismatch, and a sum that names itself through a field is no cycle.
		const used = "type A = B\ntype B = A\nfn f(a: A) -> Int { a }\nfn main() { let x: Int = f(1) }"
		p := testsupport.Analyze(t, used)
		wantCodes(t, p, "E0801")
		const sums = "type Nat { Z, S(n: Nat) }\ntype Tree { Leaf, Node(l: Tree, r: Tree) }\nfn main() {}"
		wantCodes(t, testsupport.Analyze(t, sums))

		// The one error of a long cycle has a label for each alias, and building
		// them costs space in proportion to the cycle: a loop that copied the list
		// of labels at each alias would allocate hundreds of megabytes for 4 000
		// aliases (the allocations are counted, which does not depend on the
		// machine).
		const n = 4_000
		var ring strings.Builder
		for i := range n {
			fmt.Fprintf(&ring, "type A%d = A%d\n", i, (i+1)%n)
		}
		ring.WriteString("fn main() {}\n")
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		p = testsupport.Analyze(t, ring.String())
		runtime.ReadMemStats(&after)
		got := p.Session.Sink.All()
		if len(got) != 1 || got[0].Code != diag.E(801) || len(got[0].Secondary) != n-1 {
			t.Fatalf("a cycle of %d aliases gives %d diagnostics (the first with %d secondary labels), want one with %d",
				n, len(got), len(got[0].Secondary), n-1)
		}
		if first, last := got[0].Secondary[0].Message, got[0].Secondary[n-2].Message; first != "through `A1`, defined here" || last != "through `A3999`, defined here" {
			t.Errorf("the labels run from %q to %q, want A1 to A3999", first, last)
		}
		if used := after.TotalAlloc - before.TotalAlloc; used > 64<<20 {
			t.Errorf("a cycle of %d aliases allocated %d bytes, want at most 64 MiB (%d)", n, used, 64<<20)
		}
	})

	t.Run("unknown_type_in_a_declaration_E0406", func(t *testing.T) {
		// A type name that nothing declares is E0406 where it is written in a
		// field, in a record or in an alias, with the same help as in an
		// annotation, and once however many times the name is used afterwards.
		const src = "type Shape { Circle(r: Flot), Pair(Int, Shap) }\ntype Point { x: Float, y: Boool }\ntype Meters = Shapes\n" +
			"fn f(c: Meters, p: Point) {}\nfn main() {}"
		p := testsupport.Analyze(t, src)
		wantDiags(t, "declarations", p,
			unknownType(spanIn(t, p, src, ": Flot", "Flot"), "Flot", "Float"),
			unknownType(spanIn(t, p, src, ", Shap)", "Shap"), "Shap", "Shape"),
			unknownType(spanIn(t, p, src, ": Boool", "Boool"), "Boool", "Bool"),
			unknownType(spanIn(t, p, src, "= Shapes", "Shapes"), "Shapes", "Shape"))
		// A name is reported where it is written and once, also when the alias
		// that writes it is reached from another alias before its turn.
		const chain = "type A = B\ntype B = Flot\nfn main() {}"
		q := testsupport.Analyze(t, chain)
		wantDiags(t, "an alias reached from another", q, unknownType(spanIn(t, q, chain, "= Flot", "Flot"), "Flot", "Float"))
		// What has the error type fits anywhere: the constructor still takes its
		// argument, and no use of `f` is a mismatch.
		if got := testsupport.DefType(t, p, "f"); got != "({error}, Point) -> ()" {
			t.Errorf("f has the type %s, want ({error}, Point) -> ()", got)
		}
		if got := shownAll(p, ctorNamed(t, p, "Circle").Args); !slices.Equal(got, []string{"{error}"}) {
			t.Errorf("the field of Circle has the type %q, want the error type", got)
		}
	})

	t.Run("duplicates_are_declared_and_the_first_wins", func(t *testing.T) {
		// E0302 is Resolve's, and the second declaration exists all the same, with
		// its own constructors; a name in an annotation is the first one.
		const src = "type A { X }\ntype A { Y }\nfn f(a: A) {}\nfn main() {}"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p, "E0302")
		d := p.Frontend.Decls
		if d.Defs.Len() != 2 || d.Ctors.Len() != 2 {
			t.Fatalf("%d types and %d constructors, want 2 and 2", d.Defs.Len(), d.Ctors.Len())
		}
		id, _ := fnNamed(t, p, "f")
		fn := d.Types.Node(p.Frontend.Typed.DefTypes.Get(id))
		if fn.Params[0] != d.Types.Data(0, nil) || fn.Params[0] == d.Types.Data(1, nil) {
			t.Errorf("the parameter of f is %d, want the first type, %d, and not the second, %d", fn.Params[0], d.Types.Data(0, nil), d.Types.Data(1, nil))
		}
		if y := ctorNamed(t, p, "Y"); y.Owner != 1 || y.Tag != 0 {
			t.Errorf("Y is %+v, want the first constructor of the second type", y)
		}
	})

	t.Run("a_type_the_parser_could_not_read_is_not_declared", func(t *testing.T) {
		// The parser reports the broken type and Declare adds nothing for it: no
		// type, no constructor, and no second message.
		p := testsupport.Analyze(t, "type Shape { Circle(r: ) }\nfn f(s: Shape) {}\nfn main() {}")
		wantCodes(t, p, "E0201")
		if d := p.Frontend.Decls; d.Defs.Len() != 0 || d.Ctors.Len() != 0 {
			t.Errorf("%d types and %d constructors, want none", d.Defs.Len(), d.Ctors.Len())
		}
		// Its name stands for the error type, silently, whatever uses it: a
		// function, a field, and an alias, which is not a cycle with the first
		// alias of the file whatever its id is.
		if got := testsupport.DefType(t, p, "f"); got != "({error}) -> ()" {
			t.Errorf("f has the type %s, want ({error}) -> ()", got)
		}
		q := testsupport.Analyze(t, "type A = Shape\ntype Shape { Circle(r: ) }\ntype B { M(a: Shape) }\nfn main() {}")
		wantCodes(t, q, "E0201")
		if d := q.Frontend.Decls; d.Defs.Len() != 2 || d.Defs.Get(0).Alias != d.Types.Error() || shownAll(q, ctorNamed(t, q, "M").Args)[0] != "{error}" {
			t.Errorf("an alias and a field of a broken type: %d types, the alias is %d and the field %v, want 2 types and the error type for both",
				d.Defs.Len(), d.Defs.Get(0).Alias, shownAll(q, ctorNamed(t, q, "M").Args))
		}
	})

	t.Run("unknown_constructor_E0306", func(t *testing.T) {
		// A name that starts in uppercase and nothing declares is E0306, with the
		// question and the edit when a visible constructor is spelled almost like
		// it, and with neither when none is. Only constructors are candidates: not
		// a type, a function or a variable.
		const src = "type Shape { Circle(r: Float), Square(side: Float) }\ntype Circles { a: Int }\n" +
			"fn main() {\n  let y = 1\n  let s = Circl(1.0)\n  let t = Triangle(1.0)\n  let u = Circles\n  let w = Circlh\n" +
			"  let p = Print(1)\n  let q = Y\n}"
		p := testsupport.Analyze(t, src)
		wantDiags(t, "unknown constructors", p,
			unknownCtor(spanIn(t, p, src, "= Circl(", "Circl"), "Circl", "Circle"),
			unknownCtor(spanIn(t, p, src, "= Triangle(", "Triangle"), "Triangle", ""),
			unknownCtor(spanIn(t, p, src, "= Circles\n", "Circles"), "Circles", "Circle"),
			unknownCtor(spanIn(t, p, src, "= Circlh\n", "Circlh"), "Circlh", "Circle"),
			unknownCtor(spanIn(t, p, src, "= Print(", "Print"), "Print", ""),
			unknownCtor(spanIn(t, p, src, "= Y\n", "Y"), "Y", ""))
		// Applying the edit of the first leaves a program that checks.
		const one = "type Shape { Circle(r: Float) }\nfn main() { let s = Circl(1.0) }"
		q := testsupport.Analyze(t, one)
		if all := q.Session.Sink.All(); len(all) == 1 {
			fixed := testsupport.ApplyFixits(one, all[0])
			if want := "type Shape { Circle(r: Float) }\nfn main() { let s = Circle(1.0) }"; fixed != want {
				t.Errorf("applying the edit gives %q, want %q", fixed, want)
			}
			wantCodes(t, testsupport.Analyze(t, fixed))
		} else {
			t.Errorf("%d diagnostics for %q, want 1", len(all), one)
		}

		// The first letter decides, and it is the lexer's rule: a lowercase name,
		// and one that starts with `_` whatever follows, is still an unknown name.
		for _, name := range []string{"circl", "_Circl", "x", "cIRCLE"} {
			r := testsupport.Analyze(t, "type Shape { Circle(r: Float) }\nfn main() { let s = "+name+" }")
			wantCodes(t, r, "E0301")
		}
		for _, name := range []string{"A", "Z", "Zebra", "Zed9"} {
			r := testsupport.Analyze(t, "fn main() { let s = "+name+" }")
			wantCodes(t, r, "E0306")
		}

		// A constructor of the prelude is a candidate when it is `pub`, and one that
		// is private is not, as for any name of the prelude; one of the user's file
		// does not reach the prelude.
		_, _, _, sink := resolveSources(t, "pub type Maybe { Just(Int), Nothing }\ntype Hidden { Secret }\n", "fn main() {\n  Jusst\n  Secrat\n}\n")
		var helps []string
		for _, d := range sink.All() {
			helps = append(helps, d.Help)
		}
		if want := []string{"did you mean `Just`?", ""}; !slices.Equal(helps, want) {
			t.Errorf("the helps of the unknown constructors are %q, want %q", helps, want)
		}
		_, _, _, sink = resolveSources(t, "fn f() {\n  Jusst\n}\n", "pub type Maybe { Just(Int) }\nfn main() {}\n")
		if all := sink.All(); len(all) != 1 || all[0].Code != diag.E(306) || all[0].Help != "" || len(all[0].Fixits) != 0 {
			t.Errorf("a constructor of the user's file is suggested to the prelude: %+v", all)
		}
	})

	t.Run("a_constructor_is_a_function", func(t *testing.T) {
		// A constructor with fields has a function type: it can be called and also
		// passed where a function is wanted, and then called through the
		// parameter. Without fields it is a value of its type.
		const src = "type Shape { Circle(r: Float), Rect(w: Float, h: Float), None }\nfn apply(f, x) -> Shape { f(x) }\n" +
			"fn main() {\n  let s = apply(Circle, 1.0)\n  let c = Circle\n  let n = None\n  let r = Rect(1.0, 2.0)\n}"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		for context, want := range map[string]string{
			"= apply(Circle, 1.0)\n": "Shape", "= Circle\n": "(Float) -> Shape", "= None\n": "Shape", "= Rect(1.0, 2.0)\n": "Shape",
		} {
			word := strings.TrimSuffix(strings.TrimPrefix(context, "= "), "\n")
			if got := exprType(t, p, spanIn(t, p, src, context, word)); got != want {
				t.Errorf("%s has the type %s, want %s", word, got, want)
			}
		}
		if got := testsupport.DefType(t, p, "apply"); got != "((Float) -> Shape, Float) -> Shape" {
			t.Errorf("apply has the type %s, want ((Float) -> Shape, Float) -> Shape after the call that passes Circle", got)
		}
	})

	t.Run("constructor_calls_E0401_E0402_E0403", func(t *testing.T) {
		// A constructor is called as a function is, so the same mistakes have the
		// same codes, and the labels point at the declaration of the variant: the
		// field that an argument does not fit, the name of the constructor that
		// takes another number of arguments, and nothing for a value that is not
		// called.
		const src = "type Shape {\n  Circle(r: Float)\n  Pair(Int, Bool)\n  None\n}\nfn main() {\n" +
			"  let a = Circle(true)\n  let b = Pair(1, 2)\n  let c = Circle(1.0, 2.0)\n  let d = None(1)\n  let e = Pair()\n}"
		p := testsupport.Analyze(t, src)
		wantDiags(t, "constructor calls", p,
			secondary(mismatch(spanIn(t, p, src, "(true)", "true"), "expected `Float`, found `Bool`"),
				spanIn(t, p, src, "(r: Float)", "r: Float"), "the field is declared here"),
			secondary(mismatch(spanIn(t, p, src, "(1, 2)", "2"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, "(Int, Bool)", "Bool"), "the field is declared here"),
			secondary(arityError(spanIn(t, p, src, "(1.0, 2.0)", "(1.0, 2.0)"), 1, 2),
				spanIn(t, p, src, "  Circle(r", "Circle"), "`Circle` is declared here").WithNote("`Circle` takes `(Float)`"),
			notCallable(spanIn(t, p, src, "= None(1)", "None"), "Shape"),
			secondary(arityError(spanIn(t, p, src, "Pair()", "()"), 2, 0),
				spanIn(t, p, src, "  Pair(Int", "Pair"), "`Pair` is declared here").WithNote("`Pair` takes `(Int, Bool)`"))
	})

	t.Run("a_declared_type_is_a_type_in_messages", func(t *testing.T) {
		// A declared type is named in a mismatch as the program names it, and two
		// declared types are different types even if they are shaped alike, as an
		// alias of one is the same.
		const src = "type A { MkA }\ntype B { MkB }\ntype C = A\nfn takeA(a: A) {}\nfn main() {\n  takeA(MkB)\n  takeA(MkA)\n  let c: C = MkA\n}"
		p := testsupport.Analyze(t, src)
		wantDiags(t, "two types", p,
			secondary(mismatch(spanIn(t, p, src, "(MkB)", "MkB"), "expected `A`, found `B`"),
				spanIn(t, p, src, "(a: A)", "a: A"), "the parameter is declared here"))
	})

	t.Run("an_unknown_constructor_is_one_error", func(t *testing.T) {
		// Review Focus 3: a constructor that nothing declares has the error type,
		// which fits every place, so it is the one E0306 and no E0401 on top of it:
		// inside a sum, as an operand, as an argument, as a tail and where a type
		// is annotated.
		const src = "type Shape { Circle(r: Float) }\nfn f(s: Shape) -> Int { 1 }\nfn g() -> Int { Circl(2.0) }\nfn main() {\n" +
			"  let a = 1 + Circl(2.0)\n  let b: Int = Circl\n  let c = f(Circl(1.0))\n  let d = Circl(1.0) == 2\n  let e = !Circl\n  let f = -Circl\n}"
		p := testsupport.Analyze(t, src)
		var codes []string
		for _, d := range p.Session.Sink.All() {
			codes = append(codes, d.Code.String())
		}
		if want := slices.Repeat([]string{"E0306"}, 7); !slices.Equal(codes, want) {
			t.Errorf("error codes %v, want %v: each use of Circl is one E0306 and nothing else; %+v", codes, want, p.Session.Sink.All())
		}
		// One use in a sum, alone, with the exact diagnostic.
		const one = "fn main() { let a = 1 + Circl(2.0) }"
		q := testsupport.Analyze(t, one)
		wantDiags(t, "in a sum", q, unknownCtor(spanIn(t, q, one, "+ Circl(", "Circl"), "Circl", ""))
	})
}
