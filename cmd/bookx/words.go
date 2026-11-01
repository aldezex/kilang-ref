package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// This file is `bookx words`: it counts the flow words of a chapter, the
// measure of the budget that docs/superpowers/process/prose.md sets for the
// prose (3 000 words per chapter). The count is made on the Markdown as it is
// written, with the directives still unresolved.

// excludedKinds are the containers whose body is not part of the flow: the
// reader opens them when she needs them. The page shows each one folded.
var excludedKinds = map[string]bool{"solution": true, "answer": true, "more": true}

// numberedMarkerRe is the first word of a numbered list item, `1.` or `2)`.
var numberedMarkerRe = regexp.MustCompile(`^\d+[.)]$`)

// flowWords returns the number of flow words in the Markdown of a chapter:
// the words of the running text. It leaves out
//
//   - the front matter, if the file starts with one;
//   - the fenced code blocks, fences included;
//   - the lines of a table (a line that starts with `|`);
//   - the directives (a line that is `{{…}}` and nothing else);
//   - the `:::kind` and `:::` lines of the containers themselves, so the
//     title of a `:::more` is not counted either; and
//   - the body of the containers `:::solution`, `:::answer` and `:::more`.
//
// A word is a run of characters between spaces that has at least one letter or
// digit, so a dash, an arrow or the `-` of a list item is not a word, and
// neither is the `1.` of a numbered one. Headings, list items, block quotes,
// and the bodies of the other containers do count. The fences and containers
// are read as the page and `bookx verify` read them, and a fence that is never
// closed (or a container) takes the rest of the file.
func flowWords(markdown []byte) int {
	lines := splitLines(markdown)
	if len(lines) > 0 && lines[0] == "---" {
		if end := slices.Index(lines[1:], "---"); end >= 0 {
			lines = lines[end+2:]
		}
	}
	count := 0
	var open fence  // the fenced block we are inside, if any
	var kind string // kind of the open container; "" when none
	for _, line := range lines {
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
		if kind == "" {
			if m := containerOpenRe.FindStringSubmatch(line); m != nil {
				kind = m[1]
				continue
			}
		} else if strings.TrimSpace(line) == ":::" {
			kind = ""
			continue
		}
		if excludedKinds[kind] {
			continue
		}
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "|") || (strings.HasPrefix(text, "{{") && strings.HasSuffix(text, "}}")) {
			continue
		}
		count += wordsIn(text)
	}
	return count
}

// wordsIn counts the words of one line of running text.
func wordsIn(line string) int {
	fields := strings.Fields(line)
	if len(fields) > 0 && numberedMarkerRe.MatchString(fields[0]) {
		fields = fields[1:]
	}
	n := 0
	for _, f := range fields {
		if strings.IndexFunc(f, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			n++
		}
	}
	return n
}

// runWords is `bookx words <file>…`: it prints `<flow words> <file>` for each
// file, in the order given. A file that cannot be read is reported on stderr,
// the others are still counted, and the exit code is 1.
func runWords(args []string, stdout, stderr io.Writer) int {
	_, files, ok := parseArgs("words", args, -1, false, stderr)
	if !ok {
		return exitUsage
	}
	code := exitOK
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(stderr, "bookx words: %v\n", err)
			code = exitFail
			continue
		}
		fmt.Fprintf(stdout, "%d %s\n", flowWords(data), f)
	}
	return code
}
