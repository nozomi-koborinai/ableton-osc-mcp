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

type DuplicateTrackForProcessingInput struct {
	TrackIndex int    `json:"track_index" jsonschema:"description=Source track to duplicate,minimum=0"`
	WetName    string `json:"wet_name,omitempty" jsonschema:"description=Name for the duplicated (processed/wet) track; default '<name> wet'"`
	DrySuffix  string `json:"dry_suffix,omitempty" jsonschema:"description=If set, rename the original (dry) track by appending this, e.g. ' dry'"`
}

type DuplicateTrackForProcessingOutput struct {
	DryIndex int    `json:"dry_index"`
	DryName  string `json:"dry_name"`
	WetIndex int    `json:"wet_index"`
	WetName  string `json:"wet_name"`
}

type oscQuerier interface {
	Query(address string, args ...interface{}) ([]interface{}, error)
}

type trackDupeClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
	QueryWithTimeout(timeout time.Duration, address string, args ...interface{}) ([]interface{}, error)
}

func queryTrackName(client oscQuerier, trackIndex int) (string, error) {
	res, err := client.Query("/live/track/get/name", int32(trackIndex))
	if err != nil {
		return "", err
	}
	if err := ensureResponseLen(res, 2); err != nil {
		return "", err
	}
	return fmt.Sprint(res[1]), nil
}

func queryNumTracks(client oscQuerier) (int, error) {
	res, err := client.Query("/live/song/get/num_tracks")
	if err != nil {
		return 0, err
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return 0, err
	}
	return abletonosc.AsInt(res[0])
}

// duplicateTrackForProcessing duplicates a track so the original stays dry and
// the copy (inserted right after) becomes the processed/wet version. Both start
// identical; the caller then adds/removes effects on one. Uses stock
// /live/song/duplicate_track.
func duplicateTrackForProcessing(client trackDupeClient, input DuplicateTrackForProcessingInput) (DuplicateTrackForProcessingOutput, error) {
	if input.TrackIndex < 0 {
		return DuplicateTrackForProcessingOutput{}, errors.New("track_index must be >= 0")
	}
	origName, err := queryTrackName(client, input.TrackIndex)
	if err != nil {
		return DuplicateTrackForProcessingOutput{}, fmt.Errorf("get track name: %w", err)
	}
	before, err := queryNumTracks(client)
	if err != nil {
		return DuplicateTrackForProcessingOutput{}, fmt.Errorf("get num tracks: %w", err)
	}
	if input.TrackIndex >= before {
		return DuplicateTrackForProcessingOutput{}, fmt.Errorf("track_index %d out of range (%d tracks)", input.TrackIndex, before)
	}

	if err := client.Send("/live/song/duplicate_track", int32(input.TrackIndex)); err != nil {
		return DuplicateTrackForProcessingOutput{}, fmt.Errorf("duplicate track: %w", err)
	}

	// duplicate_track is fire-and-forget; confirm a track was actually added.
	after, err := queryNumTracks(client)
	if err != nil {
		return DuplicateTrackForProcessingOutput{}, fmt.Errorf("verify duplicate: %w", err)
	}
	if after != before+1 {
		return DuplicateTrackForProcessingOutput{}, fmt.Errorf("duplicate did not add a track (before=%d after=%d)", before, after)
	}

	dryIndex := input.TrackIndex
	wetIndex := input.TrackIndex + 1

	wetName := strings.TrimSpace(input.WetName)
	if wetName == "" {
		wetName = strings.TrimSpace(origName + " wet")
	}
	if err := client.Send("/live/track/set/name", int32(wetIndex), wetName); err != nil {
		return DuplicateTrackForProcessingOutput{}, fmt.Errorf("name wet track: %w", err)
	}

	dryName := origName
	if suffix := input.DrySuffix; suffix != "" {
		dryName = origName + suffix
		if err := client.Send("/live/track/set/name", int32(dryIndex), dryName); err != nil {
			return DuplicateTrackForProcessingOutput{}, fmt.Errorf("name dry track: %w", err)
		}
	}

	return DuplicateTrackForProcessingOutput{
		DryIndex: dryIndex,
		DryName:  dryName,
		WetIndex: wetIndex,
		WetName:  wetName,
	}, nil
}

