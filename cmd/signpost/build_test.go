package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/okf"
)

// bundleFile reads a file from the bundle a build wrote.
func bundleFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, okf.BundleDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(b)
}

func TestBuildWritesABundle(t *testing.T) {
	root := fixture(t)
	stdout, stderr, code := invoke(t, "build", "--quiet", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, okf.BundleDir) {
		t.Errorf("stdout does not name the bundle directory:\n%s", stdout)
	}

	// The three files OKF reserves, plus a page for a module the fixture contains.
	for _, rel := range []string{okf.IndexPage, okf.LogPage, okf.ManifestFile, "modules/auth.md"} {
		if bundleFile(t, root, rel) == "" {
			t.Errorf("%s is empty", rel)
		}
	}
	var man map[string]any
	if err := json.Unmarshal([]byte(bundleFile(t, root, okf.ManifestFile)), &man); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if man["okf_version"] != "0.2" {
		t.Errorf("okf_version = %v", man["okf_version"])
	}
}

// A repository with no git history gets no `resource:` and no `generated:` anywhere. Both
// come from the commit, and a page stamped with a commit nobody can check is worse than an
// unstamped one — verify's staleness check compares against a sha, and there would be
// nothing to compare.
func TestBuildOmitsProvenanceWithoutAHistory(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	page := bundleFile(t, root, "modules/auth.md")
	if strings.Contains(page, "resource:") {
		t.Errorf("a resource was stamped with no commit:\n%s", page)
	}
	if strings.Contains(page, "generated:") {
		t.Errorf("a generated stamp was written with no commit date:\n%s", page)
	}
}

// Byte-stability, from outside the emitter. ADR 0005 commits the bundle, so a second run at
// the same commit that changed a byte would be commit churn in someone else's repository.
func TestBuildIsByteStable(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("first build: exit = %d\n%s", code, stderr)
	}
	before := bundleSnapshot(t, root)

	stdout, stderr, code := invoke(t, "build", "--quiet", root)
	if code != 0 {
		t.Fatalf("second build: exit = %d\n%s", code, stderr)
	}
	after := bundleSnapshot(t, root)

	if len(before) != len(after) {
		t.Fatalf("file count changed: %d then %d", len(before), len(after))
	}
	for rel, want := range before {
		if after[rel] != want {
			t.Errorf("%s changed on a re-run:\n got %q\nwant %q", rel, after[rel], want)
		}
	}
	// And the report says so, which is what a reader checks rather than diffing bytes.
	if !strings.Contains(stdout, "0 created, 0 updated") {
		t.Errorf("a re-run reported work it did not do:\n%s", stdout)
	}
}

// The property the whole design rests on, exercised through the CLI: a note written outside
// the managed markers survives a rebuild, and the run says it carried one.
func TestBuildPreservesHumanNotes(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("first build: exit = %d\n%s", code, stderr)
	}
	const rel = "modules/auth.md"
	full := filepath.Join(root, okf.BundleDir, filepath.FromSlash(rel))
	const note = "\nRate limiting lives in the gateway, not here.\n"
	if err := os.WriteFile(full, []byte(bundleFile(t, root, rel)+note), 0o600); err != nil {
		t.Fatalf("editing the page: %v", err)
	}

	stdout, stderr, code := invoke(t, "build", "--quiet", root)
	if code != 0 {
		t.Fatalf("rebuild: exit = %d\n%s", code, stderr)
	}
	if got := bundleFile(t, root, rel); !strings.Contains(got, "Rate limiting lives in the gateway") {
		t.Fatalf("the human note was lost:\n%s", got)
	}
	if !strings.Contains(stdout, "human notes, carried across") {
		t.Errorf("the run did not report carrying a note:\n%s", stdout)
	}
}

// A page whose concept no longer exists and which somebody has written on is reported and left
// alone. A renamed directory would otherwise silently delete a page someone had written notes on.
func TestBuildReportsStalePagesWithoutDeleting(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("first build: exit = %d\n%s", code, stderr)
	}
	stale := filepath.Join(root, okf.BundleDir, "modules", "gone.md")
	const content = "---\ntype: Module\n---\nmy notes on a module that moved\n"
	if err := os.WriteFile(stale, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the stale page: %v", err)
	}

	stdout, stderr, code := invoke(t, "build", "--quiet", root)
	if code != 0 {
		t.Fatalf("rebuild: exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "modules/gone.md") {
		t.Errorf("the stale page was not named:\n%s", stdout)
	}
	if !strings.Contains(stdout, "so they were kept") {
		t.Errorf("the run did not say the page was kept, which is the fact a reader needs "+
			"to distinguish this from a removal:\n%s", stdout)
	}
	if got, err := os.ReadFile(stale); err != nil || string(got) != content {
		t.Errorf("the stale page was modified or deleted: %q, %v", got, err)
	}
}

