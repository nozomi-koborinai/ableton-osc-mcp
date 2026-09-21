package tools

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/taste"
)

func TestValidateTastePreference(t *testing.T) {
	t.Parallel()

	got, err := validateTastePreference(RecordVariationPreferenceInput{
		Instrument: " DRUM ",
		Variation:  "Groove",
		Preferred:  "variation",
		Note:       "More relaxed",
	})
	if err != nil {
		t.Fatalf("validateTastePreference() error = %v", err)
	}
	if got.Instrument != "drum" || got.Variation != "groove" || got.Preferred != "variation" {
		t.Errorf("preference = %#v", got)
	}

	_, err = validateTastePreference(RecordVariationPreferenceInput{
		Instrument: "drum",
		Variation:  "octave_up",
		Preferred:  "variation",
	})
	if err == nil {
		t.Fatal("expected invalid drum variation error")
	}

	got, err = validateTastePreference(RecordVariationPreferenceInput{
		Instrument: "scene",
		Variation:  "lift",
		Preferred:  "source",
	})
	if err != nil {
		t.Fatalf("scene preference error = %v", err)
	}
	if got.Instrument != "scene" || got.Variation != "lift" {
		t.Errorf("scene preference = %#v", got)
	}

	got, err = validateTastePreference(RecordVariationPreferenceInput{
		Instrument: "mix",
		Variation:  "volume",
		Preferred:  "variation",
	})
	if err != nil {
		t.Fatalf("mix preference error = %v", err)
	}
	if got.Instrument != "mix" || got.Variation != "volume" {
		t.Errorf("mix preference = %#v", got)
	}
}

func TestTasteProfileOutput(t *testing.T) {
	t.Parallel()

	profile := taste.Profile{
		Version: 1,
		Preferences: []taste.Preference{
			{Instrument: "drum", Variation: "groove", Preferred: "variation"},
			{Instrument: "drum", Variation: "groove", Preferred: "source"},
			{Instrument: "bass", Variation: "octave_up", Preferred: "variation"},
			{Instrument: "scene", Variation: "lift", Preferred: "variation"},
		},
	}
	got := tasteProfileOutput(profile, "/tmp/taste-profile.json")
	if got.PreferencesRecorded != 4 {
		t.Errorf("PreferencesRecorded = %d, want 4", got.PreferencesRecorded)
	}
	if len(got.Summaries) != 3 {
		t.Fatalf("summaries = %#v, want 3", got.Summaries)
	}
	joined := strings.Join(got.NextSuggestions, "\n")
	if !strings.Contains(joined, "drum density") || !strings.Contains(joined, "bass groove") {
		t.Errorf("NextSuggestions = %v", got.NextSuggestions)
	}
	if !strings.Contains(joined, "scene pullback") || !strings.Contains(joined, "mix volume") {
		t.Errorf("NextSuggestions missing scene/mix = %v", got.NextSuggestions)
	}
	if !strings.Contains(joined, "fx bypass") {
		t.Errorf("NextSuggestions missing fx = %v", got.NextSuggestions)
	}
	if len(got.NextSuggestions) != 5 {
		t.Errorf("want 5 family suggestions, got %v", got.NextSuggestions)
	}
}

func TestTasteProfileColdStartSuggestions(t *testing.T) {
	t.Parallel()

	got := tasteProfileOutput(taste.Profile{Version: 1}, "/tmp/taste-profile.json")
	if got.PreferencesRecorded != 0 {
		t.Fatalf("PreferencesRecorded = %d", got.PreferencesRecorded)
	}
	if len(got.NextSuggestions) != 5 {
		t.Fatalf("cold start suggestions = %v, want 5", got.NextSuggestions)
	}
	joined := strings.Join(got.NextSuggestions, "\n")
	for _, want := range []string{"bass groove", "drum density", "fx bypass", "mix volume", "scene lift"} {
		if !strings.Contains(joined, want) {
			t.Errorf("cold start missing %q in %v", want, got.NextSuggestions)
		}
	}
}

func TestTasteStoreRoundTripForToolProfile(t *testing.T) {
	t.Parallel()

	store, err := taste.NewStore(filepath.Join(t.TempDir(), "profile.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	preference, err := validateTastePreference(RecordVariationPreferenceInput{
		Instrument: "bass",
		Variation:  "staccato",
		Preferred:  "variation",
	})
	if err != nil {
		t.Fatalf("validateTastePreference() error = %v", err)
	}
	profile, err := store.Record(preference)
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	got := tasteProfileOutput(profile, store.Path())
	if got.ProfilePath != store.Path() || got.PreferencesRecorded != 1 {
		t.Errorf("profile output = %#v", got)
	}
}

