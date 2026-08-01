package main

import (
	"encoding/json"
	"fmt"
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
	stdout, stderr, code := invoke(t, "export", "-format", "json", "--quiet", dir)
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
