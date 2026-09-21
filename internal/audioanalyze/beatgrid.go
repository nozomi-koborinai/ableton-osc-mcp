package audioanalyze

import "math"

const (
	fluxFrameSize = 1024
	fluxHopSize   = 256
	// An estimated tempo is refined within this share of itself: over three
	// minutes, a third of a percent is half a second of drift.
	beatTempoSearch = 0.015
	beatTempoStep   = 0.02 // BPM
	beatGridMinBars = 2
	beatsPerBar     = 4 // 4/4 is assumed throughout
)

// Bands of the onset envelopes. Low is kick and 808 attacks, mid the crack of
// snares and claps, high is hats.
var (
	fluxBandLow  = [2]float64{30, 150}
	fluxBandMid  = [2]float64{1500, 5000}
	fluxBandHigh = [2]float64{7000, 20000}
)

// BeatGrid places beats and bar lines on the audio.
type BeatGrid struct {
	BPM                float64 `json:"bpm"`
	BeatOffsetSec      float64 `json:"beat_offset_sec" jsonschema:"description=Time of the first beat"`
	DownbeatSec        float64 `json:"downbeat_sec" jsonschema:"description=Time of the first bar line"`
	DownbeatConfidence float64 `json:"downbeat_confidence" jsonschema:"description=0-1. When low\\, chords and drum patterns may be rotated by one to three beats; pass downbeat_sec to pin it"`
	Bars               int     `json:"bars" jsonschema:"description=Whole bars on the grid"`
}

type beatGridOptions struct {
	BPM         float64
	ExactTempo  bool // the tempo is known (a project tempo), not estimated
	DownbeatSec float64
	DownbeatSet bool
	TuningCents float64
}

func (g BeatGrid) beatSec() float64 { return 60 / g.BPM }

// bandFluxes returns one onset envelope per band: the frame-to-frame growth of
// the spectrum inside the band, at fluxHopSize spacing.
func bandFluxes(samples []float64, sampleRate int, bands ...[2]float64) [][]float64 {
	frames := 0
	if len(samples) >= fluxFrameSize {
		frames = (len(samples)-fluxFrameSize)/fluxHopSize + 1
	}
	out := make([][]float64, len(bands))
	for i := range out {
		out[i] = make([]float64, frames)
	}
	window := hannWindow(fluxFrameSize)
	buf := make([]float64, fluxFrameSize)
	previous := make([]float64, fluxFrameSize/2+1)
	current := make([]float64, fluxFrameSize/2+1)
	binHz := float64(sampleRate) / fluxFrameSize
	for f := 0; f < frames; f++ {
		start := f * fluxHopSize
		for i := range buf {
			buf[i] = samples[start+i] * window[i]
		}
		re, im := fftReal(buf)
		for bin := range current {
			current[bin] = math.Hypot(re[bin], im[bin])
		}
		if f > 0 {
			for b, band := range bands {
				lo := max(1, int(math.Floor(band[0]/binHz)))
				hi := min(len(current)-1, int(math.Ceil(band[1]/binHz)))
				for bin := lo; bin <= hi; bin++ {
					if rise := current[bin] - previous[bin]; rise > 0 {
						out[b][f] += rise
					}
				}
			}
		}
		previous, current = current, previous
	}
	return out
}

// envelopeAt reads an envelope at a time in seconds, taking the largest of the
// frames next to it: an onset rarely falls on a frame. A frame speaks for its
// centre: that is where an onset makes the spectrum grow fastest.
func envelopeAt(envelope []float64, sec float64, sampleRate int) float64 {
	centre := int(math.Round((sec*float64(sampleRate) - fluxFrameSize/2) / fluxHopSize))
	best := 0.0
	for i := centre - 1; i <= centre+1; i++ {
		if i >= 0 && i < len(envelope) {
			best = math.Max(best, envelope[i])
		}
	}
	return best
}

