package tests

import (
	"os"
	"testing"
)

// TestMain clears KI_DEBUG_PANIC_IN and KI_STACK_MB before any test runs. TestKi
// and TestErrorDocs run the compiler through driver.Main, which reads the first
// from the real environment, so a shell that had exported it would make every
// case that reaches the pass it names end in an ICE. Package driver does the
// same in its TestMain, and its tests of the hook set the variable themselves,
// with t.Setenv. The interpreter reads the second when `ki run` makes its
// machine, and the golden files that run deep recursions (deep_recursion.ki)
// need the default limit of the stack, not the one a shell exported.
func TestMain(m *testing.M) {
	for _, name := range []string{"KI_DEBUG_PANIC_IN", "KI_STACK_MB"} {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}
