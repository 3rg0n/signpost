# signpost — design

**Give models signposting for repos.**

signpost compiles a repository into a small, durable, human-editable knowledge
bundle that an agent reads *before* it starts work. It is a compilation step, not
a retrieval system: no vector database, no embeddings, no server. One binary, one
command, output is markdown.

- **Status:** design, pre-implementation
- **Language:** Go, single static binary
- **Output format:** [Open Knowledge Format v0.2](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
- **Shape:** one repo. The Go generator runs in CI; the viewer is hand-written
  HTML/CSS/JS in `site/`, published to GitHub Pages by a workflow off the merge
  path. See §7.

---

## 1. The problem

An agent opening an unfamiliar repo re-derives the same understanding every
session: which module owns what, what the entrypoints are, which files move
together, what the docs claim versus what the code does. That rediscovery is paid
for on every session, it is thrown away at the end of every session, and it is
inconsistent between runs.

The fix is not better retrieval. It is to do the derivation **once, in CI, at a
known commit**, write it down in a format the agent can navigate, and let humans
correct it in place so the corrections survive.

Two properties follow from that framing and drive every decision below:

1. **The bundle must be useful without signpost installed.** It is markdown in
   the repo. An agent, a person, or a static site generator reads it directly.
   signpost is the thing that *maintains* the bundle, not the thing that *serves*
   it.
2. **Human edits must compound.** A generator that clobbers hand-written
   corrections on every run trains people to ignore its output. Preservation of
   human review is a first-class requirement, not a later feature.

### What belongs here, and what does not

Scope is decided by one question about the *thing being proposed*, not by a list of
tools that already do something adjacent
([ADR 0031](adr/0031-scope-is-a-lifecycle-test-not-a-list-of-non-goals.md)):

> Is it durable, evidence-backed repository knowledge with the bundle's lifecycle —
> compiled from the tree, committed, human-correctable, loudly stale when the
> evidence moves? Then it belongs here, regardless of which tool first thought to
> look for it. Is it a ranking, an opinion, an observation that cannot be
> reproduced from the tree at a commit, or knowledge that exists only while a
> service is running? Then it does not.

**In:** the structure index — `Exports`, `KindSymbol`, the files behind each node.
Repository-practice findings (§9.1). Cycles, bridges, doc/code drift, manifest
conflicts (§7.1). The workflow and data-artifact overlay. An MCP query surface,
which reads a committed artifact and is therefore optional by construction —
property 1 above is what makes that a query rather than a service.

**Out:** full-text search, because grep is stateless, already installed, and better
at it — signpost indexes structure, and the moment it indexed text it would be
maintaining a stale copy of something the tree already answers exactly. A 1–5
maturity score, and root-cause ranking, on the same ground: both are opinions with
no durable truth underneath (§9.1). Runtime traces, which fail the *first* clause
rather than needing §8.1's determinism argument — an observation of one execution
cannot be recompiled from the tree at a commit, so nothing downstream can check it.
Chat, which is a conversation rather than an artifact.

Two of these were written as non-goals at v0.1 and are now false as stated: this
tool does index code, and it does report readiness signals — §9.1 records that
routing those facts elsewhere "was wrong." A list of exclusions is a thing nobody
re-reads, so it went stale in place while the tool it described moved. A test does
not: it has to be applied to each new proposal, and applying it is what surfaces
the disagreement.

§6.2 is untouched by this. Which files signpost may overwrite is a trust boundary,
not a scope one, and it is already correct.

---

## 2. Supply-chain posture

> Recorded as ADR [0002](adr/0002-patchable-dependencies-not-zero-dependencies.md), which
> is the authoritative statement of this decision and of the alternatives rejected.

This is the reason signpost gets built rather than procured, so it is a design
constraint and not a footnote. The distinction that matters is **direct
dependencies we can patch versus transitive dependencies we inherit through
someone else's release cadence.**

Third-party libraries are allowed. We are on the hook to remediate their CVEs, and
Dependabot plus Renovate are enabled from the first commit to make that tractable:
a CVE in a library we depend on directly is a version bump we control and can ship
the same day.

What is *not* tractable is depending on a **tool** rather than a library. Adopting
a comparable upstream tool means inheriting its entire dependency tree — a C core
plus one grammar per language for parsing, a graph library, a clustering
implementation that pulls a JIT compiler, a JS graph library for the HTML view —
with no ability to patch any of it directly. When a CVE lands in that tree, the
options are to carry a local patch indefinitely or open a PR upstream and hope it
merges. Neither is a remediation path we can commit to on an internal SLA. That is
the actual reason to build: not dependency count, but **control of the remediation
path.**

So the rule is not "no dependencies." It is:

> Every dependency must be one we can bump ourselves, and the count must stay
> small enough that bumping is routine.

Which yields:

| Concern | Decision |
|---|---|
| Language | Go. Static binary, cross-compiles to the four platforms we care about. |
| Dependency policy | Direct deps only, few, each justified in an ADR. Dependabot + Renovate enabled day one. CI fails on a new direct dep without an ADR. |
| Go parsing | `go/parser` + `go/ast` — full-precision AST, in the stdlib, free. |
| Other-language parsing | Hand-written extractors (§4.2), nine languages at F1 1.000. A tree-sitter binding is the fallback for one language whose scored fixtures cannot reach target by hand — a library decision, not a tool decision, and ours to bump (ADR [0022](adr/0022-extractors-are-hand-written-and-tree-sitter-has-a-threshold.md)). |
| Graph algorithms | Hand-written (§4.4). Roughly 600 lines, all textbook — genuinely cheaper than a dependency. |
| Clustering | Louvain, hand-written. ~200 lines versus a JIT-compiler toolchain. Label propagation was tried first and rejected on measured behaviour (§4.4, ADR [0019](adr/0019-louvain-over-label-propagation.md)). |
| YAML | Hand-written tolerant reader and hand-written emitter, both ours. Helm templates are not YAML and a conforming parser rejects them outright, so a library would not have covered the files that matter (ADR [0001](adr/0001-hand-written-tolerant-yaml-reader.md)). |
| Model access | Two backends behind one interface (§5). Both first-party over stdlib `net` and `net/http`. |
| Telemetry | `go.opentelemetry.io/otel`, `otel/trace`, and `otel/sdk` — the only three direct dependencies. `otel/trace` holds the `Tracer` and `Span` interfaces `internal/telemetry` is written against, so an instrumenting package cannot avoid naming it. The OTLP/JSON exporter is hand-written, because upstream's *HTTP* exporter links the whole gRPC stack — 65 gRPC packages for a transport that uses none (ADR [0014](adr/0014-adopt-the-otel-sdk-and-write-the-exporter.md)). |
| SCIP enrichment | `google.golang.org/protobuf` — Google-published, heavily audited, already in codeatlas. **Not behind a build tag**: `go.mod` carries a tagged requirement anyway, so `govulncheck` reports clean on a tree with a known CVE unless someone remembers the matching `-tags`, which is the opposite of the exposure this table exists to bound (measured in ADR [0014](adr/0014-adopt-the-otel-sdk-and-write-the-exporter.md)). |
| Generator output | Markdown and JSON only. Nothing executable. |
| Viewer | Hand-written HTML, CSS, and JavaScript in `site/`. Zero JS dependencies, so there is no second dependency tree to govern (§7, ADR [0008](adr/0008-the-viewer-lives-in-this-repository.md)). |

Nothing in the generator path executes or renders untrusted input. The output is
markdown; the worst a hostile repo can do is put ugly strings in a `.md` file.
The viewer does render repo strings into a page, which is a real vulnerability
class — contained by the rules in §7.2 and by a deploy workflow that cannot break
a merge.

---

## 3. What it produces

An OKF-conformant bundle at `.signpost/`, committed to the repo — recorded as ADR
[0005](adr/0005-commit-the-bundle-to-the-repository.md), which is where the "why committed"
argument and its consequences live. One module page per *directory* holding source, per ADR
[0003](adr/0003-directory-granularity-for-module-nodes.md); the page path is also the graph
node's ID.

```
.signpost/
  index.md              # bundle root; okf_version: "0.2"; progressive-disclosure entry point
  log.md                # date-grouped change history (OKF §9)
  manifest.json         # machine-readable run record: sha, backend, coverage, skips
  modules/*.md          # one page per module / package / significant directory
  services/*.md         # deployable units: entrypoints, ports, config, deploy surface
  interfaces/*.md       # API surfaces: OpenAPI, proto, GraphQL schema, CLI commands
  data/*.md             # schemas, migrations, persistent stores
  references/*.md       # external deps, and mirrors of the docs a page derives from
  cache/                # content-hash keyed semantic output; gitignored
```

`index.md` is the file an agent is pointed at. It is a grouped, described listing
per OKF §8 — headings by concern, one line per concept with a description — so the
model can pick the three pages it needs instead of reading the bundle. It also
carries the structural findings §7.1 names, above the listing, because an agent
that reads the index and stops has to have them
([ADR 0030](adr/0030-a-finding-states-its-own-absence.md)).

**A page name is a contract, so it is derived from the thing it names.** The filename is the node
ID and every other page links to it by that ID, and the bundle is committed, so renaming a page
rewrites that file plus every page citing it. Names collide — slugging is lossy by design, and a
polyglot repository has several directories called `src` — and a colliding name is suffixed with a
short hash of the entry's own key, so `rust/src` gets `src-1slg0rn.md`. Not a counter — a counter
made a page's name
depend on how many same-named directories sorted ahead of it, so adding one renumbered the rest and
deleting one shifted them down, each time in a commit that had not touched those directories. Which
member gets the bare `src` is decided by counting the names before any is assigned, because deciding
on first sight hands the readable name to whoever the walk reaches first and takes it off the
incumbent. ADR [0015](adr/0015-a-colliding-page-name-is-suffixed-from-its-own-key.md) has the
measurements, the two rejected alternatives, and the one residual case.

### 3.1 Page shape

```markdown
---
type: Module
title: internal/auth
description: JWT validation and PAT issuance; the only writer of the token table.
resource: git://cisco-sbg/example@8f2a1c9/internal/auth
tags: [security-boundary, go]
status: stable
generated: { by: signpost/0.1.0+gemma4-12b, at: 2026-07-29T13:40:11Z }
verified:
  - { by: human:ecopelan, at: 2026-07-29 }
stale_after: 2026-10-27
sources:
  - id: adr-0007
    resource: /references/adr-0007.md
    title: "ADR 0007: tokens are opaque"
    last_modified: 2026-03-14
edges:
  - { kind: imports,    to: /modules/storage.md }
  - { kind: implements, to: /interfaces/things-jwt.md }
  - { kind: tested_by,  to: /modules/auth-test.md }
  - { kind: co_changes, to: /modules/api-gateway.md, weight: 14 }
---

# internal/auth

<!-- signpost:managed:summary -->
Validates inbound `X-Things-JWT` against the JWKS endpoint and issues `thingspat_`
tokens for CLI callers. Every write to the token table passes through
`store.PutToken` in [storage](/modules/storage.md).[^adr-0007]
<!-- /signpost:managed:summary -->

## Notes

Rate limiting looks like it belongs here but lives in the gateway for historical
reasons — see the 2025 incident review before moving it.

[^adr-0007]: ADR 0007 fixes tokens as opaque; do not add claims to the PAT.
```

Three things to note in that example:

- **`generated.by` encodes the backend and model**, so a bundle built partly on a
  local 12B and partly on a frontier model through Bedrock is auditable
  page-by-page. OKF derives trust tiers from `generated`/`verified` directly:
  absent `verified` is *unverified*, a non-`human:` actor is *machine-confirmed*,
  any `human:<id>` is *human-reviewed*. We get graded trust without inventing it.
- **`edges` is a frontmatter extension.** OKF links are deliberately untyped
  ("specific kinds are conveyed by surrounding prose"), which is too weak for code.
  OKF §11 requires consumers to tolerate unknown keys, so a typed `edges` list
  keeps the bundle conformant while carrying a real graph. We *also* emit prose
  links so a generic OKF reader still traverses the bundle.
- **An extension is a new key, never a new value on a spec key.** §4.1 and §11
  protect unknown *keys*, which is what makes `edges` and `attributes` safe. They
  say nothing about an unrecognised value on a key the spec enumerates, so
  signpost's own findings go on `signpost_status` and OKF's `status:` is left for
  a human to write — [ADR 0021](adr/0021-track-the-published-spec-and-never-overload-its-keys.md).
  Optional spec fields we decline (`stale_after`, `usage_window`) are declined on
  their merits; not adopting one is conformant, inventing one is not.
- **The `## Notes` section is outside the managed markers** and is never touched
  by signpost. That is the compounding mechanism (§6.4).

---

## 4. Pipeline

Six stages. Stages 0–3 and 5–6 require no model at all; the bundle is complete and
useful with the semantic pass switched off.

### 4.0 Discover

Walk the tree honoring `.gitignore`. Classify by extension and by manifest
filename. Skip binaries. Size caps mirror codeatlas: full ingest at ≤2 MB and
≤50k lines; oversized files get metadata plus head/tail plus an outline.

Three kinds of path are recorded but not analysed, and the distinction between them
is not tidiness — each would put a claim on a committed page that the repository
does not support:

- **Vendored** (`vendor/`, `node_modules/`, `target/`) — somebody else's code,
  unchangeable by this team. Analysing it swamps the graph with nodes nobody can act
  on. Recovered with `-include-vendored`.

  Both flags are answered in one place, `Result.Analyses`, and the walk's option is carried
  on the result so the consumers can reach it. Issue #11 was what happens otherwise: the
  walk honoured `-include-vendored` and read the files, and six consumers each decided the
  same question independently by testing `File.Vendored` with no reference to the option, so
  the flag that exists to overrule the default overruled nothing. A file stays marked
  vendored either way — the flag changes whether it is analysed, not what it is, so the skip
  report and the file's own metadata stay truthful.
- **Test fixtures** (`testdata/`, `fixtures/`, `__fixtures__/`) — sample projects
  that exist for tests to run against. Recovered with `-include-fixtures`.
- **Binaries** — no content to read.

The fixture rule was found the hard way, by signpost biting itself. Adding
`testdata/corpus` (§4.2) put `testdata/corpus/ts/app/(marketing)` in signpost's own
index as a module and react, httpx and serde in it as dependencies, and it reached
`practices.md`, which cited `testdata/corpus/py/pyproject.toml` as evidence about how
*signpost* pins its dependencies. That is not cosmetic noise. A bundle is committed
and read by people who did not build it (§4.6), so a page naming a dependency absent
from the `go.mod` is exactly the false grounding this design exists to prevent.

A fixture is deliberately neither of its two neighbours. It is not vendored — it is
this repository's own reviewed, hand-maintained code, and calling it third-party
would be a wrong explanation of a right decision in the skip list users see. It is
not a test either: a test file *exercises* the repository's surface and earns a
`tested_by` edge pointing at it, whereas a fixture is the *subject* of a test, and an
edge from a real module to a sample project would be a false claim.

`testdata` is the strongest of the three names because it is toolchain-defined rather
than conventional — the go command ignores it outright, so a Go repository cannot use
it for shipping code. `fixtures` and `__fixtures__` are conventions, included because
the cost of being wrong is asymmetric: a missed fixture puts a phantom module on a
committed page, while a misclassified real directory loses nodes a reader can see are
missing and recover with a flag.

Skipping is not the same as analysing a fixture as its own repository, and the corpus
harness does the second: it copies `testdata/corpus` to a root of its own, where those
files arrive with no `testdata` segment and get correct module paths. Only that
ordering gives the sample projects the paths a real repository would have.

### 4.1 Extract — deterministic, no model

**Go** gets `go/parser` and `go/ast` from the stdlib: packages, imports, exported
symbols, interface implementations, `main` functions, `init` side effects. Full
precision, zero dependencies, and it is our primary language.

**TypeScript/JavaScript, Python, Rust, Java, Kotlin, C, C++, Objective-C, Ruby, PHP, C#,
shell, PowerShell, Vue, Svelte and Astro**
get hand-written line-oriented extractors covering imports/requires, top-level declarations,
exports, and entrypoints.
These are not full parsers and are not trying to be. The signpost layer needs the module
graph and the public surface, and a focused extractor gets ~95% of that. Where
precision matters, SCIP enrichment (§4.3) supplies it.

**The JVM is the one language whose resolution map comes from the source rather than a
manifest** ([ADR 0017](adr/0017-a-resolution-root-may-come-from-the-source-itself.md)),
and that is a current limitation stated rather than worked around. No
`pom.xml` or `build.gradle` reader exists yet, so an import resolves against the
`package` declarations the repository's own files make — which is sufficient, because a
`package` declaration is exactly the name another file writes in its `import`, and it is
better than the alternative: deriving a package from its path yields
`src.main.java.com.example.api`, which resolves nothing anybody wrote. Two consequences
follow, both visible in the coverage report rather than papered over. A JVM import naming
no in-repo package resolves to *nothing* — never to an invented Maven coordinate the
repository never declared. And because the standard layout declares each package twice,
once per source set, one package name maps to two directories: production wins, and the
tiebreaker cannot be directory order, since Gradle's extra source set is conventionally
`integrationTest` and Android's is `androidTest` and both sort ahead of `main`.

A JVM test is also the one case where a third statement beats imports. Same-package
access needs no import, so a test's subject is precisely the one name its import list
does not contain — the `tested_by` edge comes from the package the test *declares*.

**The C family has no module system at all, so two things are modelled differently
there.** An `#include` is a path fragment, and what turns it into a file is the build's
`-I` flags, which signpost does not read. So resolution walks outward from the including
file's own directory trying the conventional roots — `include`, `src`, the directory
itself, `lib`, `source` — and stops at the nearest ancestor that holds the file. Anchoring
at the repository root would be correct for a single-project repository and wrong for
every other shape, since a monorepo has an `include/` per project. The delimiter is kept
on the import (`"util/buffer.h"` versus `<stdio.h>`) because the delimiter *is* the
resolution rule: quoted means look beside this file first, and a quoted include is never
the system library. Standard-library recognition then has a shape no other language has —
a C++ standard header has no extension, so an extensionless angled include is the standard
library *by construction*, with no list to go stale; C's own headers end in `.h` and are
indistinguishable by shape from a project's, so those need a list.

The second is `.h`, which is C, C++ or Objective-C and only its content can say which.
Classification is name-only by design, so a `.h` is labelled C — the family's lowest
common denominator — and one extractor claims all three languages and reads the whole
family's syntax regardless of which label dispatched the file. The label is a placeholder
rather than a finding, and it does not vote on the language of the directory it sits in:
an Objective-C directory holds a `.h` for every `.m`, and counting the header gives a tie
that would erase Objective-C from the bundle of a repository written in it.

**Ruby, PHP and C# each get the resolution rule their own ecosystem states, and no two of
them are the same rule.** Ruby's is a search path: a bare `require "corpus/format"` is found
by walking the load path, which in a repository means the conventional `lib/` under each
gem root, and a `require_relative` is internal by construction with no lookup needed at all.
PHP's is a declared map — the PSR-4 block in `composer.json` is the only place a namespace
prefix is bound to a directory, and it is delimited by backslash, so a prefix test done on
the string routes `CorpusKernel\Boot` under `Corpus\` and draws an edge to code that does not
exist. C# has no manifest naming its own namespaces at all, which puts it in the JVM's
position: `using Corpus.Domain` is spelled the same whether `Corpus.Domain` is a project in
this tree or a package on nuget.org, so the map is built from the namespaces the repository's
own files declare, and the delimiter there is the dot. Where a rule runs out the import is a
gap in the coverage report, never a coordinate the repository never declared.

The .NET runtime needs a rule rather than a prefix, and that is the interesting half.
`System.*` arrives with the SDK and is versioned with it, so a reference page for it is a
supply-chain entry for the toolchain. `Microsoft.*` splits: `Microsoft.Win32`,
`Microsoft.CSharp` and `Microsoft.VisualBasic` ship with the SDK and
`Microsoft.Extensions.Logging` is a `PackageReference` somebody upgrades. A check accepting
`Microsoft.*` as the platform hides the second kind behind the word "runtime", which is the
one direction that costs a reader something they cannot re-derive from the page.

A `ProjectReference` is the C# case where a declared dependency is *not* an external one. It
names another `.csproj` in the tree, so it composes rather than imports: the reference page
is suppressed and the edge lands on the referenced project's own module. And a .NET test
project is why C# has no `addTestEdges` arm despite looking like the JVM — it declares a
namespace of its own, `Corpus.Api.Tests`, which resolves to the test directory itself and
yields a self-edge; a C# test names its subject with a `using`, because a different namespace
is exactly what a `using` is for.

**Shell and PowerShell are two extractors, which is the C family's decision inverted, and it
rests on the same test:** whether one set of rules can read both. They are the pair a reader
would most expect to share an extractor, and they agree on `#` for a comment and on nothing
else load-bearing. A function nested inside another is global in shell and dies with the
enclosing scope in PowerShell, so identical nesting means opposite things about the public
surface. `source` names a file; `Import-Module` names a file *or* a module. And the string
rules diverge far enough that a heredoc and a here-string are separate scanner states, each
with its own closer. One extractor claiming both would still score well on every fixture and
would quietly stop naming one of the two languages anywhere in a bundle.

Shell is the one language in the tool with **no registry behind it at all**, and that shapes
where its gaps are reported. `source` and `.` name paths, so a path that reaches no file
cannot be a dependency somebody forgot to declare — there is no gem, package or module it
might have meant instead. Resolution returns internal-with-no-target, and the specifier lands
on the *unlinked* line: a first-party import that reached no page. It can never appear among
the unresolved, because that line means "go and declare this", and for shell there is nothing
to declare. A shell specifier surfacing there would mean resolution had begun inventing
packages for a language that has none.

PowerShell has both halves, split on a single character: a specifier containing `/` or `\` is
a path and resolves against the tree, trying the name, then `+.psm1`, then `+.ps1`, and never
falling through to a registry; a bare name is a gallery module. Its runtime is two runtimes,
because PowerShell runs on .NET — the engine modules whose cmdlets are the language's
vocabulary, and the .NET namespaces a `using namespace` reaches. The engine modules are a
closed list rather than a `Microsoft.PowerShell.*` prefix, for the same reason
`Microsoft.Extensions.*` forced a rule on the .NET side: `Microsoft.PowerShell.Crescendo` is a
separately versioned gallery module somebody installs and patches, and a prefix folds it into
the shell it merely shares a name with. A `#Requires -Modules` is read as a *requirement* and
not as a pin — it names no version and no source — and the `.psd1` module manifest whose
`RequiredModules` key would pin it is not read, so such a module is reported as a gap rather
than invented as a gallery entry the repository never wrote. That puts PowerShell beside the
JVM as the second ecosystem with no declared list for an import to match.

**Vue, Svelte and Astro are one extractor and it is not a new reader — it is a preprocessor in
front of the TypeScript one.** A single-file component is a document with program text inside it:
the `<script>` block is TypeScript or JavaScript, the rest is template and style. So the file is
read by blanking every region that is not script, byte for byte, and handing the result to the
existing extractor. Blanking rather than slicing is the whole of the decision — a slice
renumbers every line after the first fence, and a facts stream reporting an import at line 4 of a
file where it sits at line 12 is worse than no facts, because the position looks authoritative.
The three languages differ in fence syntax and in nothing an extractor cares about, and each
allows more than one script block: Vue's `<script setup>` beside a plain `<script>` is what its
own migration path produces, and Svelte's `context="module"` block holds the exports the instance
block cannot. So every block is read, not the first one found.

Two things are deliberately *not* read. A `<style>` block declares nothing this graph can hold, so
its `@import` of a stylesheet must appear in neither gap line — as unresolved it would tell a
reader to declare a dependency that is a file in the same tree, and as unlinked it would claim a
missing edge onto a node no stylesheet should ever have. And an SFC is never an entrypoint: it is
a component a framework mounts, so the entrypoint the TypeScript extractor would infer from a
top-level call is discarded.

The extension is the resolution detail worth stating. `./Badge.svelte` and `../layouts/Base.astro`
name their extension, which is the ordinary spelling in all three ecosystems, so resolution tries
the specifier exactly as written before appending the extensions it knows — the reverse order
looks for `./Badge.svelte.ts`, reaches nothing, and reports an unlinked import for a file sitting
next to the one that named it. And the component extensions are not runtimes: `vue`, `svelte` and
`astro` are declared dependencies with reference pages, so a component language is grouped with
TypeScript for Node-builtin recognition and nowhere else.

Extractors stay hand-written, and the threshold at which that changes is written down
([ADR 0022](adr/0022-extractors-are-hand-written-and-tree-sitter-has-a-threshold.md)): a
tree-sitter Go binding becomes the answer for *one* language when that language's scored
fixtures cannot be brought to the §4.2 targets by hand — not when an extractor has a bug in
it, which is what fixtures are for. It would be a direct library dependency we bump
ourselves, a different proposition from inheriting a tool's grammar tree, and it stays behind
the same extractor interface, so it is a swap rather than a redesign.

**What is deliberately not read is written down too, by category rather than by file type**
([ADR 0025](adr/0025-the-census-long-tail-is-declined-by-category.md)). A census across roughly
14,000 repositories produced a long tail, and five categories answer nearly all of it: editor and
tooling artifacts are not repository content; diagram formats are a picture of structure rather
than a statement of it; templating layers restate their host language's imports, so reading both
double-counts and misattributes; stylesheets and markup reach nothing this graph holds a node for;
and the low-count languages are declined on count, not on kind, each revisited when a fleet scan
shows it blocking coverage somebody actually has. Stating the category rather than the extension is
what keeps `.jinja2`, `.erb`, `.hbs` and `.gotmpl` one decision instead of four.

The first four languages are chosen for the same reason you chose them: Go, Rust,
TypeScript, and Python have the strongest tooling and the strongest model training
coverage. Java and Kotlin follow because they are the largest bodies of code the tool
could not read at all, and they share one namespace and one resolution map because the
compiler does — a Kotlin file importing a Java package is ordinary in every JVM
repository. C, C++ and Objective-C come next, and for the same reason as one another:
they are one family sharing one preprocessor and one header convention, so the language
boundary between them is not a boundary an extractor can see. Ruby, PHP and C# follow next,
and their reason is the inverse of the C family's: they have nothing in
common with each other or with what came before, and each is the language a large body of
existing services is written in. Shell and PowerShell come next for a reason
neither of the two preceding groups had: they are not what a repository is *written* in, they
are what it is built, released and operated by. A repository whose scripts declare nothing
reads as one with no build and no deployment path, which is the one part of a bundle an agent
is most likely to act on. Vue, Svelte and Astro come last of the seventeen because they are the
cheapest of the set — the reader already existed and what was missing was getting the program
text out of the document around it — and because they are where the frontend of a repository
already covered on its server side actually lives. Everything else falls back to a generic
extractor (comment headers, filename conventions, sibling context) plus the semantic pass.

**Manifests and infrastructure are the highest-value deterministic signal and the
part comparable tools mostly skip.** All of this is exact, cheap, and structural:

| Source | Yields |
|---|---|
| `go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`, `Gemfile`, `*.gemspec`, `composer.json`, `*.csproj` | external deps, module identity, scripts, entrypoints |
| `CMakeLists.txt`, `*.cmake`, `MODULE.bazel`, `WORKSPACE`, `BUILD.bazel`, `*.bzl` | what the project builds, which of its own libraries a target links, found and fetched packages, declared tests |
| `Containerfile` / `Dockerfile`, compose files | services, ports, base images, build inputs |
| `*.tf`, `*.tfvars` | what runs, where state lives, which of the repository's own directories the infrastructure is composed from, secret *references* |
| `.github/workflows/*` | how it builds, tests, and ships; what gates exist |
| Helm charts, k8s manifests | deployable units, config surface, secrets *references* |
| `*.proto`, OpenAPI, GraphQL SDL | interface contracts |
| `migrations/*` | data model and its evolution |
| `CODEOWNERS`, `AGENTS.md`, `CLAUDE.md`, `docs/adr/*` | ownership, stated rules, decisions |

**A secret reference names two things, and both have to be right.** The credential is
recorded by name and never by value, which is the rule that keeps a committed bundle
from being an exfiltration path. The second half is *whose* reference it is: a
reference is attributed to the service that makes it, and a service inherits only the
references a file states without naming any service — a compose top-level `secrets:`
block, an OpenAPI security scheme. It is tempting to treat this as cosmetic, because
no value moves either way. It is not. "This service reads that credential" is a fact
a reader acts on without re-deriving, so an over-broad attribution says a credential
is reachable from somewhere it is not, and that is how a threat model or an incident
scope gets drawn around the wrong set of services. A missing edge prompts a question;
a fabricated one prompts a conclusion.

So there is a third state, and it is not the same as "unknown". A compose file's
top-level `secrets:` block declares credentials for the services beside it without
saying which reads which, and handing them to all of them trades a false claim for no
claim at all. A Terraform `variable "db_password"` is not that shape: one `.tf` file
holds a dozen unrelated resources, and which of them reads the variable is stated in an
expression the reader does not evaluate. Such a reference is kept and deliberately
attributed to nothing — it still answers "does this file touch credentials", and it
reaches no page. A fact with nowhere to go, rather than a fact in the wrong place.
This is one half of
[ADR 0016](adr/0016-a-reader-records-what-only-it-can-know.md): a reader records what no
downstream consumer can re-derive, including that it could not attribute something.

**A build file is the only place some structure is stated, and no build file can settle it
alone.** For C there is no second source: `#include` says which header a file reads and nothing
about what gets linked into what, and no C manifest states it either. So reading `CMakeLists.txt`
is not an increment on dependency coverage — it is the difference between a C repository having
structure in the bundle and having none. But `target_link_libraries(buffer_test PRIVATE
corpus_buffer_core cmocka)` gives the reader no way to tell the two names apart: one is a library
declared by an `add_library` in a *different* file, one is third-party, and CMake's own resolution
consults the whole configured project. Bazel states which is which in the label — `@repo`, `//pkg`,
`:name` — and moves the question to where `//` points, which is the workspace root and not the
repository root. Both are settled in `assemble`, where the whole tree is visible, from declarations
the readers record verbatim:
[ADR 0023](adr/0023-a-build-declaration-is-settled-where-the-tree-is-visible.md). Reported the
wrong way, a library the repository builds becomes a supply-chain entry for its own code while a
real dependency drops out of it; a label read repository-relative loses every internal edge a
workspace below the top declares, or lands one on an unrelated directory of the same name.

**A configuration is mostly wiring, and only some of it is a unit.** Terraform is read
for what a reader can act on: resources that run something or hold state become units,
and the policy attachments, firewall rules, and route table associations a real
configuration declares by the hundred do not — a page for each would bury the one
service among the plumbing around it. `data` blocks are read and never become units;
they describe what another configuration owns. A secret store is the one exception to
the "runs something" rule, on the same grounds as a k8s `Secret` document: the resource
*is* the named credential, so where the credentials live is a thing a reader looks up.
And a declared dependency whose target is a directory of this repository is not external
— a local `module "queue" { source = "./modules/queue" }` is a composition edge, not a
third-party reference page, which is the same distinction an npm workspace sibling gets.
Whether a declaration resolved locally is recorded by the reader that saw the `./`, since
`modules/rds` and `hashicorp/vpc/aws` are the same string shape and only the syntax
distinguishes them — the other half of
[ADR 0016](adr/0016-a-reader-records-what-only-it-can-know.md).

**Git signals** via `git log`: co-change pairs, churn per path, author concentration,
last-touch date, first-commit date. Co-change is the cheapest way to find coupling that
imports do not show. All of it annotates nodes the structural pass already created, and
none of it creates one:
[ADR 0020](adr/0020-git-history-annotates-the-map-and-never-draws-it.md).

Two further reads sit alongside those, and both answer a question with a count rather than
with text. **Commit-message shape** rides on the same walk — one more field on a format git
is already producing — and yields how many subjects were seen, how many follow Conventional
Commits, and how many name an issue. No subject is stored anywhere: a message is arbitrary
bytes from an untrusted repository bound for a committed markdown file, and counting one owns
no escaping problem where keeping one would. Adoption is bimodal in practice, measured at
100/99/96/83/11/0/0 percent across seven repositories, so the *rate* is the signal and a
repository at 11% is reported as not using the convention rather than as partly using it.
**Tags** are a separate read, because a tag is a ref and not a commit: how many are reachable
from the described commit, the newest, and how far past it this commit is. Reachability is
`--merged`, so tagging an unrelated branch does not move the number. A shallow clone has no
tags and neither does an untagged repository, so the two are distinguished explicitly and the
first reports as unknown with the fix named — §4.2's rule applied to a signal rather than to
an extractor. Both land as practice findings, not on the graph. Blame, branch topology, and
`.git/config` remotes are refused with reasons in
[ADR 0026](adr/0026-history-is-read-where-a-count-answers-the-question.md), which also records
the field-order rule the log format follows: the two fields a repository controls come last,
because git accepts a unit separator inside an author name and a name containing one used to
shift the date out of its own field.

Git and a forge are the recommended setup, and where they are present they are
authoritative: what is tracked, what is ignored, and which commit the bundle describes
are theirs to decide, not signpost's. But git is not a requirement for producing a
bundle. A tree that arrives as a tarball still gets one, on best effort: history signals
are reported as not read, and the pages carry no `resource:` or `generated:` stamp at all
rather than a commit nobody can check. `verify` reports its staleness check as skipped
and exits zero. Every page a repository with history would get is still written, under
the same name, because a page's identity comes from the tree and not the log
([ADR 0015](adr/0015-a-colliding-page-name-is-suffixed-from-its-own-key.md)). This is a
corner case, not a supported mode of operation, and it degrades by saying less rather
than by guessing.

### 4.2 Extractor accuracy is measured, not asserted

Each extractor ships with a fixture corpus and a scored test: extracted
imports/exports versus a hand-labeled expectation. The score is reported in
`manifest.json` per language. When an extractor is below target for a language
present in the repo, the affected pages say so in `status` and the bundle records
it in `skipped_checks`. Absence of measurement is never presented as a clean bill.

**A gap has two kinds and they get two lines.** "No extractor for `.kt`" says signpost
recognised the language and cannot read it. "N file(s) of no recognised kind" says it
could not determine what the file was, so no reader was ever offered it. Folding them
into one line lets a repository whose only frontend is `.astro` read as covered, which
is how the second kind went unreported for the whole of v0.1.0: the classification was
written in one place and read in none, while the two classes that *can* come back
empty-handed each carried an `Unhandled` map the report prints. Which extensions are in
the source table decided whether the gap was visible at all — `.sh` and `.sql` land
there as an unhandled *language* and get counted; `.astro` and `.vue` never reached the
stage that counts, and both are read now — and every extractor added widens that table.

**Running signpost on signpost is not sufficient, and the gap is structural.** The
CI dogfood job exercises the paths this repository contains, and this repository is
Go with kebab-case filenames. It cannot reach the TypeScript, Python, or Rust
extractors; it cannot reach an npm, Cargo, or pyproject manifest; and it cannot
reach a path carrying a character that is an indicator in YAML, because none of its
tracked paths contain one. That last gap cost something real: a Next.js dynamic
route written unquoted into a flow mapping made four pages of a real repository
unreadable from that line down, with every unit test and the dogfood job green.

So `testdata/corpus` is a second repository, synthetic and committed: all four
first-class languages, four manifest ecosystems, a gating workflow and a
schedule-only one, and the filenames that break naive emitters — bracketed and
parenthesised route directories, a comma in a basename. It is staged as its own git
repository before analysis, because signpost reads history and the corpus's
directory inside this checkout carries *signpost's* commits.

Two checks run against it, and the division of labour matters. The Go tests assert
**named facts** — this language produced a module page, this manifest was read —
never counts, because a count assertion fails on every improvement to an extractor
and never says which fact was lost. Separately, CI parses every emitted page with a
**conforming third-party YAML reader**: signpost's own is tolerant by design (ADR
0001), built to keep reading past what a conforming parser rejects, which makes it
the wrong instrument for proving its own output well-formed.

