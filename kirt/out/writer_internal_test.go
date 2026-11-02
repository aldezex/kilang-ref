package out

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriterModes reads the unexported lineFlush field, which is why it lives in
// this package and not in writer_test.go: what it checks is a decision, and
// nothing a regular file or a character device shows when it is written to.
func TestWriterModes(t *testing.T) {
	t.Run("for_file_flushes_lines_only_for_a_character_device", func(t *testing.T) {
		regular, err := os.Create(filepath.Join(t.TempDir(), "regular"))
		if err != nil {
			t.Fatalf("cannot create a regular file: %v", err)
		}
		defer regular.Close()
		// os.DevNull is a character device and not a terminal: ForFile goes by
		// the mode of the file, as its comment says.
		device, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("cannot open %s: %v", os.DevNull, err)
		}
		defer device.Close()
		// A file that is closed cannot be inspected: Stat fails.
		closed, err := os.Create(filepath.Join(t.TempDir(), "closed"))
		if err != nil {
			t.Fatalf("cannot create a file to close: %v", err)
		}
		if err := closed.Close(); err != nil {
			t.Fatalf("cannot close the file: %v", err)
		}
		rows := []struct {
			name string
			file *os.File
			want bool
		}{
			{"regular_file", regular, false},
			{"character_device", device, true},
			{"file_that_cannot_be_inspected", closed, false},
		}
		for _, row := range rows {
			if got := ForFile(row.file).lineFlush; got != row.want {
				t.Errorf("%s: ForFile(f).lineFlush = %v, want %v", row.name, got, row.want)
			}
		}
	})

	t.Run("stderr_always_flushes_lines", func(t *testing.T) {
		if !Stderr.lineFlush {
			t.Errorf("Stderr.lineFlush = false, want true whatever stderr is attached to")
		}
	})
}
