# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### 2026-07-29

- Initial repository: Go module `github.com/cisco-sbg-emu/signpost`, `.gitignore`,
  README, and this changelog.
- `docs/design.md` — full design for the deterministic core, the OKF output
  bundle, the two model backends, the generator/viewer repo split, CI topology,
  and the supply-chain posture that motivates building rather than adopting.
- `internal/graph` — the in-memory knowledge graph:
  - Typed nodes (`Module`, `Service`, `Interface`, `Data Store`, `Document`,
    `External Dependency`, `Symbol`) whose kinds double as the OKF `type` field.
  - Typed edges (`imports`, `calls`, `implements`, `defines`, `configures`,
    `deploys`, `tested_by`, `documents`, `co_changes`, `owns`) each carrying a
    confidence level of `extracted`, `inferred`, or `ambiguous`, so consumers can
    weight what they trust and reviewers can audit what was guessed.
  - Merge semantics that never clobber: re-adding a node unions tags, files, and
    attributes while preserving existing prose; a `Kind` conflict is an error
    rather than a silent overwrite. Duplicate edges merge by summing weight and
    keeping the stronger confidence, independent of insertion order.
  - Structural metrics: degree and hub ranking, iterative Tarjan SCC for
    dependency cycles, weakly connected components for orphans and
    doc-versus-code islands, cross-cluster bridge detection, and deterministic
    shortest-path traversal.
  - Louvain community detection, hand-written, for the clusters that become
    `index.md` headings.
  - Determinism throughout — sorted iteration, no seeds, no randomised restarts —
    verified by tests that repeat clustering and pathfinding 25 times in-process
    and across 5 separate test processes.
- `internal/discover` — pipeline stage 0 (design §4.0):
  - Hand-written `.gitignore` matcher: anchoring, directory-only patterns,
    negation with correct later-wins precedence, `**` across segments, character
    classes, escapes, and per-directory nesting where a deeper file overrides its
    parent. Matching is case-sensitive on every platform so a Windows checkout and
    a Linux runner agree.
  - Classification into source (dispatched by language), manifest, lock file,
    infrastructure, contract, migration, doc, ownership, and data — filename-based
    and therefore cheap and deterministic.
  - Bounded reads, because a repository is untrusted input: 2 MiB / 50k-line caps
    with head-and-tail retention beyond them, binary detection by NUL byte and
    UTF-8 validity, a total-bytes budget, symlinks recorded but never followed,
    and irregular files skipped so a FIFO cannot block the walk.
  - CRLF normalised at ingest. Without it the same commit yields a different
    bundle on Windows than in CI, which is the determinism requirement in §8.1
    enforced at the point content enters the pipeline.
  - Vendored trees (`vendor/`, `node_modules/`, `.venv/`, build output) pruned and
    recorded in `Skipped`, so an incomplete walk never looks complete.
- `internal/extract` — the extraction contract and its measurement:
  - Language-neutral `Facts` (package, imports with aliases and named symbols,
    symbols with kind/exportedness/receiver/doc, entrypoints) plus an `Extractor`
    interface and a registry that dispatches by language. Extractors return facts
    rather than writing graph nodes, so a fixture can hand-label them and graph
    assembly stays a single shared concern.
  - Normalisation that sorts and merges by fact identity rather than source
    position, so reordering declarations in a file does not churn the bundle.
  - **The §4.2 scoring harness, built before any extractor**: precision, recall,
    and F1 per fact kind against hand-labeled fixtures, with targets of 0.95 (F1,
    imports) and 0.90 (symbols), and the offending values named so a failure is
    actionable. Scored separately per fact kind because an extractor that finds
    every import but half the symbols is useful for the module graph and useless
    for the public surface — one aggregate number would hide that.
  - Go extractor on stdlib `go/parser` + `go/ast`: packages, imports (aliases and
    blank imports retained, since a blank import is invisible `init` coupling),
    types versus interfaces, methods with generic receivers reduced to their base
    type, const/var groups, doc comments from both grouped and single
    declarations, and `main`/`init` entrypoints. An exported method on an
    unexported type is correctly not public surface. A file that does not compile
    still yields its imports and is marked `Incomplete` rather than discarded.
  - **Measured: Go scores F1 1.000 on both imports and symbols** across a
    five-file corpus (9 imports, 22 symbols), and parses all of signpost's own Go
    files with no failures. Go uses the real parser, so this is the precision
    baseline the hand-written extractors are scored against, not merely a pass.

### Changed

#### 2026-07-29

- `docs/design.md` reviewed against the current state of a comparable
  industry-standard tool, using its source rather than a write-up of it. Four
  amendments:
  - **§4.5 — repository content is now treated as untrusted input.** The semantic
    pass wraps every file in a hash-stamped delimiter block, defangs sequences that
    could forge a role turn or a premature closing delimiter, and relies on
    schema-constrained sampling plus the grounding rule as the second layer. This
    closes a real gap: the pass feeds repo content to a model whose output is
    committed and then read by agents that act on it, so injection here is a path
    into the artifact agents trust. A comparable tool already had this control.
  - **§8.0 — merge behaviour for the committed bundle.** Building only on the
    default branch, one page per concept so conflicts stay small and readable, and
    regeneration at the merge commit as the documented tiebreaker. No custom git
    merge driver: it requires per-contributor local configuration and silently
    degrades without it.
  - **§12.1 — what is *not* a differentiator.** Backend flexibility, confidence
    tiers, and prompt hardening are parity or catch-up, not advantages. The
    surviving list is OKF as the on-disk contract, in-file human-review
    preservation, per-page graded trust, loud staleness, byte determinism,
    infrastructure extraction, and a patchable dependency tree.
  - **§11 — injection is mitigated, not solved**, stated plainly, with
    backend-less operation named as the answer for repos that cannot accept the
    residual risk.
- Corrected a stale reference to "fixed seeds in label propagation" in §8.1 left
  over from the Louvain change; clustering is deterministic by construction, with
  no seed to set.

### Notes

- **Zero third-party dependencies so far.** `go.mod` has no `require` block. The
  policy is not zero dependencies but *patchable* dependencies: every direct
  dependency must be one we can bump ourselves, and few enough that bumping stays
  routine. See `docs/design.md` §2.
- **Louvain replaced label propagation** during implementation. LPA was chosen
  first for being a third the size, on the assumption that cluster quality would
  not matter for index headings. Measurement disproved it: on two dense groups
  joined by a single edge, synchronous LPA with a lowest-label tie-break collapses
  the entire graph into one community — the documented giant-community pathology.
  A single cluster containing the whole repo makes the headings worthless. The
  failing test is retained in the suite as a regression guard.
- Tarjan is iterative rather than recursive, with a 20,000-node deep-chain test,
  because recursion risks stack exhaustion on the large monorepos signpost targets.
