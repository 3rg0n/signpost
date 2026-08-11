# 26. History is read where a count answers the question

## Status

Accepted

## Context

The git walk already reads what the map needs: co-change, churn, first- and last-touch dates,
author concentration. [ADR 0020](0020-git-history-annotates-the-map-and-never-draws-it.md) settled
where those may go — history annotates a node the structural pass already drew and never creates
one. What it did not settle is which *other* things git knows are worth a subprocess.

git will answer a great deal. Five candidates were considered, each with a real question behind it:

1. **Commit messages** — what shape does a message take in this repository, and are changes
   traceable to an issue?
2. **Tags** — is this versioned, and which released version is this commit near?
3. **Blame** — who last touched each line, and how old is the code in each file?
4. **Branch topology** — how many branches, how long-lived, merged how?
5. **`.git/config` remotes** — where does this repository live?

Two properties decide these, and both were measured rather than assumed.

**Cost is process spawn, not git.** On the development host a bare `git rev-parse` costs
1200–1400ms — the spawn dominates, and every candidate's real price is how many of them it needs.
Measured on this repository: `git tag` 1463–1581ms, `for-each-ref` 1947–2052ms, `describe`
1890–3057ms. All three are one spawn's worth of work. Blame is not: a bulk run over 81 files cost
109718ms total, against 99393ms for the same number of bare spawns — so the *marginal* blame work
is 127ms per file and the spawn is the rest. A prior estimate of 963ms per file was ~87% spawn
overhead and is corrected here. The refusal of blame stands, but on the right grounds: it is one
process per file, ~100s of pure spawn for 81 files on this host, and untenable across a fleet.

**What leaves the package decides what has to be escaped.** A commit subject is arbitrary bytes
from an untrusted repository, bound for a markdown file that is committed and rendered. Storing one
would mean owning a length cap, marker escaping, a rule about URLs and names and pasted secrets,
and a test for each. Counting one owns none of that. This is the same boundary
[ADR 0016](0016-a-reader-records-what-only-it-can-know.md) draws from the other side: a reader
records what only it can know, and what this reader knows about a subject is which shape it has.

Measuring the first candidate also found a defect. The pretty format was
`%H<US>%aN<US>%ad<US>`, and git accepts a unit separator inside an author name — verified against
git 2.51.1, which takes any byte but NUL there. A name containing one shifted every following
field right, so `git config user.name $'ev\x1fil'` made the **date** parse as `il`, and every
module page recorded that fragment as its `first_commit` and `last_commit`. That is a silent
corruption of a field on ~53 pages, produced by a repository setting its own config.

## Decision

**Read commit-message shape and tags. Refuse blame, branch topology, and remotes. Order the log
format so the fields a repository controls come last.**

### Read: commit-message shape, as counts only

`internal/vcs/conventions.go` classifies the subjects of the walk already in hand and returns six
integers: subjects seen, how many follow Conventional Commits, how many are fixes, features, and
reverts, and how many name an issue as `#N`. **No subject reaches an exported type and none is
written anywhere.** That is the security boundary above, not an economy.

Zero extra process spawns: `%s` is one more field on a format git is already producing.

Not `--grep`, which looked cheaper and is wrong. `--grep` matches any line of the whole message
rather than the subject, so a body quoting an earlier subject matches. Measured on a 2000-commit
repository, `^Co-Authored-By` matched **528** commits — a string that can never appear in a
subject — and the conventional-commit pattern gave **362** against the subject-only truth of
**341**. Each pattern is also its own `git rev-list`, at a spawn apiece, to re-derive what the
running walk holds.

The rate is the signal, not the count. Adoption measured across seven repositories was 100%, 99%,
96%, 83%, 11%, 0%, 0% — bimodal, with nothing between 11 and 83. A repository at 11% is not a
partial adopter; it is one that does not use the convention with four commits that happen to parse.
So `internal/practice` reports adoption above two thirds as a declaration and anything below it as
an absence *with the rate stated*, because "follows no convention" alone reads as though signpost
found nothing.

### Read: tags, with the shallow-clone ambiguity made explicit

`internal/vcs/tags.go` reads tags reachable from the commit being described: how many, the newest
and its date, and how many commits the described commit is past it. Two spawns.

Reachability is `--merged=<sha>`, so tagging an unrelated branch does not move the number and a
bundle verified as of a recorded commit sees the tags that commit had. The value is attached
rather than passed after `--end-of-options`, which every other revision in the package hides
behind: `--merged` takes the following argument as its value, so `for-each-ref` reads the
sentinel itself as the revision. The attached form is safe by construction — a value glued to
its option cannot be parsed as an option — and `validCommit` has already restricted it to forty
hex characters.

