# signpost

**Give models signposting for repos.**

signpost compiles a repository into a small, durable knowledge bundle that an
agent reads *before* it starts work — so it begins oriented instead of
re-deriving the same structure every session.

It is a compilation step, not a retrieval system: no vector database, no
embeddings, no server. One binary, one command, output is markdown committed to
the repo.

```bash
signpost build .        # writes .signpost/
signpost verify .       # conformance, links, staleness — non-zero on failure
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
- **Trust is graded and auditable.** OKF's `generated` / `verified` / `sources`
  fields distinguish unverified, machine-confirmed, and human-reviewed content,
  per page, with the producing model recorded.
- **Stale fails loudly.** `verify` exits non-zero. A silently stale knowledge
  artifact is worse than none — it is confidently wrong.
- **Deterministic.** Same commit in, identical bytes out. CI commits the
  bundle, so nondeterminism would mean commit churn.
- **Works with no model at all.** The deterministic pass produces a complete,
  accurate structural bundle. The semantic pass only adds what prose alone can
  supply.

## Status

**v0.1 in progress — deterministic core.** No model required, no network.

| Component | State |
|---|---|
| Graph model, metrics, clustering | done |
| Language extractors (Go, TS/JS, Python, Rust) | next |
| Manifest + infrastructure extraction | next |
| Git signals (co-change, churn, ownership) | next |
| OKF emit + verify | next |
| Mermaid / GraphML / DOT export | next |
| Semantic pass (inferd, OpenAI-compatible) | v0.2 |
| `signpost-view` — GitHub Pages viewer | v0.4, separate repo |

See [docs/design.md](docs/design.md) for the full design, including the
supply-chain posture that motivates it.

## Build

Requires Go 1.26+.

```bash
go build ./cmd/signpost
go test ./...
golangci-lint run ./...
```

## License

Cisco Internal.
