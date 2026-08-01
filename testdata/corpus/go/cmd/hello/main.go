// Command hello prints a greeting. Corpus fixture.
package main

import (
	"fmt"

	"example.com/corpus/greeter"
)

func main() { fmt.Println(greeter.New("world")) }
