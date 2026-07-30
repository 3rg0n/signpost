# 4. Confidence is a first-class field on every node and edge

## Status

Accepted

## Context

signpost produces two kinds of claim from the same pipeline into the same artifact. The
deterministic pass reads facts out of source and manifests: this file imports that
specifier, this manifest declares that dependency, these two directories changed in the
same commit forty times. The semantic pass asks a model to say what a module is *for* and
how two areas relate, which is where the artifact's prose value comes from and also where
it can be wrong.

Both end up as nodes and edges in one graph, and the graph gets committed to the
repository and read by agents that act on it.

**A guess presented as a fact is the failure mode that makes the artifact worse than
nothing.** An agent that reads "auth validates tokens for the gateway" cannot tell whether
that was parsed or generated. If it was generated and wrong, the agent proceeds on a false
premise with full confidence — and it had no way to know it should have checked. A missing
claim costs a rediscovery; a confident wrong claim costs a wrong change. This is why
recording provenance is not a nicety here.

**And the two are not distinguishable after the fact.** Once a model-authored edge is in
the graph alongside a parsed one, nothing about its shape says which it was. The
distinction has to be carried, or it is lost at the moment of writing.

Three options were considered.

1. **Emit only what is deterministic.** Guarantees the problem never arises, and forfeits
   the semantic pass — which is a large fraction of why an agent would read the bundle at
   all. Rejected: prose is the point, not an extra.
2. **Segregate by location** — deterministic facts in one file or section, model output in
   another. Simple, and it fails at the granularity that matters: a module's page needs its
   parsed imports and its generated summary together, because that is how it is read. A
   split by file makes the reader reassemble what belongs on one page, and an edge does not
   have a natural home under it.
3. **A confidence field on every node and edge**, preserved by everything that renders one.

## Decision

**Every node and edge carries `extracted`, `inferred`, or `ambiguous`, and every consumer
preserves the distinction.**

- **`extracted`** — read out of source or a manifest. Deterministic.
- **`inferred`** — derived by a model. Requires a grounding citation.
- **`ambiguous`** — model output the model itself flagged as uncertain.

The rule cuts both ways, and both directions are enforced:

- **Anything that produces a node or an edge marks it.** Nothing in the deterministic pass
  may emit `inferred`; only the semantic pass may, and it must cite.
- **Anything that renders one preserves it.** Dashed edges in Mermaid and DOT, a verbatim
  `confidence` attribute in GraphML and JSON. A rendered graph that flattened the two would
  make a guess look like a fact at exactly the moment a human is skimming it.

Two implementation details follow from making it a field rather than a convention.

**Merging two statements of the same relationship keeps the stronger confidence.** When a
model infers an edge that the parser also found, the result is `extracted` — the fact
outranks the guess, rather than the later write winning. Confidence is ranked for this
purpose, so the merge is defined rather than incidental.

**CI asserts it.** The dogfood job runs signpost on this repository with no model
configured and fails if any edge is not `extracted`. That is the check that catches a
deterministic extractor quietly learning to guess: a change that flattens the distinction
is a bug even when every unit test passes, and this is the gate that says so.

## Consequences

**Every extractor, every emitter, and every export format is bound by this.** A new export
format that cannot represent the distinction is not a valid export format. A new extractor
that emits a plausible-but-derived edge as `extracted` is a bug of the most serious kind
available in this codebase, because it is invisible in the output and destroys the property
the artifact is trusted for.

**The three-way split is a floor, not a scale.** It says where a claim came from, not how
likely it is to be right — an `extracted` edge from a misparsed file is still wrong. What
the field buys is that a reviewer knows which claims to audit and which to trust
structurally. A numeric score was not chosen: a confidence percentage on a parsed import
is meaningless, and on model output it would be a number the model made up.

**`ambiguous` depends on the model being willing to say so**, which is the weakest link in
the scheme. A model that is confidently wrong emits `inferred`, and nothing here catches
it — the grounding citation is the mitigation, because a citation can be checked against
the file it names. That is a review affordance rather than a guarantee, and it is stated
here so it is not mistaken for one.

**The bundle can be read at two trust levels, which is the payoff.** A reader who only
wants facts can filter to `extracted` and get a complete, accurate structural map. A reader
who wants the prose gets it clearly marked. Neither has to take the other on faith, and
`verify` can gate on the distinction.

Design reference: [docs/design.md](../design.md) §3, §4.4.
