---
type: Module
title: internal/vcs
description: 6 go files; 55 exported symbols.
resource: git://github.com/3rg0n/signpost@2cd4f8bc004339d2aea2ae4fb417b773e0bf044d/internal/vcs
generated: { by: signpost/dev, at: "2026-07-30" }
attributes:
  - { name: commits, value: "3" }
  - { name: exported, value: "55" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-07-30" }
  - { name: lines_added, value: "1872" }
  - { name: lines_removed, value: "10" }
  - { name: package, value: vcs }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 67% }
edges:
  - { kind: co_changes, to: /modules/okf.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: /modules/okf.md, confidence: extracted, weight: 1, source: internal/vcs/git_test.go }
  - { kind: co_changes, to: /modules/signpost.md, confidence: extracted, weight: 3 }
---
# internal/vcs

<!-- signpost:managed:summary -->
6 go files; 55 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
6 files:
- `internal/vcs/aggregate_test.go`
- `internal/vcs/git.go`
- `internal/vcs/git_test.go`
- `internal/vcs/parse.go`
- `internal/vcs/parse_test.go`
- `internal/vcs/vcs.go`

- **Changes with**: [internal/okf](/modules/okf.md) ×2, [cmd/signpost](/modules/signpost.md) ×3

- **Imports**: [internal/okf](/modules/okf.md) ×1
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
