# 7. The bundle names the commit it describes, and staleness is a content comparison

## Status

Accepted

## Context

[ADR 0005](0005-commit-the-bundle-to-the-repository.md) commits the bundle to the repository
and states that `verify` must fail loudly, because a silently stale knowledge artifact is
worse than none. It did not say what the bundle claims to describe, or what `verify`
compares. Implementing the pull-request check turned both into blocking questions, and the
obvious answer to each is wrong.

**Every page carries a provenance stamp, and it is part of the page's bytes.** A page's
frontmatter has `resource: git://host/repo@<sha>/<path>` and `generated.at: <date>`. That is
what makes a committed artifact auditable — a reader can see which commit it describes
without running anything. It also means the stamp is not metadata *about* the comparison;
it is inside the thing being compared.

**The obvious identity is HEAD, and HEAD cannot work.** Committing the bundle advances HEAD.
So a bundle stamped with HEAD names the commit *before* the one that carries it, forever: the
next run re-stamps, which is another commit, which moves HEAD again. The artifact never
converges, `verify` fails on every committed bundle, and the workflow commits on every push
in perpetuity — the commit churn ADR 0005 identifies as the fastest way to kill adoption.

**The obvious staleness check is "re-render and compare every byte", and it fails on every
pull request.** §8.0 forbids building the bundle on branches, precisely so two branches
cannot conflict inside `.signpost/`. The unavoidable consequence: on any branch the committed
stamp names an older commit *by construction*. Since the stamp is in the bytes, a strict
comparison reports every page out of date on every pull request — including one that changed
nothing but a typo in a documentation file. Measured on a real docs-only branch: exit 1, five
problems, and the only difference in the diff was the sha. A gate that is red on a typo is a
gate people switch off, and once it is off, the staleness check this tool is built around is
gone.

**The same problem blocks the single-developer pattern outright.** A developer who builds the
bundle locally and commits it alongside their code stamps the *parent* commit — the sha of
the commit carrying the stamp does not exist when the stamp is written. Measured: stamp
`c50cb5a`, commit created `916b5ab`. No flag fixes this; it is the order of events.

Two more elaborate designs were considered and rejected on evidence:

1. **Identify the bundle by a digest of its inputs** rather than by a commit, so provenance
   stops moving with history. Abandoned once testing showed the drift it was meant to absorb
   does not exist: history attributes are per-directory, so a documentation-only change does
   not move them. It would also have replaced an auditable sha — something a reader can look
   up — with a number that means nothing outside signpost.
2. **Prove the recorded sha is an ancestor of the branch** (`git merge-base --is-ancestor`)
   before trusting it. Abandoned because the manifest can only enter the tree through a
   commit, which makes it exactly as authoritative as the source being analysed: nothing
   proves the analysed `.go` files are the real ones either, and treating one file in the
   tree as suspect while trusting every other is incoherent. It would also false-positive on
   a squash merge or a rebase, where the recorded sha no longer exists but the content is
   perfectly current.

## Decision

**Two rules, one for each side of the question.**

**The bundle names the newest commit that changed something other than the bundle.** Not
HEAD. A commit whose only effect was rewriting `.signpost/` did not change the code being
described, so it does not move the identity. A commit that changed code *and* the bundle is a
code change and does move it. A repository containing nothing but a bundle falls back to
HEAD, since there is no earlier commit to prefer and an unstamped page would claim less than
the tool knows.

**Staleness is a comparison of content. Provenance is compared only where it is written.**
The default `verify` is strict: it compares the stamp against this tree, because on the
default branch signpost is the thing that *writes* the stamp, so something has to check that
what it wrote is true. Everywhere else — branches, pull requests, a locally built bundle
committed with its code — `verify -as-of-bundle` takes the two provenance fields from
`manifest.json` and compares content byte for byte against a fresh render.

`-as-of-bundle` is a flag rather than the default, and it announces itself in the run's
`skipped:` output naming the commit it judged against. This is the check whose quiet success
would destroy the tool's value, so a run that relaxes any part of it says so.

## Consequences

**The artifact converges, which is the property everything else rests on.** The workflow
commits once after a code change and then reports "unchanged" on every subsequent run.
Rehearsed against real git across five runs: one commit, two no-ops, a code change, one
commit, one no-op.

**The strict check earns its place by having caught something.** The identity was stamped one
commit off until a bug in the exclusion was found. Nothing in a content-only comparison would
have caught it, because both sides were wrong in the same way — this is exactly why the job
that writes the stamp does not get to relax the check on it.

**`-as-of-bundle` does not weaken the gate, and this is testable.** Only the two provenance
fields come from the manifest; a branch that changes what the map says still fails. Measured
on a branch adding a package: `modules/b.md: the repository has this concept and the bundle
has no page for it`. A missing, unparseable, or resource-less manifest adopts nothing and
leaves the strict comparison in place, which then reports it — a bundle that cannot say what
it describes has no provenance, and inventing some to make a gate pass is the false pass this
command exists to prevent.

**A wrong stamp can mislabel, but cannot hide.** The manifest is trusted for provenance
without being validated against git. A hand-edited stamp is therefore a reviewable diff in a
machine-generated file, and forging one changes which commit a page claims to describe — it
cannot conceal stale content, because the content comparison runs either way. Provenance is
read from `manifest.json` rather than from a page for the same reason: the manifest is the one
file in the bundle no human has a claim on, where every field is a machine record of the run
that wrote it.

**A locally built bundle committed atomically with its code has residual drift, and it is
inherent.** Under `-as-of-bundle` the stamp is fine, but history attributes for a directory
inside the pending commit change once it lands: `commits: 1 → 2`, a new `last_commit`,
`lines_added: 3 → 4`. You cannot record a commit's history before making it. Two honest
options, both verified: build with `-no-history` for a structure-only bundle that verifies
clean atomically, or commit the code first and the bundle second, which is what CI does and
which converges. This is documented as a boundary rather than papered over.

**Shallow clones produce a different bundle at the same commit.** Same commit, same binary,
`depth=1` versus full: `commits: 5 → 1`, `lines_added: 7 → 4`. The stamp is identical and the
content is not, so a bundle committed from a shallow clone records thinner numbers than the
repository has. `internal/vcs` already reports the shallowness with `fetch-depth: 0` as the
remedy; both workflow jobs set it. The check cannot detect this on its own — it is why the
report exists.

**The bundle directory name is now load-bearing in two packages.** `internal/vcs` must
exclude it to compute the identity, and it cannot import `internal/okf`, which reads the
graph `vcs` feeds. The constant is duplicated with a test that fails if the two ever
disagree; a silent rename would otherwise leave the exclusion pointing at nothing and stop
convergence without any test failing.

Design reference: [docs/design.md](../design.md) §4.6, §8.0, §8.1.
