package graph

import (
	"reflect"
	"testing"
)

// mustNode adds a module node, failing the test on error.
func mustNode(t *testing.T, g *Graph, id string) {
	t.Helper()
	if err := g.AddNode(&Node{ID: id, Kind: KindModule, Title: id}); err != nil {
		t.Fatalf("AddNode(%s): %v", id, err)
	}
}

// build makes a graph from node ids and from->to import edges.
func build(t *testing.T, ids []string, imports [][2]string) *Graph {
	t.Helper()
	g := New()
	for _, id := range ids {
		mustNode(t, g, id)
	}
	for _, e := range imports {
		g.AddEdge(Edge{From: e[0], To: e[1], Kind: EdgeImports, Conf: Extracted})
	}
	return g
}

func TestAddNodeRejectsEmptyID(t *testing.T) {
	g := New()
	if err := g.AddNode(&Node{Kind: KindModule, Title: "x"}); err == nil {
		t.Fatal("expected error for empty node ID")
	}
}

func TestAddNodeKindConflictIsAnError(t *testing.T) {
	g := New()
	mustNode(t, g, "/m/a")
	err := g.AddNode(&Node{ID: "/m/a", Kind: KindService, Title: "a"})
	if err == nil {
		t.Fatal("expected error when re-adding a node with a different Kind")
	}
}

func TestAddNodeMergesWithoutClobbering(t *testing.T) {
	g := New()
	if err := g.AddNode(&Node{
		ID: "/m/a", Kind: KindModule, Title: "auth",
		Description: "original", Tags: []string{"go"},
		Files: []string{"a.go"}, Attrs: map[string]string{"port": "8080"},
	}); err != nil {
		t.Fatal(err)
	}
	// A second extractor contributes more facts but must not overwrite prose.
	if err := g.AddNode(&Node{
		ID: "/m/a", Kind: KindModule, Description: "replacement",
		Tags: []string{"security", "go"}, Files: []string{"b.go", "a.go"},
		Attrs: map[string]string{"port": "9090", "image": "wolfi"},
	}); err != nil {
		t.Fatal(err)
	}
	n := g.Node("/m/a")
	if n.Description != "original" {
		t.Errorf("Description = %q, want the original preserved", n.Description)
	}
	if got, want := n.Tags, []string{"go", "security"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Tags = %v, want %v (unioned + sorted)", got, want)
	}
	if got, want := n.Files, []string{"a.go", "b.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Files = %v, want %v (deduped + sorted)", got, want)
	}
	if n.Attrs["port"] != "8080" {
		t.Errorf("Attrs[port] = %q, want 8080 preserved", n.Attrs["port"])
	}
	if n.Attrs["image"] != "wolfi" {
		t.Errorf("Attrs[image] = %q, want the new key added", n.Attrs["image"])
	}
}

func TestAddEdgeDropsSelfEdges(t *testing.T) {
	g := build(t, []string{"/m/a"}, [][2]string{{"/m/a", "/m/a"}})
	if _, edges := g.Counts(); edges != 0 {
		t.Errorf("edges = %d, want 0 (self-edge dropped)", edges)
	}
}

func TestAddEdgeMergesWeightAndKeepsStrongerConfidence(t *testing.T) {
	g := New()
	mustNode(t, g, "/m/a")
	mustNode(t, g, "/m/b")
	g.AddEdge(Edge{From: "/m/a", To: "/m/b", Kind: EdgeImports, Conf: Inferred, Weight: 2})
	g.AddEdge(Edge{From: "/m/a", To: "/m/b", Kind: EdgeImports, Conf: Extracted, Weight: 3, Source: "a.go"})

	es := g.Edges()
	if len(es) != 1 {
		t.Fatalf("edges = %d, want 1 (merged)", len(es))
	}
	if es[0].Weight != 5 {
		t.Errorf("Weight = %d, want 5 (summed)", es[0].Weight)
	}
	if es[0].Conf != Extracted {
		t.Errorf("Conf = %q, want extracted (stronger wins)", es[0].Conf)
	}
	if es[0].Source != "a.go" {
		t.Errorf("Source = %q, want a.go (from the stronger edge)", es[0].Source)
	}
}

