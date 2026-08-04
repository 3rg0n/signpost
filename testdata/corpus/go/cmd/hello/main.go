// Command hello prints a greeting. Corpus fixture.
package main

import (
	"fmt"

	// The package, not the module. go.mod sits at `go/` and declares
	// `example.com/corpus/greeter`, so the package in `go/greeter/` is that path plus its
	// directory — the doubled last segment is what the real toolchain requires and what
	// makes this import resolve to a node. Spelling it as the bare module path named a
	// directory holding no Go files, so this file's only internal import drew no edge at
	// all: the resolver placed it inside the module, correctly declined to invent an
	// external package for it, and until the unlinked count existed nothing said so.
	"example.com/corpus/greeter/greeter"

	// Inside the module and pointing at a directory that holds no Go file, which is a
	// third outcome distinct from both of its neighbours here: not an edge, and not a
	// dependency either. Generated code, a build-tagged package, a directory whose files
	// all exceeded the size cap — the resolver places the path exactly and finds nothing
	// to link. It must be counted as unlinked and must not appear among the unresolved,
	// because there is nothing to go and declare.
	"example.com/corpus/greeter/internal/generated"

	// The negative boundary for module-prefix matching. The module declared in go.mod is
	// `example.com/corpus/greeter`, and this path shares every character of it up to a
	// segment boundary that does not exist — `greeterx` is not `greeter`. A prefix test
	// written on strings rather than on path segments claims this as internal and resolves
	// it to the greeter directory, which is a confident wrong answer. Nothing declares it,
	// so unresolved is the only honest outcome.
	"example.com/corpus/greeterx/format"
)

func main() {
	fmt.Println(greeter.New("world"))
	_ = generated.Table
	_ = format.Pad
}
