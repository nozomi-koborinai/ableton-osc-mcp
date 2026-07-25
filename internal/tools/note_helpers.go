package tools

import (
	"math"
	"math/rand"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

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
