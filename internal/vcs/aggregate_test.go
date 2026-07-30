package vcs

import (
	"fmt"
	"testing"
)

func c(hash, author, date string, files ...fileChange) commit {
	return commit{hash: hash, author: author, date: date, files: files}
}

func f(path string, ins, del int) fileChange {
	return fileChange{path: path, insertions: ins, deletions: del}
}

func TestAggregateChurnAndDates(t *testing.T) {
	// Newest first, which is the order git walks and which aggregate relies on to tell
	// a first touch from a last one.
	s := aggregate([]commit{
		c("c3", "Ann", "2026-03-01", f("a/x.go", 5, 1)),
		c("c2", "Bob", "2026-02-01", f("a/x.go", 2, 0), f("b/y.go", 9, 3)),
		c("c1", "Ann", "2026-01-01", f("a/x.go", 10, 0)),
	}, Options{}.withDefaults())

	if !s.Available {
		t.Fatal("signals reported unavailable")
	}
	x := s.Paths["a/x.go"]
	if x == nil {
		t.Fatal("a/x.go absent")
	}
	if x.Commits != 3 {
		t.Errorf("commits = %d, want 3", x.Commits)
	}
	if x.Insertions != 17 || x.Deletions != 1 {
		t.Errorf("churn = +%d/-%d, want +17/-1", x.Insertions, x.Deletions)
	}
	// The walk is newest-first, so Last is the newest date seen and First the oldest.
	if x.Last != "2026-03-01" {
		t.Errorf("Last = %q, want 2026-03-01", x.Last)
	}
	if x.First != "2026-01-01" {
		t.Errorf("First = %q, want 2026-01-01", x.First)
	}
}

// A moved file keeps one history under its current name. git marks the rename only in the
// commit that performed it, so without folding the old path forward the churn of a
// recently moved file reads as though the code were new — which is exactly backwards.
func TestAggregateFoldsRenamedHistoryForward(t *testing.T) {
	s := aggregate([]commit{
		// Newest first, as git walks. The rename is seen before the commits using the old
		// name, which is what makes a single pass sufficient.
		c("c3", "Ann", "2026-03-01", f("a/new.go", 1, 0)),
		c("c2", "Ann", "2026-02-01", fileChange{path: "a/new.go", oldPath: "a/old.go", insertions: 2}),
		c("c1", "Ann", "2026-01-01", f("a/old.go", 10, 0)),
	}, Options{}.withDefaults())

	if _, stale := s.Paths["a/old.go"]; stale {
		t.Errorf("the pre-rename path survived: %v", pathKeys(s))
	}
	p := s.Paths["a/new.go"]
	if p == nil {
		t.Fatalf("current path absent: %v", pathKeys(s))
	}
	if p.Commits != 3 {
		t.Errorf("commits = %d, want 3 across the rename", p.Commits)
	}
	if p.Insertions != 13 {
		t.Errorf("insertions = %d, want 13 across the rename", p.Insertions)
	}
	// The pre-rename commit is the file's real first appearance.
	if p.First != "2026-01-01" {
		t.Errorf("First = %q, want the pre-rename date", p.First)
	}
}

// a -> b -> c. The oldest commits name a, and a is only linked to c through b.
func TestAggregateFollowsChainedRenames(t *testing.T) {
	s := aggregate([]commit{
		c("c4", "Ann", "2026-04-01", fileChange{path: "c.go", oldPath: "b.go", insertions: 1}),
		c("c3", "Ann", "2026-03-01", f("b.go", 1, 0)),
		c("c2", "Ann", "2026-02-01", fileChange{path: "b.go", oldPath: "a.go", insertions: 1}),
		c("c1", "Ann", "2026-01-01", f("a.go", 1, 0)),
	}, Options{}.withDefaults())

	if len(s.Paths) != 1 {
		t.Fatalf("chain split into %v, want one path", pathKeys(s))
	}
	p := s.Paths["c.go"]
	if p == nil || p.Commits != 4 || p.Insertions != 4 {
		t.Errorf("chained history = %+v, want 4 commits and 4 insertions under c.go", p)
	}
}

func pathKeys(s *Signals) []string {
	var out []string
	for _, p := range s.PathsSorted() {
		out = append(out, p.Path)
	}
	return out
}

