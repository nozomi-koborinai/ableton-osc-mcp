package tools

import "testing"

func TestIndexForName(t *testing.T) {
	if idx, ok := indexForName(simplerPlaybackModes, "slicing"); !ok || idx != 2 {
		t.Errorf("slicing = (%d,%v), want (2,true)", idx, ok)
	}
	if idx, ok := indexForName(simplerPlaybackModes, "SLICING"); !ok || idx != 2 {
		t.Errorf("case-insensitive SLICING = (%d,%v), want (2,true)", idx, ok)
	}
	if idx, ok := indexForName(simplerPlaybackModes, "1"); !ok || idx != 1 {
		t.Errorf("numeric '1' = (%d,%v), want (1,true)", idx, ok)
	}
	if _, ok := indexForName(simplerPlaybackModes, "nope"); ok {
		t.Error("unknown name should not resolve")
	}
	if _, ok := indexForName(simplerPlaybackModes, "9"); ok {
		t.Error("out-of-range numeric should not resolve")
	}
	if idx, ok := indexForName(simplerBeatDivisions, "1 Bar"); !ok || idx != 8 {
		t.Errorf("'1 Bar' = (%d,%v), want (8,true)", idx, ok)
	}
}

func TestParseSimplerStateOK(t *testing.T) {
	// track, device, ok, playback=2(slicing), slicing_playback=1(poly),
	// slicing_style=1(beat), beat_division=4(1/4), num_slices=8, has_sample=1
	res := []interface{}{int32(6), int32(0), "ok", int32(2), int32(1), int32(1), int32(4), int32(8), int32(1)}
	st, err := parseSimplerState(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.PlaybackMode != 2 || st.PlaybackModeName != "slicing" {
		t.Errorf("playback = %d/%q, want 2/slicing", st.PlaybackMode, st.PlaybackModeName)
	}
	if st.SlicingPlaybackModeName != "poly" {
		t.Errorf("slicing_playback_name = %q, want poly", st.SlicingPlaybackModeName)
	}
	if st.SlicingStyle == nil || *st.SlicingStyle != 1 || st.SlicingStyleName != "beat" {
		t.Errorf("slicing_style = %v/%q, want 1/beat", st.SlicingStyle, st.SlicingStyleName)
	}
	if st.SlicingBeatDivision == nil || *st.SlicingBeatDivision != 4 || st.SlicingBeatDivisionName != "1/4" {
		t.Errorf("beat_division = %v/%q, want 4/'1/4'", st.SlicingBeatDivision, st.SlicingBeatDivisionName)
	}
	if st.NumSlices != 8 || !st.HasSample {
		t.Errorf("num_slices/has_sample = %d/%v, want 8/true", st.NumSlices, st.HasSample)
	}
}

func TestParseSimplerStateNoSample(t *testing.T) {
	// has_sample=0, slicing_style/beat_division = -1 (should be omitted → nil)
	res := []interface{}{int32(6), int32(0), "ok", int32(0), int32(0), int32(-1), int32(-1), int32(0), int32(0)}
	st, err := parseSimplerState(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.PlaybackModeName != "classic" {
		t.Errorf("playback_name = %q, want classic", st.PlaybackModeName)
	}
	if st.SlicingStyle != nil {
		t.Errorf("slicing_style = %v, want nil when no sample", st.SlicingStyle)
	}
	if st.SlicingBeatDivision != nil {
		t.Errorf("beat_division = %v, want nil when no sample", st.SlicingBeatDivision)
	}
	if st.HasSample {
		t.Error("has_sample = true, want false")
	}
}

func TestParseSimplerStateNotSimpler(t *testing.T) {
	res := []interface{}{int32(3), int32(1), "not_simpler"}
	_, err := parseSimplerState(res)
	if err == nil {
		t.Fatal("expected error for not_simpler, got nil")
	}
}

func TestParseSimplerSlices(t *testing.T) {
	// track, device, ok, sample_rate=44100, sample_length=88200, slices at 0, 22050, 44100
	res := []interface{}{int32(6), int32(0), "ok", int32(44100), int32(88200), int32(0), int32(22050), int32(44100)}
	out, err := parseSimplerSlices(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SampleRate != 44100 || out.SampleLength != 88200 {
		t.Errorf("rate/length = %d/%d, want 44100/88200", out.SampleRate, out.SampleLength)
	}
	if out.LengthSec != 2.0 {
		t.Errorf("length_sec = %v, want 2.0", out.LengthSec)
	}
	if len(out.Slices) != 3 {
		t.Fatalf("slices len = %d, want 3", len(out.Slices))
	}
	if out.Slices[1].StartSample != 22050 || out.Slices[1].StartSec != 0.5 {
		t.Errorf("slice[1] = %d/%v, want 22050/0.5", out.Slices[1].StartSample, out.Slices[1].StartSec)
	}
	if out.Slices[0].Note != 36 || out.Slices[2].Note != 38 {
		t.Errorf("notes = %d..%d, want 36..38", out.Slices[0].Note, out.Slices[2].Note)
	}
}

func TestParseSimplerSlicesNoSample(t *testing.T) {
	res := []interface{}{int32(6), int32(0), "no_sample"}
	_, err := parseSimplerSlices(res)
	if err == nil {
		t.Fatal("expected error for no_sample, got nil")
	}
}
