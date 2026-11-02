// bookx: not-in-book

package toolchain_test

import (
	"fmt"
	"os"
	"testing"
)

// helperEnv is the environment variable that tells TestHelperProcess what to do:
// "panic" or "exit101". Without it the test does nothing.
const helperEnv = "KI_TEST_HELPER"

// TestHelperProcess is not a test of its own: it is the program that TestExitCodes
// starts, the test binary run again with -test.run=^TestHelperProcess$. A test
// cannot observe the exit code of its own process, so it runs another and looks
// at how that one ended. In a normal run the variable is not set and the test
// returns at once.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv(helperEnv) {
	case "":
		return
	case "panic":
		panic("boom")
	case "exit101":
		defer fmt.Println("deferred")
		os.Exit(101)
	}
}
