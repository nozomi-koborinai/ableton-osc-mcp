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
//
// It models what Live 11.0.12 was observed to do (2026-09-21):
//   - Session Record records into the armed track's slot in the *selected*
//     scene, and only if that slot has a stop button.
//   - Recording starts on the bar after Session Record goes on, and ends on the
//     bar after it goes off. Disarming before that bar cuts the take short.
//   - Launching a scene acts on the armed track too: a stop button in that row
//     cancels the recording, and so does launching the recording row itself.
//   - Commands take a moment to reach Live (AbletonOSC works through its queue
//     on a timer), so one sent just before a bar line lands after it and waits
//     for the following bar.
//   - Session Record records on *every* armed track. Live arms a MIDI track by
//     itself when it gets selected, so a listener's set usually has one: it
//     would get a stray empty clip, and whatever it was playing would stop.
type fakeRecorder struct {
	tempo         float64
	signature     int
	songTime      float64
	isPlaying     bool
	quantization  int
	trackNames    []string
	hasClip       map[int][]bool // per track, per scene
	stopButton    map[int][]bool // per track, per scene
	solo          []bool
	selectedScene int
	filePath      string
	sendErr       map[string]error

	userArmed    map[int]bool // tracks the listener has armed (not the recording track)
	strayTakes   int          // recordings Live started on the listener's armed tracks
	armedTrack   int
	recStatus    int     // 0 off, 1 recording, 2 waiting for the bar
	recOnAt      float64 // bar on which a pending recording starts
	recOffAt     float64 // bar on which a pending stop lands (0 = none)
	recRow       int
	recStartBeat float64
	sceneAt      map[float64][]int // scene launches, by the bar they land on
	truncated    bool              // the track was disarmed before the recording had ended
	takeBeats    []float64         // length of every finished take
	calls        []auditionCall
}

func newFakeRecorder() *fakeRecorder {
	return &fakeRecorder{
		tempo: 120, signature: 4, songTime: 1.5, isPlaying: true, quantization: 7,
		trackNames: []string{"Drums", "Bass", "Measure"},
		hasClip:    map[int][]bool{0: {true, true, false}, 1: {true, false, false}, 2: {false, false, false}},
		stopButton: map[int][]bool{0: {true, true, true}, 1: {true, true, true}, 2: {true, true, true}},
		solo:       []bool{false, false, false},
		filePath:   "/tmp/Measure 0001.aif",
		armedTrack: -1, recRow: -1,
		sceneAt:   map[float64][]int{},
		userArmed: map[int]bool{},
	}
}

// commandLatencyBeats is how long a command takes to take effect in the fake.
const commandLatencyBeats = 0.3

func (f *fakeRecorder) nextBar() float64 {
	bar := float64(f.signature)
	return (math.Floor(f.songTime/bar) + 1) * bar
}

// barForCommand is the bar line a command sent now will land on.
func (f *fakeRecorder) barForCommand() float64 {
	bar := float64(f.signature)
	return (math.Floor((f.songTime+commandLatencyBeats)/bar) + 1) * bar
}

// advance moves song time forward and lets everything that was waiting for a
// bar line happen as that line is crossed.
func (f *fakeRecorder) advance(d time.Duration) {
	target := f.songTime + d.Seconds()*f.tempo/60
	for {
		bar := f.nextBar()
		if bar > target {
			break
		}
		f.songTime = bar
		f.crossBar(bar)
	}
	f.songTime = target
}

