package practice

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/vcs"
)

// history builds the Signals this package reads, with history available.
func history(c vcs.Conventions, r vcs.Releases) *vcs.Signals {
	return &vcs.Signals{Available: true, Conventions: c, Releases: r}
}

// historyTexts returns the text of every history finding, joined, so a test can assert on
// what the page says without depending on how many findings it took to say it.
func historyTexts(r *Result) string {
	var out []string
	for _, f := range r.Findings {
		if f.Topic == TopicHistory {
			out = append(out, f.Text)
		}
	}
	return strings.Join(out, "\n")
}

func historyFindingsOf(h *vcs.Signals) *Result {
	return Analyse(Input{Discovered: walk("main.go"), History: h})
}

// The distinction the whole nil-handling exists for, and the one place this topic departs from
// how every other topic here treats a missing input.
//
// -no-history and a tarball with no .git both arrive as nil. Neither is evidence that the
// repository does not tag releases or does not use conventional commits, so neither may produce
// an absence: that would be signpost asserting a fact about somebody's repository on the basis
// of a flag they passed.
func TestHistoryNotReadProducesNoFindings(t *testing.T) {
	for name, h := range map[string]*vcs.Signals{
		"nil":         nil,
		"unavailable": {Available: false, Reason: "git is not installed"},
	} {
		res := historyFindingsOf(h)
		for _, f := range res.Findings {
			if f.Topic == TopicHistory {
				t.Errorf("%s history produced a finding: %q", name, f.Text)
			}
		}
	}
}

// A repository that uses the convention says so, and says which types it used.
func TestConventionalCommitsAreDeclared(t *testing.T) {
	res := historyFindingsOf(history(vcs.Conventions{
		Subjects: 100, Conventional: 96, Fixes: 30, Features: 40, IssueRefs: 50,
	}, vcs.Releases{}))

	got := findingsMatching(res, TopicHistory, "Conventional Commits")
	if len(got) != 1 {
		t.Fatalf("got %d findings naming the convention, want 1: %q", len(got), historyTexts(res))
	}
	if !got[0].Declared {
		t.Error("a repository following the convention was reported as an absence")
	}
	// The counts are stated, because "follows Conventional Commits" without them is a claim
	// a reader cannot check against the log.
	for _, want := range []string{"96", "100", "30", "40"} {
		if !strings.Contains(got[0].Text, want) {
			t.Errorf("text does not state %s: %q", want, got[0].Text)
		}
	}
	// The claim comes from a set of commits rather than from a file, so citing one would
	// attribute it to a file that does not make it.
	if len(got[0].Sources) != 0 {
		t.Errorf("a claim about commit messages cited files: %+v", got[0].Sources)
	}
}

// The negative boundary. A repository that does not use the convention must be reported as not
// using it, and the rate must still appear: a bare "follows no convention" reads as though
// signpost found nothing to say.
func TestUnconventionalCommitsAreReportedWithTheirRate(t *testing.T) {
	res := historyFindingsOf(history(vcs.Conventions{
		Subjects: 715, Conventional: 340, Fixes: 100, Features: 200,
	}, vcs.Releases{}))

	got := findingsMatching(res, TopicHistory, "no machine-readable convention")
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %q", len(got), historyTexts(res))
	}
	if got[0].Declared {
		t.Error("a repository following no convention was reported as a declaration")
	}
	if !strings.Contains(got[0].Text, "340") || !strings.Contains(got[0].Text, "715") {
		t.Errorf("the rate is not stated: %q", got[0].Text)
	}
	// And it must not also claim the opposite.
	if n := len(findingsMatching(res, TopicHistory, "follow Conventional Commits")); n != 0 {
		t.Errorf("both readings were reported at once: %q", historyTexts(res))
	}
}

// The threshold, from both sides of it. Adoption measured across seven repositories was
// bimodal — 100, 99, 96, 83, 11, 0, 0 — so the cut separates two real groups rather than
// grading a continuum, and a repository at 11% is one that does not use the convention with a
// handful of commits that happen to parse.
func TestConventionThresholdFromBothSides(t *testing.T) {
	cases := []struct {
		name       string
		subjects   int
		convention int
		declared   bool
	}{
		{"every commit", 100, 100, true},
		{"almost every commit", 235, 234, true},
		{"most commits", 168, 154, true},
		{"exactly the threshold", 3, 2, true},
		{"just under the threshold", 100, 66, false},
		{"about half", 715, 340, false},
		{"a handful that parse", 34, 4, false},
		{"none at all", 116, 0, false},
	}
	for _, c := range cases {
		res := historyFindingsOf(history(vcs.Conventions{
			Subjects: c.subjects, Conventional: c.convention,
		}, vcs.Releases{}))
		declared := len(findingsMatching(res, TopicHistory, "Conventional Commits:")) == 1
		absent := len(findingsMatching(res, TopicHistory, "no machine-readable convention")) == 1
		if declared != c.declared || absent == c.declared {
			t.Errorf("%s (%d/%d): declared=%v absent=%v, want declared=%v",
				c.name, c.convention, c.subjects, declared, absent, c.declared)
		}
	}
}

// No subjects read at all — an empty repository, or a walk that produced nothing. Zero of zero
// is not a rate, and reporting it either way would be inventing a finding.
func TestNoSubjectsProducesNoConventionFinding(t *testing.T) {
	res := historyFindingsOf(history(vcs.Conventions{}, vcs.Releases{}))
	if got := historyTexts(res); got != "" {
		t.Errorf("a walk with no subjects produced %q", got)
	}
}

