// Package vcs reads what a repository's history says about its own structure.
//
// This is the signal design §4.1 calls out as "the cheapest way to find coupling that
// imports do not show": two files that always change together are coupled whether or
// not either imports the other, and that is exactly the coupling a static read cannot
// see. A test and its subject, a handler and the migration it depends on, a config key
// and the code that reads it — none of those are import edges, and all of them are
// things an agent about to make a change needs to know.
//
// Three properties shape everything here, and each is a test:
//
//   - **Absence is reported, never fatal.** git may be missing, the directory may not
//     be a repository, the clone may be shallow, the history may be empty. None of
//     those are errors: the deterministic core produces a complete structural bundle
//     without any of this (§4.4), and history is additive. What is never acceptable is
//     silence — a shallow clone that yielded thin signals must say so, because "no
//     co-change found" and "no history to look at" are different claims and only one of
//     them is a fact about the code.
//   - **Deterministic.** Same history in, same signals out. Every map is drained
//     through a sorted key list before it reaches output, for the reason §8.1 gives.
//   - **Bounded.** A repository is untrusted input, and history is the one input with
//     no natural size limit. Commits are capped, and a commit touching an implausible
//     number of directories contributes churn but no co-change.
//
// Nothing here shells out to anything but git, and every argument is a compile-time
// constant: the repository path is passed as the subprocess's working directory rather
// than interpolated into an argument, so a directory named `--upload-pack=...` cannot
// become a flag.
package vcs

import (
	"sort"
	"strconv"
	"time"
)

// Options bound the read. The zero value is usable and applies the defaults below.
type Options struct {
	// MaxCommits caps how far back to walk. Zero applies DefaultMaxCommits.
	//
	// A cap rather than the whole history, because the value of a co-change signal
	// decays: a pair that last changed together four years and one rewrite ago is not
	// telling you about the code as it stands. The cap doubles as the bound on an
	// input with no inherent size.
	MaxCommits int

	// MaxDirsPerCommit is the largest number of distinct directories a commit may touch
	// and still contribute co-change. Zero applies DefaultMaxDirsPerCommit.
	//
	// A dependency bump, a licence-header sweep, a formatter rollout, or an initial
	// import touches everything and means nothing: it would relate every directory to
	// every other, which is both the densest possible graph and the least informative.
	// Churn from such a commit is still counted — the files really did change.
	MaxDirsPerCommit int

	// Timeout bounds the git invocation. Zero applies DefaultTimeout.
	Timeout time.Duration
}

// Defaults. Deliberately generous: the point of the caps is to make an unbounded input
// bounded, not to sample.
const (
	DefaultMaxCommits       = 2000
	DefaultMaxDirsPerCommit = 40
	DefaultTimeout          = 60 * time.Second
)

