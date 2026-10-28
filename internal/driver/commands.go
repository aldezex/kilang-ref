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
	newCommand("check", "ki check [--diagnostics=human|short|json] FILE", runCheck),
	newCommand("test", "ki test [--root DIR] [--update] PATH...", runTest),
	newCommand("emit", "ki emit tokens|ast [--diagnostics=human|short|json] FILE", runEmit),
}

// newCommand returns a command, called name, whose Run calls run with the
// command itself. run needs its own command to report a usage error with the
// right name and usage line, but it cannot look itself up in commands:
// commands is initialized from Run, and Go rejects a package variable whose
// initializer refers back to it ("initialization cycle"). So Run is a closure
// that captures a pointer to its command. The table holds a copy of that
// command, with the same Name, Usage and Run.
func newCommand(name, usageLine string, run func(s *Session, c *command, args []string) int) command {
	c := &command{Name: name, Usage: usageLine}
	c.Run = func(s *Session, args []string) int { return run(s, c, args) }
	return *c
}

// versionCommand returns a command, called name, that prints ki's version:
// "ki version" does the same as "ki --version".
func versionCommand(name, usageLine string) command {
	return newCommand(name, usageLine, runVersion)
}

// newFlagSet returns an empty FlagSet for c. ContinueOnError makes Parse return
// the error instead of ending the process, and io.Discard drops the help text
// that flag would print on its own: parseFlags reports the error in ki's format.
func newFlagSet(c *command) *flag.FlagSet {
	fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags parses args with fs. When the command line is already answered it
// returns that answer's exit code and done == true, and the command must
// return it. A request for help (-h, -help or --help) is not a mistake in the
// command line, so ki answers it with the usage line on stdout and exit code 0;
// any other error is a usage error.
func parseFlags(s *Session, c *command, fs *flag.FlagSet, args []string) (code int, done bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printUsage(s, c), true
		}
		return usageError(s, c, err), true
	}
	return 0, false
}

// fileArgs reads the words after the name of c for a command whose line is
// `[--diagnostics=human|short|json] FILE`, as check and emit are. It sets the
// session's diagnostic format from the flag and returns the path of FILE, which
// the command reads with LoadAndParse (or load, if it needs the file alone).
//
// When the command line is already answered it returns that answer's exit code
// and done == true, and the command must return it. That is the case for a
// request for help (code 0) and for a usage error (2).
func fileArgs(s *Session, c *command, args []string) (path string, code int, done bool) {
	fs := newFlagSet(c)
	diagnostics := fs.String("diagnostics", "human", "how to write diagnostics: human, short or json")
	if code, done := parseFlags(s, c, fs, args); done {
		return "", code, true
	}
	format, err := parseDiagFormat(*diagnostics)
	if err != nil {
		return "", usageError(s, c, err), true
	}
	switch fs.NArg() {
	case 0:
		return "", usageError(s, c, fmt.Errorf("missing FILE")), true
	case 1:
	default:
		return "", usageError(s, c, fmt.Errorf("unexpected argument %q", fs.Arg(1))), true
	}
	s.Format = format
	return fs.Arg(0), 0, false
}

// runVersion prints ki's version and, in parentheses, the version of Go that
// built it. It accepts no flags and no arguments; it still parses args with a
// flag.FlagSet, so a stray flag gets the same message as in any other command.
func runVersion(s *Session, c *command, args []string) int {
	fs := newFlagSet(c)
	if code, done := parseFlags(s, c, fs, args); done {
		return code
	}
	if fs.NArg() > 0 {
		return usageError(s, c, fmt.Errorf("unexpected argument %q", fs.Arg(0)))
	}
	return printOut(s, c, fmt.Sprintf("ki %s (%s)\n", Version, goVersion()))
}

// printUsage answers a request for help: the usage line of c on stdout, and
// exit code 0 unless stdout cannot be written (see printOut).
func printUsage(s *Session, c *command) int {
	return printOut(s, c, "usage: "+c.Usage+"\n")
}

// printOut writes text to stdout, the answer that c exists to give. If the
// write fails (a full disk, say) ki reports it on stderr and returns exit code
// 1, so a script that redirects the output does not take a lost answer for a
// successful one. A closed stdout pipe never gets this far: the Go runtime
// ends the process with SIGPIPE (status 141 in a shell) before Write returns.
func printOut(s *Session, c *command, text string) int {
	if _, err := io.WriteString(s.Stdout, text); err != nil {
		return writeError(s, c, err)
	}
	return 0
}

// writeError reports that c could not write its answer to stdout and returns
// exit code 1. printOut calls it for an answer written in one piece, and
// runTest for a report that it writes a line at a time.
func writeError(s *Session, c *command, err error) int {
	// stderr is the last place left to report the failure, and if it fails
	// too there is nowhere else to say so.
	_, _ = fmt.Fprintf(s.Stderr, "ki %s: cannot write to stdout: %v\n", c.Name, err)
	return 1
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
