---
type: Module
title: internal/hook
description: 2 go files; 17 exported symbols.
resource: git://github.com/3rg0n/signpost@b4c8f5076ffb2ea629c1401ed3d7029542906974/internal/hook
generated: { by: signpost/dev, at: "2026-08-16" }
attributes:
  - { name: commits, value: "2" }
  - { name: exported, value: "17" }
  - { name: files, value: "2" }
  - { name: first_commit, value: "2026-08-02" }
  - { name: last_commit, value: "2026-08-02" }
  - { name: lines_added, value: "1287" }
  - { name: lines_removed, value: "4" }
  - { name: package, value: hook }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: imports, to: ./okf.md, confidence: extracted, weight: 1, source: internal/hook/hook_test.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 2 }
---
# internal/hook

<!-- signpost:managed:summary -->
2 go files; 17 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
2 files:
- `internal/hook/hook.go`
- `internal/hook/hook_test.go`

- **Exports** (17): `Block`, `BundleDir`, `Check`, `CheckFast`, `CheckVerify`, `EnvCheck`, `Fast`, `Install`, `InstallResult`, `ParseCheck`, `Paths`, `Resolve`, `Script`, `Status`, `Status.Stale`, `Uninstall`, `UninstallResult`

- **Changes with**: [\(repository root\)](./root.md) ×2

- **Imports**: [internal/okf](./okf.md) ×1
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
