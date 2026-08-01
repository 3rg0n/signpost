---
type: Module
title: internal/assemble
description: 6 go files; 39 exported symbols.
resource: git://github.com/3rg0n/signpost@a6fa38aba4861a1d7d1c7acc3e5d5616a37c4a76/internal/assemble
generated: { by: signpost/dev, at: "2026-08-01" }
attributes:
  - { name: commits, value: "3" }
  - { name: exported, value: "39" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-01" }
  - { name: lines_added, value: "2853" }
  - { name: lines_removed, value: "3" }
  - { name: owners, value: "@3rg0n" }
  - { name: package, value: assemble }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: imports, to: /modules/discover.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: imports, to: /modules/extract.md, confidence: extracted, weight: 2, source: internal/assemble/assemble.go }
  - { kind: imports, to: /modules/graph.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: imports, to: /modules/manifest.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: /modules/signpost.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: /modules/vcs.md, confidence: extracted, weight: 3, source: internal/assemble/assemble.go }
---
# internal/assemble

<!-- signpost:managed:summary -->
6 go files; 39 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
6 files:
- `internal/assemble/assemble.go`
- `internal/assemble/assemble_test.go`
- `internal/assemble/describe.go`
- `internal/assemble/history_test.go`
- `internal/assemble/id.go`
- `internal/assemble/resolve.go`

- **Changes with**: [cmd/signpost](/modules/signpost.md) ×2

- **Imports**: [internal/discover](/modules/discover.md) ×4, [internal/extract](/modules/extract.md) ×2, [internal/graph](/modules/graph.md) ×4, [internal/manifest](/modules/manifest.md) ×4, [internal/vcs](/modules/vcs.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
