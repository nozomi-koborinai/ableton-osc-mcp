package tools

import (
	"errors"
	"fmt"
	"math"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const maxMixVariationDelta = 0.2

type MixSnapshotInput struct {
	TrackIndices []int `json:"track_indices,omitempty" jsonschema:"description=Tracks to capture; omit to include all regular tracks"`
}

type MixTrackLevel struct {
	TrackIndex int     `json:"track_index"`
	Volume     float64 `json:"volume" jsonschema:"description=Track volume 0.0-1.0"`
}

type MixSnapshotOutput struct {
	Tracks []MixTrackLevel `json:"tracks"`
}

type MixVolumeChange struct {
	TrackIndex int     `json:"track_index" jsonschema:"minimum=0"`
	Delta      float64 `json:"delta" jsonschema:"description=Relative volume change for the B version (-0.2 to 0.2),minimum=-0.2,maximum=0.2"`
}

type ApplyMixVariationInput struct {
	Changes []MixVolumeChange `json:"changes" jsonschema:"description=One or more small volume changes for the B version"`
}

type ApplyMixVariationOutput struct {
	Before MixSnapshotOutput `json:"before" jsonschema:"description=Use this snapshot with ableton_restore_mix_snapshot to return to A"`
	After  MixSnapshotOutput `json:"after"`
}

type RestoreMixSnapshotInput struct {
	Tracks []MixTrackLevel `json:"tracks" jsonschema:"description=Snapshot tracks returned by ableton_capture_mix_snapshot or ableton_apply_mix_variation"`
}

type mixABClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonCaptureMixSnapshot(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_capture_mix_snapshot",
		"Ableton Live: capture current track volumes as an A/B mix snapshot",
		func(_ *ai.ToolContext, input MixSnapshotInput) (MixSnapshotOutput, error) {
			return captureMixSnapshot(client, input.TrackIndices)
		},
	)
}

func NewAbletonApplyMixVariation(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_apply_mix_variation",
		"Ableton Live: apply small track-volume changes for a B mix version and return the A snapshot for restoration",
		func(_ *ai.ToolContext, input ApplyMixVariationInput) (ApplyMixVariationOutput, error) {
			return applyMixVariation(client, input)
		},
	)
}

func NewAbletonRestoreMixSnapshot(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_restore_mix_snapshot",
		"Ableton Live: restore track volumes from an A/B mix snapshot",
		func(_ *ai.ToolContext, input RestoreMixSnapshotInput) (MixSnapshotOutput, error) {
			return restoreMixSnapshot(client, input.Tracks)
		},
	)
}

func captureMixSnapshot(client mixABClient, requested []int) (MixSnapshotOutput, error) {
	indices, err := resolveMixTracks(client, requested)
	if err != nil {
		return MixSnapshotOutput{}, err
	}
	return captureMixTracks(client, indices)
}

func applyMixVariation(client mixABClient, input ApplyMixVariationInput) (ApplyMixVariationOutput, error) {
	if len(input.Changes) == 0 {
		return ApplyMixVariationOutput{}, errors.New("changes must not be empty")
	}

	indices := make([]int, 0, len(input.Changes))
	deltas := make(map[int]float64, len(input.Changes))
	for _, change := range input.Changes {
		if change.TrackIndex < 0 {
			return ApplyMixVariationOutput{}, errors.New("changes track_index must be >= 0")
		}
		if math.Abs(change.Delta) > maxMixVariationDelta {
			return ApplyMixVariationOutput{}, fmt.Errorf("changes delta must be between -%.1f and %.1f", maxMixVariationDelta, maxMixVariationDelta)
		}
		if _, exists := deltas[change.TrackIndex]; exists {
			return ApplyMixVariationOutput{}, fmt.Errorf("duplicate track_index in changes: %d", change.TrackIndex)
		}
		indices = append(indices, change.TrackIndex)
		deltas[change.TrackIndex] = change.Delta
	}

	before, err := captureMixTracks(client, indices)
	if err != nil {
		return ApplyMixVariationOutput{}, err
	}
	afterTracks := make([]MixTrackLevel, 0, len(before.Tracks))
	for _, track := range before.Tracks {
		afterTracks = append(afterTracks, MixTrackLevel{
			TrackIndex: track.TrackIndex,
			Volume:     clampMixVolume(track.Volume + deltas[track.TrackIndex]),
		})
	}
	if err := setMixTracksTransactionally(client, before.Tracks, afterTracks); err != nil {
		return ApplyMixVariationOutput{}, err
	}
	return ApplyMixVariationOutput{
		Before: before,
		After:  MixSnapshotOutput{Tracks: afterTracks},
	}, nil
}

