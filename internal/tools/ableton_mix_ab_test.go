package tools

import (
	"errors"
	"math"
	"strings"
	"testing"
)

type mixABCall struct {
	address string
	index   int
	volume  float64
}

type mixABStub struct {
	volumes   map[int]float64
	failTrack *int
	calls     []mixABCall
}

func (s *mixABStub) Query(address string, args ...interface{}) ([]interface{}, error) {
	switch address {
	case "/live/song/get/track_names":
		return []interface{}{"Drums", "Bass"}, nil
	case "/live/track/get/volume":
		index := int(args[0].(int32))
		volume, ok := s.volumes[index]
		if !ok {
			return nil, errors.New("missing track volume")
		}
		return []interface{}{int32(index), float32(volume)}, nil
	default:
		return nil, errors.New("unexpected query: " + address)
	}
}

func (s *mixABStub) Send(address string, args ...interface{}) error {
	if address != "/live/track/set/volume" {
		return errors.New("unexpected send: " + address)
	}
	index := int(args[0].(int32))
	volume := float64(args[1].(float32))
	s.calls = append(s.calls, mixABCall{address: address, index: index, volume: volume})
	if s.failTrack != nil && index == *s.failTrack {
		return errors.New("set volume failed")
	}
	s.volumes[index] = volume
	return nil
}

func TestCaptureMixSnapshot(t *testing.T) {
	t.Parallel()

	client := &mixABStub{volumes: map[int]float64{0: 0.5, 1: 0.7}}
	got, err := captureMixSnapshot(client, nil)
	if err != nil {
		t.Fatalf("captureMixSnapshot() error = %v", err)
	}
	if len(got.Tracks) != 2 {
		t.Fatalf("tracks = %#v, want two", got.Tracks)
	}
	if got.Tracks[0] != (MixTrackLevel{TrackIndex: 0, Volume: 0.5}) {
		t.Errorf("track 0 = %#v", got.Tracks[0])
	}
}

func TestApplyMixVariationAndRestore(t *testing.T) {
	t.Parallel()

	client := &mixABStub{volumes: map[int]float64{0: 0.5, 1: 0.7}}
	got, err := applyMixVariation(client, ApplyMixVariationInput{
		Changes: []MixVolumeChange{
			{TrackIndex: 0, Delta: 0.1},
			{TrackIndex: 1, Delta: -0.1},
		},
	})
	if err != nil {
		t.Fatalf("applyMixVariation() error = %v", err)
	}
	if math.Abs(got.Before.Tracks[0].Volume-0.5) > 1e-6 || math.Abs(got.After.Tracks[0].Volume-0.6) > 1e-6 {
		t.Errorf("track 0 snapshots = %#v", got)
	}
	if math.Abs(client.volumes[1]-0.6) > 1e-6 {
		t.Errorf("track 1 volume = %v, want 0.6", client.volumes[1])
	}
	if !strings.Contains(got.PreferencePrompt, "instrument=mix variation=volume") {
		t.Errorf("preference_prompt = %q", got.PreferencePrompt)
	}

	restored, err := restoreMixSnapshot(client, got.Before.Tracks)
	if err != nil {
		t.Fatalf("restoreMixSnapshot() error = %v", err)
	}
	if len(restored.Tracks) != 2 || math.Abs(client.volumes[0]-0.5) > 1e-6 || math.Abs(client.volumes[1]-0.7) > 1e-6 {
		t.Errorf("restored = %#v volumes=%v", restored, client.volumes)
	}
}

func TestApplyMixVariationRollsBackOnSetFailure(t *testing.T) {
	t.Parallel()

	failing := 1
	client := &mixABStub{
		volumes:   map[int]float64{0: 0.5, 1: 0.7},
		failTrack: &failing,
	}
	_, err := applyMixVariation(client, ApplyMixVariationInput{
		Changes: []MixVolumeChange{
			{TrackIndex: 0, Delta: 0.1},
			{TrackIndex: 1, Delta: -0.1},
		},
	})
	if err == nil {
		t.Fatal("applyMixVariation() error = nil, want error")
	}
	if math.Abs(client.volumes[0]-0.5) > 1e-6 {
		t.Errorf("track 0 volume = %v, want rollback to 0.5", client.volumes[0])
	}
}

func TestApplyMixVariationValidation(t *testing.T) {
	t.Parallel()

	client := &mixABStub{volumes: map[int]float64{0: 0.5}}
	_, err := applyMixVariation(client, ApplyMixVariationInput{})
	if err == nil {
		t.Fatal("empty changes should fail")
	}
	_, err = applyMixVariation(client, ApplyMixVariationInput{
		Changes: []MixVolumeChange{{TrackIndex: 0, Delta: 0.3}},
	})
	if err == nil {
		t.Fatal("oversized delta should fail")
	}
}
