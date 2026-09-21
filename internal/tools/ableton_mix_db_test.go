package tools

import "testing"

func TestCaptureMixSnapshotShowsLevelsInDB(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85, 0.6}}
	got, err := captureMixSnapshot(live, nil)
	if err != nil {
		t.Fatalf("captureMixSnapshot() error = %v", err)
	}
	if len(got.Tracks) != 2 || got.Tracks[0].VolumeDB != "0.0 dB" || got.Tracks[1].VolumeDB != "-10.0 dB" {
		t.Errorf("tracks = %+v", got.Tracks)
	}

	live.noPatch = true
	got, err = captureMixSnapshot(live, nil)
	if err != nil || len(got.Tracks) != 2 || got.Tracks[0].VolumeDB != "" {
		t.Errorf("without the patch: tracks = %+v, err = %v; want raw volumes and no dB", got.Tracks, err)
	}
}
