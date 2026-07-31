---
type: Module
title: internal/okf
description: 13 go files; 166 exported symbols.
resource: git://github.com/3rg0n/signpost@55fea4b6f8546df1d8ab560eca846cd0ef22845e/internal/okf
generated: { by: signpost/dev, at: "2026-07-31" }
attributes:
  - { name: commits, value: "3" }
  - { name: exported, value: "166" }
  - { name: files, value: "13" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-07-30" }
  - { name: lines_added, value: "5500" }
  - { name: lines_removed, value: "7" }
  - { name: package, value: okf }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 67% }
edges:
  - { kind: imports, to: /modules/graph.md, confidence: extracted, weight: 7, source: internal/okf/bundle.go }
  - { kind: imports, to: /modules/manifest.md, confidence: extracted, weight: 4, source: internal/okf/bundle_test.go }
  - { kind: co_changes, to: /modules/signpost.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: /modules/vcs.md, confidence: extracted, weight: 2 }
---
# internal/okf

<!-- signpost:managed:summary -->
13 go files; 166 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
13 files:
- `internal/okf/bundle.go`
- `internal/okf/bundle_test.go`
- `internal/okf/emit.go`
- `internal/okf/emit_test.go`
- `internal/okf/frontmatter.go`
- `internal/okf/frontmatter_test.go`
- `internal/okf/manifest.go`
- `internal/okf/page.go`
- `internal/okf/page_test.go`
- `internal/okf/verify.go`
- `internal/okf/verify_test.go`
- `internal/okf/yaml.go`
- `internal/okf/yaml_test.go`

- **Changes with**: [cmd/signpost](/modules/signpost.md) ×3, [internal/vcs](/modules/vcs.md) ×2

- **Imports**: [internal/graph](/modules/graph.md) ×7, [internal/manifest](/modules/manifest.md) ×4
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
