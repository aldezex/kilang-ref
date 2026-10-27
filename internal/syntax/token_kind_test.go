package syntax_test

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/aldezex/kilang/internal/syntax"
)

func TestTokenKind(t *testing.T) {
	t.Run("generated_file_is_up_to_date", func(t *testing.T) {
		// `go test` runs in the package directory, so this is the command
		// `go generate` runs, except that without -o the generator writes the
		// source to stdout instead of to token_kind.go.
		cmd := exec.Command("go", "run", "./gen")
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("go run ./gen failed: %v\n%s", err, stderr.String())
		}
		have, err := os.ReadFile("token_kind.go")
		if err != nil {
			t.Fatalf("cannot read token_kind.go: %v", err)
		}
		if !bytes.Equal(stdout.Bytes(), have) {
			t.Error("token_kind.go differs from the output of `go run ./gen`: run `go generate ./internal/syntax`")
		}
	})
	t.Run("strings", func(t *testing.T) {
		// The first and the last constant of the file, a keyword and a
		// punctuation token in the middle: an off-by-one in the name table
		// shifts every name after it.
		for _, row := range []struct {
			kind syntax.TokenKind
			want string
		}{
			{syntax.KwFn, "KwFn"},
			{syntax.LParen, "LParen"},
			{syntax.Eof, "Eof"},
			{syntax.Error, "Error"},
			{syntax.TokenKind(255), "TokenKind(255)"}, // not a kind
		} {
			if got := row.kind.String(); got != row.want {
				t.Errorf("%s: String() = %q", row.want, got)
			}
		}
	})
}
