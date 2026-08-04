package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/3rg0n/signpost/internal/config"
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
//
// # Every bug lands here
//
// A fixed bug earns a stage in this file, not only a unit test in the package that owns the
// fix. The two are not substitutes. A unit test proves the function behaves; a stage here
// proves the *binary* behaves, on a repository whose shape signpost's own tree cannot
// produce, through the same command a user runs. Both defects this harness has been extended
// for were invisible to the package tests that covered their code:
//
//   - Issue #9, a YAML flow indicator in a path: every unit test in internal/okf was green,
//     because no path in this repository contains a bracket.
//   - The CRLF checkout: every unit test in internal/okf was green, and so was CI, because
//     this repository's .gitattributes pins `eol=lf` — so the tree that would have shown the
//     bug is the one tree configured to prevent it.
//
// That is the pattern worth naming: a bug that survives a package's own tests survives
// because the tree those tests run in cannot express the condition. The corpus can, so this
// is where the condition gets expressed.

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
//
// The name is the directory, taken from the page's title, rather than the page's filename.
// Several of these directories are called `src` and only one of them can be src.md, so the
// filenames of the others carry a suffix that identifies the directory and is deliberately
// not predictable from ordering — asserting on it would be asserting on the ID scheme,
// which is not what this test is about.
func TestCorpusFindsEveryLanguage(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	byTitle := map[string]string{}
	for name, body := range pages {
		if !strings.HasPrefix(name, "modules/") {
			continue
		}
		if title := frontmatterTitle(body); title != "" {
			byTitle[title] = name
		}
	}

	for _, want := range []struct{ language, dir string }{
		{"Go", "go/greeter"},
		{"Go (a second module, so collision resolution runs)", "go/cmd/hello"},
		{"Rust", "rust/src"},
		{"TypeScript", "ts/src"},
		{"Python", "py/greeter"},
		{"TypeScript, bracketed directory", "ts/app/tools/[slug]"},
		{"TypeScript, parenthesised directory", "ts/app/(marketing)"},
	} {
		if _, ok := byTitle[want.dir]; !ok {
			t.Errorf("%s: no module page for %s. Module pages written:\n  %s",
				want.language, want.dir, pageNames(pages))
		}
	}
}

// frontmatterTitle reads the title line out of a page, which for a module page is its
// repo-relative directory.
func frontmatterTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "title:"); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
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

