package notation

import (
	"math"
	"strings"
	"testing"
)

func TestNormalizeShortensOverlappingSamePitch(t *testing.T) {
	// The case that showed up the first time the notation met a real drill hat
	// pattern: a pushed 16th runs into the straight one that follows it.
	notes := []Note{
		{Pitch: 42, StartTime: 0, Duration: 0.25, Velocity: 104},
		{Pitch: 42, StartTime: 0.262, Duration: 0.25, Velocity: 68},
		{Pitch: 42, StartTime: 0.5, Duration: 0.25, Velocity: 86},
	}
	got, adjusted, err := Normalize(notes)
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if len(adjusted) != 1 {
		t.Fatalf("adjusted = %+v, want exactly one", adjusted)
	}
	if math.Abs(got[1].Duration-0.238) > 1e-9 {
		t.Errorf("shortened duration = %v, want 0.238", got[1].Duration)
	}
	if adjusted[0].Pitch != 42 || math.Abs(adjusted[0].WasDuration-0.25) > 1e-9 {
		t.Errorf("adjustment = %+v", adjusted[0])
	}
	// The notes that did not overlap must come through untouched.
	if got[0].Duration != 0.25 || got[2].Duration != 0.25 {
		t.Errorf("untouched notes changed: %+v", got)
	}
}

func TestNormalizeLeavesChordsAlone(t *testing.T) {
	// Different pitches sounding together is a chord, which Live holds happily.
	// Shortening these would wreck every chord the notation writes.
	notes := []Note{
		{Pitch: 60, StartTime: 0, Duration: 4, Velocity: 90},
		{Pitch: 63, StartTime: 0, Duration: 4, Velocity: 88},
		{Pitch: 67, StartTime: 0, Duration: 4, Velocity: 86},
	}
	got, adjusted, err := Normalize(notes)
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if len(adjusted) != 0 {
		t.Errorf("a chord should not be adjusted, got %+v", adjusted)
	}
	for i := range got {
		if got[i].Duration != 4 {
			t.Errorf("note %d duration = %v, want 4", i, got[i].Duration)
		}
	}
}

func TestNormalizeLeavesTouchingNotesAlone(t *testing.T) {
	// Ending exactly where the next one starts is already legal.
	notes := []Note{
		{Pitch: 42, StartTime: 0, Duration: 0.5, Velocity: 100},
		{Pitch: 42, StartTime: 0.5, Duration: 0.5, Velocity: 100},
	}
	_, adjusted, err := Normalize(notes)
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if len(adjusted) != 0 {
		t.Errorf("touching notes should not be adjusted, got %+v", adjusted)
	}
}

func TestNormalizeIgnoresOverrunsInsideTolerance(t *testing.T) {
	// Below BeatTolerance the truncation Live performs would not register as a
	// mismatch anyway, so adjusting here would only produce noise.
	notes := []Note{
		{Pitch: 42, StartTime: 0, Duration: 0.5 + BeatTolerance/2, Velocity: 100},
		{Pitch: 42, StartTime: 0.5, Duration: 0.5, Velocity: 100},
	}
	_, adjusted, err := Normalize(notes)
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if len(adjusted) != 0 {
		t.Errorf("an overrun inside the tolerance should be left alone, got %+v", adjusted)
	}
}

func TestNormalizeRejectsSamePitchSameStart(t *testing.T) {
	notes := []Note{
		{Pitch: 42, StartTime: 1.5, Duration: 0.5, Velocity: 100},
		{Pitch: 42, StartTime: 1.5, Duration: 0.25, Velocity: 80},
	}
	_, _, err := Normalize(notes)
	if err == nil {
		t.Fatal("expected an error for two notes of one pitch at one position")
	}
	// The message has to say which note, or there is nothing to act on.
	if !strings.Contains(err.Error(), "F#1") {
		t.Errorf("error should name the pitch, got %v", err)
	}
}

func TestNormalizeHandlesManyPitchesIndependently(t *testing.T) {
	// Two overlapping runs on different pitches must each be fixed on their own
	// timeline, not against whichever note happens to come next overall.
	notes := []Note{
		{Pitch: 42, StartTime: 0, Duration: 1, Velocity: 100},
		{Pitch: 36, StartTime: 0.25, Duration: 1, Velocity: 100},
		{Pitch: 42, StartTime: 0.5, Duration: 0.5, Velocity: 100},
		{Pitch: 36, StartTime: 0.75, Duration: 0.5, Velocity: 100},
	}
	got, adjusted, err := Normalize(notes)
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if len(adjusted) != 2 {
		t.Fatalf("adjusted = %+v, want two", adjusted)
	}
	durationAt := func(pitch int, start float64) float64 {
		t.Helper()
		for _, n := range got {
			if n.Pitch == pitch && math.Abs(n.StartTime-start) < 1e-9 {
				return n.Duration
			}
		}
		t.Fatalf("no note of pitch %d at %v in %+v", pitch, start, got)
		return 0
	}
	// Each first note is cut back to where the next note of the SAME pitch starts,
	// not to the next note overall — which would have been 0.25 and 0.5 here.
	if got := durationAt(42, 0); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("pitch 42 at 0: duration = %v, want 0.5", got)
	}
	if got := durationAt(36, 0.25); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("pitch 36 at 0.25: duration = %v, want 0.5", got)
	}
}

func TestNormalizeDoesNotMutateInput(t *testing.T) {
	notes := []Note{
		{Pitch: 42, StartTime: 0, Duration: 0.5, Velocity: 100},
		{Pitch: 42, StartTime: 0.25, Duration: 0.5, Velocity: 100},
	}
	if _, _, err := Normalize(notes); err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if notes[0].Duration != 0.5 {
		t.Errorf("input was mutated: %+v", notes[0])
	}
}
