package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/3rg0n/signpost/internal/config"
	"github.com/3rg0n/signpost/internal/manifest"
	"github.com/3rg0n/signpost/internal/okf"
	"github.com/3rg0n/signpost/internal/view"
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
		{"Java", "jvm/src/main/java/com/example/api"},
		{"Java, a package under another package", "jvm/src/main/java/com/example/store/internal"},
		{"Kotlin", "jvm/src/main/kotlin/com/example/app"},
		// The second source set, and the reason it is asserted by name: it declares
		// `com.example.api` exactly as src/main does, so this page and the Java one above are
		// two directories answering to one package name. A layout that collapsed them would
		// still produce a page for the name — this says both survived as separate modules.
		{"Java, a second source set declaring a package src/main declares too", "jvm/src/integrationTest/java/com/example/api"},
		{"C", "c/src"},
		// C's public headers, which live apart from the implementation they declare. This is
		// the shape the include search path exists for: `c/src/buffer.c` includes
		// `<corpus/buffer.h>`, and nothing but a search rooted at the nearest ancestor's
		// `include/` finds it.
		{"C, the public headers in their own tree", "c/include/corpus"},
		{"C++", "cpp/src"},
		{"C++, the public headers in their own tree", "cpp/include/corpus"},
		// Objective-C, and this row is why the language is asserted by name. The directory
		// holds one `.m` and one `.h`; the header's language cannot be read from its name, so
		// it is labelled C, and counting that placeholder as a vote made the directory "c" on
		// a 1–1 tie. The page existed either way — what vanished was Objective-C itself.
		{"Objective-C", "objc/Sources"},
		{"Ruby", "ruby/lib/api"},
		// Ruby's load-path target, and the reason it is asserted separately: nothing in
		// `client.rb` says `lib`, so only a root registered from the Gemfile's directory plus
		// the gem convention makes `require "corpus/version"` mean this directory.
		{"Ruby, reached through the load path rather than relatively", "ruby/lib/corpus"},
		{"PHP", "php/src"},
		// The PSR-4 map's nested target. `Corpus\Format` maps here and not onto `src/` only
		// because the longest prefix wins, which is what keeps a repository's internal
		// structure more than one node wide.
		{"PHP, a namespace under the mapped prefix", "php/src/Format"},
		{"C#", "dotnet/Corpus.Api"},
		// Two C# projects rather than one, because the ProjectReference between them is the
		// only thing in a .NET repository that says they are separate: `using Corpus.Domain`
		// looks identical whether Corpus.Domain is a project here or a package from nuget.org.
		{"C#, a second project reached by ProjectReference", "dotnet/Corpus.Domain"},
		{"Shell", "shell/scripts"},
		// The sourced-library directory, and the reason it is asserted separately: it is what
		// `source "$SCRIPT_DIR/lib/log.sh"` reaches, and an anchored source is the form a
		// correct script writes. A rule that read only bare literal paths would leave this
		// directory unreferenced by the script beside it.
		{"Shell, reached through an anchored source", "shell/scripts/lib"},
		{"PowerShell", "powershell/scripts"},
		// The module tree, reached from the script by a Windows-separator Import-Module. Both
		// separators appear in the corpus because both appear in real PowerShell, and a
		// resolver reading only `/` finds nothing here.
		{"PowerShell, a module tree reached across separators", "powershell/src/Corpus"},
		{"Vue", "web/src/components"},
		{"Svelte", "web/src/lib"},
		{"Astro", "web/src/pages"},
		// The layout, and it is asserted separately because it is the one component directory
		// here that imports nothing. A layout is the root of a component tree, so its page
		// exists on the strength of the extractor having run rather than of an edge reaching
		// it — which is the case a resolver-driven module list would lose entirely.
		{"Astro, a layout that imports nothing", "web/src/layouts"},
	} {
		if _, ok := byTitle[want.dir]; !ok {
			t.Errorf("%s: no module page for %s. Module pages written:\n  %s",
				want.language, want.dir, pageNames(pages))
		}
	}

	// The C family shares one extractor across three languages, so the page existing says
	// nothing about which language the directory was read as. That is asserted separately:
	// each dialect must name itself on the page of the directory written in it. Without this
	// the whole family could collapse onto "c" and every row above would still pass.
	// The three newest are named here too, for a related reason: each shares its scope
	// machinery with a language already in the list — Ruby with nothing, PHP and C# with the
	// JVM — so a page for the directory says only that some extractor ran on it.
	// Shell and PowerShell are the sharpest version of that: they are the two languages a
	// reader would most expect to be one extractor, and they are not. They agree only on `#`
	// as a comment, and their scope rules are opposites — a function nested inside another is
	// global in shell and dies with the enclosing scope in PowerShell (ADR 0022). If one
	// extractor ever claimed both, every row above would still pass and one of the two
	// languages would silently stop being named anywhere in the bundle.
	for _, want := range []struct{ dir, lang string }{
		{"c/src", "c"},
		{"c/include/corpus", "c"},
		{"cpp/src", "cpp"},
		{"cpp/include/corpus", "cpp"},
		{"objc/Sources", "objc"},
		{"ruby/lib/api", "ruby"},
		{"php/src", "php"},
		{"dotnet/Corpus.Api", "csharp"},
		{"shell/scripts", "shell"},
		{"shell/scripts/lib", "shell"},
		{"powershell/scripts", "powershell"},
		{"powershell/src/Corpus", "powershell"},
		// The three single-file-component formats, and this is the sharpest case in the table
		// because one extractor reads all three *and delegates to a fourth language's*: the
		// script inside a component is TypeScript, so SFCExtractor blanks the markup and hands
		// what is left to TSExtractor. The language on the facts is reset to the component's
		// own afterwards, and this is what asserts that it was. Without these rows all three
		// directories would have pages, every row above would pass, and the bundle would call
		// them TypeScript — reporting the per-language extractor scores in manifest.json
		// against a language nobody in this tree wrote.
		{"web/src/components", "vue"},
		{"web/src/views", "vue"},
		{"web/src/lib", "svelte"},
		{"web/src/pages", "astro"},
		{"web/src/layouts", "astro"},
	} {
		name, ok := byTitle[want.dir]
		if !ok {
			continue // already reported above
		}
		if got := frontmatterLang(pages[name]); got != want.lang {
			t.Errorf("%s: read as %q, want %q. A dialect that never appears in the bundle is "+
				"a language signpost claims to support and does not name.",
				want.dir, got, want.lang)
		}
	}
}

// frontmatterLang reads the language out of a module page's one-line description, which is
// where the language is stated in prose: "2 objc files; 9 exported symbols."
func frontmatterLang(body string) string {
	for _, line := range strings.Split(body, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "description:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			return ""
		}
		return fields[1]
	}
	return ""
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
		{"RubyGems (Gemfile)", "references/rubygems-rack.md"},
		{"Composer (composer.json)", "references/composer-monolog-monolog.md"},
		{"NuGet (.csproj)", "references/nuget-microsoft-extensions-logging.md"},
		{"GitHub Actions (workflow)", "references/github-actions-actions-checkout.md"},
		{"Compose (compose.yaml)", "services/api.md"},
	} {
		if _, ok := pages[want.page]; !ok {
			t.Errorf("%s: no page at %s. Pages written:\n  %s",
				want.ecosystem, want.page, pageNames(pages))
		}
	}

	// A ProjectReference is a declaration in the same list as a PackageReference and must not
	// become a reference page: a repository does not pull its own project in from a registry.
	// Asserted here rather than among the near-misses above because the declaration is real and
	// correctly read — what is being checked is where it lands, and the edge it becomes instead
	// is `configures` from Corpus.Api onto Corpus.Domain.
	for _, name := range []string{
		"references/nuget-corpus-domain.md",
		"references/nuget-corpus-api.md",
	} {
		if _, bad := pages[name]; bad {
			t.Errorf("%s exists. It is a ProjectReference — a project in this repository — so a "+
				"reference page for it claims the repository depends on its own code from "+
				"outside, and nobody publishes or patches it", name)
		}
	}
}

// TestCorpusPracticesReportsBothKinds checks the practices page states what the corpus
// declares and what it does not.
//
// The corpus is built to have both, and the absences are deliberate fixture decisions rather
// than omissions: no SECURITY.md, and three manifests with no lockfile beside them — Python,
// Composer and NuGet — while four ecosystems have one. A page that only ever reported presences
// would render an absence as silence, which is the failure design §9.1 is written against.
//
// The per-ecosystem split is what the seven dependency lines assert together. One lockfile does
// not pin another supply chain, and a single "a lockfile exists" line would hide exactly that:
// this corpus has four pinned ecosystems and three unpinned ones in the same tree, so a finding
// that collapsed them would have to be wrong about at least three.
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
		"The RubyGems dependencies are pinned by a lockfile",
		"Automated dependency updates are configured",
		"ownership rules assign paths to reviewers",
		"The repository states its licence",
		"An observability library is a declared dependency",
		"rules for agents working in this repository",
		// The absences.
		"**Not declared.**",
		"No security policy was found",
		"The Python dependencies are declared but not pinned",
		"The Composer dependencies are declared but not pinned",
		// NuGet's lockfile is opt-in — it exists only when a project sets
		// RestorePackagesWithLockFile — so its absence is the normal .NET case and is still
		// reported. The reading is honest: without it a floating version range does resolve
		// differently between two restores.
		"The NuGet dependencies are declared but not pinned",
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

// TestCorpusPipelinesCarryDeclaredOrderingOnly asserts both halves of `precedes`.
//
// The positive is release.yml's `publish: needs: [build]` — the corpus's only declared job
// ordering, so the edge count is checkable rather than approximate. The negatives are what a
// plausible wrong reading produces, and there are three distinct ones:
//
//   - ci.yml's `test` and `lint` have no `needs`, so they run concurrently. A reader deriving
//     order from file position draws test → lint, which GitHub does not honour. That is a
//     confidently wrong edge, and it is worse than no edge because a reader would sequence
//     work around it.
//   - `verify: needs: [smoke-test]` names a job that does not exist. Nothing may be invented
//     for it.
//   - `build` precedes `publish` and not the reverse. Both directions are one edge in a
//     count, so the direction is asserted separately.
//
// The count is asserted here, against the exception the corpus doc gives for counts: a
// `precedes` edge exists only where a file states one, so this number moves when the fixture
// moves and not when an extractor improves. That is the property that makes it safe to assert,
// and asserting it is the only way a spurious edge fails rather than passing unnoticed.
func TestCorpusPipelinesCarryDeclaredOrderingOnly(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	// One page per job across the three workflows: ci's test, lint, and platform, nightly's
	// scan, release's build, publish, and verify.
	//
	// `build`'s page is named for its `name:` and not its key, which is the right way round —
	// the name is what GitHub's checks UI shows and what a required-check rule is written
	// against, so it is the string a reader arrives with. `platform` is the exception and the
	// reason both are asserted here: its `name:` interpolates a matrix value, so there is no
	// one string GitHub shows and none of the ones it does show are in the file. That page is
	// titled by the key.
	for _, want := range []string{
		"pipelines/ci-test.md",
		"pipelines/ci-lint.md",
		"pipelines/ci-platform.md",
		"pipelines/nightly-scan.md",
		"pipelines/release-build-the-artifact.md",
		"pipelines/release-publish.md",
		"pipelines/release-verify.md",
	} {
		if _, ok := pages[want]; !ok {
			t.Errorf("no page at %s. Pages written:\n  %s", want, pageNames(pages))
		}
	}
	// The negative half of the same boundary, asserted on the filename rather than only on
	// the title: an expression must not reach a path in a committed artifact. Slugging the raw
	// `platform (${{ matrix.os }})` yields "platform-matrix-os", which is a page named after
	// GitHub Actions syntax for a job nobody can name.
	for name := range pages {
		if strings.Contains(name, "matrix") {
			t.Errorf("a page is named %s. A `name:` holding `${{ matrix.os }}` is a template, "+
				"not a job name, and slugging it puts the expression in a committed "+
				"filename:\n  %s", name, pageNames(pages))
		}
	}

	var precedes []string
	for name, body := range pages {
		if !strings.HasPrefix(name, "pipelines/") {
			continue
		}
		for _, ln := range strings.Split(body, "\n") {
			if strings.Contains(ln, "kind: precedes") {
				precedes = append(precedes, name+": "+strings.TrimSpace(ln))
			}
		}
	}
	sort.Strings(precedes)
	if len(precedes) != 1 {
		t.Errorf("precedes edges = %d, want exactly 1 — the corpus declares one `needs`, on "+
			"release.yml's publish job. Every other job pair is concurrent, and an edge between "+
			"two of them asserts an order GitHub does not honour:\n  %s",
			len(precedes), strings.Join(precedes, "\n  "))
	}

	// Direction. The edge runs from the job that finishes first to the one that waits, so it
	// renders on `build`'s page as "runs before publish" — and the reverse view, what publish
	// is waiting on, is on publish's page as the `needs` attribute. Reversed, the bundle would
	// tell a reader to publish before building.
	//
	// The named page is also what checks the key-versus-name distinction. `publish` states
	// `needs: [build]`, naming the job's key, while the job's title is "build the artifact";
	// resolving the `needs` against the title finds nothing and reports a workflow with no
	// stated ordering at all. That is why `build` carries a `name:` here — with the two
	// strings equal, as they are for every other job in the corpus, confusing them costs
	// nothing and this file is the only place it can be caught.
	if page := pages["pipelines/release-build-the-artifact.md"]; !strings.Contains(page, "kind: precedes") ||
		!strings.Contains(page, "release-publish.md") {
		t.Errorf("release-build-the-artifact.md must carry a precedes edge to "+
			"release-publish.md. `publish` states `needs: [build]`, which names the job's key, "+
			"while the page is titled by its `name:`:\n%s", page)
	}
	if page := pages["pipelines/release-publish.md"]; strings.Contains(page, "kind: precedes") {
		t.Errorf("release-publish.md carries a precedes edge. It is the job that waits, so the "+
			"edge belongs on the one that runs first; what it waits on is its `needs`:\n%s", page)
	}
	// A `needs` naming a job that does not exist. The workflow is committed in that state and
	// GitHub never runs the job; inventing a target would be a page linking to nothing.
	if page := pages["pipelines/release-verify.md"]; strings.Contains(page, "kind: precedes") {
		t.Errorf("release-verify.md carries a precedes edge. Its `needs` names smoke-test, "+
			"which no job in the file declares:\n%s", page)
	}
	// The `needs` is still reported as an attribute even though no edge could be drawn, so the
	// broken reference is visible rather than silently dropped.
	if page := pages["pipelines/release-verify.md"]; !strings.Contains(page, "smoke-test") {
		t.Errorf("release-verify.md does not name the job its `needs` asks for. An unresolvable "+
			"reference is a fact about the workflow and must not vanish:\n%s", page)
	}
}

