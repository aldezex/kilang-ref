package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// This file is `bookx doctest`: it runs every command of the `console` blocks of
// a chapter, in a clean copy of a tag, and compares what it prints with what the
// block shows. A block under `### Sesión K` runs at the tag of that session, and
// any other at the code of the chapter (its tag chNN, or its last session). A
// difference fails from chapter 11 on, and the chapters before it are measured.
//
// An attribute of the fence says what cannot be run as it stands:
//
//	```console doctest="skip: needs a terminal"   runs nothing, and says why
//	```console doctest="varies: benchmark times"  compares the shape: any number, any run of spaces
//	```console doctest="red"                      runs at the tag before, plus this session's *_test.go
//
// The page reads the first word of the fence as its language and ignores the
// rest, so the reader sees none of this. Commands run from the root of the copy,
// and those of one block share a shell. Their state stays in the copy of the tag,
// so one block sees what an earlier one left, as in the reader's tree. A `$ cat
// file` with text below it, of a file the tag does not hold, is how a chapter
// gives a program: it writes the file, for this and all later blocks. No
// directory is added to PATH: a bare `ki` fails here because it fails there.

// doctestMode says how a block is run.
type doctestMode int

const (
	doctestRun doctestMode = iota
	doctestSkip
	doctestVaries
	doctestRed
)

// doctestCommand is a `$` line of a block and the output shown below it.
type doctestCommand struct {
	Line     int
	Cmd      string
	Expected []string
}

// doctestBlock is one `console` fence of a chapter.
type doctestBlock struct {
	Line      int
	Session   int    // the K of the `### Sesión K` it is under; 0 when none
	Container string // the container it is in; "" when none
	Mode      doctestMode
	Reason    string
	Commands  []doctestCommand
}

var (
	// fenceAttrRe is the attribute of a fence that holds the annotation.
	fenceAttrRe = regexp.MustCompile(`\bdoctest="([^"]*)"`)
	// sessionNumberRe is the number in the title of a session.
	sessionNumberRe = regexp.MustCompile(`^` + sessionHeadingPrefix + `(\d+)`)
	// doctestInputRe is a `cat` whose text below it may be a file to write. The
	// path stays inside the copy.
	doctestInputRe = regexp.MustCompile(`^cat ([\w.-][\w./-]*)$`)
	// toolchainRe is the toolchain line of a go.mod.
	toolchainRe = regexp.MustCompile(`(?m)^toolchain (\S+)`)
)

// parseConsoleBlocks reads the `console` fences of a chapter. A bad annotation
// is a problem, and its block runs as if it had none.
func parseConsoleBlocks(markdown []byte) ([]doctestBlock, []ScanError) {
	var blocks []doctestBlock
	var problems []ScanError
	var cur *doctestBlock
	var last *doctestCommand // the command the next lines belong to
	for _, l := range proseLines(markdown) {
		switch l.Kind {
		case lineFenceOpen:
			if lang, _, _ := strings.Cut(l.FenceInfo, " "); lang != "console" {
				continue
			}
			b := doctestBlock{Line: l.Number, Container: l.Container}
			if m := sessionNumberRe.FindStringSubmatch(l.Session); m != nil {
				b.Session, _ = strconv.Atoi(m[1])
			}
			if m := fenceAttrRe.FindStringSubmatch(l.FenceInfo); m != nil {
				var msg string
				b.Mode, b.Reason, msg = parseDoctestAnnotation(m[1])
				if msg != "" {
					problems = append(problems, ScanError{Line: l.Number, Message: msg})
				}
			}
			blocks = append(blocks, b)
			cur, last = &blocks[len(blocks)-1], nil
		case lineFenceClose:
			cur, last = nil, nil
		case lineFenceBody:
			if cur == nil {
				continue
			}
			text := l.Text[min(l.Indent, len(l.Text)-len(strings.TrimLeft(l.Text, " "))):]
			if last != nil && len(last.Expected) == 0 && strings.HasSuffix(last.Cmd, "\\") {
				last.Cmd += "\n" + text
			} else if cmd, ok := strings.CutPrefix(text, "$ "); ok {
				cur.Commands = append(cur.Commands, doctestCommand{Line: l.Number, Cmd: cmd})
				last = &cur.Commands[len(cur.Commands)-1]
			} else if last != nil {
				last.Expected = append(last.Expected, text)
			}
		}
	}
	return blocks, problems
}

