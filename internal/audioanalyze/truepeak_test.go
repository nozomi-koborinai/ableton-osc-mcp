package audioanalyze

import (
	"math"
	"testing"
)

func TestTruePeakFindsPeakBetweenSamples(t *testing.T) {
	t.Parallel()

	// A sine at a quarter of the sample rate, started at 45 degrees, is only
	// ever sampled at ±0.707 of its amplitude: the samples peak at -9.03 dBFS
	// while the waveform peaks at -6.02 dBFS (amplitude 0.5).
	for _, sr := range []int{44100, 48000} {
		tone := testSine(float64(sr)/4, 0.5, math.Pi/4, sr, sr)
		got := truePeakDBTP([][]float64{tone})
		if math.Abs(got-(-6.02)) > 0.2 {
			t.Errorf("sampleRate %d: true peak = %.2f dBTP, want -6.02 ±0.2", sr, got)
		}
	}
}

func TestTruePeakNeverReadsBelowSamplePeak(t *testing.T) {
	t.Parallel()

	tone := testSine(100, 1.0, 0, 48000, 48000) // samples reach full scale
	got := truePeakDBTP([][]float64{tone})
	if math.Abs(got) > 0.05 {
		t.Errorf("true peak = %.3f dBTP, want 0.0 ±0.05", got)
	}
}

func TestTruePeakTakesTheLouderChannel(t *testing.T) {
	t.Parallel()

	quiet := testSine(100, 0.1, 0, 48000, 48000)
	loud := testSine(100, 0.5, 0, 48000, 48000)
	got := truePeakDBTP([][]float64{quiet, loud})
	if math.Abs(got-(-6.02)) > 0.05 {
		t.Errorf("true peak = %.2f dBTP, want -6.02 (the louder channel)", got)
	}
}

func TestTruePeakFloorsSilence(t *testing.T) {
	t.Parallel()

	if got := truePeakDBTP([][]float64{make([]float64, 1000)}); got != -120.0 {
		t.Errorf("silence = %v, want -120", got)
	}
}
