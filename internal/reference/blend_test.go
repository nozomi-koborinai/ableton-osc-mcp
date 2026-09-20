package reference

import (
	"errors"
	"math"
	"testing"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

func twoBandMix(lufs, crest, lowDB, highDB, corr float64) audioanalyze.MixProfile {
	return audioanalyze.MixProfile{
		LUFSIntegrated: lufs,
		CrestDB:        crest,
		Bands: []audioanalyze.BandLevel{
			{Label: "<60", LoHz: 20, HiHz: 60, DB: lowDB},
			{Label: "60-120", LoHz: 60, HiHz: 120, DB: highDB},
		},
		Width: []audioanalyze.BandWidth{{Label: "1-4k", LoHz: 1000, HiHz: 4000, LRCorrelation: corr, SideMinusMidDB: -6}},
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestBlendAveragesInDBByNormalizedWeight(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	_, _ = store.Save(Profile{Name: "envy", Mix: twoBandMix(-10, 9, -10, -20, 0.8)})
	_, _ = store.Save(Profile{Name: "crayon", Mix: twoBandMix(-14, 11, -20, -10, 0.0)})

	// 3:1 normalizes to 0.75 / 0.25.
	mix, weights, err := store.Blend([]Weight{{Name: "ENVY", Weight: 3}, {Name: "crayon", Weight: 1}})
	if err != nil {
		t.Fatalf("Blend() error = %v", err)
	}
	if !near(weights[0].Weight, 0.75) || !near(weights[1].Weight, 0.25) || weights[0].Name != "envy" {
		t.Errorf("weights = %+v, want envy 0.75 / crayon 0.25", weights)
	}
	if !near(mix.Bands[0].DB, -12.5) || !near(mix.Bands[1].DB, -17.5) {
		t.Errorf("bands = %+v, want -12.5 and -17.5", mix.Bands)
	}
	if !near(mix.LUFSIntegrated, -11) || !near(mix.CrestDB, 9.5) {
		t.Errorf("lufs/crest = %v/%v, want -11 and 9.5", mix.LUFSIntegrated, mix.CrestDB)
	}
	if !near(mix.Width[0].LRCorrelation, 0.6) {
		t.Errorf("correlation = %v, want 0.6", mix.Width[0].LRCorrelation)
	}
}

func TestBlendTreatsMissingWeightAsOne(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	_, _ = store.Save(Profile{Name: "a", Mix: twoBandMix(-10, 9, -10, -20, 1)})
	_, _ = store.Save(Profile{Name: "b", Mix: twoBandMix(-20, 9, -30, -40, 1)})

	mix, weights, err := store.Blend([]Weight{{Name: "a"}, {Name: "b"}})
	if err != nil {
		t.Fatalf("Blend() error = %v", err)
	}
	if !near(weights[0].Weight, 0.5) || !near(mix.LUFSIntegrated, -15) {
		t.Errorf("weights = %+v, lufs = %v; want 0.5 each and -15", weights, mix.LUFSIntegrated)
	}
}

func TestBlendNamesTheUnknownProfileAndTheKnownOnes(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	_, _ = store.Save(Profile{Name: "envy", Mix: twoBandMix(-10, 9, -10, -20, 1)})

	_, _, err := store.Blend([]Weight{{Name: "envy"}, {Name: "rainy"}})
	var unknown *UnknownProfileError
	if !errors.As(err, &unknown) {
		t.Fatalf("error = %v, want *UnknownProfileError", err)
	}
	if unknown.Name != "rainy" || len(unknown.Known) != 1 || unknown.Known[0] != "envy" {
		t.Errorf("unknown = %+v, want name rainy and known [envy]", unknown)
	}
}

func TestBlendRejectsAnEmptyRequest(t *testing.T) {
	t.Parallel()

	if _, _, err := newTestStore(t).Blend(nil); err == nil {
		t.Error("Blend(nil) should fail")
	}
}

func TestCompareReportsDeltasAndFlagsBandsBeyondThreeDB(t *testing.T) {
	t.Parallel()

	mine := twoBandMix(-10.9, 6.5, -6.5, -17, 0.43)
	ref := twoBandMix(-9.4, 9.6, -13, -14, 0.03)

	got := Compare(mine, ref, []Weight{{Name: "blend", Weight: 1}})

	if len(got.BandDeltasDB) != 2 {
		t.Fatalf("band deltas = %+v, want 2", got.BandDeltasDB)
	}
	// mine - reference
	if !near(got.BandDeltasDB[0].DeltaDB, 6.5) || !near(got.BandDeltasDB[1].DeltaDB, -3) {
		t.Errorf("deltas = %+v, want +6.5 and -3", got.BandDeltasDB)
	}
	// Exactly 3 dB is still in range; only the +6.5 band is flagged.
	if len(got.OutOfRange) != 1 || got.OutOfRange[0] != "<60" {
		t.Errorf("out_of_range = %v, want [<60]", got.OutOfRange)
	}
	if !near(got.LUFSDelta, -1.5) || !near(got.CrestDelta, -3.1) {
		t.Errorf("lufs/crest delta = %v/%v, want -1.5 and -3.1", got.LUFSDelta, got.CrestDelta)
	}
	if len(got.WidthDeltas) != 1 || !near(got.WidthDeltas[0].Delta, 0.4) {
		t.Errorf("width deltas = %+v, want +0.4", got.WidthDeltas)
	}
	if got.Caveat == "" || len(got.Blend) != 1 {
		t.Errorf("caveat/blend missing: %+v", got)
	}
}

func TestCompareKeepsOutOfRangeAsAnEmptyList(t *testing.T) {
	t.Parallel()

	mix := twoBandMix(-10, 9, -10, -20, 0.5)
	got := Compare(mix, mix, nil)
	if got.OutOfRange == nil || len(got.OutOfRange) != 0 {
		t.Errorf("out_of_range = %#v, want an empty non-nil list (JSON [] rather than null)", got.OutOfRange)
	}
}

func TestCompareDoesNotReportNegativeZero(t *testing.T) {
	t.Parallel()

	// -0.004 rounds to zero; JSON would otherwise print it as "-0".
	mine := twoBandMix(-10.004, 9, -10.004, -20, 0.5)
	ref := twoBandMix(-10, 9, -10, -20, 0.5)

	got := Compare(mine, ref, nil)
	if got.BandDeltasDB[0].DeltaDB != 0 || math.Signbit(got.BandDeltasDB[0].DeltaDB) {
		t.Errorf("band delta = %v (signbit %v), want plain 0", got.BandDeltasDB[0].DeltaDB, math.Signbit(got.BandDeltasDB[0].DeltaDB))
	}
	if math.Signbit(got.LUFSDelta) {
		t.Errorf("lufs delta = %v, want plain 0", got.LUFSDelta)
	}
}
