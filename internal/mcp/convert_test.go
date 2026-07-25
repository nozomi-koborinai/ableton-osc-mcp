package mcp

import (
	"encoding/json"
	"testing"

	"github.com/firebase/genkit/go/ai"
)

func TestToMCPToolPreservesSchemaDetail(t *testing.T) {
	def := &ai.ToolDefinition{
		Name:        "ableton_set_song_key",
		Description: "Ableton Live: set song root note and scale",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"root_note": map[string]any{
					"type":        "integer",
					"description": "Root note (0=C 1=C# 2=D ... 11=B)",
					"minimum":     0,
					"maximum":     11,
				},
				"scale_name": map[string]any{
					"type":        "string",
					"description": "Scale name",
				},
			},
			"required": []any{"root_note", "scale_name"},
		},
	}

	tool, err := toMCPTool(def)
	if err != nil {
		t.Fatalf("toMCPTool: %v", err)
	}
	if tool.Name != def.Name {
		t.Errorf("name = %q, want %q", tool.Name, def.Name)
	}
	if tool.Description != def.Description {
		t.Errorf("description = %q, want %q", tool.Description, def.Description)
	}

	var got map[string]any
	if err := json.Unmarshal(tool.RawInputSchema, &got); err != nil {
		t.Fatalf("unmarshal raw schema: %v", err)
	}
	props, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties missing from converted schema")
	}
	root, ok := props["root_note"].(map[string]any)
	if !ok {
		t.Fatal("root_note missing from converted schema")
	}
	if root["description"] == nil {
		t.Error("parameter description was dropped")
	}
	if root["minimum"] == nil || root["maximum"] == nil {
		t.Error("numeric constraints were dropped")
	}
	if got["required"] == nil {
		t.Error("required list was dropped")
	}
}

func TestToMCPToolHandlesEmptySchema(t *testing.T) {
	tool, err := toMCPTool(&ai.ToolDefinition{Name: "ableton_play", Description: "start playback"})
	if err != nil {
		t.Fatalf("toMCPTool: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(tool.RawInputSchema, &got); err != nil {
		t.Fatalf("unmarshal raw schema: %v", err)
	}
	if got["type"] != "object" {
		t.Errorf("type = %v, want object", got["type"])
	}
	if _, ok := got["properties"].(map[string]any); !ok {
		t.Error("properties object missing for no-arg tool")
	}
}
