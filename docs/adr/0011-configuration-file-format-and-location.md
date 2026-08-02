# 11. Configuration lives in .signpost.yml, and a config key may only change a default

## Status

Accepted

## Context

signpost is configured by flags and environment variables today. A config file has been
specified since the first design pass — `docs/design.md` §5 shows a `.signpost.yml` with
`backend`, `model`, and `budget` keys — and never built.
[ADR 0009](0009-the-semantic-pass-is-opt-in-and-egress-is-explicit.md) settled one rule
about it in advance (credentials never live in a file) and explicitly left the rest open.

This is owed now rather than later because it blocks restructuring the CLI. A config file
changes what a flag *means*: `-include-vendored` currently reads as "analyse vendored code
in this run," and the moment a file can set it, the flag also has to answer "override
whatever the file said." Deciding the verb surface without knowing that is deciding it
twice.

Four forces, and they do not point the same way.

**The bundle is committed and read by people who did not build it.** That is the property
[ADR 0005](0005-commit-the-bundle-to-the-repository.md) exists to protect, and it makes a
committed config file more consequential here than in most tools. A checked-in
`.signpost.yml` becomes part of what a reader has to know to understand why a page says
what it says. If a repository's config can silently change the *shape* of the analysis —
which files are read, whether history is analysed — then a page and the tree it describes
can disagree for reasons invisible in the page.

**`verify` is a gate, and a gate whose configuration is data is a gate that can be
disabled.** `signpost verify` exits non-zero when the bundle no longer matches the tree,
and CI depends on that. If a config key could set `-as-of-bundle`, or lower a threshold,
then a pull request could weaken its own gate by editing a file in the same commit — and
the gate would pass while reporting that everything was fine. This is the same shape as the
committed-bundle threat: whoever can land a file can change what the tool concludes.

**Reproducibility is a hard requirement (§8.1), and config is an input.** A bundle is
byte-stable given the same tree, and a config file is part of the tree. That works in our
favour — the file is committed, so it travels with the repository, and two people running
`signpost build` in the same checkout get the same bundle. It stops working the moment
config is read from outside the tree: a `~/.config/signpost.yml` would make the same
checkout produce different bundles on two machines, and the diff would be unexplainable
from anything in the repository.

**A tool with no config file pushes complexity onto every caller.** signpost's own CI
passes `-repo` in five places across four workflows, and a repository with a vendored
directory that must be analysed has to remember `-include-vendored` on every invocation
by every developer and every workflow. That is exactly what a config file is for, and
refusing one on purity grounds would be its own failure.

There is also a smaller, concrete force: signpost already owns a YAML emitter and a
tolerant YAML reader, and adding a config file must not add a dependency
([ADR 0002](0002-patchable-dependencies-not-zero-dependencies.md)).

## Decision

**Location: `.signpost.yml` at the repository root, and nowhere else.** No user-level file,
no `XDG_CONFIG_HOME`, no `--config` pointing outside the tree, no directory walk upward
past the repository root. Configuration is a property of a repository, not of a machine or
a person, because the bundle it produces is committed and shared. A config search path is
how the same checkout starts producing different bundles for two people, and §8.1 does not
survive that.

The leading dot matches `.signpost/`, the file is read from the same root the walk is
confined to, and its absence is the supported normal case — not a warning.

**Format: the same narrow YAML subset signpost already emits, read by the reader in
`internal/manifest`.** Scalars, flow mappings, block sequences. No anchors, no multi-line
scalars, no `${VAR}` interpolation — the design sketch showed `${SIGNPOST_OPENAI_API_KEY}`
and that is withdrawn, because ADR 0009 forbids the key being in the file at all and an
interpolation syntax exists mainly to put secrets in files. An unreadable or malformed
`.signpost.yml` is a **usage error, exit 2** — not a fallback to defaults. Silently
ignoring a config file somebody wrote is worse than refusing to run.

