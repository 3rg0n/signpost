# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### 2026-07-30

- `signpost build` writes the knowledge bundle to `.signpost/`, which is the
  output the rest of the project exists to produce: an `index.md`, one page per
  concept, a `log.md`, and a `manifest.json`, all Open Knowledge Format markdown
  that agents and people read without signpost installed. New `internal/okf`
  package; new `-repo` flag naming the repository in each page's `resource:` URI.

  Four properties, each of which is a defect if it fails:

  - **Human edits are never touched.** Generated prose lives between
    `<!-- signpost:managed:name -->` markers; a rebuild replaces those regions
    and carries everything else across byte-for-byte, including frontmatter keys
    a human added — held as raw source lines rather than parsed and re-emitted,
    so their comments, quoting, and key order survive exactly. Every ambiguous
    case in the merge resolves toward "this is human text": the cost of guessing
    wrong that way is a region that stops regenerating, which `verify` reports,
    where the cost of guessing the other way is an invisible deletion. The run
    says how many pages it carried notes on, because a mechanism nobody can see
    working is one nobody trusts.
  - **Nothing is deleted.** A page describing a concept the graph no longer
    contains is reported as stale and left on disk. A renamed directory would
    otherwise silently delete the page somebody had written notes on, and that
    is exactly the page worth keeping.
  - **The same commit produces the same bytes.** `generated.at` is the commit's
    author date, not the wall clock — ADR 0005 commits the bundle, so a
    timestamp would make every CI run a diff. It also answers the right
    question: when the code was written is a fact about the code, when CI ran is
    a fact about CI. A repository with no readable history gets no `resource:`
    and no `generated:` at all rather than a stamp naming a commit nobody can
    check.
  - **A human's verification is downgraded, not preserved and not dropped.**
    `verified:` stands only when the reviewer recorded *which* resource they
    reviewed and it is the one being described now. Otherwise the block is kept —
    the reviewer's name and date are the audit trail — and the page gains a
    generated `status: stale-verification` key saying the claim no longer holds.
    Written into the page rather than only reported on stdout, because the bundle
    is read by people and agents who never ran signpost, and a downgrade that
    exists only in a closed terminal is one nobody acts on. Silently retaining it
    would leave a page asserting a human checked code that has since changed,
    with nobody knowing to look; deleting it would lose the reviewer's name.
    Because `status` is generated, a re-review that records the current resource
    clears the mark on the next run with nobody editing it — the downgrade is
    recoverable in one step, which is what makes it the safe default.

  The log page accumulates rather than being rewritten: each date owns a region
  named `log-<date>`, and a later run does not generate that name, so the merge
  keeps it verbatim and appends. Two consequences are accepted rather than
  worked around — entries read oldest-first, and two runs on the same date
  collapse into one entry.

- `signpost verify` checks a committed bundle against the tree it describes and
  exits non-zero when it no longer holds. That exit code is the whole interface:
  the failure mode design §4.6 names as worse than having no bundle is a
  staleness check that exits zero, because a bundle that is silently stale is
  confidently wrong and spends trust that does not come back. New
  `internal/okf.Verify`; new `verify` subcommand taking the same flags as
  `build`.

  Five checks: frontmatter parses and carries a `type`, with the reserved
  filenames `index.md` and `log.md` typed correctly and no other page claiming
  those types; every `edges[].to`, `sources[].resource`, and prose link resolves
  to a page that exists; every `resource:` names the commit being described; and
  a rebuild would change nothing.

  Four properties, and the first is what makes the rest trustworthy:

  - **Verify re-renders rather than comparing hashes.** It calls the same
    renderer and the same merge `build` does, so "verify passes" means exactly
    "a build would change nothing" — there is no second opinion about what a
    page should contain that could drift from the emitter's and leave neither
    obviously wrong. It also catches the one cost the merge deliberately
    accepts: a managed marker broken by hand makes that region stop
    regenerating, and this is what makes that visible rather than permanent.
  - **A check that did not run is named.** "No findings" from a check that never
    executed has the same shape as a pass and only one of them is one, so an
    unresolvable relative link and a staleness comparison with no commit to
    compare against are both reported. When the manifest already says the whole
    bundle describes another commit, the per-page comparison is suppressed and
    said to be — otherwise every page restates the one line that explains all of
    them.
  - **A pass says what it covered.** Pages, edges, sources, and prose links
    resolved are printed before the verdict and are not hidden by `-quiet`. "ok"
    over zero pages and "ok" over eighty read identically in a CI log.
  - **Warnings are not failures.** An orphan page and a stale `verified:` block
    are reported and exit zero. Neither makes the bundle untrue, and both are
    litter the never-delete rule is designed to leave behind — a gate that went
    red on a rename with no supported way to fix it is a gate people switch off,
    taking the staleness check with it.

  Two consequences worth knowing. Verify must be given the same flags as the
  build it checks: `-repo` feeds every page's `resource:`, so a mismatch there
  reports a real difference that describes the invocation rather than the
  bundle. And a stale bundle exits 1 while a bad command line exits 2, because a
  CI job has to tell a problem a rebuild fixes from one that will fail
  identically forever.

