package syntax_test

import (
	"reflect"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
	"github.com/aldezex/kilang/internal/testsupport/within"
)

// The tests here are about what chapter 12, session 1 adds to the parser:
// patterns, the `match` that takes values apart with them, and the `let` that
// binds a pattern. They reuse parseFile, wantModule, dataDiags, wantOneE0201 and
// the recRun helpers of the other test files of the package.

// patSrc is a program whose only match has pat as its only arm.
func patSrc(pat string) string { return "fn f() { match x { " + pat + " => 1 } }" }

// patTree is what PrintModule writes for patSrc when the pattern prints as want.
func patTree(want string) string {
	return "(fn f (params) (ret _) (block (match x (arm " + want + " 1))))\n"
}

// firstMatch parses src, which must hold a match, and returns the module, the id of
// the file and the first match in it, and fails the test if the file has no match.
func firstMatch(t *testing.T, src string) (*syntax.Module, base.FileID, syntax.Expr) {
	t.Helper()
	m, id, _, _ := parseFile(t, src)
	matches := dataExprs(m, syntax.ExprMatch)
	if len(matches) == 0 {
		t.Fatalf("%q has no match", src)
	}
	return m, id, matches[0]
}

// armPat returns the pattern of the only arm of the match in patSrc(pat), with its
// module, so that a test can look at the tree and the spans of a pattern.
func armPat(t *testing.T, pat string) (*syntax.Module, syntax.Pat) {
	t.Helper()
	m, _, match := firstMatch(t, patSrc(pat))
	arms := m.Arms(match.Arms)
	if len(arms) != 1 {
		t.Fatalf("%q: %d arms, want 1", pat, len(arms))
	}
	return m, m.Pats.Get(arms[0].Pat)
}

// patText is the text of src that sp covers.
func patText(src string, sp base.Span) string { return src[sp.Lo:sp.Hi] }

