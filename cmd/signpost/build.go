package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/3rg0n/signpost/internal/model"
	"github.com/3rg0n/signpost/internal/okf"
	"github.com/3rg0n/signpost/internal/semantic"
	"github.com/3rg0n/signpost/internal/vcs"
)

// `signpost build` writes the OKF bundle to .signpost/.
//
// This is the command the other two exist to support: `graph` and `export` report what the
// pipeline found, and this one commits it to a durable artifact a reader will find without
// signpost installed.
//
// Two properties are the command's whole contract, and both are enforced in internal/okf
// rather than here: the same commit produces the same bytes, and text a human wrote outside
// the managed markers is never touched. This file's job is to say what happened, in enough
// detail that a run which preserved an edit or downgraded a verification is visible rather
// than buried in a file count.
func runBuild(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		u := newPrinter(errOut)
		u.printf("usage: signpost build [flags] [path]\n")
		u.printf("\nWrite the knowledge bundle to %s/ in the repository.\n\nFlags:\n", okf.BundleDir)
		fs.PrintDefaults()
	}
	var pf pipelineFlags
	pf.register(fs)
	quiet := fs.Bool("quiet", false, "suppress the coverage report on stderr")
	// The `resource:` field's host part. A repository has no way to know its own canonical
	// name — a remote URL is a checkout detail, and a fork's remote names the upstream — so
	// it is asked for rather than guessed. Absent, pages carry a commit-only resource, which
	// is enough for the staleness check verify performs and honest about what it does not
	// know.
	repo := fs.String("repo", "",
		"repository name for the resource URI, e.g. example.com/org/repo")
	// The semantic pass is opt-in twice over: a backend has to be configured (§5 makes
	// none the default) *and* this flag has to be set. Two gates rather than one because
	// they answer different questions — the environment says a model is available, and the
	// flag says this run should spend it. Without the flag, a developer who configured a
	// backend for `signpost model check` would find every subsequent build calling it.
	//
	// It is also what keeps §8's split honest. signpost.yml runs on every push and must
	// stay deterministic and byte-stable; signpost-semantic.yml runs on a schedule and
	// passes this. Relying on the environment alone would put the difference in a variable
	// somebody can set globally, where this puts it in the workflow file.
	sem := fs.Bool("semantic", false,
		"summarise modules with the configured model backend (§4.5); off by default")
	semTimeout := fs.Duration("semantic-timeout", 10*time.Minute,
		"how long the whole semantic pass may take before it stops and reports what it has")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}

	a, err := analyse(context.Background(), path, pf)
	if err != nil {
		return err
	}
	if !*quiet {
		reportCoverage(errOut, a)
	}
	// Clusters before writing: the manifest records how many the graph fell into, and the
	// assignment is computed lazily.
	a.Graph().Clusters()

	opts := buildOptions(a, *repo)
	if *sem {
		sr, err := runSemantic(a, *semTimeout)
		if err != nil {
			return err
		}
		opts.Roles = sr.Regions()
		// On stderr and not suppressed by -quiet. -quiet silences the coverage report,
		// which is a routine summary; this is the §4.2 report of what a pass could not
		// account for, and a fail-open pass whose failures are quiet is the one failure
		// mode that looks like success.
		reportSemantic(newPrinter(errOut), sr)
	}

	res, err := okf.Write(a.Discovered.Root, a.Graph(), opts)
	if err != nil {
		return err
	}
	reportBuild(newPrinter(out), a.Discovered.Root, res)
	return nil
}

