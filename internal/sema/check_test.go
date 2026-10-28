package sema_test

import (
	"cmp"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/sema"
	"github.com/aldezex/kilang/internal/syntax"
	"github.com/aldezex/kilang/internal/testsupport"
	"github.com/aldezex/kilang/internal/testsupport/within"
)

// The tests here run a source through testsupport.Analyze, which resolves the
// names and checks the types, and compare what the checker reported with
// diagnostics built by hand with the diag constructors, so that the code, the
// message, every label and its span, the notes, the help and the fix-its are
// all pinned. They reuse spanIn, paramLocal, letNamed and wantCodes
// from scope_test.go and resolve_test.go, which are in the same package.

// wantDiags fails the test unless p reported exactly the diagnostics in want,
// each equal to its partner field by field. Both lists are put in the order
// `ki check` shows them, by the start of the primary label, so that a test does
// not depend on the order in which the checker walks a function.
func wantDiags(t *testing.T, row string, p *testsupport.Program, want ...diag.Diagnostic) {
	t.Helper()
	byPosition := func(a, b diag.Diagnostic) int { return cmp.Compare(a.Primary.Span.Lo, b.Primary.Span.Lo) }
	got := slices.Clone(p.Session.Sink.All())
	slices.SortStableFunc(got, byPosition)
	want = slices.Clone(want)
	slices.SortStableFunc(want, byPosition)
	if len(got) != len(want) {
		t.Errorf("%s: %d diagnostics, want %d: %+v", row, len(got), len(want), got)
		return
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("%s: diagnostic %d is\n\t%+v\nwant\n\t%+v", row, i, got[i], want[i])
		}
	}
}

// mismatch is the E0401 whose primary label is at and says label.
func mismatch(at base.Span, label string) diag.Diagnostic {
	return diag.NewError(diag.E(401), "mismatched types", diag.Label{Span: at, Message: label})
}

// secondary adds to d the secondary label that says text at at.
func secondary(d diag.Diagnostic, at base.Span, text string) diag.Diagnostic {
	return d.WithSecondary(diag.Label{Span: at, Message: text})
}

// arityError is the E0402 for a call whose argument list is at: the function
// takes want arguments and got were supplied.
func arityError(at base.Span, want, got int) diag.Diagnostic {
	return diag.NewError(diag.E(402),
		"this function takes "+strconv.Itoa(want)+" argument(s) but "+strconv.Itoa(got)+" were supplied",
		diag.Label{Span: at, Message: "supplied here"})
}

// notCallable is the E0403 for a callee at that has the type typ.
func notCallable(at base.Span, typ string) diag.Diagnostic {
	return diag.NewError(diag.E(403), "cannot call a value of type `"+typ+"`", diag.Label{Span: at, Message: "not a function"})
}

// The help that a value left at the tail of a body that returns `()` gets: the
// first where returning the value would mend the mistake (Reason.CanReturn),
// the second where only binding it would. unify_internal_test.go, which is in
// another package, writes the same two texts out for its own rows: each test
// pins the words, so a change to them fails both.
const (
	unusedHelp     = "this value is unused: bind it with `let name = …`, or return it from the function"
	unusedBindHelp = "this value is unused: bind it with `let name = …`"
)

