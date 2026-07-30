package okf

import (
	"sort"
	"strconv"
	"strings"

	"github.com/3rg0n/signpost/internal/graph"
)

// Page generation: turning a graph node into the OKF page design §3.1 specifies.
//
// The output of this file is what the *first* run of signpost would write. Merging it with
// what is already on disk — preserving human regions and carrying `verified:` across — is
// page.go's job, and keeping the two separate is what makes both testable: generation is a
// pure function of the graph, and merging is a pure function of two pages.
//
// Nothing here composes prose. Every description is an inventory assembled from counted
// facts, for the reason assemble/describe.go states at more length: a deterministic pass
// can honestly say "12 files, 47 exported symbols" and cannot honestly say what a module
// is *for*. The managed region a model would fill is emitted with a placeholder line
// instead, so the page's shape is the same either way and the semantic pass has somewhere
// to write.

// Region names. Constants because the close-marker match is textual and a typo would
// produce a page whose region never matches, which is the one failure mode that looks like
// working code.
const (
	regionSummary   = "summary"
	regionStructure = "structure"
	regionIndex     = "index"
)

// Actor is the OKF `generated.by` string.
type Actor string

// Options configure a bundle emit.
type Options struct {
	// Actor is stamped into every page's `generated.by`.
	Actor Actor
	// Resource is the base resource URI the pages describe, e.g.
	// "git://example.com/repo@8f2a1c9". Each page appends its own path. Empty when the
	// commit is unknown, in which case pages carry no resource — an absent provenance
	// stamp being better than a wrong one.
	Resource string
	// Date is the `generated.at` value, YYYY-MM-DD. Taken from the commit rather than the
	// clock, per vcs.Commit.Date, so a re-run at the same commit produces the same bytes.
	Date string
	// AsOfBundle makes Verify compare content while taking provenance from the bundle's own
	// record instead of from this tree. Ignored everywhere else: a build always writes the
	// commit it actually describes.
	//
	// Required by a consequence of §8.0 that is not optional. The bundle is built on the
	// default branch only, so on a branch or a pull request its stamp names an older commit
	// *by construction* — and the stamp is part of every page's bytes, so a strict verify
	// reports every page as out of date on every pull request, including one that changed no
	// code at all. It is also the only way the single-developer pattern can work: building
	// locally and committing the bundle alongside the code stamps the parent commit, because
	// the sha of the commit carrying the stamp does not exist until after it is written.
	//
	// This does not weaken the staleness check. Content is still compared byte for byte
	// against a fresh render, so a change to the code still fails; only the two provenance
	// fields are taken from the bundle. Nor does it rest on trusting the manifest: the
	// manifest can only reach the tree through a commit, which makes a hand-edited stamp a
	// reviewable diff in a machine-generated file, and forging one cannot hide stale content
	// because the content comparison runs either way.
	//
	// The adoption is announced in Skipped rather than applied silently. This is the check
	// whose quiet success would destroy the tool's value, so a run that relaxes it says which
	// commit it judged against.
	AsOfBundle bool
}

// pageFor renders one node as the page a first run would write.
func pageFor(g *graph.Graph, n *graph.Node, opts Options) *Page {
	fm := &yamlDoc{}
	fm.setScalar("type", string(n.Kind))
	fm.setScalar("title", n.Title)
	fm.setScalar("description", n.Description)
	if res := resourceFor(opts.Resource, n.Path); res != "" {
		fm.setScalar("resource", res)
	}
	fm.setStrings("tags", sortedStrings(n.Tags))
	if len(opts.Actor) > 0 && opts.Date != "" {
		fm.set("generated", flowMap(
			yamlPair{"by", scalar(string(opts.Actor))},
			yamlPair{"at", scalar(opts.Date)},
		))
	}
	if attrs := attributeMap(n.Attrs); len(attrs) > 0 {
		fm.set("attributes", seq(attrs...))
	}
	if edges := edgeList(g, n.ID); len(edges) > 0 {
		fm.set("edges", seq(edges...))
	}

	body := []Region{
		humanRegion(heading(1, n.Title)),
		managedRegion(regionSummary, summaryText(n)),
		humanRegion("\n" + heading(2, "Structure") + ""),
		managedRegion(regionStructure, structureText(g, n)),
		humanRegion("\n" + heading(2, "Notes") + notesInvitation()),
	}
	return NewPage(fm.String(), body...)
}

