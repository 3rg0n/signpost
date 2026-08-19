---
type: Pipeline
title: ci corpus (a repository signpost did not write)
description: "CI job corpus (a repository signpost did not write) in the ci workflow, 22 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@b79de0676f16cad7c9fc13a1d1ef719c22f2256d/.github/workflows/ci.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-08-19" }
attributes:
  - { name: job, value: corpus (a repository signpost did not write) }
  - { name: permissions, value: contents:read }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: actions/checkout → actions/setup-go → Build → Stage the corpus as its own repository → Build and verify the corpus bundle → Every page's frontmatter parses under a conforming YAML reader → The hostile paths reached the bundle → Secrets are attributed to the service that reads them → +14 more }
  - { name: steps, value: "22" }
  - { name: workflow, value: ci }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/ci.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/ci.yml }
---
# ci corpus (a repository signpost did not write)

<!-- signpost:managed:summary -->
CI job corpus (a repository signpost did not write) in the ci workflow, 22 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/ci.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md), [actions/setup-go](../references/github-actions-actions-setup-go.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