func NewAbletonDuplicateTrackForProcessing(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_duplicate_track_for_processing",
		"Ableton Live: duplicate a track into a dry/wet pair. The original stays as the dry track (optionally suffixed) and the copy right after it becomes the processed 'wet' track. Both start identical (same clips and devices) so you can process one; returns both indices and names.",
		func(_ *ai.ToolContext, input DuplicateTrackForProcessingInput) (DuplicateTrackForProcessingOutput, error) {
			return duplicateTrackForProcessing(client, input)
		},
	)
}

type DuplicateTrackInput struct {
	TrackIndex int    `json:"track_index" jsonschema:"description=Track to duplicate; the copy is inserted immediately after,minimum=0"`
	Name       string `json:"name,omitempty" jsonschema:"description=Optional name for the duplicated track"`
}

type DuplicateTrackOutput struct {
	SourceIndex int    `json:"source_index"`
	NewIndex    int    `json:"new_index"`
	Name        string `json:"name"`
	TracksAfter int    `json:"tracks_after"`
}

func NewAbletonDuplicateTrack(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_duplicate_track",
		"Ableton Live: duplicate a track (clips + devices). The copy is inserted right after the source; confirms success via track count before/after.",
		func(_ *ai.ToolContext, input DuplicateTrackInput) (DuplicateTrackOutput, error) {
			if input.TrackIndex < 0 {
				return DuplicateTrackOutput{}, errors.New("track_index must be >= 0")
			}
			before, err := queryNumTracks(client)
			if err != nil {
				return DuplicateTrackOutput{}, fmt.Errorf("get num tracks: %w", err)
			}
			if input.TrackIndex >= before {
				return DuplicateTrackOutput{}, fmt.Errorf("track_index %d out of range (%d tracks)", input.TrackIndex, before)
			}
			if err := client.Send("/live/song/duplicate_track", int32(input.TrackIndex)); err != nil {
				return DuplicateTrackOutput{}, fmt.Errorf("duplicate track: %w", err)
			}
			after, err := queryNumTracks(client)
			if err != nil {
				return DuplicateTrackOutput{}, fmt.Errorf("verify duplicate: %w", err)
			}
			if after != before+1 {
				return DuplicateTrackOutput{}, fmt.Errorf("duplicate did not add a track (before=%d after=%d)", before, after)
			}
			newIndex := input.TrackIndex + 1
			name := strings.TrimSpace(input.Name)
			if name != "" {
				if err := client.Send("/live/track/set/name", int32(newIndex), name); err != nil {
					return DuplicateTrackOutput{}, fmt.Errorf("set track name: %w", err)
				}
			} else {
				name, _ = queryTrackName(client, newIndex)
			}
			return DuplicateTrackOutput{
				SourceIndex: input.TrackIndex,
				NewIndex:    newIndex,
				Name:        name,
				TracksAfter: after,
			}, nil
		},
	)
}

type DeleteTrackInput struct {
	TrackIndex int  `json:"track_index" jsonschema:"description=Track to delete (destructive),minimum=0"`
	Confirm    bool `json:"confirm,omitempty" jsonschema:"description=Must be true to execute; omit/false returns a preview error without deleting. Prefer ableton_preview_destructive first."`
}

type DeleteTrackOutput struct {
	TrackIndex   int    `json:"track_index"`
	Name         string `json:"name,omitempty"`
	TracksBefore int    `json:"tracks_before"`
	TracksAfter  int    `json:"tracks_after"`
}