func TestParsePat(t *testing.T) {
	t.Run("every_pattern_form_prints_as_written", func(t *testing.T) {
		// Each form of spec 5.3, alone and inside the others. The dump writes a
		// pattern as the program does, with one space after a comma, so a pattern
		// written that way prints as itself; the rows that differ are written
		// loosely, over lines, or with a parenthesis that is no node.
		rows := []struct{ name, src, want string }{
			{"wildcard", "_", "_"},
			{"variable", "x", "x"},
			{"integer", "0", "0"},
			{"negative integer", "-1", "-1"},
			{"integer of several digits", "1_000", "1000"},
			{"negative integer of several digits", "-42", "-42"},
			{"true", "true", "true"},
			{"false", "false", "false"},
			{"constructor without arguments", "None", "None"},
			{"constructor", "Some(x)", "Some(x)"},
			{"constructor with two arguments", "Rect(w, _)", "Rect(w, _)"},
			{"constructor of constructors", "Some(Some(Some(0)))", "Some(Some(Some(0)))"},
			{"record with puns", "Point { x, y }", "Point { x, y }"},
			{"record with patterns", "Point { x: 0, y: p }", "Point { x: 0, y: p }"},
			{"record with a rest", "Point { x, y: _, .. }", "Point { x, y: _, .. }"},
			{"record that is only a rest", "Point { .. }", "Point { .. }"},
			{"record with no fields", "Point {}", "Point { }"},
			{"record over lines", "Point {\n  x,\n  y: p\n}", "Point { x, y: p }"},
			{"record written close", "Point{x,y:p,..}", "Point { x, y: p, .. }"},
			{"tuple", "(a, b)", "(a, b)"},
			{"nested tuples", "(a, (b, c), _)", "(a, (b, c), _)"},
			{"parenthesis is no node", "(a)", "a"},
			{"parenthesis twice", "((a))", "a"},
			{"empty list", "[]", "[]"},
			{"list of one", "[a]", "[a]"},
			{"list", "[a, b]", "[a, b]"},
			{"list with a rest", "[a, ..t]", "[a, ..t]"},
			{"list with an ignored rest", "[a, b, .._]", "[a, b, .._]"},
			{"list that is only a rest", "[..t]", "[..t]"},
			{"list of lists", "[[a], [b, ..t]]", "[[a], [b, ..t]]"},
			{"list of constructors", "[Some(x), None]", "[Some(x), None]"},
			{"or-pattern", "A | B | C", "A | B | C"},
			{"or-pattern in an argument", "Some(1 | 2)", "Some(1 | 2)"},
			{"or-pattern in a tuple", "(A | B, C)", "(A | B, C)"},
			{"or-pattern in a parenthesis", "(A | B)", "A | B"},
			{"or-pattern with a parenthesis on the left", "(A | B) | C", "(A | B) | C"},
			{"or-pattern with a parenthesis on the right", "A | (B | C)", "A | (B | C)"},
			{"a trailing comma in a call", "Some(a,)", "Some(a)"},
			{"everything", "Line { a: (0, Some(x)), b: [_, ..r] | [] }", "Line { a: (0, Some(x)), b: [_, ..r] | [] }"},
		}
		for _, row := range rows {
			wantModule(t, row.name, patSrc(row.src), patTree(row.want))
		}
	})

	t.Run("pattern_nodes", func(t *testing.T) {
		// What each node holds. A literal says which payload is its value, so that
		// `0` and `false` (both zero) are told apart; a record pattern is one even
		// when it names nothing; a parenthesis is the pattern inside it.
		type want struct {
			src  string
			kind syntax.PatKind
		}
		for _, row := range []want{
			{"_", syntax.PatWildcard}, {"x", syntax.PatBind}, {"0", syntax.PatLit}, {"-1", syntax.PatLit},
			{"true", syntax.PatLit}, {"None", syntax.PatCtor}, {"Some(x)", syntax.PatCtor}, {"P {}", syntax.PatCtor},
			{"(a, b)", syntax.PatTuple}, {"(a)", syntax.PatBind}, {"[]", syntax.PatList}, {"[a, ..t]", syntax.PatList},
			{"A | B", syntax.PatOr},
		} {
			if _, p := armPat(t, row.src); p.Kind != row.kind {
				t.Errorf("%q is a pattern of kind %d, want %d", row.src, p.Kind, row.kind)
			}
		}

		m, zero := armPat(t, "0")
		if zero.Lit != syntax.LitInt || zero.Int != 0 || zero.Bool {
			t.Errorf("`0` is %+v, want an Int literal 0", zero)
		}
		_, no := armPat(t, "false")
		if no.Lit != syntax.LitBool || no.Bool || no.Int != 0 {
			t.Errorf("`false` is %+v, want a Bool literal false", no)
		}
		if _, yes := armPat(t, "true"); yes.Lit != syntax.LitBool || !yes.Bool {
			t.Errorf("`true` is %+v, want a Bool literal true", yes)
		}
		for src, n := range map[string]int64{"-5": -5, "- 5": -5, "9223372036854775807": 9223372036854775807, "-9223372036854775807": -9223372036854775807, "-0": 0} {
			if _, p := armPat(t, src); p.Lit != syntax.LitInt || p.Int != n {
				t.Errorf("%q is %+v, want the Int literal %d", src, p, n)
			}
		}

		// A constructor: its arguments, in order, and no record flag.
		m, some := armPat(t, "Some(Pair(a, 1), _)")
		args := m.PatList(some.Args)
		if some.Record || some.HasRest || len(args) != 2 || m.Pats.Get(args[0]).Kind != syntax.PatCtor || m.Pats.Get(args[1]).Kind != syntax.PatWildcard {
			t.Errorf("`Some(Pair(a, 1), _)` is %+v with arguments %v, want two arguments, a constructor and `_`", some, args)
		}
		if _, none := armPat(t, "None"); none.Args.Count != 0 || none.Record || none.Fields.Count != 0 {
			t.Errorf("`None` is %+v, want no arguments and no record flag", none)
		}

		// A record pattern: its fields in the order written, a pun without a pattern.
		m, rec := armPat(t, "Point { x, y: p, .. }")
		fields := m.FieldPats(rec.Fields)
		if !rec.Record || !rec.HasRest || rec.Args.Count != 0 || len(fields) != 2 {
			t.Fatalf("`Point { x, y: p, .. }` is %+v with fields %v, want a record with a rest and two fields", rec, fields)
		}
		if fields[0].HasPat || !fields[1].HasPat || m.Pats.Get(fields[1].Pat).Kind != syntax.PatBind {
			t.Errorf("fields %+v, want a pun and then a field with the pattern `p`", fields)
		}
		if _, empty := armPat(t, "Point {}"); !empty.Record || empty.HasRest || empty.Fields.Count != 0 {
			t.Errorf("`Point {}` is %+v, want a record pattern with no fields and no rest", empty)
		}
		if _, noRest := armPat(t, "Point { x }"); noRest.HasRest {
			t.Errorf("`Point { x }` has a rest")
		}

		// A list: its elements, and the pattern that stands for the rest.
		m, list := armPat(t, "[a, b, ..t]")
		if !list.HasRest || list.Args.Count != 2 || m.Pats.Get(list.Rest).Kind != syntax.PatBind {
			t.Errorf("`[a, b, ..t]` is %+v, want two elements and a variable for the rest", list)
		}
		m, ignored := armPat(t, "[a, .._]")
		if !ignored.HasRest || m.Pats.Get(ignored.Rest).Kind != syntax.PatWildcard {
			t.Errorf("`[a, .._]` is %+v, want a wildcard for the rest", ignored)
		}
		if _, plain := armPat(t, "[a, b]"); plain.HasRest || plain.Args.Count != 2 {
			t.Errorf("`[a, b]` is %+v, want two elements and no rest", plain)
		}
		if _, only := armPat(t, "[..t]"); !only.HasRest || only.Args.Count != 0 {
			t.Errorf("`[..t]` is %+v, want no elements and a rest", only)
		}
	})

	t.Run("an_or_pattern_of_three_is_one_node", func(t *testing.T) {
		// Three alternatives are one node with three, and not two nested nodes of two:
		// with two, the E0703 of Resolve would be said twice, with another second label,
		// and a hundred thousand alternatives would be a tree a hundred thousand levels
		// deep for every pass that walks it. An or-pattern in a parenthesis stays a node
		// of its own, because the parenthesis says how it groups.
		m, or := armPat(t, "A | B | C")
		alts := m.PatList(or.Args)
		if len(alts) != 3 {
			t.Fatalf("`A | B | C` has %d alternatives, want 3", len(alts))
		}
		for i, want := range []string{"A", "B", "C"} {
			if got := m.Pats.Get(alts[i]); got.Kind != syntax.PatCtor || patText(patSrc("A | B | C"), got.NameSpan) != want {
				t.Errorf("alternative %d is %+v, want the constructor %s", i, got, want)
			}
		}
		m, grouped := armPat(t, "(A | B) | C")
		alts = m.PatList(grouped.Args)
		if grouped.Kind != syntax.PatOr || len(alts) != 2 || m.Pats.Get(alts[0]).Kind != syntax.PatOr || m.Pats.Get(alts[1]).Kind != syntax.PatCtor {
			t.Errorf("`(A | B) | C` is %+v with alternatives %v, want two alternatives, the first an or-pattern", grouped, alts)
		}
		m, right := armPat(t, "A | (B | C)")
		alts = m.PatList(right.Args)
		if len(alts) != 2 || m.Pats.Get(alts[0]).Kind != syntax.PatCtor || m.Pats.Get(alts[1]).Kind != syntax.PatOr {
			t.Errorf("`A | (B | C)` has alternatives %v, want a constructor and an or-pattern", alts)
		}
		// Inside an argument it is one pattern too.
		m, some := armPat(t, "Some(A | B)")
		if args := m.PatList(some.Args); len(args) != 1 || m.Pats.Get(args[0]).Kind != syntax.PatOr {
			t.Errorf("`Some(A | B)` has arguments %v, want one or-pattern", args)
		}
	})

	t.Run("spans_of_patterns", func(t *testing.T) {
		// A pattern covers the tokens it was read from; a parenthesis is not part of
		// the pattern inside it, a tuple covers its own; a constructor covers its
		// closer; a negative number covers its sign. Names have a span of their own.
		rows := []struct{ src, covers string }{
			{"_", "_"}, {"abc", "abc"}, {"123", "123"}, {"-12", "-12"}, {"- 12", "- 12"}, {"true", "true"},
			{"None", "None"}, {"Some(x)", "Some(x)"}, {"Some( x , y )", "Some( x , y )"},
			{"Point { x, y }", "Point { x, y }"}, {"Point { .. }", "Point { .. }"},
			{"(a, b)", "(a, b)"}, {"( a )", "a"}, {"((a, b))", "(a, b)"},
			{"[a, ..t]", "[a, ..t]"}, {"[]", "[]"}, {"A | B | C", "A | B | C"}, {"(A | B)", "A | B"},
		}
		for _, row := range rows {
			src := patSrc(row.src)
			_, id, _ := firstMatch(t, src)
			_, p := armPat(t, row.src)
			if p.Span.File != id || patText(src, p.Span) != row.covers {
				t.Errorf("%q: the pattern covers %q (%+v), want %q", row.src, patText(src, p.Span), p.Span, row.covers)
			}
		}
		for _, row := range []struct{ src, name string }{{"abc", "abc"}, {"Some(x)", "Some"}, {"Point { x }", "Point"}, {"None", "None"}} {
			src := patSrc(row.src)
			if _, p := armPat(t, row.src); patText(src, p.NameSpan) != row.name {
				t.Errorf("%q: the name covers %q, want %q", row.src, patText(src, p.NameSpan), row.name)
			}
		}
		// The pattern that stands for the rest of a list covers its name, or the `_`.
		const list = "[a, ..tail]"
		m, lp := armPat(t, list)
		if rest := m.Pats.Get(lp.Rest); patText(patSrc(list), rest.Span) != "tail" || patText(patSrc(list), rest.NameSpan) != "tail" {
			t.Errorf("the rest of `%s` covers %q and its name %q, want `tail` twice", list, patText(patSrc(list), rest.Span), patText(patSrc(list), rest.NameSpan))
		}
		m, lp = armPat(t, "[.._]")
		if rest := m.Pats.Get(lp.Rest); patText(patSrc("[.._]"), rest.Span) != "_" {
			t.Errorf("the rest of `[.._]` covers %q, want `_`", patText(patSrc("[.._]"), rest.Span))
		}
		// The span of a field is its name and its pattern, or only the name.
		const rec = "Point { x, y: Some(p) }"
		src := patSrc(rec)
		m, p := armPat(t, rec)
		fields := m.FieldPats(p.Fields)
		if patText(src, fields[0].Span) != "x" || patText(src, fields[1].Span) != "y: Some(p)" {
			t.Errorf("the fields cover %q and %q, want `x` and `y: Some(p)`", patText(src, fields[0].Span), patText(src, fields[1].Span))
		}
	})

	t.Run("arms_and_guards", func(t *testing.T) {
		// An arm is a pattern, a guard if it has one, and a body; the arms are in the
		// order written; a guard is an expression up to the `=>`; an arm spans from
		// its pattern to the end of its body.
		rows := []struct{ name, src, want string }{
			{"no arms", "match x { }", "(match x)"},
			{"one arm", "match x { _ => 1 }", "(match x (arm _ 1))"},
			{"two arms", "match x { 0 => 1\n 1 => 2 }", "(match x (arm 0 1) (arm 1 2))"},
			{"guard", "match x { n if n > 0 => n }", "(match x (arm n (guard (> n 0)) n))"},
			{"guard with a call and a record", "match x { p if f(p) && p == Point { x: 1 } => 1 }",
				"(match x (arm p (guard (&& (call f p) (== p (record Point (x 1))))) 1))"},
			{"guard on an or-pattern", "match x { A | B if c => 1 }", "(match x (arm A | B (guard c) 1))"},
			{"guard and no guard", "match x { n if n < 0 => 1\n _ => 2 }", "(match x (arm n (guard (< n 0)) 1) (arm _ 2))"},
			{"body with operators", "match x { Some(v) => v + 1 * 2 }", "(match x (arm Some(v) (+ v (* 1 2))))"},
			{"body is a block", "match x { _ => { let y = 1; y } }", "(match x (arm _ (block (let y _ 1) y)))"},
			{"body is an if", "match x { _ => if a { 1 } else { 2 } }", "(match x (arm _ (if a (block 1) (block 2))))"},
			{"body is a match", "match x { A => match y { B => 1 } }", "(match x (arm A (match y (arm B 1))))"},
			{"body is a record", "match x { _ => Point { x: 1 } }", "(match x (arm _ (record Point (x 1))))"},
			{"scrutinee is a call", "match f(x, 1) { _ => 1 }", "(match (call f x 1) (arm _ 1))"},
			{"scrutinee is a field", "match p.x { _ => 1 }", "(match (field p x) (arm _ 1))"},
			{"scrutinee is a tuple", "match (a, b) { (x, y) => 1 }", "(match (tuple a b) (arm (x, y) 1))"},
			{"scrutinee has operators", "match a + b * 2 { _ => 1 }", "(match (+ a (* b 2)) (arm _ 1))"},
			{"scrutinee is a match", "match match x { _ => y } { _ => 1 }", "(match (match x (arm _ y)) (arm _ 1))"},
			{"scrutinee is a name followed by arms", "match Origin { Origin => 1 }", "(match Origin (arm Origin 1))"},
			{"match as an operand", "1 + match x { _ => 2 }", "(+ 1 (match x (arm _ 2)))"},
			{"match in a call", "g(match x { _ => 2 }, 3)", "(call g (match x (arm _ 2)) 3)"},
			{"access on a match", "match x { _ => p }.y", "(field (match x (arm _ p)) y)"},
			{"match in a condition", "if match x { _ => true } { 1 } else { 2 }", "(if (match x (arm _ true)) (block 1) (block 2))"},
			{"match as a let initializer", "let y = match x { _ => 1 }", "(let y _ (match x (arm _ 1)))"},
			{"the head is still a head after a match", "if match x { _ => 1 } == Origin { 1 } else { 2 }",
				"(if (== (match x (arm _ 1)) Origin) (block 1) (block 2))"},
			{"a record in an arm of a match in a head", "if match x { _ => Origin { x: 1 }.x == 1 } { 1 } else { 2 }",
				"(if (match x (arm _ (== (field (record Origin (x 1)) x) 1))) (block 1) (block 2))"},
			{"a record in a guard of a match in a head", "if match x { _ if Origin { x: 1 }.x == 1 => true } { 1 } else { 2 }",
				"(if (match x (arm _ (guard (== (field (record Origin (x 1)) x) 1)) true)) (block 1) (block 2))"},
		}
		for _, row := range rows {
			wantModule(t, row.name, "fn f() { "+row.src+" }", "(fn f (params) (ret _) (block "+row.want+"))\n")
		}

		const src = "fn f() { match x { Some(n) if n > 0 => n + 1\n _ => 0 } }"
		m, id, match := firstMatch(t, src)
		arms := m.Arms(match.Arms)
		if len(arms) != 2 || !arms[0].HasGuard || arms[1].HasGuard {
			t.Fatalf("arms %+v, want two, the first with a guard and the second without", arms)
		}
		if got := patText(src, arms[0].Span); arms[0].Span.File != id || got != "Some(n) if n > 0 => n + 1" {
			t.Errorf("the first arm covers %q, want `Some(n) if n > 0 => n + 1`", got)
		}
		if got := patText(src, m.Exprs.Get(arms[0].Guard).Span); got != "n > 0" {
			t.Errorf("the guard covers %q, want `n > 0`", got)
		}
		if got := patText(src, m.Exprs.Get(arms[0].Body).Span); got != "n + 1" {
			t.Errorf("the body covers %q, want `n + 1`", got)
		}
		if got := patText(src, arms[1].Span); got != "_ => 0" {
			t.Errorf("the second arm covers %q, want `_ => 0`", got)
		}
		if got := patText(src, match.Span); got != "match x { Some(n) if n > 0 => n + 1\n _ => 0 }" {
			t.Errorf("the match covers %q, want all of it up to its `}`", got)
		}
		if got := patText(src, m.Exprs.Get(match.Scrutinee).Span); got != "x" {
			t.Errorf("the scrutinee covers %q, want `x`", got)
		}
	})

	t.Run("let_with_a_pattern", func(t *testing.T) {
		// A `let` whose pattern is a plain name is the `let` of Part I, with its name;
		// any other pattern is in Pat, with no name, and it may have a type.
		rows := []struct{ name, src, want string }{
			{"tuple", "let (a, b) = t", "(let (a, b) _ t)"},
			{"wildcard", "let _ = f()", "(let _ _ (call f))"},
			{"record", "let Point { x, y } = p", "(let Point { x, y } _ p)"},
			{"record with a rest", "let Point { x, .. } = p", "(let Point { x, .. } _ p)"},
			{"list", "let [a, ..t] = xs", "(let [a, ..t] _ xs)"},
			{"constructor", "let Some(x) = o", "(let Some(x) _ o)"},
			{"literal", "let 0 = n", "(let 0 _ n)"},
			{"true", "let true = b", "(let true _ b)"},
			{"false", "let false = b", "(let false _ b)"},
			{"negative literal", "let -1 = n", "(let -1 _ n)"},
			{"with a type", "let (a, b): (Int, Int) = t", "(let (a, b) (tuple Int Int) t)"},
			{"initializer is a match", "let (a, b) = match x { _ => t }", "(let (a, b) _ (match x (arm _ t)))"},
			{"plain name", "let x = 1", "(let x _ 1)"},
			{"plain name with a type", "let x: Int = 1", "(let x Int 1)"},
			{"two in a row", "let (a, b) = t\n  let c = a", "(let (a, b) _ t) (let c _ a)"},
		}
		for _, row := range rows {
			wantModule(t, row.name, "fn f() { "+row.src+" }", "(fn f (params) (ret _) (block "+row.want+"))\n")
		}
		const src = "fn f() { let (a, b): (Int, Int) = t\n  let c = 1 }"
		m, id, _, _ := parseFile(t, src)
		var lets []syntax.Stmt
		for _, s := range m.Stmts.All() {
			lets = append(lets, s)
		}
		if len(lets) != 2 {
			t.Fatalf("%d statements, want 2", len(lets))
		}
		withPat, plain := lets[0], lets[1]
		if !withPat.HasPat || withPat.Name != base.NoSymbol || withPat.NameSpan != (base.Span{}) || !withPat.HasType {
			t.Errorf("the `let` with a pattern is %+v, want HasPat, no name and a type", withPat)
		}
		if got := patText(src, m.Pats.Get(withPat.Pat).Span); got != "(a, b)" {
			t.Errorf("the pattern covers %q, want `(a, b)`", got)
		}
		if got := patText(src, withPat.Span); withPat.Span.File != id || got != "let (a, b): (Int, Int) = t" {
			t.Errorf("the statement covers %q, want `let (a, b): (Int, Int) = t`", got)
		}
		if plain.HasPat || plain.Name == base.NoSymbol || patText(src, plain.NameSpan) != "c" {
			t.Errorf("the plain `let` is %+v, want a name and no pattern", plain)
		}
	})

	t.Run("the_line_rule_in_arms", func(t *testing.T) {
		// Arms end at a line break or a `;` (spec 5.2). A line that ends in `=>` or
		// `|` is not complete, so the body or the next alternative goes on below it;
		// a guard has to be on the line of its pattern, and a line that starts with
		// `|` is not a continuation. Blank lines and comments are only space.
		const arms = "(match x (arm A 1) (arm B | C 2) (arm D (block 3)) (arm E 4) (arm F 5) (arm _ 6))"
		wantModule(t, "arms over lines", "fn f() { match x {\n  A => 1\n\n  // a comment\n  B |\n    C => 2\n  D => {\n    3\n  }\n  E => 4; F => 5\n  _ =>\n    6\n} }",
			"(fn f (params) (ret _) (block "+arms+"))\n")
		wantModule(t, "the closing brace on the last arm's line", "fn f() { match x { A => 1\n B => 2 } }",
			"(fn f (params) (ret _) (block (match x (arm A 1) (arm B 2))))\n")
		wantModule(t, "a semicolon after the last arm", "fn f() { match x { A => 1; } }", "(fn f (params) (ret _) (block (match x (arm A 1))))\n")
		wantModule(t, "a match as the tail and as a statement", "fn f() { match a { _ => 1 }\n match b { _ => 2 } }",
			"(fn f (params) (ret _) (block (expr (match a (arm _ 1))) (match b (arm _ 2))))\n")
		wantModule(t, "a body continues with a method on the next line", "fn f() { match x {\n  A => y\n    .z\n  B => 2 } }",
			"(fn f (params) (ret _) (block (match x (arm A (field y z)) (arm B 2))))\n")
		// A `|` that starts a line is a new arm, which has no pattern: an arm without
		// its `=>`, and then the error.
		r := recParse(t, "fn f() { match x {\n  A\n  | B => 1 } }")
		r.wantDiags(t, "a line that starts with a bar", recE0201(r.at(t, "\n", 1), "expected `=>`, found newline"))
	})

	t.Run("large_matches_and_patterns_are_flat", func(t *testing.T) {
		// A hundred thousand arms, and a tuple, a list and an or-pattern of a hundred
		// thousand elements, are read with no error, and the tree stays flat: one node
		// with a list of that many children, which no pass walks by recursion. The arms
		// are constructors, so a counter of depth that is not given back would pass 256.
		// The stack of Go is capped at 1 MiB for the whole test, because the default
		// limit (1 GB) holds a hundred thousand frames and a parser that read the arms
		// or the elements by recursion would pass without anyone noticing; past the cap
		// Go ends the test binary (the tests of this package do not run in parallel).
		defer debug.SetMaxStack(debug.SetMaxStack(1 << 20))
		const n = 100000
		var b strings.Builder
		b.WriteString("fn f() { match x {\n")
		for range n {
			b.WriteString("  Some(0) => 1\n")
		}
		b.WriteString("  _ => 0\n} }")
		m, _, match := firstMatch(t, b.String())
		if arms := m.Arms(match.Arms); len(arms) != n+1 || m.Pats.Get(arms[n].Pat).Kind != syntax.PatWildcard {
			t.Errorf("%d arms, want %d, the last a wildcard", len(arms), n+1)
		}
		if d := dataDiagsOf(t, b.String()); len(d) != 0 {
			t.Errorf("a match of %d arms reports %v", n+1, d[0])
		}
		for _, row := range []struct {
			name, open, sep, last, close string
			kind                         syntax.PatKind
			rest                         bool
		}{
			{"tuple", "(", ", ", "a", ")", syntax.PatTuple, false},
			{"list", "[", ", ", "a, ..t", "]", syntax.PatList, true},
			{"or-pattern", "", " | ", "A", "", syntax.PatOr, false},
		} {
			pat := row.open + strings.Repeat("A"+row.sep, n-1) + row.last + row.close
			if row.kind == syntax.PatList {
				pat = row.open + strings.Repeat("a"+row.sep, n-1) + row.last + row.close
			}
			m, p := armPat(t, pat)
			if p.Kind != row.kind || int(p.Args.Count) != n || p.HasRest != row.rest {
				t.Errorf("%s of %d elements is %+v, want kind %d with %d arguments", row.name, n, p, row.kind, n)
			}
			if len(dataDiagsOf(t, patSrc(pat))) != 0 || len(m.Arms(dataExprs(m, syntax.ExprMatch)[0].Arms)) != 1 {
				t.Errorf("%s of %d elements: diagnostics or arms that are not one", row.name, n)
			}
		}
	})

	t.Run("a_deep_pattern_is_one_e0204", func(t *testing.T) {
		// 256 levels are fine and the 257th is one E0204, at the token that opens it
		// (a pattern of 10 000 levels is one error, not 9 744), whatever is nested:
		// constructors, tuples, parentheses, lists and records. The arm is skipped to
		// its end and the next arm is read, so the counter is given back on the way out.
		forms := []struct {
			name, open, inner, close, opener string
			roundTrip                        bool
		}{
			{"constructor", "Some(", "x", ")", "(", true},
			{"tuple", "(", "x", ", y)", "(", true},
			{"parenthesis", "(", "x", ")", "(", false},
			{"list", "[", "x", "]", "[", true},
			{"record", "P { f: ", "x", " }", "{", true},
		}
		const head = "fn f() { match x {\n  "
		for _, f := range forms {
			nest := func(n int) string { return strings.Repeat(f.open, n) + f.inner + strings.Repeat(f.close, n) }
			if d := dataDiagsOf(t, patSrc(nest(256))); len(d) != 0 {
				t.Errorf("%s nested 256 levels: %v, want no diagnostic", f.name, d[0])
			}
			if got := printModule(t, patSrc(nest(256))); f.roundTrip && got != patTree(nest(256)) {
				t.Errorf("%s nested 256 levels does not print as written", f.name)
			}
			for _, n := range []int{257, 10000} {
				r := recParse(t, head+nest(n)+" => 1\n  _ => 2 } }")
				lo := len(head) + 256*len(f.open) + strings.Index(f.open, f.opener)
				want := diag.Diagnostic{
					Severity: diag.Error, Code: diag.E(204), Message: "pattern is nested too deeply",
					Primary: diag.Label{Span: r.span(lo, lo+1), Message: "nesting level 257 starts here"},
					Notes:   []string{"the limit is 256"},
					Help:    "match the inner part with a second `match` in the body of the arm",
				}
				r.wantDiags(t, f.name+" nested "+strconv.Itoa(n)+" levels", want)
				if got := r.text(); !strings.HasSuffix(got, "(arm (error) (error)) (arm _ 2))))\n") {
					t.Errorf("%s nested %d levels: printed %s, want the broken arm and then `(arm _ 2)`", f.name, n, recShow(got))
				}
			}
		}
		// A pattern that is deep in an arm does not spend the levels of the next
		// ones, and one that is deep in a `let` is skipped with the statement.
		r := recParse(t, head+strings.Repeat("Some(", 300)+"x"+strings.Repeat(")", 300)+" => 1\n  "+strings.Repeat("Some(", 256)+"x"+strings.Repeat(")", 256)+" => 2 } }")
		if len(r.sink.All()) != 1 || !strings.Contains(r.text(), "(arm (error) (error)) (arm Some(Some(") {
			t.Errorf("a deep arm and then a pattern of 256 levels: %v and %s", r.sink.All(), recShow(r.text()))
		}
		// A level is given back whatever happens inside it: 300 siblings of each kind of
		// pattern in a tuple are no depth, and neither are three arms that go wrong 200
		// levels down, which would pass 256 together if each kept its levels.
		for _, sibling := range []string{"Some(a)", "(a, b)", "[a]", "P { x }", "(a)"} {
			if d := dataDiagsOf(t, patSrc("("+strings.Repeat(sibling+", ", 300)+"z)")); len(d) != 0 {
				t.Errorf("300 siblings %s in a tuple: %v, want no diagnostic", sibling, d[0])
			}
		}
		for _, f := range forms {
			broken := strings.Repeat(f.open, 200) + "[a, ..]" + strings.Repeat(f.close, 200)
			if f.name == "tuple" {
				broken = strings.Repeat("(", 200) + "[a, ..]" + strings.Repeat(", y)", 200)
			}
			r := recParse(t, "fn f() { match x {\n  "+broken+" => 1\n  "+broken+" => 2\n  "+broken+" => 3 } }")
			got := r.sink.All()
			if len(got) != 3 || got[0].Message != "expected identifier or `_`, found `]`" || got[1].Message != got[0].Message || got[2].Message != got[0].Message {
				t.Errorf("three %s arms that go wrong 200 levels down: %v, want three E0201 for the rest of the list", f.name, got)
			}
		}
		r = recParse(t, "fn f() {\n  let "+strings.Repeat("(", 300)+"x"+strings.Repeat(", y)", 300)+" = t\n  let z = 1\n}")
		if len(r.sink.All()) != 1 || r.text() != "(fn f (params) (ret _) (block (error) (let z _ 1)))\n" {
			t.Errorf("a deep `let` pattern: %v and %s, want one E0204 and the next statement intact", r.sink.All(), recShow(r.text()))
		}
		// A `let` has no body for the second `match`: its help says where it goes, and
		// the help of the arm, which names the body of the arm, comes back with the next
		// arm after it.
		if got := r.sink.All(); len(got) == 1 && got[0].Help != "match the inner part with a second `match` after the `let`" {
			t.Errorf("a deep `let` pattern: help %q, want the one that says to match after the `let`", got[0].Help)
		}
		r = recParse(t, "fn f() {\n  let "+strings.Repeat("(", 300)+"x"+strings.Repeat(", y)", 300)+" = t\n  match t {\n    "+strings.Repeat("Some(", 300)+"x"+strings.Repeat(")", 300)+" => 1\n  }\n}")
		if got := r.sink.All(); len(got) != 2 || got[0].Help != "match the inner part with a second `match` after the `let`" ||
			got[1].Help != "match the inner part with a second `match` in the body of the arm" {
			t.Errorf("a deep `let` pattern and then a deep arm: %v, want the help of the `let` and then the help of the arm", got)
		}
	})

	t.Run("a_broken_arm_is_one_diagnostic_and_the_others_are_read", func(t *testing.T) {
		// Three arms with a mistake each give three diagnostics, not one for the
		// rest of the match nor one for each token. A broken arm stays in the list,
		// with an error pattern or body where it went wrong, so the arms around it are
		// the arms the program has.
		r := recParse(t, "fn f() { match x {\n  A => 1\n  => 2\n  B => 3\n  C 4\n  D => 5\n  [a, ..] => 6\n  E => 7\n} }")
		r.wantText(t, "three broken arms", "(fn f (params) (ret _) (block (match x (arm A 1) (arm (error) (error)) (arm B 3) (arm C (error)) (arm D 5) (arm (error) (error)) (arm E 7))))\n")
		got := r.sink.All()
		if len(got) != 3 || got[0].Message != "expected pattern, found `=>`" || got[1].Message != "expected `=>`, found `4`" ||
			got[2].Message != "expected identifier or `_`, found `]`" {
			t.Errorf("diagnostics %v, want one for each of the three arms", got)
		}
		for i, want := range []base.Span{r.at(t, "=>", 1), r.at(t, "4", 0), r.at(t, "]", 0)} {
			if got[i].Primary.Span != want {
				t.Errorf("diagnostic %d is at %v, want %v", i, got[i].Primary.Span, want)
			}
		}
		// The arms are found by their spans: a broken arm covers what was read.
		m, _, match := firstMatch(t, r.src)
		arms := m.Arms(match.Arms)
		if len(arms) != 7 || patText(r.src, arms[3].Span) != "C" || patText(r.src, arms[2].Span) != "B => 3" {
			t.Errorf("%d arms, want 7 with `B => 3` and `C` among them", len(arms))
		}
		// Each arm that went wrong says so, and the ones that did not, do not: what the
		// passes that judge the whole match (the arms it lacks, the ones no value
		// reaches) have to leave alone is a match with one of them.
		for i, want := range []bool{false, true, false, true, false, true, false} {
			if i < len(arms) && arms[i].Broken != want {
				t.Errorf("arm %d has Broken %v, want %v", i, arms[i].Broken, want)
			}
		}
		// A `)` that is missing in a body lets it swallow the arms after it: the arm that
		// has the mistake is the broken one, and the match that remains is not the
		// program's.
		const swallowed = "fn f() { match e {\n  Num(n) => n\n  Add(a, b) => g(a) + g(b\n  Mul(a, b) => g(a) * g(b)\n  Neg(a) => 0 - g(a)\n} }"
		m, _, match = firstMatch(t, swallowed)
		if arms := m.Arms(match.Arms); len(arms) != 2 || arms[0].Broken || !arms[1].Broken {
			t.Errorf("a body that swallows the arms after it: %d arms, Broken %v, want 2, the second broken", len(arms), arms)
		}

		// A mistake in the body leaves the arm's pattern, and the next arm is read.
		r = recParse(t, "fn f() { match x {\n  A => 1 +\n  B => 2\n} }")
		r.wantText(t, "a body that stops", "(fn f (params) (ret _) (block (match x (arm A (+ 1 B)) (arm (error) (error)))))\n")
		// A closing brace that is missing is E0202 at the `{` of the match, and the
		// arms read until then are in the tree.
		r = recParse(t, "fn f() { match x {\n  A => 1\n  B => 2\n")
		r.wantDiags(t, "unclosed match", r.e0202At(r.at(t, "{", 1), "{", uint32(len(r.src))))
		r.wantText(t, "unclosed match", "(fn f (params) (ret _) (block (match x (arm A 1) (arm B 2))))\n")
		// A `{` that is missing after the scrutinee is one E0201 however many arms
		// follow, the arms being where they should be: they are read as the arms they
		// are, in the tree, and their variables are not statements for the later passes
		// to call unknown names. The same with a `)` where the `{` goes.
		for _, row := range []struct{ name, head, found string }{
			{"no brace", "match x\n", "newline"}, {"a parenthesis", "match x )\n", "`)`"},
		} {
			for _, n := range []int{1, 3, 40, 1000} {
				r := recParse(t, "fn f() {\n  let y = 1\n  "+row.head+strings.Repeat("  Some(a) => a\n", n)+"} }")
				if got := r.sink.All(); len(got) != 1 || got[0].Code != diag.E(201) || got[0].Message != "expected `{`, found "+row.found {
					t.Errorf("%s and %d arms: %v, want one E0201 for the `{`", row.name, n, got)
				}
				matches := dataExprs(r.m, syntax.ExprMatch)
				if len(matches) != 1 || len(r.m.Arms(matches[0].Arms)) != n {
					t.Errorf("%s and %d arms: %d matches, want one with %d arms", row.name, n, len(matches), n)
				}
			}
		}
		// The file ending inside a match with no `{` is the one mistake of the `{` and,
		// unless it is too close to say so, the E0202 of the function's own brace, and
		// not one for a brace that was never there. The junk before the end is skipped.
		for _, row := range []struct {
			src, found  string
			diags, open int // the `{` that the E0202 is at: 0 is the function's, 1 the match's
		}{
			{"fn f() { match x\n  A => 1\n  B => 2\n", "newline", 2, 0}, {"fn f() { match x )", "`)`", 1, 0},
			{"fn f() { match x y { A => 1\n  B => 2\n", "`y`", 2, 1},
		} {
			r = recParse(t, row.src)
			got := r.sink.All()
			if len(got) != row.diags || got[0].Message != "expected `{`, found "+row.found || (row.diags == 2 && (got[1].Code != diag.E(202) || got[1].Primary.Span != r.at(t, "{", row.open))) {
				t.Errorf("%q: %v, want the E0201 and, for %d diagnostics, the E0202 of the `{` number %d", row.src, got, row.diags, row.open)
			}
		}
		// A `let` that begins a line where an arm should be is no arm: it is what the
		// block holds after a match whose `}` was left out. It ends the match, with the
		// E0201 of the `}` that is missing and the edit that writes it, and what follows
		// is the block's: one mistake is one diagnostic however many statements come
		// after it, and the edit leaves a program with no diagnostic. A `fn` that begins
		// a line ends it the same way.
		closeAt := func(r *recRun, tok string, found, open base.Span, after int) diag.Diagnostic {
			d := recE0201Opened(found, "expected `}`, found `"+tok+"`", open, "{")
			d.Help = "close it with `}`"
			d.Fixits = []diag.FixIt{diag.Insert(r.span(after, after), "}")}
			return d
		}
		for _, k := range []int{1, 3, 50} {
			src := "fn f() {\n  let a = match x {\n    A => 1\n" + strings.Repeat("  let b = a\n", k) + "  a\n}"
			r = recParse(t, src)
			lo := int(r.at(t, "let b", 0).Lo)
			want := closeAt(r, "let", r.span(lo, lo+3), r.at(t, "{", 1), strings.Index(src, "A => 1")+len("A => 1"))
			r.wantDiags(t, strconv.Itoa(k)+" statements after a match with no `}`", want)
			r.wantText(t, strconv.Itoa(k)+" statements after a match with no `}`",
				"(fn f (params) (ret _) (block (let a _ (match x (arm A 1))) "+strings.Repeat("(let b _ a) ", k)+"a))\n")
			if again := recParse(t, applyFixits(src, want)); len(again.sink.All()) != 0 {
				t.Errorf("%d statements: the edit of the `}` leaves %v", k, again.sink.All())
			}
		}
		r = recParse(t, "fn f() {\n  let a = match x {\n    A => 1\nfn g() {}\n")
		lo := int(r.at(t, "fn g", 0).Lo)
		if got := r.sink.All(); len(got) == 0 || !reflect.DeepEqual(got[0], closeAt(r, "fn", r.span(lo, lo+2), r.at(t, "{", 1), strings.Index(r.src, "A => 1")+len("A => 1"))) {
			t.Errorf("a `fn` after the arms: %v, want the E0201 of the `}` first", got)
		}
		if matches := dataExprs(r.m, syntax.ExprMatch); len(matches) != 1 || len(r.m.Arms(matches[0].Arms)) != 1 {
			t.Errorf("a `fn` after the arms: %d matches, want one with its one arm", len(matches))
		}
		// With the `}` put as a `(`, the pattern that the `(` starts is the one mistake:
		// it ends at the `let`, which then ends the match (reported at the same token,
		// so no second diagnostic), and the statements after it are the block's.
		r = recParse(t, "fn f() {\n  let a = match x {\n    A => 1\n  (\n  let b = 2\n  a\n}")
		if got := r.sink.All(); len(got) != 1 || got[0].Message != "expected pattern, found `let`" || got[0].Primary.Span != r.span(strings.Index(r.src, "let b"), strings.Index(r.src, "let b")+3) {
			t.Errorf("a `(` for the `}`: %v, want one E0201 for a pattern at the `let`", got)
		}
		r.wantText(t, "a `(` for the `}`", "(fn f (params) (ret _) (block (let a _ (match x (arm A 1) (arm (error) (error)))) (let b _ 2) a))\n")
		// A line that only a statement can begin ends the match in the same way: a call,
		// a field access, an operand of an operator, an `if`, a `match` and a block. The
		// statement is in the tree after the match, and one mistake is one diagnostic
		// however many statements follow it, which a missing `}` costs on an `if` too.
		for _, row := range []struct{ name, stmt, tree string }{
			{"a call", "print(3)", "(expr (call print 3))"}, {"a field", "p.x", "(expr (field p x))"},
			{"an operand", "a + b", "(expr (+ a b))"}, {"an operand of `||`", "a || b", "(expr (|| a b))"}, {"an if", "if c { 1 } else { 2 }", "(expr (if c (block 1) (block 2)))"},
			{"a match", "match y { _ => 1 }", "(expr (match y (arm _ 1)))"}, {"a block", "{ 1 }", "(expr (block 1))"},
		} {
			for _, k := range []int{1, 3, 50} {
				src := "fn f() {\n  match x {\n    A => 1\n" + strings.Repeat("  "+row.stmt+"\n", k) + "  a\n}"
				r = recParse(t, src)
				lo := strings.Index(src, "\n  "+row.stmt+"\n") + 3
				word := row.stmt[:strings.IndexAny(row.stmt+" ", " (.")]
				want := closeAt(r, word, r.span(lo, lo+len(word)), r.at(t, "{", 1), strings.Index(src, "A => 1")+len("A => 1"))
				r.wantDiags(t, row.name+" after a match with no `}`", want)
				r.wantText(t, row.name+" after a match with no `}`", "(fn f (params) (ret _) (block (expr (match x (arm A 1))) "+strings.Repeat(row.tree+" ", k)+"a))\n")
				if again := recParse(t, applyFixits(src, want)); len(again.sink.All()) != 0 {
					t.Errorf("%s, %d statements: the edit of the `}` leaves %v", row.name, k, again.sink.All())
				}
			}
		}
		// A name that is a variable pattern, with the arrow missing or something that
		// cannot go after a name on its line, is an arm that went wrong and no more:
		// the one diagnostic is the arrow, and the arms after it are read.
		for _, row := range []struct{ name, src, tree string }{
			{"a number after the name", "fn f() { match v {\n  A => 1\n  x 4\n  B => 2\n} }", "(fn f (params) (ret _) (block (match v (arm A 1) (arm x (error)) (arm B 2))))\n"},
			{"a name alone", "fn f() { match v {\n  A => 1\n  x\n  (a, b) => 2\n} }", "(fn f (params) (ret _) (block (match v (arm A 1) (arm x (error)) (arm (a, b) 2))))\n"},
		} {
			r = recParse(t, row.src)
			if got := r.sink.All(); len(got) != 1 || got[0].Code != diag.E(201) || !strings.HasPrefix(got[0].Message, "expected `=>`, found ") {
				t.Errorf("%s: %v, want one E0201 for the missing `=>`", row.name, got)
			}
			r.wantText(t, row.name, row.tree)
		}
		// A line with a `=>` outside every bracket is an arm that went wrong, whatever it
		// starts with: a constructor written in lowercase or a guard with no `if` (which
		// start like a call and like an operand), the words that only a statement can
		// begin, and a block. The one diagnostic is the one of the arm, which is in the
		// list, marked as broken, and the arms after it are read: the match is not cut
		// short there, and nothing after it is read as a statement.
		for _, row := range []struct{ name, arm, msg, tree string }{
			{"a lowercase constructor", "rect(w, h) => 2", "expected `=>`, found `(`", "(arm rect (error))"},
			{"a guard with no `if`", "n > 0 => 2", "expected `=>`, found `>`", "(arm n (error))"},
			{"a name and a field", "p.x => 2", "expected `=>`, found `.`", "(arm p (error))"},
			{"a `let`", "let y => 2", "expected pattern, found `let`", "(arm (error) (error))"},
			{"an `if`", "if c => 2", "expected pattern, found `if`", "(arm (error) (error))"},
			{"a `match`", "match y => 2", "expected pattern, found `match`", "(arm (error) (error))"},
			{"a block", "{ y } => 2", "expected pattern, found `{`", "(arm (error) (error))"},
			{"a call and a list", "f(a) [b] => 2", "expected `=>`, found `(`", "(arm f (error))"},
			{"a lowercase constructor over two lines", "rect(w,\n      h) => 2", "expected `=>`, found `(`", "(arm rect (error))"},
		} {
			// In the first place, in the middle and in the last, with the match as the
			// initializer of a `let`, where the lines after it are the statements of the
			// block.
			for _, at := range []int{0, 1, 2} {
				arms := slices.Insert([]string{"A => 1", "B => 3"}, at, row.arm)
				src := "fn f() {\n  let m = match x {\n    " + strings.Join(arms, "\n    ") + "\n  }\n  m\n}"
				r = recParse(t, src)
				what := row.name + " as arm " + strconv.Itoa(at)
				if got := r.sink.All(); len(got) != 1 || got[0].Code != diag.E(201) || got[0].Message != row.msg {
					t.Errorf("%s: %v, want one E0201 %q", what, got, row.msg)
				}
				tree := slices.Insert([]string{"(arm A 1)", "(arm B 3)"}, at, row.tree)
				r.wantText(t, what, "(fn f (params) (ret _) (block (let m _ (match x "+strings.Join(tree, " ")+")) m))\n")
				m, _, match := firstMatch(t, src)
				for i, arm := range m.Arms(match.Arms) {
					if arm.Broken != (i == at) {
						t.Errorf("%s: arm %d has Broken %v, want %v", what, i, arm.Broken, i == at)
					}
				}
			}
		}
		// An arrow does not make an arm of a line when it is inside a bracket that the line
		// opens, after a closer that nothing on the line opened, after a `;` or after the
		// `=` of a `let` (a lambda is its initializer): the line is still what only a
		// statement can begin, and it ends the match, whose `}` is the first thing that is
		// wrong. The file ending in the line is the same.
		for _, stmt := range []string{"f(a => 1)", "f(a) [b => 1]", "f(a)(b => 1)", "f(a)) => 1", "f(a); b => 1", "let f = x => 1", "let y"} {
			src := "fn f() {\n  match x {\n    A => 1\n  " + stmt
			if stmt != "let y" {
				src += "\n}"
			}
			r = recParse(t, src)
			got := r.sink.All()
			if len(got) == 0 || got[0].Message != "expected `}`, found `"+stmt[:strings.IndexAny(stmt, " (")]+"`" {
				t.Errorf("%q after the arms: %v, want the E0201 for the `}` first", stmt, got)
			}
			if matches := dataExprs(r.m, syntax.ExprMatch); len(matches) != 1 || len(r.m.Arms(matches[0].Arms)) != 1 {
				t.Errorf("%q after the arms: %d matches, want one with its one arm", stmt, len(matches))
			}
		}
		// A match in the arm of another one ends at the statement, and so does the outer
		// one, which also lacks its `}`: the second E0201 is at the same token, so it is
		// not said again, and the statement is the block's.
		r = recParse(t, "fn f() {\n  match a {\n    A => match b {\n      X => 1\n  print(3)\n}")
		if got := r.sink.All(); len(got) != 1 || got[0].Message != "expected `}`, found `print`" {
			t.Errorf("a match in an arm: %v, want one E0201 for the `}`", got)
		}
		r.wantText(t, "a match in an arm", "(fn f (params) (ret _) (block (expr (match a (arm A (match b (arm X 1))))) (call print 3)))\n")
		// A `let` that does not begin a line is a mistake in an arm and no more: the
		// arm is skipped to its end and the next one is read.
		r = recParse(t, "fn f() { match x {\n  A => 1; let y = 2\n  B => 3\n} }")
		if got := r.sink.All(); len(got) != 1 || got[0].Message != "expected pattern, found `let`" {
			t.Errorf("a `let` after a `;`: %v, want one E0201 for a pattern", got)
		}
		r.wantText(t, "a `let` after a `;`", "(fn f (params) (ret _) (block (match x (arm A 1) (arm (error) (error)) (arm B 3))))\n")
		// A match with no `{` has nothing to close, and says nothing at the `let`.
		r = recParse(t, "fn f() {\n  match x\n    A => 1\n  let y = 2\n  y\n}")
		r.wantDiags(t, "a `let` after the arms of a match with no `{`", recE0201(r.at(t, "\n", 1), "expected `{`, found newline"))
		r.wantText(t, "a `let` after the arms of a match with no `{`", "(fn f (params) (ret _) (block (expr (match x (arm A 1))) (let y _ 2) y))\n")
		// A semicolon where an arm should start is skipped.
		r = recParse(t, "fn f() { match x { ; A => 1 } }")
		if got := r.sink.All(); len(got) != 1 || got[0].Code != diag.E(201) || got[0].Message != "expected pattern, found `;`" || got[0].Primary.Span != r.at(t, ";", 0) {
			t.Errorf("a stray `;`: %v, want one E0201 for a pattern at the `;`", got)
		}
	})

	t.Run("a_pattern_that_cannot_be_read_covers_what_was_read", func(t *testing.T) {
		// An error pattern covers the tokens that were consumed for it, or nothing, an
		// empty span at the token found, when none was: the spans of the tree are what
		// every later pass labels. A Float that is no pattern, and an Int or Float that the
		// lexer rejected, cover their literal (with the sign). A `let` whose pattern cannot be read is an
		// error statement that covers the `let` and what was read.
		rows := []struct{ pat, covers string }{
			{"=> 1", ""}, {"A | => 1", "A |"}, {"- y => 1", "-"}, {"Some(x => 1", "Some(x"}, {"(a, => 1", "(a,"},
			{"[a, ..] => 1", "[a, .."}, {"P { x: } => 1", "P { x: }"}, {"1.5 => 1", "1.5"}, {"-1.5 => 1", "-1.5"},
			{"99999999999999999999 => 1", "99999999999999999999"}, {"-99999999999999999999 => 1", "-99999999999999999999"}, {"1e999 => 1", "1e999"},
		}
		for _, row := range rows {
			src := "fn f() { match x { " + row.pat + " } }"
			m, id, match := firstMatch(t, src)
			pat := m.Pats.Get(m.Arms(match.Arms)[0].Pat)
			lo := strings.Index(src, row.pat)
			if pat.Kind != syntax.PatError || pat.Span.File != id || patText(src, pat.Span) != row.covers ||
				(row.covers == "" && int(pat.Span.Lo) != lo) {
				t.Errorf("%q: the pattern is %+v, want an error that covers %q", row.pat, pat, row.covers)
			}
		}
		const src = "fn f() { let (a, = 1 }"
		m, id, _, _ := parseFile(t, src)
		var lets []syntax.Stmt
		for _, s := range m.Stmts.All() {
			lets = append(lets, s)
		}
		if len(lets) != 1 || lets[0].Kind != syntax.StmtError || lets[0].Span.File != id || patText(src, lets[0].Span) != "let (a," {
			t.Errorf("a `let` with a pattern that is cut short is %+v, want an error statement that covers `let (a,`", lets)
		}
	})

	t.Run("e0201_sites_of_patterns", func(t *testing.T) {
		// What the parser says where a pattern or the arm that holds it goes wrong:
		// the message with the token found, and the primary label on that token. Each
		// source has exactly one diagnostic, whatever the rest of the arm holds.
		rows := []struct{ name, src, context, found, message, tree string }{
			{"no pattern", "fn f() { match x { => 1 } }", "=>", "=>", "expected pattern, found `=>`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"the unit is no pattern", "fn f() { match x { () => 1 } }", "{ () =>", ")", "expected pattern, found `)`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a string is no pattern yet", "fn f() { match x { \"s\" => 1 } }", "\"s\"", "\"s\"", "expected pattern, found `\"s\"`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a character is no pattern yet", "fn f() { match x { 'a' => 1 } }", "'a'", "'a'", "expected pattern, found `'a'`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a trailing comma in a tuple", "fn f() { match x { (a,) => 1 } }", ",)", ")", "expected pattern, found `)`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a constructor with empty parentheses", "fn f() { match x { Rect() => 1 } }", "Rect()", ")", "expected pattern, found `)`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"nothing after a bar", "fn f() { match x { A | => 1 } }", "| =>", "=>", "expected pattern, found `=>`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a minus and no number", "fn f() { match x { - y => 1 } }", "- y", "y", "expected number, found `y`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a minus before a name", "fn f() { match x { -y => 1 } }", "-y", "y", "expected number, found `y`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"an argument that is not closed", "fn f() { match x { Some(x => 1 } }", "=>", "=>", "expected `)`, found `=>`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a tuple that is not closed", "fn f() { match x { (a, b => 1 } }", "=>", "=>", "expected `)`, found `=>`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a list that is not closed", "fn f() { match x { [a, b => 1 } }", "=>", "=>", "expected `]`, found `=>`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a trailing comma in a list", "fn f() { match x { [a,] => 1 } }", ",]", "]", "expected pattern, found `]`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"an element after the rest", "fn f() { match x { [..t, a] => 1 } }", ",", ",", "expected `]`, found `,`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a field that is not a name", "fn f() { match x { P { 1 } => 1 } }", "1", "1", "expected identifier, found `1`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a field with no pattern", "fn f() { match x { P { x: } => 1 } }", "} =>", "}", "expected pattern, found `}`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a rest with a name in a record", "fn f() { match x { P { x, ..p } => 1 } }", "p }", "p", "expected `}`, found `p`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"two fields with no comma", "fn f() { match x { P { x y } => 1 } }", "y }", "y", "expected `}`, found `y`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a leading bar", "fn f() { match x { | A => 1 } }", "| A", "|", "expected pattern, found `|`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a parenthesis that is not closed", "fn f() { match x { (a => 1 } }", "=>", "=>", "expected `)`, found `=>`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"a tuple cut short", "fn f() { match x { (a, => 1 } }", "=> 1", "=>", "expected pattern, found `=>`", "(fn f (params) (ret _) (block (match x (arm (error) (error)))))\n"},
			{"no arrow", "fn f() { match x { A 1 } }", "1", "1", "expected `=>`, found `1`", "(fn f (params) (ret _) (block (match x (arm A (error)))))\n"},
			{"no body", "fn f() { match x { A => } }", "} }", "}", "expected expression, found `}`", "(fn f (params) (ret _) (block (match x (arm A (error)))))\n"},
			{"a comma between arms", "fn f() { match x { A => 1, B => 2 } }", ",", ",", "expected newline, found `,`", "(fn f (params) (ret _) (block (match x (arm A 1) (arm (error) (error)))))\n"},
			{"an arrow in a guard", "fn f() { match x { A if => 1 } }", "=> 1", "=>", "expected expression, found `=>`", "(fn f (params) (ret _) (block (match x (arm A (guard (error)) 1))))\n"},
			{"no brace after the scrutinee", "fn f() { match x\n{ _ => 1 } }", "\n", "\n", "expected `{`, found newline", "(fn f (params) (ret _) (block (expr (error)) (block (error))))\n"},
			{"no brace and three arms", "fn f() { match x\n  A => 1\n  B => 2\n  C => 3\n} }", "x\n", "\n", "expected `{`, found newline", "(fn f (params) (ret _) (block (match x (arm A 1) (arm B 2) (arm C 3))))\n"},
			{"a parenthesis for the brace", "fn f() { match x )\n  A => 1\n  B => 2\n} }", "x )", ")", "expected `{`, found `)`", "(fn f (params) (ret _) (block (match x (arm A 1) (arm B 2))))\n"},
			{"the first arm on the line of the scrutinee", "fn f() { match x A => 1\n  B => 2\n} }", "x A", "A", "expected `{`, found `A`", "(fn f (params) (ret _) (block (match x (arm A 1) (arm B 2))))\n"},
			{"junk before the brace", "fn f() {\n  let a = 1\n  let b = 2\n  match x y { A => 1\n  B => 2 }\n}", "x y", "y", "expected `{`, found `y`", "(fn f (params) (ret _) (block (let a _ 1) (let b _ 2) (match x (arm A 1) (arm B 2))))\n"},
			{"a parenthesis before the brace", "fn f() {\n  let a = 1\n  let b = 2\n  match x ) { A => 1\n  B => 2 }\n}", "x )", ")", "expected `{`, found `)`", "(fn f (params) (ret _) (block (let a _ 1) (let b _ 2) (match x (arm A 1) (arm B 2))))\n"},
			{"an arm with a block on the line of the scrutinee", "fn f() { match x A => { 1 }\n  B => 2\n} }", "x A", "A", "expected `{`, found `A`", "(fn f (params) (ret _) (block (match x (arm A (block 1)) (arm B 2))))\n"},
			{"a block after the match", "fn f() { { match x } { 1 } }", "x }", "}", "expected `{`, found `}`", "(fn f (params) (ret _) (block (block (error))))\n"},
			{"a brace on the next line inside a call", "fn f() { g(match x ,\n  { 1 }) }", "x ,", ",", "expected `{`, found `,`", "(fn f (params) (ret _) (block (call g (error) (block 1))))\n"},
			{"a variable as the first arm after a missing brace", "fn sign(n: Int) -> Int { match n\n  x if x > 0 => 1\n  _ => 0\n} }", "n\n", "\n", "expected `{`, found newline", "(fn sign (params (n Int)) (ret Int) (block (match n (arm x (guard (> x 0)) 1) (arm _ 0))))\n"},
			{"a match with no keyword", "fn f() { s {\n  A => 1\n  B => 2\n} }", "s {", "s", "expected `match`, found `s`", "(fn f (params) (ret _) (block (match s (arm A 1) (arm B 2))))\n"},
			{"a match with no keyword in a let", "fn f() { let a = s {\n  A => 1\n  B => 2\n} }", "= s {", "s", "expected `match`, found `s`", "(fn f (params) (ret _) (block (let a _ (match s (arm A 1) (arm B 2)))))\n"},
			{"a block after a statement", "fn f() { s { 1 } }", "s {", "{", "expected newline, found `{`", "(fn f (params) (ret _) (block (expr s) (block 1)))\n"},
			{"a call around the match", "fn f() { g(match x ,\n  A => 1\n  B => 2\n} ) }", "x ,", ",", "expected `{`, found `,`", "(fn f (params) (ret _) (block (call g (match x (arm A 1) (arm B 2)))))\n"},
			{"a semicolon where the brace goes", "fn f() { match x;\n  a => 1 }", "x;", ";", "expected `{`, found `;`", "(fn f (params) (ret _) (block (expr (error)) (expr a) (error)))\n"},
			{"the brace of the block where the brace goes", "fn f() { { match x }\n  a }", "x }", "}", "expected `{`, found `}`", "(fn f (params) (ret _) (block (expr (block (error))) a))\n"},
			{"a let with no pattern", "fn f() { let (a, = 1 }", "=", "=", "expected pattern, found `=`", "(fn f (params) (ret _) (block (error)))\n"},
			{"a let with a broken record", "fn f() { let P { x: } = p }", "} =", "}", "expected pattern, found `}`", "(fn f (params) (ret _) (block (error)))\n"},
			// A parenthesis that nothing closes hides the line breaks up to the `}` of the
			// block, and the line that starts with a `(` or a `-` is not a call of what
			// is above it nor a subtraction from it: it is the next statement.
			{"a let with a tuple not closed and a tuple after it", "fn f() {\n  let (a, b, = t\n  let k = 1\n  (b, a)\n}", "= t", "=", "expected pattern, found `=`", "(fn f (params) (ret _) (block (error) (let k _ 1) (tuple b a)))\n"},
			{"a group not closed and a tuple after it", "fn f() {\n  let a = ( t.0\n  let k = 1\n  (t.1, a)\n}", "let k", "let", "expected `)`, found `let`", "(fn f (params) (ret _) (block (let a _ (error)) (let k _ 1) (tuple (index t 1) a)))\n"},
			{"a call not closed and a negation after it", "fn f() {\n  let b = g(a\n  let k = 1\n  - k\n}", "let k", "let", "expected `)`, found `let`", "(fn f (params) (ret _) (block (let b _ (error)) (let k _ 1) (neg k)))\n"},
			{"a field after a call not closed, which continues the line", "fn f() {\n  let b = g(a\n  let k = 1\n  k\n  .x\n}", "let k", "let", "expected `)`, found `let`", "(fn f (params) (ret _) (block (let b _ (error)) (let k _ 1) (field k x)))\n"},
			{"a tuple not closed after a block inside a call", "fn f() {\n  g({ 1 })\n  let a = (b, c\n  let k = 1\n  (a, k)\n}", "let k", "let", "expected `)`, found `let`", "(fn f (params) (ret _) (block (expr (call g (block 1))) (let a _ (error)) (let k _ 1) (tuple a k)))\n"},
			{"a block inside a call after a tuple not closed", "fn f() {\n  g(x, {\n    let (a, b, = t\n    let k = 1\n    (b, a)\n  })\n}", "= t", "=", "expected pattern, found `=`", "(fn f (params) (ret _) (block (call g x (block (error) (let k _ 1) (tuple b a)))))\n"},
		}
		for _, row := range rows {
			_, id, diags := dataDiags(t, row.src)
			lo := uint32(strings.Index(row.src, row.context) + strings.Index(row.context, row.found))
			want := base.Span{File: id, Lo: lo, Hi: lo + uint32(len(row.found))}
			if len(diags) != 1 || diags[0].Code != diag.E(201) || diags[0].Message != row.message || diags[0].Primary.Span != want ||
				diags[0].Primary.Message != primaryOf(row.message) {
				t.Errorf("%s: %q gives %+v, want exactly one E0201 %q at %v", row.name, row.src, diags, row.message, want)
			}
			// The broken arm is an arm with an error where it went wrong, so the tree
			// is the one that the error leaves and not a pattern that was read half.
			m, _, in, _ := parseFile(t, row.src)
			if got := syntax.PrintModule(m, in); got != row.tree {
				t.Errorf("%s: %q printed %s, want %s", row.name, row.src, got, row.tree)
			}
		}

		// Inside a parenthesis that is open a line break is only a space, so a `(` or a
		// `-` that begins a line goes on with what is above it: a call and a subtraction.
		for _, row := range []struct{ name, src, tree string }{
			{"a call", "fn f() { g(a\n  (1)) }", "(fn f (params) (ret _) (block (call g (call a 1))))\n"},
			{"a subtraction", "fn f() { (a\n  - 1) }", "(fn f (params) (ret _) (block (- a 1)))\n"},
			{"a tuple of calls", "fn f() { (a\n  (1), b\n  (2)) }", "(fn f (params) (ret _) (block (tuple (call a 1) (call b 2))))\n"},
		} {
			r := recParse(t, row.src)
			if got := r.sink.All(); len(got) != 0 {
				t.Errorf("%s: %q gives %v, want no diagnostic", row.name, row.src, got)
			}
			r.wantText(t, row.name, row.tree)
		}

		// Parentheses with nothing in them say what a constructor writes: the name alone
		// for one with no fields, a pattern for each field otherwise. A tuple has no
		// name to say it with.
		for _, row := range []struct{ src, help string }{
			{"fn f() { match x { Dot() => 1 } }", "write `Dot` alone if it has no fields, or a pattern for each field: `Dot(…)`"},
			{"fn f() { match x { Rect() => 1 } }", "write `Rect` alone if it has no fields, or a pattern for each field: `Rect(…)`"},
			{"fn f() { match x { () => 1 } }", ""},
		} {
			r := recParse(t, row.src)
			if got := r.sink.All(); len(got) != 1 || got[0].Message != "expected pattern, found `)`" || got[0].Help != row.help || len(got[0].Fixits) != 0 {
				t.Errorf("%q: %+v, want one E0201 with the help %q and no edit", row.src, got, row.help)
			}
		}

		// A `match` whose keyword was left out is the one mistake, however many arms it
		// has: E0201 at the first token of the statement, which writes the keyword in
		// front of it in the edit, and the arms are the arms of a match on the value
		// that came first. The edit leaves a program with no diagnostic.
		kwSrc := "fn f() {\n  s {\n    A => 1\n  }\n}"
		kw := recParse(t, kwSrc)
		first := kw.at(t, "s", 0)
		wantMatch := recE0201(first, "expected `match`, found `s`")
		wantMatch.Help = "a `{` followed by arms belongs to a `match`: write the keyword before the value they take apart"
		wantMatch.Fixits = []diag.FixIt{diag.Insert(first, "match ")}
		kw.wantDiags(t, "a match with no keyword", wantMatch)
		kw.wantText(t, "a match with no keyword", "(fn f (params) (ret _) (block (match s (arm A 1))))\n")
		if again := recParse(t, applyFixits(kwSrc, wantMatch)); len(again.sink.All()) != 0 {
			t.Errorf("the edit of the keyword leaves %v", again.sink.All())
		}
		for _, n := range []int{1, 3, 40, 1000} {
			r := recParse(t, "fn f() {\n  let y = 1\n  g(y) {\n"+strings.Repeat("    Some(a) => a\n", n)+"  }\n}")
			if got := r.sink.All(); len(got) != 1 || got[0].Code != diag.E(201) || got[0].Message != "expected `match`, found `g`" {
				t.Errorf("no keyword and %d arms: %v, want one E0201 for the `match`", n, got)
			}
			matches := dataExprs(r.m, syntax.ExprMatch)
			if len(matches) != 1 || len(r.m.Arms(matches[0].Arms)) != n {
				t.Errorf("no keyword and %d arms: %d matches, want one with %d arms", n, len(matches), n)
			}
		}
		// A statement that spans lines has no edit for the keyword (an edit stays on
		// one line), and the E0201 is the same.
		kw = recParse(t, "fn f() {\n  g(1,\n    2) {\n    A => 1\n  }\n}")
		if got := kw.sink.All(); len(got) != 1 || got[0].Message != "expected `match`, found `g`" || len(got[0].Fixits) != 0 || got[0].Help == "" {
			t.Errorf("a statement on two lines: %+v, want the E0201 and its help, with no edit", got)
		}
		// A `{` after a statement that is not followed by arms is what it was: E0201
		// for a newline, the block a statement of its own. Not arms: a `=>` inside a
		// bracket, and on a later line than the first.
		for _, row := range []struct{ name, src string }{
			{"a block", "fn f() { s { 1 } }"}, {"a block with a call", "fn f() { s { g(a => 1) } }"},
			{"a block of lines", "fn f() {\n  s {\n    1\n    A => 2\n  }\n}"}, {"an empty block", "fn f() { s { } }"},
			{"a block on a `;`", "fn f() { s { A; => 2 } }"}, {"a bracket after a block", "fn f() { s { 1 } (a => 2) }"},
			// The file ending after what could start arms: there is no `=>` to be found.
			{"the end of the file after a name", "fn f() { s { A"}, {"the end of the file after a call", "fn f() { s { A(x)"},
			{"the end of the file inside a bracket", "fn f() { s { 1 + ("}, {"the end of the file in a let", "fn f() { let a = s { Some(a)"},
		} {
			r := recParse(t, row.src)
			if got := r.sink.All(); len(got) == 0 || got[0].Message != "expected newline, found `{`" || len(dataExprs(r.m, syntax.ExprMatch)) != 0 {
				t.Errorf("%s: %v, %d matches, want the E0201 for a newline first and no match", row.name, got, len(dataExprs(r.m, syntax.ExprMatch)))
			}
		}

		// A `{` on the line after a statement does not belong to it, whatever it
		// holds, and neither does the one after an expression that has a mistake in it
		// already (`s.` has no field name): it is a block of its own, and not the arms of
		// a match.
		for _, src := range []string{"fn f() {\n  s\n  { A => 1 }\n}", "fn f() { s. {\n  A => 1\n} }"} {
			kw = recParse(t, src)
			if len(dataExprs(kw.m, syntax.ExprMatch)) != 0 {
				t.Errorf("%q: %d matches, want none", src, len(dataExprs(kw.m, syntax.ExprMatch)))
			}
		}

		// The file ending inside a delimiter of a pattern is E0202 at that delimiter,
		// with the edit that closes it, and not "expected pattern, found end of file".
		for _, row := range []struct{ src, open string }{
			{"fn f() { match x { Some(a, ", "("}, {"fn f() { match x { Some(", "("}, {"fn f() { match x { (a, ", "("},
			{"fn f() { match x { [a, ", "["}, {"fn f() { match x { [a, ..", "["}, {"fn f() { match x { P { x, ", "{"},
			{"fn f() { match x { P { x: ", "{"}, {"fn f() { let (a, ", "("},
		} {
			r := recParse(t, row.src)
			opener := strings.LastIndex(row.src, row.open)
			if row.src[opener:] == "{ x, " || strings.HasSuffix(row.src, "P { x: ") {
				opener = strings.LastIndex(row.src, "{")
			}
			if got := r.sink.All(); len(got) != 1 || got[0].Code != diag.E(202) || got[0].Primary.Span != r.span(opener, opener+1) {
				t.Errorf("%q: diagnostics %v, want one E0202 at the `%s` at %d", row.src, got, row.open, opener)
			}
		}

		// A bare `..` in a list says what to write: the edit puts the `_` after it, and
		// the program it makes has no diagnostic.
		src := "fn f() { match x { [a, ..] => 1 } }"
		r := recParse(t, src)
		dots := r.at(t, "..", 0)
		close := r.at(t, "]", 0)
		wantDiag := diag.Diagnostic{
			Severity: diag.Error, Code: diag.E(201), Message: "expected identifier or `_`, found `]`",
			Primary: diag.Label{Span: close, Message: "expected identifier or `_` here"},
			Help:    "write `.._` to skip the rest of the list",
			Fixits:  []diag.FixIt{diag.Insert(base.Span{File: r.id, Lo: dots.Hi, Hi: dots.Hi}, "_")},
		}
		r.wantDiags(t, "a bare rest", wantDiag)
		if again := dataDiagsOf(t, applyFixits(src, wantDiag)); len(again) != 0 {
			t.Errorf("the edit leaves %+v", again)
		}
		// Anything else after the `..` has no edit.
		r = recParse(t, "fn f() { match x { [a, ..1] => 1 } }")
		if got := r.sink.All(); len(got) != 1 || got[0].Message != "expected identifier or `_`, found `1`" || got[0].Help != "" || len(got[0].Fixits) != 0 {
			t.Errorf("a number after the rest: %+v, want one E0201 with no help and no edit", got)
		}
	})

	t.Run("a_float_literal_is_e0706", func(t *testing.T) {
		// A Float is no pattern: one E0706 on the literal, with the note and the help
		// and no edit, and the arm is read to its end, so the mistakes after it are
		// reported (here an arm that comes after it, and an unknown arrow in the next).
		// The help writes the literal as the program has it, with its sign.
		// The help of a `let` says that a `let` has no guard, the one of an arm that has a
		// guard says to join the comparison to it, and the one of a chain of alternatives
		// to compare each of them. The name in them is one that the file does not use.
		const armHelp, letHelp = "bind it and compare in a guard: ", "a `let` has no guard: match the value with a `match` and compare in a guard: "
		const guardHelp, altHelp = "bind it and add the comparison to the guard: ", "bind it and compare each alternative in a guard: "
		e0706Help := func(sp base.Span, help string) diag.Diagnostic {
			return diag.Diagnostic{
				Severity: diag.Error, Code: diag.E(706), Message: "a `Float` literal cannot be a pattern",
				Primary: diag.Label{Span: sp, Message: "not a pattern"},
				Notes:   []string{"`==` on floats is not an equivalence (`NaN != NaN`, `0.0 == -0.0`), so a float cannot be matched by its value"},
				Help:    help,
			}
		}
		e0706With := func(sp base.Span, literal, help, name string) diag.Diagnostic {
			return e0706Help(sp, help+"`"+name+" if "+name+" == "+literal+"`")
		}
		// patSrc has a scrutinee called x, so the name that a help gets there is x1.
		e0706 := func(sp base.Span, literal string) diag.Diagnostic { return e0706With(sp, literal, armHelp, "x1") }
		r := recParse(t, "fn f(x: Float) -> Int { match x { 1.5 => 1 \n _ => 0 } }\nfn main() {}")
		r.wantDiags(t, "the example", e0706(r.at(t, "1.5", 0), "1.5"))
		r.wantText(t, "the example", "(fn f (params (x Float)) (ret Int) (block (match x (arm (error) 1) (arm _ 0))))\n(fn main (params) (ret _) (block))\n")

		for _, row := range []struct{ name, pat, literal string }{
			{"negative", "-1.5", "-1.5"}, {"negative with a space", "- 1.5", "-1.5"}, {"an exponent", "2.5e3", "2.5e3"},
			{"zero", "0.0", "0.0"}, {"with an underscore", "1_0.5", "1_0.5"},
		} {
			r := recParse(t, patSrc(row.pat))
			r.wantDiags(t, row.name, e0706(r.at(t, row.pat, 0), row.literal))
		}
		// Where a pattern may be, a Float may not: each is one E0706 on its own literal,
		// and the pattern around it is read all the same (the error is a pattern).
		for _, row := range []struct{ name, pat, text, tree string }{
			{"in a constructor", "Some(1.5)", "1.5", "Some((error))"},
			{"in a tuple", "(0, 2.5)", "2.5", "(0, (error))"},
			{"in a list", "[1.5, ..t]", "1.5", "[(error), ..t]"},
			{"in a record", "P { x: 1.5, y }", "1.5", "P { x: (error), y }"},
			{"as an alternative of a tuple", "((1, 2) | (3, 2.5))", "2.5", "(1, 2) | (3, (error))"},
			{"after a minus in a tuple", "(a, -0.5)", "-0.5", "(a, (error))"},
		} {
			r := recParse(t, patSrc(row.pat))
			r.wantDiags(t, row.name, e0706(r.at(t, row.text, 0), row.text))
			r.wantText(t, row.name, patTree(row.tree))
		}
		// A Float in a `let` is the same mistake, and the statement is a `let` still.
		r = recParse(t, "fn f() { let 1.5 = 1 }")
		r.wantDiags(t, "let", e0706With(r.at(t, "1.5", 0), "1.5", letHelp, "x"))
		r.wantText(t, "let", "(fn f (params) (ret _) (block (let (error) _ 1)))\n")
		// The name of the help is one that the file does not use anywhere: not one that
		// the pattern has already, before the literal or after it, nor the one of the
		// scrutinee, the guard, the body or the initializer, which the variable that the
		// help writes would hide, nor a field's. A `let` after an arm has the arm's help
		// no more.
		for _, row := range []struct {
			name, src, help, literal, want string
			more                           bool // other diagnostics follow the E0706
		}{
			{"a name before the pattern", "fn f() {\n  let x = 1\n  match v { (a, 2.5) => 1 } }", armHelp, "2.5", "x1", false},
			{"the scrutinee", "fn f() {\n  let a = 1\n  let b = 2\n  match x {\n    2.5 => 1 } }", armHelp, "2.5", "x1", false},
			{"a pattern after other arms", "fn f() { match v {\n  A => 1\n  B => 2\n  2.5 => 3 } }", armHelp, "2.5", "x", false},
			{"a name on the next line, with no arrow", "fn f() { match v { 2.5\n  x => 1 } }", armHelp, "2.5", "x1", true},
			{"a name after, on a later line", "fn f() {\n  let a = 1\n  match v { (2.5, x) => 1 } }", armHelp, "2.5", "x1", false},
			{"the end of the file", "fn f() { match v { 2.5", armHelp, "2.5", "x", true},
			{"a name after a `;`", "fn f() { let 2.5; x }", letHelp, "2.5", "x1", true},
			// What follows the pattern is not the pattern's: the guard of the next arm, after
			// a `;` or on the next line, and a body that starts with an `if`, give no guard to
			// this one.
			{"a guard after a `;`", "fn f() { match v { 2.5; y if c => 1 } }", armHelp, "2.5", "x", true},
			{"a guard on the next line", "fn f() { match v { 2.5\n  y if c => 1 } }", armHelp, "2.5", "x", true},
			{"a body that starts with an `if`", "fn f() { match v { 2.5 => if c { 1 } else { 2 } } }", armHelp, "2.5", "x", false},
			{"a name in the body", "fn f() { match v { 2.5 => x } }", armHelp, "2.5", "x1", false},
			{"a name in the initializer", "fn f() { let 2.5 = x }", letHelp, "2.5", "x1", false},
			{"a name on the line of the next arm", "fn f() { match v { 2.5 => 1\n  x => 2 } }", armHelp, "2.5", "x1", false},
			{"a tuple over two lines", "fn f() { match v { (2.5,\n  x) => 1 } }", armHelp, "2.5", "x1", false},
			{"a name before", patSrc("(x, 2.5)"), armHelp, "2.5", "x1", false},
			{"a name after", patSrc("(2.5, x)"), armHelp, "2.5", "x1", false},
			{"two names", patSrc("(x1, x, 2.5)"), armHelp, "2.5", "x2", false},
			{"a name in an alternative", patSrc("(x, 0) | (2.5, y)"), armHelp, "2.5", "x1", false},
			{"a field that is called x", "fn f() { match v { P { x: 2.5 } => 1 } }", armHelp, "2.5", "x1", false},
			{"a pun that is called x", patSrc("P { x, y: 2.5 }"), armHelp, "2.5", "x1", false},
			{"a let", "fn f() { let (x, 0.5) = t }", letHelp, "0.5", "x1", false},
			{"an arm after a let", "fn f() { let (a, b) = t\n  match t { 1.5 => 1 } }", armHelp, "1.5", "x", false},
		} {
			r := recParse(t, row.src)
			want := e0706With(r.at(t, row.literal, 0), row.literal, row.help, row.want)
			if row.more {
				// The E0706 is the first diagnostic and the only one of its code.
				got := r.sink.All()
				e0706s := 0
				for _, d := range got {
					if d.Code == diag.E(706) {
						e0706s++
					}
				}
				if len(got) == 0 || !reflect.DeepEqual(got[0], want) || e0706s != 1 {
					t.Errorf("%s: %q gives %+v, want the E0706 %+v first and no other", row.name, row.src, got, want)
				}
				continue
			}
			r.wantDiags(t, row.name, want)
		}
		// The variable that the help writes hides nothing: the arm of a function with a
		// parameter called x gets x1, and the arm that the help writes (`x1 if x1 == 0.5`)
		// has no diagnostic and keeps the `x` of its body for the parameter.
		const scale = "fn scale(x: Float, k: Float) -> Float {\n  match k {\n    0.5 => x * 2.0\n    _ => x\n  }\n}\n"
		r = recParse(t, scale)
		r.wantDiags(t, "a parameter called x", e0706With(r.at(t, "0.5", 0), "0.5", armHelp, "x1"))
		if got := recParse(t, strings.Replace(scale, "0.5 =>", "x1 if x1 == 0.5 =>", 1)).sink.All(); len(got) != 0 {
			t.Errorf("the arm that the help writes: %v, want no diagnostic", got)
		}
		// A name that the interner has because of another file (the prelude, say) and that
		// no token of this one spells is no reason to avoid it: the help says `x`.
		tb, sink, id, in := lexFile(t, "fn f(v: Float) -> Int { match v { 2.5 => 1 } }")
		in.InternString("x")
		r = &recRun{m: syntax.Parse(&tb, id, in, sink), in: in, sink: sink, id: id, src: "fn f(v: Float) -> Int { match v { 2.5 => 1 } }"}
		r.wantDiags(t, "a name of another file", e0706With(r.at(t, "2.5", 0), "2.5", armHelp, "x"))
		// An arm that has a guard says to join the comparison to it, and a chain of
		// alternatives that the guard has to compare each of them; an or-pattern inside
		// a bracket is read as any other float, with no such clause.
		for _, row := range []struct{ name, src, literal, help string }{
			{"a guard", "fn f() { match v { 2.5 if c => 1 } }", "2.5", "bind it and add the comparison to the guard: `x if x == 2.5 && …`"},
			{"a guard over a negative", "fn f() { match v { -2.5 if x > 0 => 1 } }", "-2.5", "bind it and add the comparison to the guard: `x1 if x1 == -2.5 && …`"},
			{"a chain of alternatives", "fn f() { match v { 1 | 2.5 => 1 } }", "2.5", "bind it and compare each alternative in a guard: `x if x == 2.5 || x == …`"},
			{"a chain of floats", "fn f() { match v { 1.5 | 2.5 => 1 } }", "1.5", "bind it and compare each alternative in a guard: `x if x == 1.5 || x == …`"},
			{"a chain and a guard", "fn f() { match v { 1 | 2.5 if c => 1 } }", "2.5", "bind it and add the comparison to the guard: `x if x == 2.5 && …`"},
			{"a chain on a later line", "fn f() {\n  let a = 1\n  match v {\n    1 | 2.5 => 1\n  }\n}", "2.5", "bind it and compare each alternative in a guard: `x if x == 2.5 || x == …`"},
			{"alternatives in a list", "fn f() { match v { [1 | 2.5, ..t] => 1 } }", "2.5", "bind it and compare in a guard: `x if x == 2.5`"},
			{"alternatives in a record", "fn f() { match v { P { x: 1 | 2.5 } => 1 } }", "2.5", "bind it and compare in a guard: `x1 if x1 == 2.5`"},
			{"a chain with a bracket after the bar", "fn f() { match v { 1 | (2, 2.5) => 1 } }", "2.5", "bind it and compare in a guard: `x if x == 2.5`"},
		} {
			r := recParse(t, row.src)
			if got := r.sink.All(); len(got) != 1 || !reflect.DeepEqual(got[0], e0706Help(r.at(t, row.literal, 0), row.help)) {
				t.Errorf("%s: %q gives %+v, want one E0706 with the help %q", row.name, row.src, got, row.help)
			}
		}
		// A mistake fewer than three tokens after the last one is the same mistake seen
		// again, and is not reported: `(1.5, 2.5)` is one E0706, on the first. Farther on
		// is another one.
		r = recParse(t, patSrc("(1.5, 2.5)"))
		r.wantDiags(t, "two floats side by side", e0706(r.at(t, "1.5", 0), "1.5"))
		r = recParse(t, patSrc("P { x: 1.5, y: 2.5 }"))
		r.wantDiags(t, "two floats apart", e0706(r.at(t, "1.5", 0), "1.5"), e0706(r.at(t, "2.5", 0), "2.5"))
		// A pattern of floats takes no pass over the file for each of them: the help
		// of each one that is reported looks at a set of names that is made once.
		var floats *recRun
		within.Run(t, "a tuple pattern of 200 000 floats", func() {
			floats = recParse(t, patSrc("("+strings.Repeat("1.5, ", 200_000-1)+"1.5)"))
		})
		if got := floats.sink.All(); len(got) != diag.MaxErrors {
			t.Errorf("200 000 floats: %d diagnostics, want the %d that the sink keeps", len(got), diag.MaxErrors)
		}
		// The lexer reports a Float that is out of range (E0109), and that is the one
		// error: an E0706 on the same digits would say it twice. The same for an Int,
		// whose help also says how an arm matches the smallest one, because a pattern
		// cannot write the subtraction that an expression does (`-9223372036854775808 =>`).
		const minHelp = "the smallest `Int` is not a literal: write it as `-9223372036854775807 - 1`; in the arm of a `match`, bind the value and compare it to that in a guard"
		for _, row := range []struct{ pat, text, help string }{
			{"1e999", "float literal is out of range", ""},
			{"99999999999999999999", "integer literal is too large", minHelp},
			{"-9223372036854775808", "integer literal is too large", minHelp},
		} {
			r := recParse(t, patSrc(row.pat))
			if got := r.sink.All(); len(got) != 1 || got[0].Message != row.text || (row.help != "" && got[0].Help != row.help) {
				t.Errorf("%q: %v, want only the lexer's %q with help %q", row.pat, got, row.text, row.help)
			}
			r.wantText(t, row.pat, patTree("(error)"))
		}
	})

	t.Run("a_record_literal_in_a_match_head_needs_parentheses", func(t *testing.T) {
		// Like the head of an `if`, the scrutinee of a `match` is followed by a `{`, so a
		// record literal there is read and asked for parentheses, with the word that names
		// this construct; inside parentheses, in the arms and after the match it is a
		// literal as anywhere.
		const src = "type Point { x: Float }\nfn f() { match Point { x: 1.0 } { _ => 1 } }"
		m, id, in, sink := parseFile(t, src)
		lit := base.Span{File: id, Lo: uint32(strings.LastIndex(src, "Point {")), Hi: uint32(strings.Index(src, "} {") + 1)}
		want := diag.Diagnostic{
			Severity: diag.Error, Code: diag.E(201), Message: "a record literal in the head of a `match` needs parentheses",
			Primary: diag.Label{Span: lit, Message: "this literal needs parentheses"},
			Help:    "write the literal between parentheses: after a name, a `{` starts the arms of the `match`",
			Fixits:  []diag.FixIt{diag.Insert(lit, "("), diag.Insert(base.Span{File: id, Lo: lit.Hi, Hi: lit.Hi}, ")")},
		}
		if got := sink.All(); len(got) != 1 || got[0].Message != want.Message || got[0].Help != want.Help || got[0].Primary != want.Primary ||
			len(got[0].Fixits) != 2 || got[0].Fixits[0] != want.Fixits[0] || got[0].Fixits[1] != want.Fixits[1] {
			t.Fatalf("diagnostics %+v, want one E0201 that asks for parentheses on the literal", got)
		}
		if tree := syntax.PrintModule(m, in); !strings.Contains(tree, "(match (record Point (x 1.0)) (arm _ 1))") {
			t.Errorf("printed %s, want the literal in the tree", tree)
		}
		if again := dataDiagsOf(t, applyFixits(src, sink.All()[0])); len(again) != 0 {
			t.Errorf("the edit leaves %+v", again)
		}
		wantModule(t, "in parentheses", "fn f() { match (Point { x: 1.0 }) { _ => 1 } }",
			"(fn f (params) (ret _) (block (match (record Point (x 1.0)) (arm _ 1))))\n")
		wantModule(t, "in an arm", "fn f() { match x { _ => Point { x: 1.0 } } }",
			"(fn f (params) (ret _) (block (match x (arm _ (record Point (x 1.0))))))\n")
		wantModule(t, "after the match", "fn f() { match x { _ => 1 }\n Point { x: 1.0 } }",
			"(fn f (params) (ret _) (block (expr (match x (arm _ 1))) (record Point (x 1.0))))\n")
		wantModule(t, "a name that is not a literal", "fn f() { match Origin { Origin => 1 } }",
			"(fn f (params) (ret _) (block (match Origin (arm Origin 1))))\n")
		// The head of an `if` inside an arm keeps its own word.
		r := recParse(t, "type P { x: Float }\nfn f() { match x { _ => if P { x: 1.0 }.x > 0.0 { 1 } else { 2 } } }")
		if got := r.sink.All(); len(got) != 1 || got[0].Message != "a record literal in the head of an `if` needs parentheses" {
			t.Errorf("an `if` in an arm: %+v, want the E0201 of the `if`", got)
		}
	})

	t.Run("nesting_of_matches_is_bounded_by_e0204", func(t *testing.T) {
		// The scrutinee, the guard and the body of an arm are each a level of nesting,
		// like the condition and the branches of an `if`: matches in matches pass 256
		// levels with one E0204 that skips that arm, and the arms around it are read.
		nest := func(n int) string { return strings.Repeat("match x { _ => ", n) + "1" + strings.Repeat(" }", n) }
		r := recParse(t, "fn f() { "+nest(256)+" }")
		r.wantDiags(t, "256 levels of arm bodies")
		for _, n := range []int{257, 2000} {
			r := recParse(t, "fn f() { match y { A => "+nest(n)+"\n B => 2 } }")
			got := r.sink.All()
			if len(got) != 1 || got[0].Code != diag.E(204) || got[0].Message != "expression is nested too deeply" {
				t.Errorf("%d matches in an arm: diagnostics %v, want one E0204", n, got)
			}
			// The arm whose body is too deep has an error body, and the next arm is read.
			if text := r.text(); strings.Count(text, "(arm _ (error))") != 1 || !strings.HasSuffix(text, "(arm B 2))))\n") {
				t.Errorf("%d matches in an arm: printed %s, want one arm with an error body and then `(arm B 2)`", n, recShow(text))
			}
			// The 256th match is the first that is not read: the level it needs is the
			// 257th. The 255th is the last, and its arm holds the error body that covers
			// the match that was skipped.
			if got := strings.Count(r.text(), "(match x"); got != 255 {
				t.Errorf("%d matches in an arm: %d of them were read, want 255", n, got)
			}
			var broken []syntax.MatchArm
			for _, arm := range r.m.ArmPool {
				if r.m.Exprs.Get(arm.Body).Kind == syntax.ExprError {
					broken = append(broken, arm)
				}
			}
			if len(broken) != 1 || !strings.HasPrefix(patText(r.src, broken[0].Span), "_ => match x { _ => match x {") {
				t.Errorf("%d matches in an arm: arms with an error body %+v, want one that covers the matches that were skipped", n, broken)
			}
		}
		// A level is given back when the match ends: 300 matches one after the other, as
		// statements and as arguments, are no depth.
		r = recParse(t, "fn f() {\n"+strings.Repeat("  match x { _ => 1 }\n", 300)+"  g("+strings.Repeat("match x { _ => 1 }, ", 300)+"1)\n}")
		r.wantDiags(t, "300 matches one after the other")
		// The scrutinee of a match is a level too: a match that is the scrutinee of a
		// match that is the scrutinee of another.
		deep := strings.Repeat("match ", 300) + "x" + strings.Repeat(" { _ => 1 }", 300)
		r = recParse(t, "fn f() { "+deep+" }")
		if got := r.sink.All(); len(got) != 1 || got[0].Code != diag.E(204) {
			t.Errorf("300 matches as scrutinees: %v, want one E0204", got)
		}
		// A guard is a level: a guard that is a match.
		r = recParse(t, "fn f() { match x { _ if "+strings.Repeat("match y { _ if ", 300)+"c"+strings.Repeat(" => true }", 300)+" => 1 } }")
		if got := r.sink.All(); len(got) != 1 || got[0].Code != diag.E(204) {
			t.Errorf("300 matches as guards: %v, want one E0204", got)
		}
	})

	t.Run("one_line_and_trailing_comma", func(t *testing.T) {
		// What the edits of a diagnostic about a constructor pattern need to know of
		// the source: no line break between the name and the closing token (OneLine,
		// for a constructor of either form), and a comma after the last field of a
		// record pattern that has no `..` (TrailingComma). Neither is set on any other
		// pattern, and the `..` clears the comma before it.
		rows := []struct {
			name, pat, ctor      string
			oneLine, trailingCom bool
		}{
			{"a name alone", "None", "None", true, false},
			{"a constructor", "Some(x)", "Some", true, false},
			{"a constructor with a trailing comma", "Some(x,)", "Some", true, false},
			{"a line break after the parenthesis", "Rect(\n a, b)", "Rect", false, false},
			{"a line break before the last argument", "Rect(a,\n b)", "Rect", false, false},
			{"a line break before the closing parenthesis", "Rect(a, b\n)", "Rect", false, false},
			{"a record", "Point { x, y }", "Point", true, false},
			{"a record with no fields", "Point { }", "Point", true, false},
			{"a record with a trailing comma", "Point { x, y, }", "Point", true, true},
			{"a record with a pattern and a trailing comma", "Point { x: Some(a), }", "Point", true, true},
			{"a record with a rest", "Point { x, .. }", "Point", true, false},
			{"a record that is only a rest", "Point { .. }", "Point", true, false},
			{"a record over lines", "Point {\n x\n }", "Point", false, false},
			{"a trailing comma and a line break", "Point { x,\n }", "Point", false, true},
			{"the inner constructor", "Some(Point { x, })", "Point", true, true},
			{"the outer constructor", "Some(Point { x, })", "Some", true, false},
			{"an inner line break reaches the outer", "Some(Point {\n x })", "Some", false, false},
		}
		for _, row := range rows {
			src := patSrc(row.pat)
			m, _, _, _ := parseFile(t, src)
			var found []syntax.Pat
			for _, p := range m.Pats.All() {
				if p.Kind == syntax.PatCtor && patText(src, p.NameSpan) == row.ctor {
					found = append(found, p)
				}
			}
			if len(found) != 1 {
				t.Errorf("%s: %d constructors called %s in %q, want 1", row.name, len(found), row.ctor, src)
				continue
			}
			if got := found[0]; got.OneLine != row.oneLine || got.TrailingComma != row.trailingCom {
				t.Errorf("%s: %s has OneLine %v and TrailingComma %v, want %v and %v", row.name, row.pat, got.OneLine, got.TrailingComma, row.oneLine, row.trailingCom)
			}
		}
		for _, pat := range []string{"x", "_", "0", "(a, b)", "[a, b]", "A | B", "(Some(a),\n b)"} {
			m, _ := armPat(t, pat)
			for _, p := range m.Pats.All() {
				if p.Kind != syntax.PatCtor && (p.OneLine || p.TrailingComma) {
					t.Errorf("%q: the %v pattern has OneLine %v and TrailingComma %v, want neither", pat, p.Kind, p.OneLine, p.TrailingComma)
				}
			}
		}
	})

	t.Run("arm_removal", func(t *testing.T) {
		// What the edit that deletes an arm (the one of W0701) takes out: the arm, and the
		// `;` that follows it with the blanks after the `;`, so that no second separator
		// is left; for an arm that is alone on its line, the line up to the break, so
		// that an empty line is left and not one of blanks. An arm that goes over lines
		// has no edit (RemovalOneLine is false), and an arm that could not be read has
		// no removal at all.
		const lines = "fn f() {\n  match x {\n    a => 1\n    b if b > 0 => 2\n    c => 3\n  }\n}"
		const one = "fn f() { match x { a => 1; b => 2; c => 3 } }"
		const block = "fn f() {\n  match x {\n    a => 1\n    b => {\n      2\n    }\n  }\n}"
		const semi = "fn f() {\n  match x {\n    a => 1;\n    b => 2; c => 3\n  }\n}"
		const broken = "fn f() { match x { a => 1\n => 2\n c => 3 } }"
		rows := []struct {
			name, src string
			arm       int
			want      string
			oneLine   bool
		}{
			{"the first of several lines", lines, 0, "    a => 1", true},
			{"an arm with a guard", lines, 1, "    b if b > 0 => 2", true},
			{"the last of several lines", lines, 2, "    c => 3", true},
			{"the first on a line", one, 0, "a => 1; ", true},
			{"one in the middle", one, 1, "b => 2; ", true},
			{"the last on a line", one, 2, "c => 3", true},
			{"an arm over lines", block, 1, "    b => {\n      2\n    }", false},
			{"a semicolon that ends the line", semi, 0, "    a => 1;", true},
			{"an arm after another on a line", semi, 1, "b => 2; ", true},
			{"the arm after an arm that is broken", broken, 2, "c => 3", true},
			{"an arm that is broken", broken, 1, "", false},
		}
		for _, row := range rows {
			m, id, match := firstMatch(t, row.src)
			arms := m.Arms(match.Arms)
			if row.arm >= len(arms) {
				t.Errorf("%s: %d arms, want at least %d", row.name, len(arms), row.arm+1)
				continue
			}
			arm := arms[row.arm]
			got := ""
			if arm.Removal != (base.Span{}) {
				got = row.src[arm.Removal.Lo:arm.Removal.Hi]
			}
			if got != row.want || arm.RemovalOneLine != row.oneLine {
				t.Errorf("%s: the removal is %q (one line: %v), want %q (%v)", row.name, got, arm.RemovalOneLine, row.want, row.oneLine)
			}
			// The span is in the file of the arm: a fix-it in another is a mistake
			// that RenderHuman answers with a panic.
			if arm.Removal != (base.Span{}) && arm.Removal.File != id {
				t.Errorf("%s: the removal is in file %d, want file %d", row.name, arm.Removal.File, id)
			}
		}
	})
}
