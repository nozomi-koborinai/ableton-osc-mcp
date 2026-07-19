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

const (
	defaultVariationStrength = 0.6
	defaultVariationHatPitch = 42
	defaultVariationSnare    = 38
)

type CreateDrumVariationInput struct {
	TrackIndex      int      `json:"track_index" jsonschema:"minimum=0"`
	SourceClipIndex int      `json:"source_clip_index" jsonschema:"minimum=0"`
	TargetClipIndex int      `json:"target_clip_index" jsonschema:"description=Must be an empty clip slot for the A/B variation,minimum=0"`
	Variation       string   `json:"variation" jsonschema:"description=One change only: groove, density, or fill"`
	Strength        *float64 `json:"strength,omitempty" jsonschema:"description=Variation intensity 0-1 (default 0.6),minimum=0,maximum=1"`
	HatPitch        *int     `json:"hat_pitch,omitempty" jsonschema:"description=Closed hat MIDI pitch for density variation (default 42),minimum=0,maximum=127"`
	SnarePitch      *int     `json:"snare_pitch,omitempty" jsonschema:"description=Snare MIDI pitch for fill variation (default 38),minimum=0,maximum=127"`
	Seed            *int64   `json:"seed,omitempty" jsonschema:"description=Optional RNG seed for reproducible groove or density variations"`
	Fire            bool     `json:"fire,omitempty" jsonschema:"description=Fire the target clip after creating it"`
}

type CreateDrumVariationOutput struct {
	TrackIndex      int     `json:"track_index"`
	SourceClipIndex int     `json:"source_clip_index"`
	TargetClipIndex int     `json:"target_clip_index"`
	Variation       string  `json:"variation"`
	Strength        float64 `json:"strength"`
	NotesChanged    int     `json:"notes_changed"`
	NotesAdded      int     `json:"notes_added"`
	Seed            int64   `json:"seed"`
	Fired           bool    `json:"fired"`
}

type variationOptions struct {
	Strength   float64
	HatPitch   int
	SnarePitch int
	Seed       int64
}

type variationClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonCreateDrumVariation(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_create_drum_variation",
		"Ableton Live: create only a drum A/B variation (groove, density, or fill) in an empty slot — prefer ableton_compare_ab_variation when you also want to audition",
		func(_ *ai.ToolContext, input CreateDrumVariationInput) (CreateDrumVariationOutput, error) {
			return createDrumVariation(client, input)
		},
	)
}