// TestCorpusAManifestWithNothingToPinSaysSo is the corpus half of the empty-manifest false
// positive, and it needs a repository rather than a fixture because the defect was found by
// reading a page, not by running a test.
//
// signpost's own practices page said "The Go dependencies are declared but not pinned by any
// lockfile in the tree, so two builds can resolve different versions" about a `go.mod` with an
// empty require block and no `go.sum`. Nothing was declared, so nothing resolves and two builds
// cannot differ — three false clauses in one sentence, on the page whose entire purpose is that
// a reader can trust it.
//
// The condition cannot be expressed by the corpus as committed: all four of its ecosystems
// declare dependencies, because every other stage here needs them to. So this stage makes one
// of them empty — the manipulate-then-rebuild shape the CRLF and stale-page stages use — and
// takes its lockfile away, which is the only arrangement that reaches the branch.
//
// All three outcomes are asserted from the one page, and that is the point. Go stays pinned,
// Python stays declared-and-unpinned, Cargo becomes the empty case. A fix that answers "nothing
// is unpinned" fails on Python, one that answers "every manifest is empty" fails on Go and
// Python, and the shipped bug fails on Cargo. No single answer satisfies the page.
func TestCorpusAManifestWithNothingToPinSaysSo(t *testing.T) {
	dir := corpusRepo(t)

	// A Cargo.toml declaring a package and no dependencies — legal, common in a workspace
	// member, and what the empty require block was. The lockfile goes with it: a manifest with
	// nothing to pin and a lockfile beside it reports as pinned, which is a different branch.
	rewrite(t, filepath.Join(dir, "rust", "Cargo.toml"),
		"[package]\nname = \"corpus-greeter\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
	if err := os.Remove(filepath.Join(dir, "rust", "Cargo.lock")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--quiet", "-m", "empty the crate's dependency table")

	if _, stderr, code := invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir); code != 0 {
		t.Fatalf("build: exit = %d\n%s", code, stderr)
	}
	page, ok := bundlePages(t, dir)[okf.PracticesPage]
	if !ok {
		t.Fatalf("no %s was written", okf.PracticesPage)
	}

	if !strings.Contains(page, "The Cargo manifest declares no dependencies") {
		t.Errorf("%s does not state that the Cargo manifest declares nothing. The crate's "+
			"dependency table is empty and no Cargo.lock is in the tree:\n%s",
			okf.PracticesPage, page)
	}
	if strings.Contains(page, "The Cargo dependencies are declared but not pinned") {
		t.Errorf("%s reports an empty dependency table as unpinned, so it tells a reader two "+
			"builds can resolve different versions of nothing:\n%s", okf.PracticesPage, page)
	}
	// The counterparts, from the same build. Without these the assertion above is satisfied by
	// a page that stopped reporting unpinned ecosystems at all — which is the more expensive
	// bug, because an unreproducible build then reads as a clean one.
	if !strings.Contains(page, "The Python dependencies are declared but not pinned") {
		t.Errorf("%s stopped reporting the unpinned Python manifest, which declares two "+
			"dependencies and has no lockfile beside it:\n%s", okf.PracticesPage, page)
	}
	if !strings.Contains(page, "The Go dependencies are pinned by a lockfile") {
		t.Errorf("%s stopped reporting the pinned Go module:\n%s", okf.PracticesPage, page)
	}
	// Exactly one manifest is empty in this tree. A count rather than a presence check, because
	// a branch that fell through would state it for Python as well and every assertion above
	// would still pass.
	if n := strings.Count(page, "manifest declares no dependencies"); n != 1 {
		t.Errorf("%d manifest(s) reported as declaring nothing, want 1 — only the crate's table "+
			"was emptied:\n%s", n, page)
	}
}

// rewrite replaces a file in the corpus copy.
func rewrite(t *testing.T, path, body string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the corpus fixture this stage rewrites is gone: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
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

// TestCorpusSecretsAreAttributedToTheServiceThatReadsThem is the regression for secrets
// sprayed across every service in a compose file.
//
// The bug, found reading a real bundle rather than by any test: a Facts is per *file*, and
// SecretRefs carried no service, so assemble linked them back through the only thing it had
// — the filename. Every service declared in a compose file therefore inherited every secret
// named anywhere in it. On the repository where this surfaced, a Caddy reverse proxy with no
// `environment:` block at all was reported as reading nine credentials including the database
// password and the SAML certificate.
//
// Worth a stage here rather than only a unit test because the claim is false in the direction
// that matters. "This service reads that credential" is a fact a reader acts on without
// re-deriving it, and an over-broad blast radius reads as a finding: it says a credential is
// reachable from somewhere it is not. A missing edge invites a question; an invented one
// invites a conclusion.
//
// The corpus expresses the condition in the shape that produces it — one compose file, two
// services, and a credential-shaped variable in exactly one of them. `db` reads
// POSTGRES_PASSWORD; `api` reads DATABASE_URL, which is not credential-shaped, so `api` reads
// no secret at all. Under the defect `api` got POSTGRES_PASSWORD from its neighbour.
func TestCorpusSecretsAreAttributedToTheServiceThatReadsThem(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	const secret = "POSTGRES_PASSWORD"
	db, ok := pages["services/db.md"]
	if !ok {
		t.Fatalf("no page for the db service, so this test asserts nothing about attribution. "+
			"Pages: %v", sortedPageNames(pages))
	}
	api, ok := pages["services/api.md"]
	if !ok {
		t.Fatalf("no page for the api service: %v", sortedPageNames(pages))
	}

	// The counterpart that still fails. Without it, a fix that stopped reporting secrets
	// entirely would satisfy the assertion below — and dropping the fact is the other way to
	// get this wrong, not the fix.
	if !strings.Contains(db, secret) {
		t.Errorf("the db service declares %s in compose.yaml and its page does not mention it, "+
			"so the reference was lost rather than attributed:\n%s", secret, db)
	}
	if !strings.Contains(db, "reads-secrets") {
		t.Errorf("the db service reads a secret and is not tagged reads-secrets:\n%s", db)
	}

	// The symptom a user would report: a service that declares no credential presented as
	// reading its neighbour's.
	if strings.Contains(api, secret) {
		t.Errorf("the api service declares no credential-shaped variable, and its page names "+
			"%s — the secret belonging to db, the other service in the same compose file. A "+
			"reader is told a credential is reachable from a service that never sees it:\n%s",
			secret, api)
	}
	if strings.Contains(api, "reads-secrets") {
		t.Errorf("the api service reads no secret and carries the reads-secrets tag:\n%s", api)
	}
}

// TestCorpusWorkspacePackagesAreNotExternalDependencies is the regression for a monorepo's
// own packages reported as third-party dependencies.
//
// The bug, found running signpost against a real npm monorepo: a workspace declares its
// sibling packages as ordinary dependencies (`"@scope/core": "workspace:*"`), and nothing
// mapped a declared package name back to the directory holding its source. So a
// cross-package import fell through to the dependency lookup, matched that entry, and drew
// an edge to an External Dependency page for code sitting in the same repository. Measured
// there: 60 of 81 scoped "external dependencies" were directories in the tree, 2064 edges
// pointed at them, and the package with 122 importers had a module node showing zero.
//
// Two false claims, both worth a stage. The supply-chain view named first-party source as
// something pulled in from outside, which is the audit a reader cannot re-derive from the
// page. And cross-package coupling — the primary architectural fact in a monorepo — was
// routed to leaf nodes, so the index's "most connected" list ranked six dependency pages
// where the real hubs belonged.
//
// Go was never affected, which is the tell: `addGoModule` gives resolveGo a name-to-
// directory map to consult before treating a specifier as external, and npm had no
// equivalent. It needed *two* things to be true at once — a package that exists in the
// repository *and* is imported by its published name — and a unit test over one resolver
// call is handed one specifier with no workspace around it.
//
// The corpus expresses it as the shape that produces it: `ts/packages/{core,api}`, each
// with its own package.json, `api` depending on `@corpus/core` with `workspace:*` and
// importing it both bare and deep. `main` points at `dist/`, which is absent — the normal
// published shape, and the reason resolution has to fall back to the source root.
func TestCorpusWorkspacePackagesAreNotExternalDependencies(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	// No page for the sibling package as a dependency. Asserted on the bundle rather than the
	// graph because the page is what a reader audits.
	for rel := range pages {
		if strings.Contains(rel, "corpus-core") || strings.Contains(rel, "corpus-api") {
			t.Errorf("%s is a page for a package that lives in this repository, presented as an "+
				"external dependency. A reader auditing what this repo pulls in from outside is "+
				"shown first-party source:\n%s", rel, pages[rel])
		}
	}

	// The counterpart that still fails, and it is the whole point: suppressing the page is only
	// correct if the import found the real module instead. A fix that dropped the edge would
	// satisfy the loop above while losing the coupling this exists to surface.
	stdout, stderr, code := invoke(t, "graph", "export", "-format", "json", "--quiet", dir)
	if code != 0 {
		t.Fatalf("export failed: exit = %d\n%s", code, stderr)
	}
	var g struct {
		Nodes []struct {
			ID, Kind, Path string
		}
		Edges []struct {
			From, To, Kind string
		}
	}
	if err := json.Unmarshal([]byte(stdout), &g); err != nil {
		t.Fatalf("export did not produce JSON: %v", err)
	}
	var apiID, coreID string
	for _, n := range g.Nodes {
		switch n.Path {
		case "ts/packages/api/src":
			apiID = n.ID
		case "ts/packages/core/src":
			coreID = n.ID
		}
		// A genuinely external dependency must still be one. Without this the exclusion could
		// be over-broad — dropping every npm node — and every assertion here would pass.
		if n.Kind == "External Dependency" && n.Path != "" {
			t.Errorf("external dependency %s has a repository path %q, so it is not external",
				n.ID, n.Path)
		}
	}
	if apiID == "" || coreID == "" {
		t.Fatalf("no module node for one of the workspace packages (api=%q core=%q), so this "+
			"test asserts nothing", apiID, coreID)
	}
	var found bool
	for _, e := range g.Edges {
		if e.From == apiID && e.To == coreID && e.Kind == "imports" {
			found = true
		}
	}
	if !found {
		t.Errorf("api imports @corpus/core by published name and there is no imports edge from "+
			"%s to %s. The reference was dropped rather than resolved, which loses the "+
			"cross-package coupling that is the main structural fact in a monorepo", apiID, coreID)
	}

	// react is declared alongside @corpus/core in the same package.json. It has to stay an
	// external node, or the exclusion is matching on the wrong thing.
	var reactExternal bool
	for _, n := range g.Nodes {
		if n.Kind == "External Dependency" && strings.Contains(n.ID, "npm-react") {
			reactExternal = true
		}
	}
	if !reactExternal {
		t.Error("react is a real npm dependency and is no longer an external node, so the " +
			"workspace exclusion is over-broad")
	}
}

// TestCorpusTSConfigPathAliasesResolve is the regression for issue #13.
//
// A TypeScript codebase states what its own import specifiers mean in
// `compilerOptions.paths`, and nothing else states it. `@fider/services` is `public/services`
// on disk because one line of tsconfig.json says so. signpost did not read that file, so it
// guessed at a handful of bare prefixes — `@/`, `~/`, `#/` — and reported every named alias as
// unresolved. On the repository where this surfaced: 542 of 3912 edges absent, 14% of the
// graph, from a single unread mapping. After: 37 unresolved, none of them an alias.
//
// Unlike #12 this under-reported rather than fabricating, and the count was printed on every
// run — but 14% of a codebase silently missing from the map is well past where a reader stops
// trusting it, and the failure mode on the other side is worse: a matched pattern that fell
// through to the npm lookup would turn `@fider/services` into a third-party dependency named
// `@fider`, which is #12's defect reached by a different road.
//
// The corpus expresses four shapes, all drawn from real configs and none of them expressible
// before this fix, because the fixture was a tsconfig with no `paths` block at all:
//
//   - a named wildcard alias, `@corpus/app/*` -> `src/*`;
//   - an alias used from a package whose own tsconfig declares only `extends`, which is the
//     dominant real shape — 11 of 14 configs in one monorepo — so the mapping is stated two
//     directories away from the file that resolves by it;
//   - two targets for one pattern where the first does not exist, since TypeScript tries them
//     in order and a resolver that stopped at the miss would find nothing;
//   - an exact pattern with no wildcard.
//
// And the file itself is JSONC — block comments, a `//` inside a URL, trailing commas — which
// is not a curiosity: both real files that declared `paths` carried comments, and a strict JSON
// parse of either fails outright, which would make the reader silently useless on exactly the
// files it exists for.
func TestCorpusTSConfigPathAliasesResolve(t *testing.T) {
	dir := buildCorpus(t)

	stdout, stderr, code := invoke(t, "graph", "export", "-format", "json", "--quiet", dir)
	if code != 0 {
		t.Fatalf("export failed: exit = %d\n%s", code, stderr)
	}
	var g struct {
		Nodes []struct {
			ID, Kind, Path, Title string
		}
		Edges []struct {
			From, To, Kind string
		}
	}
	if err := json.Unmarshal([]byte(stdout), &g); err != nil {
		t.Fatalf("export did not produce JSON: %v", err)
	}

	byPath := make(map[string]string, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.Path != "" {
			byPath[n.Path] = n.ID
		}
		// The alias prefix must never become a dependency. `@corpus/app` is a mapping, not a
		// package, and an external node for it is the fabricating failure — the one that gives
		// a reader a supply-chain entry nobody publishes. `@corpus/assets/logo.svg` is what
		// reaches that branch: the pattern matches and the target is not extracted source, so
		// there is no node to return, and a resolver that fell through on the miss rather than
		// claiming the specifier would land in the npm lookup.
		if n.Kind == "External Dependency" && (n.Title == "@corpus" || strings.HasPrefix(n.Title, "@corpus/")) {
			t.Errorf("%s is an external dependency page for a tsconfig path alias, which is a "+
				"mapping onto this repository's own directories and not a package anyone "+
				"publishes", n.Title)
		}
	}

	// Each alias shape, asserted by the edge it must produce. Named individually rather than
	// counted, because a count is satisfied by the wrong edges.
	for _, c := range []struct {
		from, to, why string
	}{
		{
			"ts/packages/api/src", "ts/src",
			"`@corpus/app/greeter` is declared in ts/tsconfig.json and used from a package " +
				"whose own tsconfig declares only `extends`. Missing this edge means the " +
				"inheritance was not followed, which is most of what a monorepo's configs do",
		},
		{
			"ts/app/tools/[slug]", "ts/app/(marketing)",
			"`@corpus/ui/*` lists two targets and the first does not exist. Missing this edge " +
				"means resolution stopped at the first miss instead of trying the rest, which " +
				"is the fallback TypeScript actually performs",
		},
		{
			"ts/app/tools/[slug]", "ts/src",
			"`@corpus/entry` is an exact pattern with no wildcard. Missing this edge means only " +
				"wildcard patterns were matched",
		},
	} {
		from, to := byPath[c.from], byPath[c.to]
		if from == "" || to == "" {
			t.Fatalf("no module node for %q or %q, so this test asserts nothing", c.from, c.to)
		}
		var found bool
		for _, e := range g.Edges {
			if e.From == from && e.To == to && e.Kind == "imports" {
				found = true
			}
		}
		if !found {
			t.Errorf("no imports edge %s -> %s (%s -> %s): %s", c.from, c.to, from, to, c.why)
		}
	}

	// An alias onto something that is not source is resolved, not reported: the mapping was
	// read, so the specifier is not a gap in the map, and counting it would tell a reader
	// signpost failed to understand an import it understood exactly. `@corpus/assets/logo.svg`
	// is that case, and it is also what reaches the fall-through — a matched pattern that did
	// not claim its specifier lands in the npm lookup, which either fabricates a package or,
	// where nothing is declared under that name, reports the codebase's own mapping as an
	// import it could not follow.
	//
	// The count is asserted in TestCorpusResolvesExactlyWhatItShould rather than here, because
	// the printed list is truncated to the top five and this specifier is the one that falls
	// off the end — a substring check for it would pass by reading `and 1 more`.

	// The counterpart that still fails. Every assertion above is satisfied by a resolver that
	// claims any bare specifier as internal until something matches, and that is worse than
	// the bug: react is a real dependency, and losing it hides what this code pulls in from
	// outside.
	//
	// Asserted on the edge rather than on the page, because the page is not evidence. An
	// external node is created for every dependency the manifest declares, imported or not,
	// so `references/npm-react.md` exists whether or not a single import ever reached it. The
	// edge is the only thing that says the resolver still sends bare specifiers there.
	var reactID string
	for _, n := range g.Nodes {
		if n.Kind == "External Dependency" && strings.Contains(n.ID, "npm-react") {
			reactID = n.ID
		}
	}
	if reactID == "" {
		t.Fatal("no external node for react, so this assertion covers nothing")
	}
	var reactImported bool
	for _, e := range g.Edges {
		if e.To == reactID && e.Kind == "imports" {
			reactImported = true
		}
	}
	if !reactImported {
		t.Error("no module imports react any more, so alias resolution is claiming bare " +
			"specifiers it was never given a mapping for and routing them into this repository")
	}
}

// TestCorpusResolvesExactlyWhatItShould is the corpus's negative boundary, and it is the only
// assertion here that fails in both directions.
//
// Everything else about resolution is asserted positively: this edge exists, that page exists.
// A positive is satisfied by a resolver that is too generous as easily as by a correct one — if
// every specifier were claimed as internal, every alias assertion in this file would stay green
// and the map would be wrong in the direction that cannot be seen. Testing that 1+1 is 2 never
// catches an adder that answers 2 for everything.
//
// So the unresolved set is asserted exactly, and the corpus carries a deliberate near-miss in
// each language, each one a name that a matcher slightly too loose would swallow:
//
//   - go `example.com/corpus/greeterx/format` — shares every character of the declared module
//     `example.com/corpus/greeter` up to a segment boundary that is not there;
//   - typescript `@corpus/apples/juice` — shares ten characters with the alias prefix
//     `@corpus/app/`, whose trailing slash is the only thing separating them;
//   - python `httpx_extras` — declared `httpx` plus a suffix, in the underscore spelling that
//     PEP 503 normalization rewrites;
//   - python `winreg_helpers` — opens with the six characters of the stdlib `winreg`, which is
//     the boundary on the other side of the completed pyStdlib list: a longer table is a wider
//     surface for a prefix match, and a package reclassified as the runtime is a gap nobody is
//     told about;
//   - rust `serde_yaml::Value` — a real crate that is not the declared `serde`, in the
//     underscore spelling the dash/underscore equivalence exists to accept;
//   - typescript `pathe/utils` — an npm package whose name opens with the four characters
//     of the Node builtin `path`, which is the boundary for issue #14's subpath rule.
//
// Each must land here and nowhere else. Two wrong homes are possible and both are worse than
// the gap: an edge into this repository, which invents structure; or an external node, which
// invents a supply-chain entry nobody declared. Which failure a given over-match produces
// depends on the repository, so neither is asserted alone — the set is.
//
// The stdlib imports are the other half. `node:fs`, python `os`, rust `std::fmt`, and the
// Node builtins addressed by subpath — `fs/promises`, `node:test/reporters` — are the runtime:
// in no manifest, patched by nobody. They must be absent from this set, and absent is also what
// a resolver that silently dropped them looks like, which is why they sit in files whose other
// imports are asserted positively above.
//
// Measured against the real binary rather than the resolver's internals, because the report on
// stderr is what a user acts on. Three mutations confirm it: comparing a Go module prefix by
// string instead of by path segment, comparing an alias prefix without its trailing slash, and
// testing a Node builtin by prefix rather than by first path segment. All three leave the node
// and edge counts at 25 and 24 — untouched, so every other assertion in this file stays green —
// and all three are caught here, one specifier at a time.
func TestCorpusResolvesExactlyWhatItShould(t *testing.T) {
	dir := corpusRepo(t)
	// No -quiet: the coverage report is on stderr and -quiet is what suppresses it. Built
	// here rather than through buildCorpus for that reason.
	_, stderr, code := invoke(t, "build", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("build failed: exit = %d\n%s", code, stderr)
	}

	// Asserted on the count and then on the names, because the printed list is truncated to
	// the five most frequent and a sixth specifier is summarised as `and 1 more`. A substring
	// check on that line would silently stop covering whichever entry sorts last, so the count
	// is what carries the assertion and the names are what make a failure legible.
	want := []string{
		"go example.com/corpus/greeterx/format",
		"python httpx_extras",
		"python winreg_helpers",
		"rust corpus_greeter::Greeting",
		"rust serde_yaml::Value",
		"typescript @corpus/apples/juice",
		"typescript pathe/utils",
	}
	got, ok := unresolvedCount(stderr)
	if !ok {
		t.Fatalf("no unresolved line in the coverage report. Every near-miss in the corpus was "+
			"resolved to something, which means resolution is claiming names nothing declares:"+
			"\n%s", stderr)
	}
	if got != len(want) {
		t.Errorf("%d unresolved specifier(s), want %d.\n\nHigher means something that resolves "+
			"is being reported as a gap. Lower means a near-miss was claimed: routed into this "+
			"repository as an edge it should not have, or attached to a declared dependency it "+
			"is not, which reports a package this code does not use.\n\nThe %d expected:\n  %s"+
			"\n\nReport:\n%s",
			got, len(want), len(want), strings.Join(want, "\n  "), stderr)
	}

	// The names, from the graph rather than the report, so nothing is hidden by truncation. An
	// unresolved specifier draws no edge and creates no node, so its absence from both is the
	// evidence — checked here for the two wrong homes an over-match would put it in.
	stdout, _, code := invoke(t, "graph", "export", "-format", "json", "--quiet", dir)
	if code != 0 {
		t.Fatalf("export failed: exit = %d", code)
	}
	var g struct {
		Nodes []struct{ ID, Kind, Title string }
	}
	if err := json.Unmarshal([]byte(stdout), &g); err != nil {
		t.Fatalf("export did not produce JSON: %v", err)
	}
	// Every near-miss, by the fragment that distinguishes it from the real name it shadows.
	for _, frag := range []string{"apples", "greeterx", "httpx-extras", "httpx_extras", "serde-yaml", "serde_yaml", "pathe", "winreg-helpers", "winreg_helpers"} {
		for _, n := range g.Nodes {
			if n.Kind != "External Dependency" {
				continue
			}
			if strings.Contains(strings.ToLower(n.Title), frag) {
				t.Errorf("%q is an external dependency page, but no manifest in the corpus "+
					"declares it. It is a near-miss of a name that is declared, so a matcher "+
					"too loose about prefixes or about the dash/underscore spelling folded it "+
					"into one — which reports a dependency this code does not have", n.Title)
			}
		}
	}
	// And the stdlib, which must not appear either: it is the runtime, and a page for it is a
	// supply-chain entry for something nobody ships or patches. `promises` covers the subpath
	// spellings of issue #14, which were counted as gaps before the first path segment was
	// what got looked up.
	for _, frag := range []string{"node:fs", "std::fmt", "promises"} {
		for _, n := range g.Nodes {
			if n.Kind == "External Dependency" && strings.Contains(n.Title, frag) {
				t.Errorf("%q is an external dependency page; it is the language runtime", n.Title)
			}
		}
	}
	// The platform-specific stdlib, matched exactly rather than by substring: `winreg_helpers`
	// is a deliberate near-miss asserted above, and a substring check for `winreg` would be
	// satisfied by the very page that must not exist.
	//
	// These two are why pyStdlib is now generated from sys.stdlib_module_names rather than kept
	// by hand. `winreg` is Windows-only and `fcntl` is Unix-only, so a list assembled from code
	// read on one platform omits precisely what the other platform's code imports — and a
	// repository doing conditional imports for portability was reported as depending on
	// packages nobody can install. Both spellings sit in this corpus so the list cannot be
	// completed for one platform and left short for the other.
	for _, name := range []string{"winreg", "fcntl"} {
		for _, n := range g.Nodes {
			if n.Kind == "External Dependency" && strings.EqualFold(n.Title, name) {
				t.Errorf("%q is an external dependency page. It is platform-specific standard "+
					"library, and nobody publishes or patches it — a page for it is a "+
					"supply-chain entry for the interpreter", n.Title)
			}
		}
	}
}

// TestCorpusPythonPackageRootsResolve is the regression for the per-package resolution root,
// and it is the Python analogue of issue #13.
//
// `resolvePython` tried exactly two roots — the repository root and `src` — with a comment
// claiming those account for essentially every Python project. That is true of a project and
// false of a monorepo, which is where the imports are. Measured on one: 28 `pyproject.toml`
// files, and `from api.client import make_api_request` written in 340 imports, each resolving
// against the package that declares it. signpost reported every one as a dependency nobody
// declares, while nine sibling packages each held their own `api/client.py`.
//
// Two packages here, deliberately holding the same module path, because the fix has a failure
// mode in each direction and neither assertion means anything alone:
//
//   - a root list that governed the whole repository would resolve both imports to whichever
//     `api/client.py` sorted first, which is an edge between two packages that cannot see each
//     other — worse than the gap it replaces, because nothing reports it as a guess;
//   - no per-package root at all leaves both unresolved, which is the shipped bug.
//
// Asserted on the exact pair of edges rather than on a count, since a count of two is
// satisfied by both imports landing in the same package.
func TestCorpusPythonPackageRootsResolve(t *testing.T) {
	dir := buildCorpus(t)

	stdout, stderr, code := invoke(t, "graph", "export", "-format", "json", "--quiet", dir)
	if code != 0 {
		t.Fatalf("export failed: exit = %d\n%s", code, stderr)
	}
	var g struct {
		Nodes []struct{ ID, Kind, Path, Title string }
		Edges []struct{ From, To, Kind string }
	}
	if err := json.Unmarshal([]byte(stdout), &g); err != nil {
		t.Fatalf("export did not produce JSON: %v", err)
	}
	byPath := make(map[string]string, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.Path != "" {
			byPath[n.Path] = n.ID
		}
	}
	imports := func(from, to string) bool {
		f, tt := byPath[from], byPath[to]
		if f == "" || tt == "" {
			t.Fatalf("no module node for %q or %q, so this test asserts nothing", from, to)
		}
		for _, e := range g.Edges {
			if e.From == f && e.To == tt && e.Kind == "imports" {
				return true
			}
		}
		return false
	}

	// The positives: each package's absolute import reaches its own code.
	for _, c := range []struct{ from, to string }{
		{"py/services/alpha", "py/services/alpha/api"},
		{"py/services/beta", "py/services/beta/api"},
	} {
		if !imports(c.from, c.to) {
			t.Errorf("no imports edge %s -> %s. `from api.client import fetch` is absolute and "+
				"names a top-level package, so it resolves only against the package root that "+
				"declares it — the repository root and src/ hold no api/ at all", c.from, c.to)
		}
	}
	// And `py/tests` importing `greeter` by top-level name, which is the same mechanism at the
	// repository's own package root. It was in the expected-unresolved list before this fix, as
	// an accepted gap rather than a boundary.
	if !imports("py/tests", "py/greeter") {
		t.Error("no imports edge py/tests -> py/greeter. `from greeter import greet` resolves " +
			"against py/, which is a package root because py/pyproject.toml declares one")
	}

	// The negatives, and they are what a repo-wide root list fails. The specifier is identical
	// in both handlers, so only the scope of the root distinguishes them.
	for _, c := range []struct{ from, to string }{
		{"py/services/alpha", "py/services/beta/api"},
		{"py/services/beta", "py/services/alpha/api"},
	} {
		if imports(c.from, c.to) {
			t.Errorf("imports edge %s -> %s. These two packages each declare their own "+
				"pyproject.toml and neither declares the other, so a root governing the whole "+
				"repository routed one package's `api.client` into the other's code — structure "+
				"that does not exist, reported with the confidence of something extracted",
				c.from, c.to)
		}
	}
}

// TestCorpusCountsWhatItCannotClassify is the regression for the coverage hole that had no
// counterpart anywhere in the pipeline.
//
// `ClassOther` is what discovery assigns to a file it cannot name. Every other class routes to
// an extractor or a manifest reader, and the two that can still come back empty-handed say so
// — `extract.RunResult` and `manifest.RunResult` each carry an `Unhandled` map that the
// coverage report prints. `ClassOther` had no such counterpart: it was written in one place
// and read in none, so a file landing there left the pipeline with nothing recording that it
// had existed.
//
// Found on a repository whose entire landing page was two `.astro` files. The report named
// `.sh` and `.sql` — both of which are in `sourceExts` as `LangOther`, so extraction counts
// them — and said nothing about the pages, while the bundle described that workspace as a
// one-file JavaScript module built from `astro.config.mjs`. Every one of the seven extractors
// still to be written adds an extension to `sourceExts`, which is why this is fixed before
// them rather than after: each would otherwise widen the same hole in a new place.
//
// Asserted on the count and then on the names, for the reason TestCorpusResolvesExactlyWhatIt
// Should gives: the printed list truncates to the six most frequent, so a substring check
// would silently stop covering whichever entry sorts last. The count is what fails in both
// directions — a reader that stopped counting lowers it, and one that counted classified files
// raises it.
func TestCorpusCountsWhatItCannotClassify(t *testing.T) {
	dir := corpusRepo(t)
	// No -quiet: the coverage report is on stderr and -quiet is what suppresses it.
	_, stderr, code := invoke(t, "build", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("build failed: exit = %d\n%s", code, stderr)
	}

	// Two `.astro` and not one, so the count cannot be satisfied by an implementation that
	// reports an extension once however many files carry it.
	const wantFiles = 4
	wantKeys := []string{".astro (2)", ".svg (1)", "license (1)"}

	got, ok := unclassifiedCount(stderr)
	if !ok {
		t.Fatalf("no `no recognised kind` line in the coverage report. The corpus holds an "+
			"unclassified .astro, .svg and LICENSE, so silence here means discovery is "+
			"claiming to have accounted for files nothing read:\n%s", stderr)
	}
	if got != wantFiles {
		t.Errorf("%d file(s) of no recognised kind, want %d.\n\nHigher means a classified file "+
			"is being reported as unread — a README, a manifest, or a binary, any of which "+
			"makes this line fire on every repository and teaches people to skip it. Lower "+
			"means a genuinely unread file stopped being counted, which is the defect this "+
			"asserts against.\n\nExpected: %s\n\nReport:\n%s",
			got, wantFiles, strings.Join(wantKeys, ", "), stderr)
	}
	line := coverageLine(stderr, "no recognised kind")
	for _, want := range wantKeys {
		if !strings.Contains(line, want) {
			t.Errorf("the unclassified line does not name %q: %q", want, line)
		}
	}

	// The negative half, and the half that decides whether the line is worth printing. Each of
	// these is classified, routed, and read; a method keyed on "did a reader produce facts"
	// rather than on the classification would report all of them. `web/README.md` sits in the
	// same directory as the two .astro files precisely so an implementation that counted by
	// directory fails here.
	for _, notWanted := range []string{".md", ".go", ".ts", ".py", ".rs", ".toml", ".json", ".yaml", ".png", "codeowners", "makefile"} {
		if strings.Contains(line, notWanted) {
			t.Errorf("%q is named on the unclassified line, but the corpus classifies and reads "+
				"it: %q", notWanted, line)
		}
	}
	// And it stays a separate line from the languages that have no extractor. Folding the two
	// together is how the defect would come back: `.astro` would be counted, printed beside
	// `.sh`, and the distinction between "cannot read this language" and "cannot tell what
	// this file is" — which is the difference between a missing extractor and a missing
	// classification — would be gone.
	if ext := coverageLine(stderr, "no extractor for"); strings.Contains(ext, ".astro") {
		t.Errorf("the unclassified .astro is named on the no-extractor line: %q", ext)
	}
}

// TestCorpusFirstPartyImportsThatReachNoPageAreCounted is the regression for the quietest
// gap the pipeline had.
//
// An import that resolution places *inside* this repository and finds no node at is a missing
// edge. The resolver's two decisions about it are both correct — the specifier is first-party,
// and inventing an external dependency for it would report a package nobody publishes, which
// is the one thing the resolver must never do. What was missing was any record that the edge
// had gone: `addImportEdges` counted a specifier only when it was *not* internal, so the
// internal branch was empty and a module whose every import landed there read as importing
// nothing at all.
//
// This is not a small class. The tsconfig `paths` gap was 542 absent edges on one repository
// with no line anywhere admitting them, and a first-party import reaching nothing is what every
// missing resolution root looks like from the outside. The distinction from `Unresolved` is the
// point of the separate count and not presentation: an unresolved specifier is a name signpost
// could not place, and the fix is a resolver that knows about it; an unlinked one it placed
// exactly, and the fix is either nothing at all — generated code is genuinely not there — or a
// reader for whatever is at that path. Merging the two loses which of those a reader is looking
// at.
//
// The corpus carries one deliberate case per shape, and they are deliberately in different
// languages because the branch is per-language:
//
//   - go `example.com/corpus/greeter/internal/generated` — inside the declared module, at a
//     directory holding a README and no Go file, which is the generated-code shape;
//   - typescript `@corpus/assets/logo.svg` — a tsconfig `paths` pattern that matched exactly and
//     maps onto an asset rather than source.
//
// Both must be counted here and neither may appear among the unresolved, since there is nothing
// for anyone to go and declare.
func TestCorpusFirstPartyImportsThatReachNoPageAreCounted(t *testing.T) {
	dir := corpusRepo(t)
	// No -quiet: the coverage report is on stderr and -quiet is what suppresses it.
	_, stderr, code := invoke(t, "build", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("build failed: exit = %d\n%s", code, stderr)
	}

	want := []string{
		"go example.com/corpus/greeter/internal/generated",
		"typescript @corpus/assets/logo.svg",
	}
	got, ok := unlinkedCount(stderr)
	if !ok {
		t.Fatalf("no `reached no page` line in the coverage report. The corpus imports %d "+
			"first-party specifiers that resolve to a real in-repo location holding no node, so "+
			"silence here means those edges went missing with nothing recording it — the defect "+
			"this asserts against:\n\n%s", len(want), stderr)
	}
	if got != len(want) {
		t.Errorf("%d unlinked specifier(s), want %d.\n\nHigher means an import that should reach "+
			"a page does not, and the new one is a real missing edge — the count is the only "+
			"assertion here that catches that, since every edge assertion in this file names the "+
			"edges it expects and cannot notice an absent one nobody listed. Lower means one of "+
			"the two below stopped being counted, or was resolved to something it should not "+
			"reach.\n\nThe %d expected:\n  %s\n\nReport:\n%s",
			got, len(want), len(want), strings.Join(want, "\n  "), stderr)
	}
	line := coverageLine(stderr, "reached no page")
	for _, w := range want {
		if !strings.Contains(line, w) {
			t.Errorf("the unlinked line does not name %q: %q", w, line)
		}
	}

	// The negative boundary, and the half that makes the count mean something. Each of these
	// resolves to a real page, so a branch that counted every first-party import rather than
	// only the ones reaching nothing would name them here — and that version of the counter
	// fires on every healthy repository, which teaches people to ignore the line.
	//
	// `example.com/corpus/greeter/greeter` is the one that matters most: it sits in the same
	// import block as the unlinked specifier, in the same module, and differs only in there
	// being Go files at the end of it.
	for _, notWanted := range []string{
		"example.com/corpus/greeter/greeter",
		"@corpus/entry",
		"@corpus/core",
		"api.client",
		"./greeter",
	} {
		if strings.Contains(line, notWanted) {
			t.Errorf("%q is reported as reaching no page, but it resolves to a module page in "+
				"this corpus: %q", notWanted, line)
		}
	}
	// And the other direction: an unlinked specifier is not an unresolved one. Nothing about
	// `internal/generated` is undeclared — it is inside the module — so asking a reader to go
	// and declare it is the wrong instruction, and a single merged map is what would give it.
	if u := coverageLine(stderr, "import(s) unresolved"); strings.Contains(u, "internal/generated") ||
		strings.Contains(u, "logo.svg") {
		t.Errorf("a first-party import that reached no page is reported as unresolved. The two "+
			"are different facts with different fixes: unresolved means signpost could not "+
			"place the name, unlinked means it placed it exactly and found nothing there. "+
			"Unresolved line: %q", u)
	}
	// The stdlib is in neither, which is the third state. `fmt`, `os`, and `std::fmt` resolve
	// to nothing on purpose — they are the runtime, and a gap reported for them is noise that
	// buries the two real entries above.
	for _, rt := range []string{" fmt", " os", "std::fmt", "node:fs"} {
		if strings.Contains(line, rt) {
			t.Errorf("%q is on the unlinked line; it is the language runtime and reaches no page "+
				"by design: %q", rt, line)
		}
	}
}

// TestCorpusPageNamesSurviveAnUnrelatedEdit is the ID-stability assertion, at the level where
// the cost is actually paid: a committed bundle.
//
// A page's name is its node ID (ADR 0003), and every other page links to it by that name. So a
// renamed page is not one file moving — it is that file, plus every page linking to it, rewritten
// in the diff of a commit that may not have touched the directory at all. Nothing in the graph is
// wrong afterwards, which is exactly why this needs asserting: `verify` passes, the tests pass,
// and the only symptom is a reviewer looking at forty changed pages for a one-directory change and
// having no way to tell which of them mean something.
//
// The corpus is the right place for it because it is the shape that triggers it. Four directories
// here are called `src` and two are called `api` or `greeter`, which is ordinary for a polyglot
// repository and is what the old positional counter numbered — so the name a page got depended on
// how many same-named directories sorted ahead of it, and adding one anywhere renumbered the rest.
//
// The edit below adds a Go package in a directory called `src` — a new member of the largest
// collision group, and the shape that renumbered every later member. It sorts ahead of the
// TypeScript and Rust `src` directories, which under a counter is the worst position: it took a
// number that already belonged to somebody and pushed every one after it along by one.
func TestCorpusPageNamesSurviveAnUnrelatedEdit(t *testing.T) {
	pageNamesFor := func(dir string) map[string]string {
		byTitle := map[string]string{}
		for name, body := range bundlePages(t, dir) {
			if strings.HasPrefix(name, "modules/") {
				byTitle[frontmatterTitle(body)] = name
			}
		}
		return byTitle
	}

	before := pageNamesFor(buildCorpus(t))
	// The fixture has to actually collide, or this test passes on a corpus that cannot show the
	// defect. Two directories sharing a page name is what makes one of them suffixed.
	if before["rust/src"] == before["ts/src"] || before["rust/src"] == "" || before["ts/src"] == "" {
		t.Fatalf("the corpus no longer has two `src` directories with distinct pages, so this "+
			"test covers nothing: rust/src=%q ts/src=%q", before["rust/src"], before["ts/src"])
	}

	dir := corpusRepo(t)
	// `go/src`, so it sorts ahead of `rust/src` and `ts/src`.
	pkg := filepath.Join(dir, "go", "src")
	if err := os.MkdirAll(pkg, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "telemetry.go"),
		[]byte("// Package telemetry is added by a test to stand for an ordinary new package.\npackage telemetry\n\n// Count does nothing.\nfunc Count() int { return 0 }\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--quiet", "-m", "add a package")
	if _, stderr, code := invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir); code != 0 {
		t.Fatalf("build after the edit: exit = %d\n%s", code, stderr)
	}
	after := pageNamesFor(dir)

	if after["go/src"] == "" {
		t.Fatalf("the added package got no page, so the edit under test did not happen. Pages:\n  %s",
			strings.Join(sortedTitles(after), "\n  "))
	}
	for title, name := range before {
		if got := after[title]; got != name {
			t.Errorf("%s moved from %s to %s. Nothing in that directory changed — a Go package was "+
				"added elsewhere — so this rename rewrites its page and every page linking to it in "+
				"a commit that has nothing to do with it",
				title, name, got)
		}
	}
}

// sortedTitles lists a title-keyed map's keys, for failure messages.
func sortedTitles(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k+" -> "+m[k])
	}
	sort.Strings(out)
	return out
}

