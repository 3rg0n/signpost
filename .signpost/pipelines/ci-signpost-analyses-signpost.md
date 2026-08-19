---
type: Pipeline
title: ci signpost analyses signpost
description: "CI job signpost analyses signpost in the ci workflow, 13 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@7441a5d393b73195ebaf1116d194318a7b9b36dd/.github/workflows/ci.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-08-19" }
attributes:
  - { name: job, value: signpost analyses signpost }
  - { name: permissions, value: contents:read }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: actions/checkout → actions/setup-go → Build → The command surface holds → Analyse this repository → Coverage floors hold → Output is byte-identical across runs → Coverage report names its gaps → +5 more }
  - { name: steps, value: "13" }
  - { name: workflow, value: ci }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/ci.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/ci.yml }
---
# ci signpost analyses signpost

<!-- signpost:managed:summary -->
CI job signpost analyses signpost in the ci workflow, 13 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/ci.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md), [actions/setup-go](../references/github-actions-actions-setup-go.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
