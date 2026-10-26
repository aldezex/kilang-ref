package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// RegionMode says how the book shows a region of code.
type RegionMode uint8

// The modes of a region, as they are written after the region id.
const (
	RegionFull     RegionMode = iota // the whole code, as it is
	RegionHint                       // a text that stands in for the code until the reader tries
	RegionSolution                   // the code, folded in the book
	RegionTest                       // a test, shown before the code that passes it
)

// regionModes maps the word that follows a region id to its mode. It is only
// ever looked up, never iterated, so the order of its entries does not matter.
var regionModes = map[string]RegionMode{
	"full":     RegionFull,
	"hint":     RegionHint,
	"solution": RegionSolution,
	"test":     RegionTest,
}

// regionIDRe is the shape of a region id: two or more lowercase words joined by
// dots, like `lexer.line_rule`. Ids do not change between chapters.
var regionIDRe = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)

// regionIDHelp finishes the message about a bad region id, in the parser of
// the markers and in the one of the directives.
const regionIDHelp = "write lowercase words joined by dots, as in `lexer.line_rule`"

// Region is a piece of a source file that the book can quote: the lines
// between a @book marker and its @end marker.
type Region struct {
	ID        string
	Mode      RegionMode
	Hint      string   // the text of a RegionHint region; empty for the others
	Path      string   // the file the region comes from
	FirstLine int      // line number in that file of Lines[0], the line after the @book marker
	Lines     []string // the code without the markers; an @elide line is shown as a comment of dots
}

// marker is a line that is a region marker: a comment whose text starts with
// one of the words @book, @end and @elide.
type marker struct {
	indent string // the whitespace before the comment
	leader string // "//" or "#"
	word   string // "book", "end" or "elide"
	rest   string // what follows the word on the line, trimmed
}

// parseMarker reports whether line is a marker. It has to start with a comment
// leader, `//` or `#` as in Go and in shell or YAML, and the word has to be
// followed by a space or by the end of the line, so that a line such as
// `x := 1 // @book a.b full` or a comment about @booking is plain code.
func parseMarker(line string) (marker, bool) {
	body := strings.TrimLeft(line, " \t")
	m := marker{indent: line[:len(line)-len(body)]}
	switch {
	case strings.HasPrefix(body, "//"):
		m.leader = "//"
	case strings.HasPrefix(body, "#"):
		m.leader = "#"
	default:
		return marker{}, false
	}
	body = strings.TrimLeft(body[len(m.leader):], " \t")
	word, rest, _ := cutWord(body)
	switch word {
	case "@book", "@end", "@elide":
		m.word = word[1:]
		m.rest = strings.TrimSpace(rest)
		return m, true
	}
	return marker{}, false
}

// cutWord splits s at its first run of spaces or tabs: it returns the word
// before it and what follows, trimmed. found is false when s has no such run.
func cutWord(s string) (word, rest string, found bool) {
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, "", false
	}
	return s[:i], strings.TrimLeft(s[i:], " \t"), true
}

// openRegion is a region whose @end has not been read yet.
type openRegion struct {
	region Region
	line   int  // line of its @book marker
	valid  bool // false when the marker had a problem: the region is read but not returned
}

// scanRegions finds the regions marked in a source file. A region is the lines
// between `@book <id> <mode>` and `@end`, each marker written as a comment on
// a line of its own. The marker lines are not part of the region, and a line
// that holds only `@elide` becomes a comment of dots with the same indentation
// (`// ...`, or `# ...` when the marker uses `#`), standing for code the book
// leaves out. In `hint` mode the id is followed by the hint, in double quotes.
//
// The regions come in the order of the file. A region with a problem is not
// returned, and every problem is one ScanError, sorted by line. Regions do not
// nest: a @book inside an open region reports the open one as never closed.
func scanRegions(path string, text []byte) ([]Region, []ScanError) {
	var regions []Region
	var errs []ScanError
	fail := func(line int, format string, args ...any) {
		errs = append(errs, ScanError{Path: path, Line: line, Message: fmt.Sprintf(format, args...)})
	}
	firstSeen := map[string]int{} // region id -> line of its @book
	var open *openRegion

	closeUnclosed := func() {
		if open != nil {
			fail(open.line, "region `%s` is never closed: it needs an @end line", open.region.ID)
			open = nil
		}
	}

	for i, line := range splitLines(text) {
		n := i + 1
		m, isMarker := parseMarker(line)
		if !isMarker {
			if open != nil && open.valid {
				open.region.Lines = append(open.region.Lines, line)
			}
			continue
		}
		switch m.word {
		case "book":
			closeUnclosed()
			open = &openRegion{region: Region{Path: path, FirstLine: n + 1}, line: n}
			msg := parseBook(m.rest, &open.region)
			id := open.region.ID
			first, seen := firstSeen[id]
			if msg == "" && seen {
				msg = fmt.Sprintf("duplicate region id `%s`, first defined at line %d", id, first)
			}
			if id != "" && !seen {
				firstSeen[id] = n
			}
			if msg != "" {
				fail(n, "%s", msg)
			} else {
				open.valid = true
			}
		case "end":
			if open == nil {
				fail(n, "@end without a matching @book")
				continue
			}
			if open.valid {
				regions = append(regions, open.region)
			}
			open = nil
		case "elide":
			if open == nil {
				fail(n, "@elide outside a region")
				continue
			}
			if open.valid {
				open.region.Lines = append(open.region.Lines, m.indent+m.leader+" ...")
			}
		}
	}
	closeUnclosed()
	sortScanErrors(errs)
	return regions, errs
}

// parseBook reads what follows `@book` (the id, the mode and, for a hint, its
// text) into r. It returns the message of the first problem, or "" when there
// is none. The id is set as soon as it is read, even if the rest is wrong, so
// that a second region with the same id is still noticed.
func parseBook(rest string, r *Region) string {
	idText, rest, _ := cutWord(rest)
	if idText == "" {
		return "@book needs a region id and a mode"
	}
	if !regionIDRe.MatchString(idText) {
		return fmt.Sprintf("invalid region id `%s`: %s", idText, regionIDHelp)
	}
	r.ID = idText
	modeText, rest, _ := cutWord(rest)
	if modeText == "" {
		return fmt.Sprintf("@book `%s` needs a mode: full, hint, solution or test", idText)
	}
	mode, ok := regionModes[modeText]
	if !ok {
		return fmt.Sprintf("unknown region mode `%s`: want full, hint, solution or test", modeText)
	}
	r.Mode = mode
	if mode != RegionHint {
		if rest != "" {
			return fmt.Sprintf("mode `%s` takes no text", modeText)
		}
		return ""
	}
	if !strings.HasPrefix(rest, `"`) {
		return "mode `hint` needs a quoted text, as in `hint \"write the loop\"`"
	}
	hint, err := strconv.Unquote(rest)
	if err != nil {
		return "the hint text is not a valid quoted string"
	}
	r.Hint = hint
	return ""
}
