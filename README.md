# signpost

**Give models signposting for repos.**

signpost compiles a repository into a small, durable knowledge bundle that an
agent reads *before* it starts work — so it begins oriented instead of
re-deriving the same structure every session.

It is a compilation step, not a retrieval system: no vector database, no
embeddings, nothing to run in production. One binary, one command, output is
markdown committed to the repo.

```bash
signpost build .                      # write the bundle to .signpost/ — the point of the tool
signpost verify .                     # is the committed bundle still true? non-zero if not
signpost graph show .                 # report structure: hubs, cycles, bridges, islands
signpost view .                       # open the graph in a browser, served from 127.0.0.1
signpost graph export -format mermaid .   # render the graph for a diagram or another tool
```

`view` is the one command that listens on a port, and only while you are looking:
it serves the graph on 127.0.0.1 until you interrupt it, writes nothing, and there
is no flag to bind anywhere else.

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

First-class languages are Go, TypeScript/JavaScript, Python, Rust, Java, and Kotlin —
imports, public surface, and entrypoints, each extractor scored against hand-labeled
fixtures at F1 1.000 for both imports and symbols. Java and Kotlin share one resolution
map because the compiler does, and it is built from the `package` declarations in the
source: no `pom.xml` or `build.gradle` reader exists yet, so a JVM import that names no
package in the repository is reported as a gap rather than turned into a Maven coordinate
nobody declared.

Beyond source, signpost reads what a repository states about itself: `go.mod`,
`package.json`, `pyproject.toml`, `requirements.txt`, and `Cargo.toml` for
dependencies; Containerfiles, compose files, GitHub Actions workflows,
Kubernetes manifests, Helm charts, and Terraform for deployment; protobuf, OpenAPI/Swagger,
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

Git is the recommended setup and, where it is there, it is authoritative: what is
tracked, what is ignored, and which commit the bundle describes are git's business.
But it is not required. Run signpost on a tarball with no `.git` and you still get a
bundle — every page, under the same names — with the history line reporting `history
not read` and the pages carrying no `resource:` or `generated:` stamp at all, because a
commit nobody can check is worse than no commit. `verify` reports its staleness check as
skipped and exits zero.

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

Line endings are signpost's to choose, and it chooses LF — that is what makes the same
commit produce identical bytes on Windows and Linux. Pages are read back through the
same normalisation, so a checkout that git converted to CRLF is recognised as
up-to-date rather than reported as stale; you do not need to configure anything for the
bundle to be correct. Pinning `* text=auto eol=lf` in `.gitattributes` is still worth
doing in a repository that commits a bundle, for the ordinary reason — it keeps the
diffs readable.

Pass `-repo example.com/org/repo` to name the repository in each page's
`resource:` URI. It is asked for rather than derived from a git remote: a remote
URL is a property of your checkout, and a fork's remote names the upstream.
Without it, pages carry a commit-only resource, which is still enough to tell
whether a page describes the code in front of you.

## Point your agents at it

**A committed bundle is not a discovered bundle.** Add one line to your `AGENTS.md`,
`CLAUDE.md`, or `.github/copilot-instructions.md`:

```markdown
Read `.signpost/index.md` before starting work — it is a compiled map of this
repository's structure.
```

That line is the difference between the bundle being used and being ignored, and the
gap is not small. Given the same task in two repositories that both had a bundle
committed, an agent used it in the one whose README described signpost and ignored it
entirely in the one that did not — reading eleven files by hand to re-derive structure
that was sitting in twenty-eight pages it never opened. Models are trained to read
`README.md` and `AGENTS.md`; nothing trains them to look inside a dot-directory they
have never heard of. This repository's own [`AGENTS.md`](AGENTS.md) is that pointer, and
it is the whole fix.

signpost will not write those files for you — they encode your intent, and a generator
that overwrites them is how teams learn to distrust tooling
([design §6.2](docs/design.md)). What it will do is draft the line and let you place it:

