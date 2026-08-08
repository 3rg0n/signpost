---
type: Module
title: internal/manifest
description: 33 go files; 285 exported symbols.
resource: git://github.com/3rg0n/signpost@e1038ca6ce192ebf827d95295be6c55b349c0034/internal/manifest
generated: { by: signpost/dev, at: "2026-08-08" }
attributes:
  - { name: commits, value: "7" }
  - { name: exported, value: "285" }
  - { name: files, value: "33" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-07" }
  - { name: lines_added, value: "14169" }
  - { name: lines_removed, value: "48" }
  - { name: package, value: manifest }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 86% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 4 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 4 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 19, source: internal/manifest/composer.go }
  - { kind: co_changes, to: ./practice.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 6 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 6 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 2 }
---
# internal/manifest

<!-- signpost:managed:summary -->
33 go files; 285 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
33 files:
- `internal/manifest/composer.go`
- `internal/manifest/composer_test.go`
- `internal/manifest/container.go`
- `internal/manifest/contract.go`
- `internal/manifest/contract_test.go`
- `internal/manifest/deps.go`
- `internal/manifest/deps_test.go`
- `internal/manifest/facts.go`
- `internal/manifest/gem.go`
- `internal/manifest/gem_test.go`
- `internal/manifest/gomod.go`
- `internal/manifest/hcl.go`
- `internal/manifest/hcl_test.go`
- `internal/manifest/infra_test.go`
- `internal/manifest/json.go`
- `internal/manifest/json_test.go`
- `internal/manifest/kubernetes.go`
- `internal/manifest/msbuild.go`
- `internal/manifest/msbuild_test.go`
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

- **Changes with**: [internal/assemble](./assemble.md) ×4, [internal/discover](./discover.md) ×4, [internal/practice](./practice.md) ×3, [\(repository root\)](./root.md) ×6, [cmd/signpost](./signpost.md) ×6, [site](./site.md) ×2

- **Imports**: [internal/discover](./discover.md) ×19
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
