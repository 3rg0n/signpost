---
okf_version: "0.2"
type: Index
title: Repository map
description: "Structural map of this repository: 57 concepts, 94 relationships."
resource: git://github.com/3rg0n/signpost@323acdab3608d65385f549d938831891ac6ca99b
generated: { by: signpost/dev, at: "2026-08-07" }
---
# Repository map

<!-- signpost:managed:index -->
Start here. Each line names a page and what is on it.

### How work is done here

- [How work is done here](./practices.md) — what this repository declares about building, testing, gating, and ownership, and what it does not.

### Most connected

The places a wrong assumption propagates furthest, so the places to read first.

- [cmd/signpost](./modules/signpost.md) — 34 relationships (10 in, 24 out)
- [internal/discover](./modules/discover.md) — 19 relationships (13 in, 6 out)
- [AGENTS.md](./references/agents-md.md) — 17 relationships (0 in, 17 out)
- [internal/manifest](./modules/manifest.md) — 15 relationships (10 in, 5 out)
- [internal/assemble](./modules/assemble.md) — 13 relationships (5 in, 8 out)

### Modules

- [internal/assemble](./modules/assemble.md) — 6 go files; 64 exported symbols.
- [internal/config](./modules/config.md) — 2 go files; 26 exported symbols.
- [internal/discover](./modules/discover.md) — 5 go files; 72 exported symbols.
- [internal/export](./modules/export.md) — 6 go files; 22 exported symbols.
- [internal/extract](./modules/extract.md) — 18 go files; 218 exported symbols.
- [internal/graph](./modules/graph.md) — 4 go files; 69 exported symbols.
- [internal/hook](./modules/hook.md) — 2 go files; 39 exported symbols.
- [internal/manifest](./modules/manifest.md) — 27 go files; 256 exported symbols.
- [internal/model](./modules/model.md) — 15 go files; 98 exported symbols.
- [internal/okf](./modules/okf.md) — 13 go files; 198 exported symbols.
- [internal/practice](./modules/practice.md) — 3 go files; 31 exported symbols.
- [internal/semantic](./modules/semantic.md) — 3 go files; 35 exported symbols.
- [cmd/signpost](./modules/signpost.md) — 19 go files; 116 exported symbols; entrypoint main; package main.
- [site](./modules/site.md) — 2 go files; 1 exported symbol.
- [internal/telemetry](./modules/telemetry.md) — 5 go files; 24 exported symbols.
- [internal/vcs](./modules/vcs.md) — 6 go files; 55 exported symbols.
- [internal/view](./modules/view.md) — 3 go files; 24 exported symbols.

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
- [AGENTS.md](./references/agents-md.md) — Stated constraints, 14 rules read from AGENTS.md.
- [README.md](./references/readme-md.md) — Architecture decision, 8 rules read from README.md.

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
