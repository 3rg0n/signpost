---
type: Module
title: internal/okf
description: 13 go files; 196 exported symbols.
resource: git://github.com/3rg0n/signpost@d40fd6e81570e1524a63889e3f75cc16dd7dd317/internal/okf
generated: { by: signpost/dev, at: "2026-08-03" }
attributes:
  - { name: commits, value: "8" }
  - { name: exported, value: "196" }
  - { name: files, value: "13" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-02" }
  - { name: lines_added, value: "7222" }
  - { name: lines_removed, value: "118" }
  - { name: package, value: okf }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 75% }
edges:
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 7, source: internal/okf/bundle.go }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 4, source: internal/okf/bundle_test.go }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 7 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./vcs.md, confidence: extracted, weight: 2 }
---
# internal/okf

<!-- signpost:managed:summary -->
13 go files; 196 exported symbols.
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

- **Changes with**: [cmd/signpost](./signpost.md) ×7, [site](./site.md) ×2, [internal/vcs](./vcs.md) ×2

- **Imports**: [internal/graph](./graph.md) ×7, [internal/manifest](./manifest.md) ×4
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
