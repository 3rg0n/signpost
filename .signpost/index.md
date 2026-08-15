---
okf_version: "0.2"
type: Index
title: Repository map
description: "Structural map of this repository: 87 concepts, 218 relationships."
resource: git://github.com/3rg0n/signpost@87c06ede8626e41ed763c3bac660e77870c3086f
generated: { by: signpost/dev, at: "2026-08-15" }
---
# Repository map

<!-- signpost:managed:index -->
Start here. What the shape of this repository says, then a line per page naming what is on it.

### How work is done here

- [How work is done here](./practices.md) — what this repository declares about building, testing, gating, and ownership, and what it does not.

### Most connected

The places a wrong assumption propagates furthest, so the places to read first.

- [\(repository root\)](./modules/root.md) — 50 relationships (17 in, 33 out)
- [cmd/signpost](./modules/signpost.md) — 46 relationships (15 in, 31 out)
- [internal/assemble](./modules/assemble.md) — 28 relationships (12 in, 16 out)
- [internal/manifest](./modules/manifest.md) — 26 relationships (15 in, 11 out)
- [internal/discover](./modules/discover.md) — 21 relationships (14 in, 7 out)

### Structural findings

What the shape of this repository says. Each line is a result — where one reads "none", that is the finding.

- **Import cycles: none.** No module here imports its way back to itself.
- **Cross-cluster edges: 43.** Where a change is most likely to surprise someone: the two sides are maintained as separate concerns and coupled anyway.
  - [internal/assemble](./modules/assemble.md) → [\(repository root\)](./modules/root.md) (changes with)
  - [internal/discover](./modules/discover.md) → [\(repository root\)](./modules/root.md) (changes with)
  - [internal/export](./modules/export.md) → [\(repository root\)](./modules/root.md) (changes with)
  - [internal/extract](./modules/extract.md) → [\(repository root\)](./modules/root.md) (changes with)
  - [internal/graph](./modules/graph.md) → [\(repository root\)](./modules/root.md) (changes with)
  - [internal/hook](./modules/hook.md) → [\(repository root\)](./modules/root.md) (changes with)
  - [internal/manifest](./modules/manifest.md) → [\(repository root\)](./modules/root.md) (changes with)
  - [internal/model](./modules/model.md) → [\(repository root\)](./modules/root.md) (changes with)
  - [internal/okf](./modules/okf.md) → [\(repository root\)](./modules/root.md) (changes with)
  - [internal/practice](./modules/practice.md) → [\(repository root\)](./modules/root.md) (changes with)
  - [\(repository root\)](./modules/root.md) → [internal/assemble](./modules/assemble.md) (changes with)
  - [\(repository root\)](./modules/root.md) → [internal/discover](./modules/discover.md) (changes with)
  - [\(repository root\)](./modules/root.md) → [internal/export](./modules/export.md) (changes with)
  - [\(repository root\)](./modules/root.md) → [internal/extract](./modules/extract.md) (changes with)
  - [\(repository root\)](./modules/root.md) → [internal/graph](./modules/graph.md) (changes with)
  - [\(repository root\)](./modules/root.md) → [internal/hook](./modules/hook.md) (changes with)
  - [\(repository root\)](./modules/root.md) → [internal/manifest](./modules/manifest.md) (changes with)
  - [\(repository root\)](./modules/root.md) → [internal/model](./modules/model.md) (changes with)
  - [\(repository root\)](./modules/root.md) → [internal/okf](./modules/okf.md) (changes with)
  - [\(repository root\)](./modules/root.md) → [internal/practice](./modules/practice.md) (changes with)
  - and 23 more
