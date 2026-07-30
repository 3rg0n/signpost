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
| [0006](0006-generator-and-viewer-are-separate-repositories.md) | Generator and viewer are separate repositories | Accepted |

## Decisions still owed one

Recorded here so the gap is visible rather than discovered later:

- **Configuration file format and location.** Blocks restructuring the CLI around verbs,
  since a config file changes what a flag means.
- **OpenTelemetry instrumentation.** The SDK is five to eight modules including gRPC and
  protobuf, which would end the property [ADR 0002](0002-patchable-dependencies-not-zero-dependencies.md)
  records and README asserts. Genuinely unresolved: hand-rolled OTLP/HTTP, a build tag, or
  taking the dependency are all live.
- **Louvain over label propagation.** Alternative tried, measured, and rejected — the
  textbook ADR shape, currently one line in `docs/design.md` §4.4.
- **Git history as annotation only.** History never creates a node or decides what is on the
  map. Non-obvious, easy for a future change to violate, currently a package doc comment.

## Writing one

Number sequentially, name the file `NNNN-kebab-title.md`, and add a row above. A new *direct
dependency* requires an ADR and CI enforces it: the `require` block is diffed against the
base branch, and a new entry with no ADR touched in the same pull request fails the build.
Write it before the code.
