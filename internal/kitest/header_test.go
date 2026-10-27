package kitest_test

import (
	"reflect"
	"testing"

	"github.com/aldezex/kilang/internal/kitest"
)

// ptr returns a pointer to v, to write the optional fields of a Spec.
func ptr[T any](v T) *T { return &v }

// parse parses text and fails the test if the header is malformed.
func parse(t *testing.T, text string) kitest.Spec {
	t.Helper()
	spec, err := kitest.ParseHeader([]byte(text))
	if err != nil {
		t.Fatalf("ParseHeader(%q): unexpected error: %v", text, err)
	}
	return spec
}

// parseError parses text and returns the text of the error, failing the test
// if the header is accepted.
func parseError(t *testing.T, text string) string {
	t.Helper()
	spec, err := kitest.ParseHeader([]byte(text))
	if err == nil {
		t.Fatalf("ParseHeader(%q) accepted the header: %+v", text, spec)
	}
	return err.Error()
}

func TestParseHeader(t *testing.T) {
	t.Run("minimal", func(t *testing.T) {
		got := parse(t, "// test: T03.01\n// run: check\nfn main() {}\n")
		want := kitest.Spec{TestID: "T03.01", Run: []string{"check"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	t.Run("single_line_stdout", func(t *testing.T) {
		got := parse(t, "// test: T10.03\n// run: emit tokens\n// args: --diagnostics=short -x\n"+
			"// expect-exit: 101\n// expect-stdout: 6765\nfn main() {}\n")
		want := kitest.Spec{
			TestID:       "T10.03",
			Run:          []string{"emit", "tokens"},
			Args:         []string{"--diagnostics=short", "-x"},
			ExpectExit:   ptr(101),
			ExpectStdout: ptr("6765\n"),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	t.Run("block_stderr", func(t *testing.T) {
		got := parse(t, "// test: T1\n// run: run\n// expect-stderr:\n// | a\n// |\n// |   b\n// | \n// note: after the block\nfn main() {}\n")
		// "// | " followed by nothing is an empty line too; "// |   b" keeps
		// the two spaces of indentation after the one that belongs to the marker.
		want := kitest.Spec{TestID: "T1", Run: []string{"run"}, ExpectStderr: ptr("a\n\n  b\n\n")}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}

		// A colon followed by spaces only opens a block as well, not a value
		// of one empty line: editors that trim trailing spaces turn one
		// spelling into the other.
		for _, opener := range []string{"// expect-stdout: ", "// expect-stdout:   ", "// expect-stdout: \t"} {
			got = parse(t, "// test: T1\n// run: check\n"+opener+"\n// | a\nfn main() {}\n")
			want = kitest.Spec{TestID: "T1", Run: []string{"check"}, ExpectStdout: ptr("a\n")}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%q: got %+v, want %+v", opener, got, want)
			}
		}
	})
	t.Run("annotation_same_line", func(t *testing.T) {
		got := parse(t, "// test: T1\n// run: check\nlet x = $  // ERROR(E0101) @9\n")
		want := kitest.Spec{TestID: "T1", Run: []string{"check"}, Errors: []kitest.Annotation{{Code: "E0101", Line: 3, Col: 9}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	t.Run("annotation_absolute", func(t *testing.T) {
		got := parse(t, "// test: T1\n// run: check\nfn f() {}\nfn g() {}\nfn h() {}  // ERROR(W0301) @12:32\n")
		want := kitest.Spec{TestID: "T1", Run: []string{"check"}, Errors: []kitest.Annotation{{Code: "W0301", Line: 12, Col: 32}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	t.Run("two_annotations_one_line", func(t *testing.T) {
		got := parse(t, "// test: T1\n// run: check\nx  // ERROR(E0101) @3  // ERROR(E0102) @9\ny  // ERROR(E0401) @1\n")
		want := kitest.Spec{TestID: "T1", Run: []string{"check"}, Errors: []kitest.Annotation{
			{Code: "E0101", Line: 3, Col: 3}, {Code: "E0102", Line: 3, Col: 9}, {Code: "E0401", Line: 4, Col: 1},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	t.Run("block_text_is_not_annotated", func(t *testing.T) {
		// Inside a block, `// ERROR(` is text of the expected output; outside
		// one, a line that starts with `// |` is a comment like any other, and
		// its annotation counts.
		got := parse(t, "// test: T1\n// run: check\n// expect-stderr:\n// | x // ERROR(E0101) @1\n"+
			"// |y // ERROR(E0102) @4\nlet a = 1\n")
		want := kitest.Spec{
			TestID:       "T1",
			Run:          []string{"check"},
			ExpectStderr: ptr("x // ERROR(E0101) @1\n"),
			Errors:       []kitest.Annotation{{Code: "E0102", Line: 5, Col: 4}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	t.Run("duplicate_key_is_error", func(t *testing.T) {
		const head = "// test: T1\n// run: check\n"
		tests := []struct{ name, text, want string }{
			{"test", head + "// test: T2\n", "malformed header: duplicate key 'test'"},
			{"run", head + "// run: run\n", "malformed header: duplicate key 'run'"},
			{"args", head + "// args: -x\n// args: -y\n", "malformed header: duplicate key 'args'"},
			{"expect-exit", head + "// expect-exit: 1\n// expect-exit: 2\n", "malformed header: duplicate key 'expect-exit'"},
			{"expect-stdout", head + "// expect-stdout: a\n// expect-stdout: b\n", "malformed header: duplicate key 'expect-stdout'"},
			{"expect-stderr after a block", head + "// expect-stderr:\n// | a\n// expect-stderr:\n// | b\n", "malformed header: duplicate key 'expect-stderr'"},
		}
		for _, tc := range tests {
			if got := parseError(t, tc.text); got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
			}
		}
		// Different keys are not duplicates, and neither are a key and the
		// same word as free text after the header.
		parse(t, head+"// expect-stdout: a\n// expect-stderr: b\nfn main() {}\n// run: later\n")
	})
	t.Run("missing_run_is_error", func(t *testing.T) {
		tests := []struct{ name, text, want string }{
			{"missing run", "// test: T1\nfn main() {}\n", "malformed header: missing key 'run'"},
			{"missing test", "// run: check\nfn main() {}\n", "malformed header: missing key 'test'"},
			{"test is reported first", "fn main() {}\n", "malformed header: missing key 'test'"},
			{"a key after the header does not count", "// test: T1\nfn main() {}\n// run: check\n", "malformed header: missing key 'run'"},
			{"unknown run value", "// test: T1\n// run: foo\n", "malformed header: unknown run value 'foo'"},
			{"unknown emit target", "// test: T1\n// run: emit bogus\n", "malformed header: unknown run value 'emit bogus'"},
		}
		for _, tc := range tests {
			if got := parseError(t, tc.text); got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
			}
		}
	})
	t.Run("known_key_without_space_is_error", func(t *testing.T) {
		tests := []struct{ name, text, want string }{
			{"expect-stdout", "// test: T1\n// run: check\n// expect-stdout:6765\n", "malformed header: expect-stdout needs a space after ':'"},
			{"run", "// test: T1\n// run:check\n", "malformed header: run needs a space after ':'"},
			{"test", "// test:T1\n// run: check\n", "malformed header: test needs a space after ':'"},
			{"args", "// test: T1\n// run: check\n// args:-x\n", "malformed header: args needs a space after ':'"},
			{"expect-exit", "// test: T1\n// run: check\n// expect-exit:1\n", "malformed header: expect-exit needs a space after ':'"},
			{"nothing after a key that opens no block", "// test: T1\n// run:\n", "malformed header: run needs a space after ':'"},
		}
		for _, tc := range tests {
			if got := parseError(t, tc.text); got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
			}
		}
	})
	t.Run("unknown_word_colon_is_free_text", func(t *testing.T) {
		got := parse(t, "// test: T1\n// note: anything: goes\n// run: check\n// Non-ASCII in comments is fine: año\n// expect-stdout x: y\nfn main() {}\n")
		want := kitest.Spec{TestID: "T1", Run: []string{"check"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	t.Run("bad_code_is_error", func(t *testing.T) {
		tests := []struct{ code, want string }{
			{"E01", "malformed header: bad error code 'E01'"},
			{"e0101", "malformed header: bad error code 'e0101'"},
			{"X0101", "malformed header: bad error code 'X0101'"},
			{"E01011", "malformed header: bad error code 'E01011'"},
			{"", "malformed header: bad error code ''"},
		}
		for _, tc := range tests {
			text := "// test: T1\n// run: check\nx  // ERROR(" + tc.code + ") @3\n"
			if got := parseError(t, text); got != tc.want {
				t.Errorf("%q: got %q, want %q", tc.code, got, tc.want)
			}
		}
	})
	t.Run("bad_position_is_error", func(t *testing.T) {
		const badPosition = "malformed header: bad position for ERROR(E0101): want '@COL' or '@LINE:COL'"
		tests := []struct{ name, annotation, want string }{
			{"column zero", "ERROR(E0101) @0", badPosition},
			{"line zero", "ERROR(E0101) @0:1", badPosition},
			{"column zero after a line", "ERROR(E0101) @1:0", badPosition},
			{"not a number", "ERROR(E0101) @x", badPosition},
			{"a letter after the number", "ERROR(E0101) @3x", badPosition},
			{"three numbers", "ERROR(E0101) @1:2:3", badPosition},
			{"empty", "ERROR(E0101) @", badPosition},
			{"negative", "ERROR(E0101) @-1", badPosition},
			{"past 32 bits", "ERROR(E0101) @4294967296", badPosition},
			{"no @", "ERROR(E0101)", badPosition},
			{"a number without @", "ERROR(E0101) 9", badPosition},
			{"a second annotation is checked too", "ERROR(E0101) @3  // ERROR(E0102) @0", "malformed header: bad position for ERROR(E0102): want '@COL' or '@LINE:COL'"},
			// Without the ')' the rest of the line is taken for the code.
			{"no ')'", "ERROR(E0101 @3", "malformed header: bad error code 'E0101 @3'"},
		}
		for _, tc := range tests {
			text := "// test: T1\n// run: check\nx  // " + tc.annotation + "\n"
			if got := parseError(t, text); got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
			}
		}
		// The largest column that fits in 32 bits is a position.
		got := parse(t, "// test: T1\n// run: check\nx  // ERROR(E0101) @4294967295\n")
		if want := []kitest.Annotation{{Code: "E0101", Line: 3, Col: 4294967295}}; !reflect.DeepEqual(got.Errors, want) {
			t.Errorf("the largest column: got %+v, want %+v", got.Errors, want)
		}
	})
	t.Run("bad_expect_exit_is_error", func(t *testing.T) {
		tests := []struct{ value, want string }{
			{"abc", "malformed header: bad expect-exit 'abc'"},
			{"", "malformed header: bad expect-exit ''"},
			{"1.5", "malformed header: bad expect-exit '1.5'"},
			{"0x1", "malformed header: bad expect-exit '0x1'"},
			{"1 2", "malformed header: bad expect-exit '1 2'"},
		}
		for _, tc := range tests {
			text := "// test: T1\n// run: check\n// expect-exit: " + tc.value + "\n"
			if got := parseError(t, text); got != tc.want {
				t.Errorf("%q: got %q, want %q", tc.value, got, tc.want)
			}
		}
		// Any integer is accepted, so that a test can expect what a process
		// killed by a signal reports.
		if got := parse(t, "// test: T1\n// run: check\n// expect-exit: -1\n"); got.ExpectExit == nil || *got.ExpectExit != -1 {
			t.Errorf("expect-exit: -1 gave %v", got.ExpectExit)
		}
	})
	t.Run("detects_bom_and_crlf", func(t *testing.T) {
		const bom = "\xef\xbb\xbf"
		tests := []struct {
			name, text string
			bom, crlf  bool
		}{
			{"neither", "// test: T1\n// run: check\n", false, false},
			{"bom", bom + "// test: T1\n// run: check\n", true, false},
			{"crlf", "// test: T1\r\n// run: check\r\n", false, true},
			{"both", bom + "// test: T1\r\n// run: check\r\n", true, true},
			// The terminator of the first line decides.
			{"crlf only after the first line", "// test: T1\n// run: check\r\n", false, false},
		}
		for _, tc := range tests {
			spec, err := kitest.ParseHeader([]byte(tc.text))
			if err != nil {
				t.Errorf("%s: unexpected error: %v", tc.name, err)
				continue
			}
			if spec.BOM != tc.bom || spec.CRLF != tc.crlf {
				t.Errorf("%s: got BOM=%v CRLF=%v, want BOM=%v CRLF=%v", tc.name, spec.BOM, spec.CRLF, tc.bom, tc.crlf)
			}
			if spec.TestID != "T1" || !reflect.DeepEqual(spec.Run, []string{"check"}) {
				t.Errorf("%s: the values keep a stray byte: TestID=%q Run=%q", tc.name, spec.TestID, spec.Run)
			}
		}

		// A file whose first byte (after the BOM) is the line end has no
		// header, because the header is the run of `//` lines at the start. It
		// is an error, not a panic from looking at the byte before the '\n'.
		for _, text := range []string{"\n// test: T1\n", bom + "\n// test: T1\n"} {
			if _, err := kitest.ParseHeader([]byte(text)); err == nil || err.Error() != "malformed header: missing key 'test'" {
				t.Errorf("ParseHeader(%q): got error %v, want \"malformed header: missing key 'test'\"", text, err)
			}
		}

		// TrimSpace and Fields clean a '\r' off the values above, so those
		// rows cannot tell whether the line ends were stripped. These can: a
		// block line keeps its text as it is, an annotation reads its column
		// up to the end of the line, and expect-stdout takes the rest of it.
		for _, prefix := range []string{"", bom} {
			text := prefix + "// test: T1\r\n// run: check\r\n// expect-exit: 3\r\n// expect-stdout: ok\r\n" +
				"// expect-stderr:\r\n// | a\r\n// |\r\n// | b\r\nx // ERROR(E0101) @9\r\n"
			got := parse(t, text)
			want := kitest.Spec{
				TestID:       "T1",
				Run:          []string{"check"},
				ExpectExit:   ptr(3),
				ExpectStdout: ptr("ok\n"),
				ExpectStderr: ptr("a\n\nb\n"),
				Errors:       []kitest.Annotation{{Code: "E0101", Line: 9, Col: 9}},
				BOM:          prefix != "",
				CRLF:         true,
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("CRLF header (BOM=%v): got %+v, want %+v", prefix != "", got, want)
			}
		}
	})
}
