package tools

import (
	"errors"
	"math"
	"math/rand"
	"testing"
)

type variationCall struct {
	address string
	args    []interface{}
}

type variationStub struct {
	queries map[string][]interface{}
	calls   []variationCall
	sendErr map[string]error
}

func (s *variationStub) Query(address string, args ...interface{}) ([]interface{}, error) {
	s.calls = append(s.calls, variationCall{address: "Query:" + address, args: append([]interface{}{}, args...)})
	res, ok := s.queries[address]
	if !ok {
		return nil, errors.New("unexpected query: " + address)
	}
	return res, nil
}

func (s *variationStub) Send(address string, args ...interface{}) error {
	s.calls = append(s.calls, variationCall{address: "Send:" + address, args: append([]interface{}{}, args...)})
	return s.sendErr[address]
}

func TestDensityNotes(t *testing.T) {
	t.Parallel()

	seed := int64(1)
	rng := newVariationRNG(seed)
	notes := []MidiNote{{Pitch: 42, StartTime: 0.25, Duration: 0.1, Velocity: 80}}
	got := densityNotes(notes, 2, 42, 1, rng)

	// 0.25 already exists; 0.75, 1.25, 1.75 are added.
	if len(got) != 3 {
		t.Fatalf("densityNotes() len = %d, want 3", len(got))
	}
	for _, note := range got {
		if note.Pitch != 42 || note.StartTime >= 2 {
			t.Errorf("unexpected density note: %#v", note)
		}
	}
}

func TestFillNotes(t *testing.T) {
	t.Parallel()

	got := fillNotes(nil, 4, 38, 1)
	if len(got) != 3 {
		t.Fatalf("fillNotes() len = %d, want 3", len(got))
	}
	if math.Abs(got[0].StartTime-3.25) > 1e-9 || math.Abs(got[2].StartTime-3.75) > 1e-9 {
		t.Errorf("fill notes = %#v, want final three 16ths", got)
	}

	existing := []MidiNote{{Pitch: 38, StartTime: 3.5, Duration: 0.25, Velocity: 100}}
	got = fillNotes(existing, 4, 38, 1)
	if len(got) != 2 {
		t.Errorf("fillNotes() with existing snare len = %d, want 2", len(got))
	}
}

func TestCreateDrumVariationGroovePreservesSource(t *testing.T) {
	t.Parallel()

	strength := 0.5
	seed := int64(42)
	client := &variationStub{
		queries: map[string][]interface{}{
			"/live/clip_slot/get/has_clip": {int32(1), int32(2), false},
			"/live/clip/get/notes": {
				int32(1), int32(0),
				int32(36), float32(0), float32(0.25), int32(100), false,
				int32(42), float32(0.5), float32(0.1), int32(80), false,
			},
			"/live/clip/get/length": {int32(1), int32(0), float32(4)},
		},
		sendErr: map[string]error{},
	}

	got, err := createDrumVariation(client, CreateDrumVariationInput{
		TrackIndex:      1,
		SourceClipIndex: 0,
		TargetClipIndex: 2,
		Variation:       "groove",
		Strength:        &strength,
		Seed:            &seed,
	})
	if err != nil {
		t.Fatalf("createDrumVariation() error = %v", err)
	}
	if got.NotesChanged != 2 || got.NotesAdded != 0 {
		t.Errorf("result = %#v, want two changed and zero added", got)
	}
	if hasVariationSend(client.calls, "/live/clip_slot/duplicate_clip_to") == false {
		t.Error("expected clip duplication")
	}
	if hasVariationSend(client.calls, "/live/clip/remove/notes") == false {
		t.Error("expected only the duplicated clip to be cleared")
	}
	if hasVariationSendWithClip(client.calls, "/live/clip/remove/notes", 0) {
		t.Error("source clip must not be cleared")
	}
}

func TestCreateDrumVariationDensityFiresTarget(t *testing.T) {
	t.Parallel()

	seed := int64(1)
	strength := 1.0
	client := &variationStub{
		queries: map[string][]interface{}{
			"/live/clip_slot/get/has_clip": {int32(0), int32(1), false},
			"/live/clip/get/notes": {
				int32(0), int32(0),
				int32(36), float32(0), float32(0.25), int32(100), false,
			},
			"/live/clip/get/length": {int32(0), int32(0), float32(2)},
		},
		sendErr: map[string]error{},
	}

	got, err := createDrumVariation(client, CreateDrumVariationInput{
		TrackIndex:      0,
		SourceClipIndex: 0,
		TargetClipIndex: 1,
		Variation:       "density",
		Strength:        &strength,
		Seed:            &seed,
		Fire:            true,
	})
	if err != nil {
		t.Fatalf("createDrumVariation() error = %v", err)
	}
	if got.NotesAdded == 0 {
		t.Error("density variation should add notes")
	}
	if !got.Fired || !hasVariationSend(client.calls, "/live/clip_slot/fire") {
		t.Error("expected target clip to fire")
	}
}

func TestCreateDrumVariationRejectsOccupiedTarget(t *testing.T) {
	t.Parallel()

	client := &variationStub{
		queries: map[string][]interface{}{
			"/live/clip_slot/get/has_clip": {int32(0), int32(1), true},
		},
		sendErr: map[string]error{},
	}
	_, err := createDrumVariation(client, CreateDrumVariationInput{
		TrackIndex:      0,
		SourceClipIndex: 0,
		TargetClipIndex: 1,
		Variation:       "fill",
	})
	if err == nil {
		t.Fatal("createDrumVariation() error = nil, want occupied-target error")
	}
	if hasVariationSend(client.calls, "/live/clip_slot/duplicate_clip_to") {
		t.Error("should not duplicate onto an occupied target")
	}
}

func TestCreateDrumVariationRejectsSameSlot(t *testing.T) {
	t.Parallel()

	_, err := createDrumVariation(&variationStub{}, CreateDrumVariationInput{
		TrackIndex:      0,
		SourceClipIndex: 1,
		TargetClipIndex: 1,
		Variation:       "groove",
	})
	if err == nil {
		t.Fatal("createDrumVariation() error = nil, want same-slot error")
	}
}

func newVariationRNG(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

func hasVariationSend(calls []variationCall, address string) bool {
	for _, call := range calls {
		if call.address == "Send:"+address {
			return true
		}
	}
	return false
}

func hasVariationSendWithClip(calls []variationCall, address string, clipIndex int32) bool {
	for _, call := range calls {
		if call.address != "Send:"+address || len(call.args) < 2 {
			continue
		}
		if call.args[1] == clipIndex {
			return true
		}
	}
	return false
}
