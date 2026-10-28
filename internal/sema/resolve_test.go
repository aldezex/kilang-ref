package sema_test

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/driver"
	"github.com/aldezex/kilang/internal/sema"
	"github.com/aldezex/kilang/internal/syntax"
	"github.com/aldezex/kilang/internal/testsupport"
)

// nameExprs returns the ids of the ExprName nodes of m whose name is name, in
// id order.
func nameExprs(m *syntax.Module, in *base.Interner, name string) []syntax.ExprID {
	var ids []syntax.ExprID
	for id, e := range m.Exprs.All() {
		if e.Kind == syntax.ExprName && in.String(e.Name) == name {
			ids = append(ids, id)
		}
	}
	return ids
}

// resAt returns what the n-th ExprName of module mod called name, counting from
// 0 in id order, stands for. It fails the test if there are not n+1 of them.
func resAt(t *testing.T, rp *sema.ResolvedProgram, mods []*syntax.Module, in *base.Interner, mod sema.ModuleIdx, name string, n int) sema.Res {
	t.Helper()
	ids := nameExprs(mods[mod], in, name)
	if n >= len(ids) {
		t.Fatalf("module %d has %d names called %q, want at least %d", mod, len(ids), name, n+1)
	}
	return rp.Modules[mod].ExprRes[ids[n]]
}

// onlyRes returns the resolution of the one ExprName of module mod called name.
func onlyRes(t *testing.T, rp *sema.ResolvedProgram, mods []*syntax.Module, in *base.Interner, mod sema.ModuleIdx, name string) sema.Res {
	t.Helper()
	if ids := nameExprs(mods[mod], in, name); len(ids) != 1 {
		t.Fatalf("module %d has %d names called %q, want exactly 1", mod, len(ids), name)
	}
	return resAt(t, rp, mods, in, mod, name, 0)
}

// userRes is onlyRes for the user's file of a program made by Analyze.
func userRes(t *testing.T, p *testsupport.Program, name string) sema.Res {
	t.Helper()
	return onlyRes(t, p.Frontend.Resolved, p.Frontend.Modules, p.Session.Interner, p.Frontend.User, name)
}

// defsOf returns the definitions of module mod, in id order.
func defsOf(rp *sema.ResolvedProgram, mod sema.ModuleIdx) []sema.Def {
	var defs []sema.Def
	for _, d := range rp.Defs.All() {
		if d.Module == mod {
			defs = append(defs, d)
		}
	}
	return defs
}

// wantCodes fails the test unless the errors Analyze reported have exactly
// these codes, in this order.
func wantCodes(t *testing.T, p *testsupport.Program, want ...string) {
	t.Helper()
	if got := testsupport.ErrorCodes(p); !slices.Equal(got, want) {
		t.Errorf("error codes %v, want %v; diagnostics %+v", got, want, p.Session.Sink.All())
	}
}

// resolveSources lexes and parses prelude and user as module 0 and module 1
// and resolves them, the way Session.Analyze does for the modules ParseSource
// makes, but with a prelude that the test writes. It is for the rules that the
// real prelude, which has one function, cannot show.
func resolveSources(t *testing.T, prelude, user string) (*sema.ResolvedProgram, []*syntax.Module, *base.Interner, *diag.Sink) {
	t.Helper()
	var sm base.SourceManager
	in := base.NewInterner()
	sink := &diag.Sink{}
	var mods []*syntax.Module
	for i, src := range []string{prelude, user} {
		id := sm.Add([]string{"prelude.ki", "user.ki"}[i], []byte(src))
		tb := syntax.Lex(&sm, id, in, sink)
		mods = append(mods, syntax.Parse(&tb, id, in, sink))
	}
	if sink.HasErrors() {
		t.Fatalf("the sources have syntax errors: %+v", sink.All())
	}
	return sema.Resolve(mods, 1, in, sink), mods, in, sink
}

