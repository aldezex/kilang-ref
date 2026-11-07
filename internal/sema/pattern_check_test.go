package sema_test

import (
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/sema"
	"github.com/aldezex/kilang/internal/syntax"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here are about how Resolve binds the variables of patterns and how
// Check types them and the `match` that holds them: the three reasons that explain
// a pattern (ReasonScrutinee, ReasonMatchArms and ReasonPatternArg), the guard,
// and the codes of this session, E0703 to E0705 and the growth of E0410. They
// reuse spanIn, wantDiags, wantCodes, onlyDiag, mismatch, secondary, floatEdit,
// unknownCtor, unknownType, unknownFieldDiag, noSpan and the other helpers of the
// package.

// The helps of a value that is unused where `()` is wanted (see unusedHelp and
// unusedBindHelp) for the body of an arm that is not a block, which has nowhere to
// write the `let`: it is to be written as a block.
const (
	unusedArmHelp     = "this value is unused: write the arm as `{ let name = … }`, or return it from the function"
	unusedArmBindHelp = "this value is unused: write the arm as `{ let name = … }`"
)

// shapes is the declarations that most of the sources below start with.
const shapes = "type Shape { Circle(r: Float), Rect(w: Float, h: Float), Unit }\ntype Point { x: Int, y: Int }\n"

// mismatchBecause is the E0401 at at, which says expected and found, with the
// secondary label text at because.
func mismatchBecause(at base.Span, expected, found string, because base.Span, text string) diag.Diagnostic {
	return secondary(mismatch(at, "expected `"+expected+"`, found `"+found+"`"), because, text)
}

// scrutineeMismatch is the E0401 of a pattern that does not fit the value that is
// matched (ReasonScrutinee): its secondary label is on that value and names its
// whole type, which is not the type of the pattern when the pattern is inside
// another one. found is what the label says, with its backquotes if it is a type.
func scrutineeMismatch(at base.Span, expected, found string, value base.Span, whole string) diag.Diagnostic {
	return secondary(mismatch(at, "expected `"+expected+"`, found "+found), value, "the value being matched has type `"+whole+"`")
}

// fieldMismatch is the E0401 of a sub-pattern that does not fit the field it is
// matched against (ReasonPatternArg): its secondary label is on the field.
func fieldMismatch(at base.Span, expected, found string, field base.Span) diag.Diagnostic {
	return mismatchBecause(at, expected, found, field, "the field is declared here")
}

// orDiag is the E0703 for the name that the alternative at lacks and the one at
// binds.
func orDiag(name string, lacks, binds base.Span) diag.Diagnostic {
	return diag.NewError(diag.E(703), "or-pattern binds `"+name+"` in one alternative only",
		diag.Label{Span: lacks, Message: "`" + name + "` is not bound here"}).
		WithSecondary(diag.Label{Span: binds, Message: "`" + name + "` is bound here"}).
		WithNote("every alternative of `|` binds the same names, so the body can use them")
}

// twiceBindDiag is the E0704 for the name bound again at again, after first.
func twiceBindDiag(name string, again, first base.Span) diag.Diagnostic {
	return diag.NewError(diag.E(704), "pattern binds `"+name+"` twice", diag.Label{Span: again, Message: "bound again here"}).
		WithSecondary(diag.Label{Span: first, Message: "first bound here"}).
		WithNote("a pattern binds each name once; to compare two parts, bind them apart and use a guard")
}

// plural is "s" for any count but 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// arityDiag is the E0705 for the pattern at, which names named sub-patterns where
// the constructor, declared at decl, has fields of them. write is the pattern the
// help proposes, and edit the span that the fix-it replaces with it (noSpan: no
// fix-it).
func arityDiag(at base.Span, ctor string, fields, named int, decl base.Span, write string, edit base.Span) diag.Diagnostic {
	title := "`" + ctor + "` has no fields"
	if fields > 0 {
		title = "`" + ctor + "` has " + strconv.Itoa(fields) + " field" + plural(fields) + " but the pattern names " + strconv.Itoa(named)
	}
	d := diag.NewError(diag.E(705), title, diag.Label{Span: at, Message: strconv.Itoa(named) + " sub-pattern" + plural(named)}).
		WithSecondary(diag.Label{Span: decl, Message: "`" + ctor + "` is declared here"}).
		WithHelp("write `" + write + "`")
	if edit != noSpan {
		d = d.WithFixIt(diag.Replace(edit, write))
	}
	return d
}

// missingInPattern is the E0410 for a record pattern whose closing brace is at
// closing: title and label are the wording, declared the fields that are not
// named, and edit the fix-it of the help (nil: none).
func missingInPattern(closing base.Span, title, label string, declared []base.Span, edit *diag.FixIt) diag.Diagnostic {
	d := diag.NewError(diag.E(410), title, diag.Label{Span: closing, Message: label})
	for _, at := range declared {
		d = d.WithSecondary(diag.Label{Span: at, Message: "declared here"})
	}
	d = d.WithNote("a record pattern names every field, or ends with `..`").WithHelp("add `..` to skip the rest")
	if edit != nil {
		d = d.WithFixIt(*edit)
	}
	return d
}

// diagRow is a source and the diagnostics that Analyze must report for it, which
// the row writes with at, a function that finds a word of the source by quoting
// the text around it (the diagnostics are compared whole, fix-its included). fixed,
// when it is not empty, is the source after the one fix-it of the one diagnostic is
// applied, and the result must be a program with no diagnostics at all.
type diagRow struct {
	name, src, fixed string
	want             func(at func(context, word string) base.Span) []diag.Diagnostic
}

// checkRows runs each row of rows.
func checkRows(t *testing.T, rows []diagRow) {
	t.Helper()
	for _, row := range rows {
		p := testsupport.Analyze(t, row.src)
		at := func(context, word string) base.Span { return spanIn(t, p, row.src, context, word) }
		wantDiags(t, row.name, p, row.want(at)...)
		if row.fixed == "" {
			continue
		}
		if all := p.Session.Sink.All(); len(all) != 1 {
			t.Errorf("%s: %d diagnostics, a row with a fixed source has exactly 1", row.name, len(all))
		} else if fixed := testsupport.ApplyFixits(row.src, all[0]); fixed != row.fixed {
			t.Errorf("%s: the fix-it gives\n%s\nwant\n%s", row.name, fixed, row.fixed)
		} else if again := testsupport.Analyze(t, fixed); len(again.Session.Sink.All()) != 0 {
			t.Errorf("%s: the fixed program still reports %+v", row.name, again.Session.Sink.All())
		}
	}
}

// userModule returns the tree, the names and the types of the user's file of p.
func userModule(p *testsupport.Program) (*syntax.Module, *sema.ModuleRes, *sema.TypedModule) {
	user := p.Frontend.User
	return p.Frontend.Modules[user], &p.Frontend.Resolved.Modules[user], &p.Frontend.Typed.Modules[user]
}

// localsNamed returns the variables of the user's file called name, in id order.
func localsNamed(p *testsupport.Program, name string) []sema.LocalID {
	rp := p.Frontend.Resolved
	var found []sema.LocalID
	for id, l := range rp.Locals.All() {
		if rp.Defs.Get(l.Owner).Module == p.Frontend.User && p.Session.Interner.String(l.Name) == name {
			found = append(found, id)
		}
	}
	return found
}

// oneLocal returns the one variable of the user's file called name.
func oneLocal(t *testing.T, p *testsupport.Program, name string) sema.LocalID {
	t.Helper()
	found := localsNamed(p, name)
	if len(found) != 1 {
		t.Fatalf("%d variables are called %q, want exactly 1", len(found), name)
	}
	return found[0]
}

// localType is the type of the variable id, written as Show writes it.
func localType(p *testsupport.Program, id sema.LocalID) string {
	typed := p.Frontend.Typed
	return typed.Types.Show(typed.LocalTypes.Get(id))
}

// patType is the type of the one pattern of the user's file whose span is span.
func patType(t *testing.T, p *testsupport.Program, span base.Span) string {
	t.Helper()
	m, _, typed := userModule(p)
	var found []syntax.PatID
	for id, pat := range m.Pats.All() {
		if pat.Span == span {
			found = append(found, id)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d patterns have the span %v, want exactly 1", len(found), span)
	}
	return p.Frontend.Typed.Types.Show(typed.PatTypes[found[0]])
}

// listOf writes n copies of item, with sep between them.
func listOf(n int, item, sep string) string {
	return strings.TrimSuffix(strings.Repeat(item+sep, n), sep)
}

func TestPatternTypes(t *testing.T) {
	t.Run("variables_and_their_types", func(t *testing.T) {
		// Each variable of a pattern is one Local with the type of the part it
		// binds: in a constructor, a tuple, a record (a pun too, which has no
		// pattern node of its own), a nested pattern and the arms of an
		// or-pattern, which bind the same names and share one Local each. The
		// table of a pattern holds the type of the value it matches, and the
		// name of a constructor stands for it.
		const src = shapes + "fn f(s: Shape, p: Point, t: (Int, Bool)) -> Float {\n  let (n, b) = t\n  let Point { x, y: q } = p\n" +
			"  match s {\n    Circle(r) => r\n    Rect(w, _) | Circle(w) if b => w\n    Unit => 0.0\n  }\n}"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		for _, row := range []struct{ name, want string }{
			{"r", "Float"}, {"w", "Float"}, {"n", "Int"}, {"b", "Bool"}, {"x", "Int"}, {"q", "Int"},
		} {
			if got := localType(p, oneLocal(t, p, row.name)); got != row.want {
				t.Errorf("the variable %s has type %s, want %s", row.name, got, row.want)
			}
		}
		m, rm, _ := userModule(p)
		in := p.Session.Interner
		w := oneLocal(t, p, "w")
		var bound []sema.LocalID
		for id, pat := range m.Pats.All() {
			if pat.Kind == syntax.PatBind && in.String(pat.Name) == "w" {
				bound = append(bound, rm.PatLocal[id])
			}
		}
		if !slices.Equal(bound, []sema.LocalID{w, w}) {
			t.Errorf("the two w bind %v, want the variable %d twice", bound, w)
		}
		if got, want := p.Frontend.Resolved.Locals.Get(w).Span, spanIn(t, p, src, "Rect(w, _)", "w"); got != want {
			t.Errorf("w is bound at %v, want its first occurrence %v", got, want)
		}
		x := oneLocal(t, p, "x")
		var puns []sema.LocalID
		for i, f := range m.FieldPatPool {
			if f.Name == in.InternString("x") && !f.HasPat {
				puns = append(puns, rm.FieldPatLocal[i])
			}
		}
		if !slices.Equal(puns, []sema.LocalID{x}) {
			t.Errorf("the pun x binds %v, want [%d]", puns, x)
		}
		if got, want := p.Frontend.Resolved.Locals.Get(x).Span, spanIn(t, p, src, "Point { x,", "x"); got != want {
			t.Errorf("x is bound at %v, want %v", got, want)
		}
		// A pun is found by its place among the fields of every record pattern of the
		// file, and not among those of its own pattern: the puns of a second pattern
		// come after the first's, and each is a variable of its own with the type of
		// its own field.
		const twice = "type P { v: Int }\ntype Q { v: Bool }\nfn g(p: P, q: Q) -> Int {\n  let P { v } = p\n  let Q { v } = q\n  if v { 1 } else { 0 }\n}"
		pq := testsupport.Analyze(t, twice)
		wantCodes(t, pq)
		vs := localsNamed(pq, "v")
		if len(vs) != 2 || localType(pq, vs[0]) != "Int" || localType(pq, vs[1]) != "Bool" {
			t.Fatalf("the two puns v are %v, want two variables of types Int and Bool", vs)
		}
		mq, rq, _ := userModule(pq)
		var pooled []sema.LocalID
		for i, f := range mq.FieldPatPool {
			if !f.HasPat {
				pooled = append(pooled, rq.FieldPatLocal[i])
			}
		}
		if !slices.Equal(pooled, vs) {
			t.Errorf("the puns of the two patterns bind %v, want %v", pooled, vs)
		}
		for _, row := range []struct{ context, word, want string }{
			{"Circle(r) =>", "Circle(r)", "Shape"},
			{"(r) =>", "r", "Float"},
			{"Rect(w, _) |", "Rect(w, _)", "Shape"},
			{"Rect(w, _) |", "_", "Float"},
			{"| Circle(w) if", "Circle(w)", "Shape"},
			{"Rect(w, _) | Circle(w)", "Rect(w, _) | Circle(w)", "Shape"},
			{"let (n, b)", "(n, b)", "(Int, Bool)"},
			{"let (n, b)", "b", "Bool"},
			{"let Point { x, y: q }", "Point { x, y: q }", "Point"},
			{"y: q }", "q", "Int"},
			{"Unit =>", "Unit", "Shape"},
		} {
			if got := patType(t, p, spanIn(t, p, src, row.context, row.word)); got != row.want {
				t.Errorf("the pattern %s has type %s, want %s", row.word, got, row.want)
			}
		}
		// A pattern that has no type to say, a Float literal (E0706), the name of a
		// constructor that does not exist (E0306) and that of a record type that does
		// not (E0406), has the error type, and so has one that the checker never reached.
		const odd = "type S { A(Int) }\nfn f(n: Int, s: S) -> Int {\n  match n {\n    1.5 => 1\n    _ => 2\n  }\n}\nfn g(s: S) -> Int {\n  match s {\n    Aa(x) => x\n    _ => 0\n  }\n}\n" +
			"fn h(s: S) -> Int {\n  match s {\n    Poin { x } => 1\n    _ => 0\n  }\n}"
		q := testsupport.Analyze(t, odd)
		wantCodes(t, q, "E0706", "E0306", "E0406")
		for _, row := range []struct{ context, word string }{{"1.5 =>", "1.5"}, {"Aa(x)", "Aa(x)"}, {"Aa(x)", "x"}, {"Poin { x }", "Poin { x }"}} {
			if got := patType(t, q, spanIn(t, q, odd, row.context, row.word)); got != "{error}" {
				t.Errorf("the pattern %s has type %s, want {error}", row.word, got)
			}
		}
		for id, pat := range m.Pats.All() {
			if pat.Kind != syntax.PatCtor {
				continue
			}
			res := rm.PatRes[id]
			if pat.Record {
				// The name of a record pattern is a type, which Check looks up.
				if res != (sema.Res{}) {
					t.Errorf("the record pattern %s stands for %+v, want nothing", in.String(pat.Name), res)
				}
				continue
			}
			def := p.Frontend.Resolved.Defs.Get(res.Def)
			if res.Kind != sema.ResDef || def.Kind != sema.DefCtor || def.Name != pat.Name {
				t.Errorf("the constructor %s stands for %+v (%+v)", in.String(pat.Name), res, def)
			}
		}
	})

	t.Run("scopes_of_a_pattern", func(t *testing.T) {
		// The variables of a pattern are visible in its guard and its body, and
		// nowhere else: not in the scrutinee, not in another arm, not after the
		// match. A `let` with a pattern binds them for the rest of its block and not
		// in its own initializer. A variable shadows what has its name outside, and
		// two arms that bind one name bind two variables.
		rows := []struct {
			name, src string
			want      []string
		}{
			{"the guard and the body", "fn f(n: Int) -> Int { match n { m if m > 0 => m\n _ => 0 } }", nil},
			{"the scrutinee names an outer variable", "fn f(m: Int) -> Int { match m { m => m } }", nil},
			{"another arm", "fn f(n: Int) -> Int { match n { m => 1\n _ => m } }", []string{"E0301"}},
			{"the scrutinee does not see them", "fn f(n: Int) -> Int { match m { m => m } }", []string{"E0301"}},
			{"a guard does not see the next arm", "fn f(n: Int) -> Int { match n { m if k => 1\n k => 2 } }", []string{"E0301"}},
			{"after the match", "fn f(n: Int) -> Int { let a = match n { m => m }\n m }", []string{"E0301"}},
			{"shadowing in the arm only", "fn f(n: Bool) -> Int { let a = match 1 { n => n + 1 }\n if n { a } else { 0 } }", nil},
			{"a let sees the rest of its block", "fn f() -> Int { let (a, b) = (1, 2)\n let c = a + b\n c }", nil},
			{"a let does not see itself", "fn f() -> Int { let (a, b) = (a, 1)\n b }", []string{"E0301"}},
			{"a match in the body of an arm", "fn f(n: Int) -> Int { match n { a => match a { b => a + b } } }", nil},
			{"a match in a guard", "fn f(n: Int) -> Int { match n { a if match a { b => b > 0 } => a\n _ => 0 } }", nil},
			{"one name at two types in two arms", "fn f(t: (Int, Bool)) -> Int { match t { (a, true) => a\n (_, a) => if a { 1 } else { 2 } } }", nil},
		}
		for _, row := range rows {
			p := testsupport.Analyze(t, row.src)
			if got := testsupport.ErrorCodes(p); !slices.Equal(got, row.want) {
				t.Errorf("%s: %q reports %v, want %v: %+v", row.name, row.src, got, row.want, p.Session.Sink.All())
			}
		}
		const two = "fn f(n: Int) -> Int { match n { a => a\n a if a > 0 => a } }"
		p := testsupport.Analyze(t, two)
		wantCodes(t, p)
		id, _ := fnNamed(t, p, "f")
		ids := localsNamed(p, "a")
		if len(ids) != 2 {
			t.Fatalf("two arms bind %d variables called a, want 2", len(ids))
		}
		for i, at := range []string{"a => a\n", "a if"} {
			l := p.Frontend.Resolved.Locals.Get(ids[i])
			if want := spanIn(t, p, two, at, "a"); l.Span != want || l.Owner != id {
				t.Errorf("arm %d binds %+v, want %v owned by %d", i, l, want, id)
			}
		}
		m, rm, _ := userModule(p)
		var uses []sema.LocalID
		for eid, e := range m.Exprs.All() {
			if e.Kind == syntax.ExprName && p.Session.Interner.String(e.Name) == "a" {
				uses = append(uses, rm.ExprRes[eid].Local)
			}
		}
		if want := []sema.LocalID{ids[0], ids[1], ids[1]}; !slices.Equal(uses, want) {
			t.Errorf("the uses of a stand for %v, want %v", uses, want)
		}
	})

	t.Run("arms_have_the_type_of_the_first", func(t *testing.T) {
		// With nothing expected of the match, the first arm gives it its type and
		// every other arm is checked against that one: the secondary label is on
		// the first arm's value (its tail, if it is a block). A match whose arms
		// disagree has the error type, so the place that uses it says nothing (here a
		// `let` with an annotation and an addition), and an error in the first arm is
		// the only one. A match with no arms is the error type too.
		const src = "fn f(n: Int) -> Int {\n  let x = match n {\n    0 => 1\n    1 => true\n    _ => 2.5\n  }\n  let y = match n {\n" +
			"    0 => {\n      let a = 1\n      a\n    }\n    _ => false\n  }\n  let z: Bool = x\n  x + y\n}"
		first := "the first arm has type `Int`"
		checkRows(t, []diagRow{{name: "arms", src: src, want: func(at func(string, string) base.Span) []diag.Diagnostic {
			return []diag.Diagnostic{
				mismatchBecause(at("1 => true", "true"), "Int", "Bool", at("0 => 1", "1"), first),
				mismatchBecause(at("_ => 2.5", "2.5"), "Int", "Float", at("0 => 1", "1"), first),
				mismatchBecause(at("_ => false", "false"), "Int", "Bool", at("      a\n", "a"), first),
			}
		}}})
		p := testsupport.Analyze(t, "fn f(n: Int) -> Int { let z = match n { 0 => zzz\n _ => true }\n 1 }")
		wantCodes(t, p, "E0301")
		// A match whose arms agree has their type, and what uses it is checked against
		// it: a use that fits (`w + 1`) says nothing, and one that does not (a `Bool`
		// annotation) is the one E0401, at the use.
		const agreeing = "fn f(n: Int) -> Int {\n  let w = match n {\n    0 => 1\n    _ => 2\n  }\n  let b: Bool = w\n  w + 1\n}"
		checkRows(t, []diagRow{{name: "a use of a match that agrees", src: agreeing, want: func(at func(string, string) base.Span) []diag.Diagnostic {
			return []diag.Diagnostic{mismatchBecause(at("= w\n", "w"), "Bool", "Int", at(": Bool", "Bool"), "expected because of this annotation")}
		}}})
		// A match with no arms has no type to say: it is the error type, which
		// fits where a number is added to it, where an `Int` is returned and where a
		// `Bool` is.
		p = testsupport.Analyze(t, "fn f(n: Int) -> Int {\n  let x = match n {\n  }\n  x + 1\n}\nfn g(n: Int) -> Int { match n { } }\n"+
			"fn h(n: Int) -> Bool {\n  let b = match n {\n  }\n  b\n}")
		wantCodes(t, p)
	})

	t.Run("an_expected_type_goes_to_every_arm", func(t *testing.T) {
		// A match checked against a type hands it to each arm, as a block hands it to
		// its tail: each arm that does not fit is its own error with the reason of
		// the place (the return type, a parameter, an annotation), and an Int literal
		// where a Float is expected has its edit, which a match that inferred its
		// arms and compared the result would never offer.
		checkRows(t, []diagRow{
			{name: "return type", src: "fn f(n: Int) -> Float {\n  match n {\n    0 => 1\n    _ => 2.0\n  }\n}",
				fixed: "fn f(n: Int) -> Float {\n  match n {\n    0 => 1.0\n    _ => 2.0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{floatEdit(mismatchBecause(at("0 => 1", "1"), "Float", "Int", at("-> Float", "Float"), "expected because of the return type"),
						at("0 => 1", "1"), "1.0")}
				}},
			{name: "two arms", src: "fn f(n: Int) -> Int {\n  match n {\n    0 => 1\n    1 => true\n    _ => 2.5\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						mismatchBecause(at("1 => true", "true"), "Int", "Bool", at("-> Int", "Int"), "expected because of the return type"),
						mismatchBecause(at("_ => 2.5", "2.5"), "Int", "Float", at("-> Int", "Int"), "expected because of the return type"),
					}
				}},
			{name: "argument", src: "fn h(a: Int) {}\nfn g(n: Int) {\n  h(match n {\n    0 => 1\n    _ => true\n  })\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{mismatchBecause(at("_ => true", "true"), "Int", "Bool", at("h(a: Int)", "a: Int"), "the parameter is declared here")}
				}},
			{name: "annotation", src: "fn g(n: Int) {\n  let y: Int = match n {\n    0 => 1\n    _ => true\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{mismatchBecause(at("_ => true", "true"), "Int", "Bool", at("y: Int", "Int"), "expected because of this annotation")}
				}},
			{name: "in a block tail", src: "fn f(n: Int) -> Int {\n  let a = 1\n  match n {\n    0 => a\n    _ => false\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{mismatchBecause(at("_ => false", "false"), "Int", "Bool", at("-> Int", "Int"), "expected because of the return type")}
				}},
			// A match that does not fit has the error type, so what the `if` makes of
			// it, and what is done with that, says nothing more; one that fits has
			// its type, and the same `let` is the one error.
			{name: "a match that does not fit", src: "fn f(n: Int, b: Bool) -> Bool {\n  let v = if b { 1 } else {\n    match n {\n      0 => 2\n      _ => true\n    }\n  }\n  let q: Bool = v\n  q\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{mismatchBecause(at("_ => true", "true"), "Int", "Bool", at("{ 1 }", "1"), "the other branch has type `Int`")}
				}},
			// Where `()` is wanted, as at the end of a function with no `->`, a value that is
			// unused has a help, and what it offers depends on where a `let` can be
			// written: after the `=>` it cannot, so an arm that is not a block is told to
			// become one, and an arm that is a block, or a block inside the arm, binds the
			// value as anywhere. `main` cannot return it, and the arm help says so by
			// leaving the half out.
			{name: "a bare arm where unit is wanted", src: "type S { A, B(Int) }\nfn f(s: S) {\n  match s {\n    A => 1\n    B(n) => n\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						secondary(mismatch(at("A => 1", "1"), "expected `()`, found `Int`"), at(" f(", "f"), "expected because of the return type").WithHelp(unusedArmHelp),
						secondary(mismatch(at("=> n\n", "n"), "expected `()`, found `Int`"), at(" f(", "f"), "expected because of the return type").WithHelp(unusedArmHelp),
					}
				}},
			{name: "a block arm where unit is wanted", src: "fn f(n: Int) {\n  match n {\n    0 => { 1 }\n    _ => { 2 }\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						secondary(mismatch(at("{ 1 }", "1"), "expected `()`, found `Int`"), at(" f(", "f"), "expected because of the return type").WithHelp(unusedHelp),
						secondary(mismatch(at("{ 2 }", "2"), "expected `()`, found `Int`"), at(" f(", "f"), "expected because of the return type").WithHelp(unusedHelp),
					}
				}},
			{name: "a bare arm in main", src: "fn main() {\n  match true {\n    true => 1\n    false => 2\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						secondary(mismatch(at("true => 1", "1"), "expected `()`, found `Int`"), at(" main(", "main"), "expected because of the return type").WithHelp(unusedArmBindHelp),
						secondary(mismatch(at("false => 2", "2"), "expected `()`, found `Int`"), at(" main(", "main"), "expected because of the return type").WithHelp(unusedArmBindHelp),
					}
				}},
			{name: "an if with an else as an arm", src: "fn f(n: Int) {\n  match n {\n    0 => if true { 1 } else { 2 }\n    _ => ()\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{secondary(mismatch(at("0 => if true { 1 } else { 2 }", "if true { 1 } else { 2 }"), "expected `()`, found `Int`"), at(" f(", "f"), "expected because of the return type").WithHelp(unusedArmHelp)}
				}},
			{name: "an if with no else as an arm", src: "fn f(n: Int) {\n  match n {\n    0 => if true { 1 }\n    _ => ()\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{secondary(mismatch(at("{ 1 }", "1"), "expected `()`, found `Int`"), at(" f(", "f"), "expected because of the return type").WithHelp(unusedBindHelp)}
				}},
			{name: "a match that fits", src: "fn f(n: Int, b: Bool) -> Bool {\n  let v = if b { 1 } else {\n    match n {\n      0 => 2\n      _ => 3\n    }\n  }\n  let q: Bool = v\n  q\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{mismatchBecause(at("= v", "v"), "Bool", "Int", at("q: Bool", "Bool"), "expected because of this annotation")}
				}},
		})
		// The type of a match that fits is the expected one, not the scrutinee's.
		src := "fn f(b: Bool) -> Int { match b { true => 1\n _ => 2 } }"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		if got := exprType(t, p, spanIn(t, p, src, "match b { true => 1\n _ => 2 }", "match b { true => 1\n _ => 2 }")); got != "Int" {
			t.Errorf("the match has type %s, want Int", got)
		}
		// One that does not fit is the error type, which Show writes {error}.
		const bad = "fn f(n: Int) -> Int {\n  match n {\n    0 => 1\n    _ => true\n  }\n}"
		p = testsupport.Analyze(t, bad)
		if got := exprType(t, p, spanIn(t, p, bad, "match n {\n    0 => 1\n    _ => true\n  }", "match n {\n    0 => 1\n    _ => true\n  }")); got != "{error}" {
			t.Errorf("the match that does not fit has type %s, want {error}", got)
		}
	})

	t.Run("a_pattern_has_the_type_of_the_scrutinee", func(t *testing.T) {
		// ReasonScrutinee: a pattern that does not fit the value that is matched is
		// an E0401 on the pattern, with the value as the secondary label and its
		// whole type in it, which for a pattern inside a tuple is the tuple's. A
		// constructor that does not fit says nothing about its own sub-patterns.
		// A parameter with no annotation takes the type its first pattern says.
		const src = shapes + "fn f(s: Shape, n: Int) -> Int {\n  let a = match s {\n    0 => 1\n    _ => 2\n  }\n  let b = match n {\n" +
			"    true => 1\n    Circle(r) => 2\n    Circle(true) => 3\n    Circle(Rect(a)) => 5\n    _ => 4\n  }\n  let c = match (1, true) {\n    (_, 2) => 1\n    _ => 2\n  }\n  a + b + c\n}"
		checkRows(t, []diagRow{{name: "scrutinee", src: src, want: func(at func(string, string) base.Span) []diag.Diagnostic {
			return []diag.Diagnostic{
				scrutineeMismatch(at("    0 => 1", "0"), "Shape", "`Int`", at("match s {", "s"), "Shape"),
				scrutineeMismatch(at("true => 1", "true"), "Int", "`Bool`", at("match n {", "n"), "Int"),
				scrutineeMismatch(at("Circle(r) => 2", "Circle(r)"), "Int", "`Shape`", at("match n {", "n"), "Int"),
				scrutineeMismatch(at("Circle(true) => 3", "Circle(true)"), "Int", "`Shape`", at("match n {", "n"), "Int"),
				scrutineeMismatch(at("Circle(Rect(a))", "Circle(Rect(a))"), "Int", "`Shape`", at("match n {", "n"), "Int"),
				arityDiag(at("Rect(a))", "Rect(a)"), "Rect", 2, 1, at("Rect(w: Float, h: Float)", "Rect"), "Rect(_, _)", at("Rect(a))", "Rect(a)")),
				scrutineeMismatch(at("(_, 2)", "2"), "Bool", "`Int`", at("(1, true)", "(1, true)"), "(Int, Bool)"),
			}
		}}})
		// The same label where the match is checked against a type (here the return
		// type) and not inferred.
		checkRows(t, []diagRow{{name: "checked match", src: shapes + "fn pad() -> Int { 7 }\nfn f(s: Shape) -> Int {\n  match s {\n    0 => 1\n    _ => 2\n  }\n}",
			want: func(at func(string, string) base.Span) []diag.Diagnostic {
				return []diag.Diagnostic{scrutineeMismatch(at("0 => 1", "0"), "Shape", "`Int`", at("match s {", "s"), "Shape")}
			}}})
		for _, row := range []struct{ src, fn, want string }{
			{"fn g(x) -> Int { match x {\n Circle(_) => 1\n _ => 2 } }\n" + shapes, "g", "(Shape) -> Int"},
			{"fn g(x) -> Int { match x {\n 0 => 1\n _ => 2 } }", "g", "(Int) -> Int"},
			{"fn g(x) -> Int { match x {\n true => 1\n _ => 2 } }", "g", "(Bool) -> Int"},
			{"fn g(t) -> Int { match t {\n (a, true) => a\n _ => 2 } }", "g", "((Int, Bool)) -> Int"},
			{"fn g(p) -> Int { match p {\n Point { x, y } => x + y } }\n" + shapes, "g", "(Point) -> Int"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantCodes(t, p)
			if got := testsupport.DefType(t, p, row.fn); got != row.want {
				t.Errorf("%q: %s has type %s, want %s", row.src, row.fn, got, row.want)
			}
		}
	})

	t.Run("a_sub_pattern_has_the_type_of_its_field", func(t *testing.T) {
		// ReasonPatternArg: the sub-patterns of a constructor or a record pattern are
		// checked against the types of its fields, and the secondary label is on the
		// field in its declaration, a tuple type inside a field too. A pun is a
		// variable of the field's type.
		const src = shapes + "type Pair { P((Int, Bool)) }\nfn f(s: Shape, p: Point, q: Pair) -> Int {\n  let a = match s {\n    Circle(true) => 1\n" +
			"    Rect(1, w) => 2\n    _ => 3\n  }\n  let b = match p {\n    Point { x: true, y } => y\n    _ => 4\n  }\n" +
			"  let c = match q {\n    P((a, 2)) => 5\n    _ => 6\n  }\n  a + b + c\n}"
		checkRows(t, []diagRow{{name: "fields", src: src, want: func(at func(string, string) base.Span) []diag.Diagnostic {
			return []diag.Diagnostic{
				fieldMismatch(at("Circle(true)", "true"), "Float", "Bool", at("Circle(r: Float)", "r: Float")),
				fieldMismatch(at("Rect(1, w)", "1"), "Float", "Int", at("Rect(w: Float, h: Float)", "w: Float")),
				fieldMismatch(at("x: true", "true"), "Int", "Bool", at("Point { x: Int", "x: Int")),
				fieldMismatch(at("(a, 2)", "2"), "Bool", "Int", at("P((Int, Bool))", "(Int, Bool)")),
			}
		}}})
		p := testsupport.Analyze(t, src)
		if got := localType(p, oneLocal(t, p, "w")); got != "Float" {
			t.Errorf("w has type %s, want Float", got)
		}
		// The fields of one constructor and of one record have types of their own, so
		// each sub-pattern is compared with its own field and not with the first: the
		// mistake on the second field is labelled on the second, and the arms that are
		// right (a `Bool` used as a condition, an `Int` as a number) have no error.
		const diff = "type T { A(Int, Bool) }\ntype R { n: Int, b: Bool }\nfn f(t: T, r: R) -> Int {\n  let a = match t {\n    A(1, 2) => 1\n" +
			"    A(n, b) => if b { n } else { 0 }\n  }\n  let c = match r {\n    R { n: 1, b: 2 } => 1\n    R { n, b } => if b { n } else { 0 }\n  }\n  a + c\n}"
		checkRows(t, []diagRow{{name: "fields of different types", src: diff, want: func(at func(string, string) base.Span) []diag.Diagnostic {
			return []diag.Diagnostic{
				fieldMismatch(at("A(1, 2)", "2"), "Bool", "Int", at("A(Int, Bool)", "Bool")),
				fieldMismatch(at("b: 2", "2"), "Bool", "Int", at("b: Bool", "b: Bool")),
			}
		}}})
	})

	t.Run("a_guard_must_be_bool", func(t *testing.T) {
		// The guard of an arm is checked against Bool, with no secondary label and
		// a note of its own, which is not the note of an `if` condition.
		checkRows(t, []diagRow{
			{name: "guard", src: "fn f(n: Int) -> Int {\n  match n {\n    m if m => 1\n    _ => 0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{mismatch(at("if m =>", "m"), "expected `Bool`, found `Int`").WithNote("a guard must be `Bool`")}
				}},
			{name: "condition", src: "fn f() {\n  if 1 {}\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{mismatch(at("if 1", "1"), "expected `Bool`, found `Int`").WithNote("an `if` condition must be `Bool`")}
				}},
		})
		p := testsupport.Analyze(t, "fn f(n: Int) -> Int { match n { m if m > 0 => m\n _ => 0 } }")
		wantCodes(t, p)
	})

	t.Run("or_patterns_bind_one_variable_of_one_type", func(t *testing.T) {
		// The alternatives of an or-pattern are checked against the same type and
		// bind the same variable, so a name that has two types in two alternatives
		// is an E0401 at the second, with no secondary label, and an alternative that
		// is not the type of the value is the same error as any pattern.
		const src = "type U { A(Int), B(Bool) }\nfn f(u: U, n: Int) -> Int {\n  let a = match u {\n    A(x) | B(x) => 1\n    _ => 0\n  }\n" +
			"  let b = match n {\n    0 | true => 1\n    _ => 2\n  }\n  a + b\n}"
		checkRows(t, []diagRow{{name: "or", src: src, want: func(at func(string, string) base.Span) []diag.Diagnostic {
			return []diag.Diagnostic{
				mismatch(at("B(x)", "x"), "expected `Int`, found `Bool`"),
				scrutineeMismatch(at("0 | true", "true"), "Int", "`Bool`", at("match n {", "n"), "Int"),
			}
		}}})
		// An or-pattern inside an alternative of another one binds the name the outer
		// alternatives share, and not a second variable: the `x` of `C(x)` is a `Bool`,
		// so the two `x` of the inner alternatives, which are `Int`, are one E0401 each.
		const nested = "type V { A(Int), B(Int), C(Bool) }\nfn g(t: (V, Int)) -> Int {\n  match t {\n    (C(x), 2) | (A(x) | B(x), 1) => 1\n    _ => 0\n  }\n}"
		checkRows(t, []diagRow{{name: "nested or-patterns", src: nested, want: func(at func(string, string) base.Span) []diag.Diagnostic {
			return []diag.Diagnostic{
				mismatch(at("A(x)", "x"), "expected `Bool`, found `Int`"),
				mismatch(at("B(x)", "x"), "expected `Bool`, found `Int`"),
			}
		}}})
	})

	t.Run("e0703_in_both_directions", func(t *testing.T) {
		// An or-pattern whose alternatives bind different names: one error for each
		// name, on the first alternative that lacks it, with the first that binds it
		// as the secondary label, whichever of the two comes first. The name stays
		// bound, so the body that uses it adds nothing.
		const u = "type U { A(Int), B, C(Int) }\ntype W { W(U) }\n"
		const v = "type V { A(Int), B(Int), C, D }\n"
		checkRows(t, []diagRow{
			{name: "second lacks it", src: u + "fn f(u: U) -> Int {\n  match u {\n    A(x) | B => 1\n    _ => 0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{orDiag("x", at("| B =>", "B"), at("A(x)", "A(x)"))}
				}},
			{name: "first lacks it", src: u + "fn f(u: U) -> Int {\n  match u {\n    B | A(x) => 1\n    _ => 0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{orDiag("x", at("B | A(x)", "B"), at("A(x)", "A(x)"))}
				}},
			{name: "the body uses it", src: u + "fn f(u: U) -> Int {\n  match u {\n    A(x) | B => x\n    _ => 0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{orDiag("x", at("| B =>", "B"), at("A(x)", "A(x)"))}
				}},
			{name: "three alternatives", src: u + "fn f(u: U) -> Int {\n  match u {\n    A(x) | B | C(x) => 1\n    _ => 0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{orDiag("x", at("| B |", "B"), at("A(x)", "A(x)"))}
				}},
			{name: "two alternatives lack it", src: u + "fn f(u: U) -> Int {\n  match u {\n    B | A(x) | B => 1\n    _ => 0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{orDiag("x", at("B | A(x)", "B"), at("A(x)", "A(x)"))}
				}},
			// A name that two alternatives bind and a third lacks is reported on the
			// third; one that the first binds and two lack, on the first of the two.
			{name: "two bind it and a third lacks it", src: v + "fn f(v: V) -> Int {\n  match v {\n    A(x) | B(x) | C => 1\n    _ => 0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{orDiag("x", at("| C =>", "C"), at("A(x)", "A(x)"))}
				}},
			{name: "one binds it and two lack it", src: v + "fn f(v: V) -> Int {\n  match v {\n    A(x) | C | D => 1\n    _ => 0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{orDiag("x", at("| C |", "C"), at("A(x)", "A(x)"))}
				}},
			{name: "each lacks a name of the other", src: "fn f(t: (Int, Int)) -> Int {\n  match t {\n    (x, y) | (x, z) => 1\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						orDiag("y", at("(x, z)", "(x, z)"), at("(x, y)", "(x, y)")),
						orDiag("z", at("(x, y)", "(x, y)"), at("(x, z)", "(x, z)")),
					}
				}},
			{name: "three names", src: "fn f(t: (Int, Int)) -> Int {\n  match t {\n    (x, y) | (z, _) => 1\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						orDiag("x", at("(z, _)", "(z, _)"), at("(x, y)", "(x, y)")),
						orDiag("y", at("(z, _)", "(z, _)"), at("(x, y)", "(x, y)")),
						orDiag("z", at("(x, y)", "(x, y)"), at("(z, _)", "(z, _)")),
					}
				}},
			{name: "inside a constructor", src: u + "fn f(w: W) -> Int {\n  match w {\n    W(A(x) | B) => 1\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{orDiag("x", at("| B)", "B"), at("A(x)", "A(x)"))}
				}},
		})
	})

	t.Run("e0704_in_tuples_and_records", func(t *testing.T) {
		// A pattern binds each name once: a tuple, a record with a field and with a
		// pun, a constructor, a `let`, the third use of one name, and an or-pattern
		// whose alternatives each bind a name that the pattern bound before. The
		// variable of the repeat is not the one the body sees.
		const p = "type U { A(Int), B(Int) }\n"
		checkRows(t, []diagRow{
			{name: "tuple", src: "fn f(t: (Int, Int)) -> Int {\n  match t {\n    (x, x) => x\n    _ => 0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{twiceBindDiag("x", at(", x)", "x"), at("(x,", "x"))}
				}},
			{name: "record", src: shapes + "fn f(p: Point) -> Int {\n  match p {\n    Point { x: a, y: a } => a\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{twiceBindDiag("a", at("y: a", "a"), at("x: a", "a"))}
				}},
			{name: "pun and field", src: shapes + "fn f(p: Point) -> Int {\n  match p {\n    Point { x, y: x } => x\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{twiceBindDiag("x", at("y: x", "x"), at("{ x,", "x"))}
				}},
			{name: "constructor", src: shapes + "fn f(s: Shape) -> Float {\n  match s {\n    Rect(w, w) => w\n    _ => 0.0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{twiceBindDiag("w", at(", w)", "w"), at("Rect(w,", "w"))}
				}},
			{name: "let", src: "fn f() -> Int {\n  let (a, a) = (1, 2)\n  a\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{twiceBindDiag("a", at(", a)", "a"), at("(a,", "a"))}
				}},
			{name: "three times", src: "fn f(t: (Int, Int, Int)) -> Int {\n  match t {\n    (x, x, x) => x\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						twiceBindDiag("x", at(", x, x)", "x"), at("(x,", "x")),
						twiceBindDiag("x", at(", x)", "x"), at("(x,", "x")),
					}
				}},
			{name: "a binding after the alternatives", src: p + "fn f(t: (U, Int)) -> Int {\n  match t {\n    (A(x) | B(x), x) => x\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{twiceBindDiag("x", at(", x)", "x"), at("A(x)", "x"))}
				}},
			{name: "alternatives after a binding", src: p + "fn f(t: (Int, U)) -> Int {\n  match t {\n    (x, A(x) | B(x)) => x\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						twiceBindDiag("x", at("A(x)", "x"), at("(x,", "x")),
						twiceBindDiag("x", at("B(x)", "x"), at("(x,", "x")),
					}
				}},
		})
		const src = "fn f(t: (Int, Int)) -> Int { match t { (x, x) => x\n _ => 0 } }"
		p2 := testsupport.Analyze(t, src)
		m, rm, _ := userModule(p2)
		first := oneLocalAt(t, p2, src, "(x, x)", "x")
		for eid, e := range m.Exprs.All() {
			if e.Kind == syntax.ExprName && p2.Session.Interner.String(e.Name) == "x" && rm.ExprRes[eid].Local != first {
				t.Errorf("the body's x stands for %d, want the first, %d", rm.ExprRes[eid].Local, first)
			}
		}
		if got := len(localsNamed(p2, "x")); got != 2 {
			t.Errorf("%d variables are called x, want 2: the repeat has its own", got)
		}
	})

	t.Run("e0705_more_and_fewer", func(t *testing.T) {
		// A constructor pattern names as many sub-patterns as its constructor has
		// fields: more, fewer, none for a constructor that has some, and any for one
		// that has none (which says it has no fields). The help writes the pattern
		// with a wildcard for each field and, when the pattern is on one line, the
		// edit does; applied, the program has no diagnostics. The variables of a
		// pattern that does not fit are bound all the same.
		const head = shapes + "fn f(s: Shape) -> Float {\n  match s {\n"
		const tail = "    _ => 0.0\n  }\n}"
		checkRows(t, []diagRow{
			{name: "more", src: head + "    Circle(a, b) => 1.0\n" + tail,
				fixed: head + "    Circle(_) => 1.0\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{arityDiag(at("Circle(a, b)", "Circle(a, b)"), "Circle", 1, 2, at("Circle(r: Float)", "Circle"),
						"Circle(_)", at("Circle(a, b)", "Circle(a, b)"))}
				}},
			{name: "fewer", src: head + "    Rect(a) => 1.0\n" + tail,
				fixed: head + "    Rect(_, _) => 1.0\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{arityDiag(at("Rect(a)", "Rect(a)"), "Rect", 2, 1, at("Rect(w: Float, h: Float)", "Rect"),
						"Rect(_, _)", at("Rect(a)", "Rect(a)"))}
				}},
			{name: "none written", src: head + "    Rect => 1.0\n" + tail,
				fixed: head + "    Rect(_, _) => 1.0\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{arityDiag(at("Rect =>", "Rect"), "Rect", 2, 0, at("Rect(w: Float, h: Float)", "Rect"),
						"Rect(_, _)", at("Rect =>", "Rect"))}
				}},
			{name: "a constructor with no fields", src: head + "    Unit(x) => 1.0\n" + tail,
				fixed: head + "    Unit => 1.0\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{arityDiag(at("Unit(x)", "Unit(x)"), "Unit", 0, 1, at("Unit }", "Unit"), "Unit", at("Unit(x)", "Unit(x)"))}
				}},
			{name: "over several lines", src: head + "    Rect(\n      a\n    ) => 1.0\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{arityDiag(at("Rect(\n      a\n    )", "Rect(\n      a\n    )"), "Rect", 2, 1, at("Rect(w: Float, h: Float)", "Rect"),
						"Rect(_, _)", noSpan)}
				}},
		})
		p := testsupport.Analyze(t, head+"    Rect(a) => a\n"+tail)
		wantCodes(t, p, "E0705")
		// An arm that the parser had trouble with is no place to count sub-patterns: a
		// `(` that is left out makes `Rect w) => w` an arm of the pattern `Rect`, and
		// the edit that writes `Rect(_, _)` would leave the line as broken as it was.
		// The parse error is the only one, and `w` is not an unknown name; the arms
		// around it that are fine are still judged.
		p = testsupport.Analyze(t, head+"    Rect w) => w\n"+tail)
		wantCodes(t, p, "E0201")
		p = testsupport.Analyze(t, head+"    Circle(a, b) => 1.0\n    Rect w) => w\n"+tail)
		wantCodes(t, p, "E0201", "E0705")
		// The mark is for the arm alone: with the broken arm the last of its match, a
		// `let` that comes after counts its sub-patterns all the same.
		p = testsupport.Analyze(t, head+"    Circle(r) => r\n    Rect w) => w\n  }\n}\nfn g(s: Shape) -> Float {\n  let Circle(a, b) = s\n  0.0\n}")
		wantCodes(t, p, "E0201", "E0705")
		// What is inside a pattern that does not fit is read all the same: a second
		// constructor with its own number of sub-patterns is its own error.
		checkRows(t, []diagRow{{name: "a pattern inside one", src: head + "    Rect(Circle(a, b)) => 1.0\n" + tail,
			want: func(at func(string, string) base.Span) []diag.Diagnostic {
				return []diag.Diagnostic{
					arityDiag(at("Rect(Circle(a, b))", "Rect(Circle(a, b))"), "Rect", 2, 1, at("Rect(w: Float, h: Float)", "Rect"), "Rect(_, _)", at("Rect(Circle(a, b))", "Rect(Circle(a, b))")),
					arityDiag(at("Circle(a, b)", "Circle(a, b)"), "Circle", 1, 2, at("Circle(r: Float)", "Circle"), "Circle(_)", at("Circle(a, b)", "Circle(a, b)")),
				}
			}}})
	})

	t.Run("record_patterns", func(t *testing.T) {
		// A record pattern is read as a record literal is: an unknown field is E0409
		// (a pun keeps its variable in the edit), a field named twice E0411, fields
		// that are not named are E0410 unless the pattern ends with `..`, and a name
		// that is not a record is E0412 or E0406. The E0410 edit writes the `..`
		// where it reads best and never a second comma, and it is offered only for a
		// pattern on one line.
		const cube = "type Cube { x: Int, y: Int, z: Int }\ntype P = Point\n" + shapes
		fn := func(pattern string) string {
			return cube + "fn f(p: Point, c: Cube) -> Int {\n  match p {\n    " + pattern + " => 1\n    _ => 0\n  }\n}"
		}
		cubeFn := func(pattern string) string {
			return cube + "fn f(c: Cube) -> Int {\n  match c {\n    " + pattern + " => 1\n    _ => 0\n  }\n}"
		}
		insert := func(at base.Span, text string) *diag.FixIt { f := diag.Insert(at, text); return &f }
		endOf := func(sp base.Span) base.Span { return base.Span{File: sp.File, Lo: sp.Hi, Hi: sp.Hi} }
		checkRows(t, []diagRow{
			// An unknown field is most likely the one that was meant, so the fields the
			// pattern leaves out (no `..` here) are not reported as well.
			{name: "unknown field", src: fn("Point { zed: a }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{secondary(diag.NewError(diag.E(409), "no field `zed` on `Point`", diag.Label{Span: at("zed:", "zed"), Message: "unknown field"}),
						at("type Point { x", "Point"), "`Point` is defined here")}
				}},
			{name: "an unknown field does not stop the others", src: fn("Point { zed: a, x: true, y }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						secondary(diag.NewError(diag.E(409), "no field `zed` on `Point`", diag.Label{Span: at("zed:", "zed"), Message: "unknown field"}),
							at("type Point { x", "Point"), "`Point` is defined here"),
						fieldMismatch(at("x: true", "true"), "Int", "Bool", at("Point { x: Int", "x: Int")),
					}
				}},
			{name: "a field that is named is not a suggestion", src: fn("Point { x, xz, .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{secondary(diag.NewError(diag.E(409), "no field `xz` on `Point`", diag.Label{Span: at("xz,", "xz"), Message: "unknown field"}),
						at("type Point { x", "Point"), "`Point` is defined here")}
				}},
			{name: "an unknown field with a pattern of its own", src: fn("Point { zed: Rect(a), .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						secondary(diag.NewError(diag.E(409), "no field `zed` on `Point`", diag.Label{Span: at("zed:", "zed"), Message: "unknown field"}),
							at("type Point { x", "Point"), "`Point` is defined here"),
						arityDiag(at("Rect(a)", "Rect(a)"), "Rect", 2, 1, at("Rect(w: Float, h: Float)", "Rect"), "Rect(_, _)", at("Rect(a)", "Rect(a)")),
					}
				}},
			{name: "a pattern of its own in an unknown type", src: fn("Poin { x: Rect(a), y }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						unknownType(at("Poin {", "Poin"), "Poin", "Point"),
						arityDiag(at("Rect(a)", "Rect(a)"), "Rect", 2, 1, at("Rect(w: Float, h: Float)", "Rect"), "Rect(_, _)", at("Rect(a)", "Rect(a)")),
					}
				}},
			{name: "unknown pun", src: fn("Point { xx, .. }"), fixed: fn("Point { x: xx, .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{secondary(diag.NewError(diag.E(409), "no field `xx` on `Point`", diag.Label{Span: at("{ xx", "xx"), Message: "unknown field"}),
						at("type Point { x", "Point"), "`Point` is defined here").
						WithHelp("did you mean `x`?").WithFixIt(diag.Replace(at("{ xx", "xx"), "x: xx"))}
				}},
			{name: "one missing", src: fn("Point { x }"), fixed: fn("Point { x, .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{missingInPattern(at("Point { x }", "}"), "missing field `y` in pattern `Point { … }`", "`y` is not given",
						[]base.Span{at("type Point { x: Int, y: Int }", "y: Int")}, insert(endOf(at("Point { x }", "x")), ", .."))}
				}},
			{name: "two missing", src: cubeFn("Cube { x }"), fixed: cubeFn("Cube { x, .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{missingInPattern(at("Cube { x }", "}"), "missing fields `y` and `z` in pattern `Cube { … }`", "`y` and `z` are not given",
						[]base.Span{at("y: Int, z: Int }", "y: Int"), at("y: Int, z: Int }", "z: Int")}, insert(endOf(at("Cube { x }", "x")), ", .."))}
				}},
			{name: "two named", src: cubeFn("Cube { x, y }"), fixed: cubeFn("Cube { x, y, .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{missingInPattern(at("Cube { x, y }", "}"), "missing field `z` in pattern `Cube { … }`", "`z` is not given",
						[]base.Span{at("y: Int, z: Int }", "z: Int")}, insert(endOf(at("Cube { x, y }", "y")), ", .."))}
				}},
			{name: "a trailing comma", src: fn("Point { x, }"), fixed: fn("Point { x, .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{missingInPattern(at("Point { x, }", "}"), "missing field `y` in pattern `Point { … }`", "`y` is not given",
						[]base.Span{at("type Point { x: Int, y: Int }", "y: Int")}, insert(at("Point { x, }", "}"), ".. "))}
				}},
			{name: "no field named", src: fn("Point { }"), fixed: fn("Point { .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{missingInPattern(at("Point { }", "}"), "missing fields `x` and `y` in pattern `Point { … }`", "`x` and `y` are not given",
						[]base.Span{at("type Point { x: Int, y: Int }", "x: Int"), at("type Point { x: Int, y: Int }", "y: Int")}, insert(at("Point { }", "}"), ".. "))}
				}},
			{name: "over several lines", src: fn("Point {\n      x\n    }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{missingInPattern(at("      x\n    }", "}"), "missing field `y` in pattern `Point { … }`", "`y` is not given",
						[]base.Span{at("type Point { x: Int, y: Int }", "y: Int")}, nil)}
				}},
			{name: "a rest", src: fn("Point { x, .. }"), want: func(func(string, string) base.Span) []diag.Diagnostic { return nil }},
			{name: "every field", src: fn("Point { x, y: b }"), want: func(func(string, string) base.Span) []diag.Diagnostic { return nil }},
			{name: "an alias of a record", src: fn("P { x, y }"), want: func(func(string, string) base.Span) []diag.Diagnostic { return nil }},
			{name: "a field given twice", src: fn("Point { x: a, x: b, .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{diag.NewError(diag.E(411), "field `x` is given twice", diag.Label{Span: at("x: b", "x: b"), Message: "given again here"}).
						WithSecondary(diag.Label{Span: at("x: a", "x: a"), Message: "first given here"}).WithHelp("remove one of them")}
				}},
			{name: "a pun given twice is one error", src: fn("Point { x, x, y }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{twiceBindDiag("x", at(", x,", "x"), at("{ x,", "x"))}
				}},
			// A pattern takes a value apart itself, and matches fields and does not read
			// them, so its E0412 has its own label and note (the literal and the access
			// keep theirs): the name of a sum, of an alias of one and of a builtin type.
			{name: "a sum is not a record", src: fn("Shape { x }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{secondary(diag.NewError(diag.E(412), "`Shape` is not a record", diag.Label{Span: at("Shape { x }", "Shape"), Message: "has no fields to match"}),
						at("type Shape {", "Shape"), "`Shape` is defined here").WithNote("only a record has fields; the values of a sum type are matched by the names of its variants")}
				}},
			{name: "an alias of a sum is not a record", src: shapes + "type S = Shape\nfn f(s: Shape) -> Int {\n  match s {\n    S { .. } => 1\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{secondary(diag.NewError(diag.E(412), "`S` is not a record", diag.Label{Span: at("S { .. }", "S"), Message: "has no fields to match"}),
						at("type S =", "S"), "`S` is defined here").WithNote("only a record has fields; the values of a sum type are matched by the names of its variants")}
				}},
			{name: "a builtin type is not a record", src: fn("Int { x }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{diag.NewError(diag.E(412), "`Int` is not a record", diag.Label{Span: at("Int { x }", "Int"), Message: "has no fields to match"})}
				}},
			{name: "an unknown type", src: fn("Poin { x, y }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{unknownType(at("Poin {", "Poin"), "Poin", "Point")}
				}},
			{name: "a record that is not the value", src: cube + "fn f(c: Cube) -> Int {\n  match c {\n    Point { x: 0, y: true } => 1\n    _ => 0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{scrutineeMismatch(at("Point { x: 0, y: true }", "Point { x: 0, y: true }"), "Cube", "`Point`", at("h c {", "c"), "Cube")}
				}},
			{name: "a pun given again with a pattern", src: fn("Point { x, x: b, .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{diag.NewError(diag.E(411), "field `x` is given twice", diag.Label{Span: at("x: b", "x: b"), Message: "given again here"}).
						WithSecondary(diag.Label{Span: at("{ x,", "x"), Message: "first given here"}).WithHelp("remove one of them")}
				}},
			{name: "a pun that gives a field again", src: fn("Point { x: a, x, .. }"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{diag.NewError(diag.E(411), "field `x` is given twice", diag.Label{Span: at(", x,", "x"), Message: "given again here"}).
						WithSecondary(diag.Label{Span: at("x: a", "x: a"), Message: "first given here"}).WithHelp("remove one of them")}
				}},
		})
	})

	t.Run("a_pattern_of_another_shape_is_a_mismatch", func(t *testing.T) {
		// A tuple pattern of the wrong size or against a type that is not a tuple,
		// and a list pattern against anything (there is no list type yet), are one
		// E0401 on the pattern that says what it found in words, and nothing more:
		// the variables inside are bound and have no type to complain about.
		const src = shapes + "fn first(k: Int) -> Int { match k { z => z } }\nfn f(s: Shape, n: Int) -> Int {\n  let a = match (1, true) {\n    (a, b, c) => 1\n    _ => 2\n  }\n" +
			"  let b = match n {\n    (true, 0) => 1\n    _ => 3\n  }\n  let c = match s {\n    [true, ..t] => t\n    [] => 4\n    _ => 5\n  }\n  let d = match (1, true, 2) {\n    (a, b) => 1\n    _ => 2\n  }\n  a + b + c + d\n}"
		checkRows(t, []diagRow{{name: "shapes", src: src, want: func(at func(string, string) base.Span) []diag.Diagnostic {
			return []diag.Diagnostic{
				scrutineeMismatch(at("(a, b, c)", "(a, b, c)"), "(Int, Bool)", "a tuple pattern of 3 elements", at("(1, true)", "(1, true)"), "(Int, Bool)"),
				scrutineeMismatch(at("(true, 0)", "(true, 0)"), "Int", "a tuple pattern of 2 elements", at("match n {", "n"), "Int"),
				scrutineeMismatch(at("[true, ..t]", "[true, ..t]"), "Shape", "a list pattern", at("match s {", "s"), "Shape"),
				scrutineeMismatch(at("[] =>", "[]"), "Shape", "a list pattern", at("match s {", "s"), "Shape"),
				scrutineeMismatch(at("(a, b) =>", "(a, b)"), "(Int, Bool, Int)", "a tuple pattern of 2 elements", at("(1, true, 2)", "(1, true, 2)"), "(Int, Bool, Int)"),
			}
		}}})
		// The rest of a list has the error type, and what is inside it is read; each
		// pattern keeps the type of the value it was matched against.
		p := testsupport.Analyze(t, src)
		if got := localType(p, oneLocal(t, p, "t")); got != "{error}" {
			t.Errorf("the rest of a list pattern has type %s, want {error}", got)
		}
		for _, row := range []struct{ context, word, want string }{
			{"(a, b, c)", "(a, b, c)", "(Int, Bool)"}, {"(a, b) =>", "(a, b)", "(Int, Bool, Int)"}, {"[true, ..t]", "[true, ..t]", "Shape"},
			{"[] =>", "[]", "Shape"}, {"z =>", "z", "Int"},
		} {
			if got := patType(t, p, spanIn(t, p, src, row.context, row.word)); got != row.want {
				t.Errorf("the pattern %s has type %s, want %s", row.word, got, row.want)
			}
		}
		// A tuple pattern against a parameter that an earlier pattern made a tuple is
		// compared with that tuple, and not taken for a variable to bind.
		checkRows(t, []diagRow{{name: "a bound parameter", src: "fn f(x) -> Int {\n  match x {\n    (a, true) => a + 1\n    (c, d, e) => 2\n    _ => 3\n  }\n}",
			want: func(at func(string, string) base.Span) []diag.Diagnostic {
				return []diag.Diagnostic{scrutineeMismatch(at("(c, d, e)", "(c, d, e)"), "(Int, Bool)", "a tuple pattern of 3 elements", at("match x {", "x"), "(Int, Bool)")}
			}}})
		// A function type keeps its parameters in the same place as a tuple keeps its
		// elements, and a tuple pattern of that size is not a tuple: one E0401 that
		// writes the function type, and nothing from the passes after Check, which
		// have no tuple to take apart.
		checkRows(t, []diagRow{{name: "a function value", src: "fn add(a: Int, b: Int) -> Int { a + b }\nfn f() -> Int {\n  match add {\n    (x, y) => x + y\n  }\n}",
			want: func(at func(string, string) base.Span) []diag.Diagnostic {
				return []diag.Diagnostic{scrutineeMismatch(at("(x, y)", "(x, y)"), "(Int, Int) -> Int", "a tuple pattern of 2 elements", at("match add {", "add"), "(Int, Int) -> Int")}
			}}})
		checkRows(t, []diagRow{{name: "a pattern inside a tuple", src: shapes + "fn f(n: Int) -> Int {\n  match n {\n    (Rect(a), 0) => 1\n    _ => 2\n  }\n}",
			want: func(at func(string, string) base.Span) []diag.Diagnostic {
				return []diag.Diagnostic{
					scrutineeMismatch(at("(Rect(a), 0)", "(Rect(a), 0)"), "Int", "a tuple pattern of 2 elements", at("match n {", "n"), "Int"),
					arityDiag(at("Rect(a),", "Rect(a)"), "Rect", 2, 1, at("Rect(w: Float, h: Float)", "Rect"), "Rect(_, _)", at("Rect(a),", "Rect(a)")),
				}
			}}})
		checkRows(t, []diagRow{{name: "a pattern inside a list", src: shapes + "fn f(s: Shape) -> Int {\n  match s {\n    [Rect(a)] => 1\n    _ => 2\n  }\n}",
			want: func(at func(string, string) base.Span) []diag.Diagnostic {
				return []diag.Diagnostic{
					scrutineeMismatch(at("[Rect(a)]", "[Rect(a)]"), "Shape", "a list pattern", at("match s {", "s"), "Shape"),
					arityDiag(at("Rect(a)]", "Rect(a)"), "Rect", 2, 1, at("Rect(w: Float, h: Float)", "Rect"), "Rect(_, _)", at("Rect(a)]", "Rect(a)")),
				}
			}}})
	})

	t.Run("a_let_with_a_pattern_is_typed_like_an_arm", func(t *testing.T) {
		// A `let` with a pattern is typed as an arm is, the initializer being the
		// scrutinee (or the annotation, when there is one): the variables have the
		// types of their parts, a mismatch is an E0401 on the pattern, and the
		// statement has no variable of its own, so what LetLocal holds for it is not
		// written.
		const w = "type W { W(Int) }\n"
		checkRows(t, []diagRow{
			{name: "no annotation", src: "fn f() {\n  let (a, b) = 1\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{scrutineeMismatch(at("(a, b)", "(a, b)"), "Int", "a tuple pattern of 2 elements", at("= 1", "1"), "Int")}
				}},
			{name: "a value that is not an Int", src: "fn f() {\n  let t = (1, true, 2.5)\n  let (a, b) = t\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{scrutineeMismatch(at("(a, b)", "(a, b)"), "(Int, Bool, Float)", "a tuple pattern of 2 elements", at("= t", "t"), "(Int, Bool, Float)")}
				}},
			{name: "annotation", src: "fn f() {\n  let (a, b): Int = 1\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{secondary(mismatch(at("(a, b)", "(a, b)"), "expected `Int`, found a tuple pattern of 2 elements"),
						at(": Int", "Int"), "expected because of this annotation")}
				}},
			{name: "initializer against annotation", src: "fn f() {\n  let (a, b): (Int, Int) = (1, true)\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{secondary(mismatch(at("(1, true)", "(1, true)"), "expected `(Int, Int)`, found `(Int, Bool)`"),
						at("(Int, Int)", "(Int, Int)"), "expected because of this annotation")}
				}},
			{name: "a pattern that fits", src: w + "fn f(w: W, t: (Int, Bool)) {\n  let (a, b): (Int, Bool) = t\n  let W(n) = w\n  let _ = 1\n  let (c, _) = (true, 2.0)\n}",
				want: func(func(string, string) base.Span) []diag.Diagnostic { return nil }},
		})
		p := testsupport.Analyze(t, "fn f(t: (Int, Bool)) -> Int {\n  let (a, b) = t\n  a\n}")
		wantCodes(t, p)
		if got, want := localType(p, oneLocal(t, p, "a")), "Int"; got != want {
			t.Errorf("a has type %s, want %s", got, want)
		}
		if got, want := localType(p, oneLocal(t, p, "b")), "Bool"; got != want {
			t.Errorf("b has type %s, want %s", got, want)
		}
		m, rm, _ := userModule(p)
		for id, s := range m.Stmts.All() {
			if s.Kind == syntax.StmtLet && s.HasPat && rm.LetLocal[id] != 0 {
				t.Errorf("the let with a pattern has LetLocal %d, want the zero it was allocated with", rm.LetLocal[id])
			}
		}
		// Variable 0 is the first parameter of the prelude: a let with a pattern has
		// no variable and must not write its type there.
		typeOfFirstLocal := func(src string) string {
			q := testsupport.Analyze(t, src)
			return localType(q, 0)
		}
		if with, without := typeOfFirstLocal("fn f() { let (a, b) = (1.5, true) }"), typeOfFirstLocal("fn f() {}"); with != without {
			t.Errorf("the first variable has the type %s after a `let` with a pattern, and %s without it", with, without)
		}
	})

	t.Run("an_error_type_says_nothing_more", func(t *testing.T) {
		// What an earlier error left with no type is not reported again by the
		// patterns: a scrutinee that is an unknown name, a constructor, a record type
		// or a field that does not exist. The patterns are still walked, so their
		// variables are bound and the body that uses them says nothing.
		checkRows(t, []diagRow{
			{name: "unknown scrutinee", src: shapes + "fn f() -> Float {\n  match zzz {\n    Circle(r) => r\n    0 => 1.0\n    true => 2.0\n    (true, 0) => 3.0\n    [true, ..t] => 4.0\n    Point { x, y } => 5.0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{diag.NewError(diag.E(301), "unknown name `zzz`", diag.Label{Span: at("match zzz", "zzz"), Message: "not found in this scope"})}
				}},
			// The arguments of a constructor that does not exist are walked with no type to
			// give them (`0` and `true` are not compared with anything), and a mistake of
			// their own, such as the arity of `Rect(w)`, is still reported.
			{name: "unknown constructor", src: shapes + "fn f(s: Shape) -> Float {\n  match s {\n    Circl(r) => r\n    Squar(Rect(w), 0, true) => 1.0\n    _ => 0.0\n  }\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{
						unknownCtor(at("Circl(r)", "Circl"), "Circl", "Circle"),
						unknownCtor(at("Squar(", "Squar"), "Squar", ""),
						arityDiag(at("Rect(w)", "Rect(w)"), "Rect", 2, 1, at("Rect(w: Float, h: Float)", "Rect"), "Rect(_, _)", at("Rect(w)", "Rect(w)")),
					}
				}},
			{name: "a let with an unknown initializer", src: "fn f() -> Int {\n  let (a, b) = zzz\n  a + b\n}",
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{diag.NewError(diag.E(301), "unknown name `zzz`", diag.Label{Span: at("= zzz", "zzz"), Message: "not found in this scope"})}
				}},
		})
	})

	t.Run("large_patterns_are_checked_flat", func(t *testing.T) {
		// A tuple pattern and an or-pattern of 100 000 elements, a match of 100 001
		// arms and a list pattern of 100 000 elements are checked with no more depth
		// than one that is small, under a stack of 1 MiB that a pass that went down a
		// level for each element would overflow (a fatal error, which ends the test
		// binary): the tuple against a tuple of as many elements, the arms and the
		// alternatives against an Int, the list as the one E0401 it is.
		// An or-pattern whose alternatives each bind a name of their own is the last:
		// it is an E0703 for every name, and the sink keeps the first hundred. What
		// the row shows is that the answer comes at all, and in what time is for
		// ci/mutate to say (the version that looked at every name in every alternative
		// took a minute here, which its time limit ends).
		defer debug.SetMaxStack(debug.SetMaxStack(1 << 20))
		const n = 100_000
		var distinct strings.Builder
		for i := range n {
			if i > 0 {
				distinct.WriteString(" | ")
			}
			distinct.WriteString("a" + strconv.Itoa(i))
		}
		var or703 []string
		for range diag.MaxErrors {
			or703 = append(or703, "E0703")
		}
		tuple := "(" + listOf(n, "1", ", ") + ")"
		wild := "(" + listOf(n, "_", ", ") + ")"
		var arms strings.Builder
		for i := range n {
			arms.WriteString("    " + strconv.Itoa(i) + " => " + strconv.Itoa(i) + "\n")
		}
		for _, row := range []struct {
			name, src string
			want      []string
		}{
			{"tuple", "fn f() -> Int {\n  match " + tuple + " {\n    " + wild + " => 1\n  }\n}", nil},
			{"or-pattern", "fn f(x: Int) -> Int {\n  match x {\n    " + listOf(n, "0", " | ") + " => 1\n    _ => 2\n  }\n}", nil},
			{"arms", "fn f(x: Int) -> Int {\n  match x {\n" + arms.String() + "    _ => 0\n  }\n}", nil},
			{"list", "fn f(x: Int) -> Int {\n  match x {\n    [" + listOf(n, "_", ", ") + "] => 1\n    _ => 2\n  }\n}", []string{"E0401"}},
			{"or-pattern with a name in each alternative", "fn f(x: Int) -> Int {\n  match x {\n    " + distinct.String() + " => 1\n  }\n}", or703},
		} {
			p := testsupport.Analyze(t, row.src)
			if got := testsupport.ErrorCodes(p); !slices.Equal(got, row.want) {
				t.Errorf("%s: reports %v, want %v", row.name, got, row.want)
			}
		}
	})
}

// oneLocalAt returns the variable that the pattern or name word, the first one
// inside context, is bound to: the Local whose span is that of word.
func oneLocalAt(t *testing.T, p *testsupport.Program, src, context, word string) sema.LocalID {
	t.Helper()
	span := spanIn(t, p, src, context, word)
	for id, l := range p.Frontend.Resolved.Locals.All() {
		if l.Span == span {
			return id
		}
	}
	t.Fatalf("no variable is bound at %v", span)
	panic("unreachable")
}
