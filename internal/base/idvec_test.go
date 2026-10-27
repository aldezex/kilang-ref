package base

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestIdVec(t *testing.T) {
	type cell struct{ x int }

	t.Run("push_returns_sequential_ids", func(t *testing.T) {
		var v IdVec[FileID, string]
		for want, s := range []string{"a", "b", "c"} {
			if got := v.Push(s); got != FileID(want) {
				t.Errorf("Push(%q) = %d, want %d", s, got, want)
			}
		}
		for i, want := range []string{"a", "b", "c"} {
			if got := v.Get(FileID(i)); got != want {
				t.Errorf("Get(%d) = %q, want %q", i, got, want)
			}
		}
	})

	t.Run("get_and_ptr", func(t *testing.T) {
		var v IdVec[FileID, cell]
		v.Push(cell{1})
		id := v.Push(cell{2})
		v.Ptr(id).x = 7
		if got := v.Get(id); got.x != 7 {
			t.Errorf("Get after Ptr write = %+v, want x 7", got)
		}
		if got := v.Get(0); got.x != 1 {
			t.Errorf("Get(0) = %+v, want x 1: writing through Ptr(1) touched another element", got)
		}
		// An id that does not exist must panic with the runtime's own error,
		// as the doc comments say, and not give back a zero element: that is
		// how a wrong FileID is kept from turning into an empty file. Next is
		// the first id that does not exist; 5 is far past the end.
		for _, bad := range []FileID{v.Next(), 5} {
			want := fmt.Sprintf("runtime error: index out of range [%d] with length %d", bad, v.Len())
			mustPanic(t, fmt.Sprintf("Get(%d)", bad), want, func() { v.Get(bad) })
			mustPanic(t, fmt.Sprintf("Ptr(%d)", bad), want, func() { v.Ptr(bad) })
		}
	})

	t.Run("next_is_len", func(t *testing.T) {
		var v IdVec[FileID, string]
		for n := 0; n < 3; n++ {
			if v.Len() != n || v.Next() != FileID(n) {
				t.Fatalf("before Push %d: Len = %d, Next = %d, want %d and %d", n, v.Len(), v.Next(), n, n)
			}
			if got := v.Push("x"); got != FileID(n) {
				t.Fatalf("Push returned %d, but Next had announced %d", got, n)
			}
		}
		if v.Len() != 3 || v.Next() != 3 {
			t.Errorf("after 3 pushes: Len = %d, Next = %d, want 3 and 3", v.Len(), v.Next())
		}
	})

	t.Run("all_iterates_in_order", func(t *testing.T) {
		var v IdVec[FileID, string]
		for _, s := range []string{"a", "b", "c"} {
			v.Push(s)
		}
		var ids []FileID
		var got string
		for i, x := range v.All() {
			ids = append(ids, i)
			got += x
		}
		if got != "abc" {
			t.Errorf("All yielded values %q, want \"abc\"", got)
		}
		if len(ids) != 3 || ids[0] != 0 || ids[1] != 1 || ids[2] != 2 {
			t.Errorf("All yielded ids %v, want [0 1 2]", ids)
		}

		var empty IdVec[FileID, string]
		for range empty.All() {
			t.Errorf("All on an empty IdVec yielded an element")
		}

		// Stopping early must stop the sequence. Go itself enforces it: if the
		// iterator calls yield again after the loop body has left the loop with
		// break, the runtime panics with "range function continued iteration
		// after function for loop body returned false". The body can never run
		// a second time, so there is nothing to count; the test only recovers
		// that panic, so that a broken All fails here with a message instead of
		// taking down the whole test binary.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("All kept iterating after the loop body left the loop with break: %v", r)
				}
			}()
			for range v.All() {
				break
			}
		}()
	})

	t.Run("distinct_id_types_do_not_mix", func(t *testing.T) {
		// Only the go build below needs the fixture, but go test caches a
		// passing result for as long as the files that the test itself opens
		// are unchanged. Opening the fixture here makes an edit to it rerun
		// the test, instead of leaving a cached pass for a program that now
		// compiles. It also fails clearly if the fixture is missing.
		if _, err := os.ReadFile("testdata/badids/main.go"); err != nil {
			t.Fatal(err)
		}
		// -buildvcs=false: before it compiles a main package, go build asks git
		// for the state of the checkout, and stops with "error obtaining VCS
		// status" when git refuses it (a directory that belongs to another
		// user). That would fail this test for a reason that has nothing to do
		// with typed ids, and the result is thrown away anyway (-o os.DevNull),
		// so there is no version stamp worth asking git for.
		cmd := exec.Command("go", "build", "-buildvcs=false", "-o", os.DevNull, "./testdata/badids")
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			t.Fatalf("testdata/badids compiled, but indexing an IdVec[FileID, string] with an otherID must not")
		}
		// "cannot use" is the compiler's wording for a type mismatch, and
		// the name of the wrong type shows the failure is the one we mean
		// and not, say, a missing package.
		for _, want := range []string{"cannot use", "otherID"} {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr does not contain %q:\n%s", want, stderr.String())
			}
		}
	})
}
