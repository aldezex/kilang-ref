package kitest

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
)

// `ki test --update` rewrites the expect-stdout and expect-stderr of a golden
// file with what the run really printed. It touches nothing else: not the
// expect-exit, which the author decides, not the annotations, not the code, and
// not the bytes of any line it leaves alone.
//
// The rewritten key takes the shortest form that reads back to the same text:
//   - an empty output removes the key;
//   - an output of exactly one line is written on the line of the key,
//     `// expect-stdout: 6765`, unless the line is empty or only spaces and tabs:
//     the header reads that as the opening of a block, so it goes in a block;
//   - any other output is a block, `// expect-stdout:` and one `// | ` line for
//     each line of output (`// |` for an empty one).
//
// That holds except in two cases. An output with no final line feed is written
// as if it had one, because the header has no way to say otherwise, so the run
// that follows still fails (`- 6765` and `+ 6765 [no newline at end]`). And an
// output line that ends in a carriage return, in a file whose lines end in LF,
// is written with its \r, and the header reads it without, so that run fails
// with the line both ways (`- x` and `+ x\r`). In a file with CRLF line ends it
// reads back.
//
// The lines are written as the run printed them. A byte that Ki source does not
// accept, such as a control character or an invalid UTF-8 byte, goes into the
// file too, and `ki check` then refuses the file (E0102 or E0101).
//
// A key that the file lacks is inserted after the last header line that is a
// test, run, args or expect-exit key, and expect-stderr after the expect-stdout
// block when there is one, so the order is always stdout then stderr. The lines
// it writes end as the first line of the file does (LF or CRLF), and a byte
// order mark stays where it was.

// updateFile rewrites the file at path, whose contents are data and whose header
// is spec, with the output of a run. Nothing is written if the contents would not
// change. It returns the header of the file as it is afterwards, and whether it
// had to change.
//
// The standard error is written only when the file has no annotations: then it
// is not compared as text (see RunFile), and a block of it would only be noise.
func updateFile(path string, data []byte, spec Spec, stdout, stderr string) (updated Spec, changed bool, err error) {
	var errText *string
	if len(spec.Errors) == 0 {
		errText = &stderr
	}
	text, err := rewriteExpectations(data, spec, stdout, errText)
	if err != nil {
		return Spec{}, false, err
	}
	if !bytes.Equal(text, data) {
		if err := os.WriteFile(path, text, 0o644); err != nil {
			return Spec{}, false, fmt.Errorf("cannot write file: %v", reason(err))
		}
		changed = true
	}
	updated, err = ParseHeader(text)
	return updated, changed, err
}

// lineRange is a run of lines of a file, [start, end), such as a key and its
// block. found is false for a key that the file lacks.
type lineRange struct {
	start, end int
	found      bool
}

// headerLayout says where the keys that --update cares about are.
type headerLayout struct {
	stdout, stderr lineRange
	// anchor is the index of the last line of the header that is a test, run,
	// args or expect-exit key: a key that is missing goes right after it.
	anchor int
}

// scanHeader finds the keys in the lines of a file, with the same notion of
// header as ParseHeader: the run of lines that start with `//` at the start of
// the file. text holds the lines without their line ends.
func scanHeader(text []string) (headerLayout, error) {
	h := headerLayout{anchor: -1}
	for i := 0; i < len(text) && strings.HasPrefix(text[i], "//"); i++ {
		key, rest, ok := splitKey(text[i])
		if !ok {
			continue
		}
		switch key {
		case "expect-stdout", "expect-stderr":
			_, used, err := outputValue(key, rest, text[i+1:])
			if err != nil {
				return headerLayout{}, err
			}
			r := lineRange{start: i, end: i + 1 + used, found: true}
			if key == "expect-stdout" {
				h.stdout = r
			} else {
				h.stderr = r
			}
		default:
			h.anchor = i
		}
	}
	return h, nil
}

// expectLines returns the lines, without line ends, that write the output of a
// run as the value of key (`expect-stdout` or `expect-stderr`): none for an
// empty output. An output that does not end in a line feed is written as if it
// did, because the header has no way to say otherwise.
func expectLines(key, output string) []string {
	if output == "" {
		return nil
	}
	body := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(body) == 1 && strings.Trim(body[0], " \t") != "" {
		return []string{"// " + key + ": " + body[0]}
	}
	lines := []string{"// " + key + ":"}
	for _, l := range body {
		if l == "" {
			lines = append(lines, "// |")
		} else {
			lines = append(lines, "// | "+l)
		}
	}
	return lines
}

// edit replaces the n lines that start at line at with lines.
type edit struct {
	at, n int
	lines []string
}

// rewriteExpectations returns the text of a golden file, whose header is spec,
// with its expect-stdout set to stdout and, unless stderr is nil, its
// expect-stderr set to *stderr. Every line it does not replace is copied as it
// is, line end included.
func rewriteExpectations(data []byte, spec Spec, stdout string, stderr *string) ([]byte, error) {
	prefix := ""
	if spec.BOM {
		data, prefix = data[len(utf8BOM):], utf8BOM
	}
	eol := "\n"
	if spec.CRLF {
		eol = "\r\n"
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	if last := len(lines) - 1; len(lines[last]) == 0 {
		lines = lines[:last] // the text ended in a line feed, or was empty
	}
	text := make([]string, len(lines))
	for i, l := range lines {
		text[i] = strings.TrimSuffix(strings.TrimSuffix(string(l), "\n"), "\r")
	}
	h, err := scanHeader(text)
	if err != nil {
		return nil, err
	}

	// replace says what happens to a key: it takes the place of the lines it
	// has, or it goes in at the given index.
	var edits []edit
	replace := func(r lineRange, insertAt int, newLines []string) {
		switch {
		case r.found:
			edits = append(edits, edit{r.start, r.end - r.start, newLines})
		case len(newLines) > 0:
			edits = append(edits, edit{insertAt, 0, newLines})
		}
	}
	replace(h.stdout, h.anchor+1, expectLines("expect-stdout", stdout))
	if stderr != nil {
		insertAt := h.anchor + 1
		if h.stdout.found {
			insertAt = h.stdout.end
		}
		replace(h.stderr, insertAt, expectLines("expect-stderr", *stderr))
	}
	// Two keys that go in at the same index stay in the order they were added,
	// stdout first.
	slices.SortStableFunc(edits, func(a, b edit) int { return cmp.Compare(a.at, b.at) })

	var out bytes.Buffer
	out.WriteString(prefix)
	next := 0 // the first line that has not been copied or replaced yet
	for _, e := range edits {
		for _, l := range lines[next:e.at] {
			out.Write(l)
		}
		if e.at > next && len(e.lines) > 0 && !bytes.HasSuffix(lines[e.at-1], []byte("\n")) {
			// New lines after a last line that had no line end. An edit that
			// starts where the one before it ended (e.at == next) has nothing
			// to finish: that edit wrote its lines with their line ends, so a
			// second key at the same place does not add a blank line.
			out.WriteString(eol)
		}
		for _, l := range e.lines {
			out.WriteString(l + eol)
		}
		next = e.at + e.n
	}
	for _, l := range lines[next:] {
		out.Write(l)
	}
	return out.Bytes(), nil
}
