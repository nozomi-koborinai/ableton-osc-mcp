package tools

import (
	"errors"
	"testing"
)

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

func TestApplyMixVariationInDB(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85, 0.6}}
	got, err := applyMixVariation(live, ApplyMixVariationInput{Changes: []MixVolumeChange{{TrackIndex: 1, DeltaDB: -2}}})
	if err != nil {
		t.Fatalf("applyMixVariation() error = %v", err)
	}
	if got.Before.Tracks[0].VolumeDB != "-10.0 dB" || got.After.Tracks[0].VolumeDB != "-12.0 dB" {
		t.Errorf("before/after = %+v / %+v, want -10.0 dB then -12.0 dB", got.Before.Tracks, got.After.Tracks)
	}

	// The A snapshot restores by raw value, so A comes back exactly.
	if _, err := restoreMixSnapshot(live, got.Before.Tracks); err != nil {
		t.Fatalf("restoreMixSnapshot() error = %v", err)
	}
	if live.volumes[1] != got.Before.Tracks[0].Volume {
		t.Errorf("restored volume = %v, want %v", live.volumes[1], got.Before.Tracks[0].Volume)
	}
}

func TestApplyMixVariationWantsOneKindOfDeltaPerTrack(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85}}
	for name, change := range map[string]MixVolumeChange{
		"neither":      {TrackIndex: 0},
		"both":         {TrackIndex: 0, Delta: 0.05, DeltaDB: -1},
		"dB too large": {TrackIndex: 0, DeltaDB: -13},
	} {
		if _, err := applyMixVariation(live, ApplyMixVariationInput{Changes: []MixVolumeChange{change}}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if len(live.sent) != 0 {
		t.Errorf("sent = %v, want nothing changed", live.sent)
	}
}

func TestApplyMixVariationInDBNeedsThePatch(t *testing.T) {
	t.Parallel()

	live := &fakeMixer{volumes: []float64{0.85}, noPatch: true}
	_, err := applyMixVariation(live, ApplyMixVariationInput{Changes: []MixVolumeChange{{TrackIndex: 0, DeltaDB: -2}}})
	if !isMixerDBPatchMissing(err) {
		t.Errorf("error = %v, want mixer_db_patch_missing", err)
	}
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) {
		t.Errorf("error should be actionable: %v", err)
	}
}
