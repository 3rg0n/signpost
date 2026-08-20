---
type: Module
title: internal/view
description: 5 go files; 4 exported symbols.
resource: git://github.com/3rg0n/signpost@ac0a450e63beff43084c57b386d0b89ff72f950f/internal/view
generated: { by: signpost/dev, at: "2026-08-20" }
attributes:
  - { name: commits, value: "3" }
  - { name: exported, value: "4" }
  - { name: files, value: "5" }
  - { name: first_commit, value: "2026-08-06" }
  - { name: last_commit, value: "2026-08-12" }
  - { name: lines_added, value: "1703" }
  - { name: lines_removed, value: "6" }
  - { name: package, value: view }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./site.md, confidence: extracted, weight: 2, source: internal/view/view.go }
---
# internal/view

<!-- signpost:managed:summary -->
5 go files; 4 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
5 files:
- `internal/view/browser.go`
- `internal/view/static.go`
- `internal/view/static_test.go`
- `internal/view/view.go`
- `internal/view/view_test.go`

- **Exports** (4): `DefaultPort`, `Options`, `Serve`, `WriteStatic`

- **Changes with**: [\(repository root\)](./root.md) ×3, [cmd/signpost](./signpost.md) ×3, [site](./site.md) ×2

- **Imports**: [site](./site.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
