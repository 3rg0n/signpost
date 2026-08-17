package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path"
	"sort"

	"github.com/3rg0n/signpost/internal/gitdiff"
	"github.com/3rg0n/signpost/internal/graph"
	"github.com/3rg0n/signpost/internal/vcs"
)

// `signpost graph diff` reports what changed structurally between two commits.
//
// Text rather than a picture, and a CLI command rather than a viewer feature, which is
// [ADR 0035]. The short version of the argument: design §7.1 says the structural findings
// are text because that is what an agent consumes, and there is no `-all` for a diagram —
// issue #41 had just finished paying for a bounded finding a reader could not lift.
//
// A graph at a revision that is not checked out comes from a detached `git worktree`
// analysed by the same `analyse` this binary runs everywhere else. Nothing below `analyse`
// knows whether the path it was handed is the user's checkout, which is why teaching
// discovery to read a git object tree — a second `.gitignore` layering, a second byte
// budget, a second census — buys nineteen seconds and costs a permanent second
// implementation.
//
// This is the one command in signpost that requires git rather than degrading without it.
// Everything else treats history as an optional signal and reports its absence as a fact
// (ADR 0020); there is no best-effort answer to what changed between two commits for a
// tree that has none, so this says so by name instead.
//
// [ADR 0035]: ../../docs/adr/0035-a-structural-diff-is-text-and-a-second-commit-is-a-worktree.md
func runGraphDiff(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("graph diff", flag.ContinueOnError)
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost graph diff [flags] <from> <to> [path]\n")
		u.printf("\nCompare the structure of two commits. Each revision is checked out into a\n" +
			"temporary worktree and analysed, so neither the working tree nor the index is\n" +
			"touched. Reports concepts added, removed, and renamed, and edges gained and lost;\n" +
			"a concept present at both revisions is not otherwise compared.\n\n" +
			"Co-change edges are excluded. They are drawn from the commits each revision's log\n" +
			"holds, and the newer revision's log is a superset of the older one's, so every\n" +
			"comparison would report coupling that crossed the threshold in between as a\n" +
			"structural change. Use `graph show` at each revision for those.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	var pf pipelineFlags
	pf.register(fs)
	// The same flag name `graph show` uses, for the same reason and deliberately not a
	// `-limit N` — which would fit this command better, since every finding here is one line
	// and one number really would bound them all. A reader who learned `-all` on `show` must
	// not have to learn a second spelling on `diff`; the flag surface across two sibling
	// subcommands is worth more than the extra precision.
	all := fs.Bool("all", false,
		"list every finding in full, instead of the first few of each; for a reader that greps")
	quiet := fs.Bool("quiet", false, "suppress the coverage report on stderr")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	fromRev, toRev, repo, err := diffArgs(fs)
	if err != nil {
		return err
	}
	// -all and -quiet are properties of this invocation, so neither is configurable
	// (ADR 0011). The revisions are arguments rather than keys for the same reason: a
	// repository does not have a pair of commits it is always compared at.
	cfg, err := loadConfig(repo)
	if err != nil {
		return err
	}
	applyConfig(fs, cfg, &pf)

	ctx := context.Background()
	// Checked before anything is created, so the three ways this cannot work — no git, no
	// repository, no commits — are reported against the path the user typed rather than
	// against a temp directory they never asked for.
	if err := gitdiff.Available(ctx, repo); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	// The worktrees and the renames are all expressed relative to the top of the work tree,
	// so a path pointing at a subdirectory has to resolve to the same comparison.
	root, err := gitdiff.Root(ctx, repo)
	if err != nil {
		return err
	}

	// Both revisions resolved before either is checked out, and a failure here is exit 2.
	// Two reasons, and the exit code is the smaller one: a typo in the *second* revision
	// would otherwise be reported after the first one had been checked out and analysed,
	// which is twenty seconds spent to reject a command line that was wrong before it
	// started.
	fromSHA, err := resolveRev(ctx, root, fromRev)
	if err != nil {
		return err
	}
	toSHA, err := resolveRev(ctx, root, toRev)
	if err != nil {
		return err
	}

	fromA, err := analyseRevision(ctx, root, fromSHA, pf, errOut, *quiet)
	if err != nil {
		return err
	}
	toA, err := analyseRevision(ctx, root, toSHA, pf, errOut, *quiet)
	if err != nil {
		return err
	}

	// Rename detection is asked of git, at file granularity, and aggregated to the
	// directory a module node is (ADR 0003). Not fatal: a diff that reports a moved module
	// as a removal plus an addition is a worse answer than one that names the move, and a
	// worse answer is still an answer where no answer at all is not.
	moved, err := gitdiff.Renames(ctx, root, fromSHA, toSHA)
	if err != nil {
		newPrinter(errOut).printf("warning: git could not report renames, so a moved concept "+
			"will read as one removed and one added: %v\n", err)
		moved = nil
	}

	d := diffGraphs(fromA.Graph(), toA.Graph(), movedPaths(moved))

	lim := defaultDiffLimit
	if *all {
		lim = 0
	}
	p := newPrinter(out)
	writeDiffHeader(p, root, fromSHA, toSHA, fromA, toA)
	writeDiff(p, d, lim)
	return p.Err()
}

// defaultDiffLimit bounds each of this command's five lists.
//
// One number where `graph show` needs five, because every finding here is a single line
// naming one concept or one edge — there is no case like show's, where the bound on the
// modules named *inside* a cycle line has to be far lower than the bound on the number of
// lines. Twenty rather than show's eight or ten: a diff is read for its contents rather
// than skimmed for a shape, and two consecutive commits usually fit under it entirely.
const defaultDiffLimit = 20

// diffArgs reads the two revisions and the optional path.
//
// Exactly two revisions, with no `graph diff <ref>` shorthand meaning "against HEAD". That
// shorthand is what git does and it is unavailable here for a structural reason rather than
// a stylistic one: the third positional argument is a path, so a one-revision form would
// make `graph diff HEAD~5 .` ambiguous between a path and a revision, and resolving it by
// asking git whether `.` is a commit decides an invocation's meaning from the contents of
// the repository.
func diffArgs(fs *flag.FlagSet) (from, to, repo string, err error) {
	switch fs.NArg() {
	case 2:
		return fs.Arg(0), fs.Arg(1), ".", nil
	case 3:
		return fs.Arg(0), fs.Arg(1), fs.Arg(2), nil
	}
	return "", "", "", fmt.Errorf("%w: expected two revisions and an optional path, got %d "+
		"argument(s); try `signpost graph diff HEAD~5 HEAD`", errUsage, fs.NArg())
}

// analyseRevision checks rev out into a temporary worktree and runs the pipeline against it.
//
// The worktree is removed before this returns, so the caller holds an analysis and no
// external state. That costs a full copy of the tree on disk for the duration — about
// eleven seconds and one tree per revision here — and it is why ADR 0035 rules this command
// out of anything on a hot path: `build` and `verify` read the working tree they are
// already standing in.
func analyseRevision(ctx context.Context, root, rev string, pf pipelineFlags,
	errOut io.Writer, quiet bool) (*analysis, error) {
	wt, err := gitdiff.Add(ctx, root, rev)
	if err != nil {
		return nil, err
	}
	defer func() {
		// A cleanup failure is reported and not returned. The analysis it accompanies is
		// complete and correct, and failing a diff that was computed successfully because a
		// temp directory outlived it would be the wrong trade — but a silent leftover
		// worktree is a full copy of a repository nobody knows to delete.
		if err := wt.Close(); err != nil {
			newPrinter(errOut).printf("warning: %v\n", err)
		}
	}()
	a, err := analyse(ctx, wt.Dir, pf)
	if err != nil {
		return nil, fmt.Errorf("analyse %s: %w", short(wt.Commit), err)
	}
	if !quiet {
		// Labelled, because two coverage reports arrive on the same stream and the reader has
		// to be able to tell which revision could not be read. The §4.2 rule applies per
		// revision: a diff whose older side had no extractor for half the tree reports
		// structural change that is a gap in coverage rather than a change in the code.
		newPrinter(errOut).printf("%s:\n", short(wt.Commit))
		reportCoverage(errOut, a)
	}
	return a, nil
}

// resolveRev turns a revision into a sha, reporting a bad one as a usage error.
//
// Exit 2 rather than 1, on the distinction the rest of this binary keeps: 1 means signpost
// looked and reports something about the repository, and 2 means the command line was wrong
// and re-running it unchanged fails the same way. A misspelled ref is the second, and calling
// it the first would have a CI step report a broken repository.
func resolveRev(ctx context.Context, root, rev string) (string, error) {
	sha, err := gitdiff.Resolve(ctx, root, rev)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errUsage, err)
	}
	return sha, nil
}

