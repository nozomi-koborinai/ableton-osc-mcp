package tools

import (
	"path/filepath"
	"strings"
	"testing"

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
