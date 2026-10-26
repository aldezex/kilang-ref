// Command bookx is the author's tool for the book: it reads the markers in the
// code, the test cards and the directives in the prose, and checks that they
// agree. It exists only in the reference repository and uses nothing but the
// standard library.
//
// The parsers in this package are pure functions: they take the text of a file
// and return values and errors, and never touch git or the disk. Around them,
// Git reads the tags of a repository, verify and render check and resolve the
// book through the small repoReader interface, and run (this file) is the
// command line: verify, render, web, regions and testlist.
package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// indexHTML is the page of the book, which `bookx web` writes as index.html.
//
//go:embed web/index.html
var indexHTML []byte

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// Exit codes of bookx: 0 when all went well, 1 when it found problems in the
// book or could not do its work, and 2 when it was called wrongly.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

// commandUsage is the `usage:` line of each command.
var commandUsage = map[string]string{
	"verify":   "usage: bookx verify [--repo DIR]",
	"render":   "usage: bookx render <out-dir> [--repo DIR]",
	"regions":  "usage: bookx regions <tag> [--repo DIR]",
	"testlist": "usage: bookx testlist <go-test-json-file>",
	"web":      "usage: bookx web <out-dir> [--repo DIR] [--all]",
}

// run is main without the process around it: it runs the command in args,
// writes to stdout and stderr and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	var cmd func(args []string, stdout, stderr io.Writer) int
	if len(args) > 0 {
		switch args[0] {
		case "verify":
			cmd = runVerify
		case "render":
			cmd = runRender
		case "regions":
			cmd = runRegions
		case "testlist":
			cmd = runTestlist
		case "web":
			cmd = runWeb
		}
	}
	if cmd == nil {
		fmt.Fprintln(stderr, "usage: bookx <command> [args]")
		fmt.Fprintln(stderr, "commands: verify, render, regions, testlist, web")
		return exitUsage
	}
	return cmd(args[1:], stdout, stderr)
}

// parseArgs reads the arguments of a command: the flag --repo (when withRepo)
// and exactly want positional arguments, in any order, so `render out --repo d`
// works although the flag package stops at the first argument that is not a
// flag. A command with flags of its own defines them in define. It prints the
// problem and the usage line and returns ok false when the arguments are wrong.
func parseArgs(name string, args []string, want int, withRepo bool, stderr io.Writer, define ...func(*flag.FlagSet)) (repo string, pos []string, ok bool) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo = "."
	if withRepo {
		flags.StringVar(&repo, "repo", ".", "the repository")
	}
	for _, d := range define {
		d(flags)
	}
	for {
		if err := flags.Parse(args); err != nil {
			fmt.Fprintf(stderr, "bookx %s: %v\n%s\n", name, err, commandUsage[name])
			return "", nil, false
		}
		args = flags.Args()
		if len(args) == 0 {
			break
		}
		pos, args = append(pos, args[0]), args[1:]
	}
	if len(pos) != want {
		fmt.Fprintf(stderr, "bookx %s: want %d argument(s), found %d\n%s\n", name, want, len(pos), commandUsage[name])
		return "", nil, false
	}
	return repo, pos, true
}

// runVerify is `bookx verify [--repo DIR]`.
func runVerify(args []string, stdout, stderr io.Writer) int {
	repo, _, ok := parseArgs("verify", args, 0, true, stderr)
	if !ok {
		return exitUsage
	}
	problems, err := verify(os.DirFS(repo), Git{Repo: repo})
	if err != nil {
		fmt.Fprintf(stderr, "bookx verify: %v\n", err)
		return exitFail
	}
	if len(problems) == 0 {
		fmt.Fprintln(stdout, "bookx verify: OK")
		return exitOK
	}
	for _, p := range problems {
		fmt.Fprintln(stdout, p)
	}
	return exitFail
}

