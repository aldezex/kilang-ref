package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// DirectiveKind says which of the directives of the book a Directive is.
type DirectiveKind uint8

// The directives of the book, as they are written between double braces.
const (
	DirListing DirectiveKind = iota // {{listing TAG REGION}}: a marked region of the code
	DirFile                         // {{file TAG PATH}}: a whole file
	DirDiff                         // {{diff TAG..TAG PATH}}: the changes of a file between two tags
	DirCards                        // {{cards}}: the table of test cards of the chapter
	DirRefTest                      // {{reftest TNN.MM}}: the reference test of a card
)

// Directive is a directive found in the prose of the book. Which fields it
// uses depends on its kind:
//
//	listing   Tag, Target (the region id)
//	file      Tag, Target (the path)
//	diff      Tag, TagTo, Target (the path)
//	cards     none
//	reftest   Target (the card id)
type Directive struct {
	Kind   DirectiveKind
	Tag    string
	TagTo  string
	Target string
	Line   int // 1-based line of the directive in the Markdown
}

// directiveUsage is how each directive is written, for the message about
// wrong arguments. It is only ever looked up.
var directiveUsage = map[string]string{
	"file":    "{{file TAG PATH}}",
	"diff":    "{{diff TAG..TAG PATH}}",
	"listing": "{{listing TAG REGION}}",
	"cards":   "{{cards}}",
	"reftest": "{{reftest TNN.MM}}",
}

// pathTokenRe is what a tag or a path may be made of: the characters that the
// git commands of bookx accept. It is only the first filter: checkToken adds
// that a tag or a path cannot start with `-` (git would read it as an option)
// and that a path cannot be absolute or climb out of the repository.
var pathTokenRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// findDirectives finds the directives in the Markdown of a chapter. A directive
// is `{{name args}}` anywhere in a line, outside the fenced code blocks and the
// code spans, where the same text is just an example. A fenced block is one
// that opens with three or more backticks or tildes at the start of a line
// (indented by at most three spaces) and closes with at least as many of the
// same character; a fence inside a list item or a block quote, and an indented
// code block, are not recognised, because the chapters do not use them. The
// result is in the order of the text. The errors have no Path, because the
// Markdown does not know its own name: the caller fills it in. They are sorted
// by line.
func findDirectives(markdown []byte) ([]Directive, []ScanError) {
	var ds []Directive
	var errs []ScanError
	var open fence // the fenced block we are inside, if any

	for i, line := range splitLines(markdown) {
		n := i + 1
		if open.char != 0 {
			if open.closedBy(line) {
				open = fence{}
			}
			continue
		}
		if f, ok := openingFence(line); ok {
			open = f
			continue
		}
		scanDirectives(line, n, &ds, &errs)
	}
	sortScanErrors(errs)
	return ds, errs
}

// fence is an open fenced code block: the character it is made of (backtick or
// tilde) and how many of them opened it.
type fence struct {
	char   byte
	length int
}

// openingFence reports whether line opens a fenced code block: three or more
// backticks or tildes, indented by at most three spaces. An opening of
// backticks cannot have a backtick in its info string, or it would be a code
// span (```like this```).
func openingFence(line string) (f fence, ok bool) {
	body := strings.TrimLeft(line, " ")
	if len(line)-len(body) > 3 || body == "" || (body[0] != '`' && body[0] != '~') {
		return fence{}, false
	}
	f.char = body[0]
	f.length = len(body) - len(strings.TrimLeft(body, string(f.char)))
	if f.length < 3 || (f.char == '`' && strings.Contains(body[f.length:], "`")) {
		return fence{}, false
	}
	return f, true
}

// closedBy reports whether line closes the fence: at least as many of the same
// character as the opening, and nothing else.
func (f fence) closedBy(line string) bool {
	body := strings.TrimLeft(line, " ")
	if len(line)-len(body) > 3 {
		return false
	}
	body = strings.TrimRight(body, " \t")
	return len(body) >= f.length && strings.Trim(body, string(f.char)) == ""
}

