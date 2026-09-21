package audioanalyze

import "math"

const (
	// Peaks below this are too coarse on the FFT grid to read cents off them.
	tuningMinFreq = 200.0
	tuningMaxFreq = 2000.0
	// A peak counts when it reaches this share of its frame's strongest one.
	tuningPeakShare = 0.1
	// Below this the offsets are scattered, as they are for drums and noise,
	// and no correction is applied.
	tuningMinConfidence = 0.2
)

// Tuning says how far a track sits from A = 440 equal temperament. Records are
// often pitched up or down as a whole; 45 cents flat is half way to the next
// key as far as a semitone grid is concerned, and splits every note in two.
type Tuning struct {
	Cents      float64 `json:"cents" jsonschema:"description=Offset from A=440 equal temperament\\, -50 to +50. Already applied to key and chords"`
	Confidence float64 `json:"confidence" jsonschema:"description=0-1. Low for drums and noise\\, where there is no pitch to be in or out of tune; below 0.2 no correction is applied"`
	A4Hz       float64 `json:"a4_hz" jsonschema:"description=What A4 is in this track"`
}

// estimateTuning reads the spectral peaks of every frame to a fraction of a bin
// and averages their offsets from the nearest semitone on a circle (so that
// +49 and -49 cents are neighbours), weighted by magnitude. The length of that
// average is the confidence: tonal material agrees with itself, noise does not.
func estimateTuning(samples []float64, sampleRate int) Tuning {
	none := Tuning{A4Hz: 440}
	if sampleRate <= 0 || len(samples) < keyFrameSize {
		return none
	}
	window := hannWindow(keyFrameSize)
	buf := make([]float64, keyFrameSize)
	mags := make([]float64, keyFrameSize/2+1)
	minBin := max(2, int(math.Ceil(tuningMinFreq*float64(keyFrameSize)/float64(sampleRate))))
	maxBin := min(keyFrameSize/2-2, int(math.Floor(tuningMaxFreq*float64(keyFrameSize)/float64(sampleRate))))

	var sumCos, sumSin, sumWeight float64
	for start := 0; start+keyFrameSize <= len(samples); start += keyHopSize {
		for i := range buf {
			buf[i] = samples[start+i] * window[i]
		}
		re, im := fftReal(buf)
		strongest := 0.0
		for bin := minBin - 1; bin <= maxBin+1; bin++ {
			mags[bin] = math.Hypot(re[bin], im[bin])
			strongest = math.Max(strongest, mags[bin])
		}
		if strongest <= 0 {
			continue
		}
		for bin := minBin; bin <= maxBin; bin++ {
			if mags[bin] < tuningPeakShare*strongest || mags[bin] <= mags[bin-1] || mags[bin] < mags[bin+1] || mags[bin-1] <= 0 || mags[bin+1] <= 0 {
				continue
			}
			// A parabola through the log magnitudes of the peak and its neighbours.
			a, b, c := math.Log(mags[bin-1]), math.Log(mags[bin]), math.Log(mags[bin+1])
			shift := 0.0
			if denominator := a - 2*b + c; denominator < 0 {
				shift = 0.5 * (a - c) / denominator
			}
			freq := (float64(bin) + shift) * float64(sampleRate) / float64(keyFrameSize)
			midi := 69 + 12*math.Log2(freq/440)
			angle := 2 * math.Pi * (midi - math.Round(midi))
			sumCos += mags[bin] * math.Cos(angle)
			sumSin += mags[bin] * math.Sin(angle)
			sumWeight += mags[bin]
		}
	}
	if sumWeight <= 0 {
		return none
	}
	cents := math.Atan2(sumSin, sumCos) / (2 * math.Pi) * 100
	return Tuning{
		Cents:      round2(cents),
		Confidence: round2(math.Min(1, math.Hypot(sumCos, sumSin)/sumWeight)),
		A4Hz:       round2(440 * math.Pow(2, cents/1200)),
	}
}

// correction is the offset to apply: none when the estimate is not to be trusted.
func (t Tuning) correction() float64 {
	if t.Confidence < tuningMinConfidence {
		return 0
	}
	return t.Cents
}
