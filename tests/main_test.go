package tests

import (
	"os"
	"testing"
)

// TestMain clears KI_DEBUG_PANIC_IN before any test runs. TestKi and
// TestErrorDocs run the compiler through driver.Main, which reads the variable
// from the real environment, so a shell that had exported it would make every
// case that reaches the pass it names end in an ICE. Package driver does the
// same in its TestMain, and its tests of the hook set the variable themselves,
// with t.Setenv.
func TestMain(m *testing.M) {
	if err := os.Unsetenv("KI_DEBUG_PANIC_IN"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
