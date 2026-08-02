// Package practice reports what a repository declares about how it is built, tested,
// gated, and owned — and, more usefully, what it does not.
//
// Design §9.1. The nine things a readiness scorer looks for are almost entirely facts
// §4.1 already extracts: workflow reading finds the gates, repo.go reads CODEOWNERS and
// ADRs and Makefile targets, the dependency readers see whether an observability library
// is present, and discovery classifies docs and lock files. So this package extracts
// nothing. It reads the facts the pipeline already produced and states what they add up
// to, which is why it has no file I/O and no parser: every function here is pure, and a
// bug in it cannot corrupt a bundle.
//
// # No score
//
// There is no maturity level, no percentage, and no grade, and that is the design
// decision this package exists to hold. A 1–5 rubric is an opinion that has to be
// defended and re-tuned per repository, and it invites the failure the whole project is
// built against: a repository at "level 2" reads as *measured* when it has only been
// *judged*. A finding is durable — "no test command is declared" is true or false, and
// the file that would carry one is nameable. The ranking belongs to whoever is deciding
// what to work on.
//
// This is also why Finding has no severity field. Ordering findings by importance is the
// same act as scoring, one step removed: it says which absence matters most, which
// depends on what the reader is trying to do. They are ordered by topic instead, fixed
// below, so the page is stable and a reader can scan to the topic they care about.
//
// # Absence is a finding, presence is a fact
//
// Both are emitted, and they are not the same claim. "The repository declares `make
// test`" cites the file and line that says so. "No test command is declared" cites
// nothing, because there is nothing to cite — so it names *where signpost looked*
// instead. Without that, an absence is indistinguishable from a gap in extraction, which
// is §4.2's rule that absence of measurement is never a clean bill of health.
package practice

import (
	"path"
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
	"github.com/3rg0n/signpost/internal/manifest"
)

// Topic groups findings on the page. The order of the constants is the order of the
// sections, chosen as the order a contributor meets them: how do I build it, how do I
// test it, what will stop my merge, who reviews it, what am I expected to know.
type Topic string

const (
	TopicBuild         Topic = "build"
	TopicTest          Topic = "test"
	TopicGates         Topic = "gates"
	TopicDependencies  Topic = "dependencies"
	TopicOwnership     Topic = "ownership"
	TopicDocumentation Topic = "documentation"
	TopicObservability Topic = "observability"
	TopicAgentRules    Topic = "agent rules"
)

// topicOrder fixes the section order. Explicit rather than sorted, so the reasoning above
// is reviewable and the page is byte-stable.
var topicOrder = []Topic{
	TopicBuild,
	TopicTest,
	TopicGates,
	TopicDependencies,
	TopicOwnership,
	TopicDocumentation,
	TopicObservability,
	TopicAgentRules,
}

// Finding is one thing the repository declares, or one thing it does not.
type Finding struct {
	Topic Topic
	// Declared distinguishes the two kinds of claim. True means the repository states
	// this and Sources names where; false means it does not, and Looked names where
	// signpost checked.
	Declared bool
	// Text is the finding, as a sentence. Written here rather than assembled by the
	// emitter because a finding is a claim, and the package making the claim is the one
	// that should have to phrase it.
	Text string
	// Sources are repo-relative paths backing a Declared finding, with a line where one
	// is known. Empty when Declared is false.
	Sources []Source
	// Looked names what was searched for an absence: the filenames or manifest kinds
	// that would have carried the fact. Empty when Declared is true.
	Looked []string
}

// Source is a file, and optionally a line, backing a finding.
type Source struct {
	Path string
	// Line is 1-based, or 0 when the fact is the file's existence rather than a
	// statement inside it — a LICENSE file declares its own presence and has no
	// meaningful line.
	Line int
}

// Result is every finding, in topic order.
type Result struct {
	Findings []Finding
}

// Declared and Absent count the two kinds. Reported by the CLI as a count only: two
// numbers are a summary, where a ratio between them would be a score by another name.
func (r *Result) Declared() int {
	n := 0
	for _, f := range r.Findings {
		if f.Declared {
			n++
		}
	}
	return n
}

func (r *Result) Absent() int { return len(r.Findings) - r.Declared() }

// Input is the pipeline output this package reads.
type Input struct {
	// Discovered is the file walk. Nil is tolerated and yields no findings at all
	// rather than a page full of absences: a run with no walk has not established that
	// anything is missing.
	Discovered *discover.Result
	// Manifests are the extracted non-source facts. Nil means the manifest pass did not
	// run, which is reported as such per topic rather than as an absence — see
	// noManifests.
	Manifests *manifest.RunResult
}

