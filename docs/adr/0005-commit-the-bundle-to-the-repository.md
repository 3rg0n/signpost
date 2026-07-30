# 5. Commit the bundle to the repository

## Status

Accepted

## Context

signpost derives a knowledge artifact from a repository. Where that artifact *lives* is the
decision that shapes almost everything else about the tool, because it determines who can
read it, whether corrections survive, and what "correct output" even means.

**The problem being solved is repeated rediscovery.** An agent opening an unfamiliar
repository works out which module owns what, where the entrypoints are, which files move
together, and where the docs disagree with the code. That work is paid for on every
session, discarded at the end of every session, and inconsistent between runs. Any solution
that puts the derived knowledge somewhere the next reader will not look has not solved it.

**The adoption constraint is harder than the technical one.** A knowledge tool that
requires installing something to read its output will be read by the people who installed
it and nobody else. The tech lead who would most benefit from seeing the module graph is
the one least likely to install a CLI to see it.

**And corrections are the whole value over time.** A generated artifact is wrong in places
only a human can fix. If those fixes cannot be made in the artifact itself, they are made
nowhere — and a generator that clobbers corrections teaches people to stop making them,
after which the artifact only decays.

Three options were considered.

1. **A separate service or database.** Central, queryable, always current, and it fails
   every constraint above: it needs to be run, it needs credentials, it is invisible from
   the repository, and a fork or an air-gapped checkout has nothing. It also makes the
   knowledge outlive the code it describes, which is worse than useless — stale knowledge
   nobody can see is confidently wrong knowledge.
2. **Generated on demand, into a cache.** No repository churn, always matches the working
   tree, and it means every reader pays the derivation cost, needs the binary installed,
   and gets no shared artifact to review or correct. Human edits have nowhere to live at
   all.
3. **Committed to the repository as markdown**, generated in CI at a known commit.

## Decision

**The bundle is committed to the repository it describes**, as Open Knowledge Format
markdown under `.signpost/`, generated in CI on push to the default branch.

The consequences are the point, not side effects:

- **Useful with signpost uninstalled.** It is markdown in the repo. Agents read it, people
  read it, GitHub renders it — including the Mermaid graphs, so a tech lead clicks
  `.signpost/index.md` and sees the module graph with nothing installed and no site
  deployed. The benefit accrues to everyone who has the repository, not to everyone who has
  the tool.
- **Corrections live where the artifact does.** Generated prose sits between managed
  markers; everything outside them is the author's and is never touched. A correction is a
  normal pull request against a normal markdown file, reviewed the way everything else is.
- **It is versioned with the code.** The artifact for any commit is the artifact that was
  correct at that commit, available by checking it out. No separate history to reconcile.

## Consequences

**Determinism becomes a correctness property rather than a nicety.** This is the largest
consequence and it reaches into every package. Because CI commits the output, a run
producing different bytes for the same input is commit churn in someone else's repository
on every build — and commit churn kills adoption faster than any missing feature. So:
sorted iteration everywhere and no map-order dependence; clustering deterministic by
construction, with no seeds and no randomised restarts; temperature 0 on every model call;
semantic output cached by content hash so unchanged input is never regenerated and
therefore cannot drift. Byte-stability is asserted in `verify` and in CI, which renders
every export format twice and compares bytes.

**Staleness has to fail loudly, so `verify` exists.** A committed artifact can be older
than the code, and a silently stale knowledge artifact is worse than none — it is
confidently wrong, and it looks authoritative because it is checked in. `verify` exits
non-zero and runs on pull requests.

**Merge conflicts are a first-order design concern.** Committing generated files to a
repository with parallel branches has the obvious consequence, and a knowledge tool that
makes merges painful gets deleted. Three decisions handle it, in order of how much they
matter: the bundle is not built on branches at all, so the common case produces no conflict
(the PR check *verifies*; it does not write); the bundle is many small pages rather than one
graph blob, so two branches that genuinely both regenerate it collide only on the pages they
both changed, and a markdown page with sorted frontmatter is something a human can resolve
by reading it; and regeneration at the merge commit is the always-available tiebreaker for
any generated region, because the deterministic pass is a pure function of the tree. The one
thing that must never be auto-resolved is a conflict inside a human region.

No custom git merge driver ships in v0.1, deliberately: a driver requires every contributor
to activate it locally, and an unconfigured contributor silently gets default behaviour — a
fragile place to put correctness. It remains the answer if real usage produces pain the
above does not cover.

**The repository grows, and its history carries the artifact.** That cost is accepted: the
bundle is small markdown, and the diff is the point — a reviewer can see the knowledge
change alongside the code change that caused it.

**A loop guard is mandatory**, since a workflow that commits to the repository it is
triggered by will otherwise trigger itself. Three guards, all three: `paths-ignore` on
`.signpost/**`, a skip when the actor is the bot, and `[skip ci]` on the bot commit.

**Generated output is markdown and JSON only. Nothing executable.** A file that CI writes
into a repository on every push is a supply-chain position in every repository that adopts
signpost; the worst a hostile analysed repo can do is put ugly strings in a `.md` file.
Every JS dependency lives in the separate viewer repository
([ADR 0006](0006-generator-and-viewer-are-separate-repositories.md)).

Design reference: [docs/design.md](../design.md) §3, §8.0, §8.1.
