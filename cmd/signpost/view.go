package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/3rg0n/signpost/internal/export"
	"github.com/3rg0n/signpost/internal/hook"
	"github.com/3rg0n/signpost/internal/view"
)

// `signpost view` opens the graph in a browser, served from this machine.
//
// It is the same pipeline every other command runs and the same JSON `graph export
// -format json` writes — this adds a listener and a document, and nothing else. In
// particular it writes nothing: no graph.json, no bundle, nothing in the repository.
// A `view` that left an artifact behind would be the stale second copy ADR 0008
// declined to commit, created by the one command whose output is transient.
//
// It also does not require `build` to have run. The graph comes from this invocation's
// analysis, so `view` works in a repository that has never had a bundle — which is the
// case where somebody most wants to look at the structure before deciding whether to
// commit a map of it.
func runView(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost view [flags] [path]\n")
		u.printf("\nAnalyse the repository and serve the graph on 127.0.0.1, opening a\n" +
			"browser. Runs until interrupted, and writes nothing anywhere.\n")
		u.printf("\nThe graph is read once, before the listener opens: the page describes the\n" +
			"tree as it was when the command started. Restart it to see a change.\n")
		u.printf("\nLoopback only, and there is no flag to change that — the page lists every\n" +
			"module and file signpost found, which is this repository's structure.\n")
		u.printf("\nFlags:\n")
		fs.PrintDefaults()
	}
	var pf pipelineFlags
	pf.register(fs)
	port := fs.Int("port", view.DefaultPort,
		"port on 127.0.0.1; an explicit one that is taken is an error, the default falls back")
	noOpen := fs.Bool("no-open", false,
		"print the URL and serve, without opening a browser")
	quiet := fs.Bool("quiet", false, "suppress the coverage report on stderr")
	// The same key `build` and `verify` share, and here it does one thing: name where a
	// file in the detail panel can be read. Nothing is stamped, so a wrong value costs a
	// broken link rather than a bundle that fails verification.
	repo := fs.String("repo", "",
		"repository name, e.g. github.com/org/repo; makes the file lists links")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	applyConfig(fs, cfg, &pf)
	applyRepo(fs, cfg, repo)

	// Interrupt is wired before the analysis, not after: a walk of a large repository
	// takes seconds, and a ctrl-c during it should stop the command rather than be
	// swallowed until a listener exists to be shut down.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	a, err := analyse(ctx, path, pf)
	if err != nil {
		return err
	}
	if !*quiet {
		reportCoverage(errOut, a)
	}
	// Set-ness, not a comparison against DefaultPort: `-port 7777` and an unpassed flag
	// carry the same number and must not behave the same way when it is taken. The same
	// reason applyConfig reads fs.Visit rather than the value.
	opts, err := viewOptions(ctx, a, path, *repo, *port, setFlags(fs)["port"])
	if err != nil {
		return err
	}
	return view.Serve(ctx, opts, out, !*noOpen)
}

// viewOptions turns the analysis into everything the page shows.
//
// Split out of runView because it is the half with a checkable contract: runView's rest is
// flag registration and a blocking Serve, and a test of the whole command would have to
// leave a server running to see any of this. A field wired to nothing here is not a
// compile error and not visible in internal/view's tests — it renders as a page that loads
// with an empty graph, which is exactly how the -port bug got shipped.
func viewOptions(ctx context.Context, a *analysis, path, repo string, port int, portWasAsked bool) (view.Options, error) {
	// Clusters before export, as every other rendering command does: the JSON carries a
	// cluster per node and the assignment is computed lazily.
	a.Graph().Clusters()

	var buf bytes.Buffer
	if err := export.Write(&buf, a.Graph(), export.FormatJSON); err != nil {
		return view.Options{}, err
	}
	nodes, edges := a.Graph().Counts()

	return view.Options{
		Root:         a.Discovered.Root,
		Title:        viewTitle(repo, a.Discovered.Root),
		Commit:       shortSHA(a),
		RepoBase:     repoBase(repo, a),
		Graph:        buf.Bytes(),
		Nodes:        nodes,
		Edges:        edges,
		Notes:        viewNotes(ctx, a, path),
		Port:         port,
		PortWasAsked: portWasAsked,
	}, nil
}

// viewTitle heads the page with the repository's name, or the directory's.
//
// The declared name first because it is the one a reader recognises: several checkouts
// of different repositories can all be in a directory called `src`, and a tab saying
// `src` names none of them.
func viewTitle(repo, root string) string {
	if repo != "" {
		return repo
	}
	if base := filepath.Base(root); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return "this repository"
}

