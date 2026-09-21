package tools

import (
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

func NewAbletonAudition(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_audition",
		"Ableton Live: play 2-8 labelled variants back to back, switching on bar lines, so the listener can answer with one letter. Each variant is the current state plus its own changes — clips to play instead, track volume changes in dB, devices switched on or off — and a variant with no changes (X) keeps the current state in the line-up. Runs in real time and blocks until the end: one pass is plays × bars_per_variant bars, after up to a bar of waiting. It starts playback if it is stopped, sets clip launch quantization to 1 bar for the duration, and adds one audio track named 'Audition' at the end of the set whose name shows which variant is sounding. Faders, devices, playing clips and quantization are put back when it ends, when it fails, and when the listener stops playback. It plays what exists: write variant clips first (e.g. ableton_clip_write). Use when the listener is to choose between alternatives by ear; change one thing per variant. Pass commit only after the listener has named their choice: it plays nothing, writes that variant into the set and puts nothing back.",
		func(_ *ai.ToolContext, input AuditionInput) (AuditionOutput, error) {
			return runAudition(client, time.Sleep, input)
		},
	)
}
