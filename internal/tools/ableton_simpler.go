package tools

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

// Simpler LOM enums (see Live Object Model: SimplerDevice, Sample).
var (
	simplerPlaybackModes        = []string{"classic", "one_shot", "slicing"}
	simplerSlicingPlaybackModes = []string{"mono", "poly", "thru"}
	simplerSlicingStyles        = []string{"transient", "beat", "region", "manual"}
	simplerBeatDivisions        = []string{"1/16", "1/16T", "1/8", "1/8T", "1/4", "1/4T", "1/2", "1/2T", "1 Bar", "2 Bars", "4 Bars"}
)

func nameForIndex(list []string, idx int) string {
	if idx >= 0 && idx < len(list) {
		return list[idx]
	}
	return ""
}

// indexForName resolves a human name (case-insensitive) or a numeric string to an
// enum index. Returns false when the value is empty or out of range.
func indexForName(list []string, s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n >= 0 && n < len(list) {
			return n, true
		}
		return 0, false
	}
	low := strings.ToLower(s)
	for i, name := range list {
		if strings.ToLower(name) == low {
			return i, true
		}
	}
	return 0, false
}

func simplerStatusError(status string) error {
	switch status {
	case "invalid_track_index":
		return errors.New("invalid track_index")
	case "invalid_device_index":
		return errors.New("invalid device_index")
	case "not_simpler":
		return errors.New("device is not a Simpler (OriginalSimpler)")
	case "no_sample":
		return errors.New("simpler has no sample loaded")
	default:
		return fmt.Errorf("simpler op failed: status=%s", status)
	}
}

type GetSimplerInput struct {
	TrackIndex  int `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int `json:"device_index" jsonschema:"minimum=0"`
}

type SimplerState struct {
	TrackIndex              int    `json:"track_index"`
	DeviceIndex             int    `json:"device_index"`
	PlaybackMode            int    `json:"playback_mode"`
	PlaybackModeName        string `json:"playback_mode_name"`
	SlicingPlaybackMode     int    `json:"slicing_playback_mode"`
	SlicingPlaybackModeName string `json:"slicing_playback_mode_name"`
	SlicingStyle            *int   `json:"slicing_style,omitempty"`
	SlicingStyleName        string `json:"slicing_style_name,omitempty"`
	SlicingBeatDivision     *int   `json:"slicing_beat_division,omitempty"`
	SlicingBeatDivisionName string `json:"slicing_beat_division_name,omitempty"`
	NumSlices               int    `json:"num_slices"`
	HasSample               bool   `json:"has_sample"`
}

func parseSimplerState(res []interface{}) (SimplerState, error) {
	if err := ensureResponseLen(res, 3); err != nil {
		return SimplerState{}, err
	}
	track, err := abletonosc.AsInt(res[0])
	if err != nil {
		return SimplerState{}, fmt.Errorf("parse track_index: %w", err)
	}
	device, err := abletonosc.AsInt(res[1])
	if err != nil {
		return SimplerState{}, fmt.Errorf("parse device_index: %w", err)
	}
	status := fmt.Sprint(res[2])
	if status != "ok" {
		return SimplerState{TrackIndex: track, DeviceIndex: device}, simplerStatusError(status)
	}
	if err := ensureResponseLen(res, 9); err != nil {
		return SimplerState{}, err
	}
	pm, _ := abletonosc.AsInt(res[3])
	spm, _ := abletonosc.AsInt(res[4])
	ss, _ := abletonosc.AsInt(res[5])
	bd, _ := abletonosc.AsInt(res[6])
	ns, _ := abletonosc.AsInt(res[7])
	hs, _ := abletonosc.AsInt(res[8])
	state := SimplerState{
		TrackIndex:              track,
		DeviceIndex:             device,
		PlaybackMode:            pm,
		PlaybackModeName:        nameForIndex(simplerPlaybackModes, pm),
		SlicingPlaybackMode:     spm,
		SlicingPlaybackModeName: nameForIndex(simplerSlicingPlaybackModes, spm),
		NumSlices:               ns,
		HasSample:               hs != 0,
	}
	if ss >= 0 {
		state.SlicingStyle = &ss
		state.SlicingStyleName = nameForIndex(simplerSlicingStyles, ss)
	}
	if bd >= 0 {
		state.SlicingBeatDivision = &bd
		state.SlicingBeatDivisionName = nameForIndex(simplerBeatDivisions, bd)
	}
	return state, nil
}

func getSimplerState(client *abletonosc.Client, track, device int) (SimplerState, error) {
	res, err := client.QueryWithTimeout(3*time.Second, "/live/device/simpler/get",
		int32(track), int32(device))
	if err != nil {
		return SimplerState{}, fmt.Errorf("get simpler failed (browser patch required): %w", err)
	}
	return parseSimplerState(res)
}

func NewAbletonGetSimpler(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_simpler",
		"Ableton Live: get Simpler state (playback mode, slicing mode/style/beat division, slice count) with human-readable names; requires the browser patch",
		func(_ *ai.ToolContext, input GetSimplerInput) (SimplerState, error) {
			if input.TrackIndex < 0 || input.DeviceIndex < 0 {
				return SimplerState{}, errors.New("track_index and device_index must be >= 0")
			}
			return getSimplerState(client, input.TrackIndex, input.DeviceIndex)
		},
	)
}

func sendSimplerSet(client *abletonosc.Client, track, device int, prop string, value int) error {
	res, err := client.QueryWithTimeout(3*time.Second, "/live/device/simpler/set",
		int32(track), int32(device), prop, int32(value))
	if err != nil {
		return fmt.Errorf("set %s failed (browser patch required): %w", prop, err)
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return err
	}
	status := fmt.Sprint(res[2])
	if status == "set" {
		return nil
	}
	if status == "error" && len(res) > 4 {
		return fmt.Errorf("set %s failed: %s", prop, fmt.Sprint(res[4]))
	}
	return simplerStatusError(status)
}

type SetSimplerPlaybackModeInput struct {
	TrackIndex  int    `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int    `json:"device_index" jsonschema:"minimum=0"`
	Mode        string `json:"mode" jsonschema:"description=Playback mode: classic, one_shot, or slicing"`
}

