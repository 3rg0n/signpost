---
type: Module
title: internal/extract
description: 34 go files; 80 exported symbols.
resource: git://github.com/3rg0n/signpost@87c06ede8626e41ed763c3bac660e77870c3086f/internal/extract
generated: { by: signpost/dev, at: "2026-08-15" }
attributes:
  - { name: commits, value: "10" }
  - { name: exported, value: "80" }
  - { name: files, value: "34" }
  - { name: first_commit, value: "2026-07-29" }
  - { name: last_commit, value: "2026-08-15" }
  - { name: lines_added, value: "19755" }
  - { name: lines_removed, value: "62" }
  - { name: package, value: extract }
  - { name: top_author, value: Ergon Copeland }
  - { name: top_author_share, value: 60% }
edges:
  - { kind: co_changes, to: ./assemble.md, confidence: extracted, weight: 6 }
  - { kind: co_changes, to: ./discover.md, confidence: extracted, weight: 7 }
  - { kind: imports, to: ./discover.md, confidence: extracted, weight: 31, source: internal/extract/cfamily.go }
  - { kind: co_changes, to: ./manifest.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 10 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 6 }
  - { kind: co_changes, to: ./site.md, confidence: extracted, weight: 5 }
  - { kind: imports, to: ./sqlstmt.md, confidence: extracted, weight: 2, source: internal/extract/sql.go }
---
# internal/extract

<!-- signpost:managed:summary -->
34 go files; 80 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
34 files:
- `internal/extract/cfamily.go`
- `internal/extract/cfamily_test.go`
- `internal/extract/csharp.go`
- `internal/extract/csharp_test.go`
- `internal/extract/extract.go`
- `internal/extract/extract_test.go`
- `internal/extract/golang.go`
- `internal/extract/golang_test.go`
- `internal/extract/java.go`
- `internal/extract/java_test.go`
- `internal/extract/kotlin.go`
- `internal/extract/kotlin_test.go`
- `internal/extract/lines.go`
- `internal/extract/lines_test.go`
- `internal/extract/php.go`
- `internal/extract/php_test.go`
- `internal/extract/powershell.go`
- `internal/extract/powershell_test.go`
- `internal/extract/python.go`
- `internal/extract/python_test.go`
- `internal/extract/ruby.go`
- `internal/extract/ruby_test.go`
- `internal/extract/rust.go`
- `internal/extract/rust_test.go`
- `internal/extract/score.go`
- `internal/extract/score_test.go`
- `internal/extract/sfc.go`
- `internal/extract/sfc_test.go`
- `internal/extract/shell.go`
- `internal/extract/shell_test.go`
- `internal/extract/sql.go`
- `internal/extract/sql_test.go`
- `internal/extract/typescript.go`
- `internal/extract/typescript_test.go`

- **Exports** (80): `CExtractor`, `CExtractor.Extract`, `CExtractor.Langs`, `CSharpExtractor`, `CSharpExtractor.Extract`, `CSharpExtractor.Langs`, `DefaultRegistry`, `Expected`, `Extractor`, `Facts`, `Facts.ExportedSymbols`, `Facts.ImportPaths`, `Facts.Normalize`, `Facts.SymbolNames`, `Failure`, `FirstSentence`, `Fixture`, `GoExtractor`, `GoExtractor.Extract`, `GoExtractor.Langs`, `Import`, `IncludePath`, `JavaExtractor`, `JavaExtractor.Extract`, `JavaExtractor.Langs`, `KotlinExtractor`, `KotlinExtractor.Extract`, `KotlinExtractor.Langs`, `LangScore`, `LangScore.MeetsTarget`, `LangScore.Report`, `NewRegistry`, `PHPExtractor`, `PHPExtractor.Extract`, `PHPExtractor.Langs`, `PowerShellExtractor`, `PowerShellExtractor.Extract`, `PowerShellExtractor.Langs`, `PythonExtractor`, `PythonExtractor.Extract`, `PythonExtractor.Langs`, `Query`, `Registry`, `Registry.For`, `Registry.Langs`, `Registry.Register`, `Registry.Run`, `RubyExtractor`, `RubyExtractor.Extract`, `RubyExtractor.Langs`, `RunResult`, `RustExtractor`, `RustExtractor.Extract`, `RustExtractor.Langs`, `SFCExtractor`, `SFCExtractor.Extract`, `SFCExtractor.Langs`, `Score`, `Score.F1`, `Score.Precision`, and 20 more

- **Changes with**: [internal/assemble](./assemble.md) ×6, [internal/discover](./discover.md) ×7, [internal/manifest](./manifest.md) ×2, [\(repository root\)](./root.md) ×10, [cmd/signpost](./signpost.md) ×6, [site](./site.md) ×5

- **Imports**: [internal/discover](./discover.md) ×31, [internal/sqlstmt](./sqlstmt.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