The strongest assertion is a frontmatter round-trip that validates the *key set* of
each edge mapping, and it is strong because it needs no advance knowledge of the
offending character. An unexpected key means a scalar terminated where the emitter
did not intend, whatever caused it. This matters more than parseability alone: an
unquoted `[` raises, but an unquoted `,` parses clean and silently splits the
scalar, so a check for "did it parse" passes on a page whose `source:` now names a
file that does not exist. Every path-injection defect so far — a newline, a
backtick, a `](`, then a bracket — was found by a person imagining the character,
and that does not scale.

**Every fixed bug becomes a stage in this harness.** Not only a unit test in the
package that owns the fix — that test proves the function behaves, and every defect
this harness has been extended for shipped with green unit tests over the code it
broke. The bracket was invisible because no tracked path in this repository contains
one. The CRLF checkout was invisible because this repository's `.gitattributes` pins
`eol=lf`, so the one tree signpost is developed in is the one tree configured to
prevent it. The inflated census and the misattributed secrets were both invisible
because they need a *second* thing to be wrong about — a bundle already on disk, a
neighbouring service in the same file — and a unit test over one function is handed
one input. The generalisation is worth stating plainly: **a bug survives a package's
own tests when the tree those tests run in cannot express the condition.** The corpus
exists to be a tree that can, which makes it the right home for the regression rather
than a second copy of the unit test.

