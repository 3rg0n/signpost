---
type: Pipeline
title: release build and publish
description: "CI job build and publish in the release workflow, 5 steps"
resource: git://github.com/3rg0n/signpost@84f4b54f22fbae91331c174d67531defae5e4faf/.github/workflows/release.yml
generated: { by: signpost/dev, at: "2026-08-20" }
attributes:
  - { name: job, value: build and publish }
  - { name: permissions, value: contents:write }
  - { name: runner, value: ubuntu-24.04 }
  - { name: runs, value: Checkout → Set up Go → Verify → Build → Publish }
  - { name: steps, value: "5" }
  - { name: workflow, value: release }
edges:
  - { kind: configures, to: ../references/github-actions-actions-checkout.md, confidence: extracted, source: .github/workflows/release.yml }
  - { kind: configures, to: ../references/github-actions-actions-setup-go.md, confidence: extracted, source: .github/workflows/release.yml }
---
# release build and publish

<!-- signpost:managed:summary -->
CI job build and publish in the release workflow, 5 steps
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `.github/workflows/release.yml`

- **Configures**: [actions/checkout](../references/github-actions-actions-checkout.md), [actions/setup-go](../references/github-actions-actions-setup-go.md)
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
