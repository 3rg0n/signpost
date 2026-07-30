// Command signpost compiles a repository into a structural map an agent can read
// before it starts work.
//
// `signpost build` writes the bundle to .signpost/, which is the output the rest of
// the design is about. `graph` and `export` run the same pipeline and report or
// render what it found rather than committing it — useful on their own, and the way
// to check the pipeline against a real repository without writing to it.
//
// Every command shares one analysis path (see pipeline.go), deliberately: a command
// that analysed the repository differently from `build` would report something
// `build` does not produce.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// version is stamped at link time by the release workflow
// (-ldflags "-X main.version=v0.1.0"). The default says "not from a release",
// which is what a build from a working tree is.
var version = "dev"

// command is one subcommand. Each owns its own FlagSet, which is what makes
// `signpost export --format dot .` parse the way a user expects: Go's top-level
// flag package stops at the first non-flag argument, so a single global FlagSet
// would reject flags written after the subcommand.
type command struct {
	name    string
	summary string
	// run receives the arguments after the subcommand name.
	run func(args []string, out, errOut io.Writer) error
}

func commands() []command {
	return []command{
		{"build", "write the knowledge bundle to .signpost/", runBuild},
		{"graph", "analyse a repository and report its structure", runGraph},
		{"export", "render the graph as mermaid, dot, graphml, or json", runExport},
		{"version", "print the version", runVersion},
	}
}

func main() {
	if code := run(os.Args[1:], os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

// run is main's testable body: everything reaches the process through it, so a
// test can drive the real CLI without a subprocess.
func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		usage(errOut)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(out)
		return 0
	case "-v", "--version":
		return runOr(errOut, func() error { return runVersion(nil, out, errOut) })
	}
	for _, c := range commands() {
		if c.name == args[0] {
			return runOr(errOut, func() error { return c.run(args[1:], out, errOut) })
		}
	}
	newPrinter(errOut).printf("signpost: unknown command %q\n\n", args[0])
	usage(errOut)
	return 2
}

// runOr maps an error to an exit code.
//
// The split is what lets CI tell a broken invocation from a broken repository: 2
// means the command line was wrong and re-running it the same way will fail the
// same way; 1 means signpost ran and what it found or read was the problem.
//
// flag.ErrHelp and a flag-parse failure both land in the first category. Neither is
// re-printed, because the flag package has already written the message and the
// command's usage to errOut, and a second copy of the same error reads like two
// problems.
func runOr(errOut io.Writer, fn func() error) int {
	err := fn()
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp), errors.Is(err, errFlagParse):
		return 2
	case errors.Is(err, errUsage):
		newPrinter(errOut).printf("signpost: %v\n", err)
		return 2
	}
	newPrinter(errOut).printf("signpost: %v\n", err)
	return 1
}

// errUsage marks an error the user can fix by re-invoking. errFlagParse marks the
// same thing for an error the flag package has already reported itself.
var (
	errUsage     = errors.New("usage")
	errFlagParse = errors.New("invalid flags")
)

// parseFlags reports a flag error in the one category runOr must not print twice.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return fmt.Errorf("%w: %v", errFlagParse, err)
	}
	return nil
}

func usage(w io.Writer) {
	p := newPrinter(w)
	p.printf("signpost %s — compile a repository into a map an agent can read\n\n", version)
	p.printf("usage: signpost <command> [flags] [path]\n\n")
	for _, c := range commands() {
		p.printf("  %-10s %s\n", c.name, c.summary)
	}
	p.printf("\nRun `signpost <command> -h` for a command's flags.\n")
}

func runVersion(_ []string, out, _ io.Writer) error {
	_, err := fmt.Fprintln(out, version)
	return err
}

// repoPath returns the path a command should analyse: the single positional
// argument, or the working directory.
func repoPath(fs *flag.FlagSet) (string, error) {
	switch fs.NArg() {
	case 0:
		return ".", nil
	case 1:
		return fs.Arg(0), nil
	}
	return "", fmt.Errorf("%w: expected at most one path, got %d", errUsage, fs.NArg())
}
