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

// A line of at most maxShownBytes bytes is shown whole (for the lines of a
// label; the two lines of a fix-it pair are cut together, see RenderHuman). A
// longer one (a minified file, or a binary renamed to .ki, can have a single
// line of megabytes) is shown as a window of at most maxShownBytes bytes plus
// the rest of the character the cut falls in (up to three bytes), with "..."
// at each end that was cut, so the size of a diagnostic does not depend on the
// size of the file. The window is built around the labels of the line that
// RenderHuman names or, in a fix-it pair, around its fix-its, and starts
// contextBytes bytes before the first of them.
const (
	maxShownBytes = 200
	contextBytes  = 40
	// roomForLabels is how many bytes the labels that a window is built around
	// may cover together, from the start of the first to the end of the last:
	// what is left of maxShownBytes once contextBytes of text are kept on each
	// side. A label that would make them cover more does not move the window.
	roomForLabels = maxShownBytes - 2*contextBytes
)

// cutMark stands for the part of a long line that is not shown.
const cutMark = "..."

// RenderHuman writes d the way a person reads it: the severity, code and
// message, then each line that has a label with marks under it, then the notes
// and the help, and, if d has fix-its, the same lines of the file as they are
// and as they would be with the fix-its applied. The result ends with a newline
// and has no color; color arrives with ki explain.
//
// A line longer than maxShownBytes is shown as a window (at most
// maxShownBytes bytes plus the rest of a character), with "..." where text was
// cut. The window is centred on the primary label of the line, or on the first
// label of a line that does not have it, with contextBytes bytes of text on
// each side, so a primary label that fits in roomForLabels bytes is shown whole
// and with its own mark, wherever the other labels are. It is widened to the
// other labels of the line, in the order of their position, one at a time,
// while all the labels it is built around fit in roomForLabels bytes. A
// label that does not move the window is drawn as far as the window goes: one
// that reaches past the edge up to the edge, and one that is wholly outside as
// a single mark at the edge nearest to it, under the "..." there.
//
// The fix-its follow the help line, after a line with only the margin and a
// bar. For each line of the file that a fix-it touches, in increasing order, two
// lines come: the line number, " - " and the line as it is, and the line number,
// " + " and the line with the fix-its of that line applied. They are drawn as
// the code lines are: the same display rules, with no trailing spaces, and a
// window around the fix-its, in place of the labels, if the line is long. The
// two lines of a pair are cut together: if either is longer than maxShownBytes
// both are windowed, so that an edit that takes a line across the limit does
// not leave one whole beside the other cut, with text that the first shows
// seeming to vanish in the second. Each edit works on the offsets of the
// original line, so it does not matter that an earlier edit made the line
// longer or shorter. The width of the line numbers is that of the largest one
// shown anywhere in the diagnostic, the lines of the fix-its included.
//
// RenderHuman panics, because the pass that built d is wrong and not the user's
// program, if a label or a fix-it is in another file than the primary label, if
// d has fix-its and no help line (a fix-it is only ever offered together with
// the sentence that says what it does), if a fix-it covers a line break, or if
// two fix-its of a line overlap.
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

	edits := groupFixits(d, sm)

	// g is the width of the largest line number that will be shown, in the
	// code lines and in the fix-it lines alike.
	maxLine := all[len(all)-1].line
	if n := len(edits); n > 0 {
		maxLine = max(maxLine, edits[n-1].line)
	}
	g := len(strconv.Itoa(int(maxLine)))
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
		for n, l := range group {
			lo := int(min(l.label.Span.Lo-start, uint32(len(text))))
			hi := int(min(max(l.label.Span.Hi-start, uint32(lo)), uint32(len(text))))
			extents[n] = extent{lo, hi}
		}
		// The window is built around the first label of the line, which is
		// the primary one if the line has it (the sort above), and around each
		// later label that fits with the ones already taken. Taking every
		// label would anchor the window on the leftmost one, and a secondary
		// label far to the left of the primary would push the primary mark to
		// the edge of the window.
		first, last := extents[0].lo, extents[0].hi
		for _, e := range extents[1:] {
			if lo, hi := min(first, e.lo), max(last, e.hi); hi-lo <= roomForLabels {
				first, last = lo, hi
			}
		}
		w := showWindow(text, first, last, fitsWhole(text))
		writeNumbered(&sb, g, line, "|", w.shown)

		for n, l := range group {
			// A label outside the window is marked at the nearest edge of it.
			lo := min(max(extents[n].lo, w.from), w.to) - w.from
			hi := min(max(extents[n].hi, w.from), w.to) - w.from
			width := max(w.cols[hi]-w.cols[lo], 1)
			col := w.margin + w.cols[lo]
			if extents[n].lo < w.from && extents[n].hi <= w.from {
				// Wholly before the window: under the last character of the
				// "..." that stands for it, as a label past the end is under
				// the first character of the "..." there.
				col = w.margin - 1
			}
			mark := "-"
			if l.primary {
				mark = "^"
			}
			sb.WriteString(pad + " | " + strings.Repeat(" ", col) + strings.Repeat(mark, width))
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
	if len(edits) > 0 {
		sb.WriteString(pad + " |\n")
	}
	for _, le := range edits {
		text := sm.LineText(file, le.line)
		start := sm.LineStart(file, le.line)
		first := int(le.fixits[0].Span.Lo - start)
		last := int(le.fixits[len(le.fixits)-1].Span.Hi - start)
		patched, patchedLast := applyFixits(text, start, le.fixits)
		// One decision for the pair (see above): each line asking for itself
		// would show the old line whole and cut the new one, or the other way
		// round, at the limit.
		whole := fitsWhole(text) && fitsWhole(patched)
		old := showWindow(text, first, last, whole)
		writeNumbered(&sb, g, le.line, "-", old.shown)
		now := showWindow(patched, first, patchedLast, whole)
		writeNumbered(&sb, g, le.line, "+", now.shown)
	}
	return sb.String()
}

