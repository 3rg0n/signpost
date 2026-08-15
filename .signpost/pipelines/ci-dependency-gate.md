---
type: Pipeline
title: ci dependency gate
description: "CI job dependency gate in the ci workflow, 4 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@4a1eb7582c195b6ae366c3821a303f66ed639eb8/.github/workflows/ci.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-08-15" }
attributes:
  - { name: job, value: dependency gate }
  - { name: permissions, value: contents:read }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: actions/checkout → actions/setup-go → Tidy is committed → A new direct dependency requires an ADR }
  - { name: steps, value: "4" }
  - { name: workflow, value: ci }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/ci.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/ci.yml }
---
# ci dependency gate

<!-- signpost:managed:summary -->
CI job dependency gate in the ci workflow, 4 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/ci.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md), [actions/setup-go](../references/github-actions-actions-setup-go.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