// resourceFor builds a page's resource URI. Empty base yields empty, because a resource
// naming no commit tells a reader nothing and would fail verify's sha check.
func resourceFor(base, p string) string {
	if base == "" {
		return ""
	}
	if p == "" {
		return base
	}
	return base + "/" + p
}

// attributeMap renders a node's Attrs as a block sequence of flow mappings.
//
// A sequence of `{ name: x, value: y }` rather than a nested mapping, because the emitter's
// subset has exactly one nesting level (see yaml.go) and a nested mapping would need two.
// The shape is also what OKF §11's tolerate-unknown-keys rule makes safe to extend: a
// consumer that does not know `attributes` skips it, and one that does gets a list it can
// iterate without knowing the key names in advance.
func attributeMap(attrs map[string]string) []yamlValue {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]yamlValue, 0, len(keys))
	for _, k := range keys {
		if attrs[k] == "" {
			continue
		}
		out = append(out, flowMap(
			yamlPair{"name", scalar(k)},
			yamlPair{"value", scalar(attrs[k])},
		))
	}
	return out
}

// edgeList renders a node's outgoing edges.
//
// Outgoing only. An edge appears on exactly one page, which is what keeps the bundle's
// total size linear in the edge count rather than doubling it, and the direction is the one
// a reader follows: "what does this depend on" is the question asked from a module's page.
// Incoming edges are reachable by the index, and co-change edges are emitted in both
// directions by assemble already, so a symmetric coupling appears on both its pages
// without this file doing anything.
//
// `confidence` is on every entry without exception, per ADR 0004. A renderer that dropped
// it would make an inference indistinguishable from a parsed import, which is the failure
// this project exists to avoid.
func edgeList(g *graph.Graph, id string) []yamlValue {
	edges := g.EdgesFrom(id)
	out := make([]yamlValue, 0, len(edges))
	for _, e := range edges {
		pairs := []yamlPair{
			{"kind", scalar(string(e.Kind))},
			{"to", scalar(pagePath(e.To))},
			{"confidence", scalar(string(e.Conf))},
		}
		if e.Weight > 0 {
			pairs = append(pairs, yamlPair{"weight", number(e.Weight)})
		}
		if e.Source != "" {
			pairs = append(pairs, yamlPair{"source", scalar(e.Source)})
		}
		out = append(out, flowMap(pairs...))
	}
	return out
}

// pagePath turns a node ID into the bundle-relative page it names.
//
// The ID *is* the path minus the suffix, by construction in assemble/id.go, so this is
// only ever appending ".md". Written as a function anyway because it is the one place the
// two conventions meet, and a change to either should have exactly one place to fail.
func pagePath(id string) string { return id + ".md" }

// summaryText is the placeholder for the managed region a model fills.
//
// It states what the region is rather than describing the module, because inventing a
// description here would produce text indistinguishable from a grounded one — and the
// generated/verified trust grading exists precisely to keep those apart. A reader seeing
// this line knows the semantic pass has not run; a reader seeing invented prose would not.
func summaryText(n *graph.Node) string {
	if n.Description != "" {
		return n.Description + "\n"
	}
	return "No summary yet. Run signpost with a model backend, or write one here — text " +
		"outside the managed markers is never overwritten.\n"
}

