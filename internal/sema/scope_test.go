package sema_test

import (
	"cmp"
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/sema"
	"github.com/aldezex/kilang/internal/syntax"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here read the result of Resolve through the tables it leaves beside
// the user's tree. They reuse nameExprs, resAt, onlyRes, wantCodes and
// resolveSources from resolve_test.go, which is in the same package.

// userFile returns the id of the user's file in p.
func userFile(p *testsupport.Program) base.FileID {
	return p.Frontend.Modules[p.Frontend.User].File
}

// spanIn returns the span of word in src, where word is the first occurrence of
// it inside context, a piece of src that occurs exactly once: the test says
// where a name is by quoting the text around it.
func spanIn(t *testing.T, p *testsupport.Program, src, context, word string) base.Span {
	t.Helper()
	if n := strings.Count(src, context); n != 1 {
		t.Fatalf("%q occurs %d times in %q, want exactly 1", context, n, src)
	}
	lo := strings.Index(src, context) + strings.Index(context, word)
	if lo < strings.Index(src, context) {
		t.Fatalf("%q is not in %q", word, context)
	}
	return base.Span{File: userFile(p), Lo: uint32(lo), Hi: uint32(lo + len(word))}
}

// useAt returns what the n-th name called name in the user's file, counting
// from 0 in source order, stands for.
func useAt(t *testing.T, p *testsupport.Program, name string, n int) sema.Res {
	t.Helper()
	return resAt(t, p.Frontend.Resolved, p.Frontend.Modules, p.Session.Interner, p.Frontend.User, name, n)
}

// fnNamed returns the id and the definition of the function called name in the
// user's file.
func fnNamed(t *testing.T, p *testsupport.Program, name string) (sema.DefID, sema.Def) {
	t.Helper()
	for id, d := range p.Frontend.Resolved.Defs.All() {
		if d.Module == p.Frontend.User && p.Session.Interner.String(d.Name) == name {
			return id, d
		}
	}
	t.Fatalf("the file defines no function called %q", name)
	panic("unreachable")
}

// paramsOf returns the variables that the parameters of the user's function
// called name bind, in order.
func paramsOf(t *testing.T, p *testsupport.Program, name string) []sema.LocalID {
	t.Helper()
	_, d := fnNamed(t, p, name)
	return p.Frontend.Resolved.Modules[p.Frontend.User].ParamLocals[d.Item]
}

// paramLocal returns the variable that the i-th parameter of the user's
// function called name binds. It fails the test if there is no such parameter,
// so that the checks that use the result never index past the end.
func paramLocal(t *testing.T, p *testsupport.Program, name string, i int) sema.LocalID {
	t.Helper()
	params := paramsOf(t, p, name)
	if i >= len(params) {
		t.Fatalf("%s binds %d parameters as variables, want at least %d", name, len(params), i+1)
	}
	return params[i]
}

// letNamed returns the variable that the n-th `let` called name in the user's
// file binds, counting from 0 by statement id. (A `let` whose initializer holds
// a block with lets of its own gets a later id than they do, so the tests that
// call this with n above 0 also compare the Local's span.)
func letNamed(t *testing.T, p *testsupport.Program, name string, n int) sema.LocalID {
	t.Helper()
	user := p.Frontend.User
	var lets []sema.LocalID
	for id, s := range p.Frontend.Modules[user].Stmts.All() {
		if s.Kind == syntax.StmtLet && p.Session.Interner.String(s.Name) == name {
			lets = append(lets, p.Frontend.Resolved.Modules[user].LetLocal[id])
		}
	}
	if n >= len(lets) {
		t.Fatalf("the file has %d lets called %q, want at least %d", len(lets), name, n+1)
	}
	return lets[n]
}

// userLocals returns the variables bound in the functions of the user's file,
// in id order. (The prelude's functions bind variables too: their parameters.)
func userLocals(p *testsupport.Program) []sema.Local {
	rp := p.Frontend.Resolved
	var locals []sema.Local
	for _, l := range rp.Locals.All() {
		if rp.Defs.Get(l.Owner).Module == p.Frontend.User {
			locals = append(locals, l)
		}
	}
	return locals
}

// wantLocal fails the test unless res stands for the variable id, and id is the
// variable called name that is bound at span in the function owner.
func wantLocal(t *testing.T, p *testsupport.Program, what string, res sema.Res, id sema.LocalID, name string, span base.Span, owner sema.DefID) {
	t.Helper()
	if want := (sema.Res{Kind: sema.ResLocal, Local: id}); res != want {
		t.Errorf("%s stands for %+v, want %+v", what, res, want)
		return
	}
	want := sema.Local{Name: p.Session.Interner.InternString(name), Span: span, Owner: owner}
	if got := p.Frontend.Resolved.Locals.Get(id); got != want {
		t.Errorf("%s: local %d is %+v, want %+v", what, id, got, want)
	}
}

// userDiags returns the diagnostics of p sorted by position, which is the order
// `ki check` shows them in, so that a test does not depend on the order in which
// Resolve walks a function. It fails the test unless there are exactly n, so
// that the checks that index the result never go past its end.
func userDiags(t *testing.T, p *testsupport.Program, n int) []diag.Diagnostic {
	t.Helper()
	diags := slices.Clone(p.Session.Sink.All())
	if len(diags) != n {
		t.Fatalf("%d diagnostics %+v, want exactly %d", len(diags), diags, n)
	}
	slices.SortStableFunc(diags, func(a, b diag.Diagnostic) int { return cmp.Compare(a.Primary.Span.Lo, b.Primary.Span.Lo) })
	return diags
}

// checkUnknown fails the test unless d is the E0301 for the name written at
// span, with no secondary label and no note. It leaves the help line alone: a
// name that is close to one that exists has one, and TestSuggestions is the test
// that pins what it says.
func checkUnknown(t *testing.T, d diag.Diagnostic, name string, span base.Span) {
	t.Helper()
	checkUnknownWith(t, d, name, span, nil, nil)
}

// checkUnknownSelf is checkUnknown for a name that is the one its own `let`
// binds: the diagnostic has the label on the pattern of that `let`, and the note
// with the rule (TestScopes/let_in_own_init_names_the_let).
func checkUnknownSelf(t *testing.T, d diag.Diagnostic, name string, span, letName base.Span) {
	t.Helper()
	label := diag.Label{Span: letName, Message: "`" + name + "` is bound by this `let`, which cannot see itself"}
	checkUnknownWith(t, d, name, span, []diag.Label{label}, []string{"a `let` is visible only after its own statement"})
}

// checkUnknownWith fails the test unless d is the E0301 for the name written at
// span, with exactly these secondary labels and notes.
func checkUnknownWith(t *testing.T, d diag.Diagnostic, name string, span base.Span, secondary []diag.Label, notes []string) {
	t.Helper()
	if d.Severity != diag.Error || d.Code != diag.E(301) {
		t.Errorf("%s: %v %v, want error E0301", name, d.Severity, d.Code)
	}
	if want := "unknown name `" + name + "`"; d.Message != want {
		t.Errorf("message %q, want %q", d.Message, want)
	}
	if want := (diag.Label{Span: span, Message: "not found in this scope"}); d.Primary != want {
		t.Errorf("%s: primary label %+v, want %+v", name, d.Primary, want)
	}
	if !slices.Equal(d.Secondary, secondary) || !slices.Equal(d.Notes, notes) {
		t.Errorf("%s: secondary labels %+v and notes %q, want %+v and %q", name, d.Secondary, d.Notes, secondary, notes)
	}
}

// wantUnknown fails the test unless the name at the n-th use of name stands for
// nothing and has been reported (exactly the zero fields besides the kind).
func wantUnknown(t *testing.T, p *testsupport.Program, name string, n int) {
	t.Helper()
	if res, want := useAt(t, p, name, n), (sema.Res{Kind: sema.ResError}); res != want {
		t.Errorf("use %d of %q stands for %+v, want %+v", n, name, res, want)
	}
}

func TestScopes(t *testing.T) {
	t.Run("param_resolves_to_local", func(t *testing.T) {
		// `b` is the second parameter of the first function; `first` and `print`
		// are parameters that have the name of a function of the file and of the
		// prelude; `a` is a parameter of the first function and of the last, and
		// each use finds its own. The uses are in source order: b, then first
		// and print, then a.
		const src = `fn first(a: Int, b: Int) -> Int {
  b
}
fn second(first: Int, print: Int) -> Int {
  first + print
}
fn third(a: Int) -> Int {
  a
}
`
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		firstID, _ := fnNamed(t, p, "first")
		secondID, _ := fnNamed(t, p, "second")
		thirdID, _ := fnNamed(t, p, "third")

		// The parameters of a function are its first variables, in order, and
		// each Local has the span of the name alone, not of `a: Int`.
		first, second, third := paramsOf(t, p, "first"), paramsOf(t, p, "second"), paramsOf(t, p, "third")
		if len(first) != 2 || len(second) != 2 || len(third) != 1 {
			t.Fatalf("the functions bind %d, %d and %d parameters, want 2, 2 and 1", len(first), len(second), len(third))
		}
		if got := len(userLocals(p)); got != 5 {
			t.Errorf("the file binds %d variables, want 5 (the parameters)", got)
		}
		wantLocal(t, p, "b", useAt(t, p, "b", 0), first[1], "b", spanIn(t, p, src, "(a: Int, b: Int)", "b"), firstID)
		wantLocal(t, p, "first", useAt(t, p, "first", 0), second[0], "first", spanIn(t, p, src, "(first: Int,", "first"), secondID)
		wantLocal(t, p, "print", useAt(t, p, "print", 0), second[1], "print", spanIn(t, p, src, "print: Int)", "print"), secondID)
		wantLocal(t, p, "a", useAt(t, p, "a", 0), third[0], "a", spanIn(t, p, src, "third(a: Int)", "a"), thirdID)
		if first[0] == third[0] {
			t.Errorf("the `a` of first and the `a` of third are the same variable %d", first[0])
		}
		if got := p.Frontend.Resolved.Locals.Get(first[0]); got.Name != p.Session.Interner.InternString("a") ||
			got.Span != spanIn(t, p, src, "(a: Int, b: Int)", "a") || got.Owner != firstID {
			t.Errorf("the first parameter of first is %+v, want `a` at its place, owned by first", got)
		}

		// A parameter is not visible outside its function, and a function
		// without parameters binds none.
		p = testsupport.Analyze(t, "fn a(x: Int) {}\nfn b() { x }\n")
		wantCodes(t, p, "E0301")
		wantUnknown(t, p, "x", 0)
		if params := paramsOf(t, p, "b"); len(params) != 0 {
			t.Errorf("b binds the variables %v, want none", params)
		}

		// A function with no body binds its parameters too: they are the first
		// variables of the program here, and the first of all is variable 0,
		// which must still be told apart from "no variable".
		rp, mods, in, sink := resolveSources(t, "#[extern(\"k\")]\npub fn g(p: Int, q: Int)\n", "fn f(a: Int) { a }\n")
		if sink.HasErrors() {
			t.Fatalf("unexpected errors: %+v", sink.All())
		}
		if rp.Locals.Len() != 3 {
			t.Fatalf("the program binds %d variables, want 3 (p, q and a)", rp.Locals.Len())
		}
		gParams := rp.Modules[0].ParamLocals[rp.Defs.Get(0).Item]
		if !slices.Equal(gParams, []sema.LocalID{0, 1}) {
			t.Errorf("g binds %v, want variables 0 and 1", gParams)
		}
		if got := rp.Locals.Get(1); got.Name != in.InternString("q") || got.Owner != 0 {
			t.Errorf("variable 1 is %+v, want `q`, owned by g", got)
		}
		if res, want := onlyRes(t, rp, mods, in, 1, "a"), (sema.Res{Kind: sema.ResLocal, Local: 2}); res != want {
			t.Errorf("a stands for %+v, want %+v", res, want)
		}
		rp, mods, in, _ = resolveSources(t, "", "fn f(a: Int) { a }\n")
		if res, want := onlyRes(t, rp, mods, in, 1, "a"), (sema.Res{Kind: sema.ResLocal, Local: 0}); res != want {
			t.Errorf("with no prelude, a stands for %+v, want variable 0 as %+v", res, want)
		}

		// A function defined twice (E0302) has a Def for each definition, and
		// each one owns its own variables: the owner is the definition being
		// walked, not the one that its name stands for, which is the first.
		const twice = "fn f(a: Int) { a }\nfn f(b: Int) { b }\n"
		p = testsupport.Analyze(t, twice)
		wantCodes(t, p, "E0302")
		var fs []sema.DefID
		for id, d := range p.Frontend.Resolved.Defs.All() {
			if d.Module == p.Frontend.User && p.Session.Interner.String(d.Name) == "f" {
				fs = append(fs, id)
			}
		}
		if len(fs) != 2 {
			t.Fatalf("the file defines f %d times, want 2", len(fs))
		}
		for i, name := range []string{"a", "b"} {
			params := p.Frontend.Resolved.Modules[p.Frontend.User].ParamLocals[p.Frontend.Resolved.Defs.Get(fs[i]).Item]
			if len(params) != 1 {
				t.Fatalf("definition %d of f binds %d parameters, want 1", i, len(params))
			}
			wantLocal(t, p, name, useAt(t, p, name, 0), params[0], name, spanIn(t, p, twice, "f("+name+":", name), fs[i])
		}
	})

	t.Run("let_visible_after_binding", func(t *testing.T) {
		// `a` is read by the next statement, by two blocks inside the following
		// one and by the tail; `p` is read from the same two blocks, three scopes
		// away from the parameters. Uses in source order: p, a; p, a, b; a, b.
		const src = `fn f(p: Int) -> Int {
  let a: Int = p
  let b = a
  {
    {
      p + a + b
    }
  }
  a + b
}
`
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		fID, _ := fnNamed(t, p, "f")
		param := paramLocal(t, p, "f", 0)
		la, lb := letNamed(t, p, "a", 0), letNamed(t, p, "b", 0)
		if la == lb || la == param || lb == param {
			t.Fatalf("the parameter and the two lets are variables %d, %d and %d, want three different ones", param, la, lb)
		}
		spanA, spanB := spanIn(t, p, src, "let a:", "a"), spanIn(t, p, src, "let b =", "b")
		spanP := spanIn(t, p, src, "fn f(p:", "p")
		for i := range 2 {
			wantLocal(t, p, "p", useAt(t, p, "p", i), param, "p", spanP, fID)
		}
		wantLocal(t, p, "a in the next let", useAt(t, p, "a", 0), la, "a", spanA, fID)
		wantLocal(t, p, "a in two blocks", useAt(t, p, "a", 1), la, "a", spanA, fID)
		wantLocal(t, p, "a in the tail", useAt(t, p, "a", 2), la, "a", spanA, fID)
		wantLocal(t, p, "b in two blocks", useAt(t, p, "b", 0), lb, "b", spanB, fID)
		wantLocal(t, p, "b in the tail", useAt(t, p, "b", 1), lb, "b", spanB, fID)
		if got := len(userLocals(p)); got != 3 {
			t.Errorf("the function binds %d variables, want 3", got)
		}
	})

	t.Run("let_not_visible_in_own_init", func(t *testing.T) {
		// The `x` that `let x` initializes is not bound yet: it is unknown, and
		// reported once, at the initializer. The `x` after the statement is the
		// variable the `let` binds, so the mistake does not spread.
		const src = "fn f() {\n  let x = x\n  x\n}\n"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p, "E0301")
		checkUnknownSelf(t, userDiags(t, p, 1)[0], "x", spanIn(t, p, src, "= x", "x"), spanIn(t, p, src, "let x", "x"))
		wantUnknown(t, p, "x", 0)
		fID, _ := fnNamed(t, p, "f")
		wantLocal(t, p, "x after the let", useAt(t, p, "x", 1), letNamed(t, p, "x", 0), "x", spanIn(t, p, src, "let x", "x"), fID)

		// With a parameter called x the initializer reads the parameter, and the
		// `x` that follows reads the new variable.
		const withParam = "fn g(x: Int) {\n  let x = x\n  x\n}\n"
		p = testsupport.Analyze(t, withParam)
		wantCodes(t, p)
		gID, _ := fnNamed(t, p, "g")
		wantLocal(t, p, "x in the initializer", useAt(t, p, "x", 0), paramLocal(t, p, "g", 0), "x", spanIn(t, p, withParam, "g(x:", "x"), gID)
		wantLocal(t, p, "x after the let", useAt(t, p, "x", 1), letNamed(t, p, "x", 0), "x", spanIn(t, p, withParam, "let x", "x"), gID)
	})

	t.Run("let_in_own_init_names_the_let", func(t *testing.T) {
		// The unknown name is the one that the `let` is binding: the label on
		// the pattern says so and the note gives the rule. (`total` is far from
		// every name there is, so there is no suggestion to confuse the check.)
		const src = "fn f() {\n  let total = total\n  total\n}\n"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p, "E0301")
		d := userDiags(t, p, 1)[0]
		checkUnknownSelf(t, d, "total", spanIn(t, p, src, "= total", "total"), spanIn(t, p, src, "let total", "total"))
		if d.Help != "" || len(d.Fixits) != 0 {
			t.Errorf("help %q and fixits %+v, want none", d.Help, d.Fixits)
		}

		// Inside a call, and inside a block that is the initializer: the name
		// is still the one the `let` binds, even if that `let` is not the
		// innermost one.
		const call = "fn f() {\n  let total = g(total)\n}\nfn g(x: Int) {}\n"
		p = testsupport.Analyze(t, call)
		wantCodes(t, p, "E0301")
		checkUnknownSelf(t, userDiags(t, p, 1)[0], "total", spanIn(t, p, call, "g(total)", "total"), spanIn(t, p, call, "let total", "total"))
		const nested = "fn f() {\n  let total = {\n    let inner = total\n    inner\n  }\n}\n"
		p = testsupport.Analyze(t, nested)
		wantCodes(t, p, "E0301")
		checkUnknownSelf(t, userDiags(t, p, 1)[0], "total", spanIn(t, p, nested, "= total", "total"), spanIn(t, p, nested, "let total", "total"))

		// Two `let`s that bind the same name, one inside the other: the name is
		// unknown in the initializer of both, and the label is on the inner one,
		// the `let` that the name is nearest to.
		const same = "fn f() {\n  let total = {\n    let total = total\n    total\n  }\n}\n"
		p = testsupport.Analyze(t, same)
		wantCodes(t, p, "E0301")
		checkUnknownSelf(t, userDiags(t, p, 1)[0], "total", spanIn(t, p, same, "= total\n", "total"), spanIn(t, p, same, "let total = total", "total"))

		// An unknown name that is not the one being bound gets neither label nor
		// note, the `let` that is being bound or one that already finished, in
		// another function or in this one, notwithstanding.
		for _, row := range []struct{ what, src string }{
			{"another name", "fn f() {\n  let total = totl\n}\n"},
			{"a let that finished", "fn f() {\n  { let total = 1 }\n  total\n}\n"},
			{"a let of another function", "fn a() { let total = 1 }\nfn b() { total }\n"},
			{"inside the let after it", "fn f() {\n  let total = 1\n  let other = total2\n}\n"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantCodes(t, p, "E0301")
			if d := userDiags(t, p, 1)[0]; len(d.Secondary) != 0 || len(d.Notes) != 0 {
				t.Errorf("%s: secondary labels %+v and notes %q, want none", row.what, d.Secondary, d.Notes)
			}
		}
	})

	t.Run("shadowing_picks_innermost", func(t *testing.T) {
		// An inner block binds `n` again: the use inside it is the inner
		// variable and the use after it is the outer one. Both variables exist.
		const inner = `fn f() -> Int {
  let n = 1
  {
    let n = 2
    n
  }
  n
}
`
		p := testsupport.Analyze(t, inner)
		wantCodes(t, p)
		fID, _ := fnNamed(t, p, "f")
		outerN, innerN := letNamed(t, p, "n", 0), letNamed(t, p, "n", 1)
		if outerN == innerN {
			t.Fatalf("both lets bind variable %d, want two variables", outerN)
		}
		wantLocal(t, p, "n in the block", useAt(t, p, "n", 0), innerN, "n", spanIn(t, p, inner, "let n = 2", "n"), fID)
		wantLocal(t, p, "n after the block", useAt(t, p, "n", 1), outerN, "n", spanIn(t, p, inner, "let n = 1", "n"), fID)
		if got := len(userLocals(p)); got != 2 {
			t.Errorf("the function binds %d variables, want 2", got)
		}

		// The same block can bind a name again, and the initializer of the
		// second binding still reads the first. A `let` can shadow a parameter.
		const same = `fn g(p: Int) -> Int {
  let y = p
  let y = y + 1
  let p = y
  p
}
`
		p = testsupport.Analyze(t, same)
		wantCodes(t, p)
		gID, _ := fnNamed(t, p, "g")
		param, y1, y2, p2 := paramLocal(t, p, "g", 0), letNamed(t, p, "y", 0), letNamed(t, p, "y", 1), letNamed(t, p, "p", 0)
		wantLocal(t, p, "p in the first let", useAt(t, p, "p", 0), param, "p", spanIn(t, p, same, "g(p:", "p"), gID)
		wantLocal(t, p, "y in the second let", useAt(t, p, "y", 0), y1, "y", spanIn(t, p, same, "let y = p", "y"), gID)
		wantLocal(t, p, "y in the third let", useAt(t, p, "y", 1), y2, "y", spanIn(t, p, same, "let y = y + 1", "y"), gID)
		wantLocal(t, p, "p in the tail", useAt(t, p, "p", 1), p2, "p", spanIn(t, p, same, "let p = y", "p"), gID)
		if y1 == y2 || param == p2 {
			t.Errorf("the lets share variables: y %d and %d, p %d and %d", y1, y2, param, p2)
		}
		if got := len(userLocals(p)); got != 4 {
			t.Errorf("the function binds %d variables, want 4", got)
		}

		// A variable shadows a function, the prelude's included, from its `let`
		// on: `h` is the function before `let h` and the variable after it.
		const global = `fn h() {}
fn k() {
  h
  let h = 1
  h
  let print = h
  print
}
`
		p = testsupport.Analyze(t, global)
		wantCodes(t, p)
		hID, _ := fnNamed(t, p, "h")
		if res, want := useAt(t, p, "h", 0), (sema.Res{Kind: sema.ResDef, Def: hID}); res != want {
			t.Errorf("h before its let stands for %+v, want the function %+v", res, want)
		}
		kID, _ := fnNamed(t, p, "k")
		lh := letNamed(t, p, "h", 0)
		wantLocal(t, p, "h after its let", useAt(t, p, "h", 1), lh, "h", spanIn(t, p, global, "let h = 1", "h"), kID)
		wantLocal(t, p, "h in the next let", useAt(t, p, "h", 2), lh, "h", spanIn(t, p, global, "let h = 1", "h"), kID)
		wantLocal(t, p, "print", useAt(t, p, "print", 0), letNamed(t, p, "print", 0), "print", spanIn(t, p, global, "let print", "print"), kID)

		// A parameter can be repeated, and the later one shadows the earlier,
		// as a second `let` does. Nothing is reported: Ki has no diagnostic for
		// it. Both parameters are variables, each with the span of its name.
		const params = `fn w(x: Int, x: Int) -> Int {
  x
}
`
		p = testsupport.Analyze(t, params)
		wantCodes(t, p)
		wID, _ := fnNamed(t, p, "w")
		first, second := paramLocal(t, p, "w", 0), paramLocal(t, p, "w", 1)
		if first == second {
			t.Fatalf("both parameters bind variable %d, want two variables", first)
		}
		wantLocal(t, p, "x in the body", useAt(t, p, "x", 0), second, "x", spanIn(t, p, params, "x: Int) ->", "x"), wID)
		wantLocal(t, p, "the first x", sema.Res{Kind: sema.ResLocal, Local: first}, first, "x", spanIn(t, p, params, "w(x: Int,", "x"), wID)
		if got := len(userLocals(p)); got != 2 {
			t.Errorf("the function binds %d variables, want 2", got)
		}
	})

	t.Run("block_scope_ends", func(t *testing.T) {
		// A variable lives to the end of its block: `y` after the block is
		// unknown.
		const src = "fn f() {\n  { let y = 1 }\n  y\n}\n"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p, "E0301")
		checkUnknown(t, userDiags(t, p, 1)[0], "y", spanIn(t, p, src, "\n  y\n", "y"))
		wantUnknown(t, p, "y", 0)

		// The branches of an `if` are blocks, each with a scope of its own: the
		// `else` does not see the `then`'s variable, nor does the code after.
		const branches = "fn g(c: Bool) {\n  if c { let z = 1 } else { z }\n  z\n}\n"
		p = testsupport.Analyze(t, branches)
		wantCodes(t, p, "E0301", "E0301")
		diags := userDiags(t, p, 2)
		checkUnknown(t, diags[0], "z", spanIn(t, p, branches, "else { z }", "z"))
		checkUnknown(t, diags[1], "z", spanIn(t, p, branches, "}\n  z\n", "z"))
		wantUnknown(t, p, "z", 0)
		wantUnknown(t, p, "z", 1)

		// The body of a function is a block too: its variables are not seen by
		// the next function.
		const across = "fn a() { let q = 1 }\nfn b() { q }\n"
		p = testsupport.Analyze(t, across)
		wantCodes(t, p, "E0301")
		checkUnknown(t, userDiags(t, p, 1)[0], "q", spanIn(t, p, across, "b() { q }", "q"))
	})

	t.Run("unknown_name_E0301", func(t *testing.T) {
		// `missing` is far from every name that exists (main, print), so there
		// is nothing to suggest and the help line stays empty.
		const src = "fn main() {\n  missing\n}\n"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p, "E0301")
		d := userDiags(t, p, 1)[0]
		checkUnknown(t, d, "missing", spanIn(t, p, src, "  missing", "missing"))
		if d.Help != "" || len(d.Fixits) != 0 {
			t.Errorf("help %q and fixits %+v, want none", d.Help, d.Fixits)
		}
		wantUnknown(t, p, "missing", 0)
		if locals := userLocals(p); len(locals) != 0 {
			t.Errorf("an unknown name made variables %+v, want none", locals)
		}

		// Every unknown name is reported, wherever it is: a callee, an argument
		// and an operand. The call is not otherwise wrong.
		const several = "fn main() {\n  nope(nada, 1 + nil)\n}\n"
		p = testsupport.Analyze(t, several)
		wantCodes(t, p, "E0301", "E0301", "E0301")
		diags := userDiags(t, p, 3)
		checkUnknown(t, diags[0], "nope", spanIn(t, p, several, "nope(", "nope"))
		checkUnknown(t, diags[1], "nada", spanIn(t, p, several, "nada,", "nada"))
		checkUnknown(t, diags[2], "nil", spanIn(t, p, several, "+ nil", "nil"))
		for _, name := range []string{"nope", "nada", "nil"} {
			wantUnknown(t, p, name, 0)
		}
	})

	t.Run("forward_reference_to_later_fn", func(t *testing.T) {
		// `b` is defined after the function that calls it.
		p := testsupport.Analyze(t, "fn a() { b() }\nfn b() {}")
		wantCodes(t, p)
		bID, _ := fnNamed(t, p, "b")
		if res, want := useAt(t, p, "b", 0), (sema.Res{Kind: sema.ResDef, Def: bID}); res != want {
			t.Errorf("b stands for %+v, want the function %+v of the file", res, want)
		}

		// A forward reference from inside blocks and after variables: the name
		// is not in any scope, so the search goes on to the definitions. `n` and
		// `v` are variables, and `d` and `c` (the function itself) are functions.
		const src = `fn c(n: Int) {
  let v = d(n)
  { c(v) }
}
fn d(n: Int) {}
`
		p = testsupport.Analyze(t, src)
		wantCodes(t, p)
		cID, _ := fnNamed(t, p, "c")
		dID, _ := fnNamed(t, p, "d")
		if res, want := useAt(t, p, "d", 0), (sema.Res{Kind: sema.ResDef, Def: dID}); res != want {
			t.Errorf("d stands for %+v, want %+v", res, want)
		}
		if res, want := useAt(t, p, "c", 0), (sema.Res{Kind: sema.ResDef, Def: cID}); res != want {
			t.Errorf("c stands for %+v, want %+v", res, want)
		}
		wantLocal(t, p, "n", useAt(t, p, "n", 0), paramLocal(t, p, "c", 0), "n", spanIn(t, p, src, "c(n:", "n"), cID)
		wantLocal(t, p, "v", useAt(t, p, "v", 0), letNamed(t, p, "v", 0), "v", spanIn(t, p, src, "let v", "v"), cID)
	})
}
