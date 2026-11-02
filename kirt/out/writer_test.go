package out_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/aldezex/kilang/kirt/out"
)

// failingWriter is an io.Writer whose every Write fails with err and writes nothing.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriter(t *testing.T) {
	t.Run("buffers_until_flush", func(t *testing.T) {
		var dest bytes.Buffer
		w := out.New(&dest, 0, false)
		if n, err := w.WriteString("a\n"); n != 2 || err != nil {
			t.Errorf("WriteString(\"a\\n\") = %d, %v, want 2, nil", n, err)
		}
		if got := dest.String(); got != "" {
			t.Errorf("after WriteString(\"a\\n\"), the destination holds %q, want it empty", got)
		}
		if n, err := w.Write([]byte("b")); n != 1 || err != nil {
			t.Errorf("Write(\"b\") = %d, %v, want 1, nil", n, err)
		}
		if got := dest.String(); got != "" {
			t.Errorf("after Write(\"b\"), the destination holds %q, want it empty", got)
		}
		if err := w.Flush(); err != nil {
			t.Errorf("Flush() = %v, want nil", err)
		}
		if got, want := dest.String(), "a\nb"; got != want {
			t.Errorf("after Flush, the destination holds %q, want %q", got, want)
		}
	})

	t.Run("flushes_on_newline_when_terminal", func(t *testing.T) {
		// Each row writes input to a line-flushing Writer and says what the
		// destination holds right after: everything through the last '\n', and
		// not what follows it.
		rows := []struct {
			name, input, want string
		}{
			{"line_then_partial_line", "a\nb", "a\n"},
			{"line_only", "a\n", "a\n"},
			{"newline_only", "\n", "\n"},
			{"no_newline", "ab", ""},
			{"two_lines_then_partial_line", "a\nb\nc", "a\nb\n"},
			{"newline_first", "\nab", "\n"},
			{"empty", "", ""},
		}
		for _, row := range rows {
			writes := []struct {
				name string
				do   func(w *out.Writer) (int, error)
			}{
				{"WriteString", func(w *out.Writer) (int, error) { return w.WriteString(row.input) }},
				{"Write", func(w *out.Writer) (int, error) { return w.Write([]byte(row.input)) }},
			}
			for _, write := range writes {
				var dest bytes.Buffer
				w := out.New(&dest, 0, true)
				if n, err := write.do(w); n != len(row.input) || err != nil {
					t.Errorf("%s: %s(%q) = %d, %v, want %d, nil", row.name, write.name, row.input, n, err, len(row.input))
				}
				if got := dest.String(); got != row.want {
					t.Errorf("%s: after %s(%q), the destination holds %q, want %q", row.name, write.name, row.input, got, row.want)
				}
				// Nothing is lost: what waited in the buffer comes out with Flush.
				if err := w.Flush(); err != nil {
					t.Errorf("%s: Flush() = %v, want nil", row.name, err)
				}
				if got := dest.String(); got != row.input {
					t.Errorf("%s: after %s(%q) and Flush, the destination holds %q, want the input", row.name, write.name, row.input, got)
				}
			}
		}

		// What waits in the buffer goes out with the next line break, together
		// with what that write brings.
		var dest bytes.Buffer
		w := out.New(&dest, 0, true)
		steps := []struct{ write, want string }{
			{"a\nb", "a\n"},
			{"c", "a\n"},
			{"d\ne", "a\nbcd\n"},
		}
		for _, step := range steps {
			if _, err := w.WriteString(step.write); err != nil {
				t.Fatalf("WriteString(%q) = %v, want nil", step.write, err)
			}
			if got := dest.String(); got != step.want {
				t.Errorf("after WriteString(%q), the destination holds %q, want %q", step.write, got, step.want)
			}
		}
	})

	t.Run("zero_size_means_64k", func(t *testing.T) {
		// A buffer of n bytes holds exactly n bytes without sending any on, and
		// sends them all when the next byte comes. The writes are of 1 KiB, so
		// that none is large enough to skip the buffer.
		rows := []struct {
			name  string
			size  int
			bytes int // the size of the buffer that the row expects
		}{
			{"zero", 0, 64 << 10},
			{"negative", -1, 64 << 10},
			{"explicit", 8, 8},
		}
		for _, row := range rows {
			var dest bytes.Buffer
			w := out.New(&dest, row.size, false)
			chunk := strings.Repeat("x", min(1024, row.bytes))
			for written := 0; written < row.bytes; written += len(chunk) {
				if _, err := w.WriteString(chunk); err != nil {
					t.Fatalf("%s: WriteString = %v, want nil", row.name, err)
				}
			}
			if got := dest.Len(); got != 0 {
				t.Errorf("%s: after %d bytes, the destination holds %d, want 0", row.name, row.bytes, got)
			}
			if _, err := w.WriteString("x"); err != nil {
				t.Fatalf("%s: WriteString = %v, want nil", row.name, err)
			}
			if got := dest.Len(); got != row.bytes {
				t.Errorf("%s: after %d bytes and one more, the destination holds %d, want %d", row.name, row.bytes, got, row.bytes)
			}
		}
	})

	t.Run("concurrent_writes_do_not_interleave", func(t *testing.T) {
		const (
			writers = 8
			lines   = 1000
			width   = 64 // bytes in a line, the '\n' included
		)
		for _, lineFlush := range []bool{false, true} {
			var dest bytes.Buffer
			w := out.New(&dest, 0, lineFlush)
			// Another goroutine flushes all the while: a Flush that did not take
			// the lock would write the buffer out in the middle of a Write.
			stop := make(chan struct{})
			var flusher sync.WaitGroup
			flusher.Add(1)
			go func() {
				defer flusher.Done()
				for {
					select {
					case <-stop:
						return
					default:
						if err := w.Flush(); err != nil {
							t.Errorf("lineFlush %v: Flush() = %v, want nil", lineFlush, err)
							return
						}
						runtime.Gosched()
					}
				}
			}()
			var wg sync.WaitGroup
			for g := range writers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := range lines {
						// The goroutine's letter, its line number and the letter
						// again up to the width: a line that took bytes from two
						// goroutines shows two letters.
						line := fmt.Sprintf("%c%04d", 'a'+g, i)
						line += strings.Repeat(string(rune('a'+g)), width-1-len(line)) + "\n"
						if n, err := w.WriteString(line); n != width || err != nil {
							t.Errorf("goroutine %d: WriteString of line %d = %d, %v, want %d, nil", g, i, n, err, width)
							return
						}
					}
				}()
			}
			wg.Wait()
			close(stop)
			flusher.Wait()
			if err := w.Flush(); err != nil {
				t.Fatalf("lineFlush %v: Flush() = %v, want nil", lineFlush, err)
			}

			got := strings.Split(strings.TrimSuffix(dest.String(), "\n"), "\n")
			if len(got) != writers*lines {
				t.Fatalf("lineFlush %v: the destination has %d lines, want %d", lineFlush, len(got), writers*lines)
			}
			next := make([]int, writers) // the line number each goroutine should be at
			for k, line := range got {
				if len(line) != width-1 || line[0] < 'a' || line[0] >= 'a'+writers {
					t.Fatalf("lineFlush %v: line %d is %q, which is not a line that was written", lineFlush, k, line)
				}
				g := int(line[0] - 'a')
				i, err := strconv.Atoi(line[1:5])
				want := fmt.Sprintf("%c%04d", 'a'+g, i)
				want += strings.Repeat(string(rune('a'+g)), width-1-len(want))
				if err != nil || line != want {
					t.Fatalf("lineFlush %v: line %d is %q, which is not a line that was written", lineFlush, k, line)
				}
				// Each goroutine wrote its lines one after the other, so they must
				// come out in that order, none missing and none twice.
				if i != next[g] {
					t.Fatalf("lineFlush %v: line %d is line %d of goroutine %d, want line %d", lineFlush, k, i, g, next[g])
				}
				next[g]++
			}
			for g, n := range next {
				if n != lines {
					t.Errorf("lineFlush %v: goroutine %d has %d lines in the destination, want %d", lineFlush, g, n, lines)
				}
			}
		}
	})

	t.Run("write_errors_are_reported_when_sent_on", func(t *testing.T) {
		// A destination whose every write fails, like a full disk.
		full := errors.New("no space left on device")
		// Without line flushing, the bytes are only buffered: the error comes
		// with the Flush, and then with every later call.
		w := out.New(failingWriter{full}, 0, false)
		if n, err := w.WriteString("a\n"); n != 2 || err != nil {
			t.Errorf("WriteString(\"a\\n\") = %d, %v, want 2, nil: the bytes are only buffered", n, err)
		}
		if err := w.Flush(); !errors.Is(err, full) {
			t.Errorf("Flush() = %v, want the error of the destination", err)
		}
		if _, err := w.WriteString("b"); !errors.Is(err, full) {
			t.Errorf("WriteString(\"b\") after the failure = %v, want the error of the destination", err)
		}
		if err := w.Flush(); !errors.Is(err, full) {
			t.Errorf("a second Flush() = %v, want the error of the destination", err)
		}
		// With line flushing, the write that sends the line on has the error.
		w = out.New(failingWriter{full}, 0, true)
		if _, err := w.WriteString("a\nb"); !errors.Is(err, full) {
			t.Errorf("WriteString(\"a\\nb\") with line flushing = %v, want the error of the destination", err)
		}
		// A line flushing Writer that has no line break to send on has no error yet.
		w = out.New(failingWriter{full}, 0, true)
		if n, err := w.WriteString("a"); n != 1 || err != nil {
			t.Errorf("WriteString(\"a\") with line flushing = %d, %v, want 1, nil", n, err)
		}
	})

	t.Run("flush_all_flushes_stdout_then_stderr", func(t *testing.T) {
		// Both process writers go to one buffer, so that the order they are
		// flushed in shows in it.
		savedOut, savedErr := out.Stdout, out.Stderr
		t.Cleanup(func() { out.Stdout, out.Stderr = savedOut, savedErr })
		var dest bytes.Buffer
		out.Stdout, out.Stderr = out.New(&dest, 0, false), out.New(&dest, 0, false)
		out.Stderr.WriteString("e")
		out.Stdout.WriteString("o")
		if got := dest.String(); got != "" {
			t.Fatalf("before FlushAll, the destination holds %q, want it empty", got)
		}
		out.FlushAll()
		if got, want := dest.String(), "oe"; got != want {
			t.Errorf("after FlushAll, the destination holds %q, want %q", got, want)
		}

		// FlushAll returns the error of the flush of Stdout, and only that one:
		// Stderr is where a failure is reported, so its own has nowhere to go.
		full, broken := errors.New("no space left on device"), errors.New("broken pipe")
		rows := []struct {
			name           string
			stdout, stderr io.Writer
			want           error
		}{
			{"both_work", &dest, &dest, nil},
			{"stdout_fails", failingWriter{full}, &dest, full},
			{"stderr_fails", &dest, failingWriter{broken}, nil},
			{"both_fail", failingWriter{full}, failingWriter{broken}, full},
		}
		for _, row := range rows {
			out.Stdout, out.Stderr = out.New(row.stdout, 0, false), out.New(row.stderr, 0, false)
			out.Stdout.WriteString("o")
			out.Stderr.WriteString("e")
			if err := out.FlushAll(); !errors.Is(err, row.want) {
				t.Errorf("%s: FlushAll() = %v, want %v", row.name, err, row.want)
			}
		}
	})
}
