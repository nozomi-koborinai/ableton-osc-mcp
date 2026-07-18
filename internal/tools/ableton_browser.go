package tools

import (
	"errors"
	"fmt"
	"strings"
	"time"

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

type ListBrowserFolderInput struct {
	RootName  string   `json:"root_name,omitempty" jsonschema:"description=Browser root name; omit to list roots"`
	PathParts []string `json:"path_parts,omitempty" jsonschema:"description=Optional folder path under root"`
}

type BrowserFolderItem struct {
	Name     string `json:"name"`
	Loadable bool   `json:"loadable"`
	Folder   bool   `json:"folder"`
}

type ListBrowserFolderOutput struct {
	RootName  string              `json:"root_name,omitempty" jsonschema:"description=Empty when listing roots"`
	PathParts []string            `json:"path_parts,omitempty"`
	Items     []BrowserFolderItem `json:"items"`
}

type LoadBrowserPathInput struct {
	TrackIndex int      `json:"track_index" jsonschema:"minimum=0"`
	RootName   string   `json:"root_name" jsonschema:"description=Browser root (e.g. Drums)"`
	PathParts  []string `json:"path_parts,omitempty" jsonschema:"description=Optional folder path under root"`
	ItemName   string   `json:"item_name" jsonschema:"description=Loadable item name under the folder"`
}

type LoadBrowserPathOutput struct {
	TrackIndex    int    `json:"track_index"`
	Loaded        string `json:"loaded"`
	DevicesBefore int    `json:"devices_before"`
	DevicesAfter  int    `json:"devices_after"`
}

type loadAtPathResult struct {
	TrackIndex    int
	Status        string
	Loaded        string
	DevicesBefore int
	DevicesAfter  int
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

func parseBrowserListEntry(raw string) BrowserFolderItem {
	parts := strings.Split(raw, "|")
	item := BrowserFolderItem{Name: parts[0]}
	for _, part := range parts[1:] {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch key {
		case "loadable":
			item.Loadable = parsePythonBool(value)
		case "folder":
			item.Folder = parsePythonBool(value)
		}
	}
	return item
}

func parsePythonBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return true
	default:
		return false
	}
}

func parseListBrowserFolderResponse(res []interface{}, rootName string, pathParts []string) (ListBrowserFolderOutput, error) {
	if len(res) == 0 {
		return ListBrowserFolderOutput{Items: []BrowserFolderItem{}}, nil
	}

	first := fmt.Sprint(res[0])
	switch first {
	case "root_not_found", "path_not_found":
		return ListBrowserFolderOutput{}, fmt.Errorf("browser folder list failed: status=%s reply=%v", first, res)
	case "roots":
		items := make([]BrowserFolderItem, 0, len(res)-1)
		for _, v := range res[1:] {
			items = append(items, parseBrowserListEntry(fmt.Sprint(v)))
		}
		return ListBrowserFolderOutput{Items: items}, nil
	}

	prefixLen := 1 + len(pathParts)
	if len(res) < prefixLen {
		return ListBrowserFolderOutput{}, fmt.Errorf("unexpected browser folder reply: %v", res)
	}
	if fmt.Sprint(res[0]) != rootName {
		return ListBrowserFolderOutput{}, fmt.Errorf("unexpected browser folder root: got %q want %q", res[0], rootName)
	}
	for i, part := range pathParts {
		if fmt.Sprint(res[1+i]) != part {
			return ListBrowserFolderOutput{}, fmt.Errorf("unexpected browser folder path at %d: got %q want %q", i, res[1+i], part)
		}
	}

	items := make([]BrowserFolderItem, 0, len(res)-prefixLen)
	for _, v := range res[prefixLen:] {
		items = append(items, parseBrowserListEntry(fmt.Sprint(v)))
	}
	return ListBrowserFolderOutput{
		RootName:  rootName,
		PathParts: append([]string(nil), pathParts...),
		Items:     items,
	}, nil
}

