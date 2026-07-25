package mcp

import (
	"log"

	"github.com/firebase/genkit/go/ai"
	"github.com/mark3labs/mcp-go/server"
)

// NewMCPServer builds an MCP server that exposes the given Genkit tools with
// their schemas intact. Duplicate names are skipped with a warning.
func NewMCPServer(name string, version string, toolList []ai.Tool) *server.MCPServer {
	if version == "" {
		version = "1.0.0"
	}
	s := server.NewMCPServer(name, version, server.WithToolCapabilities(false))

	seen := make(map[string]bool, len(toolList))
	for _, tl := range toolList {
		def := tl.Definition()
		if seen[def.Name] {
			log.Printf("Skipping duplicate tool: %s", def.Name)
			continue
		}
		converted, err := toMCPTool(def)
		if err != nil {
			log.Printf("Skipping tool %s: %v", def.Name, err)
			continue
		}
		s.AddTool(converted, newToolHandler(tl))
		seen[def.Name] = true
		log.Printf("Exposing tool: %s", def.Name)
	}
	log.Printf("MCP server ready: %d tools", len(seen))
	return s
}

// ServeStdio serves the MCP server over stdio.
func ServeStdio(s *server.MCPServer) error {
	return server.ServeStdio(s)
}
