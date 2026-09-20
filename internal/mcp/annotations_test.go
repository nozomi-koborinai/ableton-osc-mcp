package mcp

import (
	"context"
	"testing"

	"github.com/firebase/genkit/go/genkit"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/tools"
)

// Every registered tool must be classified. A new tool without an entry fails here.
func TestEveryToolIsClassified(t *testing.T) {
	g := genkit.Init(context.Background())
	for _, tl := range tools.Register(g, tools.Deps{}) {
		name := tl.Definition().Name
		if _, ok := toolHints[name]; !ok {
			t.Errorf("tool %s has no entry in toolHints", name)
		}
	}
}

// The table must not carry entries for tools that no longer exist.
func TestNoStaleClassifications(t *testing.T) {
	g := genkit.Init(context.Background())
	registered := map[string]bool{}
	for _, tl := range tools.Register(g, tools.Deps{}) {
		registered[tl.Definition().Name] = true
	}
	for name := range toolHints {
		if !registered[name] {
			t.Errorf("toolHints has a stale entry: %s", name)
		}
	}
}

func TestAnnotationsForReadOnlyTool(t *testing.T) {
	got := annotationsFor("ableton_get_tempo")
	if got.ReadOnlyHint == nil || !*got.ReadOnlyHint {
		t.Error("ableton_get_tempo should be readOnly")
	}
	if got.DestructiveHint == nil || *got.DestructiveHint {
		t.Error("ableton_get_tempo should not be destructive")
	}
	if got.OpenWorldHint == nil || !*got.OpenWorldHint {
		t.Error("openWorldHint should be true for every tool")
	}
}

func TestAnnotationsForDestructiveTool(t *testing.T) {
	got := annotationsFor("ableton_delete_track")
	if got.ReadOnlyHint == nil || *got.ReadOnlyHint {
		t.Error("ableton_delete_track should not be readOnly")
	}
	if got.DestructiveHint == nil || !*got.DestructiveHint {
		t.Error("ableton_delete_track should be destructive")
	}
}

// An unclassified name must fall back to the most cautious hints.
func TestAnnotationsForUnknownTool(t *testing.T) {
	got := annotationsFor("ableton_not_a_real_tool")
	if got.ReadOnlyHint == nil || *got.ReadOnlyHint {
		t.Error("unknown tool must not be marked readOnly")
	}
	if got.DestructiveHint == nil || !*got.DestructiveHint {
		t.Error("unknown tool must be marked destructive")
	}
}

// The analyze tools can write the reference profile file (save_reference_as),
// so they may not claim to be read-only even though most calls only read.
func TestAnalyzeToolsAreNotReadOnlyBecauseTheyCanSaveReferences(t *testing.T) {
	for _, name := range []string{"ableton_analyze_local_audio", "ableton_analyze_audio_url"} {
		got := annotationsFor(name)
		if got.ReadOnlyHint == nil || *got.ReadOnlyHint {
			t.Errorf("%s must not be readOnly: save_reference_as writes to disk", name)
		}
		if got.DestructiveHint == nil || *got.DestructiveHint {
			t.Errorf("%s should not be destructive", name)
		}
		if got.IdempotentHint == nil || !*got.IdempotentHint {
			t.Errorf("%s should be idempotent: saving the same profile twice leaves one profile", name)
		}
	}
	got := annotationsFor("ableton_list_reference_profiles")
	if got.ReadOnlyHint == nil || !*got.ReadOnlyHint {
		t.Error("ableton_list_reference_profiles should be readOnly")
	}
}
