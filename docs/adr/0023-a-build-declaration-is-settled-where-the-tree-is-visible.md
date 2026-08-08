# 23. A build declaration is settled where the tree is visible, not where the file is read

## Status

Accepted

## Context

[ADR 0016](0016-a-reader-records-what-only-it-can-know.md) puts a reading in the reader: whether a
Terraform `source` was local is recorded by the reader that saw the `./`, because `modules/rds` and
`hashicorp/vpc/aws` are the same string shape and only the syntax distinguishes them. Every
manifest reader before CMake and Bazel satisfies that rule completely — a `package.json`
dependency, a `go.mod` require, a `pyproject` dependency, a compose `image` are each fully
determined by the file holding them.

Build files are the first readers where that is false, and the reason is not an implementation
limit. It is what a build system is: a build graph is assembled from many files, and the facts a
reader is asked for are relations *between* them.

Two shapes, in the two systems.

**CMake links by bare name, and the name is either this repository's or not.**
`c/tests/CMakeLists.txt` says
`target_link_libraries(buffer_test PRIVATE corpus_buffer_core cmocka)`. `corpus_buffer_core` is
declared by `add_library` in `c/src/CMakeLists.txt`; `cmocka` is declared nowhere in the
repository. Nothing in that command, or anywhere in the file, distinguishes them — CMake's own
resolution consults the whole configured project. So the reader has no correct answer available
while it holds one file, and both wrong answers are damaging in the way this project cares about:
reported as external, a library the repository *builds* becomes a reference page claiming a
third-party dependency on its own code; reported as internal, `cmocka` disappears from the supply
chain. They are in one command, so no rule gets one right by accident.

**Bazel states which it is in the label, and moves the question to where `//` points.** `@repo` is
another repository, `//pkg` is this one, `:name` is this package — so `Dep.Local` needs no
reconciliation at all, and that is a real difference from CMake worth stating rather than
smoothing over. But `//` is relative to the **workspace root**, which is the directory holding
`WORKSPACE` or `MODULE.bazel`, and that equals the repository root only in a repository that is
exactly one workspace. Reading `go/greeter/BUILD.bazel` alone says nothing about where the root
above it is.

That one was a live defect, found by reading an emitted bundle rather than by a failing test. The
corpus's workspace is at `go/`, so `deps = ["//cmd/hello"]` resolved against the repository root
named nothing and the declared edge silently vanished. The silent drop is the visible half. The
dangerous half is a repository that *does* have a root-level directory of that name, where the
same rule draws a **wrong** edge and stamps it `confidence: extracted`.

Three positions were live, and they are the same three for both systems.

**Read one file, guess the rest.** Treat a bare CMake name as external and a `//` label as
repository-relative, on the grounds that both are right in the common case. Rejected: the common
case is a single-project repository, and a monorepo is where the structure being read actually
matters. This is what shipped first for Bazel and it is how the defect above got in.

**Defer, and read nothing until a whole-project model exists.** Rejected for
[ADR 0017](0017-a-resolution-root-may-come-from-the-source-itself.md)'s reason, sharpened: for C
there is no second source. A `#include` says which header a file reads and says nothing about what
is linked into what, and no C manifest states it either. Withholding the build read leaves a C
repository with symbol pages and no structure — the least useful shape the tool produces.

**Record the declaration verbatim, settle the relation in `assemble`.** What shipped.

## Decision

**A build reader records what its file states, including the fact that a name is unsettled, and
the relation is resolved where the whole tree is visible.** Concretely:

- `Facts.DeclaredByFile` carries the targets a CMake file declares, and `resolver.cmakeDeclaredIn`
  accumulates them across every file before any link is classified. A bare name matching a
  declaration anywhere in the tree is internal; a name matching none stays external and reaches
  `externals()`.
- A `//pkg` label is recorded **workspace-relative** by the reader. `manifest.IsBazelWorkspaceRoot`
  is exported so `assemble` applies Bazel's own rule for what marks a root rather than restating
  the filenames, and `resolver.bazelPackage` joins the label against the **nearest enclosing**
  root.

