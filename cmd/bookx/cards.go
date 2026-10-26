package main

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Card is a test card: the description of one test of a chapter, written in
// book/tests/chNN.tests. The text fields are in Spanish, like the book.
type Card struct {
	ID     string // "T05.07"
	Name   string // "lexer.pipe_continues_line"
	Level  string // "core" or "stretch"
	Kind   string // "unit", "prop", "example", "bench", "golden", "diag" or "run"
	File   string // the test file, or the .ki file for golden, diag and run
	Case   string
	Input  string // empty when the card has no input
	Expect string
	Why    string
	Ref    string // the name of the test, or the path of the .ki file
	Line   int    // line of the card's header in its file
}

var (
	// cardIDRe is the shape of a card id: TNN.MM, where NN is the chapter and
	// MM the number in it (two digits, or three from 100 on).
	cardIDRe = regexp.MustCompile(`^T(\d{2})\.(\d{2}|[1-9]\d{2})$`)
	// cardFileRe is the name of a file of cards.
	cardFileRe = regexp.MustCompile(`^ch(\d{2})\.tests$`)
	// cardHeaderRe splits a header line, `[T05.07] name level: core`, into the
	// text between the brackets and what follows them.
	cardHeaderRe = regexp.MustCompile(`^\[([^\]]*)\](.*)$`)
	// cardFieldRe splits a field line, `why:  text`, into its key and its text.
	cardFieldRe = regexp.MustCompile(`^([a-z]+):(.*)$`)
)

// cardIDHelp finishes the message about a bad card id, in the parser of the
// cards and in the one of the directives.
const cardIDHelp = "write it as TNN.MM, as in `T05.07`"

// cardKinds is the set of values of `kind`. It is only ever looked up.
var cardKinds = map[string]bool{
	"unit": true, "prop": true, "example": true, "bench": true,
	"golden": true, "diag": true, "run": true,
}

// cardKeys is the set of keys of the field lines. `kind` also carries the
// file, and `level` lives in the header, so neither of those is listed twice.
var cardKeys = map[string]bool{
	"kind": true, "case": true, "input": true, "expect": true, "why": true, "ref": true,
}

// cardBuilder is a card that is being read: it is finished by a blank line, by
// the next header or by the end of the file.
type cardBuilder struct {
	card   Card
	label  string               // how messages name the card: its id, or what stands where the id should be
	bad    bool                 // the card has a problem of its own and is not returned
	orphan bool                 // lines that do not belong to any card: they are skipped without more errors
	fields map[string]cardField // by key, only looked up
	last   string               // key that an indented line extends; "" for none, "?" for a field to skip
}

// cardField is the text of a field and the line where it starts.
type cardField struct {
	text string
	line int
}

// parseCards reads a file of test cards, `book/tests/chNN.tests`. A card is a
// header line, `[TNN.MM] name level: core`, followed by the field lines
// `kind`, `case`, `input` (optional), `expect`, `why` and `ref`; a line that
// starts with a space or a tab continues the field above it, and a blank line
// ends the card. `level` is `core` unless the header says `stretch`. The
// `kind` line also names the file: `kind: unit (go test)  file: path`. The ids
// have to be consecutive from 01 and their NN has to be the one in the name of
// the file.
//
// A card with a problem of its own (a bad id, a missing field, an unknown kind
// or level) is reported and left out of the result. A card whose only problem
// is its place in the sequence of ids (a gap, a repeated id, the NN of another
// chapter) is reported and returned. The errors are sorted by line.
func parseCards(file string, text []byte) ([]Card, []ScanError) {
	var cards []Card
	var errs []ScanError
	fail := func(line int, format string, args ...any) {
		errs = append(errs, ScanError{Path: file, Line: line, Message: fmt.Sprintf(format, args...)})
	}

	base := path.Base(file)
	fileNN := ""
	if m := cardFileRe.FindStringSubmatch(base); m != nil {
		fileNN = m[1]
	} else {
		fail(1, "card files are named chNN.tests, not `%s`", base)
	}

	var cur *cardBuilder
	expected := 1 // the number the next card has to carry
	finish := func() {
		if cur != nil && !cur.orphan {
			if card, ok := finishCard(cur, fail); ok {
				cards = append(cards, card)
			}
		}
		cur = nil
	}
	outside := func(line int) {
		fail(line, "line outside a card")
		cur = &cardBuilder{orphan: true}
	}

	for i, line := range splitLines(text) {
		n := i + 1
		switch {
		case strings.TrimSpace(line) == "":
			finish()

		case strings.HasPrefix(line, "["):
			finish()
			cur = &cardBuilder{fields: map[string]cardField{}}
			cur.card.Line = n
			readHeader(cur, line, n, fail)
			if cur.orphan {
				// A header that is not a header still takes its place in the
				// sequence, so the card after it is not reported as a gap.
				expected++
				break
			}
			expected = checkCardID(cur, fileNN, expected, n, fail)

		case line[0] == ' ' || line[0] == '\t':
			switch {
			case cur == nil:
				outside(n)
			case cur.orphan || cur.last == "?":
			case cur.last == "":
				fail(n, "card %s: indented line with no field above it", cur.label)
				cur.bad = true
			default:
				f := cur.fields[cur.last]
				if f.text == "" {
					f.text = strings.TrimSpace(line)
				} else {
					f.text += " " + strings.TrimSpace(line)
				}
				cur.fields[cur.last] = f
			}

		default:
			m := cardFieldRe.FindStringSubmatch(line)
			switch {
			case cur == nil:
				outside(n)
			case cur.orphan:
			case m == nil:
				fail(n, "card %s: unexpected line", cur.label)
				cur.bad = true
				cur.last = "?"
			case !cardKeys[m[1]]:
				fail(n, "card %s: unknown field `%s:`", cur.label, m[1])
				cur.bad = true
				cur.last = "?"
			default:
				if _, dup := cur.fields[m[1]]; dup {
					fail(n, "card %s: field `%s:` appears twice", cur.label, m[1])
					cur.bad = true
					cur.last = "?"
					break
				}
				cur.fields[m[1]] = cardField{text: strings.TrimSpace(m[2]), line: n}
				cur.last = m[1]
			}
		}
	}
	finish()
	sortScanErrors(errs)
	return cards, errs
}