```bash
signpost build -suggest-agents-md            # prints a stub, writes nothing at all
signpost build -suggest-agents-md >> AGENTS.md
```

The flag prints and exits — no bundle, no edit to `AGENTS.md`, nothing on disk. The `>>`
is yours to type, which is the boundary rather than an inconvenience.

And because a bundle nothing points at is the one failure a green build cannot show, a
build that finds no pointer says so:

```
nothing points at the bundle: no AGENTS.md and no README.md names .signpost/index.md,
so an agent has no reason to open it
  run `signpost build -suggest-agents-md >> AGENTS.md` to add one, or write your own
```

It looks in the five files a model is trained to open — `AGENTS.md`, `CLAUDE.md`,
`.cursorrules`, `.github/copilot-instructions.md`, `README.md` — for the index page by
name. Mentioning `.signpost/` in passing is not a pointer: a paragraph explaining what
the directory is does not tell an agent where to start.

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
invocation rather than the bundle — which is the main reason to put those flags in a
config file instead.

## Configuring it

Optional. A `.signpost.yml` at the repository root sets the defaults for the flags every
run would otherwise repeat, so `build` and `verify` cannot drift apart:

```yaml
repo: example.com/org/repo   # what every page's resource: names
ignore:                      # additional .gitignore-syntax patterns
  - generated/**
include_vendored: false      # analyse a committed node_modules or vendor/
include_fixtures: false      # analyse test fixtures
no_history: false            # skip the git pass
max_commits: 2000            # how far back the history pass reads
backend: none                # inferd | openai | none, for -semantic
model: google.gemma-3-12b-it # passed through verbatim
hooks:
  check: fast                # fast | verify, for the local hook
```

That is the whole list, and the list is short on purpose. **A key may only change a
default.** Anything deciding whether a check *fails* stays a flag — `-as-of-bundle`,
`-fail-on-cycle` — because a repository that can weaken its own gate by committing a file
is not gated. Those keys are refused with a reason rather than ignored, so a diff that
tries fails CI instead of quietly passing a weaker one. **There is nowhere to put a
credential**: the file is committed, so `api_key` is refused by name and
`SIGNPOST_OPENAI_API_KEY` is where it goes.

Three more things worth knowing. Precedence is **flag > environment > file > default**,
and a flag wins even when set to the zero value, so `-include-vendored=false` overrides
`include_vendored: true` in the file. The file is read from the root and nowhere else — no
user-level config, no `-config` path, no search upward, because a search path is how the
same checkout produces two different bundles for two people. And a file signpost cannot
read is **exit 2 and no bundle**, never a silent fall back to defaults: a missing colon is
the difference between the analysis you asked for and one that reported success.
[ADR 0011](docs/adr/0011-configuration-file-format-and-location.md) has the reasoning.

## Summaries from a model

Everything above is deterministic: counted files, parsed imports, real commits. What
none of it can honestly say is what a module is *for*. That takes a model, and
`build -semantic` is where one gets asked:

```bash
export SIGNPOST_BACKEND=inferd                  # a local daemon over IPC
# or
export SIGNPOST_BACKEND=openai                  # any OpenAI-compatible endpoint
export SIGNPOST_OPENAI_BASE_URL=https://…/v1
export SIGNPOST_OPENAI_API_KEY=…

signpost model check                            # prove the backend works first
signpost build -semantic -repo example.com/org/repo .
```

It is off unless you ask for it twice — a backend configured *and* the flag passed.
Configuring a backend is not consent to spend it on every build, and keeping the
flag out of your push workflow is what keeps that build offline and byte-stable.
Credentials are read from the environment only; there is no config file field for a
key, because config files get committed.

What a summary is allowed to be:

- **Grounded, or absent.** Every summary cites the files it rests on, and the
  citations are checked against the exact set signpost sent. One path that does not
  resolve drops the whole summary, not the citation — a summary with a bad citation
  quietly removed reads exactly like one that was right all along.
