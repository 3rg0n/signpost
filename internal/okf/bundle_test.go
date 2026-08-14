package okf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/graph"
	"github.com/3rg0n/signpost/internal/manifest"
)

// parseFrontmatter reads emitted frontmatter with the tolerant parser, returning the
// diagnostic as a string so a caller can assert it was empty.
func parseFrontmatter(t *testing.T, fm string) (*manifest.Node, string) {
	t.Helper()
	n, diag := manifest.ParseYAMLDoc(fm)
	if diag.Incomplete() {
		return n, diag.Summary()
	}
	if n == nil {
		return nil, "parsed to nothing"
	}
	return n, ""
}

// write emits the demo bundle into a fresh temp root.
func write(t *testing.T) (root string, res *Result, g *graph.Graph) {
	t.Helper()
	root = t.TempDir()
	g, _ = demoGraph(t)
	res, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	return root, res, g
}

// read returns a bundle file's bytes as a string.
func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, BundleDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(b)
}

func TestWriteProducesEveryReservedFileAndOnePagePerNode(t *testing.T) {
	root, res, g := write(t)

	nodes, _ := g.Counts()
	if len(res.Written) != nodes+3 {
		t.Errorf("wrote %d files, want %d nodes + index + log + manifest", len(res.Written), nodes)
	}
	if res.Created != len(res.Written) {
		t.Errorf("Created = %d, want every file on a first run (%d)", res.Created, len(res.Written))
	}
	for _, rel := range []string{IndexPage, LogPage, ManifestFile, "modules/internal-auth.md"} {
		if read(t, root, rel) == "" {
			t.Errorf("%s is empty", rel)
		}
	}
	// Sorted, so a consumer of Result can diff two runs.
	for i := 1; i < len(res.Written); i++ {
		if res.Written[i-1] > res.Written[i] {
			t.Fatalf("Written is not sorted: %v", res.Written)
		}
	}
}

// Property one: byte-stable. Same graph and same commit in, identical bytes out. ADR 0005
// commits the bundle, so nondeterminism here is commit churn in someone else's repository.
func TestWriteIsByteStableAcrossRuns(t *testing.T) {
	root, _, g := write(t)
	before := snapshot(t, root)

	res, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("second Write: %v", err)
	}
	after := snapshot(t, root)

	if len(before) != len(after) {
		t.Fatalf("file set changed: %d then %d", len(before), len(after))
	}
	for rel, want := range before {
		if after[rel] != want {
			t.Errorf("%s changed on a re-run:\n got %q\nwant %q", rel, after[rel], want)
		}
	}
	if res.Updated != 0 || res.Created != 0 {
		t.Errorf("second run reported %d created, %d updated; want all unchanged",
			res.Created, res.Updated)
	}
	if res.Unchanged != len(before) {
		t.Errorf("Unchanged = %d, want %d", res.Unchanged, len(before))
	}
}

// A fresh graph value with the same content produces the same bytes. Distinct from the test
// above, which re-writes from one graph: this one catches a map iteration order leaking into
// output, which a re-write of the same object would not.
func TestWriteIsStableAcrossDistinctGraphValues(t *testing.T) {
	rootA := t.TempDir()
	gA, _ := demoGraph(t)
	if _, err := Write(rootA, gA, demoOptions()); err != nil {
		t.Fatalf("Write A: %v", err)
	}
	rootB := t.TempDir()
	gB, _ := demoGraph(t)
	if _, err := Write(rootB, gB, demoOptions()); err != nil {
		t.Fatalf("Write B: %v", err)
	}
	a, b := snapshot(t, rootA), snapshot(t, rootB)
	if len(a) != len(b) {
		t.Fatalf("file counts differ: %d vs %d", len(a), len(b))
	}
	for rel, want := range a {
		if b[rel] != want {
			t.Errorf("%s differs between two builds of the same graph:\n got %q\nwant %q",
				rel, b[rel], want)
		}
	}
}

// Property two: human regions survive. A run that clobbered a correction would teach people
// to delete the tool.
func TestWritePreservesHumanEditsAcrossARebuild(t *testing.T) {
	root, _, g := write(t)
	const rel = "modules/internal-auth.md"

	page := read(t, root, rel)
	const note = "\n## Notes\n\nRate limiting lives in the gateway. Read the 2025 incident review first.\n"
	edited := page + note
	if err := os.WriteFile(filepath.Join(root, BundleDir, filepath.FromSlash(rel)),
		[]byte(edited), 0o644); err != nil {
		t.Fatalf("editing the page: %v", err)
	}

	res, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	got := read(t, root, rel)
	if !strings.Contains(got, "Read the 2025 incident review first.") {
		t.Fatalf("the human note was lost:\n%s", got)
	}
	if res.Preserved == 0 {
		t.Error("Preserved = 0; the run that carried a human edit did not report it")
	}
	// And the note is stable: a third run must not duplicate or move it.
	if _, err := Write(root, g, demoOptions()); err != nil {
		t.Fatalf("third run: %v", err)
	}
	third := read(t, root, rel)
	if third != got {
		t.Errorf("a human-edited page is not byte-stable:\n got %q\nwant %q", third, got)
	}
	if n := strings.Count(third, "Read the 2025 incident review first."); n != 1 {
		t.Errorf("the note appears %d times, want 1", n)
	}
}

// toCRLF rewrites a bundle file the way git materialises it under core.autocrlf=true.
//
// The mechanism that produces the checkout signpost has to read: a repository storing LF
// blobs, cloned on Windows by a git configured to convert on checkout. No test can call git
// to do this — the conversion depends on the developer's own git config, so a test that
// relied on it would pass or fail based on the machine rather than the code.
func toCRLF(t *testing.T, root, rel string) {
	t.Helper()
	full := filepath.Join(root, BundleDir, filepath.FromSlash(rel))
	b, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	crlf := strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n", "\r\n")
	if err := os.WriteFile(full, []byte(crlf), 0o644); err != nil {
		t.Fatalf("writing %s: %v", rel, err)
	}
}

// TestWriteTreatsACRLFCheckoutAsUnchanged is the false-staleness defect, from the build side.
//
// Reachable with no unusual setup at all: commit a bundle, clone the repository on a Windows
// machine whose git has core.autocrlf=true — a default many Windows installs select — and
// build. Every page differs from a rebuild on every line, so nothing is recognised as
// unchanged and the whole bundle is rewritten each run.
//
// This repository ships a .gitattributes pinning `* text=auto eol=lf`, which is precisely why
// signpost's own CI could never catch this: the bug is invisible in a repository already
// configured against it, and every repository is unconfigured on the first day signpost runs
// in it.
func TestWriteTreatsACRLFCheckoutAsUnchanged(t *testing.T) {
	root, res, g := write(t)
	for _, rel := range res.Written {
		if strings.HasSuffix(rel, ".md") {
			toCRLF(t, root, rel)
		}
	}

	again, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if again.Updated != 0 {
		t.Errorf("Updated = %d on a CRLF checkout of an up-to-date bundle, want 0. "+
			"Line endings are a transport encoding, not content: every page differs from "+
			"the generated LF text on every line, so the whole bundle is rewritten each run",
			again.Updated)
	}
	// The half that is worse than the churn. This count exists to tell a user their writing
	// was kept, so reporting it for a bundle nobody edited teaches them it means nothing.
	if again.Preserved != 0 {
		t.Errorf("Preserved = %d on a bundle with no human notes, want 0. "+
			"HumanText() differed only by its line endings, so the run claimed to have "+
			"carried across notes nobody wrote", again.Preserved)
	}
}

