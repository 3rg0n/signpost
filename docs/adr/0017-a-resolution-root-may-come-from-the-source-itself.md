# 17. A resolution root may come from the source itself, and an undeclared import stays a gap

## Status

Accepted

## Context

Every resolver in `internal/assemble` before Java and Kotlin resolves an import against a
*manifest*. `go.mod` states the module path a Go import is relative to. `package.json` states the
name a workspace sibling is imported by. `tsconfig.json` states the aliases. `Cargo.toml` and
`pyproject.toml` state the crate and the package root. The resolver's whole job is to read those
declarations and then match import strings against them, and
[ADR 0016](0016-a-reader-records-what-only-it-can-know.md) is about keeping that reading in the
readers.

The JVM breaks the pattern, and it breaks it in the direction that matters. `javac` does not
consult `pom.xml` to resolve `import com.example.store.Repository`. It resolves it against the
`package com.example.store;` declaration at the top of a source file, which is the language's own
statement of where a name lives; the build file states only what to *fetch* and what to put on the
classpath. So a JVM resolver has two choices about where its resolution roots come from, and only
one of them matches the compiler.

Signpost has no `pom.xml`, `build.gradle`, or `build.gradle.kts` reader. That is a gap in coverage,
not a decision — but it forces the decision here to be made on its own merits rather than deferred,
because the question is not "which source of truth is better" but "does resolution work at all
before the manifest reader exists". The answer is that it works completely for first-party code and
not at all for third-party code, and those two halves have to be treated differently.

Three positions were live.

**Wait for the manifest reader.** Resolve nothing until `pom.xml` and the Gradle files can be read,
so the JVM extractors ship producing imports that resolve to nothing. Rejected because it gets the
dependency backwards: the manifest would give us the *external* half, and the internal half — which
module imports which, the edges the graph is actually for — needs no manifest at all and would be
withheld for a reason that does not apply to it. A Java repository would get symbol pages and no
structure, which is the least useful shape the tool can produce.

**Resolve first-party from source, and infer the rest.** Treat an unresolved JVM import as an
external dependency and mint a node for it, on the theory that `org.springframework.stereotype`
obviously came from somewhere and a node named for it is better than a gap. Rejected, and this is
the position worth arguing against explicitly because it is the one that looks generous. `resolver`'s
doc comment already states the rule it violates: an external node exists only where a manifest says
the dependency exists, because the manifest is the repository's own statement about what it depends
on. A node minted from an import string is a claim the repository never made. Worse, a JVM package
name has no reliable relationship to a Maven coordinate — `org.springframework.stereotype` lives in
`spring-context`, and nothing in the import says so — so the invented node would carry a name that
cannot be looked up, cannot be version-checked, and cannot be patched. A supply-chain view whose
entries are unactionable is worse than one with a counted gap, because the gap is honest about
being a gap.

There is a sharper version of the same failure. `resolveJVM` must never fall through to
`depOrEmpty`, which is the cross-ecosystem fallback that matches a specifier against every declared
dependency regardless of language. A repository with both Java and an npm package named `store`
would draw an edge from Java code to that npm package. The rule below is what forbids it.

**Resolve first-party from source, and count the rest.** What shipped.

## Decision

**A resolution root may be a fact extracted from source, not only a manifest declaration.** For the
JVM it is the `package` declaration, carried verbatim on `Facts.Package` and recorded in
`resolver.jvmPkgs` alongside the directory that declared it. The resolution map is built from
extracted facts, and Java and Kotlin share it because the compiler does — a Kotlin file importing a
Java package is one import, not a cross-language one.

**An import that matches no first-party package and no manifest declaration is unresolved, and
nothing invents a node for it.** `resolveJVM` returns first-match-or-nothing and never reaches
`depOrEmpty`. The consequence is visible and intended: `org.springframework.stereotype` and
`javax.servlet.http` are counted in the unresolved total on every build until a Maven and Gradle
reader exists. That count is the tool reporting the boundary of what it read.

**The JDK and the Kotlin standard library are neither, and are classified rather than counted.**
`java.*`, `kotlin.*`, `sun.*`, `jdk.*`, `com.sun.*`, and `javax.*` where the JDK owns it are the
runtime: versioned with the toolchain, not declared anywhere, and not patchable independently. A
reference page for `java.util` would be a supply-chain entry for the JDK.

Three neighbouring prefixes look like they belong and do not, and each is a boundary the corpus
measures rather than asserts. `jakarta.*` is Eclipse's EE namespace and arrives entirely as Maven
artifacts. `kotlinx.*` is coroutines, serialization, and datetime — separately versioned artifacts
with their own release cadence and their own advisories. And `javax.*` is the one prefix that needs
a per-package rule rather than a prefix match, because the namespace was split between the platform
and Java EE in 1999 and the split is historical rather than structural: `javax.crypto` is in the
JDK and `javax.servlet` is an artifact somebody chose and must upgrade. The list is taken from a
current JDK's module exports, which is why `javax.annotation` and `javax.transaction` are absent —
both were exported by JDK 8 and both moved out to Maven artifacts in JDK 11.

A first segment matched as a prefix rather than as a whole segment reports all three as the
standard library, and that is the one classification that makes a dependency disappear from the
coverage report instead of appearing in it as a gap.

## Consequences

The JVM's unresolved count is structurally higher than any other language's until the build-file
readers land, and it will not fall to zero from extractor work alone. Anyone reading the build
summary on a Java repository sees third-party imports listed as gaps. That is the honest report and
it is also a standing invitation to "fix" it the wrong way, so the corpus asserts the exact
unresolved specifiers — `java org.springframework.stereotype`, `java javax.servlet.http`,
`kotlin kotlinx.coroutines` — as *expected* results, in `cmd/signpost/corpus_test.go` and in
`.github/workflows/ci.yml`. A change that starts inventing nodes for them fails the build. The
corpus also asserts the negative: no reference page exists for `java.util`, `javax.crypto`,
`kotlin.math`, or `com.sun`.

Because the roots come from source, one package name can map to two directories — the standard JVM
layout declares each package twice, once per source set. Resolution has to choose, and it prefers
the production source set, which is a tiebreaker in `addJVMPackage`'s sort rather than a rule in
`resolveJVM`. Directory order cannot decide it: `src/androidTest` and `src/integrationTest` both
sort ahead of `src/main`. "Test" is a property of the directory rather than of the first file seen
in it, so the flag means *every* file declaring this package here is a test, and a `src/main`
package holding one `*Test.java` clears it. That is not hypothetical, and getting it wrong sends
every import of that package into another source set entirely.

The same reasoning applies to `tested_by`. A JVM test declares the package it tests and imports
nothing from it, because same-package access needs no import — so its subject is precisely the one
name its import list does not contain, and `addTestEdges` reads the declaration instead of the
imports for JVM files. Every other language reads the imports.

**This ADR generalizes past the JVM, and that is the point of recording it.** C and C++ resolve an
`#include` against include paths that live in a compiler invocation, a `CMakeLists.txt`, or a Bazel
`BUILD` file, and much of what a header include names is resolvable from the tree alone. The
temptation there is identical and so is the answer: resolve what the source states, count what only
a build system could tell you, and never mint a node for a name the repository never declared.
