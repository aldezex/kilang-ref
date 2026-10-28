package syntax

import (
	"errors"
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// Number literals (spec 5.2, Literales, which gives only the examples; the rules
// below are this lexer's own):
//
//	Int   = d+ | 0x h+ | 0b b+
//	Float = d+ '.' d+ [exp] | d+ exp
//	exp   = [eE] [+-]? d+
//
// In every run of digits (the integer part, the fraction, the exponent and the
// digits after 0x or 0b) a '_' may stand between digits and after them, but a
// run that ends in '_' is malformed, and so is a 0x or 0b with no digit after
// it. Right after the prefix a '_' may also come first: 0x_FF is 255.
//
// The prefixes are lowercase: 0X1 is the Int 0 followed by the name X1. The
// lexer reads a literal as far as it goes and reports nothing about what comes
// next: a digit that the radix does not have ends the literal, so 0b102 is 0b10
// followed by the Int 2, and 12abc is an Int followed by a name. The parser has
// to make sense of those pairs.
//
// A literal has no sign: `-5` is a Minus token and a 5. Whatever the literal
// gets wrong, the token keeps its kind (Int or Float) with the value 0, so the
// parser sees a number where the programmer wrote one and adds no error of its
// own. Each literal reports at most one diagnostic, and its span is the whole
// literal.

// isDecDigit reports whether c is a decimal digit.
func isDecDigit(c byte) bool { return c >= '0' && c <= '9' }

// isHexDigit reports whether c is a hexadecimal digit.
func isHexDigit(c byte) bool {
	return isDecDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// isBinDigit reports whether c is a binary digit.
func isBinDigit(c byte) bool { return c == '0' || c == '1' }

// digitRun consumes the digits that satisfy isDigit and the underscores
// between and after them, appends the digits (without underscores) to text and
// reports where the underscores that end the run are: the span of all of them,
// which is empty if the run does not end in an underscore.
func (lx *lexer) digitRun(text []byte, isDigit func(byte) bool) (_ []byte, trailing base.Span) {
	underscores := 0 // how many '_' end the run so far
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		if c != '_' && !isDigit(c) {
			break
		}
		if c != '_' {
			text = append(text, c)
			underscores = 0
		} else {
			underscores++
		}
		lx.pos++
	}
	return text, base.Span{File: lx.id, Lo: uint32(lx.pos - underscores), Hi: uint32(lx.pos)}
}

// number lexes the Int or Float literal that starts at the current position,
// which holds a decimal digit.
func (lx *lexer) number() {
	start := lx.pos
	if lx.src[start] == '0' {
		switch lx.at(1) {
		case 'x':
			lx.radixInt(start, 16, isHexDigit)
			return
		case 'b':
			lx.radixInt(start, 2, isBinDigit)
			return
		}
	}

	// text collects what ParseInt or ParseFloat will read: the digits of the
	// literal without their underscores. ParseInt accepts none, and the integer
	// part is read before the lexer knows whether the literal is an Int or a
	// Float, so the underscores are dropped once, here, for both. (ParseFloat
	// would take a single '_' between digits, but where a '_' may stand is
	// digitRun's rule, not strconv's.) trailing collects the underscores that
	// end a run of digits (integer part, fraction or exponent), one span for
	// each run that ends in some.
	var text []byte
	var trailing []base.Span
	var t base.Span
	text, t = lx.digitRun(text, isDecDigit)
	trailing = addTrailing(trailing, t)

	isFloat := false
	// A dot belongs to the number only when a digit follows it: `1..2` is a
	// range and `1.abs` a method call on an Int.
	if lx.at(0) == '.' && isDecDigit(lx.at(1)) {
		isFloat = true
		text = append(text, '.')
		lx.pos++
		text, t = lx.digitRun(text, isDecDigit)
		trailing = addTrailing(trailing, t)
	}
	// An `e` belongs to the number only when digits follow it, with an
	// optional sign in between: `1e` is the Int 1 and the name `e`.
	if c := lx.at(0); c == 'e' || c == 'E' {
		sign := lx.at(1) == '+' || lx.at(1) == '-'
		if isDecDigit(lx.at(1)) || sign && isDecDigit(lx.at(2)) {
			isFloat = true
			text = append(text, 'e')
			lx.pos++
			if sign {
				text = append(text, lx.src[lx.pos])
				lx.pos++
			}
			text, t = lx.digitRun(text, isDecDigit)
			trailing = addTrailing(trailing, t)
		}
	}

	switch {
	case len(trailing) > 0:
		lx.malformed(start, "trailing `_`", trailingHelp, isFloat, deleteAll(trailing)...)
	case isFloat:
		lx.emitFloat(start, string(text))
	default:
		lx.emitInt(start, string(text), 10)
	}
}

// radixInt lexes a hexadecimal or binary Int whose prefix (0x or 0b) starts at
// start.
func (lx *lexer) radixInt(start, radix int, isDigit func(byte) bool) {
	prefix := string(lx.src[start : start+2])
	// The help shows digits of the base that was written.
	example := "FF"
	if radix == 2 {
		example = "1010"
	}
	lx.pos += 2
	digits, trailing := lx.digitRun(nil, isDigit)
	switch {
	case len(digits) == 0:
		// `0x_` has underscores but no digits, which is the worse problem.
		// There is no fix-it: the user has to choose the digits.
		lx.malformed(start, "missing digits after `"+prefix+"`", "write the digits after the prefix: `"+prefix+example+"`", false)
	case trailing.Lo != trailing.Hi:
		lx.malformed(start, "trailing `_`", trailingHelp, false, diag.Delete(trailing))
	default:
		lx.emitInt(start, string(digits), radix)
	}
}

// emitInt emits the Int token for the literal that goes from start to the current
// position, whose digits (no prefix, no underscores) are text in the given
// radix. A value that does not fit in 64 bits is E0105 and the token gets 0.
func (lx *lexer) emitInt(start int, text string, radix int) {
	v, err := strconv.ParseInt(text, radix, 64)
	if err != nil {
		// The digits were checked while lexing, so a range error is the only
		// one ParseInt can return.
		if !errors.Is(err, strconv.ErrRange) {
			panic("unreachable: " + err.Error())
		}
		lx.sink.Report(diag.NewError(diag.E(105), "integer literal is too large",
			diag.Label{Span: lx.span(start), Message: "does not fit in a 64-bit `Int`"}).
			WithNote("the largest `Int` is 9223372036854775807").
			WithHelp("the smallest `Int` is not a literal: write it as `-9223372036854775807 - 1`"))
		v = 0
	}
	lx.pushInt(start, v)
}

// emitFloat emits the Float token for the literal that goes from start to the
// current position, whose text (no underscores) is a valid Go float. A value
// too large for a float64 is E0109 and the token gets 0. A value too small
// is not an error: ParseFloat returns 0 and the literal is 0, as in Go.
func (lx *lexer) emitFloat(start int, text string) {
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		if !errors.Is(err, strconv.ErrRange) {
			panic("unreachable: " + err.Error())
		}
		lx.sink.Report(diag.NewError(diag.E(109), "float literal is out of range",
			diag.Label{Span: lx.span(start), Message: "does not fit in a 64-bit `Float`"}).
			WithNote("the largest finite `Float` is 1.7976931348623157e308").
			WithHelp(outOfRangeHelp(text)))
		v = 0
	}
	lx.pushFloat(start, v)
}

