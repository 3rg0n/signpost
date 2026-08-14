# 32. Order is drawn only where a file declares it

## Status

Accepted

## Context

Design §4.1 asks a workflow for "what gates exist." Until this change nothing in the bundle
answered it. `manifest.Job` had carried the facts for months — `Gate`, `Needs`, ordered `Steps`,
`Runner`, `Permissions` — and `internal/practice` read `Gate` to write a sentence on the practices
page, but no page in the bundle was addressable as a CI job. A contributor could not link to the
check that blocked them, and an agent asking which checks a change has to pass had to read
`.github/workflows/` itself.

The wider question this settles is what a *sequence* in the bundle may be built from. Every graph
in this repository so far is a set of relationships without order: `imports` says two modules are
connected, not which runs first. The temptation, once jobs are nodes, is to keep going — to render
a repository's workflows as a numbered flow a reader can follow — and the available material does
not support it.

**There is no call graph, by decision.** `EdgeCalls` and `EdgeDefines` are declared in
`graph.go` and rendered by `okf/emit.go`, and nothing in `assemble.go` produces either. The
extractors discard call sites deliberately, and two tests assert the refusal by name:
`TestCDoesNotInventFunctionsFromCalls` and `TestJavaDoesNotInventMethodsFromCalls`.
[ADR 0022](0022-extractors-are-hand-written-and-tree-sitter-has-a-threshold.md) records this as the
standing tax on a line-oriented read.

**Import edges carry no order.** Traversing imports out from an entrypoint yields a reachability
set. Rendering that set as steps would put `Extracted` confidence — the bundle's strongest — on a
sequence no file states.

What the repository *does* declare, unambiguously, is a job's `needs` and its list of steps. That
is the whole of it, and it is the boundary this ADR draws.

## Decision

**A CI job is a node of its own kind, `Pipeline`.** Not a `Service`: nothing here is
long-running. Not a `Document`: a document states a constraint, a job performs work.
`manifest.KindWorkflow` already means a workflow *file*, so `Pipeline` avoids overloading a name
that is taken. One node per job rather than one per workflow file, because a required-check rule
is configured against a job and a failing check names a job — the job is the thing a reader
arrives with.

**`precedes` is drawn only where a file states the order.** A job's `needs` is one such
statement; a job's position in the `jobs` map is not. Jobs without `needs` run concurrently, and
an edge between two of them would assert an order GitHub does not honour — worse than no edge,
because a reader would sequence work around it. Measured before writing any of this: zero `needs:`
declarations exist anywhere in this repository, and all eleven of its gating jobs run in parallel.
The corpus therefore carries a fixture that declares one,
`testdata/corpus/.github/workflows/release.yml`, with jobs on both sides of the boundary — one
`needs`, two without, and one naming a job that does not exist.

**The edge runs from the job that finishes first to the one that waits.** `publish: needs: [build]`
is `build → publish`. So the ordering renders on the earlier job's page as "Runs before", and the
later job's page carries the same fact from the other side as its `needs` attribute.

**A `needs` resolves by the job's key, not by its name, and both are kept.** They are different
strings whenever a job sets `name:`, which is ordinary — in this repository's own `ci.yml` they
differ for six of eight jobs. The key is what a `needs` names; the name is what GitHub's checks UI
shows and what a required-check rule is written against, so it titles the page. Confusing them
loses the edge silently, which is why `manifest.Job` now carries `Key` beside `Name` rather than
recovering one from the other.

**A job whose `name:` interpolates an expression is titled by its key.** `name: test (${{
matrix.os }})` is one line in a file and three checks on a pull request — `test (ubuntu-24.04)`
and two more — and none of those strings is in the tree, because the values come from the matrix
at run time. Slugging the raw text produced `/pipelines/ci-test-matrix-os`: a concept path named
after GitHub Actions syntax, in a committed filename, linked from every page that mentions the
job. The key is short, stated in the file, and already what a `needs` names. The unexpanded
`name:` is still recorded as an attribute, so a reader can see why the title differs from the
checks they saw. A `name:` holding no expression is still preferred over the key — that is the
string GitHub's checks UI shows.

**A `needs` naming a job the file does not declare draws no edge, and the name still renders.** A
workflow can be committed in that state; GitHub never runs the job. Inventing a target would be a
page linking to nothing, and dropping the reference without a word would leave a reader with a job
that appears to run unconditionally when it never runs at all. So the edge is declined and the
unresolvable name stays in the `needs` attribute.

**Which actions a job runs is read from its steps, not from the file's dependency list.**
`dedupeDeps` folds repeated declarations of one dependency per file, so eight jobs running
`actions/checkout` produce a single `Dep`. Attributing by it credits the action to one arbitrary
job and leaves the other seven unconnected — measured, as four orphaned pipeline nodes, before the
pass read `Step.Uses` directly.

**The index states the gate count as a fraction: "Merge gates: 3 of 7 CI jobs."** Both halves,
because a count of CI jobs is not a count of the checks a change meets. Following
[ADR 0030](0030-a-finding-states-its-own-absence.md), all three outcomes are written down: some
jobs gate, none of them do, or there is no CI at all. The last is the one a reader most needs
stated — silence there is indistinguishable from a run that failed to look.

**The finding says what `Gate` means and stops there: "runs on a pull request or on a push to
the default branch."** Shorter phrasings overstate it. `Gate` is set by either trigger, so
"runs against a pull request" is false for a push-only workflow — this repository's `pages.yml`
is one, and design §7 says in so many words that it is never a required check, yet it is in the
list. And *required* is not a property of the tree at all: GitHub's branch protection is
repository configuration, so a bundle that called a gating job blocking would be asserting
something no file it read says. `internal/practice` writes its own sentence from the same field
and does say "can block a merge" on the practices page — worded before this distinction was
drawn. It is wrong there for the same reason and is left to a change of its own, since it has its
own tests and its own text; this decision binds the new finding, and correcting the older
sentence to match is a follow-up, not a silent edit inside this one.

## Consequences

**Every adopter with CI gets new pages on their next build.** On this repository, thirteen. That
is a diff nobody asked for, in a committed artifact, which is why this is an ADR: reverting it
produces the same unasked-for diff again.

**A `Pipeline` node costs no new rendering machinery.** `renderAll` derives a page's path from its
ID and iterates whatever kinds the graph holds, so pages, the index listing, DOT, Mermaid, and
JSON all followed from the kind existing. Only the human-facing labels were written — a heading,
a shape, a colour, an edge label.

**The steps summary is bounded at eight names and strips a pinned ref.** A step the author did not
name is named after its `uses` by `ExtractWorkflow`, and a 40-character SHA is identical in every
job in the repository — six of them in one attribute is a line spent on nothing. Stripped only
when the name came from the `uses`: an author who writes a ref into a step's own `name:` meant it.

**No operational flow is generated, and the gap is now a named one.** A page saying "a request
enters here, then this module, then that store" is what a reader most wants, and it needs either
call edges or the runtime evidence
[ADR 0031](0031-scope-is-a-lifecycle-test-not-a-list-of-non-goals.md)'s first clause excludes —
compiled from the tree. Data-access edges are the nearest reachable next step and are separate
work, because whether two writers of one table may be linked at `Ambiguous` confidence is its own
decision.

**A repository whose CI is one job per workflow gains pages with no ordering at all.** That is the
common case and it is accepted: the gate finding is the payload for those readers, and the node has
to exist for the finding to link to it.

Design reference: [docs/design.md](../design.md) §4.1, §4.4, §7.1.
