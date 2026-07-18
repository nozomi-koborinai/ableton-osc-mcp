package tools

import (
	"errors"
	"testing"
)

type matchTempoStub struct {
	hasClip  bool
	isAudio  bool
	warping  bool
	tempo    float64
	length   float64
	calls    []string
	sendErr  map[string]error
	queryErr map[string]error
}

func (s *matchTempoStub) Query(address string, _ ...interface{}) ([]interface{}, error) {
	if err := s.queryErr[address]; err != nil {
		return nil, err
	}
	switch address {
	case "/live/clip_slot/get/has_clip":
		return []interface{}{int32(0), int32(0), s.hasClip}, nil
	case "/live/clip/get/is_audio_clip":
		return []interface{}{int32(0), int32(0), s.isAudio}, nil
	case "/live/song/get/tempo":
		return []interface{}{float32(s.tempo)}, nil
	case "/live/clip/get/length":
		return []interface{}{int32(0), int32(0), float32(s.length)}, nil
	case "/live/clip/get/warping":
		return []interface{}{int32(0), int32(0), s.warping}, nil
	default:
		return nil, errors.New("unexpected query: " + address)
	}
}

func (s *matchTempoStub) Send(address string, args ...interface{}) error {
	s.calls = append(s.calls, address)
	if err := s.sendErr[address]; err != nil {
		return err
	}
	if address == "/live/clip/set/warping" {
		s.warping = true
		// Live often reports a different beat length once warp is enabled.
		if s.length > 0 {
			s.length = s.length * 1.25
		}
	}
	return nil
}

func TestMatchClipTempo(t *testing.T) {
	t.Parallel()

	client := &matchTempoStub{
		hasClip: true,
		isAudio: true,
		tempo:   128,
		length:  4,
	}
	got, err := matchClipTempo(client, MatchClipTempoInput{
		TrackIndex: 0,
		ClipIndex:  1,
		WarpMode:   "complex",
		Fire:       true,
	})
	if err != nil {
		t.Fatalf("matchClipTempo() error = %v", err)
	}
	if !got.Warping || got.WarpMode != "complex" || got.TempoBPM != 128 || !got.Fired {
		t.Errorf("got = %#v", got)
	}
	if got.LengthBeatsBefore != 4 || got.LengthBeats <= 4 {
		t.Errorf("lengths before/after = %v / %v", got.LengthBeatsBefore, got.LengthBeats)
	}
	if !containsCall(client.calls, "/live/clip/set/warping") || !containsCall(client.calls, "/live/clip/set/warp_mode") {
		t.Errorf("calls = %v", client.calls)
	}
}

func TestMatchClipTempoRejectsMIDI(t *testing.T) {
	t.Parallel()

	_, err := matchClipTempo(&matchTempoStub{hasClip: true, isAudio: false, tempo: 120, length: 4}, MatchClipTempoInput{
		TrackIndex: 0,
		ClipIndex:  0,
	})
	if err == nil {
		t.Fatal("expected MIDI rejection")
	}
}

func TestMatchClipTempoRejectsEmptySlot(t *testing.T) {
	t.Parallel()

	_, err := matchClipTempo(&matchTempoStub{hasClip: false}, MatchClipTempoInput{
		TrackIndex: 0,
		ClipIndex:  0,
	})
	if err == nil {
		t.Fatal("expected empty slot error")
	}
}

func TestResolveWarpMode(t *testing.T) {
	t.Parallel()

	name, value, err := resolveWarpMode("")
	if err != nil || name != "beats" || value != 0 {
		t.Fatalf("default = %s %d %v", name, value, err)
	}
	_, _, err = resolveWarpMode("texture")
	if err == nil {
		t.Fatal("expected invalid warp mode error")
	}
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
