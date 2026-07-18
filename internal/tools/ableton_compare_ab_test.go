package tools

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type compareABCall struct {
	address string
	args    []interface{}
}

type compareABStub struct {
	hasClip      bool
	notes        []interface{}
	clipLength   float64
	tempo        float64
	signature    int
	isPlaying    bool
	quantization int
	songTime     float64
	calls        []compareABCall
	sendErr      map[string]error
}

func (s *compareABStub) Query(address string, args ...interface{}) ([]interface{}, error) {
	switch address {
	case "/live/clip_slot/get/has_clip":
		return []interface{}{int32(0), int32(1), s.hasClip}, nil
	case "/live/clip/get/notes":
		return s.notes, nil
	case "/live/clip/get/length":
		return []interface{}{int32(0), int32(0), float32(s.clipLength)}, nil
	case "/live/song/get/tempo":
		return []interface{}{float32(s.tempo)}, nil
	case "/live/song/get/signature_numerator":
		return []interface{}{int32(s.signature)}, nil
	case "/live/song/get/is_playing":
		if s.isPlaying {
			return []interface{}{int32(1)}, nil
		}
		return []interface{}{int32(0)}, nil
	case "/live/song/get/clip_trigger_quantization":
		return []interface{}{int32(s.quantization)}, nil
	case "/live/song/get/current_song_time":
		return []interface{}{float32(s.songTime)}, nil
	default:
		return nil, errors.New("unexpected query: " + address)
	}
}

func (s *compareABStub) Send(address string, args ...interface{}) error {
	if err := s.sendErr[address]; err != nil {
		return err
	}
	s.calls = append(s.calls, compareABCall{address: address, args: append([]interface{}{}, args...)})
	if address == "/live/song/start_playing" {
		s.isPlaying = true
	}
	if address == "/live/song/set/clip_trigger_quantization" && len(args) > 0 {
		if q, ok := args[0].(int32); ok {
			s.quantization = int(q)
		}
	}
	return nil
}

func TestCompareABVariationDrum(t *testing.T) {
	t.Parallel()

	track := 0
	source := 0
	target := 1
	bars := 1
	strength := 1.0
	seed := int64(1)
	client := &compareABStub{
		hasClip:    false,
		notes:      []interface{}{int32(0), int32(0), int32(36), float32(0), float32(0.25), int32(100), false},
		clipLength: 2,
		tempo:      120,
		signature:  4,
		isPlaying:  true,
		songTime:   0,
	}

	got, err := compareABVariation(client, CompareABVariationInput{
		Kind:            "drum",
		Variation:       "density",
		TrackIndex:      &track,
		SourceClipIndex: &source,
		TargetClipIndex: &target,
		Strength:        &strength,
		Seed:            &seed,
		BarsPerVersion:  &bars,
	}, func(d time.Duration) {
		client.songTime += d.Seconds() * client.tempo / 60
	})
	if err != nil {
		t.Fatalf("compareABVariation() error = %v", err)
	}
	if got.Kind != "drum" || got.Variation != "density" || got.NotesAdded == 0 {
		t.Errorf("result = %#v", got)
	}
	if got.SourceIndex != 0 || got.VariationIndex != 1 {
		t.Errorf("indices = %#v", got)
	}
	if !strings.Contains(got.PreferencePrompt, "instrument=drum variation=density") {
		t.Errorf("preference_prompt = %q", got.PreferencePrompt)
	}
	if !hasCompareSend(client.calls, "/live/clip_slot/duplicate_clip_to") {
		t.Error("expected variation duplicate")
	}
	if countCompareSend(client.calls, "/live/clip_slot/fire") < 2 {
		t.Error("expected audition to fire A and B")
	}
	// Create must not fire B before audition owns launching.
	dupIdx := indexCompareSend(client.calls, "/live/clip_slot/duplicate_clip_to")
	firstFire := indexCompareSend(client.calls, "/live/clip_slot/fire")
	if dupIdx < 0 || firstFire < 0 || firstFire < dupIdx {
		t.Errorf("fire should happen after create; dup=%d fire=%d", dupIdx, firstFire)
	}
}

func TestCompareABVariationRejectsClipFieldsForScene(t *testing.T) {
	t.Parallel()

	track := 0
	scene := 1
	_, err := compareABVariation(&compareABStub{}, CompareABVariationInput{
		Kind:             "scene",
		Variation:        "lift",
		TrackIndex:       &track,
		SourceSceneIndex: &scene,
		TrackIndices:     []int{0},
	}, time.Sleep)
	if err == nil {
		t.Fatal("expected clip-field rejection for scene kind")
	}
}

func TestCompareABVariationRequiresClipSlots(t *testing.T) {
	t.Parallel()

	_, err := compareABVariation(&compareABStub{}, CompareABVariationInput{
		Kind:      "bass",
		Variation: "octave_up",
	}, time.Sleep)
	if err == nil {
		t.Fatal("expected missing clip slot error")
	}
}

func hasCompareSend(calls []compareABCall, address string) bool {
	return indexCompareSend(calls, address) >= 0
}

func indexCompareSend(calls []compareABCall, address string) int {
	for i, call := range calls {
		if call.address == address {
			return i
		}
	}
	return -1
}

func countCompareSend(calls []compareABCall, address string) int {
	n := 0
	for _, call := range calls {
		if call.address == address {
			n++
		}
	}
	return n
}
