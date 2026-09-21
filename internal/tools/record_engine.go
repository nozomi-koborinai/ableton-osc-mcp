package tools

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	recordMaxBars        = 64
	recordLeadSeconds    = 0.5 // act this long ahead of a bar line; 1-bar quantization lands it on the line
	recordCheckBeats     = 0.5 // how far into the window to check that Live really is recording
	recordFileSettleWait = 150 * time.Millisecond
	recordFileSettleMax  = 20
)

type recordClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

// recordSpan is one stretch of a pass: fire a scene (or keep what is playing)
// and let it run for Bars.
type recordSpan struct {
	SceneIndex *int
	Bars       int
}

type recordPlan struct {
	TrackName       string
	Spans           []recordSpan
	StopClipsAtEnds bool // bounce: Back to Arrangement and stop all clips before the pass, stop all clips after
}

// recordedTake describes what a pass produced and when, in song time.
type recordedTake struct {
	TrackIndex    int
	RoutingType   string
	Slots         []int
	FilePaths     []string
	TempoBPM      float64
	BeatsPerBar   int
	RecordOnBeat  float64 // song time when Session Record was switched on
	WindowStart   float64 // first bar boundary after RecordOnBeat
	WindowBeats   float64 // length of all spans together
	RecordOffBeat float64 // song time when Session Record was switched off
}

type recordDeps struct {
	sleep    auditionSleeper
	fileSize func(path string) (int64, error)
}