// TestWriteStillPreservesRealNotesOnACRLFPage is the other side of the check above, and it is
// the one that would catch a fix that went too far.
//
// Normalising line endings on read must not make signpost blind to a real edit, and a Windows
// editor writes that edit in CRLF. A fix that compared normalised text but then wrote the
// generated page wholesale would pass the test above and silently delete this note.
func TestWriteStillPreservesRealNotesOnACRLFPage(t *testing.T) {
	root, _, g := write(t)
	const rel = "modules/internal-auth.md"

	// Trailing spaces included deliberately: page.go's invariant is that human text is
	// never trimmed, re-indented, or reflowed, and a normalisation that reached past line
	// endings into the rest of the text would take these with it.
	const note = "\r\n## Notes\r\n\r\nLoad-bearing. Trailing spaces:   \r\n"
	toCRLF(t, root, rel)
	full := filepath.Join(root, BundleDir, filepath.FromSlash(rel))
	b, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if err := os.WriteFile(full, append(b, []byte(note)...), 0o644); err != nil {
		t.Fatalf("editing: %v", err)
	}

	res, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	got := read(t, root, rel)
	if !strings.Contains(got, "Load-bearing.") {
		t.Fatalf("a human note written in CRLF was lost:\n%s", got)
	}
	// Asserted against the CRLF form, because the page is left exactly as it was found. The
	// merged text matches what is already on disk once line endings are set aside, so
	// writeIfChanged declines to write and the file keeps the endings its owner's git chose.
	// signpost normalises to *compare*; it does not convert a file it has no reason to touch.
	if !strings.Contains(got, "Trailing spaces:   \r\n") {
		t.Errorf("trailing whitespace was stripped from human text; normalisation reached "+
			"past line endings into the text itself:\n%q", got)
	}
	if res.Preserved != 1 {
		t.Errorf("Preserved = %d, want exactly 1 — the page that was actually edited",
			res.Preserved)
	}
}

// A managed region a human overwrote is regenerated — that is the half of the contract they
// do not own, and the page would otherwise assert a structure the graph no longer has.
func TestWriteRegeneratesManagedRegionsOverAHumanEdit(t *testing.T) {
	root, _, g := write(t)
	const rel = "modules/internal-auth.md"

	page := read(t, root, rel)
	tampered := strings.ReplaceAll(page, "internal/storage", "WRONG")
	if tampered == page {
		t.Fatal("test setup: nothing to tamper with")
	}
	if err := os.WriteFile(filepath.Join(root, BundleDir, filepath.FromSlash(rel)),
		[]byte(tampered), 0o644); err != nil {
		t.Fatalf("tampering: %v", err)
	}
	if _, err := Write(root, g, demoOptions()); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	got := read(t, root, rel)
	if strings.Contains(got, "WRONG") {
		t.Errorf("a tampered managed region was not regenerated:\n%s", got)
	}
	if got != page {
		t.Errorf("the regenerated page does not match the original:\n got %q\nwant %q", got, page)
	}
}

// The property the whole semantic design rests on, and the only reason `role` is a region of
// its own: a deterministic rebuild — which §8 runs on every push — carries the scheduled
// semantic pass's prose across untouched. Merge keeps a managed region it finds on disk but
// does not render, which is the same mechanism the log page uses to accumulate history.
//
// If this test fails, the semantic pass is writing prose that survives exactly one push.
func TestWriteCarriesARoleRegionAcrossADeterministicRebuild(t *testing.T) {
	root := t.TempDir()
	g, n := demoGraph(t)

	semantic := demoOptions()
	semantic.Roles = map[string]string{
		n.ID: "Issues and validates the tokens every other service checks.\n\n" +
			"_Summary by `model/x`, from `internal/auth/auth.go`. Not reviewed by a human._\n",
	}
	if _, err := Write(root, g, semantic); err != nil {
		t.Fatalf("semantic Write: %v", err)
	}
	const rel = "modules/internal-auth.md"
	withRole := read(t, root, rel)
	if !strings.Contains(withRole, "Issues and validates the tokens") {
		t.Fatalf("the role prose was not written:\n%s", withRole)
	}

	// Now the deterministic run: no Roles at all, exactly as `signpost build` without
	// -semantic produces.
	res, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("deterministic rebuild: %v", err)
	}
	after := read(t, root, rel)
	if after != withRole {
		t.Errorf("a deterministic rebuild changed a page carrying a role region:\n got %q\nwant %q",
			after, withRole)
	}
	// The attribution line matters as much as the prose. A role region that survived without
	// it would read as a fact signpost counted.
	if !strings.Contains(after, "Not reviewed by a human.") {
		t.Errorf("the attribution line was lost:\n%s", after)
	}
	if res.Updated != 0 {
		t.Errorf("the rebuild reported %d updated page(s); the bundle should be unchanged", res.Updated)
	}
	// And it is stable, not merely surviving once: a third and fourth run must not duplicate
	// the region or move it.
	for i := 0; i < 2; i++ {
		if _, err := Write(root, g, demoOptions()); err != nil {
			t.Fatalf("rebuild %d: %v", i+3, err)
		}
	}
	if third := read(t, root, rel); third != withRole {
		t.Errorf("a page with a role region is not byte-stable across repeated builds:\n got %q\nwant %q",
			third, withRole)
	}
	if c := strings.Count(read(t, root, rel), "signpost:managed:role"); c != 2 {
		t.Errorf("found %d role markers, want an open and a close", c)
	}
}

// A semantic run over a bundle that already has human notes must not disturb them, and the
// notes must not be relocated by the region Merge appends. This is the two mechanisms —
// preservation and accumulation — meeting on one page.
func TestWriteAddsARoleRegionWithoutDisturbingHumanNotes(t *testing.T) {
	root, _, g := write(t)
	const rel = "modules/internal-auth.md"
	full := filepath.Join(root, BundleDir, filepath.FromSlash(rel))

	const note = "\nRate limiting lives in the gateway, not here.\n"
	if err := os.WriteFile(full, []byte(read(t, root, rel)+note), 0o644); err != nil {
		t.Fatalf("editing the page: %v", err)
	}

	_, n := demoGraph(t)
	semantic := demoOptions()
	semantic.Roles = map[string]string{n.ID: "Issues and validates tokens.\n"}
	if _, err := Write(root, g, semantic); err != nil {
		t.Fatalf("semantic Write: %v", err)
	}

	got := read(t, root, rel)
	if !strings.Contains(got, "Rate limiting lives in the gateway") {
		t.Errorf("the human note was lost when a role region was added:\n%s", got)
	}
	if !strings.Contains(got, "Issues and validates tokens.") {
		t.Errorf("the role prose was not added:\n%s", got)
	}
	// The note stays under the human's own heading rather than being pushed below generated
	// prose that arrived after it.
	if strings.Index(got, "## Notes") > strings.Index(got, "Rate limiting lives") {
		t.Errorf("the note was moved out from under its heading:\n%s", got)
	}
}

