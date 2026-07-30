---
type: Module
title: cmd/signpost
description: 10 go files; 31 exported symbols; entrypoint main; package main.
resource: git://github.com/3rg0n/signpost@1fc38be2aa4a6970091541506bd34191928da885/cmd/signpost
tags: [entrypoint]
generated: { by: signpost/dev, at: "2026-07-30" }
attributes:
  - { name: commits, value: "5" }
  - { name: entrypoints, value: main }
  - { name: exported, value: "31" }
  - { name: files, value: "10" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-07-30" }
  - { name: lines_added, value: "1749" }
  - { name: lines_removed, value: "15" }
  - { name: package, value: main }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 80% }
edges:
  - { kind: imports, to: /modules/assemble.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: /modules/discover.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: /modules/export.md, confidence: extracted, weight: 1, source: cmd/signpost/export.go }
  - { kind: imports, to: /modules/extract.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: /modules/graph.md, confidence: extracted, weight: 2, source: cmd/signpost/graph.go }
  - { kind: imports, to: /modules/manifest.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: co_changes, to: /modules/okf.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: /modules/okf.md, confidence: extracted, weight: 4, source: cmd/signpost/build.go }
  - { kind: co_changes, to: /modules/vcs.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: /modules/vcs.md, confidence: extracted, weight: 2, source: cmd/signpost/build.go }
---
# cmd/signpost

<!-- signpost:managed:summary -->
10 go files; 31 exported symbols; entrypoint main; package main.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
10 files:
- `cmd/signpost/build.go`
- `cmd/signpost/build_test.go`
- `cmd/signpost/export.go`
- `cmd/signpost/graph.go`
- `cmd/signpost/main.go`
- `cmd/signpost/main_test.go`
- `cmd/signpost/pipeline.go`
- `cmd/signpost/printer.go`
- `cmd/signpost/verify.go`
- `cmd/signpost/verify_test.go`

- **Changes with**: [internal/okf](/modules/okf.md) ×3, [internal/vcs](/modules/vcs.md) ×3

- **Imports**: [internal/assemble](/modules/assemble.md) ×1, [internal/discover](/modules/discover.md) ×1, [internal/export](/modules/export.md) ×1, [internal/extract](/modules/extract.md) ×1, [internal/graph](/modules/graph.md) ×2, [internal/manifest](/modules/manifest.md) ×1, [internal/okf](/modules/okf.md) ×4, [internal/vcs](/modules/vcs.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
