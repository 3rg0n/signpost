package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/manifest"
	"github.com/3rg0n/signpost/internal/okf"
)

// The corpus harness: signpost run end to end against a repository it did not write.
//
// # Why this exists
//
// Signpost runs on itself in CI, and that is a real test — but it can only exercise the code
// paths *this* tree contains. This tree is a Go repository with kebab-case filenames, so
// self-hosting structurally cannot reach the TypeScript, Python, or Rust extractors, cannot
// reach an npm, Cargo, or pyproject manifest, and — the one that cost something — cannot
// reach a filename containing a character that is an indicator in YAML.
//
// Issue #9: a Next.js dynamic route (`app/tools/[slug]/page.tsx`) was written unquoted into a
// YAML flow mapping. `[` opens a flow sequence, so the mapping never terminated, and every
// subsequent `edges[]` entry on the page was silently unreadable. Four pages of a real
// repository lost seven edges, and `verify` called it a warning and exited 0. No amount of
// dogfooding would have found it: nothing in this repository has a bracket in a path.
//
// # What this asserts, and what it deliberately does not
//
// Named facts — this module exists, this dependency was read, this page's frontmatter parses.
// Not counts of nodes and edges: a count assertion fails on every improvement to an
// extractor, which trains people to update the number rather than read the diff, and it never
// says which fact was lost.
//
// The strongest assertion here is the round-trip in TestCorpusFrontmatterParses, because it
// is the only one that does not depend on somebody having thought of the offending character
// in advance. Every path-injection defect signpost has had so far — a newline, a backtick, a
// `](`, and then this — was found by a person imagining the character. That does not scale,
// and the round-trip is what covers the next one.

// corpusRoot is the corpus's location relative to this package.
const corpusRoot = "../../testdata/corpus"

// corpusRepo copies the corpus to a temporary directory and makes it a git repository.
//
// Copied rather than analysed in place, for two reasons that are correctness rather than
// tidiness. Signpost reads git history, and testdata/corpus inside this repository carries
// *signpost's* history — the co-change edges would describe commits to the tool rather than
// to the corpus. And `build` writes `.signpost/`, which has no business being committed here:
// it would be a second bundle for CI's staleness check to compare against the wrong tree.
func corpusRepo(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(filepath.FromSlash(corpusRoot))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("the corpus is missing at %s: %v", src, err)
	}
	dst := t.TempDir()
	copyTree(t, src, dst)

	gitRun(t, dst, "init", "--quiet", "--initial-branch=main")
	gitRun(t, dst, "config", "user.name", "Corpus Author")
	gitRun(t, dst, "config", "user.email", "corpus@example.invalid")
	gitRun(t, dst, "config", "commit.gpgsign", "false")
	// Line endings pinned in the repository the test creates, so the test does not inherit
	// the machine's core.autocrlf. Signpost writes LF; a checkout that rewrote the bundle to
	// CRLF would fail the determinism comparison below for a reason unrelated to what is
	// being tested.
	gitRun(t, dst, "config", "core.autocrlf", "false")
	gitRun(t, dst, "add", "-A")
	gitRun(t, dst, "commit", "--quiet", "-m", "the corpus")
	return dst
}

// copyTree copies a directory tree, skipping nothing.
//
// Deliberately including dotfiles: .github/workflows and .github/dependabot.yml are half the
// point of the corpus, and a copy that quietly dropped them would leave the practices
// assertions below testing a repository with no CI and no dependency automation.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		b, err := os.ReadFile(p) // #nosec G304 -- a path from a walk of this package's own testdata
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
	if err != nil {
		t.Fatalf("copying the corpus: %v", err)
	}
}

