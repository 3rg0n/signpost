# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### 2026-07-29

- Initial repository: Go module `github.com/3rg0n/signpost`, `.gitignore`,
  README, and this changelog.
- `docs/design.md` — full design for the deterministic core, the OKF output
  bundle, the two model backends, the generator/viewer repo split, CI topology,
  and the supply-chain posture that motivates building rather than adopting.
- `internal/graph` — the in-memory knowledge graph:
  - Typed nodes (`Module`, `Service`, `Interface`, `Data Store`, `Document`,
    `External Dependency`, `Symbol`) whose kinds double as the OKF `type` field.
  - Typed edges (`imports`, `calls`, `implements`, `defines`, `configures`,
    `deploys`, `tested_by`, `documents`, `co_changes`, `owns`) each carrying a
    confidence level of `extracted`, `inferred`, or `ambiguous`, so consumers can
    weight what they trust and reviewers can audit what was guessed.
  - Merge semantics that never clobber: re-adding a node unions tags, files, and
    attributes while preserving existing prose; a `Kind` conflict is an error
    rather than a silent overwrite. Duplicate edges merge by summing weight and
    keeping the stronger confidence, independent of insertion order.
  - Structural metrics: degree and hub ranking, iterative Tarjan SCC for
    dependency cycles, weakly connected components for orphans and
    doc-versus-code islands, cross-cluster bridge detection, and deterministic
    shortest-path traversal.
  - Louvain community detection, hand-written, for the clusters that become
    `index.md` headings.
  - Determinism throughout — sorted iteration, no seeds, no randomised restarts —
    verified by tests that repeat clustering and pathfinding 25 times in-process
    and across 5 separate test processes.
- `internal/discover` — pipeline stage 0 (design §4.0):
  - Hand-written `.gitignore` matcher: anchoring, directory-only patterns,
    negation with correct later-wins precedence, `**` across segments, character
    classes, escapes, and per-directory nesting where a deeper file overrides its
    parent. Matching is case-sensitive on every platform so a Windows checkout and
    a Linux runner agree.
  - Classification into source (dispatched by language), manifest, lock file,
    infrastructure, contract, migration, doc, ownership, and data — filename-based
    and therefore cheap and deterministic.
  - Bounded reads, because a repository is untrusted input: 2 MiB / 50k-line caps
    with head-and-tail retention beyond them, binary detection by NUL byte and
    UTF-8 validity, a total-bytes budget, symlinks recorded but never followed,
    and irregular files skipped so a FIFO cannot block the walk.
  - CRLF normalised at ingest. Without it the same commit yields a different
    bundle on Windows than in CI, which is the determinism requirement in §8.1
    enforced at the point content enters the pipeline.
  - Vendored trees (`vendor/`, `node_modules/`, `.venv/`, build output) pruned and
    recorded in `Skipped`, so an incomplete walk never looks complete.
