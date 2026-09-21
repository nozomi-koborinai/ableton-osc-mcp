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
	Kind             string   `json:"kind" jsonschema:"description=What to compare: drum\\, bass\\, or scene"`
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
	StopAfter        bool     `json:"stop_after,omitempty" jsonschema:"description=Stop playback after the last B"`
}

type CompareABVariationOutput struct {
	Kind             string         `json:"kind"`
	Variation        string         `json:"variation"`
	TrackIndex       *int           `json:"track_index,omitempty"`
	SourceIndex      int            `json:"source_index"`
	VariationIndex   int            `json:"variation_index"`
	NotesChanged     int            `json:"notes_changed,omitempty"`
	NotesAdded       int            `json:"notes_added,omitempty"`
	NotesSkipped     int            `json:"notes_skipped,omitempty"`
	TracksChanged    []int          `json:"tracks_changed,omitempty"`
	Seed             int64          `json:"seed,omitempty"`
	Audition         AuditionOutput `json:"audition"`
	PreferencePrompt string         `json:"preference_prompt"`
}

type compareABClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

const (
	defaultCompareBars   = 2
	maxCompareBars       = 8
	defaultCompareCycles = 1
	maxCompareCycles     = 4
)

func NewAbletonCompareABVariation(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_compare_ab_variation",
		"Ableton Live: preferred entry for drum/bass/scene A/B — create one variation into an empty target, play A then B on bar lines, and return a preference prompt. Blocks in real time (cycles × 2 × bars_per_version bars), shows A or B in the name of an 'Audition' track it adds at the end of the set, and afterwards every track plays what it played before. Stop here and wait: the choice is the listener's to make, and nothing is recorded until they state one.",
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
	// The audition's own limits come first: a bad value must not leave a variation behind.
	bars, cycles := defaultCompareBars, defaultCompareCycles
	if input.BarsPerVersion != nil {
		bars = *input.BarsPerVersion
	}
	if input.Cycles != nil {
		cycles = *input.Cycles
	}
	if bars < 1 || bars > maxCompareBars {
		return CompareABVariationOutput{}, fmt.Errorf("bars_per_version must be between 1 and %d", maxCompareBars)
	}
	if cycles < 1 || cycles > maxCompareCycles {
		return CompareABVariationOutput{}, fmt.Errorf("cycles must be between 1 and %d", maxCompareCycles)
	}

	var (
		out        CompareABVariationOutput
		a, b       []AuditionClip
		targetType = "clip"
		err        error
	)
	switch kind {
	case "drum":
		out, a, b, err = createDrumCompare(client, input, variation)
	case "bass":
		out, a, b, err = createBassCompare(client, input, variation)
	case "scene":
		targetType = "scene"
		out, a, b, err = createSceneCompare(client, input, variation)
	default:
		return CompareABVariationOutput{}, errors.New("kind must be drum, bass, or scene")
	}
	if err != nil {
		return CompareABVariationOutput{}, err
	}

	play := make([]string, 0, 2*cycles)
	for i := 0; i < cycles; i++ {
		play = append(play, "A", "B")
	}
	audition, err := runAudition(client, sleep, AuditionInput{
		Variants: []AuditionVariant{
			{Label: "A", Description: fmt.Sprintf("source (%s %d)", targetType, out.SourceIndex), Clips: a},
			{Label: "B", Description: fmt.Sprintf("%s variation (%s %d)", variation, targetType, out.VariationIndex), Clips: b},
		},
		BarsPerVariant: bars,
		Play:           play,
		StopAfter:      input.StopAfter,
	})
	if err != nil {
		return CompareABVariationOutput{}, fmt.Errorf("audition after %s variation: %w", kind, err)
	}
	audition.Prompt = "" // the question to ask is preference_prompt
	out.Audition = audition
	out.PreferencePrompt = auditionPreferencePrompt(targetType, kind, variation)
	return out, nil
}

func createDrumCompare(client compareABClient, input CompareABVariationInput, variation string) (CompareABVariationOutput, []AuditionClip, []AuditionClip, error) {
	trackIndex, sourceClip, targetClip, err := requireClipCompareSlots(input)
	if err != nil {
		return CompareABVariationOutput{}, nil, nil, err
	}
	if input.SourceSceneIndex != nil || len(input.TrackIndices) > 0 || input.VelocityDelta != nil {
		return CompareABVariationOutput{}, nil, nil, errors.New("scene fields must be omitted for drum comparisons")
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
		return CompareABVariationOutput{}, nil, nil, err
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
		},
		[]AuditionClip{{TrackIndex: track, ClipIndex: created.SourceClipIndex}},
		[]AuditionClip{{TrackIndex: track, ClipIndex: created.TargetClipIndex}},
		nil
}

