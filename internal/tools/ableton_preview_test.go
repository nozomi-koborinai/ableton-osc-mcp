package tools

import (
	"strings"
	"testing"
)

type previewFakeClient struct {
	trackName string
	devices   []string
	hasClip   bool
	clipName  string
	numTracks int
}

func (f *previewFakeClient) Query(address string, args ...interface{}) ([]interface{}, error) {
	switch address {
	case "/live/track/get/name":
		return []interface{}{args[0], f.trackName}, nil
	case "/live/track/get/devices/name":
		out := []interface{}{args[0]}
		for _, d := range f.devices {
			out = append(out, d)
		}
		return out, nil
	case "/live/song/get/num_tracks":
		return []interface{}{int32(f.numTracks)}, nil
	case "/live/clip_slot/get/has_clip":
		return []interface{}{args[0], args[1], f.hasClip}, nil
	case "/live/clip/get/name":
		return []interface{}{args[0], args[1], f.clipName}, nil
	case "/live/clip/get/notes":
		// track, clip, + 2 notes × 5 fields
		return []interface{}{args[0], args[1],
			int32(60), float32(0), float32(0.5), int32(100), false,
			int32(62), float32(1), float32(0.5), int32(90), false,
		}, nil
	}
	return nil, nil
}

func TestPreviewDeleteTrack(t *testing.T) {
	c := &previewFakeClient{trackName: "Drums", devices: []string{"Kit", "EQ"}, numTracks: 4}
	idx := 0
	out, err := previewDestructive(c, PreviewDestructiveInput{Action: "delete_track", TrackIndex: &idx})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Destructive || !strings.Contains(out.Summary, "Drums") {
		t.Errorf("out = %+v", out)
	}
	if !strings.Contains(out.Hint, "confirm=true") {
		t.Errorf("hint = %q", out.Hint)
	}
}

func TestPreviewDeleteClipEmpty(t *testing.T) {
	c := &previewFakeClient{hasClip: false}
	tIdx, cIdx := 0, 1
	out, err := previewDestructive(c, PreviewDestructiveInput{
		Action: "delete_clip", TrackIndex: &tIdx, ClipIndex: &cIdx,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Destructive {
		t.Error("empty slot should not be marked destructive")
	}
}

func TestPreviewDeleteDevice(t *testing.T) {
	c := &previewFakeClient{devices: []string{"EQ Eight", "Reverb"}}
	tIdx, dIdx := 0, 1
	out, err := previewDestructive(c, PreviewDestructiveInput{
		Action: "delete_device", TrackIndex: &tIdx, DeviceIndex: &dIdx,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.Summary, "Reverb") {
		t.Errorf("summary = %q", out.Summary)
	}
}

func TestPreviewClearScene(t *testing.T) {
	c := &previewFakeClient{numTracks: 2, hasClip: true, trackName: "A"}
	scene := 3
	out, err := previewDestructive(c, PreviewDestructiveInput{
		Action: "clear_scene_clips", SceneIndex: &scene,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.Summary, "2 clip") {
		t.Errorf("summary = %q", out.Summary)
	}
}

func TestPreviewUnknownAction(t *testing.T) {
	_, err := previewDestructive(&previewFakeClient{}, PreviewDestructiveInput{Action: "explode"})
	if err == nil || !strings.Contains(err.Error(), "unknown_action") {
		t.Fatalf("got %v", err)
	}
}

func TestRequireConfirm(t *testing.T) {
	if err := requireConfirm(true, "delete_track", "x"); err != nil {
		t.Fatalf("confirm true should pass: %v", err)
	}
	err := requireConfirm(false, "delete_track", "track 0")
	if err == nil {
		t.Fatal("expected error")
	}
	ae, ok := err.(*ActionableError)
	if !ok || ae.Code != "confirm_required" {
		t.Fatalf("got %#v", err)
	}
	if !strings.Contains(err.Error(), "next:") {
		t.Errorf("error missing next step: %v", err)
	}
}

func TestActionableErrorFormat(t *testing.T) {
	err := actionable("no_clip", "slot empty", "Create a clip first.")
	got := err.Error()
	if !strings.Contains(got, "no_clip:") || !strings.Contains(got, "next: Create a clip first.") {
		t.Errorf("format = %q", got)
	}
}