// TestCorpusGateFindingCountsBothKinds checks the index states which jobs gate a merge.
//
// The corpus has both, deliberately: ci.yml runs on pull_request so its three jobs gate, while
// nightly.yml is schedule-only and release.yml is tag-only, so their four cannot. A finding
// that reported a count without the distinction would be wrong about four of seven jobs here.
func TestCorpusGateFindingCountsBothKinds(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	index, ok := pages[okf.IndexPage]
	if !ok {
		t.Fatalf("no %s was written", okf.IndexPage)
	}
	// Three of seven: ci's test, lint, and platform. Stated as a fraction so a reader sees
	// both halves at once — how many checks a change must pass, and how much automation runs
	// outside them.
	//
	// `platform` counts once here even though the matrix expands it into two checks on the
	// pull request. That is the fact the file states: a count of expanded checks would need
	// the matrix values, and those are not in the tree.
	if !strings.Contains(index, "Merge gates: 3 of 7 CI jobs") {
		t.Errorf("%s does not state the gate count. ci.yml's three jobs gate; nightly's one and "+
			"release's three do not:\n%s", okf.IndexPage, findingsSection(index))
	}
	for _, want := range []string{
		"pipelines/ci-test.md", "pipelines/ci-lint.md", "pipelines/ci-platform.md",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("%s does not link %s among the gates:\n%s",
				okf.IndexPage, want, findingsSection(index))
		}
	}
	// The non-gating jobs must not be listed as gates. Asserted against the finding's own
	// line rather than the page, since those pages are legitimately listed under Pipelines.
	gates := gateFindingLines(index)
	for _, unwanted := range []string{"nightly-scan", "release-build-the-artifact", "release-publish"} {
		if strings.Contains(gates, unwanted) {
			t.Errorf("the merge-gate finding lists %s, which runs on a schedule or a tag and "+
				"cannot block a merge:\n%s", unwanted, gates)
		}
	}
}

// findingsSection returns the index's structural findings, for a failure message that shows the
// relevant part of a long page rather than all of it.
func findingsSection(index string) string {
	i := strings.Index(index, "### Structural findings")
	if i < 0 {
		return index
	}
	rest := index[i:]
	if j := strings.Index(rest, "\n## "); j > 0 {
		return rest[:j]
	}
	return rest
}

