package audioanalyze

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestValidateAudioURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"https", "https://youtu.be/abc123", false},
		{"http", "http://example.com/track.mp3", false},
		{"empty", "   ", true},
		{"scheme", "ftp://example.com/a.wav", true},
		{"relative", "example.com/a.wav", true},
		{"nohost", "https://", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateAudioURL(tc.in)
			if tc.wantErr && err == nil {
				t.Fatalf("validateAudioURL(%q) expected error", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateAudioURL(%q) unexpected error: %v", tc.in, err)
			}
		})
	}
}

func TestAnalyzeURLRejectsBadURL(t *testing.T) {
	t.Parallel()

	if _, err := AnalyzeURL(context.Background(), "not-a-url", 0); err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestFfmpegArgsStreamMono(t *testing.T) {
	t.Parallel()

	args := ffmpegArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-ac 2") || !strings.Contains(joined, "-ar 44100") {
		t.Errorf("ffmpeg args missing stereo/44.1k output: %v", args)
	}
	if !strings.Contains(joined, "pipe:1") {
		t.Errorf("ffmpeg args must stream to stdout, not a file: %v", args)
	}
}

func TestYtDlpArgsStreamToStdout(t *testing.T) {
	t.Parallel()

	args := ytDlpArgs("https://example.com/a")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-o -") {
		t.Errorf("yt-dlp must write to stdout (-o -), not disk: %v", args)
	}
	if args[len(args)-1] != "https://example.com/a" {
		t.Errorf("url must be the final arg: %v", args)
	}
}

func TestAnalyzeWAVStreamReader(t *testing.T) {
	t.Parallel()

	buf := clickWAVBytes(t, 44100, 120, 4)
	got, err := analyzeWAVStream(bytes.NewReader(buf), 120)
	if err != nil {
		t.Fatalf("analyzeWAVStream() error = %v", err)
	}
	if got.SampleRate != 44100 || got.Channels != 1 {
		t.Errorf("format = %#v", got)
	}
	if math.Abs(got.EstimatedBPM-120) > 3 {
		t.Errorf("estimated_bpm = %v, want ~120", got.EstimatedBPM)
	}
	if got.Path != "" || got.Note != "" {
		t.Errorf("core analysis should not set Path/Note: %#v", got)
	}
}

// clickWAVBytes builds an in-memory mono 16-bit click track for reader tests.
func clickWAVBytes(t *testing.T, sampleRate, bpm, seconds int) []byte {
	t.Helper()
	samples := sampleRate * seconds
	pcm := make([]int16, samples)
	interval := int(float64(sampleRate) * 60 / float64(bpm))
	for i := 0; i < samples; i += interval {
		end := i + sampleRate/200
		if end > samples {
			end = samples
		}
		for j := i; j < end; j++ {
			pcm[j] = 20000
		}
	}

	var b bytes.Buffer
	dataBytes := len(pcm) * 2
	write := func(v interface{}) {
		if err := binary.Write(&b, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	b.WriteString("RIFF")
	write(uint32(36 + dataBytes))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	write(uint32(16))
	write(uint16(1))
	write(uint16(1))
	write(uint32(sampleRate))
	write(uint32(sampleRate * 2))
	write(uint16(2))
	write(uint16(16))
	b.WriteString("data")
	write(uint32(dataBytes))
	for _, s := range pcm {
		write(s)
	}
	return b.Bytes()
}
