package vcs

import (
	"context"
	"strconv"
	"strings"
)

// Reading tags.
//
// Two invocations, both cheap: one `for-each-ref` for the reachable tags and one `rev-list
// --count` for the distance to the newest. Measured on a 365-tag repository at roughly the
// cost of one `git rev-parse` each — the same order as the checks Read already makes, and
// three orders below the per-file `blame` pass that the same triage refused.
//
// Refs, not commits, which is why this is not folded into the log walk: a tag is a name
// pointing at an object, and the walk sees objects.

// readReleases reports what tags say about versioning, as of the commit the analysis
// describes.
//
// An unreadable tag list is Available false with a Reason rather than an empty list, and that
// distinction is the point of the function. A shallow clone has no tags — verified, a
// `--depth 1` clone reports none — and so does a repository nobody has tagged. Both look
// identical from the tag list alone, and reporting the first as "no release is tagged" would
// be a false claim about somebody else's repository. So the caller passes what it already
// knows about the clone, and this refuses to answer when the answer would not mean anything.
func readReleases(ctx context.Context, dir string, opts Options, head Commit, shallow bool) Releases {
	if shallow {
		// Named as a CI problem for the same reason Signals.Reason is: this is where it
		// happens, a default actions/checkout is depth 1, and a warning without a fix is a
		// warning a reader cannot act on. A full-history checkout gets tags even with
		// --no-tags, because at fetch-depth 0 actions/checkout adds the tag refspec
		// explicitly — checked against the pinned version rather than assumed.
		return Releases{Reason: "the clone is shallow, so whether anything is tagged could not be" +
			" determined (in CI, set fetch-depth: 0)"}
	}
	rev := head.SHA
	if rev == "" {
		// No commit identity means readHead failed, and every ref query below would be
		// answering about HEAD while the rest of the analysis describes something else.
		return Releases{Reason: "the commit being described is not known, so tags were not read"}
	}
	if !validCommit(rev) {
		// Unreachable through Read, which only ever passes a sha git itself printed. Kept
		// because this function takes a revision and hands it to git, and the guard is what
		// makes that true of every future caller rather than of the current one.
		return Releases{Reason: "the commit being described is not a full sha, so tags were not read"}
	}

	// --merged bounds the list to tags reachable from the described commit. Reachable rather
	// than every tag in the repository, so that tagging an unrelated branch does not move
	// this number, and so a bundle read as of a recorded commit (Options.AsOf) sees the tags
	// that commit had rather than the ones the branch has now.
	//
	// Sorted newest-first by creation date, which for an annotated tag is when it was made
	// and for a lightweight one is its commit's date. Date is the primary key rather than
	// version order, because a repository tagging `2026.08` or `release-3` is not doing semver
	// and ranking its tags by a scheme it does not use would report the wrong one as latest.
	//
	// Version order breaks the tie, and the tie is not rare: creatordate is compared to the
	// second, so two tags cut in the same session are equal, and `%(creatordate:short)` renders
	// a whole day of them identically. git resolves an exact tie by refname *ascending*, which
	// picks the alphabetically first — measured, `v0.1.0` won over a `v0.2.0` created after it,
	// so the page named the older release as latest. `-v:refname` orders `v1.10.0` above
	// `v1.9.0` where plain `-refname` does not, and it degrades sensibly on names that are not
	// versions: `release-10` above `release-3`, `2026.11` above `2026.08`. git applies the keys
	// last-listed-first, so `--sort=-v:refname --sort=-creatordate` means date first, name only
	// where the dates are equal — verified, an older-dated `v9.0.0` still ranks below a
	// newer-dated `v1.0.0`.
	//
	// `--merged=<rev>`, attached, rather than a separate argument after `--end-of-options`.
	// The sentinel is what every other revision in this package hides behind, and here it does
	// not work: `--merged` takes its value as the following argument, so for-each-ref reads the
	// sentinel itself as the revision and fails with "malformed object name
	// --end-of-options". Found by test rather than reasoned about. The attached form is safe by
	// construction and not merely by convention — a value glued to its option cannot be parsed
	// as an option, whatever it contains — and validCommit above has already restricted this to
	// forty hex characters regardless.
	out, err := run(ctx, dir, opts, "for-each-ref",
		"--format=%(refname:strip=2)"+fieldSep+"%(creatordate:short)",
		"--sort=-v:refname", "--sort=-creatordate", "--merged="+rev, "refs/tags")
	if err != nil {
		return Releases{Reason: "tags could not be read: " + err.Error()}
	}

	r := Releases{Available: true}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		r.Count++
		if r.Latest != "" {
			continue
		}
		// The first line is the newest, and its name is the only tag name that leaves this
		// function. Safe to carry as text: git's own ref-name rules reject a newline, a tab,
		// `[`, `{`, `\`, `^`, `~`, `?`, `*`, and a double quote, verified by probe against
		// git 2.51 — so a tag name cannot break the YAML scalar or the markdown line it lands
		// in. It can still contain `$(id)` or a semicolon, which is why it is never handed to
		// a shell and why the consumer renders it as inline code.
		name, date, _ := strings.Cut(line, fieldSep)
		r.Latest, r.LatestDate = name, date
	}
	if r.Latest == "" {
		return r
	}
	r.CommitsSince = countSince(ctx, dir, opts, r.Latest, rev)
	return r
}

// countSince returns how many commits rev is past tag, or zero when the count cannot be had.
//
// Zero on failure is the same reading as zero on success — "not past it" — and that
// conflation is acceptable here in a way it would not be for the tag list: the distance is a
// refinement of a fact already established, where the list is the fact itself.
//
// The tag is named by its ref path rather than by the name alone, so a tag called `main`
// resolves to the tag and not to the branch. git's precedence rules would prefer the tag for
// `refs/tags/x`, but only the fully-qualified form says so outright.
func countSince(ctx context.Context, dir string, opts Options, tag, rev string) int {
	out, err := run(ctx, dir, opts, "rev-list", "--count", "--end-of-options",
		"refs/tags/"+tag+".."+rev)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || n < 0 {
		return 0
	}
	return n
}