// runSemantic builds the backend and runs the pass.
//
// The error return covers configuration only — an unknown backend name, a missing base
// URL — because that is a mistake in the invocation and failing open on it would mean a
// typo silently producing a deterministic bundle. Everything that happens *after* the
// backend is built fails open inside semantic.Run, per §5.
func runSemantic(a *analysis, timeout time.Duration) (*semantic.Result, error) {
	b, err := model.New(model.Config{Version: version})
	if err != nil {
		return nil, err
	}
	if b == nil {
		// -semantic with no backend configured. An error, not a skip: the flag is a request
		// to spend a model, and answering it with a silent deterministic build is how a
		// scheduled workflow runs for a month producing nothing while reporting success.
		return nil, fmt.Errorf("%w: -semantic needs a backend: set %s=inferd or %s=openai "+
			"(run `signpost model check` to test one)",
			errUsage, model.EnvBackend, model.EnvBackend)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return semantic.Run(ctx, semantic.Input{
		Graph:      a.Graph(),
		Discovered: a.Discovered,
		Backend:    b,
		CacheDir:   filepath.Join(a.Discovered.Root, okf.BundleDir, "cache", "summary"),
	}), nil
}

// reportSemantic says what the pass did and what it could not do.
func reportSemantic(p *printer, r *semantic.Result) {
	p.printf("semantic pass: %d module(s) summarised (%d from cache, %d call(s), "+
		"%d tokens in, %d out)\n",
		len(r.Summaries), r.Cached, r.Calls, r.InputTokens, r.OutputTokens)
	if r.Truncated > 0 {
		p.printf("  %d summarised from part of the module: the input caps applied\n", r.Truncated)
	}
	for _, s := range r.Skipped {
		p.printf("  not summarised: %s\n", s)
	}
}

// buildOptions maps the analysis's provenance onto the emitter's.
//
// Both fields come from the commit rather than the clock or the flags, which is what makes a
// re-run at the same commit produce the same bytes. A repository with no readable history
// gets neither, and the emitter omits the frontmatter keys entirely — an absent provenance
// stamp being better than one naming a commit nobody can check.
func buildOptions(a *analysis, repo string) okf.Options {
	var head vcs.Commit
	if a.History != nil && a.History.Available {
		head = a.History.Head
	}
	return okf.Options{
		Actor:    okf.Actor("signpost/" + version),
		Resource: resourceBase(repo, head.SHA),
		Date:     head.Date,
	}
}

// resourceBase builds the `git://` base URI every page's resource extends.
//
// No commit means no resource at all. A URI naming a repository but not a commit would look
// like provenance while carrying none of its value: verify's staleness check is a comparison
// against a sha, and there would be nothing to compare.
func resourceBase(repo, sha string) string {
	if sha == "" {
		return ""
	}
	if repo == "" {
		return "git://" + sha
	}
	return "git://" + repo + "@" + sha
}

// reportBuild says what the run did.
//
// The counts are ordered by what a reader needs to know, not by size. `unchanged` leading on
// a re-run is the signal that byte-stability holds; `preserved` is the number that says the
// compounding mechanism is doing something; and the two lists below are the only things in a
// build that ask for a human's attention.
func reportBuild(p *printer, root string, res *okf.Result) {
	p.printf("%s/%s\n", root, okf.BundleDir)
	p.printf("  %d page(s): %d created, %d updated, %d unchanged\n",
		len(res.Written), res.Created, res.Updated, res.Unchanged)
	if res.Preserved > 0 {
		p.printf("  %d page(s) had human notes, carried across\n", res.Preserved)
	}
	if n := len(res.Downgraded); n > 0 {
		// Named individually rather than counted: each one is a page a person reviewed at a
		// commit that is no longer the one being described, so each one is a page for that
		// person to look at again. A count would tell them there is work without saying where.
		p.printf("  %d page(s) verified against an older commit, now marked stale:\n    %s\n",
			n, joinTop(n, 10, func(i int) string { return res.Downgraded[i] }))
	}
	if n := len(res.Stale); n > 0 {
		// Reported, not deleted. See internal/okf's package comment: a renamed directory
		// would otherwise silently remove a page someone had written notes on.
		p.printf("  %d page(s) describe concepts that no longer exist (not deleted):\n    %s\n",
			n, joinTop(n, 10, func(i int) string { return res.Stale[i] }))
	}
}
