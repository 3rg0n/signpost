---
type: Module
title: internal/semantic
description: 3 go files; 35 exported symbols.
resource: git://github.com/3rg0n/signpost@aa1287096af85ed210771f340e57c53aeebd0082/internal/semantic
generated: { by: signpost/dev, at: "2026-08-10" }
attributes:
  - { name: commits, value: "3" }
  - { name: exported, value: "35" }
  - { name: files, value: "3" }
  - { name: first_commit, value: "2026-07-31" }
  - { name: last_commit, value: "2026-08-02" }
  - { name: lines_added, value: "1541" }
  - { name: lines_removed, value: "4" }
  - { name: package, value: semantic }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 67% }
edges:
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 2, source: internal/semantic/semantic.go }
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 3, source: internal/semantic/prompt.go }
  - { kind: imports, to: ./model.md, confidence: extracted, weight: 3, source: internal/semantic/prompt.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 3 }
---
# internal/semantic

<!-- signpost:managed:summary -->
3 go files; 35 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
3 files:
- `internal/semantic/prompt.go`
- `internal/semantic/semantic.go`
- `internal/semantic/semantic_test.go`

- **Exports** (35): `Budget`, `DefaultMaxCalls`, `DefaultMaxTokens`, `Input`, `Result`, `Result.Regions`, `Run`, `Summary`, `TestACitePathCarryingMarkerSyntaxIsRefused`, `TestAFullLengthSummaryThatFinishesIsKept`, `TestAnUnusualButPrintableCitePathIsStillAccepted`, `TestBudgetStopsTheRunAndSaysWhatWasNotDone`, `TestCacheKeyCoversActorAndPromptVersion`, `TestCachedSummaryIsRegroundedBeforeUse`, `TestChangedContentMissesTheCache`, `TestCitationFormattingIsToleratedButNotGuessed`, `TestFileContentIsWrappedAndPathsAreQuoted`, `TestGroundedSummaryIsKept`, `TestInventedCitationDropsTheWholeSummary`, `TestNilBackendProducesNothing`, `TestNoCacheDirStillWorks`, `TestNoCitationsIsRefused`, `TestOverlongProseIsRefusedNotTruncated`, `TestOversizeFilesAreClippedAndReported`, `TestProseCutToFitTheSchemaIsRefused`, `TestProseIsFlattenedToOneParagraph`, `TestRegionMarkersInProseAreStripped`, `TestRenderedRegionNamesTheModelAndSources`, `TestRunNeverReturnsAnErrorOnBackendFailure`, `TestShortProseWithoutAFullStopIsKept`, `TestSourceOrderIsStableAcrossRuns`, `TestTestFilesAreNotSummarised`, `TestUnavailableBackendStopsAndNamesWhatWasLost`, `TestUnchangedInputIsServedFromCache`, `TestUnparseableResponseSkipsOnlyThatNode`

- **Changes with**: [\(repository root\)](./root.md) ×3, [cmd/signpost](./signpost.md) ×3

- **Imports**: [internal/discover](./discover.md) ×2, [internal/graph](./graph.md) ×3, [internal/model](./model.md) ×3
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
