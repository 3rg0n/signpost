package practice

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
	"github.com/3rg0n/signpost/internal/manifest"
)

// What these tests are for, and what deliberately lives elsewhere.
//
// The corpus test in cmd/signpost asserts that a whole repository's findings reach the page,
// and it is the better test of the page's content because it runs the real pipeline. What it
// cannot do is construct the inputs that are awkward to arrange in a repository: no manifests
// read at all, a lockfile with no manifest, a manifest with no lockfile, an unrecognised
// lockfile basename. Those are the cases below.
//
// The recurring theme is that this package's failures are *quiet*. It cannot error — every gap
// is a finding — so a bug does not surface as a crash or an empty page. It surfaces as a
// sentence that is confidently wrong about the repository, which reads exactly like a sentence
// that is right. The lockfile pairing bug shipped in that shape: every ecosystem reported as
// unpinned in a repository that commits four lockfiles.

// facts builds a RunResult from Facts, which is all this package reads.
func facts(fs ...manifest.Facts) *manifest.RunResult {
	return &manifest.RunResult{Facts: fs}
}

// walk builds a discovery result from paths, classifying nothing. Most findings key on
// manifest Facts; the ones that key on the walk look up exact filenames.
func walk(paths ...string) *discover.Result {
	out := &discover.Result{Root: "/repo"}
	for _, p := range paths {
		out.Files = append(out.Files, discover.File{Path: p})
	}
	return out
}

// findingFor returns the findings on a topic whose text contains want.
func findingsMatching(r *Result, topic Topic, want string) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Topic == topic && strings.Contains(f.Text, want) {
			out = append(out, f)
		}
	}
	return out
}

// TestAnalyseWithoutADiscoveryResultFindsNothing is the distinction between "missing" and
// "not looked for".
//
// A nil walk yields no findings rather than a page of absences, and the difference matters
// more than it looks: a run that established nothing has not established that anything is
// missing. Rendering that as "No build command is declared" would be signpost asserting a
// fact about a repository it did not open.
func TestAnalyseWithoutADiscoveryResultFindsNothing(t *testing.T) {
	res := Analyse(Input{Manifests: facts(manifest.Facts{Path: "go.mod", Kind: manifest.KindGoMod})})
	if len(res.Findings) != 0 {
		t.Errorf("a run with no walk produced %d finding(s); it has not established that "+
			"anything is absent:\n%s", len(res.Findings), res.Render())
	}
	if got := res.Render(); got != "" {
		t.Errorf("Render on an empty result = %q, want empty — okf.renderAll uses that to "+
			"skip the page, and a heading with nothing under it claims measurement that did "+
			"not happen", got)
	}
}

// TestUnreadManifestsAreNotReportedAsAbsences is the same distinction one level down.
//
// A walk that found files but a manifest pass that read none must not report "no CI gates" or
// "no dependencies" — those are claims about a repository, and the truth is that signpost did
// not look. The page says so per topic instead.
func TestUnreadManifestsAreNotReportedAsAbsences(t *testing.T) {
	res := Analyse(Input{Discovered: walk("main.go"), Manifests: nil})

	for _, topic := range []Topic{TopicGates, TopicDependencies, TopicObservability} {
		found := false
		for _, f := range res.Findings {
			if f.Topic != topic {
				continue
			}
			found = true
			if !strings.Contains(f.Text, "No manifests were read") {
				t.Errorf("%s: with no manifests read, the finding is %q — it states a fact "+
					"about the repository when signpost did not look", topic, f.Text)
			}
		}
		if !found {
			t.Errorf("%s: no finding at all. Silence reads as nothing to report, which is the "+
				"failure this page exists to prevent", topic)
		}
	}
}

