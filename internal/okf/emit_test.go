package okf

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/graph"
)

// demoOptions are the options every test in this package emits with, so a change to the
// provenance stamp shows up in one place.
func demoOptions() Options {
	return Options{
		Actor:    "signpost/0.1.0",
		Resource: "git://example.com/repo@8f2a1c9",
		Date:     "2026-07-30",
	}
}

// demoGraph builds a small graph exercising each shape a page can carry: a module with
// files, attributes, and tags; an outgoing edge of each confidence; a weighted co-change; an
// external dependency; and an orphan.
func demoGraph(t *testing.T) (*graph.Graph, *graph.Node) {
	t.Helper()
	g := graph.New()
	add := func(n *graph.Node) *graph.Node {
		if err := g.AddNode(n); err != nil {
			t.Fatalf("AddNode(%s): %v", n.ID, err)
		}
		return g.Node(n.ID)
	}
	auth := add(&graph.Node{
		ID:          "/modules/internal-auth",
		Kind:        graph.KindModule,
		Title:       "internal/auth",
		Description: "JWT validation and PAT issuance.",
		Path:        "internal/auth",
		Lang:        "go",
		Tags:        []string{"go", "security-boundary"},
		Attrs:       map[string]string{"package": "auth", "exported_symbols": "12"},
		Files:       []string{"internal/auth/auth.go", "internal/auth/jwt.go"},
	})
	add(&graph.Node{
		ID: "/modules/internal-storage", Kind: graph.KindModule, Title: "internal/storage",
		Description: "Token table.", Path: "internal/storage",
	})
	add(&graph.Node{
		ID: "/modules/api-gateway", Kind: graph.KindModule, Title: "api/gateway",
		Path: "api/gateway",
	})
	add(&graph.Node{
		ID: "/interfaces/things-jwt", Kind: graph.KindInterface, Title: "things-jwt",
	})
	add(&graph.Node{
		ID: "/references/golang-org-x-crypto", Kind: graph.KindExternal,
		Title: "golang.org/x/crypto",
	})
	add(&graph.Node{ID: "/modules/orphan", Kind: graph.KindModule, Title: "orphan"})

	g.AddEdge(graph.Edge{
		From: auth.ID, To: "/modules/internal-storage", Kind: graph.EdgeImports,
		Conf: graph.Extracted, Source: "internal/auth/auth.go",
	})
	g.AddEdge(graph.Edge{
		From: auth.ID, To: "/interfaces/things-jwt", Kind: graph.EdgeImplements,
		Conf: graph.Inferred,
	})
	g.AddEdge(graph.Edge{
		From: auth.ID, To: "/modules/api-gateway", Kind: graph.EdgeCoChanges,
		Conf: graph.Extracted, Weight: 14,
	})
	g.AddEdge(graph.Edge{
		From: auth.ID, To: "/references/golang-org-x-crypto", Kind: graph.EdgeImports,
		Conf: graph.Ambiguous,
	})
	// Incoming, so the outgoing-only rule can be asserted.
	g.AddEdge(graph.Edge{
		From: "/modules/api-gateway", To: auth.ID, Kind: graph.EdgeImports,
		Conf: graph.Extracted,
	})
	return g, auth
}

func TestPageForFrontmatterShape(t *testing.T) {
	g, n := demoGraph(t)
	p := pageFor(g, n, demoOptions())
	fm := p.Frontmatter

	// §3.1's order, asserted as a whole rather than key by key: the order is the readable
	// part, and a test per key would pass while the page became unreadable.
	wantPrefix := "type: Module\n" +
		"title: internal/auth\n" +
		"description: JWT validation and PAT issuance.\n" +
		"resource: git://example.com/repo@8f2a1c9/internal/auth\n" +
		"tags: [go, security-boundary]\n" +
		// The date is quoted, and must stay that way: unquoted, YAML 1.1 resolves
		// `2026-07-30` to a timestamp, so a reader would hand back a date value where every
		// other consumer in this project expects the text it was written as.
		"generated: { by: signpost/0.1.0, at: \"2026-07-30\" }\n"
	if !strings.HasPrefix(fm, wantPrefix) {
		t.Errorf("frontmatter:\n got %q\nwant prefix %q", fm, wantPrefix)
	}
	if !strings.Contains(fm, "attributes:\n  - { name: exported_symbols, value: \"12\" }\n") {
		t.Errorf("attributes block wrong:\n%s", fm)
	}
	// The reader must be able to read what was written.
	root, diag := parseFrontmatter(t, fm)
	if diag != "" {
		t.Fatalf("frontmatter not readable: %s", diag)
	}
	if got := root.Get("type").String(); got != "Module" {
		t.Errorf("type = %q", got)
	}
}

