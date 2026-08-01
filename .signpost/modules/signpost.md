---
type: Module
title: cmd/signpost
description: 13 go files; 48 exported symbols; entrypoint main; package main.
resource: git://github.com/3rg0n/signpost@0f2ffcf187b8feffc8a8867bb77ca579c842f92e/cmd/signpost
tags: [entrypoint]
generated: { by: signpost/dev, at: "2026-08-01" }
attributes:
  - { name: commits, value: "9" }
  - { name: entrypoints, value: main }
  - { name: exported, value: "48" }
  - { name: files, value: "13" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-01" }
  - { name: lines_added, value: "2727" }
  - { name: lines_removed, value: "20" }
  - { name: package, value: main }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 56% }
edges:
  - { kind: imports, to: /modules/assemble.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: /modules/discover.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: /modules/export.md, confidence: extracted, weight: 1, source: cmd/signpost/export.go }
  - { kind: imports, to: /modules/extract.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: /modules/graph.md, confidence: extracted, weight: 2, source: cmd/signpost/graph.go }
  - { kind: imports, to: /modules/manifest.md, confidence: extracted, weight: 2, source: cmd/signpost/corpus_test.go }
  - { kind: imports, to: /modules/model.md, confidence: extracted, weight: 3, source: cmd/signpost/build.go }
  - { kind: co_changes, to: /modules/okf.md, confidence: extracted, weight: 5 }
  - { kind: imports, to: /modules/okf.md, confidence: extracted, weight: 5, source: cmd/signpost/build.go }
  - { kind: imports, to: /modules/practice.md, confidence: extracted, weight: 1, source: cmd/signpost/build.go }
  - { kind: co_changes, to: /modules/semantic.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: /modules/semantic.md, confidence: extracted, weight: 1, source: cmd/signpost/build.go }
  - { kind: co_changes, to: /modules/site.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: /modules/vcs.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: /modules/vcs.md, confidence: extracted, weight: 2, source: cmd/signpost/build.go }
---
# cmd/signpost

<!-- signpost:managed:summary -->
13 go files; 48 exported symbols; entrypoint main; package main.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
13 files:
- `cmd/signpost/build.go`
- `cmd/signpost/build_test.go`
- `cmd/signpost/corpus_test.go`
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

- **Changes with**: [internal/okf](/modules/okf.md) ×5, [internal/semantic](/modules/semantic.md) ×2, [site](/modules/site.md) ×2, [internal/vcs](/modules/vcs.md) ×4

- **Imports**: [internal/assemble](/modules/assemble.md) ×1, [internal/discover](/modules/discover.md) ×1, [internal/export](/modules/export.md) ×1, [internal/extract](/modules/extract.md) ×1, [internal/graph](/modules/graph.md) ×2, [internal/manifest](/modules/manifest.md) ×2, [internal/model](/modules/model.md) ×3, [internal/okf](/modules/okf.md) ×5, [internal/practice](/modules/practice.md) ×1, [internal/semantic](/modules/semantic.md) ×1, [internal/vcs](/modules/vcs.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
