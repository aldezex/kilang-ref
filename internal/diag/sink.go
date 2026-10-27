package diag

import "slices"

// MaxErrors is where the front end stops reporting (spec 7.3). After this many
// errors, one more is dropped and the Sink remembers that it was truncated.
const MaxErrors = 100

// Sink collects the diagnostics that the passes report. The zero value is an
// empty sink ready to use. A Sink is not safe for concurrent use: the compiler
// is sequential.
type Sink struct {
	diags     []Diagnostic
	errors    int // how many of diags are errors, so Report need not recount
	truncated bool
}

// Report adds d to the sink.
//
// Once the sink holds MaxErrors errors, the next error is dropped and marks the
// sink as truncated. From then on every diagnostic is dropped, warnings
// included: by then the output is too long to read, and what comes next is
// probably a cascade of the first mistakes.
func (s *Sink) Report(d Diagnostic) {
	if s.truncated {
		return
	}
	if d.Severity == Error {
		if s.errors == MaxErrors {
			s.truncated = true
			return
		}
		s.errors++
	}
	s.diags = append(s.diags, d)
}

// All returns the diagnostics kept so far, in report order (or in position
// order after SortByPosition). The caller must not modify the slice, and it is
// only valid until the next call to Report or SortByPosition.
func (s *Sink) All() []Diagnostic { return s.diags }

// ErrorCount returns how many of the kept diagnostics are errors.
func (s *Sink) ErrorCount() int { return s.errors }

// HasErrors reports whether at least one error was kept.
func (s *Sink) HasErrors() bool { return s.errors > 0 }

// Count returns how many kept diagnostics have the given code.
func (s *Sink) Count(code Code) int {
	n := 0
	for _, d := range s.diags {
		if d.Code == code {
			n++
		}
	}
	return n
}

// Truncated reports whether an error was dropped because the sink already held
// MaxErrors errors. Exactly MaxErrors errors do not truncate.
func (s *Sink) Truncated() bool { return s.truncated }

// SortByPosition orders the kept diagnostics by the start of their primary
// span: first by file, then by byte offset. Diagnostics at the same position
// keep the order in which they were reported, so the output is deterministic.
func (s *Sink) SortByPosition() {
	slices.SortStableFunc(s.diags, func(a, b Diagnostic) int {
		if a.Primary.Span.File != b.Primary.Span.File {
			if a.Primary.Span.File < b.Primary.Span.File {
				return -1
			}
			return 1
		}
		switch {
		case a.Primary.Span.Lo < b.Primary.Span.Lo:
			return -1
		case a.Primary.Span.Lo > b.Primary.Span.Lo:
			return 1
		}
		return 0
	})
}