// gitRun runs git in dir, isolated from this machine's configuration.
//
// The isolation is the same internal/vcs/git_test.go uses, and it is not optional. A global
// core.hooksPath is inherited by repositories created under it, so a secret-scanning
// pre-commit hook ran on every commit these tests make and took the package to its timeout;
// and `git commit` starts `git maintenance run --auto` detached, which holds handles under
// .git and makes the removal t.TempDir registers fail on Windows.
//
// The dates are pinned because the bundle's `generated:` stamp comes from the commit. An
// unpinned date would make the emitted bytes depend on the day the suite ran, which is the
// determinism assertion below failing for a reason that is not a bug.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=gc.auto", "GIT_CONFIG_VALUE_0=0",
		"GIT_CONFIG_KEY_1=maintenance.auto", "GIT_CONFIG_VALUE_1=false",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// buildCorpus runs `signpost build` on a fresh copy of the corpus and returns its root.
func buildCorpus(t *testing.T, extra ...string) string {
	t.Helper()
	dir := corpusRepo(t)
	args := append([]string{"build", "--quiet", "-repo", "example.com/corpus"}, extra...)
	args = append(args, dir)
	if _, stderr, code := invoke(t, args...); code != 0 {
		t.Fatalf("build: exit = %d\n%s", code, stderr)
	}
	return dir
}

// bundlePages returns every markdown page in the bundle, keyed by bundle-relative slash path.
func bundlePages(t *testing.T, dir string) map[string]string {
	t.Helper()
	root := filepath.Join(dir, okf.BundleDir)
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p) // #nosec G304 -- a path from a walk of the bundle this test wrote
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("reading the bundle: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("the build wrote no pages")
	}
	return out
}

func pageNames(pages map[string]string) string {
	out := make([]string, 0, len(pages))
	for k := range pages {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, "\n  ")
}

// TestCorpusFrontmatterParses is the assertion issue #9 could not have survived.
//
// Every page's frontmatter is parsed back with signpost's own reader, and a note the reader
// classifies as Malformed fails the test. Malformed rather than Incomplete because they are
// different claims: Incomplete means the reader stepped over a construct it does not support,
// which is ADR 0001's tolerance working as designed, and Malformed means the document is not
// parseable by anything, so a conforming reader silently loses everything after that point.
func TestCorpusFrontmatterParses(t *testing.T) {
	dir := buildCorpus(t)

	for rel, src := range bundlePages(t, dir) {
		page := okf.ParsePage(src)
		if !page.HasFrontmatter {
			t.Errorf("%s: no frontmatter", rel)
			continue
		}
		fm, diag := manifest.ParseYAMLDoc(page.Frontmatter)
		if diag.Malformed {
			t.Errorf("%s: the frontmatter this build just wrote is not parseable YAML: %s\n"+
				"---\n%s---", rel, diag.Summary(), page.Frontmatter)
			continue
		}
		if fm == nil || fm.Kind != manifest.KindMap {
			t.Errorf("%s: frontmatter did not read back as a mapping", rel)
			continue
		}
		checkEdgesReadBack(t, rel, fm)
	}
}

// edgeKeys is every key the emitter puts in an `edges[]` mapping.
var edgeKeys = map[string]bool{
	"kind": true, "to": true, "confidence": true, "weight": true, "source": true,
}

// checkEdgesReadBack requires each edge mapping to carry only keys the emitter writes.
//
// The key set rather than parseability, because parseability is not sufficient and a comma
// is the proof. An unquoted `[` makes the document unreadable, which Malformed catches. An
// unquoted `,` inside a flow mapping parses *clean* and silently splits the scalar:
// `source: py/greeter/data,notes.py` reads back as `source: py/greeter/data` with an invented
// `notes.py:` key beside it. Every consumer then gets a source path that names a file which
// does not exist, and nothing anywhere reports a problem.
//
// So the assertion is on the shape, not the content. An unexpected key means a scalar
// terminated where the emitter did not intend, whatever the character responsible — which is
// the property that makes this cover the next one of these rather than only the last.
func checkEdgesReadBack(t *testing.T, rel string, fm *manifest.Node) {
	t.Helper()
	for _, edge := range fm.Get("edges").Seq() {
		if edge.Kind != manifest.KindMap {
			t.Errorf("%s: an edge read back as %v rather than a mapping", rel, edge.Kind)
			continue
		}
		for _, k := range edge.Keys {
			if !edgeKeys[k] {
				t.Errorf("%s: an edge read back with the key %q, which the emitter does not "+
					"write. A scalar terminated early — an unquoted character in a path split "+
					"it and the remainder became a key.", rel, k)
			}
		}
	}
}