// parseDoctestAnnotation reads the value of the attribute. msg is the problem
// with it, and then the mode is the plain one.
func parseDoctestAnnotation(v string) (mode doctestMode, reason, msg string) {
	if v == "red" {
		return doctestRed, "", ""
	}
	word, reason, _ := strings.Cut(v, ":")
	reason = strings.TrimSpace(reason)
	switch word {
	case "skip":
		mode = doctestSkip
	case "varies":
		mode = doctestVaries
	default:
		return doctestRun, "", fmt.Sprintf("unknown doctest annotation `%s`; want `skip: why`, `varies: why` or `red`", v)
	}
	if reason == "" {
		return doctestRun, "", fmt.Sprintf("doctest annotation `%s` needs a reason: `%s: why`", v, word)
	}
	return mode, reason, ""
}

var (
	goTimeRe  = regexp.MustCompile(`\b\d+\.\d+s\b|\(cached\)`)
	shortRe   = regexp.MustCompile(`\d+(?:\.\d+)?(?:ms|µs|ns)\b`)
	numberRe  = regexp.MustCompile(`\d+`)
	blanksRe  = regexp.MustCompile(`[ \t]+`)
	ellipsisS = []string{"…", "..."}
)

// doctestNormalize takes out of a line what changes from run to run: the time a
// test took, which `go test` also writes as `(cached)`, and the spaces at its end.
func doctestNormalize(line string, varies bool) string {
	line = goTimeRe.ReplaceAllString(line, "TIME")
	line = shortRe.ReplaceAllString(line, "TIME")
	if varies {
		line = numberRe.ReplaceAllString(line, "N")
		line = blanksRe.ReplaceAllString(line, " ")
	}
	return strings.TrimRight(line, " \t")
}

// doctestMatch compares the output of a command with what the block shows. A
// line of the block that is only `…` stands for any number of lines, and a `…`
// inside a line for any text in it. Blank lines at the end do not count.
func doctestMatch(expected, got []string, varies bool) bool {
	normal := func(lines []string) []string {
		out := make([]string, 0, len(lines))
		for _, l := range lines {
			out = append(out, doctestNormalize(l, varies))
		}
		for len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1]
		}
		return out
	}
	want, have := normal(expected), normal(got)
	if len(want) == 0 {
		return len(have) == 0
	}
	var rx strings.Builder
	rx.WriteString(`\A`)
	for _, line := range want {
		if slices.Contains(ellipsisS, strings.TrimSpace(line)) {
			rx.WriteString(`(?:.*\n)*?`)
			continue
		}
		rx.WriteString(strings.ReplaceAll(regexp.QuoteMeta(line), "…", `.*`))
		rx.WriteString(`\n`)
	}
	rx.WriteString(`\z`)
	text := strings.Join(have, "\n")
	if len(have) > 0 {
		text += "\n"
	}
	ok, err := regexp.MatchString(rx.String(), text)
	return err == nil && ok
}

// doctestCounts are the numbers of a chapter.
type doctestCounts struct {
	Commands, OK, Differ, Inputs, Skipped, Shape, NoCommand int
}

// doctestTimeoutSeconds is the time limit of one block. A test shortens it.
var doctestTimeoutSeconds = "300"

// doctestMaxShown is how many lines of a difference are shown, on each side.
const doctestMaxShown = 12

// doctestChapter runs one chapter.
type doctestChapter struct {
	ctx     context.Context
	git     Git
	scratch string
	nn      string
	path    string
	out     io.Writer
	tags    []string
	code    string            // the tag with the code of the chapter
	copies  map[string]string // by key (a tag, or a tag and `+red`): the directory of its copy
	needs   map[string]string // by key: the toolchain the copy needs, if not this one
	inputs  map[string]string // the files the chapter wrote with a `$ cat`
	counts  doctestCounts
	failed  bool // something other than a difference went wrong
	units   int  // the number of runs, which names their units
}

// sessionTags returns the sessions of chapter nn among tags, with their K.
func sessionTags(tags []string, nn string) map[string]int {
	sessions := map[string]int{}
	for _, t := range tags {
		if m := sessionTagRe.FindStringSubmatch(t); m != nil && m[1] == nn {
			sessions[t], _ = strconv.Atoi(m[2])
		}
	}
	return sessions
}

