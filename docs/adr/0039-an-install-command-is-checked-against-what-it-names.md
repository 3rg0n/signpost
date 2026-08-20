# 39. An install command is checked against what it names

## Status

Accepted

## Context

[ADR 0037](0037-the-landing-page-is-gated-on-its-verdicts-not-its-words.md) gated the landing
page's status table and its pasted run, and left the install commands alone with the rest of
the prose. The argument there was that gating prose means asserting on wording.

A command is not prose. It is quoted text a reader copies into a terminal, it has to be right
character for character, and every claim it makes has an answer in this tree:
`raw.githubusercontent.com/3rg0n/signpost/main/install.sh` is a script that exists or does not,
and `go install github.com/3rg0n/signpost/cmd/signpost@latest` names the module in `go.mod` or
does not. The page and the README hold the same three commands today because somebody typed
them twice, and a rename of either script would make both documents wrong at once — in the one
section where being wrong costs the reader a failed command rather than a false impression.

[ADR 0038](0038-documented-languages-are-checked-against-the-registry.md) settled the general
question: a documented claim with a machine-readable answer is gated against the answer, not
against another document's copy. This applies it to the install section.

## Decision

Two tests in `site/install_test.go`.

**Parity.** Every command the page shows appears verbatim in the README's `## Install` section,
and every way to install in that section appears on the page. HTML entities decode first,
because the PowerShell command contains an `&` that the page must escape and the reader's shell
must not see escaped.

A command in that section beginning with `signpost` is something you run after installing, not
a way to install, so it is not expected on the page. Nothing else in the section can be, since
signpost is not on the reader's path yet.

**What the commands name.** The scripts named across both documents are exactly the `install.*`
files in the repository root, so a renamed, added, or deleted installer fails here rather than
404ing in a terminal. Each fetch URL's owner and repository, and the `go install` target, are
compared against the module path in `go.mod`. Both forms are counted, because a check that runs
zero times passes.

Labels stay free, as in ADR 0037. The page writes `macOS, Linux` where the README comments
`# macOS / Linux`, and neither is more right than the other.

## Consequences

A renamed installer, a moved command, or a repository rename fails a pull request. The failure
names the command, so the fix is a paste rather than a search.

Two things this does not catch. A way to install that names no file in this repository — a
package manager, say — is only held by the parity check, and only once the README states it;
the disk comparison has nothing to anchor it to. And the branch in the fetch URL is not checked,
because nothing in the tree says which branch is published.

The prose in the install section stays ungated. Whether the scripts verify a SHA-256 digest is
a claim about what `install.sh` does, and the assertion for that belongs in a test of the
script, not in a reader of the page.
