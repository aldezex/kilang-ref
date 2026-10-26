// bookx: not-in-book

package main

import (
	"slices"
	"testing"
)

func TestFindDirectives(t *testing.T) {
	t.Run("listing", func(t *testing.T) {
		src := lines(
			"Un párrafo.",
			"",
			"{{listing ch05-s4 lexer.line_rule}}",
			"",
			"Otro párrafo.",
		)
		got, errs := findDirectives(src)
		checkErrs(t, errs)
		want := []Directive{{Kind: DirListing, Tag: "ch05-s4", Target: "lexer.line_rule", Line: 3}}
		if !slices.Equal(got, want) {
			t.Errorf("directives = %+v, want %+v", got, want)
		}
	})

	t.Run("file", func(t *testing.T) {
		got, errs := findDirectives(lines("{{file ch01-s3 .github/workflows/ci.yml}}"))
		checkErrs(t, errs)
		want := []Directive{{Kind: DirFile, Tag: "ch01-s3", Target: ".github/workflows/ci.yml", Line: 1}}
		if !slices.Equal(got, want) {
			t.Errorf("directives = %+v, want %+v", got, want)
		}
	})

	t.Run("diff_range", func(t *testing.T) {
		got, errs := findDirectives(lines("", "{{diff ch06-s1..ch06-s2 internal/syntax/parser.go}}"))
		checkErrs(t, errs)
		want := []Directive{{
			Kind: DirDiff, Tag: "ch06-s1", TagTo: "ch06-s2",
			Target: "internal/syntax/parser.go", Line: 2,
		}}
		if !slices.Equal(got, want) {
			t.Errorf("directives = %+v, want %+v", got, want)
		}
	})

	t.Run("cards_and_reftest", func(t *testing.T) {
		got, errs := findDirectives(lines("{{cards}}", "", "{{reftest T05.07}}"))
		checkErrs(t, errs)
		want := []Directive{
			{Kind: DirCards, Line: 1},
			{Kind: DirRefTest, Target: "T05.07", Line: 3},
		}
		if !slices.Equal(got, want) {
			t.Errorf("directives = %+v, want %+v", got, want)
		}
	})

	t.Run("unknown_is_error", func(t *testing.T) {
		got, errs := findDirectives(lines("a", "b", "{{foo x}}"))
		checkErrs(t, errs, ":3: unknown directive `foo`: want file, diff, listing, cards or reftest")
		if len(got) != 0 {
			t.Errorf("got %d directives, want 0", len(got))
		}
	})

	t.Run("several_on_one_line_and_crlf", func(t *testing.T) {
		src := []byte("{{cards}} y {{reftest T01.02}}\r\n\r\n{{file ch01-s1 go.mod}}")
		got, errs := findDirectives(src)
		checkErrs(t, errs)
		want := []Directive{
			{Kind: DirCards, Line: 1},
			{Kind: DirRefTest, Target: "T01.02", Line: 1},
			{Kind: DirFile, Tag: "ch01-s1", Target: "go.mod", Line: 3},
		}
		if !slices.Equal(got, want) {
			t.Errorf("directives = %+v, want %+v", got, want)
		}
	})

	t.Run("spaces_inside_braces", func(t *testing.T) {
		got, errs := findDirectives(lines("{{  listing   ch05-s4\tlexer.line_rule  }}"))
		checkErrs(t, errs)
		want := []Directive{{Kind: DirListing, Tag: "ch05-s4", Target: "lexer.line_rule", Line: 1}}
		if !slices.Equal(got, want) {
			t.Errorf("directives = %+v, want %+v", got, want)
		}
	})

	t.Run("ignores_fences_and_code_spans", func(t *testing.T) {
		src := lines(
			"```",                                  // 1
			"{{cards}}",                            // 2: inside a fence
			"```",                                  // 3
			"usa `{{cards}}` o ``{{cards}}`` aquí", // 4: inside code spans
			"~~~md",                                // 5
			"{{file a b}}",                         // 6: inside a fence
			"~~~",                                  // 7
			"````",                                 // 8
			"```",                                  // 9: shorter than the opening, it does not close
			"{{cards}}",                            // 10: still inside
			"````",                                 // 11
			"~~~",                                  // 12
			"```",                                  // 13: another character, it does not close a ~~~ fence
			"{{cards}}",                            // 14: still inside
			"~~~",                                  // 15
			"```",                                  // 16
			"    ```",                              // 17: indented by four spaces, it does not close
			"{{cards}}",                            // 18: still inside
			"```",                                  // 19
			"{{reftest T01.01}}",                   // 20: the only real one
		)
		got, errs := findDirectives(src)
		checkErrs(t, errs)
		want := []Directive{{Kind: DirRefTest, Target: "T01.01", Line: 20}}
		if !slices.Equal(got, want) {
			t.Errorf("directives = %+v, want %+v", got, want)
		}
	})

	t.Run("code_span_that_never_closes", func(t *testing.T) {
		got, errs := findDirectives(lines("una comilla ` suelta {{cards}}"))
		checkErrs(t, errs)
		want := []Directive{{Kind: DirCards, Line: 1}}
		if !slices.Equal(got, want) {
			t.Errorf("directives = %+v, want %+v", got, want)
		}
	})

	t.Run("bad_arguments", func(t *testing.T) {
		const idHelp = "write it as TNN.MM, as in `T05.07`"
		rows := []struct{ src, want string }{
			{"{{file ch01-s1}}", ":1: bad arguments for `file`: want {{file TAG PATH}}"},
			{"{{file ch01-s1 a b}}", ":1: bad arguments for `file`: want {{file TAG PATH}}"},
			{"{{listing ch01-s1}}", ":1: bad arguments for `listing`: want {{listing TAG REGION}}"},
			{"{{diff ch01-s1 a.go}}", ":1: bad arguments for `diff`: want {{diff TAG..TAG PATH}}"},
			{"{{diff ch01-s1..ch01-s2}}", ":1: bad arguments for `diff`: want {{diff TAG..TAG PATH}}"},
			{"{{diff ..ch01-s2 a.go}}", ":1: bad arguments for `diff`: want {{diff TAG..TAG PATH}}"},
			{"{{diff ch01-s1.. a.go}}", ":1: bad arguments for `diff`: want {{diff TAG..TAG PATH}}"},
			{"{{diff a..b..c x.go}}", ":1: bad arguments for `diff`: want {{diff TAG..TAG PATH}}"},
			{"{{diff a...b x.go}}", ":1: bad arguments for `diff`: want {{diff TAG..TAG PATH}}"},
			{"{{diff a....b x.go}}", ":1: bad arguments for `diff`: want {{diff TAG..TAG PATH}}"},
			{"{{diff a..b x.go y}}", ":1: bad arguments for `diff`: want {{diff TAG..TAG PATH}}"},
			{"{{listing ch01-s1 a.b c}}", ":1: bad arguments for `listing`: want {{listing TAG REGION}}"},
			{"{{cards now}}", ":1: bad arguments for `cards`: want {{cards}}"},
			{"{{reftest}}", ":1: bad arguments for `reftest`: want {{reftest TNN.MM}}"},
			{"{{reftest T05.07 T05.08}}", ":1: bad arguments for `reftest`: want {{reftest TNN.MM}}"},
			{"{{reftest T5.7}}", ":1: invalid card id `T5.7`: " + idHelp},
			{"{{listing ch01-s1 ab}}", ":1: invalid region id `ab`: write lowercase words joined by dots, as in `lexer.line_rule`"},
			{"{{file ch01;rm x}}", ":1: invalid tag `ch01;rm`: only letters, digits, `.`, `_`, `-` and `/` are allowed"},
			{"{{file ch01-s1 a$b}}", ":1: invalid path `a$b`: only letters, digits, `.`, `_`, `-` and `/` are allowed"},
			{"{{diff a;b..c x.go}}", ":1: invalid tag `a;b`: only letters, digits, `.`, `_`, `-` and `/` are allowed"},
			{"{{diff a..c;d x.go}}", ":1: invalid tag `c;d`: only letters, digits, `.`, `_`, `-` and `/` are allowed"},
			{"{{file --help go.mod}}", ":1: invalid tag `--help`: it cannot start with `-`, git would read it as an option"},
			{"{{file ch01-s1 -x}}", ":1: invalid path `-x`: it cannot start with `-`, git would read it as an option"},
			{"{{listing -x a.b}}", ":1: invalid tag `-x`: it cannot start with `-`, git would read it as an option"},
			{"{{diff -x..y x.go}}", ":1: invalid tag `-x`: it cannot start with `-`, git would read it as an option"},
			{"{{diff a..-y x.go}}", ":1: invalid tag `-y`: it cannot start with `-`, git would read it as an option"},
			{"{{diff a..b -x.go}}", ":1: invalid path `-x.go`: it cannot start with `-`, git would read it as an option"},
			{"{{file ch01 ../../etc/passwd}}", ":1: invalid path `../../etc/passwd`: it has to be relative to the repository, with no `..` component"},
			{"{{file ch01 a/../b}}", ":1: invalid path `a/../b`: it has to be relative to the repository, with no `..` component"},
			{"{{file ch01 ..}}", ":1: invalid path `..`: it has to be relative to the repository, with no `..` component"},
			{"{{file ch01 /etc/passwd}}", ":1: invalid path `/etc/passwd`: it has to be relative to the repository, with no `..` component"},
			{"{{diff a..b ../x.go}}", ":1: invalid path `../x.go`: it has to be relative to the repository, with no `..` component"},
			{"{{}}", ":1: empty directive"},
			{"{{  }}", ":1: empty directive"},
			{"{{cards", ":1: unterminated directive: missing `}}`"},
		}
		for _, r := range rows {
			got, errs := findDirectives(lines(r.src))
			if g := errStrings(errs); !slices.Equal(g, []string{r.want}) {
				t.Errorf("%s: errors = %q, want %q", r.src, g, []string{r.want})
			}
			if len(got) != 0 {
				t.Errorf("%s: got %d directives, want 0", r.src, len(got))
			}
		}
	})

	t.Run("dots_inside_names_are_fine", func(t *testing.T) {
		// Only a whole `..` component climbs out; dots inside a name are just dots.
		got, errs := findDirectives(lines("{{file ch01-s1 a..b/.github/..c}}", "{{diff v1.0..v1.1 d/e..f.go}}"))
		checkErrs(t, errs)
		want := []Directive{
			{Kind: DirFile, Tag: "ch01-s1", Target: "a..b/.github/..c", Line: 1},
			{Kind: DirDiff, Tag: "v1.0", TagTo: "v1.1", Target: "d/e..f.go", Line: 2},
		}
		if !slices.Equal(got, want) {
			t.Errorf("directives = %+v, want %+v", got, want)
		}
	})

	t.Run("errors_do_not_hide_good_directives", func(t *testing.T) {
		got, errs := findDirectives(lines("{{foo}}", "{{cards}}", "{{bar}}"))
		checkErrs(t, errs,
			":1: unknown directive `foo`: want file, diff, listing, cards or reftest",
			":3: unknown directive `bar`: want file, diff, listing, cards or reftest",
		)
		want := []Directive{{Kind: DirCards, Line: 2}}
		if !slices.Equal(got, want) {
			t.Errorf("directives = %+v, want %+v", got, want)
		}
	})
}
