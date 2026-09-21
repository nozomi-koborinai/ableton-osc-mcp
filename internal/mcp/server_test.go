package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/tools"
)

// toolsListJSON drives the server the way a client does and returns the raw
// tools/list response. Inspecting the wire format directly keeps this test
// independent of mcp-go's internal types.
func toolsListJSON(t *testing.T, toolList []ai.Tool) []byte {
	t.Helper()
	s := NewMCPServer("test", "0.0.1", toolList)

	initBody := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize",` +
		`"params":{"protocolVersion":"2025-06-18","capabilities":{},` +
		`"clientInfo":{"name":"probe","version":"1"}}}`)
	if resp := s.HandleMessage(context.Background(), initBody); resp == nil {
		t.Fatal("initialize returned no response")
	}

	listBody := json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	raw, err := json.Marshal(s.HandleMessage(context.Background(), listBody))
	if err != nil {
		t.Fatalf("marshal tools/list response: %v", err)
	}
	return raw
}

func TestServerExposesFullSchemas(t *testing.T) {
	g := genkit.Init(context.Background())
	raw := toolsListJSON(t, tools.Register(g, tools.Deps{}))
	payload := string(raw)

	var envelope struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal response: %v (raw=%.400s)", err, payload)
	}
	if envelope.Error != nil {
		t.Fatalf("tools/list error: %s", envelope.Error.Message)
	}

	list := envelope.Result.Tools
	if len(list) != 100 {
		t.Fatalf("tools/list returned %d tools, want 100", len(list))
	}

	// The regression this whole change exists to prevent.
	if !strings.Contains(payload, "Root note (0=C 1=C# 2=D ... 11=B)") {
		t.Error("parameter descriptions are missing from tools/list")
	}
	if !strings.Contains(payload, `"minimum"`) {
		t.Error("numeric constraints are missing from tools/list")
	}
	if !strings.Contains(payload, `"required"`) {
		t.Error("required lists are missing from tools/list")
	}
	if strings.Contains(payload, `"outputSchema"`) {
		t.Error("output schema must not be exposed")
	}

	// Annotations must differ per tool, not be one blanket value.
	var readOnly, destructive int
	for _, tl := range list {
		ann, _ := tl["annotations"].(map[string]any)
		if b, _ := ann["readOnlyHint"].(bool); b {
			readOnly++
		}
		if b, _ := ann["destructiveHint"].(bool); b {
			destructive++
		}
	}
	if readOnly != 34 {
		t.Errorf("readOnly tools = %d, want 34", readOnly)
	}
	if destructive != 8 {
		t.Errorf("destructive tools = %d, want 8", destructive)
	}
}