func (o Options) withDefaults() Options {
	if o.MaxCommits <= 0 {
		o.MaxCommits = DefaultMaxCommits
	}
	if o.MaxDirsPerCommit <= 0 {
		o.MaxDirsPerCommit = DefaultMaxDirsPerCommit
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	return o
}

// PathHistory is what history says about one path — a file in Signals.Paths, or a
// directory in Signals.Dirs.
//
// Keyed by the path's current name. A rename is followed, so the churn of
// `internal/manifest/yaml.go` includes the commits it accrued as
// `internal/config/yaml.go` — which is the only reading that makes the number mean
// "how much this code has changed" rather than "how long since someone moved it".
type PathHistory struct {
	Path string
	// Commits is how many commits touched this path.
	Commits int
	// Insertions and Deletions sum the line counts git reported. Both are zero for a
	// binary file, which git reports as `-` rather than a number; Binary records that,
	// so a binary file's zero is distinguishable from a genuinely empty diff.
	Insertions int
	Deletions  int
	Binary     bool
	// First and Last are the author dates of the oldest and newest commit touching this
	// path, as YYYY-MM-DD. Author dates rather than commit dates: a rebase rewrites the
	// latter, and "when was this written" is the question being asked.
	//
	// First is only the true first commit when the walk was not truncated. Truncated
	// says whether it was.
	First string
	Last  string
	// Authors counts commits per author name, after .mailmap consolidation.
	Authors map[string]int
}

// TopAuthor returns the author with the most commits and their share of the total,
// breaking ties by name so the result is deterministic.
//
// Concentration is the useful form of this signal rather than the raw list: one author
// at 95% is a bus-factor finding, and five authors at 20% each is a different fact about
// the same file. Returning the pair lets a caller state either.
func (p *PathHistory) TopAuthor() (string, float64) {
	if len(p.Authors) == 0 || p.Commits == 0 {
		return "", 0
	}
	names := make([]string, 0, len(p.Authors))
	for n := range p.Authors {
		names = append(names, n)
	}
	sort.Strings(names)
	best := names[0]
	for _, n := range names[1:] {
		if p.Authors[n] > p.Authors[best] {
			best = n
		}
	}
	return best, float64(p.Authors[best]) / float64(p.Commits)
}

// Pair is two directories that changed in the same commit, and how often.
//
// Directories, not files, and that is a deliberate narrowing rather than a limitation.
// The graph's module nodes are directories (see assemble.addModules for why every one of
// the four first-class languages agrees on that grouping and on nothing finer), so a
// file-level pair would have to be aggregated to a directory before it could become an
// edge. Computing it at the directory level instead is the same answer for less work: a
// commit touching forty files across three directories yields three pairs rather than
// 780.
type Pair struct {
	// A and B are directory paths, A < B. Ordered so the pair is one fact rather than
	// two, and so output order does not depend on which file git listed first.
	A string
	B string
	// Commits is how many commits touched both directories.
	Commits int
}

// Signals is everything history yielded, including an account of what it could not.
type Signals struct {
	// Available is false when no history could be read at all. Reason says why, and
	// every other field is zero. This is not an error condition: see the package doc.
	Available bool
	// Reason explains an unavailable or partial read, in a form fit to print. Empty on
	// a complete read.
	Reason string
	// Shallow reports a shallow clone. The signals are real but truncated, and a
	// consumer must say so rather than presenting them as the whole history — this is
	// the case where a caller most needs to distinguish "not coupled" from "not looked
	// at". CI is where it bites: a default checkout is depth 1.
	Shallow bool
	// Commits is how many commits were examined.
	Commits int
	// Truncated reports that MaxCommits was reached, so First dates are lower bounds
	// and the oldest history was not read.
	Truncated bool
	// Paths is keyed by current repo-relative path, slash-separated.
	Paths map[string]*PathHistory
	// Dirs is the same signal aggregated to the directory, keyed by directory path with
	// "" for the repository root.
	//
	// Aggregated here rather than by the caller, because the aggregation is not a sum: a
	// commit touching three files in one directory is one commit for that directory, not
	// three, and adding the per-file counts would inflate every directory by its own file
	// count. Line counts do sum. The graph's nodes are directories, so this is the form
	// the graph consumes.
	Dirs map[string]*PathHistory
	// CoChange is sorted by Commits descending, then A, then B. Pairs seen in exactly
	// one commit are dropped: one shared commit is a coincidence, and keeping them
	// would bury the signal under every file that happened to arrive in the same
	// initial import.
	CoChange []Pair
	// SkippedBulkCommits counts commits that touched more directories than
	// MaxDirsPerCommit and so contributed no co-change. Reported because a repository
	// whose history is mostly sweeps has a co-change signal built from very little, and
	// a consumer should be able to say that.
	SkippedBulkCommits int

	// Head identifies the commit the analysis describes. Zero when Available is false.
	Head Commit
}

// Commit identifies the commit an analysis describes.
//
// Read even though the history walk already has HEAD in it, because the two answer
// different questions: the walk says what changed over time, and this says which tree
// was on disk. A bundle records the second in every page's `resource:` field, so that a
// reader can tell whether the page describes the code they are looking at.
type Commit struct {
	// SHA is the full 40-character hash. Full rather than abbreviated: an abbreviation
	// is only unique until the repository grows, and the field is written into a
	// committed artifact that outlives the run.
	SHA string
	// Date is the commit's author date as YYYY-MM-DD.
	//
	// This is what the emitter stamps as `generated.at`, in place of the wall clock. A
	// wall-clock timestamp would make every run of the same commit produce different
	// bytes, which is precisely the commit churn ADR 0005 forbids — and it would be
	// answering the wrong question anyway. "When was this written" is a fact about the
	// code; "when did CI happen to run" is a fact about CI.
	Date string
}

// Short returns the conventional 7-character abbreviation, for display only.
func (c Commit) Short() string {
	if len(c.SHA) < 7 {
		return c.SHA
	}
	return c.SHA[:7]
}

// PathsSorted returns the per-file signals in path order.
func (s *Signals) PathsSorted() []*PathHistory { return sortedHistories(s.Paths) }

// DirsSorted returns the per-directory signals in path order.
func (s *Signals) DirsSorted() []*PathHistory { return sortedHistories(s.Dirs) }

func sortedHistories(m map[string]*PathHistory) []*PathHistory {
	out := make([]*PathHistory, 0, len(m))
	for _, k := range sortedKeys(m) {
		out = append(out, m[k])
	}
	return out
}

// unavailable builds the result for a repository whose history could not be read.
//
// Both maps are non-nil so a caller can range over either without guarding a path it did
// not choose to take.
func unavailable(reason string) *Signals {
	return &Signals{
		Reason: reason,
		Paths:  map[string]*PathHistory{},
		Dirs:   map[string]*PathHistory{},
	}
}

// commit is one parsed history entry. Internal: the aggregate is the output, and a
// caller has no use for the raw walk.
type commit struct {
	hash   string
	author string
	date   string
	files  []fileChange
}

type fileChange struct {
	path string
	// oldPath is set only for a rename, to the name the file had before this commit.
	// Retained rather than discarded because it is the only record of the link: git marks
	// a rename in the commit where it happens but does not rewrite the older commits,
	// which still name the old path. See aggregate.
	oldPath    string
	insertions int
	deletions  int
	binary     bool
}

// aggregate turns the parsed walk into signals.
//
// Split from parsing so that it is testable on hand-built commits without git, and from
// the exec layer so that neither depends on the other. This is where the caps in Options
// are applied, and the only place either the path map or the pair map is written.
func aggregate(commits []commit, opts Options) *Signals {
	s := &Signals{
		Available: true,
		Commits:   len(commits),
		Paths:     make(map[string]*PathHistory, len(commits)),
		Dirs:      make(map[string]*PathHistory),
	}
	pairs := make(map[Pair]int)

	// canonical maps a path git named in some commit to what the file is called now.
	//
	// git marks a rename in the commit that performed it and stops there: older commits
	// still name the old path, and `--follow` — which would stitch them together — only
	// accepts a single pathspec, so it is unavailable to a whole-history walk. Without this
	// map a moved file's history splits in two, and the churn of the current path reads as
	// though the code were new. Verified by test against real git, which is how the split
	// was found.
	//
	// The walk is newest-first, so a rename is always seen before the commits that used the
	// old name — meaning one pass suffices and no second reconciliation is needed.
	canonical := make(map[string]string)
	resolve := func(p string) string {
		// Chained renames: a -> b -> c leaves b pointing at c and a pointing at b, so
		// follow the chain. Bounded by the number of entries, and self-references are
		// impossible because a rename's two paths always differ.
		for i := 0; i < len(canonical); i++ {
			next, ok := canonical[p]
			if !ok || next == p {
				break
			}
			p = next
		}
		return p
	}

	for _, c := range commits {
		// dirs accumulates this commit's per-directory totals before any of them are
		// applied. Buffered rather than applied inline because the directory's commit
		// count must go up by one however many of its files the commit touched, and
		// only a per-commit set can say that.
		dirs := make(map[string]*fileChange, len(c.files))
		for _, f := range c.files {
			path := resolve(f.path)
			if f.oldPath != "" && f.oldPath != path {
				canonical[f.oldPath] = path
			}
			touch(s.Paths, path, c, f.insertions, f.deletions, f.binary)

			d := dirOf(path)
			agg, ok := dirs[d]
			if !ok {
				agg = &fileChange{path: d}
				dirs[d] = agg
			}
			// A binary file inside a directory does not make the directory's line counts
			// unknown — the other files in it still have real counts, and the directory
			// total is a sum over files rather than a claim about one blob. So the binary
			// file simply contributes nothing to the sum.
			if !f.binary {
				agg.insertions += f.insertions
				agg.deletions += f.deletions
			}
		}
		for _, d := range sortedKeys(dirs) {
			agg := dirs[d]
			touch(s.Dirs, d, c, agg.insertions, agg.deletions, false)
		}

		if len(dirs) > opts.MaxDirsPerCommit {
			s.SkippedBulkCommits++
			continue
		}
		names := sortedKeys(dirs)
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				// names is sorted, so names[i] < names[j] and the pair is already in
				// canonical order.
				pairs[Pair{A: names[i], B: names[j]}]++
			}
		}
	}

	for pr, n := range pairs {
		if n < 2 {
			continue
		}
		pr.Commits = n
		s.CoChange = append(s.CoChange, pr)
	}
	sort.Slice(s.CoChange, func(i, j int) bool {
		a, b := s.CoChange[i], s.CoChange[j]
		if a.Commits != b.Commits {
			return a.Commits > b.Commits
		}
		if a.A != b.A {
			return a.A < b.A
		}
		return a.B < b.B
	})
	return s
}

