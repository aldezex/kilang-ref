package syntax_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// FuzzParse feeds the parser the tokens of arbitrary bytes and checks what
// every later pass relies on: Parse does not panic, every span it produces lies
// within the file, and PrintModule can walk the tree it built. The spans are
// those of the tree (items, attributes, parameters, types, statements and
// expressions, with their name and operator spans) and those of the labels of
// the diagnostics, fix-its included, and every diagnostic can be rendered in
// the three formats: RenderHuman panics for a fix-it that crosses a line break
// or that is in another file, and no input may make the lexer or the parser
// build one. (FuzzAnalyze, in internal/sema, does the same for the passes after
// the parser.) PrintModule follows every id and every list range from the
// items down, so it panics on an id that names no node or on a range that runs
// past the end of its pool; the span checks walk each pool by itself and would
// not notice either. The text PrintModule returns is not examined here.
//
// A parser whose loops did not consume a token would not return, and the
// fuzzer is not what finds that: a run that hangs stalls instead of failing.
// The timeout of `go test` does (try -timeout 20s on
// TestRecover/unclosed_brace_E0202 with such a mutant).
//
// `go test` runs only the seeds. To search for more, run for a while with
// `go test ./internal/syntax -run=^$ -fuzz=FuzzParse -fuzztime=10s`; an input
// that breaks a property is saved under testdata/fuzz/FuzzParse and becomes a
// seed.
func FuzzParse(f *testing.F) {
	f.Add("fn main() {}")
	f.Add("fn f() { (1 }")
	f.Add("fn f() -> Int { 1 + }")
	// The text of the golden test of `ki emit ast`, so that the seeds include a
	// program that parses cleanly with every kind of node in it.
	golden, err := os.ReadFile(filepath.Join("..", "..", "tests", "golden", "ch06", "fib_ast.ki"))
	if err != nil {
		f.Fatalf("cannot read the golden file that seeds the corpus: %v", err)
	}
	f.Add(string(golden))
	f.Fuzz(func(t *testing.T, src string) {
		// The file is not the first of its SourceManager (see lexFile), so a
		// span that lost its File does not pass for one that is within the file.
		var sm base.SourceManager
		sm.Add("pad.ki", nil)
		id := sm.Add("fuzz.ki", []byte(src))
		in := base.NewInterner()
		sink := &diag.Sink{}
		tb := syntax.Lex(&sm, id, in, sink)
		m := syntax.Parse(&tb, id, in, sink)

		// A panic inside PrintModule fails the property; its result is not examined.
		_ = syntax.PrintModule(m, in)

		// A span is inside the file when it belongs to it, starts no later than
		// it ends and ends no later than the last byte. A node without a name or
		// an operator leaves that span zero, which is not a span at all.
		check := func(what string, i int, sp base.Span) {
			t.Helper()
			if sp == (base.Span{}) {
				return
			}
			if sp.File != id || sp.Lo > sp.Hi || int(sp.Hi) > len(src) {
				t.Fatalf("%q: %s %d has span %+v, not within [0,%d] of file %d", src, what, i, sp, len(src), id)
			}
		}
		for i, it := range m.ItemNodes.All() {
			check("item", int(i), it.Span)
			check("item name", int(i), it.NameSpan)
		}
		for i, a := range m.AttrPool {
			check("attribute", i, a.Span)
		}
		for i, p := range m.ParamPool {
			check("parameter", i, p.Span)
		}
		for i, ty := range m.Types.All() {
			check("type", int(i), ty.Span)
		}
		for i, s := range m.Stmts.All() {
			check("statement", int(i), s.Span)
			check("statement name", int(i), s.NameSpan)
		}
		for i, e := range m.Exprs.All() {
			check("expression", int(i), e.Span)
			check("operator", int(i), e.OpSpan)
		}
		for i, d := range sink.All() {
			check("primary label of diagnostic", i, d.Primary.Span)
			for _, l := range d.Secondary {
				check("secondary label of diagnostic", i, l.Span)
			}
			for _, f := range d.Fixits {
				check("fix-it of diagnostic", i, f.Span)
			}
			// None of them panics: a fix-it over a line break or in another
			// file would, and so would a fix-it without a help line.
			_ = diag.RenderHuman(d, &sm)
			_ = diag.RenderShort(d, &sm)
			_ = diag.RenderJSON(d, &sm)
		}
	})
}