// unlinkedCount reads the specifier count out of the `reached no page` line.
//
// The specifier count and not the import count, for the reason unresolvedCount gives: one
// unplaceable name imported from forty files is one gap in the map, not forty.
func unlinkedCount(stderr string) (int, bool) {
	for _, line := range strings.Split(stderr, "\n") {
		var imports, specifiers int
		if _, err := fmt.Sscanf(strings.TrimSpace(line),
			"%d first-party import(s) reached no page across %d specifier(s):",
			&imports, &specifiers); err == nil {
			return specifiers, true
		}
	}
	return 0, false
}

// unclassifiedCount reads the file count out of the `no recognised kind` line.
//
// The file count and not the number of distinct extensions, because the question the line
// answers is how much of the repository went unread. One extension covering forty files is a
// bigger hole than four extensions covering one each.
func unclassifiedCount(stderr string) (int, bool) {
	for _, line := range strings.Split(stderr, "\n") {
		var files int
		if _, err := fmt.Sscanf(strings.TrimSpace(line),
			"%d file(s) of no recognised kind:", &files); err == nil {
			return files, true
		}
	}
	return 0, false
}

// unresolvedCount reads the specifier count out of a coverage report on stderr.
//
// The specifier count, not the import count: the two differ once one unresolvable name is
// imported from several files, and the specifier count is the one that says how many distinct
// things the map does not know.
func unresolvedCount(stderr string) (int, bool) {
	for _, line := range strings.Split(stderr, "\n") {
		var imports, specifiers int
		if _, err := fmt.Sscanf(strings.TrimSpace(line),
			"%d import(s) unresolved across %d specifier(s):", &imports, &specifiers); err == nil {
			return specifiers, true
		}
	}
	return 0, false
}

