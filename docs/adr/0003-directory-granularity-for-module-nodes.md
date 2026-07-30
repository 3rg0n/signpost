# 3. Directory granularity for module nodes

## Status

Accepted

## Context

The graph's central node kind is the module: the thing that owns code, imports other
things, and gets a page in the bundle. Something has to decide what one is. The choice
turns out to be structural rather than cosmetic, because the module node's identity is also
the bundle's file layout and the OKF concept path other pages link against.

**The four first-class languages do not agree on what a module is.** A Go package is a
directory — the compiler enforces one package per directory. A Rust module is a file *or* a
directory with a `mod.rs`, chosen per module by the author. A Python package is a directory
with a marker file, except that namespace packages have no marker and a bare `.py` file is
also importable. TypeScript has no module concept above the file at all; what looks like
one is a directory convention plus a bundler config.

**But all four agree that files in a directory belong together.** That is true of every
language in the census, not just these four, and it needs no per-language special case.

Three candidate granularities were considered.

1. **Per language, using that language's own unit.** The most faithful reading of each
   language, and it produces a graph whose node meaning changes depending on which
   language a directory happens to contain. A polyglot repository — which is the common
   case, and the case signpost exists for — would then have two node kinds that both call
   themselves modules and mean different things. Cross-language edges become ill-defined:
   an edge from a Rust file-module to a Python directory-package is not a relationship
   between comparable things. Rejected on that incoherence, and on the per-language
   special-casing it would require in the resolver, the emitter, and every export format.
2. **Per file.** Maximally precise and unusable. A mid-size repository has thousands of
   source files; a page per file is a bundle nobody reads, and a graph at that density has
   no visible structure — the clustering, the hubs, and the cycle detection all become
   noise. It also fails the artifact's purpose: an agent orienting in an unfamiliar
   repository needs to know which *area* owns what, not which file.
3. **Per directory.** Correct in all four languages, needs no special case, and lands at a
   granularity a person already navigates by.

## Decision

**One module node per directory holding extracted source.** The node's `Path` is the
repo-relative directory, and its ID is `/modules/<slug>` — which is also the bundle page's
path with the `.md` removed, so the ID namespace and the directory layout are the same
thing and the emitter never translates between them.

Two consequences of the choice are implemented deliberately rather than incidentally:

- **A directory with no extracted source gets no node.** Not an empty one. A `testdata`
  directory, a docs directory, a directory of generated files nothing parsed — none is a
  module, and a node for one would be a page about nothing.
- **Anything keyed by a path that is not itself a module resolves to the nearest ancestor
  that is.** A fact about `internal/auth/testdata` is a fact about `internal/auth`. This
  is what lets git co-change pairs, CODEOWNERS entries, and manifest facts attach to the
  graph without inventing nodes for the directories they happen to name.

## Consequences

**The ID is a public contract.** The bundle is committed and its pages link to each other
by concept path, so changing granularity later would rewrite every page's filename and
every link between them, in every repository that has adopted signpost. This is the
decision in the graph model that is most expensive to reverse, which is why it is recorded
here rather than left as a comment.

**Directory-level aggregation is not always a sum, and that has bitten.** When git history
was rolled up to the directory, a commit touching three files in one directory is *one*
commit for that directory — summing the per-file counts would inflate every directory by
its own file count. Line counts do sum, because they are a quantity of change rather than a
count of events. The same distinction will apply to every future signal aggregated to this
granularity, and getting it wrong produces numbers that look plausible and are wrong.

**A directory holding two languages is one node.** Usually that is right — a Go package
beside its generated TypeScript client is one area of concern. Where it is not, the node
records every language it saw rather than picking one, so the ambiguity is visible in the
bundle instead of resolved silently.

**Sub-directory precision is lost, by design.** A large directory with two unrelated
concerns in it reads as one module. The mitigation is that this is legible: a module node
listing forty files is itself a finding about the repository's structure, and the
alternative granularities are worse for the reasons above.

**Nearest-ancestor resolution can collapse two distinct facts onto one node**, and callers
have to handle it. Co-change is the live example: two directory pairs
(`internal/auth/testdata ↔ internal/db` and `internal/auth ↔ internal/db`) resolve to the
same module pair, and because one commit can appear in both, summing their weights would
claim more shared commits than exist. The fold takes the maximum, which is a true lower
bound. Any future signal that both aggregates by nearest ancestor *and* carries a count
inherits this hazard.

Design reference: [docs/design.md](../design.md) §3, §4.4.
