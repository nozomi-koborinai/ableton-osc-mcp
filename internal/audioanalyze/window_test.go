package audioanalyze

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stereoToneAIFF builds a 16-bit big-endian stereo AIFF holding the same tone
// on both channels.
func stereoToneAIFF(freq, amp float64, sampleRate, seconds int) []byte {
	tone := testSine(freq, amp, 0, sampleRate, sampleRate*seconds)
	pcm := make([]byte, 0, len(tone)*4)
	for _, v := range tone {
		s := uint16(int16(math.Round(v * 32767)))
		pcm = binary.BigEndian.AppendUint16(pcm, s) // L
		pcm = binary.BigEndian.AppendUint16(pcm, s) // R
	}
	return buildAIFF("AIFF", "", 2, 16, ext48000, pcm)
}

func TestAnalyzeStreamWindowsTheAudio(t *testing.T) {
	t.Parallel()

	wav := clickWAVBytes(t, 44100, 120, 4)
	got, err := analyzeStream(bytes.NewReader(wav), Options{StartSec: 1, EndSec: 3})
	if err != nil {
		t.Fatalf("analyzeStream() error = %v", err)
	}
	if math.Abs(got.DurationSec-2) > 0.01 {
		t.Errorf("duration_sec = %v, want 2 (the window, not the file)", got.DurationSec)
	}
	if got.RangeStartSec != 1 || got.RangeEndSec != 3 {
		t.Errorf("range = [%v, %v], want [1, 3]", got.RangeStartSec, got.RangeEndSec)
	}
	if got.MixProfile == nil || math.Abs(got.MixProfile.AnalyzedSec-2) > 0.01 {
		t.Errorf("mix_profile.analyzed_sec = %+v, want 2", got.MixProfile)
	}
}

func TestAnalyzeStreamWithoutWindowReportsNoRange(t *testing.T) {
	t.Parallel()

	got, err := analyzeStream(bytes.NewReader(clickWAVBytes(t, 44100, 120, 2)), Options{})
	if err != nil {
		t.Fatalf("analyzeStream() error = %v", err)
	}
	if got.RangeStartSec != 0 || got.RangeEndSec != 0 {
		t.Errorf("range = [%v, %v], want zero values when no window was asked for", got.RangeStartSec, got.RangeEndSec)
	}
	if math.Abs(got.DurationSec-2) > 0.01 {
		t.Errorf("duration_sec = %v, want 2", got.DurationSec)
	}
}

func TestAnalyzeStreamRejectsBadWindows(t *testing.T) {
	t.Parallel()

	wav := clickWAVBytes(t, 44100, 120, 4)
	cases := []struct {
		name string
		opts Options
		want string
	}{
		{"end before start", Options{StartSec: 3, EndSec: 2}, "end_sec must be greater than start_sec"},
		{"shorter than a second", Options{StartSec: 1, EndSec: 1.5}, "at least 1 second"},
		{"start past the end", Options{StartSec: 10}, "past the end"},
		{"negative start", Options{StartSec: -1}, "must be >= 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := analyzeStream(bytes.NewReader(wav), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestAnalyzeFileMeasuresAnAIFFBounce(t *testing.T) {
	t.Parallel()

	// What a bounce recorded inside Live looks like: stereo AIFF at 48 kHz.
	path := filepath.Join(t.TempDir(), "Bounce 0001.aif")
	if err := os.WriteFile(path, stereoToneAIFF(1000, math.Pow(10, -23.0/20), 48000, 3), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := AnalyzeFile(path, Options{})
	if err != nil {
		t.Fatalf("AnalyzeFile() error = %v", err)
	}
	if got.Format != "aiff" || got.SampleRate != 48000 || got.Channels != 2 {
		t.Errorf("format/rate/channels = %s/%d/%d, want aiff/48000/2", got.Format, got.SampleRate, got.Channels)
	}
	if got.MixProfile == nil {
		t.Fatal("mix_profile missing")
	}
	if math.Abs(got.MixProfile.LUFSIntegrated-(-23.0)) > 0.15 {
		t.Errorf("LUFS = %.2f, want -23.0 ±0.15", got.MixProfile.LUFSIntegrated)
	}
	if len(got.MixProfile.Bands) != 9 {
		t.Errorf("bands = %d, want 9", len(got.MixProfile.Bands))
	}
}
