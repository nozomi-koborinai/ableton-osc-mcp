package tools

import (
	"fmt"
	"math"
	"time"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	// Live clip_trigger_quantization: 4 = 1 Bar (see Live Object Model).
	auditionBarQuantization = 4
	auditionSongTimeEpsilon = 1e-3
	auditionPollInterval    = 20 * time.Millisecond
)

type auditionClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

type auditionSleeper func(time.Duration)

// barLeadSeconds is how long before a bar line a quantized command is sent.
// Commands need about a tenth of a second to reach Live, whatever the tempo.
const barLeadSeconds = 0.5

// barLeadBeats is barLeadSeconds in beats: at least a beat, never the whole bar.
func barLeadBeats(tempo float64, beatsPerBar int) float64 {
	return math.Min(math.Max(1, barLeadSeconds*tempo/60), float64(beatsPerBar)-0.5)
}

// nextSafeBarLine returns the current song time and the bar line a quantized
// command sent now will land on. If that line is closer than the lead, it waits
// for it to pass first: a command sent just before a bar line arrives after it
// and would wait a whole bar more.
func nextSafeBarLine(c recordClient, sleep auditionSleeper, tempo float64, beatsPerBar int) (float64, float64, error) {
	now, err := queryCurrentSongTime(c)
	if err != nil {
		return 0, 0, err
	}
	line := ceilBarBeat(now, beatsPerBar)
	if line-now < barLeadBeats(tempo, beatsPerBar) {
		if err := waitUntilSongTime(c, sleep, line, tempo); err != nil {
			return 0, 0, err
		}
		if now, err = queryCurrentSongTime(c); err != nil {
			return 0, 0, err
		}
		line = ceilBarBeat(now, beatsPerBar)
	}
	return now, line, nil
}

func ensureAuditionPlayback(client auditionClient, forceStart bool) (bool, error) {
	playing, err := queryAuditionIsPlaying(client)
	if err != nil {
		return false, err
	}
	if playing && !forceStart {
		return false, nil
	}
	if err := client.Send("/live/song/start_playing"); err != nil {
		return false, fmt.Errorf("start playback: %w", err)
	}
	return !playing, nil
}

func queryAuditionTempo(client auditionClient) (float64, error) {
	tempoRes, err := client.Query("/live/song/get/tempo")
	if err != nil {
		return 0, fmt.Errorf("get tempo: %w", err)
	}
	if err := ensureResponseLen(tempoRes, 1); err != nil {
		return 0, fmt.Errorf("get tempo: %w", err)
	}
	tempo, err := abletonosc.AsFloat64(tempoRes[0])
	if err != nil || tempo <= 0 {
		return 0, fmt.Errorf("unexpected tempo: %v", tempoRes)
	}
	return tempo, nil
}

func queryAuditionBeatsPerBar(client auditionClient) (int, error) {
	res, err := client.Query("/live/song/get/signature_numerator")
	if err != nil {
		return 0, fmt.Errorf("get signature numerator: %w", err)
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return 0, fmt.Errorf("get signature numerator: %w", err)
	}
	beats, err := abletonosc.AsInt(res[0])
	if err != nil || beats < 1 || beats > 16 {
		return 0, fmt.Errorf("unexpected signature numerator: %v", res)
	}
	return beats, nil
}

func queryAuditionIsPlaying(client auditionClient) (bool, error) {
	res, err := client.Query("/live/song/get/is_playing")
	if err != nil {
		return false, fmt.Errorf("get playback state: %w", err)
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return false, fmt.Errorf("get playback state: %w", err)
	}
	playing, err := abletonosc.AsBool(res[0])
	if err != nil {
		return false, fmt.Errorf("get playback state: %w", err)
	}
	return playing, nil
}

func queryClipTriggerQuantization(client auditionClient) (int, error) {
	res, err := client.Query("/live/song/get/clip_trigger_quantization")
	if err != nil {
		return 0, fmt.Errorf("get clip trigger quantization: %w", err)
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return 0, fmt.Errorf("get clip trigger quantization: %w", err)
	}
	quant, err := abletonosc.AsInt(res[0])
	if err != nil {
		return 0, fmt.Errorf("get clip trigger quantization: %w", err)
	}
	return quant, nil
}

