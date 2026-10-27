package kitest

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestWalkErrors(t *testing.T) {
	t.Run("unreadable_directory_report", func(t *testing.T) {
		root := t.TempDir()
		locked := filepath.Join(root, "locked")
		pathError := &fs.PathError{Op: "open", Path: locked, Err: fs.ErrPermission}
		tests := []struct {
			name string
			path string
			err  error
			want string
		}{
			{"permission denied", locked, pathError, "FAIL locked\n- cannot read directory: permission denied\n"},
			{"error that wraps a path error", locked, fmt.Errorf("walking: %w", pathError), "FAIL locked\n- cannot read directory: permission denied\n"},
			{"error without a path", filepath.Join(locked, "deeper"), errors.New("boom"), "FAIL locked/deeper\n- cannot read directory: boom\n"},
		}
		for _, tc := range tests {
			if got := unreadableDirectory(root, tc.path, tc.err); got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
			}
		}
	})
}
