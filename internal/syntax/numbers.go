package syntax

import (
	"errors"
	"strconv"

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
// reports whether the run ends in an underscore.
func (lx *lexer) digitRun(text []byte, isDigit func(byte) bool) (_ []byte, trailing bool) {
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		if c != '_' && !isDigit(c) {
			break
		}
		if c != '_' {
			text = append(text, c)
		}
		trailing = c == '_'
		lx.pos++
	}
	return text, trailing
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
	// digitRun's rule, not strconv's.) trailing is set when any run of digits
	// (integer part, fraction or exponent) ends in an underscore.
	var text []byte
	var trailing, t bool
	text, trailing = lx.digitRun(text, isDecDigit)

	isFloat := false
	// A dot belongs to the number only when a digit follows it: `1..2` is a
	// range and `1.abs` a method call on an Int.
	if lx.at(0) == '.' && isDecDigit(lx.at(1)) {
		isFloat = true
		text = append(text, '.')
		lx.pos++
		text, t = lx.digitRun(text, isDecDigit)
		trailing = trailing || t
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
			trailing = trailing || t
		}
	}

	switch {
	case trailing:
		lx.malformed(start, "trailing `_`", isFloat)
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
	lx.pos += 2
	digits, trailing := lx.digitRun(nil, isDigit)
	switch {
	case len(digits) == 0:
		// `0x_` has underscores but no digits, which is the worse problem.
		lx.malformed(start, "missing digits after `"+prefix+"`", false)
	case trailing:
		lx.malformed(start, "trailing `_`", false)
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
			WithNote("the largest `Int` is 9223372036854775807"))
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
			diag.Label{Span: lx.span(start), Message: "does not fit in a 64-bit `Float`"}))
		v = 0
	}
	lx.pushFloat(start, v)
}

// malformed reports E0107 on the literal that goes from start to the current
// position and emits its token (a Float if isFloat, else an Int) with the
// value 0.
func (lx *lexer) malformed(start int, label string, isFloat bool) {
	lx.sink.Report(diag.NewError(diag.E(107), "malformed number literal",
		diag.Label{Span: lx.span(start), Message: label}))
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
