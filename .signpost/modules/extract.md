---
type: Module
title: internal/extract
description: 20 go files; 257 exported symbols.
resource: git://github.com/3rg0n/signpost@b688d2bd0693ec00c1f0d3c4119919c84978a5dc/internal/extract
generated: { by: signpost/dev, at: "2026-08-07" }
attributes:
  - { name: commits, value: "6" }
  - { name: exported, value: "257" }
  - { name: files, value: "20" }
  - { name: first_commit, value: "2026-07-29" }
  - { name: last_commit, value: "2026-08-07" }
  - { name: lines_added, value: "11193" }
  - { name: lines_removed, value: "30" }
  - { name: package, value: extract }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 67% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 18, source: internal/extract/cfamily.go }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 2 }
---
# internal/extract

<!-- signpost:managed:summary -->
20 go files; 257 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
20 files:
- `internal/extract/cfamily.go`
- `internal/extract/cfamily_test.go`
- `internal/extract/extract.go`
- `internal/extract/extract_test.go`
- `internal/extract/golang.go`
- `internal/extract/golang_test.go`
- `internal/extract/java.go`
- `internal/extract/java_test.go`
- `internal/extract/kotlin.go`
- `internal/extract/kotlin_test.go`
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

- **Changes with**: [internal/assemble](./assemble.md) ×2, [internal/discover](./discover.md) ×4, [cmd/signpost](./signpost.md) ×2, [site](./site.md) ×2

- **Imports**: [internal/discover](./discover.md) ×18
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
