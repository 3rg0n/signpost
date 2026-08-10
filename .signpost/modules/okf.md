---
type: Module
title: internal/okf
description: 13 go files; 198 exported symbols.
resource: git://github.com/3rg0n/signpost@aa1287096af85ed210771f340e57c53aeebd0082/internal/okf
generated: { by: signpost/dev, at: "2026-08-10" }
attributes:
  - { name: commits, value: "10" }
  - { name: exported, value: "198" }
  - { name: files, value: "13" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-10" }
  - { name: lines_added, value: "7408" }
  - { name: lines_removed, value: "173" }
  - { name: package, value: okf }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 60% }
edges:
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 7, source: internal/okf/bundle.go }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 4, source: internal/okf/bundle_test.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 10 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 8 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./vcs.md, confidence: extracted, weight: 2 }
---
# internal/okf

<!-- signpost:managed:summary -->
13 go files; 198 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
13 files:
- `internal/okf/bundle.go`
- `internal/okf/bundle_test.go`
- `internal/okf/emit.go`
- `internal/okf/emit_test.go`
- `internal/okf/frontmatter.go`
- `internal/okf/frontmatter_test.go`
- `internal/okf/manifest.go`
- `internal/okf/page.go`
- `internal/okf/page_test.go`
- `internal/okf/verify.go`
- `internal/okf/verify_test.go`
- `internal/okf/yaml.go`
- `internal/okf/yaml_test.go`

- **Exports** (198): `Actor`, `BundleDir`, `Finding`, `Finding.String`, `FindingBrokenLink`, `FindingConformance`, `FindingKind`, `FindingMissingBundle`, `FindingMissingPage`, `FindingOrphanPage`, `FindingOutOfDate`, `FindingStaleResource`, `FindingStaleVerification`, `IndexPage`, `LogPage`, `ManifestFile`, `NewPage`, `Options`, `Page`, `Page.HumanText`, `Page.Managed`, `Page.Merge`, `Page.Render`, `ParsePage`, `PracticesPage`, `Region`, `Region.Managed`, `Result`, `TestAFileNamedLikeACloseMarkerCannotCloseTheRegion`, `TestATitleCannotForgeALinkTarget`, `TestATitleNamedLikeAnOpenMarkerCannotStartARegion`, `TestAttributeMapSortsAndSkipsEmpty`, `TestBundleLinksChecksRelativeTargetsOnlyInGeneratedRegions`, `TestBundleRelRejectsAPathEscapingTheBundle`, `TestCarryHumanKeysDropsALegacyStaleVerificationStatus`, `TestCarryHumanKeysDropsCommentsAttachedToGeneratedKeys`, `TestCarryHumanKeysDropsEveryGeneratedKeyIncludingBlocks`, `TestCarryHumanKeysDropsIndentedContinuations`, `TestCarryHumanKeysKeepsASpecStatus`, `TestCarryHumanKeysOnEmptyFrontmatter`, `TestCarryHumanKeysPreservesRawText`, `TestCheckPageRel`, `TestCodeSpanQuotesOnlyPathsThatWouldBreakTheLine`, `TestConsecutiveHumanLinesAreOneRegion`, `TestDowngrade`, `TestDuplicateRegionNameClaimsOnlyTheFirst`, `TestEdgeKindLabelFallsBackToTheRawValue`, `TestEdgeListAlwaysCarriesConfidence`, `TestEdgeListIsOutgoingOnly`, `TestEdgeTargetsArePagePaths`, `TestEdgeWeightAndSourceAreEmittedWhenPresent`, `TestEmptyManagedRegion`, `TestEnsureTrailingNewline`, `TestEscapeMarkersDefangsGeneratedText`, `TestEveryPageFrontmatterIsReadable`, `TestFilesLineIsBoundedAndSaysSo`, `TestFilesLineSingularAndPlural`, `TestFindStaleSkipsTheCacheDirectory`, `TestFrontmatterCloseIsFoundLineWise`, `TestFrontmatterFenceMustBeTheFirstLine`, and 138 more

- **Changes with**: [\(repository root\)](./root.md) ×10, [cmd/signpost](./signpost.md) ×8, [site](./site.md) ×2, [internal/vcs](./vcs.md) ×2

- **Imports**: [internal/graph](./graph.md) ×7, [internal/manifest](./manifest.md) ×4
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
