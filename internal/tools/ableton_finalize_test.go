package tools

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestFinalizeAudioUsesTheDeliveryDefaults(t *testing.T) {
	t.Parallel()

	source := filepath.Join(t.TempDir(), "beat.wav")
	writeToneWAV(t, source, 440, 2) // mono, -6.2 dBFS, and it runs right into the end of the file

	got, err := finalizeAudio(FinalizeAudioInput{Path: source})
	if err != nil {
		t.Fatalf("finalizeAudio() error = %v", err)
	}
	if want := filepath.Join(filepath.Dir(source), "beat_44k1_24b.wav"); got.OutputPath != want {
		t.Errorf("output_path = %s, want %s", got.OutputPath, want)
	}
	if got.SampleRate != 44100 || got.BitDepth != 24 || got.Channels != 1 {
		t.Errorf("format = %d / %d / %d, want 44100 / 24 / 1", got.SampleRate, got.BitDepth, got.Channels)
	}
	if math.Abs(got.GainDB-5.2) > 0.1 || math.Abs(got.Delivered.TruePeakDBTP-(-1)) > 0.05 {
		t.Errorf("gain = %v dB, delivered true peak = %v; want about +5.2 dB up to -1 dBTP", got.GainDB, got.Delivered.TruePeakDBTP)
	}
	if got.GainDB != math.Round(got.GainDB*100)/100 || got.Source.LUFSIntegrated != math.Round(got.Source.LUFSIntegrated*100)/100 {
		t.Errorf("gain = %v, source LUFS = %v; numbers should come rounded to two decimals", got.GainDB, got.Source.LUFSIntegrated)
	}
	// The tone never dies away, so this is not a file to hand in.
	if got.OK || len(got.Issues) != 1 || got.Issues[0].Code != "ending_cut_off" || !got.Issues[0].Blocking {
		t.Errorf("ok = %v, issues = %+v; want ending_cut_off holding it back", got.OK, got.Issues)
	}
	if _, err := os.Stat(got.OutputPath); err != nil {
		t.Errorf("the delivery was not written: %v", err)
	}
}

func TestFinalizeAudioPassesOptionsOn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "beat.wav")
	writeToneWAV(t, source, 440, 2)
	no, zero, ceiling := false, 0.0, -3.0
	got, err := finalizeAudio(FinalizeAudioInput{
		Path: source, OutputPath: filepath.Join(dir, "handed in.wav"),
		SampleRate: 48000, BitDepth: 16, CeilingDBTP: &ceiling, MaxGainDB: &zero, TrimTail: &no, FadeOutSec: &zero,
	})
	if err != nil {
		t.Fatalf("finalizeAudio() error = %v", err)
	}
	if got.SampleRate != 48000 || got.BitDepth != 16 || filepath.Base(got.OutputPath) != "handed in.wav" {
		t.Errorf("got %d Hz / %d bit at %s", got.SampleRate, got.BitDepth, got.OutputPath)
	}
	// No lifting allowed: the level stays, and the tool says why it is under the ceiling.
	if got.GainDB != 0 || math.Abs(got.DurationSec-2) > 0.01 {
		t.Errorf("gain = %v, duration = %v; want 0 dB and the full two seconds", got.GainDB, got.DurationSec)
	}
	limited := false
	for _, issue := range got.Issues {
		limited = limited || (issue.Code == "gain_limited" && !issue.Blocking)
	}
	if !limited {
		t.Errorf("issues = %+v, want gain_limited as a note", got.Issues)
	}
}

func TestFinalizeAudioNeverReplacesADeliveryUnasked(t *testing.T) {
	t.Parallel()

	source := filepath.Join(t.TempDir(), "beat.wav")
	writeToneWAV(t, source, 440, 1)
	if _, err := finalizeAudio(FinalizeAudioInput{Path: source}); err != nil {
		t.Fatal(err)
	}
	_, err := finalizeAudio(FinalizeAudioInput{Path: source})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "output_exists" {
		t.Fatalf("second run: error = %v, want output_exists", err)
	}
	if _, err := finalizeAudio(FinalizeAudioInput{Path: source, Overwrite: true}); err != nil {
		t.Errorf("with overwrite: error = %v", err)
	}
}

func TestFinalizeAudioRejectsWhatItCannotRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	notAudio := filepath.Join(dir, "notes.wav")
	if err := os.WriteFile(notAudio, []byte("this is not audio at all, only text"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := finalizeAudio(FinalizeAudioInput{Path: notAudio})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "unsupported_audio_format" {
		t.Errorf("text file: error = %v, want unsupported_audio_format", err)
	}
	for name, input := range map[string]FinalizeAudioInput{
		"relative path": {Path: "beat.wav"},
		"a URL":         {Path: "https://example.com/beat.wav"},
		"an mp3":        {Path: filepath.Join(dir, "beat.mp3")},
		"odd rate":      {Path: notAudio, SampleRate: 22050},
	} {
		if _, err := finalizeAudio(input); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
