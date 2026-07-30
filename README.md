# signpost

**Give models signposting for repos.**

signpost compiles a repository into a small, durable knowledge bundle that an
agent reads *before* it starts work — so it begins oriented instead of
re-deriving the same structure every session.

It is a compilation step, not a retrieval system: no vector database, no
embeddings, no server. One binary, one command, output is markdown committed to
the repo.

```bash
signpost build .                      # write the bundle to .signpost/ — the point of the tool
signpost verify .                     # is the committed bundle still true? non-zero if not
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

## What it writes

`signpost build` writes `.signpost/`: an `index.md` to start from, one page per
concept under `modules/`, `interfaces/`, `references/` and so on, a `log.md`
recording each date signpost ran, and a `manifest.json` for tools that would
rather not parse markdown. Commit it — that is the whole point, and
[ADR 0005](docs/adr/0005-commit-the-bundle-to-the-repository.md) records why.

Two rules govern rewriting, and they are why the bundle is safe to hand-edit:

- **Only the managed regions are regenerated.** Generated prose sits between
  `<!-- signpost:managed:name -->` markers. Everything else on the page — a
  `## Notes` section, a paragraph correcting the summary, a key you added to the
  frontmatter — is carried across byte-for-byte, and the run reports how many
  pages it carried notes on.
- **Nothing is deleted.** A page describing a directory that no longer exists is
  reported as stale and left alone, because a rename would otherwise silently
  delete the notes somebody wrote on it.
- **A review that no longer applies says so.** If you add a `verified:` block
  recording that you checked a page, and the commit it described has since
  changed, the block is kept and the page gains `status: stale-verification`. Your
  name and date are the audit trail; the status is the part that stops the page
  from claiming a human vouched for code they never saw. Re-review, record the
  page's current `resource:`, and the next run clears it.

Pass `-repo example.com/org/repo` to name the repository in each page's
`resource:` URI. It is asked for rather than derived from a git remote: a remote
URL is a property of your checkout, and a fork's remote names the upstream.
Without it, pages carry a commit-only resource, which is still enough to tell
whether a page describes the code in front of you.

## What it checks

`signpost verify` answers one question — is the committed bundle still true of
this tree? — and answers it with an exit code, so CI can gate on it:

```bash
signpost verify -repo example.com/org/repo .   # 0 if it holds, 1 if it does not
```

Five checks, per [design §4.6](docs/design.md): frontmatter parses and carries a
`type`, with the reserved filenames used correctly; every `edges[].to`,
`sources[].resource`, and prose link resolves to a page in the bundle; every
`resource:` names the commit being described; and a rebuild would change nothing.
That last one is checked by re-running the emitter and merging against what is on
disk, which is also how a page whose managed marker got broken by hand — and so
quietly stopped regenerating — gets caught.

Two things are deliberate about the output:

- **It says what it checked, on a pass as well as a failure.** "ok" over zero
  pages and "ok" over eighty read the same in a CI log, and only one of them is a
  result. Any check that could not run is named as skipped — an unreported skip is
  the false pass the command exists to prevent.
- **Warnings are not failures.** A page describing a directory that no longer
  exists, and a `verified:` block that has gone stale, are reported and exit zero.
  Neither makes the bundle wrong, and a gate that went red on the litter it is
  designed to leave behind is a gate people switch off.

Pass verify the same flags as the build it is checking. `-repo` feeds every page's
`resource:`, so a mismatch there reports a real difference that describes the
invocation rather than the bundle.

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
| `signpost build` — OKF emit with edit preservation | done |
| `signpost verify` — conformance, links, staleness | done |
| Semantic pass (local IPC, or any OpenAI-compatible endpoint) | v0.2 |
| `signpost-view` — GitHub Pages viewer | v0.4, separate repo |

The one thing still missing before this is usable end-to-end in CI is the workflow
that runs it: a `signpost.yml` that rebuilds the bundle on push and fails a pull
request whose bundle has gone stale.

See [docs/design.md](docs/design.md) for the full design, including the
supply-chain posture that motivates it, and [docs/adr/](docs/adr/) for the
decisions that bind it — why the dependency list is empty, why module nodes are
directories, why confidence is a field on every edge, and why the bundle is
committed.

## Build from source

Requires Go 1.26+. There are no third-party dependencies — `go.mod` has no
`require` block. That is an outcome of the policy in
[ADR 0002](docs/adr/0002-patchable-dependencies-not-zero-dependencies.md), not the
policy itself: the rule is that every dependency must be one we can bump
ourselves, and the bar is high enough that nothing has cleared it.

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
