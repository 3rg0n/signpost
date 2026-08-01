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

Each must be reported as a gap and land nowhere else. Two wrong homes are possible and both
are worse than the gap: an edge into this repository invents structure, and an external node
invents a supply-chain entry nobody declared.

`TestCorpusResolvesExactlyWhatItShould` asserts the unresolved specifier **count**, which is
what makes it fail in both directions — over-claiming lowers it, over-reporting raises it. The
count rather than a substring search, because the printed report truncates to the five most
frequent specifiers and a grep for any single one passes by matching `and 1 more`.

Alongside them sit the stdlib imports — `node:fs`, python `os`, rust `std::fmt` — which are
the runtime: in no manifest, patched by nobody, so no node and no reported gap.

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