- `internal/extract` — the extraction contract and its measurement:
  - Language-neutral `Facts` (package, imports with aliases and named symbols,
    symbols with kind/exportedness/receiver/doc, entrypoints) plus an `Extractor`
    interface and a registry that dispatches by language. Extractors return facts
    rather than writing graph nodes, so a fixture can hand-label them and graph
    assembly stays a single shared concern.
  - Normalisation that sorts and merges by fact identity rather than source
    position, so reordering declarations in a file does not churn the bundle.
  - **The §4.2 scoring harness, built before any extractor**: precision, recall,
    and F1 per fact kind against hand-labeled fixtures, with targets of 0.95 (F1,
    imports) and 0.90 (symbols), and the offending values named so a failure is
    actionable. Scored separately per fact kind because an extractor that finds
    every import but half the symbols is useful for the module graph and useless
    for the public surface — one aggregate number would hide that.
  - Go extractor on stdlib `go/parser` + `go/ast`: packages, imports (aliases and
    blank imports retained, since a blank import is invisible `init` coupling),
    types versus interfaces, methods with generic receivers reduced to their base
    type, const/var groups, doc comments from both grouped and single
    declarations, and `main`/`init` entrypoints. An exported method on an
    unexported type is correctly not public surface. A file that does not compile
    still yields its imports and is marked `Incomplete` rather than discarded.
  - **Measured: Go scores F1 1.000 on both imports and symbols** across a
    five-file corpus (9 imports, 22 symbols), and parses all of signpost's own Go
    files with no failures. Go uses the real parser, so this is the precision
    baseline the hand-written extractors are scored against, not merely a pass.
  - One shared line scanner for the three hand-written extractors, landed with its
    own tests before any extractor used it. It strips comments and blanks string
    bodies while preserving byte offsets, so a pattern can never match across the
    hole a deletion would leave and a caller can still recover a literal's real
    value from the original bytes. Handles line, block, and nested block comments;
    Python triple-quoted docstrings in both styles; JavaScript template literals;
    Rust raw strings including `r#"…"#`, char literals, and multi-line `"…"`; and
    escapes throughout. This exists because the dominant failure mode of a
    line-oriented extractor is not missing a declaration but *inventing* one from
    text inside a comment or a string — a missed import is a gap, while a spurious
    one points an agent at a module that does not exist.
  - Python extractor: all import forms including relative, star, aliased, and
    lazy function-body imports; classes with method attribution; `__all__`
    honoured as an override of the leading-underscore convention, including the
    barrel `__init__.py` whose only purpose is to re-export names it does not
    declare; docstrings; and `if __name__ == "__main__"` entrypoints. Indentation
    decides scope, because a `def` inside a function is a closure nobody outside
    can call.
  - TypeScript/JavaScript extractor, one implementation for both: every ES import
    form, re-exports (`export … from`, `export *`, `export * as`,
    `export { default as X }`) recorded as the dependencies they are, since a
    barrel file is how most packages define their surface and missing it
    disconnects the graph exactly at the package boundary; `export` lists merged
    with the declarations they name; CommonJS `require` and dynamic `import()`,
    which are not legacy in Node tooling; arrow and function-expression constants
    reported as functions; JSDoc summaries. Scope is decided by brace depth, not
    indentation, so running a file through a formatter cannot change the extracted
    symbol set — the determinism the committed bundle depends on.
  - Rust extractor: `use` trees flattened recursively through nested braces, with
    `crate::`/`self::`/`super::` kept distinct from external crates because that is
    what separates an internal edge from a dependency; `impl` and `impl … for T`
    attributing methods to the implementing type rather than the trait; trait
    methods as declared surface; `mod x;` as both a dependency edge and a
    declaration; modifier stacks (`pub unsafe extern "C" fn`); and `pub(crate)`
    deliberately *not* public API, since a bundle listing it as such would tell a
    consumer they can depend on something they cannot.
  - **Measured: Python, TypeScript/JavaScript, and Rust all score F1 1.000 on both
    imports and symbols** against five hand-labeled files each (Python 15 imports /
    21 symbols; TS/JS 19 / 25; Rust 13 / 39). Every corpus includes an adversarial
    fixture whose declarations live inside strings, docstrings, template literals,
    raw strings, and nested comments; none of them are extracted. The harness
    earned its place by finding two real gaps no behavioural test had caught — a
    Python package's `__all__` re-exports going unreported, and Rust code being
    read out of the interior of a multi-line string literal.
  - `DefaultRegistry` assembles all four extractors in one place, with a test that
    every first-class language both has an extractor and actually reads the
    language it claims. A language silently missing here would report a whole repo
    as unhandled.
