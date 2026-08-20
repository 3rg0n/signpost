---
type: Module
title: internal/practice
description: 4 go files; 18 exported symbols.
resource: git://github.com/3rg0n/signpost@d1306301254e8ea7e9b378ae577464dd2212a84c/internal/practice
generated: { by: signpost/dev, at: "2026-08-21" }
attributes:
  - { name: commits, value: "7" }
  - { name: exported, value: "18" }
  - { name: files, value: "4" }
  - { name: first_commit, value: "2026-08-01" }
  - { name: last_commit, value: "2026-08-16" }
  - { name: lines_added, value: "2321" }
  - { name: lines_removed, value: "37" }
  - { name: package, value: practice }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 57% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 2, source: internal/practice/practice.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 2, source: internal/practice/practice.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 7 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 7 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./vcs.md, confidence: extracted, weight: 2, source: internal/practice/history_test.go }
---
# internal/practice

<!-- signpost:managed:summary -->
4 go files; 18 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
4 files:
- `internal/practice/history_test.go`
- `internal/practice/practice.go`
- `internal/practice/practice_test.go`
- `internal/practice/render.go`

- **Exports** (18): `Analyse`, `Finding`, `Input`, `Result`, `Result.Absent`, `Result.Declared`, `Result.Render`, `Source`, `Topic`, `TopicAgentRules`, `TopicBuild`, `TopicDependencies`, `TopicDocumentation`, `TopicGates`, `TopicHistory`, `TopicObservability`, `TopicOwnership`, `TopicTest`

- **Changes with**: [internal/assemble](./assemble.md) ×2, [internal/discover](./discover.md) ×4, [internal/manifest](./manifest.md) ×4, [\(repository root\)](./root.md) ×7, [cmd/signpost](./signpost.md) ×7, [site](./site.md) ×4

- **Imports**: [internal/discover](./discover.md) ×2, [internal/manifest](./manifest.md) ×2, [internal/vcs](./vcs.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
