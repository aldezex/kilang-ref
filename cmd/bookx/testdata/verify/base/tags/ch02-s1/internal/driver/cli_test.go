package driver

import "testing"

func TestCLI(t *testing.T) {
	t.Run("version_prints_version", func(t *testing.T) {
		if Main() != 0 {
			t.Fatal("bad exit code")
		}
	})
	t.Run("usage_error", func(t *testing.T) {
		if usage() != 2 {
			t.Fatal("bad exit code")
		}
	})
}

func BenchmarkCLI(b *testing.B) {
	for b.Loop() {
		Main()
	}
}