func NewAbletonSetSimplerPlaybackMode(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_simpler_playback_mode",
		"Ableton Live: set Simpler playback mode (classic / one_shot / slicing); requires the browser patch. Returns the resulting Simpler state",
		func(_ *ai.ToolContext, input SetSimplerPlaybackModeInput) (SimplerState, error) {
			if input.TrackIndex < 0 || input.DeviceIndex < 0 {
				return SimplerState{}, errors.New("track_index and device_index must be >= 0")
			}
			idx, ok := indexForName(simplerPlaybackModes, input.Mode)
			if !ok {
				return SimplerState{}, fmt.Errorf("invalid mode %q; use one of: %s", input.Mode, strings.Join(simplerPlaybackModes, ", "))
			}
			if err := sendSimplerSet(client, input.TrackIndex, input.DeviceIndex, "playback_mode", idx); err != nil {
				return SimplerState{}, err
			}
			return getSimplerState(client, input.TrackIndex, input.DeviceIndex)
		},
	)
}

type SetSimplerSlicingInput struct {
	TrackIndex   int    `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex  int    `json:"device_index" jsonschema:"minimum=0"`
	Style        string `json:"style,omitempty" jsonschema:"description=Slicing style: transient, beat, region, or manual"`
	BeatDivision string `json:"beat_division,omitempty" jsonschema:"description=Beat division when style=beat: 1/16, 1/16T, 1/8, 1/8T, 1/4, 1/4T, 1/2, 1/2T, 1 Bar, 2 Bars, 4 Bars"`
}

func NewAbletonSetSimplerSlicing(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_set_simpler_slicing",
		"Ableton Live: set Simpler slicing style and/or beat division (requires a loaded sample). Does not switch playback mode; call ableton_set_simpler_playback_mode with slicing to hear slices. Returns the resulting Simpler state",
		func(_ *ai.ToolContext, input SetSimplerSlicingInput) (SimplerState, error) {
			if input.TrackIndex < 0 || input.DeviceIndex < 0 {
				return SimplerState{}, errors.New("track_index and device_index must be >= 0")
			}
			style := strings.TrimSpace(input.Style)
			bd := strings.TrimSpace(input.BeatDivision)
			if style == "" && bd == "" {
				return SimplerState{}, errors.New("provide style and/or beat_division")
			}
			if style != "" {
				idx, ok := indexForName(simplerSlicingStyles, style)
				if !ok {
					return SimplerState{}, fmt.Errorf("invalid style %q; use one of: %s", style, strings.Join(simplerSlicingStyles, ", "))
				}
				if err := sendSimplerSet(client, input.TrackIndex, input.DeviceIndex, "slicing_style", idx); err != nil {
					return SimplerState{}, err
				}
			}
			if bd != "" {
				idx, ok := indexForName(simplerBeatDivisions, bd)
				if !ok {
					return SimplerState{}, fmt.Errorf("invalid beat_division %q; use one of: %s", bd, strings.Join(simplerBeatDivisions, ", "))
				}
				if err := sendSimplerSet(client, input.TrackIndex, input.DeviceIndex, "slicing_beat_division", idx); err != nil {
					return SimplerState{}, err
				}
			}
			return getSimplerState(client, input.TrackIndex, input.DeviceIndex)
		},
	)
}

type GetSimplerSlicesInput struct {
	TrackIndex  int `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int `json:"device_index" jsonschema:"minimum=0"`
}

