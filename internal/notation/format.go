package notation

import (
	"fmt"
	"sort"
	"strings"
)

// Format renders a clip as canonical notation text. Notes come out in a fixed
// order and every field has one spelling, so the same clip always produces the
// same bytes. The rev hash depends on that.
func Format(c Clip) (string, error) {
	beatsPerBar, err := BeatsPerBar(c.SigNum, c.SigDen)
	if err != nil {
		return "", err
	}

	notes := append([]Note(nil), c.Notes...)
	sort.SliceStable(notes, func(i, j int) bool {
		a, b := notes[i], notes[j]
		switch {
		case a.StartTime != b.StartTime:
			return a.StartTime < b.StartTime
		case a.Pitch != b.Pitch:
			return a.Pitch < b.Pitch
		case a.Duration != b.Duration:
			return a.Duration < b.Duration
		default:
			return a.Velocity < b.Velocity
		}
	})

	var b strings.Builder
	fmt.Fprintf(&b, "clip %q bars=%d sig=%d/%d\n\n", c.Name, c.Bars, c.SigNum, c.SigDen)
	for _, n := range notes {
		pitch, err := FormatPitch(n.Pitch)
		if err != nil {
			return "", err
		}
		if n.Velocity < 1 || n.Velocity > 127 {
			return "", fmt.Errorf("velocity out of range: %d", n.Velocity)
		}
		if n.Duration <= 0 {
			return "", fmt.Errorf("duration must be greater than zero: %v", n.Duration)
		}
		fmt.Fprintf(&b, "  %s %s %s v%d",
			FormatPosition(n.StartTime, beatsPerBar), pitch, FormatDuration(n.Duration), n.Velocity)
		if n.Mute {
			b.WriteString(" -")
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}
