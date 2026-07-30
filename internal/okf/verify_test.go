package okf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/graph"
)

// verifyBundle runs Verify against the demo graph, failing the test on a real error.
func verifyBundle(t *testing.T, root string, g *graph.Graph) *VerifyResult {
	t.Helper()
	res, err := Verify(root, g, demoOptions())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	return res
}

// edit rewrites a bundle file. Every negative test below works by breaking a real bundle
// rather than by hand-writing one, because a hand-written fixture can drift from what the
// emitter actually produces and then the test passes while the tool is broken.
func edit(t *testing.T, root, rel string, fn func(string) string) {
	t.Helper()
	full := filepath.Join(root, BundleDir, filepath.FromSlash(rel))
	b, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(fn(string(b))), 0o644); err != nil {
		t.Fatalf("writing %s: %v", rel, err)
	}
}

// findings renders a result's failures for an assertion message.
func findings(res *VerifyResult) string {
	var b strings.Builder
	for _, f := range res.Findings {
		b.WriteString("\n  " + string(f.Kind) + " " + f.String())
	}
	for _, w := range res.Warnings {
		b.WriteString("\n  warning " + string(w.Kind) + " " + w.String())
	}
	return b.String()
}

// has reports whether a result carries a finding of a kind on a page.
func has(list []Finding, kind FindingKind, page string) bool {
	for _, f := range list {
		if f.Kind == kind && f.Page == page {
			return true
		}
	}
	return false
}

// The property the command exists for: a bundle the emitter just wrote passes, and the pass
// says what it covered. A verify reporting "ok" over zero pages is indistinguishable from a
// verify that opened nothing, which is the false pass design §4.6 forbids.
func TestVerifyPassesOnAFreshlyWrittenBundle(t *testing.T) {
	root, _, g := write(t)

	res := verifyBundle(t, root, g)
	if !res.OK() {
		t.Fatalf("a freshly written bundle did not verify:%s", findings(res))
	}
	nodes, _ := g.Counts()
	if want := nodes + 2; res.Checked.Pages != want {
		t.Errorf("checked %d pages, want %d (one per node, plus index and log)",
			res.Checked.Pages, want)
	}
	if res.Checked.Edges == 0 || res.Checked.Links == 0 {
		t.Errorf("resolved %d edges and %d prose links; both must be non-zero or the pass "+
			"means nothing", res.Checked.Edges, res.Checked.Links)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("nothing should be skipped on a complete bundle, got %v", res.Skipped)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings:%s", findings(res))
	}
}

// The failure that matters most, and the reason the exit code is the interface: a bundle
// describing another commit must not pass.
func TestVerifyFailsWhenTheBundleDescribesAnotherCommit(t *testing.T) {
	root, _, g := write(t)
	edit(t, root, ManifestFile, func(s string) string {
		return strings.ReplaceAll(s, "8f2a1c9", "0000000")
	})

	res := verifyBundle(t, root, g)
	if res.OK() {
		t.Fatal("a bundle describing another commit passed verification")
	}
	if !has(res.Findings, FindingStaleResource, ManifestFile) {
		t.Errorf("want a stale-resource finding on the manifest, got:%s", findings(res))
	}
	// Per-page comparison is suppressed and *said to be*, rather than silently skipped:
	// every page would report the same fact the manifest line already states.
	if len(res.Skipped) == 0 {
		t.Error("the suppressed per-page comparison was not reported as skipped")
	}
}

// A page's own resource is checked, not just the manifest's — a bundle can be partly
// rebuilt, and the page a reader opens is the one whose claim has to be true.
func TestVerifyFailsOnAPageDescribingAnotherCommit(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	edit(t, root, page, func(s string) string {
		return strings.Replace(s, "@8f2a1c9/internal/auth", "@0000000/internal/auth", 1)
	})

	res := verifyBundle(t, root, g)
	if !has(res.Findings, FindingStaleResource, page) {
		t.Errorf("want a stale-resource finding on %s, got:%s", page, findings(res))
	}
}

// The staleness check is the one whose silent success destroys the tool, so when it cannot
// run it is named rather than passed over.
func TestVerifyNamesTheStalenessCheckAsSkippedWithoutACommit(t *testing.T) {
	root := t.TempDir()
	g, _ := demoGraph(t)
	// No Resource: what a repository with no readable history, or a -no-history run,
	// produces.
	opts := Options{Actor: "signpost/0.1.0", Date: "2026-07-30"}
	if _, err := Write(root, g, opts); err != nil {
		t.Fatalf("Write: %v", err)
	}
	res, err := Verify(root, g, opts)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Fatalf("a bundle with no commit should still verify:%s", findings(res))
	}
	if len(res.Skipped) == 0 {
		t.Fatal("staleness was not checked and not reported as skipped, which is the " +
			"false pass verify exists to prevent")
	}
	if !strings.Contains(strings.Join(res.Skipped, " "), "staleness") {
		t.Errorf("Skipped does not name the staleness check: %v", res.Skipped)
	}
}