// sortedPageNames lists the bundle's pages, for a failure message that says what was there.
func sortedPageNames(pages map[string]string) []string {
	out := make([]string, 0, len(pages))
	for rel := range pages {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// TestCorpusSecondBuildDoesNotAnalyseTheBundle is the regression for the inflated census.
//
// The bug, found the first time signpost was pointed at somebody else's repository: the
// bundle is committed (ADR 0005), so it is not excluded by the .gitignore rules that cover an
// ordinary build directory, and the *second* run walked the pages the first run wrote. The
// census on stderr went from `analysed 141 files` to `analysed 223` on a repository with 143
// tracked files, the difference being the 82 pages of the previous run.
//
// The graph was never wrong — a bundle page produces no node — which is precisely why this
// needed a stage here. A test asserting nodes and edges is green through the whole defect. The
// number that moved is the one a user has for judging whether the map covers their repository,
// and it moved in the direction that reads as *better* coverage, growing every time the bundle
// grew. Design §4.2: unmeasured must not render as measured, and neither must self-measured.
//
// Two runs are the whole mechanism, so this builds from a bare repository rather than
// through buildCorpus — the first build has to be one this test can measure, or the
// inflation has already happened before the first assertion runs.
func TestCorpusSecondBuildDoesNotAnalyseTheBundle(t *testing.T) {
	dir := corpusRepo(t)

	// No -quiet: the census is on stderr and -quiet is what suppresses it.
	_, first, code := invoke(t, "build", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("the first build failed: exit = %d\n%s", code, first)
	}
	pages := len(bundlePages(t, dir))
	_, second, code := invoke(t, "build", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("the second build failed: exit = %d\n%s", code, second)
	}

	// The assertion is that the number does not move, not what the number is. A count
	// assertion would go red on every extractor improvement, which trains people to update
	// the number rather than read the diff.
	before, after := analysedCount(t, first), analysedCount(t, second)
	if before != after {
		t.Errorf("the file census moved between two builds of an unchanged tree: %d then %d, "+
			"a difference of %d against a bundle of %d page(s). The bundle is committed, so "+
			"it is not excluded by .gitignore, and a run that walks the pages the last run "+
			"wrote reports its own output as repository content.",
			before, after, after-before, pages)
	}
	// No page describes the bundle either. The census is the visible symptom; a module page
	// for `.signpost` would be the one that reaches a committed artifact.
	for rel := range bundlePages(t, dir) {
		if strings.Contains(rel, "signpost") {
			t.Errorf("the bundle got a page describing itself: %s", rel)
		}
	}
}

// analysedCount reads the file count out of a coverage report on stderr.
func analysedCount(t *testing.T, stderr string) int {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, "analysed ") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(line, "analysed %d files:", &n); err != nil {
			t.Fatalf("could not read the file count from %q: %v", line, err)
		}
		return n
	}
	t.Fatalf("no coverage report on stderr, so this test asserts nothing:\n%s", stderr)
	return 0
}