// The other half — issue #10. A page whose concept is gone and which holds nothing but the
// skeleton a build wrote is deleted, and the run names the file rather than folding it into a
// count. That naming is the point: this is the one line in a build reporting a deletion, and the
// name is what makes recovering the page from git possible.
func TestBuildRemovesAnUnwrittenStalePageAndNamesIt(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("first build: exit = %d\n%s", code, stderr)
	}
	const rel = "modules/ghost.md"
	full := filepath.Join(root, okf.BundleDir, filepath.FromSlash(rel))
	// A page signpost itself wrote, under a name no node has — what a renamed directory leaves.
	if err := os.WriteFile(full, []byte(bundleFile(t, root, "modules/auth.md")), 0o600); err != nil {
		t.Fatalf("writing %s: %v", rel, err)
	}

	stdout, stderr, code := invoke(t, "build", "--quiet", root)
	if code != 0 {
		t.Fatalf("rebuild: exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "1 page(s) removed") || !strings.Contains(stdout, rel) {
		t.Errorf("the removal was not reported by name:\n%s", stdout)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Errorf("%s is still on disk: %v", rel, err)
	}
	// And the bundle it left behind still verifies. A prune that removed the page but left the
	// manifest listing it would trade one wrong claim for another.
	if _, stderr, code := invoke(t, "verify", "--quiet", root); code != 0 {
		t.Errorf("verify failed after the prune: exit = %d\n%s", code, stderr)
	}
}

// -repo names the resource's host part. Asked for rather than derived from a remote, because
// a remote URL is a checkout detail and a fork's remote names the upstream.
func TestBuildRepoFlagShapesTheResource(t *testing.T) {
	if got := resourceBase("example.com/org/repo", "8f2a1c9"); got != "git://example.com/org/repo@8f2a1c9" {
		t.Errorf("resourceBase = %q", got)
	}
	// No repository name still yields a usable resource: the sha is the part verify compares.
	if got := resourceBase("", "8f2a1c9"); got != "git://8f2a1c9" {
		t.Errorf("resourceBase with no repo = %q", got)
	}
	// No commit yields nothing at all.
	if got := resourceBase("example.com/org/repo", ""); got != "" {
		t.Errorf("resourceBase with no sha = %q, want empty", got)
	}
}

// Coverage goes to stderr on build too, and is not opt-in: a bundle built from a repository
// signpost read half of must say so, or its silence reads as completeness.
func TestBuildReportsCoverageByDefault(t *testing.T) {
	root := fixture(t)
	_, stderr, code := invoke(t, "build", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "analysed") {
		t.Errorf("no coverage line:\n%s", stderr)
	}
	if !strings.Contains(stderr, ".scala") {
		t.Errorf("the unread language was not named:\n%s", stderr)
	}
}