func TestAddEdgeKeepsStrongerConfidenceRegardlessOfOrder(t *testing.T) {
	g := New()
	mustNode(t, g, "/m/a")
	mustNode(t, g, "/m/b")
	// Stronger first this time; the weaker must not downgrade it.
	g.AddEdge(Edge{From: "/m/a", To: "/m/b", Kind: EdgeImports, Conf: Extracted, Source: "a.go"})
	g.AddEdge(Edge{From: "/m/a", To: "/m/b", Kind: EdgeImports, Conf: Ambiguous, Source: "guess"})

	es := g.Edges()
	if es[0].Conf != Extracted {
		t.Errorf("Conf = %q, want extracted retained", es[0].Conf)
	}
	if es[0].Source != "a.go" {
		t.Errorf("Source = %q, want a.go retained", es[0].Source)
	}
}

func TestParallelEdgesOfDifferentKindsCoexist(t *testing.T) {
	g := New()
	mustNode(t, g, "/m/a")
	mustNode(t, g, "/m/b")
	g.AddEdge(Edge{From: "/m/a", To: "/m/b", Kind: EdgeImports, Conf: Extracted})
	g.AddEdge(Edge{From: "/m/a", To: "/m/b", Kind: EdgeCoChanges, Conf: Extracted, Weight: 7})
	if _, edges := g.Counts(); edges != 2 {
		t.Errorf("edges = %d, want 2 (different kinds are distinct)", edges)
	}
}

func TestDanglingAndDrop(t *testing.T) {
	g := New()
	mustNode(t, g, "/m/a")
	g.AddEdge(Edge{From: "/m/a", To: "/ext/nowhere", Kind: EdgeImports, Conf: Extracted})

	if d := g.Dangling(); len(d) != 1 {
		t.Fatalf("Dangling = %d, want 1", len(d))
	}
	// A dangling edge must not inflate degree.
	for _, d := range g.Degrees() {
		if d.Total != 0 {
			t.Errorf("node %s degree = %d, want 0 (dangling excluded)", d.ID, d.Total)
		}
	}
	if n := g.DropDangling(); n != 1 {
		t.Errorf("DropDangling = %d, want 1", n)
	}
	if _, edges := g.Counts(); edges != 0 {
		t.Errorf("edges = %d, want 0 after drop", edges)
	}
}

func TestDegreesAndHubsOrdering(t *testing.T) {
	// b is the hub: a->b, c->b, b->d.
	g := build(t, []string{"/m/a", "/m/b", "/m/c", "/m/d"}, [][2]string{
		{"/m/a", "/m/b"}, {"/m/c", "/m/b"}, {"/m/b", "/m/d"},
	})
	hubs := g.Hubs(2)
	if len(hubs) != 2 {
		t.Fatalf("Hubs(2) returned %d", len(hubs))
	}
	if hubs[0].ID != "/m/b" {
		t.Errorf("top hub = %s, want /m/b", hubs[0].ID)
	}
	if hubs[0].In != 2 || hubs[0].Out != 1 || hubs[0].Total != 3 {
		t.Errorf("hub degree = in%d/out%d/total%d, want in2/out1/total3", hubs[0].In, hubs[0].Out, hubs[0].Total)
	}
}

func TestOrphans(t *testing.T) {
	g := build(t, []string{"/m/a", "/m/b", "/m/lonely"}, [][2]string{{"/m/a", "/m/b"}})
	if got, want := g.Orphans(), []string{"/m/lonely"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Orphans = %v, want %v", got, want)
	}
}

func TestCyclesFindsSCCsNotTrees(t *testing.T) {
	// a->b->c->a is a cycle; d->e is not.
	g := build(t, []string{"/m/a", "/m/b", "/m/c", "/m/d", "/m/e"}, [][2]string{
		{"/m/a", "/m/b"}, {"/m/b", "/m/c"}, {"/m/c", "/m/a"},
		{"/m/d", "/m/e"},
	})
	cycles := g.Cycles()
	if len(cycles) != 1 {
		t.Fatalf("Cycles = %v, want exactly one", cycles)
	}
	if got, want := cycles[0], []string{"/m/a", "/m/b", "/m/c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cycle = %v, want %v", got, want)
	}
}

