---
type: Module
title: internal/manifest
description: 37 go files; 130 exported symbols.
resource: git://github.com/3rg0n/signpost@4a1eb7582c195b6ae366c3821a303f66ed639eb8/internal/manifest
generated: { by: signpost/dev, at: "2026-08-15" }
attributes:
  - { name: commits, value: "11" }
  - { name: exported, value: "130" }
  - { name: files, value: "37" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-15" }
  - { name: lines_added, value: "16578" }
  - { name: lines_removed, value: "222" }
  - { name: package, value: manifest }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 55% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 7 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 5 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 23, source: internal/manifest/bazel.go }
  - { kind: co_changes, to: ./extract.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./graph.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./okf.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./practice.md, confidence: extracted, weight: 4 }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 10 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 9 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./sqlstmt.md, confidence: extracted, weight: 1, source: internal/manifest/repo.go }
---
# internal/manifest

<!-- signpost:managed:summary -->
37 go files; 130 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
37 files:
- `internal/manifest/bazel.go`
- `internal/manifest/bazel_test.go`
- `internal/manifest/cmake.go`
- `internal/manifest/cmake_test.go`
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

- **Exports** (130): `Alias`, `Contract`, `DefaultRegistry`, `Dep`, `DepScope`, `Diag`, `Diag.Incomplete`, `Diag.Summary`, `Entrypoint`, `ExtractADR`, `ExtractAgentRules`, `ExtractBazel`, `ExtractCMake`, `ExtractCargo`, `ExtractCodeowners`, `ExtractCompose`, `ExtractComposer`, `ExtractContainerfile`, `ExtractGem`, `ExtractGoMod`, `ExtractGraphQL`, `ExtractHelmChart`, `ExtractHelmValues`, `ExtractKubernetes`, `ExtractMSBuild`, `ExtractMakefile`, `ExtractMigration`, `ExtractOpenAPI`, `ExtractPackageJSON`, `ExtractProto`, `ExtractPyProject`, `ExtractRequirements`, `ExtractSolution`, `ExtractTSConfig`, `ExtractTerraform`, `ExtractWorkflow`, `Facts`, `Facts.DepNames`, `Facts.DirectDepNames`, `Facts.ImageRefs`, `Facts.JobNames`, `Facts.Normalize`, `Facts.SecretNames`, `Facts.SecretNamesFor`, `Facts.ServiceNames`, `GoMod`, `GoModReplace`, `GoModReplace.Local`, `GoModRequire`, `Image`, `IsBazelWorkspaceRoot`, `Job`, `KeyValue`, `Kind`, `KindADR`, `KindAgentRules`, `KindBazel`, `KindCMake`, `KindCargo`, `KindCodeowners`, and 70 more

- **Changes with**: [internal/assemble](./assemble.md) ×7, [internal/discover](./discover.md) ×5, [internal/extract](./extract.md) ×2, [internal/graph](./graph.md) ×2, [internal/okf](./okf.md) ×3, [internal/practice](./practice.md) ×4, [\(repository root\)](./root.md) ×10, [cmd/signpost](./signpost.md) ×9, [site](./site.md) ×3

- **Imports**: [internal/discover](./discover.md) ×23, [internal/sqlstmt](./sqlstmt.md) ×1
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
