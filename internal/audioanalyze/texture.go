package audioanalyze

import "math"

// Texture describes objective timbral/mix indicators: brightness (spectral
// centroid), dynamics (crest factor), and stereo width.
type Texture struct {
	BrightnessHz  float64
	CrestFactorDB float64
	StereoWidth   float64
	Stereo        bool
}

// estimateTexture computes brightness, dynamics, and stereo width. left/right
// are used only for stereo width; when they alias mono (mono source) width is 0.
func estimateTexture(mono, left, right []float64, sampleRate, channels int) Texture {
	return Texture{
		BrightnessHz:  round1(spectralCentroid(mono, sampleRate)),
		CrestFactorDB: round2(crestFactorDB(mono)),
		StereoWidth:   round2(stereoWidth(left, right, channels)),
		Stereo:        channels >= 2,
	}
}

// spectralCentroid is the magnitude-weighted mean frequency (Hz) averaged over
// frames — a common "brightness" proxy.
func spectralCentroid(samples []float64, sampleRate int) float64 {
	if len(samples) < keyFrameSize || sampleRate <= 0 {
		return 0
	}
	window := hannWindow(keyFrameSize)
	buf := make([]float64, keyFrameSize)
	var sumCentroid float64
	var frames int
	for start := 0; start+keyFrameSize <= len(samples); start += keyHopSize {
		for i := 0; i < keyFrameSize; i++ {
			buf[i] = samples[start+i] * window[i]
		}
		re, im := fftReal(buf)
		var weighted, total float64
		for bin := 1; bin < keyFrameSize/2; bin++ {
			mag := math.Hypot(re[bin], im[bin])
			freq := float64(bin) * float64(sampleRate) / float64(keyFrameSize)
			weighted += freq * mag
			total += mag
		}
		if total > 0 {
			sumCentroid += weighted / total
			frames++
		}
	}
	if frames == 0 {
		return 0
	}
	return sumCentroid / float64(frames)
}

// crestFactorDB is 20*log10(peak/rms): higher means more transient/dynamic,
// lower means more compressed/limited.
func crestFactorDB(samples []float64) float64 {
	peak, rms := levels(samples)
	if rms <= 1e-9 || peak <= 0 {
		return 0
	}
	return 20 * math.Log10(peak/rms)
}

// stereoWidth is the ratio of side energy to mid energy, where mid=(L+R)/2 and
// side=(L-R)/2. 0 = mono/centered; larger = wider.
func stereoWidth(left, right []float64, channels int) float64 {
	if channels < 2 || len(left) == 0 || len(right) != len(left) {
		return 0
	}
	var midSq, sideSq float64
	for i := range left {
		mid := (left[i] + right[i]) / 2
		side := (left[i] - right[i]) / 2
		midSq += mid * mid
		sideSq += side * side
	}
	midRMS := math.Sqrt(midSq / float64(len(left)))
	sideRMS := math.Sqrt(sideSq / float64(len(left)))
	if midRMS <= 1e-9 {
		if sideRMS <= 1e-9 {
			return 0
		}
		return 1
	}
	return sideRMS / midRMS
}
