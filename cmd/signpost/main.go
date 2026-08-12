// Command signpost compiles a repository into a structural map an agent can read
// before it starts work.
//
// `signpost build` writes the bundle to .signpost/, which is the output the rest of
// the design is about, and `signpost verify` is what makes it trustworthy: it exits
// non-zero when the committed bundle no longer matches the tree. `graph show` and
// `graph export` run the same pipeline and report or render what it found rather than
// committing it — useful on their own, and the way to check the pipeline against a real
// repository without writing to it. `view` renders the same graph in a browser, served
// from this machine for as long as the command runs and written nowhere.
//
// Every command shares one analysis path (see pipeline.go), deliberately: a command
// that analysed the repository differently from `build` would report something
// `build` does not produce.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/3rg0n/signpost/internal/telemetry"
)

// version is stamped at link time by the release workflow
// (-ldflags "-X main.version=v0.1.0"). The default says "not from a release",
// which is what a build from a working tree is.
var version = "dev"

// command is one subcommand, or a group of them. Each leaf owns its own FlagSet,
// which is what makes `signpost graph export -format dot .` parse the way a user
// expects: Go's top-level flag package stops at the first non-flag argument, so a
// single global FlagSet would reject flags written after the subcommand.
//
// A group sets subs and leaves run nil. The two are exclusive, and dispatch relies
// on it: a group name is never itself an action.
type command struct {
	name    string
	summary string
	// run receives the arguments after the subcommand name. Nil on a group.
	run func(args []string, out, errOut io.Writer) error
	// subs is non-empty on a group, and the group's own name does nothing but
	// print these.
	subs []command
	// was names the subcommand that this group's bare name used to do, for a group
	// that was a command in a released version. It exists for one message: `signpost
	// graph .` used to analyse a repository, and now `.` is an unrecognised
	// subcommand, which is a true but useless thing to tell somebody.
	was string
}

// commands is the whole surface, and its shape is a decision rather than a
// convenience: a noun with more than one operation becomes a group, a noun with one
// stays flat, and a group's own name is never an action of its own. That is gh's rule
// — `gh issue list` and `gh pr create` group, `gh browse` and `gh api` do not, and
// bare `gh issue` prints help rather than guessing a default — and it is why `graph`
// gained `show`: it used to be both a runnable command and, once `export` belonged
// beside it, the obvious place to put it. A name that is sometimes an action and
// sometimes a namespace has to be learned twice.
//
// `build` and `verify` stay flat deliberately. Neither is an operation on an
// addressable resource — there is one bundle per repository, not a collection to
// list — and a bare `build` is the convention every adjacent tool already set: `go
// build`, `cargo build`, `docker build`. Grouping them under a noun would cost the
// tool's primary command a word to buy consistency nobody asked for.
//
// `view` is flat for the same reason, and it is the case that tests the rule rather than
// restating it: it renders the same graph `graph export` does, so it reads like a third
// sibling under `graph`. It is not one. `graph`'s two subcommands both write a rendering
// to a stream and exit; `view` binds a port and runs until interrupted, which is a
// different thing to invoke and a different thing to stop. Grouping on subject matter
// rather than on operation is what produces `docker container run`.
//
// The verbs still to come fit without moving anything: `ask why` and `ask path` are a
// group because they are siblings, and `hooks install` likewise.
func commands() []command {
	return []command{
		{name: "build", summary: "write the knowledge bundle to .signpost/", run: runBuild},
		{name: "verify", summary: "check .signpost/ against the repository; non-zero if stale", run: runVerify},
		{name: "graph", summary: "report or render the structure signpost found", was: "show", subs: []command{
			{name: "show", summary: "analyse a repository and report its structure", run: runGraphShow},
			{name: "export", summary: "render the graph as mermaid, dot, graphml, or json", run: runExport},
		}},
		{name: "view", summary: "serve the graph on 127.0.0.1 and open a browser", run: runView},
		{name: "init", summary: "write the CI files that keep the bundle honest", subs: []command{
			{name: "github", summary: "scaffold the GitHub Actions workflow and .signpost.yml", run: runInitGitHub},
		}},
		{name: "model", summary: "inspect the configured model backend", subs: []command{
			{name: "check", summary: "send one request to the configured backend and report what came back", run: runModelCheck},
		}},
		{name: "hooks", summary: "manage the optional local git hook", subs: []command{
			{name: "install", summary: "add a post-commit hook that reports a stale bundle", run: runHooksInstall},
			{name: "uninstall", summary: "remove signpost's lines from the post-commit hook", run: runHooksUninstall},
			{name: "run", summary: "report whether .signpost/ is behind the code; what the hook calls", run: runHooksRun},
		}},
		{name: "version", summary: "print the version", run: runVersion},
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
	// Telemetry is initialised here, before anything is parsed, because this is the one
	// function every path goes through — putting it in runBuild would leave `verify` and
	// `graph export` uninstrumented, and those run in CI too. Off unless
	// SIGNPOST_ENABLE_TELEMETRY asks for it, and the deferred flush is a no-op when it
	// did not (ADR 0014).
	//
	// Deferred rather than called at the end: a command returning an error still returns
	// through here, and a trace that only arrives on success is missing exactly the runs
	// somebody wanted to look at.
	defer telemetry.Init(context.Background(), errOut, version)()

	// -v is handled here rather than in dispatch because it is the one thing that is a
	// flag rather than a command; `signpost version` is the command, and this is the
	// spelling everyone tries first.
	if len(args) == 1 && (args[0] == "-v" || args[0] == "--version") {
		return runOr(errOut, func() error { return runVersion(nil, out, errOut) })
	}
	root := command{subs: commands()}
	return runOr(errOut, func() error { return dispatch(root, nil, args, out, errOut) })
}

// dispatch routes args into a group or to a leaf command, and it is the whole of the
// CLI's routing: the top level and every group go through this one function. The top
// level is a group with no name, which is what makes that literally true rather than
// nearly true.
//
// That is the point of it. The previous version had `model` implementing its own
// dispatch, its own help printer, and its own unknown-subcommand message, so a second
// group meant a second copy, and `signpost -h` and `signpost model -h` had two places
// to drift apart in.
//
// parents is the command path already consumed, which is what lets usage and errors
// name the invocation the user actually typed. Recursion means a group inside a group
// costs no new code, though nothing needs one yet.
func dispatch(node command, parents, args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		// Nothing to run. Not the same as asking for help: the user wanted an action
		// and named none, so this goes to stderr and exits 2, where `-h` goes to
		// stdout and exits 0.
		usage(errOut, node.subs, parents)
		return errReported
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(out, node.subs, parents)
		return nil
	}
	for _, c := range node.subs {
		if c.name != args[0] {
			continue
		}
		if c.run != nil {
			return c.run(args[1:], out, errOut)
		}
		return dispatch(c, append(parents, c.name), args[1:], out, errOut)
	}
	return unknown(errOut, node, parents, args[0])
}

