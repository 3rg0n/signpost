# 33. The binary replaces itself only from a verified release

## Status

Accepted

## Context

The failure a stale signpost produces is not a version number nobody reads. It is:

```
signpost: unknown command "view"
```

which reads as a missing feature, or a typo, or a broken install — anything except a binary
from a fortnight ago. ADR-era work on `signpost version` made that answerable: the banner
and the verb now report a revision and a build date rather than the bare `dev`. But
answering it leaves the reader mid-task with a tool they now know is old and a README to go
and find, in a browser, to fix it.

Two distribution facts constrain what can be done about that:

- **The artifacts are GitHub Releases.** `.github/workflows/release.yml` cross-compiles six
  targets, archives them with `LICENSE` and `README.md`, writes `checksums.txt`, and calls
  `gh release create`. There is no GHCR image and no package registry entry. Pulled
  templates from GHCR were considered and declined; the only `ghcr.io` strings in the tree
  are Terraform test fixtures.
- **`install.sh` and `install.ps1` already perform the whole transaction.** They resolve
  the tag from the redirect on `/releases/latest`, download the archive and
  `checksums.txt`, and refuse on three separate conditions before anything is written.

So the question was not whether to build a distribution channel — there is one — but
whether the binary may replace itself using it, and under what rules. A self-updating
binary is a trust boundary: it fetches an executable over the network and puts it where the
user's shell will run it. Getting that wrong is not a bug in a feature, it is a supply-chain
vector wearing this tool's name.

The counter-argument is real and was weighed: the safest self-update is none, and the
installers already work. What decides it is that they work by being *typed*, and the reader
who needs them is the one who does not know they need them.

## Decision

**`signpost update` replaces the running binary from a GitHub Release, and the release is
verified before anything is written.** The transaction is `install.sh`'s, performed by
`internal/selfupdate` in the same order and with the same refusals.

**Three refusals, none of them a warning.** No `checksums.txt` published; the platform's
archive not listed in it; a digest that does not match. Each is a distinct error because
each means something different to whoever reads it — a broken release, a partial publish, a
corrupted transfer or a substituted archive. An unverified binary is the one outcome worse
than a stale one, so none of the three degrades to a prompt.

**Verification happens inside the download, not after it.** `Download` returns the archive
only once the digest matches, so no caller can be written that unpacks first and checks
second. The ordering is a property of the API rather than a rule callers are asked to
remember.

**The release source is hard-coded and not configurable.** No flag, no environment
variable, no config key names the host. A configurable update source turns one mistyped
hostname, or one poisoned CI environment, into arbitrary code execution as the user. Tests
reach the seam through an unexported struct field, which is not a surface a user can set.

**The tag from the network is validated before it enters a URL.** It arrives in a
`Location` header — remote input — and is concatenated into a download path, so a tag of
`../../../etc` would build a URL outside the release. Only `v` followed by alphanumerics,
dots, dashes, and plus signs is accepted.

**Nothing runs unless it is typed.** No auto-update, no background version check, no
telemetry ping, nothing on a timer. A tool that changed its own behaviour between two runs
of a build would be worse than a stale one: the build that passed this morning and fails
this afternoon, with no commit between them, is the hardest kind of failure to be told
about.

**No privilege escalation, ever.** A binary in a directory that needs elevation fails with
the permission error and a pointer back to the installer. A tool that acquired privilege to
overwrite itself is a pattern nobody should have to trust, and the escalation prompt is
indistinguishable from the one an attacker would want the user trained to accept.

**Write-then-rename, in the target's own directory.** The rename is within one filesystem
and therefore atomic: an interrupted update cannot leave a partial binary where the shell
will find it, and a process already executing the old binary is never written into. Windows
refuses to rename over a file open for execution, so the old binary is moved aside first
and restored if the install rename fails.

**Symlinks are resolved, and the link is preserved.** A version manager may put signpost on
the `PATH` as a link into a versioned directory; writing over the link would replace it
with a regular file and detach it from whatever manages it. The update lands on the real
binary.

**The asset naming is a contract between two files that cannot import each other, so a
test holds it.** `release.yml` builds asset names in shell; `internal/selfupdate` predicts
them in Go. A disagreement is undetectable at run time — a renamed asset is a 404, which
reads as a network fault or a missing release rather than as the rename it is, and it would
break every platform at once with nothing failing anywhere. The contract test reads the
workflow's own `name=` line, extension rule, and target list, and derives the expectation
from them for all six published platforms rather than the one the test runs on.

## Consequences

**There is now a second path by which a signpost binary comes into existence, and it must
not drift from the first.** The mitigation is that it is the same transaction, in one
package, with the verification rules stated once. The risk that remains is prose: if
`install.sh` changes a refusal and this does not, the two disagree. That is a review
obligation on any change to either.

**`signpost update` cannot verify what it cannot see.** It checks that the archive is the
one the release published — nothing more. It does not check that the release was built from
this repository's source by this repository's workflow, because nothing in the release
attests to that today. Sigstore or provenance attestation would raise that ceiling and is
not in this decision.

**A user on a distribution channel signpost does not know about will be told the wrong
thing.** Somebody who installed via a package manager, once one exists, gets a binary
replaced underneath that manager's records. The symlink resolution covers the version-manager
case; a real package manager would need `update` to decline, and it cannot detect one yet.
Documented rather than solved.

**The verb is reachable, which is not free.** A command in this CLI exists only once it is
in `commands()`, and `runUpdate` compiled cleanly and passed `gofmt` for an hour before
anything called it. `TestUpdateIsListedBesideVersion` asserts the table entry, because a
verb absent from it is dead code that its own tests still pass.

**Four outcomes, four sentences, and the quiet ones matter most.** Already-current, a dry
run, a development build replaced, and a forced reinstall of the same version each read
differently, and two of them cannot be reached through a test that goes over the network.
A first real run reported `updated v0.1.0 to v0.1.0` for the repair case, which describes a
change of version that did not happen; the reporting is split from the command so all four
are asserted directly.

Design reference: [docs/design.md](../design.md) §1, and `install.sh` / `install.ps1` for
the transaction this mirrors.
