package syntax

import (
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

func TestBracketStack(t *testing.T) {
	t.Run("unmatched_closers_do_not_rescan", func(t *testing.T) {
		// The line rule must not cost more than one step per bracket. A closing
		// bracket that has no partner must not look through the whole stack for
		// one: a file of n openers followed by n closers of another kind would
		// take n*n steps. The inputs below are far too big for that: it would
		// run for hours, and the test timeout is what ends the test. The linear
		// version takes milliseconds.
		const depth = 5_000_000

		// leave closes the brackets of an interpolation, and the counters have
		// to go down with them, as they do in close: the bracket that was open
		// before the interpolation stays, and so does its count. A leave that
		// only cut the stack would leave the counts of ( and { at 1 and the one
		// of [ at 2, and the next closer would look for a partner in a stack
		// that does not have it.
		var s0 bracketStack
		s0.push(LBracket)
		s0.enter()
		s0.push(LParen)
		s0.push(LBracket)
		s0.push(LBrace)
		if len(s0.kinds) != 4 || len(s0.levels) != 4 || s0.parens != 1 || s0.brackets != 2 || s0.braces != 1 {
			t.Errorf("after four pushes: %d kinds and %d levels, counts (%d, %d, %d), want 4 and 4 with (1, 2, 1)",
				len(s0.kinds), len(s0.levels), s0.parens, s0.brackets, s0.braces)
		}
		s0.leave()
		if len(s0.kinds) != 1 || len(s0.levels) != 1 || s0.kinds[0] != LBracket || s0.parens != 0 || s0.brackets != 1 || s0.braces != 0 {
			t.Errorf("after leave: kinds %v and %d levels, counts (%d, %d, %d), want [LBracket] and 1 with (0, 1, 0)",
				s0.kinds, len(s0.levels), s0.parens, s0.brackets, s0.braces)
		}

		// Many open braces and as many closers with no [ to close.
		var s bracketStack
		for range depth {
			s.push(LBrace)
		}
		for range depth {
			s.close(LBracket)
		}
		if len(s.kinds) != depth || s.braces != depth || s.brackets != 0 || s.parens != 0 {
			t.Errorf("after %d closers with no partner: %d open, counts (%d, %d, %d), want %d open and (0, 0, %d)",
				depth, len(s.kinds), s.parens, s.brackets, s.braces, depth, depth)
		}

		// A closer whose partner is at the bottom closes everything above it, and
		// the counters go down for each kind it passes, not only for its own:
		// otherwise the braces would still count as open, and the next closing
		// brace would look for one that is gone.
		var t2 bracketStack
		t2.push(LBracket)
		for range depth {
			t2.push(LBrace)
		}
		t2.close(LBracket)
		if len(t2.kinds) != 0 || t2.braces != 0 || t2.brackets != 0 {
			t.Errorf("after closing the bracket at the bottom: %d open, counts (%d, %d, %d), want none open",
				len(t2.kinds), t2.parens, t2.brackets, t2.braces)
		}
		t2.close(LBrace)
		t2.close(LParen)
		if len(t2.kinds) != 0 {
			t.Errorf("closers with no partner on an empty stack left %d brackets open", len(t2.kinds))
		}

		// The same through the lexer, with a file of about a million bytes: the
		// line rule does not add a Newline anywhere, and every byte is a token.
		src := strings.Repeat("{", 500_000) + strings.Repeat("]", 500_000)
		var sm base.SourceManager
		id := sm.Add("big.ki", []byte(src))
		tb := Lex(&sm, id, base.NewInterner(), &diag.Sink{})
		if want := len(src) + 1; len(tb.Tokens) != want {
			t.Errorf("a file of %d bytes gave %d tokens, want %d", len(src), len(tb.Tokens), want)
		}
		for _, tok := range tb.Tokens {
			if tok.Kind == Newline {
				t.Fatalf("a Newline at %d in a file with no line break", tok.Span.Lo)
			}
		}
	})
}