// Analyse reports what the repository declares.
//
// Never fails. Every gap is a finding, which is the same fail-open discipline the
// semantic pass follows: a page that is missing because analysis errored teaches a reader
// nothing, where a page saying "no test command is declared" is useful even when the
// reason is that signpost cannot yet read this repository's build system.
func Analyse(in Input) *Result {
	res := &Result{}
	if in.Discovered == nil {
		return res
	}
	facts := factsOf(in.Manifests)
	byTopic := map[Topic][]Finding{}
	add := func(fs ...Finding) {
		for _, f := range fs {
			byTopic[f.Topic] = append(byTopic[f.Topic], f)
		}
	}

	add(buildFindings(facts)...)
	add(testFindings(facts, in.Discovered)...)
	add(gateFindings(facts)...)
	add(dependencyFindings(facts, in.Discovered)...)
	add(ownershipFindings(facts, in.Discovered)...)
	add(documentationFindings(in.Discovered)...)
	add(observabilityFindings(facts)...)
	add(agentRuleFindings(facts, in.Discovered)...)

	for _, t := range topicOrder {
		res.Findings = append(res.Findings, byTopic[t]...)
	}
	return res
}

func factsOf(r *manifest.RunResult) []manifest.Facts {
	if r == nil {
		return nil
	}
	return r.Facts
}

// --- build ---

// buildCommandNames are the script names that mean "build this".
//
// A name set rather than a pattern, and deliberately small. The question is whether the
// repository *declares* a build, and a declaration is a name a human chose to be found by
// — `make build`, `npm run build`. Matching loosely on any script whose command mentions a
// compiler would report a build command in repositories that have not declared one, which
// is the false-positive direction that makes a findings page untrustworthy.
var buildCommandNames = map[string]bool{
	"build": true, "compile": true, "dist": true, "package": true,
	"all": true, "release": true,
}

func buildFindings(facts []manifest.Facts) []Finding {
	var srcs []Source
	var names []string
	for _, f := range facts {
		for _, s := range f.Scripts {
			if buildCommandNames[strings.ToLower(s.Name)] {
				srcs = append(srcs, Source{Path: f.Path, Line: s.Line})
				names = append(names, s.Name)
			}
		}
	}
	if len(srcs) > 0 {
		return []Finding{{
			Topic:    TopicBuild,
			Declared: true,
			Text:     "A build command is declared: " + joinNames(names) + ".",
			Sources:  dedupeSources(srcs),
		}}
	}
	return []Finding{{
		Topic: TopicBuild,
		Text: "No build command is declared. An agent asked to build this repository has " +
			"to infer how, and its first guess is not reviewable.",
		Looked: []string{"Makefile targets", "package.json scripts", "Cargo aliases"},
	}}
}

// --- test ---

var testCommandNames = map[string]bool{
	"test": true, "tests": true, "check": true, "verify": true,
	"unit": true, "integration": true, "coverage": true,
}

func testFindings(facts []manifest.Facts, d *discover.Result) []Finding {
	var out []Finding

	var srcs []Source
	var names []string
	for _, f := range facts {
		for _, s := range f.Scripts {
			if testCommandNames[strings.ToLower(s.Name)] {
				srcs = append(srcs, Source{Path: f.Path, Line: s.Line})
				names = append(names, s.Name)
			}
		}
	}
	if len(srcs) > 0 {
		out = append(out, Finding{
			Topic:    TopicTest,
			Declared: true,
			Text:     "A test command is declared: " + joinNames(names) + ".",
			Sources:  dedupeSources(srcs),
		})
	} else {
		out = append(out, Finding{
			Topic: TopicTest,
			Text: "No test command is declared. This is the fact an agent most needs " +
				"before it offers to add a test, because it decides where the test goes " +
				"and how it is run.",
			Looked: []string{"Makefile targets", "package.json scripts", "Cargo aliases"},
		})
	}

	// Test files are counted, not judged. A count is a fact about the tree; a ratio
	// against production files would be a coverage claim this package cannot support —
	// signpost does not run the tests and does not know what they cover.
	tests := 0
	for _, f := range d.Files {
		if f.IsTest && d.Analyses(f) {
			tests++
		}
	}
	if tests > 0 {
		out = append(out, Finding{
			Topic:    TopicTest,
			Declared: true,
			Text:     plural(tests, "test file") + " in the tree.",
		})
	} else {
		out = append(out, Finding{
			Topic: TopicTest,
			Text: "No test files were found. Either there are none, or this repository " +
				"names them in a way signpost does not recognise — the coverage report says " +
				"which languages were read.",
			Looked: []string{"files classified as tests by internal/discover"},
		})
	}
	return out
}

