// Package driver is the command line of ki: it reads the arguments, runs the
// command they name and turns the result into an exit code.
package driver

import (
	"fmt"
	"io"
)

// usage is what ki writes to stderr for any invocation it does not accept.
// Its second line lists every command ki has.
const usage = "usage: ki <command> [args]\ncommands: --version\n"

// Main runs the ki command line. It never calls os.Exit: it returns the exit code, so tests can call it in-process.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 1 && args[0] == "--version":
		fmt.Fprintf(stdout, "ki %s (%s)\n", Version, goVersion())
		return 0
	default:
		// Exit code 2 means the command line itself is wrong, as opposed to
		// 1, which means the program ki was given has errors or cannot be
		// read.
		fmt.Fprint(stderr, usage)
		return 2
	}
}
