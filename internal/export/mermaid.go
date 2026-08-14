package export

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/graph"
)

// Mermaid is the format that gets pasted into a PR description or an issue, so it
// optimises for being readable at a glance rather than for completeness: node
// shapes carry kind, dashed arrows carry inferred confidence, and edge labels
// carry the relationship.
//
// GitHub renders Mermaid natively, which is why it earns a place here over the
// data formats for the review use case.

// mermaidShape wraps a label in the delimiters Mermaid uses for a node shape.
//
// The shapes are chosen so kind is legible without a legend: a service is the
// thing that runs (stadium), an interface is the boundary you talk through
// (hexagon), a data store is where state lives (cylinder), a document is
// something written (parallelogram), an external dependency is outside the
// repository (double circle), a pipeline is work that runs and finishes
// (subroutine).
func mermaidShape(k graph.Kind, label string) string {
	switch k {
	case graph.KindPipeline:
		return "[[" + label + "]]"
	case graph.KindService:
		return "([" + label + "])"
	case graph.KindInterface:
		return "{{" + label + "}}"
	case graph.KindDataStore:
		return "[(" + label + ")]"
	case graph.KindDocument:
		return "[/" + label + "/]"
	case graph.KindExternal:
		return "(((" + label + ")))"
	}
	return "[" + label + "]"
}

func writeMermaid(w io.Writer, g *graph.Graph) error {
	bw := &errWriter{w: w}
	bw.line("flowchart LR")

	names := mermaidNames(g)
	nodes := g.Nodes()

	// Clusters become subgraphs when they exist. A diagram of forty flat nodes is
	// unreadable, and the clusters are already the answer to "what belongs
	// together".
	byCluster := map[int][]*graph.Node{}
	for _, n := range nodes {
		byCluster[n.Cluster] = append(byCluster[n.Cluster], n)
	}
	clustered := len(byCluster) > 1 || !hasKey(byCluster, -1)

	for _, cid := range sortedIntKeys(byCluster) {
		members := byCluster[cid]
		indent := "    "
		if clustered && cid >= 0 {
			bw.line("    subgraph cluster%d[\"cluster %d\"]", cid, cid)
			indent = "        "
		}
		for _, n := range members {
			bw.line("%s%s%s", indent, names[n.ID], mermaidShape(n.Kind, mermaidLabel(n)))
		}
		if clustered && cid >= 0 {
			bw.line("    end")
		}
	}

	for _, e := range g.Edges() {
		from, ok1 := names[e.From]
		to, ok2 := names[e.To]
		if !ok1 || !ok2 {
			// Dangling edges are dropped rather than drawn to an invented node, the
			// same rule the emitter follows: a link to a page that does not exist is
			// worse than a missing link, because it reads as a fact.
			continue
		}
		arrow := "-->"
		if dashed(e) {
			arrow = "-.->"
		}
		bw.line("    %s %s|%s| %s", from, arrow, mermaidEdgeLabel(e), to)
	}
	return bw.err
}

// mermaidLabel is the node's title with the characters Mermaid treats as syntax
// removed. Quoting exists but is inconsistently supported across renderer
// versions, and a label that breaks the whole diagram is worse than one that
// loses a bracket.
func mermaidLabel(n *graph.Node) string {
	label := n.Title
	if label == "" {
		label = n.ID
	}
	label = strings.NewReplacer(
		"\"", "'", "[", "(", "]", ")", "{", "(", "}", ")",
		"|", "/", "<", "", ">", "", "\n", " ",
	).Replace(label)
	return strings.TrimSpace(label)
}

func mermaidEdgeLabel(e graph.Edge) string {
	label := string(e.Kind)
	if e.Weight > 1 {
		label += fmt.Sprintf(" ×%d", e.Weight)
	}
	if e.Conf == graph.Ambiguous {
		label += "?"
	}
	return label
}

// mermaidNames assigns each node a Mermaid-safe identifier.
//
// Concept paths contain slashes and Mermaid identifiers cannot, so they are
// mangled — and mangling can collide (`/modules/a-b` and `/modules/a/b` both
// become `modules_a_b`), which would silently merge two nodes into one box. So
// names are assigned over sorted IDs with a numeric suffix on collision, the same
// approach assemble takes to concept paths.
func mermaidNames(g *graph.Graph) map[string]string {
	out := make(map[string]string)
	used := make(map[string]bool)
	for _, n := range g.Nodes() {
		base := mangle(n.ID)
		name := base
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s_%d", base, i)
		}
		used[name] = true
		out[n.ID] = name
	}
	return out
}

func mangle(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return "n"
	}
	// A Mermaid identifier starting with a digit is parsed as a number.
	if s[0] >= '0' && s[0] <= '9' {
		return "n" + s
	}
	return s
}

func sortedIntKeys[V any](m map[int]V) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func hasKey[V any](m map[int]V, k int) bool {
	_, ok := m[k]
	return ok
}

// errWriter defers error handling to the end of a write, so each format reads as
// a sequence of lines rather than as a chain of error checks. Every format here
// writes to a buffer or a file, where a mid-stream failure means the whole output
// is already lost.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) line(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format+"\n", args...)
}
