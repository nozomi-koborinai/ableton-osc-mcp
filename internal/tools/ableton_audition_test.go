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
	tempo []interface{}
	calls []auditionCall
}

func (s *auditionStub) Query(address string, _ ...interface{}) ([]interface{}, error) {
	if address != "/live/song/get/tempo" {
		return nil, errors.New("unexpected query: " + address)
	}
	return s.tempo, nil
}

func (s *auditionStub) Send(address string, args ...interface{}) error {
	s.calls = append(s.calls, auditionCall{address: address, args: append([]interface{}{}, args...)})
	return nil
}

func TestAuditionABClip(t *testing.T) {
	t.Parallel()

	trackIndex := 2
	bars := 1
	cycles := 2
	client := &auditionStub{tempo: []interface{}{float32(120)}}
	var waits []time.Duration
	got, err := auditionAB(client, AuditionABInput{
		TargetType:     "clip",
		TrackIndex:     &trackIndex,
		SourceIndex:    0,
		VariationIndex: 1,
		BarsPerVersion: &bars,
		Cycles:         &cycles,
		StartPlayback:  true,
		StopAfter:      true,
	}, func(d time.Duration) {
		waits = append(waits, d)
	})
	if err != nil {
		t.Fatalf("auditionAB() error = %v", err)
	}
	if len(waits) != 4 {
		t.Fatalf("waits = %v, want four waits", waits)
	}
	for _, wait := range waits {
		if wait != 2*time.Second {
			t.Errorf("wait = %v, want 2s", wait)
		}
	}
	if got.DurationSec != 8 || got.FinalVersion != "variation" {
		t.Errorf("output = %#v", got)
	}
	wantAddresses := []string{
		"/live/song/start_playing",
		"/live/clip_slot/fire",
		"/live/clip_slot/fire",
		"/live/clip_slot/fire",
		"/live/clip_slot/fire",
		"/live/song/stop_playing",
	}
	gotAddresses := make([]string, 0, len(client.calls))
	for _, call := range client.calls {
		gotAddresses = append(gotAddresses, call.address)
	}
	if !reflect.DeepEqual(gotAddresses, wantAddresses) {
		t.Errorf("send calls = %v, want %v", gotAddresses, wantAddresses)
	}
}

func TestAuditionABScene(t *testing.T) {
	t.Parallel()

	client := &auditionStub{tempo: []interface{}{float64(60)}}
	var waits []time.Duration
	got, err := auditionAB(client, AuditionABInput{
		TargetType:     "scene",
		SourceIndex:    3,
		VariationIndex: 4,
	}, func(d time.Duration) {
		waits = append(waits, d)
	})
	if err != nil {
		t.Fatalf("auditionAB() error = %v", err)
	}
	if got.TrackIndex != nil || len(waits) != 2 || waits[0] != 8*time.Second {
		t.Errorf("output = %#v waits=%v", got, waits)
	}
	for _, call := range client.calls {
		if call.address != "/live/scene/fire" {
			t.Errorf("unexpected send: %#v", call)
		}
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
}