// recordResampledPass records Live's master output onto an audio track whose
// input is Resampling, with the armed-track + Session Record mechanism.
//
// What Live 11.0.12 was observed to do, and what this therefore relies on:
//   - Session Record records into the armed track's slot in the *selected*
//     scene, and only if that slot has a stop button.
//   - Launching a scene reaches the armed track too: a stop button in the
//     launched row cancels the recording, and so does launching the recording
//     row itself. So the take lives in a row that is never launched, which
//     keeps its stop button, while the track's other rows give theirs up.
//   - Recording starts on the bar after Session Record goes on and ends on the
//     bar after it goes off, so the file is exactly the window, bar to bar.
//     Disarming before that last bar cuts the take short.
//   - Session Record records on every armed track, and Live arms a MIDI track by
//     itself when it is selected. Left alone, the listener's armed tracks would
//     get stray clips and stop playing, so they are disarmed for the pass and
//     re-armed afterwards.
func recordResampledPass(c recordClient, deps recordDeps, plan recordPlan) (take recordedTake, err error) {
	if len(plan.Spans) == 0 {
		return recordedTake{}, errors.New("at least one span is required")
	}
	launched := map[int]bool{}
	for _, span := range plan.Spans {
		if span.Bars < 1 || span.Bars > recordMaxBars {
			return recordedTake{}, fmt.Errorf("bars must be between 1 and %d", recordMaxBars)
		}
		if span.SceneIndex != nil {
			if *span.SceneIndex < 0 {
				return recordedTake{}, fmt.Errorf("invalid scene_index: %d", *span.SceneIndex)
			}
			launched[*span.SceneIndex] = true
		}
	}

	tempo, err := queryAuditionTempo(c)
	if err != nil {
		return recordedTake{}, err
	}
	beatsPerBar, err := queryAuditionBeatsPerBar(c)
	if err != nil {
		return recordedTake{}, err
	}
	// A command needs the same time to reach Live at any tempo, so the lead is
	// set in seconds: at least a beat, and never the whole bar.
	leadBeats := math.Min(math.Max(1, recordLeadSeconds*tempo/60), float64(beatsPerBar)-0.5)
	if plan.Spans[0].SceneIndex == nil {
		playing, err := queryAuditionIsPlaying(c)
		if err != nil {
			return recordedTake{}, err
		}
		if !playing {
			return recordedTake{}, actionable("nothing_playing",
				"nothing is playing, so there is nothing to record",
				"Pass scene_index to fire a scene for the pass, or start playback first.")
		}
	}

	// Refuse a scene that does not exist before anything in the set is touched.
	numScenes, err := queryNumScenes(c)
	if err != nil {
		return recordedTake{}, err
	}
	for sceneIndex := range launched {
		if sceneIndex >= numScenes {
			return recordedTake{}, fmt.Errorf("invalid scene_index: %d (the set has %d scenes)", sceneIndex, numScenes)
		}
	}

	trackIndex, err := ensureNamedAudioTrack(c, plan.TrackName)
	if err != nil {
		return recordedTake{}, err
	}
	routing, err := pickResamplingRouting(c, trackIndex)
	if err != nil {
		return recordedTake{}, err
	}
	occupied, err := occupiedSlots(c, trackIndex)
	if err != nil {
		return recordedTake{}, err
	}
	// The last row that is neither launched nor taken; a new scene if there is none.
	row := -1
	for i := len(occupied) - 1; i >= 0; i-- {
		if !launched[i] && !occupied[i] {
			row = i
			break
		}
	}
	if row < 0 {
		if err := c.Send("/live/song/create_scene", int32(-1)); err != nil {
			return recordedTake{}, err
		}
		deps.sleep(200 * time.Millisecond)
		row = len(occupied)
		occupied = append(occupied, false)
	}

	prevQuant, err := queryClipTriggerQuantization(c)
	if err != nil {
		return recordedTake{}, err
	}
	prevSelected := -1
	if res, err := c.Query("/live/view/get/selected_scene"); err == nil && len(res) > 0 {
		if v, err := abletonosc.AsInt(res[0]); err == nil {
			prevSelected = v
		}
	}

	othersArmed, err := armedTracksExcept(c, trackIndex)
	if err != nil {
		return recordedTake{}, err
	}
	stopButtons, err := trackStopButtons(c, trackIndex, len(occupied))
	if err != nil {
		return recordedTake{}, err
	}
	var stopButtonsChanged []int

	armed, recording := false, false
	defer func() {
		if recording {
			_ = c.Send("/live/song/set/session_record", int32(0))
			_ = waitForRecordStatus(c, deps, 0, tempo, beatsPerBar)
		}
		if armed {
			_ = c.Send("/live/track/set/arm", int32(trackIndex), int32(0))
		}
		for _, track := range othersArmed {
			_ = c.Send("/live/track/set/arm", int32(track), int32(1))
		}
		// The track goes back to how scene launches treated it before the pass:
		// a take that is kept must stop again when another scene is launched.
		for _, slot := range stopButtonsChanged {
			_ = c.Send("/live/clip_slot/set/has_stop_button", int32(trackIndex), int32(slot), boolInt32(stopButtons[slot]))
		}
		_ = c.Send("/live/song/set/clip_trigger_quantization", int32(prevQuant))
		if prevSelected >= 0 {
			_ = c.Send("/live/view/set/selected_scene", int32(prevSelected))
		}
	}()

	// Monitoring In and muted: Resampling carries the master without feeding back.
	setup := [][]interface{}{
		{"/live/track/set/input_routing_type", int32(trackIndex), routing},
		{"/live/track/set/current_monitoring_state", int32(trackIndex), int32(0)},
		{"/live/track/set/mute", int32(trackIndex), int32(1)},
		{"/live/song/set/clip_trigger_quantization", int32(auditionBarQuantization)},
	}
	for i := range occupied {
		if want := i == row; want != stopButtons[i] {
			stopButtonsChanged = append(stopButtonsChanged, i)
			setup = append(setup, []interface{}{"/live/clip_slot/set/has_stop_button", int32(trackIndex), int32(i), boolInt32(want)})
		}
	}
	for _, track := range othersArmed {
		setup = append(setup, []interface{}{"/live/track/set/arm", int32(track), int32(0)})
	}
	for _, step := range setup {
		if err := c.Send(step[0].(string), step[1:]...); err != nil {
			return recordedTake{}, err
		}
	}
	if plan.StopClipsAtEnds {
		// A bounce starts from a clean slate. Setting back_to_arranger presses
		// Live's "Back to Arrangement" button, which stops every Session clip, so
		// it has no place in a pass that records what is already playing.
		if err := c.Send("/live/song/set/back_to_arranger", int32(0)); err != nil {
			return recordedTake{}, err
		}
		if err := c.Send("/live/song/stop_all_clips"); err != nil {
			return recordedTake{}, err
		}
		deps.sleep(300 * time.Millisecond)
	}
	if plan.Spans[0].SceneIndex != nil {
		if _, err := ensureAuditionPlayback(c, false); err != nil {
			return recordedTake{}, err
		}
	}

	if err := c.Send("/live/track/set/arm", int32(trackIndex), int32(1)); err != nil {
		return recordedTake{}, err
	}
	armed = true
	if err := waitForArm(c, deps, trackIndex); err != nil {
		return recordedTake{}, err
	}
	if err := c.Send("/live/view/set/selected_scene", int32(row)); err != nil {
		return recordedTake{}, err
	}
	// Commands take a moment to reach Live. Sent just before a bar line they
	// land after it and wait a whole bar more, so let a close bar line go by.
	recordOn, err := queryCurrentSongTime(c)
	if err != nil {
		return recordedTake{}, err
	}
	if next := ceilBarBeat(recordOn, beatsPerBar); next-recordOn < leadBeats {
		if err := waitUntilSongTime(c, deps.sleep, next, tempo); err != nil {
			return recordedTake{}, err
		}
		if recordOn, err = queryCurrentSongTime(c); err != nil {
			return recordedTake{}, err
		}
	}
	if err := c.Send("/live/song/set/session_record", int32(1)); err != nil {
		return recordedTake{}, err
	}
	recording = true

	// Session Record and the first scene wait for the same bar line.
	windowStart := ceilBarBeat(recordOn, beatsPerBar)
	boundary := windowStart
	for i, span := range plan.Spans {
		if span.SceneIndex != nil {
			if i > 0 {
				if err := waitUntilSongTime(c, deps.sleep, boundary-leadBeats, tempo); err != nil {
					return recordedTake{}, err
				}
			}
			if err := c.Send("/live/scene/fire", int32(*span.SceneIndex)); err != nil {
				return recordedTake{}, fmt.Errorf("fire scene %d: %w", *span.SceneIndex, err)
			}
		}
		if i == 0 {
			// Fail now rather than after the whole pass if Live did not start recording.
			if err := waitUntilSongTime(c, deps.sleep, windowStart+recordCheckBeats, tempo); err != nil {
				return recordedTake{}, err
			}
			if status, err := queryRecordStatus(c); err != nil {
				return recordedTake{}, err
			} else if status != 1 {
				now, _ := queryCurrentSongTime(c)
				return recordedTake{}, recordingMissing(plan.TrackName,
					fmt.Sprintf("session record status is %d at beat %.2f; record went on at beat %.2f and should have started on beat %.0f", status, now, recordOn, windowStart))
			}
		}
		boundary += float64(span.Bars * beatsPerBar)
	}

	// Switch off inside the last bar: the recording then ends on the window's end.
	if err := waitUntilSongTime(c, deps.sleep, boundary-leadBeats, tempo); err != nil {
		return recordedTake{}, err
	}
	if err := c.Send("/live/song/set/session_record", int32(0)); err != nil {
		return recordedTake{}, err
	}
	if err := waitForRecordStatus(c, deps, 0, tempo, beatsPerBar); err != nil {
		return recordedTake{}, err
	}
	recording = false
	if err := c.Send("/live/track/set/arm", int32(trackIndex), int32(0)); err != nil {
		return recordedTake{}, err
	}
	armed = false
	if plan.StopClipsAtEnds {
		_ = c.Send("/live/song/stop_all_clips")
	}

	after, err := occupiedSlots(c, trackIndex)
	if err != nil {
		return recordedTake{}, err
	}
	take = recordedTake{
		TrackIndex: trackIndex, RoutingType: routing, TempoBPM: tempo, BeatsPerBar: beatsPerBar,
		RecordOnBeat: recordOn, WindowStart: windowStart, WindowBeats: boundary - windowStart, RecordOffBeat: boundary,
	}
	for slot, has := range after {
		if has && (slot >= len(occupied) || !occupied[slot]) {
			take.Slots = append(take.Slots, slot)
		}
	}
	if len(take.Slots) == 0 {
		return recordedTake{}, recordingMissing(plan.TrackName, "the pass ran, but no new clip appeared on the track")
	}
	for _, slot := range take.Slots {
		// Once recording ends the new clip starts looping; it is muted, but stop it anyway.
		_ = c.Send("/live/clip/stop", int32(trackIndex), int32(slot))
		path, err := recordedFilePath(c, deps, trackIndex, slot)
		if err != nil {
			return take, err
		}
		take.FilePaths = append(take.FilePaths, path)
	}
	return take, nil
}

