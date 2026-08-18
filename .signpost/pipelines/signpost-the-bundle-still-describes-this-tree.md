---
type: Pipeline
title: signpost the bundle still describes this tree
description: "CI job the bundle still describes this tree in the signpost workflow, 4 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@7e60b898c22eb0414d3310c78c40d1edee929c09/.github/workflows/signpost.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-08-18" }
attributes:
  - { name: job, value: the bundle still describes this tree }
  - { name: permissions, value: contents:read }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: actions/checkout → actions/setup-go → Build signpost → Verify the bundle }
  - { name: steps, value: "4" }
  - { name: workflow, value: signpost }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/signpost.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/signpost.yml }
---
# signpost the bundle still describes this tree

<!-- signpost:managed:summary -->
CI job the bundle still describes this tree in the signpost workflow, 4 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/signpost.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md), [actions/setup-go](../references/github-actions-actions-setup-go.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