func TestResolve(t *testing.T) {
	t.Run("collects_top_level_fns", func(t *testing.T) {
		// The prelude's `print` is a definition too, the first of all, so the
		// two of the user's file are definitions 1 and 2 of the program.
		const src = "fn a() {}\npub fn b(x: Int) {}\n"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		rp, m, in := p.Frontend.Resolved, p.Frontend.Modules[p.Frontend.User], p.Session.Interner
		if got := rp.Defs.Len(); got != 3 {
			t.Fatalf("the program has %d definitions, want 3 (the prelude's print and the two of the file)", got)
		}
		want := []sema.Def{
			{Name: in.InternString("a"), Kind: sema.DefFn, Module: 1, Item: m.Items[0], Span: base.Span{File: m.File, Lo: 3, Hi: 4}},
			{Name: in.InternString("b"), Kind: sema.DefFn, Module: 1, Item: m.Items[1], Span: base.Span{File: m.File, Lo: 17, Hi: 18}, Pub: true},
		}
		if got := defsOf(rp, 1); !slices.Equal(got, want) {
			t.Errorf("definitions of module 1:\n%+v\nwant\n%+v", got, want)
		}
		// Definitions are numbered in source order, after those of module 0.
		if def := rp.Defs.Get(0); in.String(def.Name) != "print" || def.Module != 0 || def.Kind != sema.DefExternFn {
			t.Errorf("definition 0 is %+v, want the extern print of module 0", def)
		}
		if got, wantDef := rp.Defs.Get(1), want[0]; got != wantDef {
			t.Errorf("definition 1 is %+v, want %+v", got, wantDef)
		}
		if got, wantDef := rp.Defs.Get(2), want[1]; got != wantDef {
			t.Errorf("definition 2 is %+v, want %+v", got, wantDef)
		}
	})

	t.Run("prelude_names_visible", func(t *testing.T) {
		p := testsupport.Analyze(t, "fn main() { print(1) }")
		wantCodes(t, p)
		res := userRes(t, p, "print")
		if res.Kind != sema.ResDef {
			t.Fatalf("print resolved to %+v, want a definition", res)
		}
		def := p.Frontend.Resolved.Defs.Get(res.Def)
		if def.Kind != sema.DefExternFn || def.ExternName != "ki_print_int" || def.Module != 0 || !def.Pub ||
			p.Session.Interner.String(def.Name) != "print" {
			t.Errorf("print resolved to %+v, want the pub extern fn print of module 0 whose extern name is ki_print_int", def)
		}

		// Only what the prelude marks `pub` is seen from the user's file.
		rp, mods, in, _ := resolveSources(t, "fn secret() {}\npub fn open() {}\n", "fn main() {\n  secret\n  open\n}\n")
		if res := onlyRes(t, rp, mods, in, 1, "secret"); res.Kind == sema.ResDef {
			t.Errorf("secret resolved to %+v, want it not to resolve to the prelude's private fn", res)
		}
		res = onlyRes(t, rp, mods, in, 1, "open")
		if res.Kind != sema.ResDef || rp.Defs.Get(res.Def).Module != 0 || in.String(rp.Defs.Get(res.Def).Name) != "open" {
			t.Errorf("open resolved to %+v, want the prelude's open", res)
		}

		// The extern name is the first argument of the first `extern`
		// attribute. (Nothing checks the arguments in Part I: the attribute is
		// only in the standard library, which we write.)
		rp, _, _, _ = resolveSources(t, "#[extern(\"first\", \"second\")]\n#[extern(\"other\")]\npub fn g()\n", "fn main() {}\n")
		if got := defsOf(rp, 0); len(got) != 1 || got[0].Kind != sema.DefExternFn || got[0].ExternName != "first" {
			t.Errorf("definitions of the prelude: %+v, want one extern fn whose extern name is \"first\"", got)
		}

		// An `#[extern]` without arguments is an extern function with an empty
		// extern name, and reading its first argument would index past the end
		// of an empty list.
		rp, _, _, sink := resolveSources(t, "#[extern]\npub fn h()\n", "fn main() {}\n")
		if got := defsOf(rp, 0); len(got) != 1 || got[0].Kind != sema.DefExternFn || got[0].ExternName != "" {
			t.Errorf("definitions of a prelude with a bare #[extern]: %+v, want one extern fn with no extern name", got)
		}
		if diags := sink.All(); len(diags) != 0 {
			t.Errorf("a prelude with a bare #[extern] reported %+v, want nothing", diags)
		}
	})

	t.Run("duplicate_definition_E0302", func(t *testing.T) {
		// The second definition is the mistake and the first is where to look.
		// `f` is at byte 3 of the first line and at byte 13 of the second.
		const src = "fn f() {}\nfn f() {}\nfn main() { f() }\n"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p, "E0302")
		file := p.Frontend.Modules[p.Frontend.User].File
		first := base.Span{File: file, Lo: 3, Hi: 4}
		second := base.Span{File: file, Lo: 13, Hi: 14}
		d := p.Session.Sink.All()[0]
		if d.Message != "`f` is defined more than once" {
			t.Errorf("message %q, want %q", d.Message, "`f` is defined more than once")
		}
		if want := (diag.Label{Span: second, Message: "redefined here"}); d.Primary != want {
			t.Errorf("primary label %+v, want %+v", d.Primary, want)
		}
		if want := []diag.Label{{Span: first, Message: "first defined here"}}; !slices.Equal(d.Secondary, want) {
			t.Errorf("secondary labels %+v, want %+v", d.Secondary, want)
		}
		if len(d.Notes) != 0 || d.Help != "" {
			t.Errorf("notes %q and help %q, want none", d.Notes, d.Help)
		}
		// Both definitions are definitions of the program (and `main` a third
		// of the file), and a use of the name finds the first, which is the
		// one that the error points back to.
		if got := len(defsOf(p.Frontend.Resolved, 1)); got != 3 {
			t.Errorf("the file has %d definitions, want 3", got)
		}
		res := userRes(t, p, "f")
		if res.Kind != sema.ResDef || p.Frontend.Resolved.Defs.Get(res.Def).Span != first {
			t.Errorf("f resolved to %+v, want the definition at %+v", res, first)
		}

		// A third definition is another mistake, and it too points back to the
		// first, not to the second.
		const three = "fn f() {}\nfn f() {}\nfn f() {}\n"
		p = testsupport.Analyze(t, three)
		wantCodes(t, p, "E0302", "E0302")
		diags := p.Session.Sink.All()
		third := base.Span{File: file, Lo: 23, Hi: 24}
		if diags[0].Primary.Span != second || diags[1].Primary.Span != third {
			t.Errorf("primary spans %+v and %+v, want %+v and %+v", diags[0].Primary.Span, diags[1].Primary.Span, second, third)
		}
		for i, d := range diags {
			if len(d.Secondary) != 1 || d.Secondary[0].Span != first {
				t.Errorf("diagnostic %d: secondary labels %+v, want one at %+v", i, d.Secondary, first)
			}
		}

		// The errors come in the order they were found, which is the order of
		// the functions, and not sorted by code.
		p = testsupport.Analyze(t, "#[inline]\nfn f() {}\nfn f() {}\n")
		wantCodes(t, p, "E0305", "E0302")
	})

	t.Run("extern_only_in_std_E0304", func(t *testing.T) {
		// The attribute spans bytes [0,17). The function has no body, which
		// the parser accepts because of the attribute, so the only mistake is
		// that the attribute is in the user's file. (The prelude declares an
		// extern function of its own, and every Analyze loads it: the other
		// tests would report this error if the standard library were refused.)
		const src = "#[extern(\"ki_x\")]\nfn f()\n"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p, "E0304")
		d := p.Session.Sink.All()[0]
		file := p.Frontend.Modules[p.Frontend.User].File
		if d.Message != "`#[extern]` is only allowed in the standard library" {
			t.Errorf("message %q", d.Message)
		}
		if want := (diag.Label{Span: base.Span{File: file, Lo: 0, Hi: 17}, Message: "remove the attribute"}); d.Primary != want {
			t.Errorf("primary label %+v, want %+v", d.Primary, want)
		}
		if len(d.Secondary) != 0 || len(d.Notes) != 0 || d.Help != "" {
			t.Errorf("secondary labels %+v, notes %q, help %q, want none", d.Secondary, d.Notes, d.Help)
		}
		// The attribute is refused, so the function is a plain one, with no
		// extern name: only the standard library has extern functions.
		if got := defsOf(p.Frontend.Resolved, 1); len(got) != 1 || got[0].Kind != sema.DefFn || got[0].ExternName != "" {
			t.Errorf("definitions of the file: %+v, want one plain fn with no extern name", got)
		}

		// A function with a body gets the same error, and the function is
		// still there to be called.
		p = testsupport.Analyze(t, "#[extern(\"ki_x\")] fn g() {}\nfn main() { g() }\n")
		wantCodes(t, p, "E0304")
		res := userRes(t, p, "g")
		if res.Kind != sema.ResDef {
			t.Fatalf("g resolved to %+v, want a definition", res)
		}
		if def := p.Frontend.Resolved.Defs.Get(res.Def); def.Kind != sema.DefFn || def.ExternName != "" {
			t.Errorf("g resolved to %+v, want a plain fn with no extern name", def)
		}
	})

	t.Run("unknown_attribute_E0305", func(t *testing.T) {
		const src = "#[inline] fn f() {}\n"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p, "E0305")
		d := p.Session.Sink.All()[0]
		file := p.Frontend.Modules[p.Frontend.User].File
		if d.Message != "unknown attribute `inline`" {
			t.Errorf("message %q", d.Message)
		}
		if want := (diag.Label{Span: base.Span{File: file, Lo: 0, Hi: 9}, Message: "the only attribute in Part I is `#[extern]`"}); d.Primary != want {
			t.Errorf("primary label %+v, want %+v", d.Primary, want)
		}
		if len(d.Secondary) != 0 || len(d.Notes) != 0 || d.Help != "" {
			t.Errorf("secondary labels %+v, notes %q, help %q, want none", d.Secondary, d.Notes, d.Help)
		}

		// Every attribute of the function is looked at, each with its name and
		// its own span: `#[a]` is [0,4) and `#[b]` is [5,9).
		p = testsupport.Analyze(t, "#[a] #[b] fn f() {}\n")
		wantCodes(t, p, "E0305", "E0305")
		diags := p.Session.Sink.All()
		if diags[0].Message != "unknown attribute `a`" || diags[0].Primary.Span.Lo != 0 || diags[0].Primary.Span.Hi != 4 ||
			diags[1].Message != "unknown attribute `b`" || diags[1].Primary.Span.Lo != 5 || diags[1].Primary.Span.Hi != 9 {
			t.Errorf("diagnostics %+v, want `a` at [0,4) and `b` at [5,9)", diags)
		}

		// The rule holds in every module, the standard library's included.
		_, _, _, sink := resolveSources(t, "#[inline] pub fn g() {}\n", "fn main() {}\n")
		if got := sink.Count(diag.E(305)); got != 1 || len(sink.All()) != 1 {
			t.Errorf("a prelude with #[inline]: %d E0305 among %+v, want exactly one diagnostic", got, sink.All())
		}
	})

	t.Run("user_print_shadows_prelude", func(t *testing.T) {
		// The two `print` live in different modules, so there is no E0302, and
		// a name in the user's file finds the file's own definition first.
		p := testsupport.Analyze(t, "fn print(x: Int) {}\nfn main() { print(1) }\n")
		wantCodes(t, p)
		res := userRes(t, p, "print")
		if res.Kind != sema.ResDef {
			t.Fatalf("print resolved to %+v, want a definition", res)
		}
		def := p.Frontend.Resolved.Defs.Get(res.Def)
		if def.Module != 1 || def.Kind != sema.DefFn || def.ExternName != "" {
			t.Errorf("print resolved to %+v, want the fn of module 1", def)
		}
		// The prelude's print is still a definition of the program.
		if got := defsOf(p.Frontend.Resolved, 0); len(got) != 1 || got[0].Kind != sema.DefExternFn ||
			p.Session.Interner.String(got[0].Name) != "print" {
			t.Errorf("definitions of module 0: %+v, want the extern print", got)
		}
	})

	t.Run("main_found", func(t *testing.T) {
		// `main` is not the first definition of the program nor of the file.
		p := testsupport.Analyze(t, "fn other() {}\nfn main() {}\n")
		wantCodes(t, p)
		rp := p.Frontend.Resolved
		if !rp.HasMain {
			t.Fatal("HasMain is false, want true")
		}
		def := rp.Defs.Get(rp.Main)
		if p.Session.Interner.String(def.Name) != "main" || def.Module != 1 {
			t.Errorf("Main is %+v, want the main of module 1", def)
		}

		// Only the user's file can give the program its `main`: one in the
		// prelude is not the program's, and does not hide the user's.
		rp, _, _, _ = resolveSources(t, "pub fn main() {}\n", "fn f() {}\n")
		if rp.HasMain {
			t.Errorf("HasMain is true with a main only in the prelude, want false")
		}
		rp, _, _, _ = resolveSources(t, "pub fn main() {}\n", "fn main() {}\n")
		if !rp.HasMain || rp.Defs.Get(rp.Main).Module != 1 {
			t.Errorf("with a main in both, Main is %+v (HasMain %v), want the one of module 1", rp.Defs.Get(rp.Main), rp.HasMain)
		}
	})

	t.Run("missing_main_is_not_a_resolve_error", func(t *testing.T) {
		for _, src := range []string{"fn f() {}\n", ""} {
			p := testsupport.Analyze(t, src)
			if p.Frontend.Resolved.HasMain {
				t.Errorf("%q: HasMain is true, want false", src)
			}
			if diags := p.Session.Sink.All(); len(diags) != 0 {
				t.Errorf("%q: diagnostics %+v, want none", src, diags)
			}
		}
	})

	t.Run("names_resolve_in_every_expression", func(t *testing.T) {
		// Every kind of expression that has a child is here, and a name in
		// each place a child can be: the right-hand side of `let`, the callee
		// and the argument of a call, the operand of a prefix operator, both
		// sides of a binary operator, the condition and the three branches of
		// an if, a block that is a statement, and the tail. Also a name that
		// is defined further down, the function itself and a function that
		// comes before. There are 21 names, and each is the name of a
		// definition: print of module 0 and the rest of module 1.
		const src = `fn helper() { print }
fn main() {
  let a = print
  let b: Int = print
  print(print)
  -print
  !print
  print + print
  print && print
  if print { print } else if print { print } else { print }
  { print }
  later
  main
  helper
}
fn later() { helper }
`
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		rp, m, in := p.Frontend.Resolved, p.Frontend.Modules[p.Frontend.User], p.Session.Interner

		// Resolve reads the trees and never changes them: after it, each module
		// prints as the parser made it. A second parse of the same text, which
		// no pass has touched, is the reference.
		fresh := driver.NewSession(strings.NewReader(""), io.Discard, io.Discard)
		parsed := fresh.ParseSource("test.ki", []byte(src))
		for i, pm := range parsed.Modules {
			want := syntax.PrintModule(pm, fresh.Interner)
			if i == int(p.Frontend.User) && want == "" {
				t.Fatalf("the file prints as nothing, so the comparison below proves nothing")
			}
			if got := syntax.PrintModule(p.Frontend.Modules[i], in); got != want {
				t.Errorf("module %d prints after Resolve as\n%s\nwant what the parser made:\n%s", i, got, want)
			}
		}

		count := 0
		for id, e := range m.Exprs.All() {
			if e.Kind != syntax.ExprName {
				continue
			}
			count++
			name := in.String(e.Name)
			res := rp.Modules[p.Frontend.User].ExprRes[id]
			if res.Kind != sema.ResDef {
				t.Errorf("name %q at %+v resolved to %+v, want a definition", name, e.Span, res)
				continue
			}
			def := rp.Defs.Get(res.Def)
			wantModule := sema.ModuleIdx(1)
			if name == "print" {
				wantModule = 0
			}
			if in.String(def.Name) != name || def.Module != wantModule {
				t.Errorf("name %q at %+v resolved to %+v, want the definition of %q in module %d", name, e.Span, def, name, wantModule)
			}
		}
		if count != 21 {
			t.Errorf("the file has %d names, want 21", count)
		}
	})

	t.Run("tables_are_indexed_by_id", func(t *testing.T) {
		// The tables of each module have a row for every expression, every
		// statement and every item of it, the prelude's too.
		p := testsupport.Analyze(t, "fn f(x: Int) -> Int {\n  let y = x\n  y\n}\n#[extern(\"a\")] fn g()\n")
		rp := p.Frontend.Resolved
		if len(rp.Modules) != 2 {
			t.Fatalf("%d modules resolved, want 2", len(rp.Modules))
		}
		for i, m := range p.Frontend.Modules {
			res := rp.Modules[i]
			if len(res.ExprRes) != m.Exprs.Len() || len(res.LetLocal) != m.Stmts.Len() || len(res.ParamLocals) != m.ItemNodes.Len() {
				t.Errorf("module %d: tables of %d, %d and %d rows, want %d expressions, %d statements and %d items",
					i, len(res.ExprRes), len(res.LetLocal), len(res.ParamLocals), m.Exprs.Len(), m.Stmts.Len(), m.ItemNodes.Len())
			}
		}
		if m := p.Frontend.Modules[1]; m.Exprs.Len() == 0 || m.Stmts.Len() == 0 || m.ItemNodes.Len() != 2 {
			t.Errorf("the file has %d expressions, %d statements and %d items: the check above proves nothing", m.Exprs.Len(), m.Stmts.Len(), m.ItemNodes.Len())
		}
	})
}
