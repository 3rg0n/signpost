# 20. Git history annotates the map and never draws it

## Status

Accepted

## Context

signpost reads `git log` for co-change pairs, churn per directory, author concentration, and
first- and last-touch dates. Co-change is the cheapest coupling signal there is, and it is the
only one that finds coupling no static read can: a handler and the migration it depends on, a
proto and its generated client, a config key and the code that reads it. None of those is an
import, and all of them matter to an agent about to change one side.

The temptation history creates is to let it decide what the map contains, and every form of that
is plausible on its face:

- **A directory with history but no source becomes a node.** `internal/legacy` has thirty
  commits and five hundred deletions and is gone. Reading the log finds it; the tree does not.
- **Churn creates a "hotspot" concept with its own page,** aggregating the twenty highest-churn
  directories into something a reader can open.
- **Author concentration creates an ownership node,** so a reader can find who to ask.
- **A co-change edge names a file as its source,** because every other edge in the graph does.

The last one is the tell. An import edge cites the line that declares the import, so anybody can
open the file and check it. A co-change edge has no such line — its evidence is a set of commits,
and naming a file would attribute the claim to a place that does not make it. That difference is
not cosmetic; it is the whole distinction between what the tree says and what the log says about
the tree, and once it is blurred the bundle stops being checkable against the checkout.

The other force is that history is the least reliable input signpost has, and reliably so. git
may be absent. The directory may not be a repository — a tarball is a supported best-effort case
([ADR 0007](0007-the-bundle-names-the-commit-it-describes.md) makes the commit stamp conditional
for exactly this). The clone may be shallow, which yields thin signals that look like real ones.
The history may be a single squashed import commit. A repository whose map depends on any of
that produces a different map on a CI runner doing a shallow fetch than on a developer's full
clone, and neither the tree nor the bundle would say why.

## Decision

**History annotates nodes the structural pass already created. It never creates a node, never
creates a page, and never removes anything.** `addHistory` in `internal/assemble` runs after
every structural pass and iterates the module nodes that exist, looking each one up in the
history. A directory the history knows about and the tree does not is skipped: deleted code
still has history, and a node for it is a page about something that is not there.

**Churn, dates, and author concentration are attributes, not concepts.** `commits`,
`lines_added`, `lines_removed`, `first_commit`, `last_commit`, `top_author`,
`top_author_share`. This is the same call [ADR 0003](0003-directory-granularity-for-module-nodes.md)
makes about granularity and the same one CODEOWNERS ownership gets: churn is a property of a
module, not something deserving a page. Only module nodes are annotated — an external dependency
has no directory here, and a service or interface node is named by a manifest whose own churn
says nothing about the code implementing it.

**Co-change is the one thing history contributes to the graph's shape, it is an edge and never a
node, and it can only connect modules that already exist.** A pair naming a directory no module
covers is dropped rather than attached to the nearest ancestor or to the root, which would make
every docs-only commit look like coupling to the repository itself.

**A co-change edge is `extracted`, and it names no source.** What is extracted is the fact that
two directories appeared in the same commits N times, which is read from git rather than guessed
— [ADR 0004](0004-confidence-is-a-first-class-field.md)'s distinction, applied to a fact whose
evidence is not a file. `Weight` carries N so a consumer can weigh a pair that changed together
three times against one that did so ninety. `Source` is left empty deliberately.

**Absence is reported and never fatal.** No git, no repository, an empty history, a shallow
clone: each is a fact recorded in the bundle, not an error. `Available` gates both passes, so
unavailable signals contribute nothing even when the struct carries data. What is not acceptable
is silence — "no co-change found" and "no history to look at" are different claims and only one
of them is about the code.

**History is bounded, because a repository is untrusted input and history is the one input with
no natural size limit.** Commits are capped (`-max-commits`), and a commit touching an
implausible number of directories contributes churn but no co-change: a dependency bump, a
licence-header sweep, or an initial import would otherwise relate every directory to every
other, which is both the densest possible graph and the least informative one.

## Consequences

**Every module page is complete without git, and slightly thinner.** A tarball gets the same
pages under the same names with no churn attributes and no co-change edges. That is the intended
degradation: saying less rather than guessing, and the reason `verify` can report its staleness
check as skipped and still exit zero.

**Deleted code is invisible, and that is a real loss.** A reader who wants to know that
`internal/legacy` existed until March will not learn it here. The bundle describes the tree at a
commit; the log is how you find what is not in it, and `git log` is better at that than a
generated page would be.

**The rule is one line of ordering in `Build` and nothing enforces it structurally.** `addHistory`
and `addCoChangeEdges` run last, and both would work — wrongly — if they called `AddNode`. What
notices is `TestHistoryCreatesNoNodes` in `internal/assemble/history_test.go`, which hands the
builder a history for `internal/deleted` against a tree that has no such directory and asserts
no node appears. `TestUnavailableHistoryContributesNothing` is its negative half: signals marked
unavailable but deliberately populated must produce neither the attribute nor the edge, so it is
`Available` that gates the passes and not emptiness.

**Co-change edges do shape the clustering,** since `Clusters` runs over every edge kind
([ADR 0019](0019-louvain-over-label-propagation.md)). Two modules that always change together
tend to land in one cluster with no import between them, which is the intent, and it means the
"annotation only" rule is about nodes and pages rather than about influence. A grouping can come
from the log. A heading cannot.

**Folding directory pairs onto module pairs takes the maximum, not the sum.**
`internal/auth/testdata <-> internal/db` and `internal/auth <-> internal/db` both resolve to one
module pair, and one commit can appear in both, so summing produces a weight larger than the
number of commits that touched both modules. The maximum is a true lower bound on the real count;
a sum is not a bound at all.

**Author concentration is in the bundle, which is a person's name in a committed file.** It is
one attribute naming the highest-committing author and a share rounded to a whole percent —
enough to answer "who should review this", not a per-author breakdown. The rounding is also why a
single commit landing does not rewrite a decimal in a committed file.
