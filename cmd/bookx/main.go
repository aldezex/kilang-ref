// Command bookx is the author's tool for the book: it reads the markers in the
// code, the test cards and the directives in the prose, and checks that they
// agree. It exists only in the reference repository and uses nothing but the
// standard library.
//
// The parsers in this package are pure functions: they take the text of a file
// and return values and errors, and never touch git or the disk. The commands
// that read files and call them are added around them.
package main

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
)

func main() {
	fmt.Fprintln(os.Stderr, "usage: bookx <command> [args]")
	os.Exit(2)
}

// ScanError is a problem found in a file while parsing it. Line is 1-based.
type ScanError struct {
	Path    string
	Line    int
	Message string
}

// String formats the error as "<path>:<line>: <message>", the form every
// bookx report uses.
func (e ScanError) String() string {
	return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Message)
}

// sortScanErrors orders errs by path and then by line. The sort is stable, so
// two errors on the same line stay in the order in which they were found.
func sortScanErrors(errs []ScanError) {
	slices.SortStableFunc(errs, func(a, b ScanError) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return a.Line - b.Line
	})
}

// splitLines splits the text of a file into lines, the same way for every
// parser. Both "\n" and "\r\n" end a line, so a file written on Windows reads
// like one written on Linux; a lone "\r" does not end a line and is kept, also
// at the end of a file with no final newline. A byte order mark at the start is
// dropped, and a final newline does not start an extra empty line. An empty
// file has no lines, but an empty line inside the file is kept.
func splitLines(text []byte) []string {
	text = bytes.TrimPrefix(text, []byte("\xef\xbb\xbf"))
	if len(text) == 0 {
		return nil
	}
	s, endedByNewline := strings.CutSuffix(string(text), "\n")
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		// Only a line that a "\n" ended loses its "\r".
		if i < len(ls)-1 || endedByNewline {
			ls[i] = strings.TrimSuffix(l, "\r")
		}
	}
	return ls
}
