# 14. Adopt the OpenTelemetry SDK and write the exporter

## Status

Accepted

## Decision

signpost takes **three direct dependencies** — `go.opentelemetry.io/otel`,
`go.opentelemetry.io/otel/sdk`, and `go.opentelemetry.io/otel/trace` — and **writes its own
`SpanExporter`** speaking OTLP over HTTP with a JSON payload, on `net/http` and
`encoding/json`.

`otel/trace` is not optional and is not an oversight. It holds the `Tracer` and `Span`
interfaces the instrumenting API is written against — `internal/telemetry`'s own `Span` type
wraps a `trace.Span`, and `Stage` returns a context carrying one — so a package that
instruments anything has to name it. It arrives in the module graph as an indirect
requirement of `otel/sdk` either way; the only question is whether `go.mod` says so.

Four clauses, in the order a later change is likely to violate them:

1. **The SDK is adopted, the exporter is not.** `TracerProvider`, the batch span processor,
   resource assembly, and `OTEL_*` environment parsing are upstream's. The two-method
   `SpanExporter` interface is ours.
2. **No build tag.** Instrumentation compiles into every build. A tag would hide the
   dependency from `govulncheck`, not from the supply chain — measured below.
3. **Off by default, behind a no-op.** `SIGNPOST_ENABLE_TELEMETRY` gates it; disabled means
   no SDK is constructed, no goroutine starts, and nothing is allocated on any path.
4. **Fail open, always.** Initialisation never returns an error, export failures never
   change an exit code, and flush-on-exit is bounded. Telemetry can never be the reason
   `signpost build` or `signpost verify` failed.

This **supersedes the *consequence*** recorded in
[ADR 0002](0002-patchable-dependencies-not-zero-dependencies.md) — that `go.mod` has no
`require` block — and leaves its **rule** intact. 0002 says every direct dependency must be
one we can bump ourselves and the count must stay small enough that bumping stays routine.
Three Google-published modules on a shared release train — bumped by one `go get -u`,
because they release together — clear that bar. The empty `require` block was always
described there as "an outcome of the rule, not the rule itself."

## Context

Nothing in signpost's own execution is observable. A `build` that takes 40 seconds on one
repository and 4 minutes on another gives no answer to which stage ate the time, and the
tool runs in CI on protected branches where "it got slower" is the only report anyone can
file. `signpost verify` reports on the artifact; nothing reports on the run.

The decision is not whether to instrument. It is **what to import**, because the obvious
answer imports far more than it appears to.

### The exporter is where the weight is, not the SDK

Measured on this host with `go list -deps` and `go build`, four shapes, Go 1.26.5, OTel
v1.44.0. "Linked" counts modules that actually contribute a package to the binary
(`go list -deps -f '{{.Module.Path}}'`), which is the number that matters for exposure —
`go list -m all` also counts test-only modules of dependencies and inflates every row.

| Shape | Direct | `go.mod` lines | Linked modules | gRPC pkgs | protobuf pkgs | Binary |
|---|---|---|---|---|---|---|
| Before (stdlib only) | 0 | 0 | 0 | 0 | 0 | 11.2 MB ¹ |
| **SDK + own `SpanExporter`** | **3** | **10** | **10** | **0** | **0** | **12.8 MB** ¹ |
| SDK + `otlptracehttp` | 3 | 21 | 21 | 65 | 36 | 19.4 MB ² |
| SDK + `otlpmetrichttp` | 4 | 21 | 21 | 65 | 36 | 20.1 MB ² |

¹ signpost's own binary, `go build` on this host before and after the wiring landed — so the
first two rows are a real delta of **+1.6 MB**, and row 2's module and package counts are
measured on the shipped tree rather than on a probe.

² standalone probes. Compare them against each other for the cost of the upstream exporter;
against row 2 they also carry the probe's own baseline.

The decisive column is gRPC. **`otlptracehttp` — the HTTP exporter — links
`google.golang.org/grpc`, `protobuf`, `grpc-gateway/v2`, two `genproto` modules,
`golang.org/x/net`, `golang.org/x/text`, and `cenkalti/backoff/v5`.** 65 gRPC packages and
36 protobuf packages for a transport that uses neither. `go mod graph` confirms it is a
direct requirement of the exporter module, not something reachable only from its tests:

```
otlpmetrichttp@v1.44.0 google.golang.org/grpc@v1.81.1
otlpmetrichttp@v1.44.0 github.com/grpc-ecosystem/grpc-gateway/v2@v2.29.0
```

So the choice is not "SDK or no SDK." It is **SDK, or SDK plus the entire gRPC stack**, and
the thing that drags in the stack is the one part of the pipeline that is trivial to write.

