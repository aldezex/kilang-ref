package diag

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/aldezex/kilang/internal/base"
)

// tabWidth is the distance between tab stops when a source line is shown.
const tabWidth = 4

// A line of at most maxShownBytes bytes is shown whole. A longer one (a
// minified file, or a binary renamed to .ki, can have a single line of
// megabytes) is shown as a window of at most maxShownBytes bytes plus the rest
// of the character the cut falls in (up to three bytes), around its labels,
// with "..." at each end that was cut, so the size of a diagnostic does not
// depend on the size of the file. The window starts contextBytes bytes before
// the first label.
const (
	maxShownBytes = 200
	contextBytes  = 40
)

// cutMark stands for the part of a long line that is not shown.
const cutMark = "..."

// RenderHuman writes d the way a person reads it: the severity, code and
// message, then each line that has a label with marks under it, then the notes
// and the help. The result ends with a newline and has no color; color arrives
// with ki explain.
//
// A line longer than maxShownBytes is shown as a window around its labels (at
// most maxShownBytes bytes plus the rest of a character), with "..." where
// text was cut; a label that does not fit in the window is marked up to its
// edge.
//
// Every label of d must be in the file of the primary label. A label in
// another file would show text of the wrong file, so it panics instead.
func RenderHuman(d Diagnostic, sm *base.SourceManager) string {
	file := d.Primary.Span.File
	type placed struct {
		label   Label
		primary bool
		line    uint32
	}
	all := make([]placed, 0, 1+len(d.Secondary))
	all = append(all, placed{d.Primary, true, sm.LineCol(file, d.Primary.Span.Lo).Line})
	for _, l := range d.Secondary {
		if l.Span.File != file {
			panic(fmt.Sprintf("diag: secondary label in file %d, primary in file %d", l.Span.File, file))
		}
		all = append(all, placed{l, false, sm.LineCol(file, l.Span.Lo).Line})
	}
	// By line; on one line the primary label first, then the others by
	// column. The sort is stable, so labels at the same offset keep the order
	// in which the pass added them.
	slices.SortStableFunc(all, func(a, b placed) int {
		if a.line != b.line {
			return int(a.line) - int(b.line)
		}
		if a.primary != b.primary {
			if a.primary {
				return -1
			}
			return 1
		}
		if a.primary {
			return 0
		}
		return int(a.label.Span.Lo) - int(b.label.Span.Lo)
	})

	// g is the width of the largest line number that will be shown.
	g := len(strconv.Itoa(int(all[len(all)-1].line)))
	pad := strings.Repeat(" ", g)

	var sb strings.Builder
	pos := sm.LineCol(file, d.Primary.Span.Lo)
	fmt.Fprintf(&sb, "%s[%s]: %s\n", d.Severity, d.Code, d.Message)
	fmt.Fprintf(&sb, "%s--> %s:%d:%d\n", pad, sm.DisplayPath(file), pos.Line, pos.Col)
	sb.WriteString(pad + " |\n")

	var prev uint32 // the last line shown; 0 before the first
	for i := 0; i < len(all); {
		line := all[i].line
		if prev != 0 && line != prev+1 {
			sb.WriteString(pad + " ...\n")
		}
		prev = line

		text := sm.LineText(file, line)
		start := sm.LineStart(file, line)
		group := all[i:]
		for n := range group {
			if group[n].line != line {
				group = group[:n]
				break
			}
		}
		i += len(group)

		// The part of each span on this line, as offsets into text. A span
		// that runs over several lines is cut to this one, so a label is
		// marked up to the end of the first line it is on.
		type extent struct{ lo, hi int }
		extents := make([]extent, len(group))
		first, last := len(text), 0
		for n, l := range group {
			lo := int(min(l.label.Span.Lo-start, uint32(len(text))))
			hi := int(min(max(l.label.Span.Hi-start, uint32(lo)), uint32(len(text))))
			extents[n] = extent{lo, hi}
			first, last = min(first, lo), max(last, hi)
		}
		from, to := lineWindow(text, first, last)

		shown, cols := displayLine(text[from:to])
		margin := 0 // columns shown before the first byte of the window
		if from > 0 {
			shown, margin = cutMark+shown, len(cutMark)
		}
		if to < len(text) {
			shown += cutMark
		} else {
			// Trailing spaces and tabs are invisible. cols still counts them.
			shown = strings.TrimRight(shown, " ")
		}
		num := strconv.Itoa(int(line))
		sb.WriteString(strings.Repeat(" ", g-len(num)) + num + " |")
		if shown != "" {
			sb.WriteString(" " + shown)
		}
		sb.WriteString("\n")

		for n, l := range group {
			// A label outside the window is marked at the nearest edge of it.
			lo := min(max(extents[n].lo, from), to) - from
			hi := min(max(extents[n].hi, from), to) - from
			width := max(cols[hi]-cols[lo], 1)
			mark := "-"
			if l.primary {
				mark = "^"
			}
			sb.WriteString(pad + " | " + strings.Repeat(" ", margin+cols[lo]) + strings.Repeat(mark, width))
			if l.label.Message != "" {
				sb.WriteString(" " + l.label.Message)
			}
			sb.WriteString("\n")
		}
	}

	for _, n := range d.Notes {
		sb.WriteString(pad + " = note: " + n + "\n")
	}
	if d.Help != "" {
		sb.WriteString(pad + " = help: " + d.Help + "\n")
	}
	return sb.String()
}