// exprType returns the type of the one expression of the user's file whose span
// is span, written as Show writes it. It fails the test if no expression, or
// more than one, has that span.
func exprType(t *testing.T, p *testsupport.Program, span base.Span) string {
	t.Helper()
	user := p.Frontend.User
	var found []syntax.ExprID
	for id, e := range p.Frontend.Modules[user].Exprs.All() {
		if e.Span == span {
			found = append(found, id)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d expressions have the span %v, want exactly 1", len(found), span)
	}
	typed := p.Frontend.Typed
	return typed.Types.Show(typed.Modules[user].ExprTypes[found[0]])
}

// bindEdit applies to src the first thing the help of d suggests, a binding
// written between backquotes after "bind it with": it puts that text, with the
// value in place of the `…`, where the primary label of d is, which is where
// the value is. The test that uses it checks that advice which names an edit
// names one that works.
func bindEdit(t *testing.T, src string, d diag.Diagnostic) string {
	t.Helper()
	_, after, ok := strings.Cut(d.Help, "bind it with `")
	if !ok {
		t.Fatalf("the help %q does not say what to bind the value with", d.Help)
	}
	template, _, ok := strings.Cut(after, "`")
	if !ok || !strings.Contains(template, "…") {
		t.Fatalf("the help %q has no text with a … in it after \"bind it with\"", d.Help)
	}
	at := d.Primary.Span
	return src[:at.Lo] + strings.Replace(template, "…", src[at.Lo:at.Hi], 1) + src[at.Hi:]
}

func TestCheck(t *testing.T) {
	t.Run("fib_types", func(t *testing.T) {
		// noErrorTypes fails the test for every expression of the user's file that
		// the checker left with the error type, which is what the table holds for
		// an expression nobody has typed. The programs it is used on are correct
		// and have no expression that the parser dropped, so each one has a type
		// of its own: a block, an `if`, a call or a leaf alike.
		noErrorTypes := func(what string, p *testsupport.Program) {
			t.Helper()
			user, typed := p.Frontend.User, p.Frontend.Typed
			for id, e := range p.Frontend.Modules[user].Exprs.All() {
				if got := typed.Types.Show(typed.Modules[user].ExprTypes[id]); got == "{error}" {
					t.Errorf("%s: the expression at %v has the type {error}, want its own type", what, e.Span)
				}
			}
		}
		const src = "fn fib(n: Int) -> Int {\n  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }\n}\nfn main() { print(fib(20)) }\n"
		p := testsupport.Analyze(t, src)
		wantDiags(t, "fib", p)
		noErrorTypes("fib", p)
		for _, row := range []struct{ fn, want string }{
			{"fib", "(Int) -> Int"},
			{"main", "() -> ()"},
		} {
			if got := testsupport.DefType(t, p, row.fn); got != row.want {
				t.Errorf("type of %s is %q, want %q", row.fn, got, row.want)
			}
		}

		// Every expression has its type, and the checker records it.
		for _, row := range []struct{ context, word, want string }{
			{"if n < 2", "n < 2", "Bool"},
			{"{ n }", "n", "Int"},
			{"if n < 2 { n } else { fib(n - 1) + fib(n - 2) }", "if n < 2 { n } else { fib(n - 1) + fib(n - 2) }", "Int"},
			{"fib(n - 1) +", "fib", "(Int) -> Int"},
			{"fib(n - 1) +", "fib(n - 1)", "Int"},
			{"fib(n - 1) + fib(n - 2)", "fib(n - 1) + fib(n - 2)", "Int"},
			{"n - 1", "1", "Int"},
			{"fib(20)", "20", "Int"},
			{"fib(20)", "fib(20)", "Int"},
			{"print(fib", "print", "(Int) -> ()"},
			{"print(fib(20))", "print(fib(20))", "()"},
		} {
			if got := exprType(t, p, spanIn(t, p, src, row.context, row.word)); got != row.want {
				t.Errorf("type of %q in %q is %q, want %q", row.word, row.context, got, row.want)
			}
		}

		// Every variable has its type: the parameters and the lets.
		typed := p.Frontend.Typed
		if got := typed.Types.Show(typed.LocalTypes.Get(paramLocal(t, p, "fib", 0))); got != "Int" {
			t.Errorf("type of the parameter n is %q, want Int", got)
		}
		if got, want := typed.LocalTypes.Len(), p.Frontend.Resolved.Locals.Len(); got != want {
			t.Errorf("%d types of variables, want one for each of the %d variables", got, want)
		}
		const lets = "fn main() {\n  let a = 1\n  let b = true\n  let c = ()\n  let d: Int = 2\n  let e = fib\n}\nfn fib(n: Int) -> Int { n }\n"
		p = testsupport.Analyze(t, lets)
		wantDiags(t, "lets", p)
		noErrorTypes("lets", p)
		typed = p.Frontend.Typed
		for _, row := range []struct{ name, want string }{{"a", "Int"}, {"b", "Bool"}, {"c", "()"}, {"d", "Int"}, {"e", "(Int) -> Int"}} {
			if got := typed.Types.Show(typed.LocalTypes.Get(letNamed(t, p, row.name, 0))); got != row.want {
				t.Errorf("type of the let %s is %q, want %q", row.name, got, row.want)
			}
		}
		// An expression that no item reaches keeps the error type: the parser
		// drops the `1` when it gives up on the call, and nothing checks it.
		const dropped = "fn main() { print(1 }"
		p = testsupport.Analyze(t, dropped)
		wantCodes(t, p, "E0201")
		if got := exprType(t, p, spanIn(t, p, dropped, "(1 }", "1")); got != "{error}" {
			t.Errorf("type of the expression that the parser dropped is %q, want {error}", got)
		}
		// The signatures come out of the annotations.
		p = testsupport.Analyze(t, "fn a(x: Int, y: Bool) -> Bool { y }\nfn b() {}\nfn c(u: ()) -> () { u }\nfn d() -> Int { 1 }\n")
		wantDiags(t, "signatures", p)
		noErrorTypes("signatures", p)
		for _, row := range []struct{ fn, want string }{
			{"a", "(Int, Bool) -> Bool"}, {"b", "() -> ()"}, {"c", "(()) -> ()"}, {"d", "() -> Int"},
		} {
			if got := testsupport.DefType(t, p, row.fn); got != row.want {
				t.Errorf("type of %s is %q, want %q", row.fn, got, row.want)
			}
		}
	})

	t.Run("unannotated_param_inferred", func(t *testing.T) {
		// `fn double(x) -> Int { x * 2 }` needs its `-> Int`: a function without
		// `->` returns `()`, and `x * 2` against `()` would be an E0401. All the
		// functions of a program are one group that is not generalized, so what
		// one function's body learns about a parameter, and what a call learns
		// about it, is what every use of it sees.
		for _, row := range []struct{ name, src, fn, want string }{
			{"body_binds", "fn double(x) -> Int { x * 2 }", "double", "(Int) -> Int"},
			{"tail_against_return", "fn id(x) -> Int { x }", "id", "(Int) -> Int"},
			{"bool_operands", "fn both(a, b) -> Bool { a && b }", "both", "(Bool, Bool) -> Bool"},
			{"condition", "fn pick(c) -> Int { if c { 1 } else { 2 } }", "pick", "(Bool) -> Int"},
			{"unused_stays_a_variable", "fn unused(x) {}", "unused", "(a) -> ()"},
			{"two_variables", "fn two(x, y) {}", "two", "(a, b) -> ()"},
			{"argument_binds", "fn k(x) {}\nfn main() { k(true) }", "k", "(Bool) -> ()"},
			{"call_before_definition", "fn main() { later(1) }\nfn later(x) {}", "later", "(Int) -> ()"},
			{"call_after_definition", "fn earlier(x) {}\nfn main() { earlier(1) }", "earlier", "(Int) -> ()"},
			{"let_unifies", "fn m(x) { let y: Bool = x }", "m", "(Bool) -> ()"},
			{"called_parameter", "fn apply(g, x: Int) -> Int { g(x) }", "apply", "((Int) -> Int, Int) -> Int"},
			{"called_parameter_arguments", "fn app(g) -> Int { g(1, true) }", "app", "((Int, Bool) -> Int) -> Int"},
			{"called_parameter_result", "fn pick(g) -> Bool { g(1) }", "pick", "((Int) -> Bool) -> Bool"},
			{"called_parameter_no_arguments", "fn run(g) { g() }", "run", "(() -> ()) -> ()"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p)
			if got := testsupport.DefType(t, p, row.fn); got != row.want {
				t.Errorf("%s: type of %s is %q, want %q", row.name, row.fn, got, row.want)
			}
		}
		// Each parameter is called with the one before it, twice: p1 becomes a
		// function of (p0, p0), p2 a function of (p1, p1), and so on, so the type
		// of p40 holds the type below it twice at every level, 2^40 paths in 40
		// nodes. It is a valid program, and checking it must not walk the paths.
		// within fails the test if the check takes ten seconds, in place of the
		// ten minutes that go test waits; it does not count the steps. The same
		// program with one mistake that writes the type of p40 in its message
		// is the next two rows: Show cuts the text, so the message ends too (see
		// TypeTable.Show).
		{
			const n = 40
			params := make([]string, n+1)
			var calls strings.Builder
			for i := range params {
				params[i] = "p" + strconv.Itoa(i)
				if i > 0 {
					calls.WriteString("  " + params[i] + "(" + params[i-1] + ", " + params[i-1] + ")\n")
				}
			}
			header := "fn f(" + strings.Join(params, ", ") + ") {\n" + calls.String()
			analyze := func(what, src string) *testsupport.Program {
				var p *testsupport.Program
				within.Run(t, what, func() { p = testsupport.Analyze(t, src) })
				return p
			}
			p := analyze("checking the program with 41 parameters", header+"}")
			wantDiags(t, "calls that share their types", p)

			// A mismatch whose found type is the one of p40: the label has the
			// text of that type, cut at the limit and ending in the mark.
			src := header + "  let z: Int = p40\n}"
			p = analyze("checking the program with 41 parameters and a mismatch", src)
			typed := p.Frontend.Typed
			shown := typed.Types.Show(typed.LocalTypes.Get(paramLocal(t, p, "f", n)))
			if !strings.HasSuffix(shown, "…") || len(shown) > sema.MaxTypeText+len("…") {
				t.Fatalf("the type of p40 is written as %d bytes ending in %q, want at most %d ending in …", len(shown), shown[max(0, len(shown)-4):], sema.MaxTypeText+len("…"))
			}
			wantDiags(t, "mismatch with a type of 40 levels", p, secondary(
				mismatch(spanIn(t, p, src, "= p40", "p40"), "expected `Int`, found `"+shown+"`"),
				spanIn(t, p, src, ": Int", "Int"), "expected because of this annotation"))

			// An arity error whose note writes the types of the 41 parameters:
			// each is cut on its own, so the note is long and not endless. The
			// first parameter is a lone variable and the second a function of two
			// of them, so the note starts with them and the deep ones come after.
			src = header + "}\nfn main() { f(1) }"
			p = analyze("checking the program with 41 parameters and a call with a wrong count", src)
			all := p.Session.Sink.All()
			if len(all) != 1 {
				t.Fatalf("%d diagnostics, want exactly 1: %+v", len(all), all)
			}
			d := all[0]
			if d.Code != diag.E(402) || len(d.Notes) != 1 {
				t.Fatalf("diagnostic %+v, want an E0402 with one note", d)
			}
			note := d.Notes[0]
			const prefix = "`f` takes `(a, (a, a) -> b, ((a, a) -> b, (a, a) -> b) -> c, "
			if !strings.HasPrefix(note, prefix) || !strings.HasSuffix(note, ")`") {
				t.Errorf("the note starts %q and ends %q, want it to start %q and end `)`", note[:min(len(note), len(prefix))], note[max(0, len(note)-8):], prefix)
			}
			if cuts, most := strings.Count(note, "…"), (n+1)*(sema.MaxTypeText+len("…")+len(", ")); cuts < 30 || len(note) > most {
				t.Errorf("the note has %d bytes and %d cut types, want at most %d bytes and at least 30 cut types", len(note), cuts, most)
			}
		}
		// The parameter's variable is the type of its local and of its uses.
		const src = "fn double(x) -> Int { x * 2 }"
		p := testsupport.Analyze(t, src)
		typed := p.Frontend.Typed
		if got := typed.Types.Show(typed.LocalTypes.Get(paramLocal(t, p, "double", 0))); got != "Int" {
			t.Errorf("type of the parameter x is %q, want Int", got)
		}
		if got := exprType(t, p, spanIn(t, p, src, "{ x *", "x")); got != "Int" {
			t.Errorf("type of the use of x is %q, want Int", got)
		}

		// A variable says where it was born and why, for the notes that explain a
		// type error later: the one of a parameter without an annotation is the
		// parameter, and the one of the result of calling a parameter is the call.
		{
			const src = "fn unused(x) {}\nfn apply(g) { g(1) }"
			p := testsupport.Analyze(t, src)
			typed := p.Frontend.Typed
			varOf := func(id sema.TypeID) sema.TypeVarID {
				n := typed.Types.Node(id)
				if n.Kind != sema.TypeVariable {
					t.Fatalf("the type %s is not a variable", typed.Types.Show(id))
				}
				return n.Var
			}
			x := typed.LocalTypes.Get(paramLocal(t, p, "unused", 0))
			if got, want := sema.VarOrigin(typed.Types, varOf(x)), (sema.Origin{Span: spanIn(t, p, src, "(x)", "x"), Note: "parameter `x`"}); got != want {
				t.Errorf("origin of the variable of the parameter x is %+v, want %+v", got, want)
			}
			// g became a function of (Int), whose result is the variable that the
			// call made.
			g := typed.Types.Node(typed.Types.Resolve(typed.LocalTypes.Get(paramLocal(t, p, "apply", 0))))
			if g.Kind != sema.TypeFun {
				t.Fatalf("the parameter g has the type %s, want a function", typed.Types.Show(typed.LocalTypes.Get(paramLocal(t, p, "apply", 0))))
			}
			if got, want := sema.VarOrigin(typed.Types, varOf(g.Ret)), (sema.Origin{Span: spanIn(t, p, src, "{ g(1) }", "g(1)"), Note: "result of this call"}); got != want {
				t.Errorf("origin of the variable of the result of g(1) is %+v, want %+v", got, want)
			}
		}
	})

	t.Run("fn_without_arrow_returns_unit", func(t *testing.T) {
		// A function with no `->`, and then the same mistake against a return
		// type that is written: the secondary label sits on the name when there
		// is no `->` and on the type when there is, and the help is only for `()`.
		{
			const src = "fn f() { 1 }"
			p := testsupport.Analyze(t, src)
			want := secondary(mismatch(spanIn(t, p, src, "{ 1 }", "1"), "expected `()`, found `Int`"),
				spanIn(t, p, src, " f(", "f"), "expected because of the return type").WithHelp(unusedHelp)
			wantDiags(t, "no arrow", p, want)
		}
		{
			const src = "fn f() -> Int { true }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Int", p, secondary(mismatch(spanIn(t, p, src, "{ true }", "true"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, "-> Int", "Int"), "expected because of the return type"))
		}
		{
			const src = "fn f() -> () { 1 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "explicit unit", p, secondary(mismatch(spanIn(t, p, src, "{ 1 }", "1"), "expected `()`, found `Int`"),
				spanIn(t, p, src, "-> ()", "()"), "expected because of the return type").WithHelp(unusedHelp))
		}
		{
			// A body with nothing at the tail is `()`, and the block is where it
			// is found.
			const src = "fn f() -> Int { }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "empty body", p, secondary(mismatch(spanIn(t, p, src, "{ }", "{ }"), "expected `Int`, found `()`"),
				spanIn(t, p, src, "-> Int", "Int"), "expected because of the return type"))
		}
		{
			// A statement that is not the tail is never compared with anything.
			const src = "fn f() { 1\n 2 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "statement before the tail", p, secondary(mismatch(spanIn(t, p, src, " 2 }", "2"), "expected `()`, found `Int`"),
				spanIn(t, p, src, " f(", "f"), "expected because of the return type").WithHelp(unusedHelp))
		}
		{
			const src = "fn f() { }\nfn g() -> () { }\nfn h() { () }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "unit bodies", p)
		}

		// The help is advice, and advice has to work. Each source below has the
		// one E0401 with the help the row names. The first thing the help says,
		// to bind the value with `let name = …`, is applied to the source by
		// bindEdit, and the result must check without a diagnostic. Where the
		// help also says to return the value, the row has the program that does
		// it, written by hand: the function gets the type of the value. Where it
		// does not, returning would not do: the `if` has no `else`, so the value
		// is the end of a branch and not what the function returns.
		for _, row := range []struct {
			name, src string
			help      string
			returned  string // the program that returns the value; "" if the help does not offer it
		}{
			{"no arrow", "fn f() { 1 }", unusedHelp, "fn f() -> Int { 1 }"},
			{"explicit unit", "fn f() -> () { 1 }", unusedHelp, "fn f() -> Int { 1 }"},
			{"a call", "fn fib(n: Int) -> Int { n }\nfn g() { fib(10) }", unusedHelp, "fn fib(n: Int) -> Int { n }\nfn g() -> Int { fib(10) }"},
			{"a block", "fn f() { { 1 } }", unusedHelp, "fn f() -> Int { { 1 } }"},
			{"an if with an else", "fn f() { if true { 1 } else { 2 } }", unusedHelp, "fn f() -> Int { if true { 1 } else { 2 } }"},
			{"an if with no else", "fn f() { if true { 1 } }", unusedBindHelp, ""},
		} {
			p := testsupport.Analyze(t, row.src)
			all := p.Session.Sink.All()
			if len(all) != 1 {
				t.Errorf("%s: %d diagnostics, want exactly 1: %+v", row.name, len(all), all)
				continue
			}
			d := all[0]
			if d.Code != diag.E(401) || d.Help != row.help || len(d.Fixits) != 0 {
				t.Errorf("%s: diagnostic %+v, want an E0401 with the help %q and no fix-it", row.name, d, row.help)
				continue
			}
			bound := bindEdit(t, row.src, d)
			if got := testsupport.Analyze(t, bound).Session.Sink.All(); len(got) != 0 {
				t.Errorf("%s: after binding the value the program is %q, which reports %+v, want nothing", row.name, bound, got)
			}
			if row.returned != "" {
				if got := testsupport.Analyze(t, row.returned).Session.Sink.All(); len(got) != 0 {
					t.Errorf("%s: after returning the value the program is %q, which reports %+v, want nothing", row.name, row.returned, got)
				}
			}
		}
	})

	t.Run("call_arity_E0402", func(t *testing.T) {
		const defF = "fn f(a: Int) {}\n"
		{
			const src = defF + "fn main() { f(1, 2) }"
			p := testsupport.Analyze(t, src)
			want := secondary(arityError(spanIn(t, p, src, "f(1, 2)", "(1, 2)"), 1, 2),
				spanIn(t, p, src, " f(a", "f"), "`f` is declared here").WithNote("`f` takes `(Int)`")
			wantDiags(t, "one parameter, two arguments", p, want)
		}
		{
			const src = "fn g(a: Int, b: Bool) {}\nfn main() { g(1) }"
			p := testsupport.Analyze(t, src)
			want := secondary(arityError(spanIn(t, p, src, "g(1)", "(1)"), 2, 1),
				spanIn(t, p, src, " g(a", "g"), "`g` is declared here").WithNote("`g` takes `(Int, Bool)`")
			wantDiags(t, "two parameters, one argument", p, want)
		}
		{
			// The list starts at the `(`, whatever lies before it.
			const src = defF + "fn main() { f (1, 2) }"
			p := testsupport.Analyze(t, src)
			want := secondary(arityError(spanIn(t, p, src, "f (1, 2)", "(1, 2)"), 1, 2),
				spanIn(t, p, src, " f(a", "f"), "`f` is declared here").WithNote("`f` takes `(Int)`")
			wantDiags(t, "blank before the parenthesis", p, want)
		}
		{
			const src = defF + "fn main() { (f)(1, 2) }"
			p := testsupport.Analyze(t, src)
			want := secondary(arityError(spanIn(t, p, src, "(f)(1, 2)", "(1, 2)"), 1, 2),
				spanIn(t, p, src, " f(a", "f"), "`f` is declared here").WithNote("`f` takes `(Int)`")
			wantDiags(t, "callee in parentheses", p, want)
		}
		{
			// The prelude's file is not the user's: a secondary label there
			// could not be drawn under the primary one, so the note is all.
			const src = "fn main() { print(1, 2) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "prelude function", p, arityError(spanIn(t, p, src, "print(1, 2)", "(1, 2)"), 1, 2).
				WithNote("`print` takes `(Int)`"))
		}
		{
			// The arguments are not compared with the parameters when there are
			// too many or too few, but they are still checked on their own.
			const src = defF + "fn main() { f(true, 2 + false) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "arguments still checked", p,
				secondary(arityError(spanIn(t, p, src, "f(true, 2 + false)", "(true, 2 + false)"), 1, 2),
					spanIn(t, p, src, " f(a", "f"), "`f` is declared here").WithNote("`f` takes `(Int)`"),
				secondary(mismatch(spanIn(t, p, src, "+ false", "false"), "expected `Int`, found `Bool`"),
					spanIn(t, p, src, " + ", "+"), "`+` expects `Int` operands"))
		}
		{
			// The call still has the function's result type.
			const src = "fn f(a: Int) -> Int { a }\nfn main() { let x: Bool = f(1, 2) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "result type", p,
				secondary(arityError(spanIn(t, p, src, "f(1, 2)", "(1, 2)"), 1, 2),
					spanIn(t, p, src, " f(a", "f"), "`f` is declared here").WithNote("`f` takes `(Int)`"),
				secondary(mismatch(spanIn(t, p, src, "= f(1, 2)", "f(1, 2)"), "expected `Bool`, found `Int`"),
					spanIn(t, p, src, ": Bool", "Bool"), "expected because of this annotation"))
		}
		{
			// A called local has no definition to point at, but it has a name.
			const src = defF + "fn main() {\n  let g = f\n  g(1, 2)\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "local callee", p, arityError(spanIn(t, p, src, "g(1, 2)", "(1, 2)"), 1, 2).
				WithNote("`g` takes `(Int)`"))
		}
		{
			// A callee that is not a name has no name for the note, and no
			// definition for the secondary label.
			const src = defF + "fn main() { (if true { f } else { f })(1, 2) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "callee that is not a name", p, arityError(spanIn(t, p, src, "})(1, 2)", "(1, 2)"), 1, 2))
		}
		{
			// Parameters that are not known yet are written with names that are
			// different from one another.
			const src = "fn f(x, y) {}\nfn main() { f(1) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "unknown parameter types", p, secondary(arityError(spanIn(t, p, src, "f(1)", "(1)"), 2, 1),
				spanIn(t, p, src, " f(x", "f"), "`f` is declared here").WithNote("`f` takes `(a, b)`"))
		}
		{
			// A parameter without an annotation that an earlier call already
			// made a function: the next call finds a function and counts, so it
			// is E0402 and not a mismatch of two function types.
			const src = "fn h(g) {\n  g(1)\n  g(1, 2)\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "parameter called twice", p, arityError(spanIn(t, p, src, "g(1, 2)", "(1, 2)"), 1, 2).
				WithNote("`g` takes `(Int)`"))
		}
		{
			// The same when it was the call of the function that made the
			// parameter a function: main comes first, so h(f) is checked before
			// the body of h and the answer does not depend on the order of the
			// two bodies.
			const src = "fn f(a: Int) {}\nfn main() { h(f) }\nfn h(g) {\n  g(1, 2)\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "parameter bound by a call of its function", p, arityError(spanIn(t, p, src, "g(1, 2)", "(1, 2)"), 1, 2).
				WithNote("`g` takes `(Int)`"))
		}
		{
			const src = "fn f(a: Int) {}\nfn main() { f(1) }\n"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "right number of arguments", p)
		}
	})

	t.Run("call_arity_zero_args_E0402", func(t *testing.T) {
		for _, row := range []struct{ name, call, list string }{
			{"no blank", "f()", "()"},
			{"blank inside", "f( )", "( )"},
			{"blank before", "f ()", "()"},
		} {
			src := "fn f(a: Int) {}\nfn main() { " + row.call + " }"
			p := testsupport.Analyze(t, src)
			want := secondary(arityError(spanIn(t, p, src, row.call, row.list), 1, 0),
				spanIn(t, p, src, " f(a", "f"), "`f` is declared here").WithNote("`f` takes `(Int)`")
			wantDiags(t, row.name, p, want)
		}
		{
			// The other way: a function with no parameters, called with one.
			const src = "fn f() {}\nfn main() { f(1) }"
			p := testsupport.Analyze(t, src)
			want := secondary(arityError(spanIn(t, p, src, "f(1)", "(1)"), 0, 1),
				spanIn(t, p, src, " f()", "f"), "`f` is declared here").WithNote("`f` takes `()`")
			wantDiags(t, "no parameters", p, want)
		}
	})

	t.Run("call_non_function_E0403", func(t *testing.T) {
		{
			const src = "fn main() { let x = 1\n x(2) }"
			p := testsupport.Analyze(t, src)
			want := secondary(notCallable(spanIn(t, p, src, " x(2)", "x"), "Int"),
				spanIn(t, p, src, "let x", "x"), "`x` is bound here")
			wantDiags(t, "let", p, want)
		}
		{
			const src = "fn f(x: Bool) { x() }"
			p := testsupport.Analyze(t, src)
			want := secondary(notCallable(spanIn(t, p, src, "{ x()", "x"), "Bool"),
				spanIn(t, p, src, "(x:", "x"), "`x` is bound here")
			wantDiags(t, "parameter", p, want)
		}
		for _, row := range []struct{ name, src, context, callee, typ string }{
			{"literal", "fn main() { 1(2) }", "{ 1(", "1", "Int"},
			{"unit", "fn main() { ()(1) }", "{ ()(", "()", "()"},
			{"result of a call", "fn f() -> Int { 1 }\nfn main() { f()(2) }", "{ f()(", "f()", "Int"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p, notCallable(spanIn(t, p, row.src, row.context, row.callee), row.typ))
		}
		{
			// The arguments are still checked, and the call has no type, so
			// nothing is said about the place where its value is used.
			const src = "fn main() { 1(2 + true) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "arguments still checked", p,
				notCallable(spanIn(t, p, src, "{ 1(", "1"), "Int"),
				secondary(mismatch(spanIn(t, p, src, "+ true", "true"), "expected `Int`, found `Bool`"),
					spanIn(t, p, src, " + ", "+"), "`+` expects `Int` operands"))
			const used = "fn main() {\n  let x = 1\n  let y: Bool = x(2)\n}"
			p = testsupport.Analyze(t, used)
			wantDiags(t, "result is the error type", p, secondary(notCallable(spanIn(t, p, used, "= x(2)", "x"), "Int"),
				spanIn(t, p, used, "let x", "x"), "`x` is bound here"))
		}
		{
			// A callee that already gave an error is not reported again, and
			// its arguments are still checked on their own.
			const src = "fn main() { nope(2 + true) }"
			p := testsupport.Analyze(t, src)
			if got := testsupport.ErrorCodes(p); !slices.Equal(got, []string{"E0301", "E0401"}) {
				t.Errorf("unknown callee: error codes %v, want [E0301 E0401]; diagnostics %+v", got, p.Session.Sink.All())
			}
			const used = "fn main() { let b: Bool = nope(1) }"
			p = testsupport.Analyze(t, used)
			wantCodes(t, p, "E0301")
		}
		{
			// A function that is called is never E0403, and neither is an
			// unannotated parameter that is called (its type is not known yet).
			p := testsupport.Analyze(t, "fn f(a: Int) {}\nfn g(h) { h(1) }\nfn main() { f(1) }")
			wantDiags(t, "calls that are fine", p)
		}
	})

	t.Run("if_condition_must_be_bool", func(t *testing.T) {
		{
			const src = "fn main() { if 1 { } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Int", p, mismatch(spanIn(t, p, src, "if 1", "1"), "expected `Bool`, found `Int`").
				WithNote("an `if` condition must be `Bool`"))
		}
		{
			const src = "fn f() {}\nfn main() { if f { } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "function", p, mismatch(spanIn(t, p, src, " f {", "f"), "expected `Bool`, found `() -> ()`").
				WithNote("an `if` condition must be `Bool`"))
		}
		{
			const src = "fn main() { if true { } else if 1 { } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "else if", p, mismatch(spanIn(t, p, src, "if 1", "1"), "expected `Bool`, found `Int`").
				WithNote("an `if` condition must be `Bool`"))
		}
		{
			// A condition that is a block hands the expectation to its tail: the
			// mistake is the 1, not the braces around it.
			const src = "fn main() { if { 1 } { } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "block condition", p, mismatch(spanIn(t, p, src, "{ 1 }", "1"), "expected `Bool`, found `Int`").
				WithNote("an `if` condition must be `Bool`"))
		}
		{
			// An `if` that is not at the tail of a block, nor an else branch, has
			// nothing expected of it: it is inferred, and its condition is checked
			// all the same. Two ways in: the initializer of a `let` with no
			// annotation, and a statement that something follows.
			const src = "fn main() { let x = if 1 { 2 } else { 3 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "inferred, in a let", p, mismatch(spanIn(t, p, src, "if 1", "1"), "expected `Bool`, found `Int`").
				WithNote("an `if` condition must be `Bool`"))
		}
		{
			const src = "fn main() {\n  if 1 { }\n  print(1)\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "inferred, in a statement", p, mismatch(spanIn(t, p, src, "if 1", "1"), "expected `Bool`, found `Int`").
				WithNote("an `if` condition must be `Bool`"))
		}
		{
			const src = "fn main() { if 1 < 2 { } else { } }\nfn f(b: Bool) { if b && !b { } }\nfn g(x) { if x { } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "conditions that are fine", p)
			if got := testsupport.DefType(t, p, "g"); got != "(Bool) -> ()" {
				t.Errorf("type of g is %q, want (Bool) -> (): the condition is where x is found to be a Bool", got)
			}
		}
	})

	t.Run("if_without_else_is_unit", func(t *testing.T) {
		{
			// One error, on the whole `if`, and none on the 1.
			const src = "fn main() { let x: Int = if true { 1 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "let annotation", p, secondary(
				mismatch(spanIn(t, p, src, "= if true { 1 }", "if true { 1 }"), "expected `Int`, found `()`"),
				spanIn(t, p, src, ": Int", "Int"), "expected because of this annotation"))
		}
		{
			// When `()` is what is expected, the branch is checked against it
			// with the reason of the place, so the 1 is the mistake.
			const src = "fn main() { let x: () = if true { 1 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "unit annotation", p, secondary(
				mismatch(spanIn(t, p, src, "{ 1 }", "1"), "expected `()`, found `Int`"),
				spanIn(t, p, src, ": ()", "()"), "expected because of this annotation"))
		}
		{
			const src = "fn main() { if true { 1 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "tail of the body", p, secondary(
				mismatch(spanIn(t, p, src, "{ 1 }", "1"), "expected `()`, found `Int`"),
				spanIn(t, p, src, " main(", "main"), "expected because of the return type").WithHelp(unusedBindHelp))
		}
		{
			// The tail of a function that returns Int: one error, on the if.
			const src = "fn f() -> Int { if true { 1 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "tail of an Int body", p, secondary(
				mismatch(spanIn(t, p, src, "{ if true { 1 } }", "if true { 1 }"), "expected `Int`, found `()`"),
				spanIn(t, p, src, "-> Int", "Int"), "expected because of the return type"))
		}
		{
			// Where nothing is expected, the branch is still `()`, against a
			// reason that says why: the missing `else` is the other branch.
			const src = "fn main() {\n  if true { 1 }\n  print(1)\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "statement", p, secondary(
				mismatch(spanIn(t, p, src, "{ 1 }", "1"), "expected `()`, found `Int`"),
				spanIn(t, p, src, "if true", "if"), "the other branch has type `()`"))
		}
		{
			const src = "fn main() {\n  let x = if true { 1 }\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "unannotated let", p, secondary(
				mismatch(spanIn(t, p, src, "{ 1 }", "1"), "expected `()`, found `Int`"),
				spanIn(t, p, src, "if true", "if"), "the other branch has type `()`"))
		}
		{
			// When the `if` already fails against what is expected, the branch is
			// still inferred, for the mistakes inside it: the 1 + true is a
			// second mistake, and the expressions of the branch keep their types
			// (the 1 is an Int, not the error type that an unvisited one has).
			const src = "fn main() { let x: Int = if true { 1 + true } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "mistake inside the branch", p,
				secondary(mismatch(spanIn(t, p, src, "= if true { 1 + true }", "if true { 1 + true }"), "expected `Int`, found `()`"),
					spanIn(t, p, src, ": Int", "Int"), "expected because of this annotation"),
				secondary(mismatch(spanIn(t, p, src, "+ true }", "true"), "expected `Int`, found `Bool`"),
					spanIn(t, p, src, " + ", "+"), "`+` expects `Int` operands"))
			if got := exprType(t, p, spanIn(t, p, src, "{ 1 +", "1")); got != "Int" {
				t.Errorf("type of the 1 inside the branch is %q, want Int", got)
			}
		}
		{
			const src = "fn main() { if true { print(1) } }\nfn f() { let x: () = if true { } }\nfn g() { if true { } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "branches that are fine", p)
		}
	})

	t.Run("operand_type_mismatch", func(t *testing.T) {
		{
			// `true && 1`.
			const src = "fn main() { let b = true && 1 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "true && 1", p, secondary(mismatch(spanIn(t, p, src, "&& 1", "1"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, " && ", "&&"), "`&&` expects `Bool` operands"))
		}

		// Every binary operator wants operands of one type and gives a result of
		// another, and each of them is told apart from the others by its row.
		for _, g := range []struct {
			ops                  []string
			operand, result      string // the type of the operands and of the result
			good, bad            string // a literal of the operand type and one that is not
			badType, wrongResult string // the type of bad, and a type that the result is not
		}{
			{[]string{"+", "-", "*", "/", "%"}, "Int", "Int", "1", "true", "Bool", "Bool"},
			{[]string{"<", "<=", ">", ">="}, "Int", "Bool", "1", "true", "Bool", "Int"},
			{[]string{"&&", "||"}, "Bool", "Bool", "true", "1", "Int", "Int"},
		} {
			for _, op := range g.ops {
				because := "`" + op + "` expects `" + g.operand + "` operands"
				// The right operand is the wrong one.
				src := "fn main() { let r = " + g.good + " " + op + " " + g.bad + " }"
				p := testsupport.Analyze(t, src)
				wantDiags(t, "right operand of "+op, p, secondary(
					mismatch(spanIn(t, p, src, " "+op+" "+g.bad, g.bad), "expected `"+g.operand+"`, found `"+g.badType+"`"),
					spanIn(t, p, src, " "+op+" ", op), because))
				// The left operand is the wrong one.
				src = "fn main() { let r = " + g.bad + " " + op + " " + g.good + " }"
				p = testsupport.Analyze(t, src)
				wantDiags(t, "left operand of "+op, p, secondary(
					mismatch(spanIn(t, p, src, "= "+g.bad+" ", g.bad), "expected `"+g.operand+"`, found `"+g.badType+"`"),
					spanIn(t, p, src, " "+op+" ", op), because))
				// Right operands, and the result has the type of the operator.
				expr := g.good + " " + op + " " + g.good
				src = "fn main() { let r: " + g.wrongResult + " = " + expr + " }"
				p = testsupport.Analyze(t, src)
				wantDiags(t, "result of "+op, p, secondary(
					mismatch(spanIn(t, p, src, "= "+expr, expr), "expected `"+g.wrongResult+"`, found `"+g.result+"`"),
					spanIn(t, p, src, ": "+g.wrongResult, g.wrongResult), "expected because of this annotation"))
				src = "fn main() { let r: " + g.result + " = " + expr + " }"
				p = testsupport.Analyze(t, src)
				wantDiags(t, "operands and result of "+op+" agree", p)
			}
		}

		{
			// Both operands are wrong: one error each.
			const src = "fn main() { let r = true + false }"
			p := testsupport.Analyze(t, src)
			because := "`+` expects `Int` operands"
			plus := spanIn(t, p, src, " + ", "+")
			wantDiags(t, "both operands", p,
				secondary(mismatch(spanIn(t, p, src, "= true", "true"), "expected `Int`, found `Bool`"), plus, because),
				secondary(mismatch(spanIn(t, p, src, "+ false", "false"), "expected `Int`, found `Bool`"), plus, because))
		}
		{
			// The mistake is in the operand it is in, not in the whole sum.
			const src = "fn main() { let r = 1 + 2 * true }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "nested", p, secondary(mismatch(spanIn(t, p, src, "* true", "true"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, " * ", "*"), "`*` expects `Int` operands"))
		}

		// The unary operators.
		{
			const src = "fn main() { let r = -true }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "-true", p, secondary(mismatch(spanIn(t, p, src, "-true", "true"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, "-true", "-"), "`-` expects an `Int` operand"))
		}
		{
			const src = "fn main() { let r = !1 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "!1", p, secondary(mismatch(spanIn(t, p, src, "!1", "1"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, "!1", "!"), "`!` expects a `Bool` operand"))
		}
		for _, row := range []struct{ name, src, context, word, wantLabel string }{
			{"result of unary minus", "fn main() { let r: Bool = -1 }", "= -1", "-1", "expected `Bool`, found `Int`"},
			{"result of not", "fn main() { let r: Int = !true }", "= !true", "!true", "expected `Int`, found `Bool`"},
		} {
			p := testsupport.Analyze(t, row.src)
			annotation := row.wantLabel[len("expected `"):strings.Index(row.wantLabel, "`, found")]
			wantDiags(t, row.name, p, secondary(mismatch(spanIn(t, p, row.src, row.context, row.word), row.wantLabel),
				spanIn(t, p, row.src, ": "+annotation, annotation), "expected because of this annotation"))
		}
		{
			const src = "fn f(a: Int, b: Bool) -> Bool { !b && -a < 0 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "unary operators that are fine", p)
		}

		// `==` and `!=` check the right operand against the left one.
		{
			const src = "fn main() { let r = 1 == true }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "1 == true", p, secondary(mismatch(spanIn(t, p, src, "== true", "true"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, " == ", "=="), "`==` expects `Int` operands"))
		}
		{
			const src = "fn main() { let r = true != 1 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "true != 1", p, secondary(mismatch(spanIn(t, p, src, "!= 1", "1"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, " != ", "!="), "`!=` expects `Bool` operands"))
		}

		// An operand of the error type makes the operation the error type, so
		// nothing is said about the place where its result is used.
		for _, row := range []struct{ src, code string }{
			{"fn main() { let b: Bool = nope + 1 }", "E0301"},
			{"fn main() { let b: Bool = 1 + nope }", "E0301"},
			{"fn main() { let b: Int = nope < 1 }", "E0301"},
			{"fn main() { let b: Int = 1 >= nope }", "E0301"},
			{"fn main() { let b: Int = nope && true }", "E0301"},
			{"fn main() { let b: Int = true || nope }", "E0301"},
			{"fn main() { let b: Bool = -nope }", "E0301"},
			{"fn main() { let b: Int = !nope }", "E0301"},
			{"fn main() { let b: Int = nope == 1 }", "E0301"},
			{"fn main() { let b: Int = 1 != nope }", "E0301"},
			// The same for an operand that the parser could not read.
			{"fn main() { let b: Bool = 1 + }", "E0201"},
			{"fn main() { let b: Int = 1 < }", "E0201"},
			{"fn main() { let b: Int = 1 == }", "E0201"},
			{"fn main() { let b: Bool = - }", "E0201"},
		} {
			p := testsupport.Analyze(t, row.src)
			if got := testsupport.ErrorCodes(p); !slices.Equal(got, []string{row.code}) {
				t.Errorf("%s: error codes %v, want [%s]; diagnostics %+v", row.src, got, row.code, p.Session.Sink.All())
			}
		}
	})

	t.Run("equality_on_bools_ok", func(t *testing.T) {
		for _, row := range []struct{ name, src, fn, want string }{
			{"bools", "fn f(a: Bool, b: Bool) -> Bool { a == b }", "f", "(Bool, Bool) -> Bool"},
			{"ints", "fn g(x: Int) -> Bool { x != 3 }", "g", "(Int) -> Bool"},
			{"literals", "fn h() -> Bool { 1 == 2 }", "h", "() -> Bool"},
			{"result_is_bool", "fn main() { let b: Bool = true != false }", "main", "() -> ()"},
			{"left_binds_the_right", "fn k(x) -> Bool { 1 == x }", "k", "(Int) -> Bool"},
			{"right_binds_the_left", "fn m(x) -> Bool { x == true }", "m", "(Bool) -> Bool"},
			{"comparisons_of_comparisons", "fn n() -> Bool { (1 == 2) == (3 < 4) }", "n", "() -> Bool"},
			{"after_not", "fn o(a: Bool) -> Bool { !a == false }", "o", "(Bool) -> Bool"},
			{"in_a_condition", "fn q(x: Int) -> Int { if x == 1 { 1 } else { 2 } }", "q", "(Int) -> Int"},
			// Two parameters without annotations are not known to be anything
			// else than Int or Bool, so the comparison is accepted, whether or
			// not a call elsewhere says what they are, and in either order of
			// the functions: the call that binds them is not a mistake.
			{"unknown_parameters", "fn same(x, y) -> Bool { x == y }", "same", "(a, a) -> Bool"},
			{"parameters_bound_by_a_later_call", "fn same(x, y) -> Bool { x == y }\nfn main() { let b = same(1, 2) }", "same", "(Int, Int) -> Bool"},
			{"parameters_bound_by_an_earlier_call", "fn main() { let b = same(1, 2) }\nfn same(x, y) -> Bool { x == y }", "same", "(Int, Int) -> Bool"},
			{"parameters_bound_to_bool_later", "fn same(x, y) -> Bool { x != y }\nfn main() { let b = same(true, false) }", "same", "(Bool, Bool) -> Bool"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p)
			if got := testsupport.DefType(t, p, row.fn); got != row.want {
				t.Errorf("%s: type of %s is %q, want %q", row.name, row.fn, got, row.want)
			}
		}
		{
			// Equality, like the other operators, is the error type if an operand
			// is, and says nothing more.
			p := testsupport.Analyze(t, "fn main() { let b: Int = nope == nope }")
			wantCodes(t, p, "E0301", "E0301")
		}
	})

	t.Run("equality_on_functions_E0401", func(t *testing.T) {
		note := func(op string) string { return "`" + op + "` works on `Int` and `Bool` here" }
		// `f == f`: one error, on the left operand, with no secondary label (no
		// other place explains it), the note and no help.
		{
			const src = "fn f() {}\nfn g() -> Bool { f == f }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "f == f", p, mismatch(spanIn(t, p, src, "{ f ==", "f"), "expected `Int` or `Bool`, found `() -> ()`").
				WithNote(note("==")))
		}
		for _, row := range []struct{ name, src, context, word, found, op string }{
			{"not equal", "fn f() {}\nfn g() -> Bool { f != f }", "{ f !=", "f", "() -> ()", "!="},
			{"unit", "fn g() -> Bool { () == () }", "{ () ==", "()", "()", "=="},
			{"function with parameters", "fn h(a: Int) -> Int { a }\nfn g() -> Bool { h == h }", "{ h ==", "h", "(Int) -> Int", "=="},
			{"call of a unit function", "fn f() {}\nfn g() -> Bool { f() == f() }", "{ f() ==", "f()", "()", "=="},
			// The right operand is not checked against the left one, so it is not
			// a second mistake: `==` does not expect a function there.
			{"function and Int", "fn f() {}\nfn g() -> Bool { f == 1 }", "{ f ==", "f", "() -> ()", "=="},
			{"unit and Int", "fn g() -> Bool { () == 1 }", "{ () ==", "()", "()", "=="},
			{"function and another function", "fn f() {}\nfn h(a: Int) {}\nfn g() -> Bool { f == h }", "{ f ==", "f", "() -> ()", "=="},
			// A parameter without an annotation that the right operand makes a
			// function: the left operand is the mistake, once.
			{"parameter that the right operand makes a function", "fn f() {}\nfn g(x) -> Bool { x == f }", "{ x ==", "x", "() -> ()", "=="},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p, mismatch(spanIn(t, p, row.src, row.context, row.word), "expected `Int` or `Bool`, found `"+row.found+"`").
				WithNote(note(row.op)))
		}
		{
			// The right operand of a rejected comparison is not compared with the
			// left one, but it is still inferred: a mistake inside it is a mistake of
			// its own and gets its error, and the two do not hide each other.
			const src = "fn f() {}\nfn g() -> Bool { f == 1 + true }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "f == 1 + true", p,
				mismatch(spanIn(t, p, src, "{ f ==", "f"), "expected `Int` or `Bool`, found `() -> ()`").WithNote(note("==")),
				secondary(mismatch(spanIn(t, p, src, "+ true", "true"), "expected `Int`, found `Bool`"),
					spanIn(t, p, src, " + ", "+"), "`+` expects `Int` operands"))
		}
		{
			// When the left operand is fine, a wrong right operand is an ordinary
			// mismatch, with the operator as the reason.
			const src = "fn f() {}\nfn g() -> Bool { 1 == f }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "1 == f", p, secondary(mismatch(spanIn(t, p, src, "== f", "f"), "expected `Int`, found `() -> ()`"),
				spanIn(t, p, src, " == ", "=="), "`==` expects `Int` operands"))
		}
		{
			p := testsupport.Analyze(t, "fn g() -> Bool { nope == 1 }")
			wantCodes(t, p, "E0301")
		}
		{
			// The result of a comparison that was rejected is still Bool: a body
			// that wants Int is a second mistake, with its own error.
			const src = "fn f() {}\nfn g() -> Int { f == f }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "f == f where Int is expected", p,
				mismatch(spanIn(t, p, src, "{ f ==", "f"), "expected `Int` or `Bool`, found `() -> ()`").WithNote(note("==")),
				secondary(mismatch(spanIn(t, p, src, "{ f == f }", "f == f"), "expected `Int`, found `Bool`"),
					spanIn(t, p, src, "-> Int", "Int"), "expected because of the return type"))
		}

		// Whether a program is accepted does not depend on the order of its
		// functions. `same` compares two parameters that nothing in its own body
		// types; `main` calls it with two functions. Written either way round, the
		// program has the one error, on the `x` of `same`, with the same text: when
		// `same` comes first its body is checked before the call has said what `x`
		// is, and the left operand is judged after all the bodies.
		for _, row := range []struct{ name, src string }{
			{"definition first", "fn f() {}\nfn same(x, y) -> Bool { x == y }\nfn main() { let b = same(f, f) }"},
			{"call first", "fn f() {}\nfn main() { let b = same(f, f) }\nfn same(x, y) -> Bool { x == y }"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p, mismatch(spanIn(t, p, row.src, "{ x ==", "x"), "expected `Int` or `Bool`, found `() -> ()`").
				WithNote(note("==")))
		}
		// A parameter that nothing ever types is not an error, and one that a
		// call types as `Int` or `Bool` is not either (see equality_on_bools_ok);
		// a comparison of two of them that is rejected is reported once for each
		// comparison, not once for each place that the parameter is used.
		{
			const src = "fn f() {}\nfn same(x, y) -> Bool { x == y }\nfn other(x, y) -> Bool { x != y }\nfn main() { let a = same(f, f) }\nfn rest() { let b = other(f, f) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "two comparisons", p,
				mismatch(spanIn(t, p, src, "{ x ==", "x"), "expected `Int` or `Bool`, found `() -> ()`").WithNote(note("==")),
				mismatch(spanIn(t, p, src, "{ x !=", "x"), "expected `Int` or `Bool`, found `() -> ()`").WithNote(note("!=")))
		}
	})

	t.Run("let_annotation_mismatch", func(t *testing.T) {
		{
			const src = "fn main() { let x: Bool = 1 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Bool", p, secondary(mismatch(spanIn(t, p, src, "= 1", "1"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, ": Bool", "Bool"), "expected because of this annotation"))
		}
		{
			const src = "fn main() { let x: () = 1 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "unit, which has no help here", p, secondary(mismatch(spanIn(t, p, src, "= 1", "1"), "expected `()`, found `Int`"),
				spanIn(t, p, src, ": ()", "()"), "expected because of this annotation"))
		}
		{
			// The variable has the annotated type even when the initializer is
			// wrong, so its uses are not a second mistake.
			const src = "fn main() {\n  let x: Bool = 1\n  let y: Bool = x\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "annotated, wrong initializer", p, secondary(mismatch(spanIn(t, p, src, "= 1", "1"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, "x: Bool = 1", "Bool"), "expected because of this annotation"))
		}
		{
			// With no annotation the initializer decides, and the variable keeps
			// that type for its uses.
			const src = "fn main() {\n  let x = 1\n  let y: Bool = x\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "inferred", p, secondary(mismatch(spanIn(t, p, src, "Bool = x", "x"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, ": Bool", "Bool"), "expected because of this annotation"))
		}
		{
			// A later `let` of the same name is another variable with its own type.
			const src = "fn main() {\n  let x = 1\n  let x = true\n  let y: Bool = x\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "shadowing", p)
		}
		{
			// The annotation reaches the tail of a block initializer, which is
			// where the mismatch is found.
			const src = "fn main() { let x: Bool = { 1 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "block initializer", p, secondary(mismatch(spanIn(t, p, src, "{ 1 }", "1"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, ": Bool", "Bool"), "expected because of this annotation"))
		}
		{
			const src = "fn f() {}\nfn main() { let x: Int = f }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "function value", p, secondary(mismatch(spanIn(t, p, src, "= f", "f"), "expected `Int`, found `() -> ()`"),
				spanIn(t, p, src, ": Int", "Int"), "expected because of this annotation"))
		}
		{
			// An unknown name is E0301 and nothing else.
			p := testsupport.Analyze(t, "fn main() { let x: Int = nope }")
			wantCodes(t, p, "E0301")
		}
		{
			p := testsupport.Analyze(t, "fn main() { let x: Int = 1\n let y: Bool = true\n let z: () = () }")
			wantDiags(t, "annotations that are fine", p)
		}
	})

	t.Run("extern_print_type", func(t *testing.T) {
		p := testsupport.Analyze(t, "fn main() { print(1) }")
		wantDiags(t, "print(1)", p)
		if got := testsupport.DefType(t, p, "print"); got != "(Int) -> ()" {
			t.Errorf("type of print is %q, want (Int) -> ()", got)
		}

		// A function of the file called print shadows the prelude's, and the call
		// is checked against it: the file's print is the one DefType finds.
		p = testsupport.Analyze(t, "fn print(x: Bool) {}\nfn main() { print(true) }")
		wantDiags(t, "print of the file", p)
		if got := testsupport.DefType(t, p, "print"); got != "(Bool) -> ()" {
			t.Errorf("type of print in a file that defines it is %q, want (Bool) -> ()", got)
		}
	})

	t.Run("call_argument_mismatch", func(t *testing.T) {
		const defF = "fn f(a: Int, b: Bool) {}\n"
		{
			const src = defF + "fn main() { f(1, 2) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "second argument", p, secondary(mismatch(spanIn(t, p, src, ", 2)", "2"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, "b: Bool", "b: Bool"), "the parameter is declared here"))
		}
		{
			const src = defF + "fn main() { f(true, true) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "first argument", p, secondary(mismatch(spanIn(t, p, src, "f(true", "true"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, "a: Int", "a: Int"), "the parameter is declared here"))
		}
		{
			const src = defF + "fn main() { f(true, 2) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "both arguments", p,
				secondary(mismatch(spanIn(t, p, src, "f(true", "true"), "expected `Int`, found `Bool`"),
					spanIn(t, p, src, "a: Int", "a: Int"), "the parameter is declared here"),
				secondary(mismatch(spanIn(t, p, src, ", 2)", "2"), "expected `Bool`, found `Int`"),
					spanIn(t, p, src, "b: Bool", "b: Bool"), "the parameter is declared here"))
		}
		{
			// The first call fixes the type of an unannotated parameter, and the
			// second is where it is found wrong.
			const src = "fn g(x) {}\nfn main() {\n  g(1)\n  g(true)\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "unannotated parameter", p, secondary(mismatch(spanIn(t, p, src, "g(true)", "true"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, "(x)", "x"), "the parameter is declared here"))
		}
		{
			// The parameter of a function of the prelude lies in another file, and
			// a label there could not be drawn under the primary one, so there is
			// no secondary label. (RenderHuman refuses one.)
			const src = "fn main() { print(true) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "prelude function", p, mismatch(spanIn(t, p, src, "print(true)", "true"), "expected `Int`, found `Bool`"))
			for _, d := range p.Session.Sink.All() {
				if text := diag.RenderHuman(d, &p.Session.Sources); !strings.Contains(text, "test.ki:1:19") {
					t.Errorf("prelude function: the rendered diagnostic is\n%s\nwant it to point at test.ki:1:19", text)
				}
			}
		}
		{
			// A callee that is not a definition has no parameter to point at.
			const src = "fn f(a: Int) {}\nfn main() {\n  let g = f\n  g(true)\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "local callee", p, mismatch(spanIn(t, p, src, "g(true)", "true"), "expected `Int`, found `Bool`"))
		}
		{
			// The same with a second parameter: the one that is wrong is the
			// second argument, which has no parameter to point at either.
			const src = defF + "fn main() {\n  let g = f\n  g(1, 2)\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "local callee, second argument", p, mismatch(spanIn(t, p, src, "g(1, 2)", "2"), "expected `Bool`, found `Int`"))
		}
		{
			// The argument is checked, not just inferred: its tail is the place.
			const src = defF + "fn main() { f({ true }, true) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "block argument", p, secondary(mismatch(spanIn(t, p, src, "{ true }", "true"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, "a: Int", "a: Int"), "the parameter is declared here"))
		}
		{
			p := testsupport.Analyze(t, defF+"fn main() { f(1, true)\n f(1 + 2, 1 < 2) }")
			wantDiags(t, "arguments that are fine", p)
		}
	})

	t.Run("if_branches_must_agree", func(t *testing.T) {
		{
			const src = "fn main() { let x = if true { 1 } else { false } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "inferred", p, secondary(mismatch(spanIn(t, p, src, "{ false }", "false"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, "{ 1 }", "1"), "the other branch has type `Int`"))
		}
		{
			// With an annotation the branches are still compared with each
			// other first, and the mistake is not reported a second time on the
			// whole `if`.
			const src = "fn main() { let x: Int = if true { 1 } else { false } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "annotated, branches differ", p, secondary(mismatch(spanIn(t, p, src, "{ false }", "false"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, "{ 1 }", "1"), "the other branch has type `Int`"))
		}
		{
			// The annotation is a third type: the mistake is still between the
			// branches, and the `if` has no type to compare with the annotation.
			const src = "fn main() { let x: Bool = if true { 1 } else { false } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "annotated with a third type", p, secondary(mismatch(spanIn(t, p, src, "{ false }", "false"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, "{ 1 }", "1"), "the other branch has type `Int`"))
		}
		{
			// The branches agree and the annotation is what they disagree with.
			const src = "fn main() { let x: Bool = if true { 1 } else { 2 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "annotated, branches agree", p, secondary(
				mismatch(spanIn(t, p, src, "= if true { 1 } else { 2 }", "if true { 1 } else { 2 }"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, ": Bool", "Bool"), "expected because of this annotation"))
		}
		{
			const src = "fn f() { if true { 1 } else { 2 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "unit body", p, secondary(
				mismatch(spanIn(t, p, src, "{ if true { 1 } else { 2 } }", "if true { 1 } else { 2 }"), "expected `()`, found `Int`"),
				spanIn(t, p, src, " f(", "f"), "expected because of the return type").WithHelp(unusedHelp))
		}
		{
			// The other branch of an `else if` is the whole `if` that follows.
			const src = "fn main() { let x = if true { 1 } else if false { 2 } else { true } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "else if", p, secondary(mismatch(spanIn(t, p, src, "{ true }", "true"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, "{ 2 }", "2"), "the other branch has type `Int`"))
		}
		{
			// A branch without a tail is `()`, and the block is its place.
			const src = "fn main() { let x = if true { } else { 1 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "empty then", p, secondary(mismatch(spanIn(t, p, src, "{ 1 }", "1"), "expected `()`, found `Int`"),
				spanIn(t, p, src, "{ } else", "{ }"), "the other branch has type `()`"))
		}
		{
			// The tail of the then branch is the place even when it follows
			// statements.
			const src = "fn main() { let x = if true { let y = 1\n y } else { false } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "then with statements", p, secondary(mismatch(spanIn(t, p, src, "{ false }", "false"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, " y }", "y"), "the other branch has type `Int`"))
		}
		{
			// An `if` without `else` in the else position, whose own branch is
			// wrong: that is the one mistake, and the `if` that holds it has no
			// type to compare with the annotation.
			const src = "fn f(a: Bool, b: Bool) { let x: Int = if a { () } else if b { 1 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "else if without else, wrong branch", p, secondary(mismatch(spanIn(t, p, src, "{ 1 }", "1"), "expected `()`, found `Int`"),
				spanIn(t, p, src, "{ () }", "()"), "the other branch has type `()`"))
		}
		{
			// An `else if` with no `else` that does not fit the other branch: it is
			// `()` and the other branch is `Int`. That `if` is the one mistake and
			// has no type, so it is not compared with the annotation too.
			const src = "fn f(a: Bool, b: Bool) { let x: Bool = if a { 1 } else if b { 2 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "else if without else, not the type of the other branch", p, secondary(
				mismatch(spanIn(t, p, src, "else if b { 2 }", "if b { 2 }"), "expected `Int`, found `()`"),
				spanIn(t, p, src, "{ 1 }", "1"), "the other branch has type `Int`"))
		}
		{
			// The value of the `if` has the type of its branches.
			const src = "fn main() {\n  let x = if true { 1 } else { 2 }\n  let y: Bool = x\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "type of the if", p, secondary(mismatch(spanIn(t, p, src, "Bool = x", "x"), "expected `Bool`, found `Int`"),
				spanIn(t, p, src, ": Bool", "Bool"), "expected because of this annotation"))
		}
		{
			p := testsupport.Analyze(t, "fn f(c: Bool) -> Int { 1 + if c { 2 } else if !c { 3 } else { 4 } }")
			wantDiags(t, "branches that agree", p)
		}
	})

	t.Run("infinite_type_E0405", func(t *testing.T) {
		infinite := func(at base.Span, note string) diag.Diagnostic {
			return diag.NewError(diag.E(405), "cannot construct an infinite type",
				diag.Label{Span: at, Message: "this type would contain itself"}).WithNote(note)
		}
		{
			// A function used as a value where its own parameter is expected.
			const src = "fn f(x) { f(f) }\nfn main() { }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "f(f)", p, infinite(spanIn(t, p, src, "(f)", "f"),
				"`a` would have to be `(a) -> ()`, which has no finite size"))
		}
		{
			// A parameter that is called with itself.
			const src = "fn g(x) { x(x) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "x(x)", p, infinite(spanIn(t, p, src, "{ x(x)", "x"),
				"`a` would have to be `(a) -> b`, which has no finite size"))
		}
	})
}
