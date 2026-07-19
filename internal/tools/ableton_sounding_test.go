package tools

import (
	"reflect"
	"testing"
)

func TestParseTrackDataBlock(t *testing.T) {
	// 2 tracks, 2 scenes:
	// per track: name, mute, solo, playing, has0, has1
	values := []interface{}{
		"Drums", true, false, int32(0), true, false,
		"Bass", false, true, int32(-1), true, true,
	}
	got, err := parseTrackDataBlock(values, 2, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0].Name != "Drums" || !got[0].Mute || got[0].Solo || got[0].PlayingSlotIndex != 0 {
		t.Errorf("track0 = %+v", got[0])
	}
	if !reflect.DeepEqual(got[0].ClipSlotsOccupied, []int{0}) {
		t.Errorf("track0 occupied = %v", got[0].ClipSlotsOccupied)
	}
	if got[1].Name != "Bass" || got[1].Mute || !got[1].Solo || got[1].PlayingSlotIndex != -1 {
		t.Errorf("track1 = %+v", got[1])
	}
	if !reflect.DeepEqual(got[1].ClipSlotsOccupied, []int{0, 1}) {
		t.Errorf("track1 occupied = %v", got[1].ClipSlotsOccupied)
	}
}

func TestParseTrackDataBlockTooShort(t *testing.T) {
	if _, err := parseTrackDataBlock([]interface{}{"Drums"}, 2, 2); err == nil {
		t.Fatal("expected error for short payload")
	}
}

func TestGetSoundingSnapshot(t *testing.T) {
	client := snapshotQuerierStub{results: map[string]snapshotQueryResult{
		"/live/song/get/tempo":       {values: []interface{}{float32(90)}},
		"/live/song/get/is_playing":  {values: []interface{}{false}},
		"/live/song/get/num_tracks":  {values: []interface{}{int32(1)}},
		"/live/song/get/num_scenes":  {values: []interface{}{int32(1)}},
		"/live/song/get/scenes/name": {values: []interface{}{"Intro"}},
		"/live/song/get/track_data": {values: []interface{}{
			"Drums", false, false, int32(-1), true,
		}},
		"/live/track/get/devices/name":       {values: []interface{}{int32(0), "Boom Bap Kit"}},
		"/live/track/get/devices/class_name": {values: []interface{}{int32(0), "DrumGroupDevice"}},
	}}
	got, err := getSoundingSnapshot(client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TempoBPM != 90 || got.IsPlaying || len(got.Scenes) != 1 || got.Scenes[0].Name != "Intro" {
		t.Errorf("header = %+v", got)
	}
	if len(got.Tracks) != 1 || got.Tracks[0].Name != "Drums" {
		t.Fatalf("tracks = %+v", got.Tracks)
	}
	if len(got.Tracks[0].Devices) != 1 || got.Tracks[0].Devices[0].ClassName != "DrumGroupDevice" {
		t.Errorf("devices = %+v", got.Tracks[0].Devices)
	}
	if !reflect.DeepEqual(got.Tracks[0].ClipSlotsOccupied, []int{0}) {
		t.Errorf("occupied = %v", got.Tracks[0].ClipSlotsOccupied)
	}
}