// trackStopButtons reports, per scene, whether the track's slot has a stop
// button. Rows Live has not told us about (a scene added a moment ago) count as
// having one, which is Live's default.
func trackStopButtons(c recordClient, trackIndex, rows int) ([]bool, error) {
	res, err := c.Query("/live/song/get/track_data", int32(trackIndex), int32(trackIndex+1), "clip_slot.has_stop_button")
	if err != nil {
		return nil, fmt.Errorf("read stop buttons: %w", err)
	}
	buttons := make([]bool, rows)
	for i := range buttons {
		buttons[i] = true
		if i < len(res) {
			if buttons[i], err = asBoolish(res[i]); err != nil {
				return nil, err
			}
		}
	}
	return buttons, nil
}

func boolInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// armedTracksExcept lists the armed tracks other than the recording track.
func armedTracksExcept(c recordClient, recordingTrack int) ([]int, error) {
	res, err := c.Query("/live/song/get/track_data", int32(0), int32(-1), "track.arm")
	if err != nil {
		return nil, fmt.Errorf("read arm states: %w", err)
	}
	var armed []int
	for track, v := range res {
		on, err := asBoolish(v)
		if err != nil {
			return nil, err
		}
		if on && track != recordingTrack {
			armed = append(armed, track)
		}
	}
	return armed, nil
}

