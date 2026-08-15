package okf

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

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
	// regionRole holds the semantic pass's prose, and it is a *separate* region from
	// summary rather than a replacement for its placeholder. That is the one design
	// decision in this file that is not obvious, and it follows from Merge:
	//
	// A managed region present on disk but absent from a render is kept verbatim
	// (page.go), which is the same mechanism logPage relies on to accumulate history. So a
	// deterministic build — every push, per §8 — renders no `role`, finds the one the
	// scheduled semantic run wrote, and carries it through untouched. Writing model prose
	// into `summary` instead would put it in a region every deterministic build *does*
	// render, and the next push would overwrite it with the placeholder.
	//
	// It also keeps the two kinds of claim visually apart on the page, which §4.1's trust
	// grading asks for: `summary` is counted facts, `role` is a guess with a citation
	// line, and a reader can tell which is which without knowing how signpost works.
	regionRole = "role"
	// regionPractices holds the practices page's findings. One region for the whole body,
	// not one per topic: the sections are rendered together by internal/practice and a
	// reader's notes belong under Notes, so per-topic regions would be structure serving
	// nothing.
	regionPractices = "practices"
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

	// Roles is the semantic pass's prose, keyed by node ID, already grounded and rendered
	// by internal/semantic. Nil on every deterministic run, which is what keeps `build`
	// byte-identical with no backend configured.
	//
	// A map of finished strings rather than anything the emitter composes. This package
	// writes only what it can count (see this file's header), so the one honest way for it
	// to carry a model's claim is to be handed the text and told which page it belongs on —
	// keeping the emitter unable to invent prose even by accident, and keeping it free of
	// any dependency on the model path.
	Roles map[string]string

	// Practices is the rendered body of the practices page — what the repository declares
	// about how it is built, tested, gated, and owned, and what it does not (design §9.1).
	// Empty means the page is not written at all.
	//
	// A finished string for the same reason as Roles, and it is worth being explicit about
	// why the emitter does not compute this itself: the findings come from manifest facts
	// and the file walk, neither of which is in the graph, so computing them here would
	// give this package a dependency on the extraction packages and put a second kind of
	// claim inside the emitter. internal/practice makes the claims and owns their wording;
	// this package places the text and escapes it.
	Practices string
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
	}
	// Emitted only when there is prose to put in it. An empty `role` region on every page
	// of every deterministic bundle would be structure with nothing in it, and — because
	// Merge keeps a region it finds on disk — it would also mean a semantic run's output
	// landing in a region the next build renders empty, which is the overwrite regionRole
	// exists to avoid.
	//
	// The heading goes *inside* the managed region, unlike every other section on this
	// page. Merge takes human regions only from what is on disk and appends a new managed
	// region at the end, so a heading emitted as its own human region would be dropped on
	// the first semantic run over an existing bundle and the prose would land under
	// "Notes" with nothing naming it. Self-contained, the region reads correctly wherever
	// Merge puts it.
	if role := opts.Roles[n.ID]; role != "" {
		body = append(body, managedRegion(regionRole, "\n"+heading(2, "Role")+role))
	}
	body = append(body,
		humanRegion("\n"+heading(2, "Structure")+""),
		managedRegion(regionStructure, structureText(g, n)),
		humanRegion("\n"+heading(2, "Notes")+notesInvitation()))
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
	from := pagePath(id)
	edges := g.EdgesFrom(id)
	out := make([]yamlValue, 0, len(edges))
	for _, e := range edges {
		pairs := []yamlPair{
			{"kind", scalar(string(e.Kind))},
			{"to", scalar(relTarget(from, pagePath(e.To)))},
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

// relTarget renders a link from one bundle page to another as a page-relative path.
//
// Every rendered link in the bundle goes through this, and the reason is the one place
// where "correct in a viewer" and "correct for a reader" came apart. A bundle-absolute
// `/modules/hook.md` resolves against the *server root*, so it only works in something
// that mounts the bundle at `/`. On GitHub — which ADR 0005 names as the whole point of
// committing the bundle, a reader opening `.signpost/index.md` with nothing installed —
// it points at `github.com/modules/hook.md` and 404s. Page-relative works in both, and
// in a plain checkout opened in an editor, which the absolute form never did.
//
// Relative also survives being moved: a fork, a bundle under a different directory, or a
// subtree merge that nests the whole tree one level down all keep working, because no
// link names a root that a relocation can change.
//
// Both arguments are bundle-relative page paths, with or without the leading slash that
// pagePath carries. The `./` prefix on a sibling is deliberate: it makes the target
// unmistakably a relative path in the markdown source, where a bare `hook.md` reads like
// it could be anything.
func relTarget(from, to string) string {
	fromSegs := strings.Split(strings.TrimPrefix(from, "/"), "/")
	fromSegs = fromSegs[:len(fromSegs)-1] // the directory the link is written in
	toSegs := strings.Split(strings.TrimPrefix(to, "/"), "/")

	// The shared prefix is the directories both pages sit under. Stopping one short of
	// toSegs's end keeps the target's own filename out of the comparison, so a page named
	// the same as a directory cannot consume it.
	i := 0
	for i < len(fromSegs) && i < len(toSegs)-1 && fromSegs[i] == toSegs[i] {
		i++
	}
	out := make([]string, 0, len(fromSegs)-i+len(toSegs)-i)
	for j := i; j < len(fromSegs); j++ {
		out = append(out, "..")
	}
	out = append(out, toSegs[i:]...)
	rel := strings.Join(out, "/")
	if !strings.HasPrefix(rel, "../") {
		rel = "./" + rel
	}
	return rel
}

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
	from := pagePath(n.ID)
	var b strings.Builder
	if len(n.Files) > 0 {
		b.WriteString(filesLine(n))
	}
	if len(n.Exports) > 0 {
		b.WriteString(exportsLine(n))
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
		b.WriteString(edgeSentence(g, from, es))
		b.WriteString("\n")
	}
	b.WriteString(accessText(g, n, from))
	if b.Len() == 0 {
		return "Nothing links to or from this page. It may be dead code, an unreferenced " +
			"document, or a gap in extraction — the coverage report says which.\n"
	}
	return b.String()
}

// accessText names the modules that write and read a table, on the table's own page.
//
// The one place a page renders its *incoming* edges, and the exception edgeList's comment
// does not cover. Everywhere else, "what does this depend on" is the question asked from the
// page it is asked about: a module's page lists what the module imports, and the importers
// are reachable from the index. A table is the other way round. It depends on nothing — a
// data node has no outgoing edges at all — and the question a reader arrives with is
// entirely about the other end: something wrote a duplicate row, and which code can write
// this table is the answer. Without this the page a data symptom sends a reader to would
// show its migration history and nothing about the code, which is the gap ADR 0034 exists
// to close.
//
// Limited to writes and reads on a data page, so the asymmetry stays a property of this one
// kind of page rather than a general "and also everything pointing here", which is what
// would double the bundle's size in the edge count.
func accessText(g *graph.Graph, n *graph.Node, from string) string {
	if n.Kind != graph.KindDataStore {
		return ""
	}
	var writes, reads []graph.Edge
	for _, e := range g.EdgesTo(n.ID) {
		switch e.Kind {
		case graph.EdgeWrites:
			writes = append(writes, e)
		case graph.EdgeReads:
			reads = append(reads, e)
		}
	}
	var b strings.Builder
	// Writes first, and both lines labelled from the module's side rather than the table's:
	// the reader is looking for code, and "Written by" says which end of the link they are
	// about to follow.
	if len(writes) > 0 {
		b.WriteString("\n- **Written by**: " + incomingSentence(g, from, writes) + "\n")
	}
	if len(reads) > 0 {
		b.WriteString("\n- **Read by**: " + incomingSentence(g, from, reads) + "\n")
	}
	return b.String()
}

// incomingSentence is edgeSentence for edges pointing at this page: same rendering, linking
// From rather than To.
func incomingSentence(g *graph.Graph, from string, es []graph.Edge) string {
	flipped := make([]graph.Edge, 0, len(es))
	for _, e := range es {
		e.To = e.From
		flipped = append(flipped, e)
	}
	return edgeSentence(g, from, flipped)
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
		b.WriteString("- " + codeSpan(f) + "\n")
	}
	if len(shown) < len(n.Files) {
		b.WriteString("- and " + strconv.Itoa(len(n.Files)-len(shown)) + " more\n")
	}
	return b.String()
}

