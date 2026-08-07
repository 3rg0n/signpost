# 19. Clustering is hand-written Louvain, after label propagation was measured and rejected

## Status

Accepted

## Context

Clustering answers "what belongs together" for a reader arriving at a repository they have
never seen. Three things consume the partition, and none of them is load-bearing on its own:
`manifest.json` reports a cluster count as a one-number summary of whether the repository has
structure at all; `Bridges` reports the edges crossing a cluster boundary, which is where a
change is most likely to surprise because the two sides are maintained as separate concerns and
coupled anyway; and the DOT and Mermaid exports draw each cluster as a subgraph box, because a
flat diagram of forty nodes is unreadable. The GraphML and JSON exports carry each node's cluster
as a plain attribute and leave the grouping to whatever reads them. A reader who disagrees with a
grouping loses a box on a diagram.

That modest role is why label propagation was written first. LPA is a third the code — assign
every node its own label, then repeatedly set each node's label to the most common one among its
neighbours until nothing changes. The reasoning was that undemanding output means clustering
quality would not show.

It showed immediately, on the smallest graph anybody would draw by hand: two triangles joined by
one edge. Synchronous propagation with a lowest-label tie-break puts every node in one community.
Each round, the lowest label present in a neighbourhood wins the tie, so the minimum label floods
outward one hop at a time until it reaches every node the graph connects. This is LPA's
documented giant-community failure mode, and it is not a tuning problem: it follows from the
tie-break, and the tie-break is what makes the algorithm deterministic. Asynchronous updating in
a random node order avoids the flood and gives a different partition per run, which is
disqualifying here for a separate reason — the bundle is committed, so a partition that changes
without the code changing is commit churn in somebody else's repository
([ADR 0005](0005-commit-the-bundle-to-the-repository.md)).

One community containing the whole repository is worse than no clustering. It reports a cluster
count of one for every repository, leaves `Bridges` permanently empty — since no edge crosses a
boundary that does not exist — and draws every diagram as a single box, all while asserting a
structure the repository does not have. Three consumers, each independently useless, is what a
partition nobody depends on individually turns out to cost.

The library route was considered and rejected on the same grounds every dependency decision
here is made on. Leiden via graspologic, or NetworkX's community module, each pull a numeric
stack — and in graspologic's case a JIT compiler — whose CVEs signpost would inherit with no
direct remediation path. [ADR 0002](0002-patchable-dependencies-not-zero-dependencies.md) sets
the test: a dependency has to be one we can patch ourselves the day something lands in it. A
JIT compiler underneath a graph-partitioning convenience does not pass, and
[ADR 0014](0014-adopt-the-otel-sdk-and-write-the-exporter.md)'s three modules are the standard
for what clears the bar.

## Decision

**Clustering is Louvain modularity optimisation, hand-written in `internal/graph/louvain.go`,
just under 240 lines including its comments.** It is roughly 150 lines more arithmetic than the
label propagation it replaced, and far less than the dependency it avoids.

**Determinism comes from structure, not from a seed.** There is no seed, no randomised restart,
and no shuffle anywhere in it. Four properties produce the guarantee, and each is load-bearing
rather than incidental:

- nodes are processed in sorted-ID order, which the caller supplies
- candidate communities are evaluated in ascending community index
- a gain tie breaks toward the lowest community index, by comparing with strict `>`
- aggregation preserves index order, so pass N+1 inherits pass N's ordering

**The final numbering is by each cluster's lowest-sorting member,** not by the label values the
optimisation happened to settle on. Internal labels are an implementation detail; a cluster ID
in output is derived from the tree, so adding a module cannot renumber unrelated clusters.

**Edges are unweighted for clustering purposes: each counts 1.** `Edge.Weight` carries
multiplicity, and it is carried for reporting, where "these two modules changed together ninety
times" is the interesting fact. For coupling, whether a relationship exists is the better
signal than how many times it was observed — an import counted once per call site would make
the most-used utility in the repository look like the centre of a subsystem.

**The failure that caused this decision is a test, not a comment.**
`TestClustersSeparateDenseGroups` in `internal/graph/graph_test.go` is the two-triangles graph
with the one bridge, and it asserts two clusters with each triangle whole. LPA fails it.
`TestClustersAreDeterministic` builds the same graph twenty-five times in one process, because
Go randomises map iteration per run within a process, and compares the full partition.

## Consequences

**We own a modularity optimiser, including its bugs.** It is arithmetic over integer indices
with no external contract to check it against, so a mistake in the gain calculation produces
plausible-looking clusters rather than an error. What guards it is the shape of the tests: an
assertion that two groups stay separate is falsifiable in a way "modularity is 0.41" is not. No
modularity score is emitted anywhere, deliberately — a number nobody can act on invites tuning
toward it.

**Determinism is a property of four separate details, and any one of them can be lost by a
reasonable-looking edit.** Iterating a map instead of a sorted slice, or breaking a tie with
`>=`, changes the output without changing the behaviour anybody was editing for. The
twenty-five-run test catches the map case within a process; it cannot catch a platform-dependent
sort, which is why the committed bundle and CI's re-analysis of this repository are the real
check.

**Startup cost is a full multi-pass optimisation on every build.** It has not been the bottleneck
— extraction is — and no result is cached, since a cache would need invalidating against the tree
and that is a worse problem than recomputing.

**Clustering runs over every edge kind, including the co-change edges git history contributes**
([ADR 0020](0020-git-history-annotates-the-map-and-never-draws-it.md)). Two modules that always
change together will tend to land in one cluster even with no import between them, which is the
intent: that coupling is real and no static read finds it. The cost is that a repository with a
formatter rollout in its recent history has slightly noisier headings, bounded by the caps in
`internal/vcs`.

**A better partition is not a reason to change this.** Leiden fixes a real defect in Louvain —
it can produce internally disconnected communities — and that defect has no consequence for a
heading in a Markdown file. Replacing this supersedes the ADR; tuning it for modularity score
does not clear that bar.
