package audioanalyze

import (
	"math"
	"testing"
)

func TestFFTSinePeak(t *testing.T) {
	t.Parallel()

	const n = 1024
	const sampleRate = 8192
	const freq = 512.0 // exactly bin 64 (freq*n/sampleRate)
	in := make([]float64, n)
	for i := range in {
		in[i] = math.Sin(2 * math.Pi * freq * float64(i) / float64(sampleRate))
	}
	re, im := fftReal(in)
	wantBin := int(freq * n / sampleRate)
	peakBin, peakMag := 0, -1.0
	for b := 1; b < n/2; b++ {
		mag := math.Hypot(re[b], im[b])
		if mag > peakMag {
			peakMag, peakBin = mag, b
		}
	}
	if peakBin != wantBin {
		t.Fatalf("FFT peak bin = %d, want %d", peakBin, wantBin)
	}
}

func TestEstimateKeyCMajorTriad(t *testing.T) {
	t.Parallel()

	// C4, E4, G4 sustained -> should read as C major.
	samples := tones(44100, 5, 261.63, 329.63, 392.0)
	got, ok := estimateKey(samples, 44100)
	if !ok {
		t.Fatal("estimateKey returned ok=false")
	}
	if got.Tonic != "C" || got.Scale != "major" {
		t.Fatalf("key = %s %s (conf %.2f), want C major", got.Tonic, got.Scale, got.Confidence)
	}
}

func TestEstimateKeyGMajorScale(t *testing.T) {
	t.Parallel()

	// G major scale tones (G A B C D E F# G).
	freqs := []float64{392.0, 440.0, 493.88, 523.25, 587.33, 659.25, 739.99, 783.99}
	var samples []float64
	sr := 44100
	for _, f := range freqs {
		samples = append(samples, tones(sr, 1, f)...)
	}
	got, ok := estimateKey(samples, sr)
	if !ok {
		t.Fatal("estimateKey returned ok=false")
	}
	if got.Tonic != "G" || got.Scale != "major" {
		t.Fatalf("key = %s %s (conf %.2f), want G major", got.Tonic, got.Scale, got.Confidence)
	}
}

// tones synthesizes the sum of the given sine frequencies for the duration.
func tones(sampleRate, seconds int, freqs ...float64) []float64 {
	n := sampleRate * seconds
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		var v float64
		for _, f := range freqs {
			v += math.Sin(2 * math.Pi * f * float64(i) / float64(sampleRate))
		}
		out[i] = v / float64(len(freqs))
	}
	return out
}
