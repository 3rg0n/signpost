# Security

## Reporting a vulnerability

Report privately through GitHub's
[security advisory form](https://github.com/3rg0n/signpost/security/advisories/new)
rather than as a public issue. Include the repository shape or input that
triggers it if you can — a minimal directory tree and the command you ran is
usually enough to reproduce.

Expect an acknowledgement within a few days. signpost is maintained by one
person, so a fix lands as fast as the fix is understood rather than on a
schedule.

## What signpost does with your code

Worth stating plainly, because the answer determines most of the threat model:

- **signpost reads a repository and writes markdown.** It does not execute
  anything it finds — no build, no test run, no script, no plugin. There is no
  `eval` path and nothing in the output is executable.
- **Symlinks are recorded and never followed.** Every read goes through an
  `os.Root` handle scoped to the walk root, so a path that tried to escape the
  repository is refused below the code rather than by it.
- **Reads are bounded.** Per-file size and line caps, a total-byte budget,
  binary detection, and irregular files skipped, so a hostile or malformed tree
  cannot exhaust memory or block the walk on a FIFO.
- **Secrets are recorded as references, never values.** A Kubernetes Secret
  contributes its name and its key names; an `env_file` is recorded by path and
  never opened. This matters because the bundle is committed and often
  published, so a reader that captured a value would be a credential-disclosure
  path wearing a documentation tool's clothes. There is a test that flattens the
  entire fact struct to a string and searches it for the secret bodies, so a
  leak through any field fails the build rather than only the fields intended to
  hold one.
- **No network in the deterministic pass.** `signpost graph`, `signpost export`,
  and `signpost build` make no outbound connections. The semantic pass (v0.2) is
  the only code that talks to anything, and it is opt-in.

## Prompt injection

The bundle is read by agents that act on it, which makes the generated artifact
a target: text in a scanned repository — a rules file, a comment, a README —
could try to plant instructions that survive into the bundle and are read back
as guidance.

The mitigations are in `docs/design.md` §4.5 and §11. In summary: repository
content going to a model is wrapped in a hash-stamped delimiter block with
role-turn and delimiter-forgery sequences defanged; sampling is
schema-constrained; and text from rules files is captured as a quotation
attributed to its file, never as guidance the tool adopts.

**This is mitigated, not solved**, and the design says so rather than claiming
otherwise. If your repository cannot accept the residual risk, run signpost with
no backend configured: the deterministic pass produces a complete structural
bundle with no model in the path at all, and there is nothing to inject into.

## Supported versions

Fixes land on `main` and ship in the next tagged release. Pre-v1.0 there is no
backport branch — upgrade to the latest release.

## Verifying a download

Every release publishes `checksums.txt` alongside the archives, and both
installers verify the SHA-256 before unpacking and refuse to install on a
mismatch. If you download manually, check it yourself:

```bash
sha256sum -c checksums.txt --ignore-missing
```

GitHub Actions used in this repository are pinned by commit SHA rather than tag,
because a tag is mutable and has been moved in a real supply-chain compromise.
