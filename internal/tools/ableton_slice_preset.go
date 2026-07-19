package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const slicePresetVersion = 1

// SlicePreset captures a Simpler's manual slice map plus the slicing/playback
// context, so a chop can be reproduced later on the same sample.
type SlicePreset struct {
	Version             int       `json:"version"`
	Name                string    `json:"name"`
	SampleRate          int       `json:"sample_rate"`
	SampleLength        int       `json:"sample_length"`
	PlaybackMode        int       `json:"playback_mode"`
	SlicingStyle        int       `json:"slicing_style"`
	SlicingBeatDivision int       `json:"slicing_beat_division"`
	Slices              []int     `json:"slices"`
	SavedAt             time.Time `json:"saved_at"`
}

var slicePresetNameSanitize = regexp.MustCompile(`[^a-zA-Z0-9_.\- ]+`)

func slicePresetDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "ableton-osc-mcp", "slice-presets")
}

func slicePresetPath(name string) (string, error) {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return "", errors.New("preset name is required")
	}
	clean = slicePresetNameSanitize.ReplaceAllString(clean, "_")
	return filepath.Join(slicePresetDir(), clean+".json"), nil
}

func writeSlicePreset(path string, preset SlicePreset) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create slice preset dir: %w", err)
	}
	data, err := json.MarshalIndent(preset, "", "  ")
	if err != nil {
		return fmt.Errorf("encode slice preset: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func readSlicePreset(path string) (SlicePreset, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return SlicePreset{}, fmt.Errorf("slice preset not found: %s", strings.TrimSuffix(filepath.Base(path), ".json"))
	}
	if err != nil {
		return SlicePreset{}, fmt.Errorf("read slice preset: %w", err)
	}
	var p SlicePreset
	if err := json.Unmarshal(data, &p); err != nil {
		return SlicePreset{}, fmt.Errorf("parse slice preset: %w", err)
	}
	if p.Version != slicePresetVersion {
		return SlicePreset{}, fmt.Errorf("unsupported slice preset version: %d", p.Version)
	}
	return p, nil
}

func sliceStartSamples(slices []SimplerSlice) []int {
	out := make([]int, 0, len(slices))
	for _, s := range slices {
		out = append(out, s.StartSample)
	}
	return out
}

func derefIntOr(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}

// sendSimplerSetSlices replaces the Simpler's manual slices with the given
// sample-frame positions via the browser patch. Returns the resulting count.
func sendSimplerSetSlices(client *abletonosc.Client, track, device int, slices []int) (int, error) {
	args := []interface{}{int32(track), int32(device)}
	for _, s := range slices {
		args = append(args, int32(s))
	}
	res, err := client.QueryWithTimeout(5*time.Second, "/live/device/simpler/set/slices", args...)
	if err != nil {
		return 0, fmt.Errorf("set slices failed (browser patch required): %w", err)
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return 0, err
	}
	status := fmt.Sprint(res[2])
	if status != "ok" {
		if status == "error" && len(res) > 4 {
			return 0, fmt.Errorf("set slices failed at %s: %s", fmt.Sprint(res[3]), fmt.Sprint(res[4]))
		}
		return 0, simplerStatusError(status)
	}
	num := 0
	if len(res) > 3 {
		num, _ = abletonosc.AsInt(res[3])
	}
	return num, nil
}

