package tools

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// fakeRecorder stands in for Live during a Resampling pass. Song time only
// moves when the injected sleeper runs, so tests are instant and exact.
type fakeRecorder struct {
	tempo        float64
	signature    int
	songTime     float64
	isPlaying    bool
	quantization int
	trackNames   []string
	hasClip      map[int][]bool // per track, per scene
	solo         []bool
	filePath     string
	noClip       bool // Live records nothing (e.g. the track never armed)
	sendErr      map[string]error

	armedTrack int
	recording  bool
	calls      []auditionCall
}

func newFakeRecorder() *fakeRecorder {
	return &fakeRecorder{
		tempo: 120, signature: 4, songTime: 1.5, isPlaying: true, quantization: 7,
		trackNames: []string{"Drums", "Bass", "Measure"},
		hasClip:    map[int][]bool{0: {true, true}, 1: {true, false}, 2: {false, false}},
		solo:       []bool{false, false, false},
		filePath:   "/tmp/Measure 0001.aif",
		armedTrack: -1,
	}
}

func (f *fakeRecorder) advance(d time.Duration) { f.songTime += d.Seconds() * f.tempo / 60 }

func (f *fakeRecorder) addresses() []string {
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.address)
	}
	return out
}

func (f *fakeRecorder) Query(address string, args ...interface{}) ([]interface{}, error) {
	boolish := func(b bool) interface{} {
		if b {
			return int32(1)
		}
		return int32(0)
	}
	switch address {
	case "/live/song/get/tempo":
		return []interface{}{float32(f.tempo)}, nil
	case "/live/song/get/signature_numerator":
		return []interface{}{int32(f.signature)}, nil
	case "/live/song/get/is_playing":
		return []interface{}{boolish(f.isPlaying)}, nil
	case "/live/song/get/clip_trigger_quantization":
		return []interface{}{int32(f.quantization)}, nil
	case "/live/song/get/current_song_time":
		return []interface{}{float32(f.songTime)}, nil
	case "/live/song/get/track_names":
		names := make([]interface{}, len(f.trackNames))
		for i, n := range f.trackNames {
			names[i] = n
		}
		return names, nil
	case "/live/track/get/available_input_routing_types":
		track, _ := asTestInt(args[0])
		return []interface{}{int32(track), "Ext. In", "Resampling", "No Input"}, nil
	case "/live/song/get/track_data":
		from, _ := asTestInt(args[0])
		to, _ := asTestInt(args[1])
		if to < 0 {
			to = len(f.trackNames)
		}
		var out []interface{}
		for t := from; t < to; t++ {
			switch args[2] {
			case "clip_slot.has_clip":
				for _, has := range f.hasClip[t] {
					out = append(out, has)
				}
			case "track.solo":
				out = append(out, f.solo[t])
			}
		}
		return out, nil
	case "/live/clip/get/file_path":
		track, _ := asTestInt(args[0])
		slot, _ := asTestInt(args[1])
		return []interface{}{int32(track), int32(slot), f.filePath}, nil
	}
	return nil, errors.New("unexpected query: " + address)
}

func (f *fakeRecorder) Send(address string, args ...interface{}) error {
	if err := f.sendErr[address]; err != nil {
		return err
	}
	f.calls = append(f.calls, auditionCall{address: address, args: append([]interface{}{}, args...)})
	intArg := func(i int) int { v, _ := asTestInt(args[i]); return v }
	switch address {
	case "/live/song/start_playing":
		f.isPlaying = true
	case "/live/song/set/clip_trigger_quantization":
		f.quantization = intArg(0)
	case "/live/track/set/arm":
		if intArg(1) == 1 {
			f.armedTrack = intArg(0)
		} else {
			f.armedTrack = -1
		}
	case "/live/track/set/solo":
		f.solo[intArg(0)] = intArg(1) == 1
	case "/live/song/set/session_record":
		on := intArg(0) == 1
		if f.recording && !on && !f.noClip && f.armedTrack >= 0 {
			// Live drops the recording into the armed track's first empty slot.
			for slot, has := range f.hasClip[f.armedTrack] {
				if !has {
					f.hasClip[f.armedTrack][slot] = true
					break
				}
			}
		}
		f.recording = on
	case "/live/clip_slot/delete_clip":
		f.hasClip[intArg(0)][intArg(1)] = false
	}
	return nil
}

func (f *fakeRecorder) deps() recordDeps {
	return recordDeps{sleep: f.advance, fileSize: func(string) (int64, error) { return 4096, nil }}
}

func scene(i int) *int { return &i }

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

func TestRecordPassRecordsOneWindowOnSongTime(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	take, err := recordResampledPass(live, live.deps(), recordPlan{TrackName: "Measure", Spans: []recordSpan{{SceneIndex: scene(1), Bars: 2}}})
	if err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}

	// Recording switched on at beat 1.5; the scene lands on the next bar (beat 4)
	// and the window is two bars of 4/4.
	if take.RecordOnBeat != 1.5 || take.WindowStart != 4 || take.WindowBeats != 8 {
		t.Errorf("take = %+v, want record-on 1.5, window 4..12", take)
	}
	if take.RecordOffBeat < 12 {
		t.Errorf("recording stopped at beat %v, before the window ended at 12", take.RecordOffBeat)
	}
	if take.TrackIndex != 2 || len(take.Slots) != 1 || take.Slots[0] != 0 || take.FilePaths[0] != live.filePath {
		t.Errorf("take = %+v, want the new clip in track 2 slot 0", take)
	}

	order := live.addresses()
	for _, pair := range [][2]string{
		{"/live/track/set/arm", "/live/song/set/session_record"},
		{"/live/song/set/session_record", "/live/scene/fire"},
	} {
		if a, b := indexOf(order, pair[0]), indexOf(order, pair[1]); a < 0 || b < 0 || a > b {
			t.Errorf("%s should come before %s in %v", pair[0], pair[1], order)
		}
	}
	if live.recording || live.armedTrack != -1 || live.quantization != 7 {
		t.Errorf("left behind: recording=%v armed=%d quantization=%d; want all restored", live.recording, live.armedTrack, live.quantization)
	}
}

