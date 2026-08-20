---
type: Pipeline
title: pages deploy
description: "CI job deploy in the pages workflow, 7 steps; runs on a pull request or a default-branch push"
resource: git://github.com/3rg0n/signpost@ac0a450e63beff43084c57b386d0b89ff72f950f/.github/workflows/pages.yml
tags: [gate]
generated: { by: signpost/dev, at: "2026-08-20" }
attributes:
  - { name: job, value: deploy }
  - { name: permissions, value: "id-token:write, pages:write" }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: actions/checkout → actions/setup-go → Generate the graph the viewer reads → Check the custom domain will survive this deploy → actions/configure-pages → actions/upload-pages-artifact → actions/deploy-pages }
  - { name: steps, value: "7" }
  - { name: workflow, value: pages }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/pages.yml }
  - { kind: configures, to: ../references/github-actions-actions-configure-pages.md, confidence: extracted, source: .github/workflows/pages.yml }
  - { kind: configures, to: ../references/github-actions-actions-deploy-pages.md, confidence: extracted, source: .github/workflows/pages.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/pages.yml }
  - { kind: configures, to: ../references/github-actions-actions-upload-pages-artifact.md, confidence: extracted, source: .github/workflows/pages.yml }
---
# pages deploy

<!-- signpost:managed:summary -->
CI job deploy in the pages workflow, 7 steps; runs on a pull request or a default-branch push
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/pages.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md), [actions/configure-pages](../references/github-actions-actions-configure-pages.md), [actions/deploy-pages](../references/github-actions-actions-deploy-pages.md), [actions/setup-go](../references/github-actions-actions-setup-go.md), [actions/upload-pages-artifact](../references/github-actions-actions-upload-pages-artifact.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
