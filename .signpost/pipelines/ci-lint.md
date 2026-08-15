---
type: Pipeline
title: ci lint
description: "CI job lint in the ci workflow, 8 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@f1c85cb4603e356013b50452bd0aa50f8b4d1192/.github/workflows/ci.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-08-15" }
attributes:
  - { name: job, value: lint }
  - { name: permissions, value: contents:read }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: actions/checkout → actions/setup-go → gofmt → golangci/golangci-lint-action → staticcheck → shellcheck the installer → actionlint → installers are pure ASCII }
  - { name: steps, value: "8" }
  - { name: workflow, value: ci }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/ci.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/ci.yml }
  - { kind: configures, to: ../references/github-actions-golangci-golangci-lint-action.md, confidence: extracted, source: .github/workflows/ci.yml }
---
# ci lint

<!-- signpost:managed:summary -->
CI job lint in the ci workflow, 8 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/ci.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md), [actions/setup-go](../references/github-actions-actions-setup-go.md), [golangci/golangci-lint-action](../references/github-actions-golangci-golangci-lint-action.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
