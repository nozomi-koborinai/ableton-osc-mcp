package audioanalyze

import "math"

// Section structure detection groups frames into ~1s blocks, builds a
// self-similarity matrix over combined harmonic+energy features, and finds
// boundaries with a Foote checkerboard novelty function. Each resulting section
// is labeled by relative energy (low/medium/high) so callers can spot likely
// intros/breakdowns vs. drops/choruses. It is an approximate structural map,
// not a semantic label of the song's form.

const (
	sectionBlockSec  = 1.0 // block resolution for structure
	sectionKernelMax = 8   // checkerboard kernel half-size cap
	maxSectionBlocks = 900 // bound compute (~15 min at 1s blocks)
	minSectionBlocks = 8   // below this, structure is not meaningful
	energyLowRatio   = 0.40
	energyHighRatio  = 0.75
	// sectionEnergyWeight scales the energy dimension so loudness contrasts
	// (e.g. breakdown -> drop with the same harmony) still create boundaries.
	sectionEnergyWeight = 3.0
)

// Section is one detected structural span.
type Section struct {
	StartSec    float64 `json:"start_sec"`
	DurationSec float64 `json:"duration_sec"`
	Energy      float64 `json:"energy"` // 0..1 relative to the loudest section
	Label       string  `json:"label"`  // low | medium | high (energy tier)
}

// estimateSections returns a coarse structural map of the audio. It returns
// ok=false when the audio is too short to segment meaningfully.
func estimateSections(samples []float64, sampleRate int) ([]Section, bool) {
	if sampleRate <= 0 {
		return nil, false
	}
	frames := frameChromas(samples, sampleRate)
	frameSec := frameDurationSec(sampleRate)
	framesPerBlock := int(math.Round(sectionBlockSec / frameSec))
	if framesPerBlock < 1 {
		framesPerBlock = 1
	}

	chromas, energies := blockFeatures(frames, framesPerBlock)
	if len(chromas) > maxSectionBlocks {
		chromas = chromas[:maxSectionBlocks]
		energies = energies[:maxSectionBlocks]
	}
	if len(chromas) < minSectionBlocks {
		return nil, false
	}
	blockSec := float64(framesPerBlock) * frameSec

	features := combinedFeatures(chromas, energies)
	novelty := footeNovelty(features)
	boundaries := pickBoundaries(novelty)

	return buildSections(boundaries, energies, blockSec), true
}

// blockFeatures aggregates per-frame chroma into blocks and returns the summed
// chroma plus the total energy of each block.
func blockFeatures(frames [][12]float64, framesPerBlock int) ([][12]float64, []float64) {
	var chromas [][12]float64
	var energies []float64
	for start := 0; start < len(frames); start += framesPerBlock {
		end := start + framesPerBlock
		if end > len(frames) {
			end = len(frames)
		}
		var acc [12]float64
		var energy float64
		for _, f := range frames[start:end] {
			for pc := 0; pc < 12; pc++ {
				acc[pc] += f[pc]
				energy += f[pc]
			}
		}
		chromas = append(chromas, acc)
		energies = append(energies, energy)
	}
	return chromas, energies
}

// combinedFeatures builds L2-normalized vectors of (harmonic shape + relative
// energy) for similarity comparison.
func combinedFeatures(chromas [][12]float64, energies []float64) [][]float64 {
	maxEnergy := 0.0
	for _, e := range energies {
		if e > maxEnergy {
			maxEnergy = e
		}
	}
	if maxEnergy <= 0 {
		maxEnergy = 1
	}

	out := make([][]float64, len(chromas))
	for i := range chromas {
		vec := make([]float64, 13)
		var norm float64
		for pc := 0; pc < 12; pc++ {
			vec[pc] = chromas[i][pc]
			norm += chromas[i][pc] * chromas[i][pc]
		}
		norm = math.Sqrt(norm)
		if norm > 0 {
			for pc := 0; pc < 12; pc++ {
				vec[pc] /= norm
			}
		}
		vec[12] = sectionEnergyWeight * energies[i] / maxEnergy
		out[i] = vec
	}
	return out
}

