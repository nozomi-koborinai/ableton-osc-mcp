package tools

import (
	"errors"
	"fmt"
	"math"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	maxMixVariationDelta   = 0.2
	maxMixVariationDeltaDB = 12.0
)

type MixSnapshotInput struct {
	TrackIndices []int `json:"track_indices,omitempty" jsonschema:"description=Tracks to capture; omit to include all regular tracks"`
}

type MixTrackLevel struct {
	TrackIndex int     `json:"track_index"`
	Volume     float64 `json:"volume" jsonschema:"description=Track volume\\, raw 0.0-1.0. Restoring a snapshot uses this value"`
	VolumeDB   string  `json:"volume_db,omitempty" jsonschema:"description=The level as Live shows it\\, e.g. -6.0 dB (needs the dB mixer patch). Informational; ignored when restoring"`
}

type MixSnapshotOutput struct {
	Tracks []MixTrackLevel `json:"tracks"`
}

type MixVolumeChange struct {
	TrackIndex int     `json:"track_index" jsonschema:"minimum=0"`
	DeltaDB    float64 `json:"delta_db,omitempty" jsonschema:"description=Volume change for the B version in dB (-12 to 12). Give delta_db or delta\\, not both,minimum=-12,maximum=12"`
	Delta      float64 `json:"delta,omitempty" jsonschema:"description=Raw volume change for the B version (-0.2 to 0.2),minimum=-0.2,maximum=0.2"`
}

type ApplyMixVariationInput struct {
	Changes []MixVolumeChange `json:"changes" jsonschema:"description=One or more small volume changes for the B version"`
}

type ApplyMixVariationOutput struct {
	Before           MixSnapshotOutput `json:"before" jsonschema:"description=Use this snapshot with ableton_restore_mix_snapshot to return to A"`
	After            MixSnapshotOutput `json:"after"`
	PreferencePrompt string            `json:"preference_prompt"`
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
		"Ableton Live: capture current track volumes (raw, and in dB as Live displays them) as an A/B mix snapshot",
		func(_ *ai.ToolContext, input MixSnapshotInput) (MixSnapshotOutput, error) {
			return captureMixSnapshot(client, input.TrackIndices)
		},
	)
}

func NewAbletonApplyMixVariation(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_apply_mix_variation",
		"Ableton Live: mix A/B entry — apply small track-volume changes for B (delta_db in dB, or a raw delta) and return the A snapshot (not covered by ableton_compare_ab_variation; restore with ableton_restore_mix_snapshot)",
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
	changes := make(map[int]MixVolumeChange, len(input.Changes))
	for _, change := range input.Changes {
		if change.TrackIndex < 0 {
			return ApplyMixVariationOutput{}, errors.New("changes track_index must be >= 0")
		}
		if (change.Delta == 0) == (change.DeltaDB == 0) {
			return ApplyMixVariationOutput{}, fmt.Errorf("track %d: give delta_db or delta, one of them and not zero", change.TrackIndex)
		}
		if math.Abs(change.Delta) > maxMixVariationDelta {
			return ApplyMixVariationOutput{}, fmt.Errorf("changes delta must be between -%.1f and %.1f", maxMixVariationDelta, maxMixVariationDelta)
		}
		if math.Abs(change.DeltaDB) > maxMixVariationDeltaDB {
			return ApplyMixVariationOutput{}, fmt.Errorf("changes delta_db must be between -%.0f and %.0f", maxMixVariationDeltaDB, maxMixVariationDeltaDB)
		}
		if _, exists := changes[change.TrackIndex]; exists {
			return ApplyMixVariationOutput{}, fmt.Errorf("duplicate track_index in changes: %d", change.TrackIndex)
		}
		indices = append(indices, change.TrackIndex)
		changes[change.TrackIndex] = change
	}

	before, err := captureMixTracks(client, indices)
	if err != nil {
		return ApplyMixVariationOutput{}, err
	}
	// Work out every target first, so a dB request that cannot be resolved
	// fails before any fader has moved.
	afterTracks := make([]MixTrackLevel, 0, len(before.Tracks))
	for _, track := range before.Tracks {
		change := changes[track.TrackIndex]
		target := clampMixVolume(track.Volume + change.Delta)
		if change.DeltaDB != 0 {
			level, err := queryMixerLevel(client, trackVolumeTarget(track.TrackIndex))
			if err != nil {
				return ApplyMixVariationOutput{}, err
			}
			if level.Silent {
				return ApplyMixVariationOutput{}, actionable("delta_from_silence",
					fmt.Sprintf("track %d is at -inf dB, so a dB change has nothing to start from", track.TrackIndex),
					"Set an absolute level with ableton_set_track_volume first.")
			}
			target, err = resolveRawForDB(client, trackVolumeTarget(track.TrackIndex), level.DB+change.DeltaDB)
			if err != nil {
				return ApplyMixVariationOutput{}, err
			}
		}
		afterTracks = append(afterTracks, MixTrackLevel{TrackIndex: track.TrackIndex, Volume: target})
	}
	if err := setMixTracksTransactionally(client, before.Tracks, afterTracks); err != nil {
		return ApplyMixVariationOutput{}, err
	}
	fillVolumeDB(client, afterTracks)
	return ApplyMixVariationOutput{
		Before:           before,
		After:            MixSnapshotOutput{Tracks: afterTracks},
		PreferencePrompt: "After comparing A/B, record with ableton_record_variation_preference using instrument=mix variation=volume, then restore A with ableton_restore_mix_snapshot.",
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
	fillVolumeDB(client, tracks)
	return MixSnapshotOutput{Tracks: tracks}, nil
}

// fillVolumeDB adds the displayed level to each track. It is a bonus: on an
// old patch, or if the reply is odd, the snapshot simply goes without it.
func fillVolumeDB(client oscQuerier, tracks []MixTrackLevel) {
	levels, err := queryTrackVolumesDB(client)
	if err != nil {
		return
	}
	for i := range tracks {
		if idx := tracks[i].TrackIndex; idx >= 0 && idx < len(levels) {
			tracks[i].VolumeDB = levels[idx].Display
		}
	}
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
