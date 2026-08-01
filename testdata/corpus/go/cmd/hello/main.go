// Command hello prints a greeting. Corpus fixture.
package main

import (
	"fmt"

	"example.com/corpus/greeter"

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
	_ = format.Pad
}
