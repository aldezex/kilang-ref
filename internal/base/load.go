package base

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// MaxFileSize is the largest file Load accepts, 4 GiB minus one byte: spans
// and line tables use uint32 offsets, so a larger file could not be addressed.
const MaxFileSize = 1<<32 - 1

// ErrTooLarge is the cause Load reports for a file of 4 GiB or more.
var ErrTooLarge = errors.New("file too large (limit 4 GiB)")

// errNotRegular is the cause Load reports for a path that is not a regular
// file: a directory, a device, a pipe.
var errNotRegular = errors.New("not a regular file")

// stat and readFile are the two operating-system calls Load makes. They are
// variables only so that a test can put a fake file in front of Load: a test
// cannot make a 4 GiB file or an unreadable one on every system (root reads
// anything, Windows ignores chmod 0), but it can make a FileInfo say so.
var (
	stat     = os.Stat
	readFile = os.ReadFile
)

// utf8BOM is the UTF-8 byte order mark that some editors put at the start of a
// file.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Load reads the file at realPath, strips a UTF-8 byte order mark if there is
// one (HadBOM remembers it), and registers the contents under displayPath,
// which is the path diagnostics show.
//
// It asks os.Stat first, so a path that is not a regular file or a file
// larger than MaxFileSize is rejected without being read. Every failure has
// the text "cannot read file `<displayPath>`: <reason>" and wraps its cause, so
// errors.Is(err, fs.ErrNotExist) and errors.Is(err, ErrTooLarge) work.
func (m *SourceManager) Load(realPath, displayPath string) (FileID, error) {
	info, err := stat(realPath)
	if err != nil {
		return 0, loadError(displayPath, err)
	}
	if err := checkFile(info); err != nil {
		return 0, loadError(displayPath, err)
	}
	data, err := readFile(realPath)
	if err != nil {
		return 0, loadError(displayPath, err)
	}
	// The file may have grown since Stat. This check bounds nothing in memory
	// (ReadFile has already allocated what it read); it only keeps the offsets
	// of the stored file inside a uint32.
	if err := checkSize(int64(len(data))); err != nil {
		return 0, loadError(displayPath, err)
	}
	hadBOM := bytes.HasPrefix(data, utf8BOM)
	if hadBOM {
		data = data[len(utf8BOM):]
	}
	id := m.Add(displayPath, data)
	m.files[id].hadBOM = hadBOM
	return id, nil
}

// checkFile says whether the file that Stat described may be read: it must be
// a regular file (checked first) of at most MaxFileSize bytes. Load calls it
// before reading anything.
//
// Only a regular file has a size Stat can be trusted with: it reports 0 for a
// device such as /dev/zero, and os.ReadFile would keep reading it until memory
// ran out.
func checkFile(info fs.FileInfo) error {
	if !info.Mode().IsRegular() {
		return errNotRegular
	}
	return checkSize(info.Size())
}

// checkSize returns ErrTooLarge when a file of size bytes is larger than
// MaxFileSize. The size is an int64 so that the comparison compiles on 32-bit
// platforms, where MaxFileSize does not fit in an int.
func checkSize(size int64) error {
	if size > MaxFileSize {
		return ErrTooLarge
	}
	return nil
}

// loadError builds the error Load returns, wrapping cause. The os package
// puts the real path inside its errors; the reason shown to the user should
// name only displayPath, so a *fs.PathError is unwrapped to its Err.
func loadError(displayPath string, cause error) error {
	var pe *fs.PathError
	if errors.As(cause, &pe) {
		cause = pe.Err
	}
	return fmt.Errorf("cannot read file `%s`: %w", displayPath, cause)
}
