---
type: Module
title: internal/vcs
description: 10 go files; 16 exported symbols.
resource: git://github.com/3rg0n/signpost@b79de0676f16cad7c9fc13a1d1ef719c22f2256d/internal/vcs
generated: { by: signpost/dev, at: "2026-08-19" }
attributes:
  - { name: commits, value: "6" }
  - { name: exported, value: "16" }
  - { name: files, value: "10" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-11" }
  - { name: lines_added, value: "3143" }
  - { name: lines_removed, value: "54" }
  - { name: package, value: vcs }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 67% }
edges:
  - { kind: co_changes, to: ./okf.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./okf.md, confidence: extracted, weight: 1, source: internal/vcs/git_test.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 6 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 6 }
---
# internal/vcs

<!-- signpost:managed:summary -->
10 go files; 16 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
10 files:
- `internal/vcs/aggregate_test.go`
- `internal/vcs/conventions.go`
- `internal/vcs/conventions_test.go`
- `internal/vcs/git.go`
- `internal/vcs/git_test.go`
- `internal/vcs/parse.go`
- `internal/vcs/parse_test.go`
- `internal/vcs/tags.go`
- `internal/vcs/tags_test.go`
- `internal/vcs/vcs.go`

- **Exports** (16): `Commit`, `Commit.Short`, `Conventions`, `Conventions.Available`, `DefaultMaxCommits`, `DefaultMaxDirsPerCommit`, `DefaultTimeout`, `Options`, `Pair`, `PathHistory`, `PathHistory.TopAuthor`, `Read`, `Releases`, `Signals`, `Signals.DirsSorted`, `Signals.PathsSorted`

- **Changes with**: [internal/okf](./okf.md) ×3, [\(repository root\)](./root.md) ×6, [cmd/signpost](./signpost.md) ×6

- **Imports**: [internal/okf](./okf.md) ×1
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