func TestVerifyFailsWhenThereIsNoBundle(t *testing.T) {
	g, _ := demoGraph(t)
	res := verifyBundle(t, t.TempDir(), g)

	if res.OK() {
		t.Fatal("a missing bundle passed verification")
	}
	// One finding, not one per page: eighty missing-page findings would bury the fact that
	// explains all of them.
	if len(res.Findings) != 1 || res.Findings[0].Kind != FindingMissingBundle {
		t.Errorf("want exactly one missing-bundle finding, got:%s", findings(res))
	}
}

// The byte-stability check, and the case that makes it stronger than re-rendering twice: a
// hand-broken marker means the region stops regenerating, which page.go accepts to avoid
// deleting anyone's writing. This is the check that makes that cost visible rather than
// permanent.
func TestVerifyFailsOnAPageWhoseManagedRegionStoppedRegenerating(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	edit(t, root, page, func(s string) string {
		return strings.Replace(s, closeMarker(regionSummary), "<!-- /signpost:managed:sumary -->", 1)
	})

	res := verifyBundle(t, root, g)
	if !has(res.Findings, FindingOutOfDate, page) {
		t.Errorf("want an out-of-date finding on %s, got:%s", page, findings(res))
	}
}

func TestVerifyFailsOnEditedGeneratedContent(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	edit(t, root, page, func(s string) string {
		return strings.Replace(s, "JWT validation", "something else entirely", 1)
	})

	res := verifyBundle(t, root, g)
	if !has(res.Findings, FindingOutOfDate, page) {
		t.Errorf("want an out-of-date finding on %s, got:%s", page, findings(res))
	}
}

// The other half of that property, and the more important half: a human's text outside the
// managed markers must not make the gate fail. A verify that failed on someone's notes is a
// verify they disable, and then nothing is checked at all.
func TestVerifyPassesWithHumanNotesAndChecksTheirLinks(t *testing.T) {
	root, _, g := write(t)
	before := verifyBundle(t, root, g).Checked.Links

	const page = "modules/internal-auth.md"
	edit(t, root, page, func(s string) string {
		return s + "\n## My notes\n\nSee [storage](/modules/internal-storage.md).\n"
	})

	res := verifyBundle(t, root, g)
	if !res.OK() {
		t.Fatalf("human notes broke verification:%s", findings(res))
	}
	if res.Checked.Links != before+1 {
		t.Errorf("resolved %d links, want %d: a human's link must be checked too",
			res.Checked.Links, before+1)
	}
}

func TestVerifyFailsOnABrokenLinkAndSaysWhoseItIs(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	edit(t, root, page, func(s string) string {
		return s + "\nSee [gone](/modules/deleted.md).\n"
	})

	res := verifyBundle(t, root, g)
	if !has(res.Findings, FindingBrokenLink, page) {
		t.Fatalf("want a broken-link finding on %s, got:%s", page, findings(res))
	}
	// "prose" rather than a region name: a broken link a human wrote is a typo they can
	// fix, and one in a managed region is signpost's problem. The message says which.
	for _, f := range res.Findings {
		if f.Kind == FindingBrokenLink && !strings.Contains(f.Detail, "prose") {
			t.Errorf("finding does not say where the link is: %q", f.Detail)
		}
	}
}

func TestVerifyFailsOnABrokenEdgeTarget(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	edit(t, root, page, func(s string) string {
		return strings.Replace(s, "to: /modules/internal-storage.md",
			"to: /modules/deleted.md", 1)
	})

	res := verifyBundle(t, root, g)
	if !has(res.Findings, FindingBrokenLink, page) {
		t.Errorf("want a broken-link finding for the edge on %s, got:%s", page, findings(res))
	}
}

// A link inside code is documentation, not a link. Both spellings are tested because a page
// explaining the bundle's own conventions contains examples of both, and a gate that failed
// on them would make the bundle's own documentation unwritable.
func TestVerifyIgnoresLinksInsideCode(t *testing.T) {
	root, _, g := write(t)
	edit(t, root, "modules/internal-auth.md", func(s string) string {
		return s + "\nInline: `[x](/modules/nope.md)`.\n\n```\n[y](/modules/alsonope.md)\n```\n"
	})

	res := verifyBundle(t, root, g)
	if !res.OK() {
		t.Errorf("a link inside code was reported as broken:%s", findings(res))
	}
}