// short renders a sha the way every other command in this binary does.
func short(sha string) string { return vcs.Commit{SHA: sha}.Short() }

// structuralDiff is what changed between two revisions.
//
// Concepts and edges, and nothing about a concept present at both. A module whose exports
// or churn changed is not reported: churn differs at every commit by definition, so
// including it would make every node in the repository a finding and bury the five that are
// real ones.
type structuralDiff struct {
	added     []*graph.Node
	removed   []*graph.Node
	renamed   []renamedNode
	gained    []graph.Edge
	lost      []graph.Edge
	identical bool
}

// renamedNode is one concept that moved. Both IDs and both paths, because they answer
// different questions: the IDs are what a reader greps the two bundles for, and the paths
// are what git was asked about and what somebody types to go look.
type renamedNode struct {
	fromID, toID     string
	fromPath, toPath string
}

// movedPaths turns git's file renames into every path move a node could be matched on.
//
// Two kinds of entry, because two kinds of node carry a path. A document or a contract node
// is one file, so a file rename is directly a node rename. A module node is a directory
// (ADR 0003), so its move has to be aggregated from the renames of the files inside it.
//
// The aggregation refuses to guess. A directory whose files went to two different places is
// a split, not a move, and reporting one of the two as "the" destination would assert a
// relationship the repository does not state — the same refusal ADR 0034 makes for an
// interpolated table name and ADR 0032 for two CI jobs with no declared order. A split is
// reported as a removal plus additions, which is what it is.
func movedPaths(fileRenames map[string]string) map[string]string {
	moved := make(map[string]string, len(fileRenames)*2)
	// Destinations per source directory. A set rather than a count: what disqualifies a
	// directory is having more than one destination, however many files went to each.
	dests := map[string]map[string]bool{}
	for from, to := range fileRenames {
		moved[from] = to
		fromDir, toDir := dirOf(from), dirOf(to)
		// The repository root is not a directory that can move. Its module node carries an
		// empty Path, so an entry for it would match every node with no path at all.
		if fromDir == toDir || fromDir == "" || toDir == "" {
			continue
		}
		if dests[fromDir] == nil {
			dests[fromDir] = map[string]bool{}
		}
		dests[fromDir][toDir] = true
	}
	for dir, to := range dests {
		if len(to) != 1 {
			continue
		}
		for d := range to {
			// A file rename must not be overwritten by a directory aggregate. The file entry
			// is what git actually said; this one is derived from it.
			if _, ok := moved[dir]; !ok {
				moved[dir] = d
			}
		}
	}
	return moved
}

