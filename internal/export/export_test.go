package export

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/graph"
)

// sample builds a graph with one of everything the formats have to render: each
// node kind, an extracted edge and an inferred one, a weighted edge, an ambiguous
// edge, attributes, and a dangling edge that must not be drawn.
func sample(t *testing.T) *graph.Graph {
	t.Helper()
	g := graph.New()
	add := func(n *graph.Node) {
		if err := g.AddNode(n); err != nil {
			t.Fatalf("AddNode(%s): %v", n.ID, err)
		}
	}
	add(&graph.Node{
		ID: "/modules/auth", Kind: graph.KindModule, Title: "internal/auth",
		Description: "4 go files; 12 exported symbols.", Path: "internal/auth",
		Lang: "go", Tags: []string{"go", "security-boundary"},
		Attrs:   map[string]string{"package": "auth", "ports": "8080, 8443"},
		Files:   []string{"internal/auth/jwt.go", "internal/auth/pat.go"},
		Exports: []string{"Claims", "Token.Verify", "Validate"},
	})
	add(&graph.Node{ID: "/modules/storage", Kind: graph.KindModule, Title: "internal/storage", Lang: "go"})
	add(&graph.Node{ID: "/services/api", Kind: graph.KindService, Title: "api", Attrs: map[string]string{"image": "cgr.dev/chainguard/go:latest"}})
	add(&graph.Node{ID: "/interfaces/tokens-proto", Kind: graph.KindInterface, Title: "tokens (tokens.proto)"})
	add(&graph.Node{ID: "/data/tokens", Kind: graph.KindDataStore, Title: "tokens"})
	add(&graph.Node{ID: "/references/adr-0007", Kind: graph.KindDocument, Title: "ADR 0007: tokens are opaque"})
	add(&graph.Node{ID: "/references/go-golang-org-x-crypto", Kind: graph.KindExternal, Title: "golang.org/x/crypto"})

	g.AddEdge(graph.Edge{From: "/modules/auth", To: "/modules/storage", Kind: graph.EdgeImports, Conf: graph.Extracted, Source: "internal/auth/jwt.go"})
	g.AddEdge(graph.Edge{From: "/modules/auth", To: "/references/go-golang-org-x-crypto", Kind: graph.EdgeImports, Conf: graph.Extracted, Source: "internal/auth/jwt.go"})
	g.AddEdge(graph.Edge{From: "/modules/auth", To: "/interfaces/tokens-proto", Kind: graph.EdgeImplements, Conf: graph.Inferred})
	g.AddEdge(graph.Edge{From: "/modules/storage", To: "/data/tokens", Kind: graph.EdgeDefines, Conf: graph.Extracted})
	g.AddEdge(graph.Edge{From: "/modules/auth", To: "/modules/storage", Kind: graph.EdgeCoChanges, Conf: graph.Extracted, Weight: 14})
	g.AddEdge(graph.Edge{From: "/references/adr-0007", To: "/modules/auth", Kind: graph.EdgeDocuments, Conf: graph.Ambiguous})
	g.AddEdge(graph.Edge{From: "/services/api", To: "/modules/auth", Kind: graph.EdgeDeploys, Conf: graph.Extracted})
	// A dangling edge, which every format must drop rather than render: drawing an
	// arrow to a node nobody declared reads as a fact about the repository.
	g.AddEdge(graph.Edge{From: "/modules/auth", To: "/modules/gone", Kind: graph.EdgeImports, Conf: graph.Extracted})
	return g
}

func render(t *testing.T, g *graph.Graph, f Format) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, g, f); err != nil {
		t.Fatalf("Write(%s): %v", f, err)
	}
	if buf.Len() == 0 {
		t.Fatalf("Write(%s) produced nothing", f)
	}
	return buf.String()
}

// Byte-identical output for the same graph, in every format. The exports get
// committed and diffed, so instability here is commit churn — the same reason the
// graph package sorts everything before it returns it.
func TestEveryFormatIsDeterministic(t *testing.T) {
	for _, f := range Formats() {
		g := sample(t)
		first := render(t, g, f)
		for i := 0; i < 5; i++ {
			// A fresh graph each time, so map iteration order differs between runs
			// rather than being fixed by one graph's internal layout.
			if got := render(t, sample(t), f); got != first {
				t.Fatalf("%s: output differs between runs\nfirst:\n%s\ngot:\n%s", f, first, got)
			}
		}
	}
}

