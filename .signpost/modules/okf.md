---
type: Module
title: internal/okf
description: 14 go files; 36 exported symbols.
resource: git://github.com/3rg0n/signpost@dfd1e5d229c029d8c3e1fad3c6c83588d130900e/internal/okf
generated: { by: signpost/dev, at: "2026-08-19" }
attributes:
  - { name: commits, value: "16" }
  - { name: exported, value: "36" }
  - { name: files, value: "14" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-19" }
  - { name: lines_added, value: "8769" }
  - { name: lines_removed, value: "205" }
  - { name: package, value: okf }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 63% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 3 }
  - { kind: co_changes, to: ./graph.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 7, source: internal/okf/bundle.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 3 }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 4, source: internal/okf/bundle_test.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 16 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 14 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./vcs.md, confidence: extracted, weight: 3 }
---
# internal/okf

<!-- signpost:managed:summary -->
14 go files; 36 exported symbols.
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

- **Exports** (36): `Actor`, `BundleDir`, `Finding`, `Finding.String`, `FindingBrokenLink`, `FindingConformance`, `FindingKind`, `FindingMissingBundle`, `FindingMissingPage`, `FindingOrphanPage`, `FindingOutOfDate`, `FindingPageList`, `FindingStaleResource`, `FindingStaleVerification`, `IndexPage`, `LogPage`, `ManifestFile`, `NewPage`, `Options`, `Page`, `Page.HumanText`, `Page.Managed`, `Page.Merge`, `Page.Render`, `ParsePage`, `PracticesPage`, `RecordedCommit`, `Region`, `Region.Managed`, `Result`, `Verification`, `Verify`, `VerifyCounts`, `VerifyResult`, `VerifyResult.OK`, `Write`

- **Changes with**: [internal/assemble](./assemble.md) ×3, [internal/graph](./graph.md) ×3, [internal/manifest](./manifest.md) ×3, [\(repository root\)](./root.md) ×16, [cmd/signpost](./signpost.md) ×14, [site](./site.md) ×2, [internal/vcs](./vcs.md) ×3

- **Imports**: [internal/graph](./graph.md) ×7, [internal/manifest](./manifest.md) ×4
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
