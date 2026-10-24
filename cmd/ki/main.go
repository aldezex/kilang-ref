// Command ki is the command line of the Ki language. All the work happens in
// package driver; main only connects it to the process: the arguments, the
// three standard streams and the exit code.
package main

import (
	"os"

	"github.com/aldezex/kilang/internal/driver"
)

func main() { os.Exit(driver.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
