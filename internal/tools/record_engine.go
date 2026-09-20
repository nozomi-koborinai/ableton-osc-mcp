package tools

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	recordMaxBars        = 64
	recordTailBeats      = 0.25 // record a touch past the window so its end is inside the file
	recordFireLeadBeats  = 1.0  // fire the next scene this far ahead; 1-bar quantization lands it on the boundary
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
	StopClipsAtEnds bool // bounce: stop all clips before and after the pass
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
// input is Resampling, using the armed-track + Session Record mechanism the
// bounce tool has always used. Waiting is done on song time.
func recordResampledPass(c recordClient, deps recordDeps, plan recordPlan) (take recordedTake, err error) {
	if len(plan.Spans) == 0 {
		return recordedTake{}, errors.New("at least one span is required")
	}
	for _, span := range plan.Spans {
		if span.Bars < 1 || span.Bars > recordMaxBars {
			return recordedTake{}, fmt.Errorf("bars must be between 1 and %d", recordMaxBars)
		}
		if span.SceneIndex != nil && *span.SceneIndex < 0 {
			return recordedTake{}, fmt.Errorf("invalid scene_index: %d", *span.SceneIndex)
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

	trackIndex, err := ensureNamedAudioTrack(c, plan.TrackName)
	if err != nil {
		return recordedTake{}, err
	}
	routing, err := pickResamplingRouting(c, trackIndex)
	if err != nil {
		return recordedTake{}, err
	}
	before, err := occupiedSlots(c, trackIndex)
	if err != nil {
		return recordedTake{}, err
	}
	prevQuant, err := queryClipTriggerQuantization(c)
	if err != nil {
		return recordedTake{}, err
	}

	armed, recording := false, false
	defer func() {
		if recording {
			_ = c.Send("/live/song/set/session_record", int32(0))
		}
		if armed {
			_ = c.Send("/live/track/set/arm", int32(trackIndex), int32(0))
		}
		_ = c.Send("/live/song/set/clip_trigger_quantization", int32(prevQuant))
	}()

	// Monitoring In and muted: Resampling carries the master without feeding back.
	for _, step := range []struct {
		address string
		args    []interface{}
	}{
		{"/live/track/set/input_routing_type", []interface{}{int32(trackIndex), routing}},
		{"/live/track/set/current_monitoring_state", []interface{}{int32(trackIndex), int32(0)}},
		{"/live/track/set/mute", []interface{}{int32(trackIndex), int32(1)}},
		{"/live/song/set/back_to_arranger", []interface{}{int32(0)}},
		{"/live/song/set/clip_trigger_quantization", []interface{}{int32(auditionBarQuantization)}},
	} {
		if err := c.Send(step.address, step.args...); err != nil {
			return recordedTake{}, err
		}
	}
	if plan.StopClipsAtEnds {
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
	recordOn, err := queryCurrentSongTime(c)
	if err != nil {
		return recordedTake{}, err
	}
	if err := c.Send("/live/song/set/session_record", int32(1)); err != nil {
		return recordedTake{}, err
	}
	recording = true

	windowStart := ceilBarBeat(recordOn, beatsPerBar)
	boundary := windowStart
	for i, span := range plan.Spans {
		if span.SceneIndex != nil {
			if i > 0 {
				if err := waitUntilSongTime(c, deps.sleep, boundary-recordFireLeadBeats, tempo); err != nil {
					return recordedTake{}, err
				}
			}
			if err := c.Send("/live/scene/fire", int32(*span.SceneIndex)); err != nil {
				return recordedTake{}, fmt.Errorf("fire scene %d: %w", *span.SceneIndex, err)
			}
		}
		boundary += float64(span.Bars * beatsPerBar)
	}
	if err := waitUntilSongTime(c, deps.sleep, boundary+recordTailBeats, tempo); err != nil {
		return recordedTake{}, err
	}

	if err := c.Send("/live/song/set/session_record", int32(0)); err != nil {
		return recordedTake{}, err
	}
	recording = false
	recordOff, err := queryCurrentSongTime(c)
	if err != nil {
		return recordedTake{}, err
	}
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
		RecordOnBeat: recordOn, WindowStart: windowStart, WindowBeats: boundary - windowStart, RecordOffBeat: recordOff,
	}
	for slot, has := range after {
		if has && (slot >= len(before) || !before[slot]) {
			take.Slots = append(take.Slots, slot)
		}
	}
	if len(take.Slots) == 0 {
		return recordedTake{}, actionable("recording_missing",
			fmt.Sprintf("no new clip appeared on the %q track after recording", plan.TrackName),
			"Check that the track is an audio track Live can arm and that its input shows Resampling, then try again.")
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
