---
type: Module
title: internal/manifest
description: 23 go files; 239 exported symbols.
resource: git://github.com/3rg0n/signpost@2f488fb2af7df22b5c7ec19a6b59a33f145599f3/internal/manifest
generated: { by: signpost/dev, at: "2026-08-02" }
attributes:
  - { name: commits, value: "5" }
  - { name: exported, value: "239" }
  - { name: files, value: "23" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-02" }
  - { name: lines_added, value: "10449" }
  - { name: lines_removed, value: "35" }
  - { name: package, value: manifest }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: co_changes, to: /modules/assemble.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: /modules/discover.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: /modules/discover.md, confidence: extracted, weight: 14, source: internal/manifest/container.go }
  - { kind: co_changes, to: /modules/practice.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: /modules/signpost.md, confidence: extracted, weight: 4 }
---
# internal/manifest

<!-- signpost:managed:summary -->
23 go files; 239 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
23 files:
- `internal/manifest/container.go`
- `internal/manifest/contract.go`
- `internal/manifest/contract_test.go`
- `internal/manifest/deps.go`
- `internal/manifest/deps_test.go`
- `internal/manifest/facts.go`
- `internal/manifest/gomod.go`
- `internal/manifest/infra_test.go`
- `internal/manifest/json.go`
- `internal/manifest/json_test.go`
- `internal/manifest/kubernetes.go`
- `internal/manifest/registry.go`
- `internal/manifest/registry_test.go`
- `internal/manifest/repo.go`
- `internal/manifest/repo_test.go`
- `internal/manifest/toml.go`
- `internal/manifest/toml_test.go`
- `internal/manifest/tree.go`
- `internal/manifest/tsconfig.go`
- `internal/manifest/tsconfig_test.go`
- `internal/manifest/workflow.go`
- `internal/manifest/yaml.go`
- `internal/manifest/yaml_test.go`

- **Changes with**: [internal/assemble](/modules/assemble.md) ×2, [internal/discover](/modules/discover.md) ×3, [internal/practice](/modules/practice.md) ×2, [cmd/signpost](/modules/signpost.md) ×4

- **Imports**: [internal/discover](/modules/discover.md) ×14
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
