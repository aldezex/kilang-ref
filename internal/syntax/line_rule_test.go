package syntax_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/syntax"
)

// lineRow is one case of the tables of TestLineRule.
type lineRow struct {
	src string
	// want is the shape of every token of src, as shape writes it, separated by
	// spaces: `LowerIdent(a) Newline LowerIdent(b) Eof`.
	want string
	// nl is the span, as a [lo, hi) pair, of every Newline that want lists, in
	// order. A row without a Newline leaves it nil.
	nl [][2]uint32
	// diags is how many diagnostics the lexer reports for src; most rows are
	// clean.
	diags int
}

// newlineSpans returns the span of every Newline token of tb, in order.
func newlineSpans(tb syntax.TokenBuffer) [][2]uint32 {
	var spans [][2]uint32
	for _, tok := range tb.Tokens {
		if tok.Kind == syntax.Newline {
			spans = append(spans, [2]uint32{tok.Span.Lo, tok.Span.Hi})
		}
	}
	return spans
}

// checkLineRows lexes every row and fails the test unless the tokens, the
// spans of the Newlines and the number of diagnostics are exactly the row's.
func checkLineRows(t *testing.T, rows []lineRow) {
	t.Helper()
	for _, row := range rows {
		tb, sink, _, in := lexFile(t, row.src)
		wantShape(t, strconv.Quote(row.src), tb, in, strings.Fields(row.want)...)
		if got := newlineSpans(tb); !slices.Equal(got, row.nl) {
			t.Errorf("%q: Newline spans %v, want %v", row.src, got, row.nl)
		}
		if n := len(sink.All()); n != row.diags {
			t.Errorf("%q: got %d diagnostics, want %d: %+v", row.src, n, row.diags, sink.All())
		}
	}
}

