package tools

import (
	"errors"
	"math"
	"math/rand"
	"testing"
)

type humanizeClientStub struct {
	notesRes    []interface{}
	lengthRes   []interface{}
	calls       []string
	sendErr     map[string]error
	failAddOnce bool
	addCalls    [][]interface{}
}

func (s *humanizeClientStub) Query(address string, _ ...interface{}) ([]interface{}, error) {
	s.calls = append(s.calls, "Query:"+address)
	switch address {
	case "/live/clip/get/notes":
		return s.notesRes, nil
	case "/live/clip/get/length":
		if s.lengthRes == nil {
			return nil, errors.New("no length")
		}
		return s.lengthRes, nil
	default:
		return nil, errors.New("unexpected query")
	}
}

func (s *humanizeClientStub) Send(address string, args ...interface{}) error {
	s.calls = append(s.calls, "Send:"+address)
	if address == "/live/clip/add/notes" {
		s.addCalls = append(s.addCalls, args)
		if s.failAddOnce && len(s.addCalls) == 1 {
			return errors.New("add failed")
		}
	}
	if err, ok := s.sendErr[address]; ok {
		return err
	}
	return nil
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
