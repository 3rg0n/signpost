# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

@AGENTS.md

`CONTRIBUTING.md` is the authoritative statement of what a change must satisfy; read it before opening a PR.

## Commands

Go 1.26+. No setup beyond cloning.

```bash
go build ./cmd/signpost
go run ./cmd/signpost build .          # rebuild .signpost/ locally to see a change's effect; revert with `git checkout .signpost`
go test -count=2 -timeout 30m ./...    # full suite as CI runs it (twice: determinism is a requirement)
go test -run TestCorpusFindsEveryLanguage ./cmd/signpost   # a single test
go test ./internal/extract             # one package; includes the extractor F1 scoring harness
```

The full gate (all run over the whole tree, not just touched files):

```bash
gofmt -l .  &&  go vet ./...  &&  staticcheck ./...  &&  golangci-lint run ./...
gosec -quiet ./...  &&  govulncheck ./...  &&  gitleaks detect --no-git  &&  actionlint
```

- Use `-timeout 30m`: `internal/vcs` and `cmd/signpost` shell out to real `git`, and a default-timeout panic looks like a hang, not a timeout.
- `actionlint` silently skips shell without `shellcheck` on `PATH`. On Windows, install it from `@main`; v1.7.12 deadlocks on `run:` blocks over 4KB.
- Self-check that CI also runs: `./signpost graph show .` and `./signpost graph export -format json -quiet . | jq '.nodes | length, (.edges | length)'`. The node/edge floors live in `.github/workflows/ci.yml`; move them in the same commit if a change legitimately lowers the counts.

## Architecture

One binary (`cmd/signpost`), with every analysing command running the same deterministic pipeline (`cmd/signpost/pipeline.go`, design §4). This keeps `verify`, `graph`, `view` and `export` from disagreeing with `build`:

1. `internal/discover`: walk the tree (gitignore-aware), classify files, record vendored/fixture dirs without analysing them.
2. `internal/extract`: hand-written per-language extractors (Go uses `go/ast`) producing imports, symbols, and entrypoints. They're scored against hand-labeled fixtures, not asserted.
3. `internal/manifest`: reads package manifests, build/deploy config, and compose/CI files. Readers record secret *names*, never values.
4. `internal/vcs`: git history signals (co-change), which annotate the graph but never draw it (ADR 0020).
5. `internal/assemble`: resolves imports to module nodes (one per directory; IDs are a public contract, ADR 0003) and builds the graph. Every node/edge carries confidence `extracted` / `inferred` / `ambiguous` (ADR 0004).
6. `internal/graph`: hand-written algorithms: Louvain clustering, cycles, bridges, hubs.
7. `internal/okf`: emits/verifies the Open Knowledge Format bundle, with its own hand-written tolerant YAML (ADR 0001). It preserves text outside `signpost:managed` markers byte-for-byte and normalizes line endings.

Off the deterministic path:
- `internal/semantic` + `internal/model` run the opt-in model pass with explicit egress (ADR 0009). Repository text is quoted as untrusted, never treated as instruction.
- `internal/practice` handles repository-practice findings.
- `internal/export` renders mermaid/dot/graphml/json.
- `internal/view` + `site/` (embedded, zero-JS-dependency viewer) back `signpost view`.
- `internal/scaffold` (embedded workflows, tested against this repo's own), `internal/hook`, and `internal/selfupdate` cover the remaining commands.

`docs/design.md` is the long-form design (section numbers are cited throughout code comments). `docs/adr/` holds decisions that bind changes; they're immutable, so a reversal is a new superseding ADR. A new direct dependency requires an ADR first (only the three OpenTelemetry modules are direct deps today).

## Commits and PRs

- Closing keywords (`Closes/Fixes/Resolves #n`) anywhere in a message are checked by CI against this repo's real issues. Don't use them with numbers from other trackers; check with `gh issue view <n> --repo 3rg0n/signpost`.
- A skip-CI keyword anywhere in a message (even quoted in prose) suppresses the run, so describe it without brackets.
- Releases: tag the merge commit, not the `Rebuild the signpost bundle` commit after it (its skip keyword suppresses the release workflow). See CONTRIBUTING.md § Releasing.
