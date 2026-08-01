---
type: Module
title: internal/manifest
description: 21 go files; 223 exported symbols.
resource: git://github.com/3rg0n/signpost@585677e9112289b405d046790a9b8af1e40c8232/internal/manifest
generated: { by: signpost/dev, at: "2026-08-01" }
attributes:
  - { name: commits, value: "3" }
  - { name: exported, value: "223" }
  - { name: files, value: "21" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-01" }
  - { name: lines_added, value: "9968" }
  - { name: lines_removed, value: "31" }
  - { name: package, value: manifest }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: co_changes, to: /modules/discover.md, confidence: extracted, weight: 2 }
  - { kind: imports, to: /modules/discover.md, confidence: extracted, weight: 13, source: internal/manifest/container.go }
  - { kind: co_changes, to: /modules/signpost.md, confidence: extracted, weight: 2 }
---
# internal/manifest

<!-- signpost:managed:summary -->
21 go files; 223 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
21 files:
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
- `internal/manifest/workflow.go`
- `internal/manifest/yaml.go`
- `internal/manifest/yaml_test.go`

- **Changes with**: [internal/discover](/modules/discover.md) ×2, [cmd/signpost](/modules/signpost.md) ×2

- **Imports**: [internal/discover](/modules/discover.md) ×13
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
