package notation

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// BeatsPerBar returns how many beats one bar holds. Live counts clip time in
// quarter notes no matter what the signature says, so 6/8 is three beats, not six.
func BeatsPerBar(sigNum, sigDen int) (float64, error) {
	if sigNum <= 0 || sigDen <= 0 {
		return 0, fmt.Errorf("bad time signature %d/%d", sigNum, sigDen)
	}
	return float64(sigNum) * 4 / float64(sigDen), nil
}

// FormatPosition writes a start time as bar and beat, both counted from one.
// The separator is a colon rather than a dot so a fractional beat cannot be
// misread as a third field.
func FormatPosition(startTime, beatsPerBar float64) string {
	bar := int(math.Floor(startTime/beatsPerBar)) + 1
	beat := startTime - float64(bar-1)*beatsPerBar + 1
	return fmt.Sprintf("%d:%s", bar, formatBeats(beat))
}

// ParsePosition turns a bar and beat back into beats from the clip start.
func ParsePosition(s string, beatsPerBar float64) (float64, error) {
	barText, beatText, ok := strings.Cut(s, ":")
	if !ok {
		return 0, fmt.Errorf("position must look like 2:1.5, got %q", s)
	}
	bar, err := strconv.Atoi(barText)
	if err != nil || bar < 1 {
		return 0, fmt.Errorf("bar must be a whole number from 1, got %q", s)
	}
	beat, err := strconv.ParseFloat(beatText, 64)
	if err != nil || beat < 1 {
		return 0, fmt.Errorf("beat must be a number from 1, got %q", s)
	}
	return float64(bar-1)*beatsPerBar + (beat - 1), nil
}