// TestCorpusHostilePathsAreQuoted pins the specific defect, alongside the general check above.
//
// Both, deliberately. The round-trip proves the page is readable; this proves why, and it is
// the one that fails with a comprehensible message if someone later narrows the flow-indicator
// rule in needsYAMLQuote. A round-trip failure says "line 18: flow mapping entry made no
// progress"; this names the path that was written unquoted.
func TestCorpusHostilePathsAreQuoted(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	// Every path in the corpus carrying a character that terminates a YAML flow scalar. The
	// first is issue #9 exactly; the others are the same class reached by different
	// characters, each a real ecosystem convention rather than a contrived string.
	hostile := []string{
		"app/tools/[slug]/page.tsx",
		"app/blog/[...rest]/page.tsx",
		"greeter/data,notes.py",
	}
	for _, want := range hostile {
		seen := false
		for rel, src := range pages {
			for _, line := range strings.Split(src, "\n") {
				// Only flow collections matter. The same path appears in prose and in
				// `resource:` as a plain block scalar, where these characters are harmless,
				// and asserting on those would be asserting on the emitter's quoting style
				// rather than on correctness.
				if !strings.Contains(line, "{ ") || !strings.Contains(line, want) {
					continue
				}
				seen = true
				if !strings.Contains(line, `"`) {
					t.Errorf("%s: a path containing a YAML flow indicator is unquoted inside "+
						"a flow mapping, which makes the frontmatter unreadable from this "+
						"line down (issue #9):\n  %s", rel, strings.TrimSpace(line))
				}
			}
		}
		if !seen {
			t.Errorf("no page referenced %s inside a flow mapping. Either the corpus fixture "+
				"or the extractor changed, and this test is no longer covering the case it "+
				"was written for.", want)
		}
	}
}

// TestCorpusFindsEveryLanguage asserts each first-class language produced a module page.
//
// By name, not by count. A count says something changed; a name says the Python extractor
// stopped running.
func TestCorpusFindsEveryLanguage(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	for _, want := range []struct{ language, page string }{
		{"Go", "modules/greeter.md"},
		{"Go (a second module, so collision resolution runs)", "modules/hello.md"},
		{"Rust", "modules/src.md"},
		{"TypeScript", "modules/src-2.md"},
		{"Python", "modules/greeter-2.md"},
		{"TypeScript, bracketed directory", "modules/slug.md"},
		{"TypeScript, parenthesised directory", "modules/marketing.md"},
	} {
		if _, ok := pages[want.page]; !ok {
			t.Errorf("%s: no page at %s. Pages written:\n  %s",
				want.language, want.page, pageNames(pages))
		}
	}
}

// TestCorpusReadsEveryManifest asserts each ecosystem's dependencies reached the bundle.
//
// An external dependency becomes a reference page, so the page's existence is what says the
// manifest reader ran. One named dependency per ecosystem: the failure worth catching is a
// reader that stopped working, not a version that moved.
func TestCorpusReadsEveryManifest(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	for _, want := range []struct{ ecosystem, page string }{
		{"Go (go.mod)", "references/go-github-com-google-uuid.md"},
		{"npm (package.json)", "references/npm-next.md"},
		{"Cargo (Cargo.toml)", "references/crates-io-serde.md"},
		{"Python (pyproject.toml)", "references/pypi-httpx.md"},
		{"GitHub Actions (workflow)", "references/github-actions-actions-checkout.md"},
		{"Compose (compose.yaml)", "services/api.md"},
	} {
		if _, ok := pages[want.page]; !ok {
			t.Errorf("%s: no page at %s. Pages written:\n  %s",
				want.ecosystem, want.page, pageNames(pages))
		}
	}
}

