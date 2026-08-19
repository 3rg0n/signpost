---
type: Pipeline
title: signpost rebuild the bundle
description: "CI job rebuild the bundle in the signpost workflow, 6 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@6ef04bd3596ca99913f76a401f4265368c1cd952/.github/workflows/signpost.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-08-19" }
attributes:
  - { name: job, value: rebuild the bundle }
  - { name: permissions, value: contents:write }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: actions/checkout → actions/setup-go → Build signpost → Rebuild the bundle → Verify strictly → Commit the bundle if it changed }
  - { name: steps, value: "6" }
  - { name: workflow, value: signpost }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/signpost.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/signpost.yml }
---
# signpost rebuild the bundle

<!-- signpost:managed:summary -->
CI job rebuild the bundle in the signpost workflow, 6 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/signpost.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md), [actions/setup-go](../references/github-actions-actions-setup-go.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