// TestLockfilesPairWithManifestsByBasename is the regression for the pairing bug.
//
// It reported every ecosystem as unpinned in a repository committing four lockfiles, plus "a
// unknown lockfile is present with no manifest beside it". The cause was pairing on the
// ecosystem field of the lockfile's Facts, which internal/manifest never sets — it does not
// parse lockfiles at all, deliberately. So the bug was not a wrong mapping; it was pairing on
// a field that is always empty, and every pair failed.
//
// The assertion is on both halves: pinned where a lockfile exists, and *not* pinned where one
// does not. A fix that reported everything as pinned would satisfy the first alone.
func TestLockfilesPairWithManifestsByBasename(t *testing.T) {
	res := Analyse(Input{
		Discovered: walk("go.mod", "go.sum", "package.json", "pnpm-lock.yaml",
			"Cargo.toml", "Cargo.lock", "pyproject.toml"),
		Manifests: facts(
			manifest.Facts{Path: "go.mod", Kind: manifest.KindGoMod},
			manifest.Facts{Path: "go.sum", Kind: manifest.KindLock},
			manifest.Facts{Path: "package.json", Kind: manifest.KindPackageJSON},
			manifest.Facts{Path: "pnpm-lock.yaml", Kind: manifest.KindLock},
			manifest.Facts{Path: "Cargo.toml", Kind: manifest.KindCargo},
			manifest.Facts{Path: "Cargo.lock", Kind: manifest.KindLock},
			// Python deliberately unpinned, so the negative case is exercised by the same
			// input rather than by a second repository shaped to fail.
			manifest.Facts{Path: "pyproject.toml", Kind: manifest.KindPyProject},
		),
	})

	for _, eco := range []string{"Go", "npm", "Cargo"} {
		want := "The " + eco + " dependencies are pinned by a lockfile."
		got := findingsMatching(res, TopicDependencies, want)
		if len(got) != 1 {
			t.Errorf("%d finding(s) say %q, want 1. A lockfile is committed for %s:\n%s",
				len(got), want, eco, res.Render())
			continue
		}
		if !got[0].Declared {
			t.Errorf("%s: the pinned finding is marked as an absence", eco)
		}
	}

	if got := findingsMatching(res, TopicDependencies, "Python dependencies are declared but not pinned"); len(got) != 1 {
		t.Errorf("%d finding(s) report Python as unpinned, want 1 — no Python lockfile is in "+
			"this input:\n%s", len(got), res.Render())
	}
	if got := findingsMatching(res, TopicDependencies, "lockfile is present with no manifest"); len(got) != 0 {
		t.Errorf("%d orphan-lockfile finding(s), want 0. Every lockfile here has its manifest "+
			"beside it:\n%s", len(got), res.Render())
	}
}

// TestAnUnrecognisedLockfileIsNotReportedAsAnOrphan pins the skip in dependencyFindings.
//
// A lockfile whose basename lockEcosystem does not know is dropped rather than bucketed under
// an empty ecosystem, because an empty bucket pairs with no manifest and so gets reported as
// "a lockfile is present with no manifest beside it" — a finding about the repository when the
// truth is a gap in signpost's list. That is the sentence the pairing bug printed, and it was
// wrong in exactly this way.
func TestAnUnrecognisedLockfileIsNotReportedAsAnOrphan(t *testing.T) {
	res := Analyse(Input{
		Discovered: walk("go.mod", "go.sum", "gradle.lockfile"),
		Manifests: facts(
			manifest.Facts{Path: "go.mod", Kind: manifest.KindGoMod},
			manifest.Facts{Path: "go.sum", Kind: manifest.KindLock},
			manifest.Facts{Path: "gradle.lockfile", Kind: manifest.KindLock},
		),
	})
	for _, f := range res.Findings {
		if strings.Contains(f.Text, "with no manifest beside it") {
			t.Errorf("an unrecognised lockfile was reported as an orphan: %q. It is a gap in "+
				"lockEcosystem, not a fact about this repository", f.Text)
		}
		if strings.Contains(f.Text, "The  dependencies") {
			t.Errorf("an empty ecosystem name reached the page: %q", f.Text)
		}
	}
}

