package kitest_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/aldezex/kilang/internal/kitest"
)

// updateHead is the two required keys of a header, in the LF form.
const updateHead = "// test: T00.01\n// run: check\n"

// runUpdate writes before as f.ki in a new directory, runs it once with Update
// on and an Exec that answers with exit, stdout and stderr, and returns the file
// as it is afterwards together with the outcome of the run.
func runUpdate(t *testing.T, before string, exit int, stdout, stderr string) (after string, ok bool, report string) {
	t.Helper()
	return runUpdateWith(t, true, before, func([]string) (int, string, string) { return exit, stdout, stderr })
}

// runUpdateWith is runUpdate with the Update flag as a parameter and an answer
// that depends on the arguments, for the files that have annotations (those
// run twice).
func runUpdateWith(t *testing.T, update bool, before string, answer func(args []string) (int, string, string)) (after string, ok bool, report string) {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"f.ki": before})
	var calls []call
	r := &kitest.Runner{Root: dir, Update: update, Out: io.Discard, Exec: fakeExec(&calls, answer)}
	ok, report = r.RunFile("f.ki")
	data, err := os.ReadFile(filepath.Join(dir, "f.ki"))
	if err != nil {
		t.Fatalf("cannot read the file back: %v", err)
	}
	return string(data), ok, report
}

// updateRow is one case of the tables of TestUpdate: the file before, what the
// run printed, the file after and the report. An empty report means that the
// file passes once it is rewritten.
type updateRow struct {
	name, before string
	exit         int
	stdout       string
	stderr       string
	after        string
	report       string
}

// checkUpdateRows runs every row with Update on and fails the test unless the
// file ends as the row says and the run reports what it says.
func checkUpdateRows(t *testing.T, rows []updateRow) {
	t.Helper()
	for _, row := range rows {
		after, ok, report := runUpdate(t, row.before, row.exit, row.stdout, row.stderr)
		wantReport := row.report
		if wantReport == "" {
			wantReport = "PASS f.ki\n"
		}
		if after != row.after {
			t.Errorf("%s: the file is %q afterwards, want %q", row.name, after, row.after)
		}
		if report != wantReport || ok != (wantReport[0] == 'P') {
			t.Errorf("%s: got ok=%v and %q, want %q", row.name, ok, report, wantReport)
		}
	}
}