- **Disconnected islands: none.** Everything that is linked at all is linked into one body.
- **Unconnected concepts: 35.** Nothing links to or from these: dead code, an unreferenced document, or a gap in extraction. Which of the three it is needs a human.
  - [ADR 0001: hand written tolerant yaml reader](./references/adr-0001-hand-written-tolerant-yaml-reader.md)
  - [ADR 0002: patchable dependencies not zero dependencies](./references/adr-0002-patchable-dependencies-not-zero-dependencies.md)
  - [ADR 0003: directory granularity for module nodes](./references/adr-0003-directory-granularity-for-module-nodes.md)
  - [ADR 0004: confidence is a first class field](./references/adr-0004-confidence-is-a-first-class-field.md)
  - [ADR 0005: commit the bundle to the repository](./references/adr-0005-commit-the-bundle-to-the-repository.md)
  - [ADR 0006: generator and viewer are separate repositories](./references/adr-0006-generator-and-viewer-are-separate-repositories.md)
  - [ADR 0007: the bundle names the commit it describes](./references/adr-0007-the-bundle-names-the-commit-it-describes.md)
  - [ADR 0008: the viewer lives in this repository](./references/adr-0008-the-viewer-lives-in-this-repository.md)
  - [ADR 0009: the semantic pass is opt in and egress is explicit](./references/adr-0009-the-semantic-pass-is-opt-in-and-egress-is-explicit.md)
  - [ADR 0010: a stale page is deleted only when nobody wrote on it](./references/adr-0010-a-stale-page-is-deleted-only-when-nobody-wrote-on-it.md)
  - [ADR 0011: configuration file format and location](./references/adr-0011-configuration-file-format-and-location.md)
  - [ADR 0012: a group name is never an action](./references/adr-0012-a-group-name-is-never-an-action.md)
  - [ADR 0013: the local hook reports and ci gates](./references/adr-0013-the-local-hook-reports-and-ci-gates.md)
  - [ADR 0014: adopt the otel sdk and write the exporter](./references/adr-0014-adopt-the-otel-sdk-and-write-the-exporter.md)
  - [ADR 0015: a colliding page name is suffixed from its own key](./references/adr-0015-a-colliding-page-name-is-suffixed-from-its-own-key.md)
  - [ADR 0016: a reader records what only it can know](./references/adr-0016-a-reader-records-what-only-it-can-know.md)
  - [ADR 0017: a resolution root may come from the source itself](./references/adr-0017-a-resolution-root-may-come-from-the-source-itself.md)
  - [ADR 0018: view serves a repository over loopback](./references/adr-0018-view-serves-a-repository-over-loopback.md)
  - [ADR 0019: louvain over label propagation](./references/adr-0019-louvain-over-label-propagation.md)
  - [ADR 0020: git history annotates the map and never draws it](./references/adr-0020-git-history-annotates-the-map-and-never-draws-it.md)
  - and 15 more
- **Merge gates: 11 of 13 CI jobs.** These run on a pull request or on a push to the default branch, so they are the automated checks a change meets. Which of them is *required* is configured on the repository and is not in the tree.
  - [ci commit trailers name real issues](./pipelines/ci-commit-trailers-name-real-issues.md)
  - [ci corpus \(a repository signpost did not write\)](./pipelines/ci-corpus-a-repository-signpost-did-not-write.md)
  - [ci dependency gate](./pipelines/ci-dependency-gate.md)
  - [ci installer parses \(5.1 and 7\)](./pipelines/ci-installer-parses-5-1-and-7.md)
  - [ci lint](./pipelines/ci-lint.md)
  - [ci security](./pipelines/ci-security.md)
  - [ci signpost analyses signpost](./pipelines/ci-signpost-analyses-signpost.md)
  - [ci test](./pipelines/ci-test.md)
  - [pages deploy](./pipelines/pages-deploy.md)
  - [signpost rebuild the bundle](./pipelines/signpost-rebuild-the-bundle.md)
  - [signpost the bundle still describes this tree](./pipelines/signpost-the-bundle-still-describes-this-tree.md)

### Modules

