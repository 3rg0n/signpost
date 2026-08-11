package vcs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// Tags read through the real git binary, which is the only way to test this: readReleases is
// two git invocations and a line split, so a test with a canned string would assert that this
// file's own idea of `for-each-ref` output matches itself.

func TestReadReportsTags(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "a/x.go", "package a\n")
	gitCommit(t, dir, "first")
	gitRun(t, dir, "tag", "v0.1.0")
	write(t, dir, "a/x.go", "package a\n\nfunc F() {}\n")
	gitCommit(t, dir, "second")
	// Annotated, which is the form a release usually takes and the one whose creatordate is
	// the tagging date rather than the commit's.
	gitRun(t, dir, "tag", "-a", "v0.2.0", "-m", "release 0.2.0")
	write(t, dir, "a/x.go", "package a\n\nfunc F() {}\nfunc G() {}\n")
	gitCommit(t, dir, "third")

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	r := s.Releases
	if !r.Available {
		t.Fatalf("tags not read: %q", r.Reason)
	}
	if r.Count != 2 {
		t.Errorf("Count = %d, want 2", r.Count)
	}
	if r.Latest != "v0.2.0" {
		t.Errorf("Latest = %q, want v0.2.0 — the newest tag, not the first found", r.Latest)
	}
	if r.LatestDate == "" {
		t.Error("LatestDate is empty")
	}
	// One commit past the tag, which is the number that says whether a released version
	// still describes the code in front of the reader.
	if r.CommitsSince != 1 {
		t.Errorf("CommitsSince = %d, want 1", r.CommitsSince)
	}
	if r.Reason != "" {
		t.Errorf("Reason = %q on a successful read", r.Reason)
	}
}

// Two tags with the same creation date, which is what every release cut in one session looks
// like — creatordate compares to the second and the test harness pins every date besides.
// git breaks an exact tie by refname *ascending*, so a date-only sort named `v0.1.0` as the
// latest release of a repository whose newest tag was `v0.2.0`. Version order breaks the tie.
func TestLatestTagAmongSameDayTags(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "a/x.go", "package a\n")
	gitCommit(t, dir, "first")
	gitRun(t, dir, "tag", "v0.1.0")
	gitRun(t, dir, "tag", "v0.2.0")
	// Two digits after one, which plain refname order would rank below `v0.9.0`.
	gitRun(t, dir, "tag", "v0.10.0")

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Releases.Count != 3 {
		t.Fatalf("Count = %d, want 3", s.Releases.Count)
	}
	if s.Releases.Latest != "v0.10.0" {
		t.Errorf("Latest = %q, want v0.10.0: same-day tags were not ordered by version",
			s.Releases.Latest)
	}
}

// The negative boundary that makes the positive one mean anything: a repository nobody has
// tagged reports zero tags and still reports the read as successful. A test that only checked
// the tagged case would pass against a function that invented a tag.
func TestReadReportsNoTags(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "a/x.go", "package a\n")
	gitCommit(t, dir, "first")

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	r := s.Releases
	if !r.Available {
		t.Fatalf("an untagged repository reported unavailable: %q", r.Reason)
	}
	if r.Count != 0 || r.Latest != "" || r.LatestDate != "" || r.CommitsSince != 0 {
		t.Errorf("an untagged repository produced %+v", r)
	}
}

// A tag on a branch the described commit cannot reach must not be counted. Without --merged
// this number would move whenever anybody tagged anything anywhere in the repository, which
// would make the bundle's release count a fact about the clone rather than about the commit.
func TestReadCountsOnlyReachableTags(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "a/x.go", "package a\n")
	gitCommit(t, dir, "first")
	gitRun(t, dir, "tag", "v0.1.0")

	gitRun(t, dir, "checkout", "-b", "side")
	write(t, dir, "side.go", "package a\n")
	gitCommit(t, dir, "side change")
	gitRun(t, dir, "tag", "side-only")
	gitRun(t, dir, "checkout", "main")

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Releases.Count != 1 {
		t.Errorf("Count = %d, want 1: an unreachable tag was counted", s.Releases.Count)
	}
	if s.Releases.Latest != "v0.1.0" {
		t.Errorf("Latest = %q, want v0.1.0", s.Releases.Latest)
	}
}