// readHeader reads the header line of a card into b: its id, its name and its
// level. When the line is not a header at all, it makes b an orphan, so that
// the lines of that card do not report more errors.
func readHeader(b *cardBuilder, line string, n int, fail func(int, string, ...any)) {
	m := cardHeaderRe.FindStringSubmatch(line)
	if m == nil {
		fail(n, "malformed card header: write it as `[TNN.MM] name level: core`")
		b.orphan = true
		return
	}
	b.label = m[1]
	b.card.Level = "core"
	if !cardIDRe.MatchString(m[1]) {
		fail(n, "invalid card id `%s`: %s", m[1], cardIDHelp)
		b.bad = true
		return
	}
	b.card.ID = m[1]

	name, rest, _ := cutWord(strings.TrimSpace(m[2]))
	if name == "" {
		fail(n, "card %s: the header needs a name after the id", b.card.ID)
		b.bad = true
		return
	}
	b.card.Name = name
	if rest == "" {
		return
	}
	level, ok := strings.CutPrefix(rest, "level:")
	if !ok {
		fail(n, "card %s: after the name, the header can only say `level: core` or `level: stretch`", b.card.ID)
		b.bad = true
		return
	}
	level = strings.TrimSpace(level)
	if level != "core" && level != "stretch" {
		fail(n, "card %s: unknown level `%s`: want core or stretch", b.card.ID, level)
		b.bad = true
		return
	}
	b.card.Level = level
}

// checkCardID checks the id of the card in b against the file and against the
// sequence, and returns the number the next card has to carry. A card with a
// malformed id still takes its place in the count, so one bad id is one error
// and not a gap after it.
func checkCardID(b *cardBuilder, fileNN string, expected, n int, fail func(int, string, ...any)) int {
	m := cardIDRe.FindStringSubmatch(b.card.ID)
	if m == nil {
		return expected + 1
	}
	if fileNN != "" && m[1] != fileNN {
		fail(n, "card %s: its number must be %s, like the file `ch%s.tests`", b.card.ID, fileNN, fileNN)
		return expected + 1
	}
	found, _ := strconv.Atoi(m[2]) // cardIDRe allows only two or three digits
	if found != expected {
		fail(n, "card ids must be consecutive: expected T%s.%02d, found %s", m[1], expected, b.card.ID)
	}
	return found + 1
}

// finishCard checks that a card read from the file has every required field
// and fills in the card. It reports the problems through fail and returns the
// card with ok true only if the card has no problem of its own.
func finishCard(b *cardBuilder, fail func(int, string, ...any)) (card Card, ok bool) {
	card = b.card
	// text returns a required field; an absent field and an empty one are the same problem.
	text := func(key string) string {
		f := b.fields[key]
		if f.text == "" {
			fail(card.Line, "card %s: missing field `%s:`", b.label, key)
			b.bad = true
		}
		return f.text
	}

	if kindField := b.fields["kind"]; text("kind") != "" {
		kind, file, msg := parseKindLine(kindField.text)
		if msg != "" {
			fail(kindField.line, "card %s: %s", b.label, msg)
			b.bad = true
		}
		card.Kind, card.File = kind, file
	}
	card.Case = text("case")
	card.Input = b.fields["input"].text
	card.Expect = text("expect")
	card.Why = text("why")
	card.Ref = text("ref")
	return card, !b.bad
}

// parseKindLine reads the text of the `kind` field: the kind, an optional
// parenthesis with a note, and `file: PATH`. It returns the kind and the file,
// or the message of the problem.
func parseKindLine(text string) (kind, file, msg string) {
	end := strings.IndexAny(text, " \t(")
	if end < 0 {
		end = len(text)
	}
	kind, rest := text[:end], strings.TrimSpace(text[end:])
	if !cardKinds[kind] {
		return "", "", fmt.Sprintf("unknown kind `%s`: want unit, prop, example, bench, golden, diag or run", kind)
	}
	if strings.HasPrefix(rest, "(") {
		if rp := strings.IndexByte(rest, ')'); rp >= 0 {
			rest = strings.TrimSpace(rest[rp+1:])
		}
	}
	file, ok := strings.CutPrefix(rest, "file:")
	file = strings.TrimSpace(file)
	if !ok || file == "" {
		return kind, "", "the kind line needs `file: PATH` after the kind"
	}
	if strings.ContainsAny(file, " \t") {
		return kind, "", "the file must be a single path without spaces"
	}
	return kind, file, ""
}