// window is the part of a source line that is shown, ready to print.
type window struct {
	// shown is the text to print for the line, with the cut marks and without
	// trailing spaces if the line ends in the window.
	shown string
	// cols[i] is the number of columns shown before byte from+i of the line,
	// and cols[to-from] is the width of the window (not counting the margin).
	cols []int
	// from and to are the offsets in the line of the first byte shown and of
	// the one after the last.
	from, to int
	// margin is the number of columns shown before the first byte of the
	// window: the cut mark, if text was cut at the start.
	margin int
}

// fitsWhole reports whether a source line is short enough to be shown whole.
func fitsWhole(text string) bool { return len(text) <= maxShownBytes }

// showWindow returns what is shown of text, a source line, given that the
// window is built around the labels or fix-its that cover the bytes [first,
// last). If whole is true the line is shown whole; otherwise see lineWindow for
// which part is shown. A line that fits is cut too when whole is false: the
// caller decides, because the old and the new line of a fix-it are cut
// together.
func showWindow(text string, first, last int, whole bool) window {
	from, to := lineWindow(text, first, last, whole)
	shown, cols := displayLine(text[from:to])
	margin := 0
	if from > 0 {
		shown, margin = cutMark+shown, len(cutMark)
	}
	if to < len(text) {
		shown += cutMark
	} else {
		// Trailing spaces and tabs are invisible. cols still counts them.
		shown = strings.TrimRight(shown, " ")
	}
	return window{shown: shown, cols: cols, from: from, to: to, margin: margin}
}

// writeNumbered writes one line that starts with the line number, right
// aligned in g columns, and the mark that says what the line is: "|" for a
// line of code, "-" for the old line of a fix-it and "+" for the new one. The
// text follows after a space, unless there is none, so that an empty line has
// no trailing space.
func writeNumbered(sb *strings.Builder, g int, line uint32, mark, text string) {
	num := strconv.Itoa(int(line))
	sb.WriteString(strings.Repeat(" ", g-len(num)) + num + " " + mark)
	if text != "" {
		sb.WriteString(" " + text)
	}
	sb.WriteString("\n")
}

// lineEdits are the fix-its of d that fall on one line of the file, by the
// offset where they start. Of two that start at the same offset the later one
// in d.Fixits comes first (see groupFixits).
type lineEdits struct {
	line   uint32
	fixits []FixIt
}

