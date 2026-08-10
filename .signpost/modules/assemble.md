---
type: Module
title: internal/assemble
description: 6 go files; 73 exported symbols.
resource: git://github.com/3rg0n/signpost@aa1287096af85ed210771f340e57c53aeebd0082/internal/assemble
generated: { by: signpost/dev, at: "2026-08-10" }
attributes:
  - { name: commits, value: "17" }
  - { name: exported, value: "73" }
  - { name: files, value: "6" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-10" }
  - { name: lines_added, value: "6280" }
  - { name: lines_removed, value: "143" }
  - { name: owners, value: "@3rg0n" }
  - { name: package, value: assemble }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 65% }
edges:
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 7 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./extract.md, confidence: extracted, weight: 5 }
  - { kind: imports, to: ./extract.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 5 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 4, source: internal/assemble/assemble.go }
  - { kind: co_changes, to: ./practice.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 16 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 16 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 6 }
  - { kind: imports, to: ./vcs.md, confidence: extracted, weight: 3, source: internal/assemble/assemble.go }
---
# internal/assemble

<!-- signpost:managed:summary -->
6 go files; 73 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
6 files:
- `internal/assemble/assemble.go`
- `internal/assemble/assemble_test.go`
- `internal/assemble/describe.go`
- `internal/assemble/history_test.go`
- `internal/assemble/id.go`
- `internal/assemble/resolve.go`

- **Exports** (73): `Build`, `Input`, `Result`, `TestABazelLabelResolvesAgainstItsWorkspaceRoot`, `TestABazelLabelResolvesInsideTheNearestWorkspace`, `TestADRGetsNoModuleEdge`, `TestAFirstPartyImportWithNoTargetIsCountedSeparately`, `TestALinkAcrossSiblingDirectoriesBecomesAnEdge`, `TestALinkedSiblingTargetIsNotAnExternalDependency`, `TestANameThatSlugsOntoASuffixedIDIsStillDistinct`, `TestAgentRulesScopeByPlacement`, `TestAnUnchangedDirectoryKeepsItsIDWhenTheRepositoryChanges`, `TestAnUndeclaredLinkedLibraryStaysExternal`, `TestAssemblyIsDeterministic`, `TestBesideTestImportsDoNotClaimForeignSubjects`, `TestCIncludesResolveByPathAndDelimiter`, `TestCResolutionRespectsTheDelimiterAndRequiresAFile`, `TestCSystemHeaderRecognition`, `TestCoChangeDropsPairsWithNoModule`, `TestCoChangeEdgeIsSymmetricAndExtracted`, `TestCoChangeFoldsCollapsedPairsWithMaxNotSum`, `TestCoChangeWithinOneModuleDrawsNoEdge`, `TestCodeownersAttachesToModules`, `TestDataStoreNodePerTable`, `TestDeclaredDepsAreConnectedWithoutImports`, `TestDeletingTheBareNameHolderMovesOnlyItsOwnGroup`, `TestEmptyInputIsNotAnError`, `TestEveryEdgeIsExtracted`, `TestGoImportResolvesToModule`, `TestHeaderLabelDoesNotDecideTheModuleLanguage`, `TestHistoryAnnotatesModuleNodes`, `TestHistoryAssemblyIsDeterministic`, `TestHistoryCreatesNoNodes`, `TestHistoryKeepsAddedAndRemovedSeparate`, `TestHistoryRoundsAuthorShare`, `TestIDCollisionsAreDisambiguated`, `TestIDsAreStableForServicesAndDataStoresToo`, `TestInterfaceNodePerContractFile`, `TestJVMImportPrefersTheProductionSourceSet`, `TestJVMImportsResolveByDeclaredPackage`, `TestJVMResolutionDoesNotMatchOnPrefixOrInventDependencies`, `TestJVMTestedByComesFromTheDeclaredPackage`, `TestKubernetesCRDIsNotAnInterface`, `TestLocalDeclarationIsAnEdgeAndNotAReferencePage`, `TestManifestInSourcelessDirectory`, `TestModuleNodePerDirectory`, `TestNoDanglingEdges`, `TestNodeBuiltinSubpathsAreTheRuntime`, `TestNonLatinDirectoryGetsStableID`, `TestPythonPackageRootDoesNotReachASibling`, `TestPythonPackageRootResolvesAbsoluteImport`, `TestPythonRelativeAndAbsoluteImports`, `TestRequirementsDirectoryIsNotAPythonRoot`, `TestRustCrateAndExternImports`, `TestRustSuperIsTheEnclosingModuleNotTheParentDirectory`, `TestSecretValuesNeverReachTheGraph`, `TestServiceMergesAcrossFiles`, `TestStdlibIsInNeitherGapMap`, `TestTestedByEdge`, `TestTestsBesideCodeDrawNoEdge`, and 13 more

- **Changes with**: [internal/discover](./discover.md) ×7, [internal/extract](./extract.md) ×5, [internal/manifest](./manifest.md) ×5, [internal/practice](./practice.md) ×2, [\(repository root\)](./root.md) ×16, [cmd/signpost](./signpost.md) ×16, [site](./site.md) ×6

- **Imports**: [internal/discover](./discover.md) ×4, [internal/extract](./extract.md) ×4, [internal/graph](./graph.md) ×4, [internal/manifest](./manifest.md) ×4, [internal/vcs](./vcs.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