A stage asserts the symptom a user would report, not only the exit code — the CRLF
stage checks the fabricated "N page(s) had human notes" count as well as `verify`,
because a partial fix satisfying only `verify` still prints a number that teaches a
reader the count means nothing. And each stage has a counterpart that must still
fail: normalising line endings must not normalise away a change in what the bundle
*says*, so the same converted checkout gets one sentence edited inside a managed
region and `verify` must reject it. Without that half, the stage is satisfied by a
fix that stopped checking.

### 4.3 Enrich — optional

If a SCIP index is present (`index.scip`) or a codeatlas endpoint is configured,
signpost consumes it for precise symbols, definitions, references, and real call
edges across all seven of codeatlas's language tiers. This is strictly additive:
edges gain `confidence: extracted` instead of `inferred`, and the call graph
becomes real rather than import-shaped.

The SCIP reader is build-tagged and decodes only the subset we need (`Document`,
`Occurrence`, `SymbolInformation`) via `google.golang.org/protobuf` — a
Google-published, heavily audited library that codeatlas already carries, and one
we can bump ourselves the day a CVE lands.

**signpost never requires codeatlas to run.** codeatlas is a service with two
Postgres instances, Redis, LanceDB, and a Helm chart. signpost is a binary you
point at a directory. The relationship is enrichment, not dependency.

### 4.4 Build the graph — in process

Nodes: modules, files, symbols (when enriched), services, interfaces, data stores,
pipelines, external dependencies, documents.

Typed edges: `imports`, `calls`, `implements`, `defines`, `configures`, `deploys`,
`tested_by`, `documents`, `co_changes`, `owns`, `precedes`.

**A CI job is a node, and `precedes` is drawn only where a file declares the order.** A job is
the unit rather than a workflow file, because a required-check rule is configured against a job
and a failing check names one — the job is what a reader arrives with. A job's `needs` is a
declared ordering and becomes an edge from the job that finishes first to the one that waits.
Nothing else in the repository is. Jobs with no `needs` run concurrently, and deriving an order
from their position in the file would put `Extracted` confidence on a sequence GitHub does not
honour — a reader would then sequence work around it, which is worse than the edge's absence. The
same rule refuses a flow assembled from imports: traversing them out from an entrypoint yields a
reachability set, not a sequence, and there is no call graph to order it by (§4.1 discards call
sites, and [ADR 0022](adr/0022-extractors-are-hand-written-and-tree-sitter-has-a-threshold.md)
records why). [ADR 0032](adr/0032-order-is-drawn-only-where-a-file-declares-it.md) has the rest,
including why a `needs` resolves by a job's key and not by its name.

**A module page names its public surface, without the graph gaining a node for it.** §4.1
already extracts exported declarations in every language, and a page reporting only how
many of them there are tells an agent that a module *has* a surface while withholding the
part it needs: whether the name it is looking for is in there. So the page lists them —
methods qualified by their receiver, because `String` alone does not say which of a
module's types has it. Only exported ones, which is the load-bearing half: a list
including private helpers would describe a surface callers cannot reach, and an agent
writing against it would produce code the compiler rejects. Visibility is per-language and
not always a keyword — Python's is a leading underscore by convention, PHP's default is
public, and C inverts the rule entirely, where external linkage is the default and only
`static` withdraws it.

**A test file declares no surface, and this is the one case where every language's
visibility rule gives the wrong answer.** Go's `TestFoo` is exported and reachable by
nothing but `go test`; a PHPUnit method is public because the runner requires it. So the
file's *classification* decides rather than the declaration, reusing the same
`discover.File` flag the `tested_by` edge is drawn from — one place decides what a test is.
Measured on this repository before that rule existed: test functions were 51% of every name
the bundle printed, `internal/assemble` showed 57 of them out of 60 and `cmd/signpost` 60
out of 60, so the bound truncated the real surface off the page. A list whose truncation
drops exactly the part a reader came for is worse than the count it replaced. The test files
themselves are still listed, and the edge to them is still drawn: what is withheld is the
claim that their declarations are callable.

This stays a page attribute rather than becoming symbol nodes and `calls` edges. ADR
[0003](adr/0003-directory-granularity-for-module-nodes.md) fixes the graph at directory
granularity, and an import-shaped call graph inferred from declarations would be the
confidently-wrong artefact §4.6 exists to prevent; real call edges arrive from a SCIP
index or codeatlas per §4.3, where they are `extracted` rather than guessed. The list is
bounded and says so when it truncates, for the reason the file list is bounded: a module
with sixty exports would otherwise push its edges — the part an agent navigates by — off
the first screen.

**The exports reach the machine-readable formats split by what each consumer can do with
them.** The JSON export carries the names, because it is the format a script and the local
viewer read and a tool asking what a module offers wants the same answer the page gives.
GraphML carries `n_exports` as a count: its attributes are declared scalars that Gephi and
networkx size, colour, and rank on, and a 200-name string in a typed column is a value
nothing can compute over and every table truncates. Mermaid and DOT carry neither, for the
reason they already carry no file list — a box label is not a place for forty identifiers.
The two formats also disagree about a module with no exports, and both are right: JSON omits
the key, because most nodes in a graph are services and documents signpost never extracts
symbols from and an empty array on each would claim a measured absence; GraphML writes 0,
because a column that is blank on some rows and numeric on others cannot be ranked.

Every edge carries `confidence`: `extracted` (found in source or manifest),
`inferred` (derived by the model), `ambiguous` (model was unsure). An agent can
therefore weight what it trusts, and a reviewer can audit what was guessed. Recorded as
ADR [0004](adr/0004-confidence-is-a-first-class-field.md), which also states what the
field does *not* claim.

**Resolution precedence: this repository first, the manifest second, unresolved third.**
An import resolves against the directories signpost found before it resolves against the
dependencies a manifest declares, and a specifier that matches neither is counted as
unresolved rather than turned into a node. The order is not a preference. A monorepo
declares its own packages as ordinary dependencies — `"@scope/core": "workspace:*"` — so
both lookups match, and the manifest entry is the weaker fact: it says the package is
depended on, while the directory says where it is. Reading the manifest first turns
first-party source into a third-party dependency page, which is a false claim about the
supply chain in the direction that misleads — a reader auditing what the repository pulls
in from outside is shown code that repository wrote, and cannot tell the two kinds apart.
Each ecosystem therefore needs a name-to-directory map for the first lookup to consult;
Go had one from the start, npm did not, and its absence was a bug rather than a gap.

**Within the first lookup, a declared mapping outranks a guessed one.** Where a codebase
states what its own specifiers mean, that statement is authoritative and is consulted before
any convention: `compilerOptions.paths` in tsconfig.json is the only thing in a TypeScript
repository that says `@fider/services` is `public/services`. Reading it is not optional
enrichment — before signpost did, 542 of 3912 edges were absent on one real repository, 14%
of the graph, from a single unread mapping. The guessed prefixes (`@/`, `~/`, `#/` as
repo-relative) stay, but only *after* the declared aliases and only as a fallback for a
config signpost could not read. And a specifier a declared alias matched never falls through
to the dependency lookup, even when its target holds no extracted source: the mapping is
proof the name is first-party, so falling through would report the codebase's own directory
mapping as a package nobody publishes — the same false supply-chain claim as above, reached
by a different road.

**Being too generous is a failure mode, symmetrical with being too strict, and harder to
see.** An over-broad match produces no error and no missing edge — it produces a confident
wrong answer, either an edge into the repository that invents structure or an external node
that invents a dependency. Positive tests cannot distinguish it from correctness, so the
corpus carries a deliberate near-miss per ecosystem and asserts the unresolved-specifier
count, which moves in opposite directions for the two failures. Name matching is by path
segment and full name, never by string prefix: `example.com/corpus/greeterx` is not the
module `example.com/corpus/greeter`, `httpx_extras` is not the declared `httpx`, and
`pathe/utils` is not the Node builtin `path`, however much of a prefix they share.

**A gap in resolution has two kinds, and they get two lines.** An *unresolved* specifier is a
name signpost could not place at all; an *unlinked* one it placed exactly — inside a Go module,
under a matched alias, down a relative path — and found no node at. The second was invisible
until it was counted, because both of the resolver's decisions about it are correct: the
specifier is first-party, and inventing an external node for it would be the false
supply-chain claim above. The branch that handled it was therefore empty, and a module whose
every import landed there reported importing nothing. The two are separate lines because the
fixes are different work: an unresolved specifier needs a resolver that knows the convention,
while an unlinked one is often nothing to fix at all — generated code genuinely is not in the
tree — and otherwise needs a reader for whatever sits at that path. A handful is ordinary; a
lot means a resolution root is missing, which is the shape the tsconfig `paths` gap had.

