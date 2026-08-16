---
type: Module
title: internal/model
description: 15 go files; 36 exported symbols.
resource: git://github.com/3rg0n/signpost@b4c8f5076ffb2ea629c1401ed3d7029542906974/internal/model
generated: { by: signpost/dev, at: "2026-08-16" }
attributes:
  - { name: commits, value: "2" }
  - { name: exported, value: "36" }
  - { name: files, value: "15" }
  - { name: first_commit, value: "2026-07-31" }
  - { name: last_commit, value: "2026-08-02" }
  - { name: lines_added, value: "2853" }
  - { name: lines_removed, value: "0" }
  - { name: package, value: model }
  - { name: top_author, value: 3rg0n }
  - { name: top_author_share, value: 50% }
edges:
  - { kind: co_changes, to: ./root.md, confidence: extracted, weight: 2 }
  - { kind: co_changes, to: ./signpost.md, confidence: extracted, weight: 2 }
---
# internal/model

<!-- signpost:managed:summary -->
15 go files; 36 exported symbols.
<!-- /signpost:managed:summary -->

## Structure

<!-- signpost:managed:structure -->
15 files:
- `internal/model/backend.go`
- `internal/model/config.go`
- `internal/model/config_test.go`
- `internal/model/inferd.go`
- `internal/model/inferd_test.go`
- `internal/model/inferd_unix.go`
- `internal/model/inferd_windows.go`
- `internal/model/inferd_wire.go`
- `internal/model/inferd_wire_test.go`
- `internal/model/openai.go`
- `internal/model/openai_test.go`
- `internal/model/probe.go`
- `internal/model/probe_test.go`
- `internal/model/untrusted.go`
- `internal/model/untrusted_test.go`

- **Exports** (36): `Backend`, `BedrockBaseURL`, `Config`, `Defang`, `DefaultBedrockModel`, `DefaultInferdAddr`, `DefaultTimeout`, `EnvAPIKey`, `EnvAWSDefaultRegion`, `EnvAWSRegion`, `EnvBackend`, `EnvBaseURL`, `EnvBedrockToken`, `EnvModel`, `ErrUnavailable`, `Inferd`, `Inferd.Actor`, `Inferd.Complete`, `Kind`, `KindInferd`, `KindNone`, `KindOpenAI`, `New`, `OpenAI`, `OpenAI.Actor`, `OpenAI.Complete`, `ParseKind`, `ParseProbe`, `ProbeAnswer`, `ProbeAnswer.AnsweredCorrectly`, `ProbeRequest`, `Request`, `Result`, `Source`, `SystemPrompt`, `Wrap`

- **Changes with**: [\(repository root\)](./root.md) ×2, [cmd/signpost](./signpost.md) ×2
<!-- /signpost:managed:structure -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