// structureText renders the counted facts as prose links.
//
// Prose links in addition to the frontmatter `edges` list, per §3.1: OKF links are untyped
// by design, so a generic OKF reader traverses the bundle through these while a signpost-
// aware one reads the typed list. Emitting only the frontmatter would leave a conformant
// consumer unable to walk the graph at all.
func structureText(g *graph.Graph, n *graph.Node) string {
	var b strings.Builder
	if len(n.Files) > 0 {
		b.WriteString(filesLine(n))
	}
	byKind := map[graph.EdgeKind][]graph.Edge{}
	for _, e := range g.EdgesFrom(n.ID) {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		es := byKind[graph.EdgeKind(k)]
		b.WriteString("\n- **" + edgeKindLabel(graph.EdgeKind(k)) + "**: ")
		b.WriteString(edgeSentence(g, es))
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "Nothing links to or from this page. It may be dead code, an unreferenced " +
			"document, or a gap in extraction — the coverage report says which.\n"
	}
	return b.String()
}

func filesLine(n *graph.Node) string {
	// Listed rather than counted, up to a bound: the file list is the single most useful
	// thing on a module page for an agent deciding what to open, and a count tells it
	// nothing it can act on. Bounded because a directory with 400 files would produce a
	// page nobody reads, and the bound is stated so a truncated list never reads as a
	// complete one.
	const maxFiles = 40
	var b strings.Builder
	b.WriteString(strconv.Itoa(len(n.Files)))
	if len(n.Files) == 1 {
		b.WriteString(" file:\n")
	} else {
		b.WriteString(" files:\n")
	}
	shown := n.Files
	if len(shown) > maxFiles {
		shown = shown[:maxFiles]
	}
	for _, f := range shown {
		b.WriteString("- `" + f + "`\n")
	}
	if len(shown) < len(n.Files) {
		b.WriteString("- and " + strconv.Itoa(len(n.Files)-len(shown)) + " more\n")
	}
	return b.String()
}

// edgeSentence renders one edge kind's targets as prose links with their confidence.
func edgeSentence(g *graph.Graph, es []graph.Edge) string {
	parts := make([]string, 0, len(es))
	for _, e := range es {
		target := g.Node(e.To)
		label := e.To
		if target != nil && target.Title != "" {
			label = target.Title
		}
		s := "[" + label + "](" + pagePath(e.To) + ")"
		// The confidence marker is on the link itself rather than in a legend, because a
		// reader scanning one line must be able to tell a parsed fact from a guess without
		// scrolling. `extracted` is the silent default: annotating the common case would
		// make the uncommon one harder to see.
		if e.Conf != graph.Extracted {
			s += " (" + string(e.Conf) + ")"
		}
		if e.Weight > 0 {
			s += " ×" + strconv.Itoa(e.Weight)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// edgeKindLabel renders an edge kind for a human.
func edgeKindLabel(k graph.EdgeKind) string {
	switch k {
	case graph.EdgeImports:
		return "Imports"
	case graph.EdgeCalls:
		return "Calls"
	case graph.EdgeImplements:
		return "Implements"
	case graph.EdgeDefines:
		return "Defines"
	case graph.EdgeConfigures:
		return "Configures"
	case graph.EdgeDeploys:
		return "Deploys"
	case graph.EdgeTestedBy:
		return "Tested by"
	case graph.EdgeDocuments:
		return "Documents"
	case graph.EdgeCoChanges:
		return "Changes with"
	case graph.EdgeOwns:
		return "Owned by"
	}
	// An edge kind added to graph without a label here renders as its raw value rather
	// than as nothing, so a missing case is a cosmetic gap in one line of one page and not
	// a silently omitted relationship.
	return string(k)
}

// notesInvitation seeds the human section.
//
// The invitation is load-bearing rather than decorative. §6.1's mechanism only compounds
// if people actually write here, and an empty heading under a generated page reads as
// something the tool will overwrite. Saying so explicitly is what makes the first
// correction happen.
func notesInvitation() string {
	return "_Anything written here is yours. signpost rewrites only the regions between " +
		"its managed markers, and never this section._\n"
}
