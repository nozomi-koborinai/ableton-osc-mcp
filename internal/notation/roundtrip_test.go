package notation

import (
	"strings"
	"testing"
)

func sampleClip() Clip {
	return Clip{
		Name: "Round Trip", Bars: 4, SigNum: 4, SigDen: 4,
		Notes: []Note{
			{Pitch: 60, StartTime: 0, Duration: 0.5, Velocity: 100},
			{Pitch: 63, StartTime: 2, Duration: 0.5, Velocity: 92},
			{Pitch: 70, StartTime: 6, Duration: 0.5, Velocity: 88, Mute: true},
			{Pitch: 72, StartTime: 8.5, Duration: 0.75, Velocity: 96},
			{Pitch: 75, StartTime: 10, Duration: 1.0 / 3.0, Velocity: 90},
			{Pitch: 48, StartTime: 20, Duration: 1, Velocity: 60}, // past the clip length
		},
	}
}

func TestTextModelText(t *testing.T) {
	first, err := Format(sampleClip())
	if err != nil {
		t.Fatalf("Format error = %v", err)
	}
	parsed, err := Parse(first)
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	second, err := Format(parsed)
	if err != nil {
		t.Fatalf("Format error = %v", err)
	}
	if first != second {
		t.Errorf("text is not stable across a round trip:\n%q\n%q", first, second)
	}
}

func TestModelTextModel(t *testing.T) {
	c := sampleClip()
	text, err := Format(c)
	if err != nil {
		t.Fatalf("Format error = %v", err)
	}
	back, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	if m := Diff(c.Notes, back.Notes); len(m) != 0 {
		t.Errorf("notes changed across a round trip: %+v", m)
	}
}

func TestRevChangesWithContent(t *testing.T) {
	a, err := Format(sampleClip())
	if err != nil {
		t.Fatalf("Format error = %v", err)
	}
	changed := sampleClip()
	changed.Notes[0].Velocity = 101
	b, err := Format(changed)
	if err != nil {
		t.Fatalf("Format error = %v", err)
	}
	if Rev(a) == Rev(b) {
		t.Error("rev should change when a note changes")
	}
	// Formatting the same clip again must land on the same rev, otherwise a write
	// would be refused for a clip nobody touched.
	again, err := Format(sampleClip())
	if err != nil {
		t.Fatalf("Format error = %v", err)
	}
	if Rev(again) != Rev(a) {
		t.Error("rev should be stable for the same clip")
	}
	if len(Rev(a)) != 12 {
		t.Errorf("rev length = %d, want 12", len(Rev(a)))
	}
}

func TestDiffWithinTolerance(t *testing.T) {
	want := []Note{{Pitch: 60, StartTime: 1, Duration: 0.5, Velocity: 100}}
	got := []Note{{Pitch: 60, StartTime: 1 + BeatTolerance/2, Duration: 0.5, Velocity: 100}}
	if m := Diff(want, got); len(m) != 0 {
		t.Errorf("a difference inside the tolerance should not be reported: %+v", m)
	}
}

func TestDiffCoversFloat32ErrorAtRealPositions(t *testing.T) {
	// The measured worst case was 7.9e-6 beats at beat 511; the tolerance has to
	// swallow that, or long clips would report mismatches that are not real.
	want := []Note{{Pitch: 60, StartTime: 511.7519, Duration: 1.9377, Velocity: 100}}
	got := []Note{{Pitch: 60, StartTime: 511.751892090, Duration: 1.937700033, Velocity: 100}}
	if m := Diff(want, got); len(m) != 0 {
		t.Errorf("float32 rounding at a real position should not be reported: %+v", m)
	}
}

func TestDiffReportsRealChanges(t *testing.T) {
	want := []Note{{Pitch: 60, StartTime: 1, Duration: 0.5, Velocity: 100}}
	cases := map[string][]Note{
		"start":    {{Pitch: 60, StartTime: 2, Duration: 0.5, Velocity: 100}},
		"pitch":    {{Pitch: 61, StartTime: 1, Duration: 0.5, Velocity: 100}},
		"duration": {{Pitch: 60, StartTime: 1, Duration: 1, Velocity: 100}},
		"velocity": {{Pitch: 60, StartTime: 1, Duration: 0.5, Velocity: 99}},
		"mute":     {{Pitch: 60, StartTime: 1, Duration: 0.5, Velocity: 100, Mute: true}},
	}
	for field, got := range cases {
		t.Run(field, func(t *testing.T) {
			m := Diff(want, got)
			if len(m) == 0 {
				t.Fatalf("expected a mismatch for %s", field)
			}
		})
	}
}

func TestDiffCatchesTheFinestMusicalGrid(t *testing.T) {
	// A 128th note is the smallest thing anyone edits on. If the tolerance ever
	// grew past it, a real mistake would slip through as a match.
	const oneTwentyEighthNote = 0.03125
	want := []Note{{Pitch: 60, StartTime: 1, Duration: 0.5, Velocity: 100}}
	got := []Note{{Pitch: 60, StartTime: 1 + oneTwentyEighthNote, Duration: 0.5, Velocity: 100}}
	if m := Diff(want, got); len(m) == 0 {
		t.Error("a 128th-note shift must be reported")
	}
}

func TestDiffReportsCountChange(t *testing.T) {
	want := []Note{{Pitch: 60, StartTime: 0, Duration: 1, Velocity: 100}}
	m := Diff(want, nil)
	if len(m) == 0 || !strings.Contains(m[0].Field, "count") {
		t.Errorf("a missing note should be reported as a count change, got %+v", m)
	}
}
