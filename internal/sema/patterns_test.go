package sema_test

import (
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/sema"
	"github.com/aldezex/kilang/internal/testsupport"
)

// The tests here are about the pass "patterns": which `match` leaves values out
// (E0701), which arm no value reaches (W0701), which `let` can fail (E0702) and which
// `match` is too complex to decide (E0707). They reuse the helpers of the other tests
// of the package: checkRows and diagRow, wantCodes, wantDiags, spanIn, listOf, shapes
// and noSpan.

// e0701 is the E0701 whose primary label is at the scrutinee at, with the label and
// the help written out.
func e0701(at base.Span, label, help string) diag.Diagnostic {
	return diag.NewError(diag.E(701), "non-exhaustive match", diag.Label{Span: at, Message: label}).
		WithNote("a `match` must cover every value; guards do not count").WithHelp(help)
}

// w0701 is the warning for the arm whose pattern is at at, with the edit that deletes
// the span removal (noSpan: no edit, only the help).
func w0701(at, removal base.Span) diag.Diagnostic {
	d := diag.Diagnostic{
		Severity: diag.Warning, Code: diag.W(701), Message: "unreachable pattern",
		Primary: diag.Label{Span: at, Message: "covered by the arms above"}, Help: "remove this arm",
	}
	if removal != noSpan {
		d = d.WithFixIt(diag.Delete(removal))
	}
	return d
}

// e0702 is the E0702 for the pattern at at, with the label written out.
func e0702(at base.Span, label string) diag.Diagnostic {
	return diag.NewError(diag.E(702), "refutable pattern in `let`", diag.Label{Span: at, Message: label}).
		WithNote("a `let` binds every value, so its pattern must match all of them").
		WithHelp("use `match` to handle the other cases")
}

// e0707 is the E0707 for the scrutinee or pattern at at; what is "match" or "let",
// and label is the whole text of the primary label. The note and the help are the
// ones of the construct: a `let` has no arms to end with a `_`, and no `match`.
func e0707(at base.Span, what, label string) diag.Diagnostic {
	note := "finding a value that no arm matches is NP-complete: a `match` on many booleans can take forever"
	help := "split it into nested `match`es, or end it with a `_` arm"
	if what == "let" {
		note = "finding a value that the pattern does not match is NP-complete: a pattern over many booleans can take forever"
		help = "write `_` where every value matches, or take the value apart with `match`es of a few values each"
	}
	return diag.NewError(diag.E(707), "this `"+what+"` is too complex to check", diag.Label{Span: at, Message: label}).
		WithNote(note).
		WithHelp(help)
}

// matchOn is a program that declares types, then has a function whose parameter is
// param (`s: Shape`) and whose body is a `match` of it with the arms. Every arm is
// written with its body, `Circle(_) => 1`, and returns an Int.
func matchOn(types, param string, arms ...string) string {
	name := param[:strings.Index(param, ":")]
	return types + "fn f(" + param + ") -> Int {\n  match " + name + " {\n    " + strings.Join(arms, "\n    ") + "\n  }\n}\n"
}

// scrutinee is the span of what the first `match` of the program src takes apart: the
// text between `match ` and the ` {` that ends the line.
func scrutinee(t *testing.T, p *testsupport.Program, src string) base.Span {
	t.Helper()
	i := strings.Index(src, "match ") + len("match ")
	j := i + strings.Index(src[i:], " {\n")
	return base.Span{File: userFile(p), Lo: uint32(i), Hi: uint32(j)}
}

// coverRow is a program of one `match` and what the pass says about it: label and
// help of the E0701, or neither if the match covers every value (and then the program
// has no diagnostic at all).
type coverRow struct {
	name, src, label, help string
}

// checkCover runs each row of rows.
func checkCover(t *testing.T, rows []coverRow) {
	t.Helper()
	for _, row := range rows {
		p := testsupport.Analyze(t, row.src)
		var want []diag.Diagnostic
		if row.label != "" {
			want = append(want, e0701(scrutinee(t, p, row.src), row.label, row.help))
		}
		wantDiags(t, row.name, p, want...)
	}
}

// orWild ends the help of an E0701 whose witness has an `Int` literal in it: no list
// of arms for numbers is complete, and the help says what is.
const orWild = ", or end the `match` with a `_` arm"

// types for the tables below. Nat is a number written in unary, Six has six
// constructors and none of them has fields, Wrap has one constructor.
const (
	nat  = "type Nat { Zero, Succ(Nat) }\n"
	six  = "type Six { A, B, C, D, E, F }\n"
	wrap = "type Wrap { Wrap(Int) }\n"
	// Opt has a constructor with a field and one with none.
	optBool = "type Opt { No, Yes(Bool) }\n"
)

