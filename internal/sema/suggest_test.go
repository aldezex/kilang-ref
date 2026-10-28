package sema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/testsupport"
)

// didYouMean is the help line that suggests the name best.
func didYouMean(best string) string { return "did you mean `" + best + "`?" }

// wantSuggestion fails the test unless sink holds exactly one diagnostic, an
// E0301 for the name unknown as it is written in src (file is the file src
// came from) and whose help line is help, "" meaning no help at all. context is
// a piece of src that occurs exactly once and has unknown in it: the test says
// where the name is by quoting the text around it. row says which row of the
// test is checking.
func wantSuggestion(t *testing.T, row string, sink *diag.Sink, file base.FileID, src, context, unknown, help string) {
	t.Helper()
	if n := strings.Count(src, context); n != 1 {
		t.Fatalf("%s: %q occurs %d times in %q, want exactly 1", row, context, n, src)
	}
	diags := sink.All()
	if len(diags) != 1 {
		t.Errorf("%s: %d diagnostics %+v, want exactly 1", row, len(diags), diags)
		return
	}
	d := diags[0]
	lo := strings.Index(src, context) + strings.Index(context, unknown)
	checkUnknown(t, d, unknown, base.Span{File: file, Lo: uint32(lo), Hi: uint32(lo + len(unknown))})
	if d.Help != help {
		t.Errorf("%s: help %q, want %q", row, d.Help, help)
	}
	// A suggestion comes with the edit that follows it, over the name that is
	// written and with the name that is suggested; no suggestion, no edit.
	var wantFixits []diag.FixIt
	if help != "" {
		best := strings.TrimSuffix(strings.TrimPrefix(help, "did you mean `"), "`?")
		wantFixits = []diag.FixIt{diag.Replace(base.Span{File: file, Lo: uint32(lo), Hi: uint32(lo + len(unknown))}, best)}
	}
	if !slices.Equal(d.Fixits, wantFixits) {
		t.Errorf("%s: fixits %+v, want %+v", row, d.Fixits, wantFixits)
	}
}

// suggestionIn analyzes src together with the real prelude and checks it with
// wantSuggestion.
func suggestionIn(t *testing.T, row, src, context, unknown, help string) {
	t.Helper()
	p := testsupport.Analyze(t, src)
	wantSuggestion(t, row, &p.Session.Sink, userFile(p), src, context, unknown, help)
}

// suggestionWith is suggestionIn for a prelude that the test writes.
func suggestionWith(t *testing.T, row, prelude, src, context, unknown, help string) {
	t.Helper()
	_, mods, _, sink := resolveSources(t, prelude, src)
	wantSuggestion(t, row, sink, mods[1].File, src, context, unknown, help)
}

