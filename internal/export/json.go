package export

import (
	"encoding/json"
	"io"

	"github.com/3rg0n/signpost/internal/graph"
)

// JSON is the format for a script: jq over the node list, a CI check that counts
// cycles, a diff between two commits' graphs.
//
// The shape is declared here rather than by marshalling graph.Node directly, for
// the reason any wire format is separated from an in-memory one: the graph structs
// are free to change as extraction grows, and anything written against this output
// should not break when they do. It also lets the field names be the ones a
// consumer would guess.

type jsonGraph struct {
	Nodes []jsonNode `json:"nodes"`
	Edges []jsonEdge `json:"edges"`
}

type jsonNode struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Path        string            `json:"path,omitempty"`
	Lang        string            `json:"lang,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Attrs       map[string]string `json:"attrs,omitempty"`
	Files       []string          `json:"files,omitempty"`
	Cluster     int               `json:"cluster"`
}

type jsonEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Conf string `json:"confidence"`
	// Weight is omitted at zero, which the graph defines as "not counted" — a
	// literal 0 would read as a measured absence of co-change rather than as an
	// edge kind that carries no count.
	Weight int    `json:"weight,omitempty"`
	Source string `json:"source,omitempty"`
}

func writeJSON(w io.Writer, g *graph.Graph) error {
	out := jsonGraph{
		Nodes: make([]jsonNode, 0, len(g.Nodes())),
		Edges: make([]jsonEdge, 0),
	}
	// g.Nodes() and g.Edges() are both sorted, and Go marshals map keys in sorted
	// order, so the whole document is byte-stable without any sorting here.
	for _, n := range g.Nodes() {
		out.Nodes = append(out.Nodes, jsonNode{
			ID: n.ID, Kind: string(n.Kind), Title: n.Title,
			Description: n.Description, Path: n.Path, Lang: n.Lang,
			Tags: n.Tags, Attrs: nilIfEmpty(n.Attrs), Files: n.Files,
			Cluster: n.Cluster,
		})
	}
	for _, e := range g.Edges() {
		if !g.Has(e.From) || !g.Has(e.To) {
			continue
		}
		out.Edges = append(out.Edges, jsonEdge{
			From: e.From, To: e.To, Kind: string(e.Kind),
			Conf: string(e.Conf), Weight: e.Weight, Source: e.Source,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// HTML escaping would turn a `>` in a description or a `&` in a path into a
	// > / & escape. The output is a file read by tools, not embedded in a
	// page, and the escapes make it unreadable and needlessly diff-noisy.
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

// nilIfEmpty drops an allocated-but-empty Attrs map so `omitempty` applies. Nodes
// always carry a non-nil Attrs after AddNode, so without this every node would
// render `"attrs": {}`.
func nilIfEmpty(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
