// Package kitest is the runner of the golden files of Ki: the .ki files under
// tests/ whose first lines, a header of comments, say how to run the file and
// what the run must print. `ki test` and tests/golden_test.go both use it.
package kitest

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Annotation is an error the file expects, written in the file itself as
// `// ERROR(E0401) @32` (column 32 of the same line) or `// ERROR(E0401) @12:32`
// (line 12, column 32).
type Annotation struct {
	Code      string
	Line, Col uint32
}

// Spec is what the header of a golden file says.
type Spec struct {
	TestID string   // `// test: T10.03`: the id of the card of this test
	Run    []string // `// run: emit tokens` → ["emit", "tokens"]: the command to run
	Args   []string // `// args: --diagnostics=short`: flags that go between Run and the file

	// A nil expectation means the default: for the output streams, that the
	// stream is empty; for the exit code, 1 if some annotation is an error
	// (its code starts with 'E') and 0 otherwise.
	ExpectExit   *int
	ExpectStdout *string // the whole output, each line with its '\n'
	ExpectStderr *string

	Errors []Annotation // every annotation of the file, in file order

	// How the file was written, so that `ki test --update` can write it back
	// the same way: with a UTF-8 byte order mark, and with "\r\n" line ends.
	BOM  bool
	CRLF bool
}

// utf8BOM is the UTF-8 byte order mark that some editors put at the start of a
// file. base strips it when it loads a file but does not export it.
const utf8BOM = "\xef\xbb\xbf"

// runValues are the only commands a header may ask for.
var runValues = []string{"check", "run", "emit tokens", "emit ast", "emit core"}

// headerKeys are the words that `// <word>:` turns into a key of the header.
// Any other word followed by a colon is free text.
var headerKeys = []string{"test", "run", "args", "expect-exit", "expect-stdout", "expect-stderr"}

// ParseHeader reads the header of a golden file and the annotations of the
// whole file. Every error starts with "malformed header: ".
//
// The header is the run of lines at the start of the file that begin with
// `//`. A line `// <key>: <value>` of it sets a key. expect-stdout and
// expect-stderr may instead end their line at the colon and give the text in a
// block: the lines that follow, `// | <text>` for each line of output and
// `// |` for an empty one. A colon followed by nothing but spaces opens a
// block too, because an editor that trims trailing spaces would turn one into
// the other. A key may appear once: a second `// run:` would silently replace
// the first. Annotations are looked for in every line outside those blocks.
func ParseHeader(text []byte) (Spec, error) {
	var spec Spec
	if bytes.HasPrefix(text, []byte(utf8BOM)) {
		spec.BOM = true
		text = text[len(utf8BOM):]
	}
	// The first line end decides: a file with mixed ends is written back with
	// the first one. text is cut at '\n', so an end is CRLF when the byte
	// before the '\n' is '\r'.
	if i := bytes.IndexByte(text, '\n'); i > 0 && text[i-1] == '\r' {
		spec.CRLF = true
	}
	rawLines := bytes.Split(text, []byte("\n"))
	lines := make([]string, len(rawLines))
	for i, l := range rawLines {
		lines[i] = string(bytes.TrimSuffix(l, []byte("\r")))
	}

	seen := map[string]bool{} // the keys the header has set so far
	inHeader := true
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.HasPrefix(line, "//") {
			inHeader = false
		}
		if inHeader {
			if key, rest, ok := splitKey(line); ok {
				if seen[key] {
					return Spec{}, headerError("duplicate key '%s'", key)
				}
				seen[key] = true
				if key == "expect-stdout" || key == "expect-stderr" {
					value, used, err := outputValue(key, rest, lines[i+1:])
					if err != nil {
						return Spec{}, err
					}
					i += used
					if key == "expect-stdout" {
						spec.ExpectStdout = &value
					} else {
						spec.ExpectStderr = &value
					}
					continue
				}
				if err := spec.setKey(key, rest); err != nil {
					return Spec{}, err
				}
				continue
			}
		}
		anns, err := parseAnnotations(line, uint32(i+1))
		if err != nil {
			return Spec{}, err
		}
		spec.Errors = append(spec.Errors, anns...)
	}

	if spec.TestID == "" {
		return Spec{}, headerError("missing key 'test'")
	}
	if spec.Run == nil {
		return Spec{}, headerError("missing key 'run'")
	}
	return spec, nil
}

