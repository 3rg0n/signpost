# 30. A finding states its own absence, and the index carries it

## Status

Accepted

## Context

Design §7.1 names five structural findings — hubs, cycles, bridges, orphans, doc/code islands
— and says they are "written as **text** in `index.md`, because that is what an agent
consumes." One of the five shipped. `index.md` carried "Most connected" and nothing else, and
grepping this repository's own committed bundle for `cycle`, `bridge`, `disconnected`, or
`orphan` returned nothing.

The analysis was never missing. `signpost graph show` computes all four of the absent ones —
`g.Cycles()`, `g.Bridges()`, `g.Components()`, `g.Orphans()` — and prints them. So the finding
existed, reached a terminal, and stopped there. For a person running the command that is
useful; for an agent starting cold on a checkout it is nowhere, and the bundle is the only
artifact that is committed, reviewed, and durable. Whatever is not in it is not part of what
this repository knows about itself.

Two design questions had to be settled before writing any of it, and both had a wrong answer
that looked reasonable.

**Where it goes.** `index.md` was, until this change, a table of contents: every line named a
page. A findings section is the first content on it that is not navigation, and the obvious
alternative was a `findings.md` beside it, keeping the index clean at the cost of one more
file. §7.1 already answers this — `index.md`, by name — and the reason it is the better answer
and not just the written one is that an agent reads the index first and may read nothing else.
A finding on a page it has to be told to open is a finding it does not have. The cost is real
and accepted: the index is now two things, so its opening line says so.

**What an empty finding renders as.** The four CLI writers each `return` on a zero count, so a
clean repository prints no cycle section at all. Copying that into the bundle is the failure
mode worth naming, because it is silent: a section that vanishes when the answer is "none" is
byte-for-byte indistinguishable from a section a build failed to write, and a reader cannot
tell a clean repository from a broken generator without running the tool — which is the thing
the bundle exists to spare them.

## Decision

**Every finding is stated, whether or not it found anything.** "Import cycles: none." is
written down, as the result it is. This is deliberately the opposite of what `graph show` does,
and the asymmetry is the decision: a terminal is scrolled and a committed file is read, so
brevity wins in one place and completeness in the other.

**The findings live in a `### Structural findings` section of `index.md`,** between "Most
connected" and the page listing, inside the `index` managed region so a reader's `## Notes`
stays untouched. Four lines: import cycles, cross-cluster edges, disconnected islands,
unconnected concepts. Each names the concepts involved and links to their pages, through
`relTarget`/`proseLink` like every other link in the bundle — a root-absolute target 404s on
GitHub, which ADR 0005 names as the whole point of committing the bundle.

**`indexFindings` calls `Clusters()` itself rather than requiring a caller to have done it.**
`Bridges()` reads the cluster assignment `Clusters()` writes and returns an empty slice when it
has not run, so an emitter that assumed the ordering would write "Cross-cluster edges: none."
over a graph full of them. A missing finding is a gap; a confident wrong finding is worse, and
nothing in `bundle.go` could show the dependency. `Clusters()` is deterministic and idempotent,
so the fix costs one traversal and removes an ordering hazard.

**A single-node component is an orphan and is reported once.** `Components()` returns them, and
counting them as islands too would report one absence as two different problems. Islands are
components of size greater than one, excluding the largest — the body everything else is
measured against.

**Cluster numbers are not printed, though `graph show` prints them.** They are dense indices
with meaning only inside one run's partition, and no page in the bundle is named after one, so
they would ask a reader to resolve a reference the bundle does not hold. That an edge crosses a
boundary at all is the finding.

**Two bounds, not one.** A finding's list of lines is capped at 20; the concepts named *within*
one line are capped at 8. The second bound exists because an eleven-module cycle rendered
inline wraps into a paragraph, and this repository's own 30 unconnected concepts rendered as
one line came to about four thousand characters. Both bounds state the full count first and
count the overflow, so a truncated list never reads as a complete one.

**Nothing is stated for an empty graph.** Every line above would otherwise read as a clean
bill of health for a repository the run never looked at, which is §4.2's rule: unmeasured must
not render as measured.

## Consequences

**Every adopter's next `signpost build` produces a diff in `.signpost/index.md` that nobody
asked for.** On this repository it is 51 added lines. That is the cost of the bundle's shape
being a public contract, and it is the reason this is an ADR rather than a comment: the change
is not reversible without producing the same unasked-for diff a second time.

**A repository with genuine findings gets a longer index, and one with none gets four extra
lines.** The four-line floor is the price of the absence rule, paid by exactly the
repositories that have the least to read. It is worth it because those readers are the ones who
cannot otherwise distinguish "clean" from "not generated."

**`graph show` and the bundle now say the same things with different words** — "import cycles"
in both, but a terminal line and a markdown bullet. They are two renderings of one analysis and
will drift if either is edited alone. Not unified into a shared renderer: one writes columns
for a terminal and the other writes prose with links, and the shared part is the metric
functions, which already are shared. #41 will lift the CLI's own caps, and that is where the
two should be compared again.

**The findings are computed on every build, including the cluster pass.** `Clusters()` was
already called by `manifestJSON`, so on the `build` path this is the same traversal reached
from a second place rather than a new cost. A caller that assembles a graph and renders the
index without the CLI now gets clustering it did not ask for, which is the price of not
depending on call order.

Design reference: [docs/design.md](../design.md) §7.1, §4.2.
