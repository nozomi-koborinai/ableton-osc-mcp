package tools

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

type auditionCall struct {
	address string
	args    []interface{}
}

type auditionStub struct {
	tempo        float64
	songTime     float64
	signature    int
	isPlaying    bool
	quantization int
	queries      []string
	calls        []auditionCall
	sendErr      map[string]error
}

func (s *auditionStub) Query(address string, _ ...interface{}) ([]interface{}, error) {
	s.queries = append(s.queries, address)
	switch address {
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

func (s *auditionStub) Send(address string, args ...interface{}) error {
	if err := s.sendErr[address]; err != nil {
		return err
	}
	s.calls = append(s.calls, auditionCall{address: address, args: append([]interface{}{}, args...)})
	if address == "/live/song/start_playing" {
		s.isPlaying = true
	}
	if address == "/live/song/set/clip_trigger_quantization" && len(args) > 0 {
		if q, err := asTestInt(args[0]); err == nil {
			s.quantization = q
		}
	}
	return nil
}

func asTestInt(v interface{}) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	default:
		return 0, errors.New("not int")
	}
}

func advanceAuditionStub(s *auditionStub, d time.Duration) {
	s.songTime += d.Seconds() * s.tempo / 60
}

func TestAuditionABClip(t *testing.T) {
	t.Parallel()

	trackIndex := 2
	bars := 1
	cycles := 2
	client := &auditionStub{
		tempo:        120,
		songTime:     0.5,
		signature:    4,
		isPlaying:    true,
		quantization: 7,
	}
	got, err := auditionAB(client, AuditionABInput{
		TargetType:     "clip",
		TrackIndex:     &trackIndex,
		SourceIndex:    0,
		VariationIndex: 1,
		BarsPerVersion: &bars,
		Cycles:         &cycles,
		StopAfter:      true,
	}, func(d time.Duration) {
		advanceAuditionStub(client, d)
	})
	if err != nil {
		t.Fatalf("auditionAB() error = %v", err)
	}
	if got.BeatsPerBar != 4 || got.PlaybackStarted {
		t.Errorf("output = %#v", got)
	}
	if got.FinalVersion != "variation" {
		t.Errorf("final_version = %q", got.FinalVersion)
	}
	// From 0.5: launch at 4, hear to 8; then launch at 12, hear to 16; then 20→24; then 28→32.
	// Duration counts from each fire's song time through endBeat.
	if got.DurationSec <= 0 {
		t.Errorf("duration_sec = %v, want > 0", got.DurationSec)
	}

	wantAddresses := []string{
		"/live/song/set/clip_trigger_quantization", // 1 bar
		"/live/clip_slot/fire",
		"/live/clip_slot/fire",
		"/live/clip_slot/fire",
		"/live/clip_slot/fire",
		"/live/song/stop_playing",
		"/live/song/set/clip_trigger_quantization", // restore
	}
	gotAddresses := make([]string, 0, len(client.calls))
	for _, call := range client.calls {
		gotAddresses = append(gotAddresses, call.address)
	}
	if !reflect.DeepEqual(gotAddresses, wantAddresses) {
		t.Errorf("send calls = %v, want %v", gotAddresses, wantAddresses)
	}
	if client.quantization != 7 {
		t.Errorf("quantization after audition = %d, want restored 7", client.quantization)
	}
}

func TestAuditionABStartsPlaybackWhenStopped(t *testing.T) {
	t.Parallel()

	trackIndex := 0
	bars := 1
	client := &auditionStub{
		tempo:        120,
		songTime:     0,
		signature:    4,
		isPlaying:    false,
		quantization: 4,
	}
	got, err := auditionAB(client, AuditionABInput{
		TargetType:     "clip",
		TrackIndex:     &trackIndex,
		SourceIndex:    0,
		VariationIndex: 1,
		BarsPerVersion: &bars,
	}, func(d time.Duration) {
		advanceAuditionStub(client, d)
	})
	if err != nil {
		t.Fatalf("auditionAB() error = %v", err)
	}
	if !got.PlaybackStarted {
		t.Fatal("expected playback_started=true when transport was stopped")
	}
	if client.calls[0].address != "/live/song/start_playing" {
		t.Errorf("first send = %s, want start_playing", client.calls[0].address)
	}
}

