// Package driver is the command line of ki: it reads the arguments, runs the
// command they name and turns the result into an exit code.
package driver

import (
	"fmt"
	"io"
	"strings"
)

// Main runs the ki command line. It never calls os.Exit: it returns the exit code, so tests can call it in-process.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	s := NewSession(stdin, stdout, stderr)
	if len(args) > 0 {
		for i := range commands {
			if commands[i].Name == args[0] {
				return commands[i].Run(s, args[1:])
			}
		}
	}
	// Exit code 2 means the command line itself is wrong, as opposed to 1,
	// which means the program ki was given has errors or cannot be read, or
	// that ki cannot write the answer it was asked for.
	//
	// Only a command answers a request for help (see runVersion): "ki -h",
	// "ki --help" and "ki help" name no command, so they are command lines ki
	// does not know and end here, with the usage on stderr and exit code 2.
	// A help for ki as a whole is the book's first exercise ("ki help"); until
	// someone adds that command, these three are unknown.
	// If stderr cannot be written there is no other place to report that, and
	// the exit code already says the command line was wrong.
	_, _ = fmt.Fprint(s.Stderr, usage())
	return 2
}

// usage returns what ki writes to stderr when it gets no command or one it
// does not know. Its second line lists the name of every command in
// commands, in registration order.
func usage() string {
	names := make([]string, len(commands))
	for i, c := range commands {
		names[i] = c.Name
	}
	return "usage: ki <command> [args]\ncommands: " + strings.Join(names, ", ") + "\n"
}
