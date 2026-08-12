# 28. Scaffolded files are embedded and tested against our own

## Status

Accepted

## Context

`signpost init github` writes two files into somebody else's repository:
`.github/workflows/signpost.yml` and `.signpost.yml`. Until it existed, the README's answer
was to copy this repository's workflow by hand.

Copying is the thing that actually failed. The workflow carries the three loop guards and
the strictness split of design §8.0, and the reasons for all of them live in comments — so a
partial transcription looks like a working file. A repository that dropped `paths-ignore`
gets a workflow that retriggers on its own commit; one that put `-as-of-bundle` on the push
job gets a gate that cannot catch a wrong stamp; one that left it off the pull-request job
gets a gate that is red on every pull request, which
[ADR 0027](0027-a-gate-fails-only-on-what-the-reader-can-fix.md) is entirely about not doing.
None of those announce themselves as transcription errors.

Two questions had to be settled to ship it, and they are the ones this record exists for:
where the template lives, and what stops it from drifting away from the workflow we run.

Shipping a scaffold creates a failure mode the tool did not previously have. A template that
diverges from our own workflow is worse than no template, because we are then distributing
advice we do not follow — and the divergence surfaces as somebody else's gate behaving
differently from ours, in a repository we cannot see, reported by somebody who has no reason
to suspect the file they were handed. That is the hardest kind of bug to be told about.

The alternative to embedding, considered seriously, was publishing the templates as a tagged
OCI artifact in GHCR and having the binary pull the release matching itself. The attraction
is real: the templates stop being a build-time input, and a fix to the workflow reaches
existing installs without a new binary.

## Decision

**The templates are embedded in the binary with `go:embed`.** Four grounds, in order of
weight:

They do not version independently. A scaffolded workflow installs a pinned signpost release,
and a template that installs `vX` is only correct for `vX` — so decoupling the two buys
nothing and adds a way for them to disagree. The reachability argument cuts the other way
once that is true: a template fix that reaches a `v0.1.0` install produces a workflow
pairing new instructions with an old binary, which nothing tested.

It would make `init` the only command that touches the network, in a tool whose entire
posture is that it does not. `build`, `verify`, `graph`, and `view` all run offline; a
command that fails behind a proxy would be the exception nobody expects.

It would put an unauthenticated fetch in the path of a command that writes a file requesting
`contents: write`. Doing that safely needs signature verification, which needs a
verification dependency — and design §2 will not take a dependency we cannot patch
ourselves for something this small.

And `site/` is already embedded, so the bytes would be carried twice: roughly 57KB against
an ~11MB binary.

**The template is compared to `.github/workflows/signpost.yml` on every build.** Structure,
not bytes, because one difference is intended: this repository builds signpost from its own
source — a repository that analyses itself has to use the binary it currently contains —
and a scaffolded repository installs a pinned release. Everything else is the design and
must agree.

Three properties make that test worth having rather than decorative:

- Each anchor is asserted in **both** files. An anchor removed from ours fails as "this
  test's expectations are stale" rather than passing quietly, which is the failure mode of
  every test that checks a copy against a constant.
- The intended difference is **asserted**, not tolerated. A future edit bringing
  `go build -o signpost` into the template fails with the reason.
- Assertions are measured over command lines with comments stripped. Both files explain
  their own decisions in prose, so a substring search over the raw text measures the
  comments — which is what two of these assertions did on their first run.

**It previews by default and writes only with `-y`.** The output requests `contents: write`
and pushes to the default branch, so typing the command correctly must not be enough to
install it. A prompt was rejected rather than overlooked: a prompt needs a terminal, so it
either behaves differently under CI and in a pipe or it needs TTY detection tests cannot
exercise. Printing and stopping is the same guarantee with no hidden state.

**Nothing is overwritten, and the refusal covers both files or neither.** A plan that skipped
the blocked file and wrote the other leaves a repository with a config file and no workflow —
whose bundle silently stops being rebuilt, the exact failure the command exists to prevent.
The blocked case exits 0: the files being present is a state somebody can legitimately be in,
and a scaffold that fails when the thing already exists is one every caller has to guard.

**The install step verifies what it downloads.** The archive and the release's
`checksums.txt` are fetched directly and checked with `sha256sum -c` before anything is
unpacked, in both jobs. `curl … install.sh | sh` was the first version and is wrong for a
reason worth recording: `install.sh` verifies the archive's SHA-256, but the script doing the
verifying would itself have arrived unverified, inside the one job holding `contents: write`.
A checksum is worth nothing when the code comparing it is fetched the same way.

**`repo:` is derived from `origin`, and the output says it was derived.** A git remote is a
property of the checkout and a fork's remote names the upstream, so a derived value is a
proposal the reader has to agree with — the same defect that moved this key out of the
workflow in the first place. `.git/config` is parsed rather than shelling out to `git`, so
`init` works in a checkout on a machine without git installed; credentials, ports, and the
`.git` suffix come off, so a token in a remote URL cannot reach a committed file.

## Consequences

Adopting signpost is one command, and the workflow an adopter gets is the one we run against
ourselves rather than the one they managed to transcribe.

The parity test is now a constraint on editing our own workflow. A change to
`.github/workflows/signpost.yml` that touches an anchor fails until the template is changed
with it, which is the point — but it means the file is no longer only ours, and anyone
editing it has a second file to think about. The test says so in its failure message rather
than leaving that to be discovered.

Fixing a scaffolded workflow requires a release. There is no channel to reach an install that
already exists, which is the accepted cost of not fetching. A repository that already ran
`init` keeps the file it was given; the command will not overwrite it, so a fix reaches them
only if they ask for it.

The version in the template has to be raised deliberately at each release, and a release that
forgets leaves new adopters installing an old signpost. A test asserts the value is a release
tag rather than `latest` — the pin is what stops a scaffolded bundle's bytes from changing
because signpost changed on a day nothing in that repository did — but no test can know which
tag is current.

The asset name is hardcoded to `linux_amd64`, correct only while `runs-on` names a
linux/amd64 runner. Those two lines are far apart in the file and nothing about editing one
suggests the other, so a test ties them together; the failure without it is
`cannot execute binary file`, which says nothing about this workflow.

This is the first thing signpost writes outside `.signpost/` and outside the repository it
was built in, and `internal/scaffold` is where that decision now lives. A second scaffold —
`init pages` is the one expected — inherits all of it: embedded, previewed, never
overwriting, and tested against something real rather than against a constant.
