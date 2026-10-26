package base

import (
	"bytes"
	"fmt"
	"slices"
	"unicode/utf8"
)

// LineCol is a position in a file as a person reads it. Both fields are
// 1-based: the first character of a file is {Line: 1, Col: 1}.
type LineCol struct{ Line, Col uint32 }

// lineStarts returns the table of line starts of the file, building it on the
// first call. lines[i] is the byte offset where line i+1 begins, so lines[0]
// is always 0 and the table has one entry per '\n' plus one. The first call
// writes f.lines, so the line queries of a SourceManager are not safe for
// concurrent use; the compiler is sequential, so nothing shares it.
func (f *file) lineStarts() []uint32 {
	if f.lines != nil {
		return f.lines
	}
	lines := []uint32{0}
	rest := f.contents
	pos := 0
	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			break
		}
		pos += i + 1
		lines = append(lines, uint32(pos))
		rest = rest[i+1:]
	}
	f.lines = lines
	return lines
}

// LineCount returns the number of lines of the file: the number of '\n' plus
// one. An empty file has one line, and so does a file that ends in a newline
// (its last line is empty).
func (m *SourceManager) LineCount(id FileID) uint32 {
	return uint32(len(m.files[id].lineStarts()))
}

// LineStart returns the byte offset where the given line begins. Lines count
// from 1. It panics if line is 0 or greater than LineCount, because the
// compiler never builds such a line: that is a bug in the compiler, not an
// error in the user's program.
func (m *SourceManager) LineStart(id FileID, line uint32) uint32 {
	lines := m.files[id].lineStarts()
	if line == 0 || int64(line) > int64(len(lines)) {
		panic(fmt.Sprintf("base: line %d out of range (file has %d lines)", line, len(lines)))
	}
	return lines[line-1]
}

// LineCol converts a byte offset into a line and a column. The column counts
// code points: 1 plus the number of code points between the start of the line
// and the offset, where each byte of invalid UTF-8 counts as one. A "\r\n" is
// one line break; a lone '\r' is an ordinary character. An offset equal to the
// size of the file is valid (the position just after the last byte); a larger
// one panics, for the same reason as LineStart.
func (m *SourceManager) LineCol(id FileID, offset uint32) LineCol {
	f := m.files[id]
	if int64(offset) > int64(len(f.contents)) {
		panic(fmt.Sprintf("base: offset %d out of range (file has %d bytes)", offset, len(f.contents)))
	}
	lines := f.lineStarts()
	// BinarySearch returns the index of offset if some line starts exactly
	// there (line i+1 then). Otherwise it returns the index where offset would
	// be inserted, which is one past the line that contains it, and since
	// lines are 1-based that index is itself the line number.
	i, found := slices.BinarySearch(lines, offset)
	line := i
	if found {
		line = i + 1
	}
	col := 1 + utf8.RuneCount(f.contents[lines[line-1]:offset])
	return LineCol{Line: uint32(line), Col: uint32(col)}
}

// LineText returns the text of the given line without its line break: no '\n',
// and no '\r' right before that '\n'. A '\r' anywhere else stays, and so does
// one at the very end of a file that has no final '\n', because a lone '\r'
// does not end a line. It panics for a line that does not exist, like
// LineStart.
func (m *SourceManager) LineText(id FileID, line uint32) string {
	start := m.LineStart(id, line)
	f := m.files[id]
	lines := f.lineStarts()
	end := uint32(len(f.contents))
	if line < uint32(len(lines)) {
		// Not the last line: the next line starts right after our '\n'.
		end = lines[line] - 1
		if end > start && f.contents[end-1] == '\r' {
			end--
		}
	}
	return string(f.contents[start:end])
}
