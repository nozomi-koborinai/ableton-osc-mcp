package main

import (
	"context"
	"log"
	"os"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/config"
	mcpinternal "github.com/nozomi-koborinai/ableton-osc-mcp/internal/mcp"
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

	toolList := []ai.Tool{
		tools.NewAbletonTest(g, ableton),
		tools.NewAbletonShowMessage(g, ableton),
		tools.NewAbletonGetVersion(g, ableton),
		tools.NewAbletonGetTempo(g, ableton),
		tools.NewAbletonSetTempo(g, ableton),
		tools.NewAbletonGetTrackNames(g, ableton),
		tools.NewAbletonGetTrackDevices(g, ableton),
		tools.NewAbletonCreateMidiTrack(g, ableton),
		tools.NewAbletonCreateClip(g, ableton),
		tools.NewAbletonGetClipNotes(g, ableton),
		tools.NewAbletonFireClipSlot(g, ableton),
		tools.NewAbletonClearClipNotes(g, ableton),
		tools.NewAbletonAddMidiNotes(g, ableton),
		tools.NewAbletonOscSend(g, ableton),
	}

	// Expose Genkit tools via MCP (stdio)
	mcpServer := mcpinternal.NewMCPServer(g, "ableton-osc-mcp", version, toolList)
	if err := mcpServer.ServeStdio(); err != nil {
		log.Fatal(err)
	}
}
