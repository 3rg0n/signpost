// Package graph is signpost's in-memory knowledge graph: typed nodes,
// typed-and-confidence-tagged edges, and the structural metrics the bundle
// reports.
//
// Every operation here is deterministic. Iteration over Go maps is randomized,
// so any traversal that reaches output sorts first. That is not stylistic:
// CI commits the bundle, so nondeterminism becomes commit churn.
package graph

import (
	"fmt"
	"sort"
	"strings"
)

// Kind classifies a node. Values double as the OKF `type` field, so they are
// human-facing strings rather than opaque identifiers.
type Kind string

const (
	KindModule    Kind = "Module"
	KindService   Kind = "Service"
	KindInterface Kind = "Interface"
	KindDataStore Kind = "Data Store"
	KindDocument  Kind = "Document"
	KindExternal  Kind = "External Dependency"
	KindSymbol    Kind = "Symbol"
)

// EdgeKind is the relationship a directed edge asserts. OKF links are untyped
// by design (SPEC §6); signpost carries these in a frontmatter `edges` key,
// which §11 requires consumers to tolerate.
type EdgeKind string

const (
	EdgeImports    EdgeKind = "imports"
	EdgeCalls      EdgeKind = "calls"
	EdgeImplements EdgeKind = "implements"
	EdgeDefines    EdgeKind = "defines"
	EdgeConfigures EdgeKind = "configures"
	EdgeDeploys    EdgeKind = "deploys"
	EdgeTestedBy   EdgeKind = "tested_by"
	EdgeDocuments  EdgeKind = "documents"
	EdgeCoChanges  EdgeKind = "co_changes"
	EdgeOwns       EdgeKind = "owns"
)

// Confidence records how an edge was established, so a consumer can weight
// what it trusts and a reviewer can audit what was guessed.
type Confidence string

const (
	// Extracted edges were read out of source or a manifest. Deterministic.
	Extracted Confidence = "extracted"
	// Inferred edges were derived by a model. Requires a grounding citation.
	Inferred Confidence = "inferred"
	// Ambiguous edges are model output the model itself flagged as uncertain.
	Ambiguous Confidence = "ambiguous"
)

// Node is one concept in the graph. ID is the bundle-relative OKF concept path
// without the .md suffix (SPEC §"ID and Naming"), e.g. "/modules/internal-auth".
type Node struct {
	ID    string
	Kind  Kind
	Title string
	// Description is a one-sentence summary. Deterministic extraction fills
	// this from manifests and doc headings; the semantic pass may replace it.
	Description string
	// Path is the repo-relative filesystem path this node describes, if any.
	// Empty for abstract concepts such as external dependencies.
	Path string
	// Lang is the dominant language, when meaningful ("go", "typescript", ...).
	Lang string
	Tags []string
	// Attrs carries kind-specific facts (ports, image, module version, ...).
	// Kept as strings so emit stays trivially stable.
	Attrs map[string]string
	// Files lists the repo-relative files aggregated into this node, sorted.
	Files []string
	// Cluster is assigned by Clusters(); -1 until then.
	Cluster int
}

// Edge is a directed, typed, confidence-tagged relationship.
type Edge struct {
	From string
	To   string
	Kind EdgeKind
	Conf Confidence
	// Weight carries a count where the edge kind has one (co-change pairs,
	// repeated imports). Zero means "not counted".
	Weight int
	// Source is the repo-relative file the edge was read from, for provenance.
	Source string
}

// Graph is a directed multigraph keyed by node ID. Parallel edges between the
// same pair are permitted when their Kind differs; identical (From,To,Kind)
// triples are merged by AddEdge, taking the stronger confidence and summed
// weight.
type Graph struct {
	nodes map[string]*Node
	edges map[edgeKey]*Edge
}

type edgeKey struct {
	from string
	to   string
	kind EdgeKind
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{
		nodes: make(map[string]*Node),
		edges: make(map[edgeKey]*Edge),
	}
}

// AddNode inserts n, or merges into an existing node with the same ID. Merge
// keeps the existing Title and Description when the incoming ones are empty,
// unions Tags/Files/Attrs, and never silently changes Kind — a Kind conflict
// is a bug in an extractor, so it is reported.
func (g *Graph) AddNode(n *Node) error {
	if n.ID == "" {
		return fmt.Errorf("graph: node with empty ID (title %q)", n.Title)
	}
	if n.Cluster == 0 {
		n.Cluster = -1
	}
	cur, ok := g.nodes[n.ID]
	if !ok {
		cp := *n
		cp.Tags = dedupe(cp.Tags)
		cp.Files = dedupe(cp.Files)
		if cp.Attrs == nil {
			cp.Attrs = make(map[string]string)
		}
		g.nodes[n.ID] = &cp
		return nil
	}
	if cur.Kind != n.Kind {
		return fmt.Errorf("graph: node %s already exists as %s, cannot re-add as %s", n.ID, cur.Kind, n.Kind)
	}
	if cur.Title == "" {
		cur.Title = n.Title
	}
	if cur.Description == "" {
		cur.Description = n.Description
	}
	if cur.Path == "" {
		cur.Path = n.Path
	}
	if cur.Lang == "" {
		cur.Lang = n.Lang
	}
	cur.Tags = dedupe(append(cur.Tags, n.Tags...))
	cur.Files = dedupe(append(cur.Files, n.Files...))
	for k, v := range n.Attrs {
		if _, exists := cur.Attrs[k]; !exists {
			cur.Attrs[k] = v
		}
	}
	return nil
}

