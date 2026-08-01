package greeter

import "testing"

// Exists so tested_by has an edge to resolve.
func TestNew(t *testing.T) {
	if New("world").Text == "" {
		t.Fatal("empty")
	}
}
