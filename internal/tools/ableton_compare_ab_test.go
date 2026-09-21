package tools

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeCompareLive is the audition fake plus what creating a variation needs:
// notes to read, and clips and scenes that appear when they are duplicated.
type fakeCompareLive struct {
	*fakeAuditionLive
}

func newFakeCompareLive() *fakeCompareLive {
	live := newFakeAuditionLive()
	live.hasClip = map[int][]bool{0: {true, false, false}, 1: {true, false, false}, 2: {false, false, false}}
	return &fakeCompareLive{live}
}

func (f *fakeCompareLive) Query(address string, args ...interface{}) ([]interface{}, error) {
	switch address {
	case "/live/clip/get/notes":
		return []interface{}{args[0], args[1], int32(36), float32(0), float32(0.25), int32(100), false}, nil
	case "/live/clip/get/length":
		return []interface{}{args[0], args[1], float32(2)}, nil
	}
	return f.fakeAuditionLive.Query(address, args...)
}

func (f *fakeCompareLive) Send(address string, args ...interface{}) error {
	intArg := func(i int) int { v, _ := asTestInt(args[i]); return v }
	switch address {
	case "/live/clip_slot/duplicate_clip_to":
		f.hasClip[intArg(2)][intArg(3)] = true
	case "/live/song/duplicate_scene":
		row := intArg(0)
		for t, slots := range f.hasClip {
			grown := append([]bool{}, slots[:row+1]...)
			f.hasClip[t] = append(append(grown, slots[row]), slots[row+1:]...)
		}
	}
	return f.fakeAuditionLive.Send(address, args...)
}

func (f *fakeCompareLive) fired() [][2]int {
	var out [][2]int
	for _, e := range f.events("/live/clip_slot/fire") {
		track, _ := asTestInt(e.args[0])
		slot, _ := asTestInt(e.args[1])
		out = append(out, [2]int{track, slot})
	}
	return out
}

func TestCompareABVariationDrumAuditionsSourceThenVariationAndPutsTheTrackBack(t *testing.T) {
	t.Parallel()

	live := newFakeCompareLive()
	strength, seed := 1.0, int64(1)
	got, err := compareABVariation(live, CompareABVariationInput{
		Kind: "drum", Variation: "density",
		TrackIndex: intPtr(0), SourceClipIndex: intPtr(0), TargetClipIndex: intPtr(1),
		Strength: &strength, Seed: &seed, BarsPerVersion: intPtr(1), Cycles: intPtr(2),
	}, live.sleeper())
	if err != nil {
		t.Fatalf("compareABVariation() error = %v", err)
	}
	if got.Kind != "drum" || got.Variation != "density" || got.NotesAdded == 0 || got.SourceIndex != 0 || got.VariationIndex != 1 {
		t.Errorf("result = %#v", got)
	}
	if !strings.Contains(got.PreferencePrompt, "instrument=drum variation=density") {
		t.Errorf("preference_prompt = %q", got.PreferencePrompt)
	}

	var labels []string
	for _, p := range got.Audition.Played {
		labels = append(labels, p.Label)
	}
	if !reflect.DeepEqual(labels, []string{"A", "B", "A", "B"}) || got.Audition.BarsPerVariant != 1 || !got.Audition.Restored {
		t.Errorf("audition = %+v, want A B A B of one bar each, restored", got.Audition)
	}
	// The source was already playing, so A needs no launch. B, A, B, and the source again at the end.
	if want := [][2]int{{0, 1}, {0, 0}, {0, 1}, {0, 0}}; !reflect.DeepEqual(live.fired(), want) {
		t.Errorf("fired = %v, want %v", live.fired(), want)
	}
	if live.playing[0] != 0 {
		t.Errorf("track 0 plays slot %d afterwards, want the source clip it played before", live.playing[0])
	}
	// The variation has to exist before anything is launched.
	var order []string
	for _, e := range live.timeline {
		if e.address == "/live/clip_slot/duplicate_clip_to" || e.address == "/live/clip_slot/fire" {
			order = append(order, e.address)
		}
	}
	if len(order) == 0 || order[0] != "/live/clip_slot/duplicate_clip_to" {
		t.Errorf("order = %v, want the duplicate before the first fire", order)
	}
	if got.Audition.Prompt != "" {
		t.Errorf("audition.prompt = %q; the comparison asks its own question", got.Audition.Prompt)
	}
}

