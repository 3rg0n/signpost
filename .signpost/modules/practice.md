---
type: Module
title: internal/practice
description: 4 go files; 18 exported symbols.
resource: git://github.com/3rg0n/signpost@49f8bbc304ae1463de2155ed03ae127951d56194/internal/practice
generated: { by: signpost/dev, at: "2026-08-12" }
attributes:
  - { name: commits, value: "6" }
  - { name: exported, value: "18" }
  - { name: files, value: "4" }
  - { name: first_commit, value: "2026-08-01" }
  - { name: last_commit, value: "2026-08-11" }
  - { name: lines_added, value: "2203" }
  - { name: lines_removed, value: "9" }
  - { name: package, value: practice }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 50% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 2, source: internal/practice/practice.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 2, source: internal/practice/practice.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 6 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 6 }
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

- **Changes with**: [internal/assemble](./assemble.md) ×2, [internal/discover](./discover.md) ×4, [internal/manifest](./manifest.md) ×4, [\(repository root\)](./root.md) ×6, [cmd/signpost](./signpost.md) ×6, [site](./site.md) ×4

- **Imports**: [internal/discover](./discover.md) ×2, [internal/manifest](./manifest.md) ×2, [internal/vcs](./vcs.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