// exportsLine names the module's public surface.
//
// Named rather than only counted, for the reason filesLine gives: "4 exported symbols"
// tells an agent a module has a surface, and the names tell it whether the thing it is
// looking for is in there. This is the one place a page states what a module offers
// rather than what it depends on.
//
// Only exported declarations, which is the negative half of the claim: a page that
// listed private helpers would describe a surface callers cannot use, and an agent
// reading it would write code against a name the compiler rejects. Symbols get no node
// and no edge — ADR 0003 keeps the graph at directory granularity, so this is a page
// attribute, not a second graph.
//
// Comma-separated on one line rather than a bullet each: a module with 60 exports would
// otherwise push its edges — the part an agent navigates by — off the first screen.
func exportsLine(n *graph.Node) string {
	// Higher than filesLine's bound because names are short and a public surface is
	// nearly always smaller than it looks; a module past this is a god object, and the
	// bound says so by truncating.
	const maxExports = 60
	shown := n.Exports
	var b strings.Builder
	b.WriteString("\n- **Exports** (" + strconv.Itoa(len(n.Exports)) + "): ")
	if len(shown) > maxExports {
		shown = shown[:maxExports]
	}
	for i, s := range shown {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(codeSpan(s))
	}
	if len(shown) < len(n.Exports) {
		b.WriteString(", and " + strconv.Itoa(len(n.Exports)-len(shown)) + " more")
	}
	b.WriteString("\n")
	return b.String()
}

