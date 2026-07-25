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
