package tools

import (
	"errors"
	"fmt"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

// Live Device.type values (LOM DeviceType).
const (
	liveDeviceTypeInstrument  = 1
	liveDeviceTypeAudioEffect = 2
	liveDeviceTypeMIDIEffect  = 3
)

type CompareFXBypassInput struct {
	TrackIndex     int   `json:"track_index" jsonschema:"description=Track with the clip and FX chain,minimum=0"`
	ClipIndex      int   `json:"clip_index" jsonschema:"description=Session clip slot to fire for both A and B,minimum=0"`
	DeviceIndices  []int `json:"device_indices,omitempty" jsonschema:"description=FX devices to toggle; omit to use all audio/MIDI effects (instruments skipped)"`
	BarsPerVersion *int  `json:"bars_per_version,omitempty" jsonschema:"description=Bars to hear each version (default 2),minimum=1,maximum=8"`
	Cycles         *int  `json:"cycles,omitempty" jsonschema:"description=How many dry→wet cycles (default 1),minimum=1,maximum=4"`
	BeatsPerBar    *int  `json:"beats_per_bar,omitempty" jsonschema:"description=Override beats per bar (default: Live signature numerator),minimum=1,maximum=16"`
	StartPlayback  bool  `json:"start_playback,omitempty" jsonschema:"description=Start Live playback before the audition (also auto-starts when transport is stopped)"`
	StopAfter      bool  `json:"stop_after,omitempty" jsonschema:"description=Stop playback after the final wet version"`
}

type FXDeviceState struct {
	DeviceIndex int    `json:"device_index"`
	Name        string `json:"name,omitempty"`
	Type        int    `json:"type"`
	WasActive   bool   `json:"was_active"`
}

type CompareFXBypassOutput struct {
	TrackIndex       int             `json:"track_index"`
	ClipIndex        int             `json:"clip_index"`
	Devices          []FXDeviceState `json:"devices"`
	BarsPerVersion   int             `json:"bars_per_version"`
	Cycles           int             `json:"cycles"`
	BeatsPerBar      int             `json:"beats_per_bar"`
	TempoBPM         float64         `json:"tempo_bpm"`
	DurationSec      float64         `json:"duration_sec"`
	PlaybackStarted  bool            `json:"playback_started"`
	Restored         bool            `json:"restored"`
	TimingNote       string          `json:"timing_note"`
	PreferencePrompt string          `json:"preference_prompt"`
	NextStep         string          `json:"next_step"`
}

type fxABClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonCompareFXBypass(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_compare_fx_bypass",
		"Ableton Live: A/B the same Session clip dry vs processed — bypass selected FX (A/source), then restore their prior active state (B/variation), on song time. Does not record taste; follow with ableton_record_variation_preference instrument=fx variation=bypass.",
		func(_ *ai.ToolContext, input CompareFXBypassInput) (CompareFXBypassOutput, error) {
			return compareFXBypass(client, input, time.Sleep)
		},
	)
}