// codeSpan renders repository content — a path, a name — as a markdown code span.
//
// escapeMarkers in page.go already makes such a string unable to close the region it lands
// in, which is the security property. This is the other half: a path may contain a newline
// or a backtick, and either one breaks the span it is written into, so the list item that
// was meant to name one file silently becomes two lines of something else. Quoted in that
// case, using Go's own escaping, so the path stays on one line and stays readable — and a
// filename with a newline in it is a fact worth showing plainly rather than rendering as
// though it were two files.
//
// The common path — every ordinary filename — is untouched, so this does not change the
// bytes of any bundle built from a tree without one.
func codeSpan(s string) string {
	if strings.ContainsAny(s, "`\n\r") || strings.ContainsFunc(s, unicode.IsControl) {
		// strconv.Quote escapes the control characters but not a backtick, which is legal in
		// a Go literal and would still end the span it is being put inside. `\x60` is the
		// same character spelled as an escape Quote itself could have emitted, so the result
		// is still a valid Go string literal a reader can paste somewhere and get the path.
		return "`" + strings.ReplaceAll(strconv.Quote(s), "`", `\x60`) + "`"
	}
	return "`" + s + "`"
}

// proseLink renders a bundle link whose label is repository content.
//
// The label is a node title, which comes from a directory or file name, and markdown link
// syntax is only positional — so a directory named `x](../../etc/passwd)(` closes the label
// early and the text that follows becomes the target, with the real target trailing behind
// as inert prose. Every link on the page then points wherever that directory name said, and
// `verify` passes: the link it was asked to check is well-formed and resolves, because the
// forged one is a different link.
//
// `]`, `[`, `(` and `)` are escaped with a backslash, which is markdown's own mechanism and
// renders as the bare character — so a title containing a bracket still reads correctly.
// Only the label needs this; the target is a node ID, built by assemble rather than read
// from the tree.
func proseLink(label, target string) string {
	return "[" + escapeLinkLabel(label) + "](" + target + ")"
}

func escapeLinkLabel(s string) string {
	if !strings.ContainsAny(s, "[]()") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		if strings.ContainsRune("[]()", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// edgeSentence renders one edge kind's targets as prose links with their confidence.
//
// from is the page these links are written on, because the targets are page-relative
// (relTarget) and so cannot be rendered without knowing where they are rendered.
func edgeSentence(g *graph.Graph, from string, es []graph.Edge) string {
	parts := make([]string, 0, len(es))
	for _, e := range es {
		target := g.Node(e.To)
		label := e.To
		if target != nil && target.Title != "" {
			label = target.Title
		}
		s := proseLink(label, relTarget(from, pagePath(e.To)))
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
	case graph.EdgePrecedes:
		return "Runs before"
	case graph.EdgeWrites:
		return "Writes"
	case graph.EdgeReads:
		return "Reads"
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
