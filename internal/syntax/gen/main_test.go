package main

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// kindList returns a list of n kinds named K0, K1 and so on, none of them with
// a text, and the kinds that parse should make of it.
func kindList(n int) (list string, kinds []kind) {
	var b strings.Builder
	for i := range n {
		name := "K" + strconv.Itoa(i)
		b.WriteString(name + "\t\n")
		kinds = append(kinds, kind{name, ""})
	}
	return b.String(), kinds
}

func TestGen(t *testing.T) {
	t.Run("parse_checks_the_list", func(t *testing.T) {
		list256, kinds256 := kindList(256)
		list257, _ := kindList(257)
		for _, row := range []struct {
			name string
			list string
			want []kind // the kinds, when the list is good
			err  string // the error message, when it is not
		}{
			// Lists that are right.
			{"one kind", "KwFn\tfn\n", []kind{{"KwFn", "fn"}}, ""},
			{"no line feed at the end", "KwFn\tfn", []kind{{"KwFn", "fn"}}, ""},
			{"in file order", "KwFn\tfn\nLParen\t(\nKwLet\tlet\n", []kind{{"KwFn", "fn"}, {"LParen", "("}, {"KwLet", "let"}}, ""},
			// Only the keywords need a text, and only they must differ in it:
			// Newline and Eof, with none, are in the real list together.
			{"kinds without a text", "Newline\t\nEof\t\n", []kind{{"Newline", ""}, {"Eof", ""}}, ""},
			{"256 kinds", list256, kinds256, ""},

			// Lists that are wrong, with the line they stop at.
			{"empty list", "", nil, "line 1: want `Name<tab>text`, got \"\""},
			{"no tab", "KwFn fn\n", nil, "line 1: want `Name<tab>text`, got \"KwFn fn\""},
			{"no name", "\tfn\n", nil, "line 1: want `Name<tab>text`, got \"\\tfn\""},
			{"blank line", "KwFn\tfn\n\nKwLet\tlet\n", nil, "line 2: want `Name<tab>text`, got \"\""},
			{"blank line at the end", "KwFn\tfn\n\n", nil, "line 2: want `Name<tab>text`, got \"\""},
			{"name twice", "Eq\t=\nEq\t==\n", nil, "line 2: Eq is defined twice"},
			{"keyword without a text", "KwFn\tfn\nKwLet\t\n", nil, "line 2: keyword KwLet has no text"},
			{"keyword with the text of another", "KwFn\tfn\nKwFunc\tfn\n", nil, "line 2: keyword KwFunc has the text \"fn\", which KwFn has already"},
			{"257 kinds", list257, nil, "257 kinds do not fit in a uint8"},
		} {
			got, err := parse(row.list)
			switch {
			case row.err == "" && err != nil:
				t.Errorf("%s: parse failed with %q, want the list to be right", row.name, err)
			case row.err == "" && !slices.Equal(got, row.want):
				t.Errorf("%s: parse returned %v, want %v", row.name, got, row.want)
			case row.err != "" && (err == nil || err.Error() != row.err):
				t.Errorf("%s: parse returned the error %v, want %q", row.name, err, row.err)
			}
		}
	})
}