func recordingMissing(trackName, detail string) error {
	return actionable("recording_missing",
		fmt.Sprintf("Live did not record anything on the %q track (%s)", trackName, detail),
		"Check that the track is an audio track Live can arm and that its input shows Resampling, then try again.")
}

func queryRecordStatus(c recordClient) (int, error) {
	res, err := c.Query("/live/song/get/session_record_status")
	if err != nil {
		return 0, fmt.Errorf("get session record status: %w", err)
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return 0, err
	}
	return abletonosc.AsInt(res[0])
}

// waitForRecordStatus polls until Session Record reports want. Switching it off
// only takes effect on the next bar line, so allow two bars and a little more.
func waitForRecordStatus(c recordClient, deps recordDeps, want int, tempo float64, beatsPerBar int) error {
	polls := int((2*float64(beatsPerBar)*60/tempo+2)/auditionPollInterval.Seconds()) + 1
	for i := 0; i < polls; i++ {
		status, err := queryRecordStatus(c)
		if err != nil {
			return err
		}
		if status == want {
			return nil
		}
		deps.sleep(auditionPollInterval)
	}
	return fmt.Errorf("timed out waiting for session record status %d", want)
}

// waitForArm waits until Live reports the track as armed; Session Record
// switched on before that records nothing.
func waitForArm(c recordClient, deps recordDeps, trackIndex int) error {
	for i := 0; i < 50; i++ {
		res, err := c.Query("/live/track/get/arm", int32(trackIndex))
		if err != nil {
			return err
		}
		if len(res) >= 2 {
			if on, err := asBoolish(res[1]); err == nil && on {
				return nil
			}
		}
		deps.sleep(auditionPollInterval)
	}
	return actionable("recording_missing",
		"Live would not arm the recording track",
		"Check that the track is an audio track and not frozen, then try again.")
}

// occupiedSlots reports, per scene, whether the track has a clip.
func occupiedSlots(c recordClient, trackIndex int) ([]bool, error) {
	res, err := c.Query("/live/song/get/track_data", int32(trackIndex), int32(trackIndex+1), "clip_slot.has_clip")
	if err != nil {
		return nil, fmt.Errorf("list clip slots: %w", err)
	}
	slots := make([]bool, 0, len(res))
	for _, v := range res {
		has, err := asBoolish(v)
		if err != nil {
			return nil, err
		}
		slots = append(slots, has)
	}
	return slots, nil
}

func recordedFilePath(c recordClient, deps recordDeps, trackIndex, slot int) (string, error) {
	unreadable := func(detail string) error {
		return actionable("recorded_file_unreadable", detail,
			"Save the Live set once (an unsaved set records into a temporary folder), then try again.")
	}
	res, err := c.Query("/live/clip/get/file_path", int32(trackIndex), int32(slot))
	if err != nil {
		return "", err
	}
	if len(res) < 3 || fmt.Sprint(res[2]) == "" {
		return "", unreadable("Live did not report a file for the recorded clip")
	}
	path := fmt.Sprint(res[2])
	// Live may still be flushing the file: wait until its size stops changing.
	last := int64(-1)
	for i := 0; i < recordFileSettleMax; i++ {
		size, err := deps.fileSize(path)
		if err == nil && size > 0 && size == last {
			return path, nil
		}
		if err == nil {
			last = size
		}
		deps.sleep(recordFileSettleWait)
	}
	return "", unreadable(fmt.Sprintf("the recording at %s never finished writing", path))
}

// locateWindow finds the measured window inside the recorded file. Whether Live
// starts recording the moment Session Record goes on, or waits for the next bar,
// is not documented; the file's length tells which one happened.
func locateWindow(durationSec float64, take recordedTake) (startSec, endSec float64, err error) {
	secPerBeat := 60 / take.TempoBPM
	windowSec := take.WindowBeats * secPerBeat
	immediate := (take.RecordOffBeat - take.RecordOnBeat) * secPerBeat
	quantized := (take.RecordOffBeat - take.WindowStart) * secPerBeat

	startSec = 0
	if math.Abs(durationSec-immediate) < math.Abs(durationSec-quantized) {
		startSec = (take.WindowStart - take.RecordOnBeat) * secPerBeat
	}
	endSec = startSec + windowSec
	if endSec > durationSec+0.05 {
		return 0, 0, actionable("recording_too_short",
			fmt.Sprintf("recorded %.2f s but the window needs %.2f s from %.2f s in", durationSec, windowSec, startSec),
			"Check that the Measure track stayed armed with Resampling input for the whole pass, then try again.")
	}
	if endSec > durationSec {
		endSec = durationSec
	}
	return startSec, endSec, nil
}