- `internal/manifest` — pipeline stage 1, everything a repository states about
  itself outside its source code:
  - Four hand-written readers over one shared tree type: YAML, TOML, JSON, and a
    Go-module reader. Zero third-party dependencies; `go.mod` still has no
    `require` block. The YAML reader is deliberately tolerant rather than
    conforming — see `docs/adr/0001-hand-written-tolerant-yaml-reader.md` — because
    a Helm template is not YAML and a conforming parser rejects the file entirely,
    while its unconditional skeleton carries the kind, the containers, and the
    pinned images that are the whole deployment surface of a Helm-shipped system.
    Supports block and flow collections, block scalars with all three chomping
    modes, both quoting styles with escapes, anchors, aliases, merge keys,
    multi-document streams, and YAML 1.1 boolean spellings, because each appears in
    files this reader must read; tags and complex keys are out of scope because they
    do not. Quotedness is retained on scalars, since `"3.10"` and `3.10` mean
    different things to the tool the file is for.
  - `Facts` — one struct covering modules, dependencies, scripts, entrypoints,
    services, images, CI jobs, contracts, migrations, owners, stated rules, and
    secret *references*. Shared across every reader for the reason the source
    extractors share theirs: graph assembly wants "every service in this repo" at
    once, and a per-kind type would push that union into the consumer as N cases.
  - **Secrets are recorded as references, never values.** A Kubernetes Secret
    contributes its name and its *key names*; an `env_file` contributes the path and
    is never opened. The bundle is committed and published, so a reader that
    captured a value would be a credential-exfiltration path wearing a
    documentation tool's clothes. Proved rather than asserted: the test flattens the
    entire `Facts` struct to a string and searches it for the secret bodies, so a
    leak through any field fails, not only through the fields intended to hold one.
  - Dependency readers for `go.mod` (including `replace`, `exclude`, and the
    indirect marker), `package.json`, `pyproject.toml` (PEP 508 with markers and
    extras), `requirements.txt`, and `Cargo.toml`. Direct and transitive are kept
    distinct, because the dependency policy in §2 is about the ones a human asked
    for.
  - Infrastructure readers: Containerfiles with `ARG` resolution and multi-stage
    awareness, so a `FROM builder` is a stage reference rather than a phantom
    external image; compose files with interpolation, condition-form `depends_on`,
    and loopback-qualified port mappings preserved; GitHub Actions workflows with
    gate inference, inherited-versus-overridden `permissions`, SHA pins kept
    verbatim, service containers, reusable-workflow secret inheritance, and
    composite actions; Kubernetes workloads across the template depth a CronJob
    introduces; Helm charts, values, and templates, the last degrading to a
    skeleton with a diagnostic.
  - Contract readers, statement-oriented rather than line-oriented so a wrapped
    declaration reads as one unit: protobuf (one contract per service, since "what
    this service offers" is the fact a consumer needs and one per method would bury
    it; streaming markers retained, since they change how a client must be
    written), OpenAPI 3 and Swagger 2 (one contract per *operation*, since "the API"
    is not a reviewable unit whereas "DELETE /v1/things/{id} returns 204" is), and
    GraphQL SDL including `extend` blocks. A contract is a promise to someone
    outside the repository, which makes it the highest-consequence thing an agent
    can change without noticing: removing a proto field breaks a consumer that is
    not in the working tree and will not appear in any test run here.
  - Repository readers: SQL migrations with destructiveness detection, ownership
    from CODEOWNERS, stated rules from `AGENTS.md`/`CLAUDE.md`, ADRs with status,
    and Makefile targets with their first recipe line. A column rename counts as
    destructive because it breaks every reader of the old name the moment it lands,
    which in a rolling deployment is the previous version of the application still
    serving traffic. Rule files are captured as *quotations attributed to the file*,
    never as guidance the tool adopts, and fenced code blocks are skipped entirely —
    §4.3, and the one place in this package where it applies: a rules file is
    untrusted text that a model will read back, and a code block inside one is an
    example whose contents would read as instructions once the fence was gone.
  - An ordered dispatch registry. Unlike the source registry it cannot key on one
    field, because `discover.Class` is too coarse — a Containerfile, a compose file,
    a workflow, and a Kubernetes manifest are all `ClassInfra` — so routes are tried
    in order and the first match wins, exactly as classification orders its own
    checks. Content never decides routing: where a name is ambiguous the reader
    sniffs its own content and returns empty facts, which keeps each admission rule
    beside the code that depends on it instead of in a second, drifting copy.
    Unhandled files are reported grouped by extension, so a repository whose
    deployment is entirely Terraform reports the gap rather than looking covered
    because its `go.mod` parsed.
  - 109 tests, including determinism runs over every reader. They earned their place
    by finding three real defects behavioural reasoning had not: a secret reported
    twice because two readers found the same `secretKeyRef` by different routes, a
    GraphQL single-line type body going unread, and a **hang** in the shared flow
    scanner where an unterminated scalar consumed no bytes. The last was fixed at
    the reader's own layer with its regression test there rather than in the
    extractor that tripped it, and both flow branches now guard against making no
    progress — an unanticipated shape should degrade to a diagnostic, never to a
    spin.
