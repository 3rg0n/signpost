---
type: Module
title: internal/vcs
description: 6 go files; 13 exported symbols.
resource: git://github.com/3rg0n/signpost@82cb942ea2098d8c7d0a14ddd42b71a21b44db8b/internal/vcs
generated: { by: signpost/dev, at: "2026-08-11" }
attributes:
  - { name: commits, value: "5" }
  - { name: exported, value: "13" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-10" }
  - { name: lines_added, value: "2206" }
  - { name: lines_removed, value: "34" }
  - { name: package, value: vcs }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 60% }
edges:
  - { kind: co_changes, to: ./okf.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./okf.md, confidence: extracted, weight: 1, source: internal/vcs/git_test.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 5 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 5 }
---
# internal/vcs

<!-- signpost:managed:summary -->
6 go files; 13 exported symbols.
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

- **Exports** (13): `Commit`, `Commit.Short`, `DefaultMaxCommits`, `DefaultMaxDirsPerCommit`, `DefaultTimeout`, `Options`, `Pair`, `PathHistory`, `PathHistory.TopAuthor`, `Read`, `Signals`, `Signals.DirsSorted`, `Signals.PathsSorted`

- **Changes with**: [internal/okf](./okf.md) ×3, [\(repository root\)](./root.md) ×5, [cmd/signpost](./signpost.md) ×5

- **Imports**: [internal/okf](./okf.md) ×1
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