// diffGraphs compares two graphs, treating a moved path as the same concept.
//
// Renames are resolved before edges are compared, and that ordering is the reason the
// rename detection is worth having at all. A renamed module with fifteen imports otherwise
// reports as one removal, one addition, and thirty edge changes — a diff in which a `git mv`
// and a rewrite are indistinguishable.
func diffGraphs(from, to *graph.Graph, moved map[string]string) structuralDiff {
	var d structuralDiff

	// Candidates on the new side, keyed by path and kind. Kind is in the key because a path
	// does not identify a node on its own: a directory holding a README produces a module
	// node and a document node, and pairing across kinds would report a renamed directory as
	// a document that became a module.
	type pathKind struct {
		path string
		kind graph.Kind
	}
	newByPath := map[pathKind]*graph.Node{}
	for _, n := range to.Nodes() {
		if n.Path == "" || from.Has(n.ID) {
			continue
		}
		newByPath[pathKind{n.Path, n.Kind}] = n
	}

	renamedTo := map[string]string{}
	for _, n := range from.Nodes() {
		if to.Has(n.ID) {
			continue
		}
		dest, ok := moved[n.Path]
		if !ok || n.Path == "" {
			d.removed = append(d.removed, n)
			continue
		}
		match, ok := newByPath[pathKind{dest, n.Kind}]
		if !ok {
			// git says the path moved and the new graph has no node there. Ordinary rather
			// than surprising: a directory that moved into a vendored or ignored location is
			// not analysed at the new revision, and it is a removal from the map even though
			// the files still exist.
			d.removed = append(d.removed, n)
			continue
		}
		if match.ID == n.ID {
			// The path moved and the ID did not, which means two different directories slug
			// to the same page name. Not a rename of anything a reader can see.
			continue
		}
		d.renamed = append(d.renamed, renamedNode{
			fromID: n.ID, toID: match.ID, fromPath: n.Path, toPath: match.Path,
		})
		renamedTo[n.ID] = match.ID
		// Claimed, so a second removed node with the same destination cannot also pair with
		// it. Without this a directory that absorbed two others would be reported as two
		// renames into one node.
		delete(newByPath, pathKind{match.Path, match.Kind})
	}

	renamedFrom := map[string]bool{}
	for _, r := range d.renamed {
		renamedFrom[r.toID] = true
	}
	for _, n := range to.Nodes() {
		if !from.Has(n.ID) && !renamedFrom[n.ID] {
			d.added = append(d.added, n)
		}
	}

	// Edge sets keyed on the post-rename identity of both endpoints, so an edge that only
	// changed because its module moved is neither gained nor lost.
	type key struct {
		from, to string
		kind     graph.EdgeKind
	}
	at := func(id string) string {
		if r, ok := renamedTo[id]; ok {
			return r
		}
		return id
	}
	was := map[key]graph.Edge{}
	for _, e := range from.Edges() {
		if !structural(e) {
			continue
		}
		was[key{at(e.From), at(e.To), e.Kind}] = e
	}
	now := map[key]bool{}
	for _, e := range to.Edges() {
		if !structural(e) {
			continue
		}
		now[key{e.From, e.To, e.Kind}] = true
		if _, ok := was[key{e.From, e.To, e.Kind}]; !ok {
			d.gained = append(d.gained, e)
		}
	}
	// Reported with the identifiers the old revision used, which is where a reader who wants
	// to see the edge has to look. Sorted, because map iteration is randomised and this
	// output is compared between runs.
	for k, e := range was {
		if !now[k] {
			d.lost = append(d.lost, e)
		}
	}
	sortEdgeSlice(d.lost)

	d.identical = len(d.added) == 0 && len(d.removed) == 0 && len(d.renamed) == 0 &&
		len(d.gained) == 0 && len(d.lost) == 0
	return d
}

