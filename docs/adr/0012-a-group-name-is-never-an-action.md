# 12. A group name is never an action, and a noun with one operation stays flat

## Status

Accepted

## Decision

The rule, applied to every verb signpost will ever have:

1. **A noun with more than one operation becomes a group** — `graph show`, `graph export`.
2. **A noun with one operation stays flat** — `build`, `verify`, `version`.
3. **A group's own name is never an action.** `signpost graph` prints its subcommands and
   analyses nothing.

Clause 3 is the load-bearing one and the reason this is an ADR rather than a commit.

Two things follow from clause 3 and are part of the decision, because a group that
dead-ends is worse than a group with a default verb:

- **A bare group name exits 2 and lists its subcommands.** The user named a noun and no
  operation, which is a command line signpost cannot act on, and the answer is what to
  type instead.
- **Requested help exits 0 and prints to stdout, at every level.** `-h` on the root, on a
  group, and on a leaf all behave the same way, because help is a question that got its
  answer rather than a misuse. This is stated here because it is easy to get wrong in
  exactly one place: Go's `flag` package reports `-h` as a parse error and writes to the
  flagset's own output, so a leaf will exit 2 and print to stderr unless it is made not
  to — and then `signpost graph show -h | less` shows nothing while the shell sees a
  failure.

## Context

v0.1.0 shipped a flat surface: `build`, `verify`, `graph`, `export`, `model`, `version`.
`export` was already wrong — it renders the graph, so it is an operation *on* the graph, and
`signpost export` names the format-rendering step as though it were a peer of `build`. `model`
had already grown a group by hand (`model check`), with its own dispatch, its own help printer,
and its own unknown-subcommand message.

The forces:

**A name that is both an action and a namespace has to be learned twice.** Moving `export`
under `graph` is the obvious half. The non-obvious half is what happens to `graph` itself: keep
it runnable and `signpost graph` is an analysis *and* a namespace, so a reader has to know
which one they are getting and a writer has to document both. `git remote` has this shape and
it is a known wart — `git remote` lists, `git remote add` adds, and the list behaviour is
discoverable only by trying it.

**But uniform noun-verb costs the primary command a word.** The alternative considered and
rejected was grouping everything: `signpost bundle build`, `signpost bundle verify`. It is
consistent, and it is worse. Neither is an operation on an addressable resource — there is one
bundle per repository, not a collection to list, filter, or name — and `go build`, `cargo
build`, and `docker build` have already set the convention a user arrives with. Consistency
that nobody was confused by is not worth the tool's most-typed command growing a word.

**The surface is a public contract that erodes one verb at a time.** This is the force that
makes it an ADR. The failure is not a bad decision now; it is six months of individually
reasonable additions, each of which puts a verb where the last one went, with nothing to check
them against. `gh` — whose shape this borrows, and which follows all three clauses in practice
(`gh issue list` groups; `gh browse` and `gh api` are flat; bare `gh issue` prints help) — never
states the rule, which is how `gh repo list` and `gh search repos` both came to exist. An
unstated rule is not a rule.

**A released spelling has users, or will.** v0.1.0 is public. Nothing depends on it yet, which
is a fact with a short shelf life.

## Consequences

**`signpost export` and `signpost graph` are gone, with no aliases.** A breaking change, taken
now precisely because nothing depends on the old spelling. An alias is a second spelling to
document, test, and keep working forever; `gh alias` grew into a whole subsystem. What replaces
it is finite and deletable: a renamed verb reports where it went, and a group that used to be a
command says so rather than reporting the path it was handed as an unknown subcommand. Both are
one line of data each and both come out once the old spellings leave people's shell history.

**Every future verb has a decision procedure rather than a precedent.** `ask why` and `ask
path` are a group because they are siblings. `hooks install` is a group because `hooks
uninstall` is coming. `view` is flat because it is one thing. None of these needed a discussion;
they needed clause 1.

**Clause 3 will be argued against, and the argument will be reasonable.** It will arrive as
"bare `signpost graph` obviously means `graph show`, so why make people type it." The answer is
that it is only obvious to whoever is proposing it — `graph export` has an equal claim, and the
convenience is one word against a name that means two things. A test asserts that no group has a
runnable name, because this is the clause that would erode first and would erode silently.

**A flat verb that gains a sibling is a breaking change.** The cost of clause 1 in the other
direction: today's `verify` becomes `verify <something>` the day a second verify-shaped
operation exists, and by clause 3 the bare name stops working. Accepted, because the alternative
is pre-emptive grouping — the speculative complexity `AGENTS.md` §2 rules out — and because the
rename machinery above makes the transition cost one map entry.

**One dispatch mechanism, with the top level modelled as an unnamed group.** So `signpost -h`,
`signpost graph -h`, and `signpost model -h` share a format by construction rather than by
maintenance, and a third group costs no code. This retires `model`'s hand-rolled dispatch, which
was the second copy already.

**The help contract needs a test and a CI check, because nothing else notices it breaking.**
Both halves were wrong when the groups were added — a leaf's `-h` exited 2 while a group's
exited 0, and the leaf printed the whole of its help to stderr — and everything looked fine
from a terminal, where the two streams are the same screen and nobody reads the exit code.
The tests assert exit 0 with an empty stderr per leaf, and CI repeats it on the real binary,
because a pipe is where a person meets this and a pipe going quiet is not a failure anywhere.

## Notes

This decision needed [ADR 0011](0011-configuration-file-format-and-location.md) first. A config
file changes what a flag *means* — `-include-vendored` reads as "in this run" until a file can
set it, at which point it also means "override the file" — and deciding a verb surface without
knowing that is deciding it twice. With 0011's three flag classes settled, this was a naming and
dispatch question only.

Command reference: [docs/design.md](../design.md) §6.