// --- gates ---

func gateFindings(facts []manifest.Facts) []Finding {
	if noManifests(facts) {
		return []Finding{manifestsNotRead(TopicGates, "CI gates")}
	}

	var gates, ungated []Source
	var gateNames []string
	workflows := 0
	for _, f := range facts {
		if f.Kind != manifest.KindWorkflow {
			continue
		}
		workflows++
		for _, j := range f.Jobs {
			if j.Gate {
				gates = append(gates, Source{Path: f.Path, Line: j.Line})
				gateNames = append(gateNames, j.Name)
			} else {
				ungated = append(ungated, Source{Path: f.Path, Line: j.Line})
			}
		}
	}

	if workflows == 0 {
		return []Finding{{
			Topic: TopicGates,
			Text: "No CI workflows were found, so nothing automated can block a bad " +
				"change. Every check is whatever a reviewer remembers to run.",
			Looked: []string{".github/workflows"},
		}}
	}
	if len(gates) == 0 {
		return []Finding{{
			Topic: TopicGates,
			Text: plural(workflows, "CI workflow") + " exist, but no job runs on a pull " +
				"request or on a push to the default branch — so none of them can stop a " +
				"merge.",
			Looked: []string{".github/workflows"},
		}}
	}
	out := []Finding{{
		Topic:    TopicGates,
		Declared: true,
		Text: plural(len(gates), "job") + " can block a merge: " + joinNames(gateNames) +
			".",
		Sources: dedupeSources(gates),
	}}
	if len(ungated) > 0 {
		// Stated as a fact, not a problem. A release job or a nightly scan is correctly
		// ungated, and calling that a gap would be the rubric this package refuses to be.
		out = append(out, Finding{
			Topic:    TopicGates,
			Declared: true,
			Text: plural(len(ungated), "further CI job") + " run outside that gate — on a " +
				"schedule, a tag, or manually.",
			Sources: dedupeSources(ungated),
		})
	}
	return out
}

// --- dependencies ---

