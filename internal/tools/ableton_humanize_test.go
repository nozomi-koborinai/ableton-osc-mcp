package tools

import (
	"errors"
	"math"
	"math/rand"
	"strings"
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

func TestHumanizeClip(t *testing.T) {
	t.Parallel()

	seed := int64(99)
	strength := 0.5
	client := &humanizeClientStub{
		notesRes: []interface{}{
			int32(1), int32(0),
			int32(36), float32(0), float32(0.25), int32(100), false,
			int32(38), float32(1), float32(0.25), int32(100), false,
		},
		lengthRes: []interface{}{int32(1), int32(0), float32(4)},
	}

	got, err := humanizeClip(client, HumanizeClipInput{
		TrackIndex: 1,
		ClipIndex:  0,
		Strength:   &strength,
		Seed:       &seed,
	})
	if err != nil {
		t.Fatalf("humanizeClip() error = %v", err)
	}
	if got.NotesUpdated != 2 {
		t.Errorf("NotesUpdated = %d, want 2", got.NotesUpdated)
	}
	if got.Seed != seed {
		t.Errorf("Seed = %d, want %d", got.Seed, seed)
	}
	if got.Strength != strength {
		t.Errorf("Strength = %v, want %v", got.Strength, strength)
	}

	wantCalls := []string{
		"Query:/live/clip/get/notes",
		"Query:/live/clip/get/length",
		"Send:/live/clip/remove/notes",
		"Send:/live/clip/add/notes",
	}
	if len(client.calls) != len(wantCalls) {
		t.Fatalf("calls = %v, want %v", client.calls, wantCalls)
	}
	for i := range wantCalls {
		if client.calls[i] != wantCalls[i] {
			t.Errorf("calls[%d] = %q, want %q", i, client.calls[i], wantCalls[i])
		}
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

func TestHumanizeClipRestoresOnAddFailure(t *testing.T) {
	t.Parallel()

	seed := int64(5)
	client := &humanizeClientStub{
		notesRes: []interface{}{
			int32(0), int32(0),
			int32(36), float32(0), float32(0.25), int32(100), false,
		},
		failAddOnce: true,
	}
	_, err := humanizeClip(client, HumanizeClipInput{TrackIndex: 0, ClipIndex: 0, Seed: &seed})
	if err == nil {
		t.Fatal("humanizeClip() error = nil, want add-failure error")
	}
	if !strings.Contains(err.Error(), "restored") {
		t.Errorf("error = %q, want restore note", err)
	}
	if len(client.addCalls) != 2 {
		t.Fatalf("add/notes called %d times, want 2 (humanized + restore)", len(client.addCalls))
	}
	// The restore call must carry the original note payload (start time 0, velocity 100).
	restore := client.addCalls[1]
	if len(restore) != 7 {
		t.Fatalf("restore args len = %d, want 7", len(restore))
	}
	if restore[3] != float32(0) || restore[5] != int32(100) {
		t.Errorf("restore payload = %v, want original note", restore)
	}
}

func TestHumanizeClipRejectsEmptyClip(t *testing.T) {
	t.Parallel()

	client := &humanizeClientStub{
		notesRes: []interface{}{int32(0), int32(0)},
	}
	_, err := humanizeClip(client, HumanizeClipInput{TrackIndex: 0, ClipIndex: 0})
	if err == nil {
		t.Fatal("humanizeClip() error = nil, want empty-clip error")
	}
}

func TestResolveHumanizeOptionsValidation(t *testing.T) {
	t.Parallel()

	badTiming := 1.0
	_, err := resolveHumanizeOptions(HumanizeClipInput{TimingAmount: &badTiming})
	if err == nil {
		t.Fatal("expected timing_amount validation error")
	}

	badSwing := 1.5
	_, err = resolveHumanizeOptions(HumanizeClipInput{Swing: &badSwing})
	if err == nil {
		t.Fatal("expected swing validation error")
	}
}
