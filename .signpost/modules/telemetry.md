---
type: Module
title: internal/telemetry
description: 5 go files; 24 exported symbols.
resource: git://github.com/3rg0n/signpost@86072bf2e78fb84ab888f58a38c9494af3fac29a/internal/telemetry
generated: { by: signpost/dev, at: "2026-08-04" }
attributes:
  - { name: commits, value: "1" }
  - { name: exported, value: "24" }
  - { name: files, value: "5" }
  - { name: first_commit, value: "2026-08-02" }
  - { name: last_commit, value: "2026-08-02" }
  - { name: lines_added, value: "1525" }
  - { name: lines_removed, value: "0" }
  - { name: package, value: telemetry }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 100% }
edges:
  - { kind: imports, to: ../references/go-go-opentelemetry-io-otel.md, confidence: extracted, weight: 5, source: internal/telemetry/exporter.go }
  - { kind: imports, to: ../references/go-go-opentelemetry-io-otel-sdk.md, confidence: extracted, weight: 4, source: internal/telemetry/exporter.go }
  - { kind: imports, to: ../references/go-go-opentelemetry-io-otel-trace.md, confidence: extracted, weight: 2, source: internal/telemetry/exporter.go }
---
# internal/telemetry

<!-- signpost:managed:summary -->
5 go files; 24 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
5 files:
- `internal/telemetry/exporter.go`
- `internal/telemetry/exporter_test.go`
- `internal/telemetry/helpers_test.go`
- `internal/telemetry/telemetry.go`
- `internal/telemetry/telemetry_test.go`

- **Imports**: [go.opentelemetry.io/otel](../references/go-go-opentelemetry-io-otel.md) ×5, [go.opentelemetry.io/otel/sdk](../references/go-go-opentelemetry-io-otel-sdk.md) ×4, [go.opentelemetry.io/otel/trace](../references/go-go-opentelemetry-io-otel-trace.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