// corpusToCRLF rewrites every page in the corpus bundle the way git materialises it under
// core.autocrlf=true, and returns how many files it touched.
//
// The mechanism that produces the checkout signpost has to read: a repository storing LF
// blobs, cloned on Windows by a git configured to convert on checkout. Done by hand rather
// than by asking git, because the conversion depends on the *developer's* git config — a test
// that relied on it would pass or fail based on the machine rather than on the code, and on
// this machine it would be silently skipped by the `core.autocrlf false` corpusRepo sets to
// keep the determinism assertions honest.
//
// manifest.json is converted too. It is compared by verify after a JSON unmarshal, which is
// whitespace-insensitive, so it is not part of the bug — and that is exactly why it belongs
// here. A future change that started comparing the manifest's bytes would break under a CRLF
// checkout, and this is the only place that would say so.
func corpusToCRLF(t *testing.T, dir string) int {
	t.Helper()
	root := filepath.Join(dir, okf.BundleDir)
	n := 0
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") && !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		b, err := os.ReadFile(p) // #nosec G304 -- a path from a walk of the bundle this test wrote
		if err != nil {
			return err
		}
		// Normalised first, so a file that already carries CRLF does not become CR CR LF.
		s := strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n", "\r\n")
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			return err
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("converting the bundle to CRLF: %v", err)
	}
	if n == 0 {
		t.Fatal("no bundle file was converted, so this test asserts nothing")
	}
	return n
}

// TestCorpusCRLFCheckoutIsUpToDate is the regression for the CRLF false-staleness bug, at the
// level the unit tests in internal/okf cannot reach.
//
// The bug: every comparison signpost makes is a byte comparison against freshly generated
// content, and the emitter writes LF. A checkout that materialised the bundle with CRLF
// therefore differs on every line of every page, and three things concluded the wrong thing
// at once — `verify` called every page stale with a remedy that could not fix it, `build`
// rewrote every page, and `build` reported human notes on a bundle nobody had edited.
//
// Asserted here rather than only in internal/okf because of *why* it shipped: signpost's own
// .gitattributes pins `* text=auto eol=lf`, so the one repository the tool is developed in is
// the one repository configured to hide this. Every other repository is unconfigured on the
// first day signpost runs in it. The corpus is a repository signpost did not write, and this
// stage puts it in the state a Windows clone arrives in.
//
// All three symptoms are asserted, not just the verify one. They share a root cause today,
// and a partial fix that only satisfied verify would leave a user reading a page count and a
// notes count that are both fabricated.
func TestCorpusCRLFCheckoutIsUpToDate(t *testing.T) {
	dir := buildCorpus(t)
	before := bundlePages(t, dir)
	converted := corpusToCRLF(t, dir)

	// verify: the bundle is byte-identical to what a build produces, once the line endings
	// its checkout chose are read as the transport encoding they are.
	_, stderr, code := invoke(t, "verify", "--quiet", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Errorf("verify rejected a CRLF checkout of a bundle signpost had just written "+
			"(%d file(s) converted): exit = %d\n%s", converted, code, stderr)
	}

	// build: nothing updated, and — the symptom that mattered most — no claim of human
	// notes. Nothing outside a managed region was edited, so a non-zero count here is the
	// run inventing the number that exists to tell a user their writing was kept.
	stdout, stderr, code := invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("build failed on a CRLF checkout: exit = %d\n%s", code, stderr)
	}
	if strings.Contains(stdout, "had human notes") {
		t.Errorf("build claimed human notes on a bundle with no human edits:\n%s", stdout)
	}
	// Asserted as "nothing was written" rather than against a page count. The count in the
	// report includes manifest.json and bundlePages does not, so matching numbers would be
	// asserting on which files the helper walks — and it would go red on the next page the
	// emitter adds, which trains people to update the number instead of reading the diff.
	if !strings.Contains(stdout, "0 created, 0 updated") {
		t.Errorf("build rewrote pages on a CRLF checkout that was already correct:\n%s", stdout)
	}

	// And the bytes on disk are still the checkout's. A page whose content matches is not
	// rewritten, so signpost does not convert a repository's line endings behind its back —
	// it normalises to *compare*, which is what keeps page.go's invariant intact.
	after := bundlePages(t, dir)
	for rel, src := range after {
		if !strings.Contains(src, "\r\n") {
			t.Errorf("%s lost the line endings its checkout chose: build rewrote a page it "+
				"had just reported as unchanged", rel)
		}
		if normalizeCRLF(src) != normalizeCRLF(before[rel]) {
			t.Errorf("%s changed content on a CRLF checkout", rel)
		}
	}
}

