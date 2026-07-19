package tools

import (
	"strings"
	"testing"
)

type fakeSceneClient struct {
	numScenes   int
	sceneNames  []string
	numTracks   int
	hasClip     map[[2]int]bool
	sent        []string
	createFails bool
}

func (f *fakeSceneClient) Send(address string, args ...interface{}) error {
	f.sent = append(f.sent, address)
	switch address {
	case "/live/song/create_scene":
		if f.createFails {
			return nil
		}
		f.numScenes++
		f.sceneNames = append(f.sceneNames, "")
	case "/live/scene/set/name":
		idx := int(args[0].(int32))
		for len(f.sceneNames) <= idx {
			f.sceneNames = append(f.sceneNames, "")
		}
		f.sceneNames[idx] = args[1].(string)
	case "/live/clip_slot/delete_clip":
		key := [2]int{int(args[0].(int32)), int(args[1].(int32))}
		f.hasClip[key] = false
	case "/live/clip_slot/duplicate_clip_to":
		srcT, srcC := int(args[0].(int32)), int(args[1].(int32))
		dstT, dstC := int(args[2].(int32)), int(args[3].(int32))
		if f.hasClip[[2]int{srcT, srcC}] {
			f.hasClip[[2]int{dstT, dstC}] = true
		}
	}
	return nil
}

func (f *fakeSceneClient) Query(address string, args ...interface{}) ([]interface{}, error) {
	switch address {
	case "/live/song/get/num_scenes":
		return []interface{}{int32(f.numScenes)}, nil
	case "/live/song/get/scenes/name":
		out := make([]interface{}, len(f.sceneNames))
		for i, n := range f.sceneNames {
			out[i] = n
		}
		return out, nil
	case "/live/song/get/num_tracks":
		return []interface{}{int32(f.numTracks)}, nil
	case "/live/clip_slot/get/has_clip":
		key := [2]int{int(args[0].(int32)), int(args[1].(int32))}
		return []interface{}{args[0], args[1], f.hasClip[key]}, nil
	}
	return nil, nil
}

func TestCreateNamedScenes(t *testing.T) {
	c := &fakeSceneClient{numScenes: 1, sceneNames: []string{"Untitled"}}
	out, err := createNamedScenes(c, []string{"Intro", "Verse", "Hook"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ScenesAfter != 4 || len(out.Created) != 3 {
		t.Fatalf("out = %+v", out)
	}
	if out.Created[0].Index != 1 || out.Created[0].Name != "Intro" {
		t.Errorf("first created = %+v", out.Created[0])
	}
	if out.Created[2].Name != "Hook" || out.Created[2].Index != 3 {
		t.Errorf("last created = %+v", out.Created[2])
	}
}

func TestCreateNamedScenesRejectsEmpty(t *testing.T) {
	c := &fakeSceneClient{}
	if _, err := createNamedScenes(c, []string{"Intro", "  "}); err == nil {
		t.Fatal("expected error for blank name")
	}
}

func TestSetSceneClipPresenceDelete(t *testing.T) {
	c := &fakeSceneClient{
		numTracks: 3,
		numScenes: 2,
		hasClip: map[[2]int]bool{
			{0, 1}: true,
			{1, 1}: true,
			{2, 1}: false,
		},
	}
	out, err := setSceneClipPresence(c, SetSceneClipPresenceInput{
		SceneIndex: 1,
		Present:    false,
		Confirm:    true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	actions := map[int]string{}
	for _, ch := range out.Changes {
		actions[ch.TrackIndex] = ch.Action
	}
	if actions[0] != "deleted" || actions[1] != "deleted" || actions[2] != "skipped_empty" {
		t.Errorf("actions = %v", actions)
	}
	if c.hasClip[[2]int{0, 1}] || c.hasClip[[2]int{1, 1}] {
		t.Error("clips should be deleted")
	}
}

func TestSetSceneClipPresenceRestore(t *testing.T) {
	src := 0
	c := &fakeSceneClient{
		numTracks: 2,
		numScenes: 2,
		hasClip: map[[2]int]bool{
			{0, 0}: true,
			{1, 0}: true,
			{0, 1}: false,
			{1, 1}: true, // already present → skip
		},
	}
	out, err := setSceneClipPresence(c, SetSceneClipPresenceInput{
		SceneIndex:       1,
		Present:          true,
		SourceSceneIndex: &src,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	actions := map[int]string{}
	for _, ch := range out.Changes {
		actions[ch.TrackIndex] = ch.Action
	}
	if actions[0] != "restored" || actions[1] != "skipped_already_present" {
		t.Errorf("actions = %v", actions)
	}
	if !c.hasClip[[2]int{0, 1}] {
		t.Error("track 0 scene 1 should have been restored")
	}
}

func TestSetSceneClipPresenceRequiresSource(t *testing.T) {
	c := &fakeSceneClient{numTracks: 1}
	_, err := setSceneClipPresence(c, SetSceneClipPresenceInput{SceneIndex: 0, Present: true})
	if err == nil || !strings.Contains(err.Error(), "source_scene_index") {
		t.Fatalf("expected source_scene_index error, got %v", err)
	}
}

func TestSetSceneClipPresenceRequiresConfirm(t *testing.T) {
	c := &fakeSceneClient{numTracks: 1, hasClip: map[[2]int]bool{{0, 0}: true}}
	_, err := setSceneClipPresence(c, SetSceneClipPresenceInput{SceneIndex: 0, Present: false})
	if err == nil || !strings.Contains(err.Error(), "confirm_required") {
		t.Fatalf("expected confirm_required, got %v", err)
	}
	if c.hasClip[[2]int{0, 0}] != true {
		t.Error("clip should not be deleted without confirm")
	}
}
