// bookx: not-in-book

package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// fixture is a book that lives in memory: the working tree and the files of
// each tag. It implements repoReader, so verify and render run on it with no
// git. testdata/verify/base holds a book that has no problems: two chapters,
// both tagged. Each test loads it and breaks one thing.
//
// The SHA that Rev gives for a tag is the SHA-1 of its name; the
// verified-tags.txt of the fixture was written with those values.
type fixture struct {
	t    *testing.T
	tree fstest.MapFS
	tags map[string]fstest.MapFS
}

// loadFixture reads testdata/verify/base into memory.
func loadFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, tree: fstest.MapFS{}, tags: map[string]fstest.MapFS{}}
	root := filepath.Join("testdata", "verify", "base")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		switch {
		case parts[0] == "tree":
			f.tree[strings.Join(parts[1:], "/")] = &fstest.MapFile{Data: data}
		case parts[0] == "tags":
			if f.tags[parts[1]] == nil {
				f.tags[parts[1]] = fstest.MapFS{}
			}
			f.tags[parts[1]][strings.Join(parts[2:], "/")] = &fstest.MapFile{Data: data}
		default:
			t.Fatalf("unexpected fixture file %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("loading the fixture: %v", err)
	}
	return f
}

// cleanFixture loads the base book and fails the test if it has any problem,
// so that every problem a test sees comes from what the test changed.
func cleanFixture(t *testing.T) *fixture {
	t.Helper()
	f := loadFixture(t)
	if got := f.problems(); len(got) != 0 {
		t.Fatalf("the base fixture should be clean, but verify says:\n%s", strings.Join(got, "\n"))
	}
	return f
}

// fakeRev is the SHA that the fixture gives to a tag.
func fakeRev(tag string) string {
	sum := sha1.Sum([]byte(tag))
	return hex.EncodeToString(sum[:])
}

func (f *fixture) Show(tag, p string) ([]byte, error) {
	files, ok := f.tags[tag]
	if !ok {
		return nil, fmt.Errorf("no tag %s", tag)
	}
	file, ok := files[p]
	if !ok {
		return nil, fmt.Errorf("%s:%s: %w", tag, p, fs.ErrNotExist)
	}
	return file.Data, nil
}

func (f *fixture) Files(tag string) ([]string, error) {
	files, ok := f.tags[tag]
	if !ok {
		return nil, fmt.Errorf("no tag %s", tag)
	}
	var list []string
	for p := range files {
		list = append(list, p)
	}
	slices.Sort(list)
	return list, nil
}

func (f *fixture) Rev(tag string) (string, error) {
	if _, ok := f.tags[tag]; !ok {
		return "", fmt.Errorf("no tag %s", tag)
	}
	return fakeRev(tag), nil
}

