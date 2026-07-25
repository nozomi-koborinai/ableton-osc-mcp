package notation

import (
	"math"
	"testing"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name  string
		beats float64
		want  string
	}{
		{"whole", 4, "1/1"},
		{"quarter", 1, "1/4"},
		{"eighth", 0.5, "1/8"},
		{"dotted_eighth", 0.75, "1/8."},
		{"thirty_second", 0.125, "1/32"},
		{"triplet_falls_back_to_decimal", 1.0 / 3.0, "0.333333"},
		{"odd_value_falls_back", 1.9377, "1.9377"},
		{"long_value_falls_back", 7, "7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatDuration(tt.beats); got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.beats, got, tt.want)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"1/1", 4},
		{"1/4", 1},
		{"1/8.", 0.75},
		{"1/32", 0.125},
		{"0.333333", 0.333333},
		{"7", 7},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseDuration(tt.in)
			if err != nil {
				t.Fatalf("ParseDuration(%q) error = %v", tt.in, err)
			}
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("ParseDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseDurationRejects(t *testing.T) {
	for _, in := range []string{"", "1/8t", "1/5", "quarter", "0", "-1"} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) expected an error", in)
		}
	}
}

func TestDurationTokenRoundTrip(t *testing.T) {
	for _, tok := range durationTokens {
		got := FormatDuration(tok.Beats)
		if got != tok.Token {
			t.Errorf("FormatDuration(%v) = %q, want %q", tok.Beats, got, tok.Token)
		}
		back, err := ParseDuration(got)
		if err != nil {
			t.Fatalf("ParseDuration(%q) error = %v", got, err)
		}
		if back != tok.Beats {
			t.Errorf("round trip %v -> %q -> %v", tok.Beats, got, back)
		}
	}
}
