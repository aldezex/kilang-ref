package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
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
	"time"
)

// This file is `bookx mutate`, which ci/mutate runs: it breaks a package on
// purpose, one small edit at a time, and says which test noticed each edit. A
// mutant that survives is a bug the tests would let through.
//
// Three things make it safe to leave running, each one learned from a crash:
//
//   - It works on a copy of the working tree under build/mutate, never in place.
//     The tree cannot be left broken, because nothing ever touches it. The edit
//     is a real one in the copy, because some tests build `ki` in a subprocess,
//     which `go test -overlay` would not reach.
//   - Each mutant runs alone inside its own cgroup under the memory cap, in a
//     unit that carries its id, so a death has one culprit. What is about to run
//     goes to build/mutate/last.log first, so a machine that dies says on what.
//   - Its build cache and temporary files stay inside its directory on disk and
//     go away with it: a sweep writes gigabytes, and /tmp is a tmpfs.

// mutateOptions are the flags of the command.
type mutateOptions struct {
	Pkg     string
	Since   string
	Limit   int
	Timeout int
	Apply   string
	Old     string
	New     string
	Only    string
	List    bool
}

// mutateSharedCache, when set, is the GOCACHE of a run instead of one of its own.
// The tests set it: a new cache compiles the standard library again.
var mutateSharedCache = ""

// A verdict says what a mutant came to.
const (
	verdictKilled    = "killed"
	verdictOOM       = "oom"
	verdictTimeout   = "timeout"
	verdictSurvived  = "survived"
	verdictNoCompile = "nocompile"
)

var (
	buildFailedRe = regexp.MustCompile(`(?m)^FAIL\s+\S+ \[(?:build|setup) failed\]$`)
	failedTestRe  = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)
	failedPkgRe   = regexp.MustCompile(`(?m)^FAIL\s+(\S+)`)
	panicLineRe   = regexp.MustCompile(`(?m)^panic: .*$`)
)

// classifyRun reads the exit code and the output of `go test` on a mutant, and
// returns its verdict and what killed it.
func classifyRun(exit int, output []byte) (verdict, by string) {
	switch {
	case exit == 0:
		return verdictSurvived, ""
	case exit == exitTimedOut || bytes.Contains(output, []byte("panic: test timed out")):
		return verdictTimeout, "the time limit"
	case exit == exitKilled || bytes.Contains(output, []byte("signal: killed")):
		return verdictOOM, "the memory cap"
	case buildFailedRe.Match(output):
		return verdictNoCompile, ""
	}
	return verdictKilled, firstFailure(output)
}

// firstFailure names what failed first: the innermost subtest of the first test
// that failed, or else the panic, or else the package.
func firstFailure(output []byte) string {
	tests := failedTestRe.FindAllSubmatch(output, -1)
	if len(tests) > 0 {
		name := string(tests[0][1])
		for _, m := range tests[1:] {
			if !strings.HasPrefix(string(m[1]), name+"/") {
				break
			}
			name = string(m[1])
		}
		return name
	}
	if m := panicLineRe.Find(output); m != nil {
		return oneLine(string(m))
	}
	if m := failedPkgRe.FindSubmatch(output); m != nil {
		return string(m[1])
	}
	return "exit code"
}

// mutateRun is one run of the command.
type mutateRun struct {
	ctx     context.Context
	opts    mutateOptions
	git     Git
	tree    string // the copy of the working tree
	env     []string
	seconds int // the time limit of one mutant
	log     *os.File
	out     io.Writer
	pkgs    []string // what the second pass runs
}

