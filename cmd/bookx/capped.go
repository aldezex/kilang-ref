package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// This file is what the commands that run code share: `bookx doctest` runs the
// book's commands, `bookx mutate` broken compilers and `bookx cascade` broken
// programs. Any of them can allocate without bound, and three such runs killed
// this machine on 2026-10-27, so each one goes inside a cgroup of its own with a
// memory cap, and with a time limit. A machine with no systemd cannot give that
// cap, and there the commands refuse to run.

// capCommand wraps argv in the memory cap. The name of the unit is what the
// journal shows when the kernel kills inside it, so a death can be traced to the
// run that caused it. The tests replace this variable: they cannot count on
// systemd, and they run nothing that needs a cap.
var capCommand = systemdCap

// systemdCap is capCommand outside the tests. The cap is the one of section 11
// of docs/superpowers/process/implementer.md, plus OOMPolicy=continue: by default
// systemd stops the whole unit at the first kill, so `go test` dies with no output
// and an exit code that says nothing (15, measured). With continue the kernel kills
// only the process that passed the cap, and `go test` reports `signal: killed`.
func systemdCap(unit string, argv []string) ([]string, error) {
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return nil, fmt.Errorf("the memory cap needs systemd-run: %w", err)
	}
	return append([]string{"systemd-run", "--user", "--scope", "-q", "--unit=" + unit, "-p", "MemoryMax=4G", "-p", "MemorySwapMax=0", "-p", "OOMPolicy=continue"}, argv...), nil
}

// maxCapturedOutput is how much output of a run is kept. The output arrives at
// this process, which is outside the cap, so a run that prints without end must
// not be able to fill its memory.
const maxCapturedOutput = 4 << 20

// limitedBuffer keeps the first limit bytes written to it and drops the rest.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

// Write never fails, so the process that writes keeps running until its limits
// stop it.
func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// cappedRun is one run under the memory cap and a limit of seconds.
type cappedRun struct {
	Unit    string // the name of the systemd unit
	Dir     string
	Env     []string
	Seconds string // the time limit, as `timeout` reads it
	Argv    []string
}

// Exit codes that say a run did not end by itself: what `timeout` returns when
// the time is up, and what a shell reports for a process killed by SIGKILL,
// which is how the kernel ends a process that passes the memory cap.
const (
	exitTimedOut = 124
	exitKilled   = 137
)

// run runs it and returns its stdout and stderr, merged in the order they were
// written, and its exit code as a shell would report it: 128 plus the signal
// when a signal ended it. The error is for a run that could not start. A
// cancelled ctx ends it with SIGTERM, which `timeout` hands on to what it runs.
func (r cappedRun) run(ctx context.Context) ([]byte, int, error) {
	argv, err := capCommand(r.Unit, append([]string{"timeout", "-k", "1", r.Seconds}, r.Argv...))
	if err != nil {
		return nil, 0, err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = r.Dir, r.Env
	out := &limitedBuffer{limit: maxCapturedOutput}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	err = cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return out.buf.Bytes(), 0, nil
	case errors.As(err, &ee):
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return out.buf.Bytes(), 128 + int(ws.Signal()), nil
		}
		return out.buf.Bytes(), ee.ExitCode(), nil
	case errors.Is(err, exec.ErrWaitDelay):
		return out.buf.Bytes(), 0, nil
	default:
		return nil, 0, err
	}
}

// cleanEnv is the environment of what these commands run, the one that
// ci/ci-local.sh sets: no other toolchain behind our back, none of this machine's
// Go settings, and none of the variables that change how ki behaves.
func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "GOTOOLCHAIN", "GOFLAGS", "GOENV", "GOWORK", "LC_ALL", "KI_DEBUG_PANIC_IN", "KI_STACK_MB":
		default:
			env = append(env, kv)
		}
	}
	return append(env, "GOTOOLCHAIN=local", "GOFLAGS=", "GOENV=off", "GOWORK=off", "LC_ALL=C.UTF-8")
}

// newScratch makes a directory for one run under <repo>/build/<name>, and
// returns it and the function that removes it. It first removes what a run that
// died left there, because a killed run cannot clean up after itself and the
// caches in these directories reach gigabytes. A directory whose run is alive,
// or that does not say whose it is, stays.
func newScratch(repo, name string) (dir string, done func(), err error) {
	base := filepath.Join(repo, "build", name)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", nil, err
	}
	old, _ := filepath.Glob(filepath.Join(base, "run-*"))
	for _, d := range old {
		data, err := os.ReadFile(filepath.Join(d, "pid"))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			continue
		}
		if err := os.RemoveAll(d); err != nil {
			return "", nil, err
		}
	}
	dir, err = os.MkdirTemp(base, "run-")
	if err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	return dir, func() { os.RemoveAll(dir) }, nil
}
