// Package within runs a piece of work under a deadline, for the tests that
// check that a walk of a type does not take one step for each path to the
// bottom.
//
// It imports nothing of the compiler, so any test can use it: the tests inside
// package sema (which cannot import internal/testsupport, because that imports
// internal/driver and driver imports sema) and the ones outside it alike.
package within

import (
	"testing"
	"time"
)

// Deadline is how long Run lets a piece of work run. The work it guards takes
// microseconds when it is right, so the limit is far above anything a slow
// machine or the race detector needs, and still short enough to be a failure
// and not a wait.
const Deadline = 10 * time.Second

// Run runs work and fails the test, from the goroutine of the test, if work has
// not returned after Deadline. A walk that goes down every path of a type with
// shared parts would not fail by itself: it would run until go test gives up
// after ten minutes, and then with a panic that names the test and dumps the
// stacks, but says neither which row was stuck nor why, and ends the whole
// test binary. Run turns that into a failure of this test alone, with a
// reason, the words in what. work must not fail the test or log through t:
// only the goroutine of the test may. If it is stuck it goes on running until
// the test binary exits; the test is already failed by then.
func Run(t *testing.T, what string, work func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	select {
	case <-done:
	case <-time.After(Deadline):
		t.Fatalf("%s: not finished after %v, when it should take well under a second", what, Deadline)
	}
}
