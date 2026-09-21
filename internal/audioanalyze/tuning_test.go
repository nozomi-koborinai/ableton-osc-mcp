package audioanalyze

import (
	"math"
	"testing"
)

// detunedNotes renders MIDI notes as sums of three harmonics, the whole thing
// shifted by `cents` away from A = 440 equal temperament.
func detunedNotes(notes []int, cents float64, sampleRate int, seconds float64) []float64 {
	out := make([]float64, int(seconds*float64(sampleRate)))
	for _, note := range notes {
		freq := 440 * math.Pow(2, (float64(note)-69)/12+cents/1200)
		for h, amp := range []float64{1, 0.5, 0.25} {
			for i := range out {
				out[i] += 0.1 * amp * math.Sin(2*math.Pi*freq*float64(h+1)*float64(i)/float64(sampleRate))
			}
		}
	}
	return out
}

func testNoise(n int) []float64 {
	out := make([]float64, n)
	state := uint64(7)
	for i := range out {
		state = state*6364136223846793005 + 1442695040888963407
		out[i] = float64(int64(state>>11))/float64(1<<52) - 1
	}
	return out
}

func TestEstimateTuningFindsHowFarATrackSitsFromConcertPitch(t *testing.T) {
	t.Parallel()

	for _, cents := range []float64{-45, 20, 0} {
		got := estimateTuning(detunedNotes([]int{48, 60, 64, 67, 72}, cents, 44100, 3), 44100)
		if math.Abs(got.Cents-cents) > 3 || got.Confidence < 0.5 {
			t.Errorf("%+.0f cents: got %+.1f cents with confidence %.2f, want within 3 cents and sure of it", cents, got.Cents, got.Confidence)
		}
		if want := 440 * math.Pow(2, got.Cents/1200); math.Abs(got.A4Hz-want) > 0.01 {
			t.Errorf("a4_hz = %v, want %v", got.A4Hz, want)
		}
	}
}

func TestEstimateTuningIsUnsureAboutNoise(t *testing.T) {
	t.Parallel()

	got := estimateTuning(testNoise(3*44100), 44100)
	if got.Confidence >= 0.2 {
		t.Errorf("noise: confidence = %.2f, want it low: there is no pitch to be in or out of tune", got.Confidence)
	}
	if got := estimateTuning(make([]float64, 44100), 44100); got.Confidence != 0 || got.Cents != 0 || got.A4Hz != 440 {
		t.Errorf("silence: %+v, want no offset and no confidence", got)
	}
}

func TestTunedChromaPutsADetunedChordBackOnItsNotes(t *testing.T) {
	t.Parallel()

	// C major, 45 cents flat: half way to B major as far as the semitone grid is concerned.
	notes := []int{60, 64, 67, 72, 76, 79}
	share := func(samples []float64, cents float64) float64 {
		var chord, total float64
		for _, frame := range frameChromasTuned(samples, 44100, cents) {
			for pc, v := range frame {
				total += v
				if pc == 0 || pc == 4 || pc == 7 {
					chord += v
				}
			}
		}
		return chord / total
	}
	flat := detunedNotes(notes, -45, 44100, 2)
	inTune := share(detunedNotes(notes, 0, 44100, 2), 0)
	untuned, tuned := share(flat, 0), share(flat, -45)
	// The correction gives the flat chord nearly what the same chord has in tune
	// (the rest is the width of the FFT's lobes, which no tuning can narrow).
	if tuned < inTune-0.08 || tuned-untuned < 0.15 {
		t.Errorf("share of the energy on C, E and G: %.2f in tune; flat: %.2f untuned, %.2f tuned", inTune, untuned, tuned)
	}
}

func TestKeyOfADetunedScale(t *testing.T) {
	t.Parallel()

	// The G major scale of the key test, 45 cents flat.
	var samples []float64
	for _, note := range []int{67, 69, 71, 72, 74, 76, 78, 79} {
		samples = append(samples, detunedNotes([]int{note}, -45, 44100, 1)...)
	}
	tuning := estimateTuning(samples, 44100)
	got, ok := estimateKey(samples, 44100, tuning.correction())
	if !ok || got.Tonic != "G" || got.Scale != "major" {
		t.Errorf("key = %+v (tuning %+.1f cents), want G major", got, tuning.Cents)
	}
	// Read against A = 440, the same scale comes out as another key altogether.
	if untuned, _ := estimateKey(samples, 44100, 0); untuned.Tonic == "G" && untuned.Scale == "major" {
		t.Errorf("without the correction the key is %s %s too; this material no longer shows what the correction is for", untuned.Tonic, untuned.Scale)
	}
}
