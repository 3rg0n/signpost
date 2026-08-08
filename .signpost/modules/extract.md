---
type: Module
title: internal/extract
description: 30 go files; 344 exported symbols.
resource: git://github.com/3rg0n/signpost@e1038ca6ce192ebf827d95295be6c55b349c0034/internal/extract
generated: { by: signpost/dev, at: "2026-08-08" }
attributes:
  - { name: commits, value: "8" }
  - { name: exported, value: "344" }
  - { name: files, value: "30" }
  - { name: first_commit, value: "2026-07-29" }
  - { name: last_commit, value: "2026-08-08" }
  - { name: lines_added, value: "17814" }
  - { name: lines_removed, value: "38" }
  - { name: package, value: extract }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 50% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 4 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 6 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 28, source: internal/extract/cfamily.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 8 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 4 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 4 }
---
# internal/extract

<!-- signpost:managed:summary -->
30 go files; 344 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
30 files:
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
- `internal/extract/shell.go`
- `internal/extract/shell_test.go`
- `internal/extract/typescript.go`
- `internal/extract/typescript_test.go`

- **Changes with**: [internal/assemble](./assemble.md) ×4, [internal/discover](./discover.md) ×6, [\(repository root\)](./root.md) ×8, [cmd/signpost](./signpost.md) ×4, [site](./site.md) ×4

- **Imports**: [internal/discover](./discover.md) ×28
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