func compareFXBypass(client fxABClient, input CompareFXBypassInput, sleep auditionSleeper) (CompareFXBypassOutput, error) {
	if input.TrackIndex < 0 || input.ClipIndex < 0 {
		return CompareFXBypassOutput{}, errors.New("track_index and clip_index must be >= 0")
	}
	bars := defaultAuditionBarsPerVersion
	if input.BarsPerVersion != nil {
		bars = *input.BarsPerVersion
	}
	cycles := defaultAuditionCycles
	if input.Cycles != nil {
		cycles = *input.Cycles
	}
	if bars < 1 || bars > 8 {
		return CompareFXBypassOutput{}, errors.New("bars_per_version must be between 1 and 8")
	}
	if cycles < 1 || cycles > 4 {
		return CompareFXBypassOutput{}, errors.New("cycles must be between 1 and 4")
	}
	if sleep == nil {
		sleep = time.Sleep
	}

	devices, err := resolveFXBypassDevices(client, input.TrackIndex, input.DeviceIndices)
	if err != nil {
		return CompareFXBypassOutput{}, err
	}
	if len(devices) == 0 {
		return CompareFXBypassOutput{}, actionable(
			"no_fx_devices",
			"No audio/MIDI effect devices found to bypass on this track.",
			"Load an Audio Effect (or pass device_indices from ableton_get_track_devices), then retry.",
		)
	}

	tempo, err := queryAuditionTempo(client)
	if err != nil {
		return CompareFXBypassOutput{}, err
	}
	beatsPerBar := 0
	if input.BeatsPerBar != nil {
		beatsPerBar = *input.BeatsPerBar
		if beatsPerBar < 1 || beatsPerBar > 16 {
			return CompareFXBypassOutput{}, errors.New("beats_per_bar must be between 1 and 16")
		}
	} else {
		beatsPerBar, err = queryAuditionBeatsPerBar(client)
		if err != nil {
			return CompareFXBypassOutput{}, err
		}
	}

	playbackStarted, err := ensureAuditionPlayback(client, input.StartPlayback)
	if err != nil {
		return CompareFXBypassOutput{}, err
	}

	prevQuant, err := queryClipTriggerQuantization(client)
	if err != nil {
		return CompareFXBypassOutput{}, err
	}
	if err := client.Send("/live/song/set/clip_trigger_quantization", int32(auditionBarQuantization)); err != nil {
		return CompareFXBypassOutput{}, fmt.Errorf("set clip trigger quantization: %w", err)
	}
	restoreQuant := true
	restored := false
	defer func() {
		_ = applyFXActiveStates(client, input.TrackIndex, devices, true)
		restored = true
		if restoreQuant {
			_ = client.Send("/live/song/set/clip_trigger_quantization", int32(prevQuant))
		}
	}()

	now, err := queryCurrentSongTime(client)
	if err != nil {
		return CompareFXBypassOutput{}, err
	}
	if err := client.Send("/live/clip_slot/fire", int32(input.TrackIndex), int32(input.ClipIndex)); err != nil {
		return CompareFXBypassOutput{}, fmt.Errorf("fire clip: %w", err)
	}
	cursor := ceilBarBeat(now, beatsPerBar)
	if err := waitUntilSongTime(client, sleep, cursor, tempo); err != nil {
		return CompareFXBypassOutput{}, fmt.Errorf("wait for clip launch: %w", err)
	}

	heardBeats := 0.0
	barBeats := float64(bars * beatsPerBar)
	for i := 0; i < cycles; i++ {
		// A / source = dry (FX bypassed)
		if err := applyFXActiveStates(client, input.TrackIndex, devices, false); err != nil {
			return CompareFXBypassOutput{}, fmt.Errorf("bypass FX (cycle %d): %w", i+1, err)
		}
		endA := cursor + barBeats
		if err := waitUntilSongTime(client, sleep, endA, tempo); err != nil {
			return CompareFXBypassOutput{}, fmt.Errorf("hear dry (cycle %d): %w", i+1, err)
		}
		heardBeats += barBeats
		cursor = endA

		// B / variation = prior active states (processed)
		if err := applyFXActiveStates(client, input.TrackIndex, devices, true); err != nil {
			return CompareFXBypassOutput{}, fmt.Errorf("restore FX (cycle %d): %w", i+1, err)
		}
		endB := cursor + barBeats
		if err := waitUntilSongTime(client, sleep, endB, tempo); err != nil {
			return CompareFXBypassOutput{}, fmt.Errorf("hear wet (cycle %d): %w", i+1, err)
		}
		heardBeats += barBeats
		cursor = endB
	}

	if input.StopAfter {
		if err := client.Send("/live/song/stop_playing"); err != nil {
			return CompareFXBypassOutput{}, fmt.Errorf("stop playback: %w", err)
		}
	}

	if err := applyFXActiveStates(client, input.TrackIndex, devices, true); err != nil {
		return CompareFXBypassOutput{}, fmt.Errorf("restore FX after audition: %w", err)
	}
	restored = true
	if err := client.Send("/live/song/set/clip_trigger_quantization", int32(prevQuant)); err != nil {
		return CompareFXBypassOutput{}, fmt.Errorf("restore clip trigger quantization: %w", err)
	}
	restoreQuant = false

	return CompareFXBypassOutput{
		TrackIndex:       input.TrackIndex,
		ClipIndex:        input.ClipIndex,
		Devices:          devices,
		BarsPerVersion:   bars,
		Cycles:           cycles,
		BeatsPerBar:      beatsPerBar,
		TempoBPM:         tempo,
		DurationSec:      heardBeats * 60 / tempo,
		PlaybackStarted:  playbackStarted,
		Restored:         restored,
		TimingNote:       "A=dry (FX bypassed), B=wet (prior is_active restored). Same clip; switches land on bar boundaries with 1-bar clip trigger quantization.",
		PreferencePrompt: "Which was closer to your ideal: dry (source) or processed (variation)? Record with ableton_record_variation_preference using instrument=fx variation=bypass.",
		NextStep:         "Call ableton_record_variation_preference with instrument=fx variation=bypass preferred=source|variation after the listener chooses.",
	}, nil
}

