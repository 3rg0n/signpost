---
type: Module
title: internal/extract
description: 26 go files; 304 exported symbols.
resource: git://github.com/3rg0n/signpost@fc6e5606c41f30b83f1d2b144a451dd9f2e5a355/internal/extract
generated: { by: signpost/dev, at: "2026-08-07" }
attributes:
  - { name: commits, value: "7" }
  - { name: exported, value: "304" }
  - { name: files, value: "26" }
  - { name: first_commit, value: "2026-07-29" }
  - { name: last_commit, value: "2026-08-07" }
  - { name: lines_added, value: "15045" }
  - { name: lines_removed, value: "34" }
  - { name: package, value: extract }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 57% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 5 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 24, source: internal/extract/cfamily.go }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 3 }
---
# internal/extract

<!-- signpost:managed:summary -->
26 go files; 304 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
26 files:
- `internal/extract/cfamily.go`
- `internal/extract/cfamily_test.go`
- `internal/extract/csharp.go`
- `internal/extract/csharp_test.go`
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
- `internal/extract/php.go`
- `internal/extract/php_test.go`
- `internal/extract/python.go`
- `internal/extract/python_test.go`
- `internal/extract/ruby.go`
- `internal/extract/ruby_test.go`
- `internal/extract/rust.go`
- `internal/extract/rust_test.go`
- `internal/extract/score.go`
- `internal/extract/score_test.go`
- `internal/extract/typescript.go`
- `internal/extract/typescript_test.go`

- **Changes with**: [internal/assemble](./assemble.md) ×3, [internal/discover](./discover.md) ×5, [cmd/signpost](./signpost.md) ×3, [site](./site.md) ×3

- **Imports**: [internal/discover](./discover.md) ×24
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
