---
type: Module
title: internal/export
description: 6 go files; 7 exported symbols.
resource: git://github.com/3rg0n/signpost@b4c8f5076ffb2ea629c1401ed3d7029542906974/internal/export
generated: { by: signpost/dev, at: "2026-08-16" }
attributes:
  - { name: commits, value: "4" }
  - { name: exported, value: "7" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-15" }
  - { name: lines_added, value: "1125" }
  - { name: lines_removed, value: "5" }
  - { name: owners, value: "@3rg0n" }
  - { name: package, value: export }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 75% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./graph.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 6, source: internal/export/dot.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 3 }
---
# internal/export

<!-- signpost:managed:summary -->
6 go files; 7 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
6 files:
- `internal/export/dot.go`
- `internal/export/export.go`
- `internal/export/export_test.go`
- `internal/export/graphml.go`
- `internal/export/json.go`
- `internal/export/mermaid.go`

- **Exports** (7): `Format`, `FormatDOT`, `FormatGraphML`, `FormatJSON`, `FormatMermaid`, `Formats`, `Write`

- **Changes with**: [internal/assemble](./assemble.md) ×2, [internal/graph](./graph.md) ×2, [\(repository root\)](./root.md) ×3

- **Imports**: [internal/graph](./graph.md) ×6
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
