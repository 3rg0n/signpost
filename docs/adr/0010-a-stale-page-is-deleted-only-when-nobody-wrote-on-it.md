# 10. A stale page is deleted only when nobody wrote on it

## Status

Accepted

## Context

[ADR 0005](0005-commit-the-bundle-to-the-repository.md) commits the bundle to the repository.
That is what makes the artifact useful without signpost installed, and it is also what makes
this decision one worth recording: the files under discussion are tracked, reviewed, and
someone else's.

Until now signpost never deleted a page. The rule was deliberate and it was load-bearing: the
mechanism the whole design compounds on is that text a human writes outside the managed
markers survives every rebuild (`docs/design.md` §6.1), and a tool that removes a file it
generated will eventually remove a file somebody added to. So `build` created and updated,
`verify` warned, and the surplus was left where it was.

The cost of that rule turned out to be larger than the rule. A renamed or deleted directory
leaves a page describing a module that is not there — and not an empty stub: it carries
plausible `edges`, an `attributes` block, and a `resource:` naming a commit where the code
really did exist. It reads as authoritative, which makes an orphan **more** expensive than a
missing page rather than less. An agent handed one starts work against a module that does not
exist, and nothing in the bundle contradicts it.

It also survived every gate, which is what turned a wart into a defect. `build` reported
`342 page(s): 0 created, 342 updated, 0 unchanged` against a `.signpost/` holding 344 files,
with `counts.nodes` at 339. `verify --strict` exited 0. The only signal was the bundle's own
arithmetic, found by hand.

Three positions were live:

**Never delete; make `verify` fail.** Keeps the emitter's guarantee absolute and closes the
silence. Rejected as the whole answer: the remedy a failure names would be "delete this file
yourself", so every rename produces a red gate whose only fix is manual, on a check that
already runs on every pull request. A gate like that gets switched off, and the staleness
check this tool is built around goes with it.

**Delete unconditionally, as reference tools in this space do** — one deletes every generated
markdown file at the start of each export specifically to stop orphans accumulating. Rejected
outright. It ends the property §6.1 describes: the first time a directory is renamed, a page
somebody wrote `## Notes` on is gone, recoverable only from git by someone who noticed. That
is the single failure most likely to make a team stop correcting the tool, which is the
mechanism that makes the artifact improve rather than churn.

**Delete only what nobody wrote on.** Neither of the above generalises, because the two
failures point in opposite directions and each of the first two answers ships one of them.

## Decision

**A page whose concept is gone is deleted when, and only when, nothing on it came from a
person.** The test is the page's *content*, not the graph:

- frontmatter is present, with no unrecognised key and no `verified:` block,
- at least one managed region is present,
- and nothing outside the managed regions but headings and the notes invitation.

That is exactly what a first emit writes, so deleting such a page destroys nothing. Anything
else is kept and **reported**, which hands the decision to the person whose text it is. Every
uncertainty falls toward keeping it: an unreadable file, an undeletable file, a skeleton from
an older version of the emitter, a markdown file somebody dropped into the bundle directory.
Removals are named individually in the run's output rather than folded into the counts,
because a deletion is the one thing in a build a reader may need to recover from git.

**`verify`'s severity mirrors what `build` would do.** A surplus page a build removes is a
**failure**, because the remedy is `signpost build` — the same remedy every other failure
names. A surplus page a build keeps is a **warning**, because no command resolves it. Severity
tracks the availability of a fix, not the badness of the finding.

**The sweep runs after every write, never before.** By then the render has already succeeded,
so the concept set is real. A run that pruned first and then failed to render would delete
pages on the strength of a graph it turned out it could not emit.

**Directories are not removed**, even when a sweep empties one. An empty `services/` makes no
false claim, git does not track it, and an upward `rmdir` walk has the bundle root at the end
of it.

## Consequences

Signpost deletes tracked files in a repository it does not own. That is a real escalation of
what the tool does, and the mitigations are the content test above, the run naming every file
it removed, and git. A user who disagrees with a specific deletion recovers the page with
`git checkout`; a user who wants it kept permanently adds anything of their own to it, which
is the same gesture that preserves it from every other rewrite.

One concession is worth stating because it is a real loss: a heading a human *rewrote* reads
here as skeleton, so renaming a heading and nothing else does not save the page. The
alternative is comparing against the heading a first emit would have written, which cannot be
done — the node is gone, so its title is gone with it. Losing a heading somebody retyped is
smaller than keeping every orphan forever, which is what the strict reading amounts to.

`verify` can now fail for a reason no previous version could, so a repository upgrading
signpost may see a red gate on the first run over a bundle that has accumulated orphans. The
message names the remedy and the remedy works, which is the property that makes this
acceptable where the never-delete-and-fail option was not.

A page carrying human notes and no live concept stays in the bundle indefinitely, warned about
on every verify. That is intentional — it is a person's decision, and a warning that never
escalates is the correct shape for a finding nobody but they can close — but it does mean a
bundle can carry a permanent warning that is not a defect.