// structural reports whether an edge is one this command may compare.
//
// It excludes co-change, and that exclusion is not a filter for tidiness — it is what makes
// the output mean what the command's name says. A co-change edge is drawn from the commits
// each revision's log contains (ADR 0020), and the two revisions being compared have
// different logs *by construction*: the newer one has every commit the older one had plus
// the ones between them. So the pair that crossed the co-change threshold on the third of
// those commits appears as an edge gained, indistinguishable from an import somebody added.
//
// Measured on this repository rather than reasoned about. `graph diff HEAD~3 HEAD` reported
// exactly four findings and all four were co-change edges between /modules/config and three
// others — a diff across three commits that touched no import at all, reporting four
// structural changes. Noise at that ratio is worse than a missing finding: a reader who sees
// it once stops reading the output.
//
// The loss is real and is stated in the command's help. Co-change is the only coupling signal
// no static read produces, so "these two modules started changing together" is a finding
// somebody would want. It is not available *as a diff* because a co-change edge is a claim
// about a window of history and not about a tree, and the two revisions do not share a
// window. `graph show` at each revision reports them, which is where that question goes.
func structural(e graph.Edge) bool { return e.Kind != graph.EdgeCoChanges }

func sortEdgeSlice(es []graph.Edge) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].From != es[j].From {
			return es[i].From < es[j].From
		}
		if es[i].To != es[j].To {
			return es[i].To < es[j].To
		}
		return es[i].Kind < es[j].Kind
	})
}