// A relative link is genuinely ambiguous — a human writing `../src/main.go` means a file in
// the repository — so it is left unchecked. Unchecked and *counted*: a silently skipped link
// is a hole in the gate nobody can see.
func TestVerifyCountsRelativeLinksAsUnchecked(t *testing.T) {
	root, _, g := write(t)
	edit(t, root, "modules/internal-auth.md", func(s string) string {
		return s + "\nSee [the source](../../internal/auth/auth.go).\n"
	})

	res := verifyBundle(t, root, g)
	if !res.OK() {
		t.Fatalf("a relative link failed verification:%s", findings(res))
	}
	if len(res.Skipped) == 0 {
		t.Error("the unchecked relative link was not reported")
	}
}

func TestVerifyFailsOnAPageWithNoFrontmatter(t *testing.T) {
	root, _, g := write(t)
	const page = "references/loose.md"
	full := filepath.Join(root, BundleDir, filepath.FromSlash(page))
	if err := os.WriteFile(full, []byte("Just markdown.\n"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", page, err)
	}

	res := verifyBundle(t, root, g)
	if !has(res.Findings, FindingConformance, page) {
		t.Errorf("want a conformance finding on %s, got:%s", page, findings(res))
	}
}

func TestVerifyFailsOnAnEmptyType(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	edit(t, root, page, func(s string) string {
		return strings.Replace(s, "type: Module", "type:", 1)
	})

	res := verifyBundle(t, root, g)
	if !has(res.Findings, FindingConformance, page) {
		t.Errorf("want a conformance finding on %s, got:%s", page, findings(res))
	}
}

// Both directions of the reserved-name rule. A second page typed `Index` gives a consumer
// two roots and no way to choose, which is worse than having none.
func TestVerifyChecksReservedFilenamesBothWays(t *testing.T) {
	t.Run("reserved name with the wrong type", func(t *testing.T) {
		root, _, g := write(t)
		edit(t, root, IndexPage, func(s string) string {
			return strings.Replace(s, "type: Index", "type: Module", 1)
		})
		res := verifyBundle(t, root, g)
		if !has(res.Findings, FindingConformance, IndexPage) {
			t.Errorf("want a conformance finding on %s, got:%s", IndexPage, findings(res))
		}
	})
	t.Run("another page claiming a reserved type", func(t *testing.T) {
		root, _, g := write(t)
		const page = "modules/internal-auth.md"
		edit(t, root, page, func(s string) string {
			return strings.Replace(s, "type: Module", "type: Index", 1)
		})
		res := verifyBundle(t, root, g)
		if !has(res.Findings, FindingConformance, page) {
			t.Errorf("want a conformance finding on %s, got:%s", page, findings(res))
		}
	})
}

// An orphan page is a warning, not a failure. bundle.go never deletes a page, so this is
// the litter that rule leaves behind: failing on it would turn every rename into a red CI
// job with no supported way to fix it, and that gate gets disabled.
func TestVerifyWarnsButDoesNotFailOnAnOrphanPage(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/renamed-away.md"
	src := read(t, root, "modules/internal-auth.md")
	full := filepath.Join(root, BundleDir, filepath.FromSlash(page))
	if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
		t.Fatalf("writing %s: %v", page, err)
	}

	res := verifyBundle(t, root, g)
	if !res.OK() {
		t.Fatalf("an orphan page failed verification:%s", findings(res))
	}
	if !has(res.Warnings, FindingOrphanPage, page) {
		t.Errorf("want an orphan warning on %s, got:%s", page, findings(res))
	}
}

// A downgraded verification is a warning too: the bundle is correct and what it needs is a
// reviewer, not a rebuild. Reported because a mark nobody surfaces is a mark nobody acts on.
func TestVerifyWarnsOnADowngradedVerification(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	edit(t, root, page, func(s string) string {
		return strings.Replace(s, "---\n",
			"---\nverified:\n  - { by: human:ecopelan, at: 2026-07-30, resource: \"git://old@1111111\" }\n", 1)
	})
	// The build is what writes the status mark; verify reads it.
	if _, err := Write(root, g, demoOptions()); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	res := verifyBundle(t, root, g)
	if !res.OK() {
		t.Fatalf("a downgraded verification failed verification:%s", findings(res))
	}
	if !has(res.Warnings, FindingStaleVerification, page) {
		t.Errorf("want a stale-verification warning on %s, got:%s", page, findings(res))
	}
}

