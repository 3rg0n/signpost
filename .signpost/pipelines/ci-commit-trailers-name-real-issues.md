---
type: Pipeline
title: ci commit trailers name real issues
description: "CI job commit trailers name real issues in the ci workflow, 2 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@fd881702084abbf7ca124cb20e883404e112b991/.github/workflows/ci.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-08-19" }
attributes:
  - { name: job, value: commit trailers name real issues }
  - { name: permissions, value: "contents:read, issues:read, pull-requests:read" }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: actions/checkout → commit references an issue that exists }
  - { name: steps, value: "2" }
  - { name: workflow, value: ci }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/ci.yml }
---
# ci commit trailers name real issues

<!-- signpost:managed:summary -->
CI job commit trailers name real issues in the ci workflow, 2 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/ci.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
