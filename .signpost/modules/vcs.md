---
type: Module
title: internal/vcs
description: 6 go files; 55 exported symbols.
resource: git://github.com/3rg0n/signpost@05c19a0d8af556b5518603dc7f516086461ac852/internal/vcs
generated: { by: signpost/dev, at: "2026-08-05" }
attributes:
  - { name: commits, value: "4" }
  - { name: exported, value: "55" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-07-31" }
  - { name: lines_added, value: "1902" }
  - { name: lines_removed, value: "16" }
  - { name: package, value: vcs }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 50% }
edges:
  - { kind: co_changes, to: ./okf.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./okf.md, confidence: extracted, weight: 1, source: internal/vcs/git_test.go }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 4 }
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

- **Changes with**: [internal/okf](./okf.md) ×2, [cmd/signpost](./signpost.md) ×4

- **Imports**: [internal/okf](./okf.md) ×1
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