// outOfRangeHelp is the help of an E0109 for the float literal text, as `number`
// builds it: no underscores, and the exponent mark is a lowercase e whichever
// the file has. If the literal has an exponent and the part in front of it
// fits, the exponent is what does not, and the help says so. In any other case,
// with no exponent or with one that is not the cause (a mantissa of 400 digits),
// the value is what is too large.
func outOfRangeHelp(text string) string {
	if i := strings.IndexByte(text, 'e'); i >= 0 {
		if _, err := strconv.ParseFloat(text[:i], 64); err == nil {
			return "use a smaller exponent"
		}
	}
	return "use a smaller value"
}

// trailingHelp is the help of an E0107 whose literal has an underscore where a
// run of digits ends.
const trailingHelp = "remove the `_`"

// addTrailing adds the span of the underscores that end a run of digits to
// list, if there are any (see digitRun).
func addTrailing(list []base.Span, trailing base.Span) []base.Span {
	if trailing.Lo == trailing.Hi {
		return list
	}
	return append(list, trailing)
}

// deleteAll returns a fix-it that deletes each of spans.
func deleteAll(spans []base.Span) []diag.FixIt {
	fixits := make([]diag.FixIt, len(spans))
	for i, sp := range spans {
		fixits[i] = diag.Delete(sp)
	}
	return fixits
}

// malformed reports E0107 on the literal that goes from start to the current
// position, with the given label, help and fix-its (the help says what they
// do), and emits its token (a Float if isFloat, else an Int) with the value 0.
func (lx *lexer) malformed(start int, label, help string, isFloat bool, fixits ...diag.FixIt) {
	d := diag.NewError(diag.E(107), "malformed number literal",
		diag.Label{Span: lx.span(start), Message: label}).WithHelp(help)
	for _, f := range fixits {
		d = d.WithFixIt(f)
	}
	lx.sink.Report(d)
	if isFloat {
		lx.pushFloat(start, 0)
		return
	}
	lx.pushInt(start, 0)
}

// pushInt stores v in the table of Ints and emits the Int token, which holds
// the index of v, for the literal that goes from start to the current position.
func (lx *lexer) pushInt(start int, v int64) {
	lx.tb.Ints = append(lx.tb.Ints, v)
	lx.emit(Int, start, uint32(len(lx.tb.Ints)-1))
}

// pushFloat is pushInt for a Float.
func (lx *lexer) pushFloat(start int, v float64) {
	lx.tb.Floats = append(lx.tb.Floats, v)
	lx.emit(Float, start, uint32(len(lx.tb.Floats)-1))
}

// span returns the span from start to the current position.
func (lx *lexer) span(start int) base.Span {
	return base.Span{File: lx.id, Lo: uint32(start), Hi: uint32(lx.pos)}
}