// tagBefore returns the tag whose code comes just before session tag: the session
// before it, or else the code of the nearest chapter before it. "" when none.
func tagBefore(tags []string, tag string) string {
	m := sessionTagRe.FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	k, _ := strconv.Atoi(m[2])
	before := map[string]int{}
	for t, j := range sessionTags(tags, m[1]) {
		if j < k {
			before[t] = j
		}
	}
	if t := codeTag(m[1], false, before); t != "" {
		return t
	}
	nn, _ := strconv.Atoi(m[1])
	for n := nn - 1; n >= 1; n-- {
		prev := fmt.Sprintf("%02d", n)
		if t := codeTag(prev, slices.Contains(tags, "ch"+prev), sessionTags(tags, prev)); t != "" {
			return t
		}
	}
	return ""
}

// extract writes the files of tag into dir.
func (d *doctestChapter) extract(tag, dir string) error {
	if err := checkName("tag", tag); err != nil {
		return err
	}
	archive, err := d.git.run("archive", tag)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(d.ctx, "tar", "-x", "-C", dir)
	cmd.Stdin = bytes.NewReader(archive)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar: %v: %s", err, out)
	}
	return nil
}

// copyOf returns the directory of the clean copy for a block at tag, making it
// the first time. With red, it is the tag before, plus the test files that this
// session changed. needs is the toolchain the tag asks for when it is not the one
// installed, and then the copy cannot be used.
func (d *doctestChapter) copyOf(tag string, red bool) (dir, needs string, err error) {
	key := tag
	if red {
		key += "+red"
	}
	if dir, ok := d.copies[key]; ok {
		return dir, d.needs[key], nil
	}
	dir = filepath.Join(d.scratch, key)
	from := tag
	if red {
		if from = tagBefore(d.tags, tag); from == "" {
			return "", "", fmt.Errorf("no tag comes before %s", tag)
		}
	}
	if err := d.extract(from, dir); err != nil {
		return "", "", err
	}
	// bookx exists only in the reference: the reader's `go test ./...` does not
	// list it, and neither does what the book shows.
	if err := os.RemoveAll(filepath.Join(dir, "cmd", "bookx")); err != nil {
		return "", "", err
	}
	if red {
		names, err := d.git.run("diff", "--name-only", "--diff-filter=d", "-z", from, tag, "--", "*_test.go")
		if err != nil {
			return "", "", err
		}
		for _, name := range strings.Split(strings.TrimRight(string(names), "\x00"), "\x00") {
			if name == "" {
				continue
			}
			data, err := d.git.Show(tag, name)
			if err != nil {
				return "", "", err
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
				return "", "", err
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				return "", "", err
			}
		}
	}
	d.copies[key] = dir
	if mod, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		if m := toolchainRe.FindSubmatch(mod); m != nil {
			have, err := exec.Command("go", "env", "GOVERSION").Output()
			if err != nil {
				return "", "", fmt.Errorf("go env GOVERSION: %w", err)
			}
			if string(m[1]) != strings.TrimSpace(string(have)) {
				d.needs[key] = string(m[1])
				return dir, d.needs[key], nil
			}
		}
	}
	// The chapters tell the reader to build ki, and a tree where it does not build
	// shows it in the output of the commands that use it.
	if _, err := os.Stat(filepath.Join(dir, "cmd", "ki")); err == nil {
		build := exec.CommandContext(d.ctx, "go", "build", "-o", "build/bin/ki", "./cmd/ki")
		build.Dir, build.Env = dir, cleanEnv()
		_ = build.Run()
	}
	return dir, "", nil
}

// report writes a line about the block or the command at line.
func (d *doctestChapter) report(line int, format string, args ...any) {
	fmt.Fprintf(d.out, "%s:%d: %s\n", d.path, line, fmt.Sprintf(format, args...))
}