// TestLockEcosystemNamesMatchManifestEcosystem is the one assertion that catches the pairing
// bug's whole *class* rather than its instance.
//
// The two functions' return values are the pairing key. A mismatch does not fail loudly: it
// reports go.mod as unpinned in a repository that commits go.sum, which is a false accusation
// on the page. So every name lockEcosystem can return must be one manifestEcosystem can also
// return, checked directly rather than through a repository that happens to exercise it.
func TestLockEcosystemNamesMatchManifestEcosystem(t *testing.T) {
	fromManifests := map[string]bool{}
	for _, k := range []manifest.Kind{
		manifest.KindGoMod, manifest.KindPackageJSON, manifest.KindPyProject,
		manifest.KindRequirement, manifest.KindCargo,
	} {
		if eco := manifestEcosystem(manifest.Facts{Kind: k}); eco != "" {
			fromManifests[eco] = true
		}
	}

	for _, lock := range []string{
		"go.sum",
		"package-lock.json", "pnpm-lock.yaml", "yarn.lock", "npm-shrinkwrap.json",
		"uv.lock", "poetry.lock", "Pipfile.lock", "pdm.lock",
		"Cargo.lock",
	} {
		eco := lockEcosystem(lock)
		if eco == "" {
			t.Errorf("lockEcosystem(%q) = \"\", but it is listed in that function's own switch",
				lock)
			continue
		}
		if !fromManifests[eco] {
			t.Errorf("lockEcosystem(%q) = %q, which manifestEcosystem never returns. These two "+
				"are the pairing key, so this reports the manifest as unpinned in a repository "+
				"that commits the lockfile. Known manifest names: %v",
				lock, eco, sortedKeys(fromManifests))
		}
	}

	// The path is a basename match, so a lockfile in a subdirectory pairs too. A repository
	// with a nested npm package is the common case and prefix-matching the whole path would
	// miss it.
	if got := lockEcosystem("web/frontend/pnpm-lock.yaml"); got != "npm" {
		t.Errorf("lockEcosystem on a nested path = %q, want npm", got)
	}
	if got := lockEcosystem("go.sum.bak"); got != "" {
		t.Errorf("lockEcosystem(%q) = %q, want empty", "go.sum.bak", got)
	}
}

// TestGatesDistinguishBlockingJobsFromTheRest checks both branches of the gate finding.
//
// Gating is a property of the workflow's triggers, not of the job: GitHub's required-checks
// operates on job names and any job in a pull_request workflow can be selected, so a workflow
// on pull_request makes all of its jobs gates. Only a schedule-only workflow runs outside. The
// page reports the ungated ones as a fact rather than a gap — a nightly scan is correctly
// ungated, and calling that a finding would be the rubric this package refuses to be.
func TestGatesDistinguishBlockingJobsFromTheRest(t *testing.T) {
	res := Analyse(Input{
		Discovered: walk(".github/workflows/ci.yml", ".github/workflows/nightly.yml"),
		Manifests: facts(
			manifest.Facts{Path: ".github/workflows/ci.yml", Kind: manifest.KindWorkflow, Jobs: []manifest.Job{
				{Name: "test", Gate: true, Line: 4},
				{Name: "lint", Gate: true, Line: 20},
			}},
			manifest.Facts{Path: ".github/workflows/nightly.yml", Kind: manifest.KindWorkflow, Jobs: []manifest.Job{
				{Name: "scan", Gate: false, Line: 8},
			}},
		),
	})

	blocking := findingsMatching(res, TopicGates, "can block a merge")
	if len(blocking) != 1 {
		t.Fatalf("%d finding(s) name the blocking jobs, want 1:\n%s", len(blocking), res.Render())
	}
	for _, name := range []string{"test", "lint"} {
		if !strings.Contains(blocking[0].Text, name) {
			t.Errorf("the gate finding does not name the %s job: %q", name, blocking[0].Text)
		}
	}
	if strings.Contains(blocking[0].Text, "scan") {
		t.Errorf("the gate finding names the schedule-only job as blocking: %q", blocking[0].Text)
	}

	outside := findingsMatching(res, TopicGates, "outside that gate")
	if len(outside) != 1 {
		t.Fatalf("%d finding(s) report the ungated job, want 1:\n%s", len(outside), res.Render())
	}
	if !outside[0].Declared {
		t.Error("a job running outside the gate is marked as an absence — it is a fact about " +
			"the repository, not a gap in it")
	}
}

