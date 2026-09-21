package audioanalyze

import (
	"math"
	"testing"
)

// testSine returns n samples of a sine at freq Hz with the given peak
// amplitude and start phase.
func testSine(freq, amp, phase float64, sampleRate, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = amp * math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)+phase)
	}
	return out
}

func TestIntegratedLUFSMatchesEBUReferenceTone(t *testing.T) {
	t.Parallel()

	// EBU Tech 3341, case 1: a 1 kHz stereo sine peaking at -23 dBFS reads
	// -23.0 LUFS. Checked at both rates this server meets, because the
	// K-weighting coefficients have to follow the sample rate.
	amp := math.Pow(10, -23.0/20)
	for _, sr := range []int{44100, 48000} {
		tone := testSine(1000, amp, 0, sr, sr*20)
		got := integratedLUFS([][]float64{tone, tone}, sr)
		if math.Abs(got-(-23.0)) > 0.1 {
			t.Errorf("sampleRate %d: LUFS = %.2f, want -23.0 ±0.1", sr, got)
		}
	}
}

func TestIntegratedLUFSGatesOutSilence(t *testing.T) {
	t.Parallel()

	sr := 48000
	amp := math.Pow(10, -23.0/20)
	tone := testSine(1000, amp, 0, sr, sr*10)
	padded := append(append([]float64{}, tone...), make([]float64, sr*10)...)

	// Without gating, ten seconds of silence would pull the reading down 3 dB.
	got := integratedLUFS([][]float64{padded, padded}, sr)
	if math.Abs(got-(-23.0)) > 0.1 {
		t.Errorf("LUFS with trailing silence = %.2f, want -23.0 ±0.1", got)
	}
}

func TestIntegratedLUFSRelativeGateDropsQuietPassages(t *testing.T) {
	t.Parallel()

	// Ten seconds at -23 dBFS, then ten at -50 dBFS. The quiet half is far above
	// the -70 LUFS absolute gate, so only the relative gate (10 LU under the
	// programme level) can keep it from dragging the reading down by 3 dB.
	sr := 48000
	loud := testSine(1000, math.Pow(10, -23.0/20), 0, sr, sr*10)
	quiet := testSine(1000, math.Pow(10, -50.0/20), 0, sr, sr*10)
	programme := append(append([]float64{}, loud...), quiet...)

	got := integratedLUFS([][]float64{programme, programme}, sr)
	if math.Abs(got-(-23.0)) > 0.15 {
		t.Errorf("LUFS = %.2f, want -23.0 ±0.15 (a reading near -26 means the relative gate is missing)", got)
	}
}

func TestIntegratedLUFSFloorsInsteadOfInfinity(t *testing.T) {
	t.Parallel()

	sr := 48000
	silence := make([]float64, sr*2)
	if got := integratedLUFS([][]float64{silence, silence}, sr); got != -120.0 {
		t.Errorf("silence = %v, want -120", got)
	}
	short := testSine(1000, 0.5, 0, sr, sr/10) // 100 ms: shorter than one gating block
	if got := integratedLUFS([][]float64{short}, sr); got != -120.0 {
		t.Errorf("100 ms clip = %v, want -120", got)
	}
}