func restoreMixSnapshot(client mixABClient, tracks []MixTrackLevel) (MixSnapshotOutput, error) {
	if err := validateMixTracks(tracks); err != nil {
		return MixSnapshotOutput{}, err
	}
	indices := make([]int, 0, len(tracks))
	for _, track := range tracks {
		indices = append(indices, track.TrackIndex)
	}
	before, err := captureMixTracks(client, indices)
	if err != nil {
		return MixSnapshotOutput{}, err
	}
	if err := setMixTracksTransactionally(client, before.Tracks, tracks); err != nil {
		return MixSnapshotOutput{}, err
	}
	return MixSnapshotOutput{Tracks: copyMixTracks(tracks)}, nil
}

func resolveMixTracks(client mixABClient, requested []int) ([]int, error) {
	if len(requested) > 0 {
		seen := make(map[int]bool, len(requested))
		indices := make([]int, 0, len(requested))
		for _, index := range requested {
			if index < 0 {
				return nil, errors.New("track_indices must be >= 0")
			}
			if seen[index] {
				continue
			}
			seen[index] = true
			indices = append(indices, index)
		}
		return indices, nil
	}

	namesRes, err := client.Query("/live/song/get/track_names")
	if err != nil {
		return nil, fmt.Errorf("list tracks: %w", err)
	}
	if len(namesRes) == 0 {
		return nil, errors.New("no tracks available")
	}
	indices := make([]int, len(namesRes))
	for i := range namesRes {
		indices[i] = i
	}
	return indices, nil
}

func captureMixTracks(client mixABClient, indices []int) (MixSnapshotOutput, error) {
	tracks := make([]MixTrackLevel, 0, len(indices))
	for _, index := range indices {
		volume, err := queryMixTrackVolume(client, index)
		if err != nil {
			return MixSnapshotOutput{}, fmt.Errorf("track %d: %w", index, err)
		}
		tracks = append(tracks, MixTrackLevel{TrackIndex: index, Volume: volume})
	}
	return MixSnapshotOutput{Tracks: tracks}, nil
}

func queryMixTrackVolume(client mixABClient, trackIndex int) (float64, error) {
	res, err := client.Query("/live/track/get/volume", int32(trackIndex))
	if err != nil {
		return 0, fmt.Errorf("get volume: %w", err)
	}
	if err := ensureResponseLen(res, 2); err != nil {
		return 0, fmt.Errorf("get volume: %w", err)
	}
	return abletonosc.AsFloat64(res[1])
}

func setMixTracksTransactionally(client mixABClient, before, target []MixTrackLevel) error {
	if len(before) != len(target) {
		return errors.New("mix snapshot lengths differ")
	}
	changed := make([]MixTrackLevel, 0, len(target))
	for i, track := range target {
		if before[i].TrackIndex != track.TrackIndex {
			return errors.New("mix snapshot track indices differ")
		}
		if err := client.Send("/live/track/set/volume", int32(track.TrackIndex), float32(track.Volume)); err != nil {
			for j := len(changed) - 1; j >= 0; j-- {
				original := before[j]
				_ = client.Send("/live/track/set/volume", int32(original.TrackIndex), float32(original.Volume))
			}
			return fmt.Errorf("set track %d volume: %w", track.TrackIndex, err)
		}
		changed = append(changed, track)
	}
	return nil
}

func validateMixTracks(tracks []MixTrackLevel) error {
	if len(tracks) == 0 {
		return errors.New("tracks must not be empty")
	}
	seen := make(map[int]bool, len(tracks))
	for _, track := range tracks {
		if track.TrackIndex < 0 {
			return errors.New("tracks track_index must be >= 0")
		}
		if track.Volume < 0 || track.Volume > 1 {
			return errors.New("tracks volume must be 0.0 to 1.0")
		}
		if seen[track.TrackIndex] {
			return fmt.Errorf("duplicate track_index in tracks: %d", track.TrackIndex)
		}
		seen[track.TrackIndex] = true
	}
	return nil
}

func clampMixVolume(volume float64) float64 {
	if volume < 0 {
		return 0
	}
	if volume > 1 {
		return 1
	}
	return volume
}

func copyMixTracks(tracks []MixTrackLevel) []MixTrackLevel {
	out := make([]MixTrackLevel, len(tracks))
	copy(out, tracks)
	return out
}