// A page with no commit carries no resource. An absent provenance stamp is better than a
// wrong one, and verify's sha check has nothing to compare an empty resource against.
func TestPageForOmitsResourceWithoutACommit(t *testing.T) {
	g, n := demoGraph(t)
	p := pageFor(g, n, Options{Actor: "signpost/0.1.0", Date: "2026-07-30"})
	if strings.Contains(p.Frontmatter, "resource:") {
		t.Errorf("resource emitted with no base:\n%s", p.Frontmatter)
	}
	if !strings.Contains(p.Frontmatter, "generated: {") {
		t.Error("generated stamp dropped along with the resource")
	}
}

// No date means no `generated:` stamp. `generated.at` comes from the commit, so a run with
// no commit has no honest value for it — and inventing a wall-clock one is the churn ADR
// 0005 forbids.
func TestPageForOmitsGeneratedWithoutADate(t *testing.T) {
	g, n := demoGraph(t)
	p := pageFor(g, n, Options{Actor: "signpost/0.1.0", Resource: "git://x@aaa"})
	if strings.Contains(p.Frontmatter, "generated:") {
		t.Errorf("generated emitted with no date:\n%s", p.Frontmatter)
	}
}

// Every edge carries a confidence, without exception. A renderer that dropped it would make
// an inference indistinguishable from a parsed import, which is the failure ADR 0004 exists
// to prevent.
func TestEdgeListAlwaysCarriesConfidence(t *testing.T) {
	g, n := demoGraph(t)
	root, diag := parseFrontmatter(t, pageFor(g, n, demoOptions()).Frontmatter)
	if diag != "" {
		t.Fatalf("frontmatter not readable: %s", diag)
	}
	edges := root.Get("edges").Seq()
	if len(edges) != 4 {
		t.Fatalf("emitted %d edges, want 4 outgoing", len(edges))
	}
	for i, e := range edges {
		if e.Get("confidence").String() == "" {
			t.Errorf("edge %d has no confidence: %#v", i, e)
		}
		if e.Get("to").String() == "" {
			t.Errorf("edge %d has no target", i)
		}
	}
}

// Outgoing edges only. An edge appears on exactly one page, which keeps the bundle linear in
// the edge count rather than doubling it.
func TestEdgeListIsOutgoingOnly(t *testing.T) {
	g, n := demoGraph(t)
	fm := pageFor(g, n, demoOptions()).Frontmatter
	// api-gateway appears as a co_changes target, so its presence proves nothing; the
	// incoming `imports` from it must not.
	if strings.Contains(fm, "{ kind: imports, to: /modules/api-gateway.md") {
		t.Errorf("an incoming edge was emitted:\n%s", fm)
	}
}

// An edge target is written as a page path, not a node ID, because that is what an OKF link
// resolves against.
func TestEdgeTargetsArePagePaths(t *testing.T) {
	g, n := demoGraph(t)
	fm := pageFor(g, n, demoOptions()).Frontmatter
	if !strings.Contains(fm, "to: /modules/internal-storage.md") {
		t.Errorf("edge target is not a page path:\n%s", fm)
	}
}

func TestEdgeWeightAndSourceAreEmittedWhenPresent(t *testing.T) {
	g, n := demoGraph(t)
	fm := pageFor(g, n, demoOptions()).Frontmatter
	if !strings.Contains(fm, "weight: 14") {
		t.Errorf("co-change weight missing:\n%s", fm)
	}
	if !strings.Contains(fm, "source: internal/auth/auth.go") {
		t.Errorf("edge provenance missing:\n%s", fm)
	}
	// A zero weight is absent rather than emitted as 0, because "not counted" and "counted
	// zero times" are different facts and only the first is true here.
	if strings.Contains(fm, "weight: 0") {
		t.Errorf("a zero weight was emitted:\n%s", fm)
	}
}

