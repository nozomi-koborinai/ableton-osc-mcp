package tools

import (
	"context"
	"testing"

	"github.com/firebase/genkit/go/genkit"
)

func TestRegisterReturnsUniqueTools(t *testing.T) {
	g := genkit.Init(context.Background())
	list := Register(g, Deps{})

	if len(list) == 0 {
		t.Fatal("Register returned no tools")
	}

	seen := map[string]bool{}
	for _, tl := range list {
		name := tl.Definition().Name
		if seen[name] {
			t.Errorf("duplicate tool name: %s", name)
		}
		seen[name] = true
	}

	if len(seen) != 102 {
		t.Errorf("tool count = %d, want 102", len(seen))
	}
}
