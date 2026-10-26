package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// bookxPackage is the package whose tests never go in a chapter list: bookx is
// the author's tool and the book does not teach it.
const bookxPackage = "github.com/aldezex/kilang/cmd/bookx"

// testEvent is the part of an event of `go test -json` that parseTestList reads.
type testEvent struct {
	Action  string
	Package string
	Test    string
}

// parseTestList turns the output of `go test -json ./...` into the list of
// tests of a chapter, book/tests/chNN.list: one line `<import path> <test>`
// for each test that passed, sorted and without repeats.
//
//   - Only the events whose action is "pass" and that name a test count; the
//     package's own "pass" event has no test.
//   - The tests of cmd/bookx are dropped.
//   - FuzzX/seed#0 and FuzzX/<corpus file> become FuzzX: a fuzz target is one
//     test of the book, however many seeds it has.
//   - A test T is dropped when T/ is the start of another test of the same
//     package: a parent is covered by the cards of its subtests. TestScope is
//     kept next to TestScopes/a, because "TestScope/" is not a prefix of
//     "TestScopes/a".
//
// The sort is the byte order of the lines, so it is the same on every machine.
func parseTestList(goTestJSON []byte) ([]string, error) {
	// passed holds the tests of each package, as a set; it is only filled and
	// looked up, and the output is sorted at the end.
	passed := map[string]map[string]bool{}
	dec := json.NewDecoder(bytes.NewReader(goTestJSON))
	for {
		var ev testEvent
		err := dec.Decode(&ev)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("go test -json output: %w", err)
		}
		if ev.Action != "pass" || ev.Test == "" || ev.Package == bookxPackage {
			continue
		}
		name := ev.Test
		if isFuzzTarget(name) {
			name, _, _ = strings.Cut(name, "/")
		}
		if passed[ev.Package] == nil {
			passed[ev.Package] = map[string]bool{}
		}
		passed[ev.Package][name] = true
	}

	var lines []string
	for pkg, tests := range passed {
		parents := map[string]bool{}
		for name := range tests {
			for i := 0; i < len(name); i++ {
				if name[i] == '/' {
					parents[name[:i]] = true
				}
			}
		}
		for name := range tests {
			if !parents[name] {
				lines = append(lines, pkg+" "+name)
			}
		}
	}
	slices.Sort(lines)
	return lines, nil
}

// isFuzzTarget reports whether the name of a test, with or without a subtest
// after it, starts with the name of a fuzz target. Go requires what follows
// "Fuzz" not to start with a lowercase letter, so "Fuzzy/a" is not one.
func isFuzzTarget(name string) bool {
	rest, ok := strings.CutPrefix(name, "Fuzz")
	if !ok {
		return false
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(r)
}
