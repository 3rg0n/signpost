---
type: Module
title: internal/extract
description: 14 go files; 189 exported symbols.
resource: git://github.com/3rg0n/signpost@585677e9112289b405d046790a9b8af1e40c8232/internal/extract
generated: { by: signpost/dev, at: "2026-08-01" }
attributes:
  - { name: commits, value: "4" }
  - { name: exported, value: "189" }
  - { name: files, value: "14" }
  - { name: first_commit, value: "2026-07-29" }
  - { name: last_commit, value: "2026-07-31" }
  - { name: lines_added, value: "6885" }
  - { name: lines_removed, value: "20" }
  - { name: package, value: extract }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 75% }
edges:
  - { kind: co_changes, to: /modules/discover.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: /modules/discover.md, confidence: extracted, weight: 12, source: internal/extract/extract.go }
---
# internal/extract

<!-- signpost:managed:summary -->
14 go files; 189 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
14 files:
- `internal/extract/extract.go`
- `internal/extract/extract_test.go`
- `internal/extract/golang.go`
- `internal/extract/golang_test.go`
- `internal/extract/lines.go`
- `internal/extract/lines_test.go`
- `internal/extract/python.go`
- `internal/extract/python_test.go`
- `internal/extract/rust.go`
- `internal/extract/rust_test.go`
- `internal/extract/score.go`
- `internal/extract/score_test.go`
- `internal/extract/typescript.go`
- `internal/extract/typescript_test.go`

- **Changes with**: [internal/discover](/modules/discover.md) ×2

- **Imports**: [internal/discover](/modules/discover.md) ×12
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
