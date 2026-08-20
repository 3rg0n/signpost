---
type: Module
title: internal/discover
description: 5 go files; 44 exported symbols.
resource: git://github.com/3rg0n/signpost@84f4b54f22fbae91331c174d67531defae5e4faf/internal/discover
generated: { by: signpost/dev, at: "2026-08-20" }
attributes:
  - { name: commits, value: "14" }
  - { name: exported, value: "44" }
  - { name: files, value: "5" }
  - { name: first_commit, value: "2026-07-29" }
  - { name: last_commit, value: "2026-08-15" }
  - { name: lines_added, value: "3081" }
  - { name: lines_removed, value: "101" }
  - { name: package, value: discover }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 50% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 7 }
  - { kind: co_changes, to: ./extract.md, confidence: extracted, weight: 7 }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 5 }
  - { kind: co_changes, to: ./practice.md, confidence: extracted, weight: 4 }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 14 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 12 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 8 }
---
# internal/discover

<!-- signpost:managed:summary -->
5 go files; 44 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
5 files:
- `internal/discover/classify.go`
- `internal/discover/discover.go`
- `internal/discover/discover_test.go`
- `internal/discover/ignore.go`
- `internal/discover/ignore_test.go`

- **Exports** (44): `Class`, `ClassContract`, `ClassData`, `ClassDoc`, `ClassInfra`, `ClassManifest`, `ClassMigration`, `ClassOther`, `ClassOwnership`, `ClassSource`, `DefaultMaxTotalBytes`, `Elision`, `File`, `HeadTailBytes`, `Lang`, `LangAstro`, `LangC`, `LangCSharp`, `LangCpp`, `LangGo`, `LangJS`, `LangJava`, `LangKotlin`, `LangObjC`, `LangOther`, `LangPHP`, `LangPowerShell`, `LangPython`, `LangRuby`, `LangRust`, `LangShell`, `LangSvelte`, `LangTS`, `LangVue`, `MaxFullBytes`, `MaxFullLines`, `Options`, `Result`, `Result.Analyses`, `Result.ByClass`, `Result.Sources`, `Result.Unclassified`, `Skip`, `Walk`

- **Changes with**: [internal/assemble](./assemble.md) ×7, [internal/extract](./extract.md) ×7, [internal/manifest](./manifest.md) ×5, [internal/practice](./practice.md) ×4, [\(repository root\)](./root.md) ×14, [cmd/signpost](./signpost.md) ×12, [site](./site.md) ×8
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
