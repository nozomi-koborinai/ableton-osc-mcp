package tools

import (
	"strings"
	"testing"
	"time"
)

type fxABStub struct {
	active    map[int]int // device_index -> 0/1
	songTime  float64
	calls     []string
	setActive []int
}

func (s *fxABStub) Query(address string, args ...interface{}) ([]interface{}, error) {
	s.calls = append(s.calls, address)
	switch address {
	case "/live/track/get/devices/name":
		return []interface{}{int32(0), "Simpler", "Reverb", "Delay"}, nil
	case "/live/track/get/devices/type":
		return []interface{}{int32(0), int32(1), int32(2), int32(2)}, nil
	case "/live/device/get/is_active":
		idx := int(args[1].(int32))
		return []interface{}{int32(0), int32(idx), int32(s.active[idx])}, nil
	case "/live/device/set/is_active":
		idx := int(args[1].(int32))
		val := int(args[2].(int32))
		s.active[idx] = val
		s.setActive = append(s.setActive, val)
		return []interface{}{int32(0), int32(idx), "ok", int32(val)}, nil
	case "/live/song/get/tempo":
		return []interface{}{float32(120)}, nil
	case "/live/song/get/signature_numerator":
		return []interface{}{int32(4)}, nil
	case "/live/song/get/is_playing":
		return []interface{}{true}, nil
	case "/live/song/get/clip_trigger_quantization":
		return []interface{}{int32(4)}, nil
	case "/live/song/get/current_song_time":
		// Advance a bit each poll so waitUntilSongTime completes quickly.
		s.songTime += 2
		return []interface{}{s.songTime}, nil
	default:
		return nil, nil
	}
}

func (s *fxABStub) Send(address string, args ...interface{}) error {
	s.calls = append(s.calls, "send:"+address)
	return nil
}

func TestCompareFXBypassDryThenWet(t *testing.T) {
	t.Parallel()

	client := &fxABStub{active: map[int]int{1: 1, 2: 1}}
	nopSleep := func(time.Duration) {}
	got, err := compareFXBypass(client, CompareFXBypassInput{
		TrackIndex:     0,
		ClipIndex:      0,
		BarsPerVersion: intPtr(1),
		Cycles:         intPtr(1),
	}, nopSleep)
	if err != nil {
		t.Fatalf("compareFXBypass() error = %v", err)
	}
	if len(got.Devices) != 2 {
		t.Fatalf("devices = %+v, want Reverb+Delay", got.Devices)
	}
	if got.Devices[0].DeviceIndex != 1 || got.Devices[1].DeviceIndex != 2 {
		t.Errorf("device indices = %+v", got.Devices)
	}
	// Pattern: bypass(0), restore(1) per cycle, plus final restore(1).
	// setActive records every set is_active value in order.
	if len(client.setActive) < 4 {
		t.Fatalf("setActive calls = %v", client.setActive)
	}
	if client.setActive[0] != 0 || client.setActive[1] != 0 {
		t.Errorf("first cycle should bypass both FX, got %v", client.setActive[:2])
	}
	if !strings.Contains(got.PreferencePrompt, "instrument=fx variation=bypass") {
		t.Errorf("prompt = %q", got.PreferencePrompt)
	}
	if !got.Restored {
		t.Error("expected Restored=true")
	}
}

func TestResolveFXBypassRejectsInstrument(t *testing.T) {
	t.Parallel()

	client := &fxABStub{active: map[int]int{0: 1}}
	_, err := resolveFXBypassDevices(client, 0, []int{0})
	if err == nil || !strings.Contains(err.Error(), "instrument_not_bypassable") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateTasteFXBypass(t *testing.T) {
	t.Parallel()

	got, err := validateTastePreference(RecordVariationPreferenceInput{
		Instrument: "fx",
		Variation:  "bypass",
		Preferred:  "source",
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got.Instrument != "fx" || got.Variation != "bypass" {
		t.Errorf("got %#v", got)
	}
}
