package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type PreviewDestructiveInput struct {
	Action       string `json:"action" jsonschema:"description=One of: delete_track, delete_clip, delete_device, clear_clip_notes, clear_scene_clips"`
	TrackIndex   *int   `json:"track_index,omitempty" jsonschema:"minimum=0"`
	DeviceIndex  *int   `json:"device_index,omitempty" jsonschema:"minimum=0"`
	ClipIndex    *int   `json:"clip_index,omitempty" jsonschema:"minimum=0"`
	SceneIndex   *int   `json:"scene_index,omitempty" jsonschema:"minimum=0"`
	TrackIndices []int  `json:"track_indices,omitempty" jsonschema:"description=For clear_scene_clips: tracks to affect; omit = all"`
}

type PreviewDestructiveOutput struct {
	Action      string   `json:"action"`
	Summary     string   `json:"summary"`
	Details     []string `json:"details"`
	Destructive bool     `json:"destructive"`
	Hint        string   `json:"hint" jsonschema:"description=How to execute after reviewing the preview"`
}

func previewDestructive(client oscQuerier, input PreviewDestructiveInput) (PreviewDestructiveOutput, error) {
	action := strings.TrimSpace(strings.ToLower(input.Action))
	out := PreviewDestructiveOutput{
		Action:      action,
		Details:     []string{},
		Destructive: true,
	}

	switch action {
	case "delete_track":
		if input.TrackIndex == nil {
			return out, actionable("missing_args", "track_index is required for delete_track", "Pass track_index of the track to preview.")
		}
		idx := *input.TrackIndex
		name, err := queryTrackName(client, idx)
		if err != nil {
			return out, wrapActionable(err, "query_failed", "Check that AbletonOSC is connected, then retry.")
		}
		devs := previewDeviceNames(client, idx)
		out.Summary = fmt.Sprintf("delete track %d %q (%d devices)", idx, name, len(devs))
		out.Details = append(out.Details, fmt.Sprintf("track: [%d] %s", idx, name))
		if len(devs) == 0 {
			out.Details = append(out.Details, "devices: (none)")
		} else {
			out.Details = append(out.Details, "devices: "+strings.Join(devs, ", "))
		}
		out.Hint = "Call ableton_delete_track with the same track_index and confirm=true."

	case "delete_clip":
		if input.TrackIndex == nil || input.ClipIndex == nil {
			return out, actionable("missing_args", "track_index and clip_index are required for delete_clip", "Pass both indices of the clip slot to preview.")
		}
		t, c := *input.TrackIndex, *input.ClipIndex
		has, err := queryHasClip(client, t, c)
		if err != nil {
			return out, wrapActionable(err, "query_failed", "Check AbletonOSC connectivity, then retry.")
		}
		clipName := ""
		if has {
			if res, err := client.Query("/live/clip/get/name", int32(t), int32(c)); err == nil && len(res) >= 3 {
				clipName = fmt.Sprint(res[2])
			}
		}
		if !has {
			out.Summary = fmt.Sprintf("slot [%d,%d] is already empty (no-op)", t, c)
			out.Destructive = false
			out.Details = append(out.Details, "has_clip: false")
			out.Hint = "Nothing to delete."
		} else {
			out.Summary = fmt.Sprintf("delete clip on track %d slot %d", t, c)
			if clipName != "" {
				out.Summary += fmt.Sprintf(" (%q)", clipName)
				out.Details = append(out.Details, "clip_name: "+clipName)
			}
			out.Details = append(out.Details, "has_clip: true → would become empty")
			out.Hint = "Call ableton_delete_clip with the same indices and confirm=true."
		}

	case "delete_device":
		if input.TrackIndex == nil || input.DeviceIndex == nil {
			return out, actionable("missing_args", "track_index and device_index are required for delete_device", "Pass both indices of the device to preview.")
		}
		t, d := *input.TrackIndex, *input.DeviceIndex
		devs := previewDeviceNames(client, t)
		if d < 0 || d >= len(devs) {
			return out, actionable("invalid_device_index",
				fmt.Sprintf("device_index %d out of range (%d devices on track %d)", d, len(devs), t),
				"Call ableton_get_track_devices first and pick a valid device_index.")
		}
		out.Summary = fmt.Sprintf("delete device %d %q from track %d", d, devs[d], t)
		out.Details = append(out.Details, fmt.Sprintf("device: [%d] %s", d, devs[d]))
		out.Details = append(out.Details, fmt.Sprintf("devices_before: %d → after: %d", len(devs), len(devs)-1))
		out.Hint = "Call ableton_delete_device with the same indices and confirm=true."

	case "clear_clip_notes":
		if input.TrackIndex == nil || input.ClipIndex == nil {
			return out, actionable("missing_args", "track_index and clip_index are required for clear_clip_notes", "Pass both indices of the MIDI clip to preview.")
		}
		t, c := *input.TrackIndex, *input.ClipIndex
		has, err := queryHasClip(client, t, c)
		if err != nil {
			return out, wrapActionable(err, "query_failed", "Check AbletonOSC connectivity, then retry.")
		}
		if !has {
			return out, actionable("no_clip",
				fmt.Sprintf("no clip in slot [%d,%d]", t, c),
				"Create or select a MIDI clip first, then retry.")
		}
		n := 0
		if res, err := client.Query("/live/clip/get/notes", int32(t), int32(c)); err == nil {
			// Stock reply starts with track, clip, then flat note fields (5 per note).
			if len(res) >= 2 {
				n = (len(res) - 2) / 5
			}
		}
		out.Summary = fmt.Sprintf("clear all notes in clip [%d,%d] (~%d notes)", t, c, n)
		out.Details = append(out.Details, fmt.Sprintf("estimated_notes: %d", n))
		out.Hint = "Call ableton_clear_clip_notes with the same indices and confirm=true."

	case "clear_scene_clips":
		if input.SceneIndex == nil {
			return out, actionable("missing_args", "scene_index is required for clear_scene_clips", "Pass the scene row to clear.")
		}
		scene := *input.SceneIndex
		numTracks, err := queryNumTracks(client)
		if err != nil {
			return out, wrapActionable(err, "query_failed", "Check AbletonOSC connectivity, then retry.")
		}
		tracks := input.TrackIndices
		if len(tracks) == 0 {
			tracks = make([]int, numTracks)
			for i := range tracks {
				tracks[i] = i
			}
		}
		occupied := 0
		for _, t := range tracks {
			has, err := queryHasClip(client, t, scene)
			if err != nil {
				return out, wrapActionable(err, "query_failed", "Check AbletonOSC connectivity, then retry.")
			}
			if has {
				occupied++
				name, _ := queryTrackName(client, t)
				out.Details = append(out.Details, fmt.Sprintf("track %d %q scene %d: has clip → delete", t, name, scene))
			}
		}
		out.Summary = fmt.Sprintf("clear %d clip(s) on scene %d across %d track(s)", occupied, scene, len(tracks))
		if occupied == 0 {
			out.Destructive = false
			out.Hint = "Nothing to delete."
		} else {
			out.Hint = "Call ableton_set_scene_clip_presence with present=false, the same scene_index/track_indices, and confirm=true."
		}

	default:
		return out, actionable("unknown_action",
			fmt.Sprintf("unsupported action %q", input.Action),
			"Use delete_track, delete_clip, delete_device, clear_clip_notes, or clear_scene_clips.")
	}

	return out, nil
}

func previewDeviceNames(client oscQuerier, trackIndex int) []string {
	res, err := client.Query("/live/track/get/devices/name", int32(trackIndex))
	if err != nil || len(res) < 1 {
		return nil
	}
	return toStringSlice(res[1:])
}

func NewAbletonPreviewDestructive(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_preview_destructive",
		"Ableton Live: preview a destructive action (delete track/clip/device, clear notes, clear scene clips) as a diff summary without executing. Review the summary, then call the matching tool with confirm=true.",
		func(_ *ai.ToolContext, input PreviewDestructiveInput) (PreviewDestructiveOutput, error) {
			if strings.TrimSpace(input.Action) == "" {
				return PreviewDestructiveOutput{}, errors.New("action is required")
			}
			return previewDestructive(client, input)
		},
	)
}
