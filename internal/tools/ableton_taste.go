package tools

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/taste"
)

type RecordVariationPreferenceInput struct {
	Instrument string `json:"instrument" jsonschema:"description=Comparison family: drum\\, bass\\, scene\\, mix\\, or fx"`
	Variation  string `json:"variation" jsonschema:"description=Variation that was compared (e.g. groove\\, lift\\, volume\\, bypass)"`
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
	ProfilePath             string                 `json:"profile_path"`
	PreferencesRecorded     int                    `json:"preferences_recorded"`
	Summaries               []TasteSummary         `json:"summaries"`
	NextSuggestions         []string               `json:"next_suggestions"`
	AuditionChoicesRecorded int                    `json:"audition_choices_recorded"`
	RecentAuditionChoices   []AuditionChoiceOutput `json:"recent_audition_choices,omitempty"`
	RecordedPreference      *TastePreferenceOutput `json:"recorded_preference,omitempty"`
	RecordedAuditionChoice  *AuditionChoiceOutput  `json:"recorded_audition_choice,omitempty"`
}

type AuditionChoiceOption struct {
	Label       string `json:"label" jsonschema:"description=The label the listener heard\\, e.g. X or P"`
	Description string `json:"description" jsonschema:"description=What that variant changed (1-60 characters)"`
}

type RecordAuditionChoiceInput struct {
	Topic   string                 `json:"topic" jsonschema:"description=What the audition was about\\, in the listener's language\\, e.g. how the 808 plays (1-80 characters)"`
	Options []AuditionChoiceOption `json:"options" jsonschema:"description=Every variant the listener heard (2-8)\\, not only the winner: a choice means something next to what it was chosen over"`
	Chosen  string                 `json:"chosen" jsonschema:"description=Label of the variant the listener picked. X when they kept the current state"`
	Note    string                 `json:"note,omitempty" jsonschema:"description=The listener's own words about the choice (max 500 characters)"`
}

// AuditionChoiceOutput is one recorded choice, shaped for reading back.
type AuditionChoiceOutput struct {
	Topic             string   `json:"topic"`
	Chosen            string   `json:"chosen"`
	ChosenDescription string   `json:"chosen_description"`
	Others            []string `json:"others,omitempty"` // "label: description" of what it was chosen over
	Note              string   `json:"note,omitempty"`
	RecordedOn        string   `json:"recorded_on"`
}

const recentAuditionChoices = 10

type tasteStore interface {
	Record(preference taste.Preference) (taste.Profile, error)
	RecordAuditionChoice(choice taste.AuditionChoice) (taste.Profile, error)
	Load() (taste.Profile, error)
	Path() string
}

var tasteInstrumentOrder = []string{"bass", "drum", "fx", "mix", "scene"}