func dependencyFindings(facts []manifest.Facts, d *discover.Result) []Finding {
	if noManifests(facts) {
		return []Finding{manifestsNotRead(TopicDependencies, "declared dependencies")}
	}
	var out []Finding

	// Lockfiles are reported per ecosystem rather than as one yes/no. A repository with
	// a Go module and an npm package has two supply chains, and one lockfile does not
	// pin the other — a single "a lockfile exists" line would hide exactly that.
	locks := map[string][]Source{}
	manifests := map[string][]Source{}
	// Declared counts dependencies per ecosystem, because an unpinned ecosystem and one
	// with nothing to pin are different facts and the lockfile check cannot tell them
	// apart on its own. signpost's own go.mod had an empty require block and was reported
	// as "declared but not pinned, so two builds can resolve different versions" — false,
	// since nothing was declared and there was nothing to resolve.
	declared := map[string]int{}
	for _, f := range facts {
		if f.Kind == manifest.KindLock {
			eco := lockEcosystem(f.Path)
			if eco == "" {
				// A lockfile whose basename is not one this package knows. Skipped rather
				// than bucketed under a placeholder: it would pair with no manifest and so
				// be reported as an orphan lockfile, which is a finding about the
				// repository when the truth is a gap in the list below.
				continue
			}
			locks[eco] = append(locks[eco], Source{Path: f.Path})
			continue
		}
		if eco := manifestEcosystem(f); eco != "" {
			manifests[eco] = append(manifests[eco], Source{Path: f.Path})
			// Every dependency counts, including indirect ones: a go.mod carrying only
			// `// indirect` entries has a closure to pin even though nothing in it was
			// requested by hand, and that closure is exactly what a lockfile pins.
			declared[eco] += len(f.Deps)
		}
	}

	for _, eco := range sortedKeys(manifests) {
		if srcs := locks[eco]; len(srcs) > 0 {
			out = append(out, Finding{
				Topic:    TopicDependencies,
				Declared: true,
				Text:     "The " + eco + " dependencies are pinned by a lockfile.",
				Sources:  dedupeSources(srcs),
			})
			continue
		}
		if declared[eco] == 0 {
			// Stated rather than omitted. "This ecosystem has no dependencies" is a fact an
			// agent can act on — it means no lockfile is missing and no supply chain needs
			// reviewing — and staying silent would read as the check not having run.
			out = append(out, Finding{
				Topic:    TopicDependencies,
				Declared: true,
				Text: "The " + eco + " manifest declares no dependencies, so there is " +
					"nothing for a lockfile to pin.",
				Sources: dedupeSources(manifests[eco]),
			})
			continue
		}
		out = append(out, Finding{
			Topic: TopicDependencies,
			Text: "The " + eco + " dependencies are declared but not pinned by any " +
				"lockfile in the tree, so two builds can resolve different versions.",
			Looked: pathsOf(manifests[eco]),
		})
	}
	// A lockfile for an ecosystem with no manifest is reported rather than dropped: it
	// usually means the manifest is generated, gitignored, or in a directory the walk
	// skipped, and each of those is worth knowing.
	for _, eco := range sortedKeys(locks) {
		if len(manifests[eco]) == 0 {
			out = append(out, Finding{
				Topic:    TopicDependencies,
				Declared: true,
				Text: "A " + eco + " lockfile is present with no manifest beside it in the " +
					"tree.",
				Sources: dedupeSources(locks[eco]),
			})
		}
	}

	if len(manifests) == 0 && len(locks) == 0 {
		out = append(out, Finding{
			Topic: TopicDependencies,
			Text: "No dependency manifest was found, so this repository's supply chain is " +
				"not stated anywhere signpost can read.",
			Looked: []string{"go.mod", "package.json", "pyproject.toml",
				"requirements.txt", "Cargo.toml"},
		})
	}

	if srcs := filesNamed(d, dependabotNames); len(srcs) > 0 {
		out = append(out, Finding{
			Topic:    TopicDependencies,
			Declared: true,
			Text:     "Automated dependency updates are configured.",
			Sources:  srcs,
		})
	} else {
		out = append(out, Finding{
			Topic: TopicDependencies,
			Text: "No automated dependency updates are configured, so a published CVE in a " +
				"dependency is found by whoever happens to look.",
			Looked: dependabotNames,
		})
	}
	return out
}

// manifestEcosystem names the supply chain a manifest declares.
//
// Read from the manifest Kind rather than from Module.Ecosystem, because a manifest that
// declares no dependencies still declares an ecosystem — and Module.Ecosystem is empty on
// several readers' output.
func manifestEcosystem(f manifest.Facts) string {
	switch f.Kind {
	case manifest.KindGoMod:
		return "Go"
	case manifest.KindPackageJSON:
		return "npm"
	case manifest.KindPyProject, manifest.KindRequirement:
		return "Python"
	case manifest.KindCargo:
		return "Cargo"
	}
	return ""
}

// lockEcosystem names the ecosystem a lockfile pins, by basename.
//
// By basename rather than by asking the reader, because internal/manifest deliberately does
// not parse lockfiles — they are derived, often megabytes, and carry no architectural
// signal — so the Facts a lockfile produces name no ecosystem to pair on. That is the right
// call there and the reason the mapping has to live here.
//
// The returned names must match manifestEcosystem's exactly. They are the pairing key, so a
// mismatch does not fail loudly: it reports `go.mod` as unpinned in a repository that
// commits `go.sum`, which is a false accusation rather than a missing line. Both spellings
// are in one file, next to each other, for that reason.
func lockEcosystem(p string) string {
	switch path.Base(p) {
	case "go.sum":
		return "Go"
	case "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "npm-shrinkwrap.json":
		return "npm"
	case "uv.lock", "poetry.lock", "Pipfile.lock", "pdm.lock":
		return "Python"
	case "Cargo.lock":
		return "Cargo"
	}
	return ""
}

var dependabotNames = []string{
	".github/dependabot.yml", ".github/dependabot.yaml",
	"renovate.json", "renovate.json5", ".renovaterc", ".renovaterc.json",
	".github/renovate.json", ".github/renovate.json5",
}

// --- ownership ---

