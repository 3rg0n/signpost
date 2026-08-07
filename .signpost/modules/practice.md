---
type: Module
title: internal/practice
description: 3 go files; 31 exported symbols.
resource: git://github.com/3rg0n/signpost@fc6e5606c41f30b83f1d2b144a451dd9f2e5a355/internal/practice
generated: { by: signpost/dev, at: "2026-08-07" }
attributes:
  - { name: commits, value: "4" }
  - { name: exported, value: "31" }
  - { name: files, value: "3" }
  - { name: first_commit, value: "2026-08-01" }
  - { name: last_commit, value: "2026-08-07" }
  - { name: lines_added, value: "1645" }
  - { name: lines_removed, value: "5" }
  - { name: package, value: practice }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 75% }
edges:
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 2, source: internal/practice/practice.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 2, source: internal/practice/practice.go }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 4 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 3 }
---
# internal/practice

<!-- signpost:managed:summary -->
3 go files; 31 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
3 files:
- `internal/practice/practice.go`
- `internal/practice/practice_test.go`
- `internal/practice/render.go`

- **Changes with**: [internal/discover](./discover.md) ×3, [internal/manifest](./manifest.md) ×3, [cmd/signpost](./signpost.md) ×4, [site](./site.md) ×3

- **Imports**: [internal/discover](./discover.md) ×2, [internal/manifest](./manifest.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
