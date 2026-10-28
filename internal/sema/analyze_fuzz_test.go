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
// later passes build: E0301 replaces a name, and E0304 and E0305 delete an
// attribute or replace its name, with a span that has to stay on one line.
//
// The seeds are the ones that reach those fix-its, among them an attribute that
// runs over three lines, where deleting it would cross line breaks, and one
// with the expressions that have no names inside (`true`, `()`) and an `if` with
// no `else`, which the walk of name resolution has to go past: a kind it forgets
// is a panic. `go test` runs only the seeds. To search for more, run for a while with
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
