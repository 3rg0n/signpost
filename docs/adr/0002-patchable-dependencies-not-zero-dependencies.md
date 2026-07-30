# 2. Patchable dependencies, not zero dependencies

## Status

Accepted

## Context

signpost compiles a repository into a knowledge artifact: it walks a tree, parses four
languages, reads manifests, builds and clusters a graph, and emits markdown. Every one of
those capabilities has a mature third-party implementation, and at least one adjacent tool
already does the whole job. The question this decision answers is not "how many
dependencies" but **who owns the remediation path when a CVE lands.**

The obligation is asymmetric, and the asymmetry is the whole argument.

**A direct library dependency is a version bump we control.** When a CVE is published
against a library in `go.mod`, the remediation is `go get -u`, run the gate, ship. That is
a same-day action on our own schedule. Dependabot and Renovate are enabled on the first
commit precisely to make this routine — the posture is a commitment to *remediate*, not a
claim to have no exposure.

**A tool dependency is someone else's release cadence.** Adopting a comparable upstream
tool means inheriting its entire tree: a C parsing core plus one grammar per language, a
graph library, a clustering implementation that pulls a JIT compiler, and a JS graph
library for the HTML view. None of that is ours to patch. When a CVE lands in it, the
options are to carry a local patch indefinitely or open a PR upstream and hope it merges.
Neither is a remediation path that can be committed to on an internal SLA.

**Transitive depth multiplies the problem invisibly.** A single direct dependency with a
deep tree is not one obligation, it is every obligation in that tree, arriving without
warning and without a bump we can make ourselves. The count that matters is not the
`require` block's length but the closure's.

**And this tool runs in CI on protected branches.** A compromised or unpatchable
dependency in a binary that gates merges is a supply-chain position in every repository
that adopts signpost. That is a materially worse blast radius than the same dependency in
an application.

Three options were considered.

1. **Adopt an existing tool and wrap it.** Fastest to a working product, and it forfeits
   the remediation path entirely. Rejected: the reason to build rather than procure is
   control of remediation, and wrapping surrenders exactly that.
2. **Use libraries freely, keep the count small.** The conventional posture. Workable, but
   it has no mechanism — "few dependencies, each justified" decays within a quarter unless
   something enforces it, and by then the closure is large enough that removal is a
   project rather than a decision.
3. **Stdlib-first, each direct dependency justified in an ADR and gated in CI.** Slower to
   write, and it means hand-writing things that exist off the shelf.

What made (3) affordable is that the stdlib covers more of this problem than it appears
to. Go parsing is `go/parser` and `go/ast` — a full-precision AST, free. The graph
algorithms are roughly 600 lines of textbook work. Louvain clustering is about 200 lines
against a JIT-compiler toolchain. None of those is research; each is a known algorithm
with a known implementation, and writing it is genuinely cheaper than owning a dependency
for it.

## Decision

**Every direct dependency must be one we can bump ourselves, and the count must stay small
enough that bumping stays routine.** The rule is not zero dependencies.

Consequently, as of this decision, `go.mod` has no `require` block at all — the
deterministic core is entirely stdlib. That is an outcome of the rule, not the rule
itself: the threshold each candidate has to clear is high enough that nothing has cleared
it yet.

Three things enforce it, and the enforcement is the part that matters:

- **A new direct dependency requires an ADR**, written before the code, naming what the
  dependency buys and what the stdlib alternative costs.
- **CI fails a pull request that adds a direct dependency without one.** The `require`
  block is diffed against the base branch; a new entry with no ADR touched in the same PR
  fails the build. A reviewer looking at one plausible dependency has no view of the
  total, which is exactly why the gate is mechanical rather than social.
- **`govulncheck`, `gosec`, `go vet`, and `staticcheck`** run in the same gate, so the
  exposure that does exist is measured rather than assumed.

The bar is high but not absolute. Two candidates are explicitly pre-considered and remain
open on their merits:

- **A tree-sitter binding**, if hand-written extractor accuracy proves insufficient. That
  is a library decision, not a tool decision, and it is ours to bump.
- **`google.golang.org/protobuf`** under a build tag for SCIP enrichment — Google-published
  and heavily audited, and behind a tag so the default build is unaffected.

## Consequences

The zero-dependency property is now **asserted publicly** — in `README.md` and on the
landing page — and reversing it is therefore a visible change to what the project claims
about itself, not a quiet `go.mod` edit. That is deliberate: the assertion is what makes
the gate hard to erode.

The cost is real and is paid in code we own. A hand-written YAML reader
([ADR 0001](0001-hand-written-tolerant-yaml-reader.md)), four hand-written language
extractors, the graph algorithms, and Louvain are all ours to maintain and ours to have
bugs in. ADR 0001 records one such bug — a flow-scalar hang — found by our own tests
rather than by an upstream maintainer. That is the trade: defects in code we can fix in an
hour, instead of defects in code we can only wait on.

Some of that cost is offset rather than absorbed. Hand-writing the manifest reader is what
makes reading Helm templates possible at all, since those are not YAML and a conforming
parser rejects them outright. The extractors are scored against hand-labeled fixtures, so
"we wrote it ourselves" is a measured claim rather than a hope.

Two consequences are worth stating plainly as *limits* of this decision:

- **Capability is genuinely bounded.** A language whose grammar is too complex to
  hand-write accurately does not get a first-class extractor under this rule. The honest
  answer there is the diagnostic path — report the gap — not a silently poor extractor.
  If accuracy demands tree-sitter, the pre-considered opening above is how that gets
  revisited, in a new ADR.
- **This does not apply to the viewer.** `signpost-view` is a separate repository with its
  own tree and its own blast radius; every JS dependency lives there, and it never gates a
  merge. See [ADR 0006](0006-generator-and-viewer-are-separate-repositories.md).

Design reference: [docs/design.md](../design.md) §2.