- **In its own region.** The prose lands under `## Role`, beside the counted
  `summary` rather than replacing it, with a line naming the model and the files it
  read. A later deterministic build carries it across untouched.
- **Unable to fail your build.** A backend that goes away stops the pass and names
  the modules it never reached; anything that cannot be grounded is dropped. The
  report goes to stderr and `-quiet` does not silence it.
- **Repeatable.** Summaries are cached by a content hash of the sources that were
  sent, in `.signpost/cache/`, which is gitignored. An unchanged module is not
  re-summarised, so a re-run produces no diff.

Repository content reaching a model is untrusted input, and treated as such: files
are wrapped in delimited blocks with signpost's own markers and chat-template
control tokens defanged, the response shape is fixed by a JSON Schema rather than
requested in prose, and HTML comments are stripped from the prose before it is
written so it cannot close the region it lands in. A path gets the same treatment,
and needs it for a reason worth knowing: a POSIX filename may contain a newline, so
a file can put a line of its own choosing inside the region that names it — with no
model in the loop. Marker syntax is escaped in everything signpost generates, and a
summary citing such a path is refused outright. That is mitigation, not proof —
[design §4.5](docs/design.md) is explicit about the residual risk.

To run it in CI, copy
[`.github/workflows/signpost-semantic.yml`](.github/workflows/signpost-semantic.yml):
weekly, `workflow_dispatch`, sharing a concurrency group with the push workflow so
the two never race for the branch, and skipping with a message when no backend is
configured.

## Running it in CI

Copy [`.github/workflows/signpost.yml`](.github/workflows/signpost.yml). It is the
workflow this repository uses on itself, and it is the setup that makes the bundle
useful to a team where nobody installed signpost: CI builds the map, commits it, and
everyone reads markdown.

Two jobs, with deliberately different strictness:

- **On push to the default branch**, rebuild the bundle, verify it strictly, and
  commit it only if the bytes changed. This is the only place the bundle is written.
- **On a pull request**, run `signpost verify -as-of-bundle` and write nothing.

The `-as-of-bundle` flag is not optional there, and the reason is worth knowing
before you delete it. The bundle is only ever built on the default branch, so on a
branch its committed `resource:` stamp names an older commit *by construction* — and
that stamp is part of every page's bytes. A strict verify therefore calls every page
stale on every pull request, including one that only fixed a typo in a docs file.
`-as-of-bundle` takes the two provenance fields from the bundle's own
`manifest.json` and compares content byte for byte, so a branch that changes what
the map says still fails; it names the commit it judged against in its output.
[ADR 0007](docs/adr/0007-the-bundle-names-the-commit-it-describes.md) records the
full contract.

Check out with `fetch-depth: 0` in both jobs. A shallow clone produces a bundle with
thinner history than the repository has while carrying an identical commit stamp.

**Building locally instead.** If you would rather not run signpost in CI, build the
bundle yourself and commit it — but commit the code first and the bundle second. A
single commit carrying both stamps its own parent, because the sha of the commit
carrying the stamp does not exist when the stamp is written, and the history
attributes for a directory inside that commit change once it lands. If you need one
atomic commit, build with `-no-history`: a structure-only bundle has nothing that
moves, and it verifies clean.

**A local reminder, optional.** `signpost hooks install` adds a `post-commit` hook
that prints one line when `.signpost/` has fallen behind the code:

```
signpost hooks install                    # add it
signpost hooks install -h                 # what it will write, and where
signpost hooks uninstall                  # remove it
```

It reports and never gates — the hook cannot fail a commit, does not rebuild
anything, and `signpost verify` in CI is the check that actually fails. It defaults
to a commit comparison that costs milliseconds; `signpost hooks run -check verify`
runs the accurate one, `SIGNPOST_HOOK_CHECK=verify` makes that the default for one
shell, and `hooks: {check: verify}` in `.signpost.yml` makes it the default for the
repository.