func TestVerifyFailsOnAMissingPage(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	if err := os.Remove(filepath.Join(root, BundleDir, filepath.FromSlash(page))); err != nil {
		t.Fatalf("removing %s: %v", page, err)
	}

	res := verifyBundle(t, root, g)
	if !has(res.Findings, FindingMissingPage, page) {
		t.Errorf("want a missing-page finding on %s, got:%s", page, findings(res))
	}
}

func TestVerifyFailsOnAnUnparseableManifest(t *testing.T) {
	root, _, g := write(t)
	edit(t, root, ManifestFile, func(string) string { return "{not json" })

	res := verifyBundle(t, root, g)
	if !has(res.Findings, FindingConformance, ManifestFile) {
		t.Errorf("want a conformance finding on the manifest, got:%s", findings(res))
	}
}

// Findings come out in a stable order, because a verify whose output reordered between runs
// is unreviewable in a CI diff.
func TestVerifyFindingsAreOrderedStably(t *testing.T) {
	root, _, g := write(t)
	for _, rel := range []string{"modules/internal-auth.md", "modules/internal-storage.md",
		"modules/api-gateway.md"} {
		edit(t, root, rel, func(s string) string {
			return s + "\nSee [gone](/modules/deleted.md).\n"
		})
	}

	first := findings(verifyBundle(t, root, g))
	for i := 0; i < 3; i++ {
		if got := findings(verifyBundle(t, root, g)); got != first {
			t.Fatalf("findings reordered between runs:\nfirst:%s\nthen:%s", first, got)
		}
	}
}

func TestBundleRelRejectsAPathEscapingTheBundle(t *testing.T) {
	cases := map[string]struct {
		want string
		ok   bool
	}{
		"/modules/a.md":          {"modules/a.md", true},
		"/index.md#heading":      {"index.md", true},
		"/modules/../index.md":   {"index.md", true},
		"/../outside.md":         {"", false},
		"modules/a.md":           {"", false},
		"/modules/./b.md":        {"modules/b.md", true},
		"/modules/../../away.md": {"", false},
	}
	for in, want := range cases {
		got, ok := bundleRel(in)
		if ok != want.ok || got != want.want {
			t.Errorf("bundleRel(%q) = %q, %v; want %q, %v", in, got, ok, want.want, want.ok)
		}
	}
}

func TestStripCodeSpansLeavesLengthAndUnterminatedRuns(t *testing.T) {
	cases := map[string]string{
		"a `[x](/y.md)` b":    "a `          ` b",
		"``a ` b`` tail":      "``     `` tail",
		"unterminated ` here": "unterminated ` here",
		"no code at all":      "no code at all",
	}
	for in, want := range cases {
		if got := stripCodeSpans(in); got != want {
			t.Errorf("stripCodeSpans(%q) = %q, want %q", in, got, want)
		}
		if got := stripCodeSpans(in); len(got) != len(in) {
			t.Errorf("stripCodeSpans(%q) changed length: %d to %d", in, len(in), len(got))
		}
	}
}

// The tests below cover AsOfBundle, and the property they exist to protect is that it
// relaxes provenance without relaxing the staleness check. Both directions are asserted on
// every one of them: a bundle whose only difference is the commit passes, and a bundle whose
// content differs still fails.

// The case that forces the mode to exist. A bundle built at one commit and verified at a
// later one, with no change to the code in between, is what every branch and every pull
// request looks like — the bundle is built on the default branch only (§8.0), so its stamp is
// older by construction and a strict verify calls every page stale.
func TestVerifyAsOfBundlePassesWhenOnlyTheCommitMoved(t *testing.T) {
	root, _, g := write(t)

	// What verifying at a later commit means: the same tree, a different sha. Nothing on
	// disk is touched, so any finding below is about provenance and nothing else.
	later := demoOptions()
	later.Resource = "git://example.com/repo@deadbee"
	later.Date = "2026-08-15"

	strict, err := Verify(root, g, later)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if strict.OK() {
		t.Fatal("strict verify passed at a later commit, so this test proves nothing about " +
			"what AsOfBundle changes")
	}

	later.AsOfBundle = true
	res, err := Verify(root, g, later)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Fatalf("a bundle whose only difference is the commit did not verify:%s", findings(res))
	}
	// The pass covered the whole bundle. A relaxation that also stopped checking pages would
	// look identical from the exit code alone.
	if nodes, _ := g.Counts(); res.Checked.Pages != nodes+2 {
		t.Errorf("checked %d pages, want %d: the mode must relax provenance, not coverage",
			res.Checked.Pages, nodes+2)
	}
	// Announced, never silent. This is the check whose quiet success would destroy the
	// tool's value, so the run that relaxes it says which commit it judged against.
	joined := strings.Join(res.Skipped, " ")
	if !strings.Contains(joined, "provenance") {
		t.Errorf("the adoption was not reported as skipped: %v", res.Skipped)
	}
	if !strings.Contains(joined, "8f2a1c9") {
		t.Errorf("the skip does not name the commit that was judged against: %v", res.Skipped)
	}
}

