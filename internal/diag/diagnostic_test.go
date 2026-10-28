package diag

import (
	"fmt"
	"slices"
	"testing"

	"github.com/aldezex/kilang/internal/base"
)

func TestCode(t *testing.T) {
	t.Run("formats_four_digits", func(t *testing.T) {
		if got := E(101).String(); got != "E0101" {
			t.Errorf("E(101).String() = %q, want %q", got, "E0101")
		}
		if got := W(702).String(); got != "W0702" {
			t.Errorf("W(702).String() = %q, want %q", got, "W0702")
		}
		// %v must pick up String too, because the renderers print codes with it.
		if got := fmt.Sprintf("%v", E(101)); got != "E0101" {
			t.Errorf("Sprintf(%%v, E(101)) = %q, want %q", got, "E0101")
		}
	})
}

func TestSeverity(t *testing.T) {
	t.Run("strings", func(t *testing.T) {
		rows := []struct {
			name string
			sev  Severity
			want string
		}{
			{"error", Error, "error"},
			{"warning", Warning, "warning"},
			{"note", Note, "note"},
			{"out of range", Severity(3), "Severity(3)"},
		}
		for _, row := range rows {
			if got := row.sev.String(); got != row.want {
				t.Errorf("%s: String() = %q, want %q", row.name, got, row.want)
			}
		}
	})
}

func TestDiagnostic(t *testing.T) {
	t.Run("with_methods_return_copies", func(t *testing.T) {
		primary := Label{Span: base.Span{File: 1, Lo: 4, Hi: 5}, Message: "here"}
		first := Label{Span: base.Span{File: 1, Lo: 0, Hi: 1}, Message: "first"}
		second := Label{Span: base.Span{File: 1, Lo: 8, Hi: 9}, Message: "second"}

		d := NewError(E(401), "mismatched types", primary)
		if d.Severity != Error || d.Code != E(401) || d.Message != "mismatched types" || d.Primary != primary {
			t.Fatalf("NewError = %+v, want an Error E0401 %q at %+v", d, "mismatched types", primary)
		}

		d2 := d.WithNote("n")
		if len(d.Notes) != 0 {
			t.Errorf("WithNote changed the receiver: Notes = %q", d.Notes)
		}
		if !slices.Equal(d2.Notes, []string{"n"}) {
			t.Errorf("WithNote copy: Notes = %q, want [n]", d2.Notes)
		}
		d3 := d.WithHelp("h")
		if d.Help != "" {
			t.Errorf("WithHelp changed the receiver: Help = %q", d.Help)
		}
		if d3.Help != "h" {
			t.Errorf("WithHelp copy: Help = %q, want %q", d3.Help, "h")
		}
		// A diagnostic has one help line: a second call replaces the first
		// instead of keeping it or joining the two.
		if d5 := d3.WithHelp("h2"); d5.Help != "h2" || d3.Help != "h" {
			t.Errorf("second WithHelp: Help = %q (receiver %q), want %q (receiver %q)", d5.Help, d3.Help, "h2", "h")
		}
		d4 := d.WithSecondary(first)
		if len(d.Secondary) != 0 {
			t.Errorf("WithSecondary changed the receiver: Secondary = %v", d.Secondary)
		}
		if !slices.Equal(d4.Secondary, []Label{first}) {
			t.Errorf("WithSecondary copy: Secondary = %v, want [%v]", d4.Secondary, first)
		}

		// Two copies made from the same diagnostic must not share the spare
		// room of its slices: with capacity left over, a careless append
		// would write both new elements into the same array slot.
		spare := d
		spare.Notes = make([]string, 1, 4)
		spare.Notes[0] = "old"
		spare.Secondary = make([]Label, 1, 4)
		spare.Secondary[0] = first
		a := spare.WithNote("a").WithSecondary(second)
		b := spare.WithNote("b").WithSecondary(primary)
		if !slices.Equal(a.Notes, []string{"old", "a"}) || !slices.Equal(b.Notes, []string{"old", "b"}) {
			t.Errorf("Notes of two copies = %q and %q, want [old a] and [old b]", a.Notes, b.Notes)
		}
		if !slices.Equal(a.Secondary, []Label{first, second}) || !slices.Equal(b.Secondary, []Label{first, primary}) {
			t.Errorf("Secondary of two copies = %v and %v, want [first second] and [first primary]", a.Secondary, b.Secondary)
		}
		if len(spare.Notes) != 1 || len(spare.Secondary) != 1 {
			t.Errorf("the receiver changed: Notes = %q, Secondary = %v", spare.Notes, spare.Secondary)
		}
	})

	t.Run("with_fixit_returns_copy", func(t *testing.T) {
		d := NewError(E(301), "unknown name `fibb`", Label{Span: base.Span{File: 1, Lo: 4, Hi: 8}})
		f := Replace(base.Span{File: 1, Lo: 4, Hi: 8}, "fib")
		d2 := d.WithFixIt(f)
		if len(d.Fixits) != 0 {
			t.Errorf("WithFixIt changed the receiver: Fixits = %v", d.Fixits)
		}
		if !slices.Equal(d2.Fixits, []FixIt{f}) {
			t.Errorf("WithFixIt copy: Fixits = %v, want [%v]", d2.Fixits, f)
		}

		// Two copies made from the same diagnostic must not share the spare
		// room of its slice, as with the notes and the secondary labels.
		g := Delete(base.Span{File: 1, Lo: 0, Hi: 1})
		h := Insert(base.Span{File: 1, Lo: 2, Hi: 3}, ")")
		spare := d
		spare.Fixits = make([]FixIt, 1, 4)
		spare.Fixits[0] = f
		a, b := spare.WithFixIt(g), spare.WithFixIt(h)
		if !slices.Equal(a.Fixits, []FixIt{f, g}) || !slices.Equal(b.Fixits, []FixIt{f, h}) {
			t.Errorf("Fixits of two copies = %v and %v, want [f g] and [f h]", a.Fixits, b.Fixits)
		}
		if len(spare.Fixits) != 1 {
			t.Errorf("the receiver changed: Fixits = %v", spare.Fixits)
		}

		// The three constructors. Insert puts the text at the start of the span
		// it is given and makes an empty span there, in the same file; Delete
		// keeps the span and has no replacement.
		at := base.Span{File: 3, Lo: 7, Hi: 11}
		if got, want := Insert(at, "&& b "), (FixIt{Span: base.Span{File: 3, Lo: 7, Hi: 7}, Replacement: "&& b "}); got != want {
			t.Errorf("Insert = %+v, want %+v", got, want)
		}
		if got, want := Delete(at), (FixIt{Span: at}); got != want {
			t.Errorf("Delete = %+v, want %+v", got, want)
		}
		if got, want := Replace(at, "x"), (FixIt{Span: at, Replacement: "x"}); got != want {
			t.Errorf("Replace = %+v, want %+v", got, want)
		}
	})
}
