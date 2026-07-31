# 9. The semantic pass is opt-in, and egress is explicit

## Status

Accepted

## Context

Everything signpost has shipped so far reports what is provably in a repository. The
semantic pass is the one part that asks a model to guess, and adding it introduces two
things the rest of the tool does not have: an outbound network call, and repository
content leaving the machine.

That combination is the decision. The mechanics — an interface, two implementations, a
JSON Schema on every request — are design (`docs/design.md` §5) and not costly to reverse.
What is costly to reverse is the **default posture**, because a default that sends code
somewhere is a default that has already sent code somewhere by the time anyone notices.

Four forces, and they do not all point the same way.

**A default backend would be a silent egress path.** signpost runs in CI, on protected
branches, in repositories that are not ours. If the tool inferred a backend from whatever
credentials happened to be in the environment, then a repository that adopted signpost for
its deterministic bundle would start shipping source files to a third-party endpoint
because an unrelated variable was set for an unrelated tool. `AWS_BEARER_TOKEN_BEDROCK` is
exactly that kind of variable: it is set on developer machines for other purposes, and
AWS's own tooling writes it.

**Fail-open makes misconfiguration invisible.** §5 requires that an unreachable backend
produce the deterministic bundle and exit 0, because a broken model must never break a
merge. That is right, and it means a backend that is denied, misconfigured, or pointed at
the wrong endpoint looks identical to one that was never configured. Someone whose bundle
has no semantic pages has no way to tell which of those happened.

**Repository content in a prompt is untrusted input, and the model's output is committed.**
Anyone who can land a comment in a repository — a vendored dependency, a fork, an unmerged
pull request — can write text addressed to the model rather than to a human reader. The
output is then committed and read by agents that act on it. That makes prompt injection a
supply-chain path into the artifact agents trust (§4.5), not a curiosity.

**Remote endpoints may retain what they are sent.** Bedrock's Mantle Responses API stores
conversation state for 30 days by default. That is a property of the endpoint, not of
signpost, and it is not visible from signpost's configuration.

Against all of that: the semantic pass is the thing that makes pages readable rather than
merely correct, and a feature nobody can find is a feature nobody uses. A posture so
conservative that enabling a backend is undocumented guesswork would be its own failure.

## Decision

**The default backend is `none`, and only explicit configuration changes that.** A
credential in the environment is never sufficient. `SIGNPOST_BACKEND` — or an equivalent
config key, or a flag — has to name a backend before signpost makes a single model call.
The deterministic bundle is the product; the semantic pass improves it.

**Credentials are read from the environment and never from a file.** `.signpost.yml` is
committed, and a config format with a place to put an API key is a config format that will
eventually have one in it.

**`signpost model check` is the answer to fail-open's invisibility, and it does not fail
open.** One probe goes through the entire path — system prompt, wrapped untrusted source,
defanging, schema, response parse — and exits non-zero when the backend does not work.
It reports which model answered, that the schema held, and that the model treated the
probe's embedded injection attempt as an observation rather than an instruction. Three
separate facts, because a bare "ok" proves none of them.

**Untrusted content is fenced deterministically, not by asking nicely.** Repository
content goes in delimited `<untrusted_source path= sha256=>` blocks; signpost's own
delimiters and chat-template control tokens are defanged inside them with an inserted
zero-width space rather than deleted, so the model still describes the real file. The
system prompt states that content inside those blocks is data. None of this is proof, and
the grounding rule at emit remains the backstop — a claim whose citation does not resolve
is dropped regardless of how well-shaped it was.

**Both backends are first-party code over the standard library.** The remote backend is
`net/http` against any OpenAI-compatible endpoint; the local one implements the inferd v2
wire protocol against its published spec. No SDK, which keeps
[ADR 0002](0002-patchable-dependencies-not-zero-dependencies.md)'s empty `require` block
intact. Reaching Bedrock without the AWS SDK is what made that possible: a Bedrock API key
is minted from an IAM principal and sent as `Authorization: Bearer`, so SigV4 is not
needed. Verified live rather than assumed.

**inferd is first-class support, not a requirement.** A local daemon is the best case —
zero marginal cost, nothing leaves the machine, and the schema compiles to a grammar that
constrains the sampler — and `signpost enable inferd` will make installing one a single
documented step. It is never implied: no auto-start, no auto-install, no daemon detection
that flips the backend on.

## Consequences

**Most runs of signpost will not call a model, and that is the intended steady state.** A
repository that adopts signpost and never configures a backend gets a complete bundle. The
semantic pages are additive, so their absence is not a broken artifact — which is the
property that lets the bundle be committed and trusted without a daemon being warm.

**Enabling a backend takes a deliberate step, and someone will find that friction.** The
answer is documentation and `model check`, not a smarter default. A default that guesses
right nine times out of ten is a default that ships source code somewhere unintended the
tenth time.

**A skip has to be visible or fail-open becomes a lie.** An unreachable backend records
what was lost in `manifest.json` and says so on stderr. A run that quietly produced fewer
pages must not look identical to one that produced all of them; that reporting is load-
bearing, not courtesy.

**Model behaviour is now part of signpost's compatibility surface, and it varies.** Three
findings from verifying this live, each of which cost a round trip to learn:

- A `description` saying "one sentence" is a hint a model may ignore; `maxLength` is a
  constraint the sampler enforces. Every prose field the semantic pass requests has to be
  bounded, or a chatty model runs to the token cap.
- A response that hits the cap arrives as `finish_reason: "length"` and is reported as a
  failure rather than parsed. A truncated summary is usually still valid JSON, and
  committing one as complete is the confidently-wrong output §4.6 refuses to emit.
- A model with a reasoning channel emits its trace into the same `content` field as the
  answer, so the JSON object is located rather than assumed. That is documented recovery
  for a known behaviour, and the reason the default model is one without a reasoning
  channel.

**Endpoint retention is the operator's to evaluate, and signpost can only be honest about
it.** Pointing the backend at a remote endpoint sends repository content there under that
endpoint's terms. signpost documents the property; it cannot mitigate it. The local
backend exists partly so there is a configuration where the question does not arise.

**This does not settle where configuration lives.** `.signpost.yml` is still specified and
unbuilt, and the config-file ADR is still owed. Until it lands, backend selection is
environment and flags only — which is a narrower surface than the design describes, and
deliberately so: it is easier to add a config key later than to remove one that has a
credential in it.

Design reference: [docs/design.md](../design.md) §4.5, §5.