type SaveSlicePresetInput struct {
	TrackIndex  int    `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int    `json:"device_index" jsonschema:"minimum=0"`
	Name        string `json:"name" jsonschema:"description=Preset name (stored as JSON under the app config dir)"`
}

type SlicePresetOutput struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	NumSlices    int    `json:"num_slices"`
	SampleRate   int    `json:"sample_rate"`
	SampleLength int    `json:"sample_length"`
	Note         string `json:"note,omitempty"`
}

func NewAbletonSaveSlicePreset(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_save_slice_preset",
		"Ableton Live: save a Simpler's slice map (slice points in sample frames + slicing/playback modes) to a reusable JSON preset under the app config dir. Reproduce later on the same sample with ableton_load_slice_preset. Requires the browser patch",
		func(_ *ai.ToolContext, input SaveSlicePresetInput) (SlicePresetOutput, error) {
			if input.TrackIndex < 0 || input.DeviceIndex < 0 {
				return SlicePresetOutput{}, errors.New("track_index and device_index must be >= 0")
			}
			path, err := slicePresetPath(input.Name)
			if err != nil {
				return SlicePresetOutput{}, err
			}
			res, err := client.QueryWithTimeout(3*time.Second, "/live/device/simpler/get/slices",
				int32(input.TrackIndex), int32(input.DeviceIndex))
			if err != nil {
				return SlicePresetOutput{}, fmt.Errorf("get simpler slices failed (browser patch required): %w", err)
			}
			slicesOut, err := parseSimplerSlices(res)
			if err != nil {
				return SlicePresetOutput{}, err
			}
			state, err := getSimplerState(client, input.TrackIndex, input.DeviceIndex)
			if err != nil {
				return SlicePresetOutput{}, err
			}
			preset := SlicePreset{
				Version:             slicePresetVersion,
				Name:                strings.TrimSpace(input.Name),
				SampleRate:          slicesOut.SampleRate,
				SampleLength:        slicesOut.SampleLength,
				PlaybackMode:        state.PlaybackMode,
				SlicingStyle:        derefIntOr(state.SlicingStyle, 3),
				SlicingBeatDivision: derefIntOr(state.SlicingBeatDivision, -1),
				Slices:              sliceStartSamples(slicesOut.Slices),
				SavedAt:             time.Now().UTC(),
			}
			if len(preset.Slices) == 0 {
				return SlicePresetOutput{}, errors.New("no slices to save (slice the sample first, or check the Simpler is in a sliced state)")
			}
			if err := writeSlicePreset(path, preset); err != nil {
				return SlicePresetOutput{}, err
			}
			return SlicePresetOutput{
				Name:         preset.Name,
				Path:         path,
				NumSlices:    len(preset.Slices),
				SampleRate:   preset.SampleRate,
				SampleLength: preset.SampleLength,
			}, nil
		},
	)
}

type LoadSlicePresetInput struct {
	TrackIndex  int    `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int    `json:"device_index" jsonschema:"minimum=0"`
	Name        string `json:"name" jsonschema:"description=Preset name to restore"`
	Force       *bool  `json:"force,omitempty" jsonschema:"description=Apply even if the loaded sample length differs from the preset (default false)"`
}

func NewAbletonLoadSlicePreset(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_load_slice_preset",
		"Ableton Live: restore a saved slice preset onto a Simpler (switches to Manual slicing and re-inserts the saved slice points). Guards against a mismatched sample by length unless force=true. Requires the browser patch. Returns the resulting Simpler state",
		func(_ *ai.ToolContext, input LoadSlicePresetInput) (SimplerState, error) {
			if input.TrackIndex < 0 || input.DeviceIndex < 0 {
				return SimplerState{}, errors.New("track_index and device_index must be >= 0")
			}
			path, err := slicePresetPath(input.Name)
			if err != nil {
				return SimplerState{}, err
			}
			preset, err := readSlicePreset(path)
			if err != nil {
				return SimplerState{}, err
			}
			if len(preset.Slices) == 0 {
				return SimplerState{}, errors.New("preset has no slices")
			}
			force := input.Force != nil && *input.Force
			if !force {
				res, err := client.QueryWithTimeout(3*time.Second, "/live/device/simpler/get/slices",
					int32(input.TrackIndex), int32(input.DeviceIndex))
				if err != nil {
					return SimplerState{}, fmt.Errorf("get simpler slices failed (browser patch required): %w", err)
				}
				current, err := parseSimplerSlices(res)
				if err != nil {
					return SimplerState{}, err
				}
				if preset.SampleLength > 0 && current.SampleLength > 0 && preset.SampleLength != current.SampleLength {
					return SimplerState{}, fmt.Errorf("loaded sample length %d != preset %d; load the same sample or set force=true", current.SampleLength, preset.SampleLength)
				}
			}
			if _, err := sendSimplerSetSlices(client, input.TrackIndex, input.DeviceIndex, preset.Slices); err != nil {
				return SimplerState{}, err
			}
			// Restore playback mode so a slicing preset plays as slices again.
			if err := sendSimplerSet(client, input.TrackIndex, input.DeviceIndex, "playback_mode", preset.PlaybackMode); err != nil {
				return SimplerState{}, err
			}
			return getSimplerState(client, input.TrackIndex, input.DeviceIndex)
		},
	)
}

type ListSlicePresetsOutput struct {
	Dir     string   `json:"dir"`
	Presets []string `json:"presets"`
}

func NewAbletonListSlicePresets(g *genkit.Genkit) ai.Tool {
	return genkit.DefineTool(g, "ableton_list_slice_presets",
		"Ableton Live: list saved slice presets (names) available for ableton_load_slice_preset.",
		func(_ *ai.ToolContext, _ struct{}) (ListSlicePresetsOutput, error) {
			dir := slicePresetDir()
			out := ListSlicePresetsOutput{Dir: dir, Presets: []string{}}
			entries, err := os.ReadDir(dir)
			if errors.Is(err, os.ErrNotExist) {
				return out, nil
			}
			if err != nil {
				return ListSlicePresetsOutput{}, fmt.Errorf("read slice preset dir: %w", err)
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				out.Presets = append(out.Presets, strings.TrimSuffix(e.Name(), ".json"))
			}
			sort.Strings(out.Presets)
			return out, nil
		},
	)
}
