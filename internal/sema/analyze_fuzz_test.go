package sema_test

import (
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/testsupport"
)

// FuzzAnalyze feeds arbitrary source to every pass that Analyze runs after the
// parser (name resolution, and the passes that come after it) and checks what
// the renderers rely on: nothing panics, every span of every diagnostic (the
// labels and the fix-its) lies within its file, and every diagnostic can be
// rendered in the three formats. RenderHuman panics for a fix-it that crosses a
// line break or that is in another file; FuzzParse checks that the lexer and the
// parser never build one, and this target does the same for the fix-its that the
// later passes build: E0301 replaces a name, E0304 and E0305 delete an
// attribute or replace its name, E0404 replaces the parameters of `main` with
// `()` and deletes its return clause, E0406 replaces a type name, E0705 replaces a
// constructor pattern, E0410 writes a `..` in a record pattern and W0701 deletes an
// arm, each with a span that has to stay on one line. E0404 offers its edits only
// when each piece it edits is on one line, E0705 and E0410 only for a pattern on one
// line, and W0701 only for an arm on one line.
//
// The seeds are the ones that reach those fix-its, among them an attribute that
// runs over three lines, where deleting it would cross line breaks, a `main`
// whose parameters or return clause run over several lines, which E0404 must
// not edit, and one with the expressions that have no names inside (`true`,
// `()`) and an `if` with no `else`, which the walk of name resolution has to go
// past: a kind it forgets is a panic. `go test` runs only the seeds. To search for more, run for a while with
// `go test ./internal/sema -run=^$ -fuzz=FuzzAnalyze -fuzztime=10s`; an input
// that breaks a property is saved under testdata/fuzz/FuzzAnalyze and becomes a
// seed.
func FuzzAnalyze(f *testing.F) {
	f.Add("fn main() {}")
	// E0301 with its suggestion and its edit, and with the `let` that cannot
	// see itself.
	f.Add("fn fib(n: Int) -> Int { fibb(n) }")
	f.Add("fn main() { let x = x }")
	// E0302.
	f.Add("fn f() {}\nfn f() {}")
	// E0304 with a body and an attribute on one line (the edit deletes it), on
	// three lines (help only) and on a function with no body (help only); and
	// E0305, which has the same edit, for the same three functions.
	f.Add("#[extern(\"k\")] fn f() {}")
	f.Add("#[extern(\n\"k\"\n)]\nfn f() {}")
	f.Add("#[extern(\"k\")] fn f()")
	f.Add("#[inline] fn f() {}")
	f.Add("#[inline(\n\"x\"\n)]\nfn f() {}")
	f.Add("#[inline] fn f()")
	// A function whose signature is wrong keeps its name.
	f.Add("fn add(a Int) -> Int { a }\nfn main() { add(1) }")
	// The leaves with no name inside, and an `if` with no `else`.
	f.Add("fn main() { if true { () } }")
	// E0404 on a `main` whose parameters run over three lines and one whose
	// return clause runs over two (help only: an edit that crossed a line break
	// would make RenderHuman panic), and E0406 with its edit on a misspelled
	// type.
	f.Add("fn main(\n  x: Int\n) -> Int { x }")
	f.Add("fn main() ->\n  Int { 1 }")
	f.Add("fn f(x: Bol) {}")
	// Types and constructors: E0302 for a type and for a constructor, E0406 with
	// its edit on a declared type, the attributes on a type (E0304 and E0305
	// delete one on a single line), and a type that the parser could not read,
	// whose name and constructors are used afterwards.
	f.Add("type S { A }\ntype S { A }\nfn f(s: Ss) { A }")
	f.Add("#[extern(\"k\")] type T { A }\n#[inline] type U = Int")
	f.Add("type S { A(, B }\nfn f(s: S) { A\n B }")
	// Matches and `let`s with patterns, which name resolution and the type checker
	// read by their scrutinee and their initializer only, and a Float pattern (E0706)
	// that the parser reports.
	f.Add("fn f(x: Int) -> Int { match x { 0 | 1 => 2\n n if n > 1 => n\n _ => 0 }\n}\nfn g() { let (a, b) = (1, 2)\n let _ = zzz }")
	f.Add("fn f(x: Float) -> Int { match x { 1.5 => 1\n Some( => 2\n [a, ..] => 3 } }")
	// The edits of E0705 (a constructor pattern replaced by one with a wildcard for
	// each field) and of E0410 (a `..` written in a record pattern), on one line and
	// over several, where there is no edit; the other diagnostics of patterns that
	// have none (E0703, E0704, E0306, E0409 with a pun, E0411) and a list pattern.
	f.Add("type S { A(Int), B, C(Int, Int) }\ntype P { x: Int, y: Int }\nfn f(s: S) -> Int {\n  match s {\n    A(a, b) => 1\n    B(x) | C(x, _) => 2\n    C(\n      a\n    ) => 3\n    D(q) => 4\n    [a, ..t] => 5\n  }\n}")
	f.Add("type P { x: Int, y: Int }\nfn f(p: P) -> Int {\n  match p {\n    P { x } => 1\n    P { x, } => 2\n    P { } => 3\n    P {\n      x\n    } => 4\n    P { xx, .. } => 5\n    P { x: a, x: b, .. } => 6\n    P { x: a, y: a } => 7\n  }\n}")
	// The pass that comes after Check: E0701 and E0702 with their witnesses (a missing
	// constructor with fields, an Int, a tuple, a record), and W0701 with the edit
	// that deletes the arm, for an arm on a line of its own, one among others on a
	// line, one over several lines (help only) and one that has a guard, and an
	// or-pattern that is all taken.
	f.Add("type S { A(Int), B, C(Int, Int) }\ntype P { x: Int, y: Int }\nfn f(s: S, p: P, t: (Int, Bool), n: Int) -> Int {\n  let (a, 0) = (n, n)\n  let B = s\n  match s { A(_) => 1 }\n  match t { (a, true) => 1 }\n  match p { P { x: 0, .. } => 1 }\n  match n { 0 => 1\n 1 => 2 }\n}")
	f.Add("fn f(n: Int) -> Int {\n  match n {\n    _ => 1\n    2 => 2\n    k if k > 0 => 3\n    4 => {\n      5\n    }\n  }\n}\nfn g(n: Int) -> Int { match n { 0 => 1; _ => 2; 0 => 3; 4 => 5 } }")
	f.Add("type S { A, B }\nfn f(s: S) -> Int { match s { A | B => 1\n B | A => 2 } }")
	f.Fuzz(func(t *testing.T, src string) {
		p := testsupport.Analyze(t, src)
		sm := &p.Session.Sources
		// A span is inside its file when it starts no later than it ends and
		// ends no later than the last byte. Size panics for a file that the
		// session does not hold, which fails the property too.
		check := func(what string, i int, sp base.Span) {
			t.Helper()
			if sp.Lo > sp.Hi || sp.Hi > sm.Size(sp.File) {
				t.Fatalf("%q: %s of diagnostic %d has span %+v, not within [0,%d] of file %d", src, what, i, sp, sm.Size(sp.File), sp.File)
			}
		}
		for i, d := range p.Session.Sink.All() {
			check("primary label", i, d.Primary.Span)
			for _, l := range d.Secondary {
				check("secondary label", i, l.Span)
			}
			for _, fx := range d.Fixits {
				check("fix-it", i, fx.Span)
			}
			// None of them panics.
			_ = diag.RenderHuman(d, sm)
			_ = diag.RenderShort(d, sm)
			_ = diag.RenderJSON(d, sm)
		}
	})
}
