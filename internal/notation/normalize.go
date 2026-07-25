package notation

import (
	"fmt"
	"sort"
)

// Adjustment records one note the notation had to shorten to match what Live
// will actually store.
type Adjustment struct {
	Position    string  `json:"position"`
	Pitch       int     `json:"pitch"`
	PitchName   string  `json:"pitch_name"`
	WasDuration float64 `json:"was_duration"`
	NowDuration float64 `json:"now_duration"`
}

// Normalize applies Live's rule that two notes of the same pitch cannot overlap:
// the earlier one is cut back to end exactly where the next one begins. Without
// this the notation asks for something Live will not hold, the write comes back
// changed, and the round-trip check reports a mismatch for a clip that is in fact
// correct — which is common, because any swung or humanized hi-hat pushes notes
// into the one behind it.
//
// Different pitches sounding together are left alone. That is a chord.
//
// Overruns smaller than BeatTolerance are left alone too: Live's truncation would
// fall inside the tolerance and never surface as a mismatch, so adjusting them
// would report noise.
//
// Two notes of one pitch starting at one position cannot be resolved by
// shortening — it would leave a note of no length — and picking a survivor would
// be guesswork, so that returns an error instead.
func Normalize(notes []Note) ([]Note, []Adjustment, error) {
	out := append([]Note(nil), notes...)

	// Index by pitch so each pitch is walked along its own timeline. Comparing
	// against whichever note comes next overall would shorten notes against
	// unrelated pitches.
	byPitch := map[int][]int{}
	for i, n := range out {
		byPitch[n.Pitch] = append(byPitch[n.Pitch], i)
	}

	pitches := make([]int, 0, len(byPitch))
	for pitch := range byPitch {
		pitches = append(pitches, pitch)
	}
	sort.Ints(pitches)

	var adjusted []Adjustment
	for _, pitch := range pitches {
		idx := byPitch[pitch]
		sort.SliceStable(idx, func(a, b int) bool {
			return out[idx[a]].StartTime < out[idx[b]].StartTime
		})

		for k := 0; k+1 < len(idx); k++ {
			cur, next := &out[idx[k]], out[idx[k+1]]
			gap := next.StartTime - cur.StartTime
			if gap <= 0 {
				name, err := FormatPitch(pitch)
				if err != nil {
					name = fmt.Sprintf("pitch %d", pitch)
				}
				return nil, nil, fmt.Errorf(
					"two %s notes start at the same position (%s); remove one or move it",
					name, formatBeats(cur.StartTime))
			}
			overrun := cur.Duration - gap
			if overrun <= BeatTolerance {
				continue
			}
			name, err := FormatPitch(pitch)
			if err != nil {
				name = fmt.Sprintf("pitch %d", pitch)
			}
			adjusted = append(adjusted, Adjustment{
				Position:    formatBeats(cur.StartTime),
				Pitch:       pitch,
				PitchName:   name,
				WasDuration: cur.Duration,
				NowDuration: gap,
			})
			cur.Duration = gap
		}
	}

	sort.SliceStable(adjusted, func(a, b int) bool {
		if adjusted[a].Position != adjusted[b].Position {
			return adjusted[a].Position < adjusted[b].Position
		}
		return adjusted[a].Pitch < adjusted[b].Pitch
	})
	return out, adjusted, nil
}
