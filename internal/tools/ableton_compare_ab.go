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

type CompareABVariationInput struct {
	Kind             string   `json:"kind" jsonschema:"description=What to compare: drum, bass, or scene"`
	Variation        string   `json:"variation" jsonschema:"description=One-axis variation for the chosen kind"`
	TrackIndex       *int     `json:"track_index,omitempty" jsonschema:"description=Required for drum/bass; omit for scene,minimum=0"`
	SourceClipIndex  *int     `json:"source_clip_index,omitempty" jsonschema:"description=Required for drum/bass A clip,minimum=0"`
	TargetClipIndex  *int     `json:"target_clip_index,omitempty" jsonschema:"description=Required for drum/bass empty B slot,minimum=0"`
	SourceSceneIndex *int     `json:"source_scene_index,omitempty" jsonschema:"description=Required for scene A,minimum=0"`
	TrackIndices     []int    `json:"track_indices,omitempty" jsonschema:"description=Required for scene: MIDI tracks to vary"`
	Strength         *float64 `json:"strength,omitempty" jsonschema:"description=Drum/bass variation intensity 0-1,minimum=0,maximum=1"`
	VelocityDelta    *int     `json:"velocity_delta,omitempty" jsonschema:"description=Scene velocity change (default 12),minimum=1,maximum=30"`
	Seed             *int64   `json:"seed,omitempty" jsonschema:"description=Optional RNG seed for drum/bass groove or density"`
	BarsPerVersion   *int     `json:"bars_per_version,omitempty" jsonschema:"description=Bars to hear each version (default 2),minimum=1,maximum=8"`
	Cycles           *int     `json:"cycles,omitempty" jsonschema:"description=How many A→B cycles to play (default 1),minimum=1,maximum=4"`
	StopAfter        bool     `json:"stop_after,omitempty" jsonschema:"description=Stop playback after the final B version"`
}

type CompareABVariationOutput struct {
	Kind             string           `json:"kind"`
	Variation        string           `json:"variation"`
	TrackIndex       *int             `json:"track_index,omitempty"`
	SourceIndex      int              `json:"source_index"`
	VariationIndex   int              `json:"variation_index"`
	NotesChanged     int              `json:"notes_changed,omitempty"`
	NotesAdded       int              `json:"notes_added,omitempty"`
	NotesSkipped     int              `json:"notes_skipped,omitempty"`
	TracksChanged    []int            `json:"tracks_changed,omitempty"`
	Seed             int64            `json:"seed,omitempty"`
	Audition         AuditionABOutput `json:"audition"`
	PreferencePrompt string           `json:"preference_prompt"`
}

type compareABClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonCompareABVariation(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_compare_ab_variation",
		"Ableton Live: preferred entry for drum/bass/scene A/B — create one variation into an empty target, audition A then B, and return a preference prompt (does not record the choice; use ableton_record_variation_preference after the listener chooses)",
		func(_ *ai.ToolContext, input CompareABVariationInput) (CompareABVariationOutput, error) {
			return compareABVariation(client, input, time.Sleep)
		},
	)
}

func compareABVariation(client compareABClient, input CompareABVariationInput, sleep auditionSleeper) (CompareABVariationOutput, error) {
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	variation := strings.ToLower(strings.TrimSpace(input.Variation))
	if variation == "" {
		return CompareABVariationOutput{}, errors.New("variation is required")
	}

	var (
		out           CompareABVariationOutput
		auditionInput AuditionABInput
		err           error
	)
	out.Kind = kind
	out.Variation = variation

	switch kind {
	case "drum":
		out, auditionInput, err = createDrumCompare(client, input, variation)
	case "bass":
		out, auditionInput, err = createBassCompare(client, input, variation)
	case "scene":
		out, auditionInput, err = createSceneCompare(client, input, variation)
	default:
		return CompareABVariationOutput{}, errors.New("kind must be drum, bass, or scene")
	}
	if err != nil {
		return CompareABVariationOutput{}, err
	}

	auditionInput.BarsPerVersion = input.BarsPerVersion
	auditionInput.Cycles = input.Cycles
	auditionInput.StopAfter = input.StopAfter
	auditionInput.Instrument = kind
	auditionInput.Variation = variation

	audition, err := auditionAB(client, auditionInput, sleep)
	if err != nil {
		return CompareABVariationOutput{}, fmt.Errorf("audition after %s variation: %w", kind, err)
	}
	out.Audition = audition
	out.PreferencePrompt = audition.PreferencePrompt
	return out, nil
}