// scanDirectives finds the directives of one line of prose, skipping the code
// spans, and appends them to ds and its problems to errs.
func scanDirectives(line string, n int, ds *[]Directive, errs *[]ScanError) {
	for i := 0; i < len(line); {
		switch {
		case line[i] == '`':
			// A code span runs from a run of backticks to the next run of the
			// same length. With no such run, the backticks are just text.
			run := backtickRun(line, i)
			if end := findBacktickRun(line, i+run, run); end >= 0 {
				i = end + run
			} else {
				i += run
			}
		case strings.HasPrefix(line[i:], "{{"):
			length := strings.Index(line[i+2:], "}}")
			if length < 0 {
				*errs = append(*errs, ScanError{Line: n, Message: "unterminated directive: missing `}}`"})
				return
			}
			d, msg := parseDirective(line[i+2 : i+2+length])
			if msg != "" {
				*errs = append(*errs, ScanError{Line: n, Message: msg})
			} else {
				d.Line = n
				*ds = append(*ds, d)
			}
			i += length + 4
		default:
			i++
		}
	}
}

// backtickRun returns how many backticks start at line[i].
func backtickRun(line string, i int) int {
	j := i
	for j < len(line) && line[j] == '`' {
		j++
	}
	return j - i
}

// findBacktickRun returns where the first run of exactly run backticks starts
// in line at or after from, or -1.
func findBacktickRun(line string, from, run int) int {
	for k := from; k < len(line); {
		if line[k] != '`' {
			k++
			continue
		}
		r := backtickRun(line, k)
		if r == run {
			return k
		}
		k += r
	}
	return -1
}

// parseDirective reads the text between the braces of a directive. It returns
// the directive, or the message of the problem.
func parseDirective(inner string) (d Directive, msg string) {
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return d, "empty directive"
	}
	name, args := fields[0], fields[1:]
	usage, known := directiveUsage[name]
	if !known {
		return d, fmt.Sprintf("unknown directive `%s`: want file, diff, listing, cards or reftest", name)
	}
	badArgs := fmt.Sprintf("bad arguments for `%s`: want %s", name, usage)

	switch name {
	case "cards":
		if len(args) != 0 {
			return d, badArgs
		}
		d.Kind = DirCards
	case "reftest":
		if len(args) != 1 {
			return d, badArgs
		}
		if !cardIDRe.MatchString(args[0]) {
			return d, fmt.Sprintf("invalid card id `%s`: %s", args[0], cardIDHelp)
		}
		d.Kind, d.Target = DirRefTest, args[0]
	case "file":
		if len(args) != 2 {
			return d, badArgs
		}
		if msg := checkToken("tag", args[0]); msg != "" {
			return d, msg
		}
		if msg := checkToken("path", args[1]); msg != "" {
			return d, msg
		}
		d.Kind, d.Tag, d.Target = DirFile, args[0], args[1]
	case "listing":
		if len(args) != 2 {
			return d, badArgs
		}
		if msg := checkToken("tag", args[0]); msg != "" {
			return d, msg
		}
		if !regionIDRe.MatchString(args[1]) {
			return d, fmt.Sprintf("invalid region id `%s`: %s", args[1], regionIDHelp)
		}
		d.Kind, d.Tag, d.Target = DirListing, args[0], args[1]
	case "diff":
		if len(args) != 2 {
			return d, badArgs
		}
		// The range is split at the first `..`; a third dot (`a...b`) would
		// leave one at the start of the second tag, which is not a range.
		from, to, ok := strings.Cut(args[0], "..")
		if !ok || from == "" || to == "" || strings.HasPrefix(to, ".") || strings.Contains(to, "..") {
			return d, badArgs
		}
		for _, tag := range []string{from, to} {
			if msg := checkToken("tag", tag); msg != "" {
				return d, msg
			}
		}
		if msg := checkToken("path", args[1]); msg != "" {
			return d, msg
		}
		d.Kind, d.Tag, d.TagTo, d.Target = DirDiff, from, to, args[1]
	default:
		panic("unreachable")
	}
	return d, ""
}

// checkToken returns the message about a tag or a path that bookx does not
// accept, or "" when it is fine: characters outside pathTokenRe, a leading `-`,
// and, for a path, a leading `/` or a `..` component.
func checkToken(what, s string) string {
	if !pathTokenRe.MatchString(s) {
		return fmt.Sprintf("invalid %s `%s`: only letters, digits, `.`, `_`, `-` and `/` are allowed", what, s)
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Sprintf("invalid %s `%s`: it cannot start with `-`, git would read it as an option", what, s)
	}
	if what == "path" && (strings.HasPrefix(s, "/") || slices.Contains(strings.Split(s, "/"), "..")) {
		return fmt.Sprintf("invalid path `%s`: it has to be relative to the repository, with no `..` component", s)
	}
	return ""
}
