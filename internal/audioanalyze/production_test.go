package audioanalyze

import (
	"math"
	"testing"
)

func TestBPMAlternativesIncludesHalfAndDouble(t *testing.T) {
	// 90 → half 45 out of range; double 180 in range.
	alts := bpmAlternatives(90, 0.5)
	labels := map[string]float64{}
	for _, a := range alts {
		labels[a.Label] = a.BPM
	}
	if labels["primary"] != 90 {
		t.Errorf("primary = %v", labels["primary"])
	}
	if labels["double_time"] != 180 {
		t.Errorf("double_time = %v, want 180", labels["double_time"])
	}
	if _, ok := labels["half_time"]; ok {
		t.Errorf("half_time should be omitted below minBPM, got %v", labels["half_time"])
	}

	// 140 → half 70 in range; double 280 out of range.
	alts = bpmAlternatives(140, 0.5)
	labels = map[string]float64{}
	for _, a := range alts {
		labels[a.Label] = a.BPM
	}
	if labels["half_time"] != 70 {
		t.Errorf("half_time = %v, want 70", labels["half_time"])
	}
	if _, ok := labels["double_time"]; ok {
		t.Errorf("double_time should be omitted above maxBPM, got %v", labels["double_time"])
	}
}

func TestRMSEnvelopePerBeat(t *testing.T) {
	sr := 44100
	bpm := 120.0
	beatN := sr / 2 // 0.5s at 120
	samples := make([]float64, beatN*8)
	for i := beatN * 4; i < len(samples); i++ {
		samples[i] = 0.5
	}
	got := rmsEnvelopePerBeat(samples, sr, bpm)
	if len(got) != 8 {
		t.Fatalf("len = %d, want 8", len(got))
	}
	if got[0] > 0.01 {
		t.Errorf("quiet beat rms = %v", got[0])
	}
	if got[5] < 0.4 {
		t.Errorf("loud beat rms = %v", got[5])
	}
}

func TestEstimateBandBalanceBassHeavy(t *testing.T) {
	sr := 44100
	n := sr * 2
	samples := make([]float64, n)
	for i := range samples {
		samples[i] = math.Sin(2 * math.Pi * 110 * float64(i) / float64(sr))
	}
	got, ok := estimateBandBalance(samples, sr)
	if !ok {
		t.Fatal("estimateBandBalance returned ok=false")
	}
	if got.Low < got.Mid || got.Low < got.High {
		t.Errorf("expected low-dominant balance, got %+v", got)
	}
}

func TestRhythmDensityAndMatchAxes(t *testing.T) {
	d := rhythmDensityOnsetsPerBar(16, 8.0, 120) // 8s at 120 = 4 bars → 4 onsets/bar
	if math.Abs(d-4) > 0.1 {
		t.Errorf("density = %v, want ~4", d)
	}
	axes := buildMatchAxes(d, BandBalance{Low: 0.5, Mid: 0.3, High: 0.2}, 0.8, 4)
	if len(axes) != 3 {
		t.Fatalf("axes len = %d", len(axes))
	}
	names := map[string]bool{}
	for _, a := range axes {
		names[a.Name] = true
		if a.Hint == "" {
			t.Errorf("axis %s missing hint", a.Name)
		}
	}
	for _, want := range []string{"drum_density", "low_end_role", "space_amount"} {
		if !names[want] {
			t.Errorf("missing axis %s", want)
		}
	}
}
