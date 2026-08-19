# 38. Documented languages are checked against the registry

## Status

Accepted

## Context

`extract.DefaultRegistry` is the one place that decides which languages signpost reads. Five
documents restate that set: the README's `What it reads` paragraph and its status row, the
landing page's `reads` lead and its status row, and design §4.1. A sixth restated it as a
count, and that is the one that broke — design's decision table said `nine languages at F1
1.000` against a registry holding eighteen. Nine was right when somebody wrote it, and
nothing read the sentence again.

[ADR 0037](0037-the-landing-page-is-gated-on-its-verdicts-not-its-words.md) gated the landing
page's status table against the README's and left the prose alone, on the argument that
gating prose means asserting on wording. That argument holds for a label compared against
another document's label, where neither side is more right than the other. It does not hold
for a list of languages: the registry answers the question, so the list is a claim with a
machine-readable answer rather than copy.

Four alternatives lost.

**Gate the count.** Write `seventeen languages` and assert seventeen. The number is a second
copy of what the list already says, so the edit that adds an extractor has to find both. It
moves the drift rather than stopping it.

**Compare the documents to each other.** Extend `site/status_test.go` to diff the four lists.
This catches two documents disagreeing, which is not what happened — the four lists agree
with each other, and the count that disagreed with all of them was in a fifth file. Documents
that agree and are all wrong pass.

**Generate the lists from the registry.** Rejected for the reason ADR 0037 gives about
generating the status table: it makes a hand-written document partly machine-owned, and the
drift moves up one level to whether the committed output still matches the generator. It also
flattens phrasing that is deliberately different per document — `TypeScript/JavaScript` reads
well in a sentence and `TS/JS` reads well in a table cell.

**Give `discover.Lang` a display name in production code.** Then the names come from the code
and no test holds a table. Nothing in the product prints those names, so this is an exported
API with one caller, and a fact with one reader goes stale the same way prose does.

## Decision

`internal/extract/languages_test.go` compares each documented list against
`DefaultRegistry().Langs()` as a **set**.

Names are mapped, not compared. A `langNames` table maps every spelling a document uses onto
the languages it claims, including the two phrases that claim two at once — `TS/JS` and
`TypeScript/JavaScript`. Order is not compared, because two of these lists are sentences.
Spelling is not compared between documents, for ADR 0037's reason.

That table is checked against the registry first. An extractor with no spelling would
otherwise be missing from the expected set as well as from every document, and each
assertion would agree that nothing is wrong.

An omission is declared where it is made. Design §4.1 gives Go its own paragraph, above the
list of the rest, because Go is parsed by the standard library. That claim names Go as omitted
rather than the test inferring an exception when it fails.

Every pattern must match exactly once. A renamed section that matches nothing is a failure,
and so is a pattern that has started matching a second list elsewhere in the file.

The decision table now points at §4.1 for which languages there are and §4.2 for how they are
scored, and states no count.

## Consequences

Adding an extractor fails five assertions until five documents name the language. That is the
intent: the failure names each document and the language it is missing, so the work is a list
rather than a search. A language cannot ship unclaimed, and no document can claim one that
does not exist.

This gate says nothing about whether a claimed language is read *well*. F1 1.000 is asserted
by each extractor's own scored fixtures (§4.2), which is where that claim belongs.

Prose about languages stays ungated. The sentence about no `pom.xml` reader is a claim about
what the code does not do, and a grep cannot tell a reader from a mention of one.