// TestCorpusPracticesReportsBothKinds checks the practices page states what the corpus
// declares and what it does not.
//
// The corpus is built to have both, and the absences are deliberate fixture decisions rather
// than omissions: no SECURITY.md, and a Python manifest with no lockfile beside it while the
// other three ecosystems have one. A page that only ever reported presences would render an
// absence as silence, which is the failure design §9.1 is written against.
func TestCorpusPracticesReportsBothKinds(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	page, ok := pages[okf.PracticesPage]
	if !ok {
		t.Fatalf("no %s was written. Pages:\n  %s", okf.PracticesPage, pageNames(pages))
	}
	for _, want := range []string{
		"A build command is declared",
		"A test command is declared",
		"jobs can block a merge",
		// The non-gating workflow. Both halves of the gate distinction, since a schedule-only
		// job cannot be a required check and reporting it as one would be wrong.
		"outside that gate",
		"The Go dependencies are pinned by a lockfile",
		"The npm dependencies are pinned by a lockfile",
		"The Cargo dependencies are pinned by a lockfile",
		"Automated dependency updates are configured",
		"ownership rules assign paths to reviewers",
		"The repository states its licence",
		"An observability library is a declared dependency",
		"rules for agents working in this repository",
		// The absences.
		"**Not declared.**",
		"No security policy was found",
		"The Python dependencies are declared but not pinned",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("%s does not state %q:\n%s", okf.PracticesPage, want, page)
		}
	}

	// No score, in either direction. Design §9.1: a rubric is an opinion that has to be
	// defended and re-tuned per repository, and a repository at "level 2" reads as measured
	// when it has only been judged. This is the assertion that keeps that decision from being
	// quietly reverted by someone who thinks a page of findings wants a summary line.
	//
	// Checked against the managed region rather than the whole page, because the human intro
	// above it says "there is no score here" — matching on the bare word would fail on the
	// sentence that states the rule.
	region := page
	if i := strings.Index(page, "signpost:managed:practices -->"); i >= 0 {
		region = page[i:]
	} else {
		t.Errorf("%s has no managed region, so the emitter did not write this page",
			okf.PracticesPage)
	}
	lower := strings.ToLower(region)
	for _, unwanted := range []string{"maturity", "score", "out of 5", "grade", "rubric",
		"level 1", "level 2"} {
		if strings.Contains(lower, unwanted) {
			t.Errorf("%s states %q — the page emits findings, never a score (design §9.1)",
				okf.PracticesPage, unwanted)
		}
	}
}

// TestCorpusVerifyPassesOnItsOwnOutput is the end-to-end gate: build, then verify.
//
// Verify exiting 0 on a freshly built bundle is the property the whole tool rests on, and it
// is worth asserting against a repository signpost did not write, because the failure it
// catches is a page the emitter produces and the checker rejects. Issue #9 is exactly that
// shape: the emitter wrote it, the checker read it, and the disagreement was reported at a
// severity that let CI pass.
func TestCorpusVerifyPassesOnItsOwnOutput(t *testing.T) {
	dir := buildCorpus(t)

	_, stderr, code := invoke(t, "verify", "--quiet", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("verify rejected a bundle signpost had just written: exit = %d\n%s",
			code, stderr)
	}
}

// TestCorpusBuildIsByteStable runs the build twice over the same tree and requires identical
// bytes.
//
// ADR 0005 commits the bundle, so nondeterminism is commit churn in somebody else's
// repository. Asserted on the corpus as well as on cmd/signpost's own fixture because the
// sources of nondeterminism are map iteration and filesystem order, and those need scale to
// show up: four languages, thirty-odd files, and several module names that collide and must
// be resolved to the same suffixed page twice.
func TestCorpusBuildIsByteStable(t *testing.T) {
	dir := buildCorpus(t)
	first := bundlePages(t, dir)

	if _, stderr, code := invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir); code != 0 {
		t.Fatalf("the second build failed: exit = %d\n%s", code, stderr)
	}
	second := bundlePages(t, dir)

	if len(first) != len(second) {
		t.Fatalf("the second build wrote %d pages, the first wrote %d",
			len(second), len(first))
	}
	for rel, want := range first {
		got, ok := second[rel]
		if !ok {
			t.Errorf("%s was written by the first build and not the second", rel)
			continue
		}
		if got != want {
			t.Errorf("%s differs between two builds of the same tree", rel)
		}
	}
}