- [internal/assemble](./modules/assemble.md) — 7 go files; 3 exported symbols.
- [internal/config](./modules/config.md) — 2 go files; 3 exported symbols.
- [internal/discover](./modules/discover.md) — 5 go files; 44 exported symbols.
- [internal/export](./modules/export.md) — 6 go files; 7 exported symbols.
- [internal/extract](./modules/extract.md) — 34 go files; 80 exported symbols.
- [internal/graph](./modules/graph.md) — 4 go files; 53 exported symbols.
- [internal/hook](./modules/hook.md) — 2 go files; 17 exported symbols.
- [internal/manifest](./modules/manifest.md) — 37 go files; 130 exported symbols.
- [internal/model](./modules/model.md) — 15 go files; 36 exported symbols.
- [internal/okf](./modules/okf.md) — 14 go files; 36 exported symbols.
- [internal/practice](./modules/practice.md) — 4 go files; 18 exported symbols.
- [\(repository root\)](./modules/root.md) — 2 powershell files; 10 exported symbols; entrypoint #!, param.
- [internal/scaffold](./modules/scaffold.md) — 2 go files; 10 exported symbols.
- [internal/selfupdate](./modules/selfupdate.md) — 2 go files; 10 exported symbols.
- [internal/semantic](./modules/semantic.md) — 3 go files; 8 exported symbols.
- [cmd/signpost](./modules/signpost.md) — 26 go files; entrypoint main; package main.
- [site](./modules/site.md) — 2 go files; 1 exported symbol.
- [internal/sqlstmt](./modules/sqlstmt.md) — 2 go files; 10 exported symbols.
- [internal/telemetry](./modules/telemetry.md) — 5 go files; 8 exported symbols.
- [internal/vcs](./modules/vcs.md) — 10 go files; 16 exported symbols.
- [internal/view](./modules/view.md) — 5 go files; 4 exported symbols.

### Pipelines

- [ci commit trailers name real issues](./pipelines/ci-commit-trailers-name-real-issues.md) — CI job commit trailers name real issues in the ci workflow, 2 steps; runs on a pull request or a default-branch push
- [ci corpus \(a repository signpost did not write\)](./pipelines/ci-corpus-a-repository-signpost-did-not-write.md) — CI job corpus (a repository signpost did not write) in the ci workflow, 22 steps; runs on a pull request or a default-branch push
- [ci dependency gate](./pipelines/ci-dependency-gate.md) — CI job dependency gate in the ci workflow, 4 steps; runs on a pull request or a default-branch push
- [ci installer parses \(5.1 and 7\)](./pipelines/ci-installer-parses-5-1-and-7.md) — CI job installer parses (5.1 and 7) in the ci workflow, 3 steps; runs on a pull request or a default-branch push
- [ci lint](./pipelines/ci-lint.md) — CI job lint in the ci workflow, 8 steps; runs on a pull request or a default-branch push
- [ci security](./pipelines/ci-security.md) — CI job security in the ci workflow, 5 steps; runs on a pull request or a default-branch push
- [ci signpost analyses signpost](./pipelines/ci-signpost-analyses-signpost.md) — CI job signpost analyses signpost in the ci workflow, 13 steps; runs on a pull request or a default-branch push
- [ci test](./pipelines/ci-test.md) — CI job test (${{ matrix.os }}) in the ci workflow, 6 steps; runs on a pull request or a default-branch push
- [pages deploy](./pipelines/pages-deploy.md) — CI job deploy in the pages workflow, 7 steps; runs on a pull request or a default-branch push
- [release build and publish](./pipelines/release-build-and-publish.md) — CI job build and publish in the release workflow, 5 steps
- [signpost rebuild the bundle](./pipelines/signpost-rebuild-the-bundle.md) — CI job rebuild the bundle in the signpost workflow, 6 steps; runs on a pull request or a default-branch push
- [signpost-semantic summarise modules with a model](./pipelines/signpost-semantic-summarise-modules-with-a-model.md) — CI job summarise modules with a model in the signpost-semantic workflow, 8 steps
- [signpost the bundle still describes this tree](./pipelines/signpost-the-bundle-still-describes-this-tree.md) — CI job the bundle still describes this tree in the signpost workflow, 4 steps; runs on a pull request or a default-branch push

