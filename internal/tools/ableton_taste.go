package tools

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/taste"
)

type RecordVariationPreferenceInput struct {
	Instrument string `json:"instrument" jsonschema:"description=Instrument family: drum or bass"`
	Variation  string `json:"variation" jsonschema:"description=Variation that was compared (e.g. groove, density, octave_up)"`
	Preferred  string `json:"preferred" jsonschema:"description=Which version you preferred: source or variation"`
	Note       string `json:"note,omitempty" jsonschema:"description=Optional short reason for the choice (max 500 characters)"`
}

type TastePreferenceOutput struct {
	Instrument string `json:"instrument"`
	Variation  string `json:"variation"`
	Preferred  string `json:"preferred"`
	Note       string `json:"note,omitempty"`
}

type TasteSummary struct {
	Instrument string `json:"instrument"`
	Variation  string `json:"variation"`
	Accepted   int    `json:"accepted"`
	Rejected   int    `json:"rejected"`
}

type TasteProfileOutput struct {
	ProfilePath         string                 `json:"profile_path"`
	PreferencesRecorded int                    `json:"preferences_recorded"`
	Summaries           []TasteSummary         `json:"summaries"`
	NextSuggestions     []string               `json:"next_suggestions"`
	RecordedPreference  *TastePreferenceOutput `json:"recorded_preference,omitempty"`
}

type tasteStore interface {
	Record(preference taste.Preference) (taste.Profile, error)
	Load() (taste.Profile, error)
	Path() string
}

func NewAbletonRecordVariationPreference(g *genkit.Genkit, store tasteStore) ai.Tool {
	return genkit.DefineTool(g, "ableton_record_variation_preference",
		"Ableton Live: record whether an A/B drum or bass variation matched your taste",
		func(_ *ai.ToolContext, input RecordVariationPreferenceInput) (TasteProfileOutput, error) {
			preference, err := validateTastePreference(input)
			if err != nil {
				return TasteProfileOutput{}, err
			}
			profile, err := store.Record(preference)
			if err != nil {
				return TasteProfileOutput{}, err
			}
			out := tasteProfileOutput(profile, store.Path())
			out.RecordedPreference = &TastePreferenceOutput{
				Instrument: preference.Instrument,
				Variation:  preference.Variation,
				Preferred:  preference.Preferred,
				Note:       preference.Note,
			}
			return out, nil
		},
	)
}

func NewAbletonGetTasteProfile(g *genkit.Genkit, store tasteStore) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_taste_profile",
		"Ableton Live: summarize saved A/B variation preferences and suggest the next comparison",
		func(_ *ai.ToolContext, _ EmptyInput) (TasteProfileOutput, error) {
			profile, err := store.Load()
			if err != nil {
				return TasteProfileOutput{}, err
			}
			return tasteProfileOutput(profile, store.Path()), nil
		},
	)
}

func validateTastePreference(input RecordVariationPreferenceInput) (taste.Preference, error) {
	instrument := strings.ToLower(strings.TrimSpace(input.Instrument))
	variation := strings.ToLower(strings.TrimSpace(input.Variation))
	preferred := strings.ToLower(strings.TrimSpace(input.Preferred))
	note := strings.TrimSpace(input.Note)

	switch instrument {
	case "drum":
		if !isDrumVariation(variation) {
			return taste.Preference{}, errors.New("drum variation must be groove, density, or fill")
		}
	case "bass":
		if !isBassVariation(variation) {
			return taste.Preference{}, errors.New("bass variation must be octave_up, octave_down, staccato, or groove")
		}
	default:
		return taste.Preference{}, errors.New("instrument must be drum or bass")
	}
	if preferred != "source" && preferred != "variation" {
		return taste.Preference{}, errors.New("preferred must be source or variation")
	}
	if len(note) > 500 {
		return taste.Preference{}, errors.New("note must be 500 characters or fewer")
	}
	return taste.Preference{
		Instrument: instrument,
		Variation:  variation,
		Preferred:  preferred,
		Note:       note,
	}, nil
}

func tasteProfileOutput(profile taste.Profile, path string) TasteProfileOutput {
	type counter struct {
		instrument string
		variation  string
		accepted   int
		rejected   int
	}

	counts := make(map[string]*counter)
	for _, preference := range profile.Preferences {
		key := preference.Instrument + "\x00" + preference.Variation
		item, ok := counts[key]
		if !ok {
			item = &counter{instrument: preference.Instrument, variation: preference.Variation}
			counts[key] = item
		}
		if preference.Preferred == "variation" {
			item.accepted++
		} else {
			item.rejected++
		}
	}

	summaries := make([]TasteSummary, 0, len(counts))
	for _, item := range counts {
		summaries = append(summaries, TasteSummary{
			Instrument: item.instrument,
			Variation:  item.variation,
			Accepted:   item.accepted,
			Rejected:   item.rejected,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Instrument == summaries[j].Instrument {
			return summaries[i].Variation < summaries[j].Variation
		}
		return summaries[i].Instrument < summaries[j].Instrument
	})

	return TasteProfileOutput{
		ProfilePath:         path,
		PreferencesRecorded: len(profile.Preferences),
		Summaries:           summaries,
		NextSuggestions:     nextTasteSuggestions(summaries),
	}
}

func nextTasteSuggestions(summaries []TasteSummary) []string {
	byInstrument := map[string]map[string]int{
		"drum": {},
		"bass": {},
	}
	for _, summary := range summaries {
		if _, ok := byInstrument[summary.Instrument]; ok {
			byInstrument[summary.Instrument][summary.Variation] = summary.Accepted + summary.Rejected
		}
	}

	suggestions := make([]string, 0, 2)
	for _, instrument := range []string{"drum", "bass"} {
		counts := byInstrument[instrument]
		if len(counts) == 0 {
			continue
		}
		next := leastComparedVariation(instrument, counts)
		suggestions = append(suggestions,
			fmt.Sprintf("Try a %s %s variation next; it has been compared least often.", instrument, next),
		)
	}
	return suggestions
}

func leastComparedVariation(instrument string, counts map[string]int) string {
	var candidates []string
	switch instrument {
	case "drum":
		candidates = []string{"density", "fill", "groove"}
	case "bass":
		candidates = []string{"groove", "octave_down", "octave_up", "staccato"}
	}

	next := candidates[0]
	for _, candidate := range candidates[1:] {
		if counts[candidate] < counts[next] {
			next = candidate
		}
	}
	return next
}