func TestPatterns(t *testing.T) {
	t.Run("coverage_by_each_kind_of_type", func(t *testing.T) {
		// A `match` covers a type when its arms name every constructor, each one with
		// sub-patterns that cover its fields in turn: `true` and `false`, the
		// constructors of a sum (with fields and without), a constructor inside
		// another, the one constructor of a tuple and of a record, and an or-pattern,
		// which covers what any of its alternatives does. The witness of what is left
		// out is built by the algorithm: `Rect(_, _)`, `Succ(Succ(_))`, `(true, false)`.
		// It has a wildcard for every part that the missing value does not depend on.
		checkCover(t, []coverRow{
			{name: "bool both", src: matchOn("", "b: Bool", "true => 1", "false => 2")},
			{name: "bool true only", src: matchOn("", "b: Bool", "true => 1"), label: "pattern `false` not covered", help: "add an arm for `false`"},
			{name: "bool false only", src: matchOn("", "b: Bool", "false => 1"), label: "pattern `true` not covered", help: "add an arm for `true`"},
			{name: "all constructors", src: matchOn(shapes, "s: Shape", "Circle(r) => 1", "Rect(w, h) => 2", "Unit => 3")},
			{name: "a variable ends it", src: matchOn(shapes, "s: Shape", "Circle(r) => 1", "other => 2")},
			{name: "one missing", src: matchOn(shapes, "s: Shape", "Circle(r) => 1", "Unit => 3"),
				label: "pattern `Rect(_, _)` not covered", help: "add an arm for `Rect(_, _)`"},
			{name: "two missing", src: matchOn(shapes, "s: Shape", "Rect(_, _) => 1"),
				label: "patterns `Circle(_)` and `Unit` not covered", help: "add arms for `Circle(_)` and `Unit`"},
			{name: "no arms", src: matchOn(shapes, "s: Shape"), label: "pattern `_` not covered", help: "add an arm for `_`"},
			{name: "inside a constructor", src: matchOn(nat, "n: Nat", "Zero => 1", "Succ(Zero) => 2"),
				label: "pattern `Succ(Succ(_))` not covered", help: "add an arm for `Succ(Succ(_))`"},
			{name: "inside two levels", src: matchOn(nat, "n: Nat", "Zero => 1", "Succ(Zero) => 2", "Succ(Succ(_)) => 3")},
			{name: "a tuple", src: matchOn("", "t: (Bool, Bool)", "(true, true) => 1", "(false, _) => 2"),
				label: "pattern `(true, false)` not covered", help: "add an arm for `(true, false)`"},
			{name: "a tuple covered", src: matchOn("", "t: (Bool, Bool)", "(true, true) => 1", "(false, _) => 2", "(_, false) => 3")},
			{name: "a record", src: matchOn(shapes, "p: Point", "Point { x: 0, .. } => 1"),
				label: "pattern `Point { x: 1, .. }` not covered", help: "add an arm for `Point { x: 1, .. }`" + orWild},
			{name: "a record with every field", src: matchOn(shapes, "p: Point", "Point { x: 0, y: 0 } => 1"),
				label: "pattern `Point { x: 1, .. }` not covered", help: "add an arm for `Point { x: 1, .. }`" + orWild},
			{name: "a record covered", src: matchOn(shapes, "p: Point", "Point { x: 0, .. } => 1", "Point { y, .. } => 2")},
			{name: "a record of every field covered", src: matchOn(shapes, "p: Point", "Point { x, y } => 1")},
			// A record is written with the fields that the witness has something to
			// say about: all of them, or none, or the ones that it has.
			{name: "a record witness with each field", src: matchOn(shapes, "p: Point", "Point { x: 0, .. } => 1", "Point { y: 0, .. } => 2"),
				label: "pattern `Point { x: 1, y: 1 }` not covered", help: "add an arm for `Point { x: 1, y: 1 }`" + orWild},
			{name: "a record witness with no field", src: matchOn(shapes, "t: (Point, Bool)", "(Point { .. }, true) => 1"),
				label: "pattern `(Point { .. }, false)` not covered", help: "add an arm for `(Point { .. }, false)`"},
			// The parameters have no types written: the patterns give them theirs, and
			// the pass reads them after Check has bound them.
			{name: "parameters typed by the patterns", src: "fn f(a, b) -> Int {\n  match (a, b) {\n    (true, 0) => 1\n  }\n}\n",
				label: "pattern `(false, _)` not covered", help: "add an arm for `(false, _)`"},
			{name: "a constructor of one", src: matchOn(wrap, "w: Wrap", "Wrap(x) => x")},
			// A row that has a wildcard where another names a constructor with fields is
			// specialized by it with a wildcard for each field, and the columns after it
			// stay in line: the first arm covers every `Opt` with `true`, and the other two
			// the rest; without the third, `(No, false)` is left out.
			{name: "a wildcard row over a constructor with fields", src: matchOn(optBool, "s: (Opt, Bool)", "(_, true) => 0", "(Yes(_), false) => 1", "(No, false) => 2")},
			{name: "the same, one arm short", src: matchOn(optBool, "s: (Opt, Bool)", "(_, true) => 0", "(Yes(_), false) => 1"),
				label: "pattern `(No, false)` not covered", help: "add an arm for `(No, false)`"},
			{name: "or-pattern covers", src: matchOn(shapes, "s: Shape", "Circle(_) | Rect(_, _) | Unit => 1")},
			{name: "or-pattern leaves one", src: matchOn(shapes, "s: Shape", "Circle(_) | Unit => 1"),
				label: "pattern `Rect(_, _)` not covered", help: "add an arm for `Rect(_, _)`"},
			{name: "or-pattern inside", src: matchOn(nat, "n: Nat", "Zero => 1", "Succ(Zero | Succ(_)) => 2")},
			{name: "or-pattern inside, one short", src: matchOn(nat, "n: Nat", "Succ(Zero | Succ(_)) => 2"),
				label: "pattern `Zero` not covered", help: "add an arm for `Zero`"},
			{name: "two columns of sums", src: matchOn(shapes, "t: (Shape, Shape)", "(Circle(_), Circle(_)) => 1"),
				label: "patterns `(Rect(_, _), _)` and `(Unit, _)` not covered", help: "add arms for `(Rect(_, _), _)` and `(Unit, _)`"},
			// A leaf of the search gives one witness for each constructor that it lacks:
			// here two leaves, `false` and `true`, each lacking `Rect` and `Unit`, so four
			// witnesses in the order of the constructors (`false` before `true`), and the
			// list ends with `and more`.
			{name: "several witnesses", src: matchOn(shapes, "t: (Bool, Shape)", "(true, Circle(_)) => 1", "(false, Circle(_)) => 2"),
				label: "patterns `(false, Rect(_, _))`, `(false, Unit)`, `(true, Rect(_, _))` and more not covered",
				help:  "add arms for `(false, Rect(_, _))`, `(false, Unit)`, `(true, Rect(_, _))` and more"},
			// A missing constructor that has fields is written with wildcards for
			// them: what comes after it in the witness is the next column's.
			{name: "a column after a missing constructor", src: matchOn(shapes, "t: (Shape, Bool)", "(Circle(_), true) => 1", "(_, false) => 2"),
				label: "patterns `(Rect(_, _), true)` and `(Unit, true)` not covered", help: "add arms for `(Rect(_, _), true)` and `(Unit, true)`"},
		})
	})

	t.Run("guards_do_not_count", func(t *testing.T) {
		// An arm with a guard may fail, so it covers nothing: not for the match, and
		// not for the arms below it, however wide its pattern is. The match whose arms
		// all have guards covers nothing at all and its witness is `_`. The pattern of
		// an arm with a guard is still looked at: it is unreachable if the arms above
		// it, which have none, take every value it matches.
		const g = "n: Int"
		checkCover(t, []coverRow{
			{name: "guard on the last", src: matchOn("", "b: Bool", "true => 1", "false if true => 2"),
				label: "pattern `false` not covered", help: "add an arm for `false`"},
			{name: "every arm guarded", src: matchOn("", g, "m if m > 0 => 1", "m if m <= 0 => 2"),
				label: "pattern `_` not covered", help: "add an arm for `_`"},
			{name: "a variable with a guard", src: matchOn("", g, "m if m > 0 => 1", "m => 2")},
			{name: "a guarded arm does not cover the next", src: matchOn(shapes, "s: Shape", "Circle(_) if true => 1", "Circle(r) => 2", "Rect(_, _) => 3", "Unit => 4")},
		})
		// The arm below an arm with a guard is reached, so it is not a warning; the
		// guarded arm below one that takes everything is.
		const guarded = "fn f(n: Int) -> Int {\n  match n {\n    m if m > 0 => 1\n    m => 2\n  }\n}\n"
		wantDiags(t, "a guard does not hide the arm below", testsupport.Analyze(t, guarded))
		const below = "fn f(n: Int) -> Int {\n  match n {\n    m => 2\n    k if k > 0 => 1\n  }\n}\n"
		p := testsupport.Analyze(t, below)
		wantDiags(t, "a guarded arm under a catch-all", p, w0701(spanIn(t, p, below, "k if", "k"), spanIn(t, p, below, "    k if k > 0 => 1", "    k if k > 0 => 1")))
	})

	t.Run("an_int_needs_a_wildcard", func(t *testing.T) {
		// No set of literals is all of `Int`, so a `match` on one needs `_` or a
		// variable. The witness is a number that no arm reaching that place names, the
		// smallest one from 0 up (a count that is not the number of values left out,
		// which no one could give), and a column of an `Int` that has no literal at all
		// is `_`. In `(Bool, Int)` with `(true, 0)` and `(false, 1)` the arm for `false`
		// does not name the 0 of the other, so its witness is 0 and the one for `true`
		// is 1: a number is not chosen once for the whole column.
		const four = "type Four { A, B, C, D }\n"
		checkCover(t, []coverRow{
			{name: "zero and one", src: matchOn("", "n: Int", "0 => 1", "1 => 2"), label: "pattern `2` not covered", help: "add an arm for `2`" + orWild},
			{name: "one and two", src: matchOn("", "n: Int", "1 => 1", "2 => 2"), label: "pattern `0` not covered", help: "add an arm for `0`" + orWild},
			{name: "a gap", src: matchOn("", "n: Int", "0 => 1", "1 => 2", "3 => 3"), label: "pattern `2` not covered", help: "add an arm for `2`" + orWild},
			{name: "negative and zero", src: matchOn("", "n: Int", "-1 => 1", "0 => 2"), label: "pattern `1` not covered", help: "add an arm for `1`" + orWild},
			{name: "a wildcard", src: matchOn("", "n: Int", "0 => 1", "_ => 2")},
			{name: "a variable", src: matchOn("", "n: Int", "0 => 1", "m => m")},
			{name: "in a tuple", src: matchOn("", "t: (Int, Int)", "(a, 0) => 1"), label: "pattern `(_, 1)` not covered", help: "add an arm for `(_, 1)`" + orWild},
			{name: "in a tuple, both columns", src: matchOn("", "t: (Int, Bool)", "(0, true) => 1", "(1, _) => 2"),
				label: "pattern `(2, _)` not covered", help: "add an arm for `(2, _)`" + orWild},
			{name: "in a constructor", src: matchOn("type Box { Box(Int) }\n", "b: Box", "Box(0) => 1"), label: "pattern `Box(1)` not covered", help: "add an arm for `Box(1)`" + orWild},
			// With two witnesses the help is still one sentence, and ends with the `_`.
			{name: "a number in each of two witnesses", src: matchOn("", "t: (Bool, Int)", "(true, 0) => 1", "(false, 0) => 2"),
				label: "patterns `(false, 1)` and `(true, 1)` not covered", help: "add arms for `(false, 1)` and `(true, 1)`" + orWild},
			{name: "a number for each arm that reaches it", src: matchOn("", "t: (Bool, Int)", "(true, 0) => 1", "(false, 1) => 2"),
				label: "patterns `(false, 0)` and `(true, 1)` not covered", help: "add arms for `(false, 0)` and `(true, 1)`" + orWild},
			// The help ends with the `_` only if a witness that the label shows has a
			// number. Here `false` lacks `B`, `C` and `D`, three witnesses with no number,
			// and the fourth, `(true, _, 1)`, is what `and more` stands for: the help does
			// not mention a `_` for what the reader cannot see. With `B` named too, the
			// number is in the third witness, which is shown, and the help does.
			{name: "a number only in the witness that is not shown", src: matchOn(four, "t: (Bool, Four, Int)", "(false, A, 0) => 1", "(true, _, 0) => 2"),
				label: "patterns `(false, B, _)`, `(false, C, _)`, `(false, D, _)` and more not covered",
				help:  "add arms for `(false, B, _)`, `(false, C, _)`, `(false, D, _)` and more"},
			{name: "a number in the last witness that is shown", src: matchOn(four, "t: (Bool, Four, Int)", "(false, A, 0) => 1", "(false, B, 0) => 2", "(true, _, 0) => 3"),
				label: "patterns `(false, C, _)`, `(false, D, _)` and `(true, _, 1)` not covered",
				help:  "add arms for `(false, C, _)`, `(false, D, _)` and `(true, _, 1)`" + orWild},
		})
	})

	t.Run("witnesses_are_named_up_to_three", func(t *testing.T) {
		// The check finds one witness, and goes on for every constructor that no arm
		// names, up to a limit of three, with `and more` if it found one more than
		// that. It never counts the rest: how many values a `match` leaves out depends
		// on where the algorithm splits them, so a number would change with it. A type
		// of six constructors: with three missing, the three; with four or five, three
		// and `and more`.
		checkCover(t, []coverRow{
			{name: "one", src: matchOn(six, "x: Six", "A => 1", "B => 1", "C => 1", "D => 1", "E => 1"),
				label: "pattern `F` not covered", help: "add an arm for `F`"},
			{name: "two", src: matchOn(six, "x: Six", "A => 1", "B => 1", "C => 1", "D => 1"),
				label: "patterns `E` and `F` not covered", help: "add arms for `E` and `F`"},
			{name: "three", src: matchOn(six, "x: Six", "A => 1", "B => 1", "C => 1"),
				label: "patterns `D`, `E` and `F` not covered", help: "add arms for `D`, `E` and `F`"},
			{name: "four", src: matchOn(six, "x: Six", "A => 1", "B => 1"),
				label: "patterns `C`, `D`, `E` and more not covered", help: "add arms for `C`, `D`, `E` and more"},
			{name: "five", src: matchOn(six, "x: Six", "A => 1"),
				label: "patterns `B`, `C`, `D` and more not covered", help: "add arms for `B`, `C`, `D` and more"},
			{name: "six, with a catch-all that is not", src: matchOn(six, "x: Six", "A if true => 1"),
				label: "pattern `_` not covered", help: "add an arm for `_`"},
		})
	})

	t.Run("unreachable_arms", func(t *testing.T) {
		// An arm that no value reaches is W0701, on its pattern, with the edit that
		// deletes the arm. The arm is repeated, or it follows a `_` or a variable, or a
		// constructor that the arms above have taken whole, or an or-pattern whose
		// alternatives are all taken above. The edit takes the arm with the `;` that
		// follows it, or the whole line of an arm that is alone on one; applied, the
		// program has no diagnostic. An arm that goes over lines has only the help. An
		// alternative of an or-pattern that is taken above is not a warning: only a whole
		// arm is.
		const (
			head = "fn f(n: Int) -> Int {\n  match n {\n"
			tail = "  }\n}\n"
		)
		sh := func(arms string) string {
			return shapes + "fn f(s: Shape) -> Int {\n  match s {\n" + arms + tail
		}
		checkRows(t, []diagRow{
			{name: "repeated", src: sh("    Circle(r) => 1\n    Rect(w, h) => 2\n    Unit => 3\n    Circle(_) => 4\n"),
				fixed: sh("    Circle(r) => 1\n    Rect(w, h) => 2\n    Unit => 3\n\n"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{w0701(at("Circle(_)", "Circle(_)"), at("    Circle(_) => 4", "    Circle(_) => 4"))}
				}},
			{name: "after a wildcard", src: head + "    _ => 1\n    2 => 2\n" + tail,
				fixed: head + "    _ => 1\n\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{w0701(at("2 =>", "2"), at("    2 => 2", "    2 => 2"))}
				}},
			{name: "after a variable", src: head + "    m => m\n    0 => 2\n" + tail,
				fixed: head + "    m => m\n\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{w0701(at("0 =>", "0"), at("    0 => 2", "    0 => 2"))}
				}},
			{name: "a literal twice", src: head + "    0 => 1\n    0 => 2\n    _ => 3\n" + tail,
				fixed: head + "    0 => 1\n\n    _ => 3\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{w0701(at("0 => 2", "0"), at("    0 => 2", "    0 => 2"))}
				}},
			{name: "a constructor taken whole", src: sh("    Rect(w, h) => 1\n    Rect(_, _) => 2\n    _ => 3\n"),
				fixed: sh("    Rect(w, h) => 1\n\n    _ => 3\n"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{w0701(at("Rect(_, _)", "Rect(_, _)"), at("    Rect(_, _) => 2", "    Rect(_, _) => 2"))}
				}},
			{name: "an or-pattern all taken", src: sh("    Circle(_) | Unit => 1\n    Rect(_, _) => 2\n    Unit | Circle(_) => 3\n"),
				fixed: sh("    Circle(_) | Unit => 1\n    Rect(_, _) => 2\n\n"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{w0701(at("Unit | Circle(_)", "Unit | Circle(_)"), at("    Unit | Circle(_) => 3", "    Unit | Circle(_) => 3"))}
				}},
			// Only a whole arm is reported: an alternative of an or-pattern that is
			// taken twice, or taken by an arm above, leaves the arm with values of its own.
			{name: "an alternative taken twice", src: head + "    0 | 1 | 0 => 1\n    _ => 2\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic { return nil }},
			{name: "an alternative taken above", src: head + "    0 => 1\n    0 | 1 => 2\n    _ => 3\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic { return nil }},
			{name: "arms on one line", src: head + "    0 => 1; _ => 2; 3 => 4\n" + tail,
				fixed: head + "    0 => 1; _ => 2; \n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{w0701(at("3 =>", "3"), at("3 => 4", "3 => 4"))}
				}},
			{name: "an arm between two on one line", src: head + "    0 => 1; 0 => 5; _ => 2\n" + tail,
				fixed: head + "    0 => 1; _ => 2\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{w0701(at("0 => 5", "0"), at("0 => 5; ", "0 => 5; "))}
				}},
			{name: "an arm over several lines", src: head + "    _ => 1\n    2 => {\n      3\n    }\n" + tail,
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{w0701(at("2 => {", "2"), noSpan)}
				}},
		})
	})

	t.Run("refutable_lets", func(t *testing.T) {
		// A `let` has no other arm to turn to, so its pattern must match every value
		// of the initializer. E0702 names, on the pattern, what it does not match, as
		// a `match` does: `None`, `(_, 1)` for `(a, 0)`, and the list of those that
		// are missing, three at most. A name, `_`, a tuple or a record of names, the
		// one constructor of a type, and an or-pattern that covers a `Bool` are
		// irrefutable, with an annotation or without.
		const maybe = "type Maybe { None, Some(Int) }\n"
		let := func(types, param, pattern string) string {
			return types + "fn f(" + param + ") -> Int {\n  let " + pattern + " = " + param[:strings.Index(param, ":")] + "\n  1\n}\n"
		}
		checkRows(t, []diagRow{
			{name: "a constructor", src: let(maybe, "m: Maybe", "Some(x)"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{e0702(at("Some(x)", "Some(x)"), "`None` is not covered")}
				}},
			{name: "a literal in a tuple", src: let("", "t: (Int, Int)", "(a, 0)"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{e0702(at("(a, 0)", "(a, 0)"), "`(_, 1)` is not covered")}
				}},
			{name: "two missing", src: let(shapes, "s: Shape", "Circle(r)"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{e0702(at("Circle(r)", "Circle(r)"), "`Rect(_, _)` and `Unit` are not covered")}
				}},
			{name: "three missing", src: let("type Four { P, Q, R, S }\n", "x: Four", "P"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{e0702(at("let P =", "P"), "`Q`, `R` and `S` are not covered")}
				}},
			{name: "more than three", src: let(six, "x: Six", "A"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{e0702(at("let A =", "A"), "`B`, `C`, `D` and more are not covered")}
				}},
			{name: "a bool", src: let("", "t: (Bool, Bool)", "(true, b)"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{e0702(at("(true, b)", "(true, b)"), "`(false, _)` is not covered")}
				}},
			{name: "inside a constructor", src: let("type Pair { Pair(Maybe, Int) }\n"+maybe, "p: Pair", "Pair(Some(a), b)"),
				want: func(at func(string, string) base.Span) []diag.Diagnostic {
					return []diag.Diagnostic{e0702(at("Pair(Some(a), b)", "Pair(Some(a), b)"), "`Pair(None, _)` is not covered")}
				}},
		})
		for _, row := range []struct{ name, src string }{
			{"a tuple of names", let("", "t: (Int, Int)", "(a, b)")},
			{"nested tuples", let("", "t: (Int, (Int, Bool))", "(a, (b, c))")},
			{"a record", let(shapes, "p: Point", "Point { x, y }")},
			{"a record with a rest", let(shapes, "p: Point", "Point { x, .. }")},
			{"a wildcard", let("", "n: Int", "_")},
			{"the one constructor of a type", let(wrap, "w: Wrap", "Wrap(v)")},
			{"an or-pattern that covers a Bool", let("", "t: (Bool, Int)", "(true | false, n)")},
			{"an annotation", "fn f(t: (Int, Int)) -> Int {\n  let (a, b): (Int, Int) = t\n  a + b\n}\n"},
		} {
			wantDiags(t, row.name, testsupport.Analyze(t, row.src))
		}
	})

	t.Run("a_mistake_is_reported_once", func(t *testing.T) {
		// What Check has reported says nothing more here: a `match` whose scrutinee
		// has the error type, or one of whose patterns is a constructor that does not
		// exist, has the wrong number of fields, is of another type, a tuple of
		// another size, a list (there is no list type yet), a record with a field that
		// it does not have or lacks, or could not be read, is left alone. It would be
		// the same mistake in other words, once for each arm. A `let` is left alone as
		// well. A match next to one of them in the same file is still checked.
		list := "[" + listOf(100_000, "_", ", ") + "]"
		one := func(name, src, code string) {
			t.Helper()
			p := testsupport.Analyze(t, src)
			if got := allCodes(p); !slices.Equal(got, []string{code}) {
				t.Errorf("%s: reports %v, want only %s", name, got, code)
			}
		}
		one("an unknown constructor", matchOn(shapes, "s: Shape", "Circl(r) => 1", "Rect(w, h) => 2"), "E0306")
		one("an unknown scrutinee", shapes+"fn f() -> Int {\n  match zzz {\n    Circle(r) => 1\n  }\n}\n", "E0301")
		one("an unknown scrutinee and no arms", "fn f() -> Int {\n  match zzz {\n  }\n}\n", "E0301")
		one("an unknown scrutinee and a guarded arm", "fn f() -> Int {\n  match zzz {\n    x if true => 1\n  }\n}\n", "E0301")
		one("too few sub-patterns", matchOn(shapes, "s: Shape", "Rect(a) => 1"), "E0705")
		one("a pattern of another type", matchOn(shapes, "s: Shape", "0 => 1"), "E0401")
		one("a number where a Bool is matched", matchOn("", "b: Bool", "0 => 1"), "E0401")
		one("a Bool where an Int is matched", matchOn("", "n: Int", "true => 1"), "E0401")
		one("a constructor where an Int is matched", matchOn(shapes, "n: Int", "Circle(r) => 1"), "E0401")
		one("a constructor of another type", matchOn(shapes+"type Other { O }\n", "s: Shape", "O => 1"), "E0401")
		one("a record of another type", matchOn(shapes+"type Cube { x: Int, y: Int, z: Int }\n", "p: Point", "Cube { x: 0, y: 0, .. } => 1"), "E0401")
		one("a tuple of another size", matchOn("", "t: (Int, Bool)", "(a, b, c) => 1"), "E0401")
		one("a list", matchOn("", "n: Int", list+" => 1"), "E0401")
		one("an unknown field", matchOn(shapes, "p: Point", "Point { zed: a, .. } => 1"), "E0409")
		one("a missing field", matchOn(shapes, "p: Point", "Point { x: 0 } => 1"), "E0410")
		one("a field twice", matchOn(shapes, "p: Point", "Point { x: 0, x: 1, y: 2 } => 1"), "E0411")
		both := testsupport.Analyze(t, matchOn(shapes, "p: Point", "Point { x: 0, x: 1 } => 1"))
		if got := allCodes(both); !slices.Equal(got, []string{"E0411", "E0410"}) {
			t.Errorf("a field twice and a field missing: reports %v, want E0411 and E0410 and nothing more", got)
		}
		// A field named by two puns is the one E0704 that Resolve reports and the missing
		// one is E0410; the arm below that takes every record is not unreachable.
		pun := testsupport.Analyze(t, matchOn(shapes, "p: Point", "Point { x, x } => 1", "Point { .. } => 2"))
		if got := allCodes(pun); !slices.Equal(got, []string{"E0704", "E0410"}) {
			t.Errorf("a field twice as a pun and a field missing: reports %v, want E0704 and E0410 and nothing more", got)
		}
		one("an unknown record", matchOn(shapes, "p: Point", "Poin { x: 0, y: 1 } => 1"), "E0406")
		// A match that the parser had trouble with is left alone as well: a `)` that is
		// missing lets the body swallow the arms after it (the E0701 would name arms that
		// are in the file), and an arm that breaks off after a name (`eval (error)`)
		// reads as a variable that covers the arms below it (a W0701 for each, with an
		// edit that deletes them).
		const expr = "type Expr { Num(Int), Add(Expr, Expr), Mul(Expr, Expr), Neg(Expr) }\n"
		one("arms swallowed by a body", expr+"fn eval(e: Expr) -> Int {\n  match e {\n    Num(n) => n\n    Add(a, b) => eval(a) + eval(b\n    Mul(a, b) => eval(a) * eval(b)\n    Neg(a) => 0 - eval(a)\n  }\n}\n", "E0201")
		one("a name that covers the arms below", expr+"fn eval(e: Expr) -> Int {\n  match e {\n    Num(n) => n\n    Add(a, b) => eval(a) eval(b)\n    Mul(a, b) => eval(a) * eval(b)\n    Neg(a) => 0 - eval(a)\n  }\n}\n", "E0201")
		one("a broken arm and a match with none", expr+"fn eval(e: Expr) -> Int {\n  match e {\n    Num(n) => n\n    Add(a, b) =>\n    _ => 0\n  }\n}\n", "E0201")
		// A line among the arms that has a `=>` is an arm that went wrong, whatever it
		// starts with, and not a statement after a match whose `}` is missing: the match
		// is not cut short at it (the E0701 would name arms that are in the file, the
		// error for a `}` would be a false one), and what is reported is the arm.
		one("a lowercase constructor", matchOn(shapes, "s: Shape", "Circle(r) => 1", "rect(w, h) => 2", "Unit => 3"), "E0201")
		one("a lowercase constructor in the first arm", matchOn(shapes, "s: Shape", "circle(r) => 1", "Rect(w, h) => 2", "Unit => 3"), "E0201")
		one("a guard with no `if`", matchOn("", "n: Int", "0 => 0", "n > 0 => 1", "_ => 2"), "E0201")
		one("a let over an unknown value", "fn f() -> Int {\n  let (a, 0) = zzz\n  a\n}\n", "E0301")
		one("a let of another shape", "fn f(n: Int) -> Int {\n  let (a, 0) = n\n  a\n}\n", "E0401")
		for _, src := range []string{
			matchOn(shapes, "s: Shape", "Circle( => 1", "Unit => 2"),
			matchOn(shapes, "s: Shape", "=> 1"),
		} {
			for _, code := range allCodes(testsupport.Analyze(t, src)) {
				if code == "E0701" || code == "W0701" || code == "E0707" {
					t.Errorf("a match with a syntax error reports %s: %s", code, src)
				}
			}
		}
		both = testsupport.Analyze(t, expr+"fn g(b: Bool) -> Int {\n  match b {\n    true => 1\n  }\n}\nfn eval(e: Expr) -> Int {\n  match e {\n    Num(n) => n\n    Add(a, b) => eval(a) eval(b)\n    Mul(a, b) => 0\n  }\n}\n")
		if got := allCodes(both); !slices.Equal(got, []string{"E0201", "E0701"}) {
			t.Errorf("a broken match next to one without a mistake: reports %v, want the E0201 and the E0701 of the other and nothing more", got)
		}
		const twoMatches = "fn f(s: Shape) -> Int {\n  match s {\n    Circl(r) => 1\n  }\n}\nfn g(b: Bool) -> Int {\n  match b {\n    true => 1\n  }\n}\n"
		p := testsupport.Analyze(t, shapes+twoMatches)
		if got := allCodes(p); !slices.Equal(got, []string{"E0306", "E0701"}) {
			t.Errorf("two matches, one with an error: reports %v, want E0306 and E0701", got)
		}
	})

	t.Run("a_thousand_constructors", func(t *testing.T) {
		// A type of a thousand constructors is checked as one of two is, within the
		// budget: all of them is covered, one less is E0701 with the one that is
		// missing, five arms name three and `and more`, and a `_` after all of them is
		// the one arm that no value reaches. No test sees how many steps it takes, so
		// nothing here says that it is a pass and not a search of the arms for each
		// constructor.
		const n = 1000
		names := make([]string, n)
		arms := make([]string, n)
		for i := range names {
			names[i] = "C" + strconv.Itoa(i)
			arms[i] = names[i] + " => " + strconv.Itoa(i)
		}
		big := "type Big { " + strings.Join(names, ", ") + " }\n"
		wantDiags(t, "all", testsupport.Analyze(t, matchOn(big, "x: Big", arms...)))
		checkCover(t, []coverRow{
			{name: "one less", src: matchOn(big, "x: Big", append(slices.Clone(arms[:500]), arms[501:]...)...),
				label: "pattern `C500` not covered", help: "add an arm for `C500`"},
			{name: "five arms", src: matchOn(big, "x: Big", arms[:5]...),
				label: "patterns `C5`, `C6`, `C7` and more not covered", help: "add arms for `C5`, `C6`, `C7` and more"},
		})
		src := matchOn(big, "x: Big", append(slices.Clone(arms), "_ => 0")...)
		p := testsupport.Analyze(t, src)
		wantDiags(t, "a wildcard after all", p, w0701(spanIn(t, p, src, "    _ => 0", "_"), spanIn(t, p, src, "    _ => 0", "    _ => 0")))
	})

	t.Run("large_patterns_are_searched_flat", func(t *testing.T) {
		// The search takes a column at a time with a stack of its own, so a tuple of
		// a hundred thousand elements is as deep as a pair. A row of wildcards and a
		// last element that is the literal 1 leaves out the tuples that end in another
		// number: the witness has the hundred thousand places, with a 0 in the last,
		// and the search went through each of them to find it. A tuple of the same
		// size of literals has the 0 in the first, and a row of wildcards only covers
		// everything. The stack of Go is capped at 1 MiB for the test: the default limit
		// (1 GB) holds a recursion of a hundred thousand levels, so a search that went
		// one column at a time by calling itself would pass; past the cap Go ends the
		// test binary (the tests of this package do not run in parallel).
		defer debug.SetMaxStack(debug.SetMaxStack(1 << 20))
		const n = 100_000
		scrut := "(" + listOf(n, "1", ", ") + ")"
		prog := func(arms ...string) string {
			return "fn f() -> Int {\n  match " + scrut + " {\n    " + strings.Join(arms, "\n    ") + "\n  }\n}\n"
		}
		wilds := func(k int) string { return listOf(k, "_", ", ") }
		src := prog("(" + wilds(n-1) + ", 1) => 1")
		p := testsupport.Analyze(t, src)
		at := base.Span{File: userFile(p), Lo: uint32(strings.Index(src, "match ") + 6), Hi: uint32(strings.Index(src, "match ") + 6 + len(scrut))}
		last := "(" + wilds(n-1) + ", 0)"
		wantDiags(t, "a last element", p, e0701(at, "pattern `"+last+"` not covered", "add an arm for `"+last+"`"+orWild))
		src = prog("(" + listOf(n, "1", ", ") + ") => 1")
		p = testsupport.Analyze(t, src)
		first := "(0" + strings.Repeat(", _", n-1) + ")"
		wantDiags(t, "literals", p, e0701(at, "pattern `"+first+"` not covered", "add an arm for `"+first+"`"+orWild))
		wantDiags(t, "wildcards", testsupport.Analyze(t, prog("("+wilds(n)+") => 1")))
		src = prog("("+wilds(n-1)+", 1) => 1", "("+wilds(n)+") => 2")
		wantDiags(t, "a row of wildcards ends it", testsupport.Analyze(t, src))
	})

	t.Run("large_matches_are_checked_flat", func(t *testing.T) {
		// A match of a hundred thousand arms, and an or-pattern of a hundred thousand
		// alternatives, are checked by looking up the arms that start with the same
		// literal and not by reading every arm for each arm: with no `_` they leave
		// out the first number that no arm names, with one they are covered, and
		// an arm repeated a hundred thousand times is a warning for each copy but the
		// first, and takes no longer than distinct ones. The stack of Go is capped at
		// 1 MiB for the test, as in large_patterns_are_searched_flat: a pass that read
		// the arms or the alternatives by calling itself would end the test binary.
		defer debug.SetMaxStack(debug.SetMaxStack(1 << 20))
		const n = 100_000
		var arms, same strings.Builder
		for i := range n {
			arms.WriteString("    " + strconv.Itoa(i) + " => 1\n")
			same.WriteString("    0 => 1\n")
		}
		prog := func(body string) string { return "fn f(x: Int) -> Int {\n  match x {\n" + body + "  }\n}\n" }
		alternatives := make([]string, n)
		for i := range alternatives {
			alternatives[i] = strconv.Itoa(i)
		}
		for _, row := range []struct{ name, src, witness string }{
			{"arms", prog(arms.String()), "100000"},
			{"alternatives", prog("    " + strings.Join(alternatives, " | ") + " => 1\n"), "100000"},
		} {
			p := testsupport.Analyze(t, row.src)
			at := spanIn(t, p, row.src, "match x {", "x")
			wantDiags(t, row.name, p, e0701(at, "pattern `"+row.witness+"` not covered", "add an arm for `"+row.witness+"`"+orWild))
		}
		wantDiags(t, "arms and a wildcard", testsupport.Analyze(t, prog(arms.String()+"    _ => 0\n")))
		// The arm that no value reaches is found at the end of the hundred thousand: its
		// warning is the last of the search, which has the budget that is left and says
		// nothing when it runs out, so reading every arm above for each arm (five
		// thousand million steps) would end it long before and the warning would be
		// lost. The E0701 for the number that no arm names comes with it.
		last := prog(arms.String() + "    99999 => 2\n")
		p := testsupport.Analyze(t, last)
		wantDiags(t, "a repeated last arm", p,
			e0701(spanIn(t, p, last, "match x {", "x"), "pattern `100000` not covered", "add an arm for `100000`"+orWild),
			w0701(spanIn(t, p, last, "    99999 => 2", "99999"), spanIn(t, p, last, "    99999 => 2", "    99999 => 2")))
		// A literal inside a constructor lands with the others of that constructor, and
		// every arm of it is looked at for each one: the search is quadratic in the arms
		// that start with `Yes`, which the budget bounds (see CheckPatterns), so what is
		// shown here is a size that it reaches, two thousand, with both warnings at the end.
		const k = 2000
		var yes strings.Builder
		yes.WriteString("type Opt { No, Yes(Int) }\nfn f(o: Opt) -> Int {\n  match o {\n    No => 0\n")
		for i := range k {
			yes.WriteString("    Yes(" + strconv.Itoa(i) + ") => 1\n")
		}
		yes.WriteString("    Yes(_) => 2\n    Yes(3) => 3\n    No => 4\n  }\n}\n")
		inside := yes.String()
		p = testsupport.Analyze(t, inside)
		wantDiags(t, "literals inside a constructor", p,
			w0701(spanIn(t, p, inside, "Yes(3) => 3\n", "Yes(3)"), spanIn(t, p, inside, "    Yes(3) => 3\n", "    Yes(3) => 3")),
			w0701(spanIn(t, p, inside, "    No => 4", "No"), spanIn(t, p, inside, "    No => 4", "    No => 4")))
		p = testsupport.Analyze(t, prog(same.String()+"    _ => 0\n"))
		got := p.Session.Sink.All()
		if len(got) != n-1 {
			t.Fatalf("an arm repeated %d times has %d diagnostics, want %d", n, len(got), n-1)
		}
		for i, d := range got {
			if d.Code != diag.W(701) || d.Severity != diag.Warning {
				t.Fatalf("diagnostic %d is %+v, want the warning W0701", i, d)
			}
		}
	})

	t.Run("a_sat_encoding_is_e0707", func(t *testing.T) {
		// Finding a tuple of `Bool` that no arm of a match matches is NP-complete: each
		// arm covers the assignments that make one clause of a formula false, and the
		// arms cover them all exactly when no assignment makes the formula true, so a
		// tuple that they leave out is a solution. This is
		// a formula of 129 clauses over 30 variables, written out. It was chosen
		// because its search goes past the budget of ten million steps: with the ratio
		// of clauses to variables that makes them hardest to decide, 4.3, most formulas
		// of that size are decided inside it (they leave a value out, E0701, or cover
		// them all). The answer is the error E0707 on the scrutinee, one diagnostic, and
		// not a hang and not a guess at coverage.
		clauses := strings.Split(satFormula, ";")
		if len(clauses) != 129 {
			t.Fatalf("the formula has %d clauses, want 129", len(clauses))
		}
		var vars, row []string
		for i := 1; i <= 30; i++ {
			vars = append(vars, "a"+strconv.Itoa(i))
		}
		var src strings.Builder
		scrut := "(" + strings.Join(vars, ", ") + ")"
		src.WriteString("fn f(" + strings.Join(vars, ": Bool, ") + ": Bool) -> Int {\n  match " + scrut + " {\n")
		for _, clause := range clauses {
			row = slices.Repeat([]string{"_"}, 30)
			for _, lit := range strings.Fields(clause) {
				v, err := strconv.Atoi(lit)
				if err != nil {
					t.Fatalf("the literal %q is not a number", lit)
				}
				if v > 0 {
					row[v-1] = "true"
				} else {
					row[-v-1] = "false"
				}
			}
			src.WriteString("    (" + strings.Join(row, ", ") + ") => 1\n")
		}
		src.WriteString("  }\n}\n")
		p := testsupport.Analyze(t, src.String())
		at := spanIn(t, p, src.String(), "match "+scrut+" {", scrut)
		wantDiags(t, "3-SAT", p, e0707(at, "match", "checking it needs more than 10 000 000 steps"))
	})

	t.Run("the_budget_is_in_steps", func(t *testing.T) {
		// The budget is a variable that these tests lower, to reach E0707 with a
		// match that is small. A match of twelve columns of `true | false` is 4 096
		// cases that the search tries one by one. With a budget of 200 steps it is
		// E0707 on the scrutinee, the label saying the budget it ran out of; with the
		// default it covers every value and has nothing to say. Ending it with `_`
		// covers it with no search, so the budget does not matter for that: the arm
		// that no value reaches is looked for with what is left of it, and not
		// reported, and not an error, when it runs out; with the default budget it is
		// found. A `let` with the same pattern is E0707 too, worded for the `let`.
		letters := "abcdefghijkl"
		var params, vars, alts []string
		for _, c := range letters {
			params = append(params, string(c)+": Bool")
			vars = append(vars, string(c))
			alts = append(alts, "true | false")
		}
		scrut, pat := "("+strings.Join(vars, ", ")+")", "("+strings.Join(alts, ", ")+")"
		match := func(extra string) string {
			return "fn f(" + strings.Join(params, ", ") + ") -> Int {\n  match " + scrut + " {\n    " + pat + " => 1\n" + extra + "  }\n}\n"
		}
		let := "fn f(t: (" + strings.Repeat("Bool, ", 11) + "Bool)) -> Int {\n  let " + pat + " = t\n  1\n}\n"
		restore := sema.SetPatternBudget(200)
		defer restore()
		src := match("")
		p := testsupport.Analyze(t, src)
		wantDiags(t, "a match", p, e0707(spanIn(t, p, src, "match "+scrut+" {", scrut), "match", "checking it needs more than 200 steps"))
		p = testsupport.Analyze(t, let)
		wantDiags(t, "a let", p, e0707(spanIn(t, p, let, "let "+pat+" =", pat), "let", "checking its pattern needs more than 200 steps"))
		wantDiags(t, "a wildcard arm", testsupport.Analyze(t, match("    _ => 2\n")))
		restore()
		wantDiags(t, "a match, by default", testsupport.Analyze(t, match("")))
		wantDiags(t, "a let, by default", testsupport.Analyze(t, let))
		src = match("    _ => 2\n")
		p = testsupport.Analyze(t, src)
		wantDiags(t, "a wildcard arm, by default", p, w0701(spanIn(t, p, src, "    _ => 2", "_"), spanIn(t, p, src, "    _ => 2", "    _ => 2")))
		// The budget runs out while the arms are put in the matrix as well as in the
		// search that follows: ten alternatives of numbers cost more than five steps to
		// add. The `match` that is not covered with no search is not accepted for that,
		// and neither is the `let`.
		restore = sema.SetPatternBudget(5)
		defer restore()
		const ten = "0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9"
		src = "fn f(n: Int) -> Int {\n  match n {\n    " + ten + " => 1\n  }\n}\n"
		p = testsupport.Analyze(t, src)
		wantDiags(t, "a match whose arm is too long to add", p, e0707(spanIn(t, p, src, "match n {", "n"), "match", "checking it needs more than 5 steps"))
		src = "fn f(n: Int) -> Int {\n  let " + ten + " = n\n  1\n}\n"
		p = testsupport.Analyze(t, src)
		wantDiags(t, "a let whose pattern is too long to add", p, e0707(spanIn(t, p, src, "let "+ten+" =", ten), "let", "checking its pattern needs more than 5 steps"))
	})
}

