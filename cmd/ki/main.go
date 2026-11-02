// Command ki is the command line of the Ki language. All the work happens in
// package driver; main only connects it to the process: the arguments, the
// three standard streams and the exit code.
package main

import (
	"os"

	"github.com/aldezex/kilang/internal/driver"
	"github.com/aldezex/kilang/kirt/out"
)

// main writes through the Writers of kirt/out, which buffer, so it ends with
// out.Exit and never with os.Exit: os.Exit skips deferred calls, and what is
// still in the buffers would be lost.
func main() {
	code := driver.Main(os.Args[1:], os.Stdin, out.Stdout, out.Stderr)
	out.Exit(code)
}
