package tools

import (
	"errors"
	"math"
	"math/rand"
	"testing"
)

// clipLengthStub stands in for Live when exercising queryClipLength, which the
// drum and bass variation builders both rely on to know where a clip ends.
type clipLengthStub struct {
	res []interface{}
	err error
}

func (s *clipLengthStub) Query(_ string, _ ...interface{}) ([]interface{}, error) {
	return s.res, s.err
}

func (s *clipLengthStub) Send(_ string, _ ...interface{}) error { return nil }

func TestQueryClipLength(t *testing.T) {
	tests := []struct {
		name string
		stub *clipLengthStub
		want float64
	}{
		{"normal_reply", &clipLengthStub{res: []interface{}{0, 0, 16.0}}, 16},
		{"query_failed", &clipLengthStub{err: errors.New("no reply")}, 0},
		{"empty_reply", &clipLengthStub{res: []interface{}{}}, 0},
		{"not_a_number", &clipLengthStub{res: []interface{}{0, 0, "x"}}, 0},
		// A clip cannot be zero or negative beats long; callers treat 0 as unknown
		// and skip clamping rather than clamping everything to nothing.
		{"zero_length", &clipLengthStub{res: []interface{}{0, 0, 0.0}}, 0},
		{"negative_length", &clipLengthStub{res: []interface{}{0, 0, -4.0}}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryClipLength(tt.stub, 0, 0); got != tt.want {
				t.Errorf("queryClipLength() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddNotesArgs(t *testing.T) {
	mute := true
	args := addNotesArgs(2, 3, []MidiNote{
		{Pitch: 60, StartTime: 0, Duration: 0.5, Velocity: 100},
		{Pitch: 62, StartTime: 1, Duration: 0.25, Velocity: 90, Mute: &mute},
	})
	want := []interface{}{
		int32(2), int32(3),
		int32(60), float32(0), float32(0.5), int32(100), false,
		int32(62), float32(1), float32(0.25), int32(90), true,
	}
	if len(args) != len(want) {
		t.Fatalf("addNotesArgs() length = %d, want %d", len(args), len(want))
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg %d = %v (%T), want %v (%T)", i, args[i], args[i], want[i], want[i])
		}
	}
}

func TestHumanizeNotesIsDeterministicWithSeed(t *testing.T) {
	t.Parallel()

	notes := []MidiNote{
		{Pitch: 36, StartTime: 0, Duration: 0.25, Velocity: 100},
		{Pitch: 38, StartTime: 1, Duration: 0.25, Velocity: 100},
		{Pitch: 42, StartTime: 0.5, Duration: 0.125, Velocity: 80},
	}
	opts := humanizeOptions{
		TimingAmount:   0.03,
		VelocityAmount: 12,
		Swing:          0.4,
		Strength:       0.8,
		Seed:           42,
	}

	a := humanizeNotes(notes, opts, rand.New(rand.NewSource(opts.Seed)), 0)
	b := humanizeNotes(notes, opts, rand.New(rand.NewSource(opts.Seed)), 0)
	if len(a) != len(b) {
		t.Fatalf("len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("note[%d] differs: %#v vs %#v", i, a[i], b[i])
		}
	}
}

func TestHumanizeNotesAppliesSwingAndBounds(t *testing.T) {
	t.Parallel()

	notes := []MidiNote{
		{Pitch: 42, StartTime: 0.5, Duration: 0.125, Velocity: 80},
		{Pitch: 36, StartTime: 0, Duration: 0.25, Velocity: 1},
	}
	opts := humanizeOptions{
		TimingAmount:   0,
		VelocityAmount: 40,
		Swing:          1,
		Strength:       1,
		Seed:           7,
	}
	got := humanizeNotes(notes, opts, rand.New(rand.NewSource(opts.Seed)), 0)

	if math.Abs(got[0].StartTime-applyEighthSwing(0.5, 1)) > 1e-9 {
		t.Errorf("offbeat start = %v, want swung value", got[0].StartTime)
	}
	if got[1].StartTime != 0 {
		t.Errorf("onbeat start = %v, want 0", got[1].StartTime)
	}
	for _, n := range got {
		if n.Velocity < 1 || n.Velocity > 127 {
			t.Errorf("velocity out of range: %d", n.Velocity)
		}
	}
}

func TestApplyEighthSwing(t *testing.T) {
	t.Parallel()

	if got := applyEighthSwing(0, 1); got != 0 {
		t.Errorf("onbeat swing = %v, want 0", got)
	}
	want := 0.5 + 0.5/3
	if got := applyEighthSwing(0.5, 1); math.Abs(got-want) > 1e-9 {
		t.Errorf("offbeat swing = %v, want %v", got, want)
	}
}

func TestHumanizeNotesClampsToClipLength(t *testing.T) {
	t.Parallel()

	notes := []MidiNote{
		{Pitch: 42, StartTime: 3.98, Duration: 0.125, Velocity: 80},
	}
	opts := humanizeOptions{
		TimingAmount:   0.08,
		VelocityAmount: 0,
		Strength:       1,
		Seed:           1,
	}
	got := humanizeNotes(notes, opts, rand.New(rand.NewSource(opts.Seed)), 4.0)
	if got[0].StartTime >= 4.0 {
		t.Errorf("start = %v, want < clip length 4.0", got[0].StartTime)
	}
	if got[0].StartTime < 0 {
		t.Errorf("start = %v, want >= 0", got[0].StartTime)
	}
}