Two things it will tell you about, because both surprise people. It **appends** to
any `post-commit` hook already there and `hooks uninstall` removes only its own
lines, so a git-lfs hook survives both. And if you have `core.hooksPath` set — in
`~/.gitconfig`, say — git reads hooks only from there and ignores `.git/hooks`
entirely, so that is where the hook goes; when that directory is shared with every
repository on your machine, the install output says so. The lines added do nothing
in a repository without a bundle.

## Status

**v0.1.0 — deterministic core, complete.** No model required, no network.

| Component | State |
|---|---|
| Graph model, metrics, Louvain clustering | done |
| Discovery: gitignore, classification, bounded reads | done |
| Language extractors (Go, TS/JS, Python, Rust, Java, Kotlin) | done |
| Manifest + infrastructure extraction | done |
| Graph assembly and import resolution | done |
| Mermaid / DOT / GraphML / JSON export | done |
| `signpost graph show`, `signpost graph export` | done |
| Git signals (co-change, churn, ownership) | done |
| `signpost build` — OKF emit with edit preservation | done |
| `signpost verify` — conformance, links, staleness | done |
| `signpost.yml` — rebuild on push, gate pull requests | done |
| `signpost hooks` — optional local post-commit reminder | done |
| `.signpost.yml` — per-repository defaults, no gate keys | done |
| [Graph viewer](https://signpost.md/graph.html) — in `site/`, no JS dependencies | done |
| `signpost view` — the same viewer on 127.0.0.1, for any repository | done |
| Model backends: local IPC, or any OpenAI-compatible endpoint | done |
| `build -semantic` — module role summaries, grounded and cited | done |
| Semantic pass: doc-to-code linking, invariants, cluster labels | v0.3 |
| Viewer: search, diff between commits, deep links to source | v0.4 |

The deterministic core is usable end-to-end: build a bundle, commit it, and CI keeps
it honest. `build -semantic` adds the summaries that say what a module is *for*,
which no deterministic read can honestly produce — off unless you configure a
backend *and* pass the flag, so the ordinary build stays offline and byte-stable.
Every summary cites the files it rests on, and one citation that does not resolve
drops the summary rather than the citation.

See [docs/design.md](docs/design.md) for the full design, including the
supply-chain posture that motivates it, and [docs/adr/](docs/adr/) for the
decisions that bind it — why the dependency list is three modules long, why module
nodes are directories, why confidence is a field on every edge, and why the bundle
is committed.

## Build from source

Requires Go 1.26+. `go.mod` has three direct dependencies — the OpenTelemetry API, its
trace API, and its SDK — all Google-published and released together, so a CVE fix is one
`go get -u`. The policy in
[ADR 0002](docs/adr/0002-patchable-dependencies-not-zero-dependencies.md) is not zero
dependencies but *patchable* ones: each must be something we can bump ourselves when a
CVE lands, and there must be few enough that bumping stays routine. Everything else —
the four language extractors, the YAML reader, the graph algorithms, the clustering —
is stdlib.

The OTel exporter is hand-written for that reason.
[ADR 0014](docs/adr/0014-adopt-the-otel-sdk-and-write-the-exporter.md) has the
measurements: upstream's *HTTP* exporter links `google.golang.org/grpc`, protobuf, and
grpc-gateway — 65 gRPC packages for a transport that uses none of them — while the SDK
plus two methods of our own OTLP/JSON links ten modules and no gRPC at all.

The same holds for the site in `site/`, which carries the landing page and the
graph viewer: hand-written HTML, CSS, and JavaScript, with no `package.json` and no
lockfile ([ADR 0008](docs/adr/0008-the-viewer-lives-in-this-repository.md)). It is
published by a workflow that is off the merge path, so a broken deploy cannot fail
a build.

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
