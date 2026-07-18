package tools

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeLocalAudioRejectsURL(t *testing.T) {
	t.Parallel()

	_, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: "https://youtube.com/watch?v=abc"})
	if err == nil {
		t.Fatal("expected URL rejection")
	}
}

func TestAnalyzeLocalAudioWAV(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tone.wav")
	writeSilentWAV(t, path, 44100, 1)
	tempo := 128.0
	got, err := analyzeLocalAudio(AnalyzeLocalAudioInput{Path: path, ProjectTempo: &tempo})
	if err != nil {
		t.Fatalf("analyzeLocalAudio() error = %v", err)
	}
	if got.SampleRate != 44100 || got.NextStep == "" {
		t.Errorf("got = %#v", got)
	}
}

func writeSilentWAV(t *testing.T, path string, sampleRate, seconds int) {
	t.Helper()
	samples := sampleRate * seconds
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	dataBytes := samples * 2
	write := func(v interface{}) {
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
	zeros := make([]byte, dataBytes)
	if _, err := f.Write(zeros); err != nil {
		t.Fatal(err)
	}
}
