# 16. A reader records what only it can know, including that it does not know

## Status

Accepted

## Context

Signpost's readers are per-file and its assembler is repository-wide. A reader sees one
`compose.yaml` or one `main.tf` and produces a `manifest.Facts`; `internal/assemble` sees every
`Facts` at once and decides what becomes a node, an edge, or a page. The split is what keeps
the readers testable and the graph coherent, and it works because the assembler generally knows
*more* than any reader — it can see that a specifier matches a directory in the tree, or that
two files describe the same service.

Adding the Terraform reader produced two facts where that is false, and both shipped as defects
before they were understood as one thing.

**A local module source.** `module "queue" { source = "./modules/queue" }` and
`module "vpc" { source = "terraform-aws-modules/vpc/aws" }` reach the assembler as
`Dep{Name: "queue", Source: "modules/queue"}` and
`Dep{Name: "vpc", Source: "terraform-aws-modules/vpc/aws"}`. Those are the same
slash-separated shape. The assembler cannot tell them apart, and every rule it might guess with
is wrong in one direction: "is there a directory by that name" resolves a registry module to an
unrelated directory in a repository that happens to have one, and "does it look like a path"
misses a registry module named `modules/rds`. Only the reader saw the `./`, which is Terraform's
own rule and the only correct test. The defect shipped as an External Dependency page for
first-party infrastructure — the npm workspace-sibling failure reached by a different road,
where the supply-chain view names the repository's own code as something pulled in from outside.

**An unattributable credential reference.** `SecretRef.Service` empty meant "shared with this
file's services", and `SecretNamesFor` handed such a reference to every service in the file.
That is load-bearing and correct for a compose top-level `secrets:` block: the file declares
credentials for the services beside it without saying which reads which, so handing them to all
of them trades a false claim for no claim at all. It is false for Terraform, where one `.tf`
file holds a dozen unrelated resources and which of them reads `var.db_password` is stated in
an expression the reader does not evaluate. Read as the compose convention, an ECS service and
an S3 state backend each claimed to read three credentials neither of them names — the exact
over-broad blast radius `docs/design.md` §4.1 says a reader must never produce.

Three positions were live for each, and they are the same three:

**Let the consumer decide.** Rejected on the evidence above: for the module source there is no
rule the consumer can apply, and for the secret reference the consumer applied a rule that is
right for one reader and wrong for another. A consumer-side special case keyed on
`Facts.Kind == KindTerraform` was the tempting version — it works, and it puts a reader's
convention in the assembler, where the next reader with the same shape has to find it.

**Make the reader emit the finished artifact.** The Terraform reader could resolve
`./modules/queue` to a module node itself, and could drop the unattributable reference. Rejected
for the first: node identity is the assembler's, and a reader that minted one would need the
repository-wide view the split exists to withhold. Rejected for the second because dropping is
not free — "does this file touch credentials at all" is a question `SecretNames` answers, and
that answer would go silently wrong.

**Record the distinction and let the consumer act on it.** The reader states what it saw; the
assembler decides what to do with it.

## Decision

**When a reader knows something about a fact that no downstream consumer can re-derive, the fact
carries it.** Concretely, two fields, and the rule they are instances of:

- `Dep.Local` — this declaration's target is a directory of this repository. Set only by the
  reader that saw the syntax making it so. The assembler draws a composition edge and suppresses
  the reference page; nothing guesses from the string's shape.
- `SecretRef.Unattributed` — this reference belongs to the file as a whole and must not be handed
  to any unit in it. Distinct from an empty `Service`, which keeps its existing meaning of
  "shared with this file's services".

**"I cannot attribute this" is a fact, and it is not the same as "this belongs to everyone".**
That is the half worth recording as a decision rather than as a field, because a two-state model
looks complete until a third reader arrives. An unattributed reference is kept, still answers
whether the file touches credentials, and reaches no page: a fact with nowhere to go, rather
than a fact in the wrong place.

**A distinction that is part of a fact's meaning is part of its identity.** `Normalize` sorts on
attribution before name and `dedupeSecretRefs` includes it in the identity check, so one name
declared both ways stays two claims. Folding them would silently pick one, and which one it
picked would depend on sort order.

**The consequence is that new state goes on `Facts`, not in the consumer.** A reader-specific
branch in `internal/assemble` is the thing this decision rules out.

## Consequences

`manifest.Facts` grows a field for each such distinction, and the growth is not free: every
field is a thing the emitter, the graph, and `verify` may need to know about, and a field only
one reader sets reads as speculative to anyone who meets it in the struct rather than at its
write site. Both new fields carry their justification at the declaration for that reason.

A field that no consumer reads is a dead write, and this decision makes them easy to introduce.
That failure already happened twice in this codebase — `manifest.Facts.Entrypoints`, and then
the secret-store references themselves, which named a resource that had no node, so they reached
no page at all. The mitigation is not a rule but a test shape: assert the fact on the *bundle*,
not on the `Facts`, because a bundle assertion fails when the write goes nowhere.

`Dep.Local` is set by one reader today. A Helm chart's `file://` dependency, a Cargo `path`
dependency, and an npm `file:` specifier are the same fact, and each is currently handled by its
own mechanism or not at all. This ADR is the statement that they should converge here.

The compose convention — empty `Service` means shared — is now one of three states rather than
one of two, and it survives only because a top-level `secrets:` block really does mean that. A
reader author choosing between empty and `Unattributed` has to answer "does this file's
declaration bind the units beside it", and getting that wrong is a false claim in the direction
`docs/design.md` §4.1 warns about. The field's doc comment states the test.
