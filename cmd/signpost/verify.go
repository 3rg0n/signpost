package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/3rg0n/signpost/internal/okf"
)

// `signpost verify` checks the committed bundle against the tree it describes.
//
// This is the command that makes the rest of the design safe to rely on. A bundle is
// committed (ADR 0005), so it is read by people and agents who did not build it and cannot
// tell by looking whether it still matches the code. verify is the thing that can tell
// them, and it earns that only by failing: design §4.6 is explicit that a staleness check
// which exits zero is worse than no check, because a bundle everyone trusts and nobody
// validates is confidently wrong.
//
// So the exit code is the interface. `1` means the bundle does not match the tree; `0`
// means every check ran and passed, or ran and found only litter. Which checks ran is
// printed either way, because "ok" from a verify that opened nothing looks exactly like
// "ok" from one that opened everything.
func runVerify(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		u := newPrinter(errOut)
		u.printf("usage: signpost verify [flags] [path]\n")
		u.printf("\nCheck %s/ against the repository. Non-zero if it is stale.\n\nFlags:\n",
			okf.BundleDir)
		fs.PrintDefaults()
	}
	var pf pipelineFlags
	pf.register(fs)
	quiet := fs.Bool("quiet", false, "suppress the coverage report on stderr")
	// The same flag as `build`, and it has to be passed the same way. It feeds every page's
	// `resource:`, so verifying with a different -repo than the bundle was built with
	// reports a difference that is real and describes the invocation rather than the bundle.
	repo := fs.String("repo", "",
		"repository name for the resource URI; must match the build's")
	// What a pull-request check needs, and why it is a flag rather than the default. The
	// bundle is built on the default branch only (§8.0), so anywhere else its commit stamp is
	// behind by construction and a strict verify calls every page stale on a pull request that
	// changed no code. It is also the only way a single developer can build locally and commit
	// the bundle with the code: that commit's own sha does not exist when the stamp is written.
	//
	// The default stays strict because on the default branch signpost *writes* the stamp, so
	// something has to check that what it wrote is true. See okf.Options.AsOfBundle.
	asOf := fs.Bool("as-of-bundle", false,
		"compare content as of the commit the bundle records, rather than this tree's "+
			"(for branches and pull requests, where the bundle is older by design)")
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
	a.Graph().Clusters()

	opts := buildOptions(a, *repo)
	opts.AsOfBundle = *asOf
	// The same pass build runs, and it has to be the same one. verify renders the bundle this
	// tree would produce and compares; a page build emits and verify does not is reported as
	// an orphan plus a changed index, neither of which names the real cause. See addPractices.
	//
	// Counts are not printed here. verify's stdout is a list of what was checked and what
	// failed, and a line saying the repository declares twelve things is neither.
	addPractices(&opts, a)
	res, err := okf.Verify(a.Discovered.Root, a.Graph(), opts)
	if err != nil {
		return err
	}
	reportVerify(newPrinter(out), a.Discovered.Root, res)
	if !res.OK() {
		return errStale
	}
	return nil
}

// errStale is the failure. A distinct error rather than a bare exit code so that runOr maps
// it the same way it maps every other "signpost ran and what it found was the problem"
// case: exit 1, not 2. A CI job needs to tell a stale bundle from a mistyped flag.
var errStale = fmt.Errorf("the bundle does not match this tree")

// reportVerify prints what was checked, then what was wrong.
//
// Checked first, and not behind -quiet. The counts are the evidence that the pass means
// something: a reader who sees "0 pages" above a clean result knows the answer is "there
// was nothing here", which is a different fact from "everything resolved".
func reportVerify(p *printer, root string, res *okf.VerifyResult) {
	c := res.Checked
	p.printf("%s/%s\n", root, okf.BundleDir)
	p.printf("  checked %d page(s): %d edge(s), %d source(s), %d prose link(s)\n",
		c.Pages, c.Edges, c.Sources, c.Links)
	for _, s := range res.Skipped {
		// Every check that did not run is named. An unreported skip is the false pass the
		// whole command exists to prevent, arriving through its own output.
		p.printf("  skipped: %s\n", s)
	}
	for _, w := range res.Warnings {
		p.printf("  warning: %s\n", w)
	}
	if res.OK() {
		p.printf("  ok: the bundle matches this tree\n")
		return
	}
	p.printf("  %d problem(s):\n", len(res.Findings))
	for _, f := range res.Findings {
		p.printf("    %s\n", f)
	}
}
