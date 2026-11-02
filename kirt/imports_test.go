package kirt_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The tests of this file are the rules of spec 7.2 and 8.7 that say what may
// import what, checked on the imports of the source and not on its text, so that
// a comment that names a package does not count (this one names "unsafe").
//
//   - kirt imports the standard library and its own packages, and nothing else.
//   - The oracle, internal/interp, imports from kirt only kirt/out, kirt/splitmix
//     and kirt/fmtnum.
//   - unsafe is imported nowhere in the module but under kirt/heap/, which does not
//     exist yet.
//
// Each rule is a function over the imports that importsUnder collects. The test
// of a rule runs it on the repository, which must have no problem, and on small
// trees made for the purpose, each with the one mistake that the rule exists to
// catch, which must be reported: a rule that finds nothing in the repository
// proves nothing until it is seen to find something.

const (
	// repoRoot is the root of the repository seen from this directory: go test
	// runs the tests of a package with the package's directory as the working
	// directory.
	repoRoot   = ".."
	modulePath = "github.com/aldezex/kilang"
)

// skipAtRoot lists the directories of the repository root that hold no source of
// the module: the output of builds, the files of git, and the copies of the tree
// that the tools work in (.claude/worktrees) and keep their notes in
// (.superpowers).
var skipAtRoot = []string{"build", ".git", ".claude", ".superpowers"}

// importOf is one import of one Go file: the file, as a path from the root with
// forward slashes, and the import path with its quotes removed.
type importOf struct{ file, path string }

// importsUnder returns the imports of every .go file under root/dir, tests
// included, in the order of the files, and how many files it read. It reads only
// the import declarations (parser.ImportsOnly). It does not enter the directories
// of skipAtRoot.
func importsUnder(root, dir string) (imports []importOf, files int, err error) {
	err = filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if slices.Contains(skipAtRoot, rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		files++
		for _, spec := range file.Imports {
			p, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			imports = append(imports, importOf{rel, p})
		}
		return nil
	})
	return imports, files, err
}

// kirtProblems returns a description of each import of the files under kirt/
// that is neither of the standard library nor of a package of kirt. A path is of
// the standard library when its first element has no dot (a module path begins
// with a domain), and of kirt when it begins with the module path and "/kirt/":
// the slash makes "kirtx" not a package of kirt.
func kirtProblems(imports []importOf) []string {
	var problems []string
	for _, im := range imports {
		first, _, _ := strings.Cut(im.path, "/")
		if !strings.Contains(first, ".") || strings.HasPrefix(im.path, modulePath+"/kirt/") {
			continue
		}
		problems = append(problems, im.file+" imports "+strconv.Quote(im.path)+", which is neither the standard library nor a package of kirt")
	}
	return problems
}

// interpAllowed are the packages of kirt that the oracle may import.
var interpAllowed = []string{modulePath + "/kirt/out", modulePath + "/kirt/splitmix", modulePath + "/kirt/fmtnum"}

// interpProblems returns a description of each import of the files under
// internal/interp/ that is a package of kirt (or kirt itself) and is not one of
// interpAllowed. What it imports from elsewhere in the module is not its concern.
func interpProblems(imports []importOf) []string {
	var problems []string
	for _, im := range imports {
		isKirt := im.path == modulePath+"/kirt" || strings.HasPrefix(im.path, modulePath+"/kirt/")
		if !isKirt || slices.Contains(interpAllowed, im.path) {
			continue
		}
		problems = append(problems, im.file+" imports "+strconv.Quote(im.path)+", and the oracle may import only kirt/out, kirt/splitmix and kirt/fmtnum from kirt")
	}
	return problems
}

// unsafeProblems returns a description of each import of "unsafe" in a file that
// is not under kirt/heap/. The slash ends the name of the directory, so
// kirt/heapx/ and a file kirt/heap.go are not under it.
func unsafeProblems(imports []importOf) []string {
	var problems []string
	for _, im := range imports {
		if im.path != "unsafe" || strings.HasPrefix(im.file, "kirt/heap/") {
			continue
		}
		problems = append(problems, im.file+" imports \"unsafe\", which only kirt/heap/ may")
	}
	return problems
}

