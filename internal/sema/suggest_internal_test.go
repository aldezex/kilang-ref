package sema

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

func TestSuggest(t *testing.T) {
	// want fails the test unless suggest(name, candidates) is (best, ok). Each
	// failure says which row it is, since the tests below are tables.
	want := func(t *testing.T, row, name string, candidates []string, best string, ok bool) {
		t.Helper()
		if got, gotOK := suggest(name, candidates); got != best || gotOK != ok {
			t.Errorf("%s: suggest(%q, %q) = (%q, %v), want (%q, %v)", row, name, candidates, got, gotOK, best, ok)
		}
	}

	t.Run("picks_closest", func(t *testing.T) {
		// The typical case: the names a program has, and one slip.
		want(t, "typo of a function", "fibb", []string{"main", "fib", "print"}, "fib", true)

		// `abcdef` allows two edits. Of three candidates within reach, at
		// distances 1, 2 and 2, the one at distance 1 wins wherever it is in the
		// list: a search that stops at the first name within reach, or that
		// keeps the last, or the farthest, or the alphabetically first of those
		// within reach (`abcdaa`), picks another.
		const name = "abcdef"
		const near, far1, far2 = "abcdez", "abcdaa", "abcdxx"
		want(t, "closest first", name, []string{near, far1, far2}, near, true)
		want(t, "closest in the middle", name, []string{far1, near, far2}, near, true)
		want(t, "closest last", name, []string{far1, far2, near}, near, true)

		// A candidate longer or shorter than the name is measured too.
		want(t, "longer candidate", "counter", []string{"counterss", "counters"}, "counters", true)
		want(t, "shorter candidate", "counter", []string{"coter", "counte"}, "counte", true)
	})

	t.Run("ties_are_alphabetical", func(t *testing.T) {
		// `ac` and `aa` are one edit away from `ab`. The one that comes first in
		// the alphabet wins, whatever the order of the list: a search that keeps
		// the first of a tie, or the last, fails one of these.
		want(t, "ac then aa", "ab", []string{"ac", "aa"}, "aa", true)
		want(t, "aa then ac", "ab", []string{"aa", "ac"}, "aa", true)
		want(t, "ad, aa, ac", "ab", []string{"ad", "aa", "ac"}, "aa", true)
		want(t, "ac, ad, aa", "ab", []string{"ac", "ad", "aa"}, "aa", true)
		want(t, "aa, ad, ac", "ab", []string{"aa", "ad", "ac"}, "aa", true)
		// Longer names, and the same name twice.
		want(t, "two edits away", "abcdef", []string{"abcdzz", "abcdyy", "abcdxx"}, "abcdxx", true)
		want(t, "repeated", "ab", []string{"ac", "ac"}, "ac", true)
		// A tie is only a tie at the smallest distance: a nearer name wins even
		// when it comes later in the alphabet (`abcdaa` is two edits away,
		// `abcdez` one).
		want(t, "nearer but later", "abcdef", []string{"abcdaa", "abcdez"}, "abcdez", true)
		// A name with a single byte allows one edit (see the next test), and
		// every other one-byte name is one edit away.
		want(t, "one byte", "x", []string{"z", "y"}, "y", true)
	})

	t.Run("none_when_far", func(t *testing.T) {
		want(t, "far from all", "zzzzzz", []string{"main", "fib", "print"}, "", false)
		want(t, "no candidates", "fibb", nil, "", false)
		want(t, "empty list", "fibb", []string{}, "", false)
		// Two edits is too many for a name of 4 bytes (it allows one), and a
		// name of one byte allows one edit, not two.
		want(t, "two edits in four bytes", "fibb", []string{"fob", "ffibbbb"}, "", false)
		want(t, "two edits in one byte", "x", []string{"ab"}, "", false)
		// Names that are far are ignored, they do not cancel the near one.
		want(t, "one near among far", "fibb", []string{"zzzzzz", "fib", "qqq"}, "fib", true)
	})

	t.Run("threshold_is_len_over_three", func(t *testing.T) {
		// The most edits that a name of n bytes allows is max(1, n/3) with the
		// division of integers. Each row has a candidate exactly that far and
		// one more edit away, from the `abc…` name of n bytes by changing its
		// first letters to `z`. The comments give n and the threshold.
		rows := []struct {
			name      string
			candidate string
			ok        bool
		}{
			{"a", "z", true},                  // n 1: max(1, 0) = 1, one edit
			{"ab", "zz", false},               // n 2: max(1, 0) = 1, two edits
			{"ab", "zb", true},                // n 2: one edit
			{"abc", "zzc", false},             // n 3: max(1, 1) = 1, two edits
			{"abc", "zbc", true},              // n 3: one edit
			{"abcd", "zbcd", true},            // n 4: 1, one edit
			{"abcd", "zzcd", false},           // n 4: two edits
			{"abcde", "zzcde", false},         // n 5: max(1, 5/3) = 1, two edits
			{"abcde", "zbcde", true},          // n 5: one edit
			{"abcdef", "zzcdef", true},        // n 6: 2, two edits
			{"abcdef", "zzzdef", false},       // n 6: three edits
			{"abcdefg", "zzcdefg", true},      // n 7: 7/3 = 2, two edits
			{"abcdefg", "zzzdefg", false},     // n 7: three edits (rounding up allows them)
			{"abcdefgh", "zzcdefgh", true},    // n 8: 8/3 = 2, two edits
			{"abcdefgh", "zzzdefgh", false},   // n 8: three edits (rounding to nearest allows them)
			{"abcdefghi", "zzzdefghi", true},  // n 9: 3, three edits
			{"abcdefghi", "zzzzefghi", false}, // n 9: four edits
			// The length that counts is the name's, not the candidate's.
			{"abcd", "abcdef", false}, // n 4: 1, and a longer candidate is two edits away
			{"abcdef", "abcd", true},  // n 6: 2, and a shorter candidate is two edits away
			// A candidate whose length differs by exactly the threshold is
			// still measured, on either side (see skips_lengths_out_of_reach).
			{"abcdef", "abcdefgh", true}, // n 6: 2, and a longer candidate is two edits away
			{"a", "ab", true},            // n 1: 1, and a longer candidate is one edit away
			{"ab", "a", true},            // n 2: 1, and a shorter candidate is one edit away
		}
		for _, r := range rows {
			best := ""
			if r.ok {
				best = r.candidate
			}
			want(t, r.name+" to "+r.candidate, r.name, []string{r.candidate}, best, r.ok)
		}

		// `abcdxx` is two edits from `abcdef` and `abcxxx` is three.
		want(t, "two edits in six bytes", "abcdef", []string{"abcdxx"}, "abcdxx", true)
		want(t, "three edits in six bytes", "abcdef", []string{"abcxxx"}, "", false)
		// With both in the list the one in reach is the answer.
		want(t, "both", "abcdef", []string{"abcxxx", "abcdxx"}, "abcdxx", true)
	})

	t.Run("skips_lengths_out_of_reach", func(t *testing.T) {
		// A candidate whose length differs from the name's by more than the
		// threshold cannot be near enough, so suggest does not measure it.
		// EditDistance allocates the two rows of its table and suggest allocates
		// nothing, so a candidate that was skipped costs no allocation at all.
		// (The compiler keeps the rows of a very short candidate on the stack,
		// where they cost nothing either way, so every candidate below has at
		// least 6 bytes, like the one in reach. That floor assumes an 8-byte int:
		// with 4-byte ints the rows of a 6-byte candidate (28 bytes) would still
		// fit the 32-byte stack buffer and the check on the candidate in reach
		// would fail, so on a 32-bit target the floor would have to be higher.)
		allocs := func(name, candidate string) float64 {
			return testing.AllocsPerRun(10, func() { suggest(name, []string{candidate}) })
		}
		// A candidate in reach is measured: without this the zeros below would
		// say nothing, since they would also be the count of a suggest that
		// allocates nothing in any case.
		if got := allocs("abcdef", "abcdxx"); got == 0 {
			t.Fatalf("a candidate in reach cost %v allocations, want some: the rows below prove nothing", got)
		}
		long := strings.Repeat("x", 100)
		for _, r := range []struct{ name, candidate string }{
			{"abcdef", long},            // n 6: 2, and a candidate of 100 bytes
			{long, "abcdef"},            // n 100: 33, and a candidate of 6 bytes
			{"abcdef", "abcdefghi"},     // n 6: 2, and a candidate three bytes longer
			{"a", "abcdefg"},            // n 1: 1, and a candidate six bytes longer
			{"abcdefghij", "abcdef"},    // n 10: 3, and a candidate four bytes shorter
			{"abcdefghijkl", "abcdefg"}, // n 12: 4, and a candidate five bytes shorter
		} {
			if got := allocs(r.name, r.candidate); got != 0 {
				t.Errorf("suggest(%.12q, %.12q) cost %v allocations, want 0: the candidate is out of reach and not measured", r.name, r.candidate, got)
			}
		}
	})

	t.Run("skips_names_over_the_cap", func(t *testing.T) {
		// The distance takes time in the product of the two lengths, so a name
		// longer than maxSuggestLen is not measured at all: it gets no
		// suggestion, however near a candidate is, and the cap is on the name
		// and not on the candidates. A name of exactly the cap is measured.
		// The cap is 128 (the card and the book say so), and the lengths are
		// written as numbers and not as maxSuggestLen: names built from the
		// constant would follow it wherever it went, and the cap would be
		// pinned only in being there. The names are long, so the failures say
		// their lengths.
		at := strings.Repeat("a", 128)
		over := strings.Repeat("a", 129)
		for _, row := range []struct {
			what, name, candidate string
			suggested             bool
		}{
			{"name at the cap", at, at[:len(at)-1] + "b", true},
			{"name over the cap", over, over[:len(over)-1] + "b", false},
			// The candidates may be longer than the cap, or shorter than the
			// name by less than the threshold (a third of the name): only the
			// name counts.
			{"name at the cap, a longer candidate", at, over, true},
			{"name over the cap, a shorter candidate", over, at, false},
		} {
			best, ok := suggest(row.name, []string{row.candidate})
			if ok != row.suggested || (ok && best != row.candidate) {
				t.Errorf("%s: suggest(a name of %d bytes, one candidate of %d bytes) = (a name of %d bytes, %v), want found = %v",
					row.what, len(row.name), len(row.candidate), len(best), ok, row.suggested)
			}
		}
	})

	t.Run("no_suggestions_after_the_error_cap", func(t *testing.T) {
		// A resolver set up by hand, with an empty scope and a user module (module
		// 1) that defines `fib` and 300 other functions. `fibb` is not defined and
		// is one edit from `fib`.
		in := base.NewInterner()
		r := &resolver{in: in, mod: 1, rp: &ResolvedProgram{}, tops: []map[base.Symbol]DefID{{}, {}}, scope: &scope{}}
		define := func(name string) {
			sym := in.InternString(name)
			r.tops[1][sym] = r.rp.Defs.Push(Def{Name: sym, Module: 1})
		}
		for i := range 300 {
			define(fmt.Sprintf("def%03d", i))
		}
		define("fib")
		use := syntax.Expr{Kind: syntax.ExprName, Name: in.InternString("fibb"), Span: base.Span{Lo: 10, Hi: 14}}

		// Every error that the sink keeps carries the help line, the 100th too;
		// the 101st is dropped and the sink says it was truncated.
		r.sink = &diag.Sink{}
		for range diag.MaxErrors + 1 {
			if res := r.name(use); res != (Res{Kind: ResError}) {
				t.Fatalf("fibb stands for %+v, want ResError", res)
			}
		}
		if !r.sink.Truncated() || len(r.sink.All()) != diag.MaxErrors {
			t.Fatalf("the sink kept %d errors (truncated %v) after %d unknown names, want %d and truncated", len(r.sink.All()), r.sink.Truncated(), diag.MaxErrors+1, diag.MaxErrors)
		}
		for i, d := range r.sink.All() {
			if want := "did you mean `fib`?"; d.Help != want {
				t.Errorf("error %d of %d has the help %q, want %q", i+1, diag.MaxErrors, d.Help, want)
				break
			}
		}

		// Once the sink is full the work is not done: the candidates are not
		// gathered, so an unknown name costs a handful of allocations and not
		// one for each candidate. The number of allocations is the same in every
		// run, which a clock would not be.
		cost := func(sink *diag.Sink) float64 {
			r.sink = sink
			return testing.AllocsPerRun(50, func() { r.name(use) })
		}
		fresh := cost(&diag.Sink{})
		full := &diag.Sink{}
		for range diag.MaxErrors + 1 {
			full.Report(diag.NewError(diag.E(301), "unknown name", diag.Label{}))
		}
		capped := cost(full)
		if !full.Truncated() {
			t.Fatal("the sink is not truncated after 101 errors")
		}
		if fresh < 100 {
			t.Fatalf("an unknown name among 301 candidates cost %v allocations with room in the sink, want at least 100: the comparison proves nothing", fresh)
		}
		if capped*10 > fresh {
			t.Errorf("an unknown name cost %v allocations with a full sink and %v with room in it, want at most a tenth", capped, fresh)
		}
	})

	t.Run("candidates_are_sorted_once", func(t *testing.T) {
		// A resolver set up by hand: module 1 is the user's and module 0 the
		// prelude. The names are interned from the last in the alphabet to the
		// first, so that the order of their Symbols is not the alphabetical one.
		in := base.NewInterner()
		for _, name := range []string{"zeta", "yy", "print", "mid", "hidden", "cc", "beta", "alpha"} {
			in.InternString(name)
		}
		sym := in.InternString
		r := &resolver{in: in, mod: 1, rp: &ResolvedProgram{}, tops: []map[base.Symbol]DefID{{}, {}}}
		define := func(mod ModuleIdx, name string, pub bool) {
			id := r.rp.Defs.Push(Def{Name: sym(name), Module: mod, Pub: pub})
			if _, repeated := r.tops[mod][sym(name)]; !repeated {
				r.tops[mod][sym(name)] = id
			}
		}
		define(0, "print", true)
		define(0, "hidden", false)
		define(0, "beta", true)
		define(1, "zeta", false)
		define(1, "alpha", true)
		define(1, "mid", false)
		define(1, "mid", false) // a repeated definition
		// Two scopes: `alpha` is a variable and a definition, and `cc` and `yy`
		// are variables.
		outer := &scope{names: map[base.Symbol]LocalID{sym("yy"): 0, sym("alpha"): 1}}
		r.scope = &scope{parent: outer, names: map[base.Symbol]LocalID{sym("cc"): 2}}

		// The private `hidden` is not a candidate, `alpha` is one candidate
		// and `mid` is too. A Go map is read in a different order each time, so
		// ask again and again.
		want := []string{"alpha", "beta", "cc", "mid", "print", "yy", "zeta"}
		for i := range 50 {
			if got := r.candidates(); !slices.Equal(got, want) {
				t.Fatalf("pass %d: candidates %q, want %q", i, got, want)
			}
		}
	})
}
