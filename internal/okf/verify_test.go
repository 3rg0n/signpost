package okf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// TestVerifyPassesOnACRLFCheckout is the false-staleness defect from the gate's side, and it
// is the worse half: `build` churning is noise, but this makes verify report a bundle as
// out of date when it is byte-identical to what a build would produce.
//
// The remedy the failure names does not work, which is what makes it more than a wrong
// message. "run `signpost build` and commit the result" writes LF, git converts it back to
// CRLF on the next checkout, and the gate is red again — so a user following the instruction
// exactly ends up where they started with no way to tell why. On CI this fails the build for
// every page in the bundle on a pull request that changed nothing.
func TestVerifyPassesOnACRLFCheckout(t *testing.T) {
	root, res, g := write(t)
	for _, rel := range res.Written {
		if strings.HasSuffix(rel, ".md") {
			toCRLF(t, root, rel)
		}
	}

	got := verifyBundle(t, root, g)
	if !got.OK() {
		t.Fatalf("a CRLF checkout of an up-to-date bundle failed verification. Line endings "+
			"are how the platform's git stored the file, not what the bundle says:%s",
			findings(got))
	}
	// The pass has to be a real one. A fix that made readBundle return nothing would also
	// produce OK() here, and it would be a false pass of exactly the kind §4.6 forbids.
	if got.Checked.Pages == 0 || got.Checked.Edges == 0 || got.Checked.Links == 0 {
		t.Errorf("checked %d pages, %d edges, %d links — a pass over nothing is not a pass",
			got.Checked.Pages, got.Checked.Edges, got.Checked.Links)
	}
}

