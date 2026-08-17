package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/graph"
)

// Issue #39: a structural diff between two commits. ADR 0035 decided it is text from a CLI
// command rather than a viewer feature, and that a revision which is not checked out comes
// from a detached `git worktree` analysed by the existing pipeline.
//
// Two layers of test, and the split is deliberate. The comparison itself — which node paired
// with which, which edge survived a rename — is tested against hand-built graphs, because
// every case needs a shape (a split directory, a collision, a rename into an ignored path)
// that would take a purpose-built git history apiece to produce. The parts only git can
// answer — that a worktree gets checked out, that `-M` finds the rename, that the temp
// directory is gone afterwards — are tested through the binary against a real repository,
// because there is nothing to assert about them that a fake would not also satisfy.

// diffRepo builds a git repository with two commits and returns its path.
//
// One commit per state rather than a fixture per state: the command takes two revisions, so
// the second commit is the only way to express "and then this changed" to it.
func diffRepo(t *testing.T, first, second map[string]string) string {
	t.Helper()
	root := t.TempDir()
	gitRun(t, root, "init", "--quiet", "--initial-branch=main")
	gitRun(t, root, "config", "user.name", "Diff Author")
	gitRun(t, root, "config", "user.email", "diff@example.invalid")
	gitRun(t, root, "config", "commit.gpgsign", "false")
	gitRun(t, root, "config", "core.autocrlf", "false")
	writeTree(t, root, first)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "--quiet", "-m", "first")
	if second != nil {
		writeTree(t, root, second)
		gitRun(t, root, "add", "-A")
		gitRun(t, root, "commit", "--quiet", "-m", "second")
	}
	return root
}

// writeTree writes files, and deletes the ones whose content is the empty string.
//
// The deletion spelling is what makes a rename expressible: git detects one by comparing the
// blobs a commit removed against the ones it added, so a test that only ever wrote files
// could not produce an `R` record at all.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if content == "" {
			if err := os.Remove(full); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// goModule is a package big enough for git to score a rename against. Similarity is a
// content comparison, so a one-line file moved and edited scores below the default 50%
// threshold and comes back as a delete plus an add — which is a property of the fixture and
// not a finding about the tool.
func goModule(pkg string, imports ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	for _, im := range imports {
		fmt.Fprintf(&b, "import %q\n", im)
	}
	fmt.Fprintf(&b, "\n// %s does the work of this package, at enough length that git scores a\n"+
		"// rename of this file above its similarity threshold rather than reporting the move\n"+
		"// as one deletion and one addition.\nfunc Work() string { return %q }\n", pkg, pkg)
	return b.String()
}

// A concept added and an edge gained, which is the ordinary case and the one every other
// test here is a boundary of.
func TestDiffReportsAddedConceptsAndEdges(t *testing.T) {
	root := diffRepo(t,
		map[string]string{
			"go.mod":                "module example.com/app\n\ngo 1.26\n",
			"internal/auth/auth.go": goModule("auth"),
		},
		map[string]string{
			"internal/store/store.go": goModule("store"),
			"internal/auth/auth.go":   goModule("auth", "example.com/app/internal/store"),
		})
	stdout, stderr, code := invoke(t, "graph", "diff", "-quiet", "HEAD~1", "HEAD", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	for _, want := range []string{
		"concepts added (1)",
		"/modules/store",
		"edges gained (1)",
		"/modules/auth -imports-> /modules/store",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	// The two shas, not the two revisions typed. `HEAD~1` names a different commit tomorrow,
	// so a diff whose output cannot be tied to two commits is not reproducible.
	sha := strings.TrimSpace(gitRun(t, root, "rev-parse", "--short=7", "HEAD"))
	if !strings.Contains(stdout, sha) {
		t.Errorf("the output does not name the commit it compared (%s):\n%s", sha, stdout)
	}
}

// The negative boundary for the above, and ADR 0030's rule: two revisions with the same
// structure say so. Without this line an empty report and a run that died halfway are the
// same output.
func TestDiffStatesItsOwnAbsence(t *testing.T) {
	root := diffRepo(t,
		map[string]string{
			"go.mod":                "module example.com/app\n\ngo 1.26\n",
			"internal/auth/auth.go": goModule("auth"),
		},
		// A comment added to a file. The tree changed, the structure did not.
		map[string]string{"internal/auth/auth.go": goModule("auth") + "\n// a later thought\n"})
	stdout, stderr, code := invoke(t, "graph", "diff", "-quiet", "HEAD~1", "HEAD", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "no structural difference") {
		t.Errorf("a diff with no findings does not say so:\n%s", stdout)
	}
	// And it must not also print an empty findings heading, which would be the same
	// ambiguity one line further down.
	for _, absent := range []string{"concepts added", "concepts removed", "edges gained", "edges lost"} {
		if strings.Contains(stdout, absent) {
			t.Errorf("an empty %q heading was printed beside the absence line:\n%s", absent, stdout)
		}
	}
}

// A renamed directory is one rename, not a removal plus an addition, and its edges are
// neither gained nor lost. This is the finding item 2 of the issue is about: without it a
// `git mv` and a rewrite of the same module are indistinguishable in the output.
func TestDiffReportsARenamedModuleAsARename(t *testing.T) {
	root := diffRepo(t,
		map[string]string{
			"go.mod":                  "module example.com/app\n\ngo 1.26\n",
			"internal/auth/auth.go":   goModule("auth", "example.com/app/internal/store"),
			"internal/store/store.go": goModule("store"),
		},
		map[string]string{
			"internal/auth/auth.go":     "",
			"internal/identity/auth.go": goModule("auth", "example.com/app/internal/store"),
		})
	stdout, stderr, code := invoke(t, "graph", "diff", "-quiet", "HEAD~1", "HEAD", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "concepts renamed (1)") {
		t.Fatalf("the moved module was not reported as a rename:\n%s", stdout)
	}
	// Both IDs and both paths. The IDs are what a reader greps the two bundles for; the paths
	// are what git was asked about and what somebody types to go and look.
	for _, want := range []string{
		"/modules/auth", "/modules/identity", "internal/auth", "internal/identity",
	} {
		if !strings.Contains(findSection(stdout, "concepts renamed"), want) {
			t.Errorf("the rename line does not name %q:\n%s", want, stdout)
		}
	}
	// The edges are the point. A rename resolved after the edges were compared would report
	// this as one removal, one addition, and two edge changes.
	for _, absent := range []string{"concepts removed", "edges gained", "edges lost"} {
		if strings.Contains(stdout, absent) {
			t.Errorf("a rename was reported as %s as well:\n%s", absent, stdout)
		}
	}
}

// The negative boundary for the rename: a directory whose files went to two different places
// is a split, and naming one of the two as "the" destination would assert a relationship the
// repository does not state. Reported as a removal plus additions, which is what it is.
func TestDiffDoesNotGuessWhichHalfOfASplitIsTheRename(t *testing.T) {
	root := diffRepo(t,
		map[string]string{
			"go.mod":              "module example.com/app\n\ngo 1.26\n",
			"internal/big/one.go": goModule("big"),
			"internal/big/two.go": goModule("big") + "\nfunc Second() int { return 2 }\n",
		},
		map[string]string{
			"internal/big/one.go":   "",
			"internal/big/two.go":   "",
			"internal/left/one.go":  goModule("big"),
			"internal/right/two.go": goModule("big") + "\nfunc Second() int { return 2 }\n",
		})
	stdout, stderr, code := invoke(t, "graph", "diff", "-quiet", "HEAD~1", "HEAD", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if strings.Contains(stdout, "concepts renamed") {
		t.Errorf("a split directory was reported as a rename, which picks one destination "+
			"arbitrarily:\n%s", stdout)
	}
	for _, want := range []string{"/modules/big", "/modules/left", "/modules/right"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the split does not report %q at all:\n%s", want, stdout)
		}
	}
}

// Co-change edges are excluded, and this is the test that says why rather than only that.
// They are drawn from the commits each revision's log holds, and the newer log is a superset
// of the older one, so a pair crossing the threshold in between reads as a structural change.
// Measured on this repository first: `graph diff HEAD~3 HEAD` reported four findings and all
// four were co-change.
func TestDiffExcludesCoChangeEdges(t *testing.T) {
	g := graph.New()
	for _, id := range []string{"/modules/a", "/modules/b"} {
		if err := g.AddNode(&graph.Node{ID: id, Kind: graph.KindModule, Path: strings.TrimPrefix(id, "/modules/")}); err != nil {
			t.Fatal(err)
		}
	}
	before := graph.New()
	for _, n := range g.Nodes() {
		if err := before.AddNode(&graph.Node{ID: n.ID, Kind: n.Kind, Path: n.Path}); err != nil {
			t.Fatal(err)
		}
	}
	// The only difference between the two graphs is a co-change pair, in both directions the
	// way addCoChangeEdges draws it.
	g.AddEdge(graph.Edge{From: "/modules/a", To: "/modules/b", Kind: graph.EdgeCoChanges, Weight: 4})
	g.AddEdge(graph.Edge{From: "/modules/b", To: "/modules/a", Kind: graph.EdgeCoChanges, Weight: 4})

	d := diffGraphs(before, g, nil)
	if !d.identical {
		t.Errorf("a co-change edge appearing was reported as a structural change: "+
			"%d gained, %d lost", len(d.gained), len(d.lost))
	}
	// The negative boundary: an import between the same two nodes must still be reported, or
	// the exclusion above is indistinguishable from comparing no edges at all.
	g.AddEdge(graph.Edge{From: "/modules/a", To: "/modules/b", Kind: graph.EdgeImports})
	if d := diffGraphs(before, g, nil); len(d.gained) != 1 {
		t.Errorf("an import gained beside the co-change edges was not reported: %d gained", len(d.gained))
	}
}

// A renamed module's edges are neither gained nor lost, which is the reason the rename
// detection is worth having at all. The binary-level test covers this against real git, and it
// is here as well because that one takes forty seconds and this is the assertion a change to
// the comparison breaks: fifteen imports on a renamed module otherwise report as one removal,
// one addition, and thirty edge changes — a diff in which `git mv` and a rewrite look the same.
func TestARenamedModuleKeepsItsEdges(t *testing.T) {
	before, after := graph.New(), graph.New()
	add := func(g *graph.Graph, id, p string) {
		if err := g.AddNode(&graph.Node{ID: id, Kind: graph.KindModule, Path: p}); err != nil {
			t.Fatal(err)
		}
	}
	// One module renamed, and a second that did not move, so there is an edge in each
	// direction across the rename.
	add(before, "/modules/auth", "internal/auth")
	add(before, "/modules/api", "internal/api")
	before.AddEdge(graph.Edge{From: "/modules/api", To: "/modules/auth", Kind: graph.EdgeImports})
	before.AddEdge(graph.Edge{From: "/modules/auth", To: "/modules/api", Kind: graph.EdgeTestedBy})

	add(after, "/modules/identity", "internal/identity")
	add(after, "/modules/api", "internal/api")
	after.AddEdge(graph.Edge{From: "/modules/api", To: "/modules/identity", Kind: graph.EdgeImports})
	after.AddEdge(graph.Edge{From: "/modules/identity", To: "/modules/api", Kind: graph.EdgeTestedBy})

	d := diffGraphs(before, after, map[string]string{"internal/auth": "internal/identity"})
	if len(d.renamed) != 1 {
		t.Fatalf("renamed = %+v, want the one module that moved", d.renamed)
	}
	if len(d.gained) != 0 || len(d.lost) != 0 {
		t.Errorf("%d gained, %d lost across a rename that changed no import. Renames must be "+
			"resolved before the edge sets are compared, or every edge touching a moved module "+
			"is reported twice:\n  gained %+v\n  lost %+v",
			len(d.gained), len(d.lost), d.gained, d.lost)
	}
	if d.identical {
		t.Errorf("identical = true with one rename reported; a rename is a difference")
	}
	// The negative boundary: an edge that really did change must still be reported *through* the
	// rename. Without it, resolving renames could be implemented by comparing no edges at all.
	after.AddEdge(graph.Edge{From: "/modules/identity", To: "/modules/api", Kind: graph.EdgeImports})
	d = diffGraphs(before, after, map[string]string{"internal/auth": "internal/identity"})
	if len(d.gained) != 1 {
		t.Errorf("gained = %+v, want the one import added to the renamed module", d.gained)
	}
}

// An empty diff prints the absence line, per ADR 0030. Asserted on the writer rather than only
// through the binary, because this is the branch that distinguishes two revisions with the same
// structure from a run that died halfway, and both print nothing without it.
func TestAnEmptyDiffPrintsTheAbsenceLine(t *testing.T) {
	out := renderDiff(structuralDiff{identical: true}, defaultDiffLimit)
	if !strings.Contains(out, "no structural difference") {
		t.Errorf("an identical pair of revisions printed no absence line: %q", out)
	}
	// And a diff with findings does not claim there are none, which is the same branch read the
	// other way.
	withFindings := renderDiff(structuralDiff{
		added: []*graph.Node{{ID: "/modules/new", Kind: graph.KindModule, Path: "internal/new"}},
	}, defaultDiffLimit)
	if strings.Contains(withFindings, "no structural difference") {
		t.Errorf("a diff reporting one added concept also said there was no difference: %q",
			withFindings)
	}
	if !strings.Contains(withFindings, "/modules/new") {
		t.Errorf("the added concept was not printed: %q", withFindings)
	}
}

// The header counts the same set the findings do. `218 -> 222 edges` above an empty findings
// list is a contradiction, and it is a contradiction *in the header* — the place a reader
// looks to sanity-check a finding — so it reads as the tool having lost four edges rather than
// as its having excluded them.
func TestTheHeaderCountsTheEdgesTheFindingsCompare(t *testing.T) {
	g := graph.New()
	for _, id := range []string{"/modules/a", "/modules/b"} {
		if err := g.AddNode(&graph.Node{ID: id, Kind: graph.KindModule, Path: id}); err != nil {
			t.Fatal(err)
		}
	}
	g.AddEdge(graph.Edge{From: "/modules/a", To: "/modules/b", Kind: graph.EdgeImports})
	if got := countStructural(g); got != 1 {
		t.Fatalf("countStructural = %d, want 1", got)
	}
	// Two co-change edges added and the count must not move, while Counts() does. Both halves,
	// because a count that ignored every edge would satisfy the first alone.
	g.AddEdge(graph.Edge{From: "/modules/a", To: "/modules/b", Kind: graph.EdgeCoChanges, Weight: 3})
	g.AddEdge(graph.Edge{From: "/modules/b", To: "/modules/a", Kind: graph.EdgeCoChanges, Weight: 3})
	if got := countStructural(g); got != 1 {
		t.Errorf("countStructural = %d after two co-change edges were added, want 1: the header "+
			"would state a change in a set the findings do not compare", got)
	}
	if _, all := g.Counts(); all != 3 {
		t.Errorf("Counts() = %d edges, want 3: this test's premise is that the two numbers "+
			"differ, and they no longer do", all)
	}
}

// movedPaths turns git's file renames into the path moves a node can be matched on. Table
// driven because each case is a different way the aggregation from file to directory can be
// wrong, and each has a one-line reason.
func TestMovedPathsAggregatesFilesToDirectories(t *testing.T) {
	for _, tc := range []struct {
		name    string
		renames map[string]string
		want    map[string]string
		absent  []string
	}{{
		// The ordinary case: every file under a directory moved to one place, so the
		// directory moved. This is what makes a module rename detectable at all, since a
		// module node is a directory (ADR 0003) and git only ever reports files.
		name:    "a whole directory",
		renames: map[string]string{"internal/auth/a.go": "internal/identity/a.go"},
		want:    map[string]string{"internal/auth": "internal/identity"},
	}, {
		// Two destinations is a split, and there is no answer. Refusing is the same call
		// ADR 0034 makes for an interpolated table name: a guess here would be asserted with
		// the same confidence as a fact read from the tree.
		name: "a split directory has no destination",
		renames: map[string]string{
			"internal/big/a.go": "internal/left/a.go",
			"internal/big/b.go": "internal/right/b.go",
		},
		absent: []string{"internal/big"},
	}, {
		// A file renamed in place moves no directory. Without the guard the source and the
		// destination directory are equal and every edited file would claim its own directory
		// had moved to itself.
		name:    "a file renamed within its directory",
		renames: map[string]string{"internal/auth/old.go": "internal/auth/new.go"},
		want:    map[string]string{"internal/auth/old.go": "internal/auth/new.go"},
		absent:  []string{"internal/auth"},
	}, {
		// The root is not a directory that can move. Its module node carries an empty Path,
		// so an entry for it would match every node that has no path at all — an external
		// dependency, for one.
		name:    "a file moved out of the root",
		renames: map[string]string{"main.go": "cmd/app/main.go"},
		want:    map[string]string{"main.go": "cmd/app/main.go"},
		absent:  []string{"", "."},
	}, {
		// A file rename must survive the directory aggregation, because a document node is
		// one file and its rename is the only thing that can pair it.
		name:    "a single file's own rename is kept",
		renames: map[string]string{"docs/design.md": "docs/reference/design.md"},
		want: map[string]string{
			"docs/design.md": "docs/reference/design.md",
			"docs":           "docs/reference",
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := movedPaths(tc.renames)
			for from, to := range tc.want {
				if got[from] != to {
					t.Errorf("movedPaths[%q] = %q, want %q", from, got[from], to)
				}
			}
			for _, k := range tc.absent {
				if dest, ok := got[k]; ok {
					t.Errorf("movedPaths has %q -> %q, want no entry: the move is not one this "+
						"can know", k, dest)
				}
			}
		})
	}
}

// A rename git reported into a path the new revision does not analyse — vendored, ignored,
// or under testdata — is a removal. The node is gone from the map even though the files
// exist, and reporting a rename to a node that is not in the graph would name a page nothing
// wrote.
func TestARenameIntoAnUnanalysedPathIsARemoval(t *testing.T) {
	before, after := graph.New(), graph.New()
	if err := before.AddNode(&graph.Node{
		ID: "/modules/auth", Kind: graph.KindModule, Path: "internal/auth",
	}); err != nil {
		t.Fatal(err)
	}
	if err := after.AddNode(&graph.Node{
		ID: "/modules/other", Kind: graph.KindModule, Path: "internal/other",
	}); err != nil {
		t.Fatal(err)
	}
	d := diffGraphs(before, after, map[string]string{"internal/auth": "vendor/auth"})
	if len(d.renamed) != 0 {
		t.Errorf("a rename to a path with no node was reported as a rename: %+v", d.renamed)
	}
	if len(d.removed) != 1 || d.removed[0].ID != "/modules/auth" {
		t.Errorf("removed = %+v, want the one node whose destination is not in the graph", d.removed)
	}
}

// A path that moved between two nodes of different kinds is not a rename. A directory holding
// a README produces both a module node and a document node, so pairing across kinds would
// report a renamed directory as a document that became a module.
func TestARenameDoesNotPairAcrossKinds(t *testing.T) {
	before, after := graph.New(), graph.New()
	if err := before.AddNode(&graph.Node{
		ID: "/modules/auth", Kind: graph.KindModule, Path: "internal/auth",
	}); err != nil {
		t.Fatal(err)
	}
	if err := after.AddNode(&graph.Node{
		ID: "/references/identity", Kind: graph.KindDocument, Path: "internal/identity",
	}); err != nil {
		t.Fatal(err)
	}
	d := diffGraphs(before, after, map[string]string{"internal/auth": "internal/identity"})
	if len(d.renamed) != 0 {
		t.Errorf("a module was paired with a document at the destination path: %+v", d.renamed)
	}
	if len(d.removed) != 1 || len(d.added) != 1 {
		t.Errorf("removed = %d, added = %d, want one of each", len(d.removed), len(d.added))
	}
}

// Two removed nodes cannot both pair with one destination. A directory that absorbed another
// would otherwise be reported as two renames into the same node, which claims one module is
// two.
func TestOneDestinationTakesOnlyOneRename(t *testing.T) {
	before, after := graph.New(), graph.New()
	for _, p := range []struct{ id, path string }{
		{"/modules/auth", "internal/auth"},
		{"/modules/login", "internal/login"},
	} {
		if err := before.AddNode(&graph.Node{
			ID: p.id, Kind: graph.KindModule, Path: p.path,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := after.AddNode(&graph.Node{
		ID: "/modules/identity", Kind: graph.KindModule, Path: "internal/identity",
	}); err != nil {
		t.Fatal(err)
	}
	d := diffGraphs(before, after, map[string]string{
		"internal/auth":  "internal/identity",
		"internal/login": "internal/identity",
	})
	if len(d.renamed) != 1 {
		t.Errorf("renamed = %d, want 1: two nodes paired with one destination", len(d.renamed))
	}
	if len(d.removed) != 1 {
		t.Errorf("removed = %d, want the node that did not get the destination", len(d.removed))
	}
}

// The default report is bounded and the bound is liftable, which is issue #41's finding
// applied to this command from the start rather than after somebody hits it. Both halves in
// one test, because either alone is satisfiable by a report that is always bounded or never.
func TestDiffBoundsItsFindingsAndAllListsThemAll(t *testing.T) {
	// One more node than the bound, so the elision is exercised at the boundary rather than
	// far past it.
	const n = defaultDiffLimit + 1
	before, after := graph.New(), graph.New()
	if err := before.AddNode(&graph.Node{
		ID: "/modules/root", Kind: graph.KindModule, Path: "app",
	}); err != nil {
		t.Fatal(err)
	}
	if err := after.AddNode(&graph.Node{
		ID: "/modules/root", Kind: graph.KindModule, Path: "app",
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("/modules/added-%02d", i)
		if err := after.AddNode(&graph.Node{
			ID: id, Kind: graph.KindModule, Path: fmt.Sprintf("app/added-%02d", i),
		}); err != nil {
			t.Fatal(err)
		}
		after.AddEdge(graph.Edge{From: "/modules/root", To: id, Kind: graph.EdgeImports})
	}
	d := diffGraphs(before, after, nil)

	bounded := renderDiff(d, defaultDiffLimit)
	if !strings.Contains(bounded, "and 1 more (-all lists them)") {
		t.Errorf("the default report does not truncate at %d, so a terminal gets every "+
			"finding:\n%s", defaultDiffLimit, bounded)
	}
	// The count line is a separate fact from the list, and the elision must not touch it: a
	// reader shown twenty of twenty-one still has to be told there are twenty-one, because
	// that number is the reason to reach for -all.
	if !strings.Contains(bounded, fmt.Sprintf("concepts added (%d)", n)) {
		t.Errorf("the heading does not state the full count:\n%s", bounded)
	}
	// Every truncation names the flag, checked per line rather than once for the report. The
	// marker is printed from one place today and asserting it globally would pass if a second
	// writer stopped naming it.
	for _, line := range strings.Split(bounded, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "and ") && !strings.Contains(line, "-all") {
			t.Errorf("a truncation does not name the flag that lifts it: %q", strings.TrimSpace(line))
		}
	}

	lifted := renderDiff(d, 0)
	if strings.Contains(lifted, " more") {
		t.Errorf("-all still truncates:\n%s", lifted)
	}
	// Counted against the graph rather than a literal, because the defect this catches is a
	// report that says 21 and lists 20.
	if got := countLines(lifted, "concepts added", "/modules/added-"); got != n {
		t.Errorf("-all listed %d of %d added concepts:\n%s", got, n, lifted)
	}
	if got := countLines(lifted, "edges gained", "-imports->"); got != n {
		t.Errorf("-all listed %d of %d gained edges:\n%s", got, n, lifted)
	}
}

// -all is a property of one invocation, so `.signpost.yml` may not set it (ADR 0011). Already
// asserted for `graph show`; asserted again here because this command registers its own flag
// and a reader looking for why it is not configurable looks at the command.
func TestDiffAllIsRefusedInTheConfigFile(t *testing.T) {
	root := diffRepo(t, map[string]string{
		"go.mod":                "module example.com/app\n\ngo 1.26\n",
		"internal/auth/auth.go": goModule("auth"),
	}, nil)
	writeConfig(t, root, "all: true\n")
	_, stderr, code := invoke(t, "graph", "diff", "-quiet", "HEAD", "HEAD", root)
	if code != 2 {
		t.Errorf("exit = %d, want 2: a file setting -all must be refused, not ignored", code)
	}
	if !strings.Contains(stderr, "all is not a key this file may set") {
		t.Errorf("the refusal does not say the key is one this file may not set:\n%s", stderr)
	}
	if strings.Contains(stderr, "unknown key") {
		t.Errorf("all fell through to the unknown-key branch:\n%s", stderr)
	}
}

// The three ways this command cannot work, each with its own message, because the remedies
// are different: install git, run it somewhere else, or make a commit. ADR 0035 makes this
// the first command in signpost that requires git rather than degrading without it, so the
// error is the whole of how a reader learns that.
func TestDiffRefusesWhatItCannotCompare(t *testing.T) {
	noRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(noRepo, "x.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	gitRun(t, empty, "init", "--quiet", "--initial-branch=main")

	for _, tc := range []struct {
		name string
		root string
		want string
	}{
		{"not a repository", noRepo, "not a git repository"},
		{"no commits yet", empty, "no commits yet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := invoke(t, "graph", "diff", "-quiet", "HEAD~1", "HEAD", tc.root)
			if code != 2 {
				t.Errorf("exit = %d, want 2 for a command that cannot be run here", code)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("the error does not say %q:\n%s", tc.want, stderr)
			}
		})
	}
}

// A misspelled revision is exit 2 and is caught before anything is checked out. The exit code
// is the distinction the rest of the binary keeps — 2 means the command line was wrong — and
// the ordering is worth a test because a bad *second* revision would otherwise be reported
// after the first had been checked out and analysed, which is twenty seconds spent to reject
// an invocation that was wrong before it started.
func TestDiffRejectsABadRevisionBeforeCheckingAnythingOut(t *testing.T) {
	root := diffRepo(t, map[string]string{
		"go.mod":                "module example.com/app\n\ngo 1.26\n",
		"internal/auth/auth.go": goModule("auth"),
	}, nil)
	for _, args := range [][]string{
		{"nosuchref", "HEAD"},
		{"HEAD", "nosuchref"},
	} {
		// Deliberately not -quiet: the coverage report is the evidence that an analysis ran,
		// and a bad second revision reported *after* the first one was checked out and
		// analysed is the twenty seconds this ordering exists to not spend.
		_, stderr, code := invoke(t, "graph", "diff", args[0], args[1], root)
		if code != 2 {
			t.Errorf("%v: exit = %d, want 2\n%s", args, code, stderr)
		}
		if !strings.Contains(stderr, "nosuchref") {
			t.Errorf("%v: the error does not name the revision that is wrong:\n%s", args, stderr)
		}
		if strings.Contains(stderr, "analysed") {
			t.Errorf("%v: a revision was analysed before both were resolved, so a typo in the "+
				"second one costs a checkout of the first:\n%s", args, stderr)
		}
	}
	// Neither revision was checked out, so no worktree survives. Asserted through git rather
	// than by looking for a directory: the administrative entry under .git/worktrees outlives
	// a removed checkout, and `worktree list` is what would report the leak.
	if out := gitRun(t, root, "worktree", "list"); strings.Contains(out, "signpost-diff-") {
		t.Errorf("a worktree was created for a revision that does not resolve:\n%s", out)
	}
}

// The command takes exactly two revisions. No `graph diff <ref>` shorthand meaning "against
// HEAD", because the third positional argument is a path: a one-revision form would make
// `graph diff HEAD~5 .` ambiguous, and resolving it by asking git whether `.` is a commit
// would decide an invocation's meaning from the contents of the repository.
func TestDiffNeedsTwoRevisions(t *testing.T) {
	root := diffRepo(t, map[string]string{
		"go.mod":                "module example.com/app\n\ngo 1.26\n",
		"internal/auth/auth.go": goModule("auth"),
	}, nil)
	for _, args := range [][]string{{}, {"HEAD"}, {"HEAD", "HEAD", root, "extra"}} {
		_, stderr, code := invoke(t, append([]string{"graph", "diff", "-quiet"}, args...)...)
		if code != 2 {
			t.Errorf("%v: exit = %d, want 2 for the wrong number of arguments\n%s",
				args, code, stderr)
		}
		// The example, because "expected two revisions" does not tell somebody what to type
		// and this command's argument order is not guessable from the error alone.
		if !strings.Contains(stderr, "graph diff HEAD~5 HEAD") {
			t.Errorf("%v: the error does not show a working invocation:\n%s", args, stderr)
		}
	}
}

// The temporary worktrees are removed on the way out, which is ADR 0035's one new failure
// mode: a killed process leaves a full copy of the repository under the system temp dir, and
// `git worktree list` reports it until somebody prunes. A successful run must not.
func TestDiffLeavesNoWorktreeBehind(t *testing.T) {
	root := diffRepo(t,
		map[string]string{
			"go.mod":                "module example.com/app\n\ngo 1.26\n",
			"internal/auth/auth.go": goModule("auth"),
		},
		map[string]string{"internal/store/store.go": goModule("store")})
	if _, stderr, code := invoke(t, "graph", "diff", "-quiet", "HEAD~1", "HEAD", root); code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	// Through git, not by looking for the directory. The two can disagree: `worktree remove`
	// failing leaves an administrative entry under .git/worktrees whose checkout is gone, and
	// that entry is the leak a reader would see.
	if out := gitRun(t, root, "worktree", "list"); strings.Contains(out, "signpost-diff-") {
		t.Errorf("a worktree survived the run:\n%s", out)
	}
	// And the working tree itself is untouched, which is the property the whole worktree
	// approach exists for — a checkout in place would have been the cheaper implementation and
	// would have moved the user's HEAD.
	if out := strings.TrimSpace(gitRun(t, root, "status", "--porcelain")); out != "" {
		t.Errorf("the working tree was modified:\n%s", out)
	}
	if head := strings.TrimSpace(gitRun(t, root, "rev-parse", "--abbrev-ref", "HEAD")); head != "main" {
		t.Errorf("HEAD = %q, want main: the run moved the checkout", head)
	}
}

// A path pointing into a subdirectory compares the same two commits as the root does. The
// revisions and the renames are all expressed relative to the top of the work tree, so a
// command run from `internal/` that silently compared a different tree would be wrong in a
// way nothing in the output would show.
func TestDiffAcceptsAPathInsideTheRepository(t *testing.T) {
	root := diffRepo(t,
		map[string]string{
			"go.mod":                "module example.com/app\n\ngo 1.26\n",
			"internal/auth/auth.go": goModule("auth"),
		},
		map[string]string{"internal/store/store.go": goModule("store")})
	fromRoot, _, code := invoke(t, "graph", "diff", "-quiet", "HEAD~1", "HEAD", root)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	fromSub, stderr, code := invoke(t, "graph", "diff", "-quiet", "HEAD~1", "HEAD",
		filepath.Join(root, "internal"))
	if code != 0 {
		t.Fatalf("subdirectory: exit = %d\n%s", code, stderr)
	}
	if fromRoot != fromSub {
		t.Errorf("a subdirectory produced a different diff:\n--- root ---\n%s\n--- internal ---\n%s",
			fromRoot, fromSub)
	}
}

// renderDiff is what runGraphDiff prints for the findings, at the given bound.
func renderDiff(d structuralDiff, lim int) string {
	var b strings.Builder
	p := newPrinter(&b)
	writeDiff(p, d, lim)
	return b.String()
}
