---
type: Module
title: internal/assemble
description: 6 go files; 54 exported symbols.
resource: git://github.com/3rg0n/signpost@3bfbbaa669d02739674da97f5f1f490d8843e7ba/internal/assemble
generated: { by: signpost/dev, at: "2026-08-04" }
attributes:
  - { name: commits, value: "8" }
  - { name: exported, value: "54" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-04" }
  - { name: lines_added, value: "3876" }
  - { name: lines_removed, value: "50" }
  - { name: owners, value: "@3rg0n" }
  - { name: package, value: assemble }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: imports, to: ./extract.md, confidence: extracted, weight: 2, source: internal/assemble/assemble.go }
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 7 }
  - { kind: imports, to: ./vcs.md, confidence: extracted, weight: 3, source: internal/assemble/assemble.go }
---
# internal/assemble

<!-- signpost:managed:summary -->
6 go files; 54 exported symbols.
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

- **Changes with**: [internal/manifest](./manifest.md) ×2, [cmd/signpost](./signpost.md) ×7

- **Imports**: [internal/discover](./discover.md) ×4, [internal/extract](./extract.md) ×2, [internal/graph](./graph.md) ×4, [internal/manifest](./manifest.md) ×4, [internal/vcs](./vcs.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
