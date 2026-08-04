# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### 2026-08-02

- **OpenTelemetry traces for signpost's own run, off unless asked for.** Six spans — `analyse`
  and the five stages under it: `discover`, `extract`, `manifests`, `history`, `assemble` —
  each carrying counts of what it handled and nothing else. It answers the one question a
  timing report cannot: *which* stage is slow on a repository, and whether it is slow because
  the repository is large. `history` is usually the answer.

  **`SIGNPOST_ENABLE_TELEMETRY` is the only thing that turns it on.** An `OTEL_*` endpoint in
  the environment is not consent — CI runners carry them for unrelated collectors, and a
  default that sends anything anywhere has already sent it by the time anyone notices. With
  the gate set, the standard `OTEL_EXPORTER_OTLP_*` variables configure endpoint, headers, and
  timeout as they do for any other instrumented tool.

  **Counts only, and the type system is what enforces it.** The one method for attaching data
  to a span is `Count(key string, n int)`; there is no string setter, so a path cannot reach a
  span without adding an API to do it. Spans are named for stages, resource attributes are the
  five signpost sets plus the SDK's own, and a failed stage records status without a message —
  a Go error from this pipeline routinely reads `open /home/someone/private/repo/x.go:
  permission denied`.

  **Telemetry can never be why a build failed.** An unreachable collector, a rejected batch, a
  malformed variable: each is one line on stderr and the run continues. Reported rather than
  swallowed, because somebody who typed `=yes` has asked for telemetry and silence is
  indistinguishable from a working exporter with nothing to say.

  Three direct dependencies, and the SDK's own OTLP/HTTP exporter is not one of them —
  `otlptracehttp` links the entire gRPC stack for a package that posts JSON over HTTP, 21
  modules and 65 gRPC packages against 10 modules and none. The exporter here is ~200 lines
  against the SDK's `SpanExporter` interface. The full measurement, including a build-tag trap
  where `govulncheck` reports clean on a tree carrying a known CVE, is in
  [ADR 0014](docs/adr/0014-adopt-the-otel-sdk-and-write-the-exporter.md).

  Two corpus stages and two CI steps hold both boundaries. Positive: all six span names must
  arrive, so the content check cannot be satisfied by an exporter that sends nothing. Negative:
  nothing the corpus contains — `[slug]`, `data,notes`, `POSTGRES_PASSWORD`, internal package
  names — appears anywhere in the payload, every span attribute is an integer in signpost's
  namespace, and every bundle is compared byte-for-byte against one built with telemetry off.
  The CI steps read the payload with a parser that has no stake in signpost being right, over a
  real socket, from a real subprocess: an exporter that encoded a timestamp as a JSON number,
  or a batch that never left because the flush raced the exit, looks identical from inside the
  process that wrote it.

- **`AGENTS.md`, and a README section on pointing agents at the bundle — because a committed
  bundle is not a discovered bundle.** Measured rather than assumed. Two agents got the same
  task in two repositories that both had a bundle committed. In *this* repository one found
  `.signpost/modules/extract.md` and read it second, before any source. In a repository with
  every textual mention of signpost scrubbed out, the other read eleven files by hand and
  never opened the bundle at all — re-deriving from `go.mod`, `pyproject.toml`, `Cargo.toml`,
  and the CI workflow exactly the structure sitting in twenty-eight pages it never looked at.

  The first result was a false positive: the agent found the bundle because it was reading
  the README *of the tool that writes bundles*, and that chain does not exist in anybody
  else's repository. Adding a four-line `AGENTS.md` to the scrubbed control moved
  `.signpost/index.md` to the third file read. Models are trained to read `README.md` and
  `AGENTS.md`; nothing trains them to look inside a dot-directory they have never heard of.
  One line in a file they already read is the entire fix.

  signpost still does not write those files — they encode human intent, and a generator
  overwriting them is how teams learn to distrust tooling (design §6.2). It only reads them,
  to report whether a repository states any rules for the agents working in it, which makes
  the omission here self-diagnosing: signpost's own `practices.md` said *"No agent
  instructions were found, so an agent working here has only the code to go on"* until this
  commit, and now says *"14 stated rules."*

  The `AGENTS.md` carries more than the pointer, because the control run showed what a bare
  pointer does not prevent: the agent proposed hand-writing `modules/greeter-3.md` and
  editing `index.md`, treating generated pages as source. So the file states that managed
  regions are replaced on every run, that CI owns the rebuild on `main` while pull requests
  only verify, and — now also in CONTRIBUTING — that a rebuilt bundle does not belong in a
  PR.

- **`.signpost.yml` — a repository states how it wants to be analysed.** Optional, at the
  repository root. Nine keys, each of which sets the default for a flag: `include_vendored`,
  `include_fixtures`, `ignore`, `no_history`, `max_commits`, `repo`, `backend`, `model`, and
  `hooks.check`. `repo` is the one that earns the file on its own — it feeds every page's
  `resource:`, so `build` and `verify` must pass the same value, and signpost's own CI passes it
  in five places across four workflows.

  **A key may only change a default, and that is the whole design.** Anything that decides
  whether a check *fails* stays a flag — `-as-of-bundle`, `-fail-on-cycle` — because a repository
  that can weaken its own gate by committing a file is not gated. So does anything that is a
  property of one invocation rather than of the repository: `-quiet`, `-o`, `-format`, `-top`.
  Those keys are **refused by name with a reason**, not ignored: somebody who writes
  `fail_on_cycle: false` believes they configured something, and a tool that reads the file, does
  the opposite, and exits 0 has told them their gate is what they asked for. There is also
  nowhere to put a credential — the file is committed, so `api_key` and `openai` are refused
  pointing at `SIGNPOST_OPENAI_API_KEY`.

  Precedence is **flag > environment > file > default**, one order with no per-key exceptions.
  The flag wins even when set to the zero value, which needs `flag.Visit` rather than a
  comparison against zero: `-include-vendored=false` and an absent flag carry the same value and
  must not carry the same decision. Read from the root and nowhere else — no user-level file, no
  `XDG_CONFIG_HOME`, no `-config` outside the tree, no walk upward — because a config search path
  is how the same checkout starts producing different bundles for two people, and the committed
  bundle's byte-stability does not survive that.

  **Unlike the manifest readers, this one is intolerant.** Those step over what they cannot
  interpret by design (ADR 0001), because they read files other people wrote for other tools.
  This file is signpost's own, so *any* diagnostic is exit 2 and no bundle — including the ones
  the tolerant reader merely notes. `include_vendored true`, missing its colon, would otherwise
  mean analysing the repository the way the file said not to while reporting success. `${VAR}`
  interpolation is refused for the same reason rather than stored verbatim: design §5 once
  sketched `api_key: ${SIGNPOST_OPENAI_API_KEY}`, ADR 0011 withdrew it, and a `model:
  ${SIGNPOST_MODEL}` reaching the backend as a model id produces a 400 that says nothing about
  the config file. The file is repository content and is not exempt from the walk it configures.

  Recorded as [ADR 0011](docs/adr/0011-configuration-file-format-and-location.md), written before
  the implementation because the second and third classes are what erode: nothing about the code
  stops somebody adding `fail_on_cycle` later, so the test that refuses every key in both classes
  is the clause, and the corpus asserts a committed `fail_on_cycle: false` stops the build with no
  bundle written.