// Reverts are worth their own sentence, and only when there are some. A repository with none
// gets no finding: "no commit reverts another" is not a fact anybody needs.
func TestRevertsAreReportedOnlyWhenPresent(t *testing.T) {
	with := historyFindingsOf(history(vcs.Conventions{
		Subjects: 100, Conventional: 100, Reverts: 4,
	}, vcs.Releases{}))
	if n := len(findingsMatching(with, TopicHistory, "revert an earlier one")); n != 1 {
		t.Errorf("got %d revert findings, want 1: %q", n, historyTexts(with))
	}
	without := historyFindingsOf(history(vcs.Conventions{
		Subjects: 100, Conventional: 100,
	}, vcs.Releases{}))
	if n := len(findingsMatching(without, TopicHistory, "revert")); n != 0 {
		t.Errorf("a repository with no reverts produced %q", historyTexts(without))
	}
}

func TestTagsAreDeclaredWithTheLatestAndTheDistance(t *testing.T) {
	res := historyFindingsOf(history(vcs.Conventions{}, vcs.Releases{
		Available: true, Count: 12, Latest: "v0.1.0", LatestDate: "2026-08-01", CommitsSince: 42,
	}))
	got := findingsMatching(res, TopicHistory, "v0.1.0")
	if len(got) != 1 {
		t.Fatalf("got %d findings naming the tag, want 1: %q", len(got), historyTexts(res))
	}
	if !got[0].Declared {
		t.Error("a tagged repository was reported as an absence")
	}
	for _, want := range []string{"12 tags", "2026-08-01", "42 commits"} {
		if !strings.Contains(got[0].Text, want) {
			t.Errorf("text does not state %q: %q", want, got[0].Text)
		}
	}
	// Rendered as inline code, because a tag name may contain a semicolon or a `$(...)` —
	// git's ref rules reject the characters that would break a markdown line, but not those.
	if !strings.Contains(got[0].Text, "`v0.1.0`") {
		t.Errorf("the tag name is not marked as code: %q", got[0].Text)
	}
}

// The tag is the described commit, so nothing is past it. "0 commits back" would be true and
// useless; "which is this commit" is what the reader wants to know.
func TestATagAtTheDescribedCommitSaysSo(t *testing.T) {
	res := historyFindingsOf(history(vcs.Conventions{}, vcs.Releases{
		Available: true, Count: 1, Latest: "v1.0.0", LatestDate: "2026-08-01",
	}))
	text := historyTexts(res)
	if !strings.Contains(text, "which is this commit") {
		t.Errorf("a tag at the described commit reads as %q", text)
	}
	if strings.Contains(text, "0 commits") {
		t.Errorf("the distance was stated as zero: %q", text)
	}
}

// The negative boundary for releases: an untagged repository is reported as untagged, which is
// a different finding from a tag list that could not be read.
func TestAnUntaggedRepositoryIsAnAbsence(t *testing.T) {
	res := historyFindingsOf(history(vcs.Conventions{}, vcs.Releases{Available: true}))
	got := findingsMatching(res, TopicHistory, "No tag is reachable")
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %q", len(got), historyTexts(res))
	}
	if got[0].Declared {
		t.Error("an untagged repository was reported as a declaration")
	}
	if len(got[0].Looked) == 0 {
		t.Error("an absence with nothing named as looked for")
	}
}

// The ambiguity §4.2 records. A shallow clone has no tags and neither does an untagged
// repository; the two must not read the same, and the finding must name the fix rather than
// leaving the reader with a warning they cannot act on.
func TestAShallowCloneIsUnknownRatherThanUntagged(t *testing.T) {
	res := historyFindingsOf(history(vcs.Conventions{}, vcs.Releases{
		Reason: "the clone is shallow, so whether anything is tagged could not be determined" +
			" (in CI, set fetch-depth: 0)",
	}))
	got := findingsMatching(res, TopicHistory, "is not known")
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %q", len(got), historyTexts(res))
	}
	if got[0].Declared {
		t.Error("an unknown answer was reported as a declaration")
	}
	if !strings.Contains(got[0].Text, "fetch-depth") {
		t.Errorf("the finding does not name the fix: %q", got[0].Text)
	}
	// Not the untagged wording, which would be a false claim about the repository.
	if strings.Contains(historyTexts(res), "No tag is reachable") {
		t.Errorf("a shallow clone was reported as untagged: %q", historyTexts(res))
	}
}

// Unavailable with no reason at all produces nothing. That combination means nobody asked, and
// a finding would be manufactured from an absence of input.
func TestReleasesUnavailableWithoutAReasonSaysNothing(t *testing.T) {
	res := historyFindingsOf(history(vcs.Conventions{}, vcs.Releases{}))
	if got := historyTexts(res); got != "" {
		t.Errorf("an unavailable read with no reason produced %q", got)
	}
}

// The topic has a heading, which is what puts its findings under one. A topic reaching the page
// without a case in topicHeading renders under the fallback and reads as a bug.
func TestHistoryTopicHasAHeading(t *testing.T) {
	res := historyFindingsOf(history(vcs.Conventions{Subjects: 10, Conventional: 10}, vcs.Releases{}))
	out := res.Render()
	if !strings.Contains(out, "### How changes are recorded") {
		t.Errorf("history findings rendered without their heading:\n%s", out)
	}
}