- `internal/assemble` — pipeline stage 3, where facts from every reader become one
  graph:
  - Stable node identity independent of discovery order, so the same commit
    produces the same ids on every machine. Collisions are resolved by suffixing
    over a sorted key set rather than by first-come, because first-come makes an
    id depend on the walk.
  - Import resolution per language: Go module paths against the declared module,
    Python relative (`.`, `..`) and absolute forms, TypeScript path-relative
    specifiers with extension and index resolution, and Rust `crate::`/`self::`/
    `super::`. Anything that resolves to nothing is counted as unresolved and
    reported, never silently dropped — a resolution gap is a hole in the map and a
    user needs to know its size.
  - A self-import resolves to no edge rather than a self-loop, since a package
    importing itself is a resolution artifact and not a fact about the code.
  - Test-subject attribution decided by *placement first*: a test file sitting
    beside production code tests the code it sits beside, and its imports are not
    consulted. Reading imports there produces edges that are confidently wrong —
    `assemble_test.go` imports the graph package to assert against it, which does
    not make the graph package tested by assemble.
  - Every edge is checked against the node set before it is added; a dangling edge
    is dropped and counted, and a non-zero count is reported as a bug in assembly
    rather than as a fact about the repository.
- `internal/export` — Mermaid, DOT, GraphML, and JSON rendering:
  - **Confidence survives rendering in every format.** An edge that was not read
    directly out of the repository is dashed in the diagram formats and carries a
    verbatim confidence attribute in the data formats. "storage imports auth" read
    from an import statement and the same edge proposed by a model are different
    claims, and a reviewer looking at a diagram has no other way to tell them
    apart.
  - Dangling edges are dropped by every format rather than drawn to an invented
    node, because a link to a concept that does not exist reads as a fact.
  - Mermaid identifiers are suffixed over a sorted id set, not just mangled:
    `/modules/a-b`, `/modules/a/b`, and `/modules/a_b` all mangle to the same
    name, which would silently merge three boxes into one — a wrong diagram, not
    an ugly one. Labels are stripped rather than quoted, since quoting is
    inconsistently supported across renderer versions and a label that breaks the
    whole diagram is worse than one that loses a bracket.
  - DOT quoting is hand-written rather than `strconv.Quote`, which escapes
    non-ASCII into `\u` sequences DOT does not interpret — a Cyrillic directory
    name would render as literal escape text. Descriptions and tags go in
    tooltips, since the label has room only for a name.
  - GraphML keys are a fixed schema rather than derived from the graph: a consumer
    diffing two exports should not see the schema change because one repository
    happened to have no services. Edge ids are emitted even though the spec
    permits omitting them, because Gephi and networkx use the id as row identity
    and an absent one merges parallel edges.
  - JSON uses wire types separate from the graph structs, so anything scripted
    against the output does not break when extraction grows, with HTML escaping
    off because the output is read by tools rather than embedded in a page.
  - Determinism is tested by rendering *freshly built* graphs five times, so map
    iteration order actually varies between runs instead of being fixed by one
    graph's internal layout.
- `cmd/signpost` — the CLI:
  - `signpost graph` reports what a person is most often wrong about in their own
    repository: import cycles, cross-cluster bridges, hubs, disconnected
    components, and orphans. A listing of every module is something `ls` already
    gives you. `--fail-on-cycle` makes the cycle check a CI gate.
  - `signpost export` renders any of the four formats to stdout or, with `-o`, to
    a file. The graph is rendered to memory first and written once through a temp
    file and a rename: an export is either the whole graph or nothing, because a
    file half-written by a failure looks like a valid export of a smaller
    repository. Mode 0o644 rather than the 0o600 `CreateTemp` gives, since an
    export is a committed artifact CI reads.
  - **Coverage reporting is not opt-in.** Every analysing command prints what it
    could not account for — files not read, languages with no extractor named by
    extension, extraction failures, and unresolved imports — to stderr unless
    `--quiet`. Design §4.2: the absence of a measurement is never a clean bill of
    health. "other (12)" would not tell anyone whether those twelve files are
    Kotlin, Terraform, or shell, so `LangOther` is expanded into extension counts.
  - An exit-code contract CI can act on: 2 means the command line was wrong and
    re-running it identically will fail identically; 1 means signpost ran and what
    it found or read was the problem.
  - `--ignore` is a repeatable flag rather than a comma-separated list, because a
    gitignore pattern can legitimately contain a comma.
  - Write errors are latched and checked rather than ignored: `signpost export |
    head` closes the pipe partway through, and exiting 0 after a truncated export
    is read downstream as a successful run over a smaller repository.
  - `build` is deliberately absent until the OKF emitter lands. Shipping a command
    that writes an incomplete bundle would be worse than not offering it, since the
    bundle is the thing agents trust. `graph` and `export` run the same pipeline
    `build` will.