// copyWorkingTree copies what git would see, tracked or new but not ignored, as
// it is now. So a mutant meets the tests of a session not yet committed, and the
// copy holds no build/ or .superpowers/.
func copyWorkingTree(git Git, dst string) error {
	list, err := git.run("ls-files", "-c", "-o", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	for _, name := range strings.Split(strings.TrimRight(string(list), "\x00"), "\x00") {
		if name == "" {
			continue
		}
		src := filepath.Join(git.Repo, name)
		info, err := os.Lstat(src)
		if errors.Is(err, os.ErrNotExist) {
			continue // deleted and not yet committed
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dst, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(src)
			if err != nil {
				return err
			}
			if err := os.Symlink(link, target); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -\S+ \+(\d+)(?:,(\d+))? @@`)

// changedLines parses `git diff -U0`: for each file, the lines of its new version
// that the diff adds.
func changedLines(diff []byte) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	cur := ""
	for _, line := range strings.Split(string(diff), "\n") {
		if name, ok := strings.CutPrefix(line, "+++ b/"); ok {
			cur = name
			out[cur] = map[int]bool{}
			continue
		}
		m := hunkHeaderRe.FindStringSubmatch(line)
		if m == nil || cur == "" {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		n := 1
		if m[2] != "" {
			n, _ = strconv.Atoi(m[2])
		}
		for i := 0; i < n; i++ {
			out[cur][start+i] = true
		}
	}
	return out
}

// sinceFilter returns which lines count with --since: those the working tree
// changed since the revision, and all the lines of a file git does not track yet.
func sinceFilter(git Git, rev string) (func(string, int) bool, error) {
	if err := checkName("revision", rev); err != nil {
		return nil, err
	}
	diff, err := git.run("diff", "-U0", rev, "--")
	if err != nil {
		return nil, err
	}
	changed := changedLines(diff)
	untracked, err := git.run("ls-files", "-o", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	isNew := map[string]bool{}
	for _, name := range strings.Split(string(untracked), "\x00") {
		isNew[name] = true
	}
	return func(file string, line int) bool { return isNew[file] || changed[file][line] }, nil
}

// goTest runs `go test` in the copy under the cap, as the unit of one mutant.
func (r *mutateRun) goTest(unit string, pkgs []string, failfast bool) ([]byte, int, error) {
	args := []string{"go", "test", "-count=1", "-vet=off", fmt.Sprintf("-timeout=%ds", r.seconds)}
	if failfast {
		args = append(args, "-failfast")
	}
	return cappedRun{
		// `timeout` has to come after the go test's own limit, whose report names
		// the goroutines: it is there for what that limit cannot stop.
		Unit:    fmt.Sprintf("ki-mutate-%d-%s", os.Getpid(), unit),
		Dir:     r.tree,
		Env:     r.env,
		Seconds: strconv.Itoa(r.seconds + 15),
		Argv:    append(args, pkgs...),
	}.run(r.ctx)
}

// note writes a line to the log and to the report.
func (r *mutateRun) note(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	fmt.Fprintln(r.log, line)
	fmt.Fprintln(r.out, line)
}

// mutateTally counts the verdicts of a run.
type mutateTally struct {
	Killed, Elsewhere, OOM, Timeout, Survived, NoCompile int
}

// runMutant runs one mutant of the operators. Its file in the copy is mutated
// and put back, whatever the run does, before the function returns.
func (r *mutateRun) runMutant(m mutant, pkgs []string, secondPass bool) (verdict, by string, err error) {
	p := filepath.Join(r.tree, filepath.FromSlash(m.File))
	src, err := os.ReadFile(p)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(p, m.apply(src), 0o644); err != nil {
		return "", "", err
	}
	defer func() {
		if werr := os.WriteFile(p, src, 0o644); werr != nil && err == nil {
			err = werr
		}
	}()
	unit := m.ID
	if secondPass {
		unit += "-all"
	}
	fmt.Fprintf(r.log, "start %s\n", m)
	out, exit, err := r.goTest(unit, pkgs, !secondPass)
	if err != nil {
		return "", "", err
	}
	verdict, by = classifyRun(exit, out)
	return verdict, by, nil
}

// sweep runs the operators' mutants and reports each verdict as it comes.
func (r *mutateRun) sweep() (mutateTally, error) {
	var t mutateTally
	keep := func(string, int) bool { return true }
	if r.opts.Since != "" {
		f, err := sinceFilter(r.git, r.opts.Since)
		if err != nil {
			return t, err
		}
		keep = f
	}
	all, filtered, err := packageMutants(r.tree, filepath.Join(r.tree, filepath.FromSlash(r.opts.Pkg)), keep)
	if err != nil {
		return t, err
	}
	ms := all
	if r.opts.Only != "" {
		ids := strings.Split(r.opts.Only, ",")
		ms = slices.DeleteFunc(slices.Clone(ms), func(m mutant) bool { return !slices.Contains(ids, m.ID) })
	}
	if r.opts.Limit > 0 && len(ms) > r.opts.Limit {
		step := float64(len(ms)) / float64(r.opts.Limit)
		sample := make([]mutant, 0, r.opts.Limit)
		for i := 0; i < r.opts.Limit; i++ {
			sample = append(sample, ms[int(float64(i)*step)])
		}
		ms = sample
	}
	fmt.Fprintf(r.out, "mutate %s: %d mutants of %d (%d left out as equivalent)\n", r.opts.Pkg, len(ms), len(all), filtered)
	if r.opts.List {
		for _, m := range ms {
			fmt.Fprintln(r.out, m)
		}
		return t, nil
	}
	if err := r.baseline(); err != nil {
		return t, err
	}
	for _, m := range ms {
		verdict, by, err := r.runMutant(m, []string{r.opts.Pkg}, false)
		if err != nil {
			return t, err
		}
		if verdict == verdictSurvived && len(r.pkgs) > 1 {
			// Its own package's tests missed it, but another package may catch it.
			verdict, by, err = r.runMutant(m, r.pkgs, true)
			if err != nil {
				return t, err
			}
			if verdict == verdictKilled {
				t.Elsewhere++
			}
		}
		if err := r.ctx.Err(); err != nil {
			return t, errors.New("interrupted")
		}
		switch verdict {
		case verdictKilled:
			t.Killed++
		case verdictOOM:
			t.OOM++
		case verdictTimeout:
			t.Timeout++
		case verdictSurvived:
			t.Survived++
		case verdictNoCompile:
			t.NoCompile++
		default:
			panic("unreachable")
		}
		if by != "" {
			r.note("%-9s %s (by %s)", verdict, m, by)
		} else {
			r.note("%-9s %s", verdict, m)
		}
	}
	return t, nil
}

// baseline runs the tests with no mutant: on red tests no verdict means anything.
// It also sets the time limit of a mutant, unless a flag did: six times this run,
// with the compiling included, and at least 30 seconds. So a mutant that hangs is
// told from a slow one, and the cache of this first run spares the later ones.
func (r *mutateRun) baseline() error {
	pkgs := r.pkgs
	if r.opts.Pkg != "" {
		pkgs = []string{r.opts.Pkg}
	}
	start := time.Now()
	limit := r.seconds
	if limit == 0 {
		r.seconds = 600 // the first run compiles the standard library in a new cache
	}
	out, exit, err := r.goTest("baseline", pkgs, true)
	if err != nil {
		return err
	}
	if exit != 0 {
		tail := out
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		return fmt.Errorf("the tests fail with no mutant (exit %d):\n%s", exit, tail)
	}
	if limit == 0 {
		// Once more, to time the tests and not the compiling of everything.
		start = time.Now()
		if _, _, err := r.goTest("baseline-2", pkgs, true); err != nil {
			return err
		}
		r.seconds = max(30, 6*int(time.Since(start).Seconds()+1))
	}
	return nil
}

// applyHand runs one mutant written by hand, from a patch or from --old and --new,
// and reports its verdict. It returns whether a test killed it.
func (r *mutateRun) applyHand() (bool, error) {
	if err := r.baseline(); err != nil {
		return false, err
	}
	changed := 0
	if r.opts.Old != "" || r.opts.New != "" {
		p := filepath.Join(r.tree, filepath.FromSlash(r.opts.Apply))
		src, err := os.ReadFile(p)
		if err != nil {
			return false, err
		}
		n := bytes.Count(src, []byte(r.opts.Old))
		if r.opts.Old == "" || n != 1 {
			return false, fmt.Errorf("--old has to occur exactly once in %s, and it occurs %d times", r.opts.Apply, n)
		}
		if err := os.WriteFile(p, bytes.Replace(src, []byte(r.opts.Old), []byte(r.opts.New), 1), 0o644); err != nil {
			return false, err
		}
		changed = 1
	} else {
		n, err := r.applyPatch(r.opts.Apply)
		if err != nil {
			return false, err
		}
		changed = n
	}
	pkgs := r.pkgs
	if r.opts.Pkg != "" {
		pkgs = []string{r.opts.Pkg}
	}
	fmt.Fprintf(r.log, "start hand mutant %s\n", r.opts.Apply)
	out, exit, err := r.goTest("hand", pkgs, false)
	if err != nil {
		return false, err
	}
	verdict, by := classifyRun(exit, out)
	line := fmt.Sprintf("%-9s %s (%d file(s))", verdict, r.opts.Apply, changed)
	if by != "" {
		line += " (by " + by + ")"
	}
	r.note("%s", line)
	if verdict == verdictNoCompile {
		fmt.Fprintf(r.out, "%s", out)
	}
	return verdict == verdictKilled || verdict == verdictOOM || verdict == verdictTimeout, nil
}

// applyPatch applies a patch to the copy and returns how many files it changed.
// A patch written by hand may have wrong counts and no context lines, and that is
// fine (--recount, --unidiff-zero).
// git must not see the repository around the copy: inside one, `git apply` skips
// the paths outside its directory, and says nothing. A patch that changes nothing
// is an error, because it would survive and prove nothing.
func (r *mutateRun) applyPatch(patch string) (int, error) {
	env := append(cleanEnv(), "GIT_CEILING_DIRECTORIES="+filepath.Dir(r.tree))
	git := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(r.ctx, "git", args...)
		cmd.Dir, cmd.Env = r.tree, env
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("git %s: %v: %s", args[0], err, out)
		}
		return out, nil
	}
	stat, err := git("apply", "--recount", "--unidiff-zero", "--numstat", patch)
	if err != nil {
		return 0, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(stat)), "\n") {
		if f := strings.Fields(line); len(f) == 3 {
			files = append(files, f[2])
		}
	}
	before := map[string][]byte{}
	for _, f := range files {
		before[f], _ = os.ReadFile(filepath.Join(r.tree, filepath.FromSlash(f)))
	}
	if _, err := git("apply", "--recount", "--unidiff-zero", patch); err != nil {
		return 0, err
	}
	n := 0
	for _, f := range files {
		after, _ := os.ReadFile(filepath.Join(r.tree, filepath.FromSlash(f)))
		if !bytes.Equal(after, before[f]) {
			n++
		}
	}
	if n == 0 {
		return 0, fmt.Errorf("the patch %s changes nothing", patch)
	}
	return n, nil
}

// runMutate is `bookx mutate`: the exit code is 0 when the sweep ran to its end,
// whatever survived, because a survivor is for a person to judge. A mutant by
// hand is a claim, and it is 1 unless a test kills it. It is also 1 when the
// tests fail with no mutant, or the command cannot do its work.
func runMutate(args []string, stdout, stderr io.Writer) int {
	var o mutateOptions
	repo, _, ok := parseArgs("mutate", args, 0, true, stderr, func(f *flag.FlagSet) {
		f.StringVar(&o.Pkg, "pkg", "", "the package, ./internal/sema")
		f.StringVar(&o.Since, "since", "", "only the lines changed since this revision")
		f.IntVar(&o.Limit, "limit", 0, "at most this many mutants, evenly spaced")
		f.IntVar(&o.Timeout, "timeout", 0, "seconds for one mutant")
		f.StringVar(&o.Apply, "apply", "", "a hand-written mutant: a patch, or a file with --old and --new")
		f.StringVar(&o.Old, "old", "", "with --apply FILE: the text to replace")
		f.StringVar(&o.New, "new", "", "with --apply FILE: the text to put")
		f.StringVar(&o.Only, "only", "", "only these mutants: M0001,M0002")
		f.BoolVar(&o.List, "list", false, "list the mutants and run nothing")
	})
	if !ok {
		return exitUsage
	}
	if o.Apply == "" && o.Pkg == "" {
		fmt.Fprintf(stderr, "bookx mutate: want --pkg or --apply\n%s\n", commandUsage["mutate"])
		return exitUsage
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "bookx mutate: %v\n", err)
		return exitFail
	}
	if _, err := capCommand("ki-mutate-check", []string{"true"}); err != nil {
		return fail(err)
	}
	if o.Apply != "" && o.Old == "" && o.New == "" {
		abs, err := filepath.Abs(o.Apply)
		if err != nil {
			return fail(err)
		}
		o.Apply = abs
	}
	repo, err := filepath.Abs(repo)
	if err != nil {
		return fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scratch, done, err := newScratch(repo, "mutate")
	if err != nil {
		return fail(err)
	}
	defer done()
	log, err := os.Create(filepath.Join(repo, "build", "mutate", "last.log"))
	if err != nil {
		return fail(err)
	}
	defer log.Close()
	r := &mutateRun{ctx: ctx, opts: o, git: Git{Repo: repo}, tree: filepath.Join(scratch, "tree"), seconds: o.Timeout, log: log, out: stdout}
	if err := copyWorkingTree(r.git, r.tree); err != nil {
		return fail(err)
	}
	tmp := filepath.Join(scratch, "tmp")
	cache := filepath.Join(scratch, "gocache")
	if mutateSharedCache != "" {
		cache = mutateSharedCache
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fail(err)
	}
	r.env = append(cleanEnv(), "GOCACHE="+cache, "TMPDIR="+tmp)
	if err := r.listPackages(); err != nil {
		return fail(err)
	}

	start := time.Now()
	if o.Apply != "" {
		killed, err := r.applyHand()
		if err != nil {
			return fail(err)
		}
		if !killed {
			return exitFail
		}
		return exitOK
	}
	t, err := r.sweep()
	if err != nil {
		return fail(err)
	}
	if !o.List {
		fmt.Fprintf(stdout, "mutate %s: %d killed (%d by other packages), %d by the memory cap, %d by the time limit, %d survived, %d did not compile; %s\n",
			o.Pkg, t.Killed, t.Elsewhere, t.OOM, t.Timeout, t.Survived, t.NoCompile, time.Since(start).Round(time.Second))
	}
	return exitOK
}

// listPackages fills what the second pass runs: every package but bookx, which
// exists only in the reference and tests nothing of the compiler, unless it is
// the one under test.
func (r *mutateRun) listPackages() error {
	cmd := exec.CommandContext(r.ctx, "go", "list", "./...")
	cmd.Dir, cmd.Env = r.tree, r.env
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go list: %w", err)
	}
	for _, p := range strings.Fields(string(out)) {
		if !strings.HasSuffix(p, "/cmd/bookx") || strings.HasSuffix(r.opts.Pkg, "cmd/bookx") {
			r.pkgs = append(r.pkgs, p)
		}
	}
	return nil
}
