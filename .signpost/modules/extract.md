---
type: Module
title: internal/extract
description: 32 go files; 356 exported symbols.
resource: git://github.com/3rg0n/signpost@9a47aec509a15a7dc6e15a56901c8fe035e50358/internal/extract
generated: { by: signpost/dev, at: "2026-08-08" }
attributes:
  - { name: commits, value: "9" }
  - { name: exported, value: "356" }
  - { name: files, value: "32" }
  - { name: first_commit, value: "2026-07-29" }
  - { name: last_commit, value: "2026-08-08" }
  - { name: lines_added, value: "18598" }
  - { name: lines_removed, value: "38" }
  - { name: package, value: extract }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 56% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 5 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 7 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 30, source: internal/extract/cfamily.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 9 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 5 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 5 }
---
# internal/extract

<!-- signpost:managed:summary -->
32 go files; 356 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
32 files:
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
- `internal/extract/powershell.go`
- `internal/extract/powershell_test.go`
- `internal/extract/python.go`
- `internal/extract/python_test.go`
- `internal/extract/ruby.go`
- `internal/extract/ruby_test.go`
- `internal/extract/rust.go`
- `internal/extract/rust_test.go`
- `internal/extract/score.go`
- `internal/extract/score_test.go`
- `internal/extract/sfc.go`
- `internal/extract/sfc_test.go`
- `internal/extract/shell.go`
- `internal/extract/shell_test.go`
- `internal/extract/typescript.go`
- `internal/extract/typescript_test.go`

- **Changes with**: [internal/assemble](./assemble.md) ×5, [internal/discover](./discover.md) ×7, [\(repository root\)](./root.md) ×9, [cmd/signpost](./signpost.md) ×5, [site](./site.md) ×5

- **Imports**: [internal/discover](./discover.md) ×30
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
