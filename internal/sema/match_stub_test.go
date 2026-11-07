package sema_test

import (
	"testing"

	"github.com/aldezex/kilang/internal/testsupport"
)

// Until the sessions that bind and type patterns (ch12-s2 and after), name
// resolution and the type checker read a match by its scrutinee only, and a `let`
// with a pattern by its initializer only. These tests pin that: no pass panics on
// the nodes the parser makes for them, the scrutinee and the initializer are
// checked like any expression, and nothing inside an arm is, because the
// variables that its pattern binds are bound by no one yet and would all be
// reported as unknown names. The session that binds them replaces these rows.

func TestMatchBeforeItsTypes(t *testing.T) {
	t.Run("a_match_is_read_by_its_scrutinee_only", func(t *testing.T) {
		rows := []struct {
			name, src string
			want      []string
		}{
			{"a match that is fine", "fn f(n: Int) -> Int { match n { 0 => 1\n _ => 2 } }", nil},
			{"a match with no arms", "fn f(n: Int) -> Int { match n { } }", nil},
			{"a variable of a pattern in the body", "fn f(n: Int) -> Int { match n { m => m } }", nil},
			{"a name that nothing defines in an arm", "fn f(n: Int) -> Int { match n { m if zzz => zzz } }", nil},
			{"the match has no type to mismatch", "fn f() -> Int { match 1 { _ => true } }", nil},
			{"a match as an argument", "fn f() { print(match 1 { _ => true }) }", nil},
			{"a match in a match", "fn f(n: Int) -> Int { match match n { _ => n } { _ => 1 } }", nil},
			// The scrutinee is not the first expression of these, so a pass that walks
			// another expression of the match would not find the mistake.
			{"an unknown name as the scrutinee", "fn f() -> Int { let a = 1\n match zzz { _ => 1 } }", []string{"E0301"}},
			{"a type error in the scrutinee", "fn f() -> Int { let a = 1\n match 1 + true { _ => 1 } }", []string{"E0401"}},
			{"a match in the scrutinee", "fn f() -> Int { let a = 1\n match match zzz { _ => 1 } { _ => 1 } }", []string{"E0301"}},
		}
		for _, row := range rows {
			p := testsupport.Analyze(t, row.src)
			if got := testsupport.ErrorCodes(p); len(got) != len(row.want) || (len(got) == 1 && got[0] != row.want[0]) {
				t.Errorf("%s: %q reports %v, want %v: %+v", row.name, row.src, got, row.want, p.Session.Sink.All())
			}
		}
	})

	t.Run("a_let_with_a_pattern_is_read_by_its_initializer_only", func(t *testing.T) {
		rows := []struct {
			name, src string
			want      []string
		}{
			{"a tuple", "fn f() { let (a, b) = (1, true) }", nil},
			{"a wildcard", "fn f() { let _ = 1 }", nil},
			{"a pattern and a plain let", "fn f() -> Int { let (a, b) = (1, 2)\n let c = 3\n c }", nil},
			{"an unknown name in the initializer", "fn f() { let (a, b) = zzz }", []string{"E0301"}},
			{"a type error in the initializer", "fn f() { let (a, b) = 1 + true }", []string{"E0401"}},
			{"a match in the initializer", "fn f() { let (a, b) = match zzz { _ => 1 } }", []string{"E0301"}},
			{"a statement after it is checked", "fn f() { let (a, b) = (1, 2)\n let c: Int = true }", []string{"E0401"}},
		}
		for _, row := range rows {
			p := testsupport.Analyze(t, row.src)
			if got := testsupport.ErrorCodes(p); len(got) != len(row.want) || (len(got) == 1 && got[0] != row.want[0]) {
				t.Errorf("%s: %q reports %v, want %v: %+v", row.name, row.src, got, row.want, p.Session.Sink.All())
			}
		}
		// LetLocal holds no variable for such a `let`, and 0 is the first variable of
		// the prelude: the checker must not write the type of the initializer there.
		typeOfFirstLocal := func(src string) string {
			p := testsupport.Analyze(t, src)
			typed := p.Frontend.Typed
			return typed.Types.Show(typed.LocalTypes.Get(0))
		}
		if with, without := typeOfFirstLocal("fn f() { let (a, b) = (1.5, true) }"), typeOfFirstLocal("fn f() {}"); with != without {
			t.Errorf("the first variable has the type %s after a `let` with a pattern, and %s without it", with, without)
		}
	})
}
