package export

import (
	"encoding/xml"
	"io"
	"strings"

	"github.com/3rg0n/signpost/internal/graph"
)

// GraphML is the interchange format: Gephi, yEd, Cytoscape and networkx all read
// it, which is what makes it the right thing to emit when the graph is going to be
// analysed somewhere else rather than looked at.
//
// It is written by hand rather than through encoding/xml struct marshalling. The
// reason is the `<key>` declarations: GraphML requires every data attribute to be
// declared before use with an id, a domain, and a type, and the set of keys
// depends on what the graph actually contains. Expressing that through struct tags
// means building the same element tree in a less direct way.

// graphMLKey is one attribute declaration.
type graphMLKey struct {
	id     string
	domain string // "node" or "edge"
	name   string
	typ    string // "string" or "int"
}

// graphMLKeys are the attributes every export declares.
//
// Fixed rather than derived from the graph: a consumer diffing two exports, or a
// script written against one, should not see the schema change because a repo
// happened to have no services in it. Attrs are the exception and are flattened
// into a single string field, because their keys are open-ended (ports, image,
// table name) and declaring a GraphML key per attribute across an arbitrary
// repository would make the schema itself repo-dependent.
var graphMLKeys = []graphMLKey{
	{"n_kind", "node", "kind", "string"},
	{"n_title", "node", "title", "string"},
	{"n_desc", "node", "description", "string"},
	{"n_path", "node", "path", "string"},
	{"n_lang", "node", "lang", "string"},
	{"n_tags", "node", "tags", "string"},
	{"n_attrs", "node", "attrs", "string"},
	{"n_files", "node", "files", "int"},
	{"n_cluster", "node", "cluster", "int"},
	{"e_kind", "edge", "kind", "string"},
	{"e_conf", "edge", "confidence", "string"},
	{"e_weight", "edge", "weight", "int"},
	{"e_source", "edge", "source", "string"},
}

func writeGraphML(w io.Writer, g *graph.Graph) error {
	bw := &errWriter{w: w}
	bw.line(`<?xml version="1.0" encoding="UTF-8"?>`)
	bw.line(`<graphml xmlns="http://graphml.graphdrawing.org/xmlns"`)
	bw.line(`         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`)
	bw.line(`         xsi:schemaLocation="http://graphml.graphdrawing.org/xmlns`)
	bw.line(`         http://graphml.graphdrawing.org/xmlns/1.0/graphml.xsd">`)
	for _, k := range graphMLKeys {
		bw.line(`  <key id=%s for=%s attr.name=%s attr.type=%s/>`,
			xmlAttr(k.id), xmlAttr(k.domain), xmlAttr(k.name), xmlAttr(k.typ))
	}
	bw.line(`  <graph id="signpost" edgedefault="directed">`)

	for _, n := range g.Nodes() {
		bw.line(`    <node id=%s>`, xmlAttr(n.ID))
		data(bw, "n_kind", string(n.Kind))
		data(bw, "n_title", n.Title)
		data(bw, "n_desc", n.Description)
		data(bw, "n_path", n.Path)
		data(bw, "n_lang", n.Lang)
		data(bw, "n_tags", strings.Join(n.Tags, ","))
		data(bw, "n_attrs", flattenAttrs(n.Attrs))
		data(bw, "n_files", itoa(len(n.Files)))
		data(bw, "n_cluster", itoa(n.Cluster))
		bw.line(`    </node>`)
	}

	// Edge ids are positional over the sorted edge list. GraphML permits an edge
	// with no id, but Gephi and networkx both use it as the identity of a row, so
	// an absent id turns two parallel edges between the same pair into one.
	i := 0
	for _, e := range g.Edges() {
		if !g.Has(e.From) || !g.Has(e.To) {
			continue
		}
		bw.line(`    <edge id=%s source=%s target=%s>`, xmlAttr("e"+itoa(i)), xmlAttr(e.From), xmlAttr(e.To))
		data(bw, "e_kind", string(e.Kind))
		data(bw, "e_conf", string(e.Conf))
		data(bw, "e_weight", itoa(e.Weight))
		data(bw, "e_source", e.Source)
		bw.line(`    </edge>`)
		i++
	}

	bw.line(`  </graph>`)
	bw.line(`</graphml>`)
	return bw.err
}

// data writes one <data> element, omitting it when the value is empty — an empty
// element is not the same as an absent attribute to a consumer that distinguishes
// them, and it is noise to one that does not.
func data(bw *errWriter, key, val string) {
	if val == "" {
		return
	}
	bw.line(`      <data key=%s>%s</data>`, xmlAttr(key), xmlText(val))
}

// flattenAttrs renders Attrs as `k=v; k=v` in sorted key order. Semicolon-joined
// because a value routinely contains a comma (a port list), and losing the
// boundary between attributes would make the field unparseable.
func flattenAttrs(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attrs))
	for _, k := range sortedStringKeys(attrs) {
		parts = append(parts, k+"="+attrs[k])
	}
	return strings.Join(parts, "; ")
}

// xmlAttr returns a double-quoted, escaped XML attribute value.
func xmlAttr(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	b.WriteString(escapeXML(s))
	b.WriteByte('"')
	return b.String()
}

func xmlText(s string) string { return escapeXML(s) }

// escapeXML escapes through encoding/xml rather than a hand-written replacer, so
// the awkward cases — a stray carriage return, a control byte in a file path read
// off disk — are handled by the same code that would have to parse the result.
func escapeXML(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		// strings.Builder never fails a write, so this is unreachable; returning the
		// escaped-so-far text keeps the signature honest without a panic.
		return b.String()
	}
	return b.String()
}