### The SDK is the wrong half to hand-roll

Two of the four candidate postures were hand-rolling something. They are not equivalent.

**Hand-rolling the SDK** means owning span context propagation, sampling, a batch processor
with its queue and its drop semantics, resource merging with schema-conflict handling, and
`OTEL_*` parsing to spec. That is thousands of lines of upstream code with years of bug
reports behind it, and getting a corner of it subtly wrong produces telemetry that *lies* —
plausible spans with wrong parents or lost batches. A wrong number is worse than no number,
and this project's whole thesis
([ADR 0004](0004-confidence-is-a-first-class-field.md)) is that a guess presented as a fact
is the failure to avoid.

**Hand-rolling the exporter** is two methods:

```go
func (e *exporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error
func (e *exporter) Shutdown(ctx context.Context) error
```

OTLP/HTTP with `Content-Type: application/json` is a documented, stable wire format that
every collector accepts. `encoding/json` and `net/http` cover it. The failure mode of
getting it wrong is a collector rejecting a payload — loud, local, and caught by a test
that posts to an `httptest.Server`.

That asymmetry is the decision. Adopt the part that is hard and well-tested; write the part
that is easy and expensive to import.

### The build tag is a trap, and this is the measurement

A build tag looked like the way to have the exporter without the exposure: put the OTel
imports behind `//go:build otel` and the default build stays clean. It does not work,
because **`go.mod` carries the requirement regardless of the tag, and `govulncheck`
analyses the default build.**

Constructed as a minimal reproduction — one file behind `//go:build otel` importing
`google.golang.org/grpc@v1.55.0`, one stub behind `//go:build !otel`:

```
$ grep grpc go.mod
require google.golang.org/grpc v1.55.0

$ govulncheck ./...
No vulnerabilities found.

$ govulncheck -tags otel ./...
Vulnerability #1: GO-2024-2687
    HTTP/2 CONTINUATION flood in net/http
  Module: golang.org/x/net
    Found in: golang.org/x/net@v0.8.0
    Fixed in: golang.org/x/net@v0.23.0
      #1: tagged.go:5:8: tagtrap.init calls grpc.init, which eventually calls http2.ErrCode.String
Your code is affected by 1 vulnerability from 1 module.
```

Same tree, same `go.mod`, opposite verdicts. The gate that is supposed to catch a CVE in a
tagged dependency reports clean unless somebody remembers to pass `-tags otel`, and the
release build that ships to users would be the tagged one. A tag turns a dependency we can
see into a dependency we cannot, which is precisely inverted from what ADR 0002 exists to
achieve. **Rejected on the evidence, and recorded here so it is not re-proposed.**

### What ADR 0002's rule actually asks

0002's test is remediation ownership: *"When a CVE is published against a library in
`go.mod`, the remediation is `go get -u`, run the gate, ship."* Two data points say the two
OTel modules pass it:

- `GO-2026-6061`, hit during an earlier spike on this exact dependency set, was fixed by a
  single `go get`. No upstream PR, no local patch, no waiting.
- Both modules are Google-published, on one release train, with Dependabot and Renovate
  already configured on this repository from the first commit.

0002 also pre-considered `google.golang.org/protobuf` as an acceptable candidate on the
grounds of being "Google-published and heavily audited." It attached a build tag to that
opening; the measurement above retires the tag, not the reasoning about the publisher.

### Traces, not metrics

Signals were considered separately. **Traces are in; metrics and logs are out.**

signpost is a short-lived process that runs a fixed pipeline — discover, extract, resolve,
cluster, emit — and every question anyone has is about that one run: which stage was slow,
how many files were walked, how many edges came out ambiguous. That is a span tree. Metrics
answer questions about aggregate behaviour over time, which requires a fleet of runs to
aggregate over, and a `PeriodicReader` whose interval never fires because the process exits
first. Adding the metrics SDK would be a third module for a question nobody is asking yet.

If a future need is genuinely aggregate — say, CVE-fix latency across adopting
repositories — that is a new ADR, and it inherits the exporter written here.

### `thlibo`'s telemetry, and where it diverges

`thlibo` (an adjacent repository) instrumented the same way and is where clauses 3 and 4
come from: `THLIBO_ENABLE_TELEMETRY` gating a no-op recorder, `Init` that never returns an
error, a fixed 2-second bounded force-flush on exit because its subcommands finish before
any reader interval, delta temporality, and content-free low-cardinality enum attributes
with user-authored names redacted. All of that transfers unchanged — signpost's `build` and
`verify` are equally short-lived.

