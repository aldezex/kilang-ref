package driver

import (
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
)

// warningSrc is a program whose only diagnostic is a warning: the arm `2 => 2` of `g`
// can never run, because `_` takes every number before it.
const warningSrc = "fn g(n: Int) -> Int {\n  match n {\n    _ => 1\n    2 => 2\n  }\n}\nfn main() { print(7) }\n"

func TestWarnings(t *testing.T) {
	t.Run("check_writes_a_warning_and_exits_0", func(t *testing.T) {
		// A warning is a diagnostic about a program that is accepted, so `ki check`
		// writes it in each of the three formats and exits with 0, as for a file that
		// has nothing to say. The word before the code is the severity, in the human
		// and the short format, and the JSON has it as a key; the edit that deletes the
		// arm is in the diff form of the human format and in the fixits of the JSON.
		path := writeKi(t, warningSrc)
		human := "warning[W0701]: unreachable pattern\n" +
			" --> " + path + ":4:5\n" +
			"  |\n" +
			"4 |     2 => 2\n" +
			"  |     ^ covered by the arms above\n" +
			"  = help: remove this arm\n" +
			"  |\n" +
			"4 -     2 => 2\n" +
			"4 +\n"
		short := path + ":4:5: warning[W0701]: unreachable pattern\n"
		json := `{"severity":"warning","code":"W0701","message":"unreachable pattern","path":"` + path + `","line":4,"col":5,` +
			`"labels":[{"primary":true,"line":4,"col":5,"message":"covered by the arms above"}],"notes":[],"help":"remove this arm",` +
			`"fixits":[{"line":4,"col":1,"end_line":4,"end_col":11,"replacement":""}]}` + "\n"
		for _, row := range []struct{ format, want string }{{"human", human}, {"short", short}, {"json", json}} {
			code, stdout, stderr := runCLI(t, "check", "--diagnostics="+row.format, path)
			if code != 0 || stdout != "" || stderr != row.want {
				t.Errorf("ki check, %s: got %d, stdout %q, stderr %q, want 0, nothing on stdout and stderr %q", row.format, code, stdout, stderr, row.want)
			}
		}
	})

	t.Run("run_does_not_write_a_warning", func(t *testing.T) {
		// `ki run` writes diagnostics only for a program that it cannot run, so that
		// what the program writes, and the first line of the stderr of a panic, are all
		// that a run says: this program runs, prints 7, and its stderr is empty.
		path := writeKi(t, warningSrc)
		if code, stdout, stderr := runCLI(t, "run", path); code != 0 || stdout != "7\n" || stderr != "" {
			t.Errorf("ki run: got %d, stdout %q, stderr %q, want 0, \"7\\n\" and an empty stderr", code, stdout, stderr)
		}
	})

	t.Run("emit_leaves_warnings_out", func(t *testing.T) {
		// The dump of `ki emit` is followed by the errors, and by no warning: a
		// warning alone leaves stderr empty and the exit code 0, and with an error
		// the error is written and the warning is not, and there is no dump (Core is
		// made for a program without errors). Both are asked of `ki emit core`, on the
		// program of the warning and on the same program with an error added. The
		// ending that all the targets share is asked as well, with a Session whose
		// sink has both, because the other targets cannot have a warning.
		path := writeKi(t, warningSrc)
		dump := "fn g(n) =\n  match n {\n    _ => 1\n    2 => 2\n  }\nfn main() =\n  print(7)\n"
		if code, stdout, stderr := runCLI(t, "emit", "core", path); code != 0 || stdout != dump || stderr != "" {
			t.Errorf("ki emit core of the warning program: got %d, stdout %q, stderr %q, want 0, the dump %q and an empty stderr", code, stdout, stderr, dump)
		}
		both := writeKi(t, strings.Replace(warningSrc, "print(7)", "print(zz)", 1))
		wantErr := both + ":7:19: error[E0301]: unknown name `zz`\n"
		if code, stdout, stderr := runCLI(t, "emit", "core", "--diagnostics=short", both); code != 1 || stdout != "" || stderr != wantErr {
			t.Errorf("ki emit core of a program with a warning and an error: got %d, stdout %q, stderr %q, want 1, nothing on stdout and stderr %q", code, stdout, stderr, wantErr)
		}
		var out, errOut strings.Builder
		s := NewSession(strings.NewReader(""), &out, &errOut)
		s.Format = FormatShort
		id := s.Sources.Add("a.ki", []byte("fn f() {}\n"))
		at := base.Span{File: id, Lo: 3, Hi: 4}
		s.Sink.Report(diag.Diagnostic{Severity: diag.Warning, Code: diag.W(701), Message: "unreachable pattern",
			Primary: diag.Label{Span: at, Message: "covered by the arms above"}})
		if code := finishEmit(s, &command{Name: "emit"}, "dump\n"); code != 0 || out.String() != "dump\n" || errOut.String() != "" {
			t.Errorf("a warning: got %d, stdout %q, stderr %q, want 0, the dump and nothing on stderr", code, out.String(), errOut.String())
		}
		out.Reset()
		s.Sink.Report(diag.NewError(diag.E(301), "unknown name `y`", diag.Label{Span: at, Message: "not found in this scope"}))
		code := finishEmit(s, &command{Name: "emit"}, "dump\n")
		if want := "a.ki:1:4: error[E0301]: unknown name `y`\n"; code != 1 || out.String() != "dump\n" || errOut.String() != want {
			t.Errorf("a warning and an error: got %d, stdout %q, stderr %q, want 1, the dump and %q", code, out.String(), errOut.String(), want)
		}
	})
}
