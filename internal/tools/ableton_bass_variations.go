package tools

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const defaultBassVariationStrength = 0.6

type CreateBassVariationInput struct {
	TrackIndex      int      `json:"track_index" jsonschema:"minimum=0"`
	SourceClipIndex int      `json:"source_clip_index" jsonschema:"minimum=0"`
	TargetClipIndex int      `json:"target_clip_index" jsonschema:"description=Must be an empty clip slot for the A/B variation,minimum=0"`
	Variation       string   `json:"variation" jsonschema:"description=One change only: octave_up, octave_down, staccato, or groove"`
	Strength        *float64 `json:"strength,omitempty" jsonschema:"description=Variation intensity 0-1 (default 0.6),minimum=0,maximum=1"`
	Seed            *int64   `json:"seed,omitempty" jsonschema:"description=Optional RNG seed for reproducible groove variations"`
	Fire            bool     `json:"fire,omitempty" jsonschema:"description=Fire the target clip after creating it"`
}

type CreateBassVariationOutput struct {
	TrackIndex      int     `json:"track_index"`
	SourceClipIndex int     `json:"source_clip_index"`
	TargetClipIndex int     `json:"target_clip_index"`
	Variation       string  `json:"variation"`
	Strength        float64 `json:"strength"`
	NotesChanged    int     `json:"notes_changed"`
	NotesSkipped    int     `json:"notes_skipped"`
	Seed            int64   `json:"seed"`
	Fired           bool    `json:"fired"`
}

type bassVariationOptions struct {
	Strength float64
	Seed     int64
}

func NewAbletonCreateBassVariation(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_create_bass_variation",
		"Ableton Live: create only a bass A/B variation (octave, staccato, or groove) in an empty slot — prefer ableton_compare_ab_variation when you also want to audition",
		func(_ *ai.ToolContext, input CreateBassVariationInput) (CreateBassVariationOutput, error) {
			return createBassVariation(client, input)
		},
	)
}

func createBassVariation(client variationClient, input CreateBassVariationInput) (CreateBassVariationOutput, error) {
	if err := validateTrackClipIndices(input.TrackIndex, input.SourceClipIndex); err != nil {
		return CreateBassVariationOutput{}, err
	}
	if input.TargetClipIndex < 0 {
		return CreateBassVariationOutput{}, errors.New("target_clip_index must be >= 0")
	}
	if input.SourceClipIndex == input.TargetClipIndex {
		return CreateBassVariationOutput{}, errors.New("source_clip_index and target_clip_index must differ")
	}

	variation := strings.ToLower(strings.TrimSpace(input.Variation))
	if !isBassVariation(variation) {
		return CreateBassVariationOutput{}, errors.New("variation must be octave_up, octave_down, staccato, or groove")
	}
	opts, err := resolveBassVariationOptions(input)
	if err != nil {
		return CreateBassVariationOutput{}, err
	}

	targetHasClip, err := queryClipHasClip(client, input.TrackIndex, input.TargetClipIndex)
	if err != nil {
		return CreateBassVariationOutput{}, fmt.Errorf("check target slot: %w", err)
	}
	if targetHasClip {
		return CreateBassVariationOutput{}, errors.New("target_clip_index must be an empty slot to preserve A/B comparison")
	}

	sourceRes, err := client.Query("/live/clip/get/notes", int32(input.TrackIndex), int32(input.SourceClipIndex))
	if err != nil {
		return CreateBassVariationOutput{}, fmt.Errorf("get source notes: %w", err)
	}
	_, _, sourceNotes, err := parseClipNotesResponse(sourceRes)
	if err != nil {
		return CreateBassVariationOutput{}, fmt.Errorf("get source notes: %w", err)
	}
	if len(sourceNotes) == 0 {
		return CreateBassVariationOutput{}, errors.New("source clip has no notes to vary")
	}

	clipLength := queryClipLength(client, input.TrackIndex, input.SourceClipIndex)
	if clipLength <= 0 {
		clipLength = inferredClipLength(sourceNotes)
	}
	varied, changed, skipped := varyBassNotes(
		sourceNotes,
		clipLength,
		variation,
		opts,
		rand.New(rand.NewSource(opts.Seed)),
	)
	if changed == 0 {
		return CreateBassVariationOutput{}, errors.New("variation would not change the source clip")
	}

	if err := client.Send(
		"/live/clip_slot/duplicate_clip_to",
		int32(input.TrackIndex),
		int32(input.SourceClipIndex),
		int32(input.TargetClipIndex),
	); err != nil {
		return CreateBassVariationOutput{}, fmt.Errorf("duplicate source clip: %w", err)
	}
	if err := replaceVariationNotes(client, input.TrackIndex, input.TargetClipIndex, sourceNotes, varied); err != nil {
		return CreateBassVariationOutput{}, err
	}
	if err := client.Send(
		"/live/clip/set/name",
		int32(input.TrackIndex),
		int32(input.TargetClipIndex),
		"Variation: bass "+variation,
	); err != nil {
		return CreateBassVariationOutput{}, fmt.Errorf("set variation clip name: %w", err)
	}

	fired := false
	if input.Fire {
		if err := client.Send("/live/clip_slot/fire", int32(input.TrackIndex), int32(input.TargetClipIndex)); err != nil {
			return CreateBassVariationOutput{}, fmt.Errorf("fire variation clip: %w", err)
		}
		fired = true
	}

	return CreateBassVariationOutput{
		TrackIndex:      input.TrackIndex,
		SourceClipIndex: input.SourceClipIndex,
		TargetClipIndex: input.TargetClipIndex,
		Variation:       variation,
		Strength:        opts.Strength,
		NotesChanged:    changed,
		NotesSkipped:    skipped,
		Seed:            opts.Seed,
		Fired:           fired,
	}, nil
}