// headerError builds the error ParseHeader reports.
func headerError(format string, args ...any) error {
	return fmt.Errorf("malformed header: "+format, args...)
}

// splitKey splits a line `// <word>:<rest>` whose word is a key of the header.
// The colon must follow the word at once: `// Note that: x` has the words
// "Note that" before its colon, so it is free text and not the key "note".
func splitKey(line string) (key, rest string, ok bool) {
	body, found := strings.CutPrefix(line, "// ")
	if !found {
		return "", "", false
	}
	word, rest, found := strings.Cut(body, ":")
	if !found || !slices.Contains(headerKeys, word) {
		return "", "", false
	}
	return word, rest, true
}

// setKey stores the value of a key that takes its value on its own line.
// rest is what follows the colon.
func (s *Spec) setKey(key, rest string) error {
	value, found := strings.CutPrefix(rest, " ")
	if !found {
		return headerError("%s needs a space after ':'", key)
	}
	value = strings.TrimSpace(value)
	switch key {
	case "test":
		s.TestID = value
	case "run":
		fields := strings.Fields(value)
		if !slices.Contains(runValues, strings.Join(fields, " ")) {
			return headerError("unknown run value '%s'", value)
		}
		s.Run = fields
	case "args":
		s.Args = strings.Fields(value)
	case "expect-exit":
		n, err := strconv.Atoi(value)
		if err != nil {
			return headerError("bad expect-exit '%s'", value)
		}
		s.ExpectExit = &n
	default:
		panic("unreachable")
	}
	return nil
}

// outputValue reads the text of an expect-stdout or expect-stderr key. rest is
// what follows the colon and following are the lines after the key's line.
// It returns the text and how many of the following lines it used. A rest of
// nothing, or of spaces and tabs only, opens a block.
func outputValue(key, rest string, following []string) (value string, used int, err error) {
	if strings.Trim(rest, " \t") != "" {
		text, found := strings.CutPrefix(rest, " ")
		if !found {
			return "", 0, headerError("%s needs a space after ':'", key)
		}
		return text + "\n", 0, nil
	}
	var sb strings.Builder
	for _, l := range following {
		text, isBlockLine := strings.CutPrefix(l, "// | ")
		switch {
		case l == "// |":
			text = "" // an empty line of output has no space after the bar
		case !isBlockLine:
			return sb.String(), used, nil
		}
		sb.WriteString(text + "\n")
		used++
	}
	return sb.String(), used, nil
}

// annotationMarker starts an annotation: `// ERROR(E0401) @32`.
const annotationMarker = "// ERROR("

// parseAnnotations collects every annotation of a line. lineNo is the number
// of the line, 1-based, which is the line of an annotation written as @COL.
func parseAnnotations(line string, lineNo uint32) ([]Annotation, error) {
	var out []Annotation
	for {
		_, after, found := strings.Cut(line, annotationMarker)
		if !found {
			return out, nil
		}
		code, rest, closed := strings.Cut(after, ")")
		if !closed || !validCode(code) {
			// Without a ')' the whole rest of the line is shown as the code.
			return nil, headerError("bad error code '%s'", code)
		}
		pos, found := strings.CutPrefix(rest, " @")
		if found {
			pos, _, _ = strings.Cut(pos, " ")
		}
		a := Annotation{Code: code, Line: lineNo}
		if !found || !a.setPosition(pos) {
			return nil, headerError("bad position for ERROR(%s): want '@COL' or '@LINE:COL'", code)
		}
		out = append(out, a)
		line = rest
	}
}

// validCode reports whether code is [EW] followed by four digits.
func validCode(code string) bool {
	if len(code) != 5 || (code[0] != 'E' && code[0] != 'W') {
		return false
	}
	for _, c := range code[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// setPosition reads "COL" or "LINE:COL" into a. Lines and columns count from 1,
// so zero is not a position. It returns false if pos is neither form.
func (a *Annotation) setPosition(pos string) bool {
	lineText, colText, absolute := strings.Cut(pos, ":")
	if !absolute {
		colText = lineText
	} else {
		line, err := strconv.ParseUint(lineText, 10, 32)
		if err != nil || line == 0 {
			return false
		}
		a.Line = uint32(line)
	}
	col, err := strconv.ParseUint(colText, 10, 32)
	if err != nil || col == 0 {
		return false
	}
	a.Col = uint32(col)
	return true
}
