package audioanalyze

import "testing"

func TestEstimateSectionsTooShort(t *testing.T) {
	t.Parallel()

	samples := tones(44100, 2, 261.63)
	if _, ok := estimateSections(samples, 44100); ok {
		t.Fatal("expected ok=false for short audio")
	}
}

func TestEstimateSectionsEnergyContrast(t *testing.T) {
	t.Parallel()

	sr := 44100
	// 10s quiet C major, then 10s loud C major (same harmony, big energy jump).
	quiet := scale(tones(sr, 10, 261.63, 329.63, 392.0), 0.1)
	loud := tones(sr, 10, 261.63, 329.63, 392.0)
	samples := append(quiet, loud...)

	sections, ok := estimateSections(samples, sr)
	if !ok {
		t.Fatal("estimateSections returned ok=false")
	}
	if len(sections) < 2 {
		t.Fatalf("expected at least 2 sections, got %d: %+v", len(sections), sections)
	}
	// A boundary should land near the 10s mark.
	foundBoundary := false
	for _, s := range sections[1:] {
		if s.StartSec >= 7 && s.StartSec <= 13 {
			foundBoundary = true
		}
	}
	if !foundBoundary {
		t.Errorf("no section boundary near the 10s energy jump: %+v", sections)
	}
	// The later, louder section should carry a higher energy label than the first.
	if sections[0].Energy >= sections[len(sections)-1].Energy {
		t.Errorf("expected energy to rise across sections: %+v", sections)
	}
}

func scale(samples []float64, factor float64) []float64 {
	out := make([]float64, len(samples))
	for i, s := range samples {
		out[i] = s * factor
	}
	return out
}
