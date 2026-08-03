---
type: Module
title: internal/assemble
description: 6 go files; 51 exported symbols.
resource: git://github.com/3rg0n/signpost@168ccfd4ee0b087f79607409cdc39e074991f1df/internal/assemble
generated: { by: signpost/dev, at: "2026-08-03" }
attributes:
  - { name: commits, value: "7" }
  - { name: exported, value: "51" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-03" }
  - { name: lines_added, value: "3686" }
  - { name: lines_removed, value: "45" }
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
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 6 }
  - { kind: imports, to: ./vcs.md, confidence: extracted, weight: 3, source: internal/assemble/assemble.go }
---
# internal/assemble

<!-- signpost:managed:summary -->
6 go files; 51 exported symbols.
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

- **Changes with**: [internal/manifest](./manifest.md) ×2, [cmd/signpost](./signpost.md) ×6

- **Imports**: [internal/discover](./discover.md) ×4, [internal/extract](./extract.md) ×2, [internal/graph](./graph.md) ×4, [internal/manifest](./manifest.md) ×4, [internal/vcs](./vcs.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
