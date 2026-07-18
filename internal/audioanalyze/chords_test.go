package audioanalyze

import (
	"strings"
	"testing"
)

func TestClassifyChordCMajor(t *testing.T) {
	t.Parallel()

	// C, E, G -> C major.
	samples := tones(44100, 2, 261.63, 329.63, 392.0)
	chroma := chromagram(samples, 44100)
	chord, conf := classifyChord(chroma)
	if chord != "C" {
		t.Fatalf("chord = %s (conf %.2f), want C", chord, conf)
	}
}

func TestClassifyChordAMinor(t *testing.T) {
	t.Parallel()

	// A, C, E -> A minor.
	samples := tones(44100, 2, 440.0, 523.25, 659.25)
	chroma := chromagram(samples, 44100)
	chord, conf := classifyChord(chroma)
	if chord != "Am" {
		t.Fatalf("chord = %s (conf %.2f), want Am", chord, conf)
	}
}

func TestEstimateChordsProgression(t *testing.T) {
	t.Parallel()

	sr := 44100
	// 2s C major then 2s G major.
	var samples []float64
	samples = append(samples, tones(sr, 2, 261.63, 329.63, 392.0)...)
	samples = append(samples, tones(sr, 2, 392.0, 493.88, 587.33)...)

	segs, summary, ok := estimateChords(samples, sr)
	if !ok || len(segs) == 0 {
		t.Fatal("estimateChords returned no result")
	}
	if !strings.Contains(summary, "C") || !strings.Contains(summary, "G") {
		t.Fatalf("summary = %q, want both C and G", summary)
	}
	// First detected chord should be C, and the progression should move to G.
	if segs[0].Chord != "C" {
		t.Errorf("first chord = %s, want C", segs[0].Chord)
	}
	sawG := false
	for _, s := range segs {
		if s.Chord == "G" {
			sawG = true
		}
	}
	if !sawG {
		t.Errorf("progression never reached G: %+v", segs)
	}
}

func TestEstimateChordsSilenceIsNoChord(t *testing.T) {
	t.Parallel()

	samples := make([]float64, 44100*2)
	segs, summary, ok := estimateChords(samples, 44100)
	if !ok {
		t.Fatal("expected ok for long-enough input")
	}
	if summary != "" {
		t.Errorf("silent audio summary = %q, want empty", summary)
	}
	for _, s := range segs {
		if s.Chord != "N.C." {
			t.Errorf("silent segment chord = %s, want N.C.", s.Chord)
		}
	}
}
