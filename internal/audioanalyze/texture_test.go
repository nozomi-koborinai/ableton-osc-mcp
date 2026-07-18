package audioanalyze

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestDecodeChannelsStereo(t *testing.T) {
	t.Parallel()

	// Interleaved 16-bit stereo: L=+half, R=-half for two frames.
	var data []byte
	var lv int16 = 16384
	var rv int16 = -16384
	for i := 0; i < 2; i++ {
		data = binary.LittleEndian.AppendUint16(data, uint16(lv)) // L
		data = binary.LittleEndian.AppendUint16(data, uint16(rv)) // R
	}
	chans, err := decodeChannels(data, 2, 16, 1)
	if err != nil {
		t.Fatalf("decodeChannels() error = %v", err)
	}
	if len(chans) != 2 || len(chans[0]) != 2 {
		t.Fatalf("shape = %d x %d, want 2 x 2", len(chans), len(chans[0]))
	}
	if chans[0][0] <= 0 || chans[1][0] >= 0 {
		t.Errorf("channel split wrong: L=%v R=%v", chans[0][0], chans[1][0])
	}
	if mono := downmix(chans); math.Abs(mono[0]) > 1e-9 {
		t.Errorf("downmix of +/- should cancel to ~0, got %v", mono[0])
	}
}

func TestSpectralCentroidBrightness(t *testing.T) {
	t.Parallel()

	sr := 44100
	low := spectralCentroid(tones(sr, 2, 220.0), sr)
	high := spectralCentroid(tones(sr, 2, 4000.0), sr)
	if !(high > low) {
		t.Fatalf("expected higher tone to be brighter: low=%.1f high=%.1f", low, high)
	}
	// A 4kHz tone's centroid should sit near its fundamental.
	if math.Abs(high-4000) > 800 {
		t.Errorf("centroid for 4kHz tone = %.1f, want ~4000", high)
	}
}

func TestCrestFactorDB(t *testing.T) {
	t.Parallel()

	// A full-scale sine has a crest factor of ~3.01 dB (peak/rms = sqrt(2)).
	sine := tones(44100, 1, 1000.0)
	cf := crestFactorDB(sine)
	if math.Abs(cf-3.01) > 0.5 {
		t.Errorf("sine crest factor = %.2f dB, want ~3.01", cf)
	}
}

func TestStereoWidth(t *testing.T) {
	t.Parallel()

	n := 44100
	left := make([]float64, n)
	right := make([]float64, n)
	for i := range left {
		v := math.Sin(2 * math.Pi * 440 * float64(i) / 44100)
		left[i] = v
		right[i] = v
	}
	// Identical channels -> width 0.
	if w := stereoWidth(left, right, 2); w != 0 {
		t.Errorf("identical channels width = %v, want 0", w)
	}
	// Fully out-of-phase channels -> maximum side energy.
	for i := range right {
		right[i] = -left[i]
	}
	if w := stereoWidth(left, right, 2); w < 1 {
		t.Errorf("anti-phase channels width = %v, want large", w)
	}
	// Mono source (channels < 2) -> width 0.
	if w := stereoWidth(left, right, 1); w != 0 {
		t.Errorf("mono width = %v, want 0", w)
	}
}