// Prose links in addition to the frontmatter list, per §3.1: OKF links are untyped, so a
// generic reader traverses the bundle through these. Emitting only frontmatter would leave a
// conformant consumer unable to walk the graph.
func TestStructureRegionEmitsProseLinks(t *testing.T) {
	g, n := demoGraph(t)
	got, ok := pageFor(g, n, demoOptions()).Managed(regionStructure)
	if !ok {
		t.Fatal("no structure region")
	}
	if !strings.Contains(got, "[internal/storage](/modules/internal-storage.md)") {
		t.Errorf("no prose link to storage:\n%s", got)
	}
	if !strings.Contains(got, "**Imports**") {
		t.Errorf("edge kinds not labelled:\n%s", got)
	}
	if !strings.Contains(got, "`internal/auth/jwt.go`") {
		t.Errorf("file list missing:\n%s", got)
	}
}

// Non-extracted confidence is annotated inline. `extracted` is the silent default:
// annotating the common case would make the uncommon one harder to see.
func TestStructureAnnotatesInferredAndAmbiguousOnly(t *testing.T) {
	g, n := demoGraph(t)
	got, _ := pageFor(g, n, demoOptions()).Managed(regionStructure)
	if !strings.Contains(got, "(inferred)") {
		t.Errorf("an inferred edge was not marked:\n%s", got)
	}
	if !strings.Contains(got, "(ambiguous)") {
		t.Errorf("an ambiguous edge was not marked:\n%s", got)
	}
	if strings.Contains(got, "(extracted)") {
		t.Errorf("the default confidence was annotated:\n%s", got)
	}
}

// An unconnected page says so rather than rendering an empty section, and names the three
// things it could mean instead of implying one.
func TestStructureRegionOnAnOrphan(t *testing.T) {
	g, _ := demoGraph(t)
	got, ok := pageFor(g, g.Node("/modules/orphan"), demoOptions()).Managed(regionStructure)
	if !ok {
		t.Fatal("no structure region")
	}
	if !strings.Contains(got, "Nothing links to or from this page") {
		t.Errorf("orphan structure region = %q", got)
	}
}

// The file list is bounded and says how many it left out, so a truncated list never reads as
// a complete one.
func TestFilesLineIsBoundedAndSaysSo(t *testing.T) {
	n := &graph.Node{ID: "/modules/big", Kind: graph.KindModule, Title: "big"}
	for i := 0; i < 45; i++ {
		n.Files = append(n.Files, "pkg/f"+string(rune('a'+i%26))+string(rune('0'+i/26))+".go")
	}
	got := filesLine(n)
	if !strings.HasPrefix(got, "45 files:\n") {
		t.Errorf("count line = %q", strings.SplitN(got, "\n", 2)[0])
	}
	if !strings.Contains(got, "and 5 more") {
		t.Errorf("truncation not stated:\n%s", got)
	}
	if n := strings.Count(got, "- `"); n != 40 {
		t.Errorf("listed %d files, want the 40-file bound", n)
	}
}

