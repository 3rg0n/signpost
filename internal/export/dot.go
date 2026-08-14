package export

import (
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/3rg0n/signpost/internal/graph"
)

// Graphviz DOT is the format for a large graph: unlike Mermaid it scales past a
// few hundred nodes, and unlike GraphML it is readable as text and diffable.
//
// Clusters become `subgraph cluster_N`, which Graphviz draws as a box — the
// prefix is significant to Graphviz, not a naming convention.

// dotFill colours nodes by kind. Colours are named rather than hex so the output
// stays legible as text, and the palette is light enough that black label text
// stays readable on every fill.
var dotFill = map[graph.Kind]string{
	graph.KindModule:    "lightsteelblue",
	graph.KindService:   "palegreen",
	graph.KindInterface: "khaki",
	graph.KindDataStore: "lightpink",
	graph.KindDocument:  "lavender",
	graph.KindExternal:  "gainsboro",
	graph.KindSymbol:    "white",
	graph.KindPipeline:  "peachpuff",
}

var dotShape = map[graph.Kind]string{
	graph.KindModule:    "box",
	graph.KindService:   "component",
	graph.KindInterface: "hexagon",
	graph.KindDataStore: "cylinder",
	graph.KindDocument:  "note",
	graph.KindExternal:  "doublecircle",
	graph.KindSymbol:    "ellipse",
	// A pipeline is a sequence, and Graphviz's own name for the shape that reads as
	// one step in a flowchart is `cds`.
	graph.KindPipeline: "cds",
}

func writeDOT(w io.Writer, g *graph.Graph) error {
	bw := &errWriter{w: w}
	bw.line("digraph signpost {")
	bw.line("    rankdir=LR;")
	bw.line("    node [style=filled, fontname=\"Helvetica\"];")
	bw.line("    edge [fontname=\"Helvetica\", fontsize=10];")

	byCluster := map[int][]*graph.Node{}
	for _, n := range g.Nodes() {
		byCluster[n.Cluster] = append(byCluster[n.Cluster], n)
	}
	for _, cid := range sortedIntKeys(byCluster) {
		indent := "    "
		if cid >= 0 {
			bw.line("    subgraph cluster_%d {", cid)
			bw.line("        label=\"cluster %d\";", cid)
			bw.line("        style=dotted;")
			indent = "        "
		}
		for _, n := range byCluster[cid] {
			bw.line("%s%s [%s];", indent, dotQuote(n.ID), strings.Join(dotNodeAttrs(n), ", "))
		}
		if cid >= 0 {
			bw.line("    }")
		}
	}

	for _, e := range g.Edges() {
		if !g.Has(e.From) || !g.Has(e.To) {
			continue
		}
		bw.line("    %s -> %s [%s];", dotQuote(e.From), dotQuote(e.To), strings.Join(dotEdgeAttrs(e), ", "))
	}
	bw.line("}")
	return bw.err
}

func dotNodeAttrs(n *graph.Node) []string {
	label := n.Title
	if label == "" {
		label = n.ID
	}
	attrs := []string{
		"label=" + dotQuote(label),
		"shape=" + shapeOr(dotShape[n.Kind], "box"),
		"fillcolor=" + shapeOr(dotFill[n.Kind], "white"),
		"tooltip=" + dotQuote(tooltip(n)),
	}
	return attrs
}

// tooltip carries the description, which is where the actual signal is: a viewer
// hovering a box wants to know what the module does, and the label only has room
// for its name.
func tooltip(n *graph.Node) string {
	parts := []string{string(n.Kind)}
	if n.Description != "" {
		parts = append(parts, n.Description)
	}
	if len(n.Tags) > 0 {
		parts = append(parts, "tags: "+strings.Join(n.Tags, ", "))
	}
	return strings.Join(parts, " — ")
}

func dotEdgeAttrs(e graph.Edge) []string {
	attrs := []string{"label=" + dotQuote(mermaidEdgeLabel(e))}
	if dashed(e) {
		attrs = append(attrs, "style=dashed")
	}
	if e.Conf == graph.Ambiguous {
		attrs = append(attrs, "color=gray")
	}
	// Provenance is carried as an attribute rather than drawn: it belongs in the
	// file for anyone reading the DOT or scripting over it, and putting a file path
	// on every arrow would bury the diagram.
	if e.Source != "" {
		attrs = append(attrs, "tooltip="+dotQuote(e.Source))
	}
	return attrs
}

func shapeOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// dotQuote produces a DOT double-quoted ID. Go's strconv.Quote is close but
// escapes non-ASCII into \u sequences, which DOT does not interpret — a
// directory named in Cyrillic would render as literal escape text.
func dotQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString("\\n")
		case '\r', '\t':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// sortedStringKeys is the string counterpart of sortedIntKeys, used where an
// attribute map has to be emitted in a fixed order.
func sortedStringKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }
