// Package out is the one writer of stdout and stderr that the backends of Ki
// share (spec 8.7). It is a leaf of the runtime: it imports only the standard
// library, and no scheduler or heap.
//
// A Writer keeps what it is given in a buffer and passes it on in large pieces,
// because a write to a file descriptor is a system call and a program that
// prints a line at a time would make thousands. Two things empty the buffer
// besides a full buffer. A Writer made for a terminal empties it at each line
// break, so that a person sees a line as it is printed. And the program empties
// it before it ends: os.Exit skips deferred calls, so a program that ended
// with `defer out.FlushAll()` would lose its last output, and Exit does the
// flush itself, in plain sight. That last flush is also where a buffered
// Writer reports that its bytes could not be written, so Exit looks at it.
package out

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
)

// defaultSize is the size of the buffer of a Writer made with size 0: 64 KiB.
const defaultSize = 64 << 10

// Writer is a buffered writer that several goroutines may use at once: a mutex
// makes each Write, WriteString and Flush a whole. The bytes of one call are
// never mixed with those of another, so a goroutine that writes one line in one
// call gets that line out intact, in whatever order the calls came.
//
// The zero Writer is not usable: make one with New or ForFile.
type Writer struct {
	mu sync.Mutex
	w  *bufio.Writer
	// lineFlush is true for a Writer that sends what it has written up to the
	// last line break on, at the end of each Write.
	lineFlush bool
}

// New returns a Writer that writes to w through a buffer of size bytes. A size
// of 0 (or less) is 64 KiB. If lineFlush is true, each Write sends on all it
// has written up to the last '\n' it contains; what follows that '\n' stays in
// the buffer. A write error does not show in the call that buffered the bytes:
// it shows in the Write or Flush that sends them on.
func New(w io.Writer, size int, lineFlush bool) *Writer {
	if size <= 0 {
		size = defaultSize
	}
	return &Writer{w: bufio.NewWriterSize(w, size), lineFlush: lineFlush}
}

// ForFile returns a Writer for f with the default buffer. It flushes at each
// line break when f is a terminal, which it takes to be a character device (Mode
// has os.ModeCharDevice), and it does not when f is a file or a pipe, or when f
// cannot be inspected. A character device that is not a terminal, such as
// /dev/null, counts as one: the test is the mode, not the terminal.
func ForFile(f *os.File) *Writer {
	info, err := f.Stat()
	return New(f, 0, err == nil && info.Mode()&os.ModeCharDevice != 0)
}

// Write buffers p. It returns len(p) unless the write of the bytes that it
// sends on fails, and then the number of bytes of p that were accepted and the
// error. On a line-flushing Writer, if p contains a '\n', it then sends on
// everything buffered up to the last one of them.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.lineFlush {
		return w.w.Write(p)
	}
	// Everything through the last '\n' is a whole number of lines, which is what
	// a terminal gets; the rest is the start of the next line and waits.
	cut := bytes.LastIndexByte(p, '\n') + 1
	n, err := w.w.Write(p[:cut])
	if err == nil && cut > 0 {
		err = w.w.Flush()
	}
	if err != nil {
		return n, err
	}
	m, err := w.w.Write(p[cut:])
	return n + m, err
}

// WriteString is Write for a string: it behaves as Write([]byte(s)).
func (w *Writer) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// Flush sends on everything buffered and returns the error of the write if it
// fails. Once a write has failed, every later Write and Flush returns that error
// again.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Flush()
}

// TryFlush flushes w if it has a Flush method and returns the error of that
// flush, and does nothing and returns nil if it has none: a plain io.Writer holds
// nothing back. It is for a function that takes any io.Writer and is given a
// Writer of this package when it runs for real.
func TryFlush(w io.Writer) error {
	if f, ok := w.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}

// Stdout and Stderr are the Writers of the process's standard output and
// standard error. Stderr flushes at each line break whatever it is attached to,
// so that a message about an error is never held back behind a buffer.
var (
	Stdout = ForFile(os.Stdout)
	Stderr = New(os.Stderr, 0, true)
)

// FlushAll flushes Stdout and then Stderr. It returns the error of the flush of
// Stdout, which is the one that matters: Stderr is where a failure is reported,
// so there is nowhere to report that one, and its error is dropped.
func FlushAll() error {
	err := Stdout.Flush()
	_ = Stderr.Flush()
	return err
}

// Exit flushes Stdout and Stderr and ends the process with the exit code code.
// It exists because os.Exit does not run deferred calls, so the flush cannot be
// left to one: whoever ends the process calls Exit, and not os.Exit.
//
// A buffered Stdout has not written what it holds, so a full disk shows here,
// when the rest is sent on, or earlier, in the Write that sent an earlier part:
// a Writer keeps its first error and returns it again. If code is 0 and that
// happens, Exit writes "ki: cannot write to stdout: <error>" to Stderr and ends
// with 1, because an answer that was lost is not a success. Any other code
// stays as it is, and Exit writes nothing: the run has ended for a reason that
// it reported already (a diagnostic, the message of a panic, or the driver's own
// "cannot write to stdout" when a line-flushing Writer failed in a Write), and
// the code is what a script looks at.
func Exit(code int) {
	if err := FlushAll(); err != nil && code == 0 {
		// Stderr sends on what ends in a line break, so the message is out as
		// soon as it is written. If Stderr cannot be written either, there is
		// nowhere else to say so.
		_, _ = fmt.Fprintf(Stderr, "ki: cannot write to stdout: %v\n", err)
		code = 1
	}
	os.Exit(code)
}
