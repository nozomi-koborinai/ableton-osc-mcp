package notation

import (
	"fmt"
	"strconv"
	"strings"
)

// durationTokens lists every note value the notation is willing to name. All of
// them are powers of two or their dotted forms, so each one is exact in binary
// and matches at most one beat count. Triplets are left out on purpose: a third
// of a beat cannot be written exactly, and admitting it would need a fuzzy match
// that lets one length print two ways.
var durationTokens = []struct {
	Token string
	Beats float64
}{
	{"1/1.", 6}, {"1/1", 4},
	{"1/2.", 3}, {"1/2", 2},
	{"1/4.", 1.5}, {"1/4", 1},
	{"1/8.", 0.75}, {"1/8", 0.5},
	{"1/16.", 0.375}, {"1/16", 0.25},
	{"1/32.", 0.1875}, {"1/32", 0.125},
}

// FormatDuration names a length when the beat count is exactly a known note
// value, and falls back to a plain decimal otherwise.
func FormatDuration(beats float64) string {
	for _, d := range durationTokens {
		if beats == d.Beats {
			return d.Token
		}
	}
	return formatBeats(beats)
}

// ParseDuration accepts either a note value token or a decimal beat count.
func ParseDuration(s string) (float64, error) {
	for _, d := range durationTokens {
		if s == d.Token {
			return d.Beats, nil
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("length must be a note value like 1/8 or 1/8., or a decimal beat count, got %q", s)
	}
	if v <= 0 {
		return 0, fmt.Errorf("length must be greater than zero, got %q", s)
	}
	return v, nil
}

// formatBeats prints a beat count so that one value always yields one spelling:
// six decimal places, trailing zeros removed, no dangling decimal point.
func formatBeats(v float64) string {
	s := strconv.FormatFloat(v, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	return s
}
