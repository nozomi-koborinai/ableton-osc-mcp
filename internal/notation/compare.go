package notation

import (
	"fmt"
	"math"
	"sort"
)

// Mismatch is one difference found when a written clip is read back.
type Mismatch struct {
	Index int    `json:"index"`
	Field string `json:"field"`
	Want  string `json:"want"`
	Got   string `json:"got"`
}

// Diff compares two sets of notes the way the round-trip check needs: positions
// and lengths within BeatTolerance count as equal because OSC carries them as
// float32, while pitch, velocity and mute must match exactly.
func Diff(want, got []Note) []Mismatch {
	a := sortedForCompare(want)
	b := sortedForCompare(got)

	if len(a) != len(b) {
		return []Mismatch{{
			Index: -1,
			Field: "note count",
			Want:  fmt.Sprintf("%d", len(a)),
			Got:   fmt.Sprintf("%d", len(b)),
		}}
	}

	var out []Mismatch
	for i := range a {
		if a[i].Pitch != b[i].Pitch {
			out = append(out, Mismatch{i, "pitch", fmt.Sprint(a[i].Pitch), fmt.Sprint(b[i].Pitch)})
		}
		if math.Abs(a[i].StartTime-b[i].StartTime) > BeatTolerance {
			out = append(out, Mismatch{i, "start", formatBeats(a[i].StartTime), formatBeats(b[i].StartTime)})
		}
		if math.Abs(a[i].Duration-b[i].Duration) > BeatTolerance {
			out = append(out, Mismatch{i, "duration", formatBeats(a[i].Duration), formatBeats(b[i].Duration)})
		}
		if a[i].Velocity != b[i].Velocity {
			out = append(out, Mismatch{i, "velocity", fmt.Sprint(a[i].Velocity), fmt.Sprint(b[i].Velocity)})
		}
		if a[i].Mute != b[i].Mute {
			out = append(out, Mismatch{i, "mute", fmt.Sprint(a[i].Mute), fmt.Sprint(b[i].Mute)})
		}
	}
	return out
}

func sortedForCompare(notes []Note) []Note {
	out := append([]Note(nil), notes...)
	sort.SliceStable(out, func(i, j int) bool {
		x, y := out[i], out[j]
		switch {
		case x.StartTime != y.StartTime:
			return x.StartTime < y.StartTime
		case x.Pitch != y.Pitch:
			return x.Pitch < y.Pitch
		case x.Duration != y.Duration:
			return x.Duration < y.Duration
		default:
			return x.Velocity < y.Velocity
		}
	})
	return out
}