// An empty repository produces a valid bundle rather than an error. This is the first run in
// a fresh repository, and a crash there is the worst possible first impression.
//
// The reserved files are asserted by name rather than by count. A count is what this test used
// to check, and adding practices.md broke it with a message saying "want the three reserved
// files" — which named neither the fourth file nor whether its arrival was the bug. A name says
// which file stopped being written.
func TestBuildOnAnEmptyRepository(t *testing.T) {
	root := t.TempDir()
	_, stderr, code := invoke(t, "build", "--quiet", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	for _, name := range []string{okf.IndexPage, okf.LogPage, okf.ManifestFile, okf.PracticesPage} {
		if _, err := os.Stat(filepath.Join(root, okf.BundleDir, name)); err != nil {
			t.Errorf("%s was not written to a bundle for an empty repository: %v", name, err)
		}
	}
	if idx := bundleFile(t, root, okf.IndexPage); !strings.Contains(idx, "0 concepts") {
		t.Errorf("the index does not say the repository was empty:\n%s", idx)
	}
}

// The pointer stub prints, and it names the page an agent should open.
//
// index.md rather than the directory: a directory listing is not a starting point, and
// index.md is the page written to be one.
func TestSuggestAgentsMdPrintsAPointerAtTheIndex(t *testing.T) {
	root := fixture(t)
	stdout, stderr, code := invoke(t, "build", "-suggest-agents-md", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, okf.BundleDir+"/"+okf.IndexPage) {
		t.Errorf("the stub does not name %s/%s:\n%s", okf.BundleDir, okf.IndexPage, stdout)
	}
	// Appendable. A stub not ending in a newline joins the last line of the file it is
	// appended to, which is the one way `>> AGENTS.md` can corrupt somebody's file.
	if !strings.HasSuffix(stdout, "\n") {
		t.Errorf("the stub does not end in a newline, so appending it would join a line:\n%q", stdout)
	}
	// Markdown a person can drop in, not a bare path.
	if !strings.Contains(stdout, "#") {
		t.Errorf("the stub has no heading, so it does not read as a section:\n%s", stdout)
	}
}

// The stub says when to reach for the bundle, not only where it is.
//
// The two halves do different work and the second is the one that changes behaviour. A model
// told the map exists knows the bundle is there; it opens the handler it would have grepped
// for anyway, because nothing connected the symptom in front of it to a page. So the sentence
// has to name a symptom and the page that answers it, which is what turns "read the map" into
// a first move.
//
// Asserted on the words a reader acts on rather than on the sentence as a whole, because the
// wording is prose somebody will improve. What must survive an edit is that the stub tells a
// reader where to start when a symptom crosses more than one module, and that the thing it
// sends them to is one the bundle actually renders — see the corpus stage, which appends this
// stub and then checks the claim against a built bundle. A stub promising a page that answers
// nothing is worse than no stub: it spends the model's trust once.
func TestSuggestAgentsMdSaysWhenToReachForTheBundle(t *testing.T) {
	root := fixture(t)
	stdout, stderr, code := invoke(t, "build", "-suggest-agents-md", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	for _, want := range []string{"crosses modules", "writes it", "reads it"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the stub does not say %q, so it states where the bundle is and never when "+
				"to open it. Orientation is not triage: a model that knows the map exists still "+
				"greps first unless a symptom is named alongside the page that answers "+
				"it:\n%s", want, stdout)
		}
	}
	// Two paragraphs, because the triage sentence is a second one. Folded into the first it
	// reads as more description of the index rather than an instruction about a symptom.
	if strings.Count(stdout, "\n\n") < 2 {
		t.Errorf("the stub is one paragraph, so the triage sentence reads as more description "+
			"of the index rather than as an instruction:\n%s", stdout)
	}
}

// The negative boundary, and the one design §6.2 forbids breaking: the flag writes nothing.
// Not the bundle it would otherwise have built, and above all not AGENTS.md — signpost writes
// .signpost/ and nothing else, and a generator that overwrote the file encoding somebody's
// intent is how teams learn to distrust tooling.
func TestSuggestAgentsMdWritesNothingAtAll(t *testing.T) {
	root := fixture(t)
	// An AGENTS.md already there, with content, because overwriting an existing one is the
	// specific harm. Its bytes are the assertion.
	agents := filepath.Join(root, "AGENTS.md")
	const mine = "# my rules\n\nDo not touch this file.\n"
	if err := os.WriteFile(agents, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, root)

	if _, stderr, code := invoke(t, "build", "-suggest-agents-md", root); code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}

	if got := treeSnapshot(t, root); !maps.Equal(got, before) {
		t.Errorf("-suggest-agents-md changed the tree.\n before: %v\n  after: %v",
			sortedKeysOf(before), sortedKeysOf(got))
	}
	if b, err := os.ReadFile(agents); err != nil || string(b) != mine {
		t.Errorf("AGENTS.md was rewritten: %q (err %v)", string(b), err)
	}
	// And no bundle: the flag is not a build with an extra line of output.
	if _, err := os.Stat(filepath.Join(root, okf.BundleDir)); !os.IsNotExist(err) {
		t.Errorf("a bundle was written by a flag that only prints: %v", err)
	}
}