Sorted by creation date first and version order only as a tiebreak. Date is primary because a
repository tagging `2026.08` or `release-3` is not doing semver, and ranking its tags by a
scheme it does not use would name the wrong one as latest. The tiebreak is not an ornament:
`creatordate` compares to the second, so every release cut in one session ties, and git resolves
an exact tie by refname *ascending* — measured, `v0.1.0` beat a `v0.2.0` created after it, so the
page named the older release as latest. `-v:refname` also puts `v1.10.0` above `v1.9.0`, which
plain refname order does not, and degrades sensibly on names that are not versions.

`Releases.Available` exists because of an ambiguity that would otherwise produce a false finding.
`git clone --depth 1` yields no tags; so does a repository nobody has tagged. Reporting the first
as "no release is tagged" would be signpost asserting something about somebody else's repository on
the basis of how it was cloned. So a shallow clone is `Available: false` with a `Reason` that names
the fix, per §4.2 — the absence of a measurement is never a clean bill of health. Every signpost
workflow uses `fetch-depth: 0`, and the pinned `actions/checkout` was read rather than trusted: at
`fetchDepth <= 0` it takes the all-history refspec, which includes the tag refspec even alongside
`--no-tags`.

A tag name is safe to carry as text and unsafe as a shell argument. git's own ref rules reject a
newline, a tab, `[`, `{`, `\`, `^`, `~`, `?`, `*`, and a double quote, so a tag name cannot break
the YAML scalar or markdown line it lands in. It accepts `'`, `;`, and `$(id)` — so the name is
never handed to a shell, and the finding renders it as inline code.

### Field order in the log format

Fields are ordered by trust: hash, date, then the two a repository controls — author name and
subject — last. Ordering does not stop the shift described above; it bounds what the shift can
reach. A separator in an author name now corrupts the author name and the subject, both already
free text, and the date ahead of them is out of reach. The subject is the field this change put
downstream, and it is counted and discarded — so the worst case is one miscounted commit instead of
a page asserting a directory was first touched on "il".

### Refused: blame

One process per file. The cost is the spawn, not the blame, and no amount of care about the latter
fixes the former. `--incremental` over the whole tree in one invocation is not a shape blame
offers. And the question blame answers best — who owns this line — is a person-level fact this
project does not put on a page: `CODEOWNERS` already states ownership as policy, which is a
declaration a repository chose to make, where blame is an inference about individuals.

### Refused: branch topology

Largely inferable from what is already read, and the part that is not is about the clone rather
than the repository. `git branch -a` on a `fetch-depth: 0` checkout lists whichever remote branches
that fetch brought, so a count would vary with CI configuration rather than with the project. What
a contributor actually needs — which branch is the trunk, and what must pass to merge into it — is
already read from the workflow files by `internal/practice`.

### Refused: `.git/config` remotes

An untrusted, network-pointing value read from a file, for display. The bundle already names the
commit it describes ([ADR 0007](0007-the-bundle-names-the-commit-it-describes.md)) and takes the
repository URL from an explicit `-repo`, which is the operator stating it rather than signpost
inferring it from a config file that may name a fork, a mirror, or a host that no longer exists.

## Consequences

An agent reading a bundle learns how to write a commit here before it writes one, which is a
question no file in most repositories answers: a `CONTRIBUTING.md` can be silent while 800 commits
all say `feat:`. The convention is declared by practice, and practice is where it is written down.

The findings land under a new practice topic rather than on the graph, which
[ADR 0020](0020-git-history-annotates-the-map-and-never-draws-it.md) requires: a tag is a ref and
not a module, and a message convention is not a node at all.

The refusals are auditable and each names what would change the answer. Blame becomes affordable
if a single invocation can cover a tree; branch topology becomes meaningful if there is a way to
tell a project's branches from a fetch's; remotes become readable if there is a reason to prefer an
inferred URL over a stated one. None of the three is refused on taste.

Two costs are accepted. Message classification is deliberately conservative — a subject the parser
does not recognise counts toward the denominator and nothing else — so adoption is understated
rather than overstated, which a reader can see in the rate. And a repository whose history is not
read reports *nothing* on this topic rather than absences, which departs from how every other
practice topic handles a missing input. The two cases are different claims: a manifest signpost
cannot parse is a gap in signpost, reported as one, where `-no-history` or a tarball with no `.git`
leaves this package with no evidence in either direction.
