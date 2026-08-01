package okf

import (
	"strings"
	"testing"
)

// The tests in this file are the ones the package comment says carry the most weight, and
// they are written against one property: **text outside a managed region is returned
// byte-for-byte.** Every case that could be read two ways is asserted to resolve toward
// preserving the ambiguous text, because the cost of that choice is a region that stops
// regenerating — which verify reports — and the cost of the other is deleting writing
// nobody knows to look for.

// mustHuman asserts that every byte of want survives a parse-render cycle.
func mustHuman(t *testing.T, src, want string) {
	t.Helper()
	p := ParsePage(src)
	if got := p.HumanText(); got != want {
		t.Errorf("HumanText() = %q, want %q", got, want)
	}
}

func TestParsePageSplitsFrontmatterAndRegions(t *testing.T) {
	src := "---\ntype: Module\ntitle: auth\n---\n" +
		"# auth\n\n" +
		"<!-- signpost:managed:summary -->\ngenerated prose\n<!-- /signpost:managed:summary -->\n" +
		"\n## Notes\n\nmine.\n"
	p := ParsePage(src)

	if !p.HasFrontmatter {
		t.Fatal("HasFrontmatter = false")
	}
	if p.Frontmatter != "type: Module\ntitle: auth\n" {
		t.Errorf("Frontmatter = %q", p.Frontmatter)
	}
	got, ok := p.Managed("summary")
	if !ok {
		t.Fatal("summary region not found")
	}
	if got != "generated prose\n" {
		t.Errorf("summary = %q", got)
	}
	mustHuman(t, src, "# auth\n\n\n## Notes\n\nmine.\n")
	if rendered := p.Render(); rendered != src {
		t.Errorf("Render() is not identity:\n got %q\nwant %q", rendered, src)
	}
}

// A page with no frontmatter is a plain markdown file someone dropped in the directory.
// Distinguished from one with empty frontmatter, because the first is legitimate and the
// second is malformed, and Render must not invent a fence around the first.
func TestParsePageWithoutFrontmatter(t *testing.T) {
	src := "# just markdown\n\nno fence here.\n"
	p := ParsePage(src)
	if p.HasFrontmatter {
		t.Error("HasFrontmatter = true for a page with no fence")
	}
	if got := p.Render(); got != src {
		t.Errorf("Render() = %q, want the input unchanged", got)
	}
}

// A `---` further down the file is a horizontal rule. Treating one as a frontmatter opener
// would swallow the document above it.
func TestFrontmatterFenceMustBeTheFirstLine(t *testing.T) {
	src := "# heading\n\n---\n\nbelow the rule.\n"
	p := ParsePage(src)
	if p.HasFrontmatter {
		t.Error("a horizontal rule was read as a frontmatter fence")
	}
	mustHuman(t, src, src)
}

// An unterminated fence means we cannot tell where frontmatter ends. The whole file becomes
// body: guessing would either lose body text or parse prose as YAML.
func TestUnterminatedFrontmatterKeepsEverythingAsBody(t *testing.T) {
	src := "---\ntype: Module\n\n# no closing fence\n"
	p := ParsePage(src)
	if p.HasFrontmatter {
		t.Error("HasFrontmatter = true for an unterminated fence")
	}
	mustHuman(t, src, src)
}

// A `---` inside a quoted scalar must not close the block early, which is why the close is
// found line-wise rather than by substring scan.
func TestFrontmatterCloseIsFoundLineWise(t *testing.T) {
	src := "---\ntitle: \"a --- b\"\ntype: Module\n---\nbody\n"
	p := ParsePage(src)
	if p.Frontmatter != "title: \"a --- b\"\ntype: Module\n" {
		t.Errorf("Frontmatter = %q; the quoted --- closed the block early", p.Frontmatter)
	}
}

func TestParsePageToleratesLeadingBOM(t *testing.T) {
	src := bom + "---\ntype: Module\n---\nbody\n"
	p := ParsePage(src)
	if !p.HasFrontmatter {
		t.Fatal("a BOM before the fence hid the frontmatter")
	}
	if p.Frontmatter != "type: Module\n" {
		t.Errorf("Frontmatter = %q", p.Frontmatter)
	}
}

