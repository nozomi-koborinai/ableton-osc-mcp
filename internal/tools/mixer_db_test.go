package tools

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

// fakeMixer stands in for Live plus the patched AbletonOSC: it keeps raw fader
// positions and answers the dB handlers with a Live-shaped fader law.
type fakeMixer struct {
	volumes  []float64          // track volumes, raw
	sends    map[[2]int]float64 // {track, send} -> raw
	master   float64
	noPatch  bool // true: the dB handlers are not installed
	sent     []string
	failSend map[string]error
}

func fakeFaderDisplay(raw float64) string {
	if raw <= 0 {
		return "-inf dB"
	}
	db := 40*raw - 34
	if raw < 0.4 {
		db = -18 - (0.4-raw)*130
	}
	db = math.Round(db*10) / 10
	if db == 0 {
		db = 0 // a fader at unity reads "0.0 dB", never "-0.0 dB"
	}
	return fmt.Sprintf("%.1f dB", db)
}

func fakeRawForDB(target float64) (float64, string) {
	if target <= -70 {
		return 0, "ok"
	}
	lo, hi, best := 0.0, 1.0, 1.0
	for i := 0; i < 24; i++ {
		mid := (lo + hi) / 2
		best = mid
		shown, silent, _ := parseDBDisplay(fakeFaderDisplay(mid))
		if !silent && math.Abs(shown-target) <= 0.051 {
			return best, "ok"
		}
		if silent || shown < target {
			lo = mid
		} else {
			hi = mid
		}
	}
	return best, "out_of_range"
}

func (f *fakeMixer) Query(address string, args ...interface{}) ([]interface{}, error) {
	patched := map[string]bool{
		"/live/track/get/volume_db": true, "/live/track/get/volume_for_db": true,
		"/live/track/get/send_db": true, "/live/track/get/send_for_db": true,
		"/live/song/get/track_volumes_db": true,
		"/live/master/get/volume_db":      true, "/live/master/get/volume_for_db": true,
	}
	if patched[address] && f.noPatch {
		return nil, errors.New("no response received to query: " + address)
	}
	intArg := func(i int) int { v, _ := asTestInt(args[i]); return v }
	floatArg := func(i int) float64 {
		switch v := args[i].(type) {
		case float32:
			return float64(v)
		case float64:
			return v
		}
		return 0
	}
	switch address {
	case "/live/track/get/volume":
		t := intArg(0)
		return []interface{}{int32(t), float32(f.volumes[t])}, nil
	case "/live/track/get/volume_db":
		t := intArg(0)
		if t >= len(f.volumes) {
			return []interface{}{int32(t), "invalid_track_index"}, nil
		}
		return []interface{}{int32(t), fakeFaderDisplay(f.volumes[t]), float32(f.volumes[t])}, nil
	case "/live/track/get/volume_for_db":
		t := intArg(0)
		if t >= len(f.volumes) {
			return []interface{}{int32(t), "invalid_track_index"}, nil
		}
		raw, status := fakeRawForDB(floatArg(1))
		return []interface{}{int32(t), float32(raw), fakeFaderDisplay(raw), status}, nil
	case "/live/track/get/send_db":
		t, s := intArg(0), intArg(1)
		raw, ok := f.sends[[2]int{t, s}]
		if !ok {
			return []interface{}{int32(t), int32(s), "invalid_send_index"}, nil
		}
		return []interface{}{int32(t), int32(s), fakeFaderDisplay(raw), float32(raw)}, nil
	case "/live/track/get/send_for_db":
		t, s := intArg(0), intArg(1)
		if _, ok := f.sends[[2]int{t, s}]; !ok {
			return []interface{}{int32(t), int32(s), "invalid_send_index"}, nil
		}
		raw, status := fakeRawForDB(floatArg(2))
		return []interface{}{int32(t), int32(s), float32(raw), fakeFaderDisplay(raw), status}, nil
	case "/live/song/get/track_volumes_db":
		reply := []interface{}{int32(len(f.volumes))}
		for _, raw := range f.volumes {
			reply = append(reply, fakeFaderDisplay(raw), float32(raw))
		}
		return reply, nil
	case "/live/master/get/volume":
		return []interface{}{float32(f.master)}, nil
	case "/live/master/get/volume_db":
		return []interface{}{fakeFaderDisplay(f.master), float32(f.master)}, nil
	case "/live/master/get/volume_for_db":
		raw, status := fakeRawForDB(floatArg(0))
		return []interface{}{float32(raw), fakeFaderDisplay(raw), status}, nil
	case "/live/song/get/return_tracks":
		return []interface{}{int32(2), "A-Reverb", "B-Delay"}, nil
	case "/live/track/get/send":
		t, s := intArg(0), intArg(1)
		raw, ok := f.sends[[2]int{t, s}]
		if !ok {
			return nil, errors.New("no response received to query: " + address)
		}
		return []interface{}{int32(t), int32(s), float32(raw)}, nil
	case "/live/song/get/track_names":
		names := make([]interface{}, len(f.volumes))
		for i := range names {
			names[i] = fmt.Sprintf("Track %d", i)
		}
		return names, nil
	}
	return nil, errors.New("unexpected query: " + address)
}