// runRender is `bookx render <out-dir> [--repo DIR]`.
func runRender(args []string, stdout, stderr io.Writer) int {
	repo, pos, ok := parseArgs("render", args, 1, true, stderr)
	if !ok {
		return exitUsage
	}
	problems, err := renderBook(os.DirFS(repo), Git{Repo: repo}, pos[0])
	if err != nil {
		fmt.Fprintf(stderr, "bookx render: %v\n", err)
		return exitFail
	}
	for _, p := range problems {
		fmt.Fprintln(stdout, p)
	}
	if len(problems) > 0 {
		return exitFail
	}
	return exitOK
}

// runRegions is `bookx regions <tag> [--repo DIR]`: it lists the regions of a
// tag as `<id> <mode> <path>:<line>`, where the line is the one of the @book
// marker, sorted by id. The problems with the markers go to stderr.
func runRegions(args []string, stdout, stderr io.Writer) int {
	repo, pos, ok := parseArgs("regions", args, 1, true, stderr)
	if !ok {
		return exitUsage
	}
	b := newBook(nil, Git{Repo: repo})
	if _, err := b.tagFiles(pos[0]); err != nil {
		fmt.Fprintf(stderr, "bookx regions: %v\n", err)
		return exitFail
	}
	scan := b.scanTag(pos[0])
	regions := slices.Clone(scan.regions)
	slices.SortStableFunc(regions, func(x, y Region) int {
		if c := strings.Compare(x.ID, y.ID); c != 0 {
			return c
		}
		if c := strings.Compare(x.Path, y.Path); c != 0 {
			return c
		}
		return x.FirstLine - y.FirstLine
	})
	for _, r := range regions {
		fmt.Fprintf(stdout, "%s %s %s:%d\n", r.ID, r.Mode, r.Path, r.FirstLine-1)
	}
	for _, e := range scan.errs {
		fmt.Fprintln(stderr, e)
	}
	if len(scan.errs) > 0 {
		return exitFail
	}
	return exitOK
}

// runTestlist is `bookx testlist <go-test-json-file>`: it prints the list of
// tests that ci/check-tags.sh keeps in book/tests/chNN.list.
func runTestlist(args []string, stdout, stderr io.Writer) int {
	_, pos, ok := parseArgs("testlist", args, 1, false, stderr)
	if !ok {
		return exitUsage
	}
	data, err := os.ReadFile(pos[0])
	if err != nil {
		fmt.Fprintf(stderr, "bookx testlist: %v\n", err)
		return exitFail
	}
	lines, err := parseTestList(data)
	if err != nil {
		fmt.Fprintf(stderr, "bookx testlist: %v\n", err)
		return exitFail
	}
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}
	return exitOK
}

// ScanError is a problem found in a file while parsing it. Line is 1-based.
type ScanError struct {
	Path    string
	Line    int
	Message string
}

// String formats the error as "<path>:<line>: <message>", the form every
// bookx report uses.
func (e ScanError) String() string {
	return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Message)
}

// sortScanErrors orders errs by path and then by line. The sort is stable, so
// two errors on the same line stay in the order in which they were found.
func sortScanErrors(errs []ScanError) {
	slices.SortStableFunc(errs, func(a, b ScanError) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return a.Line - b.Line
	})
}

// splitLines splits the text of a file into lines, the same way for every
// parser. Both "\n" and "\r\n" end a line, so a file written on Windows reads
// like one written on Linux; a lone "\r" does not end a line and is kept, also
// at the end of a file with no final newline. A byte order mark at the start is
// dropped, and a final newline does not start an extra empty line. An empty
// file has no lines, but an empty line inside the file is kept.
func splitLines(text []byte) []string {
	text = bytes.TrimPrefix(text, []byte("\xef\xbb\xbf"))
	if len(text) == 0 {
		return nil
	}
	s, endedByNewline := strings.CutSuffix(string(text), "\n")
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		// Only a line that a "\n" ended loses its "\r".
		if i < len(ls)-1 || endedByNewline {
			ls[i] = strings.TrimSuffix(l, "\r")
		}
	}
	return ls
}
