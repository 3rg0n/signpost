# 34. A deterministic pass may not produce an ambiguous edge

## Status

Accepted

## Context

[ADR 0032](0032-order-is-drawn-only-where-a-file-declares-it.md) closes by naming the next
question rather than answering it: data-access edges are the nearest reachable step after CI
ordering, "because whether two writers of one table may be linked at `Ambiguous` confidence is
its own decision." This is that decision, and it has to be made before any code reads a SQL
statement, because it decides what the reader is allowed to emit.

The situation is concrete. `graph.KindDataStore` has shipped since v0.1 and
`assemble.addData` creates one node per table a migration touches — `/data/things`, with the
migration history that changed it. Nothing points at those nodes. They are reachable from the
index and from nowhere else: no module links to a table, so a reader who opens `/data/orders`
learns the schema's history and cannot learn which code writes to it. That is the half of the
map an agent most needs for the symptoms that span components, and it is the gap the pointer
sentence in issue #43 is blocked on.

`Ambiguous` is the third of three confidences and is the only one nothing produces. `Extracted`
is read out of source or a manifest. `Inferred` is a model's, and requires a grounding citation.
`Ambiguous` is documented in `graph.go` as "model output the model itself flagged as uncertain"
— and it is declared, ranked by `confRank`, styled by the DOT and Mermaid exporters, counted in
the OKF manifest, and emitted by no pass in the repository. It exists as a slot.

So when a deterministic reader finds `INSERT INTO orders` inside a string literal in
`internal/store/orders.go`, there is a real temptation available: the module clearly touches the
table, the string might be assembled at run time, the table name might be interpolated, the
statement might sit in a comment-like block the scanner mishandled — and `Ambiguous` is sitting
there unused, apparently meaning "probably, but check." Taking it would let the pass emit
everything it noticed and let the reader sort it out.

That reading is wrong, and it is wrong in a way that costs more than the edges are worth.

## Decision

**`Ambiguous` is reserved for a model that flags its own output as uncertain. A deterministic
pass may not produce it.** The rule is now the doc comment's, not just its description: a pass
that reads the tree either draws `Extracted` or draws nothing.

The reason is that `Ambiguous` is not a confidence in the ordinary sense — it is an *attribution
of doubt to a specific author*. `Inferred` says a model concluded this and cited its grounding;
`Ambiguous` says the same model looked at its own conclusion and declined to stand behind it.
Both are statements about a reasoning process that happened. A deterministic pass has no such
process to describe. When it "is not sure", what is actually true is that its rule does not cover
the case — which is a fact about signpost's extractor, not about the repository. Recording it as
an edge relabels a limitation of this tool as a property of somebody else's code.

The practical failure follows from that. An `Ambiguous` edge is unfalsifiable by review. A human
correcting the bundle can confirm or delete an `Extracted` edge, because it claims a file says
something and the file either does or does not. There is nothing to check about an edge that
claims only that signpost was unsure — deleting it asserts nothing and keeping it asserts
nothing, and §6.1's whole mechanism depends on a reviewer being able to act on what they read.
Worse, `confRank` orders the three, so `AddEdge` merges an `Ambiguous` edge into an `Extracted`
one silently: a guess between two nodes that also have a real relationship disappears into it,
and a guess between two that do not is the only one that survives to be seen.

**A deterministic pass reports what it could not resolve instead.** `Result.Unresolved` and
`Result.Unlinked` already exist for exactly this, and §4.4's third property already requires it:
"an import that resolved to nothing is counted, not dropped silently." A table name signpost
cannot resolve is the same class of fact as an import specifier it cannot place, and it goes to
the same place — a count a reader can act on, not an edge a reader cannot.

**Two writers of one table are therefore each linked to the table, and not to each other.** The
question ADR 0032 left open, answered: `orders.go` and `reconcile.py` both get an edge to
`/data/orders` at `Extracted` confidence, because each one's own source states the table it
writes. Neither gets an edge to the other. A module-to-module edge would assert a coupling no
file declares — the same refusal `EdgePrecedes` makes for two CI jobs with no `needs` between
them — and the shared table is where the coupling is legible anyway: both writers are one hop
from it, on its page, which is the page a reader with a data symptom opens.

**A statement whose target is not a literal name draws no edge.** `"INSERT INTO " + table` and
`fmt.Sprintf("DELETE FROM %s", t)` name no table in the tree. The table is whatever the caller
passes, and resolving it needs the call graph
[ADR 0022](0022-extractors-are-hand-written-and-tree-sitter-has-a-threshold.md) says this project
does not have. These are counted, not guessed at, and counted separately from a literal name that
matched no known table — the two have different remedies.

**A table signpost has never seen in a migration gets no node.** `addData` builds the data nodes
from migrations, and a source-derived writer edge points at what is already there. A repository
whose schema lives outside the tree — managed by a console, or by a framework's model classes —
therefore gets writer edges for the tables its migrations declare and nothing for the rest, with
the remainder counted. Creating a table node from a query would let a typo in a string literal
mint a page for a table that does not exist, and a table page is exactly the kind of durable
concept §4.1 wants to be true.

## Consequences

**The unused third confidence stays unused, and that is now a decision rather than an
oversight.** Anyone finding `Ambiguous` declared and unproduced would reasonably read it as
something half-built and reach for it. This ADR is what they should find instead. It stays
declared because the semantic pass (§4.5) is where it becomes producible, and because the
exporters and the manifest already handle it — removing it would be a schema change to buy
nothing.

**A reader gets fewer edges than the maximal reading would give them, and every edge they get is
checkable.** This is the trade, stated plainly: a table written through a helper that takes the
name as a parameter will not show that writer, and the coverage report will say how many such
statements there were. A bundle that is right about less is worth more than one that is
approximately right about everything, because the first can be corrected and the second cannot be
audited.

**The refusal needs its own fixtures, positive and negative.** A pass that draws an edge for a
literal table name and declines an interpolated one is two behaviours, and a corpus that only
holds the first would pass just as well against a pass that draws both. So the corpus carries
statements on both sides of the line in every language that reads SQL, and the count is asserted
rather than only the presence — an assertion that something appears cannot fail when something
extra appears beside it.

**`Unresolved` gains a second population and its report has to stay legible.** It has meant
"import specifier" up to now, and a reader seeing `orders` in that list needs to know it is a
table. Kept as distinct counters for that reason rather than one merged number: an unplaceable
import and an unplaceable table have nothing to do with each other and are fixed differently.

Design reference: [docs/design.md](../design.md) §4.1, §4.4, §4.5, §6.1.