**Nearest ancestor first, and the repository root only as a fallback.** `bazelPackage` walks
outward exactly as `resolveC` does for include roots, and for the same reason: a repository holding
two workspaces has a root in each, and the one nearer the declaring file is whose labels it writes.
A vendored checkout carrying its own `MODULE.bazel` is the ordinary case, not an exotic one. The
fallback to the repository root is what a single-workspace repository looks like, and it is where a
`BUILD` file with no root above it — vendored, or a tree not committed whole — resolves.

`REPO.bazel` is deliberately not a root. It carries repository-wide settings and Bazel requires it
to sit *beside* a `WORKSPACE` or `MODULE.bazel`, so treating it as one would put a root where Bazel
sees none.

**A declared build target is a build declaration, and the two readers that state their own targets
say so.** `internal/practice` keys on the reader's authority rather than on a widened vocabulary:
`statesItsTests` and `statesItsBuild` name the `manifest.Kind`s whose declarations are
authoritative, so a CMake `add_test(NAME buffer_roundtrip ...)` is a declared test and the
`add_executable` beside it is a declared build. Widening `testCommandNames` instead would report a
Makefile target named `buffer_roundtrip` as a test, which nothing in that file states.

**Both readers stop at what they can see, and the limits are asserted rather than assumed.** A
target produced inside a `for` loop is not a top-level call and is not read. An `http_archive` with
no `sha256` acquires no version — an invented pin is what somebody auditing that file would act on,
and a plausible wrong value is worse than a missing one.

## Consequences

The `resolver` now holds two cross-file maps that exist only because a build file cannot settle its
own references, and both must be populated before the edges are drawn. `index()` registers CMake
declarations and Bazel workspace roots in its manifest pass; `addDeclaredDepEdges` consumes them.
The ordering is a real constraint rather than an incidental one: the Bazel branch has to precede
the generic `d.Local` branch, which would otherwise join the label against the repository root and
reproduce the defect this ADR is about.

**A build read is a case where a positive assertion cannot detect the defect**, which is why the
corpus fixtures are shaped the way they are. An edge assertion is satisfied by a resolver that
claims everything, and the drop this ADR fixes was invisible for exactly that reason. So each
boundary in `TestCorpusReadsBuildGraphsAndDrawsTheirInternalEdges` is paired: `c/tests` → `c/src`
against `corpus_buffer_core` appearing in **no page**; `go/greeter` → `go/cmd/hello` against no
self-edge on `go/greeter`, since `embed = [":greeter"]` is what a rule resolving every label
against the declaring file's own directory would produce for all of them. The negatives are checked
across every page body rather than by predicted filename, because a name that must appear nowhere
can appear in a summary, an edge, or an attribute.

The nearest-root rule needs **nested** workspaces to be tested at all. A first version of
`TestABazelLabelResolvesInsideTheNearestWorkspace` used siblings, which an outward walk never
confuses, and it passed against "outermost root wins". Two identically named `tool` directories in
nested workspaces is what discriminates them.

Neither build system contributes a row to the corpus's unresolved-specifier table, and the absence
is structural. A build dependency is a *declaration*, not an import: `find_package(ZLIB)` has no
near-miss to shadow and nothing to be reported as unresolved against, so its negatives are absences
from the graph instead. A CMake or Bazel name on the unresolved line would mean a build declaration
had been routed through import resolution.

Two limits are open and stated rather than hidden. A CMake file declaring only a library declares
no program, so a repository that builds a library and no executable reports no build command — one
file's silence, satisfied by any other build file in the tree declaring a program. And
`include_directories(include)` is read and deliberately recorded nowhere, which leaves
[ADR 0017](0017-a-resolution-root-may-come-from-the-source-itself.md)'s C include-root guess in
place even where CMake states the answer; the reason is on the case arm in
`internal/manifest/cmake.go`.
