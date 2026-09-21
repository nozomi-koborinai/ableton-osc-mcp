package tools

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeAuditionLive is the fake Live of the record engine tests (song time that
// only moves when the engine sleeps, commands that need a moment to land) with
// a mixer, devices and playing clips on top.
//
// What it adds to the model of Live:
//   - A clip fire or a track stop lands on the next bar line (1-bar
//     quantization). While the transport is stopped it lands at once, and a
//     fire starts the transport.
//   - A fader or a device switch takes effect the moment it is sent. A device
//     is switched with its first parameter, "Device On": Device.is_active is
//     read-only in Live, and writing to it changes nothing.
//   - A new track is appended and becomes the selected track.
//   - The listener can stop the transport (stopAt): song time freezes there.
type fakeAuditionLive struct {
	*fakeRecorder
	mixer         *fakeMixer
	playing       map[int]int          // track -> slot now playing (-1: nothing)
	pending       map[float64][][2]int // bar line -> {track, slot} launches and stops waiting for it
	devices       map[[2]int]bool
	selectedTrack int
	stopAt        float64 // the listener stops the transport at this beat (0: never)
	timeline      []auditionEvent
}

type auditionEvent struct {
	at      float64
	address string
	args    []interface{}
}

func newFakeAuditionLive() *fakeAuditionLive {
	rec := newFakeRecorder()
	rec.trackNames = []string{"808", "Chords", "Lead"}
	rec.hasClip = map[int][]bool{0: {true, true, true}, 1: {true, false, false}, 2: {false, false, false}}
	return &fakeAuditionLive{
		fakeRecorder: rec,
		mixer:        &fakeMixer{volumes: []float64{0.85, 0.6, 0.85}}, // 0.0 dB, -10.0 dB, 0.0 dB
		playing:      map[int]int{0: 0, 1: 0, 2: -1},
		pending:      map[float64][][2]int{},
		devices:      map[[2]int]bool{{1, 0}: true, {1, 1}: true},
	}
}

func (f *fakeAuditionLive) advance(d float64) {
	if !f.isPlaying {
		return
	}
	target := f.songTime + d
	if f.stopAt > 0 && target >= f.stopAt {
		target = f.stopAt
		defer func() { f.isPlaying = false }()
	}
	for {
		bar := f.nextBar()
		if bar > target+barLineTolerance {
			break
		}
		f.songTime = bar
		f.crossBar(bar)
		for _, launch := range f.pending[bar] {
			f.playing[launch[0]] = launch[1]
		}
		delete(f.pending, bar)
	}
	f.songTime = math.Max(f.songTime, target) // never back behind a line just crossed
}

func (f *fakeAuditionLive) sleeper() auditionSleeper {
	return func(d time.Duration) { f.advance(d.Seconds() * f.tempo / 60) }
}

func (f *fakeAuditionLive) Query(address string, args ...interface{}) ([]interface{}, error) {
	intArg := func(i int) int { v, _ := asTestInt(args[i]); return v }
	switch address {
	case "/live/song/get/num_tracks":
		return []interface{}{int32(len(f.trackNames))}, nil
	case "/live/view/get/selected_track":
		return []interface{}{int32(f.selectedTrack)}, nil
	case "/live/clip_slot/get/has_clip":
		t, s := intArg(0), intArg(1)
		has := t < len(f.trackNames) && s < len(f.hasClip[t]) && f.hasClip[t][s]
		return []interface{}{int32(t), int32(s), has}, nil
	case "/live/track/get/num_devices":
		t, n := intArg(0), 0
		for key := range f.devices {
			if key[0] == t {
				n++
			}
		}
		return []interface{}{int32(t), int32(n)}, nil
	case "/live/track/get/playing_slot_index":
		return []interface{}{int32(intArg(0)), int32(f.playing[intArg(0)])}, nil
	case "/live/device/get/parameter/value":
		key := [2]int{intArg(0), intArg(1)}
		if intArg(2) != 0 {
			return nil, errors.New("the fake only knows parameter 0, Device On")
		}
		return []interface{}{int32(key[0]), int32(key[1]), int32(0), float32(boolInt32(f.devices[key]))}, nil
	case "/live/track/get/volume_db", "/live/track/get/volume_for_db", "/live/song/get/track_volumes_db":
		return f.mixer.Query(address, args...)
	}
	return f.fakeRecorder.Query(address, args...)
}

func (f *fakeAuditionLive) Send(address string, args ...interface{}) error {
	if err := f.sendErr[address]; err != nil {
		return err
	}
	f.timeline = append(f.timeline, auditionEvent{at: f.songTime, address: address, args: append([]interface{}{}, args...)})
	intArg := func(i int) int { v, _ := asTestInt(args[i]); return v }
	switch address {
	case "/live/clip_slot/fire":
		if !f.isPlaying {
			f.isPlaying, f.playing[intArg(0)] = true, intArg(1)
			return nil
		}
		bar := f.barForCommand()
		f.pending[bar] = append(f.pending[bar], [2]int{intArg(0), intArg(1)})
		return nil
	case "/live/track/stop_all_clips":
		if !f.isPlaying {
			f.playing[intArg(0)] = -1
			return nil
		}
		bar := f.barForCommand()
		f.pending[bar] = append(f.pending[bar], [2]int{intArg(0), -1})
		return nil
	case "/live/song/stop_playing":
		f.isPlaying = false
		return nil
	case "/live/view/set/selected_track":
		f.selectedTrack = intArg(0)
		return nil
	case "/live/track/set/volume":
		return f.mixer.Send(address, args...)
	case "/live/device/set/parameter/value":
		if intArg(2) == 0 {
			f.devices[[2]int{intArg(0), intArg(1)}] = args[3].(float32) >= 0.5
		}
		return nil
	case "/live/device/set/is_active":
		return nil // Live: "can't set attribute". Device.is_active is read-only, so nothing happens.
	case "/live/track/set/name":
		f.trackNames[intArg(0)] = args[1].(string)
		return nil
	case "/live/song/create_audio_track":
		f.trackNames = append(f.trackNames, "4-Audio")
		f.mixer.volumes = append(f.mixer.volumes, 0.85)
		f.selectedTrack = len(f.trackNames) - 1
		return nil
	}
	return f.fakeRecorder.Send(address, args...)
}

func (f *fakeAuditionLive) events(address string) []auditionEvent {
	var out []auditionEvent
	for _, e := range f.timeline {
		if e.address == address {
			out = append(out, e)
		}
	}
	return out
}

// indicatorName is the name of the last track, where the engine puts its indicator.
func (f *fakeAuditionLive) indicatorName() string {
	return f.trackNames[len(f.trackNames)-1]
}

// The three variants most tests use: X changes nothing, B swaps the 808 clip
// and turns the chords down 6 dB, C switches the chords' first device off.
func testVariants() []AuditionVariant {
	return []AuditionVariant{
		{Label: "X", Description: "as it is"},
		{Label: "B", Description: "808 bounce, chords -6", Clips: []AuditionClip{{TrackIndex: 0, ClipIndex: 1}}, Mix: []AuditionMix{{TrackIndex: 1, DeltaDB: -6}}},
		{Label: "C", Description: "chords without the chorus", Devices: []AuditionDevice{{TrackIndex: 1, DeviceIndex: 0, Active: false}}},
	}
}

func TestAuditionPlaysEachVariantFromTheBaselineAndRestores(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	type heard struct {
		name   string
		clip   int
		chords string
		device bool
	}
	var during []heard
	spy := &renameSpy{fakeAuditionLive: live, onRename: func(name string) {
		during = append(during, heard{name, live.playing[0], fakeFaderDisplay(live.mixer.volumes[1]), live.devices[[2]int{1, 0}]})
	}}

	got, err := runAudition(spy, live.sleeper(), AuditionInput{Variants: testVariants(), BarsPerVariant: 2})
	if err != nil {
		t.Fatalf("runAudition() error = %v", err)
	}

	want := []heard{
		{"Audition", 0, "-10.0 dB", true}, // the new indicator track gets its name
		{"Audition ▶ X: as it is", 0, "-10.0 dB", true},
		{"Audition ▶ B: 808 bounce, chords -6", 1, "-16.0 dB", true},
		{"Audition ▶ C: chords without the chorus", 0, "-10.0 dB", false}, // B's clip and fader are gone again
		{"Audition", 0, "-10.0 dB", true},
	}
	if !reflect.DeepEqual(during, want) {
		t.Errorf("heard\n  %+v\nwant\n  %+v", during, want)
	}
	if !got.Restored || got.Committed != nil || live.quantization != 7 {
		t.Errorf("restored=%v committed=%v quantization=%d; want restored, nothing committed, quantization back to 7", got.Restored, got.Committed, live.quantization)
	}
	// From beat 1.5 the first bar line is 4 (bar 2); two bars each after that.
	var bars []int
	var labels []string
	for _, p := range got.Played {
		bars, labels = append(bars, p.StartBar), append(labels, p.Label)
	}
	if !reflect.DeepEqual(labels, []string{"X", "B", "C"}) || !reflect.DeepEqual(bars, []int{2, 4, 6}) {
		t.Errorf("played %v at bars %v, want [X B C] at [2 4 6]", labels, bars)
	}
	if got.DurationSec != 12 || got.Prompt == "" {
		t.Errorf("duration = %v s, prompt = %q; want 12 s (three variants of two bars at 120 BPM)", got.DurationSec, got.Prompt)
	}
}

// renameSpy reports every rename of the indicator track, after it has happened.
type renameSpy struct {
	*fakeAuditionLive
	onRename func(name string)
}

func (s *renameSpy) Send(address string, args ...interface{}) error {
	err := s.fakeAuditionLive.Send(address, args...)
	if address == "/live/track/set/name" && err == nil {
		s.onRename(args[1].(string))
	}
	return err
}

func TestAuditionFiresClipsAheadOfTheLineAndMovesFadersJustBeforeIt(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	if _, err := runAudition(live, live.sleeper(), AuditionInput{Variants: testVariants(), Play: []string{"X", "B"}, BarsPerVariant: 2}); err != nil {
		t.Fatalf("runAudition() error = %v", err)
	}
	// B starts on beat 12 (X ran from 4). A fire sent after the line would land a bar late.
	fires := live.events("/live/clip_slot/fire")
	if len(fires) == 0 || fires[0].at > 12-commandLatencyBeats || fires[0].at < 10 {
		t.Errorf("first clip fire at beat %v, want early enough to land on bar line 12: %+v", fires[0].at, fires)
	}
	// A fader moves the moment it is told to. It goes out a moment before the
	// line, so that the downbeat already sounds like the new variant.
	volumes := live.events("/live/track/set/volume")
	if len(volumes) == 0 || volumes[0].at < 11.5 || volumes[0].at >= 12 {
		t.Errorf("first fader move at beat %v, want just ahead of bar line 12", volumes[0].at)
	}
	if names := live.events("/live/track/set/name"); len(names) < 3 || names[2].at < 12-1e-3 || names[2].at > 12.1 {
		t.Errorf("renames = %+v, want the label to change to B on bar line 12", names)
	}
}

func TestAuditionPlaysOnlyWhatIsAskedFor(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	got, err := runAudition(live, live.sleeper(), AuditionInput{Variants: testVariants(), Play: []string{"C", "B", "C"}, BarsPerVariant: 1})
	if err != nil {
		t.Fatalf("runAudition() error = %v", err)
	}
	var labels []string
	for _, p := range got.Played {
		labels = append(labels, p.Label)
	}
	if !reflect.DeepEqual(labels, []string{"C", "B", "C"}) {
		t.Errorf("played %v, want [C B C]", labels)
	}
	if live.playing[0] != 0 || !live.devices[[2]int{1, 0}] || fakeFaderDisplay(live.mixer.volumes[1]) != "-10.0 dB" {
		t.Errorf("after the audition: clip %d, device %v, chords %s; want the baseline back", live.playing[0], live.devices[[2]int{1, 0}], fakeFaderDisplay(live.mixer.volumes[1]))
	}
}

func TestAuditionCommitWritesTheChoiceInWithoutPlayingOrRestoring(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	got, err := runAudition(live, live.sleeper(), AuditionInput{Variants: testVariants(), Commit: "B"})
	if err != nil {
		t.Fatalf("runAudition() error = %v", err)
	}
	if live.playing[0] != 1 || fakeFaderDisplay(live.mixer.volumes[1]) != "-16.0 dB" {
		t.Errorf("after commit: clip %d, chords %s; want B left in place", live.playing[0], fakeFaderDisplay(live.mixer.volumes[1]))
	}
	if got.Restored || got.Committed == nil || got.Committed.Label != "B" || len(got.Played) != 0 {
		t.Fatalf("got = %+v, want B committed, nothing played, nothing restored", got)
	}
	if len(got.Committed.Mix) != 1 || got.Committed.Mix[0].VolumeDB != "-16.0 dB" {
		t.Errorf("committed mix = %+v, want the chords at -16.0 dB", got.Committed.Mix)
	}
	if len(live.trackNames) != 3 {
		t.Errorf("tracks = %v; a commit plays nothing, so it needs no indicator track", live.trackNames)
	}
}

func TestAuditionChecksEverythingBeforeTouchingLive(t *testing.T) {
	t.Parallel()

	two := testVariants()[:2]
	cases := map[string]struct {
		input AuditionInput
		code  string
	}{
		"one variant":         {AuditionInput{Variants: two[:1]}, "invalid_audition"},
		"nine variants":       {AuditionInput{Variants: append(append(append([]AuditionVariant{}, testVariants()...), testVariants()...), testVariants()...)}, "invalid_audition"},
		"duplicate label":     {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "x", Description: "again"}}}, "invalid_audition"},
		"empty description":   {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B"}}}, "invalid_audition"},
		"unknown play label":  {AuditionInput{Variants: two, Play: []string{"Q"}}, "invalid_audition"},
		"play and commit":     {AuditionInput{Variants: two, Play: []string{"X"}, Commit: "B"}, "invalid_audition"},
		"too many bars":       {AuditionInput{Variants: two, BarsPerVariant: 33}, "invalid_audition"},
		"too many plays":      {AuditionInput{Variants: two, Play: strings.Split(strings.Repeat("X,", 17)+"X", ",")}, "invalid_audition"},
		"negative index":      {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B", Description: "d", Clips: []AuditionClip{{TrackIndex: -1, ClipIndex: 0}}}}}, "invalid_audition"},
		"same clip track":     {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B", Description: "d", Clips: []AuditionClip{{TrackIndex: 0, ClipIndex: 1}, {TrackIndex: 0, ClipIndex: 2}}}}}, "invalid_audition"},
		"same device twice":   {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B", Description: "d", Devices: []AuditionDevice{{TrackIndex: 1, DeviceIndex: 0}, {TrackIndex: 1, DeviceIndex: 0, Active: true}}}}}, "invalid_audition"},
		"no such scene":       {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B", Description: "d", Clips: []AuditionClip{{TrackIndex: 0, ClipIndex: 7}}}}}, "audition_target_missing"},
		"same track twice":    {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B", Description: "d", Mix: []AuditionMix{{TrackIndex: 1, DeltaDB: -1}, {TrackIndex: 1, DeltaDB: -2}}}}}, "invalid_audition"},
		"delta too large":     {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B", Description: "d", Mix: []AuditionMix{{TrackIndex: 1, DeltaDB: -30}}}}}, "invalid_audition"},
		"no clip in the slot": {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B", Description: "d", Clips: []AuditionClip{{TrackIndex: 1, ClipIndex: 2}}}}}, "audition_target_missing"},
		"no such device":      {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B", Description: "d", Devices: []AuditionDevice{{TrackIndex: 0, DeviceIndex: 0}}}}}, "audition_target_missing"},
		"no such track":       {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B", Description: "d", Mix: []AuditionMix{{TrackIndex: 9, DeltaDB: -1}}}}}, "audition_target_missing"},
		"beyond the fader":    {AuditionInput{Variants: []AuditionVariant{two[0], {Label: "B", Description: "d", Mix: []AuditionMix{{TrackIndex: 0, DeltaDB: 12}}}}}, "level_out_of_range"},
	}
	for name, tc := range cases {
		live := newFakeAuditionLive()
		_, err := runAudition(live, live.sleeper(), tc.input)
		var actionableErr *ActionableError
		if !errors.As(err, &actionableErr) || actionableErr.Code != tc.code {
			t.Errorf("%s: error = %v, want %s", name, err, tc.code)
		}
		if len(live.timeline) != 0 {
			t.Errorf("%s: Live was touched: %v", name, live.timeline)
		}
	}
}

func TestAuditionRefusesADeltaFromASilentFader(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	live.mixer.volumes[1] = 0
	_, err := runAudition(live, live.sleeper(), AuditionInput{Variants: testVariants()})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "delta_from_silence" {
		t.Fatalf("error = %v, want delta_from_silence", err)
	}
}

func TestAuditionInDBNeedsTheMixerPatch(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	live.mixer.noPatch = true
	_, err := runAudition(live, live.sleeper(), AuditionInput{Variants: testVariants()})
	if !isMixerDBPatchMissing(err) {
		t.Errorf("error = %v, want mixer_db_patch_missing", err)
	}
	if len(live.timeline) != 0 {
		t.Errorf("Live was touched: %v", live.timeline)
	}
}

func TestAuditionPutsLiveBackWhenAStepFails(t *testing.T) {
	t.Parallel()

	// C moves the 808 fader and then switches a device. The switch fails: by then
	// half of C is in place, on top of what is left of B.
	variants := testVariants()
	variants[2].Mix = []AuditionMix{{TrackIndex: 0, DeltaDB: -3}}
	live := newFakeAuditionLive()
	failing := &failOnce{fakeAuditionLive: live, address: "/live/device/set/parameter/value"}
	_, err := runAudition(failing, live.sleeper(), AuditionInput{Variants: variants, BarsPerVariant: 1})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "audition_failed" || !strings.Contains(actionableErr.Message, "variant C") {
		t.Fatalf("error = %v, want audition_failed naming variant C", err)
	}
	// No waiting here: "back where they were" has to be true the moment the tool
	// returns, or the next audition would take the variant's clip for the baseline.
	if live.playing[0] != 0 || !live.devices[[2]int{1, 0}] {
		t.Errorf("after the failure: clip %d, device %v; want the baseline back", live.playing[0], live.devices[[2]int{1, 0}])
	}
	if bass, chords := fakeFaderDisplay(live.mixer.volumes[0]), fakeFaderDisplay(live.mixer.volumes[1]); bass != "0.0 dB" || chords != "-10.0 dB" {
		t.Errorf("after the failure: 808 %s, chords %s; want 0.0 dB and -10.0 dB", bass, chords)
	}
	if live.indicatorName() != "Audition" || live.quantization != 7 {
		t.Errorf("indicator %q, quantization %d; want both put back", live.indicatorName(), live.quantization)
	}
}

// failOnce makes the first Send to one address fail.
type failOnce struct {
	*fakeAuditionLive
	address string
	done    bool
}

func (f *failOnce) Send(address string, args ...interface{}) error {
	if address == f.address && !f.done {
		f.done = true
		return fmt.Errorf("boom on %s", address)
	}
	return f.fakeAuditionLive.Send(address, args...)
}

func TestAuditionDefaultsToEightBars(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	got, err := runAudition(live, live.sleeper(), AuditionInput{Variants: testVariants(), Play: []string{"X"}})
	if err != nil {
		t.Fatalf("runAudition() error = %v", err)
	}
	if got.BarsPerVariant != 8 || got.DurationSec != 16 || got.TempoBPM != 120 {
		t.Errorf("bars = %d, duration = %v s at %v BPM; want 8 bars, 16 s at 120", got.BarsPerVariant, got.DurationSec, got.TempoBPM)
	}
}

func TestAuditionNeverLeavesItsIndicatorSelected(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	live.selectedTrack = 1
	if _, err := runAudition(live, live.sleeper(), AuditionInput{Variants: testVariants(), Play: []string{"X"}, BarsPerVariant: 1}); err != nil {
		t.Fatalf("runAudition() error = %v", err)
	}
	if len(live.trackNames) != 4 || live.indicatorName() != "Audition" {
		t.Fatalf("tracks = %v, want an Audition track added at the end", live.trackNames)
	}
	// Live selects a track it has just created; the listener's selection comes back.
	if live.selectedTrack != 1 {
		t.Errorf("selected track = %d, want the listener's track 1", live.selectedTrack)
	}
}

func TestAuditionReusesItsIndicatorAndLeavesLookalikesAlone(t *testing.T) {
	t.Parallel()

	for _, extra := range [][]string{
		{"Audition vox", "Audition"},
		{"Audition", "Audition vox"},
		{"Audition ▶ J: left over from a run that died", "Audition vox"},
	} {
		live := newFakeAuditionLive()
		live.trackNames = append(live.trackNames, extra...)
		live.mixer.volumes = append(live.mixer.volumes, 0.85, 0.85)
		if _, err := runAudition(live, live.sleeper(), AuditionInput{Variants: testVariants(), Play: []string{"B"}, BarsPerVariant: 1}); err != nil {
			t.Fatalf("runAudition() error = %v", err)
		}
		// The listener's own "Audition vox" keeps its name, and no second indicator appears.
		want := []string{"Audition vox", "Audition"}
		if extra[1] == "Audition vox" {
			want = []string{"Audition", "Audition vox"}
		}
		if got := live.trackNames[3:]; !reflect.DeepEqual(got, want) {
			t.Errorf("from %q: tracks = %q, want %q", extra, got, want)
		}
	}
}

func TestAuditionCutsALongDescriptionInTheIndicator(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	variants := testVariants()
	variants[1].Description = strings.Repeat("ブ", 60)
	var shown []string
	spy := &renameSpy{fakeAuditionLive: live, onRename: func(name string) { shown = append(shown, name) }}
	if _, err := runAudition(spy, live.sleeper(), AuditionInput{Variants: variants, Play: []string{"B"}, BarsPerVariant: 1}); err != nil {
		t.Fatalf("runAudition() error = %v", err)
	}
	want := "Audition ▶ B: " + strings.Repeat("ブ", 39) + "…"
	if len(shown) != 3 || shown[1] != want {
		t.Errorf("shown = %q, want the second to be %q", shown, want)
	}
}

func TestAuditionGivesUpAtOnceWhenTheListenerStopsTheTransport(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	live.stopAt = 14 // two beats into B
	_, err := runAudition(live, live.sleeper(), AuditionInput{Variants: testVariants(), BarsPerVariant: 2})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "audition_interrupted" || !strings.Contains(actionableErr.Message, "B") {
		t.Fatalf("error = %v, want audition_interrupted naming variant B", err)
	}
	if live.playing[0] != 0 || fakeFaderDisplay(live.mixer.volumes[1]) != "-10.0 dB" || !live.devices[[2]int{1, 0}] {
		t.Errorf("after the stop: clip %d, chords %s, device %v; want the baseline back", live.playing[0], fakeFaderDisplay(live.mixer.volumes[1]), live.devices[[2]int{1, 0}])
	}
	// Putting a clip back starts a stopped transport; the listener wanted it stopped.
	if live.isPlaying || live.indicatorName() != "Audition" || live.quantization != 7 {
		t.Errorf("playing=%v indicator=%q quantization=%d; want stopped, Audition, 7", live.isPlaying, live.indicatorName(), live.quantization)
	}
}

func TestAuditionStartsAStoppedTransportAndCanStopItAgain(t *testing.T) {
	t.Parallel()

	live := newFakeAuditionLive()
	live.isPlaying = false
	got, err := runAudition(live, live.sleeper(), AuditionInput{Variants: testVariants(), Play: []string{"X"}, BarsPerVariant: 1, StopAfter: true})
	if err != nil {
		t.Fatalf("runAudition() error = %v", err)
	}
	if !got.PlaybackStarted || live.isPlaying {
		t.Errorf("playback_started=%v, still playing=%v; want started by the tool and stopped after", got.PlaybackStarted, live.isPlaying)
	}
}