// lineWindow returns the part [from, to) of a source line that is shown, given
// that the labels of the line cover the bytes [first, last). A line of at most
// maxShownBytes bytes is shown whole. For a longer one the window starts
// contextBytes before first and ends contextBytes after last, but is not
// longer than maxShownBytes bytes plus the rest of the character its end falls
// in: both ends move forward to the start of a character, so a character is not
// cut in two, and the end can move after it has been capped (a window of 201 to
// 203 bytes).
func lineWindow(text string, first, last int) (from, to int) {
	if len(text) <= maxShownBytes {
		return 0, len(text)
	}
	from = max(first-contextBytes, 0)
	to = min(last+contextBytes, from+maxShownBytes, len(text))
	// utf8.RuneStart is false for the (at most three) continuation bytes that
	// follow the start of a character, but also for any stray continuation
	// byte with no lead byte, and a line can be a run of millions of them. So
	// each end moves at most utf8.UTFMax-1 bytes: after a valid character that
	// is enough to reach the next start, and a stray byte is left where it is,
	// since displayLine shows each invalid byte on its own.
	for k := 0; k < utf8.UTFMax-1 && from < len(text) && !utf8.RuneStart(text[from]); k++ {
		from++
	}
	for k := 0; k < utf8.UTFMax-1 && to < len(text) && !utf8.RuneStart(text[to]); k++ {
		to++
	}
	return from, max(to, from)
}

// displayLine turns the text of a source line into the text that is printed
// for it, and says where each byte of the original lands.
//
// Printing must not change the width of what comes before a mark, so each
// character takes a known number of columns: a tab fills up to the next
// multiple of tabWidth; an invalid byte, a control character, a C1 or
// bidirectional control (see isHiddenControl) and the DEL are shown as a
// visible stand-in; and every other character is one column. That is one
// column per code point, which is not the width of a character on the screen:
// zero-width characters (U+200B, U+2060, U+FEFF) are printed as they are and
// wide ones (CJK, emoji) take two columns on a terminal, so a mark after them
// can drift. cols has one entry per byte plus one: cols[i] is the number of
// columns shown before byte i, and cols[len(text)] is the width of the whole
// line. The returned text keeps its trailing spaces.
func displayLine(text string) (shown string, cols []int) {
	var sb strings.Builder
	cols = make([]int, len(text)+1)
	width := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		for k := range size {
			cols[i+k] = width
		}
		i += size
		switch {
		case r == utf8.RuneError && size == 1:
			// One U+FFFD per bad byte: DecodeRune moves one byte on an
			// invalid sequence, which is what keeps the columns honest.
			sb.WriteRune('\uFFFD')
			width++
		case r == '\t':
			n := tabWidth - width%tabWidth
			sb.WriteString(strings.Repeat(" ", n))
			width += n
		case r < 0x20:
			// The control picture block starts at U+2400 with the same order
			// as ASCII, so U+2400+b is the picture of byte b.
			sb.WriteRune(0x2400 + r)
			width++
		case r == 0x7F:
			sb.WriteRune(0x2421)
			width++
		case isHiddenControl(r):
			s := fmt.Sprintf("<U+%04X>", r)
			sb.WriteString(s)
			width += len(s)
		default:
			sb.WriteRune(r)
			width++
		}
	}
	cols[len(text)] = width
	return sb.String(), cols
}