### Documents

- [ADR 0001: hand written tolerant yaml reader](./references/adr-0001-hand-written-tolerant-yaml-reader.md) — Architecture decision (Accepted), 15 rules read from 0001-hand-written-tolerant-yaml-reader.md.
- [ADR 0002: patchable dependencies not zero dependencies](./references/adr-0002-patchable-dependencies-not-zero-dependencies.md) — Architecture decision (Accepted), 28 rules read from 0002-patchable-dependencies-not-zero-dependencies.md.
- [ADR 0003: directory granularity for module nodes](./references/adr-0003-directory-granularity-for-module-nodes.md) — Architecture decision (Accepted), 18 rules read from 0003-directory-granularity-for-module-nodes.md.
- [ADR 0004: confidence is a first class field](./references/adr-0004-confidence-is-a-first-class-field.md) — Architecture decision (Accepted), 24 rules read from 0004-confidence-is-a-first-class-field.md.
- [ADR 0005: commit the bundle to the repository](./references/adr-0005-commit-the-bundle-to-the-repository.md) — Architecture decision (Accepted), 22 rules read from 0005-commit-the-bundle-to-the-repository.md.
- [ADR 0006: generator and viewer are separate repositories](./references/adr-0006-generator-and-viewer-are-separate-repositories.md) — Architecture decision (Superseded), 22 rules read from 0006-generator-and-viewer-are-separate-repositories.md.
- [ADR 0007: the bundle names the commit it describes](./references/adr-0007-the-bundle-names-the-commit-it-describes.md) — Architecture decision (Accepted), 21 rules read from 0007-the-bundle-names-the-commit-it-describes.md.
- [ADR 0008: the viewer lives in this repository](./references/adr-0008-the-viewer-lives-in-this-repository.md) — Architecture decision (Accepted), 23 rules read from 0008-the-viewer-lives-in-this-repository.md.
- [ADR 0009: the semantic pass is opt in and egress is explicit](./references/adr-0009-the-semantic-pass-is-opt-in-and-egress-is-explicit.md) — Architecture decision (Accepted), 25 rules read from 0009-the-semantic-pass-is-opt-in-and-egress-is-explicit.md.
- [ADR 0010: a stale page is deleted only when nobody wrote on it](./references/adr-0010-a-stale-page-is-deleted-only-when-nobody-wrote-on-it.md) — Architecture decision (Accepted), 21 rules read from 0010-a-stale-page-is-deleted-only-when-nobody-wrote-on-it.md.
- [ADR 0011: configuration file format and location](./references/adr-0011-configuration-file-format-and-location.md) — Architecture decision (Accepted), 27 rules read from 0011-configuration-file-format-and-location.md.
- [ADR 0012: a group name is never an action](./references/adr-0012-a-group-name-is-never-an-action.md) — Architecture decision (Accepted), 23 rules read from 0012-a-group-name-is-never-an-action.md.
- [ADR 0013: the local hook reports and ci gates](./references/adr-0013-the-local-hook-reports-and-ci-gates.md) — Architecture decision (Accepted), 23 rules read from 0013-the-local-hook-reports-and-ci-gates.md.
- [ADR 0014: adopt the otel sdk and write the exporter](./references/adr-0014-adopt-the-otel-sdk-and-write-the-exporter.md) — Architecture decision (Accepted), 44 rules read from 0014-adopt-the-otel-sdk-and-write-the-exporter.md.
- [ADR 0015: a colliding page name is suffixed from its own key](./references/adr-0015-a-colliding-page-name-is-suffixed-from-its-own-key.md) — Architecture decision (Accepted), 25 rules read from 0015-a-colliding-page-name-is-suffixed-from-its-own-key.md.
- [ADR 0016: a reader records what only it can know](./references/adr-0016-a-reader-records-what-only-it-can-know.md) — Architecture decision (Accepted), 19 rules read from 0016-a-reader-records-what-only-it-can-know.md.
- [ADR 0017: a resolution root may come from the source itself](./references/adr-0017-a-resolution-root-may-come-from-the-source-itself.md) — Architecture decision (Accepted), 18 rules read from 0017-a-resolution-root-may-come-from-the-source-itself.md.
- [ADR 0018: view serves a repository over loopback](./references/adr-0018-view-serves-a-repository-over-loopback.md) — Architecture decision (Accepted), 20 rules read from 0018-view-serves-a-repository-over-loopback.md.
- [ADR 0019: louvain over label propagation](./references/adr-0019-louvain-over-label-propagation.md) — Architecture decision (Accepted), 20 rules read from 0019-louvain-over-label-propagation.md.
- [ADR 0020: git history annotates the map and never draws it](./references/adr-0020-git-history-annotates-the-map-and-never-draws-it.md) — Architecture decision (Accepted), 21 rules read from 0020-git-history-annotates-the-map-and-never-draws-it.md.
- [ADR 0021: track the published spec and never overload its keys](./references/adr-0021-track-the-published-spec-and-never-overload-its-keys.md) — Architecture decision (Accepted), 14 rules read from 0021-track-the-published-spec-and-never-overload-its-keys.md.
- [ADR 0022: extractors are hand written and tree sitter has a threshold](./references/adr-0022-extractors-are-hand-written-and-tree-sitter-has-a-threshold.md) — Architecture decision (Accepted), 17 rules read from 0022-extractors-are-hand-written-and-tree-sitter-has-a-threshold.md.
- [ADR 0023: a build declaration is settled where the tree is visible](./references/adr-0023-a-build-declaration-is-settled-where-the-tree-is-visible.md) — Architecture decision (Accepted), 23 rules read from 0023-a-build-declaration-is-settled-where-the-tree-is-visible.md.
- [ADR 0024: a branch verify reads the history the bundle read](./references/adr-0024-a-branch-verify-reads-the-history-the-bundle-read.md) — Architecture decision (Accepted), 15 rules read from 0024-a-branch-verify-reads-the-history-the-bundle-read.md.
- [ADR 0025: the census long tail is declined by category](./references/adr-0025-the-census-long-tail-is-declined-by-category.md) — Architecture decision (Accepted), 15 rules read from 0025-the-census-long-tail-is-declined-by-category.md.
- [ADR 0026: history is read where a count answers the question](./references/adr-0026-history-is-read-where-a-count-answers-the-question.md) — Architecture decision (Accepted), 30 rules read from 0026-history-is-read-where-a-count-answers-the-question.md.
- [ADR 0027: a gate fails only on what the reader can fix](./references/adr-0027-a-gate-fails-only-on-what-the-reader-can-fix.md) — Architecture decision (Accepted), 21 rules read from 0027-a-gate-fails-only-on-what-the-reader-can-fix.md.
- [ADR 0028: scaffolded files are embedded and tested against our own](./references/adr-0028-scaffolded-files-are-embedded-and-tested-against-our-own.md) — Architecture decision (Accepted), 26 rules read from 0028-scaffolded-files-are-embedded-and-tested-against-our-own.md.
- [ADR 0029: the viewer is written by the run that publishes it](./references/adr-0029-the-viewer-is-written-by-the-run-that-publishes-it.md) — Architecture decision (Accepted), 24 rules read from 0029-the-viewer-is-written-by-the-run-that-publishes-it.md.
- [ADR 0030: a finding states its own absence](./references/adr-0030-a-finding-states-its-own-absence.md) — Architecture decision (Accepted), 18 rules read from 0030-a-finding-states-its-own-absence.md.
- [ADR 0031: scope is a lifecycle test not a list of non goals](./references/adr-0031-scope-is-a-lifecycle-test-not-a-list-of-non-goals.md) — Architecture decision (Accepted), 19 rules read from 0031-scope-is-a-lifecycle-test-not-a-list-of-non-goals.md.
- [ADR 0032: order is drawn only where a file declares it](./references/adr-0032-order-is-drawn-only-where-a-file-declares-it.md) — Architecture decision (Accepted), 21 rules read from 0032-order-is-drawn-only-where-a-file-declares-it.md.
- [ADR 0033: the binary replaces itself only from a verified release](./references/adr-0033-the-binary-replaces-itself-only-from-a-verified-release.md) — Architecture decision (Accepted), 24 rules read from 0033-the-binary-replaces-itself-only-from-a-verified-release.md.
- [ADR 0034: a deterministic pass may not produce an ambiguous edge](./references/adr-0034-a-deterministic-pass-may-not-produce-an-ambiguous-edge.md) — Architecture decision (Accepted), 18 rules read from 0034-a-deterministic-pass-may-not-produce-an-ambiguous-edge.md.
- [AGENTS.md](./references/agents-md.md) — Stated constraints, 14 rules read from AGENTS.md.
- [README.md](./references/readme-md.md) — Architecture decision, 7 rules read from README.md.

