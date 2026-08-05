# Corpus — a repository shaped like the ones signpost runs on

A synthetic multi-language repository, exercised end to end by
`cmd/signpost/corpus_test.go` and by the `corpus` job in `.github/workflows/ci.yml`.

## Why this exists

Signpost runs on itself in CI (`signpost.yml`), and that catches a great deal — but it
can only exercise the code paths *this tree* contains. This tree is a Go repository with
kebab-case filenames, so self-hosting structurally cannot reach:

- a TypeScript, Python, or Rust extractor,
- an npm, Cargo, or pyproject manifest reader,
- a Compose or Kubernetes file,
- **a path containing a character that is an indicator in YAML.**

The last one was [issue #9](https://github.com/3rg0n/signpost/issues/9): a Next.js
dynamic route (`app/tools/[slug]/page.tsx`) was written unquoted into a YAML flow
mapping, which made the frontmatter unparseable from that line on and silently dropped
every `edges[]` entry after it. `verify` reported it as a warning and exited 0. Nothing
in this repository has a bracket in a filename, so no amount of dogfooding would have
found it. That is the gap this directory closes.

## What is deliberately hostile here

Every one of these is a real pattern from a real ecosystem, not a contrived string:

| Path | Why |
|---|---|
| `ts/app/tools/[slug]/page.tsx` | Next.js dynamic route. `[` opens a YAML flow sequence. Issue #9. |
| `ts/app/(marketing)/page.tsx` | Next.js route group. Parentheses break a markdown link target. |
| `ts/app/blog/[...rest]/page.tsx` | Next.js catch-all route. Brackets plus dots. |
| `py/greeter/data,notes.py` | A comma is legal in a POSIX filename and terminates a YAML flow entry. |
| `go/greeter/greeter_test.go` | A test file, so `tested_by` has something to resolve. |

## Negative boundaries

Every other assertion in the harness is a positive: this edge exists, that page exists. A
positive is satisfied by a resolver that is *too generous* exactly as well as by a correct
one — claim every specifier as internal and every edge assertion stays green while the map is
wrong in the direction nobody can see. Testing that 1+1 is 2 never catches an adder that
answers 2 for everything.

So each language carries a deliberate near-miss: a name close enough to a real one that a
matcher slightly too loose swallows it, and which nothing declares.

| Specifier | Shadows | The looseness it catches |
|---|---|---|
| `example.com/corpus/greeterx/format` | the `go.mod` module `example.com/corpus/greeter` | a module prefix compared as a string instead of by path segment |
| `@corpus/apples/juice` | the tsconfig alias prefix `@corpus/app/` | an alias prefix compared without its trailing slash |
| `httpx_extras` | the declared `httpx` | PEP 503 name normalization applied as a prefix match |
| `serde_yaml::Value` | the declared `serde` | Cargo's dash/underscore equivalence applied too widely |
| `pathe/utils` | the Node builtin `path` | a builtin matched as a string prefix instead of by first path segment |
| `winreg_helpers` | the Python stdlib `winreg` | a stdlib name matched as a string prefix — the same looseness as the row above, against a table of 220 names rather than 44 |
| `com.example.apiv2` | the declared Java package `com.example.api` | a package prefix compared as a string instead of as a dot-delimited name |
| `javax.servlet.http` | the JDK's `javax.crypto` | `javax` matched on its first segment, which folds Java EE artifacts somebody upgrades into the platform |
| `kotlinx.coroutines` | the Kotlin stdlib `kotlin` | a runtime prefix matched as a string, which reclassifies three separately versioned artifacts as the toolchain |
| `org.junit.jupiter.api` | nothing — see below | not a near-miss: a real declared dependency, unresolvable because signpost reads no `pom.xml` or `build.gradle` |

Each must be reported as a gap and land nowhere else. Two wrong homes are possible and both
are worse than the gap: an edge into this repository invents structure, and an external node
invents a supply-chain entry nobody declared.

`TestCorpusResolvesExactlyWhatItShould` asserts the unresolved specifier **count**, which is
what makes it fail in both directions — over-claiming lowers it, over-reporting raises it. The
count rather than a substring search, because the printed report truncates to the five most
frequent specifiers and a grep for any single one passes by matching `and 1 more`.

The JVM rows differ from every other language's in a way worth stating plainly, because it is a
current limitation and not a fixture decision. Signpost reads no `pom.xml`, `build.gradle`, or
`build.gradle.kts`, so **no JVM manifest states this repository's dependencies** and there is no
declared list for an import to match against. Two consequences follow. `org.junit.jupiter.api` is
a real dependency of a real test and lands in the unresolved count, which is the honest answer:
the alternative is inventing a Maven coordinate the repository never wrote. And the JVM cannot
express the other half of the instruction below — an import that resolves to a declared external
dependency — so its near-misses shadow the *runtime* instead, which is why there are two of them
where other languages have one. `javax` is the sharpest case in any ecosystem here: the namespace
was split between the platform and Java EE in 1999 and the division is historical rather than
structural, so `javax.crypto` is the JDK and `javax.servlet` is an artifact with its own
advisories, and only a list of the JDK's own `javax` packages can tell them apart.

Alongside them sit the stdlib imports — `node:fs`, python `os`, rust `std::fmt`, java
`java.util` and `javax.crypto`, kotlin `kotlin.math` — which are
the runtime: in no manifest, patched by nobody, so no node and no reported gap. Two of them are
addressed by subpath, `fs/promises` and `node:test/reporters`, which is
[issue #14](https://github.com/3rg0n/signpost/issues/14): the whole specifier was looked up in a
table holding `fs`, so the subpath missed and was reported as a dependency the repository failed
to resolve. `pathe/utils` and `winreg_helpers` above are the boundary on the other side of that
rule — those two rows are what stop the fix from being a prefix comparison.

Two of the stdlib imports are *platform-specific*, which is a boundary of its own:
`py/services/alpha/handler.py` imports the Windows-only `winreg` and
`py/services/beta/handler.py` the Unix-only `fcntl`, both guarded the way portable code spells
it. The Python stdlib list used to be hand-kept, and a hand-kept list assembled from code read
on one platform omits precisely what the other platform's code imports — so the most portable
code in a tree produced the most gaps, reported as dependencies nobody can install. Both
spellings sit here so the list cannot be completed for one platform and left short for the
other. Their absence checks match the whole name rather than a fragment, because a fragment
check for `winreg` is satisfied by the `winreg_helpers` page that must not exist.

## Two packages that cannot see each other

`py/services/alpha/handler.py` and `py/services/beta/handler.py` contain the byte-identical
line `from api.client import fetch`, and each resolves to a different file. Both packages
declare their own `pyproject.toml`, neither declares the other, and each holds its own
`api/client.py`.

An absolute Python import names a top-level package, so the only thing that makes it
resolvable is the directory holding that package's manifest being on the interpreter's path.
Resolution used to try exactly two roots — the repository root and `src` — which covers a
project and not a monorepo, and a monorepo is where the imports are: one measured repository
has 28 `pyproject.toml` files and writes that one specifier in 340 imports, every one reported
as a dependency nobody declares while nine sibling packages each held their own
`api/client.py`. This is the Python shape of
[issue #13](https://github.com/3rg0n/signpost/issues/13), which was worth 14% of a
repository's edges.

`TestCorpusPythonPackageRootsResolve` reads both directions off one graph, and the pair is why
the specifier is identical in both files:

| Boundary | Must hold | What the other direction would cost |
|---|---|---|
| positive | `py/services/alpha` imports `py/services/alpha/api`, and `beta` likewise | the shipped bug: a monorepo's internal edges reported as unresolved external names |
| negative | neither package imports the other's `api` | worse than the gap — a repo-wide root list resolves both to whichever `api/client.py` sorts first, an edge between packages that cannot see each other, reported with the confidence of something extracted |

Only the *scope* of the root can distinguish them, since the specifier cannot. A unit test in
`internal/assemble` holds the third boundary, which this tree cannot express: a
`requirements/base.txt` must not make `requirements/` a resolution root. It pins what to
install and declares no package, so a root there invents edges into a directory holding no
code.

## The bundle's own lifecycle

One stage here asserts nothing about extraction. `TestCorpusStalePageIsRemovedOrReported` is
[issue #10](https://github.com/3rg0n/signpost/issues/10): a page whose concept is gone used to
stay on disk forever, and strict `verify` exited 0 with it there. It belongs in the corpus
rather than only in `internal/okf` because the bug is about a bundle *in a repository over
time* — two builds, two verifies, a real git tree — and because the page it leaves behind is
not an empty stub. It carries plausible `edges` and a `resource:` naming a commit where the
code really did exist, which is what makes an orphan more expensive than a missing page.

Its negative boundary sits in the same test rather than in the table above, because the defect
is a *pair* of failures in opposite directions:

| Planted page | Must happen | What the other direction would cost |
|---|---|---|
| a copy of a real page, untouched | deleted, and named in the run's output | never deleting is the shipped bug: the bundle describes a module that is not there |
| the same copy plus one human sentence | kept, reported, sentence intact byte for byte | deleting unconditionally takes somebody's `## Notes` on the first rename |

A fix satisfying either row alone is a fix that ships one of the two bugs, so neither
assertion means anything without the other.

## A manifest with nothing to pin

Every ecosystem here declares dependencies, deliberately, because the resolution assertions
above need them to. That makes the tree unable to express one condition: a manifest whose
dependency table is *empty*. signpost's own practices page said *"The Go dependencies are
declared but not pinned by any lockfile in the tree, so two builds can resolve different
versions"* about a `go.mod` with an empty `require` block and no `go.sum` — nothing was declared,
so nothing resolves and two builds cannot differ. The lockfile check alone cannot tell the two
apart, since "no lockfile beside this manifest" is true of an ecosystem with nothing to pin and
of one that needs pinning.

`TestCorpusAManifestWithNothingToPinSaysSo` empties the crate's `[dependencies]` table and
removes `rust/Cargo.lock`, then reads all three outcomes off the one page:

| Ecosystem in that build | Must say | What the other direction would cost |
|---|---|---|
| Cargo — empty table, no lockfile | nothing to pin | the shipped bug: a reader told two builds can resolve different versions of nothing |
| Python — two dependencies, no lockfile | declared but not pinned | the more expensive bug — an unreproducible build reading as a clean one |
| Go — one dependency, `go.sum` present | pinned by a lockfile | a page that stopped distinguishing pinned from unpinned at all |

The lockfile goes with the emptied table because a manifest with nothing to pin and a lockfile
beside it reports as pinned, which is a different branch. And the stage counts empty manifests
rather than only looking for one: a branch that fell through would state it for Python too, and
every check above would still pass.

## What a flag is supposed to change

`ts/node_modules/@corpus-vendor/logger/` is committed on purpose, and it is the only directory
here that is not the corpus's own code. A committed `node_modules` is a real pattern —
`.gitignore` does not exclude it here, and plenty of repositories vendor a package deliberately
— which is exactly the condition signpost's own tree cannot express, since nothing in it is
vendored.

`TestCorpusVendoredCodeIsOffTheMapUntilAskedFor` is
[issue #11](https://github.com/3rg0n/signpost/issues/11): `-include-vendored` promised to
analyse vendored code, the walk honoured it and read the files, and every consumer downstream
filtered them back out on `File.Vendored` without consulting the option. The flag read a
vendored tree off disk and discarded it, and the bundle was identical either way. Every unit
test covering the walk stayed green, because the walk was the one part that worked.

| Run | Must happen | What the other direction would cost |
|---|---|---|
| default | no page names the vendored module, and no page cites `vendored-only-tinycolor` | describing somebody else's repository as this one |
| `-include-vendored` | a page for the module **and** a page citing `vendored-only-tinycolor` | the shipped bug: a flag that reads files nothing looks at |
| `-include-vendored` | every page from the default run still present | the flag silently *replacing* the map rather than extending it |

The two positives are separate rows because they fail independently: extraction is gated by
`Result.Sources()` and the manifest readers by `manifest.Registry.Run`, so a fix to the first
alone analyses the vendored source and still discards the `package.json` beside it — leaving a
module whose own declaration signpost had in hand and threw away. `vendored-only-tinycolor` is
declared in that vendored manifest and nowhere else in this tree, so its name can only reach a
bundle through the reader that was dropping it.

## What must not leave the machine

This tree is the only one that can violate the no-content rule in
[ADR 0014](../../docs/adr/0014-adopt-the-otel-sdk-and-write-the-exporter.md). Telemetry's own
unit tests start spans by hand against nothing, so "no repository content reaches a span" is
asserted there against a tree with no content in it. Here there is content, and it is the
content that hurts: `ts/app/tools/[slug]/page.tsx`, `py/greeter/data,notes.py`,
`POSTGRES_PASSWORD` named in `compose.yaml`, module and package names, and every declared
dependency.

`TestCorpusTelemetryCarriesNoRepositoryContent` runs a real build against an in-process
collector and reads both boundaries off one payload:

| Boundary | Must hold | What the other direction would cost |
|---|---|---|
| positive | all six span names arrive — `analyse`, `discover`, `extract`, `manifests`, `history`, `assemble` | every check below satisfied by an exporter that sends an empty batch, which is indistinguishable from a tool that sends nothing |
| negative | no path, filename, package name, dependency, or secret name from this tree appears in the bytes | the failure the whole design exists to prevent, and it is unrecoverable — the bytes are on somebody else's collector |
| structural | every span attribute is `signpost.`-prefixed and carries `intValue` | a string setter added later, which is how a path gets in; the current API has no method that can produce one |

The positive row is not decoration. Both other rows are assertions about *absence*, and a
subsystem that exports nothing satisfies an absence check perfectly.

`TestCorpusTelemetryIsOffAndFailsOpen` covers the three configurations, each against a bundle
built with telemetry off:

| Run | Must happen | What the other direction would cost |
|---|---|---|
| endpoint set, `SIGNPOST_ENABLE_TELEMETRY` unset | nothing is posted, nothing on stderr | a repository's structure sent to whatever collector a CI runner happens to name — the case ADR 0009 forbids by default |
| enabled, collector rejects every batch | exit 0, one line on stderr | telemetry becoming a reason a build fails |
| enabled, nothing listening | exit 0, the failure reported | silence indistinguishable from a working exporter |
| all three | bundle byte-identical to the telemetry-off baseline | instrumentation changing the output it exists to measure |

The CI job repeats both against a collector outside the process, because a span encoded wrongly
— a timestamp as a JSON number rather than a string, an ID as base64 rather than hex — round-trips
correctly through signpost's own reader and is rejected by every real collector.

## A language signpost cannot name at all

`web/` holds two `.astro` files and a `README.md`, and none of the three is here to be read.
They are here to be *counted*.

`ClassOther` is what discovery assigns to a file whose kind it cannot determine. Every other
class routes to an extractor or a manifest reader, and the two that can still come back
empty-handed report it — `extract.RunResult` and `manifest.RunResult` each carry an `Unhandled`
map the coverage report prints. `ClassOther` had no counterpart: it was written in one place and
read in none, so a file landing there left the pipeline with nothing recording that it existed.

Found on a repository whose entire landing page was two `.astro` files. The coverage report
named `.sh` and `.sql` — both in `sourceExts` as `LangOther`, so extraction counts them — and
said nothing about the pages, while the bundle described that workspace as a one-file JavaScript
module built from `astro.config.mjs`. Which extension is *in* `sourceExts` decided whether the
gap was visible at all, and every extractor still to be written adds one.

`TestCorpusCountsWhatItCannotClassify` reads both halves off one report:

| Boundary | Must hold | What the other direction would cost |
|---|---|---|
| positive | the two `.astro` files, the `.svg`, and `LICENSE` are counted and named | the shipped bug: a repository's only frontend absent from a report that claims to say what went unread |
| negative | `web/README.md` is not on the line, nor any classified file — no source, manifest, doc, or binary | a line that fires on every repository, which teaches people to skip the one place coverage gaps are admitted |
| structural | it stays a *separate* line from `no extractor for` | folding them loses the difference between "cannot read this language" and "cannot tell what this file is" — a missing extractor versus a missing classification |

Two `.astro` files rather than one, because the count is the assertion and one file cannot
distinguish counting files from counting extensions. `web/README.md` sits beside them so an
implementation that counted by directory fails, and binaries are excluded on purpose: a `.png`
was classified correctly and has nothing in it to read, so counting it would bury the extensions
that are gaps under the ones that never could be.

## An import that lands exactly nowhere

Three things can happen to an import specifier, and until recently only two of them were
counted. It resolves to a page; or signpost cannot place the name at all, which is *unresolved*;
or signpost places it exactly — inside a Go module, under a `paths` alias, down a relative path
— and finds no node there. That third state is *unlinked*: the edge is missing and the map is
thinner than the code.

It was the quietest failure in the pipeline, because both of the resolver's decisions about such
an import are correct. The specifier *is* first-party, and inventing an external dependency for
it would report a package nobody publishes — the fabricating failure the resolver must never
produce. `addImportEdges` counted a specifier only when it was **not** internal, so the internal
branch was empty, and a module whose every import landed there read as importing nothing.

Two deliberate cases sit here, in different languages because the branch is per-language:

| Specifier | Where it lands | The shape it stands for |
|---|---|---|
| `example.com/corpus/greeter/internal/generated` | inside the module `go/go.mod` declares, at a directory holding a README and no Go file | generated code, a build-tagged package, a directory whose files all exceeded the size cap |
| `@corpus/assets/logo.svg` | a `paths` pattern matched whole, mapping onto `ts/assets/` | an asset alias — the mapping is real and its target is not extracted source |

`TestCorpusFirstPartyImportsThatReachNoPageAreCounted` reads all three boundaries off one report:

| Boundary | Must hold | What the other direction would cost |
|---|---|---|
| positive | both specifiers counted and named, and the **count** is 2 | the shipped bug, and the count is the only assertion in the harness that can notice a *new* missing edge — every other one names the edges it expects |
| negative | `example.com/corpus/greeter/greeter`, `@corpus/entry` and `@corpus/core` are absent | a branch counting every first-party import fires on every healthy repository, which teaches people to skip the line |
| structural | neither specifier appears among the unresolved, and no stdlib name appears in either | unresolved says *go and declare this*, which is the wrong instruction for a path the module already owns; one merged map cannot say which a reader is looking at |

`example.com/corpus/greeter/greeter` is the negative that matters. It sits in the same import
block as the unlinked one, in the same module, and differs only in there being Go files at the
end of it. It is also there because it was itself broken: the fixture spelled it as the bare
module path, which names `go/` — a directory holding `go.mod` and no source — so `go/cmd/hello`'s
only internal import drew no edge for as long as the corpus has existed. Nothing caught it,
because no assertion here named a Go internal edge and the coverage report had no line for it.
Adding the count found it on the first run, along with `use super::*` resolving out of the crate.

## Four directories called `src`

Ordinary, and the reason this tree can show a defect signpost's own repository cannot. `rust/src`,
`ts/src`, `ts/packages/api/src` and `ts/packages/core/src` all slug to `src`; `py/services/alpha/api`
and `py/services/beta/api` both slug to `api`; `go/greeter` and `py/greeter` both slug to `greeter`.
Six colliding pages in a fifty-file repository. signpost's own bundle has none, so nothing about
page naming under collision is observable by running signpost on signpost.

A page's name **is** its node ID and every other page links to it by that name ([ADR 0003](../../docs/adr/0003-directory-granularity-for-module-nodes.md)),
so a page that gets renamed is that file *plus every page citing it*, rewritten in the diff of a
commit that need not have touched the directory. Nothing in the graph is wrong afterwards. That is
what made it worth a test: `verify` passes, every other assertion passes, and the only symptom is a
reviewer facing forty changed pages for a one-directory change with no way to tell which mean
something.

The suffix used to be a counter — `src`, `src-2`, `src-3`, `src-4` in path order — so a page's name
depended on how many same-named directories sorted ahead of it. Adding one renumbered every later
member and deleting one shifted them all down. `ZT-duo-cc-plugins`, a real repository, carries a
`tests-2.md` whose name is decided that way. The suffix is now derived from the entry's own key, so
it depends on that entry and on whether its short name is shared, and on nothing else.

Two tests, at two levels, because they catch different things:

| Test | What it does | What only it can catch |
|---|---|---|
| `TestCorpusPageNamesSurviveAnUnrelatedEdit` | builds, adds `go/src`, rebuilds in process, compares names by frontmatter `title` | runs on all three platforms, so a filesystem that folds case or normalises differently shows up here |
| **ci.yml** — `A committed bundle's page names survive an unrelated edit` | commits the bundle first, then adds `go/src` and asserts `git status` reports no deletion | the reviewer-facing cost. A rename is a delete plus an add of a path never seen before; only a committed bundle can show it |

The edit both use is a Go package in `go/src` — a new member of the largest collision group, sorting
ahead of the Rust and TypeScript members. Under a counter that is the worst position available: it
takes a number already in use and pushes every later one along by one.

| Boundary | Must hold | What the other direction would cost |
|---|---|---|
| positive | no pre-existing page's name changes, and nothing is deleted | the shipped bug: a rename storm in a commit that added one package |
| negative | `go/src` gets a page of its own, distinct from every other `src` | an assertion satisfied by a build that did nothing, or by two directories sharing one page |
| structural | at least two `src-*` pages exist before the edit | without a collision group there is nothing to renumber, so the test would pass on a corpus that cannot show the defect |

The one residual is recorded rather than claimed away, in
`TestDeletingTheBareNameHolderMovesOnlyItsOwnGroup`: a name is suffixed because more than one thing
wants it, so when a collision group shrinks to a single member that member stops needing its suffix
and moves to the bare name. The alternative is suffixing every page in every bundle whether it
collides or not, which trades the readability of all of them for stability in a case that requires a
directory to be deleted.

## A repository with no git, and a bundle nothing points at

Two stages that hold at the edges of adoption rather than at the edges of extraction.

`TestCorpusBuildsWithNoGitAtAll` is the tarball case: somebody sends you a tree with no `.git`.
Git and a forge are the recommended setup and, where they exist, they own what is tracked and
which commit the bundle describes — but they are not a requirement for producing one. The stage
builds the corpus twice, once with history and once with the `.git` directory deleted, and asserts
the second build is the first minus its provenance and nothing else.

| Boundary | Must hold | What the other direction would cost |
|---|---|---|
| positive | every page the git build wrote is written again, under the same name | pages whose identity leaked out of the log rather than the tree ([ADR 0015](../../docs/adr/0015-a-colliding-page-name-is-suffixed-from-its-own-key.md)); a tarball would get a differently-named bundle |
| negative | no page's frontmatter carries `resource:` or `generated:` | a page stamped with a commit nobody can check, which `verify` would then compare against nothing |
| reporting | stderr says `history not read: not a git repository`, exit 0 | silence, which reads as *there was no history to read* rather than *this build could not read it* |
| downstream | `verify` exits 0 and names its staleness check as `skipped` | a repository that cannot pass its own gate for a reason it never states |

The guard is that `.git` is actually gone before the second build — a `RemoveAll` that silently
failed would leave the stage re-asserting the first build's result and passing.

`TestCorpusSaysNothingPointsAtTheBundle` covers the other end: a bundle no instructions name is
the one failure a green build cannot show. The corpus is the right tree for it because it *has* an
`AGENTS.md` and a `README.md`, and neither points at the map — so a build here must ask for a
pointer, and must stop asking once the stub from `build -suggest-agents-md` is appended.

That stage is also what found a defect in the check itself. The first version matched
`.signpost/`, and this file says *`build` writes `.signpost/`, which has no business being
committed here* — a sentence about the harness, not somewhere for an agent to start. The build
read it as adoption and went quiet on a repository that had adopted nothing. The check now matches
the bundle directory joined to the index page, and the stage asserts **both halves** of what makes
it a test: this README must still mention `.signpost/` somewhere, and must still never name that
joined path. Losing either turns the stage green while covering nothing — the first because
prose-that-is-not-a-pointer would have left the tree, the second because there would be no
unpointed case left to reach.

Which is why the path is described here rather than written out. **This file cannot spell it**: a
README in the corpus that names the index page makes the corpus a pointed-at repository, the build
stops asking for a pointer, and the stage fails on its own documentation. That happened while this
section was being written, and the guard is what caught it.

## Infrastructure that must not publish what it holds

`infra/` is Terraform, and it is here because it puts both boundaries in the same file. A
configuration is the most valuable structure in a repository to read — what runs, where state
lives, which of the repository's own directories the infrastructure is composed from — and it is
also the file most likely to hold a live credential, sitting one line away from the name that
credential is known by. `TestCorpusTerraformStatesWhatRunsAndNeverAValue` reads both.

Five values sit in these fixtures, each beside a name that does reach the bundle. None of them
may appear anywhere in it, and *beside* is the load-bearing word: a reader that dropped the
whole block would satisfy the negatives and fail the positives, which is what makes the pair a
test rather than two wishes.

| Value | Where | The name that does arrive |
|---|---|---|
| `corpus-state-do-not-publish` | `backend "s3"`'s bucket | `backend.s3` — which backend holds state, not the bucket holding it |
| `hunter2-do-not-publish` | an `aws_secretsmanager_secret_version`'s `secret_string` | `db` — the version resource's own name |
| `s3cr3t-material-that-must-not-be-read` | `variable "db_password"`'s `default` | `db_password`, and `TF_VAR_db_password` |
| `tfvars-value-must-never-be-read` | `staging.tfvars` | `db_password` |
| `tfvars-token-must-never-be-read` | `staging.tfvars` | `api_token` |

Two of the five are held by this stage and three by a `renderFacts` sweep in
`internal/manifest`, which is worth stating rather than leaving to be discovered. The bucket and
the `secret_string` ride on references that reach a page, so the bundle assertion is the only
thing standing under them. The other three ride on references the reader deliberately attributes
to nothing, and an unattributed reference reaches no page, so today they cannot arrive by that
road however the reader mishandles them — the assertion that catches those is one layer up,
before attribution can hide the answer. They stay in both places because attribution is a
decision and not a law.

The structural boundary is the other half, and here the negatives outnumber the positives for a
reason: a real configuration declares IAM attachments and route table associations by the
hundred, so a reader that admitted every resource would report forty pages where one thing runs
and bury the pages that mattered among them.

| Block | Page? | Why |
|---|---|---|
| `aws_ecs_service.worker` | yes | it runs something, and its `image` is the same claim a compose `image:` makes |
| `aws_sqs_queue.events`, `aws_lambda_function.consumer` | yes | in the local module, which is where the parser's brace cases live |
| `aws_secretsmanager_secret.db`, `random_password.session` | yes | a secret store *is* the named credential, so "where the credentials live" is a thing a reader looks up |
| `backend "s3"` | yes, as `terraform-state` | state is the most sensitive artifact the repository has |
| `aws_ecs_cluster.corpus` | no | capacity: `_cluster` is a workload suffix and a cluster runs nothing by itself, so this is the row that says the suffix rule is bounded by an exceptions list |
| four wiring resources | no | a policy attachment, a firewall rule, a bucket policy, a topic subscription — none of them runs anything |
| `data "aws_lambda_function" "existing"` | no | compute-shaped, and it declares nothing this configuration owns |
| `module "queue"` (`./modules/queue`) | no | a directory of this repository, and an external dependency page for it would report first-party infrastructure as something pulled in from outside |
| `module "vpc"` (`terraform-aws-modules/vpc/aws`) | yes | genuinely external, with a registry to publish an advisory against |

The last two rows are one boundary written twice. `terraform-aws-modules/vpc/aws` and
`./modules/queue` are the same slash-separated shape, so a reader guessing from the shape rather
than from Terraform's own rule — local only if it starts with `./` or `../` — gets exactly one
of them wrong whichever way it guesses.

Two things this tree cannot express, both asserted in unit tests instead:

- The composition **edge** that replaces the suppressed `queue` page. It needs a module node at
  each end, and Terraform is read as a manifest so it contributes none; `infra/` and
  `infra/modules/queue/` hold nothing but `.tf`, so no edge is drawn and that is correct.
  `TestLocalDeclarationIsAnEdgeAndNotAReferencePage` in `internal/assemble` pairs both halves
  against a tree that has the source. Planting Go files in `infra/` to stand the edge up here
  would be testing the fixture.
- The attribution of module-level references. `db_password`, `api_token`, and the sensitive
  output `db_password_arn` are inputs to `infra/main.tf` as a whole — which resource reads them
  is stated in an expression this reader does not evaluate. Handed to every service in the file,
  which is what an empty service means to a *compose* file, the ECS task and the S3 backend each
  claimed to read three credentials neither names. The stage asserts those three pages name none
  of them and carry no `reads-secrets` tag; `internal/manifest` holds the field-level rule.

`infra/modules/queue/main.tf` also carries the four places a naive brace count goes wrong — a
brace inside an interpolation inside a string, a brace in a plain string, a heredoc, and both
comment forms. None of them is a diagnostic: a miscount silently reparents the rest of the file,
so the resources below stop being top-level and their pages vanish with no error anywhere. The
two `yes` rows for that file are the observable.

## One package name, two directories

`jvm/` is Java and Kotlin in the same tree, and it is the only language here whose resolution map
is built from *extracted facts* rather than from a manifest. With no `pom.xml` or `build.gradle`
reader, an import resolves against the `package` declarations found in the source — which works,
and which has a consequence no other ecosystem has: **the standard JVM layout declares each
package twice.** Maven and Gradle put `com.example.api` in `src/main/java/com/example/api` *and*
in `src/test/java/com/example/api`, so one import names two candidate directories and only one of
them is what another module means by the name.

`TestCorpusResolvesJVMPackagesToTheRightDirectory` holds the three defects that fell out of
building these fixtures. Each row has a negative because each has a wrong answer that draws an
edge rather than omitting one, and an edge nobody flagged is worse than a gap:

| Boundary | Must hold | What the other direction would cost |
|---|---|---|
| the source set | Kotlin's `import com.example.api.Service` reaches `src/main/.../api` | an edge into the tests instead of into the code, drawn with no indication that a choice between two directories was made at all |
| the source set, negative | it does **not** reach `src/integrationTest/.../api` | satisfied by any resolver that draws both edges, which is the same map with the wrong one still in it |
| the subpackage | `com.example.store.internal` reaches its own directory | a matcher taking the first declared name that prefixes the import lands on `com.example.store`, one directory up |
| the subpackage, negative | `app` does **not** import `store` | the whole point of the row: with both imported, the wrong answer and the right one draw the same pair of edges and nothing distinguishes them |
| the test's subject | `tested_by` runs from `src/main/.../api` | reading a JVM test's imports finds every collaborator and misses the one thing under test |
| the test's subject, negative | it does **not** run from `store` | the shipped behaviour, and it is confidently wrong: the store was reported as tested by a test that never touches it |

The extra source set is called `integrationTest` rather than `test` on purpose, and that name is
the whole reason the first row can fail. Directory order used to be the tiebreaker, which looks
sound because `src/main` sorts before `src/test` — but the source set holding tests is not always
called that. Gradle's convention for the extra one is `integrationTest` and Android's is
`androidTest`, and **both sort ahead of `main`**. So a repository with either resolved every
import of a package to the copy under test. With a source set named `test`, every assertion above
passes on the broken ordering too.

`ServiceIT.java` is also what makes the basename half of test detection load-bearing here: no path
segment equals `test`, so the directory rule does not fire and the `IT` suffix is the only thing
marking the file. And it is where the third defect comes from. A JVM test declares the package it
tests and imports nothing from it — same-package access needs no import — so its subject is
precisely the one name its import list does not contain. `addTestEdges` reads the declaration
instead of the imports for these two languages, and instead of rather than alongside them: reading
both reports `store` as tested by a test of `api`.

## Running it

The harness copies this tree to a temporary directory, `git init`s it, commits, and runs
`build` then `verify`. It is copied rather than used in place for two reasons: signpost
reads git history, and a directory inside this repository's history would describe
signpost's commits rather than the corpus's; and `build` writes `.signpost/`, which has
no business being committed here.

```
go test ./cmd/signpost -run TestCorpus -v
```

## Adding a language

Add the directory, its manifest, and at least one file that imports something external
and something internal. Then add the expectations to `corpus_test.go` — the point of the
harness is that it asserts on *named* facts, so a new language with no assertions is a
directory that gets walked and proves nothing.

Add its negative boundary in the same change: one import that is a near-miss of something the
manifest declares, in whatever spelling that ecosystem's name normalization is loosest about,
and one standard-library import. Then raise the expected count in
`TestCorpusResolvesExactlyWhatItShould` and in the `Resolution reports exactly the gaps it
should` step in `ci.yml`, and confirm the new specifier is named in the table above. Without
it the language is covered only by positives, which cannot distinguish a resolver that reads
the manifest from one that says yes to everything.

Three counts are asserted here and a new language moves at least one, deliberately — a count is
the only assertion that fails in *both* directions, so each is a place a change has to be
declared rather than absorbed. Adding an extractor for `.astro` lowers the unclassified count by
two and raises the extracted one; expect `TestCorpusCountsWhatItCannotClassify` and the
`Unclassified files are counted, not swallowed` step in `ci.yml` to fail, and move the two
`web/` files out of the table above into the language sections when they do. The unlinked count
moves if the new language's fixtures import anything inside the repository that holds no node,
which the *first-party* half of the instruction above is otherwise silent about: an import that
signpost places and cannot link is a different failure from one it cannot place, and the language
is covered for only one of them until both counts are stated.

Nothing here is compiled or executed. These are inputs to a parser, so they need to be
syntactically real and do not need to be correct programs.
