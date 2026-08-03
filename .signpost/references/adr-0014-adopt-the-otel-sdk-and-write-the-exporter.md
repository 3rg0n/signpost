---
type: Document
title: "ADR 0014: adopt the otel sdk and write the exporter"
description: "Architecture decision (Accepted), 44 rules read from 0014-adopt-the-otel-sdk-and-write-the-exporter.md."
resource: git://github.com/3rg0n/signpost@168ccfd4ee0b087f79607409cdc39e074991f1df/docs/adr/0014-adopt-the-otel-sdk-and-write-the-exporter.md
tags: [accepted, adr, constraint]
generated: { by: signpost/dev, at: "2026-08-03" }
attributes:
  - { name: number, value: "0014" }
  - { name: rules, value: "44" }
  - { name: sections, value: "14. Adopt the OpenTelemetry SDK and write the exporter / Consequences, 14. Adopt the OpenTelemetry SDK and write the exporter / Context, 14. Adopt the OpenTelemetry SDK and write the exporter / Context / The SDK is the wrong half to hand-roll, 14. Adopt the OpenTelemetry SDK and write the exporter / Context / The build tag is a trap, and this is the measurement, 14. Adopt the OpenTelemetry SDK and write the exporter / Context / The exporter is where the weight is, not the SDK, 14. Adopt the OpenTelemetry SDK and write the exporter / Context / Traces, not metrics, 14. Adopt the OpenTelemetry SDK and write the exporter / Context / What ADR 0002's rule actually asks, 14. Adopt the OpenTelemetry SDK and write the exporter / Context / `thlibo`'s telemetry, and where it diverges, 14. Adopt the OpenTelemetry SDK and write the exporter / Decision, 14. Adopt the OpenTelemetry SDK and write the exporter / Notes, 14. Adopt the OpenTelemetry SDK and write the exporter / Status" }
  - { name: status, value: Accepted }
---
# ADR 0014: adopt the otel sdk and write the exporter

<!-- signpost:managed:summary -->
Architecture decision (Accepted), 44 rules read from 0014-adopt-the-otel-sdk-and-write-the-exporter.md.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
1 file:
- `docs/adr/0014-adopt-the-otel-sdk-and-write-the-exporter.md`
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
