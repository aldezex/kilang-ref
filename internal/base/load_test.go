package base

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// writeFile creates a file with the given bytes in a fresh temporary
	// directory and returns its path.
	writeFile := func(t *testing.T, name string, data []byte) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("cannot write %s: %v", path, err)
		}
		return path
	}

	// skipIfReadsCannotBeDenied skips tests that rely on chmod 0 making a read
	// fail: root ignores file permissions, and Windows ignores chmod 0.
	skipIfReadsCannotBeDenied := func(t *testing.T) {
		t.Helper()
		if os.Geteuid() == 0 || runtime.GOOS == "windows" {
			t.Skip("chmod 0 does not deny reads for root or on Windows, so the test would prove nothing")
		}
	}

	t.Run("reads_file", func(t *testing.T) {
		path := writeFile(t, "real.ki", []byte("fn main() {}"))
		var m SourceManager
		id, err := m.Load(path, "a.ki")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got := string(m.Contents(id)); got != "fn main() {}" {
			t.Errorf("Contents = %q, want %q", got, "fn main() {}")
		}
		if got := m.DisplayPath(id); got != "a.ki" {
			t.Errorf("DisplayPath = %q, want %q", got, "a.ki")
		}
		if m.HadBOM(id) {
			t.Errorf("HadBOM = true for a file without a BOM")
		}
	})

	t.Run("strips_bom", func(t *testing.T) {
		rows := []struct {
			name    string
			in      string
			want    string
			wantBOM bool
		}{
			{"one_bom", "\xEF\xBB\xBFfn", "fn", true},
			// Exactly one mark goes: a second one is part of the text.
			{"two_boms", "\xEF\xBB\xBF\xEF\xBB\xBFx", "\xEF\xBB\xBFx", true},
			// U+FFFD starts with the same byte (EF) as the mark but is not it.
			{"replacement_char", "\xEF\xBF\xBDx", "\xEF\xBF\xBDx", false},
		}
		for _, row := range rows {
			var m SourceManager
			// A file without a BOM first: HadBOM must be stored for, and read
			// from, the file that was loaded, not the first one.
			first, err := m.Load(writeFile(t, "plain.ki", []byte("fn")), "plain.ki")
			if err != nil {
				t.Fatalf("%s: Load: %v", row.name, err)
			}
			id, err := m.Load(writeFile(t, "bom.ki", []byte(row.in)), "bom.ki")
			if err != nil {
				t.Fatalf("%s: Load: %v", row.name, err)
			}
			if got := string(m.Contents(id)); got != row.want {
				t.Errorf("%s: Contents = %q, want %q", row.name, got, row.want)
			}
			if got := m.HadBOM(id); got != row.wantBOM {
				t.Errorf("%s: HadBOM = %v, want %v", row.name, got, row.wantBOM)
			}
			if m.HadBOM(first) {
				t.Errorf("%s: HadBOM of the file without a BOM = true, want false", row.name)
			}
		}
	})

	t.Run("missing_file_error", func(t *testing.T) {
		var m SourceManager
		realPath := filepath.Join(t.TempDir(), "nope.ki")
		_, err := m.Load(realPath, "nope.ki")
		if err == nil {
			t.Fatalf("Load of a missing file succeeded")
		}
		if want := "cannot read file `nope.ki`: "; !strings.HasPrefix(err.Error(), want) {
			t.Errorf("error = %q, want prefix %q", err.Error(), want)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("errors.Is(err, fs.ErrNotExist) = false for %v", err)
		}
		// Diagnostics name the file as the user wrote it, never the real path.
		if strings.Contains(err.Error(), realPath) {
			t.Errorf("error = %q mentions the real path %q", err.Error(), realPath)
		}
	})

	t.Run("read_error_names_display_path", func(t *testing.T) {
		skipIfReadsCannotBeDenied(t)
		// Stat works on a file nobody may read, so this error comes from the
		// read, not from the Stat that missing_file_error covers.
		realPath := writeFile(t, "secret-real.ki", []byte("fn"))
		if err := os.Chmod(realPath, 0); err != nil {
			t.Fatalf("cannot chmod %s: %v", realPath, err)
		}
		var m SourceManager
		_, err := m.Load(realPath, "secret.ki")
		if err == nil {
			t.Fatalf("Load of an unreadable file succeeded")
		}
		if want := "cannot read file `secret.ki`: "; !strings.HasPrefix(err.Error(), want) {
			t.Errorf("error = %q, want prefix %q", err.Error(), want)
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("errors.Is(err, fs.ErrPermission) = false for %v", err)
		}
		if strings.Contains(err.Error(), realPath) {
			t.Errorf("error = %q mentions the real path %q", err.Error(), realPath)
		}
	})

	t.Run("rejects_non_regular_file", func(t *testing.T) {
		// A directory is the one non-regular file every system lets a test
		// make with real files. The case that matters, a device such as
		// /dev/zero, has Stat size 0 and would be read until memory ran out;
		// check_file_rows and load_checks_file_before_reading cover the other
		// kinds with a fake FileInfo.
		dir := t.TempDir()
		var m SourceManager
		_, err := m.Load(dir, "dir.ki")
		if err == nil {
			t.Fatalf("Load of a directory succeeded")
		}
		if want := "cannot read file `dir.ki`: not a regular file"; err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
		if !errors.Is(err, errNotRegular) {
			t.Errorf("errors.Is(err, errNotRegular) = false for %v", err)
		}
	})

	t.Run("check_size_boundary", func(t *testing.T) {
		// checkSize runs on every system and as root, unlike the sparse-file
		// tests below, which need chmod 0. That Load applies it before
		// reading is checked by load_checks_file_before_reading.
		rows := []struct {
			name string
			size int64
			want error
		}{
			{"empty", 0, nil},
			{"one_below_limit", MaxFileSize - 1, nil},
			{"at_limit", MaxFileSize, nil},
			{"one_past_limit", MaxFileSize + 1, ErrTooLarge},
			{"far_past_limit", 1 << 40, ErrTooLarge},
		}
		for _, row := range rows {
			if got := checkSize(row.size); !errors.Is(got, row.want) {
				t.Errorf("%s: checkSize(%d) = %v, want %v", row.name, row.size, got, row.want)
			}
		}
	})

	t.Run("max_size_file_is_read", func(t *testing.T) {
		skipIfReadsCannotBeDenied(t)
		path := writeFile(t, "max.ki", nil)
		// The largest file Load accepts: 4 GiB minus one byte, sparse.
		if err := os.Truncate(path, MaxFileSize); err != nil {
			t.Fatalf("cannot make a sparse file: %v", err)
		}
		if err := os.Chmod(path, 0); err != nil {
			t.Fatalf("cannot chmod %s: %v", path, err)
		}
		// Not rejected for its size: Load goes on to read it, and the read is
		// what fails. A limit that also rejected this size would report
		// ErrTooLarge instead.
		var m SourceManager
		_, err := m.Load(path, "max.ki")
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("Load error = %v, want fs.ErrPermission", err)
		}
		if errors.Is(err, ErrTooLarge) {
			t.Errorf("Load error = %v, a file of MaxFileSize bytes is not too large", err)
		}
	})

	t.Run("rejects_4gib_file", func(t *testing.T) {
		skipIfReadsCannotBeDenied(t)
		path := writeFile(t, "huge.ki", nil)
		// A sparse file: 4 GiB of length, almost no disk.
		if err := os.Truncate(path, 1<<32); err != nil {
			t.Fatalf("cannot make a sparse file: %v", err)
		}
		// With no permissions, any read fails with fs.ErrPermission. Only an
		// implementation that checks the size with os.Stat first (which needs
		// no permission on the file) can report ErrTooLarge.
		if err := os.Chmod(path, 0); err != nil {
			t.Fatalf("cannot chmod %s: %v", path, err)
		}
		var m SourceManager
		_, err := m.Load(path, "huge.ki")
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("Load error = %v, want ErrTooLarge", err)
		}
	})

	// useFakeOS replaces the stat and readFile calls of Load for one test.
	// A nil read makes the test fail if Load reads at all.
	useFakeOS := func(t *testing.T, info fs.FileInfo, read func(string) ([]byte, error)) {
		t.Helper()
		oldStat, oldRead := stat, readFile
		t.Cleanup(func() { stat, readFile = oldStat, oldRead })
		stat = func(string) (fs.FileInfo, error) { return info, nil }
		readFile = func(path string) ([]byte, error) {
			if read == nil {
				t.Fatalf("Load read %s although the file must be rejected first", path)
			}
			return read(path)
		}
	}

	t.Run("check_file_rows", func(t *testing.T) {
		// Fake FileInfo values: no file is made, so this runs as root and on
		// Windows, and a "4 GiB file" costs nothing.
		rows := []struct {
			name string
			mode fs.FileMode
			size int64
			want error
		}{
			{"regular_empty", 0, 0, nil},
			{"regular_at_limit", 0o644, MaxFileSize, nil},
			{"regular_4gib", 0, 1 << 32, ErrTooLarge},
			{"directory", fs.ModeDir | 0o755, 0, errNotRegular},
			{"character_device", fs.ModeDevice | fs.ModeCharDevice | 0o666, 0, errNotRegular},
			{"block_device", fs.ModeDevice | 0o660, 0, errNotRegular},
			{"named_pipe", fs.ModeNamedPipe | 0o644, 0, errNotRegular},
			{"socket", fs.ModeSocket | 0o755, 0, errNotRegular},
			// The mode is checked before the size: a directory of 4 GiB is
			// reported as what it is.
			{"huge_directory", fs.ModeDir | 0o755, 1 << 32, errNotRegular},
		}
		for _, row := range rows {
			got := checkFile(fakeFileInfo{mode: row.mode, size: row.size})
			if !errors.Is(got, row.want) {
				t.Errorf("%s: checkFile = %v, want %v", row.name, got, row.want)
			}
		}
	})

	t.Run("load_checks_file_before_reading", func(t *testing.T) {
		// read is nil when Load must reject the file without reading it.
		rows := []struct {
			name string
			info fakeFileInfo
			read func(string) ([]byte, error)
			want string
		}{
			{"4gib_file", fakeFileInfo{size: 1 << 32}, nil, "cannot read file `f.ki`: file too large (limit 4 GiB)"},
			{"device", fakeFileInfo{mode: fs.ModeDevice | fs.ModeCharDevice}, nil, "cannot read file `f.ki`: not a regular file"},
			{"named_pipe", fakeFileInfo{mode: fs.ModeNamedPipe}, nil, "cannot read file `f.ki`: not a regular file"},
			// The other side of the limit: this file is read.
			{"max_size_file", fakeFileInfo{size: MaxFileSize}, func(string) ([]byte, error) { return []byte("fn"), nil }, ""},
		}
		for _, row := range rows {
			useFakeOS(t, row.info, row.read)
			var m SourceManager
			id, err := m.Load("real-f.ki", "f.ki")
			if row.want == "" {
				if err != nil {
					t.Errorf("%s: Load: %v", row.name, err)
				} else if got := string(m.Contents(id)); got != "fn" {
					t.Errorf("%s: Contents = %q, want %q", row.name, got, "fn")
				}
				continue
			}
			if err == nil || err.Error() != row.want {
				t.Errorf("%s: error = %v, want %q", row.name, err, row.want)
			}
		}
	})

	t.Run("read_error_hides_real_path", func(t *testing.T) {
		// The read fails with the real path inside, as os.ReadFile's errors
		// do. Injected, so it needs no chmod and runs as root; the chmod
		// version is read_error_names_display_path.
		realPath := writeFile(t, "secret-real.ki", []byte("fn"))
		useFakeOS(t, fakeFileInfo{size: 2}, func(path string) ([]byte, error) {
			return nil, &fs.PathError{Op: "open", Path: realPath, Err: fs.ErrPermission}
		})
		var m SourceManager
		_, err := m.Load(realPath, "secret.ki")
		if want := "cannot read file `secret.ki`: permission denied"; err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("errors.Is(err, fs.ErrPermission) = false for %v", err)
		}
	})
}

// fakeFileInfo is a fs.FileInfo with the mode and size a test chooses.
type fakeFileInfo struct {
	mode fs.FileMode
	size int64
}

func (f fakeFileInfo) Name() string       { return "f.ki" }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }
