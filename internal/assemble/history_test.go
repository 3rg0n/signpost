package assemble

import (
	"testing"

	"github.com/3rg0n/signpost/internal/graph"
	"github.com/3rg0n/signpost/internal/vcs"
)

// twoModules is the layout the history tests annotate: two directories with source, and
// a third path under one of them that holds none.
var twoModules = map[string]string{
	"go.mod":                "module example.com/app\n\ngo 1.26\n",
	"internal/auth/auth.go": "package auth\n\nfunc Check() bool { return true }\n",
	"internal/db/db.go":     "package db\n\nfunc Get() bool { return true }\n",
}

// dir is a directory history, written positionally because these tests care about the
// numbers rather than about which field holds them.
func dir(path string, commits, ins, del int, authors map[string]int) *vcs.PathHistory {
	return &vcs.PathHistory{
		Path: path, Commits: commits, Insertions: ins, Deletions: del, Authors: authors,
	}
}

func signals(dirs map[string]*vcs.PathHistory, pairs ...vcs.Pair) *vcs.Signals {
	return &vcs.Signals{
		Available: true, Commits: 10,
		Paths: map[string]*vcs.PathHistory{}, Dirs: dirs, CoChange: pairs,
	}
}

func TestHistoryAnnotatesModuleNodes(t *testing.T) {
	out := buildWithHistory(t, twoModules, signals(map[string]*vcs.PathHistory{
		"internal/auth": {
			Path: "internal/auth", Commits: 4, Insertions: 90, Deletions: 12,
			First: "2026-01-01", Last: "2026-06-01",
			Authors: map[string]int{"Ann": 3, "Bob": 1},
		},
	}))

	n := node(t, out.Graph, "/modules/auth")
	want := map[string]string{
		"commits": "4", "lines_added": "90", "lines_removed": "12",
		"first_commit": "2026-01-01", "last_commit": "2026-06-01",
		"top_author": "Ann", "top_author_share": "75%",
	}
	for k, v := range want {
		if n.Attrs[k] != v {
			t.Errorf("%s = %q, want %q", k, n.Attrs[k], v)
		}
	}
	// The other module had no history entry, which is not an error and not a zero: no
	// attribute at all, so a consumer can tell "not measured" from "measured as none".
	if db := node(t, out.Graph, "/modules/db"); db.Attrs["commits"] != "" {
		t.Errorf("db commits = %q, want absent", db.Attrs["commits"])
	}
}

// History annotates the map; it never decides what is on it. A directory with history
// and no source is not a module — deleted code still has history, and a node for it
// would be a page about something that is not there.
func TestHistoryCreatesNoNodes(t *testing.T) {
	out := buildWithHistory(t, twoModules, signals(map[string]*vcs.PathHistory{
		"internal/deleted": dir("internal/deleted", 30, 500, 500, map[string]int{"Ann": 30}),
	}))

	if n := out.Graph.Node("/modules/deleted"); n != nil {
		t.Errorf("history invented a node: %+v", n)
	}
}

// Insertions and deletions stay separate rather than netted: +900/-880 and +20/-0 net
// the same and describe entirely different modules.
func TestHistoryKeepsAddedAndRemovedSeparate(t *testing.T) {
	out := buildWithHistory(t, twoModules, signals(map[string]*vcs.PathHistory{
		"internal/auth": dir("internal/auth", 2, 900, 880, map[string]int{"Ann": 2}),
	}))

	n := node(t, out.Graph, "/modules/auth")
	if n.Attrs["lines_added"] != "900" || n.Attrs["lines_removed"] != "880" {
		t.Errorf("churn = +%s/-%s, want the two reported separately",
			n.Attrs["lines_added"], n.Attrs["lines_removed"])
	}
}

// The share is rounded to a whole percent: the precision beyond that is spurious, and a
// long decimal in a committed file would change whenever a single commit landed.
func TestHistoryRoundsAuthorShare(t *testing.T) {
	out := buildWithHistory(t, twoModules, signals(map[string]*vcs.PathHistory{
		// 2 of 3 is 66.66…%, which rounds up to 67.
		"internal/auth": dir("internal/auth", 3, 1, 0, map[string]int{"Ann": 2, "Bob": 1}),
	}))

	if got := node(t, out.Graph, "/modules/auth").Attrs["top_author_share"]; got != "67%" {
		t.Errorf("share = %q, want 67%%", got)
	}
}

func TestCoChangeEdgeIsSymmetricAndExtracted(t *testing.T) {
	out := buildWithHistory(t, twoModules, signals(nil,
		vcs.Pair{A: "internal/auth", B: "internal/db", Commits: 7},
	))
	g := out.Graph

	// Both directions, because co-change is symmetric and the graph is directed: one
	// direction would make the edge's meaning depend on which page a reader opened.
	for _, d := range [2][2]string{{"/modules/auth", "/modules/db"}, {"/modules/db", "/modules/auth"}} {
		if !hasEdge(g, d[0], d[1], graph.EdgeCoChanges) {
			t.Fatalf("no co-change edge %s -> %s", d[0], d[1])
		}
	}
	for _, e := range g.EdgesFrom("/modules/auth") {
		if e.Kind != graph.EdgeCoChanges {
			continue
		}
		// Extracted, not Inferred: what is extracted is that the two directories appeared
		// in the same commits seven times, which is read from git. The edge claims no
		// reason for the coupling.
		if e.Conf != graph.Extracted {
			t.Errorf("confidence = %q, want extracted", e.Conf)
		}
		if e.Weight != 7 {
			t.Errorf("weight = %d, want 7", e.Weight)
		}
		// The provenance is the repository's history rather than any file in the tree, so
		// naming a file here would misattribute it.
		if e.Source != "" {
			t.Errorf("Source = %q, want empty", e.Source)
		}
	}
}

