package taste

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRecordAndLoad(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "taste-profile.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	recordedAt := time.Date(2026, time.July, 18, 7, 0, 0, 0, time.UTC)
	profile, err := store.Record(Preference{
		Instrument: "drum",
		Variation:  "groove",
		Preferred:  "variation",
		Note:       "More relaxed",
		RecordedAt: recordedAt,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if profile.Version != profileVersion || len(profile.Preferences) != 1 {
		t.Fatalf("Record() profile = %#v", profile)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Preferences) != 1 {
		t.Fatalf("Load() preferences = %v, want one", loaded.Preferences)
	}
	if got := loaded.Preferences[0]; got.Instrument != "drum" || got.Variation != "groove" || got.Preferred != "variation" || !got.RecordedAt.Equal(recordedAt) {
		t.Errorf("loaded preference = %#v", got)
	}
}

func TestStoreLoadMissingReturnsEmptyProfile(t *testing.T) {
	t.Parallel()

	store, err := NewStore(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	profile, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if profile.Version != profileVersion || len(profile.Preferences) != 0 {
		t.Errorf("Load() = %#v, want empty current profile", profile)
	}
}

func TestNewStoreRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	if _, err := NewStore(""); err == nil {
		t.Fatal("NewStore(\"\") error = nil, want error")
	}
}

func TestStoreKeepsAuditionChoicesNextToPreferences(t *testing.T) {
	t.Parallel()

	store, err := NewStore(filepath.Join(t.TempDir(), "taste-profile.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if _, err := store.Record(Preference{Instrument: "drum", Variation: "groove", Preferred: "source"}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	choice := AuditionChoice{
		Topic: "how the 808 plays",
		Options: []AuditionOption{
			{Label: "X", Description: "as it is"},
			{Label: "P", Description: "bounce on the and of 2"},
		},
		Chosen: "P",
		Note:   "P is close",
	}
	profile, err := store.RecordAuditionChoice(choice)
	if err != nil {
		t.Fatalf("RecordAuditionChoice() error = %v", err)
	}
	if len(profile.Preferences) != 1 || len(profile.AuditionChoices) != 1 || profile.AuditionChoices[0].RecordedAt.IsZero() {
		t.Fatalf("profile = %#v, want one preference and one dated choice", profile)
	}

	// A preference recorded later must not drop the choices, and the other way round.
	if _, err := store.Record(Preference{Instrument: "bass", Variation: "groove", Preferred: "variation"}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Preferences) != 2 || len(loaded.AuditionChoices) != 1 {
		t.Fatalf("loaded = %#v, want two preferences and one choice", loaded)
	}
	if got := loaded.AuditionChoices[0]; got.Topic != choice.Topic || got.Chosen != "P" || len(got.Options) != 2 || got.Options[1].Description != "bounce on the and of 2" {
		t.Errorf("loaded choice = %#v", got)
	}
}

func TestStoreReadsAProfileWrittenBeforeAuditionChoices(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "taste-profile.json")
	old := `{"version": 1, "preferences": [{"instrument": "drum", "variation": "fill", "preferred": "source", "recorded_at": "2026-07-18T07:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	loaded, err := store.Load()
	if err != nil || len(loaded.Preferences) != 1 || len(loaded.AuditionChoices) != 0 {
		t.Fatalf("Load() = %#v, %v; want the old preference and no choices", loaded, err)
	}
	profile, err := store.RecordAuditionChoice(AuditionChoice{Topic: "t", Options: []AuditionOption{{Label: "X", Description: "d"}, {Label: "A", Description: "d"}}, Chosen: "A"})
	if err != nil || len(profile.Preferences) != 1 || len(profile.AuditionChoices) != 1 {
		t.Fatalf("RecordAuditionChoice() = %#v, %v", profile, err)
	}
}
