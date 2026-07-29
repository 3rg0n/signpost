# 1. Hand-written tolerant YAML reader

## Status

Accepted

## Context

Stage 1 of the pipeline reads a repository's non-source files: compose files, GitHub
Actions workflows, Kubernetes manifests, Helm charts and their templates, OpenAPI
documents, and `pyproject.toml`/`Cargo.toml`. Most of these are YAML. A reader is needed
that turns them into the facts in `internal/manifest`.

Three properties constrain the choice, and they conflict.

**Helm templates are not YAML.** A file under `templates/` is a Go text/template that
*produces* YAML. `name: {{ include "chart.fullname" . }}` is a valid template and an
invalid YAML mapping value; `{{- if .Values.ingress.enabled }}` at column 0 between two
indented block entries is not expressible in YAML at all. A conforming parser rejects the
whole document. But the unconditional skeleton of a Helm template carries most of what
signpost wants — the kind, the container names, the pinned image references, the ports —
and a repository that deploys through Helm has *all* of its deployment surface in exactly
these files. Reporting nothing for them means reporting nothing about how the system runs.

**A third-party parser is a standing CVE obligation.** The dependency posture for this
project is that any third-party package is one whose vulnerabilities we are on the hook to
remediate, on someone else's release schedule — patch locally and carry the fork, or open
a PR upstream and hope it merges. `gopkg.in/yaml.v3` is a reasonable library, but adding it
would make signpost's supply chain non-empty for a capability we only need a subset of, in
service of files we would then still fail to read (see above).

**The output is committed.** CI writes the bundle into the repository, so an unstable
reading is not a wrong answer that a rerun fixes — it is commit churn on every build.
Determinism is a correctness property here, not a nicety (design §8.1).

The options considered were: adopt `gopkg.in/yaml.v3`; adopt it and pre-process templates
into placeholder values before parsing; or write a reader.

## Decision

Write a tolerant, hand-written YAML reader in `internal/manifest/yaml.go`, alongside
matching readers for TOML and JSON. It is a fact reader, not a YAML implementation.

It reads what it can and records what it could not. A template directive is kept as its
literal text — "the image comes from a value" is itself the fact — and the document is
returned with a diagnostic saying it is a skeleton. `Facts.Incomplete` and `Facts.Note`
carry that forward, so a partial reading is never presented as a complete one (design
§4.2). Every extractor consumes the same tree type, so the tolerance is implemented once
rather than per file kind.

The subset is deliberate. Block and flow collections, block scalars with all three
chomping modes, both quoting styles with escapes, anchors, aliases, merge keys,
multi-document streams, and YAML 1.1 boolean spellings are supported, because each one
appears in real files this reader must read. Tags, complex mapping keys, and directives
are not, because they do not.

Consequences of the tolerance are stated where they bite: quotedness is retained on
scalars because `"3.10"` and `3.10` mean different things to the tool the file is for;
duplicate keys resolve last-wins, matching every real parser, because a reader that
disagreed with the tool the file is written for would report something false.

## Consequences

The reader is now the single highest-consequence file in `internal/manifest`: every
infrastructure and contract fact passes through it, so a defect there is a defect in all of
them. That is already demonstrated. Writing the OpenAPI tests surfaced a hang, not a
misreading — `readFlowScalar` stopped at every `:` and consumed no bytes, so
`security: [bearerAuth: [things:read]]` spun forever. YAML 1.2 makes `:` an indicator in
flow context only when a space or a flow indicator follows it, which is precisely what
makes `[things:read]` one scope name, `8080:8080` one port mapping, and
`registry.example.com/api:1.4.0` one image reference. The fix and its regression tests live
in the reader's own suite rather than in the extractor that happened to trip it, and both
flow branches now carry no-progress guards: an unanticipated shape should degrade to a
diagnostic, never to a hang.

That episode sets the standing obligation. The reader is tested directly and thoroughly —
not through its consumers — and any bug found via an extractor is fixed and covered at this
layer. Reading a strictly-conforming file wrongly is a bug; reading a non-conforming file
partially is the design.

Repository YAML this reader mishandles will be found in the wild, and the mitigation is the
diagnostic path rather than a promise of correctness: an unreadable region is reported, so
the bundle understates rather than fabricates. If the subset ever proves insufficient in a
way tolerance cannot cover, adopting a conforming parser for strict files while keeping this
reader for templates remains open — the tree type is the seam, and nothing above it depends
on how the tree was produced.

Zero third-party dependencies is preserved: `go.mod` still has no `require` block.