// Diff is not git's format: it lists the old lines with `-` and the new ones
// with `+`, which is enough to see that render quotes what Diff returns.
func (f *fixture) Diff(from, to, p string) ([]byte, error) {
	a, inFrom := f.tags[from][p]
	b, inTo := f.tags[to][p]
	if inFrom && inTo && bytes.Equal(a.Data, b.Data) {
		return nil, nil // like git: no output for a file that did not change
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s:%s\n+++ %s:%s\n", from, p, to, p)
	for _, side := range []struct{ tag, mark string }{{from, "-"}, {to, "+"}} {
		if file, ok := f.tags[side.tag][p]; ok {
			for _, line := range splitLines(file.Data) {
				sb.WriteString(side.mark + line + "\n")
			}
		}
	}
	return []byte(sb.String()), nil
}

func (f *fixture) Tags() ([]string, error) {
	var list []string
	for tag := range f.tags {
		list = append(list, tag)
	}
	slices.Sort(list)
	return list, nil
}

// problems runs verify and returns what it reports, one string per problem.
func (f *fixture) problems() []string {
	f.t.Helper()
	ps, err := verify(f.tree, f)
	if err != nil {
		f.t.Fatalf("verify: %v", err)
	}
	return errStrings(ps)
}

// text returns a file of the working tree.
func (f *fixture) text(p string) string {
	f.t.Helper()
	file, ok := f.tree[p]
	if !ok {
		f.t.Fatalf("the fixture tree has no %s", p)
	}
	return string(file.Data)
}

// setTree writes a file of the working tree.
func (f *fixture) setTree(p, content string) { f.tree[p] = &fstest.MapFile{Data: []byte(content)} }

// edit replaces old with new, once, in a file of the working tree.
func (f *fixture) edit(p, old, new string) {
	f.t.Helper()
	text := f.text(p)
	if strings.Count(text, old) != 1 {
		f.t.Fatalf("%s should contain %q exactly once", p, old)
	}
	f.setTree(p, strings.Replace(text, old, new, 1))
}

// lineOf returns the line (from 1) of the first line of a tree file that
// contains needle.
func (f *fixture) lineOf(p, needle string) int {
	f.t.Helper()
	for i, line := range strings.Split(f.text(p), "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	f.t.Fatalf("%s has no line with %q", p, needle)
	return 0
}

// setTag writes a file of a tag, which may not exist yet.
func (f *fixture) setTag(tag, p, content string) {
	if f.tags[tag] == nil {
		f.tags[tag] = fstest.MapFS{}
	}
	f.tags[tag][p] = &fstest.MapFile{Data: []byte(content)}
}

// editTag replaces old with new, once, in a file of a tag.
func (f *fixture) editTag(tag, p, old, new string) {
	f.t.Helper()
	data, err := f.Show(tag, p)
	if err != nil {
		f.t.Fatal(err)
	}
	if strings.Count(string(data), old) != 1 {
		f.t.Fatalf("%s:%s should contain %q exactly once", tag, p, old)
	}
	f.setTag(tag, p, strings.Replace(string(data), old, new, 1))
}

// copyTag makes the tag to with the files of the tag from.
func (f *fixture) copyTag(from, to string) {
	f.tags[to] = fstest.MapFS{}
	for p, file := range f.tags[from] {
		f.tags[to][p] = &fstest.MapFile{Data: file.Data}
	}
}

// want is the list of problems a test expects, built with fmt.Sprintf.
func want(format string, args ...any) string { return fmt.Sprintf(format, args...) }

func TestVerify(t *testing.T) {
	const ch1 = "book/ch01-taller.md"
	const ch2 = "book/ch02-lexer.md"

	// check runs the rows of a table: each one breaks the clean fixture in its
	// own way and says which problems verify has to report, in order.
	type row struct {
		name   string
		mutate func(f *fixture)
		want   func(f *fixture) []string
	}
	check := func(t *testing.T, rows []row) {
		t.Helper()
		for _, r := range rows {
			f := cleanFixture(t)
			r.mutate(f)
			got := f.problems()
			if w := r.want(f); !slices.Equal(got, w) {
				t.Errorf("%s:\n got %q\nwant %q", r.name, got, w)
			}
		}
	}

	t.Run("rule1_missing_region", func(t *testing.T) {
		check(t, []row{
			{"a region that is not in the tag",
				func(f *fixture) { f.edit(ch1, "{{listing ch01-s1 driver.main}}", "{{listing ch01-s1 driver.gone}}") },
				func(f *fixture) []string {
					return []string{want("%s:%d: region `driver.gone` is not in tag `ch01-s1`", ch1, f.lineOf(ch1, "driver.gone"))}
				}},
			{"a region that appears in two files",
				func(f *fixture) {
					f.setTag("ch01-s1", "internal/driver/other.go", string(lines("package driver", "", "// @book driver.main full", "var y = 2", "// @end")))
				},
				func(f *fixture) []string {
					return []string{
						want("%s:%d: region `driver.main` is defined 2 times in tag `ch01-s1`", ch1, f.lineOf(ch1, "driver.main")),
						"ch01-s1:internal/driver/other.go:3: duplicate region id `driver.main`, first defined at internal/driver/cli.go:4",
					}
				}},
			{"a file that is not in the tag",
				func(f *fixture) { f.edit(ch1, "{{file ch01-s1 go.mod}}", "{{file ch01-s1 nope.mod}}") },
				func(f *fixture) []string {
					return []string{want("%s:%d: `nope.mod` is not in tag `ch01-s1`", ch1, f.lineOf(ch1, "nope.mod"))}
				}},
			{"a diff of a file that is in neither tag",
				func(f *fixture) {
					f.edit(ch1, "{{diff ch01-s1..ch01-s2 internal/driver/cli.go}}", "{{diff ch01-s1..ch01-s2 nope.go}}")
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: `nope.go` is in neither tag `ch01-s1` nor tag `ch01-s2`", ch1, f.lineOf(ch1, "nope.go"))}
				}},
			{"a diff with a tag that does not exist",
				func(f *fixture) {
					f.edit(ch1, "{{diff ch01-s1..ch01-s2 internal/driver/cli.go}}", "{{diff ch01-s1..ch01-s9 internal/driver/cli.go}}")
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: unknown tag `ch01-s9`", ch1, f.lineOf(ch1, "ch01-s9"))}
				}},
			{"a reftest of a card that is not there",
				func(f *fixture) { f.edit(ch1, "{{reftest T01.01}}", "{{reftest T01.09}}") },
				func(f *fixture) []string {
					return []string{want("%s:%d: there is no card T01.09 in the cards of chapter 01", ch1, f.lineOf(ch1, "T01.09"))}
				}},
			{"a reftest of a golden card",
				func(f *fixture) { f.edit(ch1, "{{reftest T01.01}}", "{{reftest T01.03}}") },
				func(f *fixture) []string {
					return []string{want("%s:%d: card T01.03 is a `golden` card: {{reftest}} shows the Go test of a unit, prop, example or bench card", ch1, f.lineOf(ch1, "T01.03"))}
				}},
			{"a reftest whose test is not in the file",
				func(f *fixture) {
					f.editTag("ch01", "internal/driver/cli_test.go", "func TestCLI(", "func TestOther(")
				},
				func(f *fixture) []string {
					// Rule 4 also notices that the card's function is gone.
					return []string{
						want("%s:%d: test `TestCLI/version_prints_version` of card T01.01 is not in `internal/driver/cli_test.go` (tag `ch01`)", ch1, f.lineOf(ch1, "{{reftest")),
						want("book/tests/ch01.tests:%d: card T01.01: `func TestCLI(` is not in `internal/driver/cli_test.go` (tag `ch01`)", f.lineOf("book/tests/ch01.tests", "[T01.01]")),
						want("book/tests/ch01.tests:%d: card T01.02: `func TestCLI(` is not in `internal/driver/cli_test.go` (tag `ch01`)", f.lineOf("book/tests/ch01.tests", "[T01.02]")),
					}
				}},
			{"a directive with text on both sides",
				func(f *fixture) { f.edit(ch2, "{{cards}}", "Mira {{cards}} aquí.") },
				func(f *fixture) []string {
					return []string{want("%s:%d: a directive has to be alone on its line", ch2, f.lineOf(ch2, "Mira"))}
				}},
			{"text before a directive that ends the line",
				func(f *fixture) { f.edit(ch2, "{{cards}}", "Mira {{cards}}") },
				func(f *fixture) []string {
					return []string{want("%s:%d: a directive has to be alone on its line", ch2, f.lineOf(ch2, "Mira"))}
				}},
			{"text after a directive",
				func(f *fixture) { f.edit(ch2, "{{cards}}", "{{cards}} y más") },
				func(f *fixture) []string {
					return []string{want("%s:%d: a directive has to be alone on its line", ch2, f.lineOf(ch2, "y más"))}
				}},
			{"two directives on one line",
				func(f *fixture) { f.edit(ch2, "{{cards}}", "{{cards}} {{cards}}") },
				func(f *fixture) []string {
					return []string{want("%s:%d: a directive has to be alone on its line", ch2, f.lineOf(ch2, "{{cards}} {{cards}}"))}
				}},
			{"an unknown directive",
				func(f *fixture) { f.edit(ch2, "{{file ch02-s1 internal/base/source.go}}", "{{frob x}}") },
				func(f *fixture) []string {
					return []string{want("%s:%d: unknown directive `frob`: want file, diff, listing, cards or reftest", ch2, f.lineOf(ch2, "frob"))}
				}},
			{"a directive inside a code fence is only text",
				func(f *fixture) {
					f.edit(ch2, "{{file ch02-s1 internal/base/source.go}}", "```text\n{{file ch02-s1 nope.go}}\n```")
				},
				func(f *fixture) []string { return nil }},
		})
	})

	t.Run("rule2_unbalanced_marker", func(t *testing.T) {
		const open = "internal/driver/extra.go"
		check(t, []row{
			{"a @book without its @end in a cited tag",
				func(f *fixture) {
					f.setTag("ch01-s1", open, string(lines("package driver", "", "// @book extra.open full", "var x = 1")))
				},
				func(f *fixture) []string {
					return []string{"ch01-s1:" + open + ":3: region `extra.open` is never closed: it needs an @end line"}
				}},
			{"an @end without its @book",
				func(f *fixture) {
					f.setTag("ch01-s1", open, string(lines("package driver", "// @end")))
				},
				func(f *fixture) []string {
					return []string{"ch01-s1:" + open + ":2: @end without a matching @book"}
				}},
			{"a region id used in two files of a cited tag",
				func(f *fixture) {
					f.setTag("ch01-s2", "internal/driver/a.go", string(lines("package driver", "// @book twice.one full", "var a = 1", "// @end")))
					f.setTag("ch01-s2", "internal/driver/b.go", string(lines("package driver", "// @book twice.one full", "var b = 1", "// @end")))
				},
				func(f *fixture) []string {
					return []string{"ch01-s2:internal/driver/b.go:2: duplicate region id `twice.one`, first defined at internal/driver/a.go:2"}
				}},
			{"a tag the prose does not cite is not scanned",
				func(f *fixture) {
					f.setTag("ch02", open, string(lines("package driver", "", "// @book extra.open full", "var x = 1")))
				},
				func(f *fixture) []string { return nil }},
			{"files under testdata are not scanned",
				func(f *fixture) {
					f.setTag("ch01-s1", "cmd/bookx/testdata/x.go", string(lines("package x", "// @book never.closed full")))
				},
				func(f *fixture) []string { return nil }},
			{"markers in prose and docs are not scanned",
				func(f *fixture) {
					f.setTag("ch01-s1", "docs/markers.md", string(lines("// @book never.closed full")))
				},
				func(f *fixture) []string { return nil }},
		})
	})

	t.Run("rule3_gap_in_ids", func(t *testing.T) {
		check(t, []row{
			{"T02.02 then T02.04",
				func(f *fixture) { f.edit("book/tests/ch02.tests", "[T02.03]", "[T02.04]") },
				func(f *fixture) []string {
					return []string{want("book/tests/ch02.tests:%d: card ids must be consecutive: expected T02.03, found T02.04", f.lineOf("book/tests/ch02.tests", "[T02.04]"))}
				}},
			{"a repeated id",
				func(f *fixture) { f.edit("book/tests/ch02.tests", "[T02.03]", "[T02.02]") },
				func(f *fixture) []string {
					return []string{want("book/tests/ch02.tests:%d: card ids must be consecutive: expected T02.03, found T02.02", f.lineOf("book/tests/ch02.tests", "[T02.02] interner.example"))}
				}},
			{"a card of another chapter",
				func(f *fixture) { f.edit("book/tests/ch02.tests", "[T02.03]", "[T03.03]") },
				func(f *fixture) []string {
					return []string{want("book/tests/ch02.tests:%d: card T03.03: its number must be 02, like the file `ch02.tests`", f.lineOf("book/tests/ch02.tests", "[T03.03]"))}
				}},
		})
	})

	t.Run("rule3_prose_without_cards", func(t *testing.T) {
		check(t, []row{
			{"a chapter with no {{cards}}",
				func(f *fixture) { f.edit(ch2, "{{cards}}\n\n", "") },
				func(f *fixture) []string { return []string{ch2 + ":1: the chapter has no {{cards}} directive"} }},
			{"a {{cards}} that is only an example in a code span",
				func(f *fixture) { f.edit(ch2, "{{cards}}", "`{{cards}}`") },
				func(f *fixture) []string { return []string{ch2 + ":1: the chapter has no {{cards}} directive"} }},
		})
	})

	t.Run("rule4_ref_not_in_file", func(t *testing.T) {
		// A card and its test are renamed together in the card file and in the
		// list, so that only rule 4 has something to say.
		rename := func(old, new string) func(f *fixture) {
			return func(f *fixture) {
				f.edit("book/tests/ch02.tests", "ref:     "+old, "ref:     "+new)
				f.edit("book/tests/ch02.list", " "+old+"\n", " "+new+"\n")
			}
		}
		const cardsPath = "book/tests/ch02.tests"
		check(t, []row{
			{"a subtest whose literal is not in the file",
				rename("TestLexer/pipe_continues_line", "TestLexer/gone"),
				func(f *fixture) []string {
					return []string{want("%s:%d: card T02.01: the subtest name \"gone\" is not in `internal/syntax/lexer_test.go` (tag `ch02`)", cardsPath, f.lineOf(cardsPath, "[T02.01]"))}
				}},
			{"a function that is not in the file",
				rename("TestLexer/pipe_continues_line", "TestMissing/pipe_continues_line"),
				func(f *fixture) []string {
					return []string{want("%s:%d: card T02.01: `func TestMissing(` is not in `internal/syntax/lexer_test.go` (tag `ch02`)", cardsPath, f.lineOf(cardsPath, "[T02.01]"))}
				}},
			{"a name that only starts like a function of the file",
				rename("TestLexer/pipe_continues_line", "TestLex/pipe_continues_line"),
				func(f *fixture) []string {
					return []string{want("%s:%d: card T02.01: `func TestLex(` is not in `internal/syntax/lexer_test.go` (tag `ch02`)", cardsPath, f.lineOf(cardsPath, "[T02.01]"))}
				}},
			{"a fuzz target that is not in the file",
				rename("FuzzLex", "FuzzGone"),
				func(f *fixture) []string {
					return []string{want("%s:%d: card T02.02: `func FuzzGone(` is not in `internal/syntax/lexer_test.go` (tag `ch02`)", cardsPath, f.lineOf(cardsPath, "[T02.02]"))}
				}},
			{"an example that is not in the file",
				rename("ExampleInterner", "ExampleGone"),
				func(f *fixture) []string {
					return []string{want("%s:%d: card T02.03: `func ExampleGone(` is not in `internal/base/interner_test.go` (tag `ch02`)", cardsPath, f.lineOf(cardsPath, "[T02.03]"))}
				}},
			{"a file that is not in the tag",
				func(f *fixture) {
					f.edit("book/tests/ch02.tests", "file: internal/base/interner_test.go", "file: internal/base/nope_test.go")
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: card T02.03: file `internal/base/nope_test.go` does not exist in tag `ch02`", cardsPath, f.lineOf(cardsPath, "[T02.03]"))}
				}},
			{"a unit card whose ref names a fuzz target",
				rename("TestLexer/pipe_continues_line", "FuzzLex"),
				func(f *fixture) []string {
					return []string{
						want("%s:%d: card T02.01: ref `FuzzLex` of a `unit` card must start with `Test`", cardsPath, f.lineOf(cardsPath, "[T02.01]")),
					}
				}},
			{"a golden file with another id in its header",
				func(f *fixture) {
					f.editTag("ch01", "tests/golden/ch01/hello.ki", "// test: T01.03", "// test: T01.09")
				},
				func(f *fixture) []string {
					return []string{want("book/tests/ch01.tests:%d: card T01.03: `tests/golden/ch01/hello.ki` says `// test: T01.09`, not `T01.03`", f.lineOf("book/tests/ch01.tests", "[T01.03]"))}
				}},
			{"a golden file with no header line",
				func(f *fixture) { f.setTag("ch01", "tests/golden/ch01/hello.ki", "// run: run\n\nfn main() {}\n") },
				func(f *fixture) []string {
					return []string{want("book/tests/ch01.tests:%d: card T01.03: `tests/golden/ch01/hello.ki` has no `// test: T01.03` header line", f.lineOf("book/tests/ch01.tests", "[T01.03]"))}
				}},
			{"a golden file that is not in the tag",
				func(f *fixture) { delete(f.tags["ch01"], "tests/golden/ch01/hello.ki") },
				func(f *fixture) []string {
					return []string{want("book/tests/ch01.tests:%d: card T01.03: file `tests/golden/ch01/hello.ki` does not exist in tag `ch01`", f.lineOf("book/tests/ch01.tests", "[T01.03]"))}
				}},
		})
	})

	// Two things rule 5 must not report are part of the clean fixture, so every
	// row of the next two tests would fail if they were: ch02.list repeats the
	// tests of ch01.list, which need no second card, and the benchmark card
	// T01.04 has no line in ch01.list.
	t.Run("rule5_uncarded_test", func(t *testing.T) {
		const syntax = "github.com/aldezex/kilang/internal/syntax"
		const listPath = "book/tests/ch02.list"
		addToList := func(entry string) func(f *fixture) {
			return func(f *fixture) { f.setTree(listPath, f.text(listPath)+entry+"\n") }
		}
		newLine := func(f *fixture) int { return len(strings.Split(strings.TrimSuffix(f.text(listPath), "\n"), "\n")) }
		check(t, []row{
			{"a new test with no card",
				addToList(syntax + " TestLexer/extra"),
				func(f *fixture) []string {
					return []string{want("%s:%d: test `TestLexer/extra` of %s is new in the chapter and has no card with ref `TestLexer/extra`", listPath, newLine(f), syntax)}
				}},
			{"a new golden test with no card",
				addToList("github.com/aldezex/kilang/tests TestKi/golden/ch02/lex"),
				func(f *fixture) []string {
					return []string{want("%s:%d: test `TestKi/golden/ch02/lex` of github.com/aldezex/kilang/tests is new in the chapter and has no card with ref `tests/golden/ch02/lex.ki`", listPath, newLine(f))}
				}},
			{"a new test in a file marked not-in-book needs no card",
				func(f *fixture) {
					f.setTag("ch02", "internal/syntax/helper_test.go", string(lines("// bookx: not-in-book", "", "package syntax", "", "func TestHelperLexer() {}")))
					addToList(syntax + " TestHelperLexer")(f)
				},
				func(f *fixture) []string { return nil }},
			{"a new test in a file without the marker needs a card",
				func(f *fixture) {
					f.setTag("ch02", "internal/syntax/helper_test.go", string(lines("package syntax", "", "func TestHelperLexer() {}")))
					addToList(syntax + " TestHelperLexer")(f)
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: test `TestHelperLexer` of %s is new in the chapter and has no card with ref `TestHelperLexer`", listPath, newLine(f), syntax)}
				}},
			{"the marker of another file does not excuse a test",
				func(f *fixture) {
					f.setTag("ch02", "internal/syntax/helper_test.go", string(lines("// bookx: not-in-book", "", "package syntax", "", "func TestHelperOther() {}")))
					f.setTag("ch02", "internal/syntax/lexer2_test.go", string(lines("package syntax", "", "func TestHelperLexer() {}")))
					addToList(syntax + " TestHelperLexer")(f)
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: test `TestHelperLexer` of %s is new in the chapter and has no card with ref `TestHelperLexer`", listPath, newLine(f), syntax)}
				}},
			{"the exemption of ch01 needs the marker",
				func(f *fixture) {
					f.editTag("ch01", "internal/driver/helper_test.go", "// bookx: not-in-book", "// an ordinary comment")
				},
				func(f *fixture) []string {
					return []string{"book/tests/ch01.list:3: test `TestHelperProcess` of github.com/aldezex/kilang/internal/driver is new in the chapter and has no card with ref `TestHelperProcess`"}
				}},
		})
	})

	t.Run("rule5_card_without_test", func(t *testing.T) {
		dropFromList := func(p, entry string) func(f *fixture) {
			return func(f *fixture) { f.edit(p, entry+"\n", "") }
		}
		check(t, []row{
			{"a unit card whose test is not in the list",
				dropFromList("book/tests/ch02.list", "github.com/aldezex/kilang/internal/syntax TestLexer/pipe_continues_line"),
				func(f *fixture) []string {
					return []string{want("book/tests/ch02.tests:%d: card T02.01: its test `TestLexer/pipe_continues_line` is not in book/tests/ch02.list", f.lineOf("book/tests/ch02.tests", "[T02.01]"))}
				}},
			{"a prop card whose fuzz target is not in the list",
				dropFromList("book/tests/ch02.list", "github.com/aldezex/kilang/internal/syntax FuzzLex"),
				func(f *fixture) []string {
					return []string{want("book/tests/ch02.tests:%d: card T02.02: its test `FuzzLex` is not in book/tests/ch02.list", f.lineOf("book/tests/ch02.tests", "[T02.02]"))}
				}},
			{"a golden card whose test is not in the list",
				func(f *fixture) {
					entry := "github.com/aldezex/kilang/tests TestKi/golden/ch01/hello"
					f.edit("book/tests/ch01.list", entry+"\n", "")
					f.edit("book/tests/ch02.list", entry+"\n", "")
				},
				func(f *fixture) []string {
					return []string{want("book/tests/ch01.tests:%d: card T01.03: its test `tests/golden/ch01/hello.ki` is not in book/tests/ch01.list", f.lineOf("book/tests/ch01.tests", "[T01.03]"))}
				}},
		})
	})

	t.Run("rule6_unverified_tag", func(t *testing.T) {
		const verified = "book/verified-tags.txt"
		check(t, []row{
			{"a tag cited several times is reported once, at its first citation",
				func(f *fixture) {
					f.edit(verified, "ch01-s1 "+fakeRev("ch01-s1")+" 2026-10-26 go1.27.3\n", "")
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: tag `ch01-s1` is not in %s: run ci/check-tags.sh 'ch01-s1'", ch1, f.lineOf(ch1, "{{file ch01-s1"), verified)}
				}},
			{"a tag cited only by a diff",
				func(f *fixture) {
					f.edit(verified, "ch01-s2 "+fakeRev("ch01-s2")+" 2026-10-26 go1.27.3\n", "")
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: tag `ch01-s2` is not in %s: run ci/check-tags.sh 'ch01-s2'", ch1, f.lineOf(ch1, "{{diff"), verified)}
				}},
			{"a tag that was moved after it was verified",
				func(f *fixture) {
					f.edit(verified, "ch02-s1 "+fakeRev("ch02-s1"), "ch02-s1 "+strings.Repeat("0", 40))
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: tag `ch02-s1` points to %s, but %s verified %s: run ci/check-tags.sh 'ch02-s1'", ch2, f.lineOf(ch2, "{{file ch02-s1"), fakeRev("ch02-s1"), verified, strings.Repeat("0", 40))}
				}},
			{"a tag the prose does not cite needs no line",
				func(f *fixture) { f.edit(verified, "ch02 "+fakeRev("ch02")+" 2026-10-26 go1.27.3\n", "") },
				func(f *fixture) []string { return nil }},
			{"a file that is missing leaves every cited tag unverified",
				func(f *fixture) { delete(f.tree, verified) },
				func(f *fixture) []string {
					return []string{
						want("%s:%d: tag `ch01-s1` is not in %s: run ci/check-tags.sh 'ch01-s1'", ch1, f.lineOf(ch1, "{{file ch01-s1"), verified),
						want("%s:%d: tag `ch01-s2` is not in %s: run ci/check-tags.sh 'ch01-s2'", ch1, f.lineOf(ch1, "{{diff"), verified),
						want("%s:%d: tag `ch02-s1` is not in %s: run ci/check-tags.sh 'ch02-s1'", ch2, f.lineOf(ch2, "{{file ch02-s1"), verified),
					}
				}},
			{"a malformed line is reported with its number",
				func(f *fixture) { f.edit(verified, "2026-10-26 go1.27.3\nch01 ", "2026-10-26\nch01 ") },
				func(f *fixture) []string {
					// The file is unusable, so the three cited tags are unverified too.
					return []string{
						want("%s:%d: tag `ch01-s1` is not in %s: run ci/check-tags.sh 'ch01-s1'", ch1, f.lineOf(ch1, "{{file ch01-s1"), verified),
						want("%s:%d: tag `ch01-s2` is not in %s: run ci/check-tags.sh 'ch01-s2'", ch1, f.lineOf(ch1, "{{diff"), verified),
						want("%s:%d: tag `ch02-s1` is not in %s: run ci/check-tags.sh 'ch02-s1'", ch2, f.lineOf(ch2, "{{file ch02-s1"), verified),
						verified + ":2: want `<tag> <sha40> <YYYY-MM-DD> <goversion>`, found 3 fields",
					}
				}},
		})
	})

	t.Run("rule6_chapter_tag_without_prose", func(t *testing.T) {
		check(t, []row{
			{"the tag has no prose",
				func(f *fixture) { delete(f.tags["ch02"], "book/ch02-lexer.md") },
				func(f *fixture) []string {
					return []string{"book/ch02-*.md:1: tag `ch02` has no prose for the chapter"}
				}},
			{"the tag has prose of another chapter only",
				func(f *fixture) {
					delete(f.tags["ch02"], "book/ch02-lexer.md")
					f.setTag("ch02", "book/ch03-other.md", "x\n")
				},
				func(f *fixture) []string {
					return []string{"book/ch02-*.md:1: tag `ch02` has no prose for the chapter"}
				}},
			{"the tag has no cards",
				func(f *fixture) { delete(f.tags["ch02"], "book/tests/ch02.tests") },
				func(f *fixture) []string {
					return []string{"book/tests/ch02.tests:1: tag `ch02` has no cards for the chapter"}
				}},
			{"the working tree has no list of tests",
				func(f *fixture) { delete(f.tree, "book/tests/ch02.list") },
				func(f *fixture) []string {
					return []string{"book/tests/ch02.list:1: the working tree has no list of tests for the chapter; write it with ci/check-tags.sh --list-as ch02 <last session tag>"}
				}},
			{"the working tree has no cards",
				func(f *fixture) { delete(f.tree, "book/tests/ch02.tests") },
				func(f *fixture) []string {
					return []string{"book/tests/ch02.tests:1: the working tree has no cards for the chapter"}
				}},
		})
	})

	t.Run("rule7_nested_container", func(t *testing.T) {
		check(t, []row{
			{":::solution inside an open :::exercise",
				func(f *fixture) {
					f.edit(ch1, "Cambia el código de salida.\n:::\n\n:::solution", "Cambia el código de salida.\n:::solution")
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: nested container :::solution", ch1, f.lineOf(ch1, ":::solution"))}
				}},
			{"a container with arguments, nested",
				func(f *fixture) {
					f.edit(ch1, "Cambia el código de salida.\n:::", "Cambia el código de salida.\n:::from Rust\nTexto.\n:::")
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: nested container :::from", ch1, f.lineOf(ch1, ":::from Rust"))}
				}},
			{"a container that is never closed",
				func(f *fixture) { f.edit(ch1, "Devuelve 1.\n:::\n", "Devuelve 1.\n") },
				func(f *fixture) []string {
					return []string{want("%s:%d: unclosed container", ch1, f.lineOf(ch1, ":::solution"))}
				}},
			{"a container line inside a code fence does not count",
				func(f *fixture) {
					f.edit(ch1, "Cambia el código de salida.\n", "Cambia el código de salida.\n```text\n:::solution\n```\n")
				},
				func(f *fixture) []string { return nil }},
			{"containers in sequence are fine",
				func(f *fixture) {},
				func(f *fixture) []string { return nil }},
		})
	})

	t.Run("pending_chapter_uses_last_session_tag", func(t *testing.T) {
		f := cleanFixture(t)
		// ch02 has prose and a list but no chapter tag. Its session tags are
		// ch02-s1, ch02-s2 and ch02-s10. Only the last one by number, s10 and
		// not s2 as a sort of the names would say, has the tests the cards
		// name, so rule 4 passes only if it reads that one.
		const lexerTest = "internal/syntax/lexer_test.go"
		f.copyTag("ch02", "ch02-s10")
		f.copyTag("ch02", "ch02-s2")
		f.setTag("ch02-s2", lexerTest, "package syntax\n")
		f.setTag("ch02-s1", lexerTest, "package syntax\n")
		delete(f.tags, "ch02")
		if got := f.problems(); len(got) != 0 {
			t.Errorf("a pending chapter should read its code at ch02-s10, but verify says:\n%s", strings.Join(got, "\n"))
		}

		// Without the tests in ch02-s10 rule 4 fails on the same chapter, and
		// says it read that tag: the check above does not pass for lack of looking.
		f.setTag("ch02-s10", lexerTest, "package syntax\n")
		var cardsPath = "book/tests/ch02.tests"
		if got, w := f.problems(), want("%s:%d: card T02.01: `func TestLexer(` is not in `internal/syntax/lexer_test.go` (tag `ch02-s10`)", cardsPath, f.lineOf(cardsPath, "[T02.01]")); !slices.Contains(got, w) {
			t.Errorf("verify should name tag ch02-s10 in %q, but says %q", w, got)
		}

		// With no tag of the chapter's code at all there is nowhere to read from.
		for _, tag := range []string{"ch02-s1", "ch02-s2", "ch02-s10"} {
			delete(f.tags, tag)
		}
		w := "book/tests/ch02.tests:1: chapter 02 has no tag `ch02` and no session tag `ch02-sK` to read its code from"
		if got := f.problems(); !slices.Contains(got, w) {
			t.Errorf("a pending chapter with no session tag: want %q among %q", w, got)
		}
	})

	t.Run("prose_must_not_cite_chapter_tag", func(t *testing.T) {
		check(t, []row{
			{"{{file ch01 go.mod}}",
				func(f *fixture) { f.edit(ch1, "{{file ch01-s1 go.mod}}", "{{file ch01 go.mod}}") },
				func(f *fixture) []string {
					return []string{want("%s:%d: the prose cites the chapter tag `ch01`, which does not exist while the chapter is written: cite a session tag such as `ch01-s1`", ch1, f.lineOf(ch1, "{{file ch01 "))}
				}},
			{"{{diff}} with a chapter tag at the end",
				func(f *fixture) {
					f.edit(ch1, "{{diff ch01-s1..ch01-s2 internal/driver/cli.go}}", "{{diff ch01-s1..ch01 internal/driver/cli.go}}")
				},
				func(f *fixture) []string {
					return []string{want("%s:%d: the prose cites the chapter tag `ch01`, which does not exist while the chapter is written: cite a session tag such as `ch01-s1`", ch1, f.lineOf(ch1, "{{diff"))}
				}},
			{"{{listing}} of a chapter tag",
				func(f *fixture) { f.edit(ch1, "{{listing ch01-s1 driver.main}}", "{{listing ch01 driver.main}}") },
				func(f *fixture) []string {
					return []string{want("%s:%d: the prose cites the chapter tag `ch01`, which does not exist while the chapter is written: cite a session tag such as `ch01-s1`", ch1, f.lineOf(ch1, "{{listing ch01 "))}
				}},
		})
	})
}
