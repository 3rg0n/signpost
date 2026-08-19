---
type: Pipeline
title: signpost-semantic summarise modules with a model
description: "CI job summarise modules with a model in the signpost-semantic workflow, 8 steps"
resource: git://github.com/3rg0n/signpost@6ef04bd3596ca99913f76a401f4265368c1cd952/.github/workflows/signpost-semantic.yml
generated: { by: signpost/dev, at: "2026-08-19" }
attributes:
  - { name: job, value: summarise modules with a model }
  - { name: permissions, value: contents:write }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: actions/checkout → actions/setup-go → Is a model backend configured? → actions/cache → Build signpost → Rebuild the bundle with summaries → Verify strictly → Commit the summaries if they changed }
  - { name: steps, value: "8" }
  - { name: workflow, value: signpost-semantic }
edges:
  - { kind: configures, to: ../references/github-actions-actions-cache.md, confidence: extracted, source: .github/workflows/signpost-semantic.yml }
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/signpost-semantic.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/signpost-semantic.yml }
---
# signpost-semantic summarise modules with a model

<!-- signpost:managed:summary -->
CI job summarise modules with a model in the signpost-semantic workflow, 8 steps
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/signpost-semantic.yml`

- **Configures**: [actions/cache](../references/github-actions-actions-cache.md), [actions/checkout](../references/github-actions-actions-checkout.md), [actions/setup-go](../references/github-actions-actions-setup-go.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
