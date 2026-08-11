package vcs

import (
	"strings"
	"testing"
)

// The positive and negative boundaries of conventionalType, in one table.
//
// The negatives are the half that matters. A classifier that answered "yes" to everything
// would pass any set of positive-only cases and would report every repository as following
// Conventional Commits — so each negative here is a subject that looks close enough to be
// accepted by a looser parser, and the assertion is that it is refused.
func TestConventionalType(t *testing.T) {
	cases := []struct {
		subject string
		typ     string
		ok      bool
	}{
		// The shape, in each of its four legal forms.
		{"feat: add the thing", "feat", true},
		{"fix(parse): drop the guard", "fix", true},
		{"feat!: change the flag", "feat", true},
		{"refactor(vcs)!: reorder the fields", "refactor", true},
		// Case is not part of the shape; the type is reported lowercased so the caller's
		// switch does not have to know that.
		{"FIX: something", "fix", true},
		{"Fix(api): something", "fix", true},
		// Any type, not a fixed vocabulary. A project's own type is still a declaration that
		// the convention is in use, and enumerating types would make adoption depend on
		// signpost's list rather than on the repository.
		{"chore: bump", "chore", true},
		{"perf(db): index it", "perf", true},
		{"deps: bump x", "deps", true},

		// --- negatives ---
		// No colon at all: the overwhelming majority of ordinary subjects.
		{"add the thing", "", false},
		{"Merge branch 'main' into topic", "", false},
		{"Revert \"feat: add the thing\"", "", false},
		// A colon with no space after it. The spec requires the space, and this is the rule
		// that keeps a URL and a bare word-plus-colon from being read as types.
		{"feat:add the thing", "", false},
		{"see http://example.invalid/x", "", false},
		{"TODO:fix later", "", false},
		// A separator that is not a colon.
		{"feat - add the thing", "", false},
		{"feat; add the thing", "", false},
		// A type that is not letters. Digits, spaces, and punctuation before the colon are
		// not a type, so a timestamp or a path does not become one.
		{"123: numbered", "", false},
		{"v2: the rewrite", "", false},
		{"internal/vcs: reorder", "", false},
		{"two words: something", "", false},
		{"feat 1: something", "", false},
		// An unclosed scope, which is a typo rather than a convention.
		{"feat(parse: drop the guard", "", false},
		// A colon inside the scope but none after it.
		{"feat(a:b) something", "", false},
		// Nothing after the shape. A subject that is only a type says nothing about the
		// change, and the spec requires a description.
		{"feat", "", false},
		{"feat:", "", false},
		{"feat()", "", false},
		{"feat!", "", false},
		{"", "", false},
		{":", "", false},
		{": no type", "", false},
		// Past the type-length bound. A subject whose first colon is far out is not
		// conventional, and this is where countConventions stops scanning.
		{strings.Repeat("a", maxTypeLen+1) + ": long", "", false},
	}
	for _, c := range cases {
		typ, ok := conventionalType(c.subject)
		if ok != c.ok || typ != c.typ {
			t.Errorf("conventionalType(%q) = (%q, %v), want (%q, %v)",
				c.subject, typ, ok, c.typ, c.ok)
		}
	}
}

// The boundary exactly at maxTypeLen, separately from the table so that a change to the
// constant cannot silently make the test above vacuous.
func TestConventionalTypeAtTheLengthBound(t *testing.T) {
	typ := strings.Repeat("a", maxTypeLen)
	if got, ok := conventionalType(typ + ": x"); !ok || got != typ {
		t.Errorf("a type of exactly maxTypeLen was refused: (%q, %v)", got, ok)
	}
	if _, ok := conventionalType(strings.Repeat("a", maxTypeLen+1) + ": x"); ok {
		t.Error("a type one byte past maxTypeLen was accepted")
	}
}

func TestHasIssueRef(t *testing.T) {
	yes := []string{
		"fix: correct the count (#19)",
		"closes #1",
		"#42 first",
		"fix #7: the thing",
		"see org/repo#1234",
	}
	no := []string{
		// A hash with no digit after it. Each of these appears in real subjects and none is
		// a tracker reference.
		"fix: handle the # character",
		"docs: add a ## heading",
		"fix: guard #include order",
		"fix: the C# extractor",
		"#",
		"trailing #",
		"",
		"no reference at all",
	}
	for _, s := range yes {
		if !hasIssueRef(s) {
			t.Errorf("hasIssueRef(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if hasIssueRef(s) {
			t.Errorf("hasIssueRef(%q) = true, want false", s)
		}
	}
}

// countConventions over a walk that mixes both kinds of subject, asserting every counter
// including the ones that must stay at zero.
//
// The zeroes are the assertion that separates this from a test that would pass against a
// classifier counting every commit as everything.
func TestCountConventions(t *testing.T) {
	commits := []commit{
		{subject: "feat: one"},
		{subject: "feat(scope)!: two"},
		{subject: "fix: three (#19)"},
		{subject: "fix(parse): four"},
		{subject: "revert: five"},
		{subject: "chore: six"},
		{subject: "ordinary prose subject"},
		{subject: "another one, closes #7"},
		{subject: `Revert "feat: one"`},
		// Whitespace only, and empty: neither is a subject, so neither is counted at all.
		// Including them in the denominator would report a repository as less disciplined
		// than it is for a parsing reason.
		{subject: "   "},
		{subject: ""},
	}
	got := countConventions(commits)
	want := Conventions{Subjects: 9, Conventional: 6, Fixes: 2, Features: 2, Reverts: 2, IssueRefs: 2}
	if got != want {
		t.Errorf("countConventions() = %+v, want %+v", got, want)
	}
	if !got.Available() {
		t.Error("Available() = false with nine subjects counted")
	}
}

// A walk of only unconventional subjects. The counters must be zero and Available must still
// be true: "this repository does not use conventional commits" is a finding, and it is a
// different claim from "no subjects were read".
func TestCountConventionsAllUnconventional(t *testing.T) {
	got := countConventions([]commit{
		{subject: "add the thing"},
		{subject: "Merge pull request #3 from org/topic"},
		{subject: "wip"},
	})
	if got.Conventional != 0 || got.Fixes != 0 || got.Features != 0 || got.Reverts != 0 {
		t.Errorf("unconventional subjects were classified: %+v", got)
	}
	if got.Subjects != 3 {
		t.Errorf("Subjects = %d, want 3", got.Subjects)
	}
	// The merge subject names a pull request, which is a real traceability reference even
	// though the subject follows no convention. The two counters are independent.
	if got.IssueRefs != 1 {
		t.Errorf("IssueRefs = %d, want 1", got.IssueRefs)
	}
	if !got.Available() {
		t.Error("Available() = false with three subjects counted")
	}
}

// An empty walk, and a walk of nothing but empty subjects. Both must report unavailable
// rather than zero-of-zero, which is what stops the consumer dividing by it.
func TestCountConventionsNoSubjects(t *testing.T) {
	for name, commits := range map[string][]commit{
		"empty walk":     nil,
		"empty subjects": {{subject: ""}, {subject: ""}},
		"blank subjects": {{subject: " "}, {subject: "\t"}},
		"one blank":      {{subject: "\n"}},
	} {
		got := countConventions(commits)
		if got.Available() {
			t.Errorf("%s: Available() = true with %+v", name, got)
		}
		if got.Subjects != 0 {
			t.Errorf("%s: Subjects = %d, want 0", name, got.Subjects)
		}
	}
}
