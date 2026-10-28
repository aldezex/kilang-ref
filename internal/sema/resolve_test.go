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
		if len(d.Notes) != 0 {
			t.Errorf("notes %q, want none", d.Notes)
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
		if want := (diag.Label{Span: base.Span{File: file, Lo: 0, Hi: 17}, Message: "not allowed outside the standard library"}); d.Primary != want {
			t.Errorf("primary label %+v, want %+v", d.Primary, want)
		}
		if len(d.Secondary) != 0 {
			t.Errorf("secondary labels %+v, want none", d.Secondary)
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
		if want := (diag.Label{Span: base.Span{File: file, Lo: 0, Hi: 9}, Message: "not an attribute Ki knows"}); d.Primary != want {
			t.Errorf("primary label %+v, want %+v", d.Primary, want)
		}
		if len(d.Secondary) != 0 {
			t.Errorf("secondary labels %+v, want none", d.Secondary)
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

	t.Run("duplicate_definition_help", func(t *testing.T) {
		// Which of the two stays is not for the compiler to say, so the help
		// names both ways out and there is no fix-it. Every repeat gets it.
		p := testsupport.Analyze(t, "fn f() {}\nfn f() {}\nfn f() {}\n")
		wantCodes(t, p, "E0302", "E0302")
		for i, d := range p.Session.Sink.All() {
			if want := "rename one of them, or delete the second"; d.Help != want {
				t.Errorf("diagnostic %d: help %q, want %q", i, d.Help, want)
			}
			if len(d.Fixits) != 0 {
				t.Errorf("diagnostic %d: fixits %+v, want none", i, d.Fixits)
			}
		}
	})

	t.Run("extern_only_in_std_help_and_fixit", func(t *testing.T) {
		const note = "extern functions are declared by the standard library and implemented by the runtime; a program calls them through the prelude"
		const src = "#[extern(\"ki_x\")] fn g() {}\nfn main() { g() }\n"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p, "E0304")
		d := p.Session.Sink.All()[0]
		file := p.Frontend.Modules[p.Frontend.User].File
		attr := base.Span{File: file, Lo: 0, Hi: 17} // from the `#` to the `]`
		if want := (diag.Label{Span: attr, Message: "not allowed outside the standard library"}); d.Primary != want {
			t.Errorf("primary label %+v, want %+v", d.Primary, want)
		}
		if want := []string{note}; !slices.Equal(d.Notes, want) {
			t.Errorf("notes %q, want %q", d.Notes, want)
		}
		if want := "remove the attribute"; d.Help != want {
			t.Errorf("help %q, want %q", d.Help, want)
		}
		if want := []diag.FixIt{diag.Delete(attr)}; !slices.Equal(d.Fixits, want) {
			t.Errorf("fixits %+v, want %+v", d.Fixits, want)
		}
		// With the attribute gone the program is fine.
		wantCodes(t, testsupport.Analyze(t, src[attr.Hi:]))

		// An attribute that runs over several lines would make a fix-it that
		// no renderer can show, so it has the help and no fix-it, and the
		// diagnostic can be rendered.
		const multi = "#[extern(\n  \"ki_x\"\n)]\nfn g() {}\n"
		p = testsupport.Analyze(t, multi)
		wantCodes(t, p, "E0304")
		d = p.Session.Sink.All()[0]
		if d.Help != "remove the attribute" || len(d.Fixits) != 0 {
			t.Errorf("an attribute on three lines: help %q and fixits %+v, want the help and no fix-it", d.Help, d.Fixits)
		}
		_ = diag.RenderHuman(d, &p.Session.Sources)

		// A function with no body, which the attribute allowed, has no edit
		// either, on one line or on three: with the attribute deleted the
		// function has no body and the parser refuses it (E0201), so the help
		// asks for both and no single edit does it.
		const bodiless = "remove the attribute, and write a body for the function"
		for _, src := range []string{"#[extern(\"ki_x\")]\nfn g()\n", "#[extern(\n  \"ki_x\"\n)]\nfn g()\n"} {
			p = testsupport.Analyze(t, src)
			wantCodes(t, p, "E0304")
			d = p.Session.Sink.All()[0]
			if d.Help != bodiless || len(d.Fixits) != 0 {
				t.Errorf("%q: help %q and fixits %+v, want %q and no fix-it", src, d.Help, d.Fixits, bodiless)
			}
			if want := []string{note}; !slices.Equal(d.Notes, want) {
				t.Errorf("%q: notes %q, want %q", src, d.Notes, want)
			}
			_ = diag.RenderHuman(d, &p.Session.Sources)
		}
		wantCodes(t, testsupport.Analyze(t, "fn g()\n"), "E0201")
	})

	t.Run("unknown_attribute_suggests_extern", func(t *testing.T) {
		const note = "the attributes Ki knows: `extern`"
		const programNote = "a program cannot use attributes yet"
		// name and attr are the spans of the name and of the whole attribute;
		// near says whether the name is close to `extern`. In the standard
		// library a near name gets the help that suggests `extern` and the
		// fix-it that writes it in place of the name, and any other the help to
		// remove the attribute and the fix-it that deletes it. In the user's
		// file `extern` is not allowed (it is E0304), so there is nothing to
		// suggest: every name gets "remove it" and the deletion.
		rows := []struct {
			src        string
			name, attr [2]uint32
			near       bool
		}{
			{"#[extrn] fn f() {}\n", [2]uint32{2, 7}, [2]uint32{0, 8}, true},
			{"#[intern] fn f() {}\n", [2]uint32{2, 8}, [2]uint32{0, 9}, true},
			{"#[extern2] fn f() {}\n", [2]uint32{2, 9}, [2]uint32{0, 10}, true},
			{"#[inline] fn f() {}\n", [2]uint32{2, 8}, [2]uint32{0, 9}, false},
			{"#[ext] fn f() {}\n", [2]uint32{2, 5}, [2]uint32{0, 6}, false},
			{"#[inline(\"a\")] fn f() {}\n", [2]uint32{2, 8}, [2]uint32{0, 14}, false},
			// The name is not next to the `#[`: the fix-it is over the name.
			{"# [  extrn ] fn f() {}\n", [2]uint32{5, 10}, [2]uint32{0, 12}, true},
		}
		for _, row := range rows {
			// The user's file.
			p := testsupport.Analyze(t, row.src)
			wantCodes(t, p, "E0305")
			d := p.Session.Sink.All()[0]
			file := p.Frontend.Modules[p.Frontend.User].File
			attr := base.Span{File: file, Lo: row.attr[0], Hi: row.attr[1]}
			if want := (diag.Label{Span: attr, Message: "not an attribute Ki knows"}); d.Primary != want {
				t.Errorf("user %q: primary label %+v, want %+v", row.src, d.Primary, want)
			}
			if want := []string{programNote}; !slices.Equal(d.Notes, want) {
				t.Errorf("user %q: notes %q, want %q", row.src, d.Notes, want)
			}
			if want := "remove it"; d.Help != want {
				t.Errorf("user %q: help %q, want %q", row.src, d.Help, want)
			}
			if want := []diag.FixIt{diag.Delete(attr)}; !slices.Equal(d.Fixits, want) {
				t.Errorf("user %q: fixits %+v, want %+v", row.src, d.Fixits, want)
			}
			// With the attribute deleted the file is fine.
			if fixed := row.src[:row.attr[0]] + row.src[row.attr[1]:]; len(testsupport.ErrorCodes(testsupport.Analyze(t, fixed))) != 0 {
				t.Errorf("user %q: after the fix-it, %q still has errors", row.src, fixed)
			}

			// The standard library, where the attribute is the only one that
			// the file may write.
			_, mods, _, sink := resolveSources(t, row.src, "fn main() {}\n")
			diags := sink.All()
			if len(diags) != 1 {
				t.Errorf("prelude %q: %d diagnostics, want exactly 1: %+v", row.src, len(diags), diags)
				continue
			}
			d, file = diags[0], mods[0].File
			attr = base.Span{File: file, Lo: row.attr[0], Hi: row.attr[1]}
			name := base.Span{File: file, Lo: row.name[0], Hi: row.name[1]}
			if want := (diag.Label{Span: attr, Message: "not an attribute Ki knows"}); d.Primary != want {
				t.Errorf("prelude %q: primary label %+v, want %+v", row.src, d.Primary, want)
			}
			if want := []string{note}; !slices.Equal(d.Notes, want) {
				t.Errorf("prelude %q: notes %q, want %q", row.src, d.Notes, want)
			}
			help, want := "remove it", diag.Delete(attr)
			if row.near {
				help, want = "did you mean `extern`?", diag.Replace(name, "extern")
			}
			if d.Help != help {
				t.Errorf("prelude %q: help %q, want %q", row.src, d.Help, help)
			}
			if len(d.Fixits) != 1 || d.Fixits[0] != want {
				t.Errorf("prelude %q: fixits %+v, want [%+v]", row.src, d.Fixits, want)
			}
			// With the name replaced, or the attribute deleted, the standard
			// library is fine.
			fixed := row.src[:row.attr[0]] + row.src[row.attr[1]:]
			if row.near {
				fixed = row.src[:row.name[0]] + "extern" + row.src[row.name[1]:]
			}
			if _, _, _, after := resolveSources(t, fixed, "fn main() {}\n"); len(after.All()) != 0 {
				t.Errorf("prelude %q: after the fix-it, %q has diagnostics: %+v", row.src, fixed, after.All())
			}
		}

		// An attribute over several lines cannot be deleted by an edit that
		// stays on a line, so in the user's file it has the help and no fix-it.
		// In the standard library the name, which is on one line, can be
		// replaced. All of them render.
		const multiInline = "#[inline(\n  \"a\"\n)]\nfn f() {}\n"
		p := testsupport.Analyze(t, multiInline)
		wantCodes(t, p, "E0305")
		d := p.Session.Sink.All()[0]
		if d.Help != "remove it" || len(d.Fixits) != 0 {
			t.Errorf("an attribute on three lines: help %q and fixits %+v, want the help and no fix-it", d.Help, d.Fixits)
		}
		_ = diag.RenderHuman(d, &p.Session.Sources)
		const multiExtrn = "#[extrn(\n  \"a\"\n)]\nfn f() {}\n"
		p = testsupport.Analyze(t, multiExtrn)
		wantCodes(t, p, "E0305")
		d = p.Session.Sink.All()[0]
		if d.Help != "remove it" || len(d.Fixits) != 0 {
			t.Errorf("a misspelled attribute on three lines, in the user's file: help %q and fixits %+v, want the help and no fix-it", d.Help, d.Fixits)
		}
		_ = diag.RenderHuman(d, &p.Session.Sources)
		_, mods, _, sink := resolveSources(t, multiExtrn, "fn main() {}\n")
		d = sink.All()[0]
		file := mods[0].File
		if want := []diag.FixIt{diag.Replace(base.Span{File: file, Lo: 2, Hi: 7}, "extern")}; d.Help != "did you mean `extern`?" || !slices.Equal(d.Fixits, want) {
			t.Errorf("a misspelled attribute on three lines, in the standard library: help %q and fixits %+v, want the suggestion and %+v", d.Help, d.Fixits, want)
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
		// definition: print of module 0 and the rest of module 1. The kinds
		// that have no child, and an if with no else, are in the next test.
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

	t.Run("leaves_and_if_without_else", func(t *testing.T) {
		// `true`, `false` and `()` have no name inside and an `if` with no
		// `else` has no branch to walk: the walk has to go past all of them,
		// and not into an `else` that is not there. An `if` without `else`
		// has the zero id in its Else field, which is the first expression of
		// the file; here that is the `p` of `a`, and `b` has no `p`, so a walk
		// that goes into that Else resolves `p` again inside `b`, where it is
		// not a name. The body of the `if` is `()` because an `if` with no `else`
		// is worth `()`, so the source is also a program that type checks.
		const src = "fn a(p: Int) -> Int { p }\nfn b(c: Bool) {\n  if c { () }\n  false\n  true\n  ()\n}\n"
		p := testsupport.Analyze(t, src)
		wantCodes(t, p)
		if diags := p.Session.Sink.All(); len(diags) != 0 {
			t.Errorf("diagnostics %+v, want none", diags)
		}
		aID, _ := fnNamed(t, p, "a")
		bID, _ := fnNamed(t, p, "b")
		wantLocal(t, p, "the p of a", userRes(t, p, "p"), paramLocal(t, p, "a", 0), "p", spanIn(t, p, src, "p: Int", "p"), aID)
		wantLocal(t, p, "the c of b", userRes(t, p, "c"), paramLocal(t, p, "b", 0), "c", spanIn(t, p, src, "c: Bool", "c"), bID)
	})

	t.Run("broken_signature_keeps_its_name", func(t *testing.T) {
		// A function whose signature the parser cannot read is an error item,
		// but its name was read before the signature went wrong, and the
		// function exists: a call to it is no unknown name, so the mistake in
		// the signature is not repeated at every call. Each row is a file, the
		// codes it gives and what every name called callee stands for.
		rows := []struct {
			name, src string
			codes     []string
			callee    string
			calls     int          // how many names called callee the file has
			kind      sema.ResKind // what each of them stands for
			defs      int          // definitions of the user's file
		}{
			{"forgotten colon, two calls", "fn add(a Int, b Int) -> Int { a + b }\nfn main() {\n  print(add(1, 2))\n  print(add(3, 4))\n}\n",
				[]string{"E0201"}, "add", 2, sema.ResError, 1},
			{"forgotten colon, one call", "fn add(a Int, b Int) -> Int { a + b }\nfn main() { add(1, 2) }\n",
				[]string{"E0201"}, "add", 1, sema.ResError, 1},
			{"missing parenthesis", "fn add -> Int { 1 }\nfn main() { add() }\n",
				[]string{"E0201"}, "add", 1, sema.ResError, 1},
			// The lexer splits `café` at the é, so the name that was read is
			// `caf`, which is also what the call has in front of its é.
			{"letter outside ASCII", "fn caf\u00e9() {}\nfn main() { caf\u00e9() }\n",
				[]string{"E0104", "E0104"}, "caf", 1, sema.ResError, 1},
			// The module's own function, even a broken one, is found before the
			// prelude's: the call is not to the prelude's print.
			{"hides the prelude's", "fn print(x Int) {}\nfn main() { print(1) }\n",
				[]string{"E0201"}, "print", 1, sema.ResError, 1},
			// A definition that was read wins over a broken one of the same
			// name, wherever the two are, and they are not a repeated definition.
			{"broken one first", "fn f(a Int) {}\nfn f() {}\nfn main() { f() }\n",
				[]string{"E0201"}, "f", 1, sema.ResDef, 2},
			{"broken one last", "fn f() {}\nfn f(a Int) {}\nfn main() { f() }\n",
				[]string{"E0201"}, "f", 1, sema.ResDef, 2},
			// A variable still hides it, as it hides any function.
			{"variable of the same name", "fn add(a Int) {}\nfn main() {\n  let add = 1\n  print(add)\n}\n",
				[]string{"E0201"}, "add", 1, sema.ResLocal, 1},
			// Only the name of the broken function is known: the names that
			// nothing defines stay unknown.
			{"another unknown name", "fn add(a Int) {}\nfn main() { sub(1) }\n",
				[]string{"E0201", "E0301"}, "sub", 1, sema.ResError, 1},
		}
		for _, row := range rows {
			p := testsupport.Analyze(t, row.src)
			if got := testsupport.ErrorCodes(p); !slices.Equal(got, row.codes) {
				t.Errorf("%s: error codes %v, want %v; diagnostics %+v", row.name, got, row.codes, p.Session.Sink.All())
			}
			rp, user := p.Frontend.Resolved, p.Frontend.User
			ids := nameExprs(p.Frontend.Modules[user], p.Session.Interner, row.callee)
			if len(ids) != row.calls {
				t.Errorf("%s: %d names called %q, want %d", row.name, len(ids), row.callee, row.calls)
			}
			for _, id := range ids {
				if got := rp.Modules[user].ExprRes[id].Kind; got != row.kind {
					t.Errorf("%s: a name called %q stands for kind %d, want %d", row.name, row.callee, got, row.kind)
				}
			}
			// The broken function is not a definition: it has no signature
			// for anything to use.
			if got := len(defsOf(rp, user)); got != row.defs {
				t.Errorf("%s: the file has %d definitions, want %d", row.name, got, row.defs)
			}
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