func TestCompareABVariationLeavesASilentTrackSilent(t *testing.T) {
	t.Parallel()

	live := newFakeCompareLive()
	live.playing[0] = -1
	strength, seed := 1.0, int64(1)
	if _, err := compareABVariation(live, CompareABVariationInput{
		Kind: "drum", Variation: "density",
		TrackIndex: intPtr(0), SourceClipIndex: intPtr(0), TargetClipIndex: intPtr(1),
		Strength: &strength, Seed: &seed, BarsPerVersion: intPtr(1),
	}, live.sleeper()); err != nil {
		t.Fatalf("compareABVariation() error = %v", err)
	}
	if want := [][2]int{{0, 0}, {0, 1}}; !reflect.DeepEqual(live.fired(), want) {
		t.Errorf("fired = %v, want A then B", live.fired())
	}
	if live.playing[0] != -1 {
		t.Errorf("track 0 plays slot %d afterwards; it played nothing before", live.playing[0])
	}
}

func TestCompareABVariationSceneSwitchesEveryClipOfTheRowWithoutLaunchingScenes(t *testing.T) {
	t.Parallel()

	live := newFakeCompareLive()
	got, err := compareABVariation(live, CompareABVariationInput{
		Kind: "scene", Variation: "lift", SourceSceneIndex: intPtr(0), TrackIndices: []int{0}, BarsPerVersion: intPtr(1),
	}, live.sleeper())
	if err != nil {
		t.Fatalf("compareABVariation() error = %v", err)
	}
	if got.SourceIndex != 0 || got.VariationIndex != 1 || !reflect.DeepEqual(got.TracksChanged, []int{0}) {
		t.Errorf("result = %#v", got)
	}
	// Both tracks with a clip in the row switch, the varied one and the one that only came along.
	if want := [][2]int{{0, 1}, {1, 1}, {0, 0}, {1, 0}}; !reflect.DeepEqual(live.fired(), want) {
		t.Errorf("fired = %v, want %v", live.fired(), want)
	}
	if n := len(live.events("/live/scene/fire")); n != 0 {
		t.Errorf("scene launches = %d; a scene launch would also stop the tracks the row leaves empty", n)
	}
	if !strings.Contains(got.PreferencePrompt, "instrument=scene variation=lift") {
		t.Errorf("preference_prompt = %q", got.PreferencePrompt)
	}
}

func TestCompareABVariationChecksTheAuditionBeforeCreatingAnything(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]CompareABVariationInput{
		"too many bars":   {Kind: "drum", Variation: "density", TrackIndex: intPtr(0), SourceClipIndex: intPtr(0), TargetClipIndex: intPtr(1), BarsPerVersion: intPtr(9)},
		"too many cycles": {Kind: "drum", Variation: "density", TrackIndex: intPtr(0), SourceClipIndex: intPtr(0), TargetClipIndex: intPtr(1), Cycles: intPtr(5)},
	} {
		live := newFakeCompareLive()
		if _, err := compareABVariation(live, input, live.sleeper()); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if len(live.timeline) != 0 {
			t.Errorf("%s: Live was touched: %v", name, live.timeline)
		}
	}
}

func TestCompareABVariationRejectsClipFieldsForScene(t *testing.T) {
	t.Parallel()

	live := newFakeCompareLive()
	_, err := compareABVariation(live, CompareABVariationInput{
		Kind: "scene", Variation: "lift", TrackIndex: intPtr(0), SourceSceneIndex: intPtr(1), TrackIndices: []int{0},
	}, time.Sleep)
	if err == nil {
		t.Fatal("expected clip-field rejection for scene kind")
	}
}

func TestCompareABVariationRequiresClipSlots(t *testing.T) {
	t.Parallel()

	live := newFakeCompareLive()
	if _, err := compareABVariation(live, CompareABVariationInput{Kind: "bass", Variation: "octave_up"}, time.Sleep); err == nil {
		t.Fatal("expected missing clip slot error")
	}
}

func TestVariationPreferencePrompt(t *testing.T) {
	t.Parallel()

	specific := auditionPreferencePrompt("clip", "drum", "groove")
	if !strings.Contains(specific, "instrument=drum variation=groove") {
		t.Errorf("specific prompt = %q", specific)
	}
	scene := auditionPreferencePrompt("scene", "", "")
	if !strings.Contains(scene, "instrument=scene") || !strings.Contains(scene, "lift or pullback") {
		t.Errorf("scene prompt = %q", scene)
	}
	clip := auditionPreferencePrompt("clip", "", "")
	if !strings.Contains(clip, "drum or bass") {
		t.Errorf("clip prompt = %q", clip)
	}
}
