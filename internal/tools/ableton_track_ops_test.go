package tools

import (
	"strings"
	"testing"
	"time"
)

// fakeTrackClient simulates the OSC calls used by duplicateTrackForProcessing.
type fakeTrackClient struct {
	trackName          string
	tracks             int
	duplicateAddsTrack bool
	sentNames          map[int]string
	duplicatedFrom     int
}

func newFakeTrackClient(name string, tracks int) *fakeTrackClient {
	return &fakeTrackClient{
		trackName:          name,
		tracks:             tracks,
		duplicateAddsTrack: true,
		sentNames:          map[int]string{},
		duplicatedFrom:     -1,
	}
}

func (f *fakeTrackClient) Send(address string, args ...interface{}) error {
	switch address {
	case "/live/song/duplicate_track":
		f.duplicatedFrom = int(args[0].(int32))
		if f.duplicateAddsTrack {
			f.tracks++
		}
	case "/live/track/set/name":
		f.sentNames[int(args[0].(int32))] = args[1].(string)
	}
	return nil
}

func (f *fakeTrackClient) Query(address string, args ...interface{}) ([]interface{}, error) {
	switch address {
	case "/live/track/get/name":
		return []interface{}{args[0], f.trackName}, nil
	case "/live/song/get/num_tracks":
		return []interface{}{int32(f.tracks)}, nil
	}
	return nil, nil
}

func (f *fakeTrackClient) QueryWithTimeout(_ time.Duration, address string, args ...interface{}) ([]interface{}, error) {
	return f.Query(address, args...)
}

func TestDuplicateTrackDefaults(t *testing.T) {
	c := newFakeTrackClient("Piano", 7)
	out, err := duplicateTrackForProcessing(c, DuplicateTrackForProcessingInput{TrackIndex: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.DryIndex != 3 || out.WetIndex != 4 {
		t.Errorf("indices = dry %d / wet %d, want 3/4", out.DryIndex, out.WetIndex)
	}
	if out.WetName != "Piano wet" {
		t.Errorf("wet name = %q, want 'Piano wet'", out.WetName)
	}
	if out.DryName != "Piano" {
		t.Errorf("dry name = %q, want unchanged 'Piano'", out.DryName)
	}
	if c.sentNames[4] != "Piano wet" {
		t.Errorf("wet track not renamed: %v", c.sentNames)
	}
	if _, renamed := c.sentNames[3]; renamed {
		t.Error("dry track should not be renamed without dry_suffix")
	}
}

func TestDuplicateTrackWithDrySuffixAndCustomWet(t *testing.T) {
	c := newFakeTrackClient("Chop", 5)
	out, err := duplicateTrackForProcessing(c, DuplicateTrackForProcessingInput{
		TrackIndex: 1,
		WetName:    "Chop FX",
		DrySuffix:  " dry",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.WetName != "Chop FX" || c.sentNames[2] != "Chop FX" {
		t.Errorf("custom wet name not applied: out=%q sent=%v", out.WetName, c.sentNames)
	}
	if out.DryName != "Chop dry" || c.sentNames[1] != "Chop dry" {
		t.Errorf("dry suffix not applied: out=%q sent=%v", out.DryName, c.sentNames)
	}
}

func TestDuplicateTrackNoAddFails(t *testing.T) {
	c := newFakeTrackClient("Piano", 7)
	c.duplicateAddsTrack = false
	_, err := duplicateTrackForProcessing(c, DuplicateTrackForProcessingInput{TrackIndex: 3})
	if err == nil {
		t.Fatal("expected error when duplicate does not add a track")
	}
	if !strings.Contains(err.Error(), "did not add") {
		t.Errorf("error should explain the failure: %v", err)
	}
}

func TestDuplicateTrackOutOfRange(t *testing.T) {
	c := newFakeTrackClient("Piano", 4)
	if _, err := duplicateTrackForProcessing(c, DuplicateTrackForProcessingInput{TrackIndex: 9}); err == nil {
		t.Fatal("expected out-of-range error")
	}
}