func TestLineRule(t *testing.T) {
	t.Run("newline_between_statements", func(t *testing.T) {
		// The simplest case: a Newline between a and b, and its exact span, which
		// is the line feed that ends the first line.
		tb, sink, _, in := lexFile(t, "a\nb")
		wantShape(t, "a\\nb", tb, in, "LowerIdent(a)", "Newline", "LowerIdent(b)", "Eof")
		wantSpans(t, "a\\nb", tb, [2]uint32{0, 1}, [2]uint32{1, 2}, [2]uint32{2, 3}, [2]uint32{3, 3})
		// The Newline is not at the start of a line and b keeps the flag that
		// says a line break came before it.
		wantNewlines(t, "a\\nb", tb, false, false, true, false)
		wantNoDiags(t, "a\\nb", sink)

		// The span is the first line terminator after the previous token, also
		// when comments or more breaks come between the two tokens.
		checkLineRows(t, []lineRow{
			{"a\nb", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{1, 2}}, 0},
			{"a\n\n\nb", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{1, 2}}, 0},
			{"a   \n  b", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{4, 5}}, 0},
			{"a // c\nb", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{6, 7}}, 0},
			{"a /* x\ny */ b", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{6, 7}}, 0},
			{"a /* x\ny\nz */ b", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{6, 7}}, 0},
			{"a\n b\n c", "LowerIdent(a) Newline LowerIdent(b) Newline LowerIdent(c) Eof", [][2]uint32{{1, 2}, {4, 5}}, 0},
			// A break before the first token has nothing to end.
			{"\na", "LowerIdent(a) Eof", nil, 0},
			{"\n\n  // c\n  a", "LowerIdent(a) Eof", nil, 0},
			// Without a break there is no Newline.
			{"a b", "LowerIdent(a) LowerIdent(b) Eof", nil, 0},
			{"a /* x */ b", "LowerIdent(a) LowerIdent(b) Eof", nil, 0},
		})

		// Every kind that can end an expression ends a statement at a line
		// break, and the Newline sits right after it.
		enders := []string{
			"a", "A", "1", "1.5", "'a'", `"s"`, `"${x}"`, "true", "false", "(a)", "[a]", "{a}", "a?", "_",
			"return", "break", "continue",
		}
		for _, ender := range enders {
			src := ender + "\nb"
			tb, sink, _, in := lexFile(t, src)
			at := uint32(len(ender))
			n := len(tb.Tokens)
			if got := newlineSpans(tb); !slices.Equal(got, [][2]uint32{{at, at + 1}}) {
				t.Errorf("%q: Newline spans %v, want [[%d %d]]", src, got, at, at+1)
			}
			if n < 3 || tb.Tokens[n-3].Kind != syntax.Newline || tb.Tokens[n-2].Kind != syntax.LowerIdent {
				t.Errorf("%q: tokens %q, want a Newline right before b", src, shape(tb, in))
			}
			wantNoDiags(t, strconv.Quote(src), sink)
		}
	})
	t.Run("no_newline_inside_parens", func(t *testing.T) {
		checkLineRows(t, []lineRow{
			// The first row is the plain one: a comma cannot end an expression, so
			// it would pass even with no stack.
			{"f(a,\nb)", "LowerIdent(f) LParen LowerIdent(a) Comma LowerIdent(b) RParen Eof", nil, 0},
			// These end a line with an expression, inside () and [].
			{"f(a\nb)", "LowerIdent(f) LParen LowerIdent(a) LowerIdent(b) RParen Eof", nil, 0},
			{"(a\nb)", "LParen LowerIdent(a) LowerIdent(b) RParen Eof", nil, 0},
			{"[a\nb]", "LBracket LowerIdent(a) LowerIdent(b) RBracket Eof", nil, 0},
			{"f(a\n)", "LowerIdent(f) LParen LowerIdent(a) RParen Eof", nil, 0},
			{"[\n[a\nb]\n]", "LBracket LBracket LowerIdent(a) LowerIdent(b) RBracket RBracket Eof", nil, 0},
			// A pair inside the brackets does not change that once it closes.
			{"(f(a)\nb)", "LParen LowerIdent(f) LParen LowerIdent(a) RParen LowerIdent(b) RParen Eof", nil, 0},
			{"({a}\nb)", "LParen LBrace LowerIdent(a) RBrace LowerIdent(b) RParen Eof", nil, 0},
			// After the closing bracket the statement ends again.
			{"(a)\nb", "LParen LowerIdent(a) RParen Newline LowerIdent(b) Eof", [][2]uint32{{3, 4}}, 0},
			{"[a]\nb", "LBracket LowerIdent(a) RBracket Newline LowerIdent(b) Eof", [][2]uint32{{3, 4}}, 0},
			{"(a\nb)\nc", "LParen LowerIdent(a) LowerIdent(b) RParen Newline LowerIdent(c) Eof", [][2]uint32{{5, 6}}, 0},
			{"(a)\n(b)\nc", "LParen LowerIdent(a) RParen Newline LParen LowerIdent(b) RParen Newline LowerIdent(c) Eof", [][2]uint32{{3, 4}, {7, 8}}, 0},
		})
	})
	t.Run("no_newline_after_operator", func(t *testing.T) {
		checkLineRows(t, []lineRow{
			{"a +\nb", "LowerIdent(a) Plus LowerIdent(b) Eof", nil, 0},
			{"x =\n1", "LowerIdent(x) Eq Int(1) Eof", nil, 0},
			{"a,\nb", "LowerIdent(a) Comma LowerIdent(b) Eof", nil, 0},
			{"{\na", "LBrace LowerIdent(a) Eof", nil, 0},
			{"a =>\nb", "LowerIdent(a) FatArrow LowerIdent(b) Eof", nil, 0},
			{"let\nb", "KwLet LowerIdent(b) Eof", nil, 0},
		})
		// None of the kinds that cannot end an expression gets a Newline after
		// it: the 28 signs here and the 21 keywords that are not return, break,
		// continue, true or false.
		for _, tok := range []string{
			"+", "-", "*", "/", "%", "=", "==", "!=", "<", "<=", ">", ">=", "&&", "||", "|>", "|", "!",
			",", ";", ":", ".", "..", "=>", "->", "#", "(", "[", "{", "let", "var", "fn", "if", "else",
			"match", "for", "in", "while", "with", "uses", "pub", "import", "type", "trait", "impl",
			"effect", "handle", "spawn", "lazy", "select",
		} {
			src := tok + "\nb"
			tb, sink, _, in := lexFile(t, src)
			if got := newlineSpans(tb); len(got) != 0 {
				t.Errorf("%q: got Newlines at %v, want none: %q", src, got, shape(tb, in))
			}
			wantNoDiags(t, strconv.Quote(src), sink)
		}
	})
	t.Run("pipe_continues_line", func(t *testing.T) {
		checkLineRows(t, []lineRow{
			{"xs\n  |> sum", "LowerIdent(xs) Pipe LowerIdent(sum) Eof", nil, 0},
			{"let total = xs\n  |> sum\n", "KwLet LowerIdent(total) Eq LowerIdent(xs) Pipe LowerIdent(sum) Eof", nil, 0},
			// The other two operators that continue the previous line.
			{"a\n&& b", "LowerIdent(a) AndAnd LowerIdent(b) Eof", nil, 0},
			{"a\n|| b", "LowerIdent(a) OrOr LowerIdent(b) Eof", nil, 0},
			// A continued line can be followed by a new statement.
			{"xs\n|> f\nys", "LowerIdent(xs) Pipe LowerIdent(f) Newline LowerIdent(ys) Eof", [][2]uint32{{7, 8}}, 0},
			// Only the whole operators count: a single | is the bar of a
			// pattern, and it starts a statement.
			{"a\n| b", "LowerIdent(a) Newline Bar LowerIdent(b) Eof", [][2]uint32{{1, 2}}, 0},
		})
	})
	t.Run("dot_and_else_continue_line", func(t *testing.T) {
		checkLineRows(t, []lineRow{
			{"}\nelse {", "RBrace KwElse LBrace Eof", nil, 0},
			{"x\n.f", "LowerIdent(x) Dot LowerIdent(f) Eof", nil, 0},
			{"if c {\n a\n}\nelse {\n b\n}", "KwIf LowerIdent(c) LBrace LowerIdent(a) Newline RBrace KwElse LBrace LowerIdent(b) Newline RBrace Eof",
				[][2]uint32{{9, 10}, {21, 22}}, 0},
			// A line that starts with .. is a new statement: only a single dot
			// continues the line.
			{"x\n..y", "LowerIdent(x) Newline DotDot LowerIdent(y) Eof", [][2]uint32{{1, 2}}, 0},
		})
	})
	t.Run("minus_starts_new_statement", func(t *testing.T) {
		checkLineRows(t, []lineRow{
			{"a\n-b", "LowerIdent(a) Newline Minus LowerIdent(b) Eof", [][2]uint32{{1, 2}}, 0},
			// Also +, and the others that are not in the list of the line rule.
			{"a\n+b", "LowerIdent(a) Newline Plus LowerIdent(b) Eof", [][2]uint32{{1, 2}}, 0},
			{"a\n*b", "LowerIdent(a) Newline Star LowerIdent(b) Eof", [][2]uint32{{1, 2}}, 0},
			{"a\n!b", "LowerIdent(a) Newline Bang LowerIdent(b) Eof", [][2]uint32{{1, 2}}, 0},
			{"a\n==b", "LowerIdent(a) Newline EqEq LowerIdent(b) Eof", [][2]uint32{{1, 2}}, 0},
			{"a\n(b)", "LowerIdent(a) Newline LParen LowerIdent(b) RParen Eof", [][2]uint32{{1, 2}}, 0},
		})
	})
	t.Run("newline_inside_braces_in_parens", func(t *testing.T) {
		// The simplest case: the innermost bracket is the brace, so the lines
		// of the block end, though the parens around it would swallow them.
		tb, sink, _, in := lexFile(t, "f({\na\nb\n})")
		wantShape(t, "f({\\na\\nb\\n})", tb, in, "LowerIdent(f)", "LParen", "LBrace", "LowerIdent(a)", "Newline",
			"LowerIdent(b)", "Newline", "RBrace", "RParen", "Eof")
		if got, want := newlineSpans(tb), [][2]uint32{{5, 6}, {7, 8}}; !slices.Equal(got, want) {
			t.Errorf("Newline spans %v, want %v", got, want)
		}
		wantNoDiags(t, "f({\\na\\nb\\n})", sink)

		checkLineRows(t, []lineRow{
			// Parens inside a block turn the lines off again, until they close.
			{"{\nf(a\nb)\n}", "LBrace LowerIdent(f) LParen LowerIdent(a) LowerIdent(b) RParen Newline RBrace Eof", [][2]uint32{{8, 9}}, 0},
			{"([\n{\na\nb\n}\n])", "LParen LBracket LBrace LowerIdent(a) Newline LowerIdent(b) Newline RBrace RBracket RParen Eof",
				[][2]uint32{{6, 7}, {8, 9}}, 0},
			// Inside a block the closing brace ends the last line as well.
			{"{\na\n}\nb", "LBrace LowerIdent(a) Newline RBrace Newline LowerIdent(b) Eof", [][2]uint32{{3, 4}, {5, 6}}, 0},
			// A closing bracket that does not match the innermost one closes
			// the brackets inside its own partner too; if it has no partner
			// it is ignored. This keeps one missing bracket from silencing
			// every line break after it.
			{"({a)\nb", "LParen LBrace LowerIdent(a) RParen Newline LowerIdent(b) Eof", [][2]uint32{{4, 5}}, 0},
			{"{(a}\nb", "LBrace LParen LowerIdent(a) RBrace Newline LowerIdent(b) Eof", [][2]uint32{{4, 5}}, 0},
			{"(}\nb", "LParen RBrace LowerIdent(b) Eof", nil, 0},
			{")\nb", "RParen Newline LowerIdent(b) Eof", [][2]uint32{{1, 2}}, 0},
			{"a ]\nb", "LowerIdent(a) RBracket Newline LowerIdent(b) Eof", [][2]uint32{{3, 4}}, 0},
			// The end of a string ends the brackets that its interpolations
			// left open as well, so the line breaks after the string are not
			// hidden by a ( that nobody closed. A bracket that the expression
			// closed from outside, and a string inside the expression, work the
			// same.
			{"\"${ f(a }\"\nb\nc", `StringHead("") LowerIdent(f) LParen LowerIdent(a) StringTail("") Newline LowerIdent(b) Newline LowerIdent(c) Eof`,
				[][2]uint32{{10, 11}, {12, 13}}, 0},
			{"\"${ g(\"${ ( }\") }\"\nb", `StringHead("") LowerIdent(g) LParen StringHead("") LParen StringTail("") RParen StringTail("") Newline LowerIdent(b) Eof`,
				[][2]uint32{{18, 19}}, 0},
			{"( \"${ ) }\"\nb", `LParen StringHead("") RParen StringTail("") Newline LowerIdent(b) Eof`, [][2]uint32{{10, 11}}, 0},
			// But only the brackets of the expression end: the ( that was open
			// before the string still hides the line break after it.
			{"f(\"${ ( }\"\nb)", `LowerIdent(f) LParen StringHead("") LParen StringTail("") LowerIdent(b) RParen Eof`, nil, 0},
			// With a string inside the expression, each tail closes only the
			// brackets opened inside its own string: the inner one keeps the (
			// of g, and the outer one closes the ( that follows the call.
			{"\"${ g(\"${ x }\") ( }\"\nb", `StringHead("") LowerIdent(g) LParen StringHead("") LowerIdent(x) StringTail("") RParen LParen StringTail("") Newline LowerIdent(b) Eof`,
				[][2]uint32{{20, 21}}, 0},
			// The tail closes every bracket the expression left open, not just
			// the innermost one, and the same goes for an expression that
			// has a complete string between its ( and the end.
			{"\"${ ( [ }\"\nb\nc", `StringHead("") LParen LBracket StringTail("") Newline LowerIdent(b) Newline LowerIdent(c) Eof`,
				[][2]uint32{{10, 11}, {12, 13}}, 0},
			{"\"${ ( \"${ x }\" }\"\nb", `StringHead("") LParen StringHead("") LowerIdent(x) StringTail("") StringTail("") Newline LowerIdent(b) Eof`,
				[][2]uint32{{17, 18}}, 0},
			// A string that never ends has no tail to close its brackets. The
			// line break that ended it does: the first token on the next line
			// is outside the string, and so is the ( that its interpolation
			// left open.
			{"\"${ f(a\nb\nc", `Error LowerIdent(f) LParen LowerIdent(a) Newline LowerIdent(b) Newline LowerIdent(c) Eof`,
				[][2]uint32{{7, 8}, {9, 10}}, 1},
			// Only the brackets of the string go: the f( that was open before
			// it still hides the line break after c, and closes at its own ).
			{"f(a, \"${ g(b\nc)\nd", `LowerIdent(f) LParen LowerIdent(a) Comma Error LowerIdent(g) LParen LowerIdent(b) LowerIdent(c) RParen Newline LowerIdent(d) Eof`,
				[][2]uint32{{15, 16}}, 1},
			// It happens once: the ( that the next line opens is a real one,
			// and the line break inside it is a space.
			{"\"${ f(a\n(b\nc)\nd", `Error LowerIdent(f) LParen LowerIdent(a) Newline LParen LowerIdent(b) LowerIdent(c) RParen Newline LowerIdent(d) Eof`,
				[][2]uint32{{7, 8}, {13, 14}}, 1},
			// Strings inside strings that never end are one Error token, and
			// the same applies to the brackets of all of them.
			{"f(\"${ g(\"${ x\nb)\nc", `LowerIdent(f) LParen Error LowerIdent(g) LParen LowerIdent(x) LowerIdent(b) RParen Newline LowerIdent(c) Eof`,
				[][2]uint32{{16, 17}}, 1},
			// The brackets the expression closed from outside are gone, and
			// the ones it opened after that go with the line break.
			{"( \"${ ) [ x\nb\nc", `LParen Error RParen LBracket LowerIdent(x) Newline LowerIdent(b) Newline LowerIdent(c) Eof`,
				[][2]uint32{{11, 12}, {13, 14}}, 1},
			// An Error that is not a string never ends anything: the ( stays
			// open, and the line break inside it is a space.
			{"$ (a\nb", `Error LParen LowerIdent(a) LowerIdent(b) Eof`, nil, 1},
			// A StringMid leaves the brackets as they are. The inner string
			// ends at its own tail, which closes nothing: the g( was open
			// before the inner string began, so the `)` after it is what
			// closes g(. The f( stays open to the end, so the line break
			// before c is a space.
			{"f(\"${ g(\"${ x }b${ y }\") }\"\nc)", `LowerIdent(f) LParen StringHead("") LowerIdent(g) LParen StringHead("") LowerIdent(x) StringMid("b") LowerIdent(y) StringTail("") RParen StringTail("") LowerIdent(c) RParen Eof`,
				nil, 0},
			// The brackets that the tail closes are the ones opened inside the
			// expression, also when the expression closed a bracket from
			// outside first: the ( is gone and the [ that came after it goes
			// with the tail.
			{"( \"${ ) [ }\"\nb\nc", `LParen StringHead("") RParen LBracket StringTail("") Newline LowerIdent(b) Newline LowerIdent(c) Eof`,
				[][2]uint32{{12, 13}, {14, 15}}, 0},
			// The tail takes the ( or the [ of its string with it, and that has
			// to count: the ) or ] after the string then has no partner, so it
			// is ignored (it closes nothing), and the line break after it still
			// ends the statement. If the tail only cut the stack and left the
			// count of ( at 1, this ) would look for a ( that is not there.
			{"\"${ ( }\")\nb", `StringHead("") LParen StringTail("") RParen Newline LowerIdent(b) Eof`, [][2]uint32{{9, 10}}, 0},
			{"\"${ [ }\"]\nb", `StringHead("") LBracket StringTail("") RBracket Newline LowerIdent(b) Eof`, [][2]uint32{{9, 10}}, 0},
			// The same for the line break that ends a string that never ends:
			// it takes the ( with it, and the ) on the next line is ignored.
			{"\"${ (a\n)\nb", `Error LParen LowerIdent(a) Newline RParen Newline LowerIdent(b) Eof`, [][2]uint32{{6, 7}, {8, 9}}, 1},
		})
	})
	t.Run("crlf_line_endings", func(t *testing.T) {
		// Same tokens as "a\nb", but the Newline is the two bytes of the CRLF.
		tb, sink, _, in := lexFile(t, "a\r\nb")
		wantShape(t, "a\\r\\nb", tb, in, "LowerIdent(a)", "Newline", "LowerIdent(b)", "Eof")
		wantSpans(t, "a\\r\\nb", tb, [2]uint32{0, 1}, [2]uint32{1, 3}, [2]uint32{3, 4}, [2]uint32{4, 4})
		wantNoDiags(t, "a\\r\\nb", sink)

		checkLineRows(t, []lineRow{
			{"a\r\n\r\nb", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{1, 3}}, 0},
			{"a // c\r\nb", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{6, 8}}, 0},
			// A lone CR before the CRLF is whitespace; the CRLF is the last two bytes.
			{"a\r\r\nb", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{2, 4}}, 0},
			// A LF that follows a lone CR is not a CRLF: the CR and the LF
			// are apart.
			{"a\r \nb", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{3, 4}}, 0},
			// The first terminator wins whichever ending it has.
			{"a\n\r\nb", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{1, 2}}, 0},
			{"a\r\n\nb", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{1, 3}}, 0},
			// The rule works the same inside brackets and for the continuations.
			{"f(a\r\nb)", "LowerIdent(f) LParen LowerIdent(a) LowerIdent(b) RParen Eof", nil, 0},
			{"xs\r\n|> sum\r\n", "LowerIdent(xs) Pipe LowerIdent(sum) Eof", nil, 0},
			{"a\r\n", "LowerIdent(a) Eof", nil, 0},
		})
	})
	t.Run("lone_cr_is_whitespace", func(t *testing.T) {
		tb, sink, _, in := lexFile(t, "a\rb")
		wantShape(t, "a\\rb", tb, in, "LowerIdent(a)", "LowerIdent(b)", "Eof")
		wantNewlines(t, "a\\rb", tb, false, false, false)
		wantNoDiags(t, "a\\rb", sink)
		checkLineRows(t, []lineRow{
			{"a\r\rb", "LowerIdent(a) LowerIdent(b) Eof", nil, 0},
			{"a\r", "LowerIdent(a) Eof", nil, 0},
			{"\ra", "LowerIdent(a) Eof", nil, 0},
			// Inside a comment it is one more byte of the comment. It does not
			// end a // comment (y stays in it, and the Newline is the line feed
			// that follows), and it is no line break inside a /* */ comment:
			// if it were, b would start a line with no line feed before it,
			// and the line rule would have no Newline to give.
			{"a // x\ry\nb", "LowerIdent(a) Newline LowerIdent(b) Eof", [][2]uint32{{8, 9}}, 0},
			{"a /* x\ry */ b", "LowerIdent(a) LowerIdent(b) Eof", nil, 0},
		})
	})
	t.Run("no_newline_before_eof", func(t *testing.T) {
		checkLineRows(t, []lineRow{
			{"a\n", "LowerIdent(a) Eof", nil, 0},
			{"a\n\n\n", "LowerIdent(a) Eof", nil, 0},
			{"a\r\n", "LowerIdent(a) Eof", nil, 0},
			{"a // c\n", "LowerIdent(a) Eof", nil, 0},
			{"a\n// c\n", "LowerIdent(a) Eof", nil, 0},
			{"a\n/* c\n */", "LowerIdent(a) Eof", nil, 0},
			{"(a)\n", "LParen LowerIdent(a) RParen Eof", nil, 0},
			{"{a}\n", "LBrace LowerIdent(a) RBrace Eof", nil, 0},
			// And at the end of a block, the Newline comes before the brace,
			// not before the Eof.
			{"{a\n}\n", "LBrace LowerIdent(a) Newline RBrace Eof", [][2]uint32{{2, 3}}, 0},
			{"", "Eof", nil, 0},
			{"\n", "Eof", nil, 0},
		})
	})
	t.Run("error_token_does_not_end_expression", func(t *testing.T) {
		checkLineRows(t, []lineRow{
			// The simplest case: the stray $ does not end the line, so b is on
			// the same statement and no second error is possible downstream.
			{"a $\nb", "LowerIdent(a) Error LowerIdent(b) Eof", nil, 1},
			{"$\nb", "Error LowerIdent(b) Eof", nil, 1},
			{"$\n$\nb", "Error Error LowerIdent(b) Eof", nil, 2},
			// The Error token of an unterminated string is the same.
			{"a \"abc\nb", "LowerIdent(a) Error LowerIdent(b) Eof", nil, 1},
			// An Error is no exception at the start of a line: what comes
			// before it can still end a statement.
			{"a\n$", "LowerIdent(a) Newline Error Eof", [][2]uint32{{1, 2}}, 1},
			{"a\n\"abc", "LowerIdent(a) Newline Error Eof", [][2]uint32{{1, 2}}, 1},
			// An unterminated interpolation: the Error comes first and then the
			// tokens of the expression that were already read. The previous
			// token of the next line is the last of those, not the Error, so
			// the line rule is the usual one: a Newline after y that ends an
			// expression, none after a + that does not.
			{"\"${ y\nx", "Error LowerIdent(y) Newline LowerIdent(x) Eof", [][2]uint32{{5, 6}}, 1},
			{"\"${ y +\nx", "Error LowerIdent(y) Plus LowerIdent(x) Eof", nil, 1},
			// When no token of the expression was read, the previous token is the Error.
			{"\"${\nx", "Error LowerIdent(x) Eof", nil, 1},
			{"a\n\"${ y\nx", "LowerIdent(a) Newline Error LowerIdent(y) Newline LowerIdent(x) Eof", [][2]uint32{{1, 2}, {7, 8}}, 1},
		})
	})
}