func NewAbletonDeleteTrack(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_delete_track",
		"Ableton Live: delete a track by index (destructive). Requires confirm=true. Without confirm, returns a preview. Confirms success via track count before/after. Prefer mute or dry/wet duplicate when you only need to park material.",
		func(_ *ai.ToolContext, input DeleteTrackInput) (DeleteTrackOutput, error) {
			if input.TrackIndex < 0 {
				return DeleteTrackOutput{}, errors.New("track_index must be >= 0")
			}
			before, err := queryNumTracks(client)
			if err != nil {
				return DeleteTrackOutput{}, fmt.Errorf("get num tracks: %w", err)
			}
			if input.TrackIndex >= before {
				return DeleteTrackOutput{}, fmt.Errorf("track_index %d out of range (%d tracks)", input.TrackIndex, before)
			}
			name, _ := queryTrackName(client, input.TrackIndex)
			if err := requireConfirm(input.Confirm, "delete_track",
				fmt.Sprintf("track %d %q (%d total tracks → %d)", input.TrackIndex, name, before, before-1)); err != nil {
				return DeleteTrackOutput{}, err
			}
			if err := client.Send("/live/song/delete_track", int32(input.TrackIndex)); err != nil {
				return DeleteTrackOutput{}, fmt.Errorf("delete track: %w", err)
			}
			after, err := queryNumTracks(client)
			if err != nil {
				return DeleteTrackOutput{}, fmt.Errorf("verify delete: %w", err)
			}
			if after != before-1 {
				return DeleteTrackOutput{}, fmt.Errorf("delete did not remove a track (before=%d after=%d)", before, after)
			}
			return DeleteTrackOutput{
				TrackIndex:   input.TrackIndex,
				Name:         name,
				TracksBefore: before,
				TracksAfter:  after,
			}, nil
		},
	)
}

type DeleteClipInput struct {
	TrackIndex int  `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex  int  `json:"clip_index" jsonschema:"description=Scene/clip-slot index to clear,minimum=0"`
	Confirm    bool `json:"confirm,omitempty" jsonschema:"description=Must be true to execute; omit/false returns a preview error without deleting"`
}

type DeleteClipOutput struct {
	TrackIndex int  `json:"track_index"`
	ClipIndex  int  `json:"clip_index"`
	HadClip    bool `json:"had_clip"`
	Deleted    bool `json:"deleted"`
}

func NewAbletonDeleteClip(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_delete_clip",
		"Ableton Live: delete the clip in a clip slot (leaves the slot empty). Requires confirm=true when a clip is present. Confirms via has_clip before/after. Idempotent when the slot is already empty.",
		func(_ *ai.ToolContext, input DeleteClipInput) (DeleteClipOutput, error) {
			if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
				return DeleteClipOutput{}, err
			}
			had, err := slotHasClip(client, input.TrackIndex, input.ClipIndex)
			if err != nil {
				return DeleteClipOutput{}, fmt.Errorf("check has_clip: %w", err)
			}
			out := DeleteClipOutput{TrackIndex: input.TrackIndex, ClipIndex: input.ClipIndex, HadClip: had}
			if !had {
				return out, nil
			}
			if err := requireConfirm(input.Confirm, "delete_clip",
				fmt.Sprintf("clip on track %d slot %d", input.TrackIndex, input.ClipIndex)); err != nil {
				return DeleteClipOutput{}, err
			}
			if err := client.Send("/live/clip_slot/delete_clip", int32(input.TrackIndex), int32(input.ClipIndex)); err != nil {
				return DeleteClipOutput{}, fmt.Errorf("delete clip: %w", err)
			}
			still, err := slotHasClip(client, input.TrackIndex, input.ClipIndex)
			if err != nil {
				return DeleteClipOutput{}, fmt.Errorf("verify delete: %w", err)
			}
			if still {
				return DeleteClipOutput{}, errors.New("delete_clip did not clear the slot")
			}
			out.Deleted = true
			return out, nil
		},
	)
}
