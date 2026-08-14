# 31. Scope is a lifecycle test, not a list of non-goals

## Status

Accepted

## Context

Design §1 carried four bullets under "What signpost is not," written at v0.1 and never
revisited. Two of them are now false as stated, and the tool proved them false rather than
the wording being imprecise:

| Bullet | State |
|---|---|
| Not a code search index | **False.** `graph.Node.Exports` names exported declarations, `graph.KindSymbol` exists, every node carries its files, and the viewer searches names, paths, and file lists. What signpost does not index is *text*. |
| Not a readiness assessment | **Reversed by §9.1 and `internal/practice`,** which ship and report practice signals both ways — found, with the grounding file, and not found. What is declined is the 1–5 score. |
| Not a bug finder or a code reviewer | Holds, and understates the tool: cycles, orphans, unpinned manifests, and doc/code drift are already reported — as conflicts and absences with a file behind them, never as ranked defects. |
| Not a chat interface | Holds, and is now load-bearing: v0.5 is an MCP server, and "an agent queries the bundle" reads as chat unless the difference is stated. |

§9.1 already had to write *"an earlier draft of this section routed those facts to a
separate tool. That was wrong."* That correction is the evidence: the exclusion did not
prevent the wrong decision, it was simply overtaken, and the section that overtook it had
to argue the case from first principles because §1 offered no test to apply.

The failure is the format rather than the entries. A list of exclusions is a thing nobody
re-reads. It is consulted when somebody already suspects a proposal is out of scope, which
is exactly the case where the list adds nothing, and it is silent on every proposal whose
category was not imagined when it was written — the workflow and data overlay being the
one that prompted this. It also decides scope by naming other tools, which makes the
boundary move whenever *they* change, for reasons nobody here reviews.

The immediate forcing question was whether an operational workflow and data-lineage
overlay belongs in signpost. Neither the bullets nor the tool names answered it.

## Decision

**§1's exclusion list is replaced by one test, applied to the thing being proposed:**

> Is it durable, evidence-backed repository knowledge with the bundle's lifecycle —
> compiled from the tree, committed, human-correctable, loudly stale when the evidence
> moves? Then it belongs here, regardless of which tool first thought to look for it. Is
> it a ranking, an opinion, an observation that cannot be reproduced from the tree at a
> commit, or knowledge that exists only while a service is running? Then it does not.

Both halves are already written in the repository. The first is §9.1's rule — "a fact an
agent needs is a page in the bundle regardless of which tool first thought to look for
it." The second is §9.1's refusal of the maturity score. The test states them as one
question instead of two precedents somebody has to find.

**Scope is decided by the artifact's lifecycle, not by which tool has the feature.** The
four verbs — compiled, committed, correctable, loudly stale — are the properties §1's two
framing properties and §8.1 already require. A capability that has them can be maintained
here; one that does not cannot be, whoever else ships it.

**The classifications this yields, stated so the test is checkable against them.** In:
structure indexing, practice findings, cycles and drift and conflicts, the workflow and
data overlay, the MCP query surface. Out: full-text search, the maturity score, root-cause
ranking, runtime traces, chat.

**Runtime traces are out on the general ground rather than a special one.** They fail the
first clause — an observation of one execution cannot be recompiled from the tree at a
commit — which is the same clause the maturity score fails. §8.1's byte-determinism
requirement independently forbids them, but reaching for it first would have made runtime
evidence look like a CI implementation detail that a different pipeline could design
around. It is not; it is out of scope.

**Full-text search is out, and the reason is not that another tool owns it.** grep is
stateless, already installed, and exact. A text index is a copy of something the tree
answers precisely, so it can only be stale — it fails the fourth verb, not a
territorial claim. Structure is different: it cannot be recovered by grep at all, which is
why indexing it is the job.

**§6.2 is untouched.** "signpost does not write `AGENTS.md`" is a trust boundary about
which files may be overwritten, not a statement about what knowledge belongs in the
bundle. Folding it into a scope test would blur two different kinds of limit.

## Consequences

**Every future proposal has to be argued, and the test is what it is argued against.**
That is more work per proposal than reading four bullets, and it is the point: the bullets
were cheap because they were not being applied. A test that gets applied will sometimes
return an uncomfortable answer, and the workflow overlay passing it is the first instance
— it arrived from outside and the test admits it.

**The test can be wrong, and it will be re-argued rather than quietly widened.** A
capability that clearly belongs but fails the test means the test is missing a clause, and
that is an amendment to this ADR's successor, not a case-by-case exception. Exceptions are
how the list decayed.

**Two claims in the repository's own history are now contradicted in writing.** §1 said
signpost is not a code search index and not a readiness assessment; both shipped. Saying
so plainly costs a little credibility once and buys a document that can be trusted on the
third reading. The alternative — editing the bullets to be narrowly true — would keep a
format that has already failed once.

**A reader looking for "what signpost is not" will not find that heading.** The section is
now "What belongs here, and what does not," and it leads with the test rather than the
exclusions. The out-list is still there, which is what makes the change safe: nobody loses
the answer, they gain the reasoning behind it.

Design reference: [docs/design.md](../design.md) §1, §9.1, §8.1, §6.2.
