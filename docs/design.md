# signpost — design

**Give models signposting for repos.**

signpost compiles a repository into a small, durable, human-editable knowledge
bundle that an agent reads *before* it starts work. It is a compilation step, not
a retrieval system: no vector database, no embeddings, no server. One binary, one
command, output is markdown.

- **Status:** design, pre-implementation
- **Language:** Go, single static binary
- **Output format:** [Open Knowledge Format v0.2](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md)
- **Shape:** two repos — `signpost` (generator, runs in CI) and `signpost-view`
  (viewer, publishes to GitHub Pages). See §7.

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

### What signpost is not

- Not a code search index. `codeatlas` does that, with SCIP precision.
- Not a readiness assessment. `codebase-agent-readiness` does that, read-only.
- Not a bug finder or a code reviewer.
- Not a chat interface.

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
| Other-language parsing | Hand-written extractors (§4.2). A tree-sitter binding is the fallback if accuracy demands it — that is a library decision, not a tool decision, and it is ours to bump. |
| Graph algorithms | Hand-written (§4.4). Roughly 600 lines, all textbook — genuinely cheaper than a dependency. |
| Clustering | Louvain, hand-written. ~200 lines versus a JIT-compiler toolchain. Label propagation was tried first and rejected on measured behaviour (§4.4). |
| YAML | Hand-written tolerant reader and hand-written emitter, both ours. Helm templates are not YAML and a conforming parser rejects them outright, so a library would not have covered the files that matter (ADR [0001](adr/0001-hand-written-tolerant-yaml-reader.md)). |
| Model access | Two backends behind one interface (§5). Both first-party over stdlib `net` and `net/http`. |
| SCIP enrichment | `google.golang.org/protobuf` under a build tag — Google-published, heavily audited, already in codeatlas. |
| Generator output | Markdown and JSON only. Nothing executable. |
| Viewer | Separate repo, separate dependency tree, separate blast radius (§7). |

Nothing in the generator path executes or renders untrusted input. The output is
markdown; the worst a hostile repo can do is put ugly strings in a `.md` file.
Every JS dependency lives in the viewer repo, which never runs in CI on a
protected branch and cannot break a merge.

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
model can pick the three pages it needs instead of reading the bundle.

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

### 4.1 Extract — deterministic, no model

**Go** gets `go/parser` and `go/ast` from the stdlib: packages, imports, exported
symbols, interface implementations, `main` functions, `init` side effects. Full
precision, zero dependencies, and it is our primary language.

**TypeScript/JavaScript, Python, Rust** get hand-written line-oriented extractors
covering imports/requires, top-level declarations, exports, and entrypoints. These
are not full parsers and are not trying to be. The signpost layer needs the module
graph and the public surface, and a focused extractor gets ~95% of that. Where
precision matters, SCIP enrichment (§4.3) supplies it.

If the measured accuracy (§4.2) proves inadequate for a language teams actually
care about, a tree-sitter Go binding is the fallback for that language
specifically. That is a direct library dependency we bump ourselves — a different
proposition from inheriting a tool's grammar tree — and it stays behind the same
extractor interface, so it is a swap rather than a redesign.

The languages are chosen for the same reason you chose them: Go, Rust,
TypeScript, and Python have the strongest tooling and the strongest model training
coverage. Everything else falls back to a generic extractor (comment headers,
filename conventions, sibling context) plus the semantic pass.

**Manifests and infrastructure are the highest-value deterministic signal and the
part comparable tools mostly skip.** All of this is exact, cheap, and structural:

| Source | Yields |
|---|---|
| `go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml` | external deps, module identity, scripts, entrypoints |
| `Containerfile` / `Dockerfile`, compose files | services, ports, base images, build inputs |
| `.github/workflows/*` | how it builds, tests, and ships; what gates exist |
| Helm charts, k8s manifests | deployable units, config surface, secrets *references* |
| `*.proto`, OpenAPI, GraphQL SDL | interface contracts |
| `migrations/*` | data model and its evolution |
| `CODEOWNERS`, `AGENTS.md`, `CLAUDE.md`, `docs/adr/*` | ownership, stated rules, decisions |

**Git signals** via `git log` (git is present wherever this runs): co-change pairs,
churn per path, author concentration, last-touch date, first-commit date. Co-change
is the cheapest way to find coupling that imports do not show.

### 4.2 Extractor accuracy is measured, not asserted

Each extractor ships with a fixture corpus and a scored test: extracted
imports/exports versus a hand-labeled expectation. The score is reported in
`manifest.json` per language. When an extractor is below target for a language
present in the repo, the affected pages say so in `status` and the bundle records
it in `skipped_checks`. Absence of measurement is never presented as a clean bill.

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
external dependencies, documents.

