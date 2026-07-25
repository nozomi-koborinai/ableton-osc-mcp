package tools

import (
	"strings"
	"testing"
)

type clipNotationStub struct {
	hasClip bool
	isAudio bool
	name    string
	length  float64
	sigNum  int
	sigDen  int

	// notes is what Live reports before anything is written. notesAfterWrite, if
	// set, is what it reports once the notes have been replaced. Keeping the two
	// apart matters: a write re-reads the clip to check the rev BEFORE applying,
	// so a stub that returned the post-write notes the whole time would make every
	// rev check fail and hide what the test is trying to prove.
	notes           []interface{}
	notesAfterWrite []interface{}
	written         bool

	queryErr map[string]error
	sends    []string
	sendArgs map[string][]interface{}
	sendErr  map[string]error
}

func (s *clipNotationStub) Query(address string, _ ...interface{}) ([]interface{}, error) {
	if err := s.queryErr[address]; err != nil {
		return nil, err
	}
	switch address {
	case "/live/clip_slot/get/has_clip":
		return []interface{}{0, 0, s.hasClip}, nil
	case "/live/clip/get/is_audio_clip":
		return []interface{}{0, 0, s.isAudio}, nil
	case "/live/clip/get/name":
		return []interface{}{0, 0, s.name}, nil
	case "/live/clip/get/length":
		return []interface{}{0, 0, s.length}, nil
	case "/live/song/get/signature_numerator":
		return []interface{}{s.sigNum}, nil
	case "/live/song/get/signature_denominator":
		return []interface{}{s.sigDen}, nil
	case "/live/clip/get/notes":
		if s.written && s.notesAfterWrite != nil {
			return append([]interface{}{0, 0}, s.notesAfterWrite...), nil
		}
		return append([]interface{}{0, 0}, s.notes...), nil
	}
	return nil, errUnexpectedAddress(address)
}

func (s *clipNotationStub) Send(address string, args ...interface{}) error {
	s.sends = append(s.sends, address)
	if s.sendArgs == nil {
		s.sendArgs = map[string][]interface{}{}
	}
	s.sendArgs[address] = args
	if address == "/live/clip/add/notes" || address == "/live/clip/remove/notes" {
		s.written = true
	}
	return s.sendErr[address]
}

func errUnexpectedAddress(address string) error {
	return &ActionableError{Code: "test_unexpected_address", Message: address}
}

func newReadStub() *clipNotationStub {
	return &clipNotationStub{
		hasClip: true,
		name:    "Chorus Lead",
		length:  16,
		sigNum:  4,
		sigDen:  4,
		notes: []interface{}{
			60, 0.0, 0.5, 100, false,
			63, 2.0, 0.5, 92, false,
		},
	}
}

func TestReadClipNotation(t *testing.T) {
	out, err := readClipNotation(newReadStub(), ClipReadInput{TrackIndex: 0, ClipIndex: 0})
	if err != nil {
		t.Fatalf("readClipNotation error = %v", err)
	}
	want := "clip \"Chorus Lead\" bars=4 sig=4/4\n\n" +
		"  1:1 C3 1/8 v100\n" +
		"  1:3 D#3 1/8 v92\n"
	if out.Notation != want {
		t.Errorf("notation =\n%q\nwant\n%q", out.Notation, want)
	}
	if out.Rev == "" {
		t.Error("rev must not be empty")
	}
}

func TestReadClipNotationRejectsAudioClip(t *testing.T) {
	stub := newReadStub()
	stub.isAudio = true
	_, err := readClipNotation(stub, ClipReadInput{})
	assertActionable(t, err, "clip_is_audio")
}

func TestReadClipNotationRejectsMissingClip(t *testing.T) {
	stub := newReadStub()
	stub.hasClip = false
	_, err := readClipNotation(stub, ClipReadInput{})
	assertActionable(t, err, "clip_not_found")
}

func TestReadClipNotationRejectsNegativeIndices(t *testing.T) {
	if _, err := readClipNotation(newReadStub(), ClipReadInput{TrackIndex: -1}); err == nil {
		t.Error("expected an error for a negative track index")
	}
}

func assertActionable(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q", wantCode)
	}
	if !strings.Contains(err.Error(), wantCode) {
		t.Errorf("error = %v, want code %q", err, wantCode)
	}
}

func validNotation() string {
	return "clip \"Chorus Lead\" bars=4 sig=4/4\n\n" +
		"  1:1 C3 1/8 v100\n" +
		"  1:3 D#3 1/8 v92\n"
}