func (f *fakeMixer) Send(address string, args ...interface{}) error {
	if err := f.failSend[address]; err != nil {
		return err
	}
	f.sent = append(f.sent, address)
	intArg := func(i int) int { v, _ := asTestInt(args[i]); return v }
	raw := func(i int) float64 { return float64(args[i].(float32)) }
	switch address {
	case "/live/track/set/volume":
		f.volumes[intArg(0)] = raw(1)
	case "/live/track/set/send":
		f.sends[[2]int{intArg(0), intArg(1)}] = raw(2)
	case "/live/master/set/volume":
		f.master = raw(0)
	default:
		return errors.New("unexpected send: " + address)
	}
	return nil
}

func ptr(v float64) *float64 { return &v }

func TestParseDBDisplay(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]float64{"-6.0 dB": -6, "0.0 dB": 0, "6.0 dB": 6, "+3.5 dB": 3.5} {
		db, silent, err := parseDBDisplay(text)
		if err != nil || silent || db != want {
			t.Errorf("parseDBDisplay(%q) = %v, %v, %v; want %v", text, db, silent, err, want)
		}
	}
	if _, silent, err := parseDBDisplay("-inf dB"); err != nil || !silent {
		t.Errorf("-inf dB should read as silence, got silent=%v err=%v", silent, err)
	}
	for _, text := range []string{"", "50 %", "180 Hz"} {
		if _, _, err := parseDBDisplay(text); err == nil {
			t.Errorf("parseDBDisplay(%q) should fail", text)
		}
	}
}

func TestApplyLevelChangeSetsAnAbsoluteDB(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85, 0.85}}
	got, err := applyLevelChange(live, trackVolumeTarget(1), levelChange{DB: ptr(-24)}, "volume")
	if err != nil {
		t.Fatalf("applyLevelChange() error = %v", err)
	}
	// -24 dB sits below the linear part of the fader law, where a formula would miss.
	if got.Display != "-24.0 dB" || got.DB != -24 {
		t.Errorf("level = %+v, want display -24.0 dB", got)
	}
	if live.volumes[0] != 0.85 {
		t.Errorf("track 0 moved to %v; only track 1 was addressed", live.volumes[0])
	}
	if len(live.sent) != 1 || live.sent[0] != "/live/track/set/volume" {
		t.Errorf("sent = %v, want one stock volume setter call", live.sent)
	}
}

func TestApplyLevelChangeAddsADeltaToTheCurrentLevel(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.6}} // 40*0.6-34 = -10.0 dB
	got, err := applyLevelChange(live, trackVolumeTarget(0), levelChange{DeltaDB: ptr(-2)}, "volume")
	if err != nil {
		t.Fatalf("applyLevelChange() error = %v", err)
	}
	if got.Display != "-12.0 dB" {
		t.Errorf("display = %q, want -12.0 dB", got.Display)
	}
}