type SimplerSlice struct {
	Index       int     `json:"index"`
	StartSample int     `json:"start_sample"`
	StartSec    float64 `json:"start_sec"`
	Note        int     `json:"note" jsonschema:"description=Default C1-based MIDI note (36 + index); Live's slice-to-note mapping in Slicing mode"`
}

type SimplerSlicesOutput struct {
	TrackIndex   int            `json:"track_index"`
	DeviceIndex  int            `json:"device_index"`
	SampleRate   int            `json:"sample_rate"`
	SampleLength int            `json:"sample_length"`
	LengthSec    float64        `json:"length_sec"`
	Slices       []SimplerSlice `json:"slices"`
}

func parseSimplerSlices(res []interface{}) (SimplerSlicesOutput, error) {
	if err := ensureResponseLen(res, 3); err != nil {
		return SimplerSlicesOutput{}, err
	}
	track, err := abletonosc.AsInt(res[0])
	if err != nil {
		return SimplerSlicesOutput{}, fmt.Errorf("parse track_index: %w", err)
	}
	device, err := abletonosc.AsInt(res[1])
	if err != nil {
		return SimplerSlicesOutput{}, fmt.Errorf("parse device_index: %w", err)
	}
	status := fmt.Sprint(res[2])
	if status != "ok" {
		return SimplerSlicesOutput{TrackIndex: track, DeviceIndex: device}, simplerStatusError(status)
	}
	if err := ensureResponseLen(res, 5); err != nil {
		return SimplerSlicesOutput{}, err
	}
	sampleRate, _ := abletonosc.AsInt(res[3])
	sampleLength, _ := abletonosc.AsInt(res[4])
	out := SimplerSlicesOutput{
		TrackIndex:   track,
		DeviceIndex:  device,
		SampleRate:   sampleRate,
		SampleLength: sampleLength,
	}
	if sampleRate > 0 {
		out.LengthSec = float64(sampleLength) / float64(sampleRate)
	}
	for i, raw := range res[5:] {
		startSample, err := abletonosc.AsInt(raw)
		if err != nil {
			continue
		}
		slice := SimplerSlice{Index: i, StartSample: startSample, Note: 36 + i}
		if sampleRate > 0 {
			slice.StartSec = float64(startSample) / float64(sampleRate)
		}
		out.Slices = append(out.Slices, slice)
	}
	return out, nil
}

func NewAbletonGetSimplerSlices(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_simpler_slices",
		"Ableton Live: get the Simpler slice map (start position in samples and seconds per slice, plus default C1-based MIDI note); requires a sliced sample and the browser patch",
		func(_ *ai.ToolContext, input GetSimplerSlicesInput) (SimplerSlicesOutput, error) {
			if input.TrackIndex < 0 || input.DeviceIndex < 0 {
				return SimplerSlicesOutput{}, errors.New("track_index and device_index must be >= 0")
			}
			res, err := client.QueryWithTimeout(3*time.Second, "/live/device/simpler/get/slices",
				int32(input.TrackIndex), int32(input.DeviceIndex))
			if err != nil {
				return SimplerSlicesOutput{}, fmt.Errorf("get simpler slices failed (browser patch required): %w", err)
			}
			return parseSimplerSlices(res)
		},
	)
}