- `README.md` rewritten for public release with install instructions; `LICENSE`
  (MIT, © 3rg0n) and `CONTRIBUTING.md` added, the latter stating the full gate,
  the ADR-per-direct-dependency rule, and the scoring requirement for extractors.

#### 2026-07-30

- `install.sh` and `install.ps1` — one-line installers that pull a tagged
  release:
  - **Both refuse rather than degrade.** No SHA-256 tool available, a digest that
    does not match, or an archive absent from `checksums.txt` all abort with
    nothing installed and no temp directory left behind. An installer that fell
    back to "download anyway" would make the published checksums decorative.
  - `install.sh` is POSIX `sh`, works with either curl or wget and either
    `sha256sum` or `shasum`, and resolves the latest tag from the
    `/releases/latest` redirect rather than the API — no rate limit and no JSON
    parsing in shell. Installs to `$HOME/.local/bin` unless `/usr/local/bin` is
    already writable; it never escalates privilege.
  - `install.ps1` takes the architecture from `RuntimeInformation`, not
    `PROCESSOR_ARCHITECTURE`, which reports the *process* architecture and so
    reads x86 for a 32-bit shell on an ARM64 machine. Forces TLS 1.2 for
    PowerShell 5.1, renames a running `signpost.exe` aside before overwriting,
    and only ever edits the user `PATH`.
  - Both were tested end to end against real archives, including the tamper and
    missing-checksum paths.
- `.github/workflows/release.yml` — tag-triggered cross-compile for
  linux/darwin/windows × amd64/arm64, publishing checksummed archives.
  Reproducible by construction: `-trimpath`, `SOURCE_DATE_EPOCH=0`, and
  `tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0`, so the same
  tag yields the same bytes. `checksums.txt` is generated by globbing the archive
  extensions under `LC_ALL=C` so it neither attests to itself on a re-run nor
  varies in order between runners. The gate is re-run here because a tag can be
  pushed to a commit that never passed CI.
- `.github/workflows/ci.yml` — test on Linux, macOS, and Windows, because
  discovery normalises CRLF and matches gitignore case-sensitively and the point
  of both is that a Windows checkout and a Linux runner agree. `-count=2`
  everywhere and `-race` on Linux. Separate lint job (gofmt, staticcheck,
  golangci-lint, shellcheck, and a PowerShell AST parse of the installer) and
  security job (govulncheck, gosec, gitleaks over full history).
  - A `dependencies` job that fails a PR adding a *direct* dependency without an
    ADR. It parses the require block for the `// indirect` marker rather than
    using `go mod edit -json`, which would need the base `go.mod` checked out as
    a module directory. This is the §2 posture made enforceable instead of
    aspirational.
  - Every action is pinned by commit SHA rather than tag, since a tag is mutable
    and has been moved in a real supply-chain compromise. Dependabot understands
    the `# vX.Y.Z` comment, so pinning does not mean going stale.
- `.github/dependabot.yml` and `renovate.json` — weekly gomod and
  github-actions updates, grouped for minor/patch and ungrouped for major, with
  vulnerability alerts exempt from the schedule. Being on the hook to remediate
  CVEs is the reason the dependency count is kept low; this is the other half of
  that bargain.
- `SECURITY.md` — private advisory reporting, and what signpost does with your
  code: reads and writes markdown, executes nothing, never follows a symlink,
  bounds every read, and records secrets as references. States plainly that
  prompt injection is mitigated rather than solved, with backend-less operation
  named as the answer.
