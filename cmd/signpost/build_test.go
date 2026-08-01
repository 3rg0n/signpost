package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// A page whose concept no longer exists is reported and left alone. A renamed directory
// would otherwise silently delete a page someone had written notes on.
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
	if got, err := os.ReadFile(stale); err != nil || string(got) != content {
		t.Errorf("the stale page was modified or deleted: %q, %v", got, err)
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
	if !strings.Contains(stderr, ".kt") {
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
