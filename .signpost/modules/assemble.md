---
type: Module
title: internal/assemble
description: 6 go files; 68 exported symbols.
resource: git://github.com/3rg0n/signpost@fc6e5606c41f30b83f1d2b144a451dd9f2e5a355/internal/assemble
generated: { by: signpost/dev, at: "2026-08-07" }
attributes:
  - { name: commits, value: "13" }
  - { name: exported, value: "68" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-07" }
  - { name: lines_added, value: "5779" }
  - { name: lines_removed, value: "136" }
  - { name: owners, value: "@3rg0n" }
  - { name: package, value: assemble }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 85% }
edges:
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./extract.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./extract.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 12 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./vcs.md, confidence: extracted, weight: 3, source: internal/assemble/assemble.go }
---
# internal/assemble

<!-- signpost:managed:summary -->
6 go files; 68 exported symbols.
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

- **Changes with**: [internal/discover](./discover.md) ×4, [internal/extract](./extract.md) ×3, [internal/manifest](./manifest.md) ×4, [cmd/signpost](./signpost.md) ×12, [site](./site.md) ×3

- **Imports**: [internal/discover](./discover.md) ×4, [internal/extract](./extract.md) ×4, [internal/graph](./graph.md) ×4, [internal/manifest](./manifest.md) ×4, [internal/vcs](./vcs.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
