---
type: Pipeline
title: ci security
description: "CI job security in the ci workflow, 5 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@9423d6862a760d2846b57c80da3a96a280bf4409/.github/workflows/ci.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-08-17" }
attributes:
  - { name: job, value: security }
  - { name: permissions, value: contents:read }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: actions/checkout → actions/setup-go → govulncheck → gosec → gitleaks }
  - { name: steps, value: "5" }
  - { name: workflow, value: ci }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/ci.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/ci.yml }
---
# ci security

<!-- signpost:managed:summary -->
CI job security in the ci workflow, 5 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/ci.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md), [actions/setup-go](../references/github-actions-actions-setup-go.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
