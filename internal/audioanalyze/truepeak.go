package audioanalyze

import "math"

const (
	truePeakOversample   = 4
	truePeakTapsPerPhase = 12
)

var truePeakFilter = buildTruePeakFilter()

// buildTruePeakFilter returns a Hann-windowed sinc interpolator split into one
// short FIR per output phase. Each phase is normalised to unity gain at DC so a
// steady level is not misread.
func buildTruePeakFilter() [truePeakOversample][truePeakTapsPerPhase]float64 {
	n := truePeakOversample * truePeakTapsPerPhase
	center := float64(n-1) / 2
	var h [truePeakOversample][truePeakTapsPerPhase]float64
	for i := 0; i < n; i++ {
		x := (float64(i) - center) / truePeakOversample
		sinc := 1.0
		if x != 0 {
			sinc = math.Sin(math.Pi*x) / (math.Pi * x)
		}
		window := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
		h[i%truePeakOversample][i/truePeakOversample] = sinc * window
	}
	for p := range h {
		var sum float64
		for _, v := range h[p] {
			sum += v
		}
		for k := range h[p] {
			h[p][k] /= sum
		}
	}
	return h
}

// truePeakDBTP estimates the inter-sample peak by 4x oversampling (the method
// of ITU-R BS.1770-4 Annex 2). It never reads below the plain sample peak.
func truePeakDBTP(channels [][]float64) float64 {
	peak := 0.0
	for _, ch := range channels {
		for i := range ch {
			if a := math.Abs(ch[i]); a > peak {
				peak = a
			}
			for p := 0; p < truePeakOversample; p++ {
				var acc float64
				for k := 0; k < truePeakTapsPerPhase && i-k >= 0; k++ {
					acc += truePeakFilter[p][k] * ch[i-k]
				}
				if a := math.Abs(acc); a > peak {
					peak = a
				}
			}
		}
	}
	if peak <= 0 {
		return silenceFloorDB
	}
	return math.Max(20*math.Log10(peak), silenceFloorDB)
}