// shortSHA is the commit the analysis describes, abbreviated, or empty.
//
// Empty rather than a placeholder wherever history was not read. The page prints
// nothing for it and the notes already say why, so a "(unknown)" here would be the
// same admission twice, in the one spot a reader is looking for a fact.
func shortSHA(a *analysis) string {
	if a.History == nil || !a.History.Available || a.History.Head.SHA == "" {
		return ""
	}
	const short = 12
	if len(a.History.Head.SHA) < short {
		return a.History.Head.SHA
	}
	return a.History.Head.SHA[:short]
}

// linkHosts are the forges whose file URLs this command will build.
//
// A short allowlist rather than a guess from the hostname, because the path shape is
// not universal: these three serve a file at `/<owner>/<repo>/blob/<ref>/<path>`,
// while Bitbucket uses `/src/`, cgit uses `/tree/`, and Gerrit uses something else
// again. A link built on the wrong shape is a 404 on every file in the panel, which is
// worse than the plain filename a reader gets when this returns nothing. A host that
// belongs here can be added; guessing cannot be undone from the page.
//
// Enterprise installations of these products are common on their own hostnames and are
// deliberately not matched. There is nothing in a repository name that distinguishes
// `git.example.com/org/repo` running GitHub Enterprise from one running anything else,
// and this has no way to ask.
var linkHosts = []string{"github.com", "gitlab.com", "codeberg.org"}

// repoBase builds the prefix a file path is appended to, or returns empty.
//
// Empty is a supported answer and the common one: no -repo, an unrecognised host, or no
// commit to pin to. graph.js renders a filename as text when there is no base, so the
// panel still says which files are in a module — it just does not offer to open them.
//
// The commit rather than a branch name, which is the same argument ADR 0007 makes for
// the bundle's own stamp: the graph describes one tree, and a link to `main` would
// point at a file that has since moved or gone, from a page claiming to describe the
// commit where it was.
func repoBase(repo string, a *analysis) string {
	sha := ""
	if a.History != nil && a.History.Available {
		sha = a.History.Head.SHA
	}
	if repo == "" || sha == "" {
		return ""
	}
	host, rest, ok := strings.Cut(repo, "/")
	if !ok || rest == "" {
		return ""
	}
	for _, h := range linkHosts {
		if !strings.EqualFold(host, h) {
			continue
		}
		// Assembled from the matched literal host and the rest of the declared name. The
		// path segment is not escaped here and does not need to be: graph.js appends an
		// encodeURI'd file path to this, and a repository name containing a character that
		// would steer the URL is a name that does not resolve on any of these hosts.
		return "https://" + h + "/" + rest + "/blob/" + sha + "/"
	}
	return ""
}

// viewNotes says what was already out of step before the server started.
//
// The page is a picture of the tree as analysed, so what a reader needs alongside it is
// whether that tree is the one the committed bundle describes. A bundle behind the code
// is the stale note the user asked for, and it is worth stating here specifically
// because `view` is the command somebody runs *instead of* opening the bundle — so the
// staleness a bundle page would have shown them never comes up.
//
// hook.Fast rather than a verify pass: it is two `git log` calls against topology, where
// verify re-renders every page and costs about a second on this repository. `view` has
// already spent seconds on the analysis and is about to open a browser, and a note that
// is nearly always "nothing to say" does not get to add another second to that. What it
// costs is precision in one direction — a commit that touched only LICENSE reports the
// bundle as behind — and the note says "behind by n commits", which is exactly what was
// measured.
func viewNotes(ctx context.Context, a *analysis, path string) []string {
	var notes []string
	if a.History == nil {
		notes = append(notes, "Git history was not read, so there is no commit stamp and no "+
			"co-change edges. That was asked for with -no-history.")
	} else if !a.History.Available {
		notes = append(notes, "Git history was not read: "+a.History.Reason+
			". There is no commit stamp, and no co-change edges in this graph.")
	} else if a.History.Reason != "" {
		// Shallow or truncated: the numbers on the page are real but describe less history
		// than a reader will assume, and Reason names the fix.
		notes = append(notes, a.History.Reason)
	}

	st, err := hook.Fast(ctx, path)
	switch {
	case err != nil:
		// Not reported. This is a note about a second artifact, and failing to read it
		// says nothing about the graph on the page.
	case st.Stale():
		notes = append(notes, fmt.Sprintf(
			"The committed bundle in %s/ is behind this tree by %d commit(s), so it "+
				"describes an older graph than the one below. Run `signpost build` to catch it up.",
			hook.BundleDir, st.Behind))
	}
	return notes
}