func createBassCompare(client compareABClient, input CompareABVariationInput, variation string) (CompareABVariationOutput, []AuditionClip, []AuditionClip, error) {
	trackIndex, sourceClip, targetClip, err := requireClipCompareSlots(input)
	if err != nil {
		return CompareABVariationOutput{}, nil, nil, err
	}
	if input.SourceSceneIndex != nil || len(input.TrackIndices) > 0 || input.VelocityDelta != nil {
		return CompareABVariationOutput{}, nil, nil, errors.New("scene fields must be omitted for bass comparisons")
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
		return CompareABVariationOutput{}, nil, nil, err
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
		},
		[]AuditionClip{{TrackIndex: track, ClipIndex: created.SourceClipIndex}},
		[]AuditionClip{{TrackIndex: track, ClipIndex: created.TargetClipIndex}},
		nil
}

func createSceneCompare(client compareABClient, input CompareABVariationInput, variation string) (CompareABVariationOutput, []AuditionClip, []AuditionClip, error) {
	if input.SourceSceneIndex == nil {
		return CompareABVariationOutput{}, nil, nil, errors.New("source_scene_index is required for scene comparisons")
	}
	if input.TrackIndex != nil || input.SourceClipIndex != nil || input.TargetClipIndex != nil {
		return CompareABVariationOutput{}, nil, nil, errors.New("clip fields must be omitted for scene comparisons")
	}
	if input.Strength != nil || input.Seed != nil {
		return CompareABVariationOutput{}, nil, nil, errors.New("strength and seed must be omitted for scene comparisons")
	}
	created, err := createSceneEnergyVariation(client, CreateSceneEnergyVariationInput{
		SourceSceneIndex: *input.SourceSceneIndex,
		TrackIndices:     input.TrackIndices,
		Variation:        variation,
		VelocityDelta:    input.VelocityDelta,
		Fire:             false,
	})
	if err != nil {
		return CompareABVariationOutput{}, nil, nil, err
	}
	// A scene is heard as the clips of its row. Launching them one by one, and
	// not the scene, leaves alone the tracks the row has nothing for, and lets
	// the audition put every track back afterwards.
	tracks, err := tracksWithClipInScene(client, created.TargetSceneIndex)
	if err != nil {
		return CompareABVariationOutput{}, nil, nil, fmt.Errorf("list the clips of scene %d: %w", created.TargetSceneIndex, err)
	}
	var a, b []AuditionClip
	for _, track := range tracks {
		a = append(a, AuditionClip{TrackIndex: track, ClipIndex: created.SourceSceneIndex})
		b = append(b, AuditionClip{TrackIndex: track, ClipIndex: created.TargetSceneIndex})
	}
	return CompareABVariationOutput{
		Kind:           "scene",
		Variation:      created.Variation,
		SourceIndex:    created.SourceSceneIndex,
		VariationIndex: created.TargetSceneIndex,
		NotesChanged:   created.NotesChanged,
		TracksChanged:  created.TracksChanged,
	}, a, b, nil
}

// tracksWithClipInScene lists the tracks that have a clip in one scene row,
// from a single reply that covers every slot of the set.
func tracksWithClipInScene(client oscQuerier, scene int) ([]int, error) {
	numTracks, err := queryNumTracks(client)
	if err != nil {
		return nil, err
	}
	numScenes, err := queryNumScenes(client)
	if err != nil {
		return nil, err
	}
	res, err := client.Query("/live/song/get/track_data", int32(0), int32(numTracks), "clip_slot.has_clip")
	if err != nil {
		return nil, err
	}
	if scene >= numScenes || len(res) != numTracks*numScenes {
		return nil, fmt.Errorf("unexpected clip slot listing: %d values for %d tracks of %d scenes", len(res), numTracks, numScenes)
	}
	var tracks []int
	for track := 0; track < numTracks; track++ {
		has, err := asBoolish(res[track*numScenes+scene])
		if err != nil {
			return nil, err
		}
		if has {
			tracks = append(tracks, track)
		}
	}
	return tracks, nil
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

func auditionPreferencePrompt(targetType, instrument, variation string) string {
	if instrument != "" && variation != "" {
		return fmt.Sprintf(
			"Which was closer to your ideal: source or variation? Record with ableton_record_variation_preference using instrument=%s variation=%s.",
			instrument, variation,
		)
	}
	if instrument != "" {
		candidates := strings.Join(tasteVariationsFor(instrument), ", ")
		return fmt.Sprintf(
			"Which was closer to your ideal: source or variation? Record with ableton_record_variation_preference using instrument=%s and the variation you compared (%s).",
			instrument, candidates,
		)
	}
	if targetType == "scene" {
		return "Which was closer to your ideal: source or variation? Record with ableton_record_variation_preference using instrument=scene and variation=lift or pullback."
	}
	return "Which was closer to your ideal: source or variation? Record with ableton_record_variation_preference using instrument=drum or bass and the variation you compared (e.g. groove, density, octave_up)."
}