func TestRecordPassNeedsSomethingPlayingWhenNoSceneIsGiven(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.isPlaying = false
	_, err := recordResampledPass(live, live.deps(), recordPlan{TrackName: "Measure", Spans: []recordSpan{{Bars: 2}}})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "nothing_playing" {
		t.Fatalf("error = %v, want nothing_playing", err)
	}
	if indexOf(live.addresses(), "/live/song/set/session_record") >= 0 {
		t.Error("recording must not start when there is nothing to record")
	}
}

func TestRecordPassFiresLaterScenesJustAheadOfTheirBoundary(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	var firedAt []float64
	deps := live.deps()
	base := deps.sleep
	deps.sleep = func(d time.Duration) { base(d) }
	take, err := recordResampledPass(&sceneTimeRecorder{fakeRecorder: live, firedAt: &firedAt}, deps,
		recordPlan{TrackName: "Measure", Spans: []recordSpan{{SceneIndex: scene(0), Bars: 2}, {SceneIndex: scene(1), Bars: 2}}})
	if err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}
	if take.WindowBeats != 16 || len(firedAt) != 2 {
		t.Fatalf("window = %v beats, fired = %v; want 16 beats and two fires", take.WindowBeats, firedAt)
	}
	// The second scene starts at beat 12 (4 + 2 bars). With 1-bar quantization it
	// must be fired inside the bar before that, or it would start a bar late.
	if firedAt[1] < 8 || firedAt[1] >= 12 {
		t.Errorf("second scene fired at beat %v, want within [8, 12)", firedAt[1])
	}
}

// sceneTimeRecorder notes the song time of every scene fire.
type sceneTimeRecorder struct {
	*fakeRecorder
	firedAt *[]float64
}

func (s *sceneTimeRecorder) Send(address string, args ...interface{}) error {
	if address == "/live/scene/fire" {
		*s.firedAt = append(*s.firedAt, s.songTime)
	}
	return s.fakeRecorder.Send(address, args...)
}

func TestRecordPassRestoresLiveWhenAStepFails(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.sendErr = map[string]error{"/live/scene/fire": errors.New("boom")}
	_, err := recordResampledPass(live, live.deps(), recordPlan{TrackName: "Measure", Spans: []recordSpan{{SceneIndex: scene(1), Bars: 2}}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want the fire failure", err)
	}
	if live.recording || live.armedTrack != -1 || live.quantization != 7 {
		t.Errorf("left behind: recording=%v armed=%d quantization=%d; want all restored", live.recording, live.armedTrack, live.quantization)
	}
}

func TestRecordPassReportsARecordingThatNeverAppeared(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.noClip = true
	_, err := recordResampledPass(live, live.deps(), recordPlan{TrackName: "Measure", Spans: []recordSpan{{Bars: 1}}})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "recording_missing" {
		t.Fatalf("error = %v, want recording_missing", err)
	}
}

func TestRecordPassValidatesThePlan(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	for name, plan := range map[string]recordPlan{
		"no spans":       {TrackName: "Measure"},
		"zero bars":      {TrackName: "Measure", Spans: []recordSpan{{Bars: 0}}},
		"too many bars":  {TrackName: "Measure", Spans: []recordSpan{{Bars: 65}}},
		"negative scene": {TrackName: "Measure", Spans: []recordSpan{{SceneIndex: scene(-1), Bars: 1}}},
	} {
		if _, err := recordResampledPass(live, live.deps(), plan); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if len(live.calls) != 0 {
		t.Errorf("calls = %v, want Live untouched by an invalid plan", live.addresses())
	}
}

func TestLocateWindow(t *testing.T) {
	t.Parallel()

	// 120 BPM: half a second per beat. Recording on at beat 1.5, window 4..12,
	// recording off at 12.25.
	take := recordedTake{TempoBPM: 120, RecordOnBeat: 1.5, WindowStart: 4, WindowBeats: 8, RecordOffBeat: 12.25}

	// If Live started recording at once, the file runs 1.5 -> 12.25 (5.375 s) and
	// the window begins 1.25 s in.
	start, end, err := locateWindow(5.37, take)
	if err != nil || math.Abs(start-1.25) > 1e-9 || math.Abs(end-5.25) > 1e-9 {
		t.Errorf("immediate start: window = [%v, %v], %v; want [1.25, 5.25]", start, end, err)
	}
	// If Live waited for the bar, the file runs 4 -> 12.25 (4.125 s) and the
	// window begins at the top.
	start, end, err = locateWindow(4.12, take)
	if err != nil || start != 0 || math.Abs(end-4) > 1e-9 {
		t.Errorf("quantized start: window = [%v, %v], %v; want [0, 4]", start, end, err)
	}

	_, _, err = locateWindow(3.0, take)
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "recording_too_short" {
		t.Errorf("3 s file: error = %v, want recording_too_short", err)
	}
}
