# 21. Track the published spec, and never overload one of its keys

## Status

Accepted

## Decision

**signpost tracks the published Open Knowledge Format. It does not fork the vocabulary, and it
does not anticipate it.** When the spec adds a field we want, we adopt it as published. When we
need something the spec has no field for, it goes in a key the spec does not own. What we never
do is write our own value into a key OKF defines.

**An extension key is legitimate; an extension value is not.** §4.1 says producers "MAY include
any additional keys," and §11 forbids a consumer from rejecting a bundle over "unknown
additional frontmatter keys." That is a guarantee about *keys*. Nothing in the spec extends it
to values, and for a key whose values are enumerated the guarantee runs the other way: a
consumer reading `status` against §5.4's `draft | stable | deprecated` is entitled to treat
anything else as malformed. `edges` and `attributes` are safe under §4.1 because OKF has never
heard of them. `status: stale-verification` was not safe, because OKF has.

**So `status` becomes a human's key and signpost writes `signpost_status`.** The downgrade
finding — a `verified:` block that no longer matches the resource the page describes — is
signpost's conclusion about its own bookkeeping, not a statement about where the concept sits
in a lifecycle. It was only ever on `status` because that key was the nearest available shelf.
`signpost_status` sits in the same slot in §3.1's ordering, so a reader scanning for a
lifecycle field still finds both together, and the prefix names the producer that maintains it.

**`status` joins `verified` as carried, not generated.** A reader who writes `status:
deprecated` on a page is stating something signpost cannot derive from the tree and has no
standing to replace, which is the same rule [ADR 0010](0010-a-stale-page-is-deleted-only-when-nobody-wrote-on-it.md)
applies to everything else a human puts in frontmatter. One exception, and it is an upgrade
path rather than a policy: `status: stale-verification` exactly is dropped rather than carried,
because a bundle built before this ADR has our value on a key we no longer maintain, and
carrying it would leave the page asserting an out-of-vocabulary lifecycle value that nothing
will ever clear.

**Optional spec fields are declined on their merits, and declining is conformant.** §5.5's
`stale_after` is an absolute date, which is the wrong model for code: a bundle goes stale when
the tree moves, not when a calendar passes, and [ADR 0007](0007-the-bundle-names-the-commit-it-describes.md)
already makes staleness a content comparison against a commit. A date we wrote would be
confidently wrong the day after any commit. `sources[].usage_count` and `usage_window` describe
query traffic signpost does not observe. Not adopting an optional field is not a divergence;
inventing one is.

## Context

We emit `okf_version: "0.2"` and the published spec is v0.2, so the numbers agree. They agreed
by coincidence: our 0.2 was written against the spec as it stood before Google published this
revision, and matching version numbers say nothing about matching content. A field-by-field
comparison found exactly one divergence, and it was ours — `status: stale-verification`,
written by the downgrade path, on a key §5.4 enumerates.

The reasoning that produced it is in the code comment that survived until this ADR: "a value
rather than a bare `true`, because a later status has to be able to mean something else without
changing what this one meant." That is sound design for a key you own, and the mistake was not
noticing we did not own this one. It is the specific way a producer drifts from a spec it means
to follow: not by disagreeing with it, but by extending a field in a direction the spec had
already closed.

The alternative was to document the divergence and keep the value — the cost is one page in
`docs/design.md`, and no known consumer would break today, since the trust tiers OKF actually
derives come from `generated` and `verified`, not from `status`. It was rejected because the
whole proposition of emitting OKF rather than a private format is that a bundle is readable by
a tool nobody here wrote. A documented divergence is only readable by someone who reads our
documentation, which is the audience a portable format exists to stop needing.

Upstreaming was also considered. `GoogleCloudPlatform/knowledge-catalog` takes external pull
requests under a CLA, but there is no okf-specific change process, and "add a value to the
lifecycle enum for a case one producer has" is a weak proposal: our finding is about a
producer's own bookkeeping, not about a concept's lifecycle, so it does not belong in that enum
even if we owned it. §12's versioning policy — minor versions add optional fields, major
versions change required ones — also means a spec-owned key can gain values later, and
`signpost_status` costs nothing if `status` never does.

## Consequences

**A future spec version can add fields we already have, and the collision is ours to resolve.**
If OKF v0.3 defines `attributes` or `edges` differently than we do, our extension becomes a
conflict rather than an addition. This ADR is the commitment about what happens then: the spec's
meaning wins and ours is renamed, because a producer that keeps its own reading of a key the
spec now defines is the exact failure this ADR was written to fix — arriving from the other
direction. The rename is a bundle rewrite, which is the cost of having guessed.

**Every extension key now needs the `signpost_` prefix decision made explicitly, and two
existing ones do not have it.** `edges` and `attributes` predate this rule and are unprefixed,
so they carry precisely the collision risk described above. Renaming them now would rewrite
every page in every bundle for a risk that has not materialised; they stay, and the prefix is
the rule for new keys. That inconsistency is real and is the price of not rewriting the world
for a hypothetical.

**A human's `status:` is now preserved and unvalidated.** Someone can write `status: banana`
and signpost will carry it forever, because a tolerant reader that fails a build over a human's
frontmatter value is the failure [ADR 0001](0001-hand-written-tolerant-yaml-reader.md) rejected.
`verify` could warn on an out-of-enum value; it does not, and that is a gap rather than a
decision — nothing in the bundle today sets the key.

**Upgrading clears an old mark silently.** A pre-0021 bundle loses its `status:
stale-verification` line on the next build, and the finding reappears as `signpost_status` on
the same run, so the net effect is a moved line in a diff. A reader who had grepped for
`status: stale-verification` in CI gets nothing after the upgrade, which is why this is in
`CHANGELOG.md` under Changed rather than treated as an internal rename.