// Read as of a recorded commit, which is what -as-of-bundle does. The tags reported must be
// the ones that commit had, not the ones the branch has now — otherwise verifying an older
// bundle would report releases that did not exist when it was written.
func TestReadReleasesAsOfAnEarlierCommit(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "a/x.go", "package a\n")
	gitCommit(t, dir, "first")
	gitRun(t, dir, "tag", "v0.1.0")
	old := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))

	write(t, dir, "a/x.go", "package a\n\nfunc F() {}\n")
	gitCommit(t, dir, "second")
	gitRun(t, dir, "tag", "v0.2.0")

	now, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if now.Releases.Count != 2 {
		t.Fatalf("Count at HEAD = %d, want 2", now.Releases.Count)
	}

	past, err := Read(context.Background(), dir, Options{AsOf: old})
	if err != nil {
		t.Fatalf("Read as-of: %v", err)
	}
	if past.Releases.Count != 1 {
		t.Errorf("Count as of the first commit = %d, want 1", past.Releases.Count)
	}
	if past.Releases.Latest != "v0.1.0" {
		t.Errorf("Latest as of the first commit = %q, want v0.1.0", past.Releases.Latest)
	}
	// The described commit is the tag itself, so nothing is past it.
	if past.Releases.CommitsSince != 0 {
		t.Errorf("CommitsSince = %d, want 0 at the tagged commit", past.Releases.CommitsSince)
	}
}

// The ambiguity the Available flag exists for. A shallow clone has no tags, and so does a
// repository nobody tagged; reporting the first as "nothing is tagged" would be a false claim
// about somebody else's repository.
func TestReadReleasesUnknownOnAShallowClone(t *testing.T) {
	src := gitRepo(t)
	write(t, src, "a/x.go", "package a\n")
	gitCommit(t, src, "first")
	gitRun(t, src, "tag", "v0.1.0")
	write(t, src, "a/x.go", "package a\n\nfunc F() {}\n")
	gitCommit(t, src, "second")

	clone := filepath.Join(t.TempDir(), "shallow")
	// file:// rather than a plain path: git refuses --depth against a local path copy.
	gitRun(t, src, "clone", "--depth=1", "file://"+filepath.ToSlash(src), clone)

	s, err := Read(context.Background(), clone, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Releases.Available {
		t.Fatalf("a shallow clone reported a definite answer: %+v", s.Releases)
	}
	if s.Releases.Count != 0 {
		t.Errorf("Count = %d on an unavailable read", s.Releases.Count)
	}
	if !strings.Contains(s.Releases.Reason, "fetch-depth") {
		t.Errorf("Reason = %q, want it to name the CI fix", s.Releases.Reason)
	}
}

// A tag whose name collides with a branch name. `refs/tags/x..rev` resolves to the tag; the
// unqualified `x..rev` would be ambiguous, and git would prefer the branch under some
// configurations.
func TestCountSinceResolvesTheTagNotABranch(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "a/x.go", "package a\n")
	gitCommit(t, dir, "first")
	gitRun(t, dir, "tag", "release")
	write(t, dir, "a/x.go", "package a\n\nfunc F() {}\n")
	gitCommit(t, dir, "second")
	// A branch of the same name, pointed at the newer commit. If countSince resolved the
	// branch it would report zero commits since "release".
	gitRun(t, dir, "branch", "release")

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Releases.CommitsSince != 1 {
		t.Errorf("CommitsSince = %d, want 1: the branch was resolved instead of the tag",
			s.Releases.CommitsSince)
	}
}

// readReleases refuses a revision that is not a full sha, which is the guard that keeps the
// promise true for a future caller rather than only for Read.
func TestReadReleasesRefusesANonSha(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "a/x.go", "package a\n")
	gitCommit(t, dir, "first")
	gitRun(t, dir, "tag", "v0.1.0")

	opts := Options{}.withDefaults()
	for _, rev := range []string{"HEAD", "v0.1.0", "abc1234", "--output=/tmp/x", ":/first"} {
		r := readReleases(context.Background(), dir, opts, Commit{SHA: rev}, false)
		if r.Available {
			t.Errorf("readReleases accepted %q: %+v", rev, r)
		}
		if r.Reason == "" {
			t.Errorf("readReleases(%q) refused without saying why", rev)
		}
	}
	// And the empty sha, which is what readHead returns when it could not resolve one.
	if r := readReleases(context.Background(), dir, opts, Commit{}, false); r.Available {
		t.Errorf("readReleases accepted an empty sha: %+v", r)
	}
}

// A repository whose history could not be read at all reports no releases either. Reading tags
// while claiming no history would put a released version on a page that says it knows nothing
// about how the repository changes.
func TestReleasesUnavailableWithoutHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Releases.Available {
		t.Errorf("a non-repository reported releases: %+v", s.Releases)
	}
}
