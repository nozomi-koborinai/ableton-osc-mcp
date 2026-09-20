package audioanalyze

import (
	"math"
	"math/rand"
	"testing"
)

func bandDB(t *testing.T, p MixProfile, label string) float64 {
	t.Helper()
	for _, b := range p.Bands {
		if b.Label == label {
			return b.DB
		}
	}
	t.Fatalf("band %q missing from %+v", label, p.Bands)
	return 0
}

func TestMeasureMixBandsFollowTheSpectrum(t *testing.T) {
	t.Parallel()

	sr := 48000
	low := testSine(100, 0.5, 0, sr, sr*4)
	high := testSine(3000, 0.5*math.Pow(10, -10.0/20), 0, sr, sr*4) // 10 dB quieter
	mix := make([]float64, len(low))
	for i := range mix {
		mix[i] = low[i] + high[i]
	}

	got := measureMix(mix, mix, sr, 2)

	wantLabels := []string{"<60", "60-120", "120-250", "250-500", "500-1k", "1-2k", "2-4k", "4-8k", "8-16k"}
	if len(got.Bands) != len(wantLabels) {
		t.Fatalf("bands = %d, want %d", len(got.Bands), len(wantLabels))
	}
	for i, label := range wantLabels {
		if got.Bands[i].Label != label {
			t.Errorf("band %d label = %q, want %q", i, got.Bands[i].Label, label)
		}
	}
	if diff := bandDB(t, got, "60-120") - bandDB(t, got, "2-4k"); math.Abs(diff-10) > 0.5 {
		t.Errorf("60-120 minus 2-4k = %.2f dB, want 10 ±0.5", diff)
	}
	if db := bandDB(t, got, "60-120"); db < -1 {
		t.Errorf("60-120 = %.2f dB, want the dominant band (above -1 dB of the total)", db)
	}
	if db := bandDB(t, got, "500-1k"); db > -60 {
		t.Errorf("500-1k = %.2f dB, want an empty band (below -60 dB)", db)
	}
}

func TestMeasureMixWidthSeparatesMonoWideAndInverted(t *testing.T) {
	t.Parallel()

	sr := 48000
	rng := rand.New(rand.NewSource(1))
	left := make([]float64, sr*4)
	other := make([]float64, sr*4)
	inverted := make([]float64, sr*4)
	for i := range left {
		left[i] = rng.Float64()*2 - 1
		other[i] = rng.Float64()*2 - 1
		inverted[i] = -left[i]
	}

	same := measureMix(left, left, sr, 2)
	wide := measureMix(left, other, sr, 2)
	flipped := measureMix(left, inverted, sr, 2)

	if len(same.Width) != 3 {
		t.Fatalf("width bands = %d, want 3", len(same.Width))
	}
	for i, label := range []string{"250-1k", "1-4k", "4-16k"} {
		if same.Width[i].Label != label {
			t.Errorf("width band %d label = %q, want %q", i, same.Width[i].Label, label)
		}
		if same.Width[i].LRCorrelation != 1 || same.Width[i].SideMinusMidDB != -120 {
			t.Errorf("%s identical channels: corr=%v side-mid=%v, want 1 and -120", label, same.Width[i].LRCorrelation, same.Width[i].SideMinusMidDB)
		}
		if flipped.Width[i].LRCorrelation != -1 || flipped.Width[i].SideMinusMidDB != 120 {
			t.Errorf("%s inverted channels: corr=%v side-mid=%v, want -1 and 120", label, flipped.Width[i].LRCorrelation, flipped.Width[i].SideMinusMidDB)
		}
		if math.Abs(wide.Width[i].LRCorrelation) > 0.1 || math.Abs(wide.Width[i].SideMinusMidDB) > 1 {
			t.Errorf("%s independent noise: corr=%v side-mid=%v, want about 0 and 0", label, wide.Width[i].LRCorrelation, wide.Width[i].SideMinusMidDB)
		}
	}
}

func TestMeasureMixLevels(t *testing.T) {
	t.Parallel()

	sr := 48000
	tone := testSine(1000, math.Pow(10, -23.0/20), 0, sr, sr*5)

	stereo := measureMix(tone, tone, sr, 2)
	if math.Abs(stereo.LUFSIntegrated-(-23.0)) > 0.1 {
		t.Errorf("stereo LUFS = %.2f, want -23.0 ±0.1", stereo.LUFSIntegrated)
	}
	if math.Abs(stereo.TruePeakDBTP-(-23.0)) > 0.2 || math.Abs(stereo.SamplePeakDBFS-(-23.0)) > 0.05 {
		t.Errorf("peaks = %.2f dBTP / %.2f dBFS, want -23.0", stereo.TruePeakDBTP, stereo.SamplePeakDBFS)
	}
	// A sine's peak sits 3.01 dB above its RMS.
	if math.Abs(stereo.CrestDB-3.01) > 0.05 {
		t.Errorf("crest = %.2f dB, want 3.01", stereo.CrestDB)
	}
	if math.Abs(stereo.AnalyzedSec-5) > 0.01 {
		t.Errorf("analyzed_sec = %v, want 5", stereo.AnalyzedSec)
	}

	// BS.1770 counts a mono file as one channel: 3 dB below the same tone in stereo.
	mono := measureMix(tone, tone, sr, 1)
	if math.Abs(mono.LUFSIntegrated-(-26.0)) > 0.1 {
		t.Errorf("mono LUFS = %.2f, want -26.0 ±0.1", mono.LUFSIntegrated)
	}
}

func TestMeasureMixStaysFiniteOnSilence(t *testing.T) {
	t.Parallel()

	silence := make([]float64, 48000)
	got := measureMix(silence, silence, 48000, 2)

	values := []float64{got.LUFSIntegrated, got.TruePeakDBTP, got.SamplePeakDBFS, got.CrestDB}
	for _, b := range got.Bands {
		values = append(values, b.DB)
	}
	for _, w := range got.Width {
		values = append(values, w.LRCorrelation, w.SideMinusMidDB)
	}
	for i, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("value %d = %v; JSON cannot carry NaN or Inf", i, v)
		}
	}
	if got.LUFSIntegrated != -120 || got.CrestDB != 0 {
		t.Errorf("silence: LUFS=%v crest=%v, want -120 and 0", got.LUFSIntegrated, got.CrestDB)
	}
}
