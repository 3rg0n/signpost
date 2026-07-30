# signpost

**Give models signposting for repos.**

signpost compiles a repository into a small, durable knowledge bundle that an
agent reads *before* it starts work — so it begins oriented instead of
re-deriving the same structure every session.

It is a compilation step, not a retrieval system: no vector database, no
embeddings, no server. One binary, one command, output is markdown committed to
the repo.

```bash
signpost graph .                      # report structure: hubs, cycles, bridges, islands
signpost export --format mermaid .    # render the graph for a diagram or another tool
```

## Install

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/3rg0n/signpost/main/install.sh | sh
```

```powershell
# Windows
iex "& { $(irm https://raw.githubusercontent.com/3rg0n/signpost/main/install.ps1) }"
```

Both scripts fetch a tagged release archive and verify its SHA-256 against the
`checksums.txt` published with that release before installing anything. Pass a
version to pin one (`sh -s -- --version v0.1.0`, or `-Version v0.1.0` on
Windows). Or, with a Go toolchain:

```bash
go install github.com/3rg0n/signpost/cmd/signpost@latest
```

## Why it exists

An agent opening an unfamiliar repo rediscovers the same things every time:
which module owns what, where the entrypoints are, which files move together,
what the docs claim versus what the code does. That work is paid for on every
session, discarded at the end of every session, and inconsistent between runs.

signpost does the derivation once, in CI, at a known commit — and writes it down
where humans can correct it in place, so the corrections survive.

## Properties

- **Useful without signpost installed.** The output is
  [Open Knowledge Format](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
  markdown in the repo. Agents, people, and static site generators read it
  directly. signpost maintains the bundle; it does not serve it.
- **Human edits compound.** Generated prose lives between managed markers.
  Everything outside them is yours and is never touched. A generator that
  clobbers corrections teaches people to ignore it.
- **Trust is graded and auditable.** Every node and edge carries `extracted`,
  `inferred`, or `ambiguous`, and every export format preserves the distinction —
  dashed arrows in the diagram formats, a verbatim attribute in the data
  formats. A rendered graph that flattened the two would make a guess look like
  a fact.
- **Stale fails loudly.** `verify` exits non-zero. A silently stale knowledge
  artifact is worse than none — it is confidently wrong.
- **Deterministic.** Same commit in, identical bytes out. CI commits the
  bundle, so nondeterminism would mean commit churn.
- **Works with no model at all.** The deterministic pass produces a complete,
  accurate structural bundle. The semantic pass only adds what prose alone can
  supply.

## What it reads

First-class languages are Go, TypeScript/JavaScript, Python, and Rust — imports,
public surface, and entrypoints, each extractor scored against hand-labeled
fixtures at F1 1.000 for both imports and symbols.

Beyond source, signpost reads what a repository states about itself: `go.mod`,
`package.json`, `pyproject.toml`, `requirements.txt`, and `Cargo.toml` for
dependencies; Containerfiles, compose files, GitHub Actions workflows,
Kubernetes manifests, and Helm charts for deployment; protobuf, OpenAPI/Swagger,
and GraphQL SDL for contracts; SQL migrations, CODEOWNERS, ADRs, and Makefiles
for the rest. Secrets are recorded as *references* — a name and its key names,
never a value — because the bundle gets committed.

It also reads the repository's own history. Churn, first and last commit dates,
and author concentration land on each module; directories that keep changing in
the same commit become a co-change edge weighted by how often. That is the one
kind of coupling no static read can find — a handler and the migration it
depends on, a proto and its generated client, a config key and the code that
reads it are all coupled, and none of them is an import. Pass `-no-history` to
skip it, `-max-commits` to change how far back the walk goes. In CI, check out
with `fetch-depth: 0`: a shallow clone yields real but truncated signals, and
signpost says so rather than presenting them as the whole history.

## Status

**v0.1 in progress — deterministic core.** No model required, no network.

| Component | State |
|---|---|
| Graph model, metrics, Louvain clustering | done |
| Discovery: gitignore, classification, bounded reads | done |
| Language extractors (Go, TS/JS, Python, Rust) | done |
| Manifest + infrastructure extraction | done |
| Graph assembly and import resolution | done |
| Mermaid / DOT / GraphML / JSON export | done |
| `signpost graph`, `signpost export` | done |
| Git signals (co-change, churn, ownership) | done |
| `signpost build` — OKF emit with edit preservation | in progress |
| `signpost verify` | in progress |
| Semantic pass (local IPC, or any OpenAI-compatible endpoint) | v0.2 |
| `signpost-view` — GitHub Pages viewer | v0.4, separate repo |

`build` is deliberately absent from the binary until the emitter lands. A
command that wrote an incomplete bundle would be worse than one that is not
offered, because the bundle is what agents trust.

See [docs/design.md](docs/design.md) for the full design, including the
supply-chain posture that motivates it.

## Build from source

Requires Go 1.26+. There are no third-party dependencies — `go.mod` has no
`require` block.

```bash
go build ./cmd/signpost
go test ./...
golangci-lint run ./...
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). The short version: every change passes
the full gate — format, build, vet, tests, `staticcheck`, `golangci-lint`,
`gosec`, `govulncheck`, `gitleaks` — and a new direct dependency needs an ADR.

## License

[MIT](LICENSE) © 3rg0n
