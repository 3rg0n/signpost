---
type: Module
title: internal/semantic
description: 3 go files; 35 exported symbols.
resource: git://github.com/3rg0n/signpost@5f7e8c48abb18c2892d691dd3893039cce72d5c5/internal/semantic
generated: { by: signpost/dev, at: "2026-08-01" }
attributes:
  - { name: commits, value: "2" }
  - { name: exported, value: "35" }
  - { name: files, value: "3" }
  - { name: first_commit, value: "2026-07-31" }
  - { name: last_commit, value: "2026-07-31" }
  - { name: lines_added, value: "1536" }
  - { name: lines_removed, value: "1" }
  - { name: package, value: semantic }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: imports, to: /modules/discover.md, confidence: extracted, weight: 2, source: internal/semantic/semantic.go }
  - { kind: imports, to: /modules/graph.md, confidence: extracted, weight: 3, source: internal/semantic/prompt.go }
  - { kind: imports, to: /modules/model.md, confidence: extracted, weight: 3, source: internal/semantic/prompt.go }
  - { kind: co_changes, to: /modules/signpost.md, confidence: extracted, weight: 2 }
---
# internal/semantic

<!-- signpost:managed:summary -->
3 go files; 35 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
3 files:
- `internal/semantic/prompt.go`
- `internal/semantic/semantic.go`
- `internal/semantic/semantic_test.go`

- **Changes with**: [cmd/signpost](/modules/signpost.md) ×2

- **Imports**: [internal/discover](/modules/discover.md) ×2, [internal/graph](/modules/graph.md) ×3, [internal/model](/modules/model.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