Typed edges: `imports`, `calls`, `implements`, `defines`, `configures`, `deploys`,
`tested_by`, `documents`, `co_changes`, `owns`.

Every edge carries `confidence`: `extracted` (found in source or manifest),
`inferred` (derived by the model), `ambiguous` (model was unsure). An agent can
therefore weight what it trusts, and a reviewer can audit what was guessed. Recorded as
ADR [0004](adr/0004-confidence-is-a-first-class-field.md), which also states what the
field does *not* claim.

Metrics, all hand-written and all deterministic:

- degree, fan-in, fan-out → hub identification
- Tarjan SCC, iterative → dependency cycles, which are real findings
- connected components → orphans, and doc-versus-code islands
- Louvain modularity optimisation → clusters
- cross-cluster edges → the bridges where a change is most likely to surprise

**Clustering: Louvain, after measuring that label propagation does not work
here.** LPA was the first choice — a third the code, and the reasoning was that
"group related modules so the index has sensible headings" is undemanding enough
that clustering quality would not show. That was wrong, and the test that proved
it is in the suite: on two dense groups joined by a single edge, synchronous LPA
with a lowest-label tie-break collapses everything into one community. It
degenerates into a min-label flood across any connected graph — the documented
giant-community pathology. One cluster containing the whole repo makes the index
headings worthless, which is clustering's only job here.

Louvain costs about 150 more lines of arithmetic and gets it right. It is still
far cheaper than the dependency it replaces, and it is deterministic by
construction rather than by seeding: sorted node order, ascending community
evaluation, ties to the lowest index, final numbering by each cluster's
lowest-sorting member. No randomised restarts.

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
   `[INST]`, and lines that are nothing but `### System:` / `### Instruction:`.
   Without this, a file containing a premature `</untrusted_source>` smuggles
   instructions into the trusted region and the wrapper is decorative.
3. **The grounding rule and the schema do the rest.** Schema-constrained sampling
   means a successful injection still cannot change the *shape* of the output, and
   the grounding rule means an injected claim with no resolvable citation is
   dropped at emit. Defence in depth: the wrapper is the fence, and grounding is
   what catches whatever gets over it.

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

`verify` exits non-zero on failure. This matters: the failure mode to avoid is a
staleness check that exits zero, because a bundle that is silently stale is worse
than no bundle — it is confidently wrong, and it destroys trust in the tool
permanently.

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

**`none`.** Deterministic-only. Not an error state — a supported mode.

Configuration, `.signpost.yml` or environment:

```yaml
backend: inferd            # inferd | openai | none
model:   auto              # backend-resolved; stamped into generated.by
budget:  { max_calls: 400, max_tokens: 2000000 }
openai:
  base_url: ${SIGNPOST_OPENAI_BASE_URL}
  api_key:  ${SIGNPOST_OPENAI_API_KEY}   # env only, never in the file
```

**Fail-open, following thlibo's ADR 0006.** If the configured backend is
unreachable, signpost emits the deterministic bundle, records the skip in
`manifest.json` with what was lost, and exits 0. A broken model backend must never
break a merge.

---

## 6. Commands

```
signpost build [path]          # full pipeline; writes .signpost/
signpost build --deterministic # skip the semantic pass entirely
signpost verify [path]         # conformance + link + staleness; non-zero on failure
signpost why "<question>"      # traverse the bundle and answer, citing pages
signpost path <A> <B>          # shortest typed path between two concepts
signpost export --mermaid|--dot|--graphml
signpost install-hooks         # optional local post-commit hook
```

`why` and `path` are pure bundle traversal — no model, no network. They exist so
an agent can ask a question without loading the whole bundle into context.

### 6.1 Human-review preservation

The mechanism that makes the bundle compound rather than churn:

- Generated prose lives between `<!-- signpost:managed:NAME -->` markers.
  Everything outside them is human territory and is copied through verbatim.
- A `verified:` block added by a human is preserved across runs.
- When the underlying `resource` sha changes, `verified` is **downgraded** rather
  than silently kept — the page is re-marked machine-confirmed and the downgrade
  is recorded in `log.md`, so a reviewer knows to look again.
- Human-authored `## Notes` sections are never regenerated, never reordered, never
  reflowed.

### 6.2 What signpost does not write

signpost writes `.signpost/` and nothing else. It does not write `AGENTS.md`,
`README.md`, or `ARCHITECTURE.md` — those encode human intent and team
convention, and a generator overwriting them is how teams learn to distrust
tooling. `signpost build --suggest-agents-md` will print a proposed stub to stdout
for a human to take or leave, and that is the extent of it.

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

---