func TestApplyLevelChangeRefusesADeltaFromSilence(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0}}
	_, err := applyLevelChange(live, trackVolumeTarget(0), levelChange{DeltaDB: ptr(3)}, "volume")
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "delta_from_silence" {
		t.Fatalf("error = %v, want delta_from_silence", err)
	}
	if len(live.sent) != 0 {
		t.Errorf("sent = %v, want nothing changed", live.sent)
	}
}

func TestApplyLevelChangeReportsALevelTheFaderCannotReach(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85}}
	_, err := applyLevelChange(live, trackVolumeTarget(0), levelChange{DB: ptr(10)}, "volume")
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "level_out_of_range" {
		t.Fatalf("error = %v, want level_out_of_range", err)
	}
	if !strings.Contains(actionableErr.Message, "6.0 dB") {
		t.Errorf("message should say how far the fader goes: %q", actionableErr.Message)
	}
	if live.volumes[0] != 0.85 {
		t.Errorf("volume moved to %v although the request failed", live.volumes[0])
	}
}

func TestApplyLevelChangeWithoutThePatch(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85}, noPatch: true}

	_, err := applyLevelChange(live, trackVolumeTarget(0), levelChange{DB: ptr(-6)}, "volume")
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "mixer_db_patch_missing" {
		t.Fatalf("dB without the patch: error = %v, want mixer_db_patch_missing", err)
	}

	// Raw values keep working on an old patch; only the display is unavailable.
	got, err := applyLevelChange(live, trackVolumeTarget(0), levelChange{Raw: ptr(0.5)}, "volume")
	if err != nil {
		t.Fatalf("raw without the patch: error = %v", err)
	}
	if got.Raw != 0.5 || got.Display != "" || live.volumes[0] != 0.5 {
		t.Errorf("level = %+v, live = %v; want raw 0.5 and no display", got, live.volumes[0])
	}
}

func TestApplyLevelChangeWantsExactlyOneForm(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85}}
	for name, change := range map[string]levelChange{
		"none":         {},
		"raw and db":   {Raw: ptr(0.5), DB: ptr(-6)},
		"db and delta": {DB: ptr(-6), DeltaDB: ptr(1)},
		"raw above 1":  {Raw: ptr(1.5)},
		"db is NaN":    {DB: ptr(math.NaN())},
	} {
		_, err := applyLevelChange(live, trackVolumeTarget(0), change, "volume")
		var actionableErr *ActionableError
		if !errors.As(err, &actionableErr) || actionableErr.Code != "invalid_level_change" {
			t.Errorf("%s: error = %v, want invalid_level_change", name, err)
		}
	}
	if len(live.sent) != 0 {
		t.Errorf("sent = %v, want nothing changed", live.sent)
	}
}

func TestApplyLevelChangeOnSendsAndMaster(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85}, sends: map[[2]int]float64{{0, 1}: 0.2}, master: 0.85}

	send, err := applyLevelChange(live, trackSendTarget(0, 1), levelChange{DB: ptr(-12)}, "value")
	if err != nil || send.Display != "-12.0 dB" {
		t.Errorf("send = %+v, %v; want -12.0 dB", send, err)
	}
	master, err := applyLevelChange(live, masterVolumeTarget(), levelChange{DeltaDB: ptr(-3)}, "volume")
	if err != nil || master.Display != "-3.0 dB" {
		t.Errorf("master = %+v, %v; want -3.0 dB", master, err)
	}

	_, err = applyLevelChange(live, trackSendTarget(0, 5), levelChange{DB: ptr(-12)}, "value")
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "invalid_send_index" {
		t.Errorf("unknown send: error = %v, want invalid_send_index", err)
	}
}

func TestQueryTrackVolumesDB(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85, 0, 0.6}}
	got, err := queryTrackVolumesDB(live)
	if err != nil {
		t.Fatalf("queryTrackVolumesDB() error = %v", err)
	}
	if len(got) != 3 || got[0].Display != "0.0 dB" || !got[1].Silent || got[2].DB != -10 {
		t.Errorf("levels = %+v", got)
	}

	live.noPatch = true
	if _, err := queryTrackVolumesDB(live); !isMixerDBPatchMissing(err) {
		t.Errorf("without the patch: error = %v, want a patch-missing error", err)
	}
}