func (f *fakeRecorder) crossBar(bar float64) {
	for _, scene := range f.sceneAt[bar] {
		if f.armedTrack >= 0 && f.recStatus != 0 && (scene == f.recRow || f.stopButton[f.armedTrack][scene]) {
			// The launch reaches the armed track: whatever was recording, or about to, is gone.
			if f.recStatus == 1 {
				f.hasClip[f.armedTrack][f.recRow] = false
			}
			f.recStatus, f.recOffAt = 0, 0
		}
	}
	delete(f.sceneAt, bar)
	if f.recStatus == 2 && bar >= f.recOnAt {
		for _, on := range f.userArmed {
			if on {
				f.strayTakes++
			}
		}
		row := f.selectedScene
		if f.armedTrack >= 0 && f.stopButton[f.armedTrack][row] && !f.hasClip[f.armedTrack][row] {
			f.recStatus, f.recRow, f.recStartBeat = 1, row, bar
			f.hasClip[f.armedTrack][row] = true
		} else {
			f.recStatus = 0
		}
	}
	if f.recStatus == 1 && f.recOffAt != 0 && bar >= f.recOffAt {
		f.takeBeats = append(f.takeBeats, bar-f.recStartBeat)
		f.recStatus, f.recOffAt = 0, 0
	}
}

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
	case "/live/song/get/session_record_status":
		return []interface{}{int32(f.recStatus)}, nil
	case "/live/song/get/num_scenes":
		return []interface{}{int32(len(f.hasClip[0]))}, nil
	case "/live/view/get/selected_scene":
		return []interface{}{int32(f.selectedScene)}, nil
	case "/live/track/get/arm":
		track, _ := asTestInt(args[0])
		return []interface{}{int32(track), f.armedTrack == track || f.userArmed[track]}, nil
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
			case "track.arm":
				out = append(out, f.armedTrack == t || f.userArmed[t])
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
	case "/live/view/set/selected_scene":
		f.selectedScene = intArg(0)
	case "/live/clip_slot/set/has_stop_button":
		f.stopButton[intArg(0)][intArg(1)] = intArg(2) == 1
	case "/live/song/create_scene":
		for t := range f.hasClip {
			f.hasClip[t] = append(f.hasClip[t], false)
			f.stopButton[t] = append(f.stopButton[t], true)
		}
	case "/live/scene/fire":
		bar := f.barForCommand()
		f.sceneAt[bar] = append(f.sceneAt[bar], intArg(0))
	case "/live/track/set/arm":
		if track := intArg(0); track != len(f.trackNames)-1 {
			f.userArmed[track] = intArg(1) == 1
			return nil
		}
		if intArg(1) == 1 {
			f.armedTrack = intArg(0)
		} else {
			if f.recStatus == 1 {
				f.truncated = true // Live keeps only what was recorded so far
				f.takeBeats = append(f.takeBeats, math.Floor((f.songTime-f.recStartBeat)/float64(f.signature))*float64(f.signature))
				f.recStatus, f.recOffAt = 0, 0
			}
			f.armedTrack = -1
		}
	case "/live/track/set/solo":
		f.solo[intArg(0)] = intArg(1) == 1
	case "/live/song/set/session_record":
		if intArg(0) == 1 {
			if f.recStatus == 0 {
				f.recStatus, f.recOnAt = 2, f.barForCommand()
			}
		} else {
			switch f.recStatus {
			case 2:
				f.recStatus = 0
			case 1:
				f.recOffAt = f.barForCommand()
			}
		}
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
	// Scene 1 is launched, so the take goes to the last free row that is not: row 2.
	if take.TrackIndex != 2 || len(take.Slots) != 1 || take.Slots[0] != 2 || take.FilePaths[0] != live.filePath {
		t.Errorf("take = %+v, want the new clip in track 2, row 2", take)
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
	if live.recStatus != 0 || live.armedTrack != -1 || live.quantization != 7 {
		t.Errorf("left behind: record status=%d armed=%d quantization=%d; want all restored", live.recStatus, live.armedTrack, live.quantization)
	}
}

func TestRecordPassRecordsInARowThatIsNeverLaunched(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	take, err := recordResampledPass(live, live.deps(), recordPlan{TrackName: "Measure",
		Spans: []recordSpan{{SceneIndex: scene(0), Bars: 2}, {SceneIndex: scene(1), Bars: 2}}})
	if err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}
	// Scenes 0 and 1 are launched, so the take has to live in row 2, which keeps
	// its stop button (Session Record needs one) while rows 0 and 1 lose theirs
	// (or launching them would stop the recording).
	if len(take.Slots) != 1 || take.Slots[0] != 2 {
		t.Fatalf("slots = %v, want the take in row 2", take.Slots)
	}
	if got := live.stopButton[2]; got[0] || got[1] || !got[2] {
		t.Errorf("stop buttons on the Measure track = %v, want only row 2 to keep one", got)
	}
	if len(live.takeBeats) != 1 || live.takeBeats[0] != 16 || live.truncated {
		t.Errorf("takes = %v truncated = %v, want one whole 16-beat take across both scenes", live.takeBeats, live.truncated)
	}
}

func TestRecordPassAddsASceneWhenEveryRowIsTaken(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.hasClip[2][2] = true // a kept recording already sits in the only free row
	take, err := recordResampledPass(live, live.deps(), recordPlan{TrackName: "Measure",
		Spans: []recordSpan{{SceneIndex: scene(0), Bars: 1}, {SceneIndex: scene(1), Bars: 1}}})
	if err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}
	if len(take.Slots) != 1 || take.Slots[0] != 3 || len(live.hasClip[0]) != 4 {
		t.Errorf("slots = %v with %d scenes, want a new fourth row holding the take", take.Slots, len(live.hasClip[0]))
	}
}

func TestRecordPassWaitsForTheBarBeforeDisarming(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.selectedScene = 1
	take, err := recordResampledPass(live, live.deps(), recordPlan{TrackName: "Measure", Spans: []recordSpan{{Bars: 2}}})
	if err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}
	if live.truncated {
		t.Error("the track was disarmed while Live was still recording; the take is cut short")
	}
	if len(live.takeBeats) != 1 || live.takeBeats[0] != 8 {
		t.Errorf("takes = %v, want exactly the two bars that were asked for", live.takeBeats)
	}
	if take.RecordOffBeat != take.WindowStart+take.WindowBeats {
		t.Errorf("record off = %v, want the window's end %v", take.RecordOffBeat, take.WindowStart+take.WindowBeats)
	}
	if live.selectedScene != 1 {
		t.Errorf("selected scene = %d, want the listener's own selection (1) restored", live.selectedScene)
	}
}

