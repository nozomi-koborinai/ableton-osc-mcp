package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/mark3labs/mcp-go/mcp"
)

type probeInput struct {
	Value int `json:"value"`
}

type probeOutput struct {
	Doubled int    `json:"doubled"`
	Label   string `json:"label"`
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want mcp.TextContent", res.Content[0])
	}
	return tc.Text
}

func TestToolHandlerReturnsJSON(t *testing.T) {
	g := genkit.Init(context.Background())
	tool := genkit.DefineTool(g, "probe_double", "double a value",
		func(_ *ai.ToolContext, in probeInput) (probeOutput, error) {
			return probeOutput{Doubled: in.Value * 2, Label: "ok"}, nil
		},
	)

	handler := newToolHandler(tool)
	req := mcp.CallToolRequest{}
	req.Params.Name = "probe_double"
	req.Params.Arguments = map[string]any{"value": 21}

	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", textOf(t, res))
	}

	var got probeOutput
	if err := json.Unmarshal([]byte(textOf(t, res)), &got); err != nil {
		t.Fatalf("result was not JSON: %v (text=%q)", err, textOf(t, res))
	}
	if got.Doubled != 42 || got.Label != "ok" {
		t.Errorf("got %+v, want {Doubled:42 Label:ok}", got)
	}
}

func TestToolHandlerReportsErrors(t *testing.T) {
	g := genkit.Init(context.Background())
	tool := genkit.DefineTool(g, "probe_fail", "always fails",
		func(_ *ai.ToolContext, _ probeInput) (probeOutput, error) {
			return probeOutput{}, errors.New("track_index must be >= 0")
		},
	)

	handler := newToolHandler(tool)
	req := mcp.CallToolRequest{}
	req.Params.Name = "probe_fail"
	req.Params.Arguments = map[string]any{"value": 1}

	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned a transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError to be true")
	}
	if got := textOf(t, res); got == "" {
		t.Error("error result has empty text")
	}
}