func TestParsePageHandlesCRLF(t *testing.T) {
	src := "---\r\ntype: Module\r\n---\r\n" +
		"# auth\r\n" +
		"<!-- signpost:managed:summary -->\r\ngenerated\r\n<!-- /signpost:managed:summary -->\r\n" +
		"mine\r\n"
	p := ParsePage(src)
	if !p.HasFrontmatter {
		t.Fatal("CRLF frontmatter not recognised")
	}
	got, ok := p.Managed("summary")
	if !ok {
		t.Fatal("CRLF markers not recognised")
	}
	if got != "generated\r\n" {
		t.Errorf("summary = %q; the region's own line endings must survive", got)
	}
	mustHuman(t, src, "# auth\r\nmine\r\n")
}

// The dangerous case. An unterminated region means we do not know where the generated text
// was meant to stop, so replacing it would replace an unknown amount of someone's writing.
// Everything from the open marker on is therefore human.
func TestUnterminatedRegionIsAllHuman(t *testing.T) {
	src := "---\ntype: Module\n---\n" +
		"# auth\n" +
		"<!-- signpost:managed:summary -->\n" +
		"text I wrote after the marker, with no close.\n"
	p := ParsePage(src)
	if _, ok := p.Managed("summary"); ok {
		t.Fatal("an unterminated region was treated as managed")
	}
	mustHuman(t, src, "# auth\n<!-- signpost:managed:summary -->\ntext I wrote after the marker, with no close.\n")
}

// A close marker whose name does not match the open one does not close it. Mismatched names
// mean the page was hand-edited into a state we cannot interpret, and the safe reading is
// that none of it is ours.
func TestMismatchedCloseNameDoesNotClose(t *testing.T) {
	src := "---\nx: 1\n---\n" +
		"<!-- signpost:managed:summary -->\nbody\n<!-- /signpost:managed:notes -->\n"
	p := ParsePage(src)
	if _, ok := p.Managed("summary"); ok {
		t.Fatal("a mismatched close marker closed the region")
	}
	mustHuman(t, src, "<!-- signpost:managed:summary -->\nbody\n<!-- /signpost:managed:notes -->\n")
}

// A marker with text after it on the same line is prose that mentions a marker, which is
// exactly what this package's own documentation contains. Treating it as a marker would
// make signpost's own source unreadable to itself.
func TestMarkerWithTrailingTextIsNotAMarker(t *testing.T) {
	src := "---\nx: 1\n---\n" +
		"The marker <!-- signpost:managed:summary --> goes here.\n" +
		"<!-- signpost:managed:summary --> and this one has a tail\n"
	p := ParsePage(src)
	if len(p.Body) != 1 || p.Body[0].Managed() {
		t.Fatalf("Body = %#v, want one human region", p.Body)
	}
	mustHuman(t, src, "The marker <!-- signpost:managed:summary --> goes here.\n"+
		"<!-- signpost:managed:summary --> and this one has a tail\n")
}

// A stray close marker with no open is human text, not a region boundary.
func TestStrayCloseMarkerIsHuman(t *testing.T) {
	src := "---\nx: 1\n---\nbefore\n<!-- /signpost:managed:summary -->\nafter\n"
	mustHuman(t, src, "before\n<!-- /signpost:managed:summary -->\nafter\n")
}

// A name outside the allowed set is not a marker at all. The name comes out of a repository
// file and is matched textually, so a name that could contain marker syntax or vary by case
// is refused rather than interpreted.
func TestInvalidRegionNamesAreNotMarkers(t *testing.T) {
	bad := []string{
		"Summary",
		"sum mary",
		"sum:mary",
		"",
		strings.Repeat("a", 65),
	}
	for _, name := range bad {
		src := "---\nx: 1\n---\n" +
			markerPrefix + name + markerSuffix + "\nbody\n" +
			markerEnd + name + markerSuffix + "\n"
		p := ParsePage(src)
		if _, ok := p.Managed(name); ok {
			t.Errorf("region name %q was accepted", name)
		}
		if got := p.HumanText(); got != strings.TrimPrefix(src, "---\nx: 1\n---\n") {
			t.Errorf("name %q: HumanText() = %q", name, got)
		}
	}
}

