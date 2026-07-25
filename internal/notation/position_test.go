package notation

import (
	"math"
	"testing"
)

func TestBeatsPerBar(t *testing.T) {
	tests := []struct {
		num, den int
		want     float64
	}{
		{4, 4, 4},
		{3, 4, 3},
		{6, 8, 3},
		{7, 8, 3.5},
	}
	for _, tt := range tests {
		got, err := BeatsPerBar(tt.num, tt.den)
		if err != nil {
			t.Fatalf("BeatsPerBar(%d,%d) error = %v", tt.num, tt.den, err)
		}
		if got != tt.want {
			t.Errorf("BeatsPerBar(%d,%d) = %v, want %v", tt.num, tt.den, got, tt.want)
		}
	}
	for _, bad := range [][2]int{{0, 4}, {4, 0}, {-1, 4}} {
		if _, err := BeatsPerBar(bad[0], bad[1]); err == nil {
			t.Errorf("BeatsPerBar(%d,%d) expected an error", bad[0], bad[1])
		}
	}
}

func TestFormatPosition(t *testing.T) {
	tests := []struct {
		name      string
		startTime float64
		bpb       float64
		want      string
	}{
		{"clip_start", 0, 4, "1:1"},
		{"third_beat", 2, 4, "1:3"},
		{"second_bar", 4, 4, "2:1"},
		{"off_grid", 8.5, 4, "3:1.5"},
		{"three_four", 3, 3, "2:1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatPosition(tt.startTime, tt.bpb); got != tt.want {
				t.Errorf("FormatPosition(%v,%v) = %q, want %q", tt.startTime, tt.bpb, got, tt.want)
			}
		})
	}
}

func TestParsePosition(t *testing.T) {
	tests := []struct {
		in   string
		bpb  float64
		want float64
	}{
		{"1:1", 4, 0},
		{"1:3", 4, 2},
		{"2:1", 4, 4},
		{"3:1.5", 4, 8.5},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParsePosition(tt.in, tt.bpb)
			if err != nil {
				t.Fatalf("ParsePosition(%q) error = %v", tt.in, err)
			}
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("ParsePosition(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParsePositionRejects(t *testing.T) {
	// Bars and beats both start at one, so zero and negative values are mistakes,
	// and a bare number has no bar to attach to.
	for _, in := range []string{"", "1", "0:1", "1:0", "a:1", "1:b", "1:1:1"} {
		if _, err := ParsePosition(in, 4); err == nil {
			t.Errorf("ParsePosition(%q) expected an error", in)
		}
	}
}

func TestPositionRoundTrip(t *testing.T) {
	bpb := 4.0
	for _, start := range []float64{0, 0.25, 1, 2.5, 4, 7.75, 15.125} {
		s := FormatPosition(start, bpb)
		got, err := ParsePosition(s, bpb)
		if err != nil {
			t.Fatalf("ParsePosition(%q) error = %v", s, err)
		}
		if math.Abs(got-start) > 1e-9 {
			t.Errorf("round trip %v -> %q -> %v", start, s, got)
		}
	}
}
