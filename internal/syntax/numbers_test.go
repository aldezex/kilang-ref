package syntax_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// intValues returns the value of every Int token of tb, in order.
func intValues(tb syntax.TokenBuffer) []int64 {
	var vals []int64
	for _, tok := range tb.Tokens {
		if tok.Kind == syntax.Int {
			vals = append(vals, tb.Ints[tok.Payload])
		}
	}
	return vals
}

// floatValues returns the value of every Float token of tb, in order.
func floatValues(tb syntax.TokenBuffer) []float64 {
	var vals []float64
	for _, tok := range tb.Tokens {
		if tok.Kind == syntax.Float {
			vals = append(vals, tb.Floats[tok.Payload])
		}
	}
	return vals
}

// wantNoDiags fails the test if the sink holds any diagnostic.
func wantNoDiags(t *testing.T, what string, sink *diag.Sink) {
	t.Helper()
	if n := len(sink.All()); n != 0 {
		t.Errorf("%s: got %d diagnostics, want none: %+v", what, n, sink.All())
	}
}

// wantKindList fails the test unless the kinds of the tokens of tb are exactly
// want, and reports whether they are, for the callers that read the tokens by
// position afterwards.
func wantKindList(t *testing.T, what string, tb syntax.TokenBuffer, want ...syntax.TokenKind) bool {
	t.Helper()
	got := make([]syntax.TokenKind, len(tb.Tokens))
	for i, tok := range tb.Tokens {
		got[i] = tok.Kind
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s: kinds %v, want %v", what, got, want)
		return false
	}
	return true
}

// wantIntsAndName fails the test unless the Int tokens of tb have exactly the
// values ints and, when name is not "", the token at index at is an identifier
// that interns name. The caller has already checked the kinds of the tokens
// (wantKindList and a count), so that token exists.
func wantIntsAndName(t *testing.T, src string, tb syntax.TokenBuffer, in *base.Interner, ints []int64, at int, name string) {
	t.Helper()
	if got := intValues(tb); !slices.Equal(got, ints) {
		t.Errorf("%s: values %v, want %v", src, got, ints)
	}
	if name == "" {
		return
	}
	if got := in.String(base.Symbol(tb.Tokens[at].Payload)); got != name {
		t.Errorf("%s: identifier %q, want %q", src, got, name)
	}
}

