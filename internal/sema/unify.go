package sema

import (
	"strings"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// Unifier makes types equal by giving values to inference variables, and
// reports to Sink the types that cannot be made equal.
type Unifier struct {
	// Types is the table that holds the types and the variables.
	Types *TypeTable
	// Sink receives the errors.
	Sink *diag.Sink
}

// Unify makes the types expected and found the same type, binding the
// inference variables that need it. It returns whether it could.
//
// When it cannot, it reports one error to the sink and returns false. The
// error is E0401 (mismatched types) with its primary label on foundAt, where
// the found type comes from, and a secondary label, or a note, that r chooses:
// it says why the other type is the expected one. The secondary label is left
// out when r.Because is the zero Span or lies in another file than foundAt. If
// making them equal would give a variable a type that contains the variable,
// the error is E0405 instead (an infinite type), which is the same whatever r
// is.
//
// Function types are unified part by part, the parameters from the first and
// then the result, and the first part that does not match is the mistake: it
// is reported once, with the two types in the label, and the rest is not looked
// at. The variables that were bound before that stay bound, and the
// label shows them as what they are bound to. Function types with different
// numbers of parameters do not match. The effect rows of two function types
// are not compared: in Part I every function has the empty row (EmptyRow).
//
// The error type unifies with anything, without a word and without binding a
// variable, because it stands for a mistake that was already reported.
//
// The labels and the note of an error write the types the way Show does, so a
// type whose text is longer than MaxTypeText bytes appears cut, as its start
// followed by `…`.
//
// An E0401 whose reason is ReasonBlockTail and whose expected type is `()`
// also carries a help line, which says that the value at the tail is unused
// and can be bound with a `let`, and, when r.CanReturn is set, that it can be
// returned from the function. No other reason gets a help, and none gets an
// edit.
//
// Unify keeps its own stack of the pairs of types still to compare instead of
// calling itself, because how deep a type goes is up to the program that is
// being compiled. And it takes a pair of function types apart once, however
// many places hold that pair: the table builds equal types once, so a type can
// hold the same type twice at every level, and two such types of n levels have
// n nodes each and 2^n paths. A comparison that went down every path would not
// end for a program of a few lines. Binding a variable has a cost of its own,
// the occurs check; see TypeTable.Occurs for what it adds over a whole program.
func (u *Unifier) Unify(expected, found TypeID, foundAt base.Span, r Reason) bool {
	t := u.Types
	// pair is two types that have to become the same.
	type pair struct{ expected, found TypeID }
	todo := []pair{{expected, found}}
	// expanded holds the pairs of function types whose parts are already on the
	// stack or done. Only function types have parts, so only they can be met by
	// many paths; a builtin or a variable is answered on the spot.
	var expanded map[pair]struct{}
	for len(todo) > 0 {
		p := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		// Resolve first: a variable that stands for Int is Int, and comparing
		// ids before this would tell it apart from the Int it stands for. The
		// equal-ids check also keeps a variable from being bound to itself,
		// which the occurs check would take for an infinite type.
		e, f := t.Resolve(p.expected), t.Resolve(p.found)
		if e == f {
			continue
		}
		en, fn := t.Node(e), t.Node(f)
		switch {
		case en.Kind == TypeErr || fn.Kind == TypeErr:
			// Nothing to say, and nothing to bind.
		case en.Kind == TypeVariable:
			if !u.bind(en.Var, e, f, foundAt) {
				return false
			}
		case fn.Kind == TypeVariable:
			if !u.bind(fn.Var, f, e, foundAt) {
				return false
			}
		case en.Kind == TypeFun && fn.Kind == TypeFun:
			if len(en.Params) != len(fn.Params) {
				u.mismatch(expected, found, foundAt, r)
				return false
			}
			// The pair is resolved, so the same two types meet here whatever
			// path led to them. The parts of the first visit are on the stack
			// or finished, and a failure among them has already ended Unify.
			key := pair{e, f}
			if _, done := expanded[key]; done {
				continue
			}
			if expanded == nil {
				expanded = make(map[pair]struct{})
			}
			expanded[key] = struct{}{}
			// The stack gives back the last pair pushed first, so the result
			// goes in first and the parameters from the last to the first.
			todo = append(todo, pair{en.Ret, fn.Ret})
			for i := len(en.Params) - 1; i >= 0; i-- {
				todo = append(todo, pair{en.Params[i], fn.Params[i]})
			}
		default:
			// Two different builtin types, or a builtin and a function.
			u.mismatch(expected, found, foundAt, r)
			return false
		}
	}
	return true
}

// bind makes the unbound variable v, which is the type vType, stand for the
// type to, unless to contains v. It returns false after reporting E0405 in
// that case, and then v stays unbound.
func (u *Unifier) bind(v TypeVarID, vType, to TypeID, foundAt base.Span) bool {
	if u.Types.Occurs(v, to) {
		shown := u.Types.showAll(vType, to)
		u.Sink.Report(diag.NewError(diag.E(405), "cannot construct an infinite type",
			diag.Label{Span: foundAt, Message: "this type would contain itself"}).
			WithNote("`" + shown[0] + "` would have to be `" + shown[1] + "`, which has no finite size"))
		return false
	}
	u.Types.Bind(v, to)
	return true
}

// mismatch reports the E0401 for the types expected and found: the two types
// that Unify was given, and not the parts of them that differ. Both are written
// with the same names for their variables, so that two different variables are
// never both `a`, and each is cut if it is long (see TypeTable.Show).
func (u *Unifier) mismatch(expected, found TypeID, foundAt base.Span, r Reason) {
	t := u.Types
	shown := t.showAll(expected, found)
	d := diag.NewError(diag.E(401), "mismatched types",
		diag.Label{Span: foundAt, Message: "expected `" + shown[0] + "`, found `" + shown[1] + "`"})
	because := func(text string) {
		// A label in another file cannot be drawn under the primary one
		// (RenderHuman refuses it), and the zero Span is no place at all: the
		// parameter of a function of the prelude is the first, a variable that
		// holds a function the second. Without a place there is no label.
		if r.Because == (base.Span{}) || r.Because.File != foundAt.File {
			return
		}
		d = d.WithSecondary(diag.Label{Span: r.Because, Message: text})
	}
	switch r.Kind {
	case ReasonAnnotation, ReasonLetAnnotation:
		because("expected because of this annotation")
	case ReasonReturnType, ReasonBlockTail:
		because("expected because of the return type")
	case ReasonArgument:
		because("the parameter is declared here")
	case ReasonIfBranches:
		because("the other branch has type `" + shown[0] + "`")
	case ReasonIfCondition:
		d = d.WithNote("an `if` condition must be `Bool`")
	case ReasonOperand:
		switch r.Index {
		case 1:
			because("`" + r.Detail + "` expects " + indefinite(shown[0]) + " `" + shown[0] + "` operand")
		case 2:
			because("`" + r.Detail + "` expects `" + shown[0] + "` operands")
		default:
			panic("Unify: an operand reason needs Index 1 or 2")
		}
	default:
		panic("unreachable")
	}
	// A body whose result is `()` and whose tail has a value: the value is
	// not wanted, which the reader may not see when the tail is the last line
	// of a function that has no `->`. The found type is not `()` (the types
	// would be the same) and not the error type (it unifies silently), so only
	// the expected type needs looking at. This help takes no edit, because
	// what to call the variable, and whether to bind the value or to return it,
	// is for the author to say. Binding works wherever a statement does;
	// returning is offered only where the checker says it would (see
	// Reason.CanReturn): it is E0404 in `main`, and no use at the end of a
	// branch that has no `else`.
	if r.Kind == ReasonBlockTail && t.Resolve(expected) == t.Builtin(BuiltinUnit) {
		help := "this value is unused: bind it with `let name = …`"
		if r.CanReturn {
			help += ", or return it from the function"
		}
		d = d.WithHelp(help)
	}
	u.Sink.Report(d)
}

// indefinite returns the article that goes before the name of a type in a
// sentence: "an" before a name that starts with a vowel (`Int`) and "a" before
// any other (`Bool`, `()`, a function type). It looks at the first letter only.
func indefinite(typeText string) string {
	if typeText != "" && strings.ContainsRune("AEIOU", rune(typeText[0])) {
		return "an"
	}
	return "a"
}
