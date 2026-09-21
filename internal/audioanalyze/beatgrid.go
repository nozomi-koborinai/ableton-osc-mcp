package audioanalyze

import "math"

const (
	fluxFrameSize = 1024
	fluxHopSize   = 256
	// An estimated tempo is refined within this share of itself: over three
	// minutes, a third of a percent is half a second of drift.
	beatTempoSearch       = 0.015
	beatTempoStep         = 0.02 // BPM
	beatGridMinBars       = 2
	lowOnsetSpanSec       = 0.05
	beatMinTempo          = 60.0
	beatMaxTempo          = 200.0
	beatFitMargin         = 0.03 // a related tempo has to fit this much better than the estimate to replace it
	sixteenthFitWindowSec = 0.015
	beatsPerBar           = 4 // 4/4 is assumed throughout
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

// lowOnsetEnvelope is the onset envelope of the low end, on the same frames as
// bandFluxes. A spectrum cannot do this: a frame short enough to time a kick is
// shorter than a cycle of an 808, and its low bins swell and shrink with every
// swing of the wave. So the band is cut out in the time domain and rectified,
// and each moment is asked how much louder the 50 ms after it are than the
// 50 ms before it.
func lowOnsetEnvelope(samples []float64, sampleRate int) []float64 {
	frames := 0
	if len(samples) >= fluxFrameSize {
		frames = (len(samples)-fluxFrameSize)/fluxHopSize + 1
	}
	out := make([]float64, frames)
	if frames == 0 {
		return out
	}
	band := highPassBiquad(fluxBandLow[0], sampleRate).apply(samples)
	lowPass := lowPassBiquad(fluxBandLow[1], sampleRate)
	band = lowPass.apply(lowPass.apply(band))
	sums := make([]float64, len(band)+1) // running sum of the rectified band
	for i, v := range band {
		sums[i+1] = sums[i] + math.Abs(v)
	}
	span := int(lowOnsetSpanSec * float64(sampleRate))
	for f := range out {
		centre := f*fluxHopSize + fluxFrameSize/2
		if centre-span < 0 || centre+span > len(band) {
			continue
		}
		if rise := (sums[centre+span] - 2*sums[centre] + sums[centre-span]) / float64(span); rise > 0 {
			out[f] = rise
		}
	}
	return out
}

// Second-order Butterworth sections (RBJ cookbook, Q = 1/sqrt 2).
func lowPassBiquad(cutoffHz float64, sampleRate int) biquad {
	w := 2 * math.Pi * cutoffHz / float64(sampleRate)
	alpha, cos := math.Sin(w)/math.Sqrt2, math.Cos(w)
	a0 := 1 + alpha
	return biquad{b0: (1 - cos) / 2 / a0, b1: (1 - cos) / a0, b2: (1 - cos) / 2 / a0, a1: -2 * cos / a0, a2: (1 - alpha) / a0}
}

func highPassBiquad(cutoffHz float64, sampleRate int) biquad {
	w := 2 * math.Pi * cutoffHz / float64(sampleRate)
	alpha, cos := math.Sin(w)/math.Sqrt2, math.Cos(w)
	a0 := 1 + alpha
	return biquad{b0: (1 + cos) / 2 / a0, b1: -(1 + cos) / a0, b2: (1 + cos) / 2 / a0, a1: -2 * cos / a0, a2: (1 - alpha) / a0}
}

// onsetLanes are the three envelopes everything rhythmic is read from, each
// scaled to its own strongest onset. They are worked out once per analysis.
type onsetLanes struct {
	low, mid, high []float64
}

func onsetEnvelopes(samples []float64, sampleRate int) onsetLanes {
	fluxes := bandFluxes(samples, sampleRate, fluxBandMid, fluxBandHigh)
	lanes := onsetLanes{low: lowOnsetEnvelope(samples, sampleRate), mid: fluxes[0], high: fluxes[1]}
	for _, envelope := range [][]float64{lanes.low, lanes.mid, lanes.high} {
		strongest := 0.0
		for _, v := range envelope {
			strongest = math.Max(strongest, v)
		}
		for i := range envelope {
			if strongest > 0 {
				envelope[i] /= strongest
			}
		}
	}
	return lanes
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
	return buildBeatGridFrom(onsetEnvelopes(samples, sampleRate), samples, sampleRate, opts)
}

func buildBeatGridFrom(lanes onsetLanes, samples []float64, sampleRate int, opts beatGridOptions) (BeatGrid, bool) {
	if sampleRate <= 0 || opts.BPM <= 0 {
		return BeatGrid{}, false
	}
	durationSec := float64(len(samples)) / float64(sampleRate)
	if durationSec < float64(beatGridMinBars*beatsPerBar)*60/opts.BPM {
		return BeatGrid{}, false
	}
	low := lanes.low
	// Kicks and snares sit on the beats; hats fill the gaps evenly and would pull
	// the grid on to the off-beats as easily as on to the beats.
	pulse := make([]float64, len(low))
	for i := range pulse {
		pulse[i] = low[i] + lanes.mid[i]
	}

	grid := BeatGrid{BPM: opts.BPM}
	if opts.DownbeatSet {
		grid.DownbeatSec = opts.DownbeatSec
		grid.BeatOffsetSec = math.Mod(opts.DownbeatSec, grid.beatSec())
		grid.DownbeatConfidence = 1
	} else {
		// An estimated tempo is often a simple ratio away from the real one (two
		// thirds, on a swung drill beat). Each related tempo gets its best grid, and
		// the one whose sixteenths the onsets actually sit on wins; the estimate
		// itself stays unless another is clearly better.
		everything := make([]float64, len(pulse))
		for i := range everything {
			everything[i] = pulse[i] + lanes.high[i]
		}
		bestFit := -1.0
		for _, ratio := range []float64{1, 1.5, 2.0 / 3, 0.75, 4.0 / 3, 2, 0.5} {
			centre := opts.BPM * ratio
			// A tempo that is known is taken as it is, whatever it is. The range only
			// keeps the search for a related tempo among tempos music is counted in,
			// and the estimate itself is always a candidate.
			if opts.ExactTempo && ratio != 1 {
				continue
			}
			if ratio != 1 && (centre < beatMinTempo || centre > beatMaxTempo) {
				continue
			}
			bpm, phase := combBeats(pulse, sampleRate, durationSec, centre, opts.ExactTempo)
			if fit := sixteenthFit(everything, sampleRate, bpm, phase); fit > bestFit+beatFitMargin || bestFit < 0 {
				bestFit, grid.BPM, grid.BeatOffsetSec = fit, bpm, phase
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

// combBeats finds tempo and phase together: every beat of a candidate grid,
// summed. An estimated tempo is searched a little either side of itself.
func combBeats(pulse []float64, sampleRate int, durationSec, centreBPM float64, exact bool) (float64, float64) {
	hopSec := float64(fluxHopSize) / float64(sampleRate)
	lowest, highest := centreBPM, centreBPM
	if !exact {
		lowest, highest = centreBPM*(1-beatTempoSearch), centreBPM*(1+beatTempoSearch)
	}
	best, bestBPM, bestPhase := -1.0, centreBPM, 0.0
	for bpm := lowest; bpm <= highest+1e-9; bpm += beatTempoStep {
		beatSec := 60 / bpm
		for phase := 0.0; phase < beatSec; phase += hopSec {
			score := 0.0
			for at := phase; at < durationSec; at += beatSec {
				score += envelopeAt(pulse, at, sampleRate)
			}
			if score /= durationSec / beatSec; score > best {
				best, bestBPM, bestPhase = score, bpm, phase
			}
		}
	}
	return bestBPM, bestPhase
}

// sixteenthFit says how much of the onset energy falls on the grid's sixteenths,
// beyond what would fall there by chance. The window around a grid point is a
// fixed 15 ms, the width of an onset in the envelope: a window that grew with
// the step would flatter slow tempos, whose wider windows swallow whole onsets.
func sixteenthFit(onsets []float64, sampleRate int, bpm, phaseSec float64) float64 {
	stepSec := 60 / bpm / 4
	var near, total float64
	for i, v := range onsets {
		if v <= 0 {
			continue
		}
		at := (float64(i*fluxHopSize)+fluxFrameSize/2)/float64(sampleRate) - phaseSec
		total += v
		if math.Abs(at-math.Round(at/stepSec)*stepSec) <= sixteenthFitWindowSec {
			near += v
		}
	}
	chance := 2 * sixteenthFitWindowSec / stepSec
	if total <= 0 || chance >= 1 {
		return 0
	}
	return (near/total - chance) / (1 - chance)
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