func ownershipFindings(facts []manifest.Facts, d *discover.Result) []Finding {
	var out []Finding

	var owners []Source
	patterns := 0
	for _, f := range facts {
		if f.Kind != manifest.KindCodeowners {
			continue
		}
		patterns += len(f.Owners)
		owners = append(owners, Source{Path: f.Path})
	}
	if patterns > 0 {
		out = append(out, Finding{
			Topic:    TopicOwnership,
			Declared: true,
			Text:     plural(patterns, "ownership rule") + " assign paths to reviewers.",
			Sources:  dedupeSources(owners),
		})
	} else {
		out = append(out, Finding{
			Topic: TopicOwnership,
			Text: "No CODEOWNERS rules were found, so nothing states who reviews a change " +
				"to a given path.",
			Looked: []string{"CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS"},
		})
	}

	for _, want := range []struct {
		names   []string
		present string
		absent  string
	}{
		{
			names:   []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING"},
			present: "The repository states its licence.",
			absent: "No licence file was found. Without one, whether the code may be " +
				"reused at all is unstated.",
		},
		{
			names:   []string{"SECURITY.md", ".github/SECURITY.md", "docs/SECURITY.md"},
			present: "A security policy states how to report a vulnerability.",
			absent: "No security policy was found, so someone who finds a vulnerability " +
				"has to guess where to send it.",
		},
	} {
		if srcs := filesNamed(d, want.names); len(srcs) > 0 {
			out = append(out, Finding{
				Topic: TopicOwnership, Declared: true, Text: want.present, Sources: srcs,
			})
			continue
		}
		out = append(out, Finding{
			Topic: TopicOwnership, Text: want.absent, Looked: want.names,
		})
	}
	return out
}

// --- documentation ---

func documentationFindings(d *discover.Result) []Finding {
	var out []Finding

	if srcs := filesNamed(d, []string{"README.md", "README.rst", "README.adoc",
		"README.txt", "README"}); len(srcs) > 0 {
		out = append(out, Finding{
			Topic: TopicDocumentation, Declared: true,
			Text:    "The repository has a README.",
			Sources: srcs,
		})
	} else {
		out = append(out, Finding{
			Topic:  TopicDocumentation,
			Text:   "No README was found, so the first file a reader opens does not exist.",
			Looked: []string{"README.md", "README.rst", "README.adoc"},
		})
	}

	// Docs are counted excluding the bundle's own pages. Counting them would make the
	// number grow every time signpost ran, which is a tool measuring itself.
	docs := 0
	for _, f := range d.Files {
		if f.Class == discover.ClassDoc && d.Analyses(f) && !inBundle(f.Path) {
			docs++
		}
	}
	if docs > 0 {
		out = append(out, Finding{
			Topic: TopicDocumentation, Declared: true,
			Text: plural(docs, "documentation file") + " in the tree, outside the bundle.",
		})
	}
	return out
}

// --- observability ---

// observabilityMarkers are import-path fragments of libraries whose presence means the
// code can emit telemetry.
//
// Presence of a *library*, which is the honest limit of what a manifest can tell us. A
// dependency on an OpenTelemetry SDK does not prove anything is instrumented, and this
// package says so in the finding rather than overclaiming.
var observabilityMarkers = []string{
	"opentelemetry", "otel", "prometheus", "datadog", "dd-trace",
	"sentry", "jaeger", "zipkin", "statsd", "newrelic", "new-relic",
}

func observabilityFindings(facts []manifest.Facts) []Finding {
	if noManifests(facts) {
		return []Finding{manifestsNotRead(TopicObservability, "observability libraries")}
	}
	var srcs []Source
	seen := map[string]bool{}
	var names []string
	for _, f := range facts {
		for _, dep := range f.Deps {
			lower := strings.ToLower(dep.Name)
			for _, m := range observabilityMarkers {
				if !strings.Contains(lower, m) {
					continue
				}
				if !seen[dep.Name] {
					seen[dep.Name] = true
					names = append(names, dep.Name)
				}
				srcs = append(srcs, Source{Path: f.Path, Line: dep.Line})
				break
			}
		}
	}
	if len(srcs) > 0 {
		sort.Strings(names)
		return []Finding{{
			Topic:    TopicObservability,
			Declared: true,
			Text: "An observability library is a declared dependency: " + joinNames(names) +
				". Whether anything is instrumented with it is not something a manifest can say.",
			Sources: dedupeSources(srcs),
		}}
	}
	return []Finding{{
		Topic: TopicObservability,
		Text: "No observability library is a declared dependency, so a failure in " +
			"production is diagnosed from whatever the code happens to log.",
		Looked: []string{"declared dependencies of every manifest read"},
	}}
}

// --- agent rules ---

