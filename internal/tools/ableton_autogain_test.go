package tools

import (
	"errors"
	"math"
	"testing"
	"time"
)

type autogainStub struct {
	volumes map[int]float64
	meters  map[int][]float64
	meterAt map[int]int
	calls   []string
}

func (s *autogainStub) Query(address string, args ...interface{}) ([]interface{}, error) {
	s.calls = append(s.calls, "Query:"+address)
	switch address {
	case "/live/song/get/track_names":
		return []interface{}{"Drums", "Bass"}, nil
	case "/live/track/get/volume":
		idx := int(args[0].(int32))
		v, ok := s.volumes[idx]
		if !ok {
			return nil, errors.New("missing volume")
		}
		return []interface{}{int32(idx), float32(v)}, nil
	case "/live/track/get/output_meter_level":
		idx := int(args[0].(int32))
		seq, ok := s.meters[idx]
		if !ok || len(seq) == 0 {
			return nil, errors.New("missing meter")
		}
		i := s.meterAt[idx]
		if i >= len(seq) {
			i = len(seq) - 1
		}
		s.meterAt[idx] = i + 1
		return []interface{}{int32(idx), float32(seq[i])}, nil
	default:
		return nil, errors.New("unexpected query: " + address)
	}
}

func (s *autogainStub) Send(address string, args ...interface{}) error {
	s.calls = append(s.calls, "Send:"+address)
	if address == "/live/track/set/volume" {
		idx := int(args[0].(int32))
		s.volumes[idx] = float64(args[1].(float32))
	}
	return nil
}

func TestNextAutogainVolume(t *testing.T) {
	t.Parallel()

	got, capped := nextAutogainVolume(0.5, 0.2, 0.4, 0.12)
	if !capped {
		t.Fatal("expected step cap")
	}
	if math.Abs(got-0.62) > 1e-9 {
		t.Errorf("next = %v, want 0.62", got)
	}

	got, capped = nextAutogainVolume(0.5, 0.5, 0.45, 0.12)
	if capped {
		t.Fatal("did not expect cap for small move")
	}
	want := 0.5 * (0.45 / 0.5)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("next = %v, want %v", got, want)
	}
}

func TestAutogainTracksRaisesQuietTrack(t *testing.T) {
	t.Parallel()

	client := &autogainStub{
		volumes: map[int]float64{0: 0.4},
		meters: map[int][]float64{
			// sampleTrackMeter takes 3 samples per call
			0: {
				0.20, 0.20, 0.20, // before
				0.32, 0.32, 0.32, // after first bump
				0.44, 0.44, 0.44, // after second bump (within 0.45±0.05)
			},
		},
		meterAt: map[int]int{},
	}
	settle := 0
	got, err := autogainTracks(client, AutogainTracksInput{
		TrackIndices: []int{0},
		SettleMs:     &settle,
	}, func(time.Duration) {})
	if err != nil {
		t.Fatalf("autogainTracks() error = %v", err)
	}
	if len(got.Results) != 1 {
		t.Fatalf("results len = %d", len(got.Results))
	}
	r := got.Results[0]
	if r.Status != "ok" {
		t.Errorf("Status = %q, want ok", r.Status)
	}
	if r.VolumeAfter <= r.VolumeBefore {
		t.Errorf("volume did not rise: before=%v after=%v", r.VolumeBefore, r.VolumeAfter)
	}
	if r.Iterations < 1 {
		t.Errorf("Iterations = %d, want >= 1", r.Iterations)
	}
}

func TestAutogainTracksSkipsSilent(t *testing.T) {
	t.Parallel()

	client := &autogainStub{
		volumes: map[int]float64{1: 0.7},
		meters: map[int][]float64{
			1: {0.0, 0.0, 0.0},
		},
		meterAt: map[int]int{},
	}
	settle := 0
	got, err := autogainTracks(client, AutogainTracksInput{
		TrackIndices: []int{1},
		SettleMs:     &settle,
	}, func(time.Duration) {})
	if err != nil {
		t.Fatalf("autogainTracks() error = %v", err)
	}
	if got.Results[0].Status != "silent" {
		t.Errorf("Status = %q, want silent", got.Results[0].Status)
	}
	if got.Results[0].VolumeAfter != got.Results[0].VolumeBefore {
		t.Errorf("silent track volume changed: before=%v after=%v",
			got.Results[0].VolumeBefore, got.Results[0].VolumeAfter)
	}
}

func TestAutogainTracksResolvesAllTracks(t *testing.T) {
	t.Parallel()

	client := &autogainStub{
		volumes: map[int]float64{0: 0.5, 1: 0.5},
		meters: map[int][]float64{
			0: {0.45, 0.45, 0.45},
			1: {0.45, 0.45, 0.45},
		},
		meterAt: map[int]int{},
	}
	settle := 0
	got, err := autogainTracks(client, AutogainTracksInput{SettleMs: &settle}, func(time.Duration) {})
	if err != nil {
		t.Fatalf("autogainTracks() error = %v", err)
	}
	if len(got.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(got.Results))
	}
	if got.Results[0].Status != "unchanged" || got.Results[1].Status != "unchanged" {
		t.Errorf("statuses = %q, %q want unchanged", got.Results[0].Status, got.Results[1].Status)
	}
}

func TestAutogainTracksValidation(t *testing.T) {
	t.Parallel()

	bad := 0.01
	_, err := autogainTracks(&autogainStub{}, AutogainTracksInput{TargetLevel: &bad}, func(time.Duration) {})
	if err == nil {
		t.Fatal("expected target_level validation error")
	}
}
