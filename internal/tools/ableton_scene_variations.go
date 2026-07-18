package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const defaultSceneEnergyVelocityDelta = 12

type CreateSceneEnergyVariationInput struct {
	SourceSceneIndex int    `json:"source_scene_index" jsonschema:"minimum=0"`
	TrackIndices     []int  `json:"track_indices" jsonschema:"description=MIDI tracks whose clips in the source scene will change, minItems=1"`
	Variation        string `json:"variation" jsonschema:"description=One change only: lift or pullback"`
	VelocityDelta    *int   `json:"velocity_delta,omitempty" jsonschema:"description=Velocity change in MIDI units (default 12),minimum=1,maximum=30"`
	Fire             bool   `json:"fire,omitempty" jsonschema:"description=Fire the duplicated B scene after creating it"`
}

type CreateSceneEnergyVariationOutput struct {
	SourceSceneIndex int    `json:"source_scene_index"`
	TargetSceneIndex int    `json:"target_scene_index" jsonschema:"description=Duplicated B scene, inserted directly after the source scene"`
	Variation        string `json:"variation"`
	VelocityDelta    int    `json:"velocity_delta"`
	TracksChanged    []int  `json:"tracks_changed"`
	TracksSkipped    []int  `json:"tracks_skipped,omitempty"`
	NotesChanged     int    `json:"notes_changed"`
	Fired            bool   `json:"fired"`
}

type sceneVariationTrack struct {
	Index int
	Notes []MidiNote
}

func NewAbletonCreateSceneEnergyVariation(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_create_scene_energy_variation",
		"Ableton Live: duplicate a scene and create an A/B energy lift or pullback by changing selected MIDI-track velocities",
		func(_ *ai.ToolContext, input CreateSceneEnergyVariationInput) (CreateSceneEnergyVariationOutput, error) {
			return createSceneEnergyVariation(client, input)
		},
	)
}

func createSceneEnergyVariation(client variationClient, input CreateSceneEnergyVariationInput) (CreateSceneEnergyVariationOutput, error) {
	if input.SourceSceneIndex < 0 {
		return CreateSceneEnergyVariationOutput{}, errors.New("source_scene_index must be >= 0")
	}
	variation := strings.ToLower(strings.TrimSpace(input.Variation))
	if variation != "lift" && variation != "pullback" {
		return CreateSceneEnergyVariationOutput{}, errors.New("variation must be lift or pullback")
	}
	velocityDelta := defaultSceneEnergyVelocityDelta
	if input.VelocityDelta != nil {
		velocityDelta = *input.VelocityDelta
	}
	if velocityDelta < 1 || velocityDelta > 30 {
		return CreateSceneEnergyVariationOutput{}, errors.New("velocity_delta must be between 1 and 30")
	}

	numScenes, err := queryNumScenes(client)
	if err != nil {
		return CreateSceneEnergyVariationOutput{}, err
	}
	if input.SourceSceneIndex >= numScenes {
		return CreateSceneEnergyVariationOutput{}, fmt.Errorf("source_scene_index %d is outside %d scenes", input.SourceSceneIndex, numScenes)
	}
	trackIndices, err := validateSceneVariationTracks(input.TrackIndices)
	if err != nil {
		return CreateSceneEnergyVariationOutput{}, err
	}

	sourceTracks := make([]sceneVariationTrack, 0, len(trackIndices))
	skipped := make([]int, 0)
	for _, trackIndex := range trackIndices {
		hasClip, err := queryClipHasClip(client, trackIndex, input.SourceSceneIndex)
		if err != nil {
			return CreateSceneEnergyVariationOutput{}, fmt.Errorf("track %d source clip: %w", trackIndex, err)
		}
		if !hasClip {
			skipped = append(skipped, trackIndex)
			continue
		}
		res, err := client.Query("/live/clip/get/notes", int32(trackIndex), int32(input.SourceSceneIndex))
		if err != nil {
			return CreateSceneEnergyVariationOutput{}, fmt.Errorf("track %d must contain a MIDI clip: %w", trackIndex, err)
		}
		_, _, notes, err := parseClipNotesResponse(res)
		if err != nil {
			return CreateSceneEnergyVariationOutput{}, fmt.Errorf("track %d source notes: %w", trackIndex, err)
		}
		if len(notes) == 0 {
			skipped = append(skipped, trackIndex)
			continue
		}
		sourceTracks = append(sourceTracks, sceneVariationTrack{Index: trackIndex, Notes: notes})
	}
	if len(sourceTracks) == 0 {
		return CreateSceneEnergyVariationOutput{}, errors.New("no selected tracks contain MIDI notes in the source scene")
	}

	delta := velocityDelta
	if variation == "pullback" {
		delta = -velocityDelta
	}
	changedNotes := 0
	variedTracks := make([]sceneVariationTrack, 0, len(sourceTracks))
	for _, track := range sourceTracks {
		varied, changed := shiftNoteVelocities(track.Notes, delta)
		if changed == 0 {
			skipped = append(skipped, track.Index)
			continue
		}
		variedTracks = append(variedTracks, sceneVariationTrack{Index: track.Index, Notes: varied})
		changedNotes += changed
	}
	if len(variedTracks) == 0 {
		return CreateSceneEnergyVariationOutput{}, errors.New("variation would not change any selected MIDI notes")
	}

	if err := client.Send("/live/song/duplicate_scene", int32(input.SourceSceneIndex)); err != nil {
		return CreateSceneEnergyVariationOutput{}, fmt.Errorf("duplicate source scene: %w", err)
	}
	afterNumScenes, err := queryNumScenes(client)
	if err != nil {
		return CreateSceneEnergyVariationOutput{}, fmt.Errorf("verify duplicated scene: %w", err)
	}
	if afterNumScenes != numScenes+1 {
		return CreateSceneEnergyVariationOutput{}, fmt.Errorf("scene count after duplicate = %d, want %d", afterNumScenes, numScenes+1)
	}
	targetSceneIndex := input.SourceSceneIndex + 1

	for _, track := range variedTracks {
		original := sourceNotesForTrack(sourceTracks, track.Index)
		if err := replaceVariationNotes(client, track.Index, targetSceneIndex, original, track.Notes); err != nil {
			return CreateSceneEnergyVariationOutput{}, discardSceneVariation(
				client,
				targetSceneIndex,
				fmt.Errorf("track %d update failed: %w", track.Index, err),
			)
		}
	}
	if err := client.Send(
		"/live/scene/set/name",
		int32(targetSceneIndex),
		"Variation: energy "+variation,
	); err != nil {
		return CreateSceneEnergyVariationOutput{}, discardSceneVariation(client, targetSceneIndex, fmt.Errorf("set name failed: %w", err))
	}

	fired := false
	if input.Fire {
		if err := client.Send("/live/scene/fire", int32(targetSceneIndex)); err != nil {
			return CreateSceneEnergyVariationOutput{}, discardSceneVariation(client, targetSceneIndex, fmt.Errorf("fire target scene: %w", err))
		}
		fired = true
	}

	changedTracks := make([]int, 0, len(variedTracks))
	for _, track := range variedTracks {
		changedTracks = append(changedTracks, track.Index)
	}
	return CreateSceneEnergyVariationOutput{
		SourceSceneIndex: input.SourceSceneIndex,
		TargetSceneIndex: targetSceneIndex,
		Variation:        variation,
		VelocityDelta:    velocityDelta,
		TracksChanged:    changedTracks,
		TracksSkipped:    skipped,
		NotesChanged:     changedNotes,
		Fired:            fired,
	}, nil
}

