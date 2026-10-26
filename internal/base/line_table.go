package base

import (
	"bytes"
	"fmt"
	"slices"
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

// LineCol converts a byte offset into a line and a column. For now the column
// counts bytes; a later session makes it count code points. An offset
// equal to the size of the file is valid (the position just after the last
// byte); a larger one panics, for the same reason as LineStart.
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
	return LineCol{Line: uint32(line), Col: offset - lines[line-1] + 1}
}
