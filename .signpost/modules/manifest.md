---
type: Module
title: internal/manifest
description: 21 go files; 221 exported symbols.
resource: git://github.com/3rg0n/signpost@0f2ffcf187b8feffc8a8867bb77ca579c842f92e/internal/manifest
generated: { by: signpost/dev, at: "2026-08-01" }
attributes:
  - { name: commits, value: "2" }
  - { name: exported, value: "221" }
  - { name: files, value: "21" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-01" }
  - { name: lines_added, value: "9846" }
  - { name: lines_removed, value: "4" }
  - { name: package, value: manifest }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: imports, to: /modules/discover.md, confidence: extracted, weight: 13, source: internal/manifest/container.go }
---
# internal/manifest

<!-- signpost:managed:summary -->
21 go files; 221 exported symbols.
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

- **Imports**: [internal/discover](/modules/discover.md) ×13
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