func discardSceneVariation(client variationClient, sceneIndex int, cause error) error {
	if err := client.Send("/live/song/delete_scene", int32(sceneIndex)); err != nil {
		return fmt.Errorf("scene variation failed: %w; cleanup of scene %d also failed: %v", cause, sceneIndex, err)
	}
	return fmt.Errorf("scene variation failed: %w; duplicated scene %d removed", cause, sceneIndex)
}

func queryNumScenes(client variationClient) (int, error) {
	res, err := client.Query("/live/song/get/num_scenes")
	if err != nil {
		return 0, fmt.Errorf("get scene count: %w", err)
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return 0, fmt.Errorf("get scene count: %w", err)
	}
	return abletonosc.AsInt(res[0])
}

func validateSceneVariationTracks(trackIndices []int) ([]int, error) {
	if len(trackIndices) == 0 {
		return nil, errors.New("track_indices must not be empty")
	}
	seen := make(map[int]bool, len(trackIndices))
	out := make([]int, 0, len(trackIndices))
	for _, trackIndex := range trackIndices {
		if trackIndex < 0 {
			return nil, errors.New("track_indices must be >= 0")
		}
		if seen[trackIndex] {
			return nil, fmt.Errorf("duplicate track_index: %d", trackIndex)
		}
		seen[trackIndex] = true
		out = append(out, trackIndex)
	}
	return out, nil
}

func shiftNoteVelocities(notes []MidiNote, delta int) ([]MidiNote, int) {
	out := copyMidiNotes(notes)
	changed := 0
	for i := range out {
		velocity := out[i].Velocity + delta
		if velocity < 1 {
			velocity = 1
		}
		if velocity > 127 {
			velocity = 127
		}
		if velocity != out[i].Velocity {
			out[i].Velocity = velocity
			changed++
		}
	}
	return out, changed
}

func sourceNotesForTrack(tracks []sceneVariationTrack, trackIndex int) []MidiNote {
	for _, track := range tracks {
		if track.Index == trackIndex {
			return track.Notes
		}
	}
	return nil
}
