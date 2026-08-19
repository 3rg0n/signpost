# 37. The landing page is gated on its verdicts, not its words

## Status

Accepted

## Context

`site/index.html` makes two sets of claims about this tool. A status table names 25 components
and states whether each has shipped. A pasted run of `signpost graph show .` reports what the
tool measures on this repository, including the lines where it says what it could not read.

Nothing checked either one. No Go file reads `site/index.html`, no test parses it, and the
`pages` workflow publishes whatever the file says — it checks that `graph.json` carries at least
one node and that `site/CNAME` will survive the deploy, and nothing about the page's text. Both
sets of claims then went wrong within two days:

- The status table listed the semantic pass at `v0.2` and called v0.1 in progress. The README's
  table, and the `[0.1.0]` changelog section, both recorded `build -semantic`, the model
  backends, and seven other commands as released (#63).
- The pasted run reported 166 analysed files, 49 nodes, and 87 edges, against a tree that
  measures 258, 90, and 224. The sentence beside it opened "The two dim lines are the gaps"
  while the paste showed four (#64).

This is the failure the tool exists to prevent, in the one committed artifact here that the tool
does not check. [ADR 0005](0005-commit-the-bundle-to-the-repository.md) commits the bundle so it
is useful without signpost installed, and `verify` exists so that a committed document cannot
quietly stop describing its tree. The landing page is a committed document describing the same
tree, with no such gate.

Three alternatives lost.

**Row-for-row string parity.** Require each site row's label and state to equal the README's.
Not satisfiable without rewriting copy. The site words its labels differently on purpose —
`Export: Mermaid, DOT, GraphML, JSON` against `Mermaid / DOT / GraphML / JSON export`,
`Extractors: Go, TS/JS, …` against `Language extractors (Go, TS/JS, …)`, and four more. Two rows
carry links in different syntaxes. The gate would make the README's prose dictate the landing
page's copy, and no label was ever what went wrong.

**Generate the table from the README.** Drift becomes impossible, at the cost of making a
hand-written page partly machine-owned: a managed region in the HTML, a generator, and a CI
check that the committed file equals the generator's output — without which the same drift moves
up one level. It also discards the site's own wording, like the first alternative, for 25 rows
that change a few times per release.

**Compare the paste against a live run.** Rejected for the reason `cmd/signpost/corpus_test.go`
already gives about count assertions: a check that fails whenever an extractor improves trains
people to update the number instead of reading the diff. It would fail on commits that have
nothing to do with the page, in a workflow that is deliberately off the merge path.

## Decision

A test in `site/status_test.go` compares the two tables on their **verdicts** and checks
the pasted run for **internal consistency**. It runs in the existing `test` job, so it blocks a
pull request on all three platforms and adds no workflow.

The tables must have the same number of rows in the same order, and row *i* must state the same
verdict in both. The verdict is the state cell reduced to the word it decides, discarding
anything after the em dash: the README cites ADR 0035 on the declined row and the site does not,
which is the difference between a reference document and a landing page. Row labels are not
compared at all.

Each site row's `<tr>` class must match its state — `is-done` when the verdict is `done`,
`is-open` otherwise — because that class draws the row solid or dashed, and the table's caption
tells the reader to read it that way.

For the paste, three identities that hold across every improvement to the tool and break only
when somebody edits it by hand:

- the node and edge counts in its `analysed N files:` line equal those in its
  `N nodes, M edges, K clusters` line;
- the number the note spells out equals the number of dim coverage lines the paste marks;
- the bound in `hubs (top N by degree)` equals the number of hub rows under it.

The figure's caption must carry a date. That is what makes a number in it a fact about a day
rather than a claim about now, and it is the reason the paste needs no comparison against a live
run.

Every assertion fails loudly when its anchor is missing. A renamed table, an absent `## Status`
heading, or a paste with no hub heading is a failure, not zero rows silently agreeing.

## Consequences

A contradiction between the README's states and the landing page's now fails a pull request
instead of shipping. So does a hand-edited transcript.

The gate compares by position, so it couples the two tables' order. The first time the landing
page wants a different order, or a shorter table than the README's, the comparison has to become
name-keyed — and the reworded labels are not usable keys. The fix at that point is an explicit
`data-row` slug on each site row, matched against one derived from the README row. It is not
written now because nothing needs it, and a key nobody uses goes stale like any other.

Re-pasting the transcript stays a manual step, and the numbers in it stay as old as the date
beside them. This decision does not make the figure current; it makes the figure honest about
what it is and consistent with itself.

Nothing gates the rest of the page. The prose, the install commands, and the properties list
make claims no test reads. The table and the paste are gated because both demonstrably drifted;
extending this to prose would mean asserting on wording, which the first rejected alternative
already argues against.
