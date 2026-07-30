package main

import (
	"context"
	"flag"
	"io"

	"github.com/3rg0n/signpost/internal/okf"
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

	res, err := okf.Write(a.Discovered.Root, a.Graph(), buildOptions(a, *repo))
	if err != nil {
		return err
	}
	reportBuild(newPrinter(out), a.Discovered.Root, res)
	return nil
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
