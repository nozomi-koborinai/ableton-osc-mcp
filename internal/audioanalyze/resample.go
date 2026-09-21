package audioanalyze

import (
	"fmt"
	"math"
)

const (
	// resampleHalfTaps is the half-width of the low-pass, in samples of the
	// slower of the two rates. With the window below, 48k<->44.1k is flat to
	// 20 kHz and 100 dB down from 23.8 kHz on: what cannot be represented is
	// gone before it could fold back into the audio band.
	resampleHalfTaps   = 40
	resampleKaiserBeta = 10.06 // about -100 dB of stopband
	// resampleMaxPhases bounds the filter table. Every pair of standard rates
	// needs a few hundred phases at most (48k->44.1k: 147).
	resampleMaxPhases = 2048
)

// resampleRational converts a signal between two sample rates with a polyphase
// windowed-sinc low-pass: the exact rational ratio, no drift, no delay. The
// cutoff sits at the Nyquist frequency of the slower rate.
func resampleRational(x []float64, from, to int) ([]float64, error) {
	if from <= 0 || to <= 0 {
		return nil, fmt.Errorf("sample rates must be positive, got %d and %d", from, to)
	}
	if from == to {
		return append([]float64(nil), x...), nil
	}
	g := gcdInt(from, to)
	up, down := to/g, from/g
	if up > resampleMaxPhases {
		return nil, fmt.Errorf("cannot convert %d Hz to %d Hz: the ratio %d/%d needs too large a filter", from, to, up, down)
	}

	// Going down, the filter has to be as sharp relative to the new rate as it
	// would be going up, so it gets wider in input samples.
	slowdown := math.Max(1, float64(from)/float64(to))
	half := int(math.Ceil(resampleHalfTaps * slowdown))
	cutoff := 0.5 / slowdown // cycles per input sample

	// One row of coefficients per position an output sample can take between
	// two input samples. Row p serves the output instants i0 + p/up.
	table := make([][]float64, up)
	for p := range table {
		row := make([]float64, 2*half)
		sum := 0.0
		for m := range row {
			tau := float64(p)/float64(up) + float64(half-1-m) // output instant minus input sample, in input samples
			row[m] = sincLowPass(tau, cutoff) * kaiserWindow(tau/float64(half), resampleKaiserBeta)
			sum += row[m]
		}
		for m := range row {
			row[m] /= sum // exact gain at DC, the same in every row
		}
		table[p] = row
	}

	out := make([]float64, (len(x)*up+down-1)/down)
	for n := range out {
		pos := n * down
		first := pos/up - half + 1
		acc := 0.0
		for m, c := range table[pos%up] {
			if j := first + m; j >= 0 && j < len(x) {
				acc += c * x[j]
			}
		}
		out[n] = acc
	}
	return out, nil
}

// sincLowPass is the impulse response of an ideal low-pass with the given
// cutoff (cycles per sample), at a distance of tau samples.
func sincLowPass(tau, cutoff float64) float64 {
	if tau == 0 {
		return 2 * cutoff
	}
	return math.Sin(2*math.Pi*cutoff*tau) / (math.Pi * tau)
}

// kaiserWindow is the Kaiser window at r in [-1, 1], zero outside.
func kaiserWindow(r, beta float64) float64 {
	if r < -1 || r > 1 {
		return 0
	}
	return besselI0(beta*math.Sqrt(1-r*r)) / besselI0(beta)
}

// besselI0 is the modified Bessel function of the first kind, order zero.
func besselI0(x float64) float64 {
	sum, term := 1.0, 1.0
	for k := 1; k < 200; k++ {
		term *= (x / 2) / float64(k)
		sum += term * term
		if term*term < 1e-16*sum {
			break
		}
	}
	return sum
}

func gcdInt(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