func TestRecordPassKeepsTheListenersArmedTracksOutOfIt(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.userArmed[0] = true // the track the listener is playing: Live armed it on selection
	if _, err := recordResampledPass(live, live.deps(), recordPlan{TrackName: "Measure", Spans: []recordSpan{{Bars: 1}}}); err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}
	if live.strayTakes != 0 {
		t.Errorf("Live recorded on %d of the listener's tracks; their clips stop and empty clips pile up", live.strayTakes)
	}
	if !live.userArmed[0] || live.userArmed[1] {
		t.Errorf("arm states after the pass = %v, want the listener's own arming back (track 0 only)", live.userArmed)
	}
}

func TestRecordPassDoesNotStartRightBeforeABarLine(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.songTime = 3.9 // a tenth of a beat before bar line 4: a command sent now lands after it
	take, err := recordResampledPass(live, live.deps(), recordPlan{TrackName: "Measure", Spans: []recordSpan{{SceneIndex: scene(1), Bars: 1}}})
	if err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}
	if take.WindowStart != 8 {
		t.Errorf("window starts at beat %v, want 8: the pass should let bar line 4 go by and aim for the next", take.WindowStart)
	}
	if len(live.takeBeats) != 1 || live.takeBeats[0] != 4 {
		t.Errorf("takes = %v, want one whole bar", live.takeBeats)
	}
}

func TestRecordPassActsHalfASecondAheadAtAnyTempo(t *testing.T) {
	t.Parallel()

	// One beat is a full second at 60 BPM but a third of one at 180 BPM, while
	// the time a command needs to reach Live stays the same.
	live := newFakeRecorder()
	live.tempo = 180
	var offAt float64
	spy := &sendSpy{fakeRecorder: live, on: func(address string, args []interface{}) {
		if address == "/live/song/set/session_record" {
			if v, _ := asTestInt(args[0]); v == 0 && offAt == 0 {
				offAt = live.songTime
			}
		}
	}}
	take, err := recordResampledPass(spy, live.deps(), recordPlan{TrackName: "Measure", Spans: []recordSpan{{Bars: 2}}})
	if err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}
	end := take.WindowStart + take.WindowBeats
	if lead := end - offAt; lead < 1.5 {
		t.Errorf("session record was switched off %.2f beats before the window's end; at 180 BPM half a second is 1.5 beats", lead)
	}
	if len(live.takeBeats) != 1 || live.takeBeats[0] != 8 {
		t.Errorf("takes = %v, want exactly two bars", live.takeBeats)
	}
}

// sendSpy reports every Send before passing it on.
type sendSpy struct {
	*fakeRecorder
	on func(address string, args []interface{})
}

func (s *sendSpy) Send(address string, args ...interface{}) error {
	s.on(address, args)
	return s.fakeRecorder.Send(address, args...)
}

func TestRecordPassLeavesWhatIsPlayingAlone(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	if _, err := recordResampledPass(live, live.deps(), recordPlan{TrackName: "Measure", Spans: []recordSpan{{Bars: 1}}}); err != nil {
		t.Fatalf("recordResampledPass() error = %v", err)
	}
	// Setting back_to_arranger presses Live's "Back to Arrangement" button, which
	// stops every Session clip: the very sound a scene-less pass is there to record.
	for _, address := range []string{"/live/song/set/back_to_arranger", "/live/song/stop_all_clips", "/live/song/stop_playing"} {
		if indexOf(live.addresses(), address) >= 0 {
			t.Errorf("%s was sent; a pass without a scene must not stop what is playing", address)
		}
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
	if live.recStatus != 0 || live.armedTrack != -1 || live.quantization != 7 {
		t.Errorf("left behind: record status=%d armed=%d quantization=%d; want all restored", live.recStatus, live.armedTrack, live.quantization)
	}
}

func TestRecordPassReportsARecordingThatNeverAppeared(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.sendErr = map[string]error{} // arming "succeeds" but never takes: Live answers arm=false
	refuses := &armRefusingRecorder{fakeRecorder: live}
	_, err := recordResampledPass(refuses, live.deps(), recordPlan{TrackName: "Measure", Spans: []recordSpan{{Bars: 1}}})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "recording_missing" {
		t.Fatalf("error = %v, want recording_missing", err)
	}
}

// armRefusingRecorder swallows every attempt to arm a track.
type armRefusingRecorder struct{ *fakeRecorder }

func (a *armRefusingRecorder) Send(address string, args ...interface{}) error {
	if address == "/live/track/set/arm" {
		return nil
	}
	return a.fakeRecorder.Send(address, args...)
}

func TestRecordPassValidatesThePlan(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	for name, plan := range map[string]recordPlan{
		"no spans":       {TrackName: "Measure"},
		"zero bars":      {TrackName: "Measure", Spans: []recordSpan{{Bars: 0}}},
		"too many bars":  {TrackName: "Measure", Spans: []recordSpan{{Bars: 65}}},
		"negative scene": {TrackName: "Measure", Spans: []recordSpan{{SceneIndex: scene(-1), Bars: 1}}},
		"missing scene":  {TrackName: "Measure", Spans: []recordSpan{{SceneIndex: scene(3), Bars: 1}}}, // the set has rows 0-2
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
