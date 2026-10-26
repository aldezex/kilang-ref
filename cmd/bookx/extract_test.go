// bookx: not-in-book

package main

import "testing"

// goSource is the file the extraction tests read. It is built from
// interpreted strings, one per line, because it needs a raw string and a rune
// that hold braces and quotes.
var goSource = []byte(joinLines(
	"package demo",
	"",
	"// TestLexerMore has a name that starts like TestLexer.",
	"func TestLexerMore(t *testing.T) {",
	"\tt.Run(\"pipe_continues_line\", func(t *testing.T) {})",
	"}",
	"",
	"type S struct{}",
	"",
	"func (S) TestLexer(t *testing.T) { /* a method, not the test */ }",
	"",
	"// TestLexer checks the lexer.",
	"func TestLexer(t *testing.T) {",
	"\ts := \"}\" + `{`",
	"\tr := '}'",
	"\tq := '\"'",
	"\t// a comment with a } brace",
	"\t/* and a { block comment */",
	"\tt.Run(\"outer\", func(t *testing.T) {",
	"\t\tt.Run(\"pipe_continues_line\", func(t *testing.T) { t.Log(\"nested\") })",
	"\t})",
	"\tt.Run(\"pipe_continues_line_twice\", func(t *testing.T) {})",
	"\tt.Run(\"pipe_continues_line\", func(t *testing.T) {",
	"\t\tif s != \"}{\" || r != '}' || q != '\"' { // } inside a comment",
	"\t\t\tt.Fatal(\"bad }\")",
	"\t\t}",
	"\t})",
	"\tt.Run(\"after\", func(t *testing.T) {})",
	"}",
	"",
	"func TestOther(t *testing.T) {",
	"\tt.Run(\"pipe_continues_line\", func(t *testing.T) { t.Log(\"other\") })",
	"\tx.Run(\"only_here\", func(t *testing.T) { t.Log(\"not a subtest of t\") })",
	"\tx.t.Run(\"chained\", func(t *testing.T) { t.Log(\"a field called t, not the parameter\") })",
	"\tt.Run(\"only_here\", func(t *testing.T) {})",
	"\tt.Run(`raw_name`, func(t *testing.T) {})",
	"}",
	"",
	"func FuzzLex(f *testing.F) {",
	"\tf.Add(\"{\")",
	"\tf.Fuzz(func(t *testing.T, s string) {})",
	"}",
	"",
	"func ExampleInterner_Intern() {",
	"\tfmt.Println(\"wrong one\")",
	"}",
	"",
	"func ExampleInterner() {",
	"\tfmt.Println(\"}\")",
	"\t// Output: }",
	"}",
	"",
	"func BenchmarkLex(b *testing.B) {",
	"\tfor b.Loop() {",
	"\t}",
	"}",
))

// joinLines joins ls with "\n" and a final newline.
func joinLines(ls ...string) string {
	return string(lines(ls...))
}