func createDrumVariation(client variationClient, input CreateDrumVariationInput) (CreateDrumVariationOutput, error) {
	if err := validateTrackClipIndices(input.TrackIndex, input.SourceClipIndex); err != nil {
		return CreateDrumVariationOutput{}, err
	}
	if input.TargetClipIndex < 0 {
		return CreateDrumVariationOutput{}, errors.New("target_clip_index must be >= 0")
	}
	if input.SourceClipIndex == input.TargetClipIndex {
		return CreateDrumVariationOutput{}, errors.New("source_clip_index and target_clip_index must differ")
	}

	variation := strings.ToLower(strings.TrimSpace(input.Variation))
	if !isDrumVariation(variation) {
		return CreateDrumVariationOutput{}, errors.New("variation must be groove, density, or fill")
	}
	opts, err := resolveVariationOptions(input)
	if err != nil {
		return CreateDrumVariationOutput{}, err
	}

	targetHasClip, err := queryClipHasClip(client, input.TrackIndex, input.TargetClipIndex)
	if err != nil {
		return CreateDrumVariationOutput{}, fmt.Errorf("check target slot: %w", err)
	}
	if targetHasClip {
		return CreateDrumVariationOutput{}, errors.New("target_clip_index must be an empty slot to preserve A/B comparison")
	}

	sourceRes, err := client.Query("/live/clip/get/notes", int32(input.TrackIndex), int32(input.SourceClipIndex))
	if err != nil {
		return CreateDrumVariationOutput{}, fmt.Errorf("get source notes: %w", err)
	}
	_, _, sourceNotes, err := parseClipNotesResponse(sourceRes)
	if err != nil {
		return CreateDrumVariationOutput{}, fmt.Errorf("get source notes: %w", err)
	}
	if len(sourceNotes) == 0 {
		return CreateDrumVariationOutput{}, errors.New("source clip has no notes to vary")
	}

	clipLength := queryClipLength(client, input.TrackIndex, input.SourceClipIndex)
	if clipLength <= 0 {
		clipLength = inferredClipLength(sourceNotes)
	}

	rng := rand.New(rand.NewSource(opts.Seed))
	var notes []MidiNote
	var notesChanged, notesAdded int
	switch variation {
	case "groove":
		humanize := humanizeOptions{
			TimingAmount:   0.025,
			VelocityAmount: 12,
			Swing:          0.15,
			Strength:       opts.Strength,
			Seed:           opts.Seed,
		}
		notes = humanizeNotes(sourceNotes, humanize, rng, clipLength)
		notesChanged = countChangedMidiNotes(sourceNotes, notes)
	case "density":
		additions := densityNotes(sourceNotes, clipLength, opts.HatPitch, opts.Strength, rng)
		notes = append(copyMidiNotes(sourceNotes), additions...)
		notesAdded = len(additions)
	case "fill":
		additions := fillNotes(sourceNotes, clipLength, opts.SnarePitch, opts.Strength)
		notes = append(copyMidiNotes(sourceNotes), additions...)
		notesAdded = len(additions)
	}
	if notesChanged == 0 && notesAdded == 0 {
		return CreateDrumVariationOutput{}, errors.New("variation would not change the source clip")
	}

	if err := client.Send(
		"/live/clip_slot/duplicate_clip_to",
		int32(input.TrackIndex),
		int32(input.SourceClipIndex),
		int32(input.TrackIndex),
		int32(input.TargetClipIndex),
	); err != nil {
		return CreateDrumVariationOutput{}, fmt.Errorf("duplicate source clip: %w", err)
	}

	if variation == "groove" {
		if err := replaceVariationNotes(client, input.TrackIndex, input.TargetClipIndex, sourceNotes, notes); err != nil {
			return CreateDrumVariationOutput{}, err
		}
	} else {
		if err := client.Send("/live/clip/add/notes", addNotesArgs(input.TrackIndex, input.TargetClipIndex, notes[len(sourceNotes):])...); err != nil {
			return CreateDrumVariationOutput{}, fmt.Errorf("add variation notes: %w", err)
		}
	}

	if err := client.Send(
		"/live/clip/set/name",
		int32(input.TrackIndex),
		int32(input.TargetClipIndex),
		"Variation: "+variation,
	); err != nil {
		return CreateDrumVariationOutput{}, fmt.Errorf("set variation clip name: %w", err)
	}

	fired := false
	if input.Fire {
		if err := client.Send("/live/clip_slot/fire", int32(input.TrackIndex), int32(input.TargetClipIndex)); err != nil {
			return CreateDrumVariationOutput{}, fmt.Errorf("fire variation clip: %w", err)
		}
		fired = true
	}

	return CreateDrumVariationOutput{
		TrackIndex:      input.TrackIndex,
		SourceClipIndex: input.SourceClipIndex,
		TargetClipIndex: input.TargetClipIndex,
		Variation:       variation,
		Strength:        opts.Strength,
		NotesChanged:    notesChanged,
		NotesAdded:      notesAdded,
		Seed:            opts.Seed,
		Fired:           fired,
	}, nil
}

func isDrumVariation(variation string) bool {
	switch variation {
	case "groove", "density", "fill":
		return true
	default:
		return false
	}
}

func resolveVariationOptions(input CreateDrumVariationInput) (variationOptions, error) {
	opts := variationOptions{
		Strength:   defaultVariationStrength,
		HatPitch:   defaultVariationHatPitch,
		SnarePitch: defaultVariationSnare,
		Seed:       time.Now().UnixNano(),
	}
	if input.Strength != nil {
		opts.Strength = *input.Strength
	}
	if input.HatPitch != nil {
		opts.HatPitch = *input.HatPitch
	}
	if input.SnarePitch != nil {
		opts.SnarePitch = *input.SnarePitch
	}
	if input.Seed != nil {
		opts.Seed = *input.Seed
	}
	if opts.Strength < 0 || opts.Strength > 1 {
		return variationOptions{}, errors.New("strength must be between 0 and 1")
	}
	if opts.HatPitch < 0 || opts.HatPitch > 127 {
		return variationOptions{}, errors.New("hat_pitch must be 0..127")
	}
	if opts.SnarePitch < 0 || opts.SnarePitch > 127 {
		return variationOptions{}, errors.New("snare_pitch must be 0..127")
	}
	return opts, nil
}