func isBassVariation(variation string) bool {
	switch variation {
	case "octave_up", "octave_down", "staccato", "groove":
		return true
	default:
		return false
	}
}

func resolveBassVariationOptions(input CreateBassVariationInput) (bassVariationOptions, error) {
	opts := bassVariationOptions{
		Strength: defaultBassVariationStrength,
		Seed:     time.Now().UnixNano(),
	}
	if input.Strength != nil {
		opts.Strength = *input.Strength
	}
	if input.Seed != nil {
		opts.Seed = *input.Seed
	}
	if opts.Strength < 0 || opts.Strength > 1 {
		return bassVariationOptions{}, errors.New("strength must be between 0 and 1")
	}
	return opts, nil
}

func varyBassNotes(
	notes []MidiNote,
	clipLength float64,
	variation string,
	opts bassVariationOptions,
	rng *rand.Rand,
) ([]MidiNote, int, int) {
	switch variation {
	case "octave_up":
		return shiftBassOctave(notes, 12)
	case "octave_down":
		return shiftBassOctave(notes, -12)
	case "staccato":
		return shortenBassNotes(notes, opts.Strength)
	case "groove":
		humanize := humanizeOptions{
			TimingAmount:   0.018,
			VelocityAmount: 7,
			Swing:          0.08,
			Strength:       opts.Strength,
			Seed:           opts.Seed,
		}
		return humanizeNotes(notes, humanize, rng, clipLength), len(notes), 0
	default:
		return copyMidiNotes(notes), 0, 0
	}
}

func shiftBassOctave(notes []MidiNote, semitones int) ([]MidiNote, int, int) {
	out := copyMidiNotes(notes)
	changed, skipped := 0, 0
	for i := range out {
		pitch := out[i].Pitch + semitones
		if pitch < 0 || pitch > 127 {
			skipped++
			continue
		}
		out[i].Pitch = pitch
		changed++
	}
	return out, changed, skipped
}

func shortenBassNotes(notes []MidiNote, strength float64) ([]MidiNote, int, int) {
	out := copyMidiNotes(notes)
	changed := 0
	for i := range out {
		original := out[i].Duration
		// At full strength, leave 35% of the original note length, not less than 50 ms.
		shortened := math.Max(0.05, original*(1-0.65*strength))
		if shortened < original {
			out[i].Duration = shortened
			changed++
		}
	}
	return out, changed, 0
}
