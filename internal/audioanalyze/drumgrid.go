package audioanalyze

import (
	"math"
	"sort"
	"strings"
)

const (
	drumStepsPerBar  = 16
	drumCycleBars    = 2 // trap and drill patterns tend to answer themselves over two bars
	drumMinBars      = 4
	drumHitShare     = 0.35 // a step sounds when it reaches this share of the lane's typical hit
	drumTopShare     = 0.1  // the typical hit: the mean of the strongest tenth of all steps
	drumPatternOften = 0.6  // x: sounds in most rounds
	drumPatternSome  = 0.3  // o: sounds in some
)

// DrumStep is one step of the two-bar cycle that sounds often enough to count.
type DrumStep struct {
	Step     int     `json:"step" jsonschema:"description=0-31: sixteenths over two bars"`
	Share    float64 `json:"share" jsonschema:"description=Share of the rounds in which the step sounds"`
	Strength float64 `json:"strength" jsonschema:"description=How hard it hits when it does\\, 0-1 within its lane"`
}

// DrumLane is the pattern of one frequency band.
type DrumLane struct {
	Name    string     `json:"name"`
	Hears   string     `json:"hears"`
	Pattern string     `json:"pattern" jsonschema:"description=Two bars of sixteenths: x sounds in most rounds\\, o in some\\, . hardly ever"`
	Steps   []DrumStep `json:"steps"`
}

// DrumGrid is where the drums fall, folded over the whole track: a pattern
// type, not a transcription of any one bar.
type DrumGrid struct {
	CycleBars    int        `json:"cycle_bars"`
	StepsPerBar  int        `json:"steps_per_bar"`
	BarsAnalyzed int        `json:"bars_analyzed"`
	Lanes        []DrumLane `json:"lanes"`
	Note         string     `json:"note"`
}

// estimateDrumGrid reads the onset envelope of three bands on a grid of
// sixteenths and folds it over two bars.
func estimateDrumGrid(samples []float64, sampleRate int, grid BeatGrid) (DrumGrid, bool) {
	if grid.Bars < drumMinBars || sampleRate <= 0 {
		return DrumGrid{}, false
	}
	return estimateDrumGridFrom(onsetEnvelopes(samples, sampleRate), sampleRate, grid)
}

func estimateDrumGridFrom(onsets onsetLanes, sampleRate int, grid BeatGrid) (DrumGrid, bool) {
	if grid.Bars < drumMinBars || sampleRate <= 0 {
		return DrumGrid{}, false
	}
	stepSec := grid.beatSec() / (drumStepsPerBar / beatsPerBar)
	cycleSteps := drumStepsPerBar * drumCycleBars
	rounds := grid.Bars / drumCycleBars
	reach := max(1, int(math.Round(stepSec/4*float64(sampleRate)/fluxHopSize))) // a quarter of a step either side

	lanes := []DrumLane{
		{Name: "low", Hears: "kick and 808 attacks (not told apart)"},
		{Name: "mid", Hears: "the crack of snares, claps and rims; a loud vocal muddies this lane"},
		{Name: "high", Hears: "hats and shakers"},
	}
	for l, envelope := range [][]float64{onsets.low, onsets.mid, onsets.high} {
		values := make([]float64, rounds*cycleSteps)
		for n := range values {
			centre := int(math.Round(((grid.DownbeatSec+float64(n)*stepSec)*float64(sampleRate) - fluxFrameSize/2) / fluxHopSize))
			for i := centre - reach; i <= centre+reach; i++ {
				if i >= 0 && i < len(envelope) {
					values[n] = math.Max(values[n], envelope[i])
				}
			}
		}
		// What a hit is, in this lane: measured against its own strongest steps, so
		// that the steady growth of a pad underneath does not count as drumming.
		sorted := append([]float64(nil), values...)
		sort.Sort(sort.Reverse(sort.Float64Slice(sorted)))
		top := sorted[:max(1, int(drumTopShare*float64(len(sorted))))]
		typical := 0.0
		for _, v := range top {
			typical += v / float64(len(top))
		}
		if typical <= 0 {
			lanes[l].Pattern = emptyDrumPattern()
			lanes[l].Steps = []DrumStep{}
			continue
		}

		var pattern strings.Builder
		lanes[l].Steps = []DrumStep{}
		for step := 0; step < cycleSteps; step++ {
			if step > 0 && step%drumStepsPerBar == 0 {
				pattern.WriteByte('|')
			}
			hits, strength := 0, 0.0
			for round := 0; round < rounds; round++ {
				if v := values[round*cycleSteps+step]; v >= drumHitShare*typical {
					hits++
					strength += v
				}
			}
			share := float64(hits) / float64(rounds)
			switch {
			case share >= drumPatternOften:
				pattern.WriteByte('x')
			case share >= drumPatternSome:
				pattern.WriteByte('o')
			default:
				pattern.WriteByte('.')
			}
			if share >= drumPatternSome {
				lanes[l].Steps = append(lanes[l].Steps, DrumStep{Step: step, Share: round2(share), Strength: round2(math.Min(1, strength/float64(hits)/sorted[0]))})
			}
		}
		lanes[l].Pattern = pattern.String()
	}

	note := "Folded over the whole track: a pattern type, not any one bar. Kick and 808 share the low lane. Swing is not measured: steps are straight sixteenths."
	if grid.DownbeatConfidence < 0.2 {
		note += " The downbeat is a guess here: the patterns may be rotated by one to three beats; pass downbeat_sec to pin it."
	}
	return DrumGrid{CycleBars: drumCycleBars, StepsPerBar: drumStepsPerBar, BarsAnalyzed: rounds * drumCycleBars, Lanes: lanes, Note: note}, true
}

func emptyDrumPattern() string {
	bar := strings.Repeat(".", drumStepsPerBar)
	return bar + "|" + bar
}
