// Package base holds the types every pass of the compiler shares. It starts
// with source files and the spans that point into them.
package base

// FileID names a file inside a SourceManager. It is the index of the file in
// the manager, and later it will index an IdVec.
type FileID uint32

// Span is a half-open byte range [Lo, Hi) inside one file. Lo == Hi is an empty
// span, and Hi may equal the size of the file (the position just after the last
// byte).
type Span struct {
	File   FileID
	Lo, Hi uint32
}

// file is one source file the compiler has read.
type file struct {
	path     string   // the path shown in diagnostics, as the user wrote it
	contents []byte   // the bytes of the file, owned by the SourceManager
	lines    []uint32 // byte offset of the start of each line; built lazily on the first line query
	hadBOM   bool     // the file started with a UTF-8 byte order mark
}

// SourceManager owns the text of every file the compiler has read.
//
// It keeps pointers for now, so a *file never moves when files grows. A later
// session replaces the slice with an IdVec[FileID, file] once typed ids make
// pointer stability unnecessary.
type SourceManager struct {
	files []*file
}

// Add registers a file and returns its id. The manager takes ownership of
// contents: the caller must not modify the slice afterwards. It keeps the slice
// with its capacity clipped to its length, so spare capacity at the end of the
// caller's array (a buffer read from disk usually has some) is never reachable
// through Contents or Text.
func (m *SourceManager) Add(displayPath string, contents []byte) FileID {
	contents = contents[:len(contents):len(contents)]
	m.files = append(m.files, &file{path: displayPath, contents: contents})
	return FileID(len(m.files) - 1)
}

// DisplayPath returns the path given to Add, which is what diagnostics show.
func (m *SourceManager) DisplayPath(id FileID) string {
	return m.files[id].path
}

// Contents returns the bytes of the file. The slice has cap == len (Add clipped
// it), so an append by the caller reallocates and can never write into the
// file's array. Elements are not copied, so the caller must not assign to them.
func (m *SourceManager) Contents(id FileID) []byte {
	return m.files[id].contents
}

// Text returns the bytes of the span as a string. It panics if the span does
// not lie inside its file (Hi greater than the size of the file), because a
// span built by the compiler always does. The panic is the runtime's slice
// bounds error. For a slice expression s[lo:hi] the runtime checks hi against
// cap(s), not against len(s); Text panics past the end of the file only
// because Add clipped the capacity of the stored slice to its length, so the
// spare room of the array behind it is out of reach.
func (m *SourceManager) Text(sp Span) string {
	return string(m.files[sp.File].contents[sp.Lo:sp.Hi])
}

// Size returns the length of the file in bytes.
func (m *SourceManager) Size(id FileID) uint32 {
	return uint32(len(m.files[id].contents))
}
