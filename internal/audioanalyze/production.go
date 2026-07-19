package audioanalyze

import "math"

const (
	bandLowMaxHz  = 200.0
	bandMidMaxHz  = 2000.0
	maxRMSBeats   = 128
	prodFrameSize = 4096
	prodHopSize   = 2048
)

// TempoHypothesis is one plausible tempo reading (primary / half / double).
type TempoHypothesis struct {
	BPM        float64 `json:"bpm"`
	Confidence float64 `json:"confidence"`
	Label      string  `json:"label" jsonschema:"description=primary, half_time, or double_time"`
}

// KeyHypothesis is one plausible key/scale reading.
type KeyHypothesis struct {
	Tonic      string  `json:"tonic"`
	Scale      string  `json:"scale"`
	Confidence float64 `json:"confidence"`
	Label      string  `json:"label" jsonschema:"description=primary or alternative"`
}

// BandBalance is relative energy in low / mid / high bands (sums ≈ 1).
type BandBalance struct {
	Low  float64 `json:"low" jsonschema:"description=Share of energy below ~200 Hz"`
	Mid  float64 `json:"mid" jsonschema:"description=Share of energy ~200 Hz–2 kHz"`
	High float64 `json:"high" jsonschema:"description=Share of energy above ~2 kHz"`
}

// MatchAxis is one production decision axis for "what to match" when
// referencing a track — not a prescription, a ranked observation.
type MatchAxis struct {
	Name  string  `json:"name"`
	Score float64 `json:"score" jsonschema:"description=0..1 observation strength"`
	Hint  string  `json:"hint"`
}

// bpmAlternatives returns primary plus half/double-time readings when in range.
// Use when BPM confidence is low so agents can try 90 vs 180 (etc.) explicitly.
func bpmAlternatives(bpm, confidence float64) []TempoHypothesis {
	if bpm <= 0 {
		return nil
	}
	out := []TempoHypothesis{{
		BPM: bpm, Confidence: round2(confidence), Label: "primary",
	}}
	half := round1(bpm / 2)
	if half >= minBPM {
		out = append(out, TempoHypothesis{
			BPM: half, Confidence: round2(confidence * 0.65), Label: "half_time",
		})
	}
	dbl := round1(bpm * 2)
	if dbl <= maxBPM {
		out = append(out, TempoHypothesis{
			BPM: dbl, Confidence: round2(confidence * 0.65), Label: "double_time",
		})
	}
	return out
}

// estimateBandBalance accumulates spectral energy into low/mid/high bands.
func estimateBandBalance(samples []float64, sampleRate int) (BandBalance, bool) {
	if sampleRate <= 0 || len(samples) < prodFrameSize {
		return BandBalance{}, false
	}
	window := hannWindow(prodFrameSize)
	buf := make([]float64, prodFrameSize)
	var lowE, midE, highE float64
	frames := 0
	for start := 0; start+prodFrameSize <= len(samples); start += prodHopSize {
		for i := 0; i < prodFrameSize; i++ {
			buf[i] = samples[start+i] * window[i]
		}
		re, im := fftReal(buf)
		for bin := 1; bin < prodFrameSize/2; bin++ {
			mag := math.Hypot(re[bin], im[bin])
			e := mag * mag
			freq := float64(bin) * float64(sampleRate) / float64(prodFrameSize)
			switch {
			case freq < bandLowMaxHz:
				lowE += e
			case freq < bandMidMaxHz:
				midE += e
			default:
				highE += e
			}
		}
		frames++
		if frames >= 200 {
			break
		}
	}
	total := lowE + midE + highE
	if total <= 1e-18 {
		return BandBalance{}, false
	}
	return BandBalance{
		Low:  round2(lowE / total),
		Mid:  round2(midE / total),
		High: round2(highE / total),
	}, true
}

// rmsEnvelopePerBeat returns one RMS level per beat at the given tempo.
// Caps at maxRMSBeats so replies stay small for chop placement decisions.
func rmsEnvelopePerBeat(samples []float64, sampleRate int, bpm float64) []float64 {
	if bpm <= 0 || sampleRate <= 0 || len(samples) == 0 {
		return nil
	}
	beatSamples := int(math.Round(float64(sampleRate) * 60.0 / bpm))
	if beatSamples < 1 {
		return nil
	}
	nBeats := len(samples) / beatSamples
	if nBeats > maxRMSBeats {
		nBeats = maxRMSBeats
	}
	if nBeats < 1 {
		return nil
	}
	out := make([]float64, 0, nBeats)
	for b := 0; b < nBeats; b++ {
		start := b * beatSamples
		end := start + beatSamples
		if end > len(samples) {
			end = len(samples)
		}
		var sumSq float64
		for _, s := range samples[start:end] {
			sumSq += s * s
		}
		n := end - start
		if n < 1 {
			out = append(out, 0)
			continue
		}
		out = append(out, round3(math.Sqrt(sumSq/float64(n))))
	}
	return out
}

// rhythmDensityOnsetsPerBar is onsets per 4-beat bar at the given tempo.
func rhythmDensityOnsetsPerBar(onsetCount int, durationSec, bpm float64) float64 {
	if bpm <= 0 || durationSec <= 0 || onsetCount <= 0 {
		return 0
	}
	bars := durationSec * (bpm / 60.0) / 4.0
	if bars < 0.25 {
		bars = 0.25
	}
	return round2(float64(onsetCount) / bars)
}

// buildMatchAxes returns three production-oriented "what to match" axes derived
// from density, low-end share, and space proxies (stereo + crest).
func buildMatchAxes(densityPerBar float64, bands BandBalance, stereoWidth, crestDB float64) []MatchAxis {
	// Density: ~2 onsets/bar = sparse, ~8 = busy 16ths-ish, ~16 = very dense.
	densScore := clamp01((densityPerBar - 1) / 12)
	densHint := "sparse hits — leave room; avoid filling every 16th"
	if densScore > 0.66 {
		densHint = "dense rhythm — match hit rate / hat motion before melody"
	} else if densScore > 0.33 {
		densHint = "medium density — pocket and placement matter more than busyness"
	}

	lowScore := clamp01(bands.Low / 0.45) // ~0.45 low share ≈ heavy sub role
	lowHint := "thin low end — sub/808 may need to carry the weight"
	if lowScore > 0.66 {
		lowHint = "low-end forward — duck competing bass; watch kick/sub masking"
	} else if lowScore > 0.33 {
		lowHint = "balanced lows — keep kick and bass roles distinct"
	}

	// Space: wider stereo + lower crest (more compressed/sustained) ≈ more "wash".
	space := clamp01(0.55*clamp01(stereoWidth) + 0.45*clamp01((12-crestDB)/12))
	spaceHint := "dry / upfront — short amps and tight FX"
	if space > 0.66 {
		spaceHint = "spacious / washed — match reverb/delay amount and stereo width"
	} else if space > 0.33 {
		spaceHint = "moderate space — light verb/delay; keep transient edge"
	}

	return []MatchAxis{
		{Name: "drum_density", Score: round2(densScore), Hint: densHint},
		{Name: "low_end_role", Score: round2(lowScore), Hint: lowHint},
		{Name: "space_amount", Score: round2(space), Hint: spaceHint},
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}
