package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// newToolHandler runs a Genkit tool and returns its output as JSON. callScope
// may be nil; otherwise it brackets the run (see WithCallScope).
func newToolHandler(tool ai.Tool, callScope func() func()) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if callScope != nil {
			defer callScope()()
		}
		out, err := tool.RunRaw(ctx, req.GetArguments())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		switch v := out.(type) {
		case nil:
			return mcp.NewToolResultText(""), nil
		case string:
			return mcp.NewToolResultText(v), nil
		}
		data, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultText(fmt.Sprintf("%v", out)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}
}
