---
type: Module
title: internal/manifest
description: 27 go files; 256 exported symbols.
resource: git://github.com/3rg0n/signpost@7d8443b851fd8771d077beec6e134adc25bc4f59/internal/manifest
generated: { by: signpost/dev, at: "2026-08-04" }
attributes:
  - { name: commits, value: "6" }
  - { name: exported, value: "256" }
  - { name: files, value: "27" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-04" }
  - { name: lines_added, value: "12587" }
  - { name: lines_removed, value: "48" }
  - { name: package, value: manifest }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 16, source: internal/manifest/container.go }
  - { kind: co_changes, to: ./practice.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 5 }
---
# internal/manifest

<!-- signpost:managed:summary -->
27 go files; 256 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
27 files:
- `internal/manifest/container.go`
- `internal/manifest/contract.go`
- `internal/manifest/contract_test.go`
- `internal/manifest/deps.go`
- `internal/manifest/deps_test.go`
- `internal/manifest/facts.go`
- `internal/manifest/gomod.go`
- `internal/manifest/hcl.go`
- `internal/manifest/hcl_test.go`
- `internal/manifest/infra_test.go`
- `internal/manifest/json.go`
- `internal/manifest/json_test.go`
- `internal/manifest/kubernetes.go`
- `internal/manifest/registry.go`
- `internal/manifest/registry_test.go`
- `internal/manifest/repo.go`
- `internal/manifest/repo_test.go`
- `internal/manifest/terraform.go`
- `internal/manifest/terraform_test.go`
- `internal/manifest/toml.go`
- `internal/manifest/toml_test.go`
- `internal/manifest/tree.go`
- `internal/manifest/tsconfig.go`
- `internal/manifest/tsconfig_test.go`
- `internal/manifest/workflow.go`
- `internal/manifest/yaml.go`
- `internal/manifest/yaml_test.go`

- **Changes with**: [internal/assemble](./assemble.md) ×3, [internal/discover](./discover.md) ×3, [internal/practice](./practice.md) ×2, [cmd/signpost](./signpost.md) ×5

- **Imports**: [internal/discover](./discover.md) ×16
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