// The case the max-fold exists for. Two distinct directory pairs resolve to the same
// module pair, because a commit touching `internal/auth/testdata` is a commit touching
// `internal/auth`. AddEdge sums weights on an identical triple, and since one commit can
// appear in both pairs the sum can exceed the number of commits that actually touched
// both modules. The maximum is a true lower bound; the sum is not a bound at all.
func TestCoChangeFoldsCollapsedPairsWithMaxNotSum(t *testing.T) {
	out := buildWithHistory(t, twoModules, signals(nil,
		vcs.Pair{A: "internal/auth/testdata", B: "internal/db", Commits: 5},
		vcs.Pair{A: "internal/auth", B: "internal/db", Commits: 3},
	))

	found := false
	for _, e := range out.Graph.EdgesFrom("/modules/auth") {
		if e.Kind != graph.EdgeCoChanges {
			continue
		}
		found = true
		if e.Weight != 5 {
			t.Errorf("weight = %d, want 5 (the maximum, not the sum of 5 and 3)", e.Weight)
		}
	}
	if !found {
		t.Fatal("the collapsed pairs produced no edge at all")
	}
}

// Both directory pairs resolving to the same module is a self-edge, which says nothing:
// a module changes with itself in every commit that touches it.
func TestCoChangeWithinOneModuleDrawsNoEdge(t *testing.T) {
	out := buildWithHistory(t, twoModules, signals(nil,
		vcs.Pair{A: "internal/auth", B: "internal/auth/testdata", Commits: 9},
	))

	for _, e := range out.Graph.Edges() {
		if e.Kind == graph.EdgeCoChanges {
			t.Errorf("a within-module pair produced an edge: %+v", e)
		}
	}
	if out.DroppedEdges != 0 {
		t.Errorf("DroppedEdges = %d: the pair must be dropped before AddEdge, not after",
			out.DroppedEdges)
	}
}

// A pair naming a directory no module covers has nothing to attach to. Dropped rather
// than attached to the root, which would make every docs-only commit look like coupling
// to the repository itself.
func TestCoChangeDropsPairsWithNoModule(t *testing.T) {
	out := buildWithHistory(t, map[string]string{
		"internal/auth/auth.go": "package auth\n\nfunc Check() {}\n",
	}, signals(nil,
		vcs.Pair{A: "docs", B: "internal/auth", Commits: 6},
	))

	for _, e := range out.Graph.Edges() {
		if e.Kind == graph.EdgeCoChanges {
			t.Errorf("a pair with no module on one side produced an edge: %+v", e)
		}
	}
}

// Unavailable signals — no git, no repository, an empty history — contribute nothing and
// are not an error. The structural bundle is complete without any of this.
func TestUnavailableHistoryContributesNothing(t *testing.T) {
	unavailable := &vcs.Signals{
		Reason: "not a git repository",
		Paths:  map[string]*vcs.PathHistory{},
		// Populated to prove Available is what gates the passes, not emptiness.
		Dirs:     map[string]*vcs.PathHistory{"internal/auth": dir("internal/auth", 9, 1, 0, nil)},
		CoChange: []vcs.Pair{{A: "internal/auth", B: "internal/db", Commits: 4}},
	}
	out := buildWithHistory(t, twoModules, unavailable)

	if got := node(t, out.Graph, "/modules/auth").Attrs["commits"]; got != "" {
		t.Errorf("commits = %q, want absent from unavailable signals", got)
	}
	for _, e := range out.Graph.Edges() {
		if e.Kind == graph.EdgeCoChanges {
			t.Errorf("unavailable signals produced an edge: %+v", e)
		}
	}
}

// §8.1 determinism, at the layer where it is easiest to lose: the fold walks a map, and
// map iteration order is randomised per run.
func TestHistoryAssemblyIsDeterministic(t *testing.T) {
	dirs := map[string]*vcs.PathHistory{
		"internal/auth": dir("internal/auth", 4, 9, 2, map[string]int{"Ann": 2, "Bob": 2}),
		"internal/db":   dir("internal/db", 3, 5, 1, map[string]int{"Bob": 3}),
		"":              dir("", 6, 20, 4, map[string]int{"Ann": 6}),
	}
	pairs := []vcs.Pair{
		{A: "internal/auth/testdata", B: "internal/db", Commits: 5},
		{A: "internal/auth", B: "internal/db", Commits: 3},
	}
	first := ""
	for i := 0; i < 10; i++ {
		got := render(buildWithHistory(t, twoModules, signals(dirs, pairs...)))
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("run %d differed:\n%s\n---\n%s", i, got, first)
		}
	}
}