**Precedence: flag > environment > file > default.** One order, everywhere, no exceptions
per key. The reasoning is that this is the order of *specificity of intent*: a flag is this
invocation, an environment variable is this shell or this job, a file is this repository, a
default is this tool. It is also the order every caller already assumes.

**A config key may only change a default. It may never change what a flag or a gate
means.** This is the load-bearing half of the decision and the reason the ADR blocked #28.
Concretely, three classes:

- **Configurable** — keys that set the default for an *analysis-shaping* flag:
  `include_vendored`, `include_fixtures`, `ignore`, `no_history`, `max_commits`, `repo`,
  `backend`, `model`. Each is a thing about the repository that is the same for every
  caller, and each is already visible in the bundle: `manifest.json` records the skip
  report and the walk's own options, so a page and the tree cannot disagree invisibly. A
  flag on the command line always wins.

- **Not configurable, ever** — anything that changes whether a check *fails*.
  `-as-of-bundle`, `-fail-on-cycle`, and any future threshold are flags and environment
  only. A repository must not be able to weaken its own gate by committing a file, because
  the reviewer reading the diff sees a config change and CI reports success, and neither
  signal says the gate got quieter. `verify`'s severity model is a contract with CI, not a
  preference.

- **Not configurable because it is not repository state** — `-quiet`, `-o`, `-format`,
  `-verbose`, `-top`. These are properties of one invocation by one caller. A file that set
  `quiet: true` would make every developer's terminal lie about coverage for reasons in a
  file they did not read.

**Credentials are environment-only, restating ADR 0009 rather than reopening it.** There is
no config key for an API key, a token, or a credential path, and the reader rejects the
`openai.api_key` key by name with a message saying why. A format that has nowhere to put a
secret is the only format that reliably has no secrets in it.

**The file is analysed like any other manifest, not exempted from it.** `.signpost.yml` is
repository content; the walk reads it, and it appears in the bundle's census like anything
else. signpost does not get to be invisible in its own map.

## Consequences

**#28 can now be decided.** Every flag in the CLI is in exactly one of three classes, which
is what a verb restructure needed: a grouped subcommand inherits its group's configurable
defaults, and the non-configurable flags stay where they are regardless of where the verb
moves. The restructure is now a naming and dispatch question rather than a semantics
question.

**A repository can shape its own analysis, and that is visible in the artifact.** A
monorepo that vendors a package it genuinely maintains sets `include_vendored: true` once,
and every developer and workflow gets the same walk. The skip report in `manifest.json`
already records what was and was not read, so the reason a page exists stays recoverable
from the bundle.

**A repository cannot quiet its own gate, and someone will want to.** The request will
arrive as "we have one unavoidable cycle, let us configure `fail-on-cycle` off in the
file." The answer is that the flag exists and the workflow can stop passing it — a change
to CI, which is reviewed as CI. Moving that into repository data makes the gate's strength
a property of the thing being gated.

**Two config surfaces exist during the transition, and the environment one stays.**
`SIGNPOST_BACKEND` and friends are not deprecated by this: CI configures signpost through
the environment, and that is the layer above the file. What changes is that a repository
now has a place to state its own shape.

**An invalid config file fails the build, including in CI.** Exit 2, the usage category, so
the failure is distinguishable from a broken repository (exit 1). This is the deliberate
cost of not silently ignoring the file, and it means a typo in `.signpost.yml` stops a
build rather than quietly producing a differently-shaped bundle.

**Reading config is one more thing that happens before the walk, and it must not need the
walk.** The file is read directly from the root, not from the discovery result, because
discovery's options come *from* it. A small ordering constraint, recorded because it is
easy for a later refactor to invert.

**Nothing in the design's `budget` key is settled here.** `budget: { max_calls,
max_tokens }` appears in §5's sketch and is a semantic-pass concern; it is configurable in
principle under the first class above, and it is not specified or built. This ADR decides
where config lives and what it may do, not the full key set.

Design reference: [docs/design.md](../design.md) §5, §8.1.