### External dependencies

- [actions/cache](./references/github-actions-actions-cache.md) — github-actions dependency actions/cache (55cc8345863c7cc4c66a329aec7e433d2d1c52a9)
- [actions/checkout](./references/github-actions-actions-checkout.md) — github-actions dependency actions/checkout (3d3c42e5aac5ba805825da76410c181273ba90b1)
- [actions/configure-pages](./references/github-actions-actions-configure-pages.md) — github-actions dependency actions/configure-pages (45bfe0192ca1faeb007ade9deae92b16b8254a0d)
- [actions/deploy-pages](./references/github-actions-actions-deploy-pages.md) — github-actions dependency actions/deploy-pages (cd2ce8fcbc39b97be8ca5fce6e763baed58fa128)
- [actions/setup-go](./references/github-actions-actions-setup-go.md) — github-actions dependency actions/setup-go (b7ad1dad31e06c5925ef5d2fc7ad053ef454303e)
- [actions/upload-pages-artifact](./references/github-actions-actions-upload-pages-artifact.md) — github-actions dependency actions/upload-pages-artifact (fc324d3547104276b827a68afc52ff2a11cc49c9)
- [golangci/golangci-lint-action](./references/github-actions-golangci-golangci-lint-action.md) — github-actions dependency golangci/golangci-lint-action (ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a)
- [github.com/cespare/xxhash/v2](./references/go-github-com-cespare-xxhash-v2.md) — go dependency github.com/cespare/xxhash/v2 (v2.3.0)
- [github.com/go-logr/logr](./references/go-github-com-go-logr-logr.md) — go dependency github.com/go-logr/logr (v1.4.3)
- [github.com/go-logr/stdr](./references/go-github-com-go-logr-stdr.md) — go dependency github.com/go-logr/stdr (v1.2.2)
- [github.com/google/uuid](./references/go-github-com-google-uuid.md) — go dependency github.com/google/uuid (v1.6.0)
- [go.opentelemetry.io/auto/sdk](./references/go-go-opentelemetry-io-auto-sdk.md) — go dependency go.opentelemetry.io/auto/sdk (v1.2.1)
- [go.opentelemetry.io/otel](./references/go-go-opentelemetry-io-otel.md) — go dependency go.opentelemetry.io/otel (v1.44.0)
- [go.opentelemetry.io/otel/metric](./references/go-go-opentelemetry-io-otel-metric.md) — go dependency go.opentelemetry.io/otel/metric (v1.44.0)
- [go.opentelemetry.io/otel/sdk](./references/go-go-opentelemetry-io-otel-sdk.md) — go dependency go.opentelemetry.io/otel/sdk (v1.44.0)
- [go.opentelemetry.io/otel/trace](./references/go-go-opentelemetry-io-otel-trace.md) — go dependency go.opentelemetry.io/otel/trace (v1.44.0)
- [golang.org/x/sys](./references/go-golang-org-x-sys.md) — go dependency golang.org/x/sys (v0.45.0)
<!-- /signpost:managed:index -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
