# 35. A structural diff is text, and a second commit is a worktree

## Status

Accepted

## Context

[Issue #39](https://github.com/3rg0n/signpost/issues/39) asks for a structural diff between
two commits and says the question to settle first is whether it belongs in the viewer or is
a CLI command printing text. It asks that deliberately: the answer decides two of the three
pieces of work it lists, and building either without deciding means building both.

**Three things stand in the way, and only one of them is viewer work.**

1. Producing a graph at a commit that is not checked out. `discover.Walk` walks a
   filesystem through an `os.Root` handle, so there is no way to ask for a graph of
   `HEAD~20` today.
2. Deciding what a node's identity is across commits. A node is a directory
   ([ADR 0003](0003-directory-granularity-for-module-nodes.md)), so a renamed directory is
   a removed node plus an added one unless something says otherwise — the difference
   between "this module moved" and "half the repository changed."
3. Representing added, removed, and unchanged nodes in a layout solved for one. A removed
   node has no position, because it is not in the current graph, and laying out the union
   moves the *unchanged* nodes — so a diff view is not the graph view with colours on it.

**The lifecycle test settles the first question, not a preference about interfaces.**
[ADR 0031](0031-scope-is-a-lifecycle-test-not-a-list-of-non-goals.md) asks whether a
capability is durable, evidence-backed repository knowledge compiled from the tree. A
structural diff between two commits is: it is reproducible from the two revisions by
anybody, it needs no running service, and it is the same answer every time it is computed.
It passes.

What the test does *not* say is which surface renders it, and the tie is broken by who
reads it. Design §7.1 says the structural findings are text "because that is what an agent
consumes," and issue #41 had just finished demonstrating the cost of forgetting the second
reader: every finding `graph show` printed was bounded with no flag to lift it, so a model
that greps `and 35 more` had nowhere to go. A coloured node-link diagram is the same
mistake in a worse form — there is no `-all` for a picture. "Modules added, removed, edges
gained, edges lost" is prose; the diagram is a rendering of prose that only a person can
read, and only in a browser.

A text command is also testable. Nothing in the viewer is tested without a browser, and a
diff is exactly the kind of computation whose off-by-one nobody notices by looking.

**Item 1 was measured rather than reasoned about.** The issue lists three ways to get a
graph at another commit and calls teaching discovery to read from a git object tree "the
largest and the only one that does not touch the working copy at all." That framing is
right about the working copy and wrong about the cost, because a detached `git worktree`
does not touch it either. On this repository, at 466 files:

```
git worktree add --detach <tmp> HEAD~5     11.3s
signpost graph show <tmp>                   7.9s   87 nodes, 218 edges, 38 clusters
```

The second line is the point: the existing pipeline analysed a commit that was not checked
out, unmodified, because `analyse` takes a path and nothing below it knows or cares whether
that path is the user's checkout. Reading from a git object tree would mean a second
implementation of discovery — the `.gitignore` layering, the `os.Root` containment, the byte
budget, the census — kept in agreement with the first, forever, to save nineteen seconds on
a command nobody runs in a loop.

**Item 2 was measured too.** Git detects the rename at file granularity, which is the
granularity that aggregates to a directory:

```
$ git mv internal/auth internal/identity && git commit
$ git diff --name-status -M HEAD~1 HEAD
R082    internal/auth/auth.go   internal/identity/auth.go
```

A module node is its directory, so a directory rename is the set of file renames under it.
The graph does not carry this and does not need to: `git diff -M` between the two revisions
is a separate question asked of git, not a field added to `Node`.

**A fourth obstacle appeared only once it ran, and it is arithmetic rather than a bug.** The
first working version was run against this repository across three commits that touched no
import:

```
$ signpost graph diff HEAD~3 HEAD
  edges gained (4)
    /modules/config -co_changes-> /modules/assemble
    /modules/config -co_changes-> /modules/cmd-signpost
    /modules/config -co_changes-> /modules/okf
    /modules/assemble -co_changes-> /modules/config
```

Four findings, all of them false. A co-change edge is drawn from the commits each revision's
log holds ([ADR 0020](0020-git-history-annotates-the-map-and-never-draws-it.md)) and a pair
needs two of them, so the newer revision — whose log contains every commit the older one's
did, plus the ones in between — draws edges the older revision had no history for. This is
not a threshold that needs tuning or a window that needs aligning: any two revisions in
ancestor order have different logs *by construction*, so a diff that compares
history-derived edges reports history's own accumulation as structural change. At four
findings out of four, it is also the kind of noise that costs the whole output its reader.

## Decision

**The structural diff is `signpost graph diff <ref> <ref>`, printing text.** Not a viewer
feature. Item 3 of the issue — three states in one layout — is therefore not work anybody
has to do, and the viewer question is closed rather than deferred.

**A graph at another commit comes from a detached `git worktree` in a temporary directory,
analysed by the existing pipeline.** No second discovery implementation, no checkout of the
user's tree, and the worktree is removed when the command exits. `signpost graph diff` is
the first command in this tool that requires git rather than degrading without it, and it
says so by name when git is absent, because there is no best-effort answer to "what changed
between two commits" for a tree that has no commits.

**Rename detection is asked of git, and a module rename is reported as a rename.** `git diff
-M` between the two revisions supplies it at file granularity; a directory whose files all
moved together is one moved module, not a removal and an addition. Node identity in the
graph stays the directory path, unchanged from ADR 0003.

**The output is a finding, so it states its own absence.** Two revisions with no structural
difference print that they have none, per
[ADR 0030](0030-a-finding-states-its-own-absence.md), rather than printing nothing and
leaving a reader unable to tell a clean diff from a failed run.

**Only edges a tree states are compared, which excludes co-change.** The comparison is over
what the two trees say, and a co-change edge is a claim about a window of history rather than
about a tree. The exclusion covers the header's edge counts as well as the findings: two
numbers describing different sets under one heading is a contradiction in the place a reader
looks to check a finding, and `218 -> 222 edges` above an empty findings list reads as four
edges lost.

**A directory whose files went to two destinations is not a rename.** It is reported as a
removal plus additions. Naming one of the two as the destination would assert a relationship
the repository does not state, which is the refusal
[ADR 0034](0034-a-deterministic-pass-may-not-produce-an-ambiguous-edge.md) makes for an
interpolated table name and [ADR 0032](0032-order-is-drawn-only-where-a-file-declares-it.md)
for two CI jobs with no declared order.

**A misspelled revision is exit 2, and both revisions are resolved before either is checked
out.** The exit code follows the distinction the rest of the binary keeps — 1 means signpost
looked and reports something about the repository, 2 means the command line was wrong and
re-running it unchanged fails identically. The ordering is what makes that useful: validating
the second revision after the first has been analysed spends a worktree checkout to reject an
invocation that was wrong before it started.

## Consequences

**The viewer does not gain a diff, and that is a real loss.** Issue #36 asked for three
things and this is the third; a person comparing two releases visually is not served, and
the answer "read the text output" is a worse experience for that person than the picture
would have been. It is the right trade only because the picture cannot be given to the
reader design §7.1 names, and because the layout problem is genuinely unsolved rather than
merely unbuilt.

**`graph diff` costs a worktree checkout per revision.** Roughly 11 seconds and a full copy
of the tree on disk here, twice for two arbitrary refs. That is acceptable for a command
somebody types and unacceptable for anything on a hot path, so nothing in `build` or
`verify` may reach for this — those two stay on the working tree they already read.

**A temporary worktree is state outside the repository that has to be cleaned up.** A killed
process leaves it behind, and `git worktree list` will show it until pruned. The command
removes what it created on the way out, and a leftover worktree is a directory under the
system temp dir rather than anything inside the user's tree, but the failure mode exists and
is new.

**This is the first command that hard-requires git.** Everything else treats git as an
optional signal ([ADR 0020](0020-git-history-annotates-the-map-and-never-draws-it.md)) and
reports its absence as a fact. `graph diff` cannot: a tar of a repository with no history
has no two commits to compare. That asymmetry is stated in the command's own error rather
than left for somebody to discover from an empty result.

**Co-change is unavailable as a diff, and it is the signal with no static substitute.** "These
two modules started changing together" is a finding somebody would want, and it is the one
kind of coupling no read of the tree can produce. The answer is `graph show` at each revision,
which is a worse experience than a diff line would have been. There is no version of this that
works: the edge is a claim about a window of history, and two revisions do not share a window.

**Comparing against the committed bundle stays out**, as the issue says. `verify
-as-of-bundle` answers staleness, which is a different question from what changed
structurally between two revisions.
