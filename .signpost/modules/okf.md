---
type: Module
title: internal/okf
description: 14 go files; 36 exported symbols.
resource: git://github.com/3rg0n/signpost@e1c5888c3a90ec5cb8f0d6fa356527ccd037b17c/internal/okf
generated: { by: signpost/dev, at: "2026-08-11" }
attributes:
  - { name: commits, value: "12" }
  - { name: exported, value: "36" }
  - { name: files, value: "14" }
  - { name: first_commit, value: "2026-07-30" }
  - { name: last_commit, value: "2026-08-11" }
  - { name: lines_added, value: "7820" }
  - { name: lines_removed, value: "194" }
  - { name: package, value: okf }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 50% }
edges:
  - { kind: imports, to: ./graph.md, confidence: extracted, weight: 7, source: internal/okf/bundle.go }
  - { kind: imports, to: ./manifest.md, confidence: extracted, weight: 4, source: internal/okf/bundle_test.go }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 12 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 10 }
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

- **Changes with**: [\(repository root\)](./root.md) ×12, [cmd/signpost](./signpost.md) ×10, [site](./site.md) ×2, [internal/vcs](./vcs.md) ×3

- **Imports**: [internal/graph](./graph.md) ×7, [internal/manifest](./manifest.md) ×4
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
