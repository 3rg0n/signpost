---
type: Module
title: (repository root)
description: "2 powershell files; 10 exported symbols; entrypoint #!, param."
resource: git://github.com/3rg0n/signpost@3a053ce2a89f8fd620c86f0d6d3dd399ff0bcc13
tags: [entrypoint]
generated: { by: signpost/dev, at: "2026-08-10" }
attributes:
  - { name: commits, value: "58" }
  - { name: entrypoints, value: "#!, param" }
  - { name: exported, value: "10" }
  - { name: files, value: "2" }
  - { name: first_commit, value: "2026-07-29" }
  - { name: last_commit, value: "2026-08-10" }
  - { name: lines_added, value: "4710" }
  - { name: lines_removed, value: "144" }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 66% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 17 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 12 }
  - { kind: co_changes, to: ./export.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./extract.md, confidence: extracted, weight: 9 }
  - { kind: co_changes, to: ./graph.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./hook.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 7 }
  - { kind: co_changes, to: ./model.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./okf.md, confidence: extracted, weight: 11 }
  - { kind: co_changes, to: ./practice.md, confidence: extracted, weight: 5 }
  - { kind: co_changes, to: ./semantic.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 35 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 14 }
  - { kind: co_changes, to: ./vcs.md, confidence: extracted, weight: 5 }
  - { kind: configures, to: ../references/github-actions-actions-cache.md, confidence: extracted, source: .github/workflows/signpost-semantic.yml }
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/ci.yml }
  - { kind: configures, to: ../references/github-actions-actions-configure-pages.md, confidence: extracted, source: .github/workflows/pages.yml }
  - { kind: configures, to: ../references/github-actions-actions-deploy-pages.md, confidence: extracted, source: .github/workflows/pages.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/ci.yml }
  - { kind: configures, to: ../references/github-actions-actions-upload-pages-artifact.md, confidence: extracted, source: .github/workflows/pages.yml }
  - { kind: configures, to: ../references/github-actions-golangci-golangci-lint-action.md, confidence: extracted, source: .github/workflows/ci.yml }
  - { kind: configures, to: ../references/go-github-com-cespare-xxhash-v2.md, confidence: extracted, source: go.mod }
  - { kind: configures, to: ../references/go-github-com-go-logr-logr.md, confidence: extracted, source: go.mod }
  - { kind: configures, to: ../references/go-github-com-go-logr-stdr.md, confidence: extracted, source: go.mod }
  - { kind: configures, to: ../references/go-github-com-google-uuid.md, confidence: extracted, source: go.mod }
  - { kind: configures, to: ../references/go-go-opentelemetry-io-auto-sdk.md, confidence: extracted, source: go.mod }
  - { kind: configures, to: ../references/go-go-opentelemetry-io-otel.md, confidence: extracted, source: go.mod }
  - { kind: configures, to: ../references/go-go-opentelemetry-io-otel-metric.md, confidence: extracted, source: go.mod }
  - { kind: configures, to: ../references/go-go-opentelemetry-io-otel-sdk.md, confidence: extracted, source: go.mod }
  - { kind: configures, to: ../references/go-go-opentelemetry-io-otel-trace.md, confidence: extracted, source: go.mod }
  - { kind: configures, to: ../references/go-golang-org-x-sys.md, confidence: extracted, source: go.mod }
---
# (repository root)

<!-- signpost:managed:summary -->
2 powershell files; 10 exported symbols; entrypoint #!, param.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
2 files:
- `install.ps1`
- `install.sh`

- **Exports** (10): `Get-Arch`, `Get-LatestVersion`, `Write-Step`, `die`, `fetch`, `fetch_stdout`, `info`, `need`, `sha256`, `usage`

- **Changes with**: [internal/assemble](./assemble.md) ×17, [internal/discover](./discover.md) ×12, [internal/export](./export.md) ×2, [internal/extract](./extract.md) ×9, [internal/graph](./graph.md) ×3, [internal/hook](./hook.md) ×2, [internal/manifest](./manifest.md) ×7, [internal/model](./model.md) ×2, [internal/okf](./okf.md) ×11, [internal/practice](./practice.md) ×5, [internal/semantic](./semantic.md) ×3, [cmd/signpost](./signpost.md) ×35, [site](./site.md) ×14, [internal/vcs](./vcs.md) ×5

- **Configures**: [actions/cache](../references/github-actions-actions-cache.md), [actions/checkout](../references/github-actions-actions-checkout.md), [actions/configure-pages](../references/github-actions-actions-configure-pages.md), [actions/deploy-pages](../references/github-actions-actions-deploy-pages.md), [actions/setup-go](../references/github-actions-actions-setup-go.md), [actions/upload-pages-artifact](../references/github-actions-actions-upload-pages-artifact.md), [golangci/golangci-lint-action](../references/github-actions-golangci-golangci-lint-action.md), [github.com/cespare/xxhash/v2](../references/go-github-com-cespare-xxhash-v2.md), [github.com/go-logr/logr](../references/go-github-com-go-logr-logr.md), [github.com/go-logr/stdr](../references/go-github-com-go-logr-stdr.md), [github.com/google/uuid](../references/go-github-com-google-uuid.md), [go.opentelemetry.io/auto/sdk](../references/go-go-opentelemetry-io-auto-sdk.md), [go.opentelemetry.io/otel](../references/go-go-opentelemetry-io-otel.md), [go.opentelemetry.io/otel/metric](../references/go-go-opentelemetry-io-otel-metric.md), [go.opentelemetry.io/otel/sdk](../references/go-go-opentelemetry-io-otel-sdk.md), [go.opentelemetry.io/otel/trace](../references/go-go-opentelemetry-io-otel-trace.md), [golang.org/x/sys](../references/go-golang-org-x-sys.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
