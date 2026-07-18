package tools

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const (
	defaultHumanizeTimingAmount   = 0.02
	defaultHumanizeVelocityAmount = 10
	defaultHumanizeStrength       = 0.6
	maxHumanizeTimingAmount       = 0.08
	maxHumanizeVelocityAmount     = 40
)

type HumanizeClipInput struct {
	TrackIndex     int      `json:"track_index" jsonschema:"minimum=0"`
	ClipIndex      int      `json:"clip_index" jsonschema:"minimum=0"`
	TimingAmount   *float64 `json:"timing_amount,omitempty" jsonschema:"description=Max timing offset in beats (default 0.02),minimum=0,maximum=0.08"`
	VelocityAmount *int     `json:"velocity_amount,omitempty" jsonschema:"description=Max velocity offset (default 10),minimum=0,maximum=40"`
	Swing          *float64 `json:"swing,omitempty" jsonschema:"description=8th-note swing amount 0-1 (default 0),minimum=0,maximum=1"`
	Strength       *float64 `json:"strength,omitempty" jsonschema:"description=Overall humanize intensity 0-1 (default 0.6),minimum=0,maximum=1"`
	Seed           *int64   `json:"seed,omitempty" jsonschema:"description=Optional RNG seed for reproducible results"`
}

type HumanizeClipOutput struct {
	TrackIndex     int     `json:"track_index"`
	ClipIndex      int     `json:"clip_index"`
	NotesUpdated   int     `json:"notes_updated"`
	TimingAmount   float64 `json:"timing_amount"`
	VelocityAmount int     `json:"velocity_amount"`
	Swing          float64 `json:"swing"`
	Strength       float64 `json:"strength"`
	Seed           int64   `json:"seed"`
}

type humanizeOptions struct {
	TimingAmount   float64
	VelocityAmount int
	Swing          float64
	Strength       float64
	Seed           int64
}

type humanizeClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonHumanizeClip(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_humanize_clip",
		"Ableton Live: add microtiming, velocity variation, and optional swing to MIDI notes in a clip",
		func(_ *ai.ToolContext, input HumanizeClipInput) (HumanizeClipOutput, error) {
			return humanizeClip(client, input)
		},
	)
}

func humanizeClip(client humanizeClient, input HumanizeClipInput) (HumanizeClipOutput, error) {
	if err := validateTrackClipIndices(input.TrackIndex, input.ClipIndex); err != nil {
		return HumanizeClipOutput{}, err
	}

	opts, err := resolveHumanizeOptions(input)
	if err != nil {
		return HumanizeClipOutput{}, err
	}

	res, err := client.Query("/live/clip/get/notes", int32(input.TrackIndex), int32(input.ClipIndex))
	if err != nil {
		return HumanizeClipOutput{}, fmt.Errorf("get notes: %w", err)
	}
	_, _, notes, err := parseClipNotesResponse(res)
	if err != nil {
		return HumanizeClipOutput{}, fmt.Errorf("get notes: %w", err)
	}
	if len(notes) == 0 {
		return HumanizeClipOutput{}, errors.New("clip has no notes to humanize")
	}

	// Best-effort clip length so timing offsets never push notes past the loop.
	clipLength := queryClipLength(client, input.TrackIndex, input.ClipIndex)

	rng := rand.New(rand.NewSource(opts.Seed))
	humanized := humanizeNotes(notes, opts, rng, clipLength)

	if err := client.Send("/live/clip/remove/notes", int32(input.TrackIndex), int32(input.ClipIndex)); err != nil {
		return HumanizeClipOutput{}, fmt.Errorf("clear notes: %w", err)
	}

	if err := client.Send("/live/clip/add/notes", addNotesArgs(input.TrackIndex, input.ClipIndex, humanized)...); err != nil {
		// Adding failed after clearing; restore the original notes so the clip is not left empty.
		if restoreErr := client.Send("/live/clip/add/notes", addNotesArgs(input.TrackIndex, input.ClipIndex, notes)...); restoreErr != nil {
			return HumanizeClipOutput{}, fmt.Errorf("add humanized notes: %w; restore original notes also failed: %v", err, restoreErr)
		}
		return HumanizeClipOutput{}, fmt.Errorf("add humanized notes: %w (original notes restored)", err)
	}

	return HumanizeClipOutput{
		TrackIndex:     input.TrackIndex,
		ClipIndex:      input.ClipIndex,
		NotesUpdated:   len(humanized),
		TimingAmount:   opts.TimingAmount,
		VelocityAmount: opts.VelocityAmount,
		Swing:          opts.Swing,
		Strength:       opts.Strength,
		Seed:           opts.Seed,
	}, nil
}