// moved records verbs that used to be spelled differently, so somebody typing the old
// spelling from memory is told where it went rather than being told it does not exist.
// Together with command.was, this is the one place this CLI does better than the one it
// borrowed its shape from: `gh` renames a command and the old spelling simply becomes
// unknown.
//
// v0.1.0 shipped `signpost export`, and it did not keep a working alias. The release is
// public but nothing depends on it yet, and an alias is a second spelling to document,
// test, and keep working forever — `gh alias` grew into a whole subsystem. A rename note
// costs one line, changes nothing about dispatch, and can be deleted once the old
// spelling is out of everybody's shell history.
var moved = map[string]string{
	"export": "graph export",
}

// unknown reports a verb this level does not have, and tries in order: this group used
// to be a command, then a rename, then a near-miss, then nothing. The order is by
// certainty — the first two are facts and the third is a guess, and printing a guess
// when a fact was available would be worse than either.
func unknown(errOut io.Writer, node command, parents []string, name string) error {
	p := newPrinter(errOut)
	invocation := strings.Join(append([]string{"signpost"}, parents...), " ")

	// A group that used to be a command, handed what was probably its old argument.
	// `signpost graph .` is not a typo and `.` is not a subcommand somebody meant to
	// type; the whole verb moved under itself, and saying "unknown command \".\"" would
	// send the reader looking for a mistake they did not make.
	if node.was != "" && nearest(node.subs, name) == "" {
		p.printf("%s is a group of commands, and what it used to do is now `%s %s`.\n",
			invocation, invocation, node.was)
		p.printf("\n")
		usage(errOut, node.subs, parents)
		return errReported
	}

	p.printf("%s: unknown command %q\n", invocation, name)
	if to, ok := moved[name]; ok && len(parents) == 0 {
		p.printf("\n%q is now `signpost %s`.\n", name, to)
		return reported(p)
	}
	if did := nearest(node.subs, name); did != "" {
		p.printf("\nDid you mean `%s %s`?\n", invocation, did)
		return reported(p)
	}
	p.printf("\n")
	usage(errOut, node.subs, parents)
	return errReported
}

// reported returns a write failure if there was one, and otherwise the marker saying
// runOr must not print anything more.
func reported(p *printer) error {
	if err := p.Err(); err != nil {
		return err
	}
	return errReported
}

// nearest returns the one command name within a typo's distance of what was typed, or
// "" if there is no single obvious candidate. One rather than a list, and "" rather
// than a best guess: a suggestion is only worth printing when it is almost certainly
// right, and two candidates means it is not.
func nearest(cmds []command, name string) string {
	var found string
	for _, c := range cmds {
		if editDistance(c.name, name) > 2 {
			continue
		}
		if found != "" {
			return ""
		}
		found = c.name
	}
	return found
}

