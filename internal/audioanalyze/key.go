package audioanalyze

import "math"

// Key detection uses a chromagram (12 pitch-class energies) matched against the
// Krumhansl-Kessler key profiles. It reports an approximate global key/scale
// for reference only; dense, produced mixes can fool it, so callers should
// treat it as a hint rather than ground truth.

const (
	keyFrameSize = 4096
	keyHopSize   = 2048
	// Restrict binning to a musical fundamental range to reduce noise/harmonics.
	keyMinFreq = 55.0   // ~A1
	keyMaxFreq = 2000.0 // ~B6
)

var pitchClassNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// Krumhansl-Kessler tonal hierarchy profiles (C-rooted).
var (
	majorProfile = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	minorProfile = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// KeyResult holds an approximate key estimate.
type KeyResult struct {
	Tonic      string  // e.g. "C", "F#"
	Scale      string  // "major" or "minor"
	Confidence float64 // 0..1 margin between the best and next-best fit
}

// estimateKey computes a chromagram and matches it to major/minor key profiles.
func estimateKey(samples []float64, sampleRate int) (KeyResult, bool) {
	if sampleRate <= 0 || len(samples) < keyFrameSize {
		return KeyResult{}, false
	}
	chroma := chromagram(samples, sampleRate)
	var total float64
	for _, v := range chroma {
		total += v
	}
	if total <= 0 {
		return KeyResult{}, false
	}

	bestCorr := math.Inf(-1)
	secondCorr := math.Inf(-1)
	var bestTonic int
	var bestScale string
	consider := func(tonic int, scale string, corr float64) {
		if corr > bestCorr {
			secondCorr = bestCorr
			bestCorr = corr
			bestTonic = tonic
			bestScale = scale
		} else if corr > secondCorr {
			secondCorr = corr
		}
	}
	for tonic := 0; tonic < 12; tonic++ {
		consider(tonic, "major", correlateProfile(chroma, majorProfile, tonic))
		consider(tonic, "minor", correlateProfile(chroma, minorProfile, tonic))
	}
	if math.IsInf(bestCorr, -1) {
		return KeyResult{}, false
	}

	confidence := 0.0
	if !math.IsInf(secondCorr, -1) && bestCorr > 0 {
		confidence = math.Max(0, math.Min(1, (bestCorr-secondCorr)/math.Max(bestCorr, 1e-9)))
	}
	return KeyResult{
		Tonic:      pitchClassNames[bestTonic],
		Scale:      bestScale,
		Confidence: round2(confidence),
	}, true
}

// correlateProfile is the Pearson correlation between the chroma vector and a
// key profile rotated so that index 0 aligns with the given tonic.
func correlateProfile(chroma [12]float64, profile [12]float64, tonic int) float64 {
	var rotated [12]float64
	for i := 0; i < 12; i++ {
		rotated[i] = profile[((i-tonic)%12+12)%12]
	}
	return pearson(chroma[:], rotated[:])
}

func pearson(a, b []float64) float64 {
	n := float64(len(a))
	var sumA, sumB float64
	for i := range a {
		sumA += a[i]
		sumB += b[i]
	}
	meanA := sumA / n
	meanB := sumB / n
	var cov, varA, varB float64
	for i := range a {
		da := a[i] - meanA
		db := b[i] - meanB
		cov += da * db
		varA += da * da
		varB += db * db
	}
	if varA <= 1e-12 || varB <= 1e-12 {
		return 0
	}
	return cov / math.Sqrt(varA*varB)
}

// chromagram accumulates spectral magnitude into 12 pitch classes across frames.
func chromagram(samples []float64, sampleRate int) [12]float64 {
	var chroma [12]float64
	window := hannWindow(keyFrameSize)
	buf := make([]float64, keyFrameSize)
	minBin := int(math.Floor(keyMinFreq * float64(keyFrameSize) / float64(sampleRate)))
	maxBin := int(math.Ceil(keyMaxFreq * float64(keyFrameSize) / float64(sampleRate)))
	if minBin < 1 {
		minBin = 1
	}
	if maxBin > keyFrameSize/2 {
		maxBin = keyFrameSize / 2
	}

	for start := 0; start+keyFrameSize <= len(samples); start += keyHopSize {
		for i := 0; i < keyFrameSize; i++ {
			buf[i] = samples[start+i] * window[i]
		}
		re, im := fftReal(buf)
		for bin := minBin; bin <= maxBin; bin++ {
			mag := math.Hypot(re[bin], im[bin])
			if mag <= 0 {
				continue
			}
			freq := float64(bin) * float64(sampleRate) / float64(keyFrameSize)
			midi := 69.0 + 12.0*math.Log2(freq/440.0)
			pc := int(math.Round(midi)) % 12
			if pc < 0 {
				pc += 12
			}
			chroma[pc] += mag
		}
	}
	return chroma
}

func hannWindow(n int) []float64 {
	w := make([]float64, n)
	for i := 0; i < n; i++ {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	return w
}

// fftReal returns the real/imag parts of the FFT of a real-valued input. The
// input length is padded up to the next power of two.
func fftReal(input []float64) ([]float64, []float64) {
	n := nextPow2(len(input))
	re := make([]float64, n)
	im := make([]float64, n)
	copy(re, input)
	fftInPlace(re, im)
	return re, im
}

func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// fftInPlace is an iterative radix-2 Cooley-Tukey FFT. len(re)==len(im) must be
// a power of two.
func fftInPlace(re, im []float64) {
	n := len(re)
	if n <= 1 {
		return
	}
	// Bit-reversal permutation.
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wRe, wIm := math.Cos(ang), math.Sin(ang)
		for i := 0; i < n; i += length {
			curRe, curIm := 1.0, 0.0
			half := length >> 1
			for j := 0; j < half; j++ {
				uRe, uIm := re[i+j], im[i+j]
				vRe := re[i+j+half]*curRe - im[i+j+half]*curIm
				vIm := re[i+j+half]*curIm + im[i+j+half]*curRe
				re[i+j], im[i+j] = uRe+vRe, uIm+vIm
				re[i+j+half], im[i+j+half] = uRe-vRe, uIm-vIm
				curRe, curIm = curRe*wRe-curIm*wIm, curRe*wIm+curIm*wRe
			}
		}
	}
}
