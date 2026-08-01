# Corpus — a repository shaped like the ones signpost runs on

A synthetic multi-language repository, exercised end to end by
`cmd/signpost/corpus_test.go` and by the `corpus` job in `.github/workflows/ci.yml`.

## Why this exists

Signpost runs on itself in CI (`signpost.yml`), and that catches a great deal — but it
can only exercise the code paths *this tree* contains. This tree is a Go repository with
kebab-case filenames, so self-hosting structurally cannot reach:

- a TypeScript, Python, or Rust extractor,
- an npm, Cargo, or pyproject manifest reader,
- a Compose or Kubernetes file,
- **a path containing a character that is an indicator in YAML.**

The last one was [issue #9](https://github.com/3rg0n/signpost/issues/9): a Next.js
dynamic route (`app/tools/[slug]/page.tsx`) was written unquoted into a YAML flow
mapping, which made the frontmatter unparseable from that line on and silently dropped
every `edges[]` entry after it. `verify` reported it as a warning and exited 0. Nothing
in this repository has a bracket in a filename, so no amount of dogfooding would have
found it. That is the gap this directory closes.

## What is deliberately hostile here

Every one of these is a real pattern from a real ecosystem, not a contrived string:

| Path | Why |
|---|---|
| `ts/app/tools/[slug]/page.tsx` | Next.js dynamic route. `[` opens a YAML flow sequence. Issue #9. |
| `ts/app/(marketing)/page.tsx` | Next.js route group. Parentheses break a markdown link target. |
| `ts/app/blog/[...rest]/page.tsx` | Next.js catch-all route. Brackets plus dots. |
| `py/greeter/data,notes.py` | A comma is legal in a POSIX filename and terminates a YAML flow entry. |
| `go/greeter/greeter_test.go` | A test file, so `tested_by` has something to resolve. |

## Negative boundaries

Every other assertion in the harness is a positive: this edge exists, that page exists. A
positive is satisfied by a resolver that is *too generous* exactly as well as by a correct
one — claim every specifier as internal and every edge assertion stays green while the map is
wrong in the direction nobody can see. Testing that 1+1 is 2 never catches an adder that
answers 2 for everything.

So each language carries a deliberate near-miss: a name close enough to a real one that a
matcher slightly too loose swallows it, and which nothing declares.

| Specifier | Shadows | The looseness it catches |
|---|---|---|
| `example.com/corpus/greeterx/format` | the `go.mod` module `example.com/corpus/greeter` | a module prefix compared as a string instead of by path segment |
| `@corpus/apples/juice` | the tsconfig alias prefix `@corpus/app/` | an alias prefix compared without its trailing slash |
| `httpx_extras` | the declared `httpx` | PEP 503 name normalization applied as a prefix match |
| `serde_yaml::Value` | the declared `serde` | Cargo's dash/underscore equivalence applied too widely |
| `pathe/utils` | the Node builtin `path` | a builtin matched as a string prefix instead of by first path segment |

Each must be reported as a gap and land nowhere else. Two wrong homes are possible and both
are worse than the gap: an edge into this repository invents structure, and an external node
invents a supply-chain entry nobody declared.

`TestCorpusResolvesExactlyWhatItShould` asserts the unresolved specifier **count**, which is
what makes it fail in both directions — over-claiming lowers it, over-reporting raises it. The
count rather than a substring search, because the printed report truncates to the five most
frequent specifiers and a grep for any single one passes by matching `and 1 more`.

Alongside them sit the stdlib imports — `node:fs`, python `os`, rust `std::fmt` — which are
the runtime: in no manifest, patched by nobody, so no node and no reported gap. Two of them are
addressed by subpath, `fs/promises` and `node:test/reporters`, which is
[issue #14](https://github.com/3rg0n/signpost/issues/14): the whole specifier was looked up in a
table holding `fs`, so the subpath missed and was reported as a dependency the repository failed
to resolve. `pathe/utils` above is the boundary on the other side of that rule — the last row of
the table is what stops the fix from being a prefix comparison.

## The bundle's own lifecycle

One stage here asserts nothing about extraction. `TestCorpusStalePageIsRemovedOrReported` is
[issue #10](https://github.com/3rg0n/signpost/issues/10): a page whose concept is gone used to
stay on disk forever, and strict `verify` exited 0 with it there. It belongs in the corpus
rather than only in `internal/okf` because the bug is about a bundle *in a repository over
time* — two builds, two verifies, a real git tree — and because the page it leaves behind is
not an empty stub. It carries plausible `edges` and a `resource:` naming a commit where the
code really did exist, which is what makes an orphan more expensive than a missing page.

Its negative boundary sits in the same test rather than in the table above, because the defect
is a *pair* of failures in opposite directions:

| Planted page | Must happen | What the other direction would cost |
|---|---|---|
| a copy of a real page, untouched | deleted, and named in the run's output | never deleting is the shipped bug: the bundle describes a module that is not there |
| the same copy plus one human sentence | kept, reported, sentence intact byte for byte | deleting unconditionally takes somebody's `## Notes` on the first rename |

A fix satisfying either row alone is a fix that ships one of the two bugs, so neither
assertion means anything without the other.

## What a flag is supposed to change

`ts/node_modules/@corpus-vendor/logger/` is committed on purpose, and it is the only directory
here that is not the corpus's own code. A committed `node_modules` is a real pattern —
`.gitignore` does not exclude it here, and plenty of repositories vendor a package deliberately
— which is exactly the condition signpost's own tree cannot express, since nothing in it is
vendored.

`TestCorpusVendoredCodeIsOffTheMapUntilAskedFor` is
[issue #11](https://github.com/3rg0n/signpost/issues/11): `-include-vendored` promised to
analyse vendored code, the walk honoured it and read the files, and every consumer downstream
filtered them back out on `File.Vendored` without consulting the option. The flag read a
vendored tree off disk and discarded it, and the bundle was identical either way. Every unit
test covering the walk stayed green, because the walk was the one part that worked.

| Run | Must happen | What the other direction would cost |
|---|---|---|
| default | no page names the vendored module, and no page cites `vendored-only-tinycolor` | describing somebody else's repository as this one |
| `-include-vendored` | a page for the module **and** a page citing `vendored-only-tinycolor` | the shipped bug: a flag that reads files nothing looks at |
| `-include-vendored` | every page from the default run still present | the flag silently *replacing* the map rather than extending it |

The two positives are separate rows because they fail independently: extraction is gated by
`Result.Sources()` and the manifest readers by `manifest.Registry.Run`, so a fix to the first
alone analyses the vendored source and still discards the `package.json` beside it — leaving a
module whose own declaration signpost had in hand and threw away. `vendored-only-tinycolor` is
declared in that vendored manifest and nowhere else in this tree, so its name can only reach a
bundle through the reader that was dropping it.

## Running it

The harness copies this tree to a temporary directory, `git init`s it, commits, and runs
`build` then `verify`. It is copied rather than used in place for two reasons: signpost
reads git history, and a directory inside this repository's history would describe
signpost's commits rather than the corpus's; and `build` writes `.signpost/`, which has
no business being committed here.

```
go test ./cmd/signpost -run TestCorpus -v
```

## Adding a language

Add the directory, its manifest, and at least one file that imports something external
and something internal. Then add the expectations to `corpus_test.go` — the point of the
harness is that it asserts on *named* facts, so a new language with no assertions is a
directory that gets walked and proves nothing.

Add its negative boundary in the same change: one import that is a near-miss of something the
manifest declares, in whatever spelling that ecosystem's name normalization is loosest about,
and one standard-library import. Then raise the expected count in
`TestCorpusResolvesExactlyWhatItShould` and in the `Resolution reports exactly the gaps it
should` step in `ci.yml`, and confirm the new specifier is named in the table above. Without
it the language is covered only by positives, which cannot distinguish a resolver that reads
the manifest from one that says yes to everything.

Nothing here is compiled or executed. These are inputs to a parser, so they need to be
syntactically real and do not need to be correct programs.
