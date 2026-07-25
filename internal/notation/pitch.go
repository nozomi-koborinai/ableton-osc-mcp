package notation

import (
	"fmt"
	"strconv"
)

// pitchNames spells every pitch class with a sharp and never a flat. Allowing
// both spellings would let one MIDI pitch print two ways, and the notation would
// stop being recoverable from its own text.
var pitchNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// FormatPitch renders a MIDI pitch the way Live's piano roll labels it, where
// MIDI 60 reads as C3.
func FormatPitch(pitch int) (string, error) {
	if pitch < 0 || pitch > 127 {
		return "", fmt.Errorf("pitch out of range: %d", pitch)
	}
	return fmt.Sprintf("%s%d", pitchNames[pitch%12], pitch/12-2), nil
}

// ParsePitch reads a note name in Live's octave numbering back into a MIDI pitch.
func ParsePitch(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty pitch")
	}
	nameLen := 1
	if len(s) > 1 && s[1] == '#' {
		nameLen = 2
	}
	name := s[:nameLen]
	class := -1
	for i, n := range pitchNames {
		if n == name {
			class = i
			break
		}
	}
	if class < 0 {
		return 0, fmt.Errorf("unknown note name %q in %q (write sharps, not flats)", name, s)
	}
	octave, err := strconv.Atoi(s[nameLen:])
	if err != nil {
		return 0, fmt.Errorf("bad octave in %q", s)
	}
	pitch := (octave+2)*12 + class
	if pitch < 0 || pitch > 127 {
		return 0, fmt.Errorf("pitch out of range: %q", s)
	}
	return pitch, nil
}