// Markdown link syntax is positional, so a title containing `](` closes the label early and
// what follows becomes the target — with the real target trailing as inert prose. A directory
// named `x](../../../../etc/passwd)(` therefore aims every link that names it wherever the
// directory name said, and `verify` passes clean: the link it checks is well-formed and
// resolves, because the forged one is a different link. The bundle is committed and often
// published, so this is a link on a page other people read.
func TestATitleCannotForgeALinkTarget(t *testing.T) {
	g := graph.New()
	for _, n := range []*graph.Node{
		{ID: "/modules/a", Kind: graph.KindModule, Title: "a", Files: []string{"a.go"}},
		{ID: "/modules/b", Kind: graph.KindModule, Files: []string{"b.go"},
			Title: "b](https://evil.example/x)("},
	} {
		if err := g.AddNode(n); err != nil {
			t.Fatal(err)
		}
	}
	g.AddEdge(graph.Edge{From: "/modules/a", To: "/modules/b",
		Kind: graph.EdgeImports, Conf: graph.Extracted})

	// Both places a title becomes a link: the structure region on a page that names it, and
	// the index.
	root := t.TempDir()
	if _, err := Write(root, g, demoOptions()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	for _, rel := range []string{"index.md", "modules/a.md"} {
		got := read(t, root, rel)
		if strings.Contains(got, "](https://evil.example/x)") {
			t.Errorf("%s carries a forged link target:\n%s", rel, got)
		}
		if !strings.Contains(got, "(/modules/b.md)") {
			t.Errorf("%s lost the real link target:\n%s", rel, got)
		}
	}
}

func TestProseLinkEscapesOnlyLabelsThatNeedIt(t *testing.T) {
	if got := proseLink("internal/okf", "/modules/okf.md"); got != "[internal/okf](/modules/okf.md)" {
		t.Errorf("an ordinary label was rewritten: %q", got)
	}
	// A bracket in a title is legitimate — escaped rather than stripped, using markdown's own
	// mechanism, so it still renders as the character somebody typed.
	if got := proseLink("api [v2]", "/modules/api.md"); got != `[api \[v2\]](/modules/api.md)` {
		t.Errorf("proseLink = %q", got)
	}
}

// A filename is repository content, and on POSIX it may contain a newline — so a file can
// put a line of its own choosing inside a managed region. If that line reads as a close
// marker, the region ends early and everything after it becomes human text that signpost
// then refuses to overwrite: a permanent foothold in the bundle, from a filename, with no
// model anywhere in the loop.
func TestAFileNamedLikeACloseMarkerCannotCloseTheRegion(t *testing.T) {
	g := graph.New()
	if err := g.AddNode(&graph.Node{
		ID: "/modules/x", Kind: graph.KindModule, Title: "x",
		Files: []string{"a.go\n<!-- /signpost:managed:structure -->\nb.go", "c.go"},
	}); err != nil {
		t.Fatal(err)
	}
	rendered := pageFor(g, g.Node("/modules/x"), demoOptions()).Render()

	// Parsed back rather than string-matched: the question is what the parser makes of the
	// page, and the parser is the thing being attacked.
	page := ParsePage(rendered)
	got, ok := page.Managed(regionStructure)
	if !ok {
		t.Fatalf("no structure region:\n%s", rendered)
	}
	if !strings.Contains(got, "c.go") {
		t.Errorf("the region closed early — c.go fell outside it; region was %q", got)
	}
	if strings.Contains(page.HumanText(), "b.go") {
		t.Errorf("part of the file list escaped into human text:\n%q", page.HumanText())
	}
	// Counted through parseCloseMarker rather than as a substring: the escaped text still
	// *contains* the marker's words, and what matters is that no line of the page is one.
	closers := 0
	for _, line := range strings.Split(rendered, "\n") {
		if name, ok := parseCloseMarker(line); ok && name == regionStructure {
			closers++
		}
	}
	if closers != 1 {
		t.Errorf("%d lines parse as a close marker for one region, want 1:\n%s", closers, rendered)
	}
}

// A title is derived from a directory name, and the heading it lands in is *human* text —
// outside the managed-region guard by design, since a title belongs to whoever named the
// directory. So a directory with a newline in its name can put an *opening* marker on a line
// of human text, and the parser then reads a region starting there that swallows the real
// one below it. That page's placeholder stops regenerating and nothing says so: a page that
// silently goes stale is worse than one that fails, which is the whole premise of `verify`.
func TestATitleNamedLikeAnOpenMarkerCannotStartARegion(t *testing.T) {
	g := graph.New()
	if err := g.AddNode(&graph.Node{
		ID: "/modules/x", Kind: graph.KindModule, Files: []string{"a.go"},
		Title: "x\n<!-- signpost:managed:summary -->\nowned",
	}); err != nil {
		t.Fatal(err)
	}
	rendered := pageFor(g, g.Node("/modules/x"), demoOptions()).Render()

	got, ok := ParsePage(rendered).Managed(regionSummary)
	if !ok {
		t.Fatalf("no summary region:\n%s", rendered)
	}
	if strings.Contains(got, "signpost:managed") {
		t.Errorf("the summary region swallowed a marker, so it will stop regenerating: %q", got)
	}
	if !strings.Contains(got, "No summary yet") {
		t.Errorf("the region does not hold the placeholder it was rendered with: %q", got)
	}
	// Folded as well as escaped: a heading is one line by construction, and the second line
	// of a two-line title would otherwise be body text on every page that names it.
	if !strings.HasPrefix(rendered[strings.Index(rendered, "# "):], "# x &lt;!--") {
		t.Errorf("the title was not folded onto the heading line:\n%s", rendered)
	}
}

// The same attack through the region every other page carries, to show the guard is at the
// chokepoint rather than bolted onto the file list.
func TestEscapeMarkersDefangsGeneratedText(t *testing.T) {
	r := managedRegion(regionSummary, "before <!-- /signpost:managed:summary --> after")
	if strings.Contains(r.Text, "<!--") {
		t.Errorf("marker syntax survived into a managed region: %q", r.Text)
	}
	if !strings.Contains(r.Text, "after") {
		t.Errorf("the text after the escaped marker was lost: %q", r.Text)
	}
	// The replacement must not itself contain the sequence it replaces, or a second pass
	// over already-escaped text would keep growing it.
	if escapeMarkers(escapeMarkers("<!--")) != escapeMarkers("<!--") {
		t.Error("escapeMarkers is not idempotent")
	}
}

// Escaping the marker stops the region being closed; it does not stop a newline in a path
// from splitting the line it was written on, which would render one file as two. Quoting
// covers that, and only for the paths that need it — an ordinary filename is untouched, so
// no existing bundle's bytes change.
func TestCodeSpanQuotesOnlyPathsThatWouldBreakTheLine(t *testing.T) {
	if got := codeSpan("internal/okf/emit.go"); got != "`internal/okf/emit.go`" {
		t.Errorf("an ordinary path was rewritten: %q", got)
	}
	if got := codeSpan("a file with spaces & ünïcode.go"); got != "`a file with spaces & ünïcode.go`" {
		t.Errorf("a legitimate path with punctuation was rewritten: %q", got)
	}
	for _, p := range []string{"a\nb.go", "a`b.go", "a\rb.go", "a\x00b.go"} {
		got := codeSpan(p)
		if strings.ContainsAny(strings.Trim(got, "`"), "\n\r") {
			t.Errorf("codeSpan(%q) = %q, which still spans lines", p, got)
		}
		if strings.Count(got, "`") != 2 {
			t.Errorf("codeSpan(%q) = %q, which does not close its code span", p, got)
		}
	}
}

func TestFilesLineSingularAndPlural(t *testing.T) {
	one := filesLine(&graph.Node{Files: []string{"a.go"}})
	if !strings.HasPrefix(one, "1 file:\n") {
		t.Errorf("singular = %q", one)
	}
	two := filesLine(&graph.Node{Files: []string{"a.go", "b.go"}})
	if !strings.HasPrefix(two, "2 files:\n") {
		t.Errorf("plural = %q", two)
	}
}

// The summary placeholder states what the region is rather than inventing a description. A
// reader seeing this line knows the semantic pass has not run; a reader seeing invented
// prose would not.
func TestSummaryPlaceholderWhenThereIsNoDescription(t *testing.T) {
	got := summaryText(&graph.Node{ID: "/modules/x", Title: "x"})
	if !strings.Contains(got, "No summary yet") {
		t.Errorf("summaryText = %q", got)
	}
	if !strings.Contains(got, "never overwritten") {
		t.Error("the placeholder does not say the human's text is safe")
	}
}

func TestSummaryUsesTheExtractedDescriptionWhenThereIsOne(t *testing.T) {
	got := summaryText(&graph.Node{Description: "Token table."})
	if got != "Token table.\n" {
		t.Errorf("summaryText = %q", got)
	}
}

// The Notes section is outside the managed markers and says so. §6.1's mechanism only
// compounds if people write there, and an empty heading under a generated page reads as
// something the tool will overwrite.
func TestPageInvitesHumanNotesOutsideTheMarkers(t *testing.T) {
	g, n := demoGraph(t)
	p := pageFor(g, n, demoOptions())
	human := p.HumanText()
	if !strings.Contains(human, "## Notes") {
		t.Errorf("no Notes heading in human text:\n%s", human)
	}
	if !strings.Contains(human, "Anything written here is yours") {
		t.Errorf("no invitation:\n%s", human)
	}
	// And it must survive a parse of the rendered page, which is the path a real re-run
	// takes.
	if got := ParsePage(p.Render()).HumanText(); got != human {
		t.Errorf("human text changed across a render/parse cycle:\n got %q\nwant %q", got, human)
	}
}

// The role region and the deterministic build have to coexist, and these four tests are
// the contract between them. The property that matters is not "a role region renders" —
// it is that a deterministic run neither emits one nor destroys one, because §8 runs the
// deterministic pass on every push and the semantic pass on a schedule.

func TestNoRoleRegionWithoutSemanticOutput(t *testing.T) {
	g, n := demoGraph(t)
	rendered := pageFor(g, n, demoOptions()).Render()
	if strings.Contains(rendered, "signpost:managed:role") {
		t.Errorf("a deterministic build emitted a role region:\n%s", rendered)
	}
	if strings.Contains(rendered, "## Role") {
		t.Errorf("a deterministic build emitted a Role heading:\n%s", rendered)
	}
}

func TestRoleRegionCarriesItsOwnHeading(t *testing.T) {
	// The heading is inside the managed region rather than beside it, because Merge appends
	// a new managed region at the end and takes human regions only from disk — so a heading
	// emitted as its own human region would be dropped the first time a semantic run met an
	// existing bundle, leaving the prose under "Notes" with nothing naming it.
	g, n := demoGraph(t)
	opts := demoOptions()
	opts.Roles = map[string]string{n.ID: "Validates tokens.\n"}
	p := pageFor(g, n, opts)

	role, ok := p.Managed(regionRole)
	if !ok {
		t.Fatalf("no role region:\n%s", p.Render())
	}
	if !strings.Contains(role, "## Role") {
		t.Errorf("the heading is not inside the region: %q", role)
	}
	if strings.Contains(p.HumanText(), "## Role") {
		t.Errorf("the heading leaked into human text:\n%s", p.HumanText())
	}
}

func TestRoleRegionIsOnlyOnTheNodesThatHaveOne(t *testing.T) {
	g, n := demoGraph(t)
	opts := demoOptions()
	opts.Roles = map[string]string{n.ID: "Validates tokens.\n"}

	if _, ok := pageFor(g, n, opts).Managed(regionRole); !ok {
		t.Error("the summarised node has no role region")
	}
	other := g.Node("/modules/orphan")
	if _, ok := pageFor(g, other, opts).Managed(regionRole); ok {
		t.Error("an unsummarised node got a role region")
	}
}

func TestRoleDoesNotDisturbTheSummaryRegion(t *testing.T) {
	// The two are separate claims with separate trust grades (§4.1): summary is counted
	// facts, role is a grounded guess. A semantic run must not overwrite the first with the
	// second.
	g, n := demoGraph(t)
	opts := demoOptions()
	opts.Roles = map[string]string{n.ID: "Validates tokens.\n"}
	p := pageFor(g, n, opts)

	summary, ok := p.Managed(regionSummary)
	if !ok {
		t.Fatal("no summary region")
	}
	if !strings.Contains(summary, n.Description) {
		t.Errorf("the deterministic summary was replaced: %q", summary)
	}
}

func TestEdgeKindLabelFallsBackToTheRawValue(t *testing.T) {
	if got := edgeKindLabel(graph.EdgeKind("teleports_to")); got != "teleports_to" {
		t.Errorf("edgeKindLabel = %q, want the raw value", got)
	}
	if got := edgeKindLabel(graph.EdgeCoChanges); got != "Changes with" {
		t.Errorf("edgeKindLabel(co_changes) = %q", got)
	}
}

func TestResourceFor(t *testing.T) {
	cases := []struct{ base, path, want string }{
		{"git://x@aaa", "internal/auth", "git://x@aaa/internal/auth"},
		{"git://x@aaa", "", "git://x@aaa"},
		{"", "internal/auth", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := resourceFor(c.base, c.path); got != c.want {
			t.Errorf("resourceFor(%q, %q) = %q, want %q", c.base, c.path, got, c.want)
		}
	}
}

func TestAttributeMapSortsAndSkipsEmpty(t *testing.T) {
	got := attributeMap(map[string]string{"z": "1", "a": "2", "empty": ""})
	if len(got) != 2 {
		t.Fatalf("attributeMap returned %d entries, want 2", len(got))
	}
	if got[0].pairs[1].val.scalar != "2" || got[1].pairs[1].val.scalar != "1" {
		t.Errorf("attributes not sorted by name: %#v", got)
	}
}

func TestPagePath(t *testing.T) {
	if got := pagePath("/modules/auth"); got != "/modules/auth.md" {
		t.Errorf("pagePath = %q", got)
	}
}