func TestExtractGoTest(t *testing.T) {
	t.Run("top_level_func", func(t *testing.T) {
		want := joinLines(
			"func TestLexer(t *testing.T) {",
			"\ts := \"}\" + `{`",
			"\tr := '}'",
			"\tq := '\"'",
			"\t// a comment with a } brace",
			"\t/* and a { block comment */",
			"\tt.Run(\"outer\", func(t *testing.T) {",
			"\t\tt.Run(\"pipe_continues_line\", func(t *testing.T) { t.Log(\"nested\") })",
			"\t})",
			"\tt.Run(\"pipe_continues_line_twice\", func(t *testing.T) {})",
			"\tt.Run(\"pipe_continues_line\", func(t *testing.T) {",
			"\t\tif s != \"}{\" || r != '}' || q != '\"' { // } inside a comment",
			"\t\t\tt.Fatal(\"bad }\")",
			"\t\t}",
			"\t})",
			"\tt.Run(\"after\", func(t *testing.T) {})",
			"}",
		)
		want = want[:len(want)-1] // the function ends at its closing brace
		got, ok := extractGoTest(goSource, "TestLexer")
		if !ok || got != want {
			t.Errorf("extractGoTest(TestLexer) = %q, %v\nwant %q, true", got, ok, want)
		}
	})

	t.Run("subtest_call", func(t *testing.T) {
		// The second t.Run with this name is the one directly in the body of
		// TestLexer: the first one is inside "outer", and TestLexerMore and
		// TestOther have their own.
		want := joinLines(
			"t.Run(\"pipe_continues_line\", func(t *testing.T) {",
			"\t\tif s != \"}{\" || r != '}' || q != '\"' { // } inside a comment",
			"\t\t\tt.Fatal(\"bad }\")",
			"\t\t}",
			"\t})",
		)
		want = want[:len(want)-1]
		got, ok := extractGoTest(goSource, "TestLexer/pipe_continues_line")
		if !ok || got != want {
			t.Errorf("extractGoTest(TestLexer/pipe_continues_line) = %q, %v\nwant %q, true", got, ok, want)
		}

		// A one-line subtest, and one whose name is the start of another.
		rows := []struct{ ref, want string }{
			{"TestLexer/after", "t.Run(\"after\", func(t *testing.T) {})"},
			{"TestLexer/outer", joinLines(
				"t.Run(\"outer\", func(t *testing.T) {",
				"\t\tt.Run(\"pipe_continues_line\", func(t *testing.T) { t.Log(\"nested\") })",
				"\t})",
			)},
			{"TestLexer/pipe_continues_line_twice", "t.Run(\"pipe_continues_line_twice\", func(t *testing.T) {})"},
			{"TestOther/pipe_continues_line", "t.Run(\"pipe_continues_line\", func(t *testing.T) { t.Log(\"other\") })"},
			{"TestOther/only_here", "t.Run(\"only_here\", func(t *testing.T) {})"},
			{"TestOther/raw_name", "t.Run(`raw_name`, func(t *testing.T) {})"},
		}
		for _, r := range rows {
			want := r.want
			if want[len(want)-1] == '\n' {
				want = want[:len(want)-1]
			}
			got, ok := extractGoTest(goSource, r.ref)
			if !ok || got != want {
				t.Errorf("extractGoTest(%s) = %q, %v\nwant %q, true", r.ref, got, ok, want)
			}
		}
	})

	t.Run("fuzz_and_example", func(t *testing.T) {
		rows := []struct{ ref, want string }{
			{"FuzzLex", joinLines(
				"func FuzzLex(f *testing.F) {",
				"\tf.Add(\"{\")",
				"\tf.Fuzz(func(t *testing.T, s string) {})",
				"}",
			)},
			{"ExampleInterner", joinLines(
				"func ExampleInterner() {",
				"\tfmt.Println(\"}\")",
				"\t// Output: }",
				"}",
			)},
			{"ExampleInterner_Intern", joinLines(
				"func ExampleInterner_Intern() {",
				"\tfmt.Println(\"wrong one\")",
				"}",
			)},
			{"BenchmarkLex", joinLines(
				"func BenchmarkLex(b *testing.B) {",
				"\tfor b.Loop() {",
				"\t}",
				"}",
			)},
		}
		for _, r := range rows {
			want := r.want[:len(r.want)-1]
			got, ok := extractGoTest(goSource, r.ref)
			if !ok || got != want {
				t.Errorf("extractGoTest(%s) = %q, %v\nwant %q, true", r.ref, got, ok, want)
			}
		}
	})

	t.Run("missing", func(t *testing.T) {
		refs := []string{
			"TestNope",          // no such function
			"TestLexe",          // a prefix of a name is not the name
			"TestLexer/nope",    // the function exists, the subtest does not
			"TestOther/after",   // a subtest of another function
			"TestOther/chained", // x.t.Run is not a call on the parameter t
			"TestLexer/pipe",    // a prefix of a subtest name
			"FuzzLex/seed",      // only tests have subtests
			"ExampleInterner/x", // idem
			"S",                 // not a test name
			"",                  // empty
			"Test",              // no such function
		}
		for _, ref := range refs {
			if got, ok := extractGoTest(goSource, ref); ok {
				t.Errorf("extractGoTest(%q) = %q, true; want ok == false", ref, got)
			}
		}

		// Wrong signatures do not count: a method, a parameter of another
		// type, and a function that never closes.
		bad := []struct{ ref, src string }{
			{"TestM", "func (S) TestM(t *testing.T) {\n}\n"},
			{"TestP", "func TestP(t *testing.B) {\n}\n"},
			{"TestQ", "func TestQ(t *testing.T, x int) {\n}\n"},
			{"TestU", "func TestU(t *testing.T) {\n\tif true {\n}\n"},
			{"ExampleE", "func ExampleE(t *testing.T) {\n}\n"},
		}
		for _, b := range bad {
			if got, ok := extractGoTest([]byte(b.src), b.ref); ok {
				t.Errorf("extractGoTest(%q) on %q = %q, true; want ok == false", b.ref, b.src, got)
			}
		}
	})
}