func TestValidRegionNameBoundaries(t *testing.T) {
	ok := []string{"a", "summary", "log-2026-07-30", "with_underscore", "n0", strings.Repeat("a", 64)}
	for _, s := range ok {
		if !validRegionName(s) {
			t.Errorf("validRegionName(%q) = false", s)
		}
	}
	bad := []string{"", "A", "a b", "a:b", "a.b", "a/b", strings.Repeat("a", 65), "café"}
	for _, s := range bad {
		if validRegionName(s) {
			t.Errorf("validRegionName(%q) = true", s)
		}
	}
}

// The same region name twice. The first open pairs with the first matching close; what
// follows is human, including the second pair's markers. That keeps the amount of text
// signpost claims bounded by the first region rather than spanning to the last close.
func TestDuplicateRegionNameClaimsOnlyTheFirst(t *testing.T) {
	src := "---\nx: 1\n---\n" +
		"<!-- signpost:managed:summary -->\nfirst\n<!-- /signpost:managed:summary -->\n" +
		"middle\n" +
		"<!-- signpost:managed:summary -->\nsecond\n<!-- /signpost:managed:summary -->\n"
	p := ParsePage(src)
	got, ok := p.Managed("summary")
	if !ok {
		t.Fatal("no summary region")
	}
	if got != "first\n" {
		t.Errorf("Managed(summary) = %q, want the first region", got)
	}
	if rendered := p.Render(); rendered != src {
		t.Errorf("Render() is not identity:\n got %q\nwant %q", rendered, src)
	}
}

// An empty managed region is a region, not a gap. A model backend that produced nothing
// must not cause the region to be re-read as human text on the next run.
func TestEmptyManagedRegion(t *testing.T) {
	src := "---\nx: 1\n---\n<!-- signpost:managed:summary -->\n<!-- /signpost:managed:summary -->\n"
	p := ParsePage(src)
	got, ok := p.Managed("summary")
	if !ok {
		t.Fatal("an empty region was not recognised")
	}
	if got != "" {
		t.Errorf("Managed(summary) = %q, want empty", got)
	}
	if rendered := p.Render(); rendered != src {
		t.Errorf("Render() = %q, want %q", rendered, src)
	}
}

// Whitespace a human left is theirs. Not trimmed, not re-indented: deciding their
// whitespace was wrong is the same class of act as deciding their sentence was.
func TestHumanWhitespaceIsPreservedExactly(t *testing.T) {
	src := "---\nx: 1\n---\n" +
		"   indented note with trailing spaces   \n\n\n\n" +
		"<!-- signpost:managed:summary -->\ngen\n<!-- /signpost:managed:summary -->\n" +
		"\t tab-indented\t\n"
	p := ParsePage(src)
	if got := p.Render(); got != src {
		t.Errorf("Render() = %q, want %q", got, src)
	}
}

// Consecutive human lines are one region, not one per line. Not cosmetic: Merge copies
// human regions verbatim, and a per-line split would multiply the region list by the length
// of the page and make the merge's ordering harder to reason about.
func TestConsecutiveHumanLinesAreOneRegion(t *testing.T) {
	p := ParsePage("---\nx: 1\n---\na\nb\nc\n")
	if len(p.Body) != 1 {
		t.Fatalf("Body has %d regions, want 1: %#v", len(p.Body), p.Body)
	}
}

func TestRenderRoundTripsAGeneratedPage(t *testing.T) {
	src := NewPage("type: Module\n",
		humanRegion("# auth\n\n"),
		managedRegion("summary", "prose"),
		humanRegion("\n## Notes\n\n"),
	).Render()
	if got := ParsePage(src).Render(); got != src {
		t.Errorf("Render(Parse(Render(p))) != Render(p):\n got %q\nwant %q", got, src)
	}
}

// Frontmatter with no trailing newline still renders a fence on its own line, so a
// hand-edited page missing the final newline does not produce `x: 1---`.
func TestRenderAddsMissingFrontmatterNewline(t *testing.T) {
	p := &Page{Frontmatter: "x: 1", HasFrontmatter: true}
	if got := p.Render(); got != "---\nx: 1\n---\n" {
		t.Errorf("Render() = %q", got)
	}
}