func TestAggregateTopAuthorConcentration(t *testing.T) {
	s := aggregate([]commit{
		c("c4", "Ann", "2026-04-01", f("a/x.go", 1, 0)),
		c("c3", "Ann", "2026-03-01", f("a/x.go", 1, 0)),
		c("c2", "Ann", "2026-02-01", f("a/x.go", 1, 0)),
		c("c1", "Bob", "2026-01-01", f("a/x.go", 1, 0)),
	}, Options{}.withDefaults())

	name, share := s.Paths["a/x.go"].TopAuthor()
	if name != "Ann" {
		t.Errorf("top author = %q, want Ann", name)
	}
	if share != 0.75 {
		t.Errorf("share = %v, want 0.75", share)
	}
}

// Ties break by name, because the alternative is a value that depends on map iteration
// order and this number reaches the committed bundle.
func TestTopAuthorTieBreaksDeterministically(t *testing.T) {
	p := &PathHistory{Path: "x", Commits: 2, Authors: map[string]int{"Zoe": 1, "Ann": 1}}
	for i := 0; i < 50; i++ {
		if name, _ := p.TopAuthor(); name != "Ann" {
			t.Fatalf("iteration %d: top author = %q, want Ann every time", i, name)
		}
	}
}

func TestTopAuthorEmpty(t *testing.T) {
	p := &PathHistory{Path: "x"}
	if name, share := p.TopAuthor(); name != "" || share != 0 {
		t.Errorf("empty signal returned %q/%v", name, share)
	}
}

func TestAggregateCoChangePairsAndOrdering(t *testing.T) {
	s := aggregate([]commit{
		c("c3", "Ann", "2026-03-01", f("a/x.go", 1, 0), f("b/y.go", 1, 0)),
		c("c2", "Ann", "2026-02-01", f("a/x.go", 1, 0), f("b/y.go", 1, 0)),
		c("c1", "Ann", "2026-01-01", f("a/x.go", 1, 0), f("c/z.go", 1, 0)),
	}, Options{}.withDefaults())

	// a<->b twice; a<->c once, which is dropped as coincidence.
	if len(s.CoChange) != 1 {
		t.Fatalf("got %d pairs, want 1: %+v", len(s.CoChange), s.CoChange)
	}
	p := s.CoChange[0]
	if p.A != "a" || p.B != "b" || p.Commits != 2 {
		t.Errorf("pair = %+v, want a<->b at 2", p)
	}
}

// Pairs are canonically ordered, so the same coupling is one fact regardless of which
// file git happened to list first.
func TestAggregateCoChangePairOrderIsCanonical(t *testing.T) {
	s := aggregate([]commit{
		c("c2", "Ann", "2026-02-01", f("z/late.go", 1, 0), f("a/early.go", 1, 0)),
		c("c1", "Ann", "2026-01-01", f("a/early.go", 1, 0), f("z/late.go", 1, 0)),
	}, Options{}.withDefaults())

	if len(s.CoChange) != 1 {
		t.Fatalf("got %d pairs, want 1: %+v", len(s.CoChange), s.CoChange)
	}
	if s.CoChange[0].A != "a" || s.CoChange[0].B != "z" {
		t.Errorf("pair = %+v, want A=a B=z", s.CoChange[0])
	}
}

// A sweep commit — a formatter run, a licence header, an initial import — relates every
// directory to every other and says nothing. Its churn is still real and still counted.
func TestAggregateSkipsBulkCommitsForCoChangeButNotChurn(t *testing.T) {
	var files []fileChange
	for i := 0; i < 6; i++ {
		files = append(files, f(fmt.Sprintf("d%d/x.go", i), 1, 0))
	}
	opts := Options{MaxDirsPerCommit: 3}.withDefaults()
	s := aggregate([]commit{
		c("sweep", "Ann", "2026-02-01", files...),
		c("sweep2", "Ann", "2026-01-02", files...),
	}, opts)

	if s.SkippedBulkCommits != 2 {
		t.Errorf("SkippedBulkCommits = %d, want 2", s.SkippedBulkCommits)
	}
	if len(s.CoChange) != 0 {
		t.Errorf("bulk commits produced pairs: %+v", s.CoChange)
	}
	// Churn is untouched: the files really did change.
	if p := s.Paths["d0/x.go"]; p == nil || p.Commits != 2 {
		t.Errorf("churn lost for a bulk-commit file: %+v", p)
	}
}