- `.github/CODEOWNERS` — one owner, with export, assembly, the workflows, and the
  two installers called out. Also a fixture for signpost's own CODEOWNERS
  reader.
- `site/` and `.github/workflows/pages.yml` — a static landing page, hand-written
  HTML and CSS with no build step and no JavaScript, deployed to GitHub Pages off
  the merge path so a broken deploy cannot block a merge. The hero is signpost's
  real output on its own repository, including the line reporting the two files it
  has no extractor for. Amber is reserved for the unverified and used nowhere
  else, so the palette teaches the same distinction the exports draw.

### Changed

#### 2026-07-30

- `cmd/signpost` writes every diagnostic through the latching printer, so a
  closed stdout is caught on the usage and error paths too and not only on the
  export path. The one deliberate exception is the coverage report: it is a
  stderr diagnostic, and failing a run whose actual output was written
  successfully would turn a redirected stderr into a build failure.
- `docs/design.md` §9.1 added, correcting an earlier draft that routed
  repository-practice signals — declared build and test commands, CI gates,
  ownership, observability, ADR presence — outside signpost. They belong in the
  bundle: the manifest layer already extracts them. A maturity *score* is
  deliberately excluded, because a 1–5 level is a rubric rather than a
  measurement and reads as measured once it is printed, which is the exact
  failure the confidence model exists to prevent.

#### 2026-07-29

- `docs/design.md` reviewed against the current state of a comparable
  industry-standard tool, using its source rather than a write-up of it. Four
  amendments:
  - **§4.5 — repository content is now treated as untrusted input.** The semantic
    pass wraps every file in a hash-stamped delimiter block, defangs sequences that
    could forge a role turn or a premature closing delimiter, and relies on
    schema-constrained sampling plus the grounding rule as the second layer. This
    closes a real gap: the pass feeds repo content to a model whose output is
    committed and then read by agents that act on it, so injection here is a path
    into the artifact agents trust. A comparable tool already had this control.
  - **§8.0 — merge behaviour for the committed bundle.** Building only on the
    default branch, one page per concept so conflicts stay small and readable, and
    regeneration at the merge commit as the documented tiebreaker. No custom git
    merge driver: it requires per-contributor local configuration and silently
    degrades without it.
  - **§12.1 — what is *not* a differentiator.** Backend flexibility, confidence
    tiers, and prompt hardening are parity or catch-up, not advantages. The
    surviving list is OKF as the on-disk contract, in-file human-review
    preservation, per-page graded trust, loud staleness, byte determinism,
    infrastructure extraction, and a patchable dependency tree.
  - **§11 — injection is mitigated, not solved**, stated plainly, with
    backend-less operation named as the answer for repos that cannot accept the
    residual risk.
- Corrected a stale reference to "fixed seeds in label propagation" in §8.1 left
  over from the Louvain change; clustering is deterministic by construction, with
  no seed to set.
- `internal/discover` now reads every file through an `os.Root` handle scoped to
  the walk root instead of composing absolute paths. Symlinks were already recorded
  and never followed, so behaviour is unchanged — but that was an argument about
  the code being correct, and this is a guarantee enforced below it. Worth the
  change because signpost reads a tree it does not control and commits what it
  found: a path that escaped the root would put content from outside the repository
  into a file that gets pushed. `os.Root` is stdlib, so this costs no dependency.

### Notes

- **Zero third-party dependencies so far.** `go.mod` has no `require` block. The
  policy is not zero dependencies but *patchable* dependencies: every direct
  dependency must be one we can bump ourselves, and few enough that bumping stays
  routine. See `docs/design.md` §2.
- **Louvain replaced label propagation** during implementation. LPA was chosen
  first for being a third the size, on the assumption that cluster quality would
  not matter for index headings. Measurement disproved it: on two dense groups
  joined by a single edge, synchronous LPA with a lowest-label tie-break collapses
  the entire graph into one community — the documented giant-community pathology.
  A single cluster containing the whole repo makes the headings worthless. The
  failing test is retained in the suite as a regression guard.
- Tarjan is iterative rather than recursive, with a 20,000-node deep-chain test,
  because recursion risks stack exhaustion on the large monorepos signpost targets.
