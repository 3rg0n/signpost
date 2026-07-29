package graph

import "sort"

// This file holds the structural analysis signpost reports. Every algorithm is
// hand-written, textbook, and deterministic — roughly 300 lines total, which is
// genuinely cheaper than taking on a graph-library dependency and its
// transitive tree.
//
// Determinism is load-bearing: CI commits the bundle, so a run that produces
// different clusters for the same commit produces commit churn. Every traversal
// iterates sorted node IDs and sorted adjacency.

// Degree is a node's connectivity. Total is In+Out over distinct edges.
type Degree struct {
	ID    string
	In    int
	Out   int
	Total int
}

// Degrees returns per-node degree, sorted by Total descending then ID, so the
// top of the slice is the hub list. Only edges with both endpoints present are
// counted; call DropDangling first for a clean count.
func (g *Graph) Degrees() []Degree {
	in := make(map[string]int, len(g.nodes))
	out := make(map[string]int, len(g.nodes))
	for _, e := range g.edges {
		if !g.Has(e.From) || !g.Has(e.To) {
			continue
		}
		out[e.From]++
		in[e.To]++
	}
	ds := make([]Degree, 0, len(g.nodes))
	for _, n := range g.Nodes() {
		ds = append(ds, Degree{ID: n.ID, In: in[n.ID], Out: out[n.ID], Total: in[n.ID] + out[n.ID]})
	}
	sort.Slice(ds, func(i, j int) bool {
		if ds[i].Total != ds[j].Total {
			return ds[i].Total > ds[j].Total
		}
		return ds[i].ID < ds[j].ID
	})
	return ds
}

// Hubs returns the top n node IDs by total degree. These are the "god nodes":
// the places where a wrong assumption propagates furthest, which is exactly
// what an agent should read first.
func (g *Graph) Hubs(n int) []Degree {
	ds := g.Degrees()
	if n > 0 && len(ds) > n {
		ds = ds[:n]
	}
	return ds
}

// Orphans returns nodes with no edges at all, sorted by ID. An orphan is
// usually one of three things worth surfacing: dead code, a doc nothing links
// to, or an extractor gap.
func (g *Graph) Orphans() []string {
	deg := make(map[string]int, len(g.nodes))
	for _, e := range g.edges {
		if !g.Has(e.From) || !g.Has(e.To) {
			continue
		}
		deg[e.From]++
		deg[e.To]++
	}
	var out []string
	for _, n := range g.Nodes() {
		if deg[n.ID] == 0 {
			out = append(out, n.ID)
		}
	}
	return out
}

