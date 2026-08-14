# Architecture decision records

Decisions that cross-cut the codebase or are costly to reverse: public contracts, security
posture, foundational dependencies, long-lived conventions. Local choices are not recorded
here — they belong in the code, next to the code.

An ADR is **immutable once accepted.** A decision that changes gets a new ADR that
supersedes the old one; the old one stays, with its status updated, because the record of
what was decided and why is the reason the file exists. Editing one in place turns it into
documentation, which rots without saying so — [ADR 0001](0001-hand-written-tolerant-yaml-reader.md)
reversed a choice `docs/design.md` still described for months, and nothing in the design doc
could tell a reader which of its statements were decisions.

Format is [Nygard short form](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions):
Status, Context, Decision, Consequences. Context states the forces including the ones that
lost; Consequences states what this costs, not only what it buys.

| # | Decision | Status |
|---|---|---|
| [0001](0001-hand-written-tolerant-yaml-reader.md) | Hand-written tolerant YAML reader | Accepted |
| [0002](0002-patchable-dependencies-not-zero-dependencies.md) | Patchable dependencies, not zero dependencies | Accepted |
| [0003](0003-directory-granularity-for-module-nodes.md) | Directory granularity for module nodes | Accepted |
| [0004](0004-confidence-is-a-first-class-field.md) | Confidence is a first-class field on every node and edge | Accepted |
| [0005](0005-commit-the-bundle-to-the-repository.md) | Commit the bundle to the repository | Accepted |
| [0006](0006-generator-and-viewer-are-separate-repositories.md) | Generator and viewer are separate repositories | Superseded by [0008](0008-the-viewer-lives-in-this-repository.md) |
| [0007](0007-the-bundle-names-the-commit-it-describes.md) | The bundle names the commit it describes, and staleness is a content comparison | Accepted |
| [0008](0008-the-viewer-lives-in-this-repository.md) | The viewer lives in this repository, with no JavaScript dependencies | Accepted |
| [0009](0009-the-semantic-pass-is-opt-in-and-egress-is-explicit.md) | The semantic pass is opt-in, and egress is explicit | Accepted |
| [0010](0010-a-stale-page-is-deleted-only-when-nobody-wrote-on-it.md) | A stale page is deleted only when nobody wrote on it | Accepted |
| [0011](0011-configuration-file-format-and-location.md) | Configuration lives in `.signpost.yml`, and a config key may only change a default | Accepted |
| [0012](0012-a-group-name-is-never-an-action.md) | A group name is never an action, and a noun with one operation stays flat | Accepted |
| [0013](0013-the-local-hook-reports-and-ci-gates.md) | The local hook reports, CI gates, and the hook is a guest in somebody else's file | Accepted |
| [0014](0014-adopt-the-otel-sdk-and-write-the-exporter.md) | Adopt the OpenTelemetry SDK and write the exporter | Accepted |
| [0015](0015-a-colliding-page-name-is-suffixed-from-its-own-key.md) | A colliding page name is suffixed from its own key, not from its position | Accepted |
| [0016](0016-a-reader-records-what-only-it-can-know.md) | A reader records what only it can know, including that it does not know | Accepted |
| [0017](0017-a-resolution-root-may-come-from-the-source-itself.md) | A resolution root may come from the source itself, and an undeclared import stays a gap | Accepted |
| [0018](0018-view-serves-a-repository-over-loopback.md) | `view` serves a repository's structure over loopback, and holds no state | Accepted |
| [0019](0019-louvain-over-label-propagation.md) | Clustering is hand-written Louvain, after label propagation was measured and rejected | Accepted |
| [0020](0020-git-history-annotates-the-map-and-never-draws-it.md) | Git history annotates the map and never draws it | Accepted |
| [0021](0021-track-the-published-spec-and-never-overload-its-keys.md) | Track the published spec, and never overload one of its keys | Accepted |
| [0022](0022-extractors-are-hand-written-and-tree-sitter-has-a-threshold.md) | Extractors are hand-written, and tree-sitter has a written threshold | Accepted |
| [0023](0023-a-build-declaration-is-settled-where-the-tree-is-visible.md) | A build declaration is settled where the tree is visible, not where the file is read | Accepted |
| [0024](0024-a-branch-verify-reads-the-history-the-bundle-read.md) | A branch verify reads the history the bundle read, not just its stamp | Accepted |
| [0025](0025-the-census-long-tail-is-declined-by-category.md) | The census long tail is declined by category, not one file type at a time | Accepted |
| [0026](0026-history-is-read-where-a-count-answers-the-question.md) | History is read where a count answers the question | Accepted |
| [0027](0027-a-gate-fails-only-on-what-the-reader-can-fix.md) | A gate fails only on what the reader can fix | Accepted |
| [0028](0028-scaffolded-files-are-embedded-and-tested-against-our-own.md) | Scaffolded files are embedded and tested against our own | Accepted |
| [0029](0029-the-viewer-is-written-by-the-run-that-publishes-it.md) | The viewer is written by the run that publishes it | Accepted |
| [0030](0030-a-finding-states-its-own-absence.md) | A finding states its own absence, and the index carries it | Accepted |
| [0031](0031-scope-is-a-lifecycle-test-not-a-list-of-non-goals.md) | Scope is a lifecycle test, not a list of non-goals | Accepted |

[ADR 0002](0002-patchable-dependencies-not-zero-dependencies.md)'s *rule* — patchable
dependencies, few enough that bumping stays routine — still binds. Its *consequence*, that
`go.mod` has no `require` block, is superseded by
[0014](0014-adopt-the-otel-sdk-and-write-the-exporter.md), which takes three.

## Decisions still owed one

Recorded here so the gap is visible rather than discovered later. One is owed: making the declined
set visible in a run's output. `manifest.RunResult.Unhandled` counts files no route claimed, but
that count includes files other subsystems read, so printing it would overstate the gap —
[0025](0025-the-census-long-tail-is-declined-by-category.md) records why it is left undone rather
than approximated, and closing it needs a cross-subsystem notion of "read by someone" that would be
its own decision.

## Writing one

Number sequentially, name the file `NNNN-kebab-title.md`, and add a row above. A new *direct
dependency* requires an ADR and CI enforces it: the `require` block is diffed against the
base branch, and a new entry with no ADR touched in the same pull request fails the build.
Write it before the code.
