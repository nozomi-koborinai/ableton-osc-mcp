package tools

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type recipeCall struct {
	method  string
	address string
	args    []interface{}
}

type recipeClientStub struct {
	calls   []recipeCall
	queries map[string][]interface{}
	sendErr map[string]error
}

func (s *recipeClientStub) Send(address string, args ...interface{}) error {
	s.calls = append(s.calls, recipeCall{method: "Send", address: address, args: append([]interface{}{}, args...)})
	if err, ok := s.sendErr[address]; ok {
		return err
	}
	return nil
}

func (s *recipeClientStub) Query(address string, args ...interface{}) ([]interface{}, error) {
	s.calls = append(s.calls, recipeCall{method: "Query", address: address, args: append([]interface{}{}, args...)})
	res, ok := s.queries[address]
	if !ok {
		return nil, errors.New("unexpected query: " + address)
	}
	return res, nil
}

func (s *recipeClientStub) QueryWithTimeout(_ time.Duration, address string, args ...interface{}) ([]interface{}, error) {
	s.calls = append(s.calls, recipeCall{method: "QueryWithTimeout", address: address, args: append([]interface{}{}, args...)})
	res, ok := s.queries[address]
	if !ok {
		return nil, errors.New("unexpected query: " + address)
	}
	return res, nil
}

func TestBuildDrumPattern(t *testing.T) {
	t.Parallel()

	t.Run("basic_backbeat_one_bar", func(t *testing.T) {
		t.Parallel()
		notes, err := buildDrumPattern("basic_backbeat", 4)
		if err != nil {
			t.Fatalf("buildDrumPattern() error = %v", err)
		}
		if countPitch(notes, drumPitchKick) != 2 {
			t.Errorf("kick count = %d, want 2", countPitch(notes, drumPitchKick))
		}
		if countPitch(notes, drumPitchSnare) != 2 {
			t.Errorf("snare count = %d, want 2", countPitch(notes, drumPitchSnare))
		}
		if countPitch(notes, drumPitchHat) != 8 {
			t.Errorf("hat count = %d, want 8", countPitch(notes, drumPitchHat))
		}
	})

	t.Run("four_on_floor_two_bars", func(t *testing.T) {
		t.Parallel()
		notes, err := buildDrumPattern("four_on_floor", 8)
		if err != nil {
			t.Fatalf("buildDrumPattern() error = %v", err)
		}
		if countPitch(notes, drumPitchKick) != 8 {
			t.Errorf("kick count = %d, want 8", countPitch(notes, drumPitchKick))
		}
	})

	t.Run("kick_only", func(t *testing.T) {
		t.Parallel()
		notes, err := buildDrumPattern("kick_only", 4)
		if err != nil {
			t.Fatalf("buildDrumPattern() error = %v", err)
		}
		if countPitch(notes, drumPitchKick) != 4 {
			t.Errorf("kick count = %d, want 4", countPitch(notes, drumPitchKick))
		}
		if countPitch(notes, drumPitchSnare) != 0 || countPitch(notes, drumPitchHat) != 0 {
			t.Errorf("kick_only should not include snare/hat")
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		t.Parallel()
		_, err := buildDrumPattern("latin", 16)
		if err == nil {
			t.Fatal("buildDrumPattern() error = nil, want error")
		}
	})
}

func TestSetupDrumTrackByKitName(t *testing.T) {
	t.Parallel()

	client := &recipeClientStub{
		queries: map[string][]interface{}{
			"/live/song/get/num_tracks":     {int32(3)},
			"/live/track/load/browser_item": {int32(2), "loaded", "Street Kit", int32(0), int32(1)},
			"/live/clip_slot/get/has_clip":  {int32(2), int32(0), int32(1)},
		},
	}

	got, err := setupDrumTrack(client, SetupDrumTrackInput{
		KitName: "Street Kit",
		Pattern: "kick_only",
		Fire:    true,
	})
	if err != nil {
		t.Fatalf("setupDrumTrack() error = %v", err)
	}
	if got.TrackIndex != 2 {
		t.Errorf("TrackIndex = %d, want 2", got.TrackIndex)
	}
	if got.TrackName != "Street Kit" {
		t.Errorf("TrackName = %q, want Street Kit", got.TrackName)
	}
	if got.Loaded != "Street Kit" {
		t.Errorf("Loaded = %q, want Street Kit", got.Loaded)
	}
	if got.NotesAdded != 16 {
		t.Errorf("NotesAdded = %d, want 16", got.NotesAdded)
	}
	if !got.Fired {
		t.Error("Fired = false, want true")
	}

	if !hasCall(client.calls, "Send", "/live/song/create_midi_track") {
		t.Error("expected create_midi_track")
	}
	if !hasCall(client.calls, "Query", "/live/track/load/browser_item") {
		t.Error("expected load/browser_item")
	}
	if !hasCall(client.calls, "Send", "/live/clip/add/notes") {
		t.Error("expected add/notes")
	}
	if !hasCall(client.calls, "Send", "/live/clip_slot/fire") {
		t.Error("expected fire")
	}
}

func TestSetupDrumTrackByPath(t *testing.T) {
	t.Parallel()

	client := &recipeClientStub{
		queries: map[string][]interface{}{
			"/live/song/get/num_tracks":    {int32(1)},
			"/live/browser/load_at_path":   {int32(0), "loaded", "Core Kit", int32(0), int32(1)},
			"/live/clip_slot/get/has_clip": {int32(0), int32(1), int32(1)},
		},
	}
	clipIndex := 1
	got, err := setupDrumTrack(client, SetupDrumTrackInput{
		RootName:    "Drums",
		PathParts:   []string{"Kits"},
		ItemName:    "Core Kit",
		TrackName:   "Beat",
		ClipIndex:   &clipIndex,
		LengthBeats: 4,
		Pattern:     "basic_backbeat",
	})
	if err != nil {
		t.Fatalf("setupDrumTrack() error = %v", err)
	}
	if got.TrackName != "Beat" {
		t.Errorf("TrackName = %q, want Beat", got.TrackName)
	}
	if got.ClipIndex != 1 {
		t.Errorf("ClipIndex = %d, want 1", got.ClipIndex)
	}
	if !hasCall(client.calls, "QueryWithTimeout", "/live/browser/load_at_path") {
		t.Error("expected load_at_path")
	}
	if hasCall(client.calls, "Send", "/live/clip_slot/fire") {
		t.Error("did not expect fire")
	}
}

func TestSetupDrumTrackValidation(t *testing.T) {
	t.Parallel()

	_, err := setupDrumTrack(&recipeClientStub{}, SetupDrumTrackInput{})
	if err == nil || !strings.Contains(err.Error(), "either kit_name or root_name") {
		t.Fatalf("setupDrumTrack() error = %v, want mutual exclusion error", err)
	}

	_, err = setupDrumTrack(&recipeClientStub{}, SetupDrumTrackInput{
		KitName:  "Street Kit",
		RootName: "Drums",
		ItemName: "Street Kit",
	})
	if err == nil || !strings.Contains(err.Error(), "either kit_name or root_name") {
		t.Fatalf("setupDrumTrack() error = %v, want mutual exclusion error", err)
	}
}

func countPitch(notes []MidiNote, pitch int) int {
	n := 0
	for _, note := range notes {
		if note.Pitch == pitch {
			n++
		}
	}
	return n
}

func hasCall(calls []recipeCall, method, address string) bool {
	for _, c := range calls {
		if c.method == method && c.address == address {
			return true
		}
	}
	return false
}