// A repository whose instructions do not name the bundle is told so, because that is the
// failure a green build cannot show: every page correct, verify passing, and no agent opening
// it.
func TestBuildSaysWhenNothingPointsAtTheBundle(t *testing.T) {
	root := fixture(t)
	_, stderr, code := invoke(t, "build", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "nothing points at the bundle") {
		t.Errorf("an unpointed bundle was not reported:\n%s", stderr)
	}
	// The note names the fix. A line saying only that something is missing leaves the reader
	// to compose a sentence, which is the work the flag exists to remove.
	if !strings.Contains(stderr, "-suggest-agents-md") {
		t.Errorf("the note does not name the flag that fixes it:\n%s", stderr)
	}
}

// The negative boundary on the note: a repository that *has* a pointer is not nagged. A
// diagnostic that fires on a repository which already did the thing is one people learn to
// filter out, and it takes the useful firings with it.
func TestBuildIsQuietWhenAPointerExists(t *testing.T) {
	// Written out rather than ranged over `pointerFiles`, which is the difference between a
	// test and a tautology: a loop over the list under test loses a case when somebody deletes
	// an entry, and passes while doing it. Deleting `README.md` from the list survived this
	// test until the names were pinned here.
	want := []string{
		"AGENTS.md",
		"CLAUDE.md",
		".cursorrules",
		".github/copilot-instructions.md",
		"README.md",
	}
	if !slices.Equal(pointerFiles, want) {
		t.Fatalf("pointerFiles changed to %v.\nEach entry is a file a model is trained to open "+
			"before starting work, so adding or removing one changes which repositories get "+
			"nagged. Update %v here deliberately.", pointerFiles, want)
	}
	// Every one of them, one at a time, because a repository states its rules in exactly one
	// and recognising only AGENTS.md would nag the rest.
	for _, name := range want {
		t.Run(name, func(t *testing.T) {
			root := fixture(t)
			p := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			body := "# rules\n\nRead `" + okf.BundleDir + "/" + okf.IndexPage + "` first.\n"
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, stderr, code := invoke(t, "build", root)
			if code != 0 {
				t.Fatalf("exit = %d\n%s", code, stderr)
			}
			if strings.Contains(stderr, "nothing points at the bundle") {
				t.Errorf("%s names the bundle and the build still asked for a pointer:\n%s",
					name, stderr)
			}
		})
	}
}

// The other half of that boundary, and the one that makes the check worth having: a file that
// exists but does not mention the bundle is not a pointer. Without this, `pointsAtTheBundle`
// could be testing for the file's existence and pass every assertion above.
func TestAnAgentsFileThatNeverNamesTheBundleIsNotAPointer(t *testing.T) {
	root := fixture(t)
	body := "# rules\n\nRun make test before pushing. Nothing here mentions the map.\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := invoke(t, "build", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "nothing points at the bundle") {
		t.Errorf("an AGENTS.md that never names %s was accepted as a pointer:\n%s",
			okf.BundleDir, stderr)
	}
}

// Prose about the bundle directory is not a pointer at the map, and this is the boundary the
// corpus found: its README explains that the harness writes `.signpost/`, which is a sentence
// about the tool and not somewhere for an agent to start. A check keyed on the directory read
// that as adoption and went quiet on a repository that had adopted nothing.
func TestMentioningTheBundleDirectoryIsNotAPointer(t *testing.T) {
	root := fixture(t)
	body := "# rules\n\nThe test harness writes a bundle to `" + okf.BundleDir +
		"/` and deletes it afterwards.\n"
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := invoke(t, "build", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "nothing points at the bundle") {
		t.Errorf("a README mentioning %s in passing was accepted as a pointer at the map:\n%s",
			okf.BundleDir, stderr)
	}
}

// treeSnapshot reads every file in the tree outside the bundle, keyed by slash path. Used to
// assert a command wrote nothing, which needs the whole tree rather than one file: "it did not
// touch AGENTS.md" and "it touched nothing" are different claims.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sortedKeysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Nothing executable lands in a directory that gets committed and often published.
func TestBuildWritesNoExecutableFiles(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	for rel, content := range bundleSnapshot(t, root) {
		if !strings.HasSuffix(rel, ".md") && !strings.HasSuffix(rel, ".json") {
			t.Errorf("bundle contains %s, which is neither markdown nor JSON", rel)
		}
		if strings.HasPrefix(content, "#!") {
			t.Errorf("%s begins with a shebang", rel)
		}
	}
}

// bundleSnapshot reads every file in the bundle, keyed by slash-relative path.
func bundleSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	dir := filepath.Join(root, okf.BundleDir)
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
		t.Fatalf("walking the bundle: %v", err)
	}
	return out
}