- **`signpost hooks install` — an optional local `post-commit` reminder.** Prints one line when
  `.signpost/` has fallen behind the code, for anyone building the bundle locally rather than in
  CI. `signpost hooks uninstall` removes it; `signpost hooks run` is what the hook calls and is
  useful by hand.

  It reports and never gates. The hook cannot fail a commit, does not rebuild anything, and is
  `post-commit` rather than `pre-commit` — a rebuild on every commit on every branch is exactly
  the merge pain the "bundle is built only on the default branch" rule exists to avoid. CI's
  `signpost verify` remains the only thing that fails a build over a stale bundle, and
  CONTRIBUTING says so explicitly, because a hook that is *expected* is a gate enforced on
  machines nobody can inspect.

  Two behaviours will surprise people, so both are in the install output and the README. It
  **appends** a marked block to whatever `post-commit` hook is already there and uninstall takes
  out only its own lines, so a git-lfs hook survives both. And it installs where
  `git rev-parse --git-path hooks` points, which honours `core.hooksPath` at any scope — when
  that is set, git reads hooks from that one directory for every repository on the machine and
  ignores `.git/hooks` entirely, so writing to `.git/hooks` anyway would install a file that
  never runs while reporting success. git-lfs resolved this the same way in
  [git-lfs/git-lfs#3240](https://github.com/git-lfs/git-lfs/issues/3240). The block carries
  three guards — `[ -d .signpost ]`, `command -v signpost`, and `|| true` — so it is inert in a
  repository without a bundle, on a machine without signpost, and under `set -e`.

  Two check modes, defaulting to the cheap one because this runs on every commit. `fast` is two
  `git log -1` calls and costs milliseconds; it answers "the bundle was written before the newest
  code commit", which reports a commit that touched only `LICENSE` as behind. `-check verify`
  runs the same `verify -as-of-bundle` comparison CI gates on — about a second here, and it
  names the pages that would actually change. `SIGNPOST_HOOK_CHECK` sets the default for an
  invocation, and a `hooks.check` key in `.signpost.yml` sets it per repository. The accurate
  mode calls `verify` rather than reimplementing it: a second answer to "is the bundle current"
  would eventually disagree with the gate.

  Recorded as
  [ADR 0013](docs/adr/0013-the-local-hook-reports-and-ci-gates.md), because both halves are
  conventions that erode silently — clause 1 will be argued against as "a `pre-commit` hook
  would stop the bad commit existing", and clause 4 is invisible when broken. The test that
  covers them drives a real `git commit` with a stub on `PATH`: mode, shebang, location,
  Git-for-Windows `sh` dispatch, and the never-gates property are one assertion, and none of
  them is reachable from a unit test.

### Changed

#### 2026-08-04

- **A colliding page name is now suffixed with a hash of the thing it names rather than a
  position, so `src-2.md` becomes `src-1slg0rn.md`
  ([ADR 0015](docs/adr/0015-a-colliding-page-name-is-suffixed-from-its-own-key.md)).** A one-time
  rename in every adopted bundle that has a collision; a bundle without one, including signpost's
  own, is byte-identical.

  The name is a contract. It is the node ID, every other page links to it by that ID, and the
  bundle is committed — so a renamed page is that file *plus every page citing it*, rewritten in
  the diff of a commit that need not have touched the directory. The suffix was a counter in path
  order, which made a page's name depend on how many same-named directories sorted ahead of it: in
  a tree with `a/auth`, `b/auth` and `c/a-u-t-h`, adding `aa/auth` renamed `b/auth`'s page from
  `auth-2` to `auth-3`, and deleting `a/auth` renamed it again to `auth`. Twice, for a directory
  that had not changed. Adding a directory that did not collide moved nothing, which is what
  locates the cost: it is paid by every later member of the group. `ZT-duo-cc-plugins`, a
  repository with a bundle already, carries a `tests-2.md` named this way.

  Nothing in the graph is wrong afterwards, which is why it needed asserting rather than noticing:
  `verify` passes, every test passes, and the symptom is a reviewer facing forty changed pages for
  a one-directory change with no way to tell which mean something. The bundle's whole claim is
  that it is reviewable.

  **Hashing the key was not sufficient on its own.** Somebody still has to get the bare readable
  `src`, and giving it to whoever the walk sees first means a newcomer sorting ahead of the
  incumbent takes the name off it — and adding a `src` to a repository that has two is an ordinary
  edit, so roughly half of such additions would rename someone else's page. So the names are
  counted before any is assigned and a shared one is suffixed for *every* member including the
  first. That was found by a corpus test failing on `rust/src moved from modules/src.md to
  modules/src-1slg0rn.md`, not by reasoning about it.

  One residual, recorded rather than claimed away: when a collision group shrinks to a single
  member, that member stops needing a suffix and moves to the bare name. Bounded to that group.
  Closing it means suffixing every page in every bundle whether it collides or not, which trades
  the readability of all of them for stability in a case that needs a directory deleted.

  Held at two levels because they catch different things. `TestCorpusPageNamesSurviveAnUnrelatedEdit`
  rebuilds in process on all three platforms, so a filesystem that folds case differently shows up
  there; the corpus job's *A committed bundle's page names survive an unrelated edit* commits the
  bundle first and asserts `git status` reports no deletion, which is the reviewer-facing cost and
  the thing no in-process test can measure — a rename is a delete plus an add of a path never seen
  before. Both add a Go package in `go/src`, a new member of the largest collision group sorting
  ahead of the Rust and TypeScript members, which under a counter is the worst position available.
  Five mutations, each killed by a distinct assertion: restoring the counter, hashing the slug
  instead of the key, ignoring the reservation, dropping the terminating counter, and skipping the
  reservation for services or for data stores.

#### 2026-08-02

- **The zero-dependency claim is withdrawn from the README, the landing page, and
  `CONTRIBUTING`, ahead of the three OpenTelemetry modules
  ([ADR 0014](docs/adr/0014-adopt-the-otel-sdk-and-write-the-exporter.md)).** ADR 0002's *rule*
  still binds — a direct dependency must be one we can bump ourselves, and there must be few
  enough that bumping stays routine — but its consequence, an empty `require` block, does not.
  Only the consequence is superseded, and the evidence that the rule holds is that
  `GO-2026-6061` was remediated by one `go get`.

  The measurements are in the ADR, and two of them are worth knowing before somebody repeats
  them. Upstream's *HTTP* exporter links `google.golang.org/grpc`, protobuf, and grpc-gateway —
  **65 gRPC packages and 36 protobuf packages for a transport that uses neither** — where the
  SDK plus two methods of our own OTLP/JSON links ten modules and no gRPC at all. And a build
  tag does not contain a dependency: on one tree, `govulncheck ./...` reported *"No
  vulnerabilities found"* while `govulncheck -tags otel ./...` reported a known CVE. A tag hides
  a dependency from the scanner, not from the supply chain, which is the inverse of what
  ADR 0002 exists to achieve — so the telemetry code will not be behind one.

  `go list -m all` is also the wrong instrument, and using it is how the estimate in the ADR
  index came to say "five to eight modules". It counts test-only modules of dependencies.
  `go list -deps -f '{{if .Module}}{{.Module.Path}}{{end}}' ./... | sort -u` counts modules that
  actually contribute a linked package, and that is the number `CONTRIBUTING` now asks for.

- **Breaking: `signpost export` is now `signpost graph export`, and `signpost graph` is now
  `signpost graph show`.** One rule now decides the shape of every verb: a noun with more than
  one operation becomes a group, a noun with one stays flat, and *a group's own name is never
  an action*. That last clause is what forced `show` — with `export` beside it, `graph` would
  have been simultaneously a command and a namespace, which has to be learned twice and is
  ambiguous the first time somebody types `signpost graph` expecting either behaviour.

  `build` and `verify` stay flat, deliberately. Neither is an operation on an addressable
  resource — there is one bundle per repository, not a collection to list — and a bare `build`
  is the convention `go build`, `cargo build`, and `docker build` already set. Uniform
  noun-verb grouping would have cost the primary command a word to buy a consistency nobody
  was confused by.

  No aliases. The old spellings are gone rather than deprecated: v0.1.0 is public but nothing
  depends on it, and an alias is a second spelling to document, test, and keep working
  forever. What replaces them is cheaper and finite — an old verb reports where it went
  (`"export" is now signpost graph export`), and `signpost graph .`, which used to be a valid
  invocation, says the group's behaviour moved to `graph show` rather than reporting `.` as an
  unknown command. Both notes are deletable once the old spellings leave people's shell
  history.

  The shape is `gh`'s, and the rule above is the one `gh` follows without ever stating —
  which is why `gh repo list` and `gh search repos` both exist. Stating it is what stops the
  surface drifting a verb at a time. Two things are better than `gh` here: a renamed command
  is not simply unknown, and a mistyped one gets a suggestion when exactly one candidate is
  within a typo's distance — one candidate or none, since a suggester that always guesses is
  noise.

  Underneath, dispatch is one recursive function over one command tree, with the top level
  modelled as an unnamed group. `model` previously hand-rolled its own dispatch, help printer,
  and unknown-subcommand message, so a second group meant a second copy and two places for
  `signpost -h` and `signpost model -h` to drift apart. Help now works at every level in one
  format, group names are marked as taking a subcommand, and a group with no runnable name
  cannot silently acquire a default verb — a test asserts it, because that is exactly how the
  rule above would erode.

- The command surface is now a rule rather than a layout, recorded in
  [ADR 0012](docs/adr/0012-a-group-name-is-never-an-action.md): a noun with more than one
  operation becomes a group, a noun with one stays flat, and a group's own name is never an
  action. It is an ADR because the surface is a public contract whose failure mode is erosion
  — six months of individually reasonable additions with nothing to check them against.

- Configuration is decided in
  [ADR 0011](docs/adr/0011-configuration-file-format-and-location.md): `.signpost.yml` at the
  repository root and nowhere else, precedence flag > environment > file > default, and a
  config key may only change a default — never what a flag or a gate *means*. It was owed
  before the CLI could be restructured, since a config file changes what a flag means. Nothing
  is implemented; the decision is recorded, including that flags which decide whether a check
  fails (`-as-of-bundle`, `-fail-on-cycle`, thresholds) are permanently not configurable, so a
  repository cannot quiet its own gate by committing a file.

### Fixed

#### 2026-08-04

- **An import signpost placed inside the repository and found nothing at drew no edge and said
  nothing, so a module could report importing nothing while importing plenty.** Three things can
  happen to an import specifier: it resolves to a page; signpost cannot place the name at all,
  which was already counted and reported; or signpost places it exactly — inside a Go module,
  under a matched `paths` alias, down a relative path — and finds no node there. The third state
  went unrecorded. The branch that handled it was empty, because a specifier was counted only
  when it was *not* internal.

  It stayed hidden because both of the resolver's decisions about such an import are right. The
  specifier really is first-party, and inventing an external dependency for it would report a
  package nobody publishes — the false supply-chain claim the resolver exists to avoid. Two
  correct decisions were between them the reason nothing recorded the missing edge.

  **Its own line, because it is its own fix.** An unresolved specifier needs a resolver that
  knows the convention; an unlinked one is often nothing to fix — generated code genuinely is not
  in the tree — and otherwise needs a reader for whatever sits at that path. A handful is
  ordinary. A lot means a resolution root is missing, which is the shape the tsconfig `paths` gap
  had: 542 absent edges on one repository, 14% of its graph, with no line anywhere admitting
  them.

  The count found two live defects on its first run against the corpus, both below. Neither was
  visible to any existing assertion: every edge check in the harness names the edges it expects,
  and none of them can notice an absent edge nobody listed.

- **`use super::*` resolved out of the crate, so the commonest `super` in Rust reached nothing.**
  `super` is the parent *module*, and Rust's module tree matches the directory tree only loosely.
  Resolution went up a directory unconditionally, which is right only for `mod.rs` — the one
  spelling whose module *is* its directory. Every other file is a module inside the module its
  directory stands for, so its parent is that directory. For `src/lib.rs` a directory up is where
  `Cargo.toml` lives, holding no source: the import landed outside the crate and linked to
  nothing.

  The case that matters is `use super::*` inside an inline `#[cfg(test)] mod tests`, which is by
  a wide margin the most frequent `super` in the language, and where the parent module is the
  enclosing file itself. That resolves to the file's own module and so to a self-edge, which the
  graph drops — correctly: a test module importing the file it is written in tells a reader
  nothing the file did not already say. What was wrong was reaching for the wrong directory to
  get there. Mutation-tested in both directions: resolving always to the parent restores the
  original defect, and resolving always to the file's own directory loses the `mod.rs` edge.

- **A corpus fixture's only internal Go import had never resolved.** `go/go.mod` declares
  `example.com/corpus/greeter` at `go/`, so the package in `go/greeter/` is that path plus its
  directory. The fixture imported the bare module path, which names a directory holding `go.mod`
  and no Go file. It had been that way since the corpus was written, and nothing caught it: no
  assertion named a Go internal edge, and the coverage report had no line for an import that
  resolves inside the repository and reaches nothing. Fixing it draws the edge, and the corrected
  specifier now serves as the negative boundary for the count that found it — same import block,
  same module, differing only in there being Go files at the end of it.

#### 2026-08-03

- **Files signpost could not identify were not reported anywhere, so a repository whose only
  frontend it could not read looked covered.** Discovery assigns a class to every file, and
  every class but one routes somewhere: to an extractor, or to a manifest reader. The two that
  can still come back empty-handed say so — each carries an `Unhandled` map the coverage report
  prints. The class meaning *"signpost cannot tell what this is"* had no counterpart. It was
  written in one place and read in none, so a file landing there left the pipeline with nothing
  recording it had existed.

  Found on a repository whose entire landing page was two `.astro` files. The report named
  `.sh` and `.sql`, said nothing about the pages, and the bundle described that workspace as a
  one-file JavaScript module built from `astro.config.mjs`. The difference is which extensions
  the source table contains: `.sh` and `.sql` are in it as an unhandled *language*, so
  extraction counts them; `.astro` and `.vue` are not, so they never reached the stage that
  counts. **Every extractor added widens that table**, which is why this is fixed before the
  next seven rather than after — each would otherwise open the same hole somewhere new.

  **Two lines, not one, because they are different admissions.** "No extractor for `.kt`" says
  signpost knows what Kotlin is and cannot read it. "N file(s) of no recognised kind" says it
  could not determine what the file was, so no reader was offered it at all. Folding them lets
  the second hide behind the first. Binaries are excluded on purpose: a `.png` was classified
  correctly and has nothing in it to read, and counting it would bury the extensions that are
  gaps under the ones that never could be.

  The corpus carries `web/` — two `.astro` files, and a `README.md` beside them that must *not*
  appear on the line. Two files rather than one, since the count is the assertion and one file
  cannot distinguish counting files from counting extensions; the README because an
  implementation keyed on "did a reader produce facts about this directory" would report it.
  Asserted in the Go suite, again in CI through the real binary, and on signpost's own tree,
  which now reports its `site/*.html`, `LICENSE`, and `CNAME`. Verified by mutation: dropping
  the binary exclusion, counting every class, and skipping the case fold each fail a different
  test.

- **A Python package's absolute imports resolved against the repository, not the package, so a
  monorepo lost its internal edges.** `from api.client import make_api_request` names a
  top-level package, and the only thing that makes it resolvable is the directory holding that
  package's `pyproject.toml` being on the interpreter's path. Resolution tried exactly two
  roots — the repository root and `src` — on the reasoning that those cover essentially every
  Python project. True of a project. False of a monorepo, which is where the imports are: one
  measured repository has 28 `pyproject.toml` files, and that single specifier appears in 340
  imports, every one reported as a dependency nobody declares while nine sibling packages each
  held their own `api/client.py`. Same class of defect as the tsconfig `paths` gap fixed on
  2026-08-01, which was worth 14% of that repository's edges.

  **A root has to be scoped or it invents structure, and those nine `api/client.py` files are
  why.** A root list governing the whole repository would resolve one package's `api.client`
  to whichever of the nine sorted first — an edge between two packages that cannot see each
  other, reported with the confidence of something extracted and with nothing in the bundle
  marking it as a guess. That is worse than the gap it replaces. So a root governs only the
  files beneath it, nearest first, and the repository root stays last so a single-package or
  `src`-layout tree with no manifest in the walk still resolves as it did.

  **`pyproject.toml` only, of the two Python manifests signpost reads.** A `requirements.txt`
  pins what to install and declares no package; `requirements/base.txt` is a real and common
  spelling, and registering its directory would make `requirements/` a resolution root and
  invent edges into a directory holding no code. A unit test holds that boundary directly.

  Two corpus packages now hold the same module path deliberately, because the fix fails in
  both directions and only one of them is visible in a count: `py/services/alpha/handler.py`
  and `py/services/beta/handler.py` contain the byte-identical line `from api.client import
  fetch`, resolving to different files. No per-package root leaves both unresolved; an
  unscoped root sends both to the same place. The corpus asserts both edges exist and that
  neither crosses, in the Go suite and again in CI through the real binary.

- **`winreg` and `msvcrt` were reported as external dependencies, so portable Python looked
  like the analysis had failed.** The standard-library list was hand-kept, on the reasoning
  that a missing entry degrades to "unresolved" — visible and correctable rather than silent.
  That reasoning was wrong in a direction it did not anticipate: the names it omitted were not
  obscure, they were *platform-specific*. `winreg` and `msvcrt` are Windows-only; `fcntl`,
  `grp`, `pwd` and `termios` are Unix-only. A list assembled from code read on one platform
  omits exactly what the other platform's code imports, so a repository doing conditional
  imports for portability was reported as depending on packages nobody can install — and the
  gap count, the number a reader uses to judge whether the map covers their repository, was
  inflated by the most portable code in the tree. The list against
  `sys.stdlib_module_names` was 146 entries with 74 missing and none extra.

  It is now generated from `sys.stdlib_module_names` rather than patched, because patching the
  two names found on this host would leave the same list short for the other platform. Kept as
  a literal rather than read at runtime: signpost does not require a Python interpreter to read
  Python. The modules PEP 594 and PEP 632 removed stay in, because signpost reads a repository
  rather than a running process — a project pinned to `requires-python = ">=3.8"` imports `cgi`
  and `distutils` correctly, and there is no PyPI package named `cgi` to mistake this for.

  **A longer table is a wider surface for a loose match, which is the other boundary.** The
  corpus imports `winreg_helpers`, which opens with the six characters of the stdlib `winreg`
  and is the standard library of nothing; it must be reported as a gap. `pathe` against the
  Node builtin `path` is the same shape. Both spellings of the platform split — `winreg` and
  `fcntl` — sit in one tree so the list cannot be completed for one platform and left short
  for the other, and the absence checks match the whole name rather than a fragment, since a
  fragment check for `winreg` would be satisfied by the very page that must not exist.

#### 2026-08-02

- **A manifest declaring no dependencies was reported as unpinned, so the practices page told
  readers two builds could resolve different versions of nothing.** Found by reading
  signpost's own page, which said *"The Go dependencies are declared but not pinned by any
  lockfile in the tree, so two builds can resolve different versions"* about a `go.mod` with an
  empty `require` block and no `go.sum`. Every clause of it is false: nothing was declared, so
  nothing resolves and two builds cannot differ. The lockfile check alone cannot tell the two
  apart — "no lockfile beside this manifest" is true of an ecosystem with nothing to pin and of
  one that needs pinning, and only the dependency count separates them. An ecosystem with an
  empty table now states that, because a reader can act on it: no lockfile is missing and no
  supply chain needs reviewing.

  It is also the rare false positive that would have fixed *itself* into invisibility. The three
  OpenTelemetry requires ([ADR 0014](docs/adr/0014-adopt-the-otel-sdk-and-write-the-exporter.md))
  create a `go.sum`, at which point the sentence becomes accidentally true and nothing about the
  logic has changed — so this landed first, and the regression is pinned by an input rather than
  by the repository the tests run in.

  The corpus could not express the condition as committed: all four of its ecosystems declare
  dependencies, deliberately, because the resolution assertions need them to. So the stage
  empties a crate's dependency table and removes its lockfile, then asserts all three outcomes
  from one page — Go pinned, Python declared-and-unpinned, Cargo with nothing to pin — plus the
  count of empty manifests. No single answer satisfies that: "nothing is unpinned" fails on
  Python, "every manifest is empty" fails on Go and Python, and the shipped bug fails on Cargo.

- **Every link between bundle pages was root-absolute, so every one of them 404'd on
  GitHub.** `/modules/hook.md` resolves against the *web server root*: on
  `github.com/3rg0n/signpost/blob/main/.signpost/index.md` it points at
  `github.com/modules/hook.md`. It only ever worked in a viewer that mounts the bundle at
  `/` — and ADR 0005 names GitHub browsing as the whole reason the bundle is committed, a
  reader opening `.signpost/index.md` and seeing the module graph with nothing installed.
  That reader had 118 broken links across 40 pages. Links are now page-relative (`./b.md`,
  `../interfaces/x.md`), which works in a viewer, on GitHub, and in a checkout opened in an
  editor, and survives relocation: a fork, a bundle under a different directory, or a
  subtree merge that nests the tree one level down all keep working, because no link names
  a root that a relocation can change.

  **Nothing failed, and the reason it could not fail is the part worth recording.**
  `verify`'s resolver interpreted the same absolute form the emitter wrote, so the two
  agreed with each other and disagreed with every markdown renderer in existence. A gate
  built out of one half of a round trip cannot see a bug in the convention both halves
  share. So the regression test resolves each link the way a renderer does — join it to the
  directory of the page it is written on and look for that file on disk — with no help from
  signpost's own resolver, and it lives in the corpus so it runs through the binary against
  a repository this tree cannot produce.

  The change also came within one line of silently emptying the link gate. `bundleLinks`
  returned only `/`-prefixed targets and counted the rest as `skipped`; moving the emitter
  to relative links without touching it would have turned 118 *checked* links into 118
  *skipped* ones while `verify` kept printing ok. It now takes the region's provenance: in a
  generated region a relative target is signpost's own and is checked, and in human prose it
  is genuinely ambiguous — `../../internal/auth/auth.go` in somebody's notes means a
  repository file — and is counted as unchecked. `verify` on this repository reports 123
  prose links checked, which is the number that would have gone to zero.

  `bundleRel` still resolves the absolute form, deliberately: `verify` runs against a bundle
  on disk that a rebuild has not necessarily touched, and a resolver that stopped
  understanding the old form would report every page of an older bundle as broken rather
  than stale. `resource:` URIs stay absolute — OKF requires a globally-unique identifier,
  and a relative one identifies nothing.

- **`hooks install` reported a hooks directory inside the repository as shared with every
  repository on the machine**, when the repository was reached through a symlink. That warning
  is what tells a person the file they are about to have edited is not only theirs, so getting
  it wrong in this direction is a warning about somebody else's tool that is not there.

  git resolves one side of the comparison and not the other: `--show-toplevel` comes back with
  symlinks expanded, and an absolute `core.hooksPath` comes back exactly as it was written. So
  the two disagree whenever the repository's own path contains a symlink — macOS is in that
  state by default, since `/var` is a symlink to `/private/var`. Both sides are now resolved
  before comparison, and `Paths.Dir` is left as git reported it, because that is the path git
  will use and the path the person set.

  Found by a CI failure on macOS and Windows runners that this host could not reproduce, in an
  assertion comparing a `t.TempDir()` path against a git-reported one — a real asymmetry
  surfacing as a test bug. Windows passed locally only because this developer's home directory
  escapes 8.3 short-name mangling, and macOS has no local run at all. The test half now asks
  the filesystem (`os.SameFile`) rather than comparing strings, and the product half has its
  own case with the symlink in the repository's path prefix rather than below it — which is
  where the defect actually lives, and which the first attempt at that test got wrong and a
  mutation caught.

- **`-h` on a command exited 2 and printed its help to stderr.** So
  `signpost graph show -h | less` showed nothing and the shell saw a failure, while
  `signpost -h` and `signpost graph -h` exited 0 and printed to stdout. Requested help is an
  answer, not a misuse: `-h` now exits 0 and writes to stdout at every level.

  Both halves come from Go's `flag` package, which reports `-h` as a parse error and writes
  usage to the flagset's own output — so a leaf command inherits the wrong exit code and the
  wrong stream unless it is made not to, and a terminal hides both, since the two streams are
  one screen there and nobody reads the exit code of a help invocation. Each leaf now picks one
  writer for the whole of its usage, prose and flag list together; the first attempt at this
  fixed only the prose and left `PrintDefaults`' output on stderr, which is help split down the
  middle and reads as working from either side alone. Tests assert exit 0 with an empty stderr
  for every leaf, and CI repeats it against the built binary.

- **`-include-vendored` did nothing.** Its help text promises to *analyse vendored
  third-party code instead of only recording it*, and the walk honoured it: vendored
  directories were descended into and their files read off disk. Then every consumer filtered
  them straight back out. Six sites each decided the same question independently, each spelled
  `!f.Vendored` with no reference to the option — so the flag read a vendored tree into memory
  and discarded it, and a user who passed it got a bundle byte-identical to one built without
  it. `Sources()` was the one that mattered, because extraction is driven from it.

  The decision now lives in one place, `discover.Result.Analyses`, and the walk carries its
  option forward on the result — which is what the consumers were missing: each held the
  result and none held the options that produced it. A vendored file stays marked vendored, so
  the skip report and the file's metadata remain truthful; the flag changes whether a file is
  analysed, not what it is, matching the invariant `-include-fixtures` already held.

  Two halves fail independently and both are asserted: extraction, and the manifest readers.
  Fixing only the first analyses a vendored package's source and still discards the
  `package.json` beside it, leaving a module whose own declaration signpost had in hand and
  threw away. The corpus carries a committed `ts/node_modules/` for this — a real pattern
  `.gitignore` does not exclude, and a condition signpost's own tree cannot express, which is
  why the defect survived the unit tests that covered the walk and stopped there. Its vendored
  manifest declares a dependency named nowhere else in the corpus, so that name can only reach
  a bundle through the reader that was dropping it.

#### 2026-08-01

- **`build` never deleted a page whose concept was gone, and strict `verify` exited 0 with
  the orphan present.** A renamed or deleted directory left the page behind forever. It was
  not an empty stub: it carried plausible `edges`, an `attributes` block, and a `resource:`
  naming a commit where the code really did exist, so it reads as authoritative — which makes
  an orphan more expensive than a missing page rather than less, and an agent handed one
  starts work against a module that is not there. Both gates were silent. On a real
  repository, `build` reported `342 page(s): 0 created, 342 updated, 0 unchanged` against a
  `.signpost/` holding 344 files, with `counts.nodes` at 339; the only signal was the
  bundle's own arithmetic, and nothing surfaced it.

  `build` now deletes a surplus page when, and only when, nothing on it came from a person:
  frontmatter with no unrecognised key and no `verified:` block, at least one managed region,
  and nothing outside those regions but headings and the notes invitation. Removals are named
  in the run's output rather than folded into the counts, because this is the one line in a
  build reporting a *deletion* and the name is what makes recovering the page from git
  possible. Anything else is kept and reported, as before. Every uncertainty — an unreadable
  file, an undeletable file, a skeleton from an older version — falls toward keeping it, and
  a markdown file somebody dropped into the bundle directory is never signpost's to remove.

  `verify`'s severity mirrors what `build` would do, which is what makes the finding
  actionable: a failure on a surplus page a build removes, because the remedy is
  `signpost build`, and a warning on one a build keeps, because no command can resolve it and
  a red gate with no supported fix is a gate people switch off. The corpus asserts both
  boundaries in one stage, because the defect is the pair — deleting unconditionally passes
  the first half and destroys somebody's `## Notes` on the first rename, which is the one
  failure the emitter exists to prevent. Recorded as
  [ADR 0010](docs/adr/0010-a-stale-page-is-deleted-only-when-nobody-wrote-on-it.md): signpost
  now deletes tracked files in a repository it does not own, and the never-delete rule it
  reverses was a documented convention.

- **`manifest.pages` omitted `practices.md`, in every bundle signpost had ever written.**
  This repository's own listed 32 pages against 33 on disk. The list is what a consumer is
  invited to read *instead of* walking the directory, so a list that does not name what was
  written is a claim the bundle makes and does not keep — and `verify` was green on all of
  them, because its byte comparison checks the manifest against a fresh render of *itself*
  and both sides agreed on the same wrong list. `verify` now compares the published list
  against the page set a build writes, in both directions, which is what found this. Fixing
  it changes `manifest.json` for every existing bundle: the first rebuild after upgrading
  adds the missing entry.

- **A Node builtin addressed by subpath was counted as a dependency signpost could not
  resolve.** `fs/promises` is the runtime; there is no other way to reach the promise-based
  API. The builtin table holds `fs`, and the whole specifier was looked up in it, so every
  subpath spelling missed and landed in the unresolved report. Ten are affected — the
  `/promises` variants of `fs`, `dns`, `stream`, `timers` and `readline`, plus `util/types`,
  `stream/web`, `stream/consumers`, `assert/strict` and `path/posix` — along with
  `node:test/reporters`, which needs the `node:` prefix trimmed before the subpath is cut.
  The first path segment is now what gets looked up, which is what the Python and Rust arms
  of the same function already did with their own separators.

  Cosmetic, uniquely among the defects fixed here: no node was fabricated and no edge lost.
  What it spent was the unresolved count itself, which is the one number telling a reader how
  much of their repository signpost did not understand — and a count inflated by things it
  understood exactly is a count nobody reads. On webex/webex-js-sdk, the repository where this
  surfaced: unresolved imports 16 → 9 across 11 → 9 specifiers, with nodes and edges unchanged
  at 834 and 5429.

  The rule cuts on the separator rather than matching a prefix, and the corpus asserts that
  boundary: `pathe/utils` is a real npm package opening with the four characters of the builtin
  `path`, and it must still be reported. A prefix comparison could not lose a declared
  dependency's edge — this test runs only after resolution has already failed — but it would
  silence an honest gap as the runtime, which is the same failure this fix removes, pointed the
  other way.

- **A named tsconfig `paths` alias resolved to nothing.** `compilerOptions.paths` is where a
  TypeScript codebase states what its own import specifiers mean — `@fider/services` is
  `public/services` because one line of one config says so, and nothing else in the
  repository states it. signpost never read the file. It guessed at a handful of bare
  prefixes (`@/`, `~/`, `#/` as repo-relative) and reported every named alias as unresolved.
  Measured on a real repository: 542 of 3912 edges absent, 14% of the graph, from a single
  unread mapping. After: unresolved imports 542 → 37, edges 3912 → 4024, and none of the
  remaining gaps is an alias.

  Reading the file needed three things that are not obvious from the format's name.
  tsconfig.json is JSONC, not JSON — both real configs that declared `paths` carried
  comments and one ended its `paths` object with a trailing comma, so a strict parse of
  either fails outright and the reader would have been silently useless on exactly the files
  it exists for. The stripper is string-aware, because `"https://aka.ms/tsconfig.json"` is a
  real value and a regex for `//` destroys the rest of that line; it blanks comments to
  spaces rather than removing them, and keeps newlines inside block comments, so every
  `paths` line number still points where a reader following it back to source expects.
  And `extends` has to be followed: 11 of 14 configs in one monorepo declare it, most of
  them declaring nothing else, so the aliases a package resolves by are stated two
  directories away from the files that use them. Aliases are matched most-specific-first on
  two axes — deeper scope, then longer prefix — and multiple targets for one pattern are an
  ordered fallback rather than a set, which is why they are exempt from `Normalize`'s
  sorting, the same as `Job.Steps`.

  A matched alias never falls through to the dependency lookup, even when its target holds
  no extracted source. The mapping is proof the specifier is first-party, so falling through
  would report the codebase's own directory mapping as a package nobody publishes — the same
  fabricated supply-chain entry the workspace fix below removed, reached by a different road.

  The corpus grew the shapes to express it: a JSONC config with four alias patterns, a
  package whose own config states only `extends`, a two-target pattern whose first target
  does not exist, an exact pattern with no wildcard, and a mapping onto an asset that is not
  extracted source. Resolution precedence is now written down in
  [Design §4.4](docs/design.md) — a declared mapping outranks a guessed one, within the
  repository-first rule that already governed the ecosystem lookups.

- **The corpus tested resolution only in the direction that could not catch over-matching.**
  Every assertion was a positive — this edge exists, that page exists — and a positive is
  satisfied by a resolver that claims *everything* exactly as well as by a correct one.
  Testing that 1+1 is 2 never catches an adder that answers 2 for everything. The failure it
  could not see is the worse one: not a missing edge but a confident wrong answer, either an
  edge into the repository that invents structure or an external node that invents a
  dependency nobody declared.

  Each language now carries a deliberate near-miss of a name that *is* declared, spelled the
  way that ecosystem's normalization is loosest about: go `example.com/corpus/greeterx/format`
  against the declared module `example.com/corpus/greeter`; typescript `@corpus/apples/juice`
  against the alias prefix `@corpus/app/`, separated only by a slash; python `httpx_extras`
  against the declared `httpx`, in the underscore spelling PEP 503 rewrites; rust
  `serde_yaml::Value` against the declared `serde`, in the spelling Cargo's dash/underscore
  equivalence accepts; and typescript `pathe/utils`, an npm package opening with the four
  characters of the Node builtin `path`. Alongside them, a standard-library import per
  language, which must produce neither a node nor a reported gap.

  Asserted as the unresolved-specifier *count*, in `TestCorpusResolvesExactlyWhatItShould`
  and in a new CI step against the shipped binary. The count is what fails in both
  directions — over-claiming lowers it, over-reporting raises it — and a count rather than a
  substring search because the printed report truncates to the five most frequent
  specifiers, so a grep for any single one passes by matching `and 2 more`. Verified by six
  mutations, each of which leaves the node and edge totals untouched at 25 and 24 and so
  passes every other assertion in the suite: comparing a Go module prefix by string instead
  of by path segment (7 → 6), comparing an alias prefix without its trailing slash (7 → 6),
  falling back to prefix matching in the dependency lookup (7 → 5), letting a matched alias
  fall through to the npm lookup (7 → 8), looking a Node builtin up by whole specifier
  (7 → 9), and matching one as a string prefix (7 → 6).

- **A monorepo's own packages were reported as third-party dependencies.** npm packages
  in a workspace import each other by published name — `import {x} from "@scope/core"`,
  not by relative path — and the resolver had no map from a declared package name to the
  directory declaring it. So every cross-package import fell through to the
  declared-dependency lookup, matched the `workspace:*` entry sitting in `package.json`,
  and resolved to an external node. Go was never affected for exactly this reason: it has
  had that map since the beginning.

  Two false claims, needing two fixes. The edges pointed at a fabricated external node
  instead of the module holding the package's own source, and the node itself was written
  as an External Dependency page for code in the repository. Measured on a real monorepo
  before the fix: 60 of 81 scoped externals were directories in `packages/`, 2064 edges
  pointed at them, and the module node for `@webex/webex-core`'s source showed zero
  importers while its fabricated twin showed 122. After: unresolved imports 43 → 15,
  module-to-module imports 614 → 1601, nodes 904 → 827, packages misreported as external
  60 → 0. The code-coupling hubs went from six external `/references/npm-*` entries to
  real first-party modules, with lodash and sinon still correctly external.

  A declaration is not discarded, only redirected: the `workspace:*` entry now draws an
  edge onto the module holding that package's source, which is what it was always
  describing, and is the part of a monorepo's structure stated nowhere else. Resolution
  precedence is now written down in [Design §4.4](docs/design.md) — this repository
  first, the manifest second, unresolved third — because its absence is what let an
  ecosystem ship without the map.

  The corpus grew a two-package workspace to express it, with both a bare and a deep
  cross-package import and a `main` pointing at a `dist/` that is not in the tree. One
  unit test over one resolver call could not have caught this: the defect needs a package
  that exists *and* is imported by published name, both at once.

- **signpost analysed its own bundle, inflating the file census on every run.** Only
  `.git/` was excluded from the walk. The bundle is committed on purpose ([ADR
  0005](docs/adr/0005-commit-the-bundle-to-the-repository.md)), so no `.gitignore` rule covers it the way
  one covers a build directory — which is exactly how this shipped — and the second run
  walked the pages the first run wrote. On a repository with 143 tracked files, the
  census went from `analysed 141 files` to `analysed 223`, the difference being the 82
  pages of the previous build.

  The graph was never affected: a bundle page produces no node, so every assertion about
  nodes and edges stayed green through the whole defect. What moved is the number a user
  has for judging whether the map covers their repository, and it moved *upward* — in
  the direction that reads as better coverage — growing every time the bundle grew.
  [Design §4.2](docs/design.md) requires that unmeasured never render as measured;
  self-measured is the same failure wearing a larger number.

  The bundle directory is now excluded from the walk, in neither the analysed set nor
  the skipped one: a skipped entry claims the repository contains something signpost
  declined to read, and these files are not the repository's content at all.

- **Every service declared in a compose file was reported as reading every secret named
  anywhere in that file.** The facts one reader produces are scoped to a file, and a
  secret reference carried no record of which service made it, so graph assembly
  attributed them through the only link available — the filename. A compose file with
  eight services and nine credentials between them gave all nine to all eight. On the
  repository where this surfaced, a Caddy reverse proxy with no `environment:` block at
  all was described as reading the database password, the session secret, and the SAML
  certificate.

  Nothing leaked: these are names, never values, and that invariant was never in
  question. What was wrong is the blast radius, and it was wrong in the direction that
  matters. "This service reads that credential" is a fact a reader acts on without
  re-deriving it, so an invented one says a credential is reachable from somewhere it is
  not — which is how a threat model, an incident scope, or a least-privilege review ends
  up drawn around the wrong set of services. A missing edge prompts a question; a
  fabricated one prompts a conclusion.

  A secret reference now records the service that makes it, and each service's page
  gets its own references plus the file's unattributed ones — a compose top-level
  `secrets:` block names credentials for the file without saying which service reads
  them, and dropping those would trade a false claim for a missing one. A reference
  naming a *different* service is never included. The same attribution now applies per
  document in a multi-document Kubernetes file, where a reference found in one resource
  was previously a fact about all of them.

- **A bundle checked out with CRLF line endings was reported as stale on every page,
  and the remedy the message named did not work.** Every claim signpost makes about an
  existing bundle is a byte comparison against freshly generated content, and the
  emitter writes LF. So a checkout under git's `core.autocrlf=true` — a default many
  Windows installs select — differed from a rebuild on every line, and three things
  failed at once: `verify` reported "a build would change this page" for every page in
  a bundle that was byte-identical to what a build produces; `build` rewrote all of
  them each run instead of reporting them unchanged; and `build` claimed "N page(s) had
  human notes, carried across" on a bundle with no human notes at all, because
  `HumanText()` differed only by its line endings.

  The verify failure was the one that had no way out. "run `signpost build` and commit
  the result" writes LF, git converts it back on the next checkout, and the gate stays
  red — so a user following the instruction exactly ended up where they started. On CI
  it failed a pull request that changed nothing, on every page.

  Pages are now normalised to LF when read, so a CRLF checkout is recognised as
  up-to-date. Scope is deliberately narrow: a lone CR is left alone, since no git
  conversion produces one and a bare CR in a page is a byte somebody put there on
  purpose; and signpost normalises to *compare*, not to convert, so a page whose
  content matches is not rewritten and keeps whatever line endings its owner's git
  chose. §6.1's invariant is intact — human text is never modified, only decoded on
  read, the same way a Windows editor's UTF-8 BOM is stripped.

  **This repository could never have caught it in CI**, which is the part worth
  recording: `.gitattributes` here pins `* text=auto eol=lf`, so the bug is invisible
  in a repository already configured against it — and every repository is unconfigured
  on the first day signpost runs in it. Fixing it in the tool rather than only
  recommending `.gitattributes` is what makes a bundle correct before anyone configures
  anything. The recommendation stands anyway, for the ordinary reason that pinning LF
  keeps diffs readable. Documented in [design §6.4](docs/design.md).

### Notes

#### 2026-08-03

- **Commit `2785918` closes issues that do not exist, and the history is not being
  rewritten to fix it.** Its last two trailers name issues 29 and 35, which are ids from a
  local task list rather than this repository's issues — those stop at
  [#14](https://github.com/3rg0n/signpost/issues/14). GitHub renders both as links to 404s
  and closed nothing. The two real changes are the telemetry work and the empty-manifest
  lockfile fix, both described in this file under 2026-08-02.

  Nothing is unrecoverable about the numbers themselves — but they cannot be made to
  refer to anything either. GitHub shares one sequence between issues and pull requests,
  and this repository has merged 8 pull requests and opened 14 issues, so 29 and 35 are
  permanently unassignable: filing an issue now yields 15.

  **The rewrite is the wrong fix, and the cost is countable.** Rewriting `2785918` rewrites
  the three commits after it, and one of those carries the bundle: 53 of its files name a
  `git://github.com/3rg0n/signpost@<sha>` provenance stamp, 52 of them in a `resource:`
  key. [ADR 0007](docs/adr/0007-the-bundle-names-the-commit-it-describes.md) puts that
  stamp *inside the bytes being compared* — it is what makes a committed artifact
  auditable without running anything — so a `filter-repo` pass converts a wrong link in a
  commit message into 53 pages naming a commit that no longer exists, in the artifact this
  project exists to produce. Every external reference to those shas breaks with them. That
  is a real cost paid to correct something already inert. So the record is forward-only:
  this entry is the correction, and the trailers stay as they were written.

  **What changes is that it cannot happen again.** CI now reads the `Closes`, `Fixes` and
  `Resolves` references of every commit in a pushed range and asks GitHub whether each
  number exists, failing the run when one comes back 404. Asked of the API rather than
  compared against a highest-known number, because a number *below* the ceiling can still
  be wrong — a deleted issue, or one transferred away — and a ceiling learned from `gh
  issue list` is stale the moment somebody files something. The endpoint covers pull
  requests too, which share the sequence, so a `Fixes` on a PR number is accepted as the
  valid reference it is.

  **The check failed its own commit, and that is the more useful finding.** The first
  version of this entry and of the commit message carrying it both quoted the bad
  reference verbatim to explain the defect. GitHub reads closing keywords *anywhere* in a
  commit message — not only in a trailer block — so the sentence describing the bug was
  itself a live closing instruction. The step reported it against the commit adding the
  step. Nothing closed, because those numbers still resolve to nothing, but the same
  paragraph written about a real issue would have closed it silently while claiming to
  document why closing it was wrong. There is no position in a commit message that quotes
  a closing keyword safely, so the convention in `CONTRIBUTING.md` is to name the commit
  rather than the reference. `owner/repo#n` and full issue URLs also close, in the
  repository they name; the check does not cover them, because its token is scoped to this
  one and a number this repository cannot resolve is correct there.

## [0.1.0] — 2026-08-01

The deterministic core, complete. v0.0.1 named the three things that had to land
before this number was honest — `signpost build`, `signpost verify`, and git signal
extraction — and all three are here, along with the OKF emitter with human-edit
preservation, the `signpost.yml` workflow that keeps a committed bundle honest, the
graph viewer, and an opt-in semantic pass that stays off unless a backend is
configured *and* the flag is passed.

No model and no network are required for anything in this release. `go.mod` still has
no `require` block.

### Added

#### 2026-07-31

- `practices.md`, a bundle page stating what the repository declares about how it is
  worked in: build and test commands, which CI jobs can block a merge and which run
  outside that gate, whether dependencies are pinned by a lockfile and updated
  automatically, ownership rules, licence, security policy, observability, ADRs, and
  agent instructions. New `internal/practice` package; design §9.1.

  **Findings, never a score.** No level, no grade, no rubric — a 1–5 scale is an
  opinion that has to be defended and re-tuned per repository, and a repo at "level
  2" reads as *measured* when it has only been *judged*. That is the exact failure
  the confidence model exists to prevent, so the page states what it found and what
  it did not find, and stops there.

  **Absences are stated, not implied by silence.** A page that only ever reported
  presences would render a missing security policy identically to one it never looked
  for. Each pillar reports both ways, with the file that grounds it.

  Deterministic, so it runs on every `build` and `verify` rather than behind a flag —
  it reads manifests the discovery pass already opened and asks no model anything.

- A multi-language test corpus and the CI job that runs signpost against it:
  `testdata/corpus` is a synthetic repository with all four first-class languages,
  four manifest ecosystems, a Compose file, two workflows, and the filenames that
  break naive emitters. `go test ./cmd/signpost -run TestCorpus` exercises it
  in-process on all three platforms; a new `corpus` CI job additionally checks the
  emitted YAML with PyYAML.

  **Dogfooding has a ceiling, and this is what clears it.** Signpost runs on
  signpost in CI, which is a real test of the paths *this* tree contains — and this
  tree is Go with kebab-case filenames. Self-hosting structurally cannot reach the
  TypeScript, Python, or Rust extractors, cannot reach an npm, Cargo, or pyproject
  manifest, and cannot reach a path carrying a YAML indicator: zero of signpost's
  171 tracked paths contain `[`, `]`, `{`, `}`, or `,`. Issue #9 lived on the far
  side of that ceiling, and no additional unit test would have found it either,
  because nobody had thought of the character.

  **PyYAML rather than signpost's own reader, deliberately.** That reader is
  tolerant by design (ADR 0001) — built to keep reading past what a conforming
  parser rejects — which makes it the wrong instrument for proving its own output is
  well-formed. PyYAML has no stake in signpost being right.

  **Assertions are named facts, not counts.** A count assertion fails on every
  improvement to an extractor, which trains people to update the number rather than
  read the diff, and it never says which fact was lost. The strongest check here is a
  frontmatter round-trip that validates the *key set* of every edge mapping, and it
  is strong precisely because it needs no advance knowledge of the offending
  character: an unexpected key means a scalar terminated where the emitter did not
  intend, whatever caused it. Every path-injection defect so far — a newline, a
  backtick, a `](`, then a bracket — was found by a person imagining the character,
  and that does not scale.

  Both gates were mutation-tested rather than assumed. With the fix reverted the Go
  test fails on all three hostile pages and the CI job reports 3 of 26; with only the
  comma case reverted, both still fail on it — which is the case an earlier,
  parse-only version of the check let through silently.

  It earned its keep before a single assertion was written: its first run found the
  two `Fixed` defects below that no unit test and no dogfooding run could reach. One
  needs a multi-ecosystem repository, the other needs a bundle built and then
  verified end to end.

- `manifest.Diag.Malformed` distinguishes a document no parser can read from one the
  tolerant reader merely stepped over. Both were notes, and they are different
  claims: a Helm directive or tab indentation is ADR 0001's tolerance working as
  designed and the document is still valid YAML, where an unterminated flow
  collection means everything after it is lost to any reader. The flag is set even
  when the note itself is dropped by the note cap — a capped note costs a location,
  a dropped flag would silently turn an unreadable document back into a clean one.

- The semantic pass, wired into `build` and shipped: `signpost build -semantic`
  summarises what each module is *for* with the configured model backend, and
  writes the prose into the bundle. New `internal/semantic` package, new
  `.github/workflows/signpost-semantic.yml` running it weekly. Design §4.5 and §8.

  **Opt-in twice over.** A backend has to be configured — `none` is the default
  and nothing infers one — *and* `-semantic` has to be passed. The two gates
  answer different questions: the environment says a model is available, the flag
  says this run should spend it. Without the second, anyone who configured a
  backend to try `signpost model check` would find every subsequent build calling
  it. It also keeps the per-push build byte-stable, because the difference between
  the two CI workflows lives in a workflow file rather than in an environment
  variable somebody can set repository-wide. `-semantic` with no backend is exit
  2, not a silent deterministic build — a scheduled job that reports success while
  summarising nothing is worse than one that fails.

  **The prose lands in a `role` region of its own**, beside the deterministic
  `summary` rather than replacing it, and that is what makes it survive. A managed
  region present on disk but absent from a fresh render is kept verbatim, so a
  deterministic build renders no `role` and carries the scheduled run's across
  untouched — where writing into `summary` would put it in a region every build
  does render, and the next push would overwrite it with the placeholder. The
  separation also keeps counted facts and a grounded guess visually apart on the
  page, the latter carrying a line naming the model and the files it read.

  **A response is not trusted because it parsed.** Every claim cites files, the
  citations are checked against the exact set that was sent, and one invented path
  drops the whole summary rather than the citation — a summary with a bad citation
  removed reads exactly like one that was grounded all along. Bounds are schema
  constraints, not requests in prose, because `maxLength` is enforced by the
  sampler where "one or two sentences" is a hint; prose that came back over the
  bound, or cut off at it, is refused rather than repaired. Cached entries are re-grounded when read, since a cache file is a
  file in a working tree. And every HTML comment is stripped from the prose before
  it is written: text containing a region's own close marker would otherwise close
  that region, turning everything after it into human text signpost then refuses
  to overwrite — a permanent foothold for whoever can talk a model into emitting
  one string.

  **It cannot fail a build**, and that is a compile-time fact rather than a
  convention: the pass has no error return. A backend that goes away stops the
  pass and names the modules that were never attempted; a response that cannot be
  grounded drops only that one. What was skipped is reported on stderr and
  deliberately not silenced by `-quiet` — a fail-open pass whose failures are
  quiet is the one failure mode that looks like success. It is not recorded in the
  bundle, because `manifest.json` is regenerated wholesale and a skip record there
  would churn on every later push and turn the staleness gate red over a run that
  is finished.

  Scoped to role summaries only. Invariant extraction, doc-to-code linking, and
  cluster labels remain specified and unimplemented; the second is the valuable
  one and deserves better than being bolted onto a pass built for one-paragraph
  answers.

  The workflow keeps AWS credentials out of Actions, per the standing rule. It
  reads an OpenAI-compatible endpoint and key from repository variables and
  secrets; Bedrock over OIDC is not offered, because the bearer-token surface this
  backend uses is gated on `bedrock:CallWithBearerToken`, which the usual OIDC
  role pattern does not grant. A repository with no backend configured skips with
  a message rather than failing, so a fork does not inherit a red weekly cron.

- Zoom and pan in the graph viewer: wheel to zoom, drag to move, `+` / `−` /
  reset buttons, and the arrow keys to pan from the keyboard. Everything drawn
  sits inside one SVG transform group, so node and edge coordinates stay in
  layout space and a magnified view is the same picture rather than a second
  layout that could disagree with the first.

  Hand-rolled, as ADR 0008 requires — the viewer has no dependencies and this did
  not change that. The details that make it usable rather than merely present:
  zooming holds the point under the cursor, so what you were looking at does not
  slide away; the view is clamped so the frame stays covered and there is no
  panning into empty space; the wheel is only claimed while zoomed in, so page
  scrolling still works over a graph at 100%; a drag that moves does not also
  select the node it ended on; and the transform is held outside the render path,
  so toggling a filter no longer discards the reader's position.

- Model backends for the semantic pass, behind one `Backend` interface: `inferd`
  over local IPC, `openai` against any OpenAI-compatible endpoint, and `none`.
  New `internal/model` package. Both implementations are first-party code over
  the standard library — no SDK, so the empty `require` block ADR 0002 records
  stays empty.

  **The default is `none`, and a credential alone never changes it.** Enabling a
  backend takes explicit configuration. Inferring one from a stray environment
  variable would mean a build that silently spends tokens and ships repository
  content to a third party because something unrelated was set;
  `AWS_BEARER_TOKEN_BEDROCK` is exactly that kind of variable. Credentials are
  read from the environment and never from a file, because `.signpost.yml` is
  committed and a config format with a place to put an API key eventually has
  one in it. See ADR 0009.

- `signpost model check` sends one probe through the whole path — system prompt,
  wrapped untrusted source, defanging, JSON Schema, response parse — and exits
  non-zero when the backend does not work. It exists because the semantic pass
  fails open by design, which is right for a build and useless for someone
  trying to find out why their bundle has no semantic pages. It reports three
  separate facts rather than a bare "ok": which model answered, that the schema
  held, and that the model reported the probe's embedded injection attempt as an
  observation instead of complying.

- Prompt-injection fencing for repository content. Files reach a model inside
  delimited `<untrusted_source path= sha256=>` blocks with signpost's own
  delimiters and chat-template control tokens neutralised by an inserted
  zero-width space — inserted rather than deleted, because a summary built from
  text with holes in it describes a file that does not exist. Markers that occur
  legitimately mid-sentence, like `### System:`, are defanged only when alone on
  a line, so an honest file documenting a prompt format is not corrupted. The
  grounding rule at emit remains the backstop.

  Anyone who can land a comment in a repository can write text addressed to the
  model rather than to a human reader, and the output is committed and read by
  agents that act on it. That makes this a supply-chain path, not a curiosity.

- Bedrock reached without the AWS SDK, verified live. A Bedrock API key is
  minted from an IAM principal and sent as `Authorization: Bearer`, so SigV4 is
  not needed and `net/http` is sufficient. Four findings are recorded in
  `docs/design.md` §5 because none is guessable: the working path is
  `/openai/v1` rather than `/v1`; `bedrock-runtime` and `bedrock-mantle` are
  separately authorised, and bedrock-runtime is what an ordinary account already
  has; no Amazon generative model is on the Chat Completions surface at all, so
  Titan and Nova are not options there; and model ids reject both the `:0`
  suffix and the `global.` cross-region prefix, so the configured id is passed
  through verbatim.

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

- The `signpost` workflow rebuilds the bundle on push to the default branch and
  checks it on pull requests, which is what makes the committed artifact useful in
  a repository where nobody installed signpost ([ADR 0005](docs/adr/0005-commit-the-bundle-to-the-repository.md)).
  Two jobs with deliberately different strictness, because §8.0 keeps the bundle
  off branches so two branches cannot conflict inside `.signpost/`:

  - **build**, on push to `main`, is the only place the bundle is ever written. It
    builds the binary from this repository rather than installing a release — a
    repository that analyses itself must use the code it currently contains, or the
    bundle describes one thing while being produced by another — then verifies
    *strictly*, and commits only when the bytes changed. A run that committed
    regardless would put a no-op commit on the default branch for every push.
  - **verify**, on pull requests, checks the bundle's content and writes nothing.

  All three loop guards [ADR 0005](docs/adr/0005-commit-the-bundle-to-the-repository.md)
  requires are present, since a workflow that commits to the repository triggering
  it will otherwise trigger itself: `paths-ignore` on `.signpost/**`, a skip when
  the actor is the bot, and `[skip ci]` on the bot's commit. The actor is tested
  rather than the commit message, because a message is content a contributor can
  copy by accident. Convergence was rehearsed against real git rather than
  reasoned about: five consecutive runs produced one commit, two no-ops, then one
  commit for a code change and one more no-op.

  The pull-request job skips when the *base branch* has no bundle yet, which is the
  state every repository is in on the day it adopts signpost. Found by opening the
  pull request that added the workflow — the only way it could have been found — where
  the gate failed telling a contributor to run `signpost build`, the one thing §8.0
  forbids on a branch. The condition is deliberately narrower than "is there a bundle
  here": a pull request that *deletes* the bundle still fails, because the base has it
  and the tree does not. An unresolvable base ref fails loudly rather than skipping,
  since that is a fault in the checkout rather than a fact about the repository, and a
  gate that quietly skips when it cannot see is the false pass `verify` exists to
  prevent.

  Two details are load-bearing and neither is obvious. The commit step stages
  before it asks, because `git diff` reports modifications to tracked files and
  says nothing about new ones — on a first-ever run, when the whole bundle is
  untracked, a plain `git diff --quiet` reports no change and the bundle would
  never be committed at all. And the push is `--force-with-lease`: concurrency
  queues runs but does not stop a human pushing between this job's checkout and
  its push, and overwriting someone's commit to land generated markdown would be
  the worst possible trade. Both jobs check out with `fetch-depth: 0`, because a
  bundle built from a shallow clone records thinner history than the repository
  has while carrying an identical commit stamp.

- `signpost verify -as-of-bundle` compares the bundle's content while taking
  provenance from the bundle's own record, which is what a pull-request check
  needs and what lets a single developer build locally and commit the bundle with
  their code. Recorded as [ADR 0007](docs/adr/0007-the-bundle-names-the-commit-it-describes.md),
  because it is a public contract about what `verify` promises.

  It exists because a consequence of §8.0 is not optional. The bundle is built on
  the default branch only, so on a branch its committed stamp names an older commit
  *by construction* — and the stamp is part of every page's bytes, so a strict
  verify calls every page stale on every pull request, including one that changed
  no code at all. Measured on a documentation-only branch: exit 1, five problems,
  and the only difference in the diff was the sha. A gate that is red on a typo is
  a gate people switch off, after which the staleness check this tool is built
  around is gone. The single-developer pattern is blocked the same way and worse:
  building locally and committing the bundle alongside the code stamps the
  *parent*, because the sha of the commit carrying the stamp does not exist when
  the stamp is written.

  It does not weaken the gate. Only the two provenance fields come from
  `manifest.json`; content is still compared byte for byte against a fresh render,
  so a branch that changes what the map says still fails — measured on a branch
  adding a package: `modules/b.md: the repository has this concept and the bundle
  has no page for it`. Provenance is read from the manifest rather than from a page
  because the manifest is the one file in the bundle no human has a claim on, and a
  missing or unparseable one adopts nothing and leaves the strict comparison in
  place, which then reports it. Nothing validates the recorded sha against git: the
  manifest reaches the tree the same way every other file does, through a commit,
  which makes it exactly as authoritative as the source being analysed — and a
  forged stamp can mislabel which commit produced a page but cannot hide a stale
  one, because the content comparison runs either way.

  It is a flag rather than the default, and it names the commit it judged against
  in the run's `skipped:` output. The default stays strict because on the default
  branch signpost is the thing that *writes* the stamp, so something has to check
  that what it wrote is true — which is not hypothetical, per the fix below.

  One boundary is documented rather than worked around: a bundle built locally and
  committed atomically with its code still shows drift in history attributes, since
  a directory inside the pending commit gains a commit once it lands. You cannot
  record a commit's history before making it. Build with `-no-history` for a
  structure-only bundle that verifies clean atomically, or commit the code first
  and the bundle second, which is what CI does.

- A graph viewer at [`/graph.html`](https://3rg0n.github.io/signpost/graph.html),
  and a switch in the top bar of both pages for moving between the overview and
  it. The graph is the thing the tool produces and the landing page could only
  describe it, so the page that advertises the tool now shows its actual output on
  its own repository: every node, edge, attribute, and file path comes from
  `signpost export -format json`, generated at deploy time and never committed
  (`site/graph.json` is gitignored). Nodes are filterable by kind, edges by kind,
  and selecting one shows what signpost read about it — attributes, files linked
  to the repository, and incident edges.

  It lives in `site/` with **no `package.json`, no lockfile, and no JavaScript
  dependencies**, which is [ADR 0008](docs/adr/0008-the-viewer-lives-in-this-repository.md)
  superseding [ADR 0006](docs/adr/0006-the-generator-and-the-viewer-are-separate-repositories.md).
  0006 split the viewer into its own repository on the premise that anything worth
  using needs a dependency tree the generator cannot afford to govern. The real
  export is 25 nodes and 27 edges across three node kinds; a node-link view over
  that is hand-written SVG, and the premise did not survive the measurement. A
  real graph library would need a new ADR, at which point the split becomes live
  again — "just one small dependency" is how the property in ADR 0002 decays.

  Four decisions worth knowing, each of which is a defect if it goes the other
  way:

  - **Every string in the viewer came out of a repository**, which is a real
    injection surface. There is no `innerHTML` anywhere in `graph.js`: nodes are
    built with `createElement` and `textContent`, class and attribute values are
    restricted to `[a-z0-9-]`, and file paths are `encodeURI`d before going into
    an `href`. The CSP (`default-src 'none'`, no `unsafe-inline` in `script-src`)
    is the backstop for a bug in that discipline, not the defence itself.
  - **Layout runs per connected component.** A single force pass cannot place a
    node with no edges — nothing balances the repulsion, so the 16 edgeless nodes
    pushed to the frame edge and collapsed the connected core into a corner. The
    nodes with edges get a force layout; the nodes without get a captioned band
    that says so. "signpost read no edges here" is a finding, and a tidy labelled
    row states it where a scatter around the rim looks like an accident.
  - **Co-change edges have no arrowheads.** The relation is symmetric — two
    directories changed in the same commits — and the generator records it as a
    pair of directed edges. Drawing a head, or listing both directions in the
    detail panel, would assert a direction the data does not carry, so reciprocal
    same-kind edges fold to `↔`.
  - **The arrangement is deterministic.** A seeded generator, never
    `Math.random()`: someone who reloads and sees a different picture cannot tell
    whether the repository changed.

  The Pages workflow now builds the binary, exports the graph, and fails rather
  than publishing an empty one. It checks out with `fetch-depth: 0` because
  co-change comes from `git log` — a shallow clone would publish a graph quietly
  missing an entire edge kind. It deploys on every push rather than on a `paths:`
  filter: the graph derives from the whole tree, so a filter naming source
  directories goes stale the first time one is added. This workflow is still not a
  required check, so a broken deploy cannot fail a merge — the isolation ADR 0006
  wanted is this workflow's topology, not a repository boundary.

### Fixed

#### 2026-07-31

- Sample projects under `testdata/` were analysed as though they were the repository
  being described, putting modules and dependencies on committed pages that the
  repository does not have.

  signpost found this by biting itself. Adding `testdata/corpus` — four sample
  projects deliberately built to look like real repositories — put
  `testdata/corpus/ts/app/(marketing)` in signpost's own index as a module and react,
  httpx and serde in it as external dependencies. Worse, it reached `practices.md`,
  which cited `testdata/corpus/py/pyproject.toml` as evidence about how *signpost*
  pins its dependencies. A bundle is committed and read by people who did not build it,
  so a page naming a dependency that appears in no `go.mod` is not cosmetic noise —
  it is the false grounding the design exists to prevent.

  `testdata/`, `fixtures/`, and `__fixtures__/` are now recorded but not analysed,
  the same treatment vendored code already got, and named as
  `test fixture directory` in the skip list rather than folded into "vendored" —
  telling somebody their own reviewed fixture is third-party code would be a wrong
  explanation of a right decision. `-include-fixtures` recovers a directory genuinely
  named `fixtures` that holds shipping code.

  A fixture is deliberately neither of its neighbours. Not vendored: it is this
  repository's own hand-maintained code. Not a test either — a test *exercises* the
  repository's surface and earns a `tested_by` edge pointing at it, while a fixture is
  the *subject* of a test, and an edge from a real module to a sample project would be
  a false claim.

  **This changes what a bundle contains.** Any repository with a sample project under
  `testdata/` loses those phantom modules and dependencies on the next
  `signpost build`. signpost's own bundle went from 56 pages to 34 and its practices
  page from "17 declared, 1 not declared" to "10 declared, 5 not declared" — the
  second number rising is the point, because the corpus had been supplying evidence
  about a repository that does not exist. This does not affect the corpus harness,
  which copies the corpus to a root of its own where those files are the surface
  rather than a sample.

- A path containing a YAML flow indicator made a page's frontmatter unreadable from
  that line down. Issue #9. `app/tools/[slug]/page.tsx` — an ordinary Next.js
  dynamic route — was written unquoted into an `edges[]` flow mapping, where `[`
  opens a flow sequence. The mapping never terminated, so a conforming parser lost
  every edge from that point on, and four pages of a real repository lost seven
  edges between them.

  The quoting rule tested indicators against the first character only, which is
  correct for a block scalar and wrong for a flow one. In a flow context `[`, `]`,
  `{`, `}`, and `,` end the current scalar *wherever they appear* — position is not
  part of the rule. Now any path carrying one is quoted.

  A comma was the worse half of the same bug and the reason the fix covers all five
  rather than just brackets. An unquoted `[` raises, so it is at least loud: the
  document is unreadable and every parser says so. An unquoted `,` parses **clean**
  and silently splits the scalar — `source: py/greeter/data,notes.py` reads back as
  `source: py/greeter/data` with an invented `notes.py:` key beside it. Consumers
  get a source path naming a file that does not exist, and nothing anywhere reports
  a problem.

  `verify` reported the bracket case as a warning and exited 0, which is what let it
  reach a commit. That half is tracked separately; a `Malformed` flag now
  distinguishes an unparseable document from a construct the tolerant reader stepped
  over (see Added).

- `verify` rejected a bundle `build` had just written. `build` ran the practices
  pass and `runVerify` did not, so verify re-rendered a bundle with no
  `practices.md` and reported the difference twice over — once as an orphan page
  "describing a concept this repository no longer has", and once as an index a build
  would change. Neither message named the cause, because neither was about the
  repository. Both commands now share one `addPractices` helper, so the two cannot
  drift again.

  This is the shape of failure verify is structurally prone to: it works by
  rendering the bundle the current tree would produce and comparing, so any page one
  command emits and the other does not is reported as a property of the repository.

- Every ecosystem's dependencies were reported as unpinned in a repository that
  commits lockfiles, alongside "a unknown lockfile is present with no manifest
  beside it". The practices pass paired lockfiles to manifests on the ecosystem
  field of the Fact, and `internal/manifest` deliberately does not parse lockfiles —
  they are derived, often megabytes, and carry no architectural signal — so that
  field was never set. Pairing is by basename now, with both spellings of each
  ecosystem name in one file next to each other: a mismatch does not fail loudly, it
  reports `go.mod` as unpinned in a repository that commits `go.sum`, which is a
  false accusation rather than a missing line.

- A truncated model summary was committed as though it were complete. Found by
  running the semantic pass against a live OpenAI-compatible backend for the first
  time — the pass shipped wired but exercised only against a fake, and this is what
  the first real run turned up.

  The schema bounds `role` with `maxLength`, and the response path already refuses
  a summary that comes back over it or with `finish_reason: "length"`. But a
  backend has a third option nobody had accounted for: enforce the bound by
  *cutting the string at it* and returning the prefix. That arrives as valid JSON,
  of a legal length, with `finish_reason: "stop"` — so grounding passed, the length
  check passed, and the page got a sentence ending mid-word. Five of twelve modules
  on this repository, each landing at exactly the 400-character cap, ending on
  `edge confidens` and `dependencies that are v`. A reader sees prose that stops,
  which reads as a module whose purpose is genuinely undescribed past that point
  rather than as a fault — and §4.5 promises a summary is dropped rather than
  softened.

  Refused now, on two signals together rather than either alone. Length near the
  cap is not enough: a model that uses its whole budget and finishes its sentence
  has answered well, and refusing that would throw away the most complete summaries
  in the bundle. Missing terminal punctuation is not enough either — prose
  flattened from a list legitimately ends on a list item, and refusing over
  punctuation is a stylistic judgement §4.5 does not authorise. Their conjunction
  is specific to the failure: text that ran to the cap *and* did not finish a
  sentence is text the backend stopped writing. The threshold carries slack rather
  than testing the exact cap, because a backend that reserves a byte for a
  terminator or counts UTF-16 units cuts near the limit rather than on it, and a
  check keyed to the exact number would pass all of those through.

  `promptVersion` is bumped to `role/2`, so a cache written before the fix is not
  served past it — the cache key is what keeps a truncated summary from outliving
  the check that would now refuse it.

  Verified against the live backend: the six modules whose summaries finished are
  kept and unchanged, the six that were cut are refused and named on stderr. No
  truncated prose ever reached a commit; the bad run was local only.

- Every skipped-module line in the semantic pass printed its reason twice — `not
  summarised: /modules/okf not summarised: …`. `semantic.Run` writes entries that
  are already complete sentences naming what it did not do, and the CLI prefixed
  them again. Worse on the other two kinds of entry, where the added prefix was
  simply false: a pass that stopped early, and a cache that could not be written,
  are not modules that were not summarised. The prefix is gone and the entries
  print as written.

- A filename could take permanent control of a bundle page. Managed regions are
  found by matching a line against a marker, and a POSIX filename may contain a
  newline — so a file named
  ``a.go\n<!-- /signpost:managed:structure -->\nb.go`` put a line of the
  repository's choosing inside the region that lists it, closing that region
  early. Everything below the injected marker became human text, which signpost
  then refuses to overwrite: the page stopped regenerating, and nothing said so.
  No model was involved; adding the file was enough.

  Three findings, one cause — generated text reaching the page without the
  assumption that it holds no marker ever being enforced:

  - The file list on every module page, in shipped code, since the emitter
    landed. This is the pre-existing half.
  - A cite path in a role summary, in the semantic pass added the same day.
    Grounding checks that a cited path is one signpost sent, and a path that
    really is in the tree passes that check while still carrying the newline.
  - A page's `## Title` heading, which is *human* text by design — a title comes
    from a directory name and a reader may rewrite it — so it does not pass
    through the managed-region guard at all. A directory named with a newline and
    an **opening** marker made the parser read a region starting in human text,
    which then swallowed the real region below it.

  Fixed at the two chokepoints all generated text passes through rather than at
  the call sites, so a future region inherits the guard instead of needing to
  remember it: `<!--` is escaped in every managed region and in every generated
  heading, and a heading is folded to one line. A path that would break the code
  span it is written into — a newline, a backtick, a control character — is
  quoted rather than dropped, because a filename with a newline in it is a fact
  worth showing plainly. The semantic pass additionally *refuses* a summary whose
  cite path carries marker syntax, keeping "dropped, not softened" the rule on
  that side; the two checks are independent on purpose. The refusal is narrow —
  control characters and backticks only — so paths with spaces, accents, and
  ordinary punctuation are still cited.

  Verified inert on real content: rebuilding this repository's own bundle
  produces no escape artifact on any of its 32 pages, and `verify` passes.

- A directory name could aim the bundle's links wherever it liked. Markdown link
  syntax is positional, so a directory named `x](https://evil.example/y)(` closed
  the label of every link that named it and turned what followed into the target,
  leaving the real target trailing as inert prose. `verify` passed clean, because
  the link it was asked to check is well-formed and resolves — the forged one is a
  different link. This affected the index, the hub list, and the structure region
  on every page that links to the directory; the bundle is committed and often
  published, so these are links other people follow.

  Every generated link now goes through one function that backslash-escapes
  brackets and parentheses in the label. Escaped rather than stripped, using
  markdown's own mechanism, so a title with a legitimate bracket in it — `api
  [v2]` — still renders as what somebody typed. Only labels need it: a link's
  target is a node ID that `assemble` builds, not text read from the tree.

  The export formats were already correct here and are unchanged — Mermaid, DOT,
  and GraphML each escape per-format, with a test covering exactly this class of
  title.

- The graph viewer crowded its nodes into the middle of the frame and let their
  labels overlap. Two separate causes, both measured against the rendered page
  rather than guessed at:

  - Fitting a component into its cell preserved aspect ratio, so the shorter axis
    always bound. The force pass settles this repository's one connected component
    roughly square, the cell it goes into is much wider than it is tall, and the
    height bound: ten nodes were drawn across 320 of 1000 available units, with
    680 units of width left empty. The axes are now scaled separately, bounded to
    2.6× apart so a wide cell cannot flatten a component into a line. This is
    sound specifically because the force pass spaces node *centres* and has no
    notion of the label — the aspect ratio it happens to settle into is not a fact
    about the graph, while which nodes are near which is, and that survives.
  - Nothing anywhere in the layout knew how wide a label is. Neighbouring nodes
    settled 51–52 units apart while `internal/assemble` renders at about 113
    units, so the text had no room not to collide. Spacing is now decided on the
    footprint a node and its label occupy together, and a final pass measures
    every pair directly and separates any that overlap along whichever axis is
    closer to clear.

  Zoom was not the fix for this and could not have been: scaling magnifies the
  dot and the text together, so two labels that overlap at 100% overlap
  identically at 600%. Zoom makes a graph navigable; only spacing makes it
  legible.

- The graph viewer's frame was a fixed height that a larger repository than
  signpost's own overflowed, silently. Both halves of the frame were affected and
  both are now sized to what they hold, growing the picture rather than crushing
  its contents:

  - A repository with enough edgeless nodes needed a band taller than the whole
    frame. Subtracting it left a negative main area, and from there negative cell
    heights, negative scale factors, a section rule drawn 208 units above the top
    edge, and — measured on a graph of 86 nodes — 26 nodes placed at negative
    coordinates, outside the viewBox, where a browser draws nothing at all. Those
    nodes were not crowded or misplaced; they were absent, with no indication that
    anything was missing.
  - Many small components each got a cell too small to hold them: sixteen
    six-node components were given cells 191 by 108 units, which fit three
    long-name footprints and were asked to fit six. Separating overlapping labels
    can only move them into space that exists, so no amount of it resolved this —
    measured at 64 overlapping label pairs, now 0. The height needed is computed
    from the footprints before anything is placed.
  - The number of columns those cells were arranged in ignored label width. A
    square-ish grid put a hundred components in ten columns, each cell 55 units
    wide holding a 136-unit label, and growing the frame downwards cannot fix a
    cell that is too narrow — the frame's width is the page's and does not grow.
    Measured on 500 nodes: 386 overlapping pairs, now 0. Columns are now capped
    at what a cell wide enough for the widest label allows, and the rows follow.

  The two functions that decide the grid — one asking how tall the frame must be,
  one placing the nodes in it — now derive it from a single shared function. They
  previously each computed it, which is a correctness requirement rather than a
  tidiness one: a height computed for a grid other than the one drawn is not an
  answer about the drawing.

  The width stays fixed at the page's width; only the height grows, and the SVG
  scales to its container as before. Nothing about the rendering of signpost's own
  graph changes — same 660-unit frame, same coordinates to the byte.

- Clicking a node did nothing after panning the view and zooming back out. The
  flag that stops a drag from selecting whatever node it ended on was only ever
  cleared by a node click, so a drag ending on empty space left it set and
  swallowed the next real one. It is now cleared by any press on the graph,
  whether or not that press can begin a drag.

- Four extractor defects, found by scoring the Rust and TypeScript extractors
  against independent reference parsers over real third-party source rather than
  against hand-written fixtures. Fixtures only test the forms someone thought to
  write down; every one of these is a form that occurs in ordinary code and that
  no fixture contained.

  - Rust: a re-export with restricted visibility was dropped entirely.
    `pub(super) use wire::{A, B}` and `pub(crate) use x::Y` are imports, and the
    dispatch matched a literal `use ` / `pub use ` prefix, which cannot see a
    parenthesized visibility clause. It now goes through the same keyword
    classifier every other item uses, so the visibility form is irrelevant to
    whether the line is recognised.
  - Rust: `const unsafe fn f()` reported a constant named `unsafe`. `const` is
    both an item keyword and a function modifier, and it was treated as a
    modifier only when the literal word `fn` came next — so `const unsafe fn` and
    `const extern "C" fn` were read as const items named after the intervening
    modifier. This is the failure the line-oriented design cares about most: not
    a missed declaration but an invented one, a symbol the file never wrote.
  - TypeScript: a dynamic `import("m").then(...)` at the start of a line lost the
    dependency. The statement branch claimed any line beginning with `import`,
    the statement parser correctly rejected it as an expression, and the line was
    consumed before the expression form could run. The branch now tries both.
  - TypeScript: `declare module 'ext' { }` reported nothing at all. An ambient
    module names its target with a string literal rather than an identifier, so
    the name was rejected as not an identifier — meaning a `.d.ts` whose entire
    purpose is to type an untyped package came back with an empty surface. The
    recovered declaration is public surface whether or not it carries `export`,
    because a quoted name makes it an ambient *external* module: it types the
    package for every file in the program by existing, nothing imports it, and
    `export` does not apply. A bare identifier is a namespace instead and keeps
    the ordinary rule.

- Sentinel defanging matched case-sensitively, so `</UNTRUSTED_SOURCE>` in a
  repository file passed through intact and closed the wrapper — landing
  everything after it in the trusted region of the prompt, which is exactly the
  escape the wrapper exists to prevent, spelled in capitals. Matching is now
  case-insensitive across every sentinel, and the casing a file used is preserved
  when the match is rewritten, so defanging stays invisible to a human reading
  the generated page.

  The forged-role-heading rule had the same shape of gap: it compared each line
  against a fixed list of exact strings, so `## System:`, `###  System:` with two
  spaces, and `### system` without a colon all survived. It now matches the
  line's shape — hashes, optional space, a role word, optional colon — and still
  leaves an ordinary Markdown heading alone. Found by probing the implementation
  rather than by reading it; the old code leaked all 15 cases the new tests
  assert.

- `internal/vcs`'s test repositories no longer inherit the machine's git
  configuration. They pinned identity and dates already but not hooks or
  maintenance, and both leaked: a global `core.hooksPath` is inherited by any
  repository created under it, so a developer with a secret-scanning pre-commit
  hook paid that scan on every commit the suite makes — enough to take the
  package from seconds to past the test timeout under `-count=2`. Separately,
  `git commit` starts `git maintenance run --auto` detached, and on Windows the
  handles it holds under `.git` block the directory removal `t.TempDir`
  registers, so cleanup failed with "The directory is not empty" against
  whichever test finished first. That presented as a flaky assertion and was
  neither flaky nor an assertion. Isolation is now set through the environment
  rather than `git config`, because the shallow-clone test runs git against a
  directory the helper never touched.

- The test timeout in CI and release is raised to 30 minutes from the 10-minute
  default. `internal/vcs` runs the real git binary for every case, twice, and a
  Windows runner is slow enough at process creation that the default left no
  margin. Raised rather than removed, so a genuine hang still fails the job.

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

- The bundle stamped the wrong commit, one behind, forever. The identity came from
  `HEAD`, and committing the bundle advances `HEAD` — so a committed bundle named
  the commit before the one carrying it, the next run re-stamped, and that commit
  moved `HEAD` again. The artifact never converged: `verify` failed on every
  committed bundle and the workflow committed on every push in perpetuity, which is
  precisely the commit churn [ADR 0005](docs/adr/0005-commit-the-bundle-to-the-repository.md)
  identifies as the fastest way to kill adoption.

  The identity is now the newest commit that changed something other than the
  bundle, per [ADR 0007](docs/adr/0007-the-bundle-names-the-commit-it-describes.md).
  A commit whose only effect was regenerating the description did not change the
  code being described; one that changed code *and* the bundle is a code change and
  does move it. A repository containing nothing but a bundle falls back to `HEAD`,
  since git reports an all-excluded pathspec as empty output and exit 0 rather than
  as an error, and an unstamped page would claim less than the tool knows.

  Found by the strict `verify` in CI, which is the argument for keeping it strict
  on the branch that writes the stamp: a content-only comparison would have had
  both sides wrong in the same way and reported nothing. The bundle directory name
  is now load-bearing in `internal/vcs`, which cannot import `internal/okf` — it
  reads the graph `vcs` feeds — so the constant is duplicated with a test that
  fails if the two ever disagree. A silent rename would otherwise leave the
  exclusion pointing at a path that no longer exists and stop convergence with
  every test still green.

### Changed

#### 2026-07-31

- **`verify` now fails, rather than warns, on a page whose frontmatter no conforming
  YAML reader can read.** This is the half of issue #9 that let the defect reach a
  commit: the emitter wrote an unparseable page, the checker read it, and the
  disagreement was reported at a severity that let CI go green. A bundle is committed
  and read by people and agents who did not build it, so a checker that calls an
  unreadable page a nit is the false pass verify exists to prevent (§4.6).

  A page the tolerant reader merely *stepped over* still warns and still passes, and
  keeping that split is the reason this took a new flag rather than a severity bump.
  Failing on every note would fail builds over a hand-edited block ADR 0001 says to
  tolerate, and a gate that fires on legitimate input is a gate somebody turns off.

  The check runs before the "is this a mapping" test, because a broken document still
  reads back as a mapping: an unterminated flow collection loses everything after it
  and what remains is a well-formed map of whatever came first. That is exactly how
  issue #9's pages passed.

  **This can turn a previously-green bundle red.** If it does, the bundle was already
  wrong and its consumers were already losing edges silently — `signpost build` with
  this release rewrites the affected pages correctly.

- The site is served from **`signpost.md`**. `site/CNAME` carries the domain, which
  is what makes the setting survive: GitHub Pages reads the custom domain from a
  file in the published artifact, so a deploy from a `site/` without one clears the
  domain configured in the repository settings and quietly reverts the site to the
  `github.io` address. Both pages gained a `rel=canonical` and the landing page's
  `og:url` now names the apex, because both hostnames keep resolving and serving
  identical bytes — which is exactly the case a canonical tag settles.

#### 2026-07-30

- Dependabot and Renovate both wait seven days before proposing a version. The
  threat this addresses is not a bad release but a hostile one: an account takeover
  publishes a malicious version, and the window between publication and the ecosystem
  noticing is measured in hours. A tool whose whole posture is "dependencies we can
  patch ourselves"
  ([ADR 0002](docs/adr/0002-patchable-dependencies-not-zero-dependencies.md)) should
  not also be the fastest adopter of a version nobody has looked at yet. Security
  advisories are exempt in both tools, which is the right asymmetry — a known CVE is a
  reason to move faster, not to wait. Both were flagged by `semgrep --config=auto`,
  which is now clean.

- CI gates both installers as pure ASCII and parses `install.ps1` under
  **Windows PowerShell 5.1 as well as pwsh 7**, on a Windows runner. The
  previous check ran only under pwsh 7, which defaults to UTF-8 and parsed the
  broken file happily — a gate that passed on a script the majority shell could
  not run. The ASCII check avoids `grep -P`, which is absent from some builds
  and errors out under `LC_ALL=C` in others, and it branches on the exit code
  explicitly: 1 is clean, 0 is a finding, anything else means the check itself
  broke. An `if grep ...` would have read that error as "nothing found".

- The landing page's hero and status table were describing a repository two weeks
  behind the one they sit in — 11 nodes and 13 edges against a real 25 and 27 —
  and the table still advertised the viewer as a separate repository. Both now
  match `signpost graph .`, which matters more than it did: the numbers are one
  click away from the rendered graph that would contradict them.

- Below the viewer's three-column breakpoint the plot scrolls horizontally against
  a minimum width rather than scaling to fit. The whole picture is one viewBox, so
  a phone-width column shrank the 11px node labels to about 5px, which is not
  reading. A graph you pan is usable; a graph whose labels you cannot read is not.

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