// writeDiffHeader states what was compared and how big each side was.
//
// The two shas rather than the two revisions the caller typed: `HEAD~5` names a different
// commit tomorrow, and a diff whose output cannot be tied to two commits is not something
// anybody can reproduce. The counts are here because they are the cheapest check on a
// finding below — a hundred concepts removed reads very differently under `412 -> 415
// nodes` than under `412 -> 312`.
func writeDiffHeader(p *printer, root, fromSHA, toSHA string, from, to *analysis) {
	fn, _ := from.Graph().Counts()
	tn, _ := to.Graph().Counts()
	p.printf("%s\n", root)
	p.printf("  %s -> %s\n", short(fromSHA), short(toSHA))
	// The comparable edges rather than Counts()' total, which would include the co-change
	// edges structural() excludes. Two numbers describing different sets under one heading is
	// how a reader concludes the tool lost four edges: `218 -> 222 edges` above an empty
	// findings list is a contradiction, and the contradiction would be in the header.
	p.printf("  %d nodes, %d edges -> %d nodes, %d edges\n",
		fn, countStructural(from.Graph()), tn, countStructural(to.Graph()))
}

// countStructural counts the edges this command compares.
func countStructural(g *graph.Graph) int {
	n := 0
	for _, e := range g.Edges() {
		if structural(e) {
			n++
		}
	}
	return n
}

// writeDiff prints the findings, or says there are none.
//
// The absence is a line rather than an empty report, per
// [ADR 0030](../../docs/adr/0030-a-finding-states-its-own-absence.md): two revisions with
// the same structure and a run that failed halfway both print nothing otherwise, and a
// reader cannot tell them apart.
func writeDiff(p *printer, d structuralDiff, lim int) {
	if d.identical {
		p.printf("\nno structural difference: the same concepts and the same edges at both " +
			"revisions\n")
		return
	}
	writeNodeList(p, "concepts added", d.added, lim)
	writeNodeList(p, "concepts removed", d.removed, lim)
	if len(d.renamed) > 0 {
		p.printf("\nconcepts renamed (%d)\n", len(d.renamed))
		for i, r := range d.renamed {
			if truncated(p, lim, i, len(d.renamed)) {
				break
			}
			p.printf("  %s -> %s  (%s -> %s)\n", r.fromID, r.toID, r.fromPath, r.toPath)
		}
	}
	writeEdgeList(p, "edges gained", d.gained, lim)
	writeEdgeList(p, "edges lost", d.lost, lim)
}

func writeNodeList(p *printer, heading string, nodes []*graph.Node, lim int) {
	if len(nodes) == 0 {
		return
	}
	p.printf("\n%s (%d)\n", heading, len(nodes))
	for i, n := range nodes {
		if truncated(p, lim, i, len(nodes)) {
			return
		}
		// The kind, because the list mixes them and they are not equally interesting: a
		// Document appearing is a file somebody wrote, and a Module appearing is a package
		// that did not exist.
		p.printf("  %-42s %s\n", n.ID, n.Kind)
	}
}

func writeEdgeList(p *printer, heading string, edges []graph.Edge, lim int) {
	if len(edges) == 0 {
		return
	}
	p.printf("\n%s (%d)\n", heading, len(edges))
	for i, e := range edges {
		if truncated(p, lim, i, len(edges)) {
			return
		}
		// The same spelling `graph show` uses for a bridge, so the two commands' output can
		// be read by one reader without a second format to learn.
		p.printf("  %s -%s-> %s\n", e.From, e.Kind, e.To)
	}
}

// dirOf is the directory a node path belongs to, under the convention assemble uses: a file
// at the root belongs to "", which is the module node for the repository itself.
//
// path rather than path/filepath, because every path here is slash-separated on every
// platform — discover.File.Path normalises Windows backslashes so the bundle is
// byte-identical across platforms, and git reports slashes too.
func dirOf(p string) string {
	d := path.Dir(p)
	if d == "." || d == "/" {
		return ""
	}
	return d
}

// truncated prints the elision marker and reports that the caller should stop.
//
// The marker names the flag that lifts it, which is issue #41's finding rather than a
// convention: `and 66 more` is not a prompt to go anywhere unless there is somewhere to go.
func truncated(p *printer, lim, i, total int) bool {
	if lim <= 0 || i != lim {
		return false
	}
	p.printf("  and %d more (-all lists them)\n", total-i)
	return true
}