// buildBeatGrid finds where the beats fall and which of them starts a bar.
func buildBeatGrid(samples []float64, sampleRate int, opts beatGridOptions) (BeatGrid, bool) {
	if sampleRate <= 0 || opts.BPM <= 0 {
		return BeatGrid{}, false
	}
	durationSec := float64(len(samples)) / float64(sampleRate)
	if durationSec < float64(beatGridMinBars*beatsPerBar)*60/opts.BPM {
		return BeatGrid{}, false
	}
	hopSec := float64(fluxHopSize) / float64(sampleRate)
	fluxes := bandFluxes(samples, sampleRate, fluxBandLow, fluxBandMid)
	low := fluxes[0]
	// Kicks and snares sit on the beats; hats fill the gaps evenly and would pull
	// the grid on to the off-beats as easily as on to the beats.
	pulse := make([]float64, len(low))
	for i := range pulse {
		pulse[i] = low[i] + fluxes[1][i]
	}

	grid := BeatGrid{BPM: opts.BPM}
	if opts.DownbeatSet {
		grid.DownbeatSec = opts.DownbeatSec
		grid.BeatOffsetSec = math.Mod(opts.DownbeatSec, grid.beatSec())
		grid.DownbeatConfidence = 1
	} else {
		// The comb: every beat of a candidate grid, summed. Tempo and phase together.
		best := -1.0
		lowest, highest := opts.BPM, opts.BPM
		if !opts.ExactTempo {
			lowest, highest = opts.BPM*(1-beatTempoSearch), opts.BPM*(1+beatTempoSearch)
		}
		for bpm := lowest; bpm <= highest+1e-9; bpm += beatTempoStep {
			beatSec := 60 / bpm
			for phase := 0.0; phase < beatSec; phase += hopSec {
				score := 0.0
				for at := phase; at < durationSec; at += beatSec {
					score += envelopeAt(pulse, at, sampleRate)
				}
				if score /= durationSec / beatSec; score > best {
					best, grid.BPM, grid.BeatOffsetSec = score, bpm, phase
				}
			}
		}
		grid.BPM = round2(grid.BPM)
		grid.DownbeatSec, grid.DownbeatConfidence = findDownbeat(samples, sampleRate, grid, low, opts.TuningCents)
	}
	grid.Bars = int(math.Floor((durationSec - grid.DownbeatSec) / (beatsPerBar * grid.beatSec())))
	if grid.Bars < beatGridMinBars {
		return BeatGrid{}, false
	}
	grid.BeatOffsetSec, grid.DownbeatSec = round3(grid.BeatOffsetSec), round3(grid.DownbeatSec)
	return grid, true
}

// findDownbeat picks which beat starts the bar. Two things tend to happen on a
// downbeat, whatever the style: the low end hits hardest, and the harmony
// changes. Each of the four rotations is scored on both.
func findDownbeat(samples []float64, sampleRate int, grid BeatGrid, low []float64, tuningCents float64) (float64, float64) {
	beatSec := grid.beatSec()
	beats := int((float64(len(samples))/float64(sampleRate) - grid.BeatOffsetSec) / beatSec)
	if beats < beatsPerBar*beatGridMinBars {
		return grid.BeatOffsetSec, 0
	}

	// Chroma per beat, from the frames that fall inside it.
	frames := frameChromasTuned(samples, sampleRate, tuningCents)
	chroma := make([][12]float64, beats)
	for f, frame := range frames {
		centre := (float64(f)*float64(keyHopSize) + keyFrameSize/2) / float64(sampleRate)
		if b := int(math.Floor((centre - grid.BeatOffsetSec) / beatSec)); b >= 0 && b < beats {
			for pc, v := range frame {
				chroma[b][pc] += v
			}
		}
	}

	var hits, changes [beatsPerBar]float64
	for b := 0; b < beats; b++ {
		hits[b%beatsPerBar] += envelopeAt(low, grid.BeatOffsetSec+float64(b)*beatSec, sampleRate)
		if b > 0 {
			changes[b%beatsPerBar] += 1 - cosine12(chroma[b], chroma[b-1])
		}
	}
	share := func(values [beatsPerBar]float64) [beatsPerBar]float64 {
		total := 0.0
		for _, v := range values {
			total += v
		}
		if total <= 0 {
			return [beatsPerBar]float64{}
		}
		for i := range values {
			values[i] /= total
		}
		return values
	}
	hits, changes = share(hits), share(changes)

	best, second, rotation := -1.0, -1.0, 0
	for r := 0; r < beatsPerBar; r++ {
		score := hits[r] + changes[r]
		if score > best {
			best, second, rotation = score, best, r
		} else if score > second {
			second = score
		}
	}
	confidence := 0.0
	if best > 0 {
		confidence = round2(math.Min(1, (best-second)/best))
	}
	return grid.BeatOffsetSec + float64(rotation)*beatSec, confidence
}

func cosine12(a, b [12]float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na <= 0 || nb <= 0 {
		return 1 // nothing to compare: no change
	}
	return dot / math.Sqrt(na*nb)
}