func parseLoadAtPathResponse(res []interface{}) (loadAtPathResult, error) {
	if err := ensureResponseLen(res, 3); err != nil {
		return loadAtPathResult{}, err
	}
	trackIndex, err := abletonosc.AsInt(res[0])
	if err != nil {
		return loadAtPathResult{}, fmt.Errorf("parse track_index: %w", err)
	}
	status := fmt.Sprint(res[1])
	out := loadAtPathResult{
		TrackIndex: trackIndex,
		Status:     status,
		Loaded:     fmt.Sprint(res[2]),
	}
	if status != "loaded" {
		return out, fmt.Errorf("load failed: status=%s reply=%v", status, res)
	}
	if err := ensureResponseLen(res, 5); err != nil {
		return out, err
	}
	before, err := abletonosc.AsInt(res[3])
	if err != nil {
		return out, fmt.Errorf("parse devices_before: %w", err)
	}
	after, err := abletonosc.AsInt(res[4])
	if err != nil {
		return out, fmt.Errorf("parse devices_after: %w", err)
	}
	out.DevicesBefore = before
	out.DevicesAfter = after
	return out, nil
}

func loadAtPathArgs(trackIndex int, rootName string, pathParts []string, itemName string) []interface{} {
	args := []interface{}{int32(trackIndex), int32(-1), rootName}
	for _, p := range pathParts {
		args = append(args, p)
	}
	args = append(args, itemName)
	return args
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

func NewAbletonListBrowserFolder(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_list_browser_folder", "Ableton Live: list Browser roots or folder children (requires browser patch)",
		func(_ *ai.ToolContext, input ListBrowserFolderInput) (ListBrowserFolderOutput, error) {
			rootName := strings.TrimSpace(input.RootName)
			pathParts := make([]string, 0, len(input.PathParts))
			for _, part := range input.PathParts {
				trimmed := strings.TrimSpace(part)
				if trimmed == "" {
					return ListBrowserFolderOutput{}, errors.New("path_parts must not contain empty strings")
				}
				pathParts = append(pathParts, trimmed)
			}
			if rootName == "" && len(pathParts) > 0 {
				return ListBrowserFolderOutput{}, errors.New("root_name is required when path_parts is set")
			}

			var args []interface{}
			if rootName != "" {
				args = append(args, rootName)
				for _, part := range pathParts {
					args = append(args, part)
				}
			}

			res, err := client.Query("/live/browser/list_folder", args...)
			if err != nil {
				return ListBrowserFolderOutput{}, fmt.Errorf("browser list failed (abletonosc browser patch required): %w", err)
			}
			return parseListBrowserFolderResponse(res, rootName, pathParts)
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

func NewAbletonLoadBrowserPath(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_load_browser_path", "Ableton Live: load a Browser item onto a track by exact path (requires browser patch)",
		func(_ *ai.ToolContext, input LoadBrowserPathInput) (LoadBrowserPathOutput, error) {
			if input.TrackIndex < 0 {
				return LoadBrowserPathOutput{}, errors.New("track_index must be >= 0")
			}
			rootName := strings.TrimSpace(input.RootName)
			itemName := strings.TrimSpace(input.ItemName)
			if rootName == "" || itemName == "" {
				return LoadBrowserPathOutput{}, errors.New("root_name and item_name are required")
			}
			pathParts := make([]string, 0, len(input.PathParts))
			for _, part := range input.PathParts {
				trimmed := strings.TrimSpace(part)
				if trimmed == "" {
					return LoadBrowserPathOutput{}, errors.New("path_parts must not contain empty strings")
				}
				pathParts = append(pathParts, trimmed)
			}

			res, err := client.QueryWithTimeout(
				10*time.Second,
				"/live/browser/load_at_path",
				loadAtPathArgs(input.TrackIndex, rootName, pathParts, itemName)...,
			)
			if err != nil {
				return LoadBrowserPathOutput{}, fmt.Errorf("browser path load failed (abletonosc browser patch required): %w", err)
			}
			parsed, err := parseLoadAtPathResponse(res)
			if err != nil {
				return LoadBrowserPathOutput{}, err
			}
			return LoadBrowserPathOutput{
				TrackIndex:    parsed.TrackIndex,
				Loaded:        parsed.Loaded,
				DevicesBefore: parsed.DevicesBefore,
				DevicesAfter:  parsed.DevicesAfter,
			}, nil
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