// adjacency builds a sorted outgoing adjacency list. When kinds is non-empty
// only those edge kinds are included. Used by every traversal below so that
// iteration order is fixed.
func (g *Graph) adjacency(kinds ...EdgeKind) map[string][]string {
	want := make(map[EdgeKind]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	adj := make(map[string][]string, len(g.nodes))
	for _, n := range g.Nodes() {
		adj[n.ID] = nil
	}
	for _, e := range g.Edges() { // sorted
		if len(want) > 0 && !want[e.Kind] {
			continue
		}
		if !g.Has(e.From) || !g.Has(e.To) {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
	}
	for id := range adj {
		adj[id] = dedupe(adj[id])
	}
	return adj
}

// undirected builds a sorted symmetric adjacency list.
func (g *Graph) undirected(kinds ...EdgeKind) map[string][]string {
	want := make(map[EdgeKind]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	adj := make(map[string][]string, len(g.nodes))
	for _, n := range g.Nodes() {
		adj[n.ID] = nil
	}
	for _, e := range g.Edges() {
		if len(want) > 0 && !want[e.Kind] {
			continue
		}
		if !g.Has(e.From) || !g.Has(e.To) {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}
	for id := range adj {
		adj[id] = dedupe(adj[id])
	}
	return adj
}

// Cycles returns dependency cycles among the given edge kinds (default:
// imports), as strongly connected components of size > 1. Each cycle's node
// IDs are sorted, and the cycles themselves are sorted, so output is stable.
//
// An import cycle is a real finding: it means the two modules cannot be
// understood or changed independently.
//
// Tarjan's algorithm, iterative to avoid stack depth limits on large repos.
func (g *Graph) Cycles(kinds ...EdgeKind) [][]string {
	if len(kinds) == 0 {
		kinds = []EdgeKind{EdgeImports}
	}
	adj := g.adjacency(kinds...)

	index := make(map[string]int, len(adj))
	low := make(map[string]int, len(adj))
	onStack := make(map[string]bool, len(adj))
	var stack []string
	next := 0
	var comps [][]string

	// frame is one node's state in the iterative DFS.
	type frame struct {
		node string
		ai   int // next adjacency position to examine
	}

	for _, root := range sortedKeys(adj) {
		if _, seen := index[root]; seen {
			continue
		}
		work := []frame{{node: root}}
		index[root] = next
		low[root] = next
		next++
		stack = append(stack, root)
		onStack[root] = true

		for len(work) > 0 {
			f := &work[len(work)-1]
			if f.ai < len(adj[f.node]) {
				w := adj[f.node][f.ai]
				f.ai++
				if _, seen := index[w]; !seen {
					index[w] = next
					low[w] = next
					next++
					stack = append(stack, w)
					onStack[w] = true
					work = append(work, frame{node: w})
				} else if onStack[w] {
					if index[w] < low[f.node] {
						low[f.node] = index[w]
					}
				}
				continue
			}
			// Node exhausted: close it out.
			v := f.node
			work = work[:len(work)-1]
			if low[v] == index[v] {
				var comp []string
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					comp = append(comp, w)
					if w == v {
						break
					}
				}
				if len(comp) > 1 {
					sort.Strings(comp)
					comps = append(comps, comp)
				}
			}
			if len(work) > 0 {
				parent := work[len(work)-1].node
				if low[v] < low[parent] {
					low[parent] = low[v]
				}
			}
		}
	}
	sort.Slice(comps, func(i, j int) bool {
		if len(comps[i]) != len(comps[j]) {
			return len(comps[i]) > len(comps[j])
		}
		return comps[i][0] < comps[j][0]
	})
	return comps
}

// Components returns weakly connected components over all edge kinds, each
// sorted, ordered largest first. More than one component means the graph has
// islands — most often docs that describe code without any structural link to
// it, which is the drift the semantic pass exists to close.
func (g *Graph) Components() [][]string {
	adj := g.undirected()
	seen := make(map[string]bool, len(adj))
	var comps [][]string
	for _, root := range sortedKeys(adj) {
		if seen[root] {
			continue
		}
		var comp []string
		queue := []string{root}
		seen[root] = true
		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			comp = append(comp, v)
			for _, w := range adj[v] {
				if !seen[w] {
					seen[w] = true
					queue = append(queue, w)
				}
			}
		}
		sort.Strings(comp)
		comps = append(comps, comp)
	}
	sort.Slice(comps, func(i, j int) bool {
		if len(comps[i]) != len(comps[j]) {
			return len(comps[i]) > len(comps[j])
		}
		return comps[i][0] < comps[j][0]
	})
	return comps
}

// Clusters groups related nodes and writes the result to Node.Cluster,
// returning cluster ID -> sorted member IDs.
//
// Louvain modularity optimisation, hand-written (see louvain.go). The obvious
// cheaper choice is label propagation, and it was tried first: on a graph of
// two dense groups joined by one edge it collapses everything into a single
// community, because synchronous propagation with a lowest-label tie-break
// behaves like a min-label flood. That is LPA's documented giant-community
// pathology, and it makes the index headings useless — which is the one job
// clustering has here. Louvain costs about 150 lines more and gets it right.
//
// Determinism comes from structure, not from a seed: nodes are visited in
// sorted ID order, candidate communities are evaluated in ascending index
// order, ties break toward the lowest index, and the final numbering is by
// each cluster's lowest-sorting member. There is no randomisation anywhere.
func (g *Graph) Clusters() map[int][]string {
	adj := g.undirected()
	ids := sortedKeys(adj)
	label := louvain(ids, adj)

	// Renumber to dense, stable cluster IDs: order clusters by their
	// lowest-sorting member so the numbering does not depend on label values.
	groups := make(map[int][]string)
	for _, id := range ids {
		groups[label[id]] = append(groups[label[id]], id)
	}
	type rep struct {
		first string // lowest-sorting member, the stable ordering key
		label int
	}
	reps := make([]rep, 0, len(groups))
	for l, members := range groups {
		sort.Strings(members)
		groups[l] = members
		reps = append(reps, rep{first: members[0], label: l})
	}
	sort.Slice(reps, func(i, j int) bool { return reps[i].first < reps[j].first })

	out := make(map[int][]string, len(reps))
	for newID, r := range reps {
		members := groups[r.label]
		out[newID] = members
		for _, id := range members {
			if n := g.nodes[id]; n != nil {
				n.Cluster = newID
			}
		}
	}
	return out
}

// Bridge is an edge crossing a cluster boundary.
type Bridge struct {
	Edge
	FromCluster int
	ToCluster   int
}

// Bridges returns cross-cluster edges, sorted. Call Clusters first. These are
// where a change is most likely to surprise: the two sides are maintained as
// separate concerns but are coupled anyway.
func (g *Graph) Bridges() []Bridge {
	var out []Bridge
	for _, e := range g.Edges() {
		from, to := g.nodes[e.From], g.nodes[e.To]
		if from == nil || to == nil {
			continue
		}
		if from.Cluster != to.Cluster && from.Cluster >= 0 && to.Cluster >= 0 {
			out = append(out, Bridge{Edge: e, FromCluster: from.Cluster, ToCluster: to.Cluster})
		}
	}
	return out
}

// Path returns a shortest path from -> to over the given edge kinds (all kinds
// when empty), as node IDs inclusive of both ends. Nil when unreachable.
// BFS over sorted adjacency, so the path chosen among equals is stable.
func (g *Graph) Path(from, to string, kinds ...EdgeKind) []string {
	if !g.Has(from) || !g.Has(to) {
		return nil
	}
	if from == to {
		return []string{from}
	}
	adj := g.adjacency(kinds...)
	prev := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		for _, w := range adj[v] {
			if _, seen := prev[w]; seen {
				continue
			}
			prev[w] = v
			if w == to {
				var path []string
				for cur := to; cur != ""; cur = prev[cur] {
					path = append(path, cur)
				}
				reverse(path)
				return path
			}
			queue = append(queue, w)
		}
	}
	return nil
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