// footeNovelty correlates a checkerboard kernel along the self-similarity
// matrix diagonal to produce a boundary-novelty curve.
func footeNovelty(features [][]float64) []float64 {
	n := len(features)
	half := sectionKernelMax
	if half > n/2 {
		half = n / 2
	}
	if half < 1 {
		half = 1
	}
	kernel := checkerboardKernel(half)
	novelty := make([]float64, n)
	for c := 0; c < n; c++ {
		var sum float64
		for i := -half; i < half; i++ {
			for j := -half; j < half; j++ {
				ri, rj := c+i, c+j
				if ri < 0 || rj < 0 || ri >= n || rj >= n {
					continue
				}
				sum += kernel[i+half][j+half] * cosineSim(features[ri], features[rj])
			}
		}
		if sum > 0 {
			novelty[c] = sum
		}
	}
	return novelty
}

func checkerboardKernel(half int) [][]float64 {
	size := 2 * half
	k := make([][]float64, size)
	sigma := float64(half) / 2.0
	for i := 0; i < size; i++ {
		k[i] = make([]float64, size)
		for j := 0; j < size; j++ {
			di := float64(i-half) + 0.5
			dj := float64(j-half) + 0.5
			g := math.Exp(-(di*di + dj*dj) / (2 * sigma * sigma))
			sign := 1.0
			// Opposite quadrants are positive; adjacent are negative.
			if (i < half) != (j < half) {
				sign = -1.0
			}
			k[i][j] = sign * g
		}
	}
	return k
}

func cosineSim(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na <= 1e-12 || nb <= 1e-12 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

// pickBoundaries selects novelty peaks above an adaptive threshold, always
// including the start (block 0) as the first boundary.
func pickBoundaries(novelty []float64) []int {
	n := len(novelty)
	boundaries := []int{0}
	if n < 3 {
		return boundaries
	}
	mean, std := meanStd(novelty)
	threshold := mean + 0.6*std
	minGap := sectionKernelMax
	if minGap < 2 {
		minGap = 2
	}
	last := 0
	for i := 1; i < n-1; i++ {
		if novelty[i] < threshold {
			continue
		}
		if novelty[i] >= novelty[i-1] && novelty[i] >= novelty[i+1] && i-last >= minGap {
			boundaries = append(boundaries, i)
			last = i
		}
	}
	return boundaries
}

func meanStd(v []float64) (float64, float64) {
	if len(v) == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	mean := sum / float64(len(v))
	var varSum float64
	for _, x := range v {
		d := x - mean
		varSum += d * d
	}
	return mean, math.Sqrt(varSum / float64(len(v)))
}

// buildSections turns block boundaries into timed sections with energy labels.
func buildSections(boundaries []int, energies []float64, blockSec float64) []Section {
	maxEnergy := 0.0
	for _, e := range energies {
		if e > maxEnergy {
			maxEnergy = e
		}
	}
	if maxEnergy <= 0 {
		maxEnergy = 1
	}

	var sections []Section
	for bi, start := range boundaries {
		end := len(energies)
		if bi+1 < len(boundaries) {
			end = boundaries[bi+1]
		}
		if end <= start {
			continue
		}
		var sum float64
		for _, e := range energies[start:end] {
			sum += e
		}
		avg := sum / float64(end-start)
		ratio := avg / maxEnergy
		sections = append(sections, Section{
			StartSec:    round2(float64(start) * blockSec),
			DurationSec: round2(float64(end-start) * blockSec),
			Energy:      round2(ratio),
			Label:       energyLabel(ratio),
		})
	}
	return sections
}

func energyLabel(ratio float64) string {
	switch {
	case ratio < energyLowRatio:
		return "low"
	case ratio < energyHighRatio:
		return "medium"
	default:
		return "high"
	}
}
