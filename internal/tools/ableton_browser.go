package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type FindBrowserItemInput struct {
	Query string `json:"query" jsonschema:"description=Search string for Live Browser items (e.g. drum kit name)"`
}

type FindBrowserItemOutput struct {
	Matches []string `json:"matches" jsonschema:"description=Matching browser paths (up to 20)"`
}

type LoadBrowserItemInput struct {
	TrackIndex int    `json:"track_index" jsonschema:"minimum=0"`
	ItemName   string `json:"item_name" jsonschema:"description=Browser item name to load (e.g. Street Kit)"`
}

type LoadBrowserItemOutput struct {
	TrackIndex    int    `json:"track_index"`
	Status        string `json:"status" jsonschema:"description=loaded or not_found"`
	ItemName      string `json:"item_name"`
	DevicesBefore *int   `json:"devices_before,omitempty"`
	DevicesAfter  *int   `json:"devices_after,omitempty"`
}

type LoadDevicePresetInput struct {
	TrackIndex  int    `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int    `json:"device_index" jsonschema:"minimum=0"`
	PresetName  string `json:"preset_name" jsonschema:"description=Preset name to hotswap onto the device"`
}

type LoadDevicePresetOutput struct {
	TrackIndex  int    `json:"track_index"`
	DeviceIndex int    `json:"device_index"`
	Status      string `json:"status" jsonschema:"description=loaded, not_found, or invalid_device_index"`
	PresetName  string `json:"preset_name"`
}

func parseLoadBrowserItemResponse(res []interface{}) (LoadBrowserItemOutput, error) {
	if err := ensureResponseLen(res, 3); err != nil {
		return LoadBrowserItemOutput{}, err
	}
	trackIndex, err := abletonosc.AsInt(res[0])
	if err != nil {
		return LoadBrowserItemOutput{}, fmt.Errorf("parse track_index: %w", err)
	}
	status := fmt.Sprint(res[1])
	out := LoadBrowserItemOutput{
		TrackIndex: trackIndex,
		Status:     status,
		ItemName:   fmt.Sprint(res[2]),
	}
	if len(res) >= 5 {
		before, err1 := abletonosc.AsInt(res[3])
		after, err2 := abletonosc.AsInt(res[4])
		if err1 == nil && err2 == nil {
			out.DevicesBefore = &before
			out.DevicesAfter = &after
		}
	}
	if status != "loaded" {
		return out, fmt.Errorf("browser item not loaded: status=%s item=%s", status, out.ItemName)
	}
	return out, nil
}

func parseLoadDevicePresetResponse(res []interface{}) (LoadDevicePresetOutput, error) {
	if err := ensureResponseLen(res, 4); err != nil {
		return LoadDevicePresetOutput{}, err
	}
	trackIndex, err := abletonosc.AsInt(res[0])
	if err != nil {
		return LoadDevicePresetOutput{}, fmt.Errorf("parse track_index: %w", err)
	}
	deviceIndex, err := abletonosc.AsInt(res[1])
	if err != nil {
		return LoadDevicePresetOutput{}, fmt.Errorf("parse device_index: %w", err)
	}
	status := fmt.Sprint(res[2])
	out := LoadDevicePresetOutput{
		TrackIndex:  trackIndex,
		DeviceIndex: deviceIndex,
		Status:      status,
		PresetName:  fmt.Sprint(res[3]),
	}
	if status != "loaded" {
		return out, fmt.Errorf("preset not loaded: status=%s preset=%s", status, out.PresetName)
	}
	return out, nil
}

func NewAbletonFindBrowserItem(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_find_browser_item", "Ableton Live: search Browser for loadable items (requires browser patch)",
		func(_ *ai.ToolContext, input FindBrowserItemInput) (FindBrowserItemOutput, error) {
			query := strings.TrimSpace(input.Query)
			if query == "" {
				return FindBrowserItemOutput{}, errors.New("query is required")
			}
			res, err := client.Query("/live/browser/find", query)
			if err != nil {
				return FindBrowserItemOutput{}, fmt.Errorf("browser find failed (abletonosc browser patch required): %w", err)
			}
			return FindBrowserItemOutput{Matches: toStringSlice(res)}, nil
		},
	)
}

func NewAbletonLoadBrowserItem(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_load_browser_item", "Ableton Live: load a Browser item (e.g. Drum Rack kit) onto a track",
		func(_ *ai.ToolContext, input LoadBrowserItemInput) (LoadBrowserItemOutput, error) {
			if input.TrackIndex < 0 {
				return LoadBrowserItemOutput{}, errors.New("track_index must be >= 0")
			}
			itemName := strings.TrimSpace(input.ItemName)
			if itemName == "" {
				return LoadBrowserItemOutput{}, errors.New("item_name is required")
			}
			res, err := client.Query("/live/track/load/browser_item", int32(input.TrackIndex), itemName)
			if err != nil {
				return LoadBrowserItemOutput{}, fmt.Errorf("browser load failed (abletonosc browser patch required): %w", err)
			}
			return parseLoadBrowserItemResponse(res)
		},
	)
}

func NewAbletonLoadDevicePreset(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_load_device_preset", "Ableton Live: hotswap a preset onto an existing device",
		func(_ *ai.ToolContext, input LoadDevicePresetInput) (LoadDevicePresetOutput, error) {
			if input.TrackIndex < 0 {
				return LoadDevicePresetOutput{}, errors.New("track_index must be >= 0")
			}
			if input.DeviceIndex < 0 {
				return LoadDevicePresetOutput{}, errors.New("device_index must be >= 0")
			}
			presetName := strings.TrimSpace(input.PresetName)
			if presetName == "" {
				return LoadDevicePresetOutput{}, errors.New("preset_name is required")
			}
			res, err := client.Query(
				"/live/device/load/preset",
				int32(input.TrackIndex),
				int32(input.DeviceIndex),
				presetName,
			)
			if err != nil {
				return LoadDevicePresetOutput{}, fmt.Errorf("preset load failed (abletonosc browser patch required): %w", err)
			}
			return parseLoadDevicePresetResponse(res)
		},
	)
}
