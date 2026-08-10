---
type: Module
title: internal/practice
description: 3 go files; 17 exported symbols.
resource: git://github.com/3rg0n/signpost@82cb942ea2098d8c7d0a14ddd42b71a21b44db8b/internal/practice
generated: { by: signpost/dev, at: "2026-08-11" }
attributes:
  - { name: commits, value: "5" }
  - { name: exported, value: "17" }
  - { name: files, value: "3" }
  - { name: first_commit, value: "2026-08-01" }
  - { name: last_commit, value: "2026-08-09" }
  - { name: lines_added, value: "1801" }
  - { name: lines_removed, value: "8" }
  - { name: package, value: practice }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 60% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 2, source: internal/practice/practice.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 2, source: internal/practice/practice.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 5 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 5 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 4 }
---
# internal/practice

<!-- signpost:managed:summary -->
3 go files; 17 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
3 files:
- `internal/practice/practice.go`
- `internal/practice/practice_test.go`
- `internal/practice/render.go`

- **Exports** (17): `Analyse`, `Finding`, `Input`, `Result`, `Result.Absent`, `Result.Declared`, `Result.Render`, `Source`, `Topic`, `TopicAgentRules`, `TopicBuild`, `TopicDependencies`, `TopicDocumentation`, `TopicGates`, `TopicObservability`, `TopicOwnership`, `TopicTest`

- **Changes with**: [internal/assemble](./assemble.md) ×2, [internal/discover](./discover.md) ×4, [internal/manifest](./manifest.md) ×4, [\(repository root\)](./root.md) ×5, [cmd/signpost](./signpost.md) ×5, [site](./site.md) ×4

- **Imports**: [internal/discover](./discover.md) ×2, [internal/manifest](./manifest.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