// runBlock runs one block and reports what differs. The error is for what stops
// the whole command, such as a missing memory cap.
func (d *doctestChapter) runBlock(b doctestBlock) error {
	d.counts.Commands += len(b.Commands)
	if len(b.Commands) == 0 {
		d.counts.NoCommand++
		return nil
	}
	if b.Mode == doctestSkip {
		d.counts.Skipped += len(b.Commands)
		d.report(b.Line, "skip: %s", b.Reason)
		return nil
	}
	tag := d.code
	if t := fmt.Sprintf("ch%s-s%d", d.nn, b.Session); slices.Contains(d.tags, t) {
		tag = t
	}
	if b.Mode == doctestRed && b.Session == 0 {
		d.report(b.Line, "red: this block is in no session")
		d.failed = true
		return nil
	}
	dir, needs, err := d.copyOf(tag, b.Mode == doctestRed)
	if err != nil {
		what := "cannot copy the tag"
		if b.Mode == doctestRed {
			what = "red"
		}
		d.report(b.Line, "%s: %v", what, err)
		d.failed = true
		return nil
	}
	if needs != "" {
		d.counts.Skipped += len(b.Commands)
		d.report(b.Line, "skip: %s needs %s", tag, needs)
		return nil
	}
	files, err := d.git.Files(tag)
	if err != nil {
		return err
	}

	// The programs of the blocks before this one.
	for name, text := range d.inputs {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			return err
		}
	}

	// A marker line before each command and after the last one splits the output.
	// Each one starts with a newline, in case the command before did not end its
	// line, and $? is saved around it, so `$ echo $?` sees the command before. A
	// `$ cat` of a file the tag does not hold writes it at that point of the block,
	// because a later command may read it and an earlier one the version before.
	var run []doctestCommand
	var script strings.Builder
	script.WriteString("exec 2>&1\n")
	for _, c := range b.Commands {
		m := doctestInputRe.FindStringSubmatch(c.Cmd)
		if m != nil && len(c.Expected) > 0 && !strings.Contains(m[1], "..") && !slices.Contains(files, m[1]) {
			body := strings.Join(c.Expected, "\n") + "\n"
			d.inputs[m[1]] = body
			d.counts.Inputs++
			fmt.Fprintf(&script, "__rc=$?; mkdir -p \"$(dirname '%s')\"; cat > '%s' <<'%s'\n%s%s\n(exit $__rc)\n", m[1], m[1], doctestHeredoc, body, doctestHeredoc)
			continue
		}
		fmt.Fprintf(&script, "__rc=$?; printf '\\n%s\\n'; (exit $__rc); %s\n", doctestMarker(len(run)), c.Cmd)
		run = append(run, c)
	}
	if len(run) == 0 {
		return nil
	}
	fmt.Fprintf(&script, "printf '\\n%s\\n'\n", doctestMarker(len(run)))
	d.units++
	// The script goes in a file because systemd-run expands `${x}` in the arguments
	// it is given, and the programs of the book are full of them.
	scriptPath := filepath.Join(d.scratch, fmt.Sprintf("block-%d.sh", d.units))
	if err := os.WriteFile(scriptPath, []byte(script.String()), 0o644); err != nil {
		return err
	}
	output, exit, err := cappedRun{
		Unit:    fmt.Sprintf("ki-doctest-%d-ch%s-%d", os.Getpid(), d.nn, d.units),
		Dir:     dir,
		Env:     cleanEnv(),
		Seconds: doctestTimeoutSeconds,
		Argv:    []string{"bash", scriptPath},
	}.run(d.ctx)
	if err != nil {
		return err
	}
	text := string(output)
	for i, c := range run {
		got, finished := doctestOutput(text, i)
		if finished && doctestMatch(c.Expected, got, b.Mode == doctestVaries) {
			d.counts.OK++
		} else {
			d.counts.Differ++
			d.report(c.Line, "differ at %s: $ %s", tag, c.Cmd)
			for _, l := range c.Expected[:min(doctestMaxShown, len(c.Expected))] {
				fmt.Fprintf(d.out, "  - %s\n", l)
			}
			for _, l := range got[:min(doctestMaxShown, len(got))] {
				fmt.Fprintf(d.out, "  + %s\n", l)
			}
			if !finished {
				fmt.Fprintf(d.out, "  stopped: exit %d\n", exit)
			}
		}
		if b.Mode == doctestVaries {
			d.counts.Shape++
		}
	}
	return nil
}

// doctestHeredoc ends the text of a file written by a block's script.
const doctestHeredoc = "__BOOKX_DOCTEST_INPUT__"