// A role region does not make verify report the page as out of date. It cannot, since neither
// the fresh render nor the merge produces one — but this is the check that would fail first if
// the merge rule underneath it changed, and a red staleness gate on every push is the failure
// that would get the semantic pass turned off.
func TestVerifyPassesOnABundleCarryingRoleRegions(t *testing.T) {
	root := t.TempDir()
	g, n := demoGraph(t)
	semantic := demoOptions()
	semantic.Roles = map[string]string{n.ID: "Issues and validates tokens.\n"}
	if _, err := Write(root, g, semantic); err != nil {
		t.Fatalf("semantic Write: %v", err)
	}

	// Verified with the deterministic options a CI run would use — no Roles.
	res, err := Verify(root, g, demoOptions())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Fatalf("a bundle with role regions did not verify:%s", findings(res))
	}
}

// Property three: nothing executable, nothing secret. The bundle is committed and often
// published, and a generator that wrote a script into a repository would be a supply-chain
// hazard rather than a knowledge artifact.
func TestWriteEmitsOnlyMarkdownAndJSON(t *testing.T) {
	root, _, _ := write(t)
	for rel, content := range snapshot(t, root) {
		switch {
		case strings.HasSuffix(rel, ".md"), strings.HasSuffix(rel, ".json"):
		default:
			t.Errorf("bundle contains %s, which is neither markdown nor JSON", rel)
		}
		// A shebang or an HTML script tag is the shape an executable payload takes in a file
		// a browser or a shell might read. Neither has any business in a knowledge artifact.
		if strings.HasPrefix(content, "#!") {
			t.Errorf("%s begins with a shebang", rel)
		}
		if strings.Contains(strings.ToLower(content), "<script") {
			t.Errorf("%s contains a script tag", rel)
		}
	}
	// And the files are not executable.
	err := filepath.WalkDir(filepath.Join(root, BundleDir), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o111 != 0 {
			t.Errorf("%s is executable (mode %v)", p, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the bundle: %v", err)
	}
}

// A page is written into the directory its node ID names, so an OKF link resolves as a
// relative path on disk.
func TestWriteCreatesDirectoriesFromNodeIDs(t *testing.T) {
	root, _, _ := write(t)
	for _, rel := range []string{
		"modules/internal-auth.md",
		"interfaces/things-jwt.md",
		"references/golang-org-x-crypto.md",
	} {
		if _, err := os.Stat(filepath.Join(root, BundleDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s not written: %v", rel, err)
		}
	}
}

// Node IDs come from repository directory names, which is untrusted input. assemble's slug
// strips the characters that would escape the bundle; this is the second gate, in a different
// package, so a change to one cannot silently break the property.
func TestWriteRefusesAPagePathThatEscapesTheBundle(t *testing.T) {
	bad := []string{
		"/modules/../../etc/passwd",
		"../outside",
		"/modules/",
		"",
	}
	for _, id := range bad {
		g := graph.New()
		if err := g.AddNode(&graph.Node{ID: id, Kind: graph.KindModule, Title: "x"}); err != nil {
			// An empty ID is refused by the graph itself, which is the outer gate working.
			continue
		}
		root := t.TempDir()
		if _, err := Write(root, g, demoOptions()); err == nil {
			t.Errorf("Write accepted node ID %q", id)
		}
		// And nothing was written outside the bundle.
		if _, err := os.Stat(filepath.Join(root, "etc")); err == nil {
			t.Errorf("node ID %q wrote outside the bundle", id)
		}
	}
}

func TestCheckPageRel(t *testing.T) {
	ok := []string{"index.md", "modules/auth.md", "a/b/c.md"}
	for _, rel := range ok {
		if err := checkPageRel(rel); err != nil {
			t.Errorf("checkPageRel(%q) = %v, want nil", rel, err)
		}
	}
	bad := []string{"", "index", "modules/auth.txt", "../a.md", "a/../b.md", "/a.md", "a//b.md", "a/./b.md"}
	for _, rel := range bad {
		if err := checkPageRel(rel); err == nil {
			t.Errorf("checkPageRel(%q) = nil, want an error", rel)
		}
	}
}

// A page whose node no longer exists and which somebody has written on is reported, never
// deleted. A renamed directory or a regressed extractor must not silently take a human's notes
// with it, which is the half of issue #10 that argues against pruning at all.
func TestWriteReportsStalePagesWithoutDeletingThem(t *testing.T) {
	root, _, g := write(t)
	stalePath := filepath.Join(root, BundleDir, "modules", "removed.md")
	if err := os.WriteFile(stalePath, []byte("---\nx: 1\n---\nmy notes\n"), 0o644); err != nil {
		t.Fatalf("writing the stale page: %v", err)
	}

	res, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if strings.Join(res.Stale, ",") != "modules/removed.md" {
		t.Errorf("Stale = %v, want [modules/removed.md]", res.Stale)
	}
	if got, err := os.ReadFile(stalePath); err != nil || string(got) != "---\nx: 1\n---\nmy notes\n" {
		t.Errorf("the stale page was modified or deleted: %q, %v", got, err)
	}
	if len(res.Removed) != 0 {
		t.Errorf("Removed = %v, want nothing: the page carried text nobody generated", res.Removed)
	}
}

// The other half of issue #10: a page whose concept is gone and which holds nothing but the
// skeleton a first emit wrote *is* deleted, and the run names it.
//
// Both halves are needed and neither is sufficient. Without this one, a bundle keeps a page
// describing a module that is not there — with plausible edges and a resource stamp naming a
// commit where the code really did exist — indefinitely, and every gate stays green. Without
// the one above, the first rename destroys somebody's notes. The test between them is whether
// anything on the page came from a person.
func TestWriteRemovesAnUnwrittenStalePage(t *testing.T) {
	root, _, g := write(t)
	const rel = "modules/ghost.md"
	full := filepath.Join(root, BundleDir, filepath.FromSlash(rel))
	// A page signpost itself wrote, copied. Constructing one by hand would be asserting against
	// this test's idea of a skeleton rather than against the emitter's.
	if err := os.WriteFile(full, []byte(read(t, root, "modules/internal-auth.md")), 0o644); err != nil {
		t.Fatalf("writing %s: %v", rel, err)
	}

	res, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if strings.Join(res.Removed, ",") != rel {
		t.Errorf("Removed = %v, want [%s]", res.Removed, rel)
	}
	if len(res.Stale) != 0 {
		t.Errorf("Stale = %v, want nothing: the page held only what the emitter wrote", res.Stale)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Errorf("%s is still on disk: %v", rel, err)
	}
}

// prunable is the whole of the delete/keep decision, so it is asserted directly as well as
// through Write. Every case here is a way a page can carry something a person put there, and
// each one must fall toward keeping it — the failure this guards is a single wrong answer
// deleting a paragraph nobody can get back without git.
func TestPrunableKeepsAnythingAPersonTouched(t *testing.T) {
	root, _, _ := write(t)
	skeleton := read(t, root, "modules/internal-auth.md")
	if !prunable(skeleton) {
		t.Fatalf("a page exactly as the emitter wrote it is not prunable, so nothing ever is:\n%s",
			skeleton)
	}

	keep := map[string]string{
		"a note under Notes":     skeleton + "\nRate limiting lives in the gateway.\n",
		"a note before the body": strings.Replace(skeleton, "\n# ", "\nMine.\n\n# ", 1),
		"a human `verified:` block": strings.Replace(skeleton, "---\n",
			"---\nverified:\n  - { by: human:ecopelan, at: 2026-07-30 }\n", 1),
		"an unrecognised frontmatter key": strings.Replace(skeleton, "---\n",
			"---\nowner: platform-team\n", 1),
		// Not a page signpost wrote. A markdown file somebody dropped in the bundle directory is
		// not signpost's to delete no matter what the graph says.
		"no frontmatter":    "# Scratch\n\nSomething I was drafting.\n",
		"no managed region": "---\ntype: Module\n---\n# Notes\n\nWhatever this is.\n",
		"an empty file":     "",
		"frontmatter only":  "---\ntype: Module\n---\n",
		"a broken open marker": strings.Replace(skeleton,
			"<!-- signpost:managed:summary -->", "<!-- signpost:managed:summary", 1),
	}
	for what, src := range keep {
		if prunable(src) {
			t.Errorf("a page with %s is prunable, so a rebuild deletes it:\n%s", what, src)
		}
	}
}

// cache/ is gitignored and content-hash keyed. Skipped entirely rather than reported as
// thousands of stale files.
func TestFindStaleSkipsTheCacheDirectory(t *testing.T) {
	root, _, g := write(t)
	cache := filepath.Join(root, BundleDir, "cache", "ab12")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cache, "summary.md"), []byte("cached\n"), 0o644); err != nil {
		t.Fatalf("writing the cache entry: %v", err)
	}
	res, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(res.Stale) != 0 {
		t.Errorf("Stale = %v, want nothing from cache/", res.Stale)
	}
}

// Two nodes rendering to one path would mean one silently overwrote the other, producing a
// bundle describing fewer things than the graph contains. assemble's collision resolution is
// what prevents it; this asserts the emitter notices if it ever fails.
func TestWriteRefusesDuplicatePagePaths(t *testing.T) {
	// A graph cannot hold two nodes with one ID, so the collision is built at the path
	// level: a leading slash is trimmed when the page path is formed, so these two IDs
	// differ while their pages do not.
	g := graph.New()
	for _, id := range []string{"/modules/auth", "modules/auth"} {
		if err := g.AddNode(&graph.Node{ID: id, Kind: graph.KindModule, Title: "auth"}); err != nil {
			t.Fatalf("AddNode(%s): %v", id, err)
		}
	}
	if _, err := Write(t.TempDir(), g, demoOptions()); err == nil {
		t.Error("Write accepted two nodes rendering to one page path")
	} else if !strings.Contains(err.Error(), "both render to") {
		t.Errorf("error = %v, want it to name the collision", err)
	}
}

// A human verification of a different commit is reported so a reviewer knows to look again.
func TestWriteReportsADowngradedVerification(t *testing.T) {
	root, _, g := write(t)
	const rel = "modules/internal-auth.md"
	full := filepath.Join(root, BundleDir, filepath.FromSlash(rel))

	page := read(t, root, rel)
	reviewed := strings.Replace(page, "---\ntype: Module\n",
		"---\nverified:\n  - { by: human:ecopelan, at: 2026-07-29, resource: git://example.com/repo@0000000/internal/auth }\ntype: Module\n", 1)
	if reviewed == page {
		t.Fatal("test setup: frontmatter shape changed")
	}
	if err := os.WriteFile(full, []byte(reviewed), 0o644); err != nil {
		t.Fatalf("writing the reviewed page: %v", err)
	}

	res, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if strings.Join(res.Downgraded, ",") != rel {
		t.Errorf("Downgraded = %v, want [%s]", res.Downgraded, rel)
	}
	// The block itself is kept: the reviewer's name and date are the audit trail.
	got := read(t, root, rel)
	if !strings.Contains(got, "human:ecopelan") {
		t.Errorf("the verified block was dropped:\n%s", got)
	}
	// And the page says so itself. The bundle is read by people and agents that never ran
	// signpost, so a downgrade recorded only in the run's stdout is one nobody acts on.
	if !strings.Contains(got, statusKey+": "+statusStaleVerification) {
		t.Errorf("the page carries no stale-verification status:\n%s", got)
	}
	// And it is signpost's key, not OKF's. §5.4 enumerates `status` as draft|stable|deprecated,
	// so writing our finding there would put an out-of-vocabulary value on a spec-owned key.
	// Anchored to a line start, because `signpost_status:` contains `status:` as a substring —
	// an unanchored check passes on the very output it is meant to reject.
	if strings.Contains(got, "\nstatus:") {
		t.Errorf("the finding was written to the spec's status key:\n%s", got)
	}
	// Written into the generated half, above the human's block, so the two do not interleave.
	if strings.Index(got, statusKey+":") > strings.Index(got, "verified:") {
		t.Errorf("status was written below the human keys:\n%s", got)
	}
	if _, diag := parseFrontmatter(t, ParsePage(got).Frontmatter); diag != "" {
		t.Errorf("a downgraded page's frontmatter is unreadable: %s", diag)
	}
}

// The mark clears itself. `status` is a generated key, so a page whose verification comes to
// match the resource again loses the mark on the next run with nobody editing it — which is
// what makes the downgrade recoverable in one step rather than leaving a scar.
func TestWriteClearsAStaleVerificationStatusWhenItMatchesAgain(t *testing.T) {
	root, _, g := write(t)
	const rel = "modules/internal-auth.md"
	full := filepath.Join(root, BundleDir, filepath.FromSlash(rel))

	// A review of an older commit: downgraded, and the page is marked.
	page := read(t, root, rel)
	stale := strings.Replace(page, "---\ntype: Module\n",
		"---\nverified:\n  - { by: human:ecopelan, at: 2026-07-29, resource: git://example.com/repo@0000000/internal/auth }\ntype: Module\n", 1)
	if err := os.WriteFile(full, []byte(stale), 0o600); err != nil {
		t.Fatalf("writing the reviewed page: %v", err)
	}
	if _, err := Write(root, g, demoOptions()); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	if got := read(t, root, rel); !strings.Contains(got, statusKey+": "+statusStaleVerification) {
		t.Fatalf("test setup: the page was not marked:\n%s", got)
	}

	// The reviewer re-checks, recording the resource the page now describes.
	marked := read(t, root, rel)
	fixed := strings.Replace(marked,
		"resource: git://example.com/repo@0000000/internal/auth",
		"resource: "+demoOptions().Resource+"/internal/auth", 1)
	if fixed == marked {
		t.Fatal("test setup: the reviewed resource was not replaced")
	}
	if err := os.WriteFile(full, []byte(fixed), 0o600); err != nil {
		t.Fatalf("writing the re-reviewed page: %v", err)
	}

	res, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	if len(res.Downgraded) != 0 {
		t.Errorf("Downgraded = %v, want nothing", res.Downgraded)
	}
	got := read(t, root, rel)
	if strings.Contains(got, statusKey+":") {
		t.Errorf("the stale-verification status survived a matching review:\n%s", got)
	}
	if !strings.Contains(got, "human:ecopelan") {
		t.Errorf("the verified block was dropped while clearing the status:\n%s", got)
	}
}

// A marked page is byte-stable. The status is generated, so a second run at the same commit
// must reproduce it exactly rather than inserting a second copy — the failure a line-based
// insert makes easy and ADR 0005 makes expensive.
func TestWriteIsStableWhileAVerificationIsDowngraded(t *testing.T) {
	root, _, g := write(t)
	const rel = "modules/internal-auth.md"
	full := filepath.Join(root, BundleDir, filepath.FromSlash(rel))
	page := read(t, root, rel)
	stale := strings.Replace(page, "---\ntype: Module\n",
		"---\nverified:\n  - { by: human:ecopelan, at: 2026-07-29, resource: git://example.com/repo@0000000/x }\ntype: Module\n", 1)
	if err := os.WriteFile(full, []byte(stale), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := Write(root, g, demoOptions()); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	first := read(t, root, rel)
	if _, err := Write(root, g, demoOptions()); err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	if second := read(t, root, rel); second != first {
		t.Errorf("a downgraded page is not byte-stable:\n got %q\nwant %q", second, first)
	}
	if n := strings.Count(first, statusKey+":"); n != 1 {
		t.Errorf("%d status lines, want exactly 1:\n%s", n, first)
	}
}

func TestWithStatusPlacesTheKeyInSpecOrder(t *testing.T) {
	// After the keys the spec puts before `status`, and before those that follow, per §3.1.
	got := withStatus("type: Module\ntitle: x\ntags: [go]\ngenerated: { by: a, at: \"b\" }\n", "s")
	want := "type: Module\ntitle: x\ntags: [go]\n" + statusKey + ": s\ngenerated: { by: a, at: \"b\" }\n"
	if got != want {
		t.Errorf("withStatus =\n%q\nwant\n%q", got, want)
	}
	// Nothing follows it: appended rather than dropped.
	if got := withStatus("type: Module\n", "s"); got != "type: Module\n"+statusKey+": s\n" {
		t.Errorf("withStatus with no trailing keys = %q", got)
	}
	// A multi-line block belonging to a preceding key keeps its lines with that key rather
	// than having the status inserted into the middle of it.
	got = withStatus("tags: [go]\nedges:\n  - { kind: imports, to: /a.md }\n", "s")
	want = "tags: [go]\n" + statusKey + ": s\nedges:\n  - { kind: imports, to: /a.md }\n"
	if got != want {
		t.Errorf("withStatus across a block =\n%q\nwant\n%q", got, want)
	}
	// A human's spec `status:` is one of the keys signpost's line goes *after*, so the two sit
	// adjacent in spec order rather than signpost's landing above a lifecycle value it does
	// not own. It is also carried, not overwritten — see TestCarryHumanKeysKeepsASpecStatus.
	got = withStatus("type: Module\nstatus: deprecated\ngenerated: { by: a, at: \"b\" }\n", "s")
	want = "type: Module\nstatus: deprecated\n" + statusKey + ": s\ngenerated: { by: a, at: \"b\" }\n"
	if got != want {
		t.Errorf("withStatus beside a spec status =\n%q\nwant\n%q", got, want)
	}
}

// Idempotent. No caller can currently pass frontmatter already carrying signpost's status —
// the emitter never writes the key — but two such lines in a committed file would leave a
// YAML reader taking the second, so the function must not rely on that.
func TestWithStatusReplacesAnExistingStatus(t *testing.T) {
	got := withStatus("type: Module\n"+statusKey+": something-else\ngenerated: { by: a, at: \"b\" }\n", "s")
	want := "type: Module\n" + statusKey + ": s\ngenerated: { by: a, at: \"b\" }\n"
	if got != want {
		t.Errorf("withStatus over an existing status =\n%q\nwant\n%q", got, want)
	}
	if n := strings.Count(got, statusKey+":"); n != 1 {
		t.Errorf("%d status lines, want 1", n)
	}
	// And repeated application is a no-op beyond the first.
	if again := withStatus(got, "s"); again != got {
		t.Errorf("withStatus is not idempotent:\n got %q\nwant %q", again, got)
	}
	// A multi-line status block is dropped whole, not left with orphaned continuation lines
	// that would attach themselves to the key above.
	got = withStatus("type: Module\n"+statusKey+":\n  - a\n  - b\nedges:\n  - x\n", "s")
	want = "type: Module\n" + statusKey + ": s\nedges:\n  - x\n"
	if got != want {
		t.Errorf("withStatus over a block status =\n%q\nwant\n%q", got, want)
	}
	// The negative boundary: the spec's own key is a different key and must survive untouched,
	// including a multi-line value. Replacing it would let signpost overwrite a lifecycle
	// value a human set, which is the failure ADR 0021 exists to prevent.
	got = withStatus("type: Module\nstatus: deprecated\nedges:\n  - x\n", "s")
	want = "type: Module\nstatus: deprecated\n" + statusKey + ": s\nedges:\n  - x\n"
	if got != want {
		t.Errorf("withStatus consumed the spec's status =\n%q\nwant\n%q", got, want)
	}
}

// A verification of the resource the page currently describes stands. This is the case that
// must not report — an earlier version compared against the bundle's base URI instead of the
// page's own resource, which reported every reviewed page as downgraded on every run.
func TestWriteDoesNotDowngradeAVerificationOfTheCurrentResource(t *testing.T) {
	root, _, g := write(t)
	const rel = "modules/internal-auth.md"
	full := filepath.Join(root, BundleDir, filepath.FromSlash(rel))

	page := read(t, root, rel)
	res := demoOptions().Resource + "/internal/auth"
	reviewed := strings.Replace(page, "---\ntype: Module\n",
		"---\nverified:\n  - { by: human:ecopelan, at: 2026-07-30, resource: "+res+" }\ntype: Module\n", 1)
	if err := os.WriteFile(full, []byte(reviewed), 0o644); err != nil {
		t.Fatalf("writing the reviewed page: %v", err)
	}

	out, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(out.Downgraded) != 0 {
		t.Errorf("Downgraded = %v, want nothing", out.Downgraded)
	}
}

// A machine confirmation is never downgraded: it is regenerated alongside the page.
func TestWriteDoesNotDowngradeAMachineConfirmation(t *testing.T) {
	root, _, g := write(t)
	const rel = "modules/internal-auth.md"
	full := filepath.Join(root, BundleDir, filepath.FromSlash(rel))
	page := read(t, root, rel)
	reviewed := strings.Replace(page, "---\ntype: Module\n",
		"---\nverified:\n  - { by: signpost/0.0.9, at: 2020-01-01 }\ntype: Module\n", 1)
	if err := os.WriteFile(full, []byte(reviewed), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := Write(root, g, demoOptions())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if len(out.Downgraded) != 0 {
		t.Errorf("Downgraded = %v, want nothing", out.Downgraded)
	}
}

// The index is what an agent is pointed at: grouped by concern with a described line per
// concept, so it can pick the three pages it needs instead of reading the bundle.
func TestIndexPageGroupsByKindWithHubsFirst(t *testing.T) {
	root, _, _ := write(t)
	idx := read(t, root, IndexPage)

	if !strings.Contains(idx, "okf_version: \"0.2\"") {
		t.Errorf("index does not declare okf_version:\n%s", idx)
	}
	iHubs := strings.Index(idx, "### Most connected")
	iModules := strings.Index(idx, "### Modules")
	if iHubs < 0 || iModules < 0 {
		t.Fatalf("index sections missing:\n%s", idx)
	}
	if iHubs > iModules {
		t.Error("hubs are not listed first")
	}
	// Fixed section order: modules and services before the reference material they depend on.
	iRefs := strings.Index(idx, "### External dependencies")
	if iRefs > 0 && iRefs < iModules {
		t.Error("external dependencies were listed above modules")
	}
	// One described line per concept, with a page-relative target: the index sits at the
	// bundle root, so a page under modules/ is `./modules/...` from it.
	if !strings.Contains(idx, "[internal/storage](./modules/internal-storage.md) — Token table.") {
		t.Errorf("index line is not described:\n%s", idx)
	}
	// The negative half. A root-absolute target resolves against the web server root, so on
	// GitHub — where ADR 0005 expects the bundle to be read with nothing installed —
	// `/modules/x.md` is a 404. None may survive anywhere on the page.
	if strings.Contains(idx, "](/") {
		t.Errorf("the index carries a root-absolute link, which 404s on GitHub:\n%s", idx)
	}
}

// A node with no edges is not a hub. Listing an unconnected page under "most connected"
// would be actively misleading.
func TestIndexHubsExcludeUnconnectedNodes(t *testing.T) {
	root, _, _ := write(t)
	idx := read(t, root, IndexPage)
	hubs := idx[strings.Index(idx, "### Most connected"):]
	// Bounded at the findings section rather than at "### Modules", because the findings sit
	// between the two and name the orphan on purpose — as an unconnected concept, which is
	// the opposite claim.
	if end := strings.Index(hubs, "### Structural findings"); end > 0 {
		hubs = hubs[:end]
	}
	if strings.Contains(hubs, "/modules/orphan.md") {
		t.Errorf("an unconnected node was listed as a hub:\n%s", hubs)
	}
}

// Every structural finding is named whether or not it has anything to report. This is the
// half that made the analysis useful only to whoever ran the command: a section that vanishes
// when the answer is "none" is indistinguishable from a section a build failed to write, so a
// reader cannot tell a clean repository from a broken generator without running the tool.
func TestIndexFindingsNameEveryResultIncludingTheAbsentOnes(t *testing.T) {
	root, _, _ := write(t)
	idx := read(t, root, IndexPage)

	if !strings.Contains(idx, "### Structural findings") {
		t.Fatalf("the index carries no findings section:\n%s", idx)
	}
	// Between the hubs and the page listing: the findings are about the repository, and the
	// lines below them are navigation.
	iHubs := strings.Index(idx, "### Most connected")
	iFindings := strings.Index(idx, "### Structural findings")
	iModules := strings.Index(idx, "### Modules")
	if iHubs >= iFindings || iFindings >= iModules {
		t.Errorf("findings are not between the hubs and the page listing (%d, %d, %d)",
			iHubs, iFindings, iModules)
	}
	// The demo graph is clean on three of the four, so three of these assert an absence is
	// *written down* rather than omitted.
	for _, want := range []string{
		"**Import cycles: none.**",
		"**Cross-cluster edges:",
		"**Disconnected islands: none.**",
		"**Unconnected concepts: 1.**",
	} {
		if !strings.Contains(idx, want) {
			t.Errorf("findings do not state %q:\n%s", want, idx)
		}
	}
	// The finding links to the page, so a reader can act on it without going back to the
	// graph. Page-relative, for the reason the section above this one gives.
	if !strings.Contains(idx, "[orphan](./modules/orphan.md)") {
		t.Errorf("the unconnected concept is named but not linked:\n%s", idx)
	}
	if strings.Contains(idx, "](/") {
		t.Errorf("a finding carries a root-absolute link, which 404s on GitHub:\n%s", idx)
	}
}

// findingsGraph has one of each finding: two import cycles in two communities joined by a
// single edge, a two-node island, and an orphan.
//
// Two triangles joined by one edge is the shape ADR 0019 names, and it is here for the same
// reason: it is the smallest graph whose Louvain partition is genuinely two clusters, which is
// what makes the cross-cluster count non-zero and therefore assertable.
func findingsGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g := graph.New()
	add := func(id, title string, k graph.Kind) {
		if err := g.AddNode(&graph.Node{ID: id, Kind: k, Title: title}); err != nil {
			t.Fatalf("AddNode(%s): %v", id, err)
		}
	}
	ring := func(ids ...string) {
		for i, from := range ids {
			g.AddEdge(graph.Edge{
				From: from, To: ids[(i+1)%len(ids)], Kind: graph.EdgeImports,
				Conf: graph.Extracted,
			})
		}
	}
	for _, n := range []string{"a1", "a2", "a3", "b1", "b2", "b3", "lonely"} {
		add("/modules/"+n, n, graph.KindModule)
	}
	add("/references/doc-one", "doc one", graph.KindDocument)
	add("/references/doc-two", "doc two", graph.KindDocument)

	ring("/modules/a1", "/modules/a2", "/modules/a3")
	ring("/modules/b1", "/modules/b2", "/modules/b3")
	// The one edge between the two communities.
	g.AddEdge(graph.Edge{
		From: "/modules/a1", To: "/modules/b1", Kind: graph.EdgeImports, Conf: graph.Extracted,
	})
	// Linked to each other and to nothing else: an island, not two orphans.
	g.AddEdge(graph.Edge{
		From: "/references/doc-one", To: "/references/doc-two", Kind: graph.EdgeDocuments,
		Conf: graph.Extracted,
	})
	return g
}

// The positive half: each finding reports what it found, with the concepts named.
//
// The cross-cluster assertion is the load-bearing one. Bridges() reads the assignment
// Clusters() writes and returns an empty slice when it has not run, so an emitter that
// skipped Clusters() would write "Cross-cluster edges: none." over a graph that has one —
// a confident wrong finding, which is worse than a missing section. Only a graph whose
// partition is really two clusters can catch that, which is what findingsGraph is for.
func TestIndexFindingsReportWhatTheyFound(t *testing.T) {
	root := t.TempDir()
	g := findingsGraph(t)
	if _, err := Write(root, g, demoOptions()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	idx := read(t, root, IndexPage)

	for _, want := range []string{
		// Two directed triangles are two strongly connected components.
		"**Import cycles: 2.**",
		"3 modules: [a1](./modules/a1.md), [a2](./modules/a2.md), [a3](./modules/a3.md)",
		"**Cross-cluster edges: 1.**",
		"[a1](./modules/a1.md) → [b1](./modules/b1.md) (imports)",
		"**Disconnected islands: 1.**",
		"2 concepts: [doc one](./references/doc-one.md), [doc two](./references/doc-two.md)",
		"**Unconnected concepts: 1.**",
		"[lonely](./modules/lonely.md)",
	} {
		if !strings.Contains(idx, want) {
			t.Errorf("findings do not state %q:\n%s", want, idx)
		}
	}
	// The negatives, so a run that emitted every line unconditionally with a hardcoded "none"
	// would fail rather than read as clean.
	for _, unwanted := range []string{
		"Import cycles: none",
		"Cross-cluster edges: none",
		"Disconnected islands: none",
		"Unconnected concepts: none",
	} {
		if strings.Contains(idx, unwanted) {
			t.Errorf("findings claim %q on a graph that has one:\n%s", unwanted, idx)
		}
	}
	// A single-node component is an orphan and is reported as one. Counting it as an island
	// too would report the same absence as two different problems.
	if strings.Contains(idx, "1 concept: [lonely]") {
		t.Errorf("the orphan was also counted as an island:\n%s", idx)
	}
}

// A long finding is bounded, and the bound says so. An index listing four thousand
// cross-cluster edges is one nobody reads, and a truncated list that did not name its
// overflow would read as a complete one — which is the failure the count exists to prevent.
func TestIndexFindingsBoundLongListsAndNameTheOverflow(t *testing.T) {
	root := t.TempDir()
	g := graph.New()
	const orphans = maxFindingItems + 7
	for i := range orphans {
		id := "/modules/n" + strconv.Itoa(i)
		if err := g.AddNode(&graph.Node{ID: id, Kind: graph.KindModule, Title: id}); err != nil {
			t.Fatalf("AddNode(%s): %v", id, err)
		}
	}
	if _, err := Write(root, g, demoOptions()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	idx := read(t, root, IndexPage)

	if !strings.Contains(idx, "**Unconnected concepts: "+strconv.Itoa(orphans)+".**") {
		t.Errorf("the full count is not stated:\n%s", idx)
	}
	if !strings.Contains(idx, "and 7 more") {
		t.Errorf("the truncated tail is not counted, so the list reads as complete:\n%s", idx)
	}
}

// pipelineGraph holds CI jobs, some of which gate a change and some of which cannot.
func pipelineGraph(t *testing.T, gates ...string) *graph.Graph {
	t.Helper()
	g := graph.New()
	for _, id := range []string{"test", "lint", "nightly-scan", "release"} {
		n := &graph.Node{ID: "/pipelines/" + id, Kind: graph.KindPipeline, Title: id}
		for _, gate := range gates {
			if gate == id {
				n.Tags = []string{"gate"}
			}
		}
		if err := g.AddNode(n); err != nil {
			t.Fatalf("AddNode(%s): %v", id, err)
		}
	}
	return g
}

// Design §4.1 asks a workflow for "what gates exist", and this finding is the one line in the
// bundle that answers it. The distinction is the whole content: a count of CI jobs is not a
// count of the checks a change meets, and reporting four where two gate would be wrong about
// half of them.
//
// Stated as a fraction so a reader sees both halves at once — how many checks a change meets,
// and how much automation runs outside them. The non-gating jobs are asserted absent from the
// list rather than from the page, since they are legitimately named in the page listing below.
//
// The claim is also asserted to stop where the fact does. `Gate` is set by a pull_request
// trigger *or* a push to the default branch, and whether a gating check is *required* is branch
// protection — repository configuration, which no file in the tree states. A finding that said
// these jobs block a merge would be asserting both beyond its evidence.
func TestGateFindingDistinguishesBlockingJobsFromTheRest(t *testing.T) {
	body := indexBody(pipelineGraph(t, "test", "lint"), Options{})

	if !strings.Contains(body, "**Merge gates: 2 of 4 CI jobs.**") {
		t.Errorf("the gate finding does not state the fraction:\n%s", body)
	}
	for _, want := range []string{"[test](./pipelines/test.md)", "[lint](./pipelines/lint.md)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the gate finding does not link %s:\n%s", want, body)
		}
	}
	gates := gateFindingLines(body)
	for _, unwanted := range []string{"nightly-scan", "release"} {
		if strings.Contains(gates, unwanted) {
			t.Errorf("the gate finding lists %s, which does not gate a change:\n%s", unwanted, gates)
		}
	}
	if !strings.Contains(body, "pull request or on a push to the default branch") {
		t.Errorf("the gate finding does not say what makes a job a gate. Either trigger sets it, "+
			"and naming only the pull request is false for a push-only workflow:\n%s", gates)
	}
	if !strings.Contains(body, "is configured on the repository and is not in the tree") {
		t.Errorf("the gate finding does not say that *required* is branch protection rather than "+
			"anything it read:\n%s", gates)
	}
}

// gateFindingLines returns the merge-gate finding and the jobs it names, and nothing else.
//
// A finding is one unindented `- **` bullet followed by its own indented items, so it ends at
// the first line that is neither. Terminating on the next finding instead would be wrong here:
// this one is emitted last, so there is none, and the search would run into the page listing —
// where every pipeline is named whether it gates or not.
func gateFindingLines(body string) string {
	lines := strings.Split(body, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "- **Merge gates:") {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := start + 1
	for end < len(lines) && strings.HasPrefix(lines[end], "  - ") {
		end++
	}
	return strings.Join(lines[start:end], "\n")
}

// A repository whose every workflow runs on a schedule or a tag has automation and no gates, and
// that is a finding rather than a blank — ADR 0030. A reader looking for the checks a change must
// pass needs to be told there are none, not left with a section that says nothing.
func TestGateFindingReportsAWorkflowSetThatGatesNothing(t *testing.T) {
	body := indexBody(pipelineGraph(t), Options{})

	if !strings.Contains(body, "**Merge gates: none of 4 CI jobs.**") {
		t.Errorf("a workflow set with no gates is not reported:\n%s", body)
	}
	// Not the empty-repository line: this repository has CI, and saying it does not would be a
	// different and false claim.
	if strings.Contains(body, "no CI jobs found") {
		t.Errorf("four CI jobs were reported as none found:\n%s", body)
	}
}

// And the absence, on a graph that has other findings to report. A repository with no CI at all
// is the case a reader most needs stated: silence here is indistinguishable from a run that
// failed to look, and the answer changes what an agent does with the repository.
func TestGateFindingReportsARepositoryWithNoCI(t *testing.T) {
	body := indexBody(findingsGraph(t), Options{})

	if !strings.Contains(body, "**Merge gates: no CI jobs found.**") {
		t.Errorf("a repository with no CI does not say so:\n%s", body)
	}
}

// Nothing measured, nothing stated — §4.2. On an empty graph every finding above would read
// as a clean bill of health for a repository the run never looked at.
func TestIndexFindingsAreSilentOnAnEmptyGraph(t *testing.T) {
	body := indexBody(graph.New(), Options{})
	if strings.Contains(body, "Structural findings") {
		t.Errorf("an empty graph produced findings:\n%s", body)
	}
}

// The log accumulates. A run on a later date appends its entry and leaves the earlier one
// untouched — a history whose past entries a generator rewrites is not a history.
func TestLogAccumulatesOneEntryPerDate(t *testing.T) {
	root, _, g := write(t)

	later := demoOptions()
	later.Date = "2026-08-01"
	later.Resource = "git://example.com/repo@bbbbbbb"
	if _, err := Write(root, g, later); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	log := read(t, root, LogPage)
	if !strings.Contains(log, "## 2026-07-30") {
		t.Errorf("the first run's entry was rewritten:\n%s", log)
	}
	if !strings.Contains(log, "## 2026-08-01") {
		t.Errorf("the second run's entry is missing:\n%s", log)
	}
	if strings.Index(log, "## 2026-07-30") > strings.Index(log, "## 2026-08-01") {
		t.Error("entries are not oldest-first")
	}
	// Two commits on the same day collapse to one entry: that is what date-grouped means, and
	// it keeps a repository that rebuilds on every push from growing an entry per push.
	if _, err := Write(root, g, later); err != nil {
		t.Fatalf("third Write: %v", err)
	}
	if n := strings.Count(read(t, root, LogPage), "## 2026-08-01"); n != 1 {
		t.Errorf("the same date produced %d entries, want 1", n)
	}
}

// A date that would not survive the marker round-trip falls back to a fixed name. A region
// name the emitter writes and then cannot recognise would silently become human text and
// stop regenerating, which is the failure mode that looks like working code.
func TestLogRegionNameIsAlwaysValid(t *testing.T) {
	for _, date := range []string{"", "2026-07-30", "not a date", "2026/07/30", strings.Repeat("9", 200)} {
		name := logRegion(date)
		if !validRegionName(name) {
			t.Errorf("logRegion(%q) = %q, which is not a valid region name", date, name)
		}
	}
	if got := logRegion("2026-07-30"); got != "log-2026-07-30" {
		t.Errorf("logRegion = %q", got)
	}
	if got := logRegion(""); got != "log-unknown" {
		t.Errorf("logRegion(\"\") = %q", got)
	}
}

// Every field in the manifest answers a question verify asks. Read back as JSON rather than
// string-matched, because a consumer parses it.
func TestManifestRecordsTheRun(t *testing.T) {
	root, _, g := write(t)
	var m bundleManifest
	if err := json.Unmarshal([]byte(read(t, root, ManifestFile)), &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}

	nodes, edges := g.Counts()
	if m.OKFVersion != "0.2" {
		t.Errorf("okf_version = %q", m.OKFVersion)
	}
	if m.Generator != "signpost/0.1.0" {
		t.Errorf("generator = %q", m.Generator)
	}
	if m.Resource != demoOptions().Resource {
		t.Errorf("resource = %q", m.Resource)
	}
	if m.Date != "2026-07-30" {
		t.Errorf("date = %q", m.Date)
	}
	if m.Counts.Nodes != nodes || m.Counts.Edges != edges {
		t.Errorf("counts = %+v, want %d nodes and %d edges", m.Counts, nodes, edges)
	}
	if len(m.Pages) != nodes+2 {
		t.Errorf("pages lists %d entries, want %d nodes + index + log", len(m.Pages), nodes)
	}
	for i := 1; i < len(m.Pages); i++ {
		if m.Pages[i-1] > m.Pages[i] {
			t.Fatalf("pages is not sorted: %v", m.Pages)
		}
	}
}

// Every confidence level is present even at zero. An absent key and 0 look alike to a
// careless consumer but mean "not computed" and "none inferred", and only the second is a
// fact about the bundle.
func TestManifestConfidenceCountsIncludeZeros(t *testing.T) {
	g := graph.New()
	if err := g.AddNode(&graph.Node{ID: "/modules/a", Kind: graph.KindModule, Title: "a"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := Write(root, g, demoOptions()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var m bundleManifest
	if err := json.Unmarshal([]byte(read(t, root, ManifestFile)), &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	for _, level := range []string{"extracted", "inferred", "ambiguous"} {
		if _, ok := m.Confidence[level]; !ok {
			t.Errorf("confidence.%s absent; a consumer cannot tell zero from unmeasured", level)
		}
	}
}

// A no-model run contains nothing but extracted edges, which is what CI asserts. Here the
// demo graph deliberately contains all three, so the assertion is that the tally is right
// rather than that it is zero.
func TestManifestConfidenceTallyIsCorrect(t *testing.T) {
	root, _, _ := write(t)
	var m bundleManifest
	if err := json.Unmarshal([]byte(read(t, root, ManifestFile)), &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if m.Confidence["inferred"] != 1 {
		t.Errorf("inferred = %d, want 1", m.Confidence["inferred"])
	}
	if m.Confidence["ambiguous"] != 1 {
		t.Errorf("ambiguous = %d, want 1", m.Confidence["ambiguous"])
	}
	if m.Confidence["extracted"] != 3 {
		t.Errorf("extracted = %d, want 3", m.Confidence["extracted"])
	}
}

// Every page's frontmatter must be readable by the parser verify will use. A bundle this
// package can write and this project cannot read would fail later, in a different command.
func TestEveryPageFrontmatterIsReadable(t *testing.T) {
	root, res, _ := write(t)
	for _, rel := range res.Written {
		if !strings.HasSuffix(rel, ".md") {
			continue
		}
		p := ParsePage(read(t, root, rel))
		if !p.HasFrontmatter {
			t.Errorf("%s has no frontmatter", rel)
			continue
		}
		if _, diag := parseFrontmatter(t, p.Frontmatter); diag != "" {
			t.Errorf("%s frontmatter not readable: %s", rel, diag)
		}
	}
}

// An empty graph produces a valid bundle rather than an error or a partial one. This is the
// first-run case on a repository signpost has no extractor for, and a crash there is the
// worst possible first impression.
func TestWriteOnAnEmptyGraph(t *testing.T) {
	root := t.TempDir()
	res, err := Write(root, graph.New(), demoOptions())
	if err != nil {
		t.Fatalf("Write on an empty graph: %v", err)
	}
	if len(res.Written) != 3 {
		t.Errorf("wrote %v, want just the three reserved files", res.Written)
	}
	if idx := read(t, root, IndexPage); !strings.Contains(idx, "0 concepts") {
		t.Errorf("the index does not say the graph was empty:\n%s", idx)
	}
}

// No commit means no resource anywhere in the bundle, rather than a resource naming nothing.
func TestWriteWithoutACommit(t *testing.T) {
	root := t.TempDir()
	g, _ := demoGraph(t)
	if _, err := Write(root, g, Options{Actor: "signpost/0.1.0"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	for rel, content := range snapshot(t, root) {
		if strings.Contains(content, "resource:") {
			t.Errorf("%s carries a resource with no commit:\n%s", rel, content)
		}
	}
	if log := read(t, root, LogPage); !strings.Contains(log, "unknown date") {
		t.Errorf("the log does not say the date was unknown:\n%s", log)
	}
}

func TestPlural(t *testing.T) {
	cases := map[int]string{0: "0 concepts", 1: "1 concept", 2: "2 concepts"}
	for n, want := range cases {
		if got := plural(n, "concept"); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
}

// snapshot reads every file in the bundle, keyed by slash-relative path.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	dir := filepath.Join(root, BundleDir)
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) // #nosec G304 -- test-owned temp directory
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return out
}