// editDistance is Levenshtein, two rows rather than a full matrix. Hand-written
// because a typo hint is not worth a dependency (ADR 0002), and it is eight lines.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// runOr maps an error to an exit code.
//
// The split is what lets CI tell a broken invocation from a broken repository: 2
// means the command line was wrong and re-running it the same way will fail the
// same way; 1 means signpost ran and what it found or read was the problem.
//
// **Requested help is not a misuse.** `-h` on any command exits 0, because the user
// asked a question and got the answer — the same as `git --help` and `go help`. This
// is stated here because it is easy to get wrong in exactly one place: Go's flag
// package returns flag.ErrHelp for `-h`, which arrives looking like every other parse
// failure, and letting it fall through would make `signpost graph show -h` exit 2
// while `signpost graph -h` exits 0. Help is either a question or a misuse; it cannot
// be one at the top level and the other one level down.
//
// A flag-parse failure and errReported do land in the misuse category, and neither is
// re-printed: the flag package has already written its message and the command's usage
// to errOut, dispatch has already printed a rename note or a suggestion, and a second
// summary underneath reads like two separate problems.
func runOr(errOut io.Writer, fn func() error) int {
	err := fn()
	switch {
	// errPending joins the zero cases because it is not a failure — it is verify reporting
	// differences whose only remedy is the rebuild that runs after the branch merges, and
	// design §4.6 makes the exit code mean *whether the reader must act*. It is an error value
	// at all so that the one caller for whom the remedy does exist — the post-commit hook, on a
	// machine where nothing is going to merge — can tell that case apart without re-deriving it.
	// See runVerify.
	case err == nil, errors.Is(err, flag.ErrHelp), errors.Is(err, errPending):
		return 0
	case errors.Is(err, errFlagParse), errors.Is(err, errReported):
		return 2
	case errors.Is(err, errUsage):
		newPrinter(errOut).printf("signpost: %v\n", err)
		return 2
	}
	newPrinter(errOut).printf("signpost: %v\n", err)
	return 1
}

// errUsage marks an error the user can fix by re-invoking. errFlagParse and
// errReported mark the same thing for an error that has already been reported —
// errFlagParse by the flag package, errReported by dispatch, which prints a rename
// note, a typo hint, or usage and would only make things worse by having runOr print
// a summary of it underneath.
var (
	errUsage     = errors.New("usage")
	errFlagParse = errors.New("invalid flags")
	errReported  = errors.New("reported")
)

// helpStream picks where a leaf command's usage text goes: stdout when the user asked
// for it, stderr when it accompanies a parse error. Groups get this for free because
// dispatch calls usage with the writer it wants; a leaf's usage is called by the flag
// package, which offers no way to tell the two cases apart.
//
// `signpost graph show -h | less` has to show something, and before this it printed the
// whole of its help to stderr and exited 2, so a pipe got nothing and a shell got a
// failure. Requested help is an answer, and an answer goes to stdout.
//
// The scan is only choosing a stream, never deciding whether to print — the flag package
// still does that. So the one false positive it can have is harmless: in
// `-ignore -h`, `-h` is `-ignore`'s value, flag parses it, usage is never called, and
// nothing is written to the stream this picked.
func helpStream(args []string, out, errOut io.Writer) io.Writer {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "-help" {
			return out
		}
	}
	return errOut
}

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

// usage prints one level of the command tree. Groups are marked, so `signpost -h`
// says that `graph` takes a subcommand rather than leaving the reader to try it and
// find out.
//
// The same function serves the root and every group, which is the reason `graph` and
// `model` cannot describe themselves inconsistently: there is one format, and adding a
// group cannot forget to follow it.
func usage(w io.Writer, cmds []command, parents []string) {
	p := newPrinter(w)
	path := strings.Join(append([]string{"signpost"}, parents...), " ")
	if len(parents) == 0 {
		p.printf("signpost %s — compile a repository into a map an agent can read\n\n", version)
	}
	// `[args]` rather than `[flags] [path]`, because this line covers several commands
	// at once and they do not agree: `build` takes a path, `model check` takes none.
	// The leaf's own -h states its arguments exactly, and this line's job is only to
	// say that a command name comes next.
	p.printf("usage: %s <command> [args]\n\n", path)

	width := 0
	for _, c := range cmds {
		if len(c.name) > width {
			width = len(c.name)
		}
	}
	for _, c := range cmds {
		p.printf("  %-*s  %s", width, c.name, c.summary)
		if c.run == nil {
			p.printf(" (takes a subcommand)")
		}
		p.printf("\n")
	}
	p.printf("\nRun `%s <command> -h` for a command's flags.\n", path)
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
