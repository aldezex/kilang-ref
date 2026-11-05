package sema_test

import (
	"slices"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here are about Float in the checker: the type, its literals, the
// operators that work on it and the order of the functions. They reuse
// wantDiags, mismatch, secondary, spanIn and unknownType from check_test.go and
// wantCodes from resolve_test.go, which are in the same package.

// rejectedOperand is the E0401 for an operand at that is of the type found, a
// type that the operator op does not work on: the label says which types it
// takes (expected), and the note says it again (works). It has no secondary
// label, because no other place in the program explains the rule.
func rejectedOperand(at base.Span, found, expected, op, works string) diag.Diagnostic {
	return mismatch(at, "expected "+expected+", found `"+found+"`").WithNote("`" + op + "` works on " + works + " here")
}

// floatEdit adds to the E0401 d, whose primary label is at at, the help and the
// edit that write the Int literal there as the Float that is the same number:
// the one mismatch that has an edit, because it has no other meaning.
func floatEdit(d diag.Diagnostic, at base.Span, text string) diag.Diagnostic {
	return d.WithHelp("write the number as a `Float`: `" + text + "`").WithFixIt(diag.Replace(at, text))
}

const (
	// What the arithmetic and the ordering take, and what `==` takes.
	numberTypes   = "`Int` or `Float`"
	numbersWork   = "`Int` and `Float`"
	equatableType = "`Int`, `Bool` or `Float`"
	equatableWork = "`Int`, `Bool` and `Float`"
)

func TestFloatCheck(t *testing.T) {
	t.Run("float_is_a_type_with_literals", func(t *testing.T) {
		for _, row := range []struct{ name, src, fn, want string }{
			{"literal", "fn a() -> Float { 1.5 }", "a", "() -> Float"},
			{"exponent_literal", "fn a() -> Float { 1e21 }", "a", "() -> Float"},
			{"parameter", "fn b(x: Float) -> Float { x }", "b", "(Float) -> Float"},
			{"annotated_let", "fn c() { let x: Float = 2.0 }", "c", "() -> ()"},
			{"inferred_let", "fn d() -> Float {\n  let x = 0.1\n  x\n}", "d", "() -> Float"},
			{"prelude_printFloat", "fn main() {}", "printFloat", "(Float) -> ()"},
			{"prelude_toFloat", "fn main() {}", "toFloat", "(Int) -> Float"},
			{"prelude_truncate", "fn main() {}", "truncate", "(Float) -> Int"},
			{"prelude_sqrt", "fn main() {}", "sqrt", "(Float) -> Float"},
			{"prelude_function_as_value", "fn f() { let g = truncate }", "f", "() -> ()"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p)
			if got := testsupport.DefType(t, p, row.fn); got != row.want {
				t.Errorf("%s: type of %s is %q, want %q", row.name, row.fn, got, row.want)
			}
		}
	})

	t.Run("float_and_int_are_not_converted", func(t *testing.T) {
		{
			const src = "fn main() { let x: Float = 1 }"
			p := testsupport.Analyze(t, src)
			at := spanIn(t, p, src, "= 1", "1")
			wantDiags(t, "Int for Float", p, floatEdit(secondary(mismatch(at, "expected `Float`, found `Int`"),
				spanIn(t, p, src, ": Float", "Float"), "expected because of this annotation"), at, "1.0"))
		}
		{
			const src = "fn main() { let n: Int = 1.0 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Float for Int", p, secondary(mismatch(spanIn(t, p, src, "= 1.0", "1.0"), "expected `Int`, found `Float`"),
				spanIn(t, p, src, ": Int", "Int"), "expected because of this annotation"))
		}
		{
			const src = "fn g(x: Float) {}\nfn main() { g(1) }"
			p := testsupport.Analyze(t, src)
			at := spanIn(t, p, src, "g(1)", "1")
			wantDiags(t, "argument", p, floatEdit(secondary(mismatch(at, "expected `Float`, found `Int`"),
				spanIn(t, p, src, "x: Float", "x: Float"), "the parameter is declared here"), at, "1.0"))
		}
		{
			const src = "fn f() -> Float { 1 }"
			p := testsupport.Analyze(t, src)
			at := spanIn(t, p, src, "{ 1 }", "1")
			wantDiags(t, "return", p, floatEdit(secondary(mismatch(at, "expected `Float`, found `Int`"),
				spanIn(t, p, src, "-> Float", "Float"), "expected because of the return type"), at, "1.0"))
		}
		{
			const src = "fn main() { let r: Int = 1.0 + 2.0 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "result of a sum of Floats", p, secondary(mismatch(spanIn(t, p, src, "= 1.0 + 2.0", "1.0 + 2.0"), "expected `Int`, found `Float`"),
				spanIn(t, p, src, ": Int", "Int"), "expected because of this annotation"))
		}
		// The edit of an Int literal where a Float is expected writes the same number
		// as a Float, whichever way the literal is written (a zero too: `0x0` is the
		// number 0 and not a number that did not fit), and the program that results
		// checks. A number above 2^53 has the edit only when a Float holds it exactly
		// (2^62 does, 2^53 + 1 does not). Nothing else has an edit: not a variable,
		// not a negative number (an operator on a literal), not an Int where
		// something else is expected, and a Float where an Int is expected is the
		// other mistake.
		for _, row := range []struct{ src, fixed string }{
			{"fn main() { let x: Float = 1 }", "fn main() { let x: Float = 1.0 }"},
			{"fn main() { printFloat(0x10) }", "fn main() { printFloat(16.0) }"},
			{"fn main() { let h = 1.0 / 0 }", "fn main() { let h = 1.0 / 0.0 }"},
			{"fn main() { let r = sqrt(1_000) }", "fn main() { let r = sqrt(1000.0) }"},
			{"fn main() { printFloat(0) }", "fn main() { printFloat(0.0) }"},
			{"fn main() { printFloat(0x0) }", "fn main() { printFloat(0.0) }"},
			{"fn main() { printFloat(0_0) }", "fn main() { printFloat(0.0) }"},
			{"fn main() { printFloat(0b0) }", "fn main() { printFloat(0.0) }"},
			{"fn main() { printFloat(9007199254740992) }", "fn main() { printFloat(9007199254740992.0) }"},
			{"fn main() { printFloat(4611686018427387904) }", "fn main() { printFloat(4611686018427387904.0) }"},
		} {
			p := testsupport.Analyze(t, row.src)
			all := p.Session.Sink.All()
			if len(all) != 1 || len(all[0].Fixits) != 1 {
				t.Errorf("%q: diagnostics %+v, want one with one edit", row.src, all)
				continue
			}
			if fixed := testsupport.ApplyFixits(row.src, all[0]); fixed != row.fixed {
				t.Errorf("%q: the edit gives %q, want %q", row.src, fixed, row.fixed)
			} else {
				wantDiags(t, row.src, testsupport.Analyze(t, fixed))
			}
		}
		// A number that the lexer reported is its error and nothing else: it has the
		// value 0 in the token, and 0 is not the number written, so the parser makes
		// an error node of it and no E0401 follows, with an edit that would have
		// written `0.0`. That holds for a number that does not fit in an Int (E0105)
		// and for one that is malformed (E0107): an underscore that ends the digits,
		// or a prefix with no digits after it.
		for _, row := range []struct{ src, code string }{
			{"fn main() { printFloat(99999999999999999999) }", "E0105"},
			{"fn main() { printFloat(0x8000_0000_0000_0000) }", "E0105"},
			{"fn main() { let b: Bool = 9223372036854775808 }", "E0105"},
			{"fn main() { let b: Bool = 0_ }", "E0107"},
			{"fn main() { printFloat(1_) }", "E0107"},
			{"fn main() { printFloat(0_0_) }", "E0107"},
			{"fn main() { printFloat(0x_) }", "E0107"},
			{"fn main() { printFloat(0x0_) }", "E0107"},
			{"fn main() { printFloat(0b) }", "E0107"},
		} {
			if got := testsupport.ErrorCodes(testsupport.Analyze(t, row.src)); len(got) != 1 || got[0] != row.code {
				t.Errorf("%q: error codes %v, want %s alone", row.src, got, row.code)
			}
		}
		// Each is found among several, wherever it stands in the file, and a zero that
		// was written is not taken for one of them.
		const several = "fn main() {\n  let a: Bool = 0_\n  let z: Int = 0\n  let b: Bool = 99999999999999999999\n  let y: Int = 0x0\n  let c: Bool = 0x_\n}"
		if got := testsupport.ErrorCodes(testsupport.Analyze(t, several)); !slices.Equal(got, []string{"E0107", "E0105", "E0107"}) {
			t.Errorf("%q: error codes %v, want E0107, E0105 and E0107", several, got)
		}
		// The place may expect a Float without having it written: a parameter that
		// nothing annotates is bound by the body to the type of what it is given to
		// (here `printFloat`), and the Int literal in the call, which comes after
		// the body, has the edit all the same. The expected type is that variable,
		// and it is its meaning that is a Float.
		{
			const src = "fn f(x) { printFloat(x) }\nfn main() { f(1) }"
			p := testsupport.Analyze(t, src)
			at := spanIn(t, p, src, "f(1)", "1")
			wantDiags(t, "a parameter bound to Float", p, floatEdit(secondary(mismatch(at, "expected `Float`, found `Int`"),
				spanIn(t, p, src, "f(x)", "x"), "the parameter is declared here"), at, "1.0"))
		}
		// A Float that is expected goes into both branches of an `if`, so the branch
		// that is an Int is the one with the error and the edit, whichever comes
		// first. (Without it the `then` branch decides, and the error of the first
		// program would be on the `2.5`.)
		{
			const src = "fn f(b: Bool) -> Float { if b { 1 } else { 2.5 } }"
			p := testsupport.Analyze(t, src)
			at := spanIn(t, p, src, "{ 1 }", "1")
			wantDiags(t, "Int then, Float else", p, floatEdit(secondary(mismatch(at, "expected `Float`, found `Int`"),
				spanIn(t, p, src, "-> Float", "Float"), "expected because of the return type"), at, "1.0"))
		}
		{
			const src = "fn f(b: Bool) -> Float { if b { 2.5 } else { 1 } }"
			p := testsupport.Analyze(t, src)
			at := spanIn(t, p, src, "{ 1 }", "1")
			wantDiags(t, "Float then, Int else", p, floatEdit(secondary(mismatch(at, "expected `Float`, found `Int`"),
				spanIn(t, p, src, "-> Float", "Float"), "expected because of the return type"), at, "1.0"))
		}
		{
			const src = "fn main() {\n  let b = true\n  let x: Float = if b { 1 } else if !b { 2 } else { 3.5 }\n}"
			p := testsupport.Analyze(t, src)
			one, two := spanIn(t, p, src, "{ 1 }", "1"), spanIn(t, p, src, "{ 2 }", "2")
			ann := spanIn(t, p, src, ": Float", "Float")
			wantDiags(t, "two Int branches of an else if chain", p,
				floatEdit(secondary(mismatch(one, "expected `Float`, found `Int`"), ann, "expected because of this annotation"), one, "1.0"),
				floatEdit(secondary(mismatch(two, "expected `Float`, found `Int`"), ann, "expected because of this annotation"), two, "2.0"))
		}
		{
			// A variable has no edit, and the branch that is a Float has no error.
			const src = "fn f(b: Bool, n: Int) -> Float { if b { n } else { 2.5 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Int variable in then", p, secondary(mismatch(spanIn(t, p, src, "{ n }", "n"), "expected `Float`, found `Int`"),
				spanIn(t, p, src, "-> Float", "Float"), "expected because of the return type"))
		}
		{
			// The rule is only for a Float: the `then` branch still decides for any
			// other type, and the `2.5` is the one marked.
			const src = "fn f(b: Bool) -> Int { if b { 1 } else { 2.5 } }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Int expected", p, secondary(mismatch(spanIn(t, p, src, "{ 2.5 }", "2.5"), "expected `Int`, found `Float`"),
				spanIn(t, p, src, "{ 1 }", "1"), "the other branch has type `Int`"))
		}
		for _, src := range []string{
			"fn main() {\n  let n = 1\n  let x: Float = n\n}", "fn main() { let x: Float = -1 }", "fn main() { let b: Bool = 1 }",
			"fn main() { let r: Int = 1.0 }",
			// A Float does not hold these numbers: the Float that the edit would write
			// is another one (2^53 + 1 is written as 9007199254740992.0).
			"fn main() { printFloat(9007199254740993) }", "fn main() { printFloat(9223372036854775807) }",
		} {
			all := testsupport.Analyze(t, src).Session.Sink.All()
			if len(all) != 1 || all[0].Help != "" || len(all[0].Fixits) != 0 {
				t.Errorf("%q: diagnostics %+v, want one with no help and no edit", src, all)
			}
		}
	})

	t.Run("operators_on_floats_have_the_type_of_their_operands", func(t *testing.T) {
		for _, op := range []string{"+", "-", "*", "/"} {
			p := testsupport.Analyze(t, "fn f(a: Float, b: Float) -> Float { a "+op+" b }")
			wantDiags(t, op, p)
			if got := testsupport.DefType(t, p, "f"); got != "(Float, Float) -> Float" {
				t.Errorf("%s: type of f is %q, want (Float, Float) -> Float", op, got)
			}
		}
		for _, op := range []string{"<", "<=", ">", ">=", "==", "!="} {
			p := testsupport.Analyze(t, "fn f(a: Float, b: Float) -> Bool { a "+op+" b }")
			wantDiags(t, op, p)
			if got := testsupport.DefType(t, p, "f"); got != "(Float, Float) -> Bool" {
				t.Errorf("%s: type of f is %q, want (Float, Float) -> Bool", op, got)
			}
		}
		for _, row := range []struct{ name, src, want string }{
			{"negation", "fn f(a: Float) -> Float { -a }", "(Float) -> Float"},
			{"negative_literal", "fn f() -> Float { -2.5 }", "() -> Float"},
			{"precedence", "fn f(a: Float) -> Float { a * a + 1.0 }", "(Float) -> Float"},
			{"comparison_of_sums", "fn f(a: Float) -> Bool { a + 1.0 < a * 2.0 }", "(Float) -> Bool"},
			{"int_is_unchanged", "fn f(a: Int, b: Int) -> Int { a % b + a * b - -a }", "(Int, Int) -> Int"},
			{"literal_on_the_left", "fn f(x) -> Bool { 0.5 < x }", "(Float) -> Bool"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p)
			if got := testsupport.DefType(t, p, "f"); got != row.want {
				t.Errorf("%s: type of f is %q, want %q", row.name, got, row.want)
			}
		}
	})

	t.Run("operands_are_one_type_and_the_left_one_decides", func(t *testing.T) {
		for _, op := range []string{"+", "-", "*", "/", "<", "<=", ">", ">=", "==", "!="} {
			// An Int on the left asks for Int on the right.
			src := "fn main() { let r = 1 " + op + " 2.0 }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Int "+op+" Float", p, secondary(mismatch(spanIn(t, p, src, " "+op+" 2.0", "2.0"), "expected `Int`, found `Float`"),
				spanIn(t, p, src, " "+op+" ", op), "`"+op+"` expects `Int` operands"))
			// And a Float on the left asks for Float.
			src = "fn main() { let r = 1.0 " + op + " 2 }"
			p = testsupport.Analyze(t, src)
			at := spanIn(t, p, src, " "+op+" 2", "2")
			wantDiags(t, "Float "+op+" Int", p, floatEdit(secondary(mismatch(at, "expected `Float`, found `Int`"),
				spanIn(t, p, src, " "+op+" ", op), "`"+op+"` expects `Float` operands"), at, "2.0"))
		}
		{
			const src = "fn main() { let r = 1.0 == true }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Float == Bool", p, secondary(mismatch(spanIn(t, p, src, "== true", "true"), "expected `Float`, found `Bool`"),
				spanIn(t, p, src, " == ", "=="), "`==` expects `Float` operands"))
		}
		// A right operand that does not fit is the one mistake: the operation has the
		// error type, and not the type of the left operand, so the place that uses
		// the result does not complain about the same `Float` again (as the
		// remainder does). The `Int` is on the left and the result is wanted as a
		// `Float`, which is what a reader who mixes the two writes.
		for _, op := range []string{"+", "-", "*", "/"} {
			src := "fn f(i: Int, x: Float) -> Float { i " + op + " x }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "return of Int "+op+" Float", p, secondary(mismatch(spanIn(t, p, src, " "+op+" x", "x"), "expected `Int`, found `Float`"),
				spanIn(t, p, src, " "+op+" ", op), "`"+op+"` expects `Int` operands"))
			src = "fn g(i: Int, x: Float) { printFloat(i " + op + " x) }"
			p = testsupport.Analyze(t, src)
			wantDiags(t, "argument of Int "+op+" Float", p, secondary(mismatch(spanIn(t, p, src, " "+op+" x", "x"), "expected `Int`, found `Float`"),
				spanIn(t, p, src, " "+op+" ", op), "`"+op+"` expects `Int` operands"))
		}
		{
			const src = "fn f(i: Int, x: Float) -> Bool { i < x }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Int < Float", p, secondary(mismatch(spanIn(t, p, src, " < x", "x"), "expected `Int`, found `Float`"),
				spanIn(t, p, src, " < ", "<"), "`<` expects `Int` operands"))
		}
	})

	t.Run("remainder_has_no_float_and_reports_once", func(t *testing.T) {
		because := "`%` expects `Int` operands"
		// A Float on the left is the one mistake, on that operand, and the rest
		// of the operation says nothing: neither the right operand nor the place
		// where the result is used.
		for _, row := range []struct{ name, src, context string }{
			{"two_floats", "fn main() { let r = 1.0 % 2.0 }", "= 1.0 %"},
			{"float_and_int", "fn main() { let r = 1.0 % 2 }", "= 1.0 %"},
			{"where_a_float_is_expected", "fn main() { let r: Float = 1.0 % 2.0 }", "= 1.0 %"},
			{"where_a_bool_is_expected", "fn main() { let r: Bool = 1.0 % 2.0 }", "= 1.0 %"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p, secondary(mismatch(spanIn(t, p, row.src, row.context, "1.0"), "expected `Int`, found `Float`"),
				spanIn(t, p, row.src, " % ", "%"), because))
		}
		// A Float on the right is the one mistake too, and the operation has the
		// error type.
		for _, row := range []struct{ name, src string }{
			{"int_and_float", "fn main() { let r = 1 % 2.0 }"},
			{"where_a_float_is_expected", "fn main() { let r: Float = 1 % 2.0 }"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p, secondary(mismatch(spanIn(t, p, row.src, "% 2.0", "2.0"), "expected `Int`, found `Float`"),
				spanIn(t, p, row.src, " % ", "%"), because))
		}
		// A left operand that no type has been given waits for the bodies, as that
		// of the other operators does, and the right operand may give it its type:
		// then it is the left operand that is wrong, and the error is on it, with
		// the same reason, once however many `%` it is the operand of.
		for _, row := range []struct{ name, src, context, found string }{
			{"free_left_operand_bound_by_a_float", "fn f(x) -> Float { x % 2.0 }", "x % 2", "Float"},
			{"free_left_operand_bound_by_a_bool", "fn f(x) { let r = x % true }", "x % t", "Bool"},
			{"refused_where_the_right_operand_binds_it", "fn f(x) {\n  let r = x % true\n  let s = x % x\n}", "x % t", "Bool"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p, secondary(mismatch(spanIn(t, p, row.src, row.context, "x"), "expected `Int`, found `"+row.found+"`"),
				spanIn(t, p, row.src, row.context, "%"), because))
		}
		{
			// Two wrong operands are one error: the right one is not compared.
			const src = "fn main() { let r = true % false }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "two Bools", p, secondary(mismatch(spanIn(t, p, src, "= true", "true"), "expected `Int`, found `Bool`"),
				spanIn(t, p, src, " % ", "%"), because))
		}
		{
			// The right operand is still read for its own mistakes.
			const src = "fn main() { let r = 1.0 % (1 + true) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "mistake inside the right operand", p,
				secondary(mismatch(spanIn(t, p, src, "= 1.0", "1.0"), "expected `Int`, found `Float`"), spanIn(t, p, src, " % ", "%"), because),
				secondary(mismatch(spanIn(t, p, src, "+ true", "true"), "expected `Int`, found `Bool`"), spanIn(t, p, src, " + ", "+"), "`+` expects `Int` operands"))
		}
		{
			// A left operand that is not the first expression of the file: the
			// operand that is judged is the one written to the left of the `%`.
			const src = "fn main() {\n  let a = 1\n  let r = 2.5 % 2\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "operand far from the start", p, secondary(mismatch(spanIn(t, p, src, "= 2.5", "2.5"), "expected `Int`, found `Float`"),
				spanIn(t, p, src, " % ", "%"), because))
			p = testsupport.Analyze(t, "fn f() -> Int { 7 + 8 % 3 }")
			wantDiags(t, "a good remainder inside a sum", p)
		}
		// An operand that is the error type says nothing, and neither does the
		// operation: where its result is used, it fits any type.
		for _, row := range []struct {
			src   string
			codes []string
		}{
			{"fn main() { let b: Bool = nope % 1 }", []string{"E0301"}},
			{"fn main() { let b: Bool = 1 % nope }", []string{"E0301"}},
			{"fn main() { let b: Bool = nope % nope }", []string{"E0301", "E0301"}},
		} {
			wantCodes(t, testsupport.Analyze(t, row.src), row.codes...)
		}
		{
			// The `2` on the right of `%` types the parameter: `%` waits for the bodies
			// like `+` (see remainder), and `x + 2` would settle it the same way, to the
			// type of the right operand, here an Int.
			p := testsupport.Analyze(t, "fn f(x) -> Int { x % 2 }")
			wantDiags(t, "free operand", p)
			if got := testsupport.DefType(t, p, "f"); got != "(Int) -> Int" {
				t.Errorf("type of f is %q, want (Int) -> Int", got)
			}
		}
	})

	t.Run("left_operand_that_is_not_a_number_is_one_error", func(t *testing.T) {
		for _, row := range []struct{ name, src, context, word, found, op string }{
			{"bool_plus", "fn main() { let r = true + 1 }", "= true", "true", "Bool", "+"},
			{"bool_less", "fn main() { let r = true < 1 }", "= true", "true", "Bool", "<"},
			{"unit_times", "fn main() { let r = () * 1 }", "= ()", "()", "()", "*"},
			{"function_divided", "fn f() {}\nfn main() { let r = f / 2.0 }", "= f", "f", "() -> ()", "/"},
			// The right operand is not compared with a type that nobody asked for.
			{"both_wrong", "fn main() { let r = true + false }", "= true", "true", "Bool", "+"},
			// Whatever is expected of the result, the operation has the error type.
			{"result_where_a_bool_is_expected", "fn main() { let r: Bool = true + 1 }", "= true", "true", "Bool", "+"},
			{"result_where_a_float_is_expected", "fn main() { let r: Float = true * 1.0 }", "= true", "true", "Bool", "*"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p, rejectedOperand(spanIn(t, p, row.src, row.context, row.word), row.found, numberTypes, row.op, numbersWork))
		}
		{
			// What is inside the right operand is read all the same, and gets its own error.
			const src = "fn main() { let r = true + (1 + true) }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "mistake inside the right operand", p,
				rejectedOperand(spanIn(t, p, src, "= true", "true"), "Bool", numberTypes, "+", numbersWork),
				secondary(mismatch(spanIn(t, p, src, "+ true)", "true"), "expected `Int`, found `Bool`"),
					spanIn(t, p, src, "(1 + true)", "+"), "`+` expects `Int` operands"))
		}
		for _, row := range []struct{ name, src, context, word, found string }{
			{"negated_bool", "fn main() { let r = -true }", "-true", "true", "Bool"},
			{"negated_unit", "fn main() { let r = -() }", "-()", "()", "()"},
			{"negated_bool_where_an_int_is_expected", "fn main() { let r: Int = -true }", "-true", "true", "Bool"},
			{"negated_bool_where_a_bool_is_expected", "fn main() { let r: Bool = -true }", "-true", "true", "Bool"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p, rejectedOperand(spanIn(t, p, row.src, row.context, row.word), row.found, numberTypes, "-", numbersWork))
		}
		{
			// The error type says nothing, and so does the operation.
			p := testsupport.Analyze(t, "fn main() { let b: Bool = nope + 1.0 }")
			wantCodes(t, p, "E0301")
		}
	})

	t.Run("an_operator_waits_for_its_free_left_operand", func(t *testing.T) {
		// The type of `x` is what the rest of the program says it is, so the
		// verdict cannot depend on which function comes first: each program is
		// written in the two orders, and has the same type and the same errors.
		both := func(f, user string) [2]string { return [2]string{f + "\n" + user, user + "\n" + f} }
		for _, row := range []struct {
			name, fn string
			srcs     [2]string
			want     string
		}{
			{"square", "sq", both("fn sq(x) -> Float { x * x }", "fn main() { printFloat(sq(2.0)) }"), "(Float) -> Float"},
			{"half", "half", both("fn half(x) -> Float { x / 2.0 }", "fn main() { printFloat(half(3.0)) }"), "(Float) -> Float"},
			{"negation", "neg", both("fn neg(x) -> Float { -x }", "fn main() { printFloat(neg(1.5)) }"), "(Float) -> Float"},
			{"int_sum", "dbl", both("fn dbl(x) -> Int { x + x }", "fn main() { print(dbl(2)) }"), "(Int) -> Int"},
			{"float_ordering", "lt", both("fn lt(x, y) -> Bool { x < y }", "fn main() { let b = lt(1.0, 2.0) }"), "(Float, Float) -> Bool"},
			{"int_ordering", "lt", both("fn lt(x, y) -> Bool { x < y }", "fn main() { let b = lt(1, 2) }"), "(Int, Int) -> Bool"},
			{"float_equality", "same", both("fn same(x, y) -> Bool { x == y }", "fn main() { let b = same(0.5, 0.5) }"), "(Float, Float) -> Bool"},
			{"chain_of_sums", "total", both("fn total(a, b, c) -> Float { a + b * c }", "fn main() { printFloat(total(1.0, 2.0, 3.0)) }"), "(Float, Float, Float) -> Float"},
			{"remainder", "rem", both("fn rem(x, y) -> Int { x % y }", "fn main() { print(rem(7, 2)) }"), "(Int, Int) -> Int"},
		} {
			for i, src := range row.srcs {
				p := testsupport.Analyze(t, src)
				wantDiags(t, row.name, p)
				if got := testsupport.DefType(t, p, row.fn); got != row.want {
					t.Errorf("%s, order %d: type of %s is %q, want %q", row.name, i, row.fn, got, row.want)
				}
			}
		}

		// Nothing binds the parameters here, and nothing says they are not
		// numbers: the type keeps its variables.
		for _, row := range []struct{ name, src, fn, want string }{
			{"ordering", "fn lt(x, y) -> Bool { x < y }", "lt", "(a, a) -> Bool"},
			{"sum_that_is_not_used", "fn f(x) { let y = x + x }", "f", "(a) -> ()"},
			{"negation_that_is_not_used", "fn f(x) { let y = -x }", "f", "(a) -> ()"},
			{"remainder", "fn rem(x, y) -> Int { x % y }", "rem", "(a, a) -> Int"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p)
			if got := testsupport.DefType(t, p, row.fn); got != row.want {
				t.Errorf("%s: type of %s is %q, want %q", row.name, row.fn, got, row.want)
			}
		}

		// A call that makes the operand a type that the operator does not work
		// on is one error, on the left operand, with the same text whichever
		// function is written first.
		for _, row := range []struct {
			name, fn, user, context, word, found, op string
			expected, works                          string
		}{
			{"bool_sum", "fn f(x) { let y = x + x }", "fn main() { f(true) }", "= x +", "x", "Bool", "+", numberTypes, numbersWork},
			{"bool_ordering", "fn f(x, y) { let b = x < y }", "fn main() { f(true, false) }", "= x <", "x", "Bool", "<", numberTypes, numbersWork},
			{"function_negation", "fn f(x) { let y = -x }\nfn g() {}", "fn main() { f(g) }", "= -x", "x", "() -> ()", "-", numberTypes, numbersWork},
			{"unit_equality", "fn f(x) { let b = x == x }", "fn main() { f(()) }", "= x ==", "x", "()", "==", equatableType, equatableWork},
			// One variable in several operators is one mistake, at the first.
			{"chain_of_sums", "fn f(x) { let y = x * x + x }", "fn main() { f(true) }", "= x *", "x", "Bool", "*", numberTypes, numbersWork},
			// The same variable in operators of different statements, too: the call
			// makes it a Bool, and it is one mistake in the type of `x`, whether the
			// call is checked before the function or after it.
			{"sum_and_negation", "fn f(x) {\n  let a = x + x\n  let b = -x\n}", "fn main() { f(true) }", "= x +", "x", "Bool", "+", numberTypes, numbersWork},
			// The operands of one chain of arithmetic are one variable (a sum has the
			// type of its left operand), so they are one mistake too.
			{"chain_of_three_parameters", "fn f(a, b, c) { let y = a + b + c }", "fn main() { f(true, true, true) }", "= a +", "a", "Bool", "+", numberTypes, numbersWork},
			// An operator that waits does not hold back the ones after it: the first
			// call is fine for `x` and the second is not for `y`, and `y` is judged.
			{"a_refusal_after_an_accepted_operand", "fn f(x) { let a = x + x }\nfn g(y) { let b = y + y }", "fn main() {\n  f(1.0)\n  g(true)\n}",
				"= y +", "y", "Bool", "+", numberTypes, numbersWork},
		} {
			for _, src := range both(row.fn, row.user) {
				p := testsupport.Analyze(t, src)
				wantDiags(t, row.name, p, rejectedOperand(spanIn(t, p, src, row.context, row.word), row.found, row.expected, row.op, row.works))
			}
		}

		// Two parameters that a comparison made one type are still two variables,
		// one bound to the other, and each is reported where an operator refuses it:
		// the mistake is reported once for each variable, not once for each type
		// that a call gives them. (The key of the report is the variable, as it was
		// written; see firstRejection.)
		const cmp = "fn f(x, y) {\n  let c = x == y\n  let a = x + x\n  let b = y + y\n}\nfn main() { f(true, true) }"
		p := testsupport.Analyze(t, cmp)
		wantDiags(t, "unified_by_a_comparison", p,
			rejectedOperand(spanIn(t, p, cmp, "= x +", "x"), "Bool", numberTypes, "+", numbersWork),
			rejectedOperand(spanIn(t, p, cmp, "= y +", "y"), "Bool", numberTypes, "+", numbersWork))

		// `%` is judged in the same way, and says why with the operator as the
		// reason. In the second program the variable is in two `%`, and the error is
		// at the first.
		for _, row := range []struct{ name, fn, user, context, found string }{
			{"float_remainder", "fn r(x, y) -> Int { x % y }", "fn main() { let a = r(1.5, 2.0) }", "{ x %", "Float"},
			{"bool_remainder_twice", "fn r(x, y) {\n  let a = x % y\n  let b = x % y\n}", "fn main() { r(true, false) }", "a = x %", "Bool"},
		} {
			for _, src := range both(row.fn, row.user) {
				p := testsupport.Analyze(t, src)
				wantDiags(t, row.name, p, secondary(mismatch(spanIn(t, p, src, row.context, "x"), "expected `Int`, found `"+row.found+"`"),
					spanIn(t, p, src, row.context, "%"), "`%` expects `Int` operands"))
			}
		}

		// The right operand can make the left one a type that the operator does
		// not work on, and then it is the one mistake, and the operation has the
		// error type wherever its result is used. The variable is reported once,
		// where its right operand binds it, however many operators use it later.
		for _, row := range []struct{ name, src, context, found, op string }{
			{"sum_with_a_bool", "fn f(x) { let r = x + true }", "= x +", "Bool", "+"},
			{"sum_with_a_bool_where_a_bool_is_expected", "fn f(x) { let r: Bool = x + true }", "= x +", "Bool", "+"},
			{"ordering_with_a_bool_where_an_int_is_expected", "fn f(x) { let r: Int = x < true }", "= x <", "Bool", "<"},
			{"product_with_unit", "fn f(x) { let r = x * () }", "= x *", "()", "*"},
			{"sum_with_a_bool_then_another_sum", "fn f(x) {\n  let r = x + true\n  let s = x + x\n}", "= x + t", "Bool", "+"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p, rejectedOperand(spanIn(t, p, row.src, row.context, "x"), row.found, numberTypes, row.op, numbersWork))
		}
		{
			// The same for `==`: refused where its right operand binds it, and
			// then used by a second `==`.
			const src = "fn f() {}\nfn g(x) {\n  let a = x == f\n  let b = x == x\n}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "equality refused where the right operand binds it", p,
				rejectedOperand(spanIn(t, p, src, "= x == f", "x"), "() -> ()", equatableType, "==", equatableWork))
		}
		{
			// The tail of the body binds the operand: the sum is the type of the
			// operand and the function returns a Bool.
			const src = "fn bad(x) -> Bool { x + x }"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "operand bound by the return type", p, rejectedOperand(spanIn(t, p, src, "{ x +", "x"), "Bool", numberTypes, "+", numbersWork))
		}
	})

	t.Run("equality_takes_floats", func(t *testing.T) {
		for _, row := range []struct{ name, src, fn, want string }{
			{"literal_on_the_right", "fn z(x) -> Bool { x == 0.5 }", "z", "(Float) -> Bool"},
			{"literal_on_the_left", "fn z(x) -> Bool { 0.5 != x }", "z", "(Float) -> Bool"},
			{"nan_test", "fn isNaN(x: Float) -> Bool { x != x }", "isNaN", "(Float) -> Bool"},
		} {
			p := testsupport.Analyze(t, row.src)
			wantDiags(t, row.name, p)
			if got := testsupport.DefType(t, p, row.fn); got != row.want {
				t.Errorf("%s: type of %s is %q, want %q", row.name, row.fn, got, row.want)
			}
		}
	})

	t.Run("unknown_type_names_float", func(t *testing.T) {
		// Float is a type now, and the name next to it suggests it.
		{
			const src = "fn f(x: Flot) {}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Flot", p, unknownType(spanIn(t, p, src, ": Flot", "Flot"), "Flot", "Float"))
		}
		{
			const src = "fn f(x: Floot) {}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Floot", p, unknownType(spanIn(t, p, src, ": Floot", "Floot"), "Floot", "Float"))
		}
		{
			const src = "fn f(x: Double) {}"
			p := testsupport.Analyze(t, src)
			wantDiags(t, "Double", p, unknownType(spanIn(t, p, src, ": Double", "Double"), "Double", ""))
		}
	})
}
