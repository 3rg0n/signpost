---
type: Module
title: internal/practice
description: 3 go files; 29 exported symbols.
resource: git://github.com/3rg0n/signpost@d0ea89253a2bdcb6948c4746ef4cd98bce8db2b0/internal/practice
generated: { by: signpost/dev, at: "2026-08-02" }
attributes:
  - { name: commits, value: "2" }
  - { name: exported, value: "29" }
  - { name: files, value: "3" }
  - { name: first_commit, value: "2026-08-01" }
  - { name: last_commit, value: "2026-08-02" }
  - { name: lines_added, value: "1490" }
  - { name: lines_removed, value: "2" }
  - { name: package, value: practice }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 2, source: internal/practice/practice.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 2, source: internal/practice/practice.go }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 2 }
---
# internal/practice

<!-- signpost:managed:summary -->
3 go files; 29 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
3 files:
- `internal/practice/practice.go`
- `internal/practice/practice_test.go`
- `internal/practice/render.go`

- **Changes with**: [internal/discover](./discover.md) ×2, [internal/manifest](./manifest.md) ×2, [cmd/signpost](./signpost.md) ×2

- **Imports**: [internal/discover](./discover.md) ×2, [internal/manifest](./manifest.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