// The property that makes the mode safe to enable on every pull request: it is still a gate.
func TestVerifyAsOfBundleStillFailsOnStaleContent(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	// A change to the description is what a code change looks like by the time it reaches a
	// page: the same node, different counted facts.
	edit(t, root, page, func(s string) string {
		return strings.Replace(s, "description:", "description: stale ", 1)
	})

	later := demoOptions()
	later.Resource = "git://example.com/repo@deadbee"
	later.AsOfBundle = true

	res, err := Verify(root, g, later)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK() {
		t.Fatalf("stale content passed under AsOfBundle, so the mode is not a gate:%s",
			findings(res))
	}
	if !has(res.Findings, FindingOutOfDate, page) {
		t.Errorf("want an out-of-date finding on %s, got:%s", page, findings(res))
	}
}

// A missing page is a content difference, not a provenance one. This is the shape a pull
// request adding a package takes, and it must fail: the bundle genuinely does not describe
// the repository any more.
func TestVerifyAsOfBundleFailsOnAMissingPage(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	if err := os.Remove(filepath.Join(root, BundleDir, filepath.FromSlash(page))); err != nil {
		t.Fatal(err)
	}

	opts := demoOptions()
	opts.AsOfBundle = true
	res, err := Verify(root, g, opts)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !has(res.Findings, FindingMissingPage, page) {
		t.Errorf("want a missing-page finding on %s, got:%s", page, findings(res))
	}
}

// A bundle that cannot say what it describes adopts nothing, and the strict comparison it
// falls back to then reports the manifest. Inventing provenance to make the gate pass is the
// false pass this file exists to prevent, so the mode must not become an escape hatch for a
// bundle with no record at all.
func TestVerifyAsOfBundleDoesNotRescueAnUnreadableManifest(t *testing.T) {
	root, _, g := write(t)
	edit(t, root, ManifestFile, func(string) string { return "{ not json" })

	opts := demoOptions()
	opts.AsOfBundle = true
	res, err := Verify(root, g, opts)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK() {
		t.Fatalf("an unparseable manifest passed under AsOfBundle:%s", findings(res))
	}
	if !has(res.Findings, FindingConformance, ManifestFile) {
		t.Errorf("want a conformance finding on the manifest, got:%s", findings(res))
	}
}

// The other half of that guard, and the one an implementation is most likely to get wrong.
// A manifest that parses but records no resource has no provenance to adopt, and adopting an
// empty one would blank every page's `resource:` — which would then match a fresh render
// that also has none, turning a bundle that cannot say what it describes into a pass. The
// strict comparison has to stay in place and report it.
func TestVerifyAsOfBundleDoesNotRescueAResourcelessManifest(t *testing.T) {
	root, _, g := write(t)
	edit(t, root, ManifestFile, func(src string) string {
		var man map[string]any
		if err := json.Unmarshal([]byte(src), &man); err != nil {
			t.Fatal(err)
		}
		man["resource"] = ""
		out, err := json.Marshal(man)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	})

	opts := demoOptions()
	opts.AsOfBundle = true
	res, err := Verify(root, g, opts)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK() {
		t.Fatalf("a manifest recording no resource passed under AsOfBundle:%s", findings(res))
	}
	// stale-resource rather than conformance: the file parsed, so the defect is what it
	// records, not whether it can be read. Conformance is reserved for a manifest no reader
	// can get a field out of at all.
	if !has(res.Findings, FindingStaleResource, ManifestFile) {
		t.Errorf("want a stale-resource finding on the manifest, got:%s", findings(res))
	}
	// Nothing was adopted, so nothing is announced. A skip naming an empty commit would be
	// exactly the false reassurance this test exists to rule out.
	for _, s := range res.Skipped {
		if strings.Contains(s, "provenance") {
			t.Errorf("adopted provenance from a manifest that has none: %q", s)
		}
	}
}