Where it diverges is the module count: thlibo took **12 direct OTel modules**, including
both the gRPC *and* HTTP exporter variants for two signals. That is the part not to copy.
It is a different posture, not a mistake — thlibo has no ADR 0002 — but it is the exact
outcome the measurement above rejects for a binary that gates merges in other people's
repositories.

## Consequences

**The public zero-dependency claim ends, and ends visibly.** ADR 0002 made that claim
load-bearing precisely so it could not be reversed by a quiet `go.mod` edit. So it is
removed in the same change as this ADR, everywhere it is asserted:
`README.md` §Build from source, `CONTRIBUTING.md` §Getting set up, and the properties list
on the landing page. The replacement states the rule (patchable, few) and the count (three),
because a claim about the count is checkable and a claim about the vibe is not.

`site/`'s no-JavaScript-dependency claim is untouched — that is
[ADR 0008](0008-the-viewer-lives-in-this-repository.md) and a separate tree.

**The dependency gate now has something to gate.** CI diffs the `require` block against the
base branch and fails a PR that adds a direct dependency with no ADR touched. It had been
running against an empty block since the first commit, so it had never been exercised on a
real addition — an untested gate is not a gate. Replayed locally against this change, it
names all three added modules and then passes, because this ADR is in the same change:

```
Direct dependencies added: go.opentelemetry.io/otel go.opentelemetry.io/otel/sdk
  go.opentelemetry.io/otel/trace
An ADR under docs/adr/ is touched in this change.
```

Worth noting for whoever reads the gate next: its `awk` skips `// indirect` lines, so it saw
nothing when the modules were first added to `go.mod` and only fired once
`internal/telemetry` imported them and `go mod tidy` promoted them. That is the correct
behaviour — an indirect requirement is not a dependency this project chose — but it means the
gate reports on imports, not on `go.mod` edits.

**`go.sum` starts existing, and the practice reader was wrong about it.** signpost reports on
its own repository, and it said: *"The Go dependencies are declared but not pinned by any
lockfile in the tree, so two builds can resolve different versions."* That is false with zero
requires and no `go.sum` — there is nothing to resolve. After this change it would become
true-by-accident, which is worse: a false positive that starts passing hides itself. So it was
fixed first, and pinned by a fixture rather than by this repository, since the tree that
demonstrates the bug stops existing the moment the requires below land.

**Instrumentation is in every build, including the one users install.** Clause 2 has a cost:
the SDK's bytes are paid by everyone, including the majority who never set
`SIGNPOST_ENABLE_TELEMETRY`. Measured on the real binary once the wiring landed, that is
**11.2 MB → 12.8 MB, +1.6 MB** (`go build`; 7.9 MB → 9.0 MB with the release workflow's
`-trimpath -ldflags "-s -w"`). Accepted. The alternative is a gate that cannot see the
dependency it is gating, and 1.6 MB on a developer tool distributed as a checksummed release
archive is not the constraint that should decide a supply-chain posture.

**Egress is now possible from the deterministic core, and that is new.**
[ADR 0009](0009-the-semantic-pass-is-opt-in-and-egress-is-explicit.md) made the semantic
pass opt-in because a default that sends code somewhere has already sent code somewhere by
the time anyone notices. The same reasoning binds here, which is why clause 3 gates on an
explicit variable and why a credential or an `OTEL_EXPORTER_OTLP_ENDPOINT` in the
environment is never sufficient on its own — CI environments have `OTEL_*` set for unrelated
collectors. Unlike the semantic pass, spans carry **no repository content**: stage names,
counts, durations, and fixed enum attributes only. A path never becomes an attribute. That
is a rule for the implementation to hold and for its tests to assert, not a property the SDK
provides.

**Three modules is a floor, not a budget.** `otel/metric` arrives as an indirect requirement
of `otel/sdk`, and adding the metrics SDK later would promote it to direct alongside a fourth
module for the metrics SDK itself. That is a new ADR under 0002's rule, not a follow-on to
this one. Ten modules link into the binary in total; the other six —
`cespare/xxhash/v2`, `go-logr/logr`, `go-logr/stdr`, `google/uuid`, `auto/sdk`, and
`golang.org/x/sys` — are the SDK's own transitive set, and are listed here because 0002's
remediation test applies to what ships, not only to what `go.mod` names.

## Notes

Measurements are reproducible: build each shape in a scratch module and compare
`go list -deps -f '{{if .Module}}{{.Module.Path}}{{end}}' ./... | sort -u`. The build-tag
reproduction needs a dependency version with a published advisory —
`google.golang.org/grpc@v1.55.0` was used above — and the two `govulncheck` invocations
differ only by `-tags`.

Design reference: [docs/design.md](../design.md) §2 for the supply-chain posture, §5 for the
egress precedent.