- Five ADRs and an index at `docs/adr/README.md`. Only the narrowest decision in
  the project had one; everything foundational was prose in `docs/design.md`,
  which is a different genre — design.md describes what the system *is*, while an
  ADR records that alternatives were weighed and rejected and is immutable once
  accepted. A reader of design.md could not tell which of its statements were
  decisions. Now recorded: patchable dependencies rather than zero dependencies
  (0002), directory granularity for module nodes and the public contract its IDs
  form (0003), confidence as a first-class field on every node and edge (0004),
  committing the bundle and the determinism requirement that follows (0005), and
  the generator/viewer repository split (0006). The index also lists the four
  decisions still owed one, so the gap is visible rather than discovered later.

- Git history is read and folded into the graph. `internal/vcs` walks
  `git log --numstat` and yields per-file and per-directory churn, first and last
  author dates, author concentration, and co-change pairs: two directories that
  keep appearing in the same commit. That last one is why the package exists — a
  handler and the migration it depends on, a proto and its generated client, a
  config key and the code that reads it are all coupled, and none of them is an
  import edge any static read can see. Co-change becomes a `co_changes` edge
  carrying the commit count as its weight; churn becomes attributes on the module
  node. New flags: `-no-history` and `-max-commits`.

  Four properties shape the implementation:

  - **Absence is reported, never fatal.** A missing git, a directory that is not
    a repository, an empty history, and a shallow clone are all facts rather than
    errors — the structural bundle is complete without any of this, and CI
    asserts it is byte-identical under `-no-history` apart from the co-change
    edges. What is refused is silence: a shallow clone yields real but truncated
    signals, and "no co-change found" and "no history to look at" are different
    claims, so the coverage report always states which one it is.
  - **A rename keeps one history.** `--find-renames` marks a rename only in the
    commit that performed it and does not rewrite the older commits, which still
    name the old path; `--follow` would, but git accepts it for a single pathspec
    only, so it is unavailable to a whole-history walk. Without folding the old
    path forward, a moved file's history splits in two and the current path's
    churn reads as though the code were new. Chains (`a → b → c`) are followed
    transitively. Found by an integration test against real git — every
    fixture-level parse test passed while the behaviour was wrong.
  - **History annotates the map, never decides what is on it.** Both passes run
    last and touch only nodes the structural pass already created. A directory
    with history and no source is not a module: deleted code still has history,
    and a node for it would be a page about something that is not there.
  - **Bounded, and safe against a hostile repository.** Commits are capped, and a
    commit touching an implausible number of directories contributes churn but no
    co-change — a formatter rollout or an initial import relates everything to
    everything and says nothing. Paths are read under `-z`, because line-oriented
    git output C-quotes any path containing a space, a quote, or a non-ASCII byte,
    and a filename is the part of an untrusted repository most likely to be
    adversarial. The repository path is the subprocess's working directory rather
    than a git argument, so a directory named `--upload-pack=...` cannot become a
    flag, and config is hardened per-invocation with `-c` rather than by
    environment, since `GIT_CONFIG_NOSYSTEM` suppresses system and global config
    but `.git/config` belongs to the repository being analysed.