func resolveFXBypassDevices(client fxABClient, trackIndex int, requested []int) ([]FXDeviceState, error) {
	nameRes, err := client.Query("/live/track/get/devices/name", int32(trackIndex))
	if err != nil {
		return nil, fmt.Errorf("get device names: %w", err)
	}
	typeRes, err := client.Query("/live/track/get/devices/type", int32(trackIndex))
	if err != nil {
		return nil, fmt.Errorf("get device types: %w", err)
	}
	if err := ensureResponseLen(nameRes, 1); err != nil {
		return nil, fmt.Errorf("get device names: %w", err)
	}
	if err := ensureResponseLen(typeRes, 1); err != nil {
		return nil, fmt.Errorf("get device types: %w", err)
	}
	names := toStringSlice(nameRes[1:])
	types := make([]int, 0, len(typeRes)-1)
	for _, v := range typeRes[1:] {
		n, err := abletonosc.AsInt(v)
		if err != nil {
			return nil, fmt.Errorf("device type: %w", err)
		}
		types = append(types, n)
	}
	if len(names) != len(types) {
		return nil, fmt.Errorf("device list length mismatch: names=%d types=%d", len(names), len(types))
	}

	pick := map[int]bool{}
	if len(requested) > 0 {
		for _, idx := range requested {
			if idx < 0 || idx >= len(types) {
				return nil, fmt.Errorf("device_indices contains invalid index %d (track has %d devices)", idx, len(types))
			}
			if types[idx] == liveDeviceTypeInstrument {
				return nil, actionable(
					"instrument_not_bypassable",
					fmt.Sprintf("device_index %d is an instrument; FX bypass A/B only toggles audio/MIDI effects.", idx),
					"Omit instruments from device_indices, or omit device_indices to auto-select effects.",
				)
			}
			pick[idx] = true
		}
	} else {
		for i, typ := range types {
			if typ == liveDeviceTypeAudioEffect || typ == liveDeviceTypeMIDIEffect {
				pick[i] = true
			}
		}
	}

	out := make([]FXDeviceState, 0, len(pick))
	for i := 0; i < len(types); i++ {
		if !pick[i] {
			continue
		}
		active, err := queryDeviceIsActive(client, trackIndex, i)
		if err != nil {
			return nil, err
		}
		name := ""
		if i < len(names) {
			name = names[i]
		}
		out = append(out, FXDeviceState{
			DeviceIndex: i,
			Name:        name,
			Type:        types[i],
			WasActive:   active,
		})
	}
	return out, nil
}

func queryDeviceIsActive(client fxABClient, trackIndex, deviceIndex int) (bool, error) {
	res, err := client.Query("/live/device/get/is_active", int32(trackIndex), int32(deviceIndex))
	if err != nil {
		return false, actionable(
			"device_is_active_unavailable",
			fmt.Sprintf("could not read is_active for device %d: %v", deviceIndex, err),
			"Install/update the browser patch (device get/set is_active), then restart Live or send /live/api/reload.",
		)
	}
	if len(res) >= 3 {
		if status, ok := res[2].(string); ok && status != "" {
			return false, actionable(
				"device_is_active_error",
				fmt.Sprintf("get is_active failed: %s", status),
				"Check track_index/device_index with ableton_get_track_devices, then retry.",
			)
		}
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return false, fmt.Errorf("get is_active: %w", err)
	}
	active, err := abletonosc.AsInt(res[2])
	if err != nil {
		return false, fmt.Errorf("get is_active: %w", err)
	}
	return active != 0, nil
}

// applyFXActiveStates sets each device active=false (bypass) or restores WasActive when restore=true.
func applyFXActiveStates(client fxABClient, trackIndex int, devices []FXDeviceState, restore bool) error {
	for _, d := range devices {
		want := false
		if restore {
			want = d.WasActive
		}
		activeArg := int32(0)
		if want {
			activeArg = 1
		}
		res, err := client.Query("/live/device/set/is_active", int32(trackIndex), int32(d.DeviceIndex), activeArg)
		if err != nil {
			return fmt.Errorf("set is_active device %d: %w", d.DeviceIndex, err)
		}
		if len(res) >= 3 {
			if status, ok := res[2].(string); ok && status != "ok" {
				return fmt.Errorf("set is_active device %d: %s", d.DeviceIndex, status)
			}
		}
	}
	return nil
}
