---
type: Pipeline
title: ci installer parses (5.1 and 7)
description: "CI job installer parses (5.1 and 7) in the ci workflow, 3 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@822d068b2c3d5e794e1d629591d395182a789e91/.github/workflows/ci.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-10-07" }
attributes:
  - { name: job, value: installer parses (5.1 and 7) }
  - { name: permissions, value: contents:read }
  - { name: runner, value: windows-2025 }
  - { name: runs, value: actions/checkout → Parse under Windows PowerShell 5.1 → Parse under PowerShell 7 }
  - { name: steps, value: "3" }
  - { name: workflow, value: ci }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/ci.yml }
---
# ci installer parses (5.1 and 7)

<!-- signpost:managed:summary -->
CI job installer parses (5.1 and 7) in the ci workflow, 3 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/ci.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