// isHiddenControl reports whether r is a C1 control or a bidirectional
// control. They are invisible or reorder the text around them, so a diagnostic
// that printed them as they are could show something other than the file.
func isHiddenControl(r rune) bool {
	switch {
	case r >= 0x80 && r <= 0x9F: // C1 controls
		return true
	case r == 0x061C, r == 0x200E, r == 0x200F: // Arabic letter mark, LRM, RLM
		return true
	case r >= 0x202A && r <= 0x202E: // embeddings and overrides
		return true
	case r >= 0x2066 && r <= 0x2069: // isolates
		return true
	}
	return false
}

// RenderShort writes d on one line, "path:line:col: severity[code]: message",
// the way editors and grep-like tools expect it. Labels, notes and help are
// not part of it. The result ends with a newline.
func RenderShort(d Diagnostic, sm *base.SourceManager) string {
	file := d.Primary.Span.File
	pos := sm.LineCol(file, d.Primary.Span.Lo)
	return fmt.Sprintf("%s:%d:%d: %s[%s]: %s\n", sm.DisplayPath(file), pos.Line, pos.Col, d.Severity, d.Code, d.Message)
}

// jsonLabel and jsonDiagnostic fix the keys of the JSON output and, because
// encoding/json writes struct fields in declaration order, their order too.
type jsonLabel struct {
	Primary bool   `json:"primary"`
	Line    uint32 `json:"line"`
	Col     uint32 `json:"col"`
	Message string `json:"message"`
}

type jsonDiagnostic struct {
	Severity string      `json:"severity"`
	Code     string      `json:"code"`
	Message  string      `json:"message"`
	Path     string      `json:"path"`
	Line     uint32      `json:"line"`
	Col      uint32      `json:"col"`
	Labels   []jsonLabel `json:"labels"`
	Notes    []string    `json:"notes"`
	Help     *string     `json:"help"`
}

// RenderJSON writes d as one line of JSON for tools to read: the keys are
// severity, code, message, path, line, col, labels (the primary one first),
// notes (an empty array if there are none) and help (null if there is none).
// The result ends with a newline.
func RenderJSON(d Diagnostic, sm *base.SourceManager) string {
	file := d.Primary.Span.File
	pos := sm.LineCol(file, d.Primary.Span.Lo)
	out := jsonDiagnostic{
		Severity: d.Severity.String(),
		Code:     d.Code.String(),
		Message:  d.Message,
		Path:     sm.DisplayPath(file),
		Line:     pos.Line,
		Col:      pos.Col,
		Labels:   make([]jsonLabel, 0, 1+len(d.Secondary)),
		Notes:    []string{},
	}
	add := func(l Label, primary bool) {
		p := sm.LineCol(l.Span.File, l.Span.Lo)
		out.Labels = append(out.Labels, jsonLabel{Primary: primary, Line: p.Line, Col: p.Col, Message: l.Message})
	}
	add(d.Primary, true)
	for _, l := range d.Secondary {
		add(l, false)
	}
	out.Notes = append(out.Notes, d.Notes...)
	if d.Help != "" {
		out.Help = &d.Help
	}
	// Encode, unlike Marshal, can be told not to write <, > and & as \u003c,
	// \u003e and \u0026 (a protection for JSON inside HTML that does not apply
	// here), which would make `Vec<Int>` unreadable in a message. It ends the
	// value with a newline.
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		// Strings, numbers and bools always encode; this is a bug in the
		// compiler, not a problem with the user's program.
		panic("diag: encoding a diagnostic: " + err.Error())
	}
	return sb.String()
}
