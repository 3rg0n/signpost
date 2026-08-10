# 24. A branch verify reads the history the bundle read, not just its stamp

## Status

Accepted

## Context

[ADR 0007](0007-the-bundle-names-the-commit-it-describes.md) stamps every page with the commit it
describes, and [ADR 0005](0005-commit-the-bundle-to-the-repository.md) commits the bundle. Together those
make the staleness check possible and make it awkward in one specific place: the bundle is written
on the default branch only (design §8.0), so on a branch its stamp names an older commit *by
construction*, and a strict `verify` reports every page as stale on a pull request that changed no
code. `verify -as-of-bundle` was the answer — take the two provenance fields from the bundle's own
`manifest.json`, compare the content byte for byte.

That was not enough, and the gap was found the way gaps like it are usually found: the gate had
never once passed on a conforming pull request. Three earlier pull requests appeared to pass it,
and auditing their diffs showed all three had committed `.signpost/` on the branch — which §8.0
forbids. On PR #26 the gate reported five problems on a branch whose only content change was two
Go files and a paragraph of documentation, and a controlled probe established why: one commit
adding a single comment line produced one problem, and a commit touching two directories produced
two.

[ADR 0020](0020-git-history-annotates-the-map-and-never-draws-it.md) is the cause, and it is
working as designed. History annotates the map — so seven churn attributes on a module page
(`commits`, `lines_added`, `lines_removed`, `first_commit`, `last_commit`, `top_author`,
`top_author_share`) and the `co_changes` edges are read from git, and all of them land in page
**content** rather than in frontmatter. Every commit on a branch moves them. A comment changes
`commits` and `lines_added` on that directory's page. A commit touching two directories can create
a `co_changes` edge, and that moves the edge totals on `index.md`, `log.md`, and `manifest.json`
too.

The obvious fix — adopt the recorded churn values field by field, the way provenance is already
adopted — cannot work, and finding that out is what produced this ADR rather than a patch. The
edge counts are arithmetic over a graph that genuinely has one more edge in it than the bundle's
graph had. There is no recorded field to copy for a number that was computed.

## Decision

**`-as-of-bundle` reads git history as of the commit the bundle records.** The log walk ends at
that commit, so the analysis sees exactly the commits the bundle saw, and every history-derived
field — attribute, edge, and count alike — is identical by construction rather than by exception.
The commit is read from `manifest.json` *before* the analysis runs, because it is an input to the
analysis and not something to reconcile after it.

Four constraints, each of which a later change will want to relax:

1. **Content is still compared byte for byte.** The mode relaxes which history is read, not
   whether pages match. A code change fails, a new module fails, a renamed module fails. This is
   what makes it safe to pass on every pull request, and it is asserted from both directions in
   `cmd/signpost`: a comment-only commit passes, a new module does not, and the same repository
   verified strictly still fails.
2. **The recorded sha is untrusted input.** It arrives from `manifest.json`, a committed file
   anyone with a pull request can edit, on its way to a git argument list — and git's revision
   syntax is wide enough to be dangerous on its own terms. Accepted only as forty lowercase hex
   characters, and passed after a `--end-of-options` sentinel regardless. `HEAD@{upstream}`,
   `:/text`, a branch name, a path-shaped revision, and an abbreviation are all refused. The
   sentinel is added only on the invocations that carry a sha, because it wants git 2.24 and the
   ordinary read must not: the commit-stamp walk reports failure by returning no commit, so a git
   that rejected the sentinel would quietly emit an unstamped bundle instead of an error.
3. **A sha this clone does not have falls back to reading from HEAD.** That is not an exotic case:
   a squash merge or a rebase leaves the recorded sha with no object behind it while the content it
   describes is perfectly current. Failing there would break the gate on the repositories that
   squash-merge, which is most of them.
4. **Both fallbacks are printed.** A run that read history from HEAD never claims to have read it
   as of anything else, and a run that read as of an older commit says which. Churn numbers on a
   page that describe a different commit than the reader's branch are only honest if the run says
   so — a reader comparing a page's `commits` attribute against `git log` would otherwise find it
   short by however many commits the branch has.

The default stays strict, with no `-as-of-bundle`. On the default branch signpost *writes* the
stamp, so something has to check that what it wrote is true. And per design §6.0.2 the flag stays a
flag: a repository that can weaken its own gate by committing a file is not gated.

## Consequences

The gate becomes a gate. Before this it was red on every conforming pull request, which is the
failure design §4.6 warns about arriving from the other direction — a check nobody can keep green
is a check somebody turns off, and the staleness guarantee goes with it. The three earlier pull
requests that passed did so by committing the bundle on the branch, so §8.0 was being violated in
practice to work around a defect in the check that enforces it.

`internal/vcs` gains an `AsOf` option and reports which commit it actually read as of, which is a
second field a caller has to look at rather than assume. `internal/okf` gains one exported
accessor, `RecordedCommit`, and it is the only one of its kind: every other fact about the bundle
is read *after* the analysis, which is why nothing else needs to reach in first.

A branch verify now reads slightly less history than the branch has, which is the point, and means
the churn attributes a pull-request run computes are not the ones the merge will produce. That is
correct — the bundle on `main` will be rebuilt at the merge commit and will get the newer numbers
then — but it means `verify -as-of-bundle` answers "does this bundle match the code" and not "what
will the next bundle say".
