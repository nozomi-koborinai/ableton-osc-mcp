package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/mark3labs/mcp-go/mcp"
)

// toMCPTool converts a Genkit tool definition into an MCP tool, passing the
// input schema through untouched so parameter descriptions and constraints survive.
func toMCPTool(def *ai.ToolDefinition) (mcp.Tool, error) {
	schema := def.InputSchema
	if len(schema) == 0 {
		schema = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return mcp.Tool{}, fmt.Errorf("marshal input schema for %s: %w", def.Name, err)
	}
	tool := mcp.NewToolWithRawSchema(def.Name, def.Description, raw)
	tool.Annotations = annotationsFor(def.Name)
	return tool, nil
}
