package mcp

import (
	"log"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	genkitMcp "github.com/firebase/genkit/go/plugins/mcp"
)

type namedTool interface {
	Name() string
}

// NewMCPServer creates a Genkit MCP server and logs the exposed tools.
func NewMCPServer(g *genkit.Genkit, name string, version string, tools []ai.Tool) *genkitMcp.GenkitMCPServer {
	if version == "" {
		version = "1.0.0"
	}
	for _, tool := range tools {
		if t, ok := tool.(namedTool); ok {
			log.Printf("Exposing tool: %s", t.Name())
		}
	}
	return genkitMcp.NewMCPServer(g, genkitMcp.MCPServerOptions{
		Name:    name,
		Version: version,
	})
}
