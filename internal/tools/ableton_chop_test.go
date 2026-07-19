package tools

import (
	"math"
	"testing"
)

func intPtr(v int) *int             { return &v }
func int64Ptr(v int64) *int64       { return &v }
func float64Ptr(v float64) *float64 { return &v }
func boolPtr(v bool) *bool          { return &v }

func TestGenerateChopDraftDeterministic(t *testing.T) {
	t.Parallel()

	in := ChopDraftInput{
		NumSlices: 8,
		Bars:      intPtr(1),
		Grid:      "1/16",
		Density:   float64Ptr(1.0),
		Seed:      int64Ptr(42),
	}
	a, err := generateChopDraft(in)
	if err != nil {
		t.Fatalf("generateChopDraft() error = %v", err)
	}
	b, err := generateChopDraft(in)
	if err != nil {
		t.Fatalf("generateChopDraft() error = %v", err)
	}
	if len(a.Notes) != len(b.Notes) {
		t.Fatalf("non-deterministic length: %d vs %d", len(a.Notes), len(b.Notes))
	}
	for i := range a.Notes {
		if a.Notes[i] != b.Notes[i] {
			t.Fatalf("note %d differs across identical seeds: %#v vs %#v", i, a.Notes[i], b.Notes[i])
		}
	}
	// density 1.0 over 1 bar at 1/16 => 16 steps all filled.
	if len(a.Notes) != 16 {
		t.Fatalf("notes = %d, want 16 at density 1.0", len(a.Notes))
	}
	if a.LengthBeats != 4 || a.StepBeats != 0.25 {
		t.Errorf("length/step = %v/%v, want 4/0.25", a.LengthBeats, a.StepBeats)
	}
}

func TestGenerateChopDraftAvoidCopy(t *testing.T) {
	t.Parallel()

	in := ChopDraftInput{
		NumSlices: 8,
		Bars:      intPtr(2),
		Grid:      "1/16",
		Density:   float64Ptr(1.0),
		AvoidCopy: boolPtr(true),
		BaseNote:  intPtr(36),
		Seed:      int64Ptr(7),
	}
	out, err := generateChopDraft(in)
	if err != nil {
		t.Fatalf("generateChopDraft() error = %v", err)
	}
	for i, n := range out.Notes {
		if n.Pitch < 36 || n.Pitch > 43 {
			t.Errorf("note %d pitch %d out of slice range [36,43]", i, n.Pitch)
		}
		if n.Velocity < 1 || n.Velocity > 127 {
			t.Errorf("note %d velocity %d out of range", i, n.Velocity)
		}
		// start times align to the 1/16 grid.
		if r := math.Mod(n.StartTime, 0.25); math.Abs(r) > 1e-9 && math.Abs(r-0.25) > 1e-9 {
			t.Errorf("note %d start %v not on 1/16 grid", i, n.StartTime)
		}
		if i > 0 {
			prev := out.Notes[i-1].Pitch
			if n.Pitch == prev {
				t.Errorf("note %d repeats previous slice (avoid_copy)", i)
			}
			if n.Pitch == prev+1 {
				t.Errorf("note %d is ascending-consecutive to previous (avoid_copy)", i)
			}
		}
	}
}

func TestGenerateChopDraftValidation(t *testing.T) {
	t.Parallel()

	if _, err := generateChopDraft(ChopDraftInput{NumSlices: 0}); err == nil {
		t.Error("expected error for num_slices=0")
	}
	if _, err := generateChopDraft(ChopDraftInput{NumSlices: 120, BaseNote: intPtr(36)}); err == nil {
		t.Error("expected error when base_note+slices exceeds 127")
	}
	if _, err := generateChopDraft(ChopDraftInput{NumSlices: 4, Grid: "1/3"}); err == nil {
		t.Error("expected error for invalid grid")
	}
}

func TestGenerateChopDraftNeverEmpty(t *testing.T) {
	t.Parallel()

	out, err := generateChopDraft(ChopDraftInput{
		NumSlices: 4,
		Density:   float64Ptr(0.05),
		Seed:      int64Ptr(1),
	})
	if err != nil {
		t.Fatalf("generateChopDraft() error = %v", err)
	}
	if len(out.Notes) == 0 {
		t.Error("draft must contain at least one note")
	}
}