// groupFixits checks the fix-its of d and returns them grouped by line, the
// lines in increasing order and on each line the fix-its in the order of their
// position. Of those at one position the later one in d.Fixits comes first,
// which is what applying them from the right gives (see FixIt). It panics for
// the four reasons RenderHuman lists.
func groupFixits(d Diagnostic, sm *base.SourceManager) []lineEdits {
	if len(d.Fixits) == 0 {
		return nil
	}
	if d.Help == "" {
		panic("diag: fix-its without a help line")
	}
	file := d.Primary.Span.File
	type placed struct {
		line  uint32
		index int // position in d.Fixits
		fix   FixIt
	}
	all := make([]placed, 0, len(d.Fixits))
	for i, f := range d.Fixits {
		if f.Span.File != file {
			panic(fmt.Sprintf("diag: fix-it in file %d, primary in file %d", f.Span.File, file))
		}
		line := sm.LineCol(file, f.Span.Lo).Line
		// The line break is not part of the line: the text of the line ends
		// before the "\n" and before the "\r" of a "\r\n". A fix-it that goes
		// past that would replace the break.
		if f.Span.Hi > sm.LineStart(file, line)+uint32(len(sm.LineText(file, line))) {
			panic("diag: fix-it spans a line break")
		}
		all = append(all, placed{line, i, f})
	}
	slices.SortFunc(all, func(a, b placed) int {
		if a.line != b.line {
			return int(a.line) - int(b.line)
		}
		if a.fix.Span.Lo != b.fix.Span.Lo {
			return int(a.fix.Span.Lo) - int(b.fix.Span.Lo)
		}
		return b.index - a.index
	})
	var out []lineEdits
	for i, p := range all {
		if i > 0 && all[i-1].line == p.line {
			if prev := all[i-1].fix; p.fix.Span.Lo < prev.Span.Hi {
				panic("diag: overlapping fix-its")
			}
			out[len(out)-1].fixits = append(out[len(out)-1].fixits, p.fix)
			continue
		}
		out = append(out, lineEdits{line: p.line, fixits: []FixIt{p.fix}})
	}
	return out
}

// applyFixits returns text, the line of the file that starts at offset start,
// with fixits applied, and where the last replacement ends in the result. The
// fix-its are sorted by position, those at one position with the later one of
// d.Fixits first, and do not overlap (see groupFixits). Each one is placed by
// its offsets in the original line, so an edit never moves another: this is the
// same as applying them from the right.
func applyFixits(text string, start uint32, fixits []FixIt) (patched string, lastEnd int) {
	var sb strings.Builder
	at := 0 // how much of text has been copied
	for _, f := range fixits {
		lo, hi := int(f.Span.Lo-start), int(f.Span.Hi-start)
		sb.WriteString(text[at:lo])
		sb.WriteString(f.Replacement)
		lastEnd = sb.Len()
		at = hi
	}
	sb.WriteString(text[at:])
	return sb.String(), lastEnd
}

// lineWindow returns the part [from, to) of a source line that is shown, given
// that the window is built around the bytes [first, last): the ones that the
// labels RenderHuman takes cover, or the fix-its of a pair. If whole is true
// that is the line itself, and callers pass fitsWhole(text) unless the line is
// shown together with another one. Otherwise the window starts contextBytes
// before first and ends contextBytes after last, but is not longer than
// maxShownBytes bytes plus the rest of the character its end falls in: both
// ends move forward to the start of a character, so a character is not cut in
// two, and the end can move after it has been capped (a window of 201 to 203
// bytes).
func lineWindow(text string, first, last int, whole bool) (from, to int) {
	if whole {
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
	Fixits   []jsonFixit `json:"fixits"`
}

// jsonFixit is one fix-it in the JSON output: where the text to replace starts
// and ends (the end is exclusive, so an insertion has the same start and end)
// and what goes there.
type jsonFixit struct {
	Line        uint32 `json:"line"`
	Col         uint32 `json:"col"`
	EndLine     uint32 `json:"end_line"`
	EndCol      uint32 `json:"end_col"`
	Replacement string `json:"replacement"`
}

// RenderJSON writes d as one line of JSON for tools to read: the keys are
// severity, code, message, path, line, col, labels (the primary one first),
// notes (an empty array if there are none), help (null if there is none) and
// fixits (an empty array if there are none; each has line, col, end_line,
// end_col and replacement, with positions as for the labels and the end
// exclusive; the array has the order of d.Fixits, and a tool that applies the
// edits writes the later of two at one offset first, as FixIt says). The result
// ends with a newline.
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
		Fixits:   make([]jsonFixit, 0, len(d.Fixits)),
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
	for _, f := range d.Fixits {
		from, to := sm.LineCol(f.Span.File, f.Span.Lo), sm.LineCol(f.Span.File, f.Span.Hi)
		out.Fixits = append(out.Fixits, jsonFixit{
			Line: from.Line, Col: from.Col, EndLine: to.Line, EndCol: to.Col, Replacement: f.Replacement,
		})
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
