// Package greeter builds greetings. Corpus fixture: not compiled, not run.
package greeter

import (
	"fmt"

	"github.com/google/uuid"
)

// Greeting is a greeting with an identity.
type Greeting struct {
	ID   string
	Text string
}

// New builds a Greeting for name.
func New(name string) Greeting {
	return Greeting{ID: uuid.NewString(), Text: fmt.Sprintf("hello, %s", name)}
}

// String renders the greeting.
func (g Greeting) String() string { return g.Text }

func unexported() {}