func TestCyclesFindsTwoDisjointCycles(t *testing.T) {
	g := build(t, []string{"/m/a", "/m/b", "/m/x", "/m/y", "/m/z"}, [][2]string{
		{"/m/a", "/m/b"}, {"/m/b", "/m/a"},
		{"/m/x", "/m/y"}, {"/m/y", "/m/z"}, {"/m/z", "/m/x"},
	})
	cycles := g.Cycles()
	if len(cycles) != 2 {
		t.Fatalf("Cycles = %v, want 2", cycles)
	}
	// Larger component first.
	if len(cycles[0]) != 3 || len(cycles[1]) != 2 {
		t.Errorf("cycle sizes = %d,%d; want 3,2 (largest first)", len(cycles[0]), len(cycles[1]))
	}
}

// A long chain exercises the iterative DFS: a recursive Tarjan would risk
// stack exhaustion on a large monorepo, so depth must not be a limit.
func TestCyclesDeepChainDoesNotOverflow(t *testing.T) {
	const n = 20000
	ids := make([]string, 0, n)
	edges := make([][2]string, 0, n)
	for i := 0; i < n; i++ {
		ids = append(ids, "/m/"+pad(i))
	}
	for i := 0; i < n-1; i++ {
		edges = append(edges, [2]string{"/m/" + pad(i), "/m/" + pad(i+1)})
	}
	// Close the loop so the whole chain is one SCC.
	edges = append(edges, [2]string{"/m/" + pad(n-1), "/m/" + pad(0)})

	g := build(t, ids, edges)
	cycles := g.Cycles()
	if len(cycles) != 1 || len(cycles[0]) != n {
		t.Fatalf("expected one SCC of %d nodes, got %d cycles", n, len(cycles))
	}
}

func TestComponentsSeparatesIslands(t *testing.T) {
	// Two islands: {a,b} and {docs}. This is the doc/code drift shape.
	g := build(t, []string{"/m/a", "/m/b", "/docs/spec"}, [][2]string{{"/m/a", "/m/b"}})
	comps := g.Components()
	if len(comps) != 2 {
		t.Fatalf("Components = %v, want 2", comps)
	}
	if got, want := comps[0], []string{"/m/a", "/m/b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("largest component = %v, want %v", got, want)
	}
	if got, want := comps[1], []string{"/docs/spec"}; !reflect.DeepEqual(got, want) {
		t.Errorf("second component = %v, want %v", got, want)
	}
}

func TestClustersSeparateDenseGroups(t *testing.T) {
	// Two triangles joined by a single bridge edge.
	g := build(t,
		[]string{"/m/a1", "/m/a2", "/m/a3", "/m/b1", "/m/b2", "/m/b3"},
		[][2]string{
			{"/m/a1", "/m/a2"}, {"/m/a2", "/m/a3"}, {"/m/a3", "/m/a1"},
			{"/m/b1", "/m/b2"}, {"/m/b2", "/m/b3"}, {"/m/b3", "/m/b1"},
			{"/m/a1", "/m/b1"},
		})
	clusters := g.Clusters()
	if len(clusters) != 2 {
		t.Fatalf("clusters = %v, want 2 groups", clusters)
	}
	// Every a* shares a cluster, and it differs from the b* cluster.
	ca := g.Node("/m/a1").Cluster
	cb := g.Node("/m/b1").Cluster
	if ca == cb {
		t.Fatal("the two triangles collapsed into one cluster")
	}
	for _, id := range []string{"/m/a2", "/m/a3"} {
		if g.Node(id).Cluster != ca {
			t.Errorf("%s cluster = %d, want %d", id, g.Node(id).Cluster, ca)
		}
	}
	for _, id := range []string{"/m/b2", "/m/b3"} {
		if g.Node(id).Cluster != cb {
			t.Errorf("%s cluster = %d, want %d", id, g.Node(id).Cluster, cb)
		}
	}
}