func TestWriteClipNotationRejectsBadNotationBeforeTouchingLive(t *testing.T) {
	stub := newReadStub()
	_, err := writeClipNotation(stub, ClipWriteInput{Notation: "not a clip", Rev: "whatever"})
	assertActionable(t, err, "notation_parse_error")
	if len(stub.sends) != 0 {
		t.Errorf("nothing should have been sent to Live, got %v", stub.sends)
	}
}

func TestWriteClipNotationRejectsStaleRev(t *testing.T) {
	stub := newReadStub()
	_, err := writeClipNotation(stub, ClipWriteInput{Notation: validNotation(), Rev: "000000000000"})
	assertActionable(t, err, "rev_mismatch")
	if len(stub.sends) != 0 {
		t.Errorf("a stale write must not touch Live, got %v", stub.sends)
	}
}

func TestWriteClipNotationRejectsBarsMismatch(t *testing.T) {
	stub := newReadStub()
	current, err := readClipNotation(stub, ClipReadInput{})
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	wrongBars := "clip \"Chorus Lead\" bars=8 sig=4/4\n\n  1:1 C3 1/8 v100\n"
	_, err = writeClipNotation(stub, ClipWriteInput{Notation: wrongBars, Rev: current.Rev})
	assertActionable(t, err, "bars_mismatch")
}

func TestWriteClipNotationAppliesAndVerifies(t *testing.T) {
	stub := newReadStub()
	current, err := readClipNotation(stub, ClipReadInput{})
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	// After the write, Live reports back exactly what was asked for.
	stub.notesAfterWrite = []interface{}{
		60, 0.0, 0.5, 100, false,
		67, 4.0, 1.0, 104, false,
	}
	text := "clip \"Chorus Lead\" bars=4 sig=4/4\n\n  1:1 C3 1/8 v100\n  2:1 G3 1/4 v104\n"

	out, err := writeClipNotation(stub, ClipWriteInput{Notation: text, Rev: current.Rev})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	if !out.Verified {
		t.Errorf("expected the round trip to verify, mismatches = %+v", out.Mismatches)
	}
	if out.NotesWritten != 2 {
		t.Errorf("NotesWritten = %d, want 2", out.NotesWritten)
	}
	if out.Rev == "" {
		t.Error("a fresh rev must come back")
	}
	assertSentInOrder(t, stub.sends,
		"/live/clip/remove/notes",
		"/live/clip/add/notes",
		"/live/clip/set/name",
	)
}

func TestWriteClipNotationReportsRoundTripMismatch(t *testing.T) {
	stub := newReadStub()
	current, err := readClipNotation(stub, ClipReadInput{})
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	// Live comes back with a different velocity than was asked for.
	stub.notesAfterWrite = []interface{}{60, 0.0, 0.5, 64, false}
	text := "clip \"Chorus Lead\" bars=4 sig=4/4\n\n  1:1 C3 1/8 v100\n"

	out, err := writeClipNotation(stub, ClipWriteInput{Notation: text, Rev: current.Rev})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	if out.Verified {
		t.Error("expected the verification to fail")
	}
	if len(out.Mismatches) == 0 {
		t.Error("expected the mismatch to be reported")
	}
}

func TestWriteClipNotationCreatesWhenRevIsEmpty(t *testing.T) {
	stub := newReadStub()
	stub.hasClip = false
	stub.notes = nil
	stub.notesAfterWrite = []interface{}{60, 0.0, 0.5, 100, false}
	text := "clip \"New\" bars=4 sig=4/4\n\n  1:1 C3 1/8 v100\n"

	out, err := writeClipNotation(stub, ClipWriteInput{Notation: text, Rev: ""})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	if out.NotesWritten != 1 {
		t.Errorf("NotesWritten = %d, want 1", out.NotesWritten)
	}
	if got := stub.sendArgs["/live/clip_slot/create_clip"]; len(got) != 3 {
		t.Fatalf("create_clip args = %v", got)
	}
	// bars=4 in 4/4 is 16 beats.
	if got := stub.sendArgs["/live/clip_slot/create_clip"][2]; got != float32(16) {
		t.Errorf("clip length = %v, want 16 beats", got)
	}
}

func TestWriteClipNotationRefusesToRecreateExistingClip(t *testing.T) {
	stub := newReadStub()
	_, err := writeClipNotation(stub, ClipWriteInput{Notation: validNotation(), Rev: ""})
	assertActionable(t, err, "clip_exists")
	if len(stub.sends) != 0 {
		t.Errorf("nothing should have been sent, got %v", stub.sends)
	}
}

func assertSentInOrder(t *testing.T, sends []string, want ...string) {
	t.Helper()
	i := 0
	for _, s := range sends {
		if i < len(want) && s == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Errorf("sends = %v, want them to include %v in order", sends, want)
	}
}
