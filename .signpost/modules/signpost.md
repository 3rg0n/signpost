---
type: Module
title: cmd/signpost
description: 12 go files; 41 exported symbols; entrypoint main; package main.
resource: git://github.com/3rg0n/signpost@55fea4b6f8546df1d8ab560eca846cd0ef22845e/cmd/signpost
tags: [entrypoint]
generated: { by: signpost/dev, at: "2026-07-31" }
attributes:
  - { name: commits, value: "6" }
  - { name: entrypoints, value: main }
  - { name: exported, value: "41" }
  - { name: files, value: "12" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-07-31" }
  - { name: lines_added, value: "2119" }
  - { name: lines_removed, value: "15" }
  - { name: package, value: main }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 67% }
edges:
  - { kind: imports, to: /modules/assemble.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: /modules/discover.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: /modules/export.md, confidence: extracted, weight: 1, source: cmd/signpost/export.go }
  - { kind: imports, to: /modules/extract.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: /modules/graph.md, confidence: extracted, weight: 2, source: cmd/signpost/graph.go }
  - { kind: imports, to: /modules/manifest.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: /modules/model.md, confidence: extracted, weight: 2, source: cmd/signpost/model.go }
  - { kind: co_changes, to: /modules/okf.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: /modules/okf.md, confidence: extracted, weight: 4, source: cmd/signpost/build.go }
  - { kind: co_changes, to: /modules/vcs.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: /modules/vcs.md, confidence: extracted, weight: 2, source: cmd/signpost/build.go }
---
# cmd/signpost

<!-- signpost:managed:summary -->
12 go files; 41 exported symbols; entrypoint main; package main.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
12 files:
- `cmd/signpost/build.go`
- `cmd/signpost/build_test.go`
- `cmd/signpost/export.go`
- `cmd/signpost/graph.go`
- `cmd/signpost/main.go`
- `cmd/signpost/main_test.go`
- `cmd/signpost/model.go`
- `cmd/signpost/model_test.go`
- `cmd/signpost/pipeline.go`
- `cmd/signpost/printer.go`
- `cmd/signpost/verify.go`
- `cmd/signpost/verify_test.go`

- **Changes with**: [internal/okf](/modules/okf.md) ×3, [internal/vcs](/modules/vcs.md) ×4

- **Imports**: [internal/assemble](/modules/assemble.md) ×1, [internal/discover](/modules/discover.md) ×1, [internal/export](/modules/export.md) ×1, [internal/extract](/modules/extract.md) ×1, [internal/graph](/modules/graph.md) ×2, [internal/manifest](/modules/manifest.md) ×1, [internal/model](/modules/model.md) ×2, [internal/okf](/modules/okf.md) ×4, [internal/vcs](/modules/vcs.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