// allCodes returns the code of every diagnostic of p, warnings included, in the
// order of the sink.
func allCodes(p *testsupport.Program) []string {
	var codes []string
	for _, d := range p.Session.Sink.All() {
		codes = append(codes, d.Code.String())
	}
	return codes
}

// satFormula is a formula of 129 clauses of three literals over the variables 1 to
// 30, chosen among random ones because its search goes past the budget, and written
// out so that the test does not depend on a random number generator: a clause is its
// three literals, a negative one is the variable negated, and clauses are separated
// by `;`. A positive literal is the pattern `true` in the arm of its clause, and a
// negative one `false`.
const satFormula = "3 4 25;-20 -14 16;2 22 6;22 4 -16;-1 -13 -21;5 -12 -17;-26 -8 -21;29 -12 6;28 15 12;-6 20 8;-1 9 -28;-17 -18 1;18 4 23;25 -8 -29;28 16 8;19 -17 -23;-25 -12 2;22 -8 -15;-10 -22 -8;1 -23 -21;-25 -10 2;-15 -25 1;-4 -22 -12;-3 -20 12;-29 7 28;4 29 -18;-10 1 -20;27 11 -22;20 -8 13;-1 18 6;29 -4 -19;-2 -13 24;3 15 -22;-12 30 -7;-19 15 -11;2 1 23;-18 -5 -17;-18 23 -8;-19 -20 28;30 -12 18;-25 1 15;10 -1 -2;-28 13 24;-11 18 -25;29 -23 30;7 -12 30;2 -15 23;25 30 -20;10 4 25;-2 -10 20;-15 -11 8;17 -20 19;2 1 23;-1 25 -10;-13 -4 -2;-14 7 -25;-1 20 15;-10 -1 19;-16 30 -4;3 13 -27;5 -29 -18;3 12 1;-29 -19 8;16 -8 6;-10 -9 -26;-17 -9 14;-21 2 -14;-5 14 21;19 27 30;-30 -25 28;26 20 -19;25 -9 -27;-27 -10 -5;6 -1 -28;-11 14 -13;-23 10 -4;25 -11 -6;16 -30 -24;-16 -8 30;23 -7 -6;1 7 -22;17 22 11;-22 21 11;20 6 -14;-29 12 11;29 24 10;2 -1 27;12 11 -30;16 -23 12;-16 9 11;22 -17 -18;-11 13 -5;27 10 23;9 29 5;-11 -26 -20;15 17 -21;-17 -26 3;-9 11 29;29 -24 25;2 20 -23;-9 24 -15;12 -17 -2;-27 -11 -26;19 -23 -16;13 4 -29;-14 -24 -13;-23 2 -7;-16 28 4;26 7 27;-30 16 -12;-9 -28 -14;18 -22 -12;21 22 4;-25 1 16;-28 -9 -26;4 -15 -16;21 24 -16;-13 11 24;21 1 7;-22 -2 1;8 19 -11;2 -27 21;-19 20 8;-28 30 8;2 19 -17;-29 -19 17;-27 -3 -23;-19 25 1;-24 18 -7"
