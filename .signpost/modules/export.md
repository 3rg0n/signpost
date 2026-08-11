---
type: Module
title: internal/export
description: 6 go files; 7 exported symbols.
resource: git://github.com/3rg0n/signpost@073b67069d8e856c7451f4ec02e65165995e11a6/internal/export
generated: { by: signpost/dev, at: "2026-08-11" }
attributes:
  - { name: commits, value: "3" }
  - { name: exported, value: "7" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-10" }
  - { name: lines_added, value: "1117" }
  - { name: lines_removed, value: "4" }
  - { name: owners, value: "@3rg0n" }
  - { name: package, value: export }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 67% }
edges:
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 6, source: internal/export/dot.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 2 }
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

- **Changes with**: [\(repository root\)](./root.md) ×2

- **Imports**: [internal/graph](./graph.md) ×6
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