**The runtime is neither a dependency nor a gap.** `import os`, `import "fmt"` and
`import fs from "node:fs"` are resolved — to nothing that deserves a node. Nobody patches
the standard library separately from the toolchain, so a reference page for it is a
supply-chain entry for something that has no supply chain, and counting it unresolved is
worse: it would make every honest repository look as though assembly had failed. Each
language is tested on its first path segment, cut on that language's own separator — `/`,
`::`, `.`, and `/` again for Node after any `node:` prefix is trimmed. The segment matters
because these modules are routinely addressed by subpath: `fs/promises` is the only way to
reach the promise-based API, and looking the whole specifier up in a table holding `fs`
reported the runtime as a dependency the repository could not resolve.

Metrics, all hand-written and all deterministic:

- degree, fan-in, fan-out → hub identification
- Tarjan SCC, iterative → dependency cycles, which are real findings
- connected components → orphans, and doc-versus-code islands
- Louvain modularity optimisation → clusters
- cross-cluster edges → the bridges where a change is most likely to surprise

**Clustering: Louvain, after measuring that label propagation does not work
here.** LPA was the first choice — a third the code, and the reasoning was that
"group related modules" is undemanding enough that clustering quality would not
show. That was wrong, and the test that proved it is in the suite
(`TestClustersSeparateDenseGroups`): on two dense groups joined by a single edge,
synchronous LPA with a lowest-label tie-break collapses everything into one
community. It degenerates into a min-label flood across any connected graph — the
documented giant-community pathology. One cluster containing the whole repo
breaks every consumer of the partition at once: `manifest.json`'s cluster count
reads 1 for every repository, cross-cluster bridges are empty because no edge can
cross a boundary that does not exist, and the DOT and Mermaid diagrams draw one box.

Louvain costs about 150 more lines of arithmetic and gets it right. It is still
far cheaper than the dependency it replaces, and it is deterministic by
construction rather than by seeding: sorted node order, ascending community
evaluation, ties to the lowest index, final numbering by each cluster's
lowest-sorting member. No randomised restarts. Recorded as
[ADR 0019](adr/0019-louvain-over-label-propagation.md).

Tarjan is iterative rather than recursive, and there is a 20k-node deep-chain
test for it, because a recursive implementation risks stack exhaustion on a large
monorepo — which is exactly the case signpost is for.

### 4.5 Semantic pass — model, optional, schema-constrained

The deterministic pass yields structure. It cannot yield *purpose*. The semantic
pass fills exactly the gaps where prose is the only source:

1. **Role summaries** — what a module is for, in two sentences, grounded in the
   symbols and docs already extracted.
2. **Invariants and constraints** stated in `AGENTS.md`, ADRs, and CHANGELOG that
   are not visible in code.
3. **Doc-to-code linking** — the highest-value output. Deterministic extraction
   leaves docs as a disconnected island: the spec and the implementation agree in
   meaning with nothing structurally joining them. The model's job is to attach
   "Scan Verification Gate" in a design doc to the function that implements it,
   as an `inferred` edge. This is where doc/code drift becomes visible, and it is
   the thing no amount of AST parsing will produce.
4. **Cluster labels** for `index.md` headings.

Every call is **schema-constrained**: a JSON Schema goes out with the request and
comes back as validated JSON. On inferd this compiles to a GBNF grammar that
constrains the sampler, which is what makes a local 12B reliable enough to trust
here — the model cannot emit malformed output, only wrong output, and wrong output
is caught by the grounding rule below.

**Grounding rule:** every semantic claim must cite a `sources[]` entry pointing at
a real file, and the citation is verified to resolve before emit. A claim that
cannot be grounded is dropped, not softened. This is the guard against a small
local model inventing plausible architecture.

Per-node, budget-bounded, and cached by content hash — an unchanged file is never
re-summarized.

#### What is implemented, and how it lands on the page

Output 1 only: role summaries. Invariant extraction, doc-to-code linking, and
cluster labels are specified above and not attempted — and the third is the one
worth the wait, so it should not be bolted onto a pass built for one-paragraph
answers.

The pass is **opt-in twice over**: a backend has to be configured — §5 makes
`none` the default and nothing infers one — *and* `build -semantic` has to be
passed. Two gates because they answer different questions. The environment says a
model is available; the flag says this run should spend it. Without the second, a
developer who configured a backend to try `signpost model check` would find every
subsequent build calling it. It is also what keeps §8's split honest: the
difference between the per-push build and the scheduled one lives in a workflow
file rather than in an environment variable somebody can set repository-wide.

`-semantic` with no backend configured is an error, not a skip. The flag is a
request to spend a model, and answering it with a silent deterministic build is
how a scheduled workflow runs for a month producing nothing while reporting
success.

Model prose lands in a **`role` region of its own**, beside the deterministic
`summary` rather than replacing it. That follows from the merge rule §6.1
describes: a managed region present on disk but absent from a fresh render is kept
verbatim. So a deterministic build renders no `role`, finds the one the scheduled
run wrote, and carries it through untouched — while writing into `summary` would
put model prose in a region every build *does* render, and the next push would
overwrite it with the placeholder. The separation also keeps the two kinds of
claim visually apart, which §4.1's trust grading asks for: `summary` is counted
facts, `role` is a grounded guess carrying an attribution line naming the model
and the files it read.

Everything after the backend is built **fails open**, and that is a compile-time
fact rather than a convention — the pass has no error return. A backend that goes
away stops the pass and names what was lost; a response that cannot be grounded
drops that one summary. Neither can fail a build.

What could not be summarised is reported on **stderr**, not recorded in the
bundle. §4.2's rule is that a pass says what it could not account for, but
`manifest.json` is regenerated wholesale, so a skip record there would change on
every subsequent deterministic push and turn the staleness gate red on a fact
about a run that is over. The report is deliberately not suppressed by `-quiet`,
which silences the routine coverage summary: a fail-open pass whose failures are
quiet is the one failure mode that looks like success.

#### Repository content is untrusted input

This is a security boundary, and it was missing from the first draft of this
design. The semantic pass reads files out of a repository and puts them in a
prompt. Anyone who can land a comment in that repository — a dependency vendored
into the tree, a fork, a contributor, a PR that has not merged yet — can write
text that is addressed to the model rather than to a human reader. The output of
that model is then **committed to the repo and read by agents that act on it**.
That makes prompt injection here a supply-chain path into the artifact agents
trust, not a curiosity.

Three mechanisms, all cheap and all in the deterministic part of the code:

1. **Delimited, hash-stamped blocks.** Every file goes into the prompt wrapped as
   `<untrusted_source path="..." sha256="...">` … `</untrusted_source>`. The
   system prompt states that content inside such a block is data to be analysed
   and never instructions to follow, and that instructions found inside one are to
   be ignored rather than obeyed.
2. **Sentinel defanging.** Before wrapping, neutralise — by inserting a
   zero-width space rather than deleting, so offsets and analysis stay usable —
   any sequence a hostile file could use to forge a role turn or break out of the
   wrapper: our own opening and closing delimiter, chat-template control tokens
   (`<|im_start|>`, `<|system|>`, `<|endoftext|>` and friends), `<<SYS>>`,
   `[INST]`, and lines that are nothing but a heading naming a chat role.
   Without this, a file containing a premature `</untrusted_source>` smuggles
   instructions into the trusted region and the wrapper is decorative.

   Two properties of the *matching* are load-bearing, both established by an
   escape that got through a first implementation. Matching is **case-insensitive**:
   `</UNTRUSTED_SOURCE>` closes the block for a model reading rendered text exactly
   as well as the lower-case spelling, and a case-sensitive replace passed it
   through untouched. And the role-heading rule matches the line's **shape** rather
   than a fixed list of strings, because `## System:`, `###  System:` with two
   spaces, and `### system` without a colon all imitate a chat transcript just as
   well as the canonical spelling. Casing is preserved when a match is rewritten —
   defanging is meant to be invisible to a human reading the generated page, and
   silently down-casing a line of someone's source is a visible edit to the file
   being described.
3. **The grounding rule and the schema do the rest.** Schema-constrained sampling
   means a successful injection still cannot change the *shape* of the output, and
   the grounding rule means an injected claim with no resolvable citation is
   dropped at emit. Defence in depth: the wrapper is the fence, and grounding is
   what catches whatever gets over it.

   Two details of the enforcement are worth stating because neither is a matter
   of taste. Every bound on the response — length, citation count — is a schema
   constraint rather than a sentence in the prompt: a `description` saying "one or
   two sentences" is a hint a model may ignore, and `maxLength` is a constraint
   the sampler enforces. And an over-long or partly-grounded answer is **refused,
   not repaired**: a summary with one invented citation removed reads exactly like
   one that was grounded all along, and a summary cut mid-sentence and committed
   as complete is the confidently-wrong output this design exists to avoid. A
   cached entry is re-grounded when it is read, because a cache file is a file in
   a working tree and so is exactly as untrustworthy as a fresh response.

   A schema bound being *enforced* is not the same as the answer being *complete*,
   and the difference is a real observed failure rather than a hypothetical. A
   backend may honour `maxLength` by cutting the string at it and returning the
   prefix — valid JSON, of a legal length, with `finish_reason: "stop"` — which
   passes every check above while putting a sentence that stops mid-word on the
   page. So completeness is checked separately from length: prose that ran to the
   cap *and* did not finish a sentence was stopped by the backend rather than
   ended by the model, and is refused. Neither signal alone would do. A model that
   uses its whole budget and finishes is answering well, and prose flattened from
   a list legitimately ends without a full stop, so refusing on either signal by
   itself would drop good summaries over a matter of style.
4. **Model prose cannot alter the page it lands in.** The text is written inside a
   managed region, and regions are found by matching marker lines textually — so
   prose containing `<!-- /signpost:managed:role -->` would close its own region,
   and everything after it would become human text signpost then refuses to
   overwrite. That is a permanent foothold for whoever can talk a model into
   emitting one string. Every HTML comment is stripped from the prose before it is
   written, not just the markers, and an unterminated `<!--` takes the rest of the
   text with it.
5. **Nor can a filename.** The prose is not the only untrusted text that reaches a
   page. A summary names the files it rests on, and a path is repository content —
   on POSIX a filename may contain a newline, so a file can put a line of its own
   choosing inside the region that cites it, and a line that reads as a close
   marker ends the region early. That needs no model in the loop at all: the
   deterministic file list on every module page carries the same exposure, and so
   does a `## Title` heading derived from a directory name, which is *human* text by
   design and therefore outside the managed-region guard entirely.

   Enforced at the two chokepoints all generated text passes through rather than at
   each call site, so a region added later inherits the guard: `<!--` is escaped in
   every managed region and every generated heading, and a heading is folded to one
   line. A path that would break the code span it sits in is quoted rather than
   dropped, because a filename containing a newline is a fact worth showing plainly.
   The semantic pass *additionally* refuses a summary whose cite path carries marker
   syntax — grounding checks that a path was one signpost sent, which a real file
   with a crafted name passes — keeping refusal the rule on that side and escaping
   the rule on the emitter's. The two are independent on purpose: neither is relied
   on to be the only one.

   The same reasoning covers a second, quieter case in the same family: markdown
   link syntax is positional, so a directory named `x](https://evil.example/y)(`
   closes the label of every link that names it and makes what follows the target.
   `verify` passes, because the link it checks is well-formed and resolves — the
   forged one is a different link. Every generated link's label is escaped through
   one function; the target never needs it, being a node ID `assemble` built rather
   than text from the tree.

   The general rule this settles on: **any repository-derived string that reaches a
   page is escaped at the point the page is assembled, not at the point it was
   read.** A path, a title, a description and a summary arrive by different routes
   and no single reader can vouch for all of them, while the emitter is one place
   that sees all four.

Prompt hardening is mitigation, not proof — a sufficiently clever injection inside
a delimiter block can still influence a summary. What bounds the damage is that the
model's only reachable output is schema-shaped, citation-checked prose in a page a
human can review, and that `generated.by` records what produced it. Worth stating
plainly in §11 as well: signpost reduces this risk, it does not eliminate it.

### 4.6 Emit and verify

Emit the bundle. Then `signpost verify` checks:

- OKF §11 conformance: parseable frontmatter, non-empty `type`, reserved filenames
  used correctly
- every `edges[].to` and every prose bundle-relative link resolves
- every `sources[].resource` resolves
- `resource` shas match the commit being described
- byte-stability: a second run at the same sha with a warm cache produces an
  identical bundle
- `manifest.pages` names exactly the pages a build writes
- no page describes a concept the repository does not have

`verify` exits non-zero on failure. This matters: the failure mode to avoid is a
staleness check that exits zero, because a bundle that is silently stale is worse
than no bundle — it is confidently wrong, and it destroys trust in the tool
permanently.

**A page whose concept is gone is deleted, unless somebody has written on it.** A
renamed or deleted directory leaves a page describing a module that is not there,
carrying plausible `edges`, an `attributes` block, and a `resource:` naming a commit
where the code really did exist. That reads as authoritative, which makes it more
expensive than a missing page rather than less. But deleting unconditionally would
take a human's `## Notes` with it on the first rename, and preserving those is what
§8.1 compounds on. So the test is the page's *content*, not the graph: a page holding
nothing but the skeleton a first emit wrote is removed and named in the run's output,
and anything else is kept and reported for a human to resolve. Every uncertainty — an
unreadable file, an unrecognised frontmatter key, a `verified:` block — falls toward
keeping it.

`verify`'s severity mirrors that split, and the mirror is what makes the finding
actionable rather than decorative. A surplus page a build **would** remove is a
failure, because the remedy is `signpost build` — the same remedy every other failure
here names. A surplus page a build **keeps** is a warning, because no command can
resolve it and a red gate with no supported fix is a gate people switch off.

**`-as-of-bundle` reads history as of the bundle's commit, not just its stamp.** The bundle
is written on the default branch only (§8.0), so everywhere else the commit it records is
behind by construction — which is why the pull-request gate passes this flag at all. What is
not obvious, and cost a red gate on every conforming pull request to find, is that the stamp
is not the only thing a commit moves. Seven churn attributes on a module page (`commits`,
`lines_added`, `lines_removed`, `first_commit`, `last_commit`, `top_author`,
`top_author_share`) and the `co_changes` edges are read from git, and they land in page
*content*. One commit adding a comment changes `commits` and `lines_added` on that
directory's page. One commit touching two directories can create a `co_changes` edge, and
that moves the edge totals on `index.md`, `log.md`, and `manifest.json` as well.

Adopting the recorded values field by field does not fix it. The edge counts are arithmetic
over a graph that genuinely has one more edge in it than the bundle's graph did, and there is
no field to copy for that. So the flag reads the *history* as of the recorded commit instead:
`git log` ends there, the analysis sees exactly the commits the bundle saw, and every
history-derived field is identical by construction rather than by exception. Nothing about the
content comparison is relaxed — a code change still fails, which is what makes the mode safe
to pass on every pull request.

Two properties keep that honest. The recorded sha is untrusted input on its way to an argument
list — it comes from `manifest.json`, a committed file anyone with a pull request can edit — so
it is accepted only as forty lowercase hex characters and passed after a `--end-of-options`
sentinel; `HEAD@{upstream}`, `:/text`, a branch name, and an abbreviation are all refused, and
a refused value falls back to reading from HEAD. And a sha this clone does not have — what a
squash merge or a rebase leaves behind — is the same fallback rather than an error, because the
content it describes is perfectly current and failing there would break the gate on exactly the
repositories that squash-merge. Both fallbacks are printed: a run that read history from HEAD
never claims to have read it as of anything else.

