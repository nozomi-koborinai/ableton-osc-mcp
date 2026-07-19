package tools

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSlicePresetPathSanitize(t *testing.T) {
	p, err := slicePresetPath("my chop/../evil*name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	base := filepath.Base(p)
	if base != "my chop_.._evil_name.json" {
		t.Errorf("sanitized base = %q", base)
	}
	if _, err := slicePresetPath("   "); err == nil {
		t.Error("empty name should error")
	}
}

func TestSlicePresetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chop.json")
	in := SlicePreset{
		Version:             slicePresetVersion,
		Name:                "chop",
		SampleRate:          44100,
		SampleLength:        88200,
		PlaybackMode:        2,
		SlicingStyle:        3,
		SlicingBeatDivision: -1,
		Slices:              []int{0, 22050, 44100},
		SavedAt:             time.Now().UTC().Truncate(time.Second),
	}
	if err := writeSlicePreset(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readSlicePreset(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SampleLength != in.SampleLength || len(got.Slices) != 3 || got.Slices[2] != 44100 {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if got.PlaybackMode != 2 || got.SlicingStyle != 3 {
		t.Errorf("modes mismatch: playback=%d style=%d", got.PlaybackMode, got.SlicingStyle)
	}
}

func TestReadSlicePresetMissing(t *testing.T) {
	_, err := readSlicePreset(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected error for missing preset")
	}
}

func TestReadSlicePresetBadVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.json")
	if err := writeSlicePreset(path, SlicePreset{Version: 999, Slices: []int{0}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := readSlicePreset(path); err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestSliceStartSamples(t *testing.T) {
	slices := []SimplerSlice{{StartSample: 0}, {StartSample: 100}, {StartSample: 250}}
	got := sliceStartSamples(slices)
	if len(got) != 3 || got[1] != 100 || got[2] != 250 {
		t.Errorf("sliceStartSamples = %v", got)
	}
}

func TestDerefIntOr(t *testing.T) {
	v := 7
	if got := derefIntOr(&v, 3); got != 7 {
		t.Errorf("derefIntOr(&7,3) = %d, want 7", got)
	}
	if got := derefIntOr(nil, 3); got != 3 {
		t.Errorf("derefIntOr(nil,3) = %d, want 3", got)
	}
}