func createDrumCompare(client compareABClient, input CompareABVariationInput, variation string) (CompareABVariationOutput, AuditionABInput, error) {
	trackIndex, sourceClip, targetClip, err := requireClipCompareSlots(input)
	if err != nil {
		return CompareABVariationOutput{}, AuditionABInput{}, err
	}
	if input.SourceSceneIndex != nil || len(input.TrackIndices) > 0 || input.VelocityDelta != nil {
		return CompareABVariationOutput{}, AuditionABInput{}, errors.New("scene fields must be omitted for drum comparisons")
	}
	created, err := createDrumVariation(client, CreateDrumVariationInput{
		TrackIndex:      trackIndex,
		SourceClipIndex: sourceClip,
		TargetClipIndex: targetClip,
		Variation:       variation,
		Strength:        input.Strength,
		Seed:            input.Seed,
		Fire:            false,
	})
	if err != nil {
		return CompareABVariationOutput{}, AuditionABInput{}, err
	}
	track := created.TrackIndex
	return CompareABVariationOutput{
			Kind:           "drum",
			Variation:      created.Variation,
			TrackIndex:     &track,
			SourceIndex:    created.SourceClipIndex,
			VariationIndex: created.TargetClipIndex,
			NotesChanged:   created.NotesChanged,
			NotesAdded:     created.NotesAdded,
			Seed:           created.Seed,
		}, AuditionABInput{
			TargetType:     "clip",
			TrackIndex:     &track,
			SourceIndex:    created.SourceClipIndex,
			VariationIndex: created.TargetClipIndex,
		}, nil
}

func createBassCompare(client compareABClient, input CompareABVariationInput, variation string) (CompareABVariationOutput, AuditionABInput, error) {
	trackIndex, sourceClip, targetClip, err := requireClipCompareSlots(input)
	if err != nil {
		return CompareABVariationOutput{}, AuditionABInput{}, err
	}
	if input.SourceSceneIndex != nil || len(input.TrackIndices) > 0 || input.VelocityDelta != nil {
		return CompareABVariationOutput{}, AuditionABInput{}, errors.New("scene fields must be omitted for bass comparisons")
	}
	created, err := createBassVariation(client, CreateBassVariationInput{
		TrackIndex:      trackIndex,
		SourceClipIndex: sourceClip,
		TargetClipIndex: targetClip,
		Variation:       variation,
		Strength:        input.Strength,
		Seed:            input.Seed,
		Fire:            false,
	})
	if err != nil {
		return CompareABVariationOutput{}, AuditionABInput{}, err
	}
	track := created.TrackIndex
	return CompareABVariationOutput{
			Kind:           "bass",
			Variation:      created.Variation,
			TrackIndex:     &track,
			SourceIndex:    created.SourceClipIndex,
			VariationIndex: created.TargetClipIndex,
			NotesChanged:   created.NotesChanged,
			NotesSkipped:   created.NotesSkipped,
			Seed:           created.Seed,
		}, AuditionABInput{
			TargetType:     "clip",
			TrackIndex:     &track,
			SourceIndex:    created.SourceClipIndex,
			VariationIndex: created.TargetClipIndex,
		}, nil
}

func createSceneCompare(client compareABClient, input CompareABVariationInput, variation string) (CompareABVariationOutput, AuditionABInput, error) {
	if input.SourceSceneIndex == nil {
		return CompareABVariationOutput{}, AuditionABInput{}, errors.New("source_scene_index is required for scene comparisons")
	}
	if input.TrackIndex != nil || input.SourceClipIndex != nil || input.TargetClipIndex != nil {
		return CompareABVariationOutput{}, AuditionABInput{}, errors.New("clip fields must be omitted for scene comparisons")
	}
	if input.Strength != nil || input.Seed != nil {
		return CompareABVariationOutput{}, AuditionABInput{}, errors.New("strength and seed must be omitted for scene comparisons")
	}
	created, err := createSceneEnergyVariation(client, CreateSceneEnergyVariationInput{
		SourceSceneIndex: *input.SourceSceneIndex,
		TrackIndices:     input.TrackIndices,
		Variation:        variation,
		VelocityDelta:    input.VelocityDelta,
		Fire:             false,
	})
	if err != nil {
		return CompareABVariationOutput{}, AuditionABInput{}, err
	}
	return CompareABVariationOutput{
			Kind:           "scene",
			Variation:      created.Variation,
			SourceIndex:    created.SourceSceneIndex,
			VariationIndex: created.TargetSceneIndex,
			NotesChanged:   created.NotesChanged,
			TracksChanged:  created.TracksChanged,
		}, AuditionABInput{
			TargetType:     "scene",
			SourceIndex:    created.SourceSceneIndex,
			VariationIndex: created.TargetSceneIndex,
		}, nil
}

func requireClipCompareSlots(input CompareABVariationInput) (trackIndex, sourceClip, targetClip int, err error) {
	if input.TrackIndex == nil {
		return 0, 0, 0, errors.New("track_index is required for clip comparisons")
	}
	if input.SourceClipIndex == nil {
		return 0, 0, 0, errors.New("source_clip_index is required for clip comparisons")
	}
	if input.TargetClipIndex == nil {
		return 0, 0, 0, errors.New("target_clip_index is required for clip comparisons")
	}
	return *input.TrackIndex, *input.SourceClipIndex, *input.TargetClipIndex, nil
}