// Merge: the property the whole design rests on.
func TestMergeReplacesManagedAndKeepsHuman(t *testing.T) {
	onDisk := ParsePage("---\ntype: Module\ntitle: old\n---\n" +
		"# auth\n\n" +
		"<!-- signpost:managed:summary -->\nstale generated text\n<!-- /signpost:managed:summary -->\n" +
		"\n## Notes\n\nRate limiting lives in the gateway. Read the incident review first.\n")
	generated := ParsePage("---\ntype: Module\ntitle: new\n---\n" +
		"# auth\n\n" +
		"<!-- signpost:managed:summary -->\nfresh generated text\n<!-- /signpost:managed:summary -->\n" +
		"\n## Notes\n\nboilerplate invitation\n")

	out := onDisk.Merge(generated).Render()

	if !strings.Contains(out, "fresh generated text") {
		t.Error("the managed region was not replaced")
	}
	if strings.Contains(out, "stale generated text") {
		t.Error("the stale managed text survived")
	}
	if !strings.Contains(out, "Read the incident review first.") {
		t.Fatal("a human note was lost")
	}
	if strings.Contains(out, "boilerplate invitation") {
		t.Error("the generated page's own human seed text overwrote the human's version")
	}
	if !strings.Contains(out, "title: new") {
		t.Error("a generated frontmatter key was not refreshed")
	}
}

// A region the generator no longer produces keeps its content. It may be one an older
// signpost wrote, and the text is at worst stale rather than wrong — where dropping it
// silently deletes content on a downgrade.
func TestMergeKeepsRegionsTheGeneratorNoLongerProduces(t *testing.T) {
	onDisk := ParsePage("---\nx: 1\n---\n" +
		"<!-- signpost:managed:summary -->\nA\n<!-- /signpost:managed:summary -->\n" +
		"<!-- signpost:managed:retired -->\nB\n<!-- /signpost:managed:retired -->\n")
	generated := ParsePage("---\nx: 1\n---\n" +
		"<!-- signpost:managed:summary -->\nA'\n<!-- /signpost:managed:summary -->\n")

	out := onDisk.Merge(generated)
	if got, _ := out.Managed("summary"); got != "A'\n" {
		t.Errorf("summary = %q, want the regenerated text", got)
	}
	if got, ok := out.Managed("retired"); !ok || got != "B\n" {
		t.Errorf("retired = %q, %v; want its content kept", got, ok)
	}
}

// A region the generator has newly added is appended rather than inserted, because next's
// position is relative to its own regions and the human text around the existing ones is
// the thing being preserved.
func TestMergeAppendsNewRegionsAfterExistingContent(t *testing.T) {
	onDisk := ParsePage("---\nx: 1\n---\n" +
		"my heading\n" +
		"<!-- signpost:managed:summary -->\nA\n<!-- /signpost:managed:summary -->\n" +
		"my trailing note\n")
	generated := ParsePage("---\nx: 1\n---\n" +
		"<!-- signpost:managed:practices -->\nnew section\n<!-- /signpost:managed:practices -->\n" +
		"<!-- signpost:managed:summary -->\nA'\n<!-- /signpost:managed:summary -->\n")

	out := onDisk.Merge(generated).Render()
	iNote := strings.Index(out, "my trailing note")
	iNew := strings.Index(out, "new section")
	if iNote < 0 || iNew < 0 {
		t.Fatalf("output missing content:\n%s", out)
	}
	if iNew < iNote {
		t.Error("a new region was inserted above human text instead of appended below it")
	}
}

// Merging into a page with no frontmatter produces one. A plain markdown file in the bundle
// gets adopted rather than either skipped or rewritten wholesale.
func TestMergeAddsFrontmatterToAPlainMarkdownPage(t *testing.T) {
	onDisk := ParsePage("# hand-written page\n\nmy notes\n")
	generated := ParsePage("---\ntype: Module\n---\n" +
		"<!-- signpost:managed:summary -->\ngen\n<!-- /signpost:managed:summary -->\n")

	out := onDisk.Merge(generated).Render()
	if !strings.HasPrefix(out, "---\ntype: Module\n---\n") {
		t.Errorf("frontmatter not added:\n%s", out)
	}
	if !strings.Contains(out, "my notes") {
		t.Error("the human page's content was lost")
	}
	if !strings.Contains(out, "gen") {
		t.Error("the new managed region was not appended")
	}
}

