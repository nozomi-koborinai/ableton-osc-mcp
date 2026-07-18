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
}

func TestTasteProfileOutput(t *testing.T) {
	t.Parallel()

	profile := taste.Profile{
		Version: 1,
		Preferences: []taste.Preference{
			{Instrument: "drum", Variation: "groove", Preferred: "variation"},
			{Instrument: "drum", Variation: "groove", Preferred: "source"},
			{Instrument: "bass", Variation: "octave_up", Preferred: "variation"},
		},
	}
	got := tasteProfileOutput(profile, "/tmp/taste-profile.json")
	if got.PreferencesRecorded != 3 {
		t.Errorf("PreferencesRecorded = %d, want 3", got.PreferencesRecorded)
	}
	if len(got.Summaries) != 2 {
		t.Fatalf("summaries = %#v, want 2", got.Summaries)
	}
	if got.Summaries[1].Instrument != "drum" || got.Summaries[1].Variation != "groove" || got.Summaries[1].Accepted != 1 || got.Summaries[1].Rejected != 1 {
		t.Errorf("drum summary = %#v, want groove 1/1", got.Summaries[1])
	}
	joined := strings.Join(got.NextSuggestions, "\n")
	if !strings.Contains(joined, "drum density") || !strings.Contains(joined, "bass groove") {
		t.Errorf("NextSuggestions = %v", got.NextSuggestions)
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
