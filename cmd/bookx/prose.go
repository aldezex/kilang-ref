package main

import (
	"regexp"
	"slices"
	"strings"
)

// This file reads the Markdown of a chapter line by line: where each line
// stands among the fences, the containers and the headings. `bookx words`,
// `bookx lint` and `bookx doctest` all read a chapter through it, so they agree
// on what is text and what is code.

// proseLineKind says what a line of a chapter is.
type proseLineKind int

const (
	lineText           proseLineKind = iota // running text, a heading, a table or a directive
	lineFenceOpen                           // the line that opens a fenced code block
	lineFenceBody                           // a line inside a fenced code block
	lineFenceClose                          // the line that closes a fenced code block
	lineContainerOpen                       // a `:::kind` line that opens a container
	lineContainerClose                      // the `:::` line that closes a container
)

// proseLine is one line of a chapter, after its front matter.
type proseLine struct {
	Number    int    // 1-based, counted in the file with its front matter
	Text      string // the whole line
	Kind      proseLineKind
	Container string // the kind of the open container; "" when none
	Title     string // the argument of the `:::kind` line, while that container is open
	Section   string // the title of the last `##` heading
	Session   string // the title of the last `###` heading, when it is a session
	FenceInfo string // on lineFenceOpen and in its body: what follows the opening
	Indent    int    // on lineFenceOpen and in its body: the spaces before the opening
}

// sessionHeadingPrefix starts the title of the heading of a session.
const sessionHeadingPrefix = "Sesión "

// proseLines splits the Markdown of a chapter into its lines, leaving out the
// front matter if the file starts with one. Fences and containers are read as
// the page and `bookx verify` read them: a fence that is never closed, or a
// container, takes the rest of the file, and a `:::kind` line inside an open
// container is text, because containers do not nest.
func proseLines(markdown []byte) []proseLine {
	lines := splitLines(markdown)
	first := 0
	if len(lines) > 0 && lines[0] == "---" {
		if end := slices.Index(lines[1:], "---"); end >= 0 {
			first = end + 2
		}
	}
	out := make([]proseLine, 0, len(lines)-first)
	var open fence
	var cur proseLine // what stays from one line to the next
	for i := first; i < len(lines); i++ {
		line := lines[i]
		l := cur
		l.Number, l.Text, l.Kind = i+1, line, lineText
		switch {
		case open.char != 0:
			l.Kind = lineFenceBody
			if open.closedBy(line) {
				l.Kind = lineFenceClose
				open = fence{}
				cur.FenceInfo, cur.Indent = "", 0
			}
		default:
			if f, ok := openingFence(line); ok {
				open = f
				body := strings.TrimLeft(line, " ")
				l.Kind = lineFenceOpen
				l.FenceInfo = strings.TrimSpace(strings.TrimLeft(body, string(f.char)))
				l.Indent = len(line) - len(body)
				cur.FenceInfo, cur.Indent = l.FenceInfo, l.Indent
			} else if l.Container == "" {
				if m := containerOpenRe.FindStringSubmatch(line); m != nil {
					l.Kind, l.Container, l.Title = lineContainerOpen, m[1], strings.TrimSpace(m[2])
					cur.Container, cur.Title = l.Container, l.Title
				}
			} else if strings.TrimSpace(line) == ":::" {
				l.Kind = lineContainerClose
				cur.Container, cur.Title = "", ""
			}
			if l.Kind == lineText {
				if title, ok := strings.CutPrefix(line, "## "); ok {
					cur.Section, cur.Session = strings.TrimSpace(title), ""
					l.Section, l.Session = cur.Section, ""
				} else if title, ok := strings.CutPrefix(line, "### "); ok {
					cur.Session = ""
					if strings.HasPrefix(title, sessionHeadingPrefix) {
						cur.Session = strings.TrimSpace(title)
					}
					l.Session = cur.Session
				}
			}
		}
		out = append(out, l)
	}
	return out
}

// isTableLine reports whether a line of text is a row of a table.
func isTableLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "|")
}

// isDirectiveLine reports whether a line of text is a directive and nothing else.
func isDirectiveLine(line string) bool {
	text := strings.TrimSpace(line)
	return strings.HasPrefix(text, "{{") && strings.HasSuffix(text, "}}")
}

// codeSpanRe is a code span of a line of text.
var codeSpanRe = regexp.MustCompile("`[^`]*`")

// withoutCodeSpans replaces the code spans of a line with a space, so what is
// left is what the sentence says in words of its own.
func withoutCodeSpans(line string) string {
	return codeSpanRe.ReplaceAllString(line, " ")
}
