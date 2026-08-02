---
type: Module
title: internal/assemble
description: 6 go files; 48 exported symbols.
resource: git://github.com/3rg0n/signpost@278591843f208cdf9fab889edbe7d6dc5e89c551/internal/assemble
generated: { by: signpost/dev, at: "2026-08-02" }
attributes:
  - { name: commits, value: "6" }
  - { name: exported, value: "48" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-01" }
  - { name: lines_added, value: "3470" }
  - { name: lines_removed, value: "9" }
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
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 5 }
  - { kind: imports, to: ./vcs.md, confidence: extracted, weight: 3, source: internal/assemble/assemble.go }
---
# internal/assemble

<!-- signpost:managed:summary -->
6 go files; 48 exported symbols.
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

- **Changes with**: [internal/manifest](./manifest.md) ×2, [cmd/signpost](./signpost.md) ×5

- **Imports**: [internal/discover](./discover.md) ×4, [internal/extract](./extract.md) ×2, [internal/graph](./graph.md) ×4, [internal/manifest](./manifest.md) ×4, [internal/vcs](./vcs.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
