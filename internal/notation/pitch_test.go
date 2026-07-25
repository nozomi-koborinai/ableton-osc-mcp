package notation

import "testing"

func TestFormatPitch(t *testing.T) {
	tests := []struct {
		name  string
		pitch int
		want  string
	}{
		{"live_middle_c", 60, "C3"},
		{"lowest", 0, "C-2"},
		{"highest", 127, "G8"},
		{"sharp_never_flat", 63, "D#3"},
		{"b_natural", 71, "B3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FormatPitch(tt.pitch)
			if err != nil {
				t.Fatalf("FormatPitch(%d) error = %v", tt.pitch, err)
			}
			if got != tt.want {
				t.Errorf("FormatPitch(%d) = %q, want %q", tt.pitch, got, tt.want)
			}
		})
	}
}

func TestFormatPitchOutOfRange(t *testing.T) {
	for _, pitch := range []int{-1, 128} {
		if _, err := FormatPitch(pitch); err == nil {
			t.Errorf("FormatPitch(%d) expected an error", pitch)
		}
	}
}

func TestParsePitch(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"C3", 60},
		{"C-2", 0},
		{"G8", 127},
		{"D#3", 63},
		{"B3", 71},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParsePitch(tt.in)
			if err != nil {
				t.Fatalf("ParsePitch(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParsePitch(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParsePitchRejectsFlats(t *testing.T) {
	// A flat spelling would give one pitch two spellings and break the round trip.
	for _, in := range []string{"Eb3", "Bb3", "", "H3", "C", "C##3"} {
		if _, err := ParsePitch(in); err == nil {
			t.Errorf("ParsePitch(%q) expected an error", in)
		}
	}
}

func TestPitchRoundTrip(t *testing.T) {
	for pitch := 0; pitch <= 127; pitch++ {
		s, err := FormatPitch(pitch)
		if err != nil {
			t.Fatalf("FormatPitch(%d) error = %v", pitch, err)
		}
		got, err := ParsePitch(s)
		if err != nil {
			t.Fatalf("ParsePitch(%q) error = %v", s, err)
		}
		if got != pitch {
			t.Errorf("round trip %d -> %q -> %d", pitch, s, got)
		}
	}
}
