// Package export renders the graph in formats other tools already read.
//
// The bundle (design §3) is the primary artifact and these are not a substitute
// for it: a Mermaid diagram cannot carry provenance, and a GraphML file is not
// something an agent reads before starting work. What they are for is the case
// where the graph needs to leave signpost — pasted into a PR description, opened
// in Gephi, diffed in CI, or handed to a script that does not want to parse
// markdown.
//
// Two properties carry over from the graph package and are tested here:
//
//   - **Deterministic.** Byte-identical output for the same graph. Exports get
//     committed and diffed like anything else, and a format that reordered its
//     own nodes between runs would be useless for that.
//   - **Confidence survives the round trip.** An edge a model inferred renders
//     differently from one read out of source — dashed in the diagram formats, an
//     attribute in the data formats. A rendered graph that flattens the two makes
//     a guess look like a fact, which is the failure this project is built to
//     avoid.
package export

import (
	"fmt"
	"io"

	"github.com/3rg0n/signpost/internal/graph"
)

// Format names an output format.
type Format string

const (
	FormatMermaid Format = "mermaid"
	FormatDOT     Format = "dot"
	FormatGraphML Format = "graphml"
	FormatJSON    Format = "json"
)

// Formats lists every supported format, in the order a help message shows them.
func Formats() []Format {
	return []Format{FormatMermaid, FormatDOT, FormatGraphML, FormatJSON}
}

// Write renders g to w in the named format.
func Write(w io.Writer, g *graph.Graph, f Format) error {
	switch f {
	case FormatMermaid:
		return writeMermaid(w, g)
	case FormatDOT:
		return writeDOT(w, g)
	case FormatGraphML:
		return writeGraphML(w, g)
	case FormatJSON:
		return writeJSON(w, g)
	}
	return fmt.Errorf("export: unknown format %q", f)
}

// dashed reports whether an edge was not read directly out of the repository.
//
// Every diagram format below draws these differently. The distinction is the one
// thing a picture of a graph must not lose: "storage imports auth" read from an
// import statement and the same edge proposed by a model are different claims,
// and a reviewer looking at a diagram has no other way to tell them apart.
func dashed(e graph.Edge) bool { return e.Conf != graph.Extracted }