func TestUpdate(t *testing.T) {
	t.Run("rewrites_stdout_block", func(t *testing.T) {
		const before = updateHead + "// expect-stdout:\n// | old1\n// | old2\nfn main() {}\n"
		const after = updateHead + "// expect-stdout:\n// | new1\n// | new2\nfn main() {}\n"

		// Two lines for two lines. Without Update the same run fails and the
		// file stays.
		got, ok, report := runUpdateWith(t, false, before, func([]string) (int, string, string) { return 0, "new1\nnew2\n", "" })
		if want := "FAIL f.ki\n- old1\n+ new1\n"; ok || report != want || got != before {
			t.Errorf("without Update: got ok=%v, %q and the file %q, want false, %q and the file as it was", ok, report, got, want)
		}
		checkUpdateRows(t, []updateRow{
			{"two lines for two lines", before, 0, "new1\nnew2\n", "", after, ""},
			{"more lines than before", before, 0, "a\nb\nc\n", "", updateHead + "// expect-stdout:\n// | a\n// | b\n// | c\nfn main() {}\n", ""},
			{"fewer lines than before",
				updateHead + "// expect-stdout:\n// | 1\n// | 2\n// | 3\nfn main() {}\n", 0, "a\nb\n", "",
				updateHead + "// expect-stdout:\n// | a\n// | b\nfn main() {}\n", ""},
			// An empty line of output is `// |`, with no space after the bar.
			{"empty line in the output", before, 0, "a\n\nb\n", "", updateHead + "// expect-stdout:\n// | a\n// |\n// | b\nfn main() {}\n", ""},
			// A line of output that looks like a block line is still just text.
			{"output that looks like the header", before, 0, "// | x\n// test: y\n", "",
				updateHead + "// expect-stdout:\n// | // | x\n// | // test: y\nfn main() {}\n", ""},
			// The keys around the block, and the code after it, are not touched.
			{"keys around the block",
				updateHead + "// args: --diagnostics=short\n// expect-stdout:\n// | old1\n// | old2\n// expect-exit: 0\nfn main() {}\n", 0, "new1\nnew2\n", "",
				updateHead + "// args: --diagnostics=short\n// expect-stdout:\n// | new1\n// | new2\n// expect-exit: 0\nfn main() {}\n", ""},
			{"block at the end of the file", updateHead + "// expect-stdout:\n// | old1\n// | old2\n", 0, "new1\nnew2\n", "",
				updateHead + "// expect-stdout:\n// | new1\n// | new2\n", ""},
			// The block may start on the very first line of the file, before test
			// and run: the scan of the header begins at line 0.
			{"block on the first line",
				"// expect-stdout:\n// | old1\n// | old2\n" + updateHead + "fn main() {}\n", 0, "new1\nnew2\n", "",
				"// expect-stdout:\n// | new1\n// | new2\n" + updateHead + "fn main() {}\n", ""},
			// Both outputs are rewritten, each where its key already is.
			{"stdout and stderr",
				updateHead + "// expect-stdout:\n// | o\n// | p\n// expect-stderr:\n// | e\n// | f\nfn main() {}\n", 0, "O\nP\n", "E\nF\n",
				updateHead + "// expect-stdout:\n// | O\n// | P\n// expect-stderr:\n// | E\n// | F\nfn main() {}\n", ""},
			{"stderr before stdout in the file",
				updateHead + "// expect-stderr:\n// | e\n// | f\n// expect-stdout:\n// | o\n// | p\n", 0, "O\nP\n", "E\nF\n",
				updateHead + "// expect-stderr:\n// | E\n// | F\n// expect-stdout:\n// | O\n// | P\n", ""},
		})

		// A file that is already right is run once and not written, so it passes
		// even where it cannot be written; one that needs the rewrite fails there,
		// and says why. (A system that lets the test write to a file without
		// permission, such as one that runs it as root, cannot show the second.)
		dir := t.TempDir()
		path := filepath.Join(dir, "f.ki")
		writeFiles(t, dir, map[string]string{"f.ki": after})
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatalf("cannot make the file read-only: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		var calls []call
		answer := fakeExec(&calls, func([]string) (int, string, string) { return 0, "new1\nnew2\n", "" })
		r := &kitest.Runner{Root: dir, Update: true, Out: io.Discard, Exec: answer}
		if ok, report := r.RunFile("f.ki"); !ok || report != "PASS f.ki\n" || len(calls) != 1 {
			t.Errorf("file that is right: got ok=%v, %q and %d runs, want true, PASS and 1 run", ok, report, len(calls))
		}
		if f, err := os.OpenFile(path, os.O_WRONLY, 0); err != nil {
			calls = nil
			answer = fakeExec(&calls, func([]string) (int, string, string) { return 0, "other\n", "" })
			r = &kitest.Runner{Root: dir, Update: true, Out: io.Discard, Exec: answer}
			ok, report := r.RunFile("f.ki")
			if want := "FAIL f.ki\n- cannot write file: permission denied\n"; ok || report != want || len(calls) != 1 {
				t.Errorf("file that cannot be written: got ok=%v, %q and %d runs, want false, %q and 1 run", ok, report, len(calls), want)
			}
		} else {
			f.Close()
		}

		// A file whose header is malformed is reported and left as it was.
		const bad = "// test: T00.01\nfn main() {}\n"
		got, ok, report = runUpdateWith(t, true, bad, func([]string) (int, string, string) { return 0, "x\n", "" })
		if want := "FAIL f.ki\n- malformed header: missing key 'run'\n"; ok || report != want || got != bad {
			t.Errorf("malformed header: got ok=%v, %q and the file %q, want false, %q and the file as it was", ok, report, got, want)
		}
	})
	t.Run("single_line_form", func(t *testing.T) {
		checkUpdateRows(t, []updateRow{
			// The single-line form replaces a block, replaces a single line and is
			// inserted.
			{"one line replaces a block", updateHead + "// expect-stdout:\n// | 1\n// | 2\nfn main() {}\n", 0, "6765\n", "",
				updateHead + "// expect-stdout: 6765\nfn main() {}\n", ""},
			{"one line replaces a line", updateHead + "// expect-stdout: 6764\nfn main() {}\n", 0, "6765\n", "",
				updateHead + "// expect-stdout: 6765\nfn main() {}\n", ""},
			{"one line is inserted", updateHead + "fn main() {}\n", 0, "6765\n", "",
				updateHead + "// expect-stdout: 6765\nfn main() {}\n", ""},
			{"stderr as well", updateHead + "fn main() {}\n", 0, "", "boom\n",
				updateHead + "// expect-stderr: boom\nfn main() {}\n", ""},
			// Two lines are a block, also when the first one is empty.
			{"two lines are a block", updateHead + "// expect-stdout: 1\n", 0, "a\nb\n", "",
				updateHead + "// expect-stdout:\n// | a\n// | b\n", ""},
			// The text after the one space is the line, spaces included.
			{"leading spaces", updateHead, 0, "  x\n", "", updateHead + "// expect-stdout:   x\n", ""},
			{"trailing spaces", updateHead, 0, "x  \n", "", updateHead + "// expect-stdout: x  \n", ""},
			// A line with nothing but spaces or tabs would read as the opening of
			// a block, so it is written as a block.
			{"empty line", updateHead, 0, "\n", "", updateHead + "// expect-stdout:\n// |\n", ""},
			{"blank line", updateHead, 0, "  \n", "", updateHead + "// expect-stdout:\n// |   \n", ""},
			{"tab line", updateHead, 0, "\t\n", "", updateHead + "// expect-stdout:\n// | \t\n", ""},
			// Only the last line feed is the end of the output. Any other that
			// comes after text is the end of an empty line, which stays in the
			// block, so an output that ends in an empty line can be recorded.
			{"trailing empty line", updateHead, 0, "a\n\n", "", updateHead + "// expect-stdout:\n// | a\n// |\n", ""},
			{"two empty lines", updateHead, 0, "\n\n", "", updateHead + "// expect-stdout:\n// |\n// |\n", ""},
			// The format cannot say "no line feed at the end", so the file is
			// written with the line and the run still fails, as it should.
			{"output without a line feed", updateHead, 0, "6765", "", updateHead + "// expect-stdout: 6765\n",
				"FAIL f.ki\n- 6765\n+ 6765 [no newline at end]\n"},
		})
	})
	t.Run("empty_output_removes_key", func(t *testing.T) {
		checkUpdateRows(t, []updateRow{
			{"stderr block", updateHead + "// expect-stdout: o\n// expect-stderr:\n// | e1\n// | e2\nfn main() {}\n", 0, "o\n", "",
				updateHead + "// expect-stdout: o\nfn main() {}\n", ""},
			{"stderr single line", updateHead + "// expect-stdout: o\n// expect-stderr: e\nfn main() {}\n", 0, "o\n", "",
				updateHead + "// expect-stdout: o\nfn main() {}\n", ""},
			{"stdout block", updateHead + "// expect-stdout:\n// | x\n// | y\n// expect-stderr: e\nfn main() {}\n", 0, "", "e\n",
				updateHead + "// expect-stderr: e\nfn main() {}\n", ""},
			{"both", updateHead + "// expect-stdout: o\n// expect-stderr: e\n", 0, "", "", updateHead, ""},
			// An empty output with no key leaves the file as it is.
			{"nothing to remove", updateHead + "fn main() {}\n", 0, "", "", updateHead + "fn main() {}\n", ""},
			// A key on the last line, with no line feed after it.
			{"last line without a line feed", updateHead + "// expect-stderr: e", 0, "", "", updateHead, ""},
		})
	})
	t.Run("inserts_missing_key_after_header", func(t *testing.T) {
		checkUpdateRows(t, []updateRow{
			// The new key goes right after expect-exit.
			{"after expect-exit", updateHead + "// expect-exit: 1\nfn main() {}\n", 1, "out\n", "",
				updateHead + "// expect-exit: 1\n// expect-stdout: out\nfn main() {}\n", ""},
			{"after run", updateHead + "fn main() {}\n", 0, "out\n", "", updateHead + "// expect-stdout: out\nfn main() {}\n", ""},
			{"after args", updateHead + "// args: --diagnostics=short\nfn main() {}\n", 0, "out\n", "",
				updateHead + "// args: --diagnostics=short\n// expect-stdout: out\nfn main() {}\n", ""},
			// The last of test, run, args and expect-exit, wherever it is.
			{"after the last of the four", "// test: T00.01\n// args: x\n// run: check\n// expect-exit: 1\n// args2: y\nfn main() {}\n", 1, "out\n", "",
				"// test: T00.01\n// args: x\n// run: check\n// expect-exit: 1\n// expect-stdout: out\n// args2: y\nfn main() {}\n", ""},
			{"before a free comment", updateHead + "// note: free text\nfn main() {}\n", 0, "out\n", "",
				updateHead + "// expect-stdout: out\n// note: free text\nfn main() {}\n", ""},
			// Both keys, stdout first.
			{"stdout then stderr", updateHead + "// expect-exit: 1\nfn main() {}\n", 1, "o\n", "e\n",
				updateHead + "// expect-exit: 1\n// expect-stdout: o\n// expect-stderr: e\nfn main() {}\n", ""},
			// A key that is missing goes where the order puts it: stderr after
			// the stdout block, stdout before the stderr key.
			{"stderr after the stdout block", updateHead + "// expect-stdout:\n// | a\n// | b\nfn main() {}\n", 0, "a\nb\n", "e\n",
				updateHead + "// expect-stdout:\n// | a\n// | b\n// expect-stderr: e\nfn main() {}\n", ""},
			{"stdout before the stderr key", updateHead + "// expect-stderr: e\nfn main() {}\n", 0, "o\n", "e\n",
				updateHead + "// expect-stdout: o\n// expect-stderr: e\nfn main() {}\n", ""},
			{"stderr where the stdout key was removed", updateHead + "// expect-stdout: x\nfn main() {}\n", 0, "", "e\n",
				updateHead + "// expect-stderr: e\nfn main() {}\n", ""},
			{"a block", updateHead + "fn main() {}\n", 0, "a\nb\n", "", updateHead + "// expect-stdout:\n// | a\n// | b\nfn main() {}\n", ""},
			// The header is the whole file and has no line feed at the end.
			{"after a last line without a line feed", "// test: T00.01\n// run: check", 0, "out\n", "",
				updateHead + "// expect-stdout: out\n", ""},
			// Two edits at that same last line: it gets its line end once, not
			// once for each edit. They are two new keys, or a stdout that
			// replaces the last line and a new stderr right after it.
			{"two keys after a last line without a line feed", "// test: T00.01\n// run: check", 0, "out\n", "err\n",
				updateHead + "// expect-stdout: out\n// expect-stderr: err\n", ""},
			{"a new stderr after a replaced last line without a line feed", updateHead + "// expect-stdout: old", 0, "out\n", "err\n",
				updateHead + "// expect-stdout: out\n// expect-stderr: err\n", ""},
		})

		// A line added above the code moves the code down. A program whose
		// output names a line is run again on the file as it now is, so the
		// report says the truth: the first update wrote the line count of the
		// file before it, and fails. The second one settles.
		dir := t.TempDir()
		writeFiles(t, dir, map[string]string{"f.ki": updateHead + "fn main() {}\n"})
		lineCount := func([]string) (int, string, string) {
			data, err := os.ReadFile("f.ki")
			if err != nil {
				t.Errorf("the runner did not leave the file in place: %v", err)
			}
			return 0, strconv.Itoa(bytes.Count(data, []byte("\n"))) + "\n", ""
		}
		var calls []call
		r := &kitest.Runner{Root: dir, Update: true, Out: io.Discard, Exec: fakeExec(&calls, lineCount)}
		for i, want := range []string{"FAIL f.ki\n- 3\n+ 4\n", "PASS f.ki\n"} {
			if _, report := r.RunFile("f.ki"); report != want {
				t.Errorf("update %d: got %q, want %q", i+1, report, want)
			}
		}
		if data, err := os.ReadFile(filepath.Join(dir, "f.ki")); err != nil || string(data) != updateHead+"// expect-stdout: 4\nfn main() {}\n" {
			t.Errorf("the file is %q afterwards (error %v), want the line count 4", data, err)
		}
	})
	t.Run("preserves_bom_and_crlf", func(t *testing.T) {
		const bom = "\xef\xbb\xbf"
		checkUpdateRows(t, []updateRow{
			// The BOM stays, every other byte too, and the new lines end in CRLF.
			{"replacing a block", bom + "// test: T00.01\r\n// run: check\r\n// expect-stdout:\r\n// | old\r\nfn main() {}\r\n", 0, "a\nb\n", "",
				bom + "// test: T00.01\r\n// run: check\r\n// expect-stdout:\r\n// | a\r\n// | b\r\nfn main() {}\r\n", ""},
			{"inserting", bom + "// test: T00.01\r\n// run: check\r\nfn main() {}\r\n", 0, "a\nb\n", "e\n",
				bom + "// test: T00.01\r\n// run: check\r\n// expect-stdout:\r\n// | a\r\n// | b\r\n// expect-stderr: e\r\nfn main() {}\r\n", ""},
			{"removing", bom + "// test: T00.01\r\n// run: check\r\n// expect-stdout: x\r\n// expect-stderr: e\r\nfn main() {}\r\n", 0, "", "e\n",
				bom + "// test: T00.01\r\n// run: check\r\n// expect-stderr: e\r\nfn main() {}\r\n", ""},
			{"CRLF without a BOM", "// test: T00.01\r\n// run: check\r\n// expect-stdout: old\r\nfn main() {}\r\n", 0, "new\n", "",
				"// test: T00.01\r\n// run: check\r\n// expect-stdout: new\r\nfn main() {}\r\n", ""},
			{"a BOM with LF", bom + updateHead + "// expect-stdout: old\nfn main() {}\n", 0, "new\n", "",
				bom + updateHead + "// expect-stdout: new\nfn main() {}\n", ""},
			// The first line decides, and the bytes elsewhere stay as they were.
			{"LF first, CRLF later", "// test: T00.01\n// run: check\r\n// expect-stdout: old\r\nfn main() {}\r\nlet x = 1", 0, "new\n", "",
				"// test: T00.01\n// run: check\r\n// expect-stdout: new\nfn main() {}\r\nlet x = 1", ""},
			{"CRLF first, LF later", "// test: T00.01\r\n// run: check\n// expect-stdout: old\nfn main() {}\n", 0, "a\nb\n", "",
				"// test: T00.01\r\n// run: check\n// expect-stdout:\r\n// | a\r\n// | b\r\nfn main() {}\n", ""},
		})
	})
	t.Run("never_touches_expect_exit", func(t *testing.T) {
		checkUpdateRows(t, []updateRow{
			// The expected exit code is a decision of the author, so the run
			// still fails on it.
			{"exit code differs", updateHead + "// expect-exit: 7\n// expect-stdout: old\n", 1, "new\n", "",
				updateHead + "// expect-exit: 7\n// expect-stdout: new\n", "FAIL f.ki\n- exit 7\n+ exit 1\n"},
			{"expect-exit 0", updateHead + "// expect-exit: 0\n// expect-stdout: old\n", 3, "new\n", "",
				updateHead + "// expect-exit: 0\n// expect-stdout: new\n", "FAIL f.ki\n- exit 0\n+ exit 3\n"},
			// And none is added when the file has none.
			{"no expect-exit", updateHead + "// expect-stdout: old\n", 1, "new\n", "",
				updateHead + "// expect-stdout: new\n", "FAIL f.ki\n- exit 0\n+ exit 1\n"},
		})
	})
	t.Run("keeps_annotations", func(t *testing.T) {
		diagJSON := `{"severity":"error","code":"E0101","message":"m","path":"f.ki","line":3,"col":9,"labels":[],"notes":[],"help":null}` + "\n"
		// answer is the run of an erroneous file: the first pass prints text on
		// both streams and the second, with --diagnostics=json, the diagnostic.
		answer := func(stdout string) func([]string) (int, string, string) {
			return func(args []string) (int, string, string) {
				if slices.Contains(args, "--diagnostics=json") {
					return 1, "", diagJSON
				}
				return 1, stdout, "error[E0101]: free text, never compared\n"
			}
		}
		const annotated = updateHead + "let x = 1  // ERROR(E0101) @9\n"
		rows := []struct {
			name, before, stdout, after string
		}{
			// The annotation is still the same text, and stderr, which is not
			// compared in a file that has annotations, is not written either.
			{"no output", annotated, "", annotated},
			{"stdout is rewritten", annotated, "out\n", updateHead + "// expect-stdout: out\nlet x = 1  // ERROR(E0101) @9\n"},
			{"stale stdout is rewritten", updateHead + "// expect-stdout: old\nlet x = 1  // ERROR(E0101) @9\n", "new\n",
				updateHead + "// expect-stdout: new\nlet x = 1  // ERROR(E0101) @9\n"},
			{"an expect-stderr is left as it is", updateHead + "// expect-stderr: old\nlet x = 1  // ERROR(E0101) @9\n", "",
				updateHead + "// expect-stderr: old\nlet x = 1  // ERROR(E0101) @9\n"},
			{"an annotation in the header", updateHead + "// ERROR(E0101) @3:9\nlet x = 1\n", "out\n",
				updateHead + "// expect-stdout: out\n// ERROR(E0101) @3:9\nlet x = 1\n"},
			{"two annotations in a line", updateHead + "let x = 1  // ERROR(E0101) @9  // ERROR(E0101) @3:9\n", "",
				updateHead + "let x = 1  // ERROR(E0101) @9  // ERROR(E0101) @3:9\n"},
		}
		for _, row := range rows {
			after, _, _ := runUpdateWith(t, true, row.before, answer(row.stdout))
			if after != row.after {
				t.Errorf("%s: the file is %q afterwards, want %q", row.name, after, row.after)
			}
		}
		// The annotation of the first row is still checked after the rewrite:
		// the run passes where the diagnostic is, and fails where it is not.
		_, ok, report := runUpdateWith(t, true, annotated, answer(""))
		if !ok || report != "PASS f.ki\n" {
			t.Errorf("annotation at the diagnostic: got ok=%v and %q, want PASS", ok, report)
		}
		_, ok, report = runUpdateWith(t, true, updateHead+"let x = 1  // ERROR(E0101) @1\n", answer(""))
		if want := "FAIL f.ki\nmissing E0101 at 3:1\nunexpected E0101 at 3:9\n"; ok || report != want {
			t.Errorf("annotation elsewhere: got ok=%v and %q, want %q", ok, report, want)
		}

		// The line that Update inserts above the code moves the code down, and the
		// annotations are not moved with it. This run reports the diagnostic where
		// the code is in the file as it is at that moment: at line 3 before the
		// insertion and at line 4 after it. An absolute `@3:9` keeps saying 3, so
		// the run that judges the file fails on it; a relative `@9` goes with its
		// line and passes.
		atCode := func(args []string) (int, string, string) {
			data, err := os.ReadFile("f.ki")
			if err != nil {
				t.Errorf("the runner did not leave the file in place: %v", err)
			}
			if !slices.Contains(args, "--diagnostics=json") {
				return 1, "out\n", ""
			}
			line := 1 + bytes.Count(data[:bytes.Index(data, []byte("let x"))], []byte("\n"))
			return 1, "", fmt.Sprintf(`{"severity":"error","code":"E0101","message":"m","path":"f.ki","line":%d,"col":9,"labels":[],"notes":[],"help":null}`+"\n", line)
		}
		after, ok, report := runUpdateWith(t, true, updateHead+"let x = 1  // ERROR(E0101) @3:9\n", atCode)
		if want := updateHead + "// expect-stdout: out\nlet x = 1  // ERROR(E0101) @3:9\n"; after != want {
			t.Errorf("absolute annotation: the file is %q afterwards, want %q", after, want)
		}
		if want := "FAIL f.ki\nmissing E0101 at 3:9\nunexpected E0101 at 4:9\n"; ok || report != want {
			t.Errorf("absolute annotation: got ok=%v and %q, want %q", ok, report, want)
		}
		_, ok, report = runUpdateWith(t, true, updateHead+"let x = 1  // ERROR(E0101) @9\n", atCode)
		if !ok || report != "PASS f.ki\n" {
			t.Errorf("relative annotation: got ok=%v and %q, want PASS", ok, report)
		}
	})
}
