# 36. A kept orphan says so on the page

## Status

Accepted

## Context

[ADR 0010](0010-a-stale-page-is-deleted-only-when-nobody-wrote-on-it.md) decided that a page
whose concept is gone is deleted when, and only when, nothing on it came from a person.
Anything else is *kept and reported*. Its last consequence names the gap this record closes:

> A page carrying human notes and no live concept stays in the bundle indefinitely, warned
> about on every verify.

"Reported" there meant two places, and both are outside the artifact. `build` names the page on
stdout, and `verify` warns about it. [ADR 0005](0005-commit-the-bundle-to-the-repository.md)
commits the bundle precisely so that it is useful without signpost installed, which means the
reader this most concerns is the one who never runs either command. To that reader a kept orphan
is indistinguishable from a live page: the same plausible `edges`, the same `attributes` block,
the same `resource:` naming a commit where the code really did exist. That is the state 0010
itself called *more* expensive than a missing page, and 0010 closed it only for the pages it
could delete.

[ADR 0021](0021-track-the-published-spec-and-never-overload-its-keys.md) already settled where
such a finding goes and set the precedent for writing one down. When a human's `verified:` block
stops matching the resource, the page gains `signpost_status: stale-verification` rather than
only appearing in a run's output — because a finding that exists only in a terminal somebody has
closed is a finding nobody acts on. The mechanism exists; this is a second value on it.

Three alternatives lost.

**Record it in `log.md`.** Rejected on reading the emitter rather than the design doc.
`log.md` accumulates dated entries, which is correct for history and wrong for current state: an
entry saying a concept was removed would never clear when the directory came back under its old
name. It would add a claim that goes stale in order to fix a claim that reads as current.

**Make `signpost_status` a list, so a page can carry both findings.** Rejected. It changes the
key's shape for every consumer, and its §3.1 slot with it, to say something no reader needs —
once the thing a page describes is gone, whether a human's review of it is current is a question
about nothing.

**Make `verify` fail on an unmarked orphan.** Rejected for
[ADR 0027](0027-a-gate-fails-only-on-what-the-reader-can-fix.md)'s reason and 0010's. `verify`
already warns about the same page. A second finding would turn one fact into two, and would make
a warning into a failure through the back door on a page whose resolution is a person's alone.

## Decision

**A kept orphan gains a generated `signpost_status: concept-removed`.** The sweep writes it into
the page it decided not to delete, so the page states why it is still there.

**One scalar, and `concept-removed` outranks `stale-verification`.** A page can be both: a human
reviewed it at an older commit, and then the concept went away. The second answer makes the
first moot, so the sweep's mark replaces the merge's rather than joining it. The precedence is
implemented by ordering alone — the insert replaces whatever is there, and the sweep runs after
every page is merged — which is why it is written down here.

**Only a page signpost wrote is marked**: frontmatter, and at least one managed region. A
markdown file somebody dropped into the bundle directory is not this tool's to annotate, for the
same reason 0010 will not delete one. It is still reported as kept, which is the part a reader
acts on.

**The mark is generated, which makes it both self-clearing and unable to save the page.** A
rebuild in which the concept exists again replaces the generated half and the mark goes with it,
with nobody editing anything. And because it is not a human key, removing the notes that kept
the page leaves it prunable on the next run — marking an orphan does not make it permanent.

**Marking never fails a build, and never rewrites a page it did not change.** A page that could
not be written is a page that is still there exactly as it was, and the bundle the run rendered
is correct — the posture the sweep already takes on a page it cannot read or cannot remove.

**`verify` gains no finding for this.** See the third rejected position above.

## Consequences

`build` now edits a tracked file it declined to delete. That is an escalation of what the tool
does, in the same direction 0010 went and one step further, and the mitigations are the same
shape: only pages signpost itself wrote are touched, the run names every one of them, the change
is one generated key, and git shows the diff. A user who wants the mark gone deletes the page,
which is the decision the sweep was handing them anyway.

A repository upgrading signpost sees a one-time diff on every kept orphan it has accumulated.
Byte-stable from then on, and the same upgrade cost 0010 accepted for the pages it removes.

**`verify` does not require the mark, so a gate can pass on a bundle a build would change.**
The up-to-date check compares only the pages a run produces, and an orphan is by definition not
one of them. This is deliberate, per the third rejected position, but it is a genuine hole in
"a build would change nothing" and is recorded rather than left to be discovered by whoever next
reads that check. The write is idempotent, so the divergence closes on the next build.

`signpost_status` now has an open vocabulary, and each value is a contract with whoever reads
the bundle. The cost is the one 0021 moved off OKF's `status:` to avoid, arriving on signpost's
own key: a consumer switching on the value has to tolerate one it does not recognise. It is a
smaller cost here for a reason worth stating — the key belongs to signpost, so an unrecognised
value on it is under-specified rather than out-of-vocabulary, where a value outside `draft |
stable | deprecated` on a spec-owned key is malformed.