func TestSuggestions(t *testing.T) {
	t.Run("suggests_closest_name", func(t *testing.T) {
		// The program of the golden file: `fibb` for `fib`, called from the
		// body of `fib` itself.
		const fib = "fn fib(n: Int) -> Int {\n  if n < 2 { n } else { fibb(n - 1) + fib(n - 2) }\n}\nfn main() {\n  print(fib(20))\n}\n"
		suggestionIn(t, "fibb", fib, "{ fibb(", "fibb", didYouMean("fib"))

		// A function defined further down the file.
		suggestionIn(t, "forward", "fn main() {\n  helpr()\n}\nfn helper() {}\n", "  helpr()", "helpr", didYouMean("helper"))

		// Variables are candidates too: a parameter, a `let` of the same block,
		// and the variables of the blocks around the one the name is in.
		suggestionIn(t, "parameter", "fn f(counter: Int) -> Int {\n  countr\n}\n", "  countr", "countr", didYouMean("counter"))
		suggestionIn(t, "let", "fn f() -> Int {\n  let total = 1\n  totl\n}\n", "  totl", "totl", didYouMean("total"))
		suggestionIn(t, "parameter from a block", "fn f(width: Int) {\n  let value = 1\n  {\n    widt\n  }\n}\n", "    widt", "widt", didYouMean("width"))
		suggestionIn(t, "let from two blocks", "fn f(width: Int) {\n  let value = 1\n  {\n    {\n      valu\n    }\n  }\n}\n", "      valu", "valu", didYouMean("value"))

		// Variables and functions compete on distance alone, and a tie goes to
		// the name that comes first in the alphabet, whichever of the two it is.
		suggestionIn(t, "function first in the alphabet", "fn gamma() {}\nfn f(gammb: Int) {\n  gammc\n}\n", "  gammc", "gammc", didYouMean("gamma"))
		suggestionIn(t, "variable first in the alphabet", "fn gammb() {}\nfn f(gamma: Int) {\n  gammc\n}\n", "  gammc", "gammc", didYouMean("gamma"))

		// The candidates are the definitions of the module being walked, whichever
		// module it is: a name that is unknown inside the prelude finds the
		// prelude's own functions (private ones too), not the user's.
		const prelude = "fn helper() {}\nfn broken() { helpr() }\n"
		_, mods, _, sink := resolveSources(t, prelude, "fn main() {}\n")
		wantSuggestion(t, "unknown name in the prelude", sink, mods[0].File, prelude, "{ helpr()", "helpr", didYouMean("helper"))
	})

	t.Run("suggestion_comes_with_fixit", func(t *testing.T) {
		// The fix-it replaces the unknown name, all of it, by the suggested one,
		// and nothing else. The names are written out here and not taken from
		// the help line, which is what wantSuggestion does.
		const fib = "fn fib(n: Int) -> Int {\n  if n < 2 { n } else { fibb(n - 1) + fib(n - 2) }\n}\nfn main() {\n  print(fib(20))\n}\n"
		p := testsupport.Analyze(t, fib)
		d := p.Session.Sink.All()[0]
		at := uint32(strings.Index(fib, "fibb"))
		if want := []diag.FixIt{diag.Replace(base.Span{File: userFile(p), Lo: at, Hi: at + 4}, "fib")}; !slices.Equal(d.Fixits, want) {
			t.Errorf("fibb: fixits %+v, want %+v", d.Fixits, want)
		}

		// A shorter name is replaced by a longer one, and a name in another
		// place of the line works the same.
		const short = "fn main() {\n  prnt(1)\n}\n"
		p = testsupport.Analyze(t, short)
		d = p.Session.Sink.All()[0]
		at = uint32(strings.Index(short, "prnt"))
		if want := []diag.FixIt{diag.Replace(base.Span{File: userFile(p), Lo: at, Hi: at + 4}, "print")}; !slices.Equal(d.Fixits, want) {
			t.Errorf("prnt: fixits %+v, want %+v", d.Fixits, want)
		}

		// Nothing to suggest, nothing to replace.
		p = testsupport.Analyze(t, "fn main() {\n  zzzzzz\n}\n")
		if d = p.Session.Sink.All()[0]; len(d.Fixits) != 0 || d.Help != "" {
			t.Errorf("zzzzzz: help %q and fixits %+v, want none", d.Help, d.Fixits)
		}
	})

	t.Run("suggests_prelude_name", func(t *testing.T) {
		// `print` is not in the file: it is the prelude's.
		suggestionIn(t, "print", "fn main() {\n  prnt(1)\n}\n", "  prnt(1)", "prnt", didYouMean("print"))

		// A prelude written for the test, with a name that is `pub`.
		suggestionWith(t, "pub name", "pub fn visible() {}\n", "fn main() {\n  visibl()\n}\n", "  visibl()", "visibl", didYouMean("visible"))
	})

	t.Run("no_suggestion_when_far", func(t *testing.T) {
		// `zzzzzz` is far from every name there is (main, print).
		suggestionIn(t, "far from everything", "fn main() {\n  zzzzzz\n}\n", "  zzzzzz", "zzzzzz", "")

		// Close to a variable that the name cannot see: one whose block has
		// ended, one that is bound further down, and one of another function.
		suggestionIn(t, "block ended", "fn f() {\n  { let apple = 1 }\n  appl\n}\n", "  appl", "appl", "")
		suggestionIn(t, "not yet bound", "fn f() {\n  appl\n  let apple = 1\n}\n", "  appl", "appl", "")
		suggestionIn(t, "other function", "fn a(count: Int) {}\nfn b() {\n  coun\n}\n", "  coun", "coun", "")

		// Close to a name of the prelude that is not `pub`: the program cannot
		// use it, so it cannot mean it.
		suggestionWith(t, "private prelude name", "fn secrett() {}\n", "fn main() {\n  secret()\n}\n", "  secret()", "secret", "")

		// A name that the prelude defines twice stands for its first definition
		// (the second is E0302, in the prelude). Here the first is private and the
		// second is `pub`: the program cannot use either, so `dupl` has nothing
		// to suggest.
		_, mods, _, sink := resolveSources(t, "fn dupe() {}\npub fn dupe() {}\n", "fn main() {\n  dupl()\n}\n")
		var unknown []diag.Diagnostic
		for _, d := range sink.All() {
			if d.Code == diag.E(301) {
				unknown = append(unknown, d)
			}
		}
		if len(unknown) != 1 {
			t.Fatalf("repeated private definition: %d unknown names in %+v, want 1", len(unknown), sink.All())
		}
		if unknown[0].Help != "" || unknown[0].Primary.Span.File != mods[1].File {
			t.Errorf("repeated private definition: E0301 %+v, want one in the user's file with no help", unknown[0])
		}
	})
}