**On a branch, a difference the merge resolves is reported and does not fail.** Reading the
bundle's history made every history-derived field identical, and the gate went red anyway on
thirteen consecutive pull requests — every one of them correctly. The reason is the remedy rather
than the comparison: the failures above all name `signpost build`, and §8.0 forbids running it on
a branch. So a pull request that added a package had a red gate whose instructions its author was
not permitted to follow, and the real remedy was to merge and let the push job rebuild. A check
that is red whenever anybody touches structure gets merged past as a habit, and the habit does not
pause for the run where the bundle is genuinely broken. Inverting it would be worse: then the
broken bundle is the green one.

So under `-as-of-bundle` findings are sorted by what the reader can do about them:

| severity | meaning | the reader's move |
|---|---|---|
| failure | wrong now, and wrong after the merge too | fix it; the gate is red |
| pending | a rebuild after the merge resolves it, and nothing else can | nothing; the gate is green |
| warning | no command resolves it | a human decides |

Four kinds are pending, and the list is what the distinction lives or dies by: a page a build
would rewrite, a concept with no page, a page with no concept, and a `pages` list that is
arithmetic over those two. Everything else stays a failure — a deleted bundle, a link with no
target, frontmatter no conforming reader can parse, a page claiming a commit that is not the one
being described. A merge inherits every one of those rather than repairing it.

Pending exists on a branch and nowhere else. The strict verify is the run that *writes* the
bundle, so it has no later rebuild to defer to and each of those four kinds is a defect there —
the same asymmetry this flag already draws for provenance, one severity further down. And pending
findings are printed in full above the verdict, never folded into a count: "nothing to do" is only
trustworthy if the reader can see what was set aside and disagree with it. A gate that silently
swallowed a page it decided was somebody else's problem would be the confidently-wrong artefact
this section exists to prevent, arriving through the exit code instead of the pages.

The post-commit hook (§6.0.1) reads the same run and reports pending as a reminder, because on a
developer's machine the remedy exists: there is no merge and no push job, so `signpost build` is
theirs to run. Same comparison, same severity, opposite audience.

---

## 5. Model backends

One interface, two implementations, selected by config. This is what makes
signpost runnable both on a laptop and in GitHub Actions.

```go
// Backend is a schema-constrained text generator. Both implementations are
// first-party code over the standard library.
type Backend interface {
    // Complete sends a prompt plus a JSON Schema and returns validated JSON.
    Complete(ctx context.Context, req Request) (Result, error)

    // Actor is the OKF `generated.by` string, e.g. "signpost/0.1.0+gemma4-12b".
    Actor() string
}
```

**`inferd` (local).** For anything running on a machine we control: a developer
laptop, a self-hosted runner, or a cloud VM. Speaks the inferd IPC wire protocol
over a Unix socket or Windows named pipe — length-prefixed, type-tagged framing,
with `response_format` carrying the JSON Schema.

Implemented **against `docs/protocol-v2.md` in the inferd repo**, not against any
existing client's source. The protocol is the frozen contract and carries an
in-band `wire_version` that fails loudly on mismatch, which is precisely the
condition under which implementing to a spec beats vendoring a client — it
decouples signpost's release cadence from anyone else's. Zero marginal cost per
run, nothing leaves the machine, no network listener involved.

**`openai` (remote).** For anywhere we do not control the host: GitHub-hosted
runners, or any environment where a resident model is not an option. `net/http`
against any OpenAI-compatible `/v1/chat/completions` with
`response_format: {type: "json_schema"}` — which covers Bedrock, Anthropic,
vLLM, LiteLLM, and Ollama with one implementation and no SDK. Credentials come
from the environment; base URL and model id are config.

Bedrock is worth documenting precisely, because none of it is guessable and all of
it was verified live rather than read off a doc page:

- **The path is `/openai/v1`, not `/v1`.** `https://bedrock-runtime.<region>.amazonaws.com/openai/v1/chat/completions`
  serves; the `/v1` spelling answers `UnknownOperationException`.
- **`bedrock-runtime` rather than the newer `bedrock-mantle` endpoint**, even though
  AWS recommends mantle. The two are separately authorised — mantle gates on
  `bedrock-mantle:*` against its own `project/default` resource, bedrock-runtime on
  `bedrock:CallWithBearerToken` — and a role permitted to call one can be denied the
  other. bedrock-runtime is the surface an account with ordinary Bedrock access
  already has.
- **A bearer token replaces SigV4, which is what keeps the dependency list empty.**
  A Bedrock API key is not an IAM access key: it is minted from an IAM principal
  and sent as `Authorization: Bearer`, so `net/http` reaches Bedrock with no AWS
  SDK (ADR 0002).
- **No Amazon generative model is on this surface.** Every Titan text model there
  is now an embeddings model, and the Nova family supports Invoke and Converse but
  not Chat Completions. "Use a cheap Amazon model" is not an option on an
  OpenAI-compatible path.
- **Model ids carry no version suffix and reject `global.`** — `google.gemma-3-12b-it`
  works, the `:0`-suffixed and `global.`-prefixed forms return 400. Gemma's model
  card lists Geo and Global inference as unsupported. So the configured id is passed
  through verbatim; any normalisation would rewrite a working id into a 400.

**`none`.** Deterministic-only, and the default. Not an error state — a supported
mode, and the one most runs use. Inferring a backend from a stray environment
variable would mean a build that silently spends tokens and ships repository
content to a third party because something unrelated was set, so the semantic pass
is opt-in and nothing but explicit configuration turns it on.

Configuration is split, and [ADR 0011](adr/0011-configuration-file-format-and-location.md)
is where the line falls. *Which* model is a property of the repository, so it goes in
`.signpost.yml`. *Whether* to spend one is a decision each run makes, so `-semantic`
stays a flag. And the credential is read from the environment only — the file is
committed, and a format with a place for an API key is a format that eventually has one
in it, so the reader refuses `api_key` and `openai` by name rather than ignoring them.

```yaml
# .signpost.yml, at the repository root and nowhere else
backend: inferd            # inferd | openai | none (default none)
model:   google.gemma-3-12b-it   # passed through verbatim; stamped into generated.by
```

```sh
# the environment: the credential, and the endpoint that needs it
SIGNPOST_OPENAI_BASE_URL=... SIGNPOST_OPENAI_API_KEY=... signpost build -semantic
```

An earlier sketch of this section put `base_url` and `api_key` in the file as
`${SIGNPOST_OPENAI_API_KEY}`. ADR 0011 withdrew both the keys and the interpolation
syntax: an expansion syntax exists mainly to put secrets in files, and there is nothing
here that wants to expand one. So `${...}` anywhere in the document is now an error, not
a value — because the alternative is silent, `model: ${SIGNPOST_MODEL}` reaching the
backend verbatim as a model id and the resulting 400 saying nothing about the file.
`budget`, also sketched here, is refused for a different reason: nothing reads it yet,
and a key that looks configured and is not is worse than its absence.

**Fail-open, following thlibo's ADR 0006.** If the configured backend is
unreachable, signpost emits the deterministic bundle, records the skip in
`manifest.json` with what was lost, and exits 0. A broken model backend must never
break a merge.

The consequence is that a misconfigured backend is invisible during a build, which
is right for a build and wrong for someone trying to find out why their bundle has
no semantic pages. **`signpost model check` is where that question gets a straight
answer**: it sends one probe through the whole path — system prompt, wrapped
untrusted source, schema, response parse — and exits non-zero when the backend does
not work. It reports three separate facts, because a bare "ok" proves none of them:
that the schema held, that the model identified the source, and that it reported
the probe's embedded injection attempt as an observation rather than complying.

**Two findings from running that probe live, both of which generalise to the whole
semantic pass.** First, a `description` saying "one sentence" is a hint a model may
ignore, while `maxLength` is a constraint the sampler enforces — Gemma 3 answered
correctly and then elaborated until it hit the token cap. Second, a response that
hits the cap arrives as `finish_reason: "length"`, and it is reported as a failure
rather than parsed: a truncated claim is usually still valid JSON, and committing
one as complete is exactly the confidently-wrong output §4.6 refuses to emit.
Bounding the prose fields is the fix; raising the token cap only moves the cliff.

**A third finding, from the first live run of the full pass rather than the probe,
completes that picture and is the least guessable of the three.** Bounding the
prose field does not by itself guarantee a complete answer, because `maxLength` and
`finish_reason` do not cover the case where the *backend* satisfies the bound on the
model's behalf: an OpenAI-compatible server cut each over-long summary at exactly
the cap and returned the prefix with `finish_reason: "stop"`. Five of twelve modules
on this repository, each ending mid-word. Nothing in the response says it happened —
the JSON is well-formed, the length is legal, the stop reason is the normal one — so
the only place to catch it is in the text, by asking whether the sentence finished.
The general lesson is the one worth carrying: a constraint the *protocol* reports as
satisfied may have been satisfied by truncation, and a pass that commits prose to
somebody's repository has to check the prose rather than the protocol.

**Not every model on an OpenAI-compatible endpoint returns the constrained object
alone.** A model with a reasoning channel emits its trace into the same `content`
string as the answer — gpt-oss on Bedrock returns `<reasoning>…</reasoning>{…}`
under a strict `json_schema` — so the object is located rather than assumed. That
is documented recovery for a known model behaviour, not blanket permissiveness: the
result must still parse as an object. It is also why the default model is one
without a reasoning channel.

---

## 6. Commands

```
signpost build [path]              # deterministic pipeline; writes .signpost/
signpost build -semantic           # and summarise modules with the configured backend
signpost verify [path]             # conformance + link + staleness; non-zero on failure
signpost graph show [path]         # report structure: hubs, cycles, bridges, islands
signpost graph export -format ...  # mermaid, dot, graphml, or json
signpost view [path]               # serve the graph on 127.0.0.1 and open a browser
signpost view -static <dir>        # write that same page to a directory and exit
signpost init github               # scaffold the workflow that keeps a bundle honest
signpost init pages                # scaffold the workflow that publishes the graph
signpost model check               # prove the configured backend works; non-zero if not
signpost ask why "<question>"      # traverse the bundle and answer, citing pages
signpost ask path <A> <B>          # shortest typed path between two concepts
signpost hooks install             # optional local post-commit hook
signpost hooks uninstall           # remove it, leaving any other hook alone
signpost hooks run                 # what the hook calls; reports, never fails
signpost version
signpost update                    # replace this binary with a verified release
```

`ask why` and `ask path` are pure bundle traversal — no model, no network. They exist
so an agent can ask a question without loading the whole bundle into context.

### 6.0 What is grouped, and what is not

One rule decides where every verb goes: **a noun with more than one operation becomes a
group, a noun with one stays flat, and a group's own name is never an action.** So
`signpost graph` prints its subcommands and `graph show` does what a bare `graph` did in
v0.1.0, while `build` and `verify` stay flat because a bare `build` is the convention `go
build` and `cargo build` already set.

The reasoning, the alternative rejected (uniform noun-verb grouping), and the cost of the
third clause are in
[ADR 0012](adr/0012-a-group-name-is-never-an-action.md).

Dispatch is one recursive function over one command tree, with the top level modelled as
an unnamed group. Every level's help, unknown-command message, and exit code therefore
come from the same code, so a new group cannot describe itself differently from an
existing one. Two behaviours exist for the v0.1.0 rename and are meant to be deleted: an
old verb reports where it went, and a mistyped one gets a suggestion when exactly one
candidate is within a typo's distance.

### 6.0.1 The local hook reports; CI gates

`hooks install` adds a `post-commit` hook that prints one line when the committed bundle
has fallen behind the code. It is a convenience, and the division from CI is the whole of
its design:

- **The hook never fails a commit.** It is `post-commit`, so the commit object already
  exists when it runs, and it exits 0 whatever it finds. `signpost verify` in CI is what
  gates. A hook that could break `git commit` over an optional knowledge artifact gets
  deleted within a day and takes the tool with it.
- **It never rebuilds.** §8.0 keeps the bundle off branches so that two branches do not
  both regenerate `.signpost/` and make merges painful; a hook that rebuilt on commit
  would recreate exactly that, on every branch.
- **It is appended, never written over.** A `post-commit` hook already exists on a great
  many machines — git-lfs installs one — and the lines go on the end between markers, so
  `hooks uninstall` can remove signpost's and leave the rest.
- **It goes where git actually looks.** When `core.hooksPath` is set at any scope,
  including in `~/.gitconfig`, that directory is the only place git reads hooks from and
  `.git/hooks` is ignored entirely, so writing to `.git/hooks` would install a file that
  never runs. `hooks install` follows the resolved path and says so, including when the
  path is shared with every repository on the machine. The block is guarded to do nothing
  in a repository without a bundle, which is what makes that defensible.

Two checks, configurable, because they trade accuracy against a cost paid on every
commit:

| `-check` | Cost | Answers |
|---|---|---|
| `fast` (default) | milliseconds; two `git log -1` | is the newest code commit newer than the newest `.signpost/` commit |
| `verify` | ~1s on this repository | which pages would actually change, via `verify -as-of-bundle` |

`fast` is the default because a second on every commit is what makes a hook an
irritation, and its inaccuracy is one-directional and stated: a commit touching only
`LICENSE` moves the code commit without changing a page, and `fast` calls that behind.
`verify` calls the real `verify` rather than reimplementing the comparison, so the hook
cannot come to disagree with the gate. Precedence is `-check` > `SIGNPOST_HOOK_CHECK` >
`hooks.check` in `.signpost.yml` > `fast`, per [ADR 0011](adr/0011-configuration-file-format-and-location.md).

The four rules above are [ADR 0013](adr/0013-the-local-hook-reports-and-ci-gates.md),
which also records the measurements behind the two checks and why `-as-of-bundle` is the
accurate one — a strict `verify` after a code commit reported 38 problems here where
`-as-of-bundle` reported the 1 that was real.

### 6.0.2 `.signpost.yml` may only change a default

A repository states how it wants to be analysed in `.signpost.yml` at its root.
[ADR 0011](adr/0011-configuration-file-format-and-location.md) has the reasoning; three
rules are the whole of it.

**The root and nowhere else.** No user-level file, no `XDG_CONFIG_HOME`, no `-config`
pointing outside the tree, no walk upward. A config search path is how the same checkout
starts producing different bundles for two people, and §8.1's byte-stability requirement
does not survive that.

**A key may only change a default.** These eight, plus `hooks.check`:

```yaml
include_vendored: false      # analyse committed node_modules and vendor/
include_fixtures: false      # analyse test fixtures
ignore: [generated/**]       # additional .gitignore-syntax patterns
no_history: false            # skip the git pass
max_commits: 2000            # how far back the history pass reads
repo: example.com/org/repo    # the resource URI every page is stamped with
backend: none                # inferd | openai | none
model: google.gemma-3-12b-it # passed through verbatim
hooks: { check: fast }       # fast | verify
```

`repo` is the key that has to be here rather than in a workflow, and §8.0's fourth decision
is why: it names the repository being described, a workflow knows only the repository it is
running in, and on a fork those are different. This repository's own `.signpost.yml` holds
that one key and nothing else.

