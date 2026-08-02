---
type: Module
title: cmd/signpost
description: 17 go files; 92 exported symbols; entrypoint main; package main.
resource: git://github.com/3rg0n/signpost@d0ea89253a2bdcb6948c4746ef4cd98bce8db2b0/cmd/signpost
tags: [entrypoint]
generated: { by: signpost/dev, at: "2026-08-02" }
attributes:
  - { name: commits, value: "19" }
  - { name: entrypoints, value: main }
  - { name: exported, value: "92" }
  - { name: files, value: "17" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-02" }
  - { name: lines_added, value: "5875" }
  - { name: lines_removed, value: "185" }
  - { name: package, value: main }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 79% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 5 }
  - { kind: imports, to: ./assemble.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: ./config.md, confidence: extracted, weight: 6, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: ./export.md, confidence: extracted, weight: 1, source: cmd/signpost/export.go }
  - { kind: imports, to: ./extract.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 2, source: cmd/signpost/graph.go }
  - { kind: imports, to: ./hook.md, confidence: extracted, weight: 2, source: cmd/signpost/hooks.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 2, source: cmd/signpost/corpus_test.go }
  - { kind: co_changes, to: ./model.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./model.md, confidence: extracted, weight: 5, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./okf.md, confidence: extracted, weight: 7 }
  - { kind: imports, to: ./okf.md, confidence: extracted, weight: 5, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./practice.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./practice.md, confidence: extracted, weight: 1, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./semantic.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./semantic.md, confidence: extracted, weight: 1, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./vcs.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./vcs.md, confidence: extracted, weight: 2, source: cmd/signpost/build.go }
---
# cmd/signpost

<!-- signpost:managed:summary -->
17 go files; 92 exported symbols; entrypoint main; package main.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
17 files:
- `cmd/signpost/build.go`
- `cmd/signpost/build_test.go`
- `cmd/signpost/config.go`
- `cmd/signpost/config_test.go`
- `cmd/signpost/corpus_test.go`
- `cmd/signpost/export.go`
- `cmd/signpost/graph.go`
- `cmd/signpost/hooks.go`
- `cmd/signpost/hooks_test.go`
- `cmd/signpost/main.go`
- `cmd/signpost/main_test.go`
- `cmd/signpost/model.go`
- `cmd/signpost/model_test.go`
- `cmd/signpost/pipeline.go`
- `cmd/signpost/printer.go`
- `cmd/signpost/verify.go`
- `cmd/signpost/verify_test.go`

- **Changes with**: [internal/assemble](./assemble.md) ×5, [internal/discover](./discover.md) ×3, [internal/manifest](./manifest.md) ×4, [internal/model](./model.md) ×2, [internal/okf](./okf.md) ×7, [internal/practice](./practice.md) ×2, [internal/semantic](./semantic.md) ×3, [site](./site.md) ×3, [internal/vcs](./vcs.md) ×4

- **Imports**: [internal/assemble](./assemble.md) ×1, [internal/config](./config.md) ×6, [internal/discover](./discover.md) ×1, [internal/export](./export.md) ×1, [internal/extract](./extract.md) ×1, [internal/graph](./graph.md) ×2, [internal/hook](./hook.md) ×2, [internal/manifest](./manifest.md) ×2, [internal/model](./model.md) ×5, [internal/okf](./okf.md) ×5, [internal/practice](./practice.md) ×1, [internal/semantic](./semantic.md) ×1, [internal/vcs](./vcs.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