func queryCurrentSongTime(client auditionClient) (float64, error) {
	res, err := client.Query("/live/song/get/current_song_time")
	if err != nil {
		return 0, fmt.Errorf("get current song time: %w", err)
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return 0, fmt.Errorf("get current song time: %w", err)
	}
	songTime, err := abletonosc.AsFloat64(res[0])
	if err != nil {
		return 0, fmt.Errorf("get current song time: %w", err)
	}
	return songTime, nil
}

// songTimeFinalStretch is the part of a wait that is slept, not polled.
const songTimeFinalStretch = 300 * time.Millisecond

// errTransportStopped says song time will not get anywhere: playback is off.
var errTransportStopped = &ActionableError{
	Code:     "transport_stopped",
	Message:  "playback was stopped in Live while the tool was waiting on song time",
	NextStep: "Start playback (or let the tool start it) and run it again.",
}

// transportStallPolls is how many polls in a row song time may stand still
// before the wait asks whether playback is still on. A poll is a round trip to
// Live, which answers on a timer of about a tenth of a second, so this is
// roughly half a second: long enough for a transport that has only just been
// told to start (measured on Live 11: 25 polls took 2.9 s).
const transportStallPolls = 5

func waitUntilSongTime(client auditionClient, sleep auditionSleeper, targetBeats, tempo float64) error {
	remainingBeats := targetBeats
	if now, err := queryCurrentSongTime(client); err == nil {
		remainingBeats = targetBeats - now
	}
	if remainingBeats < 0 {
		remainingBeats = 0
	}
	// Wall-clock deadline guards against a stuck transport; allow 2x expected wait + 2s.
	timeout := time.Duration((remainingBeats*60/tempo)*2*float64(time.Second)) + 2*time.Second
	deadline := time.Now().Add(timeout)

	last, stalled := math.Inf(-1), 0
	for {
		now, err := queryCurrentSongTime(client)
		if err != nil {
			return err
		}
		if now+auditionSongTimeEpsilon >= targetBeats {
			return nil
		}
		if now != last {
			last, stalled = now, 0
		} else if stalled++; stalled >= transportStallPolls {
			// Someone pressed stop. Waiting out the deadline would only keep them waiting.
			if playing, err := queryAuditionIsPlaying(client); err == nil && !playing {
				return errTransportStopped
			}
			stalled = 0
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for song time %.3f (last %.3f)", targetBeats, now)
		}
		// A poll is a round trip of about a tenth of a second, so polling up to the
		// target overshoots it by as much (measured: a fader sent "150 ms ahead" of
		// a bar line arrived 50 ms after it). The last stretch is slept in one go.
		if remaining := time.Duration((targetBeats - now) * 60 / tempo * float64(time.Second)); remaining <= songTimeFinalStretch {
			sleep(remaining)
			return nil
		}
		sleep(auditionPollInterval)
	}
}

// ensureStillPlaying guards a launch. The last stretch of a wait is slept, not
// polled, so a stop in that stretch goes unnoticed by the wait. Launching a clip
// or a scene starts a stopped transport: it would undo what the listener has
// just done, and the tool would carry on as if nothing had happened.
func ensureStillPlaying(client auditionClient) error {
	if playing, err := queryAuditionIsPlaying(client); err == nil && !playing {
		return errTransportStopped
	}
	return nil
}

// ceilBarBeat returns the next bar boundary strictly after songTime when quantized to 1 bar.
// Firing exactly on a downbeat still waits for the following bar under Live's 1-bar quantization.
func ceilBarBeat(songTime float64, beatsPerBar int) float64 {
	bpb := float64(beatsPerBar)
	if bpb <= 0 {
		return songTime
	}
	return (math.Floor(songTime/bpb+auditionSongTimeEpsilon) + 1) * bpb
}
