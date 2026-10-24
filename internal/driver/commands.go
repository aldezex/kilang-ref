package driver

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// command is one word ki understands after its own name, such as "version",
// together with what ki does when it sees it.
type command struct {
	Name  string
	Usage string                              // one line: "ki version"
	Run   func(s *Session, args []string) int // args are the words after the command name
}

// commands holds every command ki has, in registration order. Main looks the
// first argument up here, and the "commands:" line of the usage text lists
// the names in this order, "--version" first.
var commands = []command{
	versionCommand("--version", "ki --version"),
	versionCommand("version", "ki version"),
}

// versionCommand returns a command, called name, that prints ki's version:
// "ki version" does the same as "ki --version".
//
// Run needs its own command to report a usage error with the right name and
// usage line, but it cannot look itself up in commands: commands is
// initialized from Run, and Go rejects a package variable whose initializer
// refers back to it ("initialization cycle"). So Run is a closure that
// captures a pointer to its command. The table holds a copy of that command,
// with the same Name, Usage and Run.
func versionCommand(name, usageLine string) command {
	c := &command{Name: name, Usage: usageLine}
	c.Run = func(s *Session, args []string) int { return runVersion(s, c, args) }
	return *c
}

// runVersion prints ki's version and, in parentheses, the version of Go that
// built it. It accepts no flags and no arguments; it still parses args with a
// flag.FlagSet, so a stray flag gets the same message as in any other command.
// The one exception is a request for help (-h, -help or --help): that is not a
// mistake in the command line, so ki answers it with the usage line on stdout
// and exit code 0.
func runVersion(s *Session, c *command, args []string) int {
	fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
	// ContinueOnError makes Parse return the error instead of ending the
	// process, and io.Discard drops the help text that flag would print on its
	// own: usageError reports the error in ki's format instead.
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printOut(s, c, "usage: "+c.Usage+"\n")
		}
		return usageError(s, c, err)
	}
	if fs.NArg() > 0 {
		return usageError(s, c, fmt.Errorf("unexpected argument %q", fs.Arg(0)))
	}
	return printOut(s, c, fmt.Sprintf("ki %s (%s)\n", Version, goVersion()))
}

// printOut writes text to stdout, the answer that c exists to give. If the
// write fails (a full disk, say) ki reports it on stderr and returns exit code
// 1, so a script that redirects the output does not take a lost answer for a
// successful one. A closed stdout pipe never gets this far: the Go runtime
// ends the process with SIGPIPE (status 141 in a shell) before Write returns.
func printOut(s *Session, c *command, text string) int {
	if _, err := io.WriteString(s.Stdout, text); err != nil {
		// stderr is the last place left to report the failure, and if it fails
		// too there is nowhere else to say so.
		_, _ = fmt.Fprintf(s.Stderr, "ki %s: cannot write to stdout: %v\n", c.Name, err)
		return 1
	}
	return 0
}

// usageError reports a command line that c cannot accept. It writes
// "ki <name>: <err>" and then c's usage line to stderr, and returns exit code
// 2, which means the command line itself is wrong.
func usageError(s *Session, c *command, err error) int {
	// If stderr cannot be written there is no other place to report that, and
	// the exit code already says the command line was wrong.
	_, _ = fmt.Fprintf(s.Stderr, "ki %s: %v\nusage: %s\n", c.Name, err, c.Usage)
	return 2
}
