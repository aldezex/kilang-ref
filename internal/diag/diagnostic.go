// Package diag defines diagnostics: the errors and warnings that compiler
// passes report about the user's program. A diagnostic is a plain value that a
// pass hands to a Sink; nothing here prints anything.
package diag

import (
	"fmt"
	"slices"

	"github.com/aldezex/kilang/internal/base"
)

// Severity says how serious a diagnostic is.
type Severity uint8

const (
	// Error means the program is wrong and cannot go on to the next stage.
	Error Severity = iota
	// Warning means the program is accepted but probably not what was meant.
	Warning
	// Note is extra information. Passes use the Notes field of a Diagnostic
	// for most of it; this severity is for a note that stands on its own.
	Note
)

// String returns "error", "warning" or "note". Any other value, which only a
// bug can produce, is shown as "Severity(n)" so that it is easy to spot.
func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	case Note:
		return "note"
	}
	return fmt.Sprintf("Severity(%d)", uint8(s))
}

// Code is the stable identifier of a kind of diagnostic: a letter ('E' for an
// error, 'W' for a warning) and a number whose hundreds say which stage found
// it (E01xx lexer, E02xx parser, and so on). A code is never reused, so users
// can search for it.
type Code struct {
	Letter byte
	Number uint16
}

// E returns the error code with number n.
func E(n uint16) Code { return Code{Letter: 'E', Number: n} }

// W returns the warning code with number n.
func W(n uint16) Code { return Code{Letter: 'W', Number: n} }

// String writes the code with at least four digits: E(101) is "E0101".
func (c Code) String() string {
	return fmt.Sprintf("%c%04d", c.Letter, c.Number)
}

// Label points at a piece of source text and says something about it.
type Label struct {
	Span    base.Span
	Message string
}

// FixIt is a suggested edit: replace the text of Span with Replacement.
type FixIt struct {
	Span        base.Span
	Replacement string
}

// Diagnostic is one error, warning or note about the user's program.
//
// Rendering rules live in the renderers, not here: a Diagnostic only carries
// the facts. Use the With methods to add to it, which keeps a diagnostic that
// is built in one place from being changed by another.
type Diagnostic struct {
	Severity  Severity
	Code      Code
	Message   string
	Primary   Label
	Secondary []Label
	Notes     []string
	Help      string // empty if none
	Fixits    []FixIt
}

// NewError returns an error diagnostic with the given code, message and primary
// label. The other fields start empty.
func NewError(code Code, message string, primary Label) Diagnostic {
	return Diagnostic{Severity: Error, Code: code, Message: message, Primary: primary}
}

// WithSecondary returns a copy of d with l added after its secondary labels.
// d itself does not change.
func (d Diagnostic) WithSecondary(l Label) Diagnostic {
	// slices.Clip drops the spare capacity, so append must allocate a new
	// array instead of writing into one that d (or another copy) still uses.
	d.Secondary = append(slices.Clip(d.Secondary), l)
	return d
}

// WithNote returns a copy of d with n added after its notes. d itself does not
// change.
func (d Diagnostic) WithNote(n string) Diagnostic {
	d.Notes = append(slices.Clip(d.Notes), n)
	return d
}

// WithHelp returns a copy of d whose help line is h. A diagnostic has at most
// one help line, so it replaces any earlier one.
func (d Diagnostic) WithHelp(h string) Diagnostic {
	d.Help = h
	return d
}
