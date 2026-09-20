package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

func TestToolCallRunsInsideCallScope(t *testing.T) {
	var scopeOpen, ranInsideScope, scopeClosed bool

	g := genkit.Init(context.Background())
	tool := genkit.DefineTool(g, "probe_scope", "reports whether the call scope is open",
		func(_ *ai.ToolContext, _ probeInput) (probeOutput, error) {
			ranInsideScope = scopeOpen
			return probeOutput{Label: "ok"}, nil
		},
	)

	s := NewMCPServer("test", "0.0.1", []ai.Tool{tool}, WithCallScope(func() func() {
		scopeOpen = true
		return func() {
			scopeOpen = false
			scopeClosed = true
		}
	}))

	initBody := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize",` +
		`"params":{"protocolVersion":"2025-06-18","capabilities":{},` +
		`"clientInfo":{"name":"probe","version":"1"}}}`)
	if resp := s.HandleMessage(context.Background(), initBody); resp == nil {
		t.Fatal("initialize returned no response")
	}
	callBody := json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"tools/call",` +
		`"params":{"name":"probe_scope","arguments":{"value":1}}}`)
	if resp := s.HandleMessage(context.Background(), callBody); resp == nil {
		t.Fatal("tools/call returned no response")
	}

	if !ranInsideScope {
		t.Error("tool ran before the call scope was opened")
	}
	if !scopeClosed {
		t.Error("call scope was never closed after the tool returned")
	}
}
