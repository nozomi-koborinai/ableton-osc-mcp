package tools

import (
	"errors"
	"testing"
)

func TestSetTrackVolumeInDB(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85, 0.85}}
	got, err := setTrackVolume(live, SetTrackVolumeInput{TrackIndex: 1, DB: ptr(-6)})
	if err != nil {
		t.Fatalf("setTrackVolume() error = %v", err)
	}
	if got.TrackIndex == nil || *got.TrackIndex != 1 || got.Display != "-6.0 dB" || got.SendIndex != nil {
		t.Errorf("got = %+v, want track 1 at -6.0 dB", got)
	}
	if _, err := setTrackVolume(live, SetTrackVolumeInput{TrackIndex: -1, DB: ptr(-6)}); err == nil {
		t.Error("negative track_index should fail")
	}
}

func TestSetTrackVolumeKeepsAcceptingRawValues(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85}, noPatch: true}
	got, err := setTrackVolume(live, SetTrackVolumeInput{TrackIndex: 0, Volume: ptr(0.5)})
	if err != nil {
		t.Fatalf("setTrackVolume() error = %v", err)
	}
	if got.Value != 0.5 || got.Display != "" || live.volumes[0] != 0.5 {
		t.Errorf("got = %+v, live = %v; want raw 0.5 applied on an old patch", got, live.volumes[0])
	}
}

func TestSetTrackSendByDelta(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85}, sends: map[[2]int]float64{{0, 1}: 0.6}} // -10.0 dB
	got, err := setTrackSend(live, SetTrackSendInput{TrackIndex: 0, SendIndex: 1, DeltaDB: ptr(4)})
	if err != nil {
		t.Fatalf("setTrackSend() error = %v", err)
	}
	if got.Display != "-6.0 dB" || got.SendIndex == nil || *got.SendIndex != 1 {
		t.Errorf("got = %+v, want send 1 at -6.0 dB", got)
	}
}

func TestSetMasterVolumeInDB(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{master: 0.85}
	got, err := setMasterVolume(live, SetMasterVolumeInput{DB: ptr(-3)})
	if err != nil {
		t.Fatalf("setMasterVolume() error = %v", err)
	}
	if got.Display != "-3.0 dB" || got.TrackIndex != nil || got.SendIndex != nil {
		t.Errorf("got = %+v, want -3.0 dB with no indices", got)
	}

	_, err = setMasterVolume(live, SetMasterVolumeInput{})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "invalid_level_change" {
		t.Errorf("empty input: error = %v, want invalid_level_change", err)
	}
}

func TestGetTrackSendsShowsLevelsInDB(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85}, sends: map[[2]int]float64{{0, 0}: 0.85, {0, 1}: 0}}
	got, err := getTrackSends(live, GetTrackSendsInput{TrackIndex: 0})
	if err != nil {
		t.Fatalf("getTrackSends() error = %v", err)
	}
	if len(got.Sends) != 2 || got.Sends[0].ReturnName != "A-Reverb" || got.Sends[0].Display != "0.0 dB" || got.Sends[1].Display != "-inf dB" {
		t.Errorf("sends = %+v", got.Sends)
	}

	live.noPatch = true
	got, err = getTrackSends(live, GetTrackSendsInput{TrackIndex: 0})
	if err != nil || len(got.Sends) != 2 || got.Sends[0].Display != "" {
		t.Errorf("without the patch: sends = %+v, err = %v; want raw values and no display", got.Sends, err)
	}
}

func TestGetMasterVolumeShowsTheLevelInDB(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{master: 0.6}
	got, err := getMasterVolume(live)
	if err != nil || got.Display != "-10.0 dB" {
		t.Errorf("got = %+v, %v; want -10.0 dB", got, err)
	}
	live.noPatch = true
	got, err = getMasterVolume(live)
	if err != nil || got.Display != "" || got.Volume == 0 {
		t.Errorf("without the patch: got = %+v, %v; want the raw volume and no display", got, err)
	}
}