// TestWorkflowsThatCannotBlockAMergeAreReported covers the branch between "no CI at all" and
// "CI that gates".
//
// A repository with workflows none of which run on a pull request or a default-branch push
// has automation that cannot stop a bad change, and that is a different fact from having no
// automation. Reporting it as "no CI workflows were found" would be false.
func TestWorkflowsThatCannotBlockAMergeAreReported(t *testing.T) {
	res := Analyse(Input{
		Discovered: walk(".github/workflows/release.yml"),
		Manifests: facts(manifest.Facts{
			Path: ".github/workflows/release.yml", Kind: manifest.KindWorkflow,
			Jobs: []manifest.Job{{Name: "publish", Gate: false, Line: 6}},
		}),
	})

	if got := findingsMatching(res, TopicGates, "no job runs on a pull request"); len(got) != 1 {
		t.Errorf("%d finding(s) report workflows that cannot gate, want 1:\n%s",
			len(got), res.Render())
	}
	if got := findingsMatching(res, TopicGates, "No CI workflows were found"); len(got) != 0 {
		t.Errorf("a repository with a workflow was reported as having none:\n%s", res.Render())
	}
}

// TestEveryFindingCarriesItsGrounding is the invariant behind the whole page.
//
// A declared finding names where it is stated; an absence names where signpost looked. Without
// the second, a reader cannot tell a repository that does not declare a test command from one
// whose build system signpost does not read — and those call for opposite actions. This is
// checked structurally over every finding a real-ish input produces, because it is the
// property most likely to be dropped by someone adding a topic and forgetting one field.
func TestEveryFindingCarriesItsGrounding(t *testing.T) {
	res := Analyse(Input{
		Discovered: walk("go.mod", "go.sum", "Makefile", "README.md", "LICENSE",
			"CODEOWNERS", "AGENTS.md", ".github/dependabot.yml",
			".github/workflows/ci.yml", "main.go", "main_test.go"),
		Manifests: facts(
			manifest.Facts{Path: "go.mod", Kind: manifest.KindGoMod, Deps: []manifest.Dep{
				{Name: "go.opentelemetry.io/otel", Line: 5},
			}},
			manifest.Facts{Path: "go.sum", Kind: manifest.KindLock},
			manifest.Facts{Path: "Makefile", Kind: manifest.KindMakefile, Scripts: []manifest.Script{
				{Name: "build", Command: "go build ./...", Line: 2},
				{Name: "test", Command: "go test ./...", Line: 5},
			}},
			manifest.Facts{Path: ".github/workflows/ci.yml", Kind: manifest.KindWorkflow,
				Jobs: []manifest.Job{{Name: "test", Gate: true, Line: 4}}},
		),
	})
	if len(res.Findings) == 0 {
		t.Fatal("no findings from an input with a manifest, a lockfile, and a workflow")
	}

	for _, f := range res.Findings {
		if f.Text == "" {
			t.Errorf("%s: a finding with no text", f.Topic)
		}
		if f.Declared {
			if len(f.Sources) == 0 {
				t.Errorf("%s: %q is declared but cites nothing. An unsourced claim on this page "+
					"is indistinguishable from a guess", f.Topic, f.Text)
			}
			for _, s := range f.Sources {
				if s.Path == "" {
					t.Errorf("%s: %q cites a source with an empty path", f.Topic, f.Text)
				}
			}
			if len(f.Looked) > 0 {
				t.Errorf("%s: %q is declared and also names where signpost looked; the two are "+
					"alternatives", f.Topic, f.Text)
			}
			continue
		}
		if len(f.Looked) == 0 {
			t.Errorf("%s: %q is an absence that does not say where signpost looked, so a "+
				"reader cannot tell a missing declaration from an unread build system",
				f.Topic, f.Text)
		}
		if len(f.Sources) > 0 {
			t.Errorf("%s: %q is an absence with sources attached", f.Topic, f.Text)
		}
	}

	if res.Declared() == 0 {
		t.Error("nothing was declared by a repository with a Makefile, a lockfile, and a gating " +
			"workflow")
	}
	if res.Declared()+res.Absent() != len(res.Findings) {
		t.Errorf("Declared()=%d + Absent()=%d != %d findings",
			res.Declared(), res.Absent(), len(res.Findings))
	}
}

