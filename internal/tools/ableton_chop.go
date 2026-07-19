package tools

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

// chopGrids maps a step-grid name to its length in beats (4/4).
var chopGrids = map[string]float64{
	"1/4":  1.0,
	"1/8":  0.5,
	"1/16": 0.25,
	"1/32": 0.125,
}

type ChopDraftInput struct {
	NumSlices int      `json:"num_slices" jsonschema:"description=Number of available slices/pads to arrange,minimum=1,maximum=128"`
	BaseNote  *int     `json:"base_note,omitempty" jsonschema:"description=MIDI note for slice 0 (default 36 = C1),minimum=0,maximum=127"`
	Bars      *int     `json:"bars,omitempty" jsonschema:"description=Pattern length in bars (default 1),minimum=1,maximum=16"`
	Grid      string   `json:"grid,omitempty" jsonschema:"description=Step grid: 1/4, 1/8, 1/16 (default), or 1/32"`
	Density   *float64 `json:"density,omitempty" jsonschema:"description=Fraction of steps that get a hit, 0..1 (default 0.5)"`
	AvoidCopy *bool    `json:"avoid_copy,omitempty" jsonschema:"description=Avoid reproducing the source's original slice order (default true)"`
	Seed      *int64   `json:"seed,omitempty" jsonschema:"description=Random seed for a reproducible draft"`
}

type ChopDraftOutput struct {
	Notes       []MidiNote `json:"notes"`
	Grid        string     `json:"grid"`
	Bars        int        `json:"bars"`
	StepBeats   float64    `json:"step_beats"`
	LengthBeats float64    `json:"length_beats"`
	Seed        int64      `json:"seed"`
	Note        string     `json:"note"`
	NextStep    string     `json:"next_step"`
}

func NewAbletonChopDraft(g *genkit.Genkit) ai.Tool {
	return genkit.DefineTool(g, "ableton_chop_draft",
		"Generate a MIDI draft that arranges chop slices into a rhythmic pattern WITHOUT reproducing the source's original order (avoid_copy). This is a placement suggestion for slices/pads (e.g. a Simpler in Slice mode or a Drum Rack), not a transcription of any melody. Returns notes to review, then apply with ableton_add_midi_notes.",
		func(_ *ai.ToolContext, input ChopDraftInput) (ChopDraftOutput, error) {
			return generateChopDraft(input)
		},
	)
}

func generateChopDraft(input ChopDraftInput) (ChopDraftOutput, error) {
	if input.NumSlices < 1 || input.NumSlices > 128 {
		return ChopDraftOutput{}, errors.New("num_slices must be 1..128")
	}
	baseNote := 36
	if input.BaseNote != nil {
		baseNote = *input.BaseNote
	}
	if baseNote < 0 || baseNote > 127 {
		return ChopDraftOutput{}, errors.New("base_note must be 0..127")
	}
	if baseNote+input.NumSlices-1 > 127 {
		return ChopDraftOutput{}, fmt.Errorf("base_note %d + %d slices exceeds MIDI note 127; lower base_note or reduce num_slices", baseNote, input.NumSlices)
	}

	bars := 1
	if input.Bars != nil {
		bars = *input.Bars
	}
	if bars < 1 || bars > 16 {
		return ChopDraftOutput{}, errors.New("bars must be 1..16")
	}

	grid := input.Grid
	if grid == "" {
		grid = "1/16"
	}
	stepBeats, ok := chopGrids[grid]
	if !ok {
		return ChopDraftOutput{}, fmt.Errorf("invalid grid %q; use 1/4, 1/8, 1/16, or 1/32", grid)
	}

	density := 0.5
	if input.Density != nil {
		density = *input.Density
	}
	if density < 0.05 {
		density = 0.05
	}
	if density > 1 {
		density = 1
	}

	avoidCopy := true
	if input.AvoidCopy != nil {
		avoidCopy = *input.AvoidCopy
	}

	seed := time.Now().UnixNano()
	if input.Seed != nil {
		seed = *input.Seed
	}
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // musical draft, not security-sensitive

	lengthBeats := float64(bars) * 4.0
	steps := int(math.Round(lengthBeats / stepBeats))
	stepsPerBeat := int(math.Round(1.0 / stepBeats))
	if stepsPerBeat < 1 {
		stepsPerBeat = 1
	}

	notes := make([]MidiNote, 0, steps)
	prevSlice := -1
	for step := 0; step < steps; step++ {
		if rng.Float64() > density {
			continue
		}
		slice := pickChopSlice(rng, input.NumSlices, avoidCopy, prevSlice)
		start := round4(float64(step) * stepBeats)
		notes = append(notes, MidiNote{
			Pitch:     baseNote + slice,
			StartTime: start,
			Duration:  stepBeats,
			Velocity:  chopVelocity(rng, step, stepsPerBeat),
		})
		prevSlice = slice
	}

	// Guarantee at least one hit so the draft is never empty.
	if len(notes) == 0 {
		slice := rng.Intn(input.NumSlices)
		notes = append(notes, MidiNote{
			Pitch:     baseNote + slice,
			StartTime: 0,
			Duration:  stepBeats,
			Velocity:  chopVelocity(rng, 0, stepsPerBeat),
		})
	}

	return ChopDraftOutput{
		Notes:       notes,
		Grid:        grid,
		Bars:        bars,
		StepBeats:   stepBeats,
		LengthBeats: lengthBeats,
		Seed:        seed,
		Note:        "Placement suggestion only: a rearrangement of slice triggers, not a transcription of the source. Review/edit before applying.",
		NextStep:    fmt.Sprintf("Create a clip of %.0f beats, then apply with ableton_add_midi_notes(notes). Reuse the same seed to reproduce this draft.", lengthBeats),
	}, nil
}

// pickChopSlice chooses a slice index. When avoidCopy is set it skips immediate
// repeats and ascending-consecutive picks so the result reads as a rearrangement
// rather than a replay of the source order.
func pickChopSlice(rng *rand.Rand, n int, avoidCopy bool, prev int) int {
	if n == 1 {
		return 0
	}
	if !avoidCopy {
		return rng.Intn(n)
	}
	for tries := 0; tries < 8; tries++ {
		s := rng.Intn(n)
		if s == prev {
			continue
		}
		if prev >= 0 && s == prev+1 {
			continue
		}
		return s
	}
	// Fallback: anything that is not an immediate repeat.
	s := rng.Intn(n)
	if s == prev {
		s = (s + 1) % n
	}
	return s
}

func chopVelocity(rng *rand.Rand, step, stepsPerBeat int) int {
	v := 92 + rng.Intn(16) // 92..107
	if stepsPerBeat > 0 && step%stepsPerBeat == 0 {
		v += 12 // accent on the beat
	}
	if v > 127 {
		v = 127
	}
	return v
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