// normalizeCRLF is the test's own copy of the normalisation under test.
//
// Deliberately not internal/okf's, which is unexported and is the code this file is checking.
// A test that compared using the function it is testing would pass by construction.
func normalizeCRLF(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// TestCorpusCRLFCheckoutStillFailsOnRealDrift is the other half, and without it the test
// above is satisfied by a fix that stops checking.
//
// Normalising line endings must not normalise away a difference in what the bundle *says*. So
// the same CRLF checkout gets one sentence changed inside a managed region — a real edit to
// generated prose, of exactly the kind a rebuild would restore — and verify must still fail.
func TestCorpusCRLFCheckoutStillFailsOnRealDrift(t *testing.T) {
	dir := buildCorpus(t)
	corpusToCRLF(t, dir)

	// The index's own managed region, edited in place. Chosen because index.md exists in
	// every bundle regardless of which extractors ran, so this does not go quietly green if
	// the corpus fixture changes shape.
	full := filepath.Join(dir, okf.BundleDir, okf.IndexPage)
	b, err := os.ReadFile(full) // #nosec G304 -- the bundle this test wrote
	if err != nil {
		t.Fatal(err)
	}
	marker := "<!-- signpost:managed:"
	i := strings.Index(string(b), marker)
	if i < 0 {
		t.Fatalf("%s has no managed region, so this test cannot introduce drift", okf.IndexPage)
	}
	j := strings.Index(string(b)[i:], "\r\n")
	if j < 0 {
		t.Fatalf("%s was not converted to CRLF", okf.IndexPage)
	}
	drifted := string(b)[:i+j+2] + "Not what the graph says.\r\n" + string(b)[i+j+2:]
	if err := os.WriteFile(full, []byte(drifted), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := invoke(t, "verify", "--quiet", "-repo", "example.com/corpus", dir)
	if code == 0 {
		t.Fatalf("verify passed on a CRLF checkout whose managed region had been edited. "+
			"Normalising line endings must not normalise away a change in what the bundle "+
			"says.\n%s", stderr)
	}
}

// TestCorpusStalePageIsRemovedOrReported is the regression for issue #10 at the level the unit
// tests cannot reach: a real repository, a real bundle, both commands, and both boundaries.
//
// The bug: build wrote and updated but never deleted, and strict verify exited 0 with the orphan
// present. In the field that showed up as a run reporting `342 page(s): 0 created, 342 updated,
// 0 unchanged` against a directory holding 344 files, with `counts.nodes` at 339 — three pages
// describing modules that were not there, each carrying plausible edges and a `resource:` naming
// a commit where the code really did exist. That reads as authoritative, which makes it more
// expensive than a missing page, and every gate was green.
//
// Both boundaries are asserted in one stage because the defect is the *pair*: a fix that deletes
// unconditionally passes the positive half and destroys a human's notes on the first rename, and
// a fix that deletes nothing passes the negative half and is the shipped bug. Neither assertion
// means anything without the other.
func TestCorpusStalePageIsRemovedOrReported(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)
	// A page signpost itself wrote, copied to a name no node has — what a renamed or deleted
	// directory leaves behind. Copied rather than hand-written: a fixture skeleton can drift from
	// what the emitter produces, and then this passes while build has stopped pruning.
	donor := sortedPageNames(pages)[len(pages)-1]
	const (
		skeleton = "modules/ghost-skeleton.md"
		written  = "modules/ghost-annotated.md"
	)
	plant := func(rel, extra string) {
		t.Helper()
		full := filepath.Join(dir, okf.BundleDir, filepath.FromSlash(rel))
		if err := os.WriteFile(full, []byte(pages[donor]+extra), 0o600); err != nil {
			t.Fatalf("planting %s: %v", rel, err)
		}
	}
	plant(skeleton, "")
	plant(written, "\nKeep this: the code moved to services/identity.\n")

	// verify, before any rebuild. The severities differ because the remedies differ, and that is
	// the property under test — a failure whose fix is `signpost build`, and a warning for the
	// page no command can resolve.
	stdout, stderr, code := invoke(t, "verify", "--quiet", "-repo", "example.com/corpus", dir)
	if code == 0 {
		t.Errorf("verify passed with a page describing a concept the repository does not have:\n%s",
			stdout)
	}
	if !strings.Contains(stdout, skeleton) {
		t.Errorf("verify did not name %s:\n%s%s", skeleton, stdout, stderr)
	}
	if !strings.Contains(stdout, written) {
		t.Errorf("verify did not mention %s, which a build keeps and only a human can "+
			"resolve:\n%s%s", written, stdout, stderr)
	}

	stdout, stderr, code = invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("build: exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "1 page(s) removed") || !strings.Contains(stdout, skeleton) {
		t.Errorf("build did not report removing %s by name:\n%s", skeleton, stdout)
	}
	if !strings.Contains(stdout, "so they were kept") || !strings.Contains(stdout, written) {
		t.Errorf("build did not report keeping %s:\n%s", written, stdout)
	}

	after := bundlePages(t, dir)
	if _, ok := after[skeleton]; ok {
		t.Errorf("%s survived a rebuild, so the bundle still describes a concept that is gone",
			skeleton)
	}
	// The negative boundary, and the reason build is allowed to delete at all. The sentence has to
	// still be there, byte for byte — a fix that kept the file and rewrote it would lose the same
	// thing more quietly.
	if src, ok := after[written]; !ok {
		t.Errorf("%s was deleted: a rename must not take somebody's notes with it", written)
	} else if !strings.Contains(src, "Keep this: the code moved to services/identity.") {
		t.Errorf("%s lost the human sentence:\n%s", written, src)
	}
	// Every page the run legitimately writes is still there. A sweep that over-reached would
	// satisfy every assertion above while deleting the bundle around them.
	for rel := range pages {
		if _, ok := after[rel]; !ok {
			t.Errorf("%s was deleted and it describes a concept the repository has", rel)
		}
	}

	// And what is left is a bundle whose only remaining finding is the one a human owns. Asserted
	// because the gate is the deliverable: a prune that fixed the pages and left manifest.json
	// listing the deleted one would trade one wrong claim for another.
	stdout, stderr, code = invoke(t, "verify", "--quiet", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Errorf("verify still fails after the rebuild that was its stated remedy: exit = %d\n%s",
			code, stderr)
	}
	if !strings.Contains(stdout, written) {
		t.Errorf("the kept page stopped being reported, so nothing tells a human it is theirs "+
			"to resolve:\n%s%s", stdout, stderr)
	}
}

// TestCorpusVendoredCodeIsOffTheMapUntilAskedFor is issue #11.
//
// `-include-vendored` promised to "analyse vendored third-party code instead of only recording
// it", and it read the files: `File.Content` was populated, and the census on stderr counted
// them. Nothing downstream ever looked at them. Six consumers each filtered on `File.Vendored`
// with no reference to the option — `Sources()` being the one that decided it, since extraction
// is driven from there — so the flag moved the file count and the node count stayed where it
// was. The sibling flag `-include-fixtures` worked, and that asymmetry is what surfaced this.
//
// It belongs in the corpus rather than only in internal/discover for the reason the harness
// exists: nothing in *this* repository is vendored, so no amount of dogfooding reaches the
// filters. The corpus carries `ts/node_modules/@corpus-vendor/logger`, which is a committed
// node_modules — a real pattern, and one .gitignore does not exclude.
//
// The assertion is the *bundle*, not the file count, because the file count is what the broken
// flag already moved. A page for the vendored module is what analysing it means.
func TestCorpusVendoredCodeIsOffTheMapUntilAskedFor(t *testing.T) {
	const (
		vendoredModule = "logger"
		// Declared only by the vendored package.json, so it can reach a bundle by no other
		// route. This is what separates "the manifest reader ran" from "the walk read a file".
		vendoredDep = "vendored-only-tinycolor"
	)

	// The negative boundary first, and it is the one that carries the weight. Vendored code is
	// somebody else's, unchangeable by this team, and a bundle that puts it on the map swamps
	// the graph with nodes nobody can act on and claims a dependency the repository does not
	// declare. A fix that threaded the option too widely — or defaulted it on — ships that.
	off := bundlePages(t, buildCorpus(t))
	for rel, src := range off {
		if strings.Contains(rel, vendoredModule) || strings.Contains(rel, vendoredDep) {
			t.Errorf("%s describes vendored third-party code that nothing asked signpost to "+
				"analyse. A committed node_modules is not this repository's surface:\n%s", rel, src)
		}
		if strings.Contains(src, vendoredDep) {
			t.Errorf("%s cites %s, which only the vendored package.json declares:\n%s",
				rel, vendoredDep, src)
		}
	}

	// And the positive: with the flag, both halves arrive. They fail independently, which is why
	// both are named and why one stage asserts both — measured, not assumed. Reverting
	// `Sources()` alone loses the module page and leaves the citation intact; reverting
	// manifest.Registry.Run alone does the reverse. So a fix to the extraction half analyses
	// the vendored source and still discards the package.json beside it, leaving a module
	// whose own declaration signpost read and threw away.
	on := bundlePages(t, buildCorpus(t, "-include-vendored"))
	var gotModule, gotDep string
	for rel, src := range on {
		if strings.Contains(rel, vendoredModule) {
			gotModule = rel
		}
		if strings.Contains(src, vendoredDep) {
			gotDep = rel
		}
	}
	if gotModule == "" {
		t.Errorf("-include-vendored produced no page for the vendored module. This is issue #11: "+
			"the walk honoured the flag, every consumer filtered the result out again, and the "+
			"flag changed the file count and nothing else.\n\nPages:\n  %s", pageNames(on))
	}
	if gotDep == "" {
		t.Errorf("-include-vendored did not read the vendored package.json: nothing in the bundle "+
			"names %s, which only that file declares. Sources() and ByClass() fail separately, so "+
			"the extraction half can be fixed with this half still broken.\n\nPages:\n  %s",
			vendoredDep, pageNames(on))
	}
	// Turning the flag on adds; it must not take anything away. Every page the default build
	// wrote is still written, which is what distinguishes "analyse vendored code as well" from
	// "analyse a different repository".
	for rel := range off {
		if _, ok := on[rel]; !ok {
			t.Errorf("%s is in the default bundle and absent with -include-vendored", rel)
		}
	}
}

// The .signpost.yml reader, on a repository that has one. ADR 0011's whole claim is that a
// committed file changes the *bundle* — so the assertion is the bundle, through the binary, on a
// tree where a key has something to act on.
//
// This is not what cmd/signpost/config_test.go covers. Those tests drive precedence on a
// two-module Go fixture, where `include_vendored` has nothing vendored to include: the fixture
// grows a node_modules for the occasion. The corpus already carries a committed
// `ts/node_modules/@corpus-vendor/logger`, which is the condition, and the same key reaching the
// same place through the file rather than the flag is a second path to a filter that has already
// failed once — issue #11's six consumers filtered on `File.Vendored` with no reference to the
// option, so the flag moved the file count and nothing else.
func TestCorpusConfigShapesTheBundle(t *testing.T) {
	const (
		vendoredModule = "logger"
		vendoredDep    = "vendored-only-tinycolor"
	)

	// Written into the copy, then committed, because that is how the file arrives in a real
	// repository — and because an uncommitted file would leave the tree dirty for the history
	// pass to describe.
	dir := corpusRepo(t)
	writeConfig(t, dir, "include_vendored: true\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--quiet", "-m", "configure signpost")

	if _, stderr, code := invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir); code != 0 {
		t.Fatalf("build: exit = %d\n%s", code, stderr)
	}
	pages := bundlePages(t, dir)

	var gotModule, gotDep string
	for rel, src := range pages {
		if strings.Contains(rel, vendoredModule) {
			gotModule = rel
		}
		if strings.Contains(src, vendoredDep) {
			gotDep = rel
		}
	}
	// Both halves, for the reason TestCorpusVendoredCodeIsOffTheMapUntilAskedFor names: the
	// extraction half and the manifest half fail separately, so a key that reached only one of
	// them produces a module page whose own declaration signpost read and threw away.
	if gotModule == "" {
		t.Errorf("include_vendored: true in %s produced no page for the vendored module. The key "+
			"reaches the walk and no further, which is issue #11 by a second route.\n\nPages:\n  %s",
			config.File, pageNames(pages))
	}
	if gotDep == "" {
		t.Errorf("include_vendored: true in %s did not reach the manifest reader: nothing names %s, "+
			"which only the vendored package.json declares.\n\nPages:\n  %s",
			config.File, vendoredDep, pageNames(pages))
	}

	// The negative boundary, and it is the half that fails if the reader stops reading values:
	// the same file saying false must produce the default bundle. Asserted on the same tree, so
	// the only difference between the two runs is the one line.
	//
	// Rebuilt in place rather than on a fresh copy, which makes this stricter than a comparison
	// of two independent builds: the pages the first build wrote are still on disk, so a page for
	// the vendored module that the second build fails to *remove* fails here too. That is the
	// stale-page path, and it is the concrete way "the config was honoured" and "the bundle
	// reflects the config" come apart.
	writeConfig(t, dir, "include_vendored: false\n")
	if _, stderr, code := invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir); code != 0 {
		t.Fatalf("the second build failed: exit = %d\n%s", code, stderr)
	}
	for rel, src := range bundlePages(t, dir) {
		if strings.Contains(rel, vendoredModule) {
			t.Errorf("%s survives include_vendored: false. Either the value was ignored and any "+
				"present key reads as true, or the page it wrote before was not removed", rel)
		}
		if strings.Contains(src, vendoredDep) {
			t.Errorf("%s cites %s with include_vendored: false", rel, vendoredDep)
		}
	}
}

