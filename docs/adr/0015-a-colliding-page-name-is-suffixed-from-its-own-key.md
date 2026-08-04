# 15. A colliding page name is suffixed from its own key, not from its position

## Status

Accepted

## Context

A page's filename is its node ID, and every other page in the bundle links to it by that ID
([ADR 0003](0003-directory-granularity-for-module-nodes.md)). The bundle is committed
([ADR 0005](0005-commit-the-bundle-to-the-repository.md)). Those two together make a page name a
public contract in the most literal sense: it appears in the working tree of every repository
that has adopted signpost, and in the diff of every commit that changes it.

Names collide. `slug` is deliberately lossy — the result is a filename checked out on
case-insensitive filesystems, so `Auth` and `auth` are the same file — and a polyglot repository
has several directories called `src`. `testdata/corpus` has four, plus two called `api` and two
called `greeter`: six collisions in fifty files, which is ordinary rather than pathological.
Something must disambiguate them, because `AddNode` treats a repeated ID as the same node, so a
collision left alone is one page claiming to describe two directories — a wrong graph, not an ugly
one.

The original rule was a counter in path order: `src`, `src-2`, `src-3`, `src-4`. It is correct —
every ID is unique — and its cost is entirely in the diff.

**A page's name depended on how many same-named directories sorted ahead of it.** Measured, not
assumed. In a tree with `a/auth`, `b/auth` and `c/a-u-t-h`, adding an unrelated `aa/auth` renamed
`b/auth`'s page from `auth-2` to `auth-3`, and deleting `a/auth` renamed it again to `auth`. Twice,
for a directory that had not changed. Adding a directory that *didn't* collide moved nothing, which
locates the cause precisely: the churn is within the colliding group, and adding a member to a
group of four rewrites the pages of the three that follow it. `ZT-duo-cc-plugins`, a repository
with a bundle already, carries a `tests-2.md` whose name is decided this way.

What that costs is not disk. A renamed page is that file, plus every page linking to it, rewritten
in a commit that need not have touched the directory. Nothing in the graph is wrong afterwards —
which is exactly the problem. `verify` passes, the tests pass, and the symptom is a reviewer
looking at forty changed pages for a one-directory change with no way to tell which of them mean
something. The bundle's whole claim is that it is reviewable; a rename storm is the failure mode
that makes people stop reading the diff.

Three candidates were considered.

1. **Keep the counter, and accept the churn.** Cheapest, and it is what shipped in v0.1.0. It
   fails on the one property the bundle is committed *for*: a diff a human will read.

2. **Suffix from the entry's own key.** `src-1slg0rn`, where the suffix is a hash of
   `/modules/rust/src`. An entry's ID then depends on its own key and on nothing else about the
   run. Tried first, and **not sufficient on its own**: it still needs a rule for who gets the bare
   readable `src`, and first-come — whoever the walk sees first — means a newcomer sorting ahead of
   the incumbent *takes the name off it*. Adding a `src` to a repository that has two is an ordinary
   edit, so under a first-come rule roughly half of those additions rename somebody else's page.
   That was caught by a corpus test failing on `rust/src moved from modules/src.md to
   modules/src-1slg0rn.md`, not by reasoning.

3. **Suffix every name whether it collides or not.** `signpost-1f4ka9` in a repository with nothing
   else called `signpost`. Perfectly stable and it makes every page in every bundle unreadable, to
   buy stability in a case that requires a directory to be deleted.

## Decision

**A colliding page name is suffixed with a short hash of the entry's own key. Whether a name is
shared is decided before any name is assigned, and a shared name is suffixed for every member of
the group including the first.**

Concretely, `internal/assemble`:

- `ids.reserve(prefix, names)` counts the short names of exactly the entries that will be
  assigned. A name more than one entry wants is *shared*.
- `ids.assign(prefix, key, name)` gives an entry the bare `prefix+slug(name)` only if that name is
  not shared; otherwise `prefix + slug + "-" + keyHash(prefix+key)`.
- `keyHash` is fnv32a in base36 — lowercase alphanumerics, the whole alphabet a
  case-insensitive filesystem leaves safe.
- Reservation happens in one pass in `builder.run`, because a prefix can have more than one
  source: an external dependency and an ADR both become a page under `/references/`, and a
  collision between the two is no different from one within either.

Two rules follow from the reservation being a count of *entries*, and both were bugs before they
were rules. Services and data stores are folded by name before assignment — one node per service
across every compose file — so the same name in two files is one entry, not a collision; counting
occurrences suffixed every service declared twice. And an entry the caller skips must be skipped
here too: a name counted for something that never gets a page suffixes the page that does, for a
collision that does not exist. That is why the npm-workspace-sibling filter is a shared `externals`
helper rather than duplicated at both call sites.

Reservation is optional, deliberately. An unreserved entry still gets a unique ID — `assign` falls
back to checking what it has already used — so a caller that forgets loses stability, not
correctness.

## Consequences

**Every adopted bundle with a colliding page renames those pages once.** `src-2.md` becomes
`src-1slg0rn.md`. This is a breaking change to a public contract, taken once, in exchange for the
churn stopping afterwards. A bundle with no collisions is byte-identical, which includes
signpost's own — so this repository's own dogfooding could not have detected either the defect or
the fix, and the corpus is what does.

**A suffixed name is less readable than a numbered one.** `src-1slg0rn.md` says less to a person
scanning a directory listing than `src-2.md` did. Bounded to pages that actually collide, which is
the trade: readability of the colliding minority for a diff the majority can review.

**32 bits collides, and the counter survives for that case only.** A brute-force search found
`p46047/auth` and `p540990/auth` both hashing to `1qhazc2`, so a repository somewhere around a
hundred thousand same-named directories can reach it. A shared ID is one page describing two
directories, so uniqueness wins over stability where the two conflict: `assign` appends `-2`,
`-3` after the hash. `TestTwoDirectoriesWhoseKeysHashAlikeStillGetDistinctIDs` uses the found
pair, with a guard that fails if the collision ever stops being one.

**One residual remains, recorded rather than claimed away.** A name is suffixed because more than
one thing wants it, so when a collision group shrinks to a single member that member stops needing
its suffix and moves to the bare name. Deleting a directory therefore renames one page in its own
group — bounded to that group, not the bundle — and
`TestDeletingTheBareNameHolderMovesOnlyItsOwnGroup` exists so a reader who finds a moved page
finds the reason in the suite rather than deducing it. Closing it means candidate 3 above, which
costs every page in every repository.

**The guarantee is asserted at two levels, because they catch different things.**
`TestCorpusPageNamesSurviveAnUnrelatedEdit` rebuilds in process on all three platforms, so a
filesystem that folds case differently shows up there. The `corpus` job's *A committed bundle's
page names survive an unrelated edit* step commits the bundle first and asserts `git status`
reports no deletion, which is the reviewer-facing cost and the thing no in-process test can
measure: a rename is a delete plus an add of a path never seen before.

## Notes

The defect is reproducible: build a tree with `a/auth`, `b/auth` and `c/a-u-t-h`, note
`b/auth`'s page name, add `aa/auth`, rebuild. Under the old rule the name moves.

Design reference: [docs/design.md](../design.md) §3 for the bundle layout the IDs are also the
namespace of.
