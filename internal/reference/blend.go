package reference

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

// Caveat travels with every comparison, because nothing here can tell whether
// a reference contains vocals.
const Caveat = "References that contain vocals read high between 1 and 8 kHz, so a mix without vocals looks short there. Judge those bands against an instrumental reference, or by ear."

// outOfRangeDB is how far a band may sit from the reference before it is listed.
const outOfRangeDB = 3.0

// Weight names one saved profile and how much it counts in a blend.
type Weight struct {
	Name   string  `json:"name" jsonschema:"description=Saved reference profile name"`
	Weight float64 `json:"weight,omitempty" jsonschema:"description=Relative weight; omitted or 0 means 1. Weights are normalized to sum to 1,minimum=0"`
}

// UnknownProfileError reports a blend that names a profile that was never saved.
type UnknownProfileError struct {
	Name  string
	Known []string
}

func (e *UnknownProfileError) Error() string {
	if len(e.Known) == 0 {
		return fmt.Sprintf("no saved reference named %q (none are saved yet)", e.Name)
	}
	return fmt.Sprintf("no saved reference named %q (saved: %s)", e.Name, strings.Join(e.Known, ", "))
}

// Blend averages the named profiles in dB, weighted. It returns the blended
// profile and the weights after normalization.
func (s *Store) Blend(weights []Weight) (audioanalyze.MixProfile, []Weight, error) {
	if len(weights) == 0 {
		return audioanalyze.MixProfile{}, nil, errors.New("at least one reference is required")
	}
	profiles, err := s.List()
	if err != nil {
		return audioanalyze.MixProfile{}, nil, err
	}
	byName := make(map[string]Profile, len(profiles))
	known := make([]string, 0, len(profiles))
	for _, p := range profiles {
		byName[p.Name] = p
		known = append(known, p.Name)
	}

	normalized := make([]Weight, 0, len(weights))
	picked := make([]audioanalyze.MixProfile, 0, len(weights))
	var total float64
	for _, w := range weights {
		name, err := NormalizeName(w.Name)
		if err != nil {
			return audioanalyze.MixProfile{}, nil, err
		}
		p, ok := byName[name]
		if !ok {
			return audioanalyze.MixProfile{}, nil, &UnknownProfileError{Name: name, Known: known}
		}
		if w.Weight < 0 {
			return audioanalyze.MixProfile{}, nil, fmt.Errorf("weight for %q must not be negative", name)
		}
		weight := w.Weight
		if weight == 0 {
			weight = 1
		}
		normalized = append(normalized, Weight{Name: name, Weight: weight})
		picked = append(picked, p.Mix)
		total += weight
	}
	for i := range normalized {
		normalized[i].Weight /= total
	}

	first := picked[0]
	out := audioanalyze.MixProfile{
		Bands: make([]audioanalyze.BandLevel, len(first.Bands)),
		Width: make([]audioanalyze.BandWidth, len(first.Width)),
	}
	copy(out.Bands, first.Bands)
	copy(out.Width, first.Width)
	for i := range out.Bands {
		out.Bands[i].DB = 0
	}
	for i := range out.Width {
		out.Width[i].LRCorrelation, out.Width[i].SideMinusMidDB = 0, 0
	}
	for j, mix := range picked {
		if len(mix.Bands) != len(first.Bands) || len(mix.Width) != len(first.Width) {
			return audioanalyze.MixProfile{}, nil, fmt.Errorf("reference %q was saved with a different band layout; save it again", normalized[j].Name)
		}
		w := normalized[j].Weight
		out.LUFSIntegrated += w * mix.LUFSIntegrated
		out.TruePeakDBTP += w * mix.TruePeakDBTP
		out.SamplePeakDBFS += w * mix.SamplePeakDBFS
		out.CrestDB += w * mix.CrestDB
		for i := range mix.Bands {
			out.Bands[i].DB += w * mix.Bands[i].DB
		}
		for i := range mix.Width {
			out.Width[i].LRCorrelation += w * mix.Width[i].LRCorrelation
			out.Width[i].SideMinusMidDB += w * mix.Width[i].SideMinusMidDB
		}
	}
	return out, normalized, nil
}

// BandDelta is one band of mine against the reference.
type BandDelta struct {
	Label       string  `json:"label"`
	MineDB      float64 `json:"mine_db"`
	ReferenceDB float64 `json:"reference_db"`
	DeltaDB     float64 `json:"delta_db" jsonschema:"description=mine minus reference; positive means this band is stronger than the reference"`
}

// WidthDelta compares left/right correlation in one band.
type WidthDelta struct {
	Label     string  `json:"label"`
	Mine      float64 `json:"mine"`
	Reference float64 `json:"reference"`
	Delta     float64 `json:"delta" jsonschema:"description=mine minus reference correlation; positive means narrower than the reference"`
}

// Comparison is mine measured against a (blended) reference. It reports
// differences and stops there: what to do about them is not this package's call.
type Comparison struct {
	Blend        []Weight     `json:"blend"`
	BandDeltasDB []BandDelta  `json:"band_deltas_db"`
	OutOfRange   []string     `json:"out_of_range" jsonschema:"description=Bands more than 3 dB away from the reference"`
	LUFSDelta    float64      `json:"lufs_delta"`
	CrestDelta   float64      `json:"crest_delta_db"`
	WidthDeltas  []WidthDelta `json:"width_deltas"`
	Caveat       string       `json:"caveat"`
}

// Compare matches bands by label, so a reference saved with fewer bands still
// compares on the ones it has.
func Compare(mine, ref audioanalyze.MixProfile, blend []Weight) Comparison {
	out := Comparison{
		Blend:        blend,
		BandDeltasDB: []BandDelta{},
		OutOfRange:   []string{},
		LUFSDelta:    round2(mine.LUFSIntegrated - ref.LUFSIntegrated),
		CrestDelta:   round2(mine.CrestDB - ref.CrestDB),
		WidthDeltas:  []WidthDelta{},
		Caveat:       Caveat,
	}
	refBands := make(map[string]float64, len(ref.Bands))
	for _, b := range ref.Bands {
		refBands[b.Label] = b.DB
	}
	for _, b := range mine.Bands {
		refDB, ok := refBands[b.Label]
		if !ok {
			continue
		}
		delta := round2(b.DB - refDB)
		out.BandDeltasDB = append(out.BandDeltasDB, BandDelta{Label: b.Label, MineDB: b.DB, ReferenceDB: round2(refDB), DeltaDB: delta})
		if math.Abs(delta) > outOfRangeDB {
			out.OutOfRange = append(out.OutOfRange, b.Label)
		}
	}
	refWidth := make(map[string]float64, len(ref.Width))
	for _, w := range ref.Width {
		refWidth[w.Label] = w.LRCorrelation
	}
	for _, w := range mine.Width {
		refCorr, ok := refWidth[w.Label]
		if !ok {
			continue
		}
		out.WidthDeltas = append(out.WidthDeltas, WidthDelta{
			Label: w.Label, Mine: w.LRCorrelation, Reference: round2(refCorr), Delta: round2(w.LRCorrelation - refCorr),
		})
	}
	return out
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