// writeTree creates the files of a tree in a new temporary directory and returns
// the directory. files maps a path from the root, with forward slashes, to the
// text of the file.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("cannot create the directory of %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatalf("cannot write %s: %v", name, err)
		}
	}
	return root
}

// goFile returns the text of a Go file that imports the packages paths, or none.
func goFile(paths ...string) string {
	text := "package p\n"
	for _, p := range paths {
		text += "import " + strconv.Quote(p) + "\n"
	}
	return text
}

// checkRule runs rule on the imports of root/dir and compares what it returns
// with want, which may be nil. It fails the test if no .go file is read, because
// a root that is wrong would make every rule pass.
func checkRule(t *testing.T, what, root, dir string, rule func([]importOf) []string, want []string) {
	t.Helper()
	imports, files, err := importsUnder(root, dir)
	if err != nil {
		t.Fatalf("%s: cannot read the imports under %s: %v", what, dir, err)
	}
	if files == 0 {
		t.Fatalf("%s: found no .go file under %s: the rule would pass without checking anything", what, dir)
	}
	if got := rule(imports); !slices.Equal(got, want) {
		t.Errorf("%s: problems %q, want %q", what, got, want)
	}
}

func TestImports(t *testing.T) {
	t.Run("kirt_only_std_and_kirt", func(t *testing.T) {
		checkRule(t, "the repository", repoRoot, "kirt", kirtProblems, nil)

		const out = modulePath + "/kirt/out"
		rows := []struct {
			name  string
			files map[string]string
			want  []string
		}{
			{"std_and_kirt", map[string]string{"kirt/a/a.go": goFile("fmt", "net/http", "sync/atomic", out)}, nil},
			// The standard library is told by the first element of the path.
			{"dot_only_after_the_first_element", map[string]string{"kirt/a/a.go": goFile("vendor/lib.v2")}, nil},
			// Every import of a file is read, not only the first.
			{"violation_is_the_second_import", map[string]string{"kirt/a/a.go": goFile("fmt", "golang.org/x/sys")},
				[]string{`kirt/a/a.go imports "golang.org/x/sys", which is neither the standard library nor a package of kirt`}},
			{"module_package_outside_kirt", map[string]string{"kirt/a/a.go": goFile(modulePath + "/internal/base")},
				[]string{`kirt/a/a.go imports "` + modulePath + `/internal/base", which is neither the standard library nor a package of kirt`}},
			{"the_module_root", map[string]string{"kirt/a/a.go": goFile(modulePath)},
				[]string{`kirt/a/a.go imports "` + modulePath + `", which is neither the standard library nor a package of kirt`}},
			{"name_that_only_starts_with_kirt", map[string]string{"kirt/a/a.go": goFile(modulePath + "/kirtx/out")},
				[]string{`kirt/a/a.go imports "` + modulePath + `/kirtx/out", which is neither the standard library nor a package of kirt`}},
			{"third_party", map[string]string{"kirt/a/a.go": goFile("golang.org/x/sys/unix")},
				[]string{`kirt/a/a.go imports "golang.org/x/sys/unix", which is neither the standard library nor a package of kirt`}},
			// A test file counts, and so does a directory several levels down.
			{"third_party_in_a_test_deep_down", map[string]string{"kirt/a/b/c/c_test.go": goFile("golang.org/x/text", "testing")},
				[]string{`kirt/a/b/c/c_test.go imports "golang.org/x/text", which is neither the standard library nor a package of kirt`}},
			{"two_files_in_order", map[string]string{"kirt/a/a.go": goFile("example.com/one"), "kirt/b/b.go": goFile("example.com/two")},
				[]string{
					`kirt/a/a.go imports "example.com/one", which is neither the standard library nor a package of kirt`,
					`kirt/b/b.go imports "example.com/two", which is neither the standard library nor a package of kirt`,
				}},
		}
		for _, row := range rows {
			checkRule(t, row.name, writeTree(t, row.files), "kirt", kirtProblems, row.want)
		}
	})

	t.Run("interp_only_whitelisted_kirt", func(t *testing.T) {
		checkRule(t, "the repository", repoRoot, "internal/interp", interpProblems, nil)

		const kirt = modulePath + "/kirt"
		problem := func(path string) []string {
			return []string{`internal/interp/a.go imports "` + path + `", and the oracle may import only kirt/out, kirt/splitmix and kirt/fmtnum from kirt`}
		}
		rows := []struct {
			name, imports string
			want          []string
		}{
			{"the_three_that_are_allowed", kirt + "/out " + kirt + "/splitmix " + kirt + "/fmtnum", nil},
			{"std_and_the_rest_of_the_module", "fmt " + modulePath + "/internal/core " + modulePath + "/internal/base", nil},
			{"second_import_of_the_file", "fmt " + kirt + "/heap", problem(kirt + "/heap")},
			{"heap", kirt + "/heap", problem(kirt + "/heap")},
			{"poll", kirt + "/poll", problem(kirt + "/poll")},
			{"kirt_itself", kirt, problem(kirt)},
			{"below_an_allowed_package", kirt + "/out/sub", problem(kirt + "/out/sub")},
		}
		for _, row := range rows {
			root := writeTree(t, map[string]string{"internal/interp/a.go": goFile(strings.Fields(row.imports)...)})
			checkRule(t, row.name, root, "internal/interp", interpProblems, row.want)
		}

		// Only the oracle is held to the rule: another package may import any of kirt.
		root := writeTree(t, map[string]string{
			"internal/interp/a.go": goFile("fmt"),
			"internal/driver/d.go": goFile(kirt + "/heap"),
		})
		checkRule(t, "another_package", root, "internal/interp", interpProblems, nil)
	})

	t.Run("unsafe_only_in_kirt_heap", func(t *testing.T) {
		checkRule(t, "the repository", repoRoot, ".", unsafeProblems, nil)

		problem := func(file string) []string { return []string{file + ` imports "unsafe", which only kirt/heap/ may`} }
		rows := []struct {
			name  string
			files map[string]string
			want  []string
		}{
			{"outside_kirt", map[string]string{"internal/x/x.go": goFile("unsafe")}, problem("internal/x/x.go")},
			{"in_kirt_but_not_heap", map[string]string{"kirt/out/o.go": goFile("unsafe")}, problem("kirt/out/o.go")},
			{"under_kirt_heap", map[string]string{"kirt/heap/h.go": goFile("unsafe")}, nil},
			{"test_under_kirt_heap", map[string]string{"kirt/heap/h_test.go": goFile("unsafe")}, nil},
			{"deep_under_kirt_heap", map[string]string{"kirt/heap/a/b/h.go": goFile("unsafe")}, nil},
			{"name_that_only_starts_with_heap", map[string]string{"kirt/heapx/h.go": goFile("unsafe")}, problem("kirt/heapx/h.go")},
			{"file_called_heap", map[string]string{"kirt/heap.go": goFile("unsafe")}, problem("kirt/heap.go")},
			// A renamed import is still the package unsafe.
			{"renamed_import", map[string]string{"cmd/ki/m.go": "package main\nimport u \"unsafe\"\n"}, problem("cmd/ki/m.go")},
			// A mention is not an import.
			{"comment_and_string", map[string]string{"internal/x/x.go": "package x\n// This does not use unsafe.\nvar s = \"unsafe\"\n"}, nil},
			// The directories of skipAtRoot are not the module's source...
			{"skipped_directories", map[string]string{
				"build/x/x.go":                      goFile("unsafe"),
				".git/x.go":                         goFile("unsafe"),
				".claude/worktrees/w/internal/x.go": goFile("unsafe"),
				".superpowers/x.go":                 goFile("unsafe"),
				"ok.go":                             goFile("fmt"),
			}, nil},
			// ...but only at the root: a directory of that name deeper in the tree is.
			{"build_below_the_root", map[string]string{"internal/build/x.go": goFile("unsafe")}, problem("internal/build/x.go")},
			{"two_files_in_order", map[string]string{"a/a.go": goFile("unsafe"), "b/b.go": goFile("unsafe")},
				[]string{`a/a.go imports "unsafe", which only kirt/heap/ may`, `b/b.go imports "unsafe", which only kirt/heap/ may`}},
		}
		for _, row := range rows {
			checkRule(t, row.name, writeTree(t, row.files), ".", unsafeProblems, row.want)
		}
	})
}