func NewAbletonRecordVariationPreference(g *genkit.Genkit, store tasteStore) ai.Tool {
	return genkit.DefineTool(g, "ableton_record_variation_preference",
		"Ableton Live: record which side won (drum, bass, scene, mix, or fx), after the listener has actually heard an A/B comparison and said which they prefer. Never call this on your own — it writes to the taste profile on disk. Nothing to record until a preference has been stated.",
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

func NewAbletonRecordAuditionChoice(g *genkit.Genkit, store tasteStore) ai.Tool {
	return genkit.DefineTool(g, "ableton_record_audition_choice",
		"Ableton Live: keep what the listener picked in an ableton_audition — the topic, every variant they heard, the label they chose and their own words. Never call this on your own: it writes to the taste profile on disk, and there is nothing to record until the listener has named a choice. Not for ableton_compare_ab_variation (ableton_record_variation_preference keeps those).",
		func(_ *ai.ToolContext, input RecordAuditionChoiceInput) (TasteProfileOutput, error) {
			return recordAuditionChoice(store, input)
		},
	)
}

func recordAuditionChoice(store tasteStore, input RecordAuditionChoiceInput) (TasteProfileOutput, error) {
	choice, err := validateAuditionChoice(input)
	if err != nil {
		return TasteProfileOutput{}, err
	}
	profile, err := store.RecordAuditionChoice(choice)
	if err != nil {
		return TasteProfileOutput{}, err
	}
	out := tasteProfileOutput(profile, store.Path())
	recorded := auditionChoiceOutput(profile.AuditionChoices[len(profile.AuditionChoices)-1])
	out.RecordedAuditionChoice = &recorded
	return out, nil
}

func validateAuditionChoice(input RecordAuditionChoiceInput) (taste.AuditionChoice, error) {
	topic := strings.TrimSpace(input.Topic)
	if n := utf8.RuneCountInString(topic); n < 1 || n > 80 {
		return taste.AuditionChoice{}, errors.New("topic must be 1 to 80 characters")
	}
	if n := len(input.Options); n < minAuditionVariants || n > maxAuditionVariants {
		return taste.AuditionChoice{}, fmt.Errorf("options must list the %d to %d variants the listener heard, got %d", minAuditionVariants, maxAuditionVariants, n)
	}
	note := strings.TrimSpace(input.Note)
	if utf8.RuneCountInString(note) > 500 {
		return taste.AuditionChoice{}, errors.New("note must be 500 characters or fewer")
	}

	choice := taste.AuditionChoice{Topic: topic, Note: note}
	seen := map[string]bool{}
	wanted := strings.ToLower(strings.TrimSpace(input.Chosen))
	for i, option := range input.Options {
		label := strings.TrimSpace(option.Label)
		description := strings.TrimSpace(option.Description)
		if n := utf8.RuneCountInString(label); n < 1 || n > maxAuditionLabelLen {
			return taste.AuditionChoice{}, fmt.Errorf("options[%d]: label must be 1 to %d characters", i, maxAuditionLabelLen)
		}
		if n := utf8.RuneCountInString(description); n < 1 || n > maxAuditionDescLen {
			return taste.AuditionChoice{}, fmt.Errorf("option %s: description must be 1 to %d characters", label, maxAuditionDescLen)
		}
		key := strings.ToLower(label)
		if seen[key] {
			return taste.AuditionChoice{}, fmt.Errorf("label %q is used twice", label)
		}
		seen[key] = true
		if key == wanted {
			choice.Chosen = label
		}
		choice.Options = append(choice.Options, taste.AuditionOption{Label: label, Description: description})
	}
	if choice.Chosen == "" {
		return taste.AuditionChoice{}, fmt.Errorf("chosen names %q, which is not one of the options", input.Chosen)
	}
	return choice, nil
}

func auditionChoiceOutput(choice taste.AuditionChoice) AuditionChoiceOutput {
	out := AuditionChoiceOutput{
		Topic:      choice.Topic,
		Chosen:     choice.Chosen,
		Note:       choice.Note,
		RecordedOn: choice.RecordedAt.Format("2006-01-02"),
	}
	for _, option := range choice.Options {
		if option.Label == choice.Chosen {
			out.ChosenDescription = option.Description
		} else {
			out.Others = append(out.Others, option.Label+": "+option.Description)
		}
	}
	return out
}

func NewAbletonGetTasteProfile(g *genkit.Genkit, store tasteStore) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_taste_profile",
		"Ableton Live: summarize saved A/B preferences, list the last audition choices, and suggest the next comparison. Use when the listener asks what to try next; not needed when they have already said what to compare.",
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

	if err := validateTasteInstrumentVariation(instrument, variation); err != nil {
		return taste.Preference{}, err
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

func validateTasteInstrumentVariation(instrument, variation string) error {
	switch instrument {
	case "drum":
		if !isDrumVariation(variation) {
			return errors.New("drum variation must be groove, density, or fill")
		}
	case "bass":
		if !isBassVariation(variation) {
			return errors.New("bass variation must be octave_up, octave_down, staccato, or groove")
		}
	case "scene":
		if !isSceneVariation(variation) {
			return errors.New("scene variation must be lift or pullback")
		}
	case "mix":
		if !isMixVariation(variation) {
			return errors.New("mix variation must be volume")
		}
	case "fx":
		if !isFXVariation(variation) {
			return errors.New("fx variation must be bypass")
		}
	default:
		return errors.New("instrument must be drum, bass, scene, mix, or fx")
	}
	return nil
}

func isSceneVariation(variation string) bool {
	return variation == "lift" || variation == "pullback"
}

func isMixVariation(variation string) bool {
	return variation == "volume"
}

func isFXVariation(variation string) bool {
	return variation == "bypass"
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

	// The newest choices first: they say where the listener's taste is now.
	var recent []AuditionChoiceOutput
	for i := len(profile.AuditionChoices) - 1; i >= 0 && len(recent) < recentAuditionChoices; i-- {
		recent = append(recent, auditionChoiceOutput(profile.AuditionChoices[i]))
	}

	return TasteProfileOutput{
		ProfilePath:             path,
		PreferencesRecorded:     len(profile.Preferences),
		Summaries:               summaries,
		NextSuggestions:         nextTasteSuggestions(summaries),
		AuditionChoicesRecorded: len(profile.AuditionChoices),
		RecentAuditionChoices:   recent,
	}
}

func nextTasteSuggestions(summaries []TasteSummary) []string {
	byInstrument := map[string]map[string]int{}
	for _, instrument := range tasteInstrumentOrder {
		byInstrument[instrument] = map[string]int{}
	}
	for _, summary := range summaries {
		if _, ok := byInstrument[summary.Instrument]; ok {
			byInstrument[summary.Instrument][summary.Variation] = summary.Accepted + summary.Rejected
		}
	}

	suggestions := make([]string, 0, len(tasteInstrumentOrder))
	for _, instrument := range tasteInstrumentOrder {
		next := leastComparedVariation(instrument, byInstrument[instrument])
		suggestions = append(suggestions,
			fmt.Sprintf("Try a %s %s variation next; it has been compared least often.", instrument, next),
		)
	}
	return suggestions
}

func leastComparedVariation(instrument string, counts map[string]int) string {
	candidates := tasteVariationsFor(instrument)
	next := candidates[0]
	for _, candidate := range candidates[1:] {
		if counts[candidate] < counts[next] {
			next = candidate
		}
	}
	return next
}

func tasteVariationsFor(instrument string) []string {
	switch instrument {
	case "drum":
		return []string{"density", "fill", "groove"}
	case "bass":
		return []string{"groove", "octave_down", "octave_up", "staccato"}
	case "scene":
		return []string{"lift", "pullback"}
	case "mix":
		return []string{"volume"}
	case "fx":
		return []string{"bypass"}
	default:
		return nil
	}
}
