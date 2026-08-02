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
./signpost graph show .
./signpost graph export -format json -quiet . | jq '.nodes | length, (.edges | length)'
```

That job asserts what the run produced, not that it exited 0 — `export` exits 0
on an empty graph, so an exit-code check would pass on a total collapse. If a
change legitimately moves the node or edge counts below the floors in
`.github/workflows/ci.yml`, move the floor in the same commit and say why.

**`signpost hooks install` is not part of the gate, and will not become part of
it.** It adds a `post-commit` hook that prints one line when `.signpost/` has
fallen behind the code — a convenience for anyone building the bundle locally, and
nothing more. It cannot fail a commit, it does not rebuild anything, and skipping
it changes nothing about whether your change lands: `signpost verify` in CI is what
gates. It is worth knowing that it appends to whatever `post-commit` hook you
already have, that `hooks uninstall` removes only its own lines, and that it
installs where `core.hooksPath` points when you have one set.

## A fixed bug becomes a corpus regression

`testdata/corpus` is a synthetic repository — all four first-class languages, four
manifest ecosystems, and the filenames that break naive emitters. It is the only
harness that runs the whole binary against a repository signpost did not write, and
**every bug fix adds a stage to it**, in `cmd/signpost/corpus_test.go` and in the
`corpus` job. A unit test beside the fix is necessary and is not sufficient.

The reason is in the defects the harness has been extended for. Every one had green
unit tests in the package that owned the code, and every one shipped anyway:

- A YAML flow indicator in a path made four pages unreadable from that line down.
  Nothing in *this* repository's tracked paths contains a bracket, so no test here
  could express the input.
- A CRLF checkout made `verify` call every page stale, `build` rewrite all of them,
  and `build` report human notes on a bundle nobody had edited. This repository's
  `.gitattributes` pins `eol=lf`, so the one tree signpost is developed in is the
  one tree configured to hide it.
- The walk read signpost's own committed bundle, so the file census grew on every
  run. It needs a bundle already on disk to go wrong, and it moved no node or edge —
  a test asserting the graph is green through the whole defect.
- Every service in a compose file inherited every secret named in that file, so a
  reverse proxy was reported as reading the database password. It needs a *second*
  service in the file to be wrong about, and a unit test over one extractor call
  reads one file.
- An npm workspace's own packages were emitted as third-party dependencies, and the
  imports between them pointed at those fabricated nodes. It needs a package that
  exists in the tree *and* is imported by its published name — two things true at
  once, which one resolver call handed one specifier cannot express. The corpus was a
  single flat `package.json` and could not be wrong about this at all.
- A named tsconfig `paths` alias resolved nowhere, because nothing read the file that
  defines it — 542 of 3912 edges absent on a real repository, 14% of the graph, from a
  single unread mapping. It needs a config declaring `paths`, a package whose own
  config states only `extends`, and a file importing through the inherited alias. And
  the file is JSONC, so it needs comments in it: both real configs that declared
  `paths` carried them, and a strict JSON parse of either fails outright.

That is the pattern: a bug survives a package's own tests when the tree those tests
run in cannot express the condition. So when you fix one, ask what shape of
repository would have caught it, and put that shape in the corpus — a file, a path,
or a stage that manipulates the bundle before re-running the command. Two rules keep
these stages honest:

- **Assert the symptom a user would report,** not only the exit code. The CRLF stage
  checks the phantom "had human notes" count as well as `verify`, because a partial
  fix satisfying only `verify` would still print a fabricated number.
- **Add the counterpart that still fails.** For every "this is now accepted" stage,
  one that introduces real drift and requires a non-zero exit. Without it, the stage
  is satisfied by a fix that stopped checking. Prove it by disabling your fix and
  confirming both halves change verdict.
- **Add the negative boundary, not just the positive.** A stage asserting that a thing
  now resolves is satisfied by a fix that resolves *everything* — that resolution is
  wrong in the one direction the stage cannot see. Testing that 1+1 is 2 never catches
  an adder that answers 2 for everything. So a fix that made something match also needs
  an input that must **not** match: a near-miss of the name it now recognises, spelled
  the way that ecosystem's normalization is loosest about. The corpus keeps one per
  language and asserts the unresolved specifier *count*, which is what fails in both
  directions — over-claiming lowers it, over-reporting raises it. See
  [`testdata/corpus/README.md`](testdata/corpus/README.md#negative-boundaries).

## Dependencies

**A new direct dependency requires an ADR** in [`docs/adr/`](docs/adr/), and the
bar is high. The policy is not zero dependencies but *patchable* ones: each
direct dependency must be something we can bump ourselves, and there must be few
enough that bumping stays routine. A dependency whose own tree we cannot patch
means waiting on an upstream maintainer to fix a CVE in a tool people run in CI.
The full argument, including the alternatives rejected, is
[ADR 0002](docs/adr/0002-patchable-dependencies-not-zero-dependencies.md).

Write the ADR before the code, in Nygard short form, naming what the dependency
buys and what the stdlib alternative costs.

## Decisions

Read [`docs/adr/`](docs/adr/) before proposing anything structural. Six decisions
are recorded there and they bind changes rather than describe them — notably that
confidence is a first-class field on every node and edge
([0004](docs/adr/0004-confidence-is-a-first-class-field.md)), that module nodes
are one per directory and their IDs are a public contract
([0003](docs/adr/0003-directory-granularity-for-module-nodes.md)), and that the
bundle is committed, which is why determinism is a correctness property here
([0005](docs/adr/0005-commit-the-bundle-to-the-repository.md)).

An ADR is immutable once accepted. A change that reverses one adds a new ADR that
supersedes it rather than editing the old file — `docs/design.md` described a
dependency ADR 0001 had already removed, which is exactly the rot the immutability
rule exists to prevent.

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
minimal directory tree and the command you ran. `signpost graph show .` output on a
real repo is often enough.

## License

By contributing you agree your contribution is licensed under the
[MIT License](LICENSE).