func auditionChoiceInput() RecordAuditionChoiceInput {
	return RecordAuditionChoiceInput{
		Topic: "808 の鳴らし方",
		Options: []AuditionChoiceOption{
			{Label: "X", Description: "今のまま"},
			{Label: "P", Description: "2 拍目の裏で跳ねる"},
			{Label: "Q", Description: "小節の頭だけ伸ばす"},
		},
		Chosen: "p",
		Note:   "P がちかいかな",
	}
}

func TestValidateAuditionChoice(t *testing.T) {
	t.Parallel()

	got, err := validateAuditionChoice(auditionChoiceInput())
	if err != nil {
		t.Fatalf("validateAuditionChoice() error = %v", err)
	}
	// The label is kept the way the option spells it, however the answer was typed.
	if got.Chosen != "P" || got.Topic != "808 の鳴らし方" || len(got.Options) != 3 || got.Note != "P がちかいかな" {
		t.Errorf("choice = %#v", got)
	}

	for name, change := range map[string]func(*RecordAuditionChoiceInput){
		"no topic":   func(in *RecordAuditionChoiceInput) { in.Topic = "  " },
		"long topic": func(in *RecordAuditionChoiceInput) { in.Topic = strings.Repeat("あ", 81) },
		"one option": func(in *RecordAuditionChoiceInput) { in.Options = in.Options[:1] },
		"nine options": func(in *RecordAuditionChoiceInput) {
			in.Options = append(in.Options, make([]AuditionChoiceOption, 6)...)
		},
		"duplicate label":   func(in *RecordAuditionChoiceInput) { in.Options[2].Label = "x" },
		"empty description": func(in *RecordAuditionChoiceInput) { in.Options[1].Description = "" },
		"unknown choice":    func(in *RecordAuditionChoiceInput) { in.Chosen = "Z" },
		"long note":         func(in *RecordAuditionChoiceInput) { in.Note = strings.Repeat("あ", 501) },
	} {
		input := auditionChoiceInput()
		change(&input)
		if _, err := validateAuditionChoice(input); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestTasteProfileShowsTheLastTenAuditionChoicesNewestFirst(t *testing.T) {
	t.Parallel()

	profile := taste.Profile{Version: 1}
	for i := 0; i < 12; i++ {
		profile.AuditionChoices = append(profile.AuditionChoices, taste.AuditionChoice{
			Topic: fmt.Sprintf("round %d", i),
			Options: []taste.AuditionOption{
				{Label: "X", Description: "as it is"},
				{Label: "A", Description: "brighter"},
			},
			Chosen:     "A",
			Note:       "A",
			RecordedAt: time.Date(2026, time.September, 1+i, 12, 0, 0, 0, time.UTC),
		})
	}
	out := tasteProfileOutput(profile, "/tmp/taste.json")
	if out.AuditionChoicesRecorded != 12 || len(out.RecentAuditionChoices) != 10 {
		t.Fatalf("recorded = %d, recent = %d; want 12 and 10", out.AuditionChoicesRecorded, len(out.RecentAuditionChoices))
	}
	first := out.RecentAuditionChoices[0]
	if first.Topic != "round 11" || first.Chosen != "A" || first.ChosenDescription != "brighter" || first.RecordedOn != "2026-09-12" {
		t.Errorf("first = %#v, want round 11 (the newest), A: brighter, 2026-09-12", first)
	}
	if len(first.Others) != 1 || first.Others[0] != "X: as it is" {
		t.Errorf("others = %v, want what A was chosen over", first.Others)
	}
	if last := out.RecentAuditionChoices[9]; last.Topic != "round 2" {
		t.Errorf("last = %#v, want round 2", last)
	}
}

func TestRecordAuditionChoiceWritesToTheProfile(t *testing.T) {
	t.Parallel()

	store, err := taste.NewStore(filepath.Join(t.TempDir(), "taste-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := recordAuditionChoice(store, auditionChoiceInput())
	if err != nil {
		t.Fatalf("recordAuditionChoice() error = %v", err)
	}
	if out.RecordedAuditionChoice == nil || out.RecordedAuditionChoice.Chosen != "P" || out.RecordedAuditionChoice.ChosenDescription != "2 拍目の裏で跳ねる" {
		t.Errorf("recorded = %#v", out.RecordedAuditionChoice)
	}
	loaded, err := store.Load()
	if err != nil || len(loaded.AuditionChoices) != 1 {
		t.Fatalf("Load() = %#v, %v; want the choice on disk", loaded, err)
	}

	bad := auditionChoiceInput()
	bad.Chosen = "Z"
	if _, err := recordAuditionChoice(store, bad); err == nil {
		t.Error("an unknown choice should fail")
	}
	if loaded, _ := store.Load(); len(loaded.AuditionChoices) != 1 {
		t.Errorf("choices on disk = %d; a rejected choice must not be written", len(loaded.AuditionChoices))
	}
}
