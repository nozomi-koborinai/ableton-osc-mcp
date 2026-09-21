package audioanalyze

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeDurationReadsAnAIFFBounce(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "take.aif")
	if err := os.WriteFile(path, stereoToneAIFF(440, 0.5, 48000, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ProbeDuration(path)
	if err != nil || math.Abs(got-2) > 0.001 {
		t.Errorf("ProbeDuration() = %v, %v; want 2 s", got, err)
	}
	if _, err := ProbeDuration("relative.wav"); err == nil {
		t.Error("a relative path should be rejected like AnalyzeFile does")
	}
}
