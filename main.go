package main

import (
	"context"
	"log"
	"os"

	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/config"
	mcpinternal "github.com/nozomi-koborinai/ableton-osc-mcp/internal/mcp"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/taste"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/tools"
)

// These are injected at build time by GoReleaser.
// e.g. -ldflags "-X main.version=v0.1.0 -X main.commit=... -X main.date=..."
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// MCP uses stdout, so logs go to stderr.
	log.SetOutput(os.Stderr)

	ctx := context.Background()
	g := genkit.Init(ctx)

	cfg := config.Load()

	log.Printf("Starting ableton-osc-mcp %s (commit=%s, date=%s)", version, commit, date)

	ableton, err := abletonosc.NewClient(cfg.AbletonHost, cfg.AbletonPort, cfg.AbletonClientPort, cfg.Timeout)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = ableton.Close()
	}()
	tasteStore, err := taste.NewStore(cfg.TasteProfilePath)
	if err != nil {
		log.Fatal(err)
	}

	toolList := tools.Register(g, tools.Deps{
		Client:     ableton,
		TasteStore: tasteStore,
		Diagnose: tools.DiagnoseSettings{
			Host:       cfg.AbletonHost,
			Port:       cfg.AbletonPort,
			ClientPort: cfg.AbletonClientPort,
			Timeout:    cfg.Timeout,
		},
		Splice: tools.SpliceLibrarySettings{ConfiguredPath: cfg.SplicePath},
	})

	// Expose Genkit tools via MCP (stdio)
	mcpServer := mcpinternal.NewMCPServer(g, "ableton-osc-mcp", version, toolList)
	if err := mcpServer.ServeStdio(); err != nil {
		log.Fatal(err)
	}
}