// The distinction between what was read and what was guessed has to survive
// rendering. A diagram that draws an inferred edge identically to an extracted one
// presents a model's guess as a fact.
func TestInferredEdgesAreDistinguishable(t *testing.T) {
	g := sample(t)

	mmd := render(t, g, FormatMermaid)
	if !strings.Contains(mmd, "-.->|implements|") {
		t.Error("mermaid: inferred edge is not dashed")
	}
	if strings.Contains(mmd, "-->|implements|") {
		t.Error("mermaid: inferred edge rendered with a solid arrow")
	}

	dot := render(t, g, FormatDOT)
	for _, line := range strings.Split(dot, "\n") {
		if strings.Contains(line, "label=\"implements\"") && !strings.Contains(line, "style=dashed") {
			t.Errorf("dot: inferred edge is not dashed: %s", line)
		}
		if strings.Contains(line, "label=\"imports\"") && strings.Contains(line, "style=dashed") {
			t.Errorf("dot: extracted edge rendered as dashed: %s", line)
		}
	}

	// The data formats carry it verbatim rather than as a style, which is what a
	// consumer filtering on trust needs.
	if !strings.Contains(render(t, g, FormatGraphML), `<data key="e_conf">inferred</data>`) {
		t.Error("graphml: confidence not carried")
	}
	if !strings.Contains(render(t, g, FormatJSON), `"confidence": "inferred"`) {
		t.Error("json: confidence not carried")
	}
}

// An edge whose endpoint does not exist is dropped by every format.
func TestDanglingEdgesAreNotRendered(t *testing.T) {
	g := sample(t)
	for _, f := range Formats() {
		out := render(t, g, f)
		if strings.Contains(out, "gone") {
			t.Errorf("%s: rendered an edge to a node that does not exist", f)
		}
	}
}

// Every node reaches the output. A format that silently dropped a node would
// under-report the repository, which is the failure mode this project treats as
// worse than being verbose.
func TestEveryNodeIsRendered(t *testing.T) {
	g := sample(t)
	for _, f := range Formats() {
		out := render(t, g, f)
		for _, n := range g.Nodes() {
			want := n.ID
			if f == FormatMermaid {
				// Mermaid identifiers are mangled, so the title is what appears.
				want = mermaidLabel(n)
			}
			if !strings.Contains(out, want) {
				t.Errorf("%s: node %s missing from output", f, n.ID)
			}
		}
	}
}

// Kind has to be recoverable from the output, because it is what tells a reader
// whether a box is code, a running service, or a document.
func TestNodeKindIsCarried(t *testing.T) {
	g := sample(t)

	mmd := render(t, g, FormatMermaid)
	for _, want := range []string{
		"([api])",                         // service: stadium
		"{{tokens (tokens.proto)}}",       // interface: hexagon
		"[(tokens)]",                      // data store: cylinder
		"[/ADR 0007: tokens are opaque/]", // document: parallelogram
		"(((golang.org/x/crypto)))",       // external: double circle
	} {
		if !strings.Contains(mmd, want) {
			t.Errorf("mermaid: missing shape %q", want)
		}
	}

	dot := render(t, g, FormatDOT)
	for _, want := range []string{"shape=component", "shape=hexagon", "shape=cylinder", "shape=note", "shape=doublecircle"} {
		if !strings.Contains(dot, want) {
			t.Errorf("dot: missing %q", want)
		}
	}

	if !strings.Contains(render(t, g, FormatGraphML), `<data key="n_kind">Service</data>`) {
		t.Error("graphml: node kind not carried")
	}
}

// Mermaid syntax characters in a title must not break the diagram. A label
// containing a bracket turns the rest of the file into a parse error, so the
// characters are stripped rather than quoted.
func TestMermaidLabelsAreSanitised(t *testing.T) {
	g := graph.New()
	if err := g.AddNode(&graph.Node{
		ID: "/modules/weird", Kind: graph.KindModule,
		Title: `pkg[a]{b} "c" |d| <e>`,
	}); err != nil {
		t.Fatal(err)
	}
	out := render(t, g, FormatMermaid)
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "pkg") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("node not rendered")
	}
	// One opening and one closing bracket: the shape's own. Anything else means a
	// title character survived as syntax.
	if n := strings.Count(line, "["); n != 1 {
		t.Errorf("label leaked %d extra '[' into syntax: %s", n-1, line)
	}
	for _, bad := range []string{`"`, "{", "}", "|", "<", ">"} {
		if strings.Contains(line, bad) {
			t.Errorf("label leaked %q: %s", bad, line)
		}
	}
}

