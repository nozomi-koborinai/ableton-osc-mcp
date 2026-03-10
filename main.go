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
		// Song / Transport
		tools.NewAbletonTest(g, ableton),
		tools.NewAbletonShowMessage(g, ableton),
		tools.NewAbletonGetVersion(g, ableton),
		tools.NewAbletonGetTempo(g, ableton),
		tools.NewAbletonSetTempo(g, ableton),
		tools.NewAbletonPlay(g, ableton),
		tools.NewAbletonStop(g, ableton),
		tools.NewAbletonStopAllClips(g, ableton),
		tools.NewAbletonSetSongKey(g, ableton),
		tools.NewAbletonSessionRecord(g, ableton),
		tools.NewAbletonCaptureMidi(g, ableton),
		tools.NewAbletonSetMetronome(g, ableton),

		// Tracks
		tools.NewAbletonGetTrackNames(g, ableton),
		tools.NewAbletonGetTrackDevices(g, ableton),
		tools.NewAbletonCreateMidiTrack(g, ableton),
		tools.NewAbletonCreateAudioTrack(g, ableton),
		tools.NewAbletonSetTrackName(g, ableton),
		tools.NewAbletonMuteTrack(g, ableton),
		tools.NewAbletonSoloTrack(g, ableton),
		tools.NewAbletonArmTrack(g, ableton),
		tools.NewAbletonSetTrackVolume(g, ableton),
		tools.NewAbletonSetMonitoring(g, ableton),

		// Clips
		tools.NewAbletonCreateClip(g, ableton),
		tools.NewAbletonGetClipNotes(g, ableton),
		tools.NewAbletonFireClipSlot(g, ableton),
		tools.NewAbletonStopClip(g, ableton),
		tools.NewAbletonClearClipNotes(g, ableton),
		tools.NewAbletonAddMidiNotes(g, ableton),
		tools.NewAbletonDuplicateClipTo(g, ableton),
		tools.NewAbletonSetClipName(g, ableton),

		// Scenes
		tools.NewAbletonFireScene(g, ableton),
		tools.NewAbletonSetSceneName(g, ableton),
		tools.NewAbletonCreateScene(g, ableton),
		tools.NewAbletonDuplicateScene(g, ableton),

		// Devices
		tools.NewAbletonGetDeviceParameters(g, ableton),
		tools.NewAbletonSetDeviceParameter(g, ableton),

		// Raw OSC
		tools.NewAbletonOscSend(g, ableton),
	}

	// Expose Genkit tools via MCP (stdio)
	mcpServer := mcpinternal.NewMCPServer(g, "ableton-osc-mcp", version, toolList)
	if err := mcpServer.ServeStdio(); err != nil {
		log.Fatal(err)
	}
}
