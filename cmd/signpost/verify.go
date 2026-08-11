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
// So the exit code is the interface, and what it has to mean is *whether the reader must
// act*. `1` means something is wrong that they can fix; `0` means carry on. Which checks ran
// is printed either way, because "ok" from a verify that opened nothing looks exactly like
// "ok" from one that opened everything.
//
// Under -as-of-bundle that puts one class of difference outside the exit code: the pages a
// rebuild would change because this branch added or moved structure. §8.0 forbids rebuilding
// the bundle on a branch, so the remedy is the merge, and a red gate whose instructions the
// author is not allowed to follow is a gate everybody learns to merge past — including on the
// run where the bundle is really broken. Those differences are printed in full and counted as
// pending. See okf.classifyPending; the strict verify has no merge to defer to and keeps
// failing on all of them.
func runVerify(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	// One writer for the whole of this command's usage, so the prose above and
	// PrintDefaults' flag list cannot land on different streams.
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
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
	// The same load `build` does, in the same place, because verify's answer is a comparison
	// against what build produces: a verify that walked the tree differently would report a
	// difference describing the two invocations rather than the bundle. -as-of-bundle above
	// stays out of it — ADR 0011's second class, and there is no key for it.
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	applyConfig(fs, cfg, &pf)
	applyRepo(fs, cfg, repo)

	// Before the analysis, because the commit the bundle records is an *input* to it rather
	// than something to compare afterwards. Seven churn attributes and the co-change edges are
	// derived from history and land in page content, so on a branch every commit moves them:
	// one commit adding a comment changes `commits` and `lines_added` on that directory's page,
	// and a commit touching two directories can add a co_changes edge, which moves the edge
	// totals on index.md, log.md, and manifest.json. Reading the history the bundle read makes
	// all of them identical by construction. See vcs.Options.AsOf.
	//
	// Empty is the ordinary answer for a repository with no bundle yet, and it needs no branch
	// here: verify reports the missing bundle, and this analysis was going to read from HEAD
	// anyway.
	if *asOf {
		pf.asOfCommit = okf.RecordedCommit(path)
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
	if len(res.Pending) > 0 {
		return errPending
	}
	return nil
}

// errStale is the failure. A distinct error rather than a bare exit code so that runOr maps
// it the same way it maps every other "signpost ran and what it found was the problem"
// case: exit 1, not 2. A CI job needs to tell a stale bundle from a mistyped flag.
var errStale = fmt.Errorf("the bundle does not match this tree")

// errPending is not a failure and runOr never sees it: the pending case exits 0, which is the
// whole point of the severity. It exists for the one caller that is neither CI nor a person
// reading a terminal — the post-commit hook, which shares this function precisely so a second
// implementation of "is the bundle current" cannot drift from the one CI gates on.
//
// The hook needs the opposite answer from the same run. Pending means "a rebuild after the merge
// resolves this", and on a developer's machine after a commit there is no merge and no push job:
// the remedy is `signpost build`, right now, by them. So the hook has to print its reminder for
// exactly the differences CI is right to stay quiet about. Distinguishing them by re-deriving
// anything would reintroduce the second implementation; this returns the fact.
var errPending = fmt.Errorf("the bundle will be rebuilt after this merges")

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
	// Printed in full, above the verdict, and never folded into a count. These are the
	// differences the merge resolves, so the reader's action is nothing — but "nothing to do"
	// is only trustworthy if they can see what was set aside and disagree with it. A gate that
	// silently swallowed a page it decided was somebody else's problem would be the false pass
	// this command exists to prevent, arriving through the exit code instead of the output.
	if len(res.Pending) > 0 {
		p.printf("  %d difference(s) the merge will rebuild, not this branch:\n",
			len(res.Pending))
		for _, f := range res.Pending {
			p.printf("    %s\n", f)
		}
	}
	if res.OK() {
		if len(res.Pending) > 0 {
			// Deliberately not "ok: the bundle matches this tree", which would be false.
			// It does not match; nothing on this branch is supposed to make it match.
			p.printf("  ok: nothing to do here — the bundle is rebuilt after this merges\n")
			return
		}
		p.printf("  ok: the bundle matches this tree\n")
		return
	}
	p.printf("  %d problem(s):\n", len(res.Findings))
	for _, f := range res.Findings {
		p.printf("    %s\n", f)
	}
}
