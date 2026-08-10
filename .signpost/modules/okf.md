---
type: Module
title: internal/okf
description: 14 go files; 35 exported symbols.
resource: git://github.com/3rg0n/signpost@82cb942ea2098d8c7d0a14ddd42b71a21b44db8b/internal/okf
generated: { by: signpost/dev, at: "2026-08-11" }
attributes:
  - { name: commits, value: "11" }
  - { name: exported, value: "35" }
  - { name: files, value: "14" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-10" }
  - { name: lines_added, value: "7548" }
  - { name: lines_removed, value: "173" }
  - { name: package, value: okf }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 55% }
edges:
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 7, source: internal/okf/bundle.go }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 4, source: internal/okf/bundle_test.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 11 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 9 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./vcs.md, confidence: extracted, weight: 3 }
---
# internal/okf

<!-- signpost:managed:summary -->
14 go files; 35 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
14 files:
- `internal/okf/bundle.go`
- `internal/okf/bundle_test.go`
- `internal/okf/emit.go`
- `internal/okf/emit_test.go`
- `internal/okf/frontmatter.go`
- `internal/okf/frontmatter_test.go`
- `internal/okf/manifest.go`
- `internal/okf/manifest_test.go`
- `internal/okf/page.go`
- `internal/okf/page_test.go`
- `internal/okf/verify.go`
- `internal/okf/verify_test.go`
- `internal/okf/yaml.go`
- `internal/okf/yaml_test.go`

- **Exports** (35): `Actor`, `BundleDir`, `Finding`, `Finding.String`, `FindingBrokenLink`, `FindingConformance`, `FindingKind`, `FindingMissingBundle`, `FindingMissingPage`, `FindingOrphanPage`, `FindingOutOfDate`, `FindingStaleResource`, `FindingStaleVerification`, `IndexPage`, `LogPage`, `ManifestFile`, `NewPage`, `Options`, `Page`, `Page.HumanText`, `Page.Managed`, `Page.Merge`, `Page.Render`, `ParsePage`, `PracticesPage`, `RecordedCommit`, `Region`, `Region.Managed`, `Result`, `Verification`, `Verify`, `VerifyCounts`, `VerifyResult`, `VerifyResult.OK`, `Write`

- **Changes with**: [\(repository root\)](./root.md) ×11, [cmd/signpost](./signpost.md) ×9, [site](./site.md) ×2, [internal/vcs](./vcs.md) ×3

- **Imports**: [internal/graph](./graph.md) ×7, [internal/manifest](./manifest.md) ×4
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
