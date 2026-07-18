package tools

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type spliceStub struct {
	hasClip  bool
	calls    []string
	reply    []interface{}
	queryErr error
}

func (s *spliceStub) Query(address string, args ...interface{}) ([]interface{}, error) {
	s.calls = append(s.calls, "Query:"+address)
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	if address == "/live/clip_slot/get/has_clip" {
		return []interface{}{int32(0), int32(0), s.hasClip}, nil
	}
	if address == "/live/clip_slot/create_audio_clip" {
		if s.reply != nil {
			return s.reply, nil
		}
		return []interface{}{int32(0), int32(0), "created", args[2]}, nil
	}
	return nil, errors.New("unexpected query: " + address)
}

func (s *spliceStub) Send(address string, _ ...interface{}) error {
	s.calls = append(s.calls, "Send:"+address)
	return nil
}

func TestGetSpliceLibraryMissing(t *testing.T) {
	t.Parallel()

	got := getSpliceLibrary(filepath.Join(t.TempDir(), "nope"))
	if got.Exists || got.Source != "env" {
		t.Errorf("got = %#v", got)
	}
}

func TestSearchSpliceSamples(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "kick.wav")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	max := 5
	got, err := searchSpliceSamples(root, SearchSpliceSamplesInput{Query: "kick", MaxResults: &max})
	if err != nil {
		t.Fatalf("searchSpliceSamples() error = %v", err)
	}
	if len(got.Samples) != 1 || got.Samples[0].Name != "kick.wav" {
		t.Errorf("got = %#v", got)
	}
}

func TestLoadSpliceSample(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	abs := filepath.Join(root, "snare.wav")
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &spliceStub{}
	got, err := loadSpliceSample(client, root, LoadSpliceSampleInput{
		AbsolutePath: abs,
		TrackIndex:   0,
		ClipIndex:    0,
		Fire:         true,
	})
	if err != nil {
		t.Fatalf("loadSpliceSample() error = %v", err)
	}
	if !got.Fired || got.AbsolutePath != abs {
		t.Errorf("got = %#v", got)
	}
}

func TestLoadSpliceSampleRejectsOccupiedSlot(t *testing.T) {
	t.Parallel()

	client := &spliceStub{hasClip: true}
	_, err := loadSpliceSample(client, t.TempDir(), LoadSpliceSampleInput{
		AbsolutePath: "/tmp/x.wav",
		TrackIndex:   0,
		ClipIndex:    1,
	})
	if err == nil {
		t.Fatal("expected occupied slot error")
	}
}

func TestLoadSpliceSampleUnsupportedLive(t *testing.T) {
	t.Parallel()

	client := &spliceStub{
		reply: []interface{}{int32(0), int32(0), "unsupported", "create_audio_clip requires Ableton Live 12.0.5+"},
	}
	_, err := loadSpliceSample(client, t.TempDir(), LoadSpliceSampleInput{
		AbsolutePath: "/tmp/x.wav",
		TrackIndex:   0,
		ClipIndex:    0,
	})
	if err == nil || err.Error() == "" {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveSpliceSampleRelativePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	got, err := resolveSpliceSamplePath(root, "", "sounds/kick.wav")
	if err != nil {
		t.Fatalf("resolveSpliceSamplePath() error = %v", err)
	}
	want := filepath.Join(root, "sounds", "kick.wav")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	_, err = resolveSpliceSamplePath(root, "", "../escape.wav")
	if err == nil {
		t.Fatal("expected path escape error")
	}
}

func TestParseCreateAudioClipResponse(t *testing.T) {
	t.Parallel()

	if err := parseCreateAudioClipResponse([]interface{}{int32(1), int32(2), "created", "/tmp/a.wav"}, 1, 2); err != nil {
		t.Fatalf("created reply error = %v", err)
	}
	if err := parseCreateAudioClipResponse([]interface{}{int32(1), int32(2), "not_audio_track"}, 1, 2); err == nil {
		t.Fatal("expected not_audio_track error")
	}
}
