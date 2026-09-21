package tools

import "math"

// barLeadSeconds is how long before a bar line a quantized command is sent.
// Commands need about a tenth of a second to reach Live, whatever the tempo.
const barLeadSeconds = 0.5

// barLeadBeats is barLeadSeconds in beats: at least a beat, never the whole bar.
func barLeadBeats(tempo float64, beatsPerBar int) float64 {
	return math.Min(math.Max(1, barLeadSeconds*tempo/60), float64(beatsPerBar)-0.5)
}

// nextSafeBarLine returns the current song time and the bar line a quantized
// command sent now will land on. If that line is closer than the lead, it waits
// for it to pass first: a command sent just before a bar line arrives after it
// and would wait a whole bar more.
func nextSafeBarLine(c recordClient, sleep auditionSleeper, tempo float64, beatsPerBar int) (float64, float64, error) {
	now, err := queryCurrentSongTime(c)
	if err != nil {
		return 0, 0, err
	}
	line := ceilBarBeat(now, beatsPerBar)
	if line-now < barLeadBeats(tempo, beatsPerBar) {
		if err := waitUntilSongTime(c, sleep, line, tempo); err != nil {
			return 0, 0, err
		}
		if now, err = queryCurrentSongTime(c); err != nil {
			return 0, 0, err
		}
		line = ceilBarBeat(now, beatsPerBar)
	}
	return now, line, nil
}