// A repository cannot weaken its own gate by committing a file — ADR 0011's second class, on the
// tree where it would be attempted. The clause erodes by somebody adding a key later, so the
// assertion is that the build *stops*: exit 2, naming the key, rather than a bundle built to a
// quieter standard.
//
// Beside the stage above rather than only in internal/config because the reader returning an
// error and the command exiting 2 are separate facts, and the corpus is where the second one is
// asserted on a repository somebody could plausibly commit this to.
func TestCorpusGateWeakeningConfigStopsTheBuild(t *testing.T) {
	dir := corpusRepo(t)
	writeConfig(t, dir, "# make CI quieter\nfail_on_cycle: false\n")

	_, stderr, code := invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir)
	if code != 2 {
		t.Fatalf("exit = %d, want 2: a committed file turned off a check\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "fail_on_cycle") {
		t.Errorf("the error does not name the key:\n%s", stderr)
	}
	// No bundle, which is the part that matters. A build that reported the error and wrote pages
	// anyway would leave CI comparing against a bundle nobody's configuration produced.
	if _, err := os.Stat(filepath.Join(dir, okf.BundleDir)); !os.IsNotExist(err) {
		t.Errorf("a bundle was written despite the refused key: %v", err)
	}
}

// TestCorpusEveryLinkResolvesAndSurvivesRelocation is the regression test for the bug that every
// intra-bundle link was root-absolute.
//
// `/modules/hook.md` resolves against the *web server root*, so it only worked in a viewer
// that mounts the bundle at `/`. On GitHub — which ADR 0005 names as the entire point of
// committing the bundle, a reader opening `.signpost/index.md` with nothing installed — it
// pointed at `github.com/modules/hook.md` and 404'd. Every generated link in this
// repository's own bundle was broken for that reader, and nothing failed: `verify`'s
// resolver interpreted the same absolute form the emitter wrote, so the two agreed with each
// other and disagreed with every renderer.
//
// So this test deliberately does not use signpost's resolver. It resolves each link the way
// a renderer does — join it to the directory of the page it is written on, and look for that
// file on disk — because a resolver bug is exactly what hid this.
//
// The corpus rather than a unit test because the failure needs shape to appear: pages at the
// bundle root (index, log, practices) linking down into subdirectories, pages under
// `modules/` linking to siblings, and pages linking across to `references/` and
// `interfaces/`. A bundle with one directory would pass with `./` hard-coded.
func TestCorpusEveryLinkResolvesAndSurvivesRelocation(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	// Every file in the bundle, not only the pages: manifest.json is a legitimate target.
	onDisk := map[string]bool{}
	root := filepath.Join(dir, okf.BundleDir)
	if err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
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
		onDisk[filepath.ToSlash(rel)] = true
		return nil
	}); err != nil {
		t.Fatalf("walking the bundle: %v", err)
	}

	checked := 0
	for rel, src := range pages {
		for _, target := range generatedLinks(src) {
			// The positive half of the boundary: an absolute target is the bug, so it fails
			// here rather than being resolved leniently. Checked before resolution because
			// `/index.md` would otherwise resolve fine after trimming and the defect would
			// survive with the test still green.
			if strings.HasPrefix(target, "/") {
				t.Errorf("%s links to %s, which is root-absolute: it resolves against the "+
					"server root, so it 404s on GitHub and in a plain checkout", rel, target)
				continue
			}
			checked++
			// Resolved the way a markdown renderer does, with no help from okf.
			want := path.Clean(path.Join(path.Dir(rel), target))
			if !onDisk[want] {
				t.Errorf("%s links to %s, which resolves to %s — not in the bundle:\n  %s",
					rel, target, want, pageNames(pages))
			}
		}
	}

	// The count matters, per the corpus's negative-boundary rule: a `generatedLinks` that
	// returned nothing would make every assertion above vacuous, and a bundle whose pages
	// stopped linking to each other is the failure ADR 0004's traversability rests on. The
	// bound is loose because the corpus grows; it is the zero and near-zero cases that are
	// being excluded.
	if checked < 20 {
		t.Errorf("only %d generated links were checked across %d pages, so this test is "+
			"asserting almost nothing", checked, len(pages))
	}

	assertBundleSurvivesRelocation(t, pages)
}

// generatedLinks returns the markdown link targets in a page's managed regions, and the
// `to:` targets in its frontmatter edges.
//
// Managed regions only, because those are the links signpost wrote and is accountable for.
// A relative link in somebody's notes may legitimately name a repository file rather than a
// bundle page, which is the same distinction verify draws.
//
// Written here with its own scanner rather than calling into okf so that a bug in okf's link
// parsing cannot hide a bug in okf's link emission.
func generatedLinks(src string) []string {
	var out []string
	inRegion := false
	inFrontmatter := false
	for i, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			// Frontmatter is the leading fenced block only; a `---` later in the body is a
			// horizontal rule.
			if i == 0 {
				inFrontmatter = true
				continue
			}
			if inFrontmatter {
				inFrontmatter = false
			}
			continue
		}
		if inFrontmatter {
			// `- { kind: imports, to: ./x.md, confidence: extracted }`
			if idx := strings.Index(line, " to: "); idx >= 0 {
				rest := line[idx+len(" to: "):]
				if end := strings.IndexAny(rest, ",}"); end >= 0 {
					out = append(out, strings.TrimSpace(rest[:end]))
				}
			}
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "<!-- signpost:managed:"):
			inRegion = true
			continue
		case strings.HasPrefix(trimmed, "<!-- /signpost:managed:"):
			inRegion = false
			continue
		}
		if !inRegion {
			continue
		}
		out = append(out, markdownTargets(line)...)
	}
	return out
}

// markdownTargets pulls `](target)` out of a line, skipping code spans so an example link in
// generated prose is not treated as a link.
func markdownTargets(line string) []string {
	// Blank the contents of inline code spans.
	if strings.Count(line, "`") >= 2 {
		var b strings.Builder
		open := false
		for _, r := range line {
			if r == '`' {
				open = !open
				b.WriteRune(r)
				continue
			}
			if open {
				b.WriteByte(' ')
				continue
			}
			b.WriteRune(r)
		}
		line = b.String()
	}
	var out []string
	for i := 0; i+1 < len(line); i++ {
		if line[i] != ']' || line[i+1] != '(' {
			continue
		}
		rest := line[i+2:]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			break
		}
		target := strings.TrimSpace(rest[:end])
		i += 2 + end
		if sp := strings.IndexAny(target, " \t"); sp >= 0 {
			target = target[:sp]
		}
		if idx := strings.IndexByte(target, '#'); idx >= 0 {
			target = target[:idx]
		}
		if target == "" || strings.Contains(target, "://") {
			continue
		}
		out = append(out, target)
	}
	return out
}