// Merge is idempotent: merging the same generated page twice produces the same bytes. This
// is byte-stability at the page level, and it is what keeps a re-run at the same commit from
// showing up as a diff.
func TestMergeIsIdempotent(t *testing.T) {
	generated := ParsePage("---\ntype: Module\ntitle: auth\n---\n" +
		"# auth\n\n" +
		"<!-- signpost:managed:summary -->\ngen\n<!-- /signpost:managed:summary -->\n" +
		"\n## Notes\n\nseed\n")
	first := ParsePage(generated.Render()).Merge(generated).Render()
	second := ParsePage(first).Merge(generated).Render()
	if first != second {
		t.Errorf("merge is not idempotent:\nfirst  %q\nsecond %q", first, second)
	}
}

// A human who deleted a managed region's markers entirely gets the region back, appended,
// rather than losing it. Their surrounding text is untouched either way.
func TestMergeRestoresADeletedRegion(t *testing.T) {
	onDisk := ParsePage("---\nx: 1\n---\n# auth\n\nI deleted the generated bit.\n")
	generated := ParsePage("---\nx: 1\n---\n" +
		"<!-- signpost:managed:summary -->\ngen\n<!-- /signpost:managed:summary -->\n")
	out := onDisk.Merge(generated).Render()
	if !strings.Contains(out, "I deleted the generated bit.") {
		t.Error("human text lost")
	}
	if !strings.Contains(out, "<!-- signpost:managed:summary -->") {
		t.Error("the deleted region was not restored")
	}
}

func TestEnsureTrailingNewline(t *testing.T) {
	cases := map[string]string{
		"":           "\n",
		"a":          "a\n",
		"a\n":        "a\n",
		"a\n\n\n":    "a\n",
		"a\nb":       "a\nb\n",
		"\n":         "\n",
		"trailing  ": "trailing  \n",
		"keep\ttabs": "keep\ttabs\n",
		// A CRLF run leaves one blank line rather than none. Asserted as it behaves, not as
		// it might ideally: the close marker still lands on its own line, and the input
		// cannot arise, since generated text is emitted with \n and text read from disk goes
		// through Merge instead. See ensureTrailingNewline.
		"crlf\r\n\r\n": "crlf\r\n\r\n",
	}
	for in, want := range cases {
		if got := ensureTrailingNewline(in); got != want {
			t.Errorf("ensureTrailingNewline(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNormalizeReadOnlyTouchesCRLF pins the scope of the read-side normalisation directly.
//
// The rule is narrow on purpose. Converting CRLF is undoing a transport encoding git chose;
// touching anything else would be editing what a human wrote, which the invariant at the top
// of page.go forbids. The lone-CR case is the one worth spelling out: no git conversion
// produces a bare CR, so one that reaches here is a byte someone put in the file deliberately
// — a classic Mac line ending, or content inside a code fence — and rewriting it would lose
// information rather than recover it.
func TestNormalizeReadOnlyTouchesCRLF(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"a\nb\n":                  "a\nb\n",                // already LF: unchanged
		"a\r\nb\r\n":              "a\nb\n",                // the whole point
		"a\r\nb\n":                "a\nb\n",                // mixed, as a partial hand-edit leaves it
		"a\rb":                    "a\rb",                  // lone CR is content, not a line ending
		"a\r\rb":                  "a\r\rb",                // still content
		"a\r\r\nb":                "a\r\nb",                // CR then CRLF: only the CRLF converts
		"trailing spaces:   \r\n": "trailing spaces:   \n", // whitespace before the break survives
		"\r\n":                    "\n",
	}
	for in, want := range cases {
		if got := normalizeRead(in); got != want {
			t.Errorf("normalizeRead(%q) = %q, want %q", in, got, want)
		}
	}
}