// Determinism is the property CI depends on: same input, identical clusters,
// every time. Map iteration is randomized per run within a process, so
// repeating in-process is a real test of the sorting discipline.
func TestClustersAreDeterministic(t *testing.T) {
	ids := []string{"/m/a1", "/m/a2", "/m/a3", "/m/b1", "/m/b2", "/m/b3", "/m/c1"}
	edges := [][2]string{
		{"/m/a1", "/m/a2"}, {"/m/a2", "/m/a3"}, {"/m/a3", "/m/a1"},
		{"/m/b1", "/m/b2"}, {"/m/b2", "/m/b3"}, {"/m/b3", "/m/b1"},
		{"/m/a1", "/m/b1"}, {"/m/c1", "/m/a2"},
	}
	var first map[int][]string
	for i := 0; i < 25; i++ {
		g := build(t, ids, edges)
		got := g.Clusters()
		if i == 0 {
			first = got
			continue
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d produced different clusters:\n got %v\nwant %v", i, got, first)
		}
	}
}

func TestBridgesCrossClustersOnly(t *testing.T) {
	g := build(t,
		[]string{"/m/a1", "/m/a2", "/m/a3", "/m/b1", "/m/b2", "/m/b3"},
		[][2]string{
			{"/m/a1", "/m/a2"}, {"/m/a2", "/m/a3"}, {"/m/a3", "/m/a1"},
			{"/m/b1", "/m/b2"}, {"/m/b2", "/m/b3"}, {"/m/b3", "/m/b1"},
			{"/m/a1", "/m/b1"},
		})
	g.Clusters()
	bridges := g.Bridges()
	if len(bridges) != 1 {
		t.Fatalf("Bridges = %v, want exactly the one crossing edge", bridges)
	}
	if bridges[0].From != "/m/a1" || bridges[0].To != "/m/b1" {
		t.Errorf("bridge = %s->%s, want /m/a1->/m/b1", bridges[0].From, bridges[0].To)
	}
}

func TestPath(t *testing.T) {
	g := build(t, []string{"/m/a", "/m/b", "/m/c", "/m/d"}, [][2]string{
		{"/m/a", "/m/b"}, {"/m/b", "/m/c"},
	})
	if got, want := g.Path("/m/a", "/m/c"), []string{"/m/a", "/m/b", "/m/c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Path = %v, want %v", got, want)
	}
	if got := g.Path("/m/a", "/m/d"); got != nil {
		t.Errorf("Path to unreachable = %v, want nil", got)
	}
	if got, want := g.Path("/m/a", "/m/a"), []string{"/m/a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Path to self = %v, want %v", got, want)
	}
	if got := g.Path("/m/a", "/m/missing"); got != nil {
		t.Errorf("Path to nonexistent node = %v, want nil", got)
	}
}

func TestPathIsDeterministicAmongEqualLengths(t *testing.T) {
	// Two shortest paths exist: via /m/x and via /m/y. Sorted adjacency must
	// make the choice stable.
	ids := []string{"/m/a", "/m/x", "/m/y", "/m/z"}
	edges := [][2]string{
		{"/m/a", "/m/x"}, {"/m/a", "/m/y"},
		{"/m/x", "/m/z"}, {"/m/y", "/m/z"},
	}
	var first []string
	for i := 0; i < 25; i++ {
		g := build(t, ids, edges)
		got := g.Path("/m/a", "/m/z")
		if len(got) != 3 {
			t.Fatalf("path length = %d, want 3", len(got))
		}
		if i == 0 {
			first = got
			continue
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d chose a different path: %v vs %v", i, got, first)
		}
	}
}

func TestEdgesFromExcludesDangling(t *testing.T) {
	g := New()
	mustNode(t, g, "/m/a")
	mustNode(t, g, "/m/b")
	g.AddEdge(Edge{From: "/m/a", To: "/m/b", Kind: EdgeImports, Conf: Extracted})
	g.AddEdge(Edge{From: "/m/a", To: "/ext/gone", Kind: EdgeImports, Conf: Extracted})

	from := g.EdgesFrom("/m/a")
	if len(from) != 1 || from[0].To != "/m/b" {
		t.Errorf("EdgesFrom = %v, want only the resolvable edge", from)
	}
	if to := g.EdgesTo("/m/b"); len(to) != 1 || to[0].From != "/m/a" {
		t.Errorf("EdgesTo = %v, want the one incoming edge", to)
	}
}

// pad formats i as a fixed-width string so lexical order matches numeric order,
// keeping the deep-chain fixture's sorted traversal aligned with its topology.
func pad(i int) string {
	const width = 6
	s := ""
	for n := i; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	if s == "" {
		s = "0"
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}
