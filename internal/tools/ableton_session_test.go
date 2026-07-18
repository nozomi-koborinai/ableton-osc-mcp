package tools

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type snapshotQueryResult struct {
	values []interface{}
	err    error
}

type snapshotQuerierStub struct {
	results map[string]snapshotQueryResult
}

func (s snapshotQuerierStub) Query(address string, _ ...interface{}) ([]interface{}, error) {
	result, ok := s.results[address]
	if !ok {
		return nil, errors.New("unexpected query")
	}
	return result.values, result.err
}

func TestGetSessionSnapshot(t *testing.T) {
	t.Parallel()

	client := snapshotQuerierStub{results: map[string]snapshotQueryResult{
		"/live/song/get/tempo":      {values: []interface{}{float32(128)}},
		"/live/song/get/is_playing": {values: []interface{}{int32(1)}},
		"/live/song/get/num_scenes": {values: []interface{}{int32(8)}},
		"/live/song/get/track_names": {
			values: []interface{}{"Drums", "Bass"},
		},
	}}

	got, err := getSessionSnapshot(client)
	if err != nil {
		t.Fatalf("getSessionSnapshot() error = %v", err)
	}

	want := SessionSnapshotOutput{
		TempoBPM:  128,
		IsPlaying: true,
		NumScenes: 8,
		Tracks: []SessionTrack{
			{Index: 0, Name: "Drums"},
			{Index: 1, Name: "Bass"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("getSessionSnapshot() = %#v, want %#v", got, want)
	}
}

func TestGetSessionSnapshotReturnsContextualError(t *testing.T) {
	t.Parallel()

	client := snapshotQuerierStub{results: map[string]snapshotQueryResult{
		"/live/song/get/tempo": {err: errors.New("timeout")},
	}}

	_, err := getSessionSnapshot(client)
	if err == nil {
		t.Fatal("getSessionSnapshot() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "get tempo") {
		t.Errorf("getSessionSnapshot() error = %q, want tempo context", err)
	}
}

func TestGetSessionSnapshotReturnsEmptyTrackList(t *testing.T) {
	t.Parallel()

	client := snapshotQuerierStub{results: map[string]snapshotQueryResult{
		"/live/song/get/tempo":       {values: []interface{}{float64(120)}},
		"/live/song/get/is_playing":  {values: []interface{}{false}},
		"/live/song/get/num_scenes":  {values: []interface{}{0}},
		"/live/song/get/track_names": {values: []interface{}{}},
	}}

	got, err := getSessionSnapshot(client)
	if err != nil {
		t.Fatalf("getSessionSnapshot() error = %v", err)
	}
	if got.Tracks == nil {
		t.Fatal("getSessionSnapshot() Tracks = nil, want empty list")
	}
	if len(got.Tracks) != 0 {
		t.Errorf("getSessionSnapshot() Tracks = %v, want empty list", got.Tracks)
	}
}
