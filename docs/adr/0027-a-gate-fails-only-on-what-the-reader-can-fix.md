# 27. A gate fails only on what the reader can fix

## Status

Accepted

## Context

[ADR 0024](0024-a-branch-verify-reads-the-history-the-bundle-read.md) made `verify -as-of-bundle`
read git history as of the commit the bundle records, so every history-derived field on a branch is
identical by construction. It worked, and the gate stayed red. Thirteen consecutive pull requests
failed it after that change — every one of them correctly, and not one of them acted on.

The count is the finding. Each failure named the same remedy the strict verify names,
`run signpost build and commit the result`, and design §8.0 forbids running it on a branch: the
bundle is written on the default branch only, so two branches cannot collide in it. So a pull
request that added a package had a red gate whose instructions its author was not permitted to
follow. The remedy that does work is to merge and let the push job rebuild — which means the
correct response to that particular red was to merge past it, thirteen times, and it was.

That is the failure mode, not the noise. A check that goes red whenever anybody touches structure
trains everybody to merge past it, and the habit does not pause for the run where the bundle is
genuinely broken. The gate that fails constantly and the gate that does not exist have the same
value, and the first one costs a CI job.

Two alternatives were weighed and both are worse:

**Invert it, so a difference is green and a match is red.** This was considered seriously because
it follows from "these failures mean nothing," and it collapses two causes into one verdict. Cause
A is the pull request adding or moving structure, which a rebuild after the merge resolves. Cause B
is the pull request contradicting the bundle — a deleted bundle, a link with no target,
non-conforming frontmatter, a page claiming the wrong commit. Inverting makes cause B the passing
case: the genuinely broken bundle becomes the green one, which is the confidently-wrong artefact
§4.6 exists to prevent, arriving through the exit code.

**`continue-on-error`, so the red is advisory.** The same disease in a yellow coat. A check whose
failure a human is expected to read and judge is a check whose failure gets skimmed, and the
skimming is what this ADR is about. It also makes every future failure — including cause B —
advisory, since the annotation is per job and not per finding.

[ADR 0010](0010-a-stale-page-is-deleted-only-when-nobody-wrote-on-it.md) already drew the correct
line for one finding and design §4.6 states it: a surplus page a build *would* remove is a failure
because the remedy is `signpost build`, and one a build *keeps* is a warning because no command
resolves it and a red gate with no supported fix is a gate people switch off. That sentence turned
out to describe the whole gate rather than one finding.

## Decision

**Severity follows the remedy, not the correctness of the observation.** A failure means the reader
must act. A pass means carry on. A difference nobody can act on is neither, so it gets its own
severity and stays out of the exit code:

| severity | meaning | the reader's move |
|---|---|---|
| failure | wrong now, and wrong after the merge too | fix it; the gate is red |
| pending | a rebuild after the merge resolves it, and nothing else can | nothing; the gate is green |
| warning | no command resolves it | a human decides |

Four constraints, and the first is the one the decision lives or dies by.

1. **Pending is an enumerated list of finding kinds, not a property of the mode.** Four kinds
   qualify, because for each of them a build on the branch is the remedy and §8.0 forbids it: a
   page a build would rewrite (`out-of-date`), a concept the bundle has no page for
   (`missing-page`), a page whose concept is gone and which a build would delete (`orphan-page`),
   and a `pages` list that disagrees with what a build writes (`page-list`, which is arithmetic
   over the other two and moves whenever they do). Everything else stays a failure: a deleted
   bundle, a link with no target, frontmatter no conforming reader can parse, a page claiming a
   commit that is not the one being described. A merge inherits every one of those rather than
   repairing it.

2. **Pending exists under `-as-of-bundle` and nowhere else.** The strict verify is the run that
   *writes* the bundle, so it has no later rebuild to defer to and each of those four kinds is a
   defect there. This is what keeps the split a distinction rather than a hole, and it is the same
   asymmetry 0024 drew for provenance, one severity further down.

3. **Pending findings are printed in full, above the verdict, and never folded into a count.**
   "Nothing to do" is only trustworthy if the reader can see what was set aside and disagree with
   it. A gate that silently swallowed a page it decided was somebody else's problem would be the
   false pass §4.6 forbids, arriving through the output instead of the pages. The verdict line says
   so too: `ok: nothing to do here — the bundle is rebuilt after this merges`, deliberately not
   `ok: the bundle matches this tree`, which would be false.

4. **`page-list` becomes its own finding kind.** It was a conformance finding, and conformance is
   the kind that must never be pending — unparseable frontmatter and a short `pages` list can land
   on the same file, `manifest.json`, and need opposite severities on a branch. Severity follows
   the kind, so the two cannot share one.

And one consequence of constraint 1 that reads like an exception and is not: **the post-commit hook
prints a reminder for exactly what CI stays silent about.** Per
[ADR 0013](0013-the-local-hook-reports-and-ci-gates.md) the hook calls `verify -as-of-bundle`
rather than reimplementing the comparison, because a hook that disagrees with the gate is worse
than no hook. Pending says "the rebuild after the merge resolves this", which is true on a branch
and false on a laptop: there is no merge here and no push job, so `signpost build` *is* the remedy
and the person reading the line is the one who runs it. Same comparison, same severity, opposite
audience — so the hook reports pending as a reminder while the gate reports it as nothing to do.
The distinction reaches it as a distinct error value from the shared function rather than by
re-deriving anything, since re-deriving is how the two implementations would drift apart.

This does not supersede 0024. Its constraint 1 said content is compared byte for byte and a new
module fails; the comparison is unchanged and the pages still differ. What changed is which
severity that difference carries on a branch, and the strict verify still fails on all of it.

## Consequences

The red means something again. A failing `verify` job on a pull request is now a bundle the author
has to fix, and the ordinary case of adding a package is green — which is the only state in which a
future cause-B failure gets read rather than skimmed.

The cost is a class of difference the branch gate no longer catches. If a pull request's structural
changes are wrong in a way only a rebuild would reveal, the branch says nothing and the strict
verify on the default branch reports it after the merge — where the bundle is written, so the
remedy exists and is automatic. That is a real gap and it is deliberate: catching it on the branch
requires letting branches build the bundle, which §8.0 refuses for reasons that have nothing to do
with this.

A reader of the output now has three severities to tell apart instead of two, and a CI log with
pending findings looks busier than a clean one while being a pass. That is the trade for not
teaching people to ignore red.

`VerifyResult` gains a `Pending` list, and every consumer that reported `Findings` has to decide
which of the two it means — the same kind of second field 0024 added to `internal/vcs`, and for the
same reason: the honest answer needs more than one number.
