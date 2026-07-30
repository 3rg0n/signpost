# 6. Generator and viewer are separate repositories

## Status

Accepted

## Context

signpost produces a graph, and a graph wants to be looked at. An interactive view with
filtering, search, and deep links to source is a large part of what makes the artifact
valuable to a human — and every implementation of one is JavaScript with a dependency tree.

That collides directly with two decisions already made. The generator's dependency posture
is stdlib-first with a CI gate on new direct dependencies
([ADR 0002](0002-patchable-dependencies-not-zero-dependencies.md)), and the generator runs
in CI on protected branches, writing into the repository
([ADR 0005](0005-commit-the-bundle-to-the-repository.md)). A JS graph library cannot go
anywhere near that binary.

**The security exposure is concentrated in exactly one place.** The generator's worst case
is bounded: it reads an untrusted repository and writes markdown, so a hostile repo can put
ugly strings in a `.md` file. The viewer's worst case is not bounded that way — it
interpolates untrusted repository strings (file paths, module names, commit authors) into a
rendered page, which is the classic injection surface. That is a real vulnerability class,
and it is worth being deliberate about where it lives.

**The audiences and cadences differ too.** The generator is a binary that gates merges and
must stay boring. The viewer is a static site that should be free to iterate, add features,
and take dependencies to do so.

Three options were considered.

1. **An `html` output format in the generator.** One tool, one install, and it puts the JS
   dependency tree and the injection surface inside the binary that gates merges. It also
   caps the viewer's ambition permanently: anything good enough to be worth using needs
   dependencies the generator cannot afford.
2. **A hosted service that renders bundles.** Best possible viewer, and it reintroduces
   everything ADR 0005 rejected — something to run, credentials, and a dependency on
   infrastructure being alive for the artifact to be readable.
3. **Two repositories**: the generator emits `graph.json`; a separate viewer consumes it and
   publishes to GitHub Pages.

## Decision

**Two repositories with separate dependency trees and separate blast radii.**

- **`signpost`** — the Go generator. Markdown and JSON only; no HTML, no JS, no server.
  This is the repository that gates merges, so its dependency list stays short and every
  entry is justified.
- **`signpost-view`** — a static site generator consuming `graph.json`, publishing to
  GitHub Pages. Every JS dependency lives here. It never runs in CI on a protected branch
  and cannot break a merge.

The seam is `graph.json`, and the viewer treats it as untrusted input even though signpost
generated it — the repository it was generated *from* was untrusted, so the strings inside
it are too.

**The viewer is optional, and the generator is complete without it.** This is what makes
the split honest rather than a deferral. A team can adopt the generator, read `index.md`
and its Mermaid graphs directly in GitHub, open the GraphML export in yEd or Gephi, and
never deploy a site. GraphML is the interop win: it carries typed edges, confidence levels,
cluster assignments, and node attributes, and it opens in tooling teams already have. For
many users that is the entire visual story.

The structural findings — hubs, cycles, bridges, orphans, doc/code islands — are written as
**text** in the bundle, because that is what an agent consumes. A picture is for the human
skim; the prose is the load-bearing artifact.

## Consequences

**The dangerous part becomes tractable by being isolated.** The viewer is one repository
with one job, so its security requirements can be stated and reviewed as a unit: escape
everything, no `innerHTML`, a strict CSP, no network egress from the page. That is a
reviewable boundary rather than a feature buried in a general-purpose tool.

**A CVE in the graph library is a bump in a repository that publishes a static site**, not
in the binary that gates merges. If the viewer build breaks, merges keep working and the
bundle is still correct.

**The viewer can be properly good.** Freed from "must not add dependencies to the thing
that gates merges," it can have real filtering, search, diff-between-commits, and deep
links to source. A generator that also rendered HTML could never afford any of that.

**The cost is two repositories to release and keep in step.** `graph.json` is now a
contract between them: a schema change in the generator can break the viewer, and the two
version independently. That is accepted, and it is the reason the JSON export is treated as
a stable interface rather than a debugging convenience.

**Mermaid in the bundle is a deliberate duplication.** The generator emits Mermaid graphs
inline despite the viewer existing, because zero-setup skimming is worth having twice over.
Mermaid degrades past a few dozen nodes, so it is capped — clusters and top hubs at the
root, members on cluster pages. It is the skim, not the real visual, and it is not expected
to compete with the viewer.

Design reference: [docs/design.md](../design.md) §7.
