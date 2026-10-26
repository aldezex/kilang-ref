package syntax

import "testing"

func TestLexer(t *testing.T) {
	t.Run("pipe_continues_line", func(t *testing.T) {
		if continues("|> x") {
			t.Fatal("wrong")
		}
	})
}

func FuzzLex(f *testing.F) {
	f.Add("x")
	f.Fuzz(func(t *testing.T, s string) { continues(s) })
}
