package tools

import (
	"errors"
	"reflect"
	"testing"
)

type sceneVariationCall struct {
	address string
	args    []interface{}
}

type sceneVariationStub struct {
	queries map[string][][]interface{}
	queryAt map[string]int
	calls   []sceneVariationCall
	sendErr map[string]error
}

func (s *sceneVariationStub) Query(address string, args ...interface{}) ([]interface{}, error) {
	s.calls = append(s.calls, sceneVariationCall{address: "Query:" + address, args: append([]interface{}{}, args...)})
	sequence, ok := s.queries[address]
	if !ok {
		return nil, errors.New("unexpected query: " + address)
	}
	index := s.queryAt[address]
	if index >= len(sequence) {
		index = len(sequence) - 1
	}
	s.queryAt[address]++
	return sequence[index], nil
}

func (s *sceneVariationStub) Send(address string, args ...interface{}) error {
	s.calls = append(s.calls, sceneVariationCall{address: "Send:" + address, args: append([]interface{}{}, args...)})
	return s.sendErr[address]
}

func TestShiftNoteVelocities(t *testing.T) {
	t.Parallel()

	notes := []MidiNote{
		{Pitch: 36, StartTime: 0, Duration: 0.25, Velocity: 120},
		{Pitch: 38, StartTime: 1, Duration: 0.25, Velocity: 5},
	}
	lifted, changed := shiftNoteVelocities(notes, 12)
	if changed != 2 || lifted[0].Velocity != 127 || lifted[1].Velocity != 17 {
		t.Errorf("lifted = %#v changed=%d", lifted, changed)
	}
	pulledBack, changed := shiftNoteVelocities(notes, -12)
	if changed != 2 || pulledBack[0].Velocity != 108 || pulledBack[1].Velocity != 1 {
		t.Errorf("pulledBack = %#v changed=%d", pulledBack, changed)
	}
	if notes[0].Velocity != 120 {
		t.Error("source notes were mutated")
	}
}

func TestCreateSceneEnergyVariation(t *testing.T) {
	t.Parallel()

	client := &sceneVariationStub{
		queries: map[string][][]interface{}{
			"/live/song/get/num_scenes": {
				{int32(3)},
				{int32(4)},
			},
			"/live/clip_slot/get/has_clip": {
				{int32(0), int32(1), true},
			},
			"/live/clip/get/notes": {
				{
					int32(0), int32(1),
					int32(36), float32(0), float32(0.25), int32(100), false,
				},
			},
		},
		queryAt: map[string]int{},
	}

	got, err := createSceneEnergyVariation(client, CreateSceneEnergyVariationInput{
		SourceSceneIndex: 1,
		TrackIndices:     []int{0},
		Variation:        "lift",
		Fire:             true,
	})
	if err != nil {
		t.Fatalf("createSceneEnergyVariation() error = %v", err)
	}
	if got.TargetSceneIndex != 2 || got.NotesChanged != 1 || !got.Fired {
		t.Errorf("result = %#v", got)
	}
	if !hasSceneVariationSend(client.calls, "/live/song/duplicate_scene") {
		t.Error("expected scene duplication")
	}
	if !hasSceneVariationSend(client.calls, "/live/scene/set/name") {
		t.Error("expected scene name")
	}
	if !hasSceneVariationSend(client.calls, "/live/scene/fire") {
		t.Error("expected target scene fire")
	}
}

func TestCreateSceneEnergyVariationSkipsEmptyTrack(t *testing.T) {
	t.Parallel()

	client := &sceneVariationStub{
		queries: map[string][][]interface{}{
			"/live/song/get/num_scenes": {
				{int32(3)},
			},
			"/live/clip_slot/get/has_clip": {
				{int32(0), int32(1), false},
			},
		},
		queryAt: map[string]int{},
	}
	_, err := createSceneEnergyVariation(client, CreateSceneEnergyVariationInput{
		SourceSceneIndex: 1,
		TrackIndices:     []int{0},
		Variation:        "lift",
	})
	if err == nil {
		t.Fatal("createSceneEnergyVariation() error = nil, want no-MIDI error")
	}
	if hasSceneVariationSend(client.calls, "/live/song/duplicate_scene") {
		t.Error("must not duplicate a scene when no MIDI notes can change")
	}
}

func TestCreateSceneEnergyVariationRemovesDuplicateOnFailure(t *testing.T) {
	t.Parallel()

	client := &sceneVariationStub{
		queries: map[string][][]interface{}{
			"/live/song/get/num_scenes": {
				{int32(2)},
				{int32(3)},
			},
			"/live/clip_slot/get/has_clip": {
				{int32(0), int32(0), true},
			},
			"/live/clip/get/notes": {
				{
					int32(0), int32(0),
					int32(36), float32(0), float32(0.25), int32(100), false,
				},
			},
		},
		queryAt: map[string]int{},
		sendErr: map[string]error{
			"/live/scene/set/name": errors.New("set name failed"),
		},
	}
	_, err := createSceneEnergyVariation(client, CreateSceneEnergyVariationInput{
		SourceSceneIndex: 0,
		TrackIndices:     []int{0},
		Variation:        "lift",
	})
	if err == nil {
		t.Fatal("createSceneEnergyVariation() error = nil, want error")
	}
	if !hasSceneVariationSend(client.calls, "/live/song/delete_scene") {
		t.Error("expected duplicated scene cleanup")
	}
}

func TestValidateSceneVariationTracks(t *testing.T) {
	t.Parallel()

	got, err := validateSceneVariationTracks([]int{2, 0})
	if err != nil || !reflect.DeepEqual(got, []int{2, 0}) {
		t.Errorf("validateSceneVariationTracks() = %v, %v", got, err)
	}
	_, err = validateSceneVariationTracks([]int{0, 0})
	if err == nil {
		t.Fatal("duplicate tracks should fail")
	}
}

func hasSceneVariationSend(calls []sceneVariationCall, address string) bool {
	for _, call := range calls {
		if call.address == "Send:"+address {
			return true
		}
	}
	return false
}