func TestNumbers(t *testing.T) {
	t.Run("int_decimal_hex_binary", func(t *testing.T) {
		src := "42 0xFF 0b1010 1_000"
		tb, sink := lexAll(t, src)
		wantNoDiags(t, src, sink)
		// The span loop below reads the tokens by position. Fatalf, not Errorf,
		// so that a Lex that returns too few tokens ends the test here with this
		// message instead of with an index out of range.
		if len(tb.Tokens) != 5 {
			t.Fatalf("%s: got %d tokens, want 5: %+v", src, len(tb.Tokens), tb.Tokens)
		}
		wantKindList(t, src, tb, syntax.Int, syntax.Int, syntax.Int, syntax.Int, syntax.Eof)
		if got, want := intValues(tb), []int64{42, 255, 10, 1000}; !slices.Equal(got, want) {
			t.Errorf("%s: values %v, want %v", src, got, want)
		}
		// Each token covers its whole literal, prefix and underscores included.
		wantSpans := [][2]uint32{{0, 2}, {3, 7}, {8, 14}, {15, 20}}
		for i, w := range wantSpans {
			if sp := tb.Tokens[i].Span; sp.Lo != w[0] || sp.Hi != w[1] {
				t.Errorf("token %d has span [%d,%d), want [%d,%d)", i, sp.Lo, sp.Hi, w[0], w[1])
			}
		}

		// The hexadecimal digits a to f in both cases, a mix of the two with an
		// underscore in the middle, and an underscore right after the prefix.
		src = "0xabcdef 0xABCDEF 0xDEAD_beef 0x_FF"
		tb, sink = lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.Int, syntax.Int, syntax.Int, syntax.Int, syntax.Eof)
		if got, want := intValues(tb), []int64{11259375, 11259375, 3735928559, 255}; !slices.Equal(got, want) {
			t.Errorf("%s: values %v, want %v", src, got, want)
		}

		// A prefix is lowercase and follows a 0, and a digit that the base does
		// not have ends the number. The lexer reports nothing about these: it
		// reads a number, then whatever comes next as a token of its own, and the
		// parser makes sense of the pair.
		for _, row := range []struct {
			src   string
			kinds []syntax.TokenKind
			ints  []int64 // the value of every Int token, in order
			name  string  // the name in token 1, or "" if token 1 is not one
		}{
			// 0X and 0B are no prefixes: a 0 and a name that starts with the letter.
			{"0X1", []syntax.TokenKind{syntax.Int, syntax.UpperIdent, syntax.Eof}, []int64{0}, "X1"},
			{"0B1", []syntax.TokenKind{syntax.Int, syntax.UpperIdent, syntax.Eof}, []int64{0}, "B1"},
			// Only a 0 starts a prefix: 5x1 is a 5 and the name x1.
			{"5x1", []syntax.TokenKind{syntax.Int, syntax.LowerIdent, syntax.Eof}, []int64{5}, "x1"},
			{"1b1", []syntax.TokenKind{syntax.Int, syntax.LowerIdent, syntax.Eof}, []int64{1}, "b1"},
			// A digit or a letter that the base does not have ends the number.
			{"0b102", []syntax.TokenKind{syntax.Int, syntax.Int, syntax.Eof}, []int64{2, 2}, ""},
			{"12abc", []syntax.TokenKind{syntax.Int, syntax.LowerIdent, syntax.Eof}, []int64{12}, "abc"},
			{"0xFG", []syntax.TokenKind{syntax.Int, syntax.UpperIdent, syntax.Eof}, []int64{15}, "G"},
		} {
			tb, sink, _, in := lexFile(t, row.src)
			wantNoDiags(t, row.src, sink)
			if !wantKindList(t, row.src, tb, row.kinds...) {
				continue // the checks below read the tokens by position
			}
			wantIntsAndName(t, row.src, tb, in, row.ints, 1, row.name)
		}
	})
	t.Run("int_max_ok", func(t *testing.T) {
		src := "9223372036854775807"
		tb, sink := lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.Int, syntax.Eof)
		if got, want := intValues(tb), []int64{9223372036854775807}; !slices.Equal(got, want) {
			t.Errorf("%s: values %v, want %v", src, got, want)
		}
	})
	t.Run("int_hex_max_ok", func(t *testing.T) {
		src := "0x7FFF_FFFF_FFFF_FFFF"
		tb, sink := lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.Int, syntax.Eof)
		if got, want := intValues(tb), []int64{9223372036854775807}; !slices.Equal(got, want) {
			t.Errorf("%s: values %v, want %v", src, got, want)
		}
	})
	t.Run("int_overflow_E0105", func(t *testing.T) {
		src := "9223372036854775808"
		tb, sink, id, _ := lexFile(t, src)
		// The notes are read from the first diagnostic, so a missing one ends
		// the test here (see int_decimal_hex_binary).
		if len(sink.All()) != 1 {
			t.Fatalf("%s: got %d diagnostics, want exactly 1: %+v", src, len(sink.All()), sink.All())
		}
		wantOne(t, src, sink.All(), id, diag.E(105), "integer literal is too large", 0, 19, "does not fit in a 64-bit `Int`")
		if notes := sink.All()[0].Notes; !slices.Equal(notes, []string{"the largest `Int` is 9223372036854775807"}) {
			t.Errorf("%s: notes %q, want the largest Int", src, notes)
		}
		wantKindList(t, src, tb, syntax.Int, syntax.Eof)
		if got := intValues(tb); !slices.Equal(got, []int64{0}) {
			t.Errorf("%s: values %v, want [0]", src, got)
		}
	})
	t.Run("hex_overflow_E0105", func(t *testing.T) {
		src := "0x8000_0000_0000_0000"
		tb, sink, id, _ := lexFile(t, src)
		wantOne(t, src, sink.All(), id, diag.E(105), "integer literal is too large", 0, 21, "does not fit in a 64-bit `Int`")
		wantKindList(t, src, tb, syntax.Int, syntax.Eof)
		if got := intValues(tb); !slices.Equal(got, []int64{0}) {
			t.Errorf("%s: values %v, want [0]", src, got)
		}
	})
	t.Run("trailing_underscore_E0107", func(t *testing.T) {
		rows := []struct {
			src   string
			float bool
		}{
			{"1_", false},
			{"0xFF_", false},
			{"0b1_", false},
			{"1_.5", true},
			{"1.5_", true},
			{"1e5_", true},
		}
		for _, row := range rows {
			tb, sink, id, _ := lexFile(t, row.src)
			wantOne(t, row.src, sink.All(), id, diag.E(107), "malformed number literal", 0, uint32(len(row.src)), "trailing `_`")
			kind := syntax.Int
			if row.float {
				kind = syntax.Float
			}
			wantKindList(t, row.src, tb, kind, syntax.Eof)
			// The token keeps its kind and gets the value 0.
			var wantInts []int64
			var wantFloats []float64
			if row.float {
				wantFloats = []float64{0}
			} else {
				wantInts = []int64{0}
			}
			if got := intValues(tb); !slices.Equal(got, wantInts) {
				t.Errorf("%s: Int values %v, want %v", row.src, got, wantInts)
			}
			if got := floatValues(tb); !slices.Equal(got, wantFloats) {
				t.Errorf("%s: Float values %v, want %v", row.src, got, wantFloats)
			}
		}
	})
	t.Run("hex_without_digits_E0107", func(t *testing.T) {
		rows := []struct {
			src   string
			label string
		}{
			{"0x", "missing digits after `0x`"},
			{"0x_", "missing digits after `0x`"},
			{"0b", "missing digits after `0b`"},
			{"0b_", "missing digits after `0b`"},
		}
		for _, row := range rows {
			tb, sink, id, _ := lexFile(t, row.src)
			wantOne(t, row.src, sink.All(), id, diag.E(107), "malformed number literal", 0, uint32(len(row.src)), row.label)
			wantKindList(t, row.src, tb, syntax.Int, syntax.Eof)
			if got := intValues(tb); !slices.Equal(got, []int64{0}) {
				t.Errorf("%s: values %v, want [0]", row.src, got)
			}
		}

		// A digit that the base does not have is no digit either, so the
		// prefix is alone: the literal is the two characters of the prefix, and
		// the next token is lexed on its own. The last two rows are the
		// character just before a (`) and the one just before A (@). A range of
		// hexadecimal digits that started one character early would take them
		// for digits. They are no token of Ki either, so each also gets an
		// E0104 of its own, after the E0107.
		for _, row := range []struct {
			src   string
			label string
			kinds []syntax.TokenKind
			ints  []int64 // the value of every Int token, in order
			name  string  // the name in token 1, or "" if token 1 is not one
			stray string  // the character in token 1 when it gets an E0104, or ""
		}{
			{"0b2", "missing digits after `0b`", []syntax.TokenKind{syntax.Int, syntax.Int, syntax.Eof}, []int64{0, 2}, "", ""},
			{"0xg", "missing digits after `0x`", []syntax.TokenKind{syntax.Int, syntax.LowerIdent, syntax.Eof}, []int64{0}, "g", ""},
			{"0x`", "missing digits after `0x`", []syntax.TokenKind{syntax.Int, syntax.Error, syntax.Eof}, []int64{0}, "", "`"},
			{"0x@", "missing digits after `0x`", []syntax.TokenKind{syntax.Int, syntax.Error, syntax.Eof}, []int64{0}, "", "@"},
		} {
			tb, sink, id, in := lexFile(t, row.src)
			diags := sink.All()
			if row.stray != "" {
				// The stray character comes after the prefix, so its E0104 is
				// the second diagnostic; the E0107 is the first.
				if len(diags) != 2 {
					t.Errorf("%s: got %d diagnostics, want exactly 2: %+v", row.src, len(diags), diags)
					continue
				}
				wantOne(t, row.src, diags[1:], id, diag.E(104), "unexpected character `"+row.stray+"`", 2, 3, "not valid in Ki source")
				diags = diags[:1]
			}
			wantOne(t, row.src, diags, id, diag.E(107), "malformed number literal", 0, 2, row.label)
			if !wantKindList(t, row.src, tb, row.kinds...) {
				continue // the checks below read the tokens by position
			}
			wantIntsAndName(t, row.src, tb, in, row.ints, 1, row.name)
		}
	})
	t.Run("float_forms", func(t *testing.T) {
		src := "3.14 1e-9 2.5E3 1_000.5"
		tb, sink := lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.Float, syntax.Float, syntax.Float, syntax.Float, syntax.Eof)
		if got, want := floatValues(tb), []float64{3.14, 1e-9, 2500, 1000.5}; !slices.Equal(got, want) {
			t.Errorf("%s: values %v, want %v", src, got, want)
		}
		// The exponent may carry a plus sign and its own underscores, and so
		// may the fraction: every run of digits takes a '_' between digits.
		src = "1e+2 1E1_0 0.5e0 0.1_5"
		tb, sink = lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.Float, syntax.Float, syntax.Float, syntax.Float, syntax.Eof)
		if got, want := floatValues(tb), []float64{100, 1e10, 0.5, 0.15}; !slices.Equal(got, want) {
			t.Errorf("%s: values %v, want %v", src, got, want)
		}
	})
	t.Run("exponent_needs_digits", func(t *testing.T) {
		tb, sink, _, in := lexFile(t, "1e")
		wantNoDiags(t, "1e", sink)
		// The identifier is read by position, so Fatalf (see int_decimal_hex_binary).
		if len(tb.Tokens) != 3 {
			t.Fatalf("1e: got %d tokens, want 3 (Int LowerIdent Eof): %+v", len(tb.Tokens), tb.Tokens)
		}
		wantKindList(t, "1e", tb, syntax.Int, syntax.LowerIdent, syntax.Eof)
		wantIntsAndName(t, "1e", tb, in, []int64{1}, 1, "e")

		tb, sink, _, in = lexFile(t, "1e+")
		wantNoDiags(t, "1e+", sink)
		if len(tb.Tokens) != 4 {
			t.Fatalf("1e+: got %d tokens, want 4 (Int LowerIdent Plus Eof): %+v", len(tb.Tokens), tb.Tokens)
		}
		wantKindList(t, "1e+", tb, syntax.Int, syntax.LowerIdent, syntax.Plus, syntax.Eof)
		wantIntsAndName(t, "1e+", tb, in, []int64{1}, 1, "e")
	})
	t.Run("float_out_of_range_E0109", func(t *testing.T) {
		src := "1e999"
		tb, sink, id, _ := lexFile(t, src)
		wantOne(t, src, sink.All(), id, diag.E(109), "float literal is out of range", 0, 5, "does not fit in a 64-bit `Float`")
		wantKindList(t, src, tb, syntax.Float, syntax.Eof)
		if got := floatValues(tb); !slices.Equal(got, []float64{0}) {
			t.Errorf("%s: values %v, want [0]", src, got)
		}
	})
	t.Run("e0105_help_names_the_minimum", func(t *testing.T) {
		// The most likely way to hit E0105 is to write the smallest Int, whose
		// magnitude is one too many; the help shows the way to spell it. The
		// same text goes with every literal that is too large, in decimal and
		// in hexadecimal. There is no fix-it: the rewritten expression needs
		// the minus sign in front of the literal, which the literal's span
		// does not cover.
		const help = "the smallest `Int` is not a literal: write it as `-9223372036854775807 - 1`"
		for _, src := range []string{"9223372036854775808", "0x8000_0000_0000_0000", "99999999999999999999"} {
			_, sink := lexAll(t, src)
			diags := sink.All()
			if len(diags) != 1 {
				t.Errorf("%s: got %d diagnostics, want exactly 1: %+v", src, len(diags), diags)
				continue
			}
			if d := diags[0]; d.Help != help || len(d.Fixits) != 0 {
				t.Errorf("%s: help %q and fixits %+v, want help %q and no fix-it", src, d.Help, d.Fixits, help)
			}
		}
	})
	t.Run("e0107_trailing_underscore_fixit", func(t *testing.T) {
		// The fix-it deletes the underscores that end a run of digits, all of
		// them (`1__` has two), in every run that ends in one, and nothing else:
		// the underscore is not always the last byte of the literal (`1_.5`,
		// `1_e5`). Applied, the fix-its leave a literal with no mistake.
		for _, row := range []struct {
			src   string
			spans [][2]uint32 // the underscores to delete
		}{
			{"1_", [][2]uint32{{1, 2}}},
			{"1__", [][2]uint32{{1, 3}}},
			{"1_000_", [][2]uint32{{5, 6}}},
			{"0xFF_", [][2]uint32{{4, 5}}},
			{"0b1_", [][2]uint32{{3, 4}}},
			{"1_.5", [][2]uint32{{1, 2}}},
			{"1.5_", [][2]uint32{{3, 4}}},
			{"1e5_", [][2]uint32{{3, 4}}},
			{"1_e5", [][2]uint32{{1, 2}}},
			{"1.5_e3", [][2]uint32{{3, 4}}},
			{"1_.5_", [][2]uint32{{1, 2}, {4, 5}}},
			{"1_.5_e3_", [][2]uint32{{1, 2}, {4, 5}, {7, 8}}},
		} {
			_, sink, id, _ := lexFile(t, row.src)
			diags := sink.All()
			if len(diags) != 1 {
				t.Errorf("%s: got %d diagnostics, want exactly 1: %+v", row.src, len(diags), diags)
				continue
			}
			d := diags[0]
			if want := "remove the `_`"; d.Help != want {
				t.Errorf("%s: help %q, want %q", row.src, d.Help, want)
			}
			var want []diag.FixIt
			for _, sp := range row.spans {
				want = append(want, diag.Delete(base.Span{File: id, Lo: sp[0], Hi: sp[1]}))
			}
			if !slices.Equal(d.Fixits, want) {
				t.Errorf("%s: fixits %+v, want %+v", row.src, d.Fixits, want)
			}
			_, again := lexAll(t, applyFixits(row.src, d))
			wantNoDiags(t, "after the fix-it of "+row.src, again)
		}
	})
	t.Run("e0107_missing_digits_help", func(t *testing.T) {
		// No digits, no edit: the user has to choose them. The help shows the
		// prefix that was written, with digits of its base.
		for _, row := range []struct{ src, help string }{
			{"0x", "write the digits after the prefix: `0xFF`"},
			{"0x_", "write the digits after the prefix: `0xFF`"},
			{"0xg", "write the digits after the prefix: `0xFF`"},
			{"0b", "write the digits after the prefix: `0b1010`"},
			{"0b_", "write the digits after the prefix: `0b1010`"},
			{"0b2", "write the digits after the prefix: `0b1010`"},
		} {
			_, sink := lexAll(t, row.src)
			diags := sink.All()
			if len(diags) < 1 || diags[0].Code != diag.E(107) {
				t.Errorf("%s: diagnostics %+v, want an E0107 first", row.src, diags)
				continue
			}
			if d := diags[0]; d.Help != row.help || len(d.Fixits) != 0 {
				t.Errorf("%s: help %q and fixits %+v, want help %q and no fix-it", row.src, d.Help, d.Fixits, row.help)
			}
		}
	})
	t.Run("e0109_note_and_help", func(t *testing.T) {
		// The help names the exponent when the literal has one and the part in
		// front of it fits: it is the exponent that does not. Otherwise the
		// value is what is too large, with no exponent or with one that is not
		// the cause.
		huge := "1" + strings.Repeat("0", 400)
		for _, row := range []struct{ what, src, help string }{
			{"exponent", "1e999", "use a smaller exponent"},
			{"capital E", "1E999", "use a smaller exponent"},
			{"fraction and exponent", "1.5e999", "use a smaller exponent"},
			{"no exponent", huge + ".0", "use a smaller value"},
			{"huge value, small exponent", huge + ".0e1", "use a smaller value"},
		} {
			_, sink := lexAll(t, row.src)
			diags := sink.All()
			if len(diags) != 1 || diags[0].Code != diag.E(109) {
				t.Errorf("%s: got diagnostics %+v, want exactly one E0109", row.what, diags)
				continue
			}
			d := diags[0]
			if want := []string{"the largest finite `Float` is 1.7976931348623157e308"}; !slices.Equal(d.Notes, want) {
				t.Errorf("%s: notes %q, want %q", row.what, d.Notes, want)
			}
			if d.Help != row.help {
				t.Errorf("%s: help %q, want %q", row.what, d.Help, row.help)
			}
			if len(d.Fixits) != 0 {
				t.Errorf("%s: fixits %+v, want none", row.what, d.Fixits)
			}
		}
	})
	t.Run("float_underflow_is_zero", func(t *testing.T) {
		src := "1e-999"
		tb, sink := lexAll(t, src)
		wantNoDiags(t, src, sink)
		wantKindList(t, src, tb, syntax.Float, syntax.Eof)
		if got := floatValues(tb); !slices.Equal(got, []float64{0}) {
			t.Errorf("%s: values %v, want [0]", src, got)
		}
	})
	t.Run("int_then_range", func(t *testing.T) {
		tb, sink := lexAll(t, "1..2")
		wantNoDiags(t, "1..2", sink)
		wantKindList(t, "1..2", tb, syntax.Int, syntax.DotDot, syntax.Int, syntax.Eof)
		if got := intValues(tb); !slices.Equal(got, []int64{1, 2}) {
			t.Errorf("1..2: values %v, want [1 2]", got)
		}
	})
	t.Run("int_then_method", func(t *testing.T) {
		tb, sink, _, in := lexFile(t, "1.abs")
		wantNoDiags(t, "1.abs", sink)
		if len(tb.Tokens) != 4 {
			t.Fatalf("1.abs: got %d tokens, want 4 (Int Dot LowerIdent Eof): %+v", len(tb.Tokens), tb.Tokens)
		}
		wantKindList(t, "1.abs", tb, syntax.Int, syntax.Dot, syntax.LowerIdent, syntax.Eof)
		wantIntsAndName(t, "1.abs", tb, in, []int64{1}, 2, "abs")
	})
}
