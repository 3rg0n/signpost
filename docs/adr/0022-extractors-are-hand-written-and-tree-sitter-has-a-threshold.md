# 22. Extractors are hand-written, and tree-sitter has a written threshold

## Status

Accepted

## Context

`docs/design.md` has said since the first draft that extractors are hand-written and that "a
tree-sitter Go binding is the fallback for that language specifically" if measured accuracy proves
inadequate. `docs/adr/README.md` has listed the choice under "decisions still owed one" for as
long, with the reason: it binds every language task rather than the one being worked on, and the
threshold at which accuracy would justify the binding had never been written down. A fallback with
no trigger is not a plan. It is a sentence that lets each language's author decide the question
again, differently, and the ninth language is late to be discovering that.

There is now enough evidence to decide it rather than restate it. Nine languages ship
hand-written — Go on `go/parser`, then TypeScript/JavaScript, Python, Rust, Java, Kotlin, C, C++,
and Objective-C line-oriented — and every one of them scores F1 1.000 for both imports and symbols
against a hand-labeled corpus, against targets of 0.95 and 0.90. Two of the nine were the hard
cases on purpose. The JVM has a resolution map built from source rather than a manifest
([ADR 0017](0017-a-resolution-root-may-come-from-the-source-itself.md)). The C family has a
preprocessor, three languages sharing one header extension, and no module system at all. Neither
needed a parser.

What the same nine languages did need was **seventeen defects found by fixtures**, and the shape of
those defects is the argument. Not one was a parsing failure in the sense a grammar would prevent.
Quoted `#include`s went missing because the scanner blanks a string's body and the path was read
from the scanned text instead of the raw line. `union Slot;` borrowed the following type's brace.
`struct Buffer *buffer_make(size_t)` was read as a definition of `Buffer` because a type keyword,
a type name and an opening brace were all on the line — reporting a type the file only mentions
and losing the function that line declares. An attribute in front of a return type moved the first
parenthesis on the line, which every rule reads as the parameter list, and
`__attribute__((unused)) static int helper(void)` produced no symbol at all. An out-of-line C++
member definition claimed public visibility and overrode the `private:` its class body states.
Objective-C selectors collapsed onto their first part. `cTestBasename` lowercased a basename and
lost the capital separating `ReaderTests.m` from `protests.c`. A tree-sitter grammar would have
prevented the token-position ones and been entirely silent on the rest — because the rest are
questions about *what to record*, not about how the syntax nests. The visibility of an out-of-line
definition, whether a forward declaration is a symbol, whether a category declares a type, whether
a `.h` votes on its directory's language: a parse tree contains the information and states no
answer.

Three positions were live.

**Adopt tree-sitter now, for everything.** Uniform grammars, no hand-written line scanners, and
the brace-matching class of defect goes away. Rejected on the dependency arithmetic that
[ADR 0002](0002-patchable-dependencies-not-zero-dependencies.md) sets out. `go-tree-sitter` plus
one grammar module per language is ten-plus direct dependencies, each a cgo build, and cgo is what
takes `CGO_ENABLED=0` off the table — which is the property that makes the release archives static
single binaries. The measured accuracy is already at ceiling, so what the dependency buys is not
correctness. It buys a different failure surface for defects that are not in the parser.

**Keep the sentence as it stands.** Rejected because it is the status quo that produced this ADR's
own context: nine languages written under a rule whose trigger nobody could state.

**Hand-written, with a threshold and a scope.** What this records.

## Decision

**Extractors are hand-written, and the decision is not per-language.** A new language gets a
hand-written extractor scored against hand-labeled fixtures at the targets in
`internal/extract/score.go` — `TargetImportF1` 0.95 and `TargetSymbolF1` 0.90, with zero failures
and zero mismatches. Author's preference is not a reason to reach for a grammar.

**Tree-sitter becomes the answer for one language when, and only when, that language's fixtures
cannot be brought to target by hand.** Concretely: a scored corpus exists, it holds the constructs
the language's own idiom actually uses, and F1 stays below target after the defects the fixtures
expose have been fixed. Then a `tree-sitter` binding for *that language* is justified, it is a
direct dependency and so needs its own ADR under
[ADR 0002](0002-patchable-dependencies-not-zero-dependencies.md), and it sits behind the existing
`Extractor` interface — `Langs()` and `Extract()`, unchanged — so it is a swap and not a redesign.

**A defect found by a fixture is not evidence for the threshold.** It is evidence the fixtures are
working. Seventeen of them were fixed by hand across nine languages, each one now a named regression
test. The threshold is about a language whose *syntax* resists a line-oriented read, not about a
language whose extractor has bugs in it — and the seventeen say those are different things that look
alike from a distance.

**The corpus is where the argument is settled.** A language's fixtures must carry negative
boundaries and not only positive ones: a resolver that claims every specifier satisfies every
positive assertion in the suite. Each language's near-miss specifiers are asserted as *expected*
unresolved results in `cmd/signpost/corpus_test.go`, so a change that starts claiming them fails
the build.

## Consequences

The line-oriented scanner in `internal/extract/lines.go` is load-bearing for eight of the nine
languages and every future one, so it is the highest-leverage code in the package and its
invariants are not local details. Two have already produced defects in more than one language: it
blanks a string's body while keeping the delimiters, so anything reading a quoted path must read
`codeLine.Raw` rather than `.Text`; and depth tracking must survive char literals, preprocessor
braces, and constructs that close a scope without a brace — Objective-C's `@end`, Python's
indentation. A third language reading a quoted path will hit the first of those, which is why the
`cInclude` fix carries the reason in a comment rather than only in a test.

Nine hand-written extractors are nine bodies of code to maintain as languages change, and no
grammar upstream absorbs a syntax addition on our behalf. C++26, a new Kotlin construct, and
Python's pattern matching are each our work. That is the cost, and it is accepted knowingly: the
alternative is not less work, it is the same work relocated into keeping ten cgo grammar
dependencies current — with the static binary given up as the entry fee.

There is a class of question a parse tree would not answer, and recording the threshold makes it
visible rather than solving it. Whether an out-of-line definition's visibility is knowable from
the file it is in, whether a forward declaration is a symbol, whether a `.h`'s language label
should vote on its directory: these are decisions about what the map should say, they live in the
extractor either way, and every one of them was found by a fixture and fixed by a rule with the
reason written next to it. Reaching for a grammar because such a question was answered wrongly
would move the code and keep the defect.

Where a grammar *would* have helped is narrower than it looks and worth naming honestly, because
it is the cost side of this decision. Six of the seventeen were a line matcher reading the wrong
token as the name, or one construct as another that shares its opening tokens: a forward
declaration for a definition, a struct-returning function for a struct, an attribute's argument
list for a parameter list, an export macro for a type's name. Every one was fixed by a rule about
which position in the declaration carries which meaning, and every one is now a regression test; a
grammar would have made none of them possible. That is the standing tax on a line-oriented read,
and it is charged per language: C's preprocessor makes it the worst case so far, and each of the
six was found by a fixture rather than by a bug report. The threshold above is where paying it
stops being worth it — not when such a defect appears, but when fixing them stops bringing a
language's fixtures to target.

The `Extractor` interface is now load-bearing in a way it was not before this is written down.
Because the threshold's remedy is a swap behind it, it must stay narrow enough to swap behind:
`Langs()` plus a pure `Extract()` — same input, same output, no filesystem, no network, no clock.
A convenience added to it for one hand-written extractor is a constraint on every future
replacement.
