// bookx: not-in-book

package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// goTestEvents writes events of `go test -json` in the shape parseTestList
// reads: one JSON object per line, with the action, the package and, when
// tests is not empty, a line for each test.
func goTestEvents(action, pkg string, tests ...string) string {
	if len(tests) == 0 {
		return fmt.Sprintf("{\"Action\":%q,\"Package\":%q}\n", action, pkg)
	}
	var sb strings.Builder
	for _, test := range tests {
		fmt.Fprintf(&sb, "{\"Action\":%q,\"Package\":%q,\"Test\":%q}\n", action, pkg, test)
	}
	return sb.String()
}

// checkList fails unless the list parseTestList builds from input is exactly want.
func checkList(t *testing.T, input string, want ...string) {
	t.Helper()
	got, err := parseTestList([]byte(input))
	if err != nil {
		t.Fatalf("parseTestList: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("parseTestList =\n  %q\nwant\n  %q", got, want)
	}
}

func TestParseTestList(t *testing.T) {
	t.Run("pass_events_only", func(t *testing.T) {
		// A real run: TestPass passes, TestFail fails, TestSkip is skipped,
		// and TestGroup has one subtest that passes and one that fails (so the
		// group itself fails). The events come in the order the tests ran, and
		// the list is in byte order, which puts TestGroup/ok first.
		sample, err := os.ReadFile("testdata/testlist/pass_fail_skip.json")
		if err != nil {
			t.Fatal(err)
		}
		// A package that passes reports "pass" with no test: it is not a test.
		input := string(sample) + goTestEvents("pass", "example.com/m/q")
		checkList(t, input,
			"example.com/m/p TestGroup/ok",
			"example.com/m/p TestPass",
		)

		if _, err := parseTestList([]byte("this is not json\n")); err == nil {
			t.Error("parseTestList accepted text that is not JSON")
		}
		checkList(t, "")
	})

	t.Run("drops_parents_and_bookx", func(t *testing.T) {
		const syntax = "github.com/aldezex/kilang/internal/syntax"
		const other = "github.com/aldezex/kilang/internal/base"
		input := goTestEvents("pass", syntax,
			"TestLexer", "TestLexer/a", // a parent and its subtest
			"TestScope", "TestScopes/a", // "TestScope/" is not a prefix of "TestScopes/a"
			"TestDeep", "TestDeep/x", "TestDeep/x/y", // every level above the leaf goes
			"TestAlone",
		) +
			goTestEvents("pass", other, "TestLexer") + // same name, another package: no subtest there
			goTestEvents("pass", bookxPackage, "TestScanRegions", "TestScanRegions/full", "TestGit")
		checkList(t, input,
			other+" TestLexer",
			syntax+" TestAlone",
			syntax+" TestDeep/x/y",
			syntax+" TestLexer/a",
			syntax+" TestScope",
			syntax+" TestScopes/a",
		)
	})

	t.Run("collapses_fuzz_keeps_example", func(t *testing.T) {
		const syntax = "github.com/aldezex/kilang/internal/syntax"
		const base = "github.com/aldezex/kilang/internal/base"
		input := goTestEvents("pass", syntax,
			"FuzzLex/seed#0", "FuzzLex/seed#1", "FuzzLex", "FuzzLex/1f3a9c07b2d4e5f6", // seeds, a corpus file and the target
			"FuzzParse/seed#0", // a target whose own event is missing is still one test
			"Fuzzy", "Fuzzy/a", // not a fuzz target: the name after "Fuzz" starts in lowercase
		) + goTestEvents("pass", base, "ExampleInterner", "FuzzLex/seed#0")
		checkList(t, input,
			base+" ExampleInterner",
			base+" FuzzLex",
			syntax+" FuzzLex",
			syntax+" FuzzParse",
			syntax+" Fuzzy/a",
		)
	})
}

func TestParseVerifiedTags(t *testing.T) {
	t.Run("format", func(t *testing.T) {
		const sha = "0123456789abcdef0123456789abcdef01234567"
		good := "ch01-s1 " + sha + " 2026-10-26 go1.27.3\n"
		got, err := parseVerifiedTags([]byte(good))
		want := []VerifiedTag{{Tag: "ch01-s1", SHA: sha, Date: "2026-10-26", GoVersion: "go1.27.3"}}
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("one well-formed line: got %v, %v; want %v", got, err, want)
		}

		// Several lines keep the order of the file; blank lines and CRLF are fine.
		two := "ch01-s2 " + sha + " 2026-10-27 go1.27.4\r\n\r\nch01 " + sha + " 2026-10-28 go1.28rc1\r\n"
		got, err = parseVerifiedTags([]byte(two))
		if err != nil || len(got) != 2 || got[0].Tag != "ch01-s2" || got[1] != (VerifiedTag{"ch01", sha, "2026-10-28", "go1.28rc1"}) {
			t.Errorf("two lines: got %v, %v", got, err)
		}

		if got, err := parseVerifiedTags(nil); err != nil || len(got) != 0 {
			t.Errorf("an empty file: got %v, %v; want no tags and no error", got, err)
		}

		bad := []struct{ name, text, want string }{
			{"39-char sha", "ch01-s1 " + sha[:39] + " 2026-10-26 go1.27.3\n", "line 1: `" + sha[:39] + "` is not a 40-character lowercase hexadecimal SHA"},
			{"41-char sha", "ch01-s1 " + sha + "0 2026-10-26 go1.27.3\n", "line 1: `" + sha + "0` is not a 40-character lowercase hexadecimal SHA"},
			{"uppercase sha", "ch01-s1 " + strings.ToUpper(sha) + " 2026-10-26 go1.27.3\n", "line 1: `" + strings.ToUpper(sha) + "` is not a 40-character lowercase hexadecimal SHA"},
			{"three fields", "ch01-s1 " + sha + " 2026-10-26\n", "line 1: want `<tag> <sha40> <YYYY-MM-DD> <goversion>`, found 3 fields"},
			{"five fields", "ch01-s1 " + sha + " 2026-10-26 go1.27.3 extra\n", "line 1: want `<tag> <sha40> <YYYY-MM-DD> <goversion>`, found 5 fields"},
			{"double space", "ch01-s1  " + sha + " 2026-10-26 go1.27.3\n", "line 1: want `<tag> <sha40> <YYYY-MM-DD> <goversion>`, found 5 fields"},
			{"month 13", "ch01-s1 " + sha + " 2026-13-01 go1.27.3\n", "line 1: `2026-13-01` is not a date written YYYY-MM-DD"},
			{"one-digit day", "ch01-s1 " + sha + " 2026-10-6 go1.27.3\n", "line 1: `2026-10-6` is not a date written YYYY-MM-DD"},
			{"go version without go", "ch01-s1 " + sha + " 2026-10-26 1.27.3\n", "line 1: `1.27.3` is not a Go version such as go1.27.3"},
			{"bad tag", "ch01;s1 " + sha + " 2026-10-26 go1.27.3\n", "line 1: invalid tag \"ch01;s1\": only letters, digits, `.`, `_`, `-` and `/` are allowed, and it cannot start with `-`"},
			{"error on line 3", good + "ch01-s2 " + sha + " 2026-10-26 go1.27.3\n" + "ch01-s3 " + sha + " 2026-10-26\n", "line 3: want `<tag> <sha40> <YYYY-MM-DD> <goversion>`, found 3 fields"},
			{"repeated tag", good + good, "line 2: tag `ch01-s1` appears twice"},
		}
		for _, c := range bad {
			got, err := parseVerifiedTags([]byte(c.text))
			if err == nil || err.Error() != c.want {
				t.Errorf("%s: got %v, %v; want the error %q", c.name, got, err, c.want)
				continue
			}
			var le *lineError
			if !errors.As(err, &le) || le.Line == 0 {
				t.Errorf("%s: the error should be a *lineError with its line", c.name)
			}
		}
	})
}