func agentRuleFindings(facts []manifest.Facts, d *discover.Result) []Finding {
	var out []Finding

	var srcs []Source
	rules := 0
	for _, f := range facts {
		if f.Kind != manifest.KindAgentRules {
			continue
		}
		rules += len(f.Rules)
		srcs = append(srcs, Source{Path: f.Path})
	}
	if len(srcs) > 0 {
		out = append(out, Finding{
			Topic:    TopicAgentRules,
			Declared: true,
			Text:     plural(rules, "stated rule") + " for agents working in this repository.",
			Sources:  dedupeSources(srcs),
		})
	} else {
		out = append(out, Finding{
			Topic: TopicAgentRules,
			Text: "No agent instructions were found, so an agent working here has only the " +
				"code to go on.",
			Looked: []string{"AGENTS.md", "CLAUDE.md", ".cursorrules", ".github/copilot-instructions.md"},
		})
	}

	adrs := 0
	var adrSrcs []Source
	for _, f := range facts {
		if f.Kind == manifest.KindADR {
			adrs++
			adrSrcs = append(adrSrcs, Source{Path: f.Path})
		}
	}
	if adrs > 0 {
		out = append(out, Finding{
			Topic:    TopicAgentRules,
			Declared: true,
			Text: plural(adrs, "architecture decision record") + " state why things are " +
				"the way they are.",
			Sources: dedupeSources(adrSrcs),
		})
	}
	return out
}

// --- shared ---

// noManifests distinguishes "the manifest pass did not run" from "it ran and found
// nothing". The two look identical in the facts and mean opposite things, and reporting
// the first as an absence would be §4.2's failure exactly: presenting an unmeasured thing
// as a measured one.
func noManifests(facts []manifest.Facts) bool { return len(facts) == 0 }

func manifestsNotRead(t Topic, what string) Finding {
	return Finding{
		Topic: t,
		Text: "No manifests were read, so nothing here says anything about " + what +
			" either way.",
		Looked: []string{"every non-source file the walk classified as a manifest"},
	}
}

// inBundle reports whether a path is one of signpost's own pages.
func inBundle(p string) bool {
	return p == ".signpost" || strings.HasPrefix(p, ".signpost/")
}

// filesNamed returns the discovered files whose path matches any of names, compared
// case-insensitively on the whole repo-relative path.
//
// Case-insensitive because `LICENSE`, `License` and `license` are the same declaration,
// and a findings page that reported a missing licence on a repository that has
// `License.md` would be wrong in the direction that costs trust.
func filesNamed(d *discover.Result, names []string) []Source {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[strings.ToLower(n)] = true
	}
	var out []Source
	for _, f := range d.Files {
		if want[strings.ToLower(f.Path)] {
			out = append(out, Source{Path: f.Path})
		}
	}
	return dedupeSources(out)
}

// dedupeSources sorts and deduplicates, keeping the lowest line per path.
//
// Sorted so the page is byte-stable: facts are gathered in file order, which is stable
// today, but a page whose bytes depend on that is a page that churns the day it changes.
func dedupeSources(in []Source) []Source {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool {
		if in[i].Path != in[j].Path {
			return in[i].Path < in[j].Path
		}
		return in[i].Line < in[j].Line
	})
	out := in[:0:0]
	for _, s := range in {
		if len(out) > 0 && out[len(out)-1].Path == s.Path {
			continue
		}
		out = append(out, s)
	}
	return out
}

func pathsOf(srcs []Source) []string {
	out := make([]string, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, s.Path)
	}
	sort.Strings(out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// joinNames renders a deduplicated, sorted, bounded name list.
//
// Bounded because a Makefile with forty test targets would otherwise put forty names in
// one sentence, and the finding is that test commands exist rather than what each is
// called. The overflow is stated rather than silent: a truncated list that reads as
// complete is the failure mode this project keeps finding in its own output.
func joinNames(names []string) string {
	const max = 6
	seen := map[string]bool{}
	uniq := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		uniq = append(uniq, n)
	}
	sort.Strings(uniq)
	if len(uniq) == 0 {
		return "unnamed"
	}
	shown := uniq
	extra := 0
	if len(shown) > max {
		extra = len(shown) - max
		shown = shown[:max]
	}
	quoted := make([]string, 0, len(shown))
	for _, n := range shown {
		quoted = append(quoted, "`"+n+"`")
	}
	s := strings.Join(quoted, ", ")
	if extra > 0 {
		s += ", and " + plural(extra, "other")
	}
	return s
}

func plural(n int, unit string) string {
	s := itoa(n) + " " + unit
	if n != 1 {
		s += "s"
	}
	return s
}

// itoa avoids importing strconv for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