func queryClipHasClip(client variationClient, trackIndex, clipIndex int) (bool, error) {
	res, err := client.Query("/live/clip_slot/get/has_clip", int32(trackIndex), int32(clipIndex))
	if err != nil {
		return false, err
	}
	if err := ensureResponseLen(res, 3); err != nil {
		return false, err
	}
	return abletonosc.AsBool(res[2])
}

func replaceVariationNotes(client variationClient, trackIndex, clipIndex int, original, replacement []MidiNote) error {
	if err := client.Send("/live/clip/remove/notes", int32(trackIndex), int32(clipIndex)); err != nil {
		return fmt.Errorf("clear duplicated clip: %w", err)
	}
	if err := client.Send("/live/clip/add/notes", addNotesArgs(trackIndex, clipIndex, replacement)...); err != nil {
		if restoreErr := client.Send("/live/clip/add/notes", addNotesArgs(trackIndex, clipIndex, original)...); restoreErr != nil {
			return fmt.Errorf("add groove variation: %w; restore duplicate also failed: %v", err, restoreErr)
		}
		return fmt.Errorf("add groove variation: %w (duplicate restored)", err)
	}
	return nil
}

func densityNotes(notes []MidiNote, clipLength float64, hatPitch int, strength float64, rng *rand.Rand) []MidiNote {
	additions := make([]MidiNote, 0)
	for start := 0.25; start < clipLength; start += 0.5 {
		if hasNoteAt(notes, hatPitch, start) || rng.Float64() > strength {
			continue
		}
		additions = append(additions, MidiNote{
			Pitch:     hatPitch,
			StartTime: start,
			Duration:  0.1,
			Velocity:  55 + int(rng.Float64()*25),
		})
	}
	return additions
}

func fillNotes(notes []MidiNote, clipLength float64, snarePitch int, strength float64) []MidiNote {
	if clipLength < 1 || strength == 0 {
		return []MidiNote{}
	}
	candidates := []float64{clipLength - 0.75, clipLength - 0.5, clipLength - 0.25}
	count := int(math.Ceil(float64(len(candidates)) * strength))
	additions := make([]MidiNote, 0, count)
	for i, start := range candidates {
		if i >= count || start < 0 || hasNoteAt(notes, snarePitch, start) {
			continue
		}
		additions = append(additions, MidiNote{
			Pitch:     snarePitch,
			StartTime: start,
			Duration:  0.12,
			Velocity:  70 + i*12,
		})
	}
	return additions
}

func hasNoteAt(notes []MidiNote, pitch int, start float64) bool {
	for _, note := range notes {
		if note.Pitch == pitch && math.Abs(note.StartTime-start) < 0.001 {
			return true
		}
	}
	return false
}

func inferredClipLength(notes []MidiNote) float64 {
	end := 0.0
	for _, note := range notes {
		end = math.Max(end, note.StartTime+note.Duration)
	}
	if end <= 0 {
		return 1
	}
	return math.Ceil(end)
}

func copyMidiNotes(notes []MidiNote) []MidiNote {
	out := make([]MidiNote, 0, len(notes))
	for _, note := range notes {
		copied := note
		if note.Mute != nil {
			m := *note.Mute
			copied.Mute = &m
		}
		out = append(out, copied)
	}
	return out
}

func countChangedMidiNotes(original, varied []MidiNote) int {
	if len(original) != len(varied) {
		n := len(varied)
		if len(original) > n {
			n = len(original)
		}
		return n
	}
	changed := 0
	for i := range original {
		if !midiNotesEqual(original[i], varied[i]) {
			changed++
		}
	}
	return changed
}

func midiNotesEqual(a, b MidiNote) bool {
	if a.Pitch != b.Pitch || a.Velocity != b.Velocity {
		return false
	}
	if math.Abs(a.StartTime-b.StartTime) > 1e-6 || math.Abs(a.Duration-b.Duration) > 1e-6 {
		return false
	}
	return midiMuteValue(a.Mute) == midiMuteValue(b.Mute)
}

func midiMuteValue(mute *bool) bool {
	if mute == nil {
		return false
	}
	return *mute
}