// touch records one commit's effect on one path, creating the entry on first sight.
//
// Shared by the file and directory passes so the two cannot drift: a bug fixed in the
// date handling of one would otherwise have to be remembered in the other.
func touch(into map[string]*PathHistory, path string, c commit, ins, del int, binary bool) {
	p, ok := into[path]
	if !ok {
		p = &PathHistory{Path: path, Authors: map[string]int{}}
		into[path] = p
	}
	p.Commits++
	p.Binary = p.Binary || binary
	if p.Binary {
		// A binary file's line counts are not zero, they are unknown: git writes `-`
		// where a number would go. Once any revision of a path was binary, the running
		// total cannot be read as "lines changed" — so it is held at zero rather than
		// accumulating whichever text revisions happen to surround the binary one.
		// Suppressing on p.Binary rather than the argument is what makes that independent
		// of where in the walk the binary revision appears.
		p.Insertions, p.Deletions = 0, 0
	} else {
		p.Insertions += ins
		p.Deletions += del
	}
	if c.author != "" {
		p.Authors[c.author]++
	}
	// The walk is newest-first, so the first date seen for a path is its last touch and
	// the last date seen is its first. Assigning both unconditionally on every commit,
	// rather than comparing, keeps this correct without assuming the dates are
	// well-ordered — a repository can contain an author date from the future, and one does
	// not have to be malformed to do so.
	if p.Last == "" {
		p.Last = c.date
	}
	if c.date != "" {
		p.First = c.date
	}
}

// sortedKeys returns a map's keys in order, so every loop that reaches output walks a
// sorted slice — the determinism requirement of §8.1.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// dirOf returns the directory holding a path, "" for a file at the repository root.
// Slash-separated on every platform: these are git paths, not filesystem paths, and git
// uses forward slashes everywhere including on Windows.
func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return ""
}

// atoiOr parses a git numstat count, returning fallback for anything unexpected. git
// writes `-` for a binary file, which the caller detects before calling this.
func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