// AddEdge inserts e, merging with an identical (From,To,Kind) triple by summing
// Weight and keeping the stronger confidence. Self-edges are dropped — they
// carry no signal here and would distort degree metrics.
//
// Edges to unknown nodes are permitted at build time; Dangling() reports them
// and the emitter drops them, because OKF consumers must tolerate broken links
// but signpost should not deliberately write them.
func (g *Graph) AddEdge(e Edge) {
	if e.From == "" || e.To == "" || e.From == e.To {
		return
	}
	k := edgeKey{e.From, e.To, e.Kind}
	cur, ok := g.edges[k]
	if !ok {
		cp := e
		g.edges[k] = &cp
		return
	}
	cur.Weight += e.Weight
	if stronger(e.Conf, cur.Conf) {
		cur.Conf = e.Conf
		cur.Source = e.Source
	}
}

// stronger reports whether a is a higher-trust confidence than b.
func stronger(a, b Confidence) bool { return confRank(a) > confRank(b) }

func confRank(c Confidence) int {
	switch c {
	case Extracted:
		return 3
	case Inferred:
		return 2
	case Ambiguous:
		return 1
	}
	return 0
}

// Node returns the node with the given ID, or nil.
func (g *Graph) Node(id string) *Node { return g.nodes[id] }

// Has reports whether a node with the given ID exists.
func (g *Graph) Has(id string) bool { _, ok := g.nodes[id]; return ok }

// Nodes returns every node sorted by ID.
func (g *Graph) Nodes() []*Node {
	out := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// NodesOfKind returns every node of the given kind, sorted by ID.
func (g *Graph) NodesOfKind(k Kind) []*Node {
	var out []*Node
	for _, n := range g.Nodes() {
		if n.Kind == k {
			out = append(out, n)
		}
	}
	return out
}

// Edges returns every edge sorted by (From, To, Kind).
func (g *Graph) Edges() []Edge {
	out := make([]Edge, 0, len(g.edges))
	for _, e := range g.edges {
		out = append(out, *e)
	}
	sortEdges(out)
	return out
}

// EdgesFrom returns outgoing edges from id, sorted, excluding those whose
// target does not exist.
func (g *Graph) EdgesFrom(id string) []Edge {
	var out []Edge
	for _, e := range g.edges {
		if e.From == id && g.Has(e.To) {
			out = append(out, *e)
		}
	}
	sortEdges(out)
	return out
}

// EdgesTo returns incoming edges to id, sorted, excluding those whose source
// does not exist.
func (g *Graph) EdgesTo(id string) []Edge {
	var out []Edge
	for _, e := range g.edges {
		if e.To == id && g.Has(e.From) {
			out = append(out, *e)
		}
	}
	sortEdges(out)
	return out
}

func sortEdges(es []Edge) {
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

// Dangling returns edges pointing at a node that does not exist, sorted. These
// are normal during extraction (an import of a package outside the repo) and
// are resolved by either creating an External node or dropping the edge.
func (g *Graph) Dangling() []Edge {
	var out []Edge
	for _, e := range g.edges {
		if !g.Has(e.From) || !g.Has(e.To) {
			out = append(out, *e)
		}
	}
	sortEdges(out)
	return out
}

// DropDangling removes edges whose endpoints are not both present, returning
// the count removed.
func (g *Graph) DropDangling() int {
	var drop []edgeKey
	for k, e := range g.edges {
		if !g.Has(e.From) || !g.Has(e.To) {
			drop = append(drop, k)
		}
	}
	// Sorted deletion keeps behaviour identical run to run even though map
	// deletion order is unobservable — cheap insurance against future changes
	// that make the order matter.
	sort.Slice(drop, func(i, j int) bool {
		if drop[i].from != drop[j].from {
			return drop[i].from < drop[j].from
		}
		if drop[i].to != drop[j].to {
			return drop[i].to < drop[j].to
		}
		return drop[i].kind < drop[j].kind
	})
	for _, k := range drop {
		delete(g.edges, k)
	}
	return len(drop)
}

// Counts returns node and edge totals.
func (g *Graph) Counts() (nodes, edges int) { return len(g.nodes), len(g.edges) }

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