// The determinism requirement of §8.1, at this layer. Map iteration order is randomised
// per run, so an ordering bug here would be intermittent rather than absent.
func TestAggregateIsDeterministic(t *testing.T) {
	commits := []commit{
		c("c4", "Ann", "2026-04-01", f("a/x.go", 1, 0), f("b/y.go", 1, 0), f("c/z.go", 1, 0)),
		c("c3", "Bob", "2026-03-01", f("a/x.go", 1, 0), f("b/y.go", 1, 0)),
		c("c2", "Ann", "2026-02-01", f("b/y.go", 1, 0), f("c/z.go", 1, 0)),
		c("c1", "Bob", "2026-01-01", f("a/x.go", 1, 0), f("c/z.go", 1, 0)),
	}
	want := render(aggregate(commits, Options{}.withDefaults()))
	for i := 0; i < 25; i++ {
		if got := render(aggregate(commits, Options{}.withDefaults())); got != want {
			t.Fatalf("run %d differs:\n got %s\nwant %s", i, got, want)
		}
	}
}

// render flattens signals into a comparable string, through the sorted accessors a
// consumer uses rather than by walking the maps directly.
func render(s *Signals) string {
	out := ""
	for _, set := range [][]*PathHistory{s.PathsSorted(), s.DirsSorted()} {
		for _, p := range set {
			name, share := p.TopAuthor()
			out += fmt.Sprintf("%s c=%d +%d-%d %s..%s top=%s/%.2f\n",
				p.Path, p.Commits, p.Insertions, p.Deletions, p.First, p.Last, name, share)
		}
	}
	for _, pr := range s.CoChange {
		out += fmt.Sprintf("%s<->%s %d\n", pr.A, pr.B, pr.Commits)
	}
	return out
}

func TestAggregateBinaryKeepsCountsUnknown(t *testing.T) {
	s := aggregate([]commit{
		c("c2", "Ann", "2026-02-01", fileChange{path: "logo.png", binary: true}),
		// A text change to the same path afterwards must not resurrect line counts for
		// a file whose history includes a binary revision.
		c("c1", "Ann", "2026-01-01", f("logo.png", 4, 0)),
	}, Options{}.withDefaults())

	p := s.Paths["logo.png"]
	if !p.Binary {
		t.Error("path not marked binary")
	}
	if p.Insertions != 0 || p.Deletions != 0 {
		t.Errorf("binary path reported +%d/-%d, want unknown (zero)", p.Insertions, p.Deletions)
	}
}

// Root-level files share the "" directory. Pairing them with each other would claim every
// top-level file is coupled to every other, which says nothing.
func TestAggregateRootFilesDoNotPairWithThemselves(t *testing.T) {
	s := aggregate([]commit{
		c("c2", "Ann", "2026-02-01", f("README.md", 1, 0), f("CHANGELOG.md", 1, 0)),
		c("c1", "Ann", "2026-01-01", f("README.md", 1, 0), f("CHANGELOG.md", 1, 0)),
	}, Options{}.withDefaults())

	if len(s.CoChange) != 0 {
		t.Errorf("root-level files produced pairs: %+v", s.CoChange)
	}
}

// The directory rollup is not a sum of its files. A commit touching three files in one
// directory is one commit for that directory; adding the per-file counts would inflate
// every directory by its own file count, which is the number the graph would then print.
func TestAggregateDirsCountCommitsNotFiles(t *testing.T) {
	s := aggregate([]commit{
		c("c2", "Ann", "2026-02-01", f("a/x.go", 1, 0), f("a/y.go", 2, 0), f("a/z.go", 3, 1)),
		c("c1", "Ann", "2026-01-01", f("a/x.go", 4, 0)),
	}, Options{}.withDefaults())

	d := s.Dirs["a"]
	if d == nil {
		t.Fatalf("directory absent: %v", dirKeys(s))
	}
	if d.Commits != 2 {
		t.Errorf("directory commits = %d, want 2 (one per commit, not per file)", d.Commits)
	}
	// Line counts do sum: they are a quantity of change, not a count of events.
	if d.Insertions != 10 || d.Deletions != 1 {
		t.Errorf("directory churn = +%d/-%d, want +10/-1", d.Insertions, d.Deletions)
	}
	// An author committed once to the directory, not three times.
	if got := d.Authors["Ann"]; got != 2 {
		t.Errorf("author commits = %d, want 2", got)
	}
	if name, share := d.TopAuthor(); name != "Ann" || share != 1 {
		t.Errorf("top author = %q at %v, want Ann at 1", name, share)
	}
}