// assertBundleSurvivesRelocation is the fork half of the same property.
//
// A relative link names no root, so moving the whole bundle cannot break it. Asserted by
// moving the bundle into a subdirectory and re-resolving every link from there: with the
// absolute form every link breaks, and with the relative form the set of resolved targets is
// unchanged. This is what makes a fork, a subtree merge, or a bundle published under a path
// prefix keep working — the thing the absolute form silently did not do.
//
// A subtree of the test above rather than its own top-level test because it asserts a
// different property of the same artifact, and a corpus build is the expensive part: on a
// Windows host each one costs ~28 seconds of process creation, so a second build to re-read
// bytes this test already holds buys nothing.
func assertBundleSurvivesRelocation(t *testing.T, pages map[string]string) {
	t.Helper()

	// Relocation is a pure path operation on the bundle: every page keeps its bytes, and only
	// the prefix each one sits under changes. So a link that resolves within the moved set
	// resolves in the original, and one that named the old root does not.
	moved := map[string]bool{}
	for rel := range pages {
		moved["docs/knowledge/"+rel] = true
	}

	broken := 0
	for rel, src := range pages {
		from := "docs/knowledge/" + rel
		for _, target := range generatedLinks(src) {
			if !strings.HasSuffix(target, ".md") {
				continue
			}
			if !moved[path.Clean(path.Join(path.Dir(from), target))] {
				broken++
				if broken <= 3 {
					t.Errorf("after moving the bundle under docs/knowledge/, %s no longer "+
						"resolves from %s", target, from)
				}
			}
		}
	}
	if broken > 3 {
		t.Errorf("and %d more links broke on relocation", broken-3)
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
// TestCorpusTelemetryCarriesNoRepositoryContent is the corpus half of ADR 0014's clause on
// span content, and it needs a repository rather than a fixture for the same reason every
// other stage here does: the unit tests in internal/telemetry start spans by hand, so
// "no repository content reaches a span" is asserted there against a tree that has no
// content. The rule is about what happens when a real repository goes through the pipeline.
//
// The corpus is the tree that can violate it. It holds `app/tools/[slug]/page.tsx`,
// `py/greeter/data,notes.py`, a `POSTGRES_PASSWORD` reference in compose.yaml, and internal
// package names — every category of thing that must not leave the machine. A future change
// adding a string attribute to a span, or an error message to Span.Failed, would compile,
// pass the package tests, and fail here.
//
// Both boundaries, in one run. Positive: five named stage spans arrive, so a broken exporter
// cannot satisfy the negative half by sending nothing. Negative: nothing in the payload names
// anything in the tree.
func TestCorpusTelemetryCarriesNoRepositoryContent(t *testing.T) {
	dir := corpusRepo(t)

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("SIGNPOST_ENABLE_TELEMETRY", "1")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", srv.URL+"/v1/traces")
	// Nothing extra in the resource. The build's own environment is what an operator has set,
	// and a resource attribute is attached to every span in the batch — so this stage asserts
	// the default set and TestNoRepositoryContentReachesTheWire covers the override.
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")

	_, stderr, code := invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("build: exit = %d\n%s", code, stderr)
	}
	// Nothing about telemetry on stderr. With a collector answering 200 there is nothing to
	// report, and a note here would mean the exporter is failing on a working endpoint.
	if strings.Contains(stderr, "telemetry:") {
		t.Errorf("the build reported a telemetry fault against a collector answering 200:\n%s",
			stderr)
	}

	mu.Lock()
	payload := strings.Join(bodies, "\n")
	mu.Unlock()
	if payload == "" {
		t.Fatal("the collector received nothing, so this stage asserts nothing about what a " +
			"real repository's spans contain")
	}

	// The positive boundary. Every stage the pipeline runs is named, so an exporter that
	// dropped spans — or a pipeline whose stages stopped being instrumented — fails here
	// rather than passing the content check by sending an empty batch.
	for _, stage := range []string{"analyse", "discover", "extract", "manifests", "history",
		"assemble"} {
		if !strings.Contains(payload, `"name":"`+stage+`"`) {
			t.Errorf("no span named %q reached the collector, so the trace does not answer "+
				"which stage was slow", stage)
		}
	}

	// The negative boundary: nothing the corpus contains appears anywhere in the bytes. Each
	// of these is a real category, not a sample — a path, a path with a YAML indicator in it,
	// an internal package name, a dependency name, a service name, and the name of a secret.
	for _, forbidden := range []string{
		"page.tsx", "[slug]", "data,notes", "greeter", "codec",
		"corpus", "POSTGRES_PASSWORD", "example.com",
		"go.mod", "Cargo.toml", "pyproject", "compose",
	} {
		if strings.Contains(payload, forbidden) {
			t.Errorf("%q from the corpus reached the collector. A span carries stage names, "+
				"counts, and durations; a repository's contents leave the machine only through "+
				"the semantic pass, which is opt-in (ADR 0009, ADR 0014)", forbidden)
		}
	}

	// And the shape, structurally, so a value that is neither a count nor a fixed name fails
	// even when it happens not to match a string above. Asserted on the decoded payload
	// because the check that matters is "every span attribute is an integer", and the corpus's
	// own filenames are exactly what a string attribute would carry.
	for _, body := range bodies {
		var p struct {
			ResourceSpans []struct {
				ScopeSpans []struct {
					Spans []struct {
						Name       string `json:"name"`
						Attributes []struct {
							Key   string         `json:"key"`
							Value map[string]any `json:"value"`
						} `json:"attributes"`
					} `json:"spans"`
				} `json:"scopeSpans"`
			} `json:"resourceSpans"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatalf("the collector received invalid JSON: %v", err)
		}
		for _, rs := range p.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				for _, s := range ss.Spans {
					for _, a := range s.Attributes {
						if !strings.HasPrefix(a.Key, "signpost.") {
							t.Errorf("span %q carries attribute %q, outside signpost's own "+
								"namespace", s.Name, a.Key)
						}
						if _, ok := a.Value["intValue"]; !ok {
							t.Errorf("span %q carries %q = %v, which is not a count — a path "+
								"reaches a span as a string attribute and there is no API on "+
								"telemetry.Span that can produce one", s.Name, a.Key, a.Value)
						}
					}
				}
			}
		}
	}
}

// TestCorpusTelemetryIsOffAndFailsOpen is the other half: the default, and the promise that
// telemetry can never be why a build failed.
//
// Three runs on the same tree. Off, which is what every user gets. Enabled against a
// collector that refuses everything. And enabled against an endpoint nothing is listening
// on — the CI case, where an OTEL_* variable is set for a collector that is not reachable
// from the runner. All three must produce the same bundle and exit 0.
func TestCorpusTelemetryIsOffAndFailsOpen(t *testing.T) {
	var got int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&got, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	// An address that accepts nothing. Closed immediately, so the port is almost certainly
	// free and a connection is refused rather than hanging.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	baseline := bundlePages(t, buildCorpus(t))

	for _, tc := range []struct {
		name     string
		enable   string
		endpoint string
		// posts is whether the collector should see traffic, which is what separates "off"
		// from "on and failing".
		posts bool
	}{
		{name: "off, which is every user", enable: "", endpoint: srv.URL + "/v1/traces"},
		{name: "on, and the collector rejects every batch", enable: "1",
			endpoint: srv.URL + "/v1/traces", posts: true},
		{name: "on, and nothing is listening", enable: "1",
			endpoint: deadURL + "/v1/traces"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			atomic.StoreInt32(&got, 0)
			if tc.enable != "" {
				t.Setenv("SIGNPOST_ENABLE_TELEMETRY", tc.enable)
			}
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", tc.endpoint)

			dir := corpusRepo(t)
			_, stderr, code := invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir)
			if code != 0 {
				t.Fatalf("a telemetry failure changed the exit code: exit = %d\n%s\n"+
					"Telemetry can never be the reason a build failed (ADR 0014 clause 4)",
					code, stderr)
			}

			// The bundle is the deliverable, and it must be unaffected. Byte-identical to the
			// baseline built with telemetry off — a span that leaked into an emitted page, or
			// an exporter that perturbed a map iteration, shows up here.
			pages := bundlePages(t, dir)
			if len(pages) != len(baseline) {
				t.Fatalf("%d page(s) with telemetry %q, %d with it off",
					len(pages), tc.enable, len(baseline))
			}
			for rel, want := range baseline {
				if pages[rel] != want {
					t.Errorf("%s differs from the bundle built with telemetry off", rel)
				}
			}

			if posted := atomic.LoadInt32(&got) > 0; posted != tc.posts {
				if tc.posts {
					t.Errorf("the collector saw no batch, so this case does not exercise a " +
						"failing export")
				} else {
					t.Errorf("a batch was posted with SIGNPOST_ENABLE_TELEMETRY=%q; an "+
						"OTEL_* endpoint in the environment is never sufficient on its own "+
						"(ADR 0009)", tc.enable)
				}
			}
			// A failure that is reported, not swallowed. §4.2: the absence of a measurement
			// must never read as a clean bill of health, so somebody who asked for telemetry
			// and is not getting it learns so.
			if tc.enable != "" && !strings.Contains(stderr, "telemetry:") {
				t.Errorf("telemetry failed silently, so the run got no trace and no reason:\n%s",
					stderr)
			}
			if tc.enable == "" && strings.Contains(stderr, "telemetry:") {
				t.Errorf("a run with telemetry off mentioned it on stderr:\n%s", stderr)
			}
		})
	}
}

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

// TestCorpusBuildsWithNoGitAtAll is the tarball case: somebody sends you a repository as an
// archive, you unpack it, and there is no .git anywhere.
//
// Unlikely and a corner, but the interesting part is what "best effort" has to mean for it to
// be safe. Git is authoritative where it is present — it decides what is tracked, what is
// versioned, and which branch a bundle belongs to — and none of that is signpost's to
// reimplement. What signpost owes a tree with no git is a bundle built from the files, and
// silence on every claim that only a commit could support.
//
// So this stage asserts the degradation is honest in both directions, which is the pair that
// makes it a boundary rather than a smoke test. Positive: the pages that describe the *files*
// are all still there, byte-identical to the ones a git build writes, because nothing about a
// module's structure came from history. Negative: not one page carries a `resource:` or a
// `generated:` key, because both come from the commit and a page stamped with provenance
// nobody can check is worse than an unstamped one. A build that fell back to the clock, to a
// zero sha, or to `git://@` would pass every other test in this file.
//
// The corpus rather than a two-file fixture, because the condition to catch is a reader that
// needs git for something other than history — a resolution root, a file list, an ignore rule
// — and that only shows up across four languages and forty files.
func TestCorpusBuildsWithNoGitAtAll(t *testing.T) {
	// The git build first, as the comparison. Same tree, same flags, so any difference below
	// is history and nothing else.
	withGit := bundlePages(t, buildCorpus(t))

	dir := corpusRepo(t)
	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	// The guard. Without it a change to corpusRepo that stopped creating a repository would
	// make this stage assert nothing while passing.
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git is still there, so this stage is testing a git build: %v", err)
	}

	stdout, stderr, code := invoke(t, "build", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("a tree with no git failed to build: exit = %d\n%s", code, stderr)
	}
	// Said out loud, per §4.2. A bundle with no co-change edges looks identical whether the
	// repository has no coupling or signpost read no history, and the reader would take the
	// first for the second.
	if !strings.Contains(stderr, "history not read: not a git repository") {
		t.Errorf("the build did not say why it read no history:\n%s", stderr)
	}
	if !strings.Contains(stdout, "page(s):") {
		t.Errorf("no pages were reported:\n%s", stdout)
	}

	pages := bundlePages(t, dir)

	// The negative boundary, and the one that matters: no provenance anywhere.
	for _, rel := range sortedPageNames(pages) {
		fm := pages[rel]
		if i := strings.Index(fm, "\n---"); i > 0 {
			fm = fm[:i]
		}
		for _, key := range []string{"resource:", "generated:"} {
			if strings.Contains(fm, key) {
				t.Errorf("%s carries %s with no commit to name, so it claims provenance "+
					"nobody can check:\n%s", rel, key, fm)
			}
		}
	}

	// The positive boundary. Every page a git build wrote about the *files* is still written,
	// with the same name — the ID scheme is content-derived (ADR 0015) and takes nothing from
	// history, so a page that moved or vanished here means a reader reached for git to decide
	// something it should not have.
	for _, rel := range sortedPageNames(withGit) {
		if _, ok := pages[rel]; !ok {
			t.Errorf("%s was written from a git checkout and not from the same tree without "+
				"one. Pages written:\n  %s", rel, pageNames(pages))
		}
	}

	// And verify accepts it, naming the check it could not run. A staleness check that has no
	// commit to compare against and reports "ok" is the false pass verify exists to prevent,
	// so the skip has to be named.
	vout, verr, vcode := invoke(t, "verify", "-repo", "example.com/corpus", dir)
	if vcode != 0 {
		t.Fatalf("verify rejected a bundle built from the same tree it is checking: "+
			"exit = %d\n%s\n%s", vcode, vout, verr)
	}
	if !strings.Contains(vout, "skipped") {
		t.Errorf("verify passed without naming the staleness check as skipped, so an "+
			"unstamped bundle reads as a verified one:\n%s", vout)
	}
}

// TestCorpusSaysNothingPointsAtTheBundle is the adoption gap, asserted on a repository that
// has an AGENTS.md and still does not point at the map.
//
// This is the failure mode a green build cannot show. Every page is correct, verify passes,
// and no agent ever opens the directory — because nothing a model is trained to read mentions
// it. The corpus is the right shape for it precisely because it *does* have an AGENTS.md and a
// README.md, so a check that fired on the file's absence rather than on its content would pass
// here and be useless in the field.
func TestCorpusSaysNothingPointsAtTheBundle(t *testing.T) {
	dir := corpusRepo(t)
	target := okf.BundleDir + "/" + okf.IndexPage
	// The guard: the corpus states rules for agents and points at no map. If it ever gains a
	// pointer, this stage stops testing what it claims to.
	agents, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(agents), target) {
		t.Fatalf("the corpus AGENTS.md now names %s, so this stage cannot reach the "+
			"unpointed case", target)
	}
	// And the condition that made this check precise rather than nearly right. The corpus
	// README mentions `.signpost/` in prose about the harness — a sentence explaining that a
	// build writes one — which is not a pointer at anything. A check keyed on the directory
	// read that as adoption and stayed silent; keyed on the index page it does not. This is
	// asserted rather than commented because the looser rule passed every other stage here.
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), okf.BundleDir) {
		t.Fatalf("the corpus README no longer mentions %s at all, so this stage no longer "+
			"covers prose-that-is-not-a-pointer", okf.BundleDir)
	}
	if strings.Contains(string(readme), target) {
		t.Fatalf("the corpus README now points at %s, so this stage cannot reach the "+
			"unpointed case", target)
	}

	_, stderr, code := invoke(t, "build", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("build: exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "nothing points at the bundle") {
		t.Errorf("a repository whose instructions never name %s was not told so:\n%s",
			okf.BundleDir, stderr)
	}

	// The fix works on this repository: append the stub, rebuild, and the note is gone. The
	// two halves together are what make the note actionable rather than decorative — a
	// suggestion that does not silence the thing suggesting it is a suggestion nobody follows
	// twice.
	stub, _, code := invoke(t, "build", "-suggest-agents-md", dir)
	if code != 0 {
		t.Fatalf("-suggest-agents-md: exit = %d", code)
	}
	rewrite(t, filepath.Join(dir, "AGENTS.md"), string(agents)+"\n"+stub)

	_, stderr, code = invoke(t, "build", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("rebuild: exit = %d\n%s", code, stderr)
	}
	if strings.Contains(stderr, "nothing points at the bundle") {
		t.Errorf("the stub signpost suggested does not satisfy the check that suggested "+
			"it:\n%s", stderr)
	}
}