// Two IDs that mangle to the same Mermaid identifier must stay two boxes.
// Silently merging them would be a wrong diagram, not an ugly one.
func TestMermaidIdentifierCollisionsAreDisambiguated(t *testing.T) {
	g := graph.New()
	for _, id := range []string{"/modules/a-b", "/modules/a/b", "/modules/a_b"} {
		if err := g.AddNode(&graph.Node{ID: id, Kind: graph.KindModule, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	names := mermaidNames(g)
	seen := map[string]string{}
	for id, name := range names {
		if prev, dup := seen[name]; dup {
			t.Errorf("%s and %s both mangled to %s", prev, id, name)
		}
		seen[name] = id
	}
	if len(seen) != 3 {
		t.Errorf("got %d identifiers for 3 nodes", len(seen))
	}
}

// Output has to parse as the format it claims to be.
func TestGraphMLIsWellFormedXML(t *testing.T) {
	out := render(t, sample(t), FormatGraphML)
	dec := xml.NewDecoder(strings.NewReader(out))
	for {
		_, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("graphml is not well-formed: %v", err)
		}
	}
	// Every key referenced by a <data> element must be declared, or a strict
	// consumer rejects the whole document.
	declared := map[string]bool{}
	for _, k := range graphMLKeys {
		declared[k.id] = true
	}
	for _, part := range strings.Split(out, `<data key="`)[1:] {
		key := part[:strings.Index(part, `"`)]
		if !declared[key] {
			t.Errorf("undeclared data key %q", key)
		}
	}
}

func TestJSONRoundTrips(t *testing.T) {
	g := sample(t)
	out := render(t, g, FormatJSON)
	var back jsonGraph
	if err := json.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("json does not parse: %v", err)
	}
	if len(back.Nodes) != len(g.Nodes()) {
		t.Errorf("nodes = %d, want %d", len(back.Nodes), len(g.Nodes()))
	}
	// One fewer than the graph holds: the dangling edge is dropped.
	if want := len(g.Edges()) - 1; len(back.Edges) != want {
		t.Errorf("edges = %d, want %d", len(back.Edges), want)
	}
	var auth *jsonNode
	for i := range back.Nodes {
		if back.Nodes[i].ID == "/modules/auth" {
			auth = &back.Nodes[i]
		}
	}
	if auth == nil {
		t.Fatal("/modules/auth missing")
	}
	if auth.Attrs["package"] != "auth" {
		t.Errorf("attrs lost: %v", auth.Attrs)
	}
	if len(auth.Files) != 2 {
		t.Errorf("files = %v, want 2", auth.Files)
	}
	if len(auth.Exports) != 3 {
		t.Errorf("exports = %v, want 3", auth.Exports)
	}
	// A node with no attributes has the key omitted rather than rendered empty.
	if strings.Contains(out, `"attrs": {}`) {
		t.Error("empty attrs map rendered instead of omitted")
	}
}

// The public surface is carried by name where a script can read it and by count
// where a layout tool can rank on it, and by neither where a diagram would only be
// made unreadable by it.
//
// The split is the assertion, not an implementation detail: an agent that reads the
// JSON to find out what a module offers gets the same names the module page shows it,
// which is the point of exporting them at all. GraphML gets `n_exports` as an int
// because its attributes are typed scalars a tool sizes and filters on — the names in
// that cell are a string nothing can compute over. Mermaid and DOT get nothing, for
// the same reason they already carry no file list: a box label is not a place to put
// 49 identifiers.
func TestExportsReachTheDataFormatsAndNotTheDiagrams(t *testing.T) {
	g := sample(t)

	js := render(t, g, FormatJSON)
	for _, want := range []string{`"exports"`, `"Claims"`, `"Token.Verify"`, `"Validate"`} {
		if !strings.Contains(js, want) {
			t.Errorf("json: %s missing", want)
		}
	}

	xmlOut := render(t, g, FormatGraphML)
	if !strings.Contains(xmlOut, `<data key="n_exports">3</data>`) {
		t.Error("graphml: export count missing")
	}
	// The count and not the names. A GraphML that carried both would have made
	// `n_exports` mean two different things depending on which node you read.
	if strings.Contains(xmlOut, "Token.Verify") {
		t.Error("graphml: carried export names, which its schema types as an int")
	}

	for _, f := range []Format{FormatMermaid, FormatDOT} {
		if out := render(t, g, f); strings.Contains(out, "Token.Verify") {
			t.Errorf("%s: an export name reached a diagram label", f)
		}
	}
}

// A module with no exports: JSON omits the key, GraphML writes the zero.
//
// The formats disagree and both are right for their consumer. In JSON a script tests
// `if (n.exports)`, so an empty array on every service, document, and external node
// would assert those have a measured public surface of nothing — and this graph is
// mostly nodes signpost never extracts symbols from, so the honest answer is silence.
// GraphML has already declared `n_exports` as an int for every node in the document;
// a column that is blank on some rows and numeric on others cannot be ranked or
// averaged, and 0 there is a fact a chart can plot. `n_files` and `n_cluster` resolve
// it the same way, `n_cluster` going as far as writing -1 for unassigned.
func TestNodesWithNoExportsSayNothingInJSONAndZeroInGraphML(t *testing.T) {
	g := graph.New()
	if err := g.AddNode(&graph.Node{ID: "/modules/bare", Kind: graph.KindModule, Title: "bare"}); err != nil {
		t.Fatal(err)
	}
	if out := render(t, g, FormatJSON); strings.Contains(out, `"exports"`) {
		t.Errorf("json: rendered an exports key for a node with none:\n%s", out)
	}
	if out := render(t, g, FormatGraphML); !strings.Contains(out, `<data key="n_exports">0</data>`) {
		t.Errorf("graphml: an int column has to hold a number on every row:\n%s", out)
	}
}

// A weighted edge's count has to reach the output: co-change weight is the whole
// signal of a co_changes edge, and an unweighted one says nothing.
func TestWeightIsCarried(t *testing.T) {
	g := sample(t)
	if !strings.Contains(render(t, g, FormatMermaid), "co_changes ×14") {
		t.Error("mermaid: weight missing")
	}
	if !strings.Contains(render(t, g, FormatDOT), `label="co_changes ×14"`) {
		t.Error("dot: weight missing")
	}
	if !strings.Contains(render(t, g, FormatGraphML), `<data key="e_weight">14</data>`) {
		t.Error("graphml: weight missing")
	}
	if !strings.Contains(render(t, g, FormatJSON), `"weight": 14`) {
		t.Error("json: weight missing")
	}
}

// Clusters become subgraphs, which is what makes a diagram of a real repository
// readable — the same grouping the bundle index uses for headings.
func TestClustersBecomeSubgraphs(t *testing.T) {
	g := sample(t)
	g.Clusters()
	mmd := render(t, g, FormatMermaid)
	if !strings.Contains(mmd, "subgraph cluster") {
		t.Error("mermaid: clusters not rendered as subgraphs")
	}
	if !strings.Contains(mmd, "\n    end") {
		t.Error("mermaid: subgraph not closed")
	}
	dot := render(t, g, FormatDOT)
	// The `cluster_` prefix is significant to Graphviz, not a convention: without
	// it the subgraph is not drawn as a box.
	if !strings.Contains(dot, "subgraph cluster_") {
		t.Error("dot: clusters not rendered as subgraphs")
	}
}

// An unclustered graph must not grow a spurious `cluster -1` box.
func TestUnclusteredGraphHasNoSubgraph(t *testing.T) {
	g := sample(t)
	if strings.Contains(render(t, g, FormatMermaid), "cluster -1") {
		t.Error("mermaid: rendered the unassigned cluster as a box")
	}
	if strings.Contains(render(t, g, FormatDOT), "cluster_-1") {
		t.Error("dot: rendered the unassigned cluster as a box")
	}
}

func TestEmptyGraphIsValidOutput(t *testing.T) {
	g := graph.New()
	for _, f := range Formats() {
		out := render(t, g, f)
		if strings.TrimSpace(out) == "" {
			t.Errorf("%s: empty graph produced no output", f)
		}
	}
	// Still parseable, because a repository signpost could not extract anything
	// from should produce an empty export rather than a broken one.
	if err := json.Unmarshal([]byte(render(t, g, FormatJSON)), &jsonGraph{}); err != nil {
		t.Errorf("json: empty graph does not parse: %v", err)
	}
}

func TestUnknownFormatIsAnError(t *testing.T) {
	if err := Write(&bytes.Buffer{}, graph.New(), Format("svg")); err == nil {
		t.Error("want an error for an unsupported format")
	}
}

// A non-ASCII title must survive rather than becoming escape sequences. Go's
// strconv.Quote would emit \u escapes that Graphviz does not interpret, and
// encoding/json's HTML escaping would mangle the data formats.
func TestNonASCIITitlesSurvive(t *testing.T) {
	g := graph.New()
	if err := g.AddNode(&graph.Node{
		ID: "/modules/x", Kind: graph.KindModule,
		Title: "модуль/データ", Description: "a & b > c",
	}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []Format{FormatDOT, FormatJSON} {
		if out := render(t, g, f); !strings.Contains(out, "модуль/データ") {
			t.Errorf("%s: non-ASCII title was escaped or dropped", f)
		}
	}
	if out := render(t, g, FormatJSON); !strings.Contains(out, "a & b > c") {
		t.Error("json: HTML-escaped a description")
	}
}
