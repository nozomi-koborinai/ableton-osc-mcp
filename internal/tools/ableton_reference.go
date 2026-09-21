package tools

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/reference"
)

type referenceStore interface {
	Save(p reference.Profile) (reference.Profile, error)
	List() ([]reference.Profile, error)
	Blend(weights []reference.Weight) (audioanalyze.MixProfile, []reference.Weight, error)
	Path() string
}

type ReferenceProfileSummary struct {
	Name        string                   `json:"name"`
	SourceKind  string                   `json:"source_kind"`
	Source      string                   `json:"source"`
	SavedAt     string                   `json:"saved_at"`
	RangeSec    [2]float64               `json:"range_sec"`
	DurationSec float64                  `json:"duration_sec"`
	BPM         float64                  `json:"bpm"`
	Key         string                   `json:"key,omitempty"`
	Scale       string                   `json:"scale,omitempty"`
	LUFS        float64                  `json:"lufs_integrated"`
	CrestDB     float64                  `json:"crest_db"`
	Bands       []audioanalyze.BandLevel `json:"bands"`
}

type ListReferenceProfilesOutput struct {
	StorePath string                    `json:"store_path"`
	Profiles  []ReferenceProfileSummary `json:"profiles"`
}

func NewAbletonListReferenceProfiles(g *genkit.Genkit, store referenceStore) ai.Tool {
	return genkit.DefineTool(g, "ableton_list_reference_profiles",
		"Saved reference mix profiles: name, source, LUFS, crest, and 9-band spectrum, as kept by save_reference_as. Read-only. Not needed when the person has already named the references to compare against.",
		func(_ *ai.ToolContext, _ EmptyInput) (ListReferenceProfilesOutput, error) {
			return listReferenceProfiles(store)
		},
	)
}

func listReferenceProfiles(store referenceStore) (ListReferenceProfilesOutput, error) {
	if store == nil {
		return ListReferenceProfilesOutput{}, errReferenceStoreUnavailable()
	}
	profiles, err := store.List()
	if err != nil {
		return ListReferenceProfilesOutput{}, err
	}
	out := ListReferenceProfilesOutput{StorePath: store.Path(), Profiles: make([]ReferenceProfileSummary, 0, len(profiles))}
	for _, p := range profiles {
		out.Profiles = append(out.Profiles, ReferenceProfileSummary{
			Name:        p.Name,
			SourceKind:  p.SourceKind,
			Source:      p.Source,
			SavedAt:     p.SavedAt.UTC().Format(time.RFC3339),
			RangeSec:    p.RangeSec,
			DurationSec: p.DurationSec,
			BPM:         p.BPM,
			Key:         p.Key,
			Scale:       p.Scale,
			LUFS:        p.Mix.LUFSIntegrated,
			CrestDB:     p.Mix.CrestDB,
			Bands:       p.Mix.Bands,
		})
	}
	return out, nil
}

func errReferenceStoreUnavailable() error {
	return actionable("reference_store_unavailable",
		"reference profiles are not configured in this server",
		"Restart the server; the store path comes from ABLETON_OSC_REFERENCE_PROFILES_PATH or the OS config directory.")
}

// checkReferenceName validates a save_reference_as value before any audio is
// decoded, so a typo does not cost a whole analysis.
func checkReferenceName(store referenceStore, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	if store == nil {
		return "", errReferenceStoreUnavailable()
	}
	name, err := reference.NormalizeName(raw)
	if err != nil {
		return "", actionable("invalid_reference_name", err.Error(), "Use 1-40 characters from a-z, 0-9, '-' and '_'.")
	}
	return name, nil
}

// blendReferences resolves the references to compare against. It returns nil
// when none were requested.
func blendReferences(store referenceStore, weights []reference.Weight) (*audioanalyze.MixProfile, []reference.Weight, error) {
	if len(weights) == 0 {
		return nil, nil, nil
	}
	if store == nil {
		return nil, nil, errReferenceStoreUnavailable()
	}
	mix, normalized, err := store.Blend(weights)
	if err != nil {
		var unknown *reference.UnknownProfileError
		if errors.As(err, &unknown) {
			next := "No references are saved yet; analyze a track with save_reference_as first."
			if len(unknown.Known) > 0 {
				next = fmt.Sprintf("Pick from the saved references: %s. Or save a new one with save_reference_as.", strings.Join(unknown.Known, ", "))
			}
			return nil, nil, actionable("unknown_reference", unknown.Error(), next)
		}
		return nil, nil, err
	}
	return &mix, normalized, nil
}

// saveReference keeps the numbers of one analysis under name.
func saveReference(store referenceStore, name, kind, source string, got audioanalyze.Result) error {
	if got.MixProfile == nil {
		return errors.New("analysis produced no mix_profile to save")
	}
	rangeSec := [2]float64{0, got.DurationSec}
	if got.RangeEndSec > 0 {
		rangeSec = [2]float64{got.RangeStartSec, got.RangeEndSec}
	}
	_, err := store.Save(reference.Profile{
		Name:        name,
		SourceKind:  kind,
		Source:      source,
		RangeSec:    rangeSec,
		DurationSec: got.DurationSec,
		BPM:         got.EstimatedBPM,
		Key:         got.Key,
		Scale:       got.Scale,
		Mix:         *got.MixProfile,
	})
	return err
}

// analysisOptions turns the optional tool inputs into audioanalyze.Options.
func analysisOptions(projectTempo, startSec, endSec *float64) audioanalyze.Options {
	var opts audioanalyze.Options
	if projectTempo != nil {
		opts.ProjectTempo = *projectTempo
	}
	if startSec != nil {
		opts.StartSec = *startSec
	}
	if endSec != nil {
		opts.EndSec = *endSec
	}
	return opts
}

// wrapUnsupportedFormat gives an undecodable file a next step.
func wrapUnsupportedFormat(err error) error {
	if errors.Is(err, audioanalyze.ErrUnsupportedFormat) {
		return actionable("unsupported_audio_format", err.Error(), "Provide WAV, or uncompressed AIFF (PCM or 32-bit float).")
	}
	return err
}