// A binary file does not make its directory's line counts unknown: the other files in the
// directory still have real counts, and the directory total is a sum over files rather
// than a claim about one blob.
func TestAggregateDirsToleratesBinaryMembers(t *testing.T) {
	s := aggregate([]commit{
		c("c1", "Ann", "2026-01-01",
			fileChange{path: "assets/logo.png", binary: true},
			f("assets/gen.go", 5, 0)),
	}, Options{}.withDefaults())

	d := s.Dirs["assets"]
	if d == nil {
		t.Fatalf("directory absent: %v", dirKeys(s))
	}
	if d.Binary {
		t.Error("a directory containing a binary file was itself marked binary")
	}
	if d.Insertions != 5 {
		t.Errorf("insertions = %d, want 5 from the text file alone", d.Insertions)
	}
}

// Root-level files roll up under "", which is the key the graph uses for the repository
// root. An empty string is a real key here, not a missing one.
func TestAggregateDirsUsesEmptyKeyForRoot(t *testing.T) {
	s := aggregate([]commit{
		c("c1", "Ann", "2026-01-01", f("README.md", 1, 0)),
	}, Options{}.withDefaults())

	if _, ok := s.Dirs[""]; !ok {
		t.Errorf("root directory absent: %v", dirKeys(s))
	}
}

// A bulk commit contributes no co-change, and that must not cost it its churn — the skip
// happens after both rollups are applied.
func TestAggregateBulkCommitStillCountsDirChurn(t *testing.T) {
	var files []fileChange
	for i := 0; i < 6; i++ {
		files = append(files, f(fmt.Sprintf("d%d/x.go", i), 2, 0))
	}
	s := aggregate([]commit{
		c("sweep", "Ann", "2026-01-01", files...),
	}, Options{MaxDirsPerCommit: 3}.withDefaults())

	if s.SkippedBulkCommits != 1 {
		t.Errorf("SkippedBulkCommits = %d, want 1", s.SkippedBulkCommits)
	}
	d := s.Dirs["d0"]
	if d == nil || d.Commits != 1 || d.Insertions != 2 {
		t.Errorf("directory churn lost on a bulk commit: %+v", d)
	}
}

// A rename across directories moves the file's whole history to the new directory, since
// the directory rollup is keyed by the canonical path.
func TestAggregateDirsFollowRenames(t *testing.T) {
	s := aggregate([]commit{
		c("c2", "Ann", "2026-02-01", fileChange{path: "new/x.go", oldPath: "old/x.go", insertions: 1}),
		c("c1", "Ann", "2026-01-01", f("old/x.go", 9, 0)),
	}, Options{}.withDefaults())

	if _, stale := s.Dirs["old"]; stale {
		t.Errorf("the pre-rename directory survived: %v", dirKeys(s))
	}
	d := s.Dirs["new"]
	if d == nil || d.Commits != 2 || d.Insertions != 10 {
		t.Errorf("directory history = %+v, want 2 commits and 10 insertions under new", d)
	}
}

func dirKeys(s *Signals) []string {
	var out []string
	for _, d := range s.DirsSorted() {
		out = append(out, d.Path)
	}
	return out
}

func TestAggregateEmpty(t *testing.T) {
	s := aggregate(nil, Options{}.withDefaults())
	if !s.Available {
		t.Error("an empty walk is available, just empty")
	}
	if s.Commits != 0 || len(s.Paths) != 0 || len(s.Dirs) != 0 || len(s.CoChange) != 0 {
		t.Errorf("non-empty result from no commits: %+v", s)
	}
}