func TestAuditionABScene(t *testing.T) {
	t.Parallel()

	client := &auditionStub{
		tempo:        60,
		songTime:     1,
		signature:    4,
		isPlaying:    true,
		quantization: 9,
	}
	got, err := auditionAB(client, AuditionABInput{
		TargetType:     "scene",
		SourceIndex:    3,
		VariationIndex: 4,
	}, func(d time.Duration) {
		advanceAuditionStub(client, d)
	})
	if err != nil {
		t.Fatalf("auditionAB() error = %v", err)
	}
	if got.TrackIndex != nil || got.BeatsPerBar != 4 {
		t.Errorf("output = %#v", got)
	}
	for _, call := range client.calls {
		if call.address == "/live/scene/fire" || call.address == "/live/song/set/clip_trigger_quantization" {
			continue
		}
		t.Errorf("unexpected send: %#v", call)
	}
	if client.quantization != 9 {
		t.Errorf("quantization = %d, want restored 9", client.quantization)
	}
}

func TestAuditionABUsesBeatsPerBarOverride(t *testing.T) {
	t.Parallel()

	trackIndex := 0
	bars := 1
	beats := 3
	client := &auditionStub{
		tempo:        120,
		songTime:     0.1,
		signature:    4,
		isPlaying:    true,
		quantization: 4,
	}
	got, err := auditionAB(client, AuditionABInput{
		TargetType:     "clip",
		TrackIndex:     &trackIndex,
		SourceIndex:    0,
		VariationIndex: 2,
		BarsPerVersion: &bars,
		BeatsPerBar:    &beats,
	}, func(d time.Duration) {
		advanceAuditionStub(client, d)
	})
	if err != nil {
		t.Fatalf("auditionAB() error = %v", err)
	}
	if got.BeatsPerBar != 3 {
		t.Errorf("beats_per_bar = %d, want 3", got.BeatsPerBar)
	}
	for _, q := range client.queries {
		if q == "/live/song/get/signature_numerator" {
			t.Fatal("should not query signature when beats_per_bar override is set")
		}
	}
}

func TestCeilBarBeat(t *testing.T) {
	t.Parallel()

	if got := ceilBarBeat(0, 4); got != 4 {
		t.Errorf("ceilBarBeat(0) = %v, want 4", got)
	}
	if got := ceilBarBeat(0.5, 4); got != 4 {
		t.Errorf("ceilBarBeat(0.5) = %v, want 4", got)
	}
	if got := ceilBarBeat(4, 4); got != 8 {
		t.Errorf("ceilBarBeat(4) = %v, want 8", got)
	}
	if got := ceilBarBeat(7.9, 4); got != 8 {
		t.Errorf("ceilBarBeat(7.9) = %v, want 8", got)
	}
}

func TestValidateAuditionInput(t *testing.T) {
	t.Parallel()

	_, _, _, _, err := validateAuditionInput(AuditionABInput{
		TargetType:     "clip",
		SourceIndex:    0,
		VariationIndex: 1,
	})
	if err == nil {
		t.Fatal("clip audition without track index should fail")
	}

	trackIndex := 0
	_, _, _, _, err = validateAuditionInput(AuditionABInput{
		TargetType:     "scene",
		TrackIndex:     &trackIndex,
		SourceIndex:    0,
		VariationIndex: 1,
	})
	if err == nil {
		t.Fatal("scene audition with track index should fail")
	}

	_, _, _, beats, err := validateAuditionInput(AuditionABInput{
		TargetType:     "scene",
		SourceIndex:    0,
		VariationIndex: 1,
	})
	if err != nil {
		t.Fatalf("validateAuditionInput() error = %v", err)
	}
	if beats != 0 {
		t.Errorf("beats override = %d, want 0 (query Live)", beats)
	}
}
