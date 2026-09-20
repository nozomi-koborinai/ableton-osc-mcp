package mcp

import (
	"log"

	"github.com/firebase/genkit/go/ai"
	"github.com/mark3labs/mcp-go/server"
)

// Option adjusts how NewMCPServer runs tools.
type Option func(*options)

type options struct {
	callScope func() func()
}

// WithCallScope runs open before every tool call and the func it returns once
// the call is over, whether it succeeded or not.
func WithCallScope(open func() func()) Option {
	return func(o *options) { o.callScope = open }
}

// NewMCPServer builds an MCP server that exposes the given Genkit tools with
// their schemas intact. Duplicate names are skipped with a warning.
func NewMCPServer(name string, version string, toolList []ai.Tool, opts ...Option) *server.MCPServer {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
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
		s.AddTool(converted, newToolHandler(tl, o.callScope))
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
