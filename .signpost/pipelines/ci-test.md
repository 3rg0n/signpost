---
type: Pipeline
title: ci test
description: "CI job test (${{ matrix.os }}) in the ci workflow, 6 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@9423d6862a760d2846b57c80da3a96a280bf4409/.github/workflows/ci.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-08-17" }
attributes:
  - { name: job, value: "test (${{ matrix.os }})" }
  - { name: permissions, value: contents:read }
  - { name: runner, value: "${{ matrix.os }}" }
  - { name: runs, value: actions/checkout → actions/setup-go → Build → Vet → Test → Test with the race detector }
  - { name: steps, value: "6" }
  - { name: workflow, value: ci }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/ci.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/ci.yml }
---
# ci test

<!-- signpost:managed:summary -->
CI job test (${{ matrix.os }}) in the ci workflow, 6 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/ci.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md), [actions/setup-go](../references/github-actions-actions-setup-go.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