// doctestMarker is the line that goes before command i of a block.
func doctestMarker(i int) string { return fmt.Sprintf("@@@bookx-doctest-%d@@@", i) }

// doctestOutput returns what command i printed, and whether it finished, which
// is to say whether the marker of the next command came.
func doctestOutput(text string, i int) (lines []string, finished bool) {
	_, after, ok := strings.Cut(text, "\n"+doctestMarker(i)+"\n")
	if !ok {
		return nil, false
	}
	body, _, finished := strings.Cut(after, "\n"+doctestMarker(i+1)+"\n")
	body = strings.TrimSuffix(body, "\n")
	if body == "" {
		return nil, finished
	}
	return strings.Split(body, "\n"), finished
}

// runDoctest is `bookx doctest [--repo DIR] <file>...`: it prints what differs
// and one line of counts per chapter. The exit code is 1 when a chapter from
// firstStrictChapter on has a difference or a block that cannot run, or the
// command cannot do its work.
func runDoctest(args []string, stdout, stderr io.Writer) int {
	repo, files, ok := parseArgs("doctest", args, -1, true, stderr)
	if !ok {
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	repo, err := filepath.Abs(repo)
	if err == nil {
		_, err = capCommand("ki-doctest-check", []string{"true"})
	}
	if err != nil {
		fmt.Fprintf(stderr, "bookx doctest: %v\n", err)
		return exitFail
	}
	git := Git{Repo: repo}
	tags, err := git.Tags()
	if err != nil {
		fmt.Fprintf(stderr, "bookx doctest: %v\n", err)
		return exitFail
	}
	scratch, done, err := newScratch(repo, "doctest")
	if err != nil {
		fmt.Fprintf(stderr, "bookx doctest: %v\n", err)
		return exitFail
	}
	defer done()

	code := exitOK
	for _, f := range files {
		m := proseNameRe.FindStringSubmatch(filepath.Base(f))
		if m == nil {
			fmt.Fprintf(stderr, "bookx doctest: %s: not a chapter (chNN-slug.md)\n", f)
			code = exitFail
			continue
		}
		text, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(stderr, "bookx doctest: %v\n", err)
			code = exitFail
			continue
		}
		dir := filepath.Join(scratch, "ch"+m[1])
		failed, err := doctestFile(ctx, git, tags, dir, m[1], f, text, stdout)
		if err != nil {
			fmt.Fprintf(stderr, "bookx doctest: %v\n", err)
			return exitFail
		}
		if failed && strictChapter(m[1]) {
			code = exitFail
		}
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(stderr, "bookx doctest: %v\n", err)
			code = exitFail
		}
	}
	return code
}

// doctestFile runs one chapter in its scratch directory, and reports whether
// anything in it differs or could not run.
func doctestFile(ctx context.Context, git Git, tags []string, scratch, nn, path string, text []byte, out io.Writer) (failed bool, err error) {
	d := &doctestChapter{
		ctx: ctx, git: git, scratch: scratch, nn: nn, path: path, out: out, tags: tags,
		code:   codeTag(nn, slices.Contains(tags, "ch"+nn), sessionTags(tags, nn)),
		copies: map[string]string{}, needs: map[string]string{}, inputs: map[string]string{},
	}
	blocks, problems := parseConsoleBlocks(text)
	for _, p := range problems {
		d.report(p.Line, "%s", p.Message)
		d.failed = true
	}
	if d.code == "" {
		d.report(1, "chapter %s has no tag `ch%s` and no session tag `ch%s-sK`", nn, nn, nn)
		d.failed = true
		blocks = nil
	}
	for _, b := range blocks {
		if err := d.runBlock(b); err != nil {
			return false, err
		}
		if err := ctx.Err(); err != nil {
			return false, errors.New("interrupted")
		}
	}
	c := d.counts
	tail := ""
	if !strictChapter(nn) {
		tail = fmt.Sprintf(" (report only: before chapter %d)", firstStrictChapter)
	}
	fmt.Fprintf(out, "doctest %s: %d commands: %d ok, %d differ, %d inputs, %d skipped, %d shape only, %d blocks without a command%s\n",
		path, c.Commands, c.OK, c.Differ, c.Inputs, c.Skipped, c.Shape, c.NoCommand, tail)
	return d.failed || c.Differ > 0, nil
}
