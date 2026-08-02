# AGENTS.md

**Read [`.signpost/index.md`](.signpost/index.md) before you start.** It is a compiled
map of this repository — every package, what depends on what, which places are most
connected — and reading it first is faster and more accurate than deriving the same
structure from the source tree.

This file exists because of a measured failure. Two agents were given the same task in
two repositories that both had a bundle committed. The one working in *this* repository
found `.signpost/` and used it; the one working in a repository with no textual mention
of signpost read eleven files by hand and never opened the bundle at all — it
re-derived from `go.mod`, `pyproject.toml`, `Cargo.toml`, and the CI workflow exactly
the structure that was sitting in twenty-eight pages it never looked at. A dot-directory
is not discoverable. A line in the file you are reading now is.

Signpost's own `internal/practice` reports a repository with no `AGENTS.md` as one where
"an agent working here has only the code to go on," so a repository that ships this tool
and lacks the pointer is making its own diagnosis about itself.

## Where to look

| You want | Read |
|---|---|
| the shape of the repository | [`.signpost/index.md`](.signpost/index.md) — start here; most-connected packages first |
| one package in detail | `.signpost/modules/<name>.md` — its files, exported symbols, and edges |
| how this repository builds, tests, and gates | [`.signpost/practices.md`](.signpost/practices.md) |
| the same data without parsing markdown | `.signpost/manifest.json` |
| why something is built the way it is | [`docs/adr/`](docs/adr/) — decisions, immutable once accepted |
| how the pipeline fits together | [`docs/design.md`](docs/design.md) |
| how to contribute | [`CONTRIBUTING.md`](CONTRIBUTING.md) |

The bundle describes structure, not intent. It will tell you that `internal/assemble`
depends on `internal/extract`; it will not tell you why the dependency runs that
direction. That is what the ADRs are for, and the two are complementary rather than
redundant.

## The bundle is generated

`.signpost/` is written by `signpost build` and committed
([ADR 0005](docs/adr/0005-commit-the-bundle-to-the-repository.md)). Two consequences for
anything you change here:

- **Do not hand-edit generated regions.** Prose between `<!-- signpost:managed:name -->`
  markers is replaced on every run. Text outside those markers is carried across
  byte-for-byte and is yours to write — a `## Notes` section on a page is the intended
  place for something the extractors cannot know.
- **CI owns the rebuild on `main`.** Pull requests run `verify` and write nothing; only
  a push to `main` rebuilds and commits the bundle. So do not commit bundle changes on a
  branch to make a diff look complete — it will conflict with the rebuild, and `verify`
  is what tells you whether the committed bundle is still true.

If you changed code and want to see the effect, `go run ./cmd/signpost build .` and read
the diff. That is also the fastest check that a change to the emitter did what you meant.

## Working in this repository

The gate is the same one CI runs, and it runs over the whole tree rather than the files
you touched: `gofmt -l`, `go vet ./...`, `staticcheck ./...`, `golangci-lint run`,
`gosec ./...`, `govulncheck ./...`, and `go test -count=2 -timeout 30m ./...`. The
timeout is raised from the default deliberately — the corpus tests shell out to real git
for every case, and on Windows the default is not a comfortable margin.

Two conventions that are load-bearing rather than stylistic:

- **Every fixed bug earns a stage in the corpus** (`testdata/corpus`, driven from
  `cmd/signpost/corpus_test.go`), not only a unit test in the package that owns the fix.
  A unit test proves the function behaves; a corpus stage proves the *binary* behaves on
  a repository whose shape this tree cannot produce. Several defects were invisible to
  green package tests for exactly that reason.
- **Assertions need a negative boundary.** A test that only checks the true case cannot
  distinguish working code from code that returns true for everything. Assert the count,
  and assert what must *not* appear.

Output is byte-stable by requirement, not by accident: the bundle is committed to other
people's repositories, so nondeterministic output is commit churn in someone else's
history. Anything that iterates a map before emitting needs to sort first.
