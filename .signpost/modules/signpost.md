---
type: Module
title: cmd/signpost
description: 29 go files; entrypoint main; package main.
resource: git://github.com/3rg0n/signpost@fd881702084abbf7ca124cb20e883404e112b991/cmd/signpost
tags: [entrypoint]
generated: { by: signpost/dev, at: "2026-08-19" }
attributes:
  - { name: commits, value: "53" }
  - { name: entrypoints, value: main }
  - { name: exported, value: "0" }
  - { name: files, value: "29" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-19" }
  - { name: lines_added, value: "14901" }
  - { name: lines_removed, value: "360" }
  - { name: package, value: main }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 57% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 19 }
  - { kind: imports, to: ./assemble.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: co_changes, to: ./config.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./config.md, confidence: extracted, weight: 6, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 12 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 2, source: cmd/signpost/budget_test.go }
  - { kind: imports, to: ./export.md, confidence: extracted, weight: 2, source: cmd/signpost/export.go }
  - { kind: co_changes, to: ./extract.md, confidence: extracted, weight: 6 }
  - { kind: imports, to: ./extract.md, confidence: extracted, weight: 1, source: cmd/signpost/pipeline.go }
  - { kind: imports, to: ./gitdiff.md, confidence: extracted, weight: 1, source: cmd/signpost/diff.go }
  - { kind: co_changes, to: ./graph.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 5, source: cmd/signpost/diff.go }
  - { kind: imports, to: ./hook.md, confidence: extracted, weight: 3, source: cmd/signpost/hooks.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 9 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 2, source: cmd/signpost/corpus_test.go }
  - { kind: co_changes, to: ./model.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./model.md, confidence: extracted, weight: 5, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./okf.md, confidence: extracted, weight: 14 }
  - { kind: imports, to: ./okf.md, confidence: extracted, weight: 6, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./practice.md, confidence: extracted, weight: 7 }
  - { kind: imports, to: ./practice.md, confidence: extracted, weight: 1, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 52 }
  - { kind: co_changes, to: ./scaffold.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: ./scaffold.md, confidence: extracted, weight: 2, source: cmd/signpost/init.go }
  - { kind: imports, to: ./selfupdate.md, confidence: extracted, weight: 2, source: cmd/signpost/update.go }
  - { kind: co_changes, to: ./semantic.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./semantic.md, confidence: extracted, weight: 1, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 13 }
  - { kind: imports, to: ./telemetry.md, confidence: extracted, weight: 2, source: cmd/signpost/main.go }
  - { kind: co_changes, to: ./vcs.md, confidence: extracted, weight: 6 }
  - { kind: imports, to: ./vcs.md, confidence: extracted, weight: 6, source: cmd/signpost/build.go }
  - { kind: co_changes, to: ./view.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./view.md, confidence: extracted, weight: 3, source: cmd/signpost/corpus_test.go }
---
# cmd/signpost

<!-- signpost:managed:summary -->
29 go files; entrypoint main; package main.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
29 files:
- `cmd/signpost/budget_test.go`
- `cmd/signpost/build.go`
- `cmd/signpost/build_test.go`
- `cmd/signpost/config.go`
- `cmd/signpost/config_test.go`
- `cmd/signpost/corpus_test.go`
- `cmd/signpost/diff.go`
- `cmd/signpost/diff_test.go`
- `cmd/signpost/export.go`
- `cmd/signpost/graph.go`
- `cmd/signpost/graph_test.go`
- `cmd/signpost/hooks.go`
- `cmd/signpost/hooks_test.go`
- `cmd/signpost/init.go`
- `cmd/signpost/init_test.go`
- `cmd/signpost/main.go`
- `cmd/signpost/main_test.go`
- `cmd/signpost/model.go`
- `cmd/signpost/model_test.go`
- `cmd/signpost/pipeline.go`
- `cmd/signpost/printer.go`
- `cmd/signpost/update.go`
- `cmd/signpost/update_test.go`
- `cmd/signpost/verify.go`
- `cmd/signpost/verify_test.go`
- `cmd/signpost/version.go`
- `cmd/signpost/version_test.go`
- `cmd/signpost/view.go`
- `cmd/signpost/view_test.go`

- **Changes with**: [internal/assemble](./assemble.md) ×19, [internal/config](./config.md) ×2, [internal/discover](./discover.md) ×12, [internal/extract](./extract.md) ×6, [internal/graph](./graph.md) ×3, [internal/manifest](./manifest.md) ×9, [internal/model](./model.md) ×2, [internal/okf](./okf.md) ×14, [internal/practice](./practice.md) ×7, [\(repository root\)](./root.md) ×52, [internal/scaffold](./scaffold.md) ×2, [internal/semantic](./semantic.md) ×3, [site](./site.md) ×13, [internal/vcs](./vcs.md) ×6, [internal/view](./view.md) ×3

- **Imports**: [internal/assemble](./assemble.md) ×1, [internal/config](./config.md) ×6, [internal/discover](./discover.md) ×2, [internal/export](./export.md) ×2, [internal/extract](./extract.md) ×1, [internal/gitdiff](./gitdiff.md) ×1, [internal/graph](./graph.md) ×5, [internal/hook](./hook.md) ×3, [internal/manifest](./manifest.md) ×2, [internal/model](./model.md) ×5, [internal/okf](./okf.md) ×6, [internal/practice](./practice.md) ×1, [internal/scaffold](./scaffold.md) ×2, [internal/selfupdate](./selfupdate.md) ×2, [internal/semantic](./semantic.md) ×1, [internal/telemetry](./telemetry.md) ×2, [internal/vcs](./vcs.md) ×6, [internal/view](./view.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