Anything that decides whether a check *fails* stays a flag — `-as-of-bundle`,
`-fail-on-cycle`, any future threshold — because a repository that can weaken its own gate
by committing a file is not gated. So is anything that is a property of one invocation
rather than of the repository: `-quiet`, `-o`, `-format`, `-verbose`, `-top`. Those keys
are **refused by name with a reason**, not ignored, because somebody who writes
`fail_on_cycle: false` believes they have configured something, and a tool that reads the
file, does the opposite, and exits 0 has told them their gate is what they asked for.

**One precedence order, no exceptions per key:** flag > environment > file > default. The
flag wins even when it is set to the zero value, which is why the reader asks
`flag.Visit` whether a flag was passed rather than comparing against zero —
`-include-vendored=false` and an absent flag carry the same value and must not carry the
same decision.

Unlike the manifest readers, which step over what they cannot interpret because they read
other people's files (ADR 0001), this reader is intolerant: **any** diagnostic is exit 2,
including the ones those readers tolerate. `include_vendored true`, missing its colon, is
a line the tolerant reader notes and steps over, and stepping over it would mean analysing
the repository the way the file said not to while reporting success. The file is also
repository content and is not exempt from the walk it configures — signpost does not get
to be invisible in its own map.

### 6.0.3 The binary replaces itself only from a verified release

`signpost update` exists for a symptom that does not look like its cause. A stale binary
does not report a version; it reports `signpost: unknown command "view"`, which reads as a
missing feature or a typo. §6's banner and `version` made that answerable — and then left
the reader with a tool they now know is old and a README to go and find.

It adds no distribution channel. The artifacts are the GitHub Releases that
`.github/workflows/release.yml` publishes, which is what `install.sh` and `install.ps1`
already read, and `internal/selfupdate` performs the same transaction in the same order so
there is one place the verification rules live. The rules, in full:

- **Three refusals, none of them a warning.** No `checksums.txt` published, the platform's
  archive not listed in it, or a digest that does not match — and nothing is written. Each
  is a separate error because each means something different: a broken release, a partial
  publish, or a substituted archive. Verification happens *inside* the download, so no
  caller can be written that unpacks first and checks second.
- **The release source is hard-coded.** No flag or environment variable names the host. A
  configurable update source turns one mistyped hostname into arbitrary code execution as
  the user.
- **The tag from the network is validated before it enters a URL.** It arrives in a
  `Location` header, which is remote input concatenated into a download path.
- **Nothing runs unless it is typed.** No auto-update, no background check, nothing on a
  timer. A build that passed this morning and fails this afternoon with no commit between
  them is the hardest kind of failure to be told about.
- **No privilege escalation.** An install needing elevation fails with the permission error
  and points at the installer.
- **Write-then-rename in the target's own directory**, so an interrupted update leaves a
  working binary and a running process is never written into. Symlinks are resolved, so a
  version manager's link keeps pointing at the binary it manages.

The asset naming is a contract between two files that cannot import each other, and a
disagreement is invisible at run time — a renamed asset is a 404, blamed on the network.
So a test reads `release.yml` and derives the expectation from it, for all six published
platforms rather than the one the test runs on.
[ADR 0033](adr/0033-the-binary-replaces-itself-only-from-a-verified-release.md) records the
decision, including what this does *not* verify: that the release was built from this
source by this workflow, which nothing in a release attests to today.

### 6.1 Human-review preservation

The mechanism that makes the bundle compound rather than churn:

- Generated prose lives between `<!-- signpost:managed:NAME -->` markers.
  Everything outside them is human territory and is copied through verbatim.
- A `verified:` block added by a human is preserved across runs.
- When the underlying `resource` sha changes, `verified` is **downgraded** rather
  than silently kept — the page is re-marked machine-confirmed, gains
  `signpost_status: stale-verification`, and the downgrade is recorded in
  `log.md`, so a reviewer knows to look again. The mark is on signpost's key, not
  OKF's `status:`, per [ADR 0021](adr/0021-track-the-published-spec-and-never-overload-its-keys.md).
- Human-authored `## Notes` sections are never regenerated, never reordered, never
  reflowed.
- A page whose concept is gone is deleted only when nothing on it came from a person
  (§4.6). Anything a human touched — a note, a rewritten heading, a `verified:` block,
  an unrecognised frontmatter key — makes the page theirs to remove, and the run says
  so instead.

### 6.2 What signpost does not write

signpost writes `.signpost/` and nothing else. It does not write `AGENTS.md`,
`README.md`, or `ARCHITECTURE.md` — those encode human intent and team
convention, and a generator overwriting them is how teams learn to distrust
tooling. `signpost build -suggest-agents-md` prints a proposed stub to stdout for a
human to take or leave, and that is the extent of it: it writes nothing — not
`AGENTS.md`, not even the bundle — so the `>>` that appends it is the human's to type.

The other half of that boundary is a build that says when nothing points at the bundle.
A bundle no instructions name is the one failure a green build cannot show — every page
correct, `verify` passing, and no agent ever opening it — so a build whose `AGENTS.md`,
`CLAUDE.md`, `.cursorrules`, `.github/copilot-instructions.md`, and `README.md` all fail
to name `.signpost/index.md` says so on stderr and names the flag that fixes it. The
index page rather than the directory: `.signpost/` appears in prose that is not a pointer,
and matching it reports a repository as pointed-at while no agent has been given anywhere
to start.

### 6.3 YAML

Asymmetric, deliberately.

**Writing** uses a hand-written emitter over a narrow subset — scalars, flow
mappings, block sequences of flow mappings. Trivial to emit and byte-stable, which
a general-purpose library does not guarantee across versions. Byte-stability is a
hard requirement (§8.1), so this is worth owning.

**Reading** uses the hand-written tolerant reader in `internal/manifest`
(ADR [0001](adr/0001-hand-written-tolerant-yaml-reader.md)). An earlier draft of this
design specified `gopkg.in/yaml.v3` here, on the argument that parsing frontmatter a
*human* has edited is where hand-rolling gets risky — people add comments, anchors, block
scalars, odd quoting, and multi-line strings, and silently mangling someone's `verified:`
block is the exact failure that destroys trust in the tool. That argument stands, and the
reader is built to it: anchors, aliases, merge keys, both quoting styles with escapes, and
block scalars with all three chomping modes are supported, and an unreadable region is
reported as a diagnostic rather than dropped.

What changed the decision is that the same reader has to read Helm templates, which are not
YAML at all — a conforming parser rejects the whole document, so the library would have left
the deployment surface unread. Owning one tolerant reader covers both cases; a library plus
a hand-written template path would have been two.

### 6.4 Line endings are a transport encoding

signpost writes LF, always, and normalises CRLF to LF when it *reads* a page back.
The write side follows from byte-stability (§8.1): the bundle's line endings are
signpost's to choose, and choosing one is what lets a Windows contributor and a
Linux runner produce identical bytes. The read side is less obvious and was a
defect before it was a rule.

Every claim signpost makes about an existing bundle is a byte comparison against
freshly generated content. So a checkout that materialised the bundle with CRLF —
git's `core.autocrlf=true`, which many Windows installs select by default —
differs from a rebuild on every line of every page, and three things go wrong at
once:

- **`verify` reports every page as out of date.** The bundle is byte-identical to
  what a build would produce, so the finding is false *and* the remedy it names
  does not work: a rebuild writes LF, git converts it back on the next checkout,
  and the gate stays red. On a pull request that changed nothing, CI fails on the
  whole bundle.
- **`build` rewrites every page each run**, so nothing is ever `unchanged` and a
  filesystem watcher fires on the entire bundle.
- **`build` reports human notes nobody wrote.** `HumanText()` differs by its line
  endings, so a bundle with no `## Notes` at all reports "N page(s) had human
  notes, carried across". That is the worst of the three: the count exists to
  tell someone their writing survived, so inventing it teaches them the number
  means nothing.

Normalising on read fixes all three, and it is the right layer rather than the
convenient one. A `.gitattributes` pinning `* text=auto eol=lf` also prevents it,
and this repository ships one — which is precisely why signpost's own CI could
never have caught this. The bug is invisible in a repository already configured
against it, and **every** repository is unconfigured on the first day signpost
runs in it. Fixing it in the tool means a bundle is correct before anyone
configures anything; recommending `.gitattributes` alone would mean the tool is
wrong by default and every user has to know why.

Two limits keep this from becoming an edit. A lone CR is left alone: no git
conversion produces one, so a bare CR that reaches the reader is a byte somebody
put in the file deliberately. And signpost normalises to *compare*, not to
convert — a page whose content matches is not rewritten, so a file keeps whatever
line endings its owner's git chose. That preserves §6.1's invariant exactly: human
text is never modified. It is decoded on read, the same way a UTF-8 BOM from a
Windows editor is tolerated and stripped. What a person wrote is their text; the
bytes their platform chose to store it in are not, and treating the second as the
first is what produced all three bugs above.

Pinning LF in `.gitattributes` is still worth doing in a repository that commits a
bundle, for a reason that has nothing to do with correctness: it keeps the diffs
readable. It is a recommendation, not a prerequisite.

---

## 7. One repo: generator and viewer

> Recorded as ADR [0008](adr/0008-the-viewer-lives-in-this-repository.md), superseding
> [0006](adr/0006-generator-and-viewer-are-separate-repositories.md), which put the
> viewer in a second repository.

The generator and the visual are separate products with different audiences and
different risk profiles, but they do not need separate repositories to stay apart.
What has to stay off the merge path is the *deploy*, and that is a property of the
workflow topology: `pages.yml` has its own trigger, its own concurrency group, and
its own permissions, and is never a required check.

The dependency tree the split was protecting against is not needed. Measured on
this repository, the graph is 23 nodes and 27 edges across three node kinds — a
node-link view over that is hand-written SVG, not a graph library. So the viewer
lives in `site/` with **zero JavaScript dependencies**, and the generator's
posture is unchanged.

### 7.1 `signpost` — the generator

Go binary. Runs locally and in CI. Emits the OKF bundle plus graph exports.
Markdown and JSON only, and nothing to deploy or keep running. This is the repo
that gates merges, so its dependency list stays short and every entry is
justified.

`view` is the one command that opens a socket, and it is a viewer rather than a
service: it holds a loopback listener for as long as you are looking at the page
and writes nothing anywhere. There is still nothing to deploy, nothing to
operate, and no state — see §7.3. `view -static` writes that page to a directory
you name instead of serving it, which is a deploy's input and not state either
(§7.4).

Inline in the bundle: **Mermaid** graphs in `index.md` and each cluster page.
GitHub renders Mermaid natively, so a tech lead clicks `.signpost/index.md` and
sees the module graph with nothing installed and no site deployed. Mermaid
degrades past a few dozen nodes, so it is capped — clusters and top hubs at the
root, members on cluster pages. It is the zero-setup skim, not the real visual.

Exports for anyone with their own tooling:

```
signpost graph export -format graphml   # Gephi, yEd, Cytoscape
signpost graph export -format dot       # Graphviz
signpost graph export -format json      # the graph, for the viewer and for scripts
```

**GraphML is the interop win.** It carries typed edges, confidence levels, cluster
assignments, and arbitrary node attributes, and it opens in tooling teams already
have and already trust. For a lot of internal users that is the whole visual story
and no site needs to exist.

The structural findings — hubs, cycles, bridges, orphans, doc/code islands, merge
gates — are written as **text** in `index.md`, because that is what an agent
consumes. A picture is for the human skim; the prose is the load-bearing artifact.

The merge-gate finding is the line that answers §4.1's "what gates exist", and it
states a fraction — "11 of 13 CI jobs" on this repository — because a count of CI
jobs is not a count of the checks a change meets. It reports what the fact says and no
more: a job runs on a pull request or on a push to the default branch. A job counts
once even where a matrix expands it into several checks, since the matrix values are
not in the tree; for the same reason a job whose `name:` interpolates one is titled by
its key rather than after the expression. Which of those is *required* is branch
protection, which is repository configuration and not in the tree, so the
finding says so — this repository's `pages.yml` gates by that definition and §7 makes
it never a required check. A repository whose every workflow runs on a schedule or a
tag has automation and no gates, and one with no CI at all has neither: both are
reported rather than left blank
([ADR 0032](adr/0032-order-is-drawn-only-where-a-file-declares-it.md)).

A finding with nothing to report states that it found nothing, rather than being
omitted. `graph show` does the opposite and both are right: a terminal is scrolled
past, and in a committed file a section that vanishes when clean is
indistinguishable from one a build failed to write
([ADR 0030](adr/0030-a-finding-states-its-own-absence.md)).

### 7.2 `site/` — the landing page and the viewer

Hand-written HTML, CSS, and JavaScript, published to **GitHub Pages** by
`pages.yml`. Two pages sharing one stylesheet and one top bar: the landing page,
and `graph.html` — a browsable node-link view of this repository's own graph. No
install, nothing to run, and a URL a person can paste into a review.

The seam is `graph.json`, produced by `signpost graph export -format json` in the deploy
job and **not committed**: it has no value without the page that reads it, and a
committed copy would be a second artifact that can go stale.

The site is served from **`signpost.md`**, and `site/CNAME` is a required part of
the published artifact rather than a convenience. Pages reads the custom domain from
that file on every deploy, so a `site/` published without one *clears* the domain in
the repository settings — the failure is a deploy that succeeds while moving the site
back to the `github.io` address. Anything that rebuilds the artifact has to carry the
file along. The `github.io` address keeps resolving, which is why both pages carry a
`rel=canonical` naming the apex: two hostnames serving the same bytes is the case
that tag exists for.

The constraints, which are the whole reason this can live here:

- **Zero JavaScript dependencies.** No `package.json`, no lockfile, no npm or
  pnpm anywhere in this repository. The layout is a hand-written
  Fruchterman-Reingold pass followed by an overlap pass that separates nodes by
  the footprint their label occupies rather than by the distance between their
  centres, inside a frame whose height is computed from those footprints so the
  separation pass has somewhere to move them to; the rest is filtering, search, a
  detail panel, and zoom and pan on an SVG transform group. If the viewer ever genuinely
  needs a graph library, that is a new ADR and the two-repository split becomes
  the live option again.
- **Not in the merge path.** `pages.yml` is a separate workflow with its own
  trigger and permissions, and is never a required check. A viewer that breaks
  fails a deploy, not a merge, and the bundle is still correct.
- **The injection surface is contained by rule.** The known vulnerability class is
  interpolating untrusted repo strings — paths, module names, authors — into a
  page. The rule for `site/`: escape everything, **no `innerHTML`**, a strict CSP
  (`default-src 'none'`, no `unsafe-inline` in `script-src`), no network egress
  beyond the same-origin `graph.json`, and that JSON treated as untrusted input
  even though we generated it.

**This directory is published by this repository's own deploy and by nothing else.** The
scaffolded Pages workflow (§8.3) writes the viewer with `signpost view -static` (§7.4)
instead, because a repository that adopted signpost has no `site/` and no command produces
one — the assets live in the binary. Ours keeps uploading `site/` because it publishes a
landing page the exporter knows nothing about and carries a `CNAME` the exporter must not
invent, so the parity test between the two files asserts that difference rather than
tolerating it. Converging them would mean teaching the exporter about this project's
landing page, which is the wrong direction: the exporter's subject is the viewer, and the
landing page is content.

