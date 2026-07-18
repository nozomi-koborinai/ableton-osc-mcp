package tools

import (
	"math"
	"math/rand"
	"testing"
)

func TestShiftBassOctave(t *testing.T) {
	t.Parallel()

	notes := []MidiNote{
		{Pitch: 36, StartTime: 0, Duration: 1, Velocity: 100},
		{Pitch: 120, StartTime: 1, Duration: 1, Velocity: 100},
	}
	got, changed, skipped := shiftBassOctave(notes, 12)
	if changed != 1 || skipped != 1 {
		t.Fatalf("changed=%d skipped=%d, want 1/1", changed, skipped)
	}
	if got[0].Pitch != 48 || got[1].Pitch != 120 {
		t.Errorf("pitches = %d, %d; want 48, 120", got[0].Pitch, got[1].Pitch)
	}
	if notes[0].Pitch != 36 {
		t.Error("source notes were mutated")
	}
}

func TestShortenBassNotes(t *testing.T) {
	t.Parallel()

	notes := []MidiNote{{Pitch: 36, StartTime: 0, Duration: 1, Velocity: 100}}
	got, changed, skipped := shortenBassNotes(notes, 1)
	if changed != 1 || skipped != 0 {
		t.Fatalf("changed=%d skipped=%d, want 1/0", changed, skipped)
	}
	if math.Abs(got[0].Duration-0.35) > 1e-9 {
		t.Errorf("duration = %v, want 0.35", got[0].Duration)
	}
}

func TestVaryBassNotesGroove(t *testing.T) {
	t.Parallel()

	notes := []MidiNote{{Pitch: 36, StartTime: 0.5, Duration: 0.5, Velocity: 100}}
	opts := bassVariationOptions{Strength: 1, Seed: 42}
	got, changed, skipped := varyBassNotes(notes, 4, "groove", opts, rand.New(rand.NewSource(opts.Seed)))
	if changed != 1 || skipped != 0 {
		t.Fatalf("changed=%d skipped=%d, want 1/0", changed, skipped)
	}
	if got[0].StartTime == notes[0].StartTime && got[0].Velocity == notes[0].Velocity {
		t.Error("groove variation did not alter timing or velocity")
	}
}

func TestCreateBassVariation(t *testing.T) {
	t.Parallel()

	seed := int64(7)
	strength := 1.0
	client := &variationStub{
		queries: map[string][]interface{}{
			"/live/clip_slot/get/has_clip": {int32(2), int32(1), false},
			"/live/clip/get/notes": {
				int32(2), int32(0),
				int32(36), float32(0), float32(1), int32(100), false,
			},
			"/live/clip/get/length": {int32(2), int32(0), float32(4)},
		},
		sendErr: map[string]error{},
	}

	got, err := createBassVariation(client, CreateBassVariationInput{
		TrackIndex:      2,
		SourceClipIndex: 0,
		TargetClipIndex: 1,
		Variation:       "octave_up",
		Strength:        &strength,
		Seed:            &seed,
		Fire:            true,
	})
	if err != nil {
		t.Fatalf("createBassVariation() error = %v", err)
	}
	if got.NotesChanged != 1 || got.NotesSkipped != 0 {
		t.Errorf("result = %#v, want one changed / no skipped", got)
	}
	if !got.Fired {
		t.Error("Fired = false, want true")
	}
	if !hasVariationSend(client.calls, "/live/clip_slot/duplicate_clip_to") {
		t.Error("expected source clip duplication")
	}
	if hasVariationSendWithClip(client.calls, "/live/clip/remove/notes", 0) {
		t.Error("source clip must not be cleared")
	}
}

func TestCreateBassVariationRejectsNoChange(t *testing.T) {
	t.Parallel()

	strength := 0.0
	client := &variationStub{
		queries: map[string][]interface{}{
			"/live/clip_slot/get/has_clip": {int32(0), int32(1), false},
			"/live/clip/get/notes": {
				int32(0), int32(0),
				int32(36), float32(0), float32(1), int32(100), false,
			},
			"/live/clip/get/length": {int32(0), int32(0), float32(4)},
		},
		sendErr: map[string]error{},
	}
	_, err := createBassVariation(client, CreateBassVariationInput{
		TrackIndex:      0,
		SourceClipIndex: 0,
		TargetClipIndex: 1,
		Variation:       "staccato",
		Strength:        &strength,
	})
	if err == nil {
		t.Fatal("createBassVariation() error = nil, want no-change error")
	}
	if hasVariationSend(client.calls, "/live/clip_slot/duplicate_clip_to") {
		t.Error("must not duplicate when no variation can be made")
	}
}
