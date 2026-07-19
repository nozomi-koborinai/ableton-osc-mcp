package audioanalyze

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRejectsURL(t *testing.T) {
	t.Parallel()

	_, err := AnalyzeFile("https://example.com/a.wav", 0)
	if err == nil {
		t.Fatal("expected URL rejection")
	}
	_, err = AnalyzeFile("relative/a.wav", 0)
	if err == nil {
		t.Fatal("expected relative path rejection")
	}
}

func TestAnalyzeClickTrackBPM(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "clicks_120.wav")
	writeClickWAV(t, path, 44100, 120, 4)
	got, err := AnalyzeFile(path, 120)
	if err != nil {
		t.Fatalf("AnalyzeFile() error = %v", err)
	}
	if got.SampleRate != 44100 || got.Channels != 1 {
		t.Errorf("format = %#v", got)
	}
	if math.Abs(got.EstimatedBPM-120) > 3 {
		t.Errorf("estimated_bpm = %v, want ~120", got.EstimatedBPM)
	}
	if got.DurationSec < 3.5 || got.OnsetCount < 4 {
		t.Errorf("duration/onsets = %#v", got)
	}
	if got.LengthBarsAtBPM <= 0 {
		t.Errorf("length_bars_at_project_tempo = %v", got.LengthBarsAtBPM)
	}
	if len(got.BPMAlternatives) < 1 || got.BPMAlternatives[0].Label != "primary" {
		t.Errorf("bpm_alternatives = %+v", got.BPMAlternatives)
	}
	if got.RhythmDensity <= 0 {
		t.Errorf("rhythm_density = %v", got.RhythmDensity)
	}
	if len(got.RMSPerBeat) < 4 {
		t.Errorf("rms_per_beat len = %d, want >= 4", len(got.RMSPerBeat))
	}
	if got.BandBalance == nil {
		t.Error("band_balance missing")
	}
	if len(got.MatchAxes) != 3 {
		t.Errorf("match_axes = %+v", got.MatchAxes)
	}
}

func writeClickWAV(t *testing.T, path string, sampleRate, bpm, seconds int) {
	t.Helper()
	samples := sampleRate * seconds
	pcm := make([]int16, samples)
	interval := int(float64(sampleRate) * 60 / float64(bpm))
	for i := 0; i < samples; i += interval {
		end := i + sampleRate/200 // 5ms click
		if end > samples {
			end = samples
		}
		for j := i; j < end; j++ {
			pcm[j] = 20000
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	dataBytes := len(pcm) * 2
	write := func(v interface{}) {
		t.Helper()
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = f.Write([]byte("RIFF"))
	write(uint32(36 + dataBytes))
	_, _ = f.Write([]byte("WAVE"))
	_, _ = f.Write([]byte("fmt "))
	write(uint32(16))
	write(uint16(1))
	write(uint16(1))
	write(uint32(sampleRate))
	write(uint32(sampleRate * 2))
	write(uint16(2))
	write(uint16(16))
	_, _ = f.Write([]byte("data"))
	write(uint32(dataBytes))
	for _, s := range pcm {
		write(s)
	}
}

func TestDetectOnsetsClickTrack(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "clicks_120.wav")
	writeClickWAV(t, path, 44100, 120, 4)
	got, err := AnalyzeFile(path, 120)
	if err != nil {
		t.Fatalf("AnalyzeFile() error = %v", err)
	}
	if len(got.Onsets) != got.OnsetCount {
		t.Fatalf("len(onsets)=%d != onset_count=%d", len(got.Onsets), got.OnsetCount)
	}
	if len(got.Onsets) < 4 {
		t.Fatalf("onsets = %d, want >= 4", len(got.Onsets))
	}
	for i, o := range got.Onsets {
		if o.Strength < 0 || o.Strength > 1 {
			t.Errorf("onset %d strength out of range: %v", i, o.Strength)
		}
		if o.Sec < 0 || o.Beat < 0 {
			t.Errorf("onset %d has negative sec/beat: %#v", i, o)
		}
	}
	// The first click sits near the start of the file.
	if got.Onsets[0].Beat > 0.6 {
		t.Errorf("first onset beat = %v, want near 0", got.Onsets[0].Beat)
	}
	// At 120 BPM clicks are 0.5s = 1 beat apart; consecutive onsets should be
	// roughly a beat apart.
	if len(got.Onsets) >= 2 {
		gap := got.Onsets[1].Beat - got.Onsets[0].Beat
		if gap < 0.5 || gap > 1.5 {
			t.Errorf("beat gap = %v, want ~1", gap)
		}
	}
}