**Search narrows the graph; it does not replace it.** A box above the kind and edge
filters matches a node's name, its path, and the files inside it, and what survives is
still the diagram in the positions it already had — the layout is solved once at load and
is never recomputed, so typing removes nodes rather than rearranging them. Recomputing per
keystroke would move the node whose name is being typed, which is the node the reader is
watching. Descriptions are excluded on purpose: they are generated prose, so a search for
`files` would match nearly every module, and the page says so where a query matches
nothing rather than leaving a reader to infer it. One consequence is structural — a node
hidden by either the kind filters or the search is hidden by the same predicate, because an
edge is drawn only when both of its ends are and a second copy of that rule is how a line
gets drawn into empty space.

The viewer is optional. A team can adopt the generator, read `index.md` and the
Mermaid graphs in GitHub, open the GraphML in yEd, and never deploy a site.

### 7.3 `signpost view` — the same viewer, on any repository

`graph.html` shows *this* repository, because the deploy job runs the export against
this tree. Everyone else's repository is the interesting one, and until `view` the
only way to see it was to install a graph tool and open a GraphML file.

`view` analyses the repository, binds `127.0.0.1`, serves the graph, and opens the
default browser. It runs until interrupted. The assets are `site/`'s own bytes via
`go:embed` — one viewer, not a fork of one — and `graph.js` is unchanged except for
reading the file-link base off a `data-` attribute instead of hardcoding this
repository's.

Three decisions carry the design, and they are recorded in
[ADR 0018](adr/0018-view-serves-a-repository-over-loopback.md):

- **No artifact and no state.** Nothing is cached, nothing is written to the
  repository, and there is no `graph.json` on disk. A `view` that wrote one would
  create exactly the stale second artifact §7.2 declines to commit, from the one
  command whose output is transient. It also does not require `build` to have run:
  the graph comes from this invocation, which is the case where somebody most wants
  to look at the structure *before* deciding to commit a map of it. `-static` is the
  one exception and it is a narrow one — see below.
- **The graph is a snapshot taken before the listener opens.** Nothing re-analyses on
  a request and nothing watches the tree. A viewer that re-read the repository would
  change while somebody was reading it, and one that re-analysed per request would
  spend seconds of CPU on a reload — a full pass over this repository's 185 files takes
  about five seconds. The page states which commit it describes and what was already out of step
  when it started, including a note when the committed bundle is behind the tree.
  Restarting is the refresh.
- **Loopback, and the `Host` header is checked.** The page lists every module and
  every file, which is a private repository's structure. `127.0.0.1` is a literal in
  the code rather than a configurable field, so no flag, config key, or environment
  variable can widen it. That alone is not sufficient: a page the user is browsing can
  issue requests to loopback, and the same-origin policy stops it *reading* the
  responses only because no CORS header is set. DNS rebinding defeats that — an
  attacker's hostname re-resolves to `127.0.0.1` and the browser treats the response as
  same-origin with their page — so a request whose `Host` is not a loopback name is
  refused before any repository content reaches the response.

Three smaller things follow from those. The routing is an explicit map from path to
bytes with content types written as literals, not a `FileServer` over the embedded FS:
`FileServer` serves directory listings, and it resolves types through
`mime.TypeByExtension`, which on Windows reads the registry — so a machine with an odd
`HKCR\.js` can serve the viewer as something the browser declines to execute. The
served document's CSP omits the webfont origins `graph.html` allows, because a local
tool that fetched a font would tell a third party which repositories you open and would
render in a fallback face on a machine with no route. And the URL is printed *before*
the browser is opened and before anything is served, which is the difference between a
command that works over SSH and one that appears to hang.

`-port` distinguishes a port you named from the default you did not: a named port that
cannot be bound is an error, because you named it most likely because something else is
configured to reach it, and quietly serving a different one satisfies the command and
not the intent. The default falls back to whatever is free and says so.

### 7.4 `signpost view -static` — the same viewer, as files

`-static <dir>` writes the five things `view` serves — the page, the stylesheet, the
script, the icon, and `graph.json` — into a directory and exits. It is how a deploy
publishes the viewer, and it is the reason §8.3's scaffold can exist at all: until it
did, the viewer was committed here or embedded in a binary that only bound ports, so a
Pages workflow handed to another repository had nothing to upload.

**The exception to "writes nothing anywhere" is narrower than it looks, and
[ADR 0029](adr/0029-the-viewer-is-written-by-the-run-that-publishes-it.md) records why
it is not a reversal of §7.2.** What is declined there is a *committed* copy of derived
data — an artifact that outlives the run that made it, in a project whose central claim
is that stale fails loudly. These files are written and uploaded by the same run, so
there is no interval in which they exist and the tree has moved on. The argument §7.2
makes about `graph.json` extends to the page: it has no value without the graph beside
it, and they are produced together or not at all.

Three properties are decisions rather than implementation:

- **The files come from the map the server routes, not from a list.** `WriteStatic`
  reads the same `assets()` the HTTP handler does, and names each file after its route
  with `/` becoming `index.html`. A fifth asset added to the server and forgotten in the
  export cannot happen, and the test asserts against `assets()` rather than against
  filenames so that it stays true.
- **One document, with the address as the switch.** `view.html` renders the local
  address and the ctrl-c line only when there is an address, so a published page does
  not claim to be served from somebody's laptop. A second template would double the
  drift surface §8.2's parity discipline exists to catch.
- **The `<meta>` CSP is the only CSP an exported page has.** `Serve` sets the header, and
  the header is the copy that binds; a static host sends whatever it likes and Pages
  sends no CSP at all. §7.2's hardening rule therefore rests on a tag inside the
  document once the page is published, and a test asserts it survives the export.

`-static` refuses `-port` and `-no-open` rather than ignoring them, with exit 2: it does
not listen, so neither can be honoured, and a dropped flag leaves somebody believing
something happened that did not.

---

## 8. CI

Two workflows plus a PR check.

**`signpost.yml`** — on push to the default branch. Runs `signpost build` and
`signpost verify` on a hosted runner. No model, no infra, on the order of seconds
— there is no flag to opt out of the semantic pass because it is off unless asked
for (§4.5). Commits `.signpost/` only when the diff is non-empty.

Loop guard, all three: `paths-ignore: ['.signpost/**']`, a skip when the actor is
the bot, and `[skip ci]` on the bot commit.

**`signpost-semantic.yml`** — on schedule and `workflow_dispatch`. Runs
`signpost build -semantic`, which is the only place either of §4.5's two gates is
opened: this workflow configures a backend and passes the flag, and `signpost.yml`
passes neither. Same binary, same schema, different `generated.by`. On a
self-hosted runner the backend is inferd over IPC with the model already warm; on
a hosted runner it is any OpenAI-compatible endpoint, reached with a key from
Actions secrets.

Three properties of that job are not incidental. It shares `signpost.yml`'s
**concurrency group**, because both commit to the default branch and two pushes to
`.signpost/` in flight at once is a lease failure at best. The **summary cache is
restored and saved by prefix key**, which is what makes the pass affordable and
what makes it produce no diff most weeks — `.signpost/cache/` is gitignored, so
the Actions cache is the only place it persists. And a **missing backend is a skip
with a message**, unlike the binary, which exits non-zero: a fork inherits this
schedule and has no secrets, and a red weekly cron in somebody else's fork is
noise they did not ask for. A backend named but with no credential *does* fail,
because that is a misconfiguration rather than a choice.

Bedrock over OIDC is **not** wired up, and the reason is specific rather than a
matter of effort: the OpenAI-compatible surface authenticates with a bearer token
gated on `bedrock:CallWithBearerToken`, which the usual OIDC role pattern does not
grant, so a role provisioned that way cannot make the call. Long-lived AWS access
keys in Actions are not an acceptable substitute. Pointing the backend at another
OpenAI-compatible endpoint keeps AWS credentials out of CI entirely, which is what
the shipped workflow does.

**PR check** — `signpost verify` runs on pull requests and fails when the bundle
is stale relative to the diff. Non-zero exit, surfaced in the PR.

### 8.0 The bundle must not create merge conflicts

Committing generated files to a repository with parallel branches has an obvious
consequence that the first draft of this design did not address: two branches that
both touch `.signpost/` conflict, and a knowledge tool that makes merges painful
gets deleted. Three decisions handle it, in order of how much they matter:

1. **The bundle is not built on branches.** `signpost.yml` runs on push to the
   default branch only. Feature branches never regenerate the bundle, so the
   common case produces no conflict at all — the branch simply does not touch
   `.signpost/`. The PR check *verifies*; it does not write. This is the whole
   reason the artifact is generated post-merge rather than per-PR.
2. **One page per concept, so conflicts stay small and mergeable.** The bundle is
   many small markdown files rather than one large graph blob. Two branches that
   genuinely both regenerate it collide only on the pages they both changed, and a
   markdown page with sorted frontmatter and a managed prose region is something a
   human can resolve by reading it. A single serialised graph file is not — which
   is why a tool that ships one needs a custom merge driver to union it. Choosing
   many small files is choosing not to need that driver.
3. **Regeneration is the tiebreaker, and `index.md` is generated last.** Any
   conflict in a generated region is resolvable by discarding both sides and
   re-running `signpost build` at the merge commit, because the deterministic pass
   is a pure function of the tree. That is documented as the remedy. The one thing
   this must never do is resolve a conflict inside a human region — `## Notes` and
   `verified:` blocks conflict like any other hand-written text and are the
   author's to reconcile.

4. **The repository's name comes from the repository, not from the run.** Every page's
   `resource:` is `git://<repo>@<sha>`, and `<repo>` is a thing a checkout cannot know —
   a remote URL is a checkout detail and a fork's remote names the upstream — so it is
   asked for. Asking the *workflow* is what (1)–(3) do not cover: signpost's own CI
   passed `-repo "github.com/${GITHUB_REPOSITORY}"`, which names the repository the job
   runs in. A fork's own CI therefore restamped every page carrying a resource, over
   identical source at an identical commit, and the fork's first sync from upstream
   conflicted inside `.signpost/` — not two branches writing the bundle, but two
   *repositories* writing it with different answers to what the repository is called. So
   the name belongs in `.signpost.yml`, where it is committed and travels with the clone.
   A fork that means to publish under its own name changes that line, in a diff that says
   so. The flag stays, and still wins (ADR 0011), for the caller who is describing a tree
   that is not a checkout of the thing being named.

We ship no custom git merge driver in v0.1. A merge driver requires every
contributor to configure it locally (`.gitattributes` names it; only
`git config merge.*.driver` activates it), and an unconfigured contributor
silently gets default behaviour — so a driver is a fragile place to put
correctness. If real usage produces conflict pain that (1)–(3) do not cover, the
driver is the v0.3 answer, with "regenerate at the merge commit" as the always-available
fallback that needs no local setup.

**Dependency governance**, enabled on the first commit, because the posture in §2 is
a commitment to remediate rather than a claim to have no exposure:

- **Dependabot** — security alerts and automated patch PRs. Go modules and GitHub
  Actions only; there is no npm ecosystem to cover, because the viewer has no JS
  dependencies (§7.2).
- **Renovate** — grouped non-security updates on a weekly cadence, so routine
  bumps do not arrive as a stream of individual PRs.
- **CI dependency gate** — a new *direct* dependency fails the build unless an ADR
  accompanies it. This is the mechanism that keeps the list short enough for
  bumping to stay routine; without it, "few dependencies, each justified" decays
  within a quarter.
- **`govulncheck` in the gate**, per the standing scanner policy, plus `gosec`,
  `go vet`, and `staticcheck`.
- GitHub Actions **pinned by commit SHA**, not tag.

### 8.1 Determinism is a hard requirement

> A consequence of committing the bundle, recorded as ADR
> [0005](adr/0005-commit-the-bundle-to-the-repository.md) along with the merge-conflict
> handling in §8.0.

Because CI commits the bundle, a run that produces different bytes for the same
commit produces commit churn, and commit churn kills adoption faster than any
missing feature. So:

- sorted iteration everywhere; no map-order dependence
- clustering deterministic by construction — no seeds, no randomised restarts
  (§4.4); a seeded algorithm is reproducible only as long as nobody touches the
  seed, whereas sorted traversal is reproducible because there is nothing to touch
- temperature 0 on every model call
- semantic output cached by content hash, so unchanged input is never
  regenerated and therefore cannot drift
- byte-stability asserted in `verify` and in CI

This is also why the semantic pass runs on a schedule rather than per-merge: the
deterministic pass is genuinely deterministic, and the semantic pass is only
stable because of the cache.

### 8.2 The workflow is scaffolded, and the scaffold is tested against ours

`signpost init github` writes `.github/workflows/signpost.yml` and `.signpost.yml`
into another repository. Before it existed the instruction was to copy this
repository's workflow by hand, which meant every adopter transcribed the three loop
guards and the strictness split of §8.0 — and the reasons for those live in comments,
so a partial transcription looks like a working file.

**The template is compared to the workflow this repository runs, on every build.**
That test is the reason the template can be trusted at all: a scaffold that drifts is
worse than none, because it ships advice we do not follow and the divergence appears
as somebody else's gate behaving differently from ours, which is the hardest kind of
bug to be told about. It compares structure rather than bytes, since one difference is
intended and must be — this repository builds signpost from its own source, because a
repository that analyses itself has to use the binary it currently contains, and a
scaffolded repository installs a pinned release. The anchors are asserted in *both*
files, so removing one from ours fails as a stale expectation instead of passing
quietly.

**Embedded in the binary, not pulled from a registry.** The alternative considered was
publishing the templates as a tagged OCI artifact in GHCR and having `init` fetch the
release matching the binary. Declined on four grounds. The templates do not version
independently of the binary — a workflow that installs `vX` is only correct for `vX`
— so decoupling buys nothing and adds a way for the two to disagree. It would make
`init` the only command that touches the network, in a tool whose whole posture is
that it does not. It would put an unauthenticated fetch in the path of a command
writing a file that requests `contents: write`, which then needs signature
verification, which is a dependency §2 says we cannot patch ourselves. And `site/` is
already embedded, so the bytes would be carried twice.

**Preview by default; `-y` writes.** The output requests `contents: write` and pushes
to the default branch, so typing the command correctly must not be enough to install
it. A prompt was rejected rather than overlooked: a prompt needs a terminal, so it
either behaves differently under CI and in a pipe or it needs TTY detection tests
cannot exercise. Printing and stopping is the same guarantee with no hidden state.

**Nothing is overwritten, and the refusal covers both files.** A plan that skipped the
blocked file and wrote the other would leave a repository with a config file and no
workflow — a repository whose bundle silently stops being rebuilt, which is the exact
failure the command exists to prevent. It exits 0, because the files being present is
a state somebody can legitimately be in and a scaffold that fails when the thing
already exists is one every caller has to guard.

**The install step verifies what it downloads.** The archive and the release's
`checksums.txt` are fetched directly and checked with `sha256sum -c` before anything
is unpacked, in both jobs. Piping `install.sh` into a shell was the first version and
is wrong for a reason worth recording: `install.sh` verifies the archive, but the
script doing the verifying would itself have arrived unverified over the network,
inside the one job holding `contents: write`. A checksum is worth nothing when the
code comparing it is fetched the same way. The asset name is written out rather than
detected because `runs-on` is fixed, and a test ties the two together so a change to
one cannot silently outlive the other.

### 8.3 The Pages deploy is scaffolded too, and publishing is somebody else's decision

`signpost init pages` writes one file, `.github/workflows/pages.yml`. It inherits
everything §8.2 established — embedded, previewed, `-y` writes, never overwriting, and
compared against the workflow this repository runs — and it publishes the viewer with
`signpost view -static` (§7.4) rather than uploading a committed copy of it.