- The dogfood job builds a bundle from this repository and asserts the three
  properties that make a committed one safe to adopt: byte-identical across two
  runs of the real binary, a note appended outside the managed markers survives a
  rebuild and is reported as carried, and nothing in the output is other than
  markdown or JSON or carries an executable bit. All three are already covered by
  unit tests; none of them was covered end-to-end through the binary, which is
  where both v0.0.1 installer bugs lived. It runs last in the job because it
  writes into the checkout, and the scratch copy it diffs against goes to
  `RUNNER_TEMP` rather than the tree — a copy left in the repository is content
  the second run would discover, which would change the thing being measured.

- The dogfood job also asserts both directions of `verify`'s exit code: zero on
  the bundle it just built, and non-zero after a new package is added. A gate
  that has only ever been observed passing has not been shown to be able to
  fail, and the direction that matters is the one nobody exercises by accident.
  The pass is required to report at least ten pages checked, since a pass over
  nothing is not a pass, and the verify output is printed before the assertion
  rather than after — under `set -e` a failing command would otherwise abort the
  step first, leaving a log that says a gate failed without saying what it found.

- CI runs signpost on signpost. The binary analyses this repository on every
  push, and the job asserts what the run produced rather than that it exited 0 —
  `export` exits 0 on a completely empty graph, so an exit-code check would go
  green on a total pipeline collapse. It holds floors on node, edge, and module
  counts, set well under current values so ordinary growth never trips them;
  requires every edge in a no-model run to carry `extracted` confidence, since
  only the semantic pass may infer; renders all four export formats twice and
  compares bytes; and checks the coverage report is well-formed and still names
  its own gaps. The floors are counted through `jq` rather than by grepping the
  JSON, because attribute keys come from the analysed repository and a repo with
  an attribute named `id` would inflate a grep count — parsing is also the only
  check here that the export is valid JSON. It also asserts history was actually
  read: the job checks out with `fetch-depth: 0` specifically so it can be, and
  without an assertion a regression that stopped reading git would leave the job
  green while the bundle silently lost churn and co-change.

### Fixed

#### 2026-07-30

Both defects below were found by installing v0.0.1 from the published URLs the
way a user would, not by reading the scripts. Neither is reachable by any check
that does not actually run an install.

- `install.ps1` could not be parsed by Windows PowerShell 5.1 at all — the
  edition an `iex (irm ...)` line lands in on a default Windows box. 5.1 decodes
  a BOM-less script as Windows-1252 rather than UTF-8, so a UTF-8 em dash
  arrives as three characters whose last is U+201D, a right double quotation
  mark the 5.1 parser accepts as a string terminator. The string closed early
  and the file failed to parse several lines from the actual character. The file
  is now pure ASCII and says so in its own `.NOTES`.
- `install.sh` resolved the latest release to the literal string `latest` and
  then refused to install anything. `curl`'s `%{url_effective}` reports the URL
  curl last requested, not the `Location` it was handed, so the `HEAD` of
  `/releases/latest` needed `-L` to actually follow the redirect. The tag is
  also stripped of the trailing carriage return a header line carries, and the
  failure message now prints what it parsed instead of only that it failed.

### Changed

#### 2026-07-30

- CI gates both installers as pure ASCII and parses `install.ps1` under
  **Windows PowerShell 5.1 as well as pwsh 7**, on a Windows runner. The
  previous check ran only under pwsh 7, which defaults to UTF-8 and parsed the
  broken file happily — a gate that passed on a script the majority shell could
  not run. The ASCII check avoids `grep -P`, which is absent from some builds
  and errors out under `LC_ALL=C` in others, and it branches on the exit code
  explicitly: 1 is clean, 0 is a finding, anything else means the check itself
  broke. An `if grep ...` would have read that error as "nothing found".

## [0.0.1] — 2026-07-30

First tagged release. Deliberately **not** v0.1.0: v0.1 is the deterministic core
*complete*, and `signpost build`, `signpost verify`, and git signal extraction
have not landed. Publishing 0.1.0 now would claim a milestone the status table on
the same commit says is unmet. What is here — `graph` and `export` over the full
extraction pipeline — is finished and tested; the version number says only that
the surface is still moving.

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
