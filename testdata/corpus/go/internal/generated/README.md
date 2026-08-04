# generated

A directory the Go module owns and signpost cannot read.

`go/cmd/hello/main.go` imports `example.com/corpus/greeter/internal/generated`, which is
inside the module declared by `go/go.mod` — so resolution places it exactly, at this
directory. There is no Go file here, so there is no module node to draw an edge to.

That is the whole of the fixture. It is the honest outcome, not a bug: inventing an
external dependency for a path the module owns would report a package nobody publishes,
and the resolver must never do that. What was missing was any record that the edge had
gone. `assemble.Result.Unlinked` is that record, and
`TestCorpusFirstPartyImportsThatReachNoPageAreCounted` asserts this specifier is on it.

Real repositories reach this state constantly — a `protoc` output directory that is
generated at build time and gitignored, a package behind a build tag, a directory whose
files all exceeded the size cap. A handful of these is ordinary. Hundreds means a
resolution root is missing, which is the shape the tsconfig `paths` gap had: 542 absent
edges and no line anywhere admitting it.

Nothing here is compiled. The README exists so git tracks the directory; a `.gitkeep`
would be skipped by discovery and the directory would not survive the copy into a test
repository.