**It requests `contents: read` and writes nothing to the repository.** That is the
property which makes it safe to hand somebody: `signpost.yml` needs `contents: write`
because it commits a bundle, and a deploy that acquired the same permission would be a
token with push access in a job whose entire purpose is to publish to the internet. The
parity test asserts the absence — no `contents: write`, no `git push`, no `git commit` —
because that is not the kind of thing to notice in review a year from now.

**Nothing signpost writes can enable Pages, and that is the design rather than a
limitation.** `actions/configure-pages` will only switch it on when given a token other
than `GITHUB_TOKEN`, so the scaffolded workflow is inert until somebody sets
Settings → Pages → Source to "GitHub Actions". That act is the consent.

The obvious alternative was to have `init pages` call `repos/{owner}/{repo}` and refuse
unless it could confirm the site would be private. Declined for the two reasons in
[ADR 0029](adr/0029-the-viewer-is-written-by-the-run-that-publishes-it.md): it would make
`init` the only command that touches the network, against §2 and §8.2, and it would gate
a step that was never the one that publishes.

**So the consequence is stated instead — in the preview, in the confirmation, in
`init pages -h`, and in the file's own comments — and asserted in all four.** What gets
published is every module name, every file path, and the ownership signals read out of
git history. Whether that URL is private follows GitHub's rule and not the intuition:
publishing a site privately requires GitHub Enterprise Cloud, and access control applies
only to project sites from private or internal repositories owned by an *organization*. A
personal account's private repository publishes a site anyone can read, and an
organization site cannot use access control at any tier.

Two things the scaffolded workflow deliberately omits, both places where copying ours
would have been wrong:

- **No `site/CNAME` check.** Ours has one because publishing `site/` without that file
  *clears* the custom domain in settings (§7.2) — a deploy that succeeds while moving the
  site back to the `github.io` address. An adopter has no apex domain to protect, and a
  check for a file that will never exist is a step that fails on the first run.
- **No `paths:` filter, for §7.2's reason.** The graph is derived from the whole tree, so
  any commit can change what gets published, and the bundle-rebuild commit cannot serve as
  the trigger because it carries `[skip ci]`.

What it does add is a guard ours does not need: after the export it counts nodes in
`graph.json` and fails below one. A viewer fed an empty graph renders an empty frame and
looks like it works, so the deploy fails rather than publishing a page that says nothing.
The check is in the workflow rather than in `WriteStatic`, because an empty repository is
not an error in a command whose job is to describe whatever it was pointed at.

---

## 9. Relationship to adjacent work

| Tool | Role | Direction |
|---|---|---|
| `codebase-agent-readiness` | Read-only consultant. Predicts agent friction, writes nothing, hands the tech lead a stack-ranked work plan. | signpost is the contractor for part of that plan. The bundle becomes an input signal to the next scan, and `overall_stability` is the measure of whether signposting actually helped. |
| `codeatlas` | Code intelligence service. SCIP symbols, call graphs, BM25 retrieval, co-change, over GraphQL/MCP. | **Pattern reference**, and an optional enrichment source when an index happens to exist. Never a dependency, and no code is lifted from it. |
| `thlibo` | Compresses tool output at the agent boundary. | **Pattern reference only.** Validates the architecture — deterministic handling first, local model for the residue only — and the fail-open-when-the-backend-is-down discipline. signpost implements its own inferd client against the protocol spec. |
| `inferd` | Local inference daemon. Warm model, IPC, schema-constrained sampling. | A backend, for local and self-hosted execution. |
| `microsoft/agentrc` | Scores AI-readiness across nine pillars on a five-level maturity model, then generates instruction files through the Copilot SDK. | Overlaps on **signals**, not on output. See §9.1. |

The division of labor is deliberate: readiness *measures*, signpost *writes*,
codeatlas *serves*. Merging any two of them would produce a tool that is worse at
both jobs.

### 9.1 Repository-practice signals belong in the bundle

The nine pillars a readiness scorer looks for — a linter config, a declared build
command, a declared test command, docs, a lockfile, a formatter, observability
libraries, `LICENSE`/`CODEOWNERS`/`SECURITY.md`/Dependabot, and agent
configuration — are almost entirely **facts already extracted by §4.1**. Workflow
reading infers the gates, `repo.go` reads CODEOWNERS and ADRs and Makefile
targets, the dependency readers see whether an OpenTelemetry library is present,
and discovery classifies docs and lock files. An earlier draft of this section
routed those facts to a separate tool. That was wrong: the extraction is already
done here, and a fact an agent needs is a page in the bundle regardless of which
tool first thought to look for it.

So v0.1 emits them, as ordinary graph content with ordinary provenance: a
`practices` page recording what the repository declares about how it is built,
tested, gated, and owned — and, more usefully, what it does not. "No test command
is declared for `internal/export`" is exactly the kind of thing an agent should
know before it offers to add a test, and it is a fact with a file and a line
behind it.

**Shipped** in `internal/practice`, rendered into `.signpost/practices.md` and
linked from the index. It is deterministic — it reads manifests the discovery pass
has already opened and asks no model anything — so it runs on every `build` and
every `verify` rather than behind a flag. That it runs on *both* is a correctness
requirement rather than a convenience: `verify` works by rendering the bundle the
current tree would produce and comparing, so a page one command emits and the other
does not is reported as an orphan page plus a changed index, neither of which names
the cause.

Two things the implementation makes explicit. Each pillar reports **both ways** —
found, with the file that grounds it, or not found — because a page that only ever
reported presences would render a missing security policy identically to one it
never looked for, and silence is the failure this section is written against. Where
both ways is not enough, the third is stated rather than folded into one of them:
a manifest declaring no dependencies is not an unpinned one, and reporting it as
unpinned told a reader two builds could resolve different versions of nothing. And
the CI gate distinction is **per workflow, not per job**: GitHub's required-checks
operates on job names, any of which can be selected, so every job in a
`pull_request` workflow can block a merge, and only a schedule-only workflow runs
outside that gate.

**What signpost does not emit is the score.** A 1–5 maturity level is a rubric,
and a rubric is an opinion that has to be defended, re-tuned, and argued about
per repository, with no durable truth underneath it. It also invites the failure
this whole design is built against: a repository at "level 2" reads as *measured*
when it has only been *judged*. The finding is the durable artifact; the ranking
belongs to whoever is deciding what to work on. This is the same reason §12
reports cycles and bridges rather than a "code health" number.

Two constraints make this ours to do rather than a case for adopting the other
tool. Its detection is npm-shaped — build and test commands are looked for in
`package.json`, lockfiles in the npm/pnpm/yarn/bun set — so a Go, Rust, or Python
repository scores badly for reasons that are artifacts of the scorer rather than
facts about the repository, and signpost is polyglot at the extractor layer by
construction. And it carries nine direct npm dependencies (including React and a
terminal UI framework) plus a vendor-specific generation SDK and a Node runtime,
against the posture in §2: signpost is one static binary whose `go.mod` has no
`require` block, and this feature adds none.

**On the pattern references:** codeatlas and thlibo are cited for architecture, not
for code. signpost is a standalone repo with its own client implementations, its
own tests, and its own release cadence. Nothing is copied in, and no build-time or
runtime dependency on either is introduced.

---

## 10. Phasing

**v0.1 — deterministic core.** Discover, Go/TS/Python/Rust extractors, manifest and
infrastructure extraction, git signals, graph build with metrics, OKF emit, verify,
Mermaid, `signpost.yml`. No model anywhere. This version is independently useful:
it produces an accurate structural map and a working `index.md`.

**v0.2 — semantic pass.** Backend interface, inferd and openai-compat
implementations, schema-constrained extraction, grounding enforcement,
content-hash cache, human-review preservation, `signpost-semantic.yml`.

**v0.3 — enrichment and query.** SCIP reader, codeatlas bridge, `why` and `path`,
GraphML/DOT/JSON export.

**v0.4 — the viewer, deepened.** The node-link view in `site/graph.html` ships with
v0.1; v0.4 adds what it does not yet have — search, diff-between-commits, and deep
links to source. Still zero JS dependencies, still hardened per §7.2.

**v0.5 — MCP server.** stdio JSON-RPC so an agent queries the bundle instead of
reading files. Hand-rolled, following the pattern codeatlas already proved with
its hand-written GraphQL engine.

Each version ships with the full lint/test/scan gate green and a measured
extractor accuracy score per language.

---

## 12. Where this should be genuinely better

Matching the reference behavior is the floor, not the goal. Six places where the
design above is not just a reimplementation:

**1. Human corrections survive and accumulate.** Managed-marker regions, preserved
`verified:` blocks, sha-triggered downgrade of stale verification (§6.1). The
reference approach regenerates its output, so a correction lasts until the next
run. This is the single biggest difference, because it is what turns a report into
an asset: after six months the bundle contains things no tool could ever have
derived, and the tool is what keeps them attached to the right code.

**2. Graded, auditable trust per page.** OKF's `generated`/`verified`/`sources`
give unverified, machine-confirmed, and human-reviewed tiers for free, and
`generated.by` records which backend and model produced each page. A bundle built
partly on a local 12B and partly on a frontier model is auditable page by page, and
an agent can be told to weight human-reviewed content higher. Undifferentiated
confidence tags on edges do not give you that.

**3. Manifest and infrastructure extraction.** Compose files, workflows, Helm
charts, protos, OpenAPI, migrations, CODEOWNERS. This is exact, free, needs no
model, and answers the questions agents actually get stuck on: what deploys, what
listens on what port, what the CI gate is, who owns this. AST-plus-vision
pipelines largely skip it, and it is arguably the highest value-per-line in the
whole design.

**4. Staleness that fails loudly.** `stale_after` in frontmatter, sha-pinned
`resource`, `verify` exiting non-zero, a PR check that fails on a stale bundle. A
silently stale knowledge artifact is worse than none — it is confidently wrong, and
it burns trust in the tool permanently. This gets first-class treatment rather than
a hook that exits zero on failure.

**5. Byte-level determinism.** Sorted iteration, no seeds, temperature 0,
content-hash caching, byte-stability asserted in CI (§8.1). Because CI commits the
bundle, non-determinism means commit churn, and commit churn kills adoption faster
than any missing feature. Worth noting that this is a known pain point in
tooling of this kind, not a hypothetical: model-generated cluster labels vary
between runs on the same input, so the same conceptual group gets a different name
and its page becomes an orphan — a real tool in this space deletes every generated
markdown file at the start of each export specifically to stop the orphans piling
up. Deterministic labels are what make committing the artifact viable at all.

**6. Grounding enforced, not requested.** Every semantic claim cites a
`sources[]` entry that is verified to resolve before emit; ungrounded claims are
dropped rather than softened. Combined with schema-constrained sampling, that is
what makes a local model safe to use here — and it is what makes the output safe to
put in front of an agent that will act on it.

The honest summary of the difference: the reference tool is a **snapshot
generator** — impressive one-shot output, regenerated each time. This is designed
as a **maintained artifact** — versioned, reviewed, trust-graded, and correct at a
known commit or loudly stale. The second is harder to build and much more useful
at month six.

### 12.1 What is not a differentiator

Reviewed against the current state of the reference implementation rather than
against a write-up of it, because a list of advantages that turn out to be
table stakes is how a project talks itself into building the wrong thing.

**Backend flexibility is not a differentiator.** Bedrock, OpenAI, Anthropic,
Gemini, DeepSeek and local Ollama backends already exist there, as does a
schema-shaped extraction prompt with `EXTRACTED` / `INFERRED` / `AMBIGUOUS`
confidence on every edge — the same three-tier vocabulary `internal/graph` uses.
Our §5 is the right design and inferd-over-IPC is genuinely ours, but "runs
against whatever model you have" is parity, not an edge.

**Prompt-injection hardening is not a differentiator — it is catch-up.** The
reference implementation ships delimiter blocks, forged-marker defanging, SSRF
allowlisting, fetch size caps, path-traversal confinement, and HTML escaping. It
had this before we did. §4.5's hardening exists because *we were missing a control
a comparable tool already had*, and that is the honest framing.

**Not overlapping as much as it first appears: machine-written learnings.** The
reference tool accumulates model-generated lessons into a file. That is not the
same thing as differentiator 1 — generated notes are regenerable and unreviewed,
whereas the point of managed markers is that a *human* correction survives a
rebuild and carries a `verified:` attribution. But it does mean "the knowledge
compounds" is a contested claim rather than an obvious one, and the argument has to
rest specifically on human review surviving.

**What does survive scrutiny:** OKF as the on-disk contract (their outputs are
bespoke formats — Obsidian vaults, wikis, `graph.json`, Cypher — so nothing else
reads them without adapters); human-review preservation *inside* a file rather
than file-level ownership manifests; per-page graded trust with the producing model
recorded; staleness that exits non-zero; byte determinism; manifest and
infrastructure extraction; and a single static binary with a dependency tree we
can bump ourselves. That list is shorter than six items and it is the real one.

**The dependency argument is unchanged and remains the strongest one.** The
reference implementation's lockfile resolves to roughly 200 packages — including a
JIT compiler, an ONNX runtime, speech-to-text, a media downloader, plotting
libraries, and 26 parser grammars — for a tool whose job is to read a repository
and write markdown. Its direct dependencies are declared with no version
constraints at all. None of that is a criticism of the tool; it is the arithmetic
of §2. A CVE anywhere in that tree, in a tool we adopted rather than a library we
depend on, is a patch we carry or an upstream PR we hope lands. That is the
remediation path we are declining, and it does not depend on any feature comparison
being favourable.

---

## 11. What this does not solve

Stated plainly, because a tool that oversells is a tool people stop using.

**Token-reduction claims in this space are largely an accounting artifact.**
Headline compression ratios compare a graph summary against reading every raw
file, which is not what an agent actually does — a competent agent greps and reads
selectively. The honest claims for signpost are: an agent starts oriented instead
of re-deriving structure; doc-versus-code drift becomes visible; and the knowledge
compounds because human corrections survive. Those are worth the build. A specific
multiplier is not a number worth quoting.

**The bundle records structure and stated intent, not rationale.** It can tell you
that `auth` and `gateway` change together 14 times and that rate limiting sits in
the gateway. It cannot tell you that this is because of a 2025 incident, unless a
human writes that in the `## Notes` section — which is exactly why §6.1 exists.
The institutional knowledge is still on the team; signpost gives it a durable
place to live and a reason to be written down once.

**Small repos do not need this.** Under a few dozen files, the value is a
structural sanity check, not orientation. The tool earns its keep where structural
complexity exceeds what a person or a model holds in working memory.

**Prompt injection is mitigated, not solved.** §4.5 fences untrusted repository
content, defangs forged markers, constrains output to a schema, and drops
ungrounded claims. A determined injection inside a delimiter block can still bias a
summary — nobody has a proof against that. What the design guarantees is narrower
and worth stating exactly: the blast radius is schema-shaped, citation-checked
prose on a reviewable page with the producing model recorded in `generated.by`. The
deterministic pass, which carries the structural load, never sends anything to a
model and so is not exposed at all. A repo whose threat model cannot tolerate the
residual risk should run with no backend configured, which is a supported mode and
still produces the complete structural bundle.

**A local 12B is not a frontier model.** Schema constraints and the grounding rule
keep it honest about *form* and *citation*, and the deterministic pass carries the
structural load, so the semantic pass is doing narrow, well-scoped summarization
rather than reasoning about architecture. Where a repo justifies better summaries,
point the same binary at Bedrock and the only thing that changes is
`generated.by`.