## 7. Two repos: generator and viewer

> Recorded as ADR [0006](adr/0006-generator-and-viewer-are-separate-repositories.md).

The generator and the visual are separate products with different audiences,
different dependency trees, and different risk profiles. Splitting them is what
lets the viewer be genuinely good without putting a JS dependency tree anywhere
near a protected branch.

### 7.1 `signpost` — the generator

Go binary. Runs locally and in CI. Emits the OKF bundle plus graph exports.
Markdown and JSON only; no HTML, no JS, no server. This is the repo that gates
merges, so its dependency list stays short and every entry is justified.

Inline in the bundle: **Mermaid** graphs in `index.md` and each cluster page.
GitHub renders Mermaid natively, so a tech lead clicks `.signpost/index.md` and
sees the module graph with nothing installed and no site deployed. Mermaid
degrades past a few dozen nodes, so it is capped — clusters and top hubs at the
root, members on cluster pages. It is the zero-setup skim, not the real visual.

Exports for anyone with their own tooling:

```
signpost export --graphml   # Gephi, yEd, Cytoscape
signpost export --dot       # Graphviz
signpost export --json      # the graph, for the viewer and for scripts
```

**GraphML is the interop win.** It carries typed edges, confidence levels, cluster
assignments, and arbitrary node attributes, and it opens in tooling teams already
have and already trust. For a lot of internal users that is the whole visual story
and no site needs to exist.

The structural findings — hubs, cycles, bridges, orphans, doc/code islands — are
written as **text** in `index.md`, because that is what an agent consumes. A
picture is for the human skim; the prose is the load-bearing artifact.

### 7.2 `signpost-view` — the viewer

A static site generator that consumes `graph.json` and publishes to **GitHub
Pages**. Each repo gets a browsable graph at its Pages URL — no install, no local
server, nothing to run. The interactive view lives at a URL a person can bookmark
and paste into a review.

Why this split is strictly better than bundling an `HTML` output into the
generator:

- **Blast radius.** Every JS dependency lives here. A CVE in the graph library is
  a bump in a repo that publishes a static site, not in the binary that gates
  merges. The generator's dependency tree stays auditable on its own terms.
- **Not in the merge path.** Pages deploy is a separate workflow. If the viewer
  build breaks, merges keep working and the bundle is still correct.
- **The dangerous part becomes tractable.** The known vulnerability class here is
  interpolating untrusted repo strings into a page. Isolated in a viewer, that is
  one repo with one job: escape everything, no `innerHTML`, a strict CSP, no
  network egress from the page, and `graph.json` treated as untrusted input even
  though we generated it. That is a reviewable security boundary rather than a
  feature buried in a general-purpose tool.
- **It can be properly good.** Freed from "must not add deps to the thing that
  gates merges," the viewer can have real filtering, search, diff-between-commits,
  and deep links to source. A generator that also renders HTML can never afford
  any of that.

The viewer is optional. A team can adopt the generator, read `index.md` and the
Mermaid graphs in GitHub, open the GraphML in yEd, and never deploy a site.

---

## 8. CI

Two workflows plus a PR check.

**`signpost.yml`** — on push to the default branch. Runs
`signpost build --deterministic` and `signpost verify` on a hosted runner. No
model, no infra, on the order of seconds. Commits `.signpost/` only when the diff
is non-empty.

Loop guard, all three: `paths-ignore: ['.signpost/**']`, a skip when the actor is
the bot, and `[skip ci]` on the bot commit.

**`signpost-semantic.yml`** — on schedule and `workflow_dispatch`. Full pipeline
with a backend configured. On a self-hosted runner this is inferd over IPC with
the model already warm. On a hosted runner it is Bedrock through the
OpenAI-compatible endpoint with OIDC-issued credentials. Same binary, same schema,
different `generated.by`.

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

We ship no custom git merge driver in v0.1. A merge driver requires every
contributor to configure it locally (`.gitattributes` names it; only
`git config merge.*.driver` activates it), and an unconfigured contributor
silently gets default behaviour — so a driver is a fragile place to put
correctness. If real usage produces conflict pain that (1)–(3) do not cover, the
driver is the v0.3 answer, with "regenerate at the merge commit" as the always-available
fallback that needs no local setup.

**Dependency governance**, enabled on the first commit in both repos, because the
posture in §2 is a commitment to remediate rather than a claim to have no exposure:

- **Dependabot** — security alerts and automated patch PRs, both repos, Go modules
  and (for the viewer) npm.
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

**v0.4 — `signpost-view`.** Separate repo. Static site from `graph.json`, GitHub
Pages deploy, filtering and search, diff-between-commits, deep links to source.
Hardened per §7.2.

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
