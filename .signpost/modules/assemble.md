---
type: Module
title: internal/assemble
description: 7 go files; 3 exported symbols.
resource: git://github.com/3rg0n/signpost@fc4af02fdfa37579f05a7855ddd1be7f2607689f/internal/assemble
generated: { by: signpost/dev, at: "2026-08-19" }
attributes:
  - { name: commits, value: "20" }
  - { name: exported, value: "3" }
  - { name: files, value: "7" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-15" }
  - { name: lines_added, value: "7266" }
  - { name: lines_removed, value: "150" }
  - { name: owners, value: "@3rg0n" }
  - { name: package, value: assemble }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 55% }
edges:
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 7 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./export.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./extract.md, confidence: extracted, weight: 6 }
  - { kind: imports, to: ./extract.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./graph.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 5, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 7 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./okf.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./practice.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 19 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 19 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 6 }
  - { kind: imports, to: ./sqlstmt.md, confidence: extracted, weight: 1, source: internal/assemble/assemble.go }
  - { kind: imports, to: ./vcs.md, confidence: extracted, weight: 3, source: internal/assemble/assemble.go }
---
# internal/assemble

<!-- signpost:managed:summary -->
7 go files; 3 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
7 files:
- `internal/assemble/assemble.go`
- `internal/assemble/assemble_test.go`
- `internal/assemble/data_test.go`
- `internal/assemble/describe.go`
- `internal/assemble/history_test.go`
- `internal/assemble/id.go`
- `internal/assemble/resolve.go`

- **Exports** (3): `Build`, `Input`, `Result`

- **Changes with**: [internal/discover](./discover.md) ×7, [internal/export](./export.md) ×2, [internal/extract](./extract.md) ×6, [internal/graph](./graph.md) ×3, [internal/manifest](./manifest.md) ×7, [internal/okf](./okf.md) ×3, [internal/practice](./practice.md) ×2, [\(repository root\)](./root.md) ×19, [cmd/signpost](./signpost.md) ×19, [site](./site.md) ×6

- **Imports**: [internal/discover](./discover.md) ×4, [internal/extract](./extract.md) ×4, [internal/graph](./graph.md) ×5, [internal/manifest](./manifest.md) ×4, [internal/sqlstmt](./sqlstmt.md) ×1, [internal/vcs](./vcs.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