// gateFindingLines returns the merge-gate finding and the list of jobs it names, and nothing
// else on the page.
//
// A finding is one unindented `- **` bullet followed by its own indented `  - ` items, so the
// finding ends at the first line that is not one of those items. Terminating on the *next*
// finding instead would be wrong here for a reason that is easy to miss: the merge-gate finding
// is the last one emitted, so there is no following bullet, and the search would run past the
// end of the section and into the index's Pipelines listing — where every pipeline is named,
// gate or not, and any assertion about which jobs the finding omits would pass or fail on the
// wrong text.
func gateFindingLines(index string) string {
	lines := strings.Split(index, "\n")
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
	return strings.Join(lines[start:end], "\n") + "\n"
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
// The condition cannot be expressed by the corpus as committed: every one of its ecosystems
// declares dependencies, because every other stage here needs them to. So this stage makes one
// of them empty — the manipulate-then-rebuild shape the CRLF and stale-page stages use — and
// takes its lockfile away, which is the only arrangement that reaches the branch.
//
// One manifest in the corpus does declare nothing — `dotnet/Corpus.Domain.csproj`, which is what
// a .NET domain project ordinarily looks like — and it is deliberately not this stage's subject.
// The finding is counted per *ecosystem*, not per file, and Corpus.Api declares two NuGet
// packages, so NuGet reports as unpinned rather than as empty. That is the count below holding
// for a reason worth having in the corpus: a per-file reading of this branch would state it for
// NuGet too and the assertion would fail.
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

// TestCorpusHistoryPracticeReportsBothReadings is the corpus half of the history topic, and it
// needs a repository because both findings come from `git log` and `git for-each-ref` rather than
// from a file anything can fixture.
//
// The corpus as committed is the negative case in both directions: one commit, subject "the
// corpus", no tags. So this stage adds the positive one on top and asserts both from the same
// page — conventional subjects and a tag reachable from the described commit.
//
// Both readings from one build is the point, and it is the same shape the dependency stages use.
// A classifier that answered "conventional" to everything satisfies the first assertion and fails
// the rate; one that answered "no convention" satisfies nothing. And the tag assertions pin the
// two defects this pass was shipped with: `--merged --end-of-options <sha>`, which git read as a
// malformed object name and which reported every repository as untagged, and a date-only sort,
// which on same-day tags let git break the tie by refname ascending and name the *oldest* release
// as latest.
func TestCorpusHistoryPracticeReportsBothReadings(t *testing.T) {
	dir := corpusRepo(t)

	// Four conventional subjects here plus the one after the tag below, against the one prose
	// subject corpusRepo made: 5 of 6, over the two-thirds threshold and deliberately not 6 of 6.
	// A page that reports 100% cannot distinguish a rate from a presence check.
	//
	// Each one touches a file rather than being `--allow-empty`. That is load-bearing and was
	// found the hard way: readHead names the newest commit that changed something outside the
	// bundle, so a run of empty commits leaves the *described* commit back at the initial import
	// and every tag added after it is genuinely unreachable. The page was right and the test was
	// wrong.
	notes := filepath.Join(dir, "history-notes.md")
	for i, msg := range []string{
		"feat(greeter): add a second greeting",
		"fix(greeter): correct the salutation",
		"docs: describe the corpus layout (#7)",
		"chore: tidy",
	} {
		if err := os.WriteFile(notes, []byte(strings.Repeat("a line\n", i+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "commit", "--quiet", "-m", msg)
	}
	// Two tags on the same day, which is what a release cut in one session looks like and the
	// arrangement that made the sort tiebreak load-bearing. v0.2.0 is the newer name and must be
	// reported as latest even though v0.1.0 sorts first by refname.
	gitRun(t, dir, "tag", "v0.1.0")
	gitRun(t, dir, "tag", "v0.2.0")
	if err := os.WriteFile(notes, []byte("one more line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--quiet", "-m", "feat: one more after the tag")

	if _, stderr, code := invoke(t, "build", "--quiet", "-repo", "example.com/corpus", dir); code != 0 {
		t.Fatalf("build: exit = %d\n%s", code, stderr)
	}
	page, ok := bundlePages(t, dir)[okf.PracticesPage]
	if !ok {
		t.Fatalf("no %s was written", okf.PracticesPage)
	}

	if !strings.Contains(page, "### How changes are recorded") {
		t.Fatalf("%s has no history section:\n%s", okf.PracticesPage, page)
	}
	for _, want := range []string{
		// The convention, with its rate stated rather than asserted.
		"Commit subjects follow Conventional Commits: 5 of 6",
		// The release facts. The tag name as inline code, because a tag name may hold a
		// semicolon or a `$(...)` that git's ref rules do not reject.
		"`v0.2.0`",
		"2 tags reachable from this commit",
		"1 commit back",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("%s does not state %q:\n%s", okf.PracticesPage, want, page)
		}
	}
	// The wrong tag, from the tie that git resolves by refname ascending.
	if strings.Contains(page, "`v0.1.0`") {
		t.Errorf("%s names v0.1.0 as the latest release. Two tags share a creation date and the "+
			"older name won the tie:\n%s", okf.PracticesPage, page)
	}
	// Neither absence, from a build where both facts are present. Without these the assertions
	// above pass on a page that states both readings at once.
	for _, unwanted := range []string{
		"follow no machine-readable convention",
		"No tag is reachable from this commit",
		"is not known",
	} {
		if strings.Contains(page, unwanted) {
			t.Errorf("%s states %q on a repository that has the fact:\n%s",
				okf.PracticesPage, unwanted, page)
		}
	}
}

// The negative half, from the corpus exactly as committed: one prose subject and no tags. A page
// that reported no history findings at all would pass every assertion in the stage above by
// reporting nothing, and this is what catches that — an untagged repository with unconventional
// messages is a fact worth stating, not silence.
func TestCorpusHistoryPracticeReportsTheAbsences(t *testing.T) {
	dir := buildCorpus(t)
	page, ok := bundlePages(t, dir)[okf.PracticesPage]
	if !ok {
		t.Fatalf("no %s was written", okf.PracticesPage)
	}
	for _, want := range []string{
		"Commit subjects follow no machine-readable convention",
		// The rate, on the absence too. "follows no convention" alone reads as though signpost
		// found nothing to say.
		"0 of 1",
		"No tag is reachable from this commit",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("%s does not state %q. The corpus has one prose subject and no tags:\n%s",
				okf.PracticesPage, want, page)
		}
	}
	if strings.Contains(page, "Commit subjects follow Conventional Commits") {
		t.Errorf("%s claims the corpus follows the convention:\n%s", okf.PracticesPage, page)
	}
	// Not the shallow-clone wording. The corpus repository is a full clone, so "not known" would
	// be signpost reporting a gap in a measurement it made successfully.
	if strings.Contains(page, "is not known") {
		t.Errorf("%s reports the tag question as unknown on a full clone:\n%s",
			okf.PracticesPage, page)
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

// TestCorpusBranchGateSeparatesWhatTheAuthorCanFix is the regression for a gate that was right
// thirteen consecutive times and useless anyway.
//
// Every one of those pull requests failed for the same reason: it added or moved structure, so a
// rebuild would rewrite pages, and the failure said `run signpost build and commit the result` —
// which §8.0 forbids on a branch, because the bundle is written on the default branch only so two
// branches cannot collide in it. A check that is red whenever anybody touches a directory, naming
// a remedy nobody is allowed to apply, teaches everyone to merge past it; and then the run where
// the bundle is genuinely broken is merged past too.
//
// The pair is the property, not either half. A gate that passes everything under -as-of-bundle
// satisfies the first stage below and is no gate at all; a gate that fails everything satisfies
// the second and is the shipped bug. So both run against the *same* branch state — a corpus that
// has gained a module and had a page it links to deleted — where the two differences are
// simultaneous and only one of them has a remedy the author can reach.
func TestCorpusBranchGateSeparatesWhatTheAuthorCanFix(t *testing.T) {
	dir := buildCorpus(t)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--quiet", "-m", "the bundle")

	// What a branch does: a module the bundle has no page for. A Go package rather than an edit
	// to an existing file, so the difference is structural and reaches the page list, index.md,
	// and log.md as well — the four kinds a rebuild owns, all at once.
	pkg := filepath.Join(dir, "go", "lateshipping")
	if err := os.MkdirAll(pkg, 0o750); err != nil {
		t.Fatal(err)
	}
	src := []byte("package lateshipping\n\nfunc Ship() {}\n")
	if err := os.WriteFile(filepath.Join(pkg, "ship.go"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--quiet", "-m", "feat: a module the bundle predates")

	stdout, stderr, code := invoke(t, "verify", "--quiet", "-as-of-bundle",
		"-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("the branch gate failed on a difference only the merge can resolve: exit = %d\n%s%s",
			code, stdout, stderr)
	}
	// Reported in full, and named. "Nothing to do" is only trustworthy if the reader can see what
	// was set aside and disagree with it — a gate that silently swallowed a page it decided was
	// somebody else's problem would be the false pass §4.6 forbids, arriving through the exit code
	// instead of the output.
	for _, want := range []string{"lateshipping", "the merge will rebuild", "rebuilt after this merges"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the pass does not say %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "matches this tree") {
		t.Errorf("claimed a match on a bundle that is a page short:\n%s", stdout)
	}

	// The same tree, verified strictly. This is what runs on the default branch, where signpost
	// *writes* the stamp and there is no later rebuild to defer to, so every one of those
	// differences is a defect. Without this the stage above is indistinguishable from a gate that
	// stopped checking.
	stdout, _, code = invoke(t, "verify", "--quiet", "-repo", "example.com/corpus", dir)
	if code != 1 {
		t.Fatalf("a strict verify passed on a bundle a build would rewrite: exit = %d\n%s",
			code, stdout)
	}
	if !strings.Contains(stdout, "lateshipping") {
		t.Errorf("the strict failure does not name the module that appeared:\n%s", stdout)
	}

	// And now the negative boundary, on that same branch: a page index.md links to, deleted. No
	// rebuild after the merge repairs this — the link dangles in every checkout of this bundle —
	// so it has to reach the exit code even though the tree also carries the deferred differences
	// asserted above. A classifier keyed off the mode, or off the message text, passes everything
	// above and fails here.
	// A module page rather than index.md itself, and that distinction is the finding: index.md is
	// where the links live, so deleting *it* takes the dangling references with it and leaves a
	// bundle the rebuild does restore. What no rebuild restores is a link with no target.
	var victim string
	for _, rel := range sortedPageNames(bundlePages(t, dir)) {
		if strings.HasPrefix(rel, "modules/") {
			victim = rel
			break
		}
	}
	if victim == "" {
		t.Fatal("the corpus bundle has no module page, so there is no link to dangle")
	}
	if err := os.Remove(filepath.Join(dir, okf.BundleDir,
		filepath.FromSlash(victim))); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--quiet", "-m", "break: delete a page the bundle links to")

	stdout, stderr, code = invoke(t, "verify", "--quiet", "-as-of-bundle",
		"-repo", "example.com/corpus", dir)
	if code != 1 {
		t.Fatalf("the branch gate passed on a bundle no rebuild repairs: exit = %d\n%s%s",
			code, stdout, stderr)
	}
	if !strings.Contains(stdout, "is not in the bundle") {
		t.Errorf("the failure does not say the link resolves to nothing:\n%s", stdout)
	}
	// Both severities in one run, and both visible. A failure that stopped printing the deferred
	// differences would leave the author guessing which half is theirs.
	if !strings.Contains(stdout, "the merge will rebuild") {
		t.Errorf("the deferred differences vanished once something else failed:\n%s", stdout)
	}
	if !strings.Contains(stdout, "problem(s)") {
		t.Errorf("the failure does not separate what must be acted on:\n%s", stdout)
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

// TestCorpusTerraformStatesWhatRunsAndNeverAValue is the Terraform stage.
//
// A configuration is the file in a repository most likely to hold a live credential and the
// one whose structure a reader most needs, which puts both boundaries in the same place. It
// is also mostly wiring: a real configuration declares IAM attachments and route table
// associations by the hundred, so a reader that admitted every resource would report forty
// pages where one thing runs, and the pages that mattered would be unfindable among them.
//
// The negatives here are the values, and they are asserted over the whole bundle rather than
// per page: five strings sit in the fixtures beside a name the reader does record — a backend
// bucket, a `secret_string`, a sensitive variable's default, and two .tfvars assignments — and
// the bundle is committed and published, so any one of them reaching it is a credential
// exfiltration path wearing a documentation tool's clothes. A per-page assertion would miss a
// value that leaked through the index or a page nobody thought to check.
//
// Two of the five are load-bearing here and three are defence in depth, which is worth stating
// rather than leaving to be discovered. The bucket and the `secret_string` sit on references
// that reach a page, so this stage is what holds them — a reader that carried either was caught
// here and nowhere else. The variable default and the two .tfvars values sit on references the
// reader deliberately attributes to nothing, and an unattributed reference reaches no page, so
// today they cannot arrive by that road whatever the reader does with them; the assertion that
// holds those is the renderFacts sweep in internal/manifest, which asks the same question one
// layer up, before attribution can hide the answer. They stay here because attribution is a
// decision and not a law: if a later change gives those references somewhere to land, this is
// the assertion that notices the value came with them.
func TestCorpusTerraformStatesWhatRunsAndNeverAValue(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	// Not one value from any Terraform fixture, anywhere. Every string below sits in the
	// fixture immediately beside a name that *does* reach the bundle, which is the only way to
	// tell "the reader took the name and stopped" from "the reader did not run".
	for _, secret := range []string{
		"corpus-state-do-not-publish",           // the backend bucket, beside `backend "s3"`
		"hunter2-do-not-publish",                // a secret_string, beside its resource name
		"s3cr3t-material-that-must-not-be-read", // a sensitive variable's default
		"tfvars-value-must-never-be-read",       // a .tfvars assignment to db_password
		"tfvars-token-must-never-be-read",       // and to api_token
	} {
		for rel, body := range pages {
			if strings.Contains(body, secret) {
				t.Errorf("%s contains %q, a value from a Terraform fixture. The bundle is committed "+
					"and published, so a value that crosses into it is a credential leaving the "+
					"repository through the documentation", rel, secret)
			}
		}
	}

	// The names, which are the counterpart: suppressing the values is only correct if the
	// facts beside them arrived. Without this half, a reader that read no .tf file at all
	// would satisfy every assertion above.
	for _, want := range []struct{ page, fact string }{
		// A workload, and its image — the same claim a compose file's `image:` makes.
		{"services/aws-ecs-service-worker.md", "docker.io/library/golang:1.26-alpine"},
		// A secret store is a page for one reason: the resource *is* the named credential, so
		// "where the credentials in this configuration live" is a thing a reader looks up.
		{"services/aws-secretsmanager-secret-db.md", "corpus/db-credentials"},
		// Read from the local module, which is where the parser's brace cases live. A miscount
		// there is silent — it reparents the rest of the file — so this page vanishing is the
		// observable.
		{"services/aws-sqs-queue-events.md", "aws_sqs_queue"},
		{"services/aws-lambda-function-consumer.md", "aws_lambda_function"},
		// A registry module is genuinely external and keeps its page.
		{"references/terraform-vpc.md", "terraform-aws-modules/vpc/aws"},
		{"references/terraform-aws.md", "hashicorp/aws"},
	} {
		body, ok := pages[want.page]
		if !ok {
			t.Errorf("no page at %s, so a fact the configuration states reached no reader. Pages:\n  %s",
				want.page, pageNames(pages))
			continue
		}
		if !strings.Contains(body, want.fact) {
			t.Errorf("%s does not state %q:\n%s", want.page, want.fact, body)
		}
	}

	// And the negative boundary on structure. Each of these is in the fixture on purpose, and
	// each is a page a reader cannot act on: wiring that runs nothing, capacity that runs
	// nothing by itself, a `data` block declaring something another configuration owns, and a
	// directory of this repository dressed as a third-party dependency.
	for _, unwanted := range []struct{ page, why string }{
		{"services/aws-iam-role-policy-attachment-worker.md", "wiring: it attaches a policy and runs nothing"},
		{"services/aws-security-group-rule-worker-egress.md", "wiring: a firewall rule is not a unit"},
		{"services/aws-s3-bucket-policy-assets.md", "wiring: a bucket policy is not a place state lives"},
		{"services/aws-sns-topic-subscription-alerts.md", "wiring: a subscription is not a workload"},
		{"services/aws-ecs-cluster-corpus.md", "capacity, and on the exceptions list: `_cluster` is a workload suffix and a cluster runs nothing by itself"},
		{"services/aws-lambda-function-existing.md", "a `data` block: compute-shaped, and it declares nothing this configuration owns"},
		{"services/aws-sqs-queue-policy-events.md", "wiring: `_policy` is not a workload suffix"},
		{"references/terraform-queue.md", "a directory in this repository, presented as something pulled in from outside"},
	} {
		if body, ok := pages[unwanted.page]; ok {
			t.Errorf("%s exists and should not — %s. A page a reader cannot act on buries the ones "+
				"they can:\n%s", unwanted.page, unwanted.why, body)
		}
	}

	// The other half of that row — a composition edge drawn where the reference page was
	// suppressed — is asserted in internal/assemble, not here, and the reason is a property of
	// the design rather than a gap in this fixture. Terraform is read as a manifest, so it
	// contributes no module nodes of its own, and the edge needs one at each end: the nearest
	// module above the declaring file and one for the directory named. `infra/` and
	// `infra/modules/queue/` hold nothing but `.tf`, so neither end exists and correctly no
	// edge is drawn. Faking it would mean planting source in two directories to stand up an
	// edge, which tests the fixture. TestLocalDeclarationIsAnEdgeAndNotAReferencePage pairs
	// both halves against a tree that does have the source.

	// The unattributed-reference boundary, on the bundle. `db_password` and `api_token` are
	// module-level inputs to infra/main.tf and `db_password_arn` is a sensitive output: which
	// resource reads them is stated in an expression this reader does not evaluate. Handed to
	// every service in the file — which is what an empty Service means to a compose file — the
	// ECS task and the S3 state backend each claimed to read three credentials neither names.
	for _, page := range []string{
		"services/aws-ecs-service-worker.md",
		"services/terraform-state.md",
		"services/aws-sqs-queue-events.md",
	} {
		body, ok := pages[page]
		if !ok {
			continue // absence is the other tests' business
		}
		for _, unwanted := range []string{"db_password", "api_token", "db_password_arn"} {
			if strings.Contains(body, unwanted) {
				t.Errorf("%s names %q, a module-level input to the configuration. Nothing in the "+
					"file says this unit reads it, and a reader is told a credential is reachable "+
					"from somewhere it is not:\n%s", page, unwanted, body)
			}
		}
		if strings.Contains(body, "reads-secrets") {
			t.Errorf("%s carries reads-secrets and names no credential of its own:\n%s", page, body)
		}
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
	//
	// Scoped to `references/`, which is where an external dependency's page goes, and the claim
	// this test makes is about that directory: a first-party package must not appear there. The
	// scan once covered every page in the bundle, which was the same assertion only as long as
	// no *module* was named `corpus-api` — and the .NET tree named two, `dotnet/Corpus.Api` and
	// `dotnet/Corpus.Api.Tests`, whose module pages slug to exactly that. A module page for
	// first-party source is what correct output looks like. Matching on the name inside
	// `references/` rather than on a predicted `npm-` filename is deliberate and stronger: a
	// fabricated external is fabricated whatever ecosystem prefix it acquires, and the C#
	// `ProjectReference` reaches the same defect through a `nuget-` name.
	for rel := range pages {
		if !strings.HasPrefix(rel, "references/") {
			continue
		}
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
//     of the Node builtin `path`, which is the boundary for issue #14's subpath rule;
//   - java `com.example.apiv2` — shares every character of the declared package
//     `com.example.api`, which a package prefix compared as a string rather than as a
//     dot-delimited name folds into it;
//   - java `javax.servlet.http` and kotlin `kotlinx.coroutines` — the two JVM near-misses of
//     the *runtime* rather than of a declared dependency. `javax` is split between the JDK
//     and Maven artifacts by a 1999 decision and nothing else, so `javax.crypto` is the
//     platform and `javax.servlet` is a dependency somebody upgrades; `kotlinx` opens with
//     the six characters of `kotlin` and is separately versioned. Both are here because the
//     JVM has no manifest reader yet, so nothing but this list distinguishes a wrongly
//     classified runtime name from a correctly classified one.
//
// Ruby, PHP and C# each carry two, and the pairs are chosen the same way the JVM's are —
// one against a declared dependency, one against the runtime rule — because those are the
// two directions a name can be swallowed in:
//
//   - ruby `net/ldap` — the `net-ldap` gem, one segment away from the stdlib `net/http`.
//     Ruby's runtime table is matched on the whole require path with no first-segment rule,
//     and this pair is why: cutting on the slash and asking about `net` would report a gem
//     somebody has to install and patch as the standard library;
//   - ruby `rack_extras` — opens with the four characters of the declared gem `rack`, in the
//     underscore spelling depKeys' PEP 503 normalization legitimately folds for Python;
//   - php `CorpusKernel\Boot` — shares the six characters of the PSR-4 prefix `Corpus\` that
//     composer.json maps onto `src/`. A namespace nests on the backslash, so a prefix test
//     done on the string routes this into the production tree and draws an edge to a file
//     that is not there. PHP's isStdlib is `false` always, so it lands here every time;
//   - php `MonologExtras\Handler` — the same boundary on the dependency side: it opens with
//     the name of the declared `monolog/monolog`, and the candidate list is what must not
//     match it by prefix;
//   - csharp `Corpus.DomainModel` — shares every character of the declared namespace
//     `Corpus.Domain`, which a dotted prefix compared as a string folds into it, exactly as
//     `com.example.apiv2` does on the JVM;
//   - csharp `Microsoft.Extensions.Caching.Memory` — the .NET runtime near-miss, and the
//     reason `Microsoft.*` is split rather than accepted whole. `Microsoft.Win32` beside it
//     in the same file is the platform; this is a NuGet package nobody declared, and a rule
//     taking the first segment for the runtime hides it.
//
// The C family's four near-misses are all boundaries of the include search path, and each
// probes a different rule, because C resolution has no manifest and no module system — only
// a search order and the two delimiters:
//
//   - c `<corpus/buffers.h>` — one letter from the project's own public header
//     `<corpus/buffer.h>`, which a search matching a directory rather than a file, or
//     matching by prefix, resolves into the header next to it;
//   - c `<stdlib_extras.h>` — opens with the six characters of the runtime header `stdlib.h`,
//     which is the boundary of the cRuntimeHeaders list; a package reclassified as the
//     platform is a dependency nobody is told to patch;
//   - cpp `<gtest_extras/matchers.h>` — a test framework's header, angled because it is on
//     the include path and not because it is the platform. Its extension is the only thing
//     separating it from `<memory>` beside it, which is the whole of the C++ stdlib rule;
//   - objc `<CorpusKit/CorpusKit.h>` — spelled exactly like an Apple framework and shipped by
//     no SDK. A rule taking any capitalised first segment for a framework calls it the
//     platform, and a dependency classified as the platform disappears from the report.
//
// C has one more gap of the same kind as `org.junit.jupiter.api`: the quoted
// `c "unity.h"`, a vendored test framework the corpus does not carry. Quoted and therefore
// searched in the repository first, found nowhere, and reported — rather than guessed at as
// an external nobody declared, which is what C would need a build-file reader to know.
//
// Their positive counterparts are asserted in TestCIncludesResolveByPathAndDelimiter and
// TestCSystemHeaderRecognition: the quoted form against the including file's own directory,
// the angled form through the nearest ancestor's `include/`, and the stdlib in both its
// extensionless C++ shape and its listed C one.
//
// PowerShell carries the same pair as the others and one entry of the JVM's kind, and it is the
// language where the runtime side is hardest to see:
//
//   - powershell `PesterExtras` — opens with every character of `Pester`, which the module the
//     corpus requires is named. A candidate list matched by prefix swallows it and reports a
//     module this code does not load;
//   - powershell `Microsoft.PowerShell.Crescendo` — the runtime near-miss, and the reason the
//     engine modules are a closed list rather than a `Microsoft.PowerShell.*` prefix. It opens
//     with the whole name of the engine module `Microsoft.PowerShell.Utility` and is a
//     separately versioned gallery module somebody installs and patches;
//   - powershell `Pester` — the limitation, the same shape as `org.junit.jupiter.api`. It *is*
//     declared, by `#Requires -Modules Pester`, and a `#Requires` is a requirement rather than a
//     pin: it names no version and no source. The file that would pin it is a `.psd1` module
//     manifest, which signpost does not read (classify.go), so there is no declared list for
//     this name to match. Reported as a gap rather than invented as a PowerShell Gallery entry
//     the repository never wrote — and it is why PowerShell, like the JVM, cannot supply the
//     other half of the standard pattern.
//
// Shell contributes nothing to this list, and its absence is a fact about the language rather
// than a missing fixture. There is no shell package registry, so a `source` that reaches no file
// cannot be a dependency somebody forgot to declare — resolveShell returns internal for it, and
// the corpus's deliberate shell near-miss is asserted in
// TestCorpusFirstPartyImportsThatReachNoPageAreCounted instead. A shell specifier appearing
// *here* would mean the resolver had started inventing packages for a language that has none.
//
// The JVM has one more gap that is a limitation rather than a near-miss: `org.junit.jupiter.api`
// is a real declared dependency, and signpost reads no pom.xml or build.gradle, so there is no
// declared list for it to match. It lands here rather than being invented as a Maven coordinate
// the repository never wrote, which is the honest answer and is also why the JVM cannot supply
// the other half of the standard pattern — an import that resolves to a declared external.
//
// Each must land here and nowhere else. Two wrong homes are possible and both are worse than
// the gap: an edge into this repository, which invents structure; or an external node, which
// invents a supply-chain entry nobody declared. Which failure a given over-match produces
// depends on the repository, so neither is asserted alone — the set is.
//
// The stdlib imports are the other half. `node:fs`, python `os`, rust `std::fmt`, java
// `java.util.List` and `javax.crypto`, kotlin `kotlin.math`, and the Node builtins addressed by
// subpath — `fs/promises`, `node:test/reporters` — are the runtime:
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
		`c "unity.h"`,
		"c <corpus/buffers.h>",
		"c <stdlib_extras.h>",
		"cpp <gtest_extras/matchers.h>",
		"csharp Corpus.DomainModel",
		"csharp Microsoft.Extensions.Caching.Memory",
		"go example.com/corpus/greeterx/format",
		"java com.example.apiv2",
		"java javax.servlet.http",
		"java org.junit.jupiter.api",
		"kotlin kotlinx.coroutines",
		"objc <CorpusKit/CorpusKit.h>",
		`php CorpusKernel\Boot`,
		`php MonologExtras\Handler`,
		"powershell Microsoft.PowerShell.Crescendo",
		"powershell Pester",
		"powershell PesterExtras",
		"python httpx_extras",
		"python winreg_helpers",
		"ruby net/ldap",
		"ruby rack_extras",
		"rust corpus_greeter::Greeting",
		"rust serde_yaml::Value",
		"typescript @corpus/apples/juice",
		"typescript pathe/utils",
		"vue vue-router",
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
	// Six of these are fragments rather than whole names for a reason the entry beside them
	// makes: `monologextras` cannot be shortened to `monolog`, and `rack_extras` cannot be
	// shortened to `rack`, because the declared dependency each one shadows is a page that must
	// exist. `caching` is what distinguishes the undeclared `Microsoft.Extensions.Caching.Memory`
	// from the declared `Microsoft.Extensions.Logging` beside it in the same file. `pesterextras`
	// and `crescendo` are the same case for PowerShell: `pester` cannot be shortened, because
	// `Pester` is the name a `#Requires` in the corpus states, and `crescendo` is what separates
	// the gallery module from the engine module whose whole name it opens with.
	// `router` is the fragment for the newest of them, and it cannot be shortened to `vue`:
	// `vue-router` opens with every character of the declared `vue`, which is a page that must
	// exist, so a check for `vue` would be satisfied by the framework's own reference page.
	for _, frag := range []string{"apples", "greeterx", "httpx-extras", "httpx_extras", "serde-yaml", "serde_yaml", "pathe", "winreg-helpers", "winreg_helpers", "apiv2", "junit", "servlet", "kotlinx", "buffers", "stdlib-extras", "stdlib_extras", "gtest-extras", "gtest_extras", "corpuskit", "unity", "ldap", "rack-extras", "rack_extras", "corpuskernel", "monologextras", "domainmodel", "caching", "pesterextras", "crescendo", "router"} {
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
	// The JVM runtime, matched exactly for the same reason: `javax.servlet.http` and
	// `kotlinx.coroutines` are near-misses asserted above, and a substring check for `javax` or
	// `kotlin` would be satisfied by the pages that must not exist. These four are the platform
	// — the JDK and the Kotlin standard library, shipped and patched with the toolchain — and
	// they sit one segment away from a name that is not, which is the whole reason the JVM
	// carries two runtime near-misses rather than one.
	for _, name := range []string{"java.util", "javax.crypto", "kotlin.math", "com.sun"} {
		for _, n := range g.Nodes {
			if n.Kind == "External Dependency" && strings.EqualFold(n.Title, name) {
				t.Errorf("%q is an external dependency page. It is the JVM runtime, versioned "+
					"with the toolchain rather than declared in a build file, so a page for it "+
					"is a supply-chain entry for the JDK", n.Title)
			}
		}
	}
	// The three newest languages' runtimes, matched exactly for the same reason and each one
	// segment from a near-miss above. `net/http` and `json` are what make Ruby's whole-path rule
	// necessary; `System.Text.Json` and `Microsoft.Win32` are what make .NET's `Microsoft` split
	// necessary. `Throwable` is PHP's whole runtime story: the language has no importable
	// standard library, so a single-segment `use` naming a built-in class is the one case that
	// must record no import at all — a page for it is the global namespace sold as a package.
	for _, name := range []string{"net/http", "json", "System.Text.Json", "Microsoft.Win32", "Throwable"} {
		for _, n := range g.Nodes {
			if n.Kind == "External Dependency" && strings.EqualFold(n.Title, name) {
				t.Errorf("%q is an external dependency page. It is the language runtime — the "+
					"interpreter or the SDK ships it and nobody publishes or patches it — so a "+
					"page for it is a supply-chain entry for the toolchain", n.Title)
			}
		}
	}
	// PowerShell's runtime, which is two runtimes rather than one: the engine modules whose
	// cmdlets are the language's vocabulary, and the .NET namespaces a `using namespace` reaches,
	// because PowerShell runs on .NET. `Microsoft.PowerShell.Utility` sits one segment from the
	// gallery module asserted above and `System.Text` is what the corpus's own
	// `using namespace System.Text` names, so both must reach no page while the near-miss beside
	// each is reported.
	for _, name := range []string{"Microsoft.PowerShell.Utility", "System.Text"} {
		for _, n := range g.Nodes {
			if n.Kind == "External Dependency" && strings.EqualFold(n.Title, name) {
				t.Errorf("%q is an external dependency page. PowerShell ships it — an engine "+
					"module or a .NET namespace, versioned with the shell and the runtime "+
					"beneath it — so a page for it is a supply-chain entry for the interpreter",
					n.Title)
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

// TestCorpusResolvesJVMPackagesToTheRightDirectory is the regression for the three defects the
// JVM fixtures exposed, and all three come from one fact: the JVM is the only language here
// whose resolution map is built from extracted facts rather than from a manifest.
//
// signpost reads no pom.xml or build.gradle, so a JVM import resolves against the `package`
// declarations found in the source. That works, and it has a consequence the other languages
// do not have — the standard layout declares each package *twice*, once per source set, so
// `com.example.api` names both `src/main/java/com/example/api` and
// `src/integrationTest/java/com/example/api` and only one of them is what another module means
// by it. Three things went wrong there, each asserted below and each with a negative:
//
//   - the wrong source set. Directory order was the tiebreaker, and it cannot be: `src/test`
//     happens to sort after `src/main`, but the source set holding tests is not always called
//     that. Gradle's convention for the extra one is `integrationTest` and Android's is
//     `androidTest`, and both sort *ahead* of `main` — so a repository with either resolved
//     every import of a package to the copy under test. An edge into the tests instead of into
//     the code, drawn with no indication a choice was made. The fixture uses `integrationTest`
//     for exactly this reason: with a source set named `test`, the assertion would pass on the
//     broken ordering too.
//   - the parent package instead of the one asked for. `com.example.store.internal` is a
//     subpackage of `com.example.store`, and a matcher taking the first name that prefixes the
//     import lands on the parent. The Kotlin file imports the subpackage and deliberately does
//     *not* import `store` — with both imported, the wrong answer and the right one draw the
//     same pair of edges and nothing distinguishes them.
//   - `tested_by` pointing at a collaborator. A JVM test declares the package it tests and
//     imports nothing from it, because same-package access needs no import. So reading the
//     imports of `ServiceIT` finds `com.example.store` and misses `com.example.api` — the
//     graph said the store was tested by a test of the API, which is the confidently-wrong
//     edge the rule exists to avoid.
func TestCorpusResolvesJVMPackagesToTheRightDirectory(t *testing.T) {
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
	const (
		mainAPI = "jvm/src/main/java/com/example/api"
		testAPI = "jvm/src/integrationTest/java/com/example/api"
		store   = "jvm/src/main/java/com/example/store"
		nested  = "jvm/src/main/java/com/example/store/internal"
		app     = "jvm/src/main/kotlin/com/example/app"
	)
	edge := func(kind, from, to string) bool {
		f, tt := byPath[from], byPath[to]
		if f == "" || tt == "" {
			t.Fatalf("no module node for %q or %q, so this test asserts nothing", from, to)
		}
		for _, e := range g.Edges {
			if e.From == f && e.To == tt && e.Kind == kind {
				return true
			}
		}
		return false
	}

	// The source set. Kotlin's `import com.example.api.Service` must reach the production copy.
	if !edge("imports", app, mainAPI) {
		t.Errorf("no imports edge %s -> %s. `com.example.api` is declared in two source sets, "+
			"so the import names two candidates; production is the one another module means",
			app, mainAPI)
	}
	if edge("imports", app, testAPI) {
		t.Errorf("imports edge %s -> %s. The import resolved to the copy of the package under "+
			"the test source set, which is an edge into the tests instead of into the code. "+
			"Directory order put %q ahead of `main` — which is why the tiebreaker cannot be the "+
			"directory", app, testAPI, "integrationTest")
	}

	// The subpackage. `com.example.store.internal` must reach its own directory, not its parent.
	if !edge("imports", app, nested) {
		t.Errorf("no imports edge %s -> %s. `com.example.store.internal` is declared here and "+
			"`com.example.store` is declared one directory up, so a matcher that stops at the "+
			"first name prefixing the import lands on the parent", app, nested)
	}
	if edge("imports", app, store) {
		t.Errorf("imports edge %s -> %s. Nothing in the Kotlin file imports `com.example.store` "+
			"— it imports the subpackage — so the edge points at the package *containing* the "+
			"one that was asked for", app, store)
	}

	// The subject of the test, from its package declaration rather than from its imports.
	if !edge("tested_by", mainAPI, testAPI) {
		t.Errorf("no tested_by edge %s -> %s. ServiceIT declares `com.example.api` and sits in "+
			"a different directory, so the declaration is the only statement of what it tests: "+
			"same-package access needs no import, so its subject is the one thing its import "+
			"list does not name", mainAPI, testAPI)
	}
	if edge("tested_by", store, testAPI) {
		t.Errorf("tested_by edge %s -> %s. `com.example.store` is what ServiceIT imports and "+
			"`com.example.api` is what it declares — reading the imports reports every "+
			"collaborator as the thing under test", store, testAPI)
	}
}

// TestCorpusReadsBuildGraphsAndDrawsTheirInternalEdges covers the two build systems that state
// structure no other file in a repository states, and every assertion here is paired with the
// negative that makes it mean something.
//
// A build file is the only place a C project says which library its executable links, and the
// only place a Bazel package says which packages it is built against. Reading them is
// therefore not an incremental gain in dependency coverage — it is the difference between a C
// repository having structure in the bundle and having none. But both directions of the reading
// are wrong in ways that read as correct on the page, which is why the negatives are here:
//
//   - CMake links by bare name, and a name is either this repository's own library or a
//     package from outside. Reported the wrong way, `corpus_buffer_core` becomes a reference
//     page claiming the project depends on code it builds itself, and `cmocka` disappears from
//     the supply chain. Both are linked in the same command of `c/tests/CMakeLists.txt`, so no
//     rule can get one right by accident.
//   - Bazel states which it is in the label, so the risk moves to *where* `//` points: it is
//     the workspace root, and the corpus workspace is `go/`. Read as repository-relative,
//     `//cmd/hello` names nothing and the edge silently vanishes — which is how this defect was
//     found, by reading the emitted page rather than by a failing test.
//   - Both readers stop at what they can see. A target built inside a `for` loop is not a
//     top-level call, so `corpus_generated_a` and `corpus_generated_b` are deliberately unread,
//     and an `http_archive` with no `sha256` must not acquire a version — an invented pin is
//     what somebody auditing that file would act on.
//
// The pinned-and-unpinned archive pair is the sharpest of these, because the failure is a
// *plausible* value rather than a missing one. `rules_python` carries its sha256 as the version
// and `corpus_unpinned_archive` carries no version attribute at all, and a reader that filled
// the second one in from the URL would produce a page that looks exactly as trustworthy.
func TestCorpusReadsBuildGraphsAndDrawsTheirInternalEdges(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	// What the two build systems declare that nothing else in the corpus does.
	for _, want := range []struct{ what, page string }{
		{"a CMake find_package", "references/cmake-openssl.md"},
		{"an optional find_package, which is still a dependency", "references/cmake-zlib.md"},
		{"a link to a library declared in no build file here", "references/cmake-cmocka.md"},
		{"a bzlmod bazel_dep", "references/bazel-rules-go.md"},
		{"a dev_dependency, which is a declared dependency too", "references/bazel-gazelle.md"},
		{"an http_archive", "references/bazel-rules-python.md"},
		{"a label naming another repository", "references/bazel-com-github-google-uuid.md"},
	} {
		if _, ok := pages[want.page]; !ok {
			t.Errorf("%s: no page at %s. Pages written:\n  %s",
				want.what, want.page, pageNames(pages))
		}
	}

	// The negatives, checked across every page rather than by expected filename: a name that
	// must appear nowhere can appear in a summary, an edge, or an attribute, and asserting only
	// that one page is missing would leave the other three routes open.
	for _, bad := range []struct{ name, why string }{
		{"corpus_buffer_core", "a library this repository builds, linked by name from " +
			"c/tests/CMakeLists.txt. It is declared in c/src/CMakeLists.txt, which that file " +
			"cannot see, so a reader settling this one file at a time reports the project's " +
			"own library as a third-party dependency"},
		{"corpus_generated_a", "a target built inside a for loop in go/cmd/hello/BUILD.bazel. " +
			"Loop bodies are not top-level calls and are deliberately unread, so naming it " +
			"claims a target signpost did not actually read"},
		{"corpus_generated_b", "the second target from the same loop"},
	} {
		for name, body := range pages {
			if strings.Contains(body, bad.name) {
				t.Errorf("%s names %q: %s", name, bad.name, bad.why)
			}
		}
	}

	// The pin and the absence of one, from the same file.
	pinned, ok := pages["references/bazel-rules-python.md"]
	if !ok {
		t.Fatalf("no page for the pinned http_archive, so this asserts nothing")
	}
	const sha = "9c6e26911a79fbf510a8f06d8eedb40f412023cf7fa6d1461def27116bff022c"
	if !strings.Contains(pinned, sha) {
		t.Errorf("references/bazel-rules-python.md does not carry the sha256 the archive is "+
			"pinned to. That checksum is the pin — without it the page says a dependency "+
			"exists and nothing about whether two builds fetch the same bytes:\n%s", pinned)
	}
	unpinned, ok := pages["references/bazel-corpus-unpinned-archive.md"]
	if !ok {
		t.Fatalf("no page for the unpinned http_archive, so this asserts nothing")
	}
	if strings.Contains(unpinned, "name: version") {
		t.Errorf("references/bazel-corpus-unpinned-archive.md carries a version attribute. The "+
			"archive declares no sha256 and no version, so any value here was invented — and a "+
			"version on this page reads as a pin to whoever audits the supply chain:\n%s",
			unpinned)
	}

	// The internal edges, which are the whole reason to read a build file. Asserted from the
	// graph rather than the pages, because an edge's rendering is a relative link and the
	// question here is what it points at.
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
	for _, want := range []struct{ from, to, why string }{
		{"c/tests", "c/src", "`target_link_libraries(buffer_test PRIVATE corpus_buffer_core " +
			"cmocka)` links a library declared in c/src/CMakeLists.txt. Which library a test " +
			"binary is built against is stated in that command and nowhere else in this tree"},
		{"go/greeter", "go/cmd/hello", "`deps = [\"//cmd/hello\"]` names a package in this " +
			"workspace, whose root is go/ and not the repository root. Read as " +
			"repository-relative the label names nothing and the declared edge disappears " +
			"with no gap recorded anywhere"},
	} {
		from, to := byPath[want.from], byPath[want.to]
		if from == "" || to == "" {
			t.Fatalf("no module node for %q or %q, so this asserts nothing", want.from, want.to)
		}
		found := false
		for _, e := range g.Edges {
			if e.From == from && e.To == to && e.Kind == "configures" {
				found = true
				break
			}
		}
		if !found {
			var got []string
			for _, e := range g.Edges {
				if e.From == from {
					got = append(got, e.Kind+" -> "+e.To)
				}
			}
			t.Errorf("no configures edge %s -> %s. %s.\nEdges from %s: %v",
				want.from, want.to, want.why, want.from, got)
		}
	}

	// The relative label, which is the negative half of the edge assertions above. `embed =
	// [":greeter"]` in go/greeter/BUILD.bazel names that package itself, so it must draw no
	// edge: a module configuring itself is not structure, and it is the shape a rule that
	// resolved every label against the declaring file's own directory would produce for all
	// of them.
	if id := byPath["go/greeter"]; id != "" {
		for _, e := range g.Edges {
			if e.From == id && e.To == id {
				t.Errorf("self-edge %s on go/greeter. A relative Bazel label names the package "+
					"it is written in, and an edge from a module to itself states nothing", e.Kind)
			}
		}
	}
}

// TestCorpusNamesThePublicSurfaceAndNothingElse covers what a module page says a module
// *offers*, which every other assertion in this file leaves alone: the rest of the bundle is
// about what a directory depends on, and this is the one line about what it exposes.
//
// The positives are cheap and the negatives are the test. "Exported" is a different rule in
// every language here, and each rule has a plausible wrong reading that produces a page an
// agent would act on — a name it writes a call against and the compiler then rejects, or worse,
// a private helper it treats as a supported entry point:
//
//   - Go and Rust and TypeScript state visibility on the declaration, so the failure is a
//     reader that ignores the marker: `unexported`, `builds`, and Rust's non-`pub` items are
//     each beside an exported sibling in the same file, so no rule gets one right by accident.
//   - Python has no keyword at all, only the leading-underscore convention, so `_internal`
//     sits next to `render` in one file. A reader with no convention emits both.
//   - PHP's `private function log` is the inverse case: PHP's *default* is public, so a
//     reader that treats an absent modifier as private loses real surface, and one that
//     ignores modifiers entirely gains a private one.
//   - Ruby's `private` is a sticky section marker rather than a per-method modifier, and it
//     applies to methods only. `Format.normalise` sits under it and `SEPARATOR` above it, so
//     both halves of that rule are checked by one file.
//   - C is the sharpest, because it has no visibility keyword and the rule is inverted:
//     external is the default and `static` is what removes a symbol from the link surface.
//     `corpus_buffer_shrink` is static and must not appear; `corpus_internal_note` is
//     declared in a *private header* and must, because a header's location is a convention
//     and linkage is the fact. Reading the second as private would be a reader substituting
//     its own taste for what the language says.
//   - A test declaration is the case where every language's visibility rule gives the wrong
//     answer. Go's `TestNew` is exported and reachable by nothing but `go test`; PHPUnit's
//     `GreeterTest::testGreets` is public because the runner requires it. Visibility says
//     surface and the truth is the opposite, so the file's *classification* decides and not
//     the declaration. This is the negative with a measurement behind it rather than a
//     hypothetical: when it was missing, test functions were 51% of every name the bundle
//     for this repository showed, and two module pages showed nothing else — the real
//     surface was truncated off the page by names no caller can use.
//
// The count is asserted against the list on the same page, which is the assertion that cannot
// be satisfied by half a fix: the number and the names come out of one pass in assemble, so a
// page claiming five above a list of four is a reader counting something it is not naming.
func TestCorpusNamesThePublicSurfaceAndNothingElse(t *testing.T) {
	dir := buildCorpus(t)
	pages := bundlePages(t, dir)

	// One module per visibility rule, keyed by the page its directory produces. The name is
	// listed exactly as a caller would write it, receiver-qualified for a method, because a
	// bare `String` does not say which type has it.
	for _, want := range []struct {
		page, why string
		names     []string
	}{
		{"modules/greeter-1i6wvb3.md", "Go states visibility in the capital letter, and a " +
			"method is only reachable through its receiver",
			[]string{"`Greeting`", "`Greeting.String`", "`New`"}},
		// Rust's page is src-1slg0rn and TypeScript's is src-14hy2yg. Worth stating, because
		// both languages export a `Greeting` and a `greet` in this corpus, so an entry naming
		// only those two passes against either page and asserts nothing about which rule was
		// read. `Greeting.new` and `formatter` are Rust's alone.
		{"modules/src-1slg0rn.md", "Rust states it in `pub`, on the item and on the module",
			[]string{"`Greeting`", "`Greeting.new`", "`formatter`", "`render`"}},
		// ts/src holds greeter.ts beside greeter.test.ts, so this is the positive half of the
		// test-declaration negative below: excluding a test file's declarations must not be
		// implemented by excluding the directory that holds one.
		{"modules/src-14hy2yg.md", "TypeScript states it in `export`, and a directory holding " +
			"production and test files together still names what the production file exports",
			[]string{"`Greeting`", "`greet`"}},
		{"modules/format.md", "PHP's default is public, so a method with no modifier is surface",
			[]string{"`Renderer`", "`Renderer.render`"}},
		{"modules/corpus-7wiw6h.md", "Ruby's `private` marker applies to methods only, so a " +
			"constant above it and a constant below it are both still reachable",
			[]string{"`Corpus.VERSION`", "`Format.SEPARATOR`", "`Format.greet`"}},
		{"modules/src-1hvu130.md", "C has no visibility keyword: external is the default, so " +
			"a function declared in a private header is still link surface",
			[]string{"`corpus_buffer_make`", "`corpus_internal_note`"}},
	} {
		body, ok := pages[want.page]
		if !ok {
			t.Errorf("no page at %s, so its surface asserts nothing. Pages written:\n  %s",
				want.page, pageNames(pages))
			continue
		}
		for _, name := range want.names {
			if !strings.Contains(body, name) {
				t.Errorf("%s does not name %s among its exports. %s:\n%s",
					want.page, name, want.why, body)
			}
		}
	}

	// The negatives, checked across every page rather than on the one that should have
	// excluded them. A name that must appear nowhere can reach a page through a summary, an
	// attribute or an edge as easily as through the exports line, and asserting only that one
	// page omits it would leave the other routes open.
	//
	// Each of these is a declaration the language says callers cannot reach, sitting in a file
	// whose exported siblings are asserted above — so a reader that emitted the whole symbol
	// table would pass every positive here and fail only this loop.
	//
	// Matched as a whole rendered name rather than as a substring, because these are short
	// identifiers and the corpus deliberately contains longer ones that contain them:
	// `corpus_internal_note` has external linkage and belongs on its page, and a substring
	// search for `_internal` fails on it — which is a test asserting the opposite of the rule
	// it is written to defend.
	for _, bad := range []struct{ name, why string }{
		{"unexported", "a lowercase Go func in go/greeter/greeter.go, and separately a " +
			"non-`export` function in ts/src/greeter.ts. Both sit beside an exported " +
			"declaration in the same file, so naming it is a reader that read the " +
			"declaration and skipped the marker"},
		{"_internal", "a leading-underscore Python function in py/greeter/formatter.py. " +
			"Python states nothing, so the convention is the only rule there is — and a " +
			"reader with no convention publishes every helper in the language"},
		{"corpus_buffer_shrink", "a `static` C function in c/src/buffer.c. `static` is the " +
			"one thing that removes a symbol from the link surface, so naming this claims a " +
			"caller in another translation unit can reach a symbol the linker will not give " +
			"them"},
		{"normalise", "a method under Ruby's `private` section marker in " +
			"ruby/lib/corpus/format.rb. The marker is sticky rather than per-method, so a " +
			"reader that only understands `private def` sees nothing and exports it"},
		{"TestNew", "an exported Go test func in go/greeter/greeter_test.go. Go says it is " +
			"surface and Go is wrong about who can call it: `go test` reaches it and no " +
			"caller can. Visibility is the wrong question for a test file, so the file's " +
			"classification has to answer it — and when it did not, test declarations were " +
			"half of every name this repository's own bundle printed"},
		{"testGreets", "a PHPUnit method in php/tests/GreeterTest.php, public because the " +
			"runner requires it to be. The inverse of the Go case and the same conclusion: no " +
			"visibility rule in any language distinguishes a test from a surface, so a reader " +
			"that trusts the modifier here publishes the test suite as an API"},
	} {
		for name, body := range pages {
			if namesWholeIdentifier(body, bad.name) {
				t.Errorf("%s names %q: %s", name, bad.name, bad.why)
			}
		}
	}

	// The count against the list, on every module page that has one. This is the assertion a
	// partial fix cannot satisfy: the two numbers come from one pass, and a page that says
	// "5 exported symbols" above four names is describing a surface that does not exist.
	checked := 0
	for name, body := range pages {
		if !strings.HasPrefix(name, "modules/") {
			continue
		}
		claimed, names, ok := exportsClaim(body)
		if !ok {
			continue
		}
		checked++
		if claimed != len(names) {
			t.Errorf("%s claims %d export(s) and names %d: %v.\n%s",
				name, claimed, len(names), names, body)
		}
		// The description states the same number in prose, and it is the line a reader sees
		// first — in the index, before opening the page at all.
		if !strings.Contains(body, "description: ") {
			continue
		}
		want := strconv.Itoa(claimed) + " exported symbol"
		if claimed != 1 {
			want += "s"
		}
		if !strings.Contains(body, want) {
			t.Errorf("%s does not say %q in its description, so the prose and the list "+
				"disagree about the size of the surface:\n%s", name, want, body)
		}
	}
	// Without this the loop above is satisfied by a bundle that lists no exports anywhere,
	// which is exactly the state this feature replaced.
	if checked < 20 {
		t.Errorf("only %d module page(s) carry an exports line; the corpus spans 18 languages "+
			"and every one of them declares a public surface, so a number this low means the "+
			"line stopped being emitted rather than that the assertions passed", checked)
	}
}

// namesWholeIdentifier reports whether body mentions name as a complete identifier rather
// than as part of a longer one.
//
// The boundary is the identifier character set, not a word boundary: `\bnormalise\b` matches
// inside `Format.normalise`, which is the same name and should match, but a word boundary also
// fires inside `corpus_internal_note` for `_internal` because `_` is a word character in some
// engines and not others. Deciding it here, on the one rule that matters — a neighbouring
// letter, digit or underscore means this is a different identifier — leaves nothing to a
// regexp dialect. A leading `.` is not a boundary character, so a receiver-qualified method
// still matches its own name.
func namesWholeIdentifier(body, name string) bool {
	ident := func(r byte) bool {
		return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
	}
	for i := 0; ; {
		j := strings.Index(body[i:], name)
		if j < 0 {
			return false
		}
		at := i + j
		end := at + len(name)
		before := at == 0 || !ident(body[at-1])
		after := end == len(body) || !ident(body[end])
		if before && after {
			return true
		}
		i = at + 1
	}
}

// exportsClaim reads a module page's exports line, returning the count it states and the names
// it lists. Parsed out of the rendered page rather than read from the graph on purpose: the
// claim being checked is what a reader sees, and a count that agrees with the graph while
// disagreeing with the list beside it is the failure this catches.
func exportsClaim(body string) (int, []string, bool) {
	for _, line := range strings.Split(body, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "- **Exports** (")
		if !ok {
			continue
		}
		count, list, ok := strings.Cut(rest, "): ")
		if !ok {
			return 0, nil, false
		}
		n, err := strconv.Atoi(count)
		if err != nil {
			return 0, nil, false
		}
		var names []string
		for _, f := range strings.Split(list, ", ") {
			f = strings.TrimSpace(f)
			// The stated bound. A truncated list is honest about being one, and counting
			// "and 12 more" as a name would turn that into an off-by-one failure.
			if strings.HasSuffix(f, " more") {
				return n, nil, false
			}
			if f != "" {
				names = append(names, f)
			}
		}
		return n, names, true
	}
	return 0, nil, false
}

// TestCorpusResolvesIncludesThroughTheSearchPath is the C family's structural assertion, and
// what it covers is the one thing C resolution has instead of a module system: a search order.
//
// There is no manifest naming an include root and no declaration in the file saying where its
// headers live. `#include <corpus/buffer.h>` is a path fragment and nothing more; what turns
// it into a file is the build's `-I`, which signpost does not read. So resolution walks
// outward from the including file's own directory trying the conventional roots, and the
// corpus is laid out to make the walk's boundaries visible:
//
//   - `c/src/buffer.c` includes its own public header through `c/include/`, which is an
//     ancestor's include root and not the repository's. Anchored at the repository root — the
//     shipped behaviour before this — every project in a monorepo with its own `include/`
//     resolves nothing, and C reads as a language signpost cannot follow rather than as a
//     search path mismodelled;
//   - `c/tests/buffer_test.c` includes the same header from a sibling directory, which is the
//     same walk one level further out and is what draws the `tested_by` edge;
//   - `cpp/src/session.cc` does the same through `cpp/include/`, and it must land there rather
//     than in `c/include/` — two include roots exist in this repository and the nearest
//     ancestor's is the one the build declares.
//
// The negative is the quoted form. `#include "internal.h"` in `c/src/buffer.c` names a file in
// that same directory, so it resolves inside the module and draws no edge at all — an edge
// from `c/src` to itself would be a module importing itself, which is not structure.
func TestCorpusResolvesIncludesThroughTheSearchPath(t *testing.T) {
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
	const (
		cSrc     = "c/src"
		cInclude = "c/include/corpus"
		cTests   = "c/tests"
		cppSrc   = "cpp/src"
		cppInc   = "cpp/include/corpus"
	)
	edge := func(kind, from, to string) bool {
		f, tt := byPath[from], byPath[to]
		if f == "" || tt == "" {
			t.Fatalf("no module node for %q or %q, so this test asserts nothing", from, to)
		}
		for _, e := range g.Edges {
			if e.From == f && e.To == tt && e.Kind == kind {
				return true
			}
		}
		return false
	}

	if !edge("imports", cSrc, cInclude) {
		t.Errorf("no imports edge %s -> %s. `#include <corpus/buffer.h>` resolves through "+
			"c/include/, which is this file's nearest ancestor holding an include root and not "+
			"the repository's — a search anchored at the root finds it in no repository laid "+
			"out as more than one project", cSrc, cInclude)
	}
	if !edge("imports", cppSrc, cppInc) {
		t.Errorf("no imports edge %s -> %s. `#include <corpus/session.hpp>` resolves through "+
			"cpp/include/", cppSrc, cppInc)
	}
	// The two include roots are named identically below their own trees, so a walk that did
	// not stop at the nearest ancestor would satisfy the positives above from the wrong one.
	if edge("imports", cppSrc, cInclude) {
		t.Errorf("imports edge %s -> %s. Both trees hold a `corpus/` under `include/`, so the "+
			"C++ include resolved against the C project's root — an edge between two projects "+
			"that name each other nowhere", cppSrc, cInclude)
	}
	if edge("imports", cSrc, cppInc) {
		t.Errorf("imports edge %s -> %s. The C include resolved against the C++ project's "+
			"include root", cSrc, cppInc)
	}
	if !edge("tested_by", cInclude, cTests) {
		t.Errorf("no tested_by edge %s -> %s. buffer_test.c includes the header it exercises "+
			"and is marked as a test twice over — by its `tests/` directory and by its "+
			"`_test.c` suffix", cInclude, cTests)
	}
	// The quoted form, which resolves inside the including module and so draws nothing.
	if edge("imports", cSrc, cSrc) {
		t.Errorf("imports edge %s -> %s. `#include \"internal.h\"` names a file in this same "+
			"directory, so it resolves within the module — a self-edge is not structure",
			cSrc, cSrc)
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
// Those two `.astro` files are read now, which is what the fix was for, and the extension
// that carries the count moved to `.css` in the same directory rather than the assertion
// weakening to one file. That move is the mechanism working: an extension leaves this line by
// gaining a reader, and something genuinely unread has to be standing on it or the count stops
// meaning anything. A stylesheet is the honest successor, since it declares nothing this graph
// can hold — the component extractor blanks `<style>` for that reason — so it is unclassified
// because there is nothing to classify it as, not because a reader is missing.
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

	// Two `.css` and not one, so the count cannot be satisfied by an implementation that
	// reports an extension once however many files carry it.
	//
	// `release` is the entry with no extension at all, and it is a limitation this line exists to
	// state rather than an oddity: it is an executable shell script — shebang, a `source`, a
	// function — and classification is filename-only by design, so nothing reads it and no
	// extractor is ever offered it. Named here because that is the whole job of this line. A
	// bundle that silently omitted a script sourcing a library in the same tree would read as a
	// repository whose scripts declare nothing.
	const wantFiles = 5
	wantKeys := []string{".css (2)", ".svg (1)", "license (1)", "release (1)"}

	got, ok := unclassifiedCount(stderr)
	if !ok {
		t.Fatalf("no `no recognised kind` line in the coverage report. The corpus holds two "+
			"unclassified .css files, an .svg and a LICENSE, so silence here means discovery "+
			"is claiming to have accounted for files nothing read:\n%s", stderr)
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
	// same tree as the two .css files precisely so an implementation that counted by directory
	// fails here, and so do the components: `.vue`, `.svelte` and `.astro` are the extensions
	// that were on this line until the single-file-component extractor claimed them, so a
	// regression that unclassified any of them again shows up here rather than as a quietly
	// smaller graph.
	for _, notWanted := range []string{".md", ".go", ".ts", ".py", ".rs", ".toml", ".json", ".yaml", ".png", "codeowners", "makefile", ".sh", ".ps1", ".psm1", ".vue", ".svelte", ".astro"} {
		if strings.Contains(line, notWanted) {
			t.Errorf("%q is named on the unclassified line, but the corpus classifies and reads "+
				"it: %q", notWanted, line)
		}
	}
	// And it stays a separate line from the languages that have no extractor. Folding the two
	// together is how the defect would come back: `.css` would be counted, printed beside the
	// languages signpost knows and cannot read, and the distinction between "cannot read this
	// language" and "cannot tell what this file is" — which is the difference between a missing
	// extractor and a missing classification — would be gone.
	if ext := coverageLine(stderr, "no extractor for"); strings.Contains(ext, ".css") {
		t.Errorf("the unclassified .css is named on the no-extractor line: %q", ext)
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
//     maps onto an asset rather than source;
//   - shell `./lib/logs.sh` — one letter from the `lib/log.sh` the same script sources
//     successfully two lines above it;
//   - svelte `./Badges.svelte` — the same shape in a component, one letter from the
//     `./Badge.svelte` imported on the line above it. It lands here rather than among the
//     unresolved for the reason the shell entry does: a relative specifier names a file, so a
//     path reaching nothing cannot be an npm package somebody forgot to declare. A component
//     tree is where this matters most, since every component import in a real frontend is
//     relative and carries its extension.
//
// The shell entry is here rather than among the unresolved for a structural reason, and it is
// the reason shell appears in this test and in no other gap assertion: there is no shell package
// registry. `source` names a file, so a path reaching nothing cannot be a dependency somebody
// forgot to declare — there is no gem, npm or NuGet it could be naming instead — and resolveShell
// returns internal with an empty ID rather than falling through to a registry lookup. So every
// shell gap in every repository lands here by construction, and one appearing on the unresolved
// line would mean the resolver had begun inventing packages for a language that has none.
//
// All four must be counted here and none may appear among the unresolved, since there is nothing
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
		"shell ./lib/logs.sh",
		"svelte ./Badges.svelte",
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
			"the four below stopped being counted, or was resolved to something it should not "+
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
	//
	// `./lib/log.sh` is the shell half of that, and it is the pair that matters most for the
	// language: it is sourced by the same script, on the line above the one that reaches nothing,
	// through the same `$SCRIPT_DIR` anchor. So the difference between the entry above and this
	// one is a single letter in the filename — which is what a resolver too eager to match a
	// sibling would erase, turning every mistyped source into a satisfied edge.
	//
	// `./Badge.svelte` is the same pair a third time, in the language where it is the ordinary
	// case rather than a mistake worth noticing: it is imported by the same component, on the
	// line above the one that reaches nothing, and differs by one letter. `../styles/card.css`
	// is a fourth kind — a stylesheet named by a component's `<style>` block, which is not a
	// node in this graph at all — so it belongs on neither gap line, and an extractor that read
	// the style region would put it on this one.
	for _, notWanted := range []string{
		"example.com/corpus/greeter/greeter",
		"@corpus/entry",
		"@corpus/core",
		"api.client",
		"./greeter",
		"./lib/log.sh",
		"./lib/retry.sh",
		"./Badge.svelte",
		"../components/Avatar.vue",
		"card.css",
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
		strings.Contains(u, "logo.svg") || strings.Contains(u, "Badges.svelte") {
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
// TestCorpusIndexStatesEveryStructuralFinding is the regression for issue #42: four of the
// five findings design §7.1 promises in `index.md` were computed on every run, printed by
// `graph show`, and never written into the bundle. The analysis reached a terminal and stopped
// there, which for an agent reading a checkout is nowhere.
//
// It runs on the corpus rather than on this repository because the corpus is the only tree that
// carries both boundaries at once. Signpost's own bundle has no import cycles and no islands,
// so a self-hosted assertion could only ever check the "none" wording, and an emitter that
// hardcoded "none" everywhere would pass it. The corpus has a genuine two-node island — the
// Terraform `api` and `db` services, linked to each other and to nothing else — alongside no
// cycles, so the positive and negative halves are asserted against the same file.
//
// The counts are deliberately not asserted, per this file's rule: 31 cross-cluster edges is a
// number every extractor improvement moves, and a test that pins it teaches people to update
// the number rather than read the diff. What is asserted is that each finding is *named*, that
// the one with something to report names the concepts and links to their pages, and that the
// ones with nothing to report say so instead of vanishing.
func TestCorpusIndexStatesEveryStructuralFinding(t *testing.T) {
	dir := buildCorpus(t)
	idx := bundlePages(t, dir)[okf.IndexPage]
	if idx == "" {
		t.Fatal("the build wrote no index")
	}

	// Every finding is named, whatever it found. This is the half that made the section
	// worth adding: a section that disappears when clean is indistinguishable from one the
	// build failed to write, so a reader cannot tell a clean repository from a broken
	// generator without running the tool themselves.
	for _, want := range []string{
		"### Structural findings",
		"**Import cycles:",
		"**Cross-cluster edges:",
		"**Disconnected islands:",
		"**Unconnected concepts:",
	} {
		if !strings.Contains(idx, want) {
			t.Errorf("the index does not state %q, so the finding is unavailable to anyone "+
				"who did not run `graph show`:\n%s", want, idx)
		}
	}

	// The negative boundary. The corpus has no import cycle, and the absence has to be
	// written down as the result it is rather than omitted.
	if !strings.Contains(idx, "**Import cycles: none.**") {
		t.Errorf("the corpus has no import cycles and the index does not say so:\n%s", idx)
	}

	// The positive boundary, from the same file: the Terraform services are an island, and
	// the finding names them and links to their pages. Asserted by name rather than by count
	// so an extractor that stopped producing the island fails here.
	island := findingLine(idx, "**Disconnected islands:")
	if strings.Contains(island, "none") {
		t.Errorf("the corpus's two-node Terraform island is not reported:\n%s", idx)
	}
	for _, want := range []string{"[api](./services/api.md)", "[db](./services/db.md)"} {
		if !strings.Contains(idx, want) {
			t.Errorf("the island finding does not link %s:\n%s", want, idx)
		}
	}

	// Cross-cluster edges are non-empty here, end to end through the binary: the corpus falls
	// into several communities with edges between them, so a `none` means the cluster pass or
	// Bridges() stopped producing anything on the real build path.
	//
	// It does not assert that `indexFindings` calls `Clusters()` itself, and the distinction
	// is worth stating because it was measured: removing that call leaves this test green,
	// since `build` already clusters before writing. The emitter's own test in internal/okf
	// is what covers it, on a graph nothing else has clustered.
	if bridges := findingLine(idx, "**Cross-cluster edges:"); strings.Contains(bridges, "none") {
		t.Errorf("the corpus falls into several clusters with edges between them, so a "+
			"`none` here means the cluster pass did not run:\n%s", idx)
	}
}

// findingLine returns the index line beginning with prefix, or "" when there is none.
func findingLine(idx, prefix string) string {
	for _, line := range strings.Split(idx, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- "+prefix) {
			return line
		}
	}
	return ""
}

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

	// And the stub's second sentence has to be true of the bundle sitting next to it, which is
	// the assertion no unit test on the string can make. It tells a reader that a data store's
	// page names the modules that write and read it; this checks that a bundle built from a
	// repository with data stores in it actually renders both. An instruction naming content the
	// artifact does not carry is worse than no instruction — a model follows it once, finds a
	// page that answers nothing, and stops trusting the file (ADR 0034). This stage is where
	// that can be caught, because it has both the stub and a built bundle.
	pages := bundlePages(t, dir)
	orders, ok := pages["data/orders.md"]
	if !ok {
		t.Fatalf("no data/orders.md, so the corpus no longer has the data store the stub's "+
			"triage sentence sends a reader to: %v", sortedPageNames(pages))
	}
	for _, sentence := range []string{"**Written by**", "**Read by**"} {
		if !strings.Contains(orders, sentence) {
			t.Errorf("data/orders.md carries no %s sentence, and the AGENTS.md stub appended "+
				"above promises a reader that it does. Either the stub's claim or the page's "+
				"content is wrong, and the stub is the one that costs the reader's trust:\n%s",
				sentence, orders)
		}
	}
}

// TestCorpusViewServesWhatItAnalysed is `view`'s stage, and it is here rather than only in
// view_test.go for the reason at the top of this file: the unit tests build their own
// Options and internal/view's tests build their own handler, so neither runs the pipeline
// that connects them. This runs the binary against a repository signpost's own tree cannot
// produce and reads what came back over the socket.
//
// The bug that earns the stage is the one `view` shipped with: `-port N` on a taken port
// bound a different one and printed a URL nobody asked for. Both halves are asserted, which
// is what makes it a boundary — a fix that made every collision fatal would break the
// default and still pass a one-sided check.
//
// Not a count of nodes and edges, for the reason this file gives. What is asserted is that
// the graph the socket returns is the graph the analysis built, that it carries a module from
// each first-class language — so a pipeline that lost the extractors is caught — and that
// nothing was written to the repository, which is `view`'s one standing promise.
func TestCorpusViewServesWhatItAnalysed(t *testing.T) {
	dir := corpusRepo(t)
	before := workingTree(t, dir)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"view", "-port", port, "-no-open", "-quiet", "-repo", "example.com/corpus", dir}

	// The negative half first, while the port is still held. Run against a deadline because
	// the failure mode is not a bad exit code — under the bug it serves forever, and an
	// inline call would hang the package rather than report anything.
	type result struct {
		stderr string
		code   int
	}
	done := make(chan result, 1)
	go func() {
		var out, errOut lockedBuffer
		code := run(args, &out, &errOut)
		done <- result{errOut.String(), code}
	}()
	select {
	case r := <-done:
		if r.code == 0 {
			t.Fatalf("view exited 0 with -port %s taken; a named port that cannot be bound is an error", port)
		}
		if !strings.Contains(r.stderr, "127.0.0.1:"+port) {
			t.Errorf("the error does not name the address it could not bind: %q", r.stderr)
		}
	case <-time.After(90 * time.Second):
		t.Fatalf("view is still running with -port %s taken; it fell back to a free port "+
			"instead of reporting the collision", port)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	// The positive half: the same port, now free, and the command serves the analysis on it.
	var out, errOut lockedBuffer
	go func() { _ = run(args, &out, &errOut) }()

	base := "http://127.0.0.1:" + port
	client := &http.Client{Timeout: 10 * time.Second}
	body := viewGet(t, client, base+"/graph.json", &out, &errOut)

	var doc struct {
		Nodes []struct {
			ID   string `json:"id"`
			Lang string `json:"lang"`
		} `json:"nodes"`
		Edges []json.RawMessage `json:"edges"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("the served graph is not the shape graph.js reads: %v\n%s", err, body)
	}
	if len(doc.Nodes) == 0 || len(doc.Edges) == 0 {
		t.Fatalf("the served graph has %d nodes and %d edges; the corpus has modules that "+
			"import each other", len(doc.Nodes), len(doc.Edges))
	}
	// One module per first-class language, which is what says the pipeline behind the socket
	// is the whole pipeline rather than a Go-only path through it.
	want := map[string]bool{"go": false, "typescript": false, "python": false, "rust": false}
	for _, n := range doc.Nodes {
		if _, ok := want[n.Lang]; ok {
			want[n.Lang] = true
		}
	}
	for lang, found := range want {
		if !found {
			t.Errorf("no %s module reached the served graph; the analysis behind the socket "+
				"is missing a language", lang)
		}
	}

	// The page itself, which is where a repository's own strings are interpolated. The corpus
	// exists because it holds paths this repository cannot: a bracketed Next.js route, a
	// backtick, a `](`. Escaped rather than dropped is the requirement — a reader has to see
	// the real name — and the page has to name the repository it describes.
	page := string(viewGet(t, client, base+"/", &out, &errOut))
	if strings.Contains(page, "<script>alert") || strings.Contains(page, "onerror=") {
		t.Errorf("the served page carries executable markup from the repository:\n%s", page)
	}
	if !strings.Contains(page, "example.com/corpus") {
		t.Errorf("the page does not name the repository it describes:\n%s", page)
	}

	// The other side of the same field, and the arm that makes this a boundary rather than a
	// one-directional check: an *unnamed* port that is taken falls back and serves anyway. A
	// fix that made every collision fatal would satisfy the negative half above and break
	// the default, and without this it would pass — the two arms above both pass -port.
	//
	// The default port is occupied for the duration, and if something else already holds it
	// that is the same condition, so either outcome of the bind is usable. Nothing is taken
	// from a server already running there.
	if held, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", view.DefaultPort)); err == nil {
		defer func() { _ = held.Close() }()
	}
	var fbOut, fbErr lockedBuffer
	go func() {
		_ = run([]string{"view", "-no-open", "-quiet", "-repo", "example.com/corpus", dir}, &fbOut, &fbErr)
	}()
	fbURL := viewBanner(t, &fbOut, &fbErr)
	if fbURL == fmt.Sprintf("http://127.0.0.1:%d/", view.DefaultPort) {
		t.Fatalf("the default port was not occupied, so this arm did not reach the fallback: %s", fbURL)
	}
	// Served, not merely announced. A banner printed before a failed listen would be worse
	// than no banner.
	if body := viewGet(t, client, fbURL+"graph.json", &fbOut, &fbErr); len(body) == 0 {
		t.Errorf("the fallback port served an empty graph")
	}
	if banner := fbOut.String(); !strings.Contains(banner, fmt.Sprintf("(port %d was in use)", view.DefaultPort)) {
		t.Errorf("the fallback did not say why the URL is not the default one:\n%s", banner)
	}

	// Nothing written: `view`'s one standing promise, and the reason it is not a `build`
	// variant. A graph.json left in the tree would be the stale second artifact ADR 0008
	// declined to commit, produced by the one command whose output is transient.
	after := workingTree(t, dir)
	for rel, sum := range after {
		if _, ok := before[rel]; !ok {
			t.Errorf("view created %s", rel)
			continue
		}
		if before[rel] != sum {
			t.Errorf("view wrote to %s", rel)
		}
	}
	for rel := range before {
		if _, ok := after[rel]; !ok {
			t.Errorf("view removed %s", rel)
		}
	}
}

// viewBanner waits for the URL `view` prints and returns it.
//
// Read out of the banner rather than assumed, because the port is the thing under test: an
// arm that expects a fallback cannot know the number in advance, and one that computed it
// would be asserting its own arithmetic.
func viewBanner(t *testing.T, out, errOut *lockedBuffer) string {
	t.Helper()
	re := regexp.MustCompile(`http://127\.0\.0\.1:\d+/`)
	deadline := time.Now().Add(90 * time.Second)
	for {
		if m := re.FindString(out.String()); m != "" {
			return m
		}
		if time.Now().After(deadline) {
			t.Fatalf("view printed no URL\nstdout:\n%s\nstderr:\n%s", out.String(), errOut.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// viewGet fetches from a server that is still starting, and fails with what the command
// printed if it never answers. The retry is not flakiness tolerance: `view` analyses the
// repository before it binds, so there is a real interval where nothing is listening.
func viewGet(t *testing.T, client *http.Client, url string, out, errOut *lockedBuffer) []byte {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err == nil {
			body, rerr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if rerr != nil {
				t.Fatal(rerr)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s = %d\n%s", url, resp.StatusCode, body)
			}
			return body
		}
		if time.Now().After(deadline) {
			t.Fatalf("view never answered on %s: %v\nstdout:\n%s\nstderr:\n%s",
				url, err, out.String(), errOut.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// workingTree is treeSnapshot without .git, which changes on its own — index mtimes,
// reflogs, an auto-gc that `git commit` starts detached — and none of that is signpost
// writing to the repository.
func workingTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := treeSnapshot(t, root)
	for rel := range files {
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			delete(files, rel)
		}
	}
	if len(files) == 0 {
		t.Fatalf("%s has no files outside .git", root)
	}
	return files
}

// TestCorpusDrawsEveryDataEdgeAndNoneItGuessedAt is the stage for the data pass, and it
// asserts counts rather than presence for a reason the store fixtures are built around.
//
// Twelve stores hold the same three shapes — a query whose table is spelled out, a query
// whose table the language builds at run time, and a string that mentions a verb and is
// prose — because the reader is one implementation and the *recovery* of a string literal is
// twelve: eleven scanner configurations plus Go's parser. A fixture in one language proves
// the reader; a fixture per recovery path proves the recovery. This stage is where the
// second claim is checked against the binary rather than against a scanner unit test.
//
// The failure it guards is not a missing edge. It is an edge nobody wrote: a page named
// `%s`, a table called `the` read out of "could not update the order", a writer minted from
// the middle line of a C++ raw string. Every one of those looks like a richer map, and a
// presence assertion — "orders has a writer" — passes for all of them. A count fails in both
// directions, which is the only shape that catches the over-claim.
func TestCorpusDrawsEveryDataEdgeAndNoneItGuessedAt(t *testing.T) {
	dir := corpusRepo(t)
	// No -quiet: the interpolated-gap count is on stderr and -quiet is what suppresses it.
	_, stderr, code := invoke(t, "build", "-repo", "example.com/corpus", dir)
	if code != 0 {
		t.Fatalf("build failed: exit = %d\n%s", code, stderr)
	}

	stdout, _, code := invoke(t, "graph", "export", "-format", "json", "--quiet", dir)
	if code != 0 {
		t.Fatalf("export failed: exit = %d", code)
	}
	var g struct {
		Nodes []struct {
			ID, Kind, Path, Title string
		}
		Edges []struct {
			From, To, Kind, Confidence, Source string
			Weight                             int
		}
	}
	if err := json.Unmarshal([]byte(stdout), &g); err != nil {
		t.Fatalf("export did not produce JSON: %v", err)
	}

	// The four tables the two migrations declare, and nothing else. A data node is created
	// only by a migration (ADR 0034), so this list is also the assertion that no name a
	// *source file* held became a page — which is where `%s`, `{table}` and `$table` would
	// arrive from.
	tables := map[string]string{} // node ID -> table name
	var names []string
	for _, n := range g.Nodes {
		if n.Kind != "Data Store" {
			continue
		}
		tables[n.ID] = n.Title
		names = append(names, n.Title)
	}
	sort.Strings(names)
	wantTables := []string{"audit_log", "customers", "orders", "orders_customer_idx"}
	if strings.Join(names, " ") != strings.Join(wantTables, " ") {
		t.Fatalf("data store nodes = %v, want %v.\n\nHigher means a table page was minted from "+
			"something no migration declares — an interpolation placeholder, a raw string's "+
			"unquoted middle line, a bare word out of prose — and a committed artifact naming "+
			"a table that does not exist is worse than one missing an edge. Lower means the "+
			"migration reader stopped seeing a CREATE.", names, wantTables)
	}

	byPath := map[string]string{}
	for _, n := range g.Nodes {
		if n.Path != "" {
			byPath[n.ID] = n.Path
		}
	}
	access := map[string][]string{}
	total := 0
	for _, e := range g.Edges {
		if e.Kind != "writes" && e.Kind != "reads" {
			continue
		}
		total++
		table, ok := tables[e.To]
		if !ok {
			t.Errorf("a %s edge from %s points at %s, which is not a data store. A data edge's "+
				"target is a table, and one pointing anywhere else means the access was "+
				"attached to whatever node happened to carry the name", e.Kind, e.From, e.To)
			continue
		}
		access[e.Kind+" "+table] = append(access[e.Kind+" "+table], byPath[e.From])
		// Provenance on every edge rather than sampled. A data edge is the one an on-call
		// reader acts on — "who writes this table" — and one with no file behind it is an
		// assertion they cannot check.
		if e.Source == "" {
			t.Errorf("the %s edge %s -> %s names no source file. A reader following a data "+
				"edge is looking for the statement, and an edge with no provenance sends them "+
				"to grep the repository", e.Kind, byPath[e.From], table)
		}
		// Extracted and never Ambiguous: the pass is deterministic, and ADR 0034 reserves
		// Ambiguous for a model flagging its own output. A deterministic pass that hedged
		// would teach a reader to discount every edge in the bundle.
		if e.Confidence != "extracted" {
			t.Errorf("the %s edge %s -> %s has confidence %q, want extracted. The SQL pass "+
				"reads a literal name or draws nothing (ADR 0034)",
				e.Kind, byPath[e.From], table, e.Confidence)
		}
		// No weight, ever. A module writing `orders` from eleven call sites is more verbose,
		// not eleven times a writer, and a weight there renders as coupling strength.
		if e.Weight != 0 {
			t.Errorf("the %s edge %s -> %s carries weight %d. A data edge has no count — "+
				"eleven statements against one table is one module writing it",
				e.Kind, byPath[e.From], table, e.Weight)
		}
	}

	// The twelve recovery paths, named by the module each store fixture lands in. Asserted as
	// the exact set rather than as a count, because a count of twelve is satisfied by one
	// language's store being read twice and another's not at all — and "not at all" is the
	// failure the fixtures exist to catch. `c/src` and `cpp/src` share one scanner (ADR 0022)
	// and `ts/packages/api/src` covers both JS-like configurations, which is why twelve
	// recovery paths reach thirteen module entries.
	everyStore := []string{
		"c/src",
		"cpp/src",
		"dotnet/Corpus.Api",
		"go/store",
		"jvm/src/main/java/com/example/store",
		"jvm/src/main/kotlin/com/example/app",
		"php/src",
		"powershell/src/Corpus",
		"py/greeter",
		"ruby/lib/corpus",
		"rust/src",
		"shell/scripts",
		"ts/packages/api/src",
	}
	// Every store reads `orders`. Twelve of the thirteen write `customers`: Go is the one that
	// writes `orders` instead, so `orders` has exactly one writer and the read/write split is
	// not an artefact of a single shared fixture.
	//
	// `reads customers` is the short list on purpose. Seven of the stores hold a second read
	// naming `customers` — a JOIN, or Rust's raw-string LIKE query — and the rest do not, so
	// this is the entry that fails if the reader starts finding a table per bare word.
	want := map[string][]string{
		"reads orders": everyStore,
		"writes customers": {
			"c/src",
			"cpp/src",
			"dotnet/Corpus.Api",
			"jvm/src/main/java/com/example/store",
			"jvm/src/main/kotlin/com/example/app",
			"php/src",
			"powershell/src/Corpus",
			"py/greeter",
			"ruby/lib/corpus",
			"rust/src",
			"shell/scripts",
			"ts/packages/api/src",
		},
		"writes orders": {"go/store"},
		"reads customers": {
			"dotnet/Corpus.Api",
			"go/store",
			"jvm/src/main/java/com/example/store",
			"php/src",
			"powershell/src/Corpus",
			"rust/src",
			"shell/scripts",
		},
	}
	for key, wantMods := range want {
		got := access[key]
		sort.Strings(got)
		// Compared as joined text rather than element by element: a module path holds no
		// spaces, so the join is unambiguous, and the failure message wants both lists whole
		// anyway.
		if strings.Join(got, " ") != strings.Join(wantMods, " ") {
			t.Errorf("%s: modules = %v, want %v.\n\nA module missing here is a recovery path "+
				"that stopped working, and since the store fixtures are one per path the "+
				"absent entry names the scanner configuration to look at. A module appearing "+
				"that should not is a table read out of text that is not a statement.",
				key, got, wantMods)
		}
	}
	// The count as well as the sets, so an access pair this test does not name cannot appear
	// at all. `writes audit_log` is what that would be, and it is why the total is asserted
	// alongside the four sets rather than derived from them and trusted.
	wantTotal := 0
	for _, mods := range want {
		wantTotal += len(mods)
	}
	if total != wantTotal {
		t.Errorf("%d data edge(s), want %d. The sets above name every one, so a difference "+
			"here is an access pair nothing in this test expects", total, wantTotal)
	}

	// audit_log is the negative boundary, and it is the C++ raw string's. `scanC` does not
	// model `R"delim(...)delim"` — the delimiter is arbitrary text rather than a run of
	// hashes, so Rust's rule does not transfer — and the fixture states the cost: the query is
	// read as nothing. That is the acceptable direction. The unacceptable one is a table
	// minted from the delimiter, or an access read off `FROM audit_log` sitting unquoted in
	// the middle of the literal, which is exactly the text a looser reader finds.
	//
	// So audit_log must exist, because a migration creates it, and must carry no access at
	// all, because nothing in the corpus touches it in a form this reader recovers.
	for key, mods := range access {
		if strings.HasSuffix(key, " audit_log") {
			t.Errorf("audit_log has %s: %v. Nothing in the corpus reaches it in a readable "+
				"form — the only statement naming it is inside a C++ raw string, which the "+
				"scanner does not model — so an edge here was read out of an unmodelled "+
				"literal's unquoted body, meaning a delimiter or a bare word became an access",
				key, mods)
		}
	}

	// The page, since the graph is not what a reader opens. A data page is the one page that
	// renders *incoming* edges, so these two sentences are the pass's whole output as far as
	// anybody using the bundle is concerned.
	pages := bundlePages(t, dir)
	orders, ok := pages["data/orders.md"]
	if !ok {
		t.Fatalf("no data/orders.md in the bundle: %v", sortedPageNames(pages))
	}
	if !strings.Contains(orders, "**Written by**: [go/store]") {
		t.Errorf("data/orders.md does not name go/store as its writer. One module in the whole "+
			"corpus writes orders, and which one is the question a data page exists to "+
			"answer:\n%s", orders)
	}
	for _, mod := range everyStore {
		if !strings.Contains(orders, "["+mod+"]") {
			t.Errorf("data/orders.md does not name %s. Every store fixture reads orders, and "+
				"the reader list is what an on-call reader scans:\n%s", mod, orders)
		}
	}
	auditLog, ok := pages["data/audit-log.md"]
	if !ok {
		t.Fatalf("no data/audit-log.md in the bundle: %v", sortedPageNames(pages))
	}
	for _, sentence := range []string{"**Written by**", "**Read by**"} {
		if strings.Contains(auditLog, sentence) {
			t.Errorf("data/audit-log.md carries a %s sentence. The table exists because a "+
				"migration creates it and nothing reaches it in a form this reader recovers, "+
				"so the correct page names no module at all:\n%s", sentence, auditLog)
		}
	}
	// And no page named after interpolation syntax. Checked against the page names rather
	// than the node titles because the pages are what gets committed: a file called
	// `data/table.md` in a repository is a claim, and a slugger that strips `%`, `{` and `$`
	// turns every placeholder into a plausible-looking name.
	for rel := range pages {
		if !strings.HasPrefix(rel, "data/") {
			continue
		}
		switch rel {
		case "data/audit-log.md", "data/customers.md", "data/orders.md",
			"data/orders-customer-idx.md":
			continue
		}
		t.Errorf("%s is a data page, and the corpus declares four tables. It was named after "+
			"something a source file held rather than something a migration declares — a "+
			"format placeholder, an interpolated variable, a bare word out of prose", rel)
	}

	// The gaps, which are the other half of ADR 0034 and the half no presence assertion
	// reaches. Each store holds a statement whose table it builds at run time, and every one
	// must be counted and none resolved. Asserted as a number rather than "at least one" for
	// the reason the counts above are: a gap silently dropped is a query read as touching no
	// table, which in the output is indistinguishable from a module that touches none.
	//
	// Fifteen, and the two above thirteen are a known limitation rather than two more
	// interpolated statements — `TestProseClosedByAPunctuationMarkIsCountedAsAGap` in
	// internal/sqlstmt states it. The TypeScript and shell prose fixtures both read
	// "insert into <interpolation> failed, retrying", and a comma closes a two-run of bare
	// words exactly as it does in `FROM orders o, customers c`, so the gate accepts them and
	// their interpolated name is counted. No table is drawn, which is the bound on the cost.
	// The number is written here rather than derived so that fixing the limitation fails this
	// stage and points at the test that explains it.
	const wantGaps = 15
	gaps, ok := interpolatedCount(stderr)
	if !ok {
		t.Fatalf("no run-time table-name line in the coverage report. Every store fixture "+
			"builds one table name at run time, so silence here means the pass is claiming it "+
			"read every statement in the corpus:\n%s", stderr)
	}
	if gaps != wantGaps {
		t.Errorf("%d statement(s) building a table name at run time, want %d.\n\nLower means "+
			"an interpolated statement stopped being counted, which is the silent failure ADR "+
			"0034 separates the two counts to prevent — the module reads as touching one table "+
			"fewer and nothing says so. Higher means prose was counted as a query signpost "+
			"could not resolve, which inflates the number a reader uses to judge how much of "+
			"the map is missing.\n\nReport:\n%s", gaps, wantGaps, stderr)
	}
	// And the other gap count stays empty. A table a *source* names and no migration declares
	// is a separate line with a separate remedy — write the migration, or fix the typo — and
	// the corpus has none, so anything here is a name read out of text that is not a
	// statement. Folding the two counts into one is what would hide it.
	if line := coverageLine(stderr, "no migration declares"); line != "" {
		t.Errorf("the coverage report names tables no migration declares: %q. Every table the "+
			"corpus's sources name is created by db/migrations, so an entry here is a bare "+
			"word the reader took for a table name", line)
	}
}

// interpolatedCount reads the statement count out of the run-time table-name line.
//
// The statement count is the only number on the line, and deliberately: unlike an unresolved
// import there is no name to group by. `DELETE FROM %s` and `DELETE FROM {table}` are the
// same gap to a reader — a statement whose target is not knowable from the source — and a
// list of the placeholders would be a list of format-string syntax rather than of tables.
func interpolatedCount(stderr string) (int, bool) {
	for _, line := range strings.Split(stderr, "\n") {
		var stmts int
		if _, err := fmt.Sscanf(strings.TrimSpace(line),
			"%d statement(s) build a table name at run time", &stmts); err == nil {
			return stmts, true
		}
	}
	return 0, false
}
