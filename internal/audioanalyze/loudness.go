package audioanalyze

import "math"

// silenceFloorDB stands in for -Inf, which JSON cannot carry.
const silenceFloorDB = -120.0

type biquad struct{ b0, b1, b2, a1, a2 float64 }

func (q biquad) apply(x []float64) []float64 {
	y := make([]float64, len(x))
	var x1, x2, y1, y2 float64
	for i, v := range x {
		o := q.b0*v + q.b1*x1 + q.b2*x2 - q.a1*y1 - q.a2*y2
		x2, x1 = x1, v
		y2, y1 = y1, o
		y[i] = o
	}
	return y
}

// kWeightingFilters returns the two ITU-R BS.1770-4 stages (high shelf, then
// high-pass) for sampleRate. At 48 kHz they equal the coefficients printed in
// the standard.
func kWeightingFilters(sampleRate int) (biquad, biquad) {
	const (
		shelfHz = 1681.974450955533
		shelfDB = 3.999843853973347
		shelfQ  = 0.7071752369554196
		hpHz    = 38.13547087602444
		hpQ     = 0.5003270373238773
	)
	fs := float64(sampleRate)

	k := math.Tan(math.Pi * shelfHz / fs)
	vh := math.Pow(10, shelfDB/20)
	vb := math.Pow(vh, 0.4996667741545416)
	a0 := 1 + k/shelfQ + k*k
	shelf := biquad{
		b0: (vh + vb*k/shelfQ + k*k) / a0,
		b1: 2 * (k*k - vh) / a0,
		b2: (vh - vb*k/shelfQ + k*k) / a0,
		a1: 2 * (k*k - 1) / a0,
		a2: (1 - k/shelfQ + k*k) / a0,
	}

	k = math.Tan(math.Pi * hpHz / fs)
	a0 = 1 + k/hpQ + k*k
	highPass := biquad{b0: 1, b1: -2, b2: 1, a1: 2 * (k*k - 1) / a0, a2: (1 - k/hpQ + k*k) / a0}
	return shelf, highPass
}

// integratedLUFS measures programme loudness per ITU-R BS.1770-4: K-weighting,
// 400 ms blocks with 75% overlap, a -70 LUFS absolute gate and a -10 LU
// relative gate. channels holds left and right (or one mono channel).
func integratedLUFS(channels [][]float64, sampleRate int) float64 {
	if len(channels) == 0 || sampleRate <= 0 {
		return silenceFloorDB
	}
	block := int(0.4 * float64(sampleRate))
	step := int(0.1 * float64(sampleRate))
	n := len(channels[0])
	if block == 0 || step == 0 || n < block {
		return silenceFloorDB
	}

	shelf, highPass := kWeightingFilters(sampleRate)
	weighted := make([][]float64, len(channels))
	for i, ch := range channels {
		weighted[i] = highPass.apply(shelf.apply(ch))
	}

	// Per block: mean square summed over channels (channel weights are 1).
	var powers []float64
	for start := 0; start+block <= n; start += step {
		var sum float64
		for _, ch := range weighted {
			var sq float64
			for _, v := range ch[start : start+block] {
				sq += v * v
			}
			sum += sq / float64(block)
		}
		powers = append(powers, sum)
	}

	loudness := func(p float64) float64 { return -0.691 + 10*math.Log10(p) }
	meanOf := func(keep func(float64) bool) (float64, bool) {
		var sum float64
		var count int
		for _, p := range powers {
			if p > 0 && keep(p) {
				sum += p
				count++
			}
		}
		if count == 0 {
			return 0, false
		}
		return sum / float64(count), true
	}

	aboveAbsolute := func(p float64) bool { return loudness(p) > -70 }
	mean, ok := meanOf(aboveAbsolute)
	if !ok {
		return silenceFloorDB
	}
	relativeGate := loudness(mean) - 10
	mean, ok = meanOf(func(p float64) bool { return aboveAbsolute(p) && loudness(p) > relativeGate })
	if !ok {
		return silenceFloorDB
	}
	return math.Max(loudness(mean), silenceFloorDB)
}