func resolveHumanizeOptions(input HumanizeClipInput) (humanizeOptions, error) {
	opts := humanizeOptions{
		TimingAmount:   defaultHumanizeTimingAmount,
		VelocityAmount: defaultHumanizeVelocityAmount,
		Strength:       defaultHumanizeStrength,
		Seed:           time.Now().UnixNano(),
	}
	if input.TimingAmount != nil {
		opts.TimingAmount = *input.TimingAmount
	}
	if input.VelocityAmount != nil {
		opts.VelocityAmount = *input.VelocityAmount
	}
	if input.Swing != nil {
		opts.Swing = *input.Swing
	}
	if input.Strength != nil {
		opts.Strength = *input.Strength
	}
	if input.Seed != nil {
		opts.Seed = *input.Seed
	}

	if opts.TimingAmount < 0 || opts.TimingAmount > maxHumanizeTimingAmount {
		return humanizeOptions{}, fmt.Errorf("timing_amount must be between 0 and %.2f", maxHumanizeTimingAmount)
	}
	if opts.VelocityAmount < 0 || opts.VelocityAmount > maxHumanizeVelocityAmount {
		return humanizeOptions{}, fmt.Errorf("velocity_amount must be between 0 and %d", maxHumanizeVelocityAmount)
	}
	if opts.Swing < 0 || opts.Swing > 1 {
		return humanizeOptions{}, errors.New("swing must be between 0 and 1")
	}
	if opts.Strength < 0 || opts.Strength > 1 {
		return humanizeOptions{}, errors.New("strength must be between 0 and 1")
	}
	return opts, nil
}

func humanizeNotes(notes []MidiNote, opts humanizeOptions, rng *rand.Rand, clipLength float64) []MidiNote {
	timingMax := opts.TimingAmount * opts.Strength
	velocityMax := float64(opts.VelocityAmount) * opts.Strength
	swing := opts.Swing * opts.Strength

	out := make([]MidiNote, 0, len(notes))
	for _, n := range notes {
		start := n.StartTime
		if swing > 0 {
			start = applyEighthSwing(start, swing)
		}
		if timingMax > 0 {
			start += (rng.Float64()*2 - 1) * timingMax
		}
		if start < 0 {
			start = 0
		}
		// Keep notes inside the clip loop; never move a note earlier than its origin bar.
		if clipLength > 0 && start >= clipLength {
			start = math.Max(n.StartTime, clipLength-timingMax)
			if start < 0 {
				start = 0
			}
		}

		velocity := n.Velocity
		if velocityMax > 0 {
			delta := int(math.Round((rng.Float64()*2 - 1) * velocityMax))
			velocity += delta
		}
		if velocity < 1 {
			velocity = 1
		}
		if velocity > 127 {
			velocity = 127
		}

		duration := n.Duration
		if duration < 0.01 {
			duration = 0.01
		}

		note := MidiNote{
			Pitch:     n.Pitch,
			StartTime: start,
			Duration:  duration,
			Velocity:  velocity,
		}
		if n.Mute != nil {
			m := *n.Mute
			note.Mute = &m
		}
		out = append(out, note)
	}
	return out
}

func addNotesArgs(trackIndex, clipIndex int, notes []MidiNote) []interface{} {
	args := []interface{}{int32(trackIndex), int32(clipIndex)}
	for _, n := range notes {
		mute := false
		if n.Mute != nil {
			mute = *n.Mute
		}
		args = append(args,
			int32(n.Pitch),
			float32(n.StartTime),
			float32(n.Duration),
			int32(n.Velocity),
			mute,
		)
	}
	return args
}

// queryClipLength returns the clip loop length in beats, or 0 when unavailable.
func queryClipLength(client humanizeClient, trackIndex, clipIndex int) float64 {
	res, err := client.Query("/live/clip/get/length", int32(trackIndex), int32(clipIndex))
	if err != nil || len(res) == 0 {
		return 0
	}
	// Reply is (track_index, clip_index, length); fall back to the last value otherwise.
	length, err := abletonosc.AsFloat64(res[len(res)-1])
	if err != nil || length <= 0 {
		return 0
	}
	return length
}

// applyEighthSwing delays offbeat eighth notes toward the next onbeat.
// swing=0 keeps even 8ths; swing=1 delays by one third of an 8th (triplet-ish feel).
func applyEighthSwing(start, swing float64) float64 {
	const unit = 0.5
	slot := math.Floor((start + 1e-9) / unit)
	if int(slot)%2 == 0 {
		return start
	}
	return start + swing*unit/3
}
