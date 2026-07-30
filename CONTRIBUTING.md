# Contributing to signpost

Contributions are welcome. This file states what a change has to satisfy before
it lands, so a review is about the change and not about the process.

## Getting set up

Go 1.26 or later. Nothing else — signpost has no third-party dependencies, so
there is no install step beyond cloning.

```bash
git clone https://github.com/3rg0n/signpost
cd signpost
go build ./cmd/signpost
go test ./...
```

## The gate

Every change passes all of these across the whole module, not just the files it
touched. CI runs them; run them locally first.

```bash
gofmt -l .              # must print nothing
go build ./...
go vet ./...
go test -count=2 ./...  # twice, because determinism is a requirement here
staticcheck ./...
golangci-lint run ./...
gosec -quiet ./...
govulncheck ./...
gitleaks detect --no-git
```

`-count=2` is not superstition. signpost's output is committed to the
repositories it analyses, so nondeterministic output is commit churn in someone
else's repo. Several packages have tests that render the same graph five times
and compare bytes; a change that makes output depend on map iteration order
fails there.

CI also runs signpost on signpost, which is the only check that exercises
discovery, extraction, manifest reading, resolution, clustering, and export
together against a real tree instead of a fixture. Run it yourself the same way:

```bash
go build -o signpost ./cmd/signpost
./signpost graph .
./signpost export -format json -quiet . | jq '.nodes | length, (.edges | length)'
```

That job asserts what the run produced, not that it exited 0 — `export` exits 0
on an empty graph, so an exit-code check would pass on a total collapse. If a
change legitimately moves the node or edge counts below the floors in
`.github/workflows/ci.yml`, move the floor in the same commit and say why.

## Dependencies

**A new direct dependency requires an ADR** in `docs/adr/`, and the bar is
high. The policy is not zero dependencies but *patchable* ones: each direct
dependency must be something we can bump ourselves, and there must be few enough
that bumping stays routine. A dependency whose own tree we cannot patch means
waiting on an upstream maintainer to fix a CVE in a tool people run in CI.

Write the ADR before the code, in Nygard short form, naming what the dependency
buys and what the stdlib alternative costs.

## What a change should look like

- **Confidence is load-bearing.** Anything that produces a node or an edge marks
  it `extracted`, `inferred`, or `ambiguous`, and anything that renders one
  preserves the distinction. A guess that is presented as a fact is the failure
  mode this project exists to avoid, so a change that flattens the two is a bug
  even if every test passes.
- **Extractors are scored, not asserted.** A new or changed extractor is
  measured by the harness in `internal/extract` against hand-labeled fixtures —
  precision, recall, and F1 per fact kind. The targets are F1 0.95 for imports
  and 0.90 for symbols. Include an adversarial fixture whose declarations live
  inside strings and comments: the dominant failure of a line-oriented extractor
  is inventing a declaration, not missing one.
- **Secrets are references.** A reader may record a secret's name and its key
  names. It may never record a value, and it may never open a file referenced as
  holding one. The bundle is committed and published.
- **Repository content is untrusted.** Text from a scanned repo — especially a
  rules file — is quoted and attributed, never adopted as instruction.
- **Match the surrounding code.** Comment where the reasoning isn't evident from
  the code, not on every line. No abstractions for a single call site.

## Commits and pull requests

Write the commit message as a description of the change and why it was made.
Open the PR against `main`; CI runs the gate above plus a determinism check.

Bug reports are most useful with the repository shape that triggered them — a
minimal directory tree and the command you ran. `signpost graph .` output on a
real repo is often enough.

## License

By contributing you agree your contribution is licensed under the
[MIT License](LICENSE).