// TestVerifyStillFailsOnRealDriftInACRLFCheckout is the mutation the fix above must survive.
//
// Normalising line endings must not cost the check its teeth. This edits the *content* of a
// page in a CRLF bundle, which is a real difference a build would change, and the gate has
// to still catch it — otherwise the fix for a false negative bought a false positive, which
// is the trade this whole file exists to refuse.
func TestVerifyStillFailsOnRealDriftInACRLFCheckout(t *testing.T) {
	root, res, g := write(t)
	for _, rel := range res.Written {
		if strings.HasSuffix(rel, ".md") {
			toCRLF(t, root, rel)
		}
	}
	// Inside a managed region, which is the drift this check exists to find: a build would
	// regenerate this text, so the committed page says something the graph does not.
	edit(t, root, "modules/internal-auth.md", func(s string) string {
		return strings.Replace(s, "JWT validation and PAT issuance.",
			"Not what the graph says.", 1)
	})

	got := verifyBundle(t, root, g)
	if got.OK() {
		t.Error("a CRLF bundle whose content actually drifted passed verification; " +
			"normalising line endings must not make the byte comparison toothless")
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
		return strings.Replace(s, "to: ./internal-storage.md", "to: ./deleted.md", 1)
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

// TestVerifyFailsOnUnparseableFrontmatter is the half of issue #9 that let it reach a commit.
//
// The emitter wrote an unquoted `[` into a flow mapping and the checker read the page, warned,
// and exited 0. That is the false pass verify exists to prevent (§4.6): a bundle everyone trusts
// and nothing validates is confidently wrong.
//
// The page below still parses as a mapping — `type` and `title` come back intact, because the
// fault is in the middle of the document and everything before it is fine. That is why the
// Malformed check has to run before the mapping check, and why a test that only asserted "the
// frontmatter is not a mapping" would pass on the bug.
func TestVerifyFailsOnUnparseableFrontmatter(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	edit(t, root, page, func(s string) string {
		return strings.Replace(s, "\n---\n",
			"\ntags: [go, security\n---\n", 1)
	})

	res := verifyBundle(t, root, g)
	if res.OK() {
		t.Fatalf("a page whose frontmatter no conforming reader can read passed verification. "+
			"This is issue #9: the emitter wrote it, verify read it, and the disagreement was "+
			"reported at a severity that let CI go green.%s", findings(res))
	}
	if !has(res.Findings, FindingConformance, page) {
		t.Errorf("want a conformance *finding* on %s, not a warning, got:%s", page, findings(res))
	}
}

// TestVerifyWarnsButDoesNotFailOnTolerableFrontmatter is the other side of that severity split.
//
// A construct the tolerant reader stepped over is ADR 0001 working as designed — the document is
// valid YAML and every reader agrees with what was read. Failing on it would fail builds over a
// human's hand-edit, and a gate that fires on legitimate input is a gate somebody turns off.
// Asserted here so that a future change tightening the Malformed check cannot quietly take this
// with it.
func TestVerifyWarnsButDoesNotFailOnTolerableFrontmatter(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	edit(t, root, page, func(s string) string {
		// A tab-indented block: something this reader records and steps over, and something a
		// conforming parser also rejects for indentation — but not a truncated document.
		return strings.Replace(s, "\n---\n", "\nnotes:\n\tkept: by hand\n---\n", 1)
	})

	res := verifyBundle(t, root, g)
	if !res.OK() {
		t.Fatalf("a tolerated construct failed verification, which fails a build over a "+
			"hand-edit ADR 0001 says to tolerate:%s", findings(res))
	}
	if !has(res.Warnings, FindingConformance, page) {
		t.Errorf("want a conformance warning on %s, got:%s", page, findings(res))
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

// An orphan page a build would delete is a failure — the half of issue #10 that closes the
// hole. A page for a module that is not there carries plausible edges and a resource stamp
// naming a commit where the code really did exist, so it reads as authoritative; before this it
// survived both gates, with build silent and verify exiting 0.
//
// The severity is a failure specifically because the remedy is the one every other failure here
// names: run build. That is the property the pair of tests is asserting — not "orphans are bad"
// but "the severity says whether a command can fix it".
func TestVerifyFailsOnAnOrphanPageABuildWouldDelete(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/renamed-away.md"
	// A page signpost wrote, copied under a name no node has. Nothing on it came from a person,
	// so sweepStale removes it.
	src := read(t, root, "modules/internal-auth.md")
	full := filepath.Join(root, BundleDir, filepath.FromSlash(page))
	if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
		t.Fatalf("writing %s: %v", page, err)
	}

	res := verifyBundle(t, root, g)
	if !has(res.Findings, FindingOrphanPage, page) {
		t.Fatalf("want an orphan failure on %s, got:%s", page, findings(res))
	}
	if res.OK() {
		t.Error("verify passed with a page describing a concept the repository does not have")
	}
}

// An orphan page a build would *keep* is a warning, because no command resolves it: somebody's
// text is on it and only they can say where it belongs. Failing would be a red gate whose only
// fix is editing files by hand, which is the gate that gets switched off on the first rename.
func TestVerifyWarnsOnAnOrphanPageABuildWouldKeep(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/renamed-away.md"
	src := read(t, root, "modules/internal-auth.md") + "\nThis moved to internal/identity.\n"
	full := filepath.Join(root, BundleDir, filepath.FromSlash(page))
	if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
		t.Fatalf("writing %s: %v", page, err)
	}

	res := verifyBundle(t, root, g)
	if !res.OK() {
		t.Fatalf("a written-on orphan page failed verification:%s", findings(res))
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
	// Keyed by the page the link is written on and the target, because a page-relative
	// target cannot be resolved without both — which is the whole change here.
	cases := map[[2]string]struct {
		want string
		ok   bool
	}{
		// Bundle-absolute. Still resolved, because a bundle on disk from before the move to
		// relative links is full of these and verify has to report it as stale rather than as
		// eighty broken links.
		{"index.md", "/modules/a.md"}:            {"modules/a.md", true},
		{"index.md", "/index.md#heading"}:        {"index.md", true},
		{"modules/a.md", "/modules/../index.md"}: {"index.md", true},
		{"index.md", "/../outside.md"}:           {"", false},
		{"index.md", "/modules/./b.md"}:          {"modules/b.md", true},
		{"index.md", "/modules/../../away.md"}:   {"", false},

		// Page-relative, the form relTarget writes now. Resolved against the linking page's
		// directory, so the same target text means different files on different pages.
		{"modules/a.md", "./b.md"}:          {"modules/b.md", true},
		{"modules/a.md", "../index.md"}:     {"index.md", true},
		{"index.md", "./modules/a.md"}:      {"modules/a.md", true},
		{"modules/a.md", "./b.md#heading"}:  {"modules/b.md", true},
		{"modules/a.md", "../modules/c.md"}: {"modules/c.md", true},

		// Negative boundaries. Without these a resolver that returned ok for everything
		// would pass every case above, and "the link resolved" would stop meaning anything.
		{"modules/a.md", "../../outside.md"}: {"", false}, // walks out of the bundle
		{"index.md", "../outside.md"}:        {"", false}, // one level up from the root
		{"index.md", "modules/a.md"}:         {"", false}, // bare: ambiguous, not ours
		{"index.md", "main.go"}:              {"", false}, // a repository file in a note
		{"index.md", "https://x.example/a"}:  {"", false}, // not a bundle target at all
		{"index.md", "#heading"}:             {"", false}, // same-page fragment, no file
		{"index.md", "./"}:                   {"", false}, // a directory is not a page
	}
	for in, want := range cases {
		from, target := in[0], in[1]
		got, ok := bundleRel(from, target)
		if ok != want.ok || got != want.want {
			t.Errorf("bundleRel(%q, %q) = %q, %v; want %q, %v",
				from, target, got, ok, want.want, want.ok)
		}
	}
}

// relTarget is the other half of the same contract: what the emitter writes has to be what
// bundleRel reads back. Asserted as a round trip rather than only on the rendered string,
// because the two live in different files and a change to either alone is the failure.
func TestRelTargetRoundTripsThroughBundleRel(t *testing.T) {
	pages := []string{
		"index.md", "log.md", "practices.md",
		"modules/a.md", "modules/b.md", "references/x.md",
		"modules/nested/deep.md",
	}
	for _, from := range pages {
		for _, to := range pages {
			target := relTarget(from, to)
			if !isRelTarget(target) {
				t.Errorf("relTarget(%q, %q) = %q, which is not explicitly relative: "+
					"bundleLinks would count it as unchecked and the gate would not see it",
					from, to, target)
			}
			got, ok := bundleRel(from, target)
			if !ok || got != to {
				t.Errorf("relTarget(%q, %q) = %q, which bundleRel reads back as %q, %v",
					from, to, target, got, ok)
			}
		}
	}
}

// A link in a generated region is signpost's own and must be checked; the same text in
// human prose must not be. That asymmetry is what keeps the gate honest without failing a
// build over somebody's note, and it is the one thing that would have silently emptied the
// link check when the emitter moved to relative targets.
func TestBundleLinksChecksRelativeTargetsOnlyInGeneratedRegions(t *testing.T) {
	const text = "See [b](./b.md) and [src](../../internal/auth/auth.go).\n"

	managed, mSkipped := bundleLinks(text, true)
	if len(managed) != 2 || mSkipped != 0 {
		t.Errorf("in a generated region: checked %v, skipped %d; want both links checked",
			managed, mSkipped)
	}

	human, hSkipped := bundleLinks(text, false)
	if len(human) != 0 || hSkipped != 2 {
		t.Errorf("in human prose: checked %v, skipped %d; want both left unchecked — a note "+
			"naming a repository file is not a bundle link", human, hSkipped)
	}

	// Bundle-absolute is checked in both: nothing outside a bundle is named that way.
	for _, m := range []bool{true, false} {
		got, _ := bundleLinks("See [a](/modules/a.md).\n", m)
		if len(got) != 1 {
			t.Errorf("managed=%v: absolute target not checked: %v", m, got)
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

// Content a rebuild would change is reported and does not fail — on a branch, and only there.
//
// This test used to assert the opposite, and the assertion was the defect. A page whose bytes a
// build would rewrite is what adding a module or editing a doc comment *looks like* by the time
// it reaches the bundle, and §8.0 forbids the branch from running the build that would fix it. So
// the failure named a remedy the author was not allowed to apply, on nearly every pull request,
// until red meant nothing. Both halves below are the property that replaced it: reported in full,
// and still a failure on the run that writes the bundle.
func TestVerifyAsOfBundleDefersContentARebuildWillRewrite(t *testing.T) {
	root, _, g := write(t)
	const page = "modules/internal-auth.md"
	// A change to the description is what a code change looks like by the time it reaches a
	// page: the same node, different counted facts.
	edit(t, root, page, func(s string) string {
		return strings.Replace(s, "description:", "description: stale ", 1)
	})

	later := demoOptions()
	later.Resource = "git://example.com/repo@deadbee"

	// Strict first, because it is what makes the relaxation below a distinction rather than a
	// hole. The default branch is where signpost writes these bytes, so there is no later
	// rebuild to defer to and the same difference is a defect.
	strict, err := Verify(root, g, later)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !has(strict.Findings, FindingOutOfDate, page) {
		t.Fatalf("a strict verify did not fail on stale content, so this test proves nothing "+
			"about what AsOfBundle defers:%s", findings(strict))
	}

	later.AsOfBundle = true
	res, err := Verify(root, g, later)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Errorf("content a rebuild resolves failed the branch gate:%s", findings(res))
	}
	// Deferred, never dropped. A gate that silently swallowed a page it decided was somebody
	// else's problem would be the false pass this file exists to prevent, arriving through the
	// exit code instead of the output.
	if !has(res.Pending, FindingOutOfDate, page) {
		t.Errorf("want a pending out-of-date finding on %s, got:%s", page, findings(res))
	}
	if has(res.Findings, FindingOutOfDate, page) {
		t.Errorf("%s is both pending and failing, so the split moved nothing", page)
	}
}

// A missing page is the same event from the other side: the branch added a concept the bundle has
// no page for. Deferred on a branch, because writing that page is the merge's job, and a failure
// on the run that writes it.
func TestVerifyAsOfBundleDefersAMissingPage(t *testing.T) {
	root, _, g := write(t)
	// Added to the graph after the bundle was written, rather than deleted from disk. That is
	// what a branch adding a package *is*, and it isolates the finding: deleting a page instead
	// would also dangle every link to it, which is a different severity and is asserted below.
	const page = "modules/late-arrival.md"
	if err := g.AddNode(&graph.Node{
		ID: "/modules/late-arrival", Kind: graph.KindModule, Title: "late/arrival",
		Path: "late/arrival",
	}); err != nil {
		t.Fatal(err)
	}

	opts := demoOptions()
	strict, err := Verify(root, g, opts)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !has(strict.Findings, FindingMissingPage, page) {
		t.Fatalf("a strict verify did not fail on a missing page:%s", findings(strict))
	}

	opts.AsOfBundle = true
	res, err := Verify(root, g, opts)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !has(res.Pending, FindingMissingPage, page) {
		t.Errorf("want a pending missing-page finding on %s, got:%s", page, findings(res))
	}
}

// The negative boundary the whole split lives or dies by: a bundle that is broken *now* stays a
// failure on a branch, because the merge inherits it rather than repairing it. Every kind here is
// one a rebuild does not resolve, and every one of them shares a page with a difference the
// rebuild does resolve — so a classifier keying off the page, or off the message text, or off
// "anything under -as-of-bundle", passes the tests above and fails these.
func TestVerifyAsOfBundleStillFailsOnWhatTheMergeCannotFix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    FindingKind
		page    string
		breakIt func(t *testing.T, root string)
	}{
		{
			// Deleting a module page leaves index.md pointing at a path that is not there. The
			// missing page itself is pending (above); the dangling link is not, because after the
			// merge the link is still dangling in every checkout of this branch's bundle.
			name: "a link naming a page the bundle does not contain",
			kind: FindingBrokenLink,
			page: IndexPage,
			breakIt: func(t *testing.T, root string) {
				rel := filepath.FromSlash("modules/internal-auth.md")
				if err := os.Remove(filepath.Join(root, BundleDir, rel)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "frontmatter no conforming reader can parse",
			kind: FindingConformance,
			page: "modules/internal-auth.md",
			breakIt: func(t *testing.T, root string) {
				edit(t, root, "modules/internal-auth.md", func(s string) string {
					return strings.Replace(s, "type:", "type: [unterminated\nkey:", 1)
				})
			},
		},
		{
			name: "the bundle deleted outright",
			kind: FindingMissingBundle,
			page: "",
			breakIt: func(t *testing.T, root string) {
				if err := os.RemoveAll(filepath.Join(root, BundleDir)); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _, g := write(t)
			tc.breakIt(t, root)

			opts := demoOptions()
			opts.AsOfBundle = true
			res, err := Verify(root, g, opts)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if res.OK() {
				t.Fatalf("the branch gate passed on a bundle the merge cannot fix:%s", findings(res))
			}
			if !has(res.Findings, tc.kind, tc.page) {
				t.Errorf("want a failing %s finding on %q, got:%s", tc.kind, tc.page, findings(res))
			}
			if has(res.Pending, tc.kind, tc.page) {
				t.Errorf("%s on %q was deferred to a rebuild that does not resolve it",
					tc.kind, tc.page)
			}
		})
	}
}

// Pending exists on a branch and nowhere else. Asserted over every kind at once rather than one
// per case, because the hole this rules out is a classifier that runs unconditionally: the strict
// verify is the run that *writes* the bundle, so a difference it deferred would be written to
// main and never looked at again.
func TestVerifyPendingIsEmptyWithoutAsOfBundle(t *testing.T) {
	root, _, g := write(t)
	edit(t, root, "modules/internal-auth.md", func(s string) string {
		return strings.Replace(s, "description:", "description: stale ", 1)
	})
	if err := g.AddNode(&graph.Node{
		ID: "/modules/late-arrival", Kind: graph.KindModule, Title: "late/arrival",
		Path: "late/arrival",
	}); err != nil {
		t.Fatal(err)
	}

	res := verifyBundle(t, root, g)
	if len(res.Pending) != 0 {
		t.Errorf("a strict verify deferred %d finding(s) to a rebuild it is itself performing: %v",
			len(res.Pending), res.Pending)
	}
	if res.OK() {
		t.Errorf("a strict verify passed on both a stale page and a missing one:%s", findings(res))
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

// The invariant behind issue #10's third item, asserted on a bundle nobody has touched: the page
// list a consumer is invited to read *instead of* walking the directory names exactly the pages
// that were written.
//
// Run with and without a practices page, because the conditional page is where the real defect
// was and the unconditional ones would have passed throughout. Every bundle this repository
// produced left practices.md out of `pages` — 32 listed against 33 on disk — and verify was green
// on all of them: checkUpToDate compares the manifest against a fresh render of *itself*, so both
// sides agreed on the same wrong list. Only a comparison against the page set catches an emitter
// that is wrong the same way every time.
func TestManifestListsEveryPageWritten(t *testing.T) {
	for _, practices := range []string{"", "## Build\n\n`go test ./...`\n"} {
		name := "without a practices page"
		if practices != "" {
			name = "with a practices page"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			g, _ := demoGraph(t)
			opts := demoOptions()
			opts.Practices = practices
			res, err := Write(root, g, opts)
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			man, err := readManifest(filepath.Join(root, BundleDir))
			if err != nil {
				t.Fatalf("readManifest: %v", err)
			}

			var want []string
			for _, rel := range res.Written {
				if strings.HasSuffix(rel, ".md") {
					want = append(want, rel)
				}
			}
			if strings.Join(man.Pages, "\n") != strings.Join(want, "\n") {
				t.Errorf("manifest lists %d page(s) and the build wrote %d:\n listed: %v\nwritten: %v",
					len(man.Pages), len(want), man.Pages, want)
			}
			// Named as well as covered by the comparison above, in both directions. This is the
			// page that was actually missing, so a refactor of pageList that drops it again should
			// fail on a message saying which page rather than on a diff of two lists.
			if got := slices.Contains(man.Pages, PracticesPage); got != (practices != "") {
				t.Errorf("%s listed = %v, want %v: %v", PracticesPage, got, practices != "", man.Pages)
			}
		})
	}
}

// A page list that disagrees with what a build writes is a failure, in both directions. Named
// separately because the two are different defects for a consumer: a page it will never open, and
// a path it will try to open and not find.
func TestVerifyFailsOnAManifestPageListThatDisagrees(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func([]string) []string
		want   string
	}{
		{
			name:   "a page written and not listed",
			mutate: func(p []string) []string { return p[1:] },
			want:   "was written and is not listed",
		},
		{
			name:   "a page listed and not written",
			mutate: func(p []string) []string { return append(p, "modules/never-written.md") },
			want:   "is listed and was not written",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _, g := write(t)
			edit(t, root, ManifestFile, func(src string) string {
				var man map[string]any
				if err := json.Unmarshal([]byte(src), &man); err != nil {
					t.Fatal(err)
				}
				var pages []string
				for _, p := range man["pages"].([]any) {
					pages = append(pages, p.(string))
				}
				man["pages"] = tc.mutate(pages)
				out, err := json.Marshal(man)
				if err != nil {
					t.Fatal(err)
				}
				return string(out)
			})

			res := verifyBundle(t, root, g)
			if !has(res.Findings, FindingPageList, ManifestFile) {
				t.Fatalf("want a page-list finding on the manifest, got:%s", findings(res))
			}
			if !strings.Contains(findings(res), tc.want) {
				t.Errorf("no finding says %q, so the reader is not told which way it is wrong:%s",
					tc.want, findings(res))
			}
			// Its own kind rather than a conformance finding, and this is why: on a branch the
			// list is short by exactly the pages the branch adds, which is the rebuild's job,
			// where unparseable frontmatter on the same file is not. Severity has to follow the
			// kind, so the two cannot share one.
			asOf := demoOptions()
			asOf.AsOfBundle = true
			branch, err := Verify(root, g, asOf)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if !has(branch.Pending, FindingPageList, ManifestFile) {
				t.Errorf("want the page list deferred on a branch, got:%s", findings(branch))
			}
		})
	}
}
