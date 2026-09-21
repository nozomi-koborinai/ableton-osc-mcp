package audioanalyze

import "fmt"

// deepMaxSec bounds the deep analysis: two minutes hold dozens of rounds of
// any loop, and the window options choose which two minutes.
const deepMaxSec = 120

// analyzeDeep adds the grid, the harmony and the drum grid to a result. It
// returns a note when it could not: deep is a request, not a promise.
func analyzeDeep(out *Result, mono []float64, sampleRate int, opts Options, estimatedBPM, tuningCents float64, key KeyResult, keyOK bool) string {
	if limit := deepMaxSec * sampleRate; len(mono) > limit {
		mono = mono[:limit]
	}
	gridOptions := beatGridOptions{BPM: estimatedBPM, DownbeatSec: opts.DownbeatSec, DownbeatSet: opts.DownbeatSet, TuningCents: tuningCents}
	if opts.ProjectTempo > 0 {
		gridOptions.BPM, gridOptions.ExactTempo = opts.ProjectTempo, true
	}
	if gridOptions.BPM <= 0 {
		return "No beat grid: no tempo could be estimated. Pass project_tempo when the tempo is known."
	}
	grid, ok := buildBeatGrid(mono, sampleRate, gridOptions)
	if !ok {
		return fmt.Sprintf("No beat grid: the audio holds fewer than %d bars at %.1f BPM.", beatGridMinBars, gridOptions.BPM)
	}
	out.Grid = &grid
	if harmony, ok := estimateHarmony(mono, sampleRate, grid, tuningCents, key, keyOK); ok {
		out.Harmony = &harmony
	}
	if drums, ok := estimateDrumGrid(mono, sampleRate, grid); ok {
		out.DrumGrid = &drums
	}
	note := ""
	if !gridOptions.ExactTempo {
		note = "The grid stands on an estimated tempo: if it is half or double the real one, the drum grid reads at half or double speed. Pass project_tempo to settle it."
	}
	return note
}
