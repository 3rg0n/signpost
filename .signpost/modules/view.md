---
type: Module
title: internal/view
description: 3 go files; 24 exported symbols.
resource: git://github.com/3rg0n/signpost@aa1287096af85ed210771f340e57c53aeebd0082/internal/view
generated: { by: signpost/dev, at: "2026-08-10" }
attributes:
  - { name: commits, value: "1" }
  - { name: exported, value: "24" }
  - { name: files, value: "3" }
  - { name: first_commit, value: "2026-08-06" }
  - { name: last_commit, value: "2026-08-06" }
  - { name: lines_added, value: "1249" }
  - { name: lines_removed, value: "0" }
  - { name: package, value: view }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: imports, to: ./site.md, confidence: extracted, weight: 2, source: internal/view/view.go }
---
# internal/view

<!-- signpost:managed:summary -->
3 go files; 24 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
3 files:
- `internal/view/browser.go`
- `internal/view/view.go`
- `internal/view/view_test.go`

- **Exports** (24): `DefaultPort`, `Options`, `Serve`, `TestAssetsAreTypedLiterally`, `TestCheckHost`, `TestCheckURL`, `TestGraphIsServedVerbatim`, `TestHandlerAcceptsLoopbackHosts`, `TestHandlerHeadHasNoBody`, `TestHandlerRefusesNonLoopbackHost`, `TestHandlerRefusesWrites`, `TestHandlerServesNothingOffDisk`, `TestHandlerServesTheAssetSet`, `TestHandlerSetsHeadersOnErrorsToo`, `TestListenAskedPortCollisionIsAnError`, `TestListenBindsLoopbackOnly`, `TestOpenerNamesALauncher`, `TestRenderEscapesRepositoryStrings`, `TestRenderMakesNoOutboundRequest`, `TestRenderOmitsRepoBaseWhenEmpty`, `TestRenderStatesWhatWasRead`, `TestServeReportsAnAskedPortCollision`, `TestServeStopsOnContextCancel`, `TestViewMarkupMatchesPublishedViewer`

- **Imports**: [site](./site.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