// TestFindingsAreInTopicOrder pins the page's section order and, with it, byte-stability.
//
// Analyse buckets by topic and then emits in topicOrder, so findings from one topic can never
// be split by another's. A page whose sections interleaved differently between runs would
// churn the committed bundle on every build (design §8.1).
func TestFindingsAreInTopicOrder(t *testing.T) {
	res := Analyse(Input{
		Discovered: walk("go.mod", "Makefile", "README.md", "LICENSE"),
		Manifests: facts(
			manifest.Facts{Path: "go.mod", Kind: manifest.KindGoMod},
			manifest.Facts{Path: "Makefile", Kind: manifest.KindMakefile,
				Scripts: []manifest.Script{{Name: "build", Line: 1}}},
		),
	})

	rank := map[Topic]int{}
	for i, t := range topicOrder {
		rank[t] = i
	}
	last := -1
	seen := map[Topic]bool{}
	for _, f := range res.Findings {
		r, ok := rank[f.Topic]
		if !ok {
			t.Errorf("a finding carries the topic %q, which is not in topicOrder — Analyse "+
				"drops it from the page entirely", f.Topic)
			continue
		}
		if r < last {
			t.Errorf("%s appears after a later topic; the sections are interleaved", f.Topic)
		}
		if r != last && seen[f.Topic] {
			t.Errorf("%s appears in two separate runs, so its section is split", f.Topic)
		}
		seen[f.Topic] = true
		last = r
	}
}

// TestRenderStatesAbsencesInWords, and never as a score.
//
// The marker on an absence is the phrase "Not declared", not a symbol: an agent reads this
// page as text, and "not declared" survives being summarised where a bare ✗ does not. And the
// rendered page carries no rubric vocabulary — design §9.1 is explicit that a level reads as
// measured when it has only been judged. Both are properties of the output rather than of the
// findings, so they are checked here rather than on Result.
func TestRenderStatesAbsencesInWords(t *testing.T) {
	res := Analyse(Input{Discovered: walk("main.go")})
	page := res.Render()
	if page == "" {
		t.Fatal("a walk that found a file rendered nothing")
	}
	if !strings.Contains(page, "**Not declared.**") {
		t.Errorf("a page of absences does not carry the words:\n%s", page)
	}
	if !strings.Contains(page, "Looked in") {
		t.Errorf("a page of absences does not say where signpost looked:\n%s", page)
	}

	lower := strings.ToLower(page)
	for _, unwanted := range []string{"maturity", "score", "grade", "rubric", "out of 5",
		"level 1", "level 2"} {
		if strings.Contains(lower, unwanted) {
			t.Errorf("the rendered page states %q — this page emits findings, never a score "+
				"(design §9.1)", unwanted)
		}
	}
}

// TestRenderBoundsACitationListOutLoud checks the overflow is counted rather than hidden.
//
// A list that silently stopped at the cap would read as complete, which is the same class of
// failure as an unreported skip: the reader has no way to know the page is not telling them
// everything.
func TestRenderBoundsACitationListOutLoud(t *testing.T) {
	var srcs []Source
	for i := 0; i < maxRenderedSources+3; i++ {
		srcs = append(srcs, Source{Path: "pkg/" + string(rune('a'+i)) + "/go.mod"})
	}
	got := renderFinding(Finding{
		Topic: TopicDependencies, Declared: true, Text: "Declared.", Sources: srcs,
	})
	if !strings.Contains(got, "and 3 other files") {
		t.Errorf("a truncated citation list does not count what it dropped:\n%s", got)
	}
	if strings.Contains(got, "pkg/g/go.mod") {
		t.Errorf("the list rendered past its cap:\n%s", got)
	}
}

// TestRenderPathsAsCodeNotLinks pins the one thing this file must not do.
//
// A markdown link here would be a bundle-relative link to a repository path, and okf.Verify
// resolves every prose link in a page against the bundle — so a link to `go.mod` fails
// verification of a bundle that is otherwise correct. A code span also makes a path containing
// markdown characters read as itself.
func TestRenderPathsAsCodeNotLinks(t *testing.T) {
	got := renderFinding(Finding{
		Topic: TopicDependencies, Declared: true, Text: "Declared.",
		Sources: []Source{{Path: "app/tools/[slug]/package.json", Line: 3}},
	})
	if !strings.Contains(got, "`app/tools/[slug]/package.json` line 3") {
		t.Errorf("a cited path is not rendered as inline code with its line:\n%s", got)
	}
	if strings.Contains(got, "](") {
		t.Errorf("a citation rendered as a markdown link, which verify resolves against the "+
			"bundle and would fail on:\n%s", got)
	}
}
